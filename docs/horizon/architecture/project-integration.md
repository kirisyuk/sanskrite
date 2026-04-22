# Интеграция с Project и DataSource

## Обзор

Horizon использует общесистемную модель проектов и источников данных для изоляции, переиспользования данных и обработки источников.

## Принципы интеграции

### 1. Общие данные с переиспользованием
- **HorizonFragment всегда глобальные** (`tenantId: "global"`) - это общие данные системы
- **Владелец**: SYSTEM (не проект, не тенант)
- **Переиспользование**: Один HorizonFragment используется множеством проектов
- **Экономическая модель "Data Bounty"**: Первый проект оплачивает создание (Bounty), последующие получают доступ через подписку (Access Fee)

### 2. Изоляция через Lens (подписка)
- **Lens привязан к Project** - определяет, какие HorizonFragment видит проект
- **Изоляция на уровне Lens**: Проект видит только те HorizonFragment, которые указаны в `scope` его активных Lens
- **Запросы привязаны к Project**: ScoutingJob привязан к Project (запрос идет от проекта), но создает общие данные

### 3. Переиспользование Sources
- Scout использует существующие DataSource как источники данных
- Интеграция с существующей системой обработки (crawler, poller)
- Frame ссылается на DataSource или Document

## Модель общих данных HorizonFragment

### Принцип общих данных

**Ключевой принцип**: HorizonFragment — это **общие данные**, совместно создаваемый и используемый актив системы. Владельцем является **SYSTEM**, а не конкретный проект или тенант.

**Характеристики**:
- `tenantId: "global"` или `tenantId: ""` (всегда глобальные)
- `projectId: null` (никогда не привязан к конкретному проекту)
- Создаются системой при первом запросе от любого проекта
- Используются всеми проектами через Lens (подписку)

**Примеры общих сущностей**:
- Крупные компании (TSMC, Apple, Samsung)
- Критическая инфраструктура (порты, аэропорты)
- Геополитические регионы
- Ключевые ресурсы (нефть, газ, полупроводники)
- Любые сущности реального мира

**Экономическая модель "Data Bounty"**:
- **Первопроходец (Pioneer)**: Проект, запрашивающий данные по отсутствующему в системе объекту, оплачивает полную стоимость его разведки (Bounty)
- **Последователь (Follower)**: Проекты, запрашивающие доступ к уже существующему объекту, оплачивают значительно меньшую стоимость подписки (Access Fee)
- **Распределенные затраты**: Стоимость постоянного мониторинга (Watchdog) для популярных узлов распределяется пропорционально между всеми активными подписчиками

**Жизненный цикл через подписки**:
- Каждый HorizonFragment имеет счетчик `activeSubscribers` (количество активных Lens, подписанных на этот фрагмент)
- При `activeSubscribers > 0` — узел активен, мониторинг включен
- При `activeSubscribers = 0` — узел переходит в состояние Dormant, мониторинг прекращается
- Узлы в состоянии Dormant архивируются по истечении срока

**Изоляция через Lens, а не через HorizonFragment**:
- Изоляция данных происходит на уровне **Lens** (который привязан к Project)
- Проект видит только те HorizonFragment, на которые подписан через свои Lens
- Сам HorizonFragment остается общим для всех проектов

### Структура HorizonFragment как общих данных

```go
type HorizonFragment struct {
    // BaseOntologyFields
    ID          string
    TenantID    string  // Всегда "global" или "" - данные общие для всех
    Version     int
    // ...
    
    // НЕТ projectId - данные общие!
    
    // Основные поля
    CanonicalPath string
    // ...
    
    // Управление жизненным циклом через подписки
    Governance Governance `json:"governance" firestore:"governance"`
}

type Governance struct {
    Owner              string   `json:"owner" firestore:"owner"` // Всегда "SYSTEM"
    ActiveSubscribers  int       `json:"activeSubscribers" firestore:"activeSubscribers"` // Количество активных Lens
    Status             string   `json:"status" firestore:"status"` // "ACTIVE", "DORMANT", "ARCHIVED"
    
    // Метаданные о создании (для биллинга)
    CreatedByProjectID *string  `json:"createdByProjectId,omitempty" firestore:"createdByProjectId,omitempty"` // Проект, оплативший создание
    CreatedAt          time.Time `json:"createdAt" firestore:"createdAt"`
}
```

