"""Phase 8: aggregate validation report."""

from __future__ import annotations

from agenthorde_common.canonicalizer.schemas import CanonicalEdge, CanonicalFragment, ValidationReport
from agenthorde_common.canonicalizer.validation import validate_graph


def run_phase8(fragments: list[CanonicalFragment], edges: list[CanonicalEdge]) -> ValidationReport:
    return validate_graph(fragments, edges)
