# AgentHorde Taxonomy Migration — Remaining Work Specification (P7–P14)

> **Purpose.** This is the single authoritative task list for finishing the AgentHorde (AH)
> taxonomy migration after P0–P6. It is written for a fresh coding agent that has **no prior
> context** about the migration. Execute phases strictly in the order P7 → P14. Do not reorder.
> Do not skip. Do not merge phases.
>
> The user picked **Variant A** for `materialized-payload-rewrite`: writer-side rewrite first,
> no dual-read compatibility shims, overlay layer is allowed to be temporarily blind on legacy
> data until `migrate-run` fills it with AH-shape data. This decision is binding.

---

## 0. Mandatory reading before writing any code

Read these files end-to-end **before** starting Phase 7. They encode decisions that are
otherwise invisible. Do not ask the user to re-state them.

### 0.1. Project invariants (workspace-wide rules)

1. `/Users/kirisyuk/cursor/sanskrite/AGENTS.md` — hard rules about supply-chain identity.
2. `/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/supply-chain-canonical-control-plane.md`
3. `/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md`
4. `/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md`
5. `/Users/kirisyuk/cursor/sanskrite/docs/agents/agenthorde-runtimes.md` (if present; optional but useful context)

### 0.2. Taxonomy SoT + codegen

1. `agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py` — Python SoT.
2. `shared/taxonomy/taxonomy.json` — frozen JSON snapshot (committed).
3. `backend/internal/taxonomy/ah/taxonomy_generated.go` — generated Go enums/lookup tables.
4. `backend/internal/taxonomy/ah/helpers.go` — hand-written helpers (`IsKnownFragmentClass`,
   `IsKnownRelation`, `NormalizeRelation`, `RelationAllowedOnLayer`, `AttributeSpecFor`, …).
5. `backend/internal/taxonomy/ah/doc.go` — the `go:generate` directive.
6. `web/src/lib/taxonomy.generated.ts` — generated TS mirror.
7. `docs/taxonomy/CODEGEN.md` — three-stage pipeline (py → json → go+ts) and how to regenerate.
8. `docs/taxonomy/field_inventory.yaml` — every backend field’s disposition (keep / lift-to-
   attributes / drop / rename).
9. `docs/taxonomy/migration_map.yaml` — every legacy enum value’s disposition with
   `fragment_type_map`, `dependency_type_map`, and routing/heuristic blocks. **The migrate-cli
   uses this file directly; every mapping is authoritative.**
10. `docs/taxonomy/cli_tools_inventory.yaml` — which `cmd/tools/*` packages survived the cleanup
    and why.

### 0.3. Model layer (the data shape to respect)

1. `backend/internal/horizon/models/fragment.go` — `HorizonFragment.Class` (`ah.FragmentClass`),
   `Subtype` (string), `Attributes map[string]interface{}`, `IsLegacySupplyLink()`, custom
   `UnmarshalJSON` that translates legacy `type` field via `FragmentClassFromLegacyType`.
2. `backend/internal/horizon/models/fragment_class.go` — `LegacyFragmentTypeAliases`, the
   authoritative legacy→(class, subtype) map and all closed-subtype helpers.
3. `backend/internal/horizon/models/dependency_edge.go` — `DependencyEdge.Relation`
   (`ah.RelationName`), `Qualifiers`, `Attributes`, `UnmarshalJSON` with
   `NormalizeWireRelationStrict`.
4. `backend/internal/horizon/models/attributes.go` — typed attribute/qualifier accessors.
5. `backend/internal/horizon/models/transmission_qualifiers.go` — AH-aligned relation-set
   classifier for signal propagation.
6. `backend/internal/horizon/models/propagation_profile.go` + `axis_spec.go` — propagation SoT.
7. `backend/internal/horizon/models/materialized_artifact.go` — the **legacy** payload schemas
   that P9 rewrites.
8. `backend/internal/horizon/models/graph_intent.go` — wire-level taxonomy allowlists + legacy
   alias table (the canonical example of “legacy at the wire boundary, AH inside”).
9. `backend/internal/horizon/models/supply_link_helpers.go` — TRANSITIONAL; `IsLegacySupplyLink`
   is the sentinel predicate. **Do not delete before Phase 13.**
10. `backend/internal/horizon/models/lens_item.go` — reference model for how LensItem carries
    `Class`/`Subtype`/`Relation` today.

### 0.4. Example of a clean package-local shim

`backend/internal/http/horizon/legacy_aliases.go` — 30+ files in that package compile against
typed AH values via these aliases. **This is the pattern.** When a future phase asks for such a
shim, mirror this file (package-local, one file, `_ = ah.FragmentClass` to prevent “unused
import” drift, no `__legacy_*` sentinels, no equality checks against collapsed aliases).

### 0.5. What is already done (P0–P6) — do not redo

- P0: Freeze AH SoT, codegen Go + TS, field inventory, migration map.
- P1: Rewrite `internal/horizon/models/*`.
- P2: Rewrite `edgesemantics`, `entity_semantics`, `canonical`, `policy`, `contractsemantics`,
  `productdict`, `regulatorytransform`, `graphenrich`, `repo`.
- P3: Rewrite `scout/*`, `scout/agents/*`, `scout/contracts`, `scout/intra_graph`, `scout/pipeline`,
  `signallanding`, `analytical`, `merge`, `manualresolve`, `pathbinding`, `workscope`,
  `metriccore`, `pathbuilder`, `propagation`, `signals`, `enrich`.
- P4: Rewrite `lens`, `qa`, `supplychain`, `materialize`, `profile`, `importance`, `focusobject`,
  `internalknowledge`, `risk`, `scenarios/{dubaicit,kzvat,teslabattery}`, `watch`, `watchruntime`,
  `entity_linker`. Propagation profiles rewritten to AH SoT.
- P5: Rewrite `internal/http/horizon/*`, `internal/agentmesh/scout`. Response wire format for
  fragments **changed** from `type` to `class`/`subtype`. Frontend must catch up in P12.
- Tools-cleanup: `cmd/tools/*` reduced from 198 to 27 packages.
- P6: `cmd/server/main.go`, 19 surviving `cmd/tools/*`, scenarios all build and test.

**Known technical debt entering P7:**

1. ~30 `legacy_aliases.go` files inside `backend/internal/*` (inventoried in §13). These are
   **bounded** shims (package-local only). Phase 13 removes them.
2. Three `cmd/tools/*` packages (`horizon_import_worldmonitor`, `horizon_supply_repair`,
   `horizon_smart_dedup`) still contain hardcoded legacy wire tokens like
   `ah.RelationName("located_in")`. These reflect pre-migration Firestore state and will stop
   being a problem after Phase 11 completes successfully.
3. `models/supply_link_helpers.go` — TRANSITIONAL; removed in Phase 13 after Phase 11 succeeds.
4. `pathbuilder/legacy_helpers.go`, `domain/legacy_helpers.go`, `importance/legacy_helpers.go`,
   `scout/legacy_helpers.go`, `scout/agents/legacy_helpers.go`, `propagation/legacy_helpers.go`
   — same story.
5. Test data files in `focusobject/testdata/focus_legal_access_graph.json` still contain
   UPPER_SNAKE relation strings **on purpose** (they exercise the `UnmarshalJSON` translation
   path). Do not rewrite those fixtures.

---

## 1. Global invariants (apply to EVERY phase)

### 1.1. Typing

- **Use `ah.FragmentClass` and `ah.RelationName`** (from `backend/internal/taxonomy/ah`) for all
  new code. Never introduce raw `string` typed taxonomy fields on struct fields or function
  signatures. Existing `string`-typed wire-boundary fields stay as-is.
