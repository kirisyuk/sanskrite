"""Phase 2: deduplicate mentions (fuzzy + product/gov dictionaries)."""

from __future__ import annotations

from rapidfuzz import fuzz

from agenthorde.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde.canonicalizer.schemas import Candidates, CanonicalEntity, ContextHints, EntityMention, MergeCandidate
from agenthorde.canonicalizer.slug import merge_iso2_hints, normalize_surface


def run_phase2(
    mentions: list[EntityMention],
    fuzzy_threshold: int,
    merge_candidate_lo: int,
    dict_runtime: DictionaryRuntime | None,
    candidates: Candidates,
) -> list[CanonicalEntity]:
    if not mentions:
        return []

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

    def pair_score(i: int, j: int) -> int:
        mi, mj = mentions[i], mentions[j]
        if (mi.type or "") != (mj.type or ""):
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
    return entities
