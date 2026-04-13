from __future__ import annotations

# Optional Vertex ADK / GenAI SDKs are usually installed in the dedicated venv.
# pyright: reportMissingImports=false

import json
import os
import re
import asyncio
from functools import partial
from collections import deque
from collections.abc import AsyncGenerator
from typing import Any, Literal

from google.adk.agents import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event, EventActions
from google.genai import types as genai_types
import pydantic
from pydantic import BaseModel, ConfigDict, Field, PrivateAttr

from agentloom.composer import build_final_chain_summary
from agentloom.internal_brief import build_internal_brief
from agentloom.narrative_coverage import (
    build_repair_hint,
    explicit_narrative_synthesis_failure,
    roles_mentioned_in_narrative,
    validate_narrative_coverage,
)
from agentloom.question_rubric import build_question_rubric, merge_rubric_with_llm_extras
from agentloom.contracts import (
    AgentContract,
    AgentLoomRequest,
    AgentResult,
    BranchPlan,
    BranchState,
    CandidatePath,
    FailureRecord,
    GraphFragment,
    GraphObservation,
    HypothesisTurn,
    HypothesisTurnDecision,
    HypothesisTurnPlan,
    HypothesisState,
    InvestigationMission,
    InvestigationRole,
    LearnedFinding,
    LogicTraceEntry,
    RoleContract,
    RoleSelection,
    NarrativeSynthesisStatus,
    RubricLLMExtras,
    RuntimeLogEntry,
    StructuredNarrativeAnswer,
    SupervisorDecision,
    ToolAction,
    ToolActionResult,
    ToolContract,
)
from agentloom.taxonomy import (
    ROLE_CONTRACTS_BY_ROLE,
    ROLE_GOVERNANCE,
    ROLE_POLICY,
    ROLE_SUPPLY_CHAIN,
    SPECIALIZED_INVESTIGATOR_BY_ROLE,
)

APP_NAME = "agentloom"
USER_ID = "agentloom-runner"
SESSION_ID = "default"

STATE_REQUEST = "input_request"
STATE_SELECTED_ROLES = "selected_roles"
STATE_ROLE_SELECTION = "role_selection"
STATE_ROLE_CONTRACTS = "role_contracts"
STATE_TOOL_CONTRACTS = "tool_contracts"
STATE_AGENT_CONTRACTS = "agent_contracts"
STATE_CURRENT_MISSION = "current_mission"
STATE_WORKING_GRAPH = "working_graph"
STATE_GRAPH_FRAGMENTS = "graph_fragments"
STATE_REPLACED_NODE_IDS = "replaced_node_ids"
STATE_BRANCH_STATES = "branch_states"
STATE_OPEN_GAPS = "open_gaps"
STATE_ANSWERED_QUESTIONS = "answered_questions"
STATE_FAILURES = "failures"
STATE_AGENT_RESULT_HISTORY = "agent_result_history"
STATE_LAST_AGENT_RESULT = "last_agent_result"
STATE_HYPOTHESES = "hypotheses"
STATE_HYPOTHESIS_TURNS = "hypothesis_turns"
STATE_LEARNED_FINDINGS = "learned_findings"
STATE_PROGRESS_HISTORY = "progress_history"
STATE_GLOBAL_STAGNATION = "global_stagnation_count"
STATE_LOGIC_TRACE = "logic_trace"
STATE_RUNTIME_LOG = "runtime_logs"
STATE_SUPERVISOR_DECISION = "supervisor_decision"
STATE_SUPERVISOR_CYCLE = "supervisor_cycle"
STATE_FINAL = "final_chain_summary"
STATE_SUPERVISOR_CONTEXT = "supervisor_context"
STATE_INVESTIGATOR_CONTEXT = "investigator_context"
STATE_INVESTIGATOR_PLAN = "investigator_plan"
STATE_HYPOTHESIS_TURN_CONTEXT = "hypothesis_turn_context"
STATE_HYPOTHESIS_TURN_DECISION = "hypothesis_turn_decision"
STATE_INVESTIGATOR_EVIDENCE = "investigator_evidence"
STATE_GAP_BRIDGE_CONTEXT = "gap_bridge_context"
STATE_GAP_BRIDGE_RESULT = "gap_bridge_agent_result"
STATE_CRITIC_CONTEXT = "critic_context"
STATE_SUPPLY_CHAIN_INVESTIGATOR_RESULT = "supply_chain_investigator_agent_result"
STATE_GOVERNANCE_INVESTIGATOR_RESULT = "governance_and_control_investigator_agent_result"
STATE_POLICY_INVESTIGATOR_RESULT = "policy_impact_investigator_agent_result"
STATE_INVESTIGATOR_SYNTHESIS = "investigator_synthesis"
STATE_INVESTIGATOR_SYNTHESIS_CONTEXT = "investigator_synthesis_context"
STATE_CRITIC_RESULT = "critic_agent_result"
STATE_INTERNAL_BRIEF = "internal_brief"
STATE_NARRATIVE_CONTEXT = "narrative_composer_context"
STATE_STRUCTURED_NARRATIVE = "structured_narrative"
STATE_RUBRIC_LLM_CONTEXT = "rubric_llm_context"
STATE_RUBRIC_LLM_RESULT = "rubric_llm_extras"

RESULT_KEY_BY_AGENT = {
    "supply_chain_investigator_agent": STATE_SUPPLY_CHAIN_INVESTIGATOR_RESULT,
    "governance_and_control_investigator_agent": STATE_GOVERNANCE_INVESTIGATOR_RESULT,
    "policy_impact_investigator_agent": STATE_POLICY_INVESTIGATOR_RESULT,
    "gap_bridge_agent": STATE_GAP_BRIDGE_RESULT,
    "critic_agent": STATE_CRITIC_RESULT,
}

_MODEL_ALIASES = {
    "vertex-gemini-3-flash-preview": "gemini-3-flash-preview",
    "vertex-gemini-3.1-flash-lite-preview": "gemini-3.1-flash-lite-preview",
    "vertex-gemini-2.5-flash-lite": "gemini-2.5-flash-lite",
    "vertex-gemini-2.5-flash": "gemini-2.5-flash",
    "vertex-gemini-2.5-pro": "gemini-2.5-pro",
}

_TOKEN_RE = re.compile(r"[a-z0-9]{3,}", re.I)


class LocalModel(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)


class InvestigatorPlan(LocalModel):
    assessment: str
    rationale: list[str]
    branch_plans: list[BranchPlan] = Field(default_factory=list, alias="branchPlans")
    blocking_failure: FailureRecord | None = Field(default=None, alias="blockingFailure")


class GapEntry(LocalModel):
    gap_id: str = Field(alias="gapId")
    text: str
    status: Literal["open", "addressed", "resolved"] = "open"
    gap_kind: Literal["structural", "detail", "unknown"] = Field(default="unknown", alias="gapKind")
    origin: str = ""
    addressed_by: str = Field(default="", alias="addressedBy")
    cycle: int = 0


class InvestigatorSynthesis(LocalModel):
    cross_branch_assessment: str = Field(alias="crossBranchAssessment")
    connected_pairs: list[str] = Field(default_factory=list, alias="connectedPairs")
    disconnected_pairs: list[str] = Field(default_factory=list, alias="disconnectedPairs")
    bridge_gaps: list[str] = Field(default_factory=list, alias="bridgeGaps")
    bridge_questions: list[str] = Field(default_factory=list, alias="bridgeQuestions")
    redundant_overlaps: list[str] = Field(default_factory=list, alias="redundantOverlaps")
    addressed_gap_texts: list[str] = Field(default_factory=list, alias="addressedGapTexts")
    synthesis_summary: str = Field(alias="synthesisSummary")


_NEGATIVE_PATH_SIGNALS = re.compile(
    r"\b(no direct|no evidence|not found|not confirmed|no confirmed|no current|withdrew|withdrawn|cancelled|canceled|"
    r"no direct equity|no direct supply|not publicly available|could not establish|unable to verify)\b",
    re.IGNORECASE,
)
_INDIRECT_PATH_SIGNALS = re.compile(
    r"\b(indirect|intermediar|through|via|processed through|off-take|joint venture|midstream|trading hub|"
    r"third[- ]party)\b",
    re.IGNORECASE,
)
_PROCUREMENT_SIGNALS = re.compile(
    r"\b(procurement|procure|off-?take|contractual|long-term|long term|sourcing strategy|supplier base|"
    r"vertical integration|recycling|circular|locali[sz]ation|stake-held|stake held|supply agreement)\b",
    re.IGNORECASE,
)
_STABLE_POLICY_BACKGROUND_SIGNALS = re.compile(
    r"\b(traceability|due diligence|compliance framework|permitting|certification|import restriction|export restriction|"
    r"carbon accounting)\b",
    re.IGNORECASE,
)
_DETAIL_FRONTIER_SIGNALS = re.compile(
    r"\b(tonnage|volume|quantity|amount|pricing|price|cost|percentage|percent|share|stake|exact date|timeline|"
    r"contract terms?|pricing formula|margin|valuation)\b",
    re.IGNORECASE,
)
_PLACEHOLDER_LABEL_PATTERNS = [
    # Broad sink pattern: any leading token + "battery manufacturing plant/facility" (common LLM placeholder shape).
    re.compile(r"\b\w+\s+battery\s+manufacturing\s+(?:plant|facility)\b", re.IGNORECASE),
    re.compile(r"\b(?:sink-side|downstream)\s+battery(?:\s+manufacturing)?\s+(?:plant|facility)\b", re.IGNORECASE),
    re.compile(r"\b(?:upstream|source-side)\s+mining\s+facility\b", re.IGNORECASE),
    re.compile(r"\bregional\s+(?:mining|processing|refining)\s+(?:facility|plant|producer|operator)\b", re.IGNORECASE),
    re.compile(
        r"\b(?:domestic|regional|global|upstream|downstream)\s+"
        r"(?:mining|battery|processing|refining|manufacturing)\s+"
        r"(?:facility|plant|producer|manufacturer|refinery|operator)\b",
        re.IGNORECASE,
    ),
]


class _StateToolContext:
    def __init__(self, state: dict[str, Any]) -> None:
        self.state = state


def _truncate_text(value: str, limit: int = 320) -> str:
    text = value.strip()
    if len(text) <= limit:
        return text
    return text[: limit - 3] + "..."


def _json_safe(value: Any, depth: int = 0) -> Any:
    if depth > 4:
        return "<trimmed>"
    if value is None or isinstance(value, (bool, int, float)):
        return value
    if isinstance(value, str):
        return _truncate_text(value, 400)
    if isinstance(value, list):
        return [_json_safe(item, depth + 1) for item in value[:12]]
    if isinstance(value, dict):
        result: dict[str, Any] = {}
        for idx, (key, item) in enumerate(value.items()):
            if idx >= 24:
                result["..."] = "<trimmed>"
                break
            result[str(key)] = _json_safe(item, depth + 1)
        return result
    if hasattr(value, "model_dump"):
        return _json_safe(value.model_dump(mode="json", by_alias=True), depth + 1)
    return _truncate_text(str(value), 400)


class StructuredOutputAgent(BaseAgent):
    def __init__(
        self,
        *,
        name: str,
        model: str,
        instruction: str,
        output_schema: type[BaseModel],
        output_key: str,
        context_key: str,
        generate_content_config: genai_types.GenerateContentConfig,
        description: str = "",
    ) -> None:
        super().__init__(name=name, description=description)
        self._model = model
        self._instruction = instruction
        self._output_schema = output_schema
        self._output_key = output_key
        self._context_key = context_key
        self._generate_content_config = generate_content_config

    def _render_instruction(self, state: dict[str, Any]) -> str:
        context_payload = state.get(self._context_key, {}) or {}
        rendered_context = json.dumps(_json_safe(context_payload), ensure_ascii=False, indent=2)
        placeholder = "{" + self._context_key + "}"
        if placeholder in self._instruction:
            return self._instruction.replace(placeholder, rendered_context)
        return self._instruction

    def _response_config(self) -> genai_types.GenerateContentConfig:
        kwargs: dict[str, Any] = {
            "response_mime_type": "application/json",
            "response_schema": self._output_schema,
        }
        max_output_tokens = getattr(self._generate_content_config, "max_output_tokens", None)
        if max_output_tokens is not None:
            kwargs["max_output_tokens"] = max_output_tokens
        temperature = getattr(self._generate_content_config, "temperature", None)
        if temperature is not None:
            kwargs["temperature"] = temperature
        return genai_types.GenerateContentConfig(**kwargs)

    def _generate_with_retry(self, *, contents: str, config_obj: genai_types.GenerateContentConfig) -> Any:
        from google.genai import Client

        client = Client(
            vertexai=True,
            project=os.getenv("GOOGLE_CLOUD_PROJECT") or _vertex_grounding_config().get("project"),
            location=os.getenv("GOOGLE_CLOUD_LOCATION", "global") or _vertex_grounding_config().get("location"),
        )
        used_resource_fallback = False
        model = self._model
        while True:
            try:
                return client.models.generate_content(model=model, contents=contents, config=config_obj)
            except Exception as exc:
                if not _is_resource_exhausted_error(exc):
                    raise
                fallback_model = _model_with_resource_fallback(model, used_fallback=used_resource_fallback)
                if fallback_model == model:
                    raise
                used_resource_fallback = True
                model = fallback_model

    def _finalize_parsed_output(self, ctx: InvocationContext, payload: Any) -> BaseModel:
        """Apply schema-specific repair before strict Pydantic validation."""
        if self._output_schema is SupervisorDecision:
            if isinstance(payload, dict):
                d = dict(payload)
            elif isinstance(payload, BaseModel):
                d = payload.model_dump(mode="json", by_alias=True)
            else:
                d = {}
            request_obj = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
            d = _coerce_supervisor_decision_payload(
                d,
                selected_role_literals=list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
                cycle=int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0),
                user_question=request_obj.question,
            )
            return SupervisorDecision.model_validate(d)
        return self._output_schema.model_validate(payload)

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        prompt = self._render_instruction(ctx.session.state)
        max_attempts = 3
        last_exc: Exception | None = None
        result = None
        for attempt in range(max_attempts):
            try:
                response = self._generate_with_retry(contents=prompt, config_obj=self._response_config())
                parsed = getattr(response, "parsed", None)
                if parsed is not None:
                    result = self._finalize_parsed_output(ctx, parsed)
                else:
                    json_text = _extract_json_object(_response_json_text(response))
                    if self._output_schema is SupervisorDecision:
                        result = self._finalize_parsed_output(ctx, json.loads(json_text))
                    else:
                        result = self._output_schema.model_validate_json(json_text)
                break
            except (pydantic.ValidationError, json.JSONDecodeError, ValueError) as exc:
                last_exc = exc
                if attempt < max_attempts - 1:
                    continue
        if result is None:
            raise last_exc  # type: ignore[misc]
        payload = result.model_dump(mode="json", by_alias=True)
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={self._output_key: payload}),
            content=genai_types.Content(
                role="model",
                parts=[genai_types.Part(text=json.dumps(payload, ensure_ascii=False))],
            ),
        )


def _runtime_log_entry(
    state: dict[str, Any],
    *,
    actor: str,
    phase: str,
    message: str,
    model: str = "",
    basis: dict[str, Any] | None = None,
    request: dict[str, Any] | None = None,
    result: dict[str, Any] | None = None,
) -> RuntimeLogEntry:
    current = list(state.get(STATE_RUNTIME_LOG) or [])
    return RuntimeLogEntry(
        sequence=len(current) + 1,
        actor=actor,
        phase=phase,  # type: ignore[arg-type]
        message=message,
        model=model,
        basis=_json_safe(basis or {}),
        request=_json_safe(request or {}),
        result=_json_safe(result or {}),
    )


def _runtime_log_event(
    state: dict[str, Any],
    *,
    actor: str,
    phase: str,
    message: str,
    model: str = "",
    basis: dict[str, Any] | None = None,
    request: dict[str, Any] | None = None,
    result: dict[str, Any] | None = None,
) -> Event:
    entry = _runtime_log_entry(
        state,
        actor=actor,
        phase=phase,
        message=message,
        model=model,
        basis=basis,
        request=request,
        result=result,
    )
    current = list(state.get(STATE_RUNTIME_LOG) or [])
    current.append(entry.model_dump(mode="json", by_alias=True))
    return Event(
        author=actor,
        actions=EventActions(state_delta={STATE_RUNTIME_LOG: current}),
        content=genai_types.Content(
            role="model",
            parts=[
                genai_types.Part(
                    text=json.dumps(
                        {"runtimeLog": entry.model_dump(mode="json", by_alias=True)},
                        ensure_ascii=False,
                    )
                )
            ],
        ),
    )


def _apply_event_state_delta_to_session(ctx: InvocationContext, event: Event) -> None:
    state_delta = getattr(getattr(event, "actions", None), "state_delta", None) or {}
    if not state_delta:
        return
    for key, value in state_delta.items():
        ctx.session.state[key] = value


def _resolve_model(*candidates: str, fallback: str) -> str:
    for candidate in candidates:
        normalized = _MODEL_ALIASES.get(candidate.strip(), candidate.strip()) if candidate else ""
        if normalized:
            return normalized
    return fallback


# Flash / lite models: use AGENTLOOM_* only. Do not inherit AGENTMESH_MODEL_FLASH_REASONING here — that
# env is for other services and would silently override AgentLoom defaults in shared dev shells.
SUPERVISOR_MODEL = _resolve_model(os.getenv("AGENTLOOM_SUPERVISOR_MODEL", ""), fallback="gemini-3.1-flash-lite-preview")
INVESTIGATOR_MODEL = _resolve_model(os.getenv("AGENTLOOM_WORKER_MODEL", ""), fallback="gemini-3.1-flash-lite-preview")
CRITIC_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_CRITIC_MODEL", ""),
    os.getenv("AGENTMESH_MODEL_PRO", ""),
    fallback="gemini-2.5-pro",
)
COMPOSER_MODEL = _resolve_model(os.getenv("AGENTLOOM_COMPOSER_MODEL", ""), fallback=CRITIC_MODEL)
GROUNDING_MODEL = _resolve_model(os.getenv("AGENTLOOM_GROUNDING_MODEL", ""), fallback="gemini-3.1-flash-lite-preview")
# Used on 429 / quota for **all** generate_content retries (supervisor, tools, etc.)
RESOURCE_EXHAUSTION_FALLBACK_MODEL = os.getenv(
    "AGENTLOOM_RESOURCE_EXHAUSTION_FALLBACK_MODEL",
    "gemini-2.5-flash-lite",
).strip()
MAX_SUPERVISOR_CYCLES = int(os.getenv("AGENTLOOM_MAX_SUPERVISOR_CYCLES", "10"))


def _narrative_synthesis_enabled() -> bool:
    return os.getenv("AGENTLOOM_NARRATIVE_SYNTHESIS", "on").strip().lower() not in {"off", "0", "false", "no"}


def _rubric_llm_enabled() -> bool:
    return os.getenv("AGENTLOOM_RUBRIC_LLM", "").strip().lower() in {"1", "true", "on", "yes"}


ROLE_CONTRACTS: dict[InvestigationRole, RoleContract] = ROLE_CONTRACTS_BY_ROLE
SPECIALIZED_INVESTIGATOR_AGENTS = set(SPECIALIZED_INVESTIGATOR_BY_ROLE.values())


def _investigator_agent_for_role(role: InvestigationRole) -> str:
    return SPECIALIZED_INVESTIGATOR_BY_ROLE[role]


def _specialized_role_from_agent_name(agent_name: str) -> InvestigationRole | None:
    for role, specialized_agent in SPECIALIZED_INVESTIGATOR_BY_ROLE.items():
        if specialized_agent == agent_name:
            return role
    return None


_SUPERVISOR_DELEGATE_AGENTS = frozenset(
    {
        "supply_chain_investigator_agent",
        "governance_and_control_investigator_agent",
        "policy_impact_investigator_agent",
        "gap_bridge_agent",
        "critic_agent",
    }
)


def _coerce_supervisor_decision_payload(
    raw: dict[str, Any],
    *,
    selected_role_literals: list[str],
    cycle: int,
    user_question: str,
) -> dict[str, Any]:
    """
    Repair common LLM omissions so SupervisorDecision can validate without silently changing intent:
    missing `mission`, empty `assignedRoles`, missing missionId/objective/groundingMode.
    Specialized investigators always get exactly their role in assignedRoles.
    """
    if not raw:
        return raw
    next_agent = raw.get("nextAgent") or raw.get("next_agent")
    if next_agent not in _SUPERVISOR_DELEGATE_AGENTS:
        return raw

    out = {**raw}
    mission_raw = out.get("mission")
    mission: dict[str, Any] = {**(mission_raw if isinstance(mission_raw, dict) else {})}
    out["mission"] = mission

    valid_roles = [
        r
        for r in selected_role_literals
        if r in ("supply_chain", "governance_and_control", "policy_impact")
    ]
    if not valid_roles:
        valid_roles = ["supply_chain"]

    spec = _specialized_role_from_agent_name(str(next_agent))
    if spec is not None:
        mission["assignedRoles"] = [spec]
    else:
        existing = mission.get("assignedRoles") or mission.get("assigned_roles")
        if not existing:
            mission["assignedRoles"] = list(valid_roles)

    if not str(mission.get("missionId") or mission.get("mission_id") or "").strip():
        mission["missionId"] = f"supervisor-cycle-{cycle}-{next_agent}"

    objective = str(mission.get("objective") or "").strip()
    if not objective:
        q = (user_question or "").strip()[:400]
        if next_agent == "critic_agent":
            mission["objective"] = (
                "Assess whether the current graph is sufficient for an honest composed answer, "
                f"given reachability and role coverage, for: {q}"
            )
        elif next_agent == "gap_bridge_agent":
            mission["objective"] = (
                "Propose bridging hypotheses or targeted next questions to connect partial graph "
                f"structure for: {q}"
            )
        else:
            mission["objective"] = f"Advance investigation via {next_agent} for: {q}"

    gm = mission.get("groundingMode") or mission.get("grounding_mode")
    if gm not in ("knowledge_only", "mixed", "corpus_required"):
        mission["groundingMode"] = "mixed"

    return out


def _role_contract(role: InvestigationRole) -> RoleContract:
    return ROLE_CONTRACTS[role]


def _role_playbook_text(role: InvestigationRole) -> str:
    contract = _role_contract(role)
    tool_lines = [
        f"- {tool}: {guidance}"
        for tool, guidance in contract.tool_use_guidance.items()
    ]
    closure_lines = [f"- {item}" for item in contract.structural_closure_criteria]
    playbook_lines = [f"- {item}" for item in contract.investigation_playbook]
    role_title = {
        ROLE_SUPPLY_CHAIN: "SupplyChainInvestigator",
        ROLE_GOVERNANCE: "GovernanceAndControlInvestigator",
        ROLE_POLICY: "RegulatoryImpactInvestigator",
    }[role]
    return (
        f"Specialized role: {role_title}\n"
        f"Domain: {contract.domain_mode}\n"
        f"Description: {contract.description}\n"
        "Domain investigation playbook:\n"
        + "\n".join(playbook_lines)
        + "\nTool-use guidance:\n"
        + "\n".join(tool_lines)
        + "\nStructural closure criteria:\n"
        + "\n".join(closure_lines)
    )


def _specialized_instruction(base_instruction: str, role: InvestigationRole) -> str:
    return (
        _role_playbook_text(role)
        + "\n\nRole specialization rule:\n"
        + "- You are not a general investigator. Stay inside this role's playbook and graph semantics.\n"
        + "- Every active hypothesis you handle belongs to this one domain role.\n"
        + "- If another domain becomes relevant, surface it as a frontier, gap, or handoff; do not silently switch roles inside the same hypothesis.\n\n"
        + base_instruction
    )

ROLE_SELECTOR_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_ROLE_SELECTOR_MAX_OUTPUT_TOKENS", "1536")),
    temperature=0,
    response_mime_type="application/json",
)
SUPERVISOR_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_SUPERVISOR_MAX_OUTPUT_TOKENS", "16384")),
    temperature=0,
    response_mime_type="application/json",
)
INVESTIGATOR_PLANNER_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_INVESTIGATOR_PLANNER_MAX_OUTPUT_TOKENS", "16384")),
    temperature=0,
    response_mime_type="application/json",
)
INVESTIGATOR_SYNTHESIZER_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_INVESTIGATOR_SYNTHESIZER_MAX_OUTPUT_TOKENS", "16384")),
    temperature=0,
    response_mime_type="application/json",
)
GAP_BRIDGE_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_GAP_BRIDGE_MAX_OUTPUT_TOKENS", "16384")),
    temperature=0,
    response_mime_type="application/json",
)
CRITIC_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_CRITIC_MAX_OUTPUT_TOKENS", "16384")),
    temperature=0,
    response_mime_type="application/json",
)
NARRATIVE_COMPOSER_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_NARRATIVE_MAX_OUTPUT_TOKENS", "8192")),
    temperature=0,
    response_mime_type="application/json",
)
RUBRIC_LLM_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_RUBRIC_LLM_MAX_OUTPUT_TOKENS", "2048")),
    temperature=0,
    response_mime_type="application/json",
)

TOOL_MAX_OUTPUT_TOKENS = int(os.getenv("AGENTLOOM_TOOL_MAX_OUTPUT_TOKENS", "16384"))
RETRIEVAL_MAX_OUTPUT_TOKENS = int(os.getenv("AGENTLOOM_RETRIEVAL_MAX_OUTPUT_TOKENS", "8192"))

GRAPH_OBSERVATION_CONTRACT = """
GraphObservation contract:
- observationId: string. Stable within the tool result.
- subjectId: string. Stable node id such as "organization:example-operator" or "product:example-intermediate".
- subject: string. Human-readable node label.
- subjectType: string. Must be one of the entity types allowed by the active role contracts.
- relation: string. Must be one of the allowedEdgeTypes from the active role contracts.
- objectId: string. Stable node id.
- object: string. Human-readable node label.
- objectType: string. Must be one of the entity types allowed by the active role contracts.
- qualifiers: string array. Optional qualifiers such as geography, route, time, process, or confidence notes.
- confidence: number between 0 and 1.
- supportType: one of ["document", "model_knowledge", "mixed", "hypothesis"].
- provenance: string array of source URIs, document ids, or evidence handles.
- supportNote: short explanation of why this edge is supported or still uncertain.
- branchId: string. Must match the branch being answered.
- alternativeGroupId: string. Use the same id when several alternative paths belong to the same alternative family.
"""

CONFIDENCE_CALIBRATION = """
Confidence calibration by source:
- document-backed (source_mode=document): 0.7-0.95 when the retrieved document explicitly names this edge.
- mixed (source_mode=mixed): 0.5-0.8 when combining document evidence with model inference.
- model knowledge (source_mode=model): 0.2-0.6. Generic industry knowledge = 0.2-0.3, specific but unverified claims = 0.4-0.5, widely reported facts = 0.5-0.6.
- hypothesis: 0.1-0.4 for structural edges that assume a connection exists but the specific actor or route is unknown.
When source_mode is "model", any edge whose specific actors or evidence you cannot cite from a concrete public source must have confidence below 0.5. Edges that exist only to connect the graph structurally should use supportType=hypothesis with confidence 0.1-0.4.
"""

EDGE_DIRECTION_RULES = """
Edge direction contract (mandatory — edges violating these rules are auto-reversed or dropped):
- PRODUCES: ORGANIZATION/FACILITY → PRODUCT. Never product→facility.
- SUPPLIES_TO: ORGANIZATION/FACILITY/PRODUCT → ORGANIZATION/FACILITY. Supplier→customer. Never to GEO_REGION or PRODUCT.
- TRANSFORMS_INTO: PRODUCT → PRODUCT only. Never facility→product.
- ASSEMBLES_INTO: PRODUCT → PRODUCT only.
- USED_IN: PRODUCT → PRODUCT only.
- EXTRACTS_FROM: FACILITY → DEPOSIT only.
- LOCATED_IN: any entity → GEO_REGION. Never geo→entity.
- OWNS: ORGANIZATION/PERSON → ORGANIZATION/FACILITY/PRODUCT/DEPOSIT. Owner→asset.
- CONTROLS: ORGANIZATION/PERSON → ORGANIZATION/FACILITY/PRODUCT. Controller→entity.
- REGULATES: ORGANIZATION/PROGRAM → ORGANIZATION/FACILITY/PRODUCT/GEO_REGION.
- INVESTS_IN: ORGANIZATION/PERSON → ORGANIZATION/PROGRAM/PRODUCT. Investor→investee.
- MANAGES: ORGANIZATION/PERSON → ORGANIZATION/FACILITY/PROGRAM.
- ADMINISTERS: ORGANIZATION/GEO_REGION → ORGANIZATION/PROGRAM/GEO_REGION/FACILITY/DEPOSIT.
If you are unsure of the direction, think: "who is the actor/source?" That entity goes on the left (subject). "What is being acted upon?" goes on the right (object).
"""

SUPPLY_CHAIN_GRAPH_RULES = """
Supply-chain graph rules:
- Colloquial commodity, element, or material names in the user question are not operational nodes. Treat them as intent to map the relevant traded or processed product-stage ladder for that use-case. Illustrative examples of stage classes only (non-exhaustive, adapt to the sector in the question): ores and concentrates, intermediate chemicals or salts, alloys, precursor stages toward downstream goods—encode each real stage as a PRODUCT node with lawful edges; do not treat the list as mandatory coverage or as domain-specific canon.
- Do not model countries or regions as if they directly produce, transform, supply, or assemble products.
- Geography belongs in overlay edges such as LOCATED_IN or OPERATES_IN.
- Deposits are geological sources. Use EXTRACTS_FROM from a FACILITY or ORGANIZATION to a DEPOSIT.
- Facilities or organizations produce and supply products. Use PRODUCES or SUPPLIES_TO from FACILITY/ORGANIZATION to PRODUCT or another operational node.
- Product transformation steps must be PRODUCT --TRANSFORMS_INTO--> PRODUCT.
- Product usage or assembly steps must not terminate in a country or region.
- End-of-chain geography must be represented as FACILITY/ORGANIZATION --LOCATED_IN--> GEO_REGION, not PRODUCT --ASSEMBLES_INTO--> GEO_REGION.
- Prefer concrete typed nodes over abstract placeholders. Labels such as "Regional Mining Facility", "Downstream Battery Plant", or a vague regional producer or refiner acting as a single anonymous stand-in are provisional placeholders, not satisfactory closure when a concrete ORG/FACILITY/DEPOSIT/PRODUCT should exist.
- If you only know a broad placeholder now, keep it with low confidence or supportType=hypothesis, then expose a frontier question or a replacement patch that asks what concrete ORG, FACILITY, DEPOSIT, PRODUCT, or other role-valid typed node should replace it.
- Multiple facilities may legitimately produce or supply the same product. Do not collapse them into one fake node. Return several nodes and edges when the evidence supports several parallel suppliers, receivers, or alternative paths. Expect multiple plausible route families unless the question explicitly narrows to one; use alternativeGroupId and parallel fragments instead of forcing a single winner when evidence supports multiplicity.
- Adjacent-frame context (regional processing near the sink, alternate supplier class, market-pool effects) may appear as separate `alternative_chain` fragments, `learnedFindings`, or low-confidence hypotheses when it informs the user's decision but does not justify a fake direct flow edge between the user's named anchors.
- Structural connectivity matters more than quantitative detail. A typed edge with unknown tonnage is still far more valuable than a tonnage estimate with no typed edge.
"""

SUPPLY_CHAIN_PROFILE_CONTRACT = """
Ideal supply-chain profile for one or several alternative chains:
- source geology: one or more DEPOSIT nodes
- extraction layer: FACILITY and/or ORGANIZATION nodes that extract from deposits
- primary output layer: PRODUCTS produced by mining or initial processing facilities
- transformation layer: one or more PRODUCT --TRANSFORMS_INTO--> PRODUCT steps
- operating facility layer: FACILITY nodes that produce, refine, convert, or assemble along the path
- ownership/control overlay: ORGANIZATION nodes that own, control, invest in, or manage key facilities
- downstream manufacturing layer: FACILITY and ORGANIZATION nodes that consume battery materials and produce downstream products
- geography overlay: GEO_REGION nodes connected through LOCATED_IN or OPERATES_IN
- alternative paths: if more than one path exists, use alternativeGroupId and represent each path as a separate fragment or fragment family
- multiplicity: the same layer may contain several facilities or organizations in parallel. Preserve that multiplicity instead of forcing one canonical actor when the question or evidence supports several.
- contractual layer: procurement model, offtake, sourcing strategy, localization, recycling strategy, and vertical integration are useful learned findings but do not by themselves replace missing operational flow edges.

Use this profile diagnostically:
- if a layer is missing, say so in uncoveredQuestions
- if only a holistic re-query makes sense, ask one whole-chain uncovered question
- if multiple alternatives exist but only one is known, ask for the missing alternative path explicitly
- if a stable background regulation is only contextual and not the missing operational edge, keep it as context or learned finding instead of spending a grounded turn on it
- distinguish qualitative structural closure from quantitative detail:
  - qualitative = do we have the right typed nodes and typed links?
  - quantitative = how much, what share, what tonnage, what exact percentage?
  - qualitative closure is the main criterion for graph usefulness; quantitative uncertainty should be recorded as detail, not treated as a reason to pretend the graph is disconnected.
"""

GRAPH_FRAGMENT_CONTRACT = """
GraphFragment contract:
- This is the single graph-result format for every graph operation in the hypothesis loop.
- The same GraphFragment structure is used for:
  - the first broad skeleton;
  - later additions or enrichments;
  - replacements or structural corrections.
- There is no separate "overview answer format" versus "mutation answer format". The runtime always consumes GraphFragment objects and mutates the same shared graph state under one protocol.
- Therefore, when you answer a broad root question, do not invent a different structure. Return ordinary graphFragments that can later be appended to, replaced, or refined by the same rules.
- fragmentId: string. Stable within the branch.
- fragmentType: one of ["overview_chain", "chain_fragment", "alternative_chain", "ownership_overlay", "policy_overlay", "hypothesis"].
- title: short human-readable title.
- humanQuestion: the exact human-readable question this fragment answers.
- humanSummary: short human-readable summary of what was found.
- branchId: string. Must match the branch being answered.
- alternativeGroupId: string. Use when multiple fragments belong to one alternative-path family.
- startNodeId: optional string. The fragment entry node when known.
- endNodeId: optional string. The fragment exit node when known.
- patchMode: one of ["append", "replace_node", "replace_fragment"].
  - append = add a new fragment without replacing any prior placeholder.
  - replace_node = the fragment replaces one broad or fake typed node with a more concrete graph fragment.
  - replace_fragment = the fragment supersedes one or more earlier fragments because the new graph is structurally better.
- oldId: optional string. The single node or fragment id being replaced.
- oldIds: optional string array. Use when replacing several prior placeholders/fragments at once.
- patchRationale: required for replace_node/replace_fragment. Explain why the old node/fragment was too broad, wrong, or structurally weaker than the new one.
- observations: array of GraphObservation.
- completeness: one of ["partial", "complete", "hypothesis"].
- qualityScore: number between 0 and 1.

Patch semantics:
- Use replacement when the current graph contains a broad placeholder and you now know the concrete ORG/FACILITY/DEPOSIT/PRODUCT node or fragment that should stand in its place.
- Replacement may expand one node into a multi-hop fragment. Example: replacing a broad downstream battery plant placeholder with a short chain of named FACILITY and PRODUCT stages. Another valid example: replacing a broad upstream mining facility with a concrete mine, deposit, and first salable product output linked by lawful edges.
- Replacement is graph-safe and does not imply that the old node was useless; it means the new fragment is a better structural representation.
- Replacement is a real graph mutation, not decorative metadata. After a valid `replace_node` or `replace_fragment`, the runtime updates shared memory so later reasoning sees the replacement graph, not the superseded placeholder graph.
- Therefore, when you issue a replacement patch, make the new fragment structurally self-sufficient enough to occupy the old slot in the chain, ownership tree, star, or network that is being improved.
- Think of `patchMode` as the only mutation verb:
  - `append` = extend the current graph;
  - `replace_node` = replace a broad or wrong node with a better fragment;
  - `replace_fragment` = replace a weaker prior fragment with a stronger one.
- The graph inside one hypothesis is always created and mutated by these same rules.
"""

TOOL_ACTION_RESULT_CONTRACT = """
    ToolActionResult contract:
- actionId: string. Must equal the incoming actionId.
- branchId: string. Must equal the incoming branchId.
- toolName: string. Must equal the incoming toolName.
- pathAssessment: one of ["confirmed", "best_supported_indirect", "rejected", "still_open", "not_applicable"].
- pathAssessmentNote: short human-readable explanation of why this candidate path is confirmed, rejected, or still open.
- status: one of ["success", "partial", "blocked", "failed"].
- humanSummary: short human-readable result summary.
- closureStatus: one of ["advanced", "closed", "unchanged", "redirected"].
- resolvedFrontier: string array. Which missing links or unknowns this action resolved.
- remainingFrontier: string array. Which missing links or unknowns still remain after this action.
- discoveredFrontier: string array. New missing links or unknowns discovered while answering.
- graphFragments: array of GraphFragment. Use [] when blocked or failed. Fragments may append to the graph or replace earlier broad placeholders/fragments using patchMode + oldId/oldIds.
- learnedFindings: array of LearnedFinding. Every useful result is still a result even when it does not yield a new graph edge. Record stable knowledge, negative results, procurement-model findings, and other reusable context here so later turns can ground against it.
- uncoveredQuestions: string array of unresolved questions that remain after this action.
- sourceDocuments: string array of source URIs or document ids used in the answer.
- failure: null or FailureRecord.

LearnedFinding contract:
- findingId: string. Stable within the branch.
- hypothesisId: string. Usually the current branchId unless you are explicitly referring to the parent hypothesis.
- branchId: string. Must match the branch being answered.
- role: one of the active investigation roles.
- questionAsked: the exact question that produced this learned result.
- findingKind: one of ["operational_flow", "contractual_procurement", "governance_overlay", "policy_overlay", "negative_result", "context"].
- summary: short reusable statement of what the system learned.
- evidenceStatus: one of ["verified", "reasoned", "mixed", "hypothesis"].
- confidence: number between 0 and 1.
- relatedEntities: entity labels or ids that later turns should reuse for grounding.
- sourceDocuments: string array of source URIs or document ids used for the finding.

FailureRecord contract:
- code: one of ["missing_question", "missing_corpus", "missing_graph", "blocked_by_capability", "agent_failed"].
- message: short human-readable failure explanation.
- blocking: boolean.
- missingInputs: string array.
"""

