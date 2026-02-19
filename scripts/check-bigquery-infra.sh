#!/usr/bin/env bash
# Проверка инфраструктуры BigQuery/Vertex AI для проекта sanskrite

set -euo pipefail

PROJECT_ID="${GCP_PROJECT_ID:-h1horizon}"
REGION="US"

echo "=== Проверка инфраструктуры BigQuery/Vertex AI ==="
echo "Project: $PROJECT_ID"
echo ""

# 1. Проверка API
echo "1. Проверка включенных API..."
APIS=$(gcloud services list --enabled --filter="name:bigquery.googleapis.com OR name:aiplatform.googleapis.com OR name:discoveryengine.googleapis.com OR name:bigqueryconnection.googleapis.com" --format="value(name)" 2>/dev/null || echo "")

if echo "$APIS" | grep -q "bigquery.googleapis.com"; then
  echo "  ✅ BigQuery API включен"
else
  echo "  ❌ BigQuery API не включен"
fi

if echo "$APIS" | grep -q "aiplatform.googleapis.com"; then
  echo "  ✅ Vertex AI API включен"
else
  echo "  ❌ Vertex AI API не включен"
fi

if echo "$APIS" | grep -q "discoveryengine.googleapis.com"; then
  echo "  ✅ Discovery Engine API включен"
else
  echo "  ❌ Discovery Engine API не включен"
fi

if echo "$APIS" | grep -q "bigqueryconnection.googleapis.com"; then
  echo "  ✅ BigQuery Connections API включен"
else
  echo "  ❌ BigQuery Connections API не включен"
fi

echo ""

# 2. Проверка секретов
echo "2. Проверка секретов..."
if gcloud secrets describe gemini_api_key --project="$PROJECT_ID" >/dev/null 2>&1; then
  echo "  ✅ gemini_api_key существует"
else
  echo "  ❌ gemini_api_key не найден"
fi

echo ""

# 3. Проверка BigQuery dataset
echo "3. Проверка BigQuery dataset..."
if bq show "$PROJECT_ID:h1horizon_documents" >/dev/null 2>&1; then
  echo "  ✅ Dataset h1horizon_documents существует"
  
  # Проверка таблиц
  TABLES=$(bq ls "$PROJECT_ID:h1horizon_documents" 2>&1 | grep -E "TABLE|VIEW|EXTERNAL" | awk '{print $1}' || echo "")
  if echo "$TABLES" | grep -q "documents_metadata"; then
    echo "    ✅ Таблица documents_metadata существует"
  else
    echo "    ⚠️  Таблица documents_metadata не найдена"
  fi
  
  if echo "$TABLES" | grep -q "core_docs_metadata"; then
    echo "    ✅ Таблица core_docs_metadata существует"
  else
    echo "    ⚠️  Таблица core_docs_metadata не найдена"
  fi
  
  if echo "$TABLES" | grep -q "documents_object_table"; then
    echo "    ✅ Object Table documents_object_table существует"
  else
    echo "    ⚠️  Object Table documents_object_table не найдена"
  fi
  
  if echo "$TABLES" | grep -q "core_docs_object_table"; then
    echo "    ✅ Object Table core_docs_object_table существует"
  else
    echo "    ⚠️  Object Table core_docs_object_table не найдена"
  fi
else
  echo "  ❌ Dataset h1horizon_documents не найден"
fi

echo ""

# 4. Проверка BigQuery connections
echo "4. Проверка BigQuery connections..."
CONNECTIONS=$(bq ls --connection --location="$REGION" --format=json 2>/dev/null | jq -r '.[] | "\(.name | split("/") | .[-1])"' || echo "")
if [ -n "$CONNECTIONS" ]; then
  echo "  ✅ Connections найдены:"
  echo "$CONNECTIONS" | while read -r name; do
    echo "    - $name"
  done
  
  if echo "$CONNECTIONS" | grep -q "gcs-connection"; then
    echo "    ✅ gcs-connection существует (для Object Tables)"
  else
    echo "    ⚠️  gcs-connection не найден"
  fi
  
  if echo "$CONNECTIONS" | grep -q "vertex-ai-connection"; then
    echo "    ✅ vertex-ai-connection существует (для BigQuery ML)"
  else
    echo "    ⚠️  vertex-ai-connection не найден"
  fi
else
  echo "  ⚠️  Connections не найдены"
fi

echo ""

# 5. Проверка бакетов GCS
echo "5. Проверка бакетов GCS..."
if gsutil ls "gs://$PROJECT_ID-rag-documents" >/dev/null 2>&1; then
  echo "  ✅ Бакет $PROJECT_ID-rag-documents существует"
else
  echo "  ❌ Бакет $PROJECT_ID-rag-documents не найден"
fi

if gsutil ls "gs://$PROJECT_ID-core-docs" >/dev/null 2>&1; then
  echo "  ✅ Бакет $PROJECT_ID-core-docs существует"
else
  echo "  ❌ Бакет $PROJECT_ID-core-docs не найден"
fi

echo ""

# 6. Проверка прав service account
echo "6. Проверка прав service account backend@sanskrite.iam.gserviceaccount.com..."
SA_ROLES=$(gcloud projects get-iam-policy "$PROJECT_ID" --flatten="bindings[].members" --filter="bindings.members:backend@sanskrite.iam.gserviceaccount.com" --format="value(bindings.role)" 2>/dev/null || echo "")

if echo "$SA_ROLES" | grep -q "roles/bigquery"; then
  echo "  ✅ Service account имеет права BigQuery"
else
  echo "  ⚠️  Service account не имеет явных прав BigQuery (может использовать Application Default Credentials)"
fi

if echo "$SA_ROLES" | grep -q "roles/storage"; then
  echo "  ✅ Service account имеет права Storage"
else
  echo "  ❌ Service account не имеет прав Storage"
fi

if echo "$SA_ROLES" | grep -q "roles/secretmanager.secretAccessor"; then
  echo "  ✅ Service account имеет права Secret Manager"
else
  echo "  ❌ Service account не имеет прав Secret Manager"
fi

echo ""

# 7. Проверка переменных окружения
echo "7. Рекомендуемые переменные окружения:"
echo "  - GCP_PROJECT_ID=$PROJECT_ID"
echo "  - GOOGLE_CLOUD_PROJECT=$PROJECT_ID"
echo "  - BIGQUERY_DATASET_ID=h1horizon_documents (опционально, по умолчанию)"
echo "  - GEMINI_API_KEY (из Secret Manager)"
echo "  - VERTEX_LOCATION=us-central1 (опционально)"
echo "  - RAG_DOCUMENTS_BUCKET=$PROJECT_ID-rag-documents"
echo "  - CORE_DOCS_BUCKET=$PROJECT_ID-core-docs"

echo ""
echo "=== Проверка завершена ==="

