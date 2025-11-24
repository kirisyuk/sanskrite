#!/usr/bin/env bash
set -euo pipefail

# Script to sync documents with Vertex AI Search
# Usage: ./scripts/sync-vertex-search.sh [tenantId]

TENANT_ID="${1:-}"

if [[ -z "$TENANT_ID" ]]; then
  echo "Usage: $0 <tenantId>"
  echo "Example: $0 'kirisyuk@gmail.com'"
  exit 1
fi

GCP_PROJECT=$(gcloud config get-value project 2>/dev/null || echo "sanskrite")
if [[ -z "$GCP_PROJECT" || "$GCP_PROJECT" == "(unset)" ]]; then
  echo "Error: GCP project is not set. Run: gcloud config set project <ID>"
  exit 1
fi

echo "Project: $GCP_PROJECT"
echo "Tenant ID: $TENANT_ID"

cd "$(dirname "$0")/../backend"

# Export environment variables
# Note: GEMINI_API_KEY may be set for other services, but Vertex AI Search will use OAuth2
export VERTEX_SEARCH_LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
export VERTEX_SEARCH_DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"
export RAG_DOCUMENTS_BUCKET="${RAG_DOCUMENTS_BUCKET:-sanskrite-rag-documents}"

# Use Application Default Credentials (OAuth2) for Vertex AI Search authentication
echo "Using Application Default Credentials (OAuth2) for Vertex AI Search authentication"

# Run sync
go run ./cmd/sync-vertex-search --project "$GCP_PROJECT" --tenant "$TENANT_ID"

