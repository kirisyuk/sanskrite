"""Investigation preflight: breadth profile merge (request + intent) and red-team compose gates."""

from __future__ import annotations

from typing import Any

from agentloom.contracts import AgentLoomRequest, InvestigationPreflight, QuestionIntent
from agentloom.limits import RED_TEAM_BREADTH_WEIGHT_THRESHOLD, RED_TEAM_REQUIRED_FOR_BREADTH


def normalize_investigation_preflight(request: AgentLoomRequest, intent: QuestionIntent) -> InvestigationPreflight:
    """
    Merge explicit request.investigationPreflight with intent-derived signals.
    Precedence: any field explicitly set on `request.investigationPreflight` wins; intent only fills gaps.
    """
    if request.investigation_preflight is not None:
        base = request.investigation_preflight.model_copy()
        explicit = set(request.investigation_preflight.model_fields_set)
    else:
        base = InvestigationPreflight()
        explicit = set()

    bp = base.breadth_profile
    bw = float(base.breadth_weight)
    req_alt = base.require_alternative_route_families
    req_counter = base.require_epistemic_counterview
    req_src = base.require_other_sources_sinks
    req_adj = base.require_adjacent_regions_balance

    if "require_alternative_route_families" not in explicit and intent.wants_alternatives:
        req_alt = True
    if "require_epistemic_counterview" not in explicit and intent.wants_counter_narrative:
        req_counter = True
    if "require_adjacent_regions_balance" not in explicit and intent.wants_adjacent_regions:
        req_adj = True
    if "breadth_weight" not in explicit and intent.stresses_geopolitical_risk:
        bw = min(1.0, bw + 0.15)
    if "breadth_profile" not in explicit and intent.stresses_geopolitical_risk and bp == "routine":
        bp = "standard"

    if (
        "require_other_sources_sinks" not in explicit
        and (intent.wants_alternatives or intent.stresses_geopolitical_risk)
    ):
        req_src = req_src or intent.wants_alternatives

    if "breadth_profile" not in explicit and "breadth_weight" not in explicit:
        if bw >= 0.85 and bp in ("routine", "standard"):
            bp = "deep_breadth"
        elif bw >= 0.65 and bp == "routine":
            bp = "standard"

    return InvestigationPreflight(
        breadthProfile=bp,
        breadthWeight=round(min(1.0, max(0.0, bw)), 3),
        requireAlternativeRouteFamilies=req_alt,
        requireEpistemicCounterview=req_counter,
        requireOtherSourcesSinks=req_src,
        requireAdjacentRegionsBalance=req_adj,
    )


def preflight_requires_red_team_pass(preflight: InvestigationPreflight | dict[str, Any]) -> bool:
    """Whether product policy expects a red-team pass before compose (when frame is eligible)."""
    if not RED_TEAM_REQUIRED_FOR_BREADTH:
        return False
    p = preflight if isinstance(preflight, InvestigationPreflight) else InvestigationPreflight.model_validate(preflight)
    if p.breadth_profile in ("deep_breadth", "corridor_stress"):
        return True
    if p.breadth_weight >= RED_TEAM_BREADTH_WEIGHT_THRESHOLD:
        return True
    return bool(
        p.require_alternative_route_families
        or p.require_epistemic_counterview
        or p.require_other_sources_sinks
        or p.require_adjacent_regions_balance
    )


def red_team_frame_eligible(reachability: dict[str, Any]) -> bool:
    """Enough graph for red team to attack / broaden (not an empty sketch)."""
    gc = int(reachability.get("groundedCompleteChainCount") or 0)
    sc = int(reachability.get("structuralCompleteChainCount") or 0)
    tb = int(reachability.get("typedBackboneChainCount") or 0)
    return gc > 0 or sc > 0 or tb > 0
