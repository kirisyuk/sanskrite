# P15 — schema-indexes-align — CLOSEOUT

**Status:** DONE (code + index config). Deploy to Firestore projects is a separate operational step (see below).

## Objective

Align the Firestore read-path and composite-index configuration with the AH schema that P1–P14 landed in code. This unblocks `bootstrap-clean` by ensuring: (a) repo-level edge queries hit the real `relation` field, not a ghost `type` field; (b) composite indexes exist for every live query pattern on `horizon_fragments`, `horizon_dependency_edges`, `horizon_path_cache`.

## What changed

### Code (commit 1)

`backend/internal/horizon/repo/dependency_edges.go` — six call-sites in `GetBySourceTargetAndType` / `ListBySourceTargetAndType` / `ListBySource` / `ListByTarget` / `ListBySourceIDsAndType` / `ListByTargetIDsAndType`: `.Where("type", …)` → `.Where("relation", …)` with `string(edgeType)` / `string(*edgeType)` for Firestore equality values.

Out-of-scope `type`-queries remain untouched (valid domain fields): `internal/repo/ontology.go`, `internal/horizon/repo/exposure_paths.go`, `internal/horizon/repo/lens_artifacts.go`, `cmd/tools/horizon_deduplicate_fragments/main.go`.

### Index config (commit 2)

**Removed (8 composite indexes)**

| # | collectionGroup | fields (order / config) |
|---|-------------------|-------------------------|
| 1 | `horizon_fragments` | `tenantId` ASC + `type` ASC |
| 2 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `type` ASC |
| 3 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `type` ASC + `properties.criticality` DESC |
| 4 | `horizon_dependency_edges` | `tenantId` ASC + `targetFragmentId` ASC + `type` ASC |
| 5 | `horizon_dependency_edges` | `tenantId` ASC + `targetFragmentId` ASC + `type` ASC + `properties.criticality` DESC |
| 6 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `targetFragmentId` ASC + `type` ASC |
| 7 | `criticalchain_nodes` | `kind` ASC (single-field composite in `indexes`) |
| 8 | `criticalchain_edges` | `kind` ASC (single-field composite in `indexes`) |

**Added (9 composite indexes)**

| # | collectionGroup | fields |
|---|-------------------|--------|
| 1 | `horizon_fragments` | `tenantId` ASC + `class` ASC |
| 2 | `horizon_fragments` | `class` ASC + `subtype` ASC |
| 3 | `horizon_fragments` | `tenantId` ASC + `class` ASC + `subtype` ASC |
| 4 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `relation` ASC |
| 5 | `horizon_dependency_edges` | `tenantId` ASC + `targetFragmentId` ASC + `relation` ASC |
| 6 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `targetFragmentId` ASC + `relation` ASC |
| 7 | `horizon_dependency_edges` | `tenantId` ASC + `sourceFragmentId` ASC + `relation` ASC + `properties.criticality` DESC |
| 8 | `horizon_dependency_edges` | `tenantId` ASC + `targetFragmentId` ASC + `relation` ASC + `properties.criticality` DESC |
| 9 | `horizon_path_cache` | `tenant` ASC + `nodeRefs` ARRAY_CONTAINS |

**Net change:** 218 → 219 entries in the `indexes` array (+1 composite). Rationale for index 9: `backend/internal/horizon/pathquery/cache_backend.go` uses `Where("tenant", "==", tenantID).Where("nodeRefs", "array-contains", cp)` on `horizon_path_cache`.

## How to deploy

**Test / staging:**

```bash
cd web
firebase use <project-id>
firebase deploy --only firestore:indexes
```

Firebase CLI applies additions/removals declaratively from `firestore.indexes.json`.

**Production:** same command against the production Firebase project after staging validation.

For the `horizon_path_cache` index (array-contains): Firebase CLI supports this natively; no special `gcloud` incantation is required. (The `gcloud`-only path in `docs/lessons-learned/firestore-vector-search.md` applies to vector field overrides, which are not used here.)

## Verification logs (2026-04-21)

```text
$ cd backend && go build ./... && go vet ./... && go test ./internal/horizon/repo/... ./internal/horizon/... -count=1
ok  	github.com/igorkirisyuk/sanskrite/internal/horizon/repo	191.242s
ok  	github.com/igorkirisyuk/sanskrite/internal/horizon	1.490s
(... additional internal/horizon/* packages ok ...)
exit code: 0

$ cd backend && go build ./... && go vet ./... && go test ./internal/horizon/repo/... -count=1
ok  	github.com/igorkirisyuk/sanskrite/internal/horizon/repo	190.982s
exit code: 0

$ git grep -n 'Where("type"' backend/internal/horizon/repo/dependency_edges.go
# (no output — expected)

$ git grep -n 'Where("type"' backend/internal/horizon/repo/fragments.go
# (no output — expected)

$ python3 -m json.tool web/firestore.indexes.json >/dev/null && echo OK
OK

$ python3 -c "import json; d=json.load(open('web/firestore.indexes.json')); print('indexes:', len(d['indexes']))"
indexes: 219
```

**Strict global duplicate check** (collectionGroup + ordered field tuple) still reports one **pre-existing** duplicate pair on `ontology_claims` (`tenantId` ASC, `createdAt` DESC) — present already at 218 total indexes (217 unique). P15 did not remove or merge that pair; do not treat a failing assert on the whole file as a P15 regression until that duplicate is cleaned up separately.

## Known gaps / follow-ups

- **`ontology_claims` duplicate composite** — two identical `(tenantId, createdAt DESC)` definitions in `web/firestore.indexes.json` predate P15; global uniqueness assert over all `indexes` fails until one entry is dropped.
- **`horizon_path_cache` purge/GC job** — not in this phase; if added later, a composite such as `(tenant ASC, expiresAt ASC)` may be needed — track separately.
- **Index deploy** to test/prod Firestore projects is operational; **not** part of this commit set. Owner: (TBD).
- **Data re-ingestion** via scout pipeline remains a separate deferred todo.

This closes the last schema-level blocker for `bootstrap-clean` on the code and config side.
