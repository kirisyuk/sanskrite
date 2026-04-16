from __future__ import annotations

import json
import os
import re
import uuid
from typing import Any

from google import genai
from google.genai import types as genai_types
from pydantic import ValidationError

from agenthorde.config import STATE_PACK_KEY, STATE_TOOL_LEDGER_KEY, WORKER_MODEL
from agenthorde.schemas.state import ApplyPatchRecord, SessionPack
from agenthorde.schemas.tools_io import (
    ApplyToolInput,
    ApplyToolOutput,
    KnowledgeSearchOutput,
    RetrieveToolInput,
    RetrieveToolOutput,
    ToolEnvelope,
)

def _load_pack(tool_context) -> SessionPack:
    raw = tool_context.state.get(STATE_PACK_KEY) or {}
    if isinstance(raw, SessionPack):
        return raw
    try:
        return SessionPack.model_validate(raw)
    except ValidationError:
        return SessionPack()


def _save_pack(tool_context, pack: SessionPack) -> None:
    tool_context.state[STATE_PACK_KEY] = pack.model_dump(mode="json", by_alias=True)


def _append_ledger(tool_context, entry: dict[str, Any]) -> None:
    led = tool_context.state.get(STATE_TOOL_LEDGER_KEY)
    if not isinstance(led, list):
        led = []
    led.append(entry)
    tool_context.state[STATE_TOOL_LEDGER_KEY] = led


def _sources_from_grounding(response: genai_types.GenerateContentResponse) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for candidate in response.candidates or []:
        gm = getattr(candidate, "grounding_metadata", None)
        if not gm or not getattr(gm, "grounding_chunks", None):
            continue
        for chunk in gm.grounding_chunks or []:
            web = getattr(chunk, "web", None)
            if not web:
                continue
            out.append(
                {
                    "title": getattr(web, "title", "") or "",
                    "url": getattr(web, "uri", "") or "",
                    "domain": getattr(web, "domain", "") or "",
                }
            )
    return out[:24]


def web_search(search_query: str, tool_context=None) -> dict[str, Any]:
    """Vertex-backed Google Search grounding via genai (Python callable for ADK AFC compatibility)."""
    if tool_context is None:
        return ToolEnvelope(
            ok=False,
            tool="web_search",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    q = (search_query or "").strip()
    if not q:
        env = ToolEnvelope(ok=False, tool="web_search", status="validation_error", message="empty search_query")
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    project = os.environ.get("GOOGLE_CLOUD_PROJECT", "").strip()
    location = os.environ.get("GOOGLE_CLOUD_LOCATION", "global").strip()
    if not project:
        env = ToolEnvelope(
            ok=False,
            tool="web_search",
            status="internal_error",
            message="GOOGLE_CLOUD_PROJECT is not set",
        )
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    model = os.getenv("AGENTHORDE_WORKER_MODEL", WORKER_MODEL).strip() or WORKER_MODEL
    prompt = (
        "You are a research fetcher. Using Google Search grounding, return concise factual bullets "
        "that directly address the query. Prefer primary sources, trade press, regulators, and company "
        "disclosures. If uncertain, say what is uncertain.\n\nQuery:\n"
        f"{q}"
    )
    try:
        client = genai.Client(vertexai=True, project=project, location=location)
        response = client.models.generate_content(
            model=model,
            contents=prompt,
            config=genai_types.GenerateContentConfig(
                temperature=0.2,
                tools=[genai_types.Tool(google_search=genai_types.GoogleSearch())],
            ),
        )
        text = (response.text or "").strip()
        sources = _sources_from_grounding(response)
        payload = {"answer": text, "sources": sources}
        env = ToolEnvelope(ok=True, tool="web_search", payload=payload)
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    except Exception as exc:  # pragma: no cover
        env = ToolEnvelope(ok=False, tool="web_search", status="internal_error", message=str(exc))
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)


