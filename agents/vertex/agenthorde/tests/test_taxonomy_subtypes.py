"""Tests for the identity-carrying subtype normalizers.

The prompt describes `product.subtype`, `evidence.subtype` and
`community.subtype` as identity-carrying: they become a hard segment in the
canonical path. Drift between free-form LLM outputs for the same concept
must collapse to one canonical family, otherwise phase 3 silently splits
fragments and dedup breaks. These tests pin that contract.
"""

from __future__ import annotations

from agenthorde_common.canonicalizer.taxonomy import (
    EVIDENCE_SUBTYPES,
    PRODUCT_FAMILIES,
    normalize_community_subtype,
    normalize_evidence_subtype,
    normalize_product_family,
)


class TestNormalizeProductFamily:
    def test_empty_defaults_to_general(self) -> None:
        assert normalize_product_family(None) == "general"
        assert normalize_product_family("") == "general"
        assert normalize_product_family("   ") == "general"

    def test_exact_enum_values_pass_through(self) -> None:
        for fam in PRODUCT_FAMILIES:
            assert normalize_product_family(fam) == fam

    def test_semiconductor_aliases_collapse_to_chip(self) -> None:
        for raw in (
            "semiconductor",
            "semiconductors",
            "chip",
            "chips",
            "memory",
            "DRAM",
            "NAND",
            "logic",
            "logic_chip",
            "wafer",
            "IC",
            "integrated_circuit",
        ):
            assert normalize_product_family(raw) == "chip", raw

    def test_oil_and_gas_aliases_collapse_to_hydrocarbon(self) -> None:
        for raw in ("hydrocarbons", "crude", "crude oil", "LNG", "LPG", "naphtha", "diesel"):
            assert normalize_product_family(raw) == "hydrocarbon", raw

    def test_vehicle_aliases_collapse(self) -> None:
        for raw in ("vehicles", "car", "cars", "auto", "automotive", "EV", "electric_vehicle"):
            assert normalize_product_family(raw) == "vehicle", raw

    def test_unknown_subtype_falls_back_to_general_not_raw(self) -> None:
        assert normalize_product_family("exotic-llm-family") == "general"
        assert normalize_product_family("weird_stuff") == "general"

    def test_plural_collapses_when_singular_is_enum(self) -> None:
        assert normalize_product_family("batteries") == "battery"
        assert normalize_product_family("components") == "component"
        assert normalize_product_family("containers") == "container"

    def test_case_and_separator_tolerance(self) -> None:
        assert normalize_product_family("Hydrocarbon") == "hydrocarbon"
        assert normalize_product_family("LITHIUM ION") == "battery"
        assert normalize_product_family("refined-metal") == "refined_metal"


class TestNormalizeEvidenceSubtype:
    def test_empty_defaults_to_entity(self) -> None:
        assert normalize_evidence_subtype(None) == "entity"
        assert normalize_evidence_subtype("") == "entity"

    def test_exact_enum_values_pass_through(self) -> None:
        for st in EVIDENCE_SUBTYPES:
            assert normalize_evidence_subtype(st) == st

    def test_news_aliases_collapse_to_quote(self) -> None:
        for raw in ("news", "article", "interview", "comment"):
            assert normalize_evidence_subtype(raw) == "quote", raw

    def test_filing_aliases_collapse(self) -> None:
        assert normalize_evidence_subtype("SEC filing") == "filing"
        assert normalize_evidence_subtype("10k") == "filing"
        assert normalize_evidence_subtype("10Q") == "filing"

    def test_unknown_falls_back_to_other(self) -> None:
        assert normalize_evidence_subtype("weird-new-kind") == "other"


class TestNormalizeCommunitySubtypeContract:
    """Sanity-check: existing normalizer still behaves like its documented contract."""

    def test_family_aliases(self) -> None:
        assert normalize_community_subtype("family") == "dynasty"
        assert normalize_community_subtype("royal house") == "dynasty"
        assert normalize_community_subtype("chaebol") == "dynasty"

    def test_unknown_falls_back(self) -> None:
        assert normalize_community_subtype("weird-subtype") == "key-community"
        assert normalize_community_subtype(None) == "key-community"
