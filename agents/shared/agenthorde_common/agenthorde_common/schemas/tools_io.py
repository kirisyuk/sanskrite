from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field


class ToolEnvelope(BaseModel):
    """Uniform tool outcome wrapper (success and failure)."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    ok: bool
    tool: str
    status: Literal["ok", "validation_error", "internal_error"] = "ok"
    message: str = ""
    payload: dict[str, Any] = Field(default_factory=dict)


class WebSearchSummary(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    query: str
    note: str = "Results come from model grounding / search tool; summarize in agent."


class KnowledgeSearchInput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    query: str
    scope: str = "general"


class KnowledgeSearchOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    answer: str
    stability: Literal["high", "medium", "low"] = "medium"
    caveats: list[str] = Field(default_factory=list)


class RetrieveToolInput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    query: str
    scope: Literal["working_graph", "reasoning_graph", "all"] = "all"
    limit: int = Field(default=24, ge=1, le=200)


class RetrieveToolOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    matches: list[dict[str, Any]] = Field(default_factory=list)
    scope: str = "all"


class ApplyToolInput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    schema_version: int = Field(default=1, alias="schemaVersion")
    hypothesis_id: str = Field(default="", alias="hypothesisId")
    patch: dict[str, Any] = Field(default_factory=dict)


class ApplyToolOutput(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    applied_index: int = Field(alias="appliedIndex")
    working_graph_node_count: int = Field(default=0, alias="workingGraphNodeCount")