- **Never introduce new `FragmentType` / `DependencyType` aliases** beyond the shims that
  already exist. If a new package needs backward-compat, replicate the
  `internal/http/horizon/legacy_aliases.go` pattern exactly (package-local, one file). Do not
  add new `__legacy_*` sentinel constants; use `IsLegacy*` predicates on the fragment instead.
- **Wire boundaries** (HTTP request JSON, Firestore doc decode, YAML fixtures) may keep
  UPPER_SNAKE strings. Translation happens in `UnmarshalJSON` / `FragmentClassFromLegacyType` /
  `NormalizeWireRelationStrict`. Everywhere else, use AH types.

### 1.2. Supply-chain identity rules (from AGENTS.md)

- Product identity is the canonical product path, not the raw label.
- `produces` / `supplies` / `leg_connects` are distinct relations and must not be conflated.
- Transformations (`TRANSFORMS_INTO`, `ASSEMBLES_INTO`) live as **`siteTransforms[]`** attribute
  on the site node, not as edges.
- New unknown products / product pairs go to candidate collections, never to ad-hoc dictionaries.

### 1.3. Tests

- Never weaken an existing test threshold/assertion. If a test fails, fix the code.
- If a test is genuinely obsolete (feature removed), delete the test file in the same commit
  that removes the feature; do not leave zombie stubs.
- Pre-existing out-of-scope failures that you may observe:
  - `internal/horizon/internalknowledge/warmup_service_test.go` — time-related flake, not a
    taxonomy issue.
  - `internal/ontology/...` tests that call live OpenAI — sandbox cannot reach the API.
- Never broaden test scope into these two areas as part of P7–P14.

### 1.4. Commit discipline

- One logical change per commit, bisect-friendly.
- Commit message format:
  ```
  <area>: <short imperative summary>

  <body: what / why / any follow-ups>
  ```
  Areas in play: `taxonomy`, `backend`, `frontend`, `agents`, `docs`, `scripts`, `tests`.
- Always run `go build ./...` and `go vet ./...` before committing backend changes; `npm run
  build` for frontend changes.
- Never amend a pushed commit. Never force-push. Never update `git config`. Never skip hooks.
- Never commit `.env`, credentials, snapshots larger than 10 MB, or binaries. If you introduce
  a new build artifact pattern, add it to `.gitignore` in the same commit.

### 1.5. Escalation (stop and wait for user)

Stop and surface the question to the user instead of guessing when any of the following occur:

1. `migration_map.yaml` / `field_inventory.yaml` is missing an entry that you need — do not add
   one unilaterally. Propose the addition and wait.
2. A taxonomy concept you need does not exist in `taxonomy.py` (e.g., a new relation). Do not
   add it to `taxonomy.py` without approval.
3. A behavioral choice with a user-visible consequence (e.g., URL shape, response field name,
   `MaterializedKind` rename).
4. A test that fails for non-obvious reasons. Do not lower the threshold; surface it.
5. Unrecoverable data shape ambiguity during migrate-cli design. Surface the cases, propose
   options.
6. `pathquery` HTTP shape — the user wants to see the proposed API before implementation.

---

## 2. Verification commands (memorize these)

```bash
# Backend build + vet
cd backend && go build ./... && go vet ./...

# Full backend test (fast, minus the 2 known failing packages)
cd backend && go test ./internal/...

# Codegen guards (must all be clean)
cd agents/shared/agenthorde_common && python3 -m scripts.export_taxonomy --out ../../../shared/taxonomy/taxonomy.json --check
cd backend && go generate ./internal/taxonomy/ah && go test ./internal/taxonomy/ah/...
cd web && npm run gen:taxonomy:check

# Frontend build + typecheck + lint
cd web && npm run build && npm run typecheck && npm run lint

# Legacy-string audit (must return empty by end of Phase 13)
cd backend && rg -n '"DEPENDS_ON"|"SUPPLIES_TO"|"TRANSFORMS_INTO"|"ASSEMBLES_INTO"|"USED_IN"|"DOMAIN"|"SPHERE"|"SUPPLY_LINK"|"AFFECTED_ZONE"|"MEDIA_CHANNEL"|"PROGRAM"|"FACILITY"|"DEPOSIT"|"GEO_REGION"|"TRACK"|"EVENT"|"DECISION_POINT"|"MECHANISM"' \
  --glob '!internal/horizon/models/graph_intent.go' \
  --glob '!internal/horizon/models/fragment_class.go' \
  --glob '!internal/horizon/models/dependency_edge.go' \
  --glob '!internal/horizon/models/transmission_qualifiers.go' \
  --glob '!internal/horizon/models/fragment.go' \
  --glob '!internal/horizon/models/relation_alias.go' \
  --glob '!**/*_test.go' \
  --glob '!**/testdata/**'
```

The last command is the **cleanup-code acceptance oracle**. Every remaining hit after Phase 13
must be explicitly justified in a code comment (`// wire boundary: Firestore legacy docs`).

---

## 3. Phase P7 — `criticalchain-fold`

### 3.1. Goal

Fold the standalone `internal/criticalchain/*` subsystem into the general AH graph so that
there is a single supply-chain engine. Delete the `criticalchain` package hierarchy. Keep the
current `/api/v1/criticalchain/*` HTTP surface online by re-implementing it as a thin view
over generic AH queries.

### 3.2. Current state (as of P6 closeout)

- Package tree: `backend/internal/criticalchain/{,adapters,config,datasource,models,pipeline,repo,scoring,signal}`
  (~1600 LoC total; per-file LoC in `docs/taxonomy/cli_tools_inventory.yaml` comments).
- HTTP: `backend/internal/http/criticalchain/handlers.go` (225 LoC, 11 endpoints).
- Firestore collections used: `criticalchain_nodes`, `criticalchain_edges`, `criticalchain_conflict_zones`,
  `criticalchain_sanctions`. Kinds inside: `material`, `facility`, `processing_step`, `route`.
- Frontend consumers: `web/src/pages/CriticalChainPage.tsx`, `web/src/components/CriticalChainGraph.tsx`,
  `web/src/components/CriticalChainMap.tsx`, `web/src/lib/criticalchain-api.ts`,
  `web/src/components/{TopBar,SignalFusionPanel,MonitoringBoard,App}.tsx`.

### 3.3. Target mapping

Legacy criticalchain → AH taxonomy (already encoded in `migration_map.yaml`):

| Legacy                       | AH target                                                        |
|------------------------------|------------------------------------------------------------------|
| `Material`                   | `FragmentClass=product`, canonical_path `/product/<family>/<slug>`|
| `Facility` (Kind=mine)       | `FragmentClass=site`, Subtype=`deposit` + extraction siteTransform |
| `Facility` (Kind=refinery)   | `FragmentClass=site`, Subtype=`refinery` + refining siteTransform  |
| `Facility` (Kind=processor)  | `FragmentClass=site`, Subtype=`processing_plant` + processing siteTransform |
| `Facility` (Kind=manufacturer)| `FragmentClass=site`, Subtype=`factory` + assembly siteTransform  |
| `ProcessingStep` (edge)      | `supplies` edge + optionally `produces` edge on the source site  |
| `Route`                      | `site` fragment with subtype=`route` (or existing `/route/*`) + a `leg_connects` chain |
| `ConflictZone`               | `FragmentClass=geo`, Subtype=`affected_zone` + `disrupts` edges  |
| `SanctionRecord`             | `FragmentClass=evidence`, Subtype=`sanction` + `subject_to`/`assurance_of` relations depending on issuer |

