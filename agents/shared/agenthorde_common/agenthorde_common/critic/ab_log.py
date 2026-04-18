"""Append-only JSONL log of critic attempts for offline A/B comparison.

One line per attempt (Vertex + every OpenRouter fallback), regardless
of whether the attempt succeeded. Downstream tooling (`jq`, DuckDB,
pandas) can diff verdicts across models on the same query.
"""

from __future__ import annotations

import json
import logging
import os
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

_LOGGER = logging.getLogger(__name__)


@dataclass(slots=True)
class CriticAttemptLog:
    cycle: int
    provider: str
    model: str
    ok: bool
    latency_ms: int
    input_tokens: int = 0
    output_tokens: int = 0
    verdict_sufficient: bool | None = None
    verdict_composition: str | None = None
    blockers_count: int = 0
    non_blockers_count: int = 0
    error: str | None = None
    session_id: str = ""
    extra: dict[str, Any] = field(default_factory=dict)
    recorded_at: float = field(default_factory=time.time)


def append_ab_log(path: str | os.PathLike[str] | None, entry: CriticAttemptLog) -> None:
    """Append one JSONL entry. Silently skip if `path` is None or empty."""

    if not path:
        return
    p = Path(path)
    try:
        p.parent.mkdir(parents=True, exist_ok=True)
        with p.open("a", encoding="utf-8") as fh:
            fh.write(json.dumps(asdict(entry), ensure_ascii=False, default=str))
            fh.write("\n")
    except OSError as exc:  # pragma: no cover - log but never raise from a critic path.
        _LOGGER.warning("critic_ab_log write failed path=%s err=%s", p, exc)
