from __future__ import annotations

# Optional Vertex ADK / GenAI SDKs are usually installed in the dedicated venv.
# pyright: reportMissingImports=false

import json
import os
import re
import asyncio
from collections.abc import AsyncGenerator
from typing import Any, Literal

from google.adk.agents import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event, EventActions
from google.genai import types as genai_types
import pydantic
from pydantic import BaseModel, ConfigDict, Field

from agentloom.composer import build_final_chain_summary
from agentloom.contracts import (
    AgentContract,
    AgentLoomRequest,
    AgentResult,
    BranchPlan,
    BranchState,
    FailureRecord,
    GraphFragment,
    GraphObservation,
    InvestigationMission,
    InvestigationRole,
    LogicTraceEntry,
    RoleContract,
    RoleSelection,
    RuntimeLogEntry,
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
STATE_BRANCH_STATES = "branch_states"
STATE_OPEN_GAPS = "open_gaps"
STATE_ANSWERED_QUESTIONS = "answered_questions"
STATE_FAILURES = "failures"
STATE_AGENT_RESULT_HISTORY = "agent_result_history"
STATE_LAST_AGENT_RESULT = "last_agent_result"
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
STATE_INVESTIGATOR_EVIDENCE = "investigator_evidence"
STATE_GAP_BRIDGE_CONTEXT = "gap_bridge_context"
STATE_GAP_BRIDGE_RESULT = "gap_bridge_agent_result"
STATE_CRITIC_CONTEXT = "critic_context"
STATE_INVESTIGATOR_RESULT = "investigator_agent_result"
STATE_INVESTIGATOR_SYNTHESIS = "investigator_synthesis"
STATE_INVESTIGATOR_SYNTHESIS_CONTEXT = "investigator_synthesis_context"
STATE_CRITIC_RESULT = "critic_agent_result"

RESULT_KEY_BY_AGENT = {
    "investigator_agent": STATE_INVESTIGATOR_RESULT,
    "gap_bridge_agent": STATE_GAP_BRIDGE_RESULT,
    "critic_agent": STATE_CRITIC_RESULT,
}

_MODEL_ALIASES = {
    "vertex-gemini-3-flash-preview": "gemini-3-flash-preview",
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
                    result = self._output_schema.model_validate(parsed)
                else:
                    result = self._output_schema.model_validate_json(_extract_json_object(_response_json_text(response)))
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


def _resolve_model(*candidates: str, fallback: str) -> str:
    for candidate in candidates:
        normalized = _MODEL_ALIASES.get(candidate.strip(), candidate.strip()) if candidate else ""
        if normalized:
            return normalized
    return fallback


SUPERVISOR_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_SUPERVISOR_MODEL", ""),
    os.getenv("AGENTMESH_MODEL_FLASH_REASONING", ""),
    fallback="gemini-3-flash-preview",
)
INVESTIGATOR_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_WORKER_MODEL", ""),
    os.getenv("AGENTMESH_MODEL_FLASH_REASONING", ""),
    fallback="gemini-3-flash-preview",
)
CRITIC_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_CRITIC_MODEL", ""),
    os.getenv("AGENTMESH_MODEL_PRO", ""),
    fallback="gemini-2.5-pro",
)
GROUNDING_MODEL = _resolve_model(
    os.getenv("AGENTLOOM_GROUNDING_MODEL", ""),
    os.getenv("AGENTMESH_MODEL_FLASH_REASONING", ""),
    fallback="gemini-3-flash-preview",
)
RESOURCE_EXHAUSTION_FALLBACK_MODEL = os.getenv(
    "AGENTLOOM_RESOURCE_EXHAUSTION_FALLBACK_MODEL",
    "gemini-2.5-flash",
).strip()
MAX_SUPERVISOR_CYCLES = int(os.getenv("AGENTLOOM_MAX_SUPERVISOR_CYCLES", "10"))
ROLE_CONTRACTS: dict[InvestigationRole, RoleContract] = ROLE_CONTRACTS_BY_ROLE

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

TOOL_MAX_OUTPUT_TOKENS = int(os.getenv("AGENTLOOM_TOOL_MAX_OUTPUT_TOKENS", "8192"))
RETRIEVAL_MAX_OUTPUT_TOKENS = int(os.getenv("AGENTLOOM_RETRIEVAL_MAX_OUTPUT_TOKENS", "4096"))

GRAPH_OBSERVATION_CONTRACT = """
GraphObservation contract:
- observationId: string. Stable within the tool result.
- subjectId: string. Stable node id such as "company:tsingshan-group" or "product:nickel-sulfate".
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
- Do not model countries or regions as if they directly produce, transform, supply, or assemble products.
- Geography belongs in overlay edges such as LOCATED_IN or OPERATES_IN.
- Deposits are geological sources. Use EXTRACTS_FROM from a FACILITY or ORGANIZATION to a DEPOSIT.
- Facilities or organizations produce and supply products. Use PRODUCES or SUPPLIES_TO from FACILITY/ORGANIZATION to PRODUCT or another operational node.
- Product transformation steps must be PRODUCT --TRANSFORMS_INTO--> PRODUCT.
- Product usage or assembly steps must not terminate in a country or region.
- End-of-chain geography must be represented as FACILITY/ORGANIZATION --LOCATED_IN--> GEO_REGION, not PRODUCT --ASSEMBLES_INTO--> GEO_REGION.
- If the exact facility is unknown, prefer a generic facility node like "Nickel refinery" or "Battery cell plant" over a country pretending to act.
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

Use this profile diagnostically:
- if a layer is missing, say so in uncoveredQuestions
- if only a holistic re-query makes sense, ask one whole-chain uncovered question
- if multiple alternatives exist but only one is known, ask for the missing alternative path explicitly
"""

GRAPH_FRAGMENT_CONTRACT = """
GraphFragment contract:
- fragmentId: string. Stable within the branch.
- fragmentType: one of ["overview_chain", "chain_fragment", "alternative_chain", "ownership_overlay", "policy_overlay", "hypothesis"].
- title: short human-readable title.
- humanQuestion: the exact human-readable question this fragment answers.
- humanSummary: short human-readable summary of what was found.
- branchId: string. Must match the branch being answered.
- alternativeGroupId: string. Use when multiple fragments belong to one alternative-path family.
- startNodeId: optional string. The fragment entry node when known.
- endNodeId: optional string. The fragment exit node when known.
- observations: array of GraphObservation.
- completeness: one of ["partial", "complete", "hypothesis"].
- qualityScore: number between 0 and 1.
"""

