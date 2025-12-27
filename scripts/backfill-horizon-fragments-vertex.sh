#!/usr/bin/env bash
set -euo pipefail

# Backfill Horizon Fragments into a dedicated Vertex AI Search Data Store, in batches (<= 100 files per import).
# This avoids Discovery Engine's file expansion limit when importing gs://.../** patterns.
# Discovery Engine import ограничивает gcsSource.inputUris максимум 100 значений.
#
# Defaults:
# - PROJECT: current gcloud project
# - BUCKET: ${PROJECT}-horizon-fragments
# - TENANT: global
# - DATA_STORE_ID: horizon-fragments-store
# - BATCH_SIZE: 100 (максимум для Discovery Engine)
#
# Usage:
#   scripts/backfill-horizon-fragments-vertex.sh
#   PROJECT=sanskrite BUCKET=sanskrite-horizon-fragments DATA_STORE_ID=horizon-fragments-store TENANT=global BATCH_SIZE=100 scripts/backfill-horizon-fragments-vertex.sh
#

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require gcloud
require curl
require split

PROJECT="${PROJECT:-$(gcloud config get-value project 2>/dev/null || true)}"
if [[ -z "${PROJECT}" || "${PROJECT}" == "(unset)" ]]; then
  echo "GCP project is not set. Run: gcloud config set project <ID>" >&2
  exit 1
fi

TENANT="${TENANT:-global}"
BUCKET="${BUCKET:-${PROJECT}-horizon-fragments}"
DATA_STORE_ID="${DATA_STORE_ID:-horizon-fragments-store}"
BATCH_SIZE="${BATCH_SIZE:-100}"
START_BATCH="${START_BATCH:-1}" # 1-based, inclusive
END_BATCH="${END_BATCH:-0}"     # 0 = no limit

# Discovery Engine import ограничивает gcsSource.inputUris максимум 100 значений
if [[ "${BATCH_SIZE}" -gt 100 ]]; then
  echo "BATCH_SIZE must be <= 100 (Discovery Engine import limit for inputUris). Got: ${BATCH_SIZE}" >&2
  exit 1
fi

if ! [[ "${START_BATCH}" =~ ^[0-9]+$ ]] || [[ "${START_BATCH}" -lt 1 ]]; then
  echo "START_BATCH must be a positive integer (1-based). Got: ${START_BATCH}" >&2
  exit 1
fi
if ! [[ "${END_BATCH}" =~ ^[0-9]+$ ]] || [[ "${END_BATCH}" -lt 0 ]]; then
  echo "END_BATCH must be an integer >= 0 (0 = no limit). Got: ${END_BATCH}" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

LIST_PATH="gs://${BUCKET}/${TENANT}/horizon_fragments/*.json"
echo "[backfill] Project=$PROJECT"
echo "[backfill] Bucket=$BUCKET"
echo "[backfill] Tenant=$TENANT"
echo "[backfill] DataStore=$DATA_STORE_ID"
echo "[backfill] BatchSize=$BATCH_SIZE"
echo "[backfill] StartBatch=$START_BATCH EndBatch=$END_BATCH"
echo "[backfill] Listing: $LIST_PATH"

gcloud storage ls "$LIST_PATH" > "$TMP_DIR/uris.txt" || true

TOTAL="$(wc -l < "$TMP_DIR/uris.txt" | tr -d ' ')"
if [[ "${TOTAL}" == "0" ]]; then
  echo "[backfill] No files found at: $LIST_PATH" >&2
  exit 1
fi
echo "[backfill] Found ${TOTAL} files"

split -l "$BATCH_SIZE" -d -a 4 "$TMP_DIR/uris.txt" "$TMP_DIR/batch_"

IMPORT_URL="https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/global/collections/default_collection/dataStores/${DATA_STORE_ID}/branches/default_branch/documents:import"

get_access_token() {
  # Refresh token per batch to avoid mid-run expiry/reauth issues.
  gcloud auth print-access-token --project="$PROJECT" 2>/dev/null || true
}

batch_idx=0
for batch_file in "$TMP_DIR"/batch_*; do
  batch_idx=$((batch_idx + 1))
  if [[ "${batch_idx}" -lt "${START_BATCH}" ]]; then
    continue
  fi
  if [[ "${END_BATCH}" -gt 0 && "${batch_idx}" -gt "${END_BATCH}" ]]; then
    break
  fi
  req_json="$TMP_DIR/req_${batch_idx}.json"

  echo -n "{\"gcsSource\":{\"inputUris\":[" > "$req_json"
  first=1
  while IFS= read -r uri; do
    uri="$(echo "$uri" | tr -d '\r' | xargs || true)"
    [[ -z "$uri" ]] && continue
    if [[ "$first" -eq 1 ]]; then
      first=0
    else
      echo -n "," >> "$req_json"
    fi
    # URIs are plain gs://... paths, safe to quote directly.
    printf "\"%s\"" "$uri" >> "$req_json"
  done < "$batch_file"
  echo -n "],\"dataSchema\":\"document\"},\"reconciliationMode\":\"INCREMENTAL\"}" >> "$req_json"

  count_in_batch="$(wc -l < "$batch_file" | tr -d ' ')"
  echo "[backfill] Importing batch ${batch_idx} (files=${count_in_batch})..."

  ACCESS_TOKEN="$(get_access_token)"
  if [[ -z "$ACCESS_TOKEN" ]]; then
    echo "[backfill] Failed to get access token. Run: gcloud auth login" >&2
    exit 1
  fi

  resp="$(curl -sS -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "X-Goog-User-Project: ${PROJECT}" \
    -H "Content-Type: application/json" \
    "${IMPORT_URL}" \
    --data-binary @"${req_json}")"

  # If token expired mid-run, refresh once and retry.
  if echo "$resp" | grep -q "\"status\"[ ]*:[ ]*\"UNAUTHENTICATED\""; then
    ACCESS_TOKEN="$(get_access_token)"
    if [[ -n "$ACCESS_TOKEN" ]]; then
      resp="$(curl -sS -X POST \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -H "X-Goog-User-Project: ${PROJECT}" \
        -H "Content-Type: application/json" \
        "${IMPORT_URL}" \
        --data-binary @"${req_json}")"
    fi
  fi

  if echo "$resp" | grep -q "\"name\""; then
    op="$(echo "$resp" | sed -n 's/.*"name"[ ]*:[ ]*"\([^"]*\)".*/\1/p' | head -n 1)"
    echo "[backfill] ✅ Started import operation: ${op:-<unknown>}"
  else
    echo "[backfill] ❌ Import failed for batch ${batch_idx}. Response:" >&2
    echo "$resp" >&2
    exit 1
  fi
done

echo "[backfill] Done. Imports are async; indexing may take a few minutes."





