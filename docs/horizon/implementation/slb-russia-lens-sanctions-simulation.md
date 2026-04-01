# SLB Russia Lens Sanctions Simulation

## Lens

- `lensId`: `l8dOd17uKYYTKD0CI9hy`
- name: `Slb`
- tenant: `igor@nordicvision.org`
- project: `DRGeH8tCC0EIoRpLlv3D`

## Goal

Take the existing SLB lens and run a sanctions/compliance stress scenario against the existing graph first, without manually adding new graph objects. If the result is weak, identify the delta, fix pipeline/runtime logic, and only then decide whether graph enrichment is actually required.

## What Already Existed In The Lens

The lens already contained a meaningful structure before any manual intervention:

- `/org/us/slb`
- `/loc/ru`
- `/domain/v1/supply-chain/geo/ru/subject/slb`
- `/domain/v1/policy-impact/geo/ru/topic/sanctions`
- `/market/ru/energy/oilfield-services`
- `/org/ae/schlumberger-middle-east`
- `/fac/ae/jebel-ali-logistics-center`

The lens also already carried strong supply-chain intent in `domainQueries` and `viewConfig`, including:

- `SLB Oilfield Equipment and Spare Parts Supply Chain to Russia`
- supply-chain execution plan anchored on `/org/us/slb`
- upstream geographies: `ru`, `cn`, `ae`, `tr`, `kz`, `in`
- products: oilfield equipment, pumps, drill bits, telemetry tools, chemicals
- policy-impact domain for sanctions on Russian operations

`viewConfig` also contained contract-specific path hints and summaries for:

- Jebel Ali transshipment
- Tyumen pump and specialty-chemicals facilities
- Rosneft supply base
- Kazakhstan liquid-mud and phosphate nodes
- China equipment and chemical manufacturing nodes

That matters because it means the graph was not empty. The first weak result came from under-consuming this lens structure.

## Scenario

External signal:

> Secondary sanctions tighten around SLB-related equipment, spare parts, well-completion systems, pumps, telemetry tools, and drilling chemicals flowing into Russia through third-country hubs. How will legal/compliance, settlement, physical flow, and external interpretation react?

Contours:

- `legal/compliance`
- `financial/settlement`
- `physical_flow/supply`
- `info/reputation`

## Baseline Run: Existing Lens Only, Before Fixes

Initial runs:

- `legal`: `0bmQPbt93WEOr2dVYfXd`
- `financial`: `nQahzb2zZRJru4cm74vi`
- `physical`: `iDz3Eus6WAw3LUDNm49s`
- `info`: `zvM1UeZyodGN3UElAH2Y`

Result quality was weak.

### Baseline metrics

Legal:

- `risk_score=0.193`
- `policy_pressure=0.143`
- `narrative_volatility=0.242`

Financial:

- `risk_score=0.232`
- `price_pressure=0.176`
- `policy_pressure=0.138`

Info:

- `risk_score=0.209`
- `narrative_volatility=0.381`
- `policy_pressure=0.123`

Physical:

- one-world tail, but completed worlds were still flat and generic

### Why baseline was weak

The actor layer showed the problem immediately:

- generic fallback roles like `regulator`, `firm`, `consumer`, `media`
- mixed ad hoc LLM classes like `CorporateCompliance`, `LogisticsManagement`, `Manufacturer`, `StateIndustrialist`
- weak decision layer
- almost no functional separation between sanctions, routing, procurement, and localization

The graph was not the primary problem.
The simulation runtime was not reading enough of the lens and had no scenario-native overlay for this case.

## Delta Diagnosis

Three concrete issues were identified.

### 1. Lens bootstrap was not deep enough

The simulation service only harvested a narrow `viewDirective`, but this lens stores a lot of useful graph hints deeper inside `viewConfig`, including contract-specific:

- `FocusNodePaths`
- `HighlightPaths`
- `ResearchPlan`

So the simulation lost a large part of the authored lens intent.

### 2. Supply-chain anchors were polluted by non-path values

`extractLensSupplyChainAnchors(...)` was appending raw `productFilter` and `geoFilter` values like:

- `oilfield-equipment`
- `ru`
- `ae`

These are not canonical graph paths. They polluted fallback seed ordering and weakened baseline scope selection.

### 3. No scenario-native SLB/Russia sanctions overlay existed

The runtime already had curated overlays for:

- `kzvat`
- `tesla_battery_logistics`
- `space_satellite_sanctions`

But nothing for the SLB Russia sanctions/transshipment/localization case. So even a good lens still fell into a mostly generic swarm.

## Fixes Applied

### Pipeline/runtime fixes

