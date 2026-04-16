"""Question intent: LLM structured extract + regex/rubric fallback."""

from __future__ import annotations

import json
import os
import re
import time
from typing import Any

import pydantic

from agentloom.contracts import AgentLoomRequest, IntentExtractionMeta, InvestigationRole, QuestionIntent
from agentloom.limits import (
    QUESTION_INTENT_LLM,
    QUESTION_INTENT_LLM_CORRIDOR_ONLY,
    QUESTION_INTENT_MAX_OUTPUT_TOKENS,
    QUESTION_INTENT_MODEL,
)
from agentloom.question_rubric import build_question_rubric
from agentloom.wire_unmarshal import recover_wire_json_text

_SCOPE_TOKEN_STOPWORDS = {
    "the",
    "a",
    "an",
    "is",
    "are",
    "of",
    "in",
    "to",
    "for",
    "and",
    "or",
    "from",
    "by",
    "with",
    "on",
    "at",
    "what",
    "which",
    "how",
    "that",
    "this",
    "it",
    "its",
    "map",
    "identify",
    "surface",
    "likely",
    "strongest",
    "pressure",
    "points",
    "supply",
    "chain",
}

_QUESTION_INTENT_LLM_PROMPT = """You extract QuestionIntent JSON for an investigation supervisor (supply chain graph).

Rules:
- sourceScope / sinkScope: short readable anchors when the question implies a directional corridor (e.g. regions or markets "from X to Y"); otherwise empty strings.
- corridorActive: true only if the user clearly asks to trace, map, or compare flow along such a corridor.
- wantsAlternatives: true if alternatives, parallel routes, or materially different path families are asked.
- wantsOwnership: true if ownership, control, offtake, leverage, or "who controls" is central.
- wantsPolicy: true if regulation, sanctions, export controls, permits, or policy choke points are central.
- wantsAdjacentRegions: true if neighboring markets, regional balance, "other regions", or geographic breadth beyond a single corridor is central.
- wantsCounterNarrative: true if competing interpretations, other perspectives, epistemic uncertainty, or "what else could explain" is central.
- stressesGeopoliticalRisk: true if sanctions, straits/choke points, geopolitical risk, or export-control politics is central (not routine permitting).
- extractionConfidence: your confidence in this extraction (0..1), not factual supply-chain certainty.
- extractionNotes: one short note (<= 200 chars), machine-oriented.

Question:
{question}

Requested roles (JSON hints, may be empty):
{roles_json}

Return exactly one JSON object matching the QuestionIntent schema (populate all fields)."""


def _breadth_axes_from_rubric_and_question(question: str, rubric_ids: set[str]) -> dict[str, bool]:
    q = (question or "").lower()
    return {
        "wants_adjacent_regions": bool(
            "axis_alternative_paths" in rubric_ids
            or re.search(
                r"\b(adjacent|neighboring|neighbouring|neighbou?ring|regional balance|other regions?|wider region)\b",
                q,
            )
        ),
        "wants_counter_narrative": bool(
            re.search(
                r"\b(counter\s*narrative|competing view|other perspective|alternative (?:account|interpretation)|epistemic|what else could)\b",
                q,
            )
        ),
        "stresses_geopolitical_risk": bool(
            "axis_regulatory_choke" in rubric_ids
            or re.search(
                r"\b(sanction|geopolitic|export control|embargo|choke\s*point|strait|tariff war|trade war)\b",
                q,
            )
        ),
    }


def _normalize_scope_token(token: str) -> str:
    text = re.sub(r"[^a-z0-9]+", "", (token or "").lower())
    for suffix in ("ing", "ers", "ies", "ers", "es", "s", "ed", "or", "ion", "al", "an"):
        if len(text) > len(suffix) + 3 and text.endswith(suffix):
            text = text[: -len(suffix)]
            break
    return text


