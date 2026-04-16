"""Structured investigation todos: seeding and deterministic status updates."""

from __future__ import annotations

import uuid
from typing import Any

from agentloom.contracts import InvestigationPreflight, InvestigationRole, InvestigationTodoItem, InvestigationTodoPhase, QuestionIntent
from agentloom.limits import RED_TEAM_BREADTH_WEIGHT_THRESHOLD, TODO_FLOW_BACKBONE_ACCEPTS_TYPED_BACKBONE


def _tid(prefix: str) -> str:
    return f"{prefix}-{uuid.uuid4().hex[:10]}"


def _preflight_seeds_alternatives_phase(preflight: InvestigationPreflight | None) -> bool:
    if preflight is None:
        return False
    if preflight.breadth_profile in ("deep_breadth", "corridor_stress"):
        return True
    if float(preflight.breadth_weight) >= RED_TEAM_BREADTH_WEIGHT_THRESHOLD:
        return True
    return bool(
        preflight.require_alternative_route_families
        or preflight.require_epistemic_counterview
        or preflight.require_other_sources_sinks
        or preflight.require_adjacent_regions_balance
    )


def _preflight_urges_early_alternatives(pf: InvestigationPreflight | None) -> bool:
    """After first structural/grounded chain, move alternatives to in_progress when breadth is high."""
    if pf is None:
        return False
    if pf.breadth_profile in ("deep_breadth", "corridor_stress"):
        return True
    if float(pf.breadth_weight) >= RED_TEAM_BREADTH_WEIGHT_THRESHOLD:
        return True
    return bool(
        pf.require_alternative_route_families
        or pf.require_other_sources_sinks
        or pf.require_adjacent_regions_balance
    )


def seed_investigation_todos(
    intent: QuestionIntent,
    selected_roles: list[InvestigationRole],
    preflight: InvestigationPreflight | None = None,
) -> list[dict[str, Any]]:
    """Create initial todo list from intent, optional normalized preflight, and selected roles."""
    todos: list[InvestigationTodoItem] = []
    if intent.corridor_active and (intent.source_scope.strip() or intent.sink_scope.strip()):
        todos.append(
            InvestigationTodoItem(
                todoId=_tid("corridor"),
                title="Resolve operational corridor from the question in the working graph",
                phase="corridor",
                status="pending",
            )
        )
    todos.append(
        InvestigationTodoItem(
            todoId=_tid("flow"),
            title="Establish at least one business-complete operational flow backbone (source → process → sink)",
            phase="flow_backbone",
            status="pending",
        )
    )
    if intent.wants_alternatives or _preflight_seeds_alternatives_phase(preflight):
        todos.append(
            InvestigationTodoItem(
                todoId=_tid("alt"),
                title="Cover alternative routes or processing families where the question asks for them",
                phase="alternatives",
                status="pending",
            )
        )
    if "governance_and_control" in selected_roles or intent.wants_ownership:
        todos.append(
            InvestigationTodoItem(
                todoId=_tid("gov"),
                title="Ownership and control overlay on concrete operational nodes",
                phase="governance_overlay",
                status="pending",
            )
        )
    if "policy_impact" in selected_roles or intent.wants_policy:
        todos.append(
            InvestigationTodoItem(
                todoId=_tid("pol"),
                title="Policy / regulatory choke points relevant to the question",
                phase="policy_overlay",
                status="pending",
            )
        )
    todos.append(
        InvestigationTodoItem(
            todoId=_tid("prep"),
            title="Prepare for honest composition (critic pass, epistemic caveats)",
            phase="compose_prep",
            status="pending",
        )
    )
    return [t.model_dump(mode="json", by_alias=True) for t in todos]


def _find_todo(todos: list[dict[str, Any]], phase: InvestigationTodoPhase) -> dict[str, Any] | None:
    for item in todos:
        if item.get("phase") == phase and item.get("status") not in ("done", "cancelled"):
            return item
    return None


