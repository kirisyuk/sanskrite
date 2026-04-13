from __future__ import annotations

import json
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError, model_validator


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)


class CorpusDocument(StrictModel):
    document_id: str = Field(alias="documentId")
    title: str
    text: str
    source_uri: str = Field(default="", alias="sourceUri")
    source_type: str = Field(default="corpus", alias="sourceType")
    tags: list[str] = Field(default_factory=list)
    metadata: dict[str, Any] = Field(default_factory=dict)


InvestigationRole = Literal["supply_chain", "governance_and_control", "policy_impact"]
SpecializedInvestigatorAgentName = Literal[
    "supply_chain_investigator_agent",
    "governance_and_control_investigator_agent",
    "policy_impact_investigator_agent",
]
DelegatedAgentName = Literal[
    "supply_chain_investigator_agent",
    "governance_and_control_investigator_agent",
    "policy_impact_investigator_agent",
    "gap_bridge_agent",
    "critic_agent",
]


class AgentLoomRequest(StrictModel):
    question: str
    corpus_documents: list[CorpusDocument] = Field(default_factory=list, alias="corpusDocuments")
    requested_roles: list[InvestigationRole] = Field(default_factory=list, alias="requestedRoles")

    @classmethod
    def from_json_text(cls, text: str) -> "AgentLoomRequest":
        try:
            payload = json.loads(text)
        except json.JSONDecodeError as exc:
            raise ValueError(f"request must be valid JSON: {exc}") from exc
        try:
            return cls.model_validate(payload)
        except ValidationError as exc:
            raise ValueError(f"request does not match AgentLoomRequest: {exc}") from exc


class GraphObservation(StrictModel):
    observation_id: str = Field(alias="observationId")
    subject_id: str = Field(default="", alias="subjectId")
    subject: str
    subject_type: str = Field(default="", alias="subjectType")
    relation: str
    object_id: str = Field(default="", alias="objectId")
    object: str
    object_type: str = Field(default="", alias="objectType")
    qualifiers: list[str] = Field(default_factory=list)
    confidence: float
    support_type: Literal["document", "model_knowledge", "mixed", "hypothesis"] = Field(alias="supportType")
    provenance: list[str] = Field(default_factory=list)
    support_note: str = Field(default="", alias="supportNote")
    branch_id: str = Field(default="", alias="branchId")
    alternative_group_id: str = Field(default="", alias="alternativeGroupId")


class GraphFragment(StrictModel):
    fragment_id: str = Field(alias="fragmentId")
    fragment_type: Literal[
        "overview_chain",
        "chain_fragment",
        "alternative_chain",
        "ownership_overlay",
        "policy_overlay",
        "hypothesis",
    ] = Field(alias="fragmentType")
    title: str
    human_question: str = Field(alias="humanQuestion")
    human_summary: str = Field(alias="humanSummary")
    branch_id: str = Field(alias="branchId")
    alternative_group_id: str = Field(default="", alias="alternativeGroupId")
    start_node_id: str = Field(default="", alias="startNodeId")
    end_node_id: str = Field(default="", alias="endNodeId")
    patch_mode: Literal["append", "replace_node", "replace_fragment"] = Field(default="append", alias="patchMode")
    old_id: str = Field(default="", alias="oldId")
    old_ids: list[str] = Field(default_factory=list, alias="oldIds")
    patch_rationale: str = Field(default="", alias="patchRationale")
    observations: list[GraphObservation] = Field(default_factory=list)
    completeness: Literal["partial", "complete", "hypothesis"] = "partial"
    quality_score: float = Field(default=0.0, alias="qualityScore")

    @model_validator(mode="after")
    def validate_patch_contract(self) -> "GraphFragment":
        if self.patch_mode == "append":
            return self
        if not self.old_id and not self.old_ids:
            raise ValueError("replace_node/replace_fragment requires oldId or oldIds")
        if not self.patch_rationale:
            raise ValueError("replace_node/replace_fragment requires patchRationale")
        return self


