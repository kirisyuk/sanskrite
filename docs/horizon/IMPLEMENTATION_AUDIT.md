# Аудит реализации подсистемы Horizon

**Дата проверки**: 2025-01-XX  
**Проверено**: Детальная сверка реализации с документацией

## Обзор

Данный документ содержит детальный анализ расхождений между документацией подсистемы Horizon и текущей реализацией в коде. Проверка проведена на основе анализа исходного кода в `backend/internal/horizon/` и `backend/internal/http/horizon/`.

## Статус реализации компонентов

### ✅ Реализовано полностью

1. **Модели данных** (`backend/internal/horizon/models/`)
   - ✅ HorizonFragment - соответствует документации
   - ✅ Frame - соответствует документации
   - ✅ Lens - соответствует документации
   - ✅ DependencyEdge - соответствует документации
   - ✅ ScoutingJob - соответствует документации
   - ✅ WatchdogConfig - соответствует документации
   - ✅ Общие типы (MetricValue, Location, KBReference, Provenance, Governance) - соответствуют

2. **Репозитории** (`backend/internal/horizon/repo/`)
   - ✅ Все репозитории созданы и соответствуют структуре документации

3. **Lens Subsystem** (`backend/internal/horizon/lens/`)
   - ✅ LensService - реализован
   - ✅ LifecycleManager - реализован
   - ✅ MetricsCalculator - реализован
   - ✅ Subscription management - реализован

4. **Propagation Engine** (`backend/internal/horizon/propagation/`)
   - ✅ PropagationEngine - реализован
   - ✅ WaveDamping - реализован
   - ✅ ThresholdManager - реализован
   - ✅ LazyEvaluator - реализован
   - ✅ Topology optimization - реализован

5. **Scout Engine** (`backend/internal/horizon/scout/`)
   - ✅ ScoutEngine - реализован
   - ✅ ScoutAgent - реализован
   - ✅ CriticAgent - реализован
   - ✅ ResolverAgent - реализован
   - ✅ ScoutPlanner - реализован
   - ✅ SourceExecutor - реализован
   - ✅ FrameBuilder - реализован
   - ✅ ScoutWorkflow - реализован

6. **Watchdog Service** (`backend/internal/horizon/watchdog/`)
   - ✅ WatchdogService - реализован
   - ✅ Monitor - реализован
   - ✅ Scheduler - реализован

### ⚠️ Частично реализовано

1. **HTTP API Handlers** (`backend/internal/http/horizon/`)
   - ✅ **Полностью реализованы**:
     - ✅ `POST /api/horizon/lenses` - CreateLens
     - ✅ `GET /api/horizon/lenses/{id}` - GetLens
     - ✅ `PUT /api/horizon/lenses/{id}` - UpdateLens
     - ✅ `DELETE /api/horizon/lenses/{id}` - DeleteLens
     - ✅ `GET /api/horizon/lenses?projectId={projectId}&status={status}` - список Lens проекта (реализован в `ListLenses`)
     - ✅ `POST /api/horizon/lenses/{id}/recalculate` - принудительный пересчет (реализован в `RecalculateLens`)
     - ✅ `GET /api/horizon/fragments` - ListFragments
     - ✅ `GET /api/horizon/fragments/{id}` - GetFragment
     - ✅ `GET /api/horizon/fragments/by-path?path={canonicalPath}` - получение по canonicalPath (реализован в `GetFragmentByPath`)
     - ✅ `GET /api/horizon/fragments/{id}/dependencies` - зависимости узла (реализован в `GetFragmentDependencies`)
     - ✅ `POST /api/horizon/edges` - создание DependencyEdge (реализован в `CreateEdge`)
     - ✅ `PUT /api/horizon/edges/{edgeId}` - обновление DependencyEdge (реализован в `UpdateEdge`)
     - ✅ `DELETE /api/horizon/edges/{edgeId}` - удаление DependencyEdge (реализован в `DeleteEdge`)
     - ✅ `GET /api/horizon/scout/jobs/{jobId}` - статус ScoutingJob (реализован в `GetScoutingJob`, но с TODO)
     - ✅ Дополнительные endpoints: `/cockpit`, `/assembly-status`, `/simulate`, `/entities/search`, `/co-pilot/chat`
   - ✅ **Реализованы (ранее были заглушками)**:
     - ✅ `GET /api/horizon/fragments/{id}/versions` - история версий (использует репозиторий версий)
     - ✅ `POST /api/horizon/fragments/{id}/refresh` - принудительное обновление (создает ScoutingJob)
     - ✅ `GET /api/horizon/scout/jobs` - список ScoutingJob (использует репозиторий)
     - ✅ `GET /api/horizon/propagation/status` - статус распространения (использует Propagation Engine)
     - ✅ `POST /api/horizon/propagation/trigger` - принудительное распространение (использует Propagation Engine)

