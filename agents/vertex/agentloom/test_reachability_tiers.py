"""Tests for typed operational backbone reachability and delegation escape hatch."""

from __future__ import annotations

import unittest
from unittest import mock

from agentloom import agent as agentloom_agent
from agentloom.agent import (
    STATE_EMPTY_TYPED_PAYLOAD_STREAK,
    STATE_GLOBAL_STAGNATION,
    STATE_INVESTIGATION_EPOCH,
    STATE_INVESTIGATION_TODOS,
    STATE_LAST_CRITIC_VERDICT,
    STATE_LOGIC_TRACE,
    STATE_SELECTED_ROLES,
    STATE_WORKING_GRAPH,
    _apply_graph_fragment_mutations,
    _orchestration_policy_allow_compose,
)
from agentloom.contracts import EntityNode, GraphFragment, InvestigationMission, SupervisorDecision
from agentloom.delegation_policy import apply_delegate_constraints
from agentloom.investigation_todos import merge_todos_after_graph_progress, seed_investigation_todos
import agentloom.limits as agentloom_limits
from agentloom.question_intent import QuestionIntent
from agentloom.taxonomy import ROLE_SUPPLY_CHAIN
from agentloom.reachability_tiers import compute_typed_operational_backbone_entries
from agentloom.operational_graph import graph_state_from_raw, node_map


def _sample_typed_backbone_graph() -> dict:
    """Minimal graph: 2 hosts, product-carrying flow, transformation step (not business-complete)."""
    return {
        "entityNodes": [
            {"nodeId": "facility:alpha", "label": "Alpha Plant", "entityType": "FACILITY", "confidence": 0.5, "supportType": "hypothesis"},
            {"nodeId": "facility:beta", "label": "Beta Plant", "entityType": "FACILITY", "confidence": 0.5, "supportType": "hypothesis"},
            {"nodeId": "product:stage1", "label": "Stage1", "entityType": "PRODUCT", "confidence": 0.5, "supportType": "hypothesis"},
            {"nodeId": "product:stage2", "label": "Stage2", "entityType": "PRODUCT", "confidence": 0.5, "supportType": "hypothesis"},
        ],
        "flowLegs": [
            {
                "legId": "leg-ab",
                "supplierNodeId": "facility:alpha",
                "customerNodeId": "facility:beta",
                "productNodeId": "product:stage1",
                "confidence": 0.4,
                "supportType": "hypothesis",
            }
        ],
        "processSteps": [
            {
                "stepId": "step-t1",
                "hostNodeId": "facility:beta",
                "processKind": "transformation",
                "inputNodeIds": ["product:stage1"],
                "outputNodeIds": ["product:stage2"],
                "confidence": 0.4,
                "supportType": "hypothesis",
            }
        ],
        "overlayEdges": [],
    }


