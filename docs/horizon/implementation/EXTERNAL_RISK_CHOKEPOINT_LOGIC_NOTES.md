# External Risk & Opportunity Intelligence — Design Notes

## 1. Business Outcome (Primary Value)

### 1.1 Goal

Proactive external-risk/opportunity intelligence for concrete legal entities:

- risks and opportunities must reach affected `/org`/legal-entity targets **before they internally recognize the change**.

### 1.2 User and decision context

Primary user: analyst evaluating external exposure of portfolio entities.

Decision scenarios:

1. **Pre-emptive mitigation**: "Hormuz escalation signals are rising; entities X, Y, Z are exposed via energy supply chains — review contingency now."
2. **Opportunity capture**: "New lithium deposit confirmed in region A; entities P, Q have production capacity — evaluate upstream integration."
3. **Regime-shift awareness**: "Sanctions package changes settlement rules for sector S; entities exposed via financial/settlement axis — compliance review required."

In each case, the system output must answer:

- **who** is impacted (entity list with paths),
- **why** (mechanism, transmission path, source evidence),
- **how severe** (impact score),
- **how likely** (likelihood score),
- **how soon** (time horizon),
- **what to monitor next** (escalation indicators).

### 1.3 Success criteria

Entity-level risk/opportunity states become decision-ready without manual causal-path reconstruction.

KPI set:

| # | KPI | Description |
|---|-----|-------------|
| 1 | `entity_lead_time` | Median/p75 time between first external evidence and risk readiness at target entity node |
| 2 | `targeting_precision_at_entity` | Share of entities marked impacted that were truly impacted |
| 3 | `missed_material_entity_rate` | Share of materially impacted entities not identified in time |
| 4 | `precision_recall_balance` | Calibrated balance between false positives and false negatives at entity level |
| 5 | `assessment_cycle_time` | Time from trigger to persisted explainable entity-level risk result |
| 6 | `explainability_acceptance_rate` | Share of entity-level outputs accepted by analysts without manual rework |
| 7 | `dependency_path_completion_rate` | Share of material risk states with at least one valid explainable transmission path |
| 8 | `mechanism_attribute_completeness` | Completeness of required transmission qualifiers on critical paths |
| 9 | `chokepoint_policy_activation_recall` | Share of true chokepoint signals where chokepoint policy was correctly activated |

Deferred KPI (dispatch phase):

- `decision_action_rate`: share of dispatched outputs that led to concrete action.

Design guardrail:

- if a feature improves model sophistication but does not improve entity lead-time, precision, or actionability — it is not a priority.

### 1.4 Current phase boundary

- Transport/delivery/dispatching channels are out of scope at this stage.
- Success criterion is correct and timely risk/opportunity convergence to concrete entity nodes.
- Downstream dispatch can be layered on top of stable node-level outputs.

### 1.5 Scope boundary (intentional)

This design targets external systemic consequences:

- **Included**: world events and their cascading effects on markets, societies, dependency chains.
- **Excluded**: direct internal telemetry/control responsibility of asset owners (e.g., dam vibration sensors).

---

## 2. Context: Hormuz as Reference Scenario

### 2.1 Problem statement

- Object: Strait of Hormuz (`/loc/ir/hormuz`) or equivalent chokepoint.
- Event claim: "Iran intends to close the strait."
- Reality: low historical closure probability, very high systemic impact if realized.

The system must:

1. Detect and score such signals correctly.
2. Distinguish rhetoric/noise from serious escalation.
3. Propagate risk only to relevant markets/entities/objects.
4. Produce entity-level proof-of-impact for each affected org.

### 2.2 Expected target behavior (Hormuz)

1. Event extracted to axis-aligned signals (`geo/conflict`, `logistics/capacity`, `physical_flow/supply`).
2. Escalation classifier distinguishes rhetoric vs serious buildup.
3. Applicability gate checks exposure paths to affected energy/shipping markets and dependent orgs.
4. Propagation occurs only through relevant market/geo/dependency paths.
5. Rollup shows: likelihood, impact, exposure scope, top contributors and paths.
6. Entity-level risk state becomes `dispatch-ready` only when appetite and escalation conditions are jointly met.
7. For each impacted entity, a `proof-of-impact` artifact is persisted with: source signals, transmission path(s), mechanism qualifiers, confidence and uncertainty trace.

### 2.3 Applicability beyond Hormuz

This architecture applies to all systemic external event classes (see Event Catalog, section 7):

