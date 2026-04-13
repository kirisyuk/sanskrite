from __future__ import annotations

from collections import OrderedDict

from agentloom.contracts import (
    BranchState,
    CandidatePath,
    ChainSummary,
    FinalChainSummary,
    GraphFragment,
    GraphObservation,
    HypothesisState,
    InternalBrief,
    LearnedFinding,
    LogicTraceEntry,
    NarrativeSynthesisStatus,
    RuntimeLogEntry,
    StructuredNarrativeAnswer,
)
from agentloom.internal_brief import build_internal_brief
from agentloom.narrative_coverage import full_narrative_text_from_answer


def _epistemic_lead_for_concise_answer(internal_brief: InternalBrief, executive_body: str) -> str:
    """
    When LLM narrative is used as the body, prepend reachability truth and caveats
    so conciseAnswer does not read more grounded than the graph.
    """
    body = (executive_body or "").strip()
    if not body:
        return ""
    body_lower = body.lower()
    chunks: list[str] = []
    if internal_brief.complete_chain_count == 0 or internal_brief.epistemic_caveats:
        rl = (internal_brief.reachability_one_liner or "").strip()
        if internal_brief.complete_chain_count == 0 and rl:
            short = rl if len(rl) <= 360 else rl[:357].rstrip() + "…"
            if short.lower() not in body_lower:
                chunks.append(short.rstrip(".") + ".")
        for raw in internal_brief.epistemic_caveats:
            c = raw.strip()
            if not c or len(c) < 10:
                continue
            head = c[: min(80, len(c))].lower()
            if head in body_lower or c.lower() in body_lower:
                continue
            chunks.append(c.rstrip(".") + ".")
    if not chunks:
        return body
    lead = " ".join(_dedupe_preserve_order(chunks)).strip()
    merged = f"{lead} {body}".strip()
    return merged[:4000]


