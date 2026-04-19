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
from google.adk.agents.run_config import RunConfig
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types as genai_types

from agenthorde.config import (  # noqa: E402
    APP_NAME,
    SESSION_ID,
    USER_ID,
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_DECISION_INFLUENCE_OUTPUT_KEY,
    STATE_FINAL_ANSWER_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
    STATE_TOOL_LEDGER_KEY,
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
    p.add_argument(
        "--skip-canonicalizer",
        action="store_true",
        help="Skip post-run canonicalizer (no extra LLM / Firestore for dictionaries).",
    )
    p.add_argument(
        "--followup-from",
        default="",
        help=(
            "Path to a prior run's JSON output. Its canonicalGraph.followUpPlan is "
            "injected into the next question as {knownAnchors, deferredResearch, "
            "aggregateMentions} so analyzers skip rediscovery and focus on gaps."
        ),
    )
    return p.parse_args()


def _load_followup_plan(path: str) -> dict[str, Any]:
    """Extract followUpPlan + knownAnchors from a prior --run output JSON."""
    if not path:
        return {}
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    if not isinstance(raw, dict):
        return {}
    cg = raw.get("canonicalGraph") or {}
    if not isinstance(cg, dict):
        return {}
    plan = cg.get("followUpPlan")
    if not isinstance(plan, dict):
        return {}
    return plan


def _merge_followup_into_question(normalized: str, plan: dict[str, Any]) -> str:
    if not plan:
        return normalized
    try:
        obj = json.loads(normalized)
    except json.JSONDecodeError:
        obj = {"question": normalized.strip()}
    if not isinstance(obj, dict):
        obj = {"question": str(obj)}
    obj["priorFollowUpPlan"] = {
        "knownAnchors": plan.get("knownAnchors", [])[:64],
        "deferredResearch": plan.get("deferredResearch", [])[:32],
        "aggregateMentions": plan.get("aggregateMentions", [])[:32],
        "counters": plan.get("counters", {}),
    }
    return json.dumps(obj, ensure_ascii=False)


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


async def _run_async(user_text: str, *, skip_canonicalizer: bool = False) -> dict[str, Any]:
    session_service = InMemorySessionService()
    await session_service.create_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    runner = Runner(agent=root_agent, app_name=APP_NAME, session_service=session_service)
    content = genai_types.Content(role="user", parts=[genai_types.Part(text=user_text)])
    max_llm_calls = int(os.getenv("AGENTHORDE_MAX_LLM_CALLS", "2500"))
    run_config = RunConfig(max_llm_calls=max_llm_calls)
    async for _event in runner.run_async(
        user_id=USER_ID,
        session_id=SESSION_ID,
        new_message=content,
        run_config=run_config,
    ):
        pass
    session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    if session is None:
        raise RuntimeError("Session not found after run")
    st = dict(session.state or {})
    out: dict[str, Any] = {
        "finalAnswer": st.get(STATE_FINAL_ANSWER_KEY, ""),
        "pack": st.get(STATE_PACK_KEY, {}),
        "supervisorDecision": st.get("ah_supervisor_decision", {}),
        "analyzerOutput": st.get(STATE_ANALYZER_OUTPUT_KEY, {}),
        "governanceOutput": st.get(STATE_GOVERNANCE_OUTPUT_KEY, {}),
        "sanctionsOutput": st.get(STATE_SANCTIONS_OUTPUT_KEY, {}),
        "decisionInfluenceOutput": st.get(STATE_DECISION_INFLUENCE_OUTPUT_KEY, {}),
        "criticOutput": st.get("ah_critic_output", {}),
        "redTeamOutput": st.get("ah_red_team_output", {}),
        "toolLedger": st.get(STATE_TOOL_LEDGER_KEY, []),
    }
    if not skip_canonicalizer:
        from agenthorde_common.canonicalizer import canonicalize
        from agenthorde_common.canonicalizer.schemas import CanonicalizerInput

        cin = CanonicalizerInput(
            pack=out.get("pack") or {},
            analyzer_output=out.get("analyzerOutput") or {},
            governance_output=out.get("governanceOutput") or {},
            sanctions_output=out.get("sanctionsOutput") or {},
            decision_influence_output=out.get("decisionInfluenceOutput") or {},
            tool_ledger=out.get("toolLedger") or [],
            final_answer=out.get("finalAnswer") or "",
            run_id=SESSION_ID,
        )
        cg = await canonicalize(cin)
        out["canonicalGraph"] = cg.model_dump(mode="json", by_alias=True)
    else:
        out["canonicalGraph"] = None
    return out


def main() -> None:
    args = parse_args()
    raw = _load_user_payload(args)
    normalized = _normalize_input_text(raw)
    if args.followup_from:
        plan = _load_followup_plan(args.followup_from)
        if plan:
            normalized = _merge_followup_into_question(normalized, plan)
            print(
                f"AgentHorde: injected priorFollowUpPlan from {args.followup_from} "
                f"({len(plan.get('knownAnchors') or [])} anchors, "
                f"{len(plan.get('deferredResearch') or [])} deferred items)",
                file=sys.stderr,
            )
    if args.validate_only:
        print(normalized)
        return
    project, location = _configure_vertex_oauth_runtime()
    print(f"AgentHorde: Vertex project={project} location={location}", file=sys.stderr)
    out = asyncio.run(_run_async(normalized, skip_canonicalizer=args.skip_canonicalizer))
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