- **Sanctions**: `legal/access` + `financial/settlement` axes, transmission via ownership/partnership/supply edges.
- **Climate events**: `operational/continuity` + `physical_flow/supply`, transmission via facility/geo/production edges.
- **Technology shifts**: `operational/continuity` + `legal/access`, transmission via production/market edges.

The Hormuz example is representative but not special — the same pipeline handles all classes.

---

## 3. Current System: What Works (Code-Backed Baseline)

### 3.1 Base taxonomy (stable, no changes required for risk layer)

The 12-axis basis is complete and validated (`models/axis_spec.go`):

| # | Axis | Description |
|---|------|-------------|
| 1 | `legal/access` | Legal permission to operate/transact |
| 2 | `legal/compliance` | Secondary exposure from sanctioned/regulated entities |
| 3 | `financial/settlement` | Payment rails, banking access, clearing, FX |
| 4 | `financial/credit` | Credit availability, counterparty risk |
| 5 | `physical_flow/supply` | Physical availability of goods/materials |
| 6 | `physical_flow/demand` | Demand changes (surge or collapse) |
| 7 | `logistics/capacity` | Transportation/infrastructure capacity |
| 8 | `logistics/latency` | Delays, lead time increases |
| 9 | `operational/continuity` | Ability to operate |
| 10 | `info/uncertainty` | Ambiguity of rules, timelines, exceptions |
| 11 | `info/reputation` | Public perception, ESG signals, brand impact |
| 12 | `geo/conflict` | Geopolitical instability, conflict zones |

Each axis has a default `AxisSpec` with damping, max hops, propagation mode, strong edge types, and axis transformations. No new axes are needed for the risk layer.

### 3.2 Signal contract (already extended for tail-risk)

`SignalDescriptor` (`models/signal_descriptor.go`) already carries all required fields:

| Field | Location | Status |
|-------|----------|--------|
| `axis`, `mechanism`, `amplitude`, `polarity` | `SignalDescriptor` | Stable |
| `actorPaths`, `seedPaths`, `locationPath`, `spherePaths` | `SignalDescriptor` | Stable |
| `confidence`, `sourceRefs`, `observedAt` | `SignalDescriptor` | Stable |
| `likelihood` (0..1) | `SignalConstraints` | **Exists, used in extraction** |
| `impact` (0..1) | `SignalConstraints` | **Exists, used in extraction** |
| `timeHorizon` (immediate\|short\_term\|medium\_term\|long\_term) | `SignalConstraints` | **Exists, validated** |
| `modality` (rhetoric\|potential\|projected\|escalation\|disruption\|confirmed\|actual) | `SignalConstraints` | **Exists, validated, ranked** |
| `escalationLevel` (none\|watch\|warning\|critical) | `SignalConstraints` | **Exists, validated** |
| `escalationConfidence` (0..1) | `SignalConstraints` | **Exists, validated** |
| `evidenceBuckets` (intent/capability/preparation/corroboration) | `SignalConstraints` | **Exists** |

No signal contract extension is needed. The model is ready.

### 3.3 Modality as single realization axis (already decided)

Modality is the single ordered dimension of "how real is this":

`rhetoric (0) → potential (1) → projected (2) → escalation (3) → disruption (4) → confirmed (5) → actual (6)`

`likelihood` and `impact` are orthogonal numeric dimensions. No `scenarioType` field — `modality` is the sole realization axis.

### 3.4 Base pipeline (Scout runtime)

Current chain in Scout runtime (`scout/signal_pipeline.go`):

`Event → SignalExtractor.Extract() → EscalationClassifier.Assess() → SeedInjector.Inject() → PropagationEngine.PropagateSignalFromSeeds() → fragmentChangedHook → Dispatcher → Interpreter`

Components and their wiring status:

| Component | Code | Wired in runtime | Notes |
|-----------|------|-------------------|-------|
| **SignalExtractor** | `signals/extractor.go` | Yes (signal\_pipeline, events\_inject) | Rule-based + LLM fallback |
| **EscalationClassifier** | `signals/escalation_classifier.go` | Yes | Called from `SignalPipeline.enrichSignalEscalation()` before persistence/propagation |
| **ExternalApplicabilityEvaluator** | `signals/applicability_evaluator.go` | Yes | Wired into `SeedInjector.WithApplicabilityEvaluator()` in Scout signal pipeline and events inject |
| **SeedInjector** | `signals/seed_injector.go` | Yes | Applicability evaluator optional |
| **PropagationEngine** | `propagation/engine.go` | Yes | Hot path + cold path |
| **ProfileLookup** | `propagation/profile_lookup.go` | Yes | `Initialize()` called at startup with fallback to defaults |
| **SignalPropagation** | `propagation/signal_propagation.go` | Yes | BFS with axis profiles |
| **InterpretationDispatcher** | `interpretation/dispatcher.go` | Yes | Triggered via `PropagationEngine.WithFragmentChangedHook()` |
| **InterpretationProcessor** | `interpretation/processor.go` | Yes | `Start()` called at server startup |
| **Interpreter** | `interpretation/interpreter.go` | Yes (background + on-demand) | `signalsRepo` wired via `interpretation/signal_repo.go` (`OntologySignalFactsRepo`) |
| **RollupService** | `signals/rollup.go` | **No** | Service uses direct fragment state |
| **FeedbackEmitter** | `feedback/feedback.go` | Yes | `attention_escalation` and `re_evaluate` are both handled in feedback router |
| **AppetiteResolver** | `risk/appetite_resolver.go` | Yes | Used in `GetFragmentRollup()` |
| **ConcentrationAnalyzer** | `risk/concentration.go` | Yes | Used in `GetConcentrationSnapshot()` |
| **Risk Pipeline** | `risk/pipeline.go` | Yes | Independent `RiskJob` stage runner (`Identify→Escalate→Applicability→Seed→Propagate→Rollup→Interpret→Alert`) |
| **Risk Cloud Tasks** | `tasks/risk_cloudtasks.go` + `main.go` | Yes | Dedicated queue/worker endpoint: `/api/v1/internal/tasks/risk/run` |
| **Risk HTTP control plane** | `http/horizon/risk_jobs.go` | Yes | Manual on-demand `POST/GET /api/horizon/risk/jobs` |

### 3.5 Other stable infrastructure

Already working and should be preserved:

- Strict relation normalization and direction enforcement (`engine_ext`, `edge_materializer`, `edge_normalizer`).
- Scout/Research/Critic prompt discipline around structural/causal relations.
- Automatic hierarchy builders for geo/market/product scaffolding.
- Edge-level `Qualifiers` map and `Metrics` map on `DependencyEdge` (schema supports arbitrary typed qualifiers).
- Transmission qualifier normalization on `DEPENDS_ON` edges (`throughput_share`, `capacity_constraint`, `reroute_penalty`, `reroute_latency_delta_hours`, `reroute_cost_delta_pct`) with best-effort enrichment hints.
- `ExposurePathArtifact` and `ExposurePathsRepository` (`repo/exposure_paths.go`, `models/exposure_path.go`).
- Risk appetite profiles with per-axis thresholds (`models/risk_appetite.go`, `repo/risk_appetite_profiles.go`).
- High-profile calculator with `chokepoint_proxy` signal type (`highprofile/calculator.go`).
- Tail scenario detection in `GetFragmentRollup()` (impact >= 0.85 & likelihood <= 0.60 triggers threshold adjustment).

---

## 4. Gap Analysis: Why Current Logic Is Not Yet Sufficient

### Gap 1: Risk/Scout separation is partially complete

Independent Risk runtime now exists (`RiskJob`, dedicated queue, dedicated worker endpoint, dedicated stage machine).
Remaining coupling risk:

- Scout hot path still executes signal propagation in parallel to Risk runtime for backward compatibility.
- Some operational entrypoints still rely on Scout-originated triggers first, then fan out to Risk.
- Full "Risk as executor of record" migration for all external-risk decisions is not fully complete.

### Gap 2: Runtime wiring is improved, but not complete end-to-end

Several previously disconnected components are now wired in runtime path:

- `EscalationClassifier` is called from `SignalPipeline`.
- `ExternalApplicabilityEvaluator` is wired into `SeedInjector` in the Scout signal path.
- `ProfileLookup.Initialize()` is called at startup.
- `InterpretationDispatcher` and `InterpretationProcessor` are active.
- `re_evaluate` feedback event is handled.

Remaining runtime gaps:

- `RollupService` is still not wired as primary rollup source.
- Dual-run period exists (Scout-path + Risk-path); migration toggles and ownership boundaries still need hardening.

### Gap 3: Proof-of-impact exists but is still v1

`RiskStagePropagate` now emits `proofOfImpact` per entity-like node (`/org`, `/gov`) with source signals and transmission paths.
Remaining gap: enrich artifact to include edge-level mechanism qualifiers and stronger calibrated path confidence (currently conservative defaults).

### Gap 4: Exposure path typing expanded, coverage now the bottleneck

