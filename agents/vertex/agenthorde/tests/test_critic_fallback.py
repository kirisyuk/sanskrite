"""Integration tests for the critic fallback orchestrator.

We don't spin up real Vertex / ADK here. Instead we inject:
- A minimal `FakeContext` that exposes `.session.state` and matches the
  shape `run_critic_with_fallback` expects.
- A `FakeVertexAgent` whose `run_async` is itself an async generator
  that either writes a valid verdict into state (happy-path) or writes
  garbage / raises.
- An `httpx.MockTransport` so `OpenRouterCriticClient` returns
  deterministic payloads per model in the fallback chain.
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncGenerator
from pathlib import Path
from typing import Any, Callable

import httpx
import pytest

from agenthorde.config import STATE_CRITIC_OUTPUT_KEY
from agenthorde.critic.fallback import run_critic_with_fallback


class _FakeSession:
    def __init__(self) -> None:
        self.state: dict[str, Any] = {}
        self.id = "sess-test"


class _FakeCtx:
    def __init__(self) -> None:
        self.session = _FakeSession()


class _FakeVertexAgent:
    """Emulates ADK `critic_agent.run_async(ctx)`.

    behavior:
      - "ok" writes a valid CriticVerdict (by alias) into state and yields nothing.
      - "garbage" writes non-schema-conformant JSON and yields nothing.
      - "raise" raises RuntimeError.
      - "silent" yields nothing and writes nothing.
    """

    def __init__(self, behavior: str) -> None:
        self.behavior = behavior
        self.calls = 0

    async def run_async(self, ctx: _FakeCtx) -> AsyncGenerator[Any, None]:
        self.calls += 1
        if self.behavior == "ok":
            ctx.session.state[STATE_CRITIC_OUTPUT_KEY] = {
                "sufficient": True,
                "criticalFindings": [],
                "advisoryFindings": [],
                "missionHint": "",
                "compositionRecommendation": "go",
            }
        elif self.behavior == "garbage":
            ctx.session.state[STATE_CRITIC_OUTPUT_KEY] = {"wrong": "shape"}
        elif self.behavior == "raise":
            raise RuntimeError("vertex quota exhausted")
        # "silent" does nothing at all.
        if False:  # pragma: no cover - generator type hint glue
            yield None


def _install_transport(
    monkeypatch: pytest.MonkeyPatch,
    handler: Callable[[httpx.Request], httpx.Response],
) -> list[dict[str, Any]]:
    captured: list[dict[str, Any]] = []

    def _wrap(req: httpx.Request) -> httpx.Response:
        captured.append(
            {"url": str(req.url), "body": json.loads(req.content or b"{}")}
        )
        return handler(req)

    transport = httpx.MockTransport(_wrap)
    orig = httpx.AsyncClient

    def _patched(*a: Any, **kw: Any) -> httpx.AsyncClient:
        kw["transport"] = transport
        return orig(*a, **kw)

    monkeypatch.setattr(httpx, "AsyncClient", _patched)
    return captured


def _common_env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> Path:
    log_path = tmp_path / "ab.jsonl"
    monkeypatch.setattr("agenthorde.critic.fallback.CRITIC_AB_LOG_PATH", str(log_path))
    monkeypatch.setenv("OPENROUTER_API_KEY", "test-key")
    return log_path


def _drain(
    agen: AsyncGenerator[Any, None],
) -> list[Any]:
    async def _run() -> list[Any]:
        out: list[Any] = []
        async for ev in agen:
            out.append(ev)
        return out

    return asyncio.run(_run())


class TestCriticFallback:
    def test_vertex_success_skips_fallback(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        log_path = _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS",
            ["z-ai/glm-5.1", "deepseek/deepseek-r1"],
        )

        captured = _install_transport(
            monkeypatch,
            lambda _r: httpx.Response(500, text="should not be called"),
        )
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("ok")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=1)  # type: ignore[arg-type]
        )

        assert vertex.calls == 1
        assert captured == []
        verdict = ctx.session.state[STATE_CRITIC_OUTPUT_KEY]
        assert verdict["sufficient"] is True

        lines = log_path.read_text(encoding="utf-8").strip().splitlines()
        assert len(lines) == 1
        row = json.loads(lines[0])
        assert row["provider"] == "vertex"
        assert row["ok"] is True

    def test_vertex_failure_falls_through_first_openrouter_model(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        log_path = _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS",
            ["z-ai/glm-5.1", "deepseek/deepseek-r1"],
        )

        verdict_json = json.dumps(
            {
                "sufficient": False,
                "criticalFindings": [
                    {
                        "summary": "dominant narrative not challenged",
                        "refs": [],
                        "gapClass": "methodology.framing_bias",
                    }
                ],
                "advisoryFindings": [],
                "missionHint": "probe X",
                "compositionRecommendation": "conditional",
            }
        )

        def handler(req: httpx.Request) -> httpx.Response:
            body = json.loads(req.content or b"{}")
            model = body.get("model")
            if model == "z-ai/glm-5.1":
                return httpx.Response(
                    200,
                    json={
                        "choices": [
                            {
                                "message": {"content": verdict_json},
                                "finish_reason": "stop",
                            }
                        ],
                        "usage": {"prompt_tokens": 33, "completion_tokens": 22},
                    },
                )
            return httpx.Response(500, text=f"should not reach {model}")

        captured = _install_transport(monkeypatch, handler)
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        events = _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=2)  # type: ignore[arg-type]
        )

        assert vertex.calls == 1
        assert len(captured) == 1, "second OR model must not be called once the first succeeds"
        assert captured[0]["body"]["model"] == "z-ai/glm-5.1"
        stored = ctx.session.state[STATE_CRITIC_OUTPUT_KEY]
        assert stored["sufficient"] is False
        assert stored["criticalFindings"] == [
            {
                "summary": "dominant narrative not challenged",
                "refs": [],
                "gapClass": "methodology.framing_bias",
            }
        ]
        assert stored["compositionRecommendation"] == "conditional"
        assert len(events) == 1

        rows = [
            json.loads(l)
            for l in log_path.read_text(encoding="utf-8").strip().splitlines()
        ]
        assert rows[0]["provider"] == "vertex" and rows[0]["ok"] is False
        assert rows[1]["provider"] == "openrouter"
        assert rows[1]["model"] == "z-ai/glm-5.1"
        assert rows[1]["ok"] is True
        assert rows[1]["input_tokens"] == 33
        assert rows[1]["output_tokens"] == 22

    def test_first_openrouter_http_error_tries_next(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        log_path = _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS",
            ["z-ai/glm-5.1", "deepseek/deepseek-r1"],
        )

        verdict_json = json.dumps(
            {
                "sufficient": True,
                "criticalFindings": [],
                "advisoryFindings": [],
                "missionHint": "",
                "compositionRecommendation": "go",
            }
        )

        def handler(req: httpx.Request) -> httpx.Response:
            body = json.loads(req.content or b"{}")
            model = body.get("model")
            if model == "z-ai/glm-5.1":
                return httpx.Response(429, text="rate limited")
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "message": {"content": verdict_json},
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {"prompt_tokens": 10, "completion_tokens": 5},
                },
            )

        captured = _install_transport(monkeypatch, handler)
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=3)  # type: ignore[arg-type]
        )

        assert [c["body"]["model"] for c in captured] == [
            "z-ai/glm-5.1",
            "deepseek/deepseek-r1",
        ]
        stored = ctx.session.state[STATE_CRITIC_OUTPUT_KEY]
        assert stored["sufficient"] is True

        rows = [
            json.loads(l)
            for l in log_path.read_text(encoding="utf-8").strip().splitlines()
        ]
        assert [r["model"] for r in rows] == [
            "gemini",
            "z-ai/glm-5.1",
            "deepseek/deepseek-r1",
        ]
        assert [r["ok"] for r in rows] == [False, False, True]
        assert "status=429" in (rows[1]["error"] or "")

    def test_schema_validation_failure_counts_as_error(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS", ["z-ai/glm-5.1"]
        )

        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "message": {"content": '{"unexpected":true}'},
                            "finish_reason": "stop",
                        }
                    ]
                },
            )

        _install_transport(monkeypatch, handler)
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=4)  # type: ignore[arg-type]
        )

        assert STATE_CRITIC_OUTPUT_KEY not in ctx.session.state

    def test_empty_fallback_chain_noop_after_vertex_failure(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr("agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS", [])

        captured = _install_transport(
            monkeypatch,
            lambda _r: httpx.Response(500, text="should not be called"),
        )
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=5)  # type: ignore[arg-type]
        )

        assert captured == []
        assert STATE_CRITIC_OUTPUT_KEY not in ctx.session.state

    def test_model_overrides_shape_request_body(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        """o4-mini override must omit `temperature` and inject `reasoning.effort=low`."""

        _common_env(monkeypatch, tmp_path)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS",
            ["openai/o4-mini", "z-ai/glm-5.1"],
        )
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_OR_MODEL_OVERRIDES",
            {
                "openai/o4-mini": {
                    "temperature": None,
                    "extra_body": {"reasoning": {"effort": "low"}},
                }
            },
        )
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_OR_MAX_OUTPUT_TOKENS", 6144
        )

        verdict_json = json.dumps(
            {
                "sufficient": True,
                "criticalFindings": [],
                "advisoryFindings": [],
                "missionHint": "",
                "compositionRecommendation": "go",
            }
        )

        def handler(_req: httpx.Request) -> httpx.Response:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "message": {"content": verdict_json},
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {"prompt_tokens": 10, "completion_tokens": 5},
                },
            )

        captured = _install_transport(monkeypatch, handler)
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=7)  # type: ignore[arg-type]
        )

        assert len(captured) == 1, "only the first OR rung should be called"
        body = captured[0]["body"]
        assert body["model"] == "openai/o4-mini"
        assert "temperature" not in body, "temperature must be omitted for o4-mini"
        assert body["reasoning"] == {"effort": "low"}
        assert body["max_tokens"] == 6144

    def test_missing_openrouter_key_logs_and_exits(
        self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path
    ) -> None:
        log_path = _common_env(monkeypatch, tmp_path)
        monkeypatch.delenv("OPENROUTER_API_KEY", raising=False)
        monkeypatch.setattr(
            "agenthorde.critic.fallback.CRITIC_FALLBACK_MODELS", ["z-ai/glm-5.1"]
        )

        captured = _install_transport(
            monkeypatch,
            lambda _r: httpx.Response(500, text="should not be called"),
        )
        ctx = _FakeCtx()
        vertex = _FakeVertexAgent("silent")

        _drain(
            run_critic_with_fallback(ctx, vertex_critic_agent=vertex, cycle=6)  # type: ignore[arg-type]
        )

        assert captured == []
        rows = [
            json.loads(l)
            for l in log_path.read_text(encoding="utf-8").strip().splitlines()
        ]
        assert any(r.get("model") == "<init>" for r in rows)
