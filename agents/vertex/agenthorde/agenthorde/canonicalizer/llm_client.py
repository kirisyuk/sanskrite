"""Vertex genai JSON calls for canonicalizer phases (with skip + mock hooks)."""

from __future__ import annotations

import asyncio
import json
import os
from typing import Any

from agenthorde.canonicalizer.version import CANONICALIZER_VERSION
from agenthorde.config import CANONICALIZER_LLM_MAX_RETRIES, CANONICALIZER_LLM_TIMEOUT_SEC


def _package_prompt(name: str) -> str:
    import os.path as p

    base = p.join(p.dirname(__file__), "prompts", f"{name}.txt")
    try:
        with open(base, encoding="utf-8") as f:
            return f.read()
    except OSError:
        return ""


class LLMPhaseClient:
    def __init__(self, mock_responses: dict[str, dict[str, Any]] | None = None) -> None:
        self.mock_responses = mock_responses or {}

    async def generate_json(
        self,
        phase_key: str,
        model: str,
        user_payload: str,
        extra_system: str = "",
    ) -> dict[str, Any]:
        if phase_key in self.mock_responses:
            return dict(self.mock_responses[phase_key])
        def _skip(name: str) -> bool:
            return (os.getenv(name) or "").strip().lower() in ("1", "true", "yes")

        if _skip("AGENTHORDE_CANONICALIZER_SKIP") or _skip("CANONICALIZER_SKIP"):
            return {}
        project = os.environ.get("GOOGLE_CLOUD_PROJECT", "").strip()
        location = os.environ.get("GOOGLE_CLOUD_LOCATION", "global").strip()
        if not project:
            return {}

        system = (
            f"You are the AgentHorde canonicalizer ({CANONICALIZER_VERSION}). "
            f"Output a single JSON object only, no markdown.\n{extra_system}".strip()
        )
        prompt_file = {"1": "phase1_entities", "4": "phase4_relations", "6": "phase6_metrics"}.get(
            phase_key, "phase1_entities"
        )
        base = _package_prompt(prompt_file)
        if base:
            system = system + "\n\n" + base

        def _call() -> dict[str, Any]:
            from google import genai
            from google.genai import types as genai_types

            client = genai.Client(vertexai=True, project=project, location=location)
            resp = client.models.generate_content(
                model=model,
                contents=user_payload,
                config=genai_types.GenerateContentConfig(
                    temperature=0.1,
                    system_instruction=system,
                    response_mime_type="application/json",
                ),
            )
            text = (resp.text or "").strip()
            if not text:
                return {}
            return json.loads(text)

        timeout = max(1.0, CANONICALIZER_LLM_TIMEOUT_SEC)
        retries = max(1, CANONICALIZER_LLM_MAX_RETRIES)
        for attempt in range(retries):
            try:
                return await asyncio.wait_for(asyncio.to_thread(_call), timeout=timeout)
            except asyncio.CancelledError:
                raise
            except Exception:
                if attempt + 1 >= retries:
                    return {}
                await asyncio.sleep(min(2.0, 0.4 * (attempt + 1)))
        return {}
