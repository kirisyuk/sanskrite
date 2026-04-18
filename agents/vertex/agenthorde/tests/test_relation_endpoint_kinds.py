"""Unit tests for the RELATION_ENDPOINT_KINDS catalog and helpers."""

from __future__ import annotations

import pytest

from agenthorde_common.canonicalizer.taxonomy import (
    RELATION_ENDPOINT_KINDS,
    is_forbidden_endpoint_pair,
    relation_kind_for_endpoints,
)


@pytest.mark.parametrize(
    "relation,from_cls,to_cls,expected_kind",
    [
        ("supplies", "site", "site", "physical_leg"),
        ("supplies", "org", "site", "seller_to_destination"),
        ("supplies", "org", "org", "commercial"),
        ("supplies", "org", "geo", "actor_to_region"),
        ("supplies", "org", "market", "actor_to_market"),
        ("supplies", "org", "route", "actor_via_corridor"),
        ("supplies", "site", "geo", "site_to_region"),
        ("supplies", "geo", "geo", "trade_flow"),
        ("transships_at", "route", "site", "route_hub"),
        ("transships_at", "org", "site", "carrier_hub"),
        ("transships_at", "site", "site", "hub_cascade"),
        ("charters", "org", "vessel_class", "fleet_charter"),
        ("passes_through", "vessel_class", "route", "class_over_corridor"),
        ("passes_through", "route", "geo", "route_over_region"),
        ("passes_through", "site", "route", "chokepoint_on_corridor"),
        ("leg_connects", "site", "site", "site_to_site"),
        ("leg_connects", "route", "site", "route_to_site"),
        ("alternate_to", "vessel_class", "vessel_class", "fleet_alt"),
        ("constrained_by", "vessel_class", "route", "class_over_route_draft"),
        ("exposes_to", "route", "regime", "route_exposed"),
        ("subject_to", "org", "regime", "org_subject_to"),
        ("mitigated_by", "regime", "route", "regime_mitigated_by_route"),
        ("priced_under", "product", "market", "product_benchmark"),
        ("benchmarked_to", "product", "market", "product_benchmark"),
        ("publishes_benchmark", "org", "market", "price_reference_publisher"),
        ("beneficially_owns", "org", "org", "institutional_stake"),
        ("beneficially_owns", "person", "org", "ubo_stake"),
        ("listed_on", "org", "market", "listed"),
        ("incorporated_in", "org", "geo", "incorporated"),
    ],
)
def test_recognized_kind_labels(relation, from_cls, to_cls, expected_kind) -> None:
    assert relation_kind_for_endpoints(relation, from_cls, to_cls) == expected_kind


def test_unknown_relation_returns_none() -> None:
    assert relation_kind_for_endpoints("nope", "org", "org") is None


def test_unspecified_pair_returns_none() -> None:
    """vessel_class alternate_to route is neither recognized nor forbidden in
    the fleet_alt projection — it should fall through to None (warning level)."""
    assert relation_kind_for_endpoints("alternate_to", "vessel_class", "route") is None


@pytest.mark.parametrize(
    "relation,from_cls,to_cls",
    [
        ("supplies", "person", "product"),
        ("supplies", "product", "product"),
        ("supplies", "vessel_class", "person"),
        ("charters", "person", "vessel_class"),
        ("charters", "site", "vessel_class"),
        ("subsidiary_of", "person", "org"),
        ("subsidiary_of", "org", "person"),
        ("regulates", "site", "org"),
        ("operates", "site", "org"),
        ("produces", "product", "org"),
        ("leg_connects", "org", "org"),
        ("passes_through", "org", "geo"),
    ],
)
def test_blacklist_pairs_forbidden(relation, from_cls, to_cls) -> None:
    assert is_forbidden_endpoint_pair(relation, from_cls, to_cls)


def test_subject_to_requires_regime_target() -> None:
    assert is_forbidden_endpoint_pair("subject_to", "org", "geo")
    assert not is_forbidden_endpoint_pair("subject_to", "org", "regime")


def test_exposes_to_requires_regime_target() -> None:
    assert is_forbidden_endpoint_pair("exposes_to", "site", "org")
    assert not is_forbidden_endpoint_pair("exposes_to", "site", "regime")


def test_mitigated_by_requires_regime_source() -> None:
    assert is_forbidden_endpoint_pair("mitigated_by", "org", "route")
    assert not is_forbidden_endpoint_pair("mitigated_by", "regime", "route")


def test_benchmarked_to_requires_market_target() -> None:
    assert is_forbidden_endpoint_pair("benchmarked_to", "product", "regime")
    assert not is_forbidden_endpoint_pair("benchmarked_to", "product", "market")


def test_publishes_benchmark_requires_org_to_market() -> None:
    """PRA -> market is the only recognized shape for publishes_benchmark."""
    assert not is_forbidden_endpoint_pair("publishes_benchmark", "org", "market")
    assert is_forbidden_endpoint_pair("publishes_benchmark", "product", "market")
    assert is_forbidden_endpoint_pair("publishes_benchmark", "org", "product")
    assert is_forbidden_endpoint_pair("publishes_benchmark", "site", "market")
    assert is_forbidden_endpoint_pair("publishes_benchmark", "person", "market")


def test_kinds_are_snake_case_short_identifiers() -> None:
    """Labels must be short snake_case — downstream queries rely on them."""
    import re

    pat = re.compile(r"^[a-z][a-z0-9_]{1,40}$")
    for relation, mp in RELATION_ENDPOINT_KINDS.items():
        for (fc, tc), kind in mp.items():
            assert pat.match(kind), (
                f"Bad kind label {kind!r} for {relation} {fc}->{tc}"
            )


def test_kinds_and_blacklist_dont_overlap() -> None:
    """A pair cannot simultaneously be recognized and blacklisted."""
    from agenthorde_common.canonicalizer.taxonomy import RELATION_ENDPOINT_BLACKLIST

    for relation, kinds in RELATION_ENDPOINT_KINDS.items():
        bl = RELATION_ENDPOINT_BLACKLIST.get(relation, frozenset())
        overlap = set(kinds.keys()) & bl
        assert not overlap, (
            f"Relation {relation}: pairs {overlap} are both recognized and blacklisted"
        )
