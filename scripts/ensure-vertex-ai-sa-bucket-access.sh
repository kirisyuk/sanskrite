#!/usr/bin/env bash
# Grant Vertex AI managed SA access to RAG bucket.
# The gcp-sa-aiplatform SA is provisioned by GCP on first Vertex AI use.
# Run this after deploy if the grant failed, or after first Vertex AI API call.
set -euo pipefail

PROJECT_ID="${GCP_PROJECT_ID:-$(gcloud config get-value project 2>/dev/null || true)}"
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && {
  echo "Set GCP project: gcloud config set project <ID> or GCP_PROJECT_ID=..."
  exit 1
}

RAG_BUCKET="${RAG_DOCUMENTS_BUCKET:-${PROJECT_ID}-rag-documents}"
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format="value(projectNumber)" 2>/dev/null || true)
[[ -z "$PROJECT_NUMBER" ]] && { echo "Could not get project number"; exit 1; }

VERTEX_AI_SA="service-${PROJECT_NUMBER}@gcp-sa-aiplatform.iam.gserviceaccount.com"
echo "Project: $PROJECT_ID | Bucket: gs://$RAG_BUCKET | SA: $VERTEX_AI_SA"

if gcloud storage buckets add-iam-policy-binding "gs://${RAG_BUCKET}" \
  --member="serviceAccount:${VERTEX_AI_SA}" \
  --role="roles/storage.objectViewer" \
  --project="$PROJECT_ID"; then
  echo "✓ Vertex AI SA granted access to gs://${RAG_BUCKET}"
else
  echo "✗ Failed. The Vertex AI SA is provisioned on first Vertex AI use."
  echo "  Try: make a Vertex AI API call (e.g. list models), wait ~1 min, then re-run this script."
  exit 1
fi
