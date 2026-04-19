"""Full AgentHorde supervisor cycle on Azure OpenAI (same gates/merges as Vertex ADK)."""

from __future__ import annotations

import logging
import time
from typing import Any, MutableMapping

_LOG = logging.getLogger("agenthorde_azure.supervisor_loop")

_PARALLEL_OK = frozenset(
    {"supply_chain_analyzer", "governance_control_analyzer", "sanctions_analyzer"}
)

from agenthorde_common.orchestration.supervisor_core import (
    _check_gates,
    _dump_pack,
    _escalation_hints_from_raw,
    _load_pack,
    _log_decision,
    _log_gate,
    _merge_analyzer_into_pack,
    _merge_red_team_temperature,
    _safe_critic,
    _sync_prompt_keys,
    build_final_answer_text,
    coerce_supervisor_decision,
    extract_user_text_from_payload,
    seed_session_state,
)
from agenthorde_common.runtime.state_keys import (
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_CRITIC_OUTPUT_KEY,
    STATE_FINAL_ANSWER_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_QUESTION_PLAN_KEY,
    STATE_RED_TEAM_OUTPUT_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
    STATE_SUPERVISOR_DECISION_KEY,
    STATE_TOOL_LEDGER_KEY,
)
from agenthorde_common.schemas.agents_io import CriticVerdict, EscalationHint

from agenthorde_azure.azure_critic import run_critic_azure_then_openrouter
from agenthorde_azure.config import MAX_SUPERVISOR_CYCLES
from agenthorde_azure.openai_agent_turns import (
    run_named_worker_turn,
    run_question_planner_turn,
    run_supervisor_router_turn,
)


