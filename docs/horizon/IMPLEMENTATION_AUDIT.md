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
   - ⚠️ **Реализованы с заглушками/TODO**:
     - ⚠️ `GET /api/horizon/fragments/{id}/versions` - история версий (заглушка, возвращает пустой массив)
     - ⚠️ `POST /api/horizon/fragments/{id}/refresh` - принудительное обновление (заглушка, TODO в коде)
     - ⚠️ `GET /api/horizon/scout/jobs` - список ScoutingJob (заглушка, возвращает пустой массив)
     - ⚠️ `GET /api/horizon/propagation/status` - статус распространения (заглушка, TODO в коде)
     - ⚠️ `POST /api/horizon/propagation/trigger` - принудительное распространение (заглушка, TODO в коде)

### ❌ Не реализовано

1. **Версионирование HorizonFragment**
   - ✅ Репозиторий `HorizonFragmentVersionsRepository` создан (`backend/internal/horizon/repo/fragment_versions.go`)
   - ✅ Методы `Create`, `ListByFragment`, `GetByVersionID` реализованы
   - ❌ **Логика сохранения версий при обновлении НЕ интегрирована**: метод `Update` в `HorizonFragmentsRepository` не вызывает сохранение версии
   - ⚠️ API endpoint `GET /api/horizon/fragments/{id}/versions` реализован, но возвращает заглушку (не использует репозиторий версий)

2. **Версионирование DependencyEdge**
   - ❌ Репозиторий для версий DependencyEdge не создан
   - ❌ Логика отслеживания изменений ребер не реализована

3. **WebSocket события**
   - ❌ WebSocket endpoint для подписки на события не реализован
   - ✅ События публикуются через Realtime (`realtime.Publisher`), но WebSocket endpoint отсутствует

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
   - ⚠️ `GET /api/horizon/fragments/{id}/versions` - история версий (заглушка, не использует репозиторий)
   - ⚠️ `POST /api/horizon/fragments/{id}/refresh` - принудительное обновление (заглушка, TODO)
   - ✅ `GET /api/horizon/fragments/{id}/dependencies` - зависимости узла (`GetFragmentDependencies`)

3. **DependencyEdge endpoints**:
   - ✅ `POST /api/horizon/edges` - создание (`CreateEdge`)
   - ✅ `PUT /api/horizon/edges/{edgeId}` - обновление (`UpdateEdge`)
   - ✅ `DELETE /api/horizon/edges/{edgeId}` - удаление (`DeleteEdge`)

4. **Scout endpoints**:
   - ⚠️ `GET /api/horizon/scout/jobs/{jobId}` - статус ScoutingJob (реализован, но с TODO для progress/steps)
   - ⚠️ `GET /api/horizon/scout/jobs` - список ScoutingJob (заглушка, возвращает пустой массив)

5. **Propagation endpoints**:
   - ⚠️ `GET /api/horizon/propagation/status` - статус распространения (заглушка, TODO)
   - ⚠️ `POST /api/horizon/propagation/trigger` - принудительное распространение (заглушка, TODO)

#### ✅ Реализованные endpoints (не из документации)

- `GET /api/horizon/lenses/{id}/cockpit` - данные для Cockpit UI
- `GET /api/horizon/lenses/{id}/assembly-status` - статус сборки
- `POST /api/horizon/lenses/{id}/simulate` - симуляция what-if
- `POST /api/horizon/entities/search` - поиск сущностей
- `POST /api/horizon/co-pilot/chat` - Co-Pilot чат

### 8. Версионирование

#### ⚠️ Частично реализовано

1. **HorizonFragment версионирование**:
   - ✅ Репозиторий `HorizonFragmentVersionsRepository` создан (`backend/internal/horizon/repo/fragment_versions.go`)
   - ✅ Коллекция `horizon_fragment_versions` используется в репозитории
   - ✅ Методы `Create`, `ListByFragment`, `GetByVersionID` реализованы
   - ❌ **КРИТИЧНО**: Логика сохранения версий при обновлении НЕ интегрирована в `HorizonFragmentsRepository.Update()`
   - ⚠️ API endpoint реализован, но возвращает заглушку (не использует репозиторий версий)

2. **DependencyEdge версионирование**:
   - ❌ Репозиторий для версий DependencyEdge не создан
   - ❌ Логика отслеживания изменений не реализована

### 9. WebSocket Events

