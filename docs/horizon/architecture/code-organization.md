# Организация кода в моноядре

## Принципы организации

### 1. Изоляция через отдельный модуль
- Новый модуль `backend/internal/horizon/` — полностью изолированная логика мониторинга горизонта
- Четкие границы между основной системой и horizon
- Минимальные зависимости от основной системы (только через интерфейсы)

### 2. Переиспользование через dependency injection
- Использование существующих компонентов через интерфейсы
- Нет прямых зависимостей на конкретные реализации
- Возможность тестирования с моками

### 3. Двусторонняя интеграция
- Основная система может использовать артефакты horizon (HorizonFragment, Frame)
- Horizon использует инфраструктуру основной системы (AI, Agent, Realtime)

## Структура модуля

```
backend/internal/horizon/
├── models/              # Модели данных horizon
│   ├── horizon_fragment.go
│   ├── frame.go
│   ├── lens.go
│   ├── dependency_edge.go
│   └── watchdog.go
├── scout/              # Scout Engine подсистема
│   ├── scout_agent.go
│   ├── critic_agent.go
│   ├── resolver_agent.go
│   ├── planner.go
│   └── executor.go
├── propagation/        # Propagation Engine подсистема
│   ├── engine.go
│   ├── wave_damping.go
│   ├── delta_threshold.go
│   └── lazy_evaluator.go
├── watchdog/          # Watchdog подсистема
│   ├── scheduler.go
│   ├── scanner.go
│   └── config.go
├── lens/              # Lens Subsystem
│   ├── service.go
│   ├── calculator.go
│   └── view_builder.go
├── repo/              # Репозитории для horizon
│   ├── horizon_fragments.go
│   ├── frames.go
│   ├── lenses.go
│   ├── dependency_edges.go
│   └── watchdog_configs.go
├── service.go         # Главный сервис horizon (orchestrator)
└── interfaces.go      # Интерфейсы для dependency injection
```

## Интеграция с существующими модулями

### Использование через интерфейсы

```go
// backend/internal/horizon/interfaces.go

package horizon

import (
    "context"
    "github.com/igorkirisyuk/sanskrite/internal/ai"
    "github.com/igorkirisyuk/sanskrite/internal/agents"
    "github.com/igorkirisyuk/sanskrite/internal/realtime"
    "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HorizonServiceDependencies содержит зависимости от основной системы
type HorizonServiceDependencies struct {
    // AI система
    AIExecutor      ai.TaskExecutor
    EmbeddingProvider ai.EmbeddingProvider
    
    // Agent система
    AgentRegistry   agents.Registry
    AgentJobsRepo  *repo.AgentJobsRepository
    
    // Realtime система
    RealtimePublisher realtime.Publisher
    
    // Репозитории основной системы (для связи с Entity, Document)
    EntitiesRepo    *repo.EntityRepository
    DocumentsRepo   *repo.DocumentsRepository
    
    // Другие зависимости
    Crawler         CrawlerInterface  // Абстракция над internal/crawler
    Logger          LoggerInterface
}

// HorizonService - главный сервис модуля
type HorizonService struct {
    deps HorizonServiceDependencies
    
    // Подсистемы
    scout      *ScoutEngine
    propagation *PropagationEngine
    watchdog   *WatchdogService
    lens       *LensService
    
    // Репозитории horizon
    fragmentsRepo *HorizonFragmentsRepository
    framesRepo    *FramesRepository
    lensesRepo    *LensesRepository
    edgesRepo     *DependencyEdgesRepository
}
```

### Репозитории в отдельном пакете

