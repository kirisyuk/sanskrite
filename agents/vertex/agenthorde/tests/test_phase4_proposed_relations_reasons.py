"""Proposed-relation pre-pass: reasons for unresolved endpoints (skipped vs gap vs fuzzy miss)."""

from __future__ import annotations

from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.phases.phase4_relations import (
    _edges_from_proposed_relations,
    _proposed_relation_unresolved_reason,
)
from agenthorde_common.canonicalizer.schemas import Candidates, CanonicalEntity


def _ent(eid: str, surface: str) -> CanonicalEntity:
    return CanonicalEntity(
        entity_id=eid,
        type="org",
        surface_forms=[surface],
        canonical_path=f"/org/na/{eid}",
    )


def test_proposed_relation_reason_skipped_endpoint() -> None:
    ws = WorkingState(
        skipped_registry_ids=frozenset({"analyzer:e2"}),
        registry_ids_phase1_coverage_gap=frozenset(),
        analyzer_entity_label_by_ns={"analyzer:e1": "Alpha", "analyzer:e2": "SkippedCo"},
        agent_registry_id_to_entity_id={"analyzer:e1": "e0"},
    )
    entities = [_ent("e0", "Alpha")]
    cand = Candidates()
    edges = _edges_from_proposed_relations(
        [{"srcId": "analyzer:e1", "dstId": "analyzer:e2", "relation": "supplies"}],
        entities,
        ws,
        cand,
        "r1",
        "v-test",
        dict_runtime=None,
    )
    assert edges == []
    assert len(cand.unknown_relations) == 1
    assert cand.unknown_relations[0].reason == "entity_skipped_by_phase1"


def test_proposed_relation_reason_coverage_gap() -> None:
    ws = WorkingState(
        skipped_registry_ids=frozenset(),
        registry_ids_phase1_coverage_gap=frozenset({"analyzer:e2"}),
        analyzer_entity_label_by_ns={"analyzer:e1": "Alpha", "analyzer:e2": "MissingMention"},
        agent_registry_id_to_entity_id={"analyzer:e1": "e0"},
    )
    entities = [_ent("e0", "Alpha")]
    cand = Candidates()
    edges = _edges_from_proposed_relations(
        [{"srcId": "analyzer:e1", "dstId": "analyzer:e2", "relation": "supplies"}],
        entities,
        ws,
        cand,
        "r1",
        "v-test",
        dict_runtime=None,
    )
    assert edges == []
    assert cand.unknown_relations[0].reason == "registry_mention_not_extracted"


def test_skipped_endpoint_does_not_use_label_fallback() -> None:
    """LLM skipped e2: do not silently attach the registry label to another entity."""
    ws = WorkingState(
        skipped_registry_ids=frozenset({"analyzer:e2"}),
        registry_ids_phase1_coverage_gap=frozenset(),
        analyzer_entity_label_by_ns={"analyzer:e1": "Alpha", "analyzer:e2": "Alpha"},
        agent_registry_id_to_entity_id={"analyzer:e1": "e0"},
    )
    entities = [_ent("e0", "Alpha")]
    cand = Candidates()
    edges = _edges_from_proposed_relations(
        [{"srcId": "analyzer:e1", "dstId": "analyzer:e2", "relation": "supplies"}],
        entities,
        ws,
        cand,
        "r1",
        "v-test",
        dict_runtime=None,
    )
    assert edges == []
    assert cand.unknown_relations[0].reason == "entity_skipped_by_phase1"


def test_proposed_relation_unresolved_reason_priority() -> None:
    ws = WorkingState(
        skipped_registry_ids=frozenset({"analyzer:a"}),
        registry_ids_phase1_coverage_gap=frozenset({"analyzer:b"}),
    )
    assert (
        _proposed_relation_unresolved_reason("analyzer:a", "analyzer:b", None, None, ws)
        == "entity_skipped_by_phase1"
    )