TOOL_ACTION_RESULT_EXAMPLES = """
Valid success example:
{
  "actionId": "branch-1-a1",
  "branchId": "branch-1",
  "toolName": "retrieve",
  "pathAssessment": "still_open",
  "pathAssessmentNote": "The upstream path is grounded, but the downstream named converter and receiver are still missing.",
  "status": "success",
  "humanSummary": "Found a grounded supply-chain fragment from extraction through hydrometallurgical conversion into a downstream battery-input product stage.",
  "closureStatus": "advanced",
  "resolvedFrontier": [
    "Confirmed a facility-aware path from deposit through mined ore into a soluble intermediate."
  ],
  "remainingFrontier": [
    "Identify the facility or organization that converts the mixed intermediate into the declared battery-input product stage."
  ],
  "discoveredFrontier": [
    "Determine whether the downstream receiver sits in the destination market region or behind an intermediate toll processor."
  ],
  "graphFragments": [
    {
      "fragmentId": "branch-1-f1",
      "fragmentType": "chain_fragment",
      "title": "Laterite ore to battery-input salt",
      "humanQuestion": "What sits between laterite mining and the declared downstream battery-input product stage?",
      "humanSummary": "The grounded path runs from laterite ore through pressure-leach style conversion into a mixed intermediate, then toward a named sulfate-class battery input.",
      "branchId": "branch-1",
      "alternativeGroupId": "route_family_a",
      "startNodeId": "deposit:example-laterite-deposit",
      "endNodeId": "product:example-battery-input-salt",
      "patchMode": "append",
      "oldId": "",
      "oldIds": [],
      "patchRationale": "",
      "observations": [
        {
          "observationId": "branch-1-f1-o1",
          "subjectId": "facility:example-extraction-complex",
          "subject": "Example extraction complex",
          "subjectType": "FACILITY",
          "relation": "EXTRACTS_FROM",
          "objectId": "deposit:example-laterite-deposit",
          "object": "Example laterite deposit",
          "objectType": "DEPOSIT",
          "qualifiers": ["laterite ore body"],
          "confidence": 0.79,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence ties the mining facility to the laterite deposit.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "observationId": "branch-1-f1-o2",
          "subjectId": "facility:example-extraction-complex",
          "subject": "Example extraction complex",
          "subjectType": "FACILITY",
          "relation": "PRODUCES",
          "objectId": "product:example-laterite-ore",
          "object": "Example laterite ore",
          "objectType": "PRODUCT",
          "qualifiers": [],
          "confidence": 0.77,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence identifies mined laterite output.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "observationId": "branch-1-f1-o3",
          "subjectId": "product:example-laterite-ore",
          "subject": "Example laterite ore",
          "subjectType": "PRODUCT",
          "relation": "TRANSFORMS_INTO",
          "objectId": "product:example-mixed-intermediate",
          "object": "Example mixed hydroxide intermediate",
          "objectType": "PRODUCT",
          "qualifiers": ["hydrometallurgical conversion"],
          "confidence": 0.74,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Evidence describes leach conversion into the mixed intermediate.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        }
      ],
      "completeness": "partial",
      "qualityScore": 0.72
    }
  ],
  "uncoveredQuestions": [
    "Which named converter produces the battery-input salt in this path?"
  ],
  "sourceDocuments": ["gs://source/doc-1"],
  "failure": null
}

Valid blocked example:
{
  "actionId": "branch-2-a1",
  "branchId": "branch-2",
  "toolName": "retrieve",
  "pathAssessment": "still_open",
  "pathAssessmentNote": "Memory alone cannot confirm or reject this ownership path.",
  "status": "blocked",
  "humanSummary": "Shared memory does not yet contain a typed ownership path for this question.",
  "closureStatus": "unchanged",
  "resolvedFrontier": [],
  "remainingFrontier": [
    "Which company controls the refining stage in this path?"
  ],
  "discoveredFrontier": [],
  "graphFragments": [],
  "learnedFindings": [
    {
      "findingId": "branch-2-lf1",
      "hypothesisId": "branch-2",
      "branchId": "branch-2",
      "role": "governance_and_control",
      "questionAsked": "Which company controls the refining stage in this path?",
      "findingKind": "negative_result",
      "summary": "Shared memory currently contains no typed ownership path for the refining stage.",
      "evidenceStatus": "reasoned",
      "confidence": 0.61,
      "relatedEntities": ["refining stage"],
      "sourceDocuments": []
    }
  ],
  "uncoveredQuestions": [
    "Which company controls the refining stage in this path?"
  ],
  "sourceDocuments": [],
  "failure": {
    "code": "missing_graph",
    "message": "No relevant typed ownership fragment exists in shared memory.",
    "blocking": false,
    "missingInputs": ["working_graph"]
  }
}

Valid replacement example:
{
  "actionId": "branch-3-a2",
  "branchId": "branch-3",
  "toolName": "web_search",
  "pathAssessment": "confirmed",
  "pathAssessmentNote": "The broad sink-side geographic placeholder can now be replaced with concrete receiving facilities.",
  "status": "success",
  "humanSummary": "Replaced the broad sink anchor with named receiving facilities.",
  "closureStatus": "closed",
  "resolvedFrontier": [
    "Replace the broad downstream anchor with concrete facilities in the destination market."
  ],
  "remainingFrontier": [],
  "discoveredFrontier": [
    "Which supplier or intermediary connects the battery-input product stage to these named facilities?"
  ],
  "graphFragments": [
    {
      "fragmentId": "branch-3-f2",
      "fragmentType": "chain_fragment",
      "title": "Concrete downstream receiving facilities",
      "humanQuestion": "Which concrete facilities should replace the current broad sink-side placeholder node?",
      "humanSummary": "The broad sink is better represented by specific receiving facilities.",
      "branchId": "branch-3",
      "alternativeGroupId": "",
      "startNodeId": "product:example-battery-input-salt",
      "endNodeId": "facility:example-cathode-input-plant",
      "patchMode": "replace_node",
      "oldId": "facility:placeholder-downstream-battery-plant",
      "oldIds": [],
      "patchRationale": "The prior sink node was only a geographic placeholder. The new fragment inserts concrete facilities that should occupy that position in the path.",
      "observations": [
        {
          "observationId": "branch-3-f2-o1",
          "subjectId": "product:example-battery-input-salt",
          "subject": "Example battery-input salt",
          "subjectType": "PRODUCT",
          "relation": "SUPPLIES_TO",
          "objectId": "facility:example-cathode-input-plant",
          "object": "Example cathode-input plant",
          "objectType": "FACILITY",
          "qualifiers": [],
          "confidence": 0.72,
          "supportType": "document",
          "provenance": ["gs://source/doc-9"],
          "supportNote": "Named public evidence ties the downstream facility to battery-material intake.",
          "branchId": "branch-3",
          "alternativeGroupId": ""
        }
      ],
      "completeness": "partial",
      "qualityScore": 0.84
    }
  ],
  "learnedFindings": [
    {
      "findingId": "branch-3-lf1",
      "hypothesisId": "branch-3",
      "branchId": "branch-3",
      "role": "supply_chain",
      "questionAsked": "Which concrete facilities should replace the current broad sink-side placeholder node?",
      "findingKind": "operational_flow",
      "summary": "Example cathode-input plant is a concrete downstream receiving node that replaces the broad sink placeholder.",
      "evidenceStatus": "verified",
      "confidence": 0.72,
      "relatedEntities": ["Example battery-input salt", "Example cathode-input plant"],
      "sourceDocuments": ["gs://source/doc-9"]
    }
  ],
  "uncoveredQuestions": [
    "Which supplier or intermediary connects the battery-input product stage to the named cathode-input plant?"
  ],
  "sourceDocuments": ["gs://source/doc-9"],
  "failure": null
}
"""

BRANCH_PLAN_CONTRACT = """
BranchPlan contract:
- branchId: short stable id.
- hypothesis: short branch hypothesis.
- parentHypothesisId: optional id of the hypothesis this branch came from. Empty only for the first/root hypothesis branch.
- hypothesisRelation: one of ["root", "dependent_followup", "independent_child"].
- spawnReason: short human-readable reason why this branch exists now.
- independenceBasis: short explanation of why this branch must stay inside the current hypothesis loop or can live independently.
- pathKind: one of ["overview", "direct", "indirect", "alternative", "bridge"].
- humanQuestion: one concrete human-readable question the branch will answer.
- humanAction: one human-readable sentence describing what the branch will do.
- closureTarget: what graph closure this branch is trying to establish, disprove, or refine.
- knownContext: short state-aware facts the system already knows and that justify this branch.
- frontier: ordered list of specific missing links, missing actors, or missing layers the branch will try to close.
- assignedRoles: subset of the mission roles.
- preferredTools: ordered array using only ["reason", "retrieve", "web_search"].
- targetEntities: optional entity names the branch should focus on.
- fragmentType: one of ["overview_chain", "chain_fragment", "alternative_chain", "ownership_overlay", "policy_overlay", "hypothesis"].
- parallelGroup: string. Use the same value for branches that can run in parallel.
- stopAfterNoImprovement: integer >= 1.
"""

BRANCH_PLAN_EXAMPLE = """
Example 1 — whole-hypothesis branch on empty graph:
{
  "assessment": "The graph is empty. The first priority is a complete hypothesis-level answer to the question as one connected graph.",
  "rationale": [
    "No graph structure exists yet. Must start with a whole-picture hypothesis.",
    "Model knowledge can produce a connected graph with hypothesis-level edges that subsequent cycles will verify.",
    "One parallel grounding branch can start gathering evidence for the most critical segment."
  ],
  "branchPlans": [
    {
      "branchId": "full_hypothesis",
      "hypothesis": "A complete hypothesis-level graph answering the question can be built from model knowledge.",
      "parentHypothesisId": "",
      "hypothesisRelation": "root",
      "spawnReason": "The graph is empty and needs one broad starting hypothesis.",
      "independenceBasis": "This is the root hypothesis for the whole investigation.",
      "pathKind": "overview",
      "humanQuestion": "What is the complete picture of how the investigated material's relevant product stages reach the named downstream battery use-case, as one connected graph from source geology or import to end product?",
      "humanAction": "Build a whole hypothesis-level graph answering the question end-to-end using model knowledge.",
      "closureTarget": "One connected graph from source to destination covering the full question scope, with hypothesis-level edges where specific evidence is missing.",
      "knownContext": [
        "The graph is empty. No prior structure exists."
      ],
      "frontier": [
        "Complete connected graph answering the question, even if many edges are hypothesis-level."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["reason"],
      "targetEntities": ["user material phrase", "destination scope", "downstream use-case"],
      "fragmentType": "overview_chain",
      "parallelGroup": "initial",
      "stopAfterNoImprovement": 1
    }
  ],
  "blockingFailure": null
}

Example 2 — weak-spot verification on populated graph:
{
  "assessment": "The graph has a hypothesis backbone. Several edges have confidence below 0.4 and need targeted verification.",
  "rationale": [
    "The backbone hypothesis connects source to destination but the midstream segment is unverified.",
    "Two weak edges can be verified in parallel with Vertex grounded search."
  ],
  "branchPlans": [
    {
      "branchId": "verify_refining_link",
      "hypothesis": "The hypothesis edge from mined ore to a downstream battery-input product stage can be grounded with real facility data.",
      "parentHypothesisId": "full_hypothesis",
      "hypothesisRelation": "dependent_followup",
      "spawnReason": "The overview hypothesis exposed an ungrounded refining hop that blocks closure.",
      "independenceBasis": "This branch refines a weak edge inside the current overview hypothesis and should merge back into it.",
      "pathKind": "direct",
      "humanQuestion": "Which specific facilities refine the documented ore or concentrate into the declared battery-input product stage? The current graph hypothesizes this link but has no grounded evidence.",
      "humanAction": "Verify the hypothesis refining link with grounded facility-level evidence.",
      "closureTarget": "Replace the hypothesis edge between upstream extraction output and the downstream battery-input product with a grounded, facility-level connection.",
      "knownContext": [
        "The graph already contains a hypothesis edge: Example Ore --TRANSFORMS_INTO--> Example Battery-Input Product (confidence 0.3, supportType=hypothesis).",
        "Upstream extraction and downstream cell or pack manufacturing are already in the graph at higher confidence."
      ],
      "frontier": [
        "Identify specific refining facility or organization.",
        "Confirm the input-output product transformation."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["retrieve", "reason"],
      "targetEntities": ["battery-input product stage", "source-side scope", "refining"],
      "fragmentType": "chain_fragment",
      "parallelGroup": "verify",
      "stopAfterNoImprovement": 2
    }
  ],
  "blockingFailure": null
}

Example 3 — one hypothesis spawns parallel follow-ups and an independent child:
{
  "assessment": "The primary path backbone already exists. Sink resolution and ownership clarification can run in parallel inside the same hypothesis loop, while an alternative jurisdiction route is independent enough to become its own child hypothesis.",
  "rationale": [
    "The current hypothesis already contains a usable broad picture.",
    "Two clarifications are dependent follow-ups because they refine the same parent path.",
    "A separate jurisdictional route can now be investigated independently without waiting for the parent loop to finish."
  ],
  "branchPlans": [
    {
      "branchId": "resolve_sink_nodes",
      "hypothesis": "The broad sink can be replaced with concrete receiving facilities.",
      "parentHypothesisId": "full_hypothesis",
      "hypothesisRelation": "dependent_followup",
      "spawnReason": "The parent hypothesis already reaches the destination market but the sink is still broad.",
      "independenceBasis": "This branch refines the existing parent hypothesis and must merge back into it.",
      "pathKind": "direct",
      "humanQuestion": "Which concrete facilities should replace the current broad sink-side placeholder node?",
      "humanAction": "Resolve the sink-side facilities inside the current path hypothesis.",
      "closureTarget": "Replace the broad sink node with concrete receiving facilities.",
      "knownContext": [
        "The parent hypothesis already reaches the destination market region but still uses a broad sink node."
      ],
      "frontier": [
        "Name the concrete receiving facilities in the destination scope."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["retrieve", "web_search"],
      "targetEntities": ["destination scope", "battery facilities"],
      "fragmentType": "chain_fragment",
      "parallelGroup": "parent_h1_refine",
      "stopAfterNoImprovement": 2
    },
    {
      "branchId": "map_parent_ownership",
      "hypothesis": "The parent path likely runs through a named intermediary layer between the current product stage and the sink.",
      "parentHypothesisId": "full_hypothesis",
      "hypothesisRelation": "dependent_followup",
      "spawnReason": "The parent hypothesis reaches a downstream sink but still lacks the immediate operational intermediary layer.",
      "independenceBasis": "This branch fills an operational bridge inside the same parent supply-chain graph and must merge back into it.",
      "pathKind": "bridge",
      "humanQuestion": "Which intermediary facility or processor sits between the current product stage and the named downstream sink?",
      "humanAction": "Resolve the missing intermediary layer inside the current parent path.",
      "closureTarget": "Insert the operational intermediary layer between the known product stage and the downstream sink.",
      "knownContext": [
        "The parent hypothesis already includes a named downstream sink but still lacks the intermediary operational layer."
      ],
      "frontier": [
        "Identify the immediate intermediary facility, processor, or supplier between the known product stage and the sink."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["retrieve", "web_search"],
      "targetEntities": ["intermediate product stage", "intermediary", "downstream sink"],
      "fragmentType": "chain_fragment",
      "parallelGroup": "parent_h1_refine",
      "stopAfterNoImprovement": 2
    },
    {
      "branchId": "alternative_jurisdiction_route",
      "hypothesis": "A materially different jurisdictional route may reach the same downstream market through different companies and facilities.",
      "parentHypothesisId": "full_hypothesis",
      "hypothesisRelation": "independent_child",
      "spawnReason": "The parent route is clear enough that alternatives can now be investigated independently.",
      "independenceBasis": "This alternative route can be studied on its own and does not require the parent hypothesis to be finished.",
      "pathKind": "alternative",
      "humanQuestion": "What materially different route reaches the same downstream market through another jurisdiction or company set?",
      "humanAction": "Open an independent alternative-path hypothesis.",
      "closureTarget": "Establish a separate alternative route that can be compared with the parent path.",
      "knownContext": [
        "One plausible primary route already exists."
      ],
      "frontier": [
        "Identify a materially different alternative path."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["reason", "web_search"],
      "targetEntities": ["alternative route", "other jurisdiction"],
      "fragmentType": "alternative_chain",
      "parallelGroup": "alternative_family",
      "stopAfterNoImprovement": 2
    }
  ],
  "blockingFailure": null
}
"""

REASONING_MODES_GUIDANCE = """
Reasoning modes available to the planner and tool prompts.
These are not mandatory workflow steps. They are investigation options that should be chosen only when they fit the current state.

1. Primary full-path mode
- When it is useful:
  - the graph is empty or nearly empty;
  - the question asks "how is this structured?" or "how does X reach Y?";
  - the current state has broad anchors (countries, sectors, regions) but no coherent path.
- What to ask:
  - ask for the single most plausible end-to-end path or connected graph that answers the question now;
  - explicitly allow hypothesis-level edges where evidence is not yet grounded;
  - ask for named source, intermediate, and receiving entities if they can be inferred.
- What the result should look like:
  - one `overview_chain` or `chain_fragment` with a long observation array;
  - it should prefer a connected multi-hop path over isolated facts;
  - if endpoints are still broad, mark them as provisional and expose the missing endpoint-resolution questions in `remainingFrontier`;
  - if a later step finds concrete typed nodes or operators, return a replacement fragment with `patchMode=replace_node` or `replace_fragment` instead of silently leaving the placeholder beside the better graph.
  - this is not a special root-only format: the result must still be a normal GraphFragment payload that will later be mutated by append/replace under the same shared-graph contract.

2. Alternative-path mode
- When it is useful:
  - the primary path already exists;
  - the user explicitly asks for alternatives, variants, or competing routes;
  - the current path appears technologically narrow or geographically narrow.
- What to ask:
  - ask whether materially different technological, geographic, or intermediary routes exist;
  - name the currently known path in `knownContext` so the tool does not simply restate it;
  - ask for each alternative as its own path or fragment family.
- What the result should look like:
  - one or more `alternative_chain` or `chain_fragment` results;
  - use `alternativeGroupId` when several fragments belong to one alternative family;
  - clearly separate "same path with more detail" from "genuinely different route."

3. Endpoint-resolution mode
- When it is useful:
  - the current path starts or ends at broad anchors like countries, sectors, or markets;
  - the path contains placeholders such as a generic downstream battery plant or a generic regional mine;
  - a path cannot be closed because the true sender or receiver node is unknown.
- What to ask:
  - ask who or what the actual source, operator, facility, organization, deposit, product-stage, or receiving node is for the investigated material or its derivative;
  - ask specifically for the source-side or sink-side endpoint if only one side is vague;
  - state what broad anchor is already known and what concrete node must replace it.
- What the result should look like:
  - one or more `chain_fragment` or overlay fragments that replace broad anchors with concrete typed nodes;
  - the new fragment should explicitly connect the resolved endpoint back into the existing path;
  - if the current graph contains a fake anchor like a regional mining facility or a downstream battery plant placeholder, use patch semantics to replace it with concrete facilities, organizations, deposits, products, or receiving nodes.

4. Bridge mode
- When it is useful:
  - two graph islands already exist but are not connected;
  - the current graph has a break point or dead-end node;
  - a direct route was rejected but an indirect route may survive.
- What to ask:
  - ask what sits between node A and node B;
  - ask for the next operational receiver, intermediary, converter, or control layer that could connect the islands;
  - if the direct route was rejected, ask for the best-supported indirect bridge rather than re-asking the direct route.
- What the result should look like:
  - one `bridge` or `chain_fragment` result that explicitly names the missing intermediary layer or states that no supported bridge was found.

5. Adjacent-frame mode
- When it is useful:
  - the user question implies a narrow geographic or trade corridor, but decision-relevant context lives in **neighboring** processing, sourcing pools, or regional conversion that is **not** coextensive with a direct A-to-B flow edge;
  - omitting that neighborhood would make the final answer misleading even though evidence does not support faking `SUPPLIES_TO` between the named anchors.
- What to ask:
  - capture adjacent context as its own `alternative_chain` / `chain_fragment` family with a distinct `alternativeGroupId`, or as explicit `learnedFindings`, or as clearly labeled low-confidence hypotheses—never as `document`-backed flow pretending to close the user's narrow corridor.
- What the result should look like:
  - fragments or findings that a downstream critic and composer can separate from grounded reachability; parallel to—not instead of—endpoint resolution and bridge work on the primary path.

Frontier-generation discipline for broad reasoning:
- When the action is broad (`pathKind=overview`, `fragmentType=overview_chain`, or the question asks for the whole picture), you are not finished after drawing one plausible path.
- Use `resolvedFrontier` for what the current broad picture now explains.
- Use `remainingFrontier` for gaps inside the same active hypothesis that still need refinement before the current path is operationally usable.
- Use `discoveredFrontier` for newly revealed, independently meaningful next lines of investigation.
- If several candidate source nodes, sink nodes, operators, or intermediaries appear to occupy the same layer of the same hypothesis graph, do not treat each one as a separate route by default. Keep them as parallel candidates inside the same hypothesis unless they form materially different route families.
- A dependent follow-up branch is expected to merge back into the same hypothesis graph. Ask dependent questions as if the answer will land on the current graph object, not as if it will create a brand-new chain artifact.
- If your broad picture contains one workable hot path and the user explicitly asked for alternatives, `discoveredFrontier` should usually include at least one materially different route-family question.
- If your broad picture already mixes more than one materially different route family (for example different jurisdictions, technologies, or intermediary classes), surface that explicitly. Name the currently visible families and ask for the missing or less-developed family as a separate next investigation.
- If source-side or sink-side endpoints are still broad geographic or market anchors, surface explicit endpoint-resolution questions in `discoveredFrontier` or `remainingFrontier`.
- A good discovered-frontier question is concrete and graph-shaped. Example: "What materially different processing-family route reaches the same downstream battery sink compared with the current hydrometallurgical route?" Example: "Which concrete mines, deposits, or operators in the source-side scope should replace the current broad source anchor?" Example: "Which concrete receiving facilities in the destination scope should replace the current broad downstream anchor?"
- Do not leave alternatives buried inside `humanSummary`. If an alternative route family is visible or likely relevant to the user question, expose it as a frontier question.
"""


def _vertex_grounding_config() -> dict[str, Any]:
    project = _first_non_empty(
        os.getenv("GOOGLE_CLOUD_PROJECT"),
        os.getenv("GCP_PROJECT_ID"),
        os.getenv("VERTEX_SEARCH_PROJECT_ID"),
    )
    location = _first_non_empty(
        os.getenv("VERTEX_SEARCH_LOCATION"),
        os.getenv("AGENTLOOM_VERTEX_LOCATION"),
        os.getenv("GOOGLE_CLOUD_LOCATION"),
        "global",
    )
    data_store = _first_non_empty(
        os.getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID"),
        os.getenv("VERTEX_SEARCH_DATA_STORE_ID"),
        os.getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
        os.getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
    )
    if not project:
        return {
            "available": False,
            "reason": "GOOGLE_CLOUD_PROJECT or GCP_PROJECT_ID is required for Vertex grounded search.",
        }
    if not data_store:
        return {
            "available": False,
            "reason": "No Vertex AI Search datastore is configured (set VERTEX_SEARCH_*_DATA_STORE_ID).",
        }
    datastore_path = data_store
    if "/" not in data_store:
        datastore_path = (
            f"projects/{project}/locations/global/collections/default_collection/dataStores/{data_store}"
        )
    return {
        "available": True,
        "project": project,
        "location": location,
        "dataStoreId": data_store,
        "datastorePath": datastore_path,
    }


def _web_search_available() -> dict[str, Any]:
    project = _first_non_empty(
        os.getenv("GOOGLE_CLOUD_PROJECT"),
        os.getenv("GCP_PROJECT_ID"),
    )
    if not project:
        return {"available": False, "reason": "GOOGLE_CLOUD_PROJECT is required for Google Search grounding."}
    return {"available": True, "project": project}


TOOL_CONTRACTS = [
    ToolContract(
        name="read_shared_memory",
        category="memory",
        capability="Read the current shared ADK session memory, including selected roles, role contracts, graph state, gaps, failures, and mission state.",
        requiredInputs=[],
        outputShape="JSON object with question, selectedRoles, roleContracts, currentMission, graph observations, open gaps, failures, tool availability, and last agent result.",
        failureModes=["returns initialized empty state when no prior investigation memory exists"],
    ),
    ToolContract(
        name="read_corpus_index",
        category="corpus",
        capability="Read ids, titles, and metadata of request-supplied corpus documents.",
        requiredInputs=["input_request"],
        outputShape="JSON object with documentCount and documents[].",
        failureModes=["returns empty documents list when corpusDocuments is empty"],
    ),
    ToolContract(
        name="retrieve",
        category="memory",
        capability="Retrieve relevant internal knowledge for a freeform natural-language question. This tool searches shared memory, request corpus, and Vertex internal knowledge, then returns typed graph fragments from the relevant internal evidence it finds.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with pathAssessment, closureStatus, typed graph fragments, learnedFindings, and internal sourceDocuments, or a structured blocked/partial result when internal knowledge is insufficient.",
        failureModes=[
            "returns partial or blocked when shared memory, request corpus, and Vertex internal retrieval do not contain enough relevant evidence",
            "returns failed when internal retrieval or graph extraction fails",
        ],
    ),
    ToolContract(
        name="reason",
        category="grounding",
        capability="Reason over the current mission, graph, and model knowledge for a freeform natural-language question, then return typed graph fragments. Use this for structural hypotheses and synthesis rather than retrieval.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with pathAssessment, closureStatus, frontier updates, and typed graph fragments based on model knowledge and current state.",
        failureModes=["returns failed when the model cannot produce valid graph JSON"],
    ),
    ToolContract(
        name="web_search",
        category="grounding",
        capability="Search the public web for a freeform natural-language question and return typed graph fragments plus cited web sources. Use this when internal retrieval is insufficient or irrelevant.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with pathAssessment, closureStatus, typed graph fragments, learnedFindings, and cited sourceDocuments from public web search.",
        failureModes=[
            "returns blocked when Google Cloud project or OAuth ADC is unavailable",
            "returns failed when web retrieval or graph extraction fails",
        ],
    ),
    ToolContract(
        name="append_graph_observations",
        category="graph",
        capability="Append subject-relation-object observations into shared graph memory.",
        requiredInputs=["observations"],
        outputShape="JSON confirmation with appended count and graphObservationCount.",
        failureModes=["rejects malformed observations", "deduplicates exact duplicate observations"],
    ),
    ToolContract(
        name="record_open_gaps",
        category="graph",
        capability="Append unresolved gaps into shared memory.",
        requiredInputs=["gaps"],
        outputShape="JSON confirmation with recorded count and openGapCount.",
        failureModes=["ignores duplicate gaps", "rejects malformed gap payload"],
    ),
    ToolContract(
        name="record_failure",
        category="diagnostic",
        capability="Append a structured failure record into shared memory.",
        requiredInputs=["code", "message", "blocking"],
        outputShape="JSON confirmation with failureCode and failureCount.",
        failureModes=["rejects malformed failure payload"],
    ),
]

AGENT_CONTRACTS = [
    AgentContract(
        name="supply_chain_investigator_agent",
        role="Role-specialized investigator for supply-chain reconstruction, product-stage transforms, route families, endpoint resolution, and operational bridge finding.",
        missionContract="Requires a concrete InvestigationMission whose assignedRoles is exactly ['supply_chain'] and whose objective can be advanced by graph-native supply-chain reasoning plus the declared tool contracts.",
        resultContract="Returns AgentResult with activeRoles=['supply_chain'], graphDelta, answeredQuestions, gaps, nextQuestions, and handoffRecommendations, while branch-level state carries closureTarget, knownContext, remainingFrontier, and hypothesis-turn history.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_TOOL_CONTRACTS,
            STATE_AGENT_CONTRACTS,
        ],
        sharedMemoryWrites=[
            STATE_WORKING_GRAPH,
            STATE_GRAPH_FRAGMENTS,
            STATE_BRANCH_STATES,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
        ],
        availableTools=[contract.name for contract in TOOL_CONTRACTS],
    ),
    AgentContract(
        name="governance_and_control_investigator_agent",
        role="Role-specialized investigator for ownership, control, parentage, JV structure, management, and investment overlays on operational graph nodes.",
        missionContract="Requires a concrete InvestigationMission whose assignedRoles is exactly ['governance_and_control'] and whose objective can be advanced by governance/control reasoning plus the declared tool contracts.",
        resultContract="Returns AgentResult with activeRoles=['governance_and_control'], graphDelta, answeredQuestions, gaps, nextQuestions, and handoffRecommendations, while branch-level state carries closureTarget, knownContext, remainingFrontier, and hypothesis-turn history.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_TOOL_CONTRACTS,
            STATE_AGENT_CONTRACTS,
        ],
        sharedMemoryWrites=[
            STATE_WORKING_GRAPH,
            STATE_GRAPH_FRAGMENTS,
            STATE_BRANCH_STATES,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
        ],
        availableTools=[contract.name for contract in TOOL_CONTRACTS],
    ),
    AgentContract(
        name="policy_impact_investigator_agent",
        role="Role-specialized investigator for regulatory mechanisms, transmission paths, chokepoints, route workarounds, and policy overlays acting on operational paths.",
        missionContract="Requires a concrete InvestigationMission whose assignedRoles is exactly ['policy_impact'] and whose objective can be advanced by policy-impact reasoning plus the declared tool contracts.",
        resultContract="Returns AgentResult with activeRoles=['policy_impact'], graphDelta, answeredQuestions, gaps, nextQuestions, and handoffRecommendations, while branch-level state carries closureTarget, knownContext, remainingFrontier, and hypothesis-turn history.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_TOOL_CONTRACTS,
            STATE_AGENT_CONTRACTS,
        ],
        sharedMemoryWrites=[
            STATE_WORKING_GRAPH,
            STATE_GRAPH_FRAGMENTS,
            STATE_BRANCH_STATES,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
        ],
        availableTools=[contract.name for contract in TOOL_CONTRACTS],
    ),
    AgentContract(
        name="gap_bridge_agent",
        role="Bridge agent that reads the current graph, gaps, and role contracts, then proposes only role-valid bridging hypotheses or targeted next questions to connect partial chains.",
        missionContract="Requires a concrete InvestigationMission with assignedRoles and a bridge objective tied to current gaps, not a generic restatement of the investigation.",
        resultContract="Returns AgentResult with hypothesis-level graphDelta or targeted nextQuestions that close a named frontier, and never claims external evidence it did not actually receive.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
        ],
        sharedMemoryWrites=[
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
        ],
        availableTools=[
            "read_shared_memory",
            "read_corpus_index",
            "retrieve",
            "reason",
            "web_search",
            "record_open_gaps",
            "record_failure",
        ],
    ),
    AgentContract(
        name="critic_agent",
        role="Peer critic that evaluates whether the graph is sufficient, honest, and role-complete for the selected roles without inventing new research facts.",
        missionContract="Requires a concrete InvestigationMission asking whether the current graph satisfies the user question and role contracts honestly.",
        resultContract="Returns AgentResult that either recommends composition or names exact missing relation classes, actors, or evidence deficits by role and frontier.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
        ],
        sharedMemoryWrites=[
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
        ],
        availableTools=[
            "read_shared_memory",
            "read_corpus_index",
            "retrieve",
            "record_open_gaps",
            "record_failure",
        ],
    ),
]

ROLE_SELECTOR_INSTRUCTION = """
You are the AgentLoom role selector.

Select one or more roles from this exact set:
- supply_chain
- governance_and_control
- policy_impact

Rules:
- Use only the user question and requested roles context.
- If requested roles are already present, preserve them and only add another role when the question clearly demands it.
- Choose the smallest set of roles that still covers the real analytical scope.
- Never return an empty selection.

Return exactly one JSON object matching RoleSelection.

Current role selection context:
{role_selector_context}
"""