async def run_supervisor_session_azure(
    *,
    client: Any,
    supervisor_deployment: str,
    worker_deployment: str,
    critic_deployment: str,
    user_payload: str,
    session_id: str = "",
) -> dict[str, Any]:
    """Run the full multi-agent loop; return the same shape as ``runner_cli`` (pre-canonicalizer)."""
    state: dict[str, Any] = {}
    question = extract_user_text_from_payload(user_payload)
    pack = seed_session_state(state, question, max_supervisor_cycles=MAX_SUPERVISOR_CYCLES)
    _LOG.info(
        "session start: supervisor=%s worker=%s critic=%s cap=%d question=%r",
        supervisor_deployment,
        worker_deployment,
        critic_deployment,
        MAX_SUPERVISOR_CYCLES,
        question[:160] + ("…" if len(question) > 160 else ""),
    )
    session_started = time.perf_counter()

    # Cycle-0: question_planner runs once per session, before any supervisor or
    # analyzer turn. Provides the structured QuestionPlan that downstream roles
    # consume via {ah_question_plan_json}. Failure is non-fatal — downstream
    # prompts treat empty plan as "use heuristics" (Vertex parity).
    if not state.get(STATE_QUESTION_PLAN_KEY):
        _sync_prompt_keys(state, pack, mission="")
        try:
            await run_question_planner_turn(
                client=client,
                deployment=supervisor_deployment,
                state=state,
            )
        except Exception as exc:  # noqa: BLE001 - planner is optional scaffolding
            _LOG.warning("question_planner: best-effort failure: %s", exc)
        _sync_prompt_keys(state, pack, mission="")

    while True:
        pack = _load_pack(state)
        cycle = int(pack.cycle_index or 0) + 1
        cap = max(int(pack.supervisor_cap or MAX_SUPERVISOR_CYCLES), MAX_SUPERVISOR_CYCLES)
        if cycle > cap:
            _LOG.info("cycle cap reached (cycle=%d cap=%d); exiting loop", cycle, cap)
            break
        pack.cycle_index = cycle
        _dump_pack(state, pack)
        _sync_prompt_keys(state, pack, mission=str(state.get("ah_mission") or ""))
        _LOG.info("cycle %d/%d start", cycle, cap)

        last_critic = _safe_critic(state.get(STATE_CRITIC_OUTPUT_KEY))
        last_hints: list[EscalationHint] = []
        for key in (STATE_ANALYZER_OUTPUT_KEY, STATE_GOVERNANCE_OUTPUT_KEY, STATE_SANCTIONS_OUTPUT_KEY):
            hints = _escalation_hints_from_raw(state.get(key))
            if hints:
                last_hints.extend(hints)

        router_t0 = time.perf_counter()
        _LOG.info("cycle %d: supervisor_router call start", cycle)
        await run_supervisor_router_turn(client=client, deployment=supervisor_deployment, state=state)
        _LOG.info(
            "cycle %d: supervisor_router done in %.1fs",
            cycle,
            time.perf_counter() - router_t0,
        )

        raw_dec = state.get(STATE_SUPERVISOR_DECISION_KEY)
        dec = coerce_supervisor_decision(
            pack,
            cycle,
            raw_dec,
            last_critic,
            last_hints,
            max_supervisor_cycles=MAX_SUPERVISOR_CYCLES,
        )
        pack.research_temperature = dec.research_temperature
        _log_decision(pack, cycle, dec)
        state[STATE_SUPERVISOR_DECISION_KEY] = dec.model_dump(mode="json", by_alias=True)
        _dump_pack(state, pack)

        mission = dec.mission or ""
        state["ah_mission"] = mission
        _sync_prompt_keys(state, pack, mission=mission)
        _LOG.info(
            "cycle %d decision: next_agent=%s parallel=%s temperature=%.2f mission=%r",
            cycle,
            dec.next_agent,
            list(dec.parallel_agents or []),
            float(dec.research_temperature or 0.0),
            (mission[:200] + ("…" if len(mission) > 200 else "")),
        )

        if dec.next_agent == "finalize":
            gate_hit = _check_gates(pack, state, cycle)
            if gate_hit is not None:
                gate_name, retry_agent, directive = gate_hit
                _LOG.info(
                    "cycle %d finalize gate retry: gate=%s retry_agent=%s",
                    cycle,
                    gate_name,
                    retry_agent,
                )
                pack.gates_retried.append(gate_name)
                _log_gate(
                    pack,
                    cycle,
                    gate_name,
                    "retry",
                    rationale=directive,
                    decision=retry_agent,  # type: ignore[arg-type]
                )
                state["ah_mission"] = directive
                _sync_prompt_keys(state, pack, mission=directive)
                _dump_pack(state, pack)
                await run_named_worker_turn(
                    client=client,
                    deployment=worker_deployment,
                    agent_name=retry_agent,
                    state=state,
                )
                pack = _load_pack(state)
                out_key = {
                    "governance_control_analyzer": STATE_GOVERNANCE_OUTPUT_KEY,
                    "sanctions_analyzer": STATE_SANCTIONS_OUTPUT_KEY,
                }.get(retry_agent, STATE_ANALYZER_OUTPUT_KEY)
                origin = (
                    "governance"
                    if retry_agent == "governance_control_analyzer"
                    else "sanctions"
                    if retry_agent == "sanctions_analyzer"
                    else "analyzer"
                )
                _merge_analyzer_into_pack(pack, state.get(out_key), origin)
                _dump_pack(state, pack)
                continue
            _log_gate(pack, cycle, "", "pass", rationale="all gates passed", decision="finalize")
            _dump_pack(state, pack)
            _LOG.info("cycle %d: all gates passed; finalize", cycle)
            break

        worker_map = {
            "supply_chain_analyzer": (STATE_ANALYZER_OUTPUT_KEY, "analyzer"),
            "governance_control_analyzer": (STATE_GOVERNANCE_OUTPUT_KEY, "governance"),
            "sanctions_analyzer": (STATE_SANCTIONS_OUTPUT_KEY, "sanctions"),
        }
        if dec.next_agent in worker_map:
            out_key, origin = worker_map[dec.next_agent]
            t0 = time.perf_counter()
            _LOG.info("cycle %d: worker %s start", cycle, dec.next_agent)
            await run_named_worker_turn(
                client=client, deployment=worker_deployment, agent_name=dec.next_agent, state=state
            )
            _LOG.info(
                "cycle %d: worker %s done in %.1fs",
                cycle,
                dec.next_agent,
                time.perf_counter() - t0,
            )
            pack = _load_pack(state)
            _merge_analyzer_into_pack(pack, state.get(out_key), origin)
            _dump_pack(state, pack)
        elif dec.next_agent == "parallel_fanout":
            names = [n for n in (dec.parallel_agents or []) if n in _PARALLEL_OK]
            _LOG.info("cycle %d: parallel_fanout workers=%s", cycle, names)
            for n in names:
                t0 = time.perf_counter()
                _LOG.info("cycle %d: (fanout) worker %s start", cycle, n)
                await run_named_worker_turn(client=client, deployment=worker_deployment, agent_name=n, state=state)
                _LOG.info(
                    "cycle %d: (fanout) worker %s done in %.1fs",
                    cycle,
                    n,
                    time.perf_counter() - t0,
                )
                pack = _load_pack(state)
                if n == "supply_chain_analyzer":
                    _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY), "analyzer")
                elif n == "governance_control_analyzer":
                    _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY), "governance")
                elif n == "sanctions_analyzer":
                    _merge_analyzer_into_pack(pack, state.get(STATE_SANCTIONS_OUTPUT_KEY), "sanctions")
                _dump_pack(state, pack)
        elif dec.next_agent == "critic_agent":
            t0 = time.perf_counter()
            _LOG.info("cycle %d: critic start", cycle)
            await run_critic_azure_then_openrouter(
                client=client,
                critic_deployment=critic_deployment,
                worker_deployment=worker_deployment,
                state=state,
                cycle=cycle,
                session_id=session_id,
            )
            _LOG.info("cycle %d: critic done in %.1fs", cycle, time.perf_counter() - t0)
            try:
                last_critic2 = CriticVerdict.model_validate(state.get(STATE_CRITIC_OUTPUT_KEY))
            except Exception:
                last_critic2 = None
            if last_critic2 and last_critic2.sufficient:
                _LOG.info("cycle %d: critic sufficient=True; finishing", cycle)
                pack = _load_pack(state)
                pack.done = True
                _dump_pack(state, pack)
                break
            _LOG.info(
                "cycle %d: critic sufficient=%s; continue",
                cycle,
                bool(last_critic2.sufficient) if last_critic2 else None,
            )
        elif dec.next_agent == "red_team_agent":
            t0 = time.perf_counter()
            _LOG.info("cycle %d: red_team start", cycle)
            await run_named_worker_turn(
                client=client, deployment=worker_deployment, agent_name="red_team_agent", state=state
            )
            _LOG.info("cycle %d: red_team done in %.1fs", cycle, time.perf_counter() - t0)
            pack = _load_pack(state)
            _merge_red_team_temperature(pack, state.get(STATE_RED_TEAM_OUTPUT_KEY))
            if pack.extensions_pending_red_team:
                pack.extensions_pending_red_team = False
            _dump_pack(state, pack)

        pack = _load_pack(state)
        _dump_pack(state, pack)

    pack = _load_pack(state)
    final = build_final_answer_text(state, pack, max_supervisor_cycles=MAX_SUPERVISOR_CYCLES)
    state[STATE_FINAL_ANSWER_KEY] = final
    pack.final_answer = final
    pack.done = True
    _dump_pack(state, pack)
    _LOG.info(
        "session end in %.1fs; final_answer_chars=%d",
        time.perf_counter() - session_started,
        len(final or ""),
    )

    return {
        "finalAnswer": state.get(STATE_FINAL_ANSWER_KEY, ""),
        "pack": state.get(STATE_PACK_KEY, {}),
        "supervisorDecision": state.get(STATE_SUPERVISOR_DECISION_KEY, {}),
        "analyzerOutput": state.get(STATE_ANALYZER_OUTPUT_KEY, {}),
        "governanceOutput": state.get(STATE_GOVERNANCE_OUTPUT_KEY, {}),
        "sanctionsOutput": state.get(STATE_SANCTIONS_OUTPUT_KEY, {}),
        "criticOutput": state.get(STATE_CRITIC_OUTPUT_KEY, {}),
        "redTeamOutput": state.get(STATE_RED_TEAM_OUTPUT_KEY, {}),
        "toolLedger": state.get(STATE_TOOL_LEDGER_KEY, []),
    }
