# Risk & Opportunity Architecture
## Horizon Knowledge Graph: Signal Propagation System

**Version**: 2.1 (Canonical Contracts)
**Date**: 2026-01-18
**Status**: Architectural Design

**Implementation Status**:
- ✅ **Canonical Name Rules Implemented** (2026-01-18): Organization legal form normalization, person honorifics, composite geo support, and privacy-compliant person paths are implemented in `backend/internal/taxonomy/paths.go`

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Foundation: Canonical Paths & Taxonomy](#foundation-canonical-paths--taxonomy)
3. [Canonical Contracts](#canonical-contracts)
4. [Signal Lifecycle](#signal-lifecycle)
5. [Propagation](#propagation)
6. [Composition & Triggers](#composition--triggers)
7. [Lazy Interpretation](#lazy-interpretation)
8. [Use Cases & Examples](#use-cases--examples)
9. [Implementation](#implementation)
10. [Implementation Status](#implementation-status)
11. [Roadmap](#roadmap)
12. [Technical Decisions](#technical-decisions)
13. [Glossary](#glossary)

---

## Executive Summary

### Vision

Transform the Horizon Knowledge Graph into a **signal propagation system** that:
- Extracts **signals** from events using a finite set of quasi-physical **axes**
- Propagates signals through the graph using **deterministic axis-based profiles**
- Stores metrics on nodes with **bounded provenance** (top-K contributions)
- Interprets risks/opportunities **lazily** only when needed (thresholds, VIP nodes, or queries)

### Key Innovation: Actor–Signal–Medium Model

| Component | Definition | Example |
|-----------|------------|---------|
| **Actor** | Canonical entity that causes or is affected by signal | `/gov/ir/civil-aviation`, `/org/ae/flydubai-llc` |
| **Signal** | Quasi-physical impulse along one of 12 axes | `axis: logistics/capacity`, `polarity: risk`, `amplitude: 0.8` |
| **Medium** | Graph edges + propagation profile that carries signal | `DEPENDS_ON[facet=logistics]` with damping 0.7, maxHops 3 |

### Core Principles

1. **Finite axis basis**: All signals map to exactly one of 12 quasi-physical axes (no free-form categories)
2. **Geo in payload, not in metric path**: `locationPath` and `constraints.destinationPaths` carry geography; metric paths are universal (`/metric/{polarity}/{axis}/{mechanism?}`)
3. **Risk and Opportunity are separate**: Different `polarity` values, **never balanced against each other** (sugar ≠ salt)
4. **Bounded propagation**: Strong edges first, top-K contributions per node, threshold pruning, cycle guard
5. **Lazy interpretation**: LLM only at endpoints (signal extraction + human-readable output)

### System Flow

```
Event → SignalDescriptor(s) → Metric Updates on Seed Nodes → Propagation → Node Metrics → Triggers → Lazy Interpretation
        ↑ LLM/Rules           ↑ axis-based                   ↑ deterministic              ↑ thresholds  ↑ LLM when needed
```

---

## Foundation: Canonical Paths & Taxonomy

### Canonical Path Philosophy

Canonical paths are **Horizon-first identifiers** that serve dual purposes:
1. **Stable addressing**: Deterministic, human-readable IDs
2. **Taxonomic classification**: Structure encodes hierarchy

**Core Design Principles:**
- **Deterministic structure**: Fixed segment counts (e.g., `/loc/{cc}/{admin1}/{city}` always 4 segments)
- **Geographic scoping**: Second token is `{geo}` (geographic jurisdiction) - ISO3166-1 alpha2 country code or composite (e.g., `/org/{geo}/{slug}`, `/gov/{geo}/{slug}`)
- **Composite jurisdictions**: Second token `{geo}` may be composite: multiple ISO3166-1 alpha2 codes joined by `-` (e.g., `tr-sy` for Turkey-Syria cross-border entities)
- **Hierarchical when useful**: Product taxonomy, location hierarchy
- **Pragmatic over elegant**: `/loc/ae/na/na` is "boring" but battle-tested

### Why `/loc/ae/na/na` is Brilliant

This appears verbose ("UAE / not-applicable / not-applicable"), but it's **deterministic perfection**:
- **Zero special cases**: Always exactly 4 segments, always parseable the same way
- **Self-documenting**: `na` explicitly signals "country-level, no region/city"
- **Index-friendly**: Works perfectly with prefix indexes, LIKE patterns, sorting
- **Battle-tested**: Years of production use, zero bugs from edge cases

**Philosophy**: Boring technology wins. Consistency beats elegance.

### Composite Jurisdictions

**Rule**: When an entity operates across multiple countries or represents a cross-border relationship, the second token `{geo}` may be a composite of multiple ISO3166-1 alpha2 codes joined by `-`.

**Format**: `{iso2}-{iso2}[-{iso2}...]` (2-3 countries typically, sorted lexicographically)

**Use Cases:**
- **Cross-border organizations**: Joint ventures, alliances operating in multiple jurisdictions
- **Intergovernmental bodies**: Regional organizations (GCC, NAFTA, etc.)
- **Cross-border locations**: Border regions, shared territories
- **Transnational markets**: Markets spanning multiple countries
- **Cross-border programs**: Humanitarian corridors, infrastructure projects

**Examples:**
- `/org/tr-sy/logistics-network-llc` - Turkey-Syria cross-border logistics entity
- `/gov/ae-sa-om/gcc-council` - GCC regulatory body (UAE, Saudi, Oman)
- `/loc/us-mx/na/na` - US-Mexico border region
- `/market/tr-sy/cross-border-trade` - Turkey-Syria trade market

**Validation Rules:**
- Each component must be valid ISO3166-1 alpha2 code
- Components sorted lexicographically (e.g., `ae-sa`, not `sa-ae`)
- Maximum 3 countries per composite (to avoid explosion)
- `na` cannot be part of composite (use single `na` for unknown jurisdiction)

**Indexing Behavior:**
- Composite paths index under **all component countries** for filtering
- Example: `/org/tr-sy/entity` appears in queries for both `tr` and `sy`

### Organization Name Canonization Rules

**Purpose**: Ensure consistent, predictable organization names in canonical paths while preserving legal accuracy.

#### Rule 1: Legal Form Suffix — Canonical Normalization

**Problem**: The same legal form may appear in different languages/scripts:
- ТОО "Ромашка" (Kazakh/Russian) = Romashka LLC (English) = **same entity, different representations**
- АО "Газпром" (Russian) = Gazprom JSC (English) = **same entity**

**Solution**: Normalize ALL legal forms to a **canonical English-based form** in lowercase.

**Format**: Organization name slug + `-` + canonical legal form (lowercase)

**Examples:**
- `/org/us/tesla-inc`
- `/org/de/siemens-jsc`
- `/org/ru/gazprom-pjsc` (not `pao`)
- `/org/kz/romashka-llc` (not `too`)

#### Legal Form Normalization Table

| National Form | Language/Country | Canonical Form | Description |
|---------------|------------------|----------------|-------------|
| LLC, ТОО, ООО, GmbH, SARL, SRL | Various | `llc` | Limited Liability Company equivalents |
| JSC, АО, AG, SA, SpA, AS, AB | Various | `jsc` | Joint Stock Company equivalents |
| ПАО | Russian | `pjsc` | Public Joint Stock Company |
| Ltd, Limited | English | `ltd` | Limited |
| Inc, Incorporated | English | `inc` | Incorporated |
| Corp, Corporation | English | `corp` | Corporation |
| PLC | English | `plc` | Public Limited Company |

#### Rule 2: Mandatory Legal Form

**Requirement**: Legal form **must** be included in the canonical path if it is known, even if it's redundant or commonly omitted in casual references.

#### Rule 3: Latin Script & Transliteration

**Primary Rule**: Organization names and legal forms are **always** represented in Latin script in canonical paths.

**Transliteration Standards:**
- **Cyrillic → Latin**: ISO 9:1995 (GOST 7.79-2000)
- **Arabic → Latin**: ALA-LC Romanization Tables
- **Chinese → Latin**: Pinyin (Hanyu Pinyin)
- **Japanese → Latin**: Modified Hepburn romanization

### Person Name Canonization Rules

#### Rule 1: Honorific/Title Prefix

**Format**: `/person/{honorific?}-{name-slug}`

**Position**: Honorific or title **always** appears at the **beginning** of the slug (before the name).

**Formatting**: Lowercase, no dots, hyphenated to the name.

**Examples:**
- `/person/sheikh-tahnoon-bin-zayed`
- `/person/hrh-mohammed-bin-salman`
- `/person/he-sultan-al-jaber`
- `/person/dr-tedros-adhanom`

#### Rule 2: No Geographic Token in Person Canonical Paths

**Format**: `/person/{honorific?}-{name-slug}` (NO `{geo}` token)

**Rationale**:
1. **GDPR Compliance**: Geographic data in canonical paths creates persistent identifiers that may violate privacy regulations
2. **Privacy**: People move, have multiple citizenships
3. **Deduplication**: Geographic disambiguation handled via metadata matching, not path structure

**Geographic Data Storage**: Store in `identifiers` (nationality, residence, birthCountry), **NOT** in canonical path.

### Disambiguation Rules for Scouts/Research

**Problem**: Simple abbreviations are often ambiguous (MIT = university or Turkish intelligence agency).

**Rules:**
1. **Never resolve to canonical path based on abbreviation alone**
2. **Require jurisdiction or domain confirmation**
3. **Prefer full name in canonical path**, store abbreviation in `aliases`

### Canonical Path Structure Summary

| Namespace | Format | Geo Token? |
|-----------|--------|------------|
| `/org` | `/org/{geo}/{name-slug}-{legal-form}` | ✅ Required |
| `/gov` | `/gov/{geo}/{agency-slug}` | ✅ Required |
| `/person` | `/person/{honorific?}-{name-slug}` | ❌ **NO** (privacy) |
| `/product` | `/product/{category}/{type}/{stage}/{slug}` | ❌ No |
| `/loc` | `/loc/{cc}/{admin1}/{city}` | ✅ Required |
| `/market` | `/market/{geo}/{market-slug}` | ✅ Required |

### Canonical Path Examples

```
# Organizations
/org/us/tesla-inc
/org/kz/romashka-llc
/org/tr-sy/cross-border-logistics-llc

# Government
/gov/sa/sfda
/gov/ae-sa-om/gcc-regulatory-council

# Persons (no geo, for privacy)
/person/elon-musk
/person/sheikh-tahnoon-bin-zayed
/person/hrh-mohammed-bin-salman

# Locations
/loc/us/tx/austin
/loc/ae/na/na
/loc/tr-sy/na/na

# Products
/product/raw/mineral/extraction/lithium-ore
/product/finished/mechanical/assembly/electric-vehicle
```

### IndexPaths: Denormalized Coordinates

**Purpose**: Fast filtering without joins (up to 80 paths per fragment)

```go
indexPaths := []string{
    fragment.CanonicalPath,
    "/loc/us/tx/austin", "/loc/us/tx", "/loc/us",
    "/sphere/energy", "/sphere/manufacturing",
    "/industry/automotive/vehicles/ev",
    "/id/wikidataid/q478214",
    "/tag/public-company",
}
```

**Key insight**: This denormalization enables signal propagation to efficiently filter by sector, location, or entity type without graph traversal.

---

## Canonical Contracts

This section defines the **three core contracts** that bind together Actor, Signal, and Medium into a unified, predictable system.

### Contract 1: Signal Descriptor

A **SignalDescriptor** is the canonical object that transforms real-world events into quasi-physical impulses. It bridges semantic extraction (LLM) and deterministic propagation (algorithm).

#### Structure

```go
// SignalDescriptor is the canonical representation of a quasi-physical impulse.
// It is extracted from events and stored as OntologyFact(kind=signal).
type SignalDescriptor struct {
    // === Identity ===
    ID        string    `json:"id"`
    FactID    string    `json:"factId"`    // Reference to source OntologyFact(kind=event)
    CreatedAt time.Time `json:"createdAt"`

    // === Quasi-Physics (the "what") ===
    Axis      string  `json:"axis"`      // Primary physics axis (see Contract 2)
    Mechanism string  `json:"mechanism"` // Optional: specific mechanism (e.g., "sanctions", "airspace_closure")
    Amplitude float64 `json:"amplitude"` // [0.0, 1.0] - strength of the impulse
    Polarity  string  `json:"polarity"`  // "risk" | "opportunity"

    // === Spatial Binding (the "where") ===
    ActorPaths    []string `json:"actorPaths"`    // Who caused this (e.g., ["/gov/ir/civil-aviation"])
    SeedPaths     []string `json:"seedPaths"`     // Where to inject the impulse (initial affected nodes)
    LocationPath  string   `json:"locationPath"`  // Geographic scope (e.g., "/loc/ir/na/na")
    SpherePaths   []string `json:"spherePaths"`   // Domain/industry scope

    // === Constraints (the "conditions") ===
    Constraints SignalConstraints `json:"constraints,omitempty"`

    // === Provenance ===
    Confidence  float64   `json:"confidence"`  // [0.0, 1.0]
    SourceRefs  []string  `json:"sourceRefs"`  // Document IDs
    ObservedAt  time.Time `json:"observedAt"`  // When the event occurred
}

type SignalConstraints struct {
    DestinationPaths []string `json:"destinationPaths,omitempty"` // For route-dependent signals
    Timeframe        string   `json:"timeframe,omitempty"`        // "immediate", "short_term", "long_term"
    Modality         string   `json:"modality,omitempty"`         // "actual", "projected", "potential"
    Scope            string   `json:"scope,omitempty"`            // "primary", "secondary"
}
```

#### Canonical Metric Path

Each signal maps to a canonical metric path on affected nodes:

```
/metric/{polarity}/{axis}/{mechanism?}

Examples:
  /metric/risk/legal/access                    # Legal access restriction
  /metric/risk/legal/compliance                # Secondary compliance exposure
  /metric/risk/physical_flow/supply            # Supply disruption
  /metric/risk/logistics/capacity              # Logistics capacity constraint
  /metric/opportunity/financial/demand         # Demand increase opportunity
```

**Critical**: Metric paths are **axis-based**, not location-based. The `LocationPath` and `Constraints.DestinationPaths` in SignalDescriptor determine **where the signal seeds and propagates**, but the metric path itself remains location-agnostic.

#### Invariants

1. **Axis must be from Contract 2 basis** (no free-form axes)
2. **Amplitude ∈ [0.0, 1.0]** (normalized)
3. **Polarity ∈ {"risk", "opportunity"}** (separate fields, **not balanced**)
4. **At least one of SeedPaths or LocationPath must be present**
5. **ActorPaths should be canonical** (`/gov/...`, `/org/...`)

---

### Contract 2: Axis Basis

The **axis basis** is the finite set of quasi-physical dimensions. All signals MUST map to one of these axes. This is the "limited number of ways risks arrive" principle.

#### The 12-Axis Basis

| # | Axis | Description | Nature |
|---|------|-------------|--------|
| 1 | `legal/access` | Legal permission to operate/transact (licenses, sanctions, bans) | Binary barrier |
| 2 | `legal/compliance` | Secondary exposure risk from association with sanctioned/regulated entities | Gradient exposure |
| 3 | `financial/settlement` | Payment rails, banking access, clearing, FX (ability to move money) | Liquidity flow |
| 4 | `financial/credit` | Credit availability, counterparty risk, default exposure | Solvency |
| 5 | `physical_flow/supply` | Physical availability of goods/materials (shortage or glut) | Material flow |
| 6 | `physical_flow/demand` | Demand changes (surge or collapse) | Market force |
| 7 | `logistics/capacity` | Transportation/infrastructure capacity (routes, ports, airspace) | Throughput |
| 8 | `logistics/latency` | Delays, lead time increases (not blockage, but slowdown) | Time cost |
| 9 | `operational/continuity` | Ability to operate (strikes, failures, disasters) | Uptime |
| 10 | `info/uncertainty` | Ambiguity of rules, timelines, exceptions (regulatory fog) | Entropy |
| 11 | `info/reputation` | Public perception, ESG signals, brand impact | Sentiment |
| 12 | `geo/conflict` | Geopolitical instability, conflict zones, diplomatic friction | Environment |

#### Axis Properties

Each axis has inherent properties that inform default propagation behavior:

```go
type AxisSpec struct {
    Key              string   `json:"key"`              // e.g., "legal/access"
    Label            string   `json:"label"`            // Human-readable
    DefaultDamping   float64  `json:"defaultDamping"`   // [0.0, 1.0]
    DefaultMaxHops   int      `json:"defaultMaxHops"`   // Typical propagation depth
    PropagationMode  string   `json:"propagationMode"`  // "upstream", "downstream", "both"
    StrongEdgeTypes  []string `json:"strongEdgeTypes"`  // Default strong edges
    TransformsTo     []string `json:"transformsTo"`     // Possible transformations
}
```

#### Axis Selection Rule

When extracting signals from events:
1. **Primary axis** = the most direct physical effect
2. **Secondary axes** = consequential effects (may generate separate signals)

Example: "US sanctions on Huawei" →
- Primary: `legal/access` (direct ban)
- Secondary: `legal/compliance` (partners at risk), `physical_flow/supply` (chip shortage), `info/uncertainty` (exception ambiguity)

---

### Contract 3: Medium Specification (Axis → Propagation Profile)

The **medium** defines how each axis propagates through the graph. This binds the quasi-physics to the existing taxonomy of edges.

#### Propagation Profile Structure

```yaml
# Example: legal/access profile
legal/access:
  defaultDamping: 0.95          # Slow decay (legal effects persist)
  defaultMaxHops: 5             # Far reach (ownership chains are deep)
  propagationMode: upstream     # Flows UP ownership/control chains
  strongEdges:
    - type: OWNS
      direction: target_to_source
      weight: 1.0
    - type: CONTROLS
      direction: target_to_source
      weight: 0.9
    - type: SUBSIDIARY_OF
      direction: source_to_target
      weight: 1.0
    - type: REGULATES
      direction: source_to_target
      weight: 1.2               # Amplification
  transformations:
    - onEdgeType: PARTNERS_WITH
      toAxis: legal/compliance
      factor: 0.8
```

#### Key Profiles Summary

| Axis | Damping | MaxHops | Mode | Strong Edges |
|------|---------|---------|------|--------------|
| `legal/access` | 0.95 | 5 | upstream | OWNS, CONTROLS, SUBSIDIARY_OF, REGULATES |
| `legal/compliance` | 0.85 | 3 | both | PARTNERS_WITH, SUPPLIES_TO, DEPENDS_ON[regulatory] |
| `financial/settlement` | 0.80 | 4 | both | DEPENDS_ON[payment], OWNS, INVESTS_IN |
| `physical_flow/supply` | 0.75 | 4 | downstream | SUPPLIES_TO, DEPENDS_ON[supply], TRANSFORMS_INTO |
| `logistics/capacity` | 0.70 | 3 | downstream | DEPENDS_ON[logistics], LOCATED_IN, OPERATES_IN |
| `geo/conflict` | 0.80 | 2 | downstream | LOCATED_IN, OPERATES_IN |
| `operational/continuity` | 0.70 | 3 | upstream | OWNS, MANAGES, PRODUCES |
| `info/uncertainty` | 0.60 | 2 | both | DEPENDS_ON[regulatory], PARTNERS_WITH |

#### Edge Physics

Each edge contributes to propagation via its properties:

```go
func (e *DependencyEdge) DampedAmplitude(input float64, profile PropagationProfile) float64 {
    baseDamping := profile.DefaultDamping
    
    // Criticality reduces damping (higher criticality = less loss)
    effectiveDamping := baseDamping + (1-baseDamping) * e.Properties.Criticality * 0.5
    
    // Amplification multiplies
    amplified := input * effectiveDamping * e.Properties.Amplification
    
    // Substitutability reduces signal (easy to bypass = weaker propagation)
    if sub := e.GetSubstitutability(); sub > 0 {
        amplified *= (1 - sub*0.5)
    }
    
    return math.Min(amplified, 1.0)
}
```

#### Invariants

1. **Every axis has a defined PropagationProfile** (no implicit behavior)
2. **StrongEdges are explicit** - if not listed, edge type is weak for this axis
3. **Transformations are explicit** - axis changes only via declared rules
4. **Damping ∈ [0, 1]** - never amplifies on its own (amplification comes from edge properties)

---

### Contract Summary

| Component | Contract | Key Invariants |
|-----------|----------|----------------|
| **Actor** | Canonical paths (`/gov/...`, `/org/...`) | ISO2 jurisdiction, unique slug, dedup rules |
| **Signal** | SignalDescriptor → `/metric/{polarity}/{axis}/{mechanism?}` | Axis from basis, amplitude [0,1], polarity separate |
| **Medium** | PropagationProfile per axis | Strong edges explicit, damping/hops/direction defined |

**The system becomes predictable because:**
- Events → Signals is a **finite mapping** (any event maps to 1-N signals from 12-axis basis)
- Signals → Propagation is **deterministic** (axis + profile + edge physics)
- Propagation → Interpretation is **lazy** (only when needed, via LLM)

---

## Signal Lifecycle

### Overview

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ Event       │ →  │ Signal Extraction│ →  │ Seed Injection  │ →  │ Propagation     │
│ (OntologyFact)   │ (LLM/Rules)      │    │ (Metric Updates)│    │ (BFS by Profile)│
└─────────────┘    └──────────────────┘    └─────────────────┘    └─────────────────┘
                                                                           │
                                                                           ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────────────────┐
│ Interpretation  │ ←  │ Trigger Check   │ ←  │ Node Metrics + Provenance   │
│ (LLM, lazy)     │    │ (threshold/VIP) │    │ (bounded top-K contributions)│
└─────────────────┘    └─────────────────┘    └─────────────────────────────┘
```

### Step 1: Event Ingestion

Events arrive as `OntologyFact(kind=event)` from various sources:
- News APIs / RSS feeds
- Analyst reports
- Metric triggers (e.g., "revenue drops 30%")
- Manual analyst input

### Step 2: Signal Extraction

**Input**: `OntologyFact(kind=event)`
**Output**: 1..N `SignalDescriptor`

Extraction can be:
- **LLM-based**: For complex events requiring semantic understanding
- **Rule-based**: For well-defined patterns (e.g., "port closure" → `logistics/capacity`)

**Key decision**: Select primary axis from 12-axis basis, determine polarity, amplitude, seed paths.

### Step 3: Seed Injection

For each `SignalDescriptor`:
1. Resolve `SeedPaths` to fragments (or use `LocationPath` → LOCATED_IN edges to find entities)
2. Create/update metric on each seed node:
   - Metric path: `/metric/{polarity}/{axis}/{mechanism?}`
   - Value: `amplitude`
   - Provenance: link to SignalDescriptor

### Step 4: Propagation

Propagate from seed nodes using axis-specific `PropagationProfile`. See [Propagation](#propagation) section.

### Step 5: Node Metrics with Provenance

After propagation, each affected node has:
- **Metric values**: Map of `/metric/...` paths → amplitude
- **Provenance**: Top-K contributions per metric (source signals, paths, decay factors)

**Bounded fan-in contract**: Each `(node, metricPath)` stores at most **K contributions** (e.g., K=10). When new contribution arrives, keep top-K by amplitude.

### Step 6: Trigger & Interpretation

Triggers evaluate nodes for interpretation (see [Composition & Triggers](#composition--triggers)).

---

## Propagation

### Core Principles

1. **Strong edges first**: Propagation follows `strongEdges` from the axis profile
2. **Weak edges on-demand**: Non-strong edges can be explored via explicit query, not automatic propagation
3. **Bounded traversal**:
   - `maxHops` from profile
   - Threshold stop: don't propagate below minimum amplitude (e.g., 0.05)
   - Cycle guard: skip if already visited with equal-or-stronger amplitude
   - Top-K per node: bound incoming contributions

### Algorithm (Conceptual)

```
propagate(signal, seedNodes, profile):
    queue = [(node, signal.amplitude, 0, [node]) for node in seedNodes]
    visited = {}  # node → max amplitude seen
    
    while queue not empty:
        (node, amplitude, hops, path) = queue.pop()
        
        # Threshold stop
        if amplitude < MIN_THRESHOLD:
            continue
        
        # Cycle guard (allow revisit only if stronger)
        if visited[node] >= amplitude:
            continue
        visited[node] = amplitude
        
        # Hop limit
        if hops >= profile.maxHops:
            continue
        
        # Update node metric (with bounded provenance)
        updateNodeMetric(node, signal.metricPath, amplitude, path)
        
        # Find neighbors via strong edges
        for edge in getStrongEdges(node, profile):
            if matchesDirection(edge, profile.propagationMode):
                nextAmplitude = edge.dampedAmplitude(amplitude, profile)
                
                # Check for axis transformation
                nextAxis = applyTransformation(signal.axis, edge, profile)
                
                queue.push((edge.target, nextAmplitude, hops+1, path + [edge.target]))
```

### Complexity Contracts

| Contract | Description |
|----------|-------------|
| **Bounded hops** | `maxHops` per axis profile (typically 2-5) |
| **Threshold pruning** | Stop propagation when amplitude < 0.05 |
| **Cycle guard** | Node visited once per wave (strongest wins) |
| **Top-K provenance** | Max K contributions per (node, metricPath) |
| **Strong edges only** | Weak edges require explicit query |

### Why This Makes System Observable

- **MVP-friendly**: You can see exactly which edges carried the signal
- **Debuggable**: Trace any metric value back to source events via provenance
- **Tunable**: Adjust profile parameters (damping, maxHops) and observe effect
- **Predictable**: No "magic" emergent behavior, only explicit propagation rules

---

## Composition & Triggers

### Node Scoring

Each node can compute an aggregate **risk score** or **opportunity score** from its metrics:

```go
func (n *Node) ComputeScore(polarity string) float64 {
    score := 0.0
    for metricPath, value := range n.Metrics {
        if hasPolarity(metricPath, polarity) {
            axis := extractAxis(metricPath)
            weight := getAxisWeight(axis)  // configurable per use-case
            score += value * weight
        }
    }
    return min(score, 1.0)
}
```

**Note**: This is **simple weighted aggregation**, not "wave interference" or "resonance". Complexity is optional and data-driven.

### Optional: Synergy Rules

If certain axis combinations should amplify each other, define explicit **synergy rules**:

```yaml
synergy_rules:
  - axes: [physical_flow/supply, logistics/capacity]
    condition: both > 0.4
    bonus: 0.2
    reason: "Supply shortage + logistics blockage = compounded disruption"
  
  - axes: [legal/access, info/reputation]
    condition: both > 0.5
    bonus: 0.3
    reason: "Sanctions + bad press = partners flee"
```

These are **data-driven rules**, not physics simulation.

### Triggers for Interpretation

A node is flagged for interpretation when:

| Trigger | Condition | Priority |
|---------|-----------|----------|
| **Score threshold** | `riskScore > 0.7` | HIGH |
| **Rapid change** | `deltaScore(24h) > 0.3` | MEDIUM |
| **VIP node** | Node in VIP list (Fortune 500, user portfolio) | Always if any metric > 0.1 |
| **User query** | Explicit "What are risks for X?" | On-demand |

### Trigger Output

When triggered, node is queued for lazy interpretation with:
- Current metrics (all axes)
- Top-K provenance per metric
- Score breakdown
- Trigger reason

---

## Lazy Interpretation

### Philosophy

**Don't interpret until necessary.** Most nodes have low/zero metrics. Only generate human-readable risk descriptions when:
1. Threshold exceeded
2. VIP node has any significant metric
3. User explicitly queries

### Interpretation Input

```go
type InterpretationRequest struct {
    NodeID       string
    CanonicalPath string
    NodeLabel    string
    
    // Current state
    Metrics      map[string]float64  // metricPath → amplitude
    RiskScore    float64
    OpportunityScore float64
    
    // Provenance (top-K per metric)
    Provenance   map[string][]Contribution
    
    // Trigger info
    TriggerType  string  // "threshold", "vip", "query", "delta"
    TriggerValue float64
}

type Contribution struct {
    SignalID      string
    SourceEventID string
    EventSummary  string
    Path          []string  // propagation path
    Amplitude     float64
    Hops          int
}
```

### Interpretation Output

```go
type RiskInterpretation struct {
    NodeID       string
    Summary      string   // 2-3 sentence human-readable summary
    
    // Breakdown by axis
    AxisBreakdown []AxisContribution
    
    // Top contributing events
    TopEvents    []EventProvenance
    
    // Estimated impact
    Impact       ImpactEstimate
    
    // Recommended actions
    Mitigations  []Mitigation
    
    // Meta
    Confidence   float64
    GeneratedAt  time.Time
    ExpiresAt    time.Time  // TTL for cache
}
```

### LLM Prompt Structure

```
You are interpreting risk signals for an entity in a knowledge graph.

Entity: {nodeLabel} ({canonicalPath})

Current Risk Metrics:
- /metric/risk/physical_flow/supply: 0.45
- /metric/risk/logistics/capacity: 0.30

Top Contributing Events:
1. "Iran closes airspace" (2024-01-15)
   - Path: /gov/ir/civil-aviation → /loc/ir/na/na → /org/ae/flydubai-llc
   - Contribution: 0.35 to logistics/capacity

2. "Red Sea shipping attacks" (2024-01-10)
   - Path: /loc/ye/na/na → /route/red-sea → /org/ae/flydubai-llc
   - Contribution: 0.15 to logistics/capacity

Provide:
1. Summary (2-3 sentences explaining the risk situation)
2. Impact estimate (operational, financial, timeline)
3. Recommended mitigations (2-4 actionable items)
```

---

## Use Cases & Examples

### Use Case 1: Iran Closes Airspace

**Event**: Iran announces closure of its airspace to commercial flights effective immediately.

**Step 1: Signal Extraction**

```json
{
  "id": "sig_ir_airspace_001",
  "factId": "evt_iran_airspace_closure",
  "axis": "logistics/capacity",
  "mechanism": "airspace_closure",
  "amplitude": 0.85,
  "polarity": "risk",
  "actorPaths": ["/gov/ir/civil-aviation-authority"],
  "seedPaths": [],
  "locationPath": "/loc/ir/na/na",
  "spherePaths": ["/sphere/aviation", "/sphere/logistics"],
  "constraints": {
    "destinationPaths": [],
    "timeframe": "immediate",
    "modality": "actual"
  },
  "confidence": 0.95
}
```

**Step 2: Seed Injection**

Signal seeds to all entities with `OPERATES_IN` or `DEPENDS_ON[logistics]` edges to `/loc/ir/na/na`:
- `/org/ae/emirates-llc` (hub uses Iranian airspace for Asia routes)
- `/org/ae/flydubai-llc` (routes to Central Asia cross Iran)
- `/org/qa/qatar-airways-llc` (some routes affected)

**Step 3: Propagation (Flydubai example)**

```
/loc/ir/na/na (source via geo seeding)
  /metric/risk/logistics/capacity/airspace_closure: 0.85

  ↓ OPERATES_IN (criticality=0.6, damping=0.70)

/org/ae/flydubai-llc
  /metric/risk/logistics/capacity/airspace_closure: 0.36
  
  Route-specific analysis (e.g., Kazakhstan) applies destination filters at query time.
  
  ↓ SUPPLIES_TO (weak edge, not in profile strongEdges)
  
  [Propagation stops here - weak edge not followed automatically]
```

**Impact on "Flydubai → Kazakhstan" route**:
- Metric: `/metric/risk/logistics/capacity/airspace_closure` = 0.36 (MEDIUM)
- Interpretation: "Moderate impact on Central Asia routes; 15-20% of scheduled flights may require rerouting via Turkey or Pakistan airspace"

**Why "slightly" for Kazakhstan?**
- Route-specific query applies `destinationPaths: ["/loc/kz/na/na"]` to scope impact
- Base amplitude 0.36 is below threshold for "critical"
- Alternative routes exist (Turkey corridor) → substitutability reduces impact

**Step 4: Lazy Interpretation (if triggered)**

```
Risk Interpretation for Flydubai:

Summary:
Flydubai faces MEDIUM logistics capacity constraint (0.36) from Iran's 
airspace closure. Central Asia routes (Kazakhstan, Uzbekistan) most affected
as they typically transit Iranian airspace. Alternative routing via Turkey
adds 45-60 minutes flight time and ~$2,500 fuel cost per flight.

Impact:
- Flight Operations: 15-20% of Central Asia flights delayed/rerouted
- Cost: +$50-80K daily in additional fuel
- Timeline: Until airspace reopens (unknown duration)

Mitigations:
1. Reroute via Turkey corridor (established alternative)
2. Temporarily reduce frequency on low-margin Kazakhstan routes
3. Monitor for exceptions (cargo, diplomatic flights)
```

---

### Use Case 2: US Sanctions on Huawei

**Event**: US adds Huawei to Entity List, banning technology exports.

**Step 1: Signal Extraction**

Primary signal:
```json
{
  "axis": "legal/access",
  "mechanism": "sanctions",
  "amplitude": 1.0,
  "polarity": "risk",
  "actorPaths": ["/gov/us/commerce-department"],
  "seedPaths": ["/org/cn/huawei-ltd"],
  "locationPath": "/loc/us/na/na",
  "constraints": {
    "scope": "primary"
  }
}
```

Secondary signals:
- `legal/compliance` (amplitude 0.8) for partners
- `physical_flow/supply` (amplitude 0.7) for chip supply
- `info/uncertainty` (amplitude 0.9) for exceptions/licenses

**Step 2: Propagation**

```
/org/cn/huawei-ltd (seed)
  /metric/risk/legal/access: 1.0

  ↓ OWNS (upstream, weight 1.0, damping 0.95)
  [Transformation: legal/access → legal/compliance × 0.8]

/org/cn/huawei-investment-holdings-ltd
  /metric/risk/legal/compliance: 1.0 × 0.95 × 0.8 = 0.76

  ↓ PARTNERS_WITH (weight 0.9, damping 0.85)

/org/tw/tsmc-ltd
  /metric/risk/legal/compliance: 0.76 × 0.85 × 0.9 = 0.58
```

**Step 3: Interpretation for TSMC**

```
Risk Interpretation for TSMC:

Summary:
TSMC faces MEDIUM-HIGH compliance risk (0.58) from US sanctions on Huawei.
As a key chip supplier via partnership ties, TSMC must navigate complex
export control regulations. Revenue exposure: $8-12B annually from Huawei.

Top Contributors:
1. US Entity List designation of Huawei (direct legal barrier)
2. Ownership chain propagation (compliance exposure via Huawei Holdings)

Mitigations:
1. Seek USTR exemptions for legacy chips (<7nm)
2. Diversify client base (Apple, AMD already 60% of revenue)
3. Geographic diversification (Arizona fab reduces China exposure)

Confidence: 0.85
```

---

### Use Case 3: Saudi Aramco Refinery Fire

**Event**: Major fire at Abqaiq refinery reduces oil output by 30%.

**Step 1: Signal Extraction**

```json
{
  "axis": "physical_flow/supply",
  "mechanism": "facility_damage",
  "amplitude": 0.85,
  "polarity": "risk",
  "actorPaths": [],
  "seedPaths": ["/org/sa/saudi-aramco-jsc", "/facility/sa/abqaiq-refinery"],
  "locationPath": "/loc/sa/na/na",
  "constraints": {
    "timeframe": "short_term"
  }
}
```

**Step 2: Propagation (downstream supply chain)**

```
/org/sa/saudi-aramco-jsc (seed)
  /metric/risk/physical_flow/supply: 0.85

  ↓ SUPPLIES_TO (downstream, criticality=0.75, damping=0.75)

/org/cn/sinopec-jsc (Chinese refiner)
  /metric/risk/physical_flow/supply: 0.85 × 0.75 × 0.75 = 0.48

  ↓ SUPPLIES_TO (criticality=0.65, damping=0.75)
  [Amplification: node tagged "just-in-time" → ×1.5]

/org/cn/china-eastern-airlines-jsc
  /metric/risk/physical_flow/supply: 0.48 × 0.75 × 0.65 × 1.5 = 0.35

  ↓ Threshold: 0.35 > 0.05, continue but weakening...
```

---

## Implementation

### System Components

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Event Sources                               │
│  (News APIs, Analyst Reports, Metric Triggers, Manual Input)        │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Signal Extractor                                │
│  - LLM-based or rule-based                                          │
│  - Event → 1..N SignalDescriptor                                    │
│  - Axis selection from 12-axis basis                                │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Propagation Engine                              │
│  - Axis → PropagationProfile lookup                                 │
│  - BFS on strong edges with damping                                 │
│  - Bounded: maxHops, threshold, cycle guard, top-K                  │
│  - Update node metrics + provenance                                 │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Trigger Evaluator                               │
│  - Score computation per node                                       │
│  - Threshold/VIP/Delta detection                                    │
│  - Queue nodes for interpretation                                   │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Lazy Interpreter                                │
│  - LLM-based risk/opportunity explanation                           │
│  - Uses metrics + top-K provenance                                  │
│  - Generates human-readable summary + mitigations                   │
│  - Caches with TTL                                                  │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          UI / API                                    │
│  - Node metrics dashboard                                           │
│  - Provenance explorer (trace to source events)                     │
│  - Risk/opportunity summaries                                       │
│  - "What if" queries                                                │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Implementation Points

1. **SignalDescriptor storage**: As `OntologyFact(kind=signal)` in existing ontology_facts collection
2. **Axis profiles**: Stored in `horizon_taxonomy` or dedicated `horizon_propagation_profiles` collection
3. **Node metrics**: Stored on `HorizonFragment.State` with metric paths as keys
4. **Provenance**: Stored as subcollection or embedded array on fragment (bounded to top-K)
5. **Propagation engine**: Extends existing `backend/internal/horizon/propagation/engine.go`
6. **Interpretation cache**: TTL-based cache for generated interpretations

### What Already Exists (to reuse)

| Component | Location | Status |
|-----------|----------|--------|
| Canonical path functions | `backend/internal/taxonomy/paths.go` | ✅ Implemented |
| PropagationEngine | `backend/internal/horizon/propagation/engine.go` | ✅ Base exists |
| WaveDamping | `backend/internal/horizon/propagation/wave_damping.go` | ✅ Exists |
| PropagationPolicyRule | `backend/internal/horizon/policy/propagation_policy.go` | ✅ Exists |
| SynthesisRule (event→signal) | `backend/internal/horizon/repo/synthesis_rules.go` | ✅ Exists |
| SignalTypeSpec | `backend/internal/taxonomy/runtime.go` | ✅ Exists |
| DependencyEdge with qualifiers | `backend/internal/horizon/models/dependency_edge.go` | ✅ Exists |

---

## Implementation Status

### ✅ Canonical Name Rules (Completed: 2026-01-18)

**Location**: `backend/internal/taxonomy/paths.go`

**Implemented Features**:

1. **Legal Form Normalization** (`normalizeLegalForm`, `extractLegalFormFromName`)
   - National legal forms → canonical English equivalents (lowercase)
   - Prevents duplicates: ТОО "Ромашка" = Romashka LLC → `/org/kz/romashka-llc`
   - Supports: LLC equivalents (ТОО, ООО, GmbH, SARL, SRL), JSC equivalents (АО, ПАО, AG, SA), LTD, INC, etc.

2. **Organization Canonical Paths** (`OrgCanonicalPath`)
   - Format: `/org/{geo}/{name-slug}-{legal-form}`
   - Extracts and normalizes legal form from organization name
   - Supports composite geo (`tr-sy`, `ae-sa-om`)

3. **Government Canonical Paths** (`GovCanonicalPath`)
   - Format: `/gov/{geo}/{agency-slug}`
   - Supports composite geo for intergovernmental bodies

4. **Person Canonical Paths** (`PersonCanonicalPath`, `normalizeHonorific`, `extractHonorificFromName`)
   - Format: `/person/{honorific?}-{name-slug}` (NO geo token for privacy/GDPR)
   - Extracts and normalizes honorifics/titles (HRH, Sheikh, HE, Dr, Gen, etc.)
   - Geographic data stored in identifiers/metadata, not in canonical path

5. **Composite Geo Support** (`isCompositeGeo`)
   - Validates composite jurisdictions (2-3 ISO3166-1 alpha2 codes joined by `-`)
   - Lexicographically sorted, max 3 countries

**Testing**: Code compiles successfully. Ready for integration testing.

---

## Roadmap

### Milestone 1: Signal Infrastructure (2-3 weeks)

**Goal**: SignalDescriptor model + axis profiles + basic extraction

**Deliverables:**
- [ ] `SignalDescriptor` struct in models
- [ ] 12-axis `AxisSpec` definitions in taxonomy
- [ ] `PropagationProfile` per axis (stored in Firestore)
- [ ] Update `SynthesisRule` to generate `SignalDescriptor`
- [ ] Basic LLM prompt for signal extraction from complex events

**Success Criteria:**
- Can extract SignalDescriptor from event
- Axis correctly selected from 12-axis basis
- Signal stored as OntologyFact(kind=signal)

### Milestone 2: Bounded Propagation (2-3 weeks)

**Goal**: Propagation engine with complexity contracts

**Deliverables:**
- [ ] Axis-based profile lookup in PropagationEngine
- [ ] Strong edges filtering by axis
- [ ] Bounded traversal: maxHops, threshold, cycle guard
- [ ] Top-K provenance storage per (node, metricPath)
- [ ] Metric path updates on fragments

**Success Criteria:**
- Signal propagates correctly through strong edges
- Provenance traceable from any node metric to source
- No unbounded propagation (verified by tests)

### Milestone 3: Triggers & Interpretation (2-3 weeks)

**Goal**: Threshold detection + lazy LLM interpretation

**Deliverables:**
- [ ] Score computation from node metrics
- [ ] Trigger evaluator (threshold/VIP/delta)
- [ ] Interpretation request queue
- [ ] LLM-based interpreter with prompt template
- [ ] Interpretation cache with TTL

**Success Criteria:**
- High-score nodes automatically flagged
- VIP nodes always interpreted if any metric > 0.1
- Interpretations cached and refreshed on TTL

### Milestone 4: Integration & UI (2-3 weeks)

**Goal**: End-to-end flow + basic visualization

**Deliverables:**
- [ ] Event ingestion → signal → propagation → trigger → interpretation pipeline
- [ ] Node metrics API endpoint
- [ ] Provenance explorer UI (trace to source)
- [ ] Risk/opportunity summary display

**Success Criteria:**
- News event flows through entire pipeline automatically
- Analyst can trace any risk to source event
- Risk summaries display in UI

---

## Technical Decisions

### Decision 1: 12-Axis Basis vs Free-form Categories

**Choice**: Fixed 12-axis basis

**Rationale:**
- **Finite mapping**: Any event maps to known set of axes
- **Predictable propagation**: Each axis has defined profile
- **Debuggability**: No ambiguous "what category is this?"
- **Extensibility**: Add new axis deliberately, not accidentally

### Decision 2: Risk and Opportunity Separate

**Choice**: Separate `polarity` field, never balanced

**Rationale:**
- **Sugar ≠ Salt**: Reducing risk doesn't create opportunity (different dimensions)
- **Clarity**: Each polarity tracked independently
- **Query simplicity**: Filter by polarity directly

### Decision 3: Bounded Propagation (Top-K, Strong Edges)

**Choice**: Strong edges only, top-K contributions, threshold stop

**Rationale:**
- **Observable**: Can see exactly which paths matter
- **Debuggable**: No "magic" from all-paths exploration
- **MVP-friendly**: Start with strong edges, add weak edges later
- **Cost-controlled**: Bounded Firestore operations

**Trade-off**: May miss some weak-path contributions, but these are noise in practice.

### Decision 4: Geo in Payload, Not in Metric Path

**Choice**: `locationPath`, `constraints.destinationPaths` in SignalDescriptor; metric path is universal

**Rationale:**
- **Universal metrics**: Same axis meaning regardless of location
- **Query flexibility**: Filter by location at query time
- **No path explosion**: Metric paths stay bounded

### Decision 5: Lazy Interpretation

**Choice**: LLM only when threshold/VIP/query triggers

**Rationale:**
- **Cost**: LLM calls expensive; most nodes have negligible metrics
- **Freshness**: Generate on-demand, not stale pre-computed
- **Relevance**: Only interpret what matters

---

## Glossary

**Amplitude**: Strength of a signal [0.0, 1.0], analogous to wave height.

**Axis**: One of 12 quasi-physical dimensions that signals can align to (e.g., `legal/access`, `logistics/capacity`).

**Axis Basis**: The complete set of 12 axes that define the signal space.

**Bounded Propagation**: Propagation with explicit limits: maxHops, threshold, cycle guard, top-K.

**Canonical Path**: Deterministic, human-readable identifier for entities (e.g., `/org/us/tesla-inc`).

**Contribution**: A single source of amplitude for a metric on a node (signal + path + decay).

**Damping**: Reduction in amplitude as signal passes through an edge [0.0, 1.0].

**Lazy Interpretation**: Generating human-readable risk/opportunity descriptions only when triggered.

**Mechanism**: Optional qualifier on axis specifying how the effect manifests (e.g., `sanctions`, `airspace_closure`).

**Metric Path**: Canonical path for a metric on a node: `/metric/{polarity}/{axis}/{mechanism?}`.

**Polarity**: Whether signal represents `risk` or `opportunity` (separate, not balanced).

**Propagation Profile**: Configuration for how an axis propagates: damping, maxHops, direction, strong edges, transforms.

**Provenance**: Traceable history of how a metric value arrived at a node (source signals + paths).

**Signal**: A quasi-physical impulse extracted from an event, aligned to one axis.

**SignalDescriptor**: The canonical data structure representing a signal.

**Strong Edge**: Edge type that carries signal for a given axis (defined in propagation profile).

**Top-K**: Bounded number of contributions stored per (node, metricPath) for provenance.

**Trigger**: Condition that causes a node to be queued for interpretation (threshold, VIP, delta, query).

---

**End of Document**

*This architecture defines a predictable signal propagation system based on the Actor–Signal–Medium model. By using a finite 12-axis basis, bounded propagation, and lazy interpretation, we achieve scalability, debuggability, and cost control while maintaining full provenance from events to risk/opportunity assessments.*
