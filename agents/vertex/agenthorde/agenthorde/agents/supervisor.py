from __future__ import annotations

# pyright: reportMissingImports=false

import asyncio
from collections.abc import AsyncGenerator
from typing import Any

from google.adk.agents import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event, EventActions
from google.genai import types as genai_types

from agenthorde.agents.workers import (
    critic_agent,
    governance_control_analyzer,
    question_planner,
    red_team_agent,
    sanctions_analyzer,
    supervisor_router,
    supply_chain_analyzer,
)
from agenthorde.config import MAX_SUPERVISOR_CYCLES, STATE_FINAL_ANSWER_KEY
from agenthorde.critic import run_critic_with_fallback
from agenthorde_common.orchestration.supervisor_core import (
    _ANALYZER_OUTPUT_KEY_FOR_AGENT,
    _apply_extend_cap,
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
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_QUESTION_PLAN_KEY,
    STATE_RED_TEAM_OUTPUT_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
    STATE_SUPERVISOR_DECISION_KEY,
    STATE_TOOL_LEDGER_KEY,
)
from agenthorde_common.schemas.agents_io import CriticVerdict, EscalationHint, SupervisorDecision
from agenthorde_common.schemas.state import SessionPack


def _extract_user_text(ctx: InvocationContext) -> str:
    parts: list[str] = []
    if ctx.user_content and ctx.user_content.parts:
        for p in ctx.user_content.parts:
            t = getattr(p, "text", None) or ""
            if t:
                parts.append(t)
    return extract_user_text_from_payload("\n".join(parts))


def _coerce_decision(
    pack: SessionPack,
    cycle: int,
    raw: Any,
    last_critic: CriticVerdict | None,
    last_hints: list[EscalationHint] | None = None,
) -> SupervisorDecision:
    """Tests and ADK code use this name; delegates to shared ``coerce_supervisor_decision``."""
    return coerce_supervisor_decision(
        pack,
        cycle,
        raw,
        last_critic,
        last_hints,
        max_supervisor_cycles=MAX_SUPERVISOR_CYCLES,
    )


