from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from agenthorde.schemas.graph import ReasoningGraph

SupervisorNextAgent = Literal[
    "supply_chain_analyzer",
    "governance_control_analyzer",
    "critic_agent",
    "red_team_agent",
    "finalize",
]


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


class SessionPack(BaseModel):
    """Canonical typed session blob stored under STATE_PACK_KEY."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    schema_version: int = Field(default=1, alias="schemaVersion")
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
    apply_log: list[ApplyPatchRecord] = Field(default_factory=list, alias="applyLog")
    cycle_index: int = Field(default=0, alias="cycleIndex")
    supervisor_cap: int = Field(default=8, alias="supervisorCap")
    done: bool = False
    final_answer: str = Field(default="", alias="finalAnswer")
