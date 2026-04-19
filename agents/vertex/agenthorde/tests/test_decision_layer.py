"""Tests for the decision/mechanism/influences/assures layer added to AgentHorde.

Covers:
    * canonical paths for /decision and /mechanism (phase 3),
    * RELATION_ENDPOINT_KINDS for the 6 new relations + procedural blacklists,
    * required closed-enum attributes (`influence_kind`, `assurance_type`),
    * normalize_* helpers for MechanismCategory / DecisionSubtype /
      InfluenceKind / AssuranceType.
"""

from __future__ import annotations

from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.phases.phase3_paths import build_canonical_path, run_phase3
from agenthorde_common.canonicalizer.schemas import (
    Candidates,
    CanonicalEdge,
    CanonicalEntity,
    CanonicalFragment,
    ContextHints,
)
from agenthorde_common.canonicalizer.taxonomy import (
    ASSURANCE_TYPES,
    DECISION_SUBTYPES,
    FRAGMENT_CLASSES,
    INFLUENCE_KINDS,
    MECHANISM_CATEGORIES,
    RELATION_ENDPOINT_KINDS,
    RELATION_LAYER_SECTION,
    RELATION_NAMES,
    is_forbidden_endpoint_pair,
    normalize_assurance_type,
    normalize_decision_subtype,
    normalize_influence_kind,
    normalize_mechanism_category,
    relation_kind_for_endpoints,
)
from agenthorde_common.canonicalizer.validation import validate_canonical_path, validate_graph
from agenthorde_common.schemas.graph import ReasoningNode


def test_decision_and_mechanism_in_fragment_classes() -> None:
    assert "decision" in FRAGMENT_CLASSES
    assert "mechanism" in FRAGMENT_CLASSES


def test_new_relations_present() -> None:
    for r in ("gatekeeps", "wields", "enacted_via", "imposes", "influences", "assures"):
        assert r in RELATION_NAMES, f"missing relation: {r}"
    assert RELATION_LAYER_SECTION["gatekeeps"] == "decision_layer"
    assert RELATION_LAYER_SECTION["assures"] == "assurance"


def test_normalize_helpers_closed_enums() -> None:
    assert normalize_mechanism_category("Tariff") == "economic"
    assert normalize_mechanism_category(None) == "regulatory"
    assert normalize_mechanism_category("regulatory") == "regulatory"

    assert normalize_decision_subtype("OFAC designation") == "sanctions_designation"
    assert normalize_decision_subtype("court ruling") == "judicial_interpretation"
    assert normalize_decision_subtype(None) == "regulatory_ruling"

    assert normalize_influence_kind("lobby") == "lobbying"
    assert normalize_influence_kind("vendor lock in") == "technical_dependency"
    assert normalize_influence_kind(None) == "informal_channel"

    assert normalize_assurance_type("ISO certification") == "certification"
    assert normalize_assurance_type("SOC2") == "attestation"
    assert normalize_assurance_type("inspection") == "regulatory_inspection"
    assert normalize_assurance_type(None) == "audit"


def test_endpoint_kinds_for_new_relations() -> None:
    assert relation_kind_for_endpoints("gatekeeps", "org", "decision") == "decision_owner"
    assert relation_kind_for_endpoints("wields", "org", "mechanism") == "mechanism_user"
    assert relation_kind_for_endpoints("enacted_via", "decision", "mechanism") == "mechanism_choice"
    assert relation_kind_for_endpoints("imposes", "mechanism", "site") == "imposes_on_site"
    assert relation_kind_for_endpoints("influences", "org", "decision") == "informal_decision_sway"
    assert relation_kind_for_endpoints("assures", "org", "site") == "assurer_to_assuree_site"


