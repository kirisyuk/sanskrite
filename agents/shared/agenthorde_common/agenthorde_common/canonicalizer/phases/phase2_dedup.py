"""Phase 2: deduplicate mentions (fuzzy + product/gov dictionaries).

Identity gate
-------------
Mirrors the canonical-control-plane invariant from ``AGENTS.md``:

> identity is determined by the canonical_path, not by raw surface form.

For fragment classes that are inherently jurisdiction-bound
(``regime``, ``program``, ``project``, ``site``, ``geo``, ``person``,
``org``, ``community``, ``market``) the future canonical-path prefix
includes a country/jurisdiction segment. Two mentions with **incompatible**
``iso2_hint`` therefore can NEVER be the same fragment, regardless of
surface similarity. This guards the well-known ``EU Chips Act`` ↔
``US CHIPS Act`` collapse: their tokens overlap heavily but they are
distinct regulatory regimes.

Where the hint is missing on one side we still run the surface comparator
so we don't over-fragment; where it is missing on both sides the
comparator is unchanged. Where it differs and is set on both sides we
hard-stop the merge and emit a ``namespace_conflict`` MergeCandidate so
the next cycle / critic sees the near-collision explicitly.
"""

from __future__ import annotations

from rapidfuzz import fuzz

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.geo_iso import iso2_from_country_names
from agenthorde_common.canonicalizer.schemas import Candidates, CanonicalEntity, ContextHints, EntityMention, MergeCandidate
from agenthorde_common.canonicalizer.slug import extract_iso2_from_text, merge_iso2_hints, normalize_surface


# Fragment classes whose identity is intrinsically jurisdiction-scoped.
# A mention's iso2_hint, if known, becomes a hard discriminator for these
# classes during dedup. Other classes (vessel_class, route, evidence,
# product, mechanism) are global by construction or use a different namespace
# (product family / mechanism category) and are not subject to the gate.
#
# `program` (legacy) is now an `org` subtype rather than a standalone class —
# jurisdiction is enforced via the org canonical path slot.
# `decision` is jurisdiction-scoped because the same domain slug can host
# different decisions in different jurisdictions; mechanisms are reusable
# instruments and intentionally global.
_JURISDICTION_BOUND_TYPES: frozenset[str] = frozenset(
    {
        "regime",
        "project",
        "site",
        "geo",
        "person",
        "org",
        "community",
        "market",
        "decision",
    }
)


def _infer_iso2_for_mention(mention: EntityMention) -> str | None:
    """Best-effort jurisdiction inference for a single mention.

    Combines: existing context hint > surface ISO2 token > country-name
    scan over the surface. We do NOT scan the whole claim text here —
    that's phase 3's job and would cross-contaminate identity across
    different mentions sharing a paragraph.
    """
    explicit = mention.context_hints.iso2_hint
    if explicit and len(explicit) == 2 and explicit.isalpha():
        return explicit.lower()
    # Surface-driven: "EU Chips Act" → "eu" via country-name scan; "US BIS Entity List" → "us".
    surface = mention.surface or ""
    return iso2_from_country_names(surface) or extract_iso2_from_text(surface)