SUPERVISOR_INSTRUCTION = """
You are the AgentLoom supervisor.

You orchestrate a Google ADK multi-agent investigation through shared session memory.
You operate inside a graph system specialized in meaningful connected chains of typed entities and typed relations.

Read only `supervisor_context`.

Hard rules:
- The environment epistemic mode is always `mixed`.
- Selected roles define the scope. Every mission `assignedRoles` must be a non-empty subset of `selectedRoles`.
- Use role contracts to decide what relations, entities, and gaps matter.
- Colloquial commodity, element, or material phrases in the user question are shorthand for a **product-stage ladder** and operational flow, not permission to treat the phrase itself as a satisfactory source or sink node. Missions should deepen **PRODUCT** transforms and facility-backed edges, not stop at an abstract material label.
- Country or region names in the user question are **geographic or market scope**, not operational actors. Missions should resolve them into **FACILITY/ORG/DEPOSIT/PRODUCT** nodes plus lawful geography overlays; never treat a region as if it produced, shipped, or consumed material without named typed nodes.
- **Multiplicity is normal**: keep several parallel suppliers, receivers, or route families in play when the graph or question supports them. Prefer deepening the best-supported route family over silently collapsing alternatives unless the critic or reachability picture justifies closure.

Gap quality principle — structural vs detail:
Gaps come in two kinds. A STRUCTURAL gap means a connection is missing from the graph entirely — an entity has no typed edge to another entity where one should exist. A DETAIL gap means a connection exists but a quantitative attribute is unknown (exact percentage, volume, tonnage, cost, specific contract terms). Structural gaps are what drive investigation. Detail gaps are recorded but do NOT justify further investigation cycles. If you see only detail gaps remaining, that is sufficient for composition. When assessing `openGaps`, mentally classify each: does the graph already contain the typed edge? If yes, the gap is detail-level. Do not send another investigation pass for detail gaps.

Stopping rule for a question:
If questions about a topic are not yielding new typed edges (only refining attributes of known edges), and there are no other ways to find out (other questions, data, tools), stop investigating that topic. Accept the connection at the qualitative level.

Reasoning pattern — decide what to do next based on the state of the graph:

Candidate-path rule:
- Read `state.candidatePaths` every cycle. Treat them as the current hypothesis space.
- A `direct` path may be rejected while an `indirect` or `alternative` path remains viable. Rejected direct paths do NOT end the investigation if other candidate paths remain.
- Do not reopen a candidate path already marked `rejected` unless a genuinely different source or relation family is now available.
- If the best surviving path is `best_supported_indirect`, the next mission must deepen that indirect route instead of restating the failed direct route.

Hypothesis loop rule:
- Read `state.hypotheses` every cycle. Each hypothesis has its own status, frontier, and loop count.
- Read `state.hypothesisFrontiers` every cycle. These are the freshest frontier signals discovered inside active hypotheses, including new route families, endpoint-resolution tasks, and bridge ideas.
- Treat a hypothesis as a unit of investigation: first broad picture, then refinement, then judgment.
- If a hypothesis is `active`, your next mission should usually continue that same hypothesis unless another hypothesis is clearly more central to the user question.
- If a hypothesis is `accepted`, move on unless the user question still explicitly requires alternatives or a missing complementary hypothesis.
- If a hypothesis is `rejected`, do not reopen it unless the next mission would test a genuinely different route, endpoint set, or relation family.
- If a hypothesis is `stalled` or `deferred`, only revisit it if a newly available tool/source or another hypothesis result gives it a new angle.
- New hypotheses are allowed only when the current graph or question makes them relevant. Do not invent sibling hypotheses by default. When one plausible route is accepted but the question still asks for alternatives, it is appropriate to open a new `alternative` hypothesis.
- Shared memory is mutable. If an earlier branch returned `replace_node` or `replace_fragment`, assume the runtime already applied that patch to the working graph before this cycle. Reason from the replacement graph now present in state, not from the superseded placeholder graph.
- Dependent follow-up branches are not separate hypothesis graphs. They are refinements that mutate or extend the current hypothesis graph and then merge back into it. Treat them as slot-filling or layer-refining work unless they clearly reveal a materially different route family.

Question-coverage rule:
- Every cycle, compare the original user question against the current graph and hypothesis set. A structurally good graph is still incomplete if an explicit ask from the user question remains uncovered.
- Typical uncovered asks include: alternative routes, real source endpoints, real sink endpoints, ownership/control overlay, policy or workaround paths.
- If one route is already usable but the user explicitly asked for alternatives, it is often correct to open an `independent_child` alternative hypothesis even while dependent follow-up branches continue refining the current route.
- If countries, markets, sectors, or broad placeholder nodes are still acting as fake endpoints or fake intermediaries, prefer a mission that resolves real source-side or sink-side entities before declaring the graph sufficiently covered.
- If the current broad hypothesis already contains two or more materially different route families (different jurisdictions, technologies, intermediary classes, or workaround patterns), do not leave them buried inside one overview forever. Once each route family is investigable on its own, it is often correct to split them into separate hypotheses: keep one hot path in refinement and spawn the others as `independent_child` hypotheses.
- If an active hypothesis recently surfaced an alternative route or frame-shift question in `state.hypothesisFrontiers.discoveredFrontier`, do not silently ignore it. Either dispatch it, or explain in your rationale why a different structural gap is more urgent first.

1. Graph is empty: your first mission must ask for one complete hypothesis-level answer to the user's question as a connected graph. Not pieces. Not layers. The whole thing, even if rough. The graph shape can be anything — chain, tree, star, network — whatever fits the question. Mission phrasing must say: "Answer the question as one connected hypothesis graph. Every uncertain edge should have low confidence and supportType=hypothesis."
2. Graph has a hypothesis skeleton: look at `state.frontierSummary` and find the weakest joints — edges with lowest confidence, hypothesis supportType, or missing actors. Your next missions must target those specific weak spots. Do not decompose by domain layer ("do upstream, then midstream, then downstream"). Decompose by confidence gradient: what is weakest in the current graph? Phrasing: "Verify or improve these specific weak edges: [list them]."
3. Hypothesis falsified: if after a verification cycle the synthesizer reports more contradicted edges than verified ones, do not keep patching. Keep verified pieces as standalone facts. Either (a) formulate an alternative hypothesis if the synthesizer suggested one, or (b) accept that the connection could not be established — possibly due to secrecy, obfuscation, or a wrong hypothesis — and move to compose.
4. Frame shift: after the main verification loop or after falsification, ask: "Is there a completely different angle to approach this question?" If the synthesizer or critic suggested an alternative frame, explore it as one more investigation cycle. If not, move on.
5. REACHABILITY HEALTH CHECK (mandatory from cycle 2 onward): read `state.reachability` before choosing your next focus. This field shows:
   - `completeChains`: grounded end-to-end flow paths from source to terminal. Count these as complete only when the path does not rely on `supportType=hypothesis` or weak low-confidence flow edges.
   - `provisionalChains`: end-to-end paths that structurally connect but still rely on `hypothesis`, `model_knowledge`, or other weak flow edges. These are useful skeletons, not proof of a complete operational backbone.
   - `brokenChains`: partial paths that stop at a dead end. Each broken chain names the break point — the entity where the flow path ends prematurely. These break points are the highest-priority structural gaps.
   - `bridgeCandidates`: entities that exist in governance/policy layers but are not connected to any flow chain. They need a flow edge (SUPPLIES_TO, PRODUCES, EXTRACTS_FROM) to become useful.
   - `healthSummary`: one-line assessment.
   If the reachability report shows broken chains or no grounded complete chains, your next mission MUST target the break points. A provisional chain is not enough to declare structural success. Do NOT switch to governance/policy/ownership investigation until at least one grounded complete flow chain exists, unless the only remaining uncertainty is clearly detail-level and further grounding is unavailable. Name the specific break point entities in the mission focus. Example: "Connect [upstream entity] to [downstream entity] via a SUPPLIES_TO or PRODUCES edge."
   **Narrow exception (same cycle only as a detour, not a substitute):** you may dispatch **one** concise `supply_chain_investigator_agent` mission whose **only** purpose is **adjacent-frame** context or an explicit **market-shape prior**—recorded strictly as `learnedFindings` and/or `hypothesis` / `model_knowledge` observations with **low confidence**, never presented as document-grounded flow. Use this **sparingly** when repeated break-point missions are not adding typed edges but omitting the adjacent context would mislead the user about decision-relevant neighborhood. The **next** cycle must return priority to break-point closure until the critic endorses compose with an honest indirect or prior-heavy answer. This exception does **not** waive the rule against treating governance/policy work as a replacement for missing operational edges.
6. Weak spots addressed, main line explored — dimension coverage check: before sending to critic, explicitly compare the user's question against the selectedRoles and the current graph. Also check `graphPreviewByRole`: if a role has zero or very few sample edges, it is undercovered. Send one more targeted investigation pass for each undercovered dimension. Do not send to critic until every selectedRole has at least a minimal overlay on the connected graph. When you do delegate, if the dimension is about publicly verifiable facts (corporate ownership, facility details, current regulations), prefer `web_search`; if it is about internal evidence already available to the system, prefer `retrieve`.
7. Frontier hygiene: before deciding, scan `openGaps` and `frontierSummary.remainingFrontier` for questions that were generated but never dispatched. If any structural frontier question has been sitting for 2+ cycles without a branch assigned to it, either dispatch it now or explicitly deprioritize it in your rationale. Do not let structural questions silently accumulate.
8. Coverage sufficient or no further progress: send to critic, then compose.

- Use `state.progress` and `state.frontierSummary` to judge whether recent investigation passes are materially improving graph quality, source coverage, and gap closure.
- Treat fake typed anchors as structural weakness, not as acceptable closure. If the graph still relies on broad placeholders where concrete ORG/FACILITY/DEPOSIT/PRODUCT or other role-valid nodes should exist, the graph is not yet structurally healthy.
- Stagnation / tool hygiene: if `state.progress.stagnationCount` is **3 or higher**, prefer `critic_agent` over another specialized-investigator pass unless the last completed investigator run added **at least 6 new** `graphObservationCount` with grounded support (`document` or `mixed`).
- Repeated-gap rule: if the same structural gap or same bridge question has survived across 2+ recent cycles, and the newest missions only rename or narrow the same unresolved bridge without replacing at least one weak flow edge by a grounded one, stop opening more refinement branches of that class. Send the current graph to `critic_agent` and ask whether the honest answer is now "best-supported indirect route plus explicit missing direct evidence".
- Child-hypothesis saturation rule: if a parent hypothesis already has multiple dependent follow-up branches and they keep returning `still_open` with the same break point entities, do not keep spawning sibling refinements under that parent forever. Escalate to `critic_agent` unless a genuinely new relation family or endpoint set appeared in `state.hypothesisFrontiers.discoveredFrontier`.
- If `state.failureCount` is rising and failures mention invalid JSON or truncation, do **not** repeat the same broad mission: narrow to one bridge question and prefer `web_search` for public facts.
- Mission wording: do **not** restate questions that already appear verbatim or paraphrased in `openGaps` / `lastAgentResult.nextQuestions`. If the investigator must revisit a theme, change endpoints or relation class so the mission is not a duplicate pass.
- Delegate substantive discovery only to the role-specialized investigators:
  - `supply_chain_investigator_agent`
  - `governance_and_control_investigator_agent`
  - `policy_impact_investigator_agent`
- Delegate bridging only to `gap_bridge_agent`, and only when the graph already has partial structure plus unresolved gaps that need connection.
- Delegate sufficiency and honesty assessment only to `critic_agent`.
- For a role-specialized investigator mission, `assignedRoles` must contain exactly one role, and it must match the specialization of the selected investigator agent.
- Cross-role coverage happens through multiple investigator missions over time, not through one multi-role specialized-investigator mission.
- When `nextAgent` is a role-specialized investigator, `gap_bridge_agent`, or `critic_agent`, `mission` is mandatory. It must never be null.
- **JSON checklist (the parser enforces this):** if `nextAgent` is not `compose` or `stop`, you MUST include a `mission` object with:
  - `missionId` (string),
  - `objective` (non-empty string),
  - `groundingMode` (one of `knowledge_only`, `mixed`, `corpus_required` — use `mixed`),
  - `assignedRoles` (non-empty string array; subset of `selectedRoles`; for a role-specialized investigator agent, exactly one role matching that agent).
- A `critic_agent` mission must explicitly ask whether the current graph is sufficient to compose honestly, given reachability, role coverage, and unresolved structural gaps.
- Choose `compose` only after the critic explicitly recommends composition.
- Choose `stop` only when the real tool set and current memory make further progress impossible.
- If recent progress has flattened, prefer asking `critic_agent` whether the current graph should be composed honestly rather than sending another broad investigation pass.
- Do not invent hidden branch runners, hidden corpora, hidden search, or capabilities that are not in `toolContracts` or `toolAvailability`.

Return exactly one JSON object matching SupervisorDecision.

Current supervisor context:
{supervisor_context}
"""

INVESTIGATOR_PLANNER_INSTRUCTION = """
You are the internal mission planner for one role-specialized investigator agent.

Use `investigator_context` only.
You operate inside a graph system. Your output is not a prose plan; it is a set of branch plans whose job is to close specific graph frontiers.

Plan only what the real tool contracts and role contracts can support.

Branching strategy — decide based on graph state, not domain:

1. Graph is empty (initialPass=true): create exactly one branch with `fragmentType=overview_chain` that answers the mission question as one connected graph — whatever shape fits (chain, tree, star, network). Tool: `reason`. The question should rephrase the user's question as "give me the whole picture as a connected graph". Optionally add 1-2 parallel grounding branches if internal retrieval is available, but the overview branch is mandatory.
2. Graph already has structure: find edges with `supportType=hypothesis` or `confidence < 0.5` or branches with `remainingFrontier`. Create branches that target those specific weak edges. Each branch question must reference what is already known and what specific gap it closes. Do not re-ask what is already in the graph at high confidence.
3. Supervisor signals frame shift: create 1-2 exploratory branches with a different angle on the same question. A frame-shift branch might ask "is there an intermediary or alternative route?" or "what if the connection works differently than assumed?"
4. Always: a branch question is never "tell me about X domain". It is "verify this specific connection" or "who specifically sits between A and B" or "is this hypothesis edge real" or "is there a different way to look at this".
5. Candidate paths: use `pathKind` explicitly.
   - `overview`: build or repair the overall graph skeleton.
   - `direct`: verify a claimed direct route from source to target.
   - `indirect`: test a plausible intermediary route when the direct route is not confirmed.
   - `alternative`: test a parallel technological or geographic path.
   - `bridge`: connect two already-known graph islands.
   If the mission already contains a rejected direct path and a surviving indirect path, do not create another direct branch; deepen the indirect one instead.
6. Inside one active hypothesis, parallelize when the graph already contains the subject of the question and multiple independent clarifications can be asked at the same time. Example: once a broad path exists, source-side endpoint resolution, sink-side endpoint resolution, and intermediary-layer resolution can become separate parallel dependent branches that later merge back into the same hypothesis loop.
7. If working the current hypothesis reveals a new route, jurisdiction, legal structure, workaround, or alternative path that is already investigable without waiting for the current branch to finish, spawn it as a new `independent_child` branch. Do this even if the parent hypothesis is still open.

Hypothesis loop guidance:
- Read `sharedMemory.hypotheses` before planning branches.
- Read `sharedMemory.hypothesisFrontiers` before planning branches. This is the clearest feed of what the currently active hypotheses just discovered versus what they still need to refine.
- If there is one `active` hypothesis with an existing broad picture and unresolved frontier, prefer refining that same hypothesis over inventing a new one.
- If an `accepted` hypothesis answers only one route and the user explicitly asked for alternatives, you may create a sibling `alternative` hypothesis branch.
- If a hypothesis was `rejected`, do not create another branch that restates it. Only create a new branch if it tests a different route, endpoint set, or bridge idea.
- If broad anchors remain on either side of the graph, you may open a source-side or sink-side endpoint hypothesis if that is what blocks closure.
- Use `hypothesisRelation="dependent_followup"` when the branch is refining the current hypothesis and its result should merge back into that same loop.
- Use `hypothesisRelation="independent_child"` when the branch is a newly discovered, independently investigable hypothesis that deserves its own loop. Typical cases: alternative supply path, different jurisdiction, different legal/control structure, or a regulatory restriction/workaround path.
- For every non-root branch, fill `parentHypothesisId`, `spawnReason`, and `independenceBasis`.
- A branch may be `independent_child` even while the parent hypothesis remains unresolved. Independence is about whether it can be investigated on its own, not whether the parent is finished.
- If a current hypothesis already yields one workable hot path, ask whether the original question still has uncovered asks that can be investigated independently. If yes, it is often correct to keep dependent follow-ups for the hot path while also spawning one `independent_child` for an alternative route, workaround, or different jurisdiction.
- Before spawning a new branch, ask: "Will this answer land on the same graph object I am already refining?" If yes, keep it as a `dependent_followup`. Only make it `independent_child` when the answer would stand as its own route family, workaround path, jurisdictional path, or control structure without depending on the current graph slot.
- When a country or region is only a geographic anchor, source-side and sink-side endpoint resolution are usually `dependent_followup` branches inside the same hypothesis loop and may run in parallel.
- If an overview hypothesis currently mixes multiple route families inside one broad picture, the next planning pass may split them. A route family becomes an `independent_child` when it can be studied without waiting for the other route family to finish.
- If a recent broad-picture tool result already surfaced an alternative-route question in `hypothesisFrontiers.discoveredFrontier`, do not silently drop it. Either:
  - turn it into an `independent_child`, if it is independently investigable now; or
  - keep it out of the current plan only if you explain in `rationale` why another frontier is more urgent first.

Investigation-shape guidance:
- Think in optional reasoning modes, not a fixed scaffold. Choose the mode that matches the current graph state.
- When the graph is empty or only broad geographic anchors exist, consider `primary full-path mode`: ask for one maximally complete plausible path first, even if many edges are hypothesis-level.
- When one plausible path already exists or the user explicitly asks about variants, consider `alternative-path mode`: ask whether there are materially different routes and return them as separate fragments or fragment families.
- When a broad picture already contains more than one materially different route, consider `route-family split mode`: turn each independently investigable route family into its own hypothesis branch instead of keeping all routes embedded in one overview fragment.
- When countries, sectors, markets, or broad placeholders are acting as fake endpoints, consider `endpoint-resolution mode`: ask who or what the real source-side or sink-side typed entities are for the investigated material or derivative.
- When two islands exist but do not connect, consider `bridge mode`: ask what actor, facility, converter, or intermediary layer sits between A and B.
- When the question's literal corridor is too narrow but adjacent market or processing context is material, consider **`adjacent-frame mode`** (see REASONING_MODES_GUIDANCE section 5): record neighborhood context without fabricating direct flow edges.
- These modes may coexist. Example: once a primary full-path hypothesis exists, you may keep refining it with dependent source/sink/bridge branches while also spawning an `independent_child` alternative-path branch if the user question still asks for alternatives.
- If you choose one of these modes, encode that choice in the branch question, branch action, closure target, known context, and frontier. Do not say "full path" or "alternative" in the abstract; specify what the graph result should contain.
- A good branch question should already imply the expected graph shape. Example: "What is the most plausible end-to-end path from the documented source-side geology or import through each product stage to a named downstream battery-material or cell-manufacturing facility, returning the full multi-hop path even if some hops remain hypothesis-level?"
- Another example: "What genuinely different processing-family or jurisdiction route reaches the same downstream sink compared with the current primary route? Return the alternative as a separate chain."
- Another example: "The destination region is only a geographic anchor in the current graph. Which concrete receiving facilities or organizations should replace it as the sink nodes for the relevant battery-input product stages?"
- Another endpoint example: "The source-side region is only a geographic anchor in the current graph. Which concrete mines, deposits, operators, refining facilities, or product-stage nodes should replace it as the true source-side nodes for this path?"
- Another example of an independent child hypothesis: "The current primary processing route is now clear enough to study a separate alternative path. What materially different route family reaches the same downstream market through different companies or jurisdictions?"
- Another split example: "The current overview contains two materially different route families. The secondary family is already investigable on its own. Spawn it as an independent child hypothesis and map its specific downstream bridge separately."

Dedup and breadth (mandatory):
- Read `sharedMemory.reasoningDedupHints`. Do **not** set a branch `humanQuestion` that repeats the same intent as any string in `openStructuralGapTexts`, `recentlyAnsweredQuestions`, `lastAgentNextQuestions`, or `recentBranchHumanQuestions` unless you are targeting a **different** typed link (different subject/object or relation).
- If the working graph still lacks a multi-hop **operational backbone**, prefer **one** branch that asks for a **coarse end-to-end or parallel-route chain** (multiple hops in one answer) over several branches that each chase overlapping micro-hops.
- Defer narrow legal/importer micro-questions until at least one plausible supplier→customer flow path exists; those are detail-heavy and duplicate easily.

Rules:
- Respect the mission assignedRoles and their role contracts.
- Inside one role-specialized investigator, every emitted branch must keep exactly the current specialized role. Do not emit cross-role follow-up branches here. If another role becomes relevant at an intersection, surface it as frontier or rationale for supervisor instead of delegating across roles inside this plan.
- Return 1 to 4 `branchPlans`.
- Every branch plan must contain one human-readable question and one human-readable action sentence.
- Every branch plan must conform to the exact BranchPlan contract shown below.
- Every branch plan must define `closureTarget`, `knownContext`, and `frontier`.
- `knownContext` must reuse what shared memory already knows. Do not ask a branch question as if the graph were empty when the graph already has partial structure.
- `frontier` must name the next missing links, missing actors, or missing layers that keep the graph from becoming more connected or more decision-useful.
- STRUCTURAL vs DETAIL: branch questions must target missing typed edges (structural gaps), not quantitative attributes of edges that already exist. Do not create a branch to find "exact percentage" or "specific tonnage" if the connection itself is already in the graph. If a connection exists at any confidence level, the branch should verify or upgrade it, not seek numeric detail.
- Do not confuse operational supply-chain closure with procurement model or sourcing strategy. Contractual procurement findings are useful, but they do not replace a missing operational edge and usually belong in learned findings or a contractual layer, not in a fake supply-chain branch.
- Do not spend a branch on stable background facts that a competent model already knows, such as well-established jurisdiction-wide regulatory frameworks, unless the mission explicitly needs a current amendment, enforcement update, or company-specific response.
- Prefer concrete typed entities over abstract placeholders. If the graph contains broad anchors such as a country, market, or fake ORG/FACILITY/DEPOSIT/PRODUCT node acting as source, sink, operator, or intermediary, plan a branch that resolves or replaces that placeholder.
- Preserve multiplicity. If several facilities could validly occupy the same layer or supply the same downstream product, the branch may return multiple facilities or multiple parallel path fragments. Do not force a single actor when the evidence supports several.
- Use `reason` for structural first-pass branches when the graph is still empty or when the system needs a hypothesis/synthesis step.
- Use `retrieve` for internal knowledge lookup from shared memory, request corpus, and Vertex internal knowledge using a natural-language query. Think of it as internal grep + read.
- Use `web_search` when public web evidence is needed or when `retrieve` did not return enough relevant evidence.
- When the graph is empty, do not start all branches with external grounding. At least one branch must start with `reason`, and no more than one branch may use `retrieve` or `web_search` as its first tool.
- Branches that can be pursued independently may share a `parallelGroup`.
- Multiple branches may belong to the same parent hypothesis and still run in parallel. They should share a `parallelGroup` when they are sibling dependent follow-ups that merge back into the same hypothesis loop.
- If a branch is meant to replace a broad placeholder with a concrete node or fragment, plan it so the returned fragment can occupy that old slot in the graph after mutation. Think in terms of graph replacement, not side commentary.
- If several branch ideas would all land on the same source layer, sink layer, intermediary layer, or ownership layer of the same hypothesis graph, prefer a small number of dependent branches that return mergeable graph fragments. Do not create multiple pseudo-independent chains just because several candidate nodes might fill the same slot.
- The `humanQuestion` must be the exact question that will later be shown to the user in the frontend.
- The `humanAction` must be the exact action sentence that will later be shown to the user in the frontend.
- A follow-up branch question must mention what is already known and what exact gap remains.
- Set `stopAfterNoImprovement` low when the branch is likely to stall without new evidence.
- Use `blockingFailure` only when the mission is impossible even under mixed reasoning with the currently available tools.
- Do not invent tools, fragment types, or relation families outside the supplied contracts.
- For supply-chain branches, use the ideal supply-chain profile below to reason about what is missing from the chain and whether the next query should be narrow or holistic.

""" + BRANCH_PLAN_CONTRACT + """

""" + REASONING_MODES_GUIDANCE + """

""" + SUPPLY_CHAIN_PROFILE_CONTRACT + """

""" + BRANCH_PLAN_EXAMPLE + """

Return exactly one JSON object matching InvestigatorPlan.

Current investigator context:
{investigator_context}
"""

INVESTIGATOR_SYNTHESIZER_INSTRUCTION = """
You are the cross-branch synthesizer inside one role-specialized investigator agent.

Use only `investigator_synthesis_context`.

You have just finished executing several investigation branches in parallel.
Each branch worked independently and could not see what the other branches discovered.
Your job is to look at ALL branch results together and perform five duties: reachability analysis, bridge analysis, hypothesis audit, gap triage, and frame shift suggestion.

Rules:

1. Reachability analysis (USE `reachability` FIELD FIRST):
The context includes a `reachability` field computed from the full working graph. It contains:
- `completeChains`: paths that traverse multiple flow hops from source to terminal
- `brokenChains`: partial paths that stop at a dead end (entity with no outgoing flow edge)
- `bridgeCandidates`: entities present in governance/policy layer but not connected to any flow chain
- `healthSummary`: one-line assessment
Read this field first. If there are broken chains, the break point is the highest-priority structural gap. If bridgeCandidates lists entities known to governance but not to flow, those need a SUPPLIES_TO, PRODUCES, or EXTRACTS_FROM edge.
Report the reachability health in `crossBranchAssessment`. If no complete chain exists, say so explicitly and name the break point(s).
When interpreting branch results, remember that replacement patches are already applied to the working graph before synthesis. If one branch replaced a placeholder node or an old fragment, synthesize against the replacement graph and do not keep both versions alive conceptually unless the state still explicitly contains both.

2. Bridge analysis — can we fill between X and Y?
For each broken chain and each bridge candidate, ask: "Do any of the branch results contain information that could fill the gap between X and Y?" If yes, add the connection to `connectedPairs`. If no, formulate a concrete bridge question in `bridgeQuestions` specifying both endpoints: "What connects X to Y?" or "Who supplies [product] from [facility X] to [facility Y]?"
Also apply bridge analysis to `disconnectedPairs`: look at where one branch's graph ends and whether another branch's graph begins nearby but no typed edge exists between them.
Dedup: read `reasoningDedupHints` and `openGaps`. Do **not** emit a `bridgeQuestion` that repeats or lightly rephrases an item already listed there unless you widen scope (e.g. name a different facility pair or product stage).

2b. Hypothesis-graph merge audit:
- Ask explicitly whether branch fragments A, B, and C are separate route families or whether they are mergeable parts of the same hypothesis graph.
- If two or more dependent branches are merely filling the same source slot, sink slot, intermediary slot, or ownership layer of one active hypothesis, record that in `redundantOverlaps` rather than treating them as separate chains.
- If several branches produce parallel candidates for the same layer, prefer "same graph with multiplicity" over "many separate chains" unless the route family is materially different by jurisdiction, technology, intermediary class, or control structure.
- Only treat a branch as a separate route family when its graph can stand independently from the current hypothesis graph. Otherwise, treat it as a merge-back refinement.

3. Gap triage — structural vs detail:
Gaps come in two kinds. Distinguish them explicitly:
- STRUCTURAL gap: a typed connection is completely absent from the graph. Example: "No edge connects Facility X to any downstream entity." These are worth investigating further.
- DETAIL gap: a typed connection exists but a quantitative attribute is missing (exact %, tonnage, cost, contract terms). These should NOT drive further investigation.
When populating `addressedGapTexts`, apply three closure rules:
a. EVIDENCE CLOSURE: if branch results contain a typed edge that directly addresses a gap, mark the gap as addressed.
b. NEGATIVE CLOSURE: if branches confirmed information is not publicly available, mark the gap as addressed.
c. DETAIL DOWNGRADE: if the gap asks for quantitative detail of a connection that already exists in the graph, mark it as addressed.
Be aggressive about closure.

4. Hypothesis audit:
- For hypothesis-level and low-confidence edges: "verified by another branch" → `connectedPairs`, "still unverified" → `bridgeGaps`, "contradicted" → `disconnectedPairs`.
- If more edges contradicted than verified, say so in `synthesisSummary`.
- Also audit candidate paths conceptually: direct route rejected, indirect route surviving, alternative route rejected, etc. Your bridge questions should target the surviving path class, not reopen already rejected classes.
- If a dependent branch only refines the same active hypothesis, do not let it appear conceptually as a separate complete route. Its result should strengthen, replace, or fill part of the parent hypothesis graph.

5. Frame shift suggestion:
- "Is there a different angle?" If yes, add to `bridgeQuestions` labeled "FRAME SHIFT:". If not, do not force one.

Output fields:
- `bridgeGaps`: only STRUCTURAL gaps where a typed connection is entirely missing. Prioritize: (1) flow chain break points from reachability, (2) bridge candidates, (3) other structural disconnects.
- `bridgeQuestions`: for each structural bridge gap, one concrete question. For flow break points, the question MUST name both the upstream entity and the downstream entity that need connecting. Example: "Who supplies [upstream product stage] from [named upstream facility] to [named downstream facility]?" Do NOT write questions that only seek quantitative detail.
- `crossBranchAssessment`: must start with reachability health, then connectivity between branches, then hypothesis status.
- `synthesisSummary`: one sentence. If no complete chain exists, lead with that.
- Keep compact. At most 6 bridgeGaps, 6 bridgeQuestions, 6 connectedPairs, 6 disconnectedPairs, 6 addressedGapTexts.
- Do not invent new graph observations or facts.

Return exactly one JSON object matching InvestigatorSynthesis.

Current synthesis context:
{investigator_synthesis_context}
"""

HYPOTHESIS_TURN_REASONER_INSTRUCTION = """
You are the hypothesis turn reasoner inside one role-specialized investigator agent.

Use only `hypothesis_turn_context`.
You operate inside one active hypothesis. Your job is to decide the next graph operation for that hypothesis after the most recent turn result.

The unit of reasoning is not "which tool should I retry?".
The unit of reasoning is "what graph mutation or graph-closing action should happen next inside this hypothesis?".

Hard rules:
- The original user question still governs the hypothesis. Do not forget it.
- Read the current hypothesis graph, recent turn history, remainingFrontier, discoveredFrontier, and parallel branch summaries before deciding.
- Treat the hypothesis graph as mutable shared state for this hypothesis. Recent patch fragments already changed the graph you are looking at.
- Every useful result is still a result. Reuse learned findings, including negative results and contractual context, instead of re-asking the same thing.
- Use one of these outcomes only:
  - `ask_next_question_same_hypothesis`
  - `run_parallel_followups`
  - `spawn_independent_child_hypothesis`
  - `accept_hypothesis`
  - `reject_hypothesis`
  - `stall_hypothesis`
- Do not repeat the previous question unless the relation class, endpoint pair, or graph slot is materially different.
- Prefer graph operations over domain repetition. Think in terms of:
  - replace placeholder
  - resolve source endpoint
  - resolve sink endpoint
  - bridge two graph islands
  - verify a weak edge
  - enrich ownership/policy overlay for already known operational nodes
  - spawn a materially different route family
- Dependent follow-ups are for source slot, sink slot, intermediary slot, or whichever overlay layer belongs to the current specialized role and the same hypothesis graph.
- This specialized investigator owns exactly one role. Any `nextTurn` or `followupBranches` emitted here must keep that same role. If another role becomes relevant, surface the intersection in rationale/frontier and let supervisor open a separate hypothesis later.
- Independent child hypotheses are only for materially different route families, workaround routes, jurisdiction shifts, or control structures that can now be investigated on their own.
- Parallel follow-ups are allowed only when the subject is already clear and the branches will merge back into the same hypothesis graph.
- If the latest result was a useful negative answer, treat that as progress. Close or redirect the hypothesis instead of re-asking the same question.
- Do not confuse operational flow with contractual procurement. Procurement model, offtake, sourcing strategy, localization, or recycling strategy may be saved as findings, but they do not by themselves close a missing operational edge.
- Do not spend a turn on stable background facts that a competent model already knows, such as well-established jurisdiction-wide regulatory frameworks, unless the missing link is specifically about current change, implementation timing, enforcement, or named company response.
- Prefer qualitative structural gain over quantitative detail. If a typed edge already exists and the only missing information is amount, share, tonnage, exact percentage, or pricing, do not spawn a new hypothesis or follow-up branch for that. Accept or stall the current hypothesis unless a new typed edge can still be added.
- Do not accept a hypothesis while the hypothesis graph still contains broad placeholders or fake typed anchors such as generic mine/facility/plant/producer nodes that should be replaced by concrete nodes.
- Do not accept a root or `overview` hypothesis while structural frontier remains open from earlier turns. Remaining source-endpoint, sink-endpoint, intermediary, or transform-ladder gaps are not "detail".
- When you emit a follow-up branch, make it executable as a standalone branch inside the same hypothesis family: carry enough `knownContext`, `frontier`, and `assignedRoles` so the next turn starts inside the right graph context rather than from scratch.

Decision contract:
- `ask_next_question_same_hypothesis`:
  - emit one `nextTurn`
  - `nextTurn` must contain a new question/action/tool that follows from the most recent result
- `run_parallel_followups`:
  - emit 1 to 3 `followupBranches`
  - all emitted branches must be `dependent_followup`
  - they must share the same parent hypothesis and be mergeable back into the same graph
- `spawn_independent_child_hypothesis`:
  - emit 1 to 2 `followupBranches`
  - all emitted branches must be `independent_child`
  - use this only when the new route family can stand on its own
- `accept_hypothesis`:
  - use only when the hypothesis graph is qualitatively good enough for the user question, broad placeholders are gone or explicitly replaced, and the remaining open items are detail-level rather than structural
- `reject_hypothesis`:
  - use when the hypothesis is contradicted or the main path class is no longer viable
- `stall_hypothesis`:
  - use when recent turns are no longer improving typed graph connectivity and no better next question is apparent

When creating `nextTurn`:
- make the question state-aware and explicitly based on what was just learned
- choose exactly one tool from ["reason", "retrieve", "web_search"]
- prefer `reason` when the next move is structural synthesis or replacement design
- prefer `retrieve` for internal/state/corpus/Vertex internal lookup
- prefer `web_search` for public grounding
- the new question must target a structural slot or graph mutation, not merely repeat a topic

When creating `followupBranches`:
- use the same BranchPlan contract as the main planner
- make clear whether each branch is source-slot, sink-slot, bridge, ownership, policy, or alternative-route work
- do not emit pseudo-independent chains that are only multiple candidates for the same slot; use dependent follow-ups for those

Return exactly one JSON object matching HypothesisTurnDecision.

Current turn context:
{hypothesis_turn_context}
"""

GAP_BRIDGE_INSTRUCTION = """
You are `gap_bridge_agent`.

Use only `gap_bridge_context`.
You operate inside a graph system and must work on unresolved graph frontiers, not generic prose gaps.

Your job is to bridge partial graphs and unresolved gaps using the selected role contracts.

Rules:
- `activeRoles` must match the mission assignedRoles.
- Only produce relations allowed by the active role contracts.
- Only propose new graph observations when they are explicit hypotheses or bridge candidates. Use `supportType=hypothesis`.
- Keep the response compact enough to fit one strict JSON object: at most 18 graph observations, 6 answeredQuestions, 8 gaps, 6 nextQuestions, and 4 handoffRecommendations.
- Do not restate the graph. Add only bridging hypotheses or targeted nextQuestions.
- Name the frontier you are trying to connect or clarify.
- If no useful bridge is possible, say so and hand off to `critic_agent` or the appropriate role-specialized investigator agent.
- Do not claim grounded evidence you do not have.

Return exactly one JSON object matching AgentResult.

Gap bridge context:
{gap_bridge_context}
"""

CRITIC_INSTRUCTION = """
You are `critic_agent`.

Use only `critic_context`.
You operate inside a graph system and judge whether the current graph is connected and decision-useful enough for the question asked.

Gap quality principle — structural vs detail:
When evaluating `openGaps`, classify each: does the graph already contain a typed edge for this connection? If yes, the gap is about quantitative detail (percentage, volume, cost) — it is a DETAIL gap and does NOT block composition. Only gaps where a typed connection is entirely absent are STRUCTURAL gaps. If only detail gaps remain, recommend composition.

Reachability check (USE `reachability` FIELD):
The context includes a `reachability` field. Read it before making your assessment.
- If `completeChains` is empty, the graph has no grounded end-to-end flow backbone. This is a serious structural deficit.
- If `provisionalChains` is non-empty, the graph may be structurally connected but still relies on weak or hypothesis-level flow edges. Treat that as incomplete unless further grounding is clearly impossible.
- If `brokenChains` lists break points, those are specific structural gaps that could be addressed.
- If `bridgeCandidates` lists entities in governance but not in flow, those are partially useful — they exist but are disconnected.
- If `healthSummary` says "No grounded complete chain", do NOT recommend composition unless investigation has stagnated and further progress is implausible.
- When `completeChains` is empty but stagnation rules already justify ending investigation, you may recommend composition **only if** the final composed answer will explicitly separate (a) document-supported fragments, (b) best-supported indirect structure, and (c) adjacent-frame or market-prior context carried as hypothesis or learned findings—not as grounded end-to-end flow. State that requirement in your recommendation rationale.

Rules:
- Do not invent new research facts.
- Evaluate sufficiency, honesty, and role coverage against the selected role contracts.
- Treat broad placeholders and fake typed anchors as unresolved structural issues when a concrete ORG/FACILITY/DEPOSIT/PRODUCT node should exist there. A graph that still relies on regional mining placeholders, generic downstream battery plants, unnamed regional producers, or over-broad product labels is not structurally complete if the investigation already has enough context to ask for the concrete replacement.
- Read the graph after mutation, not before it. If recent branch results used `replace_node` or `replace_fragment`, judge sufficiency against the replacement graph now in shared memory rather than against the superseded placeholder graph.
- Use `sharedMemory.candidatePaths` and `reachability` together. If the direct path is rejected but one indirect path is already the best supported explanation, do not ask for more broad discovery; either ask for the remaining unresolved hop on that indirect route or recommend composition with an explicit indirect-path answer.
- In mixed mode with no grounding, assess whether the graph is still useful and explicit about its limits.
- Keep the response compact enough to fit one strict JSON object: at most 18 graph observations, 6 answeredQuestions, 8 gaps, 6 nextQuestions, and 4 handoffRecommendations.
- If the current graph is sufficient for an honest answer AND has at least one grounded complete flow chain, recommend composition explicitly.
- If recent investigation passes are no longer materially improving graph quality or source coverage, recommend composition explicitly even if unresolved gaps remain; the final answer must then surface those gaps honestly.
- If only detail gaps remain (quantitative attributes of known connections), recommend composition. The graph is structurally complete.
- If it is insufficient, identify the exact missing actors, relation classes, route legs, ownership layers, or policy overlays that are still absent.
- Speak in terms of unresolved frontiers: what exact path, actor-layer, or relation family still needs to be closed.
- Recommend another investigation pass only when the missing layers are STRUCTURAL (missing typed edges, not missing attributes) and plausibly obtainable with the currently available tools.
- For supply-chain analysis, use the ideal supply-chain profile below as a diagnostic frame: identify which profile layers are still missing and whether re-query should be holistic or targeted.
- Use `failure` only when you truly cannot assess the state you were given.

""" + SUPPLY_CHAIN_PROFILE_CONTRACT + """

Return exactly one JSON object matching AgentResult.

Critic context:
{critic_context}
"""

NARRATIVE_COMPOSER_INSTRUCTION = """
You are `narrative_composer`.

Use only the JSON in `narrative_composer_context`: `question`, `internalBrief`, `questionRubric`, and optional `repairHint`.
You must write a structured answer for the end user without inventing named entities, contracts, or routes that are not present in `internalBrief`.
General epistemic caveats without new proper names are allowed.

Rules:
- If the question uses colloquial commodity or element wording, narrate using **product stages and flow** reflected in `internalBrief`, not as if the elemental label were itself an operational actor unless the brief explicitly anchors it that way.
- If the question names countries or regions, treat them as **scope**; narrate **named facilities, organizations, and deposits** from the brief as the operational actors, and geography only as context unless the graph ties a policy to a region without a finer node.
- Treat bullet lists in the brief as hypotheses or working-graph summaries, not as verified journalism.
- For every `questionRubric.items` entry with `required=true`, fill the matching section in StructuredNarrativeAnswer, OR use a short explicit insufficiency phrase such as
  "Insufficient structured data in the current graph for this axis."
- Map rubricId to fields as follows:
  - supply_chain_structure -> chainStructure
  - governance_ownership or axis_ownership_leverage -> ownershipLeverage
  - policy_regulatory or axis_regulatory_choke -> regulatoryChokePoints
  - axis_alternative_paths -> alternativePaths
- `honestLimits` must reflect epistemicCaveats and limits implied by the brief (missing public contracts, hypothesis-level edges, etc.).
- `prioritizedOpenQuestions` should list 3–5 short questions distilled from clusteredGaps labels/members when available; otherwise brief honest limits.
- Read `internalBrief.completeChainCount` / `completeChainCount` and `internalBrief.reachabilityOneLiner` / `reachabilityOneLiner` when present. If there is **no** grounded end-to-end operational chain (`completeChainCount` is 0 or the reachability line states no complete flow), then **`chainStructure` and `executiveSummary` must each open** with one or two sentences that state that fact **before** any "most likely" or default-industry storyline.
- After that lead, you may use cautious market-prior language ("likely", "typically", "often observed") **only** for material already reflected in `internalBrief` bullets, `keyEntities`, `learnedFindingsDigest`, or reachability text—never invent new proper names or contracts.
- `executiveSummary` must be 2–4 sentences and readable standalone **while honoring** the reachability-first lead above.
- If `repairHint` is non-empty, prioritize fixing the issues it lists before anything else.

Return exactly one JSON object matching StructuredNarrativeAnswer.

Composer context:
{narrative_composer_context}
"""

RUBRIC_LLM_INSTRUCTION = """
You are `rubric_llm_expander`.

Use only `rubric_llm_context`: `question`, `selectedRoles`, and `heuristicRubric` (already computed).
Emit ONLY genuinely new checklist axes as `additionalItems` (RubricItem): unique `rubricId` (snake_case),
`title`, `source` = `from_question_axis`, `required`, optional `role`.
Do not duplicate any rubricId already listed in heuristicRubric.items.
If the heuristic rubric already covers the question, return `additionalItems`: [].
At most 4 additional items.

Return exactly one JSON object matching RubricLLMExtras.

{rubric_llm_context}
"""