def scope_tokens(text: str) -> list[str]:
    out: list[str] = []
    seen: set[str] = set()
    for raw in re.findall(r"[A-Za-z0-9]+", (text or "").lower()):
        if raw in _SCOPE_TOKEN_STOPWORDS or len(raw) <= 2:
            continue
        normalized = _normalize_scope_token(raw)
        if len(normalized) <= 2 or normalized in _SCOPE_TOKEN_STOPWORDS or normalized in seen:
            continue
        seen.add(normalized)
        out.append(normalized)
    return out


_SCOPE_TAIL_PHRASES = re.compile(
    r"(?i),\s*(likely\s+structured|likely|probably|possibly|in\s+practice|for\s+example)\b.*$"
)


def _sanitize_corridor_scope_text(text: str) -> str:
    """Trim trailing meta-language after geography (plan 1A)."""
    s = (text or "").strip()
    if not s:
        return ""
    s = re.sub(r"(?i)\blikely\s+structured\b.*$", "", s)
    s = re.sub(r"(?i),\s*in\s+practice\b.*$", "", s)
    s = _SCOPE_TAIL_PHRASES.sub("", s)
    return " ".join(s.split())


def _strip_scope_phrase(text: str) -> str:
    cleaned = re.sub(
        r"\b(?:identify|surface|show|map|trace|assess|find|describe|explain)\b.*$",
        "",
        (text or "").strip(),
        flags=re.IGNORECASE,
    )
    cleaned = re.sub(r"^[\s,;:.-]+|[\s,;:.-]+$", "", cleaned)
    cleaned = re.sub(r"^(?:a|an|the)\s+", "", cleaned, flags=re.IGNORECASE)
    return " ".join(cleaned.split())


def regex_operational_corridor_dict(question: str) -> dict[str, Any]:
    """Same shape as legacy _requested_operational_corridor (for reachability compatibility)."""
    q = (question or "").strip()
    if not q:
        return {
            "active": False,
            "sourceScope": "",
            "sinkScope": "",
            "sourceTokens": [],
            "sinkTokens": [],
        }
    source_scope = ""
    sink_scope = ""
    match = re.search(
        r"\bfrom\s+(?P<source>.+?)\s+to\s+(?P<sink>.+?)(?=(?:,|;|\.|\?|\band\s+(?:identify|surface|show|map|trace|assess|find|describe|explain)\b|$))",
        q,
        re.IGNORECASE,
    )
    if match:
        source_scope = _sanitize_corridor_scope_text(_strip_scope_phrase(match.group("source")))
        sink_scope = _sanitize_corridor_scope_text(_strip_scope_phrase(match.group("sink")))
    src_toks = scope_tokens(source_scope)
    snk_toks = scope_tokens(sink_scope)
    garbage = {"structur", "structure", "structured", "practic", "practice"}
    src_toks = [t for t in src_toks if t not in garbage]
    snk_toks = [t for t in snk_toks if t not in garbage]
    return {
        "active": bool(source_scope or sink_scope),
        "sourceScope": source_scope,
        "sinkScope": sink_scope,
        "sourceTokens": src_toks,
        "sinkTokens": snk_toks,
    }


def intent_to_operational_corridor_dict(intent: QuestionIntent) -> dict[str, Any]:
    """Build reachability corridor dict from structured intent."""
    src = (intent.source_scope or "").strip()
    sink = (intent.sink_scope or "").strip()
    active = bool(intent.corridor_active and (src or sink))
    if not src and not sink:
        return {
            "active": False,
            "sourceScope": "",
            "sinkScope": "",
            "sourceTokens": [],
            "sinkTokens": [],
        }
    return {
        "active": active,
        "sourceScope": src,
        "sinkScope": sink,
        "sourceTokens": scope_tokens(src),
        "sinkTokens": scope_tokens(sink),
    }


