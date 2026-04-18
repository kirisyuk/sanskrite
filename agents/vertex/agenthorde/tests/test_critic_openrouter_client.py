from __future__ import annotations

import asyncio
import json
from typing import Any, Callable

import httpx
import pytest

from agenthorde.critic.openrouter_client import (
    OpenRouterCriticClient,
    OpenRouterCriticError,
)


def _install_mock_transport(
    monkeypatch: pytest.MonkeyPatch,
    handler: Callable[[httpx.Request], httpx.Response],
) -> list[dict[str, Any]]:
    captured: list[dict[str, Any]] = []

    def _wrapper(request: httpx.Request) -> httpx.Response:
        captured.append(
            {
                "url": str(request.url),
                "headers": {k.lower(): v for k, v in request.headers.items()},
                "body": json.loads(request.content or b"{}"),
            }
        )
        return handler(request)

    transport = httpx.MockTransport(_wrapper)
    orig = httpx.AsyncClient

    def _patched(*args: Any, **kwargs: Any) -> httpx.AsyncClient:
        kwargs["transport"] = transport
        return orig(*args, **kwargs)

    monkeypatch.setattr(httpx, "AsyncClient", _patched)
    monkeypatch.setenv("OPENROUTER_API_KEY", "test-key")
    monkeypatch.setenv("OPENROUTER_APP_TITLE", "sanskrite-test")
    monkeypatch.setenv("OPENROUTER_HTTP_REFERER", "https://example.local")
    return captured


class TestOpenRouterCriticClient:
    def test_strict_schema_request_shape(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "message": {"content": '{"sufficient": true}'},
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {"prompt_tokens": 11, "completion_tokens": 5},
                },
            )

        captured = _install_mock_transport(monkeypatch, handler)
        client = OpenRouterCriticClient(timeout_s=1.0)
        schema = {"type": "object", "properties": {"sufficient": {"type": "boolean"}}}

        async def _run() -> Any:
            return await client.chat_structured(
                model="z-ai/glm-5.1",
                prompt="eval",
                json_schema=schema,
                strict=True,
            )

        result = asyncio.run(_run())

        assert result.content == '{"sufficient": true}'
        assert result.input_tokens == 11
        assert result.output_tokens == 5
        assert result.finish_reason == "stop"
        assert len(captured) == 1
        req = captured[0]
        assert req["url"].endswith("/chat/completions")
        assert req["headers"]["authorization"] == "Bearer test-key"
        assert req["headers"]["x-title"] == "sanskrite-test"
        body = req["body"]
        assert body["model"] == "z-ai/glm-5.1"
        assert body["response_format"]["type"] == "json_schema"
        assert body["response_format"]["json_schema"]["strict"] is True
        assert body["response_format"]["json_schema"]["schema"] == schema

    def test_raises_on_non_200(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(429, text="rate limited")

        _install_mock_transport(monkeypatch, handler)
        client = OpenRouterCriticClient(timeout_s=1.0)

        async def _run() -> Any:
            return await client.chat_structured(
                model="z-ai/glm-5.1",
                prompt="eval",
                json_schema={"type": "object"},
            )

        with pytest.raises(OpenRouterCriticError, match="status=429"):
            asyncio.run(_run())

    def test_raises_on_truncated(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {"message": {"content": "partial"}, "finish_reason": "length"},
                    ]
                },
            )

        _install_mock_transport(monkeypatch, handler)
        client = OpenRouterCriticClient(timeout_s=1.0)

        async def _run() -> Any:
            return await client.chat_structured(
                model="z-ai/glm-5.1",
                prompt="eval",
                json_schema={"type": "object"},
            )

        with pytest.raises(OpenRouterCriticError, match="truncated"):
            asyncio.run(_run())

    def test_raises_on_empty_content(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {"message": {"content": "   "}, "finish_reason": "stop"},
                    ]
                },
            )

        _install_mock_transport(monkeypatch, handler)
        client = OpenRouterCriticClient(timeout_s=1.0)

        async def _run() -> Any:
            return await client.chat_structured(
                model="z-ai/glm-5.1",
                prompt="eval",
                json_schema={"type": "object"},
            )

        with pytest.raises(OpenRouterCriticError, match="empty content"):
            asyncio.run(_run())

    def test_init_without_key(self, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.delenv("OPENROUTER_API_KEY", raising=False)
        with pytest.raises(OpenRouterCriticError, match="OPENROUTER_API_KEY"):
            OpenRouterCriticClient()
