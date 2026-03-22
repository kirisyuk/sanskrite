# RAG Documents/Chunks Removal — Implementation Plan

**Status:** Phase 1 Complete (RSS facts-only)  
**Date:** 2026-02-21  
**Context:** Unify on ontology facts + GCS/Vertex. Remove `rag_documents`, `rag_chunks`, and Firestore vector index.

**Phase 1 done:** RSS uses facts-only path: NormalizedDocument → document.extract → FactsExtractor → OntologyFacts. No Document/Chunks for RSS.

---

## 1. Current Flow

```
[RSS / Horizon Monitor / Telegram / Web]
         ↓
[Provider: fetch → NormalizedDocument[]]
         ↓
[Orchestrator.processProviderDocuments]
         ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. docs.GetByExternalID / docs.Create / docs.Upsert  → rag_documents       │
│ 2. ChunkText() → embed.EmbedTexts() → chunks.Create   → rag_chunks           │
│ 3. docs.Upsert(status="embedded")                                            │
│ 4. publish(document.process) [optional, skipped for horizon_monitor_feed]      │
└─────────────────────────────────────────────────────────────────────────────┘
         ↓
[DocumentProcessor] (subscribed to document.process)
         ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. docsRepo.GetByExternalID → load Document                                  │
│ 2. LLM: summarization, structured extraction (entities, events, claims)       │
│ 3. OntologyExtractor.ExtractFromDocument(doc)                                 │
│ 4. factsRepo.Create/Upsert → ontology_facts                                   │
│ 5. OntologyFactsRepository.exportToGCSAndSync → GCS + Vertex                  │
│ 6. docsRepo.Upsert(status="processed")                                        │
└─────────────────────────────────────────────────────────────────────────────┘
         ↓
[OntologyFact] → GCS → Vertex AI Search
```

**Horizon Monitor (ontology_fact mode) nuance:**  
`HorizonMonitorPipeline.ProcessDocuments` receives `NormalizedDocument[]` directly and creates `OntologyFact`s via `upsertEventFact` without using Document/Chunks. However, `processProviderDocuments` is still called first and creates Document+Chunks for every feed item. Those Document/Chunks are unused by the ontology pipeline in this mode.

**CoreDoc indexing:**  
CoreDocs are indexed separately via `ProjectsHandler.indexCoreDoc` and `CoreDocsToolExecutor`: they create Document + Chunks in rag_documents/rag_chunks for RAG search.

---

## 2. Target Flow

```
[RSS / Horizon Monitor / Telegram / Web]
         ↓
[Provider: fetch → NormalizedDocument[]]
         ↓
[Inline extraction OR Signaling adapter]
         ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Extract entities/events/claims from NormalizedDocument (LLM or rules)      │
│ 2. Build OntologyFact(s)                                                      │
│ 3. factsRepo.Create/Upsert → ontology_facts                                   │
│ 4. OntologyFactsRepository.exportToGCSAndSync → GCS + Vertex                   │
└─────────────────────────────────────────────────────────────────────────────┘
         ↓
[OntologyFact] → GCS → Vertex AI Search
```

**No Document. No Chunks.**

- **RSS / Horizon Monitor:** NormalizedDocument → inline extraction or signaling adapter → OntologyFact
- **CoreDocs:** CoreDoc content → inline extraction → OntologyFact (or continue using Vertex for project docs via a different path)
- **Semantic search:** Vertex AI Search over ontology_facts (and optionally Horizon fragments), not rag_chunks

---

## 3. Consumers of DocumentsRepository and ChunksRepository

### 3.1 DocumentsRepository

| Consumer | Location | Usage | Action |
|----------|----------|-------|--------|
| **Orchestrator** | `internal/discovery/orchestrator.go` | Create/Upsert Document in processProviderDocuments; IngestWeb, IngestTelegram | **Remove** — stop creating Documents; feed NormalizedDocument to extraction |
| **DocumentProcessor** | `internal/discovery/processor_worker.go` | GetByExternalID, Upsert(status) | **Remove** — processor obsolete; extraction inline |
| **DiscoveryHandler** | `internal/http/discovery.go` | GetByID for SearchDocuments (resolve doc from chunk) | **Replace** — search via Vertex/OntologyFacts; no doc resolution |
| **SourcesHandler** | `internal/http/sources.go` | ListBySource, ListByStatus (ReprocessDocuments) | **Migrate** — ListBySource → OntologyFactsRepo by sourceId; ReprocessDocuments → republish extraction events or remove |
| **ProjectsHandler** | `internal/http/projects.go` | indexCoreDoc, CheckDocumentIndexedStatus, DeleteBySource | **Migrate** — indexCoreDoc → OntologyFact path; CheckDocumentIndexedStatus → Vertex/OntologyFacts; DeleteBySource → delete facts by source |
| **CoreDocsToolExecutor** | `internal/http/coredocs_tools.go` | Create Document for CoreDoc indexing | **Migrate** — index CoreDoc as OntologyFact |
| **DocumentReadExecutor** | `internal/agents/capability_executors.go` | GetByID, GetByCoreDocID (resolve doc ID → CoreDoc) | **Replace** — resolve via OntologyFactsRepo or CoreDocsRepo; Vertex returns fact IDs |
| **DataSearchExecutor** | `internal/agents/capability_executors_advanced.go` | documentsRepo for validation | **Replace** — validate via OntologyFactsRepo or Vertex |
| **SourceExecutor** | `internal/horizon/scout/executor.go` | Create Document for web/document sources | **Replace** — return content directly or emit OntologyFact |
| **Monitor** | `internal/horizon/watchdog/monitor.go` | documentsRepo (scan sources) | **Migrate** — scan OntologyFacts by source or use factsRepo-only path |
| **VertexSearchTracker** | `internal/ai/vertex_search_tracker.go` | MarkTenantDocumentsAsIndexed | **Replace** — track Vertex import for ontology_facts; remove doc-status concept |
| **DocumentsHandler** | (artifact queries) | Uses docsRepo for artifact document queries | **Migrate** — query ontology_facts or Vertex |

