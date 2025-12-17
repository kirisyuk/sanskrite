# Модель данных системы мониторинга горизонта

## Обзор

Модель данных системы мониторинга горизонта построена на основе графовой модели знаний, где узлы представляют сущности реального мира (HorizonFragment), а ребра описывают зависимости между ними (DependencyEdge). Данные для наполнения графа поставляются через неизменяемые кадры (Frame), а пользовательские представления определяются через Lens.

## Firestore коллекции

### horizon_fragments

**Назначение**: Хранит узлы графа знаний (HorizonFragment)

**Структура документа**:
```json
{
  "id": "uuid",
  "tenantId": "global",
  "canonicalPath": "/org/tsmc/fab/21",
  "type": "ORGANIZATION | FACILITY | GEO_REGION | EVENT",
  "entityPath": "/Infrastructure/Manufacturing/Factory",
  "sectorPath": "/Technology/Semiconductors/Manufacturing",
  "entityId": "uuid",
  "state": {
    "operational_health": {
      "value": 0.85,
      "confidence": 0.95,
      "observedAt": "2025-12-17T14:30:00Z",
      "unit": null,
      "errorMargin": { "low": 0.80, "high": 0.90 }
    },
    "supply_chain_risk": {
      "value": 0.4,
      "confidence": 0.88,
      "observedAt": "2025-12-17T14:30:00Z"
    }
  },
  "kbRefs": [
    {
      "kb": "Wikidata",
      "externalId": "Q123456",
      "confidence": 0.95,
      "lastSync": "2025-12-17T10:00:00Z"
    }
  ],
  "identifiers": {
    "ISIN": "US1234567890",
    "LEI": "5493000KJTIIGC8Y1R12",
    "Ticker": "TSM"
  },
  "homeLocation": {
    "name": "Hsinchu, Taiwan",
    "iso6709": "+2457+12058/",
    "iso3166_1": "TW",
    "iso3166_2": "TW-HSQ"
  },
  "validFrom": "2020-01-01T00:00:00Z",
  "validTo": null,
  "labels": {
    "en": "TSMC Fab 21",
    "ru": "TSMC Фабрика 21"
  },
  "tags": ["semiconductor", "critical-infrastructure"],
  "status": "active",
  "governance": {
    "owner": "SYSTEM",
    "activeSubscribers": 5,
    "status": "ACTIVE",
    "createdByProjectId": "project-uuid-1"
  },
  "provenance": {
    "sourceRefs": [
      {
        "sourceId": "doc-uuid",
        "sourceType": "document"
      }
    ],
    "dataSourceIds": ["source-uuid-1"],
    "documentIds": ["doc-uuid"],
    "observedAt": "2025-12-17T14:30:00Z",
    "method": "extracted"
  },
  "metadata": {
    "createdAt": "2025-12-15T10:00:00Z",
    "lastUpdatedAt": "2025-12-17T14:30:00Z",
    "lastVerifiedAt": "2025-12-17T14:25:00Z",
    "sourceFrameId": "uuid",
    "epistemicStatus": "VERIFIED | REJECTED | LOW_CONFIDENCE"
  },
  "governance": {
    "owner": "SYSTEM",
    "activeSubscribers": 5,
    "status": "ACTIVE | DORMANT | ARCHIVED"
  },
  "version": 15,
  "createdAt": "2025-12-15T10:00:00Z",
  "updatedAt": "2025-12-17T14:30:00Z",
  "createdBy": "system",
  "updatedBy": "system"
}
```

**Индексы**:
- `(tenantId, canonicalPath)` — уникальный индекс (tenantId всегда "global")
- `(tenantId, status, updatedAt DESC)` — для запросов активных узлов
- `(tenantId, governance.activeSubscribers, status)` — для управления жизненным циклом
- `(canonicalPath)` — для поиска по canonicalPath (данные глобальные)

**Принцип общих данных**:
- **HorizonFragment всегда глобальные**: `tenantId: "global"`, `projectId: null` (не существует)
- **Владелец**: SYSTEM (через `governance.owner`)
- **Доступ через Lens**: Проект видит HorizonFragment через подписку через Lens (который привязан к Project)
- **Жизненный цикл**: Управляется через `governance.activeSubscribers` (количество активных Lens, подписанных на этот фрагмент)