`ExposureType` now includes logistics-route, legal-regulatory, financial-settlement, geopolitical, climate-hazard, information-space.
Current bottleneck moved to data coverage/quality of these typed paths across entities and geos.

### Gap 5: Chokepoint matching improved, but policy source is still static

`profile_lookup.go` now uses alias normalization with canonical chokepoint IDs.
Remaining gap:

- alias/policy registry is still code-local (not externalized config),
- canonical geo-path governance for chokepoints still needs stricter alignment with validation policy.

### Gap 6: Graph lacks transmission-grade structure for systemic scenarios

Relation graph quality is good at structural consistency, but transmission-critical qualifiers (capacity, throughput/volume share, substitutability, reroute latency, reroute cost) are optional and often absent. Current high-profile detection includes `chokepoint_proxy` heuristic, which is useful for triage but not sufficient as transmission truth source.

### Gap 7: Propagation profiles are generic, not chokepoint-aware

Current `GetForSignal()` applies lightweight chokepoint boosts, and propagation now also consumes transmission qualifiers (`throughput_share`, `capacity_constraint`, reroute penalties/latency/cost deltas). This is a strong v1 but still not a full policy-pack system with externalized route-class models.

### Gap 8: Appetite thresholds are static for tail events

Tail scenario detection exists in `GetFragmentRollup()` (threshold adjusted to 75%/70% of base for extreme tails). But there is no formal scenario mode (normal vs tail-event) and no asymmetric trigger policies by market/geo.

---

## 5. Required Additions (Priority Order with Dependencies)

### Dependency graph

```
P0: Scout/Risk separation ──────────────────────────────────┐
                                                             │
P0.1: Transmission-ready world model (prompts + critic) ─────┤
                                                             │
P1a: Wire existing components ──────────────────────────────┤
  ├─ EscalationClassifier into signal pipeline               │
  ├─ ApplicabilityEvaluator into signal pipeline             │
  ├─ InterpretationDispatcher + Processor startup            │
  ├─ ProfileLookup.Initialize() at startup                   │
  └─ re_evaluate feedback handler                            │
                                                             │
P1b: Entity-level proof-of-impact artifact ─────────────────┤
  (depends on P1a: applicability + propagation wired)        │
                                                             │
P1c: Expand ExposureType enum ──────────────────────────────┤
  (depends on P0.1: graph has typed exposure channels)       │
                                                             │
P1d: Tighten applicability with exposure-path gating ────────┤
  (depends on P1a + P1c)                                     │
                                                             │
P2a: ChokepointAliasMap + config-driven policy activation ──┤
  (depends on P0.1)                                          │
                                                             │
P2b: Chokepoint-specific propagation policy packs ──────────┤
  (depends on P2a + graph data for priority chokepoints)     │
                                                             │
P2c: Tail-event appetite mode ──────────────────────────────┤
  (depends on P1a: escalation classifier wired)              │
                                                             │
P3: Sparse-graph robustness (on-demand expansion) ──────────┘
```

### P0. Decouple risk runtime from Scout runtime

Risk/opportunity processing must be a first-class pipeline, not a side effect of Scout jobs.

**Architecture rule:**

- `Scout` lifecycle: `Source/Query → ScoutJob → Fragment/Edge/Fact updates`.
- `Risk` lifecycle: `RiskTrigger(Event/Signal/Watchdog/Manual) → RiskJob → Identify/Escalate/Applicability/Seed/Propagate/Rollup/Interpret/Alert`.

**Mandatory invariants:**

- Scout and Risk have different job models, queues, state machines, retries, and terminal statuses.
- Scout may publish facts/signals and update graph state, but must not orchestrate risk stages.
- Risk must be runnable independently from a canonical anchor (`/org/...`, `/domain/...`, `/loc/...`) even when no new Scout job exists.
- Watchdog for risk semantics enqueues `RiskJob`, not `ScoutingJob`.
- Event-level idempotency and wave deduplication belong to `RiskJob` domain.
- Shared surface between Scout and Risk is limited to data contracts (`ontology_facts`, signal schema, graph references).

**Operational contracts:**

- Separate Cloud Tasks queue and worker endpoint for risk runtime.
- Dedicated WS event types for risk job/stage visibility (shared WS transport, separate semantic stream).
- Stage-level audit artifacts (`reasonCodes`, `inputs/outputs`, `attempts`, `terminalReason`) persisted in risk domain storage.
- Manual operator entrypoint: "start from anchor and evaluate/propagate risk now" without Scout coupling.