def apply_investigation_todo_updates_from_agent(
    todos: list[dict[str, Any]],
    updates: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    """Apply structured todo status updates from AgentResult (by todoId); unknown ids ignored."""
    if not updates or not todos:
        return todos
    umap = {str(u.get("todoId") or u.get("todo_id") or "").strip(): u for u in updates if isinstance(u, dict)}
    out: list[dict[str, Any]] = []
    for raw in todos:
        item = dict(raw) if isinstance(raw, dict) else {}
        tid = str(item.get("todoId") or item.get("todo_id") or "").strip()
        if tid and tid in umap:
            u = umap[tid]
            st = u.get("status")
            if st in ("pending", "in_progress", "done", "blocked", "cancelled"):
                item["status"] = st
            br = str(u.get("blockedReason") or u.get("blocked_reason") or "").strip()
            if br:
                item["blockedReason"] = br
        out.append(item)
    return out


def merge_todos_after_graph_progress(
    state: dict[str, Any],
    *,
    todos_key: str = "investigation_todos",
    grounded_complete_chains: int,
    structural_complete_chains: int,
    partial_chains: int,
    typed_backbone_chains: int = 0,
    requested_corridor_satisfied: bool,
    investigation_preflight_key: str = "investigation_preflight",
) -> list[dict[str, Any]]:
    """Deterministic transitions from reachability signals (no LLM)."""
    raw = list(state.get(todos_key) or [])
    todos = [dict(x) for x in raw if isinstance(x, dict)]
    if not todos:
        return todos

    pf: InvestigationPreflight | None = None
    raw_pf = state.get(investigation_preflight_key)
    if raw_pf:
        try:
            pf = InvestigationPreflight.model_validate(raw_pf)
        except Exception:
            pf = None

    def mark_done(phase: InvestigationTodoPhase) -> None:
        for item in todos:
            if item.get("phase") == phase and item.get("status") == "pending":
                item["status"] = "done"

    if requested_corridor_satisfied:
        mark_done("corridor")
    flow_done = grounded_complete_chains > 0 or structural_complete_chains > 0
    if TODO_FLOW_BACKBONE_ACCEPTS_TYPED_BACKBONE and typed_backbone_chains > 0:
        flow_done = True
    if flow_done:
        mark_done("flow_backbone")
    if grounded_complete_chains > 1 or structural_complete_chains > 1:
        alt = _find_todo(todos, "alternatives")
        if alt and alt.get("status") == "pending":
            alt["status"] = "in_progress"
    elif _preflight_urges_early_alternatives(pf) and (grounded_complete_chains + structural_complete_chains) >= 1:
        alt = _find_todo(todos, "alternatives")
        if alt and alt.get("status") == "pending":
            alt["status"] = "in_progress"
    if grounded_complete_chains > 0:
        for item in todos:
            if item.get("phase") in ("governance_overlay", "policy_overlay") and item.get("status") == "pending":
                item["status"] = "in_progress"
    backbone_any = (
        grounded_complete_chains + structural_complete_chains > 0
        or (TODO_FLOW_BACKBONE_ACCEPTS_TYPED_BACKBONE and typed_backbone_chains > 0)
    )
    if partial_chains == 0 and backbone_any:
        mark_done("compose_prep")
    return todos


def todos_compact_for_critic(todos: list[dict[str, Any]], *, limit: int = 24) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for item in todos[:limit]:
        if not isinstance(item, dict):
            continue
        out.append(
            {
                "todoId": item.get("todoId") or item.get("todo_id"),
                "title": (item.get("title") or "")[:200],
                "phase": item.get("phase"),
                "status": item.get("status"),
            }
        )
    return out


def required_todos_satisfied(todos: list[dict[str, Any]]) -> bool:
    """True if corridor + flow backbone are done (or absent). compose_prep is advanced by merge, not a hard gate here."""
    for item in todos:
        if not isinstance(item, dict):
            continue
        st = item.get("status")
        if st in ("pending", "in_progress"):
            phase = item.get("phase")
            if phase in ("corridor", "flow_backbone"):
                return False
    return True
