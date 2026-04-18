#!/usr/bin/env python3
"""Manual probe: Azure OpenAI Responses API with ``web_search`` tool on o-series (o4-mini).

Goal: find the minimal call shape that (a) actually runs web grounding on Azure,
(b) returns ``output_text`` with ``url_citation`` annotations, (c) works without
hitting the ``'temperature' is not supported with this model`` 400.

We try four variants against AZURE_OPENAI_DEPLOYMENT=o4-mini and print, per
variant: HTTP outcome, latency, response.id, output_text snippet, sources count,
and first 3 source URLs.

Usage:
    python3 scripts/probe_azure_web_search.py

Reads AZURE_OPENAI_* from environment (.env should already be sourced).
"""

from __future__ import annotations

import asyncio
import os
import sys
import time
from typing import Any


def _mask(s: str, keep: int = 4) -> str:
    if not s:
        return "<empty>"
    return s[:keep] + "…" + s[-keep:] if len(s) > 2 * keep else "***"


async def _one_call(
    client: Any,
    *,
    label: str,
    deployment: str,
    query: str,
    include_temperature: bool,
    use_search_context_size: bool,
) -> None:
    from agenthorde_common.tools.openai_responses_web import (
        citations_from_openai_response,
        output_text_from_openai_response,
    )

    tools: list[dict[str, Any]] = [{"type": "web_search"}]
    if use_search_context_size:
        tools = [{"type": "web_search", "search_context_size": "medium"}]

    kwargs: dict[str, Any] = {
        "model": deployment,
        "tools": tools,
        "tool_choice": "auto",
        "input": (
            "You are a research fetcher. Using live web grounding, return concise factual "
            "bullets that directly address the query, each with a URL citation.\n\n"
            f"Query:\n{query}"
        ),
        "max_output_tokens": 3000,
    }
    if include_temperature:
        kwargs["temperature"] = 0.2

    t0 = time.perf_counter()
    print(f"\n=== {label} ===")
    print(f"  tools={tools} temperature={'yes' if include_temperature else 'no'}")
    try:
        resp = await client.responses.create(**kwargs)
    except Exception as exc:
        dt = time.perf_counter() - t0
        print(f"  ERROR after {dt:.1f}s: {type(exc).__name__}: {exc}")
        return

    dt = time.perf_counter() - t0
    resp_id = getattr(resp, "id", "?")
    err = getattr(resp, "error", None)
    status = getattr(resp, "status", None)
    text = output_text_from_openai_response(resp)
    sources = citations_from_openai_response(resp)
    types_in_output = []
    for item in getattr(resp, "output", None) or []:
        t = getattr(item, "type", None)
        types_in_output.append(t)

    print(f"  OK in {dt:.1f}s  id={resp_id}  status={status}  error={err}")
    print(f"  output items types: {types_in_output}")

    raw_ann: list[dict[str, Any]] = []
    for item in getattr(resp, "output", None) or []:
        if getattr(item, "type", None) != "message":
            continue
        for part in getattr(item, "content", None) or []:
            anns = getattr(part, "annotations", None) or []
            for a in anns:
                raw_ann.append(
                    {
                        "type": getattr(a, "type", None),
                        "url": getattr(a, "url", None),
                        "title": getattr(a, "title", None),
                    }
                )
    ws_calls = 0
    ws_queries: list[str] = []
    for item in getattr(resp, "output", None) or []:
        if getattr(item, "type", None) == "web_search_call":
            ws_calls += 1
            act = getattr(item, "action", None)
            if act is not None:
                q = getattr(act, "query", None)
                if isinstance(q, str):
                    ws_queries.append(q)
    print(f"  web_search_calls={ws_calls} queries={ws_queries[:3]}")
    print(f"  raw_annotation_count={len(raw_ann)} types={[a['type'] for a in raw_ann[:5]]}")
    print(f"  output_text_chars={len(text)} citations={len(sources)}")

    try:
        import json as _json
        from openai import BaseModel as _BM  # type: ignore

        def _dump(obj: Any) -> Any:
            if hasattr(obj, "model_dump"):
                return obj.model_dump()
            return obj

        for i, item in enumerate(getattr(resp, "output", None) or []):
            t = getattr(item, "type", None)
            if t == "web_search_call":
                d = _dump(item)
                print(f"  DUMP web_search_call[{i}] keys={list(d.keys())}")
                act = d.get("action")
                if isinstance(act, dict):
                    print(f"    action keys={list(act.keys())} query={str(act.get('query'))[:120]!r}")
                    srcs = act.get("sources") or act.get("citations") or []
                    print(f"    action.sources_count={len(srcs) if isinstance(srcs, list) else 'n/a'}")
                    if isinstance(srcs, list):
                        for ki, s in enumerate(srcs[:3]):
                            print(f"    action.sources[{ki}]={_json.dumps(s)[:260]}")
                break
        for i, item in enumerate(getattr(resp, "output", None) or []):
            t = getattr(item, "type", None)
            if t == "message":
                d = _dump(item)
                content = d.get("content") or []
                for j, part in enumerate(content):
                    print(f"  DUMP message.content[{j}] keys={list(part.keys()) if isinstance(part, dict) else type(part).__name__}")
                    if isinstance(part, dict):
                        ann = part.get("annotations") or []
                        print(f"    annotations_count={len(ann)}")
                        for k, a in enumerate(ann[:3]):
                            print(f"    ann[{k}]={_json.dumps(a)[:240]}")
                break
    except Exception as e:
        print(f"  (dump failed: {e})")
    if text:
        snippet = text[:360].replace("\n", " ")
        print(f"  text[:360]={snippet!r}")
    for i, s in enumerate(sources[:3]):
        print(f"  src[{i}] {s.get('domain','?'):<30} {s.get('url','')[:110]}")


async def main() -> int:
    endpoint = (os.getenv("AZURE_OPENAI_ENDPOINT") or "").rstrip("/")
    api_key = (os.getenv("AZURE_OPENAI_API_KEY") or "").strip()
    deployment = (os.getenv("AZURE_OPENAI_DEPLOYMENT") or "").strip()
    api_version = (os.getenv("AZURE_OPENAI_API_VERSION") or "2025-03-01-preview").strip()
    if not (endpoint and api_key and deployment):
        print("AZURE_OPENAI_ENDPOINT / AZURE_OPENAI_API_KEY / AZURE_OPENAI_DEPLOYMENT are required", file=sys.stderr)
        return 2

    print(f"endpoint={endpoint}")
    print(f"api_version={api_version}")
    print(f"deployment={deployment}")
    print(f"api_key={_mask(api_key)}")

    from openai import AsyncAzureOpenAI

    client = AsyncAzureOpenAI(
        azure_endpoint=endpoint,
        api_key=api_key,
        api_version=api_version,
    )

    query = "What are the busiest container ports along the Malacca Strait in 2024?"

    await _one_call(
        client,
        label="B) NO temperature, tools=[web_search], strict-cite prompt",
        deployment=deployment,
        query=query,
        include_temperature=False,
        use_search_context_size=False,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