class LearnedFinding(StrictModel):
    finding_id: str = Field(alias="findingId")
    hypothesis_id: str = Field(alias="hypothesisId")
    branch_id: str = Field(alias="branchId")
    role: InvestigationRole
    question_asked: str = Field(alias="questionAsked")
    finding_kind: Literal[
        "operational_flow",
        "contractual_procurement",
        "governance_overlay",
        "policy_overlay",
        "negative_result",
        "context",
    ] = Field(alias="findingKind")
    summary: str
    evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = Field(alias="evidenceStatus")
    confidence: float
    related_entities: list[str] = Field(default_factory=list, alias="relatedEntities")
    source_documents: list[str] = Field(default_factory=list, alias="sourceDocuments")


class FailureRecord(StrictModel):
    code: Literal[
        "missing_question",
        "missing_corpus",
        "missing_graph",
        "blocked_by_capability",
        "agent_failed",
    ]
    message: str
    blocking: bool
    missing_inputs: list[str] = Field(default_factory=list, alias="missingInputs")


class RoleContract(StrictModel):
    role: InvestigationRole
    domain_mode: str = Field(alias="domainMode")
    contract_types: list[str] = Field(default_factory=list, alias="contractTypes")
    description: str
    expected_entity_types: list[str] = Field(default_factory=list, alias="expectedEntityTypes")
    actor_entity_types: list[str] = Field(default_factory=list, alias="actorEntityTypes")
    allowed_edge_types: list[str] = Field(default_factory=list, alias="allowedEdgeTypes")
    path_carried_edges: list[str] = Field(default_factory=list, alias="pathCarriedEdges")
    overlay_edges: list[str] = Field(default_factory=list, alias="overlayEdges")
    missing_info_prompts: list[str] = Field(default_factory=list, alias="missingInfoPrompts")
    investigation_playbook: list[str] = Field(default_factory=list, alias="investigationPlaybook")
    tool_use_guidance: dict[str, str] = Field(default_factory=dict, alias="toolUseGuidance")
    structural_closure_criteria: list[str] = Field(default_factory=list, alias="structuralClosureCriteria")


class RoleSelection(StrictModel):
    selected_roles: list[InvestigationRole] = Field(alias="selectedRoles")
    rationale: list[str]


class InvestigationMission(StrictModel):
    mission_id: str = Field(alias="missionId")
    objective: str
    focus: list[str] = Field(default_factory=list)
    assigned_roles: list[InvestigationRole] = Field(default_factory=list, alias="assignedRoles")
    grounding_mode: Literal["knowledge_only", "mixed", "corpus_required"] = Field(alias="groundingMode")
    success_criteria: list[str] = Field(default_factory=list, alias="successCriteria")


class BranchPlan(StrictModel):
    branch_id: str = Field(alias="branchId")
    hypothesis: str
    parent_hypothesis_id: str = Field(default="", alias="parentHypothesisId")
    hypothesis_relation: Literal["root", "dependent_followup", "independent_child"] = Field(
        default="root",
        alias="hypothesisRelation",
    )
    spawn_reason: str = Field(default="", alias="spawnReason")
    independence_basis: str = Field(default="", alias="independenceBasis")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(
        default="overview",
        alias="pathKind",
    )
    human_question: str = Field(alias="humanQuestion")
    human_action: str = Field(alias="humanAction")
    closure_target: str = Field(alias="closureTarget")
    known_context: list[str] = Field(default_factory=list, alias="knownContext")
    frontier: list[str] = Field(default_factory=list)
    assigned_roles: list[InvestigationRole] = Field(default_factory=list, alias="assignedRoles")
    preferred_tools: list[
        Literal[
            "retrieve",
            "reason",
            "web_search",
        ]
    ] = Field(default_factory=list, alias="preferredTools")
    target_entities: list[str] = Field(default_factory=list, alias="targetEntities")
    fragment_type: Literal[
        "overview_chain",
        "chain_fragment",
        "alternative_chain",
        "ownership_overlay",
        "policy_overlay",
        "hypothesis",
    ] = Field(alias="fragmentType")
    parallel_group: str = Field(default="", alias="parallelGroup")
    stop_after_no_improvement: int = Field(default=2, alias="stopAfterNoImprovement")


