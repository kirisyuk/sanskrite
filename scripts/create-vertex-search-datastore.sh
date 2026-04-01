#!/bin/bash
# Создание Vertex AI Search Data Store для Horizon (Nordic Vision)

set -e

PROJECT_ID=$(gcloud config get-value project)
if [ -z "$PROJECT_ID" ]; then
  echo "ERROR: GCP project not set. Run: gcloud config set project PROJECT_ID"
  exit 1
fi

DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"
LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
DISPLAY_NAME="${VERTEX_SEARCH_DATA_STORE_DISPLAY_NAME:-}"
if [ -z "$DISPLAY_NAME" ]; then
  if [ "$DATA_STORE_ID" = "nv-horizon-fragments-store" ]; then
    DISPLAY_NAME="Horizon Fragments Store"
  else
    DISPLAY_NAME="RAG Documents Store"
  fi
fi

echo "=== Создание Vertex AI Search Data Store ==="
echo "Проект: $PROJECT_ID"
echo "Data Store ID: $DATA_STORE_ID"
echo "Location: $LOCATION"
echo "Display Name: $DISPLAY_NAME"
echo ""

# Проверка существования (через REST API, т.к. gcloud discovery-engine может быть недоступен)
ACCESS_TOKEN="$(gcloud auth print-access-token 2>/dev/null || true)"
if [ -z "$ACCESS_TOKEN" ]; then
  echo "ERROR: Не удалось получить access token (gcloud auth print-access-token)."
  exit 1
fi

RESP_FILE="$(mktemp)"
HTTP_CODE="$(
  curl -sS -o "$RESP_FILE" -w "%{http_code}" \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "x-goog-user-project: $PROJECT_ID" \
    "https://discoveryengine.googleapis.com/v1beta/projects/$PROJECT_ID/locations/$LOCATION/collections/default_collection/dataStores/$DATA_STORE_ID" \
    2>/dev/null || echo "000"
)"
if [ "$HTTP_CODE" = "200" ]; then
  echo "✓ Data Store уже существует: $DATA_STORE_ID"
  exit 0
fi

# Проверка API
if ! gcloud services list --enabled --filter="name:discoveryengine.googleapis.com" --format="value(name)" 2>/dev/null | grep -q discoveryengine; then
  echo "Включение Discovery Engine API..."
  gcloud services enable discoveryengine.googleapis.com
fi

echo "Создание Data Store..."
echo "Это может занять несколько минут..."

curl -X POST \
  "https://discoveryengine.googleapis.com/v1beta/projects/$PROJECT_ID/locations/$LOCATION/collections/default_collection/dataStores?dataStoreId=$DATA_STORE_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "x-goog-user-project: $PROJECT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "displayName": "'"$DISPLAY_NAME"'",
    "industryVertical": "GENERIC",
    "solutionTypes": ["SOLUTION_TYPE_SEARCH"],
    "contentConfig": "CONTENT_REQUIRED"
  }' || {
    echo ""
    echo "⚠ Ошибка создания через API. Попробуйте создать через консоль GCP:"
    echo "   https://console.cloud.google.com/gen-app-builder/data-stores"
    echo ""
    echo "Или используйте gcloud (если доступно в вашей версии):"
    echo "   gcloud alpha discovery-engine data-stores create \\"
    echo "     --data-store-id=$DATA_STORE_ID \\"
    echo "     --location=$LOCATION \\"
    echo "     --display-name='RAG Documents Store'"
    exit 1
  }

echo ""
echo "✓ Data Store создан: $DATA_STORE_ID"
echo ""
echo "Проверка статуса..."
sleep 5

RESP_FILE_2="$(mktemp)"
HTTP_CODE_2="$(
  curl -sS -o "$RESP_FILE_2" -w "%{http_code}" \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "x-goog-user-project: $PROJECT_ID" \
    "https://discoveryengine.googleapis.com/v1beta/projects/$PROJECT_ID/locations/$LOCATION/collections/default_collection/dataStores/$DATA_STORE_ID" \
    2>/dev/null || echo "000"
)"
if [ "$HTTP_CODE_2" = "200" ]; then
  echo "✓ Data Store подтвержден"
else
  echo "⚠ Data Store создан (операция принята), но GET пока возвращает HTTP $HTTP_CODE_2 — возможно нужно подождать 1-2 минуты."
fi

echo ""
echo "Готово!"