**Важно**: 
- `projectId` **НЕ** добавляется в HorizonFragment - данные общие
- Изоляция происходит через **Lens** (который привязан к Project)
- Проект видит только те HorizonFragment, на которые подписан через свои Lens
```

## Интеграция ScoutingJob с Project

### Структура ScoutingJob

```go
type ScoutingJob struct {
    AgentJob // Встраивание существующей структуры
    
    // Контекст проекта
    TenantID  string `json:"tenantId" firestore:"tenantId"`
    ProjectID string `json:"projectId" firestore:"projectId"` // Обязательно!
    LensID    string `json:"lensId" firestore:"lensId"`
    
    // Целевой узел
    TargetCanonicalPath string `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`
    
    // Источники данных
    SourceIDs []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"` // DataSource IDs из проекта
    
    // Стратегия и биллинг
    ScoutingStrategy string         `json:"scoutingStrategy" firestore:"scoutingStrategy"`
    BillingContext   BillingContext `json:"billingContext" firestore:"billingContext"`
    
    // Состояние
    CurrentState ScoutingState `json:"currentState" firestore:"currentState"`
}
```

### Использование Sources проекта

```go
// Scout Planner использует Sources проекта для планирования
func (p *ScoutPlanner) PlanSources(ctx context.Context, job *ScoutingJob) (*SourcePlan, error) {
    // 1. Получаем Sources проекта
    sources, err := p.dataSourcesRepo.ListByProject(ctx, job.TenantID, job.ProjectID)
    if err != nil {
        return nil, fmt.Errorf("failed to list project sources: %w", err)
    }
    
    // 2. Фильтруем релевантные Sources
    relevantSources := p.filterRelevantSources(sources, job.TargetCanonicalPath)
    
    // 3. Добавляем глобальные Sources (web, rss, telegram_channel)
    globalSources, err := p.dataSourcesRepo.ListGlobal(ctx)
    if err == nil {
        relevantSources = append(relevantSources, p.filterRelevantSources(globalSources, job.TargetCanonicalPath)...)
    }
    
    // 4. Формируем план
    return &SourcePlan{
        Sources: relevantSources,
        Strategy: job.ScoutingStrategy,
    }, nil
}
```

## Использование DataSource в Scout

### Типы Sources для Horizon

#### Поддерживаемые типы DataSource

1. **web** — веб-страницы
   - Используется через существующий crawler
   - Frame ссылается на DataSource и Document

2. **rss** — RSS-каналы
   - Используется через существующий poller
   - Frame ссылается на DataSource и Document

3. **telegram_channel** — Telegram-каналы
   - Используется через существующую интеграцию
   - Frame ссылается на DataSource и Document

4. **document** — документы
   - Используется через существующую систему обработки документов
   - Frame ссылается на DataSource и Document

5. **api** — API источники (будущее)
   - Прямые API вызовы
   - Frame ссылается на DataSource

### Интеграция с существующей обработкой

```go
// Scout Executor использует существующую инфраструктуру
type ScoutExecutor struct {
    crawler    *crawler.Crawler
    poller     *discovery.PollerWorker
    documentsRepo *repo.DocumentsRepository
    chunksRepo    *repo.ChunksRepository
}

func (e *ScoutExecutor) ExecuteSource(ctx context.Context, source *models.DataSource) (*SourceContent, error) {
    switch source.Kind {
    case "web":
        // Используем существующий crawler
        doc, err := e.crawler.Fetch(ctx, *source.Details.URI)
        if err != nil {
            return nil, fmt.Errorf("failed to fetch web source: %w", err)
        }
        
        // Сохраняем как Document (если еще не сохранен)
        document, err := e.ensureDocument(ctx, source, doc)
        if err != nil {
            return nil, err
        }
        
        return &SourceContent{
            SourceID:   source.ID,
            DocumentID: document.ID,
            Content:    doc.Text,
            Metadata:   map[string]interface{}{
                "url": *source.Details.URI,
                "kind": "web",
            },
        }, nil
        
    case "rss":
        // Используем существующий poller
        items, err := e.poller.FetchRSS(ctx, *source.Details.RSSURL)
        if err != nil {
            return nil, fmt.Errorf("failed to fetch RSS: %w", err)
        }
        
        // Обрабатываем каждый item как Document
        // ...
        
    case "telegram_channel":
        // Используем существующую интеграцию
        // ...
        
    case "document":
        // Используем существующий Document
        document, err := e.documentsRepo.Get(ctx, source.ID)
        if err != nil {
            return nil, fmt.Errorf("failed to get document: %w", err)
        }
        
        // Получаем контент из Chunks
        chunks, err := e.chunksRepo.GetByDocID(ctx, document.ID)
        if err != nil {
            return nil, fmt.Errorf("failed to get chunks: %w", err)
        }
        
        content := e.combineChunks(chunks)
        
        return &SourceContent{
            SourceID:   source.ID,
            DocumentID: document.ID,
            Content:    content,
            Metadata:   map[string]interface{}{
                "kind": "document",
            },
        }, nil
        
    default:
        return nil, fmt.Errorf("unsupported source kind: %s", source.Kind)
    }
}
```

## Связь Frame с DataSource и Document

### Расширенная структура Frame

```go
type Frame struct {
    // ... существующие поля
    
    // Связь с источниками данных
    SourceIDs      []string `json:"sourceIds" firestore:"sourceIds"`       // DataSource IDs
    DocumentIDs    []string `json:"documentIds,omitempty" firestore:"documentIds,omitempty"` // Document IDs (если есть)
    
    // Детальная Provenance
    Provenance     ClaimProvenance `json:"provenance" firestore:"provenance"`
    
    // Контекст проекта
    ProjectID      string `json:"projectId" firestore:"projectId"`
    TenantID       string `json:"tenantId" firestore:"tenantId"`
}

