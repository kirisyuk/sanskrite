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

## Следующие шаги

1. Создать задачи для реализации отсутствующих endpoints
2. Исправить критические расхождения в логике
3. Реализовать версионирование
4. Обновить документацию с учетом реализованных дополнительных endpoints
5. Проверить и создать все необходимые индексы Firestore