TOOL_ACTION_RESULT_CONTRACT = """
ToolActionResult contract:
- actionId: string. Must equal the incoming actionId.
- branchId: string. Must equal the incoming branchId.
- toolName: string. Must equal the incoming toolName.
- status: one of ["success", "partial", "blocked", "failed"].
- humanSummary: short human-readable result summary.
- closureStatus: one of ["advanced", "closed", "unchanged", "redirected"].
- resolvedFrontier: string array. Which missing links or unknowns this action resolved.
- remainingFrontier: string array. Which missing links or unknowns still remain after this action.
- discoveredFrontier: string array. New missing links or unknowns discovered while answering.
- graphFragments: array of GraphFragment. Use [] when blocked or failed.
- uncoveredQuestions: string array of unresolved questions that remain after this action.
- sourceDocuments: string array of source URIs or document ids used in the answer.
- failure: null or FailureRecord.

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
  "toolName": "vertex_question_to_graph",
  "status": "success",
  "humanSummary": "Found a grounded supply-chain fragment from Indonesian nickel mining through HPAL processing into battery-grade nickel sulfate.",
  "closureStatus": "advanced",
  "resolvedFrontier": [
    "Confirmed a facility-aware laterite-to-MHP path."
  ],
  "remainingFrontier": [
    "Identify the facility or organization that converts MHP into nickel sulfate."
  ],
  "discoveredFrontier": [
    "Determine whether the downstream receiver is in Germany or an intermediate European facility."
  ],
  "graphFragments": [
    {
      "fragmentId": "branch-1-f1",
      "fragmentType": "chain_fragment",
      "title": "Indonesia laterite to nickel sulfate",
      "humanQuestion": "What sits between Indonesian laterite mining and battery-grade nickel sulfate?",
      "humanSummary": "The main grounded path runs from laterite ore through HPAL to mixed hydroxide precipitate and then to nickel sulfate.",
      "branchId": "branch-1",
      "alternativeGroupId": "laterite_routes",
      "startNodeId": "deposit:morowali-laterite-deposit",
      "endNodeId": "product:nickel-sulfate",
      "observations": [
        {
          "observationId": "branch-1-f1-o1",
          "subjectId": "facility:morowali-mining-complex",
          "subject": "Morowali mining complex",
          "subjectType": "FACILITY",
          "relation": "EXTRACTS_FROM",
          "objectId": "deposit:morowali-laterite-deposit",
          "object": "Morowali laterite deposit",
          "objectType": "DEPOSIT",
          "qualifiers": ["nickel laterite"],
          "confidence": 0.79,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence ties the mining facility to the laterite deposit.",
          "branchId": "branch-1",
          "alternativeGroupId": "laterite_routes"
        },
        {
          "observationId": "branch-1-f1-o2",
          "subjectId": "facility:morowali-mining-complex",
          "subject": "Morowali mining complex",
          "subjectType": "FACILITY",
          "relation": "PRODUCES",
          "objectId": "product:nickel-laterite-ore",
          "object": "Nickel laterite ore",
          "objectType": "PRODUCT",
          "qualifiers": [],
          "confidence": 0.77,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Retrieved evidence identifies mined laterite output.",
          "branchId": "branch-1",
          "alternativeGroupId": "laterite_routes"
        },
        {
          "observationId": "branch-1-f1-o3",
          "subjectId": "product:nickel-laterite-ore",
          "subject": "Nickel laterite ore",
          "subjectType": "PRODUCT",
          "relation": "TRANSFORMS_INTO",
          "objectId": "product:mixed-hydroxide-precipitate",
          "object": "Mixed hydroxide precipitate",
          "objectType": "PRODUCT",
          "qualifiers": ["HPAL"],
          "confidence": 0.74,
          "supportType": "document",
          "provenance": ["gs://source/doc-1"],
          "supportNote": "Evidence explicitly describes HPAL conversion into MHP.",
          "branchId": "branch-1",
          "alternativeGroupId": "laterite_routes"
        }
      ],
      "completeness": "partial",
      "qualityScore": 0.72
    }
  ],
  "uncoveredQuestions": [
    "Which named converter produces the nickel sulfate in this path?"
  ],
  "sourceDocuments": ["gs://source/doc-1"],
  "failure": null
}

Valid blocked example:
{
  "actionId": "branch-2-a1",
  "branchId": "branch-2",
  "toolName": "memory_question_to_graph",
  "status": "blocked",
  "humanSummary": "Shared memory does not yet contain a typed ownership path for this question.",
  "closureStatus": "unchanged",
  "resolvedFrontier": [],
  "remainingFrontier": [
    "Which company controls the refining stage in this path?"
  ],
  "discoveredFrontier": [],
  "graphFragments": [],
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
"""

