# CriticalChain fold inventory (Phase P7)

Authoritative spec: `docs/taxonomy/REMAINING_WORK_SPEC.md` §3.

## Symbols / helpers the general graph needed for the fold

| Need | Status |
|------|--------|
| Query fragments by `class` / `class`+`subtype` (tenant `global`) | **Present** — `HorizonFragmentsRepository.ListByTypeAndTenant`, `ListByClassSubtypeAndTenant`, `ListByLegacyTypeAndTenant` |
| Query dependency edges by source/target + relation | **Present** — `DependencyEdgesRepository.ListBySource`, `ListByTarget`, typed `ah.RelationName` |
| `site_transforms` on site nodes | **Added P7** — `HorizonFragment.SiteTransforms` / `SetSiteTransforms` / `AppendSiteTransform` in `internal/horizon/models/site_transforms.go` |
| Embedded critical-material YAML catalog (demo names / stages / chokepoints) | **Moved** — `internal/criticalchainview/config` (same `go:embed materials/*.yaml` layout as legacy `criticalchain/config`) |
| Signal fusion helper | **Moved** — `internal/criticalchainview/signal/fusion.go` |

No new taxonomy.py relations or migration_map fragment entries were required for P7.

## Firestore collections (`criticalchain_*`)

| Collection | Migrated in P7? | P10 target |
|------------|------------------|------------|
| `criticalchain_nodes` | **No** (read path removed; data untouched) | `horizon_fragments` (products + sites) per §3.3 |
| `criticalchain_edges` | **No** | `dependency_edges` (`supplies`, `produces`, `leg_connects`, …) |
| `criticalchain_conflict_zones` | **No** | `horizon_fragments` (`geo` / `affected_zone`) + `disrupts` edges |
| `criticalchain_sanctions` | **No** | `horizon_fragments` (`evidence` / `sanction`) + issuer-specific relations |

`collections_map` for these four names is committed in `docs/taxonomy/migration_map.yaml` for `cmd/taxonomy_migrate` (Phase P10).

## Frontend call sites (`web/src`)

Paths that call `/api/v1/criticalchain/*` (wire shape unchanged):

- `web/src/lib/criticalchain-api.ts`
- `web/src/pages/CriticalChainPage.tsx`
- `web/src/components/CriticalChainGraph.tsx`
- `web/src/components/CriticalChainMap.tsx`
- `web/src/components/TopBar.tsx`
- `web/src/components/SignalFusionPanel.tsx`
- `web/src/components/MonitoringBoard.tsx`
- `web/src/components/App.tsx`

## Backend packages removed

- `backend/internal/criticalchain/**` (adapters, datasource, pipeline, repo, scoring — ingestion obsolete).

## Backend packages added

- `backend/internal/criticalchainview/**` — read-only projection over `horizon_fragments` + `dependency_edges`.
- `backend/internal/http/criticalchain` — same routes; `POST|GET /api/v1/criticalchain/admin/pipeline/run` → **410 Gone** with pointer to `cmd/taxonomy_migrate`.

## Follow-ups (later phases)

- **P10** — implement `criticalchain_migrator.go` using `collections_map` + §3.3 mapping table.
- **P12** — optional: route CriticalChain UI pathfinding through `/api/v1/horizon/pathquery` when P8 lands.
