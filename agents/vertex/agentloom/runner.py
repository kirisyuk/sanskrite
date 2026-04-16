from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from typing import Any

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
    parser.add_argument(
        "--trace-summary",
        action="store_true",
        help="After the run, print a JSON reasoning digest from logic_trace to stderr (supervisor assessment, rationale, mission, reachability slice).",
    )
    parser.add_argument(
        "--require-final",
        action="store_true",
        help="Exit with code 2 if session has no final_chain_summary (useful for e2e: composition did not run).",
    )
    parser.add_argument(
        "--timing-report",
        action="store_true",
        help=(
            "After the run, print wall-clock timing: gaps between consecutive runtime log events, "
            "and estimated LLM latency per llm_request→llm_result pair (same actor)."
        ),
    )
    parser.add_argument(
        "--max-supervisor-cycles",
        type=int,
        default=None,
        metavar="N",
        help=(
            "Set AGENTLOOM_MAX_SUPERVISOR_CYCLES for this process only (supervisor loop depth). "
            "If omitted, use the environment variable or the built-in default from agentloom.limits."
        ),
    )
    return parser.parse_args()


def _stderr(text: str) -> None:
    sys.stderr.write(text + "\n")
    sys.stderr.flush()


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
            "VERTEX_PROJECT_ID), or pick a default with `gcloud config set project <id>` so ADC "
            "can resolve the quota project."
        )
    location = (
        os.getenv("GOOGLE_CLOUD_LOCATION", "").strip()
        or os.getenv("VERTEX_SEARCH_LOCATION", "").strip()
        or os.getenv("AGENTLOOM_VERTEX_LOCATION", "").strip()
        or "global"
    )
    os.environ["GOOGLE_CLOUD_LOCATION"] = location
    os.environ["GOOGLE_CLOUD_PROJECT"] = project
    return project, location


def _extract_runtime_log_from_event(event: Any) -> dict[str, Any] | None:
    """Parse a single runtime log dict from an ADK event, if present."""
    content = getattr(event, "content", None)
    if content is None or not getattr(content, "parts", None):
        return None
    for part in content.parts:
        text = getattr(part, "text", "") or ""
        if not text.strip():
            continue
        try:
            payload = json.loads(text)
        except json.JSONDecodeError:
            continue
        runtime_log = payload.get("runtimeLog")
        if isinstance(runtime_log, dict):
            return runtime_log
    return None


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


def _format_timing_report(timing_data: dict[str, Any]) -> str:
    """Human-readable timing summary for stderr (LLM spans + largest inter-log gaps)."""
    rows: list[dict[str, Any]] = timing_data.get("rows") or []
    llm_spans: list[dict[str, Any]] = timing_data.get("llm_spans") or []
    total_s = float(timing_data.get("total_wall_s") or 0.0)
    lines: list[str] = []
    lines.append("--- timing report (wall clock; LLM ≈ llm_request→llm_result same actor) ---")
    lines.append(f"total_wall_s: {total_s:.2f}")
    lines.append(f"runtime_log_events: {len(rows)}")
    lines.append("")

    llm_sorted = sorted(llm_spans, key=lambda x: float(x.get("ms") or 0.0), reverse=True)
    lines.append("LLM spans (request→result, same actor) — heaviest first:")
    if not llm_sorted:
        lines.append("  (none parsed; check that agents emit llm_request/llm_result pairs)")
    else:
        for i, sp in enumerate(llm_sorted[:40], 1):
            ms = float(sp.get("ms") or 0.0)
            actor = sp.get("actor") or "?"
            model = sp.get("model") or ""
            seq = sp.get("seq")
            msg = (sp.get("message") or "")[:100]
            lines.append(
                f"  {i:02}. {ms:8.0f} ms  seq={seq}  actor={actor}  model={model}"
            )
            if msg:
                lines.append(f"       {msg}")
    lines.append("")

    gaps_sorted = sorted(rows, key=lambda x: float(x.get("gap_since_prev_ms") or 0.0), reverse=True)
    lines.append("Largest gaps since previous runtime log line (includes tool calls, other work, network):")
    for i, r in enumerate(gaps_sorted[:35], 1):
        g = float(r.get("gap_since_prev_ms") or 0.0)
        lines.append(
            f"  {i:02}. {g:8.0f} ms  cum={float(r.get('cum_ms') or 0):.0f} ms  "
            f"[{r.get('sequence')}] {r.get('actor')} {r.get('phase')} {r.get('model') or ''}"
        )
        mshort = (r.get("message_short") or "")[:90]
        if mshort:
            lines.append(f"       {mshort}")
    lines.append("")
    lines.append(
        "Note: gaps are between consecutive runtime log emissions; parallel branch work can interleave. "
        "LLM spans pair only llm_request with the next llm_result for the same actor."
    )
    return "\n".join(lines)


