from agenthorde_common.llm.azure_chat_params import (
    AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS,
    chat_completion_budget_kwargs,
    token_limit_kwargs,
    uses_max_completion_tokens,
)


def test_uses_max_completion_tokens_o_series() -> None:
    assert uses_max_completion_tokens("o4-mini") is True
    assert uses_max_completion_tokens("gpt-5") is True
    assert uses_max_completion_tokens("gpt-4o") is False


def test_token_limit_kwargs_branch() -> None:
    assert "max_completion_tokens" in token_limit_kwargs("o4-mini", max_completion_tokens=100, max_tokens=50)
    assert "max_tokens" in token_limit_kwargs("gpt-4o", max_completion_tokens=100, max_tokens=50)


def test_chat_completion_budget_default_8k() -> None:
    o = chat_completion_budget_kwargs("o4-mini")
    assert o == {"max_completion_tokens": AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS}
    g = chat_completion_budget_kwargs("gpt-4o")
    assert g == {"max_tokens": AZURE_REASONING_MODEL_MAX_COMPLETION_TOKENS}
