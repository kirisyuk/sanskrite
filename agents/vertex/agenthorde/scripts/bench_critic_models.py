"""One-shot bench: same critic-style reasoning prompt across all candidate models.

Each model receives:
  * An English reasoning task that requires non-trivial judgement about
    a small semiconductor supply-chain pack.
  * The strict JSON Schema derived from `CriticVerdict`.
  * `response_format.json_schema.strict=true` (OpenRouter enforces).

For every model we record: wall-clock latency, returned finish_reason,
input/output tokens (from OpenRouter usage field), raw content length,
validated verdict fields, and any error.

Results are emitted as a compact table and also as one JSONL line per
attempt to `/tmp/agenthorde_smoke/critic_bench.jsonl` for later diffing.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from pathlib import Path

# Allow running as `python scripts/bench_critic_models.py` from repo root.
THIS = Path(__file__).resolve()
ROOT = THIS.parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from agenthorde.critic.openrouter_client import (  # noqa: E402
    OpenRouterCriticClient,
    OpenRouterCriticError,
)
from agenthorde.critic.strict_schema import to_openai_strict_schema  # noqa: E402
from agenthorde_common.critic.prompt import render_critic_prompt  # noqa: E402
from agenthorde_common.runtime.state_keys import (  # noqa: E402
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
)
from agenthorde_common.schemas.agents_io import CriticVerdict  # noqa: E402


DEFAULT_MODELS = [
    "z-ai/glm-5.1",
    "deepseek/deepseek-r1",
    "nvidia/nemotron-3-super-120b-a12b",
    "deepseek/deepseek-chat-v3.1",
    "nvidia/nemotron-3-nano-30b-a3b",
    "qwen/qwen3.5-397b-a17b",
    "qwen/qwen3.5-plus-02-15",
    "openai/o4-mini",
]


MODEL_OVERRIDES: dict[str, dict] = {
    "openai/o4-mini": {
        "temperature": None,
        "extra_body": {"reasoning": {"effort": "low"}},
    },
}


_SEMI_USER_QUESTION = (
    "Analyse the advanced-node semiconductor supply chain under CHIPS Act / "
    "US export control pressure. Focus on dependencies between TSMC, ASML, "
    "Samsung, Intel, SMIC, and Japanese/Dutch equipment vendors (Tokyo Electron, "
    "ASML); cover both the design and lithography layers. Flag single points of failure."
)

_SEMI_PACK = {
    "cycleIndex": 4,
    "supervisorCap": 8,
    "researchTemperature": 0.45,
    "reasoningGraph": {
        "nodes": [
            {"id": "n1", "kind": "org", "text": "TSMC", "layer": "supply", "evidenceRefs": ["web:1", "web:4"]},
            {"id": "n2", "kind": "org", "text": "ASML", "layer": "supply", "evidenceRefs": ["web:2"]},
            {"id": "n3", "kind": "org", "text": "Samsung Foundry", "layer": "supply", "evidenceRefs": ["web:3"]},
            {"id": "n4", "kind": "org", "text": "Intel Foundry", "layer": "supply", "evidenceRefs": ["web:5"]},
            {"id": "n5", "kind": "org", "text": "SMIC", "layer": "supply", "evidenceRefs": ["web:6"]},
            {"id": "n6", "kind": "org", "text": "Tokyo Electron", "layer": "supply", "evidenceRefs": ["web:7"]},
            {"id": "n7", "kind": "geo", "text": "Taiwan", "layer": "supply", "evidenceRefs": ["web:1"]},
            {"id": "n8", "kind": "geo", "text": "Netherlands", "layer": "supply", "evidenceRefs": ["web:2"]},
            {"id": "n9", "kind": "product", "text": "EUV lithography system", "layer": "supply", "evidenceRefs": ["web:2"]},
            {"id": "n10", "kind": "regime", "text": "US Export Controls (BIS/EAR)", "layer": "regulation", "evidenceRefs": ["web:8"]},
            {"id": "n11", "kind": "claim", "text": "ASML is the sole supplier of EUV lithography tools worldwide.", "evidenceRefs": ["web:2", "web:9"]},
            {"id": "n12", "kind": "claim", "text": "TSMC manufactures the majority of advanced nodes (<=3nm) globally.", "evidenceRefs": ["web:1", "web:4"]},
        ],
        "edges": [
            {"sourceFragmentId": "n2", "targetFragmentId": "n1", "relation": "supplies"},
            {"sourceFragmentId": "n2", "targetFragmentId": "n3", "relation": "supplies"},
            {"sourceFragmentId": "n2", "targetFragmentId": "n4", "relation": "supplies"},
            {"sourceFragmentId": "n1", "targetFragmentId": "n7", "relation": "incorporated_in"},
            {"sourceFragmentId": "n2", "targetFragmentId": "n8", "relation": "incorporated_in"},
            {"sourceFragmentId": "n10", "targetFragmentId": "n2", "relation": "restricts"},
            {"sourceFragmentId": "n10", "targetFragmentId": "n5", "relation": "restricts"},
        ],
    },
    "openQuestions": [
        "Exact share of global EUV shipments routed through ASML to TSMC vs Samsung vs Intel",
        "Role of Japanese suppliers (Tokyo Electron, JSR, Shin-Etsu) in the photoresist/etch bottleneck",
        "Whether SMIC's N+2 (7nm-class) line can scale without EUV",
    ],
    "toolLedger": {"webSearchCalls": 5, "retrieveCalls": 3},
}

_SEMI_ANALYZER = {
    "chainSummary": (
        "Advanced nodes concentrate at TSMC (Taiwan) and ASML (Netherlands); "
        "export controls target SMIC, funnelling the bottleneck through a single EUV supplier."
    ),
    "proposedEntities": [
        {"id": "e1", "label": "JSR Corporation"},
        {"id": "e2", "label": "Shin-Etsu Chemical"},
    ],
    "proposedNodes": [
        {"id": "c1", "kind": "claim", "text": "JSR Corporation supplies EUV photoresist used by leading-edge fabs."},
        {"id": "c2", "kind": "claim", "text": "Shin-Etsu Chemical is a dominant supplier of advanced photoresist materials."},
    ],
    "proposedRelations": [],
    "openQuestions": [
        "Quantified share of JSR / Shin-Etsu in global EUV photoresist supply",
    ],
    "escalationHints": [
        {
            "reason": "Japanese materials bottleneck unexplored",
            "suggestedAgent": "supply_chain_analyzer",
            "confidence": 0.65,
        }
    ],
}


def _build_critic_prompt() -> str:
    """Construct the critic prompt via the production renderer + synthetic state."""

    state: dict = {
        "ah_user_question": _SEMI_USER_QUESTION,
        STATE_PACK_KEY: _SEMI_PACK,
        STATE_ANALYZER_OUTPUT_KEY: _SEMI_ANALYZER,
        STATE_GOVERNANCE_OUTPUT_KEY: {},
        STATE_SANCTIONS_OUTPUT_KEY: {},
    }
    return render_critic_prompt(state, tools_disabled=True)


def _summarize_text(text: str, n: int = 140) -> str:
    t = " ".join(text.split())
    return t if len(t) <= n else t[: n - 3] + "..."


def _registry_ids_in_prompt(prompt: str) -> set[str]:
    """Extract the set of registry ids from the markdown table embedded in the prompt."""

    import re

    ids: set[str] = set()
    for match in re.finditer(r"^\|\s*`([^`]+)`\s*\|", prompt, flags=re.MULTILINE):
        ids.add(match.group(1).strip())
    return ids


def _ref_stats(verdict: dict | None, registry: set[str]) -> dict:
    """Compute ref-validity / density / specificity stats for a verdict."""

    if not verdict:
        return {"refs_total": 0, "refs_valid": 0, "refs_invalid": 0, "refs_with_any": 0, "items": 0}
    items = list(verdict.get("criticalFindings") or []) + list(
        verdict.get("advisoryFindings") or []
    )
    total = 0
    valid = 0
    invalid: list[str] = []
    items_with_any = 0
    for item in items:
        if not isinstance(item, dict):
            continue
        refs = item.get("refs") or []
        if refs:
            items_with_any += 1
        for r in refs:
            total += 1
            if r in registry:
                valid += 1
            else:
                invalid.append(r)
    return {
        "refs_total": total,
        "refs_valid": valid,
        "refs_invalid": total - valid,
        "refs_with_any": items_with_any,
        "items": len([i for i in items if isinstance(i, dict)]),
        "invalid_examples": invalid[:5],
    }


async def _bench_one(
    client: OpenRouterCriticClient,
    model: str,
    prompt: str,
    schema: dict,
    strict: bool,
    temperature: float,
    timeout_s: float,
    max_output_tokens: int,
) -> dict:
    row: dict = {
        "model": model,
        "ok": False,
        "latency_ms": None,
        "input_tokens": None,
        "output_tokens": None,
        "finish_reason": None,
        "content_chars": None,
        "verdict": None,
        "error": None,
    }
    override = MODEL_OVERRIDES.get(model, {})
    effective_temperature = override["temperature"] if "temperature" in override else temperature
    extra_body = override.get("extra_body")
    t0 = time.perf_counter()
    try:
        result = await client.chat_structured(
            model=model,
            prompt=prompt,
            json_schema=schema,
            strict=strict,
            max_output_tokens=max_output_tokens,
            temperature=effective_temperature,
            extra_body=extra_body,
        )
    except OpenRouterCriticError as exc:
        row["latency_ms"] = int((time.perf_counter() - t0) * 1000)
        row["error"] = f"{type(exc).__name__}: {exc}"
        return row

    row["latency_ms"] = result.latency_ms
    row["input_tokens"] = result.input_tokens
    row["output_tokens"] = result.output_tokens
    row["finish_reason"] = result.finish_reason
    row["content_chars"] = len(result.content)

    try:
        parsed = json.loads(result.content)
    except json.JSONDecodeError as exc:
        row["error"] = f"invalid_json: {exc}"
        row["raw_preview"] = _summarize_text(result.content)
        return row

    try:
        verdict = CriticVerdict.model_validate(parsed)
    except Exception as exc:
        row["error"] = f"schema_fail: {exc}"
        row["raw_preview"] = _summarize_text(result.content)
        return row

    row["ok"] = True
    row["verdict"] = verdict.model_dump(mode="json", by_alias=True)
    return row


async def _main() -> None:
    parser = argparse.ArgumentParser(description="Bench critic-like reasoning across models")
    parser.add_argument("--models", nargs="*", default=DEFAULT_MODELS)
    parser.add_argument("--no-strict", action="store_true")
    parser.add_argument("--temperature", type=float, default=0.2)
    parser.add_argument("--timeout", type=float, default=240.0)
    parser.add_argument("--max-output-tokens", type=int, default=2048)
    parser.add_argument(
        "--jsonl-out",
        default="/tmp/agenthorde_smoke/critic_bench.jsonl",
    )
    args = parser.parse_args()

    if not os.getenv("OPENROUTER_API_KEY"):
        print("ERROR: OPENROUTER_API_KEY is not set", file=sys.stderr)
        sys.exit(2)

    client = OpenRouterCriticClient(timeout_s=args.timeout)
    schema = to_openai_strict_schema(CriticVerdict.model_json_schema(by_alias=True))
    strict = not args.no_strict
    prompt = _build_critic_prompt()
    registry_ids = _registry_ids_in_prompt(prompt)

    out_path = Path(args.jsonl_out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as fh:
        fh.write(json.dumps({"prompt_chars": len(prompt), "strict": strict}) + "\n")

    print(f"prompt_chars={len(prompt)} strict={strict} timeout={args.timeout}s")
    print(f"registry_ids_in_prompt={len(registry_ids)}")
    print(f"models={args.models}\n")
    print(
        f"{'model':<40} {'ok':<4} {'lat_ms':>7} {'in_tok':>7} {'out_tok':>7} "
        f"{'suff':<5} {'comp':<11} {'crit':>4} {'adv':>4} {'refs':>10}  notes"
    )
    print("-" * 145)

    rows: list[dict] = []
    for model in args.models:
        row = await _bench_one(
            client,
            model,
            prompt,
            schema,
            strict,
            args.temperature,
            args.timeout,
            args.max_output_tokens,
        )
        rs = _ref_stats(row.get("verdict"), registry_ids)
        row["ref_stats"] = rs
        rows.append(row)
        ok = "OK" if row["ok"] else "FAIL"
        lat = row["latency_ms"] if row["latency_ms"] is not None else "-"
        itok = row["input_tokens"] if row["input_tokens"] is not None else "-"
        otok = row["output_tokens"] if row["output_tokens"] is not None else "-"
        if row["ok"]:
            v = row["verdict"] or {}
            suff = str(v.get("sufficient"))
            comp = str(v.get("compositionRecommendation", ""))
            blk = len(v.get("criticalFindings") or [])
            nblk = len(v.get("advisoryFindings") or [])
            hint = _summarize_text(str(v.get("missionHint") or ""), 80)
            note = f"mission_hint={hint}"
            refs_col = f"{rs['refs_valid']}/{rs['refs_total']}"
        else:
            suff = comp = "-"
            blk = nblk = 0
            note = row["error"] or ""
            if "raw_preview" in row:
                note += f" preview={row['raw_preview']}"
            refs_col = "-"
        print(
            f"{model:<40} {ok:<4} {str(lat):>7} {str(itok):>7} {str(otok):>7} "
            f"{suff:<5} {comp:<11} {blk:>3} {nblk:>4} {refs_col:>10}  {note}"
        )
        with out_path.open("a", encoding="utf-8") as fh:
            fh.write(json.dumps(row, ensure_ascii=False) + "\n")

    print()
    success = [r for r in rows if r["ok"]]
    failures = [r for r in rows if not r["ok"]]
    print(f"summary: {len(success)} ok / {len(failures)} fail over {len(rows)} models")
    if success:
        best_lat = min(success, key=lambda r: r["latency_ms"] or 10**9)
        print(f"fastest_ok: {best_lat['model']} @ {best_lat['latency_ms']} ms")
    print(f"jsonl={out_path}")


if __name__ == "__main__":
    asyncio.run(_main())
