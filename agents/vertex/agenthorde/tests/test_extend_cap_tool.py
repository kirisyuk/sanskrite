"""Unit tests for the extend_cycle_cap tool."""

from __future__ import annotations

from agenthorde.config import STATE_PACK_KEY, STATE_TOOL_LEDGER_KEY
from agenthorde_common.schemas.state import SessionPack
from agenthorde_common.tools.tool_context import state_as_tool_context
from agenthorde.tools.builtins import extend_cycle_cap


def _ctx(pack: SessionPack):
    state = {
        STATE_PACK_KEY: pack.model_dump(mode="json", by_alias=True),
        STATE_TOOL_LEDGER_KEY: [],
    }
    return state_as_tool_context(state)


def _pack(**kw) -> SessionPack:
    d = {"user_question": "Q", "supervisor_cap": 10, "initial_supervisor_cap": 10}
    d.update(kw)
    return SessionPack(**d)


def test_extend_cycle_cap_happy_path() -> None:
    ctx = _ctx(_pack())
    out = extend_cycle_cap(
        reason="uncovered sanctions family",
        uncovered_families=["sanctions"],
        tool_context=ctx,
    )
    assert out["ok"] is True
    payload = out["payload"]
    assert payload["extensionsUsed"] == 1
    assert payload["supervisorCap"] == 11
    assert payload["redTeamGate"] is True
    updated = SessionPack.model_validate(ctx.state[STATE_PACK_KEY])
    assert updated.extensions_pending_red_team is True
    assert updated.extensions_used == 1
    assert updated.supervisor_cap == 11


def test_extend_cycle_cap_blocks_after_three() -> None:
    ctx = _ctx(_pack(extensions_used=3, supervisor_cap=13))
    out = extend_cycle_cap(reason="x", tool_context=ctx)
    assert out["ok"] is False
    assert out["status"] == "validation_error"
    updated = SessionPack.model_validate(ctx.state[STATE_PACK_KEY])
    assert updated.extensions_used == 3
    assert updated.supervisor_cap == 13


def test_extend_cycle_cap_rounds_up_large_caps() -> None:
    ctx = _ctx(_pack(supervisor_cap=25, initial_supervisor_cap=25))
    out = extend_cycle_cap(reason="x", tool_context=ctx)
    assert out["ok"] is True
    assert out["payload"]["addedCycles"] == 3  # ceil(25 * 0.1)
    assert out["payload"]["supervisorCap"] == 28


def test_extend_cycle_cap_requires_context() -> None:
    out = extend_cycle_cap(reason="x", tool_context=None)
    assert out["ok"] is False
    assert out["status"] == "internal_error"