**Связь с Entity**: HorizonFragment может быть связан с Entity из онтологии через поле `entityId` (опционально)

**Связь с Project**: 
- HorizonFragment **НЕ** привязан к Project напрямую (данные общие)
- Доступ контролируется через **Lens** (который привязан к Project через `projectId`)
- Проект видит только те HorizonFragment, которые указаны в `scope` его активных Lens

### frames

**Назначение**: Неизменяемые кадры данных от Scout Engine

**Структура документа**:
```json
{
  "id": "uuid",
  "tenantId": "string",
  "scoutRequestId": "uuid",
  "projectId": "uuid",
  "tenantId": "string",
  "targetCanonicalPath": "/org/tsmc/fab/21",
  "sourceDocumentId": "uuid",
  "sourceIds": ["source-uuid-1", "source-uuid-2"],
  "documentIds": ["doc-uuid-1"],
  "extractedState": {
    "operational_health": {
      "value": 0.85,
      "confidence": 0.95,
      "observedAt": "2025-12-17T11:15:00Z",
      "unit": null,
      "errorMargin": { "low": 0.80, "high": 0.90 }
    }
  },
  "evidence": [
    {
      "sourceUrl": "https://www.reuters.com/...",
      "sourceHash": "sha256:f2ca1bb6c7e9...",
      "reliabilityScore": 1.0,
      "quote": "TSMC confirmed that production..."
    }
  ],
  "provenance": {
    "sourceRefs": [
      {
        "sourceId": "doc-uuid",
        "sourceType": "document"
      }
    ],
    "observedAt": "2025-12-17T11:15:00Z",
    "method": "extracted"
  },
  "kbRefs": [
    {
      "kb": "Wikidata",
      "externalId": "Q123456",
      "confidence": 0.90
    }
  ],
  "confidence": 0.95,
  "errorMargin": {
    "low": 0.80,
    "high": 0.90
  },
  "validationMeta": {
    "scoutLlmVersion": "gpt-5.2-pro",
    "criticLlmVersion": "claude-4.5-opus",
    "chainOfThought": "Initial query returned 3 sources...",
    "validationResult": "ACCEPTED | REJECTED | LOW_CONFIDENCE"
  },
  "createdAt": "2025-12-17T11:15:00Z"
}
```

**Индексы**:
- `(tenantId, targetCanonicalPath, createdAt DESC)` — для истории кадров узла
- `(tenantId, scoutRequestId)` — для связи с запросом
- `(tenantId, validationMeta.validationResult, createdAt DESC)` — для фильтрации по статусу

**Неизменяемость**: Frame документы никогда не обновляются, только создаются новые

**Связь с DataSource и Document**: 
- Frame ссылается на DataSource через `sourceIds` (массив DataSource IDs)
- Frame ссылается на Document через `documentIds` (массив Document IDs)
- Frame может ссылаться на Document через `sourceDocumentId` (для обратной совместимости)
- Provenance содержит детальные ссылки через `dataSourceIds` и `documentIds`

### lenses

**Назначение**: Пользовательские конфигурации мониторинга и анализа

**Структура документа**:
```json
{
  "id": "uuid",
  "tenantId": "string",
  "projectId": "string",
  "name": "Риски поставок полупроводников из Тайваня",
  "scope": [
    "/org/tsmc/fab/21",
    "/geo/tw"
  ],
  "viewConfig": {
    "depth": 3,
    "viewType": "graph | table | timeline"
  },
  "alertConfig": {
    "thresholds": {
      "operational_health": 0.5
    },
    "notifications": ["email", "websocket"]
  },
  "status": "ACTIVE | ARCHIVED",
  "createdAt": "2025-12-15T10:00:00Z",
  "updatedAt": "2025-12-17T14:30:00Z"
}
```

**Индексы**:
- `(tenantId, projectId, status, updatedAt DESC)` — для списка Lens проекта
- `(tenantId, status)` — для активных Lens
- `(tenantId, scope)` — для поиска Lens по узлам (array-contains)

**Связь с Project**: Lens всегда связан с Project через `projectId`

### dependency_edges

**Назначение**: Связи между HorizonFragment с метриками влияния

