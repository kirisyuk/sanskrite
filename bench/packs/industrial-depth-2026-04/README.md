# Industrial Depth Probes — Free Tier · 2026-04

Open-ended industrial knowledge probes for benchmarking LLMs on **vertical
depth** rather than schema compliance, latency, or judgment calibration.

This is the second batch in the Insights portal lineage; the first batch
(`bench/results/critic-free-tier-2026-04/`) measured strict-JSON contract
fidelity on a single semiconductor pack. This batch measures something
orthogonal: **how deep does the model actually go on a real industrial
question, in prose, with no schema and no role contract**.

## Verticals (Tier 1 of the Insights vision)

1. `battery_critical_minerals/` — NMC vs LFP cathode supply chain under IRA / EU CRMA, lithium hydroxide tiers, HPAL Ni from Indonesia, DRC cobalt routes, NdPr / REE downstream of Bayan Obo, Chinese export licensing on Ga/Ge/graphite/W.
2. `pharma_biologics/` — BIOSECURE Act consequences, mAb CDMO landscape outside the named-entity list, mRNA ionizable lipid IP and GMP supply, plasma fractionation, GLP-1 API capacity (semaglutide / tirzepatide), compounding pharmacy regulation.
3. `nuclear_energy/` — Full uranium fuel cycle (U3O8 → UF6 → enriched UF6 → UO2 → assembly), HALEU producers, Westinghouse VVER fuel programs, heavy-duty gas turbine slot backlog (H/J class), LNG carrier shipyards and GTT membrane technology.
4. `aerospace_titanium/` — Aerospace-grade titanium sponge after Russia disruption, CFM LEAP vs P&W GTF (powder metal contamination), large structural castings/forgings (PCC/Howmet/ATI), engine architecture monolithicity, COMAC C919 foreign-supplier dependence.

## Layout per vertical

```
<vertical>/
  prompt.md       # exact text sent to the model (English, open-ended prose)
  markers.yaml    # tier-1 / tier-2 / tier-3 knowledge markers for auto-scoring
                  # (the model never sees this file)
```

## What we measure

For each `(model × pack)` cell:

- **Latency** (ms wall-clock for the full response).
- **Tokens** (input / output, from OpenRouter `usage`).
- **Marker hits per tier** — case-insensitive substring or regex match
  against `markers.yaml`. Counts per tier.
- **Raw response text** — preserved in JSONL for human spot-check and
  hallucination audit.

We deliberately do **not** auto-judge correctness with another LLM in v0.
Marker-based scoring is conservative and reproducible; the qualitative
read of the top responses is human.

## What this can and cannot prove

Can prove:
- Recall depth differences between models (tier-3 marker hit rate).
- Recency awareness (markers tied to 2023–2026 events).
- Hallucination patterns when cross-checked against the named entity list.

Cannot prove:
- Reasoning quality beyond fact recall.
- Which model would best fit a specific role in a multi-agent system
  (that requires the role-fit benchmark, see `insights-portal-vision.md`).
- Anything statistically significant from a single pack — we report
  marker hit counts as observations, not as a leaderboard.
