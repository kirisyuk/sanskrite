"""v2 supervisor semantics: escalation hints, extend_cycle_cap, red-team gate,
parallel_fanout validation, idempotent merge, sanctions dispatch map."""

from __future__ import annotations

from agenthorde.agents.supervisor import (
    _ANALYZER_OUTPUT_KEY_FOR_AGENT,
    _apply_extend_cap,
    _coerce_decision,
    _escalation_hints_from_raw,
    _merge_analyzer_into_pack,
)
from agenthorde.config import (
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_DECISION_INFLUENCE_OUTPUT_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
)
from agenthorde_common.schemas.agents_io import EscalationHint, SupervisorDecision
from agenthorde_common.schemas.graph import ReasoningNode
from agenthorde_common.schemas.state import SessionPack


def _pack(**kw) -> SessionPack:
    d = {
        "user_question": "Q",
        "supervisor_cap": 8,
        "initial_supervisor_cap": 8,
    }
    d.update(kw)
    return SessionPack(**d)


# --- escalation hints ----------------------------------------------------


def test_escalation_hint_routes_to_sanctions() -> None:
    pack = _pack()
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    hint = EscalationHint(
        reason="shadow fleet suspected",
        suggested_agent="sanctions_analyzer",
        confidence=0.8,
    )
    raw = {
        "nextAgent": "supply_chain_analyzer",
        "mission": "m",
        "researchTemperature": 0.3,
    }
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[hint])
    assert dec.next_agent == "sanctions_analyzer"


def test_low_confidence_hint_is_ignored() -> None:
    pack = _pack()
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    hint = EscalationHint(
        reason="weak signal",
        suggested_agent="sanctions_analyzer",
        confidence=0.3,
    )
    raw = {"nextAgent": "supply_chain_analyzer", "mission": "m", "researchTemperature": 0.3}
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[hint])
    assert dec.next_agent == "supply_chain_analyzer"


def test_hints_from_raw_reads_both_camel_and_snake() -> None:
    raw = {
        "chainSummary": "",
        "proposedEntities": [],
        "proposedNodes": [],
        "proposedRelations": [],
        "openQuestions": [],
        "escalationHints": [
            {"reason": "r", "suggestedAgent": "sanctions_analyzer", "confidence": 0.75}
        ],
    }
    hints = _escalation_hints_from_raw(raw)
    assert len(hints) == 1
    assert hints[0].suggested_agent == "sanctions_analyzer"


# --- extend_cycle_cap / red-team gate ------------------------------------


def test_apply_extend_cap_grows_cap_and_sets_gate() -> None:
    pack = _pack(supervisor_cap=10, initial_supervisor_cap=10)
    applied = _apply_extend_cap(pack, 1)
    assert applied == 1
    assert pack.extensions_used == 1
    assert pack.supervisor_cap == 11
    assert pack.extensions_pending_red_team is True


def test_apply_extend_cap_caps_at_three() -> None:
    pack = _pack(supervisor_cap=10, initial_supervisor_cap=10, extensions_used=2)
    applied = _apply_extend_cap(pack, 5)
    assert applied == 1
    assert pack.extensions_used == 3
    # Further calls are no-ops.
    applied2 = _apply_extend_cap(pack, 1)
    assert applied2 == 0


def test_coerce_applies_extend_cap_from_decision() -> None:
    pack = _pack(supervisor_cap=10, initial_supervisor_cap=10)
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    raw = {
        "nextAgent": "supply_chain_analyzer",
        "mission": "m",
        "researchTemperature": 0.8,
        "extendCapBy": 2,
    }
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[])
    # Gate forces red_team_agent after extension.
    assert dec.next_agent == "red_team_agent"
    assert pack.extensions_used == 2
    assert pack.supervisor_cap == 12


def test_red_team_gate_blocks_other_agents_when_pending() -> None:
    pack = _pack()
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    pack.extensions_pending_red_team = True
    raw = {"nextAgent": "governance_control_analyzer", "mission": "m", "researchTemperature": 0.5}
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[])
    assert dec.next_agent == "red_team_agent"


