"""Surface resolver: singular/plural fallback for vessel_class and the like."""

from agenthorde.canonicalizer.phases.phase4_relations import _resolve_surface_to_entity
from agenthorde.canonicalizer.schemas import CanonicalEntity


def _mk(entity_id: str, t: str, surface: str, path: str) -> CanonicalEntity:
    return CanonicalEntity(
        entity_id=entity_id,
        type=t,
        surface_forms=[surface],
        canonical_path=path,
    )


def test_resolver_exact_surface_match() -> None:
    e = _mk("e1", "org", "Tesla, Inc.", "/org/us/tesla-inc")
    assert _resolve_surface_to_entity("Tesla, Inc.", [e]) is e


def test_resolver_matches_singular_against_plural_surface() -> None:
    """Analyzer writes 'VLCC' but phase1 extracted surface as 'VLCCs'."""
    e = _mk("e1", "vessel_class", "VLCCs", "/vessel_class/na/vlccs")
    assert _resolve_surface_to_entity("VLCC", [e]) is e


def test_resolver_matches_plural_against_singular_surface() -> None:
    e = _mk("e1", "vessel_class", "VLCC", "/vessel_class/na/vlcc")
    assert _resolve_surface_to_entity("VLCCs", [e]) is e


def test_resolver_es_suffix_fallback() -> None:
    e = _mk("e1", "vessel_class", "Valemaxes", "/vessel_class/na/valemaxes")
    assert _resolve_surface_to_entity("Valemax", [e]) is e


def test_resolver_canonical_path_lookup() -> None:
    e = _mk("e1", "product", "Crude oil", "/product/hydrocarbon/crude-oil")
    assert _resolve_surface_to_entity("/product/hydrocarbon/crude-oil", [e]) is e


def test_resolver_returns_none_for_unknown_surface() -> None:
    e = _mk("e1", "org", "Acme", "/org/na/acme")
    assert _resolve_surface_to_entity("Globex", [e]) is None
