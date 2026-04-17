"""Infer ISO2 from country names / tokens in free text (phase 3 hint enrichment)."""

from __future__ import annotations

import re

# Lowercase keys; longest phrases first for substring scan order
_COUNTRY_PHRASES: list[tuple[str, str]] = [
    ("united arab emirates", "ae"),
    ("u.a.e.", "ae"),
    ("uae", "ae"),
    ("united states of america", "us"),
    ("united states", "us"),
    ("u.s.a.", "us"),
    ("usa", "us"),
    ("united kingdom", "gb"),
    ("u.k.", "gb"),
    ("great britain", "gb"),
    ("south korea", "kr"),
    ("north korea", "kp"),
    ("hong kong", "hk"),
    ("saudi arabia", "sa"),
    ("south africa", "za"),
    ("new zealand", "nz"),
    ("czech republic", "cz"),
    ("the netherlands", "nl"),
    ("the philippines", "ph"),
    ("people's republic of china", "cn"),
    ("prc", "cn"),
    ("russian federation", "ru"),
    ("abu dhabi", "ae"),
    ("dubai", "ae"),
    ("singapore", "sg"),
    ("malaysia", "my"),
    ("indonesia", "id"),
    ("thailand", "th"),
    ("vietnam", "vn"),
    ("japan", "jp"),
    ("china", "cn"),
    ("india", "in"),
    ("germany", "de"),
    ("france", "fr"),
    ("italy", "it"),
    ("spain", "es"),
    ("portugal", "pt"),
    ("netherlands", "nl"),
    ("belgium", "be"),
    ("switzerland", "ch"),
    ("austria", "at"),
    ("poland", "pl"),
    ("sweden", "se"),
    ("norway", "no"),
    ("denmark", "dk"),
    ("finland", "fi"),
    ("ireland", "ie"),
    ("canada", "ca"),
    ("mexico", "mx"),
    ("brazil", "br"),
    ("argentina", "ar"),
    ("chile", "cl"),
    ("peru", "pe"),
    ("colombia", "co"),
    ("australia", "au"),
    ("turkey", "tr"),
    ("israel", "il"),
    ("qatar", "qa"),
    ("kuwait", "kw"),
    ("oman", "om"),
    ("bahrain", "bh"),
    ("egypt", "eg"),
    ("nigeria", "ng"),
    ("kenya", "ke"),
    ("pakistan", "pk"),
    ("bangladesh", "bd"),
    ("ukraine", "ua"),
    ("russia", "ru"),
]


def iso2_from_country_names(text: str) -> str | None:
    if not text:
        return None
    low = text.lower()
    for phrase, iso in _COUNTRY_PHRASES:
        if phrase in low:
            return iso
    return None


def collect_geo_iso2_from_reasoning_graph(claim_texts: list[str], hypothesis_texts: list[str]) -> str | None:
    """Scan claim / hypothesis texts for country mentions (proxy for /geo in same narrative)."""
    blob = " ".join(claim_texts + hypothesis_texts)
    return iso2_from_country_names(blob)
