# horizon_identify_stresses_smoke

Smoke tool for manual `identify-stresses` verification on a real Horizon lens.

What it does:
- resolves a target lens from Firestore or takes an explicit `-lens-id`
- builds a `visible_set` request from lens nodes, materialized supply-chain paths, supply links, and lens graph edges
- runs `DebugIdentify` first (`grounded -> normalize`) and saves diagnostics
- optionally runs the real apply flow (`identify-stresses` immediate graph/artifact updates) without propagation

Typical usage:

```bash
cd backend
go run ./cmd/tools/horizon_identify_stresses_smoke -project etigram -tenant all -list
go run ./cmd/tools/horizon_identify_stresses_smoke -project etigram -tenant 'igor@etigram.pro' -lens-id BakrpGUDgGR0wGF8tJXZ -apply=false
go run ./cmd/tools/horizon_identify_stresses_smoke -project etigram -tenant 'igor@etigram.pro' -lens-id BakrpGUDgGR0wGF8tJXZ -apply=true
```

Artifacts are written under `backend/tmp/identify_stresses_smoke_<lens>_<timestamp>/`:
- `request.json`
- `debug.json`
- `events.json`
- `summary.json`
