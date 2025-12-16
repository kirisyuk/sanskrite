# Sync All Documents to Vertex AI Search

Скрипт для синхронизации всех документов из GCS в Vertex AI Search для всех тенантов и проектов.

## Назначение

Синхронизирует все документы из обоих bucket'ов (`core-docs` и `rag-documents`) в Vertex AI Search:
- Обрабатывает все тенанты автоматически
- Синхронизирует оба bucket'а для каждого тенанта
- Использует инкрементальную синхронизацию (только новые/измененные документы)

## Использование

### Синхронизировать все документы для всех тенантов:

```bash
go run backend/cmd/sync-all-vertex-search/main.go --project=YOUR_PROJECT_ID
```

### Синхронизировать для конкретного тенанта:

```bash
go run backend/cmd/sync-all-vertex-search/main.go --project=YOUR_PROJECT_ID --tenant=user@example.com
```

### Настроить задержку между синхронизациями:

```bash
go run backend/cmd/sync-all-vertex-search/main.go --project=YOUR_PROJECT_ID --wait=10s
```

### Использование скрипта-обертки:

```bash
./scripts/sync-all-vertex-search.sh

# Для конкретного тенанта
TENANT_ID=user@example.com ./scripts/sync-all-vertex-search.sh

# С настройкой задержки
WAIT_BETWEEN=10s ./scripts/sync-all-vertex-search.sh
```

## Что синхронизируется

1. **CoreDocs bucket** (`{project}-core-docs`):
   - Все документы для каждого тенанта: `gs://{bucket}/{tenantId}/**`

2. **RAG Documents bucket** (`{project}-rag-documents`):
   - Все документы для каждого тенанта: `gs://{bucket}/{tenantId}/**`

## Переменные окружения

- `GCP_PROJECT_ID` или `GOOGLE_CLOUD_PROJECT` - ID проекта GCP
- `VERTEX_SEARCH_DATA_STORE_ID` - ID Data Store в Vertex AI Search (обязательно)
- `VERTEX_SEARCH_LOCATION` - Location Data Store (по умолчанию: `global`)
- `CORE_DOCS_BUCKET` - bucket для CoreDocs (по умолчанию: `{project}-core-docs`)
- `RAG_DOCUMENTS_BUCKET` - bucket для RAG документов (по умолчанию: `{project}-rag-documents`)

## Авторизация

Требуется авторизация через Application Default Credentials:

```bash
gcloud auth application-default login --project=YOUR_PROJECT_ID
```

Или через service account:

```bash
export GOOGLE_APPLICATION_CREDENTIALS="path/to/credentials.json"
```

## Время выполнения

- Документы становятся доступны в Vertex AI Search через **1-2 минуты** после синхронизации
- Синхронизация выполняется асинхронно - скрипт только триггерит импорт
- Для проверки статуса операций используйте:

```bash
gcloud discovery-engine operations list \
  --location=global \
  --project=YOUR_PROJECT_ID \
  --filter="metadata.operationType=IMPORT"
```

## Рекомендуемый порядок выполнения

1. Сначала обновите метаданные:
   ```bash
   ./scripts/update-gcs-metadata.sh
   ```

2. Затем синхронизируйте с Vertex AI Search:
   ```bash
   ./scripts/sync-all-vertex-search.sh
   ```



