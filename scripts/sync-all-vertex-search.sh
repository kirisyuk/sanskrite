#!/bin/bash
# Script to sync all documents from GCS to Vertex AI Search
# Processes all tenants and both buckets (core-docs and rag-documents)

set -e

PROJECT_ID="${GCP_PROJECT_ID:-$(gcloud config get-value project 2>/dev/null)}"
TENANT_ID="${TENANT_ID:-}"
WAIT_BETWEEN="${WAIT_BETWEEN:-5s}"

if [[ -z "$PROJECT_ID" ]]; then
  echo "Error: PROJECT_ID not set. Set GCP_PROJECT_ID env var or configure gcloud default project" >&2
  exit 1
fi

# Check required env vars
if [[ -z "$VERTEX_SEARCH_DATA_STORE_ID" ]]; then
  echo "Error: VERTEX_SEARCH_DATA_STORE_ID not set" >&2
  exit 1
fi

echo "=== Syncing all documents to Vertex AI Search ==="
echo "Project: $PROJECT_ID"
echo "DataStore: $VERTEX_SEARCH_DATA_STORE_ID"
echo "Location: ${VERTEX_SEARCH_LOCATION:-global}"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant: $TENANT_ID"
else
  echo "Tenant: ALL (will process all tenants)"
fi
echo "Wait between syncs: $WAIT_BETWEEN"

# Build command
CMD="go run backend/cmd/sync-all-vertex-search/main.go --project=$PROJECT_ID --wait=$WAIT_BETWEEN"

if [[ -n "$TENANT_ID" ]]; then
  CMD="$CMD --tenant=$TENANT_ID"
fi

echo ""
echo "=== STARTING SYNC ==="
$CMD

echo ""
echo "✓ Sync complete!"
echo "Note: Documents will be available in Vertex AI Search within 1-2 minutes"

