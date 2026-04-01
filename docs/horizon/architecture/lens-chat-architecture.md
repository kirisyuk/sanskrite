# Lens Chat Architecture

## Status

Proposed architecture for the first productized `Lens Chat` implementation.

This document fixes the product and system direction for a lens-native conversational interface that:

- belongs to the lens as an artifact;
- works across `Map`, `Graph`, `Documents`, and later other lens surfaces;
- uses warmed internal knowledge for fast semantic answers;
- uses deterministic Horizon tools for grounding, retrieval, and safe mutation;
- does not depend on `Lens QA` as a separate legacy path.

---

## 1. Product Decision

### 1.1 What Lens Chat is

`Lens Chat` is a persistent conversational artifact attached to a lens.

It is not:

- a separate display mode;
- a replacement for `Map`, `Graph`, or `Documents`;
- a freeform chat disconnected from Horizon structures.

It is:

- a lens-bound conversation thread;
- a semantic entry point into lens knowledge and operations;
- a control surface for discovery, explanation, tracing, and safe change planning.

### 1.2 Relationship to lens display tabs

The existing top-level lens display tabs remain separate.

Examples:

- `Map`
- `Graph`
- `Analysis`
- `Documents`

The chat does not own those modes and does not replace them.

Instead:

- the same chat exists regardless of the active tab;
- the active tab changes how the answer is visualized;
- the underlying conversation state remains the same.

This keeps the mental model clean:

- the lens is the object;
- the chat is one of the lens artifacts;
- the tab is a rendering surface.

### 1.3 User-facing behavior

The system should answer in stages:

1. Identify what is known quickly.
2. Expand into more precise structure only when needed.
3. Show the result on the active surface.
4. If the user wants to modify the lens or graph, switch into safe planning flow.

That means:

- a simple question should feel fast and direct;
- a structural question may take more steps;
- a mutation request must go through plan/preview/confirm.

---

## 2. What We Are Explicitly Not Building

### 2.1 No separate Lens QA product path

`Lens QA` may remain a source of reusable backend pieces, but it is not the target UX.

The product path becomes:

- `Lens Chat`

not:

- `Lens QA`
- `Ask this graph`
- multiple overlapping “analysis chats”

### 2.2 No mode-coupled chat identity

We should not create:

- one chat for `Map`;
- another chat for `Graph`;
- another chat for `Documents`.

That would fragment history and force the user to mentally reconcile separate conversations about the same lens.

### 2.3 No prompt-only autonomous execution

The system should not rely on:

- “let the model freely call whatever tools it wants and hope for good behavior”.

Instead:

- the model orchestrates within an explicit execution policy;
- the system owns the control flow;
- deterministic tools own structural truth and mutation safety.

---

## 3. Core Runtime Principle

The system has two distinct planes:

### 3.1 Semantic answer plane

The first answer step should come from warmed internal knowledge over prepared Vertex-backed lens corpus.

This plane is good for:

- subject-matter understanding;
- semantic retrieval;
- identifying candidate actors, products, facilities, routes, and gaps;
- answering “what is going on here?” quickly.

This plane is not the final source of graph truth.

### 3.2 Structural action plane

Structural truth must come from Horizon deterministic capabilities:

- canonical objects;
- materialized supply-chain artifacts;
- fragment traces;
- trace tools;
- graph intent planning;
- scope expansion flows.

This plane is good for:

- exact canonical targets;
- evidence-backed route and artifact retrieval;
- deterministic mutation planning;
- UI-highlight payloads tied to actual objects and artifacts.

### 3.3 Resulting rule

The chat should operate with this sequence:

1. semantic read first;
2. deterministic deepening second;
3. synthesis last.

Not the other way around.

---

## 4. Lens Chat Answer Model

### 4.1 Four-stage answer flow

Each user turn should conceptually pass through:

1. `semantic read`
2. `orchestrator decision`
3. `deterministic retrieval or action planning`
4. `final synthesis`

### 4.2 Stage 1: semantic read

The system uses warmed internal knowledge to answer:

- what objects seem relevant;
- what products seem relevant;
- what corridors or routes are likely relevant;
- whether the question is simple, structural, or mutating.

This stage should be fast and not depend on heavy local rebuilding.

### 4.3 Stage 2: orchestrator decision

The orchestrator decides:

- whether the semantic context is sufficient;
- whether canonical object inspection is needed;
- whether materialized artifact lookup is needed;
- whether tracing is needed;
- whether the user is asking for change rather than explanation.

### 4.4 Stage 3: deterministic retrieval or planning

Only the necessary structural tools are used.

Typical examples:

- object discovery;
- object fetch by canonical path;
- materialized artifact search;
- fragment trace;
- fast trace;
- graph preflight;
- graph dry-run.

### 4.5 Stage 4: final synthesis

The final response should combine:

- semantic context from internal knowledge;
- exact evidence from deterministic tools;
- explicit uncertainty when evidence is weak;
- a visualization payload for the active surface.

---

## 5. Tool Contract

## 5.1 Fast read tools

These are safe, cheap, and should be preferred first.

- `get_internal_knowledge_status`
- `search_internal_knowledge`
- `search_objects`
- `get_objects_by_paths`

### 5.2 Structural retrieval tools

These are deterministic and evidence-oriented.

- `search_supply_chain_artifacts`
- `get_fragment_trace`

### 5.3 Expensive structural tools

These should be used only after cheaper options fail or when the user explicitly asks for exact path structure.

- `fast_trace_supply_chain`
- `trace_supply_chain`

### 5.4 Mutation preparation tools

- `preflight_scope_change`
- `plan_graph_change`
- `dry_run_graph_change`

### 5.5 Mutation tools

- `expand_lens_scope`
- `apply_graph_change`

These should never be called as the first reaction to a request.

---

## 6. Orchestrator Policy

## 6.1 For normal analytical questions

Default policy:

1. `search_internal_knowledge`
2. if enough context exists, synthesize answer
3. if exact objects are needed:
   `search_objects -> get_objects_by_paths`
4. if route or supply context is needed:
   `search_supply_chain_artifacts`
5. if local explanation around a fragment is needed:
   `get_fragment_trace`
6. if exact path structure is still missing:
   `fast_trace_supply_chain`
7. only in the rare expensive case:
   `trace_supply_chain`

## 6.2 For requests that imply change

Default policy:

1. semantic read first
2. `preflight_scope_change`
3. if the action is lens enrichment:
   `expand_lens_scope` only after explicit decision
4. if the action is graph mutation:
   `plan_graph_change`
   `dry_run_graph_change`
   `apply_graph_change` only after confirmation

## 6.3 Re-planning requirement

The orchestrator must not stop after the first failed retrieval path.

It should:

- try the next reasonable retrieval branch;
- degrade gracefully from exact to approximate evidence;
- stop only when it has either:
  - enough evidence;
  - or a clear, honest insufficiency state.

This is critical.

The orchestration layer is valuable only if it can:

- re-plan;
- retry;
- choose the next-best structural move.

---

## 7. UI Contract

### 7.1 Message contract

Every assistant message should conceptually carry:

- `answer`
- `evidence`
- `toolRuns`
- `visual`
- `nextActions`

### 7.2 Visual payload

The visual payload must be display-surface-neutral.

Suggested contract:

```json
{
  "target": "map|graph|documents|none",
  "highlightedFragmentIds": [],
  "highlightedEdgeIds": [],
  "highlightedCanonicalPaths": [],
  "highlightedArtifactIds": [],
  "tracePaths": [],
  "viewportHint": {},
  "selection": {}
}
```

### 7.3 Surface-specific behavior

On `Map`:

- highlight facilities, organizations, corridors, routes, and viewport.

On `Graph`:

- highlight nodes, edges, path families, and neighborhoods.

On `Documents`:

- highlight prepared capsules, evidence artifacts, and related records.

The same assistant message should drive all of these from one stored result.

---

## 8. Storage Decision: Reuse `core_interactions`

## 8.1 Decision

The lens chat should reuse `core_interactions` as the persistence layer for chat messages.

This is the correct default direction.

We do not need a new message collection for the first implementation.

## 8.2 Why this is viable

The current `Interaction` model already supports most of what a lens-native chat needs:

- `kind`
- `subkind`
- `content`
- `summary`
- `author`
- `replyTo`
- `relations`
- `model`
- `usage`
- `agentProfile`
- `sources`
- `searchQueries`
- `sectionId`
- `metadata`

That means the current interaction system already knows how to represent:

- user messages;
- assistant messages;
- system messages;
- thread linkage;
- source and tool provenance;
- per-message model and token metadata.

