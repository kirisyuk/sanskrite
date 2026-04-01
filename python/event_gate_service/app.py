import os
import re
import tempfile
from typing import Any, Dict, List, Optional

from fastapi import FastAPI
from pydantic import BaseModel, Field

try:
    import joblib  # type: ignore
except Exception:  # pragma: no cover
    joblib = None

try:
    from google.cloud import storage  # type: ignore
except Exception:  # pragma: no cover
    storage = None


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


class ArticleAssessment(BaseModel):
    isEvent: bool
    eventClass: str = ""
    eventConfidence: float = 0.0
    analysisScore: float = 0.0
    advisoryScore: float = 0.0
    broadScopeScore: float = 0.0
    concreteOutageScore: float = 0.0
    shouldCreateZone: bool
    shouldWarmExisting: bool
    shouldOnlySignal: bool
    rejectionReason: str = ""
    reasons: List[str] = Field(default_factory=list)
    raw: Dict[str, Any] = Field(default_factory=dict)


ANALYSIS_PATTERNS = [
    r"\banalysis\b",
    r"\broundup\b",
    r"\bweekly\b",
    r"\bwhat to know\b",
    r"\bexplainer\b",
    r"\boutlook\b",
    r"\bbriefing\b",
    r"\bthe d brief\b",
    r"\band a bit more\b",
    r"\brewild borders\b",
    r"\benvironment chief\b",
    r"\bin other news\b",
    r"\bwhat happens if\b",
    r"\bmust not be a staging ground\b",
    r"\bcongressional scrutiny\b",
    r"\bmeet the startups\b",
    r"\bmilitary-specific ai\b",
    r"\bmilitary specific ai\b",
    r"\bbuild soft power\b",
    r"\bstrategic agreement\b",
    r"\bto discuss regional security\b",
    r"\boffice attendance\b",
    r"\bworkforce readiness\b",
    r"\bcertification portfolio\b",
    r"\bblueprint\b",
    r"\breview\b",
    r"\banti-ship missile capability\b",
    r"\bmissile capability\b",
    r"\bwhy hedging is no longer enough\b",
]
ADVISORY_PATTERNS = [
    r"\badvisory\b",
    r"\balert\b",
    r"\bwarning\b",
    r"\bcve-\d{4}-\d+\b",
    r"\bcampaign\b",
]
BROAD_SCOPE_PATTERNS = [
    r"\bmiddle east\b",
    r"\bglobal\b",
    r"\bregion\b",
    r"\bmarkets\b",
    r"\bpressure\b",
    r"\bresilience\b",
    r"\bmore than a dozen countries\b",
    r"\bthroughout the middle east\b",
    r"\bdeter invasions\b",
    r"\bnatural border control\b",
]
CAPABILITY_PATTERNS = [
    r"\bcongressional scrutiny\b",
    r"\binvestment deals\b",
    r"\bmeet the startups\b",
    r"\bmilitary-specific ai\b",
    r"\bmilitary specific ai\b",
    r"\bsoft power\b",
    r"\bstrategic agreement\b",
    r"\bmissile capability\b",
    r"\banti-ship missile capability\b",
    r"\bto discuss regional security\b",
    r"\boffice attendance\b",
    r"\bcertification portfolio\b",
    r"\bworkforce readiness\b",
    r"\bblueprint\b",
    r"\breview\b",
    r"\bwhat happens if\b",
    r"\bwhy hedging is no longer enough\b",
    r"\bmust not be a staging ground\b",
    r"\bseeks order suppressing identities\b",
]
EVENT_PATTERNS = [
    r"\battack\b",
    r"\bstrike\b",
    r"\boutage\b",
    r"\bexplosion\b",
    r"\bearthquake\b",
    r"\bflood\b",
    r"\bwildfire\b",
    r"\bshutdown\b",
    r"\bdisruption\b",
]
CONCRETE_OUTAGE_PATTERNS = [
    r"\boutage\b",
    r"\bclosure\b",
    r"\boperations suspended\b",
    r"\bshutdown\b",
    r"\binterruption\b",
    r"\bblackout\b",
]
WARM_CONFLICT_PATTERNS = [
    r"\breinforcements?\b",
    r"\bair defence\b",
    r"\bposture\b",
    r"\bdeployed?\b",
    r"\bescalation\b",
]


