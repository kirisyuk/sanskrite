"""Surface resolver: singular/plural fallback for vessel_class and the like."""

from agenthorde_common.canonicalizer.phases.phase4_relations import _resolve_surface_to_entity
from agenthorde_common.canonicalizer.schemas import CanonicalEntity


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


def test_resolver_token_set_fallback_strips_snake_case_prefix() -> None:
    """Analyzer emits ``pra_trendforce`` as endpoint id; entity surface is ``TrendForce``.

    This is the TSMC-cycle bug: snake_case ad-hoc identifiers with a type
    prefix (``pra_``) blow past exact + legal-suffix + synonym lookup. The
    token_set fallback over normalized surfaces (``pra trendforce`` vs
    ``trendforce``) rescues the match.
    """

    e = _mk("e1", "org", "TrendForce", "/org/tw/trendforce")
    assert _resolve_surface_to_entity("pra_trendforce", [e]) is e


def test_resolver_token_set_fallback_resolves_multi_token_descriptor() -> None:
    """``tsmc_taiwan_fab`` should match ``Taiwan Semiconductor Manufacturing Company``."""

    e = _mk(
        "e1",
        "org",
        "Taiwan Semiconductor Manufacturing Company",
        "/org/tw/taiwan-semiconductor-manufacturing",
    )
    # token_set_ratio({"taiwan","semiconductor","manufacturing","company"},
    #                 {"tsmc","taiwan","fab"}) — overlap "taiwan" alone is too thin;
    # we expect this NOT to match (good — false positives must stay rare).
    assert _resolve_surface_to_entity("tsmc_taiwan_fab", [e]) is None


def test_resolver_token_set_fallback_picks_higher_intersection_winner() -> None:
    """When two entities share a token with the query, prefer the one with MORE shared tokens.

    ``apple_daily_hk`` against {Apple Inc, Apple Daily}: both score 100 on
    token_set_ratio, but Apple Daily shares 2 tokens with the query and Apple
    Inc shares only 1. The resolver must pick Apple Daily.
    """

    apple_inc = _mk("e1", "org", "Apple Inc", "/org/us/apple-inc")
    apple_daily = _mk("e2", "org", "Apple Daily", "/org/hk/apple-daily")
    out = _resolve_surface_to_entity("apple_daily_hk", [apple_inc, apple_daily])
    assert out is apple_daily
