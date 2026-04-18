from __future__ import annotations

import asyncio

from agenthorde_common.tools.openai_function_dispatch import build_standard_openai_tool_dispatch


def test_dispatch_web_search_delegates() -> None:
    called: dict[str, str] = {}

    async def run_web_search(q: str, state) -> dict:
        called["q"] = q
        return {"ok": True, "tool": "web_search", "payload": {"answer": "x"}}

    async def main() -> None:
        dispatch = build_standard_openai_tool_dispatch(run_web_search=run_web_search)
        state: dict = {}
        out = await dispatch("web_search", {"search_query": "nickel export"}, state)
        assert called["q"] == "nickel export"
        assert out["ok"] is True

    asyncio.run(main())


def test_dispatch_unknown_tool_envelope() -> None:
    async def _noop_ws(_q: str, _state) -> dict:
        return {"ok": True, "tool": "web_search", "payload": {}}

    async def main() -> None:
        dispatch = build_standard_openai_tool_dispatch(run_web_search=_noop_ws)
        out = await dispatch("nope", {}, {})
        assert out["ok"] is False
        assert out["tool"] == "nope"

    asyncio.run(main())