```go
// backend/internal/horizon/repo/horizon_fragments.go

package repo

import (
    "context"
    "cloud.google.com/go/firestore"
    "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type HorizonFragmentsRepository struct {
    col *firestore.CollectionRef
}

func NewHorizonFragmentsRepository(client *firestore.Client) *HorizonFragmentsRepository {
    return &HorizonFragmentsRepository{
        col: client.Collection("horizon_fragments"),
    }
}

func (r *HorizonFragmentsRepository) Get(ctx context.Context, id string) (*models.HorizonFragment, error) {
    // ...
}

func (r *HorizonFragmentsRepository) GetByCanonicalPath(ctx context.Context, tenantID, path string) (*models.HorizonFragment, error) {
    // ...
}

func (r *HorizonFragmentsRepository) Upsert(ctx context.Context, fragment *models.HorizonFragment) error {
    // ...
}

func (r *HorizonFragmentsRepository) SearchByVector(ctx context.Context, tenantID string, vector []float64, topK int) ([]*models.HorizonFragment, error) {
    // Использует Firestore Vector Search
    // ...
}
```

## HTTP handlers

### Вариант 1: Отдельный файл в `internal/http/`

```
backend/internal/http/
├── horizon.go            # HTTP handlers для horizon
├── agents.go
├── ontology.go
└── ...
```

**Преимущества**: Простота, все handlers в одном месте  
**Недостатки**: Файл может стать большим

### Вариант 2: Подпапка `internal/http/horizon/`

```
backend/internal/http/
├── horizon/
│   ├── handlers.go      # Основные handlers, роутинг
│   ├── lenses.go        # Lens CRUD: /api/horizon/lenses
│   ├── fragments.go     # HorizonFragment queries: /api/horizon/fragments
│   ├── scout.go         # Scout Engine: /api/horizon/scout
│   ├── propagation.go   # Propagation Engine: /api/horizon/propagation
│   ├── watchdog.go      # Watchdog: /api/horizon/watchdog
│   ├── entities.go      # Entity search: /api/horizon/entities/search
│   ├── assembly.go      # Assembly status: /api/horizon/lenses/:id/assembly-status
│   ├── cockpit.go      # Cockpit data: /api/horizon/lenses/:id/cockpit
│   ├── simulate.go      # What-If simulation: /api/horizon/lenses/:id/simulate
│   └── co_pilot.go      # Co-Pilot chat: /api/horizon/co-pilot/*
├── agents.go
├── ontology.go
└── ...
```

**Структура API путей**:
- `/api/horizon/lenses` — управление Lens
- `/api/horizon/fragments` — работа с HorizonFragment
- `/api/horizon/scout` — Scout Engine endpoints
- `/api/horizon/propagation` — Propagation Engine endpoints
- `/api/horizon/watchdog` — Watchdog endpoints
- `/api/horizon/entities/search` — Live Entity Recognition
- `/api/horizon/lenses/:id/assembly-status` — статус сборки графа
- `/api/horizon/lenses/:id/cockpit` — данные для Cockpit UI
- `/api/horizon/lenses/:id/simulate` — What-If симуляция
- `/api/horizon/co-pilot/*` — Co-Pilot чат endpoints

**Преимущества**: Лучшая изоляция, легче поддерживать  
**Недостатки**: Больше файлов

**Рекомендация**: Вариант 2 (подпапка) для лучшей изоляции

## Модели данных

### Вариант 1: Отдельный файл в `internal/models/`

```
backend/internal/models/
├── models.go
├── ontology.go
└── horizon.go            # Модели horizon
```

**Преимущества**: Все модели в одном месте  
**Недостатки**: Смешивание с основной системой

### Вариант 2: Отдельный пакет `internal/horizon/models/`

```
backend/internal/horizon/models/
├── horizon_fragment.go
├── frame.go
├── lens.go
└── ...
```

**Преимущества**: Полная изоляция, четкие границы  
**Недостатки**: Больше пакетов

**Рекомендация**: Вариант 2 (отдельный пакет) для изоляции

## Связь с основной системой

### Основная система использует артефакты horizon

```go
// backend/internal/ontology/extractor.go

package ontology

import (
    "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
    "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type OntologyExtractor struct {
    // ...
    horizonFragmentsRepo *repo.HorizonFragmentsRepository
}

// Использование HorizonFragment для обогащения Entity
func (e *OntologyExtractor) EnrichFromHorizonFragment(ctx context.Context, entity *models.Entity, fragment *horizonmodels.HorizonFragment) {
    // Используем метрики из HorizonFragment для обогащения Entity
    // ...
}
```

