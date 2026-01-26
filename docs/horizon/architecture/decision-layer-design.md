# Decision Layer Design

> **Status**: ✅ Implemented  
> **Date**: 2026-01-24 (RFC) → 2026-01-25 (Implemented)  
> **Context**: Triggered by user query analysis

## Triggering Query

```
"Explain who actually decides what in the AI-chip supply chain 
(from lithography tools to fabs to cloud deployment). 
For each decision point, list: 
(1) the agent, 
(2) their role, 
(3) the mechanism of control, 
(4) what they can block/enable, and 
(5) what evidence supports the claim."
```

This query reveals a gap: the current graph models **relations** (who is connected to whom), but not **decisions** (who decides what, and how).

---

## Core Insight: Two Layers of the Graph

### Current State: Relation Layer

The graph is a **snapshot of the world of relations**:
- ASML → SUPPLIES_TO → TSMC
- TSMC → PRODUCES → A16 chip
- US DoC → REGULATES → ASML

This answers: "Who is connected to whom?"

### Missing: Decision Layer

The query requires a **snapshot of decisions**:
- **Decision Point**: "EUV lithography export license"
  - **Agent**: US Bureau of Industry and Security (BIS)
  - **Role**: Gatekeeper
  - **Mechanism**: Export Administration Regulations (EAR)
  - **Can Block**: ASML → SMIC
  - **Evidence**: Entity List additions, October 2022

This answers: "Who decides what?"

---

## Proposed Architecture: Three Layers

```
┌─────────────────────────────────────────────────────────┐
│  DOMAIN LAYER (contract: what we want to know)          │
│  /domain/v1/power-dynamics/sector/ai-chips              │
├─────────────────────────────────────────────────────────┤
│  DECISION LAYER (context-dependent)                      │
│  DecisionPoint {                                         │
│    domain: "/domain/v1/power-dynamics/sector/ai-chips"  │
│    agent: "/gov/us/bis"                                  │
│    role: "gatekeeper"                                    │
│    mechanism: "/mechanism/regulatory/export-control"    │
│    controlsAccess: ["/org/nl/asml" → "/org/cn/smic"]     │
│    evidence: [...]                                       │
│  }                                                       │
├─────────────────────────────────────────────────────────┤
│  RELATION LAYER (static snapshot of the world)          │
│  /org/nl/asml → SUPPLIES_TO → /org/tw/tsmc              │
│  /gov/us/bis → REGULATES → /org/nl/asml                 │
└─────────────────────────────────────────────────────────┘
```

---

## Key Concept: Domains as Crystallization Points

Domains are not just query classifiers — they are **crystallization points** for the graph.
They already exist in the pipeline and graph; the proposal **extends their semantic role** rather than replacing or recreating them.

### Current Model (bottom-up)
- Graph grows from fragments to relations
- Create /org/nl/asml
- Create /org/tw/tsmc
- Create edge SUPPLIES_TO

### Proposed Model (top-down)
- Graph grows from question to answer
- Domain `/domain/v1/power-dynamics/sector/ai-chips` is created
- Domain requires: "find gatekeepers, decision points, mechanisms"
- System creates fragments that **answer the domain contract**

### Domains as First Candidates for Updates
- When a new query about "AI chip export controls" arrives → system finds existing domain
- Domain "knows" which entities belong to it
- Domain update → cascade update of its entities

```
┌──────────────────────────────────────────────────────────────┐
│  Domain: /domain/v1/power-dynamics/sector/ai-chips          │
│                                                              │
│  "Who decides what in AI chip supply chain?"                 │
│                                                              │
│  Requires expansion:                                          │
│  - DecisionPoint entities                                    │
│  - Agent entities with roles                                  │
│  - Mechanism entities                                         │
│  - CONTROLS_ACCESS edges                                     │
│  - Evidence for each claim                                    │
│                                                              │
│  Crystallizes:                                                │
│  ├── /decision/export-control/euv-to-china                   │
│  ├── /gov/us/bis (role: gatekeeper)                          │
│  ├── /mechanism/regulatory/export-control                    │
│  ├── /org/nl/asml (role: supplier, can_be_blocked)           │
│  └── /org/cn/smic (role: target, access-controlled)          │
└──────────────────────────────────────────────────────────────┘
```

---

## New Entity Type: /mechanism

**Mechanism** — a formalized way by which an agent exercises control.

### Namespace Structure

```
/mechanism/{type}/{slug}

Types:
- regulatory    — Export control, licensing, certification
- economic      — Pricing power, capacity allocation
- technological — IP lock-in, standards control
- contractual   — Exclusivity, long-term agreements
- political     — Sanctions, diplomatic pressure
```

### Examples

