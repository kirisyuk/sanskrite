from agenthorde_common.canonicalizer.phases.phase8_validate import run_phase8
from agenthorde_common.canonicalizer.schemas import CanonicalEdge, CanonicalFragment, SiteTransform
from agenthorde_common.canonicalizer.taxonomy import RELATION_NAMES


def test_phase8_reports_bad_relation_endpoints() -> None:
    f_org = CanonicalFragment(
        id="a",
        canonical_path="/org/na/x",
        fragment_class="org",
    )
    f_person = CanonicalFragment(
        id="b",
        canonical_path="/person/na/y",
        fragment_class="person",
    )
    bad = CanonicalEdge(
        id="e1",
        source_fragment_id="a",
        target_fragment_id="b",
        relation="subsidiary_of",
        layer="structural",
        layer_section="ownership",
    )
    rep = run_phase8([f_org, f_person], [bad])
    assert rep.relation_errors
    assert rep.relation_errors[0].code == "relation_endpoints_forbidden"


def test_phase8_allows_beneficially_owns_org_to_org() -> None:
    """Institutional holders (Vanguard / BlackRock / State Street) use beneficially_owns org -> org."""
    parent = CanonicalFragment(id="p", canonical_path="/org/us/vanguard", fragment_class="org")
    target = CanonicalFragment(id="t", canonical_path="/org/us/tesla-inc", fragment_class="org")
    e_inst = CanonicalEdge(
        id="e-inst",
        source_fragment_id="p",
        target_fragment_id="t",
        relation="beneficially_owns",
        layer="structural",
        layer_section="ownership",
        attributes={"stake_pct": 7.1},
    )
    rep = run_phase8([parent, target], [e_inst])
    assert not any(
        re.code in ("relation_endpoints_forbidden", "relation_endpoints_unspecified")
        for re in rep.relation_errors
    )


def test_phase8_co_shipped_removed_from_taxonomy() -> None:
    assert "co_shipped" not in RELATION_NAMES


def test_phase8_supplies_without_cargo_fails() -> None:
    mine = CanonicalFragment(id="m", canonical_path="/site/cl/mine", fragment_class="site")
    ref = CanonicalFragment(id="r", canonical_path="/site/cl/ref", fragment_class="site")
    e = CanonicalEdge(
        id="e1",
        source_fragment_id="m",
        target_fragment_id="r",
        relation="supplies",
        layer="flow",
        layer_section="flow_topology",
        attributes={},
    )
    rep = run_phase8([mine, ref], [e])
    assert any(x.code == "supply_missing_cargo" for x in rep.relation_errors)


def test_phase8_supplies_with_product_fragment_id_ok() -> None:
    import uuid

    ns = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    ppath = "/product/lithium/spodumene"
    pid = str(uuid.uuid5(ns, ppath))
    mine = CanonicalFragment(id="m", canonical_path="/site/cl/mine", fragment_class="site")
    ref = CanonicalFragment(id="r", canonical_path="/site/cl/ref", fragment_class="site")
    prod = CanonicalFragment(id=pid, canonical_path=ppath, fragment_class="product")
    e = CanonicalEdge(
        id="e1",
        source_fragment_id="m",
        target_fragment_id="r",
        relation="supplies",
        layer="flow",
        layer_section="flow_topology",
        attributes={"product_fragment_id": pid, "product": ppath},
    )
    rep = run_phase8([mine, ref, prod], [e])
    assert not any(x.code == "supply_missing_cargo" for x in rep.relation_errors)


