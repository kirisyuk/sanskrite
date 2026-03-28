# Core Layer Contracts and WorkScope v2

## Purpose

This document makes two things explicit:

1. which Horizon layers are semantic kernels versus orchestration/projection layers
2. how `WorkScope + ExecutionIntent` should drive idempotent kernel execution without becoming a parallel ontology

The target state is:

- lenses are assembly/view points over the world model
- ad-hoc work uses the same kernel
- graph enrichment, path materialization, metric hydration, propagation, and simulation stay universal
- `WorkScope` defines the world slice
- `ExecutionIntent` defines the current action
- `WorkRequest` composes both for planning

## Core Rule

`WorkScope` answers:

- what is in play
- where work starts
- how far the caller may expand
- what the stable boundary shape is

`WorkScope` does **not** answer:

- canonical identity truth
- domain semantics truth
- signal physics
- risk semantics

`ExecutionIntent` answers:

- what the caller wants to do now
- which layers are requested explicitly
- how aggressively freshness/mutation should be applied
- how to project outputs for this request

Those semantics must not be baked into the stable scope.

## Layer Contracts

### 1. Canonical Identity / Taxonomy

Purpose:
- canonical paths
- entity typing
- product synonym dictionary
- product transform dictionary
- mechanism / regulator identity normalization

Primary contract:
- canonical path rules
- dictionaries and candidate queues

Depends on `WorkScope`:
- no, except that callers may submit refs discovered inside a scope

Vertical outputs:
- canonical refs
- normalized relation semantics
- candidate observations for unresolved identity

### 2. Graph Enrichment

Purpose:
- enrich fragments, edges, and evidentiary facts
- fill graph gaps deterministically or via scout/research workflows

Primary contract:
- domain prompts/contracts
- fact persistence
- graph entity/edge invariants

Depends on `WorkScope`:
- yes, for anchors, requested expansion policy, and idempotent execution volume

Vertical outputs:
- new or updated fragments / edges / facts

### 3. Path Materialization

Purpose:
- materialize supply paths, legs, route-bearing shoulders, and path-carried substrates

Primary contract:
- canonical supply/path semantics
- path artifact contracts

Depends on `WorkScope`:
- yes, for which anchors/substrate to materialize and how broadly to expand

Vertical outputs:
- materialized path artifacts
- leg payloads
- route-bearing execution substrate

### 4. Metric Core

Purpose:
- hydrate dynamic operational metrics
- maintain freshness, provenance, and write-back discipline

Primary contract:
- metric families
- freshness/TTL rules
- target scopes `node | edge | path | leg`

Depends on `WorkScope`:
- yes, for target selection and refresh budget
- no, for metric truth semantics themselves

Vertical outputs:
- hydrated metric values
- freshness/provenance state
- operational snapshots

### 5. Propagation Core

Purpose:
- encode/transfer/decode signals through the graph

Primary contract:
- `SignalDescriptor`
- domain adapters
- propagation profiles and policies

Depends on `WorkScope`:
- only for seeds, targets, trace budget, and projection
- not for signal physics

Vertical outputs:
- propagated node/path impact
- traces
- decoded business metrics

### 6. Risk and Simulation

Purpose:
- derive risk primitives
- run scenarios
- produce interpretation, ranking, and alerts

Primary contract:
- stage machines
- risk primitives
- scenario contracts

Depends on `WorkScope`:
- yes, for entry scope, projection, and execution budget
- not for the underlying risk semantics

Vertical outputs:
- scenario outputs
- derived risk findings
- recommendations / alerts / interpretations

### 7. WorkScope and Request Orchestration

Purpose:
- unify lens, watch, scout, simulation, API, and ad-hoc entrypoints
- make execution idempotent and layer-aware

Primary contract:
- `WorkScope`: scope kind, anchors, boundary expansion
- `ExecutionIntent`: action, entrypoint, requested layers, projection, freshness, mutation
- `WorkRequest`: stable scope + ephemeral action

Depends on `WorkScope`:
- this layer is `WorkScope + ExecutionIntent`

Vertical outputs:
- normalized `WorkScope`
- `ResolvedWorkSet`
- ordered execution plan

## WorkScope

`WorkScope` keeps the old ref-compatible payload:

- `tenantId`
- `projectId`
- `SimulationBaselineRefs`-shaped refs

And adds only stable scope metadata:

- `scopeKind`
- `expansionPolicy`

This split is intentional:

- refs preserve compatibility with existing runtime and persistence
- scope metadata says what world slice this is

## ExecutionIntent

`ExecutionIntent` is per-request, not per-scope:

- `entryPoint`
- `action`
- `domainModes`
- `requestedLayers`
- `projectionPolicy`
- `freshnessMode`
- `mutationMode`

This is the missing separation between:

- a lens or watch subject as a stable world slice
- a simulation, hydration, graph enrich, or risk request over that slice

For lens enrichment specifically, `mutationMode` now also governs whether runtime may auto-add discovered paths back into lens scope:

- `persist_world_keep_scope`
- `persist_world_scope_refs_only`
- `persist_world_expand_scope`

That keeps `lens` as an entry/view surface while the world model remains universal.

Explicit lens scope growth follows a separate rule:

- it is not a side effect of enrichment
- it has one parametric service contract
- all UI actions that “expand the lens” must reduce to the same expansion request shape

This keeps user-facing scope semantics coherent even when multiple interaction patterns exist.

## WorkRequest

`WorkRequest` is the universal kernel input:

- `scope`
- `intent`

## Idempotent Kernel Execution

Any caller should be able to issue the same class of request:

1. construct `WorkScope`
2. construct `ExecutionIntent`
3. normalize them into `WorkRequest`
4. resolve the scope into `ResolvedWorkSet`
5. build execution plan
5. run requested layers in canonical order
6. project outputs according to policy

This must work equally for:

- lens-triggered work
- watch-triggered work
- scout-triggered work
- simulation baseline build
- ad-hoc anchored requests outside a lens

## Lenses

Lens semantics in the target state:

- a lens is a point of assembly and view over part of the world
- a lens is not a separate ontology
- a lens is not a separate simulation engine
- a lens is not a separate risk engine

Lens-specific work should only differ in:

- how it builds `WorkScope`
- which `ExecutionIntent` it issues now

## Practical Implementation Rule

When adding new logic:

- if it changes truth semantics, put it in the semantic layer that owns that truth
- if it changes stable scope semantics, put it in `workscope`
- if it changes the current requested action, put it in `ExecutionIntent`

That separation keeps the kernel universal while allowing lenses and ad-hoc entrypoints to reuse it safely.
