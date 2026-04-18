"""Supervisor merge/gate/coercion logic shared by Vertex ADK and Azure runtimes."""

from __future__ import annotations

import json
import math
from typing import Any

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
from agenthorde_common.schemas.agents_io import (
    AnalyzerOutput,
    CriticVerdict,
    EscalationHint,
    RedTeamOutput,
    SupervisorDecision,
)
from agenthorde_common.schemas.graph import ReasoningNode
from agenthorde_common.schemas.state import (
    HypothesisRecord,
    PackProposedEntity,
    PackProposedRelation,
    SessionPack,
    SupervisorTraceEntry,
)

def extract_user_text_from_payload(raw_user: str) -> str:
    raw = (raw_user or "").strip()
    if raw.startswith("{") and '"question"' in raw:
        try:
            obj = json.loads(raw)
            if isinstance(obj, dict) and obj.get("question"):
                return str(obj["question"]).strip()
        except json.JSONDecodeError:
            pass
    return raw


def seed_session_state(
    state: dict[str, Any],
    user_question: str,
    *,
    max_supervisor_cycles: int,
) -> SessionPack:
    """Create initial SessionPack and persist into ``state`` (same defaults as Vertex supervisor)."""
    pack = SessionPack(
        user_question=user_question,
        supervisor_cap=max_supervisor_cycles,
        initial_supervisor_cap=max_supervisor_cycles,
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
    _sync_prompt_keys(state, pack, mission="")
    return pack


def build_final_answer_text(state: dict[str, Any], pack: SessionPack, *, max_supervisor_cycles: int) -> str:
    """Deterministic finalize string from analyzer outputs (matches Vertex supervisor)."""
    _merge_analyzer_into_pack(pack, state.get(STATE_ANALYZER_OUTPUT_KEY), "analyzer")
    _merge_analyzer_into_pack(pack, state.get(STATE_GOVERNANCE_OUTPUT_KEY), "governance")
    _merge_analyzer_into_pack(pack, state.get(STATE_SANCTIONS_OUTPUT_KEY), "sanctions")
    _dump_pack(state, pack)
    ao_raw = state.get(STATE_ANALYZER_OUTPUT_KEY) or {}
    go_raw = state.get(STATE_GOVERNANCE_OUTPUT_KEY) or {}
    sx_raw = state.get(STATE_SANCTIONS_OUTPUT_KEY) or {}
    summary_sc = ""
    summary_gov = ""
    summary_sanc = ""
    try:
        summary_sc = AnalyzerOutput.model_validate(ao_raw).chain_summary
    except Exception:
        summary_sc = str(ao_raw) if ao_raw else ""
    try:
        summary_gov = AnalyzerOutput.model_validate(go_raw).chain_summary
    except Exception:
        summary_gov = str(go_raw) if go_raw else ""
    try:
        summary_sanc = AnalyzerOutput.model_validate(sx_raw).chain_summary
    except Exception:
        summary_sanc = str(sx_raw) if sx_raw else ""
    parts: list[str] = []
    if (summary_sc or "").strip():
        parts.append(summary_sc.strip())
    if (summary_gov or "").strip():
        parts.append("Governance & control:\n" + summary_gov.strip())
    if (summary_sanc or "").strip():
        parts.append("Sanctions & controls:\n" + summary_sanc.strip())
    summary = "\n\n".join(parts) if parts else "(no analyzer summary)"
    caveat = ""
    if pack.cycle_index >= max_supervisor_cycles and not pack.done:
        caveat = "\n\n[Stopped: supervisor cycle cap reached.]"
    return summary + caveat


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
    state["ah_sanctions_json"] = _json_compact(state.get(STATE_SANCTIONS_OUTPUT_KEY) or {})
    state["ah_red_team_json"] = _json_compact(state.get(STATE_RED_TEAM_OUTPUT_KEY) or {})
    state["ah_question_plan_json"] = _json_compact(state.get(STATE_QUESTION_PLAN_KEY) or {})


def _rel_key_ids(src_id: str, dst_id: str, relation: str) -> tuple[str, str, str]:
    return (
        (src_id or "").strip().lower(),
        (dst_id or "").strip().lower(),
        (relation or "").strip().lower(),
    )


def _namespace_entity_id(origin: str, local_id: str) -> str:
    """Prefix analyzer-local ids (e1, e2) with origin to avoid parallel_fanout collisions."""
    lid = (local_id or "").strip()
    if not lid:
        return ""
    for prefix in ("analyzer:", "governance:", "sanctions:", "pack:"):
        if lid.startswith(prefix):
            return lid
    return f"{origin}:{lid}"


def _node_richness(n: ReasoningNode) -> tuple[int, int]:
    """Rank nodes for idempotent merge: more evidence_refs > longer text wins."""
    refs = len(n.evidence_refs or [])
    txt = len(n.text or "")
    return (refs, txt)


def _merge_analyzer_into_pack(pack: SessionPack, raw: Any, origin: str) -> None:
    """Idempotent merge: repeated calls with the same output must not dup nodes
    or edges, and richer nodes (more evidence / longer text) win tie-breaks."""
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
        node = n if isinstance(n, ReasoningNode) else ReasoningNode.model_validate(n)
        prev = existing.get(node.id)
        if prev is None or _node_richness(node) > _node_richness(prev):
            existing[node.id] = node
    pack.reasoning_graph.nodes = list(existing.values())

    # Merge proposed_entities (registry) — dedup by namespacedId; longer label wins ties.
    ent_seen: dict[str, PackProposedEntity] = {e.namespaced_id: e for e in pack.proposed_entities}
    for pe in ao.proposed_entities:
        nid = _namespace_entity_id(origin, pe.id)
        if not nid:
            continue
        prev = ent_seen.get(nid)
        if prev is None or len((pe.label or "").strip()) > len((prev.label or "").strip()):
            ent_seen[nid] = PackProposedEntity(namespaced_id=nid, label=str(pe.label or "").strip())
    pack.proposed_entities = list(ent_seen.values())

    # Merge proposed_relations with dedup on (srcId, dstId, relation).
    seen: dict[tuple[str, str, str], PackProposedRelation] = {
        _rel_key_ids(r.src_id, r.dst_id, r.relation): r for r in pack.proposed_relations
    }
    for pr in ao.proposed_relations:
        sid = _namespace_entity_id(origin, pr.src_id)
        did = _namespace_entity_id(origin, pr.dst_id)
        k = _rel_key_ids(sid, did, pr.relation)
        if k in seen:
            # Keep the richer attribute bag; prior entry wins ties so merges stay stable.
            cur = seen[k]
            if (len(pr.attributes_as_dict()) > len(cur.attributes or {})) or (
                len(pr.source_claim_ids or []) > len(cur.source_claim_ids or [])
            ):
                seen[k] = PackProposedRelation(
                    src_id=sid,
                    dst_id=did,
                    relation=pr.relation,
                    attributes=dict(pr.attributes_as_dict()),
                    source_claim_ids=list(pr.source_claim_ids or []),
                )
            continue
        seen[k] = PackProposedRelation(
            src_id=sid,
            dst_id=did,
            relation=pr.relation,
            attributes=dict(pr.attributes_as_dict()),
            source_claim_ids=list(pr.source_claim_ids or []),
        )
    pack.proposed_relations = list(seen.values())


def _escalation_hints_from_raw(raw: Any) -> list[EscalationHint]:
    if not raw:
        return []
    if hasattr(raw, "model_dump"):
        raw = raw.model_dump(mode="json", by_alias=True)
    try:
        ao = AnalyzerOutput.model_validate(raw)
    except Exception:
        return []
    return list(ao.escalation_hints or [])


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
    if dec.next_agent in (
        "supply_chain_analyzer",
        "governance_control_analyzer",
        "sanctions_analyzer",
    ):
        pack.last_intent = (
            "supply_chain"
            if dec.next_agent == "supply_chain_analyzer"
            else "governance"
            if dec.next_agent == "governance_control_analyzer"
            else "sanctions"
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
    if intent == "sanctions":
        return "sanctions_analyzer"
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
        if intent in ("supply_chain", "governance", "sanctions") and len(pack.proposed_relations) == 0:
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


def _apply_extend_cap(pack: SessionPack, extend_by: int) -> int:
    """Apply a supervisor-requested cap extension.

    ``extend_by`` is clamped to the remaining budget (max 3 extensions per run
    in total; each one adds ``ceil(initial_supervisor_cap * 0.1)`` cycles).
    Returns the number of extensions actually applied this call (0..3).
    """
    if extend_by <= 0:
        return 0
    remaining = max(0, 3 - int(pack.extensions_used or 0))
    applied = min(extend_by, remaining)
    if applied <= 0:
        return 0
    base = max(1, int(pack.initial_supervisor_cap or pack.supervisor_cap or 1))
    step = max(1, math.ceil(base * 0.1))
    pack.supervisor_cap = int(pack.supervisor_cap) + step * applied
    pack.extensions_used = int(pack.extensions_used or 0) + applied
    pack.extensions_pending_red_team = True
    return applied


def coerce_supervisor_decision(
    pack: SessionPack,
    cycle: int,
    raw: Any,
    last_critic: CriticVerdict | None,
    last_hints: list[EscalationHint] | None,
    *,
    max_supervisor_cycles: int,
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
    # Apply any requested cap extensions first (once per cycle). If the LLM
    # requests an extension, the red-team gate below will force red_team_agent
    # on this very decision.
    if getattr(dec, "extend_cap_by", 0):
        _apply_extend_cap(pack, int(dec.extend_cap_by))

    # Mandatory red-team gate after extension.
    if pack.extensions_pending_red_team and dec.next_agent not in ("red_team_agent", "finalize"):
        dec = SupervisorDecision(
            next_agent="red_team_agent",
            mission="Cycle-cap extended: red-team the current framing before spending more cycles.",
            research_temperature=max(pack.research_temperature, 0.7),
            rationale="Coerced: extend_cycle_cap was used; red-team gate mandated.",
        )

    # Honor escalationHints from the last analyzer output, unless a stronger
    # gate already forced the decision. Keep it advisory: the router may have
    # already chosen a better path this cycle.
    if (
        last_hints
        and dec.next_agent in ("supply_chain_analyzer", "governance_control_analyzer")
        and not pack.extensions_pending_red_team
    ):
        top = max(last_hints, key=lambda h: float(h.confidence or 0.0))
        if top.suggested_agent != dec.next_agent and float(top.confidence or 0.0) >= 0.5:
            dec = SupervisorDecision(
                next_agent=top.suggested_agent,
                mission=dec.mission or f"Escalation: {top.reason}",
                research_temperature=dec.research_temperature,
                rationale=f"Coerced: honoring analyzer escalationHint → {top.suggested_agent}.",
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
    cap = max(pack.supervisor_cap or max_supervisor_cycles, max_supervisor_cycles)
    if cycle >= cap - 1 and dec.next_agent not in (
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
    # Final guard: parallel_fanout requires parallelAgents; otherwise fall back
    # to the router's best single-agent choice.
    if dec.next_agent == "parallel_fanout" and not dec.parallel_agents:
        dec = SupervisorDecision(
            next_agent="supply_chain_analyzer",
            mission=dec.mission or "Fallback from empty parallel_fanout.",
            research_temperature=dec.research_temperature,
            rationale="Coerced: parallel_fanout requested without parallelAgents.",
        )
    return dec


_ANALYZER_OUTPUT_KEY_FOR_AGENT: dict[str, str] = {
    "supply_chain_analyzer": STATE_ANALYZER_OUTPUT_KEY,
    "governance_control_analyzer": STATE_GOVERNANCE_OUTPUT_KEY,
    "sanctions_analyzer": STATE_SANCTIONS_OUTPUT_KEY,
}


