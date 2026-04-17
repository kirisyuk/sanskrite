"""Phase 4: verify CanonicalEdge.attributes.kind is stamped from the catalog."""

from __future__ import annotations

import uuid

import pytest

from agenthorde.canonicalizer.phases.phase4_relations import _build_edge_from_raw
from agenthorde.canonicalizer.schemas import CanonicalEntity, Candidates


def _mk_entity(entity_id: str, t: str, surface: str, path: str) -> CanonicalEntity:
    return CanonicalEntity(
        entity_id=entity_id,
        type=t,
        surface_forms=[surface],
        canonical_path=path,
    )


def _build(
    relation: str,
    *,
    src: CanonicalEntity,
    dst: CanonicalEntity,
    attrs: dict | None = None,
    entities: list[CanonicalEntity] | None = None,
):
    ents = entities if entities is not None else [src, dst]
    eids = {e.entity_id for e in ents}
    etype = {e.entity_id: e.type for e in ents}
    cand = Candidates()
    row = {
        "relation": relation,
        "from_id": src.entity_id,
        "to_id": dst.entity_id,
        "attributes": attrs or {},
    }
    edge = _build_edge_from_raw(
        row,
        ents,
        eids,
        etype,
        cand,
        run_id="run-test",
        version="test",
    )
    return edge, cand


def test_supplies_physical_leg_kind() -> None:
    src = _mk_entity("e-src", "site", "Ras Tanura", "/site/sa/ras-tanura")
    dst = _mk_entity("e-dst", "site", "Qingdao", "/site/cn/qingdao")
    ppath = "/product/hydrocarbon/crude-oil"
    ns = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    prod = CanonicalEntity(
        entity_id=str(uuid.uuid5(ns, ppath)),
        type="product",
        surface_forms=["crude oil"],
        canonical_path=ppath,
    )
    edge, cand = _build(
        "supplies",
        src=src,
        dst=dst,
        entities=[src, dst, prod],
        attrs={"product": "crude oil"},
    )
    assert edge is not None
    assert edge.attributes.get("kind") == "physical_leg"
    assert not any(
        c.reason in ("forbidden_endpoint_pair", "endpoint_pair_not_in_catalog")
        for c in cand.unknown_relations
    )


def test_supplies_actor_via_corridor_kind() -> None:
    """org→route is a recognized projection under `actor_via_corridor`."""
    org = _mk_entity("e1", "org", "Unipec", "/org/cn/unipec")
    route = _mk_entity("e2", "route", "Malacca Strait", "/route/na/malacca-strait")
    ppath = "/product/hydrocarbon/crude-oil"
    ns = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    prod = CanonicalEntity(
        entity_id=str(uuid.uuid5(ns, ppath)),
        type="product",
        surface_forms=["crude oil"],
        canonical_path=ppath,
    )
    edge, cand = _build(
        "supplies",
        src=org,
        dst=route,
        entities=[org, route, prod],
        attrs={"product": "crude oil"},
    )
    assert edge is not None
    assert edge.attributes.get("kind") == "actor_via_corridor"


def test_charters_fleet_charter_kind() -> None:
    org = _mk_entity("e1", "org", "Unipec", "/org/cn/unipec")
    vc = _mk_entity("e2", "vessel_class", "VLCC", "/vessel_class/na/vlcc")
    edge, _ = _build("charters", src=org, dst=vc)
    assert edge is not None
    assert edge.attributes.get("kind") == "fleet_charter"


def test_transships_at_carrier_hub_kind() -> None:
    org = _mk_entity("e1", "org", "Maersk", "/org/dk/maersk")
    site = _mk_entity("e2", "site", "Singapore", "/site/sg/singapore")
    edge, _ = _build("transships_at", src=org, dst=site)
    assert edge is not None
    assert edge.attributes.get("kind") == "carrier_hub"


def test_passes_through_class_over_corridor_kind() -> None:
    vc = _mk_entity("e1", "vessel_class", "VLCC", "/vessel_class/na/vlcc")
    route = _mk_entity("e2", "route", "Malacca", "/route/na/malacca")
    edge, _ = _build("passes_through", src=vc, dst=route)
    assert edge is not None
    assert edge.attributes.get("kind") == "class_over_corridor"


def test_passes_through_site_route_chokepoint_kind() -> None:
    """A strait/canal (site) passing through a corridor (route) is the
    `chokepoint_on_corridor` projection — no warnings, no candidates."""
    strait = _mk_entity("e1", "site", "Bosporus", "/site/tr/bosporus")
    route = _mk_entity("e2", "route", "Black Sea corridor", "/route/na/black-sea")
    edge, cand = _build("passes_through", src=strait, dst=route)
    assert edge is not None
    assert edge.attributes.get("kind") == "chokepoint_on_corridor"
    assert not any(
        c.reason in ("forbidden_endpoint_pair", "endpoint_pair_not_in_catalog")
        for c in cand.unknown_relations
    )


