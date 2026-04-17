"""Slug generation and ISO2 hints from context."""

from __future__ import annotations

import re
import unicodedata

from unidecode import unidecode

_ISO2 = re.compile(r"\b([A-Z]{2})\b")


def normalize_surface(s: str) -> str:
    t = (s or "").strip().lower()
    t = unicodedata.normalize("NFKD", t)
    t = "".join(c for c in t if not unicodedata.combining(c))
    t = unidecode(t)
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