def norm(text: str) -> str:
    return re.sub(r"\s+", " ", (text or "").strip().lower())


def score_patterns(text: str, patterns: List[str]) -> float:
    score = 0.0
    for pattern in patterns:
        if re.search(pattern, text, re.IGNORECASE):
            score += 0.2
    return min(score, 1.0)


def rule_assess(doc: DocumentInput) -> ArticleAssessment:
    text = norm(" ".join([doc.title, doc.summary, doc.body, doc.categoryHint]))
    host = norm(doc.sourceHost)
    analysis_score = score_patterns(text, ANALYSIS_PATTERNS)
    advisory_score = score_patterns(text, ADVISORY_PATTERNS)
    broad_scope_score = score_patterns(text, BROAD_SCOPE_PATTERNS)
    capability_score = score_patterns(text, CAPABILITY_PATTERNS)
    event_score = score_patterns(text, EVENT_PATTERNS)
    outage_score = score_patterns(text, CONCRETE_OUTAGE_PATTERNS)
    warm_score = score_patterns(text, WARM_CONFLICT_PATTERNS)

    if "the d brief" in text or "and a bit more" in text:
        analysis_score = min(1.0, analysis_score + 0.6)
        broad_scope_score = min(1.0, broad_scope_score + 0.25)
    if "rewild borders" in text or "deter invasions" in text or "environment chief" in text or "natural border control" in text:
        analysis_score = min(1.0, analysis_score + 0.55)
        broad_scope_score = min(1.0, broad_scope_score + 0.25)
        event_score = max(0.0, event_score - 0.2)
    if any(term in text for term in [
        "must not be a staging ground",
        "what happens if",
        "why hedging is no longer enough",
        "congressional scrutiny",
        "meet the startups",
        "military-specific ai",
        "military specific ai",
        "build soft power",
        "strategic agreement",
        "to discuss regional security",
        "office attendance",
        "workforce readiness",
        "certification portfolio",
        "blueprint",
        "review",
        "anti-ship missile capability",
        "missile capability",
        "seeks order suppressing identities",
    ]):
        analysis_score = min(1.0, analysis_score + 0.45)
        capability_score = min(1.0, capability_score + 0.35)
        event_score = max(0.0, event_score - 0.15)
    if "defenseone.com" in host and "the d brief" in text:
        analysis_score = min(1.0, analysis_score + 0.25)
    if "theguardian.com" in host and ("rewild borders" in text or "environment chief" in text):
        analysis_score = min(1.0, analysis_score + 0.2)
        broad_scope_score = min(1.0, broad_scope_score + 0.15)
    if "brookings.edu" in host and "must not be a staging ground" in text:
        analysis_score = min(1.0, analysis_score + 0.25)
    if any(key in host for key in ["euractiv", "brookings.edu"]):
        capability_score = min(1.0, capability_score + 0.10)

    event_class = "real_event"
    rejection_reason = ""
    reasons: List[str] = []
    should_create_zone = event_score >= 0.2 and analysis_score < 0.4 and advisory_score < 0.4 and broad_scope_score < 0.4 and capability_score < 0.35
    should_warm = warm_score >= 0.2 and event_score < 0.4
    should_only_signal = (event_score >= 0.2 or warm_score >= 0.2) and not should_create_zone and not should_warm
    is_event = should_create_zone or should_warm or should_only_signal

    if analysis_score >= 0.4:
        event_class = "analysis"
        rejection_reason = "analysis_roundup"
        reasons.append("analysis_roundup")
    elif advisory_score >= 0.4 and outage_score < 0.2:
        event_class = "advisory"
        rejection_reason = "advisory_without_outage"
        reasons.append("advisory_without_outage")
    elif broad_scope_score >= 0.4 and event_score < 0.4:
        event_class = "broad_scope"
        rejection_reason = "broad_scope_without_incident"
        reasons.append("broad_scope_without_incident")
    elif not is_event:
        event_class = "noise"
        rejection_reason = "non_event_text"
        reasons.append("non_event_text")

    confidence = min(0.98, 0.45 + event_score * 0.35 + outage_score * 0.15 + warm_score * 0.10)
    if rejection_reason:
        confidence = max(confidence, 0.72)
    return ArticleAssessment(
        isEvent=is_event,
        eventClass=event_class,
        eventConfidence=confidence,
        analysisScore=analysis_score,
        advisoryScore=advisory_score,
        broadScopeScore=broad_scope_score,
        concreteOutageScore=outage_score,
        shouldCreateZone=should_create_zone,
        shouldWarmExisting=should_warm,
        shouldOnlySignal=should_only_signal,
        rejectionReason=rejection_reason,
        reasons=reasons,
        raw={"backend": "rules"},
    )


