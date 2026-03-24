# Space/Satellite Lens Sanctions Simulation

## Lens

- `lensId`: `UQPGaI89O3Mr2WQSMYXS`
- tenant: `igor@etigram.pro`
- name: `China Aerospace Science and Technology Corporation`
- mode: existing global lens, no manual graph seeding on first pass

## What Was In Scope Before Any Graph Edits

The run intentionally started from the existing lens scope and nodes already connected to it.

Primary scope paths already present in the lens:

- `/domain/v1/supply-chain/geo/brics-gcc/sector/space`
- `/fac/in/satish-dhawan-space-centre`
- `/gov/cn/cnsa`
- `/org/ru/roscosmos`
- `/gov/ae/uae-space-agency`
- `/gov/in/isro`
- `/market/brics/aerospace/satellite-services`
- `/gov/sa/saudi-space-commission`

Additional signal-relevant nodes already encoded in lens intent and view config:

- `/org/cn/casc`
- `/org/ae/mbrsc`
- `/loc/brics`
- `/fac/cn/beijing-rocket-manufacturing-plant`
- `/fac/cn/beijing-rocket-assembly-plant`
- `/fac/cn/beijing-liquid-rocket-engine-facility`
- `/fac/ru/khimki-plant`
- `/fac/ru/polyot-omsk`
- `/fac/cn/wenchang-port`
- `/product/finished/mechanical/assembly/launch-vehicles`
- `/product/finished/mechanical/assembly/rocket-engines`
- `/product/finished/electronic/assembly/small-satellites`

## Scenario

External signal:

> Secondary sanctions and export-control tightening across BRICS-GCC satellite and launch supply chains. What scenarios are likely for launch manifests, supplier access, settlement, procurement continuity, and customer confidence?

Contours used:

- `legal`
- `financial`
- `physical`
- `info`

## First Pass: Existing Lens Only, No Manual Graph Construction

Initial live runs:

- `legal`: `ZBUoqEDClZFkHbT4WGqy`
- `financial`: `ZBpHoPuZsWUZKbfFSbYE`
- `physical`: `SSlEZziwFCTbGdu9JWmq`
- `info`: `s3dJpjcpLDqwmd9J72qy`

Result quality was weak.

Observed problems:

- actor layer fell back to generic classes like `consumer`, `firm`, `bank`, `media`, `regulator`
- sanction/compliance semantics were not visible as a first-class discipline
- physical contour was too flat:
  - `risk_score=0.203`
  - `price_pressure=0.162`
  - `policy_pressure=0.093`
  - `supply_stress=0.073`
  - `narrative_volatility=0.301`

Conclusion:

- the lens itself already had meaningful scope
- the runtime was not consuming enough of that scope and was not mapping this lens into a space/satellite scenario overlay

## Delta Analysis

The quality gap was traced to three runtime issues rather than to an empty graph.

### 1. Lens bootstrap was too shallow

The simulation baseline bootstrap was not pulling enough signal-bearing paths from the lens:

- `deferredScopePaths`
- `viewDirective.focusNodePaths`
- supply-chain anchors from `domainQueries`
- anchor-like refs from materialized and scope artifacts

This meant the run lost a large part of the lens intent before the swarm ever started.

### 2. Scope ordering was degraded

The baseline builder compacted and sorted scope refs, which changed the effective seed priority. The first pass therefore seeded from a poor alphabetical subset instead of the lens-authored anchors.

### 3. No scenario overlay for space/satellite sanctions

The runtime preferred curated scenario populations only for earlier scenarios like `kzvat` and `tesla`. The satellites lens therefore fell through to generic fallback agents.

## System Fixes Applied

### Runtime fixes

Implemented in the simulation runtime:

- richer lens scope harvesting in Horizon simulation bootstrap
- scope ordering preservation
- new scenario overlay: `space_satellite_sanctions`
- new sanctions/compliance discipline and `compliance_lead` role view

New specialized agents now participating:

- `export_control_officer`
- `sanctions_compliance_officer`
- `launch_regulator`
- `roscosmos_program_office`
- `isro_program_office`
- `uae_space_procurement`
- `saudi_space_procurement`
- `launch_vehicle_manufacturer`
- `rocket_engine_supplier`
- `satellite_manufacturer`
- `launch_manifest_planner`
- `cross_border_logistics_operator`
- `satellite_services_customer`
- `aerospace_settlement_bank`
- `space_insurer`
- `procurement_compliance_lead`
- `market_intelligence_analyst`
- `pr_agent`

New compliance-capable action set now used in this scenario:

- `request_exemption`
- `enforce_rule`
- `issue_statement`
- `reprioritize_flow`
- `allocate_capacity`
- `reserve_inventory`
- `reroute_shipment`
- `expedite_shipment`
- `adjust_guidance`
- `hedge_cost`

### Supply-chain toolchain fix

One actual bug was found and fixed in the supply-chain bootstrap toolchain:

- [`/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_lens_path_bootstrap/main.go`](/Users/kirisyuk/cursor/sanskrite/backend/cmd/tools/horizon_lens_path_bootstrap/main.go)

It was incorrectly invoking:

- `./cmd/tools/horizon_supply_repair`

instead of:

- `./backend/cmd/tools/horizon_supply_repair`

This did not cause the weak simulation result directly, but it did break the lens repair workflow and masked whether supply repair could add anything useful.

## Supply-Chain Graph Inspection

After fixing the tool path, the lens bootstrap/repair tool was rerun against the same lens.

What it showed:

- the lens already had relevant path-bearing scope
- previous diagnostics had already found candidates but imported `0`
- earlier summary stayed at:
  - `paths=39`
  - `supplyLinks=21`
  - `scopeArtifacts=0`

