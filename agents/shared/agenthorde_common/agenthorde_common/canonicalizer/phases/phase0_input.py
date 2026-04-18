"""Phase 0: assemble normalized working state from session + worker outputs."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

from pydantic import ValidationError

from agenthorde_common.schemas.agents_io import decode_wire_relation_attributes
from agenthorde_common.schemas.graph import ReasoningGraph, ReasoningNode
from agenthorde_common.schemas.state import SessionPack
from agenthorde_common.canonicalizer.schemas import ValidationIssue


def _namespace_entity_id(origin: str, local_id: str) -> str:
    lid = (local_id or "").strip()
    if not lid:
        return ""
    for prefix in ("analyzer:", "governance:", "sanctions:", "pack:"):
        if lid.startswith(prefix):
            return lid
    return f"{origin}:{lid}"


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
    # Merged entity registry rows: { "namespacedId", "label", "origin" }
    analyzer_entities: list[dict[str, str]] = field(default_factory=list)
    # Normalized relation dicts: srcId, dstId, relation, attributes, sourceClaimIds (namespaced ids)
    analyzer_relations: list[dict[str, Any]] = field(default_factory=list)
    # namespaced registry id -> verbatim label (for phase4 fallback surface resolve)
    analyzer_entity_label_by_ns: dict[str, str] = field(default_factory=dict)
    # Populated after phase2 in pipeline.py: namespaced registry id -> canonical entity_id (e0, e1, …)
    agent_registry_id_to_entity_id: dict[str, str] = field(default_factory=dict)
    # Populated by phase1: subset of analyzer_entities ids the LLM listed in skippedRegistryIds.
    skipped_registry_ids: frozenset[str] = field(default_factory=frozenset)
    # Registry ids without a kept phase1 mention (excluding skipped): missing mention or unknown type.
    registry_ids_phase1_coverage_gap: frozenset[str] = field(default_factory=frozenset)


def _blob_nonempty(blob: Any) -> bool:
    if not isinstance(blob, dict):
        return False
    cs = str(blob.get("chainSummary") or blob.get("chain_summary") or "").strip()
    pn = blob.get("proposedNodes") or blob.get("proposed_nodes") or []
    oq = blob.get("openQuestions") or blob.get("open_questions") or []
    pe = blob.get("proposedEntities") or blob.get("proposed_entities") or []
    return bool(cs) or bool(pn) or bool(oq) or bool(pe)


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


def _collect_proposed_entities(blob: Any, origin: str) -> list[dict[str, str]]:
    if not isinstance(blob, dict):
        return []
    raw = blob.get("proposedEntities") or blob.get("proposed_entities") or []
    out: list[dict[str, str]] = []
    for row in raw or []:
        if not isinstance(row, dict):
            continue
        lid = str(row.get("id") or "").strip()
        if not lid:
            continue
        ns = _namespace_entity_id(origin, lid)
        label = str(row.get("label") or "").strip()
        if ns:
            out.append({"namespacedId": ns, "label": label, "origin": origin})
    return out


def _collect_relations(blob: Any, origin: str) -> list[dict[str, Any]]:
    if not isinstance(blob, dict):
        return []
    raw = blob.get("proposedRelations") or blob.get("proposed_relations") or []
    out: list[dict[str, Any]] = []
    for r in raw or []:
        if not isinstance(r, dict):
            continue
        sid = str(r.get("srcId") or r.get("src_id") or "").strip()
        did = str(r.get("dstId") or r.get("dst_id") or "").strip()
        rel = str(r.get("relation") or "").strip()
        if not sid or not did or not rel:
            continue
        out.append(
            {
                "srcId": _namespace_entity_id(origin, sid),
                "dstId": _namespace_entity_id(origin, did),
                "relation": rel,
                "attributes": decode_wire_relation_attributes(r.get("attributes")),
                "sourceClaimIds": list(r.get("sourceClaimIds") or r.get("source_claim_ids") or []),
                "_origin": origin,
            }
        )
    return out


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

    entity_by_ns: dict[str, dict[str, str]] = {}

    def _merge_entities(rows: list[dict[str, str]]) -> None:
        for row in rows:
            ns = row.get("namespacedId") or ""
            if not ns:
                continue
            prev = entity_by_ns.get(ns)
            if prev is None or len(row.get("label") or "") > len(prev.get("label") or ""):
                entity_by_ns[ns] = row

    for _key, blob, origin in (
        ("analyzer", analyzer_output, "analyzer"),
        ("governance", governance_output, "governance"),
    ):
        if isinstance(blob, dict) and blob.get("chainSummary"):
            ws.chain_summaries.append(str(blob["chainSummary"]))
        elif isinstance(blob, dict) and blob.get("chain_summary"):
            ws.chain_summaries.append(str(blob["chain_summary"]))
        _merge_entities(_collect_proposed_entities(blob, origin))
        ws.analyzer_relations.extend(_collect_relations(blob, origin))

    # Pack-level merge (supervisor already namespaced ids in proposedEntities / proposedRelations).
    pack_entities = (pack or {}).get("proposedEntities") or (pack or {}).get("proposed_entities") or []
    if isinstance(pack_entities, list):
        for row in pack_entities:
            if not isinstance(row, dict):
                continue
            ns = str(row.get("namespacedId") or row.get("namespaced_id") or "").strip()
            if not ns:
                continue
            label = str(row.get("label") or "").strip()
            origin = str(row.get("origin") or "pack").strip() or "pack"
            _merge_entities([{"namespacedId": ns, "label": label, "origin": origin}])

    pack_rels = (pack or {}).get("proposedRelations") or (pack or {}).get("proposed_relations") or []
    if isinstance(pack_rels, list):
        for r in pack_rels:
            if not isinstance(r, dict):
                continue
            sid = str(r.get("srcId") or r.get("src_id") or "").strip()
            did = str(r.get("dstId") or r.get("dst_id") or "").strip()
            rel = str(r.get("relation") or "").strip()
            if not sid or not did or not rel:
                continue
            ws.analyzer_relations.append(
                {
                    "srcId": sid,
                    "dstId": did,
                    "relation": rel,
                    "attributes": decode_wire_relation_attributes(r.get("attributes")),
                    "sourceClaimIds": list(r.get("sourceClaimIds") or r.get("source_claim_ids") or []),
                    "_origin": "pack",
                }
            )

    reg_ids = {e["namespacedId"] for e in entity_by_ns.values()}
    ws.analyzer_entity_label_by_ns = {e["namespacedId"]: e.get("label") or "" for e in entity_by_ns.values()}

    ws.input_issues = validate_phase0_input(ws, analyzer_output, governance_output)

    # Validate relation endpoints; drop invalid rows with input_issues.
    valid_rels: list[dict[str, Any]] = []
    seen: set[tuple[str, str, str]] = set()
    for r in ws.analyzer_relations:
        sid = str(r.get("srcId") or "").strip()
        did = str(r.get("dstId") or "").strip()
        rel = str(r.get("relation") or "").strip().lower()
        if sid not in reg_ids or did not in reg_ids:
            ws.input_issues.append(
                ValidationIssue(
                    code="entity_id_not_in_registry",
                    message="proposedRelations endpoint not found in proposedEntities registry",
                    detail={"srcId": sid, "dstId": did, "relation": r.get("relation")},
                )
            )
            continue
        k = (sid.lower(), did.lower(), rel)
        if k in seen:
            continue
        seen.add(k)
        # Strip internal _origin before phase4
        row = {k2: v2 for k2, v2 in r.items() if not str(k2).startswith("_")}
        valid_rels.append(row)
    ws.analyzer_relations = valid_rels

    # Dedup entities list (stable order by namespacedId)
    ws.analyzer_entities = sorted(entity_by_ns.values(), key=lambda x: x.get("namespacedId") or "")

    wg: dict[str, Any] = {}
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
    return ws


def claims_as_prompt_block(nodes: list[ReasoningNode]) -> str:
    lines = []
    for n in nodes:
        lines.append(f"- id={n.id} kind={n.kind} text={n.text!r}")
    return "\n".join(lines) if lines else "(no claim nodes)"
