from __future__ import annotations

import json
import os
import unittest
from pydantic import ValidationError

from agentloom.agent import (
    STATE_FINAL,
    STATE_GRAPH_MUTATION_LOG,
    STATE_BRANCH_STATES,
    STATE_CURRENT_MISSION,
    STATE_HYPOTHESIS_ENTITY_REGISTRY,
    STATE_INVESTIGATION_EPOCH,
    STATE_LAST_GAP_FINGERPRINT,
    STATE_LAST_CRITIC_VERDICT,
    STATE_LOGIC_TRACE,
    STATE_OPEN_GAPS,
    STATE_OPEN_GAP_STREAK,
    STATE_GLOBAL_STAGNATION,
    STATE_PROGRESS_HISTORY,
    STATE_SELECTED_ROLES,
    STATE_WORKING_GRAPH,
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
    _host_local_process_gap_candidates,
    InvestigatorPlan,
    _normalize_agent_result,
    _normalize_followup_branch_plan,
    _normalize_investigator_plan,
    _placeholder_nodes_from_graph,
    _parse_tool_result_payload,
    _planned_tool_sequence,
    _tool_prompt,
    _tool_result_requires_reroute,
    _acceptance_block_reason,
    _detect_slot_endpoint_conflicts,
    _placeholder_resolution_hints,
    _update_branch_state,
    _delegate_constraints,
    _merge_observation,
    _orchestration_policy_allow_compose,
    _unified_progress_report,
)
from agentloom.composer import build_final_chain_summary
from agentloom.runner import _trace_reasoning_digest, _working_graph_from_logic_trace
from agentloom.contracts import (
    AgentResult,
    BranchPlan,
    BranchState,
    CandidatePath,
    CriticAgentOutput,
    EntityNode,
    FailureRecord,
    FlowLeg,
    GraphFragment,
    GraphObservation,
    HypothesisState,
    InvestigationMission,
    ProcessStep,
    SupervisorDecision,
    ToolAction,
    ToolActionResult,
)
from agentloom.contracts import LearnedFinding
from agentloom.operational_graph import graph_counts, graph_state_from_raw, observation_projection
from agentloom.taxonomy import ROLE_CONTRACTS_BY_ROLE, ROLE_GOVERNANCE, ROLE_POLICY, ROLE_SUPPLY_CHAIN
import agentloom.limits as agentloom_limits