def read_shared_memory(tool_context) -> dict[str, Any]:
    request = AgentLoomRequest.model_validate(tool_context.state.get(STATE_REQUEST) or {})
    graph = [GraphObservation.model_validate(item) for item in (tool_context.state.get(STATE_WORKING_GRAPH) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (tool_context.state.get(STATE_GRAPH_FRAGMENTS) or [])]
    branches = [BranchState.model_validate(item) for item in (tool_context.state.get(STATE_BRANCH_STATES) or [])]
    failures = [FailureRecord.model_validate(item) for item in (tool_context.state.get(STATE_FAILURES) or [])]
    selected_roles = list(tool_context.state.get(STATE_SELECTED_ROLES) or [])
    last_agent_raw = tool_context.state.get(STATE_LAST_AGENT_RESULT) or {}
    current_progress = _investigation_progress_snapshot(tool_context.state)
    frontier_summary = _frontier_summary(tool_context.state)
    candidate_paths = _derive_candidate_paths(tool_context.state)
    hypotheses = _derive_hypotheses(tool_context.state)
    turns = [HypothesisTurn.model_validate(item) for item in (tool_context.state.get(STATE_HYPOTHESIS_TURNS) or [])]
    findings = _load_learned_findings(tool_context.state)
    return {
        "question": request.question,
        "requestedRoles": request.requested_roles,
        "selectedRoles": selected_roles,
        "defaultEpistemicMode": "mixed",
        "toolAvailability": {
            "corpus": bool(request.corpus_documents),
            "vertexGroundedSearch": _vertex_grounding_config(),
            "webSearch": _web_search_available(),
        },
        "currentMission": tool_context.state.get(STATE_CURRENT_MISSION, {}) or {},
        "roleContracts": _selected_role_contract_payload(selected_roles),
        "graphObservationCount": len(graph),
        "graphObservations": [item.model_dump(mode="json", by_alias=True) for item in graph[-24:]],
        "graphFragmentCount": len(fragments),
        "graphFragments": [item.model_dump(mode="json", by_alias=True) for item in fragments[-10:]],
        "branchCount": len(branches),
        "branches": [item.model_dump(mode="json", by_alias=True) for item in branches[-6:]],
        "openGaps": _open_gap_texts(tool_context.state),
        "answeredQuestions": list(tool_context.state.get(STATE_ANSWERED_QUESTIONS) or []),
        "failures": [item.model_dump(mode="json", by_alias=True) for item in failures[-10:]],
        "lastAgentResult": last_agent_raw,
        "frontierSummary": frontier_summary,
        "candidatePaths": [item.model_dump(mode="json", by_alias=True) for item in candidate_paths],
        "hypotheses": [item.model_dump(mode="json", by_alias=True) for item in hypotheses],
        "hypothesisTurns": [item.model_dump(mode="json", by_alias=True) for item in turns[-16:]],
        "learnedFindingCount": len(findings),
        "learnedFindings": [item.model_dump(mode="json", by_alias=True) for item in findings[-20:]],
        "progress": {
            "current": current_progress,
            "stagnationCount": int(tool_context.state.get(STATE_GLOBAL_STAGNATION, 0) or 0),
            "history": list(tool_context.state.get(STATE_PROGRESS_HISTORY) or [])[-6:],
        },
    }


def read_corpus_index(tool_context) -> dict[str, Any]:
    request = AgentLoomRequest.model_validate(tool_context.state.get(STATE_REQUEST) or {})
    return {
        "documentCount": len(request.corpus_documents),
        "documents": [
            {
                "documentId": doc.document_id,
                "title": doc.title,
                "sourceUri": doc.source_uri,
                "sourceType": doc.source_type,
                "tags": doc.tags,
            }
            for doc in request.corpus_documents
        ],
    }


def read_corpus_document(document_id: str, tool_context) -> dict[str, Any]:
    request = AgentLoomRequest.model_validate(tool_context.state.get(STATE_REQUEST) or {})
    for doc in request.corpus_documents:
        if doc.document_id == document_id:
            return {
                "ok": True,
                "document": {
                    "documentId": doc.document_id,
                    "title": doc.title,
                    "text": doc.text,
                    "sourceUri": doc.source_uri,
                    "sourceType": doc.source_type,
                    "tags": doc.tags,
                    "metadata": doc.metadata,
                },
            }
    return {
        "ok": False,
        "errorCode": "not_found",
        "message": f"documentId={document_id!r} is not present in corpusDocuments",
    }


def search_corpus(query: str, tool_context, max_results: int = 5) -> dict[str, Any]:
    request = AgentLoomRequest.model_validate(tool_context.state.get(STATE_REQUEST) or {})
    terms = [term.lower() for term in _TOKEN_RE.findall(query) if len(term) >= 3]
    if not request.corpus_documents or not terms:
        return {"query": query, "matches": []}

    ranked: list[tuple[int, dict[str, Any]]] = []
    for doc in request.corpus_documents:
        haystack = f"{doc.title}\n{doc.text}".lower()
        score = sum(haystack.count(term) for term in terms)
        if score <= 0:
            continue
        ranked.append(
            (
                score,
                {
                    "documentId": doc.document_id,
                    "title": doc.title,
                    "score": score,
                    "snippet": _build_snippet(doc.text, terms),
                    "sourceUri": doc.source_uri,
                },
            )
        )
    ranked.sort(key=lambda item: (-item[0], item[1]["documentId"]))
    return {"query": query, "matches": [item[1] for item in ranked[:max_results]]}


def vertex_grounded_search(query: str, tool_context, max_results: int = 5) -> dict[str, Any]:
    config = _vertex_grounding_config()
    if not config.get("available"):
        return {
            "ok": False,
            "query": query,
            "mode": "vertex_ai_search",
            "errorCode": "unavailable",
            "message": config.get("reason", "Vertex grounded search is unavailable."),
            "sources": [],
        }

    try:
        from google.genai import Client, types as live_types
    except Exception as exc:
        return {
            "ok": False,
            "query": query,
            "mode": "vertex_ai_search",
            "errorCode": "sdk_unavailable",
            "message": str(exc),
            "sources": [],
        }

    prompt = (
        "Retrieve evidence relevant to this investigation query. "
        "Return only grounded findings that are directly supported by retrieved context.\n\n"
        f"Query: {query}"
    )

    try:
        client = Client(vertexai=True, project=config["project"], location=config["location"])
        response = client.models.generate_content(
            model=GROUNDING_MODEL,
            contents=prompt,
            config=live_types.GenerateContentConfig(
                temperature=0,
                max_output_tokens=int(os.getenv("AGENTLOOM_GROUNDED_SEARCH_MAX_OUTPUT_TOKENS", "8192")),
                tools=[
                    live_types.Tool(
                        retrieval=live_types.Retrieval(
                            vertex_ai_search=live_types.VertexAISearch(
                                datastore=config["datastorePath"],
                                max_results=max_results,
                            )
                        )
                    )
                ],
            ),
        )
    except Exception as exc:
        return {
            "ok": False,
            "query": query,
            "mode": "vertex_ai_search",
            "errorCode": "search_failed",
            "message": str(exc),
            "sources": [],
        }

    sources: list[dict[str, Any]] = []
    if response.candidates:
        candidate = response.candidates[0]
        metadata = getattr(candidate, "grounding_metadata", None)
        chunks = getattr(metadata, "grounding_chunks", None) or []
        for chunk in chunks:
            retrieved = getattr(chunk, "retrieved_context", None)
            if retrieved is None:
                continue
            sources.append(
                {
                    "title": getattr(retrieved, "title", "") or "",
                    "uri": getattr(retrieved, "uri", "") or "",
                    "documentName": getattr(retrieved, "document_name", "") or "",
                    "text": getattr(retrieved, "text", "") or "",
                }
            )

    return {
        "ok": True,
        "query": query,
        "mode": "vertex_ai_search",
        "summary": (getattr(response, "text", "") or "").strip(),
        "sources": _dedupe_sources(sources),
        "modelVersion": getattr(response, "model_version", "") or "",
    }


EDGE_DIRECTION_POLICIES: dict[str, tuple[set[str], set[str], str]] = {
    "PRODUCES": (
        {"ORGANIZATION", "FACILITY", "DEPOSIT"},
        {"PRODUCT"},
        "Producer produces product",
    ),
    "SUPPLIES_TO": (
        {"ORGANIZATION", "FACILITY", "PRODUCT"},
        {"ORGANIZATION", "FACILITY"},
        "Supplier/product supplies to customer",
    ),
    "TRANSFORMS_INTO": (
        {"PRODUCT"},
        {"PRODUCT"},
        "Product transforms into downstream product",
    ),
    "ASSEMBLES_INTO": (
        {"PRODUCT"},
        {"PRODUCT"},
        "Component assembles into downstream product",
    ),
    "USED_IN": (
        {"PRODUCT"},
        {"PRODUCT"},
        "Input product is used in downstream product",
    ),
    "EXTRACTS_FROM": (
        {"FACILITY"},
        {"DEPOSIT"},
        "Facility extracts from deposit",
    ),
    "LOCATED_IN": (
        {"FACILITY", "ORGANIZATION", "PERSON", "PRODUCT", "PROGRAM", "GEO_REGION", "DEPOSIT", "COMMUNITY"},
        {"GEO_REGION"},
        "Entity is located in region",
    ),
    "OPERATES_IN": (
        {"ORGANIZATION", "PERSON", "FACILITY"},
        {"GEO_REGION", "FACILITY", "DEPOSIT"},
        "Entity operates in location",
    ),
    "OWNS": (
        {"ORGANIZATION", "PERSON", "COMMUNITY"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "DEPOSIT"},
        "Owner owns asset",
    ),
    "CONTROLS": (
        {"ORGANIZATION", "PERSON", "COMMUNITY"},
        {"ORGANIZATION", "FACILITY", "PRODUCT"},
        "Controller controls entity",
    ),
    "MANAGES": (
        {"ORGANIZATION", "PERSON"},
        {"ORGANIZATION", "FACILITY", "PROGRAM"},
        "Manager manages entity",
    ),
    "INVESTS_IN": (
        {"ORGANIZATION", "PERSON"},
        {"ORGANIZATION", "PROGRAM", "PRODUCT", "FACILITY"},
        "Investor invests in investee",
    ),
    "PARTNERS_WITH": (
        {"ORGANIZATION", "FACILITY"},
        {"ORGANIZATION", "FACILITY"},
        "Partners collaborate (symmetric)",
    ),
    "REGULATES": (
        {"ORGANIZATION", "PROGRAM"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "GEO_REGION", "MARKET"},
        "Regulator regulates entity",
    ),
    "CONTROLS_ACCESS": (
        {"DECISION_POINT", "ORGANIZATION", "COMMUNITY", "PROGRAM"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "PROGRAM", "COMMUNITY"},
        "Gatekeeper controls access",
    ),
    "IMPOSES_CONDITION": (
        {"MECHANISM", "ORGANIZATION", "DECISION_POINT", "COMMUNITY", "PROGRAM"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "PROGRAM", "COMMUNITY"},
        "Imposes compliance constraints",
    ),
    "USES_MECHANISM": (
        {"DECISION_POINT", "ORGANIZATION", "COMMUNITY", "PROGRAM"},
        {"MECHANISM"},
        "Agent uses control mechanism",
    ),
    "APPLIES_TO": (
        {"MECHANISM"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "COMMUNITY", "PROGRAM"},
        "Mechanism applies to entity",
    ),
    "ADMINISTERS": (
        {"ORGANIZATION", "GEO_REGION"},
        {"ORGANIZATION", "PROGRAM", "GEO_REGION", "FACILITY", "DEPOSIT"},
        "Government body administers entity",
    ),
    "ASSURES": (
        {"ORGANIZATION"},
        {"ORGANIZATION", "FACILITY", "PROGRAM"},
        "Assurer provides assurance",
    ),
    "AFFILIATED_WITH": (
        {"COMMUNITY", "PERSON"},
        {"ORGANIZATION"},
        "Entity affiliated with organization",
    ),
    "BOARD_MEMBER_OF": (
        {"PERSON"},
        {"ORGANIZATION"},
        "Person is board member of organization",
    ),
    "SUBSIDIARY_OF": (
        {"ORGANIZATION"},
        {"ORGANIZATION", "PERSON", "COMMUNITY"},
        "Subsidiary is owned by parent",
    ),
    "AFFECTS": (
        {"ORGANIZATION", "PROGRAM", "COMMUNITY", "MARKET", "MECHANISM"},
        {"ORGANIZATION", "FACILITY", "GEO_REGION", "PRODUCT", "PROGRAM", "COMMUNITY", "MARKET"},
        "Source affects target",
    ),
    "IN_GEO_SCOPE": (
        {"ORGANIZATION", "PROGRAM", "MARKET", "MECHANISM", "DECISION_POINT"},
        {"GEO_REGION"},
        "Entity in geographic scope",
    ),
    "OPERATES_VIA": (
        {"COMMUNITY", "ORGANIZATION"},
        {"MEDIA_CHANNEL"},
        "Entity operates via channel",
    ),
    "DEPENDS_ON": (
        {"ORGANIZATION", "FACILITY", "PRODUCT", "PROGRAM", "COMMUNITY", "DEPOSIT", "MARKET"},
        {"ORGANIZATION", "FACILITY", "PRODUCT", "PROGRAM", "COMMUNITY", "DEPOSIT", "MARKET", "GEO_REGION"},
        "Entity depends on entity",
    ),
}


def _validate_edge_direction(
    relation: str, subject_type: str, object_type: str
) -> tuple[bool, bool]:
    """Check edge direction against policy. Returns (valid, should_reverse)."""
    policy = EDGE_DIRECTION_POLICIES.get(relation)
    if policy is None:
        return True, False
    valid_sources, valid_targets, _desc = policy
    if subject_type in valid_sources and object_type in valid_targets:
        return True, False
    if object_type in valid_sources and subject_type in valid_targets:
        return False, True
    return False, False


def append_graph_observations(observations: list[dict[str, Any]], tool_context) -> dict[str, Any]:
    current = [_observation_ids(GraphObservation.model_validate(item)) for item in (tool_context.state.get(STATE_WORKING_GRAPH) or [])]
    current, result = _merge_graph_observation_lists(
        current,
        [_observation_ids(GraphObservation.model_validate(item)) for item in observations],
    )
    tool_context.state[STATE_WORKING_GRAPH] = [item.model_dump(mode="json", by_alias=True) for item in current]
    return result


def _merge_graph_observation_lists(
    existing_observations: list[GraphObservation],
    incoming_observations: list[GraphObservation],
) -> tuple[list[GraphObservation], dict[str, Any]]:
    current = [_observation_ids(item) for item in existing_observations]
    existing = {(item.subject, item.relation, item.object, tuple(item.qualifiers)) for item in current}
    appended = 0
    reversed_count = 0
    dropped_count = 0
    rejected: list[dict[str, Any]] = []
    for idx, observation in enumerate(incoming_observations):
        try:
            observation = _observation_ids(GraphObservation.model_validate(observation.model_dump(mode="json", by_alias=True)))
        except Exception as exc:
            rejected.append({"index": idx, "error": str(exc)})
            continue

        valid, should_reverse = _validate_edge_direction(
            observation.relation, observation.subject_type, observation.object_type
        )
        if not valid and should_reverse:
            observation.subject, observation.object = observation.object, observation.subject
            observation.subject_type, observation.object_type = observation.object_type, observation.subject_type
            observation.subject_id, observation.object_id = observation.object_id, observation.subject_id
            reversed_count += 1
        elif not valid:
            dropped_count += 1
            continue

        key = (observation.subject, observation.relation, observation.object, tuple(observation.qualifiers))
        if key in existing:
            continue
        existing.add(key)
        current.append(observation)
        appended += 1
    result: dict[str, Any] = {"appended": appended, "graphObservationCount": len(current), "rejected": rejected}
    if reversed_count:
        result["reversedEdges"] = reversed_count
    if dropped_count:
        result["droppedInvalidEdges"] = dropped_count
    return current, result


def _patch_target_ids(fragment: GraphFragment) -> set[str]:
    return {item for item in [fragment.old_id, *fragment.old_ids] if item}


def _observation_references_ids(observation: GraphObservation, ids: set[str]) -> bool:
    return bool(ids) and ((observation.subject_id or "") in ids or (observation.object_id or "") in ids)


def _fragment_with_recomputed_boundaries(fragment: GraphFragment, observations: list[GraphObservation]) -> GraphFragment | None:
    if not observations:
        return None
    normalized = [_observation_ids(item) for item in observations]
    return fragment.model_copy(
        update={
            "start_node_id": normalized[0].subject_id,
            "end_node_id": normalized[-1].object_id,
            "observations": normalized,
        }
    )


def _sync_branch_graph_fragments(
    branches: list[BranchState],
    graph_fragments: list[GraphFragment],
) -> list[BranchState]:
    fragments_by_branch: dict[str, list[GraphFragment]] = {}
    for fragment in graph_fragments:
        fragments_by_branch.setdefault(fragment.branch_id, []).append(fragment)
    synced: list[BranchState] = []
    for branch in branches:
        if branch.branch_id in fragments_by_branch or branch.graph_fragments:
            synced.append(branch.model_copy(update={"graph_fragments": fragments_by_branch.get(branch.branch_id, [])}))
        else:
            synced.append(branch)
    return synced


def _apply_graph_fragment_mutations(
    state: dict[str, Any],
    incoming_fragments: list[GraphFragment],
) -> dict[str, Any]:
    existing_graph = [
        _observation_ids(GraphObservation.model_validate(item))
        for item in (state.get(STATE_WORKING_GRAPH) or [])
    ]
    existing_fragments = [
        _fragment_with_recomputed_boundaries(GraphFragment.model_validate(item), GraphFragment.model_validate(item).observations)
        for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])
    ]
    merged_fragments = [item for item in existing_fragments if item is not None]
    tombstoned_node_ids = {item for item in (state.get(STATE_REPLACED_NODE_IDS) or []) if item}

    if tombstoned_node_ids:
        cleaned_fragments: list[GraphFragment] = []
        for fragment in merged_fragments:
            kept_observations = [
                obs for obs in fragment.observations if not _observation_references_ids(obs, tombstoned_node_ids)
            ]
            updated_fragment = _fragment_with_recomputed_boundaries(fragment, kept_observations)
            if updated_fragment is not None:
                cleaned_fragments.append(updated_fragment)
        merged_fragments = cleaned_fragments

    prior_fragment_keys = {_observation_key(item) for item in _flatten_fragments(merged_fragments)}
    residual_graph = [item for item in existing_graph if _observation_key(item) not in prior_fragment_keys]
    if tombstoned_node_ids:
        residual_graph = [item for item in residual_graph if not _observation_references_ids(item, tombstoned_node_ids)]

    replaced_fragment_ids: set[str] = set()
    replaced_node_ids: set[str] = set()

    for fragment in incoming_fragments:
        normalized_fragment = _fragment_with_recomputed_boundaries(fragment, fragment.observations)
        if normalized_fragment is None:
            continue
        if tombstoned_node_ids:
            kept_observations = [
                obs for obs in normalized_fragment.observations if not _observation_references_ids(obs, tombstoned_node_ids)
            ]
            normalized_fragment = _fragment_with_recomputed_boundaries(normalized_fragment, kept_observations)
            if normalized_fragment is None:
                continue
        target_ids = _patch_target_ids(normalized_fragment)
        if normalized_fragment.patch_mode == "replace_fragment":
            replaced_fragment_ids.update(target_ids)
            merged_fragments = [item for item in merged_fragments if item.fragment_id not in target_ids]
        elif normalized_fragment.patch_mode == "replace_node":
            replaced_node_ids.update(target_ids)
            tombstoned_node_ids.update(target_ids)
            patched_fragments: list[GraphFragment] = []
            for existing_fragment in merged_fragments:
                kept_observations = [
                    obs for obs in existing_fragment.observations if not _observation_references_ids(obs, target_ids)
                ]
                updated_fragment = _fragment_with_recomputed_boundaries(existing_fragment, kept_observations)
                if updated_fragment is not None:
                    patched_fragments.append(updated_fragment)
            merged_fragments = patched_fragments

        merged_fragments = [item for item in merged_fragments if item.fragment_id != normalized_fragment.fragment_id]
        merged_fragments.append(normalized_fragment)

    if replaced_node_ids:
        residual_graph = [item for item in residual_graph if not _observation_references_ids(item, replaced_node_ids)]

    fragment_graph, merge_result = _merge_graph_observation_lists(residual_graph, _flatten_fragments(merged_fragments))
    state[STATE_GRAPH_FRAGMENTS] = [item.model_dump(mode="json", by_alias=True) for item in merged_fragments]
    state[STATE_WORKING_GRAPH] = [item.model_dump(mode="json", by_alias=True) for item in fragment_graph]
    state[STATE_REPLACED_NODE_IDS] = sorted(tombstoned_node_ids)

    if STATE_BRANCH_STATES in state:
        synced = _sync_branch_graph_fragments(
            [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])],
            merged_fragments,
        )
        state[STATE_BRANCH_STATES] = [item.model_dump(mode="json", by_alias=True) for item in synced]

    result = dict(merge_result)
    if replaced_fragment_ids:
        result["replacedFragmentIds"] = sorted(replaced_fragment_ids)
    if replaced_node_ids:
        result["replacedNodeIds"] = sorted(replaced_node_ids)
    result["graphFragmentCount"] = len(merged_fragments)
    return result


def _load_learned_findings(state: dict[str, Any]) -> list[LearnedFinding]:
    findings: list[LearnedFinding] = []
    for raw in state.get(STATE_LEARNED_FINDINGS) or []:
        try:
            findings.append(LearnedFinding.model_validate(raw))
        except Exception:
            continue
    return findings


def _save_learned_findings(state: dict[str, Any], findings: list[LearnedFinding]) -> None:
    state[STATE_LEARNED_FINDINGS] = [item.model_dump(mode="json", by_alias=True) for item in findings]


def _merge_learned_findings(existing: list[LearnedFinding], incoming: list[LearnedFinding]) -> list[LearnedFinding]:
    merged = {item.finding_id: item for item in existing}
    for finding in incoming:
        merged[finding.finding_id] = finding
    return list(merged.values())


def _record_learned_findings(state: dict[str, Any], findings: list[LearnedFinding]) -> None:
    if not findings:
        return
    merged = _merge_learned_findings(_load_learned_findings(state), findings)
    _save_learned_findings(state, merged)


def _finding_matches_context(finding: LearnedFinding, *, target_entities: list[str], branch_question: str) -> bool:
    text = " ".join([finding.summary, finding.question_asked, " ".join(finding.related_entities)]).lower()
    tokens = {
        token.lower()
        for token in _TOKEN_RE.findall(" ".join(target_entities + [branch_question]))
        if len(token) >= 4
    }
    if not tokens:
        return True
    return any(token in text for token in tokens)


def _relevant_learned_findings(
    state: dict[str, Any],
    *,
    target_entities: list[str],
    branch_question: str,
    limit: int = 6,
) -> list[LearnedFinding]:
    findings = _load_learned_findings(state)
    relevant = [
        item
        for item in findings
        if _finding_matches_context(item, target_entities=target_entities, branch_question=branch_question)
    ]
    relevant.sort(
        key=lambda item: (
            {
                "negative_result": 0,
                "operational_flow": 1,
                "contractual_procurement": 2,
                "governance_overlay": 3,
                "policy_overlay": 4,
                "context": 5,
            }.get(item.finding_kind, 9),
            -item.confidence,
        )
    )
    return relevant[:limit]


def _load_gap_entries(state: dict[str, Any]) -> list[GapEntry]:
    raw = state.get(STATE_OPEN_GAPS) or []
    entries: list[GapEntry] = []
    for item in raw:
        if isinstance(item, str):
            entries.append(GapEntry(gapId=f"gap-legacy-{len(entries)+1}", text=item))
        elif isinstance(item, dict):
            try:
                entries.append(GapEntry.model_validate(item))
            except Exception:
                text = item.get("text") or str(item)
                entries.append(GapEntry(gapId=f"gap-parse-{len(entries)+1}", text=text))
    return entries


def _save_gap_entries(state: dict[str, Any], entries: list[GapEntry]) -> None:
    state[STATE_OPEN_GAPS] = [item.model_dump(mode="json", by_alias=True) for item in entries]


def _open_gap_entries(state: dict[str, Any]) -> list[GapEntry]:
    return [item for item in _load_gap_entries(state) if item.status == "open"]


def _open_structural_gap_entries(state: dict[str, Any]) -> list[GapEntry]:
    return [item for item in _load_gap_entries(state) if item.status == "open" and item.gap_kind != "detail"]


def _open_gap_texts(state: dict[str, Any]) -> list[str]:
    return [item.text for item in _open_gap_entries(state)]


def _open_structural_gap_texts(state: dict[str, Any]) -> list[str]:
    return [item.text for item in _open_structural_gap_entries(state)]


def _open_gap_count(state: dict[str, Any]) -> int:
    return sum(1 for item in _load_gap_entries(state) if item.status == "open")


_DETAIL_SIGNALS = re.compile(
    r"\b(exact|specific|precise|percentage|%|volume|tonnage|tpa|"
    r"how much|how many|annual|quarterly|quantif|amount|figure|number of)\b",
    re.IGNORECASE,
)


def _classify_gap_kind(text: str) -> Literal["structural", "detail", "unknown"]:
    if _DETAIL_SIGNALS.search(text):
        return "detail"
    return "structural"


def _is_semantically_duplicate(new_text: str, existing_texts: list[str]) -> bool:
    """Cheap token-overlap dedup: if 60%+ of significant words overlap, it's a dupe."""
    stop = {"the", "a", "an", "is", "are", "of", "in", "to", "for", "and", "or", "from", "by", "with", "on", "at", "what", "which", "how", "that", "this", "it", "its"}
    new_tokens = {w for w in re.findall(r"[a-z]+", new_text.lower()) if w not in stop and len(w) > 2}
    if not new_tokens:
        return False
    for existing in existing_texts:
        existing_tokens = {w for w in re.findall(r"[a-z]+", existing.lower()) if w not in stop and len(w) > 2}
        if not existing_tokens:
            continue
        overlap = new_tokens & existing_tokens
        smaller = min(len(new_tokens), len(existing_tokens))
        if smaller > 0 and len(overlap) / smaller >= 0.6:
            return True
    return False


def record_open_gaps(gaps: list[str], tool_context, *, origin: str = "", cycle: int = 0) -> dict[str, Any]:
    entries = _load_gap_entries(tool_context.state)
    seen_exact = {entry.text for entry in entries}
    seen_texts = [entry.text for entry in entries]
    recorded = 0
    skipped_detail = 0
    skipped_dupe = 0
    for gap in gaps:
        normalized = gap.strip()
        if not normalized or normalized in seen_exact:
            continue
        if _is_semantically_duplicate(normalized, seen_texts):
            skipped_dupe += 1
            continue
        kind = _classify_gap_kind(normalized)
        seen_exact.add(normalized)
        seen_texts.append(normalized)
        entries.append(GapEntry(
            gapId=f"gap-{len(entries)+1}",
            text=normalized,
            status="open",
            gapKind=kind,
            origin=origin,
            cycle=cycle,
        ))
        recorded += 1
    _save_gap_entries(tool_context.state, entries)
    open_count = sum(1 for e in entries if e.status == "open")
    return {
        "recorded": recorded,
        "skippedDuplicate": skipped_dupe,
        "openGapCount": open_count,
        "totalGapCount": len(entries),
    }


def resolve_gaps(state: dict[str, Any], gap_texts: list[str], *, addressed_by: str = "") -> int:
    if not gap_texts:
        return 0
    entries = _load_gap_entries(state)
    normalized_targets = {text.strip().lower() for text in gap_texts if text.strip()}
    resolved_count = 0
    for entry in entries:
        if entry.status != "open":
            continue
        if entry.text.strip().lower() in normalized_targets:
            entry.status = "addressed"
            entry.addressed_by = addressed_by
            resolved_count += 1
    if resolved_count > 0:
        _save_gap_entries(state, entries)
    return resolved_count


def _auto_resolve_detail_gaps(state: dict[str, Any], *, addressed_by: str = "auto_detail_triage") -> int:
    """Close open detail gaps automatically. Also close gaps whose subject entities
    already appear in the working graph with at least one typed edge."""
    entries = _load_gap_entries(state)
    graph_obs = state.get(STATE_WORKING_GRAPH) or []
    graph_subjects = set()
    graph_objects = set()
    for obs in graph_obs:
        if isinstance(obs, dict):
            subj = (obs.get("subject") or "").lower().strip()
            obj = (obs.get("object") or "").lower().strip()
            if subj:
                graph_subjects.add(subj)
            if obj:
                graph_objects.add(obj)
    graph_entities = graph_subjects | graph_objects
    resolved_count = 0
    for entry in entries:
        if entry.status != "open":
            continue
        if entry.gap_kind == "detail":
            entry.status = "addressed"
            entry.addressed_by = addressed_by
            resolved_count += 1
            continue
        if entry.gap_kind == "unknown" and _DETAIL_SIGNALS.search(entry.text):
            entry.gap_kind = "detail"
            entry.status = "addressed"
            entry.addressed_by = addressed_by
            resolved_count += 1
            continue
        gap_lower = entry.text.lower()
        entity_hits = sum(1 for e in graph_entities if e in gap_lower)
        if entity_hits >= 2 and _DETAIL_SIGNALS.search(entry.text):
            entry.gap_kind = "detail"
            entry.status = "addressed"
            entry.addressed_by = addressed_by
            resolved_count += 1
    if resolved_count > 0:
        _save_gap_entries(state, entries)
    return resolved_count


def record_failure(code: str, message: str, blocking: bool, tool_context, missing_inputs: list[str] | None = None) -> dict[str, Any]:
    try:
        failure = FailureRecord(
            code=code,  # type: ignore[arg-type]
            message=message,
            blocking=blocking,
            missingInputs=missing_inputs or [],
        )
    except Exception as exc:
        return {"accepted": False, "failureCode": "invalid_failure_payload", "error": str(exc)}
    current = list(tool_context.state.get(STATE_FAILURES) or [])
    current.append(failure.model_dump(mode="json", by_alias=True))
    tool_context.state[STATE_FAILURES] = current
    return {"accepted": True, "failureCode": code, "failureCount": len(current)}


