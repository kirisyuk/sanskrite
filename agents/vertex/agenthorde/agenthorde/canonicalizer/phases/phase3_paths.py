"""Phase 3: canonical path resolution (deterministic), geo hints, collision merge, path validation."""

from __future__ import annotations

from rapidfuzz import fuzz

from agenthorde.canonicalizer.geo_iso import collect_geo_iso2_from_reasoning_graph, iso2_from_country_names
from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.schemas import (
    Candidates,
    CanonicalEntity,
    MergeCandidate,
    UnknownEntityTypeCandidate,
    ValidationIssue,
)
from agenthorde.canonicalizer.slug import extract_iso2_from_text, merge_iso2_hints, normalize_surface, slugify
from agenthorde.canonicalizer.taxonomy import INCOTERMS_2020
from agenthorde.canonicalizer.validation import validate_canonical_path


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


def build_canonical_path(ent: CanonicalEntity, ws: WorkingState, narrative_iso2: str | None) -> str:
    t = ent.type
    iso2 = _iso2_for_entity(ent, ws, narrative_iso2)
    primary = ent.surface_forms[0] if ent.surface_forms else "unknown"
    slug = slugify(primary)
    st = (ent.subtype or "entity").replace(" ", "_").lower()[:24]
    path = ""
    if t == "org":
        path = f"/org/{iso2}/{slug}"
    elif t == "person":
        path = f"/person/{iso2}/{slug}"
    elif t == "geo":
        path = f"/geo/{iso2}/{slug}"
    elif t == "site":
        path = f"/site/{iso2}/{slug}"
    elif t == "product":
        fam = slugify(ent.subtype or "general")
        path = f"/product/{fam}/{slug}"
    elif t == "vessel_class":
        path = f"/vessel_class/{iso2}/{slug}"
    elif t == "route":
        path = f"/route/{iso2}/{slug}"
    elif t == "market":
        path = f"/market/{iso2}/{slug}"
    elif t == "regime":
        path = f"/regime/{iso2}/{slug}"
    elif t == "project":
        path = f"/project/{iso2}/{slug}"
    elif t == "evidence":
        path = f"/evidence/{iso2}/{st}-{slug[:16]}"
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
