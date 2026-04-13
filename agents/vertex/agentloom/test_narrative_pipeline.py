from __future__ import annotations

import unittest

from agentloom.composer import build_final_chain_summary, build_final_chain_summary_sync
from agentloom.contracts import (
    CandidatePath,
    GraphFragment,
    GraphObservation,
    HypothesisState,
    InternalBrief,
    LearnedFinding,
    StructuredNarrativeAnswer,
)
from agentloom.gap_clustering import cluster_gap_texts
from agentloom.internal_brief import build_internal_brief
from agentloom.narrative_coverage import (
    explicit_narrative_synthesis_failure,
    validate_narrative_coverage,
)
from agentloom.question_rubric import build_question_rubric
from agentloom.taxonomy import ROLE_SUPPLY_CHAIN


class NarrativePipelineTest(unittest.TestCase):
    def test_gap_clustering_buckets_diverse_gaps(self) -> None:
        gaps = [
            "Need nickel sulfate traceability to CATL",
            "Missing contract between Indonesian refiner and German sink",
            "Ownership of PowerCo JV unclear",
            "EU battery regulation due diligence gap",
            "Bridge from HPAL output to sulfate plant",
            "Basf CAM node not linked to flow chain",
            "German gigafactory consumption opaque",
            "Regulatory choke on carbon footprint data",
            "Indonesia upstream ore grade unknown",
            "Unrelated generic gap text xyz123",
        ]
        clusters = cluster_gap_texts(gaps, max_clusters=7)
        self.assertGreaterEqual(len(clusters), 3)
        self.assertLessEqual(len(clusters), 7)
        flat = [m for c in clusters for m in c.member_gap_texts]
        self.assertEqual(set(flat), set(gaps))

    def test_gap_embedding_clustering_runs(self) -> None:
        gaps = ["nickel sulfate contract", "battery regulation traceability", "ownership of jv unclear"]
        emb = cluster_gap_texts(gaps, prefer_embedding_clustering=True)
        self.assertTrue(emb)
        flat = [m for c in emb for m in c.member_gap_texts]
        self.assertEqual(set(flat), set(gaps))

    def test_internal_brief_from_minimal_graph(self) -> None:
        frag = GraphFragment(
            fragmentId="f1",
            fragmentType="chain_fragment",
            title="Flow",
            humanQuestion="q",
            humanSummary="s",
            branchId="b1",
            observations=[
                GraphObservation(
                    observationId="o1",
                    subject="Mine",
                    subjectType="FACILITY",
                    relation="PRODUCES",
                    object="Ore",
                    objectType="PRODUCT",
                    confidence=0.7,
                    supportType="hypothesis",
                    branchId="b1",
                )
            ],
        )
        hyp = HypothesisState(
            hypothesisId="h1",
            statement="Indirect sulfate route",
            pathKind="indirect",
            status="accepted",
            summary="Refiner ships nickel sulfate toward EU battery plants.",
            humanQuestion="q",
            closureTarget="t",
            activeRoles=[ROLE_SUPPLY_CHAIN],
            sourceBranchIds=["b1"],
            sourceFragmentIds=["f1"],
            broadPictureFragmentIds=[],
            evidenceStatus="reasoned",
            confidence=0.6,
        )
        path = CandidatePath(
            pathId="p1",
            pathKind="indirect",
            status="best_supported_indirect",
            title="Sulfate route",
            summary="Operational flow via refinery and sulfate product.",
            evidenceStatus="reasoned",
            confidence=0.55,
        )
        brief = build_internal_brief(
            question="How does nickel reach EU cells?",
            selected_roles=["supply_chain"],
            working_graph=frag.observations,
            graph_fragments=[frag],
            branches=[],
            hypotheses=[hyp],
            candidate_paths=[path],
            learned_findings=[
                LearnedFinding(
                    findingId="lf1",
                    hypothesisId="h1",
                    branchId="b1",
                    role=ROLE_SUPPLY_CHAIN,
                    questionAsked="q",
                    findingKind="operational_flow",
                    summary="Sulfate is the traded intermediate.",
                    evidenceStatus="reasoned",
                    confidence=0.8,
                    relatedEntities=["Sulfate"],
                    sourceDocuments=[],
                )
            ],
            open_gap_texts=["nickel sulfate contract visibility"],
            reachability_report={"healthSummary": "No grounded complete chain", "completeChainCount": 0, "brokenChains": [{"x": 1}]},
        )
        self.assertTrue(brief.supply_chain_narrative_bullets)
        self.assertTrue(brief.learned_findings_digest)
        self.assertTrue(brief.clustered_gaps)
        self.assertIn("brokenChainReports", brief.reachability_one_liner)

    def test_validate_narrative_coverage_pass_and_fail(self) -> None:
        rubric = build_question_rubric("alternative ownership path and regulatory choke", ["supply_chain"])
        good = StructuredNarrativeAnswer(
            chainStructure="The working graph outlines ore-to-sulfate stages with remaining bridge uncertainty.",
            alternativePaths="Alternative processing families are not separately grounded in the brief.",
            ownershipLeverage="Insufficient structured data in the current graph for this axis.",
            regulatoryChokePoints="Insufficient structured data in the current graph for this axis.",
            honestLimits="Some edges are hypothesis-level rather than document-grounded.",
            prioritizedOpenQuestions=["Contracts", "Traceability", "Sink linkage"],
            executiveSummary="The graph supports a partial chain narrative but leaves procurement and regulatory detail open.",
        )
        ok, failed = validate_narrative_coverage(good, rubric)
        self.assertTrue(ok)
        self.assertEqual(failed, [])

        bad = StructuredNarrativeAnswer(
            chainStructure="x",
            alternativePaths="y",
            ownershipLeverage="z",
            regulatoryChokePoints="w",
            honestLimits="",
            prioritizedOpenQuestions=[],
            executiveSummary="short",
        )
        ok2, failed2 = validate_narrative_coverage(bad, rubric)
        self.assertFalse(ok2)
        self.assertTrue(failed2)

    def test_explicit_failure_is_diagnostic_not_a_substitute_answer(self) -> None:
        rubric = build_question_rubric("Who owns the refinery?", ["governance_and_control", "policy_impact"])
        sn = explicit_narrative_synthesis_failure(
            reason_code="validation_failed",
            detail="Rubric slot supply_chain_structure was empty.",
            failed_rubric_ids=["governance_ownership"],
        )
        self.assertIn("Narrative synthesis failed", sn.executive_summary)
        self.assertIn("validation_failed", sn.executive_summary)
        ok, _ = validate_narrative_coverage(sn, rubric)
        self.assertTrue(ok)

    def test_concise_answer_prefixes_reachability_when_narrative_ok_and_no_complete_chains(self) -> None:
        internal = InternalBrief(
            questionOneLiner="How does material X reach the sink?",
            selectedRoles=["supply_chain"],
            reachabilityOneLiner="No grounded end-to-end flow in the working graph.; completeChains~0",
            completeChainCount=0,
            epistemicCaveats=["Some edges in the working graph are hypothesis-level and not document-grounded."],
        )
        structured = StructuredNarrativeAnswer(
            chainStructure="Partial stages exist between known anchors.",
            alternativePaths="Secondary route families are not separately grounded.",
            ownershipLeverage="Insufficient structured data in the current graph for this axis.",
            regulatoryChokePoints="Insufficient structured data in the current graph for this axis.",
            honestLimits="Several hops remain model-inferred rather than citation-backed.",
            prioritizedOpenQuestions=["Bridge the sulfate plant to the named sink facility."],
            executiveSummary="The narrative focuses on partial coverage and the next verification steps.",
        )
        summary = build_final_chain_summary(
            question="How does material X reach the sink?",
            selected_roles=["supply_chain"],
            working_graph=[],
            graph_fragments=[],
            branches=[],
            hypotheses=[],
            candidate_paths=[],
            learned_findings=[],
            logic_trace=[],
            runtime_logs=[],
            structured_narrative=structured,
            internal_brief=internal,
            narrative_synthesis_status="ok",
        )
        self.assertEqual(summary.narrative_synthesis_status, "ok")
        ca = summary.concise_answer
        head = ca[:500].lower()
        self.assertTrue(
            "no grounded end-to-end" in head
            or "completechains" in head
            or "hypothesis-level" in head,
            msg=f"expected epistemic lead in conciseAnswer head: {ca[:200]!r}",
        )
        self.assertIn("partial coverage", ca.lower())

    def test_build_final_chain_summary_sync_is_explicitly_deterministic(self) -> None:
        summary = build_final_chain_summary_sync(
            question="Q?",
            selected_roles=["supply_chain"],
            working_graph=[],
            graph_fragments=[],
            branches=[],
            hypotheses=[],
            candidate_paths=[],
            learned_findings=[],
            logic_trace=[],
            runtime_logs=[],
        )
        self.assertEqual(summary.narrative_synthesis_status, "deterministic_only")
        self.assertIsNone(summary.structured_narrative)
        self.assertEqual(
            summary.concise_answer,
            "No grounded end-to-end operational chain is confirmed in the working graph. Q?",
        )


if __name__ == "__main__":
    unittest.main()
