"""Pre-finalize gate tests (supervisor_trace, structural/evidence/open_questions)."""

from __future__ import annotations

from agenthorde.agents.supervisor import (
    _check_gates,
    _coerce_decision,
    _log_decision,
    _log_gate,
    _merge_analyzer_into_pack,
)
from agenthorde.config import (
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_TOOL_LEDGER_KEY,
)
from agenthorde.schemas.agents_io import SupervisorDecision
from agenthorde.schemas.graph import ReasoningNode
from agenthorde.schemas.state import (
    PackProposedRelation,
    SessionPack,
)


def _make_pack(**kwargs) -> SessionPack:
    defaults = {"user_question": "Q", "supervisor_cap": 8}
    defaults.update(kwargs)
    return SessionPack(**defaults)


def test_structural_gate_fires_when_no_relations_after_supply_chain() -> None:
    pack = _make_pack(last_intent="supply_chain")
    pack.reasoning_graph.nodes = [
        ReasoningNode(id="n1", kind="claim", text="x")
    ]
    state: dict[str, object] = {STATE_TOOL_LEDGER_KEY: []}
    hit = _check_gates(pack, state, cycle=2)
    assert hit is not None
    gate, agent, directive = hit
    assert gate == "structural"
    assert agent == "supply_chain_analyzer"
    assert "STRUCTURAL GAP" in directive


def test_structural_gate_skipped_when_relations_present() -> None:
    pack = _make_pack(last_intent="supply_chain")
    pack.proposed_relations = [
        PackProposedRelation(src="A", dst="B", relation="supplies")
    ]
    state: dict[str, object] = {STATE_TOOL_LEDGER_KEY: []}
    # evidence gate wouldn't fire (no sources), open_questions wouldn't (empty ao).
    hit = _check_gates(pack, state, cycle=2)
    assert hit is None


def test_evidence_gate_fires_when_refs_missing_and_sources_exist() -> None:
    pack = _make_pack(last_intent="governance")
    pack.proposed_relations = [
        PackProposedRelation(src="A", dst="B", relation="subsidiary_of")
    ]
    pack.reasoning_graph.nodes = [
        ReasoningNode(id="n1", kind="claim", text="x", evidence_refs=[])
    ]
    state = {
        STATE_TOOL_LEDGER_KEY: [
            {
                "tool": "web_search",
                "ok": True,
                "payload": {
                    "answer": "stuff",
                    "sources": [
                        {"url": "https://sec.gov/a", "title": "10-K", "domain": "sec.gov"}
                    ],
                },
            }
        ]
    }
    hit = _check_gates(pack, state, cycle=2)
    assert hit is not None
    gate, agent, directive = hit
    assert gate == "evidence"
    assert agent == "governance_control_analyzer"
    assert "EVIDENCE GAP" in directive


def test_evidence_gate_not_fired_when_all_refs_filled() -> None:
    pack = _make_pack(last_intent="supply_chain")
    pack.proposed_relations = [
        PackProposedRelation(src="A", dst="B", relation="supplies")
    ]
    pack.reasoning_graph.nodes = [
        ReasoningNode(
            id="n1",
            kind="claim",
            text="x",
            evidence_refs=["https://example.com/a"],
        )
    ]
    state = {
        STATE_TOOL_LEDGER_KEY: [
            {
                "tool": "web_search",
                "ok": True,
                "payload": {
                    "answer": "a",
                    "sources": [{"url": "https://example.com/a", "domain": "example.com"}],
                },
            }
        ]
    }
    hit = _check_gates(pack, state, cycle=2)
    assert hit is None


def test_open_questions_gate_fires_when_room_in_budget() -> None:
    pack = _make_pack(last_intent="supply_chain", supervisor_cap=8)
    pack.proposed_relations = [
        PackProposedRelation(src="A", dst="B", relation="supplies")
    ]
    pack.reasoning_graph.nodes = [
        ReasoningNode(
            id="n1", kind="claim", text="x", evidence_refs=["prior_knowledge"]
        )
    ]
    state = {
        STATE_TOOL_LEDGER_KEY: [],
        STATE_ANALYZER_OUTPUT_KEY: {
            "chainSummary": "",
            "proposedNodes": [],
            "openQuestions": ["What is X?", "Who owns Y?", "When Z?"],
        },
    }
    hit = _check_gates(pack, state, cycle=2)
    assert hit is not None
    gate, _agent, directive = hit
    assert gate == "open_questions"
    assert "What is X?" in directive


