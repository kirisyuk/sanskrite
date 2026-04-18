"""AnalyzerOutput registry contract validation."""

from __future__ import annotations

import pytest

from agenthorde_common.schemas.agents_io import AnalyzerOutput


def test_analyzer_output_rejects_relation_with_unknown_src_id() -> None:
    with pytest.raises(ValueError, match="not in proposedEntities"):
        AnalyzerOutput.model_validate(
            {
                "chainSummary": "x",
                "proposedEntities": [{"id": "e1", "label": "A"}],
                "proposedNodes": [],
                "proposedRelations": [
                    {"srcId": "e1", "dstId": "e9", "relation": "supplies"},
                ],
                "openQuestions": [],
            }
        )


def test_analyzer_output_accepts_valid_registry_and_relations() -> None:
    ao = AnalyzerOutput.model_validate(
        {
            "chainSummary": "A to B",
            "proposedEntities": [
                {"id": "e1", "label": "A"},
                {"id": "e2", "label": "B"},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {"srcId": "e1", "dstId": "e2", "relation": "supplies", "attributes": {"product": "LNG"}},
            ],
            "openQuestions": [],
        }
    )
    assert len(ao.proposed_entities) == 2
    assert ao.proposed_relations[0].src_id == "e1"