### 3.2 ChunksRepository

| Consumer | Location | Usage | Action |
|----------|----------|-------|--------|
| **Orchestrator** | `internal/discovery/orchestrator.go` | chunks.Create in processProviderDocuments | **Remove** — no chunks |
| **DiscoveryHandler** | `internal/http/discovery.go` | FirestoreVectorIndex(chunksRepo).Query, chunksRepo.GetByIDs | **Replace** — Vertex AI Search over ontology_facts |
| **ProjectsHandler** | `internal/http/projects.go` | chunks.Create (indexCoreDoc), DeleteByDoc | **Remove** — no chunks |
| **CoreDocsToolExecutor** | `internal/http/coredocs_tools.go` | chunks.Create, FirestoreVectorIndex for search | **Replace** — Vertex search; index as OntologyFact |
| **FirestoreVectorIndex** | `internal/ai/vector_index_firestore.go` | Wraps ChunksRepository for KNN | **Deprecate/Remove** — use Vertex only |

### 3.3 FirestoreVectorIndex

| Consumer | Location | Action |
|----------|----------|--------|
| DiscoveryHandler | `internal/http/discovery.go` | Replace with Vertex AI Search |
| CoreDocsToolExecutor | `internal/http/coredocs_tools.go` | Replace with Vertex AI Search |

---

## 4. Phased Implementation

### Phase 1: Orchestrator — RSS → Facts (no Document/Chunks for new path)

**Goal:** Add a parallel path that produces OntologyFacts directly from NormalizedDocuments, without creating Document or Chunks.

1. **Extract extraction logic** from DocumentProcessor into a reusable component:
   - `ExtractOntologyFromNormalizedDocument(ctx, nd NormalizedDocument, tenantID, sourceID string) ([]*OntologyFact, error)`
   - Reuse OntologyExtractor patterns (entities, events, claims) but accept NormalizedDocument instead of Document.

2. **Add orchestrator option** `SkipDocumentChunks bool` (or ingest mode):
   - When true: skip `docs.Create`, `chunks.Create`, `publish(document.process)`.
   - Call extraction inline and persist via OntologyFactsRepository.

3. **Wire for Horizon Monitor first** (already ontology_fact mode):
   - In `IngestHorizonMonitorFeed`, when mode=ontology_fact: call extraction inline, skip processProviderDocuments for Document/Chunks, or add a "facts-only" branch that only runs HorizonMonitorPipeline + inline extraction.

4. **Extend to RSS** (opt-in per source or global flag):
   - When enabled: IngestRSS → NormalizedDocuments → inline extraction → OntologyFacts.
   - Keep legacy path (Document+Chunks) for sources that still need it during transition.

5. **Deliverables:**
   - New extraction adapter: `NormalizedDocument → OntologyFact[]`
   - Orchestrator branch: RSS/HorizonMonitor → facts only (no docs/chunks)
   - Feature flag or DataSource.Details field to toggle

---

### Phase 2: Remove Document/Chunks from Orchestrator

**Goal:** Stop creating Document and Chunks in the main ingest path.

1. **Default to facts-only** for all RSS and Horizon Monitor sources.
2. **Remove** `processProviderDocuments` Document/Chunk creation:
   - Delete `docs.Create`, `docs.Upsert`, `chunks.Create` from orchestrator.
   - Remove `publish(document.process)` for these sources.
3. **Deprecate DocumentProcessor** for RSS/HorizonMonitor:
   - Stop publishing document.process from orchestrator.
   - DocumentProcessor can remain for legacy Telegram/Web if still needed, or migrate those too.
4. **Migrate Telegram and Web ingest** to facts-only path (or keep minimal Document for provenance only, without Chunks).
5. **Deliverables:**
   - Orchestrator no longer creates Document/Chunks for RSS and Horizon Monitor.
   - DocumentProcessor receives no new events from these sources.

---