**Структура документа**:
```json
{
  "id": "uuid",
  "tenantId": "string",
  "sourceFragmentId": "uuid",
  "targetFragmentId": "uuid",
  "type": "SUPPLIES_TO | LOCATED_IN | OWNS | COMPETES_WITH",
  "properties": {
    "criticality": 0.9,
    "amplification": 1.1,
    "latencyHours": 72
  },
  "qualifiers": {
    "sharePercentage": 0.45,
    "contractType": "long-term",
    "alternativeSuppliers": 2
  },
  "provenance": {
    "sourceRefs": [
      {
        "sourceId": "source-uuid",
        "sourceType": "dataSource",
        "documentId": "doc-uuid"
      }
    ],
    "dataSourceIds": ["source-uuid"],
    "documentIds": ["doc-uuid"],
    "observedAt": "2025-12-17T14:30:00Z",
    "method": "extracted"
  },
  "validFrom": "2025-01-01T00:00:00Z",
  "validTo": null,
  "metadata": {
    "sourceFrameId": "uuid",
    "createdAt": "2025-12-15T10:00:00Z",
    "lastUpdatedAt": "2025-12-17T14:30:00Z"
  }
}
```

**Индексы**:
- `(tenantId, sourceFragmentId, type)` — для исходящих связей
- `(tenantId, targetFragmentId, type)` — для входящих связей
- `(tenantId, sourceFragmentId, targetFragmentId)` — для проверки существования связи
- `(tenantId, properties.criticality DESC)` — для сортировки по критичности

**Связь с Frame**: DependencyEdge создается на основе Frame со статусом VERIFIED

### watchdog_configs

**Назначение**: Конфигурации фонового мониторинга HorizonFragment

**Структура документа**:
```json
{
  "id": "uuid",
  "tenantId": "string",
  "targetFragmentId": "uuid",
  "scanInterval": "PT1H",
  "sourceList": [
    "https://www.reuters.com/...",
    "https://api.example.com/..."
  ],
  "triggerThreshold": 0.1,
  "status": "RUNNING | PAUSED",
  "lastScanAt": "2025-12-17T14:00:00Z",
  "nextScanAt": "2025-12-17T15:00:00Z",
  "createdAt": "2025-12-15T10:00:00Z",
  "updatedAt": "2025-12-17T14:30:00Z"
}
```

**Индексы**:
- `(tenantId, status, nextScanAt ASC)` — для планирования сканирований
- `(tenantId, targetFragmentId)` — для поиска конфигурации узла
- `(tenantId, status, lastScanAt DESC)` — для мониторинга активности

**Жизненный цикл**: Управляется автоматически на основе `activeSubscribers` соответствующего HorizonFragment

## Связь с существующими моделями

### HorizonFragment ↔ Entity

HorizonFragment расширяет Entity из онтологии, используя общие поля и добавляя специфичные для риск-анализа:

```go
type HorizonFragment struct {
    // BaseOntologyFields (общие с Entity)
    ID          string
    TenantID    string
    Version     int
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CreatedBy   string
    UpdatedBy   string
    Labels      map[string]string
    Description string
    Tags        []string
    Status      string
    
    // Связь с Entity
    EntityID    *string
    
    // Управление жизненным циклом (общие данные)
    Governance Governance `json:"governance" firestore:"governance"`
    
    // Расширение Entity
    KBRefs      []KBReference      // Обогащение из Wikidata и др.
    Identifiers map[string]string // ISIN, LEI, Ticker и др.
    ValidFrom   *time.Time         // Временная валидность
    ValidTo     *time.Time
    HomeLocation *Location         // С ISO стандартами
    
    // Специфичные для риск-анализа
    EntityPath  string             // Иерархический путь из таксономии
    SectorPath  string             // Отраслевой путь
    State       map[string]MetricValue // Гибкие метрики
    Provenance  Provenance         // Прослеживаемость
    
    // Семантический поиск
    VectorText      string    `json:"vectorText,omitempty" firestore:"vectorText,omitempty"`
    VectorEmbedding []float64 `json:"vectorEmbedding,omitempty" firestore:"vectorEmbedding,omitempty"`
    VectorModel     string    `json:"vectorModel,omitempty" firestore:"vectorModel,omitempty"` // text-embedding-3-small
    VectorVersion   string    `json:"vectorVersion,omitempty" firestore:"vectorVersion,omitempty"` // v1
}
```

