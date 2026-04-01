# Path-Centric Binding Architecture

## Goal

Make `SUPPLY_LINK` and `SUPPLY_CHAIN_PATH` operationally meaningful for long-range risk propagation.

The core graph remains fragment-grounded, but high-stakes reasoning should move from plain `fragment -> fragment` traversal to `fragment-grounded path -> route -> zone -> affected-zone -> endpoint` semantics.

## What Is Now In The Core

### 1. Enriched `SUPPLY_CHAIN_PATH` payload

`SUPPLY_CHAIN_PATH` now carries optional `legs` in addition to `nodes` and `edges`.

Each leg stores:
- `supplierRef`
- `customerRef`
- `productRef`
- `transportViaRefs`
- `transportViaNodeIndices`
- `mode`
- `geoRefs`
- `flowRefs`
- `alternateFlowRefs`
- `zoneRefs`
- `affectedZoneRefs`
- `spatialSemantics`
- `bindingConfidence`
- `segmentConfidence`

This makes the path itself a first-class carrier of route/zone semantics, instead of leaving them only in `NodeRefs` or UI overlays.

### 2. Shared binding engine

`internal/horizon/pathbinding` is the shared heuristic layer that binds a segment to:
- curated static flow ids
- curated static zone ids
- dynamic affected zone canonical paths
- geo refs
- route mode
- path semantics

This engine is reused both by:
- `pathbuilder` during supply-chain materialization
- `propagation` supply-link overlay during runtime virtual edge creation

This avoids having separate route logic in materialization and propagation.

### 3. Route nodes are now real payload nodes

`TransportViaRefs` are no longer only auxiliary refs.
They are materialized as path nodes and connected with explicit `LOGISTICS` edges.

This means the route survives materialization as structure, not only as metadata.

### 4. Lens artifact cache carries path bindings

`Lens.ScopeArtifacts` now caches:
- `geoRefs`
- `flowRefs`
- `zoneRefs`
- `affectedZoneRefs`
- `spatialSemantics`
- `legCount`
- `bindingConfidence`

So artifact scope can be consumed as path objects, not only as flattened `NodeRefs`.

### 5. Propagation sees spatial qualifiers

Virtual supply-link edges now carry:
- `route_mode`
- `geo_refs`
- `flow_refs`
- `alternate_flow_refs`
- `zone_refs`
- `affected_zone_refs`
- `spatial_semantics`
- `binding_confidence`

Propagation physics applies a modest but real multiplier for:
- logistics chokepoint zones
- energy hubs on supply axes
- affected-zone pressure on conflict/logistics/supply axes
- chokepoint semantics
- binding confidence

This is intentionally moderate: the goal is to make path semantics matter without destabilizing the kernel.

## Why This Is The Right Direction

The graph should not become a second GIS.
Static routes and zones can remain curated catalogs for now.

But they must stop being only visual overlays.

The correct architecture is:
- fragments = canonical substrate
- dependency edges = base causal fabric
- path artifacts = executable operational layer
- curated routes/zones = spatial constraint layer
- affected zones = dynamic pressure layer

## Current Limitations

This is still an MVP.

What it does not do yet:
- geometric intersection of routes with static polygons
- explicit path-family scoring from bound flow ids
- materialized `EXPOSURE_PATH` generation from enriched supply-chain legs
- alternate-route search using graph optimization
- path-level decay / stress accumulation state

Current zone and flow binding is heuristic, based on:
- canonical refs
- country hints
- path text
- mode inference
- existing `AFFECTS` edges for dynamic zones

That is acceptable for a first executable layer.

## Next Implementation Steps

### 1. Build `EXPOSURE_PATH` from enriched legs

For high-stakes axes, `EXPOSURE_PATH` should stop being only node/edge ids.
It should carry:
- objective fragment
- selected path id
- stressed legs
- route bindings
- zone bindings
- affected-zone bindings
- bottleneck semantics

### 2. Add structural path stress

Separate event signal from structural tension:
- route fragility
- zone pressure
- affected-zone pressure
- rerouteability
- substitutability
- recovery latency

This is the basis for long-range forecasts.

This layer is now implemented heuristically at MVP level:
- per-leg stress
- path-level bottleneck selection
- stress propagation qualifiers on runtime supply-link virtual edges
- lens metadata carrying path stress summary

### 3. Add geometric binding later

When backend has a proper shared catalog for route/zones geometry, replace heuristic country/text matching with:
- node-in-zone
- leg-crosses-zone
- route-overlaps-flow geometry

### 4. Interpret endpoints after path stress exists

Do not rush endpoint interpretation first.
The correct sequence is:
- clean graph
- rich paths
- path stress
- endpoint ranking
- interpretation

## Calibration And Live Testing

Use the diagnostic tool:

`go run ./cmd/tools/horizon_path_binding_diagnose --project=nv-horizon --tenant=global --limit=8000 --samples=3`

What it measures:
- `flow_bound`
- `zone_bound`
- `affected_bound`
- `binding_strong`
- `via_flow_bound`
- `via_zone_bound`
- average bound refs per link
- top bound flow ids
- top bound zone ids
- representative rich / missing samples

### Current heuristic target shape

The binding layer should be selective, not exhaustive.

Healthy behavior for the current MVP:
- `flow_bound` should stay materially below `zone_bound`
- `via_flow_bound` should be much higher than global `flow_bound`
- average `flowRefs` per bound link should stay close to `1`
- `alternate_route` should be rare
- generic links without route evidence should often remain unbound

### Last live probe

On March 8, 2026, the full `7771`-link probe produced:
- `flow_bound=480 (6.2%)`
- `zone_bound=4630 (59.6%)`
- `affected_bound=121 (1.6%)`
- `binding_strong=1101 (14.2%)`
- `via_flow_bound=335 (22.1% of via)`
- `avg_flow_refs=1.24`
- `avg_zone_refs=1.73`

This is materially better than the initial overbinding state where almost every link was receiving a flow binding.

The same live probe now also exposes structural stress:
- `stress_strong=9 (0.1%)`
- `avg_stress=0.28`
- `avg_recovery_hours=32.7`

The stressed tail sample is already sensible:
- Gulf / Hormuz / Malacca crude paths rise to the top
- chokepoint-exposed sea legs score higher than generic industrial legs

### Known remaining weaknesses

- static zone binding is still broad because zones are country-anchored and not geometry-anchored yet
- flow country hints in the curated catalog are noisy, so binding compensates by requiring route evidence and route-specific keywords
- placeholder refs such as `/fac/na/...` still suppress good bindings because they do not carry trustworthy geo
- dynamic `affectedZoneRefs` are still sparse because graph-side `AFFECTS` coverage is sparse
- live RSS end-to-end replay still shows that graph topology and duplicate placeholder ports/pipelines remain the main bottleneck after path stress is added