### Phase 3: Deprecate DocumentsRepository, ChunksRepository, FirestoreVectorIndex

**Goal:** Remove RAG persistence and Firestore vector index.

1. **DiscoveryHandler.SearchDocuments:**
   - Replace FirestoreVectorIndex + ChunksRepository with Vertex AI Search over ontology_facts (and Horizon datastore if applicable).
   - Map Vertex results to OntologyFact or a unified search result type.
   - Remove docsRepo.GetByID for chunk→doc resolution.

2. **ProjectsHandler / CoreDocsToolExecutor:**
   - indexCoreDoc: Create OntologyFact from CoreDoc content instead of Document+Chunks.
   - CheckDocumentIndexedStatus: Query Vertex or OntologyFactsRepo by CoreDoc ID / project.
   - DeleteBySource: Delete ontology_facts by sourceId (add method if needed).

3. **SourcesHandler:**
   - ListBySource: OntologyFactsRepo.ListBySource (add method) or Vertex search by sourceId.
   - ReprocessDocuments: Remove or replace with "re-extract facts for source" (re-fetch feed, re-run extraction).

4. **Agents:**
   - DocumentReadExecutor: Resolve via OntologyFactsRepo or CoreDocsRepo; Vertex returns fact IDs.
   - DataSearchExecutor: Validate via Vertex/OntologyFacts; remove documentsRepo dependency.

5. **SourceExecutor / Monitor:**
   - SourceExecutor: Return content or emit OntologyFact; remove documentsRepo.Create for web/document sources.
   - Monitor: Use factsRepo for scanning; remove documentsRepo.

6. **VertexSearchTracker:**
   - Adapt for ontology_facts Vertex import; remove MarkTenantDocumentsAsIndexed on DocumentsRepository (or implement equivalent for facts if needed).

7. **Remove repositories and index:**
   - Delete or deprecate `DocumentsRepository`, `ChunksRepository`.
   - Delete `FirestoreVectorIndex`.
   - Remove Firestore rules and indexes for rag_documents, rag_chunks.

8. **Deliverables:**
   - No code references to DocumentsRepository, ChunksRepository, FirestoreVectorIndex.
   - Firestore collections rag_documents, rag_chunks can be archived or dropped.

---

### Phase 4: Cleanup cmd Tools and Handlers

**Goal:** Remove or adapt all CLI tools and handlers that depended on Document/Chunks.

1. **cmd tools to remove or adapt:**
   - `check-doc-status` — remove or adapt to check OntologyFact/Vertex status.
   - `reindex-all-coredocs` — reindex CoreDocs as OntologyFacts.
   - `reindex-coredocs` — same.
   - `reindex-artifacts` — adapt to ontology_facts.
   - `update-vertex-index-status` — adapt for ontology_facts Vertex datastore.
   - `update-gcs-metadata` — adapt for ontology_facts GCS export.
   - `sync-all-vertex-search` — adapt for ontology_facts.
   - `horizon_test_signaling_rss` — remove docs/chunks deps.
   - `horizon_intent_scenario`, `horizon_test_intent_decomposition` — remove documentsRepo.

2. **Handlers:**
   - Remove DocumentsRepository, ChunksRepository from server wiring.
   - Remove DocumentProcessor worker if fully obsolete.
   - Update DocumentsHandler if it queried rag_documents.

3. **Infrastructure:**
   - scripts/deploy: Remove rag_documents, rag_chunks vector index creation.
   - firestore.rules: Remove rag_documents, rag_chunks rules.
   - firestore.indexes.json: Remove rag_documents, rag_chunks indexes.
   - BigQuery/Firestore export config: Update if rag_documents was exported.

4. **Deliverables:**
   - All cmd tools either removed or adapted.
   - Clean server bootstrap without RAG repos.

---

## 5. Data Migration (Optional)

- **Existing rag_documents/rag_chunks:** Can be left in place for historical reference or archived to GCS.
- **One-time backfill:** If needed, run a migration job: for each Document, run extraction → create OntologyFact. Chunks are not migrated (Vertex will index facts).

---

## 6. Dependencies

- **OntologyFactsRepository** — already supports GCS + Vertex sync.
- **Vertex AI Search** — must have a datastore for ontology_facts (or reuse Horizon datastore with tenant/project filters).
- **Signaling Pipeline** — see `SIGNALING_PIPELINE_IMPLEMENTATION_PLAN.md`; signaling adapter can be the "inline extraction" for event-heavy sources.

---

## 7. Open Questions

1. **CoreDoc search:** Should CoreDocs remain searchable via a separate Vertex datastore, or be folded into ontology_facts with a `sourceType=coredoc` filter?
2. **DocumentProcessor for Telegram/Web:** Migrate to facts-only or keep a minimal Document for audit trail?
3. **ReprocessDocuments semantics:** Replace with "re-extract facts" (re-fetch + extract) or remove endpoint?
4. **Vertex datastore strategy:** Single ontology_facts datastore vs. separate datastores for Horizon fragments vs. ingested facts?