`ProcessingStepType` values (`EXTRACTION/REFINING/PROCESSING/MANUFACTURING/TRANSPORT`) become
`siteTransforms[].kind` (`extraction|refining|processing|assembly|transport`) on the producing
site. `TRANSPORT` specifically becomes a `leg_connects` chain through a route node; do not emit
a siteTransform for transport.

### 3.4. Approach (step-by-step)

**P7.01 — Inventory** (`docs/taxonomy/criticalchain_fold_inventory.md`; committed):
- List every symbol the general graph is missing for the fold to work (e.g., if `siteTransforms`
  helper is missing, note it).
- List every `criticalchain_*` Firestore collection that must be migrated. Wire these into
  `migration_map.yaml` under a new `collections_map:` block if not already present; reuse
  existing mappings otherwise.
- List every frontend call site in `web/src` that hits `/api/v1/criticalchain/*`.

**P7.02 — `siteTransforms` helper on `HorizonFragment`**

Add, in `backend/internal/horizon/models/site_transforms.go` (new file):

```go
type SiteTransform struct {
    Kind          string   `json:"kind"`               // extraction|refining|processing|assembly
    InputProducts []string `json:"inputProducts,omitempty"`  // canonical paths
    OutputProducts []string `json:"outputProducts,omitempty"` // canonical paths
    YieldRatio    float64  `json:"yieldRatio,omitempty"`
    Confidence    float64  `json:"confidence,omitempty"`
    Source        string   `json:"source,omitempty"`   // evidence ref
}

func (f *HorizonFragment) SiteTransforms() []SiteTransform   // decode from Attributes["site_transforms"]
func (f *HorizonFragment) SetSiteTransforms([]SiteTransform) // encode into Attributes["site_transforms"]
func (f *HorizonFragment) AppendSiteTransform(SiteTransform) // idempotent by (kind, inputProducts, outputProducts)
```

Unit tests in `site_transforms_test.go`. ReadLints. Commit: `backend: add siteTransforms helper on HorizonFragment`.

**P7.03 — Write `criticalchain` viewer that reads from AH graph**

Create `backend/internal/criticalchainview/` (new package) with three responsibilities:

1. Read materials from AH `/product/*` fragments filtered by category attribute.
2. Read facilities from AH `/site/*` fragments by subtype.
3. Read routes + sanctions + conflict zones from AH graph and project them to the legacy
   `Material/Facility/Route/…` JSON shapes **at response time** (so the frontend doesn’t break).

Do **not** copy any mutating logic from `criticalchain/pipeline/*`. The pipeline becomes
obsolete; its ingestion responsibilities move to the scout ingestion path (if not already).

**P7.04 — Rewrite HTTP handlers**

Replace `backend/internal/http/criticalchain/handlers.go` with a new implementation that takes a
`criticalchainview.Service` instead of `criticalchain.Service` + `pipeline.Runner`. Keep URL
shapes exactly the same. The admin `/api/v1/criticalchain/admin/pipeline/run` endpoint returns
`410 Gone` with a message pointing at `cmd/taxonomy_migrate`. Update `cmd/server/main.go`
accordingly.

**P7.05 — Delete the package**

`git rm -r backend/internal/criticalchain`. Remove imports in `cmd/server/main.go`. Verify
`go build ./... && go vet ./...` clean. `go test ./internal/criticalchainview/... ./internal/http/criticalchain/...`
green.

**P7.06 — Data side**

The `criticalchain_*` Firestore collections are **not** touched by P7. Their migration lives
inside P10 (`migrate-cli`); P10 reads them, emits AH fragments/edges into `horizon_fragments` +
`dependency_edges`, and then deletes the legacy collections.

### 3.5. Acceptance

- `go build ./... && go vet ./...` clean.
- `go test ./...` green modulo the 2 pre-existing failing packages (§1.3).
- `/api/v1/criticalchain/materials`, `/facilities`, `/routes`, `/conflict-zones`, `/graph/{id}`,
  `/signals`, `/country-risk`, `/chokepoints` all return JSON of the same shape (`Material`,
  `Facility`, `Route`, `ConflictZone` structs moved to `criticalchainview`) — at least for a
  staging dataset the user provides. If the dataset is empty, return `[]`, not an error.
- `/api/v1/criticalchain/admin/pipeline/run` returns 410 Gone.
- `criticalchain_fold_inventory.md` is committed under `docs/taxonomy/`.
- `rg -n 'internal/criticalchain([^v])' backend/ | rg -v '^backend/internal/criticalchainview'`
  returns empty.

### 3.6. Commits

1. `backend: add siteTransforms helper on HorizonFragment`
2. `backend: introduce criticalchainview package reading AH graph`
3. `backend: reimplement /api/v1/criticalchain/* handlers on criticalchainview`
4. `backend: delete internal/criticalchain and pipeline runner`
5. `docs: criticalchain fold inventory + migration notes`

---

## 4. Phase P8 — `pathquery-layer`

### 4.1. Goal

Introduce `backend/internal/horizon/pathquery/*` that centralizes "find supply-chain paths
between A and B" queries, and a Firestore collection `horizon_path_cache` that memoizes them.
Unblocks the frontend inspectors from hand-rolling path queries.

### 4.2. Context

Existing packages in the neighborhood (read before designing):

- `backend/internal/horizon/pathbinding/` — binds a given path to route geometry.
- `backend/internal/horizon/pathbuilder/` — builds concrete `SupplyChainPathPayload`s from
  materialized artifacts.
- `backend/internal/horizon/pathstress/` — stress analytics.
- `backend/internal/horizon/repo/exposure_paths.go` — path-family projections (read path only).

`pathquery` is a **read-side** query API that unifies four things the frontend currently has to
stitch together: (1) direct materialized paths (`MaterializedKind=SUPPLY_CHAIN_PATH`), (2)
ad-hoc BFS over `supplies`/`produces`/`leg_connects` edges, (3) pathbuilder invocations, (4)
path_family projections.

### 4.3. Design (surface area)

```go
package pathquery

type Query struct {
    Tenant        string
    Project       string
    From          Endpoint  // canonical path OR fragment id
    To            Endpoint  // canonical path OR fragment id (optional — nil means "any downstream")
    ProductAxis   string    // canonical product path filter (optional)
    MaxHops       int       // default 8
    RelationKinds []ah.RelationName // default: {supplies, produces, leg_connects}
    IncludeStress bool
    IncludeGeo    bool
    CacheTTL      time.Duration // default 24h; 0 disables cache; -1 forces refresh
    Context       SearchContext // lens id, scope filter, as-of time
}

type Result struct {
    Paths        []*models.MaterializedArtifact // zero or more; each is a SUPPLY_CHAIN_PATH
    FromCache    bool
    GeneratedAt  time.Time
    CacheKey     string
    Diagnostics  Diagnostics // counts, fallbacks, ran pathbuilder y/n
}

type Service interface {
    QueryPaths(ctx context.Context, q Query) (*Result, error)
    InvalidatePathCache(ctx context.Context, key string) error
    InvalidateByNode(ctx context.Context, canonicalPath string) error // cascade invalidation
}
```

Key points:

- `CacheKey` derived from `(tenant, project, from, to, productAxis, maxHops, relationKinds, as-of-date)`,
  hashed SHA-256, prefixed with `pq_`.
- Cache collection: `horizon_path_cache` on Firestore. Document shape:
  ```
  {
    "cacheKey": "pq_abc...",
    "tenant":   "...",
    "project":  "...",
    "query":    { ...Query without Context, for debugging... },
    "pathIds":  ["matArt_id_1", "matArt_id_2"],
    "diagnostics": { ... },
    "generatedAt": <timestamp>,
    "expiresAt":   <timestamp>,
    "nodeRefs":    ["/org/a", "/site/b", "/product/c"]  // for reverse-index invalidation
  }
  ```
