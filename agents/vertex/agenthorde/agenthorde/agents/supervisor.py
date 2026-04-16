from __future__ import annotations

# pyright: reportMissingImports=false

import json
from collections.abc import AsyncGenerator
from typing import Any

from google.adk.agents import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event, EventActions
from google.genai import types as genai_types

from agenthorde.agents.workers import (
    critic_agent,
    governance_control_analyzer,
    red_team_agent,
    supervisor_router,
    supply_chain_analyzer,
)
from agenthorde.config import (
    MAX_SUPERVISOR_CYCLES,
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_CRITIC_OUTPUT_KEY,
    STATE_FINAL_ANSWER_KEY,
    STATE_PACK_KEY,
    STATE_RED_TEAM_OUTPUT_KEY,
    STATE_SUPERVISOR_DECISION_KEY,
    STATE_TOOL_LEDGER_KEY,
)
from agenthorde.schemas.agents_io import (
    AnalyzerOutput,
    CriticVerdict,
    RedTeamOutput,
    SupervisorDecision,
)
from agenthorde.schemas.graph import ReasoningNode
from agenthorde.schemas.state import HypothesisRecord, SessionPack


def _extract_user_text(ctx: InvocationContext) -> str:
    parts: list[str] = []
    if ctx.user_content and ctx.user_content.parts:
        for p in ctx.user_content.parts:
            t = getattr(p, "text", None) or ""
            if t:
                parts.append(t)
    raw = "\n".join(parts).strip()
    if raw.startswith("{") and '"question"' in raw:
        try:
            obj = json.loads(raw)
            if isinstance(obj, dict) and obj.get("question"):
                return str(obj["question"]).strip()
        except json.JSONDecodeError:
            pass
    return raw


def _safe_critic(raw: Any) -> CriticVerdict | None:
    if not raw:
        return None
    try:
        return CriticVerdict.model_validate(raw)
    except Exception:
        return None


def _json_compact(data: Any, limit: int = 14000) -> str:
    try:
        s = json.dumps(data, ensure_ascii=False, default=str)
    except TypeError:
        s = str(data)
    if len(s) > limit:
        return s[: limit - 20] + "\n... [truncated]"
    return s


def _load_pack(state: dict[str, Any]) -> SessionPack:
    raw = state.get(STATE_PACK_KEY) or {}
    if isinstance(raw, SessionPack):
        return raw
    return SessionPack.model_validate(raw)


def _dump_pack(state: dict[str, Any], pack: SessionPack) -> None:
    state[STATE_PACK_KEY] = pack.model_dump(mode="json", by_alias=True)


def _sync_prompt_keys(state: dict[str, Any], pack: SessionPack, mission: str = "") -> None:
    state["ah_user_question"] = pack.user_question
    state["ah_pack_json"] = _json_compact(pack.model_dump(mode="json", by_alias=True))
    state["ah_mission"] = mission
    state["ah_critic_json"] = _json_compact(state.get(STATE_CRITIC_OUTPUT_KEY) or {})
    state["ah_analyzer_json"] = _json_compact(state.get(STATE_ANALYZER_OUTPUT_KEY) or {})
    state["ah_governance_json"] = _json_compact(state.get(STATE_GOVERNANCE_OUTPUT_KEY) or {})
    state["ah_red_team_json"] = _json_compact(state.get(STATE_RED_TEAM_OUTPUT_KEY) or {})


def _merge_analyzer_into_pack(pack: SessionPack, raw: Any) -> None:
    if raw is None:
        return
    if hasattr(raw, "model_dump"):
        raw = raw.model_dump(mode="json", by_alias=True)
    try:
        ao = AnalyzerOutput.model_validate(raw)
    except Exception:
        return
    existing = {n.id: n for n in pack.reasoning_graph.nodes}
    for n in ao.proposed_nodes:
        if isinstance(n, ReasoningNode):
            existing[n.id] = n
        else:
            node = ReasoningNode.model_validate(n)
            existing[node.id] = node
    pack.reasoning_graph.nodes = list(existing.values())


def _merge_red_team_temperature(pack: SessionPack, raw: Any) -> None:
    try:
        rt = RedTeamOutput.model_validate(raw)
    except Exception:
        return
    pack.research_temperature = max(0.0, min(1.0, pack.research_temperature + rt.suggested_temperature_delta))
    note = (rt.reframe_notes or "").strip()
    if note:
        rid = f"rt-{len(pack.reasoning_graph.nodes)}"
        pack.reasoning_graph.nodes.append(
            ReasoningNode(id=rid, kind="counter", text=note, hypothesis_id="", evidence_refs=[])
        )


def _coerce_decision(
    pack: SessionPack,
    cycle: int,
    raw: Any,
    last_critic: CriticVerdict | None,
) -> SupervisorDecision:
    try:
        dec = SupervisorDecision.model_validate(raw)
    except Exception:
        dec = SupervisorDecision(
            next_agent="supply_chain_analyzer",
            mission="Initial supply-chain pass from user question.",
            research_temperature=pack.research_temperature,
            rationale="Fallback: invalid supervisor JSON.",
        )
    if not pack.reasoning_graph.nodes and dec.next_agent == "finalize":
        # Do not pick supply vs governance here — only the supervisor router (LLM) decides.
        # This branch only blocks a premature finalize; next cycle the router chooses the worker.
        dec = SupervisorDecision(
            next_agent="supply_chain_analyzer",
            mission=dec.mission or "Build an initial chain sketch before finalize.",
            research_temperature=dec.research_temperature,
            rationale="Coerced: cannot finalize with empty reasoning graph.",
        )
    if cycle >= MAX_SUPERVISOR_CYCLES - 1 and dec.next_agent not in (
        "critic_agent",
        "finalize",
    ):
        dec = SupervisorDecision(
            next_agent="critic_agent",
            mission="Budget nearly exhausted: strict sufficiency check.",
            research_temperature=pack.research_temperature,
            rationale="Coerced: hard budget pressure toward critic or finalize.",
        )
    if last_critic and last_critic.sufficient and dec.next_agent not in ("finalize", "red_team_agent"):
        dec = SupervisorDecision(
            next_agent="finalize",
            mission="",
            research_temperature=pack.research_temperature,
            rationale="Coerced: critic already marked sufficient.",
        )
    return dec


