"""Orchestrates canonicalizer phases 0–9."""

from __future__ import annotations

import time
import uuid
from typing import Any

from agenthorde.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde.canonicalizer.llm_client import LLMPhaseClient
from agenthorde.canonicalizer.phases.phase0_input import run_phase0
from agenthorde.canonicalizer.phases.phase1_entities import run_phase1
from agenthorde.canonicalizer.phases.phase2_dedup import run_phase2
from agenthorde.canonicalizer.phases.phase3_paths import run_phase3
from agenthorde.canonicalizer.phases.phase4_relations import run_phase4
from agenthorde.canonicalizer.phases.phase5_transforms import run_phase5
from agenthorde.canonicalizer.phases.phase6_metrics import run_phase6
from agenthorde.canonicalizer.phases.phase7_evidence import run_phase7
from agenthorde.canonicalizer.phases.phase8_validate import run_phase8
from agenthorde.canonicalizer.phases.phase9_output import run_phase9
from agenthorde.canonicalizer.schemas import (
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
from agenthorde.canonicalizer.version import CANONICALIZER_VERSION
from agenthorde.config import (
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
) -> tuple[list[CanonicalFragment], dict[str, str]]:
    namespace = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    fragments: list[CanonicalFragment] = []
    ent_to_frag: dict[str, str] = {}
    for ent in entities:
        fid = str(uuid.uuid5(namespace, ent.canonical_path))
        ent_to_frag[ent.entity_id] = fid
        fragments.append(
            CanonicalFragment(
                id=fid,
                canonical_path=ent.canonical_path,
                fragment_class=ent.type,
                subtype=ent.subtype or "",
                surface_forms=list(ent.surface_forms),
                mention_count=ent.mention_count,
                attributes=dict(ent.attributes) if ent.attributes else {},
                provenance=Provenance(run_id=run_id, canonicalizer_version=CANONICALIZER_VERSION),
                confidence=float(ent.per_claim_confidence or 0.7),
            )
        )
    return fragments, ent_to_frag


def _remap_edges(edges_raw: list[CanonicalEdge], ent_to_frag: dict[str, str]) -> list[CanonicalEdge]:
    out: list[CanonicalEdge] = []
    for e in edges_raw:
        sf = ent_to_frag.get(e.source_fragment_id)
        tf = ent_to_frag.get(e.target_fragment_id)
        if not sf or not tf:
            continue
        out.append(
            e.model_copy(
                update={
                    "source_fragment_id": sf,
                    "target_fragment_id": tf,
                }
            )
        )
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

    ws = run_phase0(inp.pack, inp.analyzer_output, inp.governance_output, inp.final_answer)
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
    entities = run_phase2(
        mentions,
        CANONICALIZER_FUZZY_THRESHOLD,
        CANONICALIZER_MERGE_CANDIDATE_LO,
        dict_runtime,
        candidates,
    )
    timings["phase2_ms"] = (time.perf_counter() - t2) * 1000

    t3 = time.perf_counter()
    entities, path_issues = run_phase3(
        entities,
        ws,
        candidates,
        CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD,
    )
    timings["phase3_ms"] = (time.perf_counter() - t3) * 1000

    fragments, ent_to_frag = _build_fragments(entities, run_id)

    t4 = time.perf_counter()
    edges_raw = await run_phase4(
        entities,
        ws,
        llm,
        CANONICALIZER_RELATION_MODEL,
        candidates,
        run_id,
        CANONICALIZER_VERSION,
    )
    timings["phase4_ms"] = (time.perf_counter() - t4) * 1000

    edges = _remap_edges(edges_raw, ent_to_frag)
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
