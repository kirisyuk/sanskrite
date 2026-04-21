# Интеграция с существующими компонентами Sanskrite

## Обзор

Система мониторинга горизонта интегрирована в моноядро Sanskrite и активно использует существующую инфраструктуру. Данный документ описывает детали интеграции с каждым компонентом.

**Ключевые интеграции**:
- **Project**: Все запросы привязаны к Project для изоляции
- **DataSource**: Использование существующих Sources как источников данных для Scout
- **Document**: Frame ссылается на Document для прослеживаемости
- **AI, Agent, Realtime**: Использование существующей инфраструктуры

Подробнее см. [Интеграция с Project и DataSource](project-integration.md).

## AI система (`internal/ai`)

### Использование AITaskExecutor

**Назначение**: Выполнение LLM запросов для агентов Scout, Critic, Resolver

**Интеграция**:
```go
// Пример использования в Scout Agent
executor := ai.NewAITaskExecutor(aiServices, modelManager)
result, err := executor.ExecuteTask(ctx, ai.Task{
    Provider: "openai",
    Model: "gpt-5.2-pro",
    Prompt: scoutPrompt,
    Constraints: ai.Constraints{
        JSONMode: true,
        Schema: horizonFragmentSchema,
    },
})
```

**Особенности**:
- Использование `Constraints.JSONMode` для constrained decoding
- Передача Pydantic-схем через `Constraints.Schema`
- Поддержка всех провайдеров (OpenAI, Claude, Vertex AI, Grok)

### Модели для агентов

**Scout Agent**:
- Основные: GPT-5.2, Claude 4.5, Gemini 3 Pro
- Использование через `AITaskExecutor` с JSON mode

**Critic Agent**:
- Основная: Claude 4.5 Opus
- Анализ соответствия данных источникам

**Resolver Agent**:
- Основные: GPT-5.2, Claude 4.5
- Финальное решение на основе аргументов Scout и Critic

### Версионирование моделей

Модели версионируются в `validationMeta` Frame:
```go
type ValidationMeta struct {
    ScoutLLMVersion  string `json:"scoutLlmVersion"`
    CriticLLMVersion string `json:"criticLlmVersion"`
    // ...
}
```

## Agent система (`internal/agents`)

### Расширение через новые агенты

**Новые агенты**:
- `ScoutAgent` — расширяет `TaskAgent` интерфейс
- `CriticAgent` — расширяет `TaskAgent` интерфейс
- `ResolverAgent` — расширяет `TaskAgent` интерфейс

**Регистрация**:
```go
registry := agents.NewRegistry(executor, genaiClient, publisher, logger)
registry.RegisterTaskAgent(NewScoutAgent(genaiClient, logger))
registry.RegisterTaskAgent(NewCriticAgent(genaiClient, logger))
registry.RegisterTaskAgent(NewResolverAgent(genaiClient, logger))
```

### Использование AgentJob

**ScoutingJob как AgentJob**:
- Состояния ScoutingJob маппятся на состояния AgentJob
- Использование существующей системы отслеживания прогресса
- Интеграция с WebSocket для realtime обновлений

**Маппинг состояний**:
```
PENDING → AgentJobStatusPending
PLANNING → AgentJobStatusRunning
EXECUTION → AgentJobStatusRunning
EXTRACTION → AgentJobStatusRunning
VALIDATION → AgentJobStatusRunning
RESOLUTION → AgentJobStatusRunning
VERIFIED → AgentJobStatusCompleted
REJECTED → AgentJobStatusFailed
```

### Capabilities для Scout Workflow

**Новые capabilities**:
- `scout_planning` — планирование источников данных
- `scout_extraction` — извлечение данных через LLM
- `scout_validation` — валидация данных через Critic
- `scout_resolution` — разрешение конфликтов через Resolver

**Интеграция с существующими capabilities**:
- `web_search` — для поиска источников
- `document_read` — для чтения документов
- `llm_generate` — для генерации через LLM

## Discovery система (`internal/discovery`)

### Использование существующей инфраструктуры обработки Sources

**Назначение**: Использование существующих DataSource и инфраструктуры их обработки

**Интеграция**:
```go
// Scout Executor использует существующую инфраструктуру
type ScoutExecutor struct {
    crawler        *crawler.Crawler
    poller         *discovery.PollerWorker
    documentsRepo  *repo.DocumentsRepository
    chunksRepo     *repo.ChunksRepository
    dataSourcesRepo *repo.DataSourcesRepository
}

// Обработка DataSource через существующую инфраструктуру
func (e *ScoutExecutor) ExecuteDataSource(ctx context.Context, ds *models.DataSource) (*SourceContent, string, error) {
    switch ds.Kind {
    case "web":
        // Используем существующий crawler
        doc, err := e.crawler.Fetch(ctx, *ds.Details.URI)
        // Сохраняем как Document
    case "rss":
        // Используем существующий poller
    case "telegram_channel":
        // Используем существующую интеграцию
    case "document":
        // Используем существующий Document и Chunks
    }
}
```

