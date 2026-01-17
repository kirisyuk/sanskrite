# Risk & Opportunity Architecture
## Horizon Knowledge Graph: Tension Field Propagation System

**Version**: 2.0 (Tension Field Model)
**Date**: 2026-01-17
**Status**: Architectural Design

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Foundation: Canonical Paths & Taxonomy](#foundation-canonical-paths--taxonomy)
3. [Core Concept: Risks as Tension Fields](#core-concept-risks-as-tension-fields)
4. [Tension Categories: Universal Language](#tension-categories-universal-language)
5. [Wave Propagation: Graph Physics](#wave-propagation-graph-physics)
6. [Interference & Resonance](#interference--resonance)
7. [Lazy Interpretation](#lazy-interpretation)
8. [Implementation Architecture](#implementation-architecture)
9. [Use Cases & Examples](#use-cases--examples)
10. [Implementation Roadmap](#implementation-roadmap)
11. [Technical Decisions & Trade-offs](#technical-decisions--trade-offs)

---

## Executive Summary

### Vision

Transform the Horizon Knowledge Graph into a **continuous tension field system** that:
- Encodes events as domain-agnostic tension vectors
- Propagates tensions through the graph using wave physics
- Detects emergent patterns through interference and resonance
- Interprets risks **lazily** only when needed (thresholds, VIP nodes, or queries)

### Key Innovation: Tension Field Model

**Traditional approach** (event-driven risk propagation):
```
Event → Risk Classification → Propagate Semantic Risk → Interpret at Each Node
         ↑ LLM here          ↑ Semantic BFS          ↑ Heavy, constant
```

**Tension field approach** (physics-inspired):
```
Event → Tension Encoding → Wave Propagation → Interference → Detect Peaks → Lazy Interpret
        ↑ Once            ↑ Pure physics    ↑ Emergence   ↑ Thresholds  ↑ Only when needed
```

### Why This Is Better

1. **Separation of concerns**: Semantics only at endpoints (encoding & interpretation)
2. **Emergent intelligence**: Interference creates patterns not explicitly programmed
3. **Scalability**: Middle layer is pure math (millions of nodes, no LLM)
4. **Debuggability**: Every tension has provenance, traceable back to source
5. **Opportunities for free**: Negative tensions = opportunities

### Philosophical Insight

**Risks are not semantic labels that travel through a graph. They are accumulating tensions—like stress in materials or waves in water—that interfere, resonate, and only become "meaningful" when we choose to interpret them.**

---

## Foundation: Canonical Paths & Taxonomy

### Canonical Path Philosophy

Canonical paths are **Horizon-first identifiers** that serve dual purposes:
1. **Stable addressing**: Deterministic, human-readable IDs
2. **Taxonomic classification**: Structure encodes hierarchy

**Core Design Principles:**
- **Deterministic structure**: Fixed segment counts (e.g., `/loc/{cc}/{admin1}/{city}` always 4 segments)
- **Jurisdiction scoping**: Minimize collisions (e.g., `/org/{iso2}/{slug}`)
- **Hierarchical when useful**: Product taxonomy, location hierarchy
- **Pragmatic over elegant**: `/loc/ae/na/na` is "boring" but battle-tested

### Why `/loc/ae/na/na` is Brilliant

This appears verbose ("UAE / not-applicable / not-applicable"), but it's **deterministic perfection**:
- **Zero special cases**: Always exactly 4 segments, always parseable the same way
- **Self-documenting**: `na` explicitly signals "country-level, no region/city"
- **Index-friendly**: Works perfectly with prefix indexes, LIKE patterns, sorting
- **Battle-tested**: Years of production use, zero bugs from edge cases

**Philosophy**: Boring technology wins. Consistency beats elegance.

### Taxonomy Layers

#### 1. Runtime Taxonomy (Firestore: `horizon_taxonomy/v1`)

**Source of truth for backend validation**

```
canonicalPrefixes: [org, person, product, loc, market, gov, ...]
allowedSpherePaths + sphereSynonyms
allowedEventTypes + eventTypeSynonyms
allowedRelationTypes
allowedProductCategories/Types/Stages
allowedDepositResources
allowedLifecycleStages
allowedMetricUnits
marketSegmentNormalization dictionaries
```

**Purpose**: Enforce consistency, enable updates without deployment

#### 2. Entity Taxonomy (entityPath - guide)

**Physical nature classification**

```
/Organization/Corporation/Public
/Organization/Corporation/Private
/Organization/Government/Agency
/Infrastructure/Logistics/Seaport
/Infrastructure/Energy/PowerPlant/Nuclear
/Resource/RawMaterial/Mineral
/Person/PEP
/Geo/Region/Country
```

**Purpose**: Not strict allow-list, but guide for typing and context

#### 3. Sector Taxonomy (sectorPath - guide)

**Industry classification (GICS/NAICS-like)**

```
/Energy/Fossil_Fuels/Oil_Gas_Upstream
/Technology/Semiconductors/Manufacturing
/Materials/Mining/Rare_Earths
/Automotive/Vehicles/EV
```

**Purpose**: Domain grouping for propagation rules and analytics

### Canonical Path Examples

```
# Organizations (jurisdiction-scoped)
/org/us/tesla
/org/cn/byd-auto
/org/na/unknown-startup

# Government entities (required jurisdiction)
/gov/sa/sfda
/gov/us/fda

# Persons (flat namespace)
/person/elon-musk
/person/mohammed-bin-salman

# Products (hierarchical taxonomy)
/product/raw/mineral/extraction/lithium-ore
/product/component/electronic/manufacturing/battery-cell
/product/finished/mechanical/assembly/electric-vehicle
/product/service/logistics/maritime/container-shipping

# Locations (strict 3-position model)
/loc/us/tx/austin
/loc/de/by/munich
/loc/sa/na/na        # Saudi Arabia (country-level)
/loc/ae/na/na        # UAE (country-level)

# Markets (geo-scoped)
/market/sa/event-marketing
/market/global/semiconductor-manufacturing

# Programs
/program/kazakhstan/alem-ai

# Deposits
/deposit/oil/ghawar-field

# Communities
/community/ae/trade/hawala-dubai-network

# Market Segments
/segment/finance/banking/sa
```

### IndexPaths: Denormalized Coordinates

**Purpose**: Fast filtering without joins (up to 80 paths per fragment)

```go
indexPaths := []string{
    // Canonical
    fragment.CanonicalPath,

    // Location hierarchy (with ancestors)
    "/loc/us/tx/austin",
    "/loc/us/tx",
    "/loc/us",

    // Spheres
    "/sphere/energy",
    "/sphere/manufacturing",

    // Industries (with ancestors)
    "/industry/automotive/vehicles/ev",
    "/industry/automotive/vehicles",
    "/industry/automotive",

    // Segments (with ancestors)
    "/segment/finance/banking/sa",
    "/segment/finance/banking",
    "/segment/finance",

    // Sector (with ancestors)
    "/sector/automotive/vehicles/ev",

    // Entity (with ancestors)
    "/entity/organization/corporation/public",

    // Identifiers
    "/id/wikidataid/q478214",
    "/id/legalname/tesla-inc",

    // Tags
    "/tag/public-company",
    "/tag/ev-manufacturer",
}
```

**Key insight**: This denormalization enables tension propagation to efficiently filter by sector, location, or entity type without graph traversal.

---

## Core Concept: Risks as Tension Fields

### The Paradigm Shift

**Old mental model**: "A risk is a thing that happens and spreads through edges."

**New mental model**: "Risks are tensions—like stress in materials or waves in water—that accumulate, interfere, and only become 'meaningful' when we interpret them."

### Metaphor: Ripples on Water

Imagine dropping stones into a pond:
1. **Stone = Event**: Factory fire, sanctions, strike
2. **Ripple = Tension wave**: Spreads through graph edges
3. **Interference**: Where ripples meet, they amplify or cancel
4. **Peak detection**: We only "look" at places where water height (tension) is notable
5. **Interpretation**: Only then do we say "this means X risk for Y entity"

### Why This Works

**Physics, not semantics, in the middle layer:**
- Propagation is deterministic (no LLM needed for each hop)
- Interference is emergent (system finds "perfect storms" automatically)
- Interpretation is lazy (only compute human-readable risks when needed)

### TensionField: Core Data Structure

```go
type TensionField struct {
    NodeID          string  // Canonical path of the node

    // Current tension state (continuous values, not boolean flags)
    Tensions        map[TensionCategory]float64  // 0.0-1.0 for each category

    // Incoming tension waves (provenance)
    TensionWaves    []TensionWave  // History of how tensions arrived

    // Interference metrics
    ResonanceScore  float64  // Computed from interference patterns

    // Thresholds for alerting
    AlertThreshold  float64  // When to trigger interpretation (default: 0.7)

    // Decay parameters
    DecayRate       float64  // How fast tensions naturally decay
    LastDecayTime   time.Time

    // Metadata
    LastUpdated     time.Time
    NextReviewTime  time.Time  // For scheduled decay
}
```

### TensionWave: Provenance Trail

```go
type TensionWave struct {
    // Wave identity
    Category        TensionCategory  // What kind of tension
    Amplitude       float64          // Current strength (0.0-1.0)

    // Provenance (traceable back to source)
    SourceEventID   string           // /event/... that created this
    SourceEventSummary string        // Human-readable event description
    PropagationPath []string         // [source_node, hop1, hop2, ..., current_node]
    Hops            int              // Distance from source

    // Wave physics
    InitialAmplitude float64         // Starting strength
    CumulativeDecay  float64         // Total decay applied

    // Temporal
    CreatedAt       time.Time        // When wave was created
    ArrivedAt       time.Time        // When it reached this node
    ExpiresAt       time.Time        // When it should be removed
}
```

**Key insight**: TensionWaves are **immutable records**. They don't change; new waves arrive, old waves expire. This makes the system auditable and time-travelable.

---

## Tension Categories: Universal Language

### Design Philosophy

**Tension categories are domain-agnostic primitives.** They don't say "sanctions" or "factory fire"—they say "legal barrier" or "supply shortage."

This is like physics: we don't have different laws for "apples falling" vs "planets orbiting." We have **gravity**. Similarly, we don't have different propagation for "Aramco fire" vs "lithium mine strike." We have **supply shortage tension**.

### Core Tension Categories

```go
type TensionCategory string

const (
    // ═══════════════════════════════════════════════════════════
    // PHYSICAL FLOW TENSIONS (material & logistical)
    // ═══════════════════════════════════════════════════════════

    TensionSupplyShortage   TensionCategory = "supply_shortage"
    // Scarcity of physical goods/materials
    // Examples: Factory fire, mine closure, embargo

    TensionSupplyGlut       TensionCategory = "supply_glut"
    // Oversupply, excess inventory
    // Examples: Overproduction, demand collapse

    TensionLogisticsBlock   TensionCategory = "logistics_block"
    // Physical transportation disruption
    // Examples: Port strike, shipping route closure, border closure

    TensionCapacityStrain   TensionCategory = "capacity_strain"
    // Overload of production/service capacity
    // Examples: Surge in demand, resource bottleneck

    // ═══════════════════════════════════════════════════════════
    // FINANCIAL FLOW TENSIONS (money & credit)
    // ═══════════════════════════════════════════════════════════

    TensionCreditStress     TensionCategory = "credit_stress"
    // Difficulty obtaining credit, rising default risk
    // Examples: Credit rating downgrade, loan default

    TensionLiquidityCrunch  TensionCategory = "liquidity_crunch"
    // Shortage of cash/liquid assets
    // Examples: Bank run, cash flow crisis

    TensionDemandShock      TensionCategory = "demand_shock"
    // Sudden change in demand (positive or negative)
    // Examples: Market crash, viral product launch

    TensionPriceVolatility  TensionCategory = "price_volatility"
    // Rapid, unpredictable price movements
    // Examples: Commodity price spike, currency crash

    // ═══════════════════════════════════════════════════════════
    // LEGAL & REGULATORY TENSIONS
    // ═══════════════════════════════════════════════════════════

    TensionLegalBarrier     TensionCategory = "legal_barrier"
    // Hard legal prohibition or restriction
    // Examples: Sanctions, export controls, licensing revocation

    TensionComplianceRisk   TensionCategory = "compliance_risk"
    // Risk of violating regulations (secondary/indirect)
    // Examples: Associating with sanctioned entity, gray area rules

    TensionRegulatoryShift  TensionCategory = "regulatory_shift"
    // Change in regulatory environment
    // Examples: New law, policy change, enforcement shift

    // ═══════════════════════════════════════════════════════════
    // INFORMATION TENSIONS
    // ═══════════════════════════════════════════════════════════

    TensionReputationalWave TensionCategory = "reputational_wave"
    // Damage to brand/reputation
    // Examples: Scandal, negative press, social media backlash

    TensionUncertainty      TensionCategory = "uncertainty"
    // Lack of information, unpredictability
    // Examples: Geopolitical instability, unclear policy, rumors

    TensionInformationAsymmetry TensionCategory = "information_asymmetry"
    // Some parties have better information than others
    // Examples: Insider knowledge, delayed disclosure

    // ═══════════════════════════════════════════════════════════
    // OPERATIONAL TENSIONS
    // ═══════════════════════════════════════════════════════════

    TensionResourceScarcity TensionCategory = "resource_scarcity"
    // Shortage of inputs (labor, materials, energy)
    // Examples: Worker shortage, power outage, raw material scarcity

    TensionTechnicalFailure TensionCategory = "technical_failure"
    // System/infrastructure breakdown
    // Examples: IT outage, equipment failure, cyber attack

    TensionLaborDispute     TensionCategory = "labor_dispute"
    // Conflict with workforce
    // Examples: Strike, walkout, union negotiation

    // ═══════════════════════════════════════════════════════════
    // GEOPOLITICAL TENSIONS
    // ═══════════════════════════════════════════════════════════

    TensionConflict         TensionCategory = "conflict"
    // Armed conflict, war
    // Examples: Military action, civil war, terrorism

    TensionPoliticalInstability TensionCategory = "political_instability"
    // Regime uncertainty, unrest
    // Examples: Protests, coup, election crisis

    TensionDiplomaticFriction TensionCategory = "diplomatic_friction"
    // Strained international relations
    // Examples: Trade dispute, diplomatic row, alliance breakdown
)
```

### Tension Polarity

**Important**: Some tensions are inherently **negative** (shortage, barrier), others can be **positive** (supply glut = opportunity for buyers, demand shock = opportunity for sellers).

```go
type TensionPolarity int

const (
    TensionNegative TensionPolarity = -1  // Risk/threat
    TensionNeutral  TensionPolarity = 0   // Context-dependent
    TensionPositive TensionPolarity = 1   // Opportunity
)

// Polarity can flip based on node context
func (t TensionCategory) PolarityForNode(node *HorizonFragment) TensionPolarity {
    switch t {
    case TensionSupplyShortage:
        // Negative for buyers, neutral for competitors
        if node.hasRole("buyer") {
            return TensionNegative
        }
        if node.hasRole("competitor_to_affected") {
            return TensionPositive  // Opportunity to gain market share
        }
        return TensionNeutral

    case TensionSupplyGlut:
        // Positive for buyers, negative for sellers
        if node.hasRole("buyer") {
            return TensionPositive
        }
        if node.hasRole("seller") {
            return TensionNegative
        }
        return TensionNeutral

    case TensionDemandShock:
        // Sign depends on direction (encoded in amplitude sign)
        return TensionNeutral  // Handled by amplitude +/-

    default:
        return TensionNegative
    }
}
```

---

## Wave Propagation: Graph Physics

### Propagation as BFS with Wave Physics

**Core idea**: Tensions propagate like waves through graph edges. Each edge type has physics parameters (speed, attenuation, transformation).

### WavePropagationRule

```go
type WavePropagationRule struct {
    // Rule identity
    Name            string   // "supply_shortage_downstream"

    // Which tensions propagate via this rule
    TensionCategories []TensionCategory

    // Graph topology
    EdgeTypes       []string  // "SUPPLIES_TO", "OWNS", "LOCATED_IN", etc.
    Direction       Direction // Forward, Backward, or Both
    MaxHops         int       // Maximum propagation distance

    // Wave physics
    PropagationSpeed  float64  // Hops per time unit (for temporal queries)
    Attenuation       float64  // Amplitude decay per hop (0.0-1.0)

    // Transformation rules (category can change during propagation)
    Transformations   []TensionTransformation

    // Contextual filters (when to apply this rule)
    RequireSectorMatch     bool     // Only within same sector
    RequiredNodeTypes      []string // Only to specific entity types
    RequiredMetrics        []MetricCondition  // "supply_dependency > 0.3"
    ExcludedNodeTypes      []string

    // Amplification (context-dependent strengthening)
    AmplificationRules     []AmplificationRule
}

type Direction string
const (
    DirectionForward  Direction = "forward"   // Follow edge direction
    DirectionBackward Direction = "backward"  // Reverse edge direction
    DirectionBoth     Direction = "both"      // Both directions
)

type TensionTransformation struct {
    // Input → Output category mapping
    InputCategory  TensionCategory
    OutputCategory TensionCategory

    // Transformation conditions
    RequiredNodeType string  // Only transform at certain node types
    AmplitudeFactor  float64 // Multiply amplitude (default: 1.0)
}

type MetricCondition struct {
    MetricKey string  // "supply_dependency", "ownership_percent", etc.
    Operator  string  // ">", ">=", "<", "<=", "=="
    Value     float64
}

type AmplificationRule struct {
    // Condition to trigger amplification
    Condition string  // Expression like "node.tags contains 'just-in-time'"

    // How much to amplify
    Factor    float64 // Multiply amplitude by this
}
```

### Example Rules

#### Rule 1: Supply Shortage Downstream

```go
supplyShortageRule := WavePropagationRule{
    Name: "supply_shortage_downstream",

    TensionCategories: []TensionCategory{
        TensionSupplyShortage,
        TensionLogisticsBlock,
    },

    EdgeTypes: []string{"SUPPLIES_TO", "PRODUCES"},
    Direction: DirectionForward,  // Down the supply chain
    MaxHops:   4,

    PropagationSpeed: 1.0,  // 1 hop per day
    Attenuation:      0.75, // 75% amplitude retained per hop

    RequiredMetrics: []MetricCondition{
        {MetricKey: "supply_dependency", Operator: ">", Value: 0.2},
    },

    AmplificationRules: []AmplificationRule{
        {
            Condition: "node.tags contains 'just-in-time'",
            Factor:    1.5,  // JIT manufacturing especially vulnerable
        },
        {
            Condition: "node.tags contains 'single-source'",
            Factor:    1.8,  // Single-source suppliers = high risk
        },
        {
            Condition: "node.entityPath startsWith '/Infrastructure/'",
            Factor:    1.3,  // Infrastructure has limited flexibility
        },
    },
}
```

**Example propagation:**
```
Saudi Aramco Fire (source)
  TensionSupplyShortage = 0.9

  ↓ SUPPLIES_TO (supply_dependency=0.8, attenuation=0.75)

Asian Refineries
  TensionSupplyShortage = 0.9 × 0.8 × 0.75 = 0.54

  ↓ SUPPLIES_TO (supply_dependency=0.7, attenuation=0.75)
  [Amplification: node has 'just-in-time' tag, ×1.5]

Airlines (JIT operations)
  TensionSupplyShortage = 0.54 × 0.7 × 0.75 × 1.5 = 0.42

  ↓ SUPPLIES_TO (supply_dependency=0.5, attenuation=0.75)

Tourism Companies
  TensionSupplyShortage = 0.42 × 0.5 × 0.75 = 0.16

  ↓ Threshold: 0.16 < 0.2, stop propagation
```

#### Rule 2: Legal Barrier Upstream & Lateral

```go
legalBarrierRule := WavePropagationRule{
    Name: "legal_barrier_contagion",

    TensionCategories: []TensionCategory{
        TensionLegalBarrier,
    },

    EdgeTypes: []string{"OWNED_BY", "SUBSIDIARY_OF"},
    Direction: DirectionBackward,  // Up ownership chain
    MaxHops:   10,  // Legal risk travels far

    Attenuation: 0.95,  // Weak decay (95% retained)

    Transformations: []TensionTransformation{
        {
            InputCategory:  TensionLegalBarrier,
            OutputCategory: TensionComplianceRisk,  // Transforms as it spreads
            RequiredNodeType: "/Organization/",
            AmplitudeFactor:  0.8,  // Slightly weaker as compliance risk
        },
    },

    AmplificationRules: []AmplificationRule{
        {
            Condition: "node.location.country in ['cn', 'ru', 'ir']",
            Factor:    1.5,  // Higher-risk jurisdictions
        },
        {
            Condition: "edge.metrics.ownership_percent > 0.5",
            Factor:    1.3,  // Majority ownership = stronger contagion
        },
    },
}
```

**Example propagation:**
```
Huawei (sanctioned entity)
  TensionLegalBarrier = 1.0

  ↓ OWNED_BY (ownership=1.0, attenuation=0.95) [backward]
  [Transformation: TensionLegalBarrier → TensionComplianceRisk × 0.8]

Huawei Investment Holdings (parent)
  TensionComplianceRisk = 1.0 × 0.95 × 0.8 = 0.76

  ↓ PARTNER_WITH (partnership_revenue=0.4, attenuation=0.95)
  [Amplification: China jurisdiction × 1.5]

SMIC (Chinese partner)
  TensionComplianceRisk = 0.76 × 0.4 × 0.95 × 1.5 = 0.43
```

#### Rule 3: Geopolitical Conflict via Location

```go
geopoliticalConflictRule := WavePropagationRule{
    Name: "geopolitical_conflict_exposure",

    TensionCategories: []TensionCategory{
        TensionConflict,
        TensionPoliticalInstability,
    },

    EdgeTypes: []string{"LOCATED_IN", "HAS_FACILITY_IN"},
    Direction: DirectionForward,
    MaxHops:   2,  // Local effect mostly

    Attenuation: 0.85,

    RequiredMetrics: []MetricCondition{
        {MetricKey: "employee_count", Operator: ">", Value: 50},
    },

    AmplificationRules: []AmplificationRule{
        {
            Condition: "node.entityPath contains '/Infrastructure/'",
            Factor:    1.6,  // Infrastructure especially vulnerable
        },
        {
            Condition: "node.tags contains 'critical-supply-chain'",
            Factor:    1.8,
        },
    },
}
```

**Example propagation:**
```
/loc/ye/na/na (Yemen conflict zone)
  TensionConflict = 0.85

  ↓ LOCATED_IN (employee_count=200, attenuation=0.85)
  [Amplification: Infrastructure × 1.6]

/facility/ye/aden-port (port infrastructure)
  TensionConflict = 0.85 × 0.85 × 1.6 = 1.16 → capped at 1.0

  ↓ PROVIDES_SERVICE_TO (revenue_dependency=0.6, attenuation=0.85)

/org/ae/dp-world (port operator)
  TensionConflict = 1.0 × 0.6 × 0.85 = 0.51
```

#### Rule 4: Financial Contagion via Debt

```go
financialContagionRule := WavePropagationRule{
    Name: "financial_distress_contagion",

    TensionCategories: []TensionCategory{
        TensionCreditStress,
        TensionLiquidityCrunch,
    },

    EdgeTypes: []string{"OWES_TO", "INVESTED_IN", "GUARANTEED_BY"},
    Direction: DirectionBoth,  // Affects both lenders and borrowers
    MaxHops:   5,

    Attenuation: 0.8,

    RequiredMetrics: []MetricCondition{
        {MetricKey: "exposure_amount", Operator: ">", Value: 1000000},  // $1M+
    },

    AmplificationRules: []AmplificationRule{
        {
            Condition: "node.entityPath contains '/Financial/'",
            Factor:    1.5,  // Banks/funds more exposed
        },
        {
            Condition: "node.sectorPath contains '/RealEstate/'",
            Factor:    1.4,  // Real estate sector contagion
        },
    },
}
```

---

## Interference & Resonance

### The Magic: Emergent Patterns

**Key insight**: When multiple tension waves arrive at the same node, they **interfere**. This creates patterns that weren't explicitly programmed.

### Constructive Interference (Amplification)

**Certain combinations of tensions amplify each other**—like how two sound waves in phase create a louder sound.

```go
func (e *InterferenceEngine) calculateResonance(field *TensionField) float64 {
    resonance := 0.0

    // ════════════════════════════════════════════════════════════════
    // Pattern 1: Supply Shortage + Logistics Block = Perfect Storm
    // ════════════════════════════════════════════════════════════════
    supplyShortage := field.Tensions[TensionSupplyShortage]
    logisticsBlock := field.Tensions[TensionLogisticsBlock]

    if supplyShortage > 0.4 && logisticsBlock > 0.4 {
        // Multiply effect: can't get supplies AND can't ship alternatives
        amplification := supplyShortage * logisticsBlock * 2.0
        resonance += amplification

        // If node is JIT, multiply again
        if field.Node.Tags.Contains("just-in-time") {
            resonance += amplification * 0.5
        }
    }

    // ════════════════════════════════════════════════════════════════
    // Pattern 2: Credit Stress + Demand Shock = Liquidity Crisis
    // ════════════════════════════════════════════════════════════════
    creditStress := field.Tensions[TensionCreditStress]
    demandShock := field.Tensions[TensionDemandShock]  // Negative shock

    if creditStress > 0.5 && demandShock > 0.5 {
        // Can't borrow AND revenues dropping = death spiral
        resonance += creditStress * demandShock * 2.5
    }

    // ════════════════════════════════════════════════════════════════
    // Pattern 3: Legal Barrier + Reputational Wave = Death Sentence
    // ════════════════════════════════════════════════════════════════
    legalBarrier := field.Tensions[TensionLegalBarrier]
    reputationalWave := field.Tensions[TensionReputationalWave]

    if legalBarrier > 0.6 && reputationalWave > 0.3 {
        // Legal trouble + bad press = partners flee
        resonance += (legalBarrier + reputationalWave) * 1.8
    }

    // ════════════════════════════════════════════════════════════════
    // Pattern 4: Multiple Tensions of Same Category = Exponential
    // ════════════════════════════════════════════════════════════════
    // Count number of tensions above threshold
    highTensions := 0
    for _, amplitude := range field.Tensions {
        if amplitude > 0.5 {
            highTensions++
        }
    }

    if highTensions >= 3 {
        // "Everything is on fire" scenario
        resonance += float64(highTensions) * 0.3
    }

    // ════════════════════════════════════════════════════════════════
    // Pattern 5: Sector-Specific Resonances
    // ════════════════════════════════════════════════════════════════
    if field.Node.SectorPath.Contains("/Automotive/") {
        // Automotive especially sensitive to supply + demand combo
        if supplyShortage > 0.4 && demandShock > 0.4 {
            resonance += 0.8
        }
    }

    if field.Node.SectorPath.Contains("/Financial/") {
        // Financial especially sensitive to credit + liquidity combo
        liquidityCrunch := field.Tensions[TensionLiquidityCrunch]
        if creditStress > 0.5 && liquidityCrunch > 0.5 {
            resonance += 1.2
        }
    }

    return resonance
}
```

### Destructive Interference (Cancellation)

**Some tensions cancel each other out**—like how noise-canceling headphones work.

```go
func (e *InterferenceEngine) applyDestructiveInterference(field *TensionField) {
    // ════════════════════════════════════════════════════════════════
    // Cancellation 1: Supply Glut cancels Supply Shortage
    // ════════════════════════════════════════════════════════════════
    shortage := field.Tensions[TensionSupplyShortage]
    glut := field.Tensions[TensionSupplyGlut]

    if shortage > 0 && glut > 0 {
        cancellation := math.Min(shortage, glut)
        field.Tensions[TensionSupplyShortage] -= cancellation
        field.Tensions[TensionSupplyGlut] -= cancellation

        // Log the cancellation event
        field.InterferenceEvents = append(field.InterferenceEvents, InterferenceEvent{
            Type:        "destructive",
            Categories:  []TensionCategory{TensionSupplyShortage, TensionSupplyGlut},
            Magnitude:   cancellation,
            Description: "Supply glut offset supply shortage",
        })
    }

    // ════════════════════════════════════════════════════════════════
    // Cancellation 2: Positive Demand Shock offsets Negative
    // ════════════════════════════════════════════════════════════════
    // (Demand shocks can have polarity in amplitude sign)
    // Handled by signed arithmetic in amplitude
}
```

### Phase-Dependent Interference

**Advanced**: Timing matters. Waves arriving simultaneously interfere more strongly than waves separated in time.

```go
func (e *InterferenceEngine) calculatePhaseCoherence(waves []TensionWave) float64 {
    if len(waves) < 2 {
        return 0
    }

    // Group waves by arrival time window (24 hours)
    timeWindow := 24 * time.Hour
    groups := e.groupByTimeWindow(waves, timeWindow)

    maxCoherence := 0.0
    for _, group := range groups {
        if len(group) >= 2 {
            // Multiple waves arriving simultaneously = coherent interference
            coherence := float64(len(group)) * 0.2
            if coherence > maxCoherence {
                maxCoherence = coherence
            }
        }
    }

    return maxCoherence
}
```

---

## Lazy Interpretation

### Philosophy: Don't Interpret Until Necessary

**Key insight**: Most nodes in the graph have low tensions. We don't need human-readable risk descriptions for them. Only interpret when:
1. **Threshold exceeded**: Composite stress or resonance is high
2. **VIP node**: Node is important (fortune 500, critical infrastructure, user portfolio)
3. **User query**: Someone explicitly asks "what are the risks for X?"

### Three Triggers for Interpretation

#### Trigger 1: Threshold Detection

```go
type ThresholdDetector struct {
    tensionRepo *TensionFieldRepository
}

func (d *ThresholdDetector) ScanForAlerts(ctx context.Context) []TensionAlert {
    alerts := []TensionAlert{}

    // Get all tension fields (can be millions, but we only scan values)
    fields := d.tensionRepo.GetAllFields(ctx)

    for _, field := range fields {
        // Calculate composite stress
        compositeStress := d.calculateCompositeStress(field)

        // Criterion 1: High composite stress
        if compositeStress > 0.7 {
            alerts = append(alerts, TensionAlert{
                NodeID:      field.NodeID,
                Severity:    "HIGH",
                Trigger:     "composite_stress_threshold",
                Score:       compositeStress,
                RequiresInterpretation: true,
            })
            continue
        }

        // Criterion 2: High resonance (interference)
        if field.ResonanceScore > 1.5 {
            alerts = append(alerts, TensionAlert{
                NodeID:      field.NodeID,
                Severity:    "CRITICAL",
                Trigger:     "resonance_detected",
                Score:       field.ResonanceScore,
                RequiresInterpretation: true,
            })
            continue
        }

        // Criterion 3: Rapid tension growth (delta)
        delta := d.calculateTensionDelta(field, 24*time.Hour)
        if delta > 0.3 {  // 30% increase in 24 hours
            alerts = append(alerts, TensionAlert{
                NodeID:      field.NodeID,
                Severity:    "MEDIUM",
                Trigger:     "rapid_tension_growth",
                Score:       delta,
                RequiresInterpretation: true,
            })
        }
    }

    return alerts
}

func (d *ThresholdDetector) calculateCompositeStress(field *TensionField) float64 {
    // Weighted average of all tensions
    weights := map[TensionCategory]float64{
        TensionSupplyShortage:   0.25,
        TensionLegalBarrier:     0.25,
        TensionCreditStress:     0.20,
        TensionLogisticsBlock:   0.15,
        TensionConflict:         0.15,
        // ... others get equal share of remaining weight
    }

    composite := 0.0
    totalWeight := 0.0

    for category, amplitude := range field.Tensions {
        weight := weights[category]
        if weight == 0 {
            weight = 0.05  // Default weight
        }
        composite += amplitude * weight
        totalWeight += weight
    }

    if totalWeight > 0 {
        composite /= totalWeight
    }

    // Add resonance bonus
    composite += field.ResonanceScore * 0.2

    return math.Min(composite, 1.0)
}
```

#### Trigger 2: VIP Node Monitoring

```go
func (d *ThresholdDetector) GetVIPNodeTensions(ctx context.Context) []TensionAlert {
    alerts := []TensionAlert{}

    // VIP nodes are always monitored
    vipNodes := d.identifyVIPNodes(ctx)

    for _, nodeID := range vipNodes {
        field := d.tensionRepo.GetField(ctx, nodeID)
        if field == nil {
            continue
        }

        // Any non-zero tension on VIP node = interpret
        compositeStress := d.calculateCompositeStress(field)
        if compositeStress > 0.1 {  // Lower threshold for VIPs
            alerts = append(alerts, TensionAlert{
                NodeID:      nodeID,
                Severity:    d.classifySeverity(compositeStress),
                Trigger:     "vip_node_monitoring",
                Score:       compositeStress,
                RequiresInterpretation: true,
            })
        }
    }

    return alerts
}

func (d *ThresholdDetector) identifyVIPNodes(ctx context.Context) []string {
    // Query for high-importance nodes
    filters := []string{
        "/tag/fortune-500",
        "/tag/critical-infrastructure",
        "/tag/user-portfolio",
        "/tag/government-agency",
    }

    // Also nodes with high revenue/employees
    // revenue > $1B or employees > 10000

    return d.tensionRepo.QueryNodesByFilters(ctx, filters)
}
```

#### Trigger 3: User Query

```go
func (i *LazyInterpreter) InterpretNodeRisks(ctx context.Context, nodeID string) (*RiskInterpretation, error) {
    // User explicitly asked: "What are the risks for Tesla?"

    field := i.tensionRepo.GetField(ctx, nodeID)
    if field == nil {
        return &RiskInterpretation{
            NodeID:  nodeID,
            Summary: "No tensions detected for this node.",
        }, nil
    }

    // Generate human-readable interpretation
    return i.interpretTensionField(ctx, field)
}
```

### Interpretation: Tensions → Human Language

```go
type LazyInterpreter struct {
    llm        LLMClient
    tensionRepo *TensionFieldRepository
    fragmentsRepo *HorizonFragmentsRepository
}

func (i *LazyInterpreter) interpretTensionField(
    ctx context.Context,
    field *TensionField,
) (*RiskInterpretation, error) {

    // Get node details
    node, _ := i.fragmentsRepo.GetByCanonicalPath(ctx, field.NodeID)

    // Build provenance trails (which events caused these tensions)
    provenance := i.buildProvenanceTrails(field)

    // LLM prompt
    prompt := fmt.Sprintf(`
You are analyzing tensions in a knowledge graph node.

Node: %s
Type: %s
Sector: %s
Location: %s

Current Tensions:
%s

Provenance (how tensions arrived):
%s

Interference/Resonance Score: %.2f
%s

Please provide:
1. A 2-3 sentence summary of the risk situation
2. Breakdown of top 3 contributing tensions with their sources
3. Estimated business impact (production/revenue/operations)
4. Timeline estimate for risk resolution
5. Recommended mitigation actions

Be specific, factual, and concise. Focus on actionable intelligence.
`,
        field.NodeID,
        node.Type,
        node.SectorPath,
        node.HomeLocation,
        i.formatTensions(field.Tensions),
        i.formatProvenance(provenance),
        field.ResonanceScore,
        i.explainResonance(field),
    )

    // Call LLM
    response := i.llm.Complete(ctx, prompt)

    // Parse and structure response
    interpretation := &RiskInterpretation{
        NodeID:           field.NodeID,
        NodePath:         field.NodeID,
        Summary:          response.Summary,
        CompositeStress:  i.calculateCompositeStress(field),
        ResonanceScore:   field.ResonanceScore,
        TensionBreakdown: i.buildTensionBreakdown(field),
        SourceEvents:     provenance,
        Mitigations:      response.Mitigations,
        EstimatedImpact:  response.EstimatedImpact,
        Timeline:         response.Timeline,
        Confidence:       response.Confidence,
        GeneratedAt:      time.Now(),
    }

    return interpretation, nil
}
```

### RiskInterpretation Structure

```go
type RiskInterpretation struct {
    NodeID          string
    NodePath        string  // Canonical path

    // Human-readable summary (LLM-generated)
    Summary         string
    // Example: "Tesla faces HIGH supply disruption risk (0.78 composite stress)
    //  from converging factors: lithium shortage from Chilean mine strike (3 hops),
    //  logistics delays from Red Sea conflict (4 hops), and capacity strain from
    //  Gigafactory Berlin expansion. Resonance effect amplifies stress by +0.42
    //  due to JIT operations and single-source dependencies. Estimated impact:
    //  15-20% production reduction over 2-4 weeks."

    // Quantitative metrics
    CompositeStress  float64  // 0.0-1.0
    ResonanceScore   float64  // 0.0-3.0+ (can exceed 1.0)

    // Tension breakdown (what tensions contribute how much)
    TensionBreakdown map[TensionCategory]TensionContribution

    // Provenance (where tensions came from)
    SourceEvents     []EventProvenance

    // Business impact estimate
    EstimatedImpact  ImpactEstimate

    // Resolution timeline
    Timeline         TimelineEstimate

    // Recommended actions
    Mitigations      []Mitigation

    // Confidence in interpretation
    Confidence       float64  // 0.0-1.0

    // Metadata
    GeneratedAt      time.Time
    ExpiresAt        time.Time  // When interpretation should be refreshed
}

type TensionContribution struct {
    Category        TensionCategory
    Amplitude       float64
    PercentOfTotal  float64  // What % of composite stress
    SourceWaves     []TensionWave  // Which waves created this
    ResonanceEffect float64        // Additional stress from interference
}

type EventProvenance struct {
    EventID           string   // /event/...
    EventSummary      string   // "Saudi Aramco refinery fire"
    EventDate         time.Time
    PropagationPath   []string // [source, hop1, hop2, ..., this_node]
    Hops              int
    ContributionPercent float64 // What % of total stress
    ArrivalTime       time.Time
}

type ImpactEstimate struct {
    ProductionImpact  string  // "15-20% reduction"
    RevenueImpact     string  // "$50-80M quarterly loss"
    OperationalImpact string  // "Supply delays 2-4 weeks"
    Confidence        float64
}

type TimelineEstimate struct {
    ShortTerm   string  // "1-2 weeks: peak impact"
    MediumTerm  string  // "4-6 weeks: stabilization"
    LongTerm    string  // "3 months: full recovery"
}

type Mitigation struct {
    Action      string  // "Activate alternative lithium suppliers"
    Feasibility string  // "High - existing contracts in Australia"
    Cost        string  // "$15M additional costs"
    Timeframe   string  // "2 weeks to ramp up"
    Effectiveness float64  // 0.0-1.0 (how much it reduces risk)
}
```

---

## Implementation Architecture

### System Components

```
┌───────────────────────────────────────────────────────────────────┐
│                        Event Ingestion                             │
│  Sources: News APIs, Analyst Reports, Metric Triggers, Manual     │
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                      Tension Encoder (LLM)                         │
│  Event (human language) → TensionVector (domain-agnostic)         │
│  Example: "Aramco fire" → {supply_shortage: 0.9, uncertainty: 0.7}│
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                     Wave Propagator (Physics)                      │
│  - BFS traversal with tension rules                               │
│  - Attenuation per hop                                            │
│  - Category transformations                                       │
│  - Update TensionFields on all affected nodes                     │
│  - NO INTERPRETATION (pure math)                                  │
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                  Interference Calculator                           │
│  - Detect wave collisions (multiple tensions on same node)       │
│  - Compute resonance (constructive/destructive)                   │
│  - Update ResonanceScore on nodes                                 │
│  - NO INTERPRETATION (pure math)                                  │
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                     Threshold Detector                             │
│  - Scan all TensionFields for high composite stress (>0.7)       │
│  - Check VIP nodes (always interpret if tensions > 0.1)          │
│  - Rapid growth detection (delta > 0.3 in 24 hours)              │
│  - Generate TensionAlerts (requires interpretation)               │
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                  Lazy Interpreter (LLM)                            │
│  TensionField + Provenance → Human-Readable Risk                  │
│  - ONLY when: threshold hit, VIP node, or user query             │
│  - Generate RiskInterpretation with summary, breakdown, actions   │
│  - Store interpretations with TTL (refresh when stale)            │
└────────────────────────────┬──────────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                         UI/API Layer                               │
│  - Tension heatmap (color nodes by composite stress)             │
│  - Wave animation (show propagation over time)                    │
│  - Interpreted risks (human-readable summaries)                   │
│  - Provenance explorer (trace tensions back to source events)     │
│  - Counterfactual simulator ("what if we remove this edge?")      │
└───────────────────────────────────────────────────────────────────┘
```

### Core Services

#### 1. TensionEncoder

```go
type TensionEncoder struct {
    llm LLMClient
}

func (e *TensionEncoder) EncodeEvent(ctx context.Context, event *Event) (*TensionVector, error) {
    prompt := fmt.Sprintf(`
You are encoding a real-world event into domain-agnostic tension categories.

Event: %s
Summary: %s
Location: %s
Date: %s

Available tension categories:
- supply_shortage: Physical scarcity of goods/materials
- supply_glut: Oversupply
- logistics_block: Transportation disruption
- credit_stress: Difficulty obtaining credit
- liquidity_crunch: Cash shortage
- legal_barrier: Hard legal prohibition
- compliance_risk: Risk of violating regulations
- reputational_wave: Brand damage
- uncertainty: Lack of information
- conflict: Armed conflict
- political_instability: Regime uncertainty
... [full list]

For this event, assign amplitude values (0.0-1.0) to relevant tension categories.
Use 0.0 for irrelevant categories.

Output format (JSON):
{
  "tensions": {
    "supply_shortage": 0.9,
    "uncertainty": 0.7,
    "logistics_block": 0.5
  },
  "affected_entities": ["org/sa/saudi-aramco", "loc/sa/na/na"],
  "confidence": 0.85,
  "reasoning": "Refinery fire creates immediate supply shortage..."
}
`, event.Title, event.Content, event.Location, event.Date)

    response := e.llm.Complete(ctx, prompt)
    return parseTensionVector(response)
}
```

#### 2. WavePropagator

```go
type WavePropagator struct {
    rulesEngine    *PropagationRulesEngine
    tensionRepo    *TensionFieldRepository
    fragmentsRepo  *HorizonFragmentsRepository
    edgesRepo      *HorizonEdgesRepository
}

func (p *WavePropagator) PropagateWaves(
    ctx context.Context,
    sourceEvent *Event,
    tensionVector *TensionVector,
) error {
    // 1. Get propagation rules for each tension category
    rules := p.rulesEngine.GetRulesForTensions(tensionVector.Tensions)

    // 2. BFS traversal for each tension category
    for category, initialAmplitude := range tensionVector.Tensions {
        if initialAmplitude < 0.05 {
            continue  // Skip weak tensions
        }

        // Get rules for this category
        categoryRules := filterRulesByCategory(rules, category)

        // Propagate this tension wave
        err := p.propagateSingleTension(
            ctx,
            sourceEvent,
            category,
            initialAmplitude,
            tensionVector.AffectedEntities,
            categoryRules,
        )
        if err != nil {
            return err
        }
    }

    return nil
}

func (p *WavePropagator) propagateSingleTension(
    ctx context.Context,
    sourceEvent *Event,
    category TensionCategory,
    initialAmplitude float64,
    seedNodes []string,
    rules []WavePropagationRule,
) error {

    // BFS queue
    type PropagationState struct {
        NodeID          string
        Amplitude       float64
        Hops            int
        PropagationPath []string
        CumulativeDecay float64
    }

    queue := []PropagationState{}
    for _, seedNode := range seedNodes {
        queue = append(queue, PropagationState{
            NodeID:          seedNode,
            Amplitude:       initialAmplitude,
            Hops:            0,
            PropagationPath: []string{seedNode},
            CumulativeDecay: 1.0,
        })
    }

    visited := make(map[string]bool)

    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]

        // Skip if already visited (take first path only)
        if visited[current.NodeID] {
            continue
        }
        visited[current.NodeID] = true

        // Threshold pruning
        if current.Amplitude < 0.05 {
            continue
        }

        // Add tension wave to this node's field
        wave := &TensionWave{
            Category:         category,
            Amplitude:        current.Amplitude,
            SourceEventID:    sourceEvent.ID,
            SourceEventSummary: sourceEvent.Summary,
            PropagationPath:  current.PropagationPath,
            Hops:             current.Hops,
            InitialAmplitude: initialAmplitude,
            CumulativeDecay:  current.CumulativeDecay,
            CreatedAt:        sourceEvent.Date,
            ArrivedAt:        time.Now(),
            ExpiresAt:        sourceEvent.Date.Add(90 * 24 * time.Hour),  // 90 days
        }

        err := p.tensionRepo.AddWaveToField(ctx, current.NodeID, wave)
        if err != nil {
            return err
        }

        // Find neighbors to propagate to
        for _, rule := range rules {
            if current.Hops >= rule.MaxHops {
                continue
            }

            neighbors := p.findNeighborsByRule(ctx, current.NodeID, rule)

            for _, neighbor := range neighbors {
                // Calculate amplitude decay
                decayedAmplitude := p.calculateDecay(
                    current.Amplitude,
                    neighbor.Edge,
                    rule,
                )

                // Apply amplification (context-dependent)
                amplifiedAmplitude := p.applyAmplification(
                    decayedAmplitude,
                    neighbor.Node,
                    rule,
                )

                // Check transformations (category might change)
                outputCategory := category
                for _, transform := range rule.Transformations {
                    if transform.InputCategory == category &&
                       p.matchesNodeType(neighbor.Node, transform.RequiredNodeType) {
                        outputCategory = transform.OutputCategory
                        amplifiedAmplitude *= transform.AmplitudeFactor
                    }
                }

                // Add to queue
                queue = append(queue, PropagationState{
                    NodeID:    neighbor.Node.CanonicalPath,
                    Amplitude: amplifiedAmplitude,
                    Hops:      current.Hops + 1,
                    PropagationPath: append(current.PropagationPath, neighbor.Node.CanonicalPath),
                    CumulativeDecay: current.CumulativeDecay * (amplifiedAmplitude / current.Amplitude),
                })
            }
        }
    }

    return nil
}
```

#### 3. InterferenceCalculator

```go
type InterferenceCalculator struct {
    tensionRepo *TensionFieldRepository
}

func (c *InterferenceCalculator) RecalculateInterference(ctx context.Context, nodeID string) error {
    field := c.tensionRepo.GetField(ctx, nodeID)
    if field == nil {
        return nil
    }

    // Calculate resonance from interference patterns
    resonance := c.calculateResonance(field)
    field.ResonanceScore = resonance

    // Apply destructive interference (cancellation)
    c.applyDestructiveInterference(field)

    // Update field in storage
    return c.tensionRepo.UpdateField(ctx, field)
}

// (calculateResonance and applyDestructiveInterference implementations shown earlier)
```

---

## Use Cases & Examples

### Use Case 1: Saudi Aramco Refinery Fire

**Event**: Major refinery fire reduces oil output by 30%

**Step 1: Tension Encoding**
```json
{
  "tensions": {
    "supply_shortage": 0.85,
    "uncertainty": 0.70,
    "logistics_block": 0.40
  },
  "affected_entities": [
    "/org/sa/saudi-aramco",
    "/facility/sa/abqaiq-refinery"
  ],
  "confidence": 0.90
}
```

**Step 2: Wave Propagation**
```
/org/sa/saudi-aramco (source)
  supply_shortage: 0.85

  ↓ SUPPLIES_TO (supply_dependency=0.75, attenuation=0.75)

/org/cn/sinopec (Chinese refiner)
  supply_shortage: 0.85 × 0.75 × 0.75 = 0.48

  ↓ SUPPLIES_TO (supply_dependency=0.65, attenuation=0.75)
  [Amplification: JIT operations × 1.5]

/org/cn/china-eastern-airlines
  supply_shortage: 0.48 × 0.65 × 0.75 × 1.5 = 0.35

  ↓ PROVIDES_SERVICE_TO (revenue_dependency=0.40, attenuation=0.75)

/org/cn/ctrip (travel platform)
  supply_shortage: 0.35 × 0.40 × 0.75 = 0.11

  ↓ Threshold: 0.11 > 0.10, propagate continues but weakening
```

**Step 3: Interference (at China Eastern Airlines node)**

Meanwhile, Red Sea conflict creates logistics_block tension that also reaches airlines:

```
/loc/ye/na/na (Yemen conflict)
  conflict: 0.80, logistics_block: 0.70

  ↓ (via shipping routes)

/org/cn/china-eastern-airlines
  logistics_block: 0.25 (arrived via different path)
```

**Interference at Airlines node:**
```
Tensions at /org/cn/china-eastern-airlines:
  supply_shortage: 0.35 (from Aramco fire)
  logistics_block: 0.25 (from Red Sea conflict)

Resonance calculation:
  - Both > 0.25 → amplification = 0.35 × 0.25 × 2.0 = 0.18
  - JIT operations tag → bonus +0.09
  - Total resonance: 0.27

New composite stress: (0.35 + 0.25) / 2 + 0.27 = 0.57
```

**Step 4: Threshold Detection**
```
Node: /org/cn/china-eastern-airlines
Composite Stress: 0.57 (MEDIUM)
Resonance: 0.27

Alert: MEDIUM severity, threshold triggered
Reason: Resonance detected between supply_shortage and logistics_block
Requires Interpretation: YES
```

**Step 5: Lazy Interpretation (LLM)**
```
Risk Interpretation for China Eastern Airlines:

Summary:
China Eastern Airlines faces MEDIUM supply chain stress (0.57 composite) from
converging disruptions: fuel supply shortage from Saudi Aramco refinery fire
(3 hops, 45% contribution) and logistics delays from Red Sea shipping conflict
(4 hops, 25% contribution). Resonance effect (+0.27) amplifies risk due to
just-in-time fuel procurement model. Estimated impact: 8-12% flight cancellations
over 2-3 weeks.

Top Contributing Tensions:
1. Supply Shortage (0.35) - Aramco fire → Sinopec → Airlines
   Source: Saudi Aramco Abqaiq refinery fire, 2024-01-15
   Path: /org/sa/saudi-aramco → /org/cn/sinopec → /org/cn/china-eastern-airlines

2. Logistics Block (0.25) - Red Sea conflict → Shipping routes
   Source: Yemen Houthi attacks on Red Sea shipping, 2024-01-10
   Path: /loc/ye → /route/red-sea → /org/ae/dp-world → /org/cn/china-eastern-airlines

3. Resonance Effect (+0.27) - Interference between supply + logistics
   Both disruptions affect fuel supply chain simultaneously, with no buffer inventory

Estimated Impact:
- Flight Operations: 8-12% cancellations on international routes
- Revenue Impact: $15-25M weekly loss
- Timeline: 2-3 weeks peak impact, 4-6 weeks stabilization

Recommended Mitigations:
1. Increase fuel inventory to 7-day buffer (cost: $5M, reduces risk 30%)
2. Reroute shipments via Cape of Good Hope (+15% cost, +2 weeks)
3. Negotiate spot contracts with alternative suppliers (Singapore, Korea)
4. Reduce frequency on low-margin routes temporarily

Confidence: 0.75
```

### Use Case 2: US Sanctions on Huawei

**Event**: US adds Huawei to Entity List, banning tech exports

**Step 1: Tension Encoding**
```json
{
  "tensions": {
    "legal_barrier": 1.0,
    "compliance_risk": 0.85,
    "supply_shortage": 0.70,
    "uncertainty": 0.90
  },
  "affected_entities": ["/org/cn/huawei"],
  "confidence": 1.0
}
```

**Step 2: Wave Propagation (Legal Barrier Upstream)**
```
/org/cn/huawei (sanctioned)
  legal_barrier: 1.0

  ↓ OWNED_BY (ownership=1.0, attenuation=0.95) [backward]
  [Transformation: legal_barrier → compliance_risk × 0.8]

/org/cn/huawei-investment-holdings (parent)
  compliance_risk: 1.0 × 0.95 × 0.8 = 0.76

  ↓ PARTNER_WITH (partnership_revenue=0.35, attenuation=0.95)
  [Amplification: China jurisdiction × 1.5]

/org/tw/tsmc (chip supplier)
  compliance_risk: 0.76 × 0.35 × 0.95 × 1.5 = 0.38
```

**Step 3: Supply Shortage Propagates Down**
```
/org/cn/huawei
  supply_shortage: 0.70 (can't get US chips)

  ↓ SUPPLIES_TO (dependency=0.80, attenuation=0.75)

/org/cn/china-telecom (5G network)
  supply_shortage: 0.70 × 0.80 × 0.75 = 0.42
```

**Step 4: Interpretation at TSMC**
```
Risk Interpretation for TSMC:

Summary:
TSMC faces MEDIUM compliance risk (0.38) from US sanctions on Huawei. Legal
restriction propagated via Huawei ownership structure (2 hops) and partnership
ties. High uncertainty (0.90) regarding enforcement scope and secondary sanctions.
Revenue impact: $8-12B annually if forced to cease Huawei supply.

Mitigations:
1. Seek USTR exemptions for legacy chips (<7nm)
2. Diversify client base (Apple, AMD, Nvidia already 60% of revenue)
3. Enhance compliance monitoring systems
4. Geographic diversification (Arizona fab reduces China exposure)

Confidence: 0.80
```

---

## Implementation Roadmap

### Phase 1: Foundation (3-4 weeks)

**Goal**: Minimum viable tension field system

**Deliverables:**
- [ ] TensionField and TensionWave models + Firestore schema
- [ ] TensionEncoder service (LLM-based event → tension vector)
- [ ] Basic WavePropagator (single tension category, simple BFS)
- [ ] TensionFieldRepository (CRUD operations)
- [ ] Manual event ingestion UI (analyst can input events)

**Success Criteria:**
- Can manually create an event (e.g., "Factory fire")
- LLM encodes it into tension vector
- Tension propagates 2-3 hops with attenuation
- Can query TensionField for any node and see tension values

### Phase 2: Multi-Category Propagation (2-3 weeks)

**Goal**: Support all major tension categories with rules

**Deliverables:**
- [ ] WavePropagationRule model + rule engine
- [ ] 5-6 core rule sets (supply shortage, legal barrier, geopolitical, financial, logistics)
- [ ] Metric-weighted decay (use edge metrics like supply_dependency)
- [ ] Contextual amplification (JIT, single-source, infrastructure tags)
- [ ] Category transformations (legal_barrier → compliance_risk at certain nodes)
- [ ] Threshold pruning (stop propagation when amplitude < 0.05)

**Success Criteria:**
- Can propagate all major tension types
- Decay respects edge metrics
- Amplification rules work (e.g., JIT manufacturing gets higher impact)
- Transformations work (legal barriers become compliance risks upstream)

### Phase 3: Interference & Resonance (2 weeks)

**Goal**: Emergent pattern detection

**Deliverables:**
- [ ] InterferenceCalculator service
- [ ] Resonance computation (constructive interference patterns)
- [ ] Destructive interference (tension cancellation)
- [ ] ResonanceScore field on TensionFields
- [ ] Phase coherence calculation (timing-based interference)

**Success Criteria:**
- When supply_shortage + logistics_block hit same node, resonance computed
- Resonance score accurately reflects amplification
- Can identify "perfect storm" scenarios algorithmically

### Phase 4: Lazy Interpretation (2-3 weeks)

**Goal**: Human-readable risks only when needed

**Deliverables:**
- [ ] ThresholdDetector service (scan for high-stress nodes)
- [ ] VIP node identification (fortune 500, critical infrastructure, user portfolio)
- [ ] LazyInterpreter service (LLM-based tension → risk description)
- [ ] RiskInterpretation model + storage
- [ ] Provenance trail builder (trace tensions back to source events)

**Success Criteria:**
- System automatically detects nodes with composite stress > 0.7
- VIP nodes monitored continuously (any tension > 0.1 triggers interpretation)
- User can query "What are risks for Tesla?" and get human-readable summary
- Interpretation includes provenance (which events caused these tensions)

### Phase 5: Temporal Dynamics (2 weeks)

**Goal**: Tensions evolve over time

**Deliverables:**
- [ ] Time-decay functions (tensions naturally fade)
- [ ] Scheduled decay batch job (nightly recalculation)
- [ ] Wave expiration (old waves auto-removed)
- [ ] Temporal queries ("What were risks for Tesla on 2024-01-01?")
- [ ] Tension timeline visualization (show how stress evolved)

**Success Criteria:**
- Tensions decay realistically (high initial impact, gradual fade)
- Old events don't pollute current risk view
- Can replay history ("time travel")

### Phase 6: Event Integration (2-3 weeks)

**Goal**: Automated event ingestion

**Deliverables:**
- [ ] News API integration (RSS feeds, webhooks from Bloomberg/Reuters)
- [ ] LLM-based event classification (article → tension encoding)
- [ ] Entity resolution (news text → canonical paths)
- [ ] Metric-based event triggers ("revenue drops 30%" → demand_shock)
- [ ] Analyst review queue (approve/reject/edit LLM encodings)

**Success Criteria:**
- News article about supply disruption auto-creates tension
- LLM correctly classifies event type 80%+ of time
- Entities in article resolved to canonical paths
- Analyst can review and correct before propagation

### Phase 7: Advanced Features (3-4 weeks)

**Goal**: Palantir-like analytics

**Deliverables:**
- [ ] Tension heatmap UI (color nodes by composite stress)
- [ ] Wave propagation animation (visualize spreading over time)
- [ ] Provenance path explorer (click node → see tension sources)
- [ ] Counterfactual simulator ("what if we remove this edge?")
- [ ] Opportunity detection (identify positive tensions for user portfolio)
- [ ] Risk reports (PDF/email summaries for key nodes)

**Success Criteria:**
- Analysts can visualize full tension propagation
- "What if" simulations show delta in stress scores
- Risk reports automatically generated for VIP nodes weekly

---

## Technical Decisions & Trade-offs

### Decision 1: Physics Model vs Semantic Propagation

**Choice**: Tension field physics (domain-agnostic categories) in middle layer

**Rationale:**
- **Scalability**: Pure math propagation can handle millions of nodes
- **Emergence**: Interference creates patterns not explicitly programmed
- **Debuggability**: Every tension has traceable amplitude and provenance
- **Separation of concerns**: Semantics only at endpoints (encoding & interpretation)

**Trade-off**: Initial encoding requires LLM (cost), but middle layer is free (math).

### Decision 2: Lazy vs Eager Interpretation

**Choice**: Lazy interpretation (only when threshold/VIP/query triggers)

**Rationale:**
- **Cost**: LLM calls are expensive; don't interpret millions of low-stress nodes
- **Relevance**: Most nodes have negligible tensions; interpretation is wasted
- **Freshness**: Interpretations can go stale; better to generate on-demand

**Trade-off**: Slight latency when user queries (must interpret on the fly), but acceptable for analytics workload.

### Decision 3: Continuous Field vs Event-Driven Snapshots

**Choice**: Continuous tension field (values, not events)

**Rationale:**
- **Accumulation**: Multiple events create overlapping tensions (interference)
- **Decay**: Tensions naturally fade over time (temporal dynamics)
- **Queries**: "What is current stress?" is easier than replaying event log

**Trade-off**: More storage (TensionField per node), but enables richer analytics.

### Decision 4: BFS vs Dijkstra/A*

**Choice**: BFS with threshold pruning

**Rationale:**
- **Multiple paths**: Tensions spread through ALL paths, not just shortest
- **Simplicity**: BFS is easy to implement and understand
- **Threshold pruning**: Achieves same goal as heuristic search (stop when weak)

**Trade-off**: Slightly more computation than A*, but we want all propagation paths anyway.

### Decision 5: Firestore vs Graph Database

**Choice**: Firestore for TensionFields, existing edges for propagation

**Rationale:**
- **Existing infrastructure**: Edges already in Firestore, reuse
- **Flexibility**: TensionFields are documents, easy to evolve schema
- **Scalability**: Firestore scales horizontally, proven at scale

**Trade-off**: Graph traversal is slightly slower than native graph DB, but acceptable with IndexPaths optimization.

---

## Appendix: Tension Category Reference

### Complete Tension Taxonomy

```
Physical Flow Tensions:
  - supply_shortage
  - supply_glut
  - logistics_block
  - capacity_strain

Financial Flow Tensions:
  - credit_stress
  - liquidity_crunch
  - demand_shock
  - price_volatility

Legal & Regulatory Tensions:
  - legal_barrier
  - compliance_risk
  - regulatory_shift

Information Tensions:
  - reputational_wave
  - uncertainty
  - information_asymmetry

Operational Tensions:
  - resource_scarcity
  - technical_failure
  - labor_dispute

Geopolitical Tensions:
  - conflict
  - political_instability
  - diplomatic_friction
```

### Severity Scale (Composite Stress)

```
0.9-1.0  CRITICAL   Immediate, severe impact; multiple high tensions + resonance
0.7-0.9  HIGH       Significant impact; requires urgent attention
0.4-0.7  MEDIUM     Moderate impact; monitor closely
0.2-0.4  LOW        Minor impact; watch for escalation
0.0-0.2  MINIMAL    Negligible impact; informational only
```

### Resonance Scale

```
0.0-0.5  NONE       No meaningful interference
0.5-1.0  LOW        Mild amplification
1.0-1.5  MEDIUM     Notable amplification
1.5-2.5  HIGH       Strong amplification ("perfect storm" forming)
2.5+     CRITICAL   Extreme amplification (multiple severe tensions overlapping)
```

---

## Glossary

**Amplitude**: Strength of a tension wave (0.0-1.0), analogous to wave height in physics.

**Attenuation**: Reduction in amplitude as wave propagates through an edge (like signal loss in transmission).

**Canonical Path**: Deterministic, human-readable identifier for entities (e.g., `/org/us/tesla`, `/loc/ae/na/na`).

**Composite Stress**: Weighted average of all tensions on a node, plus resonance bonus.

**Destructive Interference**: When opposing tensions cancel each other out (e.g., supply glut offsets supply shortage).

**Interference**: When multiple tension waves meet at a node; can be constructive (amplify) or destructive (cancel).

**Lazy Interpretation**: Only generating human-readable risk descriptions when needed (threshold, VIP node, or query).

**Provenance**: Traceable history of how a tension arrived at a node (source event → hops → current node).

**Resonance**: Amplification of tensions due to constructive interference (like acoustic resonance).

**Tension Category**: Domain-agnostic primitive (e.g., `supply_shortage`, `legal_barrier`) that describes type of disruption.

**Tension Field**: Collection of all tensions currently present at a node, analogous to electromagnetic field in physics.

**Tension Wave**: Single instance of a tension propagating from source event through graph, carrying provenance.

**Threshold Pruning**: Stopping propagation when amplitude drops below minimum (e.g., 0.05).

**Transformation**: Change in tension category during propagation (e.g., `legal_barrier` → `compliance_risk` at partners).

---

**End of Document**

*This architecture represents a paradigm shift from event-driven risk propagation to continuous tension field physics. By separating semantic interpretation from propagation mechanics, we achieve scalability, emergence, and debuggability—creating a true "Palantir for tensions."*
