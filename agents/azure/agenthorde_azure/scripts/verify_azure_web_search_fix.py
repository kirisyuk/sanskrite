#!/usr/bin/env python3
"""Verify production web_search path after the temperature + max_output_tokens fix."""

from __future__ import annotations

import asyncio
import json
import os
import sys


async def main() -> int:
    endpoint = (os.getenv("AZURE_OPENAI_ENDPOINT") or "").rstrip("/")
    api_key = (os.getenv("AZURE_OPENAI_API_KEY") or "").strip()
    deployment = (os.getenv("AZURE_OPENAI_DEPLOYMENT") or "").strip()
    api_version = (os.getenv("AZURE_OPENAI_API_VERSION") or "2025-03-01-preview").strip()
    if not (endpoint and api_key and deployment):
        print("missing AZURE_* env", file=sys.stderr)
        return 2

    from openai import AsyncAzureOpenAI

    client = AsyncAzureOpenAI(azure_endpoint=endpoint, api_key=api_key, api_version=api_version)

    from agenthorde_azure.web_search import _grounded_web_search_impl

    state: dict = {}
    result = await _grounded_web_search_impl(
        client=client,
        worker_deployment=deployment,
        search_query="Busiest container ports along the Malacca Strait in 2024.",
        state=state,
    )
    payload = result.get("payload") or {}
    mode = payload.get("groundingMode")
    answer = payload.get("answer") or ""
    sources = payload.get("sources") or []

    print("ok:", result.get("ok"))
    print("groundingMode:", mode)
    print("answer_chars:", len(answer))
    print("sources_count:", len(sources))
    for i, s in enumerate(sources[:5]):
        print(f"  src[{i}]: {s.get('domain','?'):<30} {s.get('url','')[:120]}")
    print("\nanswer[:400]:", answer[:400])
    return 0 if result.get("ok") and mode == "azure_openai_responses_web_search" and sources else 1


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
