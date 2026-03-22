# Taxonomy Review For Early Warning

## Goal

The target is not generic knowledge graph completeness. The target is early warning:

1. ingest RSS and other deterministic signals;
2. normalize them into canonical facts/signals with minimal LLM use;
3. propagate those signals through a graph that behaves like an operational system, not a semantic encyclopedia;
4. surface non-obvious downstream business endpoints: facilities, companies, chokepoints, markets, dependencies.

That means taxonomy quality must be evaluated by one question:

`Does this type/relation improve operational propagation and endpoint ranking, or does it mainly add semantic noise?`

## Executive Summary

The taxonomy direction is mostly right. The strongest decisions are already in place:

- `CONSUMES` was correctly removed from the active supply-chain contract.
- `PARTICIPATES_IN` correctly absorbs many `ORG <-> MARKET` variants.
- `HAS_MARKET_SEGMENT` correctly replaces `HAS_SUBMARKET`.
- `ASSOCIATED_WITH` correctly absorbs weak peer-style aliases.
- transition aliases are already collapsed into `CORPORATE_TRANSITION`.

The current problems are more structural than nominal:

- too much graph mass still lands in abstract or scaffold layers;
- `DEPENDS_ON` is still semantically overloaded;
- `SUPPLY_LINK` was modeled as a fragment/artifact but not consistently materialized into traversable operational structure;
- strategic/policy taxonomy is useful, but must stay layered above operational propagation instead of competing with it;
- prompts and postprocessors still allow some branches to overproduce generic graph facts instead of propagation-useful facts.

## Before vs After Recent Changes

### Before

The graph behaved more like a mixed semantic warehouse:

- `SUPPLY_LINK` existed, but much of it was not traversable in propagation.
- abstract hubs such as `/sphere/*`, generic `/product/*`, and weak geo containers absorbed too much signal mass;
- seed resolution could select abstract or artifact nodes;
- OCR/document-markup noise polluted seed extraction;
- facility geo debt (`/fac/na/...` even when country known) degraded propagation specificity;
- coordinate authority was mixed and not aligned to business use.

In that state the taxonomy itself was not the only problem, but the way it landed in the graph made the graph too semantically broad and too operationally weak.

### After

The graph is materially closer to an early-warning system:

- `SUPPLY_LINK` participates in propagation via runtime overlay / virtual corridors;
- semantic seeding now rejects abstract/artifact-like nodes;
- OCR/document markup is filtered before semantic seeding;
- graph enrichment is centralized in repo/scout/backfill paths;
- propagation uses overlay roles, family-aware physics, endpoint scoring, path-family scoring, and typed long-tail mode;
- `AFFECTED_ZONE` metrics now act as real propagation priors rather than UI-only artifacts;
- geo authority is now aligned around `Google -> Nominatim`, not Wikidata/GeoNames coordinates.

This is the right direction: the taxonomy is becoming more executable.

## Taxonomy Assessment

### What Should Remain Core

These types and relations form the propagation-safe operational core.

#### Core node types

- `ORGANIZATION`
- `FACILITY`
- `PRODUCT`
- `DEPOSIT`
- `MARKET`
- `GEO_REGION`
- `AFFECTED_ZONE`
- `TRACK`
- `GOV`-namespaced organizations when they behave as regulators/operators

#### Core operational relations

- `PRODUCES`
- `SUPPLIES_TO`
- `TRANSFORMS_INTO`
- `EXTRACTS_FROM`
- `LOCATED_IN`
- `OPERATES_IN`
- `OWNS`
- `CONTROLS`
- `REGULATES`
- `PARTICIPATES_IN`
- `ASSURES`
- `TRIGGERED_BY`
- `AFFECTS` from `AFFECTED_ZONE` only when target is concrete and evidence-backed

These are the relations that actually support operational propagation, counterparty discovery, market exposure, and downstream routing.

### What Should Exist But As Overlay, Not As Propagation Core

These are valid graph semantics, but they should not dominate early-warning propagation.

#### Strategic / policy overlay

- `DECISION_POINT`
- `MECHANISM`
- `CONTROLS_ACCESS`
- `IMPOSES_CONDITION`
- `GATEKEEPS_DECISION`
- `USES_MECHANISM`
- `APPLIES_TO`

These are important for interpretation, sanctions, controls, and policy transmission. But they should run above the operational graph, not replace it.

