# Lens Q&A — Question Answering Inside a Lens

> **Scope**: This document describes the **Analysis** view mode of a lens. All UX, API, and data flow sections below apply to Analysis. Graph and Map are sibling views; the lens data (graph) is shared and persisted across view switches.

---

## Lens View Modes

When the user is inside a lens, a view switcher offers three modes:

| View | Purpose |
|------|---------|
| **Graph** | Node-link diagram: entities, edges, propagation paths. Explore structure and connections. |
| **Map** | Geographic view: facilities, deposits, TRACK (vessels, aircraft), zones on a map. Spatial context. |
| **Analysis** | Q&A, tables, metrics, answer panels. Everything in this document lives here. |

**Placement**: Tab switcher in the lens area — top center or under the lens title. Separate from project/lens switcher (left). View mode is lens context, not global navigation. Labels: **Graph** · **Map** · **Analysis**.

**Graph persistence**: The graph (fragments, edges, layout) stays in memory when switching between Graph | Map | Analysis. Only unload when:
- Switching to another lens
- Switching to another project
- Closing the lens

Same data, different presentation. Map and Analysis consume the same graph; no refetch on view change.

**URL**: `?view=graph|map|analysis` for sharing and deep links.

---

## Vision

User opens a lens (supply chain EV batteries, governance of mining sector, etc.), switches to **Analysis**, types a question in natural language — and gets a grounded, evidence-backed answer in seconds, with the graph lighting up to show *why*.

```
User: "How would a 5% VAT increase in Indonesia affect our battery costs?"

System (2-3 sec):
  Answer: "Direct impact on nickel ore (+3-5% cost pass-through via PT Vale Indonesia),
  with secondary effect on cathode materials through CATL's Morowali smelter.
  Current AIS data shows 3 bulk carriers en route from Sulawesi — short-term
  inventory buffers the shock for ~6 weeks."

  [Graph highlights the propagation path, leaf nodes pulse with signal strength]
  [Evidence panel shows: Indonesian tax policy signal, AIS vessel data, SQM quarterly report]
```

Not a chatbot. A **simulation engine with a mouth**.

---

## Overview for Stakeholders

### What this is

Imagine a map of all companies, factories, resources, and routes connected to your business. That's a lens — your personal view of the world.

Right now you can view this map. We want to make it answer questions.

### How it will work

You open your lens (e.g. "EV battery supply chain"), switch to **Analysis** (Graph | Map | Analysis), and see a prompt at the bottom: "What would you like to know?"

You type: "How would a 5% VAT increase in Indonesia affect our battery costs?"

Within 2–3 seconds, three things happen:

1. **The system understands the question.** It parses: "tax change in Indonesia, need to trace impact through the nickel supply chain." A fast AI model does this in fractions of a second.

2. **The system simulates the impact.** It treats "VAT increase" as an impulse and propagates it through the graph — from the source (Indonesia) along all connected companies and products. Like dropping a stone in water and watching the ripples. At each step, the effect weakens. Pure math — runs in milliseconds, no database calls.

3. **The system assembles the answer.** It takes the simulation result (which companies are affected and how much), adds real-world data from the graph "tips" (satellite imagery of mines, vessel tracking, recent news), and generates a human-readable answer.

### What the user sees (Analysis view)

First — one sentence in large type:

> "A 5% VAT increase in Indonesia would raise your battery costs by roughly 2–4%, mainly through nickel."

At the same time, the graph (in the background or embedded) dims except for the chain through which the impact flows. Like an X-ray — only the causal path from cause to effect is visible.

Then cards appear one by one (not all at once):

**"How does it propagate?"** — visual domino chain:
```
VAT in Indonesia  →  PT Vale Indonesia  →  Nickel ore  →  CATL Morowali  →  Battery cells
■■■■■■                 ■■■■■░                ■■■■░░          ■■░░░░           ■░░░░░
cause                 strong               strong          moderate         weak
```

**"What's happening right now?"** — real data:
- 🟢 Lithium mining in Atacama — normal (satellite, 2h ago)
- 🟡 3 vessels delayed at Sulawesi port (AIS tracking, 6h ago)
- 🔴 Export quota review (Reuters, yesterday)

**"What can we do?"** — concrete actions:
1. Start talks with SQM as an alternative supplier
2. Increase cathode material buffer (~6 weeks)

**"What don't we know?"** — the system states where data is missing and offers a "Scout" button to automatically gather it.

### Where the data comes from

Nothing is invented. Every claim is backed by one of three sources:

1. **The graph** — the system knows PT Vale Indonesia supplies nickel to CATL because that link exists in the graph (found and verified by Scout)
2. **Simulation** — the system ran the "tax" impulse through the graph and computed how it decays at each step. Not LLM opinion — math on data
3. **Real signals** — at the graph "tips" (mines, ports, conflict zones) the system monitors the real world: satellite imagery, vessel tracking (AIS), internet outage monitoring, news feeds. These are tied to specific nodes and updated in real time

See **Data Sources & Leaf Evidence** for how we ingest these feeds (USGS, OpenSky, UCDP, NASA FIRMS, AIS, etc.) and normalize external API formats into our core.

### Why it's fast

- All lens data is in memory from the moment you open it. The question doesn't wait on a database
- Simulation is pure in-memory computation, no server calls. 10–50 milliseconds
- The answer starts streaming before the AI finishes thinking
- Result: first sentence visible in ~1–2 seconds

### Follow-up — conversation

After the answer, you can ask a follow-up. The system keeps context:

> "What if we switch to Australian nickel?"

The system shows two chains side by side: current (Indonesia) and alternative (Australia). You immediately see: "Indonesian path is cheaper but riskier; Australian — higher logistics cost but more stable." Trade-off at a glance.

---

## What Already Exists

| Primitive | Location | Role in Q&A |
|-----------|----------|-------------|
| `LensIntent` | `models/lens.go:76` | Stores user questions (`pending → answered → archived`) |
| `SignalDescriptor` | `models/signal_descriptor.go` | 12-axis quasi-physical impulse with amplitude, polarity, constraints |
| `PropagationEngine` | `propagation/signal_propagation.go` | BFS traversal with damping, axis transforms, waveguide boosts |
| `Interpreter` | `interpretation/interpreter.go` | Single-node signal → LLM summary + impact + mitigations |
| `DomainAnswerSnapshot` | `models/domain_answer_snapshot.go` | Thesis, ExplanatoryUnits, CriticalDeps, ImpactPaths, Alternatives, DataGaps |
| `MaterializedArtifact` | `models/materialized_artifact.go` | Pre-built supply chain paths, governance chains, ecosystem maps |
| `GraphIntent` | `models/graph_intent.go` | NL → structured graph modification plan (decomposition pattern) |
| `Waveguide` | `propagation/waveguide.go` | Signal boost on materialized chain edges (rigid conduit physics) |

**Key insight**: The system already propagates signals, interprets them per-node, and builds domain answers. What's missing is **question-driven simulation** — the ability to synthesize a hypothetical signal from a question and read the graph's response.

---

## Architecture: Three Phases

