"""Phase 1: LLM entity extraction over the analyzer registry.

By default (``CANONICALIZER_PHASE1_CHUNK_MAX <= 0``) the **entire** registry is
sent in a **single** bound LLM call — paired with OpenRouter ``openai/o4-mini``
by default in config. A separate **prose pass** (empty ``analyzer_entities``)
still runs to mine entities not present in the registry.

When ``CHUNK_MAX >= 1``, the registry is split into concurrent chunks of that
size; empty chunk responses are recursively halved up to ``PHASE1_MAX_DEPTH``
(legacy Vertex flash workaround).

Post-merge dedup: one mention per registry ``agentId``; free-prose mentions
deduped by ``(surface.casefold(), type)`` with surfaces matching a registry
label dropped.
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.phases.phase0_input import WorkingState
from agenthorde_common.canonicalizer.product_rollup import rollup_product_surface
from agenthorde_common.canonicalizer.schemas import (
    Candidates,
    ContextHints,
    EntityMention,
    RegistryCoverageIssue,
    UnknownEntityTypeCandidate,
)
from agenthorde_common.canonicalizer.taxonomy import FRAGMENT_CLASSES
from agenthorde_common.config import (
    CANONICALIZER_PHASE1_CHUNK_MAX,
    CANONICALIZER_PHASE1_MAX_DEPTH,
)

_LOG = logging.getLogger(__name__)


def _build_narrative_payload(ws: WorkingState) -> dict[str, Any]:
    """Shared prose / graph context attached to every phase1 call."""
    return {
        "claims": [{"id": n.id, "text": n.text} for n in ws.claim_nodes],
        "hypothesis_anchors": [{"id": n.id, "text": n.text} for n in ws.hypothesis_nodes],
        "counters": [{"id": n.id, "text": n.text} for n in ws.counter_nodes],
        "chain_summaries": ws.chain_summaries,
        "final_answer_excerpt": ws.final_answer[:8000],
        "working_graph_excerpt": ws.working_graph_text[:8000],
    }


async def _call_phase1(
    llm: LLMPhaseClient,
    model: str,
    narrative: dict[str, Any],
    registry_slice: list[dict[str, str]],
    *,
    mode: str,
    all_registry_labels: list[str] | None = None,
) -> dict[str, Any]:
    """One LLM call with the given registry slice and operating mode.

    ``mode`` is one of:
      * ``"bound"`` — emit exactly one mention per row in ``registry_slice``;
        no free-prose mentions (agentId must be non-null and inside this slice).
      * ``"prose"`` — ``registry_slice`` is empty; emit only mentions with
        ``agentId=null`` (entities mined from prose that are not in the registry).
    """
    payload = dict(narrative)
    payload["analyzer_entities"] = registry_slice
    if mode == "bound":
        extra = (
            "The `analyzer_entities` array lists the analyzer-proposed registry rows you must "
            "cover in this call. It may be the **entire** registry or one **chunk** when upstream "
            "sets a positive chunk size. Emit exactly ONE mention per row (surface = row label "
            "verbatim, agentId = row agentId). DO NOT emit mentions with agentId=null or agentId "
            "outside this subset; the prose pass handles free-prose mentions separately. If a "
            "row is an Incoterm or otherwise non-entity per rules, put its agentId into "
            "skippedRegistryIds and skip."
        )
    elif mode == "prose":
        known = ", ".join(sorted({l for l in (all_registry_labels or []) if l})[:200])
        extra = (
            "This is the PROSE pass. `analyzer_entities` is empty on purpose. Emit ONLY mentions "
            "with agentId=null that are NOT already represented by the agent registry. For "
            "reference, the agent registry already covers these labels (do not re-emit surfaces "
            f"that match them): [{known}]. Apply the usual coverage rules for products, regimes, "
            "routes, sites, vessel_class, markets, orgs, communities, persons, geos."
        )
    else:
        extra = ""
    return await llm.generate_json("1", model, json.dumps(payload, ensure_ascii=False), extra_system=extra)


async def _call_with_adaptive_halving(
    llm: LLMPhaseClient,
    model: str,
    narrative: dict[str, Any],
    registry_slice: list[dict[str, str]],
    *,
    depth: int,
) -> dict[str, Any]:
    """Call the bound mode; on empty result, split the slice in half and retry.

    Depth bound prevents runaway recursion on genuinely broken LLM configs.
    Returns a merged dict with ``mentions`` list and ``skippedRegistryIds`` list.
    """
    raw = await _call_phase1(llm, model, narrative, registry_slice, mode="bound")
    mentions = raw.get("mentions") or []
    skipped = raw.get("skippedRegistryIds") or raw.get("skipped_registry_ids") or []
    if mentions or len(registry_slice) <= 1 or depth >= CANONICALIZER_PHASE1_MAX_DEPTH:
        return {"mentions": list(mentions), "skippedRegistryIds": list(skipped)}
    mid = len(registry_slice) // 2
    left_slice = registry_slice[:mid]
    right_slice = registry_slice[mid:]
    _LOG.warning(
        "phase1 chunk depth=%d size=%d returned empty; halving into %d+%d",
        depth,
        len(registry_slice),
        len(left_slice),
        len(right_slice),
    )
    left, right = await asyncio.gather(
        _call_with_adaptive_halving(llm, model, narrative, left_slice, depth=depth + 1),
        _call_with_adaptive_halving(llm, model, narrative, right_slice, depth=depth + 1),
    )
    merged_mentions = list(left.get("mentions") or []) + list(right.get("mentions") or [])
    merged_skipped = list(
        {*(left.get("skippedRegistryIds") or []), *(right.get("skippedRegistryIds") or [])}
    )
    return {"mentions": merged_mentions, "skippedRegistryIds": merged_skipped}


def _chunk(seq: list[dict[str, str]], size: int) -> list[list[dict[str, str]]]:
    size = max(1, size)
    return [seq[i : i + size] for i in range(0, len(seq), size)]


def _build_entity_mention(index: int, m: dict[str, Any]) -> tuple[EntityMention | None, dict[str, Any]]:
    """Pure parse step. Returns (mention, meta) where meta carries unknown-type info.

    ``meta`` shape: {"unknown_type": bool, "reg_id": str|None, "surface": str, "claim_refs": [..]}.
    The caller uses meta to update candidates.unknown_entity_types and the
    registry_ids_unknown_type set, then folds the remaining entries into
    ``mentions``.
    """
    t_raw = m.get("type")
    if t_raw is None or str(t_raw).lower() == "null":
        t: str | None = None
    else:
        t = str(t_raw).removeprefix("/").strip().lower()
    ch = m.get("context_hints") or m.get("contextHints") or {}
    hints = ContextHints(
        iso2_hint=ch.get("iso2Hint") or ch.get("iso2_hint"),
        sector_hint=ch.get("sectorHint") or ch.get("sector_hint"),
        as_of_hint=ch.get("asOfHint") or ch.get("as_of_hint"),
    )
    surf = str(m.get("surface") or "").strip()
    reg_id = str(m.get("agentId") or m.get("agent_id") or "").strip() or None
    refs = [str(x) for x in (m.get("claim_refs") or m.get("claimRefs") or [])]
    if not surf:
        return None, {"unknown_type": False, "reg_id": reg_id, "surface": "", "claim_refs": refs}
    attrs: dict[str, object] = {}
    if t == "product":
        rolled, orig = rollup_product_surface(surf)
        if orig is not None:
            surf = rolled
            attrs["variants_seen"] = [orig]
    if t is None or t not in FRAGMENT_CLASSES:
        return None, {
            "unknown_type": True,
            "reg_id": reg_id,
            "surface": surf,
            "claim_refs": refs,
            "reason": str(m.get("reason") or "unknown_type"),
        }
    em = EntityMention(
        mention_id=f"m{index}",
        agent_registry_id=reg_id,
        surface=surf,
        type=t,
        subtype=(str(m["subtype"]) if m.get("subtype") else None),
        claim_refs=refs,
        context_hints=hints,
        per_claim_confidence=float(m.get("per_claim_confidence") or m.get("perClaimConfidence") or 0.7),
        attributes=attrs,
    )
    return em, {"unknown_type": False, "reg_id": reg_id, "surface": surf, "claim_refs": refs}


async def run_phase1(
    ws: WorkingState,
    llm: LLMPhaseClient,
    model: str,
    candidates: Candidates,
) -> list[EntityMention]:
    analyzer_entities = getattr(ws, "analyzer_entities", None) or []
    registry_rows: list[dict[str, str]] = [
        {"agentId": str(e.get("namespacedId") or "").strip(), "label": str(e.get("label") or "")}
        for e in analyzer_entities
        if str(e.get("namespacedId") or "").strip()
    ]
    narrative = _build_narrative_payload(ws)

    chunk_max_raw = int(CANONICALIZER_PHASE1_CHUNK_MAX)
    tasks: list[asyncio.Task[dict[str, Any]]] = []

    if chunk_max_raw <= 0:
        if registry_rows:

            async def _single_bound_no_halve() -> dict[str, Any]:
                raw = await _call_phase1(llm, model, narrative, registry_rows, mode="bound")
                mentions = raw.get("mentions") or []
                skipped = raw.get("skippedRegistryIds") or raw.get("skipped_registry_ids") or []
                return {"mentions": list(mentions), "skippedRegistryIds": list(skipped)}

            tasks.append(asyncio.create_task(_single_bound_no_halve()))
        _LOG.info(
            "phase1 dispatch: registry=%d rows single bound call (no chunking, no halving) + prose pass",
            len(registry_rows),
        )
    else:
        chunk_max = max(1, chunk_max_raw)
        bound_chunks = _chunk(registry_rows, chunk_max) if registry_rows else []
        for chunk_rows in bound_chunks:
            tasks.append(
                asyncio.create_task(
                    _call_with_adaptive_halving(llm, model, narrative, chunk_rows, depth=0)
                )
            )
        if bound_chunks:
            _LOG.info(
                "phase1 adaptive dispatch: registry=%d chunks=%d chunk_max=%d + prose pass",
                len(registry_rows),
                len(bound_chunks),
                chunk_max,
            )

    all_labels = [row["label"] for row in registry_rows if row.get("label")]
    prose_task = asyncio.create_task(
        _call_phase1(llm, model, narrative, [], mode="prose", all_registry_labels=all_labels)
    )
    tasks.append(prose_task)

    results = await asyncio.gather(*tasks, return_exceptions=True)

    raw_mentions: list[dict[str, Any]] = []
    skipped_norm: set[str] = set()
    for res in results:
        if isinstance(res, BaseException):
            _LOG.warning("phase1 chunk raised: %s", res)
            continue
        for m in res.get("mentions") or []:
            if isinstance(m, dict):
                raw_mentions.append(m)
        for x in res.get("skippedRegistryIds") or res.get("skipped_registry_ids") or []:
            s = str(x).strip()
            if s:
                skipped_norm.add(s)

    expected_ids = {row["agentId"] for row in registry_rows}
    label_by_ns = {row["agentId"]: row["label"].strip() for row in registry_rows}
    registry_labels_cf = {row["label"].strip().casefold() for row in registry_rows if row.get("label")}

    mentions: list[EntityMention] = []
    registry_ids_unknown_type: set[str] = set()
    seen_reg_ids: set[str] = set()
    seen_free: set[tuple[str, str]] = set()
    seen_unknown: set[tuple[str | None, str]] = set()
    next_index = 0
    for m in raw_mentions:
        em, meta = _build_entity_mention(next_index, m)
        if meta.get("unknown_type"):
            reg_id = meta.get("reg_id")
            if reg_id and reg_id in expected_ids:
                registry_ids_unknown_type.add(reg_id)
            key = (reg_id, (meta.get("surface") or "").casefold())
            if key in seen_unknown:
                continue
            seen_unknown.add(key)
            candidates.unknown_entity_types.append(
                UnknownEntityTypeCandidate(
                    surface=meta.get("surface") or "",
                    reason=meta.get("reason") or "unknown_type",
                    claim_refs=list(meta.get("claim_refs") or []),
                )
            )
            continue
        if em is None:
            continue
        reg_id = em.agent_registry_id
        if reg_id:
            if reg_id in seen_reg_ids:
                continue
            seen_reg_ids.add(reg_id)
        else:
            key = (em.surface.casefold(), em.type)
            if em.surface.casefold() in registry_labels_cf:
                continue
            if key in seen_free:
                continue
            seen_free.add(key)
        em = em.model_copy(update={"mention_id": f"m{next_index}"})
        mentions.append(em)
        next_index += 1

    skipped_in_scope = frozenset(s for s in skipped_norm if s in expected_ids)
    ws.skipped_registry_ids = skipped_in_scope

    mentioned_ids = {m.agent_registry_id for m in mentions if m.agent_registry_id}
    gap_ids: set[str] = set()
    for reg_id in sorted(expected_ids):
        if reg_id in skipped_in_scope:
            continue
        if reg_id in mentioned_ids:
            continue
        if reg_id in registry_ids_unknown_type:
            candidates.registry_coverage_issues.append(
                RegistryCoverageIssue(
                    agent_registry_id=reg_id,
                    label=label_by_ns.get(reg_id, ""),
                    reason="mention_unknown_entity_type",
                )
            )
            gap_ids.add(reg_id)
        else:
            candidates.registry_coverage_issues.append(
                RegistryCoverageIssue(
                    agent_registry_id=reg_id,
                    label=label_by_ns.get(reg_id, ""),
                    reason="no_mention_from_phase1",
                )
            )
            gap_ids.add(reg_id)
    ws.registry_ids_phase1_coverage_gap = frozenset(gap_ids)

    return mentions
