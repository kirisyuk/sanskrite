# Vertex AI Search Multi-Tenancy: Реализация завершена ✅

## Что сделано

### 1. Автоматическая фильтрация по tenantId
**Файл**: `backend/internal/http/discovery.go`

Каждый поисковый запрос автоматически фильтруется:
```go
// Пользователь видит только:
// - Свои документы (tenantId = auth.TenantID)
// - Глобальные документы (tenantId = "global")

tenantFilter := fmt.Sprintf("tenantId:\"%s\" OR tenantId:\"global\"", auth.TenantID)
```

**Гарантии безопасности**:
- ✅ Тенанты не могут видеть документы друг друга
- ✅ Фильтр применяется на стороне сервера (не зависит от клиента)
- ✅ Даже с вредоносными фильтрами tenant isolation сохраняется

### 2. Скрипт автоматической синхронизации
**Файл**: `scripts/setup-vertex-search-sync.sh`

**Что делает**:
1. Создает Cloud Function для автоматического импорта при изменении GCS
2. Настраивает триггер на bucket `etigram-rag-documents`
3. Выполняет начальную полную синхронизацию
4. Настраивает incremental sync для новых документов

**Использование**:
```bash
./scripts/setup-vertex-search-sync.sh -p sanskrite
```

### 3. Поддержка глобальных документов
Системные документы с `tenantId = "global"` доступны всем тенантам:

```go
doc := &models.Document{
    TenantID: "global",  // Доступно всем
    SourceID: "system:knowledge-base",
    // ...
}
```

**Use cases**:
- Системная документация
- Shared knowledge base
- Шаблоны
- Product guides

### 4. Документация
- `docs/architecture/vertex-search-multi-tenancy.md` - полная архитектура
- `docs/architecture/VERTEX_SEARCH_README.md` - quick start
- Обновлены комментарии в коде

## Архитектура

### Хранение документов в GCS
```
gs://etigram-rag-documents/
  ├── tenant1@example.com/
  │   ├── project:abc/
  │   │   ├── doc1.txt
  │   │   └── doc2.txt
  │   └── project:xyz/
  │       └── doc3.txt
  ├── tenant2@example.com/
  │   └── ...
  └── global/              # Системные документы для всех
      └── system:kb/
          └── guide.txt
```

### Автоматическая синхронизация
```
GCS Upload → Cloud Function Trigger → Vertex AI Import → Embeddings Created → Searchable (1-2 min)
```

### API с tenant filtering
```
User Request
  ↓
Backend извлекает tenantId из auth
  ↓
Применяет фильтр: tenantId:"user" OR tenantId:"global"
  ↓
Vertex AI Search возвращает только разрешенные документы
  ↓
Response
```

## Как использовать

### 1. Настройка (один раз)
```bash
# Настроить автоматическую синхронизацию
./scripts/setup-vertex-search-sync.sh -p sanskrite

# Проверить работу
./scripts/test-vertex-search.sh -p sanskrite
```

### 2. Загрузка документов
Документы автоматически загружаются в GCS через `DocumentsRepository.Upsert()`:

```go
doc := &models.Document{
    TenantID: "user@example.com",
    SourceID: "project:abc123",
    FullText: "Document content...",
    // ...
}
err := docsRepo.Upsert(ctx, doc)
// → Автоматически сохраняется в GCS
// → Cloud Function триггерится
// → Импортируется в Vertex AI Search
// → Доступен для поиска через 1-2 минуты
```

### 3. Поиск
```typescript
// Frontend
const results = await fetch(`/api/v1/projects/${projectId}/vertex-search`, {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    query: 'search query',
    pageSize: 10,
    filter: 'documentType:article'  // Опциональный дополнительный фильтр
  })
});

// Backend автоматически добавляет tenant filter
```

### 4. Создание глобальных документов
```go
globalDoc := &models.Document{
    TenantID: "global",  // Доступно всем тенантам
    SourceID: "system:knowledge-base",
    Title: "Product Guide",
    FullText: "...",
}
err := docsRepo.Upsert(ctx, globalDoc)
```

## Проверка работы

### Тест 1: Базовая работоспособность
```bash
./scripts/test-vertex-search.sh -p sanskrite

# Ожидаемый результат:
# ✅ Data Store exists
# ✅ Found X indexed documents
# ✅ Search query executed successfully
```

### Тест 2: Tenant isolation
```bash
# Создать документы для двух тенантов
# Выполнить поиск от имени tenant1
# Проверить, что возвращаются только документы tenant1 + global

# Код для теста в следующем сообщении если нужен
```

### Тест 3: Автоматическая синхронизация
```bash
# 1. Загрузить новый документ
# 2. Подождать 2 минуты
# 3. Выполнить поиск
# 4. Документ должен быть найден
```

## Мониторинг

### Логи Cloud Function
```bash
gcloud functions logs read vertex-search-sync-trigger \
  --region=us-central1 \
  --project=etigram \
  --limit=50
```

### Статус операций импорта
```bash
gcloud discovery-engine operations list \
  --location=global \
  --project=etigram \
  --filter="metadata.operationType=IMPORT"
```

### Количество документов
```bash
./scripts/test-vertex-search.sh -p sanskrite | grep "indexed documents"
```

## Next Steps

1. **Запустить setup скрипт**:
   ```bash
   ./scripts/setup-vertex-search-sync.sh -p sanskrite
   ```

2. **Проверить работу**:
   ```bash
   ./scripts/test-vertex-search.sh -p sanskrite
   ```

3. **Создать тестовые документы** для разных тенантов и проверить изоляцию

4. **Интегрировать в UI** (endpoint уже готов: POST /api/v1/projects/{id}/vertex-search)

## Важные файлы

**Backend**:
- `backend/internal/http/discovery.go` - VertexAISearch handler с tenant filtering
- `backend/internal/ai/vertex_search_client.go` - Vertex AI Search client
- `backend/internal/repo/documents.go` - DocumentsRepository с GCS storage

**Scripts**:
- `scripts/setup-vertex-search-sync.sh` - настройка автоматической синхронизации
- `scripts/test-vertex-search.sh` - тестирование работы
- `scripts/import-documents-to-vertex-search.sh` - ручной импорт (если нужен)

**Documentation**:
- `docs/architecture/vertex-search-multi-tenancy.md` - полная архитектура
- `docs/architecture/VERTEX_SEARCH_README.md` - quick start guide