### ❌ Не реализовано

1. **Версионирование DependencyEdge**
   - ❌ Репозиторий для версий DependencyEdge не создан
   - ❌ Логика отслеживания изменений ребер не реализована

2. **WebSocket события**
   - ✅ WebSocket endpoint реализован через общий realtime broker (`/ws?token=...`)
   - ✅ Подписка по проекту реализована control message `{"type":"subscribe","projectId":"..."}`
   - ✅ Horizon события публикуются через `realtime.Publisher` и маршрутизируются по `horizon.*` в отдельный Pub/Sub топик `etigram.horizon.events`
   - ✅ Реализованы события: `horizon.assembly.failed`, `horizon.graph.edge.upserted`, `horizon.propagation.node_updated`, `horizon.propagation.completed`
   - ⚠️ Lens-based subscription (server-side фильтрация по lensId) пока не реализована (фильтрация по lensId делается на клиенте)

## Детальные расхождения

### 1. Модель данных HorizonFragment

#### ✅ Соответствует документации

- Все поля соответствуют схеме из `data-model.md`
- Валидация реализована корректно
- `TenantID` всегда "global" - соответствует
- `Governance.Owner` всегда "SYSTEM" - соответствует
- Семантический поиск (`VectorText`, `VectorEmbedding`) - реализован

#### ⚠️ Незначительные расхождения

- В документации указано, что `governance` дублируется в структуре (строки 64-69 и 89-93 в data-model.md), но в реализации это одно поле - это правильно, документация содержит ошибку дублирования

### 2. Модель данных Frame

#### ✅ Соответствует документации

- Все поля соответствуют схеме
- `ClaimProvenance` структура соответствует
- `ValidationMeta` структура соответствует

#### ⚠️ Незначительные расхождения

- В документации `Frame` содержит `tenantId` дважды (строки 129 и 132 в data-model.md) - это ошибка в документации, в реализации корректно

### 3. Scout Engine

#### ✅ Соответствует документации

- Архитектура "Зал Суда" реализована (Scout, Critic, Resolver)
- Workflow состояний соответствует документации
- Интеграция с AgentJob системой реализована

#### ⚠️ Частичные расхождения

1. **Планирование источников** (`scout/engine.go:107-108`)
   - ⚠️ В реализации используется упрощенная логика определения `entityPath` и `sectorPath` (хардкод значений)
   - В документации указано, что Scout должен определять эти пути из таксономии на основе `targetCanonicalPath`
   - **Требуется**: Реализовать логику определения `entityPath` и `sectorPath` из `canonicalPath` или через LLM

2. **Извлечение Evidence** (`scout/evidence_extractor.go`)
   - ✅ Evidence извлекается из источников через функцию `ExtractEvidence`
   - ⚠️ Реализация упрощенная: берет первые 200 символов контента как цитату
   - **Требуется**: Улучшить извлечение релевантных цитат для каждой метрики

3. **Обработка ResolutionResult** (`scout/engine.go:143-168`)
   - ✅ При `Low_Confidence` вердикте код обновляет `extractedFragment` с исправленными данными из `ResolvedData`
   - ✅ Реализация корректна, но может быть улучшена для более полного применения исправлений

### 4. Propagation Engine

#### ✅ Соответствует документации

- Алгоритм Wave Damping реализован
- Delta Threshold реализован
- Lazy Evaluation (Hot Path / Cold Path) реализован
- События публикуются через Realtime

#### ✅ Исправлено / ⚠️ Частичные расхождения

1. **Ограничение диапазона метрик** (`propagation/engine.go:186-187`, `propagation/metric_types.go`)
   - ✅ **ИСПРАВЛЕНО**: Реализована функция `getMetricType` для определения типа метрики (magnitude/directional)
   - ✅ **ИСПРАВЛЕНО**: Функция `clampMetricValue` применяет правильные диапазоны:
     - Magnitude: [0.0, 1.0]
     - Directional: [-1.0, 1.0]
   - ✅ Метрики правильно ограничиваются в зависимости от типа

2. **Cold Path обработка** (`propagation/lazy_eval.go`, `propagation/engine.go:82`)
   - ✅ Реализован `LazyEvaluator` с методами `ScheduleColdPath` и `ProcessColdPath`
   - ⚠️ Cold Path планируется через `ScheduleColdPath`, но автоматическая обработка не запускается
   - **Требуется**: Реализовать фоновый процесс или планировщик для автоматической обработки Cold Path задач

### 5. Lens Subsystem

#### ✅ Соответствует документации

- Управление подписками реализовано
- Жизненный цикл фрагментов управляется через `activeSubscribers`
- Пересчет метрик реализован

