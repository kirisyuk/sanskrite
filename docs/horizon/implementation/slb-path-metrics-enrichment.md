# SLB Path Metrics Enrichment

Date: 2026-03-24

## What changed

This cycle pushed `SUPPLY_CHAIN_PATH` metrics deeper into the generic simulation runtime.

Pipeline fixes:
- `SUPPLY_CHAIN_PATH` payloads now carry explicit metric summaries:
  - `bindingConfidence`
  - `pathStressScore`
  - `recoveryLatencyHours`
  - `legCount`
  - `bottleneckLegIndex`
- `MaterializedArtifact` index hydration now fills transient path metric fields from payload.
- simulation baseline preview now hydrates artifact-path metrics from materialized paths and computes:
  - `artifact_path_count`
  - `artifact_path_stress_avg`
  - `artifact_path_stress_max`
  - `artifact_binding_avg`
  - `artifact_recovery_latency_max`
  - `artifact_stressed_paths`
  - `artifact_product_risk_max`
  - `artifact_shoulder_risk_max`
- baseline preview now also exposes:
  - `artifactTopShoulders`
  - `artifactTopProducts`

AgentMesh fixes:
- world bootstrap now seeds market/supply/risk from artifact path stress and recovery metrics, not only from fragment counts.
- generic decision logic now explicitly reacts to:
  - `pathStressScore`
  - `bindingConfidence`
  - `recoveryLatencyHours`
  - `productRiskScore`
  - `shoulderLoadScore`
- `regional_supply_shoulder_*` and `product_risk_manager_*` now inherit severity from artifact-path metrics.

## Manual SLB lens fill

Lens:
- `l8dOd17uKYYTKD0CI9hy`

Manual enrichment tool:
- `go run ./backend/cmd/tools/horizon_backfill_lens_path_metrics --project prevalent-horizon-test --lens l8dOd17uKYYTKD0CI9hy --min-risk 0.6 --exclude-product-contains coal`

Result:
- `68` path artifacts were written into `lens.scopeArtifacts`
- each scope artifact now carries:
  - `LegCount`
  - `BindingConfidence`
  - `PathStressScore`
  - `BottleneckLegIndex`
  - `RecoveryLatencyHrs`

This was intentionally filtered to exclude obvious off-scope contamination such as `coal`.

## Deployed revisions

- `api-00195-6q6`
- `prevalent-horizon-test-agentmesh-00064-g4q`

## Live run

Tool:
- `go run ./backend/cmd/tools/horizon_run_lens_simulation --lens l8dOd17uKYYTKD0CI9hy --question "Secondary sanctions tighten around SLB-related equipment and chemical flows into Russia. How does the supply scheme adapt across routing, localization shoulders, product families, compliance and customer commitments?" --timeout 15m`

Run:
- `s28gH2nhezk5gDJkneZB`
- status: `SUCCEEDED`

Outcome distribution:
- `price_pressure = 0.977`
- `risk_score = 0.483`
- `narrative_volatility = 0.498`
- `supply_stress = 0.161`
- `policy_pressure = 0.094`

Key mechanisms:
- `allocate_capacity`
- `delay_purchase`
- `reprioritize_flow`
- `reroute_shipment`
- `reserve_inventory`

Protocol summary:
- `allocation_protocol`
- `routing_protocol`
- `procurement_protocol`
- `coordination_protocol`

Transaction summary:
- `capacity_allocation`
- `shipment_reroute`
- `inventory_reservation`
- `procurement_order`

New graph-derived shoulders visible in monitoring:
- `shoulder_load_cn`
- `shoulder_load_ae`
- `shoulder_load_in`
- `shoulder_load_kz`

New graph-derived product desks visible in actor reactions:
- `product_risk_manager_bulk-drilling-chemicals`
- `product_risk_manager_centrifugal-pumps`
- `product_risk_manager_drill-bits`
- `product_risk_manager_hydraulic-pumps`

## Interpretation

The result is no longer only “the route is stressed”.

The simulation now reads the `SLB` case at three levels:
- corridor pressure
- shoulder pressure by country/hub
- product-family pressure

In this run:
- China and UAE clearly act as active external shoulders
- India and Kazakhstan also stay visible as live support shoulders
- pumps, drill bits, telemetry-related equipment, chemicals and completion-adjacent families are no longer implicit; they show up as dedicated product-risk actors

This means the system now explains the case as:
- a routing and allocation problem
- supported by specific cross-border shoulders
- with differentiated product-level breakpoints

That is materially stronger than the previous “generic sanctions stress” readout.
