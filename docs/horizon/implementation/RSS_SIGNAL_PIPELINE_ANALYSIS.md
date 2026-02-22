# RSS → Signals → Propagation: полный пайплайн и разрыв

## Цель

Понять, как новости из RSS превращаются в сигналы, как идёт propagation и интерференция, и как это влияет на граф. Выявить разрыв между «путь от новостей» и «путь от графа».

---

## 1. Реальный RSS-пайплайн (news-first)

```
RSS source
    ↓ IngestRSS
document.extract (Pub/Sub: FullText, Title, URL)
    ↓ FactsExtractor (в сервере)
RunDocumentExtraction (LLM) → DocumentExtraction
    ↓ OntologyExtractor
extractEntities → entityIDs (name → CanonicalPath)
extractEvents → OntologyFact(event) с CanonicalPaths = ActorPaths + LocationPath
    ↓ factsRepo.Create
ontology_facts (Firestore)
    ↓ POST /api/horizon/events:extract-signals (ручной вызов)
SignalPipeline.ProcessEvent
    ↓ SignalExtractor (LLM)
SignalDescriptor с SeedPaths (= fact.CanonicalPaths или LLM seedPaths)
    ↓ SeedInjector
GetByCanonicalPath(SeedPath) на horizon_fragments
    ↓ если путь ЕСТЬ в графе
seedFragmentIDs
    ↓ PropagationEngine
PropagateSignalFromSeeds → обновление fragment.State, propagation по рёбрам
```

---

## 2. Откуда берутся CanonicalPaths на событии

**Event fact** получает `CanonicalPaths` из:

1. **ActorPaths** — участники события. Каждый участник — Entity из `entityIDs`.
2. **entityIDs** строятся в `extractEntities`:
   - LLM извлекает сущности: `{"type": "Organization", "value": "Flydubai"}`
   - `EntityResolver.ResolveEntity("Flydubai")` → ищет в **core entities** (не в Horizon)
   - Если найден: берём `existing.CanonicalPaths[0]`
   - Если не найден: создаём Entity, path = `taxonomy.EntityCanonicalPath(type, name)` → например `/org/ae/flydubai-llc`
3. **LocationPath** — из `event.Location` (country, admin1, city) → `/loc/{cc}/{admin1}/{city}`

**Итого:** CanonicalPaths на событии — это пути из **ontology/entities** и taxonomy. Они **не** гарантированно есть в **Horizon graph** (horizon_fragments).

---

## 3. Критический разрыв: Entity path ≠ Horizon fragment

| Источник | Коллекция | Кто создаёт |
|----------|-----------|-------------|
| CanonicalPath на Entity/Event | `entities`, `ontology_facts` | OntologyExtractor, taxonomy |
| Fragment в графе | `horizon_fragments` | Scout, bootstrap, ручное создание |

**SeedInjector** делает `fragmentsRepo.GetByCanonicalPath(ctx, path)` — это **Horizon fragments**. Если путь `/org/ae/flydubai-llc` есть только в entities, но не в horizon_fragments, то:

```
no seed nodes found for signal
```

Propagation не запускается.

**Vertex AI Search (основной инструмент):** семантическое разрешение ontology path → graph fragment. SeedInjector вызывает Vertex AI Search с запросом, производным от пути (например `/org/na/microsoft` → `"microsoft"`). Берётся топ-результат с relevance ≥ 0.3. Exact path match — быстрый путь, когда путь уже есть в графе. Требует `VERTEX_SEARCH_HORIZON_DATA_STORE_ID` и индексированные horizon_fragments в Vertex.

**LocationPath fallback:** если `SeedPaths` не дали ни одного фрагмента, SeedInjector пробует `LocationPath`:
- ищет фрагмент локации по пути (например `/loc/kz/na/na`);
- если найден — ищет org/facility через рёбра `OPERATES_IN` / `LOCATED_IN` (source → location).
То есть propagation может стартовать от локации, если org-пути не в графе.

---

## 4. Почему horizon_propagation_calibration идёт «от пути»

Текущий инструмент:

1. Берёт `-path=/gov/kz/state-revenue-committee` — путь **из графа** (мы знаем, что он есть)
2. Через Gemini ищет новости про эту организацию
3. Извлекает сигналы (axis, amplitude, seedPaths)
4. Задаёт `seedPaths = [path]` — путь уже проверен
5. Propagation гарантированно находит seeds

Это **graph-first**: мы выбираем узел графа и симулируем «что было бы, если бы пришли новости про него». Удобно для калибровки propagation, но **не** показывает реальный RSS-поток.

---

## 5. Реальный RSS-поток: что нужно видеть

Чтобы «видеть, как разбор новостей, преобразование в сигналы, propagation и интерференция влияют на граф», нужен инструмент, который:

1. **Запускает полный RSS pipeline** (IngestRSS или чтение document.extract)
2. **Потребляет document.extract** → создаёт ontology facts (как FactsExtractor)
3. **Для каждого event fact**:
   - Показывает: Title, CanonicalPaths, LocationPath
   - Вызывает SignalPipeline.ProcessEvent
   - Логирует: какие SeedPaths разрешились в фрагменты, какие — нет
4. **Показывает propagation**:
   - Затронутые узлы
   - Trace (как сигнал прошёл по рёбрам)
   - Интерференция (если несколько событий бьют в одни узлы)

---

## 6. Выводы

1. **Ноды берутся только из графа** — верно. SeedInjector ищет фрагменты по `GetByCanonicalPath`. Если пути нет в `horizon_fragments`, семян нет.

2. **Почему calibration идёт «по пути»** — чтобы гарантировать наличие seeds. В реальном RSS-потоке пути приходят из extraction и могут не совпадать с графом.

3. **Чтобы RSS → propagation работал end-to-end**, нужно:
   - Либо: Scout/бootstrap уже заполнили граф теми org/loc, которые появятся в новостях
   - Либо: создавать фрагменты on-the-fly при появлении event с неизвестным путём (сейчас не делается)

4. **Интерференция** — когда несколько сигналов (от разных событий или осей) попадают в один узел. Реализовано в propagation (MergeAccumulatedWithSeedDedup, aggregateByAxisPolarity). Видно в `horizon_propagation_calibration` при `-paths=path1,path2` (multi-node mode).

---

## 7. Инструмент horizon_trace_rss_signals

Реализован в `backend/cmd/tools/horizon_trace_rss_signals/`:

- **Вход:** `-feed-url` (RSS URL) или `-source` (DataSource ID)
- **Действия:**
  1. Fetch RSS → NormalizedDocuments
  2. ExtractAndPersist для каждого документа (LLM → ontology_facts)
  3. ListByTenantAndKind(tenantID, "event") → события
  4. Для каждого event: SignalPipeline.ProcessEvent (или simulate)
  5. Вывод: event → signals → seed resolution (✓/✗) → propagation result
- **Флаги:** `-simulate` (без записи в граф), `-docs-limit`, `-events-limit`

```bash
GCP_PROJECT_ID=xxx go run ./cmd/tools/horizon_trace_rss_signals -project=xxx -feed-url=https://news.ycombinator.com/rss -docs-limit=3 -simulate
```

Полная видимость: новости → события → сигналы → семена → propagation.
