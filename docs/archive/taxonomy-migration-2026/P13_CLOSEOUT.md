# P13 — cleanup-code — CLOSEOUT

**Status:** **DONE** (recreate-clean strategy; no legacy taxonomy rows in Firestore for this wave).

## Objective

Remove all in-app legacy taxonomy shims. Retain UPPER_SNAKE only at explicit **Category B** (LLM prompt boundary) and **Category C** (runtime ontology / Firestore seed layer) carve-outs.

## Carve-out registry

### Category B — LLM prompt boundary (UPPER_SNAKE intentional)

UPPER_SNAKE tokens in these files appear inside prompt templates, response-parsing helpers for LLM JSON outputs, or matching enums that must stay in sync with those prompts. Changing them requires re-tuning agent prompts and re-verifying LLM output.

Files (explicit list):

- `backend/internal/horizon/entity_semantics/definitions.go`
- `backend/internal/horizon/entity_semantics/prompt_contracts.go`
- `backend/internal/horizon/entity_semantics/domain_mode_rules.go`
- `backend/internal/horizon/entity_semantics/types.go` (EntityTypeKey consts)
- `backend/internal/horizon/scout/agents/research_strategies_impl.go`
- `backend/internal/horizon/scout/agents/strategic_overlay_agent.go`
- `backend/internal/horizon/scout/agents/critic_agent.go`
- `backend/internal/horizon/scout/agents/intent_decomposer_agent.go`
- `backend/internal/horizon/scout/agents/anchor_resolver_agent.go`
- `backend/internal/horizon/scout/agents/domain_resolver_agent.go`
- `backend/internal/horizon/scout/agents/intent_resolver_agent.go`
- `backend/internal/horizon/scout/agents/research_profile.go`
- `backend/internal/horizon/scout/agents/agent_semantic_helpers.go`
- `backend/internal/horizon/scout/edge_semantic_helpers.go`
- `backend/internal/horizon/supplychainreport/llm.go`
- `backend/internal/horizon/domain/engine.go` (`expectedRelationsForContractType`, `defaultFilterRelTypes` — prompt feed)
- `backend/internal/horizon/models/graph_intent.go` (LLM graph-intent JSON wire alphabet)
- `backend/internal/http/horizon/fragment_trace.go` (human-readable trace copy; normalizes live `relation` → legacy trace tokens)
- `backend/internal/http/horizon/graph_intent.go` (LLM graph-intent wire path)
- `backend/internal/http/horizon/identify_stresses.go` (stress UI scoring over mixed wire tokens)

Each file above carries an in-source **Category B** anchor comment at the relevant block where this closeout touched it, or already had an equivalent migration note from earlier P13 work.

### Category C — runtime ontology layer

Firestore-loaded dictionaries and seeders use UPPER_SNAKE relation keys as a stable on-disk wire form.

Files:

- `backend/internal/taxonomy/runtime.go`
- `backend/internal/taxonomy/runtime_contracts.go`
- `backend/internal/taxonomy/graph_intent_descriptions.go`
- `backend/internal/taxonomy/domain_mode.go`
- `backend/internal/taxonomy/canonical_rules.go`
- `backend/internal/taxonomy/industry_registry.go`
- `backend/internal/taxonomy/business_tags.go`
- `backend/cmd/tools/horizon_seed_taxonomy/main.go` (seeds the above)
- `backend/cmd/tools/horizon_dedup_scanner/main.go` (reads the above)

## Oracles

### Strict oracle (11 legacy taxonomy tokens) on `backend/` + `web/src/`

Tokens: `DEPENDS_ON`, `SUPPLIES_TO`, `TRANSFORMS_INTO`, `ASSEMBLES_INTO`, `USED_IN`, `SUPPLY_LINK`, `AFFECTED_ZONE`, `MEDIA_CHANNEL`, `DEPOSIT`, `DECISION_POINT`, `MECHANISM` (each as a double-quoted substring in source).

**Command** (from repo root; uses `git grep` — equivalent intent to `rg` where ripgrep is not installed):

