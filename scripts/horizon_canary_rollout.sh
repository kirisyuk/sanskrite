#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-$(gcloud config get-value project 2>/dev/null || true)}"
REGION="${REGION:-us-central1}"
SERVICE="${SERVICE:-nv-horizon-api}"
CANDIDATE_REVISION=""
STABLE_REVISION=""
FRESHNESS="${FRESHNESS:-30m}"
LIMIT="${LIMIT:-50000}"
HOLD_MINUTES="${HOLD_MINUTES:-10}"
BASELINE_JSON=""
REPORT_DIR="${REPORT_DIR:-/tmp/horizon-canary-reports}"
DRY_RUN=0

usage() {
  cat <<USAGE
Usage: scripts/horizon_canary_rollout.sh --candidate-revision <rev> [options]

Options:
  --candidate-revision <rev>   candidate Cloud Run revision (required)
  --stable-revision <rev>      stable Cloud Run revision (default: current dominant traffic revision)
  --project <id>               GCP project (default: current gcloud project)
  --region <region>            Cloud Run region (default: us-central1)
  --service <name>             Cloud Run service (default: nv-horizon-api)
  --freshness <dur>            freshness window for gate checks (default: 30m)
  --limit <n>                  max rows for gate checks (default: 50000)
  --hold-minutes <n>           wait time between phases (default: 10)
  --baseline-json <file>       baseline metrics JSON for direction quarantine gate
  --report-dir <dir>           where phase reports are written
  --dry-run                    print actions without applying traffic changes

The script performs canary rollout phases: 10% -> 50% -> 100%.
After each phase it runs scripts/horizon_shadow_replay.sh --enforce.
On gate failure, it auto-rolls back to stable revision (100%).
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate-revision) CANDIDATE_REVISION="$2"; shift 2 ;;
    --stable-revision) STABLE_REVISION="$2"; shift 2 ;;
    --project) PROJECT_ID="$2"; shift 2 ;;
    --region) REGION="$2"; shift 2 ;;
    --service) SERVICE="$2"; shift 2 ;;
    --freshness) FRESHNESS="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --hold-minutes) HOLD_MINUTES="$2"; shift 2 ;;
    --baseline-json) BASELINE_JSON="$2"; shift 2 ;;
    --report-dir) REPORT_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$PROJECT_ID" ]]; then
  echo "PROJECT_ID is empty. Set --project or run 'gcloud config set project <id>'" >&2
  exit 1
fi

if [[ -z "$CANDIDATE_REVISION" ]]; then
  echo "--candidate-revision is required" >&2
  usage
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

if [[ -z "$STABLE_REVISION" ]]; then
  STABLE_REVISION=$(gcloud run services describe "$SERVICE" \
    --project "$PROJECT_ID" \
    --region "$REGION" \
    --format=json | jq -r '.status.traffic | max_by(.percent // 0) | .revisionName // empty')
fi

if [[ -z "$STABLE_REVISION" ]]; then
  echo "cannot determine stable revision for ${SERVICE}" >&2
  exit 1
fi

if [[ "$STABLE_REVISION" == "$CANDIDATE_REVISION" ]]; then
  echo "candidate revision equals stable revision (${CANDIDATE_REVISION}); nothing to rollout"
  exit 0
fi

mkdir -p "$REPORT_DIR"

set_traffic() {
  local candidate_pct="$1"
  local stable_pct=$((100 - candidate_pct))
  local spec="${CANDIDATE_REVISION}=${candidate_pct},${STABLE_REVISION}=${stable_pct}"
  if [[ "$candidate_pct" -eq 100 ]]; then
    spec="${CANDIDATE_REVISION}=100"
  fi

  echo "[canary] set traffic: ${spec}"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi

  gcloud run services update-traffic "$SERVICE" \
    --project "$PROJECT_ID" \
    --region "$REGION" \
    --to-revisions "$spec" \
    --quiet
}

rollback() {
  echo "[canary] rollback to stable revision ${STABLE_REVISION}=100"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi

  gcloud run services update-traffic "$SERVICE" \
    --project "$PROJECT_ID" \
    --region "$REGION" \
    --to-revisions "${STABLE_REVISION}=100" \
    --quiet
}

run_phase_gate() {
  local phase="$1"
  local report_file="${REPORT_DIR}/phase-${phase}.json"

  echo "[canary] running gate checks for phase=${phase}%"
  local cmd=(
    scripts/horizon_shadow_replay.sh
    --project "$PROJECT_ID"
    --region "$REGION"
    --service "$SERVICE"
    --freshness "$FRESHNESS"
    --limit "$LIMIT"
    --output-json "$report_file"
    --enforce
  )
  if [[ -n "$BASELINE_JSON" ]]; then
    cmd+=(--baseline-json "$BASELINE_JSON")
  fi
  "${cmd[@]}"

  echo "[canary] phase=${phase}% gates passed; report=${report_file}"
}

for phase in 10 50 100; do
  set_traffic "$phase"

  if [[ "$DRY_RUN" -eq 0 ]]; then
    echo "[canary] hold ${HOLD_MINUTES}m before gate evaluation"
    sleep "$((HOLD_MINUTES * 60))"
  fi

  if ! run_phase_gate "$phase"; then
    echo "[canary] gate failed on phase=${phase}%" >&2
    rollback
    exit 1
  fi
done

echo "[canary] rollout completed: ${CANDIDATE_REVISION}=100"
exit 0