```
 ┌─────────────────────────────────────────────────────────────────────┐
 │  USER QUESTION (NL)                                                 │
 │  "What are the supply chain blockers for lithium from Chile?"       │
 └──────────────────────┬──────────────────────────────────────────────┘
                        │
          ┌─────────────▼──────────────┐
          │  PHASE 1: DECOMPOSE        │  ~200ms (LLM structured output)
          │                            │
          │  Question → QuestionPlan:  │
          │  • HypotheticalSignals[]   │  axis, mechanism, seeds, amplitude
          │  • TargetFilter            │  what nodes to observe
          │  • AnswerMode              │  impact / blockers / alternatives
          │  • TimeHorizon             │  immediate → long_term
          └─────────────┬──────────────┘
                        │
          ┌─────────────▼──────────────┐
          │  PHASE 2: SIMULATE         │  ~100-300ms (in-memory BFS)
          │                            │
          │  Two paths:                │
          │  A) What-if → inject       │  Hypothetical propagation
          │     signal, read response  │  (shadow namespace, no persist)
          │  B) What-now → read        │  Current State metrics in scope
          │     existing signals       │
          │                            │
          │  + Leaf Evidence Collector  │  Physical signals at terminals
          │  + Chain Context Loader    │  MaterializedArtifacts in scope
          └─────────────┬──────────────┘
                        │
          ┌─────────────▼──────────────┐
          │  PHASE 3: SYNTHESIZE       │  ~1-2s (LLM generation)
          │                            │
          │  Structured context →      │
          │  Grounded answer with      │
          │  evidence refs, confidence,│
          │  highlighted graph paths   │
          └────────────────────────────┘

          Total: 1.5-3s end-to-end
```

---

## Phase 1: Decompose

### QuestionDecomposer

Takes a natural language question + lens context, returns a structured plan.

```go
type QuestionPlan struct {
    // Parsed understanding
    NormalizedQuestion string   // English-normalized form
    AnswerMode         string   // "impact_assessment" | "blocker_analysis" | "alternative_sourcing" | "risk_forecast" | "dependency_audit"
    TimeHorizon        string   // immediate | short_term | medium_term | long_term

    // What to simulate
    HypotheticalSignals []HypotheticalSignal  // Empty = read-only query (no simulation)

    // What to observe
    TargetFilter TargetFilter

    // What to include in context
    ContextHints []string  // ["supply_chains", "governance", "leaf_signals", "market_data"]
}

type HypotheticalSignal struct {
    Axis        string   // From 12-axis basis
    Mechanism   string   // Specific cause (e.g., "vat_increase", "port_closure")
    Amplitude   float64  // 0.0-1.0 estimated strength
    Polarity    string   // "risk" | "opportunity"
    SeedHints   []string // Canonical path patterns for seed resolution
    TimeHorizon string
    Modality    string   // "projected" for what-if, "actual" for real events
}

type TargetFilter struct {
    // What nodes to focus the answer on
    ObservePathPatterns []string  // e.g., ["/product/lithium-*", "/org/*/battery*"]
    ObserveAxes         []string  // Which axes matter for this question
    MaxNodes            int       // Limit context window (default 20)
}
```

### LLM Prompt for Decomposition

```
You are decomposing a business question into a simulation plan.

LENS CONTEXT:
- Scope: {lens.Scope canonical paths, top 30}
- Domain modes: {lens.DomainModes}
- Active signals: {top 5 signals by amplitude in scope}
- Available chains: {MaterializedArtifact summaries}

AVAILABLE AXES (12-axis basis):
- legal/access: sanctions, bans, license revocations
- legal/compliance: regulatory exposure, secondary sanctions
- financial/settlement: payment rails, FX, banking access
- financial/credit: credit risk, default exposure
- physical_flow/supply: material shortage or glut
- physical_flow/demand: demand surge or collapse
- logistics/capacity: route closures, port congestion
- logistics/latency: delays, lead time increases
- operational/continuity: strikes, outages, disasters
- info/uncertainty: regulatory fog, ambiguous timelines
- info/reputation: ESG signals, brand impact
- geo/conflict: geopolitical instability, conflict zones

QUESTION: "{user question}"

Return a QuestionPlan (JSON). Rules:
- For "what would happen if X" → create HypotheticalSignal with modality=projected
- For "what are the current risks/blockers" → no signals, just TargetFilter
- SeedHints: use canonical path patterns from lens scope
- Amplitude: 0.3=minor, 0.5=moderate, 0.7=significant, 0.9=severe
- AnswerMode: pick the best fit for the question
```

### Speed Optimization

The decomposition LLM call is the first latency cost. To minimize:

1. **Small model, structured output**: Use Gemini Flash / Haiku with JSON schema enforcement — ~200ms
2. **Warm cache**: Common question patterns (supply blockers, risk impact, alternatives) → cached templates. If question matches template with >0.85 similarity, skip LLM and fill from template
3. **Parallel prefetch**: While LLM decomposes, start loading lens scope fragments + artifacts from Firestore

---

## Phase 2: Simulate

### Path A: Hypothetical Propagation (What-If)

```go
func (e *QAEngine) SimulateHypothetical(
    ctx context.Context,
    signals []HypotheticalSignal,
    lensScope []string,  // fragment IDs in lens
) (*SimulationResult, error)
```

Key: **shadow namespace** — don't pollute real `fragment.State`.

```go
type SimulationResult struct {
    // Per-node impact
    NodeImpacts []NodeImpact  // sorted by amplitude desc

    // Propagation trace (for visualization)
    TracePaths []TracePath

    // Leaf evidence (physical signals at terminal nodes)
    LeafEvidence []LeafSignalEvidence

    // Chain impacts (which supply chains affected)
    ChainImpacts []ChainImpact
}

type NodeImpact struct {
    FragmentID    string
    CanonicalPath string
    Name          string
    Type          string
    // Per-axis amplitudes
    AxisAmplitudes map[string]float64  // axis → amplitude at this node
    TotalAmplitude float64             // max across axes
    Hops           int                 // distance from seed
    PathFromSeed   []string            // canonical path trace
}

type TracePath struct {
    NodeCanonicalPaths []string  // ordered sequence
    Axis               string
    Amplitudes         []float64 // amplitude at each node
    EdgeTypes          []string  // edge types traversed
}

type LeafSignalEvidence struct {
    FragmentID    string
    CanonicalPath string
    Name          string
    SignalType    string   // "ais_disruption", "satellite_fire", "internet_outage", etc.
    Axis          string
    Amplitude     float64
    Modality      string   // "actual", "confirmed"
    ObservedAt    time.Time
    Summary       string   // Human-readable from event fact
}

type ChainImpact struct {
    ArtifactID   string
    DisplayName  string
    Kind         string   // SUPPLY_CHAIN_PATH, GOVERNANCE_CHAIN
    MaxAmplitude float64  // Strongest signal on any chain node
    AffectedNodes int
    TotalNodes    int
}
```

**Implementation**: Fork `PropagateSignalFromSeeds` to write into a `map[string]map[string]float64` (fragmentID → axis → amplitude) instead of `fragment.State`. This is the shadow propagation — pure in-memory, no Firestore writes.

### Path B: Current State Query (What-Now)

For questions like "What are current supply chain risks?":

```go
func (e *QAEngine) CollectCurrentState(
    ctx context.Context,
    lensScope []string,
    targetFilter TargetFilter,
) (*SimulationResult, error)
```

Read `fragment.State` for all scope fragments, filter by target axes, rank by amplitude. No propagation needed — just a structured read.

### Leaf Evidence Collector

**This is the physical-signals-at-endpoints idea.**

Terminal nodes (deposits, facilities, zones, locations) accumulate real-world signals:
- AIS vessel tracking disruptions
- Satellite fire anomalies
- Internet outage detections
- Conflict event signals
- RSS news signals

These are already in `fragment.State` as metric values with full provenance (SignalID → EventFact → source).

