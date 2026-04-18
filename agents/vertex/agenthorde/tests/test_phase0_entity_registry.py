"""Phase 0: entity registry + namespaced relations."""

from __future__ import annotations

from agenthorde_common.canonicalizer.phases.phase0_input import run_phase0


def test_phase0_collects_entities_and_relations_from_analyzer() -> None:
    ws = run_phase0(
        pack={"reasoningGraph": {"nodes": [], "edges": []}},
        analyzer_output={
            "chainSummary": "TSMC and ASML.",
            "proposedEntities": [
                {"id": "e1", "label": "TSMC"},
                {"id": "e2", "label": "ASML Holding N.V."},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {"srcId": "e2", "dstId": "e1", "relation": "supplies", "sourceClaimIds": ["c1"]},
            ],
            "openQuestions": [],
        },
        governance_output={},
        final_answer="",
    )
    ids = {e["namespacedId"] for e in ws.analyzer_entities}
    assert ids == {"analyzer:e1", "analyzer:e2"}
    assert len(ws.analyzer_relations) == 1
    assert ws.analyzer_relations[0]["srcId"] == "analyzer:e2"
    assert ws.analyzer_relations[0]["dstId"] == "analyzer:e1"
    assert ws.analyzer_entity_label_by_ns["analyzer:e1"] == "TSMC"


def test_phase0_drops_relation_when_endpoint_missing_from_registry() -> None:
    ws = run_phase0(
        pack={"reasoningGraph": {"nodes": [], "edges": []}},
        analyzer_output={
            "chainSummary": "x",
            "proposedEntities": [{"id": "e1", "label": "Only"}],
            "proposedNodes": [],
            "proposedRelations": [
                {"srcId": "e1", "dstId": "e2", "relation": "supplies"},
            ],
            "openQuestions": [],
        },
        governance_output={},
        final_answer="",
    )
    assert ws.analyzer_relations == []
    codes = [i.code for i in ws.input_issues]
    assert "entity_id_not_in_registry" in codes


def test_phase0_pack_entities_merge_without_duplicating_namespaced_id() -> None:
    ws = run_phase0(
        pack={
            "reasoningGraph": {"nodes": [], "edges": []},
            "proposedEntities": [
                {"namespacedId": "analyzer:e1", "label": "TSMC", "origin": "analyzer"},
            ],
            "proposedRelations": [],
        },
        analyzer_output={
            "chainSummary": "y",
            "proposedEntities": [{"id": "e1", "label": "TSMC longer label wins"}],
            "proposedNodes": [],
            "proposedRelations": [],
            "openQuestions": [],
        },
        governance_output={},
        final_answer="",
    )
    tsmc = next(e for e in ws.analyzer_entities if e["namespacedId"] == "analyzer:e1")
    assert "longer" in tsmc["label"]
