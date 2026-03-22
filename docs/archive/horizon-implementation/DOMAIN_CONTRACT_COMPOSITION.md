# Domain Contract Composition

## Goal

The graph should not treat domain contracts as competing extraction modes.
They should compose into one execution model:

- `supply_chain` builds operational, product-carrying paths.
- `governance` overlays ownership/control/admin structure on the same anchors.
- `assurance` overlays certification/audit/regulatory supervision.
- `market` overlays market participation and segment structure.
- `policy_impact` overlays instruments, constraints, mechanisms, and transmission conditions.
- `strategic` overlays access-control and decision bottlenecks.
- `ecosystem` remains interpretive context and should not dominate propagation.

The end state is `fragment-grounded, path-executed, overlay-composed`.

## Current State

The codebase already had three pieces of this design:

1. `DomainMode` allowlists in
   - `/Users/kirisyuk/cursor/sanskrite/backend/internal/taxonomy/domain_mode.go`
2. runtime contract layering notes in
   - `/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/scout/contract_layers.go`
3. materialized path artifacts in
   - `/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/pathbuilder/supply_chain_builder.go`
   - `/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/materialized_artifact.go`

What was missing was a single explicit contract-semantics layer explaining:

- which domain modes are `path-carried` versus `overlay` versus `interpretive`
- which modes are allowed to complement each other
- which order they should execute in
- which edge families belong to each layer

## What Was Added

A new central package:

- `/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/contractsemantics/semantics.go`

It defines:

- graph layers:
  - `core_propagation`
  - `path_carried`
  - `contract_overlay`
  - `interpretive`
- domain-mode profiles
- execution ordering
- edge-layer classification
- complementarity rules

## Contract Composition Rules

### 1. Supply chain is the operational substrate

`supply_chain` is the only domain mode that should build product-carrying path structure.

It owns:

- `PRODUCES`
- `SUPPLIES_TO`
- `TRANSFORMS_INTO`
- `EXTRACTS_FROM`

It may also attach structural context to those paths via:

- `LOCATED_IN`
- `OPERATES_IN`
- `PARTICIPATES_IN`
- `HAS_MARKET_SEGMENT`

But those are not the core flow semantics.

### 2. Governance complements supply chain; it does not rewrite it

`governance` overlays:

- `OWNS`
- `SUBSIDIARY_OF`
- `CONTROLS`
- `MANAGES`
- `INVESTS_IN`
- `BOARD_MEMBER_OF`
- `EMPLOYED_BY`
- `FOUNDED`
- `ADMINISTERS`
- `ASSURES`

These edges should enrich the same entities and paths that supply chain already discovered.
They must not replace product flow semantics with ownership semantics.

### 3. Policy impact acts on paths and entities; it is not itself the path

`policy_impact` overlays:

- `REGULATES`
- `CONTROLS_ACCESS`
- `IMPOSES_CONDITION`
- `GATEKEEPS_DECISION`
- `USES_MECHANISM`
- `APPLIES_TO`
- `ADMINISTERS`
- `PARTICIPATES_IN`
- `LOCATED_IN`
- `OPERATES_IN`

It may also emit weaker interpretive edges:

- `INFLUENCES`
- `DEPENDS_ON`

These are useful for explanation and gating, but should not overwrite path-carried supply semantics.

### 4. Ecosystem is context, not the main propagation backbone

`ecosystem` remains valuable for context and discovery, but should be treated as interpretive-first.
It is allowed to enrich, but should not dominate long-tail propagation over operational paths.

### 5. Domain modes execute in a deterministic order

Current ordered composition:

1. `supply_chain`
2. `governance`
3. `assurance`
4. `market`
5. `policy_impact`
6. `strategic`
7. `ecosystem`
8. `general`

This order is now enforced where research and critic composition happen, so overlays sit on top of operational paths instead of racing them.

## Product-Carrying Materialized Paths

`SUPPLY_CHAIN_PATH` now needs to be treated as more than a list of nodes.

Materialized edges should say:

- which product is carried
- which operational segment they came from
- whether they are true product flow or just transformation semantics
- what best-effort metrics were carried from the source `SUPPLY_LINK`

This is now represented directly on `SupplyChainPathEdge` in:

- `/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/materialized_artifact.go`

Key fields:

- `productNodeIndex`
- `productRef`
- `supplyLinkId`
- `segmentIndex`
- `carriesProduct`
- `role`
- `metrics`
- `qualifiers`

Operational meaning:

- `SUPPLY_LINK` edge with `role=product_flow` means a real carried product between supplier and customer.
- `LOGISTICS` edge with `role=logistics_leg` means the same product is being transported across intermediate facilities.
- `ONTOLOGY` edge with `role=product_transformation` means the product changed in the chain.

This is the necessary bridge for later path stress, regulatory impact, and endpoint interpretation.

## Why This Matters For Early Warning

The goal is not to show that an event in a region affects nearby obvious neighbors.
The goal is to detect distant, non-obvious business consequences.

That only works if the system can reason over:

- event signals
- affected zones
- route/corridor exposure
- product-carrying path segments
- governance/policy overlays on top of the same entities and paths

Examples:

- conflict in the Gulf -> chokepoint pressure -> LNG route stress -> European plant input risk -> downstream customers
- VAT increase -> policy instrument -> market/facility exposure -> path cost/throughput stress -> downstream demand/supply shift

## Live Findings After This Pass

A live RSS replay still shows the main blocker is not path semantics anymore.
It is graph debt:

- duplicate placeholders like `/fac/na/port-of-rotterdam`
- route placeholders like `/fac/na/europipe-ii`
- generic placeholders like `/fac/na/general-production`

A live path-binding probe still looks stable:

- `supply_links=7771`
- `flow_bound=480 (6.2%)`
- `zone_bound=4630 (59.6%)`
- `affected_bound=121 (1.6%)`
- `binding_strong=1101 (14.2%)`
- `stress_strong=9 (0.1%)`

This means the new contract/path semantics did not destabilize the existing binding layer.

## Remaining Systemic Gaps

### 1. Duplicate placeholder operational nodes

These still distort downstream path reasoning and seed selection.
They remain the main blocker between path semantics and endpoint interpretation.

### 2. Policy overlays do not yet materialize onto supply path edges

The system now has the right layering model, but regulatory/policy overlays still mostly exist as entity/edge overlays.
The next step is to bind them to materialized path legs and path stress.

### 3. Domain artifacts for governance/regulatory/ecosystem are still graph slices, not path-aware overlays

They carry relation graphs, but not ordered path semantics.
That is acceptable for MVP, but not for final early-warning quality.

## Next Steps

1. Extend materialized paths with explicit policy/governance overlays per leg.
2. Bind regulatory/governance overlays to `path stress`, not only to entity neighbors.
3. Keep removing duplicate placeholder facilities and ports so downstream tail detection becomes trustworthy.
4. Re-run live RSS audits after each cleanup wave and watch when distant business tails start to become non-trivial.