```go
func (e *QAEngine) CollectLeafEvidence(
    ctx context.Context,
    affectedFragmentIDs []string,
) []LeafSignalEvidence
```

Logic:
1. Filter fragments to leaf types: `FACILITY`, `DEPOSIT`, `AFFECTED_ZONE`, `GEO_REGION`
2. Read their `State` metrics — look for `actual`/`confirmed` modality signals
3. Resolve provenance: SignalID → EventFact summary via `EventFactsResolver`
4. Return as structured evidence with human-readable summaries

These become the **ground truth anchors** in the answer: "Based on actual AIS data showing 3 vessels delayed at Sulawesi port..."

### Speed Optimization

1. **Scope fragments pre-loaded**: Firestore batch `GetAll` for lens scope (~50-200ms for 100 fragments)
2. **Shadow propagation is CPU-only**: BFS over in-memory graph, no I/O — ~10-50ms for typical lens
3. **Leaf evidence from cached State**: No extra reads — already loaded with fragments
4. **Parallel**: Collect leaf evidence while propagation runs

---

## Phase 3: Synthesize

### AnswerSynthesizer

```go
type QAAnswer struct {
    // Core answer
    Text       string  `json:"text"`       // 2-5 sentence grounded answer
    Confidence float64 `json:"confidence"` // 0.0-1.0

    // Evidence (for UI)
    Evidence []AnswerEvidence `json:"evidence"`

    // Graph highlights (for visualization)
    HighlightedNodeIDs []string       `json:"highlightedNodeIds"`
    HighlightedEdgeIDs []string       `json:"highlightedEdgeIds"`
    TracePaths         []TracePath    `json:"tracePaths"`

    // Structured details
    ImpactBreakdown []ImpactItem     `json:"impactBreakdown,omitempty"`
    Alternatives    []Alternative    `json:"alternatives,omitempty"`
    DataGaps        []DataGap        `json:"dataGaps,omitempty"`

    // Provenance
    AnswerMode  string `json:"answerMode"`
    TimeHorizon string `json:"timeHorizon"`
    SimulatedAxes []string `json:"simulatedAxes"`
}

type AnswerEvidence struct {
    Type          string  `json:"type"`          // "signal", "chain", "metric", "fact"
    CanonicalPath string  `json:"canonicalPath"` // What entity
    Summary       string  `json:"summary"`       // Human-readable
    Confidence    float64 `json:"confidence"`
    SourceRef     string  `json:"sourceRef,omitempty"` // Signal ID, artifact ID, etc.
}

type ImpactItem struct {
    Entity      string  `json:"entity"`      // Canonical path
    EntityName  string  `json:"entityName"`
    Axis        string  `json:"axis"`
    Amplitude   float64 `json:"amplitude"`
    Mechanism   string  `json:"mechanism"`   // How the impact reaches this entity
    Hops        int     `json:"hops"`
}
```

### LLM Context Assembly

The context window is the critical bottleneck. Must be curated, not dumped.

```
QUESTION: {normalized question}
ANSWER MODE: {impact_assessment}
TIME HORIZON: {medium_term}

=== IMPACT MAP (top 15 nodes by amplitude) ===
1. /org/pt-vale-indonesia — physical_flow/supply: 0.72 (seed, 0 hops)
   Current state: operational, nickel_ore producer
2. /product/nickel-ore — physical_flow/supply: 0.58 (1 hop via PRODUCES)
3. /org/catl — physical_flow/supply: 0.35 (2 hops via SUPPLIES_TO)
   Current state: battery_cell manufacturer, Morowali smelter
...

=== SUPPLY CHAIN PATHS (from MaterializedArtifacts) ===
Chain "Indonesia Nickel → EV Battery":
  /deposit/nickel/sulawesi → /org/pt-vale-indonesia → /product/nickel-ore
  → /org/catl (Morowali) → /product/cathode-material → /product/ev-battery-cell

=== LEAF EVIDENCE (physical signals at endpoints) ===
- /deposit/nickel/sulawesi: risk/physical_flow/supply = 0.45 (satellite: mining activity normal) [actual, 2h ago]
- /loc/id/sulawesi: risk/logistics/capacity = 0.31 (AIS: 3 bulk carriers delayed) [confirmed, 6h ago]
- /zone/id/nickel-export-ban: risk/legal/access = 0.60 (news: export quota review) [projected]

=== EXISTING ANALYSIS ===
Domain thesis: "Indonesia nickel supply chain shows moderate concentration risk..."
Critical deps: [/org/pt-vale-indonesia → /product/nickel-ore: criticality 0.82]

=== DATA GAPS ===
- CATL Morowali plant actual throughput (last reported: Q2 2025)
- Indonesia 2026 mining quota allocations (pending government announcement)

INSTRUCTIONS:
- Answer the question grounding EVERY claim in evidence from above
- Cite evidence as [evidence:N] references
- Flag uncertainty where DataGaps affect the answer
- If answer mode is "alternatives", suggest actionable substitutions
- Return JSON: { text, confidence, evidence[], impactBreakdown[], alternatives[], dataGaps[] }
```

### Speed Optimization

1. **Streaming**: Start streaming the answer text to frontend while LLM generates. First sentence appears in ~500ms
2. **Context budget**: Hard limit at 6000 tokens input. Aggressive pruning: only top-K nodes, only top-3 chains, only confirmed/actual leaf signals
3. **Cached synthesis**: If the same question (normalized) was asked <1h ago and no new signals arrived, return cached answer with `stale: false`
4. **Two-pass generation**: Fast pass (Haiku/Flash, ~500ms) for the text answer, background pass (Sonnet) for detailed ImpactBreakdown and Alternatives

---

## Speed Budget

Target: **< 3 seconds** from question to first answer text visible.

| Phase | Operation | Latency | Notes |
|-------|-----------|---------|-------|
| 0 | Firestore: load scope fragments | 150-300ms | Batch GetAll, 100 frags |
| 1 | LLM: decompose question | 200-400ms | Flash/Haiku, structured JSON |
| 1 | Firestore: load artifacts | 100-200ms | **Parallel with LLM** |
| 2 | Shadow propagation (BFS) | 10-50ms | In-memory, no I/O |
| 2 | Leaf evidence collection | 0ms | Already loaded in phase 0 |
| 3 | LLM: synthesize answer | 800-1500ms | **Streaming**, first token ~300ms |
| **Total** | | **1.3-2.5s** | First sentence visible at ~1s |

### Firestore Optimization

- **Scope fragments cache**: Lens scope fragments are loaded once when lens opens. Q&A reads from in-memory cache. Only delta-refresh on new signals (via realtime listener)
- **Artifact index**: `ScopeArtifacts` on Lens already has artifact refs. Load payload on demand, cache in memory
- **State metrics are on fragments**: No extra collection reads — `fragment.State` already has all signal amplitudes

### Pre-computation

When a lens is opened (or scope changes):
1. Load all scope fragments → memory cache
2. Index leaf nodes (FACILITY, DEPOSIT, ZONE, GEO_REGION)
3. Index existing signals by axis and amplitude
4. Pre-load MaterializedArtifact summaries (DisplayName, NodeRefs, Kind — not full Payload)

This means Phase 2 is essentially free — it's operating on data already in memory.

---

## Data Sources & Leaf Evidence — Where Answers Get Grounded

Leaf evidence (AIS, satellite fire, conflict events, news) is what makes answers grounded. This section describes where these signals come from and how we ingest them into the core.

### Three Pillars of Truth

