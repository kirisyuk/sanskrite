from __future__ import annotations

import unittest

from agentloom.agent import (
    CRITIC_CONFIG,
    GAP_BRIDGE_CONFIG,
    INVESTIGATOR_PLANNER_CONFIG,
    INVESTIGATOR_SYNTHESIZER_CONFIG,
    ROLE_CONTRACTS,
    ROLE_SELECTOR_CONFIG,
    SUPERVISOR_CONFIG,
    RETRIEVAL_MAX_OUTPUT_TOKENS,
    TOOL_MAX_OUTPUT_TOKENS,
    _branch_progress_delta,
    _normalize_agent_result,
    _parse_tool_result_payload,
    _update_branch_state,
)
from agentloom.composer import build_final_chain_summary
from agentloom.contracts import AgentResult, BranchPlan, BranchState, FailureRecord, GraphFragment, GraphObservation, InvestigationMission, ToolAction
from agentloom.taxonomy import ROLE_CONTRACTS_BY_ROLE, ROLE_GOVERNANCE, ROLE_POLICY, ROLE_SUPPLY_CHAIN


class AgentLoomRuntimeTest(unittest.TestCase):
    def test_role_contracts_are_loaded_from_taxonomy_module(self) -> None:
        self.assertEqual(ROLE_CONTRACTS, ROLE_CONTRACTS_BY_ROLE)
        self.assertEqual(
            ROLE_CONTRACTS[ROLE_GOVERNANCE].contract_types,
            ["ownership-structure", "control-map"],
        )
        self.assertIn("PRODUCES", ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].path_carried_edges)
        self.assertIn("CONTROLS", ROLE_CONTRACTS[ROLE_GOVERNANCE].allowed_edge_types)
        self.assertIn("REGULATES", ROLE_CONTRACTS[ROLE_POLICY].allowed_edge_types)

    def test_llm_configs_have_json_contract_and_output_budget(self) -> None:
        configs = [
            ROLE_SELECTOR_CONFIG,
            SUPERVISOR_CONFIG,
            INVESTIGATOR_PLANNER_CONFIG,
            INVESTIGATOR_SYNTHESIZER_CONFIG,
            GAP_BRIDGE_CONFIG,
            CRITIC_CONFIG,
        ]
        for config in configs:
            self.assertEqual(config.response_mime_type, "application/json")
            self.assertIsNotNone(config.max_output_tokens)
            self.assertGreaterEqual(int(config.max_output_tokens), 1536)
        self.assertGreaterEqual(TOOL_MAX_OUTPUT_TOKENS, 4096)
        self.assertGreaterEqual(RETRIEVAL_MAX_OUTPUT_TOKENS, 2048)

    def test_tool_payload_parser_normalizes_valid_graph_json(self) -> None:
        action = ToolAction(
            actionId="branch-1-a1",
            branchId="branch-1",
            toolName="model_question_to_graph",
            humanQuestion="What path connects ore to battery-grade nickel?",
            humanAction="Build a typed path fragment.",
            whyThisQuestion="The graph does not yet contain a typed ore-to-battery-grade path.",
            closureTarget="Recover one typed ore-to-battery-grade path.",
            knownContext=["The graph is currently empty."],
            frontier=["Identify the conversion from ore to a battery-grade product."],
            queryText="What path connects ore to battery-grade nickel?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["nickel"],
            expectedFragmentType="overview_chain",
        )
        raw = """
        {
          "actionId": "branch-1-a1",
          "branchId": "branch-1",
          "toolName": "model_question_to_graph",
          "status": "success",
          "humanSummary": "Built a typed path.",
          "closureStatus": "closed",
          "resolvedFrontier": ["Identified the conversion from ore to a battery-grade product."],
          "remainingFrontier": [],
          "discoveredFrontier": [],
          "graphFragments": [
            {
              "title": "Nickel chain",
              "humanQuestion": "What path connects ore to battery-grade nickel?",
              "humanSummary": "A typed chain from ore to nickel sulfate.",
              "branchId": "branch-1",
              "fragmentType": "overview_chain",
              "observations": [
                {
                  "observationId": "o1",
                  "subject": "Nickel ore",
                  "subjectType": "PRODUCT",
                  "relation": "TRANSFORMS_INTO",
                  "object": "Nickel sulfate",
                  "objectType": "PRODUCT",
                  "qualifiers": [],
                  "confidence": 0.7,
                  "supportType": "model_knowledge",
                  "provenance": [],
                  "supportNote": "Reasoned from model knowledge.",
                  "branchId": "branch-1",
                  "alternativeGroupId": ""
                }
              ],
              "completeness": "partial",
              "qualityScore": 0.6
            }
          ],
          "uncoveredQuestions": [],
          "sourceDocuments": [],
          "failure": null
        }
        """
        parsed = _parse_tool_result_payload(raw, action, {"TRANSFORMS_INTO"}, "model_knowledge")

        self.assertEqual(parsed.status, "success")
        self.assertEqual(parsed.graph_fragments[0].fragment_id, "branch-1-f1")
        self.assertEqual(parsed.graph_fragments[0].observations[0].relation, "TRANSFORMS_INTO")

    def test_branch_progress_depends_on_new_graph_content(self) -> None:
        previous_fragment = GraphFragment(
            fragmentId="branch-1-f1",
            fragmentType="overview_chain",
            title="Existing path",
            humanQuestion="What path connects ore to battery-grade nickel?",
            humanSummary="Existing fragment.",
            branchId="branch-1",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-ore",
                    subject="Nickel ore",
                    subjectType="PRODUCT",
                    relation="TRANSFORMS_INTO",
                    objectId="product:mhp",
                    object="Mixed hydroxide precipitate",
                    objectType="PRODUCT",
                    qualifiers=[],
                    confidence=0.6,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Existing evidence.",
                    branchId="branch-1",
                    alternativeGroupId="",
                )
            ],
            completeness="partial",
            qualityScore=0.5,
        )
        previous_state = BranchState(
            branchId="branch-1",
            hypothesis="Main chain exists",
            humanQuestion="What path connects ore to battery-grade nickel?",
            humanSummary="Existing branch.",
            closureTarget="Recover one typed ore-to-battery-grade path.",
            knownContext=["Nickel ore already transforms into MHP in the graph."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            status="active",
            qualityScore=0.5,
            stagnationCount=0,
            stopReason="",
            resolvedFrontier=[],
            remainingFrontier=["Who converts MHP into nickel sulfate?"],
            openQuestions=["Who converts MHP into nickel sulfate?"],
            toolActions=[],
            toolResults=[],
            graphFragments=[previous_fragment],
        )
        result_with_new_edge = _parse_tool_result_payload(
            """
            {
              "actionId": "branch-1-a2",
              "branchId": "branch-1",
              "toolName": "vertex_question_to_graph",
              "status": "success",
              "humanSummary": "Added the converter step.",
              "closureStatus": "closed",
              "resolvedFrontier": ["Identified the converter step from MHP to nickel sulfate."],
              "remainingFrontier": [],
              "discoveredFrontier": [],
              "graphFragments": [
                {
                  "fragmentId": "branch-1-f2",
                  "fragmentType": "chain_fragment",
                  "title": "Converter step",
                  "humanQuestion": "Who converts MHP into nickel sulfate?",
                  "humanSummary": "A converter produces nickel sulfate from MHP.",
                  "branchId": "branch-1",
                  "observations": [
                    {
                      "observationId": "o2",
                      "subject": "Mixed hydroxide precipitate",
                      "subjectType": "PRODUCT",
                      "relation": "TRANSFORMS_INTO",
                      "object": "Nickel sulfate",
                      "objectType": "PRODUCT",
                      "qualifiers": [],
                      "confidence": 0.7,
                      "supportType": "document",
                      "provenance": ["doc-2"],
                      "supportNote": "New converter evidence.",
                      "branchId": "branch-1",
                      "alternativeGroupId": ""
                    }
                  ],
                  "completeness": "partial",
                  "qualityScore": 0.6
                }
              ],
              "uncoveredQuestions": [],
              "sourceDocuments": ["doc-2"],
              "failure": null
            }
            """,
            ToolAction(
                actionId="branch-1-a2",
                branchId="branch-1",
                toolName="vertex_question_to_graph",
                humanQuestion="Who converts MHP into nickel sulfate?",
                humanAction="Ground the converter step.",
                whyThisQuestion="The graph already reaches MHP but not the next converter.",
                closureTarget="Close the downstream conversion from MHP to nickel sulfate.",
                knownContext=["Nickel ore --TRANSFORMS_INTO--> Mixed hydroxide precipitate"],
                frontier=["Identify the converter that turns MHP into nickel sulfate."],
                queryText="Who converts MHP into nickel sulfate?",
                assignedRoles=[ROLE_SUPPLY_CHAIN],
                targetEntities=["MHP", "nickel sulfate"],
                expectedFragmentType="chain_fragment",
            ),
            {"TRANSFORMS_INTO"},
            "document",
        )

        self.assertGreater(_branch_progress_delta(previous_state, result_with_new_edge), 0.05)

    def test_agent_result_is_normalized_against_active_role_contract(self) -> None:
        mission = InvestigationMission(
            missionId="m-1",
            objective="Find supply-chain structure.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
            successCriteria=["Return supply-chain path edges only."],
        )
        raw = AgentResult(
            agentName="investigator_agent",
            missionId="m-1",
            status="partial",
            summary="Mixed result.",
            activeRoles=[ROLE_SUPPLY_CHAIN],
            graphDelta=[
                GraphObservation(
                    observationId="obs-1",
                    subject="Mine",
                    relation="PRODUCES",
                    object="Nickel Ore",
                    confidence=0.7,
                    supportType="model_knowledge",
                ),
                GraphObservation(
                    observationId="obs-2",
                    subject="Regulator",
                    relation="REGULATES",
                    object="Mine",
                    confidence=0.7,
                    supportType="model_knowledge",
                ),
            ],
            gaps=[],
            answeredQuestions=["What is the main chain?"],
            nextQuestions=[],
            handoffRecommendations=["critic_agent"],
        )

        normalized = _normalize_agent_result(raw, mission, [ROLE_SUPPLY_CHAIN])

        self.assertEqual(len(normalized.graph_delta), 1)
        self.assertEqual(normalized.graph_delta[0].relation, "PRODUCES")
        self.assertIsNotNone(normalized.failure)
        self.assertEqual(normalized.failure.code, "agent_failed")
        self.assertTrue(any("outside the active role contracts" in gap for gap in normalized.gaps))

    def test_branch_does_not_converge_while_open_questions_remain(self) -> None:
        plan = BranchPlan(
            branchId="b-1",
            hypothesis="Need direct German downstream link.",
            humanQuestion="Which facility in Germany consumes battery-grade nickel sulfate?",
            humanAction="Trace the downstream German facility.",
            closureTarget="Close the downstream receiver link from battery-grade nickel sulfate to a German facility.",
            knownContext=["Vale Bécancour Nickel Sulfate Plant is already in the graph."],
            frontier=["Identify the specific German facility that consumes this nickel sulfate."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["vertex_question_to_graph"],
            targetEntities=["Germany", "nickel sulfate"],
            fragmentType="chain_fragment",
            parallelGroup="downstream",
            stopAfterNoImprovement=2,
        )
        action = ToolAction(
            actionId="b-1-a1",
            branchId="b-1",
            toolName="vertex_question_to_graph",
            humanQuestion=plan.human_question,
            humanAction=plan.human_action,
            whyThisQuestion="The graph already reaches nickel sulfate but does not yet reach a German downstream receiver.",
            closureTarget=plan.closure_target,
            knownContext=plan.known_context,
            frontier=plan.frontier,
            queryText=plan.human_question,
            assignedRoles=plan.assigned_roles,
            targetEntities=plan.target_entities,
            expectedFragmentType=plan.fragment_type,
        )
        result = _parse_tool_result_payload(
            """
            {
              "actionId": "b-1-a1",
              "branchId": "b-1",
              "toolName": "vertex_question_to_graph",
              "status": "partial",
              "humanSummary": "Found an upstream facility but not the German consumer.",
              "closureStatus": "advanced",
              "resolvedFrontier": ["Identified one upstream facility-level supply step."],
              "remainingFrontier": ["Which German facility consumes this nickel sulfate?"],
              "discoveredFrontier": ["Determine whether the path reaches Germany through an intermediate European processor."],
              "graphFragments": [
                {
                  "fragmentId": "b-1-f1",
                  "fragmentType": "chain_fragment",
                  "title": "Upstream fragment",
                  "humanQuestion": "Which facility in Germany consumes battery-grade nickel sulfate?",
                  "humanSummary": "One upstream plant was identified.",
                  "branchId": "b-1",
                  "observations": [
                    {
                      "observationId": "o1",
                      "subject": "Vale Becancour Nickel Sulfate Plant",
                      "subjectType": "FACILITY",
                      "relation": "SUPPLIES_TO",
                      "object": "Ultium CAM",
                      "objectType": "FACILITY",
                      "qualifiers": [],
                      "confidence": 0.7,
                      "supportType": "document",
                      "provenance": ["doc-1"],
                      "supportNote": "Grounded upstream step.",
                      "branchId": "b-1",
                      "alternativeGroupId": ""
                    }
                  ],
                  "completeness": "partial",
                  "qualityScore": 0.6
                }
              ],
              "uncoveredQuestions": ["Which German facility consumes this nickel sulfate?"],
              "sourceDocuments": ["doc-1"],
              "failure": null
            }
            """,
            action,
            {"SUPPLIES_TO"},
            "document",
        )

        state = _update_branch_state(None, plan, action, result)

        self.assertEqual(state.status, "needs_followup")
        self.assertEqual(state.open_questions, ["Which German facility consumes this nickel sulfate?"])
        self.assertEqual(state.remaining_frontier, ["Which German facility consumes this nickel sulfate?"])

    def test_final_summary_preserves_graph_contract(self) -> None:
        fragment = GraphFragment(
            fragmentId="frag-1",
            fragmentType="overview_chain",
            title="Primary chain",
            humanQuestion="How does the chain work?",
            humanSummary="Primary chain fragment.",
            branchId="b-1",
            observations=[
                GraphObservation(
                    observationId="obs-1",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="PRODUCES",
                    object="Nickel Ore",
                    objectType="PRODUCT",
                    confidence=0.8,
                    supportType="model_knowledge",
                    provenance=["source-a"],
                    supportNote="Needs source verification.",
                    branchId="b-1",
                )
            ],
            completeness="partial",
            qualityScore=0.5,
        )
        summary = build_final_chain_summary(
            question="How does the chain work?",
            selected_roles=[ROLE_SUPPLY_CHAIN],
            working_graph=fragment.observations,
            graph_fragments=[fragment],
            branches=[
                BranchState(
                    branchId="b-1",
                    hypothesis="Primary supply path",
                    humanQuestion="How does the chain work?",
                    humanSummary="Primary branch",
                    closureTarget="Recover the primary path.",
                    knownContext=["Mine --PRODUCES--> Nickel Ore"],
                    assignedRoles=[ROLE_SUPPLY_CHAIN],
                    status="completed",
                    qualityScore=0.5,
                    stagnationCount=0,
                    stopReason="",
                    resolvedFrontier=["Recovered the primary path."],
                    remainingFrontier=[],
                    openQuestions=[],
                    toolActions=[],
                    toolResults=[],
                    graphFragments=[fragment],
                )
            ],
            logic_trace=[],
            runtime_logs=[],
        )

        self.assertEqual(summary.selected_roles, [ROLE_SUPPLY_CHAIN])
        self.assertEqual(summary.graph_observations[0].relation, "PRODUCES")
        self.assertEqual(summary.graph_fragments[0].fragment_type, "overview_chain")
        self.assertEqual(summary.branches[0].branch_id, "b-1")
        self.assertEqual(summary.chain_summaries[0].steps[0], "Mine --PRODUCES--> Nickel Ore")
        self.assertIn("Needs source verification.", summary.open_verification_needs)


if __name__ == "__main__":
    unittest.main()