class SupervisorAgent(BaseAgent):
    """Single supervisor loop with ADK sub-agents and typed SessionPack."""

    def _agent_for_name(self, name: str):
        if name == "supply_chain_analyzer":
            return supply_chain_analyzer
        if name == "governance_control_analyzer":
            return governance_control_analyzer
        if name == "sanctions_analyzer":
            return sanctions_analyzer
        if name == "red_team_agent":
            return red_team_agent
        if name == "critic_agent":
            return critic_agent
        return None

    async def _run_analyzer_and_merge(
        self,
        ctx: InvocationContext,
        agent_name: str,
    ) -> AsyncGenerator[Event, None]:
        agent = self._agent_for_name(agent_name)
        if agent is None:
            return
        async for event in agent.run_async(ctx):
            yield event
        output_key = _ANALYZER_OUTPUT_KEY_FOR_AGENT.get(agent_name)
        if not output_key:
            return
        pack = _load_pack(ctx.session.state)
        origin = (
            "analyzer"
            if agent_name == "supply_chain_analyzer"
            else "governance"
            if agent_name == "governance_control_analyzer"
            else "sanctions"
        )
        _merge_analyzer_into_pack(pack, ctx.session.state.get(output_key), origin)
        _dump_pack(ctx.session.state, pack)

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        state = ctx.session.state
        if not state.get(STATE_PACK_KEY):
            question = _extract_user_text(ctx)
            pack = seed_session_state(
                state, question, max_supervisor_cycles=MAX_SUPERVISOR_CYCLES
            )
            yield Event(
                author=self.name,
                actions=EventActions(
                    state_delta={
                        STATE_PACK_KEY: pack.model_dump(mode="json", by_alias=True),
                        STATE_TOOL_LEDGER_KEY: [],
                    }
                ),
            )
            _sync_prompt_keys(state, pack, mission="")

        # Cycle-0: question_planner runs once per session, before any analyzer
        # or supervisor decision. Provides the structured QuestionPlan that
        # downstream roles consume via {ah_question_plan_json}. Failure here is
        # non-fatal — downstream prompts treat empty plan as "use heuristics".
        if not state.get(STATE_QUESTION_PLAN_KEY):
            try:
                async for event in question_planner.run_async(ctx):
                    yield event
                _sync_prompt_keys(state, _load_pack(state), mission=str(state.get("ah_mission") or ""))
            except Exception:  # noqa: BLE001 - best-effort, planner is optional scaffolding
                pass

        while True:
            pack = _load_pack(state)
            cycle = int(pack.cycle_index or 0) + 1
            cap = max(int(pack.supervisor_cap or MAX_SUPERVISOR_CYCLES), MAX_SUPERVISOR_CYCLES)
            if cycle > cap:
                break
            pack.cycle_index = cycle
            _dump_pack(state, pack)
            _sync_prompt_keys(state, pack, mission=str(state.get("ah_mission") or ""))

            last_critic = _safe_critic(state.get(STATE_CRITIC_OUTPUT_KEY))
            # Collect hints from the most recent analyzer output (whichever wrote last).
            last_hints: list[EscalationHint] = []
            for key in (
                STATE_ANALYZER_OUTPUT_KEY,
                STATE_GOVERNANCE_OUTPUT_KEY,
                STATE_SANCTIONS_OUTPUT_KEY,
            ):
                hints = _escalation_hints_from_raw(state.get(key))
                if hints:
                    last_hints.extend(hints)

            async for event in supervisor_router.run_async(ctx):
                yield event

            raw_dec = state.get(STATE_SUPERVISOR_DECISION_KEY)

            dec = _coerce_decision(pack, cycle, raw_dec, last_critic, last_hints)
            pack.research_temperature = dec.research_temperature
            _log_decision(pack, cycle, dec)
            state[STATE_SUPERVISOR_DECISION_KEY] = dec.model_dump(mode="json", by_alias=True)
            _dump_pack(state, pack)

            mission = dec.mission or ""
            state["ah_mission"] = mission
            _sync_prompt_keys(state, pack, mission=mission)

            if dec.next_agent == "finalize":
                # Pre-finalize post-conditions: structural / evidence / open_questions.
                gate_hit = _check_gates(pack, state, cycle)
                if gate_hit is not None:
                    gate_name, retry_agent, directive = gate_hit
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
                    if retry_agent == "governance_control_analyzer":
                        async for event in governance_control_analyzer.run_async(ctx):
                            yield event
                        pack = _load_pack(state)
                        _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY), "governance")
                    elif retry_agent == "sanctions_analyzer":
                        async for event in sanctions_analyzer.run_async(ctx):
                            yield event
                        pack = _load_pack(state)
                        _merge_analyzer_into_pack(pack, state.get(STATE_SANCTIONS_OUTPUT_KEY), "sanctions")
                    else:
                        async for event in supply_chain_analyzer.run_async(ctx):
                            yield event
                        pack = _load_pack(state)
                        _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY), "analyzer")
                    _dump_pack(state, pack)
                    continue
                _log_gate(pack, cycle, "", "pass", rationale="all gates passed", decision="finalize")
                _dump_pack(state, pack)
                break

            if dec.next_agent == "supply_chain_analyzer":
                async for event in self._run_analyzer_and_merge(ctx, "supply_chain_analyzer"):
                    yield event
            elif dec.next_agent == "governance_control_analyzer":
                async for event in self._run_analyzer_and_merge(ctx, "governance_control_analyzer"):
                    yield event
            elif dec.next_agent == "sanctions_analyzer":
                async for event in self._run_analyzer_and_merge(ctx, "sanctions_analyzer"):
                    yield event
            elif dec.next_agent == "parallel_fanout":
                # ADK agents are not guaranteed thread-safe per-context, but each
                # run_async is an async generator tied to the shared context.
                # We fan out the *drains* concurrently and forward events to the
                # outer stream in completion order. If any sub-agent is missing,
                # we silently skip it.
                names = [n for n in (dec.parallel_agents or []) if self._agent_for_name(n)]
                if not names:
                    continue
                queue: asyncio.Queue[tuple[Event | None, str]] = asyncio.Queue()

                async def _drain(name: str) -> None:
                    try:
                        async for event in self._run_analyzer_and_merge(ctx, name):
                            await queue.put((event, name))
                    finally:
                        await queue.put((None, name))

                tasks = [asyncio.create_task(_drain(n)) for n in names]
                done_count = 0
                while done_count < len(names):
                    event, _name = await queue.get()
                    if event is None:
                        done_count += 1
                        continue
                    yield event
                await asyncio.gather(*tasks, return_exceptions=True)
            elif dec.next_agent == "critic_agent":
                async for event in run_critic_with_fallback(
                    ctx,
                    vertex_critic_agent=critic_agent,
                    cycle=cycle,
                    session_id=str(getattr(ctx.session, "id", "") or ""),
                ):
                    yield event
                try:
                    last_critic = CriticVerdict.model_validate(state.get(STATE_CRITIC_OUTPUT_KEY))
                except Exception:
                    last_critic = None
                if last_critic and last_critic.sufficient:
                    pack = _load_pack(state)
                    pack.done = True
                    _dump_pack(state, pack)
                    break
            elif dec.next_agent == "red_team_agent":
                async for event in red_team_agent.run_async(ctx):
                    yield event
                pack = _load_pack(state)
                _merge_red_team_temperature(pack, state.get(STATE_RED_TEAM_OUTPUT_KEY))
                # Consume the red-team gate when an extension was pending.
                if pack.extensions_pending_red_team:
                    pack.extensions_pending_red_team = False
                _dump_pack(state, pack)

            pack = _load_pack(state)
            _dump_pack(state, pack)

        # Deterministic finalize text
        pack = _load_pack(state)
        final = build_final_answer_text(
            state, pack, max_supervisor_cycles=MAX_SUPERVISOR_CYCLES
        )
        state[STATE_FINAL_ANSWER_KEY] = final
        pack.final_answer = final
        pack.done = True
        _dump_pack(state, pack)
        # InMemorySessionService persists session.state only from event state_delta.
        # Include the merged SessionPack so get_session() / runner_cli see reasoningGraph.
        yield Event(
            author=self.name,
            content=genai_types.Content(
                role="model",
                parts=[genai_types.Part(text=final)],
            ),
            actions=EventActions(
                state_delta={
                    STATE_FINAL_ANSWER_KEY: final,
                    STATE_PACK_KEY: pack.model_dump(mode="json", by_alias=True),
                }
            ),
        )


root_agent = SupervisorAgent(
    name="agenthorde_supervisor",
    description="Supervisor-led supply chain, governance/control, and sanctions reasoning with analyzers, critic, and red team.",
    sub_agents=[
        question_planner,
        supervisor_router,
        supply_chain_analyzer,
        governance_control_analyzer,
        sanctions_analyzer,
        critic_agent,
        red_team_agent,
    ],
)
