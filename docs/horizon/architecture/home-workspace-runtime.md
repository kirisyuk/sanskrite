# Horizon Home Workspace Runtime

## Purpose

`HorizonHomeWorkspace` is the default Horizon entry surface before a user commits to a lens.

It reuses the same runtime layers that already power:

- `signal landing`
- `event-core / halo / background`
- analytical primitives and aggregates
- grounded internal knowledge retrieval
- map projection on canonical refs

The goal is to make `/horizon` a real working surface, not an empty pre-lens placeholder.

## Product contract

From Home, the user can:

1. Ask a question in the command palette / workspace input.
2. See the result as:
   - explanatory answer
   - map focus
   - grounded objects / entities
   - fresh signal core
   - halo / affected zones
   - grounded evidence hits
3. Promote the result into a lens.
4. Save selected objects into:
   - watch subjects
   - portfolio

`Lens Chat` stays lens-native, but it now sits on the same substrate and logic family as Home.

## Runtime flow

`HomeWorkspaceRun(query)` executes this pipeline:

1. Resolve candidate entities from the existing Horizon fragment/entity search.
2. Build `scopeRefs` from:
   - explicit canonical paths in the query
   - resolved entities
3. Load fresh `SignalLandingSummary` evidence:
   - by scope refs
   - by recent relevant summaries
4. Load analytical evidence:
   - impacted primitives
   - scope aggregate built on read for the workspace scope
5. Run internal knowledge retrieval using the same Vertex-backed search helpers as `Lens Chat`.
6. Merge evidence and rerank with fresh signal priority.
7. Synthesize the answer with the same operator-style grounding rules:
   - event-core first
   - impact halo second
   - structural background third
8. Return:
   - answer
   - hits
   - highlighted paths
   - event-core paths
   - halo paths
   - affected zones
   - candidate entities
   - suggested lens name
   - suggested lens scope

## UI contract

Inside Horizon shell:

- `Home` is always at the top of the sidebar.
- `/horizon` defaults to `HorizonHomeWorkspace` when no lens is selected.
- if Home has map focus, the world map is shown as a sibling pane
- selecting a lens transitions into the existing lens canvas
- portfolio remains a separate surface

The following are no longer primary product entry surfaces:

- `create lens from query`
- `Lens Console`
- `Watch Dashboard`

Their backend/runtime parts may continue to exist temporarily, but they are no longer the main UX path.

## Action model

### Create Lens

Home promotes into a real lens by:

- creating a Horizon project if none exists yet
- creating a lens with selected canonical paths as scope
- passing along `homeWorkspace` metadata in `viewConfig`

### Add to Watch

Home writes selected objects into watch runtime through the existing watch target creation API.

### Add to Portfolio

Home writes selected objects into a reusable portfolio bucket (`Home workspace`) through the existing portfolio runtime.

## Why this architecture

This avoids creating a second exploratory engine.

Instead:

- Home is a thin orchestration shell over the same landed knowledge
- Lens is a promoted, deeper, persisted continuation of that same work
- watch and portfolio consume the same canonical objects

That keeps:

- logic unified
- signal landing reusable
- analytical substrate reusable
- map semantics consistent across Home and Lens

## Current implementation notes

Current backend route:

- `POST /api/horizon/home/workspace/run`

Current frontend surface:

- `HorizonHomeWorkspace`

Current shell integration:

- `Home` in `SidebarPanel`
- `/horizon` shows Home unless a lens is explicitly selected

## Known next steps

1. Persist `HomeRun` as a first-class object if collaborative or resumable workspace history becomes necessary.
2. Expand Home actions beyond watch/portfolio/lens promotion.
3. Push more product/domain-specific halo depth into Home for gaps like `helium`, `lng`, membranes, desalination.
4. Fully retire old product-path surfaces once migration is complete.
