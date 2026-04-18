"""Azure OpenAI chat quirks: o-series / gpt-5 use ``max_completion_tokens`` instead of ``max_tokens``."""

from __future__ import annotations

# Per-call ceiling for worker/supervisor/critic style calls on Azure (reasoning + JSON can exceed a few K).
AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS = 8192


def uses_max_completion_tokens(deployment_name: str) -> bool:
    n = (deployment_name or "").strip().lower()
    return n.startswith("o") or "gpt-5" in n


def token_limit_kwargs(deployment_name: str, *, max_completion_tokens: int, max_tokens: int) -> dict[str, int]:
    """Return exactly one of the two mutually exclusive limit kwargs."""
    if uses_max_completion_tokens(deployment_name):
        return {"max_completion_tokens": max(max_completion_tokens, 16)}
    return {"max_tokens": max(max_tokens, 1)}


def chat_completion_budget_kwargs(deployment_name: str) -> dict[str, int]:
    """Default Azure chat/parse budget: 8k completion tokens (o/gpt-5) or 8k ``max_tokens`` (classic)."""
    return token_limit_kwargs(
        deployment_name,
        max_completion_tokens=AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS,
        max_tokens=AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS,
    )
