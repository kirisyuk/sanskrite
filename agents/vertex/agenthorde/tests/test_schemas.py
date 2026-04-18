from __future__ import annotations

import json
import unittest

from agenthorde_common.schemas.agents_io import AnalyzerOutput, CriticVerdict, SupervisorDecision
from agenthorde_common.schemas.graph import ReasoningGraph, ReasoningNode
from agenthorde_common.schemas.state import ApplyPatchRecord, HypothesisRecord, SessionPack
from agenthorde_common.schemas.tools_io import ApplyToolInput, ToolEnvelope


class SchemaRoundtripTests(unittest.TestCase):
    def test_session_pack_roundtrip(self) -> None:
        p = SessionPack(
            user_question="Q",
            hypotheses={"H0": HypothesisRecord(hypothesis_id="H0", title="t")},
            apply_log=[ApplyPatchRecord(patch={"x": 1})],
        )
        d = p.model_dump(mode="json", by_alias=True)
        p2 = SessionPack.model_validate(d)
        self.assertEqual(p2.user_question, "Q")
        self.assertEqual(len(p2.apply_log), 1)

    def test_reasoning_graph_nodes(self) -> None:
        g = ReasoningGraph(
            nodes=[
                ReasoningNode(id="n1", kind="claim", text="c"),
            ]
        )
        self.assertEqual(len(g.model_validate(json.loads(g.model_dump_json())).nodes), 1)

    def test_supervisor_decision(self) -> None:
        sd = SupervisorDecision(
            next_agent="supply_chain_analyzer",
            mission="m",
            research_temperature=0.5,
        )
        SupervisorDecision.model_validate(sd.model_dump(mode="json", by_alias=True))

    def test_supervisor_decision_governance_agent(self) -> None:
        sd = SupervisorDecision(
            next_agent="governance_control_analyzer",
            mission="Map UBO and subsidiaries.",
            research_temperature=0.4,
        )
        SupervisorDecision.model_validate(sd.model_dump(mode="json", by_alias=True))

    def test_tool_envelope(self) -> None:
        e = ToolEnvelope(ok=True, tool="t", payload={"a": 1})
        ToolEnvelope.model_validate(e.model_dump(mode="json", by_alias=True))

    def test_analyzer_output(self) -> None:
        ao = AnalyzerOutput(
            chain_summary="s",
            proposed_nodes=[ReasoningNode(id="x", kind="question", text="?")],
        )
        AnalyzerOutput.model_validate(ao.model_dump(mode="json", by_alias=True))

    def test_critic_verdict(self) -> None:
        cv = CriticVerdict(
            sufficient=False,
            blockers=[{"summary": "missing tier-2 supplier", "refs": ["sc_e1"]}],
            composition_recommendation="conditional",
        )
        CriticVerdict.model_validate(cv.model_dump(mode="json", by_alias=True))

    def test_apply_tool_input(self) -> None:
        ApplyToolInput(patch={"nodes": [{"id": "a", "label": "L"}]}, hypothesis_id="H0")


if __name__ == "__main__":
    unittest.main()
