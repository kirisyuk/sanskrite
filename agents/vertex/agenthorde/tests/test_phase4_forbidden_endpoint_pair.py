"""Phase 4: edges with blacklisted endpoint pairs are dropped, not emitted."""

from __future__ import annotations

from agenthorde.canonicalizer.phases.phase4_relations import _build_edge_from_raw
from agenthorde.canonicalizer.schemas import CanonicalEntity, Candidates


def _build(relation: str, fromt: str, tot: str, *, attrs=None):
    src = CanonicalEntity(
        entity_id="s1",
        type=fromt,
        surface_forms=["src"],
        canonical_path=f"/{fromt}/na/src",
    )
    dst = CanonicalEntity(
        entity_id="d1",
        type=tot,
        surface_forms=["dst"],
        canonical_path=f"/{tot}/na/dst",
    )
    ents = [src, dst]
    eids = {e.entity_id for e in ents}
    etype = {e.entity_id: e.type for e in ents}
    cand = Candidates()
    row = {
        "relation": relation,
        "from_id": src.entity_id,
        "to_id": dst.entity_id,
        "attributes": attrs or {},
    }
    edge = _build_edge_from_raw(row, ents, eids, etype, cand, "run", "v")
    return edge, cand


def test_supplies_person_product_is_forbidden() -> None:
    edge, cand = _build("supplies", "person", "product")
    assert edge is None
    assert any(
        c.reason == "forbidden_endpoint_pair" for c in cand.unknown_relations
    )


def test_exposes_to_non_regime_target_is_forbidden() -> None:
    edge, cand = _build("exposes_to", "site", "org")
    assert edge is None
    assert any(
        c.reason == "forbidden_endpoint_pair" for c in cand.unknown_relations
    )


def test_benchmarked_to_requires_market_target() -> None:
    edge, cand = _build("benchmarked_to", "product", "regime")
    assert edge is None
    assert any(
        c.reason == "forbidden_endpoint_pair" for c in cand.unknown_relations
    )


def test_mitigated_by_requires_regime_source() -> None:
    edge, cand = _build("mitigated_by", "org", "route")
    assert edge is None
    assert any(
        c.reason == "forbidden_endpoint_pair" for c in cand.unknown_relations
    )
