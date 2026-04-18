"""Tool-assisted chat then structured parse (Pydantic), for Azure / OpenRouter."""

from __future__ import annotations

import json
import logging
import time
from collections import Counter
from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any, TypeVar

from pydantic import BaseModel

from agenthorde_common.llm.azure_chat_params import chat_completion_budget_kwargs, uses_max_completion_tokens

_LOG = logging.getLogger(__name__)

T = TypeVar("T", bound=BaseModel)

ToolDispatch = Callable[[str, dict[str, Any], MutableMapping[str, Any]], Awaitable[Any]]


def _msg_to_dict(msg: Any) -> dict[str, Any]:
    if hasattr(msg, "model_dump"):
        return msg.model_dump(exclude_none=True)
    raise TypeError(type(msg))


async def parse_structured_only(
    *,
    client: Any,
    model: str,
    system_preamble: str,
    user_message: str,
    response_model: type[T],
    temperature: float = 0.2,
) -> T:
    """Single-turn structured completion (no tools)."""
    parse_kw: dict[str, Any] = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_preamble},
            {"role": "user", "content": user_message},
        ],
        "response_format": response_model,
        **chat_completion_budget_kwargs(model),
    }
    if not uses_max_completion_tokens(model):
        parse_kw["temperature"] = temperature
    t0 = time.perf_counter()
    _LOG.info("parse_structured_only: request model=%s schema=%s", model, response_model.__name__)
    parsed = await client.beta.chat.completions.parse(**parse_kw)
    usage = getattr(parsed, "usage", None)
    _LOG.info(
        "parse_structured_only: response in %.1fs usage=%s",
        time.perf_counter() - t0,
        _usage_brief(usage),
    )
    out = parsed.choices[0].message.parsed
    if out is None:
        raise RuntimeError(f"structured_parse_empty:{response_model.__name__}")
    return out


def _usage_brief(usage: Any) -> str:
    if usage is None:
        return "n/a"
    pt = getattr(usage, "prompt_tokens", None)
    ct = getattr(usage, "completion_tokens", None)
    tt = getattr(usage, "total_tokens", None)
    return f"prompt={pt} completion={ct} total={tt}"


async def run_tool_loop_then_parse(
    *,
    client: Any,
    model: str,
    system_preamble: str,
    user_message: str,
    tools: list[dict[str, Any]],
    dispatch: ToolDispatch,
    state: MutableMapping[str, Any],
    response_model: type[T],
    temperature: float = 0.2,
    max_tool_rounds: int = 24,
    final_user_nudge: str = (
        "You have finished using tools. Output the final JSON object matching the "
        "required schema exactly. Do not call tools again."
    ),
) -> T:
    """Run chat with tools, then a structured parse into ``response_model``."""
    if not tools:
        return await parse_structured_only(
            client=client,
            model=model,
            system_preamble=system_preamble,
            user_message=user_message,
            response_model=response_model,
            temperature=temperature,
        )
    messages: list[dict[str, Any]] = [
        {"role": "system", "content": system_preamble},
        {"role": "user", "content": user_message},
    ]
    for round_i in range(max_tool_rounds):
        kwargs: dict[str, Any] = {
            "model": model,
            "messages": messages,
            **chat_completion_budget_kwargs(model),
        }
        if not uses_max_completion_tokens(model):
            kwargs["temperature"] = temperature
        if tools:
            kwargs["tools"] = tools
            kwargs["tool_choice"] = "auto"
        t0 = time.perf_counter()
        _LOG.info("tool_loop round %d: chat.completions.create model=%s msgs=%d", round_i, model, len(messages))
        resp = await client.chat.completions.create(**kwargs)
        elapsed = time.perf_counter() - t0
        usage = getattr(resp, "usage", None)
        choice = resp.choices[0]
        msg = choice.message
        d = _msg_to_dict(msg)
        messages.append(d)
        tool_calls = getattr(msg, "tool_calls", None) or d.get("tool_calls")
        if tool_calls:
            names = Counter(tc.function.name for tc in tool_calls)
            _LOG.info(
                "tool_loop round %d: llm %.1fs usage=%s tool_calls=%d (%s)",
                round_i,
                elapsed,
                _usage_brief(usage),
                len(tool_calls),
                dict(names),
            )
            for tc in tool_calls:
                fn = tc.function
                name = fn.name
                try:
                    args = json.loads(fn.arguments or "{}")
                except json.JSONDecodeError:
                    args = {}
                tool_t0 = time.perf_counter()
                _LOG.info(
                    "tool_loop round %d: dispatch tool=%s args_keys=%s",
                    round_i,
                    name,
                    list(args.keys()),
                )
                try:
                    result = await dispatch(name, args, state)
                except Exception as exc:  # pragma: no cover - defensive
                    _LOG.warning("tool %s failed: %s", name, exc)
                    result = {"ok": False, "error": str(exc)}
                _LOG.info(
                    "tool_loop round %d: tool=%s done in %.1fs ok=%s",
                    round_i,
                    name,
                    time.perf_counter() - tool_t0,
                    (result.get("ok") if isinstance(result, dict) else "?"),
                )
                content = result if isinstance(result, str) else json.dumps(result, ensure_ascii=False)
                messages.append({"role": "tool", "tool_call_id": tc.id, "content": content})
            continue
        _LOG.info(
            "tool_loop round %d: llm %.1fs usage=%s no_tool_calls -> structured parse",
            round_i,
            elapsed,
            _usage_brief(usage),
        )
        # No tool calls — try structured parse from current transcript
        messages.append({"role": "user", "content": final_user_nudge})
        parse_kw: dict[str, Any] = {
            "model": model,
            "messages": messages,
            "response_format": response_model,
            **chat_completion_budget_kwargs(model),
        }
        if not uses_max_completion_tokens(model):
            parse_kw["temperature"] = temperature
        parse_t0 = time.perf_counter()
        parsed = await client.beta.chat.completions.parse(**parse_kw)
        _LOG.info(
            "tool_loop round %d: parse done in %.1fs usage=%s",
            round_i,
            time.perf_counter() - parse_t0,
            _usage_brief(getattr(parsed, "usage", None)),
        )
        out = parsed.choices[0].message.parsed
        if out is None:
            raise RuntimeError(f"structured_parse_empty:{response_model.__name__}")
        return out
    raise RuntimeError("tool_loop_exhausted")
