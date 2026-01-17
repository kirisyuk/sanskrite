## Canonicalization & Deduplication (Horizon)

### Goals

- **Stable identity**: every entity is addressed by a stable `canonicalPath` (not by label).
- **Early normalization**: normalize/dedup as close to ingestion as possible to prevent duplicate fragments/jobs/edges.
- **Type-aware strictness**: some namespaces are intentionally stricter (notably GEO).
- **Rule-first, deterministic-by-default**: policies can be configured from DB; LLM is a fallback only where explicitly used.

### Shared primitives (single source of truth)

- **Slugging**: `taxonomy.Slugify()` is the shared slug primitive (includes best-effort Cyrillic transliteration).
- **Path validation**: `taxonomy.ValidateCanonicalPath()` enforces:
  - known namespaces
  - slug-only segments (`[a-z0-9-]`)
  - namespace-specific shapes (e.g. `/gov/{iso2|na}/{slug}`, strict `/loc/{cc}/{admin1}/{city}`, etc.)

### Policies

#### CanonizationPolicy (`kind=canonization`)

Normalizes:
- `canonical_path` (lowercasing, legacy geo mapping, market tightening, etc.)
- `fragment_type` / `entity_type`
- `relation_type`, `event_type`
- `slug`

Order:
- Firestore rules (exact/pattern) first
- hardcoded fallback rules second

#### ValidationPolicy (`kind=validation`)

Validates:
- **fragments** (e.g. forbid `/discovered/*`, enforce `/loc` strictness; prefix mismatch is usually penalty)
- **edges** (combination rules, direction reversal, penalties)

Important: in most runtime flows, **`reject` is advisory** (logged/penalized) and does not block ingestion, except where hard rules already block (e.g. `/discovered/*` in precreate).

### Canonicalization services

#### CanonicalPathResolver (unified path normalization + dedup)

File: `backend/internal/horizon/scout/canonical_path_resolver.go`

`Resolve(ctx, inputPath, fragmentType)` applies:
- CanonizationPolicy rewrite (`inputType=canonical_path`)
- GEO strictification:
  - `/loc/{slug}` → `/loc/{cc}/na/na` when safely inferable
  - optional GeoNames-backed upgrades/fixes
- PRODUCT upgrade:
  - `/product/{slug}` → hierarchical taxonomy when classification is possible
- Dedup:
  - direct fragment lookup by `canonicalPath`
  - ORG dedup by identifiers (Wikidata/legalName/shortName/...)

#### ScoutEngine canonicalization

File: `backend/internal/horizon/scout/canonicalizer.go`

When converting LLM-discovered relations into fragments:
- prefer provided stable paths (non-`/discovered/*`)
- prefer existing fragments matching **preferred candidates**
- fall back to Vertex AI Search
- fall back to LLM canonicalization (strict JSON schema) as last resort

#### Lens scope canonicalization (ingress)

File: `backend/internal/horizon/lens/service.go`

When building lenses, scope paths are canonicalized early via `CanonicalPathResolver.Resolve()`, and alias→canonical rewrites are tracked to keep UI/graph stable.

#### Intent resolution canonicalization (ingress)

File: `backend/internal/horizon/scout/agents/intent_resolver_agent.go`

LLM-produced anchors are normalized by:
- segment slugging (`taxonomy.Slugify`)
- legacy geo normalization (`/region/...` → `/loc/...` when supported)
- final `taxonomy.ValidateCanonicalPath` gating (invalid anchors dropped)

#### Product IO materialization (ingress)

File: `backend/internal/horizon/materialize/product_io.go`

Product IO uses Scout’s canonical-path candidate generation and (optionally) `CanonicalPathResolver` to upgrade/normalize product paths before fragment creation.

### Deduplication components

- **OrgDeduplicator**: `backend/internal/horizon/scout/org_deduplicator.go`
  - Tiered strategy: alias-path resolution → country-as-org normalization → Wikidata → legalName → label/shortName → normalized legalName
  - `/gov/*` is treated conservatively to avoid cross-jurisdiction merges; guarded gov→org bridge exists.
- **PersonDeduplicator**: `backend/internal/horizon/scout/person_deduplicator.go`
  - Wikidata → exact name fields → phonetic key → token-subset match

### Edge canonicalization (semantic cleanup)

File: `backend/internal/horizon/models/edge_canonicalization.go`

Conservatively rewrites invalid/odd edge types/directions (e.g. market “owns” market → `HAS_SUBMARKET`, etc.).

### Observability

- **Policy telemetry**: `PolicyExecutor` records decisions (rule id/path, confidence, latency).
- **Lightweight counters**:
  - `CanonicalPathResolver.Stats()`
  - `OrgDeduplicator.Stats()`
  - `PersonDeduplicator.Stats()`

These are intentionally in-memory and best-effort; they can be periodically logged/scraped by higher-level services if needed.