**Использование**:
- Entity предоставляет каноническое имя, базовые атрибуты и KBRefs
- HorizonFragment добавляет метрики состояния, жизненный цикл и таксономию
- Связь позволяет использовать существующую онтологию для обогащения данных
- EntityEnricher может обновлять KBRefs в HorizonFragment через Entity

### Frame ↔ Document

Frame может ссылаться на Document из основной системы:

```go
type Frame struct {
    // ... основные поля
    SourceDocumentID *string `json:"sourceDocumentId,omitempty" firestore:"sourceDocumentId,omitempty"`
}
```

**Использование**:
- Document содержит исходный контент (HTML, PDF, текст)
- Frame содержит извлеченные структурированные данные
- Связь обеспечивает прослеживаемость от источника до факта

### Lens ↔ Project

Lens всегда связан с Project:

```go
type Lens struct {
    // ... основные поля
    ProjectID string `json:"projectId" firestore:"projectId"`
}
```

**Использование**:
- Project определяет контекст и владельца
- Lens определяет фокус анализа в рамках проекта
- Связь обеспечивает изоляцию данных и контроль доступа

## Версионирование

### HorizonFragment

Каждое обновление состояния HorizonFragment создает новую версию:

```go
type HorizonFragmentVersion struct {
    VersionID     string                 `json:"versionId" firestore:"versionId"`
    FragmentID    string                 `json:"fragmentId" firestore:"fragmentId"`
    State         map[string]interface{} `json:"state" firestore:"state"`
    SourceFrameID string                 `json:"sourceFrameId" firestore:"sourceFrameId"`
    Timestamp     time.Time              `json:"timestamp" firestore:"timestamp"`
}
```

**Хранилище**: Коллекция `horizon_fragment_versions` с индексами:
- `(tenantId, fragmentId, timestamp DESC)` — для истории версий узла
- `(tenantId, fragmentId, versionId)` — для конкретной версии

**Time-series**: Версии могут храниться в отдельной time-series базе для аналитики

### DependencyEdge

Изменения свойств DependencyEdge отслеживаются:

```go
type DependencyEdgeVersion struct {
    VersionID    string                 `json:"versionId" firestore:"versionId"`
    EdgeID       string                 `json:"edgeId" firestore:"edgeId"`
    Properties   map[string]interface{} `json:"properties" firestore:"properties"`
    SourceFrameID string                `json:"sourceFrameId" firestore:"sourceFrameId"`
    Timestamp    time.Time              `json:"timestamp" firestore:"timestamp"`
}
```

**Хранилище**: Коллекция `dependency_edge_versions`

## Жизненный цикл HorizonFragment

### Состояния

1. **ACTIVE** — узел активен, данные обновляются, Watchdog работает
   - Условие: `activeSubscribers > 0`
   - Действия: Регулярные обновления через Scout, активный мониторинг через Watchdog

2. **DORMANT** — узел в спящем состоянии, данные не обновляются
   - Условие: `activeSubscribers == 0`
   - Действия: Watchdog остановлен, данные начинают устаревать

3. **ARCHIVED** — узел архивирован, данные в холодном хранилище
   - Условие: Dormant дольше заданного периода (например, 1 год)
   - Действия: Данные перенесены в холодное хранилище, для повторного использования требуется полная разведка

### Переходы состояний

```
ACTIVE → DORMANT: Когда последняя Lens отписывается
DORMANT → ACTIVE: Когда новая Lens подписывается
DORMANT → ARCHIVED: После периода неактивности
ARCHIVED → ACTIVE: При запросе данных (требуется полная разведка)
```

## Схемы данных (Pydantic → Go)

### HorizonFragmentSchema_v1.3