| Pillar | Source | Role in Q&A |
|--------|--------|-------------|
| **Graph** | Scout, MaterializedArtifacts | Structure: who supplies whom, where the chain goes |
| **Simulation** | PropagationEngine | Impact: how the signal decays along the path |
| **Leaf signals** | Real-time feeds | Ground truth: "3 vessels delayed", "mining normal", "export quota review" |

Nothing is invented. Every claim is backed by one of these three.

### Current State: What Already Exists

**Taxonomy & models:**
- `FragmentType`: `TRACK` (movable objects), `FACILITY`, `DEPOSIT`, `AFFECTED_ZONE`, `GEO_REGION`
- `TrackFragmentData`: `TrackKind` (air/sea/ground), `ExternalTrackID` (MMSI/ICAO24), Lat, Lon, SpeedKts, HeadingDeg, LastSeenAt
- `ObjectType`: `military_aircraft`, `vessel`, `ground_vehicle` — extendable for supply chain: `bulk_carrier`, `tanker`, `cargo`, `ore_carrier`
- `fragment.State`: map of metric values with provenance (SignalID → EventFact → source)

**Ingestion flow:**
- `DataSource` (kind: `horizon_monitor_feed`) with URI → Orchestrator
- `fetchHorizonMonitorDocuments` → HTTP GET
- `parseHorizonMonitorDocuments` → expects `{items|events|data|results: [...]}`
- `HorizonMonitorPipeline.ProcessDocuments` → ontology_facts, TRACK fragments, signals, propagation

**Existing proxies** (`horizonmonitor_external_routes.go`):
- USGS earthquakes, OpenSky flights, GDELT, UCDP, HAPI, NOAA, NGA, FAA — open
- ACLED, NASA FIRMS — require API keys (free)

### Format Gap: External APIs vs Our Format

Our parser expects `{items|events|data|results: [array of objects]}`. External APIs return different structures:

| Source | API format | Compatible? | Notes |
|--------|------------|-------------|-------|
| UCDP | `{data: [...]}` or `{Result: [...]}` | Partial | `data` ✓; `Result` needs key |
| USGS | GeoJSON `{features: [...]}` | No | `features` not in keys |
| OpenSky | `{states: [[icao24, callsign, ...], ...]}` | No | Array of arrays, not objects |
| NASA FIRMS | `{regions: {Ukraine: [...], ...}}` | No | Nested, needs flatten |
| AIS | `{disruptions, density}` | No | No vessel-level objects |
| GDELT, HAPI | Varies | Depends | Per-endpoint |

### Strategy: Adapters in the Core

**Transform in the source ingestion layer** — not in external proxies. The core (Orchestrator + HorizonMonitorPipeline) will normalize API responses before parsing.

**Two adapter types:**

1. **PayloadAdapter** — `(body, feedType) → normalizedPayload`  
   Transforms raw API response to `{items|events|data: [...]}`. Parser unchanged.

2. **EventAdapter** — `(body, feedType, ds) → []horizonMonitorFeedEvent`  
   For non-document sources (AIS disruptions, density zones). Bypasses NormalizedDocument; pipeline gets events directly.

**Adapter registry** by `HorizonMonitorFeedType` from DataSource:
- `opensky_flights` → states array → objects with icao24, lat, lon, velocity, true_track
- `usgs_earthquakes` → features → items
- `ucdp_ged_events` → Result → data
- `nasa_firms_fires` → regions → flattened items
- `ais_snapshot` → disruptions/density → events (EventAdapter)

**OpenSky state format** (reference for adapter):
- Index 0: icao24, 1: callsign, 2: origin_country, 5: longitude, 6: latitude, 7: baro_altitude, 9: velocity, 10: true_track

### Extending Format Support

**parseHorizonMonitorJSONPayload** — add keys:
- `features` (GeoJSON)
- `Result` (UCDP)
- `regions` (FIRMS — with flatten logic)
- `states` (OpenSky — with array→object conversion)

**Alternative bindings** — when documents don't fit:
- Add `ProcessFeedEvents(ctx, tenantID, ds, feedURL, events []horizonMonitorFeedEvent)`
- Same pipeline logic (upsertTrackFragment, upsertEventFact, signals) — input is events, not docs

### Source-by-Source Mapping

| FeedType | Source | Adapter | Output |
|----------|--------|---------|--------|
| opensky_flights | OpenSky API | states→objects | docs → TRACK |
| usgs_earthquakes | USGS GeoJSON | features→items | docs |
| ucdp_ged_events | UCDP API | Result→data or pass | docs |
| nasa_firms_fires | NASA FIRMS | flatten regions | docs |
| ais_snapshot | AIS relay | disruptions→events | events |
| acled_conflict | ACLED | — | docs (if format matches) |
| hapi_conflict_context | HAPI | — | docs (if format matches) |
| gdelt_geo_protest | GDELT | — | docs |

### Open Data for AIS (when integrating directly)

- **AISHub** — free with data sharing, REST API, ~69k vessels
- **OpenAIS** — PostGIS-based, OGC API, `latest_position`, `ship`
- **aisstream.io** — WebSocket, free tier

### Implementation Location

- **Registry**: `discovery/adapters/registry.go` — FeedType → Adapter
- **Adapters**: `discovery/adapters/opensky.go`, `geojson.go`, `ucdp.go`, `firms.go`, `ais.go`
- **Injection point**: `IngestHorizonMonitorFeed` — before `parseHorizonMonitorDocuments`, check adapter by `sourceDS.HorizonMonitorFeedType`
- **ProcessFeedEvents**: `HorizonMonitorPipeline` — new method for event-based path

### Data Flow Summary

```
External API (USGS, OpenSky, etc.)
  → fetchHorizonMonitorDocuments(ctx, feedURL)
  → [if FeedType in adapterRegistry] adapter.Normalize(body) → payload or events
  → parseHorizonMonitorDocuments (if payload) OR ProcessFeedEvents (if events)
  → ontology_facts, horizon_fragments (TRACK), signals
  → propagation → fragment.State
  → LeafEvidenceCollector reads State for Q&A context
```

### Track Taxonomy — Movable Objects (Vessels, Aircraft)

To ground answers in real-time vessel and aircraft data, the system needs a stable taxonomy for TRACK fragments. This section defines the canonical path format and identification scheme.

#### Current State

- **FragmentType**: `TRACK`
- **Canonical path prefix**: `/track/` (fragment_path_policy.go)
- **TrackKind**: `air`, `sea`, `ground`
- **ObjectType**: `military_aircraft`, `vessel`, `ground_vehicle`
- **ExternalTrackID**: MMSI (vessels), ICAO24 (aircraft) — stored in TrackFragmentData

Current path format: `/track/{kind}/{geo}/{slug}-{id8}` where slug = displayName, id8 = shortHash(kind|trackIdentity). The identification number is not directly in the path — lookup by MMSI/ICAO24 requires scan.

#### Proposed Extension: ID-in-Path for Lookup

**Canonical path format**:
```
/track/{kind}/{geo}/{id_scheme}-{id}
```

| Segment | Values | Description |
|---------|--------|-------------|
| kind | `air`, `sea`, `ground` | Mobility domain |
| geo | ISO3166-1 alpha2 (e.g. `id`, `us`) or `global` | Country of last known position; `global` if unknown |
| id_scheme | `mmsi`, `icao24`, `imo`, `flight` | Identification scheme |
| id | Alphanumeric ID | The actual identifier (MMSI 9 digits, ICAO24 6 hex, IMO 7 digits, flight callsign) |

