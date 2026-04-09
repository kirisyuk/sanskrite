from __future__ import annotations

import asyncio
import json
import logging
import os
import re
from collections.abc import AsyncGenerator
from typing import Any, Callable, Literal

from google import genai
from google.adk.agents import BaseAgent, LlmAgent, LoopAgent, SequentialAgent
from google.adk.agents.callback_context import CallbackContext
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event, EventActions
from google.adk.planners import BuiltInPlanner
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.tools import google_search
from google.genai import types as genai_types
from pydantic import BaseModel, Field

APP_NAME = "vertex_harvest_portfolio_agent"
USER_ID = "vertex-harvest-runner"
SESSION_ID = "default"
WORKER_MODEL = "gemini-3-flash-preview"
CRITIC_MODEL = "gemini-3-flash-preview"
MAX_SEARCH_ITERATIONS = 0
TraceFn = Callable[[str, str, Any], None]


class SearchQuery(BaseModel):
    search_query: str = Field(description="Targeted web search query.")


class Feedback(BaseModel):
    grade: Literal["pass", "fail"] = Field(description="Pass if evidence is good enough, fail otherwise.")
    comment: str = Field(description="Critical review of the current research quality.")
    follow_up_queries: list[SearchQuery] | None = Field(
        default=None,
        description="Specific follow-up queries needed to close evidence gaps.",
    )


def collect_research_sources_callback(callback_context: CallbackContext) -> None:
    session = callback_context._invocation_context.session
    url_to_short_id = callback_context.state.get("url_to_short_id", {})
    sources = callback_context.state.get("sources", {})
    id_counter = len(url_to_short_id) + 1
    for event in session.events:
        if not (event.grounding_metadata and event.grounding_metadata.grounding_chunks):
            continue
        chunks_info = {}
        for idx, chunk in enumerate(event.grounding_metadata.grounding_chunks):
            if not chunk.web:
                continue
            url = chunk.web.uri
            title = chunk.web.title if chunk.web.title and chunk.web.title != chunk.web.domain else chunk.web.domain
            if url not in url_to_short_id:
                short_id = f"src-{id_counter}"
                url_to_short_id[url] = short_id
                sources[short_id] = {
                    "short_id": short_id,
                    "title": title,
                    "url": url,
                    "domain": chunk.web.domain,
                    "supported_claims": [],
                }
                id_counter += 1
            chunks_info[idx] = url_to_short_id[url]
        if event.grounding_metadata.grounding_supports:
            for support in event.grounding_metadata.grounding_supports:
                confidence_scores = support.confidence_scores or []
                chunk_indices = support.grounding_chunk_indices or []
                for i, chunk_idx in enumerate(chunk_indices):
                    if chunk_idx in chunks_info:
                        short_id = chunks_info[chunk_idx]
                        confidence = confidence_scores[i] if i < len(confidence_scores) else 0.5
                        text_segment = support.segment.text if support.segment else ""
                        sources[short_id]["supported_claims"].append(
                            {
                                "text_segment": text_segment,
                                "confidence": confidence,
                            }
                        )
    callback_context.state["url_to_short_id"] = url_to_short_id
    callback_context.state["sources"] = sources


class EscalationChecker(BaseAgent):
    def __init__(self, name: str):
        super().__init__(name=name)

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        evaluation_result = ctx.session.state.get("research_evaluation")
        if evaluation_result and evaluation_result.get("grade") == "pass":
            logging.info("[%s] evidence quality passed, stopping refinement loop", self.name)
            yield Event(author=self.name, actions=EventActions(escalate=True))
        else:
            yield Event(author=self.name)


