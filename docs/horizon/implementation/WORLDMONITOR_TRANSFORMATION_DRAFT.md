# HorizonMonitor -> Horizon: Draft Transformation Plan

Status: Draft v0.2 (implementation scaffolding added)  
Date: 2026-02-14  
Owner: Horizon/Core

## 1. Purpose

This document describes how to transform the temporary `worldmonitor` integration into the native `horizon` core model, without importing its UI architecture as-is.

Primary objective:
- Reuse HorizonMonitor's signal taxonomy, scoring ideas, and fusion logic.
- Keep `horizon` as the single source of truth for ontology, risk propagation, policy, and domain contracts.

Non-goal:
- Porting HorizonMonitor frontend panels/map UX into the main product in this phase.

## 2. Current State (As-Is)

HorizonMonitor contains three distinct layers:

1. Data ingestion + normalization:
- RSS/news, outages, protests, flights, vessels, climate, etc.
- Mixed deterministic and LLM classification.

2. Analytical layer:
- Threat classification (`level`, `category`).
- CII (Country Instability Index).
- Geo convergence.
- Temporal anomaly baseline (z-score).
- Infrastructure cascade BFS model.

3. Presentation layer:
- Variant-driven dashboards (`full`, `tech`), map overlays, panel ordering, local UX state.

Key conclusion:
- Layer (1) and (2) are transferable.
- Layer (3) should remain optional/adapter-level, not core.

## 3. Target State (To-Be in Horizon)

All external events become first-class Horizon `SignalDescriptor` and/or `OntologyFact(kind=event)` inputs, then pass through the existing risk pipeline.

Target flow:
1. External event arrives.
2. Normalization into Horizon event/signal contract.
3. Axis mapping (`12-axis basis`).
4. Policy + qualifier enrichment.
5. Risk pipeline stages (`IDENTIFY -> ESCALATE -> APPLY -> SEED -> PROPAGATE -> ROLLUP -> INTERPRET -> ALERT`).
6. Optional domain answer overlays for contract queries.

## 4. Taxonomy Mapping

### 4.1 Threat levels (HorizonMonitor -> Horizon)

| HorizonMonitor | Horizon constraint mapping |
|---|---|
| critical | `constraints.escalationLevel=critical` |
| high | `constraints.escalationLevel=warning` |
| medium | `constraints.escalationLevel=watch` |
| low/info | `constraints.escalationLevel=none` |

Notes:
- Keep original value in qualifiers for traceability.
- Horizon escalation remains authoritative for downstream policy decisions.

### 4.2 Event categories (HorizonMonitor -> Horizon axis/mechanism)

| Category | Primary Axis | Typical Mechanism |
|---|---|---|
| conflict / military / terrorism | `geo/conflict` | `armed_escalation`, `force_projection` |
| protest | `operational/continuity` + `geo/conflict` | `civil_unrest` |
| cyber | `operational/continuity` + `info/uncertainty` | `cyber_disruption` |
| infrastructure | `logistics/capacity` or `operational/continuity` | `infra_disruption` |
| disaster / environmental | `operational/continuity` | `natural_hazard` |
| diplomatic / economic | `legal/access`, `financial/settlement`, `info/uncertainty` | `sanctions`, `policy_shift`, `trade_barrier` |
| health | `operational/continuity` | `health_emergency` |
| tech / general | depends on context | `market_signal`, `news_context` |

### 4.3 HorizonMonitor signal types -> Horizon signal kinds

| HorizonMonitor signal type | Horizon representation |
|---|---|
| internet_outage | axis=`operational/continuity`, polarity=`risk` |
| military_flight | axis=`geo/conflict`, polarity=`risk` |
| military_vessel | axis=`geo/conflict`, polarity=`risk` |
| protest | axis=`geo/conflict` and/or `operational/continuity` |
| ais_disruption | axis=`logistics/capacity`, polarity=`risk` |
| satellite_fire | axis=`operational/continuity`, polarity=`risk` |
| temporal_anomaly | meta-signal in qualifiers + mapped base axis |

## 5. Qualifier Contract (Draft)

Use existing `Qualifiers` map on edges/fragments/signals, and standardize keys for imported HorizonMonitor semantics:

- `wm_source_tier` (int)
- `wm_source_type` (wire|gov|intel|...)
- `wm_propaganda_risk` (low|medium|high)
- `wm_threat_level` (critical|high|medium|low|info)
- `wm_threat_category` (conflict|cyber|...)
- `wm_classifier_source` (keyword|llm|ml)
- `wm_classifier_confidence` (0..1)
- `wm_geo_convergence_score` (0..100)
- `wm_temporal_zscore` (float)
- `wm_temporal_baseline_mean` (float)
- `wm_signal_types` (array)
- `wm_variant` (full|tech)

Rule:
- These keys are informational and backward-compatible.
- Final policy status still uses Horizon native policy qualifiers.

## 6. Scoring Principles to Adopt