def _slug(value: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-") or "node"


def _observation_ids(observation: GraphObservation) -> GraphObservation:
    return observation.model_copy(
        update={
            "subject_id": observation.subject_id or f"{_slug(observation.subject_type or 'entity')}:{_slug(observation.subject)}",
            "object_id": observation.object_id or f"{_slug(observation.object_type or 'entity')}:{_slug(observation.object)}",
        }
    )


def _normalize_fragment(
    fragment: GraphFragment,
    action: ToolAction,
    allowed_edges: set[str],
    default_support_type: str,
) -> GraphFragment:
    observations: list[GraphObservation] = []
    for raw in fragment.observations:
        if allowed_edges and raw.relation not in allowed_edges:
            continue
        observation = _observation_ids(raw).model_copy(
            update={
                "branch_id": raw.branch_id or action.branch_id,
                "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                "support_type": raw.support_type or default_support_type,
            }
        )
        observations.append(observation)
    return fragment.model_copy(
        update={
            "branch_id": fragment.branch_id or action.branch_id,
            "human_question": fragment.human_question or action.human_question,
            "human_summary": fragment.human_summary or fragment.title,
            "alternative_group_id": fragment.alternative_group_id or "",
            "observations": observations[:18],
            "quality_score": max(0.0, min(1.0, fragment.quality_score)),
        }
    )


def _finding_evidence_status_from_result(result: ToolActionResult) -> Literal["verified", "reasoned", "mixed", "hypothesis"]:
    observations = _flatten_fragments(result.graph_fragments)
    if observations:
        return _evidence_status_for_observations(observations)  # type: ignore[return-value]
    if result.source_documents:
        return "verified"
    if result.path_assessment == "rejected":
        return "reasoned"
    if result.status == "success":
        return "reasoned"
    return "hypothesis"


def _finding_confidence_from_result(result: ToolActionResult) -> float:
    observations = _flatten_fragments(result.graph_fragments)
    if observations:
        return round(sum(item.confidence for item in observations) / len(observations), 3)
    if result.path_assessment == "rejected":
        return 0.65
    if result.status == "success":
        return 0.6
    if result.status == "partial":
        return 0.48
    if result.status == "blocked":
        return 0.35
    return 0.25


def _finding_kind_from_action_result(
    action: ToolAction,
    result: ToolActionResult,
) -> Literal[
    "operational_flow",
    "contractual_procurement",
    "governance_overlay",
    "policy_overlay",
    "negative_result",
    "context",
]:
    text = " ".join(
        filter(
            None,
            [
                result.human_summary,
                " ".join(result.resolved_frontier),
                " ".join(result.remaining_frontier),
                " ".join(result.discovered_frontier),
                " ".join(result.uncovered_questions),
            ],
        )
    )
    role = action.assigned_roles[0] if action.assigned_roles else ROLE_SUPPLY_CHAIN
    if result.path_assessment == "rejected" or _NEGATIVE_PATH_SIGNALS.search(text):
        return "negative_result"
    if role == ROLE_GOVERNANCE:
        return "governance_overlay"
    if role == ROLE_POLICY:
        return "policy_overlay"
    if role == ROLE_SUPPLY_CHAIN and _PROCUREMENT_SIGNALS.search(text):
        return "contractual_procurement"
    if result.graph_fragments:
        return "operational_flow"
    return "context"


def _related_entities_from_action_result(action: ToolAction, result: ToolActionResult) -> list[str]:
    related = list(action.target_entities)
    for observation in _flatten_fragments(result.graph_fragments):
        if observation.subject:
            related.append(observation.subject)
        if observation.object:
            related.append(observation.object)
    return _dedupe_preserve_order([item for item in related if item])[:8]


def _normalize_learned_findings_payload(
    raw_findings: list[dict[str, Any]],
    action: ToolAction,
    result: ToolActionResult,
) -> list[LearnedFinding]:
    normalized: list[LearnedFinding] = []
    default_role = action.assigned_roles[0] if action.assigned_roles else ROLE_SUPPLY_CHAIN
    default_kind = _finding_kind_from_action_result(action, result)
    default_evidence = _finding_evidence_status_from_result(result)
    default_confidence = _finding_confidence_from_result(result)
    default_entities = _related_entities_from_action_result(action, result)
    for idx, raw in enumerate(raw_findings[:6], start=1):
        if not isinstance(raw, dict):
            continue
        try:
            finding = LearnedFinding.model_validate(
                {
                    "findingId": raw.get("findingId") or f"{action.branch_id}-lf{idx}",
                    "hypothesisId": raw.get("hypothesisId") or action.branch_id,
                    "branchId": raw.get("branchId") or action.branch_id,
                    "role": raw.get("role") or default_role,
                    "questionAsked": raw.get("questionAsked") or action.human_question,
                    "findingKind": raw.get("findingKind") or default_kind,
                    "summary": raw.get("summary") or result.human_summary,
                    "evidenceStatus": raw.get("evidenceStatus") or default_evidence,
                    "confidence": raw.get("confidence") if raw.get("confidence") is not None else default_confidence,
                    "relatedEntities": raw.get("relatedEntities") or default_entities,
                    "sourceDocuments": raw.get("sourceDocuments") or result.source_documents,
                }
            )
        except Exception:
            continue
        normalized.append(finding)
    return normalized


def _fallback_learned_finding(action: ToolAction, result: ToolActionResult) -> LearnedFinding:
    return LearnedFinding(
        findingId=f"{action.branch_id}-lf-auto-{_slug(action.action_id)}",
        hypothesisId=action.branch_id,
        branchId=action.branch_id,
        role=(action.assigned_roles[0] if action.assigned_roles else ROLE_SUPPLY_CHAIN),  # type: ignore[arg-type]
        questionAsked=action.human_question,
        findingKind=_finding_kind_from_action_result(action, result),
        summary=result.human_summary,
        evidenceStatus=_finding_evidence_status_from_result(result),
        confidence=_finding_confidence_from_result(result),
        relatedEntities=_related_entities_from_action_result(action, result),
        sourceDocuments=list(result.source_documents)[:8],
    )


def _ensure_learned_findings(action: ToolAction, result: ToolActionResult) -> ToolActionResult:
    findings = list(result.learned_findings)
    if not findings and result.human_summary:
        findings = [_fallback_learned_finding(action, result)]
    return result.model_copy(update={"learned_findings": findings[:6]})


def _flatten_fragments(fragments: list[GraphFragment]) -> list[GraphObservation]:
    flattened: list[GraphObservation] = []
    for fragment in fragments:
        flattened.extend(fragment.observations)
    return flattened


def _tool_failure_result(
    action: ToolAction,
    status: Literal["blocked", "failed"],
    message: str,
    *,
    code: str = "blocked_by_capability",
    missing_inputs: list[str] | None = None,
) -> ToolActionResult:
    return ToolActionResult(
        actionId=action.action_id,
        branchId=action.branch_id,
        toolName=action.tool_name,
        pathAssessment="still_open",
        pathAssessmentNote="The tool could not answer this step, so the candidate path remains unresolved.",
        status=status,
        humanSummary=message,
        closureStatus="unchanged",
        resolvedFrontier=[],
        remainingFrontier=list(action.frontier) or [action.closure_target],
        discoveredFrontier=[],
        graphFragments=[],
        uncoveredQuestions=[action.human_question],
        sourceDocuments=[],
        failure=FailureRecord(
            code=code,  # type: ignore[arg-type]
            message=message,
            blocking=status == "failed",
            missingInputs=missing_inputs or [],
        ),
    )


def _strip_json_fence(text: str) -> str:
    stripped = text.strip()
    if stripped.startswith("```"):
        stripped = re.sub(r"^```(?:json)?\s*", "", stripped)
        stripped = re.sub(r"\s*```$", "", stripped)
    return stripped.strip()


def _extract_json_object(text: str) -> str:
    stripped = _strip_json_fence(text)
    start = stripped.find("{")
    end = stripped.rfind("}")
    if start >= 0 and end > start:
        return stripped[start : end + 1]
    return stripped


def _response_json_text(response: Any) -> str:
    parsed = getattr(response, "parsed", None)
    if parsed is not None:
        if hasattr(parsed, "model_dump"):
            return json.dumps(parsed.model_dump(mode="json", by_alias=True), ensure_ascii=False)
        if isinstance(parsed, dict):
            return json.dumps(parsed, ensure_ascii=False)
    for candidate in getattr(response, "candidates", None) or []:
        candidate_parsed = getattr(candidate, "parsed", None)
        if candidate_parsed is not None:
            if hasattr(candidate_parsed, "model_dump"):
                return json.dumps(candidate_parsed.model_dump(mode="json", by_alias=True), ensure_ascii=False)
            if isinstance(candidate_parsed, dict):
                return json.dumps(candidate_parsed, ensure_ascii=False)
        content = getattr(candidate, "content", None)
        parts = getattr(content, "parts", None) or []
        candidate_text = "".join((getattr(part, "text", "") or "") for part in parts).strip()
        if candidate_text:
            return candidate_text
    return getattr(response, "text", "") or ""


def _is_resource_exhausted_error(exc: Exception) -> bool:
    text = str(exc).upper()
    return "RESOURCE_EXHAUSTED" in text or "QUOTA EXCEEDED" in text or "429" in text


def _model_with_resource_fallback(primary_model: str, *, used_fallback: bool) -> str:
    if used_fallback:
        return primary_model
    if RESOURCE_EXHAUSTION_FALLBACK_MODEL and RESOURCE_EXHAUSTION_FALLBACK_MODEL != primary_model:
        return RESOURCE_EXHAUSTION_FALLBACK_MODEL
    return primary_model


def _parse_tool_result_payload(
    text: str,
    action: ToolAction,
    allowed_edges: set[str],
    default_support_type: str,
) -> ToolActionResult:
    payload = json.loads(_extract_json_object(text))
    fragments_raw = payload.get("graphFragments") or payload.get("fragments") or []
    normalized_fragments: list[GraphFragment] = []
    for idx, fragment_raw in enumerate(fragments_raw[:4], start=1):
        fragment_payload = {
            "fragmentId": fragment_raw.get("fragmentId") or f"{action.branch_id}-f{idx}",
            "fragmentType": fragment_raw.get("fragmentType") or action.expected_fragment_type,
            "title": fragment_raw.get("title") or f"{action.branch_id} fragment {idx}",
            "humanQuestion": fragment_raw.get("humanQuestion") or action.human_question,
            "humanSummary": fragment_raw.get("humanSummary")
            or fragment_raw.get("summary")
            or payload.get("humanSummary")
            or payload.get("summary")
            or action.human_action,
            "branchId": fragment_raw.get("branchId") or action.branch_id,
            "alternativeGroupId": fragment_raw.get("alternativeGroupId") or "",
            "startNodeId": fragment_raw.get("startNodeId") or "",
            "endNodeId": fragment_raw.get("endNodeId") or "",
            "patchMode": fragment_raw.get("patchMode") or "append",
            "oldId": fragment_raw.get("oldId") or "",
            "oldIds": fragment_raw.get("oldIds") or [],
            "patchRationale": fragment_raw.get("patchRationale") or "",
            "observations": fragment_raw.get("observations") or fragment_raw.get("graphDelta") or [],
            "completeness": fragment_raw.get("completeness") or ("complete" if fragment_raw.get("observations") else "partial"),
            "qualityScore": fragment_raw.get("qualityScore") or payload.get("qualityScore") or 0.4,
        }
        fragment = GraphFragment.model_validate(fragment_payload)
        normalized_fragments.append(_normalize_fragment(fragment, action, allowed_edges, default_support_type))
    status = payload.get("status") or ("success" if normalized_fragments else "partial")
    result_payload = {
        "actionId": payload.get("actionId") or action.action_id,
        "branchId": payload.get("branchId") or action.branch_id,
        "toolName": payload.get("toolName") or action.tool_name,
        "pathAssessment": payload.get("pathAssessment") or "still_open",
        "pathAssessmentNote": payload.get("pathAssessmentNote") or "",
        "status": status,
        "humanSummary": payload.get("humanSummary")
        or payload.get("summary")
        or (normalized_fragments[0].human_summary if normalized_fragments else action.human_action),
        "closureStatus": payload.get("closureStatus")
        or ("closed" if normalized_fragments and not (payload.get("remainingFrontier") or payload.get("uncoveredQuestions")) else "advanced" if normalized_fragments else "unchanged"),
        "resolvedFrontier": payload.get("resolvedFrontier") or [],
        "remainingFrontier": payload.get("remainingFrontier") or payload.get("uncoveredQuestions") or list(action.frontier),
        "discoveredFrontier": payload.get("discoveredFrontier") or [],
        "graphFragments": [item.model_dump(mode="json", by_alias=True) for item in normalized_fragments],
        "learnedFindings": payload.get("learnedFindings") or [],
        "uncoveredQuestions": payload.get("uncoveredQuestions") or payload.get("nextQuestions") or [],
        "sourceDocuments": payload.get("sourceDocuments") or payload.get("sources") or [],
        "failure": payload.get("failure"),
    }
    result = ToolActionResult.model_validate(result_payload)
    normalized_findings = _normalize_learned_findings_payload(payload.get("learnedFindings") or [], action, result)
    result = result.model_copy(update={"learned_findings": normalized_findings})
    result = _ensure_learned_findings(action, result)
    return result.model_copy(
        update={
            "source_documents": list(result.source_documents)[:10],
            "uncovered_questions": list(result.uncovered_questions)[:6],
            "resolved_frontier": list(result.resolved_frontier)[:6],
            "remaining_frontier": list(result.remaining_frontier)[:6],
            "discovered_frontier": list(result.discovered_frontier)[:6],
            "learned_findings": list(result.learned_findings)[:6],
        }
    )


def _observation_key(observation: GraphObservation) -> tuple[str, str, str, str, tuple[str, ...]]:
    return (
        observation.subject_id or observation.subject,
        observation.relation,
        observation.object_id or observation.object,
        observation.branch_id or "",
        tuple(observation.qualifiers),
    )


def _tool_failure_is_malformed_output(result: ToolActionResult) -> bool:
    """True when a tool call failed because the model output was not valid JSON."""
    if result.status != "failed":
        return False
    msg = (result.human_summary or "").lower()
    needles = (
        "invalid tool json",
        "validationerror",
        "jsondecodeerror",
        "eof while parsing",
        "unterminated string",
        "expecting ',' delimiter",
    )
    return any(n in msg for n in needles)


def _tool_is_available(tool_name: str, state: dict[str, Any]) -> bool:
    if tool_name == "retrieve":
        return True
    if tool_name == "web_search":
        return bool(_web_search_available().get("available"))
    return True


def _branch_prefers_public_web(branch_plan: BranchPlan) -> bool:
    text = " ".join(
        [
            branch_plan.human_action,
            branch_plan.human_question,
            branch_plan.closure_target,
            " ".join(branch_plan.frontier),
        ]
    ).lower()
    if "web search" in text:
        return True
    public_fact_signals = (
        "current",
        "company",
        "companies",
        "ownership",
        "owner",
        "investor",
        "facility",
        "facilities",
        "manufacturer",
        "manufacturing",
        "regulation",
        "regulatory",
        "public",
        "supplier",
        "suppliers",
        "supply agreement",
    )
    return any(signal in text for signal in public_fact_signals)


def _planned_tool_sequence(branch_plan: BranchPlan, state: dict[str, Any]) -> list[str]:
    planned = list(branch_plan.preferred_tools[:4])
    if not planned:
        planned = ["reason"]

    ordered: list[str] = []
    seen: set[str] = set()

    def add(tool_name: str) -> None:
        if tool_name in seen:
            return
        if not _tool_is_available(tool_name, state):
            return
        ordered.append(tool_name)
        seen.add(tool_name)

    for tool_name in planned:
        add(tool_name)

    # Respect the LLM's explicit tool order. Runtime may append fallbacks,
    # but it must not override the chosen first tool before any result exists.
    for fallback in ("retrieve", "web_search", "reason"):
        add(fallback)

    return ordered[:5]


def _tool_result_indicates_source_mismatch(result: ToolActionResult) -> bool:
    text = " ".join(
        filter(
            None,
            [
                result.human_summary,
                result.failure.message if result.failure else "",
                " ".join(result.uncovered_questions),
                " ".join(result.remaining_frontier),
            ],
        )
    ).lower()
    mismatch_signals = (
        "missing_corpus",
        "no specific grounded evidence",
        "lacks data",
        "no relevant",
        "focused on semiconductor",
        "wrong corpus",
        "irrelevant corpus",
        "datastore",
    )
    if any(signal in text for signal in mismatch_signals):
        return True
    if result.status == "blocked" and result.failure and result.failure.code in {"missing_corpus", "blocked_by_capability"}:
        return True
    if result.tool_name == "retrieve":
        if result.status in {"partial", "blocked"} and not result.source_documents:
            return True
        if result.closure_status == "unchanged" and not result.graph_fragments:
            return True
    return False


def _tool_result_requires_reroute(
    branch_plan: BranchPlan,
    result: ToolActionResult,
    tried_tools: set[str],
    state: dict[str, Any],
) -> tuple[list[str], str]:
    if result.closure_status == "closed" and not result.remaining_frontier:
        return [], ""

    if result.tool_name == "retrieve" and _tool_result_indicates_source_mismatch(result):
        candidates: list[str] = []
        if _tool_is_available("web_search", state) and "web_search" not in tried_tools:
            candidates.append("web_search")
        if "reason" not in tried_tools:
            candidates.append("reason")
        if candidates:
            return candidates, "Internal retrieval did not produce enough relevant evidence; switching to web/public evidence next."

    if result.tool_name == "reason":
        if _branch_prefers_public_web(branch_plan) and result.remaining_frontier:
            candidates = []
            if _tool_is_available("retrieve", state) and "retrieve" not in tried_tools:
                candidates.append("retrieve")
            if _tool_is_available("web_search", state) and "web_search" not in tried_tools:
                candidates.append("web_search")
            if candidates:
                return candidates, "Reasoning outlined the structure but left fact gaps; switching to retrieval."

    return [], ""


def _tool_model_name(tool_name: str) -> str:
    if tool_name in {"retrieve", "web_search"}:
        return GROUNDING_MODEL
    return INVESTIGATOR_MODEL


def _branch_progress_delta(previous: BranchState | None, result: ToolActionResult) -> float:
    current_observations = _flatten_fragments(result.graph_fragments)
    current_keys = {_observation_key(item) for item in current_observations}
    previous_keys: set[tuple[str, str, str, str, tuple[str, ...]]] = set()
    previous_sources: set[str] = set()
    previous_findings: set[str] = set()
    previous_open_questions: set[str] = set()
    previous_frontier: set[str] = set()
    if previous is not None:
        previous_keys = {_observation_key(item) for item in _flatten_fragments(previous.graph_fragments)}
        previous_sources = set(previous_source for tool_result in previous.tool_results for previous_source in tool_result.source_documents)
        previous_findings = {item.finding_id for item in previous.learned_findings}
        previous_open_questions = set(previous.open_questions)
        previous_frontier = set(previous.remaining_frontier)
    new_observation_count = len(current_keys - previous_keys)
    new_source_count = len(set(result.source_documents) - previous_sources)
    new_finding_count = len({item.finding_id for item in result.learned_findings} - previous_findings)
    coverage_gain = max(0, len(previous_open_questions) - len(set(result.uncovered_questions)))
    frontier_gain = max(0, len(previous_frontier) - len(set(result.remaining_frontier)))
    delta = (
        (new_observation_count * 0.12)
        + (new_source_count * 0.08)
        + (new_finding_count * 0.05)
        + (coverage_gain * 0.04)
        + (frontier_gain * 0.05)
    )
    return max(0.0, min(1.0, delta))


def _investigation_progress_snapshot(state: dict[str, Any]) -> dict[str, int]:
    graph = [GraphObservation.model_validate(item) for item in (state.get(STATE_WORKING_GRAPH) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])]
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    unique_sources = {
        source
        for observation in graph
        for source in observation.provenance
        if source
    }
    return {
        "graphObservationCount": len(graph),
        "documentObservationCount": sum(1 for item in graph if item.support_type == "document"),
        "mixedObservationCount": sum(1 for item in graph if item.support_type == "mixed"),
        "fragmentCount": len(fragments),
        "completeFragmentCount": sum(1 for item in fragments if item.completeness == "complete"),
        "answeredQuestionCount": len(state.get(STATE_ANSWERED_QUESTIONS) or []),
        "openGapCount": _open_gap_count(state),
        "branchCount": len(branches),
        "unresolvedBranchCount": sum(1 for item in branches if item.status in {"needs_followup", "blocked"}),
        "uniqueSourceCount": len(unique_sources),
    }


def _progress_delta(previous: dict[str, int], current: dict[str, int]) -> dict[str, int]:
    return {
        "newGraphObservations": current["graphObservationCount"] - previous["graphObservationCount"],
        "newDocumentObservations": current["documentObservationCount"] - previous["documentObservationCount"],
        "newMixedObservations": current["mixedObservationCount"] - previous["mixedObservationCount"],
        "newFragments": current["fragmentCount"] - previous["fragmentCount"],
        "newCompleteFragments": current["completeFragmentCount"] - previous["completeFragmentCount"],
        "newAnsweredQuestions": current["answeredQuestionCount"] - previous["answeredQuestionCount"],
        "gapReduction": previous["openGapCount"] - current["openGapCount"],
        "newSources": current["uniqueSourceCount"] - previous["uniqueSourceCount"],
        "newUnresolvedBranches": current["unresolvedBranchCount"] - previous["unresolvedBranchCount"],
    }


def _material_progress(delta: dict[str, int]) -> bool:
    return any(
        (
            delta["newDocumentObservations"] >= 2,
            delta["newMixedObservations"] >= 2,
            delta["newSources"] >= 1,
            delta["newCompleteFragments"] >= 1,
            delta["newGraphObservations"] >= 6,
            delta["newAnsweredQuestions"] >= 2 and delta["gapReduction"] >= 1,
            delta["gapReduction"] >= 2,
        )
    )


def _evidence_status_for_observations(observations: list[GraphObservation]) -> str:
    support_types = {item.support_type for item in observations}
    if not support_types:
        return "reasoned"
    if support_types == {"document"}:
        return "verified"
    if support_types == {"hypothesis"}:
        return "hypothesis"
    if "document" in support_types and len(support_types) > 1:
        return "mixed"
    return "reasoned"


def _graph_reachability_report(state: dict[str, Any]) -> dict[str, Any]:
    """Analyze end-to-end reachability of the working graph using taxonomy flow edges.

    Returns a compact report with:
    - completeChains: grounded paths from source to sink that traverse at least 3 flow hops
    - provisionalChains: structurally connected paths that still rely on weak or hypothesis-level flow edges
    - brokenChains: partial paths that stop at a dead end (no outgoing flow edge)
    - bridgeCandidates: pairs (X, Y) both in the graph but not flow-connected
    - healthSummary: one-line assessment for the supervisor
    """
    obs_raw = state.get(STATE_WORKING_GRAPH) or []
    if not obs_raw:
        return {
            "completeChains": [],
            "provisionalChains": [],
            "brokenChains": [],
            "bridgeCandidates": [],
            "healthSummary": "Graph is empty.",
        }

    supply_contract = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    flow_rels: set[str] = set(supply_contract.path_carried_edges) if supply_contract else {
        "PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM",
    }
    governance_contract = ROLE_CONTRACTS.get(ROLE_GOVERNANCE)
    governance_rels: set[str] = set(governance_contract.allowed_edge_types) if governance_contract else set()

    adj: dict[str, list[tuple[str, str, float, str]]] = {}
    all_entities: dict[str, str] = {}
    extract_sources: set[str] = set()
    facility_entities: set[str] = set()
    deposit_entities: set[str] = set()

    for raw in obs_raw:
        o = raw if isinstance(raw, dict) else raw.model_dump(mode="json", by_alias=True) if hasattr(raw, "model_dump") else {}
        rel = o.get("relation", "")
        subj = o.get("subject", "")
        obj = o.get("object", "")
        conf = o.get("confidence", 0.0)
        support_type = o.get("supportType", "")
        subj_type = o.get("subjectType", "")
        obj_type = o.get("objectType", "")
        if subj:
            all_entities[subj] = subj_type
        if obj:
            all_entities[obj] = obj_type
        if subj_type == "FACILITY":
            facility_entities.add(subj)
        if obj_type == "FACILITY":
            facility_entities.add(obj)
        if subj_type == "DEPOSIT" or obj_type == "DEPOSIT":
            deposit_entities.add(subj if subj_type == "DEPOSIT" else obj)
        if rel not in flow_rels:
            continue
        adj.setdefault(subj, []).append((rel, obj, conf, support_type))
        if rel == "EXTRACTS_FROM":
            extract_sources.add(subj)

    sources = extract_sources or facility_entities & {
        e for e, t in all_entities.items() if t in ("FACILITY", "DEPOSIT")
    }
    if not sources:
        sources = {e for e, t in all_entities.items() if t == "DEPOSIT"}

    has_outgoing = set(adj.keys())
    terminal_types = {"PRODUCT", "FACILITY", "ORGANIZATION"}

    def _is_grounded_flow_edge(confidence: float, support_type: str) -> bool:
        return support_type in {"document", "mixed"} and confidence >= 0.5

    def _is_provisional_flow_edge(confidence: float, support_type: str) -> bool:
        if support_type == "hypothesis":
            return False
        return confidence >= 0.25

    def _bfs_chains(start: str, max_depth: int = 15) -> list[list[tuple[str, str, str, float, str]]]:
        chains: list[list[tuple[str, str, str, float, str]]] = []
        queue: list[tuple[str, list[tuple[str, str, str, float, str]], set[str]]] = [(start, [], {start})]
        while queue:
            node, path, visited = queue.pop(0)
            if node not in adj:
                if len(path) >= 1:
                    chains.append(path)
                continue
            extended = False
            for rel, target, edge_conf, edge_support_type in adj[node]:
                if target in visited or len(path) >= max_depth:
                    continue
                extended = True
                new_path = path + [(node, rel, target, edge_conf, edge_support_type)]
                new_visited = visited | {target}
                queue.append((target, new_path, new_visited))
            if not extended and len(path) >= 1:
                chains.append(path)
        return chains

    complete_chains: list[dict[str, Any]] = []
    provisional_chains: list[dict[str, Any]] = []
    broken_chains: list[dict[str, Any]] = []
    terminal_sink_rels = {"SUPPLIES_TO", "USED_IN", "ASSEMBLES_INTO"}

    def _is_valid_terminal_sink(chain: list[tuple[str, str, str, float, str]], end_type: str) -> bool:
        if not chain:
            return False
        last_rel = chain[-1][1]
        return end_type in {"FACILITY", "ORGANIZATION", "PRODUCT"} and last_rel in terminal_sink_rels

    def _dedupe_chain_entries(entries: list[dict[str, Any]]) -> list[dict[str, Any]]:
        seen: set[tuple[str, str, tuple[str, ...], str]] = set()
        deduped: list[dict[str, Any]] = []
        for entry in entries:
            key = (
                str(entry.get("source", "")),
                str(entry.get("sink", "")),
                tuple(entry.get("path", [])),
                str(entry.get("breakReason", entry.get("provisionalReason", ""))),
            )
            if key in seen:
                continue
            seen.add(key)
            deduped.append(entry)
        return deduped

    for src in sorted(sources)[:8]:
        chains = _bfs_chains(src)
        for chain in chains:
            end_node = chain[-1][2]
            end_type = all_entities.get(end_node, "")
            steps = [f"{s} --{r}--> {o}" for s, r, o, _conf, _support_type in chain]
            grounded_edge_count = sum(1 for _s, _r, _o, edge_conf, edge_support_type in chain if _is_grounded_flow_edge(edge_conf, edge_support_type))
            provisional_edge_count = sum(1 for _s, _r, _o, edge_conf, edge_support_type in chain if _is_provisional_flow_edge(edge_conf, edge_support_type))
            weak_edge_count = len(chain) - grounded_edge_count
            entry = {
                "source": src,
                "sink": end_node,
                "sinkType": end_type,
                "hops": len(chain),
                "path": steps[:8],
                "groundedEdgeCount": grounded_edge_count,
                "provisionalEdgeCount": provisional_edge_count,
                "weakEdgeCount": weak_edge_count,
            }
            terminal_sink = end_node not in has_outgoing and _is_valid_terminal_sink(chain, end_type)
            if terminal_sink and len(chain) >= 3 and grounded_edge_count == len(chain):
                complete_chains.append(entry)
            elif terminal_sink and len(chain) >= 3 and provisional_edge_count == len(chain):
                entry["provisionalReason"] = "Path reaches a valid terminal sink but still relies on weak or model-based flow edges."
                provisional_chains.append(entry)
            elif terminal_sink and len(chain) >= 2:
                entry["breakReason"] = (
                    f"{end_node} [{end_type}] is a valid terminal sink, but the path still depends on unsupported or hypothesis-level flow edges"
                )
                broken_chains.append(entry)
            elif len(chain) >= 2 and end_node in has_outgoing:
                pass
            elif len(chain) >= 2:
                entry["breakReason"] = f"{end_node} [{end_type}] has no outgoing flow edge"
                broken_chains.append(entry)

    bridge_candidates: list[dict[str, str]] = []
    flow_connected: set[str] = set()
    for src in sources:
        chains = _bfs_chains(src, max_depth=15)
        for chain in chains:
            for s, _r, o, _edge_conf, _edge_support_type in chain:
                flow_connected.add(s)
                flow_connected.add(o)

    for entity, etype in all_entities.items():
        if etype in ("FACILITY", "PRODUCT") and entity not in flow_connected and entity not in deposit_entities:
            in_governance = any(
                (raw if isinstance(raw, dict) else {}).get("subject") == entity or
                (raw if isinstance(raw, dict) else {}).get("object") == entity
                for raw in obs_raw
                if (raw if isinstance(raw, dict) else {}).get("relation", "") in governance_rels
            )
            if in_governance:
                bridge_candidates.append({
                    "entity": entity,
                    "type": etype,
                    "issue": f"Known in governance layer but not connected to any supply flow chain",
                })

    complete_chains = _dedupe_chain_entries(complete_chains)
    provisional_chains = _dedupe_chain_entries(provisional_chains)
    broken_chains = _dedupe_chain_entries(broken_chains)

    complete_chains.sort(key=lambda c: -c["hops"])
    provisional_chains.sort(key=lambda c: -c["hops"])
    broken_chains.sort(key=lambda c: -c["hops"])

    if not complete_chains and not provisional_chains and not broken_chains:
        health = "No flow paths found. Graph has observations but no supply chain connectivity."
    elif not complete_chains and provisional_chains:
        longest = provisional_chains[0]
        health = (
            f"No grounded complete chain. {len(provisional_chains)} provisional chain(s), "
            f"longest {longest['source']} → {longest['sink']} ({longest['hops']} hops)."
        )
    elif not complete_chains:
        longest = broken_chains[0]
        health = f"No grounded complete chain. Longest partial: {longest['source']} → {longest['sink']} ({longest['hops']} hops, breaks at {longest['sink']})"
    else:
        longest = complete_chains[0]
        n_complete = len(complete_chains)
        n_broken = len(broken_chains)
        n_provisional = len(provisional_chains)
        health = f"{n_complete} grounded complete chain(s), longest {longest['hops']} hops. {n_broken} broken chain(s)."
        if n_provisional:
            health += f" {n_provisional} provisional chain(s)."
        if bridge_candidates:
            health += f" {len(bridge_candidates)} entity(s) in governance but not in flow."

    return {
        "completeChains": complete_chains[:4],
        "provisionalChains": provisional_chains[:4],
        "brokenChains": broken_chains[:4],
        "bridgeCandidates": bridge_candidates[:6],
        "healthSummary": health,
    }


def _branch_path_assessment(
    plan: BranchPlan,
    result: ToolActionResult,
) -> tuple[Literal["confirmed", "best_supported_indirect", "rejected", "still_open", "not_applicable"], str]:
    if result.path_assessment != "still_open" or result.path_assessment_note:
        return result.path_assessment, result.path_assessment_note
    text = " ".join(
        filter(
            None,
            [
                result.human_summary,
                result.failure.message if result.failure else "",
                " ".join(result.uncovered_questions),
                " ".join(result.remaining_frontier),
                " ".join(result.discovered_frontier),
            ],
        )
    )
    if _NEGATIVE_PATH_SIGNALS.search(text):
        if plan.path_kind in {"direct", "alternative", "indirect", "bridge"}:
            return "rejected", "Branch found no supporting evidence for this candidate path."
    if result.graph_fragments and (
        plan.path_kind in {"indirect", "alternative", "bridge"} and _INDIRECT_PATH_SIGNALS.search(text)
    ):
        return "best_supported_indirect", "Branch surfaced an indirect or intermediary route that currently fits the evidence best."
    if result.closure_status == "closed" and result.graph_fragments:
        return "confirmed", "Branch closed its target path with a usable graph fragment."
    return "still_open", ""


def _dependent_descendant_branch_ids(
    branches_by_id: dict[str, BranchState],
    branch: BranchState,
) -> list[str]:
    descendants: list[str] = [branch.branch_id]
    queue: list[str] = [branch.branch_id]
    while queue:
        current_id = queue.pop(0)
        for candidate in branches_by_id.values():
            if candidate.parent_hypothesis_id != current_id:
                continue
            if candidate.hypothesis_relation != "dependent_followup":
                continue
            if candidate.branch_id in descendants:
                continue
            descendants.append(candidate.branch_id)
            queue.append(candidate.branch_id)
    return descendants


def _derive_candidate_paths(state: dict[str, Any]) -> list[CandidatePath]:
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])]
    branches_by_id = {branch.branch_id: branch for branch in branches}
    fragment_by_branch: dict[str, list[GraphFragment]] = {}
    for fragment in fragments:
        fragment_by_branch.setdefault(fragment.branch_id, []).append(fragment)
    supply_contract = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    flow_rels: set[str] = set(supply_contract.path_carried_edges) if supply_contract else {
        "PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM",
    }
    candidates: list[CandidatePath] = []
    display_roots = [
        branch
        for branch in branches
        if branch.hypothesis_relation in {"root", "independent_child"}
    ] or branches
    for branch in display_roots:
        family_branch_ids = _dependent_descendant_branch_ids(branches_by_id, branch)
        family_branches = [
            branches_by_id[branch_id]
            for branch_id in family_branch_ids
            if branch_id in branches_by_id
        ]
        branch_fragments: list[GraphFragment] = []
        for family_branch in family_branches:
            branch_fragments.extend(fragment_by_branch.get(family_branch.branch_id, []) or family_branch.graph_fragments)
        observations = [obs for fragment in branch_fragments for obs in fragment.observations]
        flow_observations = [obs for obs in observations if obs.relation in flow_rels]
        path_subjects = [obs.subject for obs in flow_observations if obs.subject]
        path_objects = [obs.object for obs in flow_observations if obs.object]
        via_nodes = _dedupe_preserve_order([node for node in path_objects[:-1] if node])[:6]
        start_node = path_subjects[0] if path_subjects else ""
        end_node = path_objects[-1] if path_objects else ""
        if not start_node and branch_fragments:
            start_node = branch_fragments[0].start_node_id or (branch_fragments[0].observations[0].subject if branch_fragments[0].observations else "")
        if not end_node and branch_fragments:
            last_fragment = branch_fragments[-1]
            end_node = last_fragment.end_node_id or (last_fragment.observations[-1].object if last_fragment.observations else "")
        evidence_status = _evidence_status_for_observations(observations)
        confidence = (
            round(sum(item.confidence for item in observations) / len(observations), 3)
            if observations
            else round(branch.quality_score, 3)
        )
        status: Literal["confirmed", "best_supported_indirect", "rejected", "still_open"] = "still_open"
        if branch.path_assessment in {"confirmed", "best_supported_indirect", "rejected"}:
            status = branch.path_assessment
        elif branch.status == "completed" and branch_fragments:
            status = "confirmed"
        summary = branch.path_assessment_note or branch.human_summary or branch.hypothesis
        unresolved_hops = _dedupe_preserve_order(
            [item for family_branch in family_branches for item in (family_branch.remaining_frontier + family_branch.open_questions)]
        )[:6]
        candidates.append(
            CandidatePath(
                pathId=branch.branch_id,
                pathKind=branch.path_kind,
                status=status,
                title=branch.human_question,
                summary=summary,
                startNode=start_node,
                endNode=end_node,
                viaNodes=via_nodes,
                supportingBranchIds=family_branch_ids[:8],
                supportingFragmentIds=[fragment.fragment_id for fragment in branch_fragments[:6]],
                evidenceStatus=evidence_status,  # type: ignore[arg-type]
                confidence=confidence,
                unresolvedHops=unresolved_hops,
            )
        )
    candidates.sort(
        key=lambda item: (
            {"confirmed": 0, "best_supported_indirect": 1, "still_open": 2, "rejected": 3}.get(item.status, 9),
            {"direct": 0, "indirect": 1, "alternative": 2, "bridge": 3, "overview": 4}.get(item.path_kind, 9),
            -item.confidence,
        )
    )
    return candidates[:10]


def _hypothesis_status(branch: BranchState) -> Literal["active", "accepted", "rejected", "stalled", "deferred"]:
    if branch.path_assessment == "rejected":
        return "rejected"
    if _structural_frontier_items(list(branch.remaining_frontier) + list(branch.open_questions)):
        return "active"
    if branch.path_assessment in {"confirmed", "best_supported_indirect"} and (
        branch.status == "completed" or branch.resolved_frontier
    ):
        return "accepted"
    if branch.status == "completed" and branch.graph_fragments:
        return "accepted"
    if branch.stop_reason and "exhausted" in branch.stop_reason.lower():
        return "stalled"
    if branch.status == "blocked":
        return "deferred"
    return "active"


def _root_hypothesis_id(branches_by_id: dict[str, BranchState], branch: BranchState) -> str:
    seen: set[str] = set()
    current = branch
    while current.parent_hypothesis_id:
        parent_id = current.parent_hypothesis_id
        if parent_id in seen:
            break
        seen.add(parent_id)
        parent = branches_by_id.get(parent_id)
        if parent is None:
            return parent_id
        current = parent
    return current.branch_id


def _derive_hypotheses(state: dict[str, Any]) -> list[HypothesisState]:
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])]
    cycle = int(state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)
    branches_by_id = {branch.branch_id: branch for branch in branches}

    fragment_ids_by_branch: dict[str, list[str]] = {}
    broad_fragment_ids_by_branch: dict[str, list[str]] = {}
    fragment_support_by_branch: dict[str, set[str]] = {}
    fragment_confidence_by_branch: dict[str, list[float]] = {}
    finding_ids_by_branch: dict[str, list[str]] = {}
    for fragment in fragments:
        fragment_ids_by_branch.setdefault(fragment.branch_id, []).append(fragment.fragment_id)
        if fragment.fragment_type in {"overview_chain", "alternative_chain", "hypothesis"}:
            broad_fragment_ids_by_branch.setdefault(fragment.branch_id, []).append(fragment.fragment_id)
        fragment_support_by_branch.setdefault(fragment.branch_id, set()).update(
            obs.support_type for obs in fragment.observations
        )
        fragment_confidence_by_branch.setdefault(fragment.branch_id, []).extend(
            obs.confidence for obs in fragment.observations
        )
    for finding in _load_learned_findings(state):
        finding_ids_by_branch.setdefault(finding.hypothesis_id or finding.branch_id, []).append(finding.finding_id)
        if finding.branch_id:
            finding_ids_by_branch.setdefault(finding.branch_id, []).append(finding.finding_id)

    hypotheses: list[HypothesisState] = []
    for branch in branches:
        support_types = fragment_support_by_branch.get(branch.branch_id, set())
        if not support_types:
            evidence_status: Literal["verified", "reasoned", "mixed", "hypothesis"] = "hypothesis"
        elif support_types == {"document"}:
            evidence_status = "verified"
        elif support_types == {"hypothesis"}:
            evidence_status = "hypothesis"
        elif "document" in support_types and len(support_types) > 1:
            evidence_status = "mixed"
        else:
            evidence_status = "reasoned"

        confidences = fragment_confidence_by_branch.get(branch.branch_id, [])
        confidence = (
            round(sum(confidences) / len(confidences), 3)
            if confidences
            else round(branch.quality_score, 3)
        )

        resolved_frontier = _dedupe_preserve_order(branch.resolved_frontier)[:8]
        remaining_frontier = [
            item for item in _dedupe_preserve_order(branch.remaining_frontier)
            if item not in resolved_frontier
        ][:8]
        discovered_frontier = [
            item for item in _dedupe_preserve_order(branch.discovered_frontier)
            if item not in resolved_frontier and item not in remaining_frontier
        ][:8]
        open_questions = [
            item for item in _dedupe_preserve_order(branch.open_questions)
            if item not in resolved_frontier
        ][:8]
        hypotheses.append(
            HypothesisState(
                hypothesisId=branch.branch_id,
                statement=branch.hypothesis,
                parentHypothesisId=branch.parent_hypothesis_id,
                rootHypothesisId=_root_hypothesis_id(branches_by_id, branch),
                hypothesisRelation=branch.hypothesis_relation,
                spawnReason=branch.spawn_reason,
                independenceBasis=branch.independence_basis,
                pathKind=branch.path_kind,
                status=_hypothesis_status(branch),
                summary=branch.path_assessment_note or branch.human_summary or branch.hypothesis,
                humanQuestion=branch.human_question,
                closureTarget=branch.closure_target,
                activeRoles=branch.assigned_roles,
                sourceBranchIds=[branch.branch_id],
                sourceFragmentIds=fragment_ids_by_branch.get(branch.branch_id, [])[:8],
                broadPictureFragmentIds=broad_fragment_ids_by_branch.get(branch.branch_id, [])[:4],
                evidenceStatus=evidence_status,
                confidence=confidence,
                resolvedFrontier=resolved_frontier,
                remainingFrontier=remaining_frontier,
                discoveredFrontier=discovered_frontier,
                openQuestions=open_questions,
                findingIds=_dedupe_preserve_order(finding_ids_by_branch.get(branch.branch_id, []))[:12],
                loopCount=max(1, len(branch.hypothesis_turns) or len(branch.tool_results)),
                lastUpdatedCycle=cycle,
                turnIds=[turn.turn_id for turn in branch.hypothesis_turns[:12]],
            )
        )

    hypotheses.sort(
        key=lambda item: (
            {"active": 0, "accepted": 1, "rejected": 2, "stalled": 3, "deferred": 4}.get(item.status, 9),
            {"overview": 0, "direct": 1, "indirect": 2, "alternative": 3, "bridge": 4}.get(item.path_kind, 9),
            -item.confidence,
        )
    )
    return hypotheses[:16]


def _role_coverage_preview(state: dict[str, Any]) -> dict[str, list[str]]:
    """Sample 3 edges per role for the supervisor, so every role is visible."""
    obs_raw = state.get(STATE_WORKING_GRAPH) or []
    supply_contract = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    governance_contract = ROLE_CONTRACTS.get(ROLE_GOVERNANCE)
    policy_contract = ROLE_CONTRACTS.get(ROLE_POLICY)

    supply_rels = set(supply_contract.allowed_edge_types) if supply_contract else set()
    governance_rels = set(governance_contract.allowed_edge_types) if governance_contract else set()
    policy_rels = set(policy_contract.allowed_edge_types) if policy_contract else set()

    buckets: dict[str, list[str]] = {"supply_chain": [], "governance": [], "policy": [], "other": []}
    for raw in obs_raw:
        o = raw if isinstance(raw, dict) else {}
        rel = o.get("relation", "")
        subj = o.get("subject", "")
        obj = o.get("object", "")
        conf = o.get("confidence", 0.0)
        st = o.get("supportType", "")
        edge_str = f"{subj} --{rel}--> {obj} [{st} {conf}]"
        if rel in supply_rels:
            buckets["supply_chain"].append(edge_str)
        elif rel in governance_rels:
            buckets["governance"].append(edge_str)
        elif rel in policy_rels:
            buckets["policy"].append(edge_str)
        else:
            buckets["other"].append(edge_str)

    result: dict[str, list[str]] = {}
    for role, edges in buckets.items():
        if not edges:
            result[role] = []
            continue
        step = max(1, len(edges) // 3)
        result[role] = [edges[i] for i in range(0, len(edges), step)][:3]
    return result


def _frontier_summary(state: dict[str, Any]) -> list[dict[str, Any]]:
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])]
    summary: list[dict[str, Any]] = []
    for branch in branches[-8:]:
        remaining = [item for item in branch.remaining_frontier if item][:4]
        if not remaining and branch.status == "completed":
            continue
        summary.append(
            {
                "branchId": branch.branch_id,
                "pathKind": branch.path_kind,
                "pathAssessment": branch.path_assessment,
                "status": branch.status,
                "closureTarget": branch.closure_target,
                "knownContext": branch.known_context[:4],
                "remainingFrontier": remaining or branch.open_questions[:4],
                "resolvedFrontier": branch.resolved_frontier[:4],
                "stopReason": branch.stop_reason,
            }
        )

    synthesis_raw = state.get(STATE_INVESTIGATOR_SYNTHESIS)
    if synthesis_raw:
        try:
            synthesis = InvestigatorSynthesis.model_validate(synthesis_raw)
            if synthesis.bridge_gaps or synthesis.bridge_questions:
                summary.append({
                    "branchId": "_cross_branch_synthesis",
                    "status": "needs_followup",
                    "closureTarget": synthesis.synthesis_summary,
                    "knownContext": synthesis.connected_pairs[:4],
                    "remainingFrontier": synthesis.bridge_gaps[:4],
                    "resolvedFrontier": [],
                    "stopReason": "",
                    "bridgeQuestions": synthesis.bridge_questions[:4],
                    "disconnectedPairs": synthesis.disconnected_pairs[:4],
                })
        except Exception:
            pass

    if fragments and len(branches) >= 2:
        boundary_view: list[dict[str, str]] = []
        for fragment in fragments[-8:]:
            if not fragment.observations:
                continue
            first_obs = fragment.observations[0]
            last_obs = fragment.observations[-1]
            boundary_view.append({
                "fragmentId": fragment.fragment_id,
                "branchId": fragment.branch_id,
                "title": fragment.title,
                "entryNode": first_obs.subject,
                "entryNodeType": first_obs.subject_type,
                "exitNode": last_obs.object,
                "exitNodeType": last_obs.object_type,
            })
        if boundary_view:
            summary.append({
                "branchId": "_fragment_boundaries",
                "status": "info",
                "closureTarget": "Cross-branch fragment boundary view for connectivity reasoning.",
                "knownContext": [],
                "remainingFrontier": [],
                "resolvedFrontier": [],
                "stopReason": "",
                "fragmentBoundaries": boundary_view,
            })

    if summary:
        return summary
    gaps = _open_gap_texts(state)[:6]
    if not gaps:
        return []
    return [
        {
            "branchId": "",
            "status": "active",
            "closureTarget": "Resolve currently open graph gaps.",
            "knownContext": [],
            "remainingFrontier": gaps,
            "resolvedFrontier": [],
            "stopReason": "",
        }
    ]