class ToolAction(StrictModel):
    action_id: str = Field(alias="actionId")
    branch_id: str = Field(alias="branchId")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(
        default="overview",
        alias="pathKind",
    )
    tool_name: Literal[
        "retrieve",
        "reason",
        "web_search",
    ] = Field(alias="toolName")
    human_question: str = Field(alias="humanQuestion")
    human_action: str = Field(alias="humanAction")
    why_this_question: str = Field(alias="whyThisQuestion")
    closure_target: str = Field(alias="closureTarget")
    known_context: list[str] = Field(default_factory=list, alias="knownContext")
    frontier: list[str] = Field(default_factory=list)
    query_text: str = Field(alias="queryText")
    assigned_roles: list[InvestigationRole] = Field(default_factory=list, alias="assignedRoles")
    target_entities: list[str] = Field(default_factory=list, alias="targetEntities")
    expected_fragment_type: str = Field(alias="expectedFragmentType")


class ToolActionResult(StrictModel):
    action_id: str = Field(alias="actionId")
    branch_id: str = Field(alias="branchId")
    tool_name: str = Field(alias="toolName")
    path_assessment: Literal["confirmed", "best_supported_indirect", "rejected", "still_open", "not_applicable"] = (
        Field(default="still_open", alias="pathAssessment")
    )
    path_assessment_note: str = Field(default="", alias="pathAssessmentNote")
    status: Literal["success", "partial", "blocked", "failed"]
    human_summary: str = Field(alias="humanSummary")
    closure_status: Literal["advanced", "closed", "unchanged", "redirected"] = Field(alias="closureStatus")
    resolved_frontier: list[str] = Field(default_factory=list, alias="resolvedFrontier")
    remaining_frontier: list[str] = Field(default_factory=list, alias="remainingFrontier")
    discovered_frontier: list[str] = Field(default_factory=list, alias="discoveredFrontier")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")
    learned_findings: list[LearnedFinding] = Field(default_factory=list, alias="learnedFindings")
    uncovered_questions: list[str] = Field(default_factory=list, alias="uncoveredQuestions")
    source_documents: list[str] = Field(default_factory=list, alias="sourceDocuments")
    failure: FailureRecord | None = None


class BranchState(StrictModel):
    branch_id: str = Field(alias="branchId")
    hypothesis: str
    parent_hypothesis_id: str = Field(default="", alias="parentHypothesisId")
    hypothesis_relation: Literal["root", "dependent_followup", "independent_child"] = Field(
        default="root",
        alias="hypothesisRelation",
    )
    spawn_reason: str = Field(default="", alias="spawnReason")
    independence_basis: str = Field(default="", alias="independenceBasis")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(
        default="overview",
        alias="pathKind",
    )
    path_assessment: Literal["confirmed", "best_supported_indirect", "rejected", "still_open", "not_applicable"] = (
        Field(default="still_open", alias="pathAssessment")
    )
    path_assessment_note: str = Field(default="", alias="pathAssessmentNote")
    human_question: str = Field(alias="humanQuestion")
    human_summary: str = Field(default="", alias="humanSummary")
    closure_target: str = Field(alias="closureTarget")
    known_context: list[str] = Field(default_factory=list, alias="knownContext")
    assigned_roles: list[InvestigationRole] = Field(default_factory=list, alias="assignedRoles")
    status: Literal["active", "needs_followup", "blocked", "completed"] = "active"
    quality_score: float = Field(default=0.0, alias="qualityScore")
    stagnation_count: int = Field(default=0, alias="stagnationCount")
    stop_reason: str = Field(default="", alias="stopReason")
    resolved_frontier: list[str] = Field(default_factory=list, alias="resolvedFrontier")
    remaining_frontier: list[str] = Field(default_factory=list, alias="remainingFrontier")
    discovered_frontier: list[str] = Field(default_factory=list, alias="discoveredFrontier")
    open_questions: list[str] = Field(default_factory=list, alias="openQuestions")
    tool_actions: list[ToolAction] = Field(default_factory=list, alias="toolActions")
    tool_results: list[ToolActionResult] = Field(default_factory=list, alias="toolResults")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")
    learned_findings: list[LearnedFinding] = Field(default_factory=list, alias="learnedFindings")
    hypothesis_turns: list["HypothesisTurn"] = Field(default_factory=list, alias="hypothesisTurns")