```go
type HorizonFragment struct {
    // BaseOntologyFields
    ID            string            `json:"id" firestore:"id"`
    TenantID      string            `json:"tenantId" firestore:"tenantId"`
    Version       int               `json:"version" firestore:"version"`
    CreatedAt     time.Time         `json:"createdAt" firestore:"createdAt"`
    UpdatedAt     time.Time         `json:"updatedAt" firestore:"updatedAt"`
    CreatedBy     string            `json:"createdBy" firestore:"createdBy"`
    UpdatedBy     string            `json:"updatedBy" firestore:"updatedBy"`
    Labels        map[string]string `json:"labels,omitempty" firestore:"labels,omitempty"`
    Description   string            `json:"description,omitempty" firestore:"description,omitempty"`
    Tags          []string          `json:"tags,omitempty" firestore:"tags,omitempty"`
    Status        string            `json:"status" firestore:"status"` // active, deprecated, draft, archived
    
    // Основные поля
    CanonicalPath string            `json:"canonicalPath" firestore:"canonicalPath"`
    Type          FragmentType      `json:"type" firestore:"type"`
    EntityPath    string            `json:"entityPath" firestore:"entityPath"` // /Organization/Corporation/Public
    SectorPath    string            `json:"sectorPath" firestore:"sectorPath"` // /Technology/Semiconductors/Manufacturing
    
    // Связь с Entity
    EntityID      *string           `json:"entityId,omitempty" firestore:"entityId,omitempty"`
    
    // Гибкие метрики
    State         map[string]MetricValue `json:"state" firestore:"state"`
    
    // Обогащение данными
    KBRefs        []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
    Identifiers   map[string]string `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`
    
    // Временная валидность
    ValidFrom     *time.Time        `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
    ValidTo       *time.Time        `json:"validTo,omitempty" firestore:"validTo,omitempty"`
    
    // Location с ISO стандартами
    HomeLocation  *Location         `json:"homeLocation,omitempty" firestore:"homeLocation,omitempty"`
    
    // Provenance
    Provenance    Provenance        `json:"provenance" firestore:"provenance"`
    
    // Метаданные и управление
    Metadata      FragmentMetadata  `json:"metadata" firestore:"metadata"`
    Governance    FragmentGovernance `json:"governance" firestore:"governance"`
}

type Location struct {
    Name     string  `json:"name,omitempty" firestore:"name,omitempty"`
    ISO6709  *string `json:"iso6709,omitempty" firestore:"iso6709,omitempty"` // координаты ISO 6709
    ISO3166_1 *string `json:"iso3166_1,omitempty" firestore:"iso3166_1,omitempty"` // ISO 3166-1 alpha-2 (страна)
    ISO3166_2 *string `json:"iso3166_2,omitempty" firestore:"iso3166_2,omitempty"` // ISO 3166-2 (субъект)
}

type KBReference struct {
    KB         string     `json:"kb" firestore:"kb"` // Wikidata, DBpedia, etc.
    ExternalID string     `json:"externalId" firestore:"externalId"`
    Confidence float64    `json:"confidence" firestore:"confidence"` // 0..1
    LastSync   *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
}

type Provenance struct {
    SourceIDs   []string    `json:"sourceIds" firestore:"sourceIds"`   // для обратной совместимости
    SourceRefs  []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
    ObservedAt  *time.Time `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
    Method      string     `json:"method,omitempty" firestore:"method,omitempty"` // extracted, curated, imported
}

type SourceRef struct {
    SourceID   string `json:"sourceId" firestore:"sourceId"`
    SourceType string `json:"sourceType" firestore:"sourceType"` // document, telegram_channel, rss, web, chat
}

type MetricValue struct {
    Value       float64     `json:"value" firestore:"value"`
    Unit        *string     `json:"unit,omitempty" firestore:"unit,omitempty"`
    ErrorMargin *ErrorMargin `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
    Confidence  float64     `json:"confidence" firestore:"confidence"` // 0..1
    ObservedAt  time.Time   `json:"observedAt" firestore:"observedAt"`
    Provenance  Provenance  `json:"provenance" firestore:"provenance"`
}

type ErrorMargin struct {
    Low   *float64 `json:"low,omitempty" firestore:"low,omitempty"`
    High  *float64 `json:"high,omitempty" firestore:"high,omitempty"`
    Value *float64 `json:"value,omitempty" firestore:"value,omitempty"` // для симметричного интервала
}

type Governance struct {
    Owner              string     `json:"owner" firestore:"owner"` // Всегда "SYSTEM"
    ActiveSubscribers  int        `json:"activeSubscribers" firestore:"activeSubscribers"` // Количество активных Lens
    Status             string     `json:"status" firestore:"status"` // "ACTIVE", "DORMANT", "ARCHIVED"
    CreatedByProjectID *string    `json:"createdByProjectId,omitempty" firestore:"createdByProjectId,omitempty"` // Проект, оплативший создание (для биллинга)
}

