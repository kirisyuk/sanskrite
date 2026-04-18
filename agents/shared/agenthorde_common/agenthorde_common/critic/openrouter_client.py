"""Thin async OpenRouter client specialised for the critic path.

Only the OpenAI-compatible `/chat/completions` endpoint is used, with
`response_format: json_schema` (strict mode by default). This is
intentionally a narrow dependency: we do not require the full Go
AITaskExecutor routing machinery yet. See `README.md` roadmap entry
"migrate critic to /api/v1/ai/critic via AITaskExecutor" for the
planned consolidation.
"""

from __future__ import annotations

import json
import logging
import os
from dataclasses import dataclass
from typing import Any

try:  # pragma: no cover - import guard only.
    import httpx
except ImportError:  # pragma: no cover - surfaced at runtime if the package is missing.
    httpx = None  # type: ignore[assignment]

_LOGGER = logging.getLogger(__name__)

OPENROUTER_API_BASE = "https://openrouter.ai/api/v1"


def _strip_markdown_fences(content: str) -> str:
    """Unwrap ```json ... ``` (or bare ``` ... ```) emitted by providers that
    ignore ``response_format`` and serialize JSON inside a code fence.

    Observed in the free-tier bench (z-ai/glm-4.5-air, occasionally other
    GLM-class providers). Idempotent: returns ``content`` unchanged when no
    fence is present.
    """

    s = content.strip()
    if not s.startswith("```"):
        return content
    lines = s.splitlines()
    if not lines:
        return content
    first = lines[0].strip()
    if first not in ("```", "```json", "```JSON"):
        return content
    body = lines[1:]
    while body and body[-1].strip() == "":
        body.pop()
    if body and body[-1].strip() == "```":
        body.pop()
    return "\n".join(body).strip() or content


class OpenRouterCriticError(RuntimeError):
    """Raised for any OpenRouter-side failure (HTTP, empty body, bad JSON)."""


@dataclass(slots=True)
class OpenRouterChatResult:
    model: str
    content: str
    input_tokens: int
    output_tokens: int
    finish_reason: str
    latency_ms: int


class OpenRouterCriticClient:
    """Minimal async client: one chat-completion call with optional strict JSON schema."""

    def __init__(
        self,
        *,
        api_key: str | None = None,
        base_url: str = OPENROUTER_API_BASE,
        timeout_s: float = 180.0,
        app_title: str | None = None,
        referer: str | None = None,
    ) -> None:
        if httpx is None:  # pragma: no cover
            raise RuntimeError(
                "httpx is required for OpenRouterCriticClient; add it to requirements.txt"
            )
        key = (api_key or os.getenv("OPENROUTER_API_KEY") or "").strip().strip("\"'")
        if not key:
            raise OpenRouterCriticError(
                "OPENROUTER_API_KEY is not set; the critic OpenRouter fallback is disabled"
            )
        self._api_key = key
        self._base_url = base_url.rstrip("/")
        self._timeout_s = timeout_s
        self._app_title = (app_title or os.getenv("OPENROUTER_APP_TITLE") or "").strip()
        self._referer = (referer or os.getenv("OPENROUTER_HTTP_REFERER") or "").strip()

    def _headers(self) -> dict[str, str]:
        h = {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }
        if self._app_title:
            h["X-Title"] = self._app_title
        if self._referer:
            h["Referer"] = self._referer
        return h

    async def chat_structured(
        self,
        *,
        model: str,
        prompt: str,
        json_schema: dict[str, Any] | None,
        strict: bool = True,
        max_output_tokens: int = 4096,
        temperature: float | None = 0.0,
        extra_body: dict[str, Any] | None = None,
    ) -> OpenRouterChatResult:
        """Run one chat completion with optional strict-schema output.

        Parameters
        ----------
        temperature:
            Pass ``None`` to omit temperature from the body (required for
            OpenAI reasoning models such as ``o4-mini`` which reject
            non-default temperature values).
        extra_body:
            Merged into the request body as-is. Intended for provider
            extensions such as ``reasoning={"effort": "low"}`` (OpenRouter
            Responses API Beta, also accepted on chat completions for
            OpenAI reasoning models).

        Raises
        ------
        OpenRouterCriticError
            For any non-2xx HTTP response, truncated answer, or empty body.
        """

        import time

        body: dict[str, Any] = {
            "model": model,
            "messages": [
                {"role": "user", "content": prompt},
            ],
            "max_tokens": int(max_output_tokens),
        }
        if temperature is not None:
            body["temperature"] = float(temperature)
        if extra_body:
            for k, v in extra_body.items():
                body[k] = v
        if json_schema is not None:
            body["response_format"] = {
                "type": "json_schema",
                "json_schema": {
                    "name": "CriticVerdict",
                    "strict": bool(strict),
                    "schema": json_schema,
                },
            }
        elif os.getenv("AGENTHORDE_CRITIC_JSON_OBJECT_FALLBACK", "1") == "1":
            body["response_format"] = {"type": "json_object"}

        url = f"{self._base_url}/chat/completions"
        started = time.perf_counter()
        async with httpx.AsyncClient(timeout=self._timeout_s) as client:  # type: ignore[union-attr]
            resp = await client.post(url, headers=self._headers(), content=json.dumps(body))
        latency_ms = int((time.perf_counter() - started) * 1000)

        if resp.status_code != 200:
            snippet = resp.text[:600] if resp.text else ""
            raise OpenRouterCriticError(
                f"openrouter status={resp.status_code} model={model} body={snippet}"
            )

        try:
            payload = resp.json()
        except json.JSONDecodeError as exc:
            raise OpenRouterCriticError(f"openrouter non-json body for model={model}") from exc

        choices = payload.get("choices") or []
        if not choices:
            raise OpenRouterCriticError(f"openrouter empty choices for model={model}")

        msg = (choices[0] or {}).get("message") or {}
        content = (msg.get("content") or "").strip()
        finish = (choices[0] or {}).get("finish_reason") or ""
        if finish == "length":
            raise OpenRouterCriticError(
                f"openrouter response truncated (finish_reason=length) model={model}"
            )
        if not content:
            raise OpenRouterCriticError(f"openrouter empty content for model={model}")
        content = _strip_markdown_fences(content)

        usage = payload.get("usage") or {}
        input_tokens = int(usage.get("prompt_tokens") or 0)
        output_tokens = int(usage.get("completion_tokens") or 0)

        return OpenRouterChatResult(
            model=model,
            content=content,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            finish_reason=str(finish),
            latency_ms=latency_ms,
        )
