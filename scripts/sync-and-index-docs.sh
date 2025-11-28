#!/bin/bash
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ID=${GCP_PROJECT_ID:-$(gcloud config get-value project 2>/dev/null || true)}
if [[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]]; then
  echo -e "${RED}GCP project is not set. Set GCP_PROJECT_ID env var or run: gcloud config set project <ID>${NC}" >&2
  exit 1
fi

TENANT_ID=${1:-""}

echo -e "${BLUE}=== Syncing CoreDocs to BigQuery ===${NC}"
echo "Project: $PROJECT_ID"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant: $TENANT_ID"
fi
echo ""

# Build sync script
echo -e "${BLUE}Building sync-coredocs-bigquery...${NC}"
cd backend
go build -o ../bin/sync-coredocs-bigquery ./cmd/sync-coredocs-bigquery
cd ..

# Run sync
echo -e "${BLUE}Running BigQuery sync...${NC}"
if [[ -n "$TENANT_ID" ]]; then
  ./bin/sync-coredocs-bigquery -project="$PROJECT_ID" -tenant="$TENANT_ID"
else
  ./bin/sync-coredocs-bigquery -project="$PROJECT_ID"
fi

echo ""
echo -e "${BLUE}=== Indexing CoreDocs for Vertex AI Search ===${NC}"

# Build reindex script
echo -e "${BLUE}Building reindex-all-coredocs...${NC}"
cd backend
go build -o ../bin/reindex-all-coredocs ./cmd/reindex-all-coredocs
cd ..

# Run reindex
echo -e "${BLUE}Running reindexing...${NC}"
if [[ -n "$TENANT_ID" ]]; then
  ./bin/reindex-all-coredocs -project="$PROJECT_ID" -tenant="$TENANT_ID"
else
  ./bin/reindex-all-coredocs -project="$PROJECT_ID"
fi

echo ""
echo -e "${GREEN}=== Complete ===${NC}"

