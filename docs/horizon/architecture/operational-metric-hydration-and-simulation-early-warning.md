# Operational Metric Hydration and Simulation Early Warning

## Summary

This document fixes the core architectural direction for dynamic operational metrics in Horizon.

The graph is not only a topology store. It is the operational memory that feeds:
- live risk interpretation,
- simulation,
- future-facing early warning,
- and graph enrichment for the next reasoning layer.

The key rule is:

`Simulation hydration is a client of the metric core, not a separate metric system.`

This means:
- metric acquisition from sources,
- watch-driven refresh,
- propagation-driven refresh,
- on-demand enrichment,
- and pre-simulation hydration

must all use the same metric model, the same freshness logic, the same scope-resolution logic, and the same source-routing logic.

## Why This Exists

Supply-chain reality changes continuously:
- chokepoints open and close,
- customs scrutiny changes,
- insurance and settlement routes degrade,
- conflict and sanctions conditions shift,
- product-safety enforcement spikes,
- counterfeit pressure changes,
- and route economics move faster than static graph structure.

If the graph only stores structural paths, the simulation will inevitably become stale.

Therefore Horizon needs a shared `operational world state` layer on top of topology.

## Architectural Commitments

### 1. Unified Operational Metric Core

There must be one shared runtime for operational metrics.

It owns:
- canonical metric families,
- freshness / TTL rules,
- confidence rules,
- provenance,
- source routing,
- write-back to graph state,
- and scope hydration logic.

It must be reused by:
- WatchSubject,
- source-driven enrichment,
- propagation,
- on-demand metric refresh,
- and simulation preflight.

We do not allow a separate simulation-only metric subsystem.

### 2. Simulation Uses the Same Hydration Logic

Before a simulation run starts, the runtime must:
1. resolve the run scope,
2. determine which operational metrics are required,
3. check freshness and confidence,
4. refresh only the stale or missing parts through the shared metric core,
5. write refreshed values back into the graph and path layers,
6. then build the simulation baseline.

Simulation does not invent a parallel truth.

### 3. Metric Model Must Be Rich Enough for Operations

Operational state should not be modeled as booleans when a graded metric is more truthful.

Preferred fields:
- `value`
- `confidence`
- `observedAt`
- `freshUntil`
- `provenance`
- `sourceKind`

Preferred target scopes:
- node / fragment,
- edge / link,
- path,
- leg.

### 4. Materialized Paths Are a First-Class Consumer

Dynamic operational metrics must be written not only to fragment state, but also into materialized path and leg payloads when relevant.

This is required so that:
- the simulation can reason directly over execution paths,
- the frontend can render live route degradation,
- the pathbuilder can expose bottlenecks,
- and agents can act on execution constraints instead of generic graph structure.

### 5. Simulation Has Two Jobs

Simulation is not only a lens-specific stress test.

It serves two purposes:

1. Lens risk interpretation
- explain what is already risky inside the current lens,
- prioritize actions,
- surface role-specific implications.

2. Future-facing graph enrichment
- detect emerging constraints,
- expose where graph/path coverage is missing,
- create inferred or candidate execution contours when justified,
- and prepare the graph for the next reasoning layer.

This is part of the future early-warning system.

### 6. Cross-Lens Early Warning Is a Target State

Simulation output must eventually be able to contribute to:
- future risk identification across lenses,
- recurring chokepoint and corridor pressure signals,
- recurring product-risk regimes,
- and cross-lens recommendations.

This does not mean simulation replaces the graph.

It means simulation becomes one of the structured producers of future-facing risk knowledge that can be rolled up above individual lenses.

## Current State

The system already moved in this direction:
- unified metric freshness and hydration logic exists in the core runtime,
- simulation preflight already consumes the same metric hydration logic,
- path and leg payloads already accept operational metrics,
- and AgentMesh reads operational seeds rather than running a separate metric pipeline.

However, this work is not finished.