Implemented in [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go):

- recursive harvesting of path-bearing nodes from nested `viewConfig`
- preservation of lens-authored scope order when building seed paths
- filtering of supply-chain anchor extraction to canonical path-like refs only

This makes the lens bootstrap self-improving for future cases, not just this one.

### New SLB scenario overlay

Added:

- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/slb_russia_fallback.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/slb_russia_fallback.go)
- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/scenarios/slb_russia_sanctions.json`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/scenarios/slb_russia_sanctions.json)

This introduced a scenario-native agent layer for:

- `export_control_officer`
- `sanctions_compliance_officer`
- `transshipment_compliance_officer`
- `slb_russia_procurement_lead`
- `jebel_ali_hub_operator`
- `tyumen_pump_assembly_operator`
- `tyumen_chemicals_operator`
- `china_equipment_supplier`
- `kazakh_chemical_supplier`
- `russian_oilfield_customer`
- `ofs_settlement_bank`
- `trade_credit_insurer`
- `parallel_import_broker`
- `market_intelligence_analyst`
- `pr_agent`

The action layer also became scenario-specific:

- `request_exemption`
- `enforce_rule`
- `reroute_shipment`
- `reserve_inventory`
- `allocate_capacity`
- `reprioritize_flow`
- `hedge_cost`
- `adjust_guidance`

### Discipline/role expansion

Extended in [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/discipline_layer.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/discipline_layer.go):

- trade compliance mapping for SLB-specific agent classes
- role overlays for `Compliance Lead`, `COO`, `PR Lead`
- SLB-specific monitoring signals:
  - `counterparty_screening_queue`
  - `jebel_ali_reexport_delay`
  - `localization_fill_rate`
  - `russian_customer_slippage`
  - `trade_credit_cover_shift`

### Supply-chain toolchain fix

Fixed in [`/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_lens_path_bootstrap/main.go`](/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_lens_path_bootstrap/main.go):

- wrong repair target path `./cmd/tools/horizon_supply_repair`
- corrected to `./backend/cmd/tools/horizon_supply_repair`

This did not create the quality jump by itself, but it fixed a real defect in the graph/toolchain path.

## Graph Enrichment Decision

No manual graph enrichment was needed to reach a good result.

That is an important conclusion.

The lens already encoded enough of the world:

- supply-chain domain
- sanctions domain
- hubs
- localized facilities
- products
- Russian customer-side nodes

The main failure was in runtime interpretation and scenario mapping, not in the absence of core graph objects.

The repaired lens bootstrap tool also showed that a large repair pass could now run correctly. For this pass, I did not wait for a full long-running repair import to complete because the scenario quality was already materially improved without manual graph growth.

## Improved Run: Same Lens, Better Runtime

Improved runs:

- `legal`: `pf73YfvpDDRrEyYWlYf5`
- `financial`: `pDoneuE2ogYZuON4rdsu`
- `physical`: `dnM95U1gTmJ3H3gNMFHV`
- `info`: `fO9tZNWJAdgUkYjQNa1x`

Extra physical retry:

- `en65yzPJOiOQx0AB7eDz`

### Legal / compliance contour

- `risk_score=0.625`
- `policy_pressure=0.975`
- `narrative_volatility=0.594`
- `supply_stress=0.281`

Dominant mechanisms:

- `enforce_rule`
- `request_exemption`
- `issue_statement`
- `broadcast_message`

Top reacting actors:

- `slb_russia_procurement_lead`
- `export_control_officer`
- `sanctions_compliance_officer`
- `jebel_ali_hub_operator`
- `transshipment_compliance_officer`
- `pr_agent`

This is now a true sanctions/compliance loop.

### Financial / settlement contour

- `risk_score=0.532`
- `price_pressure=0.642`
- `narrative_volatility=0.419`
- `policy_pressure=0.324`

Dominant mechanisms:

- `adjust_budget`
- `hedge_cost`
- `reserve_inventory`
- `adjust_guidance`
- `issue_statement`

Top reacting actors:

- `ofs_settlement_bank`
- `slb_russia_procurement_lead`
- `sanctions_compliance_officer`
- `jebel_ali_hub_operator`
- `parallel_import_broker`
- `trade_credit_insurer`

This is now a real financing-and-resilience contour instead of generic “cost stress”.

### Info / reputation contour

- `risk_score=0.566`
- `narrative_volatility=0.914`
- `policy_pressure=0.874`

Top reacting actors:

- `export_control_officer`
- `russian_oilfield_customer`
- `sanctions_compliance_officer`
- `market_intelligence_analyst`
- `pr_agent`
- `trade_credit_insurer`

This is now a meaningful external-interpretation contour:

- the issue becomes visible not only as “sanctions pressure”
- it becomes a narrative about whether SLB can still control and explain its Russia-linked operating model

### Physical contour

The physical contour showed a persistent one-world tail, but the completed worlds were highly consistent.

Across 7 completed worlds:

- dominant mechanisms were stable:
  - `allocate_capacity`
  - `delay_purchase`
  - `reprioritize_flow`
  - `reroute_shipment`
  - `reserve_inventory`
- `price_pressure` converged around `0.832-0.850`
- `risk_score` converged around `0.450-0.471`
- `policy_pressure` stayed comparatively low in the physical contour
- `supply_stress` converged to `0`

Interpretation:

- the system does not read this as a raw physical collapse
- it reads it as an expensive rerouting/localization/reprioritization problem
- the physical system survives by pushing pain into price, sequencing, and adaptation rather than into binary shutdown

That is a materially better operational answer than the baseline.

## What Changed In Human Terms

Before the fixes, the system was effectively saying:

- “some legal and narrative pressure exists”

After the fixes, it says something much closer to the real management problem:

- sanctions create a compliance and exemption queue
- that queue changes what Jebel Ali can re-export and when
- procurement reacts by switching suppliers and reprioritizing flows
- localized Tyumen nodes absorb some pressure instead of everything breaking at the border
- finance reacts through hedging, buffer discipline, and guidance
- PR and market-intelligence loops amplify or stabilize the external interpretation depending on whether operating reality is clearly communicated

That is the right shape for this lens.

## C-level Interpretation

### Cross-functional view

The most likely SLB Russia scenario is not immediate supply collapse.

It is a four-part adaptation:

1. Compliance gating tightens.
Sanctions screening, exemptions, and transshipment controls move into the critical path.

2. Routing and procurement become defensive.
The system shifts from “import directly” to “screen, reroute, reserve, reprioritize”.

3. Localized Russian nodes absorb pressure.
Tyumen assembly and specialty-chemicals facilities act as shock absorbers.

4. Financial and reputational cost rises before physical failure.
Treasury, insurers, and PR react earlier than a naïve physical-flow model would expect.

### Compliance Lead readout

What the swarm is saying:

- the core bottleneck is now `screening -> exemption -> transshipment permission`
- compliance cannot sit outside operations
- if screening and exemptions are not centralized, the system expects confusion between procurement, routing, and customer commitments

Most important decision demands:

- `Counterparty screening and license triage`
- `Regulatory pathway management`
- `Stakeholder narrative alignment`

Operational meaning:

- compliance is no longer a review layer
- it is part of the flow-control engine

### CFO readout

What the swarm is saying:

- the financial problem is not “pay more for imports” in a simple sense
- the real issue is that resilience itself becomes expensive

Why:

- `ofs_settlement_bank` and `trade_credit_insurer` are among the most active actors
- dominant actions are `hedge_cost`, `adjust_budget`, `adjust_guidance`

Operational meaning:

- cash discipline, trade-credit conditions, insurance appetite, and guidance credibility must be run together
- otherwise the business may preserve flow operationally while losing control financially

### COO readout

What the swarm is saying:

- this is a re-export and localization control problem
- not a single import bottleneck story

Why:

- physical worlds converge on `allocate_capacity`, `reroute_shipment`, `reserve_inventory`, `reprioritize_flow`
- the system prefers adaptation through reconfiguration, not collapse

Operational meaning:

- COO should treat Jebel Ali, Tyumen pump assembly, Tyumen specialty chemicals, and Russian customer scheduling as one coupled operating loop
- the main question is not “can imports continue at all?”
- it is “which combinations of routing, local fill, and customer allocation are worth protecting first?”

### PR Lead readout

What the swarm is saying:

- PR does not manage optics after the fact
- it manages whether the market story stays aligned with the actual flow-control decisions

Why:

- `pr_agent`, `market_intelligence_analyst`, and `trade_credit_insurer` become highly active in the info contour
- narrative pressure is high precisely when policy pressure is high

Operational meaning:

- if the public line outruns real screening, routing, or localization capacity, the reputational problem compounds the sanctions problem

## Bottom Line

This lens did not need manual graph population first.

It needed:

- better lens-to-simulation scope harvesting
- cleaner supply-chain anchor logic
- a scenario-native `slb-russia` overlay
- stronger discipline-aware interpretation for compliance, operations, finance, and PR

After those changes, the same existing lens became a usable C-level simulation surface.

That is the important system result:

- the graph was already materially useful
- the missing piece was how the runtime consumed and enacted it