def test_forbidden_endpoint_pairs_for_new_relations() -> None:
    # gatekeeps must always be org -> decision (formal authority).
    assert is_forbidden_endpoint_pair("gatekeeps", "person", "decision")
    assert is_forbidden_endpoint_pair("gatekeeps", "community", "decision")
    assert not is_forbidden_endpoint_pair("gatekeeps", "org", "decision")

    # wields must be org -> mechanism.
    assert is_forbidden_endpoint_pair("wields", "person", "mechanism")
    assert not is_forbidden_endpoint_pair("wields", "org", "mechanism")

    # enacted_via must be decision -> mechanism.
    assert is_forbidden_endpoint_pair("enacted_via", "org", "mechanism")
    assert is_forbidden_endpoint_pair("enacted_via", "decision", "decision")
    assert not is_forbidden_endpoint_pair("enacted_via", "decision", "mechanism")

    # imposes: mechanism -> things in the world (no decision/mechanism/evidence target).
    assert is_forbidden_endpoint_pair("imposes", "org", "site")
    assert is_forbidden_endpoint_pair("imposes", "mechanism", "decision")
    assert is_forbidden_endpoint_pair("imposes", "mechanism", "evidence")
    assert not is_forbidden_endpoint_pair("imposes", "mechanism", "site")
    assert not is_forbidden_endpoint_pair("imposes", "mechanism", "org")

    # influences: actor -> {decision,org,mechanism}.
    assert is_forbidden_endpoint_pair("influences", "decision", "decision")
    assert is_forbidden_endpoint_pair("influences", "evidence", "decision")
    assert is_forbidden_endpoint_pair("influences", "org", "site")
    assert not is_forbidden_endpoint_pair("influences", "org", "decision")
    assert not is_forbidden_endpoint_pair("influences", "community", "org")

    # assures: org -> org/site only.
    assert is_forbidden_endpoint_pair("assures", "person", "org")
    assert is_forbidden_endpoint_pair("assures", "org", "route")
    assert not is_forbidden_endpoint_pair("assures", "org", "org")
    assert not is_forbidden_endpoint_pair("assures", "org", "site")


def test_phase3_builds_decision_path() -> None:
    """/decision/{domain-slug}/{slug} — domain comes from attributes.domain_slug."""
    ws = WorkingState(
        all_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="BIS is expected to rule on the EUV export license for SMIC.",
            ),
        ],
    )
    ent = [
        CanonicalEntity(
            entity_id="e0",
            type="decision",
            subtype="regulatory_ruling",
            surface_forms=["BIS EUV export license decision"],
            mention_count=1,
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
            attributes={"domain_slug": "export-controls"},
        )
    ]
    surv, _ = run_phase3(ent, ws, Candidates(), 85)
    assert surv[0].canonical_path.startswith("/decision/export-controls/")
    assert validate_canonical_path(surv[0].canonical_path) == []


def test_phase3_builds_decision_path_with_default_domain() -> None:
    ent = CanonicalEntity(
        entity_id="e0",
        type="decision",
        subtype="commercial_strategic",
        surface_forms=["Acme board approves Project X"],
        mention_count=1,
        claim_refs=[],
    )
    path = build_canonical_path(ent, WorkingState(all_nodes=[]), narrative_iso2=None)
    assert path.startswith("/decision/general/")


def test_phase3_builds_mechanism_path_with_category() -> None:
    """/mechanism/{category}/{slug} — category from subtype, normalized."""
    ent = CanonicalEntity(
        entity_id="e0",
        type="mechanism",
        subtype="tariff",  # alias -> economic
        surface_forms=["Section 301 tariff"],
        mention_count=1,
        claim_refs=[],
    )
    path = build_canonical_path(ent, WorkingState(all_nodes=[]), narrative_iso2=None)
    assert path.startswith("/mechanism/economic/")
    assert validate_canonical_path(path) == []


def test_validate_canonical_path_rejects_unknown_mechanism_category() -> None:
    issues = validate_canonical_path("/mechanism/nonsense/section-301")
    codes = {iss.code for iss in issues}
    assert "path_mechanism_bad_category" in codes