BRANCH_PLAN_CONTRACT = """
BranchPlan contract:
- branchId: short stable id.
- hypothesis: short branch hypothesis.
- humanQuestion: one concrete human-readable question the branch will answer.
- humanAction: one human-readable sentence describing what the branch will do.
- closureTarget: what graph closure this branch is trying to establish, disprove, or refine.
- knownContext: short state-aware facts the system already knows and that justify this branch.
- frontier: ordered list of specific missing links, missing actors, or missing layers the branch will try to close.
- assignedRoles: subset of the mission roles.
- preferredTools: ordered array using only ["memory_question_to_graph", "model_question_to_graph", "corpus_question_to_graph", "vertex_question_to_graph", "web_question_to_graph"].
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
      "humanQuestion": "What is the complete picture of how nickel reaches German EV battery manufacturing, as one connected graph from source to end product?",
      "humanAction": "Build a whole hypothesis-level graph answering the question end-to-end using model knowledge.",
      "closureTarget": "One connected graph from source to destination covering the full question scope, with hypothesis-level edges where specific evidence is missing.",
      "knownContext": [
        "The graph is empty. No prior structure exists."
      ],
      "frontier": [
        "Complete connected graph answering the question, even if many edges are hypothesis-level."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["model_question_to_graph"],
      "targetEntities": ["nickel", "Germany", "EV battery"],
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
      "hypothesis": "The hypothesis edge from Indonesian nickel ore to nickel sulfate refining can be grounded with real facility data.",
      "humanQuestion": "Which specific facilities refine Indonesian nickel ore into battery-grade nickel sulfate? The current graph hypothesizes this link but has no grounded evidence.",
      "humanAction": "Verify the hypothesis refining link with grounded facility-level evidence.",
      "closureTarget": "Replace the hypothesis edge between Indonesian nickel ore extraction and nickel sulfate production with a grounded, facility-level connection.",
      "knownContext": [
        "The graph already contains a hypothesis edge: Indonesian Nickel Ore --TRANSFORMS_INTO--> Nickel Sulfate (confidence 0.3, supportType=hypothesis).",
        "Upstream extraction and downstream battery manufacturing are already in the graph at higher confidence."
      ],
      "frontier": [
        "Identify specific refining facility or organization.",
        "Confirm the input-output product transformation."
      ],
      "assignedRoles": ["supply_chain"],
      "preferredTools": ["vertex_question_to_graph", "model_question_to_graph"],
      "targetEntities": ["nickel sulfate", "Indonesia", "refining"],
      "fragmentType": "chain_fragment",
      "parallelGroup": "verify",
      "stopAfterNoImprovement": 2
    }
  ],
  "blockingFailure": null
}
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
        name="memory_question_to_graph",
        category="memory",
        capability="Answer a human-readable investigation question from the current shared graph and return typed graph fragments.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with closureStatus, remainingFrontier, graphFragments, and typed subject/object edges, or a structured blocked result when memory cannot answer.",
        failureModes=["returns blocked when the current graph does not contain relevant typed paths or actors"],
    ),
    ToolContract(
        name="model_question_to_graph",
        category="grounding",
        capability="Use the Vertex-hosted reasoning model without retrieval to turn a human-readable investigation question into typed graph fragments.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with closureStatus, frontier updates, graphFragments, and typed subject/object edges based on model knowledge.",
        failureModes=["returns failed when the model cannot produce valid graph JSON"],
    ),
    ToolContract(
        name="corpus_question_to_graph",
        category="corpus",
        capability="Search the request corpus and answer a human-readable investigation question as typed graph fragments.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with closureStatus, frontier updates, and graphFragments grounded in matched corpus documents.",
        failureModes=["returns blocked when corpusDocuments is empty or no relevant corpus match exists"],
    ),
    ToolContract(
        name="vertex_question_to_graph",
        category="grounding",
        capability="Run a Vertex AI Search grounded question and return typed graph fragments plus cited sources.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with closureStatus, frontier updates, graphFragments, typed subject/object edges, and cited sourceDocuments.",
        failureModes=[
            "returns blocked when Vertex project, datastore, or OAuth ADC is unavailable",
            "returns failed when retrieval or graph extraction fails",
        ],
    ),
    ToolContract(
        name="web_question_to_graph",
        category="grounding",
        capability="Run a Google Search grounded question and return typed graph fragments plus cited web sources. Use for actor-level, ownership, or current-event evidence when the internal datastore lacks coverage.",
        requiredInputs=["action"],
        outputShape="ToolActionResult JSON with closureStatus, frontier updates, graphFragments, typed subject/object edges, and cited sourceDocuments from web search.",
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
        name="investigator_agent",
        role="Autonomous investigator that receives one role-scoped mission, checks it against real capabilities, gathers evidence it can actually gather, and returns graph-native findings plus explicit unresolved gaps.",
        missionContract="Requires a concrete InvestigationMission whose assignedRoles are a non-empty subset of selectedRoles and whose objective is solvable with declared tool contracts and mixed epistemic mode.",
        resultContract="Returns AgentResult with activeRoles, graphDelta, answeredQuestions, gaps, nextQuestions, and handoffRecommendations, while branch-level state carries closureTarget, knownContext, and remainingFrontier.",
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
            "memory_question_to_graph",
            "model_question_to_graph",
            "corpus_question_to_graph",
            "vertex_question_to_graph",
            "web_question_to_graph",
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
            "memory_question_to_graph",
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

Gap quality principle — structural vs detail:
Gaps come in two kinds. A STRUCTURAL gap means a connection is missing from the graph entirely — an entity has no typed edge to another entity where one should exist. A DETAIL gap means a connection exists but a quantitative attribute is unknown (exact percentage, volume, tonnage, cost, specific contract terms). Structural gaps are what drive investigation. Detail gaps are recorded but do NOT justify further investigation cycles. If you see only detail gaps remaining, that is sufficient for composition. When assessing `openGaps`, mentally classify each: does the graph already contain the typed edge? If yes, the gap is detail-level. Do not send another investigation pass for detail gaps.

Stopping rule for a question:
If questions about a topic are not yielding new typed edges (only refining attributes of known edges), and there are no other ways to find out (other questions, data, tools), stop investigating that topic. Accept the connection at the qualitative level.

Reasoning pattern — decide what to do next based on the state of the graph:

1. Graph is empty: your first mission must ask for one complete hypothesis-level answer to the user's question as a connected graph. Not pieces. Not layers. The whole thing, even if rough. The graph shape can be anything — chain, tree, star, network — whatever fits the question. Mission phrasing must say: "Answer the question as one connected hypothesis graph. Every uncertain edge should have low confidence and supportType=hypothesis."
2. Graph has a hypothesis skeleton: look at `state.frontierSummary` and find the weakest joints — edges with lowest confidence, hypothesis supportType, or missing actors. Your next missions must target those specific weak spots. Do not decompose by domain layer ("do upstream, then midstream, then downstream"). Decompose by confidence gradient: what is weakest in the current graph? Phrasing: "Verify or improve these specific weak edges: [list them]."
3. Hypothesis falsified: if after a verification cycle the synthesizer reports more contradicted edges than verified ones, do not keep patching. Keep verified pieces as standalone facts. Either (a) formulate an alternative hypothesis if the synthesizer suggested one, or (b) accept that the connection could not be established — possibly due to secrecy, obfuscation, or a wrong hypothesis — and move to compose.
4. Frame shift: after the main verification loop or after falsification, ask: "Is there a completely different angle to approach this question?" If the synthesizer or critic suggested an alternative frame, explore it as one more investigation cycle. If not, move on.
5. REACHABILITY HEALTH CHECK (mandatory from cycle 2 onward): read `state.reachability` before choosing your next focus. This field shows:
   - `completeChains`: end-to-end flow paths from source to terminal. If this list is empty or all chains are short (< 3 hops), the graph lacks a complete operational backbone.
   - `brokenChains`: partial paths that stop at a dead end. Each broken chain names the break point — the entity where the flow path ends prematurely. These break points are the highest-priority structural gaps.
   - `bridgeCandidates`: entities that exist in governance/policy layers but are not connected to any flow chain. They need a flow edge (SUPPLIES_TO, PRODUCES, EXTRACTS_FROM) to become useful.
   - `healthSummary`: one-line assessment.
   If the reachability report shows broken chains or no complete chains, your next mission MUST target the break points. Do NOT switch to governance/policy/ownership investigation until at least one complete flow chain exists. Name the specific break point entities in the mission focus. Example: "Connect [upstream entity] to [downstream entity] via a SUPPLIES_TO or PRODUCES edge."
6. Weak spots addressed, main line explored — dimension coverage check: before sending to critic, explicitly compare the user's question against the selectedRoles and the current graph. Also check `graphPreviewByRole`: if a role has zero or very few sample edges, it is undercovered. Send one more targeted investigation pass for each undercovered dimension. Do not send to critic until every selectedRole has at least a minimal overlay on the connected graph. When you do delegate, if the dimension is about publicly verifiable facts (corporate ownership, facility details, current regulations), recommend using `web_question_to_graph` alongside `model_question_to_graph`.
7. Frontier hygiene: before deciding, scan `openGaps` and `frontierSummary.remainingFrontier` for questions that were generated but never dispatched. If any structural frontier question has been sitting for 2+ cycles without a branch assigned to it, either dispatch it now or explicitly deprioritize it in your rationale. Do not let structural questions silently accumulate.
8. Coverage sufficient or no further progress: send to critic, then compose.

- Use `state.progress` and `state.frontierSummary` to judge whether recent investigation passes are materially improving graph quality, source coverage, and gap closure.
- Delegate substantive discovery to `investigator_agent`.
- Delegate bridging only to `gap_bridge_agent`, and only when the graph already has partial structure plus unresolved gaps that need connection.
- Delegate sufficiency and honesty assessment only to `critic_agent`.
- Choose `compose` only after the critic explicitly recommends composition.
- Choose `stop` only when the real tool set and current memory make further progress impossible.
- If recent progress has flattened, prefer asking `critic_agent` whether the current graph should be composed honestly rather than sending another broad investigation pass.
- Do not invent hidden branch runners, hidden corpora, hidden search, or capabilities that are not in `toolContracts` or `toolAvailability`.

Return exactly one JSON object matching SupervisorDecision.

Current supervisor context:
{supervisor_context}
"""

