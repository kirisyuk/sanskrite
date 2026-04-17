"""Phase 8: unspecified endpoint pairs produce warnings, not hard errors."""

from __future__ import annotations

from agenthorde.canonicalizer.phases.phase8_validate import run_phase8
from agenthorde.canonicalizer.schemas import CanonicalEdge, CanonicalFragment


def test_unspecified_pair_lands_as_warning_not_error() -> None:
    """priced_under org → market is neither recognized (no org projection under
    priced_under in the catalog) nor blacklisted (target type is ok).
    The edge should remain in the graph; validation should log a soft warning
    rather than a hard relation_errors entry."""
    org = CanonicalFragment(
        id="o", canonical_path="/org/ch/vitol", fragment_class="org"
    )
    mkt = CanonicalFragment(
        id="m", canonical_path="/market/nl/ttf", fragment_class="market"
    )
    e = CanonicalEdge(
        id="e",
        source_fragment_id="o",
        target_fragment_id="m",
        relation="priced_under",
        layer="contractual",
        layer_section="regimes_risks",
        attributes={"kind": "unspecified"},
    )
    rep = run_phase8([org, mkt], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )
    assert any(
        w.code == "relation_endpoints_unspecified"
        for w in rep.relation_kind_warnings
    )


def test_recognized_pair_produces_no_warning_or_error() -> None:
    org = CanonicalFragment(
        id="o", canonical_path="/org/us/vanguard", fragment_class="org"
    )
    tgt = CanonicalFragment(
        id="t", canonical_path="/org/us/tesla-inc", fragment_class="org"
    )
    e = CanonicalEdge(
        id="e",
        source_fragment_id="o",
        target_fragment_id="t",
        relation="beneficially_owns",
        layer="structural",
        layer_section="ownership",
        attributes={"kind": "institutional_stake", "stake_pct": 7.1},
    )
    rep = run_phase8([org, tgt], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )
    assert not any(
        w.code == "relation_endpoints_unspecified"
        for w in rep.relation_kind_warnings
    )


def test_supplies_org_to_route_is_recognized_no_warning() -> None:
    """The key Malacca case — supplies org→route is actor_via_corridor."""
    import uuid

    ns = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    ppath = "/product/hydrocarbon/crude-oil"
    pid = str(uuid.uuid5(ns, ppath))
    org = CanonicalFragment(id="o", canonical_path="/org/cn/unipec", fragment_class="org")
    rt = CanonicalFragment(
        id="r", canonical_path="/route/na/malacca-strait", fragment_class="route"
    )
    prod = CanonicalFragment(id=pid, canonical_path=ppath, fragment_class="product")
    e = CanonicalEdge(
        id="e",
        source_fragment_id="o",
        target_fragment_id="r",
        relation="supplies",
        layer="flow",
        layer_section="flow_topology",
        attributes={
            "kind": "actor_via_corridor",
            "product_fragment_id": pid,
            "product": ppath,
        },
    )
    rep = run_phase8([org, rt, prod], [e])
    assert not any(
        re.code == "relation_endpoints_forbidden" for re in rep.relation_errors
    )
    assert not any(
        w.code == "relation_endpoints_unspecified"
        for w in rep.relation_kind_warnings
    )
