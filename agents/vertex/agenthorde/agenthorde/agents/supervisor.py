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
from agenthorde.schemas.state import (
    HypothesisRecord,
    PackProposedRelation,
    SessionPack,
    SupervisorTraceEntry,
)


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


def _rel_key(src: str, dst: str, relation: str) -> tuple[str, str, str]:
    return (
        (src or "").strip().lower(),
        (dst or "").strip().lower(),
        (relation or "").strip().lower(),
    )


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

    # Merge proposed_relations with dedup on (src, dst, relation).
    seen: dict[tuple[str, str, str], PackProposedRelation] = {
        _rel_key(r.src, r.dst, r.relation): r for r in pack.proposed_relations
    }
    for pr in ao.proposed_relations:
        k = _rel_key(pr.src, pr.dst, pr.relation)
        if k in seen:
            continue
        seen[k] = PackProposedRelation(
            src=pr.src,
            dst=pr.dst,
            relation=pr.relation,
            attributes=dict(pr.attributes or {}),
            source_claim_ids=list(pr.source_claim_ids or []),
        )
    pack.proposed_relations = list(seen.values())


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


def _log_decision(pack: SessionPack, cycle: int, dec: SupervisorDecision) -> None:
    pack.supervisor_trace.append(
        SupervisorTraceEntry(
            cycle=cycle,
            decision=dec.next_agent,
            rationale=dec.rationale or "",
        )
    )
    if dec.next_agent in ("supply_chain_analyzer", "governance_control_analyzer"):
        pack.last_intent = (
            "supply_chain"
            if dec.next_agent == "supply_chain_analyzer"
            else "governance"
        )


def _log_gate(
    pack: SessionPack,
    cycle: int,
    gate: str,
    action: str,
    rationale: str = "",
    decision: Any = "finalize",
) -> None:
    pack.supervisor_trace.append(
        SupervisorTraceEntry(
            cycle=cycle,
            decision=decision,  # type: ignore[arg-type]
            rationale=rationale,
            gate=gate,  # type: ignore[arg-type]
            gate_action=action,  # type: ignore[arg-type]
        )
    )


def _node_has_empty_refs(n: Any) -> bool:
    refs = []
    if isinstance(n, ReasoningNode):
        refs = n.evidence_refs or []
    elif isinstance(n, dict):
        refs = n.get("evidenceRefs") or n.get("evidence_refs") or []
    return not refs


def _tool_ledger_has_sources(state: dict[str, Any]) -> bool:
    ledger = state.get(STATE_TOOL_LEDGER_KEY) or []
    for entry in ledger:
        if not isinstance(entry, dict):
            continue
        if entry.get("tool") != "web_search" or not entry.get("ok"):
            continue
        payload = entry.get("payload") or {}
        if payload.get("sources"):
            return True
    return False


def _pick_retry_agent_for_intent(pack: SessionPack) -> str:
    intent = (pack.last_intent or "").strip()
    if intent == "governance":
        return "governance_control_analyzer"
    return "supply_chain_analyzer"


def _check_gates(
    pack: SessionPack,
    state: dict[str, Any],
    cycle: int,
) -> tuple[str, str, str] | None:
    """Return (gate_name, retry_agent, directive) if a gate triggers a retry, else None.

    Strict one-retry-per-gate guard via pack.gates_retried.
    """
    # structural gate.
    if "structural" not in pack.gates_retried:
        intent = (pack.last_intent or "").strip()
        if intent in ("supply_chain", "governance") and len(pack.proposed_relations) == 0:
            if cycle < pack.supervisor_cap:
                return (
                    "structural",
                    _pick_retry_agent_for_intent(pack),
                    "STRUCTURAL GAP: no proposedRelations from prior cycle. "
                    "Re-read claims and emit explicit proposedRelations using the AH relations vocabulary "
                    "(supplies, produces, operates, transships_at, passes_through, listed_on, subsidiary_of, beneficially_owns, ...).",
                )

    # evidence gate.
    if "evidence" not in pack.gates_retried:
        has_empty = any(_node_has_empty_refs(n) for n in pack.reasoning_graph.nodes)
        if has_empty and _tool_ledger_has_sources(state) and cycle < pack.supervisor_cap:
            return (
                "evidence",
                _pick_retry_agent_for_intent(pack),
                "EVIDENCE GAP: attach source URLs from toolLedger[*].payload.sources[*].url "
                "to each proposedNodes[i].evidenceRefs. Use 'prior_knowledge' only for textbook claims.",
            )

    # open_questions gate — only when we still have room to act on findings.
    if "open_questions" not in pack.gates_retried:
        ao_raw = state.get(STATE_ANALYZER_OUTPUT_KEY) or {}
        open_q: list[str] = []
        if isinstance(ao_raw, dict):
            open_q = ao_raw.get("openQuestions") or ao_raw.get("open_questions") or []
        if open_q and cycle < pack.supervisor_cap - 1:
            top3 = "; ".join(str(q) for q in open_q[:3])
            return (
                "open_questions",
                _pick_retry_agent_for_intent(pack),
                f"OPEN QUESTIONS: run targeted web_search on: {top3}. "
                "Attach resulting source URLs to proposedNodes[i].evidenceRefs.",
            )

    return None


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
                        _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY))
                    else:
                        async for event in supply_chain_analyzer.run_async(ctx):
                            yield event
                        pack = _load_pack(state)
                        _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY))
                    _dump_pack(state, pack)
                    continue
                _log_gate(pack, cycle, "", "pass", rationale="all gates passed", decision="finalize")
                _dump_pack(state, pack)
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
    description="Supervisor-led supply chain and governance/control reasoning with analyzers, critic, and red team.",
    sub_agents=[
        supervisor_router,
        supply_chain_analyzer,
        governance_control_analyzer,
        critic_agent,
        red_team_agent,
    ],
)
