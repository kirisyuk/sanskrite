"""Supervisor delegation constraints (flow-first); imported after agent module is loaded.

Matrix (high level; see limits.py for env names):

- **Flow-first default:** governance/policy investigators are constrained until flow evidence exists,
  except ``DELEGATE_FLOW_FIRST_STAGNATION_EXCEPTION`` cycles of stagnation.
- **Stagnation recycle:** when ``DELEGATE_STAGNATION_RECYCLE_SUPPLY`` is on and there is no
  business-complete chain, idle investigator streak can recycle another supply pass instead of
  escalating immediately to critic.
- **Critic escape hatches:** ``CRITIC_ALLOW_TYPED_BACKBONE`` + stagnation, or
  ``CRITIC_ALLOW_TYPED_TOOL_IDLE`` + tool-idle streak (with respective min thresholds), can allow
  critic without a grounded business-complete chain.
- **Budget:** ``MAX_SUPERVISOR_CYCLES`` and ``BUDGET_SOFT_FRACTION`` shape hard vs soft pressure.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from agentloom.contracts import InvestigationMission, PolicyCorrection, SupervisorDecision
from agentloom.limits import (
    CRITIC_ALLOW_TYPED_BACKBONE,
    CRITIC_ALLOW_TYPED_TOOL_IDLE,
    CRITIC_MIN_GROUNDED_CHAINS,
    CRITIC_TYPED_BACKBONE_MIN_STAGNATION,
    CRITIC_TYPED_BACKBONE_MIN_TOOL_IDLE,
    DELEGATE_FLOW_FIRST_STAGNATION_EXCEPTION,
    DELEGATE_STAGNATION_RECYCLE_SUPPLY,
    MAX_SUPERVISOR_CYCLES,
    REPEATED_GAP_THRESHOLD,
    STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES,
)


@dataclass(frozen=True)
class PolicyContext:
    """Single snapshot for delegate policy branches (deterministic control plane)."""

    state: dict[str, Any]
    decision: SupervisorDecision
    cycle: int
    user_question: str
    reachability: dict[str, Any]
    grounded_complete_n: int
    structural_complete_n: int
    has_any_business_complete: bool
    stagnation: int
    gap_streak: int
    hard_budget: bool
    allow_critic_without_business_chain: bool
    last_two_delegate_agents: tuple[str, ...]
    flow_grounding_gain_recent: bool
    typed_backbone_n: int
    tool_idle_streak: int


def _typed_backbone_critic_escape(ctx: PolicyContext) -> bool:
    """True when typed operational backbone + stagnation should unblock critic without business-complete chains."""
    if ctx.typed_backbone_n < 1:
        return False
    if CRITIC_ALLOW_TYPED_BACKBONE and ctx.stagnation >= CRITIC_TYPED_BACKBONE_MIN_STAGNATION:
        return True
    if CRITIC_ALLOW_TYPED_TOOL_IDLE and ctx.tool_idle_streak >= CRITIC_TYPED_BACKBONE_MIN_TOOL_IDLE:
        return True
    return False


def _build_policy_context(
    state: dict[str, Any],
    decision: SupervisorDecision,
    *,
    cycle: int,
    user_question: str,
) -> PolicyContext:
    from agentloom import agent as A

    reach = A._graph_reachability_report(state)
    complete, structural_complete = A._reachability_complete_counts(reach)
    has_business = (complete + structural_complete) > 0
    typed_bb = int(reach.get("typedBackboneChainCount") or 0)
    stagnation = int(state.get(A.STATE_GLOBAL_STAGNATION, 0) or 0)
    gap_streak = int(state.get(A.STATE_OPEN_GAP_STREAK, 0) or 0)
    hard = A._supervisor_budget_pressure(cycle, MAX_SUPERVISOR_CYCLES) == "hard"
    allow_critic = hard and cycle >= MAX_SUPERVISOR_CYCLES
    last_two = tuple(A._last_delegate_agent_names_from_trace(state, 2))
    flow_gain = A._last_flow_grounding_gain_from_trace(state, 2)
    tool_idle = int(state.get(A.STATE_TOOL_IDLE_STREAK, 0) or 0)
    return PolicyContext(
        state=state,
        decision=decision,
        cycle=cycle,
        user_question=user_question,
        reachability=reach,
        grounded_complete_n=complete,
        structural_complete_n=structural_complete,
        has_any_business_complete=has_business,
        stagnation=stagnation,
        gap_streak=gap_streak,
        hard_budget=hard,
        allow_critic_without_business_chain=allow_critic,
        last_two_delegate_agents=last_two,
        flow_grounding_gain_recent=flow_gain,
        typed_backbone_n=typed_bb,
        tool_idle_streak=tool_idle,
    )


def apply_delegate_constraints(
    state: dict[str, Any],
    decision: SupervisorDecision,
    *,
    cycle: int,
    user_question: str,
) -> tuple[SupervisorDecision, list[PolicyCorrection]]:
    """Enforce flow-first delegation; return possibly rewritten decision + policy corrections."""
    from agentloom import agent as A

    corrections: list[PolicyCorrection] = []
    if decision.next_agent in {"compose", "stop"}:
        return decision, corrections

    if decision.next_agent == "red_team_agent":
        from agentloom.contracts import InvestigationPreflight
        from agentloom.preflight import preflight_requires_red_team_pass, red_team_frame_eligible

        raw_pf = state.get(A.STATE_INVESTIGATION_PREFLIGHT) or {}
        try:
            pf = InvestigationPreflight.model_validate(raw_pf)
        except Exception:
            pf = InvestigationPreflight()
        reach = A._graph_reachability_report(state)
        if not preflight_requires_red_team_pass(pf):
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Investigation preflight does not require a dedicated red-team pass yet; extend the operational graph "
                    f"with evidence-backed structure first. Question: {user_question[:400]}"
                ),
                rationale_line="runtime_delegate_constraint: red_team_agent deferred (preflight does not require red team pass).",
            )
            corrections.append(
                PolicyCorrection(kind="delegate_constraint", detail="red_team_deferred_preflight_not_required")
            )
            return nd, corrections
        if not red_team_frame_eligible(reach):
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Establish at least one typed/structural/grounded operational backbone before a red-team breadth pass. "
                    f"Question: {user_question[:400]}"
                ),
                rationale_line="runtime_delegate_constraint: red_team_agent deferred until frame is eligible (reachability).",
            )
            corrections.append(
                PolicyCorrection(kind="delegate_constraint", detail="red_team_deferred_until_frame_eligible")
            )
            return nd, corrections
        if bool(state.get(A.STATE_RED_TEAM_SATISFIED)):
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Red team pass already recorded this session; continue operational investigation or await critic/compose per policy. "
                    f"Question: {user_question[:400]}"
                ),
                rationale_line="runtime_delegate_constraint: red_team_agent skipped (already satisfied).",
            )
            corrections.append(PolicyCorrection(kind="delegate_constraint", detail="red_team_already_satisfied"))
            return nd, corrections

    ctx = _build_policy_context(state, decision, cycle=cycle, user_question=user_question)
    next_a = decision.next_agent

    # Anti-plateau: repeated critic passes without shrinking placeholder debt — recycle one supply pass.
    if (
        next_a == "critic_agent"
        and not ctx.allow_critic_without_business_chain
        and ctx.grounded_complete_n == 0
        and A._recent_supervisor_delegate_streak(state, "critic_agent", 2)
        and A._graph_has_placeholder_debt(state)
    ):
        nd = A._force_supply_chain_investigation_decision(
            decision,
            cycle=cycle,
            objective=(
                "Two critic passes in a row did not clear runtime placeholder debt. Run one narrow supply_chain pass: "
                "prefer corpus/retrieve-backed `replace_entity` / `replace_fragment` for a `sharedMemory.placeholderNodes` "
                "entry, or add one document/mixed flow leg / process step between named hosts already in the graph. "
                f"Question: {user_question[:400]}"
            ),
            rationale_line=(
                "runtime_delegate_constraint: critic_deferred — recycle supply_chain_investigator_agent after repeated "
                "critic_agent with unresolved placeholders (anti-plateau)."
            ),
        )
        corrections.append(
            PolicyCorrection(kind="delegate_constraint", detail="critic_recycle_after_placeholder_stagnation")
        )
        return nd, corrections

    if next_a == "critic_agent" and CRITIC_MIN_GROUNDED_CHAINS > 0 and ctx.grounded_complete_n < CRITIC_MIN_GROUNDED_CHAINS:
        if not ctx.allow_critic_without_business_chain:
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Build at least one document- or mixed-grounded complete operational chain before critic review "
                    f"(policy requires groundedCompleteChainCount>={CRITIC_MIN_GROUNDED_CHAINS})."
                ),
                rationale_line=(
                    f"runtime_delegate_constraint: critic deferred until grounded complete chains "
                    f">= {CRITIC_MIN_GROUNDED_CHAINS} (current={ctx.grounded_complete_n})."
                ),
            )
            corrections.append(
                PolicyCorrection(kind="delegate_constraint", detail="critic_deferred_min_grounded_chains")
            )
            return nd, corrections

    if (
        next_a == "critic_agent"
        and not ctx.has_any_business_complete
        and not ctx.allow_critic_without_business_chain
        and not _typed_backbone_critic_escape(ctx)
    ):
        nd = A._force_supply_chain_investigation_decision(
            decision,
            cycle=cycle,
            objective=(
                "Do not exit yet. Build at least one business-complete operational chain with a real upstream source, "
                "midstream process step, and concrete downstream sink. Prioritize the break point named by reachability."
            ),
            rationale_line=(
                "runtime_delegate_constraint: critic deferred because the graph still lacks a business-complete operational chain."
            ),
        )
        corrections.append(
            PolicyCorrection(kind="delegate_constraint", detail="critic_deferred_until_business_complete_chain")
        )
        return nd, corrections

    if next_a in ("governance_and_control_investigator_agent", "policy_impact_investigator_agent") and ctx.grounded_complete_n == 0:
        last_two = list(ctx.last_two_delegate_agents)
        exception_allowed = (
            ctx.stagnation >= DELEGATE_FLOW_FIRST_STAGNATION_EXCEPTION
            and len(last_two) >= 2
            and all(x in ("supply_chain_investigator_agent", "gap_bridge_agent") for x in last_two[:2])
            and not ctx.flow_grounding_gain_recent
        )
        if not exception_allowed:
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Establish at least one document- or mixed-grounded operational chain segment toward the "
                    "question; prioritize reachability break points before governance/policy overlays."
                ),
                rationale_line=(
                    "runtime_delegate_constraint: no grounded complete chain; governance/policy deferred until "
                    "flow backbone exists (see stagnation exception in code)."
                ),
            )
            corrections.append(
                PolicyCorrection(kind="delegate_constraint", detail="flow_first_redirect_governance_policy_to_supply_chain")
            )
            return nd, corrections

    if (
        ctx.gap_streak >= REPEATED_GAP_THRESHOLD
        and next_a in A.SPECIALIZED_INVESTIGATOR_BY_ROLE.values()
        and next_a != "critic_agent"
    ):
        if (
            not ctx.has_any_business_complete
            and not ctx.allow_critic_without_business_chain
            and not _typed_backbone_critic_escape(ctx)
        ):
            nd = A._force_supply_chain_investigation_decision(
                decision,
                cycle=cycle,
                objective=(
                    "Open gaps are repeating. Instead of exiting, close one structural flow break so the graph reaches "
                    "a business-complete chain with concrete source, midstream, and sink."
                ),
                rationale_line=(
                    f"runtime_delegate_constraint: repeated gaps ({ctx.gap_streak}) but critic is blocked until a business-complete chain exists."
                ),
            )
            corrections.append(
                PolicyCorrection(kind="delegate_constraint", detail="repeated_gap_redirect_to_supply_chain")
            )
            return nd, corrections
        nd = decision.model_copy(
            update={
                "next_agent": "critic_agent",
                "rationale": list(decision.rationale)
                + [f"runtime: openGap streak {ctx.gap_streak} >= {REPEATED_GAP_THRESHOLD}; escalated to critic_agent."],
            }
        )
        m = {
            "missionId": f"runtime-repeated-gap-{cycle}",
            "objective": (
                "Open gaps have not changed materially across recent cycles. Assess whether an honest composed "
                f"answer is appropriate now, or name the single smallest structural next step. Question: {user_question[:400]}"
            ),
            "assignedRoles": list(state.get(A.STATE_SELECTED_ROLES) or [A.ROLE_SUPPLY_CHAIN])[:3],
            "groundingMode": "mixed",
            "focus": [],
            "successCriteria": [],
        }
        nd = nd.model_copy(update={"mission": InvestigationMission.model_validate(m)})
        corrections.append(PolicyCorrection(kind="delegate_constraint", detail="repeated_open_gap_escalation"))
        return nd, corrections

    if ctx.stagnation >= STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES and next_a in A.SPECIALIZED_INVESTIGATOR_BY_ROLE.values():
        if (
            not ctx.has_any_business_complete
            and not ctx.allow_critic_without_business_chain
            and not _typed_backbone_critic_escape(ctx)
        ):
            if DELEGATE_STAGNATION_RECYCLE_SUPPLY:
                nd = A._force_supply_chain_investigation_decision(
                    decision,
                    cycle=cycle,
                    objective=(
                        "Material progress has stalled, but the graph still lacks any business-complete chain. "
                        "Use the next pass to repair source-side, midstream, or sink-side structure instead of exiting."
                    ),
                    rationale_line=(
                        f"runtime_delegate_constraint: global stagnation {ctx.stagnation} but critic is blocked until a business-complete chain exists."
                    ),
                )
                corrections.append(
                    PolicyCorrection(kind="delegate_constraint", detail="stagnation_redirect_to_supply_chain")
                )
                return nd, corrections
        nd = decision.model_copy(
            update={
                "next_agent": "critic_agent",
                "rationale": list(decision.rationale)
                + [
                    f"runtime: global stagnation {ctx.stagnation} >= {STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES}; "
                    "forced critic_agent for honest exit assessment."
                ],
            }
        )
        m = {
            "missionId": f"runtime-stagnation-{cycle}",
            "objective": (
                "Material progress has stalled. Decide whether the graph is sufficient for an honest composed answer "
                f"(with epistemic caveats) or name one minimal structural follow-up. Question: {user_question[:400]}"
            ),
            "assignedRoles": list(state.get(A.STATE_SELECTED_ROLES) or [A.ROLE_SUPPLY_CHAIN])[:3],
            "groundingMode": "mixed",
            "focus": [],
            "successCriteria": [],
        }
        nd = nd.model_copy(update={"mission": InvestigationMission.model_validate(m)})
        corrections.append(PolicyCorrection(kind="delegate_constraint", detail="stagnation_idle_escalate_critic"))
        return nd, corrections

    return decision, corrections
