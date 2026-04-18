"""LLM client env aliases and skip behavior."""

from __future__ import annotations

import asyncio

import pytest

from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient


def test_canonicalizer_skip_env_alias(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("CANONICALIZER_SKIP", "1")
    monkeypatch.delenv("AGENTHORDE_CANONICALIZER_SKIP", raising=False)
    client = LLMPhaseClient()

    async def _run() -> dict:
        return await client.generate_json("4", "gemini-test", "{}")

    assert asyncio.run(_run()) == {}


def test_mock_response_bypasses_skip(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("CANONICALIZER_SKIP", "1")
    client = LLMPhaseClient(mock_responses={"4": {"relations": []}})

    async def _run() -> dict:
        return await client.generate_json("4", "m", "{}")

    assert asyncio.run(_run()) == {"relations": []}