def _working_graph_from_logic_trace(lt: list) -> dict[str, Any]:
    """Replay graphDelta observations from logic_trace into an operational graph payload."""
    from agentloom.agent import _observation_key
    from agentloom.contracts import GraphObservation
    from agentloom.operational_graph import graph_state_from_observations, graph_state_to_payload

    merged: dict[tuple, GraphObservation] = {}
    for entry in lt:
        if not isinstance(entry, dict):
            continue
        ar = entry.get("agentResult")
        if not isinstance(ar, dict):
            continue
        gd = ar.get("graphDelta") or ar.get("graph_delta") or []
        for raw in gd:
            if not isinstance(raw, dict):
                continue
            try:
                obs = GraphObservation.model_validate(raw)
            except Exception:
                continue
            merged[_observation_key(obs)] = obs
    keys = sorted(merged.keys(), key=lambda k: (str(k[0]), str(k[1]), str(k[2]), str(k[3]), str(k[4])))
    observations = [merged[k] for k in keys]
    if not observations:
        return {}
    return graph_state_to_payload(graph_state_from_observations(observations))


def _trace_reasoning_digest(session_state: dict) -> str:
    from agentloom.agent import (
        STATE_FINAL,
        STATE_GRAPH_MUTATION_LOG,
        STATE_HYPOTHESIS_ENTITY_REGISTRY,
        STATE_LOGIC_TRACE,
        STATE_WORKING_GRAPH,
        _load_operational_graph_state,
        _graph_reachability_report,
    )
    from agentloom.operational_graph import graph_counts

    lt = session_state.get(STATE_LOGIC_TRACE) or []
    last_placeholder_nodes: list = []
    last_placeholder_hints: list = []
    for entry in reversed(lt):
        if not isinstance(entry, dict):
            continue
        seen = entry.get("stateSeen") or {}
        if not isinstance(seen, dict):
            continue
        pn = seen.get("placeholderNodes")
        if isinstance(pn, list) and pn:
            last_placeholder_nodes = pn[:8]
            hints = seen.get("placeholderResolutionHints")
            last_placeholder_hints = list(hints)[:8] if isinstance(hints, list) else []
            break

    working = session_state.get(STATE_WORKING_GRAPH) or {}
    reach_state: dict = session_state
    if graph_counts(_load_operational_graph_state(session_state))["graphObservationCount"] == 0:
        replayed = _working_graph_from_logic_trace(lt)
        if replayed:
            reach_state = {**session_state, STATE_WORKING_GRAPH: replayed}

    structural_chains_digest: list[dict[str, Any]] = []
    route_families_digest: list[dict[str, Any]] = []
    reachability_headline: dict[str, Any] = {}
    if graph_counts(_load_operational_graph_state(reach_state))["graphObservationCount"] > 0:
        full_reach = _graph_reachability_report(reach_state)
        reachability_headline = {
            "healthSummary": full_reach.get("healthSummary"),
            "groundedCompleteChainCount": full_reach.get("groundedCompleteChainCount"),
            "structuralCompleteChainCount": full_reach.get("structuralCompleteChainCount"),
            "partialChainCount": full_reach.get("partialChainCount"),
            "typedBackboneChainCount": full_reach.get("typedBackboneChainCount"),
        }
        for c in (full_reach.get("structurallyCompleteChains") or [])[:2]:
            structural_chains_digest.append(
                {
                    "source": c.get("source"),
                    "sink": c.get("sink"),
                    "hops": c.get("hops"),
                    "path": c.get("path"),
                    "structuralReason": c.get("structuralReason"),
                }
            )
        for fam in (full_reach.get("routeFamilies") or [])[:8]:
            route_families_digest.append(
                {
                    "multiplicity": fam.get("multiplicity"),
                    "narrativeDuplicate": fam.get("narrativeDuplicate"),
                    "sampleSource": fam.get("sampleSource"),
                    "sampleSink": fam.get("sampleSink"),
                    "sampleHops": fam.get("sampleHops"),
                    "fingerprintHead": (fam.get("fingerprint") or [])[:4],
                }
            )

    registry_digest: dict[str, Any] = {}
    reg_raw = session_state.get(STATE_HYPOTHESIS_ENTITY_REGISTRY)
    if isinstance(reg_raw, dict):
        lineage = reg_raw.get("lineageEvents") or []
        by_branch = reg_raw.get("byBranch") or {}
        registry_digest = {
            "branchKeys": sorted(by_branch.keys())[:12],
            "lineageEventCount": len(lineage) if isinstance(lineage, list) else 0,
            "lineageTail": lineage[-4:] if isinstance(lineage, list) else [],
        }

    rows: list[dict] = []
    for i, entry in enumerate(lt):
        if not isinstance(entry, dict):
            continue
        d = entry.get("decision") or {}
        seen = entry.get("stateSeen") or {}
        if not isinstance(seen, dict):
            seen = {}
        reach = seen.get("reachability")
        mission = entry.get("mission") or {}
        if not isinstance(mission, dict):
            mission = {}
        ar = entry.get("agentResult")
        if isinstance(ar, dict):
            gd = ar.get("graphDelta") or ar.get("graph_delta") or []
            gaps = ar.get("gaps") or []
            status = ar.get("status")
        else:
            gd, gaps, status = [], [], None
        ph_nodes = seen.get("placeholderNodes")
        ph_hints = seen.get("placeholderResolutionHints")
        rs = entry.get("resultingState") or {}
        last_mut = rs.get("lastGraphMutation") if isinstance(rs, dict) else None
        pc = entry.get("policyCorrection") if isinstance(entry.get("policyCorrection"), dict) else None
        dpo = entry.get("delegatePolicyOutcome") if isinstance(entry.get("delegatePolicyOutcome"), dict) else None
        rows.append(
            {
                "index": i,
                "traceCycle": entry.get("cycle"),
                "decisionCycle": d.get("cycle"),
                "nextAgent": d.get("nextAgent"),
                "supervisorNextAgentRaw": entry.get("supervisorNextAgentRaw"),
                "delegatePolicyOutcome": dpo,
                "policyCorrection": pc,
                "assessment": d.get("assessment"),
                "rationale": d.get("rationale"),
                "completionRationale": d.get("completionRationale"),
                "missionId": mission.get("missionId"),
                "missionObjective": mission.get("objective"),
                "assignedRoles": mission.get("assignedRoles"),
                "lastGraphMutation": last_mut,
                "stateSeen": {
                    "graphObservationCount": seen.get("graphObservationCount"),
                    "openGaps": (seen.get("openGaps") or [])[:8],
                    "stagnationCount": (seen.get("progress") or {}).get("stagnationCount"),
                    "reachability": reach,
                    "placeholderNodes": ph_nodes if isinstance(ph_nodes, list) else None,
                    "placeholderResolutionHints": (ph_hints or [])[:8] if isinstance(ph_hints, list) else None,
                },
                "nextStepLogic": entry.get("nextStepLogic"),
                "agentResultBrief": {
                    "status": status,
                    "graphDeltaCount": len(gd),
                    "gapsHead": gaps[:5],
                },
            }
        )
    mut_hist = session_state.get(STATE_GRAPH_MUTATION_LOG) or []
    mut_tail = mut_hist[-3:] if isinstance(mut_hist, list) else []

    payload = {
        "logicTraceEntries": len(rows),
        "hasFinal": bool(session_state.get(STATE_FINAL)),
        "emptyTypedPayloadStreak": int(session_state.get("empty_typed_graph_payload_streak") or 0),
        "placeholderFromTrace": {
            "placeholderNodes": last_placeholder_nodes,
            "placeholderResolutionHints": last_placeholder_hints,
        },
        "graphMutationLogTail": mut_tail,
        "reachabilityHeadline": reachability_headline,
        "routeFamiliesDigest": route_families_digest,
        "hypothesisEntityRegistryDigest": registry_digest,
        "structurallyCompleteChainsDigest": structural_chains_digest,
        "reasoningTrace": rows,
    }
    return json.dumps(payload, ensure_ascii=False, indent=2)


