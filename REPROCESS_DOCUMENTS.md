# Репроцессинг существующих документов

## Проблема

Существующие документы со статусом `"embedded"` были созданы до исправления пайплайна обработки онтологии. Они не были обработаны DocumentProcessor и не имеют извлеченной онтологии.

## Решение

Добавлен эндпоинт для репроцессинга документов:

**POST** `/api/v1/sources/{sourceId}/reprocess`

### Описание

Находит все документы со статусом `"embedded"` для указанного источника и публикует события `document.process` для их повторной обработки.

### Использование

```bash
curl -X POST \
  https://etigram-api-sqt63d2k2q-uc.a.run.app/api/v1/sources/{sourceId}/reprocess \
  -H "Authorization: Bearer <token>"
```

### Ответ

```json
{
  "queued": true,
  "sourceId": "XM93A6EpXY9RGL3zqHWq",
  "count": 42,
  "message": "Reprocessing queued"
}
```

### Что происходит

1. Эндпоинт находит все документы со статусом `"embedded"` для источника
2. Асинхронно публикует события `EventDocProcess` в топик `etigram.artifact.events`
3. DocumentProcessor получает события и обрабатывает документы:
   - LLM-извлечение (категории, темы, сущности, события, claims)
   - Извлечение онтологии через `OntologyExtractor`
   - Сохранение артефактов в Firestore

### Статусы документов

- `"embedded"` → `"processing"` → `"processed"`

### Логи

Проверить обработку можно через логи:

```bash
# Логи репроцессинга
gcloud logging read "textPayload=~\"ReprocessDocuments\"" --limit 20

# Логи DocumentProcessor
gcloud logging read "textPayload=~\"doc processor\"" --limit 20

# Логи извлечения онтологии
gcloud logging read "textPayload=~\"OntologyExtractor\"" --limit 20
```

