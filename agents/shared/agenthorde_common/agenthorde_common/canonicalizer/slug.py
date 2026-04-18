"""Slug generation, ISO2 hints, and legal-suffix / parenthetical normalization.

Shared with core naming policy: the suffix/prefix lists and parenthetical
alias patterns are mirrored from
[backend/internal/horizon/naming/policy.go](../../../../backend/internal/horizon/naming/policy.go)
via `assets/legal_suffixes.json`. Keep in sync when core adds a new form.
"""

from __future__ import annotations

import json
import re
import unicodedata
from functools import lru_cache
from pathlib import Path

from unidecode import unidecode

_ISO2 = re.compile(r"\b([A-Z]{2})\b")

_ASSETS_DIR = Path(__file__).resolve().parent / "assets"
_LEGAL_SUFFIXES_JSON = _ASSETS_DIR / "legal_suffixes.json"


def normalize_surface(s: str) -> str:
    """Lower-cased, accent-stripped, whitespace-collapsed surface.

    Treats ``_`` and surface punctuation (``.``) as token boundaries so
    LLM-emitted snake_case identifiers like ``pra_trendforce`` reduce to
    ``pra trendforce`` and tokenize cleanly against entity surfaces. This
    makes the analyzer-vs-phase1 endpoint reconciliation in phase4
    (legal-suffix strip + token_set_ratio fallback) work even when the
    analyzer prefixes a type-token to the entity name.
    """
    t = (s or "").strip().lower()
    t = unicodedata.normalize("NFKD", t)
    t = "".join(c for c in t if not unicodedata.combining(c))
    t = unidecode(t)
    # Treat snake_case underscore and punctuation as token separators.
    t = t.replace("_", " ").replace(".", " ")
    t = re.sub(r"\s+", " ", t)
    return t.strip()


def slugify(text: str, max_len: int = 48) -> str:
    base = normalize_surface(text)
    base = re.sub(r"[^a-z0-9]+", "-", base)
    base = re.sub(r"-+", "-", base).strip("-")
    if not base:
        base = "unknown"
    return base[:max_len].strip("-")


def extract_iso2_from_text(text: str) -> str | None:
    if not text:
        return None
    m = _ISO2.search(text.upper())
    if m:
        return m.group(1).lower()
    return None


def merge_iso2_hints(*hints: str | None) -> str:
    for h in hints:
        if h and len(h) == 2 and h.isalpha():
            return h.lower()
    return "na"


@lru_cache(maxsize=1)
def _legal_suffix_data() -> dict[str, list[str]]:
    try:
        with _LEGAL_SUFFIXES_JSON.open("r", encoding="utf-8") as fh:
            data = json.load(fh)
    except Exception:
        return {"suffixes": [], "prefixes": [], "parenthetical_patterns": []}
    return {
        "suffixes": list(data.get("suffixes") or []),
        "prefixes": list(data.get("prefixes") or []),
        "parenthetical_patterns": list(data.get("parenthetical_patterns") or []),
    }


_PARENTHETICAL_RE_CACHE: list[re.Pattern[str]] = []


def _parenthetical_regexes() -> list[re.Pattern[str]]:
    if _PARENTHETICAL_RE_CACHE:
        return _PARENTHETICAL_RE_CACHE
    data = _legal_suffix_data()
    patterns = data.get("parenthetical_patterns") or []
    if patterns:
        joined = "|".join(re.escape(p) for p in patterns)
        _PARENTHETICAL_RE_CACHE.append(
            re.compile(rf"\s*\((?:{joined})\s[^)]*\)", flags=re.IGNORECASE)
        )
    _PARENTHETICAL_RE_CACHE.append(
        re.compile(r"\s*\(([A-Za-z0-9&.\-]{2,20})\)\s*$")
    )
    return _PARENTHETICAL_RE_CACHE


def strip_parenthetical_alias(s: str) -> str:
    """Remove ``(formerly …)`` / ``(fka …)`` annotations and trailing ``(TSMC)`` tickers.

    Mirrors [backend/internal/horizon/naming/policy.go](../../../../backend/internal/horizon/naming/policy.go)
    `StripParentheticalAliasAnnotations` + `stripTrailingAcronymParenthetical`.
    """
    out = (s or "").strip()
    if not out:
        return out
    regexes = _parenthetical_regexes()
    for rx in regexes[:-1]:
        out = rx.sub("", out)
    m = regexes[-1].search(out)
    if m:
        inside = (m.group(1) or "").strip()
        if inside and " " not in inside:
            out = regexes[-1].sub("", out)
    return " ".join(out.split())


_NON_ALNUM_SPACE = re.compile(r"\s+")


def _norm_for_suffix(s: str) -> str:
    t = s.lower().strip()
    t = t.replace(",", " ").replace(".", " ")
    return _NON_ALNUM_SPACE.sub(" ", t).strip()


def strip_legal_suffix(s: str) -> str:
    """Strip legal-form suffixes and prefixes iteratively.

    Examples:
        ``"Tesla, Inc."`` → ``"Tesla"``
        ``"Taiwan Semiconductor Manufacturing Company, Ltd."`` → ``"Taiwan Semiconductor Manufacturing"``
        ``"Wingtech Technology Co., Ltd."`` → ``"Wingtech Technology"``
        ``"JSC Kazakhtelecom"`` → ``"Kazakhtelecom"``
        ``"Nexperia B.V."`` → ``"Nexperia"``

    Punctuation is normalized before matching, so ``"B.V."`` and ``"BV"`` are
    both stripped. Returned string preserves original case where possible.
    """
    raw = (s or "").strip()
    if not raw:
        return raw
    cleaned_raw = strip_parenthetical_alias(raw)
    data = _legal_suffix_data()
    suffixes = sorted(
        {_norm_for_suffix(x) for x in data.get("suffixes") or [] if x},
        key=lambda x: -len(x),
    )
    prefixes = sorted(
        {_norm_for_suffix(x) for x in data.get("prefixes") or [] if x},
        key=lambda x: -len(x),
    )

    name = _norm_for_suffix(cleaned_raw)
    if not name:
        return cleaned_raw

    changed = True
    while changed:
        changed = False
        for sfx in suffixes:
            if not sfx:
                continue
            if name == sfx:
                name = ""
                changed = True
                break
            if name.endswith(" " + sfx):
                name = name[: -(len(sfx) + 1)].strip()
                changed = True
                break

    for pfx in prefixes:
        if not pfx:
            continue
        if name.startswith(pfx + " "):
            name = name[len(pfx) + 1 :].strip()
            break

    if not name:
        return cleaned_raw
    return name