def _requires_supply_chain_profile(action: ToolAction) -> bool:
    return "supply_chain" in action.assigned_roles and action.expected_fragment_type in {
        "overview_chain",
        "chain_fragment",
        "alternative_chain",
    }


def _relevant_graph_context(
    state: dict[str, Any],
    *,
    target_entities: list[str],
    branch_question: str,
    limit: int = 6,
) -> list[str]:
    observations = [GraphObservation.model_validate(item) for item in (state.get(STATE_WORKING_GRAPH) or [])]
    tokens = {
        token.lower()
        for token in _TOKEN_RE.findall(" ".join(target_entities + [branch_question]))
        if token
    }
    if not observations:
        return []
    scored: list[tuple[int, str]] = []
    for observation in observations:
        haystack = " ".join(
            [
                observation.subject,
                observation.subject_type,
                observation.relation,
                observation.object,
                observation.object_type,
                " ".join(observation.qualifiers),
                observation.support_note,
            ]
        ).lower()
        score = sum(1 for token in tokens if token in haystack)
        if score <= 0:
            continue
        scored.append((score, f"{observation.subject} --{observation.relation}--> {observation.object}"))
    scored.sort(key=lambda item: (-item[0], item[1]))
    return [item[1] for item in scored[:limit]]


def _contextual_query_text(
    state: dict[str, Any],
    *,
    human_question: str,
    human_action: str,
    path_kind: str,
    closure_target: str,
    target_entities: list[str],
    seed_known_context: list[str],
    frontier: list[str],
) -> str:
    known_context = _dedupe_preserve_order(
        list(seed_known_context)
        + _relevant_graph_context(
            state,
            target_entities=target_entities,
            branch_question=human_question,
        )
    )[:8]
    learned_findings = _relevant_learned_findings(
        state,
        target_entities=target_entities,
        branch_question=human_question,
        limit=4,
    )
    frontier = _dedupe_preserve_order(frontier)[:6]
    lines = [
        f"Question: {human_question}",
        f"Action: {human_action}",
        f"Candidate path type: {path_kind}",
        f"Closure target: {closure_target}",
        "What the system already knows:",
    ]
    if known_context:
        lines.extend(f"- {item}" for item in known_context)
    else:
        lines.append("- No directly relevant typed graph observations are currently in shared memory.")
    lines.append("What the system recently learned:")
    if learned_findings:
        lines.extend(
            f"- [{item.finding_kind}] {item.summary}"
            for item in learned_findings
        )
    else:
        lines.append("- No recent learned findings were carried forward for this slot.")
    lines.append("Current frontier:")
    if frontier:
        lines.extend(f"- {item}" for item in frontier)
    else:
        lines.append("- No explicit frontier was carried forward; recover the next meaningful missing link.")
    lines.append("Return typed graph fragments that either close this frontier or explain exactly what still remains open.")
    return "\n".join(lines)


def _tool_prompt(
    action: ToolAction,
    role_contracts: list[dict[str, Any]],
    shared_memory: dict[str, Any],
    source_mode: str,
) -> str:
    extra_rules = ""
    if _requires_supply_chain_profile(action):
        extra_rules = SUPPLY_CHAIN_GRAPH_RULES + "\n" + SUPPLY_CHAIN_PROFILE_CONTRACT + "\n"
    role_playbook = ""
    if len(action.assigned_roles) == 1:
        role = action.assigned_roles[0]
        role_playbook = _role_playbook_text(role) + "\n\n"
    prompt = (
        "You are a graph-native investigation tool inside AgentLoom.\n"
        "You are operating inside a graph system specialized in meaningful connected chains of typed entities and typed relations.\n"
        + role_playbook
        + "Return exactly one JSON object and nothing else.\n"
        "Do not wrap the JSON in markdown fences.\n"
        "Do not add commentary before or after the JSON.\n"
        "The JSON must match ToolActionResult exactly.\n"
        "There is one graph protocol only: the same ToolActionResult -> GraphFragment format is used for the first skeleton, later enrichment, and graph mutation.\n"
        "Do not invent a special answer shape for overview questions. Even the first whole-picture answer must be ordinary graphFragments that the runtime will later append to or replace under the same rules.\n"
        "Every factual answer must be carried by graphFragments -> observations.\n"
        "Every useful non-graph answer must also be carried by learnedFindings. Negative answers, procurement-model findings, and stable contextual knowledge are still reusable results.\n"
        "A broad question may require multiple graph fragments and a long typed observation array; do not collapse it into one entity-relation-entity pair.\n"
        "Every observation must be typed as subjectType --relation--> objectType using only relations allowed by the supplied role contracts.\n"
        "The action belongs to a candidate path class (`pathKind`). You must return `pathAssessment` saying whether that path is confirmed, rejected, still open, or currently best-supported as an indirect route.\n"
        "Preserve useful but uncertain nodes and edges by lowering confidence or using supportType=hypothesis instead of deleting them.\n"
        "Do not confuse operational supply-chain flow with contractual procurement. Procurement model, sourcing strategy, offtake, localization, recycling strategy, or vertical integration without a concrete facility-to-facility or product-stage edge belongs in learnedFindings and frontier, not in fake SUPPLIES_TO or TRANSFORMS_INTO edges.\n"
        "Do not spend a web-grounded turn on stable background facts that a competent model already knows, such as well-established jurisdiction-wide regulatory frameworks, unless the action explicitly asks for current changes, enforcement, or company-specific response.\n"
        "Use the action's closureTarget, knownContext, and frontier to understand what the system already knows and what exact graph closure is being attempted.\n"
        "The working graph is mutable shared memory. If you return `patchMode=replace_node` or `replace_fragment`, the runtime will apply that patch before later reasoning steps. Treat replacement as a real graph mutation, not as commentary.\n"
        "When replacing a broad node or old fragment, make the new fragment structurally able to occupy that old slot in the graph. Later agents will reason from the replacement graph now stored in shared memory.\n"
        "Interpret broad request styles explicitly.\n"
        "- If the question asks for a full path or whole picture, return a connected multi-hop graph fragment, not disconnected facts.\n"
        "- If the question asks for alternatives, return separate fragments or fragment families for materially different routes and use alternativeGroupId when relevant.\n"
        "- If the current endpoints are broad anchors like countries or sectors, prefer resolving them into concrete source-side or sink-side entities or facilities.\n"
        "- If the question is about what sits between A and B, return the bridge layer or explicitly state that the bridge remains open.\n"
        "- For broad overview answers, use frontier fields deliberately: keep same-hypothesis refinement in remainingFrontier, and put newly revealed independent route families, workaround routes, or endpoint-resolution lines into discoveredFrontier.\n"
        "- If the user asked for alternatives and you return only one main route, discoveredFrontier should usually include at least one materially different alternative-route question.\n"
        "- If your current overview already mixes multiple route families, discoveredFrontier should call that out explicitly so the planner can split them into separate hypotheses later.\n"
        "If you do not have enough evidence, return status=partial or blocked with uncoveredQuestions and an optional failure object.\n"
        "Never invent tools, relation names, fragment types, or entity types outside the supplied contracts.\n"
        "At most 3 graphFragments and at most 16 observations total.\n"
        f"Source mode: {source_mode}\n\n"
        + CONFIDENCE_CALIBRATION
        + "\n"
        + EDGE_DIRECTION_RULES
        + "\n"
        + GRAPH_OBSERVATION_CONTRACT
        + "\n"
        + GRAPH_FRAGMENT_CONTRACT
        + "\n"
        + TOOL_ACTION_RESULT_CONTRACT
        + "\n"
        + REASONING_MODES_GUIDANCE
        + "\n"
        + extra_rules
        + TOOL_ACTION_RESULT_EXAMPLES
        + "\n\n"
        f"Tool action:\n{json.dumps(action.model_dump(mode='json', by_alias=True), ensure_ascii=False)}\n\n"
        f"Role contracts:\n{json.dumps(role_contracts, ensure_ascii=False)}\n\n"
        f"Shared memory snapshot:\n{json.dumps(shared_memory, ensure_ascii=False)}"
    )
    return prompt


def _memory_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    graph = [GraphObservation.model_validate(item) for item in (tool_context.state.get(STATE_WORKING_GRAPH) or [])]
    if not graph:
        return _tool_failure_result(
            action,
            "blocked",
            "Shared graph memory does not yet contain typed observations relevant to this question.",
            missing_inputs=["working_graph"],
        )
    query_tokens = {token.lower() for token in _TOKEN_RE.findall(action.query_text)}
    target_tokens = {token.lower() for token in _TOKEN_RE.findall(" ".join(action.target_entities))}
    matched: list[GraphObservation] = []
    for observation in graph:
        haystack = " ".join(
            [
                observation.subject,
                observation.subject_type,
                observation.relation,
                observation.object,
                observation.object_type,
                " ".join(observation.qualifiers),
                observation.support_note,
            ]
        ).lower()
        if query_tokens and not any(token in haystack for token in query_tokens | target_tokens):
            continue
        matched.append(observation)
    if not matched:
        return _tool_failure_result(
            action,
            "blocked",
            "Shared graph memory does not answer this question yet.",
            missing_inputs=["relevant_graph_fragment"],
        )
    fragment = GraphFragment(
        fragmentId=f"{action.branch_id}-memory",
        fragmentType=action.expected_fragment_type,  # type: ignore[arg-type]
        title=f"Memory answer for {action.branch_id}",
        humanQuestion=action.human_question,
        humanSummary=f"Recovered {len(matched[:10])} relevant typed observations from shared memory.",
        branchId=action.branch_id,
        observations=[item.model_copy(update={"branch_id": action.branch_id}) for item in matched[:10]],
        completeness="partial",
        qualityScore=0.4,
    )
    return ToolActionResult(
        actionId=action.action_id,
        branchId=action.branch_id,
        toolName=action.tool_name,
        pathAssessment="still_open",
        pathAssessmentNote="Shared memory recovered relevant graph fragments but did not by itself settle this candidate path.",
        status="success",
        humanSummary=fragment.human_summary,
        closureStatus="advanced" if action.frontier else "closed",
        resolvedFrontier=[],
        remainingFrontier=list(action.frontier),
        discoveredFrontier=[],
        graphFragments=[fragment],
        uncoveredQuestions=[],
        sourceDocuments=[],
        failure=None,
    )


def _run_question_to_graph_llm(
    action: ToolAction,
    tool_context,
    *,
    retrieval: bool,
    web_search: bool = False,
    source_mode: str,
) -> ToolActionResult:
    role_contracts = _selected_role_contract_payload(action.assigned_roles)
    shared_memory = read_shared_memory(tool_context)
    allowed_edges = _allowed_edges_for_roles(action.assigned_roles)
    prompt = _tool_prompt(action, role_contracts, shared_memory, source_mode)
    # Grounded tools (Vertex search / Google Search): use the same lite stack as retrieval for JSON extraction too.
    structured_model = GROUNDING_MODEL if (web_search or retrieval) else INVESTIGATOR_MODEL
    try:
        from google.genai import Client, types as live_types
    except Exception as exc:
        return _tool_failure_result(action, "failed", str(exc), code="agent_failed")

    config = _vertex_grounding_config()
    used_resource_fallback = False

    def _generate_with_retry(*, model: str, contents: str, config_obj: Any) -> Any:
        nonlocal used_resource_fallback
        try:
            return client.models.generate_content(model=model, contents=contents, config=config_obj)
        except Exception as exc:
            if not _is_resource_exhausted_error(exc):
                raise
            fallback_model = _model_with_resource_fallback(model, used_fallback=used_resource_fallback)
            if fallback_model == model:
                raise
            used_resource_fallback = True
            return client.models.generate_content(model=fallback_model, contents=contents, config=config_obj)

    retrieved_sources: list[dict[str, Any]] = []
    extraction_prompt = prompt
    response: Any = None
    try:
        client = Client(vertexai=True, project=config.get("project") or os.getenv("GOOGLE_CLOUD_PROJECT"), location=config.get("location") or os.getenv("GOOGLE_CLOUD_LOCATION", "global"))
        if web_search or retrieval:
            retrieval_prompt = (
                "Retrieve only factual evidence relevant to the following investigation question. "
                "Do not output JSON. Return a concise grounded summary using retrieved evidence only.\n\n"
                f"Question: {action.human_question}"
            )
            if web_search:
                retrieval_tools = [live_types.Tool(google_search=live_types.GoogleSearch())]
            else:
                retrieval_tools = [
                    live_types.Tool(
                        retrieval=live_types.Retrieval(
                            vertex_ai_search=live_types.VertexAISearch(
                                datastore=config["datastorePath"],
                                max_results=4,
                            )
                        )
                    )
                ]
            retrieval_response = _generate_with_retry(
                model=GROUNDING_MODEL,
                contents=retrieval_prompt,
                config_obj=live_types.GenerateContentConfig(
                    temperature=0,
                    max_output_tokens=RETRIEVAL_MAX_OUTPUT_TOKENS,
                    tools=retrieval_tools,
                ),
            )
            if retrieval_response.candidates:
                candidate = retrieval_response.candidates[0]
                metadata = getattr(candidate, "grounding_metadata", None)
                for chunk in getattr(metadata, "grounding_chunks", None) or []:
                    retrieved = getattr(chunk, "retrieved_context", None)
                    web = getattr(chunk, "web", None)
                    if retrieved is not None:
                        retrieved_sources.append(
                            {
                                "title": getattr(retrieved, "title", "") or "",
                                "uri": getattr(retrieved, "uri", "") or getattr(retrieved, "document_name", "") or "",
                                "text": getattr(retrieved, "text", "") or "",
                            }
                        )
                    elif web is not None:
                        retrieved_sources.append(
                            {
                                "title": getattr(web, "title", "") or "",
                                "uri": getattr(web, "uri", "") or "",
                                "text": "",
                            }
                        )
                for support in getattr(metadata, "grounding_supports", None) or []:
                    segment = getattr(support, "segment", None)
                    if segment and getattr(segment, "text", None):
                        indices = getattr(support, "grounding_chunk_indices", []) or []
                        for idx in indices:
                            if 0 <= idx < len(retrieved_sources) and not retrieved_sources[idx].get("text"):
                                retrieved_sources[idx]["text"] = getattr(segment, "text", "")
            extraction_prompt = (
                _tool_prompt(action, role_contracts, shared_memory, source_mode)
                + "\n\nRetrieved evidence summary:\n"
                + (getattr(retrieval_response, "text", "") or "")
                + "\n\nRetrieved evidence chunks:\n"
                + json.dumps(_dedupe_sources(retrieved_sources), ensure_ascii=False)
            )
            response = _generate_with_retry(
                model=structured_model,
                contents=extraction_prompt,
                config_obj=live_types.GenerateContentConfig(
                    temperature=0,
                    response_mime_type="application/json",
                    response_schema=ToolActionResult,
                    max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
                ),
            )
        else:
            response = _generate_with_retry(
                model=INVESTIGATOR_MODEL,
                contents=prompt,
                config_obj=live_types.GenerateContentConfig(
                    temperature=0,
                    response_mime_type="application/json",
                    response_schema=ToolActionResult,
                    max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
                ),
            )
    except Exception as exc:
        return _tool_failure_result(
            action,
            "failed",
            str(exc),
            code="agent_failed",
            missing_inputs=["web_search" if web_search else ("vertex_grounding" if retrieval else "vertex_model")],
        )

    support_label = "mixed" if web_search else ("document" if retrieval else "model_knowledge")

    def _parse_structured(resp: Any) -> ToolActionResult:
        return _parse_tool_result_payload(
            _response_json_text(resp),
            action,
            allowed_edges,
            support_label,
        )

    json_retry_suffix = (
        "\n\nYour previous structured JSON failed validation or was truncated. "
        "Return ONLY valid JSON matching ToolActionResult. "
        "Use at most 2 graphFragments and at most 12 observations in total across all fragments. "
        "Keep strings short; escape double quotes inside JSON strings."
    )

    try:
        result = _parse_structured(response)
    except Exception as first_exc:
        try:
            response2 = _generate_with_retry(
                model=structured_model,
                contents=extraction_prompt + json_retry_suffix,
                config_obj=live_types.GenerateContentConfig(
                    temperature=0,
                    response_mime_type="application/json",
                    response_schema=ToolActionResult,
                    max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
                ),
            )
            result = _parse_structured(response2)
        except Exception:
            return _tool_failure_result(
                action, "failed", f"Invalid tool JSON: {first_exc}", code="agent_failed"
            )

    source_docs = list(result.source_documents)
    if retrieval:
        source_docs.extend(
            value.get("uri", "")
            for value in _dedupe_sources(retrieved_sources)
            if value.get("uri", "")
        )
    return result.model_copy(update={"source_documents": _dedupe_preserve_order(source_docs)[:10]})


def _reason_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    return _run_question_to_graph_llm(action, tool_context, retrieval=False, source_mode="model")


def _corpus_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    search = search_corpus(action.query_text, tool_context, max_results=4)
    if not search.get("matches"):
        return _tool_failure_result(
            action,
            "blocked",
            "Supplied corpus does not contain a relevant match for this branch question.",
            missing_inputs=["corpus_match"],
        )
    role_contracts = _selected_role_contract_payload(action.assigned_roles)
    shared_memory = read_shared_memory(tool_context)
    snippets = []
    for match in search.get("matches", [])[:4]:
        doc = read_corpus_document(match["documentId"], tool_context)
        if doc.get("ok"):
            snippets.append(doc["document"])
    prompt = _tool_prompt(action, role_contracts, shared_memory, "corpus") + "\n\nCorpus evidence:\n" + json.dumps(snippets, ensure_ascii=False)
    try:
        from google.genai import Client, types as live_types
        client = Client(vertexai=True, project=os.getenv("GOOGLE_CLOUD_PROJECT"), location=os.getenv("GOOGLE_CLOUD_LOCATION", "global"))
        response = client.models.generate_content(
            model=INVESTIGATOR_MODEL,
            contents=prompt,
            config=live_types.GenerateContentConfig(
                temperature=0,
                response_mime_type="application/json",
                response_schema=ToolActionResult,
                max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
            ),
        )
        result = _parse_tool_result_payload(
            _response_json_text(response),
            action,
            _allowed_edges_for_roles(action.assigned_roles),
            "document",
        )
        source_docs = [item.get("sourceUri", "") for item in snippets if item.get("sourceUri")]
        return result.model_copy(update={"source_documents": _dedupe_preserve_order(source_docs)[:10]})
    except Exception as exc:
        return _tool_failure_result(action, "failed", str(exc), code="agent_failed")


def _vertex_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    config = _vertex_grounding_config()
    if not config.get("available"):
        return _tool_failure_result(
            action,
            "blocked",
            config.get("reason", "Vertex grounded search is unavailable."),
            missing_inputs=["vertex_grounding"],
        )
    return _run_question_to_graph_llm(action, tool_context, retrieval=True, source_mode="vertex_grounded")


def _web_search_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    ws = _web_search_available()
    if not ws.get("available"):
        return _tool_failure_result(
            action,
            "blocked",
            ws.get("reason", "Google Search grounding is unavailable."),
            missing_inputs=["web_search"],
        )
    return _run_question_to_graph_llm(
        action,
        tool_context,
        retrieval=False,
        web_search=True,
        source_mode="web_grounded",
    )


def retrieve(action: ToolAction, tool_context) -> ToolActionResult:
    memory_result = _memory_question_to_graph(action, tool_context)
    best_result = memory_result
    if memory_result.status == "success" and memory_result.graph_fragments:
        if memory_result.closure_status == "closed" or not memory_result.remaining_frontier:
            return memory_result.model_copy(update={"tool_name": "retrieve"})
        best_result = memory_result

    request = AgentLoomRequest.model_validate(tool_context.state.get(STATE_REQUEST) or {})
    if request.corpus_documents:
        corpus_result = _corpus_question_to_graph(action, tool_context)
        if corpus_result.status in {"success", "partial"} and (
            corpus_result.graph_fragments or corpus_result.source_documents
        ):
            return corpus_result.model_copy(update={"tool_name": "retrieve"})
        best_result = corpus_result

    if _vertex_grounding_config().get("available"):
        vertex_result = _vertex_question_to_graph(action, tool_context)
        if vertex_result.status in {"success", "partial"} and (
            vertex_result.graph_fragments or vertex_result.source_documents
        ):
            return vertex_result.model_copy(update={"tool_name": "retrieve"})
        best_result = vertex_result

    return best_result.model_copy(update={"tool_name": "retrieve"})


def reason(action: ToolAction, tool_context) -> ToolActionResult:
    return _reason_question_to_graph(action, tool_context).model_copy(update={"tool_name": "reason"})


def web_search(action: ToolAction, tool_context) -> ToolActionResult:
    return _web_search_question_to_graph(action, tool_context).model_copy(update={"tool_name": "web_search"})


def _update_branch_state(
    previous: BranchState | None,
    plan: BranchPlan,
    action: ToolAction,
    result: ToolActionResult,
    *,
    turn: HypothesisTurn | None = None,
    explicit_status: Literal["active", "needs_followup", "blocked", "completed"] | None = None,
    explicit_stop_reason: str | None = None,
) -> BranchState:
    previous_quality = previous.quality_score if previous is not None else 0.0
    progress_delta = _branch_progress_delta(previous, result)
    new_quality = max(previous_quality, previous_quality + progress_delta)
    improved = progress_delta > 0.05
    stagnation = 0 if improved else ((previous.stagnation_count if previous else 0) + 1)
    status: Literal["active", "needs_followup", "blocked", "completed"] = "active"
    stop_reason = ""
    unresolved_questions = _dedupe_preserve_order(
        list(previous.open_questions if previous else []) + list(result.uncovered_questions)
    )
    resolved_frontier = _dedupe_preserve_order(
        list(previous.resolved_frontier if previous else []) + list(result.resolved_frontier)
    )
    remaining_frontier = [
        item
        for item in _dedupe_preserve_order(
            list(previous.remaining_frontier if previous else []) + list(result.remaining_frontier) + unresolved_questions
        )
        if item not in resolved_frontier
    ]
    discovered_frontier = [
        item
        for item in _dedupe_preserve_order(
            list(previous.discovered_frontier if previous else []) + list(result.discovered_frontier)
        )
        if item not in resolved_frontier
    ]
    unresolved_questions = [item for item in unresolved_questions if item not in resolved_frontier]
    path_assessment, path_assessment_note = _branch_path_assessment(plan, result)
    known_context = _dedupe_preserve_order(
        list(plan.known_context)
        + list(previous.known_context if previous else [])
        + [f"{item.subject} --{item.relation}--> {item.object}" for item in _flatten_fragments(result.graph_fragments)]
    )[:10]
    if explicit_status is not None:
        status = explicit_status
        stop_reason = explicit_stop_reason or ""
    elif path_assessment == "rejected":
        status = "completed"
        stop_reason = path_assessment_note or "Branch rejected this candidate path."
    elif result.status in {"blocked", "failed"} and not result.graph_fragments:
        status = "blocked"
        stop_reason = result.human_summary
    elif result.closure_status == "closed" and result.graph_fragments:
        status = "completed"
        stop_reason = (
            result.human_summary
            or path_assessment_note
            or "Branch closed its current hypothesis target and yielded the next frontier."
        )
    elif (
        path_assessment in {"confirmed", "best_supported_indirect"}
        and result.graph_fragments
        and not remaining_frontier
    ):
        status = "completed"
        stop_reason = path_assessment_note or "Branch produced a usable path result."
    elif stagnation >= plan.stop_after_no_improvement:
        status = "needs_followup"
        stop_reason = "Branch exhausted its current useful improvement window without closing the frontier."
    elif result.graph_fragments or remaining_frontier:
        status = "needs_followup"
    return BranchState(
        branchId=plan.branch_id,
        hypothesis=plan.hypothesis,
        parentHypothesisId=plan.parent_hypothesis_id,
        hypothesisRelation=plan.hypothesis_relation,
        spawnReason=plan.spawn_reason,
        independenceBasis=plan.independence_basis,
        pathKind=plan.path_kind,
        pathAssessment=path_assessment,
        pathAssessmentNote=path_assessment_note,
        humanQuestion=plan.human_question,
        humanSummary=result.human_summary,
        closureTarget=plan.closure_target,
        knownContext=known_context,
        assignedRoles=plan.assigned_roles,
        status=status,
        qualityScore=max(0.0, min(1.0, new_quality)),
        stagnationCount=stagnation,
        stopReason=stop_reason,
        resolvedFrontier=resolved_frontier,
        remainingFrontier=remaining_frontier,
        discoveredFrontier=discovered_frontier,
        openQuestions=unresolved_questions,
        toolActions=((previous.tool_actions if previous else []) + [action]),
        toolResults=((previous.tool_results if previous else []) + [result]),
        graphFragments=((previous.graph_fragments if previous else []) + result.graph_fragments),
        learnedFindings=((previous.learned_findings if previous else []) + result.learned_findings),
        hypothesisTurns=((previous.hypothesis_turns if previous else []) + ([turn] if turn is not None else [])),
    )


def _merge_graph_fragments(existing: list[GraphFragment], incoming: list[GraphFragment]) -> list[GraphFragment]:
    merged = {item.fragment_id: item for item in existing}
    for fragment in incoming:
        merged[fragment.fragment_id] = fragment
    return list(merged.values())


def _merge_branch_states(existing: list[BranchState], incoming: list[BranchState]) -> list[BranchState]:
    merged = {item.branch_id: item for item in existing}
    for branch in incoming:
        merged[branch.branch_id] = branch
    return list(merged.values())


class DeterministicComposerAgent(BaseAgent):
    def __init__(self) -> None:
        rubric_llm = StructuredOutputAgent(
            model=INVESTIGATOR_MODEL,
            name="rubric_llm_expander",
            description="Optionally adds rubric axes beyond deterministic heuristics (AGENTLOOM_RUBRIC_LLM).",
            instruction=RUBRIC_LLM_INSTRUCTION,
            generate_content_config=RUBRIC_LLM_CONFIG,
            output_schema=RubricLLMExtras,
            output_key=STATE_RUBRIC_LLM_RESULT,
            context_key=STATE_RUBRIC_LLM_CONTEXT,
        )
        narrative = StructuredOutputAgent(
            model=COMPOSER_MODEL,
            name="narrative_composer",
            description="Synthesizes a rubric-aligned structured narrative from InternalBrief only.",
            instruction=NARRATIVE_COMPOSER_INSTRUCTION,
            generate_content_config=NARRATIVE_COMPOSER_CONFIG,
            output_schema=StructuredNarrativeAnswer,
            output_key=STATE_STRUCTURED_NARRATIVE,
            context_key=STATE_NARRATIVE_CONTEXT,
        )
        super().__init__(name="compose", sub_agents=[rubric_llm, narrative])

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"DeterministicComposerAgent cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        request = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        working_graph = [GraphObservation.model_validate(item) for item in (ctx.session.state.get(STATE_WORKING_GRAPH) or [])]
        graph_fragments = [GraphFragment.model_validate(item) for item in (ctx.session.state.get(STATE_GRAPH_FRAGMENTS) or [])]
        branches = [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])]
        hypotheses = [HypothesisState.model_validate(item) for item in (ctx.session.state.get(STATE_HYPOTHESES) or [])]
        learned_findings = _load_learned_findings(ctx.session.state)
        logic_trace = [LogicTraceEntry.model_validate(item) for item in (ctx.session.state.get(STATE_LOGIC_TRACE) or [])]
        runtime_logs = [RuntimeLogEntry.model_validate(item) for item in (ctx.session.state.get(STATE_RUNTIME_LOG) or [])]
        candidate_paths = _derive_candidate_paths(ctx.session.state)
        open_gaps = _open_gap_texts(ctx.session.state)
        reachability_report = _graph_reachability_report(ctx.session.state)

        brief_partial = partial(
            build_internal_brief,
            request.question,
            selected_roles,
            working_graph,
            graph_fragments,
            branches,
            hypotheses,
            candidate_paths,
            learned_findings,
            open_gaps,
            reachability_report,
        )
        internal, rubric = await asyncio.gather(
            asyncio.to_thread(brief_partial),
            asyncio.to_thread(build_question_rubric, request.question, selected_roles),
        )
        internal_dump = internal.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_INTERNAL_BRIEF] = internal_dump

        rubric_llm_note = ""
        if _rubric_llm_enabled():
            ctx.session.state[STATE_RUBRIC_LLM_CONTEXT] = {
                "question": request.question,
                "selectedRoles": selected_roles,
                "heuristicRubric": rubric.model_dump(mode="json", by_alias=True),
            }
            yield _runtime_log_event(
                ctx.session.state,
                actor="rubric_llm_expander",
                phase="llm_request",
                message="Optional rubric LLM expansion is running.",
                model=INVESTIGATOR_MODEL,
                basis={"heuristicItemCount": len(rubric.items)},
            )
            async for event in self._run_named_subagent(ctx, "rubric_llm_expander"):
                yield event
            raw_ex = ctx.session.state.get(STATE_RUBRIC_LLM_RESULT) or {}
            try:
                extras = RubricLLMExtras.model_validate(raw_ex)
                rubric = merge_rubric_with_llm_extras(rubric, extras)
            except pydantic.ValidationError as exc:
                rubric_llm_note = f"AGENTLOOM_RUBRIC_LLM output invalid JSON/schema: {exc}"

        rubric_dump = rubric.model_dump(mode="json", by_alias=True)

        structured: StructuredNarrativeAnswer | None = None
        coverage_pass = False
        narrative_status: NarrativeSynthesisStatus = "deterministic_only"
        narrative_detail = rubric_llm_note
        max_extra = max(0, int(os.getenv("AGENTLOOM_NARRATIVE_MAX_RETRIES", "1")))

        if _narrative_synthesis_enabled():
            repair_hint = ""
            saw_parsed_candidate = False
            last_failed_ids: list[str] = []
            last_role_miss: list[str] = []
            for attempt in range(1 + max_extra):
                ctx.session.state[STATE_NARRATIVE_CONTEXT] = {
                    "question": request.question,
                    "internalBrief": internal_dump,
                    "questionRubric": rubric_dump,
                    "repairHint": repair_hint,
                }
                yield _runtime_log_event(
                    ctx.session.state,
                    actor="narrative_composer",
                    phase="llm_request",
                    message="Narrative composer is generating a rubric-aligned structured answer from InternalBrief.",
                    model=COMPOSER_MODEL,
                    basis={"attempt": attempt, "rubricItemCount": len(rubric.items)},
                    request={"hasRepairHint": bool(repair_hint)},
                )
                async for event in self._run_named_subagent(ctx, "narrative_composer"):
                    yield event
                raw = ctx.session.state.get(STATE_STRUCTURED_NARRATIVE) or {}
                try:
                    candidate = StructuredNarrativeAnswer.model_validate(raw)
                except pydantic.ValidationError:
                    candidate = None
                if candidate is None:
                    last_failed_ids = ["__llm_invalid_json__"]
                    break
                saw_parsed_candidate = True
                ok_cov, failed_ids = validate_narrative_coverage(candidate, rubric)
                ok_roles, role_miss = roles_mentioned_in_narrative(candidate, selected_roles)
                last_failed_ids = list(failed_ids)
                last_role_miss = list(role_miss)
                if ok_cov and ok_roles:
                    structured = candidate
                    coverage_pass = True
                    narrative_status = "ok"
                    narrative_detail = rubric_llm_note or ""
                    break
                if attempt < max_extra:
                    repair_hint = build_repair_hint(failed_ids, role_miss)

            if structured is None:
                narrative_status = "validation_failed" if saw_parsed_candidate else "llm_invalid"
                fail_detail = build_repair_hint(last_failed_ids, last_role_miss) if saw_parsed_candidate else (
                    "Model output was missing or did not match StructuredNarrativeAnswer."
                )
                if rubric_llm_note:
                    fail_detail = f"{fail_detail} | {rubric_llm_note}"
                structured = explicit_narrative_synthesis_failure(
                    reason_code=narrative_status,
                    detail=fail_detail,
                    failed_rubric_ids=last_failed_ids,
                )
                narrative_detail = fail_detail
                coverage_pass = False
        else:
            structured = None
            narrative_status = "disabled"
            extra = "AGENTLOOM_NARRATIVE_SYNTHESIS is off (deterministic conciseAnswer only)."
            narrative_detail = f"{extra} {rubric_llm_note}".strip()

        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="composition",
            message="Deterministic composer is building the final graph answer from shared memory.",
            basis={
                "question": request.question,
                "selectedRoles": selected_roles,
                "graphObservationCount": len(working_graph),
                "graphFragmentCount": len(graph_fragments),
                "branchCount": len(branches),
                "logicTraceCount": len(logic_trace),
                "runtimeLogCount": len(runtime_logs),
                "internalBriefKeys": len(internal_dump),
                "clusteredGapCount": len(internal.clustered_gaps),
                "narrativeSynthesis": _narrative_synthesis_enabled(),
                "narrativeSynthesisStatus": narrative_status,
                "coveragePass": coverage_pass,
                "rubricItemCount": len(rubric.items),
            },
        )
        final_summary = build_final_chain_summary(
            request.question,
            selected_roles,
            working_graph,
            graph_fragments,
            branches,
            hypotheses,
            candidate_paths,
            learned_findings,
            logic_trace,
            runtime_logs,
            open_gap_texts=open_gaps,
            reachability_report=reachability_report,
            structured_narrative=structured,
            internal_brief=internal,
            narrative_synthesis_status=narrative_status,
            narrative_synthesis_detail=narrative_detail,
        )
        payload = final_summary.model_dump(mode="json", by_alias=True)
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_FINAL: payload}),
            content=genai_types.Content(
                role="model",
                parts=[genai_types.Part(text=json.dumps(payload, ensure_ascii=False, indent=2))],
            ),
        )