**Implementation reference:** see `RISK_SIGNAL_PIPELINE_TECHNICAL_DESIGN.md` for RiskJob data model, stage contracts, Cloud Tasks topology, WS events, control plane, and rollout plan.

### P0.1. Build transmission-ready world model

The base Scout pipeline must build a graph usable for systemic transmission math, not only entity discovery.

**Mandatory graph contracts:**

- **Chokepoint representation**: chokepoints must be first-class canonical nodes, with alias normalization from shorthand paths to canonical loc paths.
- **Route/flow structure**: graph must include route/corridor/flow edges connecting producers, transit chokepoints, logistics infrastructure, and consuming markets/entities.
- **Mechanism-carrying dependencies**: relation extraction must prioritize typed dependencies (`DEPENDS_ON` with facet) over generic links for transmission-critical chains.
- **Transmission qualifiers on edges**: capacity, throughput/volume share, substitutability, reroute latency delta, reroute cost delta, confidence/provenance. These use the existing `DependencyEdge.Qualifiers` map and `DependencyEdge.Metrics` map — no schema migration needed.
- **Entity reachability**: for high-stakes classes, at least one explainable bounded-hop path from signal seed to affected `/org` node must exist or be explicitly marked missing.

**Prompt/policy implications:**

- Scout/Research prompts must explicitly request route and dependency qualifiers when class is logistics/geopolitical/climate-critical.
- Critic must reject outputs as incomplete when chokepoint transmission fields are missing for a relevant scenario class.
- Missing-data questions must be generated for absent transmission qualifiers, not only for missing entities.

**Taxonomy note:** No new edge types are required. Existing `DEPENDS_ON` with facets (`logistics`, `infrastructure`, `supply`, `distribution`), `SUPPLIES_TO`, `TRANSFORMS_INTO`, `CONTROLS_ACCESS` cover needed semantics. The gap is data population and prompt discipline, not schema.

### P1a. Runtime wiring status (implemented)

Core wiring tasks are now active in runtime:

| Component | Runtime status | Wiring location |
|-----------|----------------|-----------------|
| `EscalationClassifier` | **Connected** | `SignalPipeline.ProcessEvent()` → `enrichSignalEscalation()` |
| `ExternalApplicabilityEvaluator` | **Connected** | `SeedInjector.WithApplicabilityEvaluator()` in main signal pipeline and events inject |
| `InterpretationDispatcher` | **Connected** | `PropagationEngine.WithFragmentChangedHook()` |
| `InterpretationProcessor` | **Connected** | `Start()` at server startup |
| `ProfileLookup.Initialize()` | **Connected** | startup initialization in `main.go` |
| `re_evaluate` feedback | **Connected** | feedback router handles `re_evaluate` and `attention_escalation` |
| `Interpreter.signalsRepo` | **Connected** | `OntologySignalFactsRepo` adapter |

Open item: keep this wiring under explicit feature flags during migration to avoid hidden coupling regressions between Scout and Risk runtimes.

### P1b. Entity-level proof-of-impact artifact

Risk output must include an explicit `proof-of-impact` artifact per affected entity node.

Minimum fields:

- `entityPath` (`/org/...` or legal-entity canonical node),
- `sourceSignalIds`,
- `supportingFactIds/sourceRefs`,
- `propagationPaths` (ordered canonical paths and edge types),
- `pathConfidence` and uncertainty flags,
- `impact/likelihood/timeHorizon/modality` at entity node,
- `topMechanisms` and `reasonCodes`,
- `dispatchReady` boolean.

Acceptance rule:

- If no explainable proof path exists for an entity-level risk state, the state is non-dispatchable and must be downgraded or marked unresolved.

### P1c. Expand ExposureType enum

Runtime status: expanded and implemented in `models/exposure_path.go`.

Implemented additions:

- `LOGISTICS_ROUTE_RISK` — maritime/air/land route dependency.
- `LEGAL_REGULATORY_RISK` — regulatory/sanctions/compliance exposure.
- `FINANCIAL_SETTLEMENT_RISK` — payment/clearing/FX dependency.
- `GEOPOLITICAL_RISK` — conflict zone/sovereignty/territorial exposure.
- `CLIMATE_HAZARD_RISK` — weather/natural hazard exposure.
- `INFORMATION_SPACE_RISK` — platform/communications dependency.

Applicability and propagation must use these as first-class filters. Backward compatible with existing `SUPPLY_CHAIN_RISK` artifacts.