harvest_plan_generator = LlmAgent(
    model=WORKER_MODEL,
    name="harvest_plan_generator",
    description="Creates a focused evidence acquisition plan for an entity dossier.",
    instruction="""
    You are a consulting research strategist for entity harvesting.
    Build a short plan to research the target entity and produce a validated executive dossier.

    Rules:
    - Output 5-7 bullets.
    - Prefix each bullet with either [RESEARCH] or [DELIVERABLE].
    - [RESEARCH] bullets must focus on evidence acquisition: ownership, operating assets, strategic role, counterparties, reports, risk events.
    - [DELIVERABLE] bullets must describe final outputs to synthesize after research.
    - Prefer goals that can surface official pages, reports, investor materials, asset pages, filings, and high-signal trade press.
    - Do not answer the topic itself. Produce the plan only.
    """,
    output_key="harvest_plan",
)

evidence_collector = LlmAgent(
    model=WORKER_MODEL,
    name="evidence_collector",
    description="Performs the first evidence gathering pass for the entity.",
    planner=BuiltInPlanner(thinking_config=genai_types.ThinkingConfig(include_thoughts=True)),
    instruction="""
    You are a consulting-grade evidence collector.

    Inputs:
    - Harvest Plan: {harvest_plan}

    Task:
    1. Execute every [RESEARCH] goal in the harvest plan.
    2. For each goal, generate multiple targeted search queries and run them with google_search.
    3. Produce a dense research memo that captures:
       - what the entity is and why it matters
       - ownership and control
       - core assets / operating footprint
       - counterparties / partners / suppliers / infrastructure dependencies
       - report-like and official sources found
       - major risks and unresolved evidence gaps
    4. Explicitly mention promising official/report URLs when found.

    Output:
    - A structured research memo in markdown.
    - Do not produce final JSON yet.
    """,
    tools=[google_search],
    output_key="research_findings",
    after_agent_callback=collect_research_sources_callback,
)

research_evaluator = LlmAgent(
    model=CRITIC_MODEL,
    name="research_evaluator",
    description="Critically evaluates evidence quality and coverage gaps.",
    instruction="""
    You are a strict evidence reviewer for an entity dossier.

    Evaluate the current `research_findings` against this quality bar:
    - at least one usable official source or operator/asset page
    - evidence for ownership/control
    - evidence for core assets / operations / infrastructure role
    - evidence for at least one major commercial or geopolitical dependency
    - if available, report-like material, investor page, sustainability page, filing, or a clearly stated gap

    Assign:
    - pass: if evidence is coherent and good enough to synthesize a strong preliminary dossier
    - fail: if important gaps remain

    If fail, provide 4-6 very specific follow-up queries targeting the missing evidence.
    Your response must be a single raw JSON object matching the Feedback schema.
    """,
    output_schema=Feedback,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="research_evaluation",
)

enhanced_search_executor = LlmAgent(
    model=WORKER_MODEL,
    name="enhanced_search_executor",
    description="Executes refinement searches and improves the research memo.",
    planner=BuiltInPlanner(thinking_config=genai_types.ThinkingConfig(include_thoughts=True)),
    instruction="""
    You are an evidence refinement agent.

    Inputs:
    - Current research findings: {research_findings}
    - Evaluation feedback: {research_evaluation}

    Task:
    1. Execute every follow-up query from `research_evaluation.follow_up_queries` with google_search.
    2. Improve and extend the existing research findings.
    3. Specifically try to close missing official/report/asset/dependency gaps.
    4. Preserve already useful findings and append stronger corroboration.

    Output:
    - A new complete research memo replacing the old `research_findings`.
    """,
    tools=[google_search],
    output_key="research_findings",
    after_agent_callback=collect_research_sources_callback,
)