#### Domain / scaffold layer

- `DOMAIN`
- `FOCUSES_ON`
- `INVOLVES_ACTOR`
- `IN_GEO_SCOPE`
- `ABOUT`
- `SURFACES`
- `RELATED_DOMAIN`
- `BROADENS_TO`
- `NARROWS_TO`
- domain-scaffold `DEPENDS_ON`

These are useful for research orchestration and controlled exploration. They should not be treated as normal operational propagation substrate.

#### Social / narrative / contextual layer

- `COMMUNITY`
- `MEDIA_CHANNEL`
- `OPERATES_VIA`
- `MEMBER_OF`
- `AFFILIATED_WITH` input form
- `RELATED_TO` input form
- `PARTNERS_WITH` input form
- `INFLUENCES` when used as soft narrative/abstract effect

These are helpful for interpretation and OSINT context, but weak for physical business propagation unless explicitly transformed into concrete operational dependencies.

## What Should Be Collapsed Or Treated As Input-Only

### Correct existing decisions

These should stay as-is.

- `CONSUMES` should remain deprecated/input-only. `SUPPLIES_TO` is the correct single source of truth for supply flow direction.
- `DOMINATES` should remain normalized into `PARTICIPATES_IN` with qualifiers/metrics.
- `RELATED_TO`, `AFFILIATED_WITH`, `PARTNERS_WITH` should remain collapsed into `ASSOCIATED_WITH` for canonical storage.
- `WORKS_FOR`, `KEY_PERSON`, `HAS_KEY_PERSON` should remain legacy aliases, not canonical output.
- `HAS_SUBMARKET` should remain deprecated in favor of `HAS_MARKET_SEGMENT`.
- `AUDITED_BY` should remain legacy-facing; `ASSURES` is the better canonical form.
- transition aliases should continue collapsing into `CORPORATE_TRANSITION`.

### Additional recommendation

`SUPPLY_LINK` should not be treated as a normal propagation endpoint class.

It is better understood as one of the following:

- an artifact that materializes virtual operational edges;
- an evidence-bearing corridor object;
- an overlay source for `supplier -> customer`, `depends_on infrastructure`, `carries product`, `transport_via`.

In other words: keep the entity, but do not let it compete with facilities, operators, deposits, ports, and markets as a downstream terminal.

## Where The Taxonomy Still Hurts Early Warning

### 1. `DEPENDS_ON` is too overloaded

This is the single most dangerous relation for early warning quality.

Right now it mixes:

- physical/logistics dependency;
- infrastructure dependency;
- regulatory dependency;
- communication dependency;
- domain-scaffold dependency;
- market/context dependency.

That is acceptable as a capture relation, but not as an undifferentiated propagation edge.

#### Recommendation

Keep `DEPENDS_ON`, but enforce these rules:

- `facet` should be mandatory for all non-legacy `DEPENDS_ON` edges, except narrow migration/backfill cases;
- propagation should use `facet`, not raw edge type, as the primary transmission selector;
- domain-scaffold `DEPENDS_ON` should never mix with operational `DEPENDS_ON` in the same traversal pool;
- `DEPENDS_ON` without facet should be soft-quarantined or scored down for propagation.

### 2. `INFLUENCES` is too weak for core propagation

`INFLUENCES` is valid for abstract or reputational effects, but it is too soft and too semantically broad to act as a main carrier in an early-warning engine.

#### Recommendation

- keep `INFLUENCES` as interpretation/context relation;
- exclude it from strong operational traversal except for dedicated reputation/policy layers;
- never let it outrank `SUPPLIES_TO`, `DEPENDS_ON(logistics|infrastructure|supply)`, `PRODUCES`, `EXTRACTS_FROM`, `PARTICIPATES_IN`.

### 3. `DOMAIN` edges are useful but dangerous in runtime propagation

`DOMAIN` edges are good for research coverage and strategic decomposition. They are not good default transport edges for shock propagation.

#### Recommendation

- treat `DOMAIN` as orchestration/config layer, not propagation layer;
- if used in propagation at all, route it into separate interpretive walks, not into the operational kernel.

### 4. MARKET is partly overused as a semantic bucket

`MARKET` is correct and necessary. But if market nodes become too generic, they act like semantic collection points instead of business transmission structures.

#### Recommendation

