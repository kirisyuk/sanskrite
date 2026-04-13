from __future__ import annotations

import re

from typing import cast

from agentloom.contracts import InvestigationRole, QuestionRubric, RubricItem, RubricLLMExtras


def build_question_rubric(question: str, selected_roles: list[str]) -> QuestionRubric:
    """
    Deterministic rubric: one required slot per selected role plus optional axes from question text.
    """
    items: list[RubricItem] = []
    q = question.strip()
    lower = q.lower()

    if "supply_chain" in selected_roles:
        items.append(
            RubricItem(
                rubricId="supply_chain_structure",
                title="Operational supply chain: typed graph path, concrete facilities/orgs, and product-stage ladder (not elemental labels alone)",
                source="from_role",
                required=True,
                role=cast(InvestigationRole, "supply_chain"),
            )
        )
    if "governance_and_control" in selected_roles:
        items.append(
            RubricItem(
                rubricId="governance_ownership",
                title="Ownership, control, and corporate leverage on operational nodes",
                source="from_role",
                required=True,
                role=cast(InvestigationRole, "governance_and_control"),
            )
        )
    if "policy_impact" in selected_roles:
        items.append(
            RubricItem(
                rubricId="policy_regulatory",
                title="Regulatory mechanisms, transmission paths, and choke points",
                source="from_role",
                required=True,
                role=cast(InvestigationRole, "policy_impact"),
            )
        )

    if re.search(r"alternative|different\s+path|route\s+famil", lower):
        items.append(
            RubricItem(
                rubricId="axis_alternative_paths",
                title="Alternative routes or processing families where evidence allows",
                source="from_question_axis",
                required=True,
                role=None,
            )
        )
    if re.search(r"ownership|leverage|who\s+owns|control", lower):
        items.append(
            RubricItem(
                rubricId="axis_ownership_leverage",
                title="Where ownership and economic leverage sit along the chain",
                source="from_question_axis",
                required=True,
                role=None,
            )
        )
    if re.search(r"regulat|choke|compliance|due\s*diligence|carbon|passport", lower):
        items.append(
            RubricItem(
                rubricId="axis_regulatory_choke",
                title="Regulatory choke points and how they shape viable routes",
                source="from_question_axis",
                required=True,
                role=None,
            )
        )

    # Dedupe rubricId
    seen: set[str] = set()
    unique: list[RubricItem] = []
    for item in items:
        if item.rubric_id in seen:
            continue
        seen.add(item.rubric_id)
        unique.append(item)

    return QuestionRubric(items=unique)


def merge_rubric_with_llm_extras(base: QuestionRubric, extras: RubricLLMExtras) -> QuestionRubric:
    """Append LLM-proposed items; skip duplicate rubricId or empty id."""
    seen = {item.rubric_id for item in base.items}
    merged = list(base.items)
    for item in extras.additional_items:
        rid = (item.rubric_id or "").strip()
        if not rid or rid in seen:
            continue
        seen.add(rid)
        merged.append(item)
    return QuestionRubric(items=merged)
