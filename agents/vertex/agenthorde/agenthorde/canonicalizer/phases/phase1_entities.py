"""Phase 1: LLM entity extraction."""

from __future__ import annotations

import json
from typing import Any

from agenthorde.canonicalizer.llm_client import LLMPhaseClient
from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.product_rollup import rollup_product_surface
from agenthorde.canonicalizer.schemas import Candidates, ContextHints, EntityMention, UnknownEntityTypeCandidate
from agenthorde.canonicalizer.taxonomy import FRAGMENT_CLASSES


async def run_phase1(
    ws: WorkingState,
    llm: LLMPhaseClient,
    model: str,
    candidates: Candidates,
) -> list[EntityMention]:
    user = json.dumps(
        {
            "claims": [{"id": n.id, "text": n.text} for n in ws.claim_nodes],
            "hypothesis_anchors": [{"id": n.id, "text": n.text} for n in ws.hypothesis_nodes],
            "counters": [{"id": n.id, "text": n.text} for n in ws.counter_nodes],
            "chain_summaries": ws.chain_summaries,
            "final_answer_excerpt": ws.final_answer[:8000],
            "working_graph_excerpt": ws.working_graph_text[:8000],
        },
        ensure_ascii=False,
    )
    raw = await llm.generate_json("1", model, user)
    mentions: list[EntityMention] = []
    for i, m in enumerate(raw.get("mentions") or []):
        if not isinstance(m, dict):
            continue
        t_raw = m.get("type")
        t: str | None
        if t_raw is None or str(t_raw).lower() == "null":
            t = None
        else:
            t = str(t_raw).removeprefix("/").strip().lower()
        ch = m.get("context_hints") or m.get("contextHints") or {}
        hints = ContextHints(
            iso2_hint=ch.get("iso2Hint") or ch.get("iso2_hint"),
            sector_hint=ch.get("sectorHint") or ch.get("sector_hint"),
            as_of_hint=ch.get("asOfHint") or ch.get("as_of_hint"),
        )
        surf = str(m.get("surface") or "").strip()
        if not surf:
            continue
        attrs: dict[str, object] = {}
        if t == "product":
            rolled, orig = rollup_product_surface(surf)
            if orig is not None:
                surf = rolled
                attrs["variants_seen"] = [orig]
        refs = [str(x) for x in (m.get("claim_refs") or m.get("claimRefs") or [])]
        if t is None or t not in FRAGMENT_CLASSES:
            candidates.unknown_entity_types.append(
                UnknownEntityTypeCandidate(surface=surf, reason=str(m.get("reason") or "unknown_type"), claim_refs=refs)
            )
            continue
        mentions.append(
            EntityMention(
                mention_id=f"m{i}",
                surface=surf,
                type=t,
                subtype=(str(m["subtype"]) if m.get("subtype") else None),
                claim_refs=refs,
                context_hints=hints,
                per_claim_confidence=float(m.get("per_claim_confidence") or m.get("perClaimConfidence") or 0.7),
                attributes=attrs,
            )
        )
    return mentions