def test_influences_edge_requires_kind_attribute() -> None:
    bis = CanonicalFragment(
        id="f0",
        canonical_path="/org/us/bis",
        fragment_class="org",
    )
    decision = CanonicalFragment(
        id="f1",
        canonical_path="/decision/export-controls/euv-smic",
        fragment_class="decision",
    )
    sia = CanonicalFragment(
        id="f2",
        canonical_path="/org/us/semiconductor-industry-association",
        fragment_class="org",
    )
    edges = [
        CanonicalEdge(
            id="e0",
            source_fragment_id="f0",
            target_fragment_id="f1",
            relation="gatekeeps",
            layer="structural",
            layer_section="decision_layer",
        ),
        CanonicalEdge(
            id="e1",
            source_fragment_id="f2",
            target_fragment_id="f1",
            relation="influences",
            layer="structural",
            layer_section="decision_layer",
            attributes={"influence_kind": "lobbying"},
        ),
        # Missing influence_kind → should produce a relation_error.
        CanonicalEdge(
            id="e2",
            source_fragment_id="f2",
            target_fragment_id="f1",
            relation="influences",
            layer="structural",
            layer_section="decision_layer",
        ),
    ]
    report = validate_graph([bis, decision, sia], edges)
    codes = {iss.code for iss in report.relation_errors}
    assert "influences_missing_kind" in codes
    # The well-formed influences edge should not produce that error twice.
    missing_count = sum(1 for iss in report.relation_errors if iss.code == "influences_missing_kind")
    assert missing_count == 1


def test_assures_edge_requires_assurance_type() -> None:
    deloitte = CanonicalFragment(
        id="f0",
        canonical_path="/org/uk/deloitte",
        fragment_class="org",
    )
    aramco = CanonicalFragment(
        id="f1",
        canonical_path="/org/sa/saudi-aramco",
        fragment_class="org",
    )
    edges = [
        CanonicalEdge(
            id="e0",
            source_fragment_id="f0",
            target_fragment_id="f1",
            relation="assures",
            layer="structural",
            layer_section="assurance",
            attributes={"assurance_type": "audit"},
        ),
        CanonicalEdge(
            id="e1",
            source_fragment_id="f0",
            target_fragment_id="f1",
            relation="assures",
            layer="structural",
            layer_section="assurance",
        ),
    ]
    report = validate_graph([deloitte, aramco], edges)
    codes = {iss.code for iss in report.relation_errors}
    assert "assures_missing_type" in codes


def test_subtype_enum_membership_is_locked() -> None:
    # Sanity: locking the enums prevents accidental drift.
    assert MECHANISM_CATEGORIES == frozenset(
        {"regulatory", "economic", "technological", "contractual", "political"}
    )
    assert DECISION_SUBTYPES == frozenset(
        {
            "regulatory_ruling",
            "sanctions_designation",
            "judicial_interpretation",
            "commercial_strategic",
            "standards_decision",
            "coalition_position",
            "legislative",
        }
    )
    assert INFLUENCE_KINDS == frozenset(
        {
            "lobbying",
            "advisory",
            "technical_dependency",
            "market_power",
            "coalition_member",
            "judicial_precedent",
            "informal_channel",
        }
    )
    assert ASSURANCE_TYPES == frozenset(
        {"audit", "certification", "regulatory_inspection", "attestation"}
    )


def test_relation_endpoint_kinds_table_has_all_new_relations() -> None:
    for r in ("gatekeeps", "wields", "enacted_via", "imposes", "influences", "assures"):
        assert r in RELATION_ENDPOINT_KINDS, f"endpoint kinds missing for {r}"
        assert RELATION_ENDPOINT_KINDS[r], f"endpoint kinds for {r} are empty"


# ---------------------------------------------------------------------------
# /mobile_asset fragment + member_of + disrupts
# ---------------------------------------------------------------------------


def test_mobile_asset_in_fragment_classes() -> None:
    assert "mobile_asset" in FRAGMENT_CLASSES


def test_member_of_and_disrupts_relations_present() -> None:
    assert "member_of" in RELATION_NAMES
    assert "disrupts" in RELATION_NAMES
    assert RELATION_LAYER_SECTION["member_of"] == "membership"
    assert RELATION_LAYER_SECTION["disrupts"] == "disruption"


def test_member_of_endpoint_kinds_and_blacklist() -> None:
    assert relation_kind_for_endpoints("member_of", "person", "org") == "person_member_of_org"
    assert relation_kind_for_endpoints("member_of", "org", "community") == "org_member_of_industry_body"
    # site/route cannot be a member.
    assert is_forbidden_endpoint_pair("member_of", "site", "org")
    assert is_forbidden_endpoint_pair("member_of", "route", "community")
    # community cannot be a member of an org (use participates / influences).
    assert is_forbidden_endpoint_pair("member_of", "community", "org")