class RoleSpecializedInvestigatorAgent(BaseAgent):
    _specialization_role: InvestigationRole = PrivateAttr()
    _planner_name: str = PrivateAttr()
    _turn_reasoner_name: str = PrivateAttr()
    _synthesizer_name: str = PrivateAttr()
    _result_state_key: str = PrivateAttr()
    _turn_reasoner_lock: asyncio.Lock = PrivateAttr(default_factory=asyncio.Lock)

    def __init__(self, specialization_role: InvestigationRole) -> None:
        planner_name = f"{specialization_role}_investigator_planner"
        turn_reasoner_name = f"{specialization_role}_hypothesis_turn_reasoner"
        synthesizer_name = f"{specialization_role}_investigator_synthesizer"
        result_state_key = RESULT_KEY_BY_AGENT[_investigator_agent_for_role(specialization_role)]
        super().__init__(
            name=_investigator_agent_for_role(specialization_role),
            sub_agents=[
                StructuredOutputAgent(
                    model=INVESTIGATOR_MODEL,
                    name=planner_name,
                    description=f"Plans graph-closing work for the {specialization_role} specialized investigator.",
                    instruction=_specialized_instruction(INVESTIGATOR_PLANNER_INSTRUCTION, specialization_role),
                    generate_content_config=INVESTIGATOR_PLANNER_CONFIG,
                    output_schema=InvestigatorPlan,
                    output_key=STATE_INVESTIGATOR_PLAN,
                    context_key=STATE_INVESTIGATOR_CONTEXT,
                ),
                StructuredOutputAgent(
                    model=INVESTIGATOR_MODEL,
                    name=turn_reasoner_name,
                    description=f"Chooses the next graph operation inside one active {specialization_role} hypothesis.",
                    instruction=_specialized_instruction(HYPOTHESIS_TURN_REASONER_INSTRUCTION, specialization_role),
                    generate_content_config=INVESTIGATOR_SYNTHESIZER_CONFIG,
                    output_schema=HypothesisTurnDecision,
                    output_key=STATE_HYPOTHESIS_TURN_DECISION,
                    context_key=STATE_HYPOTHESIS_TURN_CONTEXT,
                ),
                StructuredOutputAgent(
                    model=INVESTIGATOR_MODEL,
                    name=synthesizer_name,
                    description=f"Synthesizes parallel branch results for the {specialization_role} specialized investigator.",
                    instruction=_specialized_instruction(INVESTIGATOR_SYNTHESIZER_INSTRUCTION, specialization_role),
                    generate_content_config=INVESTIGATOR_SYNTHESIZER_CONFIG,
                    output_schema=InvestigatorSynthesis,
                    output_key=STATE_INVESTIGATOR_SYNTHESIS,
                    context_key=STATE_INVESTIGATOR_SYNTHESIS_CONTEXT,
                ),
            ],
        )
        self._specialization_role = specialization_role
        self._planner_name = planner_name
        self._turn_reasoner_name = turn_reasoner_name
        self._synthesizer_name = synthesizer_name
        self._result_state_key = result_state_key

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        tool_context = _StateToolContext(ctx.session.state)
        mission = InvestigationMission.model_validate(ctx.session.state.get(STATE_CURRENT_MISSION) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        active_roles = _dedupe_preserve_order(list(mission.assigned_roles))
        if active_roles != [self._specialization_role]:
            result = AgentResult(
                agentName=self.name,  # type: ignore[arg-type]
                missionId=mission.mission_id or "invalid-specialized-mission",
                status="failed",
                summary=(
                    f"{self.name} requires exactly one assigned role: {self._specialization_role}. "
                    f"Received {active_roles or selected_roles}."
                ),
                activeRoles=active_roles or selected_roles,
                graphDelta=[],
                answeredQuestions=[],
                gaps=[
                    f"Specialized investigator {self.name} can only execute missions for role {self._specialization_role}."
                ],
                nextQuestions=[],
                handoffRecommendations=["critic_agent"],
                failure=FailureRecord(
                    code="agent_failed",
                    message=f"Mission role mismatch for specialized investigator {self.name}.",
                    blocking=True,
                    missingInputs=["assignedRoles"],
                ),
            )
            ctx.session.state[self._result_state_key] = result.model_dump(mode="json", by_alias=True)
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="failure",
                message="Specialized investigator rejected a mission with mismatched assignedRoles.",
                basis={"mission": mission, "expectedRole": self._specialization_role, "receivedRoles": active_roles},
                result=result,
            )
            return
        role_contract = _role_contract(self._specialization_role)
        role_contracts = [role_contract.model_dump(mode="json", by_alias=True)]
        initial_pass = not bool(ctx.session.state.get(STATE_WORKING_GRAPH))
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="delegation",
            message="Investigator received a mission from supervisor and is validating it against current memory and tool availability.",
            model=INVESTIGATOR_MODEL,
            basis={
                "mission": mission,
                "selectedRoles": selected_roles,
                "initialPass": initial_pass,
                "graphObservationCount": len(ctx.session.state.get(STATE_WORKING_GRAPH) or []),
                "openGapCount": _open_gap_count(ctx.session.state),
            },
        )
        investigator_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "initialPass": initial_pass,
            "selectedRoles": selected_roles,
            "specializedRole": self._specialization_role,
            "activeRoleContracts": role_contracts,
            "rolePlaybook": role_contract.investigation_playbook,
            "toolUseGuidance": role_contract.tool_use_guidance,
            "structuralClosureCriteria": role_contract.structural_closure_criteria,
            "sharedMemory": _planner_memory_snapshot(ctx.session.state),
            "corpusIndex": read_corpus_index(tool_context),
            "toolContracts": ctx.session.state.get(STATE_TOOL_CONTRACTS, []) or [],
        }
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_INVESTIGATOR_CONTEXT: investigator_context}),
        )

        yield _runtime_log_event(
            ctx.session.state,
            actor=self._planner_name,
            phase="llm_request",
            message="Investigator planner is choosing a feasible evidence-gathering plan.",
            model=INVESTIGATOR_MODEL,
            basis={
                "mission": mission,
                "toolAvailability": investigator_context["sharedMemory"]["toolAvailability"],
                "corpusDocumentCount": investigator_context["corpusIndex"]["documentCount"],
                "activeRoleContracts": role_contracts,
            },
            request=investigator_context,
        )
        async for event in self._run_named_subagent(ctx, self._planner_name):
            yield event
        plan = InvestigatorPlan.model_validate(ctx.session.state.get(STATE_INVESTIGATOR_PLAN) or {})
        yield _runtime_log_event(
            ctx.session.state,
            actor=self._planner_name,
            phase="llm_result",
            message="Investigator planner returned a concrete plan constrained by currently available capabilities.",
            model=INVESTIGATOR_MODEL,
            basis={"mission": mission},
            result=plan,
        )
        if plan.blocking_failure is not None:
            result = AgentResult(
                agentName=self.name,  # type: ignore[arg-type]
                missionId=mission.mission_id,
                status="failed",
                summary=plan.blocking_failure.message,
                activeRoles=mission.assigned_roles or selected_roles,
                graphDelta=[],
                answeredQuestions=[],
                gaps=[plan.blocking_failure.message],
                nextQuestions=[],
                handoffRecommendations=["stop"],
                failure=plan.blocking_failure,
            )
            ctx.session.state[self._result_state_key] = result.model_dump(mode="json", by_alias=True)
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="agent_result",
                message="Investigator mission was blocked before branch execution.",
                model=INVESTIGATOR_MODEL,
                basis={"mission": mission},
                result=result,
            )
            return

        existing_branches = {
            branch.branch_id: branch
            for branch in [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])]
        }
        runtime_queue: asyncio.Queue[Event] = asyncio.Queue()
        pending_plans: deque[BranchPlan] = deque(plan.branch_plans[:4])
        seen_branch_ids = set(existing_branches) | {item.branch_id for item in pending_plans}
        running: dict[asyncio.Task[tuple[BranchState, list[ToolActionResult], list[BranchPlan]]], str] = {}
        branch_results: list[tuple[BranchState, list[ToolActionResult]]] = []
        completed_fragments: list[GraphFragment] = []
        patch_events: list[dict[str, Any]] = []
        max_parallel = 4

        while pending_plans or running:
            while pending_plans and len(running) < max_parallel:
                branch_plan = pending_plans.popleft()
                task = asyncio.create_task(
                    self._execute_branch_plan(
                        ctx,
                        tool_context,
                        branch_plan,
                        existing_branches.get(branch_plan.branch_id),
                        runtime_queue,
                    )
                )
                running[task] = branch_plan.branch_id

            if not running:
                break

            done, _ = await asyncio.wait(set(running), timeout=0.2, return_when=asyncio.FIRST_COMPLETED)
            while not runtime_queue.empty():
                yield await runtime_queue.get()

            for task in done:
                running.pop(task, None)
                branch_state, tool_results, spawned_plans = task.result()
                branch_results.append((branch_state, tool_results))
                existing_branches[branch_state.branch_id] = branch_state

                ctx.session.state[STATE_BRANCH_STATES] = [
                    item.model_dump(mode="json", by_alias=True)
                    for item in _merge_branch_states(
                        [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])],
                        [branch_state],
                    )
                ]
                if branch_state.hypothesis_turns:
                    existing_turns = [HypothesisTurn.model_validate(item) for item in (ctx.session.state.get(STATE_HYPOTHESIS_TURNS) or [])]
                    turn_map = {item.turn_id: item for item in existing_turns}
                    for turn in branch_state.hypothesis_turns:
                        turn_map[turn.turn_id] = turn
                    ctx.session.state[STATE_HYPOTHESIS_TURNS] = [
                        item.model_dump(mode="json", by_alias=True) for item in turn_map.values()
                    ]

                branch_fragments = branch_state.graph_fragments
                if branch_fragments:
                    completed_fragments.extend(branch_fragments)
                    patch_result = _apply_graph_fragment_mutations(ctx.session.state, branch_fragments)
                    patch_events.append(
                        {
                            "branchId": branch_state.branch_id,
                            "graphMutation": patch_result,
                        }
                    )
                ctx.session.state[STATE_HYPOTHESES] = [
                    item.model_dump(mode="json", by_alias=True) for item in _derive_hypotheses(ctx.session.state)
                ]

                for spawned in spawned_plans[:3]:
                    if spawned.branch_id in seen_branch_ids:
                        continue
                    seen_branch_ids.add(spawned.branch_id)
                    pending_plans.append(spawned)

        while not runtime_queue.empty():
            yield await runtime_queue.get()

        updated_branches = [state for state, _ in branch_results]
        fragments = completed_fragments
        graph_delta = _flatten_fragments(fragments)

        async for event in self._run_cross_branch_synthesis(
            ctx, mission, updated_branches, fragments, mission.assigned_roles or [self._specialization_role],
        ):
            yield event
        synthesis_raw = ctx.session.state.get(STATE_INVESTIGATOR_SYNTHESIS)
        synthesis = InvestigatorSynthesis.model_validate(synthesis_raw) if synthesis_raw else None
        if synthesis and synthesis.addressed_gap_texts:
            resolve_gaps(
                ctx.session.state,
                synthesis.addressed_gap_texts,
                addressed_by=self._synthesizer_name,
            )
        _auto_resolve_detail_gaps(ctx.session.state, addressed_by="auto_detail_triage")

        per_branch_gaps = _dedupe_preserve_order(
            _open_structural_gap_texts(ctx.session.state)
            + [question for branch in updated_branches for question in branch.open_questions
               if not _DETAIL_SIGNALS.search(question)]
            + [
                branch.stop_reason
                for branch in updated_branches
                if branch.stop_reason and branch.status in {"needs_followup", "blocked"}
            ]
        )
        bridge_gaps = list(synthesis.bridge_gaps) if synthesis else []
        gaps = _dedupe_preserve_order(bridge_gaps + per_branch_gaps)

        answered_questions = [
            branch.human_question
            for branch in updated_branches
            if branch.status == "completed" and (branch.graph_fragments or branch.path_assessment == "rejected")
        ]
        per_branch_questions = _dedupe_preserve_order(
            [question for branch in updated_branches for question in branch.open_questions]
        )
        per_branch_discovered = _dedupe_preserve_order(
            [
                question
                for branch in updated_branches
                for tool_result in branch.tool_results[-2:]
                for question in tool_result.discovered_frontier
                if question
            ]
        )
        bridge_questions = list(synthesis.bridge_questions) if synthesis else []
        next_questions = _dedupe_preserve_order(bridge_questions + per_branch_discovered + per_branch_questions)[:8]

        synthesis_note = ""
        if synthesis and synthesis.bridge_gaps:
            synthesis_note = f" Cross-branch synthesis identified {len(synthesis.bridge_gaps)} bridge gap(s)."

        result = AgentResult(
            agentName=self.name,  # type: ignore[arg-type]
            missionId=mission.mission_id,
            status="success" if graph_delta else "partial",
            summary=(
                f"Executed {len(updated_branches)} investigation branch(es) and produced "
                f"{len(fragments)} graph fragment(s) with {len(graph_delta)} typed observations."
                + synthesis_note
            ),
            activeRoles=mission.assigned_roles or selected_roles,
            graphDelta=graph_delta[:18],
            answeredQuestions=answered_questions[:6],
            gaps=gaps[:8],
            nextQuestions=next_questions,
            handoffRecommendations=["critic_agent"],
            failure=None,
        )
        ctx.session.state[self._result_state_key] = result.model_dump(mode="json", by_alias=True)
        if result.gaps:
            record_open_gaps(result.gaps, tool_context)
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="agent_result",
            message="Investigator completed branch execution and returned a graph-native result.",
            model=INVESTIGATOR_MODEL,
            basis={"mission": mission, "branchCount": len(updated_branches)},
            result={
                "status": result.status,
                "summary": result.summary,
                "branchStatuses": {branch.branch_id: branch.status for branch in updated_branches},
                "graphFragmentCount": len(fragments),
                "graphDeltaCount": len(graph_delta),
                "graphMutationEvents": patch_events[-6:],
                "gapCount": len(result.gaps),
                "synthesisGaps": len(bridge_gaps),
            },
        )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"{self.name} cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event

    async def _run_cross_branch_synthesis(
        self,
        ctx: InvocationContext,
        mission: InvestigationMission,
        branches: list[BranchState],
        fragments: list[GraphFragment],
        selected_roles: list[str],
    ) -> AsyncGenerator[Event, None]:
        if len(branches) < 2 or not fragments:
            return
        fragment_boundaries: list[dict[str, Any]] = []
        for fragment in fragments[:8]:
            boundary_observations = fragment.observations[:2] + fragment.observations[-2:]
            boundary_edges = [
                f"{obs.subject} --{obs.relation}--> {obs.object}"
                for obs in boundary_observations
            ]
            fragment_boundaries.append({
                "fragmentId": fragment.fragment_id,
                "branchId": fragment.branch_id,
                "title": fragment.title,
                "humanQuestion": fragment.human_question,
                "startNodeId": fragment.start_node_id,
                "endNodeId": fragment.end_node_id,
                "completeness": fragment.completeness,
                "observationCount": len(fragment.observations),
                "boundaryEdges": _dedupe_preserve_order(boundary_edges)[:6],
            })
        branch_summaries: list[dict[str, Any]] = []
        for branch in branches[:6]:
            branch_summaries.append({
                "branchId": branch.branch_id,
                "hypothesis": branch.hypothesis,
                "humanQuestion": branch.human_question,
                "humanSummary": branch.human_summary,
                "status": branch.status,
                "closureTarget": branch.closure_target,
                "knownContext": branch.known_context[:4],
                "remainingFrontier": branch.remaining_frontier[:4],
                "resolvedFrontier": branch.resolved_frontier[:4],
                "openQuestions": branch.open_questions[:4],
            })
        reachability = _graph_reachability_report(ctx.session.state)
        role_contract = _role_contract(self._specialization_role)
        synthesis_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "selectedRoles": selected_roles,
            "specializedRole": self._specialization_role,
            "rolePlaybook": role_contract.investigation_playbook,
            "toolUseGuidance": role_contract.tool_use_guidance,
            "structuralClosureCriteria": role_contract.structural_closure_criteria,
            "branchSummaries": branch_summaries,
            "fragmentBoundaries": fragment_boundaries,
            "openGaps": _open_gap_texts(ctx.session.state)[:8],
            "reasoningDedupHints": _reasoning_dedup_hints(ctx.session.state),
            "reachability": reachability,
        }
        ctx.session.state[STATE_INVESTIGATOR_SYNTHESIS_CONTEXT] = synthesis_context
        yield _runtime_log_event(
            ctx.session.state,
            actor=self._synthesizer_name,
            phase="llm_request",
            message="Cross-branch synthesizer is comparing all branch results to identify bridge gaps.",
            model=INVESTIGATOR_MODEL,
            basis={
                "branchCount": len(branches),
                "fragmentCount": len(fragments),
            },
            request=synthesis_context,
        )
        try:
            agent = self.find_sub_agent(self._synthesizer_name)
            if agent is None:
                return
            async for event in agent.run_async(ctx):
                yield event
            raw = ctx.session.state.get(STATE_INVESTIGATOR_SYNTHESIS)
            if not raw:
                return
            synthesis = InvestigatorSynthesis.model_validate(raw)
        except Exception:
            return
        yield _runtime_log_event(
            ctx.session.state,
            actor=self._synthesizer_name,
            phase="llm_result",
            message=synthesis.synthesis_summary or "Cross-branch synthesis completed.",
            model=INVESTIGATOR_MODEL,
            basis={
                "bridgeGapCount": len(synthesis.bridge_gaps),
                "bridgeQuestionCount": len(synthesis.bridge_questions),
                "disconnectedPairCount": len(synthesis.disconnected_pairs),
            },
            result=synthesis.model_dump(mode="json", by_alias=True),
        )

    async def _emit_runtime_log(
        self,
        ctx: InvocationContext,
        queue: asyncio.Queue[Event],
        *,
        actor: str,
        phase: str,
        message: str,
        model: str = "",
        basis: dict[str, Any] | None = None,
        request: dict[str, Any] | None = None,
        result: dict[str, Any] | None = None,
    ) -> None:
        event = _runtime_log_event(
            ctx.session.state,
            actor=actor,
            phase=phase,
            message=message,
            model=model,
            basis=basis,
            request=request,
            result=result,
        )
        ctx.session.state[STATE_RUNTIME_LOG] = event.actions.state_delta[STATE_RUNTIME_LOG]
        await queue.put(event)

    async def _run_hypothesis_turn_reasoner(
        self,
        ctx: InvocationContext,
        runtime_queue: asyncio.Queue[Event],
        branch_plan: BranchPlan,
        branch_state: BranchState,
        local_state: dict[str, Any],
        latest_result: ToolActionResult,
    ) -> HypothesisTurnDecision:
        context = _hypothesis_turn_context(
            ctx.session.state,
            branch_plan,
            branch_state,
            local_state,
            latest_result,
        )
        async with self._turn_reasoner_lock:
            ctx.session.state[STATE_HYPOTHESIS_TURN_CONTEXT] = context
            ctx.session.state.pop(STATE_HYPOTHESIS_TURN_DECISION, None)
            await self._emit_runtime_log(
                ctx,
                runtime_queue,
                actor=self._turn_reasoner_name,
                phase="llm_request",
                message="Hypothesis turn reasoner is choosing the next graph operation for the active hypothesis.",
                model=INVESTIGATOR_MODEL,
                basis={
                    "branchId": branch_plan.branch_id,
                    "pathKind": branch_plan.path_kind,
                    "turnCount": len(branch_state.hypothesis_turns),
                },
                request=context,
            )
            async for event in self._run_named_subagent(ctx, self._turn_reasoner_name):
                _apply_event_state_delta_to_session(ctx, event)
                await runtime_queue.put(event)
            decision = HypothesisTurnDecision.model_validate(ctx.session.state.get(STATE_HYPOTHESIS_TURN_DECISION) or {})
            await self._emit_runtime_log(
                ctx,
                runtime_queue,
                actor=self._turn_reasoner_name,
                phase="llm_result",
                message=decision.decision_note,
                model=INVESTIGATOR_MODEL,
                basis={
                    "branchId": branch_plan.branch_id,
                    "decision": decision.decision,
                    "followupBranchCount": len(decision.followup_branches),
                },
                result=decision,
            )
        return decision

    async def _execute_branch_plan(
        self,
        ctx: InvocationContext,
        tool_context: _StateToolContext,
        branch_plan: BranchPlan,
        previous_state: BranchState | None,
        runtime_queue: asyncio.Queue[Event],
    ) -> tuple[BranchState, list[ToolActionResult], list[BranchPlan]]:
        state = previous_state
        local_state = _hypothesis_local_state(ctx.session.state, branch_plan, previous_state)
        tool_results: list[ToolActionResult] = []
        spawned_plans: list[BranchPlan] = []
        turn_plan = _initial_turn_plan(branch_plan)
        turn_index = len(previous_state.hypothesis_turns) if previous_state is not None else 0
        max_turns = 6

        while turn_plan is not None and turn_index < max_turns:
            turn_index += 1
            known_context = _dedupe_preserve_order(
                list(turn_plan.known_context)
                + list(state.known_context if state is not None else [])
            )[:10]
            frontier = (
                list(state.remaining_frontier)
                if state is not None and state.remaining_frontier
                else list(turn_plan.frontier or branch_plan.frontier)
            )
            contextual_query = _contextual_query_text(
                local_state,
                human_question=turn_plan.human_question,
                human_action=turn_plan.human_action,
                path_kind=turn_plan.path_kind,
                closure_target=turn_plan.closure_target,
                target_entities=turn_plan.target_entities,
                seed_known_context=known_context,
                frontier=frontier,
            )
            action = ToolAction(
                actionId=f"{branch_plan.branch_id}-a{turn_index}",
                branchId=branch_plan.branch_id,
                pathKind=turn_plan.path_kind,
                toolName=turn_plan.tool_name,
                humanQuestion=turn_plan.human_question,
                humanAction=turn_plan.human_action,
                whyThisQuestion=turn_plan.why_now,
                closureTarget=turn_plan.closure_target,
                knownContext=known_context,
                frontier=_dedupe_preserve_order(frontier)[:6],
                queryText=contextual_query,
                assignedRoles=branch_plan.assigned_roles,
                targetEntities=turn_plan.target_entities,
                expectedFragmentType=turn_plan.fragment_type,
            )
            await self._emit_runtime_log(
                ctx,
                runtime_queue,
                actor=self.name,
                phase="tool_call",
                message=turn_plan.human_action,
                model=_tool_model_name(turn_plan.tool_name),
                basis={
                    "branchId": branch_plan.branch_id,
                    "hypothesis": branch_plan.hypothesis,
                    "turnIndex": turn_index,
                },
                request=action,
            )
            tool_result = await asyncio.to_thread(self._run_tool_action, action, tool_context)
            tool_results.append(tool_result)
            await self._emit_runtime_log(
                ctx,
                runtime_queue,
                actor=self.name,
                phase="tool_result",
                message=tool_result.human_summary,
                model=_tool_model_name(turn_plan.tool_name),
                basis={"branchId": branch_plan.branch_id, "turnIndex": turn_index},
                result=tool_result,
            )
            if tool_result.learned_findings:
                _record_learned_findings(ctx.session.state, tool_result.learned_findings)
                _record_learned_findings(local_state, tool_result.learned_findings)
            if tool_result.graph_fragments:
                _apply_graph_fragment_mutations(local_state, tool_result.graph_fragments)

            provisional_state = _update_branch_state(state, branch_plan, action, tool_result)
            decision = await self._run_hypothesis_turn_reasoner(
                ctx,
                runtime_queue,
                branch_plan,
                provisional_state,
                local_state,
                tool_result,
            )
            turn = _make_hypothesis_turn(
                plan=branch_plan,
                turn_index=turn_index,
                action=action,
                result=tool_result,
                decision_after_result=decision.decision_note,
            )

            if decision.decision == "ask_next_question_same_hypothesis":
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    turn=turn,
                    explicit_status="needs_followup",
                    explicit_stop_reason=decision.decision_note,
                )
                turn_plan = decision.next_turn
                continue

            if decision.decision in {"run_parallel_followups", "spawn_independent_child_hypothesis"}:
                spawned_plans.extend(
                    [
                        _normalize_followup_branch_plan(
                            branch_plan,
                            provisional_state,
                            tool_result,
                            local_state,
                            item,
                        )
                        for item in decision.followup_branches[:3]
                    ]
                )
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    turn=turn,
                    explicit_status="needs_followup",
                    explicit_stop_reason=decision.decision_note,
                )
                break

            if decision.decision == "accept_hypothesis":
                acceptance_block_reason = _acceptance_block_reason(branch_plan, provisional_state, local_state)
                if acceptance_block_reason:
                    state = _update_branch_state(
                        state,
                        branch_plan,
                        action,
                        tool_result,
                        turn=turn,
                        explicit_status="needs_followup",
                        explicit_stop_reason=(
                            f"Acceptance blocked because the hypothesis is not structurally closed yet. "
                            f"{acceptance_block_reason}"
                        ),
                    )
                    break
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    turn=turn,
                    explicit_status="completed",
                    explicit_stop_reason=decision.decision_note,
                )
                break

            if decision.decision == "reject_hypothesis":
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    turn=turn,
                    explicit_status="completed",
                    explicit_stop_reason=decision.decision_note,
                ).model_copy(
                    update={
                        "path_assessment": "rejected",
                        "path_assessment_note": decision.decision_note,
                    }
                )
                break

            state = _update_branch_state(
                state,
                branch_plan,
                action,
                tool_result,
                turn=turn,
                explicit_status="needs_followup",
                explicit_stop_reason=decision.decision_note or "Branch exhausted its current useful improvement window without closing the frontier.",
            )
            break
        if state is None:
            state = BranchState(
                branchId=branch_plan.branch_id,
                hypothesis=branch_plan.hypothesis,
                pathKind=branch_plan.path_kind,
                pathAssessment="still_open",
                pathAssessmentNote="Branch did not produce enough evidence to classify this candidate path.",
                humanQuestion=branch_plan.human_question,
                humanSummary="Branch produced no result.",
                closureTarget=branch_plan.closure_target,
                knownContext=list(branch_plan.known_context),
                assignedRoles=branch_plan.assigned_roles,
                status="blocked",
                qualityScore=0.0,
                stagnationCount=0,
                stopReason="Branch produced no result.",
                resolvedFrontier=[],
                remainingFrontier=list(branch_plan.frontier) or [branch_plan.closure_target],
                openQuestions=[branch_plan.human_question],
                toolActions=[],
                toolResults=[],
                graphFragments=[],
                hypothesisTurns=[],
            )
        elif state.status == "active":
            state = state.model_copy(
                update={
                    "status": (
                        "completed"
                        if state.graph_fragments and not state.open_questions and not state.remaining_frontier
                        else "needs_followup"
                        if state.graph_fragments
                        else "blocked"
                    ),
                    "stop_reason": (
                        ""
                        if state.graph_fragments and not state.open_questions and not state.remaining_frontier
                        else "Branch exhausted available tools while the graph frontier remained open."
                        if state.graph_fragments
                        else "Branch exhausted available tools without producing a usable graph fragment."
                    ),
                }
            )
        return state, tool_results, spawned_plans

    def _run_tool_action(self, action: ToolAction, tool_context: _StateToolContext) -> ToolActionResult:
        if action.tool_name == "retrieve":
            return _ensure_learned_findings(action, retrieve(action, tool_context))
        if action.tool_name == "reason":
            return _ensure_learned_findings(action, reason(action, tool_context))
        if action.tool_name == "web_search":
            return _ensure_learned_findings(action, web_search(action, tool_context))
        return _ensure_learned_findings(
            action,
            _tool_failure_result(action, "failed", f"Unsupported tool {action.tool_name}", code="agent_failed"),
        )


class GapBridgeAgent(BaseAgent):
    def __init__(self) -> None:
        super().__init__(name="gap_bridge_agent", sub_agents=[gap_bridge_reasoner])

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        tool_context = _StateToolContext(ctx.session.state)
        mission = InvestigationMission.model_validate(ctx.session.state.get(STATE_CURRENT_MISSION) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="delegation",
            message="Gap/bridge agent received a mission to connect partial graph structure.",
            model=INVESTIGATOR_MODEL,
            basis={
                "mission": mission,
                "selectedRoles": selected_roles,
                "openGapCount": _open_gap_count(ctx.session.state),
                "graphObservationCount": len(ctx.session.state.get(STATE_WORKING_GRAPH) or []),
            },
        )
        gap_bridge_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "selectedRoles": selected_roles,
            "activeRoleContracts": _selected_role_contract_payload(mission.assigned_roles or selected_roles),
            "sharedMemory": read_shared_memory(tool_context),
            "corpusIndex": read_corpus_index(tool_context),
        }
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_GAP_BRIDGE_CONTEXT: gap_bridge_context}),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor="gap_bridge_reasoner",
            phase="llm_request",
            message="Gap/bridge reasoner is evaluating whether the current graph can be connected honestly.",
            model=INVESTIGATOR_MODEL,
            basis={"mission": mission},
            request=gap_bridge_context,
        )
        async for event in self._run_named_subagent(ctx, "gap_bridge_reasoner"):
            yield event
        raw_result = AgentResult.model_validate(ctx.session.state.get(STATE_GAP_BRIDGE_RESULT) or {})
        result = _normalize_agent_result(raw_result, mission, mission.assigned_roles or selected_roles)
        ctx.session.state[STATE_GAP_BRIDGE_RESULT] = result.model_dump(mode="json", by_alias=True)
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="agent_result",
            message="Gap/bridge agent returned a bridging result.",
            model=INVESTIGATOR_MODEL,
            basis={"mission": mission},
            result={
                "status": result.status,
                "summary": result.summary,
                "graphDeltaCount": len(result.graph_delta),
                "gapCount": len(result.gaps),
                "nextQuestionCount": len(result.next_questions),
                "failure": result.failure.model_dump(mode="json", by_alias=True) if result.failure else {},
            },
        )

        if result.graph_delta:
            append_graph_observations(
                [item.model_dump(mode="json", by_alias=True) for item in result.graph_delta],
                tool_context,
            )
        if result.gaps:
            record_open_gaps(result.gaps, tool_context)
        if result.failure is not None:
            record_failure(
                result.failure.code,
                result.failure.message,
                result.failure.blocking,
                tool_context,
                missing_inputs=result.failure.missing_inputs,
            )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"GapBridgeAgent cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event


class CriticAgent(BaseAgent):
    def __init__(self) -> None:
        super().__init__(name="critic_agent", sub_agents=[critic_reasoner])

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        tool_context = _StateToolContext(ctx.session.state)
        mission = InvestigationMission.model_validate(ctx.session.state.get(STATE_CURRENT_MISSION) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="delegation",
            message="Critic received a mission to assess sufficiency and honesty of the current graph.",
            model=CRITIC_MODEL,
            basis={
                "mission": mission,
                "selectedRoles": selected_roles,
                "openGapCount": _open_gap_count(ctx.session.state),
                "graphObservationCount": len(ctx.session.state.get(STATE_WORKING_GRAPH) or []),
            },
        )
        critic_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "selectedRoles": selected_roles,
            "activeRoleContracts": _selected_role_contract_payload(mission.assigned_roles or selected_roles),
            "sharedMemory": read_shared_memory(tool_context),
            "corpusIndex": read_corpus_index(tool_context),
            "reachability": _graph_reachability_report(ctx.session.state),
        }
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_CRITIC_CONTEXT: critic_context}),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor="critic_reasoner",
            phase="llm_request",
            message="Critic is evaluating whether the current graph is sufficient for an honest answer.",
            model=CRITIC_MODEL,
            basis={"mission": mission},
            request=critic_context,
        )
        async for event in self._run_named_subagent(ctx, "critic_reasoner"):
            yield event
        raw_result = AgentResult.model_validate(ctx.session.state.get(STATE_CRITIC_RESULT) or {})
        result = _normalize_agent_result(raw_result, mission, mission.assigned_roles or selected_roles)
        ctx.session.state[STATE_CRITIC_RESULT] = result.model_dump(mode="json", by_alias=True)
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="agent_result",
            message="Critic returned its sufficiency assessment.",
            model=CRITIC_MODEL,
            basis={"mission": mission},
            result={
                "status": result.status,
                "summary": result.summary,
                "gapCount": len(result.gaps),
                "handoffRecommendations": result.handoff_recommendations,
                "failure": result.failure.model_dump(mode="json", by_alias=True) if result.failure else {},
            },
        )

        if result.gaps:
            record_open_gaps(result.gaps, tool_context)
        if result.failure is not None:
            record_failure(
                result.failure.code,
                result.failure.message,
                result.failure.blocking,
                tool_context,
                missing_inputs=result.failure.missing_inputs,
            )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"CriticAgent cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event