def explicit_corridor_dict_from_request(request: AgentLoomRequest) -> dict[str, Any] | None:
    """Build operational corridor dict from request-level overrides (plan 1B)."""
    if request.operational_corridor is not None:
        oc = request.operational_corridor
        src = _sanitize_corridor_scope_text(oc.source_scope)
        snk = _sanitize_corridor_scope_text(oc.sink_scope)
        if not src and not snk:
            return None
        active = bool(oc.corridor_active and (src or snk))
        return {
            "active": active,
            "sourceScope": src,
            "sinkScope": snk,
            "sourceTokens": scope_tokens(src),
            "sinkTokens": scope_tokens(snk),
        }
    flat_src = _sanitize_corridor_scope_text(request.corridor_source_scope or "")
    flat_snk = _sanitize_corridor_scope_text(request.corridor_sink_scope or "")
    if not flat_src and not flat_snk:
        return None
    active = bool(request.corridor_active) if request.corridor_active is not None else True
    active = active and bool(flat_src or flat_snk)
    return {
        "active": active,
        "sourceScope": flat_src,
        "sinkScope": flat_snk,
        "sourceTokens": scope_tokens(flat_src),
        "sinkTokens": scope_tokens(flat_snk),
    }


def heuristic_question_intent(
    request: AgentLoomRequest,
    selected_roles: list[InvestigationRole],
) -> QuestionIntent:
    """Deterministic intent from regex corridor + question rubric axes."""
    corridor = regex_operational_corridor_dict(request.question)
    roles = list(selected_roles or request.requested_roles or [])
    rubric = build_question_rubric(request.question, [str(r) for r in roles])
    rid = {item.rubric_id for item in rubric.items}
    bx = _breadth_axes_from_rubric_and_question(request.question, rid)
    return QuestionIntent(
        sourceScope=str(corridor.get("sourceScope") or ""),
        sinkScope=str(corridor.get("sinkScope") or ""),
        corridorActive=bool(corridor.get("active")),
        wantsAlternatives="axis_alternative_paths" in rid,
        wantsOwnership="axis_ownership_leverage" in rid,
        wantsPolicy="axis_regulatory_choke" in rid,
        wantsAdjacentRegions=bx["wants_adjacent_regions"],
        wantsCounterNarrative=bx["wants_counter_narrative"],
        stressesGeopoliticalRisk=bx["stresses_geopolitical_risk"],
        intentSchemaVersion=1,
        extractionNotes="heuristic_regex+rubric",
        extractionConfidence=0.0,
    )


def _default_intent_model() -> str:
    m = (QUESTION_INTENT_MODEL or "").strip()
    if m:
        return m
    return (os.getenv("AGENTLOOM_WORKER_MODEL", "") or "gemini-3.1-flash-lite-preview").strip()


def _try_question_intent_llm(
    request: AgentLoomRequest,
    selected_roles: list[InvestigationRole],
) -> tuple[QuestionIntent | None, str, str]:
    """
    Structured Vertex call. Returns (intent, error_reason, model_id).
    On success error_reason is "".
    """
    if os.getenv("AGENTLOOM_TESTING", "").strip().lower() in ("1", "true", "yes"):
        return None, "testing_skip_llm", ""
    if not QUESTION_INTENT_LLM:
        return None, "intent_llm_disabled", ""
    project = (os.getenv("GOOGLE_CLOUD_PROJECT") or os.getenv("GOOGLE_CLOUD_PROJECT_ID") or "").strip()
    if not project:
        return None, "no_google_cloud_project", ""

    from google.genai import Client
    import google.genai.types as genai_types

    from agentloom import agent as A

    model = _default_intent_model()
    location = (os.getenv("GOOGLE_CLOUD_LOCATION", "global") or "global").strip()
    prompt = _QUESTION_INTENT_LLM_PROMPT.format(
        question=(request.question or "").strip()[:8000],
        roles_json=json.dumps([str(r) for r in selected_roles], ensure_ascii=False),
    )
    config = genai_types.GenerateContentConfig(
        temperature=0,
        response_mime_type="application/json",
        response_schema=QuestionIntent,
        max_output_tokens=QUESTION_INTENT_MAX_OUTPUT_TOKENS,
    )
    client = Client(vertexai=True, project=project, location=location)
    last_err: str = "unknown"
    used_model = model
    fb_holder = [False]
    for _ in range(2):
        try:
            response = A._vertex_generate_content_resilient(
                client,
                model=used_model,
                contents=prompt,
                config=config,
                allow_quota_model_fallback=True,
                used_resource_fallback_holder=fb_holder,
            )
            parsed = getattr(response, "parsed", None)
            if parsed is not None:
                if isinstance(parsed, QuestionIntent):
                    qi = parsed
                elif isinstance(parsed, dict):
                    qi = QuestionIntent.model_validate(parsed)
                else:
                    qi = QuestionIntent.model_validate(
                        parsed.model_dump(mode="json", by_alias=True)  # type: ignore[union-attr]
                    )
            else:
                raw = recover_wire_json_text(A._response_json_text(response))
                qi = QuestionIntent.model_validate_json(raw)
            if qi.intent_schema_version < 1:
                qi = qi.model_copy(update={"intent_schema_version": 1})
            notes = (qi.extraction_notes or "").strip()[:200]
            qi = qi.model_copy(update={"extraction_notes": notes})
            return qi, "", used_model
        except (pydantic.ValidationError, json.JSONDecodeError, ValueError, TypeError) as exc:
            last_err = f"validation:{exc}"
            continue
        except Exception as exc:
            last_err = f"{type(exc).__name__}:{exc}"
            break
    return None, last_err, used_model