```
/mechanism/regulatory/export-control        # Export control
/mechanism/regulatory/licensing             # Licensing
/mechanism/regulatory/certification         # Certification
/mechanism/economic/pricing-power           # Pricing power
/mechanism/economic/capacity-allocation     # Capacity allocation
/mechanism/technological/ip-lock-in         # Patent lock-in
/mechanism/technological/standards-control  # Standards control
/mechanism/contractual/exclusivity          # Exclusive contracts
/mechanism/contractual/long-term-agreement  # Long-term agreements
/mechanism/political/sanctions              # Sanctions
/mechanism/political/diplomatic-pressure    # Diplomatic pressure
```

### Why First-Class Entity

1. **Reusability**: One mechanism is used by many agents
   - `/mechanism/regulatory/export-control` is used by US BIS, EU DG Trade, Japan METI

2. **Attributes**: Mechanism has its own properties
   - Type (regulatory/economic/technological/contractual/political)
   - Jurisdiction (where applicable)
   - Subject (what it controls)
   - Reversibility (can it be bypassed)

3. **Relationships**: Mechanism links agent to decision point
   ```
   /gov/us/bis --[USES_MECHANISM]--> /mechanism/regulatory/export-control
   /mechanism/regulatory/export-control --[CONTROLS_ACCESS]--> /product/.../euv-lithography
   ```

---

## New Entity Type: /decision (or DecisionPoint)

### Structure

```go
type DecisionPoint struct {
    ID           string   // /decision/{domain-slug}/{decision-slug}
    Domain       string   // /domain/v1/power-dynamics/sector/ai-chips (existing domain node; reused)
    Title        string   // "EUV lithography export to China"
    
    // Who decides
    Agent        string   // /gov/us/bis
    AgentRole    string   // "gatekeeper" | "approver" | "influencer" | "enforcer"
    
    // How they decide
    Mechanism    string   // /mechanism/regulatory/export-control
    
    // What is decided about
    Subject      string   // /product/.../euv-lithography
    
    // What can happen
    ControlsAccess []Edge // [/org/nl/asml → /org/cn/smic]
    Status       string   // "blocked" | "enabled" | "conditional" | "pending"
    
    // Provenance
    Evidence     []SourceRef
    Confidence   float64
    ValidFrom    *time.Time
    ValidTo      *time.Time
}
```

### Roles Taxonomy

```
gatekeeper   — Can block/enable access (export control, licensing)
approver     — Must approve before action (regulatory approval)
influencer   — Can affect but not block (lobbying, standards bodies)
enforcer     — Ensures compliance (audit, inspection)
arbiter      — Resolves disputes (courts, arbitration)
```

---

## Intent Resolver Refactoring

### Current Problem

The Intent Resolver prompt is ~235 lines and tries to do everything at once:
- Understand "what about" → domains
- Understand "about whom/what" → anchors
- Give hints on how to expand → decompositionHints

This is at the cognitive limit for LLMs.

### Proposed Solution: Two Agents

```
┌─────────────────────────────────────────────────────────────┐
│  DomainResolverAgent                                        │
│  - Input: query                                              │
│  - Output: []DomainResolution                                │
│  - Focus: "What contract? What does user want to understand?"│
│  - Short prompt (~60 lines)                                  │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│  AnchorResolverAgent                                        │
│  - Input: query + []DomainResolution                         │
│  - Output: []Anchor + []DecompositionHint                    │
│  - Focus: "What entities? How to expand?"                    │
│  - Domain context influences anchor type selection           │
│  - Prompt (~80 lines)                                        │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│  IntentMerger (deterministic)                                │
│  - Input: domains + anchors + hints                          │
│  - Output: IntentResolution                                  │
│  - Normalization, validation, deduplication                  │
│  - Code, not LLM                                             │
└─────────────────────────────────────────────────────────────┘
```

### Why Domain First

- Domain defines context ("power-dynamics" vs "supply-chain")
- AnchorResolver knows context → better entity type selection
- For `power-dynamics` → looks for GOV, PERSON with roles
- For `supply-chain` → looks for FACILITY, PRODUCT, ORG

---

## Risk Mitigation: Two-Phase Domain Execution

**Problem**: It is difficult for LLMs to extract abstract "decision points" in the early pipeline stages when context is sparse.

**Solution**: Split domain execution into two phases. The domain contract dictates both.

### Phase 1: Physical Layer Discovery (Traditional)
- **Goal**: "Draw the map."
- **Input**: Domain + Query.
- **Action**: Scout/Research identify entities (Org, Facility, Product) and "hard" relations (Supplies, Located, Owns).
- **Output**: **Relation Graph**.

