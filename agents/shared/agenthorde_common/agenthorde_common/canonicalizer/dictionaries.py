"""Read-only Firestore access to core horizon dictionaries (optional).

Three collections are consumed:

- ``horizon_product_synonym_pairs`` — product synonyms (``/product/...``)
- ``horizon_product_transform_pairs`` — product-to-product transforms
- ``horizon_gov_synonym_pairs`` — government/regulator synonyms (``/gov/...``)
- ``horizon_synonym_pairs`` — **generic** alias → canonical path across classes
  (orgs/facilities/vessel classes/markets/sites/routes). Matches the Go
  [synonymdict runtime](../../../../backend/internal/horizon/synonymdict/runtime.go).

When Firestore is unavailable (no project id, no credentials), a small
bootstrap seed is applied so unit tests, smoke runs, and fresh environments
still resolve the highest-value acronym/legal-form pairs without hitting the
core. The seed mirrors what should eventually live in
``horizon_synonym_pairs``.
"""

from __future__ import annotations

import logging
from typing import Any

logger = logging.getLogger(__name__)

DEFAULT_SYNONYM_COLLECTION = "horizon_product_synonym_pairs"
DEFAULT_TRANSFORM_COLLECTION = "horizon_product_transform_pairs"
DEFAULT_GOV_SYNONYM_COLLECTION = "horizon_gov_synonym_pairs"
DEFAULT_GENERIC_SYNONYM_COLLECTION = "horizon_synonym_pairs"


# Bootstrap seed for the generic synonym dictionary.
# Keys are lowercased alias surfaces; values are canonical paths.
# Shipped in-code so CI / offline runs resolve the same mappings as production.
# When the core collection ships, this seed stays as the floor.
BOOTSTRAP_GENERIC_SYNONYMS: dict[str, str] = {
    # Semiconductors
    "tsmc": "/org/tw/taiwan-semiconductor-manufacturing-company",
    "taiwan semiconductor manufacturing company": "/org/tw/taiwan-semiconductor-manufacturing-company",
    "taiwan semiconductor manufacturing co": "/org/tw/taiwan-semiconductor-manufacturing-company",
    "taiwan semiconductor": "/org/tw/taiwan-semiconductor-manufacturing-company",
    "asml": "/org/nl/asml-holding",
    "asml holding": "/org/nl/asml-holding",
    "intel": "/org/us/intel",
    "intel corporation": "/org/us/intel",
    "samsung": "/org/kr/samsung-electronics",
    "samsung electronics": "/org/kr/samsung-electronics",
    "smic": "/org/cn/semiconductor-manufacturing-international",
    "semiconductor manufacturing international corporation": "/org/cn/semiconductor-manufacturing-international",
    "globalfoundries": "/org/us/globalfoundries",
    "global foundries": "/org/us/globalfoundries",
    "nvidia": "/org/us/nvidia",
    "amd": "/org/us/advanced-micro-devices",
    "advanced micro devices": "/org/us/advanced-micro-devices",
    # Price reporting agencies
    "platts": "/org/us/s-and-p-global-platts",
    "s&p global platts": "/org/us/s-and-p-global-platts",
    "s and p global platts": "/org/us/s-and-p-global-platts",
    "argus": "/org/uk/argus-media",
    "argus media": "/org/uk/argus-media",
    "opis": "/org/us/opis",
    "icis": "/org/uk/icis",
    "fastmarkets": "/org/uk/fastmarkets",
    # Auto / EV
    "tesla": "/org/us/tesla",
    "tesla inc": "/org/us/tesla",
    # Crude / energy majors (minimal seed for smoke runs)
    "saudi aramco": "/org/sa/saudi-aramco",
    "aramco": "/org/sa/saudi-aramco",
    # Markets / benchmarks
    "dated brent": "/market/int/dated-brent",
    "brent": "/market/int/brent",
    "dubai": "/market/int/dubai-crude",
    "ttf": "/market/eu/ttf",
    "jkm": "/market/int/jkm",
    # Vessel classes — singular canonical form
    "vlccs": "/vessel_class/na/vlcc",
    "vlcc": "/vessel_class/na/vlcc",
    "suezmaxes": "/vessel_class/na/suezmax",
    "suezmax": "/vessel_class/na/suezmax",
    "aframaxes": "/vessel_class/na/aframax",
    "aframax": "/vessel_class/na/aframax",
    "valemaxes": "/vessel_class/na/valemax",
    "valemax": "/vessel_class/na/valemax",
}


