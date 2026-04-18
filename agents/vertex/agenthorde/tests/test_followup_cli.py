"""Tests for --followup-from plumbing in runner_cli.py."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path


def _load_runner_cli_module():
    """Load runner_cli.py without triggering Vertex runtime imports at top level."""
    repo_root = Path(__file__).resolve().parents[1]
    path = repo_root / "runner_cli.py"
    spec = importlib.util.spec_from_file_location("_agenthorde_runner_cli_for_tests", str(path))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_load_followup_plan_extracts_blob(tmp_path) -> None:
    runner_cli = _load_runner_cli_module()
    prior_out = {
        "finalAnswer": "ok",
        "canonicalGraph": {
            "followUpPlan": {
                "knownAnchors": [
                    {"canonicalPath": "/org/tsmc", "fragmentClass": "org", "surfaceForms": ["TSMC"]}
                ],
                "deferredResearch": [
                    {"family": "sanctions", "reason": "shadow fleet"}
                ],
                "aggregateMentions": [],
                "counters": {"orphanCount": 0},
            }
        },
    }
    dump = tmp_path / "prior.json"
    dump.write_text(json.dumps(prior_out), encoding="utf-8")
    plan = runner_cli._load_followup_plan(str(dump))
    assert plan["knownAnchors"][0]["canonicalPath"] == "/org/tsmc"
    assert plan["deferredResearch"][0]["family"] == "sanctions"


def test_load_followup_plan_returns_empty_for_missing_fields(tmp_path) -> None:
    runner_cli = _load_runner_cli_module()
    dump = tmp_path / "no_plan.json"
    dump.write_text(json.dumps({"finalAnswer": "x"}), encoding="utf-8")
    plan = runner_cli._load_followup_plan(str(dump))
    assert plan == {}


def test_merge_followup_embeds_plan_into_question() -> None:
    runner_cli = _load_runner_cli_module()
    normalized = json.dumps({"question": "Investigate next leg"}, ensure_ascii=False)
    plan = {
        "knownAnchors": [{"canonicalPath": "/geo/egypt"}],
        "deferredResearch": [{"family": "sanctions"}],
        "aggregateMentions": [],
        "counters": {"orphanCount": 1},
    }
    merged = runner_cli._merge_followup_into_question(normalized, plan)
    obj = json.loads(merged)
    assert obj["question"] == "Investigate next leg"
    assert obj["priorFollowUpPlan"]["knownAnchors"] == [{"canonicalPath": "/geo/egypt"}]
    assert obj["priorFollowUpPlan"]["counters"]["orphanCount"] == 1


def test_merge_followup_passthrough_when_plan_empty() -> None:
    runner_cli = _load_runner_cli_module()
    normalized = json.dumps({"question": "X"}, ensure_ascii=False)
    assert runner_cli._merge_followup_into_question(normalized, {}) == normalized