- `InvalidateByNode` uses `array-contains` on `nodeRefs` to cascade-invalidate.

### 4.4. Approach

**P8.01** — Inventory: list the files that currently do ad-hoc path finding. Target candidates:
  `backend/internal/http/horizon/{supply_chains,fragment_trace,identify_stresses,lens_report_artifact}.go`,
  `backend/internal/horizon/supplychainreport/*`, `backend/internal/horizon/supplychain/one_hop_downstream.go`.

**P8.02** — Present the `Service` interface + Firestore shape in a short design doc at
  `docs/taxonomy/pathquery_design.md` and wait for user approval before implementation. This is
  an escalation point (§1.5).

**P8.03** — After approval: implement `pathquery.Service` (in-memory + Firestore-backed impl).

**P8.04** — Wire into `cmd/server/main.go`: construct the service once, pass it into HTTP
  handler constructors.

**P8.05** — Add HTTP endpoint: `GET /api/v1/horizon/pathquery?from=...&to=...&productAxis=...&maxHops=...&relations=...&cacheTtl=...&asOf=...`
  Response mirrors `Result` with `Paths` projected to the existing `SupplyChainPathPayload`
  wire shape.

**P8.06** — Migrate `supply_chains.go` and `fragment_trace.go` to delegate to `pathquery`
  internally. Do not change their response format yet (that is P12).

**P8.07** — Tests: emulator-backed test in `pathquery_test.go` covering cache hit/miss,
  invalidation, max-hop truncation, relation-kind filtering, product-axis filtering.

**P8.08** — Observability: `pathquery_cache_hits_total` / `pathquery_cache_misses_total` /
  `pathquery_builder_invocations_total` (reuse existing metrics package under
  `backend/internal/horizon/metriccore`).

### 4.5. Acceptance

- Design doc approved by user.
- `go build ./... && go vet ./... && go test ./...` all clean.
- New package `backend/internal/horizon/pathquery` with `>= 80%` line coverage in its own
  tests.
- `supply_chains.go` and `fragment_trace.go` both route through `pathquery.Service` (verified
  by removing the old path-finding code paths).
- Emulator test demonstrates end-to-end: miss → write → second call hits cache; node
  invalidation empties cache for that node.

### 4.6. Commits

1. `docs: pathquery design doc`
2. `backend: introduce horizon/pathquery service + Firestore cache`
3. `backend: expose /api/v1/horizon/pathquery endpoint`
4. `backend: route supply_chains + fragment_trace handlers through pathquery`
5. `backend: pathquery emulator integration test + metrics`

---

## 5. Phase P9 — `materialized-payload-rewrite` (Variant A)

### 5.1. Goal

Rewrite the writer side of every `MaterializedArtifact.Payload` producer so that new documents
carry AH-shaped payloads. Readers are **not** made backward-compatible. Legacy documents become
invisible to the overlay layer until P11 (`migrate-run`) rewrites them.

### 5.2. What changes in the payload shape

Legacy payload today uses string types for nodes (`"org"`, `"fac"`, `"product"`, `"deposit"`)
and string types for edges (`"SUPPLY_LINK"`, `"LOGISTICS"`, `"ONTOLOGY"` with
`relation="TRANSFORMS_INTO"` etc.). Rewrite to:

| Legacy `type`/`relation`        | AH target                                           |
|---------------------------------|-----------------------------------------------------|
| `"org"`                         | `class="org"`                                       |
| `"fac"`                         | `class="site"` + optional `subtype`                 |
| `"deposit"`                     | `class="site"`, `subtype="deposit"`                 |
| `"product"`                     | `class="product"`                                   |
| `"SUPPLY_LINK"`                 | `relation="supplies"`, `role="product_flow"`        |
| `"LOGISTICS"`                   | `relation="leg_connects"`, `role="logistics_leg"`   |
| `"ONTOLOGY"` + `TRANSFORMS_INTO`| Removed entirely — information moves to `siteTransforms` on the producing site node (P7.02) |
| `"ONTOLOGY"` + `ASSEMBLES_INTO` | Removed — same                                      |
| `"ONTOLOGY"` + `USED_IN`        | Removed — redundant with `produces`                 |

`SupplyChainAssemblyGraphPayload.transformEdges` and `assemblyEdges`: move the content to
`siteTransforms` on the relevant actor node; drop the edges from the payload.

`RelationsGraphPayload` (`REGULATORY_IMPACT_MAP`, `GOVERNANCE_CHAIN`, `ECOSYSTEM_MAP`, etc.):
- `nodes[].type`: string → `ah.FragmentClass`. Add optional `subtype`.
- `edges[].relation`: string → `ah.RelationName` (canonical snake_case).

### 5.3. What does **not** change

- `MaterializedKind` enum values (`SUPPLY_CHAIN_PATH`, etc.) — stay as-is; they are the
  schema discriminator for Firestore collections and overlays.
- Payload schema version (`v: 1`). Version is not bumped; the Variant A decision is that legacy
  docs become invisible, not that we run two schemas concurrently.
- `MaterializedArtifact.Labels`, `NodeRefs`, `GeoRefs`, etc. — these already hold canonical
  paths, no shape change.

### 5.4. Approach

**P9.01** — Update `backend/internal/horizon/models/materialized_artifact.go`:
- `SupplyChainPathNode.Type` → remove; add `Class ah.FragmentClass` + `Subtype string`.
- `SupplyChainPathEdge.Type` → remove; add `Relation ah.RelationName` (kept as string in JSON
  tag `"relation"` for stability).
- Delete `SupplyChainPathEdge.Relation` (the ontology transition field; obsolete).
- Rewrite `validateSupplyChainPathPayload` to enforce AH class/relation values via
  `ah.IsKnownFragmentClass` / `ah.IsKnownRelation`. No fallback for legacy strings.
- Rewrite `validateSupplyChainAssemblyGraphPayload`: drop `transformEdges` / `assemblyEdges`
  validation; validate that `actorNodes[].role` can carry a `siteTransforms[]` array.
- Rewrite `validateRelationsGraphPayload`: enforce `ah.FragmentClass` on nodes and
  `ah.RelationName` on edges.

**P9.02** — Update every writer to emit the new shape:
- `backend/internal/horizon/pathbuilder/supply_chain_builder.go` — main supply-chain path writer.
- `backend/internal/horizon/pathbuilder/supply_chain_assembly_builder.go` — assembly graph writer.
- `backend/internal/horizon/pathbuilder/domain_artifact_builder.go` — regulatory/governance/ecosystem.
- `backend/internal/horizon/pathbuilder/regulatory_ancillary.go` — timeline/cascade maps.
- `backend/internal/horizon/watchruntime/macro_agents.go` — any macro-level overlay synthesis.
- `backend/internal/horizon/scout/engine_ext.go` — scout-emitted overlays.
- `backend/internal/horizon/scout/supply_chain_diagnostics.go` — diagnostic overlays.
- `backend/internal/horizon/scout/chain_reuse.go` — reuse-based overlays.
- `backend/internal/horizon/supplychainreport/collect.go` — report materialization.
- `backend/internal/horizon/repo/fragments_merge.go` — if it writes materialized artifacts.

For each writer: replace every `Type: "org"` with `Class: ah.FragmentClassOrg`, every
`Type: "fac"` with `Class: ah.FragmentClassSite`, etc. Delete every `Type: "SUPPLY_LINK"` edge
construction in favor of `Relation: ah.RelationNameSupplies`. Move transform/assembly edges
into `siteTransforms` on the appropriate site node.

