#!/usr/bin/env python3
"""Run full AgentHorde on Azure OpenAI (no Google ADK) — same prompts, gates, and pack merge as Vertex."""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import sys
from typing import Any

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
if PACKAGE_DIR not in sys.path:
    sys.path.insert(0, PACKAGE_DIR)


def _setup_logging() -> None:
    """stderr-only progress log; stdout is reserved for the final JSON result.

    AGENTHORDE_LOG_LEVEL lets operators silence or deepen the stream (INFO by default).
    """
    level_name = (os.getenv("AGENTHORDE_LOG_LEVEL") or "INFO").upper()
    level = getattr(logging, level_name, logging.INFO)
    root = logging.getLogger()
    if root.handlers:
        return
    handler = logging.StreamHandler(stream=sys.stderr)
    handler.setFormatter(
        logging.Formatter(
            fmt="%(asctime)s %(levelname)s %(name)s: %(message)s",
            datefmt="%H:%M:%S",
        )
    )
    root.addHandler(handler)
    root.setLevel(level)
    for noisy in ("httpx", "httpcore", "openai", "urllib3"):
        logging.getLogger(noisy).setLevel(logging.WARNING)

from openai import AsyncAzureOpenAI

from agenthorde_azure.azure_openai_client import azure_client
from agenthorde_azure.config import (
    AZURE_CRITIC_DEPLOYMENT,
    AZURE_OPENAI_DEPLOYMENT,
    AZURE_SUPERVISOR_DEPLOYMENT,
    AZURE_WORKER_DEPLOYMENT,
)
from agenthorde_azure.supervisor_loop import run_supervisor_session_azure


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Run AgentHorde (Azure OpenAI) with a JSON or plain-text user payload.")
    p.add_argument("--input-file", default="", help="Path to JSON with a 'question' field, or plain text.")
    p.add_argument(
        "--input-json",
        default="",
        help='Inline JSON, e.g. \'{"question":"..."}\' (overrides --input-file if set).',
    )
    p.add_argument("--validate-only", action="store_true", help="Print normalized input JSON and exit.")
    p.add_argument("--skip-canonicalizer", action="store_true", help="Skip post-run canonicalizer.")
    p.add_argument(
        "--followup-from",
        default="",
        help="Same as Vertex runner: inject canonicalGraph.followUpPlan into the question JSON.",
    )
    p.add_argument(
        "--probe",
        action="store_true",
        help="Only verify Azure connectivity (single completion), do not run the supervisor.",
    )
    p.add_argument(
        "--canonicalize-file",
        default="",
        help="Run only the shared canonicalizer on a CanonicalizerInput JSON file.",
    )
    return p.parse_args()


def _load_followup_plan(path: str) -> dict[str, Any]:
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
    return plan if isinstance(plan, dict) else {}


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


async def _canonicalize_only(path: str) -> dict[str, Any]:
    from agenthorde_common.canonicalizer import canonicalize
    from agenthorde_common.canonicalizer.schemas import CanonicalizerInput

    with open(path, encoding="utf-8") as f:
        blob = json.load(f)
    cin = CanonicalizerInput.model_validate(blob)
    res = await canonicalize(cin)
    return res.model_dump(mode="json", by_alias=True)


async def _run_full(user_text: str, *, skip_canonicalizer: bool) -> dict[str, Any]:
    client = azure_client()
    out = await run_supervisor_session_azure(
        client=client,
        supervisor_deployment=AZURE_SUPERVISOR_DEPLOYMENT,
        worker_deployment=AZURE_WORKER_DEPLOYMENT,
        critic_deployment=AZURE_CRITIC_DEPLOYMENT,
        user_payload=user_text,
        session_id="azure-cli",
    )
    if not skip_canonicalizer:
        from agenthorde_common.canonicalizer import canonicalize
        from agenthorde_common.canonicalizer.schemas import CanonicalizerInput

        cin = CanonicalizerInput(
            pack=out.get("pack") or {},
            analyzer_output=out.get("analyzerOutput") or {},
            governance_output=out.get("governanceOutput") or {},
            tool_ledger=out.get("toolLedger") or [],
            final_answer=out.get("finalAnswer") or "",
            run_id="azure-cli",
        )
        cg = await canonicalize(cin)
        out["canonicalGraph"] = cg.model_dump(mode="json", by_alias=True)
    else:
        out["canonicalGraph"] = None
    return out


async def _probe() -> str:
    from agenthorde_azure.orchestrator import probe_deployment

    return await probe_deployment()


def main() -> None:
    _setup_logging()
    args = parse_args()
    if args.probe:
        print(asyncio.run(_probe()))
        return
    if args.canonicalize_file:
        print(json.dumps(asyncio.run(_canonicalize_only(args.canonicalize_file)), ensure_ascii=False, indent=2))
        return

    raw = _load_user_payload(args)
    normalized = _normalize_input_text(raw)
    if args.followup_from:
        plan = _load_followup_plan(args.followup_from)
        if plan:
            normalized = _merge_followup_into_question(normalized, plan)
            print(
                f"AgentHordeAzure: injected priorFollowUpPlan from {args.followup_from}",
                file=sys.stderr,
            )
    if args.validate_only:
        print(normalized)
        return

    print(
        f"AgentHordeAzure: supervisor={AZURE_SUPERVISOR_DEPLOYMENT} worker={AZURE_WORKER_DEPLOYMENT} "
        f"critic={AZURE_CRITIC_DEPLOYMENT}",
        file=sys.stderr,
    )
    out = asyncio.run(_run_full(normalized, skip_canonicalizer=args.skip_canonicalizer))
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
