#!/usr/bin/env bash
set -euo pipefail

# Script to reindex CoreDoc documents that don't have corresponding Document in rag_documents
# Usage: ./scripts/reindex-docs.sh [tenantId] [projectId] [--dry-run]

TENANT_ID="${1:-}"
PROJECT_ID="${2:-}"
DRY_RUN="${3:---dry-run}"

DRY_RUN_FLAG="--dry-run"
if [[ "$DRY_RUN" == "--no-dry-run" ]]; then
  DRY_RUN_FLAG=""
elif [[ "$DRY_RUN" != "--dry-run" && -n "$DRY_RUN" ]]; then
  echo "Usage: $0 [tenantId] [projectId] [--dry-run|--no-dry-run]"
  echo "Example: $0 'kirisyuk@gmail.com' 'hJHJii9jcuuAFFa7BNJl' --no-dry-run"
  echo ""
  echo "If tenantId and projectId are not provided, script will try to auto-detect from first CoreDoc"
  exit 1
fi

GCP_PROJECT=$(gcloud config get-value project 2>/dev/null || echo "prevalent-horizon-test")
if [[ -z "$GCP_PROJECT" || "$GCP_PROJECT" == "(unset)" ]]; then
  echo "Error: GCP project is not set. Run: gcloud config set project <ID>"
  exit 1
fi

echo "Project: $GCP_PROJECT"
if [[ -n "$TENANT_ID" ]]; then
  echo "Tenant ID: $TENANT_ID"
fi
if [[ -n "$PROJECT_ID" ]]; then
  echo "Project ID: $PROJECT_ID"
fi
echo "Dry run: $([ -z "$DRY_RUN_FLAG" ] && echo "no" || echo "yes")"

cd "$(dirname "$0")/../backend"

# Export environment variables.
# Google Gemini generative calls use Vertex AI under ADC of the caller — no GEMINI_API_KEY.
export OPENAI_API_KEY="${OPENAI_API_KEY:-$(gcloud secrets versions access latest --secret="openai_api_key" --project="$GCP_PROJECT" 2>/dev/null || echo "")}"
export VERTEX_SEARCH_LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
export VERTEX_SEARCH_DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"
export RAG_DOCUMENTS_BUCKET="${RAG_DOCUMENTS_BUCKET:-prevalent-horizon-test-rag-documents}"

if [[ -z "$OPENAI_API_KEY" ]]; then
  echo "Error: OPENAI_API_KEY not set and could not retrieve from GCP Secret Manager"
  echo "Set it via: export OPENAI_API_KEY=your-key"
  exit 1
fi

# Build and run the reindex tool
ARGS=("--project" "$GCP_PROJECT")
if [[ -n "$TENANT_ID" ]]; then
  ARGS+=("--tenant" "$TENANT_ID")
fi
if [[ -n "$PROJECT_ID" ]]; then
  ARGS+=("--project-id" "$PROJECT_ID")
fi
if [[ -n "$DRY_RUN_FLAG" ]]; then
  ARGS+=("--dry-run")
fi

go run ./cmd/reindex-coredocs "${ARGS[@]}"