**P9.03** — Update every test fixture that builds a payload inline:
- `materialized_artifact_test.go`, `pathbuilder/**_test.go`, `http/horizon/**_test.go` that
  contains payload fixtures.
- Any `testdata/*.json` fixture for materialized artifacts.

**P9.04** — Remove the legacy reader pathways **after** writers are all updated:
- Delete any `switch edge.Type { case "SUPPLY_LINK": ... }` branch that exists in readers.
- Leave the `Payload map[string]interface{}` intact; readers decode into the new struct shapes
  via the existing helper functions in `materialized_artifact_helpers.go`.

**P9.05** — Audit `go test ./internal/...` — any test that breaks because it reads legacy
  payloads from an emulator seed needs its seed updated.

**P9.06** — Update `docs/taxonomy/materialized_payload.md` (new file) that documents the new
  payload shape, with examples for each Kind. This becomes the reference for P12 (frontend).

### 5.5. Acceptance

- `go build ./... && go vet ./... && go test ./internal/...` all clean.
- Zero `Type: "(org|fac|product|deposit)"` literals in non-test backend code outside
  `models/graph_intent.go` (wire boundary) and `models/fragment_class.go` (alias map).
- Zero `Type: "(SUPPLY_LINK|LOGISTICS|ONTOLOGY)"` literals in writer code.
- Every writer produces `SupplyChainPathNode`, `SupplyChainPathEdge`,
  `SupplyChainAssemblyNode`, `SupplyChainAssemblySupplyEdge`, `RelationsGraphNode`,
  `RelationsGraphEdge` with AH-typed fields.
- `docs/taxonomy/materialized_payload.md` committed.

### 5.6. Commits

1. `models: rewrite materialized payload structs to AH class/relation types`
2. `models: tighten materialized payload validators against AH taxonomy`
3. `backend: rewrite supply-chain payload writers to AH shape`
4. `backend: rewrite assembly graph writers; transforms move to siteTransforms`
5. `backend: rewrite regulatory/governance/ecosystem payload writers to AH shape`
6. `tests: update materialized payload fixtures to AH shape`
7. `docs: materialized payload schema reference`

---

## 6. Phase P10 — `migrate-cli`

### 6.1. Goal

Build `backend/cmd/taxonomy_migrate` — a Go CLI that rewrites every legacy document in Firestore
to AH shape, in place, with dry-run / apply / validate / rollback modes, guided by
`docs/taxonomy/migration_map.yaml`.

### 6.2. Scope (collections touched)

1. `horizon_fragments` — `Type` → `Class` + `Subtype`; canonicalPath rewrites; lift-to-attributes
   fields; SUPPLY_LINK → site/supply_link; DOMAIN/SPHERE deletions (per map).
2. `dependency_edges` — `Type` → `Relation`; qualifier rewrites; per-pair-routing fans; reverse
   dispositions; `TRANSFORMS_INTO`/`ASSEMBLES_INTO` → `siteTransforms` on source site (edge
   deleted); `SUPPLY_LINK` fragment → `supplies` edge + `leg_connects` chain (the fragment is
   deleted after edge synthesis).
3. `materialized_artifacts` — payload rewrites per §5.2.
4. `horizon_lenses` — `viewConfig.filterRelTypes` / `excludeRelTypes` rewritten from UPPER_SNAKE
   to snake_case AH.
5. `ontology_facts` — relation name normalization in `subject`/`predicate` fields.
6. `worldmonitor_*` — legacy fragment types updated via same map.
7. `scout_jobs` / `scout_outbox` / `scout_batches` — no data shape change, but legacy relation
   strings in queued payloads rewritten.
8. `criticalchain_nodes` / `_edges` / `_conflict_zones` / `_sanctions` — read-only projection
   into `horizon_fragments` + `dependency_edges` (see §3.6); originals deleted after migration.

### 6.3. CLI surface

```
taxonomy_migrate \
  --project=<gcp-project> \
  --firestore-database=<name> \        # default "(default)"
  --collections=<comma-separated>      # default: all scopes §6.2
  --mode=<dryrun|apply|validate|rollback> \
  --map=<path to migration_map.yaml>   # default docs/taxonomy/migration_map.yaml
  --snapshot-dir=<dir>                 # required for apply+rollback
  --batch-size=<n>                     # default 500
  --parallel=<n>                       # default 8
  --from-doc-id=<id>                   # resume marker
  --ids-file=<file>                    # optional explicit id list
  --report=<out.json>                  # machine-readable report
  --log-level=<info|debug>
```

Modes:

- `dryrun` — reads every affected document, simulates the rewrite, writes no changes, produces
  a machine-readable report + human summary. Also asserts that every legacy value in the corpus
  has a mapping in the YAML; fails otherwise.
- `apply` — must be preceded by a `dryrun` that produced a clean report no older than 24 hours
  (report is checksummed). Writes a **snapshot** of every affected document before writing
  changes to `<snapshot-dir>/<timestamp>/<collection>/<id>.json`, then rewrites.
- `validate` — re-reads every rewritten document and asserts: (a) no legacy enum values remain,
  (b) every `HorizonFragment.Class` is known, (c) every `DependencyEdge.Relation` is known,
  (d) materialized payloads pass the new validators (§5.1), (e) every canonical path follows
  the AH pattern, (f) no orphan edges.
- `rollback` — reads the snapshot directory and restores each document verbatim. Refuses to run
  if the snapshot is older than 30 days or if the document has been touched after the snapshot
  timestamp.

### 6.4. Design rules

- **Idempotency** — every per-document rewrite must be a no-op when applied to an already-
  rewritten document. Use a sentinel attribute `attributes.__ah_migrated_at` (ISO timestamp)
  on each migrated doc to skip on repeat runs.
- **Atomicity** — per-document rewrites run in a Firestore transaction that touches only that
  document plus any synthesized children (supply_link → edge + chain). Cross-collection writes
  go through a write batch with bounded size.
- **Resume-ability** — the CLI periodically writes progress (`last-processed-id` per
  collection) to `<snapshot-dir>/<timestamp>/.progress.json`. On resume, picks up from there.
- **Concurrency** — per-collection workers, shared rate limiter. Default 8 workers, 500 writes
  per batch. Respects Firestore quota errors with exponential backoff.
- **Never delete before writing the replacement** — for `SUPPLY_LINK` fragment collapse: write
  the `supplies` edge and `leg_connects` chain first, verify round-trip, *then* delete the
  fragment.
- **Migration map as contract** — if a document has a legacy value that has no mapping in
  `migration_map.yaml`, the CLI **fails hard** with a report of unmapped values. The user
  decides whether to add the mapping (escalation §1.5) or delete the outlier.

### 6.5. File layout

```
backend/cmd/taxonomy_migrate/
  main.go                 # CLI flags + orchestration
  README.md               # usage, runbook
  migrator.go             # Migrator struct, collection dispatch
  map_loader.go           # YAML → typed map
  snapshot.go             # snapshot write/read
  fragments_migrator.go   # horizon_fragments rewrite
  edges_migrator.go       # dependency_edges rewrite
  materialized_migrator.go # materialized_artifacts payload rewrite
  lenses_migrator.go      # horizon_lenses rewrite
  ontology_facts_migrator.go
  worldmonitor_migrator.go
  scout_queues_migrator.go
  criticalchain_migrator.go  # reads criticalchain_* and projects into horizon_fragments + edges
  validator.go            # validate mode
  rollback.go             # rollback mode
  report.go               # machine-readable reports
  progress.go             # resume markers
  testdata/
    fixtures/<collection>/*.json
  *_test.go               # emulator-backed tests per collection
```

