#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-$(gcloud config get-value project 2>/dev/null || true)}"
REGION="${REGION:-us-central1}"
SERVICE="${SERVICE:-etigram-api}"
FRESHNESS="${FRESHNESS:-6h}"
LIMIT="${LIMIT:-50000}"
INPUT_LOG=""
BASELINE_JSON=""
OUTPUT_JSON=""
ENFORCE=0

MAX_READY_WITH_GAPS="${MAX_READY_WITH_GAPS:-0}"
MAX_FRESH_NO_NEW_FACTS="${MAX_FRESH_NO_NEW_FACTS:-0}"
MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS="${MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS:-0}"
MIN_SOFT_STOP_RECOVERED_RATE="${MIN_SOFT_STOP_RECOVERED_RATE:-0.80}"
MAX_DIRECTION_CONTRACT_QUARANTINE_RATE="${MAX_DIRECTION_CONTRACT_QUARANTINE_RATE:-}"

usage() {
  cat <<USAGE
Usage: scripts/horizon_shadow_replay.sh [options]

Options:
  --project <id>              GCP project (default: current gcloud project)
  --region <region>           Cloud Run region (default: us-central1)
  --service <name>            Cloud Run service (default: etigram-api)
  --freshness <dur>           gcloud log freshness (default: 6h)
  --limit <n>                 max log rows (default: 50000)
  --input-log <file>          use local log file instead of gcloud logging read
  --baseline-json <file>      baseline metrics JSON for delta gates
  --output-json <file>        write summary JSON report
  --enforce                   fail (exit 1) when any release gate fails

Gate thresholds (env):
  MAX_READY_WITH_GAPS (default: 0)
  MAX_FRESH_NO_NEW_FACTS (default: 0)
  MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS (default: 0)
  MIN_SOFT_STOP_RECOVERED_RATE (default: 0.80)
  MAX_DIRECTION_CONTRACT_QUARANTINE_RATE (optional absolute cap)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project) PROJECT_ID="$2"; shift 2 ;;
    --region) REGION="$2"; shift 2 ;;
    --service) SERVICE="$2"; shift 2 ;;
    --freshness) FRESHNESS="$2"; shift 2 ;;
    --limit) LIMIT="$2"; shift 2 ;;
    --input-log) INPUT_LOG="$2"; shift 2 ;;
    --baseline-json) BASELINE_JSON="$2"; shift 2 ;;
    --output-json) OUTPUT_JSON="$2"; shift 2 ;;
    --enforce) ENFORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$PROJECT_ID" ]]; then
  echo "PROJECT_ID is empty. Set --project or run 'gcloud config set project <id>'" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

count_literal() {
  local file="$1"
  local needle="$2"
  grep -Fic -- "$needle" "$file" || true
}

ratio() {
  local num="$1"
  local den="$2"
  awk -v n="$num" -v d="$den" 'BEGIN { if (d <= 0) { printf "0.000000" } else { printf "%.6f", n/d } }'
}

tmp_messages="$(mktemp)"
trap 'rm -f "$tmp_messages"' EXIT

if [[ -n "$INPUT_LOG" ]]; then
  if [[ ! -f "$INPUT_LOG" ]]; then
    echo "input log file not found: $INPUT_LOG" >&2
    exit 1
  fi
  cp "$INPUT_LOG" "$tmp_messages"
else
  filter="resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"${SERVICE}\""
  gcloud logging read "$filter" \
    --project "$PROJECT_ID" \
    --freshness "$FRESHNESS" \
    --limit "$LIMIT" \
    --format=json | jq -r '.[] | (.textPayload // .jsonPayload.message // .jsonPayload.msg // .protoPayload.status.message // "")' > "$tmp_messages"
fi

total_lines=$(wc -l < "$tmp_messages" | tr -d ' ')