def build_final_chain_summary(
    question: str,
    selected_roles: list[str],
    working_graph: list[GraphObservation],
    graph_fragments: list[GraphFragment],
    branches: list[BranchState],
    hypotheses: list[HypothesisState],
    candidate_paths: list[CandidatePath],
    learned_findings: list[LearnedFinding],
    logic_trace: list[LogicTraceEntry],
    runtime_logs: list[RuntimeLogEntry],
    open_gap_texts: list[str] | None = None,
    reachability_report: dict | None = None,
    *,
    structured_narrative: StructuredNarrativeAnswer | None = None,
    internal_brief: InternalBrief | None = None,
    narrative_synthesis_status: NarrativeSynthesisStatus = "deterministic_only",
    narrative_synthesis_detail: str = "",
) -> FinalChainSummary:
    source_index = OrderedDict()
    open_verification_needs: list[str] = []

    for observation in working_graph:
        for provenance in observation.provenance:
            source_index[provenance] = provenance
        if observation.support_type in {"model_knowledge", "hypothesis"} and observation.support_note:
            open_verification_needs.append(observation.support_note)

    for fragment in graph_fragments:
        for observation in fragment.observations:
            for provenance in observation.provenance:
                source_index[provenance] = provenance
            if observation.support_type in {"model_knowledge", "hypothesis"} and observation.support_note:
                open_verification_needs.append(observation.support_note)

    if open_gap_texts is not None:
        unresolved_gaps: list[str] = list(open_gap_texts)
    else:
        unresolved_gaps = []
        for entry in logic_trace:
            if entry.agent_result is None:
                continue
            unresolved_gaps.extend(entry.agent_result.gaps)

        for branch in branches:
            unresolved_gaps.extend(branch.open_questions)

    for entry in logic_trace:
        if entry.agent_result is not None and entry.agent_result.failure is not None:
            open_verification_needs.append(entry.agent_result.failure.message)

    for branch in branches:
        if branch.stop_reason:
            open_verification_needs.append(branch.stop_reason)

    unresolved_gaps = _dedupe_preserve_order(unresolved_gaps)

    if internal_brief is None:
        internal_brief = build_internal_brief(
            question,
            selected_roles,
            working_graph,
            graph_fragments,
            branches,
            hypotheses,
            candidate_paths,
            learned_findings,
            unresolved_gaps,
            reachability_report,
        )
    structured = structured_narrative
    caveats = list(internal_brief.epistemic_caveats)
    chain_summaries = _build_chain_summaries(graph_fragments, working_graph, candidate_paths)
    gap_n = len(internal_brief.clustered_gaps)

    full_narrative = ""
    if narrative_synthesis_status == "ok" and structured is not None:
        full_narrative = full_narrative_text_from_answer(structured)[:20000]
        exec_s = (structured.executive_summary or "").strip()
        if exec_s:
            concise_answer = _epistemic_lead_for_concise_answer(internal_brief, exec_s)
        else:
            concise_answer = _build_concise_answer_v2(
                question=question,
                candidate_paths=candidate_paths,
                hypotheses=hypotheses,
                unresolved_gaps=unresolved_gaps,
                learned_findings=learned_findings,
                gap_cluster_count=gap_n,
                epistemic_caveats=caveats,
                complete_chain_count=internal_brief.complete_chain_count,
            )
    elif narrative_synthesis_status in ("validation_failed", "llm_invalid") and structured is not None:
        concise_answer = (structured.executive_summary or "").strip()[:4000] or narrative_synthesis_detail
    else:
        concise_answer = _build_concise_answer_v2(
            question=question,
            candidate_paths=candidate_paths,
            hypotheses=hypotheses,
            unresolved_gaps=unresolved_gaps,
            learned_findings=learned_findings,
            gap_cluster_count=gap_n,
            epistemic_caveats=caveats,
            complete_chain_count=internal_brief.complete_chain_count,
        )

    detail = narrative_synthesis_detail
    if not detail and narrative_synthesis_status == "deterministic_only":
        detail = "No structured LLM narrative was requested or produced; conciseAnswer is deterministic glue only."

    return FinalChainSummary(
        question=question,
        selected_roles=selected_roles,
        concise_answer=concise_answer,
        narrative_synthesis_status=narrative_synthesis_status,
        narrative_synthesis_detail=detail,
        internal_brief=internal_brief,
        structured_narrative=structured,
        full_narrative_text=full_narrative,
        hypotheses=hypotheses,
        candidate_paths=candidate_paths,
        learned_findings=learned_findings,
        graph_observations=working_graph,
        graph_fragments=graph_fragments,
        branches=branches,
        chain_summaries=chain_summaries,
        unresolved_gaps=unresolved_gaps,
        open_verification_needs=_dedupe_preserve_order(open_verification_needs),
        source_documents=list(source_index.keys()),
        reachability_report=reachability_report or {},
        logic_trace=logic_trace,
        runtime_logs=runtime_logs,
    )


def build_final_chain_summary_sync(
    question: str,
    selected_roles: list[str],
    working_graph: list[GraphObservation],
    graph_fragments: list[GraphFragment],
    branches: list[BranchState],
    hypotheses: list[HypothesisState],
    candidate_paths: list[CandidatePath],
    learned_findings: list[LearnedFinding],
    logic_trace: list[LogicTraceEntry],
    runtime_logs: list[RuntimeLogEntry],
    open_gap_texts: list[str] | None = None,
    reachability_report: dict | None = None,
    *,
    internal_brief: InternalBrief | None = None,
) -> FinalChainSummary:
    """Deterministic final summary for tests/offline runs (no narrative LLM)."""
    return build_final_chain_summary(
        question,
        selected_roles,
        working_graph,
        graph_fragments,
        branches,
        hypotheses,
        candidate_paths,
        learned_findings,
        logic_trace,
        runtime_logs,
        open_gap_texts=open_gap_texts,
        reachability_report=reachability_report,
        structured_narrative=None,
        internal_brief=internal_brief,
        narrative_synthesis_status="deterministic_only",
        narrative_synthesis_detail="build_final_chain_summary_sync: explicit offline path without narrative LLM.",
    )