#### ⚠️ Частично реализовано

- События публикуются через `realtime.Publisher`
- WebSocket endpoint для подписки не реализован
- В документации указан WebSocket URL: `wss://api.sanskrite.com/ws/risk`

### 10. Индексы Firestore

#### ⚠️ Требуется проверка

- Документация содержит список обязательных индексов
- Необходимо проверить, созданы ли все индексы в Firestore
- Векторные индексы должны создаваться через `gcloud firestore indexes composite create`

## Критические находки

### 1. Версионирование не интегрировано

**Проблема**: Репозиторий `HorizonFragmentVersionsRepository` создан и имеет все необходимые методы, но не используется при обновлении фрагментов.

**Местоположение**: `backend/internal/horizon/repo/fragments.go:109-124`

**Текущая реализация**:
```go
func (r *HorizonFragmentsRepository) Update(ctx context.Context, fragment *models.HorizonFragment) error {
    // ... валидация ...
    fragment.Version++
    // НЕТ сохранения версии в horizon_fragment_versions
    _, err := r.col.Doc(fragment.ID).Set(ctx, fragment)
    return err
}
```

**Требуется**: Добавить сохранение версии перед обновлением фрагмента.

### 2. API endpoints с заглушками

**Проблема**: Несколько endpoints реализованы, но возвращают заглушки вместо реальных данных:

- `GetFragmentVersions` (`fragments.go:175-222`) - возвращает пустой массив
- `RefreshFragment` (`fragments.go:122-173`) - возвращает заглушку с TODO
- `ListScoutingJobs` (`scout.go:59-104`) - возвращает пустой массив
- `GetPropagationStatus` (`propagation.go:11-50`) - возвращает заглушку
- `TriggerPropagation` (`propagation.go:52-88`) - возвращает заглушку

**Требуется**: Реализовать полноценную логику для этих endpoints.

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

1. **Интегрировать версионирование**:
   - Добавить сохранение версии в `HorizonFragmentsRepository.Update()`
   - Использовать `HorizonFragmentVersionsRepository` при обновлении
   - Реализовать полноценный `GetFragmentVersions` endpoint

2. **Реализовать endpoints с заглушками**:
   - `RefreshFragment` - создать ScoutingJob через Scout Engine
   - `ListScoutingJobs` - использовать репозиторий для получения списка
   - `GetPropagationStatus` - получить статус из Propagation Engine
   - `TriggerPropagation` - вызвать Propagation Engine

3. **Улучшить Scout Engine**:
   - Определение entityPath и sectorPath из canonicalPath или через LLM

### Приоритет 2 (Важно)

1. **Реализовать версионирование**:
   - Создать коллекции для версий
   - Реализовать логику сохранения версий
   - Реализовать API для получения истории

2. **Улучшить Scout Engine**:
   - Определение entityPath и sectorPath из canonicalPath
   - Обработка ResolvedData при Low_Confidence
   - Создание ScoutingJob для отсутствующих фрагментов

3. **Реализовать Cold Path обработку**:
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
   - Создать недостающие индексы
   - Создать векторные индексы

## Выводы

### Общая оценка соответствия: ~85%

**Сильные стороны**:
- ✅ Модели данных полностью соответствуют документации
- ✅ Основные компоненты (Scout, Propagation, Lens, Watchdog) реализованы
- ✅ Архитектура соответствует документации
- ✅ Интеграция с существующими системами реализована
- ✅ Большинство API endpoints реализованы
- ✅ Обработка метрик в Propagation Engine исправлена (magnitude/directional)
- ✅ Создание ScoutingJob для отсутствующих фрагментов реализовано
- ✅ Evidence извлекается из источников

**Области для улучшения**:
- ⚠️ Версионирование: репозиторий создан, но не интегрирован в процесс обновления
- ⚠️ Некоторые API endpoints имеют заглушки (versions, refresh, scout jobs list, propagation status/trigger)
- ⚠️ Scout Engine: упрощенное определение entityPath/sectorPath (хардкод)
- ⚠️ Cold Path обработка планируется, но не запускается автоматически
- ⚠️ WebSocket endpoint не реализован

**Рекомендации**:
1. **КРИТИЧНО**: Интегрировать сохранение версий в `HorizonFragmentsRepository.Update()` (приоритет 1)
2. Реализовать полноценные реализации для endpoints с заглушками (приоритет 1)
3. Улучшить Scout Engine: определение entityPath/sectorPath из canonicalPath (приоритет 2)
4. Реализовать автоматическую обработку Cold Path задач (приоритет 2)
5. Реализовать WebSocket endpoint (приоритет 3)

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