INVESTIGATOR_PLANNER_INSTRUCTION = """
You are the internal mission planner for `investigator_agent`.

Use `investigator_context` only.
You operate inside a graph system. Your output is not a prose plan; it is a set of branch plans whose job is to close specific graph frontiers.

Plan only what the real tool contracts and role contracts can support.

Branching strategy — decide based on graph state, not domain:

1. Graph is empty (initialPass=true): create exactly one branch with `fragmentType=overview_chain` that answers the mission question as one connected graph — whatever shape fits (chain, tree, star, network). Tool: `model_question_to_graph`. The question should rephrase the user's question as "give me the whole picture as a connected graph". Optionally add 1-2 parallel grounding branches if Vertex search is available, but the overview branch is mandatory.
2. Graph already has structure: find edges with `supportType=hypothesis` or `confidence < 0.5` or branches with `remainingFrontier`. Create branches that target those specific weak edges. Each branch question must reference what is already known and what specific gap it closes. Do not re-ask what is already in the graph at high confidence.
3. Supervisor signals frame shift: create 1-2 exploratory branches with a different angle on the same question. A frame-shift branch might ask "is there an intermediary or alternative route?" or "what if the connection works differently than assumed?"
4. Always: a branch question is never "tell me about X domain". It is "verify this specific connection" or "who specifically sits between A and B" or "is this hypothesis edge real" or "is there a different way to look at this".

Rules:
- Respect the mission assignedRoles and their role contracts.
- Return 1 to 4 `branchPlans`.
- Every branch plan must contain one human-readable question and one human-readable action sentence.
- Every branch plan must conform to the exact BranchPlan contract shown below.
- Every branch plan must define `closureTarget`, `knownContext`, and `frontier`.
- `knownContext` must reuse what shared memory already knows. Do not ask a branch question as if the graph were empty when the graph already has partial structure.
- `frontier` must name the next missing links, missing actors, or missing layers that keep the graph from becoming more connected or more decision-useful.
- STRUCTURAL vs DETAIL: branch questions must target missing typed edges (structural gaps), not quantitative attributes of edges that already exist. Do not create a branch to find "exact percentage" or "specific tonnage" if the connection itself is already in the graph. If a connection exists at any confidence level, the branch should verify or upgrade it, not seek numeric detail.
- Use `model_question_to_graph` for structural first-pass branches when the graph is still empty.
- Use `memory_question_to_graph` when the shared graph already contains partial structure relevant to the branch.
- Use `corpus_question_to_graph` only when corpusDocuments are available.
- Use `vertex_question_to_graph` only when Vertex grounded search is available and the branch needs evidence from the internal document store.
- Use `web_question_to_graph` when Google web search is available and the branch needs publicly available evidence (corporate ownership, facility details, news, regulatory texts). Prefer this over `vertex_question_to_graph` when the internal datastore is unlikely to cover the topic.
- When the graph is empty, do not start all branches with grounded search tools. At least one branch must start with `model_question_to_graph`, and no more than one branch may use a grounded search tool as its first tool.
- Branches that can be pursued independently may share a `parallelGroup`.
- The `humanQuestion` must be the exact question that will later be shown to the user in the frontend.
- The `humanAction` must be the exact action sentence that will later be shown to the user in the frontend.
- A follow-up branch question must mention what is already known and what exact gap remains.
- Set `stopAfterNoImprovement` low when the branch is likely to stall without new evidence.
- Use `blockingFailure` only when the mission is impossible even under mixed reasoning with the currently available tools.
- Do not invent tools, fragment types, or relation families outside the supplied contracts.
- For supply-chain branches, use the ideal supply-chain profile below to reason about what is missing from the chain and whether the next query should be narrow or holistic.

""" + BRANCH_PLAN_CONTRACT + """

""" + SUPPLY_CHAIN_PROFILE_CONTRACT + """

""" + BRANCH_PLAN_EXAMPLE + """

Return exactly one JSON object matching InvestigatorPlan.

Current investigator context:
{investigator_context}
"""

