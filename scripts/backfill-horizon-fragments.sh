#!/usr/bin/env bash
# Wrapper script для запуска backfill после завершения re-export
# Использование: bash scripts/backfill-horizon-fragments.sh

set -euo pipefail

PROJECT="${PROJECT:-sanskrite}"
BUCKET="${BUCKET:-sanskrite-horizon-fragments}"
DATA_STORE_ID="${DATA_STORE_ID:-horizon-fragments-store}"
TENANT="${TENANT:-global}"
BATCH_SIZE="${BATCH_SIZE:-100}"

echo "=== Backfill Horizon Fragments to Vertex AI Search ==="
echo "Project: $PROJECT"
echo "Bucket: $BUCKET"
echo "Data Store: $DATA_STORE_ID"
echo "Tenant: $TENANT"
echo "Batch Size: $BATCH_SIZE"
echo ""

# Проверяем наличие файлов
FILE_COUNT=$(gcloud storage ls "gs://${BUCKET}/${TENANT}/horizon_fragments/*.json" 2>&1 | wc -l | tr -d ' ')
if [[ "${FILE_COUNT}" == "0" ]]; then
  echo "❌ No files found in gs://${BUCKET}/${TENANT}/horizon_fragments/"
  echo "   Make sure re-export has completed."
  exit 1
fi

echo "✅ Found ${FILE_COUNT} files to import"
echo ""

# Запускаем backfill
PROJECT="$PROJECT" BUCKET="$BUCKET" DATA_STORE_ID="$DATA_STORE_ID" TENANT="$TENANT" BATCH_SIZE="$BATCH_SIZE" \
  bash scripts/backfill-horizon-fragments-vertex.sh

echo ""
echo "✅ Backfill started. Import operations are async."
echo "   Check status via Discovery Engine API or wait for indexing to complete."

