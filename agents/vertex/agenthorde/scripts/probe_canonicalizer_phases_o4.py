#!/usr/bin/env python3
"""Manual OpenRouter probe: canonicalizer phase 4 + phase 6 prompts vs. o4-mini.

Builds user JSON shaped like ``run_phase4`` / ``run_phase6`` payloads, prepends the
same system prefix as ``LLMPhaseClient`` (version + “single JSON object”), appends
packaged prompts from ``agenthorde_common/canonicalizer/prompts/*.txt``, POSTs once per
phase. Requires ``OPENROUTER_API_KEY``.

Usage (from repo):
  cd agents/vertex/agenthorde && ../.venv-agentloom/bin/python scripts/probe_canonicalizer_phases_o4.py
"""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path

import httpx

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

import agenthorde_common
from agenthorde_common.canonicalizer.version import CANONICALIZER_VERSION

PROMPTS = Path(agenthorde_common.__file__).resolve().parent / "canonicalizer" / "prompts"
DEFAULT_INPUT = Path("/tmp/agenthorde_smoke/tsmc_v3_input.json")
MODEL = "openai/o4-mini"
OPENROUTER_URL = "https://openrouter.ai/api/v1/chat/completions"


def _load_input(path: Path) -> dict:
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def _build_phase4_user(dump: dict) -> dict:
    """Compact but structurally identical to ``run_phase4`` payload keys."""
    pack = dump.get("pack") or {}
    rg = pack.get("reasoningGraph") or {}
    nodes = rg.get("nodes") or []
    claims: list[dict[str, str | None]] = []
    for n in nodes[:40]:
        if not isinstance(n, dict):
            continue
        claims.append(
            {
                "id": str(n.get("id") or ""),
                "kind": str(n.get("kind") or "claim"),
                "text": str(n.get("text") or ""),
                "hypothesisId": n.get("hypothesisId") or n.get("hypothesis_id"),
            }
        )
    final_answer = (dump.get("finalAnswer") or "")[:6000]
    relations = pack.get("proposedRelations") or []
    wg = json.dumps(
        {"relations": [{"src": r.get("src"), "dst": r.get("dst"), "relation": r.get("relation")} for r in relations[:25]]},
        ensure_ascii=False,
    )[:4000]

    entities = [
        {
            "entity_id": "e0",
            "type": "org",
            "subtype": "listed_issuer",
            "canonical_path": "/org/tw/taiwan-semiconductor-manufacturing-company",
            "surface_forms": ["TSMC", "Taiwan Semiconductor Manufacturing Company"],
        },
        {
            "entity_id": "e1",
            "type": "org",
            "subtype": "listed_issuer",
            "canonical_path": "/org/nl/asml-holding-nv",
            "surface_forms": ["ASML", "ASML Holding N.V."],
        },
        {
            "entity_id": "e2",
            "type": "product",
            "subtype": "chip",
            "canonical_path": "/product/chip/euv-lithography-system",
            "surface_forms": ["EUV lithography system", "EUV scanner"],
        },
        {
            "entity_id": "e3",
            "type": "site",
            "subtype": "fab",
            "canonical_path": "/site/tw/tsmc-fab-18",
            "surface_forms": ["Fab 18", "TSMC Fab 18"],
        },
        {
            "entity_id": "e4",
            "type": "geo",
            "subtype": None,
            "canonical_path": "/geo/tw/taiwan",
            "surface_forms": ["Taiwan", "Republic of China (Taiwan)"],
        },
    ]
    return {
        "entities": entities,
        "claims": claims,
        "final_answer_excerpt": final_answer,
        "chain_summaries": [(dump.get("analyzer_output") or {}).get("chainSummary") or ""][:1],
        "working_graph_excerpt": wg,
        "known_edges": [],
    }


def _build_phase6_user(dump: dict) -> dict:
    pack = dump.get("pack") or {}
    rg = pack.get("reasoningGraph") or {}
    nodes = rg.get("nodes") or []
    claims = [{"id": str(n.get("id")), "text": str(n.get("text") or "")} for n in nodes[:15] if isinstance(n, dict) and n.get("id")]
    entities = [
        {"entity_id": "e0", "path": "/org/tw/taiwan-semiconductor-manufacturing-company"},
        {"entity_id": "e1", "path": "/org/nl/asml-holding-nv"},
    ]
    return {"claims": claims, "entities": entities}


def _system_for_phase(phase_file: str) -> str:
    base = (PROMPTS / phase_file).read_text(encoding="utf-8")
    return (
        f"You are the AgentHorde canonicalizer ({CANONICALIZER_VERSION}). "
        "Output a single JSON object only, no markdown.\n\n"
        f"{base}"
    )


def _call_o4(system: str, user_json: str) -> tuple[float, int, str, dict]:
    key = (os.environ.get("OPENROUTER_API_KEY") or "").strip()
    if not key:
        raise SystemExit("OPENROUTER_API_KEY is not set")
    body = {
        "model": MODEL,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user_json},
        ],
        "response_format": {"type": "json_object"},
        "reasoning": {"effort": "low"},
        "max_tokens": 8192,
    }
    t0 = time.perf_counter()
    r = httpx.post(
        OPENROUTER_URL,
        headers={
            "Authorization": f"Bearer {key}",
            "Content-Type": "application/json",
            "HTTP-Referer": "https://local.agenthorde.probe",
            "X-Title": "agenthorde-canonicalizer-phase-probe",
        },
        json=body,
        timeout=240.0,
    )
    elapsed = time.perf_counter() - t0
    r.raise_for_status()
    data = r.json()
    content = (data.get("choices") or [{}])[0].get("message", {}).get("content") or ""
    parsed = json.loads(content)
    top_keys = list(parsed.keys())
    n_rel = len(parsed.get("relations") or [])
    n_met = len(parsed.get("metrics") or [])
    summary = f"keys={top_keys} relations={n_rel} metrics={n_met} usage={data.get('usage')}"
    return elapsed, n_rel or n_met, summary, parsed


def main() -> None:
    inp_path = Path(os.environ.get("PROBE_INPUT_JSON", str(DEFAULT_INPUT)))
    dump = _load_input(inp_path)

    for label, fname, user_obj in (
        ("phase4", "phase4_relations.txt", _build_phase4_user(dump)),
        ("phase6", "phase6_metrics.txt", _build_phase6_user(dump)),
    ):
        system = _system_for_phase(fname)
        user_str = json.dumps(user_obj, ensure_ascii=False)
        print(f"=== {label}  model={MODEL}  user_bytes={len(user_str)}  system_bytes={len(system)} ===")
        elapsed, n, summary, parsed = _call_o4(system, user_str)
        print(f"elapsed={elapsed:.2f}s  {summary}")
        if label == "phase4":
            for i, row in enumerate((parsed.get("relations") or [])[:5]):
                print(f"  rel[{i}]: {row}")
        else:
            for i, row in enumerate((parsed.get("metrics") or [])[:8]):
                print(f"  met[{i}]: {row}")
        print()


if __name__ == "__main__":
    main()
