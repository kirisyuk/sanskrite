# AgentHorde taxonomy codegen pipeline

The AgentHorde taxonomy is the single source of truth (SoT) for fragment
classes, relations, subtype enums, and attribute slots. It lives in Python:

  agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py

Two consumers — the Go backend and the TypeScript frontend — never read that
Python directly. Instead, a small three-stage pipeline keeps both languages
in lock-step with the SoT.

## Stage 1 — Python → JSON snapshot

Run from `agents/shared/agenthorde_common`:

```
python3 -m scripts.export_taxonomy --out ../../../shared/taxonomy/taxonomy.json
```

The snapshot is a stable, diff-friendly JSON dump committed to the repo at
`shared/taxonomy/taxonomy.json`. CI uses

```
python3 -m scripts.export_taxonomy --out ../../../shared/taxonomy/taxonomy.json --check
```

to fail when the snapshot drifts from the Python SoT.

## Stage 2 — JSON → Go (`go generate`)

The generated Go package lives at `backend/internal/taxonomy/ah` (AgentHorde-
aligned). Its `doc.go` carries the directive:

```
//go:generate go run github.com/igorkirisyuk/sanskrite/internal/tools/taxonomygen \
//   -in ../../../../shared/taxonomy/taxonomy.json \
//   -out ./taxonomy_generated.go \
//   -package ah
```

Regenerate with:

```
cd backend && go generate ./internal/taxonomy/ah
```

CI guard mode (used by tests / pipelines):

```
cd backend && go run github.com/igorkirisyuk/sanskrite/internal/tools/taxonomygen \
  -in ./internal/taxonomy/ah/../../../../shared/taxonomy/taxonomy.json \
  -out ./internal/taxonomy/ah/taxonomy_generated.go -package ah -check
```

The codegen tool itself is a tiny stdlib-only Go program at
`backend/internal/tools/taxonomygen` with its own `go.mod`, registered as a
work-module in the top-level `go.work`. It emits typed wrappers:

* `FragmentClass`, `RelationName`, `Layer`, `LayerSection` enums
* `AssuranceType`, `DecisionSubtype`, `InfluenceKind`, `MechanismCategory`,
  `DisruptionKind`, `MobileAssetKind`, `CommunitySubtype`, `EvidenceSubtype`,
  `ProductFamily` subtype enums (each with a `<Type>Values` slice)
* `RelationAllowedLayers`, `RelationLayerSection`, `RelationEndpointKinds`,
  `RelationEndpointBlacklist` lookup tables
* `AttributeSpec` / `AttributeSpecs` and `AttrKey<Name>` constants
* `SanctionsRegimes`, `Incoterms2020` catalogs
* `SchemaVersion` constant tied to the JSON snapshot

A small smoke test (`taxonomy_generated_test.go`) locks in the high-level
shape so silent shrinkage trips the backend test run.

## Stage 3 — JSON → TypeScript (`npm run gen:taxonomy`)

The generated TS module lives at `web/src/lib/taxonomy.generated.ts` and is
emitted by `web/scripts/generate-taxonomy.mjs`. Available npm scripts:

* `npm run gen:taxonomy` — regenerate the TS module from the JSON snapshot.
* `npm run gen:taxonomy:check` — CI guard; non-zero exit if the file is stale.
* `npm run gen:taxonomy:export` — convenience wrapper that re-runs Stage 1
  before regenerating.
* `npm run gen:all` — runs schemas → types → taxonomy:export → taxonomy.

The TS file follows the standard "object-as-enum" pattern:

```ts
export const RELATION = { Supplies: "supplies", ... } as const;
export type RelationName = (typeof RELATION)[keyof typeof RELATION];
```

so consumers get exhaustive type narrowing without enum runtime overhead.
Lookup tables are typed as `Readonly<Partial<Record<RelationName, ...>>>`
where the SoT does not guarantee an entry per relation (e.g. relations with
no endpoint blacklist).

## Update workflow

1. Edit `taxonomy.py` in the canonicalizer package.
2. Run the agent test suite (`pytest agents/vertex/agenthorde/tests`) to
   make sure the SoT change does not break canonicalizer logic.
3. `cd agents/shared/agenthorde_common && python3 -m scripts.export_taxonomy --out ../../../shared/taxonomy/taxonomy.json`
4. `cd backend && go generate ./internal/taxonomy/ah && go test ./internal/taxonomy/ah/...`
5. `cd web && npm run gen:taxonomy`
6. Commit `taxonomy.py`, `shared/taxonomy/taxonomy.json`,
   `backend/internal/taxonomy/ah/taxonomy_generated.go`, and
   `web/src/lib/taxonomy.generated.ts` together. Generated files MUST land
   in the same commit as the SoT edit so CI never sees a partial state.