class SupervisorAgent(BaseAgent):
    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        if not ctx.session.state.get(STATE_REQUEST):
            request = AgentLoomRequest.from_json_text(_extract_user_text(ctx))
            yield Event(
                author=self.name,
                actions=EventActions(
                    state_delta={
                        STATE_REQUEST: request.model_dump(mode="json", by_alias=True),
                        STATE_ROLE_CONTRACTS: _all_role_contract_payload(),
                        STATE_TOOL_CONTRACTS: [item.model_dump(mode="json", by_alias=True) for item in TOOL_CONTRACTS],
                        STATE_AGENT_CONTRACTS: [item.model_dump(mode="json", by_alias=True) for item in AGENT_CONTRACTS],
                        STATE_WORKING_GRAPH: [],
                        STATE_GRAPH_FRAGMENTS: [],
                        STATE_BRANCH_STATES: [],
                        STATE_HYPOTHESES: [],
                        STATE_HYPOTHESIS_TURNS: [],
                        STATE_LEARNED_FINDINGS: [],
                        STATE_SUPPLY_CHAIN_INVESTIGATOR_RESULT: {},
                        STATE_GOVERNANCE_INVESTIGATOR_RESULT: {},
                        STATE_POLICY_INVESTIGATOR_RESULT: {},
                        STATE_GAP_BRIDGE_RESULT: {},
                        STATE_CRITIC_RESULT: {},
                        STATE_OPEN_GAPS: [],
                        STATE_ANSWERED_QUESTIONS: [],
                        STATE_FAILURES: [],
                        STATE_AGENT_RESULT_HISTORY: [],
                        STATE_PROGRESS_HISTORY: [],
                        STATE_GLOBAL_STAGNATION: 0,
                        STATE_LOGIC_TRACE: [],
                        STATE_RUNTIME_LOG: [],
                        STATE_SUPERVISOR_CYCLE: 0,
                    }
                ),
            )
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="session_init",
                message="AgentLoom initialized shared session memory for a new investigation.",
                basis={
                    "question": request.question,
                    "requestedRoles": request.requested_roles,
                    "corpusDocumentCount": len(request.corpus_documents),
                    "models": {
                        "supervisor": SUPERVISOR_MODEL,
                        "investigator": INVESTIGATOR_MODEL,
                        "critic": CRITIC_MODEL,
                        "grounding": GROUNDING_MODEL,
                    },
                    "toolAvailability": {
                        "corpus": bool(request.corpus_documents),
                        "vertexGroundedSearch": _vertex_grounding_config(),
                        "webSearch": _web_search_available(),
                    },
                },
            )

        if not ctx.session.state.get(STATE_SELECTED_ROLES):
            async for event in self._select_roles(ctx):
                yield event

        for _ in range(MAX_SUPERVISOR_CYCLES):
            cycle = int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0) + 1
            yield Event(author=self.name, actions=EventActions(state_delta={STATE_SUPERVISOR_CYCLE: cycle}))
            state_seen = self._state_snapshot(ctx)
            supervisor_context = {
                "state": state_seen,
            }
            if cycle <= 1:
                supervisor_context["selectedRoleContracts"] = _selected_role_contract_payload(ctx.session.state.get(STATE_SELECTED_ROLES, []) or [])
                supervisor_context["agentContracts"] = ctx.session.state.get(STATE_AGENT_CONTRACTS, []) or []
                supervisor_context["toolContracts"] = ctx.session.state.get(STATE_TOOL_CONTRACTS, []) or []
            yield Event(
                author=self.name,
                actions=EventActions(state_delta={STATE_SUPERVISOR_CONTEXT: supervisor_context}),
            )

            yield _runtime_log_event(
                ctx.session.state,
                actor="supervisor_reasoner",
                phase="llm_request",
                message="Supervisor is choosing the next mission from current shared memory.",
                model=SUPERVISOR_MODEL,
                basis={
                    "cycle": cycle,
                    "graphObservationCount": state_seen["graphObservationCount"],
                    "openGapCount": len(state_seen["openGaps"]),
                    "failureCount": state_seen["failureCount"],
                    "lastAgentResult": state_seen["lastAgentResult"],
                },
                request=supervisor_context,
            )
            async for event in self._run_named_subagent(ctx, "supervisor_reasoner"):
                yield event
            request_obj = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
            raw_supervisor = dict(ctx.session.state.get(STATE_SUPERVISOR_DECISION) or {})
            coerced = _coerce_supervisor_decision_payload(
                raw_supervisor,
                selected_role_literals=list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
                cycle=cycle,
                user_question=request_obj.question,
            )
            if coerced != raw_supervisor:
                ctx.session.state[STATE_SUPERVISOR_DECISION] = coerced
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="decision",
                    message="Supervisor JSON was coerced to satisfy mission contract (missing mission or assignedRoles).",
                    basis={"cycle": cycle, "coercedFields": True},
                )
            decision = SupervisorDecision.model_validate(ctx.session.state.get(STATE_SUPERVISOR_DECISION) or {})
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="decision",
                message="Supervisor produced the next control decision.",
                model=SUPERVISOR_MODEL,
                basis={"cycle": cycle, "stateSeen": state_seen},
                result=decision,
            )

            agent_result: AgentResult | None = None
            mission_failure = self._validate_mission(ctx, decision)
            if mission_failure is not None:
                agent_result = mission_failure
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="failure",
                    message="Supervisor produced an invalid delegation; the mission contract was rejected locally.",
                    basis={"cycle": cycle, "decision": decision},
                    result=mission_failure,
                )
                self._merge_agent_result(ctx, agent_result)
            elif decision.next_agent == "compose":
                logic_entry = LogicTraceEntry(
                    cycle=cycle,
                    stateSeen=state_seen,
                    decision=decision,
                    mission=decision.mission,
                    agentResult=None,
                    resultingState=self._state_snapshot(ctx),
                    nextStepLogic=self._next_step_logic(decision, None),
                )
                yield Event(
                    author=self.name,
                    actions=EventActions(state_delta={STATE_LOGIC_TRACE: self._append_logic_entry(ctx, logic_entry)}),
                )
                async for event in self._run_named_subagent(ctx, "compose"):
                    yield event
                return
            elif decision.next_agent == "stop":
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="decision",
                    message="Supervisor stopped the investigation.",
                    basis={"cycle": cycle, "completionRationale": decision.completion_rationale},
                    result=decision,
                )
                logic_entry = LogicTraceEntry(
                    cycle=cycle,
                    stateSeen=state_seen,
                    decision=decision,
                    mission=decision.mission,
                    agentResult=None,
                    resultingState=self._state_snapshot(ctx),
                    nextStepLogic=self._next_step_logic(decision, None),
                )
                yield Event(
                    author=self.name,
                    actions=EventActions(state_delta={STATE_LOGIC_TRACE: self._append_logic_entry(ctx, logic_entry)}),
                )
                return
            else:
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="delegation",
                    message="Supervisor delegated a concrete mission to a peer agent.",
                    basis={"cycle": cycle, "decisionRationale": decision.rationale},
                    request={
                        "nextAgent": decision.next_agent,
                        "mission": decision.mission.model_dump(mode="json", by_alias=True) if decision.mission else {},
                    },
                )
                yield Event(
                    author=self.name,
                    actions=EventActions(
                        state_delta={STATE_CURRENT_MISSION: decision.mission.model_dump(mode="json", by_alias=True)}
                    ),
                )
                async for event in self._run_named_subagent(ctx, decision.next_agent):
                    yield event
                agent_result = self._read_agent_result(decision.next_agent, ctx)
                self._merge_agent_result(ctx, agent_result)
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="agent_result",
                    message="Supervisor received a result back from the delegated peer agent.",
                    basis={"cycle": cycle, "delegatedAgent": decision.next_agent},
                    result=agent_result,
                )

            logic_entry = LogicTraceEntry(
                cycle=cycle,
                stateSeen=state_seen,
                decision=decision,
                mission=decision.mission,
                agentResult=agent_result,
                resultingState=self._state_snapshot(ctx),
                nextStepLogic=self._next_step_logic(decision, agent_result),
            )
            yield Event(
                author=self.name,
                actions=EventActions(state_delta={STATE_LOGIC_TRACE: self._append_logic_entry(ctx, logic_entry)}),
            )

        cycle_limit_failure = FailureRecord(
            code="blocked_by_capability",
            message="Supervisor reached the configured cycle limit before it could either compose an honest answer or stop on capability limits.",
            blocking=True,
            missingInputs=["more_cycles_or_better_agent_decisions"],
        )
        failures = [FailureRecord.model_validate(item) for item in (ctx.session.state.get(STATE_FAILURES) or [])]
        failure_key = (
            cycle_limit_failure.code,
            cycle_limit_failure.message,
            cycle_limit_failure.blocking,
            tuple(cycle_limit_failure.missing_inputs),
        )
        if failure_key not in {
            (item.code, item.message, item.blocking, tuple(item.missing_inputs))
            for item in failures
        }:
            failures.append(cycle_limit_failure)
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_FAILURES: [item.model_dump(mode="json", by_alias=True) for item in failures]}),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="failure",
            message="Supervisor hit the configured cycle limit before converging on compose or stop.",
            basis={"cycleLimit": MAX_SUPERVISOR_CYCLES},
            result=cycle_limit_failure,
        )

    async def _select_roles(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        request = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
        if request.requested_roles:
            selection = RoleSelection(
                selectedRoles=_dedupe_preserve_order(request.requested_roles)[:3],
                rationale=["Roles were provided explicitly in the request and become the initial investigation scope."],
            )
            yield Event(
                author=self.name,
                actions=EventActions(
                    state_delta={
                        STATE_ROLE_SELECTION: selection.model_dump(mode="json", by_alias=True),
                        STATE_SELECTED_ROLES: selection.selected_roles,
                    }
                ),
            )
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="role_selection",
                message="Role selection was taken directly from the request.",
                basis={"question": request.question},
                result=selection,
            )
            return

        role_selector_context = {
            "question": request.question,
            "requestedRoles": request.requested_roles,
            "availableRoles": list(ROLE_CONTRACTS.keys()),
            "roleContracts": _all_role_contract_payload(),
        }
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={"role_selector_context": role_selector_context}),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor="role_selector",
            phase="llm_request",
            message="Role selector is choosing the smallest sufficient role set for the question.",
            model=SUPERVISOR_MODEL,
            basis={"question": request.question, "requestedRoles": request.requested_roles},
            request=role_selector_context,
        )
        async for event in self._run_named_subagent(ctx, "role_selector"):
            yield event
        raw_selection = RoleSelection.model_validate(ctx.session.state.get(STATE_ROLE_SELECTION) or {})
        selection = RoleSelection(
            selectedRoles=_dedupe_preserve_order(list(raw_selection.selected_roles))[:3] or [ROLE_SUPPLY_CHAIN],
            rationale=list(raw_selection.rationale)[:6] or ["Role selector returned no rationale."],
        )
        yield Event(
            author=self.name,
            actions=EventActions(
                state_delta={
                    STATE_ROLE_SELECTION: selection.model_dump(mode="json", by_alias=True),
                    STATE_SELECTED_ROLES: selection.selected_roles,
                }
            ),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="role_selection",
            message="Role selector finished and the selected roles were committed into shared memory.",
            model=SUPERVISOR_MODEL,
            basis={"question": request.question},
            result=selection,
        )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"AgentLoom supervisor cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event

    def _read_agent_result(self, agent_name: str, ctx: InvocationContext) -> AgentResult:
        raw = ctx.session.state.get(RESULT_KEY_BY_AGENT[agent_name]) or {}
        return AgentResult.model_validate(raw)

    def _validate_mission(self, ctx: InvocationContext, decision: SupervisorDecision) -> AgentResult | None:
        if decision.next_agent in {"compose", "stop"}:
            return None
        if decision.mission is None:
            return self._invalid_delegation_result(
                decision.next_agent,
                "Supervisor delegated without providing a mission.",
                ["mission"],
                list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
            )
        selected = set(_dedupe_preserve_order(list(decision.mission.assigned_roles)))
        allowed = set(_dedupe_preserve_order(list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])))
        if not selected:
            return self._invalid_delegation_result(
                decision.next_agent,
                "Supervisor delegated without assignedRoles.",
                ["assignedRoles"],
                list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
            )
        if not selected.issubset(allowed):
            return self._invalid_delegation_result(
                decision.next_agent,
                "Mission assignedRoles are not a subset of selectedRoles.",
                ["assignedRoles", "selectedRoles"],
                list(decision.mission.assigned_roles or ctx.session.state.get(STATE_SELECTED_ROLES) or []),
            )
        specialized_role = _specialized_role_from_agent_name(decision.next_agent)
        if specialized_role is not None:
            if selected != {specialized_role}:
                return self._invalid_delegation_result(
                    decision.next_agent,
                    f"{decision.next_agent} requires assignedRoles to be exactly ['{specialized_role}'].",
                    ["assignedRoles"],
                    list(decision.mission.assigned_roles or []),
                )
        return None

    def _invalid_delegation_result(
        self,
        agent_name: str,
        message: str,
        missing_inputs: list[str],
        active_roles: list[InvestigationRole],
    ) -> AgentResult:
        return AgentResult(
            agentName=agent_name,  # type: ignore[arg-type]
            missionId="invalid-mission",
            status="failed",
            summary=message,
            activeRoles=_dedupe_preserve_order(active_roles),
            graphDelta=[],
            answeredQuestions=[],
            gaps=[message],
            nextQuestions=["Provide a mission whose assignedRoles are a non-empty subset of selectedRoles."],
            handoffRecommendations=["stop"],
            failure=FailureRecord(
                code="agent_failed",
                message=message,
                blocking=True,
                missingInputs=missing_inputs,
            ),
        )

    def _merge_agent_result(self, ctx: InvocationContext, result: AgentResult) -> None:
        previous_progress = _investigation_progress_snapshot(ctx.session.state)
        current_graph = [GraphObservation.model_validate(item) for item in (ctx.session.state.get(STATE_WORKING_GRAPH) or [])]
        existing_graph = {(item.subject, item.relation, item.object, tuple(item.qualifiers)) for item in current_graph}
        for observation in result.graph_delta:
            key = (observation.subject, observation.relation, observation.object, tuple(observation.qualifiers))
            if key in existing_graph:
                continue
            existing_graph.add(key)
            current_graph.append(observation)

        cycle = int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)
        tool_context = _StateToolContext(ctx.session.state)
        if result.gaps:
            record_open_gaps(result.gaps, tool_context, origin=result.agent_name, cycle=cycle)
        if result.answered_questions:
            resolve_gaps(ctx.session.state, result.answered_questions, addressed_by=result.agent_name)

        answered = _dedupe_preserve_order(list(ctx.session.state.get(STATE_ANSWERED_QUESTIONS) or []) + result.answered_questions)
        failures = [FailureRecord.model_validate(item) for item in (ctx.session.state.get(STATE_FAILURES) or [])]
        if result.failure is not None:
            failure_key = (
                result.failure.code,
                result.failure.message,
                result.failure.blocking,
                tuple(result.failure.missing_inputs),
            )
            existing_failure_keys = {(item.code, item.message, item.blocking, tuple(item.missing_inputs)) for item in failures}
            if failure_key not in existing_failure_keys:
                failures.append(result.failure)
        history = list(ctx.session.state.get(STATE_AGENT_RESULT_HISTORY) or [])
        history.append(result.model_dump(mode="json", by_alias=True))

        ctx.session.state[STATE_WORKING_GRAPH] = [item.model_dump(mode="json", by_alias=True) for item in current_graph]
        ctx.session.state[STATE_ANSWERED_QUESTIONS] = answered
        ctx.session.state[STATE_FAILURES] = [item.model_dump(mode="json", by_alias=True) for item in failures]
        ctx.session.state[STATE_LAST_AGENT_RESULT] = result.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_AGENT_RESULT_HISTORY] = history
        ctx.session.state[STATE_HYPOTHESES] = [
            item.model_dump(mode="json", by_alias=True) for item in _derive_hypotheses(ctx.session.state)
        ]
        current_progress = _investigation_progress_snapshot(ctx.session.state)
        delta = _progress_delta(previous_progress, current_progress)
        material = _material_progress(delta)
        stagnation = 0 if material else int(ctx.session.state.get(STATE_GLOBAL_STAGNATION, 0) or 0) + 1
        progress_history = list(ctx.session.state.get(STATE_PROGRESS_HISTORY) or [])
        progress_history.append(
            {
                "agentName": result.agent_name,
                "missionId": result.mission_id,
                "status": result.status,
                "delta": delta,
                "materialProgress": material,
                "stagnationCount": stagnation,
                "openGapCount": current_progress["openGapCount"],
                "documentObservationCount": current_progress["documentObservationCount"],
                "uniqueSourceCount": current_progress["uniqueSourceCount"],
            }
        )
        ctx.session.state[STATE_PROGRESS_HISTORY] = progress_history[-12:]
        ctx.session.state[STATE_GLOBAL_STAGNATION] = stagnation

    def _append_logic_entry(self, ctx: InvocationContext, entry: LogicTraceEntry) -> list[dict[str, Any]]:
        trace = [LogicTraceEntry.model_validate(item) for item in (ctx.session.state.get(STATE_LOGIC_TRACE) or [])]
        trace.append(entry)
        return [item.model_dump(mode="json", by_alias=True) for item in trace]

    def _state_snapshot(self, ctx: InvocationContext) -> dict[str, Any]:
        request = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
        graph = [GraphObservation.model_validate(item) for item in (ctx.session.state.get(STATE_WORKING_GRAPH) or [])]
        fragments = [GraphFragment.model_validate(item) for item in (ctx.session.state.get(STATE_GRAPH_FRAGMENTS) or [])]
        branches = [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])]
        failures = [FailureRecord.model_validate(item) for item in (ctx.session.state.get(STATE_FAILURES) or [])]
        last_result_raw = ctx.session.state.get(STATE_LAST_AGENT_RESULT) or {}
        last_result = AgentResult.model_validate(last_result_raw) if last_result_raw else None
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        current_progress = _investigation_progress_snapshot(ctx.session.state)
        frontier_summary = _frontier_summary(ctx.session.state)
        candidate_paths = _derive_candidate_paths(ctx.session.state)
        hypotheses = _derive_hypotheses(ctx.session.state)
        cycle = int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)

        last_result_compact: dict[str, Any] = {}
        if last_result:
            last_result_compact = {
                "agentName": last_result.agent_name,
                "missionId": last_result.mission_id,
                "status": last_result.status,
                "summary": last_result.summary,
                "activeRoles": last_result.active_roles,
                "gaps": last_result.gaps[:6],
                "nextQuestions": last_result.next_questions[:4],
                "handoffRecommendations": last_result.handoff_recommendations,
            }

        compact_frontier: list[dict[str, Any]]
        if cycle <= 2:
            compact_frontier = frontier_summary[:6]
        else:
            compact_frontier = []
            for fs in frontier_summary[:4]:
                compact_frontier.append({
                    "branchId": fs.get("branchId", ""),
                    "pathKind": fs.get("pathKind", ""),
                    "pathAssessment": fs.get("pathAssessment", ""),
                    "status": fs.get("status", ""),
                    "remainingFrontier": fs.get("remainingFrontier", [])[:2],
                    "stopReason": fs.get("stopReason", ""),
                })

        snapshot: dict[str, Any] = {
            "question": request.question,
            "selectedRoles": selected_roles,
            "defaultEpistemicMode": "mixed",
            "graphObservationCount": len(graph),
            "graphFragmentCount": len(fragments),
            "branchCount": len(branches),
            "openGaps": _open_gap_texts(ctx.session.state)[:8],
            "failureCount": len(failures),
            "lastAgentResult": last_result_compact,
            "frontierSummary": compact_frontier,
            "candidatePaths": [item.model_dump(mode="json", by_alias=True) for item in candidate_paths[:6]],
            "hypotheses": [item.model_dump(mode="json", by_alias=True) for item in hypotheses[:8]],
            "hypothesisFrontiers": _hypothesis_frontier_signals(ctx.session.state)[:6],
            "progress": {
                "current": current_progress,
                "stagnationCount": int(ctx.session.state.get(STATE_GLOBAL_STAGNATION, 0) or 0),
                "history": list(ctx.session.state.get(STATE_PROGRESS_HISTORY) or [])[-3:],
            },
        }
        if cycle <= 1:
            snapshot["requestedRoles"] = request.requested_roles
            snapshot["toolAvailability"] = {
                "corpus": bool(request.corpus_documents),
                "vertexGroundedSearch": _vertex_grounding_config(),
                "webSearch": _web_search_available(),
            }
            snapshot["corpusDocumentCount"] = len(request.corpus_documents)
            snapshot["answeredQuestions"] = list(ctx.session.state.get(STATE_ANSWERED_QUESTIONS) or [])
        if cycle <= 1:
            snapshot["selectedRoleContracts"] = _selected_role_contract_payload(selected_roles)
            snapshot["graphPreview"] = [item.model_dump(mode="json", by_alias=True) for item in graph[-10:]]
            snapshot["graphFragments"] = [item.model_dump(mode="json", by_alias=True) for item in fragments[-6:]]
            snapshot["branches"] = [item.model_dump(mode="json", by_alias=True) for item in branches[-4:]]
            snapshot["failures"] = [item.model_dump(mode="json", by_alias=True) for item in failures[-5:]]
        else:
            snapshot["graphPreviewByRole"] = _role_coverage_preview(ctx.session.state)
            snapshot["graphFragments"] = [
                {"title": f.title, "type": f.fragment_type, "completeness": f.completeness, "obs": len(f.observations)}
                for f in fragments[-6:]
            ]
            snapshot["branches"] = [
                {"id": b.branch_id, "status": b.status, "remaining": b.remaining_frontier[:2]}
                for b in branches[-4:]
            ]
        if cycle >= 2 and len(graph) >= 10:
            full_reach = _graph_reachability_report(ctx.session.state)
            snapshot["reachability"] = {
                "healthSummary": full_reach["healthSummary"],
                "completeChainCount": len(full_reach["completeChains"]),
                "provisionalChainCount": len(full_reach.get("provisionalChains") or []),
                "completeChainEndpoints": [
                    f"{c['source']} → {c['sink']} ({c['hops']} hops)"
                    for c in full_reach["completeChains"][:3]
                ],
                "provisionalChainEndpoints": [
                    f"{c['source']} → {c['sink']} ({c['hops']} hops)"
                    for c in (full_reach.get("provisionalChains") or [])[:3]
                ],
                "brokenChainBreakPoints": [
                    f"{c['sink']} [{c['sinkType']}]: {c.get('breakReason', '')}"
                    for c in full_reach["brokenChains"][:4]
                ],
                "bridgeCandidates": [
                    f"{b['entity']} [{b['type']}]"
                    for b in full_reach["bridgeCandidates"][:4]
                ],
            }
        return snapshot

    def _next_step_logic(self, decision: SupervisorDecision, agent_result: AgentResult | None) -> str:
        if decision.next_agent == "compose":
            return "Supervisor chose deterministic composition because critic output said the current role-scoped graph is sufficient for an honest answer."
        if decision.next_agent == "stop":
            return "Supervisor stopped because current capabilities could not improve the graph further or because the investigation had reached an honest stopping point."
        if agent_result is None:
            return "No agent result was available after delegation."
        if agent_result.failure is not None:
            return f"Delegated mission failed with {agent_result.failure.code}: {agent_result.failure.message}"
        return (
            f"Delegated {decision.next_agent} for roles={decision.mission.assigned_roles if decision.mission else []} "
            f"and got status={agent_result.status}, graphDelta={len(agent_result.graph_delta)}, gaps={len(agent_result.gaps)}."
        )


def _normalize_agent_result(
    result: AgentResult,
    mission: InvestigationMission,
    expected_roles: list[InvestigationRole],
) -> AgentResult:
    active_roles = _dedupe_preserve_order(list(result.active_roles or expected_roles))
    allowed_edges = _allowed_edges_for_roles(active_roles)
    valid_graph: list[GraphObservation] = []
    invalid_relations: list[str] = []
    for observation in result.graph_delta:
        if allowed_edges and observation.relation not in allowed_edges:
            invalid_relations.append(observation.relation)
            continue
        valid_graph.append(observation)

    gaps = list(result.gaps)
    failure = result.failure
    if invalid_relations:
        message = (
            "Agent returned observations with relations outside the active role contracts: "
            + ", ".join(_dedupe_preserve_order(invalid_relations))
        )
        gaps.append(message)
        if failure is None:
            failure = FailureRecord(
                code="agent_failed",
                message=message,
                blocking=False,
                missingInputs=["role-valid relations"],
            )

    valid_graph = valid_graph[:18]
    answered_questions = list(result.answered_questions)[:6]
    gaps = _dedupe_preserve_order(gaps)[:8]
    next_questions = list(result.next_questions)[:6]
    handoff_recommendations = list(result.handoff_recommendations)[:4]

    return AgentResult(
        agentName=result.agent_name,
        missionId=result.mission_id or mission.mission_id,
        status=result.status,
        summary=result.summary,
        activeRoles=active_roles,
        graphDelta=valid_graph,
        answeredQuestions=answered_questions,
        gaps=gaps,
        nextQuestions=next_questions,
        handoffRecommendations=handoff_recommendations,
        failure=failure,
    )


def _all_role_contract_payload() -> list[dict[str, Any]]:
    return [contract.model_dump(mode="json", by_alias=True) for contract in ROLE_CONTRACTS.values()]


def _selected_role_contract_payload(roles: list[str]) -> list[dict[str, Any]]:
    payload: list[dict[str, Any]] = []
    for role in _dedupe_preserve_order(roles):
        contract = ROLE_CONTRACTS.get(role)  # type: ignore[arg-type]
        if contract is None:
            continue
        payload.append(contract.model_dump(mode="json", by_alias=True))
    return payload


def _reasoning_dedup_hints(state: dict[str, Any]) -> dict[str, Any]:
    """Strings the planner/synthesizer must not accidentally repeat as new questions."""
    last_raw = state.get(STATE_LAST_AGENT_RESULT) or {}
    nq = last_raw.get("nextQuestions") if isinstance(last_raw.get("nextQuestions"), list) else []
    branch_qs: list[str] = []
    for raw in state.get(STATE_BRANCH_STATES) or []:
        if not isinstance(raw, dict):
            continue
        hq = raw.get("humanQuestion") or raw.get("human_question")
        if hq:
            branch_qs.append(str(hq).strip())
    return {
        "openStructuralGapTexts": _open_gap_texts(state)[:12],
        "recentlyAnsweredQuestions": [str(x).strip() for x in (state.get(STATE_ANSWERED_QUESTIONS) or [])[-12:]],
        "lastAgentNextQuestions": [str(x).strip() for x in nq[:10]],
        "recentBranchHumanQuestions": branch_qs[-10:],
    }


def _planner_memory_snapshot(state: dict[str, Any]) -> dict[str, Any]:
    shared = read_shared_memory(_StateToolContext(state))
    return {
        "question": shared["question"],
        "selectedRoles": shared["selectedRoles"],
        "defaultEpistemicMode": shared["defaultEpistemicMode"],
        "toolAvailability": shared["toolAvailability"],
        "currentMission": shared["currentMission"],
        "graphObservationCount": shared["graphObservationCount"],
        "graphObservations": shared["graphObservations"][-8:],
        "graphFragmentCount": shared["graphFragmentCount"],
        "graphFragments": shared["graphFragments"][-4:],
        "branchCount": shared["branchCount"],
        "branches": shared["branches"][-4:],
        "openGaps": shared["openGaps"][-8:],
        "answeredQuestions": shared["answeredQuestions"][-8:],
        "learnedFindings": shared["learnedFindings"][-10:],
        "failures": shared["failures"][-6:],
        "lastAgentResult": shared["lastAgentResult"],
        "frontierSummary": shared["frontierSummary"][-6:],
        "hypotheses": shared["hypotheses"][:8],
        "hypothesisTurns": shared["hypothesisTurns"][-12:],
        "hypothesisFrontiers": _hypothesis_frontier_signals(state),
        "reasoningDedupHints": _reasoning_dedup_hints(state),
    }


def _hypothesis_frontier_signals(state: dict[str, Any]) -> list[dict[str, Any]]:
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    signals: list[dict[str, Any]] = []
    for branch in branches[-8:]:
        discovered = list(branch.discovered_frontier)[:6]
        remaining = list(branch.remaining_frontier)[:6]
        open_questions = list(branch.open_questions)[:6]
        if not discovered and not remaining and not open_questions:
            continue
        signals.append(
            {
                "hypothesisId": branch.branch_id,
                "parentHypothesisId": branch.parent_hypothesis_id,
                "hypothesisRelation": branch.hypothesis_relation,
                "pathKind": branch.path_kind,
                "status": branch.status,
                "humanQuestion": branch.human_question,
                "closureTarget": branch.closure_target,
                "resolvedFrontier": branch.resolved_frontier[:4],
                "remainingFrontier": remaining,
                "discoveredFrontier": discovered,
                "openQuestions": open_questions,
                "pathAssessment": branch.path_assessment,
            }
        )
    return signals


def _branch_root_id_from_plan(state: dict[str, Any], plan: BranchPlan) -> str:
    branches = {
        branch.branch_id: branch
        for branch in [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    }
    if plan.hypothesis_relation == "independent_child":
        return plan.branch_id
    if plan.parent_hypothesis_id and plan.parent_hypothesis_id in branches:
        return _root_hypothesis_id(branches, branches[plan.parent_hypothesis_id])
    if plan.parent_hypothesis_id:
        return plan.parent_hypothesis_id
    return plan.branch_id


def _hypothesis_family_branch_ids(
    state: dict[str, Any],
    plan: BranchPlan,
    previous_state: BranchState | None = None,
) -> list[str]:
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    branches_by_id = {branch.branch_id: branch for branch in branches}
    if plan.hypothesis_relation == "independent_child":
        ids = [plan.branch_id]
        if previous_state is not None:
            ids.append(previous_state.branch_id)
        return _dedupe_preserve_order(ids)

    root_id = _branch_root_id_from_plan(state, plan)
    ids = [
        branch.branch_id
        for branch in branches
        if branch.hypothesis_relation != "independent_child"
        and _root_hypothesis_id(branches_by_id, branch) == root_id
    ]
    if plan.parent_hypothesis_id:
        ids.append(plan.parent_hypothesis_id)
    ids.append(plan.branch_id)
    if previous_state is not None:
        ids.append(previous_state.branch_id)
    return _dedupe_preserve_order(ids)


def _hypothesis_local_state(
    state: dict[str, Any],
    plan: BranchPlan,
    previous_state: BranchState | None = None,
) -> dict[str, Any]:
    family_ids = set(_hypothesis_family_branch_ids(state, plan, previous_state))
    branch_states = [
        BranchState.model_validate(item)
        for item in (state.get(STATE_BRANCH_STATES) or [])
        if isinstance(item, dict) and item.get("branchId") in family_ids
    ]
    fragments: list[GraphFragment] = []
    for branch in branch_states:
        fragments.extend(branch.graph_fragments)
    for raw in state.get(STATE_GRAPH_FRAGMENTS) or []:
        if not isinstance(raw, dict):
            continue
        if raw.get("branchId") in family_ids:
            fragments.append(GraphFragment.model_validate(raw))
    deduped: dict[str, GraphFragment] = {}
    for fragment in fragments:
        deduped[fragment.fragment_id] = fragment
    graph_fragments = list(deduped.values())
    findings = [
        finding
        for finding in _load_learned_findings(state)
        if finding.branch_id in family_ids or finding.hypothesis_id in family_ids
    ]
    return {
        STATE_GRAPH_FRAGMENTS: [item.model_dump(mode="json", by_alias=True) for item in graph_fragments],
        STATE_WORKING_GRAPH: [item.model_dump(mode="json", by_alias=True) for item in _flatten_fragments(graph_fragments)],
        STATE_BRANCH_STATES: [item.model_dump(mode="json", by_alias=True) for item in branch_states],
        STATE_REPLACED_NODE_IDS: list(state.get(STATE_REPLACED_NODE_IDS) or []),
        STATE_LEARNED_FINDINGS: [item.model_dump(mode="json", by_alias=True) for item in findings],
    }


def _is_placeholder_node(node_id: str, label: str) -> bool:
    text = f"{node_id} {label}".strip()
    return any(pattern.search(text) for pattern in _PLACEHOLDER_LABEL_PATTERNS)


def _placeholder_nodes_from_graph(graph: list[GraphObservation]) -> list[str]:
    placeholders: list[str] = []
    for observation in graph:
        for node_id, label in (
            (observation.subject_id, observation.subject),
            (observation.object_id, observation.object),
        ):
            if _is_placeholder_node(node_id, label):
                placeholders.append(label or node_id)
    return _dedupe_preserve_order(placeholders)[:8]


def _is_detail_frontier_text(text: str) -> bool:
    return bool(_DETAIL_FRONTIER_SIGNALS.search(text or ""))


def _structural_frontier_items(frontier: list[str]) -> list[str]:
    return [item for item in _dedupe_preserve_order(frontier) if item and not _is_detail_frontier_text(item)]


def _acceptance_block_reason(
    plan: BranchPlan,
    branch_state: BranchState,
    local_state: dict[str, Any],
) -> str:
    if plan.hypothesis_relation != "root" and plan.path_kind != "overview":
        return ""
    graph = [GraphObservation.model_validate(item) for item in (local_state.get(STATE_WORKING_GRAPH) or [])]
    placeholders = _placeholder_nodes_from_graph(graph)
    if placeholders:
        return f"Broad placeholders remain in the hypothesis graph: {', '.join(placeholders[:3])}."
    structural_frontier = _structural_frontier_items(
        list(branch_state.remaining_frontier) + list(branch_state.open_questions)
    )
    if structural_frontier:
        return f"Structural frontier remains open: {structural_frontier[0]}"
    return ""


def _hypothesis_parallel_branch_summaries(state: dict[str, Any], plan: BranchPlan) -> list[dict[str, Any]]:
    family_ids = set(_hypothesis_family_branch_ids(state, plan))
    summaries: list[dict[str, Any]] = []
    for raw in state.get(STATE_BRANCH_STATES) or []:
        if not isinstance(raw, dict):
            continue
        branch_id = raw.get("branchId")
        if branch_id not in family_ids or branch_id == plan.branch_id:
            continue
        summaries.append(
            {
                "branchId": branch_id,
                "hypothesisRelation": raw.get("hypothesisRelation", ""),
                "pathKind": raw.get("pathKind", ""),
                "status": raw.get("status", ""),
                "humanQuestion": raw.get("humanQuestion", ""),
                "remainingFrontier": list(raw.get("remainingFrontier") or [])[:4],
                "resolvedFrontier": list(raw.get("resolvedFrontier") or [])[:4],
            }
        )
    return summaries[:6]


def _fragment_context_lines(fragments: list[GraphFragment]) -> list[str]:
    return _dedupe_preserve_order(
        [
            f"{obs.subject} --{obs.relation}--> {obs.object}"
            for obs in _flatten_fragments(fragments)
            if obs.subject and obs.object
        ]
    )[:8]


def _normalize_followup_branch_plan(
    parent_plan: BranchPlan,
    branch_state: BranchState | None,
    latest_result: ToolActionResult,
    local_state: dict[str, Any],
    followup: BranchPlan,
) -> BranchPlan:
    inherited_context = _dedupe_preserve_order(
        list(followup.known_context)
        + _fragment_context_lines(latest_result.graph_fragments)
        + list(branch_state.known_context if branch_state is not None else [])
        + _relevant_graph_context(
            local_state,
            target_entities=followup.target_entities or parent_plan.target_entities,
            branch_question=followup.human_question,
        )
    )[:8]
    inherited_frontier = _dedupe_preserve_order(
        list(followup.frontier)
        or list(latest_result.discovered_frontier)
        or list(latest_result.remaining_frontier)
        or list(branch_state.remaining_frontier if branch_state is not None else [])
        or [followup.closure_target]
    )[:6]
    inherited_roles = list(parent_plan.assigned_roles)
    inherited_group = followup.parallel_group or parent_plan.parallel_group
    inherited_budget = max(1, int(followup.stop_after_no_improvement or parent_plan.stop_after_no_improvement or 2))
    return followup.model_copy(
        update={
            "known_context": inherited_context,
            "frontier": inherited_frontier,
            "assigned_roles": inherited_roles,
            "parallel_group": inherited_group,
            "stop_after_no_improvement": inherited_budget,
        }
    )


def _initial_turn_plan(plan: BranchPlan) -> HypothesisTurnPlan:
    tool_name = plan.preferred_tools[0] if plan.preferred_tools else "reason"
    return HypothesisTurnPlan(
        pathKind=plan.path_kind,
        toolName=tool_name,
        humanQuestion=plan.human_question,
        humanAction=plan.human_action,
        whyNow=f"This is the first turn for hypothesis '{plan.hypothesis}'.",
        closureTarget=plan.closure_target,
        knownContext=list(plan.known_context),
        frontier=list(plan.frontier),
        targetEntities=list(plan.target_entities),
        fragmentType=plan.fragment_type,
    )


def _make_hypothesis_turn(
    *,
    plan: BranchPlan,
    turn_index: int,
    action: ToolAction,
    result: ToolActionResult,
    decision_after_result: str,
) -> HypothesisTurn:
    return HypothesisTurn(
        turnId=f"{plan.branch_id}-t{turn_index}",
        hypothesisId=plan.branch_id,
        turnIndex=turn_index,
        pathKind=action.path_kind,
        toolName=action.tool_name,
        humanQuestion=action.human_question,
        humanAction=action.human_action,
        whyNow=action.why_this_question,
        resultSummary=result.human_summary,
        pathAssessment=result.path_assessment,
        closureStatus=result.closure_status,
        graphPatchModes=[fragment.patch_mode for fragment in result.graph_fragments][:6],
        graphFragmentIds=[fragment.fragment_id for fragment in result.graph_fragments][:8],
        learnedFindingIds=[finding.finding_id for finding in result.learned_findings][:8],
        resolvedFrontier=list(result.resolved_frontier)[:8],
        remainingFrontier=list(result.remaining_frontier)[:8],
        discoveredFrontier=list(result.discovered_frontier)[:8],
        decisionAfterResult=decision_after_result,
    )


def _hypothesis_turn_context(
    state: dict[str, Any],
    plan: BranchPlan,
    branch_state: BranchState | None,
    local_state: dict[str, Any],
    latest_result: ToolActionResult,
) -> dict[str, Any]:
    graph = [GraphObservation.model_validate(item) for item in (local_state.get(STATE_WORKING_GRAPH) or [])]
    fragments = [GraphFragment.model_validate(item) for item in (local_state.get(STATE_GRAPH_FRAGMENTS) or [])]
    findings = [LearnedFinding.model_validate(item) for item in (local_state.get(STATE_LEARNED_FINDINGS) or [])]
    turns = list(branch_state.hypothesis_turns if branch_state is not None else [])
    active_role = plan.assigned_roles[0] if len(plan.assigned_roles) == 1 else None
    role_contract = _role_contract(active_role) if active_role is not None else None
    return {
        "originalQuestion": AgentLoomRequest.model_validate(state.get(STATE_REQUEST) or {}).question,
        "currentMission": state.get(STATE_CURRENT_MISSION, {}) or {},
        "specializedRole": active_role or "",
        "rolePlaybook": list(role_contract.investigation_playbook) if role_contract is not None else [],
        "toolUseGuidance": dict(role_contract.tool_use_guidance) if role_contract is not None else {},
        "structuralClosureCriteria": list(role_contract.structural_closure_criteria) if role_contract is not None else [],
        "hypothesis": {
            "hypothesisId": plan.branch_id,
            "statement": plan.hypothesis,
            "parentHypothesisId": plan.parent_hypothesis_id,
            "hypothesisRelation": plan.hypothesis_relation,
            "pathKind": plan.path_kind,
            "closureTarget": plan.closure_target,
            "assignedRoles": plan.assigned_roles,
            "status": branch_state.status if branch_state is not None else "active",
            "pathAssessment": branch_state.path_assessment if branch_state is not None else "still_open",
        },
        "hypothesisGraph": {
            "graphObservationCount": len(graph),
            "graphObservations": [item.model_dump(mode="json", by_alias=True) for item in graph[-18:]],
            "graphFragmentCount": len(fragments),
            "graphFragments": [item.model_dump(mode="json", by_alias=True) for item in fragments[-8:]],
            "placeholderNodes": _placeholder_nodes_from_graph(graph),
        },
        "learnedFindings": [item.model_dump(mode="json", by_alias=True) for item in findings[-10:]],
        "turnHistory": [item.model_dump(mode="json", by_alias=True) for item in turns[-8:]],
        "latestResult": latest_result.model_dump(mode="json", by_alias=True),
        "knownContext": list(branch_state.known_context if branch_state is not None else plan.known_context)[:10],
        "resolvedFrontier": list(branch_state.resolved_frontier if branch_state is not None else [])[:8],
        "remainingFrontier": list(branch_state.remaining_frontier if branch_state is not None else plan.frontier)[:8],
        "discoveredFrontier": list(branch_state.discovered_frontier if branch_state is not None else latest_result.discovered_frontier)[:8],
        "openQuestions": list(branch_state.open_questions if branch_state is not None else [])[:8],
        "parallelBranchSummaries": _hypothesis_parallel_branch_summaries(state, plan),
        "reasoningDedupHints": _reasoning_dedup_hints(state),
        "toolAvailability": read_shared_memory(_StateToolContext(state))["toolAvailability"],
    }


def _allowed_edges_for_roles(roles: list[str]) -> set[str]:
    allowed: set[str] = set()
    for role in roles:
        contract = ROLE_CONTRACTS.get(role)  # type: ignore[arg-type]
        if contract is None:
            continue
        allowed.update(contract.allowed_edge_types)
    return allowed


def _dedupe_sources(values: list[dict[str, Any]]) -> list[dict[str, Any]]:
    seen: set[tuple[str, str, str]] = set()
    result: list[dict[str, Any]] = []
    for value in values:
        key = (
            str(value.get("title", "")),
            str(value.get("uri", "")),
            str(value.get("documentName", "")),
        )
        if key in seen:
            continue
        seen.add(key)
        result.append(value)
    return result


def _build_snippet(text: str, terms: list[str]) -> str:
    lower = text.lower()
    best = -1
    matched_term = ""
    for term in terms:
        idx = lower.find(term)
        if idx >= 0 and (best < 0 or idx < best):
            best = idx
            matched_term = term
    if best < 0:
        return text[:280]
    start = max(0, best - 120)
    end = min(len(text), best + max(len(matched_term), 40) + 180)
    return text[start:end]


def _extract_user_text(ctx: InvocationContext) -> str:
    if getattr(ctx, "user_content", None) and getattr(ctx.user_content, "parts", None):
        text = "".join(part.text or "" for part in ctx.user_content.parts if hasattr(part, "text"))
        if text.strip():
            return text
    events = getattr(ctx.session, "events", []) or []
    for event in reversed(events):
        content = getattr(event, "content", None)
        if not content or getattr(content, "role", "") != "user":
            continue
        text = "".join(part.text or "" for part in content.parts if hasattr(part, "text"))
        if text.strip():
            return text
    raise ValueError("AgentLoom expects the user message to be a JSON request envelope")


def _dedupe_preserve_order(values: list[Any]) -> list[Any]:
    seen: set[Any] = set()
    result: list[Any] = []
    for value in values:
        if value in (None, "", []):
            continue
        if value in seen:
            continue
        seen.add(value)
        result.append(value)
    return result


def _first_non_empty(*values: str | None) -> str:
    for value in values:
        if value and value.strip():
            return value.strip()
    return ""


role_selector = StructuredOutputAgent(
    model=SUPERVISOR_MODEL,
    name="role_selector",
    description="Chooses the smallest sufficient set of investigation roles for the request.",
    instruction=ROLE_SELECTOR_INSTRUCTION,
    generate_content_config=ROLE_SELECTOR_CONFIG,
    output_schema=RoleSelection,
    output_key=STATE_ROLE_SELECTION,
    context_key="role_selector_context",
)

supervisor_reasoner = StructuredOutputAgent(
    model=SUPERVISOR_MODEL,
    name="supervisor_reasoner",
    description="Chooses the next peer agent mission using explicit contracts, selected roles, and current shared state.",
    instruction=SUPERVISOR_INSTRUCTION,
    generate_content_config=SUPERVISOR_CONFIG,
    output_schema=SupervisorDecision,
    output_key=STATE_SUPERVISOR_DECISION,
    context_key=STATE_SUPERVISOR_CONTEXT,
)

investigator_planner = StructuredOutputAgent(
    model=INVESTIGATOR_MODEL,
    name="investigator_planner",
    description="Plans what the investigator can actually do with the current mission, role contracts, and real tools.",
    instruction=INVESTIGATOR_PLANNER_INSTRUCTION,
    generate_content_config=INVESTIGATOR_PLANNER_CONFIG,
    output_schema=InvestigatorPlan,
    output_key=STATE_INVESTIGATOR_PLAN,
    context_key=STATE_INVESTIGATOR_CONTEXT,
)

hypothesis_turn_reasoner = StructuredOutputAgent(
    model=INVESTIGATOR_MODEL,
    name="hypothesis_turn_reasoner",
    description="Chooses the next graph operation inside one active hypothesis after the latest turn result.",
    instruction=HYPOTHESIS_TURN_REASONER_INSTRUCTION,
    generate_content_config=INVESTIGATOR_SYNTHESIZER_CONFIG,
    output_schema=HypothesisTurnDecision,
    output_key=STATE_HYPOTHESIS_TURN_DECISION,
    context_key=STATE_HYPOTHESIS_TURN_CONTEXT,
)

investigator_synthesizer = StructuredOutputAgent(
    model=INVESTIGATOR_MODEL,
    name="investigator_synthesizer",
    description="Looks at all branch results together and identifies cross-branch gaps, bridge questions, and connectivity assessment.",
    instruction=INVESTIGATOR_SYNTHESIZER_INSTRUCTION,
    generate_content_config=INVESTIGATOR_SYNTHESIZER_CONFIG,
    output_schema=InvestigatorSynthesis,
    output_key=STATE_INVESTIGATOR_SYNTHESIS,
    context_key=STATE_INVESTIGATOR_SYNTHESIS_CONTEXT,
)

gap_bridge_reasoner = StructuredOutputAgent(
    model=INVESTIGATOR_MODEL,
    name="gap_bridge_reasoner",
    description="Produces bridging hypotheses or next questions from the current graph and gap state.",
    instruction=GAP_BRIDGE_INSTRUCTION,
    generate_content_config=GAP_BRIDGE_CONFIG,
    output_schema=AgentResult,
    output_key=STATE_GAP_BRIDGE_RESULT,
    context_key=STATE_GAP_BRIDGE_CONTEXT,
)

critic_reasoner = StructuredOutputAgent(
    model=CRITIC_MODEL,
    name="critic_reasoner",
    description="Assesses sufficiency and honesty of the current role-scoped graph state.",
    instruction=CRITIC_INSTRUCTION,
    generate_content_config=CRITIC_CONFIG,
    output_schema=AgentResult,
    output_key=STATE_CRITIC_RESULT,
    context_key=STATE_CRITIC_CONTEXT,
)

root_agent = SupervisorAgent(
    name="agentloom_supervisor",
    description="Google ADK-compatible multi-agent investigation runtime with role selection, shared session memory, explicit agent/tool contracts, and graph-native results.",
    sub_agents=[
        role_selector,
        supervisor_reasoner,
        RoleSpecializedInvestigatorAgent(ROLE_SUPPLY_CHAIN),
        RoleSpecializedInvestigatorAgent(ROLE_GOVERNANCE),
        RoleSpecializedInvestigatorAgent(ROLE_POLICY),
        GapBridgeAgent(),
        CriticAgent(),
        DeterministicComposerAgent(),
    ],
)
