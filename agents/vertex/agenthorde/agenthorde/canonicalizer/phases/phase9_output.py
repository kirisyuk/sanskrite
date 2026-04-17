"""Phase 9: confidence aggregation and final CanonicalizerResult."""

from __future__ import annotations

from agenthorde.canonicalizer.confidence import aggregate_edge_confidence, aggregate_fragment_confidence
from agenthorde.canonicalizer.schemas import (
    Candidates,
    CanonicalEdge,
    CanonicalFragment,
    CanonicalGraph,
    CanonicalizerProvenance,
    CanonicalizerResult,
    EvidenceArchive,
    SourceRef,
    ValidationReport,
)
from agenthorde.canonicalizer.version import CANONICALIZER_VERSION


def _source_pool_from_archive(archive: EvidenceArchive) -> list[SourceRef]:
    """Fallback pool used when a claim has no explicit archive link."""
    return [SourceRef(url=s.url, title=s.title, domain=s.domain) for s in archive.sources]


def run_phase9(
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
    evidence_archive: EvidenceArchive,
    candidates: Candidates,
    validation_report: ValidationReport,
    run_id: str,
    phase_timings_ms: dict[str, float],
) -> CanonicalizerResult:
    # claim_id -> list of supporting SourceRefs (used by confidence aggregation).
    sources_by_id = {s.id: SourceRef(url=s.url, title=s.title, domain=s.domain) for s in evidence_archive.sources}
    claim_map: dict[str, list[SourceRef]] = {}
    for link in evidence_archive.links:
        ref = sources_by_id.get(link.source_id)
        if ref is None:
            continue
        claim_map.setdefault(link.claim_id, []).append(ref)

    pool = _source_pool_from_archive(evidence_archive)
    for e in edges:
        for cid in e.source_claim_ids:
            claim_map.setdefault(cid, pool)

    for e in edges:
        e.confidence = aggregate_edge_confidence(e, claim_map)

    for f in fragments:
        incident = [e for e in edges if e.source_fragment_id == f.id or e.target_fragment_id == f.id]
        f.confidence = aggregate_fragment_confidence(f, incident, claim_map)

    prov = CanonicalizerProvenance(
        run_id=run_id,
        canonicalizer_version=CANONICALIZER_VERSION,
        agent_run_ref=run_id,
        phase_timings_ms=phase_timings_ms,
    )
    return CanonicalizerResult(
        graph=CanonicalGraph(fragments=fragments, edges=edges),
        evidence_archive=evidence_archive,
        candidates=candidates,
        validation_report=validation_report,
        provenance=prov,
    )
