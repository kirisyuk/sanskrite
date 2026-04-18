from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from agenthorde_common.schemas.graph import ReasoningGraph

SupervisorNextAgent = Literal[
    "supply_chain_analyzer",
    "governance_control_analyzer",
    "sanctions_analyzer",
    "critic_agent",
    "red_team_agent",
    "parallel_fanout",
    "finalize",
]

GateName = Literal["", "structural", "evidence", "open_questions"]
GateAction = Literal["pass", "retry", "skip"]


class HypothesisRecord(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    hypothesis_id: str = Field(alias="hypothesisId")
    title: str
    status: Literal["active", "merged", "discarded"] = "active"
    notes: str = ""


class ApplyPatchRecord(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    schema_version: int = Field(default=1, alias="schemaVersion")
    hypothesis_id: str = Field(default="", alias="hypothesisId")
    patch: dict = Field(default_factory=dict)


class SupervisorTraceEntry(BaseModel):
    """One supervisor decision or gate action record."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    cycle: int
    decision: SupervisorNextAgent
    rationale: str = ""
    gate: GateName = ""
    gate_action: GateAction = Field(default="pass", alias="gateAction")


class PackProposedEntity(BaseModel):
    """Merged entity registry row on SessionPack (namespaced id + label)."""

    model_config = ConfigDict(extra="ignore", populate_by_name=True)

    namespaced_id: str = Field(alias="namespacedId")
    label: str = ""


class PackProposedRelation(BaseModel):
    """Mirror of agents_io.ProposedRelation persisted on SessionPack.

    Declared locally to avoid a circular import (agents_io imports from state).
    The canonicalizer reads pack.proposedRelations as plain dicts, so the exact
    class identity is not required downstream.
    """

    model_config = ConfigDict(extra="ignore", populate_by_name=True)

    src_id: str = Field(alias="srcId")
    dst_id: str = Field(alias="dstId")
    relation: str
    attributes: dict[str, Any] = Field(default_factory=dict)
    source_claim_ids: list[str] = Field(default_factory=list, alias="sourceClaimIds")


class SessionPack(BaseModel):
    """Canonical typed session blob stored under STATE_PACK_KEY."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    schema_version: int = Field(default=2, alias="schemaVersion")
    user_question: str = Field(default="", alias="userQuestion")
    research_temperature: float = Field(
        default=0.35,
        alias="researchTemperature",
        ge=0.0,
        le=1.0,
        description="Higher => more adversarial / geopolitical tension framing.",
    )
    success_criteria_notes: str = Field(default="", alias="successCriteriaNotes")
    success_checklist: list[str] = Field(default_factory=list, alias="successChecklist")
    hypotheses: dict[str, HypothesisRecord] = Field(default_factory=dict)
    reasoning_graph: ReasoningGraph = Field(default_factory=ReasoningGraph, alias="reasoningGraph")
    working_graph: dict = Field(
        default_factory=dict,
        alias="workingGraph",
        description="In-memory graph substrate for retrieve/apply MVP.",
    )
    proposed_entities: list[PackProposedEntity] = Field(
        default_factory=list,
        alias="proposedEntities",
        description="Merged entity registry (namespaced ids) from analyzers.",
    )
    proposed_relations: list[PackProposedRelation] = Field(
        default_factory=list,
        alias="proposedRelations",
        description="Structural edges proposed by analyzers; feeds Phase 4 pre-pass.",
    )
    apply_log: list[ApplyPatchRecord] = Field(default_factory=list, alias="applyLog")
    cycle_index: int = Field(default=0, alias="cycleIndex")
    supervisor_cap: int = Field(default=8, alias="supervisorCap")
    initial_supervisor_cap: int = Field(
        default=8,
        alias="initialSupervisorCap",
        description="Original supervisor_cap at run start; used by extend_cycle_cap to size +10% increments.",
    )
    extensions_used: int = Field(
        default=0,
        alias="extensionsUsed",
        ge=0,
        le=3,
        description="Number of times the supervisor has invoked extend_cycle_cap (max 3 per run, +10% of initial cap each).",
    )
    extensions_pending_red_team: bool = Field(
        default=False,
        alias="extensionsPendingRedTeam",
        description="When true, the next cycle MUST dispatch red_team_agent to gate the extension that was just applied.",
    )
    token_spend: int = Field(
        default=0,
        alias="tokenSpend",
        description="Rough running token budget consumption; populated by the supervisor as events arrive.",
    )
    web_search_calls_used: int = Field(
        default=0,
        alias="webSearchCallsUsed",
        description="Cumulative count of web_search tool invocations.",
    )
    cost_so_far_usd: float = Field(
        default=0.0,
        alias="costSoFarUsd",
        ge=0.0,
        description="Approximate USD budget burn so far; best-effort estimate.",
    )
    supervisor_trace: list[SupervisorTraceEntry] = Field(
        default_factory=list, alias="supervisorTrace"
    )
    last_intent: str = Field(
        default="",
        alias="lastIntent",
        description="Last non-meta worker picked by supervisor_router (supply_chain/governance).",
    )
    gates_retried: list[str] = Field(
        default_factory=list,
        alias="gatesRetried",
        description="Pre-finalize gate ids already retried this run (dedup guard).",
    )
    done: bool = False
    final_answer: str = Field(default="", alias="finalAnswer")