### Horizon использует инфраструктуру основной системы

```go
// backend/internal/horizon/scout/scout_agent.go

package scout

import (
    "github.com/igorkirisyuk/sanskrite/internal/ai"
    "github.com/igorkirisyuk/sanskrite/internal/agents"
)

type ScoutAgent struct {
    aiExecutor ai.TaskExecutor
    // ...
}

func (a *ScoutAgent) ExtractData(ctx context.Context, sources []Source) (*Frame, error) {
    // Использует aiExecutor для LLM запросов
    result, err := a.aiExecutor.ExecuteTask(ctx, ai.Task{
        Provider: "openai",
        Model: "gpt-5.2-pro",
        // ...
    })
    // ...
}
```

## Инициализация в main.go

```go
// backend/cmd/server/main.go

package main

import (
    "github.com/igorkirisyuk/sanskrite/internal/horizon"
    "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
    // ...
)

func main() {
    // ... существующая инициализация
    
    // Инициализация horizon модуля
    horizonDeps := horizon.HorizonServiceDependencies{
        AIExecutor:       aiExecutor,
        EmbeddingProvider: embeddingProvider,
        AgentRegistry:    agentRegistry,
        AgentJobsRepo:    agentJobsRepo,
        RealtimePublisher: realtimePublisher,
        EntitiesRepo:     entitiesRepo,
        DocumentsRepo:    documentsRepo,
        Crawler:          crawler,
        Logger:           logger,
    }
    
    // Репозитории horizon
    horizonFragmentsRepo := repo.NewHorizonFragmentsRepository(firestoreClient)
    horizonFramesRepo := repo.NewFramesRepository(firestoreClient)
    horizonLensesRepo := repo.NewLensesRepository(firestoreClient)
    horizonEdgesRepo := repo.NewDependencyEdgesRepository(firestoreClient)
    horizonWatchdogRepo := repo.NewWatchdogConfigsRepository(firestoreClient)
    
    // Главный сервис
    horizonService := horizon.NewHorizonService(horizonDeps, horizonFragmentsRepo, horizonFramesRepo, horizonLensesRepo, horizonEdgesRepo, horizonWatchdogRepo)
    
    // HTTP handlers
    horizonHandlers := http.NewHorizonHandlers(horizonService, auth)
    
    // Регистрация маршрутов под путем /api/horizon
    router.Handle("/api/horizon/", horizonHandlers)
    // Все endpoints доступны под /api/horizon/*
    
    // Запуск фоновых сервисов
    go horizonService.Watchdog().Start(ctx)
    go horizonService.Propagation().Start(ctx)
}
```

## Frontend интеграция

### Структура фронтенда под путем `/horizon`