type ClaimProvenance struct {
    // Существующие поля
    SourceIDs       []string    `json:"sourceIds" firestore:"sourceIds"`
    SourceRefs     []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
    
    // Новые поля для интеграции
    DataSourceIDs   []string    `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"` // DataSource IDs
    DocumentIDs     []string    `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`     // Document IDs
    
    Method          ExtractionMethod `json:"method" firestore:"method"`
    ExtractedAt     time.Time        `json:"extractedAt" firestore:"extractedAt"`
    EvidenceAnchors []string         `json:"evidenceAnchors,omitempty" firestore:"evidenceAnchors,omitempty"`
    License         *string          `json:"license,omitempty" firestore:"license,omitempty"`
}

type SourceRef struct {
    SourceID   string `json:"sourceId" firestore:"sourceId"`   // DataSource ID или Document ID
    SourceType string `json:"sourceType" firestore:"sourceType"` // "dataSource", "document", "web", "rss", "telegram_channel"
    DocumentID *string `json:"documentId,omitempty" firestore:"documentId,omitempty"` // Если SourceType = "dataSource", может быть Document
}
```

## Механизм доступа к HorizonFragment через Lens

### Поиск HorizonFragment (всегда глобальные)

```go
func (r *HorizonFragmentsRepository) GetByCanonicalPath(
    ctx context.Context,
    canonicalPath string,
) (*HorizonFragment, error) {
    // HorizonFragment всегда глобальные
    doc, err := r.col.
        Where("tenantId", "==", "global").
        Where("canonicalPath", "==", canonicalPath).
        Limit(1).
        Documents(ctx).
        Next()
    
    if err != nil {
        return nil, ErrNotFound
    }
    
    var fragment HorizonFragment
    if err := doc.DataTo(&fragment); err != nil {
        return nil, err
    }
    
    return &fragment, nil
}
```

### Проверка доступа через Lens

Доступ к HorizonFragment контролируется через **Lens**, который привязан к Project:

```go
// Проверка, имеет ли проект доступ к HorizonFragment через свои Lens
func (s *HorizonService) HasAccess(ctx context.Context, projectID string, fragmentID string) (bool, error) {
    // Получаем все активные Lens проекта
    lenses, err := s.lensRepo.ListByProject(ctx, tenantID, projectID)
    if err != nil {
        return false, err
    }
    
    // Проверяем, подписан ли хотя бы один Lens на этот HorizonFragment
    for _, lens := range lenses {
        if lens.Status == "ACTIVE" && contains(lens.Scope, fragmentID) {
            return true, nil
        }
    }
    
    return false, nil
}

