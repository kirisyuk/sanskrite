# Industrial Depth — Three Recognizable Verticals · 2026-04-19

Apple-to-apples test of `openrouter/elephant-alpha` (free, provider `Stealth`) against five paid frontier models on three verticals chosen for self-recognition by business operators: **FMCG**, **European banking under Basel III endgame**, and **commercial pharma 2026** (IRA / GLP-1 / biosimilar / 340B / PBM). Single-shot per (model × pack), `temperature=0.2`, `max_tokens=8192`, `timeout=600s`, OpenRouter routing.

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

- `fmcg_consumer_goods` — Global FMCG company preparing 2027 procurement, packaging, channel; EU PPWR 2030/2040 targets, retailer concentration, tariffs, soft commodities, retail media networks.
- `banking_basel_2026` — €750B European universal bank preparing for Basel III endgame, SREP 2026, MREL/TLAC under BRRD3, SA-OR, AMLA, climate Pillar 2.
- `pharma_commercial_2026` — Top-15 pharma company; IRA Medicare price negotiation, GLP-1 supply, biosimilar wave (Stelara, Humira, Eylea, Keytruda LOE), retail pharmacy collapse, 340B reform, China API decoupling.

## Results — depth ranking by tier-3 hits

### `fmcg_consumer_goods` (87 tier-3 markers)