def test_disrupts_endpoint_kinds_and_blacklist() -> None:
    assert relation_kind_for_endpoints("disrupts", "geo", "site") == "zone_disrupts_site"
    assert relation_kind_for_endpoints("disrupts", "evidence", "route") == "event_disrupts_route"
    assert relation_kind_for_endpoints("disrupts", "geo", "mobile_asset") == "zone_disrupts_mobile_asset"
    # Decision/mechanism/regime cannot be disrupted (they are the modeling primitives).
    assert is_forbidden_endpoint_pair("disrupts", "geo", "decision")
    assert is_forbidden_endpoint_pair("disrupts", "geo", "mechanism")
    assert is_forbidden_endpoint_pair("disrupts", "geo", "regime")
    # Org / route / mobile_asset cannot themselves be a disruption source.
    assert is_forbidden_endpoint_pair("disrupts", "org", "site")
    assert is_forbidden_endpoint_pair("disrupts", "site", "route")


def test_phase3_builds_mobile_asset_path_with_imo() -> None:
    from agenthorde_common.canonicalizer.schemas import CanonicalEntity, ContextHints

    ent = CanonicalEntity(
        entity_id="e0",
        type="mobile_asset",
        subtype="vessel",
        surface_forms=["Pacific Star"],
        mention_count=1,
        claim_refs=[],
        context_hints=ContextHints(iso2_hint="lr"),
        attributes={"imo": "9876543"},
    )
    path = build_canonical_path(ent, WorkingState(all_nodes=[]), narrative_iso2=None)
    assert path == "/mobile_asset/vessel/lr/pacific-star-9876543"
    assert validate_canonical_path(path) == []


def test_phase3_normalizes_unknown_mobile_asset_subtype() -> None:
    from agenthorde_common.canonicalizer.schemas import CanonicalEntity

    ent = CanonicalEntity(
        entity_id="e0",
        type="mobile_asset",
        subtype="unknown_kind",
        surface_forms=["Mystery Object"],
        mention_count=1,
        claim_refs=[],
    )
    path = build_canonical_path(ent, WorkingState(all_nodes=[]), narrative_iso2=None)
    # Falls back to /asset, with a generated -id8 suffix from entity_id.
    assert path.startswith("/mobile_asset/asset/na/mystery-object-")


def test_validate_canonical_path_rejects_unknown_mobile_asset_kind() -> None:
    issues = validate_canonical_path("/mobile_asset/spaceship/us/voyager-1-1234")
    codes = {iss.code for iss in issues}
    assert "path_mobile_asset_bad_kind" in codes


def test_disrupts_edge_requires_disruption_kind() -> None:
    geo = CanonicalFragment(
        id="f0",
        canonical_path="/geo/ye/red-sea",
        fragment_class="geo",
    )
    site = CanonicalFragment(
        id="f1",
        canonical_path="/site/sa/jazan-refinery",
        fragment_class="site",
    )
    edges = [
        CanonicalEdge(
            id="e0",
            source_fragment_id="f0",
            target_fragment_id="f1",
            relation="disrupts",
            layer="structural",
            layer_section="disruption",
            attributes={"disruption_kind": "armed_conflict"},
        ),
        CanonicalEdge(
            id="e1",
            source_fragment_id="f0",
            target_fragment_id="f1",
            relation="disrupts",
            layer="structural",
            layer_section="disruption",
        ),
    ]
    report = validate_graph([geo, site], edges)
    codes = {iss.code for iss in report.relation_errors}
    assert "disrupts_missing_kind" in codes


def test_normalize_helpers_for_new_enums() -> None:
    from agenthorde_common.canonicalizer.taxonomy import (
        normalize_disruption_kind,
        normalize_mobile_asset_kind,
    )

    assert normalize_mobile_asset_kind("VLCC") == "vessel"
    assert normalize_mobile_asset_kind("drone") == "aircraft"
    assert normalize_mobile_asset_kind(None) == "asset"
    assert normalize_mobile_asset_kind("vessel") == "vessel"

    assert normalize_disruption_kind("typhoon") == "natural_disaster"
    assert normalize_disruption_kind("war") == "armed_conflict"
    assert normalize_disruption_kind("cyber_incident") == "cyber_incident"
    assert normalize_disruption_kind(None) == "other"