### P1d. Tighten applicability with exposure-path gating

`ExternalApplicabilityEvaluator` already has `WithExposurePathsRepo()` and `hasQualifyingExposurePath()`. The infrastructure is ready.

What's needed:

1. In `Evaluate()`, after heuristic score passes 0.35 threshold, add hard gate: check exposure path from signal seed to candidate fragment (direct or bounded-hop).
2. High-stakes axes (`logistics/capacity`, `logistics/latency`, `physical_flow/supply`, `geo/conflict`): reject propagation if no path exists, regardless of heuristic score. **Implemented and active in main pipeline.**
3. Lower-stakes axes: allow propagation but tag with `confidence_penalty: sparse_graph`. **Already implemented.**

Current task is quality calibration (false-spread vs missed-material-entity), not basic wiring.

### P2a. ChokepointAliasMap and config-driven policy activation

Current `isStrategicChokepointPath()` uses hardcoded substring matching.

Required:

- Deterministic `ChokepointAliasMap` mapping shorthand/legacy loc forms to canonical loc paths.
- Store canonical chokepoint IDs in config (Firestore or env), not hardcoded.
- Persist alias-to-canonical provenance for auditability.
- Policy packs reference canonical IDs, not substring patterns.

### P2b. Chokepoint-specific propagation policy packs

For strategic chokepoints (Hormuz, Bab el-Mandeb, Suez, Malacca, Panama):

- Dedicated edge weights and damping per route class.
- Substitution and reroute penalties.
- Latency multipliers by route class.

**Dependency**: policy packs are only useful if graph contains route/flow data (see P0.1). Without graph data, policy packs are empty shells.

**Data bootstrap plan required:**

1. Define required edge attribute schema for route/flow/corridor edges using existing `DependencyEdge.Qualifiers` and `DependencyEdge.Metrics` maps.
2. Curate initial data for 5 priority chokepoints (manual/curated for v1).
3. Build `PropagationPolicyPack` that reads edge qualifiers and computes damping dynamically (instead of fixed `StrongEdgeSpec.Weight`).
4. Register policy packs per chokepoint canonical path.

The current `PropagationProfile` with `StrongEdgeSpec` and `TransformationRule` is a sufficient base.

### P2c. Tail-event appetite mode

Current tail detection in `GetFragmentRollup()` is a good start (adjusts threshold to 75%/70% for extreme tails).

Required upgrade:

- Formal scenario mode (`normal` vs `tail_event`) as part of appetite evaluation.
- Asymmetric trigger policies: lower tolerance for catastrophic downside signals.
- Explicit tail-mode thresholds by market/geo in `RiskAppetiteProfile`.

### P3. Sparse-graph robustness

When exposure graph is sparse:

- On-demand expansion of missing dependencies (trigger targeted Scout job or research query).
- Conservative uncertainty uplift instead of blind spread.
- Tag propagated states with `sparse_graph` flag for downstream filtering.

---

## 6. Implementation Notes (Code Audit Findings)

### Note 1: Escalation classifier — deterministic baseline first, LLM second

The `EscalationClassifier` (`signals/escalation_classifier.go`) is already implemented with the correct two-phase approach.

**Phase 1 (deterministic, implemented):**

- For each `(actor_path, location_path)` pair, scans 30-day sliding window of signal facts.
- Thresholds: `watch` (>=2 signals), `warning` (>=3 + rising/multi-axis), `critical` (>=4 + rising + corroborated + maxAmp >= 0.7).
- Confidence: base 0.35 + incremental boosts for count, trend, diversity, corroboration, amplitude.
- Output: `EscalationAssessment` with `Level`, `Confidence`, `Reasons`, `Buckets`.

**Phase 2 (LLM enrichment, implemented):**

- When deterministic score reaches `warning` or above, invokes LLM for evidence bucket classification.
- Final level = `max(deterministic, llm)` with combined confidence.

Runtime status: wired into `SignalPipeline.ProcessEvent()`.

### Note 2: Exposure-path gating — ready to wire

`ExposurePathArtifact` and `ExposurePathsRepository` are implemented. `ExternalApplicabilityEvaluator` already has:

- `WithExposurePathsRepo()` builder.
- `hasQualifyingExposurePath()` with three-tier check: direct SeedPaths match, exposure artifacts (lens-scoped), graph BFS (max 2 hops).
- Hard gate for high-stakes axes, confidence penalty for others.

Runtime status: evaluator is passed to `SeedInjector` in main signal pipeline and events inject.