## 8.3 Existing code evidence

Current backend interaction model:

- [interactions.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/repo/interactions.go#L48)

Current interaction HTTP create/list/update path:

- [discovery.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/http/discovery.go#L846)

Current frontend interaction types:

- [interaction.ts](/Users/kirisyuk/cursor/sanskrite/web/src/types/interaction.ts#L13)

Current frontend chat store local interaction shape:

- [genaiChatStore.ts](/Users/kirisyuk/cursor/sanskrite/web/src/stores/genaiChatStore.ts#L71)

## 8.4 How lens chat should map onto interactions

### Message identity

Each lens chat message remains a normal interaction:

- `kind = user | assistant | system`

### Lens binding

The message is bound to a specific lens through metadata.

At minimum:

- `metadata.lensChat.lensId`

### Thread binding

If we want multiple independent threads per lens, use:

- `sectionId` as the thread/session id

This fits the current interaction model better than introducing a second parallel threading primitive.

### Reply linkage

Use existing fields:

- `replyTo`
- `relations`

For example:

- user message replies to previous assistant answer;
- assistant message answers a user message;
- follow-up answers can `refine` or `expand` previous messages.

## 8.5 Proposed metadata extension

The current metadata is too narrow.

Today it only models:

- `pinned`
- `notes`

For lens chat, it should be extended with a typed nested structure.

Suggested shape:

```json
{
  "pinned": true,
  "notes": ["important"],
  "lensChat": {
    "lensId": "string",
    "threadId": "string",
    "messageClass": "chat|tool|status|preview",
    "renderTargetHint": "map|graph|documents|none",
    "visual": {
      "highlightedCanonicalPaths": [],
      "highlightedArtifactIds": [],
      "tracePaths": [],
      "viewportHint": {}
    },
    "toolRuns": [
      {
        "tool": "search_internal_knowledge",
        "status": "ok",
        "durationMs": 1234
      }
    ],
    "evidence": {
      "artifactIds": [],
      "coreDocIds": [],
      "canonicalPaths": []
    },
    "status": "completed|partial|failed"
  }
}
```

### Important storage rule

Store compact summaries in interaction metadata, not full raw payloads.

Do not store:

- huge retrieved documents;
- giant tool results;
- raw trace internals that can be recomputed.

Do store:

- canonical ids;
- artifact ids;
- compact tool summaries;
- UI-oriented visual state;
- short evidence references.

This keeps Firestore documents bounded and avoids turning interactions into a dumping ground.

---

## 9. What Must Change To Reuse `core_interactions` Correctly

## 9.1 Expand backend metadata model

Current backend type is too narrow:

- [interactions.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/repo/interactions.go#L27)

It should be extended so that lens chat metadata is a first-class typed subtree.

## 9.2 Fix metadata merge behavior

Current update merge logic only knows how to preserve:

- `pinned`
- `notes`

See:

- [interactions.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/repo/interactions.go#L152)

If we add `metadata.lensChat.*`, this merge logic must be updated.

Otherwise:

- partial metadata updates will silently clobber or drop lens chat state.

This is the main technical caveat in reusing interactions.

## 9.3 Expand API request and response types

The create/list/update interaction API currently exposes metadata, but frontend typing is still narrow.

See:

- [api.ts](/Users/kirisyuk/cursor/sanskrite/web/src/lib/api.ts#L645)
- [api.ts](/Users/kirisyuk/cursor/sanskrite/web/src/lib/api.ts#L650)

And frontend model:

- [interaction.ts](/Users/kirisyuk/cursor/sanskrite/web/src/types/interaction.ts#L13)

These types need to explicitly carry `metadata.lensChat`.

## 9.4 Add lens-aware interaction queries

Current repository listing is project-scoped:

- [interactions.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/repo/interactions.go#L219)

That is enough for an MVP if the frontend filters by `metadata.lensChat.lensId`.

But for a proper implementation we should add:

- `ListByProjectAndLens(projectId, lensId)`

Eventually with optional:

- `sectionId`

for per-thread loading.

## 9.5 Add Firestore indexes for lens filtering

If we query directly by lens metadata on the backend, we will need composite indexes such as:

- `projectId + metadata.lensChat.lensId + createdAt`

Without this, project-wide list and client-side filter remains acceptable only for a first implementation.

## 9.6 Use explicit section ids for lens chat

The current interaction API auto-resolves open sections if the client does not provide a `sectionId`:

- [discovery.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/http/discovery.go#L1014)
- [discovery.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/http/discovery.go#L1106)

For lens chat this is risky if the same project also has other interaction flows.

Therefore lens chat should:

- always provide explicit `sectionId` for its thread;
- not rely on the generic “currently open section” behavior.

This avoids accidental cross-talk between discovery chat, document chat, and lens chat.

---

## 10. Recommended Lens Chat Persistence Model

## 10.1 Recommended message storage

For a user turn:

- create a `user` interaction
- attach `metadata.lensChat.lensId`
- attach current `sectionId`

For the assistant answer:

- create an `assistant` interaction
- set `replyTo` to the user message
- attach compact `toolRuns`
- attach compact `evidence`
- attach `visual`
- attach any `sources`, `coreDocIds`, and `searchQueries`

For intermediate orchestration status if needed:

- optionally create `system` interactions with `metadata.lensChat.messageClass = "status"`

This gives:

- a full audit trail;
- replayable UI state;
- consistent history;
- low migration cost.

## 10.2 Why not a separate lens-chat collection first

A separate collection would duplicate:

- message model;
- threading logic;
- author/model/usage fields;
- UI hydration patterns;
- API surface.

That is unnecessary at the first stage.

We should only split storage later if we hit:

- scaling limits;
- query isolation problems;
- Firestore document shape complexity that becomes unmanageable.

---

## 11. Frontend Implications

## 11.1 Chat placement

The chat should live as a persistent panel or rail attached to the lens workspace.

It should not be a tab replacement.

### 11.2 Data loading

The lens chat UI should load:

- interactions for the project;
- filtered by `metadata.lensChat.lensId`;
- optionally grouped by `sectionId` for multiple threads.

### 11.3 Rendering

The frontend should interpret:

- `content` as message text;
- `metadata.lensChat.visual` as the visualization directive;
- `sources`, `coreDocIds`, and `metadata.lensChat.evidence` as evidence footer data;
- `metadata.lensChat.toolRuns` as optional developer/debug view.

### 11.4 Shared history

When the user changes from `Map` to `Graph`, the same message history remains visible.

Only the rendering target changes.

---

## 12. Backend Implications

## 12.1 New chat orchestration endpoint

The lens chat core should eventually get its own orchestration entry point.

Conceptually:

- `POST /api/horizon/lenses/{lensId}/chat`

That entry point should:

- read lens context;
- run the orchestrator;
- persist interactions;
- return answer plus visual payload.

## 12.2 Reuse existing tool substrate

The backend can reuse:

- warmed internal knowledge retrieval;
- object discovery and canonical fetch;
- artifact search;
- fragment trace;
- trace tools;
- scope and graph intent flows.

The missing piece is not raw capability.

The missing piece is:

- a coherent lens-native orchestrator plus persistence model.

---

## 13. Migration Plan

## 13.1 Phase 1

- define lens chat contract;
- extend interaction metadata;
- persist lens chat messages into `core_interactions`;
- build lens chat UI shell;
- use `search_internal_knowledge` as hot path;
- use deterministic retrieval only on demand.

## 13.2 Phase 2

- add backend list-by-lens query;
- add Firestore indexes;
- add thread/session handling;
- improve orchestrator retry graph;
- expose developer/debug view of tool runs.

## 13.3 Phase 3

- safe write flows from chat;
- preview/apply mutation flows;
- cross-surface playback of prior answer visual states;
- richer thread management per lens.

---

## 14. Final Decision

### 14.1 Product decision

We are building one `Lens Chat` per lens artifact, independent of the current tab.

### 14.2 Runtime decision

The hot path is:

- `search_internal_knowledge`
- orchestrator decision
- deterministic tools only if needed
- final synthesis

### 14.3 Storage decision

We should reuse `core_interactions` for lens chat messages.

This is viable now because the interaction model already supports message semantics.

It requires a targeted upgrade in exactly these areas:

- metadata shape;
- metadata merge logic;
- frontend typing;
- lens-aware interaction filtering;
- explicit thread/session handling.

### 14.4 Strategic payoff

This gives us:

- a single conversation model across the product;
- low migration cost;
- consistent message history and provenance;
- a path to lens-native chat without inventing a second chat system.
