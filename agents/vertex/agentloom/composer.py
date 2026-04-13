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
    LearnedFinding,
    LogicTraceEntry,
    RuntimeLogEntry,
)


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

    chain_summaries = _build_chain_summaries(graph_fragments, working_graph, candidate_paths)
    concise_answer = _build_concise_answer(
        question=question,
        candidate_paths=candidate_paths,
        hypotheses=hypotheses,
        unresolved_gaps=unresolved_gaps,
    )

    return FinalChainSummary(
        question=question,
        selectedRoles=selected_roles,
        conciseAnswer=concise_answer,
        hypotheses=hypotheses,
        candidatePaths=candidate_paths,
        learnedFindings=learned_findings,
        graphObservations=working_graph,
        graphFragments=graph_fragments,
        branches=branches,
        chainSummaries=chain_summaries,
        unresolvedGaps=_dedupe_preserve_order(unresolved_gaps),
        openVerificationNeeds=_dedupe_preserve_order(open_verification_needs),
        sourceDocuments=list(source_index.keys()),
        reachabilityReport=reachability_report or {},
        logicTrace=logic_trace,
        runtimeLogs=runtime_logs,
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
                    "ownership_overlay": 2,
                    "policy_overlay": 3,
                    "overview_chain": 4,
                    "hypothesis": 5,
                }.get(fragment.fragment_type, 9),
                -float(fragment.quality_score or 0.0),
                -len(fragment.observations),
            ),
        )
        for fragment in ranked_fragments:
            if not fragment.observations:
                continue
            steps = [f"{item.subject} --{item.relation}--> {item.object}" for item in fragment.observations[:8]]
            signature = (fragment.title, tuple(steps))
            if signature in seen_signatures:
                continue
            seen_signatures.add(signature)
            support_types = {item.support_type for item in fragment.observations}
            summaries.append(
                ChainSummary(
                    label=fragment.title,
                    steps=steps,
                    evidenceStatus=_evidence_status_for_support_types(support_types),
                    confidence=round(fragment.quality_score or _avg_confidence(fragment.observations), 3),
                )
            )
            if len(summaries) >= 10:
                break
        if summaries:
            return summaries
    if not working_graph:
        return []

    grouped: OrderedDict[str, list[GraphObservation]] = OrderedDict()
    for observation in working_graph:
        grouped.setdefault(observation.subject, []).append(observation)

    summaries: list[ChainSummary] = []
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


def _build_concise_answer(
    question: str,
    candidate_paths: list[CandidatePath],
    hypotheses: list[HypothesisState],
    unresolved_gaps: list[str],
) -> str:
    def _looks_like_supply_route(path: CandidatePath) -> bool:
        text = f"{path.title} {path.summary} {' '.join(path.unresolved_hops)}".lower()
        if any(token in text for token in ("ownership", "owns", "control", "regulation", "policy")):
            return False
        return any(
            token in text
            for token in (
                "supply",
                "supplier",
                "route",
                "refiner",
                "refinery",
                "nickel sulfate",
                "battery",
                "facility",
                "intermediate",
            )
        )

    direct_rejected = next(
        (path for path in candidate_paths if path.path_kind == "direct" and path.status == "rejected"),
        None,
    )
    strongest_indirect = next(
        (
            path
            for path in candidate_paths
            if path.path_kind == "indirect"
            and path.status in {"confirmed", "best_supported_indirect", "still_open"}
            and _looks_like_supply_route(path)
        ),
        None,
    )
    strongest_overview = next(
        (path for path in candidate_paths if path.path_kind == "overview"),
        None,
    )
    sink_resolution = next(
        (
            path
            for path in candidate_paths
            if path.path_kind == "direct"
            and path.status == "still_open"
            and _looks_like_supply_route(path)
            and ("german" in path.title.lower() or "facility" in path.summary.lower())
        ),
        None,
    )
    accepted_ownership = next(
        (
            hypothesis
            for hypothesis in hypotheses
            if hypothesis.path_kind == "indirect" and hypothesis.status == "accepted"
        ),
        None,
    )
    parts: list[str] = []
    if strongest_overview is not None:
        parts.append(strongest_overview.summary.rstrip(".") + ".")
    if sink_resolution is not None:
        parts.append(f"Named German sink facilities are now identified: {sink_resolution.summary.rstrip('.')}.")
    if direct_rejected is not None:
        parts.append(f"Direct public route rejected: {direct_rejected.summary.rstrip('.')}.")
    if strongest_indirect is not None:
        parts.append(f"Best supported indirect route: {strongest_indirect.summary.rstrip('.')}.")
    elif direct_rejected is not None and sink_resolution is not None:
        parts.append(
            "Best supported route remains indirect: named German downstream facilities are visible, but the bridge from Indonesian producers to those facilities stays partially opaque in public data."
        )
    if accepted_ownership is not None:
        parts.append(f"Ownership and control overlay: {accepted_ownership.summary.rstrip('.')}.")
    if unresolved_gaps:
        parts.append(f"Remaining unresolved structural gaps: {len(unresolved_gaps)}.")
    if not parts:
        return question
    return " ".join(parts)


def _avg_confidence(observations: list[GraphObservation]) -> float:
    if not observations:
        return 0.0
    return sum(item.confidence for item in observations) / len(observations)


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
