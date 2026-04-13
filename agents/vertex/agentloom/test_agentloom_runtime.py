from __future__ import annotations

import unittest
from pydantic import ValidationError

from agentloom.agent import (
    CRITIC_CONFIG,
    GAP_BRIDGE_CONFIG,
    INVESTIGATOR_PLANNER_CONFIG,
    INVESTIGATOR_SYNTHESIZER_CONFIG,
    ROLE_CONTRACTS,
    ROLE_SELECTOR_CONFIG,
    RoleSpecializedInvestigatorAgent,
    SUPERVISOR_CONFIG,
    RETRIEVAL_MAX_OUTPUT_TOKENS,
    TOOL_MAX_OUTPUT_TOKENS,
    _coerce_supervisor_decision_payload,
    _branch_progress_delta,
    _apply_graph_fragment_mutations,
    _derive_candidate_paths,
    _derive_hypotheses,
    _graph_reachability_report,
    _normalize_agent_result,
    _normalize_followup_branch_plan,
    _placeholder_nodes_from_graph,
    _parse_tool_result_payload,
    _planned_tool_sequence,
    _tool_prompt,
    _tool_result_requires_reroute,
    _acceptance_block_reason,
    _update_branch_state,
)
from agentloom.composer import build_final_chain_summary
from agentloom.contracts import AgentResult, BranchPlan, BranchState, CandidatePath, FailureRecord, GraphFragment, GraphObservation, HypothesisState, InvestigationMission, SupervisorDecision, ToolAction, ToolActionResult
from agentloom.contracts import LearnedFinding
from agentloom.taxonomy import ROLE_CONTRACTS_BY_ROLE, ROLE_GOVERNANCE, ROLE_POLICY, ROLE_SUPPLY_CHAIN


