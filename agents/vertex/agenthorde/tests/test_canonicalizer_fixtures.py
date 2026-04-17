"""Lightweight checks on bundled question fixtures (used by smoke scripts)."""

from __future__ import annotations

import json
from pathlib import Path

FIXTURES = Path(__file__).resolve().parents[1] / "fixtures"


def test_lithium_fixture_json() -> None:
    p = FIXTURES / "lithium_supply_sa_to_de.json"
    obj = json.loads(p.read_text(encoding="utf-8"))
    assert "question" in obj
    assert len(str(obj["question"])) > 20


def test_malacca_fixture_json() -> None:
    p = FIXTURES / "malacca_strait_resources.json"
    obj = json.loads(p.read_text(encoding="utf-8"))
    assert "question" in obj
    q = str(obj["question"]).lower()
    assert "малакк" in q or "strait" in q or "пролив" in q