**Examples**:
- `/track/sea/id/mmsi-525123456` — vessel in Indonesian waters, MMSI 525123456
- `/track/air/us/icao24-a1b2c3` — aircraft in US airspace, ICAO24 a1b2c3
- `/track/sea/global/imo-9123456` — vessel, IMO number, geo unknown
- `/track/air/ru/flight-su123` — flight SU123, Russia

**ID schemes**:
| Scheme | Domain | Format | Example | Source |
|--------|--------|--------|---------|--------|
| mmsi | sea | 9 digits | 525123456 | AIS, vessel registries |
| icao24 | air | 6 hex chars | a1b2c3 | OpenSky, ADS-B |
| imo | sea | 7 digits (IMO number) | 9123456 | Vessel registries, AIS |
| flight | air | Callsign (alphanumeric) | su123, ual456 | OpenSky, flight plans |

**Slug normalization**:
- `mmsi-{n}`: n = exactly 9 digits, leading zeros allowed
- `icao24-{h}`: h = 6 hex chars, lowercase
- `imo-{n}`: n = 7 digits (IMO ship number)
- `flight-{c}`: c = slug(callsign), lowercase, hyphens for spaces

**Fallback** — when no external ID (e.g. synthetic event):
- `event-{hash8}` — use shortHash of source, externalID, lat, lon

#### ObjectType Extension for Supply Chain

| ObjectType | TrackKind | Use case |
|------------|-----------|----------|
| military_aircraft | air | Defense/military monitoring |
| vessel | sea | Generic vessel |
| bulk_carrier | sea | Ore, grain, coal — supply chain |
| tanker | sea | Oil, LNG, chemicals |
| cargo | sea | General cargo |
| ore_carrier | sea | Mineral bulk (nickel, lithium ore) |
| ground_vehicle | ground | Trucks, convoys |

#### Taxonomy Registry Updates

1. **canonicalPrefixes** (HorizonTaxonomy): add `/track/` to seed
2. **EntityPathTaxonomy** (entity_sector.go): add Track entries for LLM/classifier:
   - `/Track/Sea/Vessel` — vessels
   - `/Track/Sea/BulkCarrier` — bulk carriers
   - `/Track/Sea/Tanker` — tankers
   - `/Track/Air/Aircraft` — aircraft
   - `/Track/Air/MilitaryAircraft` — military aircraft
3. **isAllowedTrackObjectType**: extend with `bulk_carrier`, `tanker`, `cargo`, `ore_carrier`

#### Lookup by Identifier

With ID-in-path, fragment lookup becomes:
- By MMSI: query fragments where `canonicalPath` contains `/track/sea/` and `mmsi-{mmsi}`, or `Identifiers[externalTrackId] = mmsi`
- By ICAO24: query `canonicalPath` contains `/track/air/` and `icao24-{hex}`

`Identifiers` map on fragment should store `externalTrackId` → MMSI/ICAO24 for fast lookup regardless of path format.

---

## Visualization & UX — Design for Non-Expert Users

### Design Philosophy

The user is a business analyst, procurement manager, or C-level executive. They don't know what "propagation axis" or "amplitude 0.72" means. They think in:
- **Causes and consequences** — "this happened, so that will happen"
- **Who is affected** — companies, products, routes
- **How badly** — "a little" / "significantly" / "critically"
- **What can I do** — actionable next steps
- **How do you know this** — show me the receipts

Every piece of UI must answer one of these five questions. If it doesn't — it shouldn't be visible.

---

### 1. Question Bar — The Entry Point

Centered at the bottom of the canvas. Always visible. The single most important UI element.

```
┌────────────────────────────── Graph Canvas ────────────────────────────┐
│                                                                        │
│                        [ ... graph nodes ... ]                         │
│                                                                        │
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │  What do you want to know?                              ⌘K  ⏎ │   │
│   └────────────────────────────────────────────────────────────────┘   │
│   Suggested:  How stable are our lithium supplies?                     │
│               What happens if EU imposes new tariffs?                  │
│               Where are the single points of failure?                  │
└────────────────────────────────────────────────────────────────────────┘
```

**Key UX decisions**:
- Placeholder text is a *question*, not a command — "What do you want to know?" not "Ask about this lens..."
- **Suggested questions** are auto-generated from lens context (not query history):
  - From `LensIntent` questions (user-defined)
  - From detected hotspots ("Your lithium supply chain has 2 single points of failure")
  - From recent signals ("New Indonesian mining tax announced 3h ago — ask about impact?")
- Typing shows **instant autocomplete** from existing DomainAnswerSnapshot theses — if the system already has an analysis, surface it before calling LLM
- `⌘K` shortcut, but also just clicking anywhere on the bar

---

### 2. Answer Flow — Progressive Storytelling

The answer doesn't dump everything at once. It tells a **story in layers**.

#### Stage 1: Thinking indicator (0-1s)

Graph dims slightly. A subtle shimmer travels along edges from the "cause" node outward — the user sees the system "tracing" the impact in real time. Below the question bar:

```
   ┌────────────────────────────────────────────────────────────────┐
   │  What do you want to know?                              ⌘K  ⏎ │
   └────────────────────────────────────────────────────────────────┘
   ● ● ●  Tracing impact through 47 connected entities...
```

This isn't fake — it's the actual shadow propagation running. The shimmer follows real BFS traversal order.

#### Stage 2: The headline answer (1-2s)

Answer panel slides up. First thing visible: **one sentence**, large font, no jargon.

```
┌────────────────────────────────────────────────────────────────────────┐
│                                                                        │
│  ┌─ Answer ─────────────────────────────────────────────────────────┐  │
│  │                                                                   │  │
│  │  A 5% VAT increase in Indonesia would raise your battery cell     │  │
│  │  costs by an estimated 2-4%, mainly through nickel.               │  │
│  │                                                                   │  │
│  └───────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│   ┌────────────────────────────────────────────────────────────────┐   │
│   │  What do you want to know?                              ⌘K  ⏎ │   │
│   └────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────┘
```

At the same time, the graph highlights the **impact chain** — just the path that matters, not the whole graph.

#### Stage 3: The story unfolds (2-3s)

Below the headline, **cards appear one by one** (staggered animation, 200ms apart). Each card answers one natural question:

```
┌─ Answer ──────────────────────────────────────────────────────────────┐
│                                                                        │
│  A 5% VAT increase in Indonesia would raise your battery cell          │
│  costs by an estimated 2-4%, mainly through nickel.                    │
│                                                                        │
│  ┌─ How does it spread? ──────────────────────────────────────────┐   │
│  │                                                                 │   │
│  │  Indonesia     →    PT Vale    →   Nickel   →   CATL   → Cells │   │
│  │  tax policy        Indonesia       Ore          Morowali        │   │
│  │  ● cause           ●● high        ●● high      ● moderate      │   │
│  │                                                                 │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─ What's happening on the ground? ─────────────────────────────┐    │
│  │  🚢 3 bulk carriers en route from Sulawesi (6h ago)            │   │
│  │  📰 Export quota review announced (projected, yesterday)       │   │
│  │  🛰 Mining activity in Atacama normal (satellite, 2h ago)      │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─ What can you do? ────────────────────────────────────────────┐    │
│  │  1. Negotiate advance contracts with SQM (alternative supplier) │   │
│  │  2. Increase safety stock of cathode materials (~6 week buffer) │   │
│  │  3. Monitor Indonesian parliament vote on mining quotas         │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  ┌─ What don't we know? ─────────────────────────────────────────┐    │
│  │  ⚠ CATL Morowali actual throughput — last reported Q2 2025     │   │
│  │  ⚠ Indonesia 2026 mining quota allocations — pending           │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

**The four cards are always the same structure**:
1. **"How does it spread?"** — The impact chain, visual, left-to-right
2. **"What's happening on the ground?"** — Real-world evidence (leaf signals)
3. **"What can you do?"** — Actionable alternatives
4. **"What don't we know?"** — Transparency about gaps

The user never sees: axis names, amplitude numbers, canonical paths, fragment IDs, confidence floats.

---

### 3. Impact Chain — The Domino Visualization

This is the core visual. It replaces "propagation trace" with something anyone understands: **dominoes falling**.

```
  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
  │ Indonesia│───▶│ PT Vale  │───▶│  Nickel  │───▶│   CATL   │───▶│ Battery  │
  │ Tax      │    │Indonesia │    │   Ore    │    │ Morowali │    │  Cells   │
  │ Policy   │    │          │    │          │    │          │    │          │
  │          │    │          │    │          │    │          │    │          │
  │  ██████  │    │  █████░  │    │  ████░░  │    │  ██░░░░  │    │  █░░░░░  │
  │  cause   │    │  high    │    │  high    │    │ moderate │    │   low    │
  └──────────┘    └──────────┘    └──────────┘    └──────────┘    └──────────┘
       🏛              🏭              📦              🏭              📦