The remaining problem is no longer “whether simulation can use live metrics”.
The remaining problem is how fully and consistently that live operational layer is:
- populated,
- written back,
- reflected in overlays,
- interpreted by agents,
- and rolled up into future-facing risk outputs.

## Canonical Metric Families To Support

The exact list will grow, but the architecture should assume families like:

### Logistics and Chokepoints
- `blockage_score`
- `transit_delay_hours`
- `reroute_cost_multiplier`
- `route_reliability_score`
- `rerouteability_score`

### Customs and Compliance
- `screening_intensity_score`
- `customs_friction_score`
- `license_burden_score`
- `inspection_probability`

### Finance and Settlement
- `insurance_cover_availability`
- `settlement_reliability_score`
- `trade_credit_friction_score`

### Product and Safety
- `product_safety_enforcement_score`
- `counterfeit_seizure_pressure_score`
- `recall_exposure_score`
- `consumer_trust_score`
- `informal_channel_leakage_score`

## Scope and Hydration Rules

Hydration must be scope-aware.

The runtime should determine targets from:
- recognized axis,
- lens scope,
- materialized paths,
- relevant chokepoints, hubs, and gates,
- relevant products,
- and scenario mode.

Hydration requests must be minimal:
- do not refresh the whole world,
- refresh only stale or missing metrics relevant to the current scope.

## LLM Contract Rule

When LLM grounding is used to fill operational metrics:
- the output must be structured,
- the prompt must specify the exact response schema,
- and output token budgets must be large enough to avoid truncation.

The LLM is not asked for prose.
It is asked for metric extraction with:
- fields,
- confidence,
- timing,
- provenance,
- and reasons.

This rule matches the rest of the Horizon runtime and must stay consistent with other metric-fill paths.

## Inferred Versus Validated Execution

When the graph lacks a validated execution family but the runtime can justify a candidate from indirect evidence:
- it may create an `inferred` path family,
- but it must remain distinct from validated paths,
- must not be built from inference-on-inference chains,
- and must carry lower confidence and distinct UX rendering.

This is not a fallback for sloppy graph construction.
It is a controlled bridge between current evidence and operational reasoning.

## Implementation Backlog

### A. Core Metric Runtime

- Expand canonical operational metric families.
- Keep TTL / freshness / confidence in the shared metric model.
- Keep source routing shared across watch, propagation, sources, and on-demand enrichment.
- Keep write-back shared across fragment, edge, path, and leg layers.

### B. Path / Leg Enrichment

- Ensure all relevant operational metrics can be written into materialized paths and legs.
- Preserve freshness and provenance at path level.
- Surface bottlenecks and dominant reasons deterministically.

### C. Simulation Preflight

- Keep pre-simulation hydration as a thin client of the shared metric core.
- Require explicit metric families by simulation axis and scope.
- Fail soft when some metrics are missing, but report coverage gaps clearly.

### D. Agent-Ready Operational Snapshot

- Convert hydrated metrics into an agent-friendly operational snapshot.
- Agents should reason over:
  - blocked corridors,
  - screening burden,
  - safety enforcement,
  - reroute cost,
  - inventory pressure,
  - low-confidence inferred routes.

### E. Human-Facing Management Slice

- Distinguish full graph presence from top-ranked management overlays.
- Show validated versus inferred routes clearly.
- Surface freshness, confidence, and dominant operational reasons.
- Ensure the final readout explains what to watch and why.

### F. Cross-Lens Early Warning

- Add a future layer that aggregates simulation-enriched findings across lenses.
- Use that layer for global recommendations and future risk detection.
- Treat simulation not just as a one-off lens tool, but as a contributor to recurring risk memory.

## Non-Negotiable Rules

1. No separate simulation-only metric logic.
2. No duplicated freshness or source-routing rules outside the metric core.
3. No silent mixing of validated and inferred execution.
4. No prose-only LLM outputs where structured metric extraction is required.
5. No graph-for-graph’s-sake implementation: the graph exists to support action, interpretation, and machine reasoning.
