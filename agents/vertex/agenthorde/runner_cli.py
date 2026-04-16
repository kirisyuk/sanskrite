#!/usr/bin/env python3
"""Run AgentHorde root_agent on Vertex with ADC (OAuth)."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from typing import Any

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
if PACKAGE_DIR not in sys.path:
    sys.path.insert(0, PACKAGE_DIR)

from agenthorde.quiet_vertex_logs import install_known_vertex_noise_filters  # noqa: E402

install_known_vertex_noise_filters()

import google.auth
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types as genai_types

from agenthorde.config import (  # noqa: E402
    APP_NAME,
    SESSION_ID,
    USER_ID,
    STATE_FINAL_ANSWER_KEY,
    STATE_PACK_KEY,
)
from agenthorde.agents.supervisor import root_agent  # noqa: E402


def _configure_vertex_oauth_runtime() -> tuple[str, str]:
    os.environ["GOOGLE_GENAI_USE_VERTEXAI"] = "true"
    credentials, detected_project = google.auth.default(
        scopes=["https://www.googleapis.com/auth/cloud-platform"]
    )
    if credentials is None:
        raise RuntimeError("Vertex runtime requires Application Default Credentials via OAuth.")
    project = (
        os.getenv("GOOGLE_CLOUD_PROJECT", "").strip()
        or os.getenv("GCP_PROJECT_ID", "").strip()
        or os.getenv("VERTEX_PROJECT_ID", "").strip()
        or (detected_project or "").strip()
    )
    if not project:
        raise RuntimeError(
            "Vertex runtime needs a GCP project id: set GOOGLE_CLOUD_PROJECT (or GCP_PROJECT_ID / "
            "VERTEX_PROJECT_ID), or pick a default with `gcloud config set project <id>`."
        )
    location = (
        os.getenv("GOOGLE_CLOUD_LOCATION", "").strip()
        or os.getenv("VERTEX_SEARCH_LOCATION", "").strip()
        or os.getenv("AGENTHORDE_VERTEX_LOCATION", "").strip()
        or "global"
    )
    os.environ["GOOGLE_CLOUD_LOCATION"] = location
    os.environ["GOOGLE_CLOUD_PROJECT"] = project
    return project, location


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Run AgentHorde (Vertex ADK) with a JSON or plain-text user payload.")
    p.add_argument(
        "--input-file",
        default="",
        help="Path to JSON file with a 'question' field, or plain text.",
    )
    p.add_argument(
        "--input-json",
        default="",
        help='Inline JSON, e.g. \'{"question":"..."}\' (overrides --input-file if set).',
    )
    p.add_argument(
        "--validate-only",
        action="store_true",
        help="Parse and print normalized input JSON then exit (no Vertex calls).",
    )
    return p.parse_args()


def _load_user_payload(args: argparse.Namespace) -> str:
    if args.input_json.strip():
        return args.input_json.strip()
    if not args.input_file:
        raise SystemExit("Provide --input-json or --input-file")
    with open(args.input_file, encoding="utf-8") as f:
        return f.read().strip()


def _normalize_input_text(raw: str) -> str:
    raw = raw.strip()
    if raw.startswith("{"):
        try:
            obj = json.loads(raw)
            if isinstance(obj, dict) and "question" in obj:
                return json.dumps({"question": str(obj["question"]).strip()}, ensure_ascii=False)
        except json.JSONDecodeError:
            pass
    return json.dumps({"question": raw}, ensure_ascii=False)


async def _run_async(user_text: str) -> dict[str, Any]:
    session_service = InMemorySessionService()
    await session_service.create_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    runner = Runner(agent=root_agent, app_name=APP_NAME, session_service=session_service)
    content = genai_types.Content(role="user", parts=[genai_types.Part(text=user_text)])
    async for _event in runner.run_async(user_id=USER_ID, session_id=SESSION_ID, new_message=content):
        pass
    session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    if session is None:
        raise RuntimeError("Session not found after run")
    st = dict(session.state or {})
    return {
        "finalAnswer": st.get(STATE_FINAL_ANSWER_KEY, ""),
        "pack": st.get(STATE_PACK_KEY, {}),
        "supervisorDecision": st.get("ah_supervisor_decision", {}),
        "analyzerOutput": st.get("ah_analyzer_output", {}),
        "governanceOutput": st.get("ah_governance_output", {}),
        "criticOutput": st.get("ah_critic_output", {}),
        "redTeamOutput": st.get("ah_red_team_output", {}),
        "toolLedger": st.get("ah_tool_ledger", []),
    }


def main() -> None:
    args = parse_args()
    raw = _load_user_payload(args)
    normalized = _normalize_input_text(raw)
    if args.validate_only:
        print(normalized)
        return
    project, location = _configure_vertex_oauth_runtime()
    print(f"AgentHorde: Vertex project={project} location={location}", file=sys.stderr)
    out = asyncio.run(_run_async(normalized))
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
