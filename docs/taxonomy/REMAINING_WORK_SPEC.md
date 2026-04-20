# AgentHorde Taxonomy Migration — Remaining Work Specification (P7–P14)

> **Purpose.** This is the single authoritative task list for finishing the AgentHorde (AH)
> taxonomy migration after P0–P6. It is written for a fresh coding agent that has **no prior
> context** about the migration. Execute phases strictly in the order P7 → P14 (skipping P10 and
> P11 per the strategic decision below). Do not reorder. Do not merge phases.
>
> **Strategic decision — 2026-04-19 (supersedes earlier "Variant A" and migrate-cli plans).**
> The user has decided **not to migrate legacy Firestore data**. Instead, a **new empty Firestore
> project** is being provisioned; the old project is archived read-only. As a consequence:
>
> 1. **P10 (`migrate-cli`) is SKIPPED** — no in-place rewrite CLI will be built.
> 2. **P11 (`migrate-run`) is SKIPPED** — no dry-run / apply / validate / rollback drill.
> 3. **P9 (`materialized-payload-rewrite`) is simplified** — writer-side rewrite only. Legacy
>    reader pathways can be **deleted in the same phase** because no legacy documents exist.
>    "Variant A temporary blindness" concern is moot.
> 4. **P13 (`cleanup-code`) is expanded** — every legacy-shape shim becomes dead code from day 1.
>    Since no external API clients speak legacy UPPER_SNAKE (confirmed), the wire-boundary
>    exemptions collapse: `NormalizeWireRelationStrict`, `LegacyFragmentTypeAliases`,
>    `legacyRelationAliases`, `IsLegacyDomainFragment`, `IsLegacySupplyLink`, and the 3 `cmd/tools`
>    wire-token literals all go.
> 5. **Re-ingestion of the clean graph is DEFERRED.** It is out of scope for this plan. A minimal
>    "bootstrap-clean" runbook placeholder lives in §7 as a reminder only.
> 6. **`migration_map.yaml`, `field_inventory.yaml`, and `collections_map`** become historical
>    artifacts — moved to `docs/archive/taxonomy-migration-2026/` in P14 (not deleted; they
>    document the legacy→AH vocabulary and may be needed if legacy dumps ever resurface).
>
> The phase IDs P10 and P11 are preserved as empty stubs (§6, §7) so that cross-references from
> commit history, transcripts, and inline comments continue to resolve.

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
9. `docs/taxonomy/migration_map.yaml` — legacy→AH enum dispositions. **Now an archival
   vocabulary reference** (migrate-cli was skipped per preamble); P14 moves it to
   `docs/archive/taxonomy-migration-2026/`. Still consulted during P9 and P13 when deciding
   how to rewrite stray legacy values found in code.
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
   `ah.RelationName("located_in")`. Under the new strategy these tools are **obsolete** (they
   operated on pre-migration data) — P13 either deletes them outright or rewrites their queries
   to AH relations, whichever is simpler.
3. `models/supply_link_helpers.go` — deleted in P13. No deferral.
4. `pathbuilder/legacy_helpers.go`, `domain/legacy_helpers.go`, `importance/legacy_helpers.go`,
   `scout/legacy_helpers.go`, `scout/agents/legacy_helpers.go`, `propagation/legacy_helpers.go`
   — same story; deleted in P13.
5. Test data file `focusobject/testdata/focus_legal_access_graph.json` still contains
   UPPER_SNAKE relation strings that exercise `DependencyEdge.UnmarshalJSON` legacy translation.
   With no external wire clients and no legacy data, the translation path itself is dead code.
   P13 either rewrites this fixture to AH canonical relations (preferred) or deletes it along
   with the translation logic.

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
5. A legacy enum value or payload shape encountered in code (fixtures, helpers, cmd/tools)
   that has no mapping in `migration_map.yaml`. Surface it; do not silently normalize.
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
`410 Gone` with a message stating the admin pipeline has been retired and data now flows through
the general AH ingestion path (scout pipeline → `horizon_fragments`/`dependency_edges`).
Update `cmd/server/main.go` accordingly.

**P7.05 — Delete the package**

`git rm -r backend/internal/criticalchain`. Remove imports in `cmd/server/main.go`. Verify
`go build ./... && go vet ./...` clean. `go test ./internal/criticalchainview/... ./internal/http/criticalchain/...`
green.