quarantined_edges=$(count_literal "$tmp_messages" "Skipping quarantined edge")
direction_invalid=$(count_literal "$tmp_messages" "edge direction invalid and not reversible")
direction_role_ambiguous=$(count_literal "$tmp_messages" "requires explicit direction evidence")
source_planning_skips=$(count_literal "$tmp_messages" "skipping source planning")
no_source_content=$(count_literal "$tmp_messages" "no source content; using graph-backed internal context")
ready_with_gaps=$(count_literal "$tmp_messages" "Answer ready with gaps")
fresh_no_new_facts=$(count_literal "$tmp_messages" "fresh_no_new_facts")
research_skipped_no_searchable_gaps=$(count_literal "$tmp_messages" "research_skipped_no_searchable_gaps")
soft_stop_triggered=$(count_literal "$tmp_messages" "soft_stop_triggered")
soft_stop_recovered=$(count_literal "$tmp_messages" "soft_stop_recovered")
relationships_added=$(count_literal "$tmp_messages" "Relationship added")

edge_attempts=$((relationships_added + quarantined_edges))
direction_contract_quarantine_rate="$(ratio "$quarantined_edges" "$edge_attempts")"
if [[ "$soft_stop_triggered" -eq 0 ]]; then
  soft_stop_recovered_rate="1.000000"
else
  soft_stop_recovered_rate="$(ratio "$soft_stop_recovered" "$soft_stop_triggered")"
fi

# Optional baseline-based gate: candidate rate <= baseline * 0.5
baseline_direction_rate=""
if [[ -n "$BASELINE_JSON" ]]; then
  if [[ ! -f "$BASELINE_JSON" ]]; then
    echo "baseline file not found: $BASELINE_JSON" >&2
    exit 1
  fi
  baseline_direction_rate=$(jq -r '.direction_contract_quarantine_rate // empty' "$BASELINE_JSON")
  if [[ -n "$baseline_direction_rate" && "$baseline_direction_rate" != "null" ]]; then
    if [[ -z "$MAX_DIRECTION_CONTRACT_QUARANTINE_RATE" ]]; then
      MAX_DIRECTION_CONTRACT_QUARANTINE_RATE=$(awk -v b="$baseline_direction_rate" 'BEGIN { printf "%.6f", b * 0.5 }')
    fi
  fi
fi

# Gate evaluation
failures=()

if [[ "$ready_with_gaps" -gt "$MAX_READY_WITH_GAPS" ]]; then
  failures+=("ready_with_gaps=${ready_with_gaps} > ${MAX_READY_WITH_GAPS}")
fi

if [[ "$fresh_no_new_facts" -gt "$MAX_FRESH_NO_NEW_FACTS" ]]; then
  failures+=("fresh_no_new_facts=${fresh_no_new_facts} > ${MAX_FRESH_NO_NEW_FACTS}")
fi

if [[ "$research_skipped_no_searchable_gaps" -gt "$MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS" ]]; then
  failures+=("research_skipped_no_searchable_gaps=${research_skipped_no_searchable_gaps} > ${MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS}")
fi

if awk -v got="$soft_stop_recovered_rate" -v min="$MIN_SOFT_STOP_RECOVERED_RATE" 'BEGIN{exit !(got < min)}'; then
  failures+=("soft_stop_recovered_rate=${soft_stop_recovered_rate} < ${MIN_SOFT_STOP_RECOVERED_RATE}")
fi

if [[ -n "$MAX_DIRECTION_CONTRACT_QUARANTINE_RATE" ]]; then
  if awk -v got="$direction_contract_quarantine_rate" -v max="$MAX_DIRECTION_CONTRACT_QUARANTINE_RATE" 'BEGIN{exit !(got > max)}'; then
    failures+=("direction_contract_quarantine_rate=${direction_contract_quarantine_rate} > ${MAX_DIRECTION_CONTRACT_QUARANTINE_RATE}")
  fi
fi

