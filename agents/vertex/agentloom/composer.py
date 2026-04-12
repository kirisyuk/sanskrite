from __future__ import annotations

from collections import OrderedDict

from agentloom.contracts import (
    BranchState,
    ChainSummary,
    FinalChainSummary,
    GraphFragment,
    GraphObservation,
    LogicTraceEntry,
    RuntimeLogEntry,
)


def build_final_chain_summary(
    question: str,
    selected_roles: list[str],
    working_graph: list[GraphObservation],
    graph_fragments: list[GraphFragment],
    branches: list[BranchState],
    logic_trace: list[LogicTraceEntry],
    runtime_logs: list[RuntimeLogEntry],
    open_gap_texts: list[str] | None = None,
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

    chain_summaries = _build_chain_summaries(graph_fragments, working_graph)
    concise_answer = question
    if chain_summaries:
        concise_answer = f"{question} Recovered {len(chain_summaries)} graph-derived chain summary(s)."

    return FinalChainSummary(
        question=question,
        selectedRoles=selected_roles,
        conciseAnswer=concise_answer,
        graphObservations=working_graph,
        graphFragments=graph_fragments,
        branches=branches,
        chainSummaries=chain_summaries,
        unresolvedGaps=_dedupe_preserve_order(unresolved_gaps),
        openVerificationNeeds=_dedupe_preserve_order(open_verification_needs),
        sourceDocuments=list(source_index.keys()),
        logicTrace=logic_trace,
        runtimeLogs=runtime_logs,
    )


def _build_chain_summaries(
    graph_fragments: list[GraphFragment],
    working_graph: list[GraphObservation],
) -> list[ChainSummary]:
    if graph_fragments:
        summaries: list[ChainSummary] = []
        for fragment in graph_fragments[:10]:
            if not fragment.observations:
                continue
            steps = [f"{item.subject} --{item.relation}--> {item.object}" for item in fragment.observations[:8]]
            support_types = {item.support_type for item in fragment.observations}
            summaries.append(
                ChainSummary(
                    label=fragment.title,
                    steps=steps,
                    evidenceStatus=_evidence_status_for_support_types(support_types),
                    confidence=round(fragment.quality_score or _avg_confidence(fragment.observations), 3),
                )
            )
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