```

**Design rules**:
- **Left-to-right** = cause → effect (natural reading direction)
- **Fill bar** at the bottom of each card = impact strength. Not a number — a visual fill. Full = severe, empty = minimal. Color gradient: red → amber → green
- **Icons** are entity type icons from the existing node type system (gov = 🏛, org = 🏭, product = 📦)
- **Labels** are human names, never canonical paths. "PT Vale Indonesia", not "/org/id/pt-vale-indonesia"
- **Word labels** for impact: "cause", "high", "moderate", "low", "minimal" — never 0.72
- **Arrows** between cards animate sequentially (left to right, 150ms stagger) — the user literally sees the "domino effect" play out
- **Clicking a card** pans the main graph to that node and opens its detail panel
- If the chain branches (affects multiple targets), show a **fork**:

```
                                    ┌──────────┐    ┌──────────┐
                               ───▶│   CATL   │───▶│ Battery  │
  ┌──────────┐    ┌──────────┐/    │ Morowali │    │  Cells   │
  │ Indonesia│───▶│  Nickel  │     └──────────┘    └──────────┘
  │ Tax      │    │   Ore    │
  │ Policy   │    │          │\    ┌──────────┐    ┌──────────┐
  └──────────┘    └──────────┘ ───▶│   BYD    │───▶│ EV Cars  │
                                    │          │    │          │
                                    └──────────┘    └──────────┘
```

---

### 4. Graph Canvas — "X-Ray Mode"

When a Q&A answer is active, the graph enters **X-Ray mode**: it visually separates "what matters for this answer" from "everything else".

**Affected nodes**: Full opacity, colored border glow matching impact level
- High impact: thick warm ring (amber/red), slight scale-up (1.05x)
- Moderate: medium ring (yellow)
- Low: thin ring (muted)

**Unaffected nodes**: Dim to 25% opacity, desaturate, shrink slightly (0.95x)

**Impact path edges**: Become thick, animated. A "pulse" (small dot) travels along the edge from source to target, repeating every 2s. The dot is the color of the impact type.

**Other edges**: Fade to 10% opacity — nearly invisible.

**The effect**: The full graph fades into a ghost, and only the impact story is visible. Like an X-ray — you see the skeleton (the causal chain) through the body (the full graph).

**Exit X-Ray**: Click anywhere outside the answer panel, press Escape, or ask a new question. Graph smoothly fades back (300ms transition).

---

### 5. Evidence Layer — "Show Me the Receipts"

Every claim in the answer is backed by something visible. The user can always ask "how do you know?"

#### In-answer evidence markers

Claims in the answer text have **subtle colored underlines** (not [1] [2] [3] footnotes — those are for papers, not UIs):

```
  A 5% VAT increase in Indonesia would raise your battery cell
  ─────────────────────────────────── (underline: hover → tooltip)
  costs by an estimated 2-4%, mainly through nickel.
                         ────                ──────
```

- Hover over underlined phrase → tooltip appears showing the evidence source:
  "Based on: Indonesia nickel supply chain path (validated), PT Vale Indonesia production data (Q2 2025)"
- Click → graph pans to relevant node

#### "On the ground" card — real-world signals

This card is visually distinct from the others. It uses **a dark background with colored signal indicators**:

```
  ┌─ What's happening on the ground? ──────────────────────────┐
  │                                                             │
  │  🟢 Mining output normal          Salar de Atacama, Chile  │
  │     Satellite monitoring · 2 hours ago                      │
  │                                                             │
  │  🟡 3 vessels delayed              Sulawesi Port, Indonesia │
  │     AIS vessel tracking · 6 hours ago                       │
  │                                                             │
  │  🔴 Export quota review            Government of Indonesia  │
  │     Reuters, Financial Times · yesterday                    │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
```

- Traffic light colors: 🟢 normal / 🟡 watch / 🔴 concern
- Source type in plain language: "Satellite monitoring", "AIS vessel tracking", "Reuters"
- Time in human terms: "2 hours ago", "yesterday", not ISO timestamps
- Click any row → graph pans to that leaf node, shows its detail card with full signal history

#### Leaf node badges on graph

When Q&A is active, leaf nodes in the impact chain show **live data badges**:

```
  ┌─────────────────────┐
  │  Salar de Atacama   │
  │  Lithium Deposit    │
  │                     │
  │  🟢 mining normal   │ ← inline badge, updates in real time
  └─────────────────────┘
```

Badge text is one line, human-readable. Updates if new signal arrives during session.

---

### 6. Confidence & Uncertainty — Honest, Not Scary

Never show "confidence: 0.78". Instead:

**Answer certainty indicator** — a small label next to the headline:

| Internal score | User sees | Visual |
|---------------|-----------|--------|
| > 0.8 | "High confidence" | Green dot |
| 0.5-0.8 | "Moderate confidence — some data gaps" | Amber dot |
| < 0.5 | "Low confidence — significant unknowns" | Red dot, "What don't we know?" card is expanded by default |

**Data gaps card** uses constructive language:

```
  ┌─ What don't we know? ──────────────────────────────────────┐
  │                                                             │
  │  This answer would be stronger with:                        │
  │                                                             │
  │  • CATL Morowali plant throughput (last reported Q2 2025)   │
  │    → Would improve cost pass-through estimate               │
  │                                                             │
  │  • Indonesia 2026 mining quota allocations                  │
  │    → Key policy decision, expected Q1 2026                  │
  │                                                             │
  │  ┌──────────────────────────────────────┐                   │
  │  │  Scout for this data automatically   │  ← action button  │
  │  └──────────────────────────────────────┘                   │
  └─────────────────────────────────────────────────────────────┘
```

- Each gap explains *why* it matters ("Would improve cost pass-through estimate")
- "Scout for this data" button triggers a scouting job for the missing info — closes the loop

---

### 7. Follow-Up — Conversational, Not Stateless

After an answer, the question bar stays focused. The context carries over:

```
  User: "How would a VAT increase in Indonesia affect battery costs?"
  [Answer shown]

  User: "What about if we switch to Australian nickel?"
  [System understands "switch" = alternative sourcing, "Australian nickel" =
   re-run simulation with /deposit/nickel/australia as replacement seed]