1. Создать задачи для реализации отсутствующих endpoints
2. Исправить критические расхождения в логике
3. Реализовать версионирование
4. Обновить документацию с учетом реализованных дополнительных endpoints
5. Проверить и создать все необходимые индексы Firestore

---

## Snapshot: текущий статус разработки/деплоя (2025‑12‑21)

Эта секция — “точка сохранения” (state capture), чтобы продолжить работу с другого компьютера без контекста.

### TL;DR (что сломано/почему подсказки не работают)

- **Сломано**: `/api/horizon/entities/search` отдавал **500** в проде.
- **Корень проблемы**: мы пытались использовать `filter` в Discovery Engine (Vertex AI Search) вида `structData.type: "horizon_fragment"`, но **в текущем Data Store со схемой `dataSchema=document` фильтрация по `structData.*` не поддерживается**. Любой filter возвращает **400 INVALID_ARGUMENT**, а мы это превращали в 500.
- **Принятое решение**: **store isolation** — отдельный Vertex AI Search Data Store только под Horizon fragments + отдельный GCS bucket под их экспорт. Тогда filter не нужен.
- **Текущий блокер**: импорт/индексация Horizon fragments в новый Data Store пока **не завершены/не работают**, потому что:
  - Discovery Engine import ограничивает `gcsSource.inputUris` **максимум 100** значений (не 1000).
  - Наш JSON‑формат экспортируемых fragment‑файлов **не соответствует ожидаемому `google.cloud.discoveryengine.Document` при `dataSchema=document`** (ошибка вида: `invalid JSON ... Document.Content ... unexpected character: '\"'; expected '{'`).

### Что уже сделано в коде (Backend)

#### 1) Выделена конфигурация для Horizon Fragments в Vertex AI Search

- **Новые env vars** (Cloud Run):
  - `HORIZON_FRAGMENTS_BUCKET` — отдельный bucket для JSON‑экспорта фрагментов.
    - В деплое по умолчанию: `${PROJECT_ID}-horizon-fragments` (в проде: `sanskrite-horizon-fragments`).
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

Чтобы “скауты и пр” корректно пользовались новым store‑isolation подходом:

- В `backend/internal/horizon/scout/` добавлен helper `horizonVertexClient(...)` (выбирает dedicated Data Store).
- Из `FactApplier`, `Canonicalizer` и инструментов миграции (`cmd/tools/horizon_merge_*`) убраны фильтры `structData.type: "horizon_fragment"`.
  - Сейчас они либо используют dedicated datastore (если env задан), либо fallback на общий.

#### 5) Репозиторий фрагментов: синхронизация в Vertex при каждом Create/Update

- `backend/internal/horizon/repo/fragments.go`:
  - При `Create()` и `Update()` выполняется `uploadToGCS(...)`.
  - После upload запускается `vertexSearchSync(...)` (best‑effort, асинхронно).
  - В этот же файл добавлено логирование ошибок версионирования (не блокирует update).

> ВАЖНО: сейчас `uploadToGCS(...)` экспортирует JSON, который **пока не проходит импорт** в Discovery Engine при `dataSchema=document`. Это следующий шаг.

### Инфраструктура (GCS + Vertex AI Search)

#### 1) Созданы отдельные ресурсы

- **GCS bucket** для фрагментов:
  - `gs://sanskrite-horizon-fragments`
- **Discovery Engine Data Store** для фрагментов:
  - `horizon-fragments-store` (location: `global`)

#### 2) Деплой API (актуально на 2025‑12‑21)

- Деплой выполнялся командой:

  - `bash scripts/deploy --api`

- Результат:
  - Cloud Run service: `sanskrite-api`
  - URL: `https://sanskrite-api-sqt63d2k2q-uc.a.run.app`
  - Revision: `sanskrite-api-00708-xfn`

#### 3) Что было сделано вручную/скриптами

- Создание bucket:
  - `gcloud storage buckets create gs://sanskrite-horizon-fragments --location=US --uniform-bucket-level-access`
- Создание Data Store:
  - REST call `.../dataStores?dataStoreId=horizon-fragments-store` (добавлено и в `scripts/deploy`).

