# Product Synonym Normalization Result

Date: 2026-03-22
Project: `nv-horizon`
Tenant: `global`

## Scope

Completed stage:
- mining canonical product synonym pairs from real `PRODUCT` fragments
- persisting verified `canonical -> synonym` sample pairs into Firestore
- backfilling downstream references for already selected synonym decisions
- validating that stale product-path references in `SUPPLY_LINK` and materialized supply-chain artifacts were eliminated

This document fixes the state of the work after the successful `pass2` repair and the first real merge batch.

## Firestore State

Verified synonym sample pairs:
- collection: `horizon_product_synonym_pairs`
- active pair docs after the singular/plural canonicalization pass: `476`

Additional singular/plural pass:
- safe deterministic singular/plural pairs persisted: `52`
- artifact: `/tmp/product_singular_plural_safe_persisted.json`
- duplicate docs created by an interrupted first write were deduplicated:
  - `47` duplicate docs deactivated
  - active collection returned to the correct size after dedupe

Decision set after latest rebuild:
- file: `/tmp/product_synonym_pair_decisions_after_pass2.json`
- `decisions_built: 279`
- `synonyms_planned: 379`
- `missing_canonicals: 0`
- `missing_synonyms: 43`
- `cycle_components: 2`

## Downstream Repair Result

Final successful repair run:
- report: `/tmp/product_synonym_repair_apply_pass2.json`
- next worklist: `/tmp/product_synonym_repair_worklist_pass2.json`

Summary:
- `pairDocsLoaded: 424`
- `pathRewritesKnown: 422`
- `stalePathsBefore: 266`
- `pathRewritesPlanned: 266`
- `supplyLinksUpdated: 0`
- `materializedUpdated: 2368`
- `verifyFailures: 0`
- `remainingStalePaths: 0`

Meaning:
- product synonym path rewrites no longer leave stale refs in `SUPPLY_LINK`
- product synonym path rewrites no longer leave stale refs in materialized supply-chain overlays
- the repair worklist is now empty

## Real Merge Progress

First real merge batch after repair:
- report: `/tmp/product_synonym_apply_batch_001.json`

Summary:
- `decisions_planned: 25`
- `decisions_run: 25`
- `decisions_changed: 25`
- `merges_applied: 38`
- `merges_skipped: 0`
- `deleted_versions: 8`
- `name_updates: 0`
- `verify_failures: 0`

This confirms the current merge path is operational after downstream repair.

## Important Implementation Notes

- Synonyms are stored as sample pairs only: `canonical -> synonym`.
- Runtime graph semantics were not changed in this stage.
- Product path rewrites were repaired separately from fragment merges.
- The repair tool was changed from collection-wide scans to targeted rewrite passes:
  - `SUPPLY_LINK.productRef`
  - materialized `nodeRefs`
  - materialized `flowRefs`
  - materialized `productAxis`
- Materialized artifact writes were split into small Firestore batches to avoid RPC deadline failures.

## Remaining Work

Still not finished:
- the full fragment merge backlog is not yet exhausted
- after the first applied batch, remaining curated merge decisions are still present in `/tmp/product_synonym_pair_decisions_after_pass2.json`

Current remaining merge backlog:
- `279` decisions
- `379` synonym fragments planned

Operational next step from this checkpoint:
1. continue merge apply in batches
2. after each series of batches, rerun targeted repair
3. rebuild decisions until the merge backlog reaches zero

## Artifacts

Primary artifacts from this checkpoint:
- `/tmp/product_synonym_repair_apply_pass2.json`
- `/tmp/product_synonym_repair_worklist_pass2.json`
- `/tmp/product_synonym_pair_decisions_after_pass2.json`
- `/tmp/product_synonym_apply_batch_001.json`
- `/tmp/product_singular_plural_safe.json`
- `/tmp/product_singular_plural_safe_persisted.json`
