"""Minimal Azure-side orchestration (no ADK): probe + hooks for a future supervisor loop."""

from __future__ import annotations

from agenthorde_common.llm.azure_chat_params import chat_completion_budget_kwargs, uses_max_completion_tokens

from agenthorde_azure.azure_openai_client import azure_client
from agenthorde_azure.config import AZURE_OPENAI_DEPLOYMENT


async def probe_deployment() -> str:
    """Single chat completion to verify endpoint, key, deployment, and api-version."""
    client = azure_client()
    kwargs: dict = {
        "model": AZURE_OPENAI_DEPLOYMENT,
        "messages": [
            {"role": "system", "content": "Reply with exactly: ok"},
            {"role": "user", "content": "ping"},
        ],
        **chat_completion_budget_kwargs(AZURE_OPENAI_DEPLOYMENT),
    }
    if not uses_max_completion_tokens(AZURE_OPENAI_DEPLOYMENT):
        kwargs["temperature"] = 0
    resp = await client.chat.completions.create(**kwargs)
    choice = resp.choices[0].message
    return (choice.content or "").strip()
