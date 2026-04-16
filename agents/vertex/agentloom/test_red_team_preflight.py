"""Unit tests for investigation preflight, red-team compose gate, and todo merge."""

from __future__ import annotations

import os
import unittest
from unittest import mock

from agentloom.contracts import (
    AgentLoomRequest,
    InvestigationPreflight,
    QuestionIntent,
    RedTeamAgentOutput,
    red_team_agent_output_to_agent_result,
)
from agentloom.investigation_todos import merge_todos_after_graph_progress, seed_investigation_todos
from agentloom.preflight import normalize_investigation_preflight, preflight_requires_red_team_pass, red_team_frame_eligible


class TestPreflightRedTeam(unittest.TestCase):
    def test_preflight_requires_red_team_deep_breadth(self) -> None:
        req = AgentLoomRequest(question="q", investigationPreflight=InvestigationPreflight(breadthProfile="deep_breadth"))
        intent = QuestionIntent()
        pf = normalize_investigation_preflight(req, intent)
        self.assertTrue(preflight_requires_red_team_pass(pf))

    def test_seed_todos_from_preflight_alternatives(self) -> None:
        intent = QuestionIntent(wantsAlternatives=False)
        pf = InvestigationPreflight(breadthProfile="deep_breadth")
        todos = seed_investigation_todos(intent, ["supply_chain"], pf)
        phases = [t.get("phase") for t in todos]
        self.assertIn("alternatives", phases)

    def test_merge_alternatives_in_progress_after_one_chain_when_preflight_wide(self) -> None:
        intent = QuestionIntent()
        req = AgentLoomRequest(question="x", investigationPreflight=InvestigationPreflight(breadthProfile="deep_breadth"))
        pf = normalize_investigation_preflight(req, intent)
        todos = seed_investigation_todos(intent, ["supply_chain"], pf)
        st = {"investigation_todos": todos, "investigation_preflight": pf.model_dump(mode="json", by_alias=True)}
        merged = merge_todos_after_graph_progress(
            st,
            grounded_complete_chains=1,
            structural_complete_chains=0,
            partial_chains=0,
            typed_backbone_chains=0,
            requested_corridor_satisfied=True,
        )
        alt = next(x for x in merged if x.get("phase") == "alternatives")
        self.assertEqual(alt.get("status"), "in_progress")

    def test_red_team_frame_eligible(self) -> None:
        self.assertTrue(red_team_frame_eligible({"groundedCompleteChainCount": 1}))
        self.assertFalse(red_team_frame_eligible({"groundedCompleteChainCount": 0, "structuralCompleteChainCount": 0, "typedBackboneChainCount": 0}))

    def test_orchestration_compose_blocked_without_red_team(self) -> None:
        from agentloom.agent import (
            STATE_INVESTIGATION_PREFLIGHT,
            STATE_LAST_CRITIC_VERDICT,
            STATE_RED_TEAM_SATISFIED,
            _orchestration_policy_allow_compose,
        )

        prev = os.environ.get("AGENTLOOM_TESTING")
        os.environ["AGENTLOOM_TESTING"] = ""
        try:
            st = {
                STATE_INVESTIGATION_PREFLIGHT: InvestigationPreflight(breadthProfile="deep_breadth").model_dump(
                    mode="json", by_alias=True
                ),
                STATE_RED_TEAM_SATISFIED: False,
                STATE_LAST_CRITIC_VERDICT: {
                    "compositionRecommendation": "recommend",
                    "compositionConditions": [],
                    "investigationEpoch": 0,
                },
            }
            with mock.patch("agentloom.agent._investigation_reachability_counts_bundle") as m:
                m.return_value = {
                    "reachability": {"groundedCompleteChainCount": 1, "structuralCompleteChainCount": 0, "typedBackboneChainCount": 0},
                    "groundedCompleteChainCount": 1,
                    "structuralCompleteChainCount": 0,
                    "partialChainCount": 0,
                    "typedBackboneChainCount": 0,
                    "requestedCorridorSatisfied": True,
                }
                ok, why = _orchestration_policy_allow_compose(st)
            self.assertFalse(ok)
            self.assertEqual(why, "red_team_required_not_satisfied")
        finally:
            if prev is None:
                os.environ.pop("AGENTLOOM_TESTING", None)
            else:
                os.environ["AGENTLOOM_TESTING"] = prev

    def test_normalize_explicit_preflight_beats_intent(self) -> None:
        intent = QuestionIntent(wantsAlternatives=True, wantsCounterNarrative=True)
        req = AgentLoomRequest(
            question="q",
            investigationPreflight=InvestigationPreflight(
                requireAlternativeRouteFamilies=False,
                requireEpistemicCounterview=False,
            ),
        )
        pf = normalize_investigation_preflight(req, intent)
        self.assertFalse(pf.require_alternative_route_families)
        self.assertFalse(pf.require_epistemic_counterview)

    def test_red_team_suggested_next_questions_merge_into_agent_result(self) -> None:
        rt = RedTeamAgentOutput(
            agentName="red_team_agent",
            missionId="m1",
            status="success",
            summary="s",
            nextQuestions=["from base"],
            suggestedNextQuestions=["extra a", "extra a", "extra b"],
        )
        ar = red_team_agent_output_to_agent_result(rt)
        self.assertIn("from base", ar.next_questions)
        self.assertIn("extra a", ar.next_questions)
        self.assertIn("extra b", ar.next_questions)
        self.assertEqual(ar.next_questions.index("extra a"), ar.next_questions.index("from base") + 1)

    def test_logic_trace_red_team_compose_waiver_skips_gate(self) -> None:
        from agentloom.agent import (
            STATE_INVESTIGATION_PREFLIGHT,
            STATE_LAST_CRITIC_VERDICT,
            STATE_LOGIC_TRACE,
            STATE_RED_TEAM_SATISFIED,
            _orchestration_policy_allow_compose,
        )

        prev = os.environ.get("AGENTLOOM_TESTING")
        os.environ["AGENTLOOM_TESTING"] = ""
        try:
            st = {
                STATE_INVESTIGATION_PREFLIGHT: InvestigationPreflight(breadthProfile="deep_breadth").model_dump(
                    mode="json", by_alias=True
                ),
                STATE_RED_TEAM_SATISFIED: False,
                STATE_LOGIC_TRACE: [{"composePolicyWaivers": ["red_team_compose"], "cycle": 1}],
                STATE_LAST_CRITIC_VERDICT: {
                    "compositionRecommendation": "recommend",
                    "compositionConditions": [],
                    "investigationEpoch": 0,
                },
            }
            with mock.patch("agentloom.agent._investigation_reachability_counts_bundle") as m:
                m.return_value = {
                    "reachability": {"groundedCompleteChainCount": 1, "structuralCompleteChainCount": 0, "typedBackboneChainCount": 0},
                    "groundedCompleteChainCount": 1,
                    "structuralCompleteChainCount": 0,
                    "partialChainCount": 0,
                    "typedBackboneChainCount": 0,
                    "requestedCorridorSatisfied": True,
                }
                ok, why = _orchestration_policy_allow_compose(st)
            self.assertNotEqual(why, "red_team_required_not_satisfied")
            self.assertEqual(why, "ok")
            self.assertTrue(ok)
        finally:
            if prev is None:
                os.environ.pop("AGENTLOOM_TESTING", None)
            else:
                os.environ["AGENTLOOM_TESTING"] = prev

    def test_preflight_requires_respects_kill_switch(self) -> None:
        with mock.patch("agentloom.preflight.RED_TEAM_REQUIRED_FOR_BREADTH", False):
            self.assertFalse(preflight_requires_red_team_pass(InvestigationPreflight(breadthProfile="deep_breadth")))

    def test_supervisor_coercion_fills_red_team_mission(self) -> None:
        from agentloom.agent import _coerce_supervisor_decision_payload

        raw = {
            "cycle": 3,
            "assessment": "Broaden frame.",
            "rationale": ["Preflight breadth."],
            "nextAgent": "red_team_agent",
            "mission": {"assignedRoles": ["supply_chain"], "groundingMode": "mixed"},
        }
        fixed = _coerce_supervisor_decision_payload(
            raw,
            selected_role_literals=["supply_chain"],
            cycle=3,
            user_question="Map routes?",
        )
        obj = (fixed.get("mission") or {}).get("objective", "").lower()
        self.assertTrue("frame-broadening" in obj or "alternative route" in obj, obj)


if __name__ == "__main__":
    unittest.main()
