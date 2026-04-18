"""Azure OpenAI critic with the same OpenRouter fallback chain as Vertex."""

from __future__ import annotations

import logging
import time
from typing import Any, MutableMapping

from agenthorde_common.runtime.state_keys import STATE_CRITIC_OUTPUT_KEY

from agenthorde_azure.config import (
    CRITIC_AB_LOG_PATH,
    CRITIC_FALLBACK_MODELS,
    CRITIC_OR_MAX_OUTPUT_TOKENS,
    CRITIC_OR_MODEL_OVERRIDES,
    CRITIC_OR_STRICT,
)
from agenthorde_common.critic.ab_log import CriticAttemptLog, append_ab_log
from agenthorde_common.critic.critic_chain import run_openrouter_critic_rung
from agenthorde_common.critic.openrouter_client import OpenRouterCriticClient, OpenRouterCriticError
from agenthorde_common.critic.prompt import render_critic_prompt
from agenthorde_common.critic.strict_schema import to_openai_strict_schema
from agenthorde_common.llm.openai_tool_definitions import CRITIC_OPENAI_TOOLS
from agenthorde_common.llm.openai_tool_loop import run_tool_loop_then_parse
from agenthorde_common.schemas.agents_io import CriticVerdict

from agenthorde_azure.openai_dispatch import build_tool_dispatch

_LOG = logging.getLogger(__name__)


async def run_critic_azure_then_openrouter(
    *,
    client: Any,
    critic_deployment: str,
    worker_deployment: str,
    state: MutableMapping[str, Any],
    cycle: int,
    session_id: str = "",
) -> None:
    """Primary: Azure OpenAI with tools (same prompt as Vertex). Fallback: OpenRouter chain."""
    primary_started = time.perf_counter()
    primary_error: str | None = None
    prompt = render_critic_prompt(state, tools_disabled=False)
    dispatch = build_tool_dispatch(client, worker_deployment)
    preamble = (
        "You are the critic_agent. Use tools sparingly; prefer pack + ledger. "
        "Finish with structured CriticVerdict only."
    )
    _LOG.info(
        "critic primary(azure): cycle=%d deployment=%s prompt_chars=%d",
        cycle,
        critic_deployment,
        len(prompt),
    )
    try:
        verdict = await run_tool_loop_then_parse(
            client=client,
            model=critic_deployment,
            system_preamble=preamble,
            user_message=prompt,
            tools=CRITIC_OPENAI_TOOLS,
            dispatch=dispatch,
            state=state,
            response_model=CriticVerdict,
            temperature=0.1,
        )
        state[STATE_CRITIC_OUTPUT_KEY] = verdict.model_dump(mode="json", by_alias=True)
    except Exception as exc:
        primary_error = f"azure_critic_raise: {type(exc).__name__}: {exc}"
        _LOG.warning("critic azure primary failed: %s", primary_error)

    primary_latency_ms = int((time.perf_counter() - primary_started) * 1000)
    try:
        primary_verdict = CriticVerdict.model_validate(state.get(STATE_CRITIC_OUTPUT_KEY))
    except Exception:
        primary_verdict = None

    append_ab_log(
        CRITIC_AB_LOG_PATH,
        CriticAttemptLog(
            cycle=cycle,
            provider="azure_openai",
            model=critic_deployment,
            ok=primary_verdict is not None,
            latency_ms=primary_latency_ms,
            verdict_sufficient=(bool(primary_verdict.sufficient) if primary_verdict else None),
            verdict_composition=(str(primary_verdict.composition_recommendation) if primary_verdict else None),
            blockers_count=len(primary_verdict.blockers) if primary_verdict else 0,
            non_blockers_count=len(primary_verdict.non_blockers) if primary_verdict else 0,
            error=primary_error or (None if primary_verdict else "no_valid_verdict_in_state"),
            session_id=session_id,
        ),
    )

    _LOG.info(
        "critic primary(azure): %.1fs ok=%s err=%s",
        primary_latency_ms / 1000.0,
        primary_verdict is not None,
        primary_error,
    )
    if primary_verdict is not None:
        return

    fallback_chain = [m for m in CRITIC_FALLBACK_MODELS if m.strip()]
    if not fallback_chain:
        _LOG.info("critic fallback chain empty; giving up")
        return
    _LOG.info("critic fallback(openrouter) chain: %s", fallback_chain)

    try:
        or_client = OpenRouterCriticClient()
    except OpenRouterCriticError as exc:
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

    or_prompt = render_critic_prompt(state, tools_disabled=True)
    strict_schema = to_openai_strict_schema(CriticVerdict.model_json_schema(by_alias=True))

    for model in fallback_chain:
        override = CRITIC_OR_MODEL_OVERRIDES.get(model, {})
        temperature = override["temperature"] if "temperature" in override else 0.0
        extra_body = override.get("extra_body")
        _LOG.info("critic fallback: trying model=%s", model)
        outcome = await run_openrouter_critic_rung(
            client=or_client,
            model=model,
            prompt=or_prompt,
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
                blockers_count=len(outcome.verdict.blockers) if outcome.verdict else 0,
                non_blockers_count=(len(outcome.verdict.non_blockers) if outcome.verdict else 0),
                error=outcome.error,
                session_id=session_id,
            ),
        )
        _LOG.info(
            "critic fallback: model=%s %.1fs ok=%s err=%s",
            model,
            outcome.latency_ms / 1000.0,
            outcome.ok,
            outcome.error,
        )
        if outcome.ok and outcome.verdict is not None:
            state[STATE_CRITIC_OUTPUT_KEY] = outcome.verdict.model_dump(mode="json", by_alias=True)
            return
    _LOG.info("critic fallback exhausted all %d OpenRouter models", len(fallback_chain))
