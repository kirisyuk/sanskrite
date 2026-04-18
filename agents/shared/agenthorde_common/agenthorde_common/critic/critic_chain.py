"""OpenRouter critic attempt (shared by Vertex fallback and Azure)."""

from __future__ import annotations

import json
import time
from dataclasses import dataclass
from typing import Any

from agenthorde_common.critic.openrouter_client import OpenRouterCriticClient, OpenRouterCriticError
from agenthorde_common.schemas.agents_io import CriticVerdict


@dataclass(slots=True)
class OpenRouterCriticAttemptOutcome:
    ok: bool
    verdict: CriticVerdict | None
    error: str | None
    latency_ms: int
    input_tokens: int
    output_tokens: int


def _safe_validate_verdict(raw: Any) -> CriticVerdict | None:
    if raw is None:
        return None
    try:
        if isinstance(raw, CriticVerdict):
            return raw
        return CriticVerdict.model_validate(raw)
    except Exception:
        return None


async def run_openrouter_critic_rung(
    *,
    client: OpenRouterCriticClient,
    model: str,
    prompt: str,
    schema: dict[str, Any],
    strict: bool,
    max_output_tokens: int,
    temperature: float | None,
    extra_body: dict[str, Any] | None,
) -> OpenRouterCriticAttemptOutcome:
    started = time.perf_counter()
    try:
        result = await client.chat_structured(
            model=model,
            prompt=prompt,
            json_schema=schema,
            strict=strict,
            max_output_tokens=max_output_tokens,
            temperature=temperature,
            extra_body=extra_body,
        )
    except OpenRouterCriticError as exc:
        return OpenRouterCriticAttemptOutcome(
            ok=False,
            verdict=None,
            error=str(exc),
            latency_ms=int((time.perf_counter() - started) * 1000),
            input_tokens=0,
            output_tokens=0,
        )

    try:
        parsed = json.loads(result.content)
    except json.JSONDecodeError as exc:
        return OpenRouterCriticAttemptOutcome(
            ok=False,
            verdict=None,
            error=f"invalid_json: {exc}",
            latency_ms=result.latency_ms,
            input_tokens=result.input_tokens,
            output_tokens=result.output_tokens,
        )

    verdict = _safe_validate_verdict(parsed)
    if verdict is None:
        return OpenRouterCriticAttemptOutcome(
            ok=False,
            verdict=None,
            error="schema_validation_failed",
            latency_ms=result.latency_ms,
            input_tokens=result.input_tokens,
            output_tokens=result.output_tokens,
        )

    return OpenRouterCriticAttemptOutcome(
        ok=True,
        verdict=verdict,
        error=None,
        latency_ms=result.latency_ms,
        input_tokens=result.input_tokens,
        output_tokens=result.output_tokens,
    )