// Получение HorizonFragment для проекта (только те, на которые подписан через Lens)
func (s *HorizonService) GetFragmentsForProject(ctx context.Context, projectID string) ([]*HorizonFragment, error) {
    // Получаем все активные Lens проекта
    lenses, err := s.lensRepo.ListByProject(ctx, tenantID, projectID)
    if err != nil {
        return nil, err
    }
    
    // Собираем все canonicalPath из scope всех Lens
    canonicalPaths := make(map[string]bool)
    for _, lens := range lenses {
        if lens.Status == "ACTIVE" {
            for _, path := range lens.Scope {
                canonicalPaths[path] = true
            }
        }
    }
    
    // Получаем HorizonFragment по canonicalPath
    fragments := make([]*HorizonFragment, 0, len(canonicalPaths))
    for path := range canonicalPaths {
        fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
        if err == nil {
            fragments = append(fragments, fragment)
        }
    }
    
    return fragments, nil
}
```

**Принцип**: 
- HorizonFragment всегда глобальные и доступны всем
- Изоляция происходит на уровне **Lens** (который привязан к Project)
- Проект видит только те HorizonFragment, которые указаны в `scope` его активных Lens

## Экономическая модель с Project

### Bounty vs Access

```go
type BillingContext struct {
    ChargeModel string  `json:"chargeModel" firestore:"chargeModel"` // "Bounty" или "Access"
    MaxCost     float64 `json:"maxCost" firestore:"maxCost"`
    ProjectID   string  `json:"projectId" firestore:"projectId"`
}

// При создании HorizonFragment
func (s *HorizonService) CreateFragment(ctx context.Context, job *ScoutingJob) (*HorizonFragment, error) {
    // Проверяем, существует ли уже HorizonFragment (всегда глобальный)
    existing, err := s.fragmentsRepo.GetByCanonicalPath(ctx, job.TargetCanonicalPath)
    
    if err == nil && existing != nil {
        // HorizonFragment уже существует - это Access
        // Проект получает доступ через Lens (подписку)
        // activeSubscribers обновляется автоматически при создании/удалении Lens
        
        return existing, nil
    }
    
    // HorizonFragment не существует - создаем новый (Bounty)
    // Проект оплачивает создание, но данные становятся общими
    fragment := &HorizonFragment{
        TenantID:      "global", // Всегда глобальный
        CanonicalPath: job.TargetCanonicalPath,
        Governance: Governance{
            Owner:             "SYSTEM", // Владелец - система
            CreatedByProjectID: &job.ProjectID, // Проект, оплативший создание (для биллинга)
            ActiveSubscribers: 0, // Будет обновляться при создании Lens
            Status:            "ACTIVE",
        },
        // ...
    }
    
    // Создаем HorizonFragment
    // ...
    
    return fragment, nil
}
```

## Интеграция Lens с Project

Lens привязан к Project через `projectId`. Это обеспечивает:

1. **Изоляцию через подписку**: Проект видит только те HorizonFragment, которые указаны в `scope` его активных Lens
2. **Контекст запросов**: Все запросы Scout инициируются в контексте проекта (ScoutingJob привязан к Project)
3. **Биллинг**: Стоимость привязана к проекту
4. **Управление подписками**: При создании Lens увеличивается `activeSubscribers` соответствующих HorizonFragment, при удалении - уменьшается

**Важно**: 
- Lens определяет, какие HorizonFragment видит проект
- Сам HorizonFragment остается общим для всех проектов
- Несколько проектов могут иметь Lens с одинаковым `scope` - они будут видеть одни и те же HorizonFragment

## Использование существующих Sources в Watchdog

Watchdog может использовать Sources проекта для мониторинга:

```go
type WatchdogConfig struct {
    // ... существующие поля
    
    // Источники для мониторинга
    SourceIDs []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"` // DataSource IDs из проекта
    
    // Или использовать все Sources проекта
    UseProjectSources bool `json:"useProjectSources" firestore:"useProjectSources"`
}

func (w *WatchdogService) GetSourcesForMonitoring(ctx context.Context, config *WatchdogConfig, projectID string) ([]*models.DataSource, error) {
    if config.UseProjectSources {
        // Используем все Sources проекта
        return w.dataSourcesRepo.ListByProject(ctx, config.TenantID, projectID)
    }
    
    // Используем указанные Sources
    sources := make([]*models.DataSource, 0, len(config.SourceIDs))
    for _, sourceID := range config.SourceIDs {
        source, err := w.dataSourcesRepo.Get(ctx, sourceID)
        if err != nil {
            continue
        }
        sources = append(sources, source)
    }
    
    return sources, nil
}
```

## Преимущества интеграции

1. **Переиспользование инфраструктуры**: Используем существующий crawler, poller, обработку документов
2. **Единая модель Sources**: Один источник данных используется и в Discovery, и в Horizon
3. **Изоляция через Project**: Четкая изоляция данных и запросов
4. **Экономическая эффективность**: Общие данные оплачиваются один раз
5. **Контекстная осведомленность**: Все данные привязаны к проекту и тенанту

## См. также

- [Scout Workflow](../workflows/scout-workflow.md)
- Код моделей: `backend/internal/horizon/models/*.go`