**P7.06 — Data side**

The `criticalchain_*` Firestore collections are **not** touched by P7 and are **not** migrated
(P10/P11 skipped per preamble). In the new Firestore project these collections simply do not
exist; `criticalchainview` projects exclusively over `horizon_fragments` + `dependency_edges`
with AH subtypes. The old collections, if they still linger in the archived read-only project,
are ignored.

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

## 5. Phase P9 — `materialized-payload-rewrite`

### 5.1. Goal

Rewrite every `MaterializedArtifact.Payload` producer so that new documents carry AH-shaped
payloads, **and** delete all legacy reader pathways in the same phase. Since the new Firestore
project starts empty (see strategic decision in the preamble), there is no legacy payload to
read; any dual-shape code is immediately dead.

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
- Payload schema version (`v: 1`). Version is not bumped; there is only one live schema.
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

**P9.04** — **Delete** every legacy reader pathway in the same phase:
- Remove any `switch edge.Type { case "SUPPLY_LINK": ... }` branch in readers.
- Remove any `node.Type == "org"|"fac"|"product"|"deposit"` check.
- Delete dead helper functions in `materialized_artifact_helpers.go` that exist solely to decode
  legacy `Type`/`Relation` string pairs.
- The `Payload map[string]interface{}` field stays as the on-disk carrier, but everything that
  decodes from it now expects AH-typed structs with no fallback.

**P9.05** — Audit `go test ./internal/...` — any test that breaks because its seed or fixture
  uses the legacy payload shape gets the seed/fixture rewritten to AH. No test retains a legacy
  payload as a "should-still-parse" case.

**P9.06** — Update `docs/taxonomy/materialized_payload.md` (new file) that documents the new
  payload shape, with examples for each Kind. This becomes the reference for P12 (frontend).

### 5.5. Acceptance

- `go build ./... && go vet ./... && go test ./internal/...` all clean.
- **Zero `Type:` fields on any materialized payload struct** (`SupplyChainPathNode`,
  `SupplyChainPathEdge`, assembly/relations-graph variants). Only `Class`/`Subtype`/`Relation`.
- Zero `"SUPPLY_LINK"`, `"LOGISTICS"`, `"ONTOLOGY"` string literals anywhere in
  `backend/internal/horizon/{models,pathbuilder,scout,supplychainreport,watchruntime,repo}/**`.
- Zero `"org"`, `"fac"`, `"product"`, `"deposit"` string literals as discriminator values in
  payload construction code (exemption: human-readable labels in logs are fine).
- Every writer produces AH-shape payload structs with typed `ah.FragmentClass` / `ah.RelationName`.
- Legacy reader helpers in `materialized_artifact_helpers.go` removed (check: grep for
  `case "SUPPLY_LINK"`, `case "org"` in reader code returns zero hits).
- `docs/taxonomy/materialized_payload.md` committed.

### 5.6. Commits

1. `models: rewrite materialized payload structs to AH class/relation types`
2. `models: tighten materialized payload validators against AH taxonomy`
3. `backend: rewrite supply-chain payload writers to AH shape`
4. `backend: rewrite assembly graph writers; transforms move to siteTransforms`
5. `backend: rewrite regulatory/governance/ecosystem payload writers to AH shape`
6. `backend: delete legacy payload reader pathways`
7. `tests: update materialized payload fixtures to AH shape`
8. `docs: materialized payload schema reference`

---

## 6. Phase P10 — `migrate-cli` — **SKIPPED**

**Status: skipped per strategic decision 2026-04-19 (see preamble).**

No `backend/cmd/taxonomy_migrate` CLI is built. No in-place rewrite of Firestore data occurs.
The new Firestore project starts empty; re-ingestion is deferred (see §7).

The phase ID `P10` is preserved so that commit messages, transcripts, and inline comments that
reference it still resolve. The original ~250-line plan (scope across 8 collections, CLI surface
with 10 flags and 4 modes, design rules, file layout, P10.01–P10.09 steps, acceptance, 10
commits) exists verbatim in git history on this file (see commit predating the strategy change)
and will be extracted into `docs/archive/taxonomy-migration-2026/migrate_cli_plan.md` during P14.

**Do not execute any P10 work. Do not reconstruct the CLI from memory.** If the clean-recreate
strategy is ever reversed, retrieve the plan from git history rather than improvising.