type FragmentType string

const (
    FragmentTypeOrganization FragmentType = "ORGANIZATION"
    FragmentTypeFacility     FragmentType = "FACILITY"
    FragmentTypeGeoRegion    FragmentType = "GEO_REGION"
    FragmentTypeEvent        FragmentType = "EVENT"
)

type FragmentMetadata struct {
    CreatedAt      time.Time `json:"createdAt" firestore:"createdAt"`
    LastUpdatedAt  time.Time `json:"lastUpdatedAt" firestore:"lastUpdatedAt"`
    LastVerifiedAt time.Time `json:"lastVerifiedAt" firestore:"lastVerifiedAt"`
    SourceFrameID  string    `json:"sourceFrameId" firestore:"sourceFrameId"`
    EpistemicStatus string   `json:"epistemicStatus" firestore:"epistemicStatus"`
}

type FragmentGovernance struct {
    Owner            string `json:"owner" firestore:"owner"`
    ActiveSubscribers int   `json:"activeSubscribers" firestore:"activeSubscribers"`
    Status           string `json:"status" firestore:"status"`
}
```

### FrameSchema_v1.2

```go
type Frame struct {
    ID                  string                 `json:"id" firestore:"id"`
    TenantID            string                 `json:"tenantId" firestore:"tenantId"`
    ProjectID           string                 `json:"projectId" firestore:"projectId"`
    ScoutRequestID     string                 `json:"scoutRequestId" firestore:"scoutRequestId"`
    TargetCanonicalPath string                 `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`
    SourceIDs           []string               `json:"sourceIds" firestore:"sourceIds"`           // DataSource IDs
    SourceDocumentID    *string                `json:"sourceDocumentId,omitempty" firestore:"sourceDocumentId,omitempty"` // Для обратной совместимости
    DocumentIDs         []string               `json:"documentIds,omitempty" firestore:"documentIds,omitempty"` // Document IDs
    ExtractedState      map[string]MetricValue `json:"extractedState" firestore:"extractedState"`
    Evidence            []Evidence              `json:"evidence" firestore:"evidence"`
    Provenance          ClaimProvenance         `json:"provenance" firestore:"provenance"`
    KBRefs              []KBReference           `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
    Confidence          float64                 `json:"confidence" firestore:"confidence"` // 0..1
    ErrorMargin         *ErrorMargin            `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
    ValidationMeta      ValidationMeta          `json:"validationMeta" firestore:"validationMeta"`
    CreatedAt           time.Time               `json:"createdAt" firestore:"createdAt"`
}

type ClaimProvenance struct {
    SourceIDs       []string         `json:"sourceIds" firestore:"sourceIds"`   // Для обратной совместимости
    SourceRefs      []SourceRef      `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
    DataSourceIDs   []string         `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"` // DataSource IDs
    DocumentIDs     []string         `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`     // Document IDs
    Method          ExtractionMethod `json:"method" firestore:"method"`
    ExtractedAt     time.Time        `json:"extractedAt" firestore:"extractedAt"`
    EvidenceAnchors []string          `json:"evidenceAnchors,omitempty" firestore:"evidenceAnchors,omitempty"`
    License         *string           `json:"license,omitempty" firestore:"license,omitempty"`
}

type SourceRef struct {
    SourceID   string  `json:"sourceId" firestore:"sourceId"`   // DataSource ID или Document ID
    SourceType string  `json:"sourceType" firestore:"sourceType"` // "dataSource", "document", "web", "rss", "telegram_channel"
    DocumentID *string `json:"documentId,omitempty" firestore:"documentId,omitempty"` // Если SourceType = "dataSource", может быть Document
}

type ExtractionMethod string

const (
    ExtractionMethodNLP     ExtractionMethod = "NLP"
    ExtractionMethodHuman   ExtractionMethod = "human"
    ExtractionMethodAPI     ExtractionMethod = "API"
    ExtractionMethodKBAlign ExtractionMethod = "KBAlign"
)