- keep `MARKET`, but prefer narrower markets with actual participants and segments;
- penalize generic market tails in endpoint scoring;
- encourage materialization of `participant -> market -> segment` plus chokepoint/facility links instead of isolated market abstractions.

## Recommended Layering For The Whole System

The clean architecture for early warning should be this.

### Layer 1. Source mechanics

- RSS
- trackers
- structured feeds
- documents
- APIs

### Layer 2. Deterministic extraction

- heuristics
- dictionaries
- Aho-Corasick / weighted matching
- canonical path resolution
- geo normalization
- graph enrichment heuristics

### Layer 3. Canonical operational graph

This should contain the propagation-safe substrate.

- concrete actors
- facilities
- products
- deposits
- markets
- geographies
- affected zones
- typed operational edges

### Layer 4. Operational overlay

This is where the graph becomes propagation-ready.

- propagation roles
- business roles
- exposure modes
- criticality
- substitutability
- inventory buffers
- latency-to-impact
- edge transmission qualifiers

### Layer 5. Strategic / policy overlay

- decision points
- mechanisms
- sanctions
- access controls
- governance levers

### Layer 6. Signal engine

- event -> axis mapping
- bounded traversal
- typed random walk / restart
- path-family scoring
- endpoint scoring
- breakpoint diagnostics

### Layer 7. Interpretation / warning

- non-obvious endpoint ranking
- blast radius synthesis
- warning generation
- optional LLM summarization only after deterministic ranking

This is the key point: `taxonomy` should support layer separation. It should not pretend every edge type belongs to the same runtime physics.

## Prompt And Branching Recommendations

### 1. Never run one universal graph prompt for all purposes

The current multi-mode approach is correct. It should be pushed further, not relaxed.

For early warning, prompts should be explicitly separated into:

- operational extraction
- governance extraction
- market extraction
- strategic/policy extraction
- zone extraction
- track extraction

A single broad prompt tends to produce semantically valid but propagation-useless edges.

### 2. Supply-chain mode should stay strict

The current decision to keep supply-chain mode on:

- `PRODUCES`
- `SUPPLIES_TO`
- `TRANSFORMS_INTO`
- `EXTRACTS_FROM`
- `LOCATED_IN`
- `OPERATES_IN`
- `PARTICIPATES_IN`
- `HAS_MARKET_SEGMENT`

is correct.

For early warning, this should remain the operational backbone.

### 3. Strategic/policy prompts should be downstream, not mixed upstream

The system should not ask the extractor to mix:

- physical flow modeling;
- policy mechanism modeling;
- domain scaffold expansion;
- social/narrative context;

in one pass.

That creates valid graph density but weak signal physics.

### 4. Prompt contracts should require transport qualifiers whenever an edge is route-sensitive

For any extracted dependency in logistics/supply/infrastructure/distribution:

- `facet`
- `throughput_share`
- `capacity_constraint`
- `reroute_penalty`
- `reroute_latency_delta_hours`
- `reroute_cost_delta_pct`
- `substitutability`

should be treated as first-class extraction goals.

For early warning these qualifiers are often more important than the bare existence of the edge.

## Postprocessor Recommendations

### 1. Canonicalize aggressively

This is already one of the strongest parts of the system and should stay central.

Canonicalization should continue to:

- collapse redundant relation aliases;
- fix directionality;
- map deprecated relations;
- prevent invalid `ORG <-> MARKET` misuse;
- convert product/product flow to `TRANSFORMS_INTO`;
- convert `CONSUMES` into `SUPPLIES_TO`.

### 2. Add stronger propagation-safe gating after canonicalization

Not every canonical edge should automatically be a propagation edge.

There should be an explicit runtime decision:

- `storage-valid`
- `interpretation-valid`
- `propagation-valid`

These are not the same.

### 3. Backfill metadata systematically, not opportunistically

The biggest graph quality issue is not taxonomy names. It is missing metadata.

Mandatory backfill targets:

- fragment-level:
  - `geoFeatureType`
  - `govType`
  - `marketDomain`
  - `marketVertical`
  - `industryPaths`
  - propagation/business role hints
- edge-level:
  - `facet`
  - `transport_via`
  - `throughput_share`
  - `capacity_constraint`
  - `reroute_penalty`
  - `substitutability`
  - `edgeModality`

### 4. Resolve geo/path debt centrally

`/fac/na/...` when country is already known is not a cosmetic issue. It damages:

