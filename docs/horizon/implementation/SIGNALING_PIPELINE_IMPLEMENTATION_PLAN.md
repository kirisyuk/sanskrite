# Signaling Pipeline — Implementation Plan

**Status:** Planning  
**Date:** 2026-02-21  
**Context:** Scout no longer creates signals. Signaling is a separate pipeline fed by news flow. WorldMonitor/Discovery infrastructure provides throughput collection.

---

## 1. Architecture Overview

```
[DataSources: rss, horizon_monitor_feed]  ← existing, system schedules polls
         ↓
[Orchestrator: IngestRSS, IngestHorizonMonitorFeed]
         ↓
[Document/Event stream]
         ↓
[Signaling Adapter]  ← NEW: document/event → OntologyFact(kind=event)
         ↓
[Incident Deduplication]  ← NEW: one incident = one event
         ↓
[Signal Extraction]  ← existing SignalExtractor (rules + LLM)
         ↓
[Entity Resolution]  ← NEW: Vertex AI Search + GCS for fuzzy entity matching
         ↓
[Seed Resolution]  ← geo + actors + spheres → fragment IDs
         ↓
[Propagation]  ← existing PropagateSignalFromSeeds
```

---

## 2. System Sources (Work Unless Explicitly Disabled)

**Current behavior:** Sources are polled only when they have at least one active project link (`hasActiveProjects`). No link → no poll.

**Problem:** Baseline/system sources (sanctions, conflict, general news) are infrastructure for Signaling Pipeline. They shouldn't require every project to link them.

**New rule for system sources:**
- **System source** = polled by default, works unless explicitly disabled (`status = "paused"`).
- **Project source** = polled only when linked to at least one active project (current behavior).

**How to mark system source:**
- Add `IsSystemSource bool` to `SourceDetails` (or `Scope: "system" | "project"`).
- Or: `TenantID == ""` or `"global"` → system.
- Or: `Kind == "horizon_monitor_feed"` → system by default (configurable).

**Poller logic:**
```go
if ds.IsSystemSource {
    // Poll unless status=paused or auto-paused (ErrorCount >= 10)
    if ds.Status == "paused" || ds.ErrorCount >= 10 { skip }
} else {
    // Project source: require active project links
    if !hasActiveProjects(ctx, ds.TenantID, ds.ID) { skip }
}
```

**Implementation:** Add `IsSystemSource` (or `Scope`) to `SourceDetails`; extend `poller_worker.PublishDue` and `hasActiveProjects` check. In `poller_worker.hasActiveProjects` — for system sources return `true` without checking project links.

---

## 3. Configuration (Code)

**Concentration points** — hardcoded in code, not Firestore:

```go
// signaling/concentration.go
var defaultConcentrationFeeds = []ConcentrationFeed{
    {Topic: "sanctions", Kind: "rss", DefaultCadence: 30 * time.Minute},
    {Topic: "conflict", Kind: "horizon_monitor_feed", DefaultCadence: 15 * time.Minute},
    {Topic: "supply_chain", Kind: "rss", DefaultCadence: 60 * time.Minute},
    // ...
}
```

Sources are created/linked via existing DataSource model. Concentration config maps topic → default cadence. Per-source override via `Details.PollIntervalMinutes` (already exists).

---

## 4. Tier-Based Cadence

**Source tier** — add to DataSource or SourceProfile:

| Tier | Cadence (default) | Use case |
|------|-------------------|----------|
| `intel` | 15 min | Reuters, Bloomberg, specialized intel |
| `standard` | 30 min | horizon_monitor_feed, curated RSS |
| `broad` | 60 min | Aggregators, regional feeds |

**Implementation:**
- Add `SourceTier` to `SourceDetails` (optional): `"intel" | "standard" | "broad"`
- Extend `cadenceToDuration` in `poller_worker.go` and `orchestrator.go`:
  - If `SourceTier == "intel"` and no override → 15 min
  - If `SourceTier == "broad"` and no override → 60 min
  - Else → 30 min (current default)
- `PollIntervalMinutes` continues to override when set

---

## 5. Incident Deduplication (One Incident = One)

**Goal:** Reuters and Bloomberg report same sanction → one `OntologyFact(kind=event)`, not two.

### 5.1 Deduplication Strategy

**Fingerprint** = stable identifier for an incident, computed before event creation:

```
fingerprint = hash(
  canonical_url OR (normalized_title + primary_geo + date_bucket_6h + primary_entity)
)
```

- **Primary key:** If document has URL → canonicalize URL (strip tracking params, lowercase domain). Same URL = same incident.
- **Fallback:** No URL or URL not reliable → content-based fingerprint:
  - `normalized_title`: lowercase, trim, remove extra spaces
  - `primary_geo`: e.g. `/loc/ir/na/na` or country code
  - `date_bucket_6h`: `occurredAt.Truncate(6h)` so events within 6h window can merge
  - `primary_entity`: first extracted org name or "" (optional, for disambiguation)

### 5.2 Deduplication Store

**Option A: Firestore collection `incident_fingerprints`**

```
Collection: incident_fingerprints
Document ID: fingerprint (hash)
Fields:
  - incidentId: string (ID of the canonical OntologyFact event)
  - firstSeenAt: timestamp
  - lastSeenAt: timestamp
  - sourceIds: []string (all sources that contributed)
  - ttl: timestamp (firstSeenAt + 48h, for cleanup)
```

