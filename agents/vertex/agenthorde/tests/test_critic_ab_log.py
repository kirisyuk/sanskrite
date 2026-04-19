from __future__ import annotations

import json
from pathlib import Path

import pytest

from agenthorde.critic.ab_log import CriticAttemptLog, append_ab_log


class TestCriticABLog:
    def test_writes_jsonl_entry(self, tmp_path: Path) -> None:
        log_path = tmp_path / "nested" / "critic_ab.jsonl"
        entry = CriticAttemptLog(
            cycle=2,
            provider="openrouter",
            model="z-ai/glm-5.1",
            ok=True,
            latency_ms=1234,
            input_tokens=100,
            output_tokens=50,
            verdict_sufficient=True,
            verdict_composition="go",
            critical_findings_count=0,
            advisory_findings_count=2,
            session_id="sess-abc",
        )
        append_ab_log(log_path, entry)

        assert log_path.exists()
        lines = log_path.read_text(encoding="utf-8").strip().splitlines()
        assert len(lines) == 1
        row = json.loads(lines[0])
        assert row["model"] == "z-ai/glm-5.1"
        assert row["ok"] is True
        assert row["verdict_sufficient"] is True
        assert row["session_id"] == "sess-abc"

    def test_appends_multiple_entries(self, tmp_path: Path) -> None:
        log_path = tmp_path / "ab.jsonl"
        for i in range(3):
            append_ab_log(
                log_path,
                CriticAttemptLog(
                    cycle=i,
                    provider="openrouter",
                    model="m",
                    ok=False,
                    latency_ms=i * 10,
                    error=f"err-{i}",
                ),
            )
        lines = log_path.read_text(encoding="utf-8").strip().splitlines()
        assert len(lines) == 3
        assert [json.loads(l)["error"] for l in lines] == ["err-0", "err-1", "err-2"]

    def test_empty_path_is_silent(self, tmp_path: Path) -> None:
        for empty in ("", None):
            append_ab_log(
                empty,  # type: ignore[arg-type]
                CriticAttemptLog(
                    cycle=0,
                    provider="openrouter",
                    model="m",
                    ok=True,
                    latency_ms=1,
                ),
            )
        # No file should be created next to tmp_path.
        assert list(tmp_path.iterdir()) == []

    def test_oserror_is_swallowed(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        def _boom(*_a, **_k):  # type: ignore[no-untyped-def]
            raise OSError("disk full")

        monkeypatch.setattr(Path, "mkdir", _boom)
        append_ab_log(
            tmp_path / "x.jsonl",
            CriticAttemptLog(cycle=0, provider="openrouter", model="m", ok=True, latency_ms=0),
        )