# --- parallel_fanout validation ------------------------------------------


def test_parallel_fanout_without_agents_falls_back() -> None:
    pack = _pack()
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    raw = {
        "nextAgent": "parallel_fanout",
        "mission": "m",
        "researchTemperature": 0.3,
        "parallelAgents": [],
    }
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[])
    assert dec.next_agent == "supply_chain_analyzer"


def test_parallel_fanout_with_agents_is_preserved() -> None:
    pack = _pack()
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    raw = {
        "nextAgent": "parallel_fanout",
        "mission": "m",
        "researchTemperature": 0.3,
        "parallelAgents": ["supply_chain_analyzer", "sanctions_analyzer"],
    }
    dec = _coerce_decision(pack, cycle=2, raw=raw, last_critic=None, last_hints=[])
    assert dec.next_agent == "parallel_fanout"
    assert dec.parallel_agents == ["supply_chain_analyzer", "sanctions_analyzer"]


# --- idempotent merge ----------------------------------------------------


def test_merge_is_idempotent_on_same_output() -> None:
    pack = _pack()
    ao = {
        "chainSummary": "",
        "proposedEntities": [
            {"id": "e1", "label": "A"},
            {"id": "e2", "label": "B"},
        ],
        "proposedNodes": [
            {"id": "n1", "kind": "claim", "text": "same", "evidenceRefs": []}
        ],
        "proposedRelations": [
            {"srcId": "e1", "dstId": "e2", "relation": "supplies"},
        ],
        "openQuestions": [],
    }
    _merge_analyzer_into_pack(pack, ao, "analyzer")
    _merge_analyzer_into_pack(pack, ao, "analyzer")
    _merge_analyzer_into_pack(pack, ao, "analyzer")
    assert len(pack.reasoning_graph.nodes) == 1
    assert len(pack.proposed_relations) == 1
    assert pack.proposed_relations[0].src_id == "analyzer:e1"


def test_merge_keeps_richer_node_on_collision() -> None:
    pack = _pack()
    ao1 = {
        "chainSummary": "",
        "proposedEntities": [],
        "proposedNodes": [
            {"id": "n1", "kind": "claim", "text": "short", "evidenceRefs": []}
        ],
        "proposedRelations": [],
        "openQuestions": [],
    }
    ao2 = {
        "chainSummary": "",
        "proposedEntities": [],
        "proposedNodes": [
            {
                "id": "n1",
                "kind": "claim",
                "text": "a much longer and richer claim body",
                "evidenceRefs": ["https://example.com/a"],
            }
        ],
        "proposedRelations": [],
        "openQuestions": [],
    }
    _merge_analyzer_into_pack(pack, ao1, "analyzer")
    _merge_analyzer_into_pack(pack, ao2, "analyzer")
    assert len(pack.reasoning_graph.nodes) == 1
    winner = pack.reasoning_graph.nodes[0]
    assert winner.evidence_refs == ["https://example.com/a"]
    assert "richer" in winner.text


# --- dispatch map --------------------------------------------------------


def test_dispatch_map_covers_three_analyzers() -> None:
    assert _ANALYZER_OUTPUT_KEY_FOR_AGENT == {
        "supply_chain_analyzer": STATE_ANALYZER_OUTPUT_KEY,
        "governance_control_analyzer": STATE_GOVERNANCE_OUTPUT_KEY,
        "sanctions_analyzer": STATE_SANCTIONS_OUTPUT_KEY,
        "decision_influence_analyzer": STATE_DECISION_INFLUENCE_OUTPUT_KEY,
    }


def test_supervisor_decision_schema_accepts_new_fields() -> None:
    dec = SupervisorDecision(
        next_agent="parallel_fanout",
        mission="test",
        research_temperature=0.5,
        parallel_agents=["supply_chain_analyzer", "governance_control_analyzer"],
        extend_cap_by=1,
    )
    blob = dec.model_dump(by_alias=True)
    assert blob["parallelAgents"] == ["supply_chain_analyzer", "governance_control_analyzer"]
    assert blob["extendCapBy"] == 1
