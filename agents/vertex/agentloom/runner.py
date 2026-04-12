from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys

import google.auth
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types as genai_types

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
PACKAGE_PARENT = os.path.dirname(PACKAGE_DIR)
if PACKAGE_PARENT not in sys.path:
    sys.path.insert(0, PACKAGE_PARENT)

from agentloom.contracts import AgentLoomRequest


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run AgentLoom locally with an input JSON envelope.")
    parser.add_argument("--input-file", required=True, help="Path to AgentLoomRequest JSON")
    parser.add_argument("--validate-only", action="store_true", help="Validate request envelope and exit")
    parser.add_argument("--quiet-runtime-log", action="store_true", help="Do not print live runtime logs to stderr")
    return parser.parse_args()


def _stderr(text: str) -> None:
    sys.stderr.write(text + "\n")
    sys.stderr.flush()


def _configure_vertex_oauth_runtime() -> tuple[str, str]:
    os.environ["GOOGLE_GENAI_USE_VERTEXAI"] = "true"
    project = (
        os.getenv("GOOGLE_CLOUD_PROJECT", "").strip()
        or os.getenv("GCP_PROJECT_ID", "").strip()
        or os.getenv("VERTEX_PROJECT_ID", "").strip()
    )
    if not project:
        raise RuntimeError("Vertex runtime requires GOOGLE_CLOUD_PROJECT or GCP_PROJECT_ID.")
    os.environ["GOOGLE_CLOUD_PROJECT"] = project
    location = (
        os.getenv("GOOGLE_CLOUD_LOCATION", "").strip()
        or os.getenv("VERTEX_SEARCH_LOCATION", "").strip()
        or os.getenv("AGENTLOOM_VERTEX_LOCATION", "").strip()
        or "global"
    )
    os.environ["GOOGLE_CLOUD_LOCATION"] = location
    credentials, detected_project = google.auth.default(
        scopes=["https://www.googleapis.com/auth/cloud-platform"]
    )
    if credentials is None:
        raise RuntimeError("Vertex runtime requires Application Default Credentials via OAuth.")
    resolved_project = project or (detected_project or "").strip()
    if not resolved_project:
        raise RuntimeError("Vertex runtime could not resolve a GCP project from env or ADC.")
    os.environ["GOOGLE_CLOUD_PROJECT"] = resolved_project
    return resolved_project, location


def _format_runtime_log(entry: dict) -> str:
    seq = entry.get("sequence", "?")
    actor = entry.get("actor", "?")
    phase = entry.get("phase", "?")
    model = entry.get("model", "")
    message = entry.get("message", "")
    model_suffix = f" model={model}" if model else ""
    return f"[{seq:02}] {actor} {phase}{model_suffix}: {message}"


def _print_runtime_log_event(event, quiet: bool) -> None:
    if quiet:
        return
    content = getattr(event, "content", None)
    if content is None or not getattr(content, "parts", None):
        return
    for part in content.parts:
        text = getattr(part, "text", "") or ""
        if not text.strip():
            continue
        try:
            payload = json.loads(text)
        except json.JSONDecodeError:
            continue
        runtime_log = payload.get("runtimeLog")
        if not isinstance(runtime_log, dict):
            continue
        _stderr(_format_runtime_log(runtime_log))
        basis = runtime_log.get("basis") or {}
        request = runtime_log.get("request") or {}
        result = runtime_log.get("result") or {}
        if basis:
            _stderr("  basis: " + json.dumps(basis, ensure_ascii=False))
        if request:
            _stderr("  request: " + json.dumps(request, ensure_ascii=False))
        if result:
            _stderr("  result: " + json.dumps(result, ensure_ascii=False))


async def _run_async(payload_text: str, quiet_runtime_log: bool) -> dict:
    from agentloom.agent import APP_NAME, SESSION_ID, USER_ID, STATE_FINAL, root_agent

    session_service = InMemorySessionService()
    await session_service.create_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    runner = Runner(agent=root_agent, app_name=APP_NAME, session_service=session_service)
    content = genai_types.Content(role="user", parts=[genai_types.Part(text=payload_text)])
    async for event in runner.run_async(user_id=USER_ID, session_id=SESSION_ID, new_message=content):
        _print_runtime_log_event(event, quiet_runtime_log)
    session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    if session is None:
        raise RuntimeError("AgentLoom session not found after run")
    return dict(session.state.get(STATE_FINAL) or {})


def main() -> None:
    args = parse_args()
    project, location = _configure_vertex_oauth_runtime()
    _stderr(f"AgentLoom runtime backend: Vertex AI via OAuth (project={project}, location={location})")
    with open(args.input_file, "r", encoding="utf-8") as handle:
        payload_text = handle.read()
    request = AgentLoomRequest.from_json_text(payload_text)
    if args.validate_only:
        print(json.dumps(request.model_dump(mode="json", by_alias=True), ensure_ascii=False, indent=2))
        return
    result = asyncio.run(
        _run_async(
            json.dumps(request.model_dump(mode="json", by_alias=True), ensure_ascii=False),
            args.quiet_runtime_log,
        )
    )
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