| Model | tier1 / 29 | tier2 / 43 | tier3 / 87 | latency | out tok | usd |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic/claude-opus-4.7` | 23 | 27 | **20** | 66.2s  | 5 477 | 0.4279 |
| `openai/gpt-5.4`            | 24 | 32 | 19 | 130.9s | 5 700 | 0.0578 |
| `z-ai/glm-5.1`              | 22 | 29 | 19 | 59.8s  | 6 121 | 0.0139 |
| `google/gemini-3.1-pro`     | 23 | 29 | 18 | 53.7s  | 6 203 | 0.0319 |
| `openrouter/elephant-alpha` | 23 | 28 | 17 | **9.3s**   | 1 616 | **$0** |
| `moonshotai/kimi-k2.5`      | 24 | 35 | 15 | 464.3s | 8 022 | 0.0141 |

### `banking_basel_2026` (88 tier-3 markers)

| Model | tier1 / 32 | tier2 / 52 | tier3 / 88 | latency | out tok | usd |
|---|---:|---:|---:|---:|---:|---:|
| `openai/gpt-5.4`            | 29 | 44 | **18** | 112.6s | 6 023 | 0.0611 |
| `anthropic/claude-opus-4.7` | 27 | 37 | 15 | 87.7s  | 5 071 | 0.3976 |
| `moonshotai/kimi-k2.5`      | 25 | 33 | 13 | 205.6s | 8 192 | 0.0144 |
| `z-ai/glm-5.1`              | 25 | 40 | 12 | 63.6s  | 5 865 | 0.0133 |
| `google/gemini-3.1-pro`     | 26 | 44 | 11 | 57.5s  | 5 538 | 0.0286 |
| `openrouter/elephant-alpha` | 25 | 36 | 8  | **9.9s**   | 1 884 | **$0** |

### `pharma_commercial_2026` (102 tier-3 markers)

| Model | tier1 / 32 | tier2 / 54 | tier3 / 102 | latency | out tok | usd |
|---|---:|---:|---:|---:|---:|---:|
| `moonshotai/kimi-k2.5`      | 32 | 42 | **43** | 141.7s | 7 456 | 0.0132 |
| `z-ai/glm-5.1`              | 32 | 43 | 41 | 82.8s  | 4 188 | 0.0098 |
| `openrouter/elephant-alpha` | 32 | 36 | **40** | **17.7s**  | 1 715 | **$0** |
| `google/gemini-3.1-pro`     | 32 | 36 | 40 | 52.6s  | 6 599 | 0.0343 |
| `openai/gpt-5.4`            | 31 | 44 | 38 | 115.1s | 5 334 | 0.0546 |
| `anthropic/claude-opus-4.7` | 31 | 37 | **32** | 59.8s  | 5 163 | 0.4110 |

`anthropic/claude-opus-4.7` finishes **last** on pharma at the highest cost; `openrouter/elephant-alpha` finishes **third** at zero cost and ~3× faster.

## Headline finding — vertical-specific rank inversion

| Model | FMCG rank | Banking rank | Pharma rank |
|---|:-:|:-:|:-:|
| `anthropic/claude-opus-4.7` | **1** | 2 | **6 (last)** |
| `openai/gpt-5.4`            | 2 | **1** | 5 |
| `z-ai/glm-5.1`              | 2 | 4 | 2 |
| `google/gemini-3.1-pro`     | 4 | 5 | 3 |
| `openrouter/elephant-alpha` | 5 | **6 (last)** | **3** |
| `moonshotai/kimi-k2.5`      | **6 (last)** | 3 | **1** |

Three models (Opus, Kimi, Elephant) move from first to last (or near-last) and back across these three verticals. There is no single "best frontier model" across business-relevant verticals.

## Coverage ceiling — union of 5 paid frontier responses

| Pack | rubric tier3 | union (5 paid) | union % | elephant alone | elephant % |
|---|---:|---:|---:|---:|---:|
| `fmcg_consumer_goods` | 87 | 34 | 39% | 17 | 20% |
| `banking_basel_2026` | 88 | 28 | 32% | 8 | 9% |
| `pharma_commercial_2026` | 102 | 49 | **48%** | 40 | **39%** |

Pharma is the most "hot in news" vertical and produces the highest rubric coverage in every model. Elephant alone covers 39% on pharma — within 9 percentage points of the union of five paid frontiers (48%).

## Cost ledger

- Total batch cost: **$1.583** across 18 OK calls (3 packs × 6 models).
- `anthropic/claude-opus-4.7` consumed **78% of the spend** ($1.237 of $1.583), did not lead any pack, and finished **last** on pharma.
- Per-pack cost was remarkably stable: $0.515-$0.546.

### Cost per tier-3 marker — pharma (where the inversion is sharpest)

| Model | USD | tier-3 hits | USD / marker |
|---|---:|---:|---:|
| `openrouter/elephant-alpha` | 0      | 40 | $0       |
| `z-ai/glm-5.1`              | 0.0098 | 41 | $0.00024 |
| `moonshotai/kimi-k2.5`      | 0.0132 | 43 | $0.00031 |
| `google/gemini-3.1-pro`     | 0.0343 | 40 | $0.00086 |
| `openai/gpt-5.4`            | 0.0546 | 38 | $0.00144 |
| `anthropic/claude-opus-4.7` | 0.4110 | 32 | **$0.01284** |

`anthropic/claude-opus-4.7` cost **53×** as much per tier-3 marker as `z-ai/glm-5.1` on pharma — and was last in absolute depth.

## Per-model unique tier-3 hits (only this model caught it on a pack)

- `openrouter/elephant-alpha` — **0 unique markers across all 3 packs**. Everything elephant catches is also caught by some paid model. Elephant complements the floor; it does not extend the ceiling.
- `anthropic/claude-opus-4.7` — uniquely caught: `liberation day`, `reciprocal tariff`, `reuse target`, `article 7` (FMCG); `csrd scope`, `vice chair barr` (banking); `walgreens take-private` (pharma).
- `openai/gpt-5.4` — uniquely caught: `dollar general` (FMCG); `14th sanctions package`, `50% to 72.5%`, `pra delay` (banking); `obesity market` (pharma).
- `z-ai/glm-5.1` — uniquely caught: `srep 2026`, `texas top cop` (banking).
- `moonshotai/kimi-k2.5` — uniquely caught: `$54 billion` retail-media (FMCG); `june 2025 amla` (banking); `$100 billion` obesity TAM (pharma).
- `google/gemini-3.1-pro-preview` — uniquely caught: `2030 recyclability` (FMCG); `2024 ccar`, `michael barr` (banking).

## Notable per-model behaviors observed in the responses

- **Opus 4.7 self-corrected the prompt** on pharma: *"the user's prompt states 15/20/20 for 2027/2028/2029 — the statute is 15/15/20. Worth correcting in the CEO memo."* It is the only model to flag the prompt's factual error rather than silently propagate it.
- **Opus 4.7 leads FMCG** with high-precision regulatory specifics: *"Regulation (EU) 2025/40 entered into force 11 February 2025 and applies from 12 August 2026"*; complete Article 7 PCR percentages by material.
- **GPT-5.4 leads banking** with deep SA-OR mechanics, BI components by name, and US LTD / resolution-doctrine commentary.
- **Kimi K2.5 leads pharma** with quantified MFP discount range (38-79%), 22% average, $6B first-year Medicare savings.
- **Elephant on banking left honest empty slots**: *"Commission Implementing Regulation (EU) 2023/xxxx laid out the technical standards"* — model declined to invent a regulation number and used a placeholder instead. Compared with frontier models on this same prompt, it is materially better than confident fabrication.
- **Elephant on pharma punched up** with full Selected Drug list, correct IPAD date, but mis-estimated MFP discount as "40-50%" (truth: 22% average, 38-79% range). Wide breadth, lower precision.

## Reproduction

```bash
MODELS=(openrouter/elephant-alpha anthropic/claude-opus-4.7 \
        openai/gpt-5.4 google/gemini-3.1-pro-preview \
        z-ai/glm-5.1 moonshotai/kimi-k2.5)
for pack in fmcg_consumer_goods banking_basel_2026 pharma_commercial_2026; do
  agents/vertex/.venv-agentloom/bin/python -u \
    agents/vertex/agenthorde/scripts/bench_industrial_depth.py \
    --pack "$pack" \
    --models "${MODELS[@]}" \
    --max-tokens 8192 --temperature 0.2 --timeout 600 \
    --jsonl-out "bench/results/industrial-depth-three-verticals-2026-04/${pack}_frontier6_8k.jsonl"
done
```

Total wall time: ~30 minutes. Slowest single call: `kimi-k2.5 · fmcg_consumer_goods` at 464s. Fastest: `elephant-alpha · fmcg_consumer_goods` at 9.3s.

Raw JSONL alongside this SUMMARY.md.