```

Previous Q&A cards **stack** in the answer panel (collapsed, expandable):

```
  ┌─ Previous ──────────────────────────────────────────────────┐
  │  ▸ How would a VAT increase in Indonesia affect...          │
  └─────────────────────────────────────────────────────────────┘
  ┌─ Answer ────────────────────────────────────────────────────┐
  │  Switching to Australian nickel (BHP Nickel West, Ravensthorpe) │
  │  would reduce Indonesia exposure but increase logistics cost    │
  │  by ~8% due to longer shipping routes...                       │
  │                                                                 │
  │  [New impact chain showing Australia path]                      │
  │  [Side-by-side comparison with previous answer]                 │
  └─────────────────────────────────────────────────────────────────┘
```

**Comparison mode**: When a follow-up is about an alternative, show the two impact chains **side by side**:

```
  Current: Indonesia → PT Vale → Nickel → CATL → Cells
           ██████     █████░    ████░░   ██░░░   █░░░░
           cause      high      high     moderate low

  Alternative: Australia → BHP → Nickel → CATL → Cells
               ░░░░░░     ░░░░░  ░░░░░░   █░░░░  ░░░░░
               (no risk)  (ok)    (ok)     (moderate: logistics) (ok)
```

The user instantly sees: Indonesia path has supply risk, Australia path has logistics cost — which trade-off do they prefer?

---

### 8. Mobile / Narrow Screen Adaptation

On narrow screens (< 1024px), the answer panel takes **full width** and the graph goes behind it (still visible, dimmed, at top 30% of screen). Impact chain becomes **vertical** (top-to-bottom scroll):

```
┌──────────────────────────┐
│  [Graph, dimmed, 30%]    │
│  [Impact path visible]   │
├──────────────────────────┤
│                          │
│  Answer headline...      │
│                          │
│  ┌─ Impact chain ──────┐ │
│  │ Indonesia Tax ──────│ │
│  │       ↓             │ │
│  │ PT Vale Indonesia   │ │
│  │       ↓             │ │
│  │ Nickel Ore          │ │
│  │       ↓             │ │
│  │ CATL Morowali       │ │
│  └─────────────────────┘ │
│                          │
│  ┌─ On the ground ────┐  │
│  │ ...                 │  │
│  └─────────────────────┘  │
│                          │
│  [Question bar]          │
└──────────────────────────┘
```

---

### 9. Human-Language Translation Layer

The system internally operates on axes, canonical paths, amplitudes. The UI layer translates everything:

| Internal | User sees |
|----------|-----------|
| `physical_flow/supply` amplitude 0.7 | "High supply disruption risk" |
| `legal/compliance` amplitude 0.4 | "Moderate regulatory exposure" |
| `financial/settlement` amplitude 0.8 | "Severe payment disruption risk" |
| `logistics/latency` amplitude 0.3 | "Some shipping delays possible" |
| `/org/id/pt-vale-indonesia` | "PT Vale Indonesia" |
| `/product/raw/mineral/nickel-ore/extraction` | "Nickel Ore" |
| `SUPPLIES_TO` edge | "supplies" (or "is a supplier of") |
| `OWNS` edge | "owns" (or "is owned by") |
| Confidence 0.78 | "Moderate confidence" |
| 3 hops | "3 steps in the supply chain" |
| `modality: projected` | "Expected" |
| `modality: actual` | "Confirmed" |

This translation happens in a `humanize()` layer on the frontend. No raw data ever reaches the user's eyes.

### 10. Color Language — Impact, Not Axes

The old design used axis-dependent colors (amber for physical, blue for legal, etc.). For non-experts this is confusing — they don't think in axes.

**New rule**: Color = severity of impact, not type of impact.

| Severity | Color | Ring | Fill bar | Text label |
|----------|-------|------|----------|------------|
| Critical | `#DC2626` (red) | Thick, pulsing | Full | "Critical" |
| High | `#F59E0B` (amber) | Thick, steady | 75% | "High" |
| Moderate | `#EAB308` (yellow) | Medium | 50% | "Moderate" |
| Low | `#22C55E` (green) | Thin | 25% | "Low" |
| Unaffected | `#94A3B8` (grey) | None | Empty | — |

One scale. Red = bad, green = fine. Everyone understands this.

The *type* of impact (supply, legal, financial) is communicated through **text labels and icons**, not colors:
- 📦 Supply disruption
- ⚖️ Regulatory change
- 💰 Financial risk
- 🚛 Logistics delay
- ⚙️ Operations risk
- 📰 Reputation impact
- ⚔️ Geopolitical risk

---

### Component Structure (Technical)

Analysis view layout:

```tsx
<QAPanel>
  <QAInput
    placeholder="What do you want to know?"
    suggestions={autoSuggestions}      // From lens context + hotspots
    onSubmit={handleQuestion}
    shortcut="⌘K"
  />

  {qaState === 'tracing' && (
    <QATracingIndicator
      entityCount={tracingProgress.entityCount}
    />
  )}

  {answer && (
    <QAAnswerPanel>
      <QAHeadline
        text={answer.headline}
        certainty={humanizeCertainty(answer.confidence)}
      />

      <QAImpactChain
        steps={answer.impactChain}         // Humanized: name, icon, severity label, fill %
        onStepClick={panToNode}
        animated={true}                     // Staggered domino animation
      />

      <QAGroundTruth
        signals={answer.leafEvidence}       // Humanized: traffic light, source, time ago
        onSignalClick={panToLeafNode}
      />

      <QAActions
        items={answer.alternatives}         // "What can you do?" cards
      />

      {answer.dataGaps.length > 0 && (
        <QADataGaps
          gaps={answer.dataGaps}            // Constructive: "would be stronger with..."
          onScoutClick={triggerScoutJob}
          expanded={answer.confidence < 0.5}
        />
      )}
    </QAAnswerPanel>
  )}

  {previousAnswers.length > 0 && (
    <QAPreviousAnswers
      answers={previousAnswers}
      onExpand={showPreviousAnswer}
      comparisonMode={isFollowUp}
    />
  )}
</QAPanel>
```

### Axis Color System (Internal Use Only)

Retained for engineering/debug views, but never shown to end users:

| Axis | Internal Color | User sees instead |
|------|---------------|-------------------|
| `physical_flow/supply` | `#F59E0B` | 📦 "Supply disruption" + severity color |
| `physical_flow/demand` | `#F97316` | 📦 "Demand shift" + severity color |
| `legal/access` | `#3B82F6` | ⚖️ "Regulatory barrier" + severity color |
| `legal/compliance` | `#6366F1` | ⚖️ "Compliance exposure" + severity color |
| `financial/settlement` | `#10B981` | 💰 "Payment risk" + severity color |
| `financial/credit` | `#14B8A6` | 💰 "Credit risk" + severity color |
| `logistics/capacity` | `#8B5CF6` | 🚛 "Route disruption" + severity color |
| `logistics/latency` | `#A855F7` | 🚛 "Shipping delay" + severity color |
| `operational/continuity` | `#EF4444` | ⚙️ "Operations disruption" + severity color |
| `info/uncertainty` | `#94A3B8` | 📰 "Uncertain regulatory landscape" + severity color |
| `info/reputation` | `#64748B` | 📰 "Reputation risk" + severity color |
| `geo/conflict` | `#DC2626` | ⚔️ "Geopolitical instability" + severity color |

---

## Data Model

### New: QASession