class ReachabilityTiersTests(unittest.TestCase):
    def test_typed_backbone_detected(self) -> None:
        raw = _sample_typed_backbone_graph()
        gs = graph_state_from_raw(raw)
        nodes = node_map(gs)
        entries = compute_typed_operational_backbone_entries(gs, nodes)
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0].get("tier"), "typed_backbone")

    def test_reachability_report_includes_typed_backbone(self) -> None:
        st = {STATE_WORKING_GRAPH: _sample_typed_backbone_graph()}
        rep = agentloom_agent._graph_reachability_report(st)
        self.assertGreaterEqual(int(rep.get("typedBackboneChainCount") or 0), 1)
        self.assertIn("typedBackboneChains", rep)

    def test_stagnation_recycle_off_escalates_to_critic_instead_of_supply(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="continue supply work",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=5,
            assessment="more supply",
            rationale=["retry"],
            nextAgent="supply_chain_investigator_agent",
            mission=mission,
        )
        st = {
            STATE_GLOBAL_STAGNATION: agentloom_limits.STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES,
            STATE_LOGIC_TRACE: [],
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN],
            STATE_WORKING_GRAPH: {},
        }
        with mock.patch("agentloom.delegation_policy.DELEGATE_STAGNATION_RECYCLE_SUPPLY", False):
            out, pcs = apply_delegate_constraints(st, dec, cycle=5, user_question="trace corridor")
        self.assertEqual(out.next_agent, "critic_agent")
        self.assertTrue(any("stagnation_idle_escalate_critic" in (p.detail or "") for p in pcs))

    def test_delegate_allows_critic_when_typed_backbone_flag_on(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="decide",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=4,
            assessment="exit",
            rationale=["stalled"],
            nextAgent="critic_agent",
            mission=mission,
        )
        st = {
            STATE_GLOBAL_STAGNATION: agentloom_limits.STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES,
            STATE_LOGIC_TRACE: [],
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN],
            STATE_WORKING_GRAPH: _sample_typed_backbone_graph(),
        }
        with mock.patch("agentloom.delegation_policy.CRITIC_ALLOW_TYPED_BACKBONE", True):
            out, pcs = apply_delegate_constraints(st, dec, cycle=4, user_question="q?")
        self.assertEqual(out.next_agent, "critic_agent")
        self.assertFalse(any("critic_deferred_until_business_complete_chain" in (p.detail or "") for p in pcs))

    def test_merge_todos_marks_flow_backbone_for_typed_only(self) -> None:
        intent = QuestionIntent(
            sourceScope="A",
            sinkScope="B",
            corridorActive=True,
            wantsAlternatives=False,
            wantsOwnership=False,
            wantsPolicy=False,
        )
        todos = seed_investigation_todos(intent, [ROLE_SUPPLY_CHAIN])
        st = {STATE_INVESTIGATION_TODOS: todos}
        merged = merge_todos_after_graph_progress(
            st,
            todos_key=STATE_INVESTIGATION_TODOS,
            grounded_complete_chains=0,
            structural_complete_chains=0,
            partial_chains=1,
            typed_backbone_chains=1,
            requested_corridor_satisfied=True,
        )
        phases = {str(x.get("phase")): str(x.get("status")) for x in merged if isinstance(x, dict)}
        self.assertEqual(phases.get("flow_backbone"), "done")

    def test_empty_typed_payload_streak_increments_then_resets(self) -> None:
        frag_nodes_only = GraphFragment(
            fragmentId="frag-nodes",
            fragmentType="operational_flow",
            title="nodes only",
            humanQuestion="",
            humanSummary="",
            branchId="b1",
            entityNodes=[
                EntityNode(
                    nodeId="facility:x",
                    label="X",
                    entityType="FACILITY",
                    confidence=0.4,
                    supportType="hypothesis",
                )
            ],
        )
        state: dict = {}
        _apply_graph_fragment_mutations(state, [frag_nodes_only])
        self.assertEqual(int(state.get(STATE_EMPTY_TYPED_PAYLOAD_STREAK) or 0), 1)
        frag_with_leg = GraphFragment(
            fragmentId="frag-leg",
            fragmentType="operational_flow",
            title="with leg",
            humanQuestion="",
            humanSummary="",
            branchId="b1",
            flowLegs=[
                {
                    "legId": "l1",
                    "supplierNodeId": "facility:a",
                    "customerNodeId": "facility:b",
                    "productNodeId": "product:p",
                    "confidence": 0.5,
                    "supportType": "hypothesis",
                }
            ],
        )
        _apply_graph_fragment_mutations(state, [frag_with_leg])
        self.assertEqual(int(state.get(STATE_EMPTY_TYPED_PAYLOAD_STREAK) or 0), 0)

    def test_compose_allowed_when_todos_pending_but_typed_backbone_escape_on(self) -> None:
        epoch = 3
        st = {
            STATE_INVESTIGATION_TODOS: [
                {"todoId": "c1", "phase": "corridor", "status": "pending", "title": "corridor"},
            ],
            STATE_LAST_CRITIC_VERDICT: {
                "compositionRecommendation": "recommend",
                "investigationEpoch": epoch,
            },
            STATE_INVESTIGATION_EPOCH: epoch,
        }
        with (
            mock.patch("agentloom.agent.COMPOSE_REQUIRES_TODOS_SATISFIED", True),
            mock.patch("agentloom.agent.COMPOSE_ALLOW_WITH_TYPED_BACKBONE", True),
            mock.patch(
                "agentloom.agent._graph_reachability_report",
                return_value={"typedBackboneChainCount": 1},
            ),
        ):
            ok, reason = _orchestration_policy_allow_compose(st)
        self.assertTrue(ok)
        self.assertEqual(reason, "ok")

    def test_compose_blocked_when_todos_pending_and_no_typed_backbone_escape(self) -> None:
        epoch = 2
        st = {
            STATE_INVESTIGATION_TODOS: [
                {"todoId": "c1", "phase": "corridor", "status": "pending", "title": "corridor"},
            ],
            STATE_LAST_CRITIC_VERDICT: {
                "compositionRecommendation": "recommend",
                "investigationEpoch": epoch,
            },
            STATE_INVESTIGATION_EPOCH: epoch,
        }
        with (
            mock.patch("agentloom.agent.COMPOSE_REQUIRES_TODOS_SATISFIED", True),
            mock.patch("agentloom.agent.COMPOSE_ALLOW_WITH_TYPED_BACKBONE", False),
        ):
            ok, reason = _orchestration_policy_allow_compose(st)
        self.assertFalse(ok)
        self.assertEqual(reason, "investigation_todos_not_satisfied")


if __name__ == "__main__":
    unittest.main()
