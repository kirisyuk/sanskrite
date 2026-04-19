"""Orchestrates canonicalizer phases 0–9."""

from __future__ import annotations

import time
import uuid
from typing import Any

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.phases.phase0_input import run_phase0
from agenthorde_common.canonicalizer.phases.phase1_entities import run_phase1
from agenthorde_common.canonicalizer.phases.phase2_dedup import run_phase2
from agenthorde_common.canonicalizer.phases.phase3_paths import run_phase3
from agenthorde_common.canonicalizer.phases.phase4_relations import run_phase4
from agenthorde_common.canonicalizer.phases.phase5_transforms import run_phase5
from agenthorde_common.canonicalizer.phases.phase6_metrics import run_phase6
from agenthorde_common.canonicalizer.phases.phase7_evidence import run_phase7
from agenthorde_common.canonicalizer.phases.phase8_validate import run_phase8
from agenthorde_common.canonicalizer.phases.phase9_output import run_phase9
from agenthorde_common.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalEntity,
    CanonicalFragment,
    CanonicalizerInput,
    CanonicalizerResult,
    Candidates,
    EvidenceArchive,
    Provenance,
    ValidationIssue,
    ValidationReport,
)
from agenthorde_common.canonicalizer.version import CANONICALIZER_VERSION
from agenthorde_common.config import (
    CANONICALIZER_ENTITY_MODEL,
    CANONICALIZER_FUZZY_THRESHOLD,
    CANONICALIZER_MERGE_CANDIDATE_LO,
    CANONICALIZER_METRIC_MODEL,
    CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD,
    CANONICALIZER_RELATION_MODEL,
    HORIZON_FIRESTORE_PROJECT,
)


def _build_fragments(
    entities: list[CanonicalEntity],
    run_id: str,
    *,
    entity_added_at_cycle: dict[str, int] | None = None,
    agent_registry_id_to_entity_id: dict[str, str] | None = None,
    pack_cycle_index: int = 0,
) -> tuple[list[CanonicalFragment], dict[str, str]]:
    """Build CanonicalFragment list, propagating ``addedAtCycle`` provenance from the pack.

    For each entity we look up every namespaced agent id that resolved into it
    (analyzer:e3 → e_42, governance:e7 → e_42, …) and pick the *earliest*
    ``addedAtCycle`` recorded by the supervisor merge. Falls back to the
    pack-level ``cycleIndex`` when the entity exists only in the per-agent blob
    (newly emitted this cycle but not yet persisted into pack via the merge).
    """

    namespace = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    fragments: list[CanonicalFragment] = []
    ent_to_frag: dict[str, str] = {}

    eid_to_ns: dict[str, list[str]] = {}
    if agent_registry_id_to_entity_id:
        for ns, eid in agent_registry_id_to_entity_id.items():
            if not eid:
                continue
            eid_to_ns.setdefault(eid, []).append(ns)

    for ent in entities:
        fid = str(uuid.uuid5(namespace, ent.canonical_path))
        ent_to_frag[ent.entity_id] = fid
        added_at_cycle = 0
        for ns_id in eid_to_ns.get(ent.entity_id, []):
            cyc = (entity_added_at_cycle or {}).get(ns_id, 0)
            if cyc and (added_at_cycle == 0 or cyc < added_at_cycle):
                added_at_cycle = cyc
        if added_at_cycle == 0 and pack_cycle_index > 0:
            added_at_cycle = pack_cycle_index
        attrs = dict(ent.attributes) if ent.attributes else {}
        if added_at_cycle > 0:
            attrs.setdefault("addedAtCycle", added_at_cycle)
        prov_extras: dict[str, Any] = {}
        if added_at_cycle > 0:
            prov_extras["agentFirstCycle"] = added_at_cycle
        provenance = Provenance(
            run_id=run_id,
            canonicalizer_version=CANONICALIZER_VERSION,
            **prov_extras,
        )
        fragments.append(
            CanonicalFragment(
                id=fid,
                canonical_path=ent.canonical_path,
                fragment_class=ent.type,
                subtype=ent.subtype or "",
                surface_forms=list(ent.surface_forms),
                mention_count=ent.mention_count,
                attributes=attrs,
                provenance=provenance,
                confidence=float(ent.per_claim_confidence or 0.7),
            )
        )
    return fragments, ent_to_frag


def _remap_edges(
    edges_raw: list[CanonicalEdge],
    ent_to_frag: dict[str, str],
    *,
    fragment_added_at_cycle: dict[str, int] | None = None,
) -> list[CanonicalEdge]:
    """Remap entity ids to fragment ids and stamp ``addedAtCycle`` on the edge.

    Edge provenance follows the *latest* of its endpoints' ``addedAtCycle``
    values (the edge cannot pre-date the entity it connects). Without an
    endpoint cycle, defaults to no stamp.
    """

    out: list[CanonicalEdge] = []
    fragment_added_at_cycle = fragment_added_at_cycle or {}
    for e in edges_raw:
        sf = ent_to_frag.get(e.source_fragment_id)
        tf = ent_to_frag.get(e.target_fragment_id)
        if not sf or not tf:
            continue
        added_at_cycle = max(
            fragment_added_at_cycle.get(sf, 0),
            fragment_added_at_cycle.get(tf, 0),
        )
        update: dict[str, Any] = {
            "source_fragment_id": sf,
            "target_fragment_id": tf,
        }
        if added_at_cycle > 0:
            attrs = dict(e.attributes or {})
            attrs.setdefault("addedAtCycle", added_at_cycle)
            update["attributes"] = attrs
        out.append(e.model_copy(update=update))
    return out


