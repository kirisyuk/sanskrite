"""Append-only session tool ledger (shared by Vertex wrappers and Azure OpenAI dispatch)."""

from __future__ import annotations

from collections.abc import MutableMapping
from typing import Any

from agenthorde_common.runtime.state_keys import STATE_TOOL_LEDGER_KEY


def append_tool_ledger(state: MutableMapping[str, Any], entry: dict[str, Any]) -> None:
    led = state.get(STATE_TOOL_LEDGER_KEY)
    if not isinstance(led, list):
        led = []
    led.append(entry)
    state[STATE_TOOL_LEDGER_KEY] = led
