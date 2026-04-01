# Product Transform Pairs Result

Date: 2026-03-22
Project: `nv-horizon`
Tenant: `global`
Collection: `horizon_product_transform_pairs`
Tool: [main.go](/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_mine_product_transforms/main.go)

## Goal

Build a Firestore-backed dictionary of canonical product transformation pairs `from -> to` from real graph evidence, without mutating the graph itself.

These pairs are intended to be used later as canonical processing steps for:
- supply-chain chain stitching
- materialized path extension
- downstream canonical transformation graph building

The accepted quality bar is intentionally strict:
- only product/material transformations
- not synonyms
- not service/logistics/compliance artifacts
- not class/subclass or broad/narrow taxonomy relations
- not long multi-step jumps that skip obvious intermediates

Important nuance:
- nearest direct assembly into a final product is allowed when there is no useful missing intermediate in the data
- examples now considered valid:
  - `tire -> vehicle`
  - `battery pack -> electric vehicle`
  - `display module -> smartphone`
- examples still rejected:
  - `steel sheet -> vehicle`
  - `battery material -> vehicle`
  - `iron ore -> hot-rolled steel`

## Data Sources Used

The tool scans the live Firestore graph and combines three evidence sources:

1. direct `TRANSFORMS_INTO` edges
2. actor I/O inference from:
   - `SUPPLY_LINK` inbound products to an actor
   - `SUPPLY_LINK` outbound products from the same actor
   - `PRODUCES` edges from the same actor
3. `SUPPLY_CHAIN_ASSEMBLY_GRAPH` transform / assembly edges from `horizon_materialized_artifacts`

Before mining, products are normalized through the already persisted synonym dictionary from `horizon_product_synonym_pairs`, so transform pairs are attached to canonical product refs rather than alias tails.

## Live Graph Size Seen By The Tool

For the current production-like test graph:

- products loaded: `4891`
- active synonym pairs loaded: `424`
- `TRANSFORMS_INTO` edges loaded: `2791`
- `PRODUCES` edges loaded: `12313`
- `SUPPLY_LINK` fragments loaded: `18461`
- `SUPPLY_CHAIN_ASSEMBLY_GRAPH` artifacts loaded: `130`

## Review Passes Applied

### Pass 1

Top ranked candidate window:
- `candidate-offset=0`
- `candidate-limit=300`

Result after LLM review plus deterministic curation:
- accepted pairs: `145`
- rejected pairs: `155`
- persisted pairs: `145`

Artifacts:
- raw/curated report: [/tmp/product_transform_pairs_curated_003.json](/tmp/product_transform_pairs_curated_003.json)
- persisted report: [/tmp/product_transform_pairs_curated_003_persisted.json](/tmp/product_transform_pairs_curated_003_persisted.json)

Representative accepted pairs:
- `bauxite -> alumina`
- `pcam -> cathode-active-material`
- `ev-battery-module -> ev-battery-pack`
- `crude-helium -> helium`
- `spodumene-concentrate -> lithium-carbonate`
- `crude-oil -> diesel-fuel`
- `smartphone-chassis -> smartphone`

### Pass 2

Next ranked candidate window:
- `candidate-offset=300`
- `candidate-limit=300`

Result after LLM review plus deterministic curation:
- accepted pairs: `92`
- rejected pairs: `208`
- persisted pairs: `92`

Artifacts:
- curated report: [/tmp/product_transform_pairs_curated_004_offset300_v2.json](/tmp/product_transform_pairs_curated_004_offset300_v2.json)
- persisted report: [/tmp/product_transform_pairs_curated_004_offset300_v2_persisted.json](/tmp/product_transform_pairs_curated_004_offset300_v2_persisted.json)

Representative accepted pairs:
- `crude-oil -> naphtha`
- `uf6 -> enriched-uranium`
- `lithium-ore -> spodumene-concentrate`
- `trichlorosilane -> electronic-grade-polysilicon`
- `pig-iron -> crude-steel`
- `steel-slab -> hot-rolled-steel-coil`
- `microcontroller -> engine-control-unit`

### Pass 3

Next ranked candidate window:
- `candidate-offset=600`
- `candidate-limit=300`

Result after LLM review plus deterministic curation:
- accepted pairs: `49`
- rejected pairs: `251`
- persisted pairs: `49`

Artifacts:
- curated report: [/tmp/product_transform_pairs_curated_005_offset600_v3.json](/tmp/product_transform_pairs_curated_005_offset600_v3.json)
- persisted report: [/tmp/product_transform_pairs_curated_005_offset600_v3_persisted.json](/tmp/product_transform_pairs_curated_005_offset600_v3_persisted.json)

