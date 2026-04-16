from __future__ import annotations

# Optional Vertex ADK / GenAI SDKs are usually installed in the dedicated venv.
# pyright: reportMissingImports=false

import hashlib
import json
import os
import re
import time
import uuid
import asyncio
from concurrent.futures import ThreadPoolExecutor
from functools import partial
from collections import deque
from collections.abc import AsyncGenerator
from typing import Any, Iterable, Literal

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
from agentloom.question_intent import extract_question_intent_bundle, intent_to_operational_corridor_dict, regex_operational_corridor_dict
from agentloom.preflight import (
    normalize_investigation_preflight,
    preflight_requires_red_team_pass,
    red_team_frame_eligible,
)
from agentloom.investigation_todos import (
    apply_investigation_todo_updates_from_agent,
    merge_todos_after_graph_progress,
    required_todos_satisfied,
    seed_investigation_todos,
    todos_compact_for_critic,
)
from agentloom.context_helpers import pack_critic_bundle
from agentloom.contracts import (
    AgentContract,
    AgentLoomRequest,
    AgentResult,
    BranchPlan,
    BranchState,
    CandidatePath,
    CorpusDocument,
    CriticAgentOutput,
    EntityNode,
    FailureRecord,
    FlowLeg,
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
    OperationalGraphState,
    PolicyCorrection,
    ProcessStep,
    RoleContract,
    RoleSelection,
    NarrativeSynthesisStatus,
    QuestionIntent,
    RubricLLMExtras,
    RuntimeLogEntry,
    StructuredNarrativeAnswer,
    SupervisorDecision,
    EvidenceLedgerEntry,
    SupplyGraphPatchApply,
    SupplyQAResponse,
    ToolAction,
    ToolActionResult,
    ToolContract,
    critic_agent_output_to_agent_result,
    InvestigationPreflight,
    RedTeamAgentOutput,
    red_team_agent_output_to_agent_result,
)
from agentloom.operational_graph import (
    empty_graph_state,
    evidence_status_for_graph,
    fragment_operational_graph,
    graph_context_lines,
    graph_counts,
    graph_entity_labels,
    graph_state_from_fragments,
    graph_state_from_observations,
    graph_state_from_raw,
    graph_state_to_payload,
    node_label as _graph_node_label,
    node_map as _graph_node_map,
    node_type as _graph_node_type,
    observation_projection,
    process_step_line,
    flow_leg_line,
    overlay_edge_line,
    synthetic_id as _graph_synthetic_id,
    support_types_for_graph,
)
from agentloom.reachability_tiers import compute_typed_operational_backbone_entries
from agentloom.prompt_fragments import (
    JSON_WIRE_CONTRACT_V1,
    SUPPLY_GRAPH_PATCH_APPLY_QUERYTEXT_GUIDANCE,
    SUPPLY_GRAPH_PATCH_APPLY_TOOL_CAPABILITY,
    SUPPLYQA_WIRE_EXAMPLE,
    PROMPT_UNTRUSTED_USER_CORPUS,
    SUPERVISOR_CANON_OF_TERMS,
    SUPERVISOR_RULES_ANTI_BLOAT,
    SUPERVISOR_RULES_CORE,
    SUPERVISOR_RULES_REACHABILITY,
    SUPERVISOR_RULES_RED_TEAM,
    SUPERVISOR_RUNTIME_NOTE,
    INVESTIGATOR_PREFLIGHT_BREADTH_RULE,
    RED_TEAM_INSTRUCTION,
    TOOLACTIONRESULT_WIRE_EXAMPLE,
)
from agentloom.wire_unmarshal import (
    assert_wire_schema_version,
    parse_wire_json,
    recover_wire_json_text,
    unmarshal_wire_model,
)
from agentloom.limits import (
    BUDGET_SOFT_FRACTION,
    COMPOSE_ALLOW_WITH_TYPED_BACKBONE,
    DELEGATE_FLOW_FIRST_STAGNATION_EXCEPTION,
    DISABLE_REPLACE_HOP_REGRESSION,
    HYPOTHESIS_RUNTIME_STEER,
    HYPOTHESIS_RUNTIME_STEER_MODE,
    LIMIT_EXHAUSTED_STRATEGY,
    MAX_GRAPH_DELTA_OBSERVATIONS,
    MAX_GAPS,
    MAX_HANDOFF_RECOMMENDATIONS,
    MAX_NEXT_QUESTIONS,
    MAX_SUPERVISOR_CYCLES,
    MAX_WORKING_GRAPH_OBSERVATIONS,
    MAX_ANSWERED_QUESTIONS,
    MERGE_CONFIDENCE_EPSILON,
    REPEATED_GAP_THRESHOLD,
    SUPERVISOR_SNAPSHOT_FRONTIER_MAX_ITEMS,
    SUPERVISOR_SNAPSHOT_FRONTIER_STRING_CHARS,
    SUPERVISOR_SNAPSHOT_LAST_RESULT_SUMMARY_CHARS,
    SUPERVISOR_SNAPSHOT_OPEN_GAPS_MAX,
    COMPOSE_REQUIRES_TODOS_SATISFIED,
    SUPERVISOR_REACHABILITY_LIST_CAP,
    ALLOW_COMPOSE_WITHOUT_RED_TEAM,
    RED_TEAM_REQUIRED_FOR_BREADTH,
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
STATE_GRAPH_MUTATION_LOG = "graph_mutation_log"
STATE_EMPTY_TYPED_PAYLOAD_STREAK = "empty_typed_graph_payload_streak"
STATE_HYPOTHESIS_ENTITY_REGISTRY = "hypothesis_entity_registry"
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
STATE_RED_TEAM_CONTEXT = "red_team_context"
STATE_RED_TEAM_LLM_PAYLOAD = "red_team_llm_payload"
STATE_RED_TEAM_RESULT = "red_team_agent_result"
STATE_RED_TEAM_ARTIFACT = "red_team_artifact"
STATE_RED_TEAM_SATISFIED = "red_team_satisfied"
STATE_RED_TEAM_COMPOSE_WAIVED = "red_team_compose_waived"
STATE_INVESTIGATION_PREFLIGHT = "investigation_preflight"
STATE_INTERNAL_BRIEF = "internal_brief"
STATE_NARRATIVE_CONTEXT = "narrative_composer_context"
STATE_STRUCTURED_NARRATIVE = "structured_narrative"
STATE_RUBRIC_LLM_CONTEXT = "rubric_llm_context"
STATE_RUBRIC_LLM_RESULT = "rubric_llm_extras"
STATE_LAST_CRITIC_VERDICT = "last_critic_verdict"
STATE_INVESTIGATION_EPOCH = "investigation_epoch"
STATE_OPEN_GAP_STREAK = "open_gap_streak"
STATE_LAST_GAP_FINGERPRINT = "last_gap_fingerprint"
STATE_INVESTIGATION_BUDGET = "investigation_budget"
STATE_COMPOSER_EXTRA_EPISTEMIC_CAVEATS = "composer_extra_epistemic_caveats"
STATE_SUPERVISOR_CYCLE_LIMIT_COMPOSE = "supervisor_cycle_limit_compose"
STATE_RETRIEVAL_CACHE = "retrieve_tool_session_cache"
STATE_QUESTION_INTENT = "question_intent"
STATE_INTENT_META = "intent_extraction_meta"
STATE_INVESTIGATION_TODOS = "investigation_todos"
STATE_TOOL_IDLE_STREAK = "tool_idle_streak"
STATE_EVIDENCE_LEDGER = "evidence_ledger"
STATE_APPLIED_SUPPLY_PATCH_KEYS = "applied_supply_patch_keys"
STATE_HYPOTHESIS_STEER_HINT = "hypothesis_steer_hints"
STATE_LAST_HYPOTHESIS_RUNTIME_TELEMETRY = "hypothesis_runtime_normalize_telemetry"

RESULT_KEY_BY_AGENT = {
    "supply_chain_investigator_agent": STATE_SUPPLY_CHAIN_INVESTIGATOR_RESULT,
    "governance_and_control_investigator_agent": STATE_GOVERNANCE_INVESTIGATOR_RESULT,
    "policy_impact_investigator_agent": STATE_POLICY_INVESTIGATOR_RESULT,
    "gap_bridge_agent": STATE_GAP_BRIDGE_RESULT,
    "critic_agent": STATE_CRITIC_RESULT,
    "red_team_agent": STATE_RED_TEAM_RESULT,
}


def _request_from_state(state: dict[str, Any] | None) -> AgentLoomRequest:
    raw = (state or {}).get(STATE_REQUEST)
    if isinstance(raw, dict) and raw.get("question"):
        return AgentLoomRequest.model_validate(raw)
    return AgentLoomRequest(question="", corpusDocuments=[], requestedRoles=[])


def _load_operational_graph_state(state: dict[str, Any]) -> OperationalGraphState:
    raw = state.get(STATE_WORKING_GRAPH)
    if isinstance(raw, dict):
        return graph_state_from_raw(raw)
    if isinstance(raw, list):
        return graph_state_from_raw(raw)
    return empty_graph_state()


def _save_operational_graph_state(state: dict[str, Any], graph: OperationalGraphState) -> None:
    state[STATE_WORKING_GRAPH] = graph_state_to_payload(graph)


def _working_graph_observations(state: dict[str, Any]) -> list[GraphObservation]:
    return observation_projection(_load_operational_graph_state(state))


def _working_graph_counts(state: dict[str, Any]) -> dict[str, int]:
    return graph_counts(_load_operational_graph_state(state))

INVESTIGATION_ROLES_LITERALS = frozenset({"supply_chain", "governance_and_control", "policy_impact"})

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
    schema_version: int = Field(default=1, alias="schemaVersion", ge=1, le=1)
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
    schema_version: int = Field(default=1, alias="schemaVersion", ge=1, le=1)
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
    # Explicit placeholder markers and schedule-style stubs.
    re.compile(r"\(placeholder\)", re.IGNORECASE),
    re.compile(r"\bplaceholder\s+(?:facility|plant|node|entity|operator)\b", re.IGNORECASE),
    re.compile(
        r"\bunnamed\s+(?:\w+\s+){0,5}(?:facility|plant|manufacturer|operator|producer|converter|processor|refinery|hub)\b",
        re.IGNORECASE,
    ),
    re.compile(r"\btbd\b", re.IGNORECASE),
    re.compile(r"\bto\s+be\s+determined\b", re.IGNORECASE),
    # Generic / unknown operational shells (narrow: requires generic|unknown + operational noun).
    re.compile(
        r"\b(?:generic|unknown)\s+(?:\w+\s+){0,4}(?:extraction|mining|processing|refining|battery)\s+(?:facility|plant|operator|producer)\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\bunknown\s+(?:\w+\s+){0,3}facility\b",
        re.IGNORECASE,
    ),
    # Geographic / regional roll-ups that LLMs use as fake FACILITY anchors (not tied to one legal entity).
    re.compile(
        r"\b(?:Chinese|Asian|European|Indian|Latin\s+American|Middle\s+Eastern|Southeast\s+Asian|South\s+Korean|North\s+American)\s+"
        r"Processing\s+Facility\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:Chinese|Asian|European|Indian|Latin\s+American|Southeast\s+Asian|South\s+Korean)\s+"
        r"Battery(?:\s+Cell)?\s+Manufacturing\s+Hub\b",
        re.IGNORECASE,
    ),
    # "National champion" style processing stand-ins.
    re.compile(
        r"\b(?:Chinese|Asian|European)\s+(?:Chemical\s+)?Processing\s+Hub\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"(?:^|\s)(?:chilean|german|european|asian|global|regional|midstream|upstream|downstream|domestic|international)\s+"
        r"(?:\w+\s+){0,4}(?:facility|plant|processor|converter|manufacturer|producer|deposit)$",
        re.IGNORECASE,
    ),
]
_GENERIC_PLACEHOLDER_NODE_ID_PATTERN = re.compile(
    r"^(?:facility|organization|deposit):"
    r"(?:placeholder|unknown|unnamed|generic|refinery|converter|processor|manufacturer|plant|facility|"
    r"operator|producer|hub|port|evaporation-pond)$",
    re.IGNORECASE,
)
# Extra cues applied only to hypothesis-supported edges to avoid false positives on document-grounded labels.
_PLACEHOLDER_HYPOTHESIS_LABEL_HINTS = re.compile(
    r"(?:\(placeholder\)|\bplaceholder\b|\btbd\b|\bto\s+be\s+determined\b|\bgeneric\s+\w+|\bunknown\s+\w+|\bunnamed\b)",
    re.IGNORECASE,
)
_AGGREGATE_INTERMEDIARY_LABEL_PATTERNS = [
    re.compile(
        r"\b(?:global|regional|international|merchant|spot)\s+(?:market|trading)\s+(?:pool|hub)\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:global|regional|international|third[- ]party|generic|unnamed|unspecified)\s+"
        r"(?:converter|processor|trader|merchant|toll processor)\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:market|trading)\s+pool\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:cathode|battery|chemical|conversion)\s+(?:production|processing|manufacturing)\s+facility\s*\((?:e\.g\.|eg|for example)\b",
        re.IGNORECASE,
    ),
]


class _StateToolContext:
    def __init__(self, state: dict[str, Any]) -> None:
        self.state = state
        self.last_evidence_ledger_entry: EvidenceLedgerEntry | None = None


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
        return _vertex_generate_content_resilient(
            client,
            model=self._model,
            contents=contents,
            config=config_obj,
            allow_quota_model_fallback=True,
            used_resource_fallback_holder=[False],
        )

    def _finalize_parsed_output(self, ctx: InvocationContext, payload: Any) -> BaseModel:
        """Apply schema-specific repair before strict Pydantic validation."""
        if self._output_schema is SupervisorDecision:
            if isinstance(payload, dict):
                d = dict(payload)
            elif isinstance(payload, BaseModel):
                d = payload.model_dump(mode="json", by_alias=True)
            else:
                d = {}
            request_obj = _request_from_state(ctx.session.state)
            d = _coerce_supervisor_decision_payload(
                d,
                selected_role_literals=list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
                cycle=int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0),
                user_question=request_obj.question,
            )
            return SupervisorDecision.model_validate(d)
        if self._output_schema is CriticAgentOutput:
            if isinstance(payload, dict):
                return CriticAgentOutput.model_validate(payload)
            if isinstance(payload, BaseModel):
                return CriticAgentOutput.model_validate(payload.model_dump(mode="json", by_alias=True))
            return CriticAgentOutput.model_validate(payload)
        if self._output_schema is RedTeamAgentOutput:
            if isinstance(payload, dict):
                return RedTeamAgentOutput.model_validate(payload)
            if isinstance(payload, BaseModel):
                return RedTeamAgentOutput.model_validate(payload.model_dump(mode="json", by_alias=True))
            return RedTeamAgentOutput.model_validate(payload)
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
                    json_text = recover_wire_json_text(_response_json_text(response))
                    if self._output_schema is SupervisorDecision:
                        result = self._finalize_parsed_output(ctx, json.loads(json_text))
                    elif self._output_schema is CriticAgentOutput:
                        result = self._finalize_parsed_output(ctx, json.loads(json_text))
                    elif self._output_schema is RedTeamAgentOutput:
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
    fallback=_resolve_model(os.getenv("AGENTLOOM_WORKER_MODEL", ""), fallback="gemini-3.1-flash-lite-preview"),
)
RED_TEAM_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_RED_TEAM_MODEL", ""),
    fallback=INVESTIGATOR_MODEL,
)
COMPOSER_MODEL = _resolve_model(os.getenv("AGENTLOOM_COMPOSER_MODEL", ""), fallback=CRITIC_MODEL)
GROUNDING_MODEL = _resolve_model(os.getenv("AGENTLOOM_GROUNDING_MODEL", ""), fallback="gemini-3.1-flash-lite-preview")
# Quota / 429: optional fallback model inside `_vertex_generate_content_resilient` (tools, question_intent, etc.).
RESOURCE_EXHAUSTION_FALLBACK_MODEL = os.getenv(
    "AGENTLOOM_RESOURCE_EXHAUSTION_FALLBACK_MODEL",
    "gemini-2.5-flash-lite",
).strip()


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
        "red_team_agent",
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
    out = dict(raw)
    # LLM-returned cycle is unreliable; always stamp the real supervisor cycle for logs and logicTrace.
    out["cycle"] = cycle
    next_agent = out.get("nextAgent") or out.get("next_agent")
    if next_agent not in _SUPERVISOR_DELEGATE_AGENTS:
        return out

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
        elif next_agent == "red_team_agent":
            mission["objective"] = (
                "Propose alternative route families, jurisdictions, processing modalities, transport/geopolitical "
                f"angles, adjacent regions, other sources/sinks, and epistemic counterviews grounded in the current "
                f"working graph (do not invent document facts). Frame-broadening pass for: {q}"
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
    max_output_tokens=int(os.getenv("AGENTLOOM_CRITIC_MAX_OUTPUT_TOKENS", "4096")),
    temperature=0,
    response_mime_type="application/json",
)
RED_TEAM_CONFIG = genai_types.GenerateContentConfig(
    max_output_tokens=int(os.getenv("AGENTLOOM_RED_TEAM_MAX_OUTPUT_TOKENS", "8192")),
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
OverlayEdge / derived GraphObservation contract:
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

For supply-chain work, `observations` are a derived compatibility projection only.
Primary operational truth should be emitted through:
- entityNodes
- flowLegs
- processSteps
- overlayEdges
Use direct observations only for overlays or as a last resort when the fragment cannot yet be expressed cleanly through the operational primitives.
"""

OPERATIONAL_PRIMITIVES_CONTRACT = """
Operational fragment primitives:
- entityNodes: array of EntityNode.
  - nodeId: stable node id.
  - label: human-readable label.
  - entityType: one of the allowed entity types.
  - canonicalRef: optional canonical path or stable ref.
  - qualifiers / confidence / supportType / provenance / supportNote / branchId / alternativeGroupId / slotId / slotRole follow the same semantics as observations.
- flowLegs: array of FlowLeg.
  - legId: stable id within the tool result.
  - supplierNodeId: upstream actor or facility node id.
  - customerNodeId: downstream actor or facility node id.
  - productNodeId: the material/product carried on this leg. The product belongs on the leg, not as the leg endpoint.
  - routeNodeIds: optional logistics or transit nodes carried as route qualifiers.
  - qualifiers / confidence / supportType / provenance / supportNote / branchId / alternativeGroupId / slotId / slotRole follow the same semantics as observations.
- processSteps: array of ProcessStep.
  - stepId: stable id within the tool result.
  - hostNodeId: facility or organization where the step happens.
  - processKind: one of ["extraction", "transformation", "assembly"].
  - inputNodeIds: upstream deposits or product-stage nodes consumed by the step.
  - outputNodeIds: downstream product-stage nodes produced by the step.
  - qualifiers / confidence / supportType / provenance / supportNote / branchId / alternativeGroupId / slotId / slotRole follow the same semantics as observations.
- overlayEdges: array of GraphObservation. Use this for ownership, control, geography, and policy overlays that are not flow legs or process steps.
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
Direction contract for compatibility observations and overlay edges:
- Author operational supply-chain flow in `flowLegs` and `processSteps`, not as raw observations.
- The observation relations below are compatibility projections the runtime may derive from native primitives, or overlay relations when they are explicitly returned.
- PRODUCES: ORGANIZATION/FACILITY → PRODUCT. Never product→facility.
- SUPPLIES_TO: ORGANIZATION/FACILITY → ORGANIZATION/FACILITY. Supplier→customer. Product identity belongs on the flow leg via `productNodeId`.
- TRANSFORMS_INTO: PRODUCT → PRODUCT only. Derived from `processSteps(processKind=transformation)`.
- ASSEMBLES_INTO: PRODUCT → PRODUCT only. Derived from `processSteps(processKind=assembly)`.
- USED_IN: PRODUCT → PRODUCT only. Avoid this as primary authoring format for supply-chain flow.
- EXTRACTS_FROM: FACILITY/ORGANIZATION → DEPOSIT only. Derived from `processSteps(processKind=extraction)`.
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
- Deposits are geological sources. Represent extraction as a processStep hosted at a FACILITY/ORGANIZATION, with the deposit in inputNodeIds and the extracted product in outputNodeIds.
- Facilities or organizations move material through flowLegs; the material lives on the leg via productNodeId, not as the leg endpoint.
- Product transformation and assembly belong in processSteps on the host node, not as the primary operational path language.
- Product usage or assembly steps must not terminate in a country or region.
- End-of-chain geography must be represented as FACILITY/ORGANIZATION --LOCATED_IN--> GEO_REGION, not PRODUCT --ASSEMBLES_INTO--> GEO_REGION.
- Prefer concrete typed nodes over abstract placeholders. Labels such as "Regional Mining Facility", "Downstream Battery Plant", or a vague regional producer or refiner acting as a single anonymous stand-in are provisional placeholders, not satisfactory closure when a concrete ORG/FACILITY/DEPOSIT/PRODUCT should exist.
- If you only know a broad placeholder now, keep it with low confidence or supportType=hypothesis, then expose a frontier question or a replacement patch that asks what concrete ORG, FACILITY, DEPOSIT, PRODUCT, or other role-valid typed node should replace it.
- Multiple facilities may legitimately produce or supply the same product. Do not collapse them into one fake node. Return several nodes and edges when the evidence supports several parallel suppliers, receivers, or alternative paths. Expect multiple plausible route families unless the question explicitly narrows to one; use alternativeGroupId and parallel fragments instead of forcing a single winner when evidence supports multiplicity.
- When using `web_search` or corpus-backed `retrieve` for operational facts, each cited supply or transform claim should appear as a typed `flowLeg` or hosted `processStep` with `supportType` (`mixed` or `document` when evidence supports it), `confidence`, and `provenance` / document ids — not only natural-language branch summaries without graph primitives.
- Adjacent-frame context (regional processing near the sink, alternate supplier class, market-pool effects) may appear as separate `alternative_chain` fragments, `learnedFindings`, or low-confidence hypotheses when it informs the user's decision but does not justify a fake direct flow edge between the user's named anchors.
- Structural connectivity matters more than quantitative detail. A typed edge with unknown tonnage is still far more valuable than a tonnage estimate with no typed edge.
"""

SUPPLY_CHAIN_PROFILE_CONTRACT = """
Ideal supply-chain profile for one or several alternative chains:
- source geology: one or more DEPOSIT nodes
- extraction layer: FACILITY and/or ORGANIZATION nodes hosting extraction processSteps
- primary output layer: PRODUCTS emitted by extraction or initial processing processSteps
- transformation layer: one or more processSteps that convert one product stage into another at a named host
- operating facility layer: FACILITY nodes that produce, refine, convert, or assemble along the path
- transport layer: flowLegs carrying a named product from supplier to customer
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
- if you identified only a converter family, jurisdiction, or commercial pattern without a typed flowLeg/processStep or replacement patch, keep the route open; that is context, not operational closure
- do not mark an upstream-midstream-downstream bridge as resolved merely because you named possible converter families or said the market is concentrated elsewhere
- if a direct corridor is unsupported but the business route is still plausible, explicitly ask for an adjacent-country, third-country, or alternative operator-family route rather than stopping at a generic indirect explanation
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
- fragmentType: one of ["overview_chain", "chain_fragment", "alternative_chain", "operational_flow", "ownership_overlay", "policy_overlay", "hypothesis"].
- title: short human-readable title.
- humanQuestion: the exact human-readable question this fragment answers.
- humanSummary: short human-readable summary of what was found.
- branchId: string. Must match the branch being answered.
- alternativeGroupId: string. Use when multiple fragments belong to one alternative-path family.
- startNodeId: optional string. The fragment entry node when known.
- endNodeId: optional string. The fragment exit node when known.
- patchMode: one of ["append", "replace_entity", "replace_fragment"].
  - append = add a new fragment without replacing any prior placeholder.
  - replace_entity = the fragment replaces one broad or fake typed node with a more concrete graph fragment.
  - replace_fragment = the fragment supersedes one or more earlier fragments because the new graph is structurally better.
- oldId: optional string. The single node or fragment id being replaced.
- oldIds: optional string array. Use when replacing several prior placeholders/fragments at once.
- patchRationale: required for replace_entity/replace_fragment. Explain why the old node/fragment was too broad, wrong, or structurally weaker than the new one.
- entityNodes / flowLegs / processSteps / overlayEdges: primary operational payload.
- observations: optional derived compatibility projection. Do not treat it as the primary supply-chain authoring format.
- completeness: one of ["partial", "complete", "hypothesis"].
- qualityScore: number between 0 and 1.

Patch semantics:
- Use replacement when the current graph contains a broad placeholder and you now know the concrete ORG/FACILITY/DEPOSIT/PRODUCT node or fragment that should stand in its place.
- Replacement may expand one node into a multi-hop fragment. Example: replacing a broad downstream battery plant placeholder with a short chain of named FACILITY and PRODUCT stages. Another valid example: replacing a broad upstream mining facility with a concrete mine, deposit, and first salable product output linked by lawful edges.
- Replacement is graph-safe and does not imply that the old node was useless; it means the new fragment is a better structural representation.
- Replacement is a real graph mutation, not decorative metadata. After a valid `replace_entity` or `replace_fragment`, the runtime updates shared memory so later reasoning sees the replacement graph, not the superseded placeholder graph.
- Therefore, when you issue a replacement patch, make the new fragment structurally self-sufficient enough to occupy the old slot in the chain, ownership tree, star, or network that is being improved.
- Think of `patchMode` as the only mutation verb:
  - `append` = extend the current graph;
  - `replace_entity` = replace a broad or wrong node with a better fragment;
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
      "entityNodes": [
        {
          "nodeId": "deposit:example-laterite-deposit",
          "label": "Example laterite deposit",
          "entityType": "DEPOSIT",
          "confidence": 0.79,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence names the laterite deposit.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "nodeId": "facility:example-extraction-complex",
          "label": "Example extraction complex",
          "entityType": "FACILITY",
          "confidence": 0.77,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence ties the facility to the extraction and conversion steps.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "nodeId": "product:example-laterite-ore",
          "label": "Example laterite ore",
          "entityType": "PRODUCT",
          "confidence": 0.77,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence identifies mined laterite output.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "nodeId": "product:example-mixed-intermediate",
          "label": "Example mixed hydroxide intermediate",
          "entityType": "PRODUCT",
          "confidence": 0.74,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Evidence describes leach conversion into the mixed intermediate.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "nodeId": "product:example-battery-input-salt",
          "label": "Example battery-input salt",
          "entityType": "PRODUCT",
          "confidence": 0.71,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Evidence identifies the sulfate-class downstream battery input.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        }
      ],
      "flowLegs": [],
      "processSteps": [
        {
          "stepId": "branch-1-f1-s1",
          "hostNodeId": "facility:example-extraction-complex",
          "processKind": "extraction",
          "inputNodeIds": ["deposit:example-laterite-deposit"],
          "outputNodeIds": ["product:example-laterite-ore"],
          "qualifiers": ["laterite ore body"],
          "confidence": 0.79,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence ties the mining facility to the deposit and ore output.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "stepId": "branch-1-f1-s2",
          "hostNodeId": "facility:example-extraction-complex",
          "processKind": "transformation",
          "inputNodeIds": ["product:example-laterite-ore"],
          "outputNodeIds": ["product:example-mixed-intermediate"],
          "qualifiers": ["hydrometallurgical conversion"],
          "confidence": 0.74,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Evidence describes leach conversion into the mixed intermediate.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        },
        {
          "stepId": "branch-1-f1-s3",
          "hostNodeId": "facility:example-extraction-complex",
          "processKind": "transformation",
          "inputNodeIds": ["product:example-mixed-intermediate"],
          "outputNodeIds": ["product:example-battery-input-salt"],
          "qualifiers": ["downstream crystallization"],
          "confidence": 0.71,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "The grounded path continues into the sulfate-class battery input.",
          "branchId": "branch-1",
          "alternativeGroupId": "route_family_a"
        }
      ],
      "overlayEdges": [],
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
      "patchMode": "replace_entity",
      "oldId": "facility:placeholder-downstream-battery-plant",
      "oldIds": [],
      "patchRationale": "The prior sink node was only a geographic placeholder. The new fragment inserts concrete facilities that should occupy that position in the path.",
      "entityNodes": [
        {
          "nodeId": "product:example-battery-input-salt",
          "label": "Example battery-input salt",
          "entityType": "PRODUCT",
          "confidence": 0.72,
          "supportType": "document",
          "provenance": ["gs://source/doc-9"],
          "supportNote": "Named public evidence identifies the battery-input product stage.",
          "branchId": "branch-3",
          "alternativeGroupId": ""
        },
        {
          "nodeId": "organization:example-materials-distributor",
          "label": "Example materials distributor",
          "entityType": "ORGANIZATION",
          "confidence": 0.69,
          "supportType": "document",
          "provenance": ["gs://source/doc-9"],
          "supportNote": "Public evidence names the intermediary handling the battery-input salt.",
          "branchId": "branch-3",
          "alternativeGroupId": ""
        },
        {
          "nodeId": "facility:example-cathode-input-plant",
          "label": "Example cathode-input plant",
          "entityType": "FACILITY",
          "confidence": 0.72,
          "supportType": "document",
          "provenance": ["gs://source/doc-9"],
          "supportNote": "Named public evidence identifies the receiving plant.",
          "branchId": "branch-3",
          "alternativeGroupId": ""
        }
      ],
      "flowLegs": [
        {
          "legId": "branch-3-f2-l1",
          "supplierNodeId": "organization:example-materials-distributor",
          "customerNodeId": "facility:example-cathode-input-plant",
          "productNodeId": "product:example-battery-input-salt",
          "routeNodeIds": [],
          "qualifiers": [],
          "confidence": 0.72,
          "supportType": "document",
          "provenance": ["gs://source/doc-9"],
          "supportNote": "Named public evidence ties the distributor to battery-material intake at the receiving plant.",
          "branchId": "branch-3",
          "alternativeGroupId": ""
        }
      ],
      "processSteps": [],
      "overlayEdges": [],
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
        "The graph already contains a hypothesis process step: Example placeholder converter transforms Example Ore into Example Battery-Input Product (confidence 0.3, supportType=hypothesis).",
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
  - one `overview_chain` or `chain_fragment` with a connected operational payload (`entityNodes`, `flowLegs`, `processSteps`, `overlayEdges`);
  - it should prefer a connected multi-hop path over isolated facts;
  - if endpoints are still broad, mark them as provisional and expose the missing endpoint-resolution questions in `remainingFrontier`;
- if a later step finds concrete typed nodes or operators, return a replacement fragment with `patchMode=replace_entity` or `replace_fragment` instead of silently leaving the placeholder beside the better graph.
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
        name="supply_qa",
        category="grounding",
        capability="Structured supply-chain Q&A (answer + citations + entity candidates) without committing graph primitives.",
        requiredInputs=["action"],
        outputShape="JSON matching SupplyQAResponse (answer, citations, mentionedEntityCandidates); runtime maps to ToolActionResult with empty graphFragments.",
        failureModes=["returns failed when the model cannot produce valid JSON"],
    ),
    ToolContract(
        name="apply_supply_graph_patch",
        category="graph",
        capability=SUPPLY_GRAPH_PATCH_APPLY_TOOL_CAPABILITY,
        requiredInputs=["action.queryText JSON patch"],
        outputShape="ToolActionResult with merged graphFragments or blocked when evidenceRefIds are missing from the session ledger.",
        failureModes=["rejects invalid JSON", "blocks when evidence ids are not in evidence_ledger"],
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


def _specialized_investigator_tool_names(*, include_apply_patch: bool) -> list[str]:
    names = [contract.name for contract in TOOL_CONTRACTS]
    if not include_apply_patch:
        names = [n for n in names if n != "apply_supply_graph_patch"]
    return names


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
        availableTools=_specialized_investigator_tool_names(include_apply_patch=True),
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
        availableTools=_specialized_investigator_tool_names(include_apply_patch=False),
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
        availableTools=_specialized_investigator_tool_names(include_apply_patch=False),
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
            "supply_qa",
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
    AgentContract(
        name="red_team_agent",
        role="Adversarial breadth agent that proposes alternative route families, epistemic counterviews, and next questions without directly mutating the working graph.",
        missionContract="Requires a concrete InvestigationMission; output is proposal-only (RedTeamAgentOutput) stored as artifacts for supervisor and subsequent investigator missions.",
        resultContract="Returns AgentResult-compatible payload derived from RedTeamAgentOutput; graphDelta should be empty; downstream supply_chain missions may merge proposed branches.",
        sharedMemoryReads=[
            STATE_REQUEST,
            STATE_SELECTED_ROLES,
            STATE_ROLE_CONTRACTS,
            STATE_CURRENT_MISSION,
            STATE_WORKING_GRAPH,
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_INVESTIGATION_PREFLIGHT,
            STATE_TOOL_CONTRACTS,
            STATE_AGENT_CONTRACTS,
        ],
        sharedMemoryWrites=[
            STATE_OPEN_GAPS,
            STATE_FAILURES,
            STATE_LAST_AGENT_RESULT,
            STATE_AGENT_RESULT_HISTORY,
            STATE_RED_TEAM_ARTIFACT,
            STATE_RED_TEAM_RESULT,
            STATE_RED_TEAM_SATISFIED,
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

ROLE_SELECTOR_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
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
)

SUPERVISOR_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are the AgentLoom supervisor.

You orchestrate a Google ADK multi-agent investigation through shared session memory.
You operate inside a graph system specialized in meaningful connected chains of typed entities and typed relations.

Read only `supervisor_context`.

Hard rules:
"""
    + SUPERVISOR_RUNTIME_NOTE
    + SUPERVISOR_CANON_OF_TERMS
    + SUPERVISOR_RULES_CORE
    + SUPERVISOR_RULES_REACHABILITY
    + SUPERVISOR_RULES_ANTI_BLOAT
    + SUPERVISOR_RULES_RED_TEAM
    + """
Return exactly one JSON object matching SupervisorDecision.

Current supervisor context:
{supervisor_context}
"""
)

INVESTIGATOR_PLANNER_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are the internal mission planner for one role-specialized investigator agent.

Use `investigator_context` only.
You operate inside a graph system. Your output is not a prose plan; it is a set of branch plans whose job is to close specific graph frontiers.

Plan only what the real tool contracts and role contracts can support.

Branching strategy — decide based on graph state, not domain:

1. Graph is empty (initialPass=true): create exactly one branch with `fragmentType=overview_chain` that answers the mission question as one connected graph — whatever shape fits (chain, tree, star, network). Tool: `reason`. The question should rephrase the user's question as "give me the whole picture as a connected graph". Optionally add 1-2 parallel grounding branches if internal retrieval is available, but the overview branch is mandatory.
2. Graph already has structure: find edges with `supportType=hypothesis` or `confidence < 0.5` or branches with `remainingFrontier`. Create branches that target those specific weak edges. Each branch question must reference what is already known and what specific gap it closes. Do not re-ask what is already in the graph at high confidence.
3. Supervisor signals frame shift: create 1-2 exploratory branches with a different angle on the same question. A frame-shift branch might ask "is there an intermediary or alternative route?" or "what if the connection works differently than assumed?"
"""
    + INVESTIGATOR_PREFLIGHT_BREADTH_RULE
    + """
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
- Read `sharedMemory.entityRegistrySummary` before planning branches: it lists recent hypothesis-local entity merges (lineage after `replace_entity` / `replace_fragment`) and the largest equivalence classes—do not re-open a graph slot that already merged into a canonical endpoint unless you are widening evidence, not re-adding a duplicate node.
- Read `sharedMemory.hypothesisFrontiers` before planning branches. This is the clearest feed of what the currently active hypotheses just discovered versus what they still need to refine.
- Read `sharedMemory.evidenceLedgerCompact` for algorithmically trimmed evidence tails (ids + short previews) and per-branch eligible id counts; full ledger rows remain in session state for `apply_supply_graph_patch` validation.
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
- Read `sharedMemory.requestedCorridor`, `sharedMemory.commercialBridgeCandidates`, `sharedMemory.aggregateIntermediaryNodes`, and `sharedMemory.alternativeRouteFamilyCandidates`. If the graph still relies on aggregate intermediaries or a narrow bridge was rejected, prioritize either the top commercial bridge candidate or a materially different alternative route family before accepting an unrelated complete route.
- Read `sharedMemory.processGapCandidates`. If a named host already has inbound material legs and outbound product legs but lacks a hosted processStep linking them, prioritize that host-local transform/assembly gap before inventing a remote intermediary.
- **Placeholder branches:** if `sharedMemory.placeholderNodes` is non-empty, include **at least one** branch whose `knownContext` names a specific placeholder label or `oldId` from the graph and asks for resolution or a mergeable fragment that can drive `replace_entity` / `replace_fragment`.
- **Dedup vs targets:** repeating the same *question template* is **not** a duplicate if the **target node** changes (different subject/object endpoint, different `oldId`, or a different entry from `placeholderNodes`).
- If the working graph still lacks a multi-hop **operational backbone**, prefer **one** branch that asks for a **coarse end-to-end or parallel-route chain** (multiple hops in one answer) over several branches that each chase overlapping micro-hops.
- Treat "multi-hop backbone" as a business chain, not a token path: prefer source-anchored routes with multiple flow legs, at least one midstream transform/assembly host, multiple product stages, and a concrete downstream sink. A 1-hop or 2-hop terminal segment is still structural debt.
- Defer narrow legal/importer micro-questions until at least one plausible supplier→customer flow path exists; those are detail-heavy and duplicate easily.

Rules:
- Respect the mission assignedRoles and their role contracts.
- Inside one role-specialized investigator, every emitted branch must keep exactly the current specialized role. Do not emit cross-role follow-up branches here. If another role becomes relevant at an intersection, surface it as frontier or rationale for supervisor instead of delegating across roles inside this plan.
- If the specialized role is `supply_chain` and no business-complete chain exists yet, keep the plan on operational closure. Do not spend a supply-chain branch on ownership, policy, procurement, or market-background work until the graph contains at least one business-complete route or the operational search is clearly exhausted.
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
- For supply-chain graph mutation, use `supply_qa` first for discovery (it appends to the evidence ledger), then `apply_supply_graph_patch` only with `evidenceRefIds` that appear in that ledger for this branch or its hypothesis ancestors. Never emit `apply_supply_graph_patch` as the first tool on a branch with an empty ledger.
"""
    + SUPPLY_GRAPH_PATCH_APPLY_QUERYTEXT_GUIDANCE
    + """
- When the graph is empty, do not start all branches with external grounding. At least one branch must start with `reason`, and no more than one branch may use `retrieve` or `web_search` as its first tool.
- Branches that can be pursued independently may share a `parallelGroup`.
- Multiple branches may belong to the same parent hypothesis and still run in parallel. They should share a `parallelGroup` when they are sibling dependent follow-ups that merge back into the same hypothesis loop.
- If a branch is meant to replace a broad placeholder with a concrete node or fragment, plan it so the returned fragment can occupy that old slot in the graph after mutation. Think in terms of graph replacement, not side commentary.
- If several branch ideas would all land on the same source layer, sink layer, intermediary layer, or ownership layer of the same hypothesis graph, prefer a small number of dependent branches that return mergeable graph fragments. Do not create multiple pseudo-independent chains just because several candidate nodes might fill the same slot.
- **Slots (`slotId` / `slotRole` on observations):** when refining the same sink/source layer, reuse the same `slotId` on new flow edges so the runtime can treat updates as one slot. Only use a new `slotId` when the graph position is genuinely a different role. If two concrete candidates are alternatives, separate them with `alternativeGroupId`, not duplicate `slotId` values with unrelated endpoints.
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
)

INVESTIGATOR_SYNTHESIZER_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are the cross-branch synthesizer inside one role-specialized investigator agent.

Use only `investigator_synthesis_context`.

You have just finished executing several investigation branches in parallel.
Each branch worked independently and could not see what the other branches discovered.
Your job is to look at ALL branch results together and perform five duties: reachability analysis, bridge analysis, hypothesis audit, gap triage, and frame shift suggestion.

Rules:

1. Reachability analysis (USE `reachability` FIELD FIRST):
The context includes a `reachability` field computed from the full working graph. It contains:
- `groundedCompleteChains`: document-grounded paths to a valid terminal sink
- `structurallyCompleteChains`: terminal-reachable business-complete paths that remain weak or hypothetical on at least one hop
- `typedBackboneChains`: typed operational backbone (flow + substantive process + hosts + product) without full business-complete classification
- `partialChains`: paths that stop before a valid terminal sink
- `bridgeCandidates`: entities present in governance/policy layer but not connected to any flow chain
- `routeFamilies`: deduplicated route fingerprints with `multiplicity` (near-duplicate paths collapse here); treat high multiplicity with `narrativeDuplicate=true` as one route family, not many independent discoveries
- `healthSummary`: one-line assessment
Read this field first. If there are **partial chains**, their break point is the highest-priority structural gap. If **structurally complete** chains exist but **grounded** chains do not, say that the skeleton is terminal-connected but not document-grounded. If bridgeCandidates lists entities known to governance but not to flow, those need a product-carrying `flowLeg`, hosted `processStep`, or replacement patch that inserts those primitives.
Report the reachability health in `crossBranchAssessment`. If no **grounded** complete chain exists, say so explicitly and name the break point(s) on partial chains (or the weakest hop on a structural chain).
When interpreting branch results, remember that replacement patches are already applied to the working graph before synthesis. If one branch replaced a placeholder node or an old fragment, synthesize against the replacement graph and do not keep both versions alive conceptually unless the state still explicitly contains both.

2. Bridge analysis — can we fill between X and Y?
For each **partial chain** and each bridge candidate, ask: "Do any of the branch results contain information that could fill the gap between X and Y?" If yes, add the connection to `connectedPairs`. If no, formulate a concrete bridge question in `bridgeQuestions` specifying both endpoints: "What connects X to Y?" or "Who supplies [product] from [facility X] to [facility Y]?"
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
- `synthesisSummary`: one sentence. If no **grounded** complete chain exists, lead with that.
- Keep compact. At most 6 bridgeGaps, 6 bridgeQuestions, 6 connectedPairs, 6 disconnectedPairs, 6 addressedGapTexts.
- Do not invent new graph observations or facts.

Return exactly one JSON object matching InvestigatorSynthesis.

Current synthesis context:
{investigator_synthesis_context}
"""
)

HYPOTHESIS_TURN_REASONER_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are the hypothesis turn reasoner inside one role-specialized investigator agent.

Use only `hypothesis_turn_context`.
You operate inside one active hypothesis. Your job is to decide the next graph operation for that hypothesis after the most recent turn result.

The unit of reasoning is not "which tool should I retry?".
The unit of reasoning is "what graph mutation or graph-closing action should happen next inside this hypothesis?".

Hard rules:
- The original user question still governs the hypothesis. Do not forget it.
- Read the current hypothesis graph, recent turn history, remainingFrontier, discoveredFrontier, and parallel branch summaries before deciding.
- Read `hypothesisGraph.requestedCorridor`, `hypothesisGraph.commercialBridgeCandidates`, `hypothesisGraph.aggregateIntermediaryNodes`, and `hypothesisGraph.alternativeRouteFamilyCandidates`. If the current bridge remains generic or rejected, use an alternative route-family follow-up instead of re-asking the same narrow bridge.
- Read `hypothesisGraph.processGapCandidates`. If a named host already receives product legs and ships a downstream product but the hosted transform/assembly step is missing, treat that host-local process gap as a top-priority structural follow-up.
- If `hypothesisGraph.placeholderNodes` is non-empty, prioritize `nextTurn` / `followupBranches` that **resolve or replace** those placeholders (concrete nodes or `replace_entity`/`replace_fragment` design) over `accept_hypothesis` until the list is cleared or explicitly replaced.
- Treat the hypothesis graph as mutable shared state for this hypothesis. Recent patch fragments already changed the graph you are looking at.
- Read `entityRegistrySummary` for this hypothesis: it shows which node ids merged after replacements—do not plan a turn that reintroduces a tombstoned id or duplicates a slot already merged unless you are splitting alternatives with `alternativeGroupId`.
- **Slot discipline:** when emitting `replace_entity` for a graph slot, keep the same `slotId` on the replacement edges that fill that slot (object endpoint). Use `alternativeGroupId` when two real endpoints are meant to coexist as alternatives.
- A replacement that names a better node but drops the route's upstream or downstream attachment is not good enough. When replacing a midstream slot, reconnect both boundaries or keep the hypothesis open.
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
- If `hypothesisGraph.reachability` still has no grounded or structural business-complete chain, stay on operational closure **unless** `typedBackboneChainCount` is already > 0 and the remaining work is clearly overlay/detail—then you may accept or stall with explicit caveats instead of infinite operational retries. Do not pivot a supply-chain hypothesis into ownership, policy, procurement, or market-background turns until the chain is business-complete, a typed backbone exists with honest limits, or runtime evidence shows that no better operational follow-up remains.
- Naming converter families, stating that supply is global, or describing an integrated commercial model is not enough by itself. Unless the turn added a typed flow leg, process step, or replacement patch, treat the route as still structurally open.
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
)

GAP_BRIDGE_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are `gap_bridge_agent`.

Use only `gap_bridge_context`.
You operate inside a graph system and must work on unresolved graph frontiers, not generic prose gaps.

Your job is to bridge partial graphs and unresolved gaps using the selected role contracts.

Rules:
- `activeRoles` must match the mission assignedRoles.
- Only produce relations allowed by the active role contracts.
- Read JSON field `gap_bridge_context.allowedGraphDeltaRelations` (sorted list). Every `graphDelta[].relation` MUST be one of those strings. Do not emit governance-only relations (for example OPERATES) unless that literal appears in `allowedGraphDeltaRelations` because governance roles are active.
- Only propose new graph observations when they are explicit hypotheses or bridge candidates. Use `supportType=hypothesis`.
- Keep the response compact enough to fit one strict JSON object: at most """
    + str(MAX_GRAPH_DELTA_OBSERVATIONS)
    + """ graph observations, """
    + str(MAX_ANSWERED_QUESTIONS)
    + """ answeredQuestions, """
    + str(MAX_GAPS)
    + """ gaps, """
    + str(MAX_NEXT_QUESTIONS)
    + """ nextQuestions, and """
    + str(MAX_HANDOFF_RECOMMENDATIONS)
    + """ handoffRecommendations.
- Do not restate the graph. Add only bridging hypotheses or targeted nextQuestions.
- Name the frontier you are trying to connect or clarify.
- If no useful bridge is possible, say so and hand off to `critic_agent` or the appropriate role-specialized investigator agent.
- Do not claim grounded evidence you do not have.

Return exactly one JSON object matching AgentResult.

Gap bridge context:
{gap_bridge_context}
"""
)

CRITIC_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
You are `critic_agent`.

Use only `critic_context`.
You operate inside a graph system and judge whether the current graph is connected and decision-useful enough for the question asked.

"""
    + PROMPT_UNTRUSTED_USER_CORPUS
    + """

Required JSON fields (in addition to AgentResult fields such as agentName, missionId, status, summary, graphDelta, gaps):
- compositionRecommendation: one of "recommend" | "do_not_recommend" | "conditional"
- compositionConditions: string array; MUST be non-empty when compositionRecommendation is "conditional" (epistemic caveats the composer must honor)

Gap quality principle — structural vs detail:
When evaluating `openGaps`, classify each: does the graph already contain a typed edge for this connection? If yes, the gap is about quantitative detail (percentage, volume, cost) — it is a DETAIL gap and does NOT block composition. Only gaps where a typed connection is entirely absent are STRUCTURAL gaps. If only detail gaps remain, recommend composition.

Reachability check (USE `reachability` FIELD):
The context includes a `reachability` field. Read it before making your assessment.
- If `groundedCompleteChains` / `completeChainCount` is empty or zero, there is no document-grounded **business-complete** end-to-end flow backbone. That is a serious structural deficit **unless** further grounding is clearly impossible.
- If `structurallyCompleteChains` / `structuralChainCount` is non-empty, the graph has **business-complete but weak** skeletons (hypothesis/model_knowledge on at least one hop). Treat them as **not** grounded proof, but do not ignore them when prioritizing which hop to verify next.
- If `partialChains` lists break points, those are incomplete routes (dead ends) that still need bridging work.
- If `bridgeCandidates` lists entities in governance but not in flow, those are partially useful — they exist but are disconnected.
- If `healthSummary` indicates **no document-grounded complete chain** (or equivalent), do NOT recommend composition unless investigation has stagnated and further progress is implausible.
- When `groundedCompleteChains` / `completeChainCount` is empty but stagnation rules already justify ending investigation, you may recommend composition **only if** the final composed answer will explicitly separate (a) document-supported fragments, (b) structurally complete weak chains / best-supported indirect structure, and (c) adjacent-frame or market-prior context carried as hypothesis or learned findings—not as grounded end-to-end flow. State that requirement in your recommendation rationale.
- Reachability may count chains whose **source** is a typed FACILITY/ORGANIZATION midstream anchor, not only an upstream extract site. When you narrate industry defaults (e.g. naming OEMs), you must still reconcile them with **actual graph node labels**; do not treat narrative substitutes as evidence that broad graph nodes were replaced.

Breadth / multiplicity / red team (USE `investigationPreflight`, `redTeamArtifact`, `redTeamSatisfied`):
- When `investigationPreflight` signals high breadth or requires alternative route families / counterviews / adjacent regions, the composed answer must not read like a single-route story if the graph or red-team artifact contains materially distinct route families or competing honest interpretations.
- If `redTeamSatisfied` is true and `redTeamArtifact` lists epistemic challenges or proposed branches, reflect that multiplicity in your summary and composition conditions unless those proposals were fully merged into the graph in later investigation passes.

Broad-node / placeholder debt (USE `sharedMemory.placeholderNodes` and `sharedMemory.placeholderResolutionHints`):
- These lists are **runtime-detected** broad anchors on flow observations. When either is non-empty, you must treat them as **structural debt**, not as satisfactory named endpoints.
- Your `summary` must **name each** placeholder label (or cite the list) and state that the graph still carries aggregate stand-ins.
- Put at least one **actionable** item into `nextQuestions` or `gaps` per placeholder: e.g. dispatch `supply_chain_investigator_agent` (or the role that owns the hop) to return a `replace_entity` / `replace_fragment` patch with a concrete ORG/FACILITY and `oldId`, or to split into parallel named entities when multiplicity is real.
- While placeholders remain, prefer `compositionRecommendation` = `do_not_recommend` or `conditional` with explicit compositionConditions requiring the composer to label those nodes as **unresolved aggregates** until replaced—**unless** stagnation is extreme and you document that replacement is implausible with available tools.

Rules:
- Do not invent new research facts.
- Evaluate sufficiency, honesty, and role coverage against the selected role contracts.
- Treat broad placeholders and fake typed anchors as unresolved structural issues when a concrete ORG/FACILITY/DEPOSIT/PRODUCT node should exist there. A graph that still relies on regional mining placeholders, generic downstream battery plants, unnamed regional producers, or over-broad product labels is not structurally complete if the investigation already has enough context to ask for the concrete replacement.
- Read the graph after mutation, not before it. If recent branch results used `replace_entity` or `replace_fragment`, judge sufficiency against the replacement graph now in shared memory rather than against the superseded placeholder graph.
- Use `sharedMemory.candidatePaths` and `reachability` together. If the direct path is rejected but one indirect path is already the best supported explanation, do not ask for more broad discovery; either ask for the remaining unresolved hop on that indirect route or recommend composition with an explicit indirect-path answer.
- In mixed mode with no grounding, assess whether the graph is still useful and explicit about its limits.
- Keep the response compact enough to fit one strict JSON object: at most """
    + str(MAX_GRAPH_DELTA_OBSERVATIONS)
    + """ graph observations, """
    + str(MAX_ANSWERED_QUESTIONS)
    + """ answeredQuestions, """
    + str(MAX_GAPS)
    + """ gaps, """
    + str(MAX_NEXT_QUESTIONS)
    + """ nextQuestions, and """
    + str(MAX_HANDOFF_RECOMMENDATIONS)
    + """ handoffRecommendations.
- If the current graph is sufficient for an honest answer AND has at least one grounded complete flow chain, recommend composition explicitly.
- If recent investigation passes are no longer materially improving graph quality or source coverage, recommend composition explicitly even if unresolved gaps remain; the final answer must then surface those gaps honestly.
- If only detail gaps remain (quantitative attributes of known connections), recommend composition. The graph is structurally complete.
- If it is insufficient, identify the exact missing actors, relation classes, route legs, ownership layers, or policy overlays that are still absent.
- Speak in terms of unresolved frontiers: what exact path, actor-layer, or relation family still needs to be closed.
- Recommend another investigation pass only when the missing layers are STRUCTURAL (missing typed edges, not missing attributes) and plausibly obtainable with the currently available tools.
- For supply-chain analysis, use the ideal supply-chain profile below as a diagnostic frame: identify which profile layers are still missing and whether re-query should be holistic or targeted.
- Use `failure` only when you truly cannot assess the state you were given.

"""
    + SUPPLY_CHAIN_PROFILE_CONTRACT
    + """

Return exactly one JSON object matching CriticAgentOutput (AgentResult-compatible fields plus compositionRecommendation and compositionConditions).

Critic context:
{critic_context}
"""
)

NARRATIVE_COMPOSER_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
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
- Read `internalBrief.completeChainCount` / `completeChainCount`, `internalBrief.structuralChainCount` / `structuralChainCount`, and `internalBrief.reachabilityOneLiner` / `reachabilityOneLiner` when present. If there is **no** grounded end-to-end operational chain (`completeChainCount` is 0 or the reachability line states no document-grounded complete flow), then **`chainStructure` and `executiveSummary` must each open** with one or two sentences that state that fact **before** any "most likely" or default-industry storyline. If `structuralChainCount` > 0 while `completeChainCount` is 0, you may briefly note that a structurally complete weak chain exists—without implying it is document-grounded.
- After that lead, you may use cautious market-prior language ("likely", "typically", "often observed") **only** for material already reflected in `internalBrief` bullets, `keyEntities`, `learnedFindingsDigest`, or reachability text—never invent new proper names or contracts.
- `executiveSummary` must be 2–4 sentences and readable standalone **while honoring** the reachability-first lead above.
- If `repairHint` is non-empty, prioritize fixing the issues it lists before anything else.

Return exactly one JSON object matching StructuredNarrativeAnswer.

Composer context:
{narrative_composer_context}
"""
)

RUBRIC_LLM_INSTRUCTION = (
    JSON_WIRE_CONTRACT_V1
    + "\n\n"
    + """
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
)


def read_shared_memory(tool_context) -> dict[str, Any]:
    request = _request_from_state(tool_context.state)
    graph_state = _load_operational_graph_state(tool_context.state)
    graph = observation_projection(graph_state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=tool_context.state)
    commercial_bridge_candidates = _commercial_bridge_candidates(graph_state, state=tool_context.state)
    process_gap_candidates = _host_local_process_gap_candidates(graph_state)
    aggregate_intermediary_nodes = _aggregate_intermediary_nodes_from_graph(graph_state)
    alternative_route_family_candidates = _alternative_route_family_candidates(tool_context.state, graph_state)
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
        "securityNote": PROMPT_UNTRUSTED_USER_CORPUS,
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
        "operationalGraph": graph_state.model_dump(mode="json", by_alias=True),
        "operationalCounts": graph_counts(graph_state),
        "reachability": reachability,
        "requestedCorridor": reachability.get("requestedCorridor") or {},
        "commercialBridgeCandidates": commercial_bridge_candidates,
        "processGapCandidates": process_gap_candidates,
        "aggregateIntermediaryNodes": aggregate_intermediary_nodes,
        "alternativeRouteFamilyCandidates": alternative_route_family_candidates,
        "graphFragmentCount": len(fragments),
        "graphFragments": [item.model_dump(mode="json", by_alias=True) for item in fragments[-10:]],
        "branchCount": len(branches),
        "branches": [_branch_state_for_shared_memory(item) for item in branches[-6:]],
        "evidenceLedgerCompact": _build_evidence_ledger_compact(tool_context.state),
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
        "placeholderNodes": _placeholder_nodes_from_graph(graph_state),
        "placeholderResolutionHints": _placeholder_resolution_hints(graph_state),
        "entityRegistrySummary": _entity_registry_summary_global(tool_context.state),
        "reasoningDedupHints": _reasoning_dedup_hints(tool_context.state),
        "progress": {
            "current": current_progress,
            "stagnationCount": int(tool_context.state.get(STATE_GLOBAL_STAGNATION, 0) or 0),
            "history": list(tool_context.state.get(STATE_PROGRESS_HISTORY) or [])[-6:],
        },
        "questionIntent": tool_context.state.get(STATE_QUESTION_INTENT) or {},
        "intentExtractionMeta": tool_context.state.get(STATE_INTENT_META) or {},
        "investigationTodos": list(tool_context.state.get(STATE_INVESTIGATION_TODOS) or []),
    }


def _compact_shared_memory_for_critic(mem: dict[str, Any]) -> dict[str, Any]:
    """Narrow shared memory for critic prompts (counts + short previews; not full read_shared_memory)."""
    reach = mem.get("reachability") if isinstance(mem.get("reachability"), dict) else {}
    cap = SUPERVISOR_REACHABILITY_LIST_CAP if SUPERVISOR_REACHABILITY_LIST_CAP > 0 else 4

    def _clip(key: str) -> list[Any]:
        v = reach.get(key)
        return v[:cap] if isinstance(v, list) else []

    partial = _clip("partialChains")
    partial_preview: list[dict[str, Any]] = []
    for c in partial:
        if isinstance(c, dict):
            partial_preview.append(
                {
                    "sink": (str(c.get("sink") or ""))[:200],
                    "breakReason": (str(c.get("breakReason") or ""))[:320],
                }
            )
    return {
        "question": str(mem.get("question") or "")[:4000],
        "selectedRoles": mem.get("selectedRoles") or [],
        "currentMission": mem.get("currentMission") or {},
        "openGaps": (mem.get("openGaps") or [])[:16],
        "progress": mem.get("progress") or {},
        "operationalCounts": mem.get("operationalCounts") or {},
        "graphObservationCount": mem.get("graphObservationCount"),
        "reachability": {
            "healthSummary": reach.get("healthSummary"),
            "groundedCompleteChainCount": reach.get("groundedCompleteChainCount"),
            "structuralCompleteChainCount": reach.get("structuralCompleteChainCount"),
            "partialChainCount": reach.get("partialChainCount"),
            "typedBackboneChainCount": reach.get("typedBackboneChainCount"),
            "requestedCorridor": reach.get("requestedCorridor"),
            "requestedCorridorSatisfied": reach.get("requestedCorridorSatisfied"),
            "weakFlowComponentsCount": reach.get("weakFlowComponentsCount"),
            "groundedCompleteChains": _clip("groundedCompleteChains"),
            "structurallyCompleteChains": _clip("structurallyCompleteChains"),
            "typedBackboneChains": _clip("typedBackboneChains"),
            "partialChainBreakPoints": partial_preview[:cap],
        },
        "hypothesisCount": len(mem.get("hypotheses") or []),
        "hypothesesPreview": [h for h in (mem.get("hypotheses") or [])[:4] if isinstance(h, dict)],
        "learnedFindingCount": mem.get("learnedFindingCount"),
        "learnedFindingsPreview": [x for x in (mem.get("learnedFindings") or [])[:5] if isinstance(x, dict)],
        "placeholderNodes": mem.get("placeholderNodes") or [],
        "placeholderResolutionHints": mem.get("placeholderResolutionHints") or [],
        "candidatePaths": (mem.get("candidatePaths") or [])[:10],
    }


def read_corpus_index(tool_context) -> dict[str, Any]:
    request = _request_from_state(tool_context.state)
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
    request = _request_from_state(tool_context.state)
    for doc in request.corpus_documents:
        if doc.document_id == document_id:
            return {
                "ok": True,
                "securityNote": PROMPT_UNTRUSTED_USER_CORPUS,
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
    request = _request_from_state(tool_context.state)
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
        response = _vertex_generate_content_resilient(
            client,
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
            allow_quota_model_fallback=True,
            used_resource_fallback_holder=[False],
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
    normalized: list[GraphObservation] = []
    reversed_count = 0
    dropped_count = 0
    rejected: list[dict[str, Any]] = []
    for idx, raw in enumerate(observations):
        try:
            observation = _observation_ids(
                raw if isinstance(raw, GraphObservation) else GraphObservation.model_validate(raw)
            )
        except Exception as exc:
            rejected.append({"index": idx, "error": str(exc)})
            continue

        valid, should_reverse = _validate_edge_direction(
            observation.relation,
            observation.subject_type,
            observation.object_type,
        )
        if not valid and should_reverse:
            observation = observation.model_copy(
                update={
                    "subject": observation.object,
                    "subject_type": observation.object_type,
                    "subject_id": observation.object_id,
                    "object": observation.subject,
                    "object_type": observation.subject_type,
                    "object_id": observation.subject_id,
                }
            )
            reversed_count += 1
        elif not valid:
            dropped_count += 1
            continue
        normalized.append(observation)

    if not normalized:
        return {
            "appended": 0,
            "graphObservationCount": _working_graph_counts(tool_context.state)["graphObservationCount"],
            "rejected": rejected,
            **({"reversedEdges": reversed_count} if reversed_count else {}),
            **({"droppedInvalidEdges": dropped_count} if dropped_count else {}),
        }

    branch_id = next((item.branch_id for item in normalized if item.branch_id), "runtime")
    fragment = GraphFragment(
        fragmentId=f"runtime-append-{len(tool_context.state.get(STATE_GRAPH_FRAGMENTS) or []) + 1}",
        fragmentType="operational_flow",
        title="Runtime appended observations",
        humanQuestion="",
        humanSummary=f"Runtime appended {len(normalized)} derived observations.",
        branchId=branch_id,
        observations=normalized,
        completeness="partial",
        qualityScore=0.2,
    )
    result = _apply_graph_fragment_mutations(tool_context.state, [fragment])
    result["appended"] = len(normalized)
    result["rejected"] = rejected
    if reversed_count:
        result["reversedEdges"] = reversed_count
    if dropped_count:
        result["droppedInvalidEdges"] = dropped_count
    return result


def _patch_target_ids(fragment: GraphFragment) -> set[str]:
    return {item for item in [fragment.old_id, *fragment.old_ids] if item}


def _observation_references_ids(observation: GraphObservation, ids: set[str]) -> bool:
    return bool(ids) and ((observation.subject_id or "") in ids or (observation.object_id or "") in ids)


def _graph_endpoint_identity_pair(node_id: str, entity_type: str, label: str) -> set[str]:
    """Explicit id plus deterministic label slug id (same rule as _observation_ids)."""
    pair: set[str] = set()
    if node_id and str(node_id).strip():
        pair.add(str(node_id).strip())
    slug_id = f"{_slug(entity_type or 'entity')}:{_slug(label)}"
    if slug_id and slug_id.strip():
        pair.add(slug_id.strip())
    return pair


def _graph_endpoint_identity_tokens(observation: GraphObservation) -> set[str]:
    o = _observation_ids(observation)
    out: set[str] = set()
    out |= _graph_endpoint_identity_pair(o.subject_id, o.subject_type, o.subject)
    out |= _graph_endpoint_identity_pair(o.object_id, o.object_type, o.object)
    out.discard("")
    return out


def _removal_id_closure(all_observations: list[GraphObservation], seed_ids: set[str]) -> set[str]:
    """Expand explicit tombstone ids with label-derived ids for endpoints that already touch the seed.

    Only endpoints whose (id ∪ synthetic) intersects the closure are merged in — not the other
    endpoint of the same edge — so we do not walk the whole graph and delete unrelated edges.
    """
    closure: set[str] = {x.strip() for x in seed_ids if isinstance(x, str) and x.strip()}
    if not closure:
        return set()

    normalized: list[GraphObservation] = []
    for raw in all_observations:
        try:
            if isinstance(raw, GraphObservation):
                normalized.append(_observation_ids(raw))
            else:
                normalized.append(_observation_ids(GraphObservation.model_validate(raw)))
        except Exception:
            continue

    for _ in range(64):
        changed = False
        for o in normalized:
            delta: set[str] = set()
            for nid, typ, lab in (
                (o.subject_id, o.subject_type, o.subject),
                (o.object_id, o.object_type, o.object),
            ):
                pair = _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
                if pair & closure:
                    delta |= pair
            if delta - closure:
                closure |= delta
                changed = True
        if not changed:
            break
    return closure


def _observation_touches_removal_zone(observation: GraphObservation, zone: set[str]) -> bool:
    if not zone:
        return False
    return bool(_graph_endpoint_identity_tokens(observation) & zone)


_FLOW_EDGE_RELATIONS = {
    "PRODUCES",
    "SUPPLIES_TO",
    "TRANSFORMS_INTO",
    "ASSEMBLES_INTO",
    "USED_IN",
    "EXTRACTS_FROM",
}


def _flow_orphan_hints_after_patch(
    graph: list[GraphObservation],
    focus_observations: list[GraphObservation],
) -> list[str]:
    """Heuristic: new/changed endpoints with missing in- or out-degree on flow relations only."""
    g = [_observation_ids(o) for o in graph]
    focus_nodes: set[str] = set()
    for raw in focus_observations:
        try:
            oo = _observation_ids(GraphObservation.model_validate(raw) if not isinstance(raw, GraphObservation) else raw)
        except Exception:
            continue
        for n in (oo.subject, oo.object):
            if n and str(n).strip():
                focus_nodes.add(str(n).strip())
    if not focus_nodes:
        return []

    out_deg: dict[str, int] = {}
    in_deg: dict[str, int] = {}
    for o in g:
        if o.relation not in _FLOW_EDGE_RELATIONS:
            continue
        s, t = o.subject, o.object
        if s and t:
            out_deg[s] = out_deg.get(s, 0) + 1
            in_deg[t] = in_deg.get(t, 0) + 1

    hints: list[str] = []
    for n in sorted(focus_nodes)[:24]:
        ins = in_deg.get(n, 0)
        outs = out_deg.get(n, 0)
        if ins == 0 and outs > 0:
            hints.append(f"{n}: no incoming flow edge among {sorted(_FLOW_EDGE_RELATIONS)} (possible upstream orphan)")
        elif outs == 0 and ins > 0:
            hints.append(f"{n}: no outgoing flow edge among {sorted(_FLOW_EDGE_RELATIONS)} (possible downstream orphan)")
    return hints[:8]


def _replacement_patch_regression_reason(
    before_graph: OperationalGraphState,
    after_graph: OperationalGraphState,
) -> str:
    before_report = _graph_reachability_report_for_graph(before_graph)
    after_report = _graph_reachability_report_for_graph(after_graph)
    before_complete = sum(_reachability_complete_counts(before_report))
    after_complete = sum(_reachability_complete_counts(after_report))
    before_hops = int(before_report.get("maxOperationalRouteHops") or _max_route_hops_from_report(before_report))
    after_hops = int(after_report.get("maxOperationalRouteHops") or _max_route_hops_from_report(after_report))

    if before_complete > after_complete:
        return (
            "replacement would remove an already reachable business-complete chain "
            f"({before_complete} -> {after_complete})"
        )
    if DISABLE_REPLACE_HOP_REGRESSION:
        return ""
    if before_hops >= 4 and after_hops + 1 < before_hops:
        after_partial = after_report.get("partialChains") or []
        reason = str(after_partial[0].get("breakReason") or "").strip() if after_partial else ""
        if reason:
            return (
                f"replacement would shrink the best reachable route from {before_hops} to {after_hops} hops; "
                f"post-patch break: {reason}"
            )
        return f"replacement would shrink the best reachable route from {before_hops} to {after_hops} hops"
    return ""


def _fragment_referenced_ids(fragment: GraphFragment) -> set[str]:
    refs: set[str] = set()
    for node in fragment.entity_nodes:
        if node.node_id:
            refs.add(node.node_id)
    for leg in fragment.flow_legs:
        refs.update([leg.supplier_node_id, leg.customer_node_id, leg.product_node_id, *leg.route_node_ids])
    for step in fragment.process_steps:
        refs.update([step.host_node_id, *step.input_node_ids, *step.output_node_ids])
    for edge in [*fragment.overlay_edges, *fragment.observations]:
        if edge.subject_id:
            refs.add(edge.subject_id)
        if edge.object_id:
            refs.add(edge.object_id)
    refs.discard("")
    return refs


def _fragment_touches_removal_zone(fragment: GraphFragment, zone: set[str]) -> bool:
    if not zone:
        return False
    return bool(_fragment_referenced_ids(fragment) & zone)


def _strip_fragment_removal_zone(fragment: GraphFragment, zone: set[str]) -> GraphFragment | None:
    if not zone:
        return fragment
    payload = fragment.model_dump(mode="json", by_alias=True)
    payload["entityNodes"] = [
        item for item in payload.get("entityNodes", [])
        if str(item.get("nodeId") or "").strip() not in zone
    ]
    payload["flowLegs"] = [
        item
        for item in payload.get("flowLegs", [])
        if str(item.get("supplierNodeId") or "").strip() not in zone
        and str(item.get("customerNodeId") or "").strip() not in zone
        and str(item.get("productNodeId") or "").strip() not in zone
        and not any(str(route_id or "").strip() in zone for route_id in (item.get("routeNodeIds") or []))
    ]
    payload["processSteps"] = [
        item
        for item in payload.get("processSteps", [])
        if str(item.get("hostNodeId") or "").strip() not in zone
        and not any(str(node_id or "").strip() in zone for node_id in (item.get("inputNodeIds") or []))
        and not any(str(node_id or "").strip() in zone for node_id in (item.get("outputNodeIds") or []))
    ]
    payload["overlayEdges"] = [
        item
        for item in payload.get("overlayEdges", [])
        if str(item.get("subjectId") or "").strip() not in zone
        and str(item.get("objectId") or "").strip() not in zone
    ]
    payload["observations"] = [
        item
        for item in payload.get("observations", [])
        if str(item.get("subjectId") or "").strip() not in zone
        and str(item.get("objectId") or "").strip() not in zone
    ]
    if not any(
        payload.get(key)
        for key in ("entityNodes", "flowLegs", "processSteps", "overlayEdges", "observations")
    ):
        return None
    return GraphFragment.model_validate(payload)


def _fragment_with_recomputed_boundaries(fragment: GraphFragment, observations: list[GraphObservation] | None = None) -> GraphFragment | None:
    payload = fragment.model_dump(mode="json", by_alias=True)
    if observations is not None:
        payload["observations"] = [item.model_dump(mode="json", by_alias=True) for item in observations]
    rebuilt = GraphFragment.model_validate(payload)
    if not any(
        (
            rebuilt.entity_nodes,
            rebuilt.flow_legs,
            rebuilt.process_steps,
            rebuilt.overlay_edges,
            rebuilt.observations,
        )
    ):
        return None
    return rebuilt


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


def _branch_graph_fragments_from_state(state: dict[str, Any], branch_id: str) -> list[GraphFragment]:
    return [
        GraphFragment.model_validate(item)
        for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])
        if isinstance(item, dict) and item.get("branchId") == branch_id
    ]


def _apply_graph_fragment_mutations(
    state: dict[str, Any],
    incoming_fragments: list[GraphFragment],
) -> dict[str, Any]:
    if incoming_fragments:
        has_ops = any(bool(f.flow_legs or f.process_steps) for f in incoming_fragments)
        if not has_ops:
            state[STATE_EMPTY_TYPED_PAYLOAD_STREAK] = int(state.get(STATE_EMPTY_TYPED_PAYLOAD_STREAK) or 0) + 1
        else:
            state[STATE_EMPTY_TYPED_PAYLOAD_STREAK] = 0
    pre_merge_graph = _load_operational_graph_state(state)
    pre_merge_counts = graph_counts(pre_merge_graph)
    existing_fragments = [
        _fragment_with_recomputed_boundaries(GraphFragment.model_validate(item))
        for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])
    ]
    merged_fragments = [item for item in existing_fragments if item is not None]
    legacy_observations = _working_graph_observations(state)
    if legacy_observations:
        existing_keys = {_observation_key(item) for item in _flatten_fragments(merged_fragments)}
        residual_legacy = [item for item in legacy_observations if _observation_key(item) not in existing_keys]
        if residual_legacy:
            merged_fragments = [
                item for item in merged_fragments
                if item.fragment_id != "__legacy_working_graph__"
            ]
            merged_fragments.append(
                GraphFragment(
                    fragmentId="__legacy_working_graph__",
                    fragmentType="operational_flow",
                    title="Legacy working graph residual",
                    humanQuestion="",
                    humanSummary="Residual working-graph observations preserved during graph mutation.",
                    branchId="runtime",
                    observations=residual_legacy,
                    completeness="partial",
                    qualityScore=0.0,
                )
            )
    tombstoned_node_ids = {item for item in (state.get(STATE_REPLACED_NODE_IDS) or []) if item}

    if tombstoned_node_ids:
        cleaned_fragments: list[GraphFragment] = []
        for fragment in merged_fragments:
            updated_fragment = _strip_fragment_removal_zone(fragment, tombstoned_node_ids)
            if updated_fragment is not None:
                cleaned_fragments.append(updated_fragment)
        merged_fragments = cleaned_fragments

    replaced_fragment_ids: set[str] = set()
    replaced_node_ids: set[str] = set()
    session_removal_zone: set[str] = set()
    patch_summaries: list[dict[str, Any]] = []
    last_incoming_observations: list[GraphObservation] = []

    for fragment in incoming_fragments:
        normalized_fragment = _fragment_with_recomputed_boundaries(fragment, fragment.observations)
        if normalized_fragment is None:
            continue
        if tombstoned_node_ids:
            normalized_fragment = _strip_fragment_removal_zone(normalized_fragment, tombstoned_node_ids)
            if normalized_fragment is None:
                continue
        fragment_already_applied = False
        target_ids = _patch_target_ids(normalized_fragment)
        if normalized_fragment.patch_mode == "replace_fragment":
            replaced_fragment_ids.update(target_ids)
            merged_fragments = [item for item in merged_fragments if item.fragment_id not in target_ids]
            patch_summaries.append(
                {
                    "fragmentId": normalized_fragment.fragment_id,
                    "patchMode": "replace_fragment",
                    "targetFragmentIds": sorted(target_ids),
                    "removalZoneSize": 0,
                }
            )
            _registry_record_patch(
                state,
                branch_id=normalized_fragment.branch_id,
                fragment_id=normalized_fragment.fragment_id,
                patch_mode="replace_fragment",
                removal_zone=set(),
                incoming_observations=list(normalized_fragment.observations),
                incoming_fragment=normalized_fragment,
                explicit_seeds=set(),
            )
        elif normalized_fragment.patch_mode == "replace_entity":
            before_patch_graph = graph_state_from_fragments(merged_fragments)
            seed = set(target_ids)
            ambient_n = [
                *[obs for existing_fragment in merged_fragments for obs in existing_fragment.observations],
                *normalized_fragment.observations,
            ]
            removal_zone = _removal_id_closure(ambient_n, seed)
            candidate_fragments: list[GraphFragment] = []
            for existing_fragment in merged_fragments:
                updated_fragment = _strip_fragment_removal_zone(existing_fragment, removal_zone)
                if updated_fragment is not None:
                    candidate_fragments.append(updated_fragment)
            candidate_fragments = [item for item in candidate_fragments if item.fragment_id != normalized_fragment.fragment_id]
            candidate_fragments.append(normalized_fragment)
            regression_reason = _replacement_patch_regression_reason(
                before_patch_graph,
                graph_state_from_fragments(candidate_fragments),
            )
            if regression_reason:
                reject_code = "hop_regression" if "shrink" in regression_reason else "chain_regression"
                patch_summaries.append(
                    {
                        "fragmentId": normalized_fragment.fragment_id,
                        "patchMode": "replace_entity",
                        "explicitOldIds": sorted(seed),
                        "removalZoneSize": len(removal_zone),
                        "rejectedReason": regression_reason,
                        "rejectCode": reject_code,
                    }
                )
                record_open_gaps(
                    [
                        f"Replacement patch {normalized_fragment.fragment_id} was rejected because {regression_reason}."
                    ],
                    _StateToolContext(state),
                    origin="replaceEntityAudit",
                    cycle=int(state.get(STATE_SUPERVISOR_CYCLE, 0) or 0),
                )
                continue
            session_removal_zone |= removal_zone
            replaced_node_ids.update(seed)
            tombstoned_node_ids.update(seed)
            merged_fragments = candidate_fragments
            fragment_already_applied = True
            patch_summaries.append(
                {
                    "fragmentId": normalized_fragment.fragment_id,
                    "patchMode": "replace_entity",
                    "explicitOldIds": sorted(seed),
                    "removalZoneSize": len(removal_zone),
                }
            )
            _registry_record_patch(
                state,
                branch_id=normalized_fragment.branch_id,
                fragment_id=normalized_fragment.fragment_id,
                patch_mode="replace_entity",
                removal_zone=removal_zone,
                incoming_observations=list(normalized_fragment.observations),
                incoming_fragment=normalized_fragment,
                explicit_seeds=set(seed),
            )

        if not fragment_already_applied:
            merged_fragments = [item for item in merged_fragments if item.fragment_id != normalized_fragment.fragment_id]
            merged_fragments.append(normalized_fragment)
        if normalized_fragment.observations:
            last_incoming_observations = list(normalized_fragment.observations)

    if session_removal_zone:
        cleaned_fragments = []
        for fragment in merged_fragments:
            updated_fragment = _strip_fragment_removal_zone(fragment, session_removal_zone)
            if updated_fragment is not None:
                cleaned_fragments.append(updated_fragment)
        merged_fragments = cleaned_fragments

    graph_state = graph_state_from_fragments(merged_fragments)
    fragment_graph = observation_projection(graph_state)
    state[STATE_GRAPH_FRAGMENTS] = [item.model_dump(mode="json", by_alias=True) for item in merged_fragments]
    _save_operational_graph_state(state, graph_state)
    state[STATE_REPLACED_NODE_IDS] = sorted(tombstoned_node_ids)

    if STATE_BRANCH_STATES in state:
        synced = _sync_branch_graph_fragments(
            [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])],
            merged_fragments,
        )
        state[STATE_BRANCH_STATES] = [item.model_dump(mode="json", by_alias=True) for item in synced]

    post_counts = graph_counts(graph_state)
    result = {
        "graphObservationCount": post_counts["graphObservationCount"],
        "entityNodeCount": post_counts["entityNodeCount"],
        "flowLegCount": post_counts["flowLegCount"],
        "processStepCount": post_counts["processStepCount"],
        "overlayEdgeCount": post_counts["overlayEdgeCount"],
    }
    if replaced_fragment_ids:
        result["replacedFragmentIds"] = sorted(replaced_fragment_ids)
    if replaced_node_ids:
        result["replacedNodeIds"] = sorted(replaced_node_ids)
    result["graphFragmentCount"] = len(merged_fragments)
    result["removalZoneSize"] = len(session_removal_zone)
    result["removalZoneSample"] = sorted(session_removal_zone)[:24]
    result["workingGraphObservationDelta"] = post_counts["graphObservationCount"] - pre_merge_counts["graphObservationCount"]
    result["workingGraphNodeDelta"] = post_counts["entityNodeCount"] - pre_merge_counts["entityNodeCount"]
    result["workingGraphLegDelta"] = post_counts["flowLegCount"] - pre_merge_counts["flowLegCount"]
    result["workingGraphStepDelta"] = post_counts["processStepCount"] - pre_merge_counts["processStepCount"]

    orphan_hints = _flow_orphan_hints_after_patch(fragment_graph, last_incoming_observations)
    result["orphanFlowHints"] = orphan_hints

    slot_gaps = _detect_slot_endpoint_conflicts(fragment_graph)
    if slot_gaps:
        record_open_gaps(
            slot_gaps,
            _StateToolContext(state),
            origin="slotContract",
            cycle=int(state.get(STATE_SUPERVISOR_CYCLE, 0) or 0),
        )

    log_entry: dict[str, Any] = {
        "patchSummaries": patch_summaries,
        "explicitReplacedNodeIds": sorted(replaced_node_ids),
        "replacedFragmentIds": sorted(replaced_fragment_ids),
        "removalZoneSize": len(session_removal_zone),
        "removalZoneSample": sorted(session_removal_zone)[:24],
        "workingGraphObservationsBefore": pre_merge_counts["graphObservationCount"],
        "workingGraphObservationsAfter": post_counts["graphObservationCount"],
        "workingGraphNodesBefore": pre_merge_counts["entityNodeCount"],
        "workingGraphNodesAfter": post_counts["entityNodeCount"],
        "workingGraphLegsBefore": pre_merge_counts["flowLegCount"],
        "workingGraphLegsAfter": post_counts["flowLegCount"],
        "workingGraphStepsBefore": pre_merge_counts["processStepCount"],
        "workingGraphStepsAfter": post_counts["processStepCount"],
        "orphanFlowHints": orphan_hints,
    }
    history = [item for item in (state.get(STATE_GRAPH_MUTATION_LOG) or []) if isinstance(item, dict)]
    history.append(log_entry)
    state[STATE_GRAPH_MUTATION_LOG] = history[-24:]

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
    graph_entities = {label.lower().strip() for label in graph_entity_labels(_load_operational_graph_state(state), limit=128)}
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


def _synthetic_id_for_endpoint(entity_type: str, label: str) -> str:
    return f"{_slug(entity_type or 'entity')}:{_slug(label)}"


_SCOPE_TOKEN_STOPWORDS = {
    "the",
    "a",
    "an",
    "is",
    "are",
    "of",
    "in",
    "to",
    "for",
    "and",
    "or",
    "from",
    "by",
    "with",
    "on",
    "at",
    "what",
    "which",
    "how",
    "that",
    "this",
    "it",
    "its",
    "map",
    "identify",
    "surface",
    "likely",
    "strongest",
    "pressure",
    "points",
    "supply",
    "chain",
}


def _normalize_scope_token(token: str) -> str:
    text = re.sub(r"[^a-z0-9]+", "", (token or "").lower())
    for suffix in ("ing", "ers", "ies", "ers", "es", "s", "ed", "or", "ion", "al", "an"):
        if len(text) > len(suffix) + 3 and text.endswith(suffix):
            text = text[: -len(suffix)]
            break
    return text


def _scope_tokens(text: str) -> list[str]:
    out: list[str] = []
    seen: set[str] = set()
    for raw in re.findall(r"[A-Za-z0-9]+", (text or "").lower()):
        if raw in _SCOPE_TOKEN_STOPWORDS or len(raw) <= 2:
            continue
        normalized = _normalize_scope_token(raw)
        if len(normalized) <= 2 or normalized in _SCOPE_TOKEN_STOPWORDS or normalized in seen:
            continue
        seen.add(normalized)
        out.append(normalized)
    return out


def _scope_tokens_compatible(left: str, right: str) -> bool:
    a = _normalize_scope_token(left)
    b = _normalize_scope_token(right)
    if not a or not b:
        return False
    if a == b:
        return True
    return len(a) >= 4 and len(b) >= 4 and (a.startswith(b) or b.startswith(a))


def _scope_matches_text(blob: str, scope_text: str) -> bool:
    scope = " ".join((scope_text or "").strip().lower().split())
    if not scope:
        return False
    blob_l = " ".join((blob or "").strip().lower().split())
    if scope and scope in blob_l:
        return True
    scope_tokens = _scope_tokens(scope)
    blob_tokens = _scope_tokens(blob_l)
    if not scope_tokens or not blob_tokens:
        return False
    matched = 0
    for needle in scope_tokens:
        if any(_scope_tokens_compatible(needle, token) for token in blob_tokens):
            matched += 1
    threshold = 1 if len(scope_tokens) <= 2 else min(len(scope_tokens), max(2, len(scope_tokens) - 1))
    return matched >= threshold


def _strip_scope_phrase(text: str) -> str:
    cleaned = re.sub(
        r"\b(?:identify|surface|show|map|trace|assess|find|describe|explain)\b.*$",
        "",
        (text or "").strip(),
        flags=re.IGNORECASE,
    )
    cleaned = re.sub(r"^[\s,;:.-]+|[\s,;:.-]+$", "", cleaned)
    cleaned = re.sub(r"^(?:a|an|the)\s+", "", cleaned, flags=re.IGNORECASE)
    return " ".join(cleaned.split())


def _requested_operational_corridor(request: AgentLoomRequest) -> dict[str, Any]:
    """Regex-only corridor (legacy); prefer `_operational_corridor_from_state` when session has QuestionIntent."""
    return regex_operational_corridor_dict(request.question)


def _operational_corridor_from_state(state: dict[str, Any]) -> dict[str, Any]:
    raw = state.get(STATE_QUESTION_INTENT)
    if raw:
        try:
            return intent_to_operational_corridor_dict(QuestionIntent.model_validate(raw))
        except Exception:
            pass
    return _requested_operational_corridor(_request_from_state(state))


def _requested_corridor_intent_debug(state: dict[str, Any] | None) -> dict[str, Any]:
    """Surface whether QuestionIntent drove corridor (for supervisor / reachability JSON)."""
    if not state:
        return {"usedQuestionIntent": False}
    raw = state.get(STATE_QUESTION_INTENT)
    if not isinstance(raw, dict):
        return {"usedQuestionIntent": False}
    try:
        qi = QuestionIntent.model_validate(raw)
        return {
            "usedQuestionIntent": True,
            "intentSchemaVersion": qi.intent_schema_version,
            "corridorActive": qi.corridor_active,
            "hasSourceSinkText": bool((qi.source_scope or "").strip() or (qi.sink_scope or "").strip()),
        }
    except Exception:
        return {"usedQuestionIntent": False, "intentParseFailed": True}


def _investigation_reachability_ops_bundle(
    state: dict[str, Any],
    graph_state: OperationalGraphState | None = None,
) -> dict[str, Any]:
    """Reachability + operational extras shared by planner, hypothesis graph, and similar contexts."""
    gs = graph_state or _load_operational_graph_state(state)
    reach = _graph_reachability_report_for_graph(gs, state=state)
    return {
        "reachability": reach,
        "requestedCorridor": reach.get("requestedCorridor") or {},
        "commercialBridgeCandidates": _commercial_bridge_candidates(gs, state=state),
        "processGapCandidates": _host_local_process_gap_candidates(gs),
        "aggregateIntermediaryNodes": _aggregate_intermediary_nodes_from_graph(gs),
        "alternativeRouteFamilyCandidates": _alternative_route_family_candidates(state, gs),
    }


def _weak_flow_component_count(graph_state: OperationalGraphState) -> int:
    comp = _operational_component_index(graph_state)
    return len({v for v in comp.values()}) if comp else 0


def _request_document_index(request: AgentLoomRequest) -> dict[str, CorpusDocument]:
    index: dict[str, CorpusDocument] = {}
    for doc in request.corpus_documents:
        for key in {doc.document_id, doc.source_uri}:
            value = str(key or "").strip()
            if value:
                index[value] = doc
    return index


def _provenance_text_fragments(request: AgentLoomRequest, provenance: Iterable[str]) -> list[str]:
    doc_index = _request_document_index(request)
    out: list[str] = []
    seen: set[str] = set()
    for raw in provenance:
        key = str(raw or "").strip()
        if not key or key in seen:
            continue
        seen.add(key)
        doc = doc_index.get(key)
        if doc is None:
            continue
        out.append(doc.title)
        out.append(doc.text[:600])
        if doc.tags:
            out.append(" ".join(doc.tags))
        if doc.metadata:
            out.append(" ".join(f"{k} {v}" for k, v in sorted(doc.metadata.items()) if str(v).strip()))
    return out


def _node_supporting_text(
    node_id: str,
    node_index: dict[str, EntityNode],
    graph_state: OperationalGraphState,
    request: AgentLoomRequest,
) -> str:
    node = node_index.get(node_id)
    parts: list[str] = []
    if node is not None:
        parts.extend(
            [
                node.label,
                node.canonical_ref,
                " ".join(node.qualifiers),
                node.support_note,
                *node.provenance,
                *_provenance_text_fragments(request, node.provenance),
            ]
        )
    for leg in graph_state.flow_legs:
        if node_id not in {leg.supplier_node_id, leg.customer_node_id, leg.product_node_id, *leg.route_node_ids}:
            continue
        parts.extend([leg.support_note, *leg.qualifiers, *leg.provenance, *_provenance_text_fragments(request, leg.provenance)])
    for step in graph_state.process_steps:
        if node_id not in {step.host_node_id, *step.input_node_ids, *step.output_node_ids}:
            continue
        parts.extend([step.support_note, *step.qualifiers, *step.provenance, *_provenance_text_fragments(request, step.provenance)])
    return " ".join(part for part in parts if str(part or "").strip())


def _record_touched_node_ids(record: dict[str, Any]) -> list[str]:
    out: list[str] = []
    for op in record.get("ops") or []:
        for node_id in (
            op.get("fromHostId"),
            op.get("toHostId"),
            op.get("hostId"),
            op.get("productId"),
            op.get("outputId"),
            *list(op.get("inputIds") or []),
        ):
            nid = str(node_id or "").strip()
            if nid and nid not in out:
                out.append(nid)
    return out


def _requested_corridor_profile_for_record(
    *,
    record: dict[str, Any],
    host_id: str,
    product_id: str,
    sink_label: str,
    node_index: dict[str, EntityNode],
    graph_state: OperationalGraphState,
    request: AgentLoomRequest,
    corridor: dict[str, Any],
) -> dict[str, Any]:
    if not corridor.get("active"):
        return {
            "active": False,
            "sourceScope": "",
            "sinkScope": "",
            "matchesSourceScope": True,
            "matchesSinkScope": True,
            "missingCriteria": [],
            "isSatisfied": True,
        }
    source_scope = str(corridor.get("sourceScope") or "").strip()
    sink_scope = str(corridor.get("sinkScope") or "").strip()
    touched_nodes = _record_touched_node_ids(record)
    if host_id and host_id not in touched_nodes:
        touched_nodes.append(host_id)
    if product_id and product_id not in touched_nodes:
        touched_nodes.append(product_id)
    touched_blobs = [
        _node_supporting_text(node_id, node_index, graph_state, request)
        for node_id in touched_nodes
        if node_id
    ]
    source_matches = True
    sink_matches = True
    if source_scope:
        source_matches = any(
            _scope_matches_text(blob, source_scope)
            for blob in [record.get("sourceLabel") or "", *touched_blobs]
        )
    if sink_scope:
        sink_blob = _node_supporting_text(host_id, node_index, graph_state, request) if host_id else sink_label
        sink_matches = any(
            _scope_matches_text(blob, sink_scope)
            for blob in [sink_label, sink_blob, *touched_blobs]
        )
    missing: list[str] = []
    if source_scope and not source_matches:
        missing.append(f"route does not yet connect the requested source side: {source_scope}")
    if sink_scope and not sink_matches:
        missing.append(f"route does not yet connect the requested sink side: {sink_scope}")
    return {
        "active": True,
        "sourceScope": source_scope,
        "sinkScope": sink_scope,
        "matchesSourceScope": source_matches,
        "matchesSinkScope": sink_matches,
        "missingCriteria": missing,
        "isSatisfied": not missing,
    }


def _operational_component_index(graph_state: OperationalGraphState) -> dict[str, str]:
    adjacency: dict[str, set[str]] = {}

    def _connect(left: str, right: str) -> None:
        if not left or not right:
            return
        adjacency.setdefault(left, set()).add(right)
        adjacency.setdefault(right, set()).add(left)

    for leg in graph_state.flow_legs:
        _connect(leg.supplier_node_id, leg.customer_node_id)
    for step in graph_state.process_steps:
        if step.process_kind == "extraction":
            for node_id in step.input_node_ids:
                _connect(step.host_node_id, node_id)
        else:
            adjacency.setdefault(step.host_node_id, set())

    component_by_node: dict[str, str] = {}
    counter = 0
    for seed in sorted(adjacency):
        if seed in component_by_node:
            continue
        counter += 1
        component_id = f"component-{counter}"
        queue = deque([seed])
        component_by_node[seed] = component_id
        while queue:
            current = queue.popleft()
            for neighbor in adjacency.get(current, ()):
                if neighbor in component_by_node:
                    continue
                component_by_node[neighbor] = component_id
                queue.append(neighbor)
    return component_by_node


def _product_identity_key(node_index: dict[str, EntityNode], product_id: str) -> str:
    node = node_index.get(product_id)
    if node is not None and str(node.canonical_ref or "").strip():
        return f"canonical:{node.canonical_ref.strip().lower()}"
    label = _graph_node_label(node_index, product_id)
    tokens = _scope_tokens(label)
    return f"label:{' '.join(tokens) or _slug(label or product_id)}"


def _label_looks_like_placeholder(label: str) -> bool:
    text = label or ""
    for pat in _PLACEHOLDER_LABEL_PATTERNS:
        if pat.search(text):
            return True
    return bool(_PLACEHOLDER_HYPOTHESIS_LABEL_HINTS.search(text))


def _canonical_entity_key(node_id: str, entity_type: str, label: str) -> str:
    """Hypothesis-local identity key: type slug + label slug, or id:<slug(id)> when id is explicit and not the synthetic slug id."""
    nid = (node_id or "").strip()
    synthetic = _synthetic_id_for_endpoint(entity_type, label)
    if nid and nid != synthetic and not _label_looks_like_placeholder(label):
        return f"id:{_slug(nid)}"
    return synthetic


def _load_hypothesis_entity_registry(state: dict[str, Any]) -> dict[str, Any]:
    raw = state.get(STATE_HYPOTHESIS_ENTITY_REGISTRY)
    if isinstance(raw, dict):
        return raw
    return {"byBranch": {}, "lineageEvents": []}


def _save_hypothesis_entity_registry(state: dict[str, Any], registry: dict[str, Any]) -> None:
    state[STATE_HYPOTHESIS_ENTITY_REGISTRY] = registry


def _registry_rebuild_classes(branch: dict[str, Any]) -> None:
    id_map: dict[str, str] = branch.get("idToCanonical") or {}
    classes: dict[str, list[str]] = {}
    for nid, canon in id_map.items():
        classes.setdefault(canon, []).append(nid)
    for key in classes:
        classes[key] = sorted(set(classes[key]))
    branch["classes"] = classes


def _registry_merge_equivalence(reg: dict[str, Any], branch_id: str, canon: str, touched_ids: set[str]) -> None:
    """Point every id that shares an old class with touched_ids at canon (hypothesis-local merge)."""
    by_branch = reg.setdefault("byBranch", {})
    branch = by_branch.setdefault(branch_id, {"idToCanonical": {}, "classes": {}})
    id_map: dict[str, str] = branch["idToCanonical"]
    keys: set[str] = {canon}
    for tid in touched_ids:
        if tid in id_map:
            keys.add(id_map[tid])
    merged_ids: set[str] = set(touched_ids)
    for key in keys:
        for nid, mapped in id_map.items():
            if mapped == key:
                merged_ids.add(nid)
    for nid in merged_ids:
        id_map[nid] = canon
    _registry_rebuild_classes(branch)


def _all_endpoint_ids_from_observations(observations: list[GraphObservation]) -> set[str]:
    out: set[str] = set()
    for raw in observations:
        o = _observation_ids(raw) if isinstance(raw, GraphObservation) else _observation_ids(GraphObservation.model_validate(raw))
        for nid, typ, lab in ((o.subject_id, o.subject_type, o.subject), (o.object_id, o.object_type, o.object)):
            out |= _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
    out.discard("")
    return out


def _fragment_endpoint_records(fragment: GraphFragment) -> list[tuple[str, str, str]]:
    node_lookup: dict[str, tuple[str, str]] = {}
    for node in fragment.entity_nodes:
        node_lookup[node.node_id] = (node.entity_type, node.label)

    records: list[tuple[str, str, str]] = []
    if fragment.observations:
        for raw in fragment.observations:
            o = _observation_ids(raw) if isinstance(raw, GraphObservation) else _observation_ids(GraphObservation.model_validate(raw))
            records.append((o.subject_id, o.subject_type, o.subject))
            records.append((o.object_id, o.object_type, o.object))
        return records

    for raw in fragment.overlay_edges:
        o = _observation_ids(raw) if isinstance(raw, GraphObservation) else _observation_ids(GraphObservation.model_validate(raw))
        records.append((o.subject_id, o.subject_type, o.subject))
        records.append((o.object_id, o.object_type, o.object))
    for step in fragment.process_steps:
        host_type, host_label = node_lookup.get(step.host_node_id, ("", step.host_node_id))
        records.append((step.host_node_id, host_type, host_label))
        for node_id in step.input_node_ids:
            typ, lab = node_lookup.get(node_id, ("", node_id))
            records.append((node_id, typ, lab))
        for node_id in step.output_node_ids:
            typ, lab = node_lookup.get(node_id, ("", node_id))
            records.append((node_id, typ, lab))
    for leg in fragment.flow_legs:
        supplier_type, supplier_label = node_lookup.get(leg.supplier_node_id, ("", leg.supplier_node_id))
        customer_type, customer_label = node_lookup.get(leg.customer_node_id, ("", leg.customer_node_id))
        product_type, product_label = node_lookup.get(leg.product_node_id, ("", leg.product_node_id))
        records.append((leg.supplier_node_id, supplier_type, supplier_label))
        records.append((leg.product_node_id, product_type, product_label))
        records.append((leg.customer_node_id, customer_type, customer_label))
        for node_id in leg.route_node_ids:
            typ, lab = node_lookup.get(node_id, ("", node_id))
            records.append((node_id, typ, lab))
    for node in fragment.entity_nodes:
        records.append((node.node_id, node.entity_type, node.label))
    return records


def _all_endpoint_ids_from_fragment(fragment: GraphFragment) -> set[str]:
    out: set[str] = set()
    for nid, typ, lab in _fragment_endpoint_records(fragment):
        out |= _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
    out.discard("")
    return out


def _replacement_anchor_canonical_key(observations: list[GraphObservation], removal_zone: set[str]) -> str:
    """Pick the canonical key of the first replacement-side endpoint (reverse scan: usually the new sink)."""
    endpoints: list[tuple[str, str, str]] = []
    for raw in observations:
        o = _observation_ids(raw) if isinstance(raw, GraphObservation) else _observation_ids(GraphObservation.model_validate(raw))
        endpoints.append((o.subject_id, o.subject_type, o.subject))
        endpoints.append((o.object_id, o.object_type, o.object))
    for nid, typ, lab in reversed(endpoints):
        pair = _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
        if not pair:
            continue
        if not (pair & removal_zone):
            return _canonical_entity_key(str(nid or ""), typ, lab)
    nid, typ, lab = endpoints[-1]
    return _canonical_entity_key(str(nid or ""), typ, lab)


def _replacement_anchor_canonical_key_from_fragment(fragment: GraphFragment, removal_zone: set[str]) -> str:
    endpoints = _fragment_endpoint_records(fragment)
    for nid, typ, lab in reversed(endpoints):
        pair = _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
        if not pair:
            continue
        if not (pair & removal_zone):
            return _canonical_entity_key(str(nid or ""), typ, lab)
    if not endpoints:
        return "entity:replacement"
    nid, typ, lab = endpoints[-1]
    return _canonical_entity_key(str(nid or ""), typ, lab)


def _replacement_anchor_identity_ids(observations: list[GraphObservation], removal_zone: set[str]) -> set[str]:
    """Graph identity tokens for the replacement anchor endpoint only (not upstream neighbors on the same edge)."""
    endpoints: list[tuple[str, str, str]] = []
    for raw in observations:
        o = _observation_ids(raw) if isinstance(raw, GraphObservation) else _observation_ids(GraphObservation.model_validate(raw))
        endpoints.append((o.subject_id, o.subject_type, o.subject))
        endpoints.append((o.object_id, o.object_type, o.object))
    for nid, typ, lab in reversed(endpoints):
        pair = _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
        if not pair:
            continue
        if not (pair & removal_zone):
            return set(pair)
    nid, typ, lab = endpoints[-1]
    return _graph_endpoint_identity_pair(str(nid or ""), typ, lab)


def _replacement_anchor_identity_ids_from_fragment(fragment: GraphFragment, removal_zone: set[str]) -> set[str]:
    endpoints = _fragment_endpoint_records(fragment)
    for nid, typ, lab in reversed(endpoints):
        pair = _graph_endpoint_identity_pair(str(nid or ""), typ, lab)
        if not pair:
            continue
        if not (pair & removal_zone):
            return set(pair)
    if not endpoints:
        return set()
    nid, typ, lab = endpoints[-1]
    return _graph_endpoint_identity_pair(str(nid or ""), typ, lab)


def _registry_id_matches_anchor_kind(node_id: str, anchor_canon: str) -> bool:
    """Keep removal-zone ids that plausibly refer to the same endpoint *kind* as the anchor (avoid merging PRODUCT into FACILITY)."""
    nid = (node_id or "").strip()
    if not nid:
        return False
    if anchor_canon.startswith("id:"):
        return True
    if ":" not in anchor_canon:
        return True
    prefix = anchor_canon.split(":", 1)[0]
    if nid.startswith(f"{prefix}:"):
        return True
    if nid.startswith("id:"):
        return True
    return False


def _registry_record_patch(
    state: dict[str, Any],
    *,
    branch_id: str,
    fragment_id: str,
    patch_mode: str,
    removal_zone: set[str],
    incoming_observations: list[GraphObservation],
    incoming_fragment: GraphFragment | None = None,
    explicit_seeds: set[str],
) -> None:
    reg = _load_hypothesis_entity_registry(state)
    from_ids = set(removal_zone) | set(explicit_seeds)
    if incoming_observations:
        to_ids = _all_endpoint_ids_from_observations(incoming_observations)
        canon = _replacement_anchor_canonical_key(incoming_observations, removal_zone)
        anchor_ids = _replacement_anchor_identity_ids(incoming_observations, removal_zone)
    elif incoming_fragment is not None:
        to_ids = _all_endpoint_ids_from_fragment(incoming_fragment)
        canon = _replacement_anchor_canonical_key_from_fragment(incoming_fragment, removal_zone)
        anchor_ids = _replacement_anchor_identity_ids_from_fragment(incoming_fragment, removal_zone)
    else:
        to_ids = set()
        canon = "entity:replacement"
        anchor_ids = set()
    old_for_merge = set(explicit_seeds) | {n for n in removal_zone if _registry_id_matches_anchor_kind(n, canon)}
    touched = old_for_merge | anchor_ids
    _registry_merge_equivalence(reg, branch_id, canon, touched)
    events = reg.setdefault("lineageEvents", [])
    cycle = int(state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)
    events.append(
        {
            "branchId": branch_id,
            "fragmentId": fragment_id,
            "patchMode": patch_mode,
            "canonicalEntityKey": canon,
            "fromIds": sorted(from_ids)[:64],
            "toIds": sorted(to_ids)[:64],
            "cycle": cycle,
        }
    )
    events[:] = events[-64:]
    _save_hypothesis_entity_registry(state, reg)


def _entity_registry_summary_for_branch(state: dict[str, Any], branch_id: str) -> dict[str, Any]:
    reg = _load_hypothesis_entity_registry(state)
    branch = (reg.get("byBranch") or {}).get(branch_id) or {}
    classes: dict[str, Any] = branch.get("classes") or {}
    ranked = sorted(classes.items(), key=lambda kv: (-len(kv[1]), str(kv[0])))[:8]
    top_classes = [{"canonicalEntityKey": k, "memberCount": len(v), "memberSample": v[:6]} for k, v in ranked]
    lineage = [e for e in (reg.get("lineageEvents") or []) if isinstance(e, dict) and e.get("branchId") == branch_id][-8:]
    return {"branchId": branch_id, "topEquivalenceClasses": top_classes, "recentLineage": lineage}


def _entity_registry_summary_global(state: dict[str, Any], *, class_limit: int = 6, event_limit: int = 10) -> dict[str, Any]:
    reg = _load_hypothesis_entity_registry(state)
    flat: list[tuple[str, str, int]] = []
    for bid, branch in (reg.get("byBranch") or {}).items():
        for canon, members in (branch.get("classes") or {}).items():
            flat.append((str(bid), str(canon), len(members)))
    flat.sort(key=lambda t: (-t[2], t[0], t[1]))
    events = [e for e in (reg.get("lineageEvents") or []) if isinstance(e, dict)][-event_limit:]
    return {
        "topEquivalenceClasses": [
            {"branchId": bid, "canonicalEntityKey": canon, "memberCount": n} for bid, canon, n in flat[:class_limit]
        ],
        "recentLineage": events,
    }


def _canonical_for_reachability_step(
    state: dict[str, Any],
    branch_id: str,
    node_id: str,
    entity_type: str,
    label: str,
) -> str:
    reg = _load_hypothesis_entity_registry(state)
    branch = (reg.get("byBranch") or {}).get(branch_id) or {}
    id_map = branch.get("idToCanonical") or {}
    nid = (node_id or "").strip()
    if nid and nid in id_map:
        return str(id_map[nid])
    return _canonical_entity_key(nid, entity_type, label)


def _route_fingerprint_tuple(
    state: dict[str, Any],
    path_steps: list[dict[str, Any]],
) -> tuple[tuple[str, str, str], ...]:
    out: list[tuple[str, str, str]] = []
    for step in path_steps:
        br = str(step.get("branchId") or "")
        cs = _canonical_for_reachability_step(
            state,
            br,
            str(step.get("subjectId") or ""),
            str(step.get("subjectType") or ""),
            str(step.get("from") or ""),
        )
        co = _canonical_for_reachability_step(
            state,
            br,
            str(step.get("objectId") or ""),
            str(step.get("objectType") or ""),
            str(step.get("to") or ""),
        )
        out.append((cs, str(step.get("rel") or ""), co))
    return tuple(out)


def _detect_slot_endpoint_conflicts(graph: list[GraphObservation]) -> list[str]:
    """slotId marks the object endpoint; same slotId with different canonical sinks and no alternativeGroupId => gap."""
    buckets: dict[tuple[str, str], dict[str, list[str]]] = {}
    for raw in graph:
        try:
            o = _observation_ids(GraphObservation.model_validate(raw) if not isinstance(raw, GraphObservation) else raw)
        except Exception:
            continue
        sid = (o.slot_id or "").strip()
        if not sid:
            continue
        alt = (o.alternative_group_id or "").strip()
        canon = _canonical_entity_key(o.object_id, o.object_type, o.object)
        key = (sid, alt)
        buckets.setdefault(key, {}).setdefault(canon, []).append(o.object)
    gaps: list[str] = []
    for (slot_id, alt), canon_map in sorted(buckets.items()):
        if alt:
            continue
        distinct = [c for c, labs in canon_map.items() if labs]
        if len(distinct) <= 1:
            continue
        sample = ", ".join(f"{c}→{canon_map[c][0][:48]}" for c in distinct[:4])
        gaps.append(
            f"Slot contract conflict for slotId={slot_id!r}: multiple canonical endpoints without alternativeGroupId separation ({sample})."
        )
    return gaps


_ROUTE_FAMILY_LIMIT = 6


def _normalize_fragment(
    fragment: GraphFragment,
    action: ToolAction,
    allowed_edges: set[str],
    default_support_type: str,
) -> GraphFragment:
    entity_nodes: list[EntityNode] = []
    for idx, raw in enumerate(fragment.entity_nodes[:32], start=1):
        node_id = (raw.node_id or "").strip() or _synthetic_id_for_endpoint(raw.entity_type, raw.label)
        entity_nodes.append(
            raw.model_copy(
                update={
                    "node_id": node_id or f"{action.branch_id}-node-{idx}",
                    "branch_id": raw.branch_id or action.branch_id,
                    "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                    "support_type": raw.support_type or default_support_type,
                }
            )
        )

    flow_legs: list[FlowLeg] = []
    for idx, raw in enumerate(fragment.flow_legs[:24], start=1):
        flow_legs.append(
            raw.model_copy(
                update={
                    "leg_id": (raw.leg_id or "").strip() or f"{action.branch_id}-leg-{idx}",
                    "branch_id": raw.branch_id or action.branch_id,
                    "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                    "support_type": raw.support_type or default_support_type,
                }
            )
        )

    process_steps: list[ProcessStep] = []
    for idx, raw in enumerate(fragment.process_steps[:24], start=1):
        process_steps.append(
            raw.model_copy(
                update={
                    "step_id": (raw.step_id or "").strip() or f"{action.branch_id}-step-{idx}",
                    "branch_id": raw.branch_id or action.branch_id,
                    "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                    "support_type": raw.support_type or default_support_type,
                }
            )
        )

    overlay_edges: list[GraphObservation] = []
    for raw in fragment.overlay_edges:
        if allowed_edges and raw.relation not in allowed_edges:
            continue
        overlay_edges.append(
            _observation_ids(raw).model_copy(
                update={
                    "branch_id": raw.branch_id or action.branch_id,
                    "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                    "support_type": raw.support_type or default_support_type,
                }
            )
        )

    observations: list[GraphObservation] = []
    for raw in fragment.observations:
        if allowed_edges and raw.relation not in allowed_edges:
            continue
        observations.append(
            _observation_ids(raw).model_copy(
                update={
                    "branch_id": raw.branch_id or action.branch_id,
                    "alternative_group_id": raw.alternative_group_id or fragment.alternative_group_id,
                    "support_type": raw.support_type or default_support_type,
                }
            )
        )

    native_operational_primitives = bool(flow_legs or process_steps)
    normalized = fragment.model_copy(
        update={
            "branch_id": fragment.branch_id or action.branch_id,
            "human_question": fragment.human_question or action.human_question,
            "human_summary": fragment.human_summary or fragment.title,
            "alternative_group_id": fragment.alternative_group_id or "",
            "entity_nodes": entity_nodes,
            "flow_legs": flow_legs,
            "process_steps": process_steps,
            "overlay_edges": overlay_edges,
            "observations": [] if native_operational_primitives else observations,
            "quality_score": max(0.0, min(1.0, fragment.quality_score)),
        }
    )
    return GraphFragment.model_validate(normalized.model_dump(mode="json", by_alias=True))


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


def _logic_trace_extras(
    ctx: InvocationContext,
    *,
    supervisor_raw_next: str,
    policy_parts: list[PolicyCorrection],
    decision: SupervisorDecision,
) -> tuple[dict[str, Any] | None, dict[str, Any]]:
    hyp_snap = ctx.session.state.pop(STATE_LAST_HYPOTHESIS_RUNTIME_TELEMETRY, None)
    lt_hypothesis: dict[str, Any] | None = None
    if isinstance(hyp_snap, dict) and hyp_snap.get("applied"):
        lt_hypothesis = {
            "applied": True,
            "decisionNote": hyp_snap.get("decisionNote", ""),
            "branchId": hyp_snap.get("branchId", ""),
        }
    lt_delegate = {
        "supervisorNextAgentRaw": supervisor_raw_next,
        "delegatePolicyDetails": [p.detail for p in policy_parts],
        "nextAgent": decision.next_agent,
        "runtimeRewroteNextAgent": supervisor_raw_next != decision.next_agent,
    }
    return lt_hypothesis, lt_delegate


def _append_evidence_ledger(
    state: dict[str, Any], action: ToolAction, result: ToolActionResult
) -> EvidenceLedgerEntry | None:
    if result.status not in {"success", "partial"}:
        return None
    if action.tool_name not in {"retrieve", "reason", "web_search", "supply_qa"}:
        return None
    ledger_raw = state.get(STATE_EVIDENCE_LEDGER)
    ledger: list[dict[str, Any]] = list(ledger_raw) if isinstance(ledger_raw, list) else []
    entry_id = f"ev_{uuid.uuid4().hex[:14]}"
    entry = EvidenceLedgerEntry(
        id=entry_id,
        tool=action.tool_name,
        branchId=action.branch_id,
        summary=(result.human_summary or "")[:480],
        sourceDocuments=list(result.source_documents)[:6],
    )
    ledger.append(entry.model_dump(mode="json", by_alias=True))
    state[STATE_EVIDENCE_LEDGER] = ledger[-240:]
    return entry


def _branches_by_id(state: dict[str, Any]) -> dict[str, BranchState]:
    out: dict[str, BranchState] = {}
    for raw in state.get(STATE_BRANCH_STATES) or []:
        try:
            b = BranchState.model_validate(raw)
        except Exception:
            continue
        out[b.branch_id] = b
    return out


def _branch_ancestor_ids(state: dict[str, Any], branch_id: str) -> list[str]:
    branches = _branches_by_id(state)
    chain: list[str] = [branch_id]
    seen: set[str] = {branch_id}
    bid = branch_id
    for _ in range(32):
        b = branches.get(bid)
        if b is None:
            break
        pid = (b.parent_hypothesis_id or "").strip()
        if not pid or pid in seen:
            break
        seen.add(pid)
        chain.append(pid)
        bid = pid
    return chain


def _eligible_evidence_ledger_entry_ids(state: dict[str, Any], branch_id: str) -> set[str]:
    """Evidence ids from session ledger + this branch's append-only ledger and ancestor branches."""
    ids: set[str] = set()
    chain = _branch_ancestor_ids(state, branch_id)
    chain_set = set(chain)
    for raw in state.get(STATE_EVIDENCE_LEDGER) or []:
        if not isinstance(raw, dict):
            continue
        eid = str(raw.get("id") or "").strip()
        bid = str(raw.get("branchId") or "").strip()
        if eid and bid in chain_set:
            ids.add(eid)
    branches = _branches_by_id(state)
    for bid in chain:
        br = branches.get(bid)
        if br is None:
            continue
        for row in br.hypothesis_evidence_ledger:
            if row.id:
                ids.add(row.id)
        for row in br.parent_ledger_snapshot:
            if row.id:
                ids.add(row.id)
    return ids


_LEDGER_COMPACT_SUMMARY_CHARS = 96
_LEDGER_COMPACT_SESSION_TAIL = 20
_LEDGER_COMPACT_BRANCH_TAIL = 6
_LEDGER_COMPACT_PARENT_SNAPSHOT_TAIL = 4
_LEDGER_COMPACT_PLANNER_BY_BRANCH = 8


def _compact_evidence_ledger_row(entry: EvidenceLedgerEntry | dict[str, Any]) -> dict[str, Any]:
    """Algorithmic compaction for prompts: stable ids + short preview (full rows stay in session state)."""
    if isinstance(entry, dict):
        sid = str(entry.get("id") or "").strip()
        docs = entry.get("sourceDocuments") or entry.get("source_documents") or []
        doc_n = len(docs) if isinstance(docs, list) else 0
        summ = str(entry.get("summary") or "")
        return {
            "id": sid,
            "tool": str(entry.get("tool") or "").strip(),
            "branchId": str(entry.get("branchId") or entry.get("branch_id") or "").strip(),
            "summaryPreview": _truncate_text(summ, _LEDGER_COMPACT_SUMMARY_CHARS),
            "sourceDocumentCount": doc_n,
        }
    return {
        "id": entry.id,
        "tool": entry.tool,
        "branchId": entry.branch_id,
        "summaryPreview": _truncate_text(entry.summary or "", _LEDGER_COMPACT_SUMMARY_CHARS),
        "sourceDocumentCount": len(entry.source_documents),
    }


def _build_evidence_ledger_compact(state: dict[str, Any]) -> dict[str, Any]:
    """Cross-branch ledger digest derived from full session + branch ledgers (no manual curation)."""
    ledger_raw = state.get(STATE_EVIDENCE_LEDGER)
    session_list = list(ledger_raw) if isinstance(ledger_raw, list) else []
    session_tail_src = [x for x in session_list[-_LEDGER_COMPACT_SESSION_TAIL:] if isinstance(x, dict)]
    session_tail = [_compact_evidence_ledger_row(e) for e in session_tail_src]
    branches: list[BranchState] = []
    for raw in state.get(STATE_BRANCH_STATES) or []:
        try:
            branches.append(BranchState.model_validate(raw))
        except Exception:
            continue
    by_branch: list[dict[str, Any]] = []
    for b in branches[-12:]:
        eligible = _eligible_evidence_ledger_entry_ids(state, b.branch_id)
        by_branch.append(
            {
                "branchId": b.branch_id,
                "parentHypothesisId": b.parent_hypothesis_id,
                "hypothesisRelation": b.hypothesis_relation,
                "ledgerDigest": b.ledger_digest,
                "branchLedgerTotal": len(b.hypothesis_evidence_ledger),
                "branchLedgerTail": [
                    _compact_evidence_ledger_row(e) for e in b.hypothesis_evidence_ledger[-_LEDGER_COMPACT_BRANCH_TAIL:]
                ],
                "parentSnapshotTotal": len(b.parent_ledger_snapshot),
                "parentSnapshotTail": [
                    _compact_evidence_ledger_row(e)
                    for e in b.parent_ledger_snapshot[-_LEDGER_COMPACT_PARENT_SNAPSHOT_TAIL:]
                ],
                "eligibleEvidenceIdCount": len(eligible),
            }
        )
    hints_raw = state.get(STATE_HYPOTHESIS_STEER_HINT)
    steer_tail = list(hints_raw[-4:]) if isinstance(hints_raw, list) else []
    return {
        "sessionEntryTotal": len(session_list),
        "sessionTail": session_tail,
        "byBranch": by_branch,
        "steerHintsTail": steer_tail,
    }


def _branch_state_for_shared_memory(branch: BranchState) -> dict[str, Any]:
    """Branch JSON for prompts: ledger arrays are tails + totals (full lists remain in state)."""
    d = branch.model_dump(mode="json", by_alias=True)
    d["hypothesisEvidenceLedger"] = [
        _compact_evidence_ledger_row(e) for e in branch.hypothesis_evidence_ledger[-_LEDGER_COMPACT_BRANCH_TAIL:]
    ]
    d["hypothesisEvidenceLedgerTotal"] = len(branch.hypothesis_evidence_ledger)
    d["parentLedgerSnapshot"] = [
        _compact_evidence_ledger_row(e) for e in branch.parent_ledger_snapshot[-_LEDGER_COMPACT_PARENT_SNAPSHOT_TAIL:]
    ]
    d["parentLedgerSnapshotTotal"] = len(branch.parent_ledger_snapshot)
    return d


def _is_supply_chain_action(action: ToolAction) -> bool:
    return ROLE_SUPPLY_CHAIN in action.assigned_roles


def _fragment_has_supply_chain_operational_progress(fragment: GraphFragment) -> bool:
    if fragment.flow_legs or fragment.process_steps:
        return True
    if fragment.patch_mode in {"replace_entity", "replace_fragment"} and (
        fragment.entity_nodes or fragment.flow_legs or fragment.process_steps
    ):
        return True
    return False


def _result_has_supply_chain_operational_progress(action: ToolAction, result: ToolActionResult) -> bool:
    if not _is_supply_chain_action(action):
        return True
    if result.path_assessment == "rejected":
        return True
    return any(_fragment_has_supply_chain_operational_progress(fragment) for fragment in result.graph_fragments)


def _normalize_supply_chain_result(action: ToolAction, result: ToolActionResult) -> ToolActionResult:
    if action.tool_name == "supply_qa":
        return result
    if not _is_supply_chain_action(action):
        return result
    if result.path_assessment == "rejected":
        return result
    if _result_has_supply_chain_operational_progress(action, result):
        return result
    note = (
        "This turn surfaced contextual or commercial clues but did not add flow legs, process steps, or a replacement patch, "
        "so the operational chain remains open."
    )
    remaining_frontier = _dedupe_preserve_order(
        list(result.remaining_frontier)
        + list(result.resolved_frontier)
        + list(result.uncovered_questions)
        + list(action.frontier)
        + [action.closure_target]
    )[:8]
    discovered_frontier = _dedupe_preserve_order(
        list(result.discovered_frontier)
        + list(result.resolved_frontier)
        + list(result.uncovered_questions)
    )[:8]
    human_summary = ((result.human_summary.rstrip(".") + ". ") if result.human_summary else "") + note
    return result.model_copy(
        update={
            "path_assessment": "still_open",
            "path_assessment_note": note,
            "closure_status": "unchanged" if result.closure_status in {"advanced", "closed"} else result.closure_status,
            "human_summary": human_summary,
            "resolved_frontier": [],
            "remaining_frontier": remaining_frontier,
            "discovered_frontier": discovered_frontier,
        }
    )


def _needs_supply_chain_structural_retry(action: ToolAction, result: ToolActionResult) -> bool:
    if not _is_supply_chain_action(action):
        return False
    if result.status in {"blocked", "failed"}:
        return False
    if result.path_assessment == "rejected":
        return False
    return not _result_has_supply_chain_operational_progress(action, result)


def _supply_chain_structural_retry_suffix(
    action: ToolAction,
    result: ToolActionResult,
    shared_memory: dict[str, Any],
) -> str:
    placeholder_hints = [
        str(item).strip()
        for item in (shared_memory.get("placeholderResolutionHints") or [])[:2]
        if str(item).strip()
    ]
    frontier = [str(item).strip() for item in list(action.frontier)[:4] if str(item).strip()]
    discovered = [str(item).strip() for item in list(result.discovered_frontier)[:3] if str(item).strip()]
    return (
        "\n\nStructural repair required. Your previous answer did not add a usable operational supply-chain payload.\n"
        "Retry and return ONLY valid ToolActionResult JSON.\n"
        "Requirements for this retry:\n"
        "- Return 1-2 graphFragments with at least one real `flowLeg`, `processStep`, or a `replace_entity` / `replace_fragment` patch that resolves a current graph slot.\n"
        "- Do not return decorative fragments with only high-level context or entityNodes unless they are part of a replacement patch with `oldId` / `oldIds`.\n"
        "- If the corridor is indirect, encode the best-supported named intermediary hosts, named route-family handoff, and concrete product stages, even when confidence is low and supportType is hypothesis/model_knowledge.\n"
        "- If the action is broad or overview, the payload must be a connected operational chain fragment, not disconnected facts.\n"
        "- If you still cannot support a concrete operational primitive from this source, return `status=blocked` or `partial` with `graphFragments=[]` and explicit unresolved endpoints/operators/products in `uncoveredQuestions`.\n"
        + (f"- Current closure target: {action.closure_target}\n" if action.closure_target else "")
        + (f"- Frontier to close first: {' | '.join(frontier)}\n" if frontier else "")
        + (f"- Newly exposed gaps from the prior answer: {' | '.join(discovered)}\n" if discovered else "")
        + (
            "- Placeholder repair hints: " + " | ".join(placeholder_hints) + "\n"
            if placeholder_hints else ""
        )
    )


def _flatten_fragments(fragments: list[GraphFragment]) -> list[GraphObservation]:
    flattened: list[GraphObservation] = []
    for fragment in fragments:
        flattened.extend(observation_projection(fragment_operational_graph(fragment)))
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


def _genai_server_error_retry_max() -> int:
    raw = (os.getenv("AGENTLOOM_GENAI_SERVER_ERROR_RETRIES", "4") or "4").strip()
    try:
        n = int(raw)
    except ValueError:
        n = 4
    return max(1, min(n, 12))


def _is_genai_transient_server_error(exc: BaseException) -> bool:
    """True for Vertex/Gemini 5xx from google.genai (opaque INTERNAL, etc.)."""
    try:
        from google.genai import errors as genai_errors

        if isinstance(exc, genai_errors.ServerError):
            return True
        if isinstance(exc, genai_errors.APIError):
            code = int(getattr(exc, "code", 0) or 0)
            return 500 <= code < 600
    except Exception:
        pass
    return False


def _vertex_generate_content_resilient(
    client: Any,
    *,
    model: str,
    contents: str,
    config: Any,
    allow_quota_model_fallback: bool,
    used_resource_fallback_holder: list[bool],
) -> Any:
    """Retry generate_content on transient 5xx; optional one-step quota model fallback (429-style)."""
    max_srv = _genai_server_error_retry_max()
    srv_rix = 0
    cur_model = model
    while True:
        try:
            return client.models.generate_content(model=cur_model, contents=contents, config=config)
        except Exception as exc:
            if _is_genai_transient_server_error(exc) and srv_rix < max_srv - 1:
                srv_rix += 1
                time.sleep(min(0.25 * (2 ** (srv_rix - 1)), 12.0))
                continue
            if allow_quota_model_fallback and _is_resource_exhausted_error(exc):
                srv_rix = 0
                used_fb = bool(used_resource_fallback_holder[0])
                nxt = _model_with_resource_fallback(cur_model, used_fallback=used_fb)
                if nxt != cur_model:
                    used_resource_fallback_holder[0] = True
                    cur_model = nxt
                    continue
            raise


def _fragment_has_graph_payload(fragment: GraphFragment) -> bool:
    return any(
        (
            fragment.entity_nodes,
            fragment.flow_legs,
            fragment.process_steps,
            fragment.overlay_edges,
            fragment.observations,
        )
    )


def _enrich_tool_action_result_wire_dict(data: dict[str, Any], action: ToolAction) -> None:
    """Fill tool/fragment identity omitted in model output; strict Pydantic still applies after this."""
    if not data.get("actionId"):
        data["actionId"] = action.action_id
    if not data.get("branchId"):
        data["branchId"] = action.branch_id
    if not data.get("toolName"):
        data["toolName"] = action.tool_name
    gfs = data.get("graphFragments")
    if not isinstance(gfs, list):
        return
    for i, item in enumerate(gfs):
        if not isinstance(item, dict):
            continue
        if not item.get("fragmentId"):
            item["fragmentId"] = f"{action.branch_id}-f{i + 1}"


def _parse_tool_result_payload(
    text: str,
    action: ToolAction,
    allowed_edges: set[str],
    default_support_type: str,
) -> ToolActionResult:
    try:
        data = parse_wire_json(text)
    except (json.JSONDecodeError, ValueError) as exc:
        raise ValueError(f"ToolActionResult: invalid JSON (graph tool): {exc}") from exc
    if not isinstance(data, dict):
        raise ValueError(f"ToolActionResult: expected JSON object, got {type(data).__name__} (graph tool)")
    _enrich_tool_action_result_wire_dict(data, action)
    assert_wire_schema_version(data, model_name="ToolActionResult")
    base = ToolActionResult.model_validate(data)
    incoming_fragment_count = len(base.graph_fragments)
    normalized_fragments: list[GraphFragment] = []
    for frag in list(base.graph_fragments)[:4]:
        normalized_fragments.append(_normalize_fragment(frag, action, allowed_edges, default_support_type))
    normalized_fragments = [f for f in normalized_fragments if _fragment_has_graph_payload(f)]
    result = base.model_copy(update={"graph_fragments": normalized_fragments})
    if incoming_fragment_count and not normalized_fragments and result.status == "success":
        msg = "Tool returned graphFragments but none contained typed graph payload."
        result = result.model_copy(
            update={
                "status": "partial",
                "closure_status": "unchanged" if result.closure_status == "closed" else result.closure_status,
                "human_summary": ((result.human_summary.rstrip(".") + ". ") if result.human_summary else "") + msg,
                "remaining_frontier": list(result.remaining_frontier) or list(action.frontier) or [action.closure_target],
                "failure": result.failure
                or FailureRecord(
                    code="agent_failed",
                    message=msg,
                    blocking=False,
                    missing_inputs=["typed_graph_fragment"],
                ),
            }
        )
    raw_findings = [lf.model_dump(mode="json", by_alias=True) for lf in result.learned_findings]
    normalized_findings = _normalize_learned_findings_payload(raw_findings, action, result)
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
    if tool_name == "supply_qa":
        return bool(os.getenv("GOOGLE_CLOUD_PROJECT") or os.getenv("GCP_PROJECT_ID"))
    if tool_name == "apply_supply_graph_patch":
        if isinstance(state.get(STATE_EVIDENCE_LEDGER), list) and len(state.get(STATE_EVIDENCE_LEDGER) or []) > 0:
            return True
        for raw in state.get(STATE_BRANCH_STATES) or []:
            try:
                b = BranchState.model_validate(raw)
            except Exception:
                continue
            if b.hypothesis_evidence_ledger or b.parent_ledger_snapshot:
                return True
        return False
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
    for fallback in ("retrieve", "web_search", "reason", "supply_qa"):
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
    current_graph = graph_state_from_fragments(result.graph_fragments)
    current_counts = graph_counts(current_graph)
    current_keys = set(graph_context_lines(current_graph, limit=64))
    previous_keys: set[str] = set()
    previous_sources: set[str] = set()
    previous_findings: set[str] = set()
    previous_open_questions: set[str] = set()
    previous_frontier: set[str] = set()
    if previous is not None:
        previous_keys = set(graph_context_lines(graph_state_from_fragments(previous.graph_fragments), limit=64))
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
        + (current_counts["flowLegCount"] * 0.02)
        + (current_counts["processStepCount"] * 0.02)
        + (new_source_count * 0.08)
        + (new_finding_count * 0.05)
        + (coverage_gain * 0.04)
        + (frontier_gain * 0.05)
    )
    return max(0.0, min(1.0, delta))


def _investigation_progress_snapshot(state: dict[str, Any]) -> dict[str, int]:
    graph_state = _load_operational_graph_state(state)
    projection = observation_projection(graph_state)
    counts = graph_counts(graph_state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=state)
    fragments = [GraphFragment.model_validate(item) for item in (state.get(STATE_GRAPH_FRAGMENTS) or [])]
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    unique_sources = {
        source
        for observation in projection
        for source in observation.provenance
        if source
    }
    return {
        "graphObservationCount": counts["graphObservationCount"],
        "entityNodeCount": counts["entityNodeCount"],
        "flowLegCount": counts["flowLegCount"],
        "processStepCount": counts["processStepCount"],
        "overlayEdgeCount": counts["overlayEdgeCount"],
        "documentObservationCount": counts["documentObservationCount"],
        "mixedObservationCount": counts["mixedObservationCount"],
        "groundedFlowLegCount": counts["groundedFlowLegCount"],
        "groundedProcessStepCount": counts["groundedProcessStepCount"],
        "fragmentCount": len(fragments),
        "completeFragmentCount": sum(1 for item in fragments if item.completeness == "complete"),
        "answeredQuestionCount": len(state.get(STATE_ANSWERED_QUESTIONS) or []),
        "openGapCount": _open_gap_count(state),
        "branchCount": len(branches),
        "unresolvedBranchCount": sum(1 for item in branches if item.status in {"needs_followup", "blocked"}),
        "uniqueSourceCount": len(unique_sources),
        "groundedCompleteChainCount": _int_or_len_report(reachability, "groundedCompleteChainCount", "groundedCompleteChains"),
        "structuralCompleteChainCount": _int_or_len_report(reachability, "structuralCompleteChainCount", "structurallyCompleteChains"),
        "typedBackboneChainCount": _int_or_len_report(reachability, "typedBackboneChainCount", "typedBackboneChains"),
        "maxOperationalRouteHops": int(reachability.get("maxOperationalRouteHops") or _max_route_hops_from_report(reachability)),
    }


def _progress_delta(previous: dict[str, int], current: dict[str, int]) -> dict[str, int]:
    return {
        "newGraphObservations": current["graphObservationCount"] - previous["graphObservationCount"],
        "newEntityNodes": current["entityNodeCount"] - previous["entityNodeCount"],
        "newFlowLegs": current["flowLegCount"] - previous["flowLegCount"],
        "newProcessSteps": current["processStepCount"] - previous["processStepCount"],
        "newOverlayEdges": current["overlayEdgeCount"] - previous["overlayEdgeCount"],
        "newDocumentObservations": current["documentObservationCount"] - previous["documentObservationCount"],
        "newMixedObservations": current["mixedObservationCount"] - previous["mixedObservationCount"],
        "newGroundedFlowLegs": current["groundedFlowLegCount"] - previous["groundedFlowLegCount"],
        "newGroundedProcessSteps": current["groundedProcessStepCount"] - previous["groundedProcessStepCount"],
        "newFragments": current["fragmentCount"] - previous["fragmentCount"],
        "newCompleteFragments": current["completeFragmentCount"] - previous["completeFragmentCount"],
        "newAnsweredQuestions": current["answeredQuestionCount"] - previous["answeredQuestionCount"],
        "gapReduction": previous["openGapCount"] - current["openGapCount"],
        "newSources": current["uniqueSourceCount"] - previous["uniqueSourceCount"],
        "newUnresolvedBranches": current["unresolvedBranchCount"] - previous["unresolvedBranchCount"],
        "newGroundedCompleteChains": current["groundedCompleteChainCount"] - previous["groundedCompleteChainCount"],
        "newStructuralCompleteChains": current["structuralCompleteChainCount"] - previous["structuralCompleteChainCount"],
        "newTypedBackboneChains": current["typedBackboneChainCount"] - previous.get("typedBackboneChainCount", 0),
        "maxOperationalRouteHopGain": current["maxOperationalRouteHops"] - previous["maxOperationalRouteHops"],
    }


def _material_progress(delta: dict[str, int]) -> bool:
    return any(
        (
            delta["newDocumentObservations"] >= 2,
            delta["newMixedObservations"] >= 2,
            delta["newGroundedFlowLegs"] >= 1,
            delta["newGroundedProcessSteps"] >= 1,
            delta["newSources"] >= 1,
            delta["newCompleteFragments"] >= 1,
            delta["newFlowLegs"] >= 2,
            delta["newProcessSteps"] >= 1,
            delta["newGraphObservations"] >= 6,
            delta["newAnsweredQuestions"] >= 2 and delta["gapReduction"] >= 1,
            delta["gapReduction"] >= 2,
            delta["newGroundedCompleteChains"] >= 1,
            delta["newStructuralCompleteChains"] >= 1,
            delta.get("newTypedBackboneChains", 0) >= 1,
            delta["maxOperationalRouteHopGain"] >= 2,
        )
    )


def _flow_reachable_from(seeds: Iterable[str], adj: dict[str, list[dict[str, Any]]]) -> set[str]:
    """Forward closure along flow edges from seed node labels (used to de-duplicate BFS sources)."""
    seen: set[str] = set()
    q = deque(s for s in seeds if s)
    while q:
        n = q.popleft()
        if n in seen:
            continue
        seen.add(n)
        for edge in adj.get(n, []):
            t = str(edge.get("to", "") or "")
            if t and t not in seen:
                q.append(t)
    return seen


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


def _int_or_len_report(report: dict[str, Any], count_key: str, list_key: str) -> int:
    raw = report.get(count_key)
    if raw is not None:
        try:
            return int(raw)
        except (TypeError, ValueError):
            pass
    return len(report.get(list_key) or [])


def _max_route_hops_from_report(report: dict[str, Any]) -> int:
    max_hops = 0
    for key in ("groundedCompleteChains", "structurallyCompleteChains", "partialChains", "typedBackboneChains"):
        for entry in report.get(key) or []:
            try:
                max_hops = max(max_hops, int(entry.get("hops") or 0))
            except Exception:
                continue
    return max_hops


def _business_chain_profile(
    *,
    record: dict[str, Any],
    host_id: str,
    product_id: str,
    sink_label: str,
    node_index: dict[str, EntityNode],
) -> dict[str, Any]:
    flow_count = 0
    transform_count = 0
    assembly_count = 0
    extraction_count = 0
    host_ids: set[str] = set()
    product_ids: set[str] = set()
    placeholder_labels: list[str] = []
    aggregate_labels: list[str] = []

    def _remember_placeholder(node_id: str) -> None:
        if not node_id:
            return
        label = _graph_node_label(node_index, node_id)
        if not label:
            return
        if _is_placeholder_node(node_id, label):
            placeholder_labels.append(label)

    def _remember_aggregate(node_id: str) -> None:
        if not node_id:
            return
        node = node_index.get(node_id)
        if node is None:
            return
        if _is_unresolved_aggregate_node(
            node.node_id,
            node.label,
            entity_type=node.entity_type,
            support_type=node.support_type,
        ):
            aggregate_labels.append(node.label or node.node_id)

    for op in record["ops"]:
        kind = str(op.get("kind") or "")
        if kind == "flow":
            flow_count += 1
            from_host = str(op.get("fromHostId") or "")
            to_host = str(op.get("toHostId") or "")
            prod = str(op.get("productId") or "")
            if from_host:
                host_ids.add(from_host)
                _remember_placeholder(from_host)
                _remember_aggregate(from_host)
            if to_host:
                host_ids.add(to_host)
                _remember_placeholder(to_host)
                _remember_aggregate(to_host)
            if prod:
                product_ids.add(prod)
        else:
            if kind == "transformation":
                transform_count += 1
            elif kind == "assembly":
                assembly_count += 1
            elif kind == "extraction":
                extraction_count += 1
            host = str(op.get("hostId") or "")
            if host:
                host_ids.add(host)
                _remember_placeholder(host)
                _remember_aggregate(host)
            for input_id in op.get("inputIds") or []:
                if input_id:
                    product_ids.add(str(input_id))
            output_id = str(op.get("outputId") or "")
            if output_id:
                product_ids.add(output_id)

    if host_id and host_id in node_index:
        host_ids.add(host_id)
        _remember_placeholder(host_id)
        _remember_aggregate(host_id)
    if product_id:
        product_ids.add(product_id)

    sink_type = node_index.get(host_id).entity_type if node_index.get(host_id) is not None else ""
    source_is_anchored = record["sourceType"] == "DEPOSIT" or extraction_count > 0
    transform_like_count = transform_count + assembly_count
    concrete_sink = sink_type in {"FACILITY", "ORGANIZATION"} and not _is_placeholder_node(host_id, sink_label)

    criteria = {
        "sourceAnchored": source_is_anchored,
        "hasMultipleFlows": flow_count >= 2,
        "hasTransformLikeStep": transform_like_count >= 1,
        "hasOperationalDepth": int(record["hops"]) >= 4,
        "hasMultipleHosts": len(host_ids) >= 3,
        "hasMultipleProductStages": len(product_ids) >= 2,
        "concreteSink": concrete_sink,
        "placeholderFree": not placeholder_labels,
        "aggregateIntermediaryFree": not aggregate_labels,
    }
    missing: list[str] = []
    if not criteria["sourceAnchored"]:
        missing.append("missing a deposit-anchored or extraction-anchored upstream source")
    if not criteria["hasMultipleFlows"]:
        missing.append("needs at least two material flow legs")
    if not criteria["hasTransformLikeStep"]:
        missing.append("needs a real transformation or assembly step on a host node")
    if not criteria["hasOperationalDepth"]:
        missing.append("needs a longer multi-stage operational ladder")
    if not criteria["hasMultipleHosts"]:
        missing.append("needs more distinct operating hosts across the chain")
    if not criteria["hasMultipleProductStages"]:
        missing.append("needs multiple product stages instead of one material state")
    if not criteria["concreteSink"]:
        missing.append("sink is not yet a concrete downstream facility or organization")
    if not criteria["placeholderFree"]:
        missing.append(f"placeholder nodes remain: {', '.join(_dedupe_preserve_order(placeholder_labels)[:3])}")
    if not criteria["aggregateIntermediaryFree"]:
        missing.append(
            f"aggregate intermediary nodes remain: {', '.join(_dedupe_preserve_order(aggregate_labels)[:3])}"
        )

    return {
        "flowLegCount": flow_count,
        "transformationCount": transform_count,
        "assemblyCount": assembly_count,
        "extractionCount": extraction_count,
        "distinctHostCount": len(host_ids),
        "distinctProductStageCount": len(product_ids),
        "aggregateIntermediaryCount": len(_dedupe_preserve_order(aggregate_labels)),
        "missingCriteria": missing,
        "criteria": criteria,
        "businessScore": sum(1 for value in criteria.values() if value),
        "isBusinessComplete": not missing,
    }


def _business_chain_break_reason(profile: dict[str, Any]) -> str:
    missing = [str(item).strip() for item in profile.get("missingCriteria") or [] if str(item).strip()]
    if not missing:
        return "Terminal route is not business-complete yet."
    return "Terminal route is not business-complete yet: " + "; ".join(missing[:3]) + "."


def _graph_reachability_report_for_graph(
    graph_state: OperationalGraphState,
    *,
    state: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Analyze end-to-end reachability using operational flow legs and process steps."""
    counts = graph_counts(graph_state)
    if not any((counts["entityNodeCount"], counts["flowLegCount"], counts["processStepCount"], counts["overlayEdgeCount"])):
        req_c = _operational_corridor_from_state(state) if state is not None else {}
        return {
            "groundedCompleteChains": [],
            "structurallyCompleteChains": [],
            "partialChains": [],
            "typedBackboneChains": [],
            "bridgeCandidates": [],
            "healthSummary": "Graph is empty.",
            "groundedCompleteChainCount": 0,
            "structuralCompleteChainCount": 0,
            "partialChainCount": 0,
            "typedBackboneChainCount": 0,
            "routeFamilies": [],
            "maxOperationalRouteHops": 0,
            "weakFlowComponentsCount": 0,
            "requestedCorridorIntentDebug": _requested_corridor_intent_debug(state),
            "requestedCorridor": req_c,
            "groundedRequestedCorridorChains": [],
            "structuralRequestedCorridorChains": [],
            "groundedRequestedCorridorChainCount": 0,
            "structuralRequestedCorridorChainCount": 0,
            "requestedCorridorSatisfied": (not req_c.get("active")) if isinstance(req_c, dict) else True,
        }

    _rch_list_cap = SUPERVISOR_REACHABILITY_LIST_CAP if SUPERVISOR_REACHABILITY_LIST_CAP > 0 else 9999

    node_index = _graph_node_map(graph_state)
    flow_legs = list(graph_state.flow_legs)
    process_steps = list(graph_state.process_steps)
    overlay_edges = list(graph_state.overlay_edges)
    request = _request_from_state(state)
    requested_corridor = (
        _operational_corridor_from_state(state) if state is not None else _requested_operational_corridor(request)
    )

    def _node_type(node_id: str) -> str:
        node = node_index.get(node_id)
        return node.entity_type if node is not None else ""

    def _node_text(node_id: str) -> str:
        return _graph_node_label(node_index, node_id)

    def _state_label(host_id: str, product_id: str) -> str:
        return f"{_node_text(host_id)} [{_node_text(product_id)}]"

    def _canonical(node_id: str, branch_id: str = "") -> str:
        node = node_index.get(node_id)
        if node is None:
            return _graph_synthetic_id("", node_id)
        if state is not None:
            return _canonical_for_reachability_step(
                state,
                branch_id,
                node.node_id,
                node.entity_type,
                node.label or node.canonical_ref or node.node_id,
            )
        return _canonical_entity_key(node.node_id, node.entity_type, node.label or node.canonical_ref or node.node_id)

    def _is_grounded(confidence: float, support_type: str) -> bool:
        return support_type in {"document", "mixed"} and confidence >= 0.5

    def _op_key(op: dict[str, Any]) -> str:
        return str(op.get("key") or "")

    def _dedupe_ops(ops: list[dict[str, Any]]) -> list[dict[str, Any]]:
        seen: set[str] = set()
        out: list[dict[str, Any]] = []
        for op in ops:
            key = _op_key(op)
            if not key or key in seen:
                continue
            seen.add(key)
            out.append(op)
        return out

    def _line_for_flow(leg: FlowLeg) -> str:
        return flow_leg_line(leg, node_index)

    def _line_for_step(step: ProcessStep) -> str:
        return process_step_line(step, node_index)

    def _flow_op(leg: FlowLeg) -> dict[str, Any]:
        product_key = _canonical(leg.product_node_id, leg.branch_id)
        return {
            "key": f"flow:{leg.leg_id}",
            "kind": "flow",
            "line": _line_for_flow(leg),
            "grounded": _is_grounded(float(leg.confidence), leg.support_type),
            "confidence": float(leg.confidence),
            "supportType": leg.support_type,
            "branchId": leg.branch_id,
            "fingerprint": [
                _canonical(leg.supplier_node_id, leg.branch_id),
                f"FLOW[{product_key}]",
                _canonical(leg.customer_node_id, leg.branch_id),
            ],
            "fromHostId": leg.supplier_node_id,
            "toHostId": leg.customer_node_id,
            "productId": leg.product_node_id,
        }

    def _step_op(step: ProcessStep, output_id: str) -> dict[str, Any]:
        input_keys = [_canonical(node_id, step.branch_id) for node_id in step.input_node_ids[:6]]
        return {
            "key": f"step:{step.step_id}:{output_id}",
            "kind": step.process_kind,
            "line": _line_for_step(step),
            "grounded": _is_grounded(float(step.confidence), step.support_type),
            "confidence": float(step.confidence),
            "supportType": step.support_type,
            "branchId": step.branch_id,
            "fingerprint": [
                _canonical(step.host_node_id, step.branch_id),
                f"{step.process_kind.upper()}[{'+'.join(input_keys) or 'none'}]",
                _canonical(output_id, step.branch_id),
            ],
            "hostId": step.host_node_id,
            "inputIds": list(step.input_node_ids),
            "outputId": output_id,
        }

    def _record_from_ops(
        *,
        host_id: str,
        product_id: str,
        ops: list[dict[str, Any]],
        source_label: str,
        source_type: str,
    ) -> dict[str, Any]:
        deduped_ops = _dedupe_ops(ops)
        grounded = sum(1 for op in deduped_ops if op.get("grounded"))
        return {
            "hostId": host_id,
            "productId": product_id,
            "sourceLabel": source_label or _state_label(host_id, product_id),
            "sourceType": source_type,
            "ops": deduped_ops,
            "path": [str(op.get("line") or "") for op in deduped_ops if str(op.get("line") or "").strip()],
            "hops": len(deduped_ops),
            "groundedEdgeCount": grounded,
            "weakEdgeCount": len(deduped_ops) - grounded,
            "confidenceScore": round(sum(float(op.get("confidence") or 0.0) for op in deduped_ops), 6),
        }

    def _better(existing: dict[str, Any] | None, incoming: dict[str, Any]) -> bool:
        if existing is None:
            return True
        current_tuple = (
            int(existing["groundedEdgeCount"]),
            -int(existing["weakEdgeCount"]),
            int(existing["hops"]),
            float(existing["confidenceScore"]),
        )
        incoming_tuple = (
            int(incoming["groundedEdgeCount"]),
            -int(incoming["weakEdgeCount"]),
            int(incoming["hops"]),
            float(incoming["confidenceScore"]),
        )
        return incoming_tuple > current_tuple

    flow_out: dict[tuple[str, str], list[FlowLeg]] = {}
    flow_in: dict[tuple[str, str], list[FlowLeg]] = {}
    for leg in flow_legs:
        flow_out.setdefault((leg.supplier_node_id, leg.product_node_id), []).append(leg)
        flow_in.setdefault((leg.customer_node_id, leg.product_node_id), []).append(leg)

    steps_by_host: dict[str, list[ProcessStep]] = {}
    outputs_by_host_product: dict[tuple[str, str], list[ProcessStep]] = {}
    consumers_by_host_product: dict[tuple[str, str], list[ProcessStep]] = {}
    for step in process_steps:
        steps_by_host.setdefault(step.host_node_id, []).append(step)
        for output_id in step.output_node_ids:
            outputs_by_host_product.setdefault((step.host_node_id, output_id), []).append(step)
        for input_id in step.input_node_ids:
            consumers_by_host_product.setdefault((step.host_node_id, input_id), []).append(step)

    reachable: dict[tuple[str, str], dict[str, Any]] = {}
    partial_chains: list[dict[str, Any]] = []

    def _upsert_state(host_id: str, product_id: str, record: dict[str, Any]) -> bool:
        key = (host_id, product_id)
        changed = False
        if _better(reachable.get(key), record):
            reachable[key] = record
            changed = True
        if product_id and _node_type(product_id) == "PRODUCT" and host_id != product_id:
            mirror_key = (product_id, product_id)
            mirror_record = {
                **record,
                "hostId": product_id,
                "productId": product_id,
            }
            if _better(reachable.get(mirror_key), mirror_record):
                reachable[mirror_key] = mirror_record
                changed = True
        return changed

    for step in process_steps:
        if step.process_kind == "extraction":
            deposit_labels = [_node_text(node_id) for node_id in step.input_node_ids[:4] if node_id]
            source_label = ", ".join(deposit_labels) if deposit_labels else _node_text(step.host_node_id)
            source_type = "DEPOSIT" if deposit_labels else _node_type(step.host_node_id)
            for output_id in step.output_node_ids:
                _upsert_state(
                    step.host_node_id,
                    output_id,
                    _record_from_ops(
                        host_id=step.host_node_id,
                        product_id=output_id,
                        ops=[_step_op(step, output_id)],
                        source_label=source_label,
                        source_type=source_type,
                    ),
                )

    for step in process_steps:
        if step.process_kind != "extraction" and not step.input_node_ids:
            for output_id in step.output_node_ids:
                _upsert_state(
                    step.host_node_id,
                    output_id,
                    _record_from_ops(
                        host_id=step.host_node_id,
                        product_id=output_id,
                        ops=[_step_op(step, output_id)],
                        source_label=_node_text(step.host_node_id),
                        source_type=_node_type(step.host_node_id),
                    ),
                )

    for leg in flow_legs:
        key = (leg.supplier_node_id, leg.product_node_id)
        if key in reachable:
            continue
        if flow_in.get(key):
            continue
        if outputs_by_host_product.get(key):
            continue
        _upsert_state(
            leg.supplier_node_id,
            leg.product_node_id,
            _record_from_ops(
                host_id=leg.supplier_node_id,
                product_id=leg.product_node_id,
                ops=[],
                source_label=_state_label(leg.supplier_node_id, leg.product_node_id),
                source_type=_node_type(leg.supplier_node_id),
            ),
        )

    max_passes = 24
    for _ in range(max_passes):
        changed = False
        for (supplier_id, product_id), legs in list(flow_out.items()):
            source_record = reachable.get((supplier_id, product_id))
            if source_record is None:
                continue
            for leg in legs:
                changed |= _upsert_state(
                    leg.customer_node_id,
                    leg.product_node_id,
                    _record_from_ops(
                        host_id=leg.customer_node_id,
                        product_id=leg.product_node_id,
                        ops=[*source_record["ops"], _flow_op(leg)],
                        source_label=source_record["sourceLabel"],
                        source_type=source_record["sourceType"],
                    ),
                )

        for step in process_steps:
            if step.process_kind == "extraction":
                continue
            input_records = [reachable.get((step.host_node_id, node_id)) for node_id in step.input_node_ids]
            reachable_inputs = [record for record in input_records if record is not None]
            if step.input_node_ids and len(reachable_inputs) != len(step.input_node_ids):
                if reachable_inputs:
                    missing_inputs = [
                        _node_text(node_id)
                        for node_id, record in zip(step.input_node_ids, input_records)
                        if record is None
                    ]
                    anchor = max(reachable_inputs, key=lambda item: (item["groundedEdgeCount"], item["hops"], item["confidenceScore"]))
                    partial_chains.append(
                        {
                            "source": anchor["sourceLabel"],
                            "sink": _state_label(step.host_node_id, step.output_node_ids[0] if step.output_node_ids else step.host_node_id),
                            "sinkType": _node_type(step.host_node_id),
                            "hops": anchor["hops"],
                            "path": anchor["path"][:8],
                            "groundedEdgeCount": anchor["groundedEdgeCount"],
                            "weakEdgeCount": anchor["weakEdgeCount"],
                            "breakReason": f"Process step at {_node_text(step.host_node_id)} is missing input(s): {', '.join(missing_inputs[:4])}",
                            "routeFingerprint": [list(op["fingerprint"]) for op in anchor["ops"][:8] if isinstance(op.get("fingerprint"), list)],
                        }
                    )
                continue

            if not reachable_inputs and step.input_node_ids:
                continue

            combined_ops: list[dict[str, Any]] = []
            source_label = ""
            source_type = ""
            for record in reachable_inputs:
                combined_ops.extend(record["ops"])
                if not source_label:
                    source_label = record["sourceLabel"]
                    source_type = record["sourceType"]
            if not source_label:
                source_label = _node_text(step.host_node_id)
                source_type = _node_type(step.host_node_id)
            for output_id in step.output_node_ids:
                changed |= _upsert_state(
                    step.host_node_id,
                    output_id,
                    _record_from_ops(
                        host_id=step.host_node_id,
                        product_id=output_id,
                        ops=[*combined_ops, _step_op(step, output_id)],
                        source_label=source_label,
                        source_type=source_type,
                    ),
                )
        if not changed:
            break

    grounded_complete: list[dict[str, Any]] = []
    structurally_complete: list[dict[str, Any]] = []
    grounded_requested_corridor: list[dict[str, Any]] = []
    structural_requested_corridor: list[dict[str, Any]] = []
    flow_connected_nodes: set[str] = set()
    for (host_id, product_id), record in reachable.items():
        if host_id == product_id and _node_type(product_id) == "PRODUCT":
            continue
        terminal = not flow_out.get((host_id, product_id)) and not consumers_by_host_product.get((host_id, product_id))
        if host_id != product_id and _node_type(product_id) == "PRODUCT":
            legacy_product_flows = flow_out.get((product_id, product_id)) or []
            legacy_product_consumers = consumers_by_host_product.get((product_id, product_id)) or []
            if any(leg.customer_node_id != host_id for leg in legacy_product_flows) or legacy_product_consumers:
                terminal = False
        sink_label = _node_text(host_id) if _node_type(host_id) and _node_type(host_id) != "PRODUCT" else _state_label(host_id, product_id)
        entry = {
            "source": record["sourceLabel"],
            "sink": sink_label,
            "sinkType": _node_type(host_id) or _node_type(product_id),
            "hops": record["hops"],
            "path": record["path"][:8],
            "groundedEdgeCount": record["groundedEdgeCount"],
            "weakEdgeCount": record["weakEdgeCount"],
            "routeFingerprint": [list(op["fingerprint"]) for op in record["ops"][:8] if isinstance(op.get("fingerprint"), list)],
        }
        profile = _business_chain_profile(
            record=record,
            host_id=host_id,
            product_id=product_id,
            sink_label=sink_label,
            node_index=node_index,
        )
        corridor_profile = _requested_corridor_profile_for_record(
            record=record,
            host_id=host_id,
            product_id=product_id,
            sink_label=sink_label,
            node_index=node_index,
            graph_state=graph_state,
            request=request,
            corridor=requested_corridor,
        )
        if corridor_profile["active"]:
            profile = {
                **profile,
                "criteria": {
                    **profile["criteria"],
                    "matchesRequestedSourceScope": corridor_profile["matchesSourceScope"],
                    "matchesRequestedSinkScope": corridor_profile["matchesSinkScope"],
                },
                "missingCriteria": [*profile["missingCriteria"], *corridor_profile["missingCriteria"]],
                "businessScore": profile["businessScore"]
                + int(corridor_profile["matchesSourceScope"])
                + int(corridor_profile["matchesSinkScope"]),
                "isBusinessComplete": profile["isBusinessComplete"] and corridor_profile["isSatisfied"],
            }
        entry["businessProfile"] = profile
        entry["requestedCorridor"] = corridor_profile
        for op in record["ops"]:
            if op.get("kind") == "flow":
                if op.get("fromHostId"):
                    flow_connected_nodes.add(str(op["fromHostId"]))
                if op.get("toHostId"):
                    flow_connected_nodes.add(str(op["toHostId"]))
            elif op.get("hostId"):
                flow_connected_nodes.add(str(op["hostId"]))
        if not terminal:
            continue
        if not profile["isBusinessComplete"]:
            entry["breakReason"] = _business_chain_break_reason(profile)
            partial_chains.append(entry)
            continue
        all_grounded = record["weakEdgeCount"] == 0
        if all_grounded:
            grounded_complete.append(entry)
            if corridor_profile["isSatisfied"]:
                grounded_requested_corridor.append(entry)
        else:
            entry["structuralReason"] = (
                "Business-complete end-to-end operational route exists, but at least one leg or process step is hypothesis/model-knowledge level."
            )
            structurally_complete.append(entry)
            if corridor_profile["isSatisfied"]:
                structural_requested_corridor.append(entry)

    governance_contract = ROLE_CONTRACTS.get(ROLE_GOVERNANCE)
    governance_rels: set[str] = set(governance_contract.allowed_edge_types) if governance_contract else set()
    bridge_candidates: list[dict[str, str]] = []
    for edge in overlay_edges:
        if edge.relation not in governance_rels:
            continue
        for node_id, entity_type, label in (
            (edge.subject_id, edge.subject_type, edge.subject),
            (edge.object_id, edge.object_type, edge.object),
        ):
            if node_id and entity_type in {"FACILITY", "PRODUCT", "ORGANIZATION"} and node_id not in flow_connected_nodes:
                bridge_candidates.append(
                    {
                        "entity": label or node_id,
                        "type": entity_type,
                        "issue": "Known in overlay/governance layer but not connected to any operational flow state",
                    }
                )

    def _dedupe_entries(entries: list[dict[str, Any]]) -> list[dict[str, Any]]:
        seen: set[tuple[str, str, tuple[str, ...], str]] = set()
        out: list[dict[str, Any]] = []
        for entry in entries:
            key = (
                str(entry.get("source") or ""),
                str(entry.get("sink") or ""),
                tuple(entry.get("path") or []),
                str(entry.get("breakReason") or entry.get("structuralReason") or ""),
            )
            if key in seen:
                continue
            seen.add(key)
            out.append(entry)
        return out

    grounded_complete = sorted(_dedupe_entries(grounded_complete), key=lambda item: (-int(item.get("hops") or 0), str(item.get("sink") or "")))
    structurally_complete = sorted(_dedupe_entries(structurally_complete), key=lambda item: (-int(item.get("hops") or 0), str(item.get("sink") or "")))
    grounded_requested_corridor = sorted(
        _dedupe_entries(grounded_requested_corridor),
        key=lambda item: (-int(item.get("hops") or 0), str(item.get("sink") or "")),
    )
    structural_requested_corridor = sorted(
        _dedupe_entries(structural_requested_corridor),
        key=lambda item: (-int(item.get("hops") or 0), str(item.get("sink") or "")),
    )
    partial_chains = sorted(_dedupe_entries(partial_chains), key=lambda item: (-int(item.get("hops") or 0), str(item.get("sink") or "")))

    typed_backbone_entries = compute_typed_operational_backbone_entries(graph_state, node_index)
    typed_backbone_entries = typed_backbone_entries[:_rch_list_cap]
    tb_n = len(typed_backbone_entries)

    g_n, s_n, p_n = len(grounded_complete), len(structurally_complete), len(partial_chains)
    corridor_g_n = len(grounded_requested_corridor)
    corridor_s_n = len(structural_requested_corridor)
    if not grounded_complete and not structurally_complete and not partial_chains and not typed_backbone_entries:
        health = "No operational routes found. Graph contains nodes but no usable flow-leg/process-step connectivity."
    elif g_n == 0 and s_n > 0:
        longest = structurally_complete[0]
        health = (
            f"No document-grounded business-complete chain. {s_n} structurally complete business chain(s), "
            f"longest {longest['source']} -> {longest['sink']} ({longest['hops']} hops)."
        )
        if p_n:
            health += f" {p_n} partial path(s)."
    elif g_n == 0 and p_n > 0:
        longest = partial_chains[0]
        health = (
            f"No document-grounded business-complete chain. No structurally complete business chain. Longest partial: "
            f"{longest['source']} -> {longest['sink']} ({longest['hops']} hops)."
        )
    elif g_n == 0 and tb_n > 0:
        t0 = typed_backbone_entries[0]
        health = (
            "No document-grounded business-complete chain. No structurally complete business chain. "
            f"Typed operational backbone: {t0.get('source')} -> {t0.get('sink')} ({int(t0.get('hops') or 0)} hops); "
            "not classified as business-complete or audit-grounded."
        )
    elif g_n == 0:
        health = (
            "No document-grounded business-complete chain. No structurally complete business chain. "
            "No partial or typed-backbone route summaries."
        )
    else:
        longest = grounded_complete[0]
        health = f"{g_n} grounded business-complete chain(s), longest {longest['hops']} hops."
        if s_n:
            health += f" {s_n} structurally complete (weak) business chain(s)."
        if p_n:
            health += f" {p_n} partial path(s)."
        if bridge_candidates:
            health += f" {len(bridge_candidates)} entity(s) in governance but not in flow."
    if requested_corridor["active"]:
        if corridor_g_n + corridor_s_n == 0:
            corridor_bits = [
                item for item in [requested_corridor.get("sourceScope"), requested_corridor.get("sinkScope")] if str(item or "").strip()
            ]
            if corridor_bits:
                health += f" Requested corridor still unresolved: {' -> '.join(corridor_bits)}."
        else:
            health += (
                f" Requested corridor covered by {corridor_g_n} grounded and {corridor_s_n} structural complete chain(s)."
            )

    def _route_families_from(entries: list[dict[str, Any]]) -> list[dict[str, Any]]:
        groups: dict[tuple[tuple[str, str, str], ...], list[dict[str, Any]]] = {}
        for entry in entries:
            raw_fp = entry.get("routeFingerprint") or []
            key = tuple(
                tuple(str(item) for item in step)
                for step in raw_fp
                if isinstance(step, (list, tuple)) and len(step) == 3
            )
            groups.setdefault(key, []).append(entry)
        ranked = sorted(
            groups.items(),
            key=lambda item: (-len(item[1]), -max(int(member.get("hops") or 0) for member in item[1]), str(item[0])),
        )
        out: list[dict[str, Any]] = []
        for fingerprint, members in ranked[:_ROUTE_FAMILY_LIMIT]:
            first = members[0]
            out.append(
                {
                    "fingerprint": [list(step) for step in fingerprint],
                    "multiplicity": len(members),
                    "narrativeDuplicate": len(members) > 1,
                    "sampleSource": first.get("source"),
                    "sampleSink": first.get("sink"),
                    "sampleHops": first.get("hops"),
                }
            )
        return out

    route_families = _route_families_from(
        grounded_complete + structurally_complete + partial_chains + typed_backbone_entries
    )

    return {
        "groundedCompleteChains": grounded_complete[:_rch_list_cap],
        "structurallyCompleteChains": structurally_complete[:_rch_list_cap],
        "partialChains": partial_chains[:_rch_list_cap],
        "typedBackboneChains": typed_backbone_entries,
        "bridgeCandidates": bridge_candidates[:6],
        "healthSummary": health,
        "groundedCompleteChainCount": g_n,
        "structuralCompleteChainCount": s_n,
        "partialChainCount": p_n,
        "typedBackboneChainCount": tb_n,
        "requestedCorridor": requested_corridor,
        "groundedRequestedCorridorChains": grounded_requested_corridor[:_rch_list_cap],
        "structuralRequestedCorridorChains": structural_requested_corridor[:_rch_list_cap],
        "groundedRequestedCorridorChainCount": corridor_g_n,
        "structuralRequestedCorridorChainCount": corridor_s_n,
        "requestedCorridorSatisfied": (not requested_corridor["active"]) or (corridor_g_n + corridor_s_n > 0),
        "routeFamilies": route_families,
        "maxOperationalRouteHops": _max_route_hops_from_report(
            {
                "groundedCompleteChains": grounded_complete,
                "structurallyCompleteChains": structurally_complete,
                "partialChains": partial_chains,
                "typedBackboneChains": typed_backbone_entries,
            }
        ),
        "weakFlowComponentsCount": _weak_flow_component_count(graph_state),
        "requestedCorridorIntentDebug": _requested_corridor_intent_debug(state),
    }


def _graph_reachability_report(state: dict[str, Any]) -> dict[str, Any]:
    return _graph_reachability_report_for_graph(_load_operational_graph_state(state), state=state)


def _investigation_reachability_counts_bundle(state: dict[str, Any]) -> dict[str, Any]:
    """Single reachability read for policy/todos (keeps counts aligned)."""
    reach = _graph_reachability_report(state)
    return {
        "reachability": reach,
        "groundedCompleteChainCount": int(reach.get("groundedCompleteChainCount") or 0),
        "structuralCompleteChainCount": int(reach.get("structuralCompleteChainCount") or 0),
        "partialChainCount": int(reach.get("partialChainCount") or 0),
        "typedBackboneChainCount": int(reach.get("typedBackboneChainCount") or 0),
        "requestedCorridorSatisfied": bool(reach.get("requestedCorridorSatisfied")),
    }


def _supervisor_preflight_context(state: dict[str, Any]) -> dict[str, Any]:
    """Preflight profile + red-team scheduling hints for the supervisor (deterministic)."""
    raw_pf = state.get(STATE_INVESTIGATION_PREFLIGHT) or {}
    try:
        pf = InvestigationPreflight.model_validate(raw_pf)
    except Exception:
        pf = InvestigationPreflight()
    rb = _investigation_reachability_counts_bundle(state)
    reach = rb.get("reachability") or {}
    req_rt = preflight_requires_red_team_pass(pf)
    eligible = red_team_frame_eligible(reach)
    satisfied = bool(state.get(STATE_RED_TEAM_SATISFIED))
    cycle = int(state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)
    if ALLOW_COMPOSE_WITHOUT_RED_TEAM:
        status = "satisfied"
    elif not req_rt:
        status = "not_required"
    elif satisfied:
        status = "satisfied"
    elif eligible:
        if _supervisor_budget_pressure(cycle, MAX_SUPERVISOR_CYCLES) == "hard" and req_rt and not satisfied:
            status = "blocked_by_budget"
        else:
            status = "pending"
    else:
        status = "waiting_frame"
    rf = reach.get("routeFamilies") or []
    rfn = len(rf) if isinstance(rf, list) else 0
    todos_raw = list(state.get(STATE_INVESTIGATION_TODOS) or [])
    alt_item = next((x for x in todos_raw if isinstance(x, dict) and x.get("phase") == "alternatives"), None)
    alternatives_status = (alt_item or {}).get("status") if alt_item else None
    gc = int(rb.get("groundedCompleteChainCount") or 0)
    sc = int(rb.get("structuralCompleteChainCount") or 0)
    multiplicity_shortfall = max(0, 2 - rfn) if req_rt else 0
    breadth_debt = {
        "routeFamilyCount": rfn,
        "groundedCompleteChainCount": gc,
        "structuralCompleteChainCount": sc,
        "alternativesTodoStatus": alternatives_status,
        "preflightRequiresBreadthPass": req_rt,
        "multiplicityShortfall": multiplicity_shortfall,
    }
    return {
        "investigationPreflight": pf.model_dump(mode="json", by_alias=True),
        "preflightRequiresRedTeamPass": req_rt,
        "redTeamFrameEligible": eligible,
        "redTeamStatus": status,
        "routeFamilyCount": rfn,
        "breadthDebt": breadth_debt,
    }


def _host_local_process_gap_candidates(
    graph_state: OperationalGraphState,
) -> list[dict[str, Any]]:
    """Detect likely missing host-local transform/assembly steps from converging flow legs."""
    node_index = _graph_node_map(graph_state)
    inbound_by_host: dict[str, list[FlowLeg]] = {}
    outbound_by_host: dict[str, list[FlowLeg]] = {}
    steps_by_host: dict[str, list[ProcessStep]] = {}
    for leg in graph_state.flow_legs:
        if leg.customer_node_id:
            inbound_by_host.setdefault(leg.customer_node_id, []).append(leg)
        if leg.supplier_node_id:
            outbound_by_host.setdefault(leg.supplier_node_id, []).append(leg)
    for step in graph_state.process_steps:
        if step.host_node_id:
            steps_by_host.setdefault(step.host_node_id, []).append(step)

    candidates: list[dict[str, Any]] = []
    seen: set[tuple[str, str, tuple[str, ...], str]] = set()
    for host_id in sorted(set(inbound_by_host) & set(outbound_by_host)):
        host_type = _graph_node_type(node_index, host_id)
        if host_type not in {"FACILITY", "ORGANIZATION"}:
            continue
        host_label = _graph_node_label(node_index, host_id)
        if not host_label or _is_placeholder_node(host_id, host_label):
            continue
        inbound_legs = inbound_by_host.get(host_id) or []
        outbound_legs = outbound_by_host.get(host_id) or []
        inbound_products = _dedupe_preserve_order(
            [
                leg.product_node_id
                for leg in inbound_legs
                if leg.product_node_id and _graph_node_type(node_index, leg.product_node_id) == "PRODUCT"
            ]
        )
        outbound_products = _dedupe_preserve_order(
            [
                leg.product_node_id
                for leg in outbound_legs
                if leg.product_node_id and _graph_node_type(node_index, leg.product_node_id) == "PRODUCT"
            ]
        )
        if not inbound_products or not outbound_products:
            continue
        hosted_steps = steps_by_host.get(host_id) or []
        for output_product_id in outbound_products:
            candidate_inputs = [pid for pid in inbound_products if pid != output_product_id]
            if not candidate_inputs:
                continue
            matching_steps = [step for step in hosted_steps if output_product_id in step.output_node_ids]
            if any(set(candidate_inputs).issubset(set(step.input_node_ids)) for step in matching_steps):
                continue
            process_kind = "assembly" if len(candidate_inputs) >= 2 else "transformation"
            key = (host_id, output_product_id, tuple(candidate_inputs), process_kind)
            if key in seen:
                continue
            seen.add(key)
            inbound_labels = [_graph_node_label(node_index, pid) for pid in candidate_inputs if _graph_node_label(node_index, pid)]
            outbound_label = _graph_node_label(node_index, output_product_id)
            support_score = (
                sum(float(leg.confidence) for leg in [*inbound_legs, *outbound_legs])
                / max(len(inbound_legs) + len(outbound_legs), 1)
            )
            candidates.append(
                {
                    "hostId": host_id,
                    "host": host_label,
                    "processKind": process_kind,
                    "inputProductIds": candidate_inputs[:4],
                    "inputProducts": inbound_labels[:4],
                    "outputProductId": output_product_id,
                    "outputProduct": outbound_label,
                    "reason": (
                        f"{host_label} already receives {', '.join(inbound_labels[:3])} and ships {outbound_label}. "
                        f"The graph is missing a hosted {process_kind} step that turns inbound materials into the outbound product."
                    ),
                    "supportingLegs": _dedupe_preserve_order(
                        [flow_leg_line(leg, node_index) for leg in inbound_legs[:3]]
                        + [flow_leg_line(leg, node_index) for leg in outbound_legs[:3]]
                    )[:6],
                    "confidence": round(min(0.98, max(0.25, support_score)), 3),
                }
            )
    candidates.sort(
        key=lambda item: (
            0 if item.get("processKind") == "assembly" else 1,
            -len(item.get("inputProductIds") or []),
            -float(item.get("confidence") or 0.0),
            str(item.get("host") or ""),
        )
    )
    return candidates[:6]


def _commercial_bridge_candidates(
    graph_state: OperationalGraphState,
    *,
    state: dict[str, Any] | None = None,
) -> list[dict[str, Any]]:
    request = _request_from_state(state)
    requested_corridor = _requested_operational_corridor(request)
    node_index = _graph_node_map(graph_state)
    component_by_node = _operational_component_index(graph_state)
    if not component_by_node:
        return []

    component_nodes: dict[str, set[str]] = {}
    for node_id, component_id in component_by_node.items():
        component_nodes.setdefault(component_id, set()).add(node_id)

    extraction_components = {
        component_by_node[step.host_node_id]
        for step in graph_state.process_steps
        if step.process_kind == "extraction" and step.host_node_id in component_by_node
    }
    source_components: set[str] = set(extraction_components)
    sink_components: set[str] = set()
    for component_id, node_ids in component_nodes.items():
        component_blob = " ".join(
            _node_supporting_text(node_id, node_index, graph_state, request)
            for node_id in node_ids
            if node_id in node_index
        )
        if requested_corridor.get("sourceScope") and _scope_matches_text(component_blob, str(requested_corridor["sourceScope"])):
            source_components.add(component_id)
        if requested_corridor.get("sinkScope") and _scope_matches_text(component_blob, str(requested_corridor["sinkScope"])):
            sink_components.add(component_id)
    if not source_components:
        return []

    downstream_components = sink_components or {component_id for component_id in component_nodes if component_id not in source_components}
    if not downstream_components:
        return []

    source_supply: dict[tuple[str, str], list[dict[str, Any]]] = {}
    downstream_demand: dict[tuple[str, str], list[dict[str, Any]]] = {}

    for leg in graph_state.flow_legs:
        product_key = _product_identity_key(node_index, leg.product_node_id)
        supplier_component = component_by_node.get(leg.supplier_node_id)
        customer_component = component_by_node.get(leg.customer_node_id)
        if supplier_component in source_components:
            source_supply.setdefault((supplier_component, product_key), []).append(
                {
                    "hostId": leg.supplier_node_id,
                    "host": _graph_node_label(node_index, leg.supplier_node_id),
                    "productId": leg.product_node_id,
                    "product": _graph_node_label(node_index, leg.product_node_id),
                    "line": flow_leg_line(leg, node_index),
                    "confidence": float(leg.confidence),
                }
            )
        if customer_component in downstream_components:
            downstream_demand.setdefault((customer_component, product_key), []).append(
                {
                    "hostId": leg.customer_node_id,
                    "host": _graph_node_label(node_index, leg.customer_node_id),
                    "productId": leg.product_node_id,
                    "product": _graph_node_label(node_index, leg.product_node_id),
                    "line": flow_leg_line(leg, node_index),
                    "confidence": float(leg.confidence),
                }
            )

    for step in graph_state.process_steps:
        component_id = component_by_node.get(step.host_node_id)
        if not component_id:
            continue
        if component_id in source_components:
            for output_id in step.output_node_ids:
                product_key = _product_identity_key(node_index, output_id)
                source_supply.setdefault((component_id, product_key), []).append(
                    {
                        "hostId": step.host_node_id,
                        "host": _graph_node_label(node_index, step.host_node_id),
                        "productId": output_id,
                        "product": _graph_node_label(node_index, output_id),
                        "line": process_step_line(step, node_index),
                        "confidence": float(step.confidence),
                    }
                )
        if component_id in downstream_components:
            for input_id in step.input_node_ids:
                product_key = _product_identity_key(node_index, input_id)
                downstream_demand.setdefault((component_id, product_key), []).append(
                    {
                        "hostId": step.host_node_id,
                        "host": _graph_node_label(node_index, step.host_node_id),
                        "productId": input_id,
                        "product": _graph_node_label(node_index, input_id),
                        "line": process_step_line(step, node_index),
                        "confidence": float(step.confidence),
                    }
                )

    candidates: list[dict[str, Any]] = []
    seen: set[tuple[str, str, str]] = set()
    for (source_component, product_key), supplies in source_supply.items():
        for downstream_component in downstream_components:
            if downstream_component == source_component:
                continue
            demands = downstream_demand.get((downstream_component, product_key)) or []
            if not demands:
                continue
            upstream = max(supplies, key=lambda item: (float(item["confidence"]), item["host"], item["product"]))
            downstream = max(demands, key=lambda item: (float(item["confidence"]), item["host"], item["product"]))
            if not upstream["host"] or not downstream["host"]:
                continue
            if _is_unresolved_aggregate_node(
                str(upstream["hostId"]),
                str(upstream["host"]),
                entity_type="FACILITY",
            ) or _is_unresolved_aggregate_node(
                str(downstream["hostId"]),
                str(downstream["host"]),
                entity_type="FACILITY",
            ):
                continue
            candidate_key = (str(upstream["hostId"]), str(downstream["hostId"]), str(upstream["productId"]))
            if candidate_key in seen:
                continue
            seen.add(candidate_key)
            candidates.append(
                {
                    "bridgeKind": "commercial_flow_bridge",
                    "upstreamHostId": upstream["hostId"],
                    "upstreamHost": upstream["host"],
                    "downstreamHostId": downstream["hostId"],
                    "downstreamHost": downstream["host"],
                    "productId": upstream["productId"],
                    "product": upstream["product"],
                    "sourceComponentId": source_component,
                    "downstreamComponentId": downstream_component,
                    "sinkScopeMatched": downstream_component in sink_components,
                    "reason": (
                        f"The graph has a source-side island around {upstream['host']} and a downstream island around "
                        f"{downstream['host']} that both touch {upstream['product']}, but no commercial bridge yet explains "
                        "who moves that product between them or through which intermediary jurisdiction or operator."
                    ),
                    "supportingLines": [upstream["line"], downstream["line"]],
                    "confidence": round(
                        min(
                            0.98,
                            max(0.25, (float(upstream["confidence"]) + float(downstream["confidence"])) / 2.0),
                        ),
                        3,
                    ),
                }
            )
    candidates.sort(
        key=lambda item: (
            0 if item.get("sinkScopeMatched") else 1,
            -float(item.get("confidence") or 0.0),
            str(item.get("upstreamHost") or ""),
            str(item.get("downstreamHost") or ""),
        )
    )
    return candidates[:6]


def _operational_node_ids(graph_state: OperationalGraphState) -> set[str]:
    node_ids: set[str] = set()
    for node in graph_state.entity_nodes:
        if node.node_id:
            node_ids.add(node.node_id)
    for leg in graph_state.flow_legs:
        node_ids.update(
            node_id
            for node_id in [leg.supplier_node_id, leg.customer_node_id, leg.product_node_id, *leg.route_node_ids]
            if node_id
        )
    for step in graph_state.process_steps:
        node_ids.update(
            node_id
            for node_id in [step.host_node_id, *step.input_node_ids, *step.output_node_ids]
            if node_id
        )
    for edge in graph_state.overlay_edges:
        node_ids.update(node_id for node_id in [edge.subject_id, edge.object_id] if node_id)
    return node_ids


def _fragment_node_ids(fragments: list[GraphFragment]) -> set[str]:
    if not fragments:
        return set()
    return _operational_node_ids(graph_state_from_fragments(fragments))


def _branch_touches_commercial_bridge_candidate(branch: BranchState, candidate: dict[str, Any]) -> bool:
    candidate_ids = {
        str(candidate.get("upstreamHostId") or "").strip(),
        str(candidate.get("downstreamHostId") or "").strip(),
        str(candidate.get("productId") or "").strip(),
    }
    candidate_ids.discard("")
    fragment_ids = _fragment_node_ids(branch.graph_fragments)
    if fragment_ids and (candidate_ids & fragment_ids):
        return True
    blob = " ".join(
        [
            branch.human_question,
            branch.human_summary,
            branch.path_assessment_note,
            " ".join(branch.known_context),
            " ".join(branch.resolved_frontier),
            " ".join(branch.remaining_frontier),
            " ".join(branch.discovered_frontier),
            " ".join(branch.open_questions),
        ]
    ).lower()
    names = [
        str(candidate.get("upstreamHost") or "").strip().lower(),
        str(candidate.get("downstreamHost") or "").strip().lower(),
        str(candidate.get("product") or "").strip().lower(),
    ]
    names = [item for item in names if item]
    return sum(1 for item in names if item in blob) >= 2


def _alternative_route_family_branch_id(candidate: dict[str, Any]) -> str:
    return (
        f"alt_family_{_slug(str(candidate.get('upstreamHost') or 'upstream'))}"
        f"_{_slug(str(candidate.get('product') or 'product'))}"
        f"_to_{_slug(str(candidate.get('downstreamHost') or 'downstream'))}"
    )


def _alternative_route_family_candidates(
    state: dict[str, Any],
    graph_state: OperationalGraphState,
) -> list[dict[str, Any]]:
    bridge_candidates = _commercial_bridge_candidates(graph_state, state=state)
    if not bridge_candidates:
        return []
    aggregate_nodes = _aggregate_intermediary_nodes_from_graph(graph_state)
    aggregate_labels = [
        str(item.get("label") or item.get("nodeId") or "").strip()
        for item in aggregate_nodes
        if str(item.get("label") or item.get("nodeId") or "").strip()
    ]
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    existing_alternative_ids = {
        branch.branch_id
        for branch in branches
        if branch.hypothesis_relation == "independent_child" and branch.path_kind == "alternative"
    }
    candidates: list[dict[str, Any]] = []
    for bridge in bridge_candidates:
        touching = [branch for branch in branches if _branch_touches_commercial_bridge_candidate(branch, bridge)]
        if not touching:
            continue
        rejected_branch_ids = [branch.branch_id for branch in touching if branch.path_assessment == "rejected"]
        indirect_branch_ids = [branch.branch_id for branch in touching if branch.path_assessment == "best_supported_indirect"]
        stagnated_branch_ids = [
            branch.branch_id
            for branch in touching
            if branch.path_kind == "bridge" and branch.stagnation_count >= 1
        ]
        if not (rejected_branch_ids or indirect_branch_ids or stagnated_branch_ids):
            continue
        branch_id = _alternative_route_family_branch_id(bridge)
        if branch_id in existing_alternative_ids:
            continue
        if not rejected_branch_ids and not aggregate_labels and not indirect_branch_ids:
            continue
        candidates.append(
            {
                **bridge,
                "candidateKind": "alternative_route_family",
                "branchId": branch_id,
                "rejectedBranchIds": rejected_branch_ids[:4],
                "indirectBranchIds": indirect_branch_ids[:4],
                "stagnatedBranchIds": stagnated_branch_ids[:4],
                "aggregateIntermediaryLabels": aggregate_labels[:4],
                "reason": (
                    f"The narrow bridge around {bridge['upstreamHost']} -> {bridge['downstreamHost']} for {bridge['product']} "
                    "was rejected or remained only a generic indirect market explanation. The next valid move is a materially different "
                    "route family through another jurisdiction, operator family, or intermediary layer."
                ),
            }
        )
    candidates.sort(
        key=lambda item: (
            0 if item.get("rejectedBranchIds") else 1,
            0 if item.get("sinkScopeMatched") else 1,
            0 if item.get("aggregateIntermediaryLabels") else 1,
            -float(item.get("confidence") or 0.0),
            str(item.get("upstreamHost") or ""),
            str(item.get("downstreamHost") or ""),
        )
    )
    return candidates[:6]


def _placeholder_resolution_candidates(
    state: dict[str, Any],
    graph_state: OperationalGraphState,
) -> list[dict[str, Any]]:
    node_index = _graph_node_map(graph_state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=state)
    requested = reachability.get("requestedCorridor") or {}
    source_tokens = {
        str(token).strip().lower()
        for token in (requested.get("sourceTokens") or [])
        if str(token).strip()
    }
    sink_tokens = {
        str(token).strip().lower()
        for token in (requested.get("sinkTokens") or [])
        if str(token).strip()
    }
    candidates: list[dict[str, Any]] = []
    for node in graph_state.entity_nodes:
        if not _is_placeholder_node(node.node_id, node.label, support_type=node.support_type):
            continue
        node_id = str(node.node_id or "").strip()
        label = str(node.label or node.node_id or "").strip()
        if not node_id and not label:
            continue
        supporting_lines: list[str] = []
        branch_ids: list[str] = []
        connected_node_ids: set[str] = set()
        product_labels: list[str] = []
        flow_hits = 0
        process_hits = 0
        overlay_hits = 0
        sink_role_hits = 0
        source_role_hits = 0
        for leg in graph_state.flow_legs:
            touches = node_id in {leg.supplier_node_id, leg.customer_node_id, *leg.route_node_ids}
            if not touches:
                continue
            flow_hits += 1
            supporting_lines.append(flow_leg_line(leg, node_index))
            if leg.branch_id:
                branch_ids.append(leg.branch_id)
            connected_node_ids.update(
                value
                for value in [leg.supplier_node_id, leg.customer_node_id, leg.product_node_id, *leg.route_node_ids]
                if value
            )
            if leg.product_node_id:
                product_label = _graph_node_label(node_index, leg.product_node_id)
                if product_label:
                    product_labels.append(product_label)
            if node_id == leg.customer_node_id:
                sink_role_hits += 1
            if node_id == leg.supplier_node_id:
                source_role_hits += 1
        for step in graph_state.process_steps:
            touches = node_id == step.host_node_id or node_id in step.input_node_ids or node_id in step.output_node_ids
            if not touches:
                continue
            process_hits += 1
            supporting_lines.append(process_step_line(step, node_index))
            if step.branch_id:
                branch_ids.append(step.branch_id)
            connected_node_ids.update(
                value
                for value in [step.host_node_id, *step.input_node_ids, *step.output_node_ids]
                if value
            )
            for product_id in [*step.input_node_ids, *step.output_node_ids]:
                if product_id == node_id:
                    continue
                product_label = _graph_node_label(node_index, product_id)
                if product_label:
                    product_labels.append(product_label)
        for edge in graph_state.overlay_edges:
            if node_id not in {edge.subject_id, edge.object_id}:
                continue
            overlay_hits += 1
            supporting_lines.append(f"{edge.subject} --{edge.relation}--> {edge.object}")
            if edge.branch_id:
                branch_ids.append(edge.branch_id)
            connected_node_ids.update(value for value in [edge.subject_id, edge.object_id] if value)
        if not supporting_lines:
            supporting_lines.append(f"{label} is still a broad placeholder endpoint in the operational graph.")
        support_blob = " ".join(supporting_lines).lower()
        source_scope_matched = bool(source_tokens and any(token in support_blob for token in source_tokens))
        sink_scope_matched = bool(sink_tokens and any(token in support_blob for token in sink_tokens))
        score = (flow_hits * 5) + (process_hits * 4) + overlay_hits + (sink_role_hits * 2) + source_role_hits
        score += min(len(_dedupe_preserve_order(product_labels)), 3)
        candidates.append(
            {
                "candidateKind": "placeholder_resolution",
                "nodeId": node_id,
                "label": label,
                "entityType": node.entity_type,
                "oldId": node_id,
                "reason": (
                    f"The graph still depends on placeholder node {label}. "
                    "Resolve it with a concrete named entity or split it into real alternative route-family endpoints."
                ),
                "supportingLines": _dedupe_preserve_order(supporting_lines)[:6],
                "connectedBranchIds": _dedupe_preserve_order(branch_ids)[:6],
                "connectedNodeIds": _dedupe_preserve_order(list(connected_node_ids))[:10],
                "productLabels": _dedupe_preserve_order(product_labels)[:4],
                "sourceScopeMatched": source_scope_matched,
                "sinkScopeMatched": sink_scope_matched,
                "score": score,
                "confidence": round(float(node.confidence), 3),
            }
        )
    candidates.sort(
        key=lambda item: (
            0 if item.get("sinkScopeMatched") else 1,
            0 if item.get("sourceScopeMatched") else 1,
            -int(item.get("score") or 0),
            -float(item.get("confidence") or 0.0),
            str(item.get("label") or ""),
        )
    )
    return candidates[:6]


def _branch_addresses_placeholder_resolution(plan: BranchPlan, candidate: dict[str, Any]) -> bool:
    if plan.branch_id == str(candidate.get("branchId") or ""):
        return True
    if plan.parallel_group == "placeholder_resolution":
        return True
    blob = _branch_text_blob(plan)
    needles = [
        str(candidate.get("label") or "").strip().lower(),
        str(candidate.get("nodeId") or "").strip().lower(),
        str(candidate.get("oldId") or "").strip().lower(),
    ]
    needles.extend(str(item).strip().lower() for item in (candidate.get("productLabels") or [])[:2] if str(item).strip())
    needles = [item for item in needles if item]
    if not needles:
        return False
    has_resolution_intent = any(
        token in blob for token in ("replace", "resolve placeholder", "patchmode", "oldid", "concrete named")
    )
    return has_resolution_intent and any(item in blob for item in needles)


def _branch_touches_placeholder_resolution_candidate(branch: BranchState, candidate: dict[str, Any]) -> bool:
    candidate_ids = {
        str(candidate.get("nodeId") or "").strip(),
        *[str(item).strip() for item in (candidate.get("connectedNodeIds") or []) if str(item).strip()],
    }
    candidate_ids.discard("")
    fragment_ids = _fragment_node_ids(branch.graph_fragments)
    if fragment_ids and (candidate_ids & fragment_ids):
        return True
    blob = " ".join(
        [
            branch.human_question,
            branch.human_summary,
            branch.path_assessment_note,
            " ".join(branch.known_context),
            " ".join(branch.resolved_frontier),
            " ".join(branch.remaining_frontier),
            " ".join(branch.discovered_frontier),
            " ".join(branch.open_questions),
        ]
    ).lower()
    needles = [
        str(candidate.get("label") or "").strip().lower(),
        str(candidate.get("nodeId") or "").strip().lower(),
    ]
    needles.extend(str(item).strip().lower() for item in (candidate.get("productLabels") or [])[:2] if str(item).strip())
    needles = [item for item in needles if item]
    return any(item in blob for item in needles)


def _branch_addresses_alternative_route_family(plan: BranchPlan, candidate: dict[str, Any]) -> bool:
    if plan.branch_id == str(candidate.get("branchId") or ""):
        return True
    if plan.path_kind != "alternative":
        return False
    blob = _branch_text_blob(plan)
    needles = [
        str(candidate.get("upstreamHost") or "").strip().lower(),
        str(candidate.get("downstreamHost") or "").strip().lower(),
        str(candidate.get("product") or "").strip().lower(),
    ]
    needles = [item for item in needles if item]
    return sum(1 for item in needles if item in blob) >= 2


def _make_supply_chain_alternative_route_family_branch(
    mission: InvestigationMission,
    candidate: dict[str, Any],
    *,
    branch_id: str,
    parent_hypothesis_id: str,
) -> BranchPlan:
    upstream_host = str(candidate.get("upstreamHost") or "").strip()
    downstream_host = str(candidate.get("downstreamHost") or "").strip()
    product = str(candidate.get("product") or "").strip()
    aggregate_labels = [str(item).strip() for item in (candidate.get("aggregateIntermediaryLabels") or []) if str(item).strip()]
    return BranchPlan(
        branchId=branch_id,
        hypothesis=(
            f"A materially different route family can carry {product} from {upstream_host} into the downstream market "
            f"currently represented by {downstream_host} through another jurisdiction or intermediary layer."
        ),
        parentHypothesisId=parent_hypothesis_id,
        hypothesisRelation="independent_child",
        spawnReason=(
            "The current narrow bridge was rejected or remained only a generic market-pool explanation; "
            "a distinct route family must now be tested instead of re-asking the same bridge."
        ),
        independenceBasis=(
            "This alternative route family can stand on its own and does not depend on the rejected or over-generic parent bridge."
        ),
        pathKind="alternative",
        humanQuestion=(
            f"What materially different route family carries {product} from {upstream_host} into the downstream market "
            f"currently represented by {downstream_host}, through another jurisdiction, converter family, or intermediary operator?"
        ),
        humanAction=(
            f"Find a separate route family for {product} leaving {upstream_host}, using adjacent-country or third-country conversion "
            "when that is commercially plausible, and return named facilities/process steps instead of a generic market pool."
        ),
        closureTarget=(
            f"Produce a separate alternative route family from {upstream_host} toward {downstream_host} for {product} with real "
            "intermediary hosts, product-carrying flow legs, and a concrete downstream recipient or feeder into one."
        ),
        knownContext=[
            str(candidate.get("reason") or "").strip(),
            *[str(item).strip() for item in (candidate.get("supportingLines") or [])[:4] if str(item).strip()],
            *[
                f"Aggregate intermediary still unresolved: {label}"
                for label in aggregate_labels[:2]
            ],
        ],
        frontier=[
            "Do not restate the rejected or generic direct bridge; search a materially different operator/jurisdiction family.",
            "Prefer adjacent jurisdictions, third-country converters, or alternative CAM/cell feeder routes when they are the commercially plausible bridge.",
            "Return named facilities and hosted process steps, not only a narrative about global market pools.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["web_search", "retrieve", "reason"],
        targetEntities=[upstream_host, downstream_host, product],
        fragmentType="alternative_chain",
        parallelGroup="alternative_route_family",
        stopAfterNoImprovement=2,
    )


def _make_supply_chain_placeholder_resolution_branch(
    mission: InvestigationMission,
    candidate: dict[str, Any],
    *,
    branch_id: str,
    parent_hypothesis_id: str,
    relation: Literal["dependent_followup", "root"] = "dependent_followup",
) -> BranchPlan:
    label = str(candidate.get("label") or candidate.get("nodeId") or "placeholder").strip()
    old_id = str(candidate.get("oldId") or candidate.get("nodeId") or "").strip()
    product_labels = [str(item).strip() for item in (candidate.get("productLabels") or []) if str(item).strip()]
    product_clause = (
        f" while preserving the current product interfaces ({', '.join(product_labels[:3])})"
        if product_labels else ""
    )
    old_id_clause = f" Use oldId {old_id}." if old_id else ""
    return BranchPlan(
        branchId=branch_id,
        hypothesis=f"The placeholder endpoint {label} can be replaced by concrete named entities without breaking the current route family.",
        parentHypothesisId=parent_hypothesis_id,
        hypothesisRelation=relation,
        spawnReason="The working graph still carries a placeholder endpoint; it must be resolved before the route can be business-complete.",
        independenceBasis="This branch preserves the existing route family while replacing an aggregate stand-in with a concrete operational endpoint.",
        pathKind="bridge",
        humanQuestion=(
            f"Which concrete named entity or entities should replace placeholder node {label}{product_clause}, "
            "and what replace_entity fragment should occupy that exact graph slot?"
        ),
        humanAction=(
            f"Resolve placeholder node {label} with a replace_entity or replace_fragment patch that preserves upstream/downstream interfaces{product_clause}. "
            "If multiplicity is real, split the slot into concrete named alternatives instead of leaving one aggregate stand-in."
            + old_id_clause
        ),
        closureTarget=(
            f"Replace placeholder node {label} with concrete named ORG/FACILITY nodes and preserved operational interfaces."
        ),
        knownContext=[
            str(candidate.get("reason") or "").strip(),
            *[str(item).strip() for item in (candidate.get("supportingLines") or [])[:4] if str(item).strip()],
            *([f"Replacement oldId: {old_id}"] if old_id else []),
        ],
        frontier=[
            f"Return patchMode=replace_entity or replace_fragment for {label}.",
            "Preserve the existing product-carrying interfaces when replacing the placeholder slot.",
            "If several concrete downstream entities are real alternatives, represent them as named alternative route families instead of one aggregate endpoint.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["web_search", "retrieve", "reason"],
        targetEntities=[label, *product_labels[:3]],
        fragmentType="chain_fragment",
        parallelGroup="placeholder_resolution",
        stopAfterNoImprovement=2,
    )


def _branch_text_blob(plan: BranchPlan) -> str:
    return " ".join(
        [
            plan.human_question,
            plan.human_action,
            plan.closure_target,
            " ".join(plan.known_context),
            " ".join(plan.frontier),
            " ".join(plan.target_entities),
        ]
    ).lower()


def _branch_addresses_commercial_bridge(plan: BranchPlan, candidate: dict[str, Any]) -> bool:
    blob = _branch_text_blob(plan)
    needles = [
        str(candidate.get("upstreamHost") or "").strip().lower(),
        str(candidate.get("downstreamHost") or "").strip().lower(),
        str(candidate.get("product") or "").strip().lower(),
    ]
    needles = [item for item in needles if item]
    if not needles:
        return False
    return sum(1 for item in needles if item in blob) >= 2


def _branch_addresses_process_gap(plan: BranchPlan, candidate: dict[str, Any]) -> bool:
    blob = _branch_text_blob(plan)
    needles = [
        str(candidate.get("host") or "").strip().lower(),
        str(candidate.get("outputProduct") or "").strip().lower(),
        *[str(item).strip().lower() for item in (candidate.get("inputProducts") or [])[:2]],
    ]
    needles = [item for item in needles if item]
    if not needles:
        return False
    return sum(1 for item in needles if item in blob) >= 2


def _branch_addresses_partial_chain(plan: BranchPlan, partial: dict[str, Any]) -> bool:
    blob = _branch_text_blob(plan)
    source = str(partial.get("source") or "").strip().lower()
    sink = str(partial.get("sink") or "").strip().lower()
    if source and source in blob and sink and sink in blob:
        return True
    break_reason = str(partial.get("breakReason") or "").strip().lower()
    return bool(break_reason and break_reason[:80] in blob)


def _make_supply_chain_initial_overview_branch(
    mission: InvestigationMission,
    *,
    user_question: str,
    branch_id: str,
) -> BranchPlan:
    return BranchPlan(
        branchId=branch_id,
        hypothesis="A business-complete operational chain can be reconstructed as one connected route family.",
        parentHypothesisId="",
        hypothesisRelation="root",
        spawnReason="Initial pass must create one end-to-end business-complete supply-chain hypothesis, not a local terminal segment.",
        independenceBasis="This is the root hypothesis for the whole investigation.",
        pathKind="overview",
        humanQuestion=(
            "What is the most plausible business-complete operational chain answering the user question as one connected graph, "
            "from concrete source deposit or extraction through named intermediate product stages and host-local processing steps "
            "to a concrete downstream battery manufacturer?"
        ),
        humanAction=(
            "Build one end-to-end operational chain with source anchoring, multiple product-carrying flow legs, "
            "at least one named transformation or assembly host, and a concrete downstream battery-manufacturing sink."
        ),
        closureTarget=(
            "Produce one business-complete operational chain with deposit/extraction source, multiple flow legs, "
            "a real midstream processing layer, multiple product stages, and a concrete downstream battery-manufacturing sink."
        ),
        knownContext=[
            "The graph is empty or structurally insufficient for the user question.",
            f"Original question: {user_question[:280]}",
        ],
        frontier=[
            "Anchor the route in a concrete source deposit or extraction host.",
            "Insert the named midstream processing host(s) that convert one product stage into the next.",
            "Reach a concrete downstream battery-manufacturing facility or organization, not only an intermediate materials plant.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["reason"],
        targetEntities=["source deposit", "midstream processor", "downstream battery manufacturer"],
        fragmentType="overview_chain",
        parallelGroup="initial_business_chain",
        stopAfterNoImprovement=1,
    )


def _make_supply_chain_initial_grounding_branch(
    mission: InvestigationMission,
    *,
    user_question: str,
    branch_id: str,
) -> BranchPlan:
    return BranchPlan(
        branchId=branch_id,
        hypothesis="Request corpus and internal retrieval can ground the broad source, midstream, and sink layers for the first route family.",
        parentHypothesisId="",
        hypothesisRelation="dependent_followup",
        spawnReason="The initial hypothesis should be grounded against supplied corpus and internal retrieval as early as possible.",
        independenceBasis="This branch grounds the same root route family and must merge back into it.",
        pathKind="bridge",
        humanQuestion=(
            "Using the supplied corpus and internal retrieval, which concrete source-side nodes, midstream conversion layer, "
            "and downstream battery-manufacturing recipients are already supported for the route implied by the user question?"
        ),
        humanAction=(
            "Ground the first route family against supplied corpus and internal retrieval, focusing on named source, "
            "midstream, and downstream entities instead of short terminal segments."
        ),
        closureTarget=(
            "Ground at least one source-side node, one midstream processing layer, and one downstream battery-manufacturing sink "
            "for the initial business-complete route family."
        ),
        knownContext=[
            f"Original question: {user_question[:280]}",
            "The initial route family still needs grounded source, midstream, and sink anchors.",
        ],
        frontier=[
            "Which concrete source-side nodes are supported by supplied evidence?",
            "Which midstream conversion or assembly layer is supported by supplied evidence?",
            "Which downstream battery-manufacturing recipient is supported by supplied evidence?",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["retrieve", "web_search"],
        targetEntities=["source anchor", "midstream layer", "downstream battery manufacturer"],
        fragmentType="chain_fragment",
        parallelGroup="initial_business_chain",
        stopAfterNoImprovement=2,
    )


def _make_supply_chain_commercial_bridge_branch(
    mission: InvestigationMission,
    candidate: dict[str, Any],
    *,
    branch_id: str,
    parent_hypothesis_id: str,
    relation: Literal["dependent_followup", "root"] = "dependent_followup",
) -> BranchPlan:
    upstream_host = str(candidate.get("upstreamHost") or "").strip()
    downstream_host = str(candidate.get("downstreamHost") or "").strip()
    product = str(candidate.get("product") or "").strip()
    return BranchPlan(
        branchId=branch_id,
        hypothesis=f"A concrete commercial bridge connects {upstream_host} to {downstream_host} for {product}.",
        parentHypothesisId=parent_hypothesis_id,
        hypothesisRelation=relation,
        spawnReason="The graph already contains upstream and downstream islands touching the same product stage but still lacks the commercial bridge between them.",
        independenceBasis="This branch connects two already-relevant islands in the same corridor instead of accepting an unrelated complete route.",
        pathKind="bridge",
        humanQuestion=(
            f"What concrete commercial bridge connects {upstream_host} to {downstream_host} for {product}, "
            "including any intermediary converter, trader, toll processor, third-country route, or adjacent-jurisdiction handoff if the path is indirect?"
        ),
        humanAction=(
            f"Recover the missing commercial bridge from {upstream_host} to {downstream_host} for {product} as typed flow legs and process steps, "
            "preferring real intermediary operators or jurisdictions over a fake direct hop."
        ),
        closureTarget=(
            f"Connect {upstream_host} to {downstream_host} for {product} with a real commercial bridge layer or an honest negative result explaining why no such bridge is supportable."
        ),
        knownContext=[
            str(candidate.get("reason") or "").strip(),
            *[str(item).strip() for item in (candidate.get("supportingLines") or [])[:4] if str(item).strip()],
        ],
        frontier=[
            f"Identify the intermediary operator, converter, trader, or route that moves {product} from {upstream_host} toward {downstream_host}.",
            "If no direct bridge is supported, search adjacent regions and third-country routing without fabricating a direct supply leg.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["retrieve", "web_search", "reason"],
        targetEntities=[upstream_host, downstream_host, product],
        fragmentType="chain_fragment",
        parallelGroup="commercial_bridge",
        stopAfterNoImprovement=2,
    )


def _make_supply_chain_process_gap_branch(
    mission: InvestigationMission,
    candidate: dict[str, Any],
    *,
    branch_id: str,
    parent_hypothesis_id: str,
    relation: Literal["dependent_followup", "root"] = "dependent_followup",
) -> BranchPlan:
    host = str(candidate.get("host") or "").strip()
    output_product = str(candidate.get("outputProduct") or "").strip()
    input_products = [str(item).strip() for item in (candidate.get("inputProducts") or []) if str(item).strip()]
    process_kind = str(candidate.get("processKind") or "transformation")
    return BranchPlan(
        branchId=branch_id,
        hypothesis=f"A concrete host-local {process_kind} step at {host} closes the current structural gap.",
        parentHypothesisId=parent_hypothesis_id,
        hypothesisRelation=relation,
        spawnReason="The graph already shows inbound and outbound product flows at the same host but lacks the hosted process step that connects them.",
        independenceBasis="This branch fills a missing operational process layer inside the current hypothesis graph and must merge back into it.",
        pathKind="bridge",
        humanQuestion=(
            f"What hosted {process_kind} step occurs at {host} that turns "
            f"{', '.join(input_products[:3])} into {output_product}, and what typed processStep should encode it?"
        ),
        humanAction=(
            f"Verify the host-local {process_kind} process step at {host} and return a typed processStep plus the supporting inbound/outbound flow context."
        ),
        closureTarget=(
            f"Add a hosted {process_kind} process step at {host} linking inbound product stages "
            f"{', '.join(input_products[:3])} to outbound {output_product}."
        ),
        knownContext=[
            str(candidate.get("reason") or "").strip(),
            *[str(item).strip() for item in (candidate.get("supportingLegs") or [])[:4] if str(item).strip()],
        ],
        frontier=[
            f"Confirm the hosted {process_kind} step at {host}.",
            f"Return explicit input product nodes {', '.join(input_products[:3])} and output product node {output_product}.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["retrieve", "web_search"],
        targetEntities=[host, *input_products[:3], output_product],
        fragmentType="chain_fragment",
        parallelGroup="structural_process_gap",
        stopAfterNoImprovement=2,
    )


def _make_supply_chain_partial_bridge_branch(
    mission: InvestigationMission,
    partial: dict[str, Any],
    *,
    branch_id: str,
    parent_hypothesis_id: str,
    closure_hint: str = "",
    relation: Literal["dependent_followup", "root"] = "dependent_followup",
) -> BranchPlan:
    source = str(partial.get("source") or "").strip()
    sink = str(partial.get("sink") or "").strip()
    break_reason = str(partial.get("breakReason") or "").strip()
    closure_clause = f" toward the larger target '{closure_hint.strip()}'" if closure_hint.strip() else ""
    return BranchPlan(
        branchId=branch_id,
        hypothesis=f"The partial route from {source} to {sink} can be extended into a business-complete chain by closing its next structural hop.",
        parentHypothesisId=parent_hypothesis_id,
        hypothesisRelation=relation,
        spawnReason="Reachability shows the current route is still partial and names the exact business-chain criteria it is missing.",
        independenceBasis="This branch closes the next structural gap inside the same hypothesis graph and must merge back into it.",
        pathKind="bridge",
        humanQuestion=(
            f"What concrete structural bridge extends the current partial route from {source} to {sink}{closure_clause} into a business-complete operational chain, "
            f"specifically closing: {break_reason}"
        ),
        humanAction=(
            f"Close the next structural hop on the partial route from {source} to {sink}{closure_clause} with named facilities, host-local process steps, and product-carrying flow legs. "
            "If the literal direct bridge is unsupported, search for an adjacent-country, third-country, or alternative operator-family route that still preserves the business chain."
        ),
        closureTarget=(
            f"Extend the partial route from {source} to {sink}{closure_clause} into a longer business-complete chain with the missing structural hop grounded."
        ),
        knownContext=[
            f"Current partial route source: {source}",
            f"Current partial route sink: {sink}",
            *([f"Higher-level target: {closure_hint.strip()}"] if closure_hint.strip() else []),
            break_reason,
            *[str(item).strip() for item in (partial.get("path") or [])[:4] if str(item).strip()],
        ],
        frontier=[
            break_reason or f"Identify the missing structural hop between {source} and {sink}.",
            "Prefer real host-local processing or intermediary operators over short terminal segments.",
            "If the direct corridor is weak, look for a materially different route family through adjacent jurisdictions or third-country intermediaries.",
        ],
        assignedRoles=list(mission.assigned_roles) or [ROLE_SUPPLY_CHAIN],
        preferredTools=["reason", "web_search", "retrieve"],
        targetEntities=[source, sink],
        fragmentType="chain_fragment",
        parallelGroup="structural_bridge",
        stopAfterNoImprovement=2,
    )


def _normalize_investigator_plan(
    state: dict[str, Any],
    mission: InvestigationMission,
    specialized_role: InvestigationRole,
    plan: InvestigatorPlan,
    *,
    initial_pass: bool,
    user_question: str,
    corpus_document_count: int,
) -> InvestigatorPlan:
    if specialized_role != ROLE_SUPPLY_CHAIN:
        return plan
    graph_state = _load_operational_graph_state(state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=state)
    commercial_bridge_candidates = _commercial_bridge_candidates(graph_state, state=state)
    process_gap_candidates = _host_local_process_gap_candidates(graph_state)
    alternative_route_family_candidates = _alternative_route_family_candidates(state, graph_state)
    placeholder_resolution_candidates = _placeholder_resolution_candidates(state, graph_state)
    business_complete = sum(_reachability_complete_counts(reachability)) > 0
    normalized_branches = list(plan.branch_plans)

    if initial_pass:
        overview_idx = next(
            (
                idx
                for idx, item in enumerate(normalized_branches)
                if item.path_kind == "overview" or item.hypothesis_relation == "root"
            ),
            None,
        )
        overview_branch = _make_supply_chain_initial_overview_branch(
            mission,
            user_question=user_question,
            branch_id=normalized_branches[overview_idx].branch_id if overview_idx is not None else "full_business_chain",
        )
        if overview_idx is None:
            normalized_branches.insert(0, overview_branch)
        else:
            normalized_branches[overview_idx] = overview_branch
        if corpus_document_count > 0 and not any("retrieve" in item.preferred_tools for item in normalized_branches):
            normalized_branches.append(
                _make_supply_chain_initial_grounding_branch(
                    mission,
                    user_question=user_question,
                    branch_id="ground_initial_business_chain",
                )
            )

    if not business_complete:
        primary_placeholder = placeholder_resolution_candidates[0] if placeholder_resolution_candidates else None
        primary_alternative = alternative_route_family_candidates[0] if alternative_route_family_candidates else None
        primary_bridge = commercial_bridge_candidates[0] if commercial_bridge_candidates else None
        primary_process_gap = process_gap_candidates[0] if process_gap_candidates else None
        primary_partial = (reachability.get("partialChains") or [None])[0]
        if primary_placeholder and not any(
            _branch_addresses_placeholder_resolution(item, primary_placeholder) for item in normalized_branches
        ):
            normalized_branches.insert(
                0,
                _make_supply_chain_placeholder_resolution_branch(
                    mission,
                    primary_placeholder,
                    branch_id=(
                        f"resolve_placeholder_{_slug(str(primary_placeholder.get('label') or primary_placeholder.get('nodeId') or 'node'))}"
                    ),
                    parent_hypothesis_id="root_business_chain" if initial_pass else "root_lithium_chain",
                    relation="root" if initial_pass and not normalized_branches else "dependent_followup",
                ),
            )
        elif primary_alternative and not any(
            _branch_addresses_alternative_route_family(item, primary_alternative) for item in normalized_branches
        ):
            normalized_branches.insert(
                0,
                _make_supply_chain_alternative_route_family_branch(
                    mission,
                    primary_alternative,
                    branch_id=str(primary_alternative.get("branchId") or _alternative_route_family_branch_id(primary_alternative)),
                    parent_hypothesis_id="root_business_chain" if initial_pass else "root_lithium_chain",
                ),
            )
        elif primary_bridge and not any(_branch_addresses_commercial_bridge(item, primary_bridge) for item in normalized_branches):
            normalized_branches.insert(
                0,
                _make_supply_chain_commercial_bridge_branch(
                    mission,
                    primary_bridge,
                    branch_id=(
                        f"bridge_{_slug(str(primary_bridge.get('upstreamHost') or 'upstream'))}"
                        f"_to_{_slug(str(primary_bridge.get('downstreamHost') or 'downstream'))}"
                    ),
                    parent_hypothesis_id="root_business_chain" if initial_pass else "root_lithium_chain",
                    relation="root" if initial_pass and not normalized_branches else "dependent_followup",
                ),
            )
        elif primary_process_gap and not any(_branch_addresses_process_gap(item, primary_process_gap) for item in normalized_branches):
            normalized_branches.insert(
                0,
                _make_supply_chain_process_gap_branch(
                    mission,
                    primary_process_gap,
                    branch_id=f"close_process_gap_{_slug(str(primary_process_gap.get('host') or 'host'))}",
                    parent_hypothesis_id="root_business_chain" if initial_pass else "root_lithium_chain",
                    relation="root" if initial_pass and not normalized_branches else "dependent_followup",
                ),
            )
        elif isinstance(primary_partial, dict) and not any(_branch_addresses_partial_chain(item, primary_partial) for item in normalized_branches):
            normalized_branches.insert(
                0,
                _make_supply_chain_partial_bridge_branch(
                    mission,
                    primary_partial,
                    branch_id=f"extend_partial_{_slug(str(primary_partial.get('sink') or 'route'))}",
                    parent_hypothesis_id="root_business_chain" if initial_pass else "root_lithium_chain",
                    closure_hint=mission.objective,
                    relation="root" if initial_pass and not normalized_branches else "dependent_followup",
                ),
            )

    prioritized = sorted(
        normalized_branches,
        key=lambda item: (
            0 if any(tool == "reason" for tool in item.preferred_tools[:1]) and item.path_kind == "overview" else 1,
            0 if item.parallel_group in {"placeholder_resolution", "alternative_route_family", "commercial_bridge", "structural_process_gap", "structural_bridge", "initial_business_chain"} else 1,
            0 if item.path_kind in {"overview", "alternative", "bridge"} else 1,
            item.stop_after_no_improvement,
            item.branch_id,
        ),
    )
    deduped: dict[str, BranchPlan] = {}
    for item in prioritized:
        deduped[item.branch_id] = item
    return plan.model_copy(update={"branch_plans": list(deduped.values())[:4]})


def _supply_chain_plan_is_overlay_shift(plan: BranchPlan) -> bool:
    if plan.fragment_type in {"ownership_overlay", "policy_overlay"}:
        return True
    blob = " ".join(
        [
            plan.human_question,
            plan.human_action,
            plan.closure_target,
            " ".join(plan.frontier),
        ]
    ).lower()
    overlay_signals = (
        "ownership",
        "owner",
        "shareholder",
        "board",
        "investor",
        "policy",
        "regulatory",
        "regulation",
        "due diligence",
        "carbon footprint",
        "traceability",
    )
    return any(signal in blob for signal in overlay_signals)


def _temporary_branch_plan_from_turn(
    branch_plan: BranchPlan,
    turn: HypothesisTurnPlan | None,
) -> BranchPlan | None:
    if turn is None:
        return None
    return BranchPlan(
        branchId=branch_plan.branch_id,
        hypothesis=branch_plan.hypothesis,
        parentHypothesisId=branch_plan.parent_hypothesis_id,
        hypothesisRelation=branch_plan.hypothesis_relation,
        spawnReason=branch_plan.spawn_reason,
        independenceBasis=branch_plan.independence_basis,
        pathKind=turn.path_kind,
        humanQuestion=turn.human_question,
        humanAction=turn.human_action,
        closureTarget=turn.closure_target,
        knownContext=turn.known_context,
        frontier=turn.frontier,
        assignedRoles=branch_plan.assigned_roles,
        preferredTools=[turn.tool_name],
        targetEntities=turn.target_entities,
        fragmentType=turn.fragment_type,
        stopAfterNoImprovement=1,
    )


def _normalize_hypothesis_turn_decision(
    state: dict[str, Any],
    branch_plan: BranchPlan,
    branch_state: BranchState,
    local_state: dict[str, Any],
    latest_result: ToolActionResult,
    decision: HypothesisTurnDecision,
) -> HypothesisTurnDecision:
    if not HYPOTHESIS_RUNTIME_STEER or HYPOTHESIS_RUNTIME_STEER_MODE == "off":
        return decision
    if branch_plan.assigned_roles != [ROLE_SUPPLY_CHAIN]:
        return decision
    graph_state = _load_operational_graph_state(local_state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=local_state)
    if sum(_reachability_complete_counts(reachability)) > 0:
        return decision
    mission = InvestigationMission.model_validate(state.get(STATE_CURRENT_MISSION) or {})
    commercial_bridge_candidates = _commercial_bridge_candidates(graph_state, state=local_state)
    process_gap_candidates = _host_local_process_gap_candidates(graph_state)
    alternative_route_family_candidates = _alternative_route_family_candidates(local_state, graph_state)
    placeholder_resolution_candidates = _placeholder_resolution_candidates(local_state, graph_state)
    aggregate_intermediary_nodes = _aggregate_intermediary_nodes_from_graph(graph_state)
    primary_bridge = commercial_bridge_candidates[0] if commercial_bridge_candidates else None
    primary_process_gap = process_gap_candidates[0] if process_gap_candidates else None
    primary_partial = (reachability.get("partialChains") or [None])[0]
    relevant_placeholders = [
        candidate
        for candidate in placeholder_resolution_candidates
        if _branch_touches_placeholder_resolution_candidate(branch_state, candidate)
        or _branch_addresses_placeholder_resolution(branch_plan, candidate)
    ]
    primary_placeholder = (
        relevant_placeholders[0]
        if relevant_placeholders else (
            placeholder_resolution_candidates[0]
            if placeholder_resolution_candidates and branch_plan.path_kind == "overview"
            else None
        )
    )
    relevant_alternatives = [
        candidate
        for candidate in alternative_route_family_candidates
        if _branch_touches_commercial_bridge_candidate(branch_state, candidate)
        or _branch_addresses_commercial_bridge(branch_plan, candidate)
        or _branch_addresses_alternative_route_family(branch_plan, candidate)
    ]
    primary_alternative = relevant_alternatives[0] if relevant_alternatives else None
    if (
        primary_alternative is None
        and primary_bridge is not None
        and (
            _branch_touches_commercial_bridge_candidate(branch_state, primary_bridge)
            or _branch_addresses_commercial_bridge(branch_plan, primary_bridge)
        )
    ):
        primary_alternative = {
            **primary_bridge,
            "candidateKind": "alternative_route_family",
            "branchId": _alternative_route_family_branch_id(primary_bridge),
            "aggregateIntermediaryLabels": [
                str(item.get("label") or item.get("nodeId") or "").strip()
                for item in aggregate_intermediary_nodes
                if str(item.get("label") or item.get("nodeId") or "").strip()
            ][:4],
            "reason": (
                f"The current bridge around {primary_bridge['upstreamHost']} -> {primary_bridge['downstreamHost']} for "
                f"{primary_bridge['product']} needs a materially different route family if the narrow bridge fails."
            ),
        }
    next_turn_branch = _temporary_branch_plan_from_turn(branch_plan, decision.next_turn)
    followup_handles_placeholder = any(
        _branch_addresses_placeholder_resolution(item, primary_placeholder)
        for item in decision.followup_branches
    ) if primary_placeholder is not None else False
    if (
        primary_placeholder is not None
        and decision.decision != "reject_hypothesis"
        and not followup_handles_placeholder
        and not (
            next_turn_branch is not None
            and _branch_addresses_placeholder_resolution(next_turn_branch, primary_placeholder)
        )
    ):
        placeholder_branch = _make_supply_chain_placeholder_resolution_branch(
            mission,
            primary_placeholder,
            branch_id=f"{branch_plan.branch_id}-resolve-{_slug(str(primary_placeholder.get('label') or primary_placeholder.get('nodeId') or 'placeholder'))}",
            parent_hypothesis_id=branch_plan.branch_id,
        )
        placeholder_turn = HypothesisTurnPlan(
            pathKind="bridge",
            toolName=placeholder_branch.preferred_tools[0] if placeholder_branch.preferred_tools else "web_search",
            humanQuestion=placeholder_branch.human_question,
            humanAction=placeholder_branch.human_action,
            whyNow="The active hypothesis still depends on a placeholder endpoint that blocks a business-complete chain.",
            closureTarget=placeholder_branch.closure_target,
            knownContext=list(placeholder_branch.known_context),
            frontier=list(placeholder_branch.frontier),
            targetEntities=list(placeholder_branch.target_entities),
            fragmentType=placeholder_branch.fragment_type,
        )
        return decision.model_copy(
            update={
                "decision": "ask_next_question_same_hypothesis",
                "decision_note": (
                    "Runtime redirected the hypothesis to replace a placeholder endpoint because the operational chain "
                    "still depends on an aggregate stand-in."
                ),
                "hypothesis_status": "active",
                "next_turn": placeholder_turn,
                "followup_branches": [],
            }
        )
    if (
        primary_alternative is not None
        and branch_plan.path_kind != "alternative"
        and (
            latest_result.path_assessment == "rejected"
            or (
                latest_result.path_assessment == "best_supported_indirect"
                and (aggregate_intermediary_nodes or branch_state.stagnation_count >= 1 or latest_result.closure_status == "closed")
            )
        )
    ):
        alternative_branch = _make_supply_chain_alternative_route_family_branch(
            mission,
            primary_alternative,
            branch_id=str(primary_alternative.get("branchId") or _alternative_route_family_branch_id(primary_alternative)),
            parent_hypothesis_id=branch_plan.branch_id,
        )
        merged_followups = [
            item
            for item in decision.followup_branches
            if not _branch_addresses_alternative_route_family(item, primary_alternative)
        ]
        merged_followups = [alternative_branch, *merged_followups][:3]
        return decision.model_copy(
            update={
                "decision": "spawn_independent_child_hypothesis",
                "decision_note": (
                    "Runtime escalated the current branch into a materially different alternative route family because "
                    "the narrow bridge was rejected or remained only a generic indirect market explanation."
                ),
                "hypothesis_status": "rejected" if latest_result.path_assessment == "rejected" else "accepted",
                "next_turn": None,
                "followup_branches": merged_followups,
            }
        )
    if primary_bridge is None and primary_process_gap is None and not isinstance(primary_partial, dict):
        return decision
    if primary_bridge is not None:
        structural_branch = _make_supply_chain_commercial_bridge_branch(
            mission,
            primary_bridge,
            branch_id=f"{branch_plan.branch_id}-commercial-bridge",
            parent_hypothesis_id=branch_plan.branch_id,
        )
        structural_turn = HypothesisTurnPlan(
            pathKind="bridge",
            toolName=structural_branch.preferred_tools[0] if structural_branch.preferred_tools else "retrieve",
            humanQuestion=structural_branch.human_question,
            humanAction=structural_branch.human_action,
            whyNow="The graph still does not connect the requested corridor and shows disconnected islands touching the same product stage.",
            closureTarget=structural_branch.closure_target,
            knownContext=list(structural_branch.known_context),
            frontier=list(structural_branch.frontier),
            targetEntities=list(structural_branch.target_entities),
            fragmentType=structural_branch.fragment_type,
        )
        needs_override = decision.decision in {"accept_hypothesis", "stall_hypothesis"} or (
            decision.decision == "ask_next_question_same_hypothesis"
            and decision.next_turn is not None
            and not _branch_addresses_commercial_bridge(
                BranchPlan(
                    branchId=branch_plan.branch_id,
                    hypothesis=branch_plan.hypothesis,
                    parentHypothesisId=branch_plan.parent_hypothesis_id,
                    hypothesisRelation=branch_plan.hypothesis_relation,
                    spawnReason=branch_plan.spawn_reason,
                    independenceBasis=branch_plan.independence_basis,
                    pathKind=decision.next_turn.path_kind,
                    humanQuestion=decision.next_turn.human_question,
                    humanAction=decision.next_turn.human_action,
                    closureTarget=decision.next_turn.closure_target,
                    knownContext=decision.next_turn.known_context,
                    frontier=decision.next_turn.frontier,
                    assignedRoles=branch_plan.assigned_roles,
                    preferredTools=[decision.next_turn.tool_name],
                    targetEntities=decision.next_turn.target_entities,
                    fragmentType=decision.next_turn.fragment_type,
                    stopAfterNoImprovement=1,
                ),
                primary_bridge,
            )
        )
        if needs_override:
            return decision.model_copy(
                update={
                    "decision": "ask_next_question_same_hypothesis",
                    "decision_note": (
                        "Runtime redirected the hypothesis to a commercial bridge because the requested corridor is still disconnected."
                    ),
                    "next_turn": structural_turn,
                    "followup_branches": [],
                }
            )
        return decision
    if primary_process_gap is not None:
        structural_branch = _make_supply_chain_process_gap_branch(
            mission,
            primary_process_gap,
            branch_id=f"{branch_plan.branch_id}-close-process-gap",
            parent_hypothesis_id=branch_plan.branch_id,
        )
        structural_turn = HypothesisTurnPlan(
            pathKind="bridge",
            toolName=structural_branch.preferred_tools[0] if structural_branch.preferred_tools else "retrieve",
            humanQuestion=structural_branch.human_question,
            humanAction=structural_branch.human_action,
            whyNow="The graph shows inbound and outbound flows at the same host but lacks the hosted process step that connects them.",
            closureTarget=structural_branch.closure_target,
            knownContext=list(structural_branch.known_context),
            frontier=list(structural_branch.frontier),
            targetEntities=list(structural_branch.target_entities),
            fragmentType=structural_branch.fragment_type,
        )
        needs_override = decision.decision in {"accept_hypothesis", "stall_hypothesis"} or (
            decision.decision == "ask_next_question_same_hypothesis"
            and decision.next_turn is not None
            and not _branch_addresses_process_gap(
                BranchPlan(
                    branchId=branch_plan.branch_id,
                    hypothesis=branch_plan.hypothesis,
                    parentHypothesisId=branch_plan.parent_hypothesis_id,
                    hypothesisRelation=branch_plan.hypothesis_relation,
                    spawnReason=branch_plan.spawn_reason,
                    independenceBasis=branch_plan.independence_basis,
                    pathKind=decision.next_turn.path_kind,
                    humanQuestion=decision.next_turn.human_question,
                    humanAction=decision.next_turn.human_action,
                    closureTarget=decision.next_turn.closure_target,
                    knownContext=decision.next_turn.known_context,
                    frontier=decision.next_turn.frontier,
                    assignedRoles=branch_plan.assigned_roles,
                    preferredTools=[decision.next_turn.tool_name],
                    targetEntities=decision.next_turn.target_entities,
                    fragmentType=decision.next_turn.fragment_type,
                    stopAfterNoImprovement=1,
                ),
                primary_process_gap,
            )
        )
        if needs_override:
            return decision.model_copy(
                update={
                    "decision": "ask_next_question_same_hypothesis",
                    "decision_note": (
                        "Runtime redirected the hypothesis to a host-local process gap because the graph still lacks a business-complete chain."
                    ),
                    "next_turn": structural_turn,
                    "followup_branches": [],
                }
            )
        return decision
    structural_branch = _make_supply_chain_partial_bridge_branch(
        mission,
        primary_partial,
        branch_id=f"{branch_plan.branch_id}-extend-partial-chain",
        parent_hypothesis_id=branch_plan.branch_id,
        closure_hint=branch_plan.closure_target,
    )
    structural_turn = HypothesisTurnPlan(
        pathKind="bridge",
        toolName=structural_branch.preferred_tools[0] if structural_branch.preferred_tools else "retrieve",
        humanQuestion=structural_branch.human_question,
        humanAction=structural_branch.human_action,
        whyNow="Reachability still shows a partial chain whose next structural hop is unresolved.",
        closureTarget=structural_branch.closure_target,
        knownContext=list(structural_branch.known_context),
        frontier=list(structural_branch.frontier),
        targetEntities=list(structural_branch.target_entities),
        fragmentType=structural_branch.fragment_type,
    )
    needs_override = decision.decision in {"accept_hypothesis", "stall_hypothesis"} or (
        decision.decision == "ask_next_question_same_hypothesis"
        and next_turn_branch is not None
        and (
            _supply_chain_plan_is_overlay_shift(next_turn_branch)
            or not _branch_addresses_partial_chain(next_turn_branch, primary_partial)
        )
    )
    if (
        decision.decision == "ask_next_question_same_hypothesis"
        and next_turn_branch is not None
        and isinstance(primary_partial, dict)
        and _branch_addresses_partial_chain(next_turn_branch, primary_partial)
        and not _supply_chain_plan_is_overlay_shift(next_turn_branch)
    ):
        needs_override = False
    if needs_override:
        return decision.model_copy(
            update={
                "decision": "ask_next_question_same_hypothesis",
                "decision_note": (
                    "Runtime redirected the hypothesis to the highest-priority partial-chain break point because the "
                    "operational chain is still incomplete."
                ),
                "next_turn": structural_turn,
                "followup_branches": [],
            }
        )
    return decision


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


def _candidate_path_signature(path: CandidatePath) -> tuple[str, str, str, str]:
    return (
        path.start_node.strip().lower(),
        path.end_node.strip().lower(),
        path.path_kind,
        path.status,
    )


def _reachability_candidate_paths(state: dict[str, Any]) -> list[CandidatePath]:
    reach = _graph_reachability_report(state)
    candidates: list[CandidatePath] = []

    def _build(
        *,
        bucket: str,
        status: Literal["confirmed", "best_supported_indirect", "still_open"],
        path_kind: Literal["overview", "indirect", "bridge"],
        entries: list[dict[str, Any]],
    ) -> None:
        for idx, entry in enumerate(entries[:3], start=1):
            title = f"{entry.get('source', '')} -> {entry.get('sink', '')}".strip(" ->")
            path_lines = [str(item).strip() for item in (entry.get("path") or []) if str(item).strip()]
            summary_parts: list[str] = []
            if path_lines:
                summary_parts.append(" | ".join(path_lines[:4]))
            if status == "still_open":
                summary_parts.append(str(entry.get("breakReason") or "").strip())
            elif status == "best_supported_indirect":
                summary_parts.append(str(entry.get("structuralReason") or "").strip())
            summary = " ".join(part for part in summary_parts if part).strip()
            confidence = 0.0
            hops = max(1, int(entry.get("hops") or 0))
            grounded_edges = max(0, int(entry.get("groundedEdgeCount") or 0))
            weak_edges = max(0, int(entry.get("weakEdgeCount") or 0))
            confidence = round(min(0.98, max(0.2, (grounded_edges + 0.5 * weak_edges) / hops)), 3)
            unresolved = []
            if status == "still_open":
                unresolved_reason = str(entry.get("breakReason") or "").strip()
                if unresolved_reason:
                    unresolved.append(unresolved_reason)
            candidates.append(
                CandidatePath(
                    pathId=f"reach-{bucket}-{idx}",
                    pathKind=path_kind,
                    status=status,
                    title=title or f"Reachability {bucket} route {idx}",
                    summary=summary or title or f"Reachability-derived {bucket} route.",
                    startNode=str(entry.get("source") or ""),
                    endNode=str(entry.get("sink") or ""),
                    viaNodes=path_lines[:6],
                    supportingBranchIds=[],
                    supportingFragmentIds=[],
                    evidenceStatus=(
                        "verified"
                        if status == "confirmed"
                        else ("mixed" if status == "best_supported_indirect" else "reasoned")
                    ),
                    confidence=confidence,
                    unresolvedHops=unresolved[:4],
                )
            )

    _build(
        bucket="grounded",
        status="confirmed",
        path_kind="overview",
        entries=list(reach.get("groundedCompleteChains") or []),
    )
    _build(
        bucket="structural",
        status="best_supported_indirect",
        path_kind="indirect",
        entries=list(reach.get("structurallyCompleteChains") or []),
    )
    _build(
        bucket="partial",
        status="still_open",
        path_kind="bridge",
        entries=list(reach.get("partialChains") or []),
    )
    _build(
        bucket="typed_backbone",
        status="best_supported_indirect",
        path_kind="overview",
        entries=list(reach.get("typedBackboneChains") or []),
    )
    return candidates


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
        family_graph = graph_state_from_fragments(branch_fragments)
        family_nodes = _graph_node_map(family_graph)
        context_lines = graph_context_lines(family_graph, limit=24)
        branch_flow_lines = [flow_leg_line(leg, family_nodes) for leg in family_graph.flow_legs]
        branch_step_lines = [process_step_line(step, family_nodes) for step in family_graph.process_steps]
        via_nodes = _dedupe_preserve_order(
            [
                *[_graph_node_label(family_nodes, leg.product_node_id) for leg in family_graph.flow_legs if leg.product_node_id],
                *[_graph_node_label(family_nodes, step.host_node_id) for step in family_graph.process_steps if step.host_node_id],
            ]
        )[:6]

        start_node = ""
        end_node = ""
        if branch_fragments:
            first_fragment = branch_fragments[0]
            last_fragment = branch_fragments[-1]
            start_node = _graph_node_label(family_nodes, first_fragment.start_node_id) if first_fragment.start_node_id else ""
            end_node = _graph_node_label(family_nodes, last_fragment.end_node_id) if last_fragment.end_node_id else ""
        if not start_node and family_graph.process_steps:
            first_step = family_graph.process_steps[0]
            if first_step.process_kind == "extraction" and first_step.input_node_ids:
                start_node = _graph_node_label(family_nodes, first_step.input_node_ids[0])
            else:
                start_node = _graph_node_label(family_nodes, first_step.host_node_id)
        if not start_node and family_graph.flow_legs:
            start_node = _graph_node_label(family_nodes, family_graph.flow_legs[0].supplier_node_id)
        if not end_node and family_graph.flow_legs:
            last_leg = family_graph.flow_legs[-1]
            end_node = _graph_node_label(family_nodes, last_leg.customer_node_id)
        if not end_node and family_graph.process_steps:
            last_step = family_graph.process_steps[-1]
            if last_step.output_node_ids:
                end_node = _graph_node_label(family_nodes, last_step.output_node_ids[-1])
            else:
                end_node = _graph_node_label(family_nodes, last_step.host_node_id)
        if not start_node and branch_fragments and branch_fragments[0].observations:
            start_node = branch_fragments[0].observations[0].subject
        if not end_node and branch_fragments and branch_fragments[-1].observations:
            end_node = branch_fragments[-1].observations[-1].object

        evidence_status = evidence_status_for_graph(family_graph)  # type: ignore[arg-type]
        confidence = (
            round(
                sum(float(item.confidence) for item in [*family_graph.flow_legs, *family_graph.process_steps, *family_graph.overlay_edges])
                / max(len(family_graph.flow_legs) + len(family_graph.process_steps) + len(family_graph.overlay_edges), 1),
                3,
            )
            if any((family_graph.flow_legs, family_graph.process_steps, family_graph.overlay_edges))
            else round(branch.quality_score, 3)
        )
        status: Literal["confirmed", "best_supported_indirect", "rejected", "still_open"] = "still_open"
        if branch.path_assessment in {"confirmed", "best_supported_indirect", "rejected"}:
            status = branch.path_assessment
        elif branch.status == "completed" and branch_fragments:
            status = "confirmed"
        summary_bits = [branch.path_assessment_note or branch.human_summary or branch.hypothesis]
        if branch_step_lines:
            summary_bits.append(branch_step_lines[0])
        if branch_flow_lines:
            summary_bits.append(branch_flow_lines[0])
        summary = " ".join(bit.strip().rstrip(".") + "." for bit in summary_bits if bit and bit.strip())
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
                viaNodes=via_nodes or context_lines[:6],
                supportingBranchIds=family_branch_ids[:8],
                supportingFragmentIds=[fragment.fragment_id for fragment in branch_fragments[:6]],
                evidenceStatus=evidence_status,  # type: ignore[arg-type]
                confidence=confidence,
                unresolvedHops=unresolved_hops,
            )
        )
    seen_signatures = {_candidate_path_signature(item) for item in candidates}
    for extra in _reachability_candidate_paths(state):
        sig = _candidate_path_signature(extra)
        if sig in seen_signatures:
            continue
        seen_signatures.add(sig)
        candidates.append(extra)
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
    observations = observation_projection(_load_operational_graph_state(state))
    supply_contract = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    governance_contract = ROLE_CONTRACTS.get(ROLE_GOVERNANCE)
    policy_contract = ROLE_CONTRACTS.get(ROLE_POLICY)

    supply_rels = set(supply_contract.allowed_edge_types) if supply_contract else set()
    governance_rels = set(governance_contract.allowed_edge_types) if governance_contract else set()
    policy_rels = set(policy_contract.allowed_edge_types) if policy_contract else set()

    buckets: dict[str, list[str]] = {"supply_chain": [], "governance": [], "policy": [], "other": []}
    for observation in observations:
        rel = observation.relation
        edge_str = (
            f"{observation.subject} --{rel}--> {observation.object} "
            f"[{observation.support_type} {observation.confidence}]"
        )
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
    return _is_supply_chain_action(action)


def _relevant_graph_context(
    state: dict[str, Any],
    *,
    target_entities: list[str],
    branch_question: str,
    limit: int = 6,
) -> list[str]:
    graph_state = _load_operational_graph_state(state)
    tokens = {
        token.lower()
        for token in _TOKEN_RE.findall(" ".join(target_entities + [branch_question]))
        if token
    }
    if not any(graph_counts(graph_state).values()):
        return []
    scored: list[tuple[int, str]] = []
    for line in graph_context_lines(graph_state, limit=48):
        haystack = line.lower()
        score = sum(1 for token in tokens if token in haystack)
        if score <= 0:
            continue
        scored.append((score, line))
    for observation in observation_projection(graph_state):
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
    return _dedupe_preserve_order([item[1] for item in scored])[:limit]


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
        JSON_WIRE_CONTRACT_V1
        + "\n\nMinimal ToolActionResult wire skeleton (extend with real graphFragments):\n"
        + TOOLACTIONRESULT_WIRE_EXAMPLE
        + "\n\n"
        + "You are a graph-native investigation tool inside AgentLoom.\n"
        "You are operating inside a graph system specialized in meaningful connected chains of typed entities and typed relations.\n"
        + role_playbook
        + "Return exactly one JSON object and nothing else.\n"
        "Do not wrap the JSON in markdown fences.\n"
        "Do not add commentary before or after the JSON.\n"
        "The JSON must match ToolActionResult exactly (including schemaVersion).\n"
        "There is one graph protocol only: the same ToolActionResult -> GraphFragment format is used for the first skeleton, later enrichment, and graph mutation.\n"
        "Do not invent a special answer shape for overview questions. Even the first whole-picture answer must be ordinary graphFragments that the runtime will later append to or replace under the same rules.\n"
        "Every factual answer must be carried by graphFragments. For supply-chain work, prefer entityNodes + flowLegs + processSteps + overlayEdges; observations are only a derived compatibility projection.\n"
        "Every useful non-graph answer must also be carried by learnedFindings. Negative answers, procurement-model findings, and stable contextual knowledge are still reusable results.\n"
        "A broad question may require multiple graph fragments and a long operational payload; do not collapse it into one entity-relation-entity pair.\n"
        "Every overlay edge must be typed as subjectType --relation--> objectType using only relations allowed by the supplied role contracts.\n"
        "The action belongs to a candidate path class (`pathKind`). You must return `pathAssessment` saying whether that path is confirmed, rejected, still open, or currently best-supported as an indirect route.\n"
        "Preserve useful but uncertain nodes and edges by lowering confidence or using supportType=hypothesis instead of deleting them.\n"
        "Do not confuse operational supply-chain flow with contractual procurement. Procurement model, sourcing strategy, offtake, localization, recycling strategy, or vertical integration without a concrete flowLeg or processStep belongs in learnedFindings and frontier, not in fake operational structure.\n"
        "Aggregate stand-ins such as a global market pool, generic trader, unnamed converter, or broad intermediary hub do not close the route. If they are the best current explanation, keep them as unresolved structural frontier and search for a named route family or jurisdictional handoff.\n"
        "If you identified only converter families, likely jurisdictions, or contractual patterns but did not add a flowLeg, processStep, or replacement patch, do not mark the route as closed or resolved. Keep it open in remainingFrontier/discoveredFrontier.\n"
        "Do not spend a web-grounded turn on stable background facts that a competent model already knows, such as well-established jurisdiction-wide regulatory frameworks, unless the action explicitly asks for current changes, enforcement, or company-specific response.\n"
        "Use the action's closureTarget, knownContext, and frontier to understand what the system already knows and what exact graph closure is being attempted.\n"
        "The working graph is mutable shared memory. If you return `patchMode=replace_entity` or `replace_fragment`, the runtime will apply that patch before later reasoning steps. Treat replacement as a real graph mutation, not as commentary.\n"
        "When replacing a broad node or old fragment, make the new fragment structurally able to occupy that old slot in the graph. Later agents will reason from the replacement graph now stored in shared memory.\n"
        "Do not return a replacement that collapses a longer chain into a short terminal segment. If the replaced slot sits in the middle of an operational route, preserve both upstream and downstream interfaces in the new fragment.\n"
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
        + OPERATIONAL_PRIMITIVES_CONTRACT
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
    graph_state = _load_operational_graph_state(tool_context.state)
    if not any(graph_counts(graph_state).values()):
        return _tool_failure_result(
            action,
            "blocked",
            "Shared graph memory does not yet contain typed observations relevant to this question.",
            missing_inputs=["working_graph"],
        )
    node_map = _graph_node_map(graph_state)
    query_tokens = {token.lower() for token in _TOKEN_RE.findall(action.query_text or "")}
    target_tokens = {token.lower() for token in _TOKEN_RE.findall(" ".join(action.target_entities))}
    all_tokens = query_tokens | target_tokens

    def _score(parts: list[str]) -> int:
        haystack = " ".join(part for part in parts if part).lower()
        if not haystack:
            return 0
        if not all_tokens:
            return 1
        return sum(1 for token in all_tokens if token in haystack)

    matched_legs: list[FlowLeg] = []
    matched_steps: list[ProcessStep] = []
    matched_overlays: list[GraphObservation] = []
    matched_node_ids: set[str] = set()
    required_node_ids: set[str] = set()

    for node in graph_state.entity_nodes:
        score = _score([node.label, node.entity_type, node.canonical_ref, " ".join(node.qualifiers), node.support_note])
        if score > 0 and node.node_id:
            matched_node_ids.add(node.node_id)

    for leg in graph_state.flow_legs:
        score = _score(
            [
                _graph_node_label(node_map, leg.supplier_node_id),
                _graph_node_label(node_map, leg.customer_node_id),
                _graph_node_label(node_map, leg.product_node_id),
                " ".join(_graph_node_label(node_map, node_id) for node_id in leg.route_node_ids),
                " ".join(leg.qualifiers),
                leg.support_note,
            ]
        )
        if score <= 0:
            continue
        matched_legs.append(leg)
        required_node_ids.update([leg.supplier_node_id, leg.customer_node_id, leg.product_node_id, *leg.route_node_ids])

    for step in graph_state.process_steps:
        score = _score(
            [
                _graph_node_label(node_map, step.host_node_id),
                step.process_kind,
                " ".join(_graph_node_label(node_map, node_id) for node_id in step.input_node_ids),
                " ".join(_graph_node_label(node_map, node_id) for node_id in step.output_node_ids),
                " ".join(step.qualifiers),
                step.support_note,
            ]
        )
        if score <= 0:
            continue
        matched_steps.append(step)
        required_node_ids.update([step.host_node_id, *step.input_node_ids, *step.output_node_ids])

    for edge in graph_state.overlay_edges:
        score = _score(
            [
                edge.subject,
                edge.subject_type,
                edge.relation,
                edge.object,
                edge.object_type,
                " ".join(edge.qualifiers),
                edge.support_note,
            ]
        )
        if score <= 0:
            continue
        matched_overlays.append(edge)
        if edge.subject_id:
            required_node_ids.add(edge.subject_id)
        if edge.object_id:
            required_node_ids.add(edge.object_id)

    required_node_ids |= matched_node_ids
    if not any((matched_legs, matched_steps, matched_overlays)):
        return _tool_failure_result(
            action,
            "blocked",
            "Shared graph memory does not answer this question yet.",
            missing_inputs=["relevant_graph_fragment"],
        )
    entity_nodes = [
        node.model_copy(update={"branch_id": node.branch_id or action.branch_id})
        for node in graph_state.entity_nodes
        if node.node_id in required_node_ids
    ][:24]
    fragment = GraphFragment(
        fragmentId=f"{action.branch_id}-memory",
        fragmentType=action.expected_fragment_type,  # type: ignore[arg-type]
        title=f"Memory answer for {action.branch_id}",
        humanQuestion=action.human_question,
        humanSummary=(
            f"Recovered {len(matched_legs[:8])} flow leg(s), {len(matched_steps[:8])} process step(s), "
            f"and {len(matched_overlays[:8])} overlay edge(s) from shared memory."
        ),
        branchId=action.branch_id,
        entityNodes=entity_nodes,
        flowLegs=[item.model_copy(update={"branch_id": item.branch_id or action.branch_id}) for item in matched_legs[:8]],
        processSteps=[item.model_copy(update={"branch_id": item.branch_id or action.branch_id}) for item in matched_steps[:8]],
        overlayEdges=[
            item.model_copy(update={"branch_id": item.branch_id or action.branch_id})
            for item in matched_overlays[:8]
        ],
        completeness="partial",
        qualityScore=0.4,
    )
    return _normalize_supply_chain_result(
        action,
        ToolActionResult(
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
        ),
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
    used_resource_fallback_holder = [False]

    def _generate_with_retry(*, model: str, contents: str, config_obj: Any) -> Any:
        return _vertex_generate_content_resilient(
            client,
            model=model,
            contents=contents,
            config=config_obj,
            allow_quota_model_fallback=True,
            used_resource_fallback_holder=used_resource_fallback_holder,
        )

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

    try:
        result = _parse_structured(response)
    except Exception as first_exc:
        first_exc_text = str(first_exc)
        json_retry_suffix = (
            "\n\nYour previous structured JSON failed validation or was truncated. "
            "Return ONLY valid JSON matching ToolActionResult. "
            "Use at most 2 graphFragments and at most 12 observations in total across all fragments. "
            "Keep strings short; escape double quotes inside JSON strings."
        )
        if "outputNodeId" in first_exc_text or "outputNodeIds" in first_exc_text:
            json_retry_suffix += (
                " Every processStep MUST include at least one outputNodeIds value. "
                "If you cannot name the output product, drop that processStep and move the clue into learnedFindings or remainingFrontier."
            )
        if "supplierNodeId" in first_exc_text or "customerNodeId" in first_exc_text:
            json_retry_suffix += (
                " Every flowLeg MUST include supplierNodeId, customerNodeId, and productNodeId, and the leg endpoints must be facilities or organizations."
            )
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

    if _needs_supply_chain_structural_retry(action, result):
        structural_retry_suffix = _supply_chain_structural_retry_suffix(action, result, shared_memory)
        try:
            response3 = _generate_with_retry(
                model=structured_model,
                contents=extraction_prompt + structural_retry_suffix,
                config_obj=live_types.GenerateContentConfig(
                    temperature=0,
                    response_mime_type="application/json",
                    response_schema=ToolActionResult,
                    max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
                ),
            )
            repaired_result = _parse_structured(response3)
            if _result_has_supply_chain_operational_progress(action, repaired_result) or (
                repaired_result.status in {"blocked", "partial"}
                and not repaired_result.graph_fragments
                and (
                    len(repaired_result.uncovered_questions) > len(result.uncovered_questions)
                    or len(repaired_result.remaining_frontier) > len(result.remaining_frontier)
                )
            ):
                result = repaired_result
        except Exception:
            pass

    source_docs = list(result.source_documents)
    if retrieval:
        source_docs.extend(
            value.get("uri", "")
            for value in _dedupe_sources(retrieved_sources)
            if value.get("uri", "")
        )
    result = result.model_copy(update={"source_documents": _dedupe_preserve_order(source_docs)[:10]})
    return _normalize_supply_chain_result(action, result)


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
        response = _vertex_generate_content_resilient(
            client,
            model=INVESTIGATOR_MODEL,
            contents=prompt,
            config=live_types.GenerateContentConfig(
                temperature=0,
                response_mime_type="application/json",
                response_schema=ToolActionResult,
                max_output_tokens=TOOL_MAX_OUTPUT_TOKENS,
            ),
            allow_quota_model_fallback=True,
            used_resource_fallback_holder=[False],
        )
        result = _parse_tool_result_payload(
            _response_json_text(response),
            action,
            _allowed_edges_for_roles(action.assigned_roles),
            "document",
        )
        source_docs = [item.get("sourceUri", "") for item in snippets if item.get("sourceUri")]
        result = result.model_copy(update={"source_documents": _dedupe_preserve_order(source_docs)[:10]})
        return _normalize_supply_chain_result(action, result)
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


def _retrieve_cache_key(action: ToolAction) -> str:
    payload = json.dumps(
        {
            "queryText": (action.query_text or "").strip(),
            "humanQuestion": (action.human_question or "").strip(),
            "branchId": action.branch_id,
        },
        sort_keys=True,
        ensure_ascii=False,
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def _store_retrieve_cache(state: dict[str, Any], action: ToolAction, result: ToolActionResult) -> None:
    """Session-local LRU-ish cache invalidated on any graph merge (plan H)."""
    if not isinstance(state.get(STATE_RETRIEVAL_CACHE), dict):
        state[STATE_RETRIEVAL_CACHE] = {}
    cache = state[STATE_RETRIEVAL_CACHE]
    if not isinstance(cache, dict):
        state[STATE_RETRIEVAL_CACHE] = {}
        cache = state[STATE_RETRIEVAL_CACHE]
    key = _retrieve_cache_key(action)
    try:
        cache[key] = result.model_dump(mode="json", by_alias=True)
    except Exception:
        return
    while len(cache) > 48:
        cache.pop(next(iter(cache)))


def retrieve(action: ToolAction, tool_context) -> ToolActionResult:
    state = tool_context.state
    cache_raw = state.get(STATE_RETRIEVAL_CACHE)
    if isinstance(cache_raw, dict):
        cached = cache_raw.get(_retrieve_cache_key(action))
        if isinstance(cached, dict):
            try:
                hit = ToolActionResult.model_validate(cached)
                return hit.model_copy(update={"tool_name": "retrieve"})
            except pydantic.ValidationError:
                pass

    memory_result = _memory_question_to_graph(action, tool_context)
    best_result = memory_result
    if memory_result.status == "success" and memory_result.graph_fragments:
        if memory_result.closure_status == "closed" or not memory_result.remaining_frontier:
            out = memory_result.model_copy(update={"tool_name": "retrieve"})
            _store_retrieve_cache(state, action, out)
            return out
        best_result = memory_result

    request = _request_from_state(tool_context.state)
    if request.corpus_documents and _vertex_grounding_config().get("available"):
        with ThreadPoolExecutor(max_workers=2) as pool:
            fut_c = pool.submit(_corpus_question_to_graph, action, tool_context)
            fut_v = pool.submit(_vertex_question_to_graph, action, tool_context)
            corpus_result = fut_c.result()
            vertex_result = fut_v.result()
        for cand in (corpus_result, vertex_result):
            if cand.status in {"success", "partial"} and (cand.graph_fragments or cand.source_documents):
                out = cand.model_copy(update={"tool_name": "retrieve"})
                _store_retrieve_cache(state, action, out)
                return out
        best_result = corpus_result if corpus_result.status != "failed" else vertex_result
    elif request.corpus_documents:
        corpus_result = _corpus_question_to_graph(action, tool_context)
        if corpus_result.status in {"success", "partial"} and (
            corpus_result.graph_fragments or corpus_result.source_documents
        ):
            out = corpus_result.model_copy(update={"tool_name": "retrieve"})
            _store_retrieve_cache(state, action, out)
            return out
        best_result = corpus_result
    elif _vertex_grounding_config().get("available"):
        vertex_result = _vertex_question_to_graph(action, tool_context)
        if vertex_result.status in {"success", "partial"} and (
            vertex_result.graph_fragments or vertex_result.source_documents
        ):
            out = vertex_result.model_copy(update={"tool_name": "retrieve"})
            _store_retrieve_cache(state, action, out)
            return out
        best_result = vertex_result

    out = best_result.model_copy(update={"tool_name": "retrieve"})
    _store_retrieve_cache(state, action, out)
    return out


def reason(action: ToolAction, tool_context) -> ToolActionResult:
    return _reason_question_to_graph(action, tool_context).model_copy(update={"tool_name": "reason"})


def supply_qa(action: ToolAction, tool_context) -> ToolActionResult:
    role_contracts = _selected_role_contract_payload(action.assigned_roles)
    shared_memory = read_shared_memory(tool_context)
    prompt = (
        _tool_prompt(action, role_contracts, shared_memory, "model")
        + "\n\n--- supply_qa only (overrides ToolActionResult shape above): emit SupplyQAResponse JSON, no graph primitives. ---\n"
        + JSON_WIRE_CONTRACT_V1
        + "\nMinimal SupplyQAResponse wire skeleton:\n"
        + SUPPLYQA_WIRE_EXAMPLE
        + "\n\nReturn exactly one JSON object matching SupplyQAResponse (answer, citations, mentionedEntityCandidates)."
    )
    try:
        from google.genai import Client, types as live_types

        client = Client(
            vertexai=True,
            project=os.getenv("GOOGLE_CLOUD_PROJECT"),
            location=os.getenv("GOOGLE_CLOUD_LOCATION", "global"),
        )
        response = _vertex_generate_content_resilient(
            client,
            model=INVESTIGATOR_MODEL,
            contents=prompt,
            config=live_types.GenerateContentConfig(
                temperature=0,
                response_mime_type="application/json",
                response_schema=SupplyQAResponse,
                max_output_tokens=min(TOOL_MAX_OUTPUT_TOKENS, 4096),
            ),
            allow_quota_model_fallback=True,
            used_resource_fallback_holder=[False],
        )
        parsed = getattr(response, "parsed", None)
        if isinstance(parsed, SupplyQAResponse):
            qa = parsed
        elif isinstance(parsed, dict):
            qa = SupplyQAResponse.model_validate(parsed)
        else:
            raw_qa = recover_wire_json_text(_response_json_text(response))
            qa = SupplyQAResponse.model_validate_json(raw_qa)
    except Exception as exc:
        return _tool_failure_result(action, "failed", str(exc), code="agent_failed")
    cites = [str(c).strip() for c in qa.citations if str(c).strip()][:10]
    return ToolActionResult(
        action_id=action.action_id,
        branch_id=action.branch_id,
        tool_name="supply_qa",
        status="success",
        path_assessment="still_open",
        path_assessment_note="supply_qa_discovery",
        human_summary=qa.answer,
        closure_status="unchanged",
        resolved_frontier=[],
        remaining_frontier=list(action.frontier),
        discovered_frontier=[],
        graph_fragments=[],
        learned_findings=[],
        uncovered_questions=[str(x).strip() for x in qa.mentioned_entity_candidates if str(x).strip()][:8],
        source_documents=cites,
        failure=None,
    )


def apply_supply_graph_patch(action: ToolAction, tool_context) -> ToolActionResult:
    state = tool_context.state
    raw = (action.query_text or "").strip() or (action.human_question or "").strip()
    try:
        patch = unmarshal_wire_model(SupplyGraphPatchApply, raw, context=" (apply_supply_graph_patch)")
    except Exception as exc:
        return _tool_failure_result(action, "failed", f"Invalid SupplyGraphPatchApply JSON: {exc}", code="agent_failed")
    if patch.branch_id != action.branch_id:
        return _tool_failure_result(
            action,
            "blocked",
            f"patch branchId {patch.branch_id!r} does not match action branch {action.branch_id!r}",
            missingInputs=["branchId"],
        )
    patch_key = f"{action.branch_id}:{patch.patch_id}"
    applied_keys_raw = state.get(STATE_APPLIED_SUPPLY_PATCH_KEYS)
    applied_keys: list[str] = list(applied_keys_raw) if isinstance(applied_keys_raw, list) else []
    if patch_key in applied_keys:
        return ToolActionResult(
            action_id=action.action_id,
            branch_id=action.branch_id,
            tool_name="apply_supply_graph_patch",
            status="success",
            path_assessment="still_open",
            path_assessment_note=f"patch {patch.patch_id} already applied",
            human_summary=f"Patch {patch.patch_id} was already applied for this branch (idempotent).",
            closure_status="unchanged",
            resolved_frontier=[],
            remaining_frontier=list(action.frontier),
            discovered_frontier=[],
            graph_fragments=[],
            learned_findings=[],
            uncovered_questions=[],
            source_documents=[],
            failure=None,
        )
    valid_ids = _eligible_evidence_ledger_entry_ids(state, action.branch_id)
    missing = [eid for eid in patch.evidence_ref_ids if eid not in valid_ids]
    if missing:
        return _tool_failure_result(
            action,
            "blocked",
            f"evidenceRefIds not eligible for this branch (session + branch/ancestor ledgers): {missing[:6]}",
            missingInputs=["evidenceRefIds"],
        )
    frags = list(patch.graph_fragments)
    try:
        _apply_graph_fragment_mutations(state, frags)
    except Exception as exc:
        return _tool_failure_result(action, "failed", str(exc), code="agent_failed")
    if patch_key not in applied_keys:
        applied_keys.append(patch_key)
    state[STATE_APPLIED_SUPPLY_PATCH_KEYS] = applied_keys[-400:]
    return ToolActionResult(
        action_id=action.action_id,
        branch_id=action.branch_id,
        tool_name="apply_supply_graph_patch",
        status="success",
        path_assessment="still_open",
        path_assessment_note=f"patch {patch.patch_id} applied",
        human_summary=f"Applied patch {patch.patch_id} with {len(frags)} graph fragment(s).",
        closure_status="advanced",
        resolved_frontier=[],
        remaining_frontier=list(action.frontier),
        discovered_frontier=[],
        graph_fragments=frags,
        learned_findings=[],
        uncovered_questions=[],
        source_documents=[],
        failure=None,
    )


def web_search(action: ToolAction, tool_context) -> ToolActionResult:
    return _web_search_question_to_graph(action, tool_context).model_copy(update={"tool_name": "web_search"})


_BRANCH_LEDGER_CAP = 120


def _update_branch_state(
    previous: BranchState | None,
    plan: BranchPlan,
    action: ToolAction,
    result: ToolActionResult,
    *,
    graph_fragments_override: list[GraphFragment] | None = None,
    turn: HypothesisTurn | None = None,
    explicit_status: Literal["active", "needs_followup", "blocked", "completed"] | None = None,
    explicit_stop_reason: str | None = None,
    seed_hypothesis_ledger: list[EvidenceLedgerEntry] | None = None,
    ledger_entry: EvidenceLedgerEntry | None = None,
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
    branch_fragments = (
        list(graph_fragments_override)
        if graph_fragments_override is not None
        else ((previous.graph_fragments if previous else []) + result.graph_fragments)
    )
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
    branch_ledger: list[EvidenceLedgerEntry] = list(previous.hypothesis_evidence_ledger) if previous else []
    if previous is None and seed_hypothesis_ledger:
        branch_ledger = list(seed_hypothesis_ledger)
    parent_snap: list[EvidenceLedgerEntry] = (
        list(previous.parent_ledger_snapshot)
        if previous
        else (
            list(seed_hypothesis_ledger)
            if seed_hypothesis_ledger and plan.hypothesis_relation == "independent_child"
            else []
        )
    )
    if ledger_entry is not None:
        branch_ledger = (branch_ledger + [ledger_entry])[-_BRANCH_LEDGER_CAP:]
    ledger_digest = ",".join(e.id for e in branch_ledger[-8:])
    applied_patch_ids = list(previous.applied_supply_patch_ids if previous else [])
    if result.tool_name == "apply_supply_graph_patch" and result.status == "success":
        note = (result.path_assessment_note or "").strip()
        if note.startswith("patch ") and " applied" in note:
            pid = note[len("patch ") : note.index(" applied")].strip()
            if pid and pid not in applied_patch_ids:
                applied_patch_ids.append(pid)
        applied_patch_ids = applied_patch_ids[-40:]
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
        graphFragments=branch_fragments,
        learnedFindings=((previous.learned_findings if previous else []) + result.learned_findings),
        hypothesisTurns=((previous.hypothesis_turns if previous else []) + ([turn] if turn is not None else [])),
        hypothesisEvidenceLedger=branch_ledger,
        parentLedgerSnapshot=parent_snap,
        ledgerDigest=ledger_digest,
        appliedSupplyPatchIds=applied_patch_ids,
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
        request = _request_from_state(ctx.session.state)
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        working_graph = _load_operational_graph_state(ctx.session.state)
        working_counts = graph_counts(working_graph)
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
        raw_composer_extra = ctx.session.state.pop(STATE_COMPOSER_EXTRA_EPISTEMIC_CAVEATS, None)
        composer_extras: list[str] = []
        if isinstance(raw_composer_extra, list):
            composer_extras.extend(str(x).strip() for x in raw_composer_extra if str(x).strip())
        verdict_for_compose = ctx.session.state.get(STATE_LAST_CRITIC_VERDICT)
        if isinstance(verdict_for_compose, dict):
            rec_v = str(verdict_for_compose.get("compositionRecommendation") or verdict_for_compose.get("composition_recommendation") or "")
            if rec_v in ("recommend", "conditional"):
                for c in verdict_for_compose.get("compositionConditions") or verdict_for_compose.get("composition_conditions") or []:
                    t = str(c).strip()
                    if t:
                        line = f"Critic-stated composition condition: {t}"
                        if line not in composer_extras:
                            composer_extras.append(line)
        stopped_by_cycle_limit = bool(ctx.session.state.pop(STATE_SUPERVISOR_CYCLE_LIMIT_COMPOSE, None))
        if composer_extras:
            merged_caveats = _dedupe_preserve_order(list(internal.epistemic_caveats) + composer_extras)
            internal = internal.model_copy(update={"epistemic_caveats": merged_caveats})
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
                "graphObservationCount": working_counts["graphObservationCount"],
                "entityNodeCount": working_counts["entityNodeCount"],
                "flowLegCount": working_counts["flowLegCount"],
                "processStepCount": working_counts["processStepCount"],
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
            extra_epistemic_caveats=None,
            stopped_by_supervisor_cycle_limit=stopped_by_cycle_limit,
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
        initial_pass = _working_graph_counts(ctx.session.state)["graphObservationCount"] == 0
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
                "graphObservationCount": _working_graph_counts(ctx.session.state)["graphObservationCount"],
                "openGapCount": _open_gap_count(ctx.session.state),
            },
        )
        raw_pf_inv = ctx.session.state.get(STATE_INVESTIGATION_PREFLIGHT) or {}
        try:
            pf_inv = InvestigationPreflight.model_validate(raw_pf_inv)
            preflight_dump = pf_inv.model_dump(mode="json", by_alias=True)
        except Exception:
            preflight_dump = InvestigationPreflight().model_dump(mode="json", by_alias=True)
        investigator_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "initialPass": initial_pass,
            "selectedRoles": selected_roles,
            "specializedRole": self._specialization_role,
            "activeRoleContracts": role_contracts,
            "rolePlaybook": role_contract.investigation_playbook,
            "toolUseGuidance": role_contract.tool_use_guidance,
            "structuralClosureCriteria": role_contract.structural_closure_criteria,
            "investigationPreflight": preflight_dump,
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
        plan = _normalize_investigator_plan(
            ctx.session.state,
            mission,
            self._specialization_role,
            plan,
            initial_pass=initial_pass,
            user_question=_request_from_state(ctx.session.state).question,
            corpus_document_count=investigator_context["corpusIndex"]["documentCount"],
        )
        ctx.session.state[STATE_INVESTIGATOR_PLAN] = plan.model_dump(mode="json", by_alias=True)
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
            graphDelta=graph_delta,
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
            raw_decision = HypothesisTurnDecision.model_validate(ctx.session.state.get(STATE_HYPOTHESIS_TURN_DECISION) or {})
            raw_decision_dump = json.dumps(
                raw_decision.model_dump(mode="json", by_alias=True),
                sort_keys=True,
                ensure_ascii=False,
            )
            if HYPOTHESIS_RUNTIME_STEER and HYPOTHESIS_RUNTIME_STEER_MODE not in ("off",):
                normalized = _normalize_hypothesis_turn_decision(
                    ctx.session.state,
                    branch_plan,
                    branch_state,
                    local_state,
                    latest_result,
                    raw_decision,
                )
            else:
                normalized = raw_decision
            norm_dump = json.dumps(
                normalized.model_dump(mode="json", by_alias=True),
                sort_keys=True,
                ensure_ascii=False,
            )
            if raw_decision_dump != norm_dump:
                mode = HYPOTHESIS_RUNTIME_STEER_MODE if HYPOTHESIS_RUNTIME_STEER else "off"
                ctx.session.state[STATE_LAST_HYPOTHESIS_RUNTIME_TELEMETRY] = {
                    "applied": bool(HYPOTHESIS_RUNTIME_STEER and mode == "override"),
                    "advisoryOnly": bool(HYPOTHESIS_RUNTIME_STEER and mode == "advisory"),
                    "decisionNote": (normalized.decision_note or "")[:500],
                    "branchId": branch_plan.branch_id,
                }
            if HYPOTHESIS_RUNTIME_STEER and HYPOTHESIS_RUNTIME_STEER_MODE == "advisory":
                if raw_decision_dump != norm_dump:
                    hints = list(ctx.session.state.get(STATE_HYPOTHESIS_STEER_HINT) or [])
                    hints.append(
                        {
                            "branchId": branch_plan.branch_id,
                            "llmDecision": raw_decision.decision,
                            "suggestedDecision": normalized.decision,
                            "suggestedNote": (normalized.decision_note or "")[:800],
                        }
                    )
                    ctx.session.state[STATE_HYPOTHESIS_STEER_HINT] = hints[-32:]
                decision = raw_decision
            elif HYPOTHESIS_RUNTIME_STEER and HYPOTHESIS_RUNTIME_STEER_MODE not in ("off",):
                decision = normalized
            else:
                decision = raw_decision
            ctx.session.state[STATE_HYPOTHESIS_TURN_DECISION] = decision.model_dump(mode="json", by_alias=True)
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
        seed_hypothesis_ledger: list[EvidenceLedgerEntry] = []
        if (
            previous_state is None
            and branch_plan.hypothesis_relation == "independent_child"
            and (branch_plan.parent_hypothesis_id or "").strip()
        ):
            parent_id = (branch_plan.parent_hypothesis_id or "").strip()
            for raw in tool_context.state.get(STATE_BRANCH_STATES) or []:
                try:
                    par = BranchState.model_validate(raw)
                except Exception:
                    continue
                if par.branch_id == parent_id:
                    seed_hypothesis_ledger = list(par.hypothesis_evidence_ledger)[-80:]
                    if not seed_hypothesis_ledger:
                        for row in tool_context.state.get(STATE_EVIDENCE_LEDGER) or []:
                            if not isinstance(row, dict):
                                continue
                            if str(row.get("branchId") or "").strip() != parent_id:
                                continue
                            try:
                                seed_hypothesis_ledger.append(EvidenceLedgerEntry.model_validate(row))
                            except Exception:
                                continue
                        seed_hypothesis_ledger = seed_hypothesis_ledger[-80:]
                    break

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
            ledger_entry = tool_context.last_evidence_ledger_entry
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
            local_branch_fragments = _branch_graph_fragments_from_state(local_state, branch_plan.branch_id)

            provisional_state = _update_branch_state(
                state,
                branch_plan,
                action,
                tool_result,
                graph_fragments_override=local_branch_fragments,
                seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                ledger_entry=ledger_entry,
            )
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
                    graph_fragments_override=local_branch_fragments,
                    turn=turn,
                    explicit_status="needs_followup",
                    explicit_stop_reason=decision.decision_note,
                    seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                    ledger_entry=ledger_entry,
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
                explicit_status: Literal["active", "needs_followup", "blocked", "completed"] = "needs_followup"
                explicit_stop_reason = decision.decision_note
                post_update: dict[str, Any] = {}
                if decision.hypothesis_status in {"accepted", "rejected"}:
                    explicit_status = "completed"
                if decision.hypothesis_status == "rejected":
                    post_update = {
                        "path_assessment": "rejected",
                        "path_assessment_note": decision.decision_note,
                    }
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    graph_fragments_override=local_branch_fragments,
                    turn=turn,
                    explicit_status=explicit_status,
                    explicit_stop_reason=explicit_stop_reason,
                    seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                    ledger_entry=ledger_entry,
                )
                if post_update:
                    state = state.model_copy(update=post_update)
                break

            if decision.decision == "accept_hypothesis":
                acceptance_block_reason = _acceptance_block_reason(branch_plan, provisional_state, local_state)
                if acceptance_block_reason:
                    state = _update_branch_state(
                        state,
                        branch_plan,
                        action,
                        tool_result,
                        graph_fragments_override=local_branch_fragments,
                        turn=turn,
                        explicit_status="needs_followup",
                        explicit_stop_reason=(
                            f"Acceptance blocked because the hypothesis is not structurally closed yet. "
                            f"{acceptance_block_reason}"
                        ),
                        seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                        ledger_entry=ledger_entry,
                    )
                    break
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    graph_fragments_override=local_branch_fragments,
                    turn=turn,
                    explicit_status="completed",
                    explicit_stop_reason=decision.decision_note,
                    seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                    ledger_entry=ledger_entry,
                )
                break

            if decision.decision == "reject_hypothesis":
                state = _update_branch_state(
                    state,
                    branch_plan,
                    action,
                    tool_result,
                    graph_fragments_override=local_branch_fragments,
                    turn=turn,
                    explicit_status="completed",
                    explicit_stop_reason=decision.decision_note,
                    seed_hypothesis_ledger=seed_hypothesis_ledger if state is None else None,
                    ledger_entry=ledger_entry,
                ).model_copy(
                    update={
                        "path_assessment": "rejected",
                        "path_assessment_note": decision.decision_note,
                    }
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
        tool_context.last_evidence_ledger_entry = None

        def _finalize(result: ToolActionResult) -> ToolActionResult:
            out = _ensure_learned_findings(action, result)
            tool_context.last_evidence_ledger_entry = _append_evidence_ledger(tool_context.state, action, out)
            return out

        if action.tool_name == "retrieve":
            return _finalize(retrieve(action, tool_context))
        if action.tool_name == "reason":
            return _finalize(reason(action, tool_context))
        if action.tool_name == "web_search":
            return _finalize(web_search(action, tool_context))
        if action.tool_name == "supply_qa":
            return _finalize(supply_qa(action, tool_context))
        if action.tool_name == "apply_supply_graph_patch":
            return apply_supply_graph_patch(action, tool_context)
        return _finalize(
            _tool_failure_result(action, "failed", f"Unsupported tool {action.tool_name}", code="agent_failed"),
        )


def _pack_red_team_bundle(state: dict[str, Any], mission: InvestigationMission) -> dict[str, Any]:
    rb = _investigation_reachability_counts_bundle(state)
    pf_raw = state.get(STATE_INVESTIGATION_PREFLIGHT) or {}
    try:
        preflight = InvestigationPreflight.model_validate(pf_raw)
    except Exception:
        preflight = InvestigationPreflight()
    last = state.get(STATE_LAST_AGENT_RESULT) or {}
    summary = ""
    if isinstance(last, dict):
        summary = str(last.get("summary") or "")[:800]
    wc = _working_graph_counts(state)
    return {
        "mission": mission.model_dump(mode="json", by_alias=True),
        "investigationPreflight": preflight.model_dump(mode="json", by_alias=True),
        "reachability": rb.get("reachability"),
        "reachabilityCounts": {
            "groundedCompleteChainCount": rb.get("groundedCompleteChainCount"),
            "structuralCompleteChainCount": rb.get("structuralCompleteChainCount"),
            "partialChainCount": rb.get("partialChainCount"),
            "typedBackboneChainCount": rb.get("typedBackboneChainCount"),
            "requestedCorridorSatisfied": rb.get("requestedCorridorSatisfied"),
        },
        "lastAgentResultSummary": summary,
        "graphObservationCount": wc.get("graphObservationCount", 0),
    }


class RedTeamAgent(BaseAgent):
    def __init__(self) -> None:
        super().__init__(name="red_team_agent", sub_agents=[red_team_reasoner])

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        mission = InvestigationMission.model_validate(ctx.session.state.get(STATE_CURRENT_MISSION) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        rt_ctx = _pack_red_team_bundle(ctx.session.state, mission)
        yield Event(
            author=self.name,
            actions=EventActions(state_delta={STATE_RED_TEAM_CONTEXT: rt_ctx}),
        )
        yield _runtime_log_event(
            ctx.session.state,
            actor="red_team_reasoner",
            phase="llm_request",
            message="Red team is proposing alternative frames and epistemic counterviews.",
            model=RED_TEAM_MODEL,
            basis={"missionId": mission.mission_id},
            request=rt_ctx,
        )
        async for event in self._run_named_subagent(ctx, "red_team_reasoner"):
            yield event
        raw_payload = ctx.session.state.get(STATE_RED_TEAM_LLM_PAYLOAD) or {}
        if not isinstance(raw_payload, dict):
            raw_payload = {}
        if not str(raw_payload.get("agentName") or raw_payload.get("agent_name") or "").strip():
            raw_payload = {**raw_payload, "agentName": "red_team_agent", "missionId": mission.mission_id}
        rt_full = RedTeamAgentOutput.model_validate(raw_payload)
        raw_ar = red_team_agent_output_to_agent_result(rt_full)
        result = _normalize_agent_result(raw_ar, mission, mission.assigned_roles or selected_roles)
        ctx.session.state[STATE_RED_TEAM_ARTIFACT] = rt_full.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_RED_TEAM_RESULT] = result.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_RED_TEAM_SATISFIED] = True
        yield _runtime_log_event(
            ctx.session.state,
            actor=self.name,
            phase="agent_result",
            message="Red team returned frame-broadening proposals (graph not mutated here).",
            model=RED_TEAM_MODEL,
            basis={"mission": mission.mission_id},
            result={
                "status": result.status,
                "summary": result.summary,
                "proposedBranchCount": len(rt_full.proposed_branches),
            },
        )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"RedTeamAgent cannot find sub-agent {agent_name!r}")
        async for event in agent.run_async(ctx):
            yield event


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
                "graphObservationCount": _working_graph_counts(ctx.session.state)["graphObservationCount"],
            },
        )
        role_edges = _allowed_edges_for_roles(mission.assigned_roles or selected_roles)
        gap_bridge_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "selectedRoles": selected_roles,
            "activeRoleContracts": _selected_role_contract_payload(mission.assigned_roles or selected_roles),
            "allowedGraphDeltaRelations": sorted(role_edges),
            "sharedMemory": _planner_memory_snapshot(ctx.session.state),
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
                "graphObservationCount": _working_graph_counts(ctx.session.state)["graphObservationCount"],
            },
        )
        full_mem = read_shared_memory(tool_context)
        reach_snap = _investigation_reachability_counts_bundle(ctx.session.state)["reachability"]
        q_int_raw = ctx.session.state.get(STATE_QUESTION_INTENT)
        meta_raw = ctx.session.state.get(STATE_INTENT_META)
        todos_raw = list(ctx.session.state.get(STATE_INVESTIGATION_TODOS) or [])
        raw_pf_c = ctx.session.state.get(STATE_INVESTIGATION_PREFLIGHT) or {}
        try:
            pf_c = InvestigationPreflight.model_validate(raw_pf_c)
            pf_c_dump = pf_c.model_dump(mode="json", by_alias=True)
        except Exception:
            pf_c_dump = InvestigationPreflight().model_dump(mode="json", by_alias=True)
        rt_art = ctx.session.state.get(STATE_RED_TEAM_ARTIFACT)
        rt_art_dump = rt_art if isinstance(rt_art, dict) else {}
        critic_context = pack_critic_bundle(
            mission.model_dump(mode="json", by_alias=True),
            [str(r) for r in selected_roles],
            _selected_role_contract_payload(mission.assigned_roles or selected_roles),
            read_corpus_index(tool_context),
            reach_snap,
            question_intent=q_int_raw if isinstance(q_int_raw, dict) else {},
            intent_meta=meta_raw if isinstance(meta_raw, dict) else {},
            investigation_todos_compact=todos_compact_for_critic(todos_raw),
            shared_memory_compact=_compact_shared_memory_for_critic(full_mem),
            investigation_preflight=pf_c_dump,
            red_team_artifact=rt_art_dump,
            red_team_satisfied=bool(ctx.session.state.get(STATE_RED_TEAM_SATISFIED)),
        )
        critic_context_bytes = len(json.dumps(critic_context, ensure_ascii=False, default=str))
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
            basis={"mission": mission, "criticContextBytes": critic_context_bytes},
            request=critic_context,
        )
        _critic_t0 = time.perf_counter()
        async for event in self._run_named_subagent(ctx, "critic_reasoner"):
            yield event
        _critic_ms = round((time.perf_counter() - _critic_t0) * 1000.0, 3)
        yield _runtime_log_event(
            ctx.session.state,
            actor="critic_reasoner",
            phase="llm_complete",
            message="Critic LLM call finished.",
            model=CRITIC_MODEL,
            basis={"criticLatencyMs": _critic_ms, "criticContextBytes": critic_context_bytes},
        )
        raw_payload = ctx.session.state.get(STATE_CRITIC_RESULT) or {}
        if not isinstance(raw_payload, dict):
            raw_payload = {}
        if "compositionRecommendation" not in raw_payload and "composition_recommendation" not in raw_payload:
            raw_payload = {**raw_payload, "compositionRecommendation": "do_not_recommend", "compositionConditions": []}
        critic_full = CriticAgentOutput.model_validate(raw_payload)
        raw_ar = critic_agent_output_to_agent_result(critic_full)
        result = _normalize_agent_result(raw_ar, mission, mission.assigned_roles or selected_roles)
        ctx.session.state[STATE_CRITIC_RESULT] = result.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_LAST_CRITIC_VERDICT] = {
            "investigationEpoch": int(ctx.session.state.get(STATE_INVESTIGATION_EPOCH, 0) or 0),
            "compositionRecommendation": critic_full.composition_recommendation,
            "compositionConditions": list(critic_full.composition_conditions),
        }
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
                        STATE_WORKING_GRAPH: {},
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
                        STATE_RED_TEAM_RESULT: {},
                        STATE_RED_TEAM_ARTIFACT: {},
                        STATE_RED_TEAM_LLM_PAYLOAD: {},
                        STATE_RED_TEAM_CONTEXT: {},
                        STATE_INVESTIGATION_PREFLIGHT: InvestigationPreflight().model_dump(mode="json", by_alias=True),
                        STATE_RED_TEAM_SATISFIED: False,
                        STATE_RED_TEAM_COMPOSE_WAIVED: False,
                        STATE_OPEN_GAPS: [],
                        STATE_ANSWERED_QUESTIONS: [],
                        STATE_FAILURES: [],
                        STATE_AGENT_RESULT_HISTORY: [],
                        STATE_PROGRESS_HISTORY: [],
                        STATE_GLOBAL_STAGNATION: 0,
                        STATE_TOOL_IDLE_STREAK: 0,
                        STATE_EVIDENCE_LEDGER: [],
                        STATE_APPLIED_SUPPLY_PATCH_KEYS: [],
                        STATE_HYPOTHESIS_STEER_HINT: [],
                        STATE_LOGIC_TRACE: [],
                        STATE_RUNTIME_LOG: [],
                        STATE_SUPERVISOR_CYCLE: 0,
                        STATE_LAST_CRITIC_VERDICT: {},
                        STATE_INVESTIGATION_EPOCH: 0,
                        STATE_OPEN_GAP_STREAK: 0,
                        STATE_LAST_GAP_FINGERPRINT: "",
                        STATE_RETRIEVAL_CACHE: {},
                        STATE_INVESTIGATION_BUDGET: {},
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

        if not ctx.session.state.get(STATE_QUESTION_INTENT):
            req_obj = _request_from_state(ctx.session.state)
            roles_sel = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
            intent_obj, meta_obj = extract_question_intent_bundle(req_obj, roles_sel)
            preflight_obj = normalize_investigation_preflight(req_obj, intent_obj)
            todo_seed = seed_investigation_todos(intent_obj, roles_sel, preflight_obj)
            yield Event(
                author=self.name,
                actions=EventActions(
                    state_delta={
                        STATE_QUESTION_INTENT: intent_obj.model_dump(mode="json", by_alias=True),
                        STATE_INTENT_META: meta_obj.model_dump(mode="json", by_alias=True),
                        STATE_INVESTIGATION_TODOS: todo_seed,
                        STATE_INVESTIGATION_PREFLIGHT: preflight_obj.model_dump(mode="json", by_alias=True),
                        STATE_RED_TEAM_SATISFIED: False,
                        STATE_RED_TEAM_COMPOSE_WAIVED: False,
                    }
                ),
            )
            meta_dump = meta_obj.model_dump(mode="json", by_alias=True)
            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="intent_bootstrap",
                message="QuestionIntent and investigation todos initialized.",
                basis={
                    "intentSource": meta_dump.get("intentSource"),
                    "intentLatencyMs": meta_dump.get("intentLatencyMs"),
                    "intentFallbackReason": meta_dump.get("intentFallbackReason"),
                    "intentModel": meta_dump.get("intentModel"),
                    "investigationTodoCount": len(todo_seed),
                    "corridorActive": intent_obj.corridor_active,
                },
            )

        for _ in range(MAX_SUPERVISOR_CYCLES):
            cycle = int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0) + 1
            yield Event(author=self.name, actions=EventActions(state_delta={STATE_SUPERVISOR_CYCLE: cycle}))
            _refresh_investigation_budget(ctx.session.state, cycle)
            state_seen = self._state_snapshot(ctx)
            supervisor_context: dict[str, Any] = {
                "state": state_seen,
                "investigationControl": _supervisor_preflight_context(ctx.session.state),
            }
            bp = _supervisor_budget_pressure(cycle, MAX_SUPERVISOR_CYCLES)
            if bp == "soft":
                supervisor_context["runtimeBudgetNote"] = (
                    "Cycle budget is in the soft zone: prefer critic_agent or an honest compose/stop "
                    "over launching another broad multi-theme investigation."
                )
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
            request_obj = _request_from_state(ctx.session.state)
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
            supervisor_raw_next = decision.next_agent
            policy_trace: PolicyCorrection | None = None
            policy_parts: list[PolicyCorrection] = []

            dec2, pc_list = _delegate_constraints(
                ctx.session.state, decision, cycle=cycle, user_question=request_obj.question
            )
            for pc_dc in pc_list:
                policy_parts.append(pc_dc)
            decision = dec2
            if pc_list:
                ctx.session.state[STATE_SUPERVISOR_DECISION] = decision.model_dump(mode="json", by_alias=True)

            if _supervisor_budget_pressure(cycle, MAX_SUPERVISOR_CYCLES) == "hard" and decision.next_agent not in (
                "compose",
                "stop",
                "critic_agent",
            ):
                policy_parts.append(PolicyCorrection(kind="delegate_constraint", detail="budget_hard_escalate_critic"))
                roles_fb = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [ROLE_SUPPLY_CHAIN])[:3]
                decision = SupervisorDecision(
                    cycle=cycle,
                    assessment="Supervisor cycle budget nearly exhausted; runtime escalates to critic.",
                    rationale=["runtime_budget_hard"],
                    nextAgent="critic_agent",
                    mission=InvestigationMission(
                        missionId=f"runtime-budget-{cycle}",
                        objective=(
                            "Cycle budget is nearly exhausted. Assess whether the graph warrants honest composition "
                            f"with explicit caveats, or recommend stop. Question: {request_obj.question[:400]}"
                        ),
                        assignedRoles=roles_fb,
                        groundingMode="mixed",
                    ),
                    completionRationale="",
                )
                ctx.session.state[STATE_SUPERVISOR_DECISION] = decision.model_dump(mode="json", by_alias=True)

            if decision.next_agent == "compose":
                ok_comp, why = _orchestration_policy_allow_compose(ctx.session.state)
                if not ok_comp:
                    policy_parts.append(PolicyCorrection(kind="compose_gate", detail=why))
                    if why == "red_team_required_not_satisfied":
                        decision = _decision_force_red_team_for_compose_gate(
                            cycle=cycle,
                            user_question=request_obj.question,
                            selected_roles=list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
                        )
                    else:
                        decision = _decision_force_critic_for_compose_gate(
                            cycle=cycle,
                            user_question=request_obj.question,
                            selected_roles=list(ctx.session.state.get(STATE_SELECTED_ROLES) or []),
                        )
                    ctx.session.state[STATE_SUPERVISOR_DECISION] = decision.model_dump(mode="json", by_alias=True)

            if policy_parts:
                policy_trace = PolicyCorrection(
                    kind=policy_parts[0].kind,
                    detail=" | ".join(f"{p.kind}:{p.detail}" for p in policy_parts),
                )

            yield _runtime_log_event(
                ctx.session.state,
                actor=self.name,
                phase="decision",
                message="Supervisor produced the next control decision (after runtime policy pass).",
                model=SUPERVISOR_MODEL,
                basis={"cycle": cycle, "stateSeen": state_seen, "policyCorrection": policy_trace.model_dump(mode="json", by_alias=True) if policy_trace else {}},
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
                lt_h, lt_d = _logic_trace_extras(
                    ctx,
                    supervisor_raw_next=supervisor_raw_next,
                    policy_parts=policy_parts,
                    decision=decision,
                )
                logic_entry = LogicTraceEntry(
                    cycle=cycle,
                    stateSeen=state_seen,
                    decision=decision,
                    mission=decision.mission,
                    agentResult=None,
                    resultingState=self._state_snapshot(ctx),
                    nextStepLogic=self._next_step_logic(decision, None),
                    policyCorrection=policy_trace,
                    supervisorNextAgentRaw=supervisor_raw_next,
                    delegatePolicyOutcome=lt_d,
                    hypothesisRestructure=lt_h,
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
                lt_h, lt_d = _logic_trace_extras(
                    ctx,
                    supervisor_raw_next=supervisor_raw_next,
                    policy_parts=policy_parts,
                    decision=decision,
                )
                logic_entry = LogicTraceEntry(
                    cycle=cycle,
                    stateSeen=state_seen,
                    decision=decision,
                    mission=decision.mission,
                    agentResult=None,
                    resultingState=self._state_snapshot(ctx),
                    nextStepLogic=self._next_step_logic(decision, None),
                    policyCorrection=policy_trace,
                    supervisorNextAgentRaw=supervisor_raw_next,
                    delegatePolicyOutcome=lt_d,
                    hypothesisRestructure=lt_h,
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

                lt_h, lt_d = _logic_trace_extras(
                    ctx,
                    supervisor_raw_next=supervisor_raw_next,
                    policy_parts=policy_parts,
                    decision=decision,
                )
                logic_entry = LogicTraceEntry(
                    cycle=cycle,
                    stateSeen=state_seen,
                    decision=decision,
                    mission=decision.mission,
                    agentResult=agent_result,
                    resultingState=self._state_snapshot(ctx),
                    nextStepLogic=self._next_step_logic(decision, agent_result),
                    policyCorrection=policy_trace,
                    supervisorNextAgentRaw=supervisor_raw_next,
                    delegatePolicyOutcome=lt_d,
                    hypothesisRestructure=lt_h,
                )
                yield Event(
                    author=self.name,
                    actions=EventActions(state_delta={STATE_LOGIC_TRACE: self._append_logic_entry(ctx, logic_entry)}),
                )

        if LIMIT_EXHAUSTED_STRATEGY == "compose_with_caveats":
            ok_comp, gate_detail = _orchestration_policy_allow_compose(ctx.session.state)
            red_waived_for_limit = False
            if (
                not ok_comp
                and gate_detail == "red_team_required_not_satisfied"
                and RED_TEAM_REQUIRED_FOR_BREADTH
                and not ALLOW_COMPOSE_WITHOUT_RED_TEAM
            ):
                ctx.session.state[STATE_RED_TEAM_COMPOSE_WAIVED] = True
                ok_comp, gate_detail = _orchestration_policy_allow_compose(ctx.session.state)
                red_waived_for_limit = ok_comp
            if ok_comp:
                request_lim = _request_from_state(ctx.session.state)
                extras: list[str] = [
                    "Final composition was invoked after the supervisor cycle budget was exhausted "
                    f"(AGENTLOOM_LIMIT_EXHAUSTED_STRATEGY=compose_with_caveats; compose_gate={gate_detail}).",
                    "This answer may reflect fewer investigation rounds than ideal; treat unresolved structural gaps seriously.",
                ]
                if red_waived_for_limit:
                    extras.append(
                        "Preflight required a red-team breadth pass, but cycle budget was exhausted; compose proceeds with this caveat."
                    )
                verdict_lim = ctx.session.state.get(STATE_LAST_CRITIC_VERDICT)
                if isinstance(verdict_lim, dict):
                    for c in verdict_lim.get("compositionConditions") or []:
                        cs = str(c).strip()
                        if cs:
                            extras.append(f"Critic composition condition (must remain visible to the user): {cs}")
                ctx.session.state[STATE_COMPOSER_EXTRA_EPISTEMIC_CAVEATS] = extras
                ctx.session.state[STATE_SUPERVISOR_CYCLE_LIMIT_COMPOSE] = True
                cycle_lim = int(ctx.session.state.get(STATE_SUPERVISOR_CYCLE, 0) or 0)
                state_seen_lim = self._state_snapshot(ctx)
                waivers_lim = ["supervisor_cycle_budget"]
                if red_waived_for_limit:
                    waivers_lim.append("red_team_compose")
                pc_lim = PolicyCorrection(
                    kind="delegate_constraint",
                    detail=f"limit_exhausted_compose_with_caveats|compose_gate={gate_detail}",
                )
                decision_lim = SupervisorDecision(
                    cycle=cycle_lim,
                    assessment="Runtime invoked deterministic composition after supervisor cycle budget exhaustion.",
                    rationale=[
                        "supervisor_cycle_limit_exhausted",
                        "limit_exhausted_strategy=compose_with_caveats",
                    ],
                    nextAgent="compose",
                    mission=None,
                    completionRationale="",
                )
                ctx.session.state[STATE_SUPERVISOR_DECISION] = decision_lim.model_dump(mode="json", by_alias=True)
                lt_h_lim, lt_d_lim = _logic_trace_extras(
                    ctx,
                    supervisor_raw_next=decision_lim.next_agent,
                    policy_parts=[pc_lim],
                    decision=decision_lim,
                )
                logic_lim = LogicTraceEntry(
                    cycle=cycle_lim,
                    stateSeen=state_seen_lim,
                    decision=decision_lim,
                    mission=None,
                    agentResult=None,
                    resultingState=state_seen_lim,
                    nextStepLogic="Runtime policy: one-shot compose after supervisor cycle budget exhaustion (compose_with_caveats).",
                    policyCorrection=pc_lim,
                    supervisorNextAgentRaw=decision_lim.next_agent,
                    delegatePolicyOutcome=lt_d_lim,
                    hypothesisRestructure=lt_h_lim,
                    composePolicyWaivers=waivers_lim,
                )
                yield Event(
                    author=self.name,
                    actions=EventActions(state_delta={STATE_LOGIC_TRACE: self._append_logic_entry(ctx, logic_lim)}),
                )
                yield _runtime_log_event(
                    ctx.session.state,
                    actor=self.name,
                    phase="decision",
                    message="Cycle budget exhausted; running one-shot compose with explicit epistemic caveats.",
                    basis={"cycle": cycle_lim, "limitStrategy": "compose_with_caveats", "question": request_lim.question[:200]},
                    result=decision_lim,
                )
                async for event in self._run_named_subagent(ctx, "compose"):
                    yield event
                return

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
        request = _request_from_state(ctx.session.state)
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
        ctx.session.state[STATE_RETRIEVAL_CACHE] = {}
        previous_progress = _investigation_progress_snapshot(ctx.session.state)
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

        spec_investigators = {
            "supply_chain_investigator_agent",
            "governance_and_control_investigator_agent",
            "policy_impact_investigator_agent",
        }
        has_nonempty_graph_delta = isinstance(result.graph_delta, list) and len(result.graph_delta) > 0
        if result.agent_name in spec_investigators:
            if material or has_nonempty_graph_delta:
                ctx.session.state[STATE_TOOL_IDLE_STREAK] = 0
            else:
                ctx.session.state[STATE_TOOL_IDLE_STREAK] = int(ctx.session.state.get(STATE_TOOL_IDLE_STREAK, 0) or 0) + 1
        elif material:
            ctx.session.state[STATE_TOOL_IDLE_STREAK] = 0

        if result.agent_name not in ("critic_agent", "red_team_agent"):
            ctx.session.state[STATE_INVESTIGATION_EPOCH] = int(ctx.session.state.get(STATE_INVESTIGATION_EPOCH, 0) or 0) + 1

        new_fp = _open_gap_fingerprint(ctx.session.state)
        old_fp = str(ctx.session.state.get(STATE_LAST_GAP_FINGERPRINT) or "")
        if new_fp and new_fp == old_fp:
            ctx.session.state[STATE_OPEN_GAP_STREAK] = int(ctx.session.state.get(STATE_OPEN_GAP_STREAK, 0) or 0) + 1
        else:
            ctx.session.state[STATE_OPEN_GAP_STREAK] = 0
        ctx.session.state[STATE_LAST_GAP_FINGERPRINT] = new_fp

        rb = _investigation_reachability_counts_bundle(ctx.session.state)
        merged_todos = merge_todos_after_graph_progress(
            ctx.session.state,
            todos_key=STATE_INVESTIGATION_TODOS,
            grounded_complete_chains=rb["groundedCompleteChainCount"],
            structural_complete_chains=rb["structuralCompleteChainCount"],
            partial_chains=rb["partialChainCount"],
            typed_backbone_chains=rb["typedBackboneChainCount"],
            requested_corridor_satisfied=rb["requestedCorridorSatisfied"],
        )
        todo_updates = [u.model_dump(mode="json", by_alias=True) for u in (result.investigation_todo_updates or [])]
        if todo_updates:
            merged_todos = apply_investigation_todo_updates_from_agent(merged_todos, todo_updates)
        ctx.session.state[STATE_INVESTIGATION_TODOS] = merged_todos

    def _append_logic_entry(self, ctx: InvocationContext, entry: LogicTraceEntry) -> list[dict[str, Any]]:
        trace = [LogicTraceEntry.model_validate(item) for item in (ctx.session.state.get(STATE_LOGIC_TRACE) or [])]
        trace.append(entry)
        return [item.model_dump(mode="json", by_alias=True) for item in trace]

    def _state_snapshot(self, ctx: InvocationContext) -> dict[str, Any]:
        def _trim_field(text: str, limit: int) -> str:
            t = (text or "").strip()
            if len(t) <= limit:
                return t
            return t[: max(0, limit - 1)] + "…"

        request = _request_from_state(ctx.session.state)
        graph_state = _load_operational_graph_state(ctx.session.state)
        graph = observation_projection(graph_state)
        graph_count_snapshot = graph_counts(graph_state)
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
                "summary": _trim_field(last_result.summary, SUPERVISOR_SNAPSHOT_LAST_RESULT_SUMMARY_CHARS),
                "activeRoles": last_result.active_roles,
                "gaps": [_trim_field(g, 360) for g in last_result.gaps[:8]],
                "nextQuestions": [_trim_field(q, 280) for q in last_result.next_questions[:6]],
                "handoffRecommendations": last_result.handoff_recommendations[:MAX_HANDOFF_RECOMMENDATIONS],
            }

        compact_frontier: list[dict[str, Any]]
        if cycle <= 2:
            compact_frontier = frontier_summary[:SUPERVISOR_SNAPSHOT_FRONTIER_MAX_ITEMS]
        else:
            compact_frontier = []
            for fs in frontier_summary[:SUPERVISOR_SNAPSHOT_FRONTIER_MAX_ITEMS]:
                rf_raw = fs.get("remainingFrontier") or []
                if isinstance(rf_raw, list):
                    trimmed_rf = [_trim_field(str(x), SUPERVISOR_SNAPSHOT_FRONTIER_STRING_CHARS) for x in rf_raw[:8]]
                else:
                    trimmed_rf = []
                compact_frontier.append({
                    "branchId": fs.get("branchId", ""),
                    "pathKind": fs.get("pathKind", ""),
                    "pathAssessment": _trim_field(str(fs.get("pathAssessment", "")), 320),
                    "status": fs.get("status", ""),
                    "remainingFrontier": trimmed_rf,
                    "stopReason": _trim_field(str(fs.get("stopReason", "")), 280),
                })

        snapshot: dict[str, Any] = {
            "question": request.question,
            "questionIntent": ctx.session.state.get(STATE_QUESTION_INTENT) or {},
            "intentExtractionMeta": ctx.session.state.get(STATE_INTENT_META) or {},
            "investigationTodos": list(ctx.session.state.get(STATE_INVESTIGATION_TODOS) or [])[:24],
            "selectedRoles": selected_roles,
            "defaultEpistemicMode": "mixed",
            "graphObservationCount": graph_count_snapshot["graphObservationCount"],
            "entityNodeCount": graph_count_snapshot["entityNodeCount"],
            "flowLegCount": graph_count_snapshot["flowLegCount"],
            "processStepCount": graph_count_snapshot["processStepCount"],
            "graphFragmentCount": len(fragments),
            "branchCount": len(branches),
            "openGaps": [
                _trim_field(g, 400) for g in _open_gap_texts(ctx.session.state)[:SUPERVISOR_SNAPSHOT_OPEN_GAPS_MAX]
            ],
            "failureCount": len(failures),
            "lastAgentResult": last_result_compact,
            "frontierSummary": compact_frontier,
            "candidatePaths": [item.model_dump(mode="json", by_alias=True) for item in candidate_paths[:6]],
            "hypotheses": [item.model_dump(mode="json", by_alias=True) for item in hypotheses[:8]],
            "hypothesisFrontiers": _hypothesis_frontier_signals(ctx.session.state)[:6],
            "progress": {
                "current": current_progress,
                "stagnationCount": int(ctx.session.state.get(STATE_GLOBAL_STAGNATION, 0) or 0),
                "toolIdleStreak": int(ctx.session.state.get(STATE_TOOL_IDLE_STREAK, 0) or 0),
                "history": list(ctx.session.state.get(STATE_PROGRESS_HISTORY) or [])[-3:],
            },
            "unifiedProgress": _unified_progress_report(ctx.session.state),
            "emptyTypedPayloadStreak": int(ctx.session.state.get(STATE_EMPTY_TYPED_PAYLOAD_STREAK) or 0),
            "budgetPressure": _supervisor_budget_pressure(cycle, MAX_SUPERVISOR_CYCLES),
            "investigationBudget": ctx.session.state.get(STATE_INVESTIGATION_BUDGET) or {},
            "snapshotIntegrityNote": (
                "Supervisor snapshot is intentionally lossy: long strings are trimmed and lists are capped "
                "for JSON/token safety. Treat `unifiedProgress`, `reachability`, and `openGaps` as authoritative "
                "structural signals; do not infer absence of data from omitted preview fields alone."
            ),
            "evidenceLedgerCompact": _planner_evidence_ledger_compact_slice(
                _build_evidence_ledger_compact(ctx.session.state)
            ),
        }
        meta_raw = ctx.session.state.get(STATE_INTENT_META) or {}
        if isinstance(meta_raw, dict):
            snapshot["intentSource"] = meta_raw.get("intentSource", "")
            snapshot["intentFallbackReason"] = meta_raw.get("intentFallbackReason", "")
        try:
            qi = QuestionIntent.model_validate(ctx.session.state.get(STATE_QUESTION_INTENT) or {})
            snapshot["normalizedOperationalCorridor"] = intent_to_operational_corridor_dict(qi)
        except Exception:
            snapshot["normalizedOperationalCorridor"] = {}
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
            snapshot["operationalGraph"] = graph_state.model_dump(mode="json", by_alias=True)
            snapshot["graphFragments"] = [item.model_dump(mode="json", by_alias=True) for item in fragments[-6:]]
            snapshot["branches"] = [_branch_state_for_shared_memory(item) for item in branches[-4:]]
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
        # Reachability must be visible early (small graphs too); supervisors were choosing governance without seeing chain counts.
        if cycle >= 2 and graph_count_snapshot["graphObservationCount"] >= 1:
            _sr_list_cap = SUPERVISOR_REACHABILITY_LIST_CAP if SUPERVISOR_REACHABILITY_LIST_CAP > 0 else 4
            snapshot["placeholderNodes"] = _placeholder_nodes_from_graph(graph_state)
            snapshot["placeholderResolutionHints"] = _placeholder_resolution_hints(graph_state)
            snapshot["commercialBridgeCandidates"] = _commercial_bridge_candidates(graph_state, state=ctx.session.state)[:4]
            snapshot["processGapCandidates"] = _host_local_process_gap_candidates(graph_state)[:4]
            full_reach = _graph_reachability_report(ctx.session.state)
            snapshot["reachability"] = {
                "healthSummary": full_reach["healthSummary"],
                "completeChainCount": _int_or_len_report(full_reach, "groundedCompleteChainCount", "groundedCompleteChains"),
                "groundedCompleteChainCount": _int_or_len_report(full_reach, "groundedCompleteChainCount", "groundedCompleteChains"),
                "structuralChainCount": _int_or_len_report(full_reach, "structuralCompleteChainCount", "structurallyCompleteChains"),
                "structuralCompleteChainCount": _int_or_len_report(full_reach, "structuralCompleteChainCount", "structurallyCompleteChains"),
                "weakFlowComponentsCount": int(full_reach.get("weakFlowComponentsCount") or 0),
                "requestedCorridorCompleteChainCount": _int_or_len_report(
                    full_reach,
                    "groundedRequestedCorridorChainCount",
                    "groundedRequestedCorridorChains",
                ),
                "requestedCorridorStructuralChainCount": _int_or_len_report(
                    full_reach,
                    "structuralRequestedCorridorChainCount",
                    "structuralRequestedCorridorChains",
                ),
                "partialChainCount": _int_or_len_report(full_reach, "partialChainCount", "partialChains"),
                "typedBackboneChainCount": _int_or_len_report(
                    full_reach, "typedBackboneChainCount", "typedBackboneChains"
                ),
                "maxOperationalRouteHops": int(full_reach.get("maxOperationalRouteHops") or _max_route_hops_from_report(full_reach)),
                "requestedCorridor": full_reach.get("requestedCorridor") or {},
                "requestedCorridorSatisfied": bool(full_reach.get("requestedCorridorSatisfied")),
                "completeChainEndpoints": [
                    f"{c['source']} → {c['sink']} ({c['hops']} hops)"
                    for c in (full_reach.get("groundedCompleteChains") or [])[:_sr_list_cap]
                ],
                "structuralChainEndpoints": [
                    f"{c['source']} → {c['sink']} ({c['hops']} hops)"
                    for c in (full_reach.get("structurallyCompleteChains") or [])[:_sr_list_cap]
                ],
                "partialChainBreakPoints": [
                    f"{c['sink']} [{c['sinkType']}]: {c.get('breakReason', '')}"
                    for c in (full_reach.get("partialChains") or [])[:_sr_list_cap]
                ],
                "typedBackboneEndpoints": [
                    f"{c.get('source', '')} → {c.get('sink', '')} ({c.get('hops', 0)} hops)"
                    for c in (full_reach.get("typedBackboneChains") or [])[:_sr_list_cap]
                ],
                "bridgeCandidates": [
                    f"{b['entity']} [{b['type']}]"
                    for b in full_reach["bridgeCandidates"][:_sr_list_cap]
                ],
                "routeFamilies": full_reach.get("routeFamilies") or [],
                "requestedCorridorIntentDebug": full_reach.get("requestedCorridorIntentDebug") or {},
            }
            snapshot["entityRegistrySummary"] = _entity_registry_summary_global(ctx.session.state)
            mut_log = ctx.session.state.get(STATE_GRAPH_MUTATION_LOG) or []
            if isinstance(mut_log, list) and mut_log:
                snapshot["lastGraphMutation"] = mut_log[-1]
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
        allowed_hint = ""
        if allowed_edges:
            rels = sorted(allowed_edges)
            cap = 48
            head = ", ".join(rels[:cap])
            if len(rels) > cap:
                head += f", … (+{len(rels) - cap} more)"
            allowed_hint = f" Allowed relation types for active roles: {head}."
        message = (
            "Agent returned observations with relations outside the active role contracts: "
            + ", ".join(_dedupe_preserve_order(invalid_relations))
            + allowed_hint
        )
        gaps.append(message)
        if failure is None:
            failure = FailureRecord(
                code="agent_failed",
                message=message,
                blocking=False,
                missingInputs=["role-valid relations"],
            )

    trunc = False
    trunc_detail = ""
    raw_len = len(result.graph_delta)
    if len(valid_graph) < raw_len:
        trunc = True
        trunc_detail = (
            f"graph_delta: {raw_len - len(valid_graph)} observations dropped (relation not allowed for active roles)"
        )
    answered_questions = list(result.answered_questions)
    next_questions = list(result.next_questions)
    handoff_recommendations = list(result.handoff_recommendations)
    if len(result.answered_questions) > MAX_ANSWERED_QUESTIONS or len(result.gaps) > MAX_GAPS or len(result.next_questions) > MAX_NEXT_QUESTIONS:
        trunc = True
        trunc_detail = (trunc_detail + "; " if trunc_detail else "") + "list fields capped (answeredQuestions/gaps/nextQuestions)"
    answered_questions = answered_questions[:MAX_ANSWERED_QUESTIONS]
    gaps = _dedupe_preserve_order(gaps)[:MAX_GAPS]
    next_questions = next_questions[:MAX_NEXT_QUESTIONS]
    handoff_recommendations = handoff_recommendations[:MAX_HANDOFF_RECOMMENDATIONS]

    todo_updates = list(result.investigation_todo_updates or [])[:16]

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
        truncation=trunc,
        truncationDetail=trunc_detail,
        investigationTodoUpdates=todo_updates,
    )


def _support_type_rank(support: str) -> int:
    order = ("hypothesis", "model_knowledge", "mixed", "document")
    try:
        return order.index(support)
    except ValueError:
        return 0


def _merge_observation(existing: GraphObservation, incoming: GraphObservation) -> tuple[GraphObservation, str | None]:
    """Prefer stronger evidence and higher confidence when the same edge key appears twice.

    When confidences are within ``MERGE_CONFIDENCE_EPSILON``, prefer the stronger ``supportType``
    (document > mixed > model_knowledge > hypothesis) before using confidence as a tie-breaker.

    If two different ``supportType`` strings map to the **same** rank (unknown labels), merge cannot pick
    a strict winner: downgrade to ``hypothesis`` and emit a gap for human review.

    Returns ``(merged_observation, optional_gap_text)``.
    """
    gap: str | None = None
    eps = MERGE_CONFIDENCE_EPSILON
    ci, ce = float(incoming.confidence), float(existing.confidence)
    if abs(ci - ce) > eps:
        if ci > ce:
            chosen, other = incoming, existing
        else:
            chosen, other = existing, incoming
    else:
        ri, re = _support_type_rank(incoming.support_type), _support_type_rank(existing.support_type)
        if ri != re:
            chosen, other = (incoming, existing) if ri > re else (existing, incoming)
        elif ci >= ce:
            chosen, other = incoming, existing
        else:
            chosen, other = existing, incoming
    prov = _dedupe_preserve_order(list(chosen.provenance) + list(other.provenance))
    note = chosen.support_note or other.support_note
    if other.support_note and other.support_note not in (note or ""):
        note = f"{note} | merged: {other.support_note}" if note else other.support_note
    merged = chosen.model_copy(
        update={
            "confidence": max(float(existing.confidence), float(incoming.confidence)),
            "provenance": prov[:24],
            "support_note": note[:2000] if note else "",
        }
    )
    if (
        existing.support_type != incoming.support_type
        and _support_type_rank(existing.support_type) == _support_type_rank(incoming.support_type)
    ):
        merged = merged.model_copy(
            update={
                "support_type": "hypothesis",
                "confidence": min(float(merged.confidence), 0.85),
                "support_note": (
                    (merged.support_note or "").strip()
                    + " [runtime_merge_conflict: supportType labels disagree at equal rank; downgraded to hypothesis.]"
                ).strip()[:2000],
            }
        )
        gap = (
            "[runtime_merge_conflict] Same-edge observations used inconsistent supportType labels that map to the same "
            "evidence rank; merged as hypothesis pending human review."
        )
    return merged, gap


def _logic_trace_has_red_team_compose_waiver(state: dict[str, Any]) -> bool:
    """True if logic_trace records an explicit compose waiver for red-team (audit path)."""
    for raw in state.get(STATE_LOGIC_TRACE) or []:
        if not isinstance(raw, dict):
            continue
        waivers = raw.get("composePolicyWaivers") or raw.get("compose_policy_waivers") or []
        if not isinstance(waivers, list):
            continue
        if any(str(x).strip() == "red_team_compose" for x in waivers):
            return True
    return False


def _orchestration_policy_allow_compose(state: dict[str, Any]) -> tuple[bool, str]:
    if os.environ.get("AGENTLOOM_TESTING", "").strip() in ("1", "true", "yes"):
        return True, "testing_bypass"
    if not ALLOW_COMPOSE_WITHOUT_RED_TEAM:
        waive_rt = bool(state.get(STATE_RED_TEAM_COMPOSE_WAIVED)) or _logic_trace_has_red_team_compose_waiver(state)
        if not waive_rt:
            raw_pf = state.get(STATE_INVESTIGATION_PREFLIGHT) or {}
            try:
                pf = InvestigationPreflight.model_validate(raw_pf)
            except Exception:
                pf = InvestigationPreflight()
            rb = _investigation_reachability_counts_bundle(state)
            reach = rb.get("reachability") or {}
            if (
                preflight_requires_red_team_pass(pf)
                and red_team_frame_eligible(reach)
                and not bool(state.get(STATE_RED_TEAM_SATISFIED))
            ):
                return False, "red_team_required_not_satisfied"
    if COMPOSE_REQUIRES_TODOS_SATISFIED:
        todos = list(state.get(STATE_INVESTIGATION_TODOS) or [])
        if todos and not required_todos_satisfied(todos):
            if COMPOSE_ALLOW_WITH_TYPED_BACKBONE:
                reach = _graph_reachability_report(state)
                if int(reach.get("typedBackboneChainCount") or 0) >= 1:
                    pass
                else:
                    return False, "investigation_todos_not_satisfied"
            else:
                return False, "investigation_todos_not_satisfied"
    verdict = state.get(STATE_LAST_CRITIC_VERDICT)
    if not isinstance(verdict, dict):
        return False, "no_critic_verdict"
    rec = str(verdict.get("compositionRecommendation") or verdict.get("composition_recommendation") or "")
    if rec == "do_not_recommend":
        return False, "critic_do_not_recommend"
    if rec not in ("recommend", "conditional"):
        return False, f"invalid_or_missing_composition_recommendation:{rec!r}"
    if rec == "conditional":
        conds = verdict.get("compositionConditions") or verdict.get("composition_conditions") or []
        if not isinstance(conds, list) or not any(str(x).strip() for x in conds):
            return False, "conditional_requires_non_empty_composition_conditions"
    epoch_v = int(verdict.get("investigationEpoch", -1))
    epoch_cur = int(state.get(STATE_INVESTIGATION_EPOCH, 0) or 0)
    if epoch_v != epoch_cur:
        return False, f"stale_critic_verdict_epoch_verdict={epoch_v}_current={epoch_cur}"
    return True, "ok"


def _open_gap_fingerprint(state: dict[str, Any]) -> str:
    texts = _open_gap_texts(state)[:12]
    return "|".join(texts)


def _unified_progress_report(state: dict[str, Any]) -> dict[str, Any]:
    snap = _investigation_progress_snapshot(state)
    branches = [BranchState.model_validate(item) for item in (state.get(STATE_BRANCH_STATES) or [])]
    stag_vals = [b.stagnation_count for b in branches if b.status in {"active", "needs_followup", "blocked"}]
    hist = list(state.get(STATE_PROGRESS_HISTORY) or [])
    last = hist[-1] if hist else {}
    if isinstance(last, dict) and last:
        material_progress_last_step = (
            f"lastDelegate={last.get('agentName')!s} missionId={last.get('missionId')!s} "
            f"materialProgress={last.get('materialProgress')!s} stagnationAfter={last.get('stagnationCount')!s} "
            f"openGapCount={last.get('openGapCount')!s}"
        )
    else:
        material_progress_last_step = "no_progress_history_yet"
    return {
        "progressSnapshot": snap,
        "globalStagnation": int(state.get(STATE_GLOBAL_STAGNATION, 0) or 0),
        "branchStagnationMax": max(stag_vals) if stag_vals else 0,
        "branchStagnationMean": sum(stag_vals) / len(stag_vals) if stag_vals else 0.0,
        "openGapStreak": int(state.get(STATE_OPEN_GAP_STREAK, 0) or 0),
        "investigationEpoch": int(state.get(STATE_INVESTIGATION_EPOCH, 0) or 0),
        "materialProgressLastStep": material_progress_last_step,
    }


def _refresh_investigation_budget(state: dict[str, Any], cycle: int) -> None:
    """Single session object for supervisor budget (plan C2); token/tool approximations reserved for future wiring."""
    state[STATE_INVESTIGATION_BUDGET] = {
        "supervisorCyclesUsed": int(cycle),
        "maxSupervisorCycles": int(MAX_SUPERVISOR_CYCLES),
        "estimatedTokenBudget": None,
        "toolCallsApprox": None,
    }


def _supervisor_budget_pressure(cycle: int, max_cycles: int) -> Literal["none", "soft", "hard"]:
    if max_cycles <= 0:
        return "hard"
    remaining = max_cycles - cycle + 1
    if remaining <= 1:
        return "hard"
    if remaining / max_cycles <= BUDGET_SOFT_FRACTION:
        return "soft"
    return "none"


def _last_delegate_agent_names_from_trace(state: dict[str, Any], n: int = 2) -> list[str]:
    lt = state.get(STATE_LOGIC_TRACE) or []
    names: list[str] = []
    for entry in reversed(lt):
        if not isinstance(entry, dict):
            continue
        d = entry.get("decision") or {}
        na = d.get("nextAgent") or d.get("next_agent")
        if na in _SUPERVISOR_DELEGATE_AGENTS and na != "critic_agent":
            names.append(str(na))
            if len(names) >= n:
                break
    return names


_SUPERVISOR_TRACE_DELEGATES = frozenset(
    {
        "supply_chain_investigator_agent",
        "governance_and_control_investigator_agent",
        "policy_impact_investigator_agent",
        "gap_bridge_agent",
        "critic_agent",
        "red_team_agent",
    }
)


def _recent_supervisor_delegate_streak(state: dict[str, Any], agent_name: str, n: int) -> bool:
    """True when the last n logic_trace delegations (newest first) each used agent_name."""
    lt = state.get(STATE_LOGIC_TRACE) or []
    seq: list[str] = []
    for entry in reversed(lt):
        if not isinstance(entry, dict):
            continue
        d = entry.get("decision") or {}
        na = str(d.get("nextAgent") or d.get("next_agent") or "")
        if na not in _SUPERVISOR_TRACE_DELEGATES:
            continue
        seq.append(na)
        if len(seq) >= n:
            break
    return len(seq) >= n and all(x == agent_name for x in seq)


def _graph_has_placeholder_debt(state: dict[str, Any]) -> bool:
    return bool(_placeholder_nodes_from_graph(_load_operational_graph_state(state)))


def _last_flow_grounding_gain_from_trace(state: dict[str, Any], n: int = 2) -> bool:
    """True if any of the last n non-critic delegate results added document/mixed flow observations."""
    lt = state.get(STATE_LOGIC_TRACE) or []
    flow_edges = frozenset()
    sc = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    if sc is not None:
        flow_edges = frozenset(sc.path_carried_edges or []) | frozenset(sc.allowed_edge_types or [])
    count = 0
    for entry in reversed(lt):
        if not isinstance(entry, dict):
            continue
        d = entry.get("decision") or {}
        na = d.get("nextAgent") or d.get("next_agent")
        if na not in (
            "supply_chain_investigator_agent",
            "gap_bridge_agent",
        ):
            continue
        ar = entry.get("agentResult") or entry.get("agent_result")
        if not isinstance(ar, dict):
            continue
        gd = ar.get("graphDelta") or ar.get("graph_delta") or []
        for raw in gd:
            if not isinstance(raw, dict):
                continue
            st = raw.get("supportType") or raw.get("support_type")
            rel = raw.get("relation") or ""
            if st in ("document", "mixed") and rel in flow_edges:
                return True
        count += 1
        if count >= n:
            break
    return False


def _reachability_complete_counts(report: dict[str, Any]) -> tuple[int, int]:
    grounded = _int_or_len_report(report, "groundedCompleteChainCount", "groundedCompleteChains")
    structural = _int_or_len_report(report, "structuralCompleteChainCount", "structurallyCompleteChains")
    return grounded, structural


def _force_supply_chain_investigation_decision(
    decision: SupervisorDecision,
    *,
    cycle: int,
    objective: str,
    rationale_line: str,
) -> SupervisorDecision:
    updated = decision.model_copy(
        update={
            "next_agent": "supply_chain_investigator_agent",
            "rationale": list(decision.rationale) + [rationale_line],
        }
    )
    mission_payload = (
        decision.mission.model_dump(mode="json", by_alias=True)
        if decision.mission is not None
        else {
            "missionId": f"runtime-supply-chain-{cycle}",
            "objective": objective,
            "assignedRoles": [ROLE_SUPPLY_CHAIN],
            "groundingMode": "mixed",
            "focus": [],
            "successCriteria": [],
        }
    )
    mission_payload["assignedRoles"] = [ROLE_SUPPLY_CHAIN]
    mission_payload["missionId"] = mission_payload.get("missionId") or f"runtime-supply-chain-{cycle}"
    mission_payload["objective"] = objective
    mission_payload["groundingMode"] = "mixed"
    return updated.model_copy(update={"mission": InvestigationMission.model_validate(mission_payload)})


def _delegate_constraints(
    state: dict[str, Any],
    decision: SupervisorDecision,
    *,
    cycle: int,
    user_question: str,
) -> tuple[SupervisorDecision, list[PolicyCorrection]]:
    """Enforce flow-first delegation; return possibly rewritten decision + policy corrections."""
    from agentloom.delegation_policy import apply_delegate_constraints

    return apply_delegate_constraints(state, decision, cycle=cycle, user_question=user_question)


def _decision_force_critic_for_compose_gate(
    *,
    cycle: int,
    user_question: str,
    selected_roles: list[str],
) -> SupervisorDecision:
    m = InvestigationMission(
        missionId=f"runtime-compose-gate-{cycle}",
        objective=(
            "Supervisor requested composition but the last critic verdict is missing or stale. "
            f"Assess sufficiency for composition now. Question: {user_question[:400]}"
        ),
        assignedRoles=_dedupe_preserve_order([r for r in selected_roles if r in INVESTIGATION_ROLES_LITERALS])[:3]
        or [ROLE_SUPPLY_CHAIN],
        groundingMode="mixed",
    )
    return SupervisorDecision(
        cycle=cycle,
        assessment="Compose blocked until critic endorses composition.",
        rationale=["runtime_compose_gate"],
        nextAgent="critic_agent",
        mission=m,
        completionRationale="",
    )


def _decision_force_red_team_for_compose_gate(
    *,
    cycle: int,
    user_question: str,
    selected_roles: list[str],
) -> SupervisorDecision:
    m = InvestigationMission(
        missionId=f"runtime-compose-gate-redteam-{cycle}",
        objective=(
            "Composition was requested but investigation preflight requires a red-team breadth pass before compose. "
            "Propose alternative route families, jurisdictions, modalities, other sources/sinks, adjacent regions, and "
            f"epistemic counterviews consistent with the current working graph (proposal-only; no silent graph merge). "
            f"Question: {user_question[:400]}"
        ),
        assignedRoles=_dedupe_preserve_order([r for r in selected_roles if r in INVESTIGATION_ROLES_LITERALS])[:3]
        or [ROLE_SUPPLY_CHAIN],
        groundingMode="mixed",
    )
    return SupervisorDecision(
        cycle=cycle,
        assessment="Compose blocked until red_team_agent runs for breadth preflight.",
        rationale=["runtime_compose_gate_red_team"],
        nextAgent="red_team_agent",
        mission=m,
        completionRationale="",
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
    graph_state = _load_operational_graph_state(state)
    ops = _investigation_reachability_ops_bundle(state, graph_state)
    return {
        "question": shared["question"],
        "questionIntent": shared.get("questionIntent") or {},
        "intentExtractionMeta": shared.get("intentExtractionMeta") or {},
        "investigationTodos": shared.get("investigationTodos") or [],
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
        "placeholderNodes": _placeholder_nodes_from_graph(graph_state),
        **ops,
        "operationalGraph": shared["operationalGraph"],
        "operationalCounts": shared["operationalCounts"],
        "entityRegistrySummary": _entity_registry_summary_global(state),
        "evidenceLedgerCompact": _planner_evidence_ledger_compact_slice(shared.get("evidenceLedgerCompact")),
    }


def _planner_evidence_ledger_compact_slice(compact: Any) -> dict[str, Any]:
    if not isinstance(compact, dict):
        return {}
    out = dict(compact)
    bb = out.get("byBranch")
    if isinstance(bb, list) and len(bb) > _LEDGER_COMPACT_PLANNER_BY_BRANCH:
        out["byBranch"] = bb[-_LEDGER_COMPACT_PLANNER_BY_BRANCH :]
    return out


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
        STATE_WORKING_GRAPH: graph_state_to_payload(graph_state_from_fragments(graph_fragments)),
        STATE_BRANCH_STATES: [item.model_dump(mode="json", by_alias=True) for item in branch_states],
        STATE_REPLACED_NODE_IDS: list(state.get(STATE_REPLACED_NODE_IDS) or []),
        STATE_LEARNED_FINDINGS: [item.model_dump(mode="json", by_alias=True) for item in findings],
    }


def _is_placeholder_node(node_id: str, label: str, *, support_type: str = "") -> bool:
    node_id_text = (node_id or "").strip()
    label_text = (label or "").strip()
    text = f"{node_id_text} {label_text}".strip()
    if any(pattern.search(text) for pattern in _PLACEHOLDER_LABEL_PATTERNS):
        return True
    if node_id_text and _GENERIC_PLACEHOLDER_NODE_ID_PATTERN.search(node_id_text):
        if not label_text or label_text.lower() == node_id_text.lower() or _label_looks_like_placeholder(label_text):
            return True
    if support_type == "hypothesis" and label and _PLACEHOLDER_HYPOTHESIS_LABEL_HINTS.search(label):
        return True
    return False


def _is_unresolved_aggregate_node(
    node_id: str,
    label: str,
    *,
    entity_type: str = "",
    support_type: str = "",
) -> bool:
    if _is_placeholder_node(node_id, label, support_type=support_type):
        return True
    if entity_type not in {"FACILITY", "ORGANIZATION"}:
        return False
    text = f"{node_id} {label}".strip()
    if any(pattern.search(text) for pattern in _AGGREGATE_INTERMEDIARY_LABEL_PATTERNS):
        return True
    if support_type not in {"model_knowledge", "hypothesis"}:
        return False
    return bool(
        re.search(r"\b(?:global|regional|international|third[- ]party|merchant|spot|generic)\b", text, re.IGNORECASE)
        and re.search(r"\b(?:market|pool|hub|converter|processor|trader|merchant)\b", text, re.IGNORECASE)
    )


def _placeholder_nodes_from_graph(graph: OperationalGraphState | list[GraphObservation] | dict[str, Any]) -> list[str]:
    graph_state = graph_state_from_raw(graph) if isinstance(graph, dict) else (
        graph if isinstance(graph, OperationalGraphState) else graph_state_from_observations(graph)
    )
    placeholders: list[str] = []
    for node in graph_state.entity_nodes:
        if _is_placeholder_node(node.node_id, node.label, support_type=node.support_type):
            placeholders.append(node.label or node.node_id)
    return _dedupe_preserve_order(placeholders)[:8]


def _placeholder_resolution_hints(graph: OperationalGraphState | list[GraphObservation] | dict[str, Any]) -> list[str]:
    """Deterministic, graph-only hints for resolving placeholder endpoints (no invented names)."""
    graph_state = graph_state_from_raw(graph) if isinstance(graph, dict) else (
        graph if isinstance(graph, OperationalGraphState) else graph_state_from_observations(graph)
    )
    hints: list[str] = []
    for node in graph_state.entity_nodes:
        if not _is_placeholder_node(node.node_id, node.label, support_type=node.support_type):
            continue
        disp = (node.label or node.node_id or "").strip() or "unnamed"
        oid = (node.node_id or "").strip()
        old_part = (
            f'patchMode replace_entity/replace_fragment with oldId "{oid}"'
            if oid else
            "patchMode replace_entity/replace_fragment using the nodeId as oldId"
        )
        hints.append(
            f'Resolve placeholder node "{disp}" as a concrete ORG/FACILITY/DEPOSIT/PRODUCT allowed by role contracts; '
            f"consider {old_part} when the graph already references that slot."
        )
    return _dedupe_preserve_order(hints)[:8]


def _aggregate_intermediary_nodes_from_graph(
    graph: OperationalGraphState | list[GraphObservation] | dict[str, Any],
) -> list[dict[str, str]]:
    graph_state = graph_state_from_raw(graph) if isinstance(graph, dict) else (
        graph if isinstance(graph, OperationalGraphState) else graph_state_from_observations(graph)
    )
    unresolved: list[dict[str, str]] = []
    seen: set[str] = set()
    for node in graph_state.entity_nodes:
        if not _is_unresolved_aggregate_node(
            node.node_id,
            node.label,
            entity_type=node.entity_type,
            support_type=node.support_type,
        ):
            continue
        key = node.node_id or node.label
        if key in seen:
            continue
        seen.add(key)
        unresolved.append(
            {
                "nodeId": node.node_id,
                "label": node.label or node.node_id,
                "entityType": node.entity_type,
                "supportType": node.support_type,
                "reason": "Aggregate intermediary stands in for a real operator, facility, trader, or jurisdictional handoff.",
            }
        )
    return unresolved[:8]


def _is_detail_frontier_text(text: str) -> bool:
    return bool(_DETAIL_FRONTIER_SIGNALS.search(text or ""))


def _structural_frontier_items(frontier: list[str]) -> list[str]:
    return [item for item in _dedupe_preserve_order(frontier) if item and not _is_detail_frontier_text(item)]


def _acceptance_block_reason(
    plan: BranchPlan,
    branch_state: BranchState,
    local_state: dict[str, Any],
) -> str:
    graph_state = _load_operational_graph_state(local_state)
    reachability = _graph_reachability_report_for_graph(graph_state, state=local_state)
    placeholders = _placeholder_nodes_from_graph(graph_state)
    if placeholders:
        return f"Broad placeholders remain in the hypothesis graph: {', '.join(placeholders[:3])}."
    aggregate_nodes = _aggregate_intermediary_nodes_from_graph(graph_state)
    if aggregate_nodes:
        labels = [str(item.get("label") or item.get("nodeId") or "").strip() for item in aggregate_nodes]
        labels = [item for item in labels if item]
        if labels:
            return f"Unresolved aggregate intermediaries remain in the hypothesis graph: {', '.join(labels[:3])}."
    if plan.hypothesis_relation != "root" and plan.path_kind != "overview":
        return ""
    complete_count, structural_count = _reachability_complete_counts(reachability)
    if complete_count + structural_count <= 0:
        partial = reachability.get("partialChains") or []
        if partial:
            first = partial[0]
            return (
                "No business-complete operational chain exists yet. "
                + str(first.get("breakReason") or reachability.get("healthSummary") or "").strip()
            )
        return "No business-complete operational chain exists yet."
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
    graph_state = _load_operational_graph_state(local_state)
    graph = observation_projection(graph_state)
    ops = _investigation_reachability_ops_bundle(local_state, graph_state)
    reachability = ops["reachability"]
    fragments = [GraphFragment.model_validate(item) for item in (local_state.get(STATE_GRAPH_FRAGMENTS) or [])]
    findings = [LearnedFinding.model_validate(item) for item in (local_state.get(STATE_LEARNED_FINDINGS) or [])]
    turns = list(branch_state.hypothesis_turns if branch_state is not None else [])
    active_role = plan.assigned_roles[0] if len(plan.assigned_roles) == 1 else None
    role_contract = _role_contract(active_role) if active_role is not None else None
    return {
        "originalQuestion": _request_from_state(state).question,
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
            "graphObservationCount": graph_counts(graph_state)["graphObservationCount"],
            "operationalCounts": graph_counts(graph_state),
            "operationalGraph": graph_state.model_dump(mode="json", by_alias=True),
            "graphObservations": [item.model_dump(mode="json", by_alias=True) for item in graph[-18:]],
            "graphFragmentCount": len(fragments),
            "graphFragments": [item.model_dump(mode="json", by_alias=True) for item in fragments[-8:]],
            "placeholderNodes": _placeholder_nodes_from_graph(graph_state),
            "reachability": reachability,
            "requestedCorridor": ops.get("requestedCorridor") or {},
            "commercialBridgeCandidates": ops.get("commercialBridgeCandidates") or [],
            "processGapCandidates": ops.get("processGapCandidates") or [],
            "aggregateIntermediaryNodes": ops.get("aggregateIntermediaryNodes") or [],
            "alternativeRouteFamilyCandidates": ops.get("alternativeRouteFamilyCandidates") or [],
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
        "entityRegistrySummary": _entity_registry_summary_for_branch(state, plan.branch_id),
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
    output_schema=CriticAgentOutput,
    output_key=STATE_CRITIC_RESULT,
    context_key=STATE_CRITIC_CONTEXT,
)

red_team_reasoner = StructuredOutputAgent(
    model=RED_TEAM_MODEL,
    name="red_team_reasoner",
    description="Proposes alternative frames, route families, and epistemic counterviews without mutating the graph.",
    instruction=RED_TEAM_INSTRUCTION,
    generate_content_config=RED_TEAM_CONFIG,
    output_schema=RedTeamAgentOutput,
    output_key=STATE_RED_TEAM_LLM_PAYLOAD,
    context_key=STATE_RED_TEAM_CONTEXT,
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
        RedTeamAgent(),
        DeterministicComposerAgent(),
    ],
)
