"""Surface resolver v2: legal-suffix strip + generic synonym dictionary + e\\d+ guard."""

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.phases.phase4_relations import _resolve_surface_to_entity
from agenthorde_common.canonicalizer.schemas import CanonicalEntity
from agenthorde_common.canonicalizer.slug import (
    strip_legal_suffix,
    strip_parenthetical_alias,
)


def _mk(entity_id: str, t: str, surface: str, path: str) -> CanonicalEntity:
    return CanonicalEntity(
        entity_id=entity_id,
        type=t,
        surface_forms=[surface],
        canonical_path=path,
    )


# --- strip_legal_suffix ---------------------------------------------------


def test_strip_legal_suffix_inc() -> None:
    assert strip_legal_suffix("Tesla, Inc.") == "tesla"


def test_strip_legal_suffix_company_ltd() -> None:
    assert (
        strip_legal_suffix("Taiwan Semiconductor Manufacturing Company, Ltd.")
        == "taiwan semiconductor manufacturing"
    )


def test_strip_legal_suffix_wingtech_co_ltd() -> None:
    assert strip_legal_suffix("Wingtech Technology Co., Ltd.") == "wingtech technology"


def test_strip_legal_suffix_nexperia_bv() -> None:
    assert strip_legal_suffix("Nexperia B.V.") == "nexperia"


def test_strip_legal_suffix_jsc_prefix() -> None:
    assert strip_legal_suffix("JSC Kazakhtelecom") == "kazakhtelecom"


def test_strip_legal_suffix_empty_input_passthrough() -> None:
    assert strip_legal_suffix("") == ""


def test_strip_legal_suffix_no_change_for_plain_name() -> None:
    assert strip_legal_suffix("TSMC") == "tsmc"


# --- strip_parenthetical_alias -------------------------------------------


def test_strip_parenthetical_alias_formerly() -> None:
    assert strip_parenthetical_alias("STC Bank (formerly stc pay)") == "STC Bank"


def test_strip_parenthetical_alias_trailing_acronym() -> None:
    assert (
        strip_parenthetical_alias("Taiwan Semiconductor Manufacturing Company (TSMC)")
        == "Taiwan Semiconductor Manufacturing Company"
    )


def test_strip_parenthetical_keeps_long_explanatory_paren() -> None:
    # Multi-word parentheticals that aren't alias markers are kept.
    assert (
        strip_parenthetical_alias("Acme (makes widgets)")
        == "Acme (makes widgets)"
    )


# --- _resolve_surface_to_entity: e\d+ guard ------------------------------


def test_resolver_rejects_internal_node_id() -> None:
    e = _mk("e1", "org", "Acme", "/org/na/acme")
    # Upstream bug: LLM puts "e12" into src/dst instead of "Acme".
    assert _resolve_surface_to_entity("e12", [e]) is None
    assert _resolve_surface_to_entity("E47", [e]) is None


def test_resolver_e_prefix_with_word_is_kept() -> None:
    # Plain "e" followed by letters (not all digits) must NOT be filtered.
    e = _mk("e1", "org", "Enel", "/org/it/enel")
    assert _resolve_surface_to_entity("Enel", [e]) is e


# --- legal-suffix normalization in resolver ------------------------------


def test_resolver_matches_acronym_parenthetical_to_short_surface() -> None:
    e = _mk(
        "e1",
        "org",
        "Taiwan Semiconductor Manufacturing Company",
        "/org/tw/taiwan-semiconductor-manufacturing-company",
    )
    assert (
        _resolve_surface_to_entity(
            "Taiwan Semiconductor Manufacturing Company (TSMC)", [e]
        )
        is e
    )


def test_resolver_matches_suffix_variant() -> None:
    e = _mk("e1", "org", "Tesla", "/org/us/tesla")
    assert _resolve_surface_to_entity("Tesla, Inc.", [e]) is e


def test_resolver_matches_co_ltd_variant() -> None:
    e = _mk("e1", "org", "Wingtech Technology", "/org/cn/wingtech-technology")
    assert _resolve_surface_to_entity("Wingtech Technology Co., Ltd.", [e]) is e


# --- generic synonym dictionary -----------------------------------------


def test_resolver_uses_synonym_dict_for_acronym() -> None:
    e = _mk(
        "e1",
        "org",
        "Taiwan Semiconductor Manufacturing Company",
        "/org/tw/taiwan-semiconductor-manufacturing-company",
    )
    rt = DictionaryRuntime(None)
    # Bootstrap seed already maps TSMC → this canonical path.
    assert _resolve_surface_to_entity("TSMC", [e], rt) is e


def test_resolver_uses_synonym_dict_platts() -> None:
    e = _mk("e1", "org", "S&P Global Platts", "/org/us/s-and-p-global-platts")
    rt = DictionaryRuntime(None)
    assert _resolve_surface_to_entity("Platts", [e], rt) is e


def test_resolver_without_dict_still_works_for_plain_surface() -> None:
    e = _mk("e1", "org", "Tesla", "/org/us/tesla")
    assert _resolve_surface_to_entity("Tesla", [e], None) is e


def test_resolver_seeded_alias_after_bootstrap() -> None:
    e = _mk("e1", "org", "Globex", "/org/us/globex-corp")
    rt = DictionaryRuntime(None)
    rt.seed_generic_synonym("GBX", "/org/us/globex-corp")
    assert _resolve_surface_to_entity("GBX", [e], rt) is e