```
web/src/
├── features/
│   ├── horizon/        # Новый feature модуль под /horizon
│   │   ├── components/
│   │   │   ├── hook/                    # The Hook (вход)
│   │   │   │   ├── EntitySearchInput.tsx
│   │   │   │   ├── EntitySuggestions.tsx
│   │   │   │   └── LensAssembler.tsx
│   │   │   ├── assembly/                # The Sled (сборка графа)
│   │   │   │   ├── AssemblyView.tsx
│   │   │   │   ├── PhaseIndicator.tsx
│   │   │   │   ├── ScoutingLogs.tsx
│   │   │   │   └── GraphBuilder.tsx
│   │   │   ├── cockpit/                 # The Cockpit (профессиональный интерфейс)
│   │   │   │   ├── CockpitLayout.tsx
│   │   │   │   ├── Canvas.tsx           # Центральный граф (Impact River)
│   │   │   │   ├── Inspector.tsx        # Правая панель (контекстная)
│   │   │   │   ├── Navigation.tsx       # Левая панель (Lens список, Live Feed)
│   │   │   │   ├── SimulationBar.tsx    # Нижняя панель (What-If Mode)
│   │   │   │   ├── FragmentCard.tsx
│   │   │   │   ├── DependencyGraph.tsx
│   │   │   │   └── ImpactRiver.tsx
│   │   │   └── co-pilot/                # Co-Pilot (чат)
│   │   │       ├── CoPilotPanel.tsx
│   │   │       ├── ChatInterface.tsx
│   │   │       └── CommandHandler.tsx
│   │   ├── pages/
│   │   │   ├── HorizonHookPage.tsx      # /horizon (The Hook)
│   │   │   ├── HorizonAssemblyPage.tsx   # /horizon/assemble/:lensId (The Sled)
│   │   │   └── HorizonCockpitPage.tsx   # /horizon/cockpit/:lensId (The Cockpit)
│   │   ├── stores/
│   │   │   ├── horizonStore.ts
│   │   │   ├── assemblyStore.ts
│   │   │   └── cockpitStore.ts
│   │   ├── hooks/
│   │   │   ├── useLens.ts
│   │   │   ├── useHorizonFragment.ts
│   │   │   ├── useAssemblyStatus.ts
│   │   │   ├── useCockpit.ts
│   │   │   └── useCoPilot.ts
│   │   ├── api/
│   │   │   └── horizonApi.ts
│   │   └── types/
│   │       └── horizon.ts
│   ├── ontology/              # Существующий модуль
│   └── discovery/             # Существующий модуль
└── ...
```

**Структура путей**:
- `/horizon` — главная страница (The Hook)
- `/horizon/assemble/:lensId` — экран сборки графа (The Sled)
- `/horizon/cockpit/:lensId` — профессиональный интерфейс (The Cockpit)
- `/horizon/lenses` — список всех Lens проекта

### Использование артефактов horizon в основной системе

```typescript
// web/src/features/ontology/components/EntityCard.tsx

import { useHorizonFragment } from '@/features/horizon/api/horizonApi'

export function EntityCard({ entity }: { entity: Entity }) {
    // Используем HorizonFragment для отображения метрик
    const fragment = useHorizonFragment(entity.id)
    
    return (
        <div>
            <h3>{entity.canonicalName}</h3>
            {fragment && (
                <div>
                    <MetricDisplay metrics={fragment.state} />
                </div>
            )}
        </div>
    )
}
```

## Ограничения и правила

### 1. Нет прямых зависимостей на конкретные реализации
- Используем только интерфейсы из основной системы
- Dependency injection для всех зависимостей

### 2. Четкие границы модулей
- Horizon не знает о внутренней структуре основной системы
- Основная система знает только о публичных интерфейсах horizon

### 3. Общие модели через отдельный пакет
- Если нужны общие типы, создаем `internal/shared/` или `internal/common/`
- Или используем интерфейсы

### 4. Версионирование API
- HTTP API размещается под путем `/api/horizon/`
- Версионирование схем данных через поле `schemaVersion` в моделях
- Внутренние интерфейсы стабильны

## Миграция и развертывание

### Поэтапное развертывание
1. **Этап 1**: Создание структуры модуля, модели данных, репозитории
2. **Этап 2**: Scout Engine (без интеграции с основной системой)
3. **Этап 3**: Propagation Engine, Watchdog
4. **Этап 4**: Lens Subsystem
5. **Этап 5**: Интеграция с основной системой
6. **Этап 6**: Frontend интеграция

### Feature flags
```go
// Использование feature flags для постепенного включения
if config.Features.HorizonEnabled {
    horizonService := horizon.NewHorizonService(...)
    // ...
}
```

## Преимущества предложенной структуры

1. **Изоляция**: Четкие границы между модулями
2. **Переиспользование**: Использование существующей инфраструктуры через интерфейсы
3. **Тестируемость**: Легко тестировать с моками
4. **Масштабируемость**: Легко добавлять новые подсистемы
5. **Поддерживаемость**: Понятная структура кода

## См. также

- [Архитектурный обзор](overview.md)
- [Интеграция с Sanskrite](integration.md)
- [Модель данных](data-model.md)
- [Типовой UX-сценарий](../ux/user-journey.md)