#### ✅ Соответствует документации

1. **Создание фрагментов при отсутствии** (`lens/service.go:54-86`)
   - ✅ **РЕАЛИЗОВАНО**: При отсутствии фрагмента создается ScoutingJob с моделью Bounty
   - ✅ ScoutingJob создается с правильными параметрами (`ChargeModelBounty`, `ScoutingStrategySnapshot`)
   - ✅ Соответствует документации (Pioneer/Bounty модель)

### 6. Watchdog Service

#### ✅ Соответствует документации

- Мониторинг источников реализован
- Планирование сканирований реализовано
- Управление жизненным циклом реализовано

#### ⚠️ Частичные расхождения

1. **Использование проектных источников** (`watchdog/service.go:86-94`)
   - В `CreateConfigForFragment` передаются `sourceIDs`, но не используется `useProjectSources`
   - В документации указано, что Watchdog может использовать все Sources проекта через `useProjectSources`
   - **Требуется**: Реализовать логику использования проектных источников

### 7. API Endpoints

#### ✅ Большинство endpoints реализованы

**Статус реализации endpoints**:

1. **Lens endpoints**:
   - ✅ `GET /api/horizon/lenses?projectId={projectId}&status={status}` - список Lens проекта (`ListLenses`)
   - ✅ `POST /api/horizon/lenses/{id}/recalculate` - принудительный пересчет (`RecalculateLens`)

2. **Fragment endpoints**:
   - ✅ `GET /api/horizon/fragments/by-path?path={canonicalPath}` - получение по canonicalPath (`GetFragmentByPath`)
   - ✅ `GET /api/horizon/fragments/{id}/versions` - история версий (использует репозиторий версий)
   - ✅ `POST /api/horizon/fragments/{id}/refresh` - принудительное обновление (создает ScoutingJob)
   - ✅ `GET /api/horizon/fragments/{id}/dependencies` - зависимости узла (`GetFragmentDependencies`)

3. **DependencyEdge endpoints**:
   - ✅ `POST /api/horizon/edges` - создание (`CreateEdge`)
   - ✅ `PUT /api/horizon/edges/{edgeId}` - обновление (`UpdateEdge`)
   - ✅ `DELETE /api/horizon/edges/{edgeId}` - удаление (`DeleteEdge`)

4. **Scout endpoints**:
   - ⚠️ `GET /api/horizon/scout/jobs/{jobId}` - статус ScoutingJob (реализован, но с TODO для progress/steps)
   - ✅ `GET /api/horizon/scout/jobs` - список ScoutingJob (использует репозиторий)

5. **Propagation endpoints**:
   - ✅ `GET /api/horizon/propagation/status` - статус распространения (использует Propagation Engine)
   - ✅ `POST /api/horizon/propagation/trigger` - принудительное распространение (использует Propagation Engine)

#### ✅ Реализованные endpoints (не из документации)

- `GET /api/horizon/lenses/{id}/cockpit` - данные для Cockpit UI
- `GET /api/horizon/lenses/{id}/assembly-status` - статус сборки
- `POST /api/horizon/lenses/{id}/simulate` - симуляция what-if
- `POST /api/horizon/entities/search` - поиск сущностей
- `POST /api/horizon/co-pilot/chat` - Co-Pilot чат

### 8. Версионирование

#### ✅ Реализовано

1. **HorizonFragment версионирование**:
   - ✅ Репозиторий `HorizonFragmentVersionsRepository` создан (`backend/internal/horizon/repo/fragment_versions.go`)
   - ✅ Коллекция `horizon_fragment_versions` используется в репозитории
   - ✅ Методы `Create`, `ListByFragment`, `GetByVersionID` реализованы
   - ✅ **ИНТЕГРИРОВАНО**: Логика сохранения версий интегрирована в `HorizonFragmentsRepository.Update()`
   - ✅ API endpoint реализован и использует репозиторий версий

2. **DependencyEdge версионирование**:
   - ❌ Репозиторий для версий DependencyEdge не создан
   - ❌ Логика отслеживания изменений не реализована

### 9. WebSocket Events

#### ⚠️ Частично реализовано

- События публикуются через `realtime.Publisher`
- Реализация WS: общий `/ws` (через `cmd/etigram-ws`) + project subscription `subscribe`
- Horizon события: namespace `horizon.*` + отдельный Pub/Sub топик `etigram.horizon.events`
- В старой документации встречается `wss://.../ws/risk` — заменить на `/ws` (см. `docs/horizon/realtime.md`)

### 10. Индексы Firestore

#### ✅ Определены в firestore.indexes.json

Все обязательные индексы определены в `web/firestore.indexes.json`:

- ✅ `horizon_fragments`: tenantId + canonicalPath
- ✅ `horizon_fragments`: tenantId + governance.status + updatedAt
- ✅ `horizon_fragments`: tenantId + governance.activeSubscribers + governance.status
- ✅ `horizon_lenses`: tenantId + projectId + status + updatedAt
- ✅ `horizon_dependency_edges`: tenantId + sourceFragmentId + type
- ✅ `horizon_dependency_edges`: tenantId + targetFragmentId + type
- ✅ `horizon_watchdog_configs`: tenantId + status + nextScanAt

**Векторные индексы**:
- ✅ `horizon_fragments`: vectorEmbedding (dimension: 1536, COSINE)

**Проверка индексов**:
```bash
# Проверить существующие индексы
gcloud firestore indexes composite list --project=etigram

# Применить индексы из firestore.indexes.json
firebase deploy --only firestore:indexes
```

## Критические находки

### ✅ Исправлено: Версионирование интегрировано

**Статус**: Исправлено (2025-12-21)

**Что было сделано**:
- Логика сохранения версий интегрирована в `HorizonFragmentsRepository.Update()`
- Версия сохраняется перед обновлением фрагмента
- API endpoint `GetFragmentVersions` использует репозиторий версий

### ✅ Исправлено: API endpoints с заглушками

**Статус**: Исправлено (2025-12-21)

**Что было сделано**:
- `RefreshFragment` - реализован, создает ScoutingJob через Scout Engine
- `ListScoutingJobs` - реализован, использует репозиторий
- `GetPropagationStatus` - реализован, использует Propagation Engine
- `TriggerPropagation` - реализован, использует Propagation Engine
- `GetFragmentVersions` - реализован, использует репозиторий версий

### 3. Scout Engine: упрощенное определение таксономии

**Проблема**: `entityPath` и `sectorPath` определяются через хардкод вместо анализа `canonicalPath`.

**Местоположение**: `backend/internal/horizon/scout/engine.go:107-108`

**Текущая реализация**:
```go
entityPath := "/Organization/Corporation/Public" // Упрощенная реализация
sectorPath := "/Technology/Semiconductors/Manufacturing" // Упрощенная реализация
```

**Требуется**: Реализовать логику определения путей из `canonicalPath` или через LLM.

## Рекомендации по исправлению

### Приоритет 1 (Критично)

1. ✅ **Интегрировать версионирование**: ВЫПОЛНЕНО
2. ✅ **Реализовать endpoints с заглушками**: ВЫПОЛНЕНО
3. **Улучшить Scout Engine**:
   - Определение entityPath и sectorPath из canonicalPath или через LLM

### Приоритет 2 (Важно)

1. **Улучшить Scout Engine**:
   - Определение entityPath и sectorPath из canonicalPath
   - Обработка ResolvedData при Low_Confidence
   - Создание ScoutingJob для отсутствующих фрагментов

2. **Реализовать Cold Path обработку**:
   - Обработка отложенных задач распространения
   - Планирование и выполнение Cold Path задач

### Приоритет 3 (Желательно)

1. **Реализовать WebSocket endpoint**:
   - Создать WebSocket handler для подписки на события
   - Интегрировать с существующей Realtime системой

2. **Улучшить Watchdog**:
   - Реализовать использование проектных источников
   - Улучшить обработку ошибок

3. **Проверить и создать индексы Firestore**:
   - Проверить наличие всех индексов
   - Создать недостающие индексы через `firebase deploy --only firestore:indexes`

## Выводы

### Общая оценка соответствия: ~90%

**Сильные стороны**:
- ✅ Модели данных полностью соответствуют документации
- ✅ Основные компоненты (Scout, Propagation, Lens, Watchdog) реализованы
- ✅ Архитектура соответствует документации
- ✅ Интеграция с существующими системами реализована
- ✅ Большинство API endpoints реализованы
- ✅ Обработка метрик в Propagation Engine исправлена (magnitude/directional)
- ✅ Создание ScoutingJob для отсутствующих фрагментов реализовано
- ✅ Evidence извлекается из источников
- ✅ Версионирование интегрировано
- ✅ Все endpoints с заглушками реализованы

**Области для улучшения**:
- ⚠️ Scout Engine: упрощенное определение entityPath/sectorPath (хардкод)
- ⚠️ Cold Path обработка планируется, но не запускается автоматически
- ⚠️ WebSocket endpoint не реализован
- ⚠️ Версионирование DependencyEdge не реализовано

**Рекомендации**:
1. Улучшить Scout Engine: определение entityPath/sectorPath из canonicalPath (приоритет 1)
2. Реализовать автоматическую обработку Cold Path задач (приоритет 2)
3. Реализовать WebSocket endpoint (приоритет 3)

## Будущие исследования и улучшения

### Таксономия и иерархия