def run_phase2(
    mentions: list[EntityMention],
    fuzzy_threshold: int,
    merge_candidate_lo: int,
    dict_runtime: DictionaryRuntime | None,
    candidates: Candidates,
) -> tuple[list[CanonicalEntity], dict[int, str]]:
    """Returns (entities, mention_index -> canonical entity_id like e0, e1, …)."""
    if not mentions:
        return [], {}

    n = len(mentions)
    parent = list(range(n))

    def find(i: int) -> int:
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    def union(a: int, b: int) -> None:
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[rb] = ra

    dict_runtime = dict_runtime or DictionaryRuntime(None)

    # Pre-infer iso2 for jurisdiction-bound mentions so the gate has signal
    # even when the LLM omitted contextHints.iso2Hint. Cache by index to
    # keep pair_score O(1).
    inferred_iso2: list[str | None] = [None] * n
    for k, m in enumerate(mentions):
        if (m.type or "") in _JURISDICTION_BOUND_TYPES:
            inferred_iso2[k] = _infer_iso2_for_mention(m)

    def pair_score(i: int, j: int) -> int:
        mi, mj = mentions[i], mentions[j]
        if (mi.type or "") != (mj.type or ""):
            return 0
        # Jurisdiction gate: for jurisdiction-bound classes, two mentions
        # with KNOWN and DIFFERENT iso2 hints are different fragments by
        # definition (path-prefix conflict). No surface similarity may
        # collapse them. Record a namespace_conflict candidate so the gap
        # is visible to followUpPlan / critic.
        if (mi.type or "") in _JURISDICTION_BOUND_TYPES:
            iso_i = inferred_iso2[i]
            iso_j = inferred_iso2[j]
            if iso_i and iso_j and iso_i != iso_j:
                # Only emit a candidate if the surfaces would *otherwise*
                # have looked like a merge (cheap heuristic: token_set_ratio
                # over normalized surfaces ≥ merge_candidate_lo). Avoids
                # noise from totally unrelated entities of the same class.
                si_dbg = normalize_surface(mi.surface)
                sj_dbg = normalize_surface(mj.surface)
                near = int(fuzz.token_set_ratio(si_dbg, sj_dbg))
                if near >= merge_candidate_lo:
                    candidates.merge_candidates.append(
                        MergeCandidate(
                            entity_ids=[f"e{i}", f"e{j}"],
                            surfaces=[mi.surface, mj.surface],
                            score=float(near) / 100.0,
                            reason=f"namespace_conflict:{iso_i}_vs_{iso_j}",
                        )
                    )
                return 0
        si = normalize_surface(mi.surface)
        sj = normalize_surface(mj.surface)
        if si == sj:
            return 100
        if mi.type == "product" and dict_runtime:
            ci = dict_runtime.resolve_product_synonym(mi.surface) or dict_runtime.resolve_product_synonym(si)
            cj = dict_runtime.resolve_product_synonym(mj.surface) or dict_runtime.resolve_product_synonym(sj)
            if ci and cj and ci.lower() == cj.lower():
                return 100
            if ci and normalize_surface(ci) == sj:
                return 100
            if cj and normalize_surface(cj) == si:
                return 100
        if mi.type == "org" and dict_runtime:
            gi = dict_runtime.resolve_gov_synonym(mi.surface)
            gj = dict_runtime.resolve_gov_synonym(mj.surface)
            if gi and gj and gi == gj:
                return 100
        return int(fuzz.token_set_ratio(si, sj))

    for i in range(n):
        for j in range(i + 1, n):
            sc = pair_score(i, j)
            if sc >= fuzzy_threshold:
                union(i, j)
            elif merge_candidate_lo <= sc < fuzzy_threshold:
                candidates.merge_candidates.append(
                    MergeCandidate(
                        entity_ids=[f"e{i}", f"e{j}"],
                        surfaces=[mentions[i].surface, mentions[j].surface],
                        score=float(sc) / 100.0,
                    )
                )

    groups: dict[int, list[int]] = {}
    for i in range(n):
        r = find(i)
        groups.setdefault(r, []).append(i)

    groups_list = [groups[k] for k in sorted(groups.keys(), key=lambda x: min(groups[x]))]

    entities: list[CanonicalEntity] = []
    for gi, idxs in enumerate(groups_list):
        surfaces: list[str] = []
        claims: set[str] = set()
        hints = ContextHints()
        types: set[str] = set()
        subtypes: set[str] = set()
        pconf: list[float] = []
        merge_reason = "exact"
        merged_attrs: dict[str, object] = {}
        variants_seen: list[str] = []
        for i in idxs:
            m = mentions[i]
            surfaces.append(m.surface)
            types.add(m.type or "")
            if m.subtype:
                subtypes.add(m.subtype)
            for c in m.claim_refs:
                claims.add(c)
            pconf.append(m.per_claim_confidence)
            hints.iso2_hint = merge_iso2_hints(hints.iso2_hint, m.context_hints.iso2_hint)
            hints.sector_hint = hints.sector_hint or m.context_hints.sector_hint
            hints.as_of_hint = hints.as_of_hint or m.context_hints.as_of_hint
            for k, v in (m.attributes or {}).items():
                if k == "variants_seen":
                    if isinstance(v, list):
                        variants_seen.extend(str(x) for x in v)
                    elif v is not None:
                        variants_seen.append(str(v))
                else:
                    merged_attrs.setdefault(k, v)
            if len(idxs) > 1:
                sc_max = max(pair_score(i, j) for i in idxs for j in idxs if i < j)
                merge_reason = "dictionary" if sc_max >= 99 else "fuzzy"
        if variants_seen:
            merged_attrs["variants_seen"] = sorted(set(variants_seen))
        st = next(iter(subtypes)) if len(subtypes) == 1 else (next(iter(subtypes)) if subtypes else None)
        entities.append(
            CanonicalEntity(
                entity_id=f"e{gi}",
                type=next(iter(types)) if len(types) == 1 else "org",
                subtype=st,
                surface_forms=sorted(set(surfaces)),
                mention_count=len(idxs),
                merge_reason=merge_reason,
                claim_refs=sorted(claims),
                context_hints=hints,
                per_claim_confidence=sum(pconf) / max(len(pconf), 1),
                attributes=dict(merged_attrs),
            )
        )
    mention_index_to_entity_id: dict[int, str] = {}
    for gi, idxs in enumerate(groups_list):
        eid = f"e{gi}"
        for mi in idxs:
            mention_index_to_entity_id[mi] = eid
    return entities, mention_index_to_entity_id