class SklearnBackend:
    def __init__(self, model_path: str):
        self.model_path = model_path
        self.pipeline = None
        if joblib is not None and model_path and os.path.exists(model_path):
            self.pipeline = joblib.load(model_path)

    def available(self) -> bool:
        return self.pipeline is not None

    def assess(self, doc: DocumentInput) -> Optional[ArticleAssessment]:
        if self.pipeline is None:
            return None
        text = "\n".join([doc.sourceHost, doc.title, doc.summary, doc.body, doc.categoryHint]).strip()
        pred = self.pipeline.predict([text])[0]
        probs = {}
        if hasattr(self.pipeline, "predict_proba"):
            classes = list(self.pipeline.classes_)
            values = self.pipeline.predict_proba([text])[0]
            probs = {classes[idx]: float(values[idx]) for idx in range(len(classes))}
        best = max(probs.values()) if probs else 0.72
        return ArticleAssessment(
            isEvent=pred == "real_event",
            eventClass=str(pred),
            eventConfidence=float(best),
            analysisScore=float(probs.get("analysis", 0.0)),
            advisoryScore=float(probs.get("advisory", 0.0)),
            broadScopeScore=float(probs.get("broad_scope", 0.0)),
            concreteOutageScore=float(probs.get("real_event", 0.0)),
            shouldCreateZone=pred == "real_event",
            shouldWarmExisting=pred == "warm_posture",
            shouldOnlySignal=False,
            rejectionReason="" if pred == "real_event" else str(pred),
            reasons=[f"model:{pred}"],
            raw={"backend": "sklearn", "probabilities": probs},
        )


def download_gcs_model(gcs_uri: str) -> str:
    if storage is None:
        raise RuntimeError("google-cloud-storage is required for MODEL_GCS_URI support")
    if not gcs_uri.startswith("gs://"):
        raise RuntimeError("MODEL_GCS_URI must start with gs://")
    bucket_and_path = gcs_uri[5:]
    bucket_name, _, object_name = bucket_and_path.partition("/")
    if not bucket_name or not object_name:
        raise RuntimeError("MODEL_GCS_URI must include bucket and object path")
    client = storage.Client()
    bucket = client.bucket(bucket_name)
    blob = bucket.blob(object_name)
    fd, local_path = tempfile.mkstemp(prefix="event-gate-", suffix=".joblib")
    os.close(fd)
    blob.download_to_filename(local_path)
    return local_path


app = FastAPI(title="Horizon Event Gate", version="1.0.0")
model_backend = os.getenv("MODEL_BACKEND", "rules").strip().lower()
model_path = os.getenv("MODEL_PATH", "").strip()
model_gcs_uri = os.getenv("MODEL_GCS_URI", "").strip()
if not model_path and os.path.exists("/app/model/event_gate.joblib"):
    model_path = "/app/model/event_gate.joblib"
if model_backend == "sklearn" and not model_path and model_gcs_uri:
    model_path = download_gcs_model(model_gcs_uri)
sk_model = SklearnBackend(model_path) if model_backend == "sklearn" else None

if model_backend == "sklearn" and (sk_model is None or not sk_model.available()):
    raise RuntimeError("MODEL_BACKEND=sklearn requires MODEL_PATH to point to a readable trained model artifact")


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    return {"ok": True, "backend": model_backend, "modelLoaded": bool(sk_model and sk_model.available())}


@app.get("/api/v1/healthz")
def api_healthz() -> Dict[str, Any]:
    return healthz()


@app.get("/")
def root() -> Dict[str, Any]:
    return {"service": "event-gate", "status": "ok"}


@app.post("/v1/event-gate/assess", response_model=ArticleAssessment)
def assess(doc: DocumentInput) -> ArticleAssessment:
    if sk_model is not None and sk_model.available():
        result = sk_model.assess(doc)
        if result is not None:
            return result
    return rule_assess(doc)
