#!/usr/bin/env bash
set -euo pipefail

# Script to check CoreDoc -> Document -> Vertex AI Search linkage
# Usage: ./scripts/check-docs.sh "docId1,docId2,docId3" [tenantId]

DOC_IDS="${1:-}"
TENANT_ID="${2:-}"

if [[ -z "$DOC_IDS" ]]; then
  echo "Usage: $0 <docIds> [tenantId]"
  echo "Example: $0 'oP6K82ZCwpavD9U8WPp0,XECxce0hc09JxzzA6eq2' 'NYMI5KwzcRPhdIu1ZPHWWqOBSbf2'"
  exit 1
fi

PROJECT_ID=$(gcloud config get-value project 2>/dev/null || echo "h1horizon-488615")
if [[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]]; then
  echo "Error: GCP project is not set. Run: gcloud config set project <ID>"
  exit 1
fi

echo "Project: $PROJECT_ID"
echo "Checking documents: $DOC_IDS"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant ID: $TENANT_ID"
fi

cd "$(dirname "$0")/../backend"

# Export environment variables for Vertex AI Search
# Vertex AI Search uses OAuth2 (Application Default Credentials), not API key
# Default values (can be overridden by environment variables)
export VERTEX_SEARCH_LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
export VERTEX_SEARCH_DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"

# Build and run the check-docs tool
go run ./cmd/check-docs \
  --docs "$DOC_IDS" \
  --tenant "${TENANT_ID:-}" \
  --project "$PROJECT_ID"