def _build_chain_summaries(
    graph_fragments: list[GraphFragment],
    working_graph: list[GraphObservation],
    candidate_paths: list[CandidatePath],
) -> list[ChainSummary]:
    path_by_fragment_id: dict[str, CandidatePath] = {}
    for path in candidate_paths:
        for fragment_id in path.supporting_fragment_ids:
            path_by_fragment_id[fragment_id] = path

    if graph_fragments:
        summaries: list[ChainSummary] = []
        seen_signatures: set[tuple[str, tuple[str, ...]]] = set()
        ranked_fragments = sorted(
            graph_fragments,
            key=lambda fragment: (
                {
                    "confirmed": 0,
                    "best_supported_indirect": 1,
                    "still_open": 2,
                    "rejected": 3,
                }.get(path_by_fragment_id.get(fragment.fragment_id).status if fragment.fragment_id in path_by_fragment_id else "still_open", 9),
                {
                    "alternative_chain": 0,
                    "chain_fragment": 1,
                    "overview_chain": 2,
                    "ownership_overlay": 3,
                    "policy_overlay": 4,
                    "hypothesis": 5,
                }.get(fragment.fragment_type, 9),
                -fragment.quality_score,
            ),
        )
        for fragment in ranked_fragments:
            signature = (fragment.title, tuple(item.observation_id for item in fragment.observations))
            if signature in seen_signatures:
                continue
            seen_signatures.add(signature)
            summaries.append(
                ChainSummary(
                    label=fragment.title,
                    steps=[f"{item.subject} --{item.relation}--> {item.object}" for item in fragment.observations[:8]],
                    evidenceStatus=_evidence_status_for_support_types({item.support_type for item in fragment.observations}),
                    confidence=round(sum(item.confidence for item in fragment.observations) / max(len(fragment.observations), 1), 3),
                )
            )
        if summaries:
            return summaries[:12]

    grouped: dict[str, list[GraphObservation]] = OrderedDict()
    for item in working_graph:
        grouped.setdefault(item.subject, []).append(item)

    summaries = []
    for subject, observations in list(grouped.items())[:10]:
        steps = [f"{item.subject} --{item.relation}--> {item.object}" for item in observations[:6]]
        support_types = {item.support_type for item in observations}
        summaries.append(
            ChainSummary(
                label=subject,
                steps=steps,
                evidenceStatus=_evidence_status_for_support_types(support_types),
                confidence=round(sum(item.confidence for item in observations) / len(observations), 3),
            )
        )
    return summaries


def _policy_dense_path(path: CandidatePath) -> bool:
    text = f"{path.title} {path.summary} {' '.join(path.unresolved_hops)}".lower()
    pol = sum(1 for k in ("regulation", "policy", "compliance", "due diligence", "traceability", "cbam", "permit") if k in text)
    sup = sum(1 for k in ("supply", "supplier", "route", "refiner", "refinery", "facility", "plant", "ore", "intermediate") if k in text)
    return pol >= 1 and sup == 0


def _policy_relevant_path(path: CandidatePath) -> bool:
    t = f"{path.title} {path.summary} {' '.join(path.unresolved_hops)}".lower()
    return any(
        k in t
        for k in (
            "regulation",
            "policy",
            "compliance",
            "due diligence",
            "law",
            "permit",
            "traceability",
            "carbon",
        )
    )


def _supply_like_path(path: CandidatePath) -> bool:
    if _policy_dense_path(path):
        return False
    text = f"{path.title} {path.summary} {' '.join(path.unresolved_hops)}".lower()
    return any(
        k in text
        for k in (
            "supply",
            "supplier",
            "route",
            "refiner",
            "refinery",
            "battery",
            "facility",
            "intermediate",
            "transform",
            "product",
            "plant",
            "ore",
        )
    )


def _finding_rank(f: LearnedFinding) -> tuple[int, float]:
    order = {"verified": 0, "mixed": 1, "reasoned": 2, "hypothesis": 3}
    return (order.get(f.evidence_status, 9), -f.confidence)


