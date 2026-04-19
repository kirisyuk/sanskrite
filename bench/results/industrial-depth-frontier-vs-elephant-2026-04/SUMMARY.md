# Industrial Depth — Frontier vs Elephant · 2026-04-19

Apple-to-apples test of `openrouter/elephant-alpha` (free, provider `Stealth`) against five paid frontier models on two industrial depth packs. Single-shot per (model × pack), `temperature=0.2`, `max_tokens=8192`, `timeout=600s`, OpenRouter routing.

## Models

| Model | OpenRouter slug | Tier |
|---|---|---|
| Elephant (stealth, free) | `openrouter/elephant-alpha` | free |
| Claude Opus 4.7 | `anthropic/claude-opus-4.7` | paid frontier |
| GPT-5.4 | `openai/gpt-5.4` | paid frontier |
| Gemini 3.1 Pro Preview | `google/gemini-3.1-pro-preview` | paid frontier |
| GLM-5.1 | `z-ai/glm-5.1` | paid frontier |
| Kimi K2.5 | `moonshotai/kimi-k2.5` | paid frontier |

## Packs

- `steel_cbam` — European integrated steelmaker preparing for full CBAM enforcement in 2026 (HBI-DRI-EAF transition, low-CO2 offtake, iron-ore renegotiation, GOES, ETS reform)
- `fertilizer_ag_inputs` — Brazilian agribusiness conglomerate locking 2027 contracts under Russian/Belarusian sanctions and China P export curbs

## Results — `steel_cbam`

| Model | tier1 / 29 | tier2 / 72 | tier3 / 76 | latency | out tok | usd |
|---|---:|---:|---:|---:|---:|---:|
| `z-ai/glm-5.1`              | 28 | 33 | **22** | 123.2s | 6 545 | 0.0147 |
| `openai/gpt-5.4`            | 28 | 40 | 20 | 113.0s | 5 726 | 0.0579 |
| `anthropic/claude-opus-4.7` | 27 | 42 | 19 | 65.3s  | 5 460 | 0.4232 |
| `moonshotai/kimi-k2.5`      | 28 | 40 | 19 | 435.1s | 8 192 | 0.0143 |
| `google/gemini-3.1-pro`     | 29 | 40 | 17 | 144.1s | 4 973 | 0.0256 |
| `openrouter/elephant-alpha` | 27 | 32 | **14** | **11.5s**  | 2 170 | **$0** |

## Results — `fertilizer_ag_inputs`

| Model | tier1 / 27 | tier2 / 76 | tier3 / 67 | latency | out tok | usd |
|---|---:|---:|---:|---:|---:|---:|
| `openai/gpt-5.4`            | 27 | 32 | **15** | 130.3s | 6 063 | 0.0613 |
| `anthropic/claude-opus-4.7` | 27 | 31 | 14 | 87.6s  | 7 395 | 0.5687 |
| `google/gemini-3.1-pro`     | 27 | 22 | 9  | 42.1s  | 4 761 | 0.0245 |
| `z-ai/glm-5.1`              | 26 | 26 | 8  | 151.8s | 5 813 | 0.0131 |
| `openrouter/elephant-alpha` | 27 | 27 | **2**  | **7.2s**   | 1 120 | **$0** |
| `moonshotai/kimi-k2.5`      | — | — | FAIL | 65.5s | — | — |

Kimi failure on `fertilizer_ag_inputs`: `empty_content (finish="")` from provider `AtlasCloud` — provider-level failure, not model-level.

## Coverage ceiling — what even the union of all 5 paid frontier models captured

| Pack | rubric tier3 | union (5 paid) | union % |
|---|---:|---:|---:|
| `steel_cbam` | 76 | 31 | 41% |
| `fertilizer_ag_inputs` | 67 | 16 | 24% |

Even pooling the 5 paid frontier responses, the rubric was only 41% covered on steel and 24% on fertilizer.

## Cost ledger

- `claude-opus-4.7` consumed **83% of the total spend** ($0.99 of $1.20) and was not the tier-3 leader on either pack.
- `glm-5.1` led steel tier-3 at **$0.0007 per tier-3 marker** vs Opus at **$0.022 per tier-3 marker** — 30× cheaper per unit of industrial depth on this pack.
- `elephant-alpha` produced 14 + 2 = 16 tier-3 markers across both packs at **$0** — a dollar-cost lower bound; the relevant cost is operational (variance, routing-alias polysemy under strict-JSON, see prior post).

Total batch cost: **$1.203** (12 OK calls + 1 failed Kimi call which was not billed).

## Reproduction

```bash
MODELS=(openrouter/elephant-alpha anthropic/claude-opus-4.7 \
        openai/gpt-5.4 google/gemini-3.1-pro-preview \
        z-ai/glm-5.1 moonshotai/kimi-k2.5)
for pack in steel_cbam fertilizer_ag_inputs; do
  agents/vertex/.venv-agentloom/bin/python -u \
    agents/vertex/agenthorde/scripts/bench_industrial_depth.py \
    --pack "$pack" \
    --models "${MODELS[@]}" \
    --max-tokens 8192 --temperature 0.2 --timeout 600 \
    --jsonl-out "bench/results/industrial-depth-frontier-vs-elephant-2026-04/${pack}_frontier6_8k.jsonl"
done
```

Note: `max_tokens=8192` is load-bearing for thinking models. At `max_tokens=2048` on the same prompts, `glm-5.1` and `kimi-k2.5` both returned `empty_content (finish=length)` — the whole budget was consumed by hidden reasoning, leaving zero visible tokens. This is a benchmarking trap; see article §Methodology.

## Notable per-model behavior

- **Opus 4.7** wrote the most structured advisory memos; explicitly admitted uncertainty in one place: *"I may be confusing details here — verify"* (Cogent / Tata Steel UK on steel pack). Highest cost per marker.
- **GPT-5.4** consistently the most verbose (26 446 / 27 690 output chars), with frequent "I am precise where confident; where moving I say so" hedges. Tier-3 leader on fertilizer.
- **Gemini 3.1 Pro** produced compact answers (4 761 — 4 973 out tokens), roughly Opus-quality at 1/16th the cost.
- **GLM-5.1** wrote in executive-memo register; tier-3 leader on steel, narrowly behind Opus on fertilizer.
- **Kimi K2.5** verbose like GPT-5.4 but with ~8K output tokens and 305-435s latency; one provider-level failure.
- **Elephant** consistently fastest (~7-11s), shortest answers (1 120 — 2 170 out tokens), tier-1/2 competitive on both packs but tier-3 limited — especially on fertilizer.

Raw JSONL files alongside this SUMMARY.md.
