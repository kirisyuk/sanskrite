# P13 closeout verification

This note records the verification commands and known carve-outs for the P13 (AH snake_case wire, legacy token removal) closeout wave.

## Oracle (forbidden legacy tokens in code)

From the repository root, the following pattern is expected to yield **zero** matches in Go, TypeScript, and TSX sources:

```bash
rg 'TRANSFORMS_INTO|ASSEMBLES_INTO|SUPPLY_LINK|DEPOSIT|PROGRAM_IMPLEMENTATION|UPPER_SNAKE_CASE_EXAMPLE' \
  --glob '*.go' --glob '*.ts' --glob '*.tsx'
```

JSON and prose were spot-checked separately as needed; this gate targets executable / typed sources.

## Backend

Run from `backend/`:

```bash
go build ./...
go vet ./...
go test ./internal/... -count=1
```

Unused-code gate (P13-scoped packages only; `U1000` = unused):

```bash
go run honnef.co/go/tools/cmd/staticcheck@latest -checks=U1000 \
  ./internal/horizon/... \
  ./internal/http/horizon/... \
  ./internal/taxonomy/... \
  ./internal/discovery/... \
  ./internal/agentmesh/...
```

A globally installed `staticcheck` binary is optional; `go run` pins the tool version.

## Web (`web/`)

```bash
cd web
npm run lint
npx tsc -b
npm run test:graph-views
```

### Lint gate and carve-outs

- **Gate:** `npm run lint` must report **at most 5902** `@typescript-eslint` **errors** (warnings do not count against this ceiling). The last recorded run reported **6053 problems (5882 errors, 171 warnings)** — within the error budget.
- **Exit code:** ESLint still exits non-zero while the error backlog remains; CI or local scripts should parse counts or treat this as a tracked debt gate, not a green run.
- **`vite.config.ts`:** ESLint reports a parser project error because `vite.config.ts` is not included in the TypeScript solution referenced by `parserOptions.project`. Fixing that belongs to a small eslint/tsconfig follow-up, not the P13 taxonomy migration itself.

## Git commits for this wave

Logical split for reviewability (hashes on the branch that produced this log):

1. `45f8903a` — **Backend:** `p13: backend canonical alignment and staticcheck U1000 cleanup` — Horizon, discovery, agentmesh, HTTP handlers, cmd tools, and related tests under `backend/`.
2. `34bcfeac` — **Web:** `p13: web graph views and taxonomy alignment` — components, stores, and `web/src/lib/*` touched by P13.
3. **Docs:** `docs(taxonomy): P13 closeout verification log` — adds this file (see `git log -1 --oneline docs/taxonomy/P13_CLOSEOUT.md` for the hash).

## Follow-ups (non-blocking)

- Add `vite.config.ts` (or the whole repo root config set) to the ESLint TypeScript project, or give it an override that does not require `parserOptions.project`.
- Reduce the remaining ESLint error count below the 5902 ceiling over time until `npm run lint` can exit zero.
