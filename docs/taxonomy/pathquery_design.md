# Pathquery layer — design document (P8)

**Status:** draft for approval  
**Spec:** `docs/taxonomy/REMAINING_WORK_SPEC.md` §4 (Phase P8)  
**Escalation:** per §1.5 — **no implementation in `backend/internal/horizon/pathquery` until this document is explicitly approved** in chat.

---

## 1. Goal

Centralize read-side “find supply-chain paths between A and B” behind a single `pathquery.Service`, with optional memoization in Firestore collection **`horizon_path_cache`**. Consumers (HTTP handlers, future frontend inspectors) stop duplicating BFS / materialized lookups / pathbuilder triggers.

**Non-goals for P8:**

- Changing existing HTTP response shapes for `supply_chains` / `fragment_trace` (P12).
- Rewriting materialized payload schemas (P9).

---

## 2. Inventory (P8.01) — ad-hoc path logic to fold in later

Candidates called out in the spec (to be wired in P8.06 after the service exists):

| Area | Files (indicative) |
|------|-------------------|
| HTTP | `backend/internal/http/horizon/supply_chains.go`, `fragment_trace.go`, `identify_stresses.go`, `lens_report_artifact.go` |
| Report | `backend/internal/horizon/supplychainreport/*` |
| Supply | `backend/internal/horizon/supplychain/one_hop_downstream.go` |

Exact call sites will be confirmed during P8.06; this design does not depend on a particular file list.

---

## 3. Public API — Go types

Package: `backend/internal/horizon/pathquery`

### 3.1. Endpoint

Endpoints are resolved to internal fragment IDs + canonical paths before graph expansion (reuse existing repo helpers where possible).

```go
// EndpointKind distinguishes how From/To are interpreted before resolution.
type EndpointKind int

const (
	EndpointCanonicalPath EndpointKind = iota // value is a canonical path string, e.g. /product/...
	EndpointFragmentID                          // value is horizon_fragments document id
)

// Endpoint is one end of a path query (From or To).
// Zero value is invalid; callers must set Kind and Value.
type Endpoint struct {
	Kind  EndpointKind
	Value string // canonical path OR fragment id per Kind
}
```

**To optional:** `Query.To` is a pointer `*Endpoint`. `nil` means “any downstream” (multi-target BFS / bounded exploration from `From`); result may contain multiple `MaterializedArtifact` paths. Implementations must document max result cardinality (e.g. cap paths returned) to avoid unbounded responses.

### 3.2. SearchContext

Carries everything that must participate in cache keying and runtime filtering but is **not** stored verbatim in the cache document’s debug `query` blob if it contains secrets (keep `query` subset safe for logs).

```go
type SearchContext struct {
	LensID    string    // optional; empty = global / no lens filter
	Scope     string    // optional opaque scope key (e.g. serialized filter fingerprint)
	AsOf      time.Time // optional; zero = “now” / latest
	ProjectID string    // duplicate of Query.Project if needed for nested calls; may stay empty
}
```

**Cache key:** includes `LensID`, `Scope` (normalized), and **as-of date** (see §5.1). If `AsOf` is zero, the key uses a sentinel `"live"` or RFC3339 of truncated “today” UTC — **decision for approver:** prefer **date-only UTC** (`2006-01-02`) for stability within a day, or full timestamp if sub-day cache correctness is required.

### 3.3. Query

Aligned with REMAINING_WORK_SPEC §4.3; `Context` added as above.

```go
type Query struct {
	Tenant        string
	Project       string
	From          Endpoint
	To            *Endpoint              // nil = any downstream
	ProductAxis   string               // optional canonical product path filter
	MaxHops       int                  // default 8 when zero
	RelationKinds []ah.RelationName    // default {supplies, produces, leg_connects} when nil/empty
	IncludeStress bool
	IncludeGeo    bool
	CacheTTL      time.Duration        // default 24h; 0 = disable cache read/write; -1 = force refresh (miss + overwrite)
	Context       SearchContext
}
```