- **Связка org/person/пр - индустрия и иерархия**: 
  - Подумать над связкой между организациями, персонами и программами с индустриями
  - Исследовать иерархическую структуру связей между сущностями и отраслями
  - Рассмотреть возможность создания явных связей между canonical paths и sector paths
  - Определить, как лучше моделировать принадлежность организаций к индустриям

### Географическая и контекстная релевантность

- **"Расстояние" от запроса до horizon fragments**:
  - Исследовать концепцию "расстояния" или релевантности между запросом и horizon fragments
  - При наличии географического интереса выборка не должна касаться всего пула индустриальных игроков
  - Разработать механизм фильтрации по географической близости или релевантности
  - Учесть, что не все индустриальные игроки релевантны для конкретного географического контекста
  - Рассмотреть возможность добавления весов или метрик релевантности для фильтрации результатов

### Сложные технологические зависимости

- **Сложные связки адресов-технологий**:
  - Исследовать моделирование сложных технологических зависимостей
  - Пример: батарейные производители могут быть зависимы от лития, а могут и нет
  - Watchdogs и волны распространения изменений не должны слепо менять всё
  - Разработать механизм условных зависимостей (conditional dependencies)
  - Добавить возможность указания контекста и условий для зависимостей
  - Рассмотреть использование метаданных или атрибутов для описания характера зависимостей

### Распространение сигналов через граф зависимостей

- **Watchdog и соседние пути в графе**:
  - Необходимо, чтобы Watchdog реагировал не только на прямой canonical path, но и на соседние пути в графе зависимостей
  - Путь может не измениться напрямую, но технология или сущность, упомянутая в факте, может влиять на другие сущности через одно плечо зависимости
  - Пример: факт упоминает технологию лития → литий влияет на производителя батарей (DependencyEdge) → производитель батарей влияет на производителя электромобилей → Watchdog должен сработать для производителя электромобилей
  - Разработать механизм обхода графа зависимостей при поиске релевантных фактов
  - Определить глубину обхода (например, 1-2 плеча) для баланса между полнотой и производительностью
  - Интегрировать с Propagation Engine для согласованного поведения
  - Рассмотреть кэширование графа зависимостей для оптимизации запросов

### Оптимизация производительности через батчинг

- **Аккумуляция вызовов Scout и изменений**:
  - Необходимо аккумулировать будущие вызовы Scout и внесения изменений до фактического исполнения
  - Множество изменений может значительно пересекаться (например, один факт может влиять на несколько сущностей через граф зависимостей)
  - Пример: факт про литий может вызвать обновление для 10 производителей батарей, каждый из которых связан с 5 производителями электромобилей → вместо 50 отдельных вызовов Scout можно сделать 1 батч-запрос
  - Разработать механизм очереди и батчинга для ScoutingJob
  - Группировать запросы по источникам данных, типам сущностей или временным окнам
  - Дедуплицировать запросы для одной и той же сущности из разных источников
  - Оптимизировать Propagation Engine для батч-обработки изменений метрик
  - Рассмотреть использование транзакций или batch операций в Firestore для атомарности
  - Определить оптимальный размер батча и стратегию флаширования (по времени, по размеру, по приоритету)

## Следующие шаги

Актуальный backlog и порядок работ ведём в **roadmap**: `docs/horizon/implementation/roadmap.md`.

---

## Snapshot: текущий статус разработки/деплоя (2025‑12‑22)

Эта секция — "точка сохранения" (state capture), чтобы продолжить работу с другого компьютера без контекста.

### TL;DR (что сломано/почему подсказки не работают)

- **Сломано**: `/api/horizon/entities/search` отдавал **500** в проде.
- **Корень проблемы**: мы пытались использовать `filter` в Discovery Engine (Vertex AI Search) вида `structData.type: "horizon_fragment"`, но **в текущем Data Store со схемой `dataSchema=document` фильтрация по `structData.*` не поддерживается**. Любой filter возвращает **400 INVALID_ARGUMENT**, а мы это превращали в 500.
- **Принятое решение**: **store isolation** — отдельный Vertex AI Search Data Store только под Horizon fragments + отдельный GCS bucket под их экспорт. Тогда filter не нужен.
- **Статус (2025-12-22)**: 
  - ✅ Формат экспорта исправлен и задеплоен
  - ✅ Инструмент для re-export создан с поддержкой `--skip-existing`
  - ✅ Скрипт backfill исправлен (BATCH_SIZE=100)
  - ⏳ Re-export запущен и работает (в процессе)

### Что уже сделано в коде (Backend)

#### 1) Выделена конфигурация для Horizon Fragments в Vertex AI Search