### 6.6. Approach (step-by-step)

**P10.01** — Scaffold CLI, flag parsing, config loading, migration-map parser. Unit tests.
**P10.02** — Implement `fragments_migrator.go` with **dry-run only**. Include every
  disposition from `fragment_type_map`. Tests against Firestore emulator with hand-crafted
  fixtures.
**P10.03** — Implement `edges_migrator.go` with dry-run only. Cover all dispositions:
  `rename`, `reverse`, `rewrite_edge`, `fold_to_attributes` (for `TRANSFORMS_INTO` /
  `ASSEMBLES_INTO` — these produce `siteTransforms` mutations on the source node), and
  `per_pair_routing`.
**P10.04** — Implement `materialized_migrator.go` — dry-run. Payload rewrites per §5.2.
**P10.05** — Implement `lenses_migrator.go`, `ontology_facts_migrator.go`, `worldmonitor_migrator.go`,
  `scout_queues_migrator.go`, `criticalchain_migrator.go` — all dry-run.
**P10.06** — Implement `snapshot.go` + flip every migrator to `apply` mode.
**P10.07** — Implement `validator.go` + `rollback.go`.
**P10.08** — Reporting (`report.go`) + progress (`progress.go`) + emulator integration test
  suite.
**P10.09** — Runbook (`README.md`) with exact commands for P11.

### 6.7. Acceptance

- `go build ./cmd/taxonomy_migrate` clean.
- Emulator test suite green: seed → dryrun → apply → validate → rollback → re-validate (seed).
- Every disposition in `migration_map.yaml` has at least one emulator fixture exercising it.
- Runbook exists.
- `taxonomy_migrate --mode=dryrun --collections=horizon_fragments` against an empty emulator
  exits zero with an empty report.

### 6.8. Commits

1. `cmd/taxonomy_migrate: scaffold CLI + migration map loader`
2. `cmd/taxonomy_migrate: fragments dry-run`
3. `cmd/taxonomy_migrate: dependency edges dry-run`
4. `cmd/taxonomy_migrate: materialized artifacts dry-run`
5. `cmd/taxonomy_migrate: lenses/ontology_facts/worldmonitor/scout_queues dry-run`
6. `cmd/taxonomy_migrate: criticalchain projection dry-run`
7. `cmd/taxonomy_migrate: snapshot + apply mode`
8. `cmd/taxonomy_migrate: validate + rollback modes`
9. `cmd/taxonomy_migrate: emulator-backed test suite`
10. `docs: taxonomy_migrate runbook`

---

## 7. Phase P11 — `migrate-run`

### 7.1. Goal

Run `taxonomy_migrate` against the **test** Firestore environment first, collect reports, then
(after explicit user green-light) against production. This phase is operational and must be
driven by the user. The agent does not run `apply` against production unilaterally.

### 7.2. Runbook (the agent follows these steps)

**P11.01 — Pre-flight (test env)**

```bash
gcloud config set project <test-project>
gcloud firestore export gs://<backup-bucket>/pre-migration/$(date +%F) --collection-ids=horizon_fragments,dependency_edges,materialized_artifacts,horizon_lenses,ontology_facts,worldmonitor_fragments,scout_jobs,scout_outbox,scout_batches,criticalchain_nodes,criticalchain_edges,criticalchain_conflict_zones,criticalchain_sanctions
```

Commit the export URI + timestamp into `docs/taxonomy/migrate_run_log.md`.

**P11.02 — Dry-run (test env)**

```bash
cd backend && go run ./cmd/taxonomy_migrate \
  --project=<test-project> \
  --mode=dryrun \
  --snapshot-dir=/tmp/migrate-test-$(date +%F) \
  --report=/tmp/migrate-test-dryrun.json
```

If the dry-run reports unmapped values, escalate to user (§1.5). Otherwise commit the report
into `docs/taxonomy/reports/test-dryrun-<date>.json`.

**P11.03 — Apply (test env)**

Only after dry-run report is clean:

```bash
cd backend && go run ./cmd/taxonomy_migrate \
  --project=<test-project> \
  --mode=apply \
  --snapshot-dir=/tmp/migrate-test-snap-$(date +%F) \
  --report=/tmp/migrate-test-apply.json
```

**P11.04 — Validate (test env)**

```bash
cd backend && go run ./cmd/taxonomy_migrate \
  --project=<test-project> \
  --mode=validate \
  --report=/tmp/migrate-test-validate.json
```

Expected: zero errors. If any, rollback and escalate.

**P11.05 — Smoke tests (test env)**

Run the HTTP scenarios (Tesla battery, Dubai CIT, Kazakhstan VAT) against the test deployment.
Compare overlay counts, path counts, focus-object scores against pre-migration snapshots
(stored in `docs/horizon/scenarios/*/pre-migration-<date>.json`; create them during P11.01 if
missing).

**P11.06 — Production**

User explicitly triggers. Repeat P11.01–P11.05 against production. Agent does not run
`--mode=apply` on production without an explicit instruction message in the current chat.

### 7.3. Acceptance

- Test env `validate` report has zero errors.
- Scenario smoke tests match pre-migration baselines within ±1% on aggregate counts.
- `docs/taxonomy/migrate_run_log.md` contains timestamps, report paths, and user approvals for
  every run.
- Rollback drill: at least once during the test env run, execute a `rollback` and re-`apply`
  cycle to prove the round-trip.

### 7.4. Commits

1. `docs: capture pre-migration test scenario baselines`
2. `docs: test env dry-run + apply + validate reports`
3. `docs: production migration log`

---

## 8. Phase P12 — `frontend-refactor`

### 8.1. Goal

Bring `web/` into alignment with the AH taxonomy on the wire, consume `taxonomy.generated.ts`
everywhere, render subtypes, use `pathquery` in inspectors, drop the hand-maintained
`DependencyType` union.

### 8.2. Scope inventory (read before coding)

**Types to align** (`web/src/types/`):
- `lensItem.ts` — 48-entry `DependencyType` union must become `import { RelationName } from
  '../lib/taxonomy.generated'`. The union has legacy UPPER_SNAKE values like `SUPPLIES_TO`,
  `TRANSFORMS_INTO`; those become snake_case AH.
- `horizonView.ts` — `filterRelTypes: string[]` becomes `filterRelations: RelationName[]`.
- `horizonWatch.ts`, `ontology.ts`, `artifact.ts`, `generated.ts` — audit each for taxonomy
  mentions.
- `index.ts` — exports.

**Components + hooks that inspect fragment/edge taxonomy**:
- `web/src/components/HorizonGraphNodeTypes.tsx` — color/icon mapping; switch to Class+Subtype.
- `web/src/components/HorizonEntityInspector.tsx` — display Class+Subtype, render siteTransforms.
- `web/src/components/LensCanvasCockpit.tsx` — type filters.
- `web/src/components/MapDependencyTreeView.tsx` — relation filtering.
- `web/src/hooks/useGeoEdgeLayer.ts` — type-keyed geo rendering.
- `web/src/lib/criticalchain-api.ts` — lives under `web/src/lib/`, serves `CriticalChainPage`;
  leave as-is at the wire level (backend still exposes `/api/v1/criticalchain/*`).

**Pages**:
- `HorizonPage.tsx`, `HorizonEntityInspector.tsx`, all `Horizon*ProfilePrintPage.tsx`, `DiscoveryPage.tsx`.
- `CriticalChainPage.tsx` — keep but remove hand-rolled BFS in favor of `pathquery`.

### 8.3. Approach

**P12.01 — Type alignment**
- Delete the hand-rolled `DependencyType` in `lensItem.ts`. Replace with
  `type RelationName = import('../lib/taxonomy.generated').RelationName`.
