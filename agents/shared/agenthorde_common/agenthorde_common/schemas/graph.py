from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

ReasoningNodeKind = Literal[
    "claim",
    "question",
    "hypothesis_anchor",
    "tool_evidence",
    "counter",
    "mission",
]


class ReasoningNode(BaseModel):
    # LLM outputs may include unknown keys; ignore to keep merge robust.
    model_config = ConfigDict(extra="ignore", populate_by_name=True)

    id: str
    kind: ReasoningNodeKind
    text: str
    hypothesis_id: str = Field(default="", alias="hypothesisId")
    evidence_refs: list[str] = Field(default_factory=list, alias="evidenceRefs")
    added_at_cycle: int = Field(
        default=0,
        alias="addedAtCycle",
        ge=0,
        description=(
            "Cycle index at which this node was first added to the pack. Populated by "
            "the merge layer on first insertion; preserved on subsequent merges. "
            "Default 0 means 'pre-cycle-tracking' (legacy or fresh node from analyzer)."
        ),
    )


class ReasoningEdge(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    src: str
    dst: str
    relation: str = "supports"


class ReasoningGraph(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    nodes: list[ReasoningNode] = Field(default_factory=list)
    edges: list[ReasoningEdge] = Field(default_factory=list)
