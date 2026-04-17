from __future__ import annotations

from typing import Any, Literal

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


class ProposedRelation(BaseModel):
    """Structural edge proposed by analyzer/governance between surface-labeled entities.

    Consumed by the canonicalizer as a pre-pass before the Phase 4 LLM call; supplies
    the canonical pipeline with edges the analyzer already saw, so Phase 4 only needs
    to extend coverage rather than re-derive everything from prose.
    """

    model_config = ConfigDict(extra="ignore", populate_by_name=True)

    src: str
    dst: str
    relation: str
    attributes: dict[str, Any] = Field(default_factory=dict)
    source_claim_ids: list[str] = Field(default_factory=list, alias="sourceClaimIds")


class AnalyzerOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    chain_summary: str = Field(alias="chainSummary")
    proposed_nodes: list[ReasoningNode] = Field(default_factory=list, alias="proposedNodes")
    proposed_relations: list[ProposedRelation] = Field(
        default_factory=list, alias="proposedRelations"
    )
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
