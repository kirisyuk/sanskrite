from __future__ import annotations

import os
from typing import Any

from google import genai
from google.genai import types as genai_types

from agenthorde.config import WORKER_MODEL
from agenthorde_common.schemas.tools_io import ToolEnvelope
from agenthorde_common.tools.grounded_web_common import format_grounded_web_fetcher_prompt
from agenthorde_common.tools.session_tools import (
    ah_apply as _ah_apply,
    ah_knowledge_search as _ah_knowledge_search,
    ah_retrieve as _ah_retrieve,
    extend_cycle_cap as _extend_cycle_cap,
)
from agenthorde_common.tools.tool_context import AgentHordeToolContext, tool_state_from_context
from agenthorde_common.tools.tool_ledger import append_tool_ledger as _append_tool_ledger


def _sources_from_grounding(response: genai_types.GenerateContentResponse) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for candidate in response.candidates or []:
        gm = getattr(candidate, "grounding_metadata", None)
        if not gm or not getattr(gm, "grounding_chunks", None):
            continue
        for chunk in gm.grounding_chunks or []:
            web = getattr(chunk, "web", None)
            if not web:
                continue
            out.append(
                {
                    "title": getattr(web, "title", "") or "",
                    "url": getattr(web, "uri", "") or "",
                    "domain": getattr(web, "domain", "") or "",
                }
            )
    return out[:24]


def web_search(search_query: str, tool_context: AgentHordeToolContext | None = None) -> dict[str, Any]:
    """Vertex-backed Google Search grounding via genai (Python callable for ADK AFC compatibility)."""
    state = tool_state_from_context(tool_context)
    if state is None:
        return ToolEnvelope(
            ok=False,
            tool="web_search",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    q = (search_query or "").strip()
    if not q:
        env = ToolEnvelope(ok=False, tool="web_search", status="validation_error", message="empty search_query")
        _append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    project = os.environ.get("GOOGLE_CLOUD_PROJECT", "").strip()
    location = os.environ.get("GOOGLE_CLOUD_LOCATION", "global").strip()
    if not project:
        env = ToolEnvelope(
            ok=False,
            tool="web_search",
            status="internal_error",
            message="GOOGLE_CLOUD_PROJECT is not set",
        )
        _append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    model = os.getenv("AGENTHORDE_WORKER_MODEL", WORKER_MODEL).strip() or WORKER_MODEL
    prompt = format_grounded_web_fetcher_prompt(q)
    try:
        client = genai.Client(vertexai=True, project=project, location=location)
        response = client.models.generate_content(
            model=model,
            contents=prompt,
            config=genai_types.GenerateContentConfig(
                temperature=0.2,
                tools=[genai_types.Tool(google_search=genai_types.GoogleSearch())],
            ),
        )
        text = (response.text or "").strip()
        sources = _sources_from_grounding(response)
        payload = {"answer": text, "sources": sources}
        env = ToolEnvelope(ok=True, tool="web_search", payload=payload)
        _append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    except Exception as exc:  # pragma: no cover
        env = ToolEnvelope(ok=False, tool="web_search", status="internal_error", message=str(exc))
        _append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)


def ah_knowledge_search(
    query: str, scope: str = "general", tool_context: AgentHordeToolContext | None = None
) -> dict[str, Any]:
    state = tool_state_from_context(tool_context)
    if state is None:
        return ToolEnvelope(
            ok=False,
            tool="knowledge_search",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    return _ah_knowledge_search(query, scope, state=state)


def ah_retrieve(
    query: str, scope: str = "all", limit: int = 24, tool_context: AgentHordeToolContext | None = None
) -> dict[str, Any]:
    state = tool_state_from_context(tool_context)
    if state is None:
        return ToolEnvelope(
            ok=False,
            tool="retrieve",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    return _ah_retrieve(query, scope, limit, state=state)


def ah_apply(
    patch_json: str,
    hypothesis_id: str = "",
    tool_context: AgentHordeToolContext | None = None,
) -> dict[str, Any]:
    state = tool_state_from_context(tool_context)
    if state is None:
        return ToolEnvelope(
            ok=False,
            tool="apply",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    return _ah_apply(patch_json, hypothesis_id, state=state)


def extend_cycle_cap(
    reason: str,
    uncovered_families: list[str] | None = None,
    tool_context: AgentHordeToolContext | None = None,
) -> dict[str, Any]:
    state = tool_state_from_context(tool_context)
    if state is None:
        return ToolEnvelope(
            ok=False,
            tool="extend_cycle_cap",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    return _extend_cycle_cap(reason, uncovered_families, state=state)


def worker_tools() -> list:
    """Tools for analyzer / red_team — all plain callables so ADK AFC stays enabled."""
    return [web_search, ah_knowledge_search, ah_retrieve, ah_apply]


def supervisor_tools() -> list:
    """Tools available to the supervisor_router (budget telemetry, cap extension)."""
    return [extend_cycle_cap]
