import os
import re
from typing import Any, Dict, List, Optional

from fastapi import FastAPI
from pydantic import BaseModel, Field

try:
    from gliner import GLiNER  # type: ignore
except Exception:  # pragma: no cover
    GLiNER = None


class DocumentInput(BaseModel):
    sourceId: str = ""
    sourceHost: str = ""
    sourceUrl: str = ""
    title: str
    summary: str = ""
    body: str = ""
    url: str = ""
    publishedAt: Optional[str] = None
    language: str = ""
    sourceType: str = ""
    categoryHint: str = ""
    metadata: Dict[str, Any] = Field(default_factory=dict)


class ExtractedEntity(BaseModel):
    surface: str
    normalized: str = ""
    entityType: str
    role: str = ""
    confidence: float = 0.0
    startOffset: int = 0
    endOffset: int = 0
    countryCode: str = ""
    geoHint: str = ""
    linkCandidates: List[str] = Field(default_factory=list)
    attributes: Dict[str, Any] = Field(default_factory=dict)


class EntityExtractionResult(BaseModel):
    entities: List[ExtractedEntity] = Field(default_factory=list)
    raw: Dict[str, Any] = Field(default_factory=dict)


LABELS = [
    "country",
    "city",
    "region",
    "facility",
    "company",
    "government body",
    "port",
    "terminal",
    "pipeline",
    "refinery",
    "fab",
    "chokepoint",
    "route",
    "market",
    "product",
    "program",
]

COUNTRY_MAP = {
    "qatar": "QA",
    "iran": "IR",
    "europe": "EU",
    "germany": "DE",
    "france": "FR",
    "belgium": "BE",
    "netherlands": "NL",
    "united kingdom": "GB",
    "italy": "IT",
    "spain": "ES",
}

ENTITY_PATTERNS = {
    "port": [r"\bport of [A-Z][A-Za-z\- ]+\b", r"\b[A-Z][A-Za-z\- ]+ port\b"],
    "terminal": [r"\b[A-Z][A-Za-z0-9\- ]+ terminal\b", r"\bterminal [A-Z][A-Za-z0-9\- ]+\b"],
    "pipeline": [r"\b[A-Z][A-Za-z0-9\- ]+ pipeline\b"],
    "refinery": [r"\b[A-Z][A-Za-z0-9\- ]+ refinery\b"],
    "fab": [r"\b[A-Z][A-Za-z0-9\- ]+ fab\b", r"\bfab \d+\b"],
    "facility": [r"\b[A-Z][A-Za-z0-9\- ]+ facility\b", r"\b[A-Z][A-Za-z0-9\- ]+ plant\b"],
    "chokepoint": [r"\bStrait of Hormuz\b", r"\bBab el-Mandeb\b", r"\bSuez Canal\b", r"\bStrait of Malacca\b"],
    "market": [r"\b[A-Z][A-Za-z\- ]+ market\b", r"\bLNG market\b", r"\bgas hub\b"],
    "product": [r"\bLiquefied Natural Gas\b", r"\bLNG\b", r"\bcrude oil\b", r"\bnatural gas\b"],
    "route": [r"\b[A-Z][A-Za-z\- ]+ route\b", r"\bshipping lane\b", r"\bsea route\b"],
}


def guess_role(entity_type: str) -> str:
    if entity_type in {"country", "city", "region"}:
        return "place"
    if entity_type in {"facility", "port", "terminal", "pipeline", "refinery", "fab"}:
        return "facility"
    if entity_type in {"route", "chokepoint"}:
        return "route"
    if entity_type in {"company", "government body"}:
        return "actor"
    if entity_type == "market":
        return "market"
    if entity_type == "product":
        return "product"
    return ""