**Flow:**
1. Before creating `OntologyFact(kind=event)`, compute fingerprint.
2. Query `incident_fingerprints` by `fingerprint` where `lastSeenAt > now - 48h`.
3. If found:
   - Update existing event: merge `SourceRefs`, bump confidence if higher.
   - Update `incident_fingerprints` doc: `lastSeenAt`, append `sourceIds`.
   - **Do not create new event.** Do not run propagation again (or run idempotent merge).
4. If not found:
   - Create new event.
   - Create `incident_fingerprints` doc.

**Option B: Field on OntologyFact**

Add `IncidentFingerprint` to `OntologyFact`. Before create, query `ontology_facts` where `kind=event` AND `incidentFingerprint=fp` AND `updatedAt > now-48h`. If found → merge. Simpler but requires composite index.

**Recommendation:** Option A (separate collection) for clearer separation and easier TTL/cleanup.

### 5.3 URL Canonicalization

```go
func canonicalizeIncidentURL(url string) string {
    u, _ := url.Parse(strings.TrimSpace(url))
    if u == nil { return "" }
    u.RawQuery = ""  // strip tracking params
    u.Fragment = ""
    return strings.ToLower(u.String())
}
```

### 5.4 Content Fingerprint (no URL)

```go
func incidentContentFingerprint(title, geo, occurredAt time.Time, primaryEntity string) string {
    bucket := occurredAt.Truncate(6 * time.Hour).Unix()
    norm := strings.ToLower(strings.TrimSpace(title))
    norm = regexp.MustCompile(`\s+`).ReplaceAllString(norm, " ")
    payload := fmt.Sprintf("%s|%s|%d|%s", norm, geo, bucket, primaryEntity)
    return sha256Hex(payload)
}
```

---

## 6. Entity Resolution (Vertex AI Search + GCS)

**Goal:** Extract entity mentions from event text → resolve to `canonicalPath` in graph.

**Existing:** GCS export of Horizon fragments, Vertex AI Search index.

**Flow:**
1. From event: extract candidate entities (NER or LLM: "G42", "PIF", "Flydubai").
2. Query Vertex AI Search with entity string + optional filters (geo, sphere).
3. Return top-K fragments with scores.
4. Threshold: score > 0.7 → use as `ActorPath` / `SeedPath`.
5. Fallback: fuzzy match on `Identifiers`, `Labels`, `CanonicalPath` if Search returns nothing.

**Contract:**
```go
type EntityResolver interface {
    Resolve(ctx context.Context, query string, opts ResolveOptions) ([]ResolvedFragment, error)
}
type ResolveOptions struct {
    GeoFilter    string   // e.g. /loc/ae/na/na
    SphereFilter []string
    Limit        int
}
```

**Implementation:** New package `internal/horizon/signaling/entity_resolver.go` wrapping Vertex AI Search client. Use existing `VERTEX_SEARCH_HORIZON_DATA_STORE_ID` and GCS sync.

---

## 7. Phased Implementation

### Phase 1: Foundation (no new sources)
- [ ] Add `IsSystemSource` (or `Scope`) to `SourceDetails`; extend poller: system sources polled unless explicitly paused
- [ ] Add `SourceTier` to `SourceDetails`, extend `cadenceToDuration`
- [ ] Create `signaling` package skeleton
- [ ] Incident fingerprint logic (canonical URL + content fingerprint)
- [ ] `incident_fingerprints` Firestore collection + dedup check before event create
- [ ] Wire dedup into existing worldmonitor_pipeline `ontology_fact` path

### Phase 2: Adapter + Extraction
- [ ] Signaling adapter: document/event → `OntologyFact(kind=event)` with fingerprint
- [ ] Reuse `SignalExtractor` for event → 12-axis signals
- [ ] Seed resolution: geo from event + ActorPaths from entity resolution (stub first)
- [ ] Call `SignalPipeline.ProcessEvent` from adapter (when event is new after dedup)

### Phase 3: Entity Resolution
- [ ] `EntityResolver` with Vertex AI Search
- [ ] Wire into seed resolution: extracted entities → `Resolve` → canonical paths
- [ ] Fallback: fuzzy match on fragment store if Search empty

### Phase 4: RSS Integration
- [ ] Extend RSS ingest path: after document create, optional signaling branch
- [ ] Config: which RSS sources feed signaling (e.g. by tag or project link)
- [ ] Same dedup + extraction + propagation flow

---

## 8. Dependencies

- **Existing:** DataSource, Orchestrator, poller_worker, worldmonitor_pipeline, SignalPipeline, SignalExtractor, SeedInjector, PropagationEngine
- **New:** `incident_fingerprints` repo, `EntityResolver`, `signaling` package

---

## 9. Open Questions

1. **Merge semantics:** When we merge into existing event (dedup hit), do we re-run propagation? Or assume propagation already ran and skip? → Recommend: skip propagation on merge; optionally enqueue a lightweight "refresh" if confidence increased significantly.
2. **TTL cleanup:** Cron job to delete `incident_fingerprints` where `ttl < now`? Or Firestore TTL policy?
3. **RSS vs horizon_monitor_feed:** Both produce documents. Unify the "event" extraction step so both paths feed the same signaling adapter.