Interpretation:

- for this scenario, the main quality issue was not the absence of core supply-chain nodes
- the bigger issue was that simulation bootstrap and scenario mapping were under-consuming the existing lens structure
- the supply-chain branch still had one operational defect in the repair command path, which is now fixed

## Second Pass: Same Lens, Better Runtime, No Manual Graph Seeding

Improved live runs:

- `legal`: `GRBWNepbi3nRVNE1qxLd`
- `financial`: `7DAvpOqQzsyDCzI8FmOK`
- `physical`: `ZFldTxam4I4cPdcuJu0Y`
- `info`: `6I54eoyuj6FyZ9l2aVKl`

### Legal / compliance contour

- `status`: `SUCCEEDED`
- `risk_score=0.497`
- `policy_pressure=0.824`
- `narrative_volatility=0.464`
- `price_pressure=0.155`
- `supply_stress=0.178`

Dominant mechanisms:

- `broadcast_message`
- `request_exemption`
- `delay_purchase`
- `adjust_budget`

Top reacting actors:

- `sanctions_compliance_officer`
- `procurement_compliance_lead`
- `pr_agent`
- `satellite_services_customer`
- `isro_program_office`
- `roscosmos_program_office`

This is now a real sanctions/compliance contour rather than a generic legal contour.

### Financial / settlement contour

- `status`: `SUCCEEDED`
- `risk_score=0.438`
- `policy_pressure=0.463`
- `price_pressure=0.297`
- `supply_stress=0.189`
- `narrative_volatility=0.300`

Top reacting actors:

- `procurement_compliance_lead`
- `export_control_officer`
- `roscosmos_program_office`
- `sanctions_compliance_officer`
- `launch_regulator`
- `launch_manifest_planner`
- `aerospace_settlement_bank`

This contour now reads as settlement-plus-license friction rather than generic finance pressure.

### Physical contour

- `status`: `SUCCEEDED`
- `risk_score=0.335`
- `price_pressure=0.458`
- `policy_pressure=0.119`
- `supply_stress=0.049`
- `narrative_volatility=0.206`

Dominant mechanisms:

- `allocate_capacity`
- `reprioritize_flow`
- `broadcast_message`
- `delay_purchase`

Top disciplines:

- `product_program` `0.313`
- `supply_operations` `0.283`
- `treasury_finance` `0.232`
- `market_demand` `0.229`
- `trade_compliance` `0.186`

This is the clearest proof that the upgraded runtime is using the lens properly: the contour is now operational and manifest-driven, not generic.

### Info contour

- `status`: `SUCCEEDED`
- `risk_score=0.324`
- `narrative_volatility=0.590`
- `policy_pressure=0.224`
- `price_pressure=0.132`
- `supply_stress=0.089`

Top reacting actors:

- `procurement_compliance_lead`
- `pr_agent`
- `launch_vehicle_manufacturer`
- `satellite_manufacturer`
- `rocket_engine_supplier`

This contour now separates narrative and commercial interpretation from the physical layer.

## What Changed in Human Terms

The improved system no longer says:

- "some regulation might upset firms and media"

It now says:

- sanctions and export controls create a licensing and screening queue
- that queue directly changes launch manifest sequencing
- procurement continuity becomes conditional on compliance state
- settlement, insurance, and customer confidence react to the same gating logic

This is the right shape for a space/satellite sanctions simulation.

## Role Interpretation

### Compliance Lead

What the swarm is saying:

- compliance is not a reporting layer; it is part of the operating critical path
- the biggest practical problem is not abstract “sanctions risk” but live triage:
  - which counterparties are clean
  - which launches need exemptions
  - which manifests can move now
  - which procurements should be delayed or re-routed

Why:

- `sanctions_compliance_officer` and `procurement_compliance_lead` are the most active actors in the legal contour
- top decision demands are:
  - `Counterparty screening and license triage`
  - `Regulatory pathway management`
  - `Stakeholder narrative alignment`

How to read this operationally:

- once sanctions/compliance friction starts, the system expects the bottleneck to move into screening and licensing queues
- if compliance decisions remain decentralized across procurement, launch planning, and communications, friction compounds

What to monitor weekly:

- `policy pressure`
- `overall risk`
- `narrative volatility`
- `Counterparty screening queue`
- `Launch license delay`
- `Insured launch premium`

### COO

What the swarm is saying:

- this is a manifest-and-allocation problem before it is a pure supply collapse problem
- operations should assume reordering, prioritization, and capacity protection rather than a single catastrophic failure

Why:

- physical contour is led by `allocate_capacity` and `reprioritize_flow`
- top disciplines are `product_program` and `supply_operations`
- top exposed functions are `operations`, `logistics`, `procurement`, `product`

How to read this operationally:

- launch manifests will be re-ranked by compliance feasibility, supplier availability, and confidence effects
- engineering/program offices are less likely to halt everything and more likely to protect selected flows
- procurement and manifest planning need to act as one loop, not two separate departments

What to monitor weekly:

- `price pressure`
- `overall risk`
- `supply stress variance`
- `manifest_slip_rate`
- `launch_license_delay`
- `customer_booking_slippage`

## Bottom Line

For this lens, the first weak result did not prove that the graph was poor. It proved that the simulation runtime was not fully consuming the graph and had no scenario-native overlay for sanctions/compliance.

After fixing that, the same existing lens became materially useful without first doing manual graph population.

The remaining graph/toolchain lesson is narrower:

- the supply-chain repair path in the lens bootstrap tool was broken and is now fixed
- if this lens later needs more depth, the next graph work should focus on richer materialized scope artifacts and facility-to-program bindings rather than blind node creation
