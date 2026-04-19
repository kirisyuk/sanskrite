"""Critic fallback orchestrator.

Sequence:
1. Attempt the Vertex-native ADK `critic_agent`. Yield all of its events
   to the outer stream so ADK-level telemetry is unchanged.
2. If the Vertex attempt fails (exception, empty state write, or the
   produced state is not a valid `CriticVerdict`), iterate over the
   configured OpenRouter chain. Each OpenRouter attempt either writes
   the parsed verdict into `state[STATE_CRITIC_OUTPUT_KEY]` and emits
   one synthetic ADK event, or logs a failure and moves on.

Every attempt (primary + each fallback) produces one JSONL line in the
critic A/B log so models can be compared on the same query.

The caller is responsible for deciding whether `last_critic.sufficient`
lets the supervisor finalize; this module only guarantees that
`state[STATE_CRITIC_OUTPUT_KEY]` ends up in one of three states:

- `CriticVerdict` object model-dumped to JSON (success on any rung)
- whatever ADK wrote (success on the Vertex rung)
- the previous value / absent (every rung failed)
"""

from __future__ import annotations

import logging
import time
from collections.abc import AsyncGenerator
from typing import Any

from google.adk.agents.invocation_context import InvocationContext  # pyright: ignore[reportMissingImports]
from google.adk.events import Event, EventActions  # pyright: ignore[reportMissingImports]
from google.genai import types as genai_types  # pyright: ignore[reportMissingImports]

from agenthorde.config import (
    CRITIC_AB_LOG_PATH,
    CRITIC_FALLBACK_MODELS,
    CRITIC_OR_MAX_OUTPUT_TOKENS,
    CRITIC_OR_MODEL_OVERRIDES,
    CRITIC_OR_STRICT,
    STATE_CRITIC_OUTPUT_KEY,
)
from agenthorde_common.critic.ab_log import CriticAttemptLog, append_ab_log
from agenthorde_common.critic.critic_chain import run_openrouter_critic_rung
from agenthorde_common.critic.openrouter_client import OpenRouterCriticClient, OpenRouterCriticError
from agenthorde_common.critic.prompt import render_critic_prompt
from agenthorde_common.critic.strict_schema import to_openai_strict_schema
from agenthorde_common.schemas.agents_io import CriticVerdict

_LOGGER = logging.getLogger(__name__)


def _safe_validate_verdict(raw: Any) -> CriticVerdict | None:
    if raw is None:
        return None
    try:
        if isinstance(raw, CriticVerdict):
            return raw
        return CriticVerdict.model_validate(raw)
    except Exception:
        return None


def _synth_event(
    *,
    author: str,
    state_delta: dict[str, Any],
    content_text: str,
) -> Event:
    return Event(
        author=author,
        actions=EventActions(state_delta=state_delta),
        content=genai_types.Content(
            parts=[genai_types.Part(text=content_text)],
            role="model",
        ),
    )


async def run_critic_with_fallback(
    ctx: InvocationContext,
    *,
    vertex_critic_agent: Any,
    cycle: int,
    session_id: str = "",
) -> AsyncGenerator[Event, None]:
    """Try Vertex critic first, then the OpenRouter chain."""

    state = ctx.session.state
    primary_started = time.perf_counter()
    primary_error: str | None = None
    try:
        async for event in vertex_critic_agent.run_async(ctx):
            yield event
    except Exception as exc:  # pragma: no cover - live Vertex only.
        primary_error = f"vertex_raise: {type(exc).__name__}: {exc}"
        _LOGGER.warning("critic vertex primary raised: %s", primary_error)

    primary_latency_ms = int((time.perf_counter() - primary_started) * 1000)
    primary_verdict = _safe_validate_verdict(state.get(STATE_CRITIC_OUTPUT_KEY))

    append_ab_log(
        CRITIC_AB_LOG_PATH,
        CriticAttemptLog(
            cycle=cycle,
            provider="vertex",
            model="gemini",
            ok=primary_verdict is not None,
            latency_ms=primary_latency_ms,
            verdict_sufficient=(bool(primary_verdict.sufficient) if primary_verdict else None),
            verdict_composition=(str(primary_verdict.composition_recommendation) if primary_verdict else None),
            critical_findings_count=len(primary_verdict.critical_findings) if primary_verdict else 0,
            advisory_findings_count=len(primary_verdict.advisory_findings) if primary_verdict else 0,
            error=primary_error or (None if primary_verdict else "no_valid_verdict_in_state"),
            session_id=session_id,
        ),
    )

    if primary_verdict is not None:
        return

    fallback_chain = [m for m in CRITIC_FALLBACK_MODELS if m.strip()]
    if not fallback_chain:
        _LOGGER.info("critic fallback chain is empty; nothing to retry")
        return

    try:
        client = OpenRouterCriticClient()
    except OpenRouterCriticError as exc:
        _LOGGER.warning("critic fallback skipped: %s", exc)
        append_ab_log(
            CRITIC_AB_LOG_PATH,
            CriticAttemptLog(
                cycle=cycle,
                provider="openrouter",
                model="<init>",
                ok=False,
                latency_ms=0,
                error=str(exc),
                session_id=session_id,
            ),
        )
        return

    prompt = render_critic_prompt(state)
    strict_schema = to_openai_strict_schema(CriticVerdict.model_json_schema(by_alias=True))

    for model in fallback_chain:
        override = CRITIC_OR_MODEL_OVERRIDES.get(model, {})
        temperature = override["temperature"] if "temperature" in override else 0.0
        extra_body = override.get("extra_body")
        outcome = await run_openrouter_critic_rung(
            client=client,
            model=model,
            prompt=prompt,
            schema=strict_schema,
            strict=CRITIC_OR_STRICT,
            max_output_tokens=CRITIC_OR_MAX_OUTPUT_TOKENS,
            temperature=temperature,
            extra_body=extra_body,
        )
        append_ab_log(
            CRITIC_AB_LOG_PATH,
            CriticAttemptLog(
                cycle=cycle,
                provider="openrouter",
                model=model,
                ok=outcome.ok,
                latency_ms=outcome.latency_ms,
                input_tokens=outcome.input_tokens,
                output_tokens=outcome.output_tokens,
                verdict_sufficient=(bool(outcome.verdict.sufficient) if outcome.verdict else None),
                verdict_composition=(str(outcome.verdict.composition_recommendation) if outcome.verdict else None),
                critical_findings_count=len(outcome.verdict.critical_findings) if outcome.verdict else 0,
                advisory_findings_count=(
                    len(outcome.verdict.advisory_findings) if outcome.verdict else 0
                ),
                error=outcome.error,
                session_id=session_id,
            ),
        )

        if outcome.ok and outcome.verdict is not None:
            verdict_payload = outcome.verdict.model_dump(mode="json", by_alias=True)
            state[STATE_CRITIC_OUTPUT_KEY] = verdict_payload
            yield _synth_event(
                author="critic_agent",
                state_delta={STATE_CRITIC_OUTPUT_KEY: verdict_payload},
                content_text=(
                    f"[critic via OpenRouter:{model}] "
                    f"sufficient={outcome.verdict.sufficient} "
                    f"composition={outcome.verdict.composition_recommendation}"
                ),
            )
            return

    _LOGGER.warning("critic fallback exhausted all %d OpenRouter models", len(fallback_chain))
