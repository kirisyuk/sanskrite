"""Phase 1: adaptive chunking over the analyzer registry.

These tests check that:
  * the registry is split into chunks and a separate prose pass is issued;
  * when a chunk returns empty mentions, it is recursively halved;
  * when the recursion bottoms out (depth or size=1), the chunk is given up
    silently and coverage gap + unknown-type bookkeeping are still correct;
  * post-merge dedup keeps exactly one mention per agentId and drops free-prose
    mentions whose surface matches a registry label.
"""

from __future__ import annotations

import asyncio
from typing import Any

from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.phases import phase1_entities as p1
from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.schemas import Candidates


def _ws_with_registry(ids: list[tuple[str, str]]) -> WorkingState:
    return WorkingState(
        analyzer_entities=[
            {"namespacedId": ns, "label": label, "origin": "analyzer"}
            for ns, label in ids
        ],
        final_answer="x",
        working_graph_text="",
    )


class _ScriptedLLM(LLMPhaseClient):
    """Captures call payloads and returns responses based on the agentId subset.

    Each call carries its ``analyzer_entities`` slice — we use that slice to
    decide which mentions to return, simulating a real LLM that respects the
    partial call contract.
    """

    def __init__(self, behavior: dict[str, Any] | None = None) -> None:
        super().__init__()
        self.calls: list[dict[str, Any]] = []
        self.behavior = behavior or {}

    async def generate_json(  # type: ignore[override]
        self,
        phase_key: str,
        model: str,
        user_payload: str,
        extra_system: str = "",
    ) -> dict[str, Any]:
        import json as _json

        payload = _json.loads(user_payload)
        slice_ids = [row["agentId"] for row in payload.get("analyzer_entities") or []]
        self.calls.append({"slice_ids": list(slice_ids), "extra": extra_system})
        if not slice_ids:
            return dict(self.behavior.get("prose", {"mentions": []}))
        matcher = self.behavior.get("__empty_on_slice__")
        if callable(matcher) and matcher(slice_ids):
            return {"mentions": []}
        mentions = []
        for aid in slice_ids:
            spec = self.behavior.get(aid)
            if spec is None or spec == "empty":
                continue
            mentions.append(spec)
        return {"mentions": mentions}


def _run(ws: WorkingState, llm: LLMPhaseClient) -> tuple[list, Candidates]:
    candidates = Candidates()

    async def _go():
        return await p1.run_phase1(ws, llm, "mock", candidates)

    mentions = asyncio.run(_go())
    return mentions, candidates


def test_registry_is_split_into_chunks(monkeypatch) -> None:
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_CHUNK_MAX", 2, raising=True)
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_MAX_DEPTH", 3, raising=True)
    rows = [(f"analyzer:e{i}", f"Label {i}") for i in range(1, 6)]
    ws = _ws_with_registry(rows)
    llm = _ScriptedLLM(
        behavior={
            f"analyzer:e{i}": {
                "surface": f"Label {i}",
                "type": "org",
                "agentId": f"analyzer:e{i}",
                "claim_refs": [],
                "per_claim_confidence": 0.8,
            }
            for i in range(1, 6)
        }
    )
    mentions, candidates = _run(ws, llm)

    bound_slices = [c["slice_ids"] for c in llm.calls if c["slice_ids"]]
    assert sorted([tuple(sorted(s)) for s in bound_slices]) == [
        ("analyzer:e1", "analyzer:e2"),
        ("analyzer:e3", "analyzer:e4"),
        ("analyzer:e5",),
    ]
    assert any(c["slice_ids"] == [] for c in llm.calls), "prose pass must run"
    assert {m.agent_registry_id for m in mentions} == {f"analyzer:e{i}" for i in range(1, 6)}
    assert not candidates.registry_coverage_issues