status="pass"
if [[ ${#failures[@]} -gt 0 ]]; then
  status="fail"
fi

summary_json=$(jq -n \
  --arg project "$PROJECT_ID" \
  --arg region "$REGION" \
  --arg service "$SERVICE" \
  --arg freshness "$FRESHNESS" \
  --arg status "$status" \
  --arg baseline_direction_rate "$baseline_direction_rate" \
  --argjson total_lines "$total_lines" \
  --argjson quarantined_edges "$quarantined_edges" \
  --argjson direction_invalid "$direction_invalid" \
  --argjson direction_role_ambiguous "$direction_role_ambiguous" \
  --argjson source_planning_skips "$source_planning_skips" \
  --argjson no_source_content "$no_source_content" \
  --argjson ready_with_gaps "$ready_with_gaps" \
  --argjson fresh_no_new_facts "$fresh_no_new_facts" \
  --argjson research_skipped_no_searchable_gaps "$research_skipped_no_searchable_gaps" \
  --argjson soft_stop_triggered "$soft_stop_triggered" \
  --argjson soft_stop_recovered "$soft_stop_recovered" \
  --argjson relationships_added "$relationships_added" \
  --argjson edge_attempts "$edge_attempts" \
  --arg direction_contract_quarantine_rate "$direction_contract_quarantine_rate" \
  --arg soft_stop_recovered_rate "$soft_stop_recovered_rate" \
  --arg max_direction_contract_quarantine_rate "$MAX_DIRECTION_CONTRACT_QUARANTINE_RATE" \
  --arg max_ready_with_gaps "$MAX_READY_WITH_GAPS" \
  --arg min_soft_stop_recovered_rate "$MIN_SOFT_STOP_RECOVERED_RATE" \
  --arg max_fresh_no_new_facts "$MAX_FRESH_NO_NEW_FACTS" \
  --arg max_research_skipped_no_searchable_gaps "$MAX_RESEARCH_SKIPPED_NO_SEARCHABLE_GAPS" \
  --argjson failures "$(printf '%s\n' "${failures[@]-}" | jq -R . | jq -s '. | map(select(length > 0))')" \
  '{
    project: $project,
    region: $region,
    service: $service,
    freshness: $freshness,
    status: $status,
    metrics: {
      total_lines: $total_lines,
      quarantined_edges: $quarantined_edges,
      direction_invalid: $direction_invalid,
      direction_role_ambiguous: $direction_role_ambiguous,
      source_planning_skips: $source_planning_skips,
      no_source_content: $no_source_content,
      ready_with_gaps: $ready_with_gaps,
      fresh_no_new_facts: $fresh_no_new_facts,
      research_skipped_no_searchable_gaps: $research_skipped_no_searchable_gaps,
      soft_stop_triggered: $soft_stop_triggered,
      soft_stop_recovered: $soft_stop_recovered,
      relationships_added: $relationships_added,
      edge_attempts: $edge_attempts,
      direction_contract_quarantine_rate: ($direction_contract_quarantine_rate | tonumber),
      soft_stop_recovered_rate: ($soft_stop_recovered_rate | tonumber)
    },
    gates: {
      max_ready_with_gaps: ($max_ready_with_gaps | tonumber),
      max_fresh_no_new_facts: ($max_fresh_no_new_facts | tonumber),
      max_research_skipped_no_searchable_gaps: ($max_research_skipped_no_searchable_gaps | tonumber),
      min_soft_stop_recovered_rate: ($min_soft_stop_recovered_rate | tonumber),
      max_direction_contract_quarantine_rate: (if $max_direction_contract_quarantine_rate == "" then null else ($max_direction_contract_quarantine_rate | tonumber) end),
      baseline_direction_contract_quarantine_rate: (if $baseline_direction_rate == "" then null else ($baseline_direction_rate | tonumber) end)
    },
    failures: $failures
  }')

echo "[shadow-replay] service=${SERVICE} freshness=${FRESHNESS} status=${status}"
echo "[shadow-replay] quarantined_edges=${quarantined_edges} edge_attempts=${edge_attempts} direction_contract_quarantine_rate=${direction_contract_quarantine_rate}"
echo "[shadow-replay] ready_with_gaps=${ready_with_gaps} fresh_no_new_facts=${fresh_no_new_facts} research_skipped_no_searchable_gaps=${research_skipped_no_searchable_gaps}"
echo "[shadow-replay] soft_stop_triggered=${soft_stop_triggered} soft_stop_recovered=${soft_stop_recovered} soft_stop_recovered_rate=${soft_stop_recovered_rate}"
if [[ ${#failures[@]} -gt 0 ]]; then
  for f in "${failures[@]}"; do
    echo "[shadow-replay][gate-fail] ${f}" >&2
  done
fi

if [[ -n "$OUTPUT_JSON" ]]; then
  echo "$summary_json" > "$OUTPUT_JSON"
  echo "[shadow-replay] wrote report: $OUTPUT_JSON"
fi

if [[ "$ENFORCE" -eq 1 && "$status" != "pass" ]]; then
  exit 1
fi

exit 0