def heuristic_extract(doc: DocumentInput) -> EntityExtractionResult:
    text = "\n".join([doc.title, doc.summary, doc.body]).strip()
    entities: List[ExtractedEntity] = []
    for country_name, code in COUNTRY_MAP.items():
        match = re.search(rf"\b{re.escape(country_name)}\b", text, re.IGNORECASE)
        if match:
            entities.append(
                ExtractedEntity(
                    surface=match.group(0),
                    normalized=country_name.title(),
                    entityType="country",
                    role="place",
                    confidence=0.86,
                    startOffset=match.start(),
                    endOffset=match.end(),
                    countryCode=code,
                    geoHint=country_name.title(),
                )
            )
    for entity_type, patterns in ENTITY_PATTERNS.items():
        for pattern in patterns:
            for match in re.finditer(pattern, text):
                surface = match.group(0).strip()
                entities.append(
                    ExtractedEntity(
                        surface=surface,
                        normalized=surface,
                        entityType=entity_type,
                        role=guess_role(entity_type),
                        confidence=0.73,
                        startOffset=match.start(),
                        endOffset=match.end(),
                        geoHint=surface,
                    )
                )
    return EntityExtractionResult(entities=dedupe_entities(entities), raw={"backend": "rules"})


def dedupe_entities(items: List[ExtractedEntity]) -> List[ExtractedEntity]:
    out: List[ExtractedEntity] = []
    seen = set()
    for item in items:
        key = (item.entityType.lower(), item.surface.lower())
        if key in seen:
            continue
        seen.add(key)
        out.append(item)
    return out


class GLiNERBackend:
    def __init__(self, model_name: str):
        self.model_name = model_name.strip()
        self.model = None
        if GLiNER is not None and self.model_name:
            self.model = GLiNER.from_pretrained(self.model_name)

    def available(self) -> bool:
        return self.model is not None

    def extract(self, doc: DocumentInput) -> Optional[EntityExtractionResult]:
        if self.model is None:
            return None
        text = "\n".join([doc.title, doc.summary, doc.body]).strip()
        predictions = self.model.predict_entities(text, LABELS)
        entities: List[ExtractedEntity] = []
        for pred in predictions:
            label = str(pred.get("label", "")).strip().lower()
            start = int(pred.get("start", 0))
            end = int(pred.get("end", 0))
            surface = str(pred.get("text", "")).strip()
            if not label or not surface:
                continue
            entities.append(
                ExtractedEntity(
                    surface=surface,
                    normalized=surface,
                    entityType=label,
                    role=guess_role(label),
                    confidence=float(pred.get("score", 0.0)),
                    startOffset=start,
                    endOffset=end,
                    countryCode=COUNTRY_MAP.get(surface.lower(), ""),
                    geoHint=surface,
                )
            )
        return EntityExtractionResult(entities=dedupe_entities(entities), raw={"backend": "gliner", "model": self.model_name})


app = FastAPI(title="Horizon Entity Extract", version="1.0.0")
backend_name = os.getenv("MODEL_BACKEND", "rules").strip().lower()
gliner_name = os.getenv("GLINER_MODEL_NAME", "").strip()
gliner_backend = GLiNERBackend(gliner_name) if backend_name == "gliner" else None

if backend_name == "gliner" and (gliner_backend is None or not gliner_backend.available()):
    raise RuntimeError("MODEL_BACKEND=gliner requires GLiNER to be installed and GLINER_MODEL_NAME to be set to a loadable model")


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    return {"ok": True, "backend": backend_name, "modelLoaded": bool(gliner_backend and gliner_backend.available())}


@app.get("/api/v1/healthz")
def api_healthz() -> Dict[str, Any]:
    return healthz()


@app.get("/")
def root() -> Dict[str, Any]:
    return {"service": "entity-extract", "status": "ok"}


@app.post("/v1/entity-extract/extract", response_model=EntityExtractionResult)
def extract(doc: DocumentInput) -> EntityExtractionResult:
    if gliner_backend is not None and gliner_backend.available():
        result = gliner_backend.extract(doc)
        if result is not None:
            return result
    return heuristic_extract(doc)
