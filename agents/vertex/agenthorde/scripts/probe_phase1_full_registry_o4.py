#!/usr/bin/env python3
"""One real phase-1 run: full ``analyzer_entities`` + prose (same as pipeline).

Loads a canonicalizer-style JSON dump (``pack``, ``analyzerOutput``, …),
runs phase 0 → phase 1 with ``LLMPhaseClient`` (OpenRouter when model is
``provider/name`` and ``OPENROUTER_API_KEY`` is set).

If ``pack.proposedEntities`` is empty, optionally builds a synthetic registry from
unique ``proposedRelations`` endpoints (``--from-relations``) so large relation
graphs still exercise a fat registry.

Usage::

  cd agents/vertex/agenthorde
  export OPENROUTER_API_KEY=...
  ../.venv-agentloom/bin/python scripts/probe_phase1_full_registry_o4.py /path/to/dump.json
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.phases.phase0_input import run_phase0
from agenthorde_common.canonicalizer.phases.phase1_entities import run_phase1
from agenthorde_common.canonicalizer.schemas import Candidates
from agenthorde.config import CANONICALIZER_ENTITY_MODEL, CANONICALIZER_PHASE1_CHUNK_MAX


def _synthetic_entities_from_relations(pack: dict) -> list[dict]:
    rels = pack.get("proposedRelations") or pack.get("proposed_relations") or []
    seen: set[str] = set()
    order: list[str] = []
    for r in rels:
        if not isinstance(r, dict):
            continue
        for k in ("src", "dst", "srcId", "src_id", "dstId", "dst_id"):
            s = str(r.get(k) or "").strip()
            if not s or s in seen:
                continue
            seen.add(s)
            order.append(s)
    out: list[dict] = []
    for i, slug in enumerate(order, start=1):
        ns = slug if ":" in slug else f"analyzer:{slug}"
        label = slug.split(":", 1)[-1].replace("_", " ").strip().title() or slug
        out.append({"namespacedId": ns, "label": label, "origin": "synthetic_probe"})
    return out


async def _run(inp: dict, *, from_relations: bool) -> None:
    pack = dict(inp.get("pack") or {})
    if from_relations and not (
        pack.get("proposedEntities") or pack.get("proposed_entities")
    ):
        synth = _synthetic_entities_from_relations(pack)
        if synth:
            pack["proposedEntities"] = synth
            print(f"injected synthetic proposedEntities: {len(synth)} rows from relation endpoints")

    ao = inp.get("analyzerOutput") or inp.get("analyzer_output") or {}
    go = inp.get("governanceOutput") or inp.get("governance_output") or {}
    fa = inp.get("finalAnswer") or inp.get("final_answer") or ""

    t0 = time.perf_counter()
    ws = run_phase0(pack, ao, go, fa)
    t0_ms = (time.perf_counter() - t0) * 1000
    if ws.input_issues:
        print("phase0 input_issues:", ws.input_issues)
        return
    n_ent = len(ws.analyzer_entities or [])
    print(
        f"phase0_ms={t0_ms:.1f}  registry_rows={n_ent}  "
        f"CHUNK_MAX={CANONICALIZER_PHASE1_CHUNK_MAX}  model={CANONICALIZER_ENTITY_MODEL!r}"
    )
    if n_ent == 0:
        print("no analyzer_entities after phase0 — nothing to send to phase1")
        return

    llm = LLMPhaseClient()
    candidates = Candidates()
    t1 = time.perf_counter()
    mentions = await run_phase1(ws, llm, CANONICALIZER_ENTITY_MODEL, candidates)
    t1_ms = (time.perf_counter() - t1) * 1000
    with_hint = sum(
        1
        for m in mentions
        if (m.context_hints.iso2_hint if m.context_hints else None)
    )
    print(f"phase1_ms={t1_ms:.1f}  mentions={len(mentions)}  iso2Hint_non_null={with_hint}/{len(mentions)}")
    print(f"registry_coverage_issues={len(candidates.registry_coverage_issues)}")
    if candidates.registry_coverage_issues[:5]:
        for iss in candidates.registry_coverage_issues[:5]:
            print(f"  gap: {iss.agent_registry_id} {iss.reason}")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("json_path", type=Path, nargs="?", default=Path("/tmp/agenthorde_smoke/tsmc_v3_input.json"))
    ap.add_argument(
        "--from-relations",
        action="store_true",
        help="If pack has no proposedEntities, build registry from proposedRelations endpoints",
    )
    args = ap.parse_args()
    raw = json.loads(args.json_path.read_text(encoding="utf-8"))
    asyncio.run(_run(raw, from_relations=args.from_relations))


if __name__ == "__main__":
    main()
