# Firestore Vector Search - Lessons Learned

## Дата: 2024-11-06

## Контекст

Интеграция нативного векторного поиска Firestore для RAG (Retrieval Augmented Generation) системы. Система использует Firestore для хранения документов, чанков и эмбеддингов.

## Проблема

Изначально предполагалось, что векторные поля можно создать через:
- Firebase Console UI
- Firebase CLI (`firebase deploy --only firestore:indexes`)
- `fieldOverrides` в `firestore.indexes.json`
- `gcloud alpha firestore indexes fields create`

**Все эти подходы не работают для векторных полей.**

## Решение

Векторные поля в Firestore создаются через **composite indexes с `vector-config`**, а не через fieldOverrides или single-field indexes.

### Правильная команда создания

```bash
gcloud firestore indexes composite create \
  --collection-group=rag_chunks \
  --query-scope=COLLECTION \
  --field-config='field-path=embedding,vector-config={"dimension":"1536", "flat": "{}"}' \
  --database="(default)" \
  --project=PROJECT_ID
```

### Ключевые моменты

1. **Composite index, не single-field**: Векторные поля - это composite indexes с vector-config в одном из полей
2. **Не через UI**: Firebase Console и Google Cloud Console не имеют UI для создания векторных полей
3. **Не через Firebase CLI**: `firebase deploy --only firestore:indexes` не поддерживает vector-config в fieldOverrides
4. **Только через gcloud**: Используйте `gcloud firestore indexes composite create`

## Проверка наличия индекса

### Важно

Команда `gcloud firestore indexes composite list` **не поддерживает** флаг `--collection-group`. Нужно получать все индексы и фильтровать через jq:

```bash
# Правильная проверка
gcloud firestore indexes composite list --format=json | \
  jq '[.[] | select(.fields[]?.vectorConfig != null and (.name | contains("rag_chunks")))] | length'

# Возвращает количество векторных индексов для rag_chunks
```

### Fallback для случаев без jq

```bash
gcloud firestore indexes composite list --format=json | \
  grep -q 'rag_chunks.*embedding.*vectorConfig'
```

## Интеграция в код

### Go SDK (cloud.google.com/go/firestore v1.20.0+)

```go
// Нативный векторный поиск
vectorQuery := q.FindNearest("embedding", vector, topK, firestore.DistanceMeasureCosine, nil)
docs, err := vectorQuery.Documents(ctx).GetAll()
```

### Fallback на in-app KNN

Если нативный поиск недоступен (индекс не создан или ошибка), код автоматически переключается на in-app KNN поиск:

```go
// Автоматический fallback
if i.client != nil {
    ids, err := i.queryNative(ctx, vector, topK, tenantID, sourceIDs)
    if err != nil {
        // Fallback на in-app поиск
        return i.queryInApp(ctx, vector, topK, tenantID, sourceIDs)
    }
    return ids, nil
}
return i.queryInApp(ctx, vector, topK, tenantID, sourceIDs)
```

## Автоматизация в скрипте деплоя

### Проверка наличия индекса

```bash
local has_vector_index=false
if command -v jq >/dev/null 2>&1; then
    local vector_index_count
    vector_index_count=$(gcloud firestore indexes composite list --format=json 2>/dev/null | \
      jq '[.[] | select(.fields[]?.vectorConfig != null and (.name | contains("rag_chunks")))] | length' \
      2>/dev/null || echo "0")
    if [ "$vector_index_count" -gt 0 ] 2>/dev/null; then
        has_vector_index=true
    fi
else
    # Fallback через grep
    if gcloud firestore indexes composite list --format=json 2>/dev/null | \
       grep -q 'rag_chunks.*embedding.*vectorConfig' 2>/dev/null; then
        has_vector_index=true
    fi
fi
```

### Создание индекса

```bash
if [ "$has_vector_index" != true ]; then
    gcloud firestore indexes composite create \
      --collection-group=rag_chunks \
      --query-scope=COLLECTION \
      --field-config='field-path=embedding,vector-config={"dimension":"1536", "flat": "{}"}' \
      --database="(default)" \
      --project="$PROJECT_ID" \
      --quiet
fi
```

## Ограничения и особенности

1. **Размерность вектора**: Максимум 2048 (в нашем случае используется 1536 для OpenAI text-embedding-3-small)
2. **Distance measure**: COSINE (используется в коде через `firestore.DistanceMeasureCosine`)
3. **Максимум результатов**: 1000 документов за запрос
4. **Real-time listeners**: Не поддерживаются для векторного поиска
5. **Время создания индекса**: Может занять несколько минут после создания

## Что НЕ работает

❌ Firebase Console UI - нет опций для векторных полей  
❌ Google Cloud Console UI - нет опций для векторных полей  
❌ Firebase CLI - не поддерживает vector-config в fieldOverrides  
❌ `gcloud alpha firestore indexes fields create` - не поддерживает vector-config  
❌ `gcloud firestore indexes composite list --collection-group=...` - флаг не поддерживается  

## Что работает

✅ `gcloud firestore indexes composite create` с vector-config  
✅ Нативный поиск через `FindNearest` в Go SDK  
✅ Автоматический fallback на in-app KNN  
✅ Проверка наличия индекса через фильтрацию всех индексов  

## Рекомендации

1. **Всегда проверяйте наличие индекса** перед созданием (избегайте дублирования)
2. **Используйте fallback** - система должна работать даже без векторного индекса
3. **Документируйте процесс** - векторные поля требуют специального подхода
4. **Автоматизируйте в скрипте деплоя** - проверка и создание должны быть автоматическими
5. **Тестируйте fallback** - убедитесь, что in-app KNN работает корректно

## Связанные файлы

- `backend/internal/ai/vector_index_firestore.go` - реализация векторного поиска
- `web/firestore.indexes.json` - конфигурация индексов (не содержит векторных полей)
- `scripts/deploy` - скрипт деплоя с автоматическим созданием векторного индекса
- `backend/internal/http/discovery.go` - использование векторного поиска в API

## Ссылки

- [Firestore Vector Search Documentation](https://firebase.google.com/docs/firestore/vector-search)
- [Go Firestore SDK Documentation](https://pkg.go.dev/cloud.google.com/go/firestore)
- [gcloud firestore indexes composite create](https://cloud.google.com/sdk/gcloud/reference/firestore/indexes/composite/create)

