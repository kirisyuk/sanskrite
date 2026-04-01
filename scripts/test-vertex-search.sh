#!/usr/bin/env bash
set -euo pipefail

# Скрипт для проверки и тестирования Vertex AI Search
# Использование: ./scripts/test-vertex-search.sh [-p|--project nv-horizon]

PROJECT="nv-horizon"
DATA_STORE_ID="rag-documents-store"
LOCATION="global"
BUCKET="nv-horizon-rag-documents"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--project) PROJECT="$2"; shift 2;;
    *) echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

echo "Testing Vertex AI Search for project: $PROJECT"
echo "Data Store: $DATA_STORE_ID"
echo "Location: $LOCATION"
echo ""

# Получаем access token
ACCESS_TOKEN=$(gcloud auth print-access-token --project="$PROJECT" 2>/dev/null || echo "")
if [[ -z "$ACCESS_TOKEN" ]]; then
  echo "❌ Failed to get access token. Run: gcloud auth login"
  exit 1
fi

# 1. Проверка Data Store
echo "1. Checking Data Store status..."
DATA_STORE_INFO=$(curl -s -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "X-Goog-User-Project: ${PROJECT}" \
  "https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}" 2>/dev/null)

if echo "$DATA_STORE_INFO" | grep -q "\"name\""; then
  echo "  ✅ Data Store exists"
  DISPLAY_NAME=$(echo "$DATA_STORE_INFO" | python3 -c "import sys, json; print(json.load(sys.stdin).get('displayName', 'N/A'))" 2>/dev/null || echo "N/A")
  CREATE_TIME=$(echo "$DATA_STORE_INFO" | python3 -c "import sys, json; print(json.load(sys.stdin).get('createTime', 'N/A'))" 2>/dev/null || echo "N/A")
  echo "     Display Name: $DISPLAY_NAME"
  echo "     Created: $CREATE_TIME"
else
  echo "  ❌ Data Store not found"
  exit 1
fi

echo ""

# 2. Проверка количества документов в Data Store
echo "2. Checking indexed documents..."
DOC_COUNT=$(curl -s -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "X-Goog-User-Project: ${PROJECT}" \
  "https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}/branches/default_branch/documents?pageSize=1" 2>/dev/null | \
  python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('totalSize', 0))" 2>/dev/null || echo "0")

if [[ "$DOC_COUNT" -gt 0 ]]; then
  echo "  ✅ Found $DOC_COUNT indexed documents"
else
  echo "  ⚠️  No documents indexed yet (found: $DOC_COUNT)"
  echo "     Documents need to be imported from GCS bucket: gs://${BUCKET}"
fi

echo ""

# 3. Проверка документов в GCS
echo "3. Checking documents in GCS bucket..."
GCS_COUNT=$(gsutil ls -r "gs://${BUCKET}/**/*.txt" 2>/dev/null | grep -c "\.txt$" || echo "0")
if [[ "$GCS_COUNT" -gt 0 ]]; then
  echo "  ✅ Found $GCS_COUNT .txt files in GCS bucket"
  echo "     Sample files:"
  gsutil ls -r "gs://${BUCKET}/**/*.txt" 2>/dev/null | head -5 | sed 's/^/       /'
else
  echo "  ⚠️  No .txt files found in GCS bucket"
fi

echo ""

# 4. Проверка статуса импорта (если есть операции импорта)
echo "4. Checking import operations..."
IMPORT_OPS=$(curl -s -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "X-Goog-User-Project: ${PROJECT}" \
  "https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}/operations?filter=metadata.operationType=IMPORT" 2>/dev/null | \
  python3 -c "import sys, json; data=json.load(sys.stdin); ops=data.get('operations', []); print(len(ops))" 2>/dev/null || echo "0")

if [[ "$IMPORT_OPS" -gt 0 ]]; then
  echo "  ✅ Found $IMPORT_OPS import operation(s)"
else
  echo "  ⚠️  No import operations found"
  echo "     Documents may need to be imported manually"
fi

echo ""

# 5. Тестовый поиск (если есть документы)
if [[ "$DOC_COUNT" -gt 0 ]]; then
  echo "5. Testing search query..."
  TEST_QUERY="test"
  SEARCH_RESPONSE=$(curl -s -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "X-Goog-User-Project: ${PROJECT}" \
    -H "Content-Type: application/json" \
    "https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}/servingConfigs/default_search:search" \
    -d "{\"query\": \"${TEST_QUERY}\", \"pageSize\": 5}" 2>/dev/null)
  
  if echo "$SEARCH_RESPONSE" | grep -q "\"results\""; then
    RESULT_COUNT=$(echo "$SEARCH_RESPONSE" | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data.get('results', [])))" 2>/dev/null || echo "0")
    echo "  ✅ Search query executed successfully"
    echo "     Query: '$TEST_QUERY'"
    echo "     Results: $RESULT_COUNT"
    if [[ "$RESULT_COUNT" -gt 0 ]]; then
      echo "     Sample result titles:"
      echo "$SEARCH_RESPONSE" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    for i, r in enumerate(data.get('results', [])[:3], 1):
        doc = r.get('document', {})
        title = doc.get('title', 'No title')
        print(f'       {i}. {title}')
except:
    pass
" 2>/dev/null || echo "       (Could not parse results)"
    fi
  else
    ERROR_MSG=$(echo "$SEARCH_RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('error', {}).get('message', 'Unknown error'))" 2>/dev/null || echo "Unknown error")
    echo "  ⚠️  Search query failed: $ERROR_MSG"
  fi
else
  echo "5. Skipping search test (no documents indexed)"
fi

echo ""

# 6. Рекомендации
echo "6. Recommendations:"
if [[ "$DOC_COUNT" -eq 0 ]] && [[ "$GCS_COUNT" -gt 0 ]]; then
  echo "  📋 To import documents from GCS to Vertex AI Search:"
  echo "     1. Go to: https://console.cloud.google.com/gen-app-builder/data-stores"
  echo "     2. Select Data Store: $DATA_STORE_ID"
  echo "     3. Click 'Import' and select GCS source: gs://${BUCKET}/**"
  echo ""
  echo "     Or use API:"
  echo "     curl -X POST \\"
  echo "       -H \"Authorization: Bearer \$(gcloud auth print-access-token)\" \\"
  echo "       -H \"Content-Type: application/json\" \\"
  echo "       \"https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}/branches/default_branch/documents:import\" \\"
  echo "       -d '{\"gcsSource\": {\"inputUris\": [\"gs://${BUCKET}/**\"], \"dataSchema\": \"document\"}}'"
fi

if [[ "$DOC_COUNT" -gt 0 ]]; then
  echo "  ✅ Vertex AI Search is working!"
  echo "     You can test it via API endpoint: POST /api/v1/projects/{id}/vertex-search"
fi

echo ""
echo "Done."