class HypothesisTurnPlan(StrictModel):
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(
        default="overview",
        alias="pathKind",
    )
    tool_name: Literal["retrieve", "reason", "web_search"] = Field(alias="toolName")
    human_question: str = Field(alias="humanQuestion")
    human_action: str = Field(alias="humanAction")
    why_now: str = Field(alias="whyNow")
    closure_target: str = Field(alias="closureTarget")
    known_context: list[str] = Field(default_factory=list, alias="knownContext")
    frontier: list[str] = Field(default_factory=list)
    target_entities: list[str] = Field(default_factory=list, alias="targetEntities")
    fragment_type: Literal[
        "overview_chain",
        "chain_fragment",
        "alternative_chain",
        "ownership_overlay",
        "policy_overlay",
        "hypothesis",
    ] = Field(alias="fragmentType")


class HypothesisTurn(StrictModel):
    turn_id: str = Field(alias="turnId")
    hypothesis_id: str = Field(alias="hypothesisId")
    turn_index: int = Field(alias="turnIndex")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(alias="pathKind")
    tool_name: Literal["retrieve", "reason", "web_search"] = Field(alias="toolName")
    human_question: str = Field(alias="humanQuestion")
    human_action: str = Field(alias="humanAction")
    why_now: str = Field(alias="whyNow")
    result_summary: str = Field(alias="resultSummary")
    path_assessment: Literal["confirmed", "best_supported_indirect", "rejected", "still_open", "not_applicable"] = (
        Field(alias="pathAssessment")
    )
    closure_status: Literal["advanced", "closed", "unchanged", "redirected"] = Field(alias="closureStatus")
    graph_patch_modes: list[Literal["append", "replace_node", "replace_fragment"]] = Field(
        default_factory=list,
        alias="graphPatchModes",
    )
    graph_fragment_ids: list[str] = Field(default_factory=list, alias="graphFragmentIds")
    learned_finding_ids: list[str] = Field(default_factory=list, alias="learnedFindingIds")
    resolved_frontier: list[str] = Field(default_factory=list, alias="resolvedFrontier")
    remaining_frontier: list[str] = Field(default_factory=list, alias="remainingFrontier")
    discovered_frontier: list[str] = Field(default_factory=list, alias="discoveredFrontier")
    decision_after_result: str = Field(default="", alias="decisionAfterResult")


class HypothesisTurnDecision(StrictModel):
    assessment: str
    rationale: list[str]
    decision: Literal[
        "ask_next_question_same_hypothesis",
        "run_parallel_followups",
        "spawn_independent_child_hypothesis",
        "accept_hypothesis",
        "reject_hypothesis",
        "stall_hypothesis",
    ]
    decision_note: str = Field(alias="decisionNote")
    hypothesis_status: Literal["active", "accepted", "rejected", "stalled", "deferred"] = Field(
        default="active",
        alias="hypothesisStatus",
    )
    next_turn: HypothesisTurnPlan | None = Field(default=None, alias="nextTurn")
    followup_branches: list[BranchPlan] = Field(default_factory=list, alias="followupBranches")

    @model_validator(mode="after")
    def validate_decision_contract(self) -> "HypothesisTurnDecision":
        if self.decision == "ask_next_question_same_hypothesis":
            if self.next_turn is None:
                raise ValueError("ask_next_question_same_hypothesis requires nextTurn")
            if self.followup_branches:
                raise ValueError("ask_next_question_same_hypothesis must not emit followupBranches")
        elif self.decision in {"run_parallel_followups", "spawn_independent_child_hypothesis"}:
            if not self.followup_branches:
                raise ValueError(f"{self.decision} requires followupBranches")
        else:
            if self.next_turn is not None:
                raise ValueError(f"{self.decision} must not emit nextTurn")
        return self


