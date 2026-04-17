"""Read-only Firestore access to core horizon dictionaries (optional)."""

from __future__ import annotations

import logging
from typing import Any

logger = logging.getLogger(__name__)

DEFAULT_SYNONYM_COLLECTION = "horizon_product_synonym_pairs"
DEFAULT_TRANSFORM_COLLECTION = "horizon_product_transform_pairs"
DEFAULT_GOV_SYNONYM_COLLECTION = "horizon_gov_synonym_pairs"


class DictionaryRuntime:
    """Loads product synonyms/transforms and gov synonyms from Firestore (optional)."""

    def __init__(self, project_id: str | None) -> None:
        self.project_id = (project_id or "").strip() or None
        self._db: Any = None
        self._synonym_to_canon: dict[str, str] = {}
        self._transform_pairs: set[tuple[str, str]] = set()
        self._transform_kind: dict[tuple[str, str], str] = {}
        self._gov_synonym_to_canon: dict[str, str] = {}
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
