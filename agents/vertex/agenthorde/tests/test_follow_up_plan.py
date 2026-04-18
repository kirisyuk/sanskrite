"""Tests for canonicalizer followUpPlan builder."""

from __future__ import annotations

from agenthorde_common.canonicalizer.follow_up_plan import build_follow_up_plan
from agenthorde_common.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalFragment,
    Candidates,
    MergeCandidate,
    RegistryCoverageIssue,
    UnknownProductCandidate,
    UnknownRelationCandidate,
    UnknownTransformCandidate,
    ValidationIssue,
    ValidationReport,
)


def _frag(fid: str, path: str, cls: str, surfaces: list[str]) -> CanonicalFragment:
    return CanonicalFragment(
        id=fid,
        canonical_path=path,
        fragment_class=cls,
        surface_forms=surfaces,
        confidence=0.6,
    )


def _edge(sid: str, tid: str, relation: str = "supplies") -> CanonicalEdge:
    return CanonicalEdge(
        id=f"e-{sid}-{tid}",
        source_fragment_id=sid,
        target_fragment_id=tid,
        relation=relation,
        layer="structural",
        layer_section="flow_topology",
    )


def test_follow_up_plan_empty_when_nothing_to_report() -> None:
    plan = build_follow_up_plan([], [], Candidates(), ValidationReport())
    assert plan["aggregateMentions"] == []
    assert plan["deferredResearch"] == []
    assert plan["knownAnchors"] == []
    assert plan["counters"]["orphanCount"] == 0
    assert plan["counters"]["registryCoverageGaps"] == 0


def test_follow_up_plan_surfaces_orphans_as_aggregate_mentions() -> None:
    f1 = _frag("f1", "/org/tsmc", "org", ["TSMC"])
    f2 = _frag("f2", "/geo/egypt", "geo", ["Egypt"])  # orphan
    report = ValidationReport(orphan_nodes=["f2"])
    plan = build_follow_up_plan([f1, f2], [], Candidates(), report)
    assert len(plan["aggregateMentions"]) == 1
    mention = plan["aggregateMentions"][0]
    assert mention["fragmentId"] == "f2"
    assert mention["canonicalPath"] == "/geo/egypt"
    assert mention["status"] == "aggregate_mention"
    assert plan["counters"]["orphanCount"] == 1


def test_follow_up_plan_ranks_known_anchors_by_incidence() -> None:
    f1 = _frag("f1", "/org/tsmc", "org", ["TSMC"])
    f2 = _frag("f2", "/org/asml", "org", ["ASML"])
    f3 = _frag("f3", "/product/euv_scanner", "product", ["EUV scanner"])
    edges = [_edge("f1", "f3", "supplies"), _edge("f2", "f3", "supplies")]
    plan = build_follow_up_plan([f1, f2, f3], edges, Candidates(), ValidationReport())
    anchors = plan["knownAnchors"]
    assert anchors[0]["canonicalPath"] == "/product/euv_scanner"
    assert anchors[0]["incidentEdgeCount"] == 2
    assert {a["canonicalPath"] for a in anchors} == {
        "/product/euv_scanner",
        "/org/tsmc",
        "/org/asml",
    }


def test_follow_up_plan_classifies_deferred_items_by_family() -> None:
    candidates = Candidates(
        unknown_products=[
            UnknownProductCandidate(
                surface="plain jet fuel",
                from_site_fragment_id="f1",
                to_site_fragment_id=None,
                reason="unmapped_cargo",
            )
        ],
        unknown_relations=[
            UnknownRelationCandidate(
                relation="blockades_at",
                from_id="f1",
                to_id="f2",
                reason="missing_from_vocab",
            )
        ],
        unknown_transforms=[
            UnknownTransformCandidate(
                site_fragment_id="f1",
                from_product="/product/ingot",
                to_product="/product/wafer_300mm",
            )
        ],
        merge_candidates=[
            MergeCandidate(entity_ids=["e1", "e2"], surfaces=["Acme Ltd", "Acme Limited"], reason="fuzzy"),
        ],
    )
    report = ValidationReport(dictionary_warnings=["Incoterm 'CIF India' not recognized"])
    plan = build_follow_up_plan([], [], candidates, report)
    families = {item["family"] for item in plan["deferredResearch"]}
    assert families == {
        "product_dictionary",
        "relation_vocabulary",
        "transform_dictionary",
        "synonym_dictionary",
        "dictionary_warning",
    }
    synonym_row = next(r for r in plan["deferredResearch"] if r["family"] == "synonym_dictionary")
    assert synonym_row["suggestedAgent"] == "governance_control_analyzer"
    assert plan["counters"]["unknownProducts"] == 1
    assert plan["counters"]["unknownRelations"] == 1
    assert plan["counters"]["unknownTransforms"] == 1
    assert plan["counters"]["mergeCandidates"] == 1


def test_follow_up_plan_counts_unresolved_endpoints() -> None:
    report = ValidationReport(
        relation_errors=[
            ValidationIssue(code="unresolved_endpoint_surface", message="x"),
            ValidationIssue(code="unresolved_endpoint_surface", message="y"),
            ValidationIssue(code="orphan_node", message="z"),
        ]
    )
    plan = build_follow_up_plan([], [], Candidates(), report)
    assert plan["counters"]["unresolvedEndpoints"] == 2


def test_follow_up_plan_deferred_includes_registry_coverage() -> None:
    candidates = Candidates(
        registry_coverage_issues=[
            RegistryCoverageIssue(
                agent_registry_id="analyzer:e9",
                label="Ghost",
                reason="no_mention_from_phase1",
            )
        ]
    )
    plan = build_follow_up_plan([], [], candidates, ValidationReport())
    row = next(r for r in plan["deferredResearch"] if r["family"] == "entity_registry")
    assert row["agentRegistryId"] == "analyzer:e9"
    assert row["reason"] == "no_mention_from_phase1"
    assert plan["counters"]["registryCoverageGaps"] == 1
