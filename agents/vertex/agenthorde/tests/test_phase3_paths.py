from agenthorde.schemas.graph import ReasoningNode

from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.phases.phase3_paths import run_phase3
from agenthorde.canonicalizer.schemas import Candidates, CanonicalEntity, ContextHints


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