Representative accepted pairs:
- `lithium-brine -> lithium-carbonate`
- `crude-oil -> jet-fuel`
- `automotive-steel-coil -> stamped-body-panel`
- `crude-steel -> steel-slab`
- `kunlun-glass-display-module -> huawei-mate-60-pro`
- `semiconductor-wafer -> automotive-chipset`
- `battery-electrode-sheets -> ev-battery-cells`

### Pass 4

Next ranked candidate window:
- `candidate-offset=900`
- `candidate-limit=300`

Result after LLM review, deterministic curation, and manual tail cleanup:
- accepted pairs after curation: `119`
- rejected pairs after curation: `181`
- persisted pairs: `119`

Artifacts:
- raw report: [/tmp/product_transform_pairs_curated_006_offset900.json](/tmp/product_transform_pairs_curated_006_offset900.json)
- deterministic rerun: [/tmp/product_transform_pairs_curated_006_offset900_v4.json](/tmp/product_transform_pairs_curated_006_offset900_v4.json)
- final curated report: [/tmp/product_transform_pairs_curated_006_offset900_v5_curated.json](/tmp/product_transform_pairs_curated_006_offset900_v5_curated.json)
- persisted report: [/tmp/product_transform_pairs_curated_006_offset900_v5_curated_persisted.json](/tmp/product_transform_pairs_curated_006_offset900_v5_curated_persisted.json)

Representative accepted pairs:
- `naphtha -> ethylene`
- `cathode-active-material -> lithium-ion-battery-cell`
- `battery-cell -> battery-pack`
- `pcba -> battery-management-system`
- `display-module -> smartphone-mate-series`
- `spent-ev-battery -> black-mass`

## Persisted Result

Persisted writes across the reviewed windows:
- pass 1: `145`
- pass 2: `92`
- pass 3: `49`
- pass 4: `119`
- total persisted writes: `405`

These docs were written as canonical sample pairs into:
- `horizon_product_transform_pairs`

Each doc stores:
- canonical `fromFragmentId` / `fromPath` / `fromName`
- canonical `toFragmentId` / `toPath` / `toName`
- `relationKind`:
  - `transformation`
  - `assembly`
- confidence
- review reason
- heuristic score
- evidence payloads:
  - direct edge refs
  - actor evidence
  - assembly evidence

## Deterministic Curation Layer

After the LLM review, the tool applies an extra deterministic rejection layer to remove pairs that are structurally harmful for a canonical transformation graph.

Current curated rejects include:
- service-like pseudo-products
- logistics / abstract process artifacts
- energy-generation outputs like `... -> electricity`
- battery-material long jumps like `cathode-active-material -> battery-pack`
- cell-to-final-vehicle jumps like `battery-cell -> electric-vehicle`
- broad material-to-final-product jumps like `steel-sheet -> vehicle`

But the curation layer now explicitly allows nearest direct final assembly steps when the source is already a concrete component/module/pack and there is no useful missing intermediate product in the graph.

This curation is implemented directly in:
- [main.go](/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_mine_product_transforms/main.go)

## Important Notes

1. Canonical path is the authoritative identity.
Some product display names in the graph are broader or less precise than the canonical path slug. Example: a path can clearly be `/product/.../smartphone` while the stored label is `Consumer Electronics`. The persisted pair still uses the canonical path, so downstream logic should trust the path first.

2. The table is started, not exhausted.
The first four ranked windows (`0..299`, `300..599`, `600..899`, and `900..1199`) are already reviewed and persisted. The miner is designed to continue with higher offsets.

3. No graph mutation was performed.
Only the dictionary collection was written. Existing fragments, edges, materialized paths, supply chains, legs, and overlays were not modified in this task.

## Next Continuation Point

Continue mining with:
- `--candidate-offset 1200`
- `--candidate-limit 300`

Recommended workflow:
1. mine/report
2. inspect/correct residual noise if needed
3. persist from `--report-in` without re-running LLM

Current continuation artifacts:
- raw next-window report: [/tmp/product_transform_pairs_curated_007_offset1200.json](/tmp/product_transform_pairs_curated_007_offset1200.json)
- deterministic rerun: [/tmp/product_transform_pairs_curated_007_offset1200_v2.json](/tmp/product_transform_pairs_curated_007_offset1200_v2.json)

`007` is not persisted yet; it still has a non-trivial long-tail of broad/process/service-like candidates and should be curated further before writing to Firestore.