class DictionaryRuntime:
    """Loads product synonyms/transforms, gov synonyms, and generic synonyms.

    All Firestore access is optional: when the client cannot be initialized
    (no project id, no credentials, emulator unavailable) the runtime still
    serves the bootstrap seed for ``resolve_generic_synonym``.
    """

    def __init__(self, project_id: str | None) -> None:
        self.project_id = (project_id or "").strip() or None
        self._db: Any = None
        self._synonym_to_canon: dict[str, str] = {}
        self._transform_pairs: set[tuple[str, str]] = set()
        self._transform_kind: dict[tuple[str, str], str] = {}
        self._gov_synonym_to_canon: dict[str, str] = {}
        self._generic_synonym_to_canon: dict[str, str] = dict(BOOTSTRAP_GENERIC_SYNONYMS)
        self._loaded = False
        self.load_error: str | None = None

    def _client(self) -> Any:
        if self._db is not None:
            return self._db
        if not self.project_id:
            return None
        try:
            from google.cloud import firestore  # type: ignore

            self._db = firestore.Client(project=self.project_id)
        except Exception as exc:  # pragma: no cover
            self.load_error = str(exc)
            logger.warning("Firestore client init failed: %s", exc)
            return None
        return self._db

    def refresh(self) -> None:
        db = self._client()
        if db is None:
            self._loaded = True
            return
        try:
            for doc in db.collection(DEFAULT_SYNONYM_COLLECTION).limit(5000).stream():
                d = doc.to_dict() or {}
                if d.get("active") is False:
                    continue
                tenant = d.get("tenantId", "global")
                if tenant not in ("global", self.project_id):
                    continue
                canon = d.get("canonicalPath") or d.get("canonicalCanonicalPath") or ""
                syn = d.get("synonymPath") or ""
                if canon and syn:
                    self._synonym_to_canon[syn.strip().lower()] = canon.strip()
            for doc in db.collection(DEFAULT_TRANSFORM_COLLECTION).limit(5000).stream():
                d = doc.to_dict() or {}
                if d.get("active") is False:
                    continue
                fp = (d.get("fromPath") or "").strip()
                tp = (d.get("toPath") or "").strip()
                rk = (d.get("relationKind") or "transformation").strip().lower()
                if fp and tp:
                    key = (fp.lower(), tp.lower())
                    self._transform_pairs.add(key)
                    self._transform_kind[key] = rk
            for doc in db.collection(DEFAULT_GOV_SYNONYM_COLLECTION).limit(2000).stream():
                d = doc.to_dict() or {}
                if d.get("active") is False:
                    continue
                canon = (d.get("canonicalPath") or "").strip()
                syn = (d.get("synonymPath") or d.get("synonym") or "").strip()
                if canon and syn:
                    self._gov_synonym_to_canon[syn.lower()] = canon
            for doc in db.collection(DEFAULT_GENERIC_SYNONYM_COLLECTION).limit(10000).stream():
                d = doc.to_dict() or {}
                if d.get("active") is False:
                    continue
                tenant = d.get("tenantId", "global")
                if tenant not in ("global", self.project_id):
                    continue
                canon = (d.get("canonicalPath") or "").strip()
                # Generic collection accepts alias as either a surface label
                # (preferred) or an aliasPath; both are indexed lowercased.
                alias = (
                    d.get("aliasSurface")
                    or d.get("aliasPath")
                    or d.get("synonym")
                    or d.get("synonymPath")
                    or ""
                ).strip()
                if canon and alias:
                    self._generic_synonym_to_canon[alias.lower()] = canon
        except Exception as exc:  # pragma: no cover
            self.load_error = str(exc)
            logger.warning("Dictionary refresh failed: %s", exc)
        self._loaded = True

    def resolve_product_synonym(self, path_or_label: str) -> str | None:
        if not self._loaded:
            self.refresh()
        key = path_or_label.strip().lower()
        return self._synonym_to_canon.get(key)

    def lookup_transform(self, from_path: str, to_path: str) -> tuple[bool, str]:
        if not self._loaded:
            self.refresh()
        key = (from_path.strip().lower(), to_path.strip().lower())
        if key in self._transform_pairs:
            return True, self._transform_kind.get(key, "transformation")
        return False, ""

    def resolve_gov_synonym(self, label: str) -> str | None:
        if not self._loaded:
            self.refresh()
        return self._gov_synonym_to_canon.get(label.strip().lower())

    def resolve_generic_synonym(self, label_or_path: str) -> str | None:
        """Resolve an org/fac/market/vessel_class/... alias to its canonical path.

        Accepts raw surface labels (``"TSMC"``), explicit alias paths, or the
        already-canonical path itself (echoed back when present in values).
        Falls back to the bootstrap seed when Firestore is unreachable.
        """
        if not self._loaded:
            self.refresh()
        key = (label_or_path or "").strip().lower()
        if not key:
            return None
        if key in self._generic_synonym_to_canon:
            return self._generic_synonym_to_canon[key]
        # If caller already passed a canonical path, echo it back so callers
        # don't need to branch on "is this already canonical?".
        if key.startswith("/") and key in {v.lower() for v in self._generic_synonym_to_canon.values()}:
            return key
        return None

    def seed_generic_synonym(self, alias: str, canonical_path: str) -> None:
        """Tests / bootstrap: add an alias→canonical mapping in-memory."""
        a = (alias or "").strip().lower()
        c = (canonical_path or "").strip()
        if not a or not c:
            return
        self._generic_synonym_to_canon[a] = c
