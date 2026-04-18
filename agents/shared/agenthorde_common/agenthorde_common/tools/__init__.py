"""Shared AgentHorde tools: session pack ops, OpenAI function dispatch, grounded-web helpers."""

# Order: no dependency on ``openai_function_dispatch`` before ``session_tools`` is available.
from agenthorde_common.tools.grounded_web_common import format_grounded_web_fetcher_prompt
from agenthorde_common.tools.openai_function_dispatch import build_standard_openai_tool_dispatch
from agenthorde_common.tools.openai_responses_web import citations_from_openai_response, output_text_from_openai_response
from agenthorde_common.tools.session_tools import ah_apply, ah_knowledge_search, ah_retrieve, extend_cycle_cap
from agenthorde_common.tools.tool_context import (
    AgentHordeToolContext,
    MutableStateToolContext,
    state_as_tool_context,
    tool_state_from_context,
)
from agenthorde_common.tools.tool_ledger import append_tool_ledger

__all__ = [
    "AgentHordeToolContext",
    "MutableStateToolContext",
    "ah_apply",
    "ah_knowledge_search",
    "ah_retrieve",
    "append_tool_ledger",
    "build_standard_openai_tool_dispatch",
    "citations_from_openai_response",
    "extend_cycle_cap",
    "format_grounded_web_fetcher_prompt",
    "output_text_from_openai_response",
    "state_as_tool_context",
    "tool_state_from_context",
]
