"""LLM JSON calls for canonicalizer phases: Vertex (Gemini) or OpenRouter."""

from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import Any

from agenthorde_common.canonicalizer.version import CANONICALIZER_VERSION
from agenthorde_common.config import CANONICALIZER_LLM_MAX_RETRIES, CANONICALIZER_LLM_TIMEOUT_SEC

_LOG = logging.getLogger(__name__)


def _package_prompt(name: str) -> str:
    import os.path as p

    base = p.join(p.dirname(__file__), "prompts", f"{name}.txt")
    try:
        with open(base, encoding="utf-8") as f:
            return f.read()
    except OSError:
        return ""


def _openrouter_model(model: str) -> bool:
    """OpenRouter slugs look like ``openai/o4-mini``, ``anthropic/claude-3.5-sonnet``."""
    m = (model or "").strip()
    return "/" in m and not m.startswith("http://") and not m.startswith("https://")


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

        openrouter_key = (os.environ.get("OPENROUTER_API_KEY") or "").strip()
        use_openrouter = bool(openrouter_key) and _openrouter_model(model)

        if _openrouter_model(model) and not openrouter_key:
            _LOG.error(
                "canonicalizer phase=%s model=%s looks like OpenRouter but OPENROUTER_API_KEY is unset",
                phase_key,
                model,
            )
            return {}

        timeout = max(1.0, float(CANONICALIZER_LLM_TIMEOUT_SEC))
        retries = max(1, int(CANONICALIZER_LLM_MAX_RETRIES))

        if use_openrouter:

            def _call_openrouter() -> dict[str, Any]:
                import httpx

                url = (os.environ.get("OPENROUTER_API_BASE") or "https://openrouter.ai/api/v1/chat/completions").strip()
                max_tokens = int(
                    os.getenv(
                        "AGENTHORDE_CANONICALIZER_OPENROUTER_MAX_TOKENS",
                        os.getenv("CANONICALIZER_OPENROUTER_MAX_TOKENS", "32768"),
                    )
                )
                max_tokens = max(512, max_tokens)
                body: dict[str, Any] = {
                    "model": model.strip(),
                    "messages": [
                        {"role": "system", "content": system},
                        {"role": "user", "content": user_payload},
                    ],
                    "temperature": 0.1,
                    "max_tokens": max_tokens,
                }
                if os.getenv("AGENTHORDE_CANONICALIZER_OPENROUTER_JSON_OBJECT", "1").lower() not in (
                    "0",
                    "false",
                    "no",
                ):
                    body["response_format"] = {"type": "json_object"}
                effort = (os.getenv("AGENTHORDE_CANONICALIZER_O4_REASONING_EFFORT") or "low").strip()
                if "o4" in model.lower():
                    body["reasoning"] = {"effort": effort}
                referer = (os.getenv("OPENROUTER_HTTP_REFERER") or "https://local.agenthorde").strip()
                title = (os.getenv("OPENROUTER_APP_TITLE") or "agenthorde-canonicalizer").strip()
                r = httpx.post(
                    url,
                    headers={
                        "Authorization": f"Bearer {openrouter_key}",
                        "Content-Type": "application/json",
                        "HTTP-Referer": referer,
                        "X-Title": title,
                    },
                    json=body,
                    timeout=timeout,
                )
                if r.status_code != 200:
                    raise RuntimeError(f"OpenRouter HTTP {r.status_code}: {r.text[:500]}")
                data = r.json()
                choices = data.get("choices") or []
                if not choices:
                    raise RuntimeError(f"OpenRouter no choices: {str(data)[:500]}")
                content = (choices[0].get("message") or {}).get("content") or ""
                content = str(content).strip()
                if not content:
                    return {}
                return json.loads(content)

            last_err = ""
            for attempt in range(retries):
                try:
                    return await asyncio.wait_for(asyncio.to_thread(_call_openrouter), timeout=timeout)
                except asyncio.CancelledError:
                    raise
                except asyncio.TimeoutError:
                    last_err = f"timeout after {timeout:.1f}s"
                    _LOG.warning(
                        "canonicalizer phase=%s model=%s attempt=%d/%d %s",
                        phase_key,
                        model,
                        attempt + 1,
                        retries,
                        last_err,
                    )
                except Exception as exc:  # noqa: BLE001
                    last_err = f"{type(exc).__name__}: {exc}"
                    _LOG.warning(
                        "canonicalizer phase=%s model=%s attempt=%d/%d failed: %s",
                        phase_key,
                        model,
                        attempt + 1,
                        retries,
                        last_err,
                    )
                if attempt + 1 >= retries:
                    _LOG.error(
                        "canonicalizer phase=%s model=%s (OpenRouter) gave up after %d attempts (last=%s)",
                        phase_key,
                        model,
                        retries,
                        last_err or "unknown",
                    )
                    return {}
                await asyncio.sleep(min(2.0, 0.4 * (attempt + 1)))
            return {}

        project = os.environ.get("GOOGLE_CLOUD_PROJECT", "").strip()
        location = os.environ.get("GOOGLE_CLOUD_LOCATION", "global").strip()
        if not project:
            _LOG.warning(
                "canonicalizer phase=%s skipped: GOOGLE_CLOUD_PROJECT unset (model=%s)",
                phase_key,
                model,
            )
            return {}

        def _call_vertex() -> dict[str, Any]:
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

        last_err = ""
        for attempt in range(retries):
            try:
                return await asyncio.wait_for(asyncio.to_thread(_call_vertex), timeout=timeout)
            except asyncio.CancelledError:
                raise
            except asyncio.TimeoutError:
                last_err = f"timeout after {timeout:.1f}s"
                _LOG.warning(
                    "canonicalizer phase=%s model=%s attempt=%d/%d %s",
                    phase_key,
                    model,
                    attempt + 1,
                    retries,
                    last_err,
                )
            except Exception as exc:  # noqa: BLE001
                last_err = f"{type(exc).__name__}: {exc}"
                _LOG.warning(
                    "canonicalizer phase=%s model=%s attempt=%d/%d failed: %s",
                    phase_key,
                    model,
                    attempt + 1,
                    retries,
                    last_err,
                )
            if attempt + 1 >= retries:
                _LOG.error(
                    "canonicalizer phase=%s model=%s gave up after %d attempts (last=%s)",
                    phase_key,
                    model,
                    retries,
                    last_err or "unknown",
                )
                return {}
            await asyncio.sleep(min(2.0, 0.4 * (attempt + 1)))
        return {}
