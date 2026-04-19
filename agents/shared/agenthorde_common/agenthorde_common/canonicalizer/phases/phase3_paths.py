"""Phase 3: canonical path resolution (deterministic), geo hints, collision merge, path validation."""

from __future__ import annotations

from rapidfuzz import fuzz

from agenthorde_common.canonicalizer.geo_iso import collect_geo_iso2_from_reasoning_graph, iso2_from_country_names
from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.schemas import (
    Candidates,
    CanonicalEntity,
    MergeCandidate,
    UnknownEntityTypeCandidate,
    ValidationIssue,
)
from agenthorde_common.canonicalizer.slug import extract_iso2_from_text, merge_iso2_hints, normalize_surface, slugify
from agenthorde_common.canonicalizer.taxonomy import (
    INCOTERMS_2020,
    normalize_community_subtype,
    normalize_evidence_subtype,
    normalize_mechanism_category,
    normalize_mobile_asset_kind,
    normalize_product_family,
    sanctions_regime_canonical_path,
    sanctions_regime_code_for_alias,
)
from agenthorde_common.canonicalizer.validation import validate_canonical_path


# Precomputed normalized Incoterm surfaces; used to drop Incoterm codes that were
# mis-classified by phase 1 as `regime` entities. The phase 1 prompt forbids this
# explicitly, but LLM drift is treated here as a defensive guardrail: Incoterms
# are edge-attribute values, not standalone entities.
_INCOTERM_NORM_SET: frozenset[str] = frozenset(
    normalize_surface(code) for code in INCOTERMS_2020
)


def _is_incoterm_surface(surface: str) -> bool:
    ns = normalize_surface(surface or "")
    if not ns:
        return False
    if ns in _INCOTERM_NORM_SET:
        return True
    # Allow prefixes like "CIF China" / "FOB Singapore" which keep the code as
    # the leading token. Normalized surface uses single spaces.
    head = ns.split(" ", 1)[0]
    return head in _INCOTERM_NORM_SET


def _claim_text_for_entity(ent: CanonicalEntity, ws: WorkingState) -> str:
    parts: list[str] = []
    idset = set(ent.claim_refs)
    for n in ws.all_nodes:
        if n.id in idset:
            parts.append(n.text)
    return " ".join(parts)


def _iso2_for_entity(ent: CanonicalEntity, ws: WorkingState, narrative_iso2: str | None) -> str:
    blob = _claim_text_for_entity(ent, ws)
    h = merge_iso2_hints(
        ent.context_hints.iso2_hint,
        extract_iso2_from_text(blob),
        iso2_from_country_names(blob),
        narrative_iso2,
    )
    return h if h and len(h) == 2 else "na"


def _canonical_regime_path_from_surfaces(ent: CanonicalEntity, iso2_hint: str | None) -> str:
    # iso2_hint is the jurisdiction signal inferred upstream (context hints +
    # surface tokens + narrative). Passing it down lets the catalog lookup
    # refuse matches against regimes of a different jurisdiction — so e.g.
    # "EU Chips Act" (hint="eu") never resolves into CHIPS_ACT_GUARDRAILS
    # (jurisdiction="us") purely through the generic "chips act" alias.
    for sf in ent.surface_forms or ():
        code = sanctions_regime_code_for_alias(sf, iso2_hint=iso2_hint)
        if code:
            cp = sanctions_regime_canonical_path(code)
            if cp:
                return cp
    return ""