harvest_composer = LlmAgent(
    model=CRITIC_MODEL,
    name="harvest_composer",
    include_contents="none",
    description="Composes the final structured harvest dossier in project JSON format.",
    instruction="""
    Transform the evidence into a consulting-grade entity dossier.

    Inputs:
    - Harvest Plan: {harvest_plan}
    - Research Findings: {research_findings}
    - Source Registry: {sources}

    Return exactly one JSON object and no prose outside JSON.

    Required top-level keys:
    - entity_name
    - entity_canonical_path
    - entity_type
    - executive_brief
    - consulting_grade_portfolio
    - key_judgments
    - related_entities
    - materials
    - evidence_gaps

    consulting_grade_portfolio must contain:
    - strategic_role
    - operating_profile
    - ownership_and_control
    - commercial_relevance
    - risk_vectors
    - monitoring_recommendations

    key_judgments must contain:
    - statement
    - implication
    - confidence
    - supporting_material_titles
    - supporting_material_urls

    related_entities must contain:
    - name
    - role
    - why_relevant
    - canonical_path_hint

    materials must contain 6-10 items with:
    - title
    - url
    - publisher
    - document_type
    - source_role
    - priority
    - evidence_strength
    - why_it_matters
    - access_notes
    - cross_references

    evidence_gaps must contain:
    - gap
    - why_it_matters
    - recommended_next_step

    Critical rules:
    - Use URLs only from `Source Registry` unless a URL is explicitly present in `research_findings` and clearly supported.
    - Prefer official pages, asset pages, report-like materials, filings, and high-signal trade press.
    - If report-grade sources are missing, say so in evidence_gaps instead of hallucinating them.
    - Keep the executive brief concise, dense, and decision-useful.
    - Keep materials aligned with real evidence, not wishful placeholders.
    """,
    output_key="final_harvest_json",
)

root_agent = SequentialAgent(
    name="harvest_portfolio_workflow",
    description="Runs a planned, iterative evidence harvest and returns a structured entity dossier.",
    sub_agents=[
        harvest_plan_generator,
        evidence_collector,
        LoopAgent(
            name="iterative_evidence_refinement_loop",
            max_iterations=MAX_SEARCH_ITERATIONS,
            sub_agents=[
                research_evaluator,
                EscalationChecker(name="evidence_escalation_checker"),
                enhanced_search_executor,
            ],
        ),
        harvest_composer,
    ],
)


async def _run_prompt_async(prompt: str) -> str:
    session_service = InMemorySessionService()
    await session_service.create_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    runner = Runner(agent=root_agent, app_name=APP_NAME, session_service=session_service)
    content = genai_types.Content(role="user", parts=[genai_types.Part(text=prompt)])
    events = runner.run_async(user_id=USER_ID, session_id=SESSION_ID, new_message=content)
    final_text = ""
    async for event in events:
        if event.is_final_response() and event.content and event.content.parts:
            final_text = "".join(part.text or "" for part in event.content.parts if hasattr(part, "text"))
    return final_text