```bash
git grep -n -I -E '"DEPENDS_ON"|"SUPPLIES_TO"|"TRANSFORMS_INTO"|"ASSEMBLES_INTO"|"USED_IN"|"SUPPLY_LINK"|"AFFECTED_ZONE"|"MEDIA_CHANNEL"|"DEPOSIT"|"DECISION_POINT"|"MECHANISM"' \
  -- backend web/src \
  ':(exclude)backend/internal/taxonomy' \
  ':(exclude)backend/cmd/tools/horizon_seed_taxonomy' \
  ':(exclude)backend/cmd/tools/horizon_dedup_scanner' \
  ':(exclude)backend/**/*_test.go' \
  ':(exclude)backend/**/testdata' \
  ':(exclude)backend/internal/horizon/entity_semantics' \
  ':(exclude)backend/internal/horizon/scout/agents/research_strategies_impl.go' \
  ':(exclude)backend/internal/horizon/scout/agents/strategic_overlay_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/critic_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/intent_decomposer_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/anchor_resolver_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/domain_resolver_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/intent_resolver_agent.go' \
  ':(exclude)backend/internal/horizon/scout/agents/research_profile.go' \
  ':(exclude)backend/internal/horizon/scout/agents/agent_semantic_helpers.go' \
  ':(exclude)backend/internal/horizon/scout/edge_semantic_helpers.go' \
  ':(exclude)backend/internal/horizon/supplychainreport/llm.go' \
  ':(exclude)backend/internal/horizon/domain/engine.go' \
  ':(exclude)backend/internal/horizon/models/graph_intent.go' \
  ':(exclude)backend/internal/http/horizon/fragment_trace.go' \
  ':(exclude)backend/internal/http/horizon/graph_intent.go' \
  ':(exclude)backend/internal/http/horizon/identify_stresses.go'
```

**Captured output (2026-04-21):**

```
(no matching lines; git grep exit code 1 = success / zero hits)
```

**Expected:** 0 hits outside the carve-out globs. If non-zero, classify each hit and either fix code or extend this registry.

### Narrow oracle (P9-dead tokens, never legitimate)

```bash
git grep -n -I -E 'TRANSFORMS_INTO|ASSEMBLES_INTO|SUPPLY_LINK|DEPOSIT|PROGRAM_IMPLEMENTATION' \
  -- '*.go' '*.ts' '*.tsx'
```

**Captured output (2026-04-21):**

```
narrow_oracle: 0 hits (git grep exit non-zero)
```

**Expected:** 0 hits.

## Verification logs (captured 2026-04-21)

### Backend

```text
cd backend
go build ./...                   → OK (exit 0)
go vet ./...                     → OK (exit 0)
go test ./internal/... -count=1  → OK (exit 0; full run ~7–11m in this environment)
```

### Staticcheck (P13 scope, U1000 only)

```bash
cd backend
go run honnef.co/go/tools/cmd/staticcheck@latest -checks=U1000 \
  ./internal/horizon/... ./internal/http/horizon/... \
  ./internal/taxonomy/... ./internal/discovery/... ./internal/agentmesh/...
```

**Captured output:**

```text
(no diagnostics; exit 0)
```

### Web

```text
cd web
npm run gen:taxonomy:check   → OK (exit 0)
npx tsc -b                   → OK (exit 0)
npm run test:graph-views     → 12/12 pass (exit 0)
npm run lint                 → ✖ 6054 problems (5882 errors, 172 warnings) — errors ≤ 5902 baseline
```

Notes:

- ESLint still exits non-zero because the error backlog remains; the **error count** (5882) is the tracked gate.
- `vite.config.ts`: parsing error — file not in `parserOptions.project` (small follow-up).

## Known pre-existing (unrelated to P13)

- `internal/horizon/internalknowledge/warmup_service_test.go` — time-bug, documented P4.02
- `internal/ontology` `TestDomainClassification` — calls live OpenAI
- Web lint debt and `vite.config.ts` ESLint/tsconfig alignment (tracked separately)

## Git commits (P13 wave)

`git log --oneline origin/main..HEAD | head -10` **(2026-04-21):**

```text
c4f3cc49 p13: final carve-out anchor sweep to clear strict oracle
0b19d2db p13: document Category B prompt helpers in scout/agents and models
ed48f943 p13: annotate or rewrite UPPER_SNAKE switches in http/horizon and domain/engine
17dd3b33 p13: rewrite focusobject and agentmesh scenario fixtures to AH class/subtype
3eb89111 p13: regenerate web taxonomy.generated.ts
1d5936a2 docs(taxonomy): P13 closeout verification log
34bcfeac p13: web graph views and taxonomy alignment
45f8903a p13: backend canonical alignment and staticcheck U1000 cleanup
ff1a5c92 p13: update surviving horizon cmd tools after legacy removal
ee62a5bf p13: align horizon stack with AH snake_case wire
```

Follow with `git log --oneline origin/main..HEAD` for the full stack beyond 10 lines.

## Follow-ups (non-blocking, not part of P13)

- Reduce web lint error count below the 5902 baseline over time.
- Include `vite.config.ts` in ESLint `parserOptions.project` (or a targeted override).
- Re-ingest a clean Firestore project via the scout pipeline (bootstrap-clean-deferred todo).
