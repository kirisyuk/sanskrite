# Пайплайн обработки онтологии

## Обзор

Пайплайн обработки онтологии начинается с ingest документа и заканчивается сохранением артефактов онтологии (Entities, Events, Claims) в Firestore.

## Этапы пайплайна

### 1. Ingest (IngestTelegram)

**Файл:** `backend/internal/discovery/orchestrator.go:247-331`

**Процесс:**
1. Получение сообщений из Telegram через `telegram.FetchUpdates()`
2. Для каждого сообщения:
   - Создание/обновление документа в коллекции `rag_documents`
   - Разбиение текста на чанки (`ChunkText`)
   - Генерация эмбеддингов через `emb.EmbedTexts()`
   - Сохранение чанков в коллекцию `rag_chunks`
   - Установка статуса документа: `"new"` → `"embedded"`
   - **Публикация события** `EventDocProcess` в Pub/Sub

**Код публикации:**
```go
if o.publish != nil {
    _ = o.publish.PublishJSON(ctx, realtime.EventDocProcess, tenantID, 
        map[string]any{
            "tenantId": tenantID, 
            "sourceId": sourceID, 
            "documentId": d.ID, 
            "externalId": d.ExternalID
        })
}
```

**Статус документа:** `"embedded"`

---

### 2. Маршрутизация событий (Publisher)

**Файл:** `backend/internal/realtime/pubsub.go:129-232`

**Процесс:**
1. `PublishJSON()` получает `EventDocProcess`
2. `routeTopicName()` определяет целевой топик:
   - `EventDocProcess` → `TopicArtifactEvents` = `"horizon.artifact.events"`
3. Событие публикуется в топик `horizon.artifact.events` с атрибутами:
   - `tenantId`
   - `eventType: "document.process"`
   - `ts` (timestamp)

**Исправлено:** DocumentProcessor теперь подписывается на `horizon.artifact.events` и фильтрует события по `eventType = "document.process"`.

---

### 3. DocumentProcessor (Асинхронная обработка)

**Файл:** `backend/internal/discovery/processor_worker.go`

**Инициализация:**
```go
worker := discovery.NewDocumentProcessor(
    psClient, 
    string(realtime.TopicArtifactEvents), // = "horizon.artifact.events"
    docsRepo, 
    rulesRepo, 
    exec, 
    ontologyExtractor, 
    logger
)
```

**Подписка:**
- Топик: `horizon.artifact.events`
- Подписка: `horizon.artifact.events.document-processor.worker`
- Фильтрация: только события с `eventType = "document.process"`

**Обработка сообщения:**
1. Получение payload: `{tenantId, sourceId, documentId, externalId}`
2. Загрузка документа из Firestore по `externalId`
3. Установка статуса: `"processing"`
4. **LLM-извлечение:**
   - Summarization (краткое резюме)
   - Structured extraction (категории, темы, сущности, события, claims)
   - Извлечение через `ExecuteStructuredTask()` с JSON schema
5. **Обработка извлеченных данных:**
   - Обновление `document.Language`
   - Обновление `document.DocumentType`
   - Сохранение `document.Categories`
   - Сохранение `document.Attrs` (sentiment, keyPoints, topics, outlinks)
6. **Извлечение онтологии:**
   - Вызов `extractor.ExtractFromDocument()`
7. Fallback на правила (rules), если LLM не вернул категории
8. Установка статуса: `"processed"`

**Статус документа:** `"processing"` → `"processed"`

---

### 4. OntologyExtractor (Извлечение онтологических объектов)

**Файл:** `backend/internal/ontology/extractor.go:65-113`

**Метод:** `ExtractFromDocument()`

**Процесс:**

#### 4.1. Создание/поиск Source
- `ensureSource()` создает или находит Source для документа
- Коллекция: `ontology_sources`

#### 4.2. Извлечение Entities
**Метод:** `extractEntities()`

**Процесс:**
1. Для каждой извлеченной сущности:
   - Нормализация имени (`canonicalName`)
   - Определение типа через `mapExtractedTypeToEntityType()`
   - **EntityResolver:** проверка дубликатов через `ResolveEntity()`
   - Если не найден → создание нового Entity
2. **Создание Entity:**
   - Репозиторий: `EntityRepository`
   - Коллекция: `ontology_entities`
   - Метод: `entitiesRepo.Create()`
   - Поля: `CanonicalName`, `Type`, `Aliases`, `Attributes`