def run_portfolio_search(
    prompt: str,
    *,
    project_id: str,
    location: str = "global",
    trace_fn: TraceFn | None = None,
) -> dict:
    _emit_trace(trace_fn, "portfolio_search.start", "running", {"location": location})
    _emit_trace(trace_fn, "portfolio_search.prompt", "ok", {"prompt_preview": prompt[:2000]})
    plan = _generate_text(
        prompt=(
            "Create a compact evidence acquisition plan for this entity harvest request.\n"
            "Output 5-7 bullets. Prefix each bullet with [RESEARCH] or [DELIVERABLE].\n\n"
            f"REQUEST:\n{prompt}"
        ),
        model=WORKER_MODEL,
        project_id=project_id,
        location=location,
    )
    _emit_trace(trace_fn, "portfolio_search.plan", "ok", {"plan": plan})

    findings, sources = _generate_text_with_grounding(
        prompt=(
            "You are a nice-depth entity evidence collector.\n"
            "Using Google Search, execute this entity harvest request and produce a compact evidence slate.\n"
            "Keep it concise and decision-useful.\n"
            "Output markdown with these sections only:\n"
            "1. Entity snapshot\n"
            "2. Ownership and operating footprint\n"
            "3. Best source candidates by type\n"
            "4. Material gaps\n"
            "Limit yourself to the most decision-relevant facts and likely fetchable sources.\n\n"
            f"HARVEST PLAN:\n{plan}\n\n"
            f"REQUEST:\n{prompt}"
        ),
        model=WORKER_MODEL,
        project_id=project_id,
        location=location,
    )
    _emit_trace(
        trace_fn,
        "portfolio_search.initial_findings",
        "ok",
        {
            "findings": findings,
            "source_count": len(sources),
            "sources": _source_list_for_prompt(sources),
        },
    )
    _emit_trace(trace_fn, "portfolio_search.nice_depth_mode", "ok", {"refinement_loop": "skipped"})

    final_payload = _generate_json(
        prompt=(
            "Transform the evidence into a consulting-grade entity dossier.\n"
            "Return exactly one JSON object and no prose outside JSON.\n"
            "Required top-level keys: entity_name, entity_canonical_path, entity_type, executive_brief, consulting_grade_portfolio, key_judgments, related_entities, materials, evidence_gaps.\n"
            "consulting_grade_portfolio must contain: strategic_role, operating_profile, ownership_and_control, commercial_relevance, risk_vectors, monitoring_recommendations.\n"
            "key_judgments must contain: statement, implication, confidence, supporting_material_titles, supporting_material_urls.\n"
            "materials must contain: title, url, publisher, document_type, source_role, priority, evidence_strength, why_it_matters, access_notes, cross_references.\n"
            "evidence_gaps must contain: gap, why_it_matters, recommended_next_step.\n"
            "Use URLs only from SOURCES unless a URL appears explicitly in RESEARCH FINDINGS and is strongly supported.\n"
            "Prefer official/operator/report/asset sources over generic summaries. If a report is missing, record a gap instead of inventing it.\n\n"
            f"HARVEST PLAN:\n{plan}\n\n"
            f"REQUEST:\n{prompt}\n\n"
            f"RESEARCH FINDINGS:\n{findings}\n\n"
            f"SOURCES:\n{json.dumps(_source_list_for_prompt(sources)[:10], ensure_ascii=False, indent=2)}"
        ),
        model=CRITIC_MODEL,
        project_id=project_id,
        location=location,
    )
    _emit_trace(trace_fn, "portfolio_search.final_payload", "ok", final_payload)
    return final_payload


def run_material_rescue_search(
    prompt: str,
    *,
    project_id: str,
    location: str = "global",
    trace_fn: TraceFn | None = None,
) -> dict:
    _emit_trace(trace_fn, "material_rescue.start", "running", {"prompt_preview": prompt[:2000]})
    client = genai.Client(vertexai=True, project=project_id, location=location)
    response = client.models.generate_content(
        model=WORKER_MODEL,
        contents=prompt,
        config=genai_types.GenerateContentConfig(
            temperature=0.2,
            response_mime_type="application/json",
            tools=[genai_types.Tool(google_search=genai_types.GoogleSearch())],
        ),
    )
    payload = extract_json_object(response.text)
    _emit_trace(trace_fn, "material_rescue.payload", "ok", payload)
    return payload


def _generate_text(*, prompt: str, model: str, project_id: str, location: str) -> str:
    client = genai.Client(vertexai=True, project=project_id, location=location)
    response = client.models.generate_content(
        model=model,
        contents=prompt,
        config=genai_types.GenerateContentConfig(temperature=0.2),
    )
    return response.text or ""


def _generate_json(*, prompt: str, model: str, project_id: str, location: str) -> dict:
    client = genai.Client(vertexai=True, project=project_id, location=location)
    response = client.models.generate_content(
        model=model,
        contents=prompt,
        config=genai_types.GenerateContentConfig(
            temperature=0.2,
            response_mime_type="application/json",
        ),
    )
    return extract_json_object(response.text)