### Note 3: Feedback loop — hooks designed, partially connected

`feedback.Emitter` interface exists. `Interpreter` emits `re_evaluate` events for low-confidence/high-impact. `ConcentrationAnalyzer` emits `attention_escalation` for first-time hotspots.

**Wired**: both `attention_escalation` and `re_evaluate` are handled in feedback routing.

Next step is policy tuning for when/where re-evaluate creates `RiskJob` to avoid noisy loops.

### Note 4: Risk extraction contract — entity-impact proof hints

Current extraction captures `likelihood`, `impact`, `timeHorizon`, `modality`. It should also emit structured hints for downstream proof building:

- Candidate affected entity paths (if known from event text).
- Candidate dependency path fragments (seed → transit → target).
- Mechanism evidence references tied to each candidate path.

This keeps downstream deterministic stages explainable and reduces blind propagation.

### Note 5: On-demand mode — manual start from anchor objects

`on_demand` (`lazy`) is a valid permanent mode of Risk runtime operation. At the current stage it is launched manually by operator/API, while automatic modes can be added later.

On-demand flow:

1. Operator/API starts from anchor object (`/org`, `/domain`, `/loc`).
2. `Risk Scout` performs strict search-grounding to fetch freshest external evidence for the anchor scope.
3. Grounded evidence is persisted as deterministic snapshot (sources, timestamps, confidence, provenance).
4. Snapshot is then processed by Risk pipeline stages (`Identify → ... → Alert`) under `RiskJob`.

On-demand constraints:

- Grounding payloads must include explicit freshness metadata (`publishedAt`, `observedAt`, retrieval time).
- Snapshot processing must be replayable and idempotent.
- Scope relevance rules still apply.
- Manual start today does not change the long-term Scout/Risk separation rule.

### Note 6: Risk Scout is a permanent Risk runtime component

`Risk Scout` is a long-lived ingress layer for Risk pipeline. Risk quality depends on normalized evidence contract quality, not on a specific ingestion channel.

Allowed ingestion modes for the same `Risk Scout` contract:

1. `on_demand` (lazy) from anchor objects — **MVP mode**.
2. `scheduled_pull` (periodic refresh).
3. `event_stream` (continuous monitoring).

In all modes, `Risk Scout` outputs the same canonical envelope: normalized claims/events/signals, provenance, freshness, confidence. Switching ingestion mode must not require changes in downstream Risk stages.

### Note 7: Watchdog lifecycle migration

`monitor.go` now enqueues `RiskJob` when `RiskJobsRepository` is configured, with fallback to `ScoutingJob` only for backward compatibility.

Next step: retire fallback mode so watchdog risk semantics always flow through Risk runtime.

---

## 7. External Event Catalog v1 (Risk & Opportunity)

### 7.1 Purpose

Canonical catalog of external event classes for Horizon signal-first analysis. Not a new taxonomy layer — these are classification hints that map to the existing 12-axis basis.

### 7.2 Categories

#### 1) Macro & Monetary

- Central bank actions, sovereign fiscal/regulatory packages, FX controls.
- **Primary axes**: `financial/credit`, `financial/settlement`, `info/uncertainty`.

#### 2) Sanctions & Legal-Regulatory Shocks

- Sanctions packages, export controls, licensing regime shifts, legal bans.
- **Primary axes**: `legal/access`, `legal/compliance`, `financial/settlement`.

#### 3) Logistics & Physical Flow Disruptions

- Port closures/blockades, canal/strait restrictions, corridor outages.
- **Primary axes**: `logistics/capacity`, `logistics/latency`, `physical_flow/supply`.

#### 4) Resource & Commodity Base Changes

- Major deposit discoveries, extraction shutdowns, export quotas/bans.
- **Primary axes**: `physical_flow/supply`, `operational/continuity`, `geo/conflict` (when geopolitically driven).

#### 5) Financial-Strategic Capital Allocation

- Sovereign mega-investments, large M&A, production cluster relocation.
- **Primary axes**: `financial/credit`, `operational/continuity`, `physical_flow/demand`, `physical_flow/supply`.

#### 6) Technology Regime Shifts

- Technological breakthroughs, standard/protocol shifts, tech authorization/prohibition.
- **Primary axes**: `operational/continuity`, `info/reputation`, `legal/access`.

#### 7) Geopolitical & Security Escalation

- Conflict escalation around strategic nodes, nationalization, sabotage/cyber attacks.
- **Primary axes**: `geo/conflict`, `operational/continuity`, `logistics/capacity`.

