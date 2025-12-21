#!/usr/bin/env bash
set -euo pipefail

# Backfill Horizon Fragments into a dedicated Vertex AI Search Data Store, in batches (<= 1000 files per import).
# This avoids Discovery Engine's file expansion limit when importing gs://.../** patterns.
#
# Defaults:
# - PROJECT: current gcloud project
# - BUCKET: ${PROJECT}-horizon-fragments
# - TENANT: global
# - DATA_STORE_ID: horizon-fragments-store
# - BATCH_SIZE: 500
#
# Usage:
#   scripts/backfill-horizon-fragments-vertex.sh
#   PROJECT=sanskrite BUCKET=sanskrite-horizon-fragments DATA_STORE_ID=horizon-fragments-store BATCH_SIZE=500 scripts/backfill-horizon-fragments-vertex.sh
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
BATCH_SIZE="${BATCH_SIZE:-500}"

if [[ "${BATCH_SIZE}" -gt 1000 ]]; then
  echo "BATCH_SIZE must be <= 1000 (Discovery Engine import limit). Got: ${BATCH_SIZE}" >&2
  exit 1
fi

ACCESS_TOKEN="$(gcloud auth print-access-token --project="$PROJECT" 2>/dev/null || true)"
if [[ -z "$ACCESS_TOKEN" ]]; then
  echo "Failed to get access token. Run: gcloud auth login" >&2
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

batch_idx=0
for batch_file in "$TMP_DIR"/batch_*; do
  batch_idx=$((batch_idx + 1))
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

  resp="$(curl -sS -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "X-Goog-User-Project: ${PROJECT}" \
    -H "Content-Type: application/json" \
    "${IMPORT_URL}" \
    --data-binary @"${req_json}")"

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