class AgentLoomRuntimeTest(unittest.TestCase):
    def test_branch_plan_respects_llm_tool_order(self) -> None:
        plan = BranchPlan(
            branchId="branch-web",
            hypothesis="Public company and facility facts can be grounded from the web.",
            humanQuestion="Which companies operate EV battery facilities in Germany?",
            humanAction="Use web search to identify German battery manufacturers and their facilities.",
            closureTarget="Grounded FACILITY and ORGANIZATION nodes for German battery manufacturing.",
            knownContext=["The graph only contains a hypothesis-level German Battery Plant."],
            frontier=["Identify specific manufacturers.", "Ground the facility nodes."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["retrieve", "reason", "web_search"],
            targetEntities=["German Battery Plant", "Germany"],
            fragmentType="chain_fragment",
        )

        sequence = _planned_tool_sequence(
            plan,
            {"input_request": {"question": "q", "corpusDocuments": []}},
        )

        self.assertEqual(sequence[0], "retrieve")
        self.assertIn("retrieve", sequence)
        self.assertIn("web_search", sequence)

    def test_role_contracts_are_loaded_from_taxonomy_module(self) -> None:
        self.assertEqual(ROLE_CONTRACTS, ROLE_CONTRACTS_BY_ROLE)
        self.assertEqual(
            ROLE_CONTRACTS[ROLE_GOVERNANCE].contract_types,
            ["ownership-structure", "control-map"],
        )
        self.assertIn("PRODUCES", ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].path_carried_edges)
        self.assertIn("CONTROLS", ROLE_CONTRACTS[ROLE_GOVERNANCE].allowed_edge_types)
        self.assertIn("REGULATES", ROLE_CONTRACTS[ROLE_POLICY].allowed_edge_types)
        self.assertTrue(ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].investigation_playbook)
        self.assertIn("reason", ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].tool_use_guidance)
        self.assertTrue(ROLE_CONTRACTS[ROLE_POLICY].structural_closure_criteria)
        self.assertTrue(
            any("contractual" in item.lower() for item in ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].investigation_playbook)
        )
        self.assertTrue(
            any("stable" in item.lower() and "policy" in item.lower() for item in ROLE_CONTRACTS[ROLE_POLICY].investigation_playbook)
        )
        sc_playbook = "\n".join(ROLE_CONTRACTS[ROLE_SUPPLY_CHAIN].investigation_playbook)
        self.assertIn("Illustrative only", sc_playbook)
        self.assertIn("Adjacent-frame expansion", sc_playbook)

    def test_specialized_investigator_keeps_private_role_state(self) -> None:
        agent = RoleSpecializedInvestigatorAgent(ROLE_SUPPLY_CHAIN)

        self.assertEqual(agent.name, "supply_chain_investigator_agent")
        self.assertEqual(agent._specialization_role, ROLE_SUPPLY_CHAIN)
        self.assertEqual(agent._planner_name, "supply_chain_investigator_planner")
        self.assertEqual(agent._turn_reasoner_name, "supply_chain_hypothesis_turn_reasoner")
        self.assertEqual(agent._synthesizer_name, "supply_chain_investigator_synthesizer")

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
            toolName="reason",
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
          "toolName": "reason",
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

    def test_tool_payload_parser_preserves_patch_contract_fields(self) -> None:
        action = ToolAction(
            actionId="branch-2-a1",
            branchId="branch-2",
            toolName="web_search",
            humanQuestion="Which concrete facilities should replace the broad German sink?",
            humanAction="Replace the broad sink node with concrete receiving facilities.",
            whyThisQuestion="The current sink is a fake broad placeholder.",
            closureTarget="Replace the broad German sink node with concrete facilities.",
            knownContext=["The graph currently ends at German Battery Plant."],
            frontier=["Resolve the concrete sink facilities."],
            queryText="Which concrete facilities should replace the broad German sink?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["German Battery Plant", "Germany"],
            expectedFragmentType="chain_fragment",
        )
        raw = """
        {
          "actionId": "branch-2-a1",
          "branchId": "branch-2",
          "toolName": "web_search",
          "status": "success",
          "humanSummary": "Replaced the broad sink with a concrete facility.",
          "closureStatus": "closed",
          "resolvedFrontier": ["Replaced the broad German sink node."],
          "remainingFrontier": [],
          "discoveredFrontier": [],
          "graphFragments": [
            {
              "fragmentId": "branch-2-f1",
              "fragmentType": "chain_fragment",
              "title": "Concrete sink",
              "humanQuestion": "Which concrete facilities should replace the broad German sink?",
              "humanSummary": "ACC Kaiserslautern replaces the broad sink placeholder.",
              "branchId": "branch-2",
              "patchMode": "replace_node",
              "oldId": "facility:german-battery-plant",
              "patchRationale": "The prior node was only a broad placeholder and should be replaced by a concrete facility.",
              "observations": [
                {
                  "observationId": "o1",
                  "subject": "Nickel sulfate",
                  "subjectType": "PRODUCT",
                  "relation": "SUPPLIES_TO",
                  "object": "ACC Kaiserslautern Facility",
                  "objectType": "FACILITY",
                  "qualifiers": [],
                  "confidence": 0.78,
                  "supportType": "document",
                  "provenance": ["doc-1"],
                  "supportNote": "Grounded public evidence.",
                  "branchId": "branch-2",
                  "alternativeGroupId": ""
                }
              ],
              "completeness": "partial",
              "qualityScore": 0.8
            }
          ],
          "uncoveredQuestions": [],
          "sourceDocuments": ["doc-1"],
          "failure": null
        }
        """

        parsed = _parse_tool_result_payload(raw, action, {"SUPPLIES_TO"}, "document")

        self.assertEqual(parsed.graph_fragments[0].patch_mode, "replace_node")
        self.assertEqual(parsed.graph_fragments[0].old_id, "facility:german-battery-plant")
        self.assertIn("placeholder", parsed.graph_fragments[0].patch_rationale.lower())

    def test_tool_prompt_declares_single_graph_protocol_for_skeleton_and_mutation(self) -> None:
        action = ToolAction(
            actionId="branch-3-a1",
            branchId="branch-3",
            toolName="reason",
            humanQuestion="What is the complete picture of how nickel reaches German EV battery manufacturing?",
            humanAction="Build a whole hypothesis-level graph answering the question end-to-end.",
            whyThisQuestion="The graph is empty and needs a first connected answer.",
            closureTarget="One connected graph from source to destination covering the full question scope.",
            knownContext=["The graph is empty."],
            frontier=["Build the initial connected graph."],
            queryText="What is the complete picture of how nickel reaches German EV battery manufacturing?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["nickel", "Germany", "EV battery"],
            expectedFragmentType="overview_chain",
        )

        prompt = _tool_prompt(action, [], {}, "model")

        self.assertIn("There is one graph protocol only", prompt)
        self.assertIn("Do not invent a special answer shape for overview questions", prompt)
        self.assertIn("the same ToolActionResult -> GraphFragment format is used for the first skeleton, later enrichment, and graph mutation", prompt)
        self.assertIn("Every useful non-graph answer must also be carried by learnedFindings", prompt)
        self.assertIn("Do not confuse operational supply-chain flow with contractual procurement", prompt)

    def test_followup_branch_plan_inherits_hypothesis_context_and_roles(self) -> None:
        parent = BranchPlan(
            branchId="root",
            hypothesis="Root hypothesis",
            humanQuestion="What is the connected chain?",
            humanAction="Build the chain.",
            closureTarget="Connected graph.",
            knownContext=["Battery-grade Nickel Sulfate --SUPPLIES_TO--> German Battery Manufacturing Facility"],
            frontier=["Resolve the German sink placeholder."],
            assignedRoles=[ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            preferredTools=["reason"],
            targetEntities=["nickel sulfate", "German Battery Manufacturing Facility"],
            fragmentType="overview_chain",
        )
        followup = BranchPlan(
            branchId="resolve-sink",
            hypothesis="Resolve the sink placeholder",
            parentHypothesisId="root",
            hypothesisRelation="dependent_followup",
            pathKind="direct",
            humanQuestion="Which German facilities consume battery-grade nickel sulfate?",
            humanAction="Resolve concrete German sink facilities.",
            closureTarget="Concrete German battery facilities.",
            knownContext=[],
            frontier=[],
            assignedRoles=[ROLE_POLICY],
            preferredTools=[],
            targetEntities=[],
            fragmentType="chain_fragment",
        )
        branch_state = BranchState(
            branchId="root",
            hypothesis="Root hypothesis",
            humanQuestion=parent.human_question,
            humanSummary="Built a root skeleton.",
            closureTarget=parent.closure_target,
            knownContext=list(parent.known_context),
            assignedRoles=list(parent.assigned_roles),
            status="needs_followup",
            qualityScore=0.4,
            remainingFrontier=["Resolve the German sink placeholder."],
        )
        latest_result = ToolActionResult(
            actionId="root-a1",
            branchId="root",
            toolName="reason",
            pathAssessment="still_open",
            status="success",
            humanSummary="Built a root skeleton.",
            closureStatus="advanced",
            resolvedFrontier=["Established the main product path."],
            remainingFrontier=["Resolve the German sink placeholder."],
            discoveredFrontier=["Which German facilities consume battery-grade nickel sulfate?"],
            graphFragments=[
                GraphFragment(
                    fragmentId="root-f1",
                    fragmentType="overview_chain",
                    title="Root skeleton",
                    humanQuestion=parent.human_question,
                    humanSummary="Root skeleton",
                    branchId="root",
                    observations=[
                        GraphObservation(
                            observationId="o1",
                            subject="Battery-grade Nickel Sulfate",
                            subjectType="PRODUCT",
                            relation="SUPPLIES_TO",
                            object="German Battery Manufacturing Facility",
                            objectType="FACILITY",
                            qualifiers=[],
                            confidence=0.4,
                            supportType="hypothesis",
                            provenance=[],
                            supportNote="",
                            branchId="root",
                            alternativeGroupId="",
                        )
                    ],
                    completeness="partial",
                    qualityScore=0.4,
                )
            ],
            uncoveredQuestions=[],
            sourceDocuments=[],
            failure=None,
        )

        normalized = _normalize_followup_branch_plan(parent, branch_state, latest_result, {}, followup)

        self.assertEqual(normalized.assigned_roles, parent.assigned_roles)
        self.assertTrue(normalized.known_context)
        self.assertTrue(normalized.frontier)
        self.assertGreaterEqual(normalized.stop_after_no_improvement, 1)

    def test_update_branch_state_preserves_unresolved_structural_frontier(self) -> None:
        previous = BranchState(
            branchId="root",
            hypothesis="Root overview",
            pathKind="overview",
            humanQuestion="What is the complete chain?",
            humanSummary="Initial overview.",
            closureTarget="Connected overview graph.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            status="needs_followup",
            qualityScore=0.4,
            resolvedFrontier=["Established a broad operational path."],
            remainingFrontier=[
                "Identify concrete Indonesian mining facilities.",
                "Identify concrete German EV battery manufacturing facilities.",
            ],
            discoveredFrontier=["Determine if materially different alternative routes exist."],
            openQuestions=["Which concrete German facilities consume nickel sulfate?"],
        )
        plan = BranchPlan(
            branchId="root",
            hypothesis="Root overview",
            pathKind="overview",
            humanQuestion="What is the complete chain?",
            humanAction="Build the chain.",
            closureTarget="Connected overview graph.",
            knownContext=["The graph already has a broad source-to-sink skeleton."],
            frontier=["Identify concrete German EV battery manufacturing facilities."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["web_search"],
            targetEntities=["Germany"],
            fragmentType="overview_chain",
        )
        action = ToolAction(
            actionId="root-a2",
            branchId="root",
            pathKind="overview",
            toolName="web_search",
            humanQuestion="Which German facilities consume nickel sulfate?",
            humanAction="Resolve the German sink.",
            whyThisQuestion="The sink is still broad.",
            closureTarget="Concrete German receiving facilities.",
            knownContext=["Nickel Sulfate --SUPPLIES_TO--> German Battery Manufacturing Plant"],
            frontier=["Identify concrete German EV battery manufacturing facilities."],
            queryText="Which German facilities consume nickel sulfate?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["Germany"],
            expectedFragmentType="chain_fragment",
        )
        result = ToolActionResult(
            actionId="root-a2",
            branchId="root",
            toolName="web_search",
            pathAssessment="still_open",
            status="success",
            humanSummary="Found German facilities.",
            closureStatus="advanced",
            resolvedFrontier=["Identify concrete German EV battery manufacturing facilities."],
            remainingFrontier=["Map the intermediary precursor-to-cathode flow."],
            discoveredFrontier=["Identify a materially different alternative route family."],
            graphFragments=[],
            uncoveredQuestions=[],
            sourceDocuments=[],
            failure=None,
        )

        updated = _update_branch_state(previous, plan, action, result)

        self.assertIn("Identify concrete Indonesian mining facilities.", updated.remaining_frontier)
        self.assertIn("Map the intermediary precursor-to-cathode flow.", updated.remaining_frontier)
        self.assertNotIn("Identify concrete German EV battery manufacturing facilities.", updated.remaining_frontier)
        self.assertIn("Determine if materially different alternative routes exist.", updated.discovered_frontier)
        self.assertIn("Identify a materially different alternative route family.", updated.discovered_frontier)

    def test_placeholder_detector_catches_german_battery_manufacturing_plant(self) -> None:
        placeholders = _placeholder_nodes_from_graph(
            [
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel Sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:german-battery-manufacturing-plant",
                    object="German Battery Manufacturing Plant",
                    objectType="FACILITY",
                    confidence=0.3,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Broad sink placeholder.",
                    branchId="root",
                )
            ]
        )

        self.assertIn("German Battery Manufacturing Plant", placeholders)

    def test_root_overview_acceptance_is_blocked_by_placeholder_and_structural_frontier(self) -> None:
        plan = BranchPlan(
            branchId="root",
            hypothesis="Root overview",
            pathKind="overview",
            humanQuestion="What is the complete chain?",
            humanAction="Build the chain.",
            closureTarget="Connected overview graph.",
            knownContext=[],
            frontier=[],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["reason"],
            targetEntities=["nickel"],
            fragmentType="overview_chain",
        )
        branch_state = BranchState(
            branchId="root",
            hypothesis="Root overview",
            pathKind="overview",
            humanQuestion="What is the complete chain?",
            humanSummary="Overview is nearly complete.",
            closureTarget="Connected overview graph.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            status="completed",
            qualityScore=0.8,
            remainingFrontier=["Map the specific precursor-to-cathode flow for German EV OEMs beyond Umicore."],
            openQuestions=["Does Northvolt Drei have direct nickel sulfate supply agreements?"],
        )
        local_state = {
            "working_graph": [
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel Sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:german-battery-manufacturing-plant",
                    object="German Battery Manufacturing Plant",
                    objectType="FACILITY",
                    confidence=0.3,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Broad sink placeholder.",
                    branchId="root",
                ).model_dump(mode="json", by_alias=True)
            ]
        }

        reason = _acceptance_block_reason(plan, branch_state, local_state)

        self.assertIn("Broad placeholders remain", reason)

    def test_graph_fragment_replace_patch_requires_old_target_and_rationale(self) -> None:
        with self.assertRaises(ValidationError):
            GraphFragment(
                fragmentId="bad-fragment",
                fragmentType="chain_fragment",
                title="Bad replacement",
                humanQuestion="What replaces the broad sink?",
                humanSummary="Replacement without a target.",
                branchId="branch-3",
                patchMode="replace_node",
                observations=[],
                completeness="partial",
                qualityScore=0.3,
            )

    def test_replace_node_patch_mutates_working_graph_and_syncs_branch_fragments(self) -> None:
        placeholder_fragment = GraphFragment(
            fragmentId="overview-f1",
            fragmentType="overview_chain",
            title="Broad sink placeholder",
            humanQuestion="What is the broad chain?",
            humanSummary="A broad sink placeholder was used.",
            branchId="overview",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:german-battery-plant",
                    object="German Battery Plant",
                    objectType="FACILITY",
                    confidence=0.4,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Broad sink placeholder.",
                    branchId="overview",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.3,
        )
        policy_fragment = GraphFragment(
            fragmentId="policy-f1",
            fragmentType="policy_overlay",
            title="Policy on broad sink",
            humanQuestion="Which policy applies?",
            humanSummary="Policy was linked to the broad sink placeholder.",
            branchId="policy",
            observations=[
                GraphObservation(
                    observationId="o2",
                    subjectId="program:eu-battery-regulation",
                    subject="EU Battery Regulation",
                    subjectType="PROGRAM",
                    relation="IMPOSES_CONDITION",
                    objectId="facility:german-battery-plant",
                    object="German Battery Plant",
                    objectType="FACILITY",
                    confidence=0.5,
                    supportType="model_knowledge",
                    provenance=[],
                    supportNote="Broad sink placeholder.",
                    branchId="policy",
                )
            ],
            completeness="partial",
            qualityScore=0.4,
        )
        unrelated_observation = GraphObservation(
            observationId="o3",
            subjectId="organization:basf",
            subject="BASF SE",
            subjectType="ORGANIZATION",
            relation="OWNS",
            objectId="facility:schwarzheide",
            object="BASF Schwarzheide",
            objectType="FACILITY",
            confidence=0.9,
            supportType="document",
            provenance=["doc-1"],
            supportNote="Unrelated residual overlay.",
            branchId="ownership",
        )
        replacement_fragment = GraphFragment(
            fragmentId="sink-f1",
            fragmentType="chain_fragment",
            title="Concrete German sink",
            humanQuestion="Which concrete German facility should replace the broad sink?",
            humanSummary="ACC Kaiserslautern replaces the broad sink placeholder.",
            branchId="sink",
            patchMode="replace_node",
            oldId="facility:german-battery-plant",
            patchRationale="The broad sink placeholder must be replaced by a concrete facility node.",
            observations=[
                GraphObservation(
                    observationId="o4",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:acc-kaiserslautern",
                    object="ACC Kaiserslautern Facility",
                    objectType="FACILITY",
                    confidence=0.82,
                    supportType="document",
                    provenance=["doc-2"],
                    supportNote="Grounded sink replacement.",
                    branchId="sink",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        state = {
            "working_graph": [
                item.model_dump(mode="json", by_alias=True)
                for item in [*placeholder_fragment.observations, *policy_fragment.observations, unrelated_observation]
            ],
            "graph_fragments": [
                placeholder_fragment.model_dump(mode="json", by_alias=True),
                policy_fragment.model_dump(mode="json", by_alias=True),
            ],
            "branch_states": [
                BranchState(
                    branchId="overview",
                    hypothesis="Broad overview",
                    humanQuestion="What is the broad chain?",
                    humanSummary="Overview branch",
                    closureTarget="Build broad chain.",
                    assignedRoles=[ROLE_SUPPLY_CHAIN],
                    status="completed",
                    qualityScore=0.3,
                    stagnationCount=0,
                    resolvedFrontier=[],
                    remainingFrontier=[],
                    openQuestions=[],
                    toolActions=[],
                    toolResults=[],
                    graphFragments=[placeholder_fragment],
                ).model_dump(mode="json", by_alias=True),
                BranchState(
                    branchId="policy",
                    hypothesis="Policy overlay",
                    pathKind="indirect",
                    humanQuestion="Which policy applies?",
                    humanSummary="Policy branch",
                    closureTarget="Map policy overlay.",
                    assignedRoles=[ROLE_POLICY],
                    status="completed",
                    qualityScore=0.4,
                    stagnationCount=0,
                    resolvedFrontier=[],
                    remainingFrontier=[],
                    openQuestions=[],
                    toolActions=[],
                    toolResults=[],
                    graphFragments=[policy_fragment],
                ).model_dump(mode="json", by_alias=True),
            ],
        }

        result = _apply_graph_fragment_mutations(state, [replacement_fragment])

        mutated_graph = [GraphObservation.model_validate(item) for item in state["working_graph"]]
        self.assertIn("facility:german-battery-plant", result["replacedNodeIds"])
        self.assertFalse(any(obs.object_id == "facility:german-battery-plant" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "facility:acc-kaiserslautern" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "facility:schwarzheide" for obs in mutated_graph))

        mutated_fragments = [GraphFragment.model_validate(item) for item in state["graph_fragments"]]
        self.assertFalse(
            any(
                obs.object_id == "facility:german-battery-plant"
                for fragment in mutated_fragments
                for obs in fragment.observations
            )
        )
        synced_branches = [BranchState.model_validate(item) for item in state["branch_states"]]
        overview_branch = next(item for item in synced_branches if item.branch_id == "overview")
        self.assertEqual(overview_branch.graph_fragments, [])

    def test_replace_fragment_patch_supersedes_old_fragment_and_rebuilds_graph(self) -> None:
        old_fragment = GraphFragment(
            fragmentId="frag-old",
            fragmentType="chain_fragment",
            title="Weak bridge",
            humanQuestion="What links sulfate to the sink?",
            humanSummary="Weak bridge fragment.",
            branchId="bridge",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:broad-sink",
                    object="Broad Sink",
                    objectType="FACILITY",
                    confidence=0.3,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Weak bridge.",
                    branchId="bridge",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.2,
        )
        other_fragment = GraphFragment(
            fragmentId="frag-other",
            fragmentType="ownership_overlay",
            title="Ownership",
            humanQuestion="Who owns the plant?",
            humanSummary="Ownership fragment.",
            branchId="ownership",
            observations=[
                GraphObservation(
                    observationId="o2",
                    subjectId="organization:owner",
                    subject="Owner",
                    subjectType="ORGANIZATION",
                    relation="OWNS",
                    objectId="facility:plant",
                    object="Plant",
                    objectType="FACILITY",
                    confidence=0.9,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Grounded ownership.",
                    branchId="ownership",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        new_fragment = GraphFragment(
            fragmentId="frag-new",
            fragmentType="chain_fragment",
            title="Concrete bridge",
            humanQuestion="What concretely links sulfate to the sink?",
            humanSummary="Concrete bridge fragment.",
            branchId="bridge",
            patchMode="replace_fragment",
            oldIds=["frag-old"],
            patchRationale="The new bridge supersedes the earlier weak fragment.",
            observations=[
                GraphObservation(
                    observationId="o3",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:acc-kaiserslautern",
                    object="ACC Kaiserslautern Facility",
                    objectType="FACILITY",
                    confidence=0.82,
                    supportType="document",
                    provenance=["doc-2"],
                    supportNote="Grounded bridge.",
                    branchId="bridge",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        state = {
            "working_graph": [
                item.model_dump(mode="json", by_alias=True)
                for item in [*old_fragment.observations, *other_fragment.observations]
            ],
            "graph_fragments": [
                old_fragment.model_dump(mode="json", by_alias=True),
                other_fragment.model_dump(mode="json", by_alias=True),
            ],
            "branch_states": [],
        }

        result = _apply_graph_fragment_mutations(state, [new_fragment])

        self.assertIn("frag-old", result["replacedFragmentIds"])
        mutated_fragments = [GraphFragment.model_validate(item) for item in state["graph_fragments"]]
        self.assertEqual({item.fragment_id for item in mutated_fragments}, {"frag-new", "frag-other"})
        mutated_graph = [GraphObservation.model_validate(item) for item in state["working_graph"]]
        self.assertFalse(any(obs.object_id == "facility:broad-sink" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "facility:acc-kaiserslautern" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "facility:plant" for obs in mutated_graph))

    def test_replaced_node_tombstone_blocks_later_fragments_from_reintroducing_placeholder(self) -> None:
        replacement_fragment = GraphFragment(
            fragmentId="sink-f1",
            fragmentType="chain_fragment",
            title="Concrete German sink",
            humanQuestion="Which concrete facility replaces the broad sink?",
            humanSummary="ACC replaces the broad sink placeholder.",
            branchId="sink",
            patchMode="replace_node",
            oldId="facility:german-battery-plant",
            patchRationale="The broad sink placeholder must be removed from the operational graph.",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subjectId="product:nickel-sulfate",
                    subject="Nickel sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:acc-kaiserslautern",
                    object="ACC Kaiserslautern Facility",
                    objectType="FACILITY",
                    confidence=0.8,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Grounded sink.",
                    branchId="sink",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        stale_policy_fragment = GraphFragment(
            fragmentId="policy-f1",
            fragmentType="policy_overlay",
            title="Stale policy",
            humanQuestion="What policy hits the broad sink?",
            humanSummary="A stale branch still referenced the old placeholder.",
            branchId="policy",
            observations=[
                GraphObservation(
                    observationId="o2",
                    subjectId="program:eu-battery-passport",
                    subject="EU Battery Passport",
                    subjectType="PROGRAM",
                    relation="REGULATES",
                    objectId="facility:german-battery-plant",
                    object="German Battery Plant",
                    objectType="FACILITY",
                    confidence=0.7,
                    supportType="document",
                    provenance=["doc-2"],
                    supportNote="Should be stripped because the placeholder was already replaced.",
                    branchId="policy",
                ),
                GraphObservation(
                    observationId="o3",
                    subjectId="program:eu-battery-passport",
                    subject="EU Battery Passport",
                    subjectType="PROGRAM",
                    relation="IMPOSES_CONDITION",
                    objectId="product:nickel-sulfate",
                    object="Nickel sulfate",
                    objectType="PRODUCT",
                    confidence=0.7,
                    supportType="document",
                    provenance=["doc-2"],
                    supportNote="This edge should remain.",
                    branchId="policy",
                ),
            ],
            completeness="partial",
            qualityScore=0.7,
        )
        state = {"working_graph": [], "graph_fragments": [], "branch_states": []}

        _apply_graph_fragment_mutations(state, [replacement_fragment, stale_policy_fragment])

        mutated_graph = [GraphObservation.model_validate(item) for item in state["working_graph"]]
        self.assertFalse(any(obs.object_id == "facility:german-battery-plant" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "facility:acc-kaiserslautern" for obs in mutated_graph))
        self.assertTrue(any(obs.object_id == "product:nickel-sulfate" and obs.relation == "IMPOSES_CONDITION" for obs in mutated_graph))
        self.assertEqual(state["replaced_node_ids"], ["facility:german-battery-plant"])

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
              "toolName": "retrieve",
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
                toolName="retrieve",
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

    def test_tool_payload_parser_preserves_non_graph_findings(self) -> None:
        action = ToolAction(
            actionId="branch-ctx-a1",
            branchId="branch-ctx",
            toolName="web_search",
            humanQuestion="What are the procurement strategies for LGES Europe?",
            humanAction="Clarify the procurement model without inventing operational edges.",
            whyThisQuestion="The direct plant-level supplier edge was not confirmed.",
            closureTarget="Record the contractual procurement model and keep the operational gap explicit.",
            knownContext=["No direct plant-specific supplier edge is currently grounded."],
            frontier=["Clarify whether the missing information is a procurement model or an operational edge."],
            queryText="What are the procurement strategies for LGES Europe?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["LG Energy Solution Europe", "nickel sulfate"],
            expectedFragmentType="chain_fragment",
        )
        raw = """
        {
          "actionId": "branch-ctx-a1",
          "branchId": "branch-ctx",
          "toolName": "web_search",
          "pathAssessment": "still_open",
          "status": "partial",
          "humanSummary": "LG Energy Solution relies on diversified global procurement and recycling localization, but no plant-specific supplier edge was confirmed.",
          "closureStatus": "advanced",
          "resolvedFrontier": ["Clarified that the visible result is a procurement model rather than a direct facility-to-facility flow edge."],
          "remainingFrontier": ["Which operational intermediary supplies the European facilities?"],
          "discoveredFrontier": [],
          "graphFragments": [],
          "learnedFindings": [
            {
              "findingId": "branch-ctx-lf1",
              "hypothesisId": "branch-ctx",
              "branchId": "branch-ctx",
              "role": "supply_chain",
              "questionAsked": "What are the procurement strategies for LGES Europe?",
              "findingKind": "contractual_procurement",
              "summary": "LGES Europe uses diversified global procurement and recycling localization rather than a publicly confirmed plant-specific nickel sulfate supplier.",
              "evidenceStatus": "verified",
              "confidence": 0.71,
              "relatedEntities": ["LG Energy Solution Europe", "nickel sulfate"],
              "sourceDocuments": ["doc-ctx-1"]
            }
          ],
          "uncoveredQuestions": ["Which operational intermediary supplies the European facilities?"],
          "sourceDocuments": ["doc-ctx-1"],
          "failure": null
        }
        """

        parsed = _parse_tool_result_payload(raw, action, {"SUPPLIES_TO", "TRANSFORMS_INTO"}, "document")

        self.assertEqual(parsed.graph_fragments, [])
        self.assertEqual(parsed.learned_findings[0].finding_kind, "contractual_procurement")
        self.assertIn("procurement", parsed.learned_findings[0].summary.lower())

    def test_closed_branch_stops_even_when_next_frontier_is_discovered(self) -> None:
        plan = BranchPlan(
            branchId="resolve_sink",
            hypothesis="Concrete German receiving facilities can replace the broad sink node.",
            parentHypothesisId="full_hypothesis",
            hypothesisRelation="dependent_followup",
            spawnReason="The broad sink node must be replaced with concrete facilities.",
            independenceBasis="This branch refines the sink side of the parent hypothesis and should merge back into it.",
            pathKind="direct",
            humanQuestion="Which concrete German facilities should replace the broad sink node?",
            humanAction="Ground the receiving facilities and expose the next supplier question if one remains.",
            closureTarget="Replace the broad sink node with concrete German facilities.",
            knownContext=["The graph still uses a generic German sink node."],
            frontier=["Identify concrete German facilities."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["retrieve", "web_search"],
            targetEntities=["Germany", "battery facility"],
            fragmentType="chain_fragment",
        )
        action = ToolAction(
            actionId="resolve_sink-a1",
            branchId="resolve_sink",
            pathKind="direct",
            toolName="retrieve",
            humanQuestion=plan.human_question,
            humanAction=plan.human_action,
            whyThisQuestion="The current sink is too broad.",
            closureTarget=plan.closure_target,
            knownContext=plan.known_context,
            frontier=plan.frontier,
            queryText=plan.human_question,
            assignedRoles=plan.assigned_roles,
            targetEntities=plan.target_entities,
            expectedFragmentType=plan.fragment_type,
        )
        result = ToolActionResult(
            actionId="resolve_sink-a1",
            branchId="resolve_sink",
            toolName="retrieve",
            pathAssessment="confirmed",
            pathAssessmentNote="The broad sink node is now grounded as concrete German facilities.",
            status="success",
            humanSummary="Confirmed concrete German facilities and exposed the next supplier question.",
            closureStatus="closed",
            resolvedFrontier=["Replaced the broad sink node with concrete facilities."],
            remainingFrontier=["Who supplies nickel sulfate to those facilities?"],
            discoveredFrontier=["Determine the primary nickel sulfate suppliers for the named facilities."],
            graphFragments=[
                GraphFragment(
                    fragmentId="resolve_sink-f1",
                    fragmentType="chain_fragment",
                    title="Grounded sink facilities",
                    humanQuestion=plan.human_question,
                    humanSummary="Named German facilities replace the broad sink node.",
                    branchId="resolve_sink",
                    observations=[],
                    completeness="complete",
                    qualityScore=0.9,
                )
            ],
            uncoveredQuestions=["Who supplies nickel sulfate to those facilities?"],
            sourceDocuments=["doc-1"],
            failure=None,
        )

        updated = _update_branch_state(None, plan, action, result)

        self.assertEqual(updated.status, "completed")
        self.assertEqual(updated.parent_hypothesis_id, "full_hypothesis")
        self.assertEqual(updated.hypothesis_relation, "dependent_followup")
        self.assertIn("Who supplies nickel sulfate to those facilities?", updated.remaining_frontier)

    def test_agent_result_is_normalized_against_active_role_contract(self) -> None:
        mission = InvestigationMission(
            missionId="m-1",
            objective="Find supply-chain structure.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
            successCriteria=["Return supply-chain path edges only."],
        )
        raw = AgentResult(
            agentName="supply_chain_investigator_agent",
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
            preferredTools=["retrieve"],
            targetEntities=["Germany", "nickel sulfate"],
            fragmentType="chain_fragment",
            parallelGroup="downstream",
            stopAfterNoImprovement=2,
        )
        action = ToolAction(
            actionId="b-1-a1",
            branchId="b-1",
            toolName="retrieve",
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
              "toolName": "retrieve",
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
                    pathKind="overview",
                    pathAssessment="confirmed",
                    pathAssessmentNote="Primary path recovered.",
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
            hypotheses=[
                HypothesisState(
                    hypothesisId="b-1",
                    statement="Primary supply path",
                    pathKind="overview",
                    status="accepted",
                    summary="Primary path recovered.",
                    humanQuestion="How does the chain work?",
                    closureTarget="Recover the primary path.",
                    activeRoles=[ROLE_SUPPLY_CHAIN],
                    sourceBranchIds=["b-1"],
                    sourceFragmentIds=["frag-1"],
                    broadPictureFragmentIds=["frag-1"],
                    evidenceStatus="reasoned",
                    confidence=0.5,
                    resolvedFrontier=["Recovered the primary path."],
                    remainingFrontier=[],
                    openQuestions=[],
                    findingIds=["finding-1"],
                    loopCount=1,
                    lastUpdatedCycle=1,
                )
            ],
            candidate_paths=[],
            learned_findings=[
                LearnedFinding(
                    findingId="finding-1",
                    hypothesisId="b-1",
                    branchId="b-1",
                    role=ROLE_SUPPLY_CHAIN,
                    questionAsked="How does the chain work?",
                    findingKind="operational_flow",
                    summary="Mine produces Nickel Ore.",
                    evidenceStatus="reasoned",
                    confidence=0.5,
                    relatedEntities=["Mine", "Nickel Ore"],
                    sourceDocuments=["source-a"],
                )
            ],
            logic_trace=[],
            runtime_logs=[],
        )

        self.assertEqual(summary.selected_roles, [ROLE_SUPPLY_CHAIN])
        self.assertEqual(summary.graph_observations[0].relation, "PRODUCES")
        self.assertEqual(summary.graph_fragments[0].fragment_type, "overview_chain")
        self.assertEqual(summary.branches[0].branch_id, "b-1")
        self.assertEqual(summary.hypotheses[0].status, "accepted")
        self.assertEqual(summary.learned_findings[0].finding_id, "finding-1")
        self.assertEqual(summary.chain_summaries[0].steps[0], "Mine --PRODUCES--> Nickel Ore")
        self.assertIn("Needs source verification.", summary.open_verification_needs)
        self.assertEqual(summary.narrative_synthesis_status, "deterministic_only")

    def test_concise_answer_does_not_repeat_question_when_paths_exist(self) -> None:
        summary = build_final_chain_summary(
            question="How does the nickel chain reach German facilities?",
            selected_roles=[ROLE_SUPPLY_CHAIN],
            working_graph=[],
            graph_fragments=[],
            branches=[],
            hypotheses=[
                HypothesisState(
                    hypothesisId="ownership",
                    statement="Ownership is mapped.",
                    pathKind="indirect",
                    status="accepted",
                    summary="Ownership structures are mapped.",
                    humanQuestion="Who owns the refining layer?",
                    closureTarget="Map ownership.",
                    activeRoles=[ROLE_SUPPLY_CHAIN],
                    sourceBranchIds=["ownership"],
                    sourceFragmentIds=[],
                    broadPictureFragmentIds=[],
                    evidenceStatus="verified",
                    confidence=0.9,
                    resolvedFrontier=[],
                    remainingFrontier=[],
                    openQuestions=[],
                    findingIds=[],
                    loopCount=1,
                    lastUpdatedCycle=1,
                )
            ],
            candidate_paths=[
                CandidatePath(
                    pathId="overview",
                    pathKind="overview",
                    status="still_open",
                    title="Overview path",
                    summary="A backbone exists from Indonesian laterite to German consumers.",
                    startNode="Indonesian Laterite Deposit",
                    endNode="German consumer",
                    viaNodes=["Nickel Sulfate"],
                    supportingBranchIds=["overview"],
                    supportingFragmentIds=[],
                    evidenceStatus="mixed",
                    confidence=0.6,
                    unresolvedHops=["Name the German consumers."],
                ),
                CandidatePath(
                    pathId="direct",
                    pathKind="direct",
                    status="rejected",
                    title="Direct link",
                    summary="No direct public contract was found.",
                    startNode="Producer",
                    endNode="German consumer",
                    viaNodes=[],
                    supportingBranchIds=["direct"],
                    supportingFragmentIds=[],
                    evidenceStatus="reasoned",
                    confidence=0.3,
                    unresolvedHops=[],
                ),
            ],
            learned_findings=[],
            logic_trace=[],
            runtime_logs=[],
            open_gap_texts=["Gap A"],
            reachability_report={},
        )

        self.assertFalse(summary.concise_answer.startswith("How does the nickel chain reach German facilities?"))
        self.assertIn("Direct public route rejected", summary.concise_answer)
        self.assertTrue(
            "Remaining unresolved structural gaps: 1." in summary.concise_answer
            or "Unresolved gap themes (clustered): 1." in summary.concise_answer
        )
        self.assertIsNotNone(summary.internal_brief)
        self.assertEqual(summary.narrative_synthesis_status, "deterministic_only")
        self.assertIsNone(summary.structured_narrative)

    def test_candidate_paths_keep_rejected_direct_and_surviving_indirect_routes(self) -> None:
        indirect_fragment = GraphFragment(
            fragmentId="indirect-f1",
            fragmentType="alternative_chain",
            title="China-linked intermediary route",
            humanQuestion="Which intermediary route reaches Germany?",
            humanSummary="An indirect route through a Chinese midstream intermediary is currently best supported.",
            branchId="indirect-1",
            observations=[
                GraphObservation(
                    observationId="obs-indirect-1",
                    subject="Indonesian HPAL Plant",
                    subjectType="FACILITY",
                    relation="PRODUCES",
                    object="Nickel Matte",
                    objectType="PRODUCT",
                    confidence=0.72,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Grounded upstream step.",
                    branchId="indirect-1",
                ),
                GraphObservation(
                    observationId="obs-indirect-2",
                    subject="Nickel Matte",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="Chinese Cathode Processor",
                    objectType="FACILITY",
                    confidence=0.61,
                    supportType="mixed",
                    provenance=["doc-2"],
                    supportNote="Best-supported intermediary hop.",
                    branchId="indirect-1",
                ),
            ],
            completeness="partial",
            qualityScore=0.66,
        )
        candidates = _derive_candidate_paths(
            {
                "branch_states": [
                    BranchState(
                        branchId="direct-1",
                        hypothesis="Direct Indonesia to Germany route exists",
                        pathKind="direct",
                        pathAssessment="rejected",
                        pathAssessmentNote="No direct supply link to a German facility was confirmed.",
                        humanQuestion="Is there a direct Indonesia to Germany supply route?",
                        humanSummary="No direct route found.",
                        closureTarget="Direct route to Germany.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="completed",
                        qualityScore=0.2,
                        stagnationCount=0,
                        stopReason="No direct route found.",
                        resolvedFrontier=["Direct route hypothesis rejected."],
                        remainingFrontier=[],
                        openQuestions=[],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[],
                    ).model_dump(mode="json", by_alias=True),
                    BranchState(
                        branchId="indirect-1",
                        hypothesis="Indirect route via Chinese intermediary exists",
                        pathKind="indirect",
                        pathAssessment="best_supported_indirect",
                        pathAssessmentNote="Indirect route via Chinese midstream remains the strongest surviving explanation.",
                        humanQuestion="Which intermediary route reaches Germany?",
                        humanSummary="Indirect route remains strongest.",
                        closureTarget="Find best-supported indirect route.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="needs_followup",
                        qualityScore=0.66,
                        stagnationCount=0,
                        stopReason="",
                        resolvedFrontier=["Confirmed intermediary layer exists."],
                        remainingFrontier=["Identify the final German receiver after the intermediary."],
                        openQuestions=["Which German facility receives material after the intermediary?"],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[indirect_fragment],
                    ).model_dump(mode="json", by_alias=True),
                ],
                "graph_fragments": [indirect_fragment.model_dump(mode="json", by_alias=True)],
            }
        )

        self.assertEqual(candidates[0].status, "best_supported_indirect")
        self.assertEqual(candidates[0].path_kind, "indirect")
        self.assertEqual(candidates[1].status, "rejected")
        self.assertEqual(candidates[1].path_kind, "direct")

    def test_hypotheses_are_derived_from_branch_lifecycle(self) -> None:
        fragment = GraphFragment(
            fragmentId="frag-h1",
            fragmentType="overview_chain",
            title="Primary path",
            humanQuestion="What is the primary path?",
            humanSummary="A broad primary path exists.",
            branchId="h1",
            observations=[
                GraphObservation(
                    observationId="obs-h1",
                    subject="Deposit",
                    subjectType="DEPOSIT",
                    relation="EXTRACTS_FROM",
                    object="Mine",
                    objectType="FACILITY",
                    confidence=0.6,
                    supportType="model_knowledge",
                    provenance=[],
                    supportNote="Backbone hypothesis.",
                    branchId="h1",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.6,
        )
        hypotheses = _derive_hypotheses(
            {
                "supervisor_cycle": 3,
                "branch_states": [
                    BranchState(
                        branchId="h1",
                        hypothesis="Primary route exists",
                        pathKind="overview",
                        pathAssessment="confirmed",
                        pathAssessmentNote="Primary route is good enough to continue from.",
                        humanQuestion="What is the primary path?",
                        humanSummary="Primary hypothesis accepted.",
                        closureTarget="Recover the primary route.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="completed",
                        qualityScore=0.7,
                        stagnationCount=0,
                        stopReason="Branch produced a usable path result.",
                        resolvedFrontier=["Recovered primary route."],
                        remainingFrontier=[],
                        openQuestions=[],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[fragment],
                    ).model_dump(mode="json", by_alias=True),
                    BranchState(
                        branchId="h2",
                        hypothesis="Direct route to sink exists",
                        pathKind="direct",
                        pathAssessment="rejected",
                        pathAssessmentNote="No direct route was supported.",
                        humanQuestion="Does a direct route exist?",
                        humanSummary="Direct route rejected.",
                        closureTarget="Verify direct route.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="completed",
                        qualityScore=0.2,
                        stagnationCount=1,
                        stopReason="Branch rejected this candidate path.",
                        resolvedFrontier=[],
                        remainingFrontier=[],
                        openQuestions=[],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[],
                    ).model_dump(mode="json", by_alias=True),
                ],
                "graph_fragments": [fragment.model_dump(mode="json", by_alias=True)],
            }
        )

        self.assertEqual(hypotheses[0].hypothesis_id, "h1")
        self.assertEqual(hypotheses[0].status, "accepted")
        self.assertEqual(hypotheses[1].hypothesis_id, "h2")
        self.assertEqual(hypotheses[1].status, "rejected")

    def test_hypothesis_frontier_excludes_items_already_resolved(self) -> None:
        hypotheses = _derive_hypotheses(
            {
                "supervisor_cycle": 2,
                "branch_states": [
                    BranchState(
                        branchId="ownership",
                        hypothesis="Ownership can be mapped",
                        pathKind="indirect",
                        pathAssessment="confirmed",
                        pathAssessmentNote="Ownership mapped.",
                        humanQuestion="Who owns the facilities?",
                        humanSummary="Ownership mapped.",
                        closureTarget="Map ownership.",
                        knownContext=[],
                        assignedRoles=[ROLE_GOVERNANCE],
                        status="completed",
                        qualityScore=0.9,
                        stagnationCount=0,
                        stopReason="Ownership mapped.",
                        resolvedFrontier=["Identify owners", "Map ownership chain"],
                        remainingFrontier=["Identify owners", "Map ownership chain", "Find UBO"],
                        openQuestions=["Identify owners", "Find UBO"],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[],
                    ).model_dump(mode="json", by_alias=True)
                ],
                "graph_fragments": [],
            }
        )

        self.assertEqual(hypotheses[0].resolved_frontier, ["Identify owners", "Map ownership chain"])
        self.assertEqual(hypotheses[0].remaining_frontier, ["Find UBO"])
        self.assertEqual(hypotheses[0].open_questions, ["Find UBO"])

    def test_candidate_path_uses_flow_edges_for_start_and_end_nodes(self) -> None:
        fragment = GraphFragment(
            fragmentId="frag-acc",
            fragmentType="chain_fragment",
            title="ACC chain",
            humanQuestion="Which supplier reaches ACC?",
            humanSummary="ACC is a downstream sink.",
            branchId="acc-branch",
            observations=[
                GraphObservation(
                    observationId="loc-1",
                    subject="ACC Kaiserslautern Facility",
                    subjectType="FACILITY",
                    relation="LOCATED_IN",
                    object="Germany",
                    objectType="GEO_REGION",
                    confidence=0.9,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Location.",
                    branchId="acc-branch",
                ),
                GraphObservation(
                    observationId="flow-1",
                    subject="Nickel Sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="ACC Kaiserslautern Facility",
                    objectType="FACILITY",
                    confidence=0.6,
                    supportType="mixed",
                    provenance=["doc-2"],
                    supportNote="Supply edge.",
                    branchId="acc-branch",
                ),
            ],
            completeness="partial",
            qualityScore=0.7,
        )
        candidates = _derive_candidate_paths(
            {
                "branch_states": [
                    BranchState(
                        branchId="acc-branch",
                        hypothesis="ACC has a supplier",
                        pathKind="direct",
                        pathAssessment="still_open",
                        pathAssessmentNote="Supplier unresolved.",
                        humanQuestion="Which supplier reaches ACC?",
                        humanSummary="Need supplier.",
                        closureTarget="Resolve supplier.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="needs_followup",
                        qualityScore=0.7,
                        stagnationCount=0,
                        stopReason="",
                        resolvedFrontier=[],
                        remainingFrontier=["Find supplier"],
                        openQuestions=["Find supplier"],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[fragment],
                    ).model_dump(mode="json", by_alias=True),
                ],
                "graph_fragments": [fragment.model_dump(mode="json", by_alias=True)],
            }
        )

        self.assertEqual(candidates[0].start_node, "Nickel Sulfate")
        self.assertEqual(candidates[0].end_node, "ACC Kaiserslautern Facility")

    def test_candidate_paths_collapse_dependent_followups_into_parent_hypothesis(self) -> None:
        root_fragment = GraphFragment(
            fragmentId="root-frag",
            fragmentType="overview_chain",
            title="Root chain",
            humanQuestion="What is the broad chain?",
            humanSummary="A broad placeholder chain.",
            branchId="root",
            observations=[
                GraphObservation(
                    observationId="root-1",
                    subject="Nickel Sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="German Battery Plant",
                    objectType="FACILITY",
                    confidence=0.3,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Broad placeholder.",
                    branchId="root",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.4,
        )
        sink_fragment = GraphFragment(
            fragmentId="sink-frag",
            fragmentType="chain_fragment",
            title="Concrete sink",
            humanQuestion="Which concrete sink replaces the placeholder?",
            humanSummary="BASF Schwarzheide replaces the broad sink.",
            branchId="sink",
            patchMode="replace_node",
            oldId="facility:german-battery-plant",
            patchRationale="The broad sink is replaced by a concrete facility.",
            observations=[
                GraphObservation(
                    observationId="sink-1",
                    subject="Nickel Sulfate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="BASF Schwarzheide",
                    objectType="FACILITY",
                    confidence=0.85,
                    supportType="document",
                    provenance=["doc-1"],
                    supportNote="Grounded sink replacement.",
                    branchId="sink",
                )
            ],
            completeness="complete",
            qualityScore=0.9,
        )

        candidates = _derive_candidate_paths(
            {
                "branch_states": [
                    BranchState(
                        branchId="root",
                        hypothesis="Root hypothesis",
                        hypothesisRelation="root",
                        pathKind="overview",
                        pathAssessment="still_open",
                        humanQuestion="What is the broad chain?",
                        humanSummary="Need concrete sink.",
                        closureTarget="Build root chain.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="needs_followup",
                        qualityScore=0.5,
                        stagnationCount=0,
                        stopReason="",
                        resolvedFrontier=["Broad path exists."],
                        remainingFrontier=["Replace broad sink"],
                        openQuestions=["Which concrete sink replaces the placeholder?"],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[root_fragment],
                    ).model_dump(mode="json", by_alias=True),
                    BranchState(
                        branchId="sink",
                        hypothesis="Concrete sink exists",
                        parentHypothesisId="root",
                        hypothesisRelation="dependent_followup",
                        pathKind="direct",
                        pathAssessment="confirmed",
                        pathAssessmentNote="Concrete sink identified.",
                        humanQuestion="Which concrete sink replaces the placeholder?",
                        humanSummary="BASF Schwarzheide found.",
                        closureTarget="Replace broad sink.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="completed",
                        qualityScore=0.9,
                        stagnationCount=0,
                        stopReason="Concrete sink found.",
                        resolvedFrontier=["Replace broad sink"],
                        remainingFrontier=[],
                        openQuestions=[],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[sink_fragment],
                    ).model_dump(mode="json", by_alias=True),
                ],
                "graph_fragments": [
                    root_fragment.model_dump(mode="json", by_alias=True),
                    sink_fragment.model_dump(mode="json", by_alias=True),
                ],
            }
        )

        self.assertEqual(len(candidates), 1)
        self.assertEqual(candidates[0].path_id, "root")
        self.assertEqual(candidates[0].supporting_branch_ids, ["root", "sink"])
        self.assertEqual(candidates[0].end_node, "BASF Schwarzheide")

    def test_reachability_treats_terminal_supply_sink_as_structurally_complete_chain(self) -> None:
        report = _graph_reachability_report(
            {
                "working_graph": [
                    {"subject": "Deposit", "subjectType": "DEPOSIT", "relation": "EXTRACTS_FROM", "object": "Mine", "objectType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"subject": "Mine", "subjectType": "FACILITY", "relation": "PRODUCES", "object": "Nickel Ore", "objectType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"subject": "Nickel Ore", "subjectType": "PRODUCT", "relation": "TRANSFORMS_INTO", "object": "Nickel Sulfate", "objectType": "PRODUCT", "confidence": 0.4, "supportType": "model_knowledge"},
                    {"subject": "Nickel Sulfate", "subjectType": "PRODUCT", "relation": "SUPPLIES_TO", "object": "BASF Schwarzheide", "objectType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                    {"subject": "Nickel Sulfate", "subjectType": "PRODUCT", "relation": "SUPPLIES_TO", "object": "BASF Schwarzheide", "objectType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                ]
            }
        )

        self.assertEqual(len(report["partialChains"]), 0)
        self.assertEqual(len(report["groundedCompleteChains"]), 0)
        self.assertEqual(len(report["structurallyCompleteChains"]), 1)
        self.assertEqual(report["structurallyCompleteChains"][0]["sink"], "BASF Schwarzheide")

    def test_reachability_hypothesis_terminal_sink_is_structurally_complete_not_partial(self) -> None:
        report = _graph_reachability_report(
            {
                "working_graph": [
                    {"subject": "Deposit", "subjectType": "DEPOSIT", "relation": "EXTRACTS_FROM", "object": "Mine", "objectType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"subject": "Mine", "subjectType": "FACILITY", "relation": "PRODUCES", "object": "Nickel Ore", "objectType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"subject": "Nickel Ore", "subjectType": "PRODUCT", "relation": "TRANSFORMS_INTO", "object": "Nickel Sulfate", "objectType": "PRODUCT", "confidence": 0.2, "supportType": "hypothesis"},
                    {"subject": "Nickel Sulfate", "subjectType": "PRODUCT", "relation": "SUPPLIES_TO", "object": "BASF Schwarzheide", "objectType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                ]
            }
        )

        self.assertEqual(len(report["partialChains"]), 0)
        self.assertEqual(len(report["structurallyCompleteChains"]), 1)
        self.assertIn("hypothesis", report["structurallyCompleteChains"][0]["structuralReason"].lower())

    def test_hypotheses_preserve_parent_and_independence_metadata(self) -> None:
        hypotheses = _derive_hypotheses(
            {
                "supervisor_cycle": 4,
                "branch_states": [
                    BranchState(
                        branchId="root_path",
                        hypothesis="A primary HPAL-based path reaches German battery manufacturing.",
                        pathKind="overview",
                        pathAssessment="confirmed",
                        pathAssessmentNote="Primary path confirmed.",
                        humanQuestion="What is the primary path?",
                        humanSummary="Primary hypothesis accepted.",
                        closureTarget="Build the primary path.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="completed",
                        qualityScore=0.8,
                        stagnationCount=0,
                        stopReason="",
                        resolvedFrontier=["Primary path exists."],
                        remainingFrontier=[],
                        openQuestions=[],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[],
                    ).model_dump(mode="json", by_alias=True),
                    BranchState(
                        branchId="alt_route_jurisdiction",
                        hypothesis="A different jurisdiction may provide an alternative route.",
                        parentHypothesisId="root_path",
                        hypothesisRelation="independent_child",
                        spawnReason="The primary path is clear enough to study an alternative jurisdiction.",
                        independenceBasis="This route can be investigated independently of unresolved details in the parent path.",
                        pathKind="alternative",
                        pathAssessment="still_open",
                        pathAssessmentNote="Alternative path still being explored.",
                        humanQuestion="What alternative jurisdictional route exists?",
                        humanSummary="Alternative route still open.",
                        closureTarget="Establish an alternative jurisdictional path.",
                        knownContext=[],
                        assignedRoles=[ROLE_SUPPLY_CHAIN],
                        status="active",
                        qualityScore=0.4,
                        stagnationCount=0,
                        stopReason="",
                        resolvedFrontier=[],
                        remainingFrontier=["Identify the alternative jurisdiction."],
                        openQuestions=["Which alternative jurisdiction supplies this path?"],
                        toolActions=[],
                        toolResults=[],
                        graphFragments=[],
                    ).model_dump(mode="json", by_alias=True),
                ],
                "graph_fragments": [],
            }
        )

        child = next(item for item in hypotheses if item.hypothesis_id == "alt_route_jurisdiction")
        self.assertEqual(child.parent_hypothesis_id, "root_path")
        self.assertEqual(child.root_hypothesis_id, "root_path")
        self.assertEqual(child.hypothesis_relation, "independent_child")
        self.assertIn("alternative jurisdiction", child.spawn_reason.lower())

    def test_supervisor_delegate_decision_requires_mission(self) -> None:
        with self.assertRaises(ValueError):
            SupervisorDecision(
                cycle=3,
                assessment="Need critic review.",
                rationale=["Progress flattened."],
                nextAgent="critic_agent",
                mission=None,
            )

    def test_supervisor_coercion_fills_missing_critic_mission(self) -> None:
        raw = {
            "cycle": 5,
            "assessment": "Send to critic.",
            "rationale": ["Coverage check."],
            "nextAgent": "critic_agent",
        }
        fixed = _coerce_supervisor_decision_payload(
            raw,
            selected_role_literals=[ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            cycle=5,
            user_question="Trace lithium to Germany?",
        )
        decision = SupervisorDecision.model_validate(fixed)
        self.assertIsNotNone(decision.mission)
        self.assertEqual(set(decision.mission.assigned_roles), {ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE})
        self.assertTrue(decision.mission.objective.strip())
        self.assertEqual(decision.mission.grounding_mode, "mixed")

    def test_supervisor_coercion_forces_single_role_for_specialized_investigator(self) -> None:
        raw = {
            "cycle": 2,
            "assessment": "Supply work.",
            "rationale": ["Need flow edges."],
            "nextAgent": "supply_chain_investigator_agent",
            "mission": {
                "missionId": "bad",
                "objective": "x",
                "assignedRoles": [ROLE_GOVERNANCE, ROLE_SUPPLY_CHAIN],
                "groundingMode": "mixed",
            },
        }
        fixed = _coerce_supervisor_decision_payload(
            raw,
            selected_role_literals=[ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            cycle=2,
            user_question="Q?",
        )
        decision = SupervisorDecision.model_validate(fixed)
        self.assertEqual(decision.mission.assigned_roles, [ROLE_SUPPLY_CHAIN])

    def test_supervisor_coercion_overwrites_wrong_cycle(self) -> None:
        raw = {
            "cycle": 99,
            "assessment": "Continue supply.",
            "rationale": ["LLM repeated an old cycle number."],
            "nextAgent": "supply_chain_investigator_agent",
            "mission": {
                "missionId": "m1",
                "objective": "Close one hop.",
                "assignedRoles": [ROLE_SUPPLY_CHAIN],
                "groundingMode": "mixed",
            },
        }
        fixed = _coerce_supervisor_decision_payload(
            raw,
            selected_role_literals=[ROLE_SUPPLY_CHAIN],
            cycle=7,
            user_question="Q?",
        )
        decision = SupervisorDecision.model_validate(fixed)
        self.assertEqual(decision.cycle, 7)

    def test_supervisor_coercion_stamps_cycle_for_compose(self) -> None:
        raw = {
            "cycle": 5,
            "assessment": "Ready.",
            "rationale": ["Critic ok."],
            "nextAgent": "compose",
        }
        fixed = _coerce_supervisor_decision_payload(
            raw,
            selected_role_literals=[ROLE_SUPPLY_CHAIN],
            cycle=12,
            user_question="Q?",
        )
        decision = SupervisorDecision.model_validate(fixed)
        self.assertEqual(decision.cycle, 12)

    def test_reachability_does_not_treat_hypothesis_chain_as_complete(self) -> None:
        state = {
            "working_graph": [
                {
                    "subject": "Indonesian HPAL Plant",
                    "subjectType": "FACILITY",
                    "relation": "EXTRACTS_FROM",
                    "object": "Indonesian Laterite Deposit",
                    "objectType": "DEPOSIT",
                    "confidence": 0.3,
                    "supportType": "hypothesis",
                },
                {
                    "subject": "Indonesian HPAL Plant",
                    "subjectType": "FACILITY",
                    "relation": "PRODUCES",
                    "object": "Nickel Sulfate",
                    "objectType": "PRODUCT",
                    "confidence": 0.4,
                    "supportType": "model_knowledge",
                },
                {
                    "subject": "Nickel Sulfate",
                    "subjectType": "PRODUCT",
                    "relation": "TRANSFORMS_INTO",
                    "object": "Battery Cathode",
                    "objectType": "PRODUCT",
                    "confidence": 0.4,
                    "supportType": "model_knowledge",
                },
                {
                    "subject": "Battery Cathode",
                    "subjectType": "PRODUCT",
                    "relation": "USED_IN",
                    "object": "German Battery Plant",
                    "objectType": "FACILITY",
                    "confidence": 0.4,
                    "supportType": "hypothesis",
                },
            ]
        }

        report = _graph_reachability_report(state)

        self.assertEqual(report["groundedCompleteChains"], [])
        self.assertTrue(report["structurallyCompleteChains"] or report["partialChains"])
        self.assertIn("No document-grounded complete chain", report["healthSummary"])

    def test_vertex_partial_can_reroute_to_web(self) -> None:
        plan = BranchPlan(
            branchId="branch-reroute",
            hypothesis="Public supply chain links can be grounded on the web.",
            humanQuestion="Which German facilities receive nickel sulfate?",
            humanAction="Use web search to identify German facilities and their supply links.",
            closureTarget="Grounded downstream FACILITY nodes and SUPPLIES_TO edges.",
            knownContext=["Nickel sulfate is already in the graph."],
            frontier=["Identify the receiving German facilities."],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["retrieve", "reason"],
            targetEntities=["Nickel Sulfate", "Germany"],
            fragmentType="chain_fragment",
        )
        result = _parse_tool_result_payload(
            """
            {
              "actionId": "branch-reroute-a1",
              "branchId": "branch-reroute",
              "toolName": "retrieve",
              "status": "partial",
              "humanSummary": "The available corpus lacks data on German EV battery manufacturing and its nickel supply chain.",
              "closureStatus": "unchanged",
              "resolvedFrontier": [],
              "remainingFrontier": ["Identify the receiving German facilities."],
              "discoveredFrontier": [],
              "graphFragments": [],
              "uncoveredQuestions": ["Which German facilities receive nickel sulfate?"],
              "sourceDocuments": [],
              "failure": {
                "code": "missing_corpus",
                "message": "The available corpus lacks data on German EV battery manufacturing and its nickel supply chain.",
                "blocking": false,
                "missingInputs": ["EV_battery_manufacturing_data"]
              }
            }
            """,
            ToolAction(
                actionId="branch-reroute-a1",
                branchId="branch-reroute",
                toolName="retrieve",
                humanQuestion="Which German facilities receive nickel sulfate?",
                humanAction="Use web search to identify German facilities and their supply links.",
                whyThisQuestion="Need to close the downstream gap.",
                closureTarget="Grounded downstream FACILITY nodes and SUPPLIES_TO edges.",
                knownContext=["Nickel sulfate is already in the graph."],
                frontier=["Identify the receiving German facilities."],
                queryText="Which German facilities receive nickel sulfate?",
                assignedRoles=[ROLE_SUPPLY_CHAIN],
                targetEntities=["Nickel Sulfate", "Germany"],
                expectedFragmentType="chain_fragment",
            ),
            {"SUPPLIES_TO"},
            "document",
        )

        reroute, reason = _tool_result_requires_reroute(
            plan,
            result,
            {"retrieve"},
            {"input_request": {"question": "q", "corpusDocuments": []}},
        )

        self.assertEqual(reroute[:1], ["web_search"])
        self.assertIn("switching", reason.lower())


if __name__ == "__main__":
    unittest.main()
