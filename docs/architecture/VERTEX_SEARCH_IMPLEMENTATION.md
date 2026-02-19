# Vertex AI Search: Backend-Integrated Sync

## Как работает синхронизация

**Бэкенд сам синхронизирует GCS ↔ Vertex AI Search автоматически!**

### Поток данных:

```
1. DocumentsRepository.Upsert(doc)
   ↓
2. uploadContent() → сохраняет в GCS
   ↓
3. Асинхронно вызывается vertexSearchSync(tenantID)
   ↓
4. vertexSearchClient.ImportDocuments()
   ↓
5. Vertex AI Search импортирует документы
   ↓
6. Эмбеддинги создаются автоматически
   ↓
7. Документы доступны для поиска через 1-2 минуты
```

### Код:

**Backend** (`backend/cmd/server/main.go`):
```go
// После создания vertexSearchClient
docsRepo = docsRepo.WithVertexSearchSync(func(ctx context.Context, tenantID string) {
    gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tenantID)
    importReq := &ai.ImportDocumentsRequest{
        GCSPath:           gcsPath,
        ReconciliationMode: "INCREMENTAL",
    }
    vertexSearchClient.ImportDocuments(ctx, importReq)
})
```

**Repository** (`backend/internal/repo/documents.go`):
```go
// После успешной загрузки в GCS
if storagePath != nil {
    d.StoragePath = storagePath
    // Асинхронно триггерим синхронизацию
    if r.vertexSearchSync != nil {
        go func(tenantID string) {
            syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()
            r.vertexSearchSync(syncCtx, tenantID)
        }(d.TenantID)
    }
}
```

## Преимущества

✅ **Нет внешних зависимостей** - все в бэкенде
✅ **Автоматическая синхронизация** - при каждом Upsert
✅ **Не блокирует основной поток** - выполняется асинхронно
✅ **Incremental sync** - только новые/измененные документы
✅ **Multi-tenant aware** - синхронизирует по tenantId

## Настройка

Просто установите переменные окружения:

```bash
VERTEX_SEARCH_DATA_STORE_ID=rag-documents-store
VERTEX_SEARCH_LOCATION=global
GEMINI_API_KEY=<your-key>
RAG_DOCUMENTS_BUCKET=h1horizon-rag-documents
```

Бэкенд автоматически:
1. Создаст Vertex AI Search client
2. Настроит автоматическую синхронизацию
3. Будет триггерить импорт после каждой загрузки в GCS

## Мониторинг

Смотрите логи бэкенда:
```
[VertexAISearch] Sync triggered for tenant user@example.com: operation=projects/.../operations/import-123
```

Если нужно проверить статус операций:
```bash
gcloud discovery-engine operations list \
  --location=global \
  --project=h1horizon \
  --filter="metadata.operationType=IMPORT"
```
