"""Deterministic metric unit normalization and directional clamping (phase 6)."""

from __future__ import annotations

import re
from typing import Any


def normalize_metric_value(
    value: float | int,
    unit: str,
    *,
    directional: bool,
) -> tuple[float | int, str, str]:
    """
    Returns (value, unit, kind) where kind is magnitude | directional.
    """
    u = (unit or "").strip().lower().replace(" ", "")
    if isinstance(value, (int, float)):
        v = float(value)
    else:
        try:
            v = float(str(value).strip().replace(",", ""))
        except (TypeError, ValueError):
            v = 0.0

    if directional or u in ("direction", "directional", "corr", "correlation"):
        vv = max(-1.0, min(1.0, v))
        return vv, u or "directional", "directional"

    # Gas volumes: mcf = Mcf = kcf = 1000 cubic feet (oil & gas convention).
    # bcf = 10^9 cf = 10^6 mcf.
    if u in ("kcf", "mcf", "kscf", "mscf", "thousandscf"):
        return v, "mcf", "magnitude"
    if u == "bcf":
        return v * 1_000_000.0, "mcf", "magnitude"

    # barrels oil -> tonnes rough (6.5-7.9 bbl/t; use 7.33)
    if u in ("bbl", "barrel", "barrels"):
        tonnes = v / 7.33
        return tonnes, "metric_tonnes", "magnitude"

    if u in ("percent", "pct", "%"):
        return v, "percent", "magnitude"

    if u in ("usd", "dollars", "dollar", "usdm"):
        if u == "usdm":
            return v * 1_000_000.0, "usd", "magnitude"
        return v, "usd", "magnitude"

    return v, unit or "dimensionless", "magnitude"


def score_edge_for_ownership_metric(
    edge: Any,
    entity_ids_hint: set[str],
) -> int:
    """Higher = better match for stake_pct style metrics."""
    score = 0
    if getattr(edge, "relation", "") in ("subsidiary_of", "beneficially_owns", "joint_venture_with"):
        score += 10
    if entity_ids_hint:
        if getattr(edge, "source_fragment_id", "") in entity_ids_hint:
            score += 5
        if getattr(edge, "target_fragment_id", "") in entity_ids_hint:
            score += 5
    return score
