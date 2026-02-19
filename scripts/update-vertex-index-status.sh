#!/usr/bin/env bash
set -euo pipefail

# Script to update VertexSearchIndexedAt for existing documents
# Usage: ./scripts/update-vertex-index-status.sh [tenantId] [--force-sync]

TENANT_ID="${1:-}"
FORCE_SYNC="${2:-}"

GCP_PROJECT=$(gcloud config get-value project 2>/dev/null || echo "h1horizon")
if [[ -z "$GCP_PROJECT" || "$GCP_PROJECT" == "(unset)" ]]; then
  echo "Error: GCP project is not set. Run: gcloud config set project <ID>"
  exit 1
fi

echo "Project: $GCP_PROJECT"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant ID: $TENANT_ID"
fi
if [[ "$FORCE_SYNC" == "--force-sync" ]]; then
  echo "Mode: Force re-sync with Vertex AI Search"
else
  echo "Mode: Mark existing documents as indexed (one-time migration)"
fi

cd "$(dirname "$0")/../backend"

# Export environment variables
export VERTEX_SEARCH_LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
export VERTEX_SEARCH_DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"
export RAG_DOCUMENTS_BUCKET="${RAG_DOCUMENTS_BUCKET:-h1horizon-rag-documents}"

# Build and run the update tool
ARGS=("--project" "$GCP_PROJECT")
if [[ -n "$TENANT_ID" ]]; then
  ARGS+=("--tenant" "$TENANT_ID")
fi
if [[ "$FORCE_SYNC" == "--force-sync" ]]; then
  ARGS+=("--force-sync")
fi

go run ./cmd/update-vertex-index-status "${ARGS[@]}"