---

## 7. Phase P11 — `migrate-run` — **SKIPPED**

**Status: skipped per strategic decision 2026-04-19 (see preamble).**

No migration run is performed. The test and production Firestore projects are **not** rewritten.
A new empty Firestore project is provisioned separately; the old project is archived read-only.

The original P11 runbook (pre-flight export, dry-run, apply, validate, smoke tests, production
approval flow, rollback drill) is preserved in git history at commit `29db8e68^` on this file
and is not reproduced here.

### 7.1. Bootstrap-clean (DEFERRED — separate decision)

Populating the new empty project with data is **out of scope** for this plan. When that work
is scheduled (separately from P7–P14), it will likely consist of:

- provision a new GCP/Firestore project (IaC or `gcloud` snippet);
- ensure `docs/taxonomy/taxonomy.generated.ts` and `internal/taxonomy/ah/taxonomy_generated.go`
  are the SoT at the time of bootstrap;
- optional minimal seed in `backend/cmd/bootstrap_seed` (taxonomy roots, test tenant);
- re-ingestion via the existing AgentHorde scout pipeline on the current watch-subjects list;
- smoke tests: inspectors round-trip, `/api/v1/horizon/pathquery` end-to-end, `criticalchainview`
  renders, Tesla battery / Dubai CIT / Kazakhstan VAT scenarios green on the new data.

**No agent work is authorized against this section until the user explicitly re-opens it.**

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

Remove every shim, alias, helper, and fallback introduced during the migration. Because data is
being recreated clean (no legacy docs in the new Firestore project) and no external API clients
speak legacy UPPER_SNAKE (confirmed 2026-04-19), **the wire-boundary exemption list collapses
to empty**. After this phase the codebase contains zero legacy taxonomy concepts — no data
shims, no wire shims, no fixture shims.

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

**Legacy wire tokens in `cmd/tools/*`** (from P6 tech-debt note):
- `backend/cmd/tools/horizon_import_worldmonitor/main.go`
- `backend/cmd/tools/horizon_supply_repair/main.go`
- `backend/cmd/tools/horizon_smart_dedup/main.go`

These tools operated on pre-migration Firestore state. Under the clean-recreate strategy they
are **obsolete**. Prefer outright deletion; if anything inside them is still wanted for the
new project, extract the reusable logic into a generic helper and delete the legacy-wire
callers. No `ah.RelationName("located_in")`-style literals remain after this phase.

**Legacy translation layers in `models/`** — all removed:
- `backend/internal/horizon/models/graph_intent.go` — `legacyRelationAliases` map and any
  UPPER_SNAKE wire-input translation code deleted. `GraphIntent` speaks only AH snake_case.
- `backend/internal/horizon/models/fragment_class.go` — `LegacyFragmentTypeAliases`,
  `FragmentClassFromLegacyType`, `LegacyTypeForClass` and the `legacy_type` JSON alias branch
  in `UnmarshalJSON` all removed.
- `backend/internal/horizon/models/dependency_edge.go::UnmarshalJSON` — legacy decode path
  stripped; wire uses canonical `relation` field only.
- `backend/internal/horizon/models/transmission_qualifiers.go` — legacy qualifier keys
  removed; only AH canonical keys remain (confirmed: no third-party integrations feed the
  legacy keys).
- `NormalizeWireRelationStrict` / `LegacyRelationDecode` helpers deleted wherever they exist.

**`IsLegacy*` predicates on HorizonFragment** — all call sites and definitions deleted:
- `IsLegacySupplyLink` removed outright. The `__legacy_supply_link` identifier is removed
  from every `Identifiers` constructor and from `scout_*_deduplicator.go`.
- `isLegacyDomainFragment` in `internal/horizon/service.go` and everywhere else — deleted.
- Any other `IsLegacy*` or `legacy*` predicate flagged by `rg '\bIs?Legacy|legacy_' backend/
  internal/` — deleted.

**Frontend legacy translators**:
- `web/src/lib/taxonomy.ts::legacyRelationToAh` — deleted.
- Any persisted user-preference shape that mentioned UPPER_SNAKE relation strings — schema
  version bumped and loader rewritten to reject legacy input with a soft reset to defaults.