class CandidatePath(StrictModel):
    path_id: str = Field(alias="pathId")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(alias="pathKind")
    status: Literal["confirmed", "best_supported_indirect", "rejected", "still_open"] = "still_open"
    title: str
    summary: str
    start_node: str = Field(default="", alias="startNode")
    end_node: str = Field(default="", alias="endNode")
    via_nodes: list[str] = Field(default_factory=list, alias="viaNodes")
    supporting_branch_ids: list[str] = Field(default_factory=list, alias="supportingBranchIds")
    supporting_fragment_ids: list[str] = Field(default_factory=list, alias="supportingFragmentIds")
    evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = Field(alias="evidenceStatus")
    confidence: float
    unresolved_hops: list[str] = Field(default_factory=list, alias="unresolvedHops")


class HypothesisState(StrictModel):
    hypothesis_id: str = Field(alias="hypothesisId")
    statement: str
    parent_hypothesis_id: str = Field(default="", alias="parentHypothesisId")
    root_hypothesis_id: str = Field(default="", alias="rootHypothesisId")
    hypothesis_relation: Literal["root", "dependent_followup", "independent_child"] = Field(
        default="root",
        alias="hypothesisRelation",
    )
    spawn_reason: str = Field(default="", alias="spawnReason")
    independence_basis: str = Field(default="", alias="independenceBasis")
    path_kind: Literal["overview", "direct", "indirect", "alternative", "bridge"] = Field(alias="pathKind")
    status: Literal["active", "accepted", "rejected", "stalled", "deferred"] = "active"
    summary: str
    human_question: str = Field(alias="humanQuestion")
    closure_target: str = Field(alias="closureTarget")
    active_roles: list[InvestigationRole] = Field(default_factory=list, alias="activeRoles")
    source_branch_ids: list[str] = Field(default_factory=list, alias="sourceBranchIds")
    source_fragment_ids: list[str] = Field(default_factory=list, alias="sourceFragmentIds")
    broad_picture_fragment_ids: list[str] = Field(default_factory=list, alias="broadPictureFragmentIds")
    evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = Field(alias="evidenceStatus")
    confidence: float
    resolved_frontier: list[str] = Field(default_factory=list, alias="resolvedFrontier")
    remaining_frontier: list[str] = Field(default_factory=list, alias="remainingFrontier")
    discovered_frontier: list[str] = Field(default_factory=list, alias="discoveredFrontier")
    open_questions: list[str] = Field(default_factory=list, alias="openQuestions")
    finding_ids: list[str] = Field(default_factory=list, alias="findingIds")
    loop_count: int = Field(default=0, alias="loopCount")
    last_updated_cycle: int = Field(default=0, alias="lastUpdatedCycle")
    turn_ids: list[str] = Field(default_factory=list, alias="turnIds")


class AgentResult(StrictModel):
    agent_name: DelegatedAgentName = Field(alias="agentName")
    mission_id: str = Field(alias="missionId")
    status: Literal["success", "partial", "blocked", "failed"]
    summary: str
    active_roles: list[InvestigationRole] = Field(default_factory=list, alias="activeRoles")
    graph_delta: list[GraphObservation] = Field(default_factory=list, alias="graphDelta")
    answered_questions: list[str] = Field(default_factory=list, alias="answeredQuestions")
    gaps: list[str] = Field(default_factory=list)
    next_questions: list[str] = Field(default_factory=list, alias="nextQuestions")
    handoff_recommendations: list[str] = Field(default_factory=list, alias="handoffRecommendations")
    failure: FailureRecord | None = None


class ToolContract(StrictModel):
    name: str
    capability: str
    category: Literal["memory", "corpus", "grounding", "graph", "diagnostic"] = "memory"
    required_inputs: list[str] = Field(default_factory=list, alias="requiredInputs")
    output_shape: str = Field(alias="outputShape")
    failure_modes: list[str] = Field(default_factory=list, alias="failureModes")


