"""Sanctions / export-control regime catalog + phase3 canonicalization."""

from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.phases.phase3_paths import build_canonical_path
from agenthorde_common.canonicalizer.schemas import CanonicalEntity
from agenthorde_common.canonicalizer.taxonomy import (
    SANCTIONS_REGIMES_CATALOG,
    sanctions_regime_canonical_path,
    sanctions_regime_code_for_alias,
)


def _empty_ws() -> WorkingState:
    return WorkingState()


def test_catalog_has_core_regimes() -> None:
    required = {
        "OFAC_SDN",
        "G7_OIL_PRICE_CAP",
        "EU_RUSSIA_OIL_CAP",
        "BIS_EAR_ENTITY_LIST",
        "BIS_FDPR",
        "CHIPS_ACT_GUARDRAILS",
    }
    assert required.issubset(SANCTIONS_REGIMES_CATALOG.keys())


def test_alias_lookup_ofac_sdn() -> None:
    assert sanctions_regime_code_for_alias("OFAC SDN") == "OFAC_SDN"
    assert sanctions_regime_code_for_alias("specially designated nationals") == "OFAC_SDN"
    assert sanctions_regime_code_for_alias("SDN list") == "OFAC_SDN"


def test_alias_lookup_g7_price_cap() -> None:
    assert sanctions_regime_code_for_alias("G7 oil price cap") == "G7_OIL_PRICE_CAP"
    assert sanctions_regime_code_for_alias("Russian oil price cap") == "G7_OIL_PRICE_CAP"
    assert sanctions_regime_code_for_alias("price cap coalition") == "G7_OIL_PRICE_CAP"


def test_alias_lookup_bis_ear_entity_list() -> None:
    assert sanctions_regime_code_for_alias("Entity List") == "BIS_EAR_ENTITY_LIST"
    assert sanctions_regime_code_for_alias("BIS Entity List") == "BIS_EAR_ENTITY_LIST"


def test_alias_lookup_fdpr() -> None:
    assert sanctions_regime_code_for_alias("FDPR") == "BIS_FDPR"
    assert sanctions_regime_code_for_alias("foreign direct product rule") == "BIS_FDPR"


def test_alias_lookup_unknown_returns_empty() -> None:
    assert sanctions_regime_code_for_alias("some random label") == ""
    assert sanctions_regime_code_for_alias("") == ""


def test_canonical_path_lookup() -> None:
    assert sanctions_regime_canonical_path("OFAC_SDN") == "/regime/us/ofac-sdn"
    assert sanctions_regime_canonical_path("UNKNOWN") == ""


def test_phase3_normalizes_known_regime_surface() -> None:
    ent = CanonicalEntity(
        entity_id="e0",
        type="regime",
        subtype=None,
        surface_forms=["G7 oil price cap"],
        canonical_path="",
    )
    ws = _empty_ws()
    path = build_canonical_path(ent, ws, None)
    assert path == "/regime/int/g7-oil-price-cap"


def test_phase3_falls_back_for_unknown_regime_surface() -> None:
    ent = CanonicalEntity(
        entity_id="e0",
        type="regime",
        subtype=None,
        surface_forms=["Obscure Regional Export Order"],
        canonical_path="",
    )
    ws = _empty_ws()
    path = build_canonical_path(ent, ws, None)
    # Unknown → falls back to generic /regime/{iso2}/{slug} rule.
    assert path.startswith("/regime/")
    assert "obscure" in path


def test_alias_lookup_rejects_mismatched_iso2_hint() -> None:
    # "chips act" on its own collapses into US CHIPS_ACT_GUARDRAILS via the
    # generic alias; but when upstream has inferred iso2_hint="eu" for the
    # surrounding entity (e.g. "EU Chips Act"), the catalog must refuse the
    # match so the regime stays on the EU jurisdiction path. This is the
    # counterpart to the phase-2 jurisdiction gate — same hint, two layers.
    assert sanctions_regime_code_for_alias("chips act") == "CHIPS_ACT_GUARDRAILS"
    assert sanctions_regime_code_for_alias("chips act", iso2_hint="us") == "CHIPS_ACT_GUARDRAILS"
    assert sanctions_regime_code_for_alias("chips act", iso2_hint="eu") == ""
    assert sanctions_regime_code_for_alias("chips and science act", iso2_hint="eu") == ""


def test_alias_lookup_accepts_matching_iso2_hint() -> None:
    # When the hint matches the regime's jurisdiction, the alias still resolves.
    assert sanctions_regime_code_for_alias("entity list", iso2_hint="us") == "BIS_EAR_ENTITY_LIST"
    assert sanctions_regime_code_for_alias("eu dual-use", iso2_hint="eu") == "EU_DUAL_USE"
    assert sanctions_regime_code_for_alias("uk financial sanctions", iso2_hint="uk") == "UK_OFSI"


def test_phase3_regime_path_respects_iso2_hint() -> None:
    # A /regime entity whose surface is the generic "chips act" but whose
    # iso2 hint is "eu" must NOT collapse into the US catalog path. It falls
    # back to the generic /regime/eu/{slug} rule so phase2's jurisdiction gate
    # can keep it separate from the real US CHIPS_ACT_GUARDRAILS entity.
    from agenthorde_common.canonicalizer.schemas import ContextHints

    ent = CanonicalEntity(
        entity_id="e0",
        type="regime",
        subtype=None,
        surface_forms=["chips act"],
        canonical_path="",
        context_hints=ContextHints(iso2_hint="eu"),
    )
    ws = _empty_ws()
    path = build_canonical_path(ent, ws, None)
    assert path.startswith("/regime/eu/")
    assert path != "/regime/us/chips-act-guardrails"