def test_benchmarked_to_layer_clamped_when_llm_provides_structural() -> None:
    """Regression: LLM sometimes emits `layer: structural` for every edge.
    benchmarked_to is contractual-only — phase 4 must clamp, not produce a
    layer_error downstream."""
    prod = _mk_entity("e1", "product", "Urals", "/product/hydrocarbon/urals-crude")
    mkt = _mk_entity("e2", "market", "Dated Brent", "/market/na/dated-brent")
    ents = [prod, mkt]
    eids = {e.entity_id for e in ents}
    etype = {e.entity_id: e.type for e in ents}
    cand = Candidates()
    row = {
        "relation": "benchmarked_to",
        "from_id": prod.entity_id,
        "to_id": mkt.entity_id,
        "layer": "structural",  # LLM hallucination — not in the allowed set
        "attributes": {},
    }
    edge = _build_edge_from_raw(row, ents, eids, etype, cand, run_id="t", version="t")
    assert edge is not None
    assert edge.layer == "contractual"
    assert edge.attributes.get("kind") == "product_benchmark"


def test_charters_layer_clamped_from_bogus_value() -> None:
    """Allowed layers for charters are {contractual, flow}. 'structural'
    supplied by the LLM must be clamped to the default."""
    org = _mk_entity("e1", "org", "Sovcomflot", "/org/ru/sovcomflot")
    vc = _mk_entity("e2", "vessel_class", "Aframax", "/vessel_class/na/aframax")
    ents = [org, vc]
    eids = {e.entity_id for e in ents}
    etype = {e.entity_id: e.type for e in ents}
    cand = Candidates()
    row = {
        "relation": "charters",
        "from_id": org.entity_id,
        "to_id": vc.entity_id,
        "layer": "structural",
        "attributes": {},
    }
    edge = _build_edge_from_raw(row, ents, eids, etype, cand, run_id="t", version="t")
    assert edge is not None
    assert edge.layer in ("contractual", "flow")


def test_valid_layer_is_preserved() -> None:
    """When the LLM supplies an allowed layer, phase 4 must not override it."""
    org = _mk_entity("e1", "org", "Sovcomflot", "/org/ru/sovcomflot")
    vc = _mk_entity("e2", "vessel_class", "Aframax", "/vessel_class/na/aframax")
    ents = [org, vc]
    eids = {e.entity_id for e in ents}
    etype = {e.entity_id: e.type for e in ents}
    cand = Candidates()
    row = {
        "relation": "charters",
        "from_id": org.entity_id,
        "to_id": vc.entity_id,
        "layer": "flow",
        "attributes": {},
    }
    edge = _build_edge_from_raw(row, ents, eids, etype, cand, run_id="t", version="t")
    assert edge is not None
    assert edge.layer == "flow"


def test_unspecified_pair_kept_with_unspecified_kind_and_candidate() -> None:
    """priced_under org→market isn't in the kinds catalog (org doesn't have a
    priced_under projection) and isn't blacklisted either — edge should still
    land with attributes.kind='unspecified' and a candidate entry recording
    'endpoint_pair_not_in_catalog'."""
    org = _mk_entity("e1", "org", "Vitol", "/org/ch/vitol")
    mkt = _mk_entity("e2", "market", "TTF", "/market/nl/ttf")
    edge, cand = _build("priced_under", src=org, dst=mkt)
    assert edge is not None
    assert edge.attributes.get("kind") == "unspecified"
    assert any(
        c.reason == "endpoint_pair_not_in_catalog"
        for c in cand.unknown_relations
    )


@pytest.mark.parametrize(
    "relation,from_t,to_t",
    [
        ("produces", "site", "product"),
        ("charters", "site", "vessel_class"),
        ("operates", "org", "org"),
        # subsidiary_of org->person gets repair'd to beneficially_owns org->person,
        # which itself is blacklisted — so the edge still gets dropped.
        ("subsidiary_of", "org", "person"),
    ],
)
def test_forbidden_pair_drops_edge_and_records_candidate(
    relation: str, from_t: str, to_t: str
) -> None:
    src = _mk_entity("e1", from_t, "src-surf", f"/{from_t}/na/src")
    dst = _mk_entity("e2", to_t, "dst-surf", f"/{to_t}/na/dst")
    edge, cand = _build(relation, src=src, dst=dst)
    assert edge is None
    assert any(
        c.reason == "forbidden_endpoint_pair" for c in cand.unknown_relations
    )
