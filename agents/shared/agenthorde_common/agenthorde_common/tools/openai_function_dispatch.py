"""Standard AgentHorde worker tool names for OpenAI-style ``chat.completions`` function calling."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

from agenthorde_common.schemas.tools_io import ToolEnvelope
from agenthorde_common.tools.session_tools import (
    ah_apply,
    ah_domain_priors_lookup,
    ah_knowledge_search,
    ah_retrieve,
)

ToolDispatch = Callable[[str, dict[str, Any], MutableMapping[str, Any]], Awaitable[Any]]


def build_standard_openai_tool_dispatch(
    *,
    run_web_search: Callable[[str, MutableMapping[str, Any]], Awaitable[dict[str, Any]]],
) -> ToolDispatch:
    """Wire shared session tools + one async web_search implementation (Azure Responses, etc.)."""

    async def dispatch(name: str, args: dict[str, Any], state: MutableMapping[str, Any]) -> Any:
        if name == "web_search":
            return await run_web_search(str(args.get("search_query") or ""), state)
        if name == "knowledge_search":
            return ah_knowledge_search(
                str(args.get("query") or ""),
                str(args.get("scope") or "general"),
                state=state,
            )
        if name == "retrieve":
            return ah_retrieve(
                str(args.get("query") or ""),
                str(args.get("scope") or "all"),
                int(args.get("limit") or 24),
                state=state,
            )
        if name == "apply":
            return ah_apply(
                str(args.get("patch_json") or ""),
                str(args.get("hypothesis_id") or ""),
                state=state,
            )
        if name == "domain_priors_lookup":
            return ah_domain_priors_lookup(
                str(args.get("domain") or ""),
                str(args.get("query") or ""),
                state=state,
            )
        return ToolEnvelope(ok=False, tool=name, status="internal_error", message="unknown_tool").model_dump(
            mode="json",
            by_alias=True,
        )

    return dispatch