### Phase 2: Strategic Layer Overlay (New)
- **Goal**: "Place flags on the map."
- **Input**: Domain + **Existing Relation Graph** (context).
- **Action**: 
  - Look at specific edges (e.g., `ASML -> SMIC`).
  - Ask: "Who can block this specific connection?"
  - Find: Agents (US BIS) and Mechanisms (Export Control).
  - Create: `DecisionPoint` entities linked to the Relation Graph.
- **Output**: **Decision Graph** overlay.

**Benefits**:
1.  **Grounding**: Strategic search is grounded in real, verified entities.
2.  **Focus**: LLM context window is filled with the graph structure, reducing noise.
3.  **Efficiency**: Targeted "deep dives" on critical nodes instead of broad scanning.

---

## Pipeline Impact

### 1. DomainResolver
- Recognizes: power-dynamics + sector/ai-chips
- Creates domain contract

### 2. AnchorResolver
- In context of power-dynamics:
  - Looks for GOV entities (regulators)
  - Looks for PERSON entities (decision makers)
  - Looks for control points in supply chain

### 3. Scout
- For power-dynamics domain:
  - Special prompt: "Extract decision points, mechanisms, controls-access"
  - Creates DecisionPoint, Mechanism entities
  - Creates CONTROLS_ACCESS, USES_MECHANISM edges

### 4. Research
- For power-dynamics domain:
  - Searches for evidence for each decision claim
  - Fills in mechanism details

### 5. Critic
- For power-dynamics domain:
  - Validates: is there agent? mechanism? evidence?
  - MissingInfo: "Who controls access to EUV maintenance?"

### 6. Policy
- Validates: DecisionPoint correctly linked to domain
- Validates: Evidence exists for each claim

---

## Implementation Constraint: Unified Canonicalization & Prompts

All agents and phases (Scout, Research, Critic, Policy, domain phase 1/2) **must reuse the same taxonomy, canonicalization, and prompt logic**.  
No parallel pipelines, no divergent rules, and no “almost the same” prompt forks.

**Implications:**
- **Canonical paths** for `/decision` and `/mechanism` must be validated by the **same** taxonomy validator used elsewhere (`ValidateCanonicalPath`).
- **Policy checks** apply uniformly to all new fragments/edges, regardless of phase.
- **Prompt templates** should be extended/composed, not duplicated (reuse core entity extraction rules, append domain-specific modules).
- **Domain phase 2** must route through the same normalization and validation functions as phase 1.

This prevents drift and keeps the graph consistent across all layers.

---

## Strategic Edge Types

Strategic relations require more nuance than simple binary "block/enable".

### 1. Control Access (Gatekeeping)
**`CONTROLS_ACCESS`**
- Universal "gatekeeper" relation.
- **Qualifiers**:
  - `type`: `block` (veto), `enable` (grant), `delay` (bureaucracy), `monitor` (surveillance)
  - `mechanism`: reference to mechanism ID
  - `condition`: what triggers the control

### 2. Impose Conditions (Soft Power)
**`IMPOSES_CONDITION`**
- Subject forces object to comply with rules/costs.
- **Qualifiers**:
  - `type`: `cost` (tariffs), `compliance` (ESG/standards), `technical` (specs)
  - `impact`: description of impact

### 3. Instrumental Relations
**`USES_MECHANISM`**
- Agent → Mechanism
- Shows which tools an agent possesses.

**`APPLIES_TO`**
- Mechanism → Entity/Edge
- Shows the scope of a mechanism.

---

## Domain Analysis "On-the-Fly"

To support user-driven strategic analysis, we introduce on-the-fly domain analysis for existing graphs.

**Scenario**:
1. User loads an existing Lens (graph already populated with entities/relations).
2. User asks a strategic question (e.g., "Who controls this supply chain?").
3. System triggers **Phase 2 (Strategic Overlay)** explicitly.
4. **Input**: Existing Lens Graph + New Domain Contract.
5. **Output**: New `DecisionPoint` and `Mechanism` nodes are added to the graph, enriching the existing view.

**Architectural Implication**:
- Phase 2 logic must be decoupled from the initial pipeline.
- It can be triggered as a standalone `ScoutingJob` targeting an existing Lens.
- This allows iterative enrichment: users can ask different strategic questions about the same graph, adding layers of insight.

### Scope & Deduplication

1. **Input Scope**: Nodes within the Lens scope + their immediate relations. Domain research extends the graph but **does not create duplicates**.
2. **Domain Context**: When launching domain addition, the LLM receives a list of **existing domains in nearby connections**. This prevents domain duplication. Enrichment of existing domains is allowed — dedup logic and freshness constraints (already in codebase) handle this.

### UI Flow (Command Palette)

This is a dedicated mode within the Command Palette with staged interaction:

1. **System prepares**: Retrieves nearby domains and current scope.
2. **User inputs**: Strategic question.
3. **System confirms**: Displays interpretation under the input field ("Understood as: power-dynamics analysis for AI chip supply chain").
4. **User approves**: Clicks OK.
5. **Execution**: Phase 2 job runs, enriching the graph with strategic layer.

---

## Open Questions

### 1. DecisionPoint as Fragment or Edge Annotation?

**Option A: Fragment**
- `/decision/{domain-slug}/{decision-slug}`
- First-class citizen of the graph
- Can have its own edges, state, history

**Option B: Edge Qualifier**
- Qualifier on CONTROLS/REGULATES edge
- Lighter weight
- Less flexibility

**Recommendation**: Fragment (Option A) — decisions are complex enough to warrant first-class status.

### 2. Mechanism — Global or Domain-Scoped?

**Option A: Global**
- `/mechanism/regulatory/export-control` is reused across domains
- Pro: Single source of truth
- Con: May lose domain-specific nuance

**Option B: Domain-Scoped**
- `/domain/.../mechanism/...` is domain-specific
- Pro: Captures context
- Con: Duplication

**Recommendation**: Global with domain-context edges — mechanism is global, but its APPLICATION in a specific domain is captured via edges.

### 3. Roles — On Fragment or In Domain Context?

**Option A: On Fragment**
- Agent `/gov/us/bis` has `role: regulator` globally
- Simple but loses context

**Option B: In Domain Context**
- Agent `/gov/us/bis` globally is "regulator"
- In context of power-dynamics/ai-chips → "gatekeeper"
- More accurate but more complex

**Recommendation**: Both — global role on fragment, context-specific role on DecisionPoint.

---

## Related Contract Types

The following existing contract types are relevant for decision analysis:

```go
case "power-dynamics":
    return "Expand into: influential actors, power centers, 
            decision networks, gatekeepers, and leverage points."

case "stakeholder-mapping":
    return "Expand into: key stakeholders, influence levels, 
            interests, alliances, and decision-making dynamics."

case "regulatory-impact", "policy-and-regulation":
    return "Expand into: regulatory bodies, policy frameworks, 
            compliance requirements, enforcement trends, and impact areas."
```

### Potential New Contract Type

```go
case "decision-analysis":
    return "Expand into: decision points, agents, mechanisms of control, 
            controls-access capabilities, and supporting evidence."
```

---

## Implementation Roadmap

### Phase 1: Intent Resolver Split ✓
- [x] Create DomainResolverAgent
- [x] Create AnchorResolverAgent  
- [x] Create IntentMerger (deterministic)
- [x] Migrate existing IntentResolverAgent

### Phase 2: Mechanism Entity ✓
- [x] Define /mechanism namespace in taxonomy (`paths.go`, `canonical_parts.go`)
- [x] Add FragmentTypeMechanism (`models/common.go`)
- [x] Add USES_MECHANISM edge type (`dependency_edge.go`)
- [x] Add APPLIES_TO edge type for mechanism scope
- [x] Update Scout prompts for mechanism extraction

### Phase 3: DecisionPoint Entity ✓
- [x] Define /decision namespace in taxonomy (`paths.go`, `canonical_parts.go`)
- [x] Add FragmentTypeDecisionPoint (`models/common.go`)
- [x] Add CONTROLS_ACCESS edge type with qualifiers (block/enable/delay/monitor)
- [x] Add IMPOSES_CONDITION edge type with qualifiers (cost/compliance/technical)
- [x] Add AgentRole taxonomy (`models/common.go`: gatekeeper, approver, influencer, enforcer, arbiter)
- [x] Add MechanismCategory enum (regulatory, economic, technological, contractual, political)
- [x] Add ControlAccessType enum (block, enable, delay, monitor)
- [x] Add ConditionType enum (cost, compliance, technical)
- [x] Update Scout prompts for decision extraction

### Phase 4: Domain-Aware Pipeline ✓
- [x] StrategicOverlayAgent: domain-specific prompts with full taxonomy rules
- [x] Critic: domain-specific validation for DECISION_POINT/MECHANISM
  - Missing CONTROLS_ACCESS/USES_MECHANISM edges → missingInfo
  - Orphan decision points/mechanisms → HIGH severity conflicts
  - Strategic edge qualifier validation
- [x] Policy: domain-specific enforcement
  - Edge direction rules for CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO
  - Qualifier validation for strategic edges (type is required)
  - Fragment path validation for /decision/ and /mechanism/
- [x] PreCreateValidator: path prefix rules for DECISION_POINT, MECHANISM

---

## References

- Existing contract types: `intent_resolver_agent.go:424-545`
- Domain expansion prompts: `intent_resolver_agent.go:613-702`
- Supply chain model: `supply-chain-influence-model.md`
- Policy enforcement: `policy_checker.go`