### Backfill (перенос существующих 6999 fragments)

#### 1) Перенос объектов между bucket’ами

Перенос существующих файлов:

- `gcloud storage rsync -r gs://sanskrite-rag-documents/global/horizon_fragments gs://sanskrite-horizon-fragments/global/horizon_fragments`

Пояснение:
- `6999` — количество JSON‑файлов (по одному на фрагмент) в папке `global/horizon_fragments/`.

#### 2) Скрипт backfill импорта в Vertex AI Search

Добавлен файл:
- `scripts/backfill-horizon-fragments-vertex.sh`

Назначение:
- импортировать файлы **батчами**, чтобы не упираться в лимиты Discovery Engine.

Текущие ограничения (важно для продолжения):
- `gcsSource.inputUris` принимает **максимум 100 URIs** за один import request.
- Поэтому скрипт нужно запускать с `BATCH_SIZE<=100` (сейчас дефолт был 500 → ошибка).

Пример корректного запуска:
- `PROJECT=sanskrite BUCKET=sanskrite-horizon-fragments DATA_STORE_ID=horizon-fragments-store TENANT=global BATCH_SIZE=100 bash scripts/backfill-horizon-fragments-vertex.sh`

### Главный незакрытый блокер: формат экспортируемых JSON для Discovery Engine

Сейчас import в `horizon-fragments-store` падает с ошибкой парсинга `Document.Content`.

Причина:
- экспорт в `backend/internal/horizon/repo/fragments.go` формирует документ вида:
  - `{ "id": "...", "title": "...", "content": "...", "structData": {...} }`
- Discovery Engine при `dataSchema=document` ожидает структуру, совместимую с `google.cloud.discoveryengine.Document`, и поле `content` там **не plain string** (отсюда “expected '{'”).

Следующий шаг (FIX PLAN):
- Привести экспорт к формату, который уже используется у нас для docs‑импорта (см. `scripts/import-documents-to-vertex-search.sh`):
  - `{"id":"...","structData":{"title":"...","content":"...","uri":"...","tenantId":"...","canonicalPath":"...","type":"horizon_fragment", ...}}`
- После этого:
  - новые фрагменты будут индексироваться автоматически через single‑file import
  - backfill можно делать либо:
    - пересозданием файлов в новом bucket (re-export из Firestore), либо
    - конвертацией существующих файлов в корректный JSONL/JSON формат и импортом.

### Frontend (подсказки в AssembleLensRequest)

- В `web/src/features/horizon/components/hook/AssembleLensRequest.tsx`:
  - debounce = **500ms**
  - отмена предыдущих запросов через `AbortController`
  - fetch идёт на `/api/horizon/entities/search` (через `api.searchHorizonEntities`)
- В `web/src/lib/api.ts`:
  - добавлен метод `searchHorizonEntities(query, limit, signal)`

### “Как продолжить с другого компа” (чек‑лист)

1) **GCloud auth + project**
- `gcloud auth login`
- `gcloud config set project sanskrite`

2) **Проверить наличие ресурсов**
- `gcloud storage buckets describe gs://sanskrite-horizon-fragments`
- `ACCESS_TOKEN=$(gcloud auth print-access-token) && curl -s -H "Authorization: Bearer $ACCESS_TOKEN" -H "X-Goog-User-Project: sanskrite" "https://discoveryengine.googleapis.com/v1beta/projects/sanskrite/locations/global/dataStores" | grep horizon-fragments-store`

3) **Деплой API**
- `bash scripts/deploy --api`

4) **Проверка, что env применились**
- `gcloud run services describe sanskrite-api --region us-central1 --format='value(spec.template.spec.containers[0].env)' | grep -E 'HORIZON_FRAGMENTS_BUCKET|VERTEX_SEARCH_HORIZON_DATA_STORE_ID'`

5) **Backfill (после фикса формата экспорта)**
- Если нужно импортировать существующие файлы:
  - `PROJECT=sanskrite BUCKET=sanskrite-horizon-fragments DATA_STORE_ID=horizon-fragments-store TENANT=global BATCH_SIZE=100 bash scripts/backfill-horizon-fragments-vertex.sh`

6) **Проверка эндпоинта**
- `curl -X POST "$API_URL/api/horizon/entities/search" -H "Content-Type: application/json" -H "Authorization: Bearer <FIREBASE_ID_TOKEN>" -d '{"query":"Tesla","limit":10}'`


