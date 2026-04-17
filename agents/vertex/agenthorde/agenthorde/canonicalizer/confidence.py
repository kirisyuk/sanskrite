"""Per-source priors and noisy-OR aggregation for edge / fragment confidence."""

from __future__ import annotations

from typing import Iterable

from agenthorde.canonicalizer.schemas import CanonicalEdge, CanonicalFragment, SourceRef


def domain_confidence(domain: str) -> float:
    d = (domain or "").lower()
    if not d:
        return 0.5
    if any(
        x in d
        for x in (
            "sec.gov",
            "europa.eu",
            "gov.uk",
            "gov.ae",
            "gov.sg",
            "gov.cn",
            "regulation",
            "court",
        )
    ):
        return 0.95
    if any(x in d for x in ("reuters.com", "bloomberg.com", "ft.com", "wsj.com", "nytimes.com")):
        return 0.8
    if any(x in d for x in ("wikipedia.org", "wikidata.org")):
        return 0.55
    if d.endswith(".edu"):
        return 0.75
    return 0.6


def noisy_or(weights: Iterable[float]) -> float:
    acc = 1.0
    any_w = False
    for w in weights:
        w = max(0.0, min(1.0, float(w)))
        any_w = True
        acc *= 1.0 - w
    if not any_w:
        return 0.0
    return 1.0 - acc


def edge_support_weight(per_claim: float, sources: list[SourceRef]) -> float:
    if not sources:
        return max(0.0, min(1.0, per_claim)) * 0.5
    src_scores = [domain_confidence(s.domain) for s in sources]
    best = max(src_scores) if src_scores else 0.5
    return max(0.0, min(1.0, per_claim * best))


def aggregate_edge_confidence(edge: CanonicalEdge, claim_to_sources: dict[str, list[SourceRef]]) -> float:
    weights: list[float] = []
    base = float(edge.confidence or 0.75)
    for cid in edge.source_claim_ids:
        srcs = claim_to_sources.get(cid, [])
        weights.append(edge_support_weight(base, srcs))
    if edge.metrics:
        for mv in edge.metrics.values():
            weights.append(mv.confidence)
    if not weights and edge.confidence > 0:
        weights.append(edge.confidence)
    return noisy_or(weights) if weights else 0.0


def aggregate_fragment_confidence(
    frag: CanonicalFragment,
    incident_edges: list[CanonicalEdge],
    claim_to_sources: dict[str, list[SourceRef]],
) -> float:
    edge_scores = [aggregate_edge_confidence(e, claim_to_sources) for e in incident_edges]
    base = min(1.0, 0.35 + 0.05 * frag.mention_count)
    if edge_scores:
        return max(base, max(edge_scores))
    if frag.metrics:
        return max(base, max((m.confidence for m in frag.metrics.values()), default=base))
    return base