def _working_graph_projection(state: dict) -> list[GraphObservation]:
    return observation_projection(graph_state_from_raw(state.get("working_graph") or []))


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

        self.assertEqual(parsed.graph_fragments[0].patch_mode, "replace_entity")
        self.assertEqual(parsed.graph_fragments[0].old_id, "facility:german-battery-plant")
        self.assertIn("placeholder", parsed.graph_fragments[0].patch_rationale.lower())

    def test_tool_payload_parser_accepts_operational_primitives_fragment(self) -> None:
        action = ToolAction(
            actionId="branch-op-a1",
            branchId="branch-op",
            toolName="reason",
            humanQuestion="How does spodumene reach a battery-grade precursor plant?",
            humanAction="Build an operational supply-chain fragment.",
            whyThisQuestion="The runtime should use flow legs and process steps instead of a fake edge-only chain.",
            closureTarget="Recover one operational route with named product legs and node-local conversion.",
            knownContext=["A placeholder downstream precursor plant exists in the graph."],
            frontier=["Identify the material leg.", "Identify the local conversion step."],
            queryText="How does spodumene reach a battery-grade precursor plant?",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["spodumene", "precursor plant"],
            expectedFragmentType="operational_flow",
        )
        raw = """
        {
          "status": "success",
          "humanSummary": "Recovered an operational route with a local transformation step.",
          "closureStatus": "advanced",
          "graphFragments": [
            {
              "fragmentType": "operational_flow",
              "title": "Spodumene conversion route",
              "humanQuestion": "How does spodumene reach a battery-grade precursor plant?",
              "humanSummary": "Mine concentrate moves to a conversion plant and is transformed there.",
              "branchId": "branch-op",
              "entityNodes": [
                {"nodeId": "facility:mine", "label": "Mine Concentrator", "entityType": "FACILITY", "confidence": 0.6, "supportType": "model_knowledge"},
                {"nodeId": "facility:converter", "label": "Lithium Conversion Plant", "entityType": "FACILITY", "confidence": 0.6, "supportType": "model_knowledge"},
                {"nodeId": "product:spodumene", "label": "Spodumene concentrate", "entityType": "PRODUCT", "confidence": 0.6, "supportType": "model_knowledge"},
                {"nodeId": "product:lithium-carbonate", "label": "Lithium carbonate", "entityType": "PRODUCT", "confidence": 0.6, "supportType": "model_knowledge"}
              ],
              "flowLegs": [
                {
                  "legId": "leg-1",
                  "supplierNodeId": "facility:mine",
                  "customerNodeId": "facility:converter",
                  "productNodeId": "product:spodumene",
                  "confidence": 0.55,
                  "supportType": "model_knowledge",
                  "provenance": [],
                  "supportNote": "Typical concentrate shipment leg.",
                  "branchId": "branch-op"
                }
              ],
              "processSteps": [
                {
                  "stepId": "step-1",
                  "hostNodeId": "facility:converter",
                  "processKind": "transformation",
                  "inputNodeIds": ["product:spodumene"],
                  "outputNodeIds": ["product:lithium-carbonate"],
                  "confidence": 0.55,
                  "supportType": "model_knowledge",
                  "provenance": [],
                  "supportNote": "Typical local conversion step.",
                  "branchId": "branch-op"
                }
              ],
              "overlayEdges": [],
              "qualityScore": 0.58
            }
          ]
        }
        """

        parsed = _parse_tool_result_payload(
            raw,
            action,
            {"SUPPLIES_TO", "PRODUCES", "TRANSFORMS_INTO"},
            "model_knowledge",
        )

        fragment = parsed.graph_fragments[0]
        self.assertEqual(fragment.fragment_type, "operational_flow")
        self.assertEqual(len(fragment.entity_nodes), 4)
        self.assertEqual(len(fragment.flow_legs), 1)
        self.assertEqual(len(fragment.process_steps), 1)
        self.assertTrue(any(obs.relation == "SUPPLIES_TO" for obs in fragment.observations))
        self.assertTrue(any(obs.relation == "TRANSFORMS_INTO" for obs in fragment.observations))

    def test_tool_payload_parser_downgrades_empty_graph_fragments_to_partial(self) -> None:
        action = ToolAction(
            actionId="branch-empty-a1",
            branchId="branch-empty",
            toolName="reason",
            humanQuestion="Return a structural fragment.",
            humanAction="Build a graph fragment.",
            whyThisQuestion="A typed graph fragment is required.",
            closureTarget="Return at least one typed graph payload.",
            knownContext=["The graph needs a structural fragment."],
            frontier=["Return typed graph payload."],
            queryText="Return a structural fragment.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            targetEntities=["lithium"],
            expectedFragmentType="operational_flow",
        )
        raw = """
        {
          "status": "success",
          "humanSummary": "Returned a fragment shell.",
          "closureStatus": "closed",
          "graphFragments": [
            {
              "fragmentId": "branch-empty-f1",
              "fragmentType": "operational_flow",
              "title": "Empty shell",
              "humanQuestion": "Return a structural fragment.",
              "humanSummary": "This fragment carries no payload.",
              "branchId": "branch-empty",
              "entityNodes": [],
              "flowLegs": [],
              "processSteps": [],
              "overlayEdges": [],
              "qualityScore": 0.2
            }
          ]
        }
        """

        parsed = _parse_tool_result_payload(raw, action, {"SUPPLIES_TO", "TRANSFORMS_INTO"}, "model_knowledge")

        self.assertEqual(parsed.status, "partial")
        self.assertEqual(parsed.graph_fragments, [])
        self.assertIsNotNone(parsed.failure)
        self.assertIn("typed_graph_fragment", parsed.failure.missing_inputs)

    def test_native_fragment_discards_conflicting_raw_observations(self) -> None:
        fragment = GraphFragment(
            fragmentId="native-f1",
            fragmentType="operational_flow",
            title="Native fragment",
            humanQuestion="How does product A move to plant B?",
            humanSummary="A native flow leg should override any raw observation lane.",
            branchId="branch-native",
            entityNodes=[
                EntityNode(nodeId="facility:a", label="Facility A", entityType="FACILITY", confidence=0.6, supportType="model_knowledge"),
                EntityNode(nodeId="facility:b", label="Facility B", entityType="FACILITY", confidence=0.6, supportType="model_knowledge"),
                EntityNode(nodeId="product:a", label="Product A", entityType="PRODUCT", confidence=0.6, supportType="model_knowledge"),
            ],
            flowLegs=[
                FlowLeg(
                    legId="leg-1",
                    supplierNodeId="facility:a",
                    customerNodeId="facility:b",
                    productNodeId="product:a",
                    confidence=0.6,
                    supportType="model_knowledge",
                )
            ],
            observations=[
                GraphObservation(
                    observationId="legacy-1",
                    subjectId="product:a",
                    subject="Product A",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:b",
                    object="Facility B",
                    objectType="FACILITY",
                    confidence=0.2,
                    supportType="hypothesis",
                )
            ],
            completeness="partial",
            qualityScore=0.5,
        )

        self.assertEqual(len(fragment.flow_legs), 1)
        self.assertFalse(any(obs.subject_type == "PRODUCT" and obs.relation == "SUPPLIES_TO" for obs in fragment.observations))
        projected = [obs for obs in fragment.observations if obs.relation == "SUPPLIES_TO"]
        self.assertEqual(len(projected), 1)
        self.assertEqual(projected[0].subject_id, "facility:a")

    def test_graph_fragment_preserves_full_derived_projection_without_truncation(self) -> None:
        nodes = [
            EntityNode(nodeId="facility:converter", label="Converter", entityType="FACILITY", confidence=0.5, supportType="model_knowledge")
        ]
        steps: list[ProcessStep] = []
        for idx in range(25):
            input_id = f"product:stage-{idx}"
            output_id = f"product:stage-{idx + 1}"
            nodes.append(EntityNode(nodeId=input_id, label=f"Stage {idx}", entityType="PRODUCT", confidence=0.5, supportType="model_knowledge"))
            nodes.append(EntityNode(nodeId=output_id, label=f"Stage {idx + 1}", entityType="PRODUCT", confidence=0.5, supportType="model_knowledge"))
            steps.append(
                ProcessStep(
                    stepId=f"step-{idx}",
                    hostNodeId="facility:converter",
                    processKind="transformation",
                    inputNodeIds=[input_id],
                    outputNodeIds=[output_id],
                    confidence=0.5,
                    supportType="model_knowledge",
                )
            )
        fragment = GraphFragment(
            fragmentId="long-f1",
            fragmentType="operational_flow",
            title="Long projection",
            humanQuestion="How does the long conversion chain work?",
            humanSummary="The fragment contains many native process steps.",
            branchId="branch-long",
            entityNodes=nodes,
            processSteps=steps,
            completeness="partial",
            qualityScore=0.4,
        )

        self.assertEqual(len(fragment.process_steps), 25)
        self.assertEqual(len(fragment.observations), 50)

    def test_graph_fragment_rejects_invalid_native_flow_leg_endpoint_types(self) -> None:
        with self.assertRaises(ValidationError):
            GraphFragment(
                fragmentId="bad-leg",
                fragmentType="operational_flow",
                title="Invalid leg",
                humanQuestion="Invalid native leg?",
                humanSummary="Product endpoints are not valid flow-leg operators.",
                branchId="branch-invalid",
                entityNodes=[
                    EntityNode(nodeId="product:ore", label="Ore", entityType="PRODUCT", confidence=0.4, supportType="hypothesis"),
                    EntityNode(nodeId="facility:plant", label="Plant", entityType="FACILITY", confidence=0.4, supportType="hypothesis"),
                ],
                flowLegs=[
                    FlowLeg(
                        legId="leg-invalid",
                        supplierNodeId="product:ore",
                        customerNodeId="facility:plant",
                        productNodeId="product:ore",
                        confidence=0.4,
                        supportType="hypothesis",
                    )
                ],
                completeness="hypothesis",
                qualityScore=0.1,
            )

    def test_host_local_process_gap_candidates_detect_missing_hosted_assembly(self) -> None:
        graph_state = graph_state_from_raw(
            {
                "entityNodes": [
                    {"nodeId": "facility:sqm", "label": "SQM Salar de Atacama", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "facility:harjavalta", "label": "BASF Harjavalta pCAM Plant", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "facility:schwarzheide", "label": "BASF Schwarzheide", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "organization:catl", "label": "CATL", "entityType": "ORGANIZATION", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:lithium-carbonate", "label": "Lithium Carbonate", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:pcam", "label": "pCAM", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:cam", "label": "Cathode Active Materials", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"}
                ],
                "flowLegs": [
                    {"legId": "leg-1", "supplierNodeId": "facility:sqm", "customerNodeId": "facility:schwarzheide", "productNodeId": "product:lithium-carbonate", "confidence": 0.8, "supportType": "document"},
                    {"legId": "leg-2", "supplierNodeId": "facility:harjavalta", "customerNodeId": "facility:schwarzheide", "productNodeId": "product:pcam", "confidence": 0.8, "supportType": "document"},
                    {"legId": "leg-3", "supplierNodeId": "facility:schwarzheide", "customerNodeId": "organization:catl", "productNodeId": "product:cam", "confidence": 0.8, "supportType": "document"}
                ],
                "processSteps": [],
                "overlayEdges": []
            }
        )

        candidates = _host_local_process_gap_candidates(graph_state)

        self.assertTrue(candidates)
        first = candidates[0]
        self.assertEqual(first["host"], "BASF Schwarzheide")
        self.assertEqual(first["processKind"], "assembly")
        self.assertIn("Lithium Carbonate", first["inputProducts"])
        self.assertIn("pCAM", first["inputProducts"])
        self.assertEqual(first["outputProduct"], "Cathode Active Materials")

    def test_normalize_investigator_plan_injects_process_gap_branch(self) -> None:
        mission = InvestigationMission(
            missionId="mission-1",
            objective="Repair the missing structural gap in the chain.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
        )
        state = {
            "input_request": {
                "question": "Map the likely lithium supply chain from Chile to a German battery manufacturer.",
                "corpusDocuments": [],
            },
            STATE_CURRENT_MISSION: mission.model_dump(mode="json", by_alias=True),
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN],
            STATE_WORKING_GRAPH: {
                "entityNodes": [
                    {"nodeId": "facility:sqm", "label": "SQM Salar de Atacama", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "facility:harjavalta", "label": "BASF Harjavalta pCAM Plant", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "facility:schwarzheide", "label": "BASF Schwarzheide", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "organization:catl", "label": "CATL", "entityType": "ORGANIZATION", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:lithium-carbonate", "label": "Lithium Carbonate", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:pcam", "label": "pCAM", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                    {"nodeId": "product:cam", "label": "Cathode Active Materials", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"}
                ],
                "flowLegs": [
                    {"legId": "leg-1", "supplierNodeId": "facility:sqm", "customerNodeId": "facility:schwarzheide", "productNodeId": "product:lithium-carbonate", "confidence": 0.8, "supportType": "document"},
                    {"legId": "leg-2", "supplierNodeId": "facility:harjavalta", "customerNodeId": "facility:schwarzheide", "productNodeId": "product:pcam", "confidence": 0.8, "supportType": "document"},
                    {"legId": "leg-3", "supplierNodeId": "facility:schwarzheide", "customerNodeId": "organization:catl", "productNodeId": "product:cam", "confidence": 0.8, "supportType": "document"}
                ],
                "processSteps": [],
                "overlayEdges": []
            },
        }
        weak_plan = InvestigatorPlan(
            assessment="Need more downstream detail.",
            rationale=["The downstream side looks incomplete."],
            branchPlans=[
                BranchPlan(
                    branchId="downstream_only",
                    hypothesis="Downstream consumers need more detail.",
                    parentHypothesisId="root-chain",
                    hypothesisRelation="dependent_followup",
                    spawnReason="Focus on the downstream side.",
                    independenceBasis="This branch refines the downstream side.",
                    pathKind="direct",
                    humanQuestion="Which OEM receives cathode materials?",
                    humanAction="Map downstream consumers.",
                    closureTarget="Identify downstream consumers.",
                    knownContext=["BASF Schwarzheide already ships cathode materials downstream."],
                    frontier=["Identify downstream consumers."],
                    assignedRoles=[ROLE_SUPPLY_CHAIN],
                    preferredTools=["web_search"],
                    targetEntities=["CATL"],
                    fragmentType="chain_fragment",
                    parallelGroup="downstream",
                    stopAfterNoImprovement=2,
                )
            ],
            blockingFailure=None,
        )

        normalized = _normalize_investigator_plan(
            state,
            mission,
            ROLE_SUPPLY_CHAIN,
            weak_plan,
            initial_pass=False,
            user_question="Map the likely lithium supply chain from Chile to a German battery manufacturer.",
            corpus_document_count=0,
        )

        self.assertGreaterEqual(len(normalized.branch_plans), 1)
        self.assertEqual(normalized.branch_plans[0].path_kind, "bridge")
        self.assertIn("BASF Schwarzheide", normalized.branch_plans[0].human_question)
        self.assertIn("process", normalized.branch_plans[0].closure_target.lower())

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

    def test_placeholder_detector_catches_geographic_processing_facility_rollups(self) -> None:
        placeholders = _placeholder_nodes_from_graph(
            [
                GraphObservation(
                    observationId="o-cn-proc",
                    subjectId="product:li2co3",
                    subject="Lithium Carbonate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:chinese-processing-facility",
                    object="Chinese Processing Facility",
                    objectType="FACILITY",
                    confidence=0.8,
                    supportType="document",
                    provenance=[],
                    supportNote="Roll-up stand-in.",
                    branchId="root",
                )
            ]
        )
        self.assertIn("Chinese Processing Facility", placeholders)

    def test_placeholder_detector_catches_geographic_battery_manufacturing_hub(self) -> None:
        placeholders = _placeholder_nodes_from_graph(
            [
                GraphObservation(
                    observationId="o-asian-hub",
                    subjectId="facility:chinese-processing-facility",
                    subject="Chinese Processing Facility",
                    subjectType="FACILITY",
                    relation="SUPPLIES_TO",
                    objectId="facility:asian-battery-hub",
                    object="Asian Battery Cell Manufacturing Hub",
                    objectType="FACILITY",
                    confidence=0.5,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="Aggregate hub.",
                    branchId="root",
                )
            ]
        )
        self.assertIn("Asian Battery Cell Manufacturing Hub", placeholders)

    def test_placeholder_detector_chilean_extraction_placeholder_suffix(self) -> None:
        placeholders = _placeholder_nodes_from_graph(
            [
                GraphObservation(
                    observationId="o-chile",
                    subjectId="deposit:atacama",
                    subject="Atacama brine pool",
                    subjectType="DEPOSIT",
                    relation="EXTRACTS_FROM",
                    objectId="facility:chilean-extraction",
                    object="Chilean extraction facility (placeholder)",
                    objectType="FACILITY",
                    confidence=0.35,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="",
                    branchId="root",
                )
            ]
        )
        self.assertIn("Chilean extraction facility (placeholder)", placeholders)

    def test_placeholder_detector_hypothesis_tbd_label(self) -> None:
        placeholders = _placeholder_nodes_from_graph(
            [
                GraphObservation(
                    observationId="o-tbd",
                    subjectId="product:spodumene",
                    subject="Spodumene concentrate",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    objectId="facility:tbd-plant",
                    object="TBD conversion plant",
                    objectType="FACILITY",
                    confidence=0.2,
                    supportType="hypothesis",
                    provenance=[],
                    supportNote="",
                    branchId="root",
                )
            ]
        )
        self.assertIn("TBD conversion plant", placeholders)

    def test_acceptance_blocked_for_dependent_direct_when_placeholders_present(self) -> None:
        plan = BranchPlan(
            branchId="dep-b1",
            hypothesis="Dependent refinement",
            pathKind="direct",
            humanQuestion="Verify hop?",
            humanAction="Check edge.",
            closureTarget="Concrete endpoints.",
            knownContext=[],
            frontier=[],
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            preferredTools=["reason"],
            targetEntities=["lithium"],
            fragmentType="chain_fragment",
            hypothesisRelation="dependent_followup",
            parentHypothesisId="root",
            spawnReason="Resolve slot",
            independenceBasis="",
        )
        branch_state = BranchState(
            branchId="dep-b1",
            hypothesis="Dependent refinement",
            pathKind="direct",
            humanQuestion="Verify hop?",
            humanSummary="Still broad sink.",
            closureTarget="Concrete endpoints.",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            status="completed",
            qualityScore=0.7,
            remainingFrontier=[],
            openQuestions=[],
            hypothesisRelation="dependent_followup",
            parentHypothesisId="root",
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
                    supportNote="",
                    branchId="dep-b1",
                ).model_dump(mode="json", by_alias=True)
            ]
        }
        reason = _acceptance_block_reason(plan, branch_state, local_state)
        self.assertIn("Broad placeholders remain", reason)

    def test_placeholder_resolution_hints_are_deterministic(self) -> None:
        obs = GraphObservation(
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
            supportNote="",
            branchId="root",
        )
        hints = _placeholder_resolution_hints([obs])
        self.assertTrue(hints)
        self.assertIn("replace_entity", hints[0])
        self.assertIn("facility:german-battery-manufacturing-plant", hints[0])

    def test_working_graph_from_logic_trace_last_write_wins(self) -> None:
        o1 = GraphObservation(
            observationId="o1",
            subjectId="facility:supplier-a",
            subject="Supplier A",
            subjectType="FACILITY",
            relation="SUPPLIES_TO",
            objectId="facility:x",
            object="Facility X",
            objectType="FACILITY",
            qualifiers=["product:Product A"],
            confidence=0.2,
            supportType="hypothesis",
            provenance=[],
            supportNote="",
            branchId="root",
        )
        o2 = o1.model_copy(update={"confidence": 0.9})
        lt = [
            {"agentResult": {"graphDelta": [o1.model_dump(mode="json", by_alias=True)]}},
            {"agentResult": {"graphDelta": [o2.model_dump(mode="json", by_alias=True)]}},
        ]
        merged = _working_graph_from_logic_trace(lt)
        self.assertEqual(graph_counts(graph_state_from_raw(merged))["flowLegCount"], 1)
        projected = observation_projection(graph_state_from_raw(merged))
        self.assertEqual(len(projected), 1)
        self.assertEqual(projected[0].confidence, 0.9)

    def test_trace_reasoning_digest_includes_placeholder_digest_keys(self) -> None:
        obs = GraphObservation(
            observationId="o1",
            subjectId="product:a",
            subject="Product A",
            subjectType="PRODUCT",
            relation="SUPPLIES_TO",
            objectId="facility:german-battery-manufacturing-plant",
            object="German Battery Manufacturing Plant",
            objectType="FACILITY",
            confidence=0.3,
            supportType="hypothesis",
            provenance=[],
            supportNote="",
            branchId="root",
        )
        st = {
            "logic_trace": [
                {
                    "cycle": 2,
                    "decision": {},
                    "stateSeen": {
                        "graphObservationCount": 1,
                        "openGaps": [],
                        "progress": {},
                        "placeholderNodes": ["German Battery Manufacturing Plant"],
                        "placeholderResolutionHints": ["hint-one"],
                    },
                    "mission": {},
                    "agentResult": {"graphDelta": [obs.model_dump(mode="json", by_alias=True)], "gaps": [], "status": "success"},
                }
            ],
            "working_graph": [],
        }
        raw = _trace_reasoning_digest(st)
        digest = json.loads(raw)
        self.assertIn("placeholderFromTrace", digest)
        self.assertEqual(digest["placeholderFromTrace"]["placeholderNodes"], ["German Battery Manufacturing Plant"])
        self.assertIn("structurallyCompleteChainsDigest", digest)
        row0 = digest["reasoningTrace"][0]
        self.assertEqual(row0["stateSeen"]["placeholderNodes"], ["German Battery Manufacturing Plant"])

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

    def test_replace_entity_patch_mutates_working_graph_and_syncs_branch_fragments(self) -> None:
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

        mutated_graph = _working_graph_projection(state)
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

        self.assertIn("removalZoneSize", result)
        self.assertGreaterEqual(result.get("removalZoneSize", 0), 1)
        self.assertIn(STATE_GRAPH_MUTATION_LOG, state)
        self.assertTrue(state[STATE_GRAPH_MUTATION_LOG])

    def test_hypothesis_entity_registry_lineage_after_replace_entity(self) -> None:
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
                    supportNote="",
                    branchId="overview",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.3,
        )
        replacement_fragment = GraphFragment(
            fragmentId="sink-f1",
            fragmentType="chain_fragment",
            title="Concrete sink",
            humanQuestion="Replace sink?",
            humanSummary="Concrete sink.",
            branchId="sink",
            patchMode="replace_node",
            oldId="facility:german-battery-plant",
            patchRationale="Replace broad sink with concrete facility.",
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
                    supportNote="",
                    branchId="sink",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        state: dict = {
            "working_graph": [item.model_dump(mode="json", by_alias=True) for item in placeholder_fragment.observations],
            "graph_fragments": [placeholder_fragment.model_dump(mode="json", by_alias=True)],
            "branch_states": [],
        }
        _apply_graph_fragment_mutations(state, [replacement_fragment])
        reg = state.get(STATE_HYPOTHESIS_ENTITY_REGISTRY) or {}
        events = reg.get("lineageEvents") or []
        self.assertTrue(events)
        last = events[-1]
        self.assertEqual(last.get("patchMode"), "replace_entity")
        self.assertIn("facility:german-battery-plant", last.get("fromIds") or [])
        self.assertIn("facility:acc-kaiserslautern", last.get("toIds") or [])
        id_map = ((reg.get("byBranch") or {}).get("sink") or {}).get("idToCanonical") or {}
        self.assertEqual(id_map.get("facility:german-battery-plant"), id_map.get("facility:acc-kaiserslautern"))
        self.assertNotEqual(id_map.get("product:nickel-sulfate"), id_map.get("facility:acc-kaiserslautern"))

    def test_detect_slot_endpoint_conflict(self) -> None:
        g = [
            GraphObservation(
                observationId="a",
                subject="X",
                subjectType="PRODUCT",
                relation="SUPPLIES_TO",
                object="Plant A",
                objectType="FACILITY",
                confidence=0.5,
                supportType="hypothesis",
                slotId="sink:de:battery_consumer",
                branchId="b1",
            ),
            GraphObservation(
                observationId="b",
                subject="X",
                subjectType="PRODUCT",
                relation="SUPPLIES_TO",
                object="Plant B",
                objectType="FACILITY",
                confidence=0.5,
                supportType="hypothesis",
                slotId="sink:de:battery_consumer",
                branchId="b1",
            ),
        ]
        conflicts = _detect_slot_endpoint_conflicts(g)
        self.assertEqual(len(conflicts), 1)

    def test_apply_graph_fragment_records_slot_contract_gap(self) -> None:
        frag = GraphFragment(
            fragmentId="slot-frag",
            fragmentType="chain_fragment",
            title="Dup slot",
            humanQuestion="?",
            humanSummary="?",
            branchId="b-slot",
            patchMode="append",
            patchRationale="",
            observations=[
                GraphObservation(
                    observationId="a",
                    subject="X",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="Plant A",
                    objectType="FACILITY",
                    confidence=0.5,
                    supportType="hypothesis",
                    slotId="sink:de:test",
                    branchId="b-slot",
                ),
                GraphObservation(
                    observationId="b",
                    subject="X",
                    subjectType="PRODUCT",
                    relation="SUPPLIES_TO",
                    object="Plant B",
                    objectType="FACILITY",
                    confidence=0.5,
                    supportType="hypothesis",
                    slotId="sink:de:test",
                    branchId="b-slot",
                ),
            ],
            completeness="partial",
            qualityScore=0.5,
        )
        state: dict = {
            "working_graph": [],
            "graph_fragments": [],
            "open_gaps": [],
            "supervisor_cycle": 0,
        }
        _apply_graph_fragment_mutations(state, [frag])
        gaps_raw = state.get(STATE_OPEN_GAPS) or []
        texts: list[str] = []
        for item in gaps_raw:
            if isinstance(item, str):
                texts.append(item)
            elif isinstance(item, dict) and item.get("text"):
                texts.append(str(item["text"]))
        self.assertTrue(any("Slot contract conflict" in t for t in texts))

    def test_replace_entity_removes_residual_edges_matching_label_slug_not_only_explicit_id(self) -> None:
        """Residual-only duplicate of the same sink via slug-derived id must be pruned with the explicit oldId."""
        placeholder_fragment = GraphFragment(
            fragmentId="overview-f1",
            fragmentType="overview_chain",
            title="Broad sink",
            humanQuestion="What is the broad chain?",
            humanSummary="Placeholder chain.",
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
                    supportNote="",
                    branchId="overview",
                )
            ],
            completeness="hypothesis",
            qualityScore=0.3,
        )
        residual_slug_dup = GraphObservation(
            observationId="o-residual-dup",
            subjectId="product:nickel-sulfate",
            subject="Nickel sulfate",
            subjectType="PRODUCT",
            relation="DEPENDS_ON",
            objectId="",
            object="German Battery Plant",
            objectType="FACILITY",
            confidence=0.2,
            supportType="hypothesis",
            provenance=[],
            supportNote="Residual duplicate of same sink label without explicit objectId.",
            branchId="residual-only",
        )
        replacement_fragment = GraphFragment(
            fragmentId="sink-f1",
            fragmentType="chain_fragment",
            title="Concrete sink",
            humanQuestion="Replace sink?",
            humanSummary="Concrete sink.",
            branchId="sink",
            patchMode="replace_node",
            oldId="facility:german-battery-plant",
            patchRationale="Replace broad sink with concrete facility.",
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
                    supportNote="",
                    branchId="sink",
                )
            ],
            completeness="partial",
            qualityScore=0.8,
        )
        state = {
            "working_graph": [
                item.model_dump(mode="json", by_alias=True)
                for item in [*placeholder_fragment.observations, residual_slug_dup]
            ],
            "graph_fragments": [placeholder_fragment.model_dump(mode="json", by_alias=True)],
            "branch_states": [],
        }
        result = _apply_graph_fragment_mutations(state, [replacement_fragment])
        mutated = _working_graph_projection(state)
        self.assertFalse(any("German Battery Plant" == obs.object for obs in mutated))
        self.assertTrue(any(obs.object_id == "facility:acc-kaiserslautern" for obs in mutated))
        self.assertIn("orphanFlowHints", result)

    def test_replace_entity_patch_is_rejected_when_it_collapses_route_depth(self) -> None:
        overview_fragment = GraphFragment(
            fragmentId="overview-route",
            fragmentType="overview_chain",
            title="Long route",
            humanQuestion="What is the long route?",
            humanSummary="A business-complete route exists through a placeholder midstream plant.",
            branchId="overview",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subjectId="facility:mine",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="EXTRACTS_FROM",
                    objectId="deposit:ore-body",
                    object="Ore Body",
                    objectType="DEPOSIT",
                    confidence=0.9,
                    supportType="document",
                    branchId="overview",
                ),
                GraphObservation(
                    observationId="o2",
                    subjectId="facility:mine",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="PRODUCES",
                    objectId="product:ore",
                    object="Ore",
                    objectType="PRODUCT",
                    confidence=0.9,
                    supportType="document",
                    branchId="overview",
                ),
                GraphObservation(
                    observationId="o3",
                    subjectId="facility:mine",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="SUPPLIES_TO",
                    objectId="facility:placeholder-converter",
                    object="Placeholder Converter",
                    objectType="FACILITY",
                    qualifiers=["product:Ore"],
                    confidence=0.7,
                    supportType="mixed",
                    branchId="overview",
                ),
                GraphObservation(
                    observationId="o4",
                    subjectId="facility:placeholder-converter",
                    subject="Placeholder Converter",
                    subjectType="FACILITY",
                    relation="PRODUCES",
                    objectId="product:refined-salt",
                    object="Refined Salt",
                    objectType="PRODUCT",
                    confidence=0.6,
                    supportType="model_knowledge",
                    branchId="overview",
                ),
                GraphObservation(
                    observationId="o5",
                    subjectId="product:ore",
                    subject="Ore",
                    subjectType="PRODUCT",
                    relation="TRANSFORMS_INTO",
                    objectId="product:refined-salt",
                    object="Refined Salt",
                    objectType="PRODUCT",
                    qualifiers=["host:Placeholder Converter"],
                    confidence=0.6,
                    supportType="model_knowledge",
                    branchId="overview",
                ),
                GraphObservation(
                    observationId="o6",
                    subjectId="facility:placeholder-converter",
                    subject="Placeholder Converter",
                    subjectType="FACILITY",
                    relation="SUPPLIES_TO",
                    objectId="facility:cell-plant",
                    object="Cell Plant",
                    objectType="FACILITY",
                    qualifiers=["product:Refined Salt"],
                    confidence=0.7,
                    supportType="mixed",
                    branchId="overview",
                ),
            ],
            completeness="partial",
            qualityScore=0.7,
        )
        replacement_fragment = GraphFragment(
            fragmentId="replace-midstream",
            fragmentType="chain_fragment",
            title="Concrete but incomplete converter",
            humanQuestion="Which concrete converter replaces the placeholder?",
            humanSummary="A concrete converter name is known, but downstream continuity is still missing.",
            branchId="replace-midstream",
            patchMode="replace_entity",
            oldId="facility:placeholder-converter",
            patchRationale="Replace the placeholder converter with a named facility.",
            observations=[
                GraphObservation(
                    observationId="r1",
                    subjectId="facility:mine",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="SUPPLIES_TO",
                    objectId="facility:named-converter",
                    object="Named Converter",
                    objectType="FACILITY",
                    qualifiers=["product:Ore"],
                    confidence=0.8,
                    supportType="document",
                    branchId="replace-midstream",
                )
            ],
            completeness="partial",
            qualityScore=0.6,
        )
        state = {
            "working_graph": [item.model_dump(mode="json", by_alias=True) for item in overview_fragment.observations],
            "graph_fragments": [overview_fragment.model_dump(mode="json", by_alias=True)],
            "open_gaps": [],
            "supervisor_cycle": 2,
        }

        result = _apply_graph_fragment_mutations(state, [replacement_fragment])
        mutated = _working_graph_projection(state)
        texts = []
        for raw in state.get(STATE_OPEN_GAPS) or []:
            if isinstance(raw, str):
                texts.append(raw)
            elif isinstance(raw, dict) and raw.get("text"):
                texts.append(str(raw["text"]))
        last_mutation = (state.get(STATE_GRAPH_MUTATION_LOG) or [])[-1]

        self.assertFalse(any(obs.object_id == "facility:named-converter" for obs in mutated))
        self.assertTrue(any(obs.object_id == "facility:placeholder-converter" for obs in mutated))
        self.assertTrue(any("Replacement patch replace-midstream was rejected" in text for text in texts))
        self.assertTrue(any(item.get("rejectedReason") for item in last_mutation.get("patchSummaries", [])))

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
        mutated_graph = _working_graph_projection(state)
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

        mutated_graph = _working_graph_projection(state)
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
            entityNodes=[
                EntityNode(nodeId="organization:nickel-distributor", label="Nickel Distributor", entityType="ORGANIZATION", confidence=0.6, supportType="mixed"),
                EntityNode(nodeId="facility:acc-kaiserslautern", label="ACC Kaiserslautern Facility", entityType="FACILITY", confidence=0.9, supportType="document"),
                EntityNode(nodeId="product:nickel-sulfate", label="Nickel Sulfate", entityType="PRODUCT", confidence=0.6, supportType="mixed"),
            ],
            flowLegs=[
                FlowLeg(
                    legId="flow-1",
                    supplierNodeId="organization:nickel-distributor",
                    customerNodeId="facility:acc-kaiserslautern",
                    productNodeId="product:nickel-sulfate",
                    confidence=0.6,
                    supportType="mixed",
                    provenance=["doc-2"],
                    supportNote="Supply leg.",
                    branchId="acc-branch",
                )
            ],
            overlayEdges=[
                GraphObservation(
                    observationId="loc-1",
                    subjectId="facility:acc-kaiserslautern",
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
                )
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

        self.assertEqual(candidates[0].start_node, "Nickel Distributor")
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

        root_candidate = next(item for item in candidates if item.path_id == "root")
        self.assertEqual(root_candidate.supporting_branch_ids, ["root", "sink"])
        self.assertEqual(root_candidate.end_node, "BASF Schwarzheide")

    def test_reachability_bfs_seeds_midstream_facilities_when_extract_sources_exist(self) -> None:
        """Enterprise-anchored chains must be discoverable even if they do not extend from the same extract site."""
        report = _graph_reachability_report(
            {
                "working_graph": {
                    "entityNodes": [
                        {"nodeId": "deposit:remote-dep", "label": "RemoteDep", "entityType": "DEPOSIT", "confidence": 0.9, "supportType": "document"},
                        {"nodeId": "facility:remote-mine", "label": "RemoteMine", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                        {"nodeId": "facility:mid-plant", "label": "MidPlant", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                        {"nodeId": "facility:vw", "label": "VW", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                        {"nodeId": "product:remote-ore", "label": "RemoteOre", "entityType": "PRODUCT", "confidence": 0.9, "supportType": "document"},
                        {"nodeId": "product:li-salt", "label": "LiSalt", "entityType": "PRODUCT", "confidence": 0.9, "supportType": "document"},
                    ],
                    "flowLegs": [
                        {
                            "legId": "leg-1",
                            "supplierNodeId": "facility:mid-plant",
                            "customerNodeId": "facility:vw",
                            "productNodeId": "product:li-salt",
                            "confidence": 0.9,
                            "supportType": "document",
                        }
                    ],
                    "processSteps": [
                        {
                            "stepId": "step-1",
                            "hostNodeId": "facility:remote-mine",
                            "processKind": "extraction",
                            "inputNodeIds": ["deposit:remote-dep"],
                            "outputNodeIds": ["product:remote-ore"],
                            "confidence": 0.9,
                            "supportType": "document",
                        },
                        {
                            "stepId": "step-2",
                            "hostNodeId": "facility:mid-plant",
                            "processKind": "transformation",
                            "inputNodeIds": [],
                            "outputNodeIds": ["product:li-salt"],
                            "confidence": 0.9,
                            "supportType": "document",
                        }
                    ],
                    "overlayEdges": [],
                }
            }
        )
        self.assertEqual(len(report["groundedCompleteChains"]), 0)
        self.assertTrue(report["partialChains"])
        self.assertEqual(report["partialChains"][0]["source"], "MidPlant")
        self.assertEqual(report["partialChains"][0]["sink"], "VW")
        self.assertIn("business-complete", report["partialChains"][0]["breakReason"])

    def test_reachability_treats_terminal_supply_sink_as_structurally_complete_chain(self) -> None:
        report = _graph_reachability_report(
            {
                "working_graph": {
                    "entityNodes": [
                        {"nodeId": "deposit:mine-deposit", "label": "Deposit", "entityType": "DEPOSIT", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "facility:mine", "label": "Mine", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "facility:refinery", "label": "Refinery", "entityType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                        {"nodeId": "facility:basf", "label": "BASF Schwarzheide", "entityType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                        {"nodeId": "product:nickel-ore", "label": "Nickel Ore", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "product:nickel-sulfate", "label": "Nickel Sulfate", "entityType": "PRODUCT", "confidence": 0.4, "supportType": "model_knowledge"},
                    ],
                    "flowLegs": [
                        {
                            "legId": "leg-ore",
                            "supplierNodeId": "facility:mine",
                            "customerNodeId": "facility:refinery",
                            "productNodeId": "product:nickel-ore",
                            "confidence": 0.8,
                            "supportType": "document",
                        },
                        {
                            "legId": "leg-1",
                            "supplierNodeId": "facility:refinery",
                            "customerNodeId": "facility:basf",
                            "productNodeId": "product:nickel-sulfate",
                            "confidence": 0.4,
                            "supportType": "model_knowledge",
                        }
                    ],
                    "processSteps": [
                        {
                            "stepId": "step-1",
                            "hostNodeId": "facility:mine",
                            "processKind": "extraction",
                            "inputNodeIds": ["deposit:mine-deposit"],
                            "outputNodeIds": ["product:nickel-ore"],
                            "confidence": 0.8,
                            "supportType": "document",
                        },
                        {
                            "stepId": "step-2",
                            "hostNodeId": "facility:refinery",
                            "processKind": "transformation",
                            "inputNodeIds": ["product:nickel-ore"],
                            "outputNodeIds": ["product:nickel-sulfate"],
                            "confidence": 0.4,
                            "supportType": "model_knowledge",
                        }
                    ],
                    "overlayEdges": [],
                }
            }
        )

        self.assertEqual(len(report["partialChains"]), 0)
        self.assertEqual(len(report["groundedCompleteChains"]), 0)
        self.assertEqual(len(report["structurallyCompleteChains"]), 1)
        self.assertEqual(report["structurallyCompleteChains"][0]["sink"], "BASF Schwarzheide")

    def test_reachability_hypothesis_terminal_sink_is_structurally_complete_not_partial(self) -> None:
        report = _graph_reachability_report(
            {
                "working_graph": {
                    "entityNodes": [
                        {"nodeId": "deposit:mine-deposit", "label": "Deposit", "entityType": "DEPOSIT", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "facility:mine", "label": "Mine", "entityType": "FACILITY", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "facility:refinery", "label": "Refinery", "entityType": "FACILITY", "confidence": 0.2, "supportType": "hypothesis"},
                        {"nodeId": "facility:basf", "label": "BASF Schwarzheide", "entityType": "FACILITY", "confidence": 0.4, "supportType": "model_knowledge"},
                        {"nodeId": "product:nickel-ore", "label": "Nickel Ore", "entityType": "PRODUCT", "confidence": 0.8, "supportType": "document"},
                        {"nodeId": "product:nickel-sulfate", "label": "Nickel Sulfate", "entityType": "PRODUCT", "confidence": 0.2, "supportType": "hypothesis"},
                    ],
                    "flowLegs": [
                        {
                            "legId": "leg-ore",
                            "supplierNodeId": "facility:mine",
                            "customerNodeId": "facility:refinery",
                            "productNodeId": "product:nickel-ore",
                            "confidence": 0.8,
                            "supportType": "document",
                        },
                        {
                            "legId": "leg-1",
                            "supplierNodeId": "facility:refinery",
                            "customerNodeId": "facility:basf",
                            "productNodeId": "product:nickel-sulfate",
                            "confidence": 0.4,
                            "supportType": "model_knowledge",
                        }
                    ],
                    "processSteps": [
                        {
                            "stepId": "step-1",
                            "hostNodeId": "facility:mine",
                            "processKind": "extraction",
                            "inputNodeIds": ["deposit:mine-deposit"],
                            "outputNodeIds": ["product:nickel-ore"],
                            "confidence": 0.8,
                            "supportType": "document",
                        },
                        {
                            "stepId": "step-2",
                            "hostNodeId": "facility:refinery",
                            "processKind": "transformation",
                            "inputNodeIds": ["product:nickel-ore"],
                            "outputNodeIds": ["product:nickel-sulfate"],
                            "confidence": 0.2,
                            "supportType": "hypothesis",
                        }
                    ],
                    "overlayEdges": [],
                }
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
        self.assertIn("No document-grounded business-complete chain", report["healthSummary"])

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

    def test_trace_reasoning_digest_counts_graph_delta_camel_or_snake(self) -> None:
        from agentloom.runner import _trace_reasoning_digest

        base_entry = {
            "cycle": 2,
            "stateSeen": {
                "graphObservationCount": 8,
                "openGaps": ["bridge Chile conversion"],
                "progress": {"stagnationCount": 1},
                "reachability": {"completeChainCount": 0, "healthSummary": "no document-grounded chain"},
            },
            "decision": {
                "cycle": 2,
                "nextAgent": "supply_chain_investigator_agent",
                "assessment": "Still no grounded backbone.",
                "rationale": ["completeChainCount remains 0; deepen flow."],
            },
            "mission": {
                "missionId": "m1",
                "objective": "Add one SUPPLIES_TO hop on the weakest partial chain.",
                "assignedRoles": ["supply_chain"],
                "groundingMode": "mixed",
            },
            "nextStepLogic": "Delegated supply_chain and merged graph deltas.",
        }
        camel = {**base_entry, "agentResult": {"status": "completed", "graphDelta": [{"x": 1}], "gaps": ["g1"]}}
        snake = {**base_entry, "agentResult": {"status": "completed", "graph_delta": [{"x": 1}, {"x": 2}], "gaps": []}}
        for entry, expected_delta in ((camel, 1), (snake, 2)):
            payload = json.loads(
                _trace_reasoning_digest({STATE_LOGIC_TRACE: [entry], STATE_FINAL: {}})
            )
            self.assertEqual(payload["logicTraceEntries"], 1)
            row = payload["reasoningTrace"][0]
            self.assertEqual(row["decisionCycle"], 2)
            self.assertEqual(row["stateSeen"]["reachability"]["completeChainCount"], 0)
            self.assertEqual(row["agentResultBrief"]["graphDeltaCount"], expected_delta)

    def test_compose_gate_rejects_stale_critic_epoch(self) -> None:
        st = {
            STATE_LAST_CRITIC_VERDICT: {
                "investigationEpoch": 1,
                "compositionRecommendation": "recommend",
                "compositionConditions": [],
            },
            STATE_INVESTIGATION_EPOCH: 2,
        }
        ok, msg = _orchestration_policy_allow_compose(st)
        self.assertFalse(ok)
        self.assertIn("stale", msg)

    def test_compose_gate_accepts_matching_epoch(self) -> None:
        st = {
            STATE_LAST_CRITIC_VERDICT: {
                "investigationEpoch": 2,
                "compositionRecommendation": "recommend",
                "compositionConditions": [],
            },
            STATE_INVESTIGATION_EPOCH: 2,
        }
        ok, msg = _orchestration_policy_allow_compose(st)
        self.assertTrue(ok)
        self.assertEqual(msg, "ok")

    def test_compose_gate_requires_nonempty_conditions_for_conditional(self) -> None:
        st = {
            STATE_LAST_CRITIC_VERDICT: {
                "investigationEpoch": 1,
                "compositionRecommendation": "conditional",
                "compositionConditions": [],
            },
            STATE_INVESTIGATION_EPOCH: 1,
        }
        ok, msg = _orchestration_policy_allow_compose(st)
        self.assertFalse(ok)
        self.assertIn("conditional_requires", msg)

    def test_compose_gate_accepts_conditional_with_conditions(self) -> None:
        st = {
            STATE_LAST_CRITIC_VERDICT: {
                "investigationEpoch": 1,
                "compositionRecommendation": "conditional",
                "compositionConditions": ["Answer must label all hypothesis edges explicitly."],
            },
            STATE_INVESTIGATION_EPOCH: 1,
        }
        ok, msg = _orchestration_policy_allow_compose(st)
        self.assertTrue(ok)
        self.assertEqual(msg, "ok")

    def test_delegate_constraints_redirects_governance_without_grounded_chain(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="gov overlay",
            assignedRoles=[ROLE_GOVERNANCE],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=3,
            assessment="x",
            rationale=["y"],
            nextAgent="governance_and_control_investigator_agent",
            mission=mission,
        )
        st = {
            STATE_GLOBAL_STAGNATION: 0,
            STATE_LOGIC_TRACE: [],
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            STATE_WORKING_GRAPH: [],
        }
        out, pc = _delegate_constraints(st, dec, cycle=3, user_question="Who owns the mine?")
        self.assertIsNotNone(pc)
        self.assertEqual(out.next_agent, "supply_chain_investigator_agent")
        self.assertIsNotNone(out.mission)
        self.assertEqual(out.mission.assigned_roles, [ROLE_SUPPLY_CHAIN])

    def test_merge_observation_prefers_document_over_model_knowledge(self) -> None:
        a = GraphObservation(
            observationId="1",
            subject="A",
            relation="R",
            object="B",
            confidence=0.8,
            supportType="model_knowledge",
        )
        b = GraphObservation(
            observationId="2",
            subject="A",
            relation="R",
            object="B",
            confidence=0.8,
            supportType="document",
            provenance=["https://example.com"],
        )
        m, gap = _merge_observation(a, b)
        self.assertIsNone(gap)
        self.assertEqual(m.support_type, "document")

    def test_critic_agent_output_requires_conditions_for_conditional(self) -> None:
        with self.assertRaises(ValidationError):
            CriticAgentOutput(
                agentName="critic_agent",
                missionId="m",
                status="success",
                summary="s",
                compositionRecommendation="conditional",
                compositionConditions=[],
            )

    def test_merge_observation_epsilon_prefers_stronger_support_type(self) -> None:
        """Within MERGE_CONFIDENCE_EPSILON, document should beat mixed even if mixed has slightly higher confidence."""
        doc = GraphObservation(
            observationId="1",
            subject="A",
            relation="PRODUCES",
            object="B",
            confidence=0.905,
            supportType="document",
        )
        mixed = GraphObservation(
            observationId="2",
            subject="A",
            relation="PRODUCES",
            object="B",
            confidence=0.90,
            supportType="mixed",
        )
        merged, gap = _merge_observation(mixed, doc)
        self.assertEqual(merged.support_type, "document")
        self.assertIsNone(gap)

    def test_unified_progress_report_material_progress_last_step(self) -> None:
        st = {
            STATE_PROGRESS_HISTORY: [
                {
                    "agentName": "supply_chain_investigator_agent",
                    "missionId": "m-1",
                    "materialProgress": True,
                    "stagnationCount": 0,
                    "openGapCount": 2,
                }
            ],
            STATE_GLOBAL_STAGNATION: 0,
            STATE_BRANCH_STATES: [],
            STATE_INVESTIGATION_EPOCH: 0,
            STATE_OPEN_GAP_STREAK: 0,
        }
        rep = _unified_progress_report(st)
        self.assertIn("materialProgressLastStep", rep)
        self.assertIn("supply_chain_investigator_agent", rep["materialProgressLastStep"])

    def test_normalize_preserves_graph_delta_without_runtime_cap(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="o",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
        )
        obs = [
            GraphObservation(
                observationId=f"id-{i}",
                subject="S",
                relation="PRODUCES",
                object=f"O{i}",
                confidence=0.7,
                supportType="model_knowledge",
            )
            for i in range(8)
        ]
        raw = AgentResult(
            agentName="supply_chain_investigator_agent",
            missionId="m",
            status="success",
            summary="s",
            graphDelta=obs,
        )
        normalized = _normalize_agent_result(raw, mission, [ROLE_SUPPLY_CHAIN])
        self.assertFalse(normalized.truncation)
        self.assertEqual(len(normalized.graph_delta), 8)

    def test_delegate_constraints_stagnation_escalates_to_critic(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="dig",
            assignedRoles=[ROLE_SUPPLY_CHAIN],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=5,
            assessment="x",
            rationale=["y"],
            nextAgent="supply_chain_investigator_agent",
            mission=mission,
        )
        st = {
            STATE_GLOBAL_STAGNATION: agentloom_limits.STAGNATION_FORCE_CRITIC_CYCLES,
            STATE_LOGIC_TRACE: [],
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN],
            STATE_WORKING_GRAPH: {
                "entityNodes": [
                    {"nodeId": "deposit:mine-deposit", "label": "Deposit", "entityType": "DEPOSIT", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "facility:mine", "label": "Mine", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "facility:refinery", "label": "Refinery", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "facility:cell-plant", "label": "Cell Plant", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "product:ore", "label": "Ore", "entityType": "PRODUCT", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "product:salt", "label": "Salt", "entityType": "PRODUCT", "confidence": 0.9, "supportType": "document"},
                ],
                "flowLegs": [
                    {
                        "legId": "leg-ore",
                        "supplierNodeId": "facility:mine",
                        "customerNodeId": "facility:refinery",
                        "productNodeId": "product:ore",
                        "confidence": 0.9,
                        "supportType": "document",
                    },
                    {
                        "legId": "leg-salt",
                        "supplierNodeId": "facility:refinery",
                        "customerNodeId": "facility:cell-plant",
                        "productNodeId": "product:salt",
                        "confidence": 0.9,
                        "supportType": "document",
                    },
                ],
                "processSteps": [
                    {
                        "stepId": "step-1",
                        "hostNodeId": "facility:mine",
                        "processKind": "extraction",
                        "inputNodeIds": ["deposit:mine-deposit"],
                        "outputNodeIds": ["product:ore"],
                        "confidence": 0.9,
                        "supportType": "document",
                    },
                    {
                        "stepId": "step-2",
                        "hostNodeId": "facility:refinery",
                        "processKind": "transformation",
                        "inputNodeIds": ["product:ore"],
                        "outputNodeIds": ["product:salt"],
                        "confidence": 0.9,
                        "supportType": "document",
                    },
                ],
                "overlayEdges": [],
            },
            STATE_OPEN_GAP_STREAK: 0,
            STATE_LAST_GAP_FINGERPRINT: "",
        }
        out, pc = _delegate_constraints(st, dec, cycle=5, user_question="q?")
        self.assertIsNotNone(pc)
        self.assertEqual(out.next_agent, "critic_agent")

    def test_delegate_constraints_defers_critic_without_business_complete_chain(self) -> None:
        mission = InvestigationMission(
            missionId="m",
            objective="decide whether to stop",
            assignedRoles=[ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=4,
            assessment="Need an honest exit assessment.",
            rationale=["Progress stalled."],
            nextAgent="critic_agent",
            mission=mission,
        )
        st = {
            STATE_GLOBAL_STAGNATION: agentloom_limits.STAGNATION_FORCE_CRITIC_CYCLES,
            STATE_LOGIC_TRACE: [],
            STATE_SELECTED_ROLES: [ROLE_SUPPLY_CHAIN, ROLE_GOVERNANCE],
            STATE_WORKING_GRAPH: {
                "entityNodes": [
                    {"nodeId": "deposit:mine-deposit", "label": "Deposit", "entityType": "DEPOSIT", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "facility:mine", "label": "Mine", "entityType": "FACILITY", "confidence": 0.9, "supportType": "document"},
                    {"nodeId": "facility:placeholder-sink", "label": "Placeholder Sink", "entityType": "FACILITY", "confidence": 0.3, "supportType": "hypothesis"},
                    {"nodeId": "product:ore", "label": "Ore", "entityType": "PRODUCT", "confidence": 0.9, "supportType": "document"},
                ],
                "flowLegs": [
                    {
                        "legId": "leg-1",
                        "supplierNodeId": "facility:mine",
                        "customerNodeId": "facility:placeholder-sink",
                        "productNodeId": "product:ore",
                        "confidence": 0.4,
                        "supportType": "hypothesis",
                    }
                ],
                "processSteps": [
                    {
                        "stepId": "step-1",
                        "hostNodeId": "facility:mine",
                        "processKind": "extraction",
                        "inputNodeIds": ["deposit:mine-deposit"],
                        "outputNodeIds": ["product:ore"],
                        "confidence": 0.9,
                        "supportType": "document",
                    }
                ],
                "overlayEdges": [],
            },
            STATE_OPEN_GAP_STREAK: agentloom_limits.REPEATED_GAP_THRESHOLD,
            STATE_LAST_GAP_FINGERPRINT: "gap-1",
        }

        out, pc = _delegate_constraints(st, dec, cycle=4, user_question="q?")

        self.assertIsNotNone(pc)
        self.assertEqual(out.next_agent, "supply_chain_investigator_agent")
        self.assertEqual(out.mission.assigned_roles, [ROLE_SUPPLY_CHAIN])

    def test_golden_fixture_unified_progress_keys(self) -> None:
        path = os.path.join(os.path.dirname(__file__), "fixtures", "unified_progress_golden.json")
        with open(path, encoding="utf-8") as f:
            fixture = json.load(f)
        rep = _unified_progress_report(fixture["state"])
        self.assertEqual(rep["materialProgressLastStep"], fixture["expected"]["materialProgressLastStep"])


if __name__ == "__main__":
    unittest.main()
