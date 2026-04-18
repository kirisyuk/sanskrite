from agenthorde_common.schemas.graph import ReasoningNode

from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.phases.phase3_paths import run_phase3
from agenthorde_common.canonicalizer.schemas import Candidates, CanonicalEntity, ContextHints


def test_phase3_builds_org_path_with_iso2() -> None:
    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Acme is a US company."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="org",
            subtype="listed_issuer",
            surface_forms=["Acme"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
        )
    ]
    surv, issues = run_phase3(ent, ws, Candidates(), 85)
    assert surv[0].canonical_path.startswith("/org/us/")
    assert isinstance(issues, list)


def test_phase3_drops_incoterm_regime_into_candidates() -> None:
    """CIF / FOB mis-emitted as regime entities must be dropped, with the drop
    recorded in candidates.unknown_entity_types."""
    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Priced CIF China."),
        ],
    )
    cand = Candidates()
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="regime",
            subtype="benchmark",
            surface_forms=["CIF"],
            mention_count=1,
            claim_refs=["c1"],
        ),
        CanonicalEntity(
            entity_id="e1",
            type="regime",
            subtype="benchmark",
            surface_forms=["FOB Singapore"],
            mention_count=1,
            claim_refs=["c1"],
        ),
        # Genuine regime stays.
        CanonicalEntity(
            entity_id="e2",
            type="regime",
            subtype="piracy",
            surface_forms=["Malacca piracy"],
            mention_count=1,
            claim_refs=["c1"],
        ),
    ]
    surv, _ = run_phase3(ent, ws, cand, 85)
    surfaces = {s for e in surv for s in e.surface_forms}
    assert "CIF" not in surfaces
    assert "FOB Singapore" not in surfaces
    assert "Malacca piracy" in surfaces
    reasons = [c.reason for c in cand.unknown_entity_types]
    assert reasons.count("incoterm_mis_classified_as_regime") >= 2


def test_phase3_builds_community_dynasty_path() -> None:
    """Lee family chaebol → /community/kr/dynasty/lee-family."""

    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="The Lee family controls Samsung in South Korea."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="community",
            subtype="dynasty",
            surface_forms=["Lee family"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="kr"),
        )
    ]
    surv, issues = run_phase3(ent, ws, Candidates(), 85)
    assert len(surv) == 1
    assert surv[0].canonical_path == "/community/kr/dynasty/lee-family"
    assert not any(i.code.startswith("path_community_") for i in issues)


def test_phase3_product_family_normalizes_semiconductor_aliases() -> None:
    """`semiconductor`, `memory`, `chip` and `chips` all collapse to /product/chip/..."""

    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="DRAM chips are fabbed in Taiwan."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="product",
            subtype="semiconductor",
            surface_forms=["DRAM"],
            mention_count=1,
            claim_refs=["c1"],
        ),
        CanonicalEntity(
            entity_id="e1",
            type="product",
            subtype="memory",
            surface_forms=["NAND"],
            mention_count=1,
            claim_refs=["c1"],
        ),
        CanonicalEntity(
            entity_id="e2",
            type="product",
            subtype="chips",
            surface_forms=["logic chip"],
            mention_count=1,
            claim_refs=["c1"],
        ),
    ]
    surv, _ = run_phase3(ent, ws, Candidates(), 85)
    families = {e.canonical_path.split("/")[2] for e in surv}
    assert families == {"chip"}


def test_phase3_product_family_unknown_falls_back_to_general() -> None:
    """LLM invents a subtype — path uses the `general` fallback, no drift."""

    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Widgetware flows through Panama."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="product",
            subtype="exotic-widget-family-llm-invented",
            surface_forms=["Widgetware"],
            mention_count=1,
            claim_refs=["c1"],
        )
    ]
    surv, _ = run_phase3(ent, ws, Candidates(), 85)
    assert surv[0].canonical_path == "/product/general/widgetware"


def test_phase3_evidence_subtype_normalizes_to_closed_list() -> None:
    """news → quote; 10k → filing; everything unknown → other."""

    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Reuters reports TSMC capex."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="evidence",
            subtype="news",
            surface_forms=["Reuters TSMC capex report"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
        ),
        CanonicalEntity(
            entity_id="e1",
            type="evidence",
            subtype="10k",
            surface_forms=["TSMC 2024 10-K"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="tw"),
        ),
        CanonicalEntity(
            entity_id="e2",
            type="evidence",
            subtype="weird-llm-subtype",
            surface_forms=["Random citation"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
        ),
    ]
    surv, _ = run_phase3(ent, ws, Candidates(), 85)
    subtypes = {e.canonical_path.split("/")[3].split("-")[0] for e in surv}
    assert subtypes == {"quote", "filing", "other"}


def test_phase3_normalizes_unknown_community_subtype_to_key_community() -> None:
    """Unknown subtype falls back to key-community without raising a path error."""

    ws = WorkingState(
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="OSINT analysts watch open data flows."),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="community",
            subtype="weird-subtype-llm-invented",
            surface_forms=["OSINT analysts"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
        )
    ]
    surv, issues = run_phase3(ent, ws, Candidates(), 85)
    assert surv[0].canonical_path == "/community/us/key-community/osint-analysts"
    assert not any(i.code == "path_community_bad_subtype" for i in issues)
