from __future__ import annotations

from agentloom.contracts import QuestionRubric, StructuredNarrativeAnswer

_MIN_SECTION_LEN = 24

_INSUFFICIENCY_MARKERS = (
    "insufficient structured data",
    "insufficient data in the current graph",
    "insufficient data",
    "not enough structured data",
    "cannot be determined from the brief",
    "not present in the brief",
    "the brief does not",
    "no structured evidence",
    "недостаточно данных",
)


def _section_text(n: StructuredNarrativeAnswer, rubric_id: str) -> str:
    rid = rubric_id.lower()
    if rid in ("supply_chain_structure",):
        return n.chain_structure
    if rid in ("governance_ownership", "axis_ownership_leverage"):
        return n.ownership_leverage
    if rid in ("policy_regulatory", "axis_regulatory_choke"):
        return n.regulatory_choke_points
    if rid == "axis_alternative_paths":
        return n.alternative_paths
    return ""


def _acceptable(text: str) -> bool:
    t = (text or "").strip()
    if len(t) >= _MIN_SECTION_LEN:
        return True
    low = t.lower()
    return any(m in low for m in _INSUFFICIENCY_MARKERS)


def validate_narrative_coverage(
    narrative: StructuredNarrativeAnswer,
    rubric: QuestionRubric,
) -> tuple[bool, list[str]]:
    """
    Returns (ok, list of failed rubricIds).
    Each required rubric item must map to a non-trivial section or an explicit insufficiency phrase.
    """
    failed: list[str] = []
    for item in rubric.items:
        if not item.required:
            continue
        text = _section_text(narrative, item.rubric_id)
        if _acceptable(text):
            continue
        failed.append(item.rubric_id)
    if not (narrative.executive_summary or "").strip():
        failed.append("__executive_summary_missing__")
    elif len(narrative.executive_summary.strip()) < 20:
        failed.append("__executive_summary_too_short__")
    return len(failed) == 0, failed


def roles_mentioned_in_narrative(
    narrative: StructuredNarrativeAnswer,
    selected_role_literals: list[str],
) -> tuple[bool, list[str]]:
    blob = " ".join(
        [
            narrative.chain_structure,
            narrative.alternative_paths,
            narrative.ownership_leverage,
            narrative.regulatory_choke_points,
            narrative.honest_limits,
            narrative.executive_summary,
        ]
    ).lower()
    missing: list[str] = []
    hints = {
        "supply_chain": (
            "supply",
            "chain",
            "ore",
            "refin",
            "material",
            "flow",
            "route",
            "facility",
            "plant",
            "transform",
            "upstream",
            "downstream",
        ),
        "governance_and_control": ("own", "control", "parent", "subsidiary", "invest", "governance", "corporate", "jv", "joint venture"),
        "policy_impact": ("regulat", "policy", "compliance", "due diligence", "law", "choke", "permit", "traceability"),
    }
    for role in selected_role_literals:
        tokens = hints.get(role, ())
        if not tokens:
            continue
        if not any(tok in blob for tok in tokens):
            missing.append(role)
    return len(missing) == 0, missing


def build_repair_hint(failed_rubric_ids: list[str], role_misses: list[str]) -> str:
    parts: list[str] = []
    if failed_rubric_ids:
        parts.append("Fill or explicitly mark insufficient data for rubric slots: " + ", ".join(failed_rubric_ids))
    if role_misses:
        parts.append("Ensure the narrative mentions themes for roles: " + ", ".join(role_misses))
    return " ".join(parts) if parts else "Expand all narrative sections to meet minimum coverage."


def explicit_narrative_synthesis_failure(
    *,
    reason_code: str,
    detail: str,
    failed_rubric_ids: list[str] | None = None,
) -> StructuredNarrativeAnswer:
    """
    Opaque diagnostic object: clearly states that narrative synthesis failed — not a substitute answer.
    Wording includes supply / governance / policy tokens so cheap role checks can still pass for logging.
    """
    failed = ", ".join(failed_rubric_ids or []) or "(not enumerated)"
    core = (
        f"Narrative synthesis failed ({reason_code}). "
        f"This block is a system diagnostic only, not LLM-composed supply-chain, governance, or policy prose. "
        f"Details: {detail}. "
        f"Rubric or validation gaps: {failed}."
    )
    section = core + " No validated narrative section was produced for this slot."
    return StructuredNarrativeAnswer(
        chainStructure=section[:4000],
        alternativePaths=section[:4000],
        ownershipLeverage=section[:4000],
        regulatoryChokePoints=section[:4000],
        honestLimits=(
            "Honest limit: the structured narrative composer did not return an acceptable rubric-aligned answer. "
            + core
        )[:4000],
        prioritizedOpenQuestions=[detail.strip()[:280], f"Failure reason code: {reason_code}"],
        executiveSummary=core[:2000],
    )


def full_narrative_text_from_answer(n: StructuredNarrativeAnswer) -> str:
    parts = [
        n.chain_structure.strip(),
        n.alternative_paths.strip(),
        n.ownership_leverage.strip(),
        n.regulatory_choke_points.strip(),
        n.honest_limits.strip(),
    ]
    pq = n.prioritized_open_questions
    if pq:
        parts.append("Open questions: " + "; ".join(pq[:8]))
    parts.append(n.executive_summary.strip())
    return "\n\n".join(p for p in parts if p)
