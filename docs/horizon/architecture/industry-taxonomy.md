# Industry taxonomy (/industry/...) — v1

This document defines a **canonical, multi-valued industry taxonomy** for Horizon.

It is designed to be:
- **Strict enough** to be used for filtering/scoping (via deterministic index paths)
- **Compatible** with Wikidata, where `P452 (industry)` is often the best available signal
- **Incrementally extensible** (we start small, expand via dictionary growth)

## Goals

- Provide a canonical path namespace for industries: **`/industry/...`**
- Support **multiple industries** per organization/person/product
- Canonicalize Wikidata-derived industry signals into stable paths
- Persist taxonomy in Firestore as part of the existing `horizon_taxonomy` document
- Allow fast deterministic filtering via `indexPaths` (not only semantic search)

## Canonical path format

We use a multi-level hierarchy:

- **`/industry/{sector}`**
- **`/industry/{sector}/{group}`**
- **`/industry/{sector}/{group}/{industry}`**
- **`/industry/{sector}/{group}/{industry}/{subindustry}`**

Rules:
- segments are lowercase slugs: `[a-z0-9-]`
- multiple industries are stored as an array (see storage below)
- industry paths are treated as “coordinates” similar to `/sphere/...`

Examples:
- `/industry/energy/oil-and-gas/upstream`
- `/industry/industrials/transportation/airlines`
- `/industry/technology/software`
- `/industry/financials/banking`

## Firestore storage (taxonomy)

Industry taxonomy is stored inside the existing taxonomy doc:

- Collection: `horizon_taxonomy`
- DocID: version (e.g. `v1`)

New fields (added in v1 bootstrap):
- **`canonicalPrefixes`** includes `/industry/`
- **`allowedIndustryPaths`**: explicit allowlist of canonical industry paths (seeded small)
- **`industrySynonyms`**: normalized text → canonical `/industry/...` mapping
- **`industryWikidataMap`**: Wikidata QID → canonical `/industry/...` mapping (starts small; grows)

Seeded by tool: `backend/cmd/tools/horizon_seed_taxonomy`.

## Firestore storage (fragments and entities)

We persist industries on:
- Horizon fragments (`horizon_fragments`)
- Ontology entities (`ontology_entities`)

Field:
- **`industryPaths: []string`** — array of canonical `/industry/...` paths

Indexing:
- We include `/industry/...` into fragment `indexPaths` as deterministic coordinates.
- We also include ancestor paths (`/industry/energy`, `/industry/energy/oil-and-gas`, …) up to a small depth.

## Wikidata integration

### Primary properties

- **Organizations**: `P452 (industry)` (multi-valued)
- **People** (optional): `P101 (field of work)` / `P106 (occupation)` — not applied yet in v1 script

### Canonicalization algorithm (v1)

Given a Wikidata QID for a subject:
1. Fetch Wikidata entity data
2. Extract **all** `P452` industry QIDs (multi-valued)
3. Map each industry QID to a canonical industry path:
   - First try `industryWikidataMap[qid]`
   - Else fetch label for that industry QID and try `industrySynonyms[normalize(label)]`
   - Else fallback to `/industry/other/{slug(label)}` (strict, but less semantically rich)
4. Deduplicate + stable sort
5. Persist to fragment/entity as `industryPaths`

### Important TODO (core)

In v1, we apply Wikidata→industry mapping in the **CLI tool** (`horizon_find_missing_wikidata`).

We will later move this into the **backend core pipeline** (Scout/PreCreate or enrichment step),
so newly created fragments/entities get industries consistently without manual runs.

## Initial taxonomy (seed)

The seed is intentionally small and expands iteratively:
- energy
- materials
- industrials (transportation + aerospace/defense)
- technology (software/hardware/semiconductors)
- communications
- financials (banking/asset-management/insurance)
- consumer (retail)
- healthcare
- public (government/military)
- agriculture
- utilities
- real-estate
- other

See seed code: `backend/cmd/tools/horizon_seed_taxonomy/main.go`.


