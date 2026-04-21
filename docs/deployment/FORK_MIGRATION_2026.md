# Fork migration — Prevalent Horizon (R1–R4)

**Status:** DONE in repo (four implementation commits + this closeout). **No push, no GCP deploy** was performed from automation.

## Objective

Retire legacy infrastructure naming (`nv-horizon`, `nv_horizon`, `horde-horizon-test-20260405`, old GCP project IDs in docs/config) for a clean fork targeting **`prevalent-horizon-test`**, with short Cloud Run service names and `horizon.*` / `horizon_documents` identifiers scoped by the GCP project.

Out of scope (explicit): Go module path `github.com/igorkirisyuk/sanskrite`, Python `agenthorde_*` packages, Firestore `horizon_*` collection names, `docs/archive/**`, `temp-repos/**`.

## Waves (commits)

### R1 — Cloud Run services, `cmd/ws`, Cloud Build, deploy flag

- `git mv backend/cmd/nv_horizon_ws → backend/cmd/ws`, `cloudbuild.nv-horizon-ws.yaml → cloudbuild.ws.yaml`.
- Service names: `nv-horizon-api|ws|telegram|event-gate|entity-extract` → `api`, `ws`, `telegram`, `event-gate`, `entity-extract`.
- `scripts/deploy`: `--ws` only (no `--nv-horizon-ws` alias), `TARGET_WS`, `TARGET_SETUP_WS_INFRA`, `deploy_ws()`, `DEPLOY_WS_SERVICE_URL`.
- Web defaults and helper scripts aligned; `.gitignore` adds `backend/ws` (local `go build` binary name collision).

### R2 — Pub/Sub, Cloud Tasks, BigQuery, setup scripts

- `backend/internal/realtime/pubsub.go`: topic constants `horizon.*` with **`horizon.core.events`** (not `horizon.horizon.events`); `routeTopicName` accepts only `horizon.*` prefix for that bucket.
- Cloud Tasks queue defaults: `scout`, `scout-bg`, `risk`, `profile-refresh`, `source-poll`, `scope-agents`, `agentmesh`; AgentMesh image `agentmesh`.
- BigQuery default dataset: **`horizon_documents`** (server, services, migrate/check/sync tools).
- `scripts/setup-horizon-pubsub.sh`, `scripts/check-horizon-infra.sh`; `scripts/deploy` topic arrays and Vertex import defaults `horizon.vertex.*`.
- Admin HTTP paths moved to **`/api/v1/admin/horizon/...`** (redirect shim removed in same window so `nv_horizon` grep on `backend/cmd` stays clean).

### R3 — `tmp_*` removal, doc touch-ups

- Removed: `backend/cmd/tmp_check_lens_artifacts`, `tmp_check_paths2`, `tmp_check_supply_artifacts`, `tmp_find_chains_for_scope`, `tmp_repair_lens_paths`.
- `horizon_repair_affected_zones`: Vertex store id fallback via `VERTEX_SEARCH_DATA_STORE_ID`.
- `docs/architecture/*.md` (BigQuery / Vertex examples): project + dataset names updated to `prevalent-horizon-test` / `horizon_documents`.
- `docs/architecture/apis.md`: admin jobs table updated.

### R4 — Firebase / env templates, docs sweep, stale artifacts

- **Firebase / web:** `web/.firebaserc`, `web/firebase.json` → `prevalent-horizon-test`; `web/firebase.sdkconfig.web.json` → `{}` (replace via `firebase apps:sdkconfig WEB`); `web/.env.example` placeholders.
- **Marketing site:** `site/.firebaserc` → default `prevalent-horizon-test`, hosting site **`prevalent-horizon-site`**.
- **Assets:** `web/src/assets/nv-horizon-*` → dropped prefix (`dark-theme.png`, `light-theme.png`, backgrounds).
- **Branding / HTTP:** default `User-Agent` strings and JSON schema `$id` hosts no longer use `sanskrite`; server log prefix `[horizon]`.
- **Docs / scripts sweep:** `horde-horizon-*`, `nv-horizon` project IDs in active docs and scripts (excluding `docs/archive`, `temp-repos`).
- **Removed stale tracked files:** `backend/.tmp_fs_indexes.json`, root/web `firepit-log.txt`, assorted `logs*.json`/`timeline*`/`req.json`/scripts, `graph_structure.txt`, `analyze_*.js`, `clean_csv.py`, Tesla logistics fixtures, `tmpcheck-catl-XXXX.go`, `ingress_policy.yaml`, `policy.yaml`, `backend/market_segments_report.txt`, `backend/getFragmentEntity_logs.json`, `backend/invalid_canonical_path_logs.json`.
- **`.gitignore`:** patterns for the above classes of accidental artifacts.
- **OpenRouter test:** `OPENROUTER_APP_TITLE` expectation `horizon-test` (was `sanskrite-test`).

## Deploy checklist (operator — after GCP project exists)

1. `gcloud projects create prevalent-horizon-test` (or use existing) and enable required APIs.
2. `firebase projects:addfirebase prevalent-horizon-test` (if using Firebase).
3. `firebase apps:sdkconfig WEB` → paste into `web/firebase.sdkconfig.web.json` (replace `{}`).
4. Vertex AI Reasoning Engine for AgentMesh → set `AGENTMESH_VERTEX_REASONING_ENGINE` in env / Cloud Run.
5. Vertex Search data stores (`horizon-fragments`, `horizon-rag-documents`) — e.g. `scripts/create-vertex-search-datastore.sh` after adaptation.
6. Secret Manager: Tavily, OpenAI, OpenRouter, Gemini, etc.
7. `firebase deploy --only firestore:indexes,firestore:rules` (resolve duplicate `ontology_claims` composite in `web/firestore.indexes.json` before first deploy if still present — see P15 closeout).
8. `scripts/setup-horizon-pubsub.sh` → eight aggregated topics + subscriptions.
9. `scripts/deploy --api --ws --telegram` (and other flags as needed).
10. Scout / data pipeline for first graph load (separate runbook).

## Verification (representative)

```text
$ cd backend && go build ./... && go vet ./...
(exit 0)

$ git grep -nE 'nv[-_]horizon|horde-horizon' -- ':!docs/archive/**' ':!temp-repos/**' ':!.git/**'
(no matches)

$ python3 -m json.tool web/.firebaserc && python3 -m json.tool web/firebase.json && python3 -m json.tool web/firestore.indexes.json
(json_ok)
```

`git grep sanskrite` excluding `github.com/igorkirisyuk/sanskrite` may still list **absolute paths** under `.claude/` and agent READMEs that reference the checkout directory name `sanskrite`; that is a **local path / tooling** artifact, not the GCP project ID. Removing those paths would churn unrelated Cursor config; left as known gap until the working directory or docs are normalized.

## Known gaps / follow-ups

- **Go module rename** (`github.com/igorkirisyuk/sanskrite` → future org module): separate PR.
- **Python package rename** (`agenthorde_*`): separate PR.
- **Firestore `ontology_claims` duplicate composite** (P15): micro-PR before first `firestore:indexes` deploy.
- **Root `.env.local` / `web/.env.local`**: gitignored; populate locally from this checklist (not committed).
- **Binary testdata** (`*.test`) may still match byte patterns for legacy strings; not scanned textually.