- Add a small `legacyRelationToAh` map in `web/src/lib/taxonomy.ts` (new file) that translates
  any remaining UPPER_SNAKE strings coming from older cached lens items on the client. Log a
  console warning when used; this lets P13 confirm cache drain.
- Retype every prop / state that carries a relation: `string | DependencyType` → `RelationName`.
- Commit: `web: replace hand-rolled DependencyType with generated RelationName`.

**P12.02 — Fragment class+subtype**
- Every component that reads `fragment.type` (e.g., `type === 'ORGANIZATION'`) now reads
  `fragment.class` + `fragment.subtype`. The backend wire format already emits
  `class`+`subtype` since P5.
- Subtype-aware color/icon mapping: extend `HorizonGraphNodeTypes.tsx` with a `(class, subtype)
  → {color, icon, label}` table sourced from `taxonomy.generated.ts`.
- Commit: `web: render fragments by class+subtype using generated taxonomy`.

**P12.03 — siteTransforms rendering**
- `HorizonEntityInspector.tsx` gets a new "Site transforms" panel when the selected fragment
  has `attributes.site_transforms`. Display kind, input products (linkified), output products,
  yield ratio.
- Commit: `web: render siteTransforms on site inspector`.

**P12.04 — Supply legs along /route**
- When rendering `SupplyChainPathPayload.legs[]` on the map, draw the leg geometry along the
  `leg_connects` chain (resolved from `leg.transportViaRefs`). Today the code falls back to a
  great-circle line; after this phase, prefer the chain geometry.
- Commit: `web: render supply legs along /route geometry`.

**P12.05 — pathquery integration**
- Replace every manual BFS + `/api/v1/horizon/supply-chains` call in inspectors with a single
  call to `/api/v1/horizon/pathquery?from=...&to=...&productAxis=...`.
- Components affected: `HorizonEntityInspector.tsx`, `HorizonOnDemandDependencyResolverModal.tsx`,
  `SignalFusionPanel.tsx`, `MonitoringBoard.tsx`, `LensCanvasCockpit.tsx`,
  `CriticalChainPage.tsx`.
- Commit: `web: route supply-chain inspectors through /api/v1/horizon/pathquery`.

**P12.06 — Materialized payload readers**
- Every reader of `SupplyChainPathPayload` / `SupplyChainAssemblyGraphPayload` /
  `RelationsGraphPayload` switches to `class`/`subtype`/`relation` fields. Legacy fixtures in
  tests are rewritten; seed data in `staticTransportFlows.ts` etc. gets the same treatment.
- Commit: `web: consume materialized payloads in AH shape`.

**P12.07 — Filter chips / legend**
- `graphViewConfig.filterRelTypes` (UPPER_SNAKE) → `graphViewConfig.filterRelations` (AH
  snake_case). Migrate any persisted user preferences via the fallback in §P12.01.
- Commit: `web: migrate lens filter chips to AH relations`.

**P12.08 — Tests + build**
- `npm run build && npm run typecheck && npm run lint && npm test` clean.
- Playwright / existing e2e (if any) run green; verify with screenshots where relevant.

### 8.4. Acceptance

- `npm run gen:taxonomy:check` clean.
- `npm run build && npm run typecheck && npm run lint && npm test` all clean.
- Zero hand-rolled taxonomy unions in `web/src/types` — only re-exports from
  `taxonomy.generated.ts`.
- Manual smoke: load the Tesla battery scenario; inspector shows Class=site, Subtype=factory,
  siteTransforms panel with `assembly` kind; path queries round-trip through `/pathquery`.

### 8.5. Commits

Listed inline in §8.3; 7 commits in order P12.01–P12.07, plus one for P12.08 touch-ups if
needed (`web: typecheck/test fallout from AH refactor`).

---

## 9. Phase P13 — `cleanup-code`

### 9.1. Goal

Remove every shim, alias, helper, and fallback that was introduced for transition. After this
phase, the codebase contains zero legacy taxonomy concepts except at explicit wire/Firestore
boundaries.

### 9.2. Targets (exact files)

**Legacy alias shims** (delete or gut to empty):
```
backend/internal/discovery/legacy_aliases.go
backend/internal/horizon/focusobject/legacy_aliases.go
backend/internal/horizon/metriccore/legacy_aliases.go
backend/internal/horizon/merge/legacy_aliases.go
backend/internal/horizon/manualresolve/legacy_aliases.go
backend/internal/horizon/signals/legacy_aliases.go
backend/internal/horizon/importance/legacy_aliases.go
backend/internal/horizon/scout/pipeline/legacy_aliases.go
backend/internal/horizon/scout/intra_graph/legacy_aliases.go
backend/internal/horizon/scout/legacy_aliases.go
backend/internal/horizon/scout/contracts/legacy_aliases.go
backend/internal/horizon/scout/agents/legacy_aliases.go
backend/internal/horizon/analytical/legacy_aliases.go
backend/internal/horizon/pathbinding/legacy_aliases.go
backend/internal/horizon/supplychain/legacy_aliases.go
backend/internal/horizon/lens/legacy_aliases.go
backend/internal/horizon/profile/legacy_aliases.go
backend/internal/horizon/signallanding/legacy_aliases.go
backend/internal/horizon/propagation/legacy_aliases.go
backend/internal/horizon/scenarios/teslabattery/legacy_aliases.go
backend/internal/horizon/scenarios/kzvat/legacy_aliases.go
backend/internal/horizon/scenarios/dubaicit/legacy_aliases.go
backend/internal/horizon/enrich/legacy_aliases.go
backend/internal/horizon/materialize/legacy_aliases.go
backend/internal/horizon/pathbuilder/legacy_aliases.go
backend/internal/horizon/domain/legacy_aliases.go
backend/internal/horizon/risk/legacy_aliases.go
backend/internal/horizon/repo/legacy_aliases.go
backend/internal/horizon/policy/legacy_aliases.go
backend/internal/horizon/workscope/legacy_aliases.go
backend/internal/http/horizon/legacy_aliases.go
```

**Legacy helpers**:
```
backend/internal/horizon/importance/legacy_helpers.go
backend/internal/horizon/scout/legacy_helpers.go
backend/internal/horizon/scout/agents/legacy_helpers.go
backend/internal/horizon/models/supply_link_helpers.go
backend/internal/horizon/propagation/legacy_helpers.go
backend/internal/horizon/pathbuilder/legacy_helpers.go
backend/internal/horizon/domain/legacy_helpers.go
```

**Legacy wire tokens still present in `cmd/tools/*`** (from P6 tech-debt note):
- `backend/cmd/tools/horizon_import_worldmonitor/main.go`
- `backend/cmd/tools/horizon_supply_repair/main.go`
- `backend/cmd/tools/horizon_smart_dedup/main.go`

After Phase 11 succeeded, data in Firestore is already AH-shape. These tools must switch their
queries to AH relation names; remove the `ah.RelationName("located_in")`-style literals.

**`IsLegacy*` predicates on HorizonFragment** — audit each call site. If P11 succeeded, the
data no longer contains legacy fragments; predicates always return false. Delete the
predicates and their call sites. Exceptions: keep `IsLegacySupplyLink` in `models/fragment.go`
as a one-line guard that always returns false with a comment explaining why (reduces risk of
silent re-introduction).

**Wire boundary exemptions** (keep):
- `backend/internal/horizon/models/graph_intent.go` — accepts legacy wire input.
- `backend/internal/horizon/models/fragment_class.go` — the `LegacyFragmentTypeAliases` map is
  consumed by `migrate-cli` and by `UnmarshalJSON`; keep, but mark deprecated.
