"""Optional filtering of known-noisy Vertex / GenAI / ADK logs during CLI runs."""

from __future__ import annotations

import logging
import os
import warnings


class _GenAiResponsePartsFilter(logging.Filter):
    """GenAI logs when `.text` is read while non-text parts exist (normal during tool-calling loops)."""

    def filter(self, record: logging.LogRecord) -> bool:  # noqa: A003
        msg = record.getMessage()
        if "non-text parts in the response" in msg:
            return False
        if "multiple candidates in the response" in msg:
            return False
        return True


def install_known_vertex_noise_filters() -> None:
    """Call before importing `google.adk` / running agents to reduce stderr noise."""
    if os.getenv("AGENTHORDE_VERBOSE_VERTEX_LOGS", "").strip().lower() in ("1", "true", "yes", "on"):
        return

    warnings.filterwarnings(
        "ignore",
        category=UserWarning,
        module="google.adk.features._feature_decorator",
        message=r"\[EXPERIMENTAL\]",
    )
    # SDK uses logger name `google_genai.types` (see google/genai/types.py).
    logging.getLogger("google_genai.types").addFilter(_GenAiResponsePartsFilter())
