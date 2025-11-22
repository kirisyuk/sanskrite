# Vertex AI Search: Поведение синхронизации

## Сценарии синхронизации

### ✅ Создание документа

```
DocumentsRepository.Create(doc)
  ↓
uploadContent() → GCS (новый файл)
  ↓
[async] vertexSearchSync(tenantID)
  ↓
ImportDocuments("gs://bucket/tenantId/**", INCREMENTAL)
  ↓
Документ появляется в Vertex AI Search через 1-2 минуты
```

### ✅ Обновление документа

```
DocumentsRepository.Upsert(doc) // существующий doc
  ↓
uploadContent() → GCS (перезаписывает существующий файл)
  ↓
[async] vertexSearchSync(tenantID)
  ↓
ImportDocuments("gs://bucket/tenantId/**", INCREMENTAL)
  ↓
Vertex AI Search обновляет документ через 1-2 минуты
```

**Важно**: `GCSClient.UploadBytes()` перезаписывает существующий файл по умолчанию, поэтому обновления работают автоматически.

### ✅ Удаление документа

```
DocumentsRepository.Delete(id)
  ↓
deleteContent() → удаляет из GCS
  ↓
[async] vertexSearchSync(tenantID)
  ↓
ImportDocuments("gs://bucket/tenantId/**", INCREMENTAL)
  ↓
Vertex AI Search удаляет документ через 1-2 минуты
```

**Как это работает**: При INCREMENTAL импорте Vertex AI Search сравнивает текущее состояние GCS с индексом и автоматически удаляет документы, которых больше нет в GCS.

## INCREMENTAL vs FULL sync

### INCREMENTAL (используется по умолчанию)

- ✅ Быстрый - обрабатывает только изменения
- ✅ Эффективный - не переиндексирует все документы
- ✅ Автоматически удаляет документы, которых нет в GCS
- ✅ Обновляет измененные документы

### FULL (не используется в автоматической синхронизации)

- Переиндексирует все документы
- Используется только для начальной синхронизации или восстановления

## Multi-tenant синхронизация

Каждый tenant синхронизируется отдельно:

```go
// При создании/обновлении документа tenant1@example.com
ImportDocuments("gs://bucket/tenant1@example.com/**", INCREMENTAL)

// При создании/обновлении документа tenant2@example.com  
ImportDocuments("gs://bucket/tenant2@example.com/**", INCREMENTAL)
```

Это гарантирует:
- ✅ Изоляцию данных между тенантами
- ✅ Эффективную синхронизацию (только нужные документы)
- ✅ Независимость операций (ошибка в одном tenant не влияет на другие)

## Производительность

### Задержка синхронизации

- **GCS Upload**: мгновенно (< 1 секунда)
- **Backend trigger**: мгновенно (асинхронно)
- **Vertex AI Search import**: 10-30 секунд (операция запускается)
- **Indexing & Embeddings**: 1-2 минуты (документ становится доступен для поиска)

### Пропускная способность

- **Документы в секунду**: ~10-50 (зависит от размера)
- **Параллельные операции**: Vertex AI Search обрабатывает несколько операций одновременно
- **Batch импорт**: Можно импортировать несколько документов одной операцией

## Обработка ошибок

### Если синхронизация не удалась

1. **Логи бэкенда**: Проверьте `[VertexAISearch] Sync triggered` сообщения
2. **Операции Vertex AI**: Проверьте статус операций импорта
3. **Повторная синхронизация**: Можно запустить вручную через API

### Автоматический retry

Сейчас retry не реализован автоматически. Если операция импорта не удалась:
- Документ останется в GCS
- При следующем обновлении документа синхронизация попробует снова
- Можно запустить полную синхронизацию вручную

## Мониторинг

### Логи бэкенда

```
[VertexAISearch] Sync triggered for tenant user@example.com: operation=projects/.../operations/import-123
[VertexAISearch] Failed to trigger sync for tenant user@example.com: <error>
```

### Проверка статуса операций

```bash
gcloud discovery-engine operations list \
  --location=global \
  --project=sanskrite \
  --filter="metadata.operationType=IMPORT"
```

### Проверка индексированных документов

```bash
./scripts/test-vertex-search.sh -p sanskrite
```

## Best Practices

1. **Не блокируем основной поток**: Синхронизация выполняется асинхронно
2. **Incremental sync**: Используем INCREMENTAL для эффективности
3. **Per-tenant sync**: Синхронизируем по tenantId для изоляции
4. **Timeout protection**: 30 секунд timeout для операций синхронизации
5. **Best effort**: Ошибки синхронизации не блокируют сохранение документа
