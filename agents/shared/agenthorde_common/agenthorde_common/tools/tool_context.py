"""Tool execution context: one mutable ``state`` bag shared across platforms.

``AgentHordeToolContext`` is intentionally minimal so hosts can adapt without
importing Vertex ADK or Azure SDK:

* **Vertex / Google ADK** — the framework’s ``ToolContext`` already exposes a
  ``state`` mapping; it structurally satisfies this protocol.
* **Azure OpenAI** — the async dispatch path passes ``state`` directly; for tests
  or bridges use :func:`state_as_tool_context`.
* **MCP (Model Context Protocol)** — an MCP server tool handler typically receives
  a framework ``Context`` plus JSON arguments. Map your per-session store (or MCP
  ``session`` metadata you control) into a ``MutableMapping`` and expose it as
  ``ctx.state`` on a small object, or use :func:`state_as_tool_context`. AgentHorde
  does not depend on the ``mcp`` package; this shape stays stable if you later
  wrap MCP tools or expose AgentHorde tools *as* MCP tools behind the same state.
"""

from __future__ import annotations

from collections.abc import MutableMapping
from dataclasses import dataclass
from typing import Any, Protocol, runtime_checkable


@runtime_checkable
class AgentHordeToolContext(Protocol):
    """Anything with a mutable ``state`` dict (session pack, ledger keys, …)."""

    state: MutableMapping[str, Any]


@dataclass(slots=True)
class MutableStateToolContext:
    """Explicit carrier used in tests and non-ADK bridges (e.g. MCP → AgentHorde)."""

    state: MutableMapping[str, Any]


def state_as_tool_context(state: MutableMapping[str, Any]) -> MutableStateToolContext:
    """Wrap a bare session ``state`` for APIs that expect :class:`AgentHordeToolContext`."""
    return MutableStateToolContext(state=state)


def tool_state_from_context(tool_context: Any) -> MutableMapping[str, Any] | None:
    """Resolve ``tool_context.state`` if present and mapping-like; else ``None``."""
    if tool_context is None:
        return None
    raw = getattr(tool_context, "state", None)
    if isinstance(raw, MutableMapping):
        return raw
    return None