- `backend/internal/horizon/models/dependency_edge.go::UnmarshalJSON` — wire decode.
- `backend/internal/horizon/models/transmission_qualifiers.go` — legacy qualifier keys still
  arrive from third-party integrations.

### 9.3. Approach

**P13.01** — Delete legacy shim files one package at a time; for each, run `go build` +
  `go test` for that package and immediate dependents.
**P13.02** — Delete legacy helpers; re-home the still-useful helpers into regular package
  files with non-legacy names.
**P13.03** — Remove 3 `cmd/tools/*` wire-token literals; switch to AH relations.
**P13.04** — Audit `IsLegacy*` predicates; delete or neuter per §9.2.
**P13.05** — Run the legacy-string audit from §2; for each remaining hit, either justify with a
  code comment (`// wire boundary: ...`) or remove.
**P13.06** — Run `staticcheck ./...` (install via `go install honnef.co/go/tools/cmd/staticcheck@latest`);
  zero unused symbols. `go vet ./...` clean.
**P13.07** — Delete obsolete TODO comments about "remove after migration" that are now stale.
**P13.08** — Frontend side: remove `legacyRelationToAh` fallback in
  `web/src/lib/taxonomy.ts` (the console warning from P12.01 should not have fired during P11
  smoke — confirm via browser devtools replay logs before deleting).

### 9.4. Acceptance

- `go build ./... && go vet ./... && go test ./internal/...` clean.
- `staticcheck ./...` clean.
- Legacy-string audit from §2 returns **zero** hits outside the explicit wire-boundary
  exemption list.
- No `legacy_aliases.go` or `legacy_helpers.go` files anywhere in `backend/internal/*`.
- Frontend: `npm run build && npm run typecheck && npm run lint && npm test` clean; no
  `legacyRelationToAh` in `web/src/`.

### 9.5. Commits

Group by package/surface. Minimum ~8–12 commits:

1. `cleanup: remove http/horizon legacy aliases`
2. `cleanup: remove horizon/models/supply_link_helpers (transitional)`
3. `cleanup: remove horizon/{scout,scout/agents,scout/pipeline,…} legacy shims` (can be split
   further if diffs are big)
4. `cleanup: remove horizon/{pathbuilder,pathbinding,propagation,…} legacy shims`
5. `cleanup: remove horizon/{scenarios,discovery,workscope,…} legacy shims`
6. `cleanup: switch cmd/tools/* to AH relations (post-migration)`
7. `cleanup: remove IsLegacy* predicates — data is AH-native post migrate-run`
8. `cleanup: frontend legacyRelationToAh fallback removed`
9. `cleanup: staticcheck / vet zero-warning baseline`

---

## 10. Phase P14 — `docs-actualize`

### 10.1. Goal

Reorganize `docs/` into a predictable shape, rewrite the roadmap, archive obsolete docs, update
`AGENTS.md`. Do **not** start before P13 is merged.

### 10.2. Target structure

```
docs/
  architecture/        # system-wide overviews (supply-chain, horizon, agentmesh, lens)
  taxonomy/            # SoT: taxonomy.json, migration_map.yaml, field_inventory.yaml, CODEGEN.md, materialized_payload.md, pathquery_design.md, cli_tools_inventory.yaml, REMAINING_WORK_SPEC.md (this file), migrate_run_log.md
  agents/              # AgentHorde runtimes (vertex, azure), decision agent, benchmarking
  backend/             # per-package READMEs, API references
  frontend/            # component catalog, routing, state
  operations/          # runbooks (migrate-cli, incident response, observability)
  roadmap/             # current roadmap + dated snapshots
  archive/             # everything obsolete, with a stamped redirect README
  canonical_names_and_paths/  # unchanged; referenced from AGENTS.md
```

### 10.3. Approach

**P14.01** — Inventory every `.md` file under `docs/` today (~221 files per last count). Output
  to `docs/_inventory.md` with columns: path, last-edit, referenced-from, category. Commit.
**P14.02** — Sort every file into the target structure. Moves happen via `git mv` (never
  `rm` + `add`).
**P14.03** — Rewrite `docs/roadmap/roadmap.md` (currently 3414 lines). New version:
  - Section "Completed" — condensed story of P0–P14 with one-line summaries per phase.
  - Section "Current" — open items (post-migration feature work; user provides).
  - Section "Archive" — link to `docs/archive/roadmap-<date>.md` with the full old content
    moved verbatim for provenance.
**P14.04** — Every doc that references a moved file gets its link updated. `rg -n
  '](docs/…)'` audit.
**P14.05** — Update `AGENTS.md` (project root): point at the new `docs/taxonomy/` structure;
  remove references to obsolete docs.
**P14.06** — Each top-level folder gets an `index.md` explaining what lives there.
**P14.07** — Anything in `docs/archive/*` gets a `README.md` header stating "obsolete as of
  <date>" with a pointer at the replacement doc.
**P14.08** — Final link check: run `markdown-link-check docs/**/*.md` (or a Python equivalent
  if the tool is not available in sandbox).

### 10.4. Acceptance

- Every `.md` is either under `architecture/`, `taxonomy/`, `agents/`, `backend/`, `frontend/`,
  `operations/`, `roadmap/`, `archive/`, or `canonical_names_and_paths/`. No orphans at
  `docs/*` root except one top-level `README.md` pointing into the structure.
- Link check passes.
- Roadmap rewritten; old version preserved in `docs/archive/`.
- `AGENTS.md` updated.

### 10.5. Commits

1. `docs: inventory every doc with disposition`
2. `docs: reorganize into opinionated structure (git mv only)`
3. `docs: rewrite roadmap; archive old version`
4. `docs: per-folder index.md`
5. `docs: update AGENTS.md to new structure`
6. `docs: broken-link audit + fixes`

---

## 11. Final acceptance (whole-migration oracle)

When P7–P14 are all merged, the following assertions must hold simultaneously:

1. `cd backend && go build ./... && go vet ./... && go test ./internal/...` clean (minus the
   two pre-existing failures from §1.3 which are unrelated to taxonomy).
2. `cd backend && staticcheck ./...` clean.
3. Codegen guards all clean (§2).
4. Frontend: `cd web && npm run build && npm run typecheck && npm run lint && npm test` clean.
5. Legacy-string audit from §2 returns zero unjustified hits.
6. `taxonomy_migrate --mode=validate` on the production Firestore returns zero errors.
7. No package named `criticalchain` under `backend/internal/` except `criticalchainview`.
8. No `legacy_aliases.go` / `legacy_helpers.go` anywhere in `backend/internal/*`.
9. The taxonomy migration plan in `docs/roadmap/roadmap.md` is marked **Completed** with a
   dated summary.
10. `AGENTS.md` reflects the final doc structure.

---

## 12. Operational notes for the executing agent

- Keep the todo list tight and up to date. Use `TodoWrite` with one `in_progress` todo at a
  time. Mark phases complete only after the acceptance criteria for that phase are met.
- Do not skim AGENTS.md. Re-read it at the top of each session.
- If a file is large (>800 LoC), use `Read` with `offset`/`limit` instead of loading it all.
- Prefer parallel tool calls whenever two searches/reads are independent.
- When a subagent would help (e.g., mechanical migrations of many files), use the `Task`
  tool with `subagent_type="generalPurpose"`. Provide a fully self-contained prompt (the
  subagent has no parent context).
- Do not ask the user to confirm trivial decisions. Do ask at the §1.5 escalation points.
- At the end of every phase, produce a "closeout" message in the chat summarizing: commits
  added, tests green, follow-ups, and what phase comes next.
