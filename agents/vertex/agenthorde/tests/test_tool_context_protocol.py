from __future__ import annotations

from types import SimpleNamespace

from agenthorde_common.tools.tool_context import (
    AgentHordeToolContext,
    MutableStateToolContext,
    state_as_tool_context,
    tool_state_from_context,
)


def test_tool_state_from_context_accepts_mutable_state_tool_context() -> None:
    ctx = MutableStateToolContext(state={"k": 1})
    assert tool_state_from_context(ctx) == {"k": 1}
    assert isinstance(ctx, AgentHordeToolContext)


def test_tool_state_from_context_accepts_simple_namespace() -> None:
    ctx = SimpleNamespace(state={"a": 2})
    assert tool_state_from_context(ctx) == {"a": 2}


def test_tool_state_from_context_rejects_bad_state() -> None:
    class Bad:
        state = "not-a-mapping"

    assert tool_state_from_context(Bad()) is None


def test_state_as_tool_context_roundtrip() -> None:
    d: dict = {"x": 3}
    tc = state_as_tool_context(d)
    assert tool_state_from_context(tc) is d
