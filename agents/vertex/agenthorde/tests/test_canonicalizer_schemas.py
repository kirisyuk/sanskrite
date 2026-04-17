"""Roundtrip tests for canonicalizer schemas."""

from __future__ import annotations

from agenthorde.schemas.agents_io import AnalyzerOutput, ProposedRelation
from agenthorde.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalFragment,
    CanonicalGraph,
    CanonicalizerInput,
    CanonicalizerProvenance,
    CanonicalizerResult,
    Candidates,
    Provenance,
    SiteTransform,
    ValidationReport,
)
from agenthorde.canonicalizer.version import CANONICALIZER_VERSION


def test_canonicalizer_input_roundtrip() -> None:
    raw = {
        "pack": {"reasoningGraph": {"nodes": [], "edges": []}},
        "analyzerOutput": {"chainSummary": "x", "proposedNodes": [], "openQuestions": []},
        "governanceOutput": {"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        "toolLedger": [],
        "finalAnswer": "ok",
        "runId": "run-1",
    }
    cin = CanonicalizerInput.model_validate(raw)
    assert cin.run_id == "run-1"


def test_site_transform_multi_input_roundtrip() -> None:
    st = SiteTransform(
        inputs=["/product/a/foo", "/product/a/bar"],
        output_product="/product/a/baz",
        kind="assembly",
    )
    d = st.model_dump(mode="json", by_alias=True)
    assert d["inputs"] == ["/product/a/foo", "/product/a/bar"]
    assert d["outputProduct"] == "/product/a/baz"
    st2 = SiteTransform.model_validate(
        {"inputProduct": "/legacy/in", "outputProduct": "/legacy/out", "kind": "transformation"}
    )
    assert st2.inputs == ["/legacy/in"]
    assert st2.output_product == "/legacy/out"


def test_canonicalizer_result_dump() -> None:
    fr = CanonicalFragment(
        id="f1",
        canonical_path="/org/us/acme",
        fragment_class="org",
        provenance=Provenance(run_id="r", canonicalizer_version=CANONICALIZER_VERSION),
    )
    fr2 = CanonicalFragment(
        id="f2",
        canonical_path="/org/us/beta",
        fragment_class="org",
        provenance=Provenance(run_id="r", canonicalizer_version=CANONICALIZER_VERSION),
    )
    ed = CanonicalEdge(
        id="e1",
        source_fragment_id="f1",
        target_fragment_id="f2",
        relation="subsidiary_of",
        layer="structural",
        layer_section="ownership",
    )
    res = CanonicalizerResult(
        graph=CanonicalGraph(fragments=[fr, fr2], edges=[ed]),
        candidates=Candidates(),
        validation_report=ValidationReport(),
        provenance=CanonicalizerProvenance(
            run_id="r",
            canonicalizer_version=CANONICALIZER_VERSION,
            phase_timings_ms={"phase0_ms": 1.0},
        ),
    )
    d = res.model_dump(mode="json", by_alias=True)
    assert d["graph"]["fragments"][0]["canonicalPath"] == "/org/us/acme"


def test_proposed_relation_roundtrip() -> None:
    pr = ProposedRelation(
        src="Tesla, Inc.",
        dst="Model S",
        relation="produces",
        attributes={"as_of": "2024-01-01"},
        source_claim_ids=["n1"],
    )
    d = pr.model_dump(mode="json", by_alias=True)
    assert d["src"] == "Tesla, Inc."
    assert d["sourceClaimIds"] == ["n1"]
    pr2 = ProposedRelation.model_validate(
        {
            "src": "Port A",
            "dst": "Port B",
            "relation": "supplies",
            "attributes": {"product": "LNG", "incoterm": "FOB"},
            "sourceClaimIds": ["n2"],
        }
    )
    assert pr2.attributes["incoterm"] == "FOB"
    assert pr2.source_claim_ids == ["n2"]


def test_analyzer_output_with_proposed_relations_roundtrip() -> None:
    raw = {
        "chainSummary": "sc",
        "proposedNodes": [],
        "proposedRelations": [
            {
                "src": "Tesla",
                "dst": "Model S",
                "relation": "produces",
                "sourceClaimIds": ["n1"],
            }
        ],
        "openQuestions": [],
    }
    ao = AnalyzerOutput.model_validate(raw)
    assert len(ao.proposed_relations) == 1
    assert ao.proposed_relations[0].relation == "produces"
    d = ao.model_dump(mode="json", by_alias=True)
    assert d["proposedRelations"][0]["src"] == "Tesla"
