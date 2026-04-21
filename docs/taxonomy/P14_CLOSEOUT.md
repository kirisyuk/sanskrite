# P14 — docs-actualize — CLOSEOUT

**Status:** DONE.

## Objective

Archive completed migration artefacts, bring `AGENTS.md` and roadmap expectations in line with the post-P13 reality, and leave `docs/taxonomy/` containing only living documents (`CODEGEN.md`, `materialized_payload.md`, `pathquery_design.md`, plus this closeout).

## What moved

See [`docs/archive/taxonomy-migration-2026/README.md`](../archive/taxonomy-migration-2026/README.md).

## What stayed (under `docs/taxonomy/`)

- [`CODEGEN.md`](./CODEGEN.md) — active
- [`materialized_payload.md`](./materialized_payload.md) — active (v1 AH-shaped contract)
- [`pathquery_design.md`](./pathquery_design.md) — active

## `AGENTS.md`

Updated: taxonomy system-of-record section; carve-out rules; prohibition on ad-hoc application-level alias tables.

## Roadmap

`docs/roadmap/` contains only `site/insights-portal-vision.md` — no taxonomy-migration rows to mark. An empty commit records that scope gap (`docs(roadmap): …`).

## Verification

`find docs -type f | sort` → **233** markdown and supporting files under `docs/` (full tree is larger than the illustrative layout in the P14 brief; the invariant below is **`docs/taxonomy/`** contents).

**`docs/taxonomy/` directory (expected living + closeout files):**

```text
CODEGEN.md
materialized_payload.md
pathquery_design.md
P14_CLOSEOUT.md
```

**`docs/archive/taxonomy-migration-2026/`:**

```text
README.md
MIGRATION_SPEC_P0_P14.md
P13_CLOSEOUT.md
migration_map.yaml
field_inventory.yaml
cli_tools_inventory.yaml
criticalchain_fold_inventory.md
```

**Cross-reference oracle (2026-04-21):**

```bash
git grep -n 'docs/taxonomy/migration_map' -- .
```

Captured output: *(no matches; exit 1)*

```bash
git grep -n 'REMAINING_WORK_SPEC\.md' -- docs/ backend/ web/ agents/
```

Captured output: only references **inside** `docs/archive/taxonomy-migration-2026/` (historical prose) and the archive `README.md`.

## Post-change sanity (no doc-driven code edits)

```text
cd backend && go build ./...   → OK
cd backend && go vet ./...     → OK
cd web && npx tsc -b           → OK
```

## Git commits (this P14 wave)

```text
4999620c docs(taxonomy): P14 closeout — docs layout finalized
ea81dd62 docs: fix cross-references to archived migration artifacts
17d5b317 docs(roadmap): no taxonomy migration rows under docs/roadmap (only site/); see P14 closeout
d48aabe7 docs(AGENTS): update taxonomy SoT and carve-out rules post-P13
e9fd77a4 docs(taxonomy): archive migration spec, inventories, and P13 closeout
```

## Roll-forward checklist (human)

- **cleanup-code** — COMPLETED (P13).
- **docs-actualize** — COMPLETED (P14).
- **bootstrap-clean-deferred** — **PENDING** (re-ingestion via scout pipeline after new Firestore provisioning; not a P14 blocker).