Adopt:
- Hybrid deterministic-first + async AI refinement.
- Confidence-weighted aggregation by source quality.
- Multi-signal convergence priority boosts.
- Temporal baseline anomaly detection as an independent evidence stream.
- Stale-cache fallback and per-source circuit breaker behavior.

Do not adopt directly:
- Hardcoded geopolitics constants as final truth (country multipliers, static region boxes).
- UI-driven thresholds that are not tied to risk pipeline semantics.

## 7. Domain Contracts Alignment

HorizonMonitor analytical intents should map to existing `/domain/v1/...` contract types, not custom ad-hoc schemas.

Suggested mappings:
- CII / geopolitical posture -> `risk-assessment`, `geopolitical-risk`, `conflict-dynamics`
- Cross-country tensions -> `bilateral-relations` with composite geo facet
- Infrastructure cascade -> `supply-chain`, `logistics-flow`, `trade-flow`
- Sanctions and policy effects -> `sanctions-exposure`, `policy-and-regulation`

Facet usage:
- Reuse existing allowed facet keys (`geo`, `topic`, `asset`, `sector`, `market`, `time`, etc.).
- Keep deterministic canonical path normalization.

## 8. Migration Phases (No Code Yet)

### Phase 0: Contract freeze
- Freeze mapping tables (this document -> v1 contract).
- Approve qualifier keyset and axis mapping.
- Define acceptance tests per signal type.

### Phase 1: Ingestion adapter
- Build import adapter from HorizonMonitor event payloads into Horizon signal contract.
- Preserve provenance and confidence.
- Add observability counters for transformed vs dropped events.

### Phase 2: Risk pipeline integration
- Route transformed signals through existing risk stages.
- Ensure policy qualifiers and edge facets remain consistent.
- Validate no regressions in propagation and alerting.

### Phase 3: Domain answer integration
- Surface transformed signals in `DomainAnswerSnapshot` sections:
  - `CriticalDependencies`
  - `ControlExposure`
  - `ImpactPaths`
  - `DataGaps`
- Add explanatory-layer usage of imported evidence.

### Phase 4: Optional UI bridge
- Expose normalized outputs for cockpit/map if needed.
- Keep UI as read-model of Horizon, not a second analytical core.

## 9. Risks and Guardrails

### Main risks
- Dual truth systems (HorizonMonitor scores vs Horizon risk scores).
- Overfitting to hardcoded geopolitical constants.
- Qualifier drift (same meaning, different keys).
- LLM classification instability across providers.

### Guardrails
- Horizon score outputs are authoritative.
- HorizonMonitor-origin fields are namespaced (`wm_*`) and auditable.
- Runtime taxonomy stays in Firestore (`horizon_taxonomy/v1`) as control plane.
- Unknown relation/event types degrade safely to existing defaults.

## 10. Open Questions

1. Should CII remain an external imported metric or be reimplemented as Horizon-native derived metric spec?  
2. Which HorizonMonitor thresholds should become runtime policy (Firestore), and which remain adapter-local?  
3. Do we keep region-level heuristics (e.g., chokepoint dependency defaults) as temporary bootstrap data or move them into managed reference datasets?  
4. Should `temporal_anomaly` be a standalone signal kind in taxonomy, or only an evidence bucket attached to base signals?

## 11. Draft Acceptance Criteria

This draft is considered approved for implementation planning when:
- Axis/category mapping is agreed.
- Qualifier keyset is agreed.
- Phase boundaries and ownership are agreed.
- Open questions are assigned owners and decision dates.

## 12. Implemented Scaffolding (Current)

Implemented in repo (first working pass):

1. New source kind:
- `worldmonitor_feed` added to public source handling and ingestion paths.
- Supported in:
  - poller scheduling and worker ingest switch,
  - `SourceExecutor`,
  - HTTP source creation/ingest endpoint.

2. Polling behavior:
- Default cadence for `worldmonitor_feed` is fixed to `30m`.
- Optional per-source override via `sourceDetails.pollIntervalMinutes`.

3. HorizonMonitor feed ingest:
- `Orchestrator.IngestHorizonMonitorFeed(...)` added.
- Fetches feed URL from `sourceDetails.uri`, parses JSON/text, normalizes into documents, chunks/embeds, and updates poll state.
- Writes ingest history as kind `worldmonitor_feed`.

4. Exporter from worldmonitor repo:
- `temp-repos/worldmonitor/scripts/export-horizon-data.ts`
- Exports static datasets (`geo`, `pipelines`, `ports`) and parsed `INTEL_SOURCES` to JSON.
- npm script: `npm run export:horizon`

5. Horizon importer CLI:
- `backend/cmd/tools/horizon_import_worldmonitor/main.go`
- Imports static objects into Horizon fragments (facility/market/deposit/event/org/geo where applicable).
- Creates mandatory geo links (`LOCATED_IN`) for georeferenced entities.
- Optionally imports HorizonMonitor intel feeds as `core_datasources` (`rss` or `worldmonitor_feed`) and links to `rag_source_projects`.

