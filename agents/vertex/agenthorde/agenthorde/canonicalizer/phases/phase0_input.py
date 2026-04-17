"""Phase 0: assemble normalized working state from session + worker outputs."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

from pydantic import ValidationError

from agenthorde.schemas.graph import ReasoningGraph, ReasoningNode
from agenthorde.schemas.state import SessionPack
from agenthorde.canonicalizer.schemas import ValidationIssue


@dataclass
class WorkingState:
    claim_nodes: list[ReasoningNode] = field(default_factory=list)
    counter_nodes: list[ReasoningNode] = field(default_factory=list)
    hypothesis_nodes: list[ReasoningNode] = field(default_factory=list)
    all_nodes: list[ReasoningNode] = field(default_factory=list)
    chain_summaries: list[str] = field(default_factory=list)
    final_answer: str = ""
    working_graph_text: str = ""
    warnings: list[str] = field(default_factory=list)
    input_issues: list[ValidationIssue] = field(default_factory=list)
    analyzer_relations: list[dict[str, Any]] = field(default_factory=list)


def _blob_nonempty(blob: Any) -> bool:
    if not isinstance(blob, dict):
        return False
    cs = str(blob.get("chainSummary") or blob.get("chain_summary") or "").strip()
    pn = blob.get("proposedNodes") or blob.get("proposed_nodes") or []
    oq = blob.get("openQuestions") or blob.get("open_questions") or []
    return bool(cs) or bool(pn) or bool(oq)


def validate_phase0_input(
    ws: WorkingState,
    analyzer_output: dict[str, Any],
    governance_output: dict[str, Any],
) -> list[ValidationIssue]:
    """
    Hard gate: need at least one of:
    - non-empty reasoning graph (any nodes), or
    - non-empty analyzer output (summary / proposed nodes / questions), or
    - non-empty governance output (same).
    """
    issues: list[ValidationIssue] = []
    graph_ok = len(ws.all_nodes) > 0
    analyzer_ok = _blob_nonempty(analyzer_output)
    gov_ok = _blob_nonempty(governance_output)
    if not graph_ok and not analyzer_ok and not gov_ok:
        issues.append(
            ValidationIssue(
                code="invalid_input",
                message="Canonicalizer input is empty: no reasoningGraph nodes and no analyzer/governance payload",
                detail={
                    "graphNodeCount": len(ws.all_nodes),
                    "hasAnalyzer": bool(analyzer_output),
                    "hasGovernance": bool(governance_output),
                },
            )
        )
    return issues


def run_phase0(
    pack: dict[str, Any],
    analyzer_output: dict[str, Any],
    governance_output: dict[str, Any],
    final_answer: str,
) -> WorkingState:
    ws = WorkingState(final_answer=final_answer or "")
    try:
        sp = SessionPack.model_validate(pack or {})
        rg = sp.reasoning_graph
    except ValidationError:
        try:
            rg = ReasoningGraph.model_validate((pack or {}).get("reasoningGraph") or {})
        except ValidationError:
            rg = ReasoningGraph()
    ws.all_nodes = list(rg.nodes)
    for n in rg.nodes:
        if n.kind == "claim":
            ws.claim_nodes.append(n)
        elif n.kind == "counter":
            ws.counter_nodes.append(n)
        elif n.kind == "hypothesis_anchor":
            ws.hypothesis_nodes.append(n)
    def _collect_rels(blob: Any) -> list[dict[str, Any]]:
        if not isinstance(blob, dict):
            return []
        raw = blob.get("proposedRelations") or blob.get("proposed_relations") or []
        out: list[dict[str, Any]] = []
        for r in raw or []:
            if isinstance(r, dict):
                out.append(r)
        return out

    for _key, blob in (("analyzer", analyzer_output), ("governance", governance_output)):
        if isinstance(blob, dict) and blob.get("chainSummary"):
            ws.chain_summaries.append(str(blob["chainSummary"]))
        elif isinstance(blob, dict) and blob.get("chain_summary"):
            ws.chain_summaries.append(str(blob["chain_summary"]))
        ws.analyzer_relations.extend(_collect_rels(blob))

    # pack.proposedRelations (persisted by supervisor merge) — read raw to survive
    # partial validation failures on the pack as a whole.
    pack_rels = (pack or {}).get("proposedRelations") or (pack or {}).get("proposed_relations") or []
    if isinstance(pack_rels, list):
        for r in pack_rels:
            if isinstance(r, dict):
                ws.analyzer_relations.append(r)

    # Dedup within ws.analyzer_relations on (src, dst, relation).
    seen: set[tuple[str, str, str]] = set()
    deduped: list[dict[str, Any]] = []
    for r in ws.analyzer_relations:
        k = (
            str(r.get("src") or "").strip().lower(),
            str(r.get("dst") or "").strip().lower(),
            str(r.get("relation") or "").strip().lower(),
        )
        if not k[0] or not k[1] or not k[2]:
            continue
        if k in seen:
            continue
        seen.add(k)
        deduped.append(r)
    ws.analyzer_relations = deduped
    wg = {}
    try:
        sp2 = SessionPack.model_validate(pack or {})
        wg = sp2.working_graph or {}
    except ValidationError:
        wg = (pack or {}).get("workingGraph") or {}
    try:
        ws.working_graph_text = json.dumps(wg, ensure_ascii=False)[:120_000]
    except (TypeError, ValueError):
        ws.working_graph_text = str(wg)[:120_000]
    if not ws.claim_nodes and not ws.chain_summaries:
        ws.warnings.append("empty_claims_and_summaries")
    ws.input_issues = validate_phase0_input(ws, analyzer_output, governance_output)
    return ws


def claims_as_prompt_block(nodes: list[ReasoningNode]) -> str:
    lines = []
    for n in nodes:
        lines.append(f"- id={n.id} kind={n.kind} text={n.text!r}")
    return "\n".join(lines) if lines else "(no claim nodes)"
