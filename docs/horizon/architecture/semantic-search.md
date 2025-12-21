# Семантический поиск (Native Vertex AI Search)

## Обзор

Horizon использует нативный семантический поиск через **Google Cloud Vertex AI Search** (Discovery Engine). Это позволяет выполнять глубокий семантический поиск по всем данным системы без необходимости ручного создания и управления векторными эмбеддингами.

Мы используем единый подход для всех типов данных:
1. **Источники (Sources):** Статьи, новости, отчеты.
2. **Фрагменты (Fragments):** Узлы графа знаний.
3. **Ontology Facts:** Нормализованные факты/события (`ontology_facts`) как единый поток изменений.

## Архитектура

### Единый индекс (Data Store)

Все данные индексируются в одном **Vertex AI Search Data Store**. Разделение типов данных происходит на уровне метаданных (`structData`). Это позволяет (при необходимости) выполнять сквозной поиск или строго фильтровать по типу сущности.

### Индексация

Процесс индексации полностью автоматизирован и работает через Google Cloud Storage (GCS) как промежуточное звено.

#### 1. Документы (Sources)
*Реализовано в `DocumentsRepository`*
- При сохранении документа его текст загружается в GCS: `gs://bucket/{tenantId}/{sourceId}/{docId}.txt`.
- Автоматически триггерится импорт в Vertex AI Search.
- Метаданные: `sourceId`, `projectId`, `tenantId`.

#### 2. Фрагменты (Horizon Fragments)
*Реализовано в `HorizonFragmentsRepository`*
- При создании или обновлении фрагмента генерируется **JSON документ**.
- JSON сохраняется в GCS: `gs://bucket/{tenantId}/horizon_fragments/{fragmentId}.json`.
- Автоматически триггерится импорт в Vertex AI Search.

**Формат JSON для фрагмента:**
```json
{
  "id": "fragment_uuid",
  "title": "/fac/tsmc/fab-21",
  "content": "Полное текстовое описание фрагмента, включающее имена, алиасы, метрики, статусы...",
  "structData": {
    "type": "horizon_fragment",
    "canonicalPath": "/fac/tsmc/fab-21",
    "entityPath": "/Infrastructure/Manufacturing/Factory",
    "sectorPath": "/Technology/Semiconductors/Manufacturing",
    "status": "active",
    "tenantId": "global",
    "updatedAt": "2023-10-25T10:00:00Z"
  }
}
```

### Генерация контента (Vector Text)

Поле `content` для фрагмента генерируется автоматически методом `GenerateVectorText`. Оно включает:
- Канонические имена и пути.
- Алиасы на всех языках.
- Человекочитаемые значения метрик (например, "operational health 85%").
- Статусы (`Status`, `EpistemicStatus`) для контекста актуальности.
- Теги, лейблы, описание.
- Локацию (имена и ISO коды).

Это обеспечивает высокую точность семантического поиска, так как модель видит полный контекст сущности.

#### 3. Ontology Facts
*Реализовано в `OntologyFactsRepository` + `OntologyExtractor`*
- При извлечении онтологии из документов/чатов создаются `ontology_facts`.
- Каждый факт экспортируется в GCS как JSON: `gs://bucket/{tenantId}/ontology_facts/{factId}.json`.
- Автоматически триггерится импорт в Vertex AI Search (`dataSchema=document`).

Формат JSON для факта:
```json
{
  "id": "fact_uuid",
  "title": "Human-readable title",
  "content": "vectorText (единый поисковый текст)",
  "structData": {
    "type": "ontology_fact",
    "tenantId": "global",
    "kind": "event",
    "factType": "extracted",
    "canonicalPaths": ["/org/tsmc", "/loc/tw/na/na"],
    "actorPaths": ["/org/tsmc"],
    "locationPath": "/loc/tw/na/na",
    "confidence": 0.82,
    "updatedAt": "2025-12-20T09:18:36Z"
  }
}
```

## Использование

### 1. Scout Engine (Planning Phase)
`ScoutPlanner` использует поиск для нахождения релевантных источников перед глубоким анализом.
- **Запрос:** Canonical Path целевого фрагмента (или ключевые слова).
- **Логика:** Vertex AI находит документы, семантически близкие к теме. Из документов извлекаются `sourceId` источников.
- **Результат:** Список наиболее релевантных источников для сканирования.

### 2. Graph Search / UI
`HorizonService.SearchFragments` позволяет пользователям искать узлы графа.
- **Запрос:** Произвольный текст (например, "компании с рисками водоснабжения на Тайване").
- **Фильтр:** `structData.type: "horizon_fragment"`.
- **Результат:** Список актуальных `HorizonFragment`.

### 3. Entity Resolution
Поиск дубликатов или похожих сущностей при обработке новых сигналов.
- **Запрос:** Атрибуты новой сущности.
- **Результат:** Существующие фрагменты с высокой степенью сходства.

## Конфигурация

Для работы требуется:
1. **Google Cloud Project** с включенным Discovery Engine API.
2. **Data Store** в Vertex AI Search (Generic / Unstructured with metadata).
3. Переменные окружения:
   - `VERTEX_SEARCH_DATA_STORE_ID`
   - `VERTEX_SEARCH_LOCATION` (default: global)
   - `CORE_DOCS_BUCKET` (для GCS)