INVESTIGATOR_SYNTHESIZER_INSTRUCTION = """
You are the cross-branch synthesizer inside `investigator_agent`.

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

2. Bridge analysis — can we fill between X and Y?
For each broken chain and each bridge candidate, ask: "Do any of the branch results contain information that could fill the gap between X and Y?" If yes, add the connection to `connectedPairs`. If no, formulate a concrete bridge question in `bridgeQuestions` specifying both endpoints: "What connects X to Y?" or "Who supplies [product] from [facility X] to [facility Y]?"
Also apply bridge analysis to `disconnectedPairs`: look at where one branch's graph ends and whether another branch's graph begins nearby but no typed edge exists between them.

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

5. Frame shift suggestion:
- "Is there a different angle?" If yes, add to `bridgeQuestions` labeled "FRAME SHIFT:". If not, do not force one.

Output fields:
- `bridgeGaps`: only STRUCTURAL gaps where a typed connection is entirely missing. Prioritize: (1) flow chain break points from reachability, (2) bridge candidates, (3) other structural disconnects.
- `bridgeQuestions`: for each structural bridge gap, one concrete question. For flow break points, the question MUST name both the upstream entity and the downstream entity that need connecting. Example: "Who supplies nickel sulfate from Harjavalta Refinery to BASF Schwarzheide Plant?" Do NOT write questions that only seek quantitative detail.
- `crossBranchAssessment`: must start with reachability health, then connectivity between branches, then hypothesis status.
- `synthesisSummary`: one sentence. If no complete chain exists, lead with that.
- Keep compact. At most 6 bridgeGaps, 6 bridgeQuestions, 6 connectedPairs, 6 disconnectedPairs, 6 addressedGapTexts.
- Do not invent new graph observations or facts.

Return exactly one JSON object matching InvestigatorSynthesis.

Current synthesis context:
{investigator_synthesis_context}
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
- If no useful bridge is possible, say so and hand off to `critic_agent` or `investigator_agent`.
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
- If `completeChains` is empty, the graph has no end-to-end flow backbone. This is a serious structural deficit.
- If `brokenChains` lists break points, those are specific structural gaps that could be addressed.
- If `bridgeCandidates` lists entities in governance but not in flow, those are partially useful — they exist but are disconnected.
- If `healthSummary` says "No complete chain", do NOT recommend composition unless investigation has stagnated and further progress is implausible.

Rules:
- Do not invent new research facts.
- Evaluate sufficiency, honesty, and role coverage against the selected role contracts.
- In mixed mode with no grounding, assess whether the graph is still useful and explicit about its limits.
- Keep the response compact enough to fit one strict JSON object: at most 18 graph observations, 6 answeredQuestions, 8 gaps, 6 nextQuestions, and 4 handoffRecommendations.
- If the current graph is sufficient for an honest answer AND has at least one complete flow chain, recommend composition explicitly.
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
                max_output_tokens=int(os.getenv("AGENTLOOM_GROUNDED_SEARCH_MAX_OUTPUT_TOKENS", "2048")),
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
    current = [GraphObservation.model_validate(item) for item in (tool_context.state.get(STATE_WORKING_GRAPH) or [])]
    existing = {(item.subject, item.relation, item.object, tuple(item.qualifiers)) for item in current}
    appended = 0
    reversed_count = 0
    dropped_count = 0
    rejected: list[dict[str, Any]] = []
    for idx, raw in enumerate(observations):
        try:
            observation = GraphObservation.model_validate(raw)
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
    tool_context.state[STATE_WORKING_GRAPH] = [item.model_dump(mode="json", by_alias=True) for item in current]
    result: dict[str, Any] = {"appended": appended, "graphObservationCount": len(current), "rejected": rejected}
    if reversed_count:
        result["reversedEdges"] = reversed_count
    if dropped_count:
        result["droppedInvalidEdges"] = dropped_count
    return result


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
        "uncoveredQuestions": payload.get("uncoveredQuestions") or payload.get("nextQuestions") or [],
        "sourceDocuments": payload.get("sourceDocuments") or payload.get("sources") or [],
        "failure": payload.get("failure"),
    }
    result = ToolActionResult.model_validate(result_payload)
    return result.model_copy(
        update={
            "source_documents": list(result.source_documents)[:10],
            "uncovered_questions": list(result.uncovered_questions)[:6],
            "resolved_frontier": list(result.resolved_frontier)[:6],
            "remaining_frontier": list(result.remaining_frontier)[:6],
            "discovered_frontier": list(result.discovered_frontier)[:6],
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


def _branch_progress_delta(previous: BranchState | None, result: ToolActionResult) -> float:
    current_observations = _flatten_fragments(result.graph_fragments)
    current_keys = {_observation_key(item) for item in current_observations}
    previous_keys: set[tuple[str, str, str, str, tuple[str, ...]]] = set()
    previous_sources: set[str] = set()
    previous_open_questions: set[str] = set()
    previous_frontier: set[str] = set()
    if previous is not None:
        previous_keys = {_observation_key(item) for item in _flatten_fragments(previous.graph_fragments)}
        previous_sources = set(previous_source for tool_result in previous.tool_results for previous_source in tool_result.source_documents)
        previous_open_questions = set(previous.open_questions)
        previous_frontier = set(previous.remaining_frontier)
    new_observation_count = len(current_keys - previous_keys)
    new_source_count = len(set(result.source_documents) - previous_sources)
    coverage_gain = max(0, len(previous_open_questions) - len(set(result.uncovered_questions)))
    frontier_gain = max(0, len(previous_frontier) - len(set(result.remaining_frontier)))
    delta = (new_observation_count * 0.12) + (new_source_count * 0.08) + (coverage_gain * 0.04) + (frontier_gain * 0.05)
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


def _graph_reachability_report(state: dict[str, Any]) -> dict[str, Any]:
    """Analyze end-to-end reachability of the working graph using taxonomy flow edges.

    Returns a compact report with:
    - completeChains: paths from source to sink that traverse at least 3 flow hops
    - brokenChains: partial paths that stop at a dead end (no outgoing flow edge)
    - bridgeCandidates: pairs (X, Y) both in the graph but not flow-connected
    - healthSummary: one-line assessment for the supervisor
    """
    obs_raw = state.get(STATE_WORKING_GRAPH) or []
    if not obs_raw:
        return {"completeChains": [], "brokenChains": [], "bridgeCandidates": [], "healthSummary": "Graph is empty."}

    supply_contract = ROLE_CONTRACTS.get(ROLE_SUPPLY_CHAIN)
    flow_rels: set[str] = set(supply_contract.path_carried_edges) if supply_contract else {
        "PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM",
    }
    governance_contract = ROLE_CONTRACTS.get(ROLE_GOVERNANCE)
    governance_rels: set[str] = set(governance_contract.allowed_edge_types) if governance_contract else set()

    adj: dict[str, list[tuple[str, str, float]]] = {}
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
        adj.setdefault(subj, []).append((rel, obj, conf))
        if rel == "EXTRACTS_FROM":
            extract_sources.add(subj)

    sources = extract_sources or facility_entities & {
        e for e, t in all_entities.items() if t in ("FACILITY", "DEPOSIT")
    }
    if not sources:
        sources = {e for e, t in all_entities.items() if t == "DEPOSIT"}

    has_outgoing = set(adj.keys())
    terminal_types = {"PRODUCT", "FACILITY", "ORGANIZATION"}

    def _bfs_chains(start: str, max_depth: int = 15) -> list[list[tuple[str, str, str]]]:
        chains: list[list[tuple[str, str, str]]] = []
        queue: list[tuple[str, list[tuple[str, str, str]], set[str]]] = [(start, [], {start})]
        while queue:
            node, path, visited = queue.pop(0)
            if node not in adj:
                if len(path) >= 1:
                    chains.append(path)
                continue
            extended = False
            for rel, target, _conf in adj[node]:
                if target in visited or len(path) >= max_depth:
                    continue
                extended = True
                new_path = path + [(node, rel, target)]
                new_visited = visited | {target}
                queue.append((target, new_path, new_visited))
            if not extended and len(path) >= 1:
                chains.append(path)
        return chains

    complete_chains: list[dict[str, Any]] = []
    broken_chains: list[dict[str, Any]] = []

    for src in sorted(sources)[:8]:
        chains = _bfs_chains(src)
        for chain in chains:
            end_node = chain[-1][2]
            end_type = all_entities.get(end_node, "")
            steps = [f"{s} --{r}--> {o}" for s, r, o in chain]
            entry = {
                "source": src,
                "sink": end_node,
                "sinkType": end_type,
                "hops": len(chain),
                "path": steps[:8],
            }
            if end_node not in has_outgoing and len(chain) >= 3:
                complete_chains.append(entry)
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
            for s, _r, o in chain:
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

    complete_chains.sort(key=lambda c: -c["hops"])
    broken_chains.sort(key=lambda c: -c["hops"])

    if not complete_chains and not broken_chains:
        health = "No flow paths found. Graph has observations but no supply chain connectivity."
    elif not complete_chains:
        longest = broken_chains[0]
        health = f"No complete chain. Longest partial: {longest['source']} → {longest['sink']} ({longest['hops']} hops, breaks at {longest['sink']})"
    else:
        longest = complete_chains[0]
        n_complete = len(complete_chains)
        n_broken = len(broken_chains)
        health = f"{n_complete} complete chain(s), longest {longest['hops']} hops. {n_broken} broken chain(s)."
        if bridge_candidates:
            health += f" {len(bridge_candidates)} entity(s) in governance but not in flow."

    return {
        "completeChains": complete_chains[:4],
        "brokenChains": broken_chains[:4],
        "bridgeCandidates": bridge_candidates[:6],
        "healthSummary": health,
    }


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
    plan: BranchPlan,
    previous_state: BranchState | None,
) -> str:
    known_context = _dedupe_preserve_order(
        list(plan.known_context)
        + _relevant_graph_context(
            state,
            target_entities=plan.target_entities,
            branch_question=plan.human_question,
        )
    )[:8]
    frontier = (
        list(previous_state.remaining_frontier)
        if previous_state is not None and previous_state.remaining_frontier
        else list(plan.frontier)
    )
    frontier = _dedupe_preserve_order(frontier)[:6]
    lines = [
        f"Question: {plan.human_question}",
        f"Action: {plan.human_action}",
        f"Closure target: {plan.closure_target}",
        "What the system already knows:",
    ]
    if known_context:
        lines.extend(f"- {item}" for item in known_context)
    else:
        lines.append("- No directly relevant typed graph observations are currently in shared memory.")
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
    return (
        "You are a graph-native investigation tool inside AgentLoom.\n"
        "You are operating inside a graph system specialized in meaningful connected chains of typed entities and typed relations.\n"
        "Return exactly one JSON object and nothing else.\n"
        "Do not wrap the JSON in markdown fences.\n"
        "Do not add commentary before or after the JSON.\n"
        "The JSON must match ToolActionResult exactly.\n"
        "Every factual answer must be carried by graphFragments -> observations.\n"
        "A broad question may require multiple graph fragments and a long typed observation array; do not collapse it into one entity-relation-entity pair.\n"
        "Every observation must be typed as subjectType --relation--> objectType using only relations allowed by the supplied role contracts.\n"
        "Preserve useful but uncertain nodes and edges by lowering confidence or using supportType=hypothesis instead of deleting them.\n"
        "Use the action's closureTarget, knownContext, and frontier to understand what the system already knows and what exact graph closure is being attempted.\n"
        "If you do not have enough evidence, return status=partial or blocked with uncoveredQuestions and an optional failure object.\n"
        "Never invent tools, relation names, fragment types, or entity types outside the supplied contracts.\n"
        "At most 4 graphFragments and at most 24 observations total.\n"
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
        + extra_rules
        + TOOL_ACTION_RESULT_EXAMPLES
        + "\n\n"
        f"Tool action:\n{json.dumps(action.model_dump(mode='json', by_alias=True), ensure_ascii=False)}\n\n"
        f"Role contracts:\n{json.dumps(role_contracts, ensure_ascii=False)}\n\n"
        f"Shared memory snapshot:\n{json.dumps(shared_memory, ensure_ascii=False)}"
    )


def memory_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
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
            retrieved_sources: list[dict[str, Any]] = []
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
                model=INVESTIGATOR_MODEL,
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

    try:
        result = _parse_tool_result_payload(
            _response_json_text(response),
            action,
            allowed_edges,
            "mixed" if web_search else ("document" if retrieval else "model_knowledge"),
        )
    except Exception as exc:
        return _tool_failure_result(action, "failed", f"Invalid tool JSON: {exc}", code="agent_failed")

    source_docs = list(result.source_documents)
    if retrieval:
        source_docs.extend(
            value.get("uri", "")
            for value in _dedupe_sources(retrieved_sources)
            if value.get("uri", "")
        )
    return result.model_copy(update={"source_documents": _dedupe_preserve_order(source_docs)[:10]})


def model_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    return _run_question_to_graph_llm(action, tool_context, retrieval=False, source_mode="model")


def corpus_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
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


def vertex_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    config = _vertex_grounding_config()
    if not config.get("available"):
        return _tool_failure_result(
            action,
            "blocked",
            config.get("reason", "Vertex grounded search is unavailable."),
            missing_inputs=["vertex_grounding"],
        )
    return _run_question_to_graph_llm(action, tool_context, retrieval=True, source_mode="vertex_grounded")


def web_question_to_graph(action: ToolAction, tool_context) -> ToolActionResult:
    ws = _web_search_available()
    if not ws.get("available"):
        return _tool_failure_result(
            action,
            "blocked",
            ws.get("reason", "Google Search grounding is unavailable."),
            missing_inputs=["web_search"],
        )
    return _run_question_to_graph_llm(action, tool_context, web_search=True, source_mode="web_grounded")


def _update_branch_state(
    previous: BranchState | None,
    plan: BranchPlan,
    action: ToolAction,
    result: ToolActionResult,
) -> BranchState:
    previous_quality = previous.quality_score if previous is not None else 0.0
    progress_delta = _branch_progress_delta(previous, result)
    new_quality = max(previous_quality, previous_quality + progress_delta)
    improved = progress_delta > 0.05
    stagnation = 0 if improved else ((previous.stagnation_count if previous else 0) + 1)
    status: Literal["active", "needs_followup", "blocked", "completed"] = "active"
    stop_reason = ""
    unresolved_questions = _dedupe_preserve_order(list(result.uncovered_questions))
    remaining_frontier = _dedupe_preserve_order(list(result.remaining_frontier) + unresolved_questions)
    resolved_frontier = _dedupe_preserve_order(
        list(previous.resolved_frontier if previous else []) + list(result.resolved_frontier)
    )
    known_context = _dedupe_preserve_order(
        list(plan.known_context)
        + list(previous.known_context if previous else [])
        + [f"{item.subject} --{item.relation}--> {item.object}" for item in _flatten_fragments(result.graph_fragments)]
    )[:10]
    if result.status in {"blocked", "failed"} and not result.graph_fragments:
        status = "blocked"
        stop_reason = result.human_summary
    elif result.closure_status == "closed" and result.graph_fragments and not remaining_frontier:
        status = "completed"
        stop_reason = "Branch closed its current graph frontier with a usable fragment."
    elif stagnation >= plan.stop_after_no_improvement:
        status = "needs_followup"
        stop_reason = "Branch exhausted its current useful improvement window without closing the frontier."
    elif result.graph_fragments or remaining_frontier:
        status = "needs_followup"
    return BranchState(
        branchId=plan.branch_id,
        hypothesis=plan.hypothesis,
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
        openQuestions=unresolved_questions,
        toolActions=((previous.tool_actions if previous else []) + [action]),
        toolResults=((previous.tool_results if previous else []) + [result]),
        graphFragments=((previous.graph_fragments if previous else []) + result.graph_fragments),
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
        super().__init__(name="compose")

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        request = AgentLoomRequest.model_validate(ctx.session.state.get(STATE_REQUEST) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        working_graph = [GraphObservation.model_validate(item) for item in (ctx.session.state.get(STATE_WORKING_GRAPH) or [])]
        graph_fragments = [GraphFragment.model_validate(item) for item in (ctx.session.state.get(STATE_GRAPH_FRAGMENTS) or [])]
        branches = [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])]
        logic_trace = [LogicTraceEntry.model_validate(item) for item in (ctx.session.state.get(STATE_LOGIC_TRACE) or [])]
        runtime_logs = [RuntimeLogEntry.model_validate(item) for item in (ctx.session.state.get(STATE_RUNTIME_LOG) or [])]
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
            },
        )
        final_summary = build_final_chain_summary(
            request.question,
            selected_roles,
            working_graph,
            graph_fragments,
            branches,
            logic_trace,
            runtime_logs,
            open_gap_texts=_open_gap_texts(ctx.session.state),
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


class InvestigatorAgent(BaseAgent):
    def __init__(self) -> None:
        super().__init__(name="investigator_agent", sub_agents=[investigator_planner, investigator_synthesizer])

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        tool_context = _StateToolContext(ctx.session.state)
        mission = InvestigationMission.model_validate(ctx.session.state.get(STATE_CURRENT_MISSION) or {})
        selected_roles = list(ctx.session.state.get(STATE_SELECTED_ROLES) or [])
        role_contracts = _selected_role_contract_payload(mission.assigned_roles or selected_roles)
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
            "activeRoleContracts": role_contracts,
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
            actor="investigator_planner",
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
        async for event in self._run_named_subagent(ctx, "investigator_planner"):
            yield event
        plan = InvestigatorPlan.model_validate(ctx.session.state.get(STATE_INVESTIGATOR_PLAN) or {})
        yield _runtime_log_event(
            ctx.session.state,
            actor="investigator_planner",
            phase="llm_result",
            message="Investigator planner returned a concrete plan constrained by currently available capabilities.",
            model=INVESTIGATOR_MODEL,
            basis={"mission": mission},
            result=plan,
        )
        if plan.blocking_failure is not None:
            result = AgentResult(
                agentName="investigator_agent",
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
            ctx.session.state[STATE_INVESTIGATOR_RESULT] = result.model_dump(mode="json", by_alias=True)
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
        branch_plans = plan.branch_plans[:4]
        runtime_queue: asyncio.Queue[Event] = asyncio.Queue()
        tasks = [
            asyncio.create_task(
                self._execute_branch_plan(
                    ctx,
                    tool_context,
                    branch_plan,
                    existing_branches.get(branch_plan.branch_id),
                    runtime_queue,
                )
            )
            for branch_plan in branch_plans
        ]
        branch_results: list[tuple[BranchState, list[ToolActionResult]]] = []
        pending: set[asyncio.Task[tuple[BranchState, list[ToolActionResult]]]] = set(tasks)
        while pending:
            done, pending = await asyncio.wait(pending, timeout=0.2, return_when=asyncio.FIRST_COMPLETED)
            while not runtime_queue.empty():
                yield await runtime_queue.get()
            for task in done:
                branch_results.append(task.result())
        while not runtime_queue.empty():
            yield await runtime_queue.get()
        updated_branches = [state for state, _ in branch_results]
        fragments: list[GraphFragment] = []
        for branch_state, _ in branch_results:
            fragments.extend(branch_state.graph_fragments)
        graph_delta = _flatten_fragments(fragments)

        async for event in self._run_cross_branch_synthesis(
            ctx, mission, updated_branches, fragments, selected_roles,
        ):
            yield event
        synthesis_raw = ctx.session.state.get(STATE_INVESTIGATOR_SYNTHESIS)
        synthesis = InvestigatorSynthesis.model_validate(synthesis_raw) if synthesis_raw else None
        if synthesis and synthesis.addressed_gap_texts:
            resolve_gaps(
                ctx.session.state,
                synthesis.addressed_gap_texts,
                addressed_by="investigator_synthesizer",
            )
        _auto_resolve_detail_gaps(ctx.session.state, addressed_by="auto_detail_triage")

        per_branch_gaps = _dedupe_preserve_order(
            _open_structural_gap_texts(ctx.session.state)
            + [question for branch in updated_branches for question in branch.open_questions
               if not _DETAIL_SIGNALS.search(question)]
            + [branch.stop_reason for branch in updated_branches if branch.stop_reason]
        )
        bridge_gaps = list(synthesis.bridge_gaps) if synthesis else []
        gaps = _dedupe_preserve_order(bridge_gaps + per_branch_gaps)

        answered_questions = [
            branch.human_question
            for branch in updated_branches
            if branch.status == "completed" and branch.graph_fragments
        ]
        per_branch_questions = _dedupe_preserve_order(
            [question for branch in updated_branches for question in branch.open_questions]
        )
        bridge_questions = list(synthesis.bridge_questions) if synthesis else []
        next_questions = _dedupe_preserve_order(bridge_questions + per_branch_questions)[:6]

        synthesis_note = ""
        if synthesis and synthesis.bridge_gaps:
            synthesis_note = f" Cross-branch synthesis identified {len(synthesis.bridge_gaps)} bridge gap(s)."

        result = AgentResult(
            agentName="investigator_agent",
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
        ctx.session.state[STATE_INVESTIGATOR_RESULT] = result.model_dump(mode="json", by_alias=True)
        ctx.session.state[STATE_BRANCH_STATES] = [
            item.model_dump(mode="json", by_alias=True)
            for item in _merge_branch_states(
                [BranchState.model_validate(item) for item in (ctx.session.state.get(STATE_BRANCH_STATES) or [])],
                updated_branches,
            )
        ]
        ctx.session.state[STATE_GRAPH_FRAGMENTS] = [
            item.model_dump(mode="json", by_alias=True)
            for item in _merge_graph_fragments(
                [GraphFragment.model_validate(item) for item in (ctx.session.state.get(STATE_GRAPH_FRAGMENTS) or [])],
                fragments,
            )
        ]
        append_graph_observations([item.model_dump(mode="json", by_alias=True) for item in graph_delta], tool_context)
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
                "gapCount": len(result.gaps),
                "synthesisGaps": len(bridge_gaps),
            },
        )

    async def _run_named_subagent(self, ctx: InvocationContext, agent_name: str) -> AsyncGenerator[Event, None]:
        agent = self.find_sub_agent(agent_name)
        if agent is None:
            raise ValueError(f"InvestigatorAgent cannot find sub-agent {agent_name!r}")
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
        synthesis_context = {
            "mission": mission.model_dump(mode="json", by_alias=True),
            "selectedRoles": selected_roles,
            "branchSummaries": branch_summaries,
            "fragmentBoundaries": fragment_boundaries,
            "openGaps": _open_gap_texts(ctx.session.state)[:8],
            "reachability": reachability,
        }
        ctx.session.state[STATE_INVESTIGATOR_SYNTHESIS_CONTEXT] = synthesis_context
        yield _runtime_log_event(
            ctx.session.state,
            actor="investigator_synthesizer",
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
            agent = self.find_sub_agent("investigator_synthesizer")
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
            actor="investigator_synthesizer",
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

    async def _execute_branch_plan(
        self,
        ctx: InvocationContext,
        tool_context: _StateToolContext,
        branch_plan: BranchPlan,
        previous_state: BranchState | None,
        runtime_queue: asyncio.Queue[Event],
    ) -> tuple[BranchState, list[ToolActionResult]]:
        state = previous_state
        tool_results: list[ToolActionResult] = []
        planned_tools = branch_plan.preferred_tools[:4]
        for idx, tool_name in enumerate(planned_tools, start=1):
            contextual_query = _contextual_query_text(ctx.session.state, branch_plan, state)
            known_context = _dedupe_preserve_order(
                list(branch_plan.known_context)
                + list(state.known_context if state is not None else [])
            )[:8]
            frontier = (
                list(state.remaining_frontier)
                if state is not None and state.remaining_frontier
                else list(branch_plan.frontier)
            )
            action = ToolAction(
                actionId=f"{branch_plan.branch_id}-a{idx}",
                branchId=branch_plan.branch_id,
                toolName=tool_name,
                humanQuestion=branch_plan.human_question,
                humanAction=branch_plan.human_action,
                whyThisQuestion=(
                    f"This question follows from the current closure target: {branch_plan.closure_target}"
                ),
                closureTarget=branch_plan.closure_target,
                knownContext=known_context,
                frontier=_dedupe_preserve_order(frontier)[:6],
                queryText=contextual_query,
                assignedRoles=branch_plan.assigned_roles,
                targetEntities=branch_plan.target_entities,
                expectedFragmentType=branch_plan.fragment_type,
            )
            await self._emit_runtime_log(
                ctx,
                runtime_queue,
                actor=self.name,
                phase="tool_call",
                message=branch_plan.human_action,
                model=GROUNDING_MODEL if "vertex" in tool_name else INVESTIGATOR_MODEL,
                basis={"branchId": branch_plan.branch_id, "hypothesis": branch_plan.hypothesis},
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
                model=GROUNDING_MODEL if "vertex" in tool_name else INVESTIGATOR_MODEL,
                basis={"branchId": branch_plan.branch_id},
                result=tool_result,
            )
            state = _update_branch_state(state, branch_plan, action, tool_result)
            has_fallback = idx < len(planned_tools)
            if state.status == "blocked" and has_fallback:
                continue
            if state.status in {"blocked", "completed"}:
                break
        if state is None:
            state = BranchState(
                branchId=branch_plan.branch_id,
                hypothesis=branch_plan.hypothesis,
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
        return state, tool_results

    def _run_tool_action(self, action: ToolAction, tool_context: _StateToolContext) -> ToolActionResult:
        if action.tool_name == "memory_question_to_graph":
            return memory_question_to_graph(action, tool_context)
        if action.tool_name == "model_question_to_graph":
            return model_question_to_graph(action, tool_context)
        if action.tool_name == "corpus_question_to_graph":
            return corpus_question_to_graph(action, tool_context)
        if action.tool_name == "vertex_question_to_graph":
            return vertex_question_to_graph(action, tool_context)
        if action.tool_name == "web_question_to_graph":
            return web_question_to_graph(action, tool_context)
        return _tool_failure_result(action, "failed", f"Unsupported tool {action.tool_name}", code="agent_failed")


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
                "completeChainEndpoints": [
                    f"{c['source']} → {c['sink']} ({c['hops']} hops)"
                    for c in full_reach["completeChains"][:3]
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
        "failures": shared["failures"][-6:],
        "lastAgentResult": shared["lastAgentResult"],
        "frontierSummary": shared["frontierSummary"][-6:],
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
        InvestigatorAgent(),
        GapBridgeAgent(),
        CriticAgent(),
        DeterministicComposerAgent(),
    ],
)