- **Новые env vars** (Cloud Run):
  - `HORIZON_FRAGMENTS_BUCKET` — отдельный bucket для JSON‑экспорта фрагментов.
    - В деплое по умолчанию: `${PROJECT_ID}-horizon-fragments` (в проде: `etigram-etigram-fragments`).
  - `VERTEX_SEARCH_HORIZON_DATA_STORE_ID` — отдельный Data Store для фрагментов.
    - В деплое по умолчанию: `horizon-fragments-store`.

#### 2) Vertex AI Search client: переключение Data Store

- В `backend/internal/ai/vertex_search_client.go` добавлен метод:
  - `WithDataStore(dataStoreID string) *VertexAISearchClient` — клонирует клиента, меняя только datastore.

#### 3) Поиск фрагментов: больше не используем `filter`

- В `backend/internal/horizon/service.go`:
  - `SearchFragments()` больше **не ставит `Filter`**.
  - Использует `VERTEX_SEARCH_HORIZON_DATA_STORE_ID` через `WithDataStore(...)` (если задано).
  - Это критично, потому что фильтры по `structData.*` не поддерживаются в текущем формате Data Store.

#### 4) Scout / инструменты: убраны `structData.type: "horizon_fragment"`

Чтобы "скауты и пр" корректно пользовались новым store‑isolation подходом:

- В `backend/internal/horizon/scout/` добавлен helper `horizonVertexClient(...)` (выбирает dedicated Data Store).
- Из `FactApplier`, `Canonicalizer` и инструментов миграции (`cmd/tools/horizon_merge_*`) убраны фильтры `structData.type: "horizon_fragment"`.
  - Сейчас они либо используют dedicated datastore (если env задан), либо fallback на общий.

#### 5) Репозиторий фрагментов: синхронизация в Vertex при каждом Create/Update

- `backend/internal/horizon/repo/fragments.go`:
  - При `Create()` и `Update()` выполняется `uploadToGCS(...)`.
  - После upload запускается `vertexSearchSync(...)` (best‑effort, асинхронно).
  - В этот же файл добавлено логирование ошибок версионирования (не блокирует update).

#### 6) ✅ ИСПРАВЛЕНО: формат экспорта для Discovery Engine

**Статус**: Исправлено и задеплоено (2025-12-22)

**Что было исправлено**:
- Формат экспорта в `backend/internal/horizon/repo/fragments.go` приведен к формату, совместимому с Discovery Engine `dataSchema=document`
- Все поля (`title`, `content`, `uri`) теперь находятся внутри `structData`
- Формат соответствует ожидаемому `google.cloud.discoveryengine.Document`
- Добавлен метод `ObjectExists` в `GCSClient` для проверки существования файлов

**Новый формат экспорта**:
```json
{
  "id": "...",
  "structData": {
    "title": "...",
    "content": "...",
    "uri": "gs://...",
    "tenantId": "...",
    "canonicalPath": "...",
    "type": "horizon_fragment",
    ...
  }
}
```

**Инструмент для re-export**:
- Создан `backend/cmd/tools/horizon_reexport_fragments/main.go` для массового re-export всех фрагментов из Firestore
- Поддержка флага `--skip-existing` для продолжения с места остановки (пропускает уже экспортированные файлы)
- Использование: 
  - `go run ./cmd/tools/horizon_reexport_fragments -project etigram -bucket etigram-etigram-fragments -skip-existing`
  - Или использовать скомпилированный бинарник: `./bin/horizon_reexport_fragments -project etigram -bucket etigram-etigram-fragments -skip-existing`

### Инфраструктура (GCS + Vertex AI Search)

#### 1) Созданы отдельные ресурсы

- **GCS bucket** для фрагментов:
  - `gs://etigram-etigram-fragments`
- **Discovery Engine Data Store** для фрагментов:
  - `horizon-fragments-store` (location: `global`)

#### 2) Деплой API (актуально на 2025‑12‑22)

- Деплой выполнялся командой:

  - `bash scripts/deploy --api`

- Результат:
  - Cloud Run service: `etigram-api`
  - URL: `https://etigram-api-sqt63d2k2q-uc.a.run.app`
  - Revision: `etigram-api-00709-dn4` (последний деплой с исправленным форматом экспорта)
  - Предыдущая ревизия: `etigram-api-00708-xfn` (2025-12-21)

#### 3) Что было сделано вручную/скриптами

- Создание bucket:
  - `gcloud storage buckets create gs://etigram-etigram-fragments --location=US --uniform-bucket-level-access`
- Создание Data Store:
  - REST call `.../dataStores?dataStoreId=horizon-fragments-store` (добавлено и в `scripts/deploy`).

### Backfill (перенос существующих 6999 fragments)

#### 1) Перенос объектов между bucket'ами

Перенос существующих файлов:

- `gcloud storage rsync -r gs://etigram-rag-documents/global/horizon_fragments gs://etigram-etigram-fragments/global/horizon_fragments`

Пояснение:
- `6999` — количество JSON‑файлов (по одному на фрагмент) в папке `global/horizon_fragments/`.

#### 2) Скрипт backfill импорта в Vertex AI Search

Добавлены файлы:
- `scripts/backfill-horizon-fragments-vertex.sh` - основной скрипт для импорта батчами
- `scripts/backfill-horizon-fragments.sh` - wrapper скрипт с проверками

Назначение:
- импортировать файлы **батчами**, чтобы не упираться в лимиты Discovery Engine.

**Исправлено (2025-12-22)**:
- `BATCH_SIZE` по умолчанию изменен с 500 на 100 (лимит Discovery Engine)
- Добавлена проверка лимита <= 100
- Создан wrapper скрипт `scripts/backfill-horizon-fragments.sh` с проверками

Текущие ограничения:
- `gcsSource.inputUris` принимает **максимум 100 URIs** за один import request.

Примеры запуска:
- Простой: `bash scripts/backfill-horizon-fragments.sh`
- С параметрами: `PROJECT=etigram BUCKET=etigram-etigram-fragments DATA_STORE_ID=horizon-fragments-store TENANT=global BATCH_SIZE=100 bash scripts/backfill-horizon-fragments-vertex.sh`

### Frontend (подсказки в AssembleLensRequest)

- В `web/src/features/horizon/components/hook/AssembleLensRequest.tsx`:
  - debounce = **500ms**
  - отмена предыдущих запросов через `AbortController`
  - fetch идёт на `/api/horizon/entities/search` (через `api.searchHorizonEntities`)
- В `web/src/lib/api.ts`:
  - добавлен метод `searchHorizonEntities(query, limit, signal)`

### "Как продолжить с другого компа" (чек‑лист)

1) **GCloud auth + project**
- `gcloud auth login`
- `gcloud config set project etigram`

2) **Проверить наличие ресурсов**
- `gcloud storage buckets describe gs://etigram-etigram-fragments`
- `ACCESS_TOKEN=$(gcloud auth print-access-token) && curl -s -H "Authorization: Bearer $ACCESS_TOKEN" -H "X-Goog-User-Project: sanskrite" "https://discoveryengine.googleapis.com/v1beta/projects/etigram/locations/global/dataStores" | grep horizon-fragments-store`

3) **Деплой API**
- `bash scripts/deploy --api`

4) **Проверка, что env применились**
- `gcloud run services describe etigram-api --region us-central1 --format='value(spec.template.spec.containers[0].env)' | grep -E 'HORIZON_FRAGMENTS_BUCKET|VERTEX_SEARCH_HORIZON_DATA_STORE_ID'`

5) **Re-export фрагментов (если нужно пересоздать файлы в новом формате)**
- Рекомендуемый запуск (ресюмируемый, с чекпоинтом):
  - **Важно**: если в бакете уже есть файлы старого формата, `--skip-existing` нельзя — он НЕ перезапишет их.
  - Первый прогон для гарантированного приведения формата: **без** `--skip-existing`:
    - `./bin/horizon_reexport_fragments -project etigram -bucket etigram-etigram-fragments -tenant global -resume -checkpoint-file /tmp/horizon_reexport_checkpoint.txt -checkpoint-every 100 -heartbeat 30s -stall-timeout 30m`
  - Если процесс прервался — повторить ту же команду (чекпоинт продолжит после последнего docID).
- После того как ты уверен, что формат в бакете уже правильный, можно использовать `--skip-existing` для ускорения повторных прогонов.
- Можно стартовать вручную после конкретного docID:
  - `./bin/horizon_reexport_fragments ... -start-after <FIRESTORE_DOC_ID>`
- `--skip-existing` пропускает уже загруженные JSON в GCS.
- `--resume` читает `checkpoint-file` и продолжает **после последнего Firestore docID** (итерация упорядочена по `__name__`), чтобы не сканировать коллекцию "с нуля".

6) **Backfill (после завершения re-export)**
- Простой запуск: `bash scripts/backfill-horizon-fragments.sh`
- Или с параметрами: `PROJECT=etigram BUCKET=etigram-etigram-fragments DATA_STORE_ID=horizon-fragments-store TENANT=global BATCH_SIZE=100 bash scripts/backfill-horizon-fragments-vertex.sh`
- **Важно**: Дождаться завершения re-export перед запуском backfill
- **Важно (Discovery Engine)**: импорт `dataSchema=document` требует `Document.content` как **объект** (`{"mimeType":"text/plain","rawBytes":"<base64>"}`).
  Если `content` был строкой/лежал только в `structData`, импорты будут падать с ошибкой:
  `To create document without content, content config of data store must be NO_CONTENT.`