def _generate_text_with_grounding(
    *,
    prompt: str,
    model: str,
    project_id: str,
    location: str,
) -> tuple[str, dict]:
    client = genai.Client(vertexai=True, project=project_id, location=location)
    response = client.models.generate_content(
        model=model,
        contents=prompt,
        config=genai_types.GenerateContentConfig(
            temperature=0.2,
            tools=[genai_types.Tool(google_search=genai_types.GoogleSearch())],
        ),
    )
    return response.text or "", _extract_sources_from_response(response)


def _extract_sources_from_response(response: genai_types.GenerateContentResponse) -> dict:
    sources: dict[str, dict] = {}
    if not response.candidates:
        return sources
    counter = 1
    for candidate in response.candidates:
        grounding_metadata = getattr(candidate, "grounding_metadata", None)
        if not grounding_metadata or not grounding_metadata.grounding_chunks:
            continue
        chunks_info = {}
        for idx, chunk in enumerate(grounding_metadata.grounding_chunks):
            if not chunk.web:
                continue
            short_id = f"src-{counter}"
            counter += 1
            title = chunk.web.title if chunk.web.title and chunk.web.title != chunk.web.domain else chunk.web.domain
            sources[short_id] = {
                "short_id": short_id,
                "title": title,
                "url": chunk.web.uri,
                "domain": chunk.web.domain,
                "supported_claims": [],
            }
            chunks_info[idx] = short_id
        for support in grounding_metadata.grounding_supports or []:
            indices = support.grounding_chunk_indices or []
            scores = support.confidence_scores or []
            for i, chunk_idx in enumerate(indices):
                short_id = chunks_info.get(chunk_idx)
                if not short_id:
                    continue
                sources[short_id]["supported_claims"].append(
                    {
                        "text_segment": support.segment.text if support.segment else "",
                        "confidence": scores[i] if i < len(scores) else 0.5,
                    }
                )
    return sources


def _merge_sources(existing: dict, new: dict) -> dict:
    merged = dict(existing)
    existing_by_url = {item.get("url"): key for key, item in merged.items()}
    next_index = len(merged) + 1
    for item in new.values():
        url = item.get("url")
        if not url:
            continue
        if url in existing_by_url:
            merged[existing_by_url[url]]["supported_claims"].extend(item.get("supported_claims", []))
            continue
        short_id = f"src-{next_index}"
        next_index += 1
        merged[short_id] = {**item, "short_id": short_id}
        existing_by_url[url] = short_id
    return merged


def _source_list_for_prompt(sources: dict) -> list[dict]:
    out = []
    for item in sources.values():
        claims = item.get("supported_claims", [])
        avg_conf = sum(claim.get("confidence", 0.0) for claim in claims) / len(claims) if claims else 0.0
        out.append(
            {
                "short_id": item.get("short_id"),
                "title": item.get("title"),
                "url": item.get("url"),
                "domain": item.get("domain"),
                "claim_count": len(claims),
                "avg_confidence": round(avg_conf, 3),
                "sample_claims": [claim.get("text_segment", "") for claim in claims[:3]],
            }
        )
    out.sort(key=lambda item: (item["claim_count"], item["avg_confidence"]), reverse=True)
    return out


def _emit_trace(trace_fn: TraceFn | None, stage: str, status: str, payload: Any) -> None:
    if trace_fn is None:
        return
    try:
        trace_fn(stage, status, payload)
    except Exception:
        logging.exception("trace_fn failed for stage %s", stage)


def extract_json_object(raw: str) -> dict:
    text = raw.strip()
    code_fence_match = re.search(r"```(?:json)?\s*(\{.*\})\s*```", text, re.DOTALL)
    if code_fence_match:
        text = code_fence_match.group(1).strip()
    start = text.find("{")
    end = text.rfind("}")
    if start == -1 or end == -1 or end <= start:
        raise ValueError(f"agent did not return JSON: {raw[:500]}")
    return json.loads(text[start : end + 1])
