"""Golden-style tests for corridor normalization and explicit request overrides (plan phases 1A–1B)."""

from __future__ import annotations

import unittest

from agentloom.contracts import AgentLoomRequest, OperationalCorridorOverride
from agentloom.question_intent import explicit_corridor_dict_from_request, extract_question_intent_bundle, regex_operational_corridor_dict


class TestQuestionIntentCorridor(unittest.TestCase):
    def test_regex_strips_likely_structured_tail_from_sink(self) -> None:
        q = "Map the strongest pressure points from Chile to Germany likely structured, in practice."
        d = regex_operational_corridor_dict(q)
        self.assertIn("germany", (d.get("sinkScope") or "").lower())
        self.assertNotIn("likely structured", (d.get("sinkScope") or "").lower())
        self.assertNotIn("structur", " ".join(d.get("sinkTokens") or []))

    def test_explicit_flat_corridor_overrides_heuristic(self) -> None:
        req = AgentLoomRequest(
            question="Ignore regex corridor; use explicit.",
            corridorSourceScope="Chile",
            corridorSinkScope="Germany",
            corridorActive=True,
        )
        d = explicit_corridor_dict_from_request(req)
        self.assertIsNotNone(d)
        assert d is not None
        self.assertEqual(d.get("sourceScope"), "Chile")
        self.assertEqual(d.get("sinkScope"), "Germany")

    def test_extract_bundle_explicit_corridor_meta(self) -> None:
        req = AgentLoomRequest(
            question="from X to Y messy tail, likely structured",
            operational_corridor=OperationalCorridorOverride(
                source_scope="Portugal",
                sink_scope="Poland",
                corridor_active=True,
            ),
        )
        intent, meta = extract_question_intent_bundle(req, ["supply_chain"])
        self.assertEqual(meta.intent_source, "explicit_corridor")
        self.assertEqual(intent.source_scope, "Portugal")
        self.assertEqual(intent.sink_scope, "Poland")
        self.assertTrue(intent.corridor_active)


if __name__ == "__main__":
    unittest.main()
