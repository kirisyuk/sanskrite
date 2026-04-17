"""Roll product surfaces up to class level (strip SKU-ish suffixes)."""

from __future__ import annotations

import re

# Trailing / parenthetical suffixes removed in order (longest / most specific first).
_VARIANT_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (re.compile(r"\s+long\s+range\b", re.I), ""),
    (re.compile(r"\s+standard\s+range\b", re.I), ""),
    (re.compile(r"\s+performance\b", re.I), ""),
    (re.compile(r"\s+plaid\b", re.I), ""),
    (re.compile(r"\s+pro\s+max\b", re.I), ""),
    (re.compile(r"\s+pro\b", re.I), ""),
    (re.compile(r"\s+max\b", re.I), ""),
    (re.compile(r"\s+ultra\b", re.I), ""),
    (re.compile(r"\s+mini\b", re.I), ""),
    (re.compile(r"\s+awd\b", re.I), ""),
    (re.compile(r"\s+rwd\b", re.I), ""),
    (re.compile(r"\s+4x4\b", re.I), ""),
    (re.compile(r"\s+\(?20[0-9]{2}\)?$", re.I), ""),
    (re.compile(r"\s+\(?1[4-9]\)?$", re.I), ""),  # iPhone 17, 16, ...
)


def rollup_product_surface(surface: str) -> tuple[str, str | None]:
    """
    Return (class_level_surface, original_surface_if_changed).
    If nothing stripped, second value is None.
    """
    s = (surface or "").strip()
    if not s:
        return s, None
    original = s
    prev = None
    while prev != s:
        prev = s
        for pat, repl in _VARIANT_PATTERNS:
            s = pat.sub(repl, s).strip()
    s = re.sub(r"\s+", " ", s).strip()
    if s != original:
        return s, original
    return s, None