7) **Cloud Tasks (после стабилизации/бэкфилла)**
- Очередь Scout Cloud Tasks: `etigram-scout` (region: `us-central1`) — **сейчас PAUSED**.
- Включать обратно **после** завершения бэкфилла/стабилизации:
  - `gcloud tasks queues resume etigram-scout --project=etigram --location=us-central1`

8) **Проверка эндпоинта**
- `curl -X POST "$API_URL/api/horizon/entities/search" -H "Content-Type: application/json" -H "Authorization: Bearer <FIREBASE_ID_TOKEN>" -d '{"query":"Tesla","limit":10}'`

### Индексы Firestore

**Статус**: Определены в `web/firestore.indexes.json`

Все обязательные индексы определены в конфигурации. Для применения:

```bash
# Применить индексы
firebase deploy --only firestore:indexes --project etigram

# Или через gcloud (для векторных индексов)
gcloud firestore indexes composite create \
  --collection-group=horizon_fragments \
  --query-scope=COLLECTION \
  --field-config='field-path=tenantId,order=ASCENDING' \
  --field-config='field-path=vectorEmbedding,vector-config={"dimension":"1536", "flat": "{}"}' \
  --database="(default)" \
  --project=etigram
```

**Проверка индексов**:
```bash
gcloud firestore indexes composite list --project=etigram
```

### Текущий статус re-export (2025-12-22)

**Последний запуск**: 12:32:56
**Режим**: `--skip-existing` + `--resume` (чекпоинт по Firestore docID, чтобы не начинать "вхолостую")
**Файлы**:
- Лог: `/tmp/reexport_final.log`
- Чекпоинт: `/tmp/horizon_reexport_checkpoint.txt`

**История запусков**:
- Первый запуск (2025-12-21, 21:42:25): ~4100 фрагментов экспортировано, упал из-за ошибок Firestore (22:11:29)
- Второй запуск (2025-12-21, 22:35:52): ~14 фрагментов, завершился без статистики
- Третий запуск (2025-12-22): серия запусков с `--skip-existing`; добавлены heartbeat/backoff на `Unavailable`, чтобы не спамить логом и не падать.
- Четвертый запуск (2025-12-22, 12:32:56): добавлен чекпоинт/`--resume` (продолжение после последнего docID).

**Проверка статуса**:
```bash
# Проверить процесс
ps aux | grep horizon_reexport_fragments

# Проверить прогресс
tail -f /tmp/reexport_final.log

# Посмотреть чекпоинт (последний docID)
cat /tmp/horizon_reexport_checkpoint.txt

# Проверить количество файлов в бакете
gcloud storage ls gs://etigram-etigram-fragments/global/horizon_fragments/*.json | wc -l
```

**После завершения re-export**:
- Запустить backfill: `bash scripts/backfill-horizon-fragments.sh`
- Проверить endpoint: `curl -X POST "$API_URL/api/horizon/entities/search" -H "Content-Type: application/json" -H "Authorization: Bearer <FIREBASE_ID_TOKEN>" -d '{"query":"Tesla","limit":10}'`

**Примечание**: Процесс может долго загружать список фрагментов из Firestore (~6999 документов). Это нормально и может занять несколько минут.

### Lessons learned (2025-12-22)

- **Discovery Engine `dataSchema=document` требует `Document.content` как объект**: `{"mimeType":"text/plain","rawBytes":"<base64>"}`. Если положить текст только в `structData` или строкой — импорты падают с `To create document without content...`, и поиск будет пустой.
- **`--skip-existing` нельзя использовать при смене схемы**: он не перезапишет старые JSON в GCS, и backfill продолжит импортировать “старый формат”. Для миграции формата нужен overwrite re-export (без `--skip-existing`) + чекпоинт.
- **Backfill может падать посередине из-за OAuth**: токен истекает/требует reauth (`401 UNAUTHENTICATED`). Решение — обновлять access token на каждом батче и уметь продолжать с `START_BATCH`.
- **Firestore “Unavailable” чаще всего вызван длинным стримом + минутными P99**: полный скан `ORDER_BY __name__ ASC` на `Documents().Next()` может сыпаться. Надёжнее читать страницами (`Limit(pageSize)` + `StartAfter(lastDocID)` + `GetAll()`), плюс чекпоинт/`--resume`.
- **Не давать логам съедать диск**: `tee` в `/tmp/*.log` легко раздувается при tight-loop ошибках. Нужны backoff/троттлинг логов и ротация/очистка больших логов.
- **Cloud Tasks queue `etigram-scout`**: `PAUSED` — это ручное действие (не “пустая очередь”), после стабилизации/бэкфилла нужно явно вернуть в `RESUME`.