def _build_concise_answer_v2(
    question: str,
    candidate_paths: list[CandidatePath],
    hypotheses: list[HypothesisState],
    unresolved_gaps: list[str],
    learned_findings: list[LearnedFinding],
    gap_cluster_count: int,
    epistemic_caveats: list[str] | None = None,
    *,
    complete_chain_count: int | None = None,
) -> str:
    direct_rejected = next(
        (path for path in candidate_paths if path.path_kind == "direct" and path.status == "rejected"),
        None,
    )
    strongest_supply_indirect = next(
        (
            path
            for path in candidate_paths
            if path.path_kind == "indirect"
            and path.status in {"confirmed", "best_supported_indirect", "still_open"}
            and _supply_like_path(path)
        ),
        None,
    )
    strongest_policy = next(
        (
            path
            for path in candidate_paths
            if path.path_kind in ("indirect", "alternative", "overview", "bridge")
            and path.status in {"confirmed", "best_supported_indirect", "still_open"}
            and _policy_relevant_path(path)
        ),
        None,
    )
    strongest_overview = next((path for path in candidate_paths if path.path_kind == "overview"), None)
    sink_resolution = next(
        (
            path
            for path in candidate_paths
            if path.path_kind == "direct"
            and path.status == "still_open"
            and _supply_like_path(path)
            and (
                "sink" in path.title.lower()
                or "receiver" in path.summary.lower()
                or "facility" in path.summary.lower()
            )
        ),
        None,
    )
    alt_paths = [p for p in candidate_paths if p.path_kind == "alternative" and (p.summary or p.title)]

    governance_hypothesis = next(
        (
            h
            for h in hypotheses
            if h.status == "accepted"
            and (
                "governance_and_control" in h.active_roles
                or any(
                    k in (h.summary or h.statement).lower()
                    for k in ("ownership", "owns", "control", "parent", "subsidiary", "joint venture", "investor")
                )
            )
        ),
        None,
    )

    parts: list[str] = []
    if complete_chain_count == 0 and not (epistemic_caveats or []):
        parts.append("No grounded end-to-end operational chain is confirmed in the working graph.")
    if strongest_overview is not None:
        parts.append(strongest_overview.summary.rstrip(".") + ".")
    if alt_paths:
        parts.append(f"Alternative route considerations: {alt_paths[0].summary.rstrip('.')}.")
    if sink_resolution is not None:
        parts.append(f"Named downstream sink facilities are now identified: {sink_resolution.summary.rstrip('.')}.")
    if direct_rejected is not None:
        parts.append(f"Direct public route rejected: {direct_rejected.summary.rstrip('.')}.")
    if strongest_supply_indirect is not None:
        parts.append(f"Best supported indirect route: {strongest_supply_indirect.summary.rstrip('.')}.")
    elif direct_rejected is not None and sink_resolution is not None:
        parts.append(
            "Best supported route remains indirect: named downstream receiving facilities are visible, but the cross-regional bridge from upstream producers to those receivers stays partially opaque in public data."
        )
    if governance_hypothesis is not None:
        parts.append(f"Ownership and control overlay: {governance_hypothesis.summary.rstrip('.')}.")
    if strongest_policy is not None and strongest_policy is not strongest_supply_indirect:
        parts.append(f"Regulatory and policy angle: {strongest_policy.summary.rstrip('.')}.")

    for caveat in (epistemic_caveats or [])[:5]:
        c = caveat.strip()
        if c:
            parts.append(c.rstrip(".") + ".")

    ranked_findings = sorted(learned_findings, key=_finding_rank)
    if ranked_findings:
        digest_bits = [f.summary.rstrip(".") + "." for f in ranked_findings[:2] if f.summary.strip()]
        if digest_bits:
            parts.append("Cross-cutting findings: " + " ".join(digest_bits))

    if gap_cluster_count > 0:
        parts.append(f"Unresolved gap themes (clustered): {gap_cluster_count}.")
    elif unresolved_gaps:
        parts.append(f"Remaining unresolved structural gaps: {len(unresolved_gaps)}.")

    if not parts:
        return question
    body = " ".join(parts)
    no_chain_line = "No grounded end-to-end operational chain is confirmed in the working graph."
    if (
        complete_chain_count == 0
        and not (epistemic_caveats or [])
        and body == no_chain_line
        and question.strip()
        and question.strip() not in body
    ):
        return f"{body} {question.strip()}".strip()
    return body


def _dedupe_preserve_order(values: list[str]) -> list[str]:
    seen: set[str] = set()
    result: list[str] = []
    for value in values:
        if not value or value in seen:
            continue
        seen.add(value)
        result.append(value)
    return result


def _evidence_status_for_support_types(support_types: set[str]) -> str:
    if not support_types:
        return "reasoned"
    if support_types == {"document"}:
        return "verified"
    if support_types == {"hypothesis"}:
        return "hypothesis"
    if "document" in support_types and len(support_types) > 1:
        return "mixed"
    return "reasoned"