- seed precision;
- entity resolution;
- geospatial aggregation;
- downstream endpoint ranking.

This belongs in central graph materialization/backfill, not as one-off fixes.

## Fundamental Advice On Graph Materialization

For this task, the graph should be materialized under a stricter principle:

`materialize what transmits; annotate what explains; quarantine what only decorates.`

### Therefore:

- materialize operational edges persistently;
- materialize strategic/policy edges persistently but keep them in a separate semantic layer;
- keep scaffold/domain edges available for research orchestration, but do not let them dominate runtime propagation;
- use `SUPPLY_LINK` as corridor evidence and operational overlay, not as a terminal endpoint class.

## Suggested Taxonomy Policy For Early Warning

### Keep

- `PRODUCES`
- `SUPPLIES_TO`
- `TRANSFORMS_INTO`
- `EXTRACTS_FROM`
- `LOCATED_IN`
- `OPERATES_IN`
- `OWNS`
- `CONTROLS`
- `REGULATES`
- `PARTICIPATES_IN`
- `ASSURES`
- `AFFECTED_ZONE`
- `TRIGGERED_BY`
- typed `DEPENDS_ON` with facet

### Collapse / deprecate

- `CONSUMES` -> `SUPPLIES_TO`
- `DOMINATES` -> `PARTICIPATES_IN + qualifiers`
- `HAS_SUBMARKET` -> `HAS_MARKET_SEGMENT`
- `WORKS_FOR` / `KEY_PERSON` / `HAS_KEY_PERSON` -> canonical employment/person-governance forms
- `AUDITED_BY` -> `ASSURES`
- transition aliases -> `CORPORATE_TRANSITION`
- weak associative aliases -> `ASSOCIATED_WITH`

### Quarantine from core propagation

- `DOMAIN` scaffold edges
- weak `INFLUENCES`
- `ASSOCIATED_WITH`
- social/context-only community/media relations
- generic market or sphere abstractions
- `SUPPLY_LINK` as terminal endpoint

### Require qualifiers

- all route-sensitive `DEPENDS_ON`
- all meaningful `SUPPLIES_TO` with known logistics channel
- policy mechanism edges where downstream scope is claimed

## Practical Advice On LLM Use

For early warning, LLM should not be the main extractor.

LLM should be limited to:

- rare ambiguity resolution;
- periodic taxonomy audits;
- missing-info research where deterministic branches stall;
- final human-readable synthesis after deterministic propagation.

Everything else should prefer:

- deterministic extraction;
- canonicalization;
- postprocessing;
- graph enrichment;
- propagation physics.

## Bottom Line

The taxonomy is not fundamentally wrong. In several important places it is already better than average:

- it has canonicalization discipline;
- it has explicit domain modes;
- it already removed at least one harmful redundancy (`CONSUMES`);
- it distinguishes strategic overlay from operational flow;
- it is compatible with propagation-aware modeling.

The main next discipline is this:

- make the operational taxonomy smaller and harder;
- keep strategic and scaffold taxonomy, but layer it;
- require more qualifiers and less generic edges;
- treat graph materialization and backfill as part of the taxonomy contract;
- judge every type/edge by whether it helps find non-obvious downstream business tails.

## Additional Taxonomy Rule: Facility identity quality matters as much as facility type

Observed graph debt shows a separate structural problem:

- many facility nodes are not wrong by type;
- they are wrong by identity specificity.

Examples:

- `/fac/na/general-production`
- `/fac/na/general-distribution`
- `/fac/na/casablanca-plant`
- `/fac/na/port-of-shanghai`

These are not useful canonical identities for early warning.

Implications:

- facility taxonomy should not only answer “is this a plant / terminal / warehouse?”;
- it must also answer “is this slug specific enough to represent one operational node?”.

Practical rule:

- low-information facility slugs must not be treated as safe exact-identity anchors;
- `/fac/na/*` with weak slugs should be considered provisional and collision-prone;
- early-warning propagation should prefer:
  - geo-grounded facilities,
  - operator/site-specific facilities,
  - facilities with richer identifiers/coordinates,
  - markets/chokepoints only when the concrete facility is still unresolved.

This is not just data hygiene.
It directly changes propagation quality:

- weak facility identity creates false merges;
- false merges create fake edges;
- fake edges produce fake downstream warnings.

If a relation improves graph explainability but not early-warning transmission, it belongs in overlay, not in the operational core.