class AgentContract(StrictModel):
    name: DelegatedAgentName
    role: str
    mission_contract: str = Field(alias="missionContract")
    result_contract: str = Field(alias="resultContract")
    shared_memory_reads: list[str] = Field(default_factory=list, alias="sharedMemoryReads")
    shared_memory_writes: list[str] = Field(default_factory=list, alias="sharedMemoryWrites")
    available_tools: list[str] = Field(default_factory=list, alias="availableTools")


class SupervisorDecision(StrictModel):
    """
    Control-plane decision for the next ADK step.

    **Why validation alone is not enough:** the LLM emits one flat JSON object; Gemini's structured
    output schema marks `mission` as optional at the JSON-schema level because `compose`/`stop` omit it.
    Models sometimes return `nextAgent=critic_agent` (or another delegate) with a null/empty `mission`.
    The runtime applies deterministic `_coerce_supervisor_decision_payload` in `agent.py` inside
    `StructuredOutputAgent._finalize_parsed_output` (before the first `model_validate`) and again
    when the supervisor reads state, so missing `mission` / `assignedRoles` from the LLM do not
    abort the ADK run when they can be recovered from `selectedRoles` and `nextAgent`.
    """

    cycle: int
    assessment: str
    rationale: list[str]
    next_agent: Literal[
        "supply_chain_investigator_agent",
        "governance_and_control_investigator_agent",
        "policy_impact_investigator_agent",
        "gap_bridge_agent",
        "critic_agent",
        "compose",
        "stop",
    ] = Field(alias="nextAgent")
    mission: InvestigationMission | None = None
    completion_rationale: str = Field(default="", alias="completionRationale")

    @model_validator(mode="after")
    def validate_control_contract(self) -> "SupervisorDecision":
        if self.next_agent in {"compose", "stop"}:
            return self
        if self.mission is None:
            raise ValueError(f"mission is required when nextAgent={self.next_agent}")
        if not self.mission.assigned_roles:
            raise ValueError(f"assignedRoles are required when nextAgent={self.next_agent}")
        return self


class LogicTraceEntry(StrictModel):
    cycle: int
    state_seen: dict[str, Any] = Field(alias="stateSeen")
    decision: SupervisorDecision
    mission: InvestigationMission | None = None
    agent_result: AgentResult | None = Field(default=None, alias="agentResult")
    resulting_state: dict[str, Any] = Field(alias="resultingState")
    next_step_logic: str = Field(alias="nextStepLogic")


class RuntimeLogEntry(StrictModel):
    sequence: int
    actor: str
    phase: Literal[
        "session_init",
        "role_selection",
        "llm_request",
        "llm_result",
        "decision",
        "delegation",
        "tool_call",
        "tool_result",
        "agent_result",
        "composition",
        "failure",
    ]
    message: str
    model: str = ""
    basis: dict[str, Any] = Field(default_factory=dict)
    request: dict[str, Any] = Field(default_factory=dict)
    result: dict[str, Any] = Field(default_factory=dict)


class GapCluster(StrictModel):
    """Grouped unresolved gaps for narrative synthesis (heuristic clustering)."""

    cluster_id: str = Field(alias="clusterId")
    label: str
    member_gap_texts: list[str] = Field(alias="memberGapTexts")
    severity: str = ""


class RubricItem(StrictModel):
    """One coverage slot derived from selected roles and/or question text."""

    rubric_id: str = Field(alias="rubricId")
    title: str
    source: Literal["from_role", "from_question_axis"]
    required: bool = True
    role: InvestigationRole | None = None


class QuestionRubric(StrictModel):
    items: list[RubricItem] = Field(default_factory=list)


class RubricLLMExtras(StrictModel):
    """Additional rubric slots proposed by LLM (merged with heuristic rubric, deduped by rubricId)."""

    additional_items: list[RubricItem] = Field(default_factory=list, alias="additionalItems")


NarrativeSynthesisStatus = Literal[
    "ok",
    "disabled",
    "deterministic_only",
    "validation_failed",
    "llm_invalid",
]


