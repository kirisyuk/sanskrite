# Graph Placeholder Debt Review

## Goal

Reduce placeholder graph debt that harms deterministic `rss -> signal -> seed -> propagation` quality.

For early warning, placeholder nodes are harmful because they:

- absorb propagation mass into weak or fake endpoints;
- create false tails (`/fac/na/...`, `/loc/{cc}/na/na`);
- degrade Vertex retrieval quality;
- inflate graph size without adding operational semantics.

## Snapshot

Live graph scan on `2026-03-08`:

- initial placeholder paths: `2986`
- after current cleanup pass: `2820`

By class after cleanup:

- `facility_unknown_geo`: `2646`
- `org_unknown_jurisdiction`: `88`
- `gov_unknown_jurisdiction`: `17`
- `loc_placeholder_admin1_admin2`: `43`
- `loc_placeholder_admin2`: `26`

Main trend:

- legacy `/loc/*/na/*` padding is shrinking quickly;
- remaining debt is now dominated by facility geo/identity quality;
- org/gov debt is smaller, but more semantic than geocoding-driven.

## Root Causes

### 1. Legacy geo path contract

Old logic and tests still assumed padded geo paths:

- `/loc/{cc}/na/na`
- `/loc/{cc}/{admin1}/na`

Even though `GeoCanonicalPath(...)` already supports variable-depth `/loc/{cc}[/{level1}[/{level2}]]`.

### 2. Facility path creation with unknown geo

Several birthpoints still created `/fac/na/...` directly:

- worldmonitor importer
- graph-intent fallback
- facility/name-first canonicalization branches

### 3. Overly strict facility geo acceptance

Google geocoding could resolve many place-rich labels, but the result was rejected because:

- `homeLocation.name` was empty;
- acceptance logic required stronger place confirmation than the label alone could provide.

Examples:

- `Munich Airport Cargo Terminal`
- `Cebu IT Park Facility`
- `Astrakhan Port`
- `Kumamoto Fab 1`

### 4. Org/gov placeholder birth is mostly semantic, not geocoding

Unknown-jurisdiction org/gov paths are often caused by:

- no country/jurisdiction hint in the label;
- regional/international institutions not mapped to regional geo tokens;
- synthetic generic org placeholders from supply-chain style extraction;
- type confusion (`/gov/na/...` for non-government institutions).

Examples:

- `/gov/na/bureau-of-land-management`
- `/gov/na/dalio-family-office`
- `/gov/na/international-code-council`
- `/org/na/freight-forwarder`
- `/org/na/generic-component-manufacturer`

### 5. Synthetic `general-production` debt

Legacy deterministic logic still allowed fake operational facilities:

- `/fac/{geo}/{orgSlug}-general-production`
- `/fac/{geo}/general-production`

This contradicted the updated model:

- prefer `org -> product` when facility is unknown;
- do not synthesize fake facilities.

## Core Fixes Applied

### Placeholder path policy

Added central classifier:

- `backend/internal/horizon/canonical/placeholder_paths.go`

Classes:

- `facility_unknown_geo`
- `org_unknown_jurisdiction`
- `gov_unknown_jurisdiction`
- `loc_placeholder_admin1`
- `loc_placeholder_admin1_admin2`
- `loc_placeholder_admin2`

### Runtime propagation penalties

Placeholder endpoints are now penalized in endpoint scoring:

- `backend/internal/horizon/propagation/endpoint_scoring.go`
- `backend/internal/horizon/propagation/endpoint_scoring_profile.go`

This improves trust even before full backfill finishes.

### Worldmonitor importer birth-control

Importer facility paths now derive geo from the same resolved source as geo links, instead of forcing `/fac/na/...`:

- `backend/cmd/tools/horizon_import_worldmonitor/main.go`

### Graph-intent birth-control

Graph-intent fallback now uses geo hints from `state/metadata` when available instead of blindly defaulting to `na`:

- `backend/internal/http/horizon/graph_intent.go`

### Org/gov heuristic repair

Added deterministic org/gov placeholder repair:

- `backend/internal/horizon/scout/org_placeholder_repair.go`

Used by:

- `RevalidateOrgGeo(...)`
- org hierarchy builder
- product hierarchy builder

This adds:

- regional token inference (`asean`, `african-union`, `eu`, `gcc`, `int`, ...)
- limited country/subnational label hints
- type correction pressure (`gov` vs `org`)

### Facility place-hint acceptance

Facility geocoding now accepts place-rich labels more often even when `homeLocation.name` is empty:

- `backend/internal/horizon/scout/facility_geo_revalidation.go`

This specifically improves labels like:

- `Munich Airport Cargo Terminal`
- `Cebu IT Park Facility`
- `Astrakhan Port`

### Synthetic `general-production` removal

Removed runtime synthesis of fake facility fallbacks from org/gov refs:

- `backend/internal/horizon/models/supply_link_helpers.go`

Also disabled the legacy migration tool that could recreate this debt:

- `backend/cmd/tools/supply_chain_migrate_org_to_fac/main.go`

## Backfill Tools Added/Used

### Diagnose all placeholder paths

- `backend/cmd/tools/horizon_diagnose_placeholder_paths/main.go`

### Backfill placeholder paths

- `backend/cmd/tools/horizon_backfill_placeholder_paths/main.go`

Modes:

- `loc`
- `orggov`
- `all`

### Facility geo-path backfill

- `backend/cmd/tools/horizon_backfill_facility_geo_paths/main.go`

Google-first revalidation with controlled path migration and duplicate merge.

### Inspect unresolved placeholder geo failures

- `backend/cmd/tools/horizon_inspect_placeholder_geo_failures/main.go`

Used to inspect why specific unresolved placeholders still fail revalidation.

## What Is Still Unresolved

### Facility unknown geo (`2646`)

This is now the dominant remaining debt.

Subclasses:

- place-rich, geocodable labels that need more acceptance coverage;
- facility names with weak geographic hints;
- synthetic/generalized facilities that should never have been persisted;
- duplicate/collision debt from low-information facility slugs.

### Org/gov unknown jurisdiction (`105` total)

These are no longer primarily a geocoding problem.

The remaining debt is mostly:

- generic/synthetic institutions;
- misclassified gov/org entities;
- regional/international institutions that still lack enough deterministic mapping;
- truly ambiguous nodes that should remain unresolved rather than guessed.

### Remaining `/loc/*/na/*`

This is mostly legacy padding and should be driven close to zero by deterministic staged backfill.

## Recommended Next Fixes

### 1. Continue staged facility geo backfill

Keep applying:

- `horizon_backfill_facility_geo_paths`

in batches, because this is now the biggest payoff class.

### 2. Add create-time rejection for synthetic generic org placeholders

New org/gov nodes should not be persisted when they are clearly generic role placeholders, e.g.:

- `freight forwarder`
- `generic component manufacturer`
- `upstream steel supplier`

Recommended policy:

- if `/org/na/...` and label is generic operational role without jurisdiction and without strong source evidence:
  - do not persist as anchor fragment;
  - keep as unresolved extraction artifact or skip creation.

### 3. Expand deterministic jurisdiction hints

Especially for:

- subnational sovereign hints (`abu dhabi`, `riyadh`, etc.)
- regional institutions
- known government naming patterns

### 4. Finish removing synthetic `general-*` facility assumptions

Keep `org -> product` as the fallback representation when facility is unknown.

Do not materialize fake facilities just to satisfy path shape expectations.

## Calibration Guidance

Use this loop:

1. diagnose placeholder classes
2. inspect hard-fail samples
3. encode deterministic rule
4. backfill affected class
5. re-run graph diagnostics
6. verify propagation quality on live RSS

This is the right order because early warning quality depends more on graph hygiene than on adding more input feeds.
