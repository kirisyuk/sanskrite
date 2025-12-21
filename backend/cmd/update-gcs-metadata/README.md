# Update GCS Metadata Script

Скрипт для обновления метаданных всех документов в GCS, добавляя `tenantId` и `projectId` для правильной фильтрации в Vertex AI Search.

## Назначение

Обновляет метаданные существующих документов в GCS, добавляя:
- `tenantId` - для фильтрации по тенанту
- `projectId` - для фильтрации по проекту
- `filename` - имя файла (если отсутствует)
- `sourceId`, `documentId`, `title` - для RAG документов (если отсутствуют)

## Использование

### Обновить метаданные для всех тенантов:

```bash
go run backend/cmd/update-gcs-metadata/main.go --project=YOUR_PROJECT_ID
```

### Обновить метаданные для конкретного тенанта:

```bash
go run backend/cmd/update-gcs-metadata/main.go --project=YOUR_PROJECT_ID --tenant=user@example.com
```

### Dry run (показать что будет обновлено без изменений):

```bash
go run backend/cmd/update-gcs-metadata/main.go --project=YOUR_PROJECT_ID --dry-run
```

### Использование скрипта-обертки:

```bash
# Dry run
./scripts/update-gcs-metadata.sh

# Обновить для конкретного тенанта
TENANT_ID=user@example.com ./scripts/update-gcs-metadata.sh
```

## Что обрабатывается

1. **CoreDocs** из `core-docs` bucket:
   - Обновляет метаданные оригинальных файлов
   - Добавляет `tenantId` и `projectId` из CoreDoc

2. **Documents (RAG)** из `rag-documents` bucket:
   - Обновляет метаданные документов
   - Добавляет `tenantId` и `projectId` из Document (из SourceID или Attrs)
   - Добавляет `sourceId`, `documentId`, `title` если отсутствуют

## Переменные окружения

- `GCP_PROJECT_ID` или `GOOGLE_CLOUD_PROJECT` - ID проекта GCP
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