class InternalBrief(StrictModel):
    """Compressed, LLM-safe digest of investigation state for narrative composition."""

    question_one_liner: str = Field(alias="questionOneLiner")
    selected_roles: list[InvestigationRole] = Field(alias="selectedRoles")
    reachability_one_liner: str = Field(default="", alias="reachabilityOneLiner")
    complete_chain_count: int = Field(default=0, alias="completeChainCount")
    structural_complete_chain_count: int = Field(default=0, alias="structuralChainCount")
    bridge_candidates_short: list[str] = Field(default_factory=list, alias="bridgeCandidatesShort")
    supply_chain_narrative_bullets: list[str] = Field(default_factory=list, alias="supplyChainNarrativeBullets")
    governance_bullets: list[str] = Field(default_factory=list, alias="governanceBullets")
    policy_bullets: list[str] = Field(default_factory=list, alias="policyBullets")
    alternative_route_families: list[str] = Field(default_factory=list, alias="alternativeRouteFamilies")
    key_entities: list[str] = Field(default_factory=list, alias="keyEntities")
    clustered_gaps: list[GapCluster] = Field(default_factory=list, alias="clusteredGaps")
    learned_findings_digest: list[str] = Field(default_factory=list, alias="learnedFindingsDigest")
    epistemic_caveats: list[str] = Field(default_factory=list, alias="epistemicCaveats")


class StructuredNarrativeAnswer(StrictModel):
    """Sectioned user-facing answer; must align with QuestionRubric coverage rules."""

    chain_structure: str = Field(alias="chainStructure")
    alternative_paths: str = Field(alias="alternativePaths")
    ownership_leverage: str = Field(alias="ownershipLeverage")
    regulatory_choke_points: str = Field(alias="regulatoryChokePoints")
    honest_limits: str = Field(alias="honestLimits")
    prioritized_open_questions: list[str] = Field(default_factory=list, alias="prioritizedOpenQuestions")
    executive_summary: str = Field(alias="executiveSummary")


class ChainSummary(StrictModel):
    label: str
    steps: list[str]
    evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = Field(alias="evidenceStatus")
    confidence: float


class FinalChainSummary(StrictModel):
    """
    Terminal investigation artifact. `narrativeSynthesisStatus` + `narrativeSynthesisDetail` tell you
    whether `structuredNarrative` is real LLM output (`ok`), intentionally absent (`disabled` /
    `deterministic_only`), or an explicit failure diagnostic (`validation_failed` / `llm_invalid`).
    Prefer `structuredNarrative` for sectioned UI when status is `ok`. `fullNarrativeText` is populated
    only for successful synthesis (joined sections).
    """

    question: str
    selected_roles: list[InvestigationRole] = Field(alias="selectedRoles")
    concise_answer: str = Field(alias="conciseAnswer")
    narrative_synthesis_status: NarrativeSynthesisStatus = Field(
        default="deterministic_only",
        alias="narrativeSynthesisStatus",
    )
    narrative_synthesis_detail: str = Field(default="", alias="narrativeSynthesisDetail")
    internal_brief: InternalBrief | None = Field(default=None, alias="internalBrief")
    structured_narrative: StructuredNarrativeAnswer | None = Field(default=None, alias="structuredNarrative")
    full_narrative_text: str = Field(default="", alias="fullNarrativeText")
    hypotheses: list[HypothesisState] = Field(default_factory=list)
    candidate_paths: list[CandidatePath] = Field(default_factory=list, alias="candidatePaths")
    learned_findings: list[LearnedFinding] = Field(default_factory=list, alias="learnedFindings")
    graph_observations: list[GraphObservation] = Field(alias="graphObservations")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")
    branches: list[BranchState] = Field(default_factory=list)
    chain_summaries: list[ChainSummary] = Field(alias="chainSummaries")
    unresolved_gaps: list[str] = Field(alias="unresolvedGaps")
    open_verification_needs: list[str] = Field(alias="openVerificationNeeds")
    source_documents: list[str] = Field(alias="sourceDocuments")
    reachability_report: dict[str, Any] = Field(default_factory=dict, alias="reachabilityReport")
    logic_trace: list[LogicTraceEntry] = Field(alias="logicTrace")
    runtime_logs: list[RuntimeLogEntry] = Field(default_factory=list, alias="runtimeLogs")
