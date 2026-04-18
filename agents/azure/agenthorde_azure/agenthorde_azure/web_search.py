"""Azure / Microsoft Foundry: grounded public web via OpenAI Responses API (``web_search`` tool), not Bing v7."""

from __future__ import annotations

import logging
import time
from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

_LOG = logging.getLogger("agenthorde_azure.web_search")

from agenthorde_common.llm.azure_chat_params import chat_completion_budget_kwargs, uses_max_completion_tokens
from agenthorde_common.schemas.tools_io import ToolEnvelope
from agenthorde_common.tools.grounded_web_common import format_grounded_web_fetcher_prompt
from agenthorde_common.tools.openai_responses_web import citations_from_openai_response, output_text_from_openai_response
from agenthorde_common.tools.tool_ledger import append_tool_ledger


async def model_only_answer(client: Any, deployment: str, query: str) -> str:
    """Fallback when Responses + web_search is unavailable (API version, policy, or transport)."""
    kw: dict = {
        "model": deployment,
        "messages": [
            {
                "role": "system",
                "content": (
                    "You are a research assistant without live web access. Summarize what is "
                    "widely known about the query; flag uncertainty. Do not invent specific URLs."
                ),
            },
            {"role": "user", "content": query},
        ],
        **chat_completion_budget_kwargs(deployment),
    }
    if not uses_max_completion_tokens(deployment):
        kw["temperature"] = 0.2
    resp = await client.chat.completions.create(**kw)
    return (resp.choices[0].message.content or "").strip()


def make_azure_grounded_web_search(
    client: Any,
    worker_deployment: str,
) -> Callable[[str, MutableMapping[str, Any]], Awaitable[dict[str, Any]]]:
    """Curried async (search_query, state) -> ToolEnvelope dict for ``build_standard_openai_tool_dispatch``."""

    async def run_web_search(search_query: str, state: MutableMapping[str, Any]) -> dict[str, Any]:
        return await _grounded_web_search_impl(
            client=client,
            worker_deployment=worker_deployment,
            search_query=search_query,
            state=state,
        )

    return run_web_search


async def _grounded_web_search_impl(
    *,
    client: Any,
    worker_deployment: str,
    search_query: str,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    q = (search_query or "").strip()
    if not q:
        env = ToolEnvelope(ok=False, tool="web_search", status="validation_error", message="empty search_query")
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    t0 = time.perf_counter()
    _LOG.info("web_search: responses.create q=%r deployment=%s", q[:160], worker_deployment)
    try:
        if not hasattr(client, "responses"):
            raise RuntimeError("openai_client_missing_responses_api")
        create_kwargs: dict[str, Any] = {
            "model": worker_deployment,
            "tools": [{"type": "web_search"}],
            "tool_choice": "auto",
            "input": format_grounded_web_fetcher_prompt(q),
            "max_output_tokens": 3000,
        }
        if not uses_max_completion_tokens(worker_deployment):
            create_kwargs["temperature"] = 0.2
        resp = await client.responses.create(**create_kwargs)
        _LOG.info(
            "web_search: responses.create done in %.1fs status=%s",
            time.perf_counter() - t0,
            getattr(resp, "status", "?"),
        )
        err = getattr(resp, "error", None)
        if err is not None:
            msg = getattr(err, "message", None) or str(err)
            raise RuntimeError(f"responses_error:{msg}")
        answer = output_text_from_openai_response(resp).strip()
        sources = citations_from_openai_response(resp)
        if not answer:
            answer = await model_only_answer(client, worker_deployment, format_grounded_web_fetcher_prompt(q))
            payload = {
                "answer": answer,
                "sources": sources,
                "groundingMode": "degraded_empty_output_text",
            }
        else:
            payload = {"answer": answer, "sources": sources, "groundingMode": "azure_openai_responses_web_search"}
        env = ToolEnvelope(ok=True, tool="web_search", payload=payload)
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    except Exception as exc:
        _LOG.warning(
            "web_search: responses.create failed after %.1fs: %s (falling back to model_only)",
            time.perf_counter() - t0,
            exc,
        )
        try:
            answer = await model_only_answer(client, worker_deployment, format_grounded_web_fetcher_prompt(q))
            payload = {
                "answer": answer,
                "sources": [],
                "groundingMode": "model_only_fallback",
                "detail": str(exc),
            }
            env = ToolEnvelope(ok=True, tool="web_search", payload=payload)
        except Exception as exc2:  # pragma: no cover - defensive
            env = ToolEnvelope(ok=False, tool="web_search", status="internal_error", message=f"{exc};{exc2}")
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)


async def web_search_tool(
    *,
    client: Any,
    worker_deployment: str,
    search_query: str,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    """Backward-compatible name used by older call sites."""
    return await _grounded_web_search_impl(
        client=client,
        worker_deployment=worker_deployment,
        search_query=search_query,
        state=state,
    )
