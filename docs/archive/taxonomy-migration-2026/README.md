# Taxonomy Migration 2026 — historical archive

This directory preserves working documents from the AH-taxonomy migration
(P0–P14) for reference and post-mortem. These documents describe a completed
migration and are **not** authoritative for the running system.

For the current system of record, see:

- [`docs/taxonomy/CODEGEN.md`](../../taxonomy/CODEGEN.md) — how Go / TypeScript / Python bindings are generated
  from `shared/taxonomy/taxonomy.json`.
- [`docs/taxonomy/materialized_payload.md`](../../taxonomy/materialized_payload.md) — current MaterializedArtifact payload
  schema (v1, AH-shaped).
- [`docs/taxonomy/pathquery_design.md`](../../taxonomy/pathquery_design.md) — current `/api/v1/horizon/pathquery` design.
- [`docs/horizon/architecture/supply-chain-canonical-control-plane.md`](../../horizon/architecture/supply-chain-canonical-control-plane.md) — canonical
  control plane.
- [`docs/canonical_names_and_paths/`](../canonical_names_and_paths/) — canonical paths and product transforms.

## Contents of this archive

- **`MIGRATION_SPEC_P0_P14.md`** — the full P0–P14 plan (formerly `REMAINING_WORK_SPEC.md`); completed.
- **`P13_CLOSEOUT.md`** — final verification log for the cleanup phase.
- **`migration_map.yaml`** — legacy-type → AH disposition map (no live Firestore data uses this file).
- **`field_inventory.yaml`** — legacy fragment/edge/materialized field inventory.
- **`cli_tools_inventory.yaml`** — cull plan for `cmd/tools/` during the migration.
- **`criticalchain_fold_inventory.md`** — inventory of the criticalchain subsystem fold.