def test_open_questions_gate_skipped_near_budget_cap() -> None:
    pack = _make_pack(last_intent="supply_chain", supervisor_cap=8)
    pack.proposed_relations = [
        PackProposedRelation(src="A", dst="B", relation="supplies")
    ]
    pack.reasoning_graph.nodes = [
        ReasoningNode(id="n1", kind="claim", text="x", evidence_refs=["prior_knowledge"])
    ]
    state = {
        STATE_TOOL_LEDGER_KEY: [],
        STATE_ANALYZER_OUTPUT_KEY: {
            "chainSummary": "",
            "proposedNodes": [],
            "openQuestions": ["q1"],
        },
    }
    # cycle = cap - 1 → gate skipped.
    hit = _check_gates(pack, state, cycle=7)
    assert hit is None


def test_gate_only_retries_once_via_gates_retried() -> None:
    pack = _make_pack(last_intent="supply_chain")
    pack.reasoning_graph.nodes = [ReasoningNode(id="n1", kind="claim", text="x")]
    pack.gates_retried.append("structural")
    state: dict[str, object] = {STATE_TOOL_LEDGER_KEY: []}
    # structural already retried; no other gate triggered → None.
    hit = _check_gates(pack, state, cycle=2)
    assert hit is None


def test_log_decision_updates_last_intent_for_analyzer_pick() -> None:
    pack = _make_pack()
    dec = SupervisorDecision(
        next_agent="governance_control_analyzer",
        mission="m",
        rationale="r",
    )
    _log_decision(pack, 1, dec)
    assert pack.last_intent == "governance"
    assert len(pack.supervisor_trace) == 1
    entry = pack.supervisor_trace[0]
    assert entry.decision == "governance_control_analyzer"
    assert entry.rationale == "r"
    assert entry.gate == ""


def test_log_gate_writes_trace_entry() -> None:
    pack = _make_pack()
    _log_gate(pack, 2, "structural", "retry", rationale="gap", decision="supply_chain_analyzer")
    assert len(pack.supervisor_trace) == 1
    entry = pack.supervisor_trace[0]
    assert entry.gate == "structural"
    assert entry.gate_action == "retry"
    assert entry.decision == "supply_chain_analyzer"


def test_session_pack_backward_compat_empty_fields() -> None:
    """Old session blobs without the new fields must still validate."""
    legacy = {
        "userQuestion": "Q",
        "supervisorCap": 8,
        "reasoningGraph": {"nodes": [], "edges": []},
    }
    pack = SessionPack.model_validate(legacy)
    assert pack.proposed_relations == []
    assert pack.supervisor_trace == []
    assert pack.last_intent == ""
    assert pack.gates_retried == []


def test_merge_analyzer_fills_proposed_relations_with_dedup() -> None:
    pack = _make_pack()
    ao1 = {
        "chainSummary": "",
        "proposedNodes": [],
        "proposedRelations": [
            {"src": "A", "dst": "B", "relation": "supplies"},
            {"src": "C", "dst": "D", "relation": "produces"},
        ],
        "openQuestions": [],
    }
    ao2 = {
        "chainSummary": "",
        "proposedNodes": [],
        "proposedRelations": [
            {"src": "A", "dst": "B", "relation": "supplies"},  # dup
            {"src": "E", "dst": "F", "relation": "operates"},
        ],
        "openQuestions": [],
    }
    _merge_analyzer_into_pack(pack, ao1)
    _merge_analyzer_into_pack(pack, ao2)
    keys = {(r.src, r.dst, r.relation) for r in pack.proposed_relations}
    assert keys == {("A", "B", "supplies"), ("C", "D", "produces"), ("E", "F", "operates")}


def test_coerce_decision_fallback_is_logged_when_called_from_outer_loop() -> None:
    """Sanity check: SupervisorDecision defaults via _coerce_decision are valid for _log_decision."""
    pack = _make_pack()
    dec = _coerce_decision(pack, cycle=1, raw=None, last_critic=None)
    # With empty reasoning graph the coerce path avoids finalize.
    assert dec.next_agent != "finalize"
    _log_decision(pack, 1, dec)
    assert len(pack.supervisor_trace) == 1