```go
// QASession tracks a question-answer exchange within a lens.
// Stored in Firestore: projects/{pid}/lenses/{lid}/qa_sessions/{id}
type QASession struct {
    ID        string    `json:"id" firestore:"id"`
    LensID    string    `json:"lensId" firestore:"lensId"`
    TenantID  string    `json:"tenantId" firestore:"tenantId"`
    ProjectID string    `json:"projectId" firestore:"projectId"`

    // Question
    RawQuestion        string           `json:"rawQuestion" firestore:"rawQuestion"`
    NormalizedQuestion string           `json:"normalizedQuestion" firestore:"normalizedQuestion"`
    IntentID           string           `json:"intentId,omitempty" firestore:"intentId,omitempty"` // Link to LensIntent

    // Decomposition
    Plan *QuestionPlan `json:"plan,omitempty" firestore:"plan,omitempty"`

    // Simulation
    SimulationResult *SimulationResultSummary `json:"simulationResult,omitempty" firestore:"simulationResult,omitempty"`

    // Answer
    Answer     *QAAnswer `json:"answer,omitempty" firestore:"answer,omitempty"`
    AnsweredAt *time.Time `json:"answeredAt,omitempty" firestore:"answeredAt,omitempty"`

    // Lifecycle
    Status    string    `json:"status" firestore:"status"` // pending, decomposing, simulating, synthesizing, answered, failed
    CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

    // Performance
    DecomposeLatencyMs int `json:"decomposeLatencyMs,omitempty" firestore:"decomposeLatencyMs,omitempty"`
    SimulateLatencyMs  int `json:"simulateLatencyMs,omitempty" firestore:"simulateLatencyMs,omitempty"`
    SynthesizeLatencyMs int `json:"synthesizeLatencyMs,omitempty" firestore:"synthesizeLatencyMs,omitempty"`
    TotalLatencyMs     int `json:"totalLatencyMs,omitempty" firestore:"totalLatencyMs,omitempty"`
}

// SimulationResultSummary is a Firestore-safe summary (not full trace).
type SimulationResultSummary struct {
    AffectedNodeCount int              `json:"affectedNodeCount" firestore:"affectedNodeCount"`
    TopNodeImpacts    []NodeImpact     `json:"topNodeImpacts" firestore:"topNodeImpacts"`       // Top 20
    LeafEvidenceCount int              `json:"leafEvidenceCount" firestore:"leafEvidenceCount"`
    ChainImpacts      []ChainImpact    `json:"chainImpacts" firestore:"chainImpacts"`
    SimulatedAxes     []string         `json:"simulatedAxes" firestore:"simulatedAxes"`
}
```

### LensIntent Update

Extend existing `LensIntent` with link to QASession:

```go
type LensIntent struct {
    ID           string    `json:"id,omitempty" firestore:"id,omitempty"`
    Question     string    `json:"question" firestore:"question"`
    CreatedAt    time.Time `json:"createdAt,omitempty" firestore:"createdAt,omitempty"`
    Status       string    `json:"status,omitempty" firestore:"status,omitempty"`
    QASessionID  string    `json:"qaSessionId,omitempty" firestore:"qaSessionId,omitempty"` // NEW
}
```

---

## API

```
POST /api/horizon/lenses/{lensId}/qa
Body: { "question": "How would a 5% VAT increase..." }
Response: SSE stream

SSE events:
  event: status
  data: { "status": "decomposing", "sessionId": "qa_abc123" }

  event: status
  data: { "status": "simulating" }

  event: highlights
  data: { "highlightedNodeIds": [...], "tracePaths": [...] }

  event: answer_chunk
  data: { "text": "A 5% VAT increase in Indonesia would directly " }

  event: answer_chunk
  data: { "text": "impact nickel ore costs through PT Vale Indonesia [1]" }

  event: answer_complete
  data: { full QAAnswer JSON }
```

**Why SSE**:
1. `status` events update UI instantly (show "Simulating..." spinner)
2. `highlights` arrives before answer text → graph lights up while user reads
3. `answer_chunk` streams text word-by-word → perceived latency drops to ~800ms
4. Single HTTP connection, no WebSocket setup overhead

### Frontend Integration

```tsx
const askQuestion = async (question: string) => {
  setQAState('decomposing');

  const eventSource = new EventSource(
    `/api/horizon/lenses/${lensId}/qa?question=${encodeURIComponent(question)}`
  );

  eventSource.addEventListener('status', (e) => {
    setQAState(JSON.parse(e.data).status);
  });

  eventSource.addEventListener('highlights', (e) => {
    const { highlightedNodeIds, tracePaths } = JSON.parse(e.data);
    applyQAHighlights(highlightedNodeIds, tracePaths);  // Animate graph
  });

  eventSource.addEventListener('answer_chunk', (e) => {
    appendAnswerText(JSON.parse(e.data).text);  // Stream to panel
  });

  eventSource.addEventListener('answer_complete', (e) => {
    setQAAnswer(JSON.parse(e.data));  // Full structured answer
    setQAState('answered');
    eventSource.close();
  });
};
```

---

## Edge Cases

| Case | Handling |
|------|----------|
| Question not related to lens scope | Decomposer returns `AnswerMode: "out_of_scope"` → UI: "This question doesn't seem related to the current lens. Try rephrasing or expanding the scope." |
| No propagation paths (disconnected graph) | Skip simulation, answer from artifacts + leaf evidence only. Flag as low confidence. |
| All leaf signals stale (>7d old) | Include in evidence but mark `[stale]`. DataGap: "Real-time signals unavailable for {nodes}". Ensure feed adapters are wired and DataSource.URI points to our proxies. |
| Very large lens (500+ nodes) | Phase 2: only propagate within 4 hops of seeds. Phase 3: top-20 nodes in context. |
| Follow-up question | Carry previous QASession context. "And what about cobalt?" → reuse previous signals, adjust TargetFilter. |
| No MaterializedArtifacts | Answer from raw graph edges. Lower confidence. DataGap: "No validated supply chain paths available." |

---

## Implementation Order

| Step | Component | Effort | Depends On |
|------|-----------|--------|------------|
| 1 | `QASession` model + Firestore repo | S | — |
| 2 | `QuestionDecomposer` (LLM + templates) | M | — |
| 3 | Shadow `PropagateSignalFromSeeds` (in-memory, no persist) | M | — |
| 4 | `LeafEvidenceCollector` | S | — |
| 5 | `AnswerContextBuilder` (assembles LLM prompt) | M | 2, 3, 4 |
| 6 | `AnswerSynthesizer` (LLM + streaming) | M | 5 |
| 7 | SSE API endpoint | S | 6 |
| 8 | Frontend: Lens view switcher (Graph · Map · Analysis), graph persistence across views | S | — |
| 9 | Frontend: QAPanel + QAInput + QAAnswer (Analysis view) | M | 7 |
| 10 | Frontend: graph highlights + animations | M | 7 |
| 11 | Caching + warm prefetch | S | 6 |
| 12 | **Feed adapters** (discovery/adapters): OpenSky, USGS, UCDP, FIRMS, AIS | M | — |
| 13 | `ProcessFeedEvents` + adapter injection in IngestHorizonMonitorFeed | S | 12 |
| 14 | **Track taxonomy**: ID-in-path (`/track/{kind}/{geo}/{id_scheme}-{id}`), canonicalPrefixes, EntityPathTaxonomy, ObjectType extension | S | — |

Steps 1-4 are independent and can be built in parallel. Step 8 (view switcher) can be done early — it defines the lens shell. Step 12 (adapters) unblocks direct use of our backend proxies as DataSource.URI. Step 14 enables lookup of vessels/aircraft by MMSI/ICAO24 in canonical path.