type Evidence struct {
    SourceURL       string  `json:"sourceUrl" firestore:"sourceUrl"`
    SourceHash      string  `json:"sourceHash" firestore:"sourceHash"`
    ReliabilityScore float64 `json:"reliabilityScore" firestore:"reliabilityScore"`
    Quote           string  `json:"quote" firestore:"quote"`
}

type ValidationMeta struct {
    ScoutLLMVersion   string `json:"scoutLlmVersion" firestore:"scoutLlmVersion"`
    CriticLLMVersion  string `json:"criticLlmVersion" firestore:"criticLlmVersion"`
    ChainOfThought    string `json:"chainOfThought" firestore:"chainOfThought"`
    ValidationResult  string `json:"validationResult" firestore:"validationResult"`
}
```

### LensSchema_v1.0

```go
type Lens struct {
    ID          string                 `json:"id" firestore:"id"`
    TenantID    string                 `json:"tenantId" firestore:"tenantId"`
    ProjectID   string                 `json:"projectId" firestore:"projectId"`
    Name        string                 `json:"name" firestore:"name"`
    Scope       []string               `json:"scope" firestore:"scope"`
    ViewConfig  map[string]interface{} `json:"viewConfig" firestore:"viewConfig"`
    AlertConfig map[string]interface{} `json:"alertConfig" firestore:"alertConfig"`
    Status      string                 `json:"status" firestore:"status"`
    CreatedAt   time.Time              `json:"createdAt" firestore:"createdAt"`
    UpdatedAt   time.Time              `json:"updatedAt" firestore:"updatedAt"`
}
```

### DependencyEdgeSchema_v1.2

```go
type DependencyEdge struct {
    ID               string                 `json:"id" firestore:"id"`
    TenantID         string                 `json:"tenantId" firestore:"tenantId"`
    SourceFragmentID string                 `json:"sourceFragmentId" firestore:"sourceFragmentId"`
    TargetFragmentID string                 `json:"targetFragmentId" firestore:"targetFragmentId"`
    Type             DependencyType          `json:"type" firestore:"type"`
    Properties       EdgeProperties         `json:"properties" firestore:"properties"`
    Qualifiers       map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
    Provenance       Provenance             `json:"provenance" firestore:"provenance"`
    ValidFrom        *time.Time             `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
    ValidTo          *time.Time             `json:"validTo,omitempty" firestore:"validTo,omitempty"`
    Metadata         EdgeMetadata           `json:"metadata" firestore:"metadata"`
}

type DependencyType string

const (
    DependencyTypeSuppliesTo   DependencyType = "SUPPLIES_TO"
    DependencyTypeLocatedIn    DependencyType = "LOCATED_IN"
    DependencyTypeOwns         DependencyType = "OWNS"
    DependencyTypeCompetesWith DependencyType = "COMPETES_WITH"
)

type EdgeProperties struct {
    Criticality   float64 `json:"criticality" firestore:"criticality"`
    Amplification float64 `json:"amplification" firestore:"amplification"`
    LatencyHours  int     `json:"latencyHours" firestore:"latencyHours"`
}

type EdgeMetadata struct {
    SourceFrameID  string    `json:"sourceFrameId" firestore:"sourceFrameId"`
    CreatedAt      time.Time `json:"createdAt" firestore:"createdAt"`
    LastUpdatedAt  time.Time `json:"lastUpdatedAt" firestore:"lastUpdatedAt"`
}
```

## Индексы Firestore

### Обязательные индексы

```json
{
  "indexes": [
    {
      "collectionGroup": "horizon_fragments",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "canonicalPath", "order": "ASCENDING" }
      ]
    },
    {
      "collectionGroup": "horizon_fragments",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "governance.status", "order": "ASCENDING" },
        { "fieldPath": "updatedAt", "order": "DESCENDING" }
      ]
    },
    {
      "collectionGroup": "dependency_edges",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "sourceFragmentId", "order": "ASCENDING" },
        { "fieldPath": "type", "order": "ASCENDING" }
      ]
    },
    {
      "collectionGroup": "dependency_edges",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "targetFragmentId", "order": "ASCENDING" },
        { "fieldPath": "type", "order": "ASCENDING" }
      ]
    },
    {
      "collectionGroup": "lenses",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "projectId", "order": "ASCENDING" },
        { "fieldPath": "status", "order": "ASCENDING" },
        { "fieldPath": "updatedAt", "order": "DESCENDING" }
      ]
    },
    {
      "collectionGroup": "watchdog_configs",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tenantId", "order": "ASCENDING" },
        { "fieldPath": "status", "order": "ASCENDING" },
        { "fieldPath": "nextScanAt", "order": "ASCENDING" }
      ]
    }
  ]
}
```

### Векторные индексы для семантического поиска

**Важно**: Векторные индексы создаются через `gcloud firestore indexes composite create`, а не через Firebase CLI или `firestore.indexes.json`.

```bash
# Индекс для horizon_fragments
gcloud firestore indexes composite create \
  --collection-group=horizon_fragments \
  --query-scope=COLLECTION \
  --field-config='field-path=tenantId,order=ASCENDING' \
  --field-config='field-path=vectorEmbedding,vector-config={"dimension":"1536", "flat": "{}"}' \
  --database="(default)" \
  --project=PROJECT_ID

# Индекс для entities
gcloud firestore indexes composite create \
  --collection-group=entities \
  --query-scope=COLLECTION \
  --field-config='field-path=tenantId,order=ASCENDING' \
  --field-config='field-path=vectorEmbedding,vector-config={"dimension":"1536", "flat": "{}"}' \
  --database="(default)" \
  --project=PROJECT_ID
```

**Примечание**: Размерность вектора (1536) соответствует модели `text-embedding-3-small` от OpenAI. При использовании других моделей (например, Vertex AI) размерность может отличаться.

Подробнее см. [Семантический поиск](semantic-search.md).
```

## Связь с таксономией

Все HorizonFragment должны соответствовать базовой таксономии (Core Taxonomy v1.0):

- **entityPath**: Определяет физическую природу узла (`/Organization/*`, `/Infrastructure/*`, `/Resource/*`, `/Person/*`, `/Geo/*`)
- **sectorPath**: Определяет отрасль и сектор (`/Energy/*`, `/Technology/*`, `/Materials/*`, и т.д.)
- **homeLocation**: Использует ISO стандарты (ISO 3166-1/3166-2 для регионов, ISO 6709 для координат)
- **state**: Содержит гибкие метрики с описаниями и типами согласно таксономии
  - Стандартизированные метрики из таксономии (operational_health, capacity_utilization, и т.д.)
  - Дополнительные метрики с описаниями и типами (magnitude/directional)
  - Каждая метрика содержит value, confidence, unit, errorMargin, observedAt, provenance

**Интеграция с существующей онтологией**:
- HorizonFragment расширяет Entity через BaseOntologyFields
- Использует KBRefs для обогащения из Wikidata
- Использует Identifiers для связи с внешними системами (ISIN, LEI, Ticker)
- Использует ValidFrom/ValidTo для временной валидности
- Использует Provenance для прослеживаемости источников

Подробнее см. [Базовая таксономия](taxonomy.md).

## Интеграция с Project и DataSource

Horizon интегрируется с существующей системой проектов и источников данных:

- **Общие данные**: HorizonFragment всегда глобальные (`tenantId: "global"`), переиспользуются между проектами
- **Изоляция через Lens**: Доступ контролируется через Lens (который привязан к Project), проекты видят только те HorizonFragment, на которые подписаны
- **Запросы привязаны к Project**: ScoutingJob привязан к Project (запрос идет от проекта), но создает общие данные
- **Переиспользование Sources**: Scout использует существующие DataSource (web, rss, telegram_channel, document) как источники данных
- **Связь Frame с DataSource**: Frame ссылается на DataSource через `sourceIds` и на Document через `documentIds`

Подробнее см. [Интеграция с Project и DataSource](project-integration.md).

## Семантический поиск

Для гибкого семантического поиска HorizonFragment и Entity используется поле `vectorText`, которое содержит человекочитаемый набор всех релевантных атрибутов сущности. Это поле векторизуется и используется для семантического поиска через Firestore Vector Search или Vertex AI.

Подробнее см. [Семантический поиск](semantic-search.md).

## См. также

- [Архитектурный обзор](overview.md)
- [Интеграция с Sanskrite](integration.md)
- [Базовая таксономия](taxonomy.md)
- [Семантический поиск](semantic-search.md)
- [Жизненный цикл](../workflows/lifecycle.md)