def test_phase8_produces_org_to_product_ok() -> None:
    org = CanonicalFragment(id="o", canonical_path="/org/us/acme", fragment_class="org")
    prod = CanonicalFragment(id="p", canonical_path="/product/industrial/widget", fragment_class="product")
    e = CanonicalEdge(
        id="e1",
        source_fragment_id="o",
        target_fragment_id="p",
        relation="produces",
        layer="structural",
        layer_section="catalog",
    )
    rep = run_phase8([org, prod], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_site_transform_bad_io_reported() -> None:
    site = CanonicalFragment(
        id="s",
        canonical_path="/site/cl/plant",
        fragment_class="site",
        site_transforms=[
            SiteTransform(
                inputs=["/product/x/not-real"],
                output_product="/product/y/also-bad",
                kind="transformation",
            )
        ],
    )
    prod = CanonicalFragment(id="p", canonical_path="/product/x/real", fragment_class="product")
    rep = run_phase8([site, prod], [])
    assert any(x.code == "unknown_transform_io" for x in rep.transform_errors)


def test_phase8_allows_beneficially_owns_person_to_org() -> None:
    person = CanonicalFragment(id="p", canonical_path="/person/us/elon-musk", fragment_class="person")
    target = CanonicalFragment(id="t", canonical_path="/org/us/tesla-inc", fragment_class="org")
    e = CanonicalEdge(
        id="e-bo",
        source_fragment_id="p",
        target_fragment_id="t",
        relation="beneficially_owns",
        layer="structural",
        layer_section="ownership",
        attributes={"stake_pct": 20.5},
    )
    rep = run_phase8([person, target], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_allows_passes_through_vessel_class_to_route_and_site() -> None:
    vlcc = CanonicalFragment(
        id="v", canonical_path="/vessel_class/na/vlcc", fragment_class="vessel_class"
    )
    malacca = CanonicalFragment(
        id="r", canonical_path="/route/na/malacca-strait", fragment_class="route"
    )
    canal = CanonicalFragment(
        id="s", canonical_path="/site/pa/panama-canal", fragment_class="site"
    )
    e1 = CanonicalEdge(
        id="e1",
        source_fragment_id="v",
        target_fragment_id="r",
        relation="passes_through",
        layer="structural",
        layer_section="flow_topology",
    )
    e2 = CanonicalEdge(
        id="e2",
        source_fragment_id="v",
        target_fragment_id="s",
        relation="passes_through",
        layer="structural",
        layer_section="flow_topology",
    )
    rep = run_phase8([vlcc, malacca, canal], [e1, e2])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_allows_leg_connects_route_to_site_and_geo() -> None:
    route = CanonicalFragment(id="r", canonical_path="/route/na/suez", fragment_class="route")
    port = CanonicalFragment(id="s", canonical_path="/site/sg/singapore", fragment_class="site")
    geo = CanonicalFragment(id="g", canonical_path="/geo/cn", fragment_class="geo")
    e1 = CanonicalEdge(
        id="e1",
        source_fragment_id="r",
        target_fragment_id="s",
        relation="leg_connects",
        layer="flow",
        layer_section="flow_topology",
    )
    e2 = CanonicalEdge(
        id="e2",
        source_fragment_id="r",
        target_fragment_id="g",
        relation="leg_connects",
        layer="flow",
        layer_section="flow_topology",
    )
    rep = run_phase8([route, port, geo], [e1, e2])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_allows_constrained_by_vessel_class_to_route() -> None:
    vlcc = CanonicalFragment(
        id="v", canonical_path="/vessel_class/na/vlcc", fragment_class="vessel_class"
    )
    malacca = CanonicalFragment(
        id="r", canonical_path="/route/na/malacca-strait", fragment_class="route"
    )
    e = CanonicalEdge(
        id="e",
        source_fragment_id="v",
        target_fragment_id="r",
        relation="constrained_by",
        layer="structural",
        layer_section="regimes_risks",
    )
    rep = run_phase8([vlcc, malacca], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_allows_exposes_to_any_to_regime() -> None:
    route = CanonicalFragment(id="r", canonical_path="/route/na/malacca-strait", fragment_class="route")
    regime = CanonicalFragment(id="g", canonical_path="/regime/na/piracy", fragment_class="regime")
    e = CanonicalEdge(
        id="e",
        source_fragment_id="r",
        target_fragment_id="g",
        relation="exposes_to",
        layer="structural",
        layer_section="regimes_risks",
    )
    rep = run_phase8([route, regime], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )


def test_phase8_flags_exposes_to_non_regime_target() -> None:
    """exposes_to must still require the target to be a regime."""
    route = CanonicalFragment(id="r", canonical_path="/route/na/malacca", fragment_class="route")
    geo = CanonicalFragment(id="g", canonical_path="/geo/cn", fragment_class="geo")
    e = CanonicalEdge(
        id="e",
        source_fragment_id="r",
        target_fragment_id="g",
        relation="exposes_to",
        layer="structural",
        layer_section="regimes_risks",
    )
    rep = run_phase8([route, geo], [e])
    assert any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )
