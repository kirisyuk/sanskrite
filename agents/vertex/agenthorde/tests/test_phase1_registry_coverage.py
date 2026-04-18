"""Phase 1: skippedRegistryIds, registry coverage, and WorkingState side effects."""

from __future__ import annotations

import asyncio

from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.phases.phase1_entities import run_phase1
from agenthorde_common.canonicalizer.schemas import Candidates


def _ws_minimal() -> WorkingState:
    return WorkingState(
        analyzer_entities=[
            {"namespacedId": "analyzer:e1", "label": "Acme Corp", "origin": "analyzer"},
            {"namespacedId": "analyzer:e2", "label": "Plain CIF", "origin": "analyzer"},
            {"namespacedId": "analyzer:e3", "label": "Beta Ltd", "origin": "analyzer"},
        ],
        final_answer="x",
        working_graph_text="",
    )


def test_phase1_parses_skipped_registry_ids_and_sets_ws() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "skippedRegistryIds": ["analyzer:e2", "analyzer:ghost"],
                "mentions": [
                    {
                        "surface": "Acme Corp",
                        "type": "org",
                        "agentId": "analyzer:e1",
                        "claim_refs": [],
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Beta Ltd",
                        "type": "org",
                        "agentId": "analyzer:e3",
                        "claim_refs": [],
                        "per_claim_confidence": 0.9,
                    },
                ],
            }
        }
    )
    ws = _ws_minimal()
    candidates = Candidates()

    async def _go() -> None:
        await run_phase1(ws, mock, "mock", candidates)

    asyncio.run(_go())
    assert ws.skipped_registry_ids == frozenset({"analyzer:e2"})
    assert "analyzer:e3" not in ws.registry_ids_phase1_coverage_gap
    assert not candidates.registry_coverage_issues


def test_phase1_registry_coverage_gap_when_mention_missing() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme Corp",
                        "type": "org",
                        "agentId": "analyzer:e1",
                        "claim_refs": [],
                        "per_claim_confidence": 0.9,
                    },
                ],
            }
        }
    )
    ws = _ws_minimal()
    candidates = Candidates()

    async def _go() -> None:
        await run_phase1(ws, mock, "mock", candidates)

    asyncio.run(_go())
    gaps = {i.agent_registry_id for i in candidates.registry_coverage_issues}
    assert gaps == {"analyzer:e2", "analyzer:e3"}
    assert ws.registry_ids_phase1_coverage_gap == frozenset({"analyzer:e2", "analyzer:e3"})
    reasons = {i.agent_registry_id: i.reason for i in candidates.registry_coverage_issues}
    assert reasons["analyzer:e2"] == "no_mention_from_phase1"
    assert reasons["analyzer:e3"] == "no_mention_from_phase1"


def test_phase1_unknown_type_for_registry_row_is_coverage_gap() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme Corp",
                        "type": "org",
                        "agentId": "analyzer:e1",
                        "claim_refs": [],
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "weird",
                        "type": "not_a_real_fragment_class",
                        "agentId": "analyzer:e2",
                        "claim_refs": [],
                        "per_claim_confidence": 0.5,
                    },
                ],
            }
        }
    )
    ws = _ws_minimal()
    candidates = Candidates()

    async def _go() -> None:
        await run_phase1(ws, mock, "mock", candidates)

    asyncio.run(_go())
    e2 = next(i for i in candidates.registry_coverage_issues if i.agent_registry_id == "analyzer:e2")
    assert e2.reason == "mention_unknown_entity_type"
    assert "analyzer:e3" in ws.registry_ids_phase1_coverage_gap
