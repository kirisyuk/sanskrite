from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from agenthorde.schemas.graph import ReasoningNode
from agenthorde.schemas.state import SupervisorNextAgent


class SupervisorDecision(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    next_agent: SupervisorNextAgent = Field(alias="nextAgent")
    mission: str = ""
    research_temperature: float = Field(
        default=0.35,
        alias="researchTemperature",
        ge=0.0,
        le=1.0,
    )
    rationale: str = ""


class AnalyzerOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    chain_summary: str = Field(alias="chainSummary")
    proposed_nodes: list[ReasoningNode] = Field(default_factory=list, alias="proposedNodes")
    open_questions: list[str] = Field(default_factory=list, alias="openQuestions")


class CriticVerdict(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    sufficient: bool
    blockers: list[str] = Field(default_factory=list)
    non_blockers: list[str] = Field(default_factory=list, alias="nonBlockers")
    mission_hint: str = Field(default="", alias="missionHint")
    composition_recommendation: Literal["go", "conditional", "stop"] = Field(
        default="conditional",
        alias="compositionRecommendation",
    )


class RedTeamOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    reframe_notes: str = Field(alias="reframeNotes")
    suggested_temperature_delta: float = Field(
        default=0.0,
        alias="suggestedTemperatureDelta",
        ge=-0.5,
        le=0.5,
    )
    probe_missions: list[str] = Field(default_factory=list, alias="probeMissions")
