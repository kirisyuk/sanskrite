#!/bin/bash
# Script to update GCS metadata for all documents (add tenantId and projectId)
# This ensures all existing documents have proper metadata for Vertex AI Search filtering

set -e

PROJECT_ID="${GCP_PROJECT_ID:-$(gcloud config get-value project 2>/dev/null)}"
TENANT_ID="${TENANT_ID:-}"

if [[ -z "$PROJECT_ID" ]]; then
  echo "Error: PROJECT_ID not set. Set GCP_PROJECT_ID env var or configure gcloud default project" >&2
  exit 1
fi

echo "=== Updating GCS Metadata for Vertex AI Search ==="
echo "Project: $PROJECT_ID"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant: $TENANT_ID"
else
  echo "Tenant: ALL (will process all tenants)"
fi

# Build command
CMD="go run backend/cmd/update-gcs-metadata/main.go --project=$PROJECT_ID"

if [[ -n "$TENANT_ID" ]]; then
  CMD="$CMD --tenant=$TENANT_ID"
fi

# Dry run first to see what will be updated
echo ""
echo "=== DRY RUN ==="
$CMD --dry-run

echo ""
read -p "Continue with actual update? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Cancelled"
  exit 0
fi

echo ""
echo "=== UPDATING METADATA ==="
$CMD

echo ""
echo "✓ Metadata update complete!"
echo "Note: Run sync-all-vertex-search.sh to sync updated documents to Vertex AI Search"



