# BigQuery/Vertex AI Infrastructure Setup Checklist

## Текущий статус инфраструктуры (проект: sanskrite)

### ✅ Что уже настроено:

1. **API включены:**
   - ✅ BigQuery API (`bigquery.googleapis.com`)
   - ✅ Vertex AI API (`aiplatform.googleapis.com`)
   - ✅ Discovery Engine API (`discoveryengine.googleapis.com`)
   - ✅ BigQuery Connections API (`bigqueryconnection.googleapis.com`)

2. **Секреты:**
   - ✅ `gemini_api_key` - существует в Secret Manager
   - ✅ `gemini_api_key_test` - существует в Secret Manager

3. **BigQuery:**
   - ✅ Dataset `nv_horizon_documents` существует
   - ✅ Таблица `documents_metadata` существует
   - ✅ Таблица `core_docs_metadata` существует ✅ (создано)
   - ✅ View `documents_full` существует
   - ✅ Object Table `documents_object_table` существует ✅ (создано)
   - ✅ Object Table `core_docs_object_table` существует ✅ (создано)

4. **BigQuery Connections:**
   - ✅ `vertex-ai-connection` существует (для BigQuery ML)
   - ✅ `gcs-connection` существует (для Object Tables) ✅ (создано)

5. **GCS Buckets:**
   - ✅ `nv-horizon-rag-documents` существует
   - ✅ `sanskrite-core-docs` существует

6. **Service Account права:**
   - ✅ `backend@sanskrite.iam.gserviceaccount.com` имеет:
     - ✅ `roles/storage.objectAdmin` на бакеты
     - ✅ `roles/storage.objectViewer` на бакеты
     - ✅ `roles/secretmanager.secretAccessor`
     - ✅ `roles/bigquery.dataEditor` ✅ (добавлено)
     - ✅ `roles/bigquery.jobUser` ✅ (добавлено)

### ⚠️ Опциональные настройки:

1. **Firebase Extension для Firestore → BigQuery (опционально):**
   ```bash
   # Для автоматического экспорта RAG documents из Firestore в BigQuery
   firebase ext:install firebase/firestore-bigquery-export \
     --project=nv-horizon \
     --params=scripts/firestore-bigquery-config.json
   
   # Для CoreDocs (нужно создать отдельный конфиг)
   ```

2. **BigQuery ML Remote Model (опционально):**
   ⚠️ **Примечание:** Создание remote модели для Vertex AI через BigQuery ML требует специальной настройки и может быть недоступно в текущей версии BigQuery. 
   
   **Текущее решение:** Используем прямой вызов Vertex AI API через `BigQueryAIService`, который:
   - Генерирует SQL запросы через Vertex AI
   - Выполняет их в BigQuery
   - Возвращает результаты
   
   Это работает и не требует создания ML модели в BigQuery.

### Переменные окружения для приложения:

```bash
# Обязательные
GCP_PROJECT_ID=sanskrite
GOOGLE_CLOUD_PROJECT=nv-horizon
GEMINI_API_KEY=<из Secret Manager>

# Опциональные (есть значения по умолчанию)
BIGQUERY_DATASET_ID=nv_horizon_documents  # по умолчанию
VERTEX_LOCATION=us-central1              # по умолчанию
RAG_DOCUMENTS_BUCKET=nv-horizon-rag-documents
CORE_DOCS_BUCKET=sanskrite-core-docs
```

### Проверка работоспособности:

1. **Проверить доступ к BigQuery:**
   ```bash
   bq query --use_legacy_sql=false \
     "SELECT COUNT(*) as count FROM \`nv-horizon.nv_horizon_documents.documents_metadata\`"
   ```

2. **Проверить доступ к Vertex AI:**
   ```bash
   # Проверить через API
   curl -H "Authorization: Bearer $(gcloud auth print-access-token)" \
     "https://us-central1-aiplatform.googleapis.com/v1/projects/nv-horizon/locations/us-central1/models"
   ```

3. **Проверить доступ к секретам:**
   ```bash
   gcloud secrets versions access latest --secret="gemini_api_key" | head -c 20
   ```

### Команды для настройки:

```bash
# 1. Запустить скрипт настройки BigQuery
./scripts/setup_bigquery.sh

# 2. Проверить инфраструктуру
./scripts/check-bigquery-infra.sh

# 3. Создать недостающие таблицы и connections
# (см. выше раздел "Что нужно настроить")
```

