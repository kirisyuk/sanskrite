"""Map OpenAI tool names to AgentHorde session tools (shared pack / ledger)."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

from agenthorde_common.tools.openai_function_dispatch import build_standard_openai_tool_dispatch

from agenthorde_azure.web_search import make_azure_grounded_web_search


def build_tool_dispatch(
    client: Any,
    worker_deployment: str,
) -> Callable[[str, dict[str, Any], MutableMapping[str, Any]], Awaitable[Any]]:
    run_web_search = make_azure_grounded_web_search(client, worker_deployment)
    return build_standard_openai_tool_dispatch(run_web_search=run_web_search)