### Runbook

1. Export from worldmonitor:
```bash
cd /Users/kirisyuk/cursor/sanskrite/temp-repos/worldmonitor
npm run export:horizon
```

2. Dry-run import:
```bash
cd /Users/kirisyuk/cursor/sanskrite/backend
go run ./cmd/tools/horizon_import_worldmonitor \
  -project-id <GCP_PROJECT_ID> \
  -input ../temp-repos/worldmonitor/data/horizon/worldmonitor-export.json
```

3. Apply import (+ sources + project links):
```bash
cd /Users/kirisyuk/cursor/sanskrite/backend
go run ./cmd/tools/horizon_import_worldmonitor \
  -project-id <GCP_PROJECT_ID> \
  -input ../temp-repos/worldmonitor/data/horizon/worldmonitor-export.json \
  -apply \
  -extend-taxonomy=true \
  -import-sources=true \
  -source-kind=worldmonitor_feed \
  -tenant-id <TENANT_ID> \
  -project-link-id <PROJECT_ID> \
  -owner-user-id worldmonitor-import
```

4. Apply import with immediate Vertex indexing:
- add `-rag-bucket <HORIZON_FRAGMENTS_BUCKET>`
- add `-vertex-sync=true`
- if env is not configured, pass:
  - `-vertex-store-id <VERTEX_SEARCH_HORIZON_DATA_STORE_ID>`
  - `-vertex-location <VERTEX_SEARCH_LOCATION>`

---

Appendix A: Relevant code anchors (for implementation planning)
- `backend/internal/horizon/models/signal_descriptor.go`
- `backend/internal/horizon/models/axis_spec.go`
- `backend/internal/horizon/models/dependency_edge.go`
- `backend/internal/horizon/models/policy_qualifiers.go`
- `backend/internal/horizon/domain/gate_config.go`
- `backend/internal/horizon/risk/pipeline.go`
- `backend/internal/taxonomy/paths.go`
- `backend/internal/taxonomy/runtime.go`
- `temp-repos/worldmonitor/src/services/threat-classifier.ts`
- `temp-repos/worldmonitor/src/services/signal-aggregator.ts`
- `temp-repos/worldmonitor/src/services/country-instability.ts`
- `temp-repos/worldmonitor/src/services/infrastructure-cascade.ts`

## 13. Implemented Pipeline (2026-02-14)

The following backend pipeline is now implemented and wired:

1. Deterministic worldmonitor pipeline wiring:
- `Orchestrator.WithHorizonMonitorPipeline(...)`.
- Server wiring in `backend/cmd/server/main.go` with shared dependencies:
  - `OntologyFactsRepository`,
  - `Signal SeedInjector`,
  - `PropagationEngine + ProfileLookup`,
  - `AITaskExecutor`.

2. Ingest mode behavior (`sourceDetails.worldmonitorIngestMode`):
- `ontology_fact`:
  - emits deterministic `OntologyFact(kind=event)`,
  - emits deterministic `OntologyFact(kind=signal)`,
  - runs seed injection and propagation (best-effort),
  - updates country/grid rollups and country briefs.
- `document_feed` / `query_only`:
  - keeps documents/chunks indexing path,
  - still supports news classification + rollup updates,
  - does not route through generic `document.process` LLM extraction.

3. Worldmonitor-specific deterministic transforms:
- Feed-type to signal mapping (e.g. `risk:protest`, `risk:ais_disruption`, `risk:conflict_event`, etc.).
- Signal to axis/mechanism mapping aligned with Horizon 12-axis basis.
- Severity/amplitude/confidence normalization.
- Country code + location-path normalization (`/loc/{cc}/na/na`).

4. Aggregation features ported from worldmonitor logic:
- 24h rolling window.
- Country convergence rollups:
  - distinct signal types,
  - total/high-severity counts,
  - convergence score formula.
- Geo grid cells (1x1 degree buckets):
  - per-cell signal counts,
  - distinct type counts,
  - convergence score.

5. Briefs and classification:
- News classification facts (`news_classification`) with deterministic keyword model and optional AI refinement.
- Country brief facts (`country_brief`) generated with strict JSON output via `AITaskExecutor.ExecuteStructuredTask(...)` + `GenericVerifier`.

6. Cost/LLM guardrail:
- For `worldmonitor_feed`, `document.process` publication is disabled (`worldMonitorShouldPublishDocumentProcess=false`) to avoid generic LLM extraction load.
- AI is used selectively for compact country briefs and optional low-confidence classification refinement.

7. Taxonomy extensions (implemented):
- Added signal types and aliases to include worldmonitor API coverage:
  - `risk:conflict_event`
  - `risk:aviation_disruption`
  - `risk:maritime_hazard`
  - `risk:climate_anomaly`
  - `risk:service_outage`

Notes:
- All imported worldmonitor sources in current environment are set to `paused`.
- Activation can be staged per source/profile after validation of ontology facts and propagation behavior.