def _remap_unknown_product_candidates(ent_to_frag: dict[str, str], candidates: Candidates) -> None:
    for u in candidates.unknown_products:
        u.from_site_fragment_id = ent_to_frag.get(u.from_site_fragment_id, u.from_site_fragment_id)
        if u.to_site_fragment_id:
            u.to_site_fragment_id = ent_to_frag.get(u.to_site_fragment_id, u.to_site_fragment_id)


async def run_pipeline(
    inp: CanonicalizerInput,
    llm: LLMPhaseClient | None = None,
    dict_runtime: DictionaryRuntime | None = None,
) -> CanonicalizerResult:
    timings: dict[str, float] = {}
    t0 = time.perf_counter()
    candidates = Candidates()
    run_id = inp.run_id or "unknown-run"

    ws = run_phase0(
        inp.pack,
        inp.analyzer_output,
        inp.governance_output,
        inp.final_answer,
        sanctions_output=inp.sanctions_output,
    )
    timings["phase0_ms"] = (time.perf_counter() - t0) * 1000

    llm = llm or LLMPhaseClient()
    if dict_runtime is None:
        dict_runtime = DictionaryRuntime(HORIZON_FIRESTORE_PROJECT or None)

    if ws.input_issues:
        report = ValidationReport(input_errors=list(ws.input_issues))
        t_done = time.perf_counter()
        for k in ("phase1_ms", "phase2_ms", "phase3_ms", "phase4_ms", "phase5_ms", "phase6_ms", "phase7_ms", "phase9_ms"):
            timings.setdefault(k, 0.0)
        timings["phase8_ms"] = (t_done - t0) * 1000
        return run_phase9([], [], EvidenceArchive(), candidates, report, run_id, timings)

    t1 = time.perf_counter()
    mentions = await run_phase1(ws, llm, CANONICALIZER_ENTITY_MODEL, candidates)
    timings["phase1_ms"] = (time.perf_counter() - t1) * 1000

    t2 = time.perf_counter()
    entities, mention_idx_to_eid = run_phase2(
        mentions,
        CANONICALIZER_FUZZY_THRESHOLD,
        CANONICALIZER_MERGE_CANDIDATE_LO,
        dict_runtime,
        candidates,
    )
    timings["phase2_ms"] = (time.perf_counter() - t2) * 1000

    ws.agent_registry_id_to_entity_id = {}
    for mi, m in enumerate(mentions):
        eid = mention_idx_to_eid.get(mi)
        aid = (m.agent_registry_id or "").strip()
        if eid and aid:
            ws.agent_registry_id_to_entity_id[aid] = eid

    t3 = time.perf_counter()
    entities, path_issues = run_phase3(
        entities,
        ws,
        candidates,
        CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD,
    )
    timings["phase3_ms"] = (time.perf_counter() - t3) * 1000

    fragments, ent_to_frag = _build_fragments(
        entities,
        run_id,
        entity_added_at_cycle=ws.entity_added_at_cycle,
        agent_registry_id_to_entity_id=ws.agent_registry_id_to_entity_id,
        pack_cycle_index=ws.pack_cycle_index,
    )

    t4 = time.perf_counter()
    edges_raw = await run_phase4(
        entities,
        ws,
        llm,
        CANONICALIZER_RELATION_MODEL,
        candidates,
        run_id,
        CANONICALIZER_VERSION,
        dict_runtime=dict_runtime,
    )
    timings["phase4_ms"] = (time.perf_counter() - t4) * 1000

    fragment_added_at_cycle = {
        f.id: int((f.attributes or {}).get("addedAtCycle") or 0) for f in fragments
    }
    edges = _remap_edges(edges_raw, ent_to_frag, fragment_added_at_cycle=fragment_added_at_cycle)
    _remap_unknown_product_candidates(ent_to_frag, candidates)

    t5 = time.perf_counter()
    run_phase5(fragments, edges, dict_runtime, candidates)
    timings["phase5_ms"] = (time.perf_counter() - t5) * 1000

    t6 = time.perf_counter()
    await run_phase6(
        entities,
        fragments,
        edges,
        ws,
        llm,
        CANONICALIZER_METRIC_MODEL,
        run_id,
        CANONICALIZER_VERSION,
        ent_to_frag,
    )
    timings["phase6_ms"] = (time.perf_counter() - t6) * 1000

    t7 = time.perf_counter()
    evidence_archive = run_phase7(inp.tool_ledger, fragments, edges, ws, run_id, CANONICALIZER_VERSION)
    timings["phase7_ms"] = (time.perf_counter() - t7) * 1000

    t8 = time.perf_counter()
    report = run_phase8(fragments, edges)
    if path_issues:
        report.path_errors.extend(path_issues)
    for ut in candidates.unknown_transforms:
        report.transform_errors.append(
            ValidationIssue(
                code="unknown_transform",
                message="Transform pair not in core dictionary",
                detail={
                    "siteFragmentId": ut.site_fragment_id,
                    "fromProduct": ut.from_product,
                    "toProduct": ut.to_product,
                },
            )
        )
    timings["phase8_ms"] = (time.perf_counter() - t8) * 1000
    if dict_runtime.load_error:
        report.dictionary_warnings.append(dict_runtime.load_error)

    t9 = time.perf_counter()
    result = run_phase9(fragments, edges, evidence_archive, candidates, report, run_id, timings)
    timings["phase9_ms"] = (time.perf_counter() - t9) * 1000
    result.provenance.phase_timings_ms = timings
    return result


async def canonicalize(
    inp: CanonicalizerInput | dict[str, Any],
    llm: LLMPhaseClient | None = None,
    dict_runtime: DictionaryRuntime | None = None,
) -> CanonicalizerResult:
    if isinstance(inp, dict):
        inp = CanonicalizerInput.model_validate(inp)
    return await run_pipeline(inp, llm=llm, dict_runtime=dict_runtime)