3. **Асинхронные процессы:**
   - `EntityEnricher.EnrichEntity()` - обогащение из Wikidata
   - `DomainService.ClassifyAndLinkEntity()` - классификация по доменам

#### 4.3. Извлечение Events
**Метод:** `extractEvents()`

**Процесс:**
1. Для каждого извлеченного события:
   - Парсинг даты (`DateISO`)
   - Корректировка confidence через `TrustIntegrator.AdjustEventConfidence()`
   - Создание Event с участниками (entities)
2. **Создание Event:**
   - Репозиторий: `EventRepository`
   - Коллекция: `ontology_events`
   - Метод: `eventsRepo.Create()`
   - Поля: `Title`, `OccurredAt`, `Confidence`, `Provenance`, `Participants`
3. **Асинхронные процессы:**
   - `CausalDetector.DetectCausalLinksForEvent()` - обнаружение причинно-следственных связей
   - `DomainService.ClassifyAndLinkEvent()` - классификация по доменам

#### 4.4. Извлечение Claims
**Метод:** `extractClaims()`

**Процесс:**
1. Для каждого извлеченного утверждения:
   - Поиск SubjectRef (первый упомянутый Entity или Literal)
   - Корректировка confidence через `TrustIntegrator.AdjustClaimConfidence()`
   - Создание Claim
2. **Создание Claim:**
   - Репозиторий: `ClaimRepository`
   - Коллекция: `ontology_claims`
   - Метод: `claimsRepo.Create()`
   - Поля: `SubjectRef`, `Predicate`, `ObjectRef`, `Confidence`, `Stance`, `Provenance`
3. **Асинхронные процессы:**
   - `ConflictDetector.DetectConflictsForClaim()` - обнаружение конфликтов
   - `DomainService.ClassifyAndLinkClaim()` - классификация по доменам

---

## Структура данных в Firestore

### Документы
- **Коллекция:** `rag_documents`
- **Статусы:** `"new"` → `"embedded"` → `"processing"` → `"processed"`

### Чанки
- **Коллекция:** `rag_chunks`
- Содержат эмбеддинги и ссылки на документы

### Entities
- **Коллекция:** `ontology_entities`
- Поля: `CanonicalName`, `Type`, `Aliases`, `Attributes`, `Labels`, `Status`

### Events
- **Коллекция:** `ontology_events`
- Поля: `Title`, `OccurredAt`, `Confidence`, `Provenance`, `Participants`, `Type`

### Claims
- **Коллекция:** `ontology_claims`
- Поля: `SubjectRef`, `Predicate`, `ObjectRef`, `Confidence`, `Stance`, `Provenance`

### Sources
- **Коллекция:** `ontology_sources`
- Связывают документы с источниками данных

---

## Исправления

### ✅ Исправлено: Несоответствие топиков

**Проблема:**
- События публикуются в топик `horizon.artifact.events` (через `routeTopicName()`)
- DocumentProcessor был подписан на топик `document.process`

**Решение:**
1. Изменен DocumentProcessor для подписки на `horizon.artifact.events`
2. Добавлена фильтрация по `eventType = "document.process"` в обработчике сообщений
3. Изменено имя подписки на `horizon.artifact.events.document-processor.worker`

**Изменения:**
- `backend/cmd/server/main.go:367` - использует `TopicArtifactEvents` вместо `EventDocProcess`
- `backend/internal/discovery/processor_worker.go:65-76` - добавлена фильтрация по `eventType`
- `backend/internal/discovery/processor_worker.go:55` - изменено имя подписки

---

## Проверка работы пайплайна

### 1. Проверка публикации событий
```bash
gcloud logging read "textPayload=~\"PublishJSON.*document.process\"" --limit 10
```

### 2. Проверка обработки DocumentProcessor
```bash
gcloud logging read "textPayload=~\"doc processor\"" --limit 10
```

### 3. Проверка извлечения онтологии
```bash
gcloud logging read "textPayload=~\"OntologyExtractor\"" --limit 10
```

### 4. Проверка создания артефактов
```bash
# Проверить коллекции в Firestore
# ontology_entities, ontology_events, ontology_claims
```

---

## Временная шкала

1. **Ingest:** ~1-5 секунд (зависит от количества сообщений)
2. **Публикация события:** ~100ms
3. **DocumentProcessor:** ~5-30 секунд (зависит от размера документа и LLM)
4. **OntologyExtractor:** ~1-10 секунд (синхронно)
5. **Асинхронные процессы:** ~10-60 секунд (обогащение, классификация, детекторы)

**Общее время:** ~20-100 секунд от ingest до полной обработки онтологии

