from __future__ import annotations

import json
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError


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
    observations: list[GraphObservation] = Field(default_factory=list)
    completeness: Literal["partial", "complete", "hypothesis"] = "partial"
    quality_score: float = Field(default=0.0, alias="qualityScore")


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
    human_question: str = Field(alias="humanQuestion")
    human_action: str = Field(alias="humanAction")
    closure_target: str = Field(alias="closureTarget")
    known_context: list[str] = Field(default_factory=list, alias="knownContext")
    frontier: list[str] = Field(default_factory=list)
    assigned_roles: list[InvestigationRole] = Field(default_factory=list, alias="assignedRoles")
    preferred_tools: list[
        Literal[
            "memory_question_to_graph",
            "model_question_to_graph",
            "corpus_question_to_graph",
            "vertex_question_to_graph",
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
    tool_name: Literal[
        "memory_question_to_graph",
        "model_question_to_graph",
        "corpus_question_to_graph",
        "vertex_question_to_graph",
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
    status: Literal["success", "partial", "blocked", "failed"]
    human_summary: str = Field(alias="humanSummary")
    closure_status: Literal["advanced", "closed", "unchanged", "redirected"] = Field(alias="closureStatus")
    resolved_frontier: list[str] = Field(default_factory=list, alias="resolvedFrontier")
    remaining_frontier: list[str] = Field(default_factory=list, alias="remainingFrontier")
    discovered_frontier: list[str] = Field(default_factory=list, alias="discoveredFrontier")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")
    uncovered_questions: list[str] = Field(default_factory=list, alias="uncoveredQuestions")
    source_documents: list[str] = Field(default_factory=list, alias="sourceDocuments")
    failure: FailureRecord | None = None


class BranchState(StrictModel):
    branch_id: str = Field(alias="branchId")
    hypothesis: str
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
    open_questions: list[str] = Field(default_factory=list, alias="openQuestions")
    tool_actions: list[ToolAction] = Field(default_factory=list, alias="toolActions")
    tool_results: list[ToolActionResult] = Field(default_factory=list, alias="toolResults")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")


class AgentResult(StrictModel):
    agent_name: Literal["investigator_agent", "gap_bridge_agent", "critic_agent"] = Field(alias="agentName")
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
    name: Literal["investigator_agent", "gap_bridge_agent", "critic_agent"]
    role: str
    mission_contract: str = Field(alias="missionContract")
    result_contract: str = Field(alias="resultContract")
    shared_memory_reads: list[str] = Field(default_factory=list, alias="sharedMemoryReads")
    shared_memory_writes: list[str] = Field(default_factory=list, alias="sharedMemoryWrites")
    available_tools: list[str] = Field(default_factory=list, alias="availableTools")


class SupervisorDecision(StrictModel):
    cycle: int
    assessment: str
    rationale: list[str]
    next_agent: Literal["investigator_agent", "gap_bridge_agent", "critic_agent", "compose", "stop"] = Field(alias="nextAgent")
    mission: InvestigationMission | None = None
    completion_rationale: str = Field(default="", alias="completionRationale")


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


class ChainSummary(StrictModel):
    label: str
    steps: list[str]
    evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = Field(alias="evidenceStatus")
    confidence: float


class FinalChainSummary(StrictModel):
    question: str
    selected_roles: list[InvestigationRole] = Field(alias="selectedRoles")
    concise_answer: str = Field(alias="conciseAnswer")
    graph_observations: list[GraphObservation] = Field(alias="graphObservations")
    graph_fragments: list[GraphFragment] = Field(default_factory=list, alias="graphFragments")
    branches: list[BranchState] = Field(default_factory=list)
    chain_summaries: list[ChainSummary] = Field(alias="chainSummaries")
    unresolved_gaps: list[str] = Field(alias="unresolvedGaps")
    open_verification_needs: list[str] = Field(alias="openVerificationNeeds")
    source_documents: list[str] = Field(alias="sourceDocuments")
    logic_trace: list[LogicTraceEntry] = Field(alias="logicTrace")
    runtime_logs: list[RuntimeLogEntry] = Field(default_factory=list, alias="runtimeLogs")