def test_empty_chunk_is_halved_recursively(monkeypatch) -> None:
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_CHUNK_MAX", 4, raising=True)
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_MAX_DEPTH", 3, raising=True)
    rows = [(f"analyzer:e{i}", f"Label {i}") for i in range(1, 5)]
    ws = _ws_with_registry(rows)

    def empty_on_all_four(slice_ids: list[str]) -> bool:
        return set(slice_ids) == {f"analyzer:e{i}" for i in range(1, 5)}

    llm = _ScriptedLLM(
        behavior={
            "__empty_on_slice__": empty_on_all_four,
            **{
                f"analyzer:e{i}": {
                    "surface": f"Label {i}",
                    "type": "org",
                    "agentId": f"analyzer:e{i}",
                    "claim_refs": [],
                    "per_claim_confidence": 0.8,
                }
                for i in range(1, 5)
            },
        }
    )
    mentions, candidates = _run(ws, llm)

    bound_slices = [tuple(sorted(c["slice_ids"])) for c in llm.calls if c["slice_ids"]]
    assert ("analyzer:e1", "analyzer:e2", "analyzer:e3", "analyzer:e4") in bound_slices
    assert ("analyzer:e1", "analyzer:e2") in bound_slices
    assert ("analyzer:e3", "analyzer:e4") in bound_slices
    assert {m.agent_registry_id for m in mentions} == {f"analyzer:e{i}" for i in range(1, 5)}
    assert not candidates.registry_coverage_issues


def test_prose_mentions_are_deduped_against_registry_labels() -> None:
    rows = [("analyzer:e1", "Acme Corp")]
    ws = _ws_with_registry(rows)
    llm = _ScriptedLLM(
        behavior={
            "analyzer:e1": {
                "surface": "Acme Corp",
                "type": "org",
                "agentId": "analyzer:e1",
                "claim_refs": [],
                "per_claim_confidence": 0.9,
            },
            "prose": {
                "mentions": [
                    {
                        "surface": "Acme Corp",
                        "type": "org",
                        "agentId": None,
                        "claim_refs": [],
                        "per_claim_confidence": 0.7,
                    },
                    {
                        "surface": "Shipping Strait",
                        "type": "route",
                        "agentId": None,
                        "claim_refs": [],
                        "per_claim_confidence": 0.7,
                    },
                ]
            },
        }
    )
    mentions, _ = _run(ws, llm)
    surfaces = sorted((m.surface, m.agent_registry_id) for m in mentions)
    assert surfaces == [("Acme Corp", "analyzer:e1"), ("Shipping Strait", None)]


def test_depth_exhausted_records_coverage_gap(monkeypatch) -> None:
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_CHUNK_MAX", 2, raising=True)
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_MAX_DEPTH", 0, raising=True)
    rows = [(f"analyzer:e{i}", f"Label {i}") for i in range(1, 3)]
    ws = _ws_with_registry(rows)
    llm = _ScriptedLLM(behavior={"__empty_on_slice__": lambda s: True})
    mentions, candidates = _run(ws, llm)
    assert mentions == []
    assert {i.agent_registry_id for i in candidates.registry_coverage_issues} == {
        "analyzer:e1",
        "analyzer:e2",
    }
    assert all(i.reason == "no_mention_from_phase1" for i in candidates.registry_coverage_issues)


def test_chunk_max_zero_single_bound_call_no_halving(monkeypatch) -> None:
    """CHUNK_MAX <= 0 → one bound LLM payload with the full registry (no recursive halving)."""
    monkeypatch.setattr(p1, "CANONICALIZER_PHASE1_CHUNK_MAX", 0, raising=True)
    rows = [(f"analyzer:e{i}", f"Label {i}") for i in range(1, 8)]
    ws = _ws_with_registry(rows)
    llm = _ScriptedLLM(
        behavior={
            f"analyzer:e{i}": {
                "surface": f"Label {i}",
                "type": "org",
                "agentId": f"analyzer:e{i}",
                "claim_refs": [],
                "per_claim_confidence": 0.8,
            }
            for i in range(1, 8)
        }
    )
    mentions, candidates = _run(ws, llm)
    bound_calls = [c for c in llm.calls if c["slice_ids"]]
    assert len(bound_calls) == 1
    assert len(bound_calls[0]["slice_ids"]) == 7
    assert "SUBSET" not in bound_calls[0]["extra"] and "entire" in bound_calls[0]["extra"].lower()
    assert {m.agent_registry_id for m in mentions} == {f"analyzer:e{i}" for i in range(1, 8)}
    assert not candidates.registry_coverage_issues