**Defaults (implementation contract):**

- `MaxHops`: `8` if `<= 0`.
- `RelationKinds`: `[]ah.RelationName{ah.RelationNameSupplies, ah.RelationNameProduces, ah.RelationNameLegConnects}` if nil/empty.
- `CacheTTL`: `24 * time.Hour` if zero and caching is enabled; `0` disables cache; `-1` forces refresh.

### 3.4. Diagnostics

Structured, JSON-serializable; stored on cache documents and returned on hot paths (truncated if huge).

```go
type Diagnostics struct {
	MaterializedHits      int  // paths taken directly from existing SUPPLY_CHAIN_PATH artifacts
	BFSExpandedNodes      int  // nodes visited in ad-hoc expansion
	PathbuilderRuns       int  // times pathbuilder was invoked
	ExposurePathFamilies  int  // hits from repo exposure_paths-style projections, if used
	TruncatedMaxHops      bool // true if search stopped due to MaxHops
	TruncatedResultCap    bool // true if number of paths capped
	ProductAxisFiltered   bool
	RelationFilterApplied bool
	Warnings              []string // non-fatal, short messages
}
```

### 3.5. Result

```go
type Result struct {
	Paths        []*models.MaterializedArtifact // each Kind == SUPPLY_CHAIN_PATH; may be empty slice, never nil
	FromCache    bool
	GeneratedAt  time.Time
	CacheKey     string       // pq_<hex> per §5.1
	Diagnostics  Diagnostics
}
```

**Paths:** materialized artifacts are the **stable interchange** type already used downstream; HTTP will project payload to `SupplyChainPathPayload` wire shape (P8.05).

### 3.6. Service

```go
type Service interface {
	QueryPaths(ctx context.Context, q Query) (*Result, error)
	InvalidatePathCache(ctx context.Context, cacheKey string) error
	InvalidateByNode(ctx context.Context, canonicalPath string) error // cascade via nodeRefs (see §6)
}
```

**Invalidation contract:**

- `InvalidatePathCache`: delete or tombstone the single doc with `cacheKey` (tenant-scoped query in §6).
- `InvalidateByNode`: find all cache docs for the same tenant (and project scope if indexed) where `nodeRefs` **array-contains** `canonicalPath`, then delete/tombstone each.

---

## 4. Unified read pipeline (conceptual)

On `QueryPaths`:

1. **Normalize** query (defaults, resolve `Endpoint` → fragment ids + canonical paths).
2. **Cache lookup** (unless `CacheTTL == 0` or `-1`): compute `CacheKey`; load `horizon_path_cache` doc; if hit and `expiresAt > now`, hydrate `pathIds` → `MaterializedArtifact` via existing repo; return `FromCache: true`.
3. **Miss path** (ordered merge; exact order is implementation detail, but diagnostics must reflect it):
   - **(1)** Direct materialized paths (`MaterializedKind=SUPPLY_CHAIN_PATH`) already matching endpoints / product axis (existing repo queries).
   - **(2)** Ad-hoc BFS over `dependency_edges` restricted to `RelationKinds`, `MaxHops`, optional `ProductAxis` filter on nodes.
   - **(3)** Pathbuilder invocation when a candidate path needs materialization (increment `PathbuilderRuns`).
   - **(4)** Path-family projections (`repo/exposure_paths.go` family) when applicable.
4. **Write cache** (if TTL > 0 and not disabled): store doc (§5.2); set `nodeRefs` to the **union** of canonical paths touched on every path result (for invalidation).
5. Return `Result` with populated `Diagnostics`.

**Stress / geo:** `IncludeStress` / `IncludeGeo` flag existing enrichers (pathstress, pathbinding) **without** changing cache key unless we decide they affect artifact identity — **recommendation:** they run **after** cache hydrate and **do not** enter `CacheKey` if they only add derived fields; if they change stored payload, they **must** enter the key. **Approver call.**