async def _run_async(
    payload_text: str,
    quiet_runtime_log: bool,
    *,
    collect_timing: bool,
) -> tuple[dict, dict, dict[str, Any] | None]:
    from agentloom.agent import APP_NAME, SESSION_ID, USER_ID, STATE_FINAL, root_agent

    session_service = InMemorySessionService()
    await session_service.create_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    runner = Runner(agent=root_agent, app_name=APP_NAME, session_service=session_service)
    content = genai_types.Content(role="user", parts=[genai_types.Part(text=payload_text)])

    timing_rows: list[dict[str, Any]] = []
    llm_spans: list[dict[str, Any]] = []
    pending_llm: dict[str, float] = {}
    last_log_mono: float | None = None
    t_run_start = time.monotonic()

    async for event in runner.run_async(user_id=USER_ID, session_id=SESSION_ID, new_message=content):
        _print_runtime_log_event(event, quiet_runtime_log)
        if not collect_timing:
            continue
        now = time.monotonic()
        rl = _extract_runtime_log_from_event(event)
        if not rl:
            continue
        actor = str(rl.get("actor") or "?")
        phase = str(rl.get("phase") or "?")
        seq = rl.get("sequence")
        model = str(rl.get("model") or "")
        msg = str(rl.get("message") or "")
        gap_ms = 0.0 if last_log_mono is None else (now - last_log_mono) * 1000.0
        cum_ms = (now - t_run_start) * 1000.0
        timing_rows.append(
            {
                "sequence": seq,
                "actor": actor,
                "phase": phase,
                "model": model,
                "gap_since_prev_ms": gap_ms,
                "cum_ms": cum_ms,
                "message_short": msg[:160],
            }
        )
        last_log_mono = now

        if phase == "llm_request":
            pending_llm[actor] = now
        elif phase in ("llm_result", "llm_complete") and actor in pending_llm:
            # Critic uses llm_complete; StructuredOutputAgent paths use llm_result.
            t_req = pending_llm.pop(actor)
            llm_spans.append(
                {
                    "actor": actor,
                    "model": model,
                    "ms": (now - t_req) * 1000.0,
                    "seq": seq,
                    "message": msg[:200],
                    "closePhase": phase,
                }
            )

    session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    if session is None:
        raise RuntimeError("AgentLoom session not found after run")
    st = dict(session.state or {})
    total_wall_s = time.monotonic() - t_run_start
    timing_data: dict[str, Any] | None = None
    if collect_timing:
        timing_data = {
            "rows": timing_rows,
            "llm_spans": llm_spans,
            "total_wall_s": total_wall_s,
        }
    return dict(st.get(STATE_FINAL) or {}), st, timing_data


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
    if args.max_supervisor_cycles is not None:
        if args.max_supervisor_cycles < 1:
            raise SystemExit("agentloom runner: --max-supervisor-cycles must be >= 1")
        os.environ["AGENTLOOM_MAX_SUPERVISOR_CYCLES"] = str(args.max_supervisor_cycles)
        _stderr(f"Supervisor cycle limit for this run: {args.max_supervisor_cycles} (AGENTLOOM_MAX_SUPERVISOR_CYCLES)")
    result, session_state, timing_data = asyncio.run(
        _run_async(
            json.dumps(request.model_dump(mode="json", by_alias=True), ensure_ascii=False),
            args.quiet_runtime_log,
            collect_timing=args.timing_report,
        )
    )
    if args.timing_report and timing_data:
        _stderr(_format_timing_report(timing_data))
    if args.trace_summary:
        _stderr("--- logic_trace reasoning digest (stderr) ---")
        _stderr(_trace_reasoning_digest(session_state))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    if args.require_final and not result:
        _stderr(
            "AgentLoom runner: final_chain_summary is empty (no composed output). "
            "Typical causes: supervisor hit AGENTLOOM_MAX_SUPERVISOR_CYCLES with AGENTLOOM_LIMIT_EXHAUSTED_STRATEGY=stop, "
            "or critic compose-gate did not pass. Try AGENTLOOM_LIMIT_EXHAUSTED_STRATEGY=compose_with_caveats and/or more cycles."
        )
        sys.exit(2)


if __name__ == "__main__":
    main()