**Использование в Scout Engine**:
- Получение Sources проекта через `DataSourcesRepository.ListByProject()`
- Получение глобальных Sources (web, rss, telegram_channel)
- Фильтрация релевантных Sources для целевого узла
- Использование существующей инфраструктуры обработки (crawler, poller)
- Сохранение результатов как Document для прослеживаемости

### Использование Sources проекта

**Scout Planner получает Sources проекта**:
```go
// Получение Sources проекта
sources, err := dataSourcesRepo.ListByProject(ctx, tenantID, projectID)

// Получение глобальных Sources
globalSources, err := dataSourcesRepo.ListGlobal(ctx)

// Фильтрация релевантных Sources
relevantSources := filterRelevantSources(sources, targetCanonicalPath)
```

### Source Profiling

**Связь с SourceProfile**:
- Использование существующей системы профилирования источников
- Оценка надежности источников (`trustLevel` из DataSource)
- Интеграция с `SourceProfile` для метаданных источников

**В Frame**:
```go
type Frame struct {
    SourceIDs      []string `json:"sourceIds"`      // DataSource IDs
    DocumentIDs    []string `json:"documentIds"`    // Document IDs
    Provenance     ClaimProvenance `json:"provenance"`
}

type ClaimProvenance struct {
    DataSourceIDs []string `json:"dataSourceIds"` // DataSource IDs
    DocumentIDs   []string `json:"documentIds"`   // Document IDs
    // ...
}
```

## Ontology система (`internal/ontology`)

### Связь HorizonFragment с Entity

**Маппинг**:
- HorizonFragment может быть связан с Entity через `entityId`
- Использование существующей онтологии для обогащения данных
- Канонические имена и алиасы из Entity

**Пример**:
```go
type HorizonFragment struct {
    // ... основные поля
    EntityID *string `json:"entityId,omitempty"`
}

// При создании HorizonFragment
if entity := findEntityByCanonicalPath(path); entity != nil {
    fragment.EntityID = &entity.ID
    fragment.CanonicalName = entity.CanonicalName
    fragment.Aliases = entity.Aliases
}
```

### Использование Entity Resolver

**Назначение**: Разрешение канонических имен и алиасов

**Интеграция**:
- Использование `EntityResolver` для нормализации имен
- Дедупликация узлов через существующую систему разрешения сущностей
- Связь с Wikidata через существующие интеграции

### DependencyEdge и Relation

**Связь**:
- DependencyEdge может быть связан с Relation из онтологии
- Использование существующих типов отношений
- Расширение Relation метриками влияния (criticality, amplification)

## Realtime система (`internal/realtime`)

### Pub/Sub события

**События для Horizon (актуально)**:

- `horizon.assembly.*` — сборка Lens (Hook → Sled: stage/progress/ready; `phase` is legacy alias)
- `horizon.scout.job.*` — создание и выполнение Scout jobs (state/log/completed)
- `horizon.graph.node.upserted` — материализация/обновление узла в графе
- `horizon.propagation.started` — начало распространения
- `horizon.lens.updated` — обновление метрик Lens