---

## 5. Cache key and Firestore document

### 5.1. CacheKey

- **Input tuple** (normalized, stable ordering):  
  `(tenant, project, fromKind, fromValue, toPresent, toKind, toValue, productAxis, maxHops, relationKinds sorted lexicographically, lensId, scope, asOfKey, includeStress, includeGeo)`  
  where `toPresent` is bool, `asOfKey` is date or timestamp per §3.2 decision.
- **Serialization:** canonical JSON (sorted keys) or explicit delimiter-separated string; then **SHA-256**, hex-encode, prefix **`pq_`**.  
  Example: `pq_a1b2c3...` (64 hex chars after prefix).

### 5.2. Collection `horizon_path_cache`

Document id: **use `cacheKey` as document ID** (Firestore id = `pq_...`) to make get/put O(1) and avoid duplicate queries per key. Alternative: random id + field `cacheKey` + unique index — **recommendation:** **document ID = cacheKey** for simplicity.

| Field | Type | Notes |
|-------|------|--------|
| `cacheKey` | string | duplicate of id for readability in exports |
| `tenant` | string | required |
| `project` | string | required |
| `query` | map | **subset** of normalized Query: no secrets; omit volatile fields if any |
| `pathIds` | []string | materialized_artifacts ids |
| `diagnostics` | map | `Diagnostics` as map |
| `generatedAt` | timestamp | server time when computed |
| `expiresAt` | timestamp | `generatedAt + TTL` |
| `nodeRefs` | []string | canonical paths appearing in result paths; used for `InvalidateByNode` |

**TTL / cleanup:** optional Cloud Scheduler or lazy delete on read if `expiresAt < now`. Not required for correctness of first implementation.

### 5.3. Indexes

- Query by `tenant` + `nodeRefs` **array-contains** `canonicalPath` for invalidation (may need composite index with `tenant` + `nodeRefs`).
- If invalidation is rare in v1, acceptable fallback: batch scan with `tenant ==` and filter client-side for small caches — **not** acceptable at scale; prefer index as in spec §4.3.

---

## 6. Security / multi-tenant

- Every read/write **filters by `Query.Tenant`** (and `Project` if artifacts are project-scoped in your deployment).
- Cache documents are **never shared across tenants**; `CacheKey` includes tenant.
- HTTP handler (P8.05) must enforce authz consistent with existing Horizon APIs.

---

## 7. HTTP surface (P8.05) — preview

`GET /api/v1/horizon/pathquery?from=...&to=...&productAxis=...&maxHops=...&relations=...&cacheTtl=...&asOf=...`

- `from` / `to`: canonical path or `id:` prefix for fragment id — exact encoding **TBD in implementation** (document in README when coding).
- `relations`: comma-separated `ah.RelationName` values.
- Response: JSON mirroring `Result`, with `paths` projected to existing **`SupplyChainPathPayload`** wire shape for each element.

---

## 8. Observability (P8.08)

Counters in `metriccore` (names from spec):

- `pathquery_cache_hits_total`
- `pathquery_cache_misses_total`
- `pathquery_builder_invocations_total` (increment when pathbuilder runs; align with `Diagnostics.PathbuilderRuns`).

---

## 9. Open points for explicit approval

1. **`AsOf` in cache key:** date-only UTC vs full timestamp (§3.2).
2. **`IncludeStress` / `IncludeGeo`:** exclude from cache key (post-process only) vs include (§4).
3. **`To == nil`:** max number of paths / max BFS frontier to avoid abuse (suggested default cap, e.g. 32 paths, 10k node visits — **numbers for approver**).
4. **Document ID strategy:** Firestore doc id = `cacheKey` vs random id (§5.2).

---

## 10. Approval

Reply in the chat with **Approved** (and any deltas to §9), or requested changes. After approval, implementation follows commits in REMAINING_WORK_SPEC §4.6.