#### 8) Climate & Natural Hazard Systemic Events

- Extreme weather, multi-season climate anomalies, natural disasters.
- **Primary axes**: `operational/continuity`, `physical_flow/supply`, `logistics/latency`.

#### 9) Policy-Legal-Reputation Market Access Events

- Antitrust/regulatory rulings, carbon/CBAM/traceability policy, reputation shocks.
- **Primary axes**: `legal/access`, `info/reputation`, `physical_flow/demand`.

#### 10) Public Health & Biosecurity Systemic Events

- Pandemic outbreaks, biosecurity restrictions, health emergency policy shocks.
- **Primary axes**: `operational/continuity`, `logistics/latency`, `physical_flow/supply`, `info/uncertainty`.

#### 11) Information-Space & Communications Governance Shocks

- Platform bans, internet controls, communication channel governance shocks.
- **Primary axes**: `info/uncertainty`, `info/reputation`, `operational/continuity`.

#### 12) Grid & Energy Infrastructure Reliability Events

- Grid instability/blackouts, cross-border fuel/power constraints, energy infrastructure outages.
- **Primary axes**: `operational/continuity`, `physical_flow/supply`, `logistics/capacity`, `geo/conflict` (when politically driven).

### 7.3 Event record requirements

Each event record should include:

- `class` (from this catalog)
- `likelihood` (0..1)
- `impact` (0..1)
- `timeHorizon` (`immediate|short_term|medium_term|long_term`)
- `modality` (`rhetoric|potential|projected|escalation|disruption|confirmed|actual`)
- `leadingIndicators` (for escalation seriousness)
- `applicabilityRules` (geo/market/entity relevance — maps to existing applicability evaluator input)
- `propagationTemplate` (default path + damping hints — maps to existing `PropagationProfile`)
- `opportunityMirror` (who benefits under this scenario)

### 7.4 Implementation priority

Start with highest business leverage classes:

1. Sanctions & legal-regulatory (most deterministic transmission paths).
2. Logistics/chokepoints (highest systemic impact, Hormuz reference).
3. Macro/monetary (broad market effects).
4. Resource base changes (supply-chain transmission).
5. Geopolitical escalation (escalation classifier primary use case).
6. Grid/energy reliability.
7. Public health/biosecurity.

---

## 8. Maturity Framework

Reliable external risk/opportunity intelligence is a function of:

`Graph Quality (Field Connectivity) × Mechanism Awareness × Deterministic Computation Discipline`

### A) Field Connectivity (Graph Quality)

Required: connected, explainable dependency field, not isolated nodes.

Operational KPIs: `coverage_ratio`, `missing_critical_edge_rate`, `freshness_sla_breach_rate`, `conflict_rate`, `explainable_path_availability`.

### B) Mechanism Awareness

Signals propagate because mechanism semantics justify propagation, not because edges merely exist.

Operational KPIs: `applicability_precision`, `false_spread_rate`, `escalation_detection_quality`, `path_plausibility_score`.

### C) Deterministic Core + LLM Boundary

**LLM responsibilities**: signal/mechanism extraction from text, ambiguity resolution, hypothesis generation for graph completion, human-readable interpretation.

**Deterministic responsibilities**: applicability gating, propagation physics, appetite enforcement, thresholding, queueing, audit logic.

### D) Canonical Runtime Chain

`Event → Signal → Applicability → Seed Injection → Axis Propagation → Rollup/Explain → Interpretation → Concentration`

Owned by Risk runtime, executed under `RiskJob` control. Scout may emit events/signals that trigger this chain but must not be its executor of record.

---

## 9. Cross-References

| Document | Scope | Source of truth for |
|----------|-------|---------------------|
| This document | Business goal, gap analysis, priority plan, event catalog | "Why" and "What" |
| `RISK_SIGNAL_PIPELINE_TECHNICAL_DESIGN.md` | RiskJob lifecycle, stage contracts, Cloud Tasks, WS events, retry, runbook | "How" (runtime implementation) |
| `roadmap.md` | Overall Horizon implementation plan, stabilization status | Broader system roadmap |
| `models/axis_spec.go` | 12-axis basis, default propagation specs | Axis taxonomy (code is truth) |
| `models/signal_descriptor.go` | Signal contract, modality, time horizon, escalation | Signal schema (code is truth) |
| `models/dependency_edge.go` | Relation types, facets, edge qualifiers | Edge taxonomy (code is truth) |
| `models/exposure_path.go` | Exposure type enum, path key format | Exposure model (code is truth) |
