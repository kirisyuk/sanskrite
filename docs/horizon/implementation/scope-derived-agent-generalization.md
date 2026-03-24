# Scope-Derived Agent Generalization

- generatedAt: `2026-03-24`
- runtime revision: `etigram-agentmesh-00061-8jx`

## Goal

Validate that `regional_supply_shoulder_*` and `product_risk_manager_*` are now produced by the **core simulation runtime** from recognized scope and graph facts, rather than only by scenario-specific overlays.

## Core Changes

- `scope_derived.go` adds graph-derived shoulders and product-risk agents from `BaselineRefs.ScopePaths`.
- `processor.go` appends scope-derived agents to the runtime population for relevant logistics / supply / compliance axes.
- `discipline_layer.go` appends scope-derived monitoring signals for:
  - `shoulder_load_<country>`
  - `product_availability_<slug>`

## Validation Runs

### SLB Russia

- `lensId`: `l8dOd17uKYYTKD0CI9hy`
- `projectId`: `DRGeH8tCC0EIoRpLlv3D`
- `runId`: `Vlvdt2Co2ekQkSD0bUEA`

Observed scope-derived monitoring:
- `shoulder_load_cn`
- `shoulder_load_ae`
- `shoulder_load_in`
- `shoulder_load_kz`
- `shoulder_load_tr`
- `product_availability_telemetry-tools`
- `product_availability_well-completion-systems`
- `product_availability_specialty-drilling-chemicals`
- `product_availability_centrifugal-pumps`
- `product_availability_hydraulic-pumps`

Observed scope-derived actors in runtime output:
- `regional_supply_shoulder_cn`
- `regional_supply_shoulder_in`
- `regional_supply_shoulder_kz`
- `product_risk_manager_centrifugal-pumps`
- `product_risk_manager_hydraulic-pumps`
- `product_risk_manager_specialty-drilling-chemicals`
- `product_risk_manager_telemetry-tools`

Interpretation:
- The Russia-linked SLB chain is no longer read only as `Jebel Ali -> Tyumen`.
- The runtime now recognizes explicit external shoulders and explicit product families as stress-bearing objects in the same run.

### Qatar LNG to Europe

- `lensId`: `Pa5qjiSQDeZyKqCnrlEb`
- `projectId`: `xd0EAoHYvU6lAzu5l5cC`
- `runId`: `kKIwMm8zYw6Nto0k6bs8`

Observed scope-derived monitoring:
- `shoulder_load_be`
- `shoulder_load_nl`
- `shoulder_load_sea`
- `shoulder_load_eg`
- `product_availability_ammonia`
- `product_availability_naphtha`
- `product_availability_condensate-export-services`
- `product_availability_field-condensate`
- `product_availability_liquefied-natural-gas-lng`

Observed scope-derived actors in runtime output:
- `regional_supply_shoulder_nl`
- `regional_supply_shoulder_be`
- `regional_supply_shoulder_eg`
- `regional_supply_shoulder_sea`
- `product_risk_manager_ammonia`
- `product_risk_manager_condensate-export-services`
- `product_risk_manager_naphtha`
- `product_risk_manager_liquefied-natural-gas-lng`

Interpretation:
- The runtime generalized beyond SLB.
- On a separate LNG lens it automatically read receiving-side and chokepoint shoulders from the graph and converted product paths into product-specific risk desks.

## What This Proves

- Scope-derived shoulders and product-risk agents are now part of the core runtime path.
- They are no longer dependent on one scenario-specific fallback.
- The runtime can now explain a supply-chain shock in three layers:
  - route / corridor stress
  - regional shoulder stress
  - product-family stress

## Remaining Gaps

- Scope-derived actors do not yet dominate every readout; high-volume scenario actors can still overshadow them in the top reactions list.
- Product naming is still purely path-derived; some labels need prettier rendering.
- Gate / control actors remain weaker than physical-flow actors and should be generalized next.