def build_canonical_path(ent: CanonicalEntity, ws: WorkingState, narrative_iso2: str | None) -> str:
    t = ent.type
    iso2 = _iso2_for_entity(ent, ws, narrative_iso2)
    primary = ent.surface_forms[0] if ent.surface_forms else "unknown"
    slug = slugify(primary)
    path = ""
    if t == "regime":
        # iso2 from _iso2_for_entity is always non-empty ("na" fallback); pass
        # None to the catalog when we truly have no jurisdiction signal, so
        # backward-compatible lookup (no guard) still works for orphan regimes.
        regime_hint = iso2 if iso2 and iso2 != "na" else None
        catalog_hit = _canonical_regime_path_from_surfaces(ent, regime_hint)
        if catalog_hit:
            return catalog_hit
    if t == "org":
        path = f"/org/{iso2}/{slug}"
    elif t == "person":
        path = f"/person/{iso2}/{slug}"
    elif t == "geo":
        path = f"/geo/{iso2}/{slug}"
    elif t == "site":
        path = f"/site/{iso2}/{slug}"
    elif t == "product":
        fam = normalize_product_family(ent.subtype)
        path = f"/product/{fam}/{slug}"
    elif t == "vessel_class":
        path = f"/vessel_class/{iso2}/{slug}"
    elif t == "mobile_asset":
        # /mobile_asset/{kind}/{geo}/{slug}-{id8} — `kind` is closed enum
        # MobileAssetKind. The trailing -{id8} disambiguates two assets that
        # share the same surface name (e.g. multiple "Pacific Star" vessels);
        # we lift it from the agent-supplied identifier when available, else
        # derive from the entity_id hash to keep paths stable across reruns.
        kind = normalize_mobile_asset_kind(ent.subtype)
        ident = ""
        if ent.attributes:
            ident_val = (
                ent.attributes.get("imo")
                or ent.attributes.get("mmsi")
                or ent.attributes.get("tail_number")
                or ent.attributes.get("identifier")
            )
            if ident_val is not None:
                ident = slugify(str(ident_val))[:8]
        if not ident:
            ident = (slugify(ent.entity_id or "") or slug)[:8]
        path = f"/mobile_asset/{kind}/{iso2}/{slug}-{ident}"
    elif t == "route":
        path = f"/route/{iso2}/{slug}"
    elif t == "market":
        path = f"/market/{iso2}/{slug}"
    elif t == "regime":
        path = f"/regime/{iso2}/{slug}"
    elif t == "project":
        path = f"/project/{iso2}/{slug}"
    elif t == "evidence":
        ev_subtype = normalize_evidence_subtype(ent.subtype)
        path = f"/evidence/{iso2}/{ev_subtype}-{slug[:16]}"
    elif t == "community":
        # /community/{geo}/{subtype}/{slug} — subtype is a real path segment,
        # not collapsed into the slug. Mirrors core canonicalizer slot order.
        community_subtype = normalize_community_subtype(ent.subtype)
        path = f"/community/{iso2}/{community_subtype}/{slug}"
    elif t == "decision":
        # /decision/{domain-slug}/{slug} — domain-slug captures which strategic
        # domain the decision belongs to (export_controls, antitrust,
        # standards, monetary_policy, etc.). When the agent fails to emit a
        # domain hint, we fall back to "general" so the path remains valid.
        domain = (ent.attributes.get("domain_slug") if ent.attributes else None) or "general"
        domain_seg = normalize_surface(str(domain)).replace(" ", "-") or "general"
        path = f"/decision/{domain_seg}/{slug}"
    elif t == "mechanism":
        # /mechanism/{category}/{slug} — category is closed enum
        # MechanismCategory; mechanisms are reusable and not jurisdiction-bound,
        # so iso2 does NOT appear in the path.
        mech_cat = normalize_mechanism_category(ent.subtype)
        path = f"/mechanism/{mech_cat}/{slug}"
    else:
        path = f"/org/na/{slug}"
    return path


