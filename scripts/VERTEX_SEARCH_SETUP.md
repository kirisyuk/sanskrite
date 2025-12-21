# Настройка Vertex AI Search для всех документов

Этот документ описывает процесс настройки Vertex AI Search для всех существующих документов в системе.

## Шаг 1: Обновление метаданных GCS

Перед синхронизацией с Vertex AI Search необходимо обновить метаданные всех документов в GCS, добавив `tenantId` и `projectId` для правильной фильтрации.

### Выполнение:

```bash
# 1. Dry run - посмотреть что будет обновлено
./scripts/update-gcs-metadata.sh

# 2. Подтвердить и выполнить обновление
# (скрипт спросит подтверждение после dry run)

# Или напрямую:
go run backend/cmd/update-gcs-metadata/main.go --project=YOUR_PROJECT_ID
```

### Что обновляется:

- **CoreDocs** в `core-docs` bucket:
  - Добавляет `tenantId` из CoreDoc
  - Добавляет `projectId` из CoreDoc
  - Добавляет `filename` если отсутствует

- **Documents (RAG)** в `rag-documents` bucket:
  - Добавляет `tenantId` из Document
  - Добавляет `projectId` из SourceID (`project:projectId`) или Attrs
  - Добавляет `sourceId`, `documentId`, `title` если отсутствуют

## Шаг 2: Синхронизация с Vertex AI Search

После обновления метаданных синхронизируйте все документы с Vertex AI Search.

### Выполнение:

```bash
# Синхронизировать все документы для всех тенантов
./scripts/sync-all-vertex-search.sh

# Или напрямую:
go run backend/cmd/sync-all-vertex-search/main.go --project=YOUR_PROJECT_ID
```

### Что синхронизируется:

1. **CoreDocs bucket** (`{project}-core-docs`):
   - Все документы для каждого тенанта: `gs://{bucket}/{tenantId}/**`

2. **RAG Documents bucket** (`{project}-rag-documents`):
   - Все документы для каждого тенанта: `gs://{bucket}/{tenantId}/**`

## Авторизация

Перед выполнением скриптов убедитесь, что вы авторизованы:

```bash
gcloud auth application-default login --project=YOUR_PROJECT_ID
```

Или используйте service account:

```bash
export GOOGLE_APPLICATION_CREDENTIALS="path/to/credentials.json"
```

## Переменные окружения

Убедитесь, что установлены следующие переменные:

```bash
export GCP_PROJECT_ID="your-project-id"
export VERTEX_SEARCH_DATA_STORE_ID="rag-documents-store"
export VERTEX_SEARCH_LOCATION="global"
export CORE_DOCS_BUCKET="your-project-core-docs"  # опционально
export RAG_DOCUMENTS_BUCKET="your-project-rag-documents"  # опционально
```

## Проверка результатов

### Проверить статус операций импорта:

```bash
# В некоторых окружениях команда `gcloud discovery-engine ...` может быть недоступна.
# Тогда проверяйте через REST API (рекомендуется добавить quota project header).

# REST API: последние IMPORT операции
ACCESS_TOKEN="$(gcloud auth print-access-token)"
curl -s \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "x-goog-user-project: YOUR_PROJECT_ID" \
  "https://discoveryengine.googleapis.com/v1beta/projects/YOUR_PROJECT_ID/locations/global/operations?filter=metadata.operationType%3DIMPORT&pageSize=10"
```

### Проверить документы в Vertex AI Search:

Используйте API поиска:

```bash
curl -X POST \
  "https://discoveryengine.googleapis.com/v1beta/projects/YOUR_PROJECT_ID/locations/global/collections/default_collection/dataStores/rag-documents-store:search" \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "test query",
    "pageSize": 10,
    "filter": "tenantId:\"user@example.com\""
  }'
```

## Важные замечания

1. **Время индексации**: Документы становятся доступны в Vertex AI Search через **1-2 минуты** после синхронизации

2. **Инкрементальная синхронизация**: Скрипты используют `INCREMENTAL` режим - синхронизируются только новые/измененные документы

3. **Автоматическая синхронизация**: После настройки все новые документы будут автоматически синхронизироваться с Vertex AI Search при загрузке в GCS

4. **Метаданные**: Убедитесь, что все документы имеют правильные метаданные (`tenantId`, `projectId`) для корректной фильтрации

## Устранение проблем

### Ошибка авторизации:

```bash
# Проверить текущую авторизацию
gcloud auth list
gcloud auth application-default print-access-token

# Переавторизоваться
gcloud auth application-default login --project=YOUR_PROJECT_ID
```

### Документы не появляются в поиске:

1. Проверьте статус операций импорта
2. Убедитесь, что метаданные обновлены (`tenantId`, `projectId`)
3. Проверьте логи бэкенда на наличие ошибок синхронизации
4. Подождите 1-2 минуты после синхронизации

### Ошибки обновления метаданных:

- Убедитесь, что у service account есть права `roles/storage.objectAdmin` на bucket'ы
- Проверьте, что документы существуют в GCS
- Проверьте формат путей в `StoragePath` (должен быть `gs://bucket/path`)