**Test fixtures**:
- `focusobject/testdata/focus_legal_access_graph.json` and any other fixture exercising the
  legacy decode path — either rewritten to AH canonical relations or deleted alongside the
  decode path it verified. Zero UPPER_SNAKE relation strings remain in any test fixture.

**Archival tracking** (not removed — moved in P14):
- `docs/taxonomy/migration_map.yaml`, `field_inventory.yaml`, and the YAML `collections_map`
  stay intact as documentation of the legacy→AH vocabulary. P14 moves them to
  `docs/archive/taxonomy-migration-2026/` with an archival `README.md`.

### 9.3. Approach

**P13.01** — Delete legacy shim files one package at a time; for each, run `go build` +
  `go test` for that package and immediate dependents.
**P13.02** — Delete legacy helpers; re-home any still-useful helpers into regular package
  files with non-legacy names.
**P13.03** — Delete/rewrite the 3 `cmd/tools/*` legacy-wire callers (preferred: outright
  deletion; only keep if there's a clear use against the new clean project).
**P13.04** — Strip legacy translation layers in `models/` (`graph_intent.go`,
  `fragment_class.go`, `dependency_edge.go::UnmarshalJSON`, `transmission_qualifiers.go`,
  `NormalizeWireRelationStrict`, `LegacyRelationDecode`).
**P13.05** — Delete every `IsLegacy*` predicate and its call sites (`IsLegacySupplyLink`,
  `isLegacyDomainFragment`, `__legacy_supply_link` identifier everywhere).
**P13.06** — Frontend: delete `legacyRelationToAh` in `web/src/lib/taxonomy.ts`; bump any
  persisted user-preference schema version to refuse legacy input.
**P13.07** — Test fixtures: rewrite or delete every fixture carrying UPPER_SNAKE relation
  strings.
**P13.08** — Run the legacy-string audit from §2; **zero unjustified hits** (no "wire boundary"
  exemption comments survive — the exemption list is empty).
**P13.09** — Run `staticcheck ./...` (install via `go install honnef.co/go/tools/cmd/staticcheck@latest`);
  zero unused symbols. `go vet ./...` clean.
**P13.10** — Delete obsolete TODO comments about "remove after migration".

### 9.4. Acceptance

- `go build ./... && go vet ./... && go test ./internal/...` clean.
- `staticcheck ./...` clean.
- Legacy-string audit from §2 returns **zero** hits. No exemptions.
- No `legacy_aliases.go` or `legacy_helpers.go` files anywhere in `backend/internal/*`.
- No `Legacy*`, `legacy*`, or `__legacy_*` identifiers in backend Go code.
- Frontend: `npm run build && npm run typecheck && npm run lint && npm test` clean; no
  `legacyRelationToAh` in `web/src/`.
- `rg -n 'UPPER_SNAKE_CASE_EXAMPLE|TRANSFORMS_INTO|ASSEMBLES_INTO|SUPPLY_LINK|DEPOSIT|PROGRAM_IMPLEMENTATION'
  backend/ web/ --type go --type ts` returns matches only in archived docs under
  `docs/archive/`.

### 9.5. Commits

Group by package/surface. Minimum ~10–14 commits:

1. `cleanup: remove http/horizon legacy aliases`
2. `cleanup: remove horizon/models/supply_link_helpers + __legacy_supply_link identifier`
3. `cleanup: remove horizon/{scout,scout/agents,scout/pipeline,…} legacy shims`
4. `cleanup: remove horizon/{pathbuilder,pathbinding,propagation,…} legacy shims`
5. `cleanup: remove horizon/{scenarios,discovery,workscope,…} legacy shims`
6. `cleanup: remove legacy translation layer from models/graph_intent.go`
7. `cleanup: remove LegacyFragmentTypeAliases + FragmentClassFromLegacyType + UnmarshalJSON legacy branch`
8. `cleanup: remove dependency_edge UnmarshalJSON legacy decode + transmission qualifier legacy keys`
9. `cleanup: delete or rewrite cmd/tools/{horizon_import_worldmonitor,horizon_supply_repair,horizon_smart_dedup}`
10. `cleanup: remove IsLegacy* predicates + isLegacyDomainFragment`
11. `cleanup: frontend legacyRelationToAh + persisted-prefs schema bump`
12. `cleanup: rewrite or delete UPPER_SNAKE test fixtures`
13. `cleanup: staticcheck / vet zero-warning baseline`

---

## 10. Phase P14 — `docs-actualize`

### 10.1. Goal

Reorganize `docs/` into a predictable shape, rewrite the roadmap, archive obsolete docs, update
`AGENTS.md`. Do **not** start before P13 is merged.

### 10.2. Target structure

```
docs/
  architecture/        # system-wide overviews (supply-chain, horizon, agentmesh, lens)
  taxonomy/            # LIVE SoT: taxonomy.json, CODEGEN.md, materialized_payload.md, pathquery_design.md, cli_tools_inventory.yaml, REMAINING_WORK_SPEC.md (this file)
  agents/              # AgentHorde runtimes (vertex, azure), decision agent, benchmarking
  backend/             # per-package READMEs, API references
  frontend/            # component catalog, routing, state
  operations/          # runbooks (incident response, observability, bootstrap-clean when scheduled)
  roadmap/             # current roadmap + dated snapshots
  archive/
    taxonomy-migration-2026/  # migration_map.yaml, field_inventory.yaml, criticalchain_fold_inventory.md, former §6/§7 of this spec (migrate-cli + migrate-run plans), and README explaining these are historical
    roadmap-<date>.md          # pre-rewrite roadmap
    ...                         # other obsolete docs
  canonical_names_and_paths/  # unchanged; referenced from AGENTS.md
```

### 10.3. Approach

**P14.01** — Inventory every `.md` file under `docs/` today (~221 files per last count). Output
  to `docs/_inventory.md` with columns: path, last-edit, referenced-from, category. Commit.
**P14.02** — Sort every file into the target structure. Moves happen via `git mv` (never
  `rm` + `add`). Explicit moves to `docs/archive/taxonomy-migration-2026/`:
  - `docs/taxonomy/migration_map.yaml`
  - `docs/taxonomy/field_inventory.yaml`
  - `docs/taxonomy/criticalchain_fold_inventory.md`
  - The former §6 and §7 of this spec (migrate-cli + migrate-run plans) extracted from git
    history and saved as `migrate_cli_plan.md` and `migrate_run_plan.md` for posterity.
  - Add a `README.md` in that folder explaining: "These documents describe the in-place
    migration plan that was SKIPPED on 2026-04-19 in favor of recreating data clean in a new
    Firestore project. They are preserved for vocabulary reference only."
**P14.03** — Rewrite `docs/roadmap/roadmap.md` (currently 3414 lines). New version:
  - Section "Completed" — condensed story of P0–P14 with one-line summaries per phase.
    Explicitly note P10 and P11 as SKIPPED with strategy rationale.
  - Section "Current" — open items (post-cleanup feature work, bootstrap-clean when scheduled;
    user provides content).
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

## 11. Final acceptance (whole-effort oracle)

When P7, P8, P9, P12, P13, P14 are all merged (P10 and P11 are SKIPPED per preamble), the
following assertions must hold simultaneously:

1. `cd backend && go build ./... && go vet ./... && go test ./internal/...` clean (minus the
   two pre-existing failures from §1.3 which are unrelated to taxonomy).
2. `cd backend && staticcheck ./...` clean — zero unused symbols.
3. Codegen guards all clean (§2).
4. Frontend: `cd web && npm run build && npm run typecheck && npm run lint && npm test` clean.
5. Legacy-string audit from §2 returns **zero** hits in live code (hits inside
   `docs/archive/**` are expected and allowed).
6. No package named `criticalchain` under `backend/internal/` except `criticalchainview`.
7. No `legacy_aliases.go`, `legacy_helpers.go`, `Legacy*` constants, `__legacy_*` identifiers,
   or `IsLegacy*` predicates anywhere in `backend/internal/*` or `backend/cmd/*`.
8. No `legacyRelationToAh` or equivalent translator in `web/src/`.
9. `migration_map.yaml`, `field_inventory.yaml`, and the former migrate-cli / migrate-run
   plans live under `docs/archive/taxonomy-migration-2026/` with an explanatory README.
10. The taxonomy work in `docs/roadmap/roadmap.md` is marked **Completed** with a dated
    summary, explicitly noting P10/P11 as SKIPPED.
11. `AGENTS.md` reflects the final doc structure and no longer references obsolete migration
    tooling.

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