def _merge_entities(primary: CanonicalEntity, secondary: CanonicalEntity) -> None:
    primary.surface_forms = sorted(set(primary.surface_forms + secondary.surface_forms))
    primary.claim_refs = sorted(set(primary.claim_refs + secondary.claim_refs))
    w1 = max(1, primary.mention_count)
    w2 = max(1, secondary.mention_count)
    primary.per_claim_confidence = (
        (primary.per_claim_confidence * w1) + (secondary.per_claim_confidence * w2)
    ) / float(w1 + w2)
    primary.mention_count += secondary.mention_count
    if secondary.context_hints.iso2_hint and not primary.context_hints.iso2_hint:
        primary.context_hints.iso2_hint = secondary.context_hints.iso2_hint
    primary.merge_reason = "path_collision_merge"
    pvs = primary.attributes.get("variants_seen")
    svs = secondary.attributes.get("variants_seen")
    if pvs is not None or svs is not None:
        pl = pvs if isinstance(pvs, list) else ([str(pvs)] if pvs is not None else [])
        sl = svs if isinstance(svs, list) else ([str(svs)] if svs is not None else [])
        primary.attributes["variants_seen"] = sorted(set(pl + sl))
    for k, v in secondary.attributes.items():
        if k == "variants_seen":
            continue
        primary.attributes.setdefault(k, v)


def run_phase3(
    entities: list[CanonicalEntity],
    ws: WorkingState,
    candidates: Candidates,
    collision_fuzzy_threshold: int,
) -> tuple[list[CanonicalEntity], list[ValidationIssue]]:
    narrative_iso2 = collect_geo_iso2_from_reasoning_graph(
        [n.text for n in ws.claim_nodes],
        [n.text for n in ws.hypothesis_nodes],
    )

    # Defensive filter: drop regime entities whose surface is actually an
    # Incoterm code. These are edge-attribute values, not entities; the
    # phase 1 prompt forbids emitting them, but LLMs occasionally comply
    # imperfectly. Record each drop as a candidate so it's visible.
    filtered: list[CanonicalEntity] = []
    for ent in entities:
        if ent.type == "regime":
            surfs = ent.surface_forms or []
            if surfs and all(_is_incoterm_surface(s) for s in surfs):
                candidates.unknown_entity_types.append(
                    UnknownEntityTypeCandidate(
                        surface=surfs[0],
                        reason="incoterm_mis_classified_as_regime",
                        claim_refs=list(ent.claim_refs),
                    )
                )
                continue
        filtered.append(ent)
    entities = filtered

    for ent in entities:
        ent.canonical_path = build_canonical_path(ent, ws, narrative_iso2)

    path_to_index: dict[str, int] = {}
    to_remove: set[int] = set()
    for i, ent in enumerate(entities):
        if i in to_remove:
            continue
        p = ent.canonical_path
        if p not in path_to_index:
            path_to_index[p] = i
            continue
        j = path_to_index[p]
        other = entities[j]
        r = 0
        if ent.type == other.type:
            r = int(
                fuzz.token_set_ratio(
                    slugify(ent.surface_forms[0] if ent.surface_forms else ""),
                    slugify(other.surface_forms[0] if other.surface_forms else ""),
                )
            )
            if r >= collision_fuzzy_threshold:
                _merge_entities(other, ent)
                to_remove.add(i)
                continue
        candidates.merge_candidates.append(
            MergeCandidate(
                entity_ids=[other.entity_id, ent.entity_id],
                surfaces=list(other.surface_forms) + list(ent.surface_forms),
                score=float(r) / 100.0 if ent.type == other.type else 0.0,
                reason="path_collision_no_merge",
            )
        )
        base = p
        suf = 2
        while f"{base}-{suf}" in path_to_index:
            suf += 1
        new_p = f"{base}-{suf}"
        ent.canonical_path = new_p
        path_to_index[new_p] = i

    survivors = [e for k, e in enumerate(entities) if k not in to_remove]

    seen_issue_keys: set[tuple[str, str]] = set()
    path_issues: list[ValidationIssue] = []
    for ent in survivors:
        for iss in validate_canonical_path(ent.canonical_path):
            key = (iss.code, ent.canonical_path)
            if key in seen_issue_keys:
                continue
            seen_issue_keys.add(key)
            path_issues.append(iss)

    for i, ent in enumerate(survivors):
        ent.entity_id = f"e{i}"
    return survivors, path_issues