def extract_question_intent_bundle(
    request: AgentLoomRequest,
    selected_roles: list[InvestigationRole],
) -> tuple[QuestionIntent, IntentExtractionMeta]:
    """LLM structured extract when enabled; else or on failure: regex + rubric heuristic."""
    t0 = time.perf_counter()
    explicit = explicit_corridor_dict_from_request(request)
    if explicit is not None:
        roles = list(selected_roles or request.requested_roles or [])
        rubric = build_question_rubric(request.question, [str(r) for r in roles])
        rid = {item.rubric_id for item in rubric.items}
        bx = _breadth_axes_from_rubric_and_question(request.question, rid)
        intent = QuestionIntent(
            sourceScope=str(explicit.get("sourceScope") or ""),
            sinkScope=str(explicit.get("sinkScope") or ""),
            corridorActive=bool(explicit.get("active")),
            wantsAlternatives="axis_alternative_paths" in rid,
            wantsOwnership="axis_ownership_leverage" in rid,
            wantsPolicy="axis_regulatory_choke" in rid,
            wantsAdjacentRegions=bx["wants_adjacent_regions"],
            wantsCounterNarrative=bx["wants_counter_narrative"],
            stressesGeopoliticalRisk=bx["stresses_geopolitical_risk"],
            intentSchemaVersion=1,
            extractionNotes="explicit_request_corridor_override",
            extractionConfidence=1.0,
        )
        ms = round((time.perf_counter() - t0) * 1000.0, 3)
        return intent, IntentExtractionMeta(
            intentSource="explicit_corridor",
            intentLatencyMs=ms,
            intentFallbackReason="",
            intentModel="",
        )

    llm_intent, llm_reason, model_used = _try_question_intent_llm(request, selected_roles)
    ms = round((time.perf_counter() - t0) * 1000.0, 3)
    if llm_intent is not None:
        if QUESTION_INTENT_LLM_CORRIDOR_ONLY:
            heur = heuristic_question_intent(request, selected_roles)
            merged = heur.model_copy(
                update={
                    "source_scope": llm_intent.source_scope,
                    "sink_scope": llm_intent.sink_scope,
                    "corridor_active": llm_intent.corridor_active,
                    "extraction_notes": "corridor_llm_plus_heuristic_axes",
                    "extraction_confidence": llm_intent.extraction_confidence,
                }
            )
            return merged, IntentExtractionMeta(
                intentSource="llm",
                intentLatencyMs=ms,
                intentFallbackReason="",
                intentModel=model_used,
            )
        return llm_intent, IntentExtractionMeta(
            intentSource="llm",
            intentLatencyMs=ms,
            intentFallbackReason="",
            intentModel=model_used,
        )
    intent = heuristic_question_intent(request, selected_roles)
    fb = llm_reason or "heuristic_fallback"
    return intent, IntentExtractionMeta(
        intentSource="fallback",
        intentLatencyMs=ms,
        intentFallbackReason=fb,
        intentModel=model_used if QUESTION_INTENT_LLM else "",
    )