class SupervisorAgent(BaseAgent):
    """Single supervisor loop with ADK sub-agents and typed SessionPack."""

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        state = ctx.session.state
        if not state.get(STATE_PACK_KEY):
            question = _extract_user_text(ctx)
            pack = SessionPack(
                user_question=question,
                supervisor_cap=MAX_SUPERVISOR_CYCLES,
                success_criteria_notes="Answer the user with explicit chain logic and stated uncertainty.",
                success_checklist=[
                    "Named key entities or product scopes where possible",
                    "Separated stable facts vs time-sensitive claims",
                    "Noted missing evidence or graph gaps",
                ],
                hypotheses={
                    "H0": HypothesisRecord(hypothesis_id="H0", title="Primary interpretation", status="active")
                },
            )
            _dump_pack(state, pack)
            if state.get(STATE_TOOL_LEDGER_KEY) is None:
                state[STATE_TOOL_LEDGER_KEY] = []
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

        for cycle in range(1, MAX_SUPERVISOR_CYCLES + 1):
            pack = _load_pack(state)
            pack.cycle_index = cycle
            _dump_pack(state, pack)
            _sync_prompt_keys(state, pack, mission=str(state.get("ah_mission") or ""))

            last_critic = _safe_critic(state.get(STATE_CRITIC_OUTPUT_KEY))

            async for event in supervisor_router.run_async(ctx):
                yield event

            raw_dec = state.get(STATE_SUPERVISOR_DECISION_KEY)

            dec = _coerce_decision(pack, cycle, raw_dec, last_critic)
            pack.research_temperature = dec.research_temperature
            state[STATE_SUPERVISOR_DECISION_KEY] = dec.model_dump(mode="json", by_alias=True)
            _dump_pack(state, pack)

            mission = dec.mission or ""
            state["ah_mission"] = mission
            _sync_prompt_keys(state, pack, mission=mission)

            if dec.next_agent == "finalize":
                break

            if dec.next_agent == "supply_chain_analyzer":
                async for event in supply_chain_analyzer.run_async(ctx):
                    yield event
                pack = _load_pack(state)
                _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY))
                _dump_pack(state, pack)
            elif dec.next_agent == "governance_control_analyzer":
                async for event in governance_control_analyzer.run_async(ctx):
                    yield event
                pack = _load_pack(state)
                _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY))
                _dump_pack(state, pack)
            elif dec.next_agent == "critic_agent":
                async for event in critic_agent.run_async(ctx):
                    yield event
                try:
                    last_critic = CriticVerdict.model_validate(state.get(STATE_CRITIC_OUTPUT_KEY))
                except Exception:
                    last_critic = None
                if last_critic and last_critic.sufficient:
                    pack.done = True
                    _dump_pack(state, pack)
                    break
            elif dec.next_agent == "red_team_agent":
                async for event in red_team_agent.run_async(ctx):
                    yield event
                pack = _load_pack(state)
                _merge_red_team_temperature(pack, state.get(STATE_RED_TEAM_OUTPUT_KEY))
                _dump_pack(state, pack)

            pack = _load_pack(state)
            _dump_pack(state, pack)

        # Deterministic finalize text
        pack = _load_pack(state)
        # Ensure reasoning_graph reflects last analyzer nodes (session merges may lag behind).
        _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY))
        _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY))
        _dump_pack(state, pack)
        ao_raw = state.get(STATE_ANALYZER_OUTPUT_KEY) or {}
        go_raw = state.get(STATE_GOVERNANCE_OUTPUT_KEY) or {}
        summary_sc = ""
        summary_gov = ""
        try:
            summary_sc = AnalyzerOutput.model_validate(ao_raw).chain_summary
        except Exception:
            summary_sc = str(ao_raw) if ao_raw else ""
        try:
            summary_gov = AnalyzerOutput.model_validate(go_raw).chain_summary
        except Exception:
            summary_gov = str(go_raw) if go_raw else ""
        parts: list[str] = []
        if (summary_sc or "").strip():
            parts.append(summary_sc.strip())
        if (summary_gov or "").strip():
            parts.append("Governance & control:\n" + summary_gov.strip())
        summary = "\n\n".join(parts) if parts else "(no analyzer summary)"
        caveat = ""
        if pack.cycle_index >= MAX_SUPERVISOR_CYCLES and not pack.done:
            caveat = "\n\n[Stopped: supervisor cycle cap reached.]"
        final = summary + caveat
        state[STATE_FINAL_ANSWER_KEY] = final
        pack.final_answer = final
        pack.done = True
        _dump_pack(state, pack)
        yield Event(
            author=self.name,
            content=genai_types.Content(
                role="model",
                parts=[genai_types.Part(text=final)],
            ),
            actions=EventActions(state_delta={STATE_FINAL_ANSWER_KEY: final}),
        )


root_agent = SupervisorAgent(
    name="agenthorde_supervisor",
    description="Supervisor-led supply chain and governance/control reasoning with analyzers, critic, and red team.",
    sub_agents=[
        supervisor_router,
        supply_chain_analyzer,
        governance_control_analyzer,
        critic_agent,
        red_team_agent,
    ],
)