def ah_knowledge_search(query: str, scope: str = "general", tool_context=None) -> dict[str, Any]:
    """Stable-knowledge stub: no live model call inside the tool (agent already has world knowledge)."""
    if tool_context is None:
        return ToolEnvelope(
            ok=False,
            tool="knowledge_search",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    q = (query or "").strip()
    if not q:
        env = ToolEnvelope(ok=False, tool="knowledge_search", status="validation_error", message="empty query")
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    # Heuristic stub: classify stability from wording
    tense = bool(re.search(r"\b(2024|2025|2026|today|latest|current|sanction|export control|embargo)\b", q, re.I))
    out = KnowledgeSearchOutput(
        answer=(
            "Stub knowledge response: treat as non-authoritative narrative memory. "
            "Prefer web_search for time-sensitive or contested facts."
        ),
        stability="low" if tense else "high",
        caveats=["knowledge_search is a stub; verify with web_search or retrieve when needed."],
    )
    env = ToolEnvelope(ok=True, tool="knowledge_search", payload=out.model_dump(mode="json", by_alias=True))
    _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def ah_retrieve(query: str, scope: str = "all", limit: int = 24, tool_context=None) -> dict[str, Any]:
    """Return slices of working_graph / reasoning_graph from SessionPack."""
    if tool_context is None:
        return ToolEnvelope(
            ok=False,
            tool="retrieve",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    if not str(query or "").strip():
        env = ToolEnvelope(ok=False, tool="retrieve", status="validation_error", message="empty query")
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    try:
        inp = RetrieveToolInput(query=query, scope=scope, limit=limit)  # type: ignore[arg-type]
    except ValidationError as exc:
        env = ToolEnvelope(ok=False, tool="retrieve", status="validation_error", message=str(exc))
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    pack = _load_pack(tool_context)
    qlow = inp.query.lower().strip()
    matches: list[dict[str, Any]] = []

    def _scan(obj: Any, path: str) -> None:
        if len(matches) >= inp.limit:
            return
        if isinstance(obj, dict):
            blob = json.dumps(obj, ensure_ascii=False)[:2000]
            if qlow and qlow in blob.lower():
                matches.append({"path": path, "preview": blob[:800]})
            for k, v in list(obj.items())[:80]:
                _scan(v, f"{path}/{k}")
        elif isinstance(obj, list):
            for i, v in enumerate(obj[:80]):
                _scan(v, f"{path}[{i}]")

    if inp.scope in ("all", "working_graph"):
        _scan(pack.working_graph, "workingGraph")
    if inp.scope in ("all", "reasoning_graph"):
        rg = pack.reasoning_graph.model_dump(mode="json", by_alias=True)
        _scan(rg, "reasoningGraph")

    out = RetrieveToolOutput(matches=matches[: inp.limit], scope=inp.scope)
    env = ToolEnvelope(ok=True, tool="retrieve", payload=out.model_dump(mode="json", by_alias=True))
    _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def ah_apply(
    patch_json: str,
    hypothesis_id: str = "",
    tool_context=None,
) -> dict[str, Any]:
    """Append a typed patch to apply_log; optionally merge `nodes` into working_graph."""
    if tool_context is None:
        return ToolEnvelope(
            ok=False,
            tool="apply",
            status="internal_error",
            message="missing tool_context",
        ).model_dump(mode="json", by_alias=True)
    try:
        body = json.loads(patch_json) if patch_json else {}
    except json.JSONDecodeError as exc:
        env = ToolEnvelope(ok=False, tool="apply", status="validation_error", message=f"invalid json: {exc}")
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    try:
        inp = ApplyToolInput(
            hypothesis_id=hypothesis_id or str(body.get("hypothesisId") or ""),
            patch=body if isinstance(body, dict) else {},
        )
    except ValidationError as exc:
        env = ToolEnvelope(ok=False, tool="apply", status="validation_error", message=str(exc))
        _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    pack = _load_pack(tool_context)
    idx = len(pack.apply_log)
    rec = ApplyPatchRecord(
        schema_version=inp.schema_version,
        hypothesis_id=inp.hypothesis_id,
        patch=inp.patch,
    )
    pack.apply_log.append(rec)

    nodes = inp.patch.get("nodes")
    if isinstance(nodes, list):
        if "nodes" not in pack.working_graph:
            pack.working_graph["nodes"] = []
        wg_nodes = pack.working_graph["nodes"]
        if isinstance(wg_nodes, list):
            for n in nodes:
                if isinstance(n, dict):
                    if not n.get("id"):
                        n = {**n, "id": str(uuid.uuid4())[:12]}
                    wg_nodes.append(n)

    _save_pack(tool_context, pack)
    out = ApplyToolOutput(
        applied_index=idx,
        working_graph_node_count=len(pack.working_graph.get("nodes") or []) if isinstance(pack.working_graph, dict) else 0,
    )
    env = ToolEnvelope(ok=True, tool="apply", payload=out.model_dump(mode="json", by_alias=True))
    _append_ledger(tool_context, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def worker_tools() -> list:
    """Tools for analyzer / red_team — all plain callables so ADK AFC stays enabled."""
    return [web_search, ah_knowledge_search, ah_retrieve, ah_apply]
