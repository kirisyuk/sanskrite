# QA Vertex Grounding: почему 0 alternatives

## Цепочка

1. **EnrichVertexAlternatives** вызывает Vertex AI Search с запросом `plan.NormalizedQuestion + " alternative supplier"`.
2. Vertex возвращает `results[]` с `document.id` и `structData`.
3. Для каждого результата: `fragmentsRepo.Get(ctx, id)` — ищем в Firestore `horizon_fragments`.
4. Только horizon fragments (org, product и т.д.) есть в `horizon_fragments`; supply_chain artifacts — нет.

## Причины 0 результатов

### 1. `semanticState: "DISABLED"`

В ответе Vertex: `"semanticState": "DISABLED"` — семантический поиск отключён для этого datastore или запроса. Используется только keyword/BM25. Длинный вопрос ("How would a 5% VAT increase in Indonesia affect battery costs? alternative supplier") может плохо матчиться с индексом.

### 2. Смешанный индекс nv-horizon-fragments-store

В `nv-horizon-fragments-store` лежат:
- **horizon_fragments** — `type: "horizon_fragment"`, id = Firestore doc ID, путь `global/horizon_fragments/{id}.json`
- **supply_chain_artifacts** — `kind: "SUPPLY_CHAIN_PATH"`, id типа `4E3If71ou5fw6dueZWdm`, путь `global/supply_chain_artifacts/{id}.json`

`fragmentsRepo.Get(id)` ищет только в `horizon_fragments`. ID supply_chain артефактов не резолвятся.

### 3. Фильтр по типу

Попытка `structData.type: ANY("horizon_fragment")` дала 400 (unsupported field). `type: ANY("horizon_fragment")` принят, но при этом 0 результатов — либо horizon_fragments не индексированы, либо keyword-поиск не находит их по запросу.

### 4. Контент horizon fragments

`GenerateVectorText()` даёт: label, type, canonicalPath, legalName, sector, aliases и т.д. Запрос про "VAT Indonesia battery costs" семантически слабо связан с такими полями.

## Что сделать

1. **Включить semantic search** для datastore в консоли Vertex AI (если возможно).
2. **Упростить запрос** — извлекать ключевые сущности (nickel, Indonesia, battery) и искать `"nickel Indonesia alternative supplier"` вместо полного вопроса.
3. **Отдельный datastore** для horizon_fragments (VERTEX_SEARCH_HORIZON_DATA_STORE_ID) — без supply_chain, только org/product.
4. **Логирование** — при `fragmentsRepo.Get` fail логировать id и docType, чтобы видеть, приходят ли supply_chain результаты.