**Routing**:
- Все `horizon.*` события маршрутизируются в отдельный Pub/Sub топик `horizon.core.events`.
- Для WS delivery требуется `projectId` в payload (WS клиенты подписываются по projectId).
- `tenantId` используется как security namespace (коннекты bucket'ятся по tenant).

**Публикация событий**:
```go
_ = publisher.PublishJSON(ctx, realtime.EventType("horizon.graph.node.upserted"), tenantID, map[string]interface{}{
    "tenantId": tenantID,
    "projectId": projectID,
    "lensId": lensID,
    "fragmentId": fragmentID,
    "canonicalPath": canonicalPath,
    "status": "cached",
    "ts": time.Now().UTC().Format(time.RFC3339Nano),
})
```

### WebSocket обновления

**Интеграция с существующей системой**:
- Использование существующего WebSocket hub
- Подписка на события через control message `subscribe` по `projectId`
- Lens/Lens-assembly события поставляются в реальном времени

Подробнее: [Realtime (implementation)](../realtime.md).

## Репозитории (`internal/repo`)

### Новые репозитории

**HorizonFragmentRepository**:
```go
type HorizonFragmentRepository struct {
    col *firestore.CollectionRef
}

func NewHorizonFragmentRepository(db *firestore.Client) *HorizonFragmentRepository {
    return &HorizonFragmentRepository{
        col: db.Collection("horizon_fragments"),
    }
}
```

**FrameRepository**:
```go
type FrameRepository struct {
    col *firestore.CollectionRef
}

func NewFrameRepository(db *firestore.Client) *FrameRepository {
    return &FrameRepository{
        col: db.Collection("frames"),
    }
}
```

**LensRepository**:
```go
type LensRepository struct {
    col *firestore.CollectionRef
}

func NewLensRepository(db *firestore.Client) *LensRepository {
    return &LensRepository{
        col: db.Collection("lenses"),
    }
}
```

**DependencyEdgeRepository**:
```go
type DependencyEdgeRepository struct {
    col *firestore.CollectionRef
}

func NewDependencyEdgeRepository(db *firestore.Client) *DependencyEdgeRepository {
    return &DependencyEdgeRepository{
        col: db.Collection("dependency_edges"),
    }
}
```

### Паттерны репозиториев

**Следование существующим паттернам**:
- Использование `firestore.CollectionRef` для доступа к коллекциям
- Методы `Create`, `Get`, `Update`, `Delete`, `List`
- Поддержка транзакций для атомарных операций
- Индексы для эффективных запросов

**Пример**:
```go
func (r *HorizonFragmentRepository) GetByCanonicalPath(ctx context.Context, tenantID, path string) (*HorizonFragment, error) {
    doc, err := r.col.Where("tenantId", "==", tenantID).
        Where("canonicalPath", "==", path).
        Documents(ctx).
        Next()
    if err != nil {
        return nil, err
    }
    
    var fragment HorizonFragment
    if err := doc.DataTo(&fragment); err != nil {
        return nil, err
    }
    
    return &fragment, nil
}
```

## Мультитенантность

### TenantID во всех моделях

**Реализация**:
- Все модели содержат поле `tenantID`
- Firestore коллекции используют `tenantID` как часть ключа
- Изоляция данных на уровне репозиториев

**Проверка доступа**:
```go
func (r *HorizonFragmentRepository) Get(ctx context.Context, tenantID, id string) (*HorizonFragment, error) {
    doc, err := r.col.Doc(id).Get(ctx)
    if err != nil {
        return nil, err
    }
    
    var fragment HorizonFragment
    if err := doc.DataTo(&fragment); err != nil {
        return nil, err
    }
    
    // Проверка tenantID
    if fragment.TenantID != tenantID {
        return nil, ErrNotFound
    }
    
    return &fragment, nil
}
```

### Связь с Project

**Lens связан с Project**:
- Lens всегда имеет `projectID`
- Контроль доступа через Project
- Изоляция данных на уровне проекта

## HTTP handlers (`internal/http`)

### Новые handlers

**HorizonHandler**:
```go
type HorizonHandler struct {
    lensRepo        *repo.LensRepository
    fragmentRepo    *repo.HorizonFragmentRepository
    edgeRepo        *repo.DependencyEdgeRepository
    scoutEngine     *scout.Engine
    propagationEngine *propagation.Engine
    lensService     *lens.Service
}

func NewHorizonHandler(
    lensRepo *repo.LensRepository,
    fragmentRepo *repo.HorizonFragmentRepository,
    edgeRepo *repo.DependencyEdgeRepository,
    scoutEngine *scout.Engine,
    propagationEngine *propagation.Engine,
    lensService *lens.Service,
) *HorizonHandler {
    return &HorizonHandler{
        lensRepo: lensRepo,
        fragmentRepo: fragmentRepo,
        edgeRepo: edgeRepo,
        scoutEngine: scoutEngine,
        propagationEngine: propagationEngine,
        lensService: lensService,
    }
}
```

**Интеграция с существующими handlers**:
- Использование существующих middleware для аутентификации
- Проверка доступа через `Project` и `TenantID`
- Следование существующим паттернам обработки ошибок

## Графовая база данных

### Отложенное решение

**Текущее решение**: Firestore с индексами для связей

**Причины**:
- Простота интеграции с существующей инфраструктурой
- Достаточная производительность для начального этапа
- Возможность миграции на графовую БД в будущем

**Ограничения**:
- Сложные графовые запросы (например, поиск путей) требуют дополнительной логики
- Масштабирование для очень больших графов может быть проблематичным

**Будущее решение**:
- Neo4j или Amazon Neptune для графовых запросов
- Firestore остается для основных данных
- Синхронизация между системами

### Временная реализация

**Хранение связей в Firestore**:
- DependencyEdge хранятся как отдельные документы
- Индексы для эффективных запросов по source/target
- Вычисление путей через рекурсивные запросы или батчи

**Пример запроса зависимых узлов**:
```go
func (r *DependencyEdgeRepository) GetDependentNodes(ctx context.Context, tenantID, fragmentID string) ([]string, error) {
    docs, err := r.col.Where("tenantId", "==", tenantID).
        Where("sourceFragmentId", "==", fragmentID).
        Documents(ctx).
        GetAll()
    if err != nil {
        return nil, err
    }
    
    var dependentIDs []string
    for _, doc := range docs {
        var edge DependencyEdge
        if err := doc.DataTo(&edge); err != nil {
            continue
        }
        dependentIDs = append(dependentIDs, edge.TargetFragmentID)
    }
    
    return dependentIDs, nil
}
```

## См. также

- [Архитектурный обзор](overview.md)
- [Модель данных](data-model.md)
- [Компоненты системы](components.md)
- [Roadmap реализации](../implementation/roadmap.md)

