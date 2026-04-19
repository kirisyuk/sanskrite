"""Industrial-depth probe across LLMs.

Open-ended industrial knowledge probe — no JSON schema, no role contract,
no function calling. The model is asked to answer six escalating questions
about a real industrial scenario in prose. We capture:

  * wall-clock latency
  * input / output token usage (from OpenRouter `usage` field)
  * raw response text (preserved in JSONL for spot-check)
  * marker hits per knowledge tier (tier1 / tier2 / tier3) computed from
    a sibling ``markers.yaml`` rubric the model never sees

Scoring is intentionally conservative — substring presence only, no
LLM-as-judge in v0. The point is to see the **distribution** of depth
across models, not to claim absolute correctness.

Layout consumed:

  bench/packs/industrial-depth-2026-04/
    <vertical>/
      prompt.md      # plain text, sent verbatim as a single user message
      markers.yaml   # tier1 / tier2 / tier3 lists for marker scoring

Usage:

  python scripts/bench_industrial_depth.py \
    --pack battery_critical_minerals \
    --jsonl-out /tmp/industrial_depth_battery.jsonl \
    --models openrouter/elephant-alpha nvidia/nemotron-3-super-120b-a12b:free

  python scripts/bench_industrial_depth.py --all-packs \
    --jsonl-out-dir /tmp/industrial_depth_2026-04
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import httpx
import yaml

THIS = Path(__file__).resolve()
ROOT = THIS.parent.parent
REPO_ROOT = ROOT.parent.parent.parent  # agents/vertex/agenthorde -> repo root

PACKS_DIR = REPO_ROOT / "bench" / "packs" / "industrial-depth-2026-04"

OPENROUTER_BASE = "https://openrouter.ai/api/v1"

DEFAULT_MODELS_FREE = [
    "openrouter/elephant-alpha",
    "nvidia/nemotron-3-super-120b-a12b:free",
    "nvidia/nemotron-3-nano-30b-a3b:free",
    "nvidia/nemotron-nano-12b-v2-vl:free",
    "nvidia/nemotron-nano-9b-v2:free",
    "arcee-ai/trinity-large-preview:free",
    "z-ai/glm-4.5-air:free",
    "google/gemma-4-31b-it:free",
    "google/gemma-4-26b-a4b-it:free",
    "qwen/qwen3-next-80b-a3b-instruct:free",
    "qwen/qwen3-coder:free",
    "meta-llama/llama-3.3-70b-instruct:free",
    "openai/gpt-oss-120b:free",
    "openai/gpt-oss-20b:free",
    "minimax/minimax-m2.5:free",
]

def _discover_packs() -> list[str]:
    """Auto-discover packs as subdirectories of PACKS_DIR with prompt.md + markers.yaml."""
    if not PACKS_DIR.is_dir():
        return []
    found: list[str] = []
    for entry in sorted(PACKS_DIR.iterdir()):
        if not entry.is_dir():
            continue
        if (entry / "prompt.md").is_file() and (entry / "markers.yaml").is_file():
            found.append(entry.name)
    return found


ALL_PACKS = _discover_packs()


@dataclass(slots=True)
class PackBundle:
    name: str
    prompt: str
    tier1: list[str] = field(default_factory=list)
    tier2: list[str] = field(default_factory=list)
    tier3: list[str] = field(default_factory=list)


def _load_pack(name: str) -> PackBundle:
    pack_dir = PACKS_DIR / name
    prompt_path = pack_dir / "prompt.md"
    markers_path = pack_dir / "markers.yaml"
    if not prompt_path.is_file():
        raise FileNotFoundError(f"prompt.md not found at {prompt_path}")
    if not markers_path.is_file():
        raise FileNotFoundError(f"markers.yaml not found at {markers_path}")
    prompt = prompt_path.read_text(encoding="utf-8").strip()
    raw_markers = yaml.safe_load(markers_path.read_text(encoding="utf-8")) or {}
    return PackBundle(
        name=name,
        prompt=prompt,
        tier1=[str(m) for m in raw_markers.get("tier1") or []],
        tier2=[str(m) for m in raw_markers.get("tier2") or []],
        tier3=[str(m) for m in raw_markers.get("tier3") or []],
    )


def _score_text(text: str, markers: list[str]) -> tuple[int, list[str]]:
    """Case-insensitive substring match. Returns (hit_count, hit_examples)."""
    if not text or not markers:
        return 0, []
    haystack = text.lower()
    hits: list[str] = []
    for marker in markers:
        needle = marker.lower().strip()
        if not needle:
            continue
        if needle in haystack:
            hits.append(marker)
    return len(hits), hits


def _summarize(text: str, n: int = 220) -> str:
    one = " ".join(text.split())
    return one if len(one) <= n else one[: n - 3] + "..."


@dataclass(slots=True)
class CallResult:
    ok: bool
    latency_ms: int | None = None
    input_tokens: int | None = None
    output_tokens: int | None = None
    finish_reason: str | None = None
    content: str = ""
    error: str | None = None
    provider: str | None = None
    attempts: int = 1


async def _call_openrouter(
    client: httpx.AsyncClient,
    *,
    api_key: str,
    model: str,
    prompt: str,
    max_tokens: int,
    temperature: float | None,
    timeout_s: float,
    retries: int,
    backoff_s: float,
) -> CallResult:
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    title = (os.getenv("OPENROUTER_APP_TITLE") or "").strip()
    referer = (os.getenv("OPENROUTER_HTTP_REFERER") or "").strip()
    if title:
        headers["X-Title"] = title
    if referer:
        headers["Referer"] = referer

    body: dict[str, Any] = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": int(max_tokens),
    }
    if temperature is not None:
        body["temperature"] = float(temperature)

    last_error: str | None = None
    last_status: int | None = None
    attempts = 0
    for attempt in range(1, retries + 2):
        attempts = attempt
        t0 = time.perf_counter()
        try:
            resp = await client.post(
                f"{OPENROUTER_BASE}/chat/completions",
                headers=headers,
                content=json.dumps(body),
                timeout=timeout_s,
            )
        except httpx.TimeoutException as exc:
            last_error = f"timeout: {exc}"
            await asyncio.sleep(backoff_s * attempt)
            continue
        except httpx.HTTPError as exc:
            last_error = f"http_error: {exc}"
            await asyncio.sleep(backoff_s * attempt)
            continue
        latency_ms = int((time.perf_counter() - t0) * 1000)
        last_status = resp.status_code
        if resp.status_code >= 500 or resp.status_code == 429:
            snippet = (resp.text or "")[:300]
            last_error = f"status={resp.status_code} body={snippet}"
            await asyncio.sleep(backoff_s * attempt)
            continue
        if resp.status_code != 200:
            snippet = (resp.text or "")[:600]
            return CallResult(
                ok=False,
                latency_ms=latency_ms,
                error=f"status={resp.status_code} body={snippet}",
                attempts=attempts,
            )
        try:
            payload = resp.json()
        except json.JSONDecodeError as exc:
            return CallResult(
                ok=False,
                latency_ms=latency_ms,
                error=f"invalid_json_body: {exc}",
                attempts=attempts,
            )
        choices = payload.get("choices") or []
        if not choices:
            return CallResult(
                ok=False,
                latency_ms=latency_ms,
                error="empty_choices",
                attempts=attempts,
            )
        msg = (choices[0] or {}).get("message") or {}
        content = (msg.get("content") or "").strip()
        finish = str((choices[0] or {}).get("finish_reason") or "")
        if not content:
            return CallResult(
                ok=False,
                latency_ms=latency_ms,
                error=f"empty_content (finish={finish})",
                attempts=attempts,
            )
        usage = payload.get("usage") or {}
        provider = str(payload.get("provider") or payload.get("provider_name") or "")
        return CallResult(
            ok=True,
            latency_ms=latency_ms,
            input_tokens=int(usage.get("prompt_tokens") or 0),
            output_tokens=int(usage.get("completion_tokens") or 0),
            finish_reason=finish,
            content=content,
            provider=provider or None,
            attempts=attempts,
        )
    return CallResult(
        ok=False,
        error=f"retries_exhausted last_status={last_status} last_error={last_error}",
        attempts=attempts,
    )


async def _bench_pack(
    *,
    pack: PackBundle,
    api_key: str,
    models: list[str],
    out_path: Path,
    max_tokens: int,
    temperature: float | None,
    timeout_s: float,
    retries: int,
    backoff_s: float,
    runs: int = 1,
) -> list[dict]:
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(
        json.dumps(
            {
                "pack": pack.name,
                "prompt_chars": len(pack.prompt),
                "tier1_markers": len(pack.tier1),
                "tier2_markers": len(pack.tier2),
                "tier3_markers": len(pack.tier3),
                "n_models": len(models),
                "runs_per_cell": runs,
            }
        )
        + "\n",
        encoding="utf-8",
    )

    print(
        f"\n=== pack={pack.name} prompt_chars={len(pack.prompt)} "
        f"models={len(models)} runs={runs} ==="
    )
    print(
        f"{'model':<46} {'run':>3} {'ok':<4} {'lat_ms':>8} {'in':>6} {'out':>6} "
        f"{'t1':>4} {'t2':>4} {'t3':>4}  notes"
    )
    print("-" * 135)

    rows: list[dict] = []
    async with httpx.AsyncClient() as client:
        for model in models:
            for run_idx in range(1, runs + 1):
                res = await _call_openrouter(
                    client,
                    api_key=api_key,
                    model=model,
                    prompt=pack.prompt,
                    max_tokens=max_tokens,
                    temperature=temperature,
                    timeout_s=timeout_s,
                    retries=retries,
                    backoff_s=backoff_s,
                )
                t1_n, t1_hits = _score_text(res.content, pack.tier1)
                t2_n, t2_hits = _score_text(res.content, pack.tier2)
                t3_n, t3_hits = _score_text(res.content, pack.tier3)
                row = {
                    "pack": pack.name,
                    "model": model,
                    "run_index": run_idx,
                    "ok": res.ok,
                    "latency_ms": res.latency_ms,
                    "input_tokens": res.input_tokens,
                    "output_tokens": res.output_tokens,
                    "finish_reason": res.finish_reason,
                    "provider": res.provider,
                    "attempts": res.attempts,
                    "content_chars": len(res.content) if res.ok else 0,
                    "tier1_hits": t1_n,
                    "tier2_hits": t2_n,
                    "tier3_hits": t3_n,
                    "tier1_examples": t1_hits[:8],
                    "tier2_examples": t2_hits[:8],
                    "tier3_examples": t3_hits[:8],
                    "content": res.content,
                    "error": res.error,
                }
                rows.append(row)
                ok = "OK" if res.ok else "FAIL"
                if res.ok:
                    note = _summarize(res.content, 80)
                else:
                    note = (res.error or "")[:120]
                print(
                    f"{model:<46} {run_idx:>3} {ok:<4} {str(res.latency_ms or '-'):>8} "
                    f"{str(res.input_tokens or '-'):>6} {str(res.output_tokens or '-'):>6} "
                    f"{t1_n:>4} {t2_n:>4} {t3_n:>4}  {note}"
                )
                with out_path.open("a", encoding="utf-8") as fh:
                    fh.write(json.dumps(row, ensure_ascii=False) + "\n")
    return rows


def _print_pack_summary(rows: list[dict], pack: PackBundle) -> None:
    print(f"\n--- summary pack={pack.name} ---")
    ok_rows = [r for r in rows if r["ok"]]
    fail_rows = [r for r in rows if not r["ok"]]
    print(f"  ok={len(ok_rows)}  fail={len(fail_rows)}  total={len(rows)}")
    if not ok_rows:
        return
    sorted_by_depth = sorted(
        ok_rows,
        key=lambda r: (r["tier3_hits"], r["tier2_hits"], r["tier1_hits"]),
        reverse=True,
    )
    print("  depth ranking (tier3 desc, tie-break tier2, tier1):")
    for r in sorted_by_depth[:8]:
        print(
            f"    {r['model']:<46} "
            f"t1={r['tier1_hits']:>2}/{len(pack.tier1):>2}  "
            f"t2={r['tier2_hits']:>2}/{len(pack.tier2):>2}  "
            f"t3={r['tier3_hits']:>2}/{len(pack.tier3):>2}  "
            f"{r['latency_ms']/1000:.1f}s  out={r['output_tokens']}"
        )
    fastest = min(ok_rows, key=lambda r: r["latency_ms"] or 10**9)
    print(
        f"  fastest_ok: {fastest['model']} @ {fastest['latency_ms']}ms "
        f"(t1={fastest['tier1_hits']} t2={fastest['tier2_hits']} t3={fastest['tier3_hits']})"
    )


async def _main() -> None:
    parser = argparse.ArgumentParser(description="Industrial-depth probe across LLMs")
    parser.add_argument(
        "--pack",
        choices=ALL_PACKS,
        help="Run a single pack. Mutually exclusive with --all-packs.",
    )
    parser.add_argument(
        "--all-packs",
        action="store_true",
        help="Run all 4 packs sequentially, writing one JSONL per pack into --jsonl-out-dir.",
    )
    parser.add_argument("--models", nargs="*", default=DEFAULT_MODELS_FREE)
    parser.add_argument("--max-tokens", type=int, default=4096)
    parser.add_argument("--temperature", type=float, default=0.2)
    parser.add_argument("--timeout", type=float, default=240.0)
    parser.add_argument("--retries", type=int, default=1, help="Retries on 429/5xx/timeout per call")
    parser.add_argument("--backoff", type=float, default=4.0)
    parser.add_argument(
        "--runs",
        type=int,
        default=1,
        help="How many independent calls to make per (model, pack) cell. Useful for variance probes.",
    )
    parser.add_argument(
        "--jsonl-out",
        default=None,
        help="Output JSONL path (single-pack mode).",
    )
    parser.add_argument(
        "--jsonl-out-dir",
        default=None,
        help="Output directory for one JSONL per pack (--all-packs mode).",
    )
    args = parser.parse_args()

    if not args.pack and not args.all_packs:
        parser.error("specify --pack <name> or --all-packs")
    if args.pack and args.all_packs:
        parser.error("--pack and --all-packs are mutually exclusive")

    api_key = (os.getenv("OPENROUTER_API_KEY") or "").strip().strip("'\"")
    if not api_key:
        print("ERROR: OPENROUTER_API_KEY is not set", file=sys.stderr)
        sys.exit(2)

    if args.all_packs:
        out_dir = Path(args.jsonl_out_dir or "/tmp/industrial_depth_2026-04")
        out_dir.mkdir(parents=True, exist_ok=True)
        all_rows: dict[str, list[dict]] = {}
        for pack_name in ALL_PACKS:
            pack = _load_pack(pack_name)
            jsonl_path = out_dir / f"{pack_name}.jsonl"
            rows = await _bench_pack(
                pack=pack,
                api_key=api_key,
                models=args.models,
                out_path=jsonl_path,
                max_tokens=args.max_tokens,
                temperature=args.temperature,
                timeout_s=args.timeout,
                retries=args.retries,
                backoff_s=args.backoff,
                runs=args.runs,
            )
            _print_pack_summary(rows, pack)
            all_rows[pack_name] = rows
        print(f"\nALL DONE. JSONL files in: {out_dir}")
        # Cross-pack quick view
        print("\n=== cross-pack tier3 leaderboard (sum of tier3 hits across packs) ===")
        agg: dict[str, dict[str, int]] = {}
        for pack_name, rows in all_rows.items():
            for r in rows:
                if not r["ok"]:
                    continue
                a = agg.setdefault(r["model"], {"t1": 0, "t2": 0, "t3": 0, "ok_packs": 0})
                a["t1"] += r["tier1_hits"]
                a["t2"] += r["tier2_hits"]
                a["t3"] += r["tier3_hits"]
                a["ok_packs"] += 1
        ranked = sorted(agg.items(), key=lambda kv: (kv[1]["t3"], kv[1]["t2"], kv[1]["t1"]), reverse=True)
        for model, a in ranked:
            print(
                f"  {model:<46}  "
                f"packs_ok={a['ok_packs']}/{len(ALL_PACKS)}  "
                f"Σt1={a['t1']:>3}  Σt2={a['t2']:>3}  Σt3={a['t3']:>3}"
            )
        return

    pack = _load_pack(args.pack)
    out_path = Path(args.jsonl_out or f"/tmp/industrial_depth_{args.pack}.jsonl")
    rows = await _bench_pack(
        pack=pack,
        api_key=api_key,
        models=args.models,
        out_path=out_path,
        max_tokens=args.max_tokens,
        temperature=args.temperature,
        timeout_s=args.timeout,
        retries=args.retries,
        backoff_s=args.backoff,
        runs=args.runs,
    )
    _print_pack_summary(rows, pack)
    print(f"\njsonl={out_path}")


if __name__ == "__main__":
    asyncio.run(_main())
