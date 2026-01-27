# Knowledge Graph & Scenario Analysis System

**Status:** Design  
**Priority:** High  
**Estimate:** 3-6 months (milestone-based rollout)

## Executive Summary

Система глобальной базы знаний (Knowledge Graph) с мульти-доменными агентами для извлечения, анализа и генерации сценариев на основе реальных данных из внешних источников. Система позволяет анализировать сложные взаимосвязи между финансовыми, политическими, социокультурными и технологическими данными, выявлять тренды на разных временных горизонтах и генерировать комплексные сценарии для принятия решений.

## Problem Statement

Текущая система в unfold режиме работает как чат с рефлексией, но:
- Нет систематизации знаний из внешних источников
- Нет анализа взаимосвязей между разными типами данных
- Нет детекции трендов и прогнозирования
- Каждый тенант заново анализирует одни и те же публичные данные
- Нет комплексного анализа сложных вопросов, затрагивающих несколько доменов

## Solution Overview

### Core Concepts

1. **Global Knowledge Graph** - общая база знаний для всех тенантов (Layer 2)
2. **Domain Agents** - модульные агенты для разных типов данных (financial, political, social, technical)
3. **Multi-Perspective Analysis** - учет множественных точек зрения для чувствительных тем
4. **Cross-Domain Relations** - связи между данными разных доменов
5. **Temporal Trend Detection** - иерархическая детекция трендов на разных временных горизонтах
6. **Scenario Analysis** - генерация комплексных сценариев на основе знаний

### Architecture Layers

```
Layer 1: RAW EXTERNAL DATA (Global, shared)
├─ DataSources (web, telegram, RSS)
├─ SourceProfiles (trust scores, editorial stance)
├─ Documents (сырой контент)
└─ Chunks + Embeddings (векторизованный контент)

Layer 2: EXTRACTED KNOWLEDGE (Global, shared)
├─ Entities (люди, организации, места)
├─ Facts (утверждения, данные, статистика)
├─ Events (события, даты)
├─ Metrics (количественные данные)
├─ Trends (паттерны, тренды)
└─ Relations (связи между сущностями)

Layer 3: TENANT-SPECIFIC ANALYSIS (Private)
├─ Project Interactions (чаты, вопросы)
├─ Private Documents (внутренние документы)
├─ Project-specific Scenarios (связывающие Layer 2 с обсуждением)
└─ Decisions & Insights (выводы конкретного тенанта)
```

## Architecture

### Domain Agents

Модульная архитектура с отделимыми агентами для каждого домена знаний.

#### Interface

```go
type DomainAgent interface {
    Domain() string
    Name() string
    Version() string
    Capabilities() []string
    
    HandleQuery(ctx context.Context, req *AgentQueryRequest) (*AgentQueryResponse, error)
    ExtractKnowledge(ctx context.Context, doc *Document) (*ExtractedKnowledge, error)
    GenerateScenario(ctx context.Context, knowledge []DomainKnowledge) (*DomainScenario, error)
    Health(ctx context.Context) (*AgentHealth, error)
}
```

#### Agents

1. **FinancialAgent** - финансовые данные
   - Структурированные числовые данные
   - Временные ряды
   - Метрики и индикаторы
   - Высокая точность требований

2. **PoliticalAgent** - политические данные
   - Множественные перспективы (обязательно)
   - Политический контекст
   - Bias tracking
   - Balance scoring

3. **SocialAgent** - социокультурные данные
   - Культурный контекст
   - Социальные группы
   - Публичное мнение
   - Интерпретации

4. **TechnicalAgent** - технологические данные
   - Технические спецификации
   - Инновации
   - Производительность
   - Объективные факты

#### Base Agent

Общая логика для всех агентов:
- Embeddings
- Confidence calculation
- Time range filtering
- Trend detection
- Caching

#### Унификация с существующими агентами

**Архитектурное решение:** Иерархия интерфейсов с встраиванием (embedding) для унификации всех типов агентов.

**Иерархия интерфейсов:**

```
BaseAgent (базовый интерфейс)
├── ID(), Name(), DisplayName(), Description()
├── Skills(), Type(), IsExternal()
└── Health()

TaskAgent extends BaseAgent (для существующих агентов)
├── BaseAgent (встроен)
├── TaskSchema(), ResultSchema()
└── Execute(), ExecuteWithStream()

DomainAgent extends BaseAgent (для Knowledge Graph)
├── BaseAgent (встроен)
├── Domain(), Version(), Capabilities()
└── HandleQuery(), ExtractKnowledge(), GenerateScenario()

Agent (универсальный интерфейс)
├── BaseAgent (встроен)
└── ExecuteAsTask() (унифицированный метод выполнения)
```

**Типы агентов:**

1. **TaskAgent** (существующие):
   - Расширяет `BaseAgent`
   - Используют JSON схемы для задач (`TaskSchema`, `ResultSchema`)
   - Выполняют задачи через `Execute()` / `ExecuteWithStream()`
   - Примеры: `EntitiesAgent`, `DataAgent`

2. **DomainAgent** (новые для Knowledge Graph):
   - Расширяет `BaseAgent`
   - Специализированы на извлечение знаний и генерацию сценариев
   - Методы: `HandleQuery()`, `ExtractKnowledge()`, `GenerateScenario()`
   - Примеры: `FinancialAgent`, `PoliticalAgent`, `SocialAgent`, `TechnicalAgent`

**Адаптеры для унификации:**

Оба типа агентов адаптируются к универсальному `Agent` интерфейсу:

- `TaskAgentAdapter` - адаптирует `TaskAgent` → `Agent`
- `DomainAgentAdapter` - адаптирует `DomainAgent` → `Agent`

```go
// Регистрация TaskAgent через адаптер
registry.RegisterTaskAgent(entitiesAgent)

// Регистрация DomainAgent через адаптер
registry.RegisterDomainAgent(financialAgent)

// Использование через единый интерфейс Agent
agent, _ := registry.Get("financial")
result, _ := agent.ExecuteAsTask(ctx, taskJSON, onUpdate)
```

**Преимущества иерархии:**
- ✅ Единый базовый интерфейс `BaseAgent` для всех агентов
- ✅ Типизация через встраивание интерфейсов (interface embedding)
- ✅ Универсальный `Agent` интерфейс для Registry
- ✅ Обратная совместимость: существующие агенты не меняются
- ✅ Чистое разделение ответственности через специализированные интерфейсы
- ✅ Легко расширять: новые типы агентов могут расширять `BaseAgent`

**Реализация:**
- `backend/internal/agents/agent.go` - иерархия интерфейсов (`BaseAgent`, `TaskAgent`, `Agent`)
- `backend/internal/agents/domain_agent.go` - интерфейс `DomainAgent` и типы данных
- `backend/internal/agents/task_agent_adapter.go` - адаптер `TaskAgent` → `Agent`
- `backend/internal/agents/domain_agent_adapter.go` - адаптер `DomainAgent` → `Agent`
- `backend/internal/agents/registry.go` - методы `RegisterTaskAgent()` и `RegisterDomainAgent()` для регистрации через адаптеры

### Knowledge Graph Data Model

#### Entities

```go
type KnowledgeEntity struct {
    ID              string
    CanonicalName   string
    Type            string // "person", "organization", "location", "concept"
    Aliases         []string
    
    // Entity Resolution
    ClusterID       string
    MergedFrom      []string
    Confidence      float64
    
    Attributes      map[string]any
    Embedding       []float64
    Sources         []KnowledgeSourceRef
    
    FirstSeen       time.Time
    LastSeen        time.Time
    MentionCount    int
    Relevance       float64
    TrustScore      float64
}
```

#### Facts (Domain-Specific)

**FinancialKnowledge:**
- Structured metrics
- Time series data
- Verification requirements
- Precision tracking

**PoliticalKnowledge:**
- Multiple perspectives (required)
- Political context
- Source bias tracking
- Balance score

**SocialKnowledge:**
- Cultural context
- Affected groups
- Interpretations
- Subjectivity level

**TechnicalKnowledge:**
- Specifications
- Measurable values
- Verification methods

#### Relations

```go
type KnowledgeRelation struct {
    FromEntity      string
    ToEntity        string
    RelationType    string // "works_for", "located_in", "owns"
    Directed        bool
    ValidFrom       time.Time
    ValidUntil      *time.Time
    Strength        float64
    Confidence      float64
}
```

#### Cross-Domain Relations

```go
type CrossDomainRelation struct {
    FromDomain      string
    FromKnowledge   string
    ToDomain        string
    ToKnowledge     string
    RelationType    string // "causes", "influences", "correlates"
    Strength        float64
    Evidence        []Evidence
    ValidFrom       time.Time
    ValidUntil      *time.Time
}
```

### Trend Detection System

#### Temporal Hierarchy

```
HorizonImmediate  (< 1 день)
HorizonShortTerm  (1 день - 1 неделя)
HorizonMediumTerm (1 неделя - 3 месяца)
HorizonLongTerm   (3 месяца - 1 год)
HorizonStrategic  (> 1 год)
```

#### Trend Structure

```go
type Trend struct {
    ID              string
    Domain          string
    Type            string // "increase", "decrease", "oscillation"
    Horizon         TemporalHorizon
    Strength        float64
    Confidence      float64
    Direction       string // "up", "down", "sideways"
    Velocity        float64
    Acceleration    float64
    
    // Hierarchy
    ParentTrend     *string
    ChildTrends     []string
    
    // Cross-domain
    RelatedTrends   []RelatedTrend
    
    // Forecast
    Forecast        *TrendForecast
}
```

#### Cross-Domain Trends

```go
type CrossDomainTrend struct {
    Domains         []string
    Pattern         string // "cascade", "correlation", "causation"
    DomainTrends    map[string]string
    TimeLag         map[string]int
    LeadingDomain   string
    Strength        float64
    Horizon         TemporalHorizon
}
```

### Scenario Analysis

#### Multi-Domain Scenarios

```go
type ScenarioAnalysis struct {
    // Stage information
    Stage               string                  `json:"stage"`      // "quick_scan" or "enriched"
    Status              string                  `json:"status"`     // "preliminary" or "final"
    Confidence          float64                 `json:"confidence"` // Overall confidence
    
    // Knowledge Base (for graph visualization)
    KnowledgeBase       KnowledgeBaseSnapshot   `json:"knowledgeBase"`
    
    // Scenarios
    Scenarios           []Scenario              `json:"scenarios"`
    AlternativeScenarios []AlternativeScenario  `json:"alternativeScenarios,omitempty"`
    
    // Cross-domain analysis
    CrossDomainLinks    []CrossDomainLink       `json:"crossDomainLinks"`
    
    // Assessment
    Risks               []RiskAssessment        `json:"risks,omitempty"`
    Uncertainty         *UncertaintyAnalysis    `json:"uncertainty,omitempty"`
    
    // For Stage 1 (preliminary) only
    PendingTasks        []TaskSpec              `json:"pendingTasks,omitempty"`
    
    // For Stage 2 (enriched) only
    Enrichment          *EnrichmentInfo         `json:"enrichment,omitempty"`
}

type KnowledgeBaseSnapshot struct {
    Entities    []KnowledgeEntity   `json:"entities"`
    Facts       []KnowledgeFact     `json:"facts"`
    Trends      []Trend             `json:"trends"`
    Events      []KnowledgeEvent    `json:"events,omitempty"`
    Metrics     []KnowledgeMetric   `json:"metrics,omitempty"`
    AgentResults map[string]AgentResult `json:"agentResults,omitempty"` // Only in enriched stage
}

type EnrichmentInfo struct {
    NewEntities           int     `json:"newEntities"`
    NewFacts              int     `json:"newFacts"`
    NewTrends             int     `json:"newTrends"`
    ImprovedScenarios     bool    `json:"improvedScenarios"`
    ConfidenceImprovement float64 `json:"confidenceImprovement"` // e.g., 0.27 (0.55 → 0.82)
}

type QuickScanResult struct {
    Scenarios       []Scenario              `json:"scenarios"`
    KnowledgeBase   KnowledgeBaseSnapshot   `json:"knowledgeBase"`
    PendingTasks    []TaskSpec              `json:"pendingTasks"`
    Confidence      float64                 `json:"confidence"`
    Status          string                  `json:"status"`
    ProcessingTime  time.Duration           `json:"processingTime"`
}
```

#### Scenario Structure

```go
type Scenario struct {
    ID                  string                      `json:"id"`
    Title               string                      `json:"title"`
    Description         string                      `json:"description"`
    Type                string                      `json:"type"` // "baseline", "optimistic", "pessimistic"
    
    // Domains
    Domains             []string                    `json:"domains"`
    DomainScenarios     map[string]DomainScenario   `json:"domainScenarios,omitempty"`
    
    // Cross-domain
    CrossDomainLinks    []CrossDomainLink           `json:"crossDomainLinks,omitempty"`
    
    // Contributions (для графового представления)
    ContributingFacts   []string                    `json:"contributingFacts,omitempty"`   // Fact IDs
    ContributingTrends  []string                    `json:"contributingTrends,omitempty"`  // Trend IDs
    ContributingEvents  []string                    `json:"contributingEvents,omitempty"`  // Event IDs
    
    // Assessment
    Likelihood          float64                     `json:"likelihood"` // 0-1 (0.7 = 70%)
    Confidence          float64                     `json:"confidence"` // 0-1
    Impact              MultiDomainImpact           `json:"impact"`
    
    // Sources
    Sources             []SourceRef                 `json:"sources"`
}
```

**TypeScript Interfaces (Frontend):**

```typescript
interface ScenarioAnalysis {
  // Stage information
  stage: "quick_scan" | "enriched";
  status: "preliminary" | "final";
  confidence: number; // 0-1
  
  // Knowledge Base (for graph visualization)
  knowledgeBase: KnowledgeBaseSnapshot;
  
  // Scenarios
  scenarios: Scenario[];
  alternativeScenarios?: AlternativeScenario[];
  
  // Cross-domain analysis
  crossDomainLinks: CrossDomainLink[];
  
  // Assessment
  risks?: RiskAssessment[];
  uncertainty?: UncertaintyAnalysis;
  
  // For Stage 1 (preliminary) only
  pendingTasks?: TaskSpec[];
  
  // For Stage 2 (enriched) only
  enrichment?: EnrichmentInfo;
}

interface KnowledgeBaseSnapshot {
  entities: KnowledgeEntity[];
  facts: KnowledgeFact[];
  trends: Trend[];
  events?: KnowledgeEvent[];
  metrics?: KnowledgeMetric[];
  agentResults?: Record<string, AgentResult>; // Only in enriched stage
}

interface EnrichmentInfo {
  newEntities: number;
  newFacts: number;
  newTrends: number;
  improvedScenarios: boolean;
  confidenceImprovement: number; // e.g., 0.27 (0.55 → 0.82)
}

interface Scenario {
  id: string;
  title: string;
  description: string;
  type: "baseline" | "optimistic" | "pessimistic";
  
  // Domains
  domains: string[];
  domainScenarios?: Record<string, DomainScenario>;
  
  // Cross-domain
  crossDomainLinks?: CrossDomainLink[];
  
  // Contributions (для графового представления)
  contributingFacts?: string[];   // Fact IDs
  contributingTrends?: string[];  // Trend IDs
  contributingEvents?: string[];  // Event IDs
  
  // Assessment
  likelihood: number; // 0-1 (0.7 = 70%)
  confidence: number; // 0-1
  impact: MultiDomainImpact;
  
  // Sources
  sources: SourceRef[];
}

interface TaskSpec {
  agent: string;
  task: string;
  priority: number;
  required: boolean;
}
```

## Key Features

### 1. Entity Resolution

**Дедупликация сущностей с использованием:**
- Semantic similarity (embeddings)
- Attribute matching
- Context validation
- Conservative thresholds (0.9+ for merge)

**Strategies:**
- Type-based filtering (person != organization)
- Critical attribute checking (birthDate, nationality)
- Context matching (sources similarity)

### 2. Multi-Perspective Extraction

**Для чувствительных тем (политика, социум):**
- Neutral statement extraction
- Multiple perspectives identification
- Source bias tracking
- Balance scoring

**Bias Mitigation:**
- Multi-prompt strategies
- Source diversity tracking
- Confidence calibration
- Perspective balance

### 3. Temporal Knowledge

**Versioning Strategies:**
- **Refinement data:** Replace when refined
- **Evolving data:** Version all changes
- **Temporal queries:** Knowledge at time T

**Implementation:**
- ValidFrom/ValidUntil fields
- Version tracking
- Snapshot generation (for performance)

### 4. Cross-Domain Analysis

**Automatic Detection:**
- Correlation analysis
- Temporal lag detection
- Causality inference (with caution)
- Pattern recognition (cascade, correlation, divergence)

**Consistency Validation:**
- Temporal alignment
- Value conflicts
- Logical consistency
- Cross-domain checks

### 5. Trend Detection

**Multi-Horizon Detection:**
- Statistical methods (for quantitative)
- LLM-based (for qualitative)
- Hierarchical organization
- Cross-domain patterns

**Trend Types:**
- Increase/Decrease
- Oscillation
- Emergence
- Decline

## Unified Chat Interface

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Scenario Analysis Flow                                 │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  Stage 1: Quick Scanner (< 1 min)                      │
│  ┌──────────────────────────────────────────────────┐  │
│  │ QuickAnalyzer                                    │  │
│  │ ├─ Query Analysis (LLM, fast)                  │  │
│  │ ├─ Knowledge Lookup (Firestore)                │  │
│  │ ├─ Quick Scenario Gen (LLM, fast)               │  │
│  │ └─ Task Planning                                │  │
│  └──────────────────────────────────────────────────┘  │
│            │                                            │
│            ▼                                            │
│  Return preliminary Interaction                        │
│                                                          │
│  Stage 2: Agent Orchestration (async, 2-10 min)       │
│  ┌──────────────────────────────────────────────────┐  │
│  │ ScenarioOrchestrator                             │  │
│  │ ├─ Agent Task Distribution                      │  │
│  │ ├─ Parallel Agent Execution                     │  │
│  │ ├─ Progress Tracking (WebSocket)                │  │
│  │ ├─ Result Aggregation                           │  │
│  │ └─ Scenario Enrichment                          │  │
│  └──────────────────────────────────────────────────┘  │
│            │                                            │
│            ▼                                            │
│  Update Interaction (enriched)                          │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### Router

```go
type KnowledgeRouter struct {
    agents              map[string]DomainAgent
    crossDomainService  *CrossDomainService
    quickAnalyzer       *QuickAnalyzer          // Stage 1
    scenarioOrchestrator *ScenarioOrchestrator   // Stage 2
    intentClassifier    *IntentClassifier
}

// QuickAnalyzer - быстрый скрининг
type QuickAnalyzer struct {
    genaiClient        *gen_combined.Client
    knowledgeRepo       *KnowledgeRepository
    trendsRepo          *TrendsRepository
    entitiesRepo        *KnowledgeEntitiesRepository
}

// ScenarioOrchestrator - оркестрация агентов
type ScenarioOrchestrator struct {
    agents              map[string]DomainAgent
    crossDomainService  *CrossDomainService
    scenarioAnalyzer    *ScenarioAnalyzer
    publisher           *realtime.Publisher // WebSocket updates
}
```

### QuickAnalyzer Implementation

```go
func (q *QuickAnalyzer) QuickScan(ctx context.Context, query string, projectID string) (*QuickScanResult, error) {
    startTime := time.Now()
    
    // 1. Query Analysis (5-10s)
    analysis := q.analyzeQuery(ctx, query)
    
    // 2. Knowledge Lookup (2-5s)
    existingKnowledge := q.lookupKnowledge(ctx, analysis.Entities, analysis.Domains)
    
    // 3. Quick Scenario Generation (10-20s)
    quickScenarios := q.generateQuickScenarios(ctx, query, existingKnowledge)
    
    // 4. Task Planning (2-5s)
    tasks := q.planAgentTasks(ctx, analysis, existingKnowledge)
    
    return &QuickScanResult{
        Scenarios: quickScenarios,
        KnowledgeBase: existingKnowledge,
        PendingTasks: tasks,
        Confidence: 0.55, // Low, will be improved
        Status: "preliminary",
        ProcessingTime: time.Since(startTime),
    }, nil
}

func (q *QuickAnalyzer) analyzeQuery(ctx context.Context, query string) *QueryAnalysis {
    // Быстрый LLM анализ (gemini_flash)
    prompt := buildQueryAnalysisPrompt(query)
    
    result, _ := q.genaiClient.GenerateJSONStrict(ctx, prompt, nil, getQueryAnalysisSchema(), gen_combined.Options{
        ModelOverride: "gemini_flash",
        Temperature: 0.3,
        MaxOutputTokens: 1024,
    })
    
    var analysis QueryAnalysis
    json.Unmarshal([]byte(result.Content), &analysis)
    
    return &analysis
}

type QueryAnalysis struct {
    Domains  []string   `json:"domains"`
    Entities []string   `json:"entities"`
    Tasks    []TaskSpec `json:"tasks"`
}

type TaskSpec struct {
    Agent     string   `json:"agent"`
    Task      string   `json:"task"`
    Priority  int      `json:"priority"`
    Required  bool     `json:"required"`
}
```

### ScenarioOrchestrator Implementation

```go
func (o *ScenarioOrchestrator) OrchestrateAgents(ctx context.Context, quickScan *QuickScanResult, projectID string) error {
    // Публикуем начало оркестрации
    o.publisher.PublishJSON(ctx, realtime.EventScenarioOrchestrationStarted, projectID, map[string]any{
        "projectId": projectID,
        "tasks": quickScan.PendingTasks,
    })
    
    // Запускаем агентов параллельно
    var wg sync.WaitGroup
    results := make(map[string]*AgentResult)
    errors := make(map[string]error)
    
    for _, task := range quickScan.PendingTasks {
        if !task.Required {
            continue // Опциональные задачи пропускаем для скорости
        }
        
        agent, ok := o.agents[task.Agent]
        if !ok {
            continue
        }
        
        wg.Add(1)
        go func(t TaskSpec) {
            defer wg.Done()
            
            // Публикуем прогресс
            o.publishProgress(ctx, projectID, t.Agent, 0, "Starting...")
            
            // Выполняем задачу
            result, err := o.executeAgentTask(ctx, agent, t, projectID)
            
            if err != nil {
                errors[t.Agent] = err
                o.publishProgress(ctx, projectID, t.Agent, 0, fmt.Sprintf("Error: %v", err))
            } else {
                results[t.Agent] = result
                o.publishProgress(ctx, projectID, t.Agent, 100, "Completed")
            }
        }(task)
    }
    
    wg.Wait()
    
    // Агрегируем результаты
    enriched := o.enrichScenario(ctx, quickScan, results)
    
    // Обновляем Interaction
    o.updateInteraction(ctx, projectID, enriched)
    
    // Публикуем завершение
    o.publisher.PublishJSON(ctx, realtime.EventScenarioOrchestrationCompleted, projectID, map[string]any{
        "projectId": projectID,
        "enrichment": enriched.Enrichment,
    })
    
    return nil
}

func (o *ScenarioOrchestrator) executeAgentTask(ctx context.Context, agent DomainAgent, task TaskSpec, projectID string) (*AgentResult, error) {
    // Создаем callback для прогресса
    progressCallback := func(update AgentUpdate) {
        o.publishProgress(ctx, projectID, task.Agent, update.Progress, update.Message)
    }
    
    // Выполняем задачу через агента
    req := &AgentQueryRequest{
        Query: task.Task,
        ProjectID: projectID,
        Intent: "analyze",
    }
    
    resp, err := agent.HandleQuery(ctx, req)
    if err != nil {
        return nil, err
    }
    
    return &AgentResult{
        Agent: task.Agent,
        Data: resp.Data,
        Confidence: resp.Confidence,
        ProcessingTime: resp.ProcessingTime,
    }, nil
}

func (o *ScenarioOrchestrator) publishProgress(ctx context.Context, projectID, agentID string, progress int, message string) {
    o.publisher.PublishJSON(ctx, realtime.EventScenarioAgentProgress, projectID, map[string]any{
        "agentId": agentID,
        "progress": progress,
        "message": message,
    })
}
```

### Request Flow

**Scenario Mode Flow:**

1. User query → Chat Interface (UnfoldRequest, mode="scenario")
2. POST `/api/v1/chat/knowledge/scenario`
3. **Stage 1: Quick Scan (< 1 min)**
   - QuickAnalyzer.QuickScan()
     - Query Analysis (LLM, fast) → domains, entities, tasks
     - Knowledge Lookup (Firestore) → existing knowledge
     - Quick Scenario Gen (LLM, fast) → preliminary scenarios
     - Task Planning → agent tasks
   - Return Interaction (preliminary, confidence ~0.55)
   - Start Stage 2 async (не блокируем ответ)
4. **Stage 2: Agent Orchestration (async, 2-10 min)**
   - ScenarioOrchestrator.OrchestrateAgents()
     - Запуск агентов параллельно
     - WebSocket progress updates
     - Aggregation результатов
     - Scenario enrichment
   - Update Interaction (enriched, confidence ~0.8+)
   - WebSocket notification

**Other Modes Flow:**

- `text` → обычный чат (текущая логика, без изменений)
- `graph` → graph mode (если реализован, без изменений)

### Chat Integration

**UnfoldRequest.tsx Changes:**
```typescript
// Добавляется режим "scenario"
<select value={mode} onChange={handleModeChange}>
  <option value="text">Text</option>
  <option value="graph">Graph</option>
  <option value="scenario">Scenario Analysis</option> // НОВОЕ
</select>

// При отправке запроса
if (mode === 'scenario') {
  // POST /api/v1/chat/knowledge/scenario
  // Вместо обычного /api/v1/genai/generate
}
```

**Response Handling:**
- **Text mode:** обычные текстовые ответы (как сейчас)
- **Scenario mode:** структурированные scenario-сообщения

### Scenario Messages

**Interaction Structure:**
```go
type Interaction struct {
    // ... existing fields ...
    Kind: "note" | "system"
    Subkind: "scenario.analysis"
    Content: JSON.stringify(ScenarioAnalysis) // Полная структура
    ContentSanitized: JSON.stringify(ScenarioAnalysis)
    
    // Метаданные для scenario
    Metadata: {
        Domains: []string
        Confidence: float64
        KnowledgeCount: int
        ProcessingTime: time.Duration
    }
}
```

**Frontend Component:**
```typescript
// ScenarioMessage.tsx - специальный компонент для scenario
<ScenarioMessage 
  scenario={parsedScenarioAnalysis}
  domains={scenario.domains}
  onExpand={...}
/>

// Отображает:
// - Summary текстом
// - Scenarios list (collapsible)
// - Cross-domain links visualization
// - Trends timeline
// - Risks & uncertainties
// - Interactive exploration
```

### Message Type Detection

**В ChatMessage или DiscoveryPage:**
```typescript
// При рендеринге Interaction
if (interaction.subkind === 'scenario.analysis') {
  // Парсим Content как ScenarioAnalysis
  const scenario = JSON.parse(interaction.content);
  return <ScenarioMessage scenario={scenario} />;
} else {
  // Обычный текст
  return <ChatMessage>{interaction.content}</ChatMessage>;
}
```

**Сложность scenario-сообщений:**
- Текущие сообщения: простой текст (string)
- Scenario сообщения: сложная структура (ScenarioAnalysis object)
  - Множественные сценарии
  - Кросс-доменные связи
  - Тренды и прогнозы
  - Риски и неопределенности
  - Интерактивные элементы

### UI Components Required

**Новые компоненты:**
1. **ScenarioMessage.tsx** - главный компонент для scenario
2. **ScenarioCard.tsx** - карточка одного сценария
3. **TrendTimeline.tsx** - визуализация трендов
4. **CrossDomainGraph.tsx** - граф связей между доменами
5. **RiskIndicator.tsx** - индикаторы рисков
6. **ScenarioComparison.tsx** - сравнение сценариев

**Расширение существующих:**
- **ChatMessage.tsx** - добавить проверку subkind
- **DiscoveryPage.tsx** - обработка scenario-сообщений
- **UnfoldRequest.tsx** - добавление режима "scenario"

### Two-Stage Processing

**Этап 1: Быстрый скрининг (< 1 минута)**

Цель: Быстро дать пользователю ответ на основе существующих знаний и определить задачи для агентов.

**Процесс:**
1. Query analysis (LLM, быстрый)
   - Определение доменов
   - Извлечение ключевых entities/facts из запроса
   - Понимание задач (что нужно найти/проанализировать)
2. Quick knowledge lookup
   - Поиск существующих знаний в Knowledge Graph
   - Быстрый скрининг трендов
   - Базовая структурированная база для анализа
3. Task planning
   - Определение задач для агентов
   - Приоритизация задач
   - Планирование оркестрации
4. Quick scenario generation
   - Базовая генерация сценариев на основе существующих знаний
   - Confidence может быть низким (0.5-0.6)
   - Отмечается как "preliminary"

**Результат (Interaction #1):**
```go
Interaction {
    kind: "note",
    subkind: "scenario.analysis",
    content: JSON.stringify(ScenarioAnalysis{
        Stage: "quick_scan",
        Status: "preliminary",
        Confidence: 0.55,
        
        KnowledgeBase: KnowledgeBaseSnapshot{
            Entities: [...], // Из существующей базы
            Facts: [...],
            Trends: [...],
        },
        
        Scenarios: []Scenario{
            {
                ID: "baseline_1",
                Title: "Economic decline leads to political instability",
                Type: "baseline",
                Likelihood: 0.7,  // 70%
                Confidence: 0.55,
                Domains: []string{"financial", "political"},
                ContributingFacts: []string{"fact_gdp_decline", "fact_approval_drop"},
                Sources: [...],
            },
        },
        
        CrossDomainLinks: []CrossDomainLink{
            {
                SourceDomain: "financial",
                TargetDomain: "political",
                RelationType: "causes",
                TimeLag: 30, // days
                Strength: 0.75,
            },
        },
        
        PendingTasks: []TaskSpec{
            { Agent: "financial", Task: "Get GDP data for 2024", Priority: 1, Required: true },
            { Agent: "political", Task: "Get approval ratings", Priority: 1, Required: true },
        },
    })
}
```

**Этап 2: Асинхронная оркестрация агентов**

Цель: Обогатить быстрый скрининг данными от агентов.

**Процесс:**
1. Запуск агентов параллельно
   - FinancialAgent: собирает финансовые данные
   - PoliticalAgent: собирает политические данные
   - SocialAgent: собирает социокультурные данные (если нужно)
2. Агенты работают независимо
   - Каждый агент выполняет свою задачу
   - Может занимать минуты
   - Прогресс отслеживается через WebSocket
3. Агрегация результатов
   - Объединение результатов от всех агентов
   - Обновление Knowledge Graph (если нашли новое)
   - Cross-domain analysis
4. Обогащение сценариев
   - Пересчет сценариев с новыми данными
   - Улучшение confidence
   - Добавление деталей и источников

**Результат (Interaction #2 - обновление):**
```go
Interaction {
    id: "<same as quick scan>", // Обновляем существующий
    kind: "note",
    subkind: "scenario.analysis",
    content: JSON.stringify(ScenarioAnalysis{
        Stage: "enriched",
        Status: "final",
        Confidence: 0.82,
        
        KnowledgeBase: KnowledgeBaseSnapshot{
            Entities: [...],  // + новые от агентов
            Facts: [...],     // + новые факты
            Trends: [...],    // + новые тренды
            AgentResults: map[string]AgentResult{
                "financial": { DataPoints: [...], Confidence: 0.85 },
                "political": { Perspectives: [...], Confidence: 0.78 },
            },
        },
        
        Scenarios: []Scenario{
            {
                ID: "baseline_1",
                Title: "Economic decline leads to political instability",
                Type: "baseline",
                Description: "GDP decline of 5% correlates with 15% approval drop, leading to increased protests...",
                Likelihood: 0.7,  // 70%
                Confidence: 0.82, // Улучшенный
                Domains: []string{"financial", "political"},
                ContributingFacts: []string{"fact_gdp_decline", "fact_approval_drop", "fact_protest_increase"},
                ContributingTrends: []string{"trend_gdp_declining", "trend_approval_declining"},
                Sources: [...],  // +12 источников
            },
            {
                ID: "optimistic_1",
                Title: "Quick economic recovery maintains stability",
                Type: "optimistic",
                Likelihood: 0.2,  // 20%
                Confidence: 0.72,
                // ...
            },
            {
                ID: "pessimistic_1",
                Title: "Prolonged crisis leads to regime change",
                Type: "pessimistic",
                Likelihood: 0.1,  // 10%
                Confidence: 0.75,
                // ...
            },
        },
        
        CrossDomainLinks: []CrossDomainLink{
            {
                SourceDomain: "financial",
                TargetDomain: "political",
                RelationType: "causes",
                TimeLag: 30,
                Strength: 0.75,
                Confidence: 0.82,
                Description: "GDP decline leads to approval rating drop after 30 days",
                Sources: [...],
            },
        },
        
        Risks: []RiskAssessment{
            {
                ID: "risk_political_instability",
                Severity: "high",
                Description: "Political instability risk due to declining approval",
                Likelihood: 0.65,
            },
        },
        
        Enrichment: &EnrichmentInfo{
            NewEntities: 12,
            NewFacts: 45,
            NewTrends: 3,
            ImprovedScenarios: true,
            ConfidenceImprovement: 0.27, // 0.55 → 0.82
        },
    })
}
```

### Practical Flow Example

**User Action:**
1. Пользователь выбирает режим "scenario" в UnfoldRequest
2. Вводит запрос: "Как экономический кризис влияет на политическую стабильность?"

**Stage 1: Quick Scan (< 1 минута)**
```
POST /api/v1/chat/knowledge/scenario
{
  "query": "Как экономический кризис влияет на политическую стабильность?",
  "projectId": "project_123",
  "mode": "scenario"
}

→ QuickAnalysis:
   1. LLM analysis (gemini_flash, 5-10s)
      - Домены: ["financial", "political"]
      - Entities: ["GDP", "approval rating", "protests"]
      - Tasks: ["Get GDP trends", "Get approval ratings", "Get protest data"]
   
   2. Knowledge lookup (Firestore, 2-5s)
      - Существующие знания по этим entities
      - Базовые тренды (если есть)
   
   3. Quick scenario generation (gemini_fast, 10-20s)
      - Базовые сценарии на существующих данных
      - Confidence: 0.55 (низкий, но есть что показать)
   
   4. Task planning (2-5s)
      - Определение задач для агентов
      - Приоритизация

→ Response (через 20-40 секунд):
   Interaction {
     subkind: "scenario.analysis",
     content: {
       stage: "quick_scan",
       scenarios: [...], // Базовые
       pendingTasks: [...],
       status: "preliminary"
     }
   }
```

**Stage 2: Agent Orchestration (асинхронно, 2-10 минут)**
```
→ WebSocket: "Starting agent orchestration..."

→ FinancialAgent.start() (параллельно)
   - Собирает GDP данные
   - Анализирует тренды
   - Прогресс: 0% → 50% → 100%
   
→ PoliticalAgent.start() (параллельно)
   - Собирает approval ratings
   - Анализирует перспективы
   - Прогресс: 0% → 50% → 100%

→ WebSocket updates:
   "FinancialAgent: 50% - Collecting market data..."
   "PoliticalAgent: 30% - Analyzing sources..."
   "FinancialAgent: 100% - Completed"
   "PoliticalAgent: 100% - Completed"

→ Aggregation & Enrichment:
   - Объединяет результаты агентов
   - Обновляет Knowledge Graph
   - Пересчитывает сценарии
   - Улучшает confidence: 0.55 → 0.82

→ Final Update (через 3-5 минут):
   Interaction.update() {
     content: {
       stage: "enriched",
       scenarios: [...], // Обогащенные
       confidence: 0.82,
       status: "final",
       enrichment: {
         newEntities: 12,
         newFacts: 45,
         improvedScenarios: true
       }
     }
   }
   
→ WebSocket: "Scenario analysis enriched with agent data"
```

**Frontend Display:**
```typescript
// DiscoveryPage.tsx
{interactions.map(interaction => {
  if (interaction.subkind === 'scenario.analysis') {
    const scenario = JSON.parse(interaction.content);
    return (
      <ScenarioMessage 
        key={interaction.id}
        scenario={scenario}
        interactionId={interaction.id}
      />
    );
  }
  // Обычное сообщение
  return <ChatMessage>...</ChatMessage>;
})}
```

**ScenarioMessage отображает:**

**Stage 1 (Preliminary):**
```
┌─────────────────────────────────────────────────┐
│ ⚡ Quick Scan Results (20-40s)                  │
├─────────────────────────────────────────────────┤
│ Status: Preliminary • Confidence: 55%          │
│                                                   │
│ Summary: На основе существующих данных...        │
│                                                   │
│ ┌─ Preliminary Scenarios ────────────────────┐ │
│ │ ● Baseline (based on historical patterns)   │ │
│ │   [Limited details, will be enriched]       │ │
│ └──────────────────────────────────────────────┘ │
│                                                   │
│ 🔄 Enriching with agent data...                  │
│    • FinancialAgent: Starting...                  │
│    • PoliticalAgent: Starting...                  │
└─────────────────────────────────────────────────┘
```

**Stage 2 (Enriched):**
```
┌─────────────────────────────────────────────────┐
│ ✅ Enriched Analysis (3-5 min)                  │
├─────────────────────────────────────────────────┤
│ Status: Final • Confidence: 82%                │
│                                                   │
│ Summary: Комплексный анализ показывает...        │
│                                                   │
│ ┌─ Scenarios ─────────────────────────────────┐ │
│ │ ● Baseline (70% likelihood) ✅             │ │
│ │   GDP ↓5% → Approval ↓15% → Protests ↑40%  │ │
│ │   [12 sources, 45 facts, high confidence]  │ │
│ │                                           │ │
│ │ ● Optimistic (20% likelihood)             │ │
│ │   Quick recovery → Stability maintained   │ │
│ │   [8 sources, 23 facts]                    │ │
│ │                                           │ │
│ │ ● Pessimistic (10% likelihood)             │ │
│ │   Prolonged crisis → Regime change        │ │
│ │   [15 sources, 67 facts]                    │ │
│ └───────────────────────────────────────────┘ │
│                                                   │
│ ┌─ Cross-Domain Links ───────────────────────┐ │
│ │ Financial ─[30d]→ Political ─[60d]→ Social │ │
│ │ Strength: 0.75 • Cascade pattern            │ │
│ └──────────────────────────────────────────────┘ │
│                                                   │
│ ┌─ Trends ───────────────────────────────────┐ │
│ │ GDP: ▾▾▾ (declining, -5.2% YoY)           │ │
│ │ Approval: ▾▾ (decreasing, -15pts)          │ │
│ │ Protests: ▴▴▴ (increasing, +40% events)    │ │
│ └──────────────────────────────────────────────┘ │
│                                                   │
│ Risks: High (political instability)              │
│ Enrichment: +12 entities, +45 facts, +3 trends   │
└─────────────────────────────────────────────────┘
```

**UI States:**
- **Loading:** "Analyzing scenario..." (первые 20-40s)
- **Preliminary:** Quick scan результаты с индикатором обогащения
- **Enriching:** Прогресс агентов через WebSocket
- **Final:** Обогащенные результаты с полными данными

**Отличие от текстовых сообщений:**
- Текстовое: "Экономический кризис обычно приводит к политической нестабильности..."
- Scenario Preliminary: Структурированные данные (базовые, confidence ~0.55)
- Scenario Enriched: Полные структурированные данные с источниками, трендами, рисками (confidence ~0.8+)

### Response Formats

**Text Mode (current):**
- Простой текст от LLM
- Markdown поддержка
- Inline code blocks

**Scenario Mode (new):**
- **Summary:** текстовое резюме (для быстрого понимания)
- **Structured Data:** полная структура ScenarioAnalysis
- **Visualizations:**
  - Timeline с трендами
  - Cross-domain connections graph
  - Scenario comparison table
  - Risk indicators
  - Confidence indicators
  - **React Flow Graph:** интерактивный граф узлов и связей

**Unified View (Palantir-style):**
- Summary
- Key points
- Timeline
- Metrics cards
- Perspectives

**Domain-Specific Views:**
- Financial: Charts, tables, time series
- Political: Perspectives, timeline, balance
- Social: Cultural context, groups
- Technical: Specifications, metrics

## Graph Visualization (React Flow)

### Overview

Scenario messages могут быть отображены как интерактивный граф в React Flow, показывающий:
- **Узлы (Nodes):** Entities, Facts, Trends, Scenarios, Sources, Metrics
- **Связи (Edges):** Influences, Contributions, Causality, Correlations, Evidence
- **Метаданные:** Confidence, Sources, Timestamps, Contributions, Domain

### Graph Data Structure

```typescript
interface ScenarioGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  metadata: {
    scenarioId: string;
    confidence: number;
    domains: string[];
    totalEntities: number;
    totalFacts: number;
    totalTrends: number;
  };
}

interface GraphNode {
  id: string;
  type: NodeType;
  data: NodeData;
  position: { x: number; y: number }; // Auto-layout
}

type NodeType = 
  | "entity"           // Сущность (Person, Organization, Concept)
  | "fact"             // Факт/утверждение
  | "trend"            // Тренд
  | "scenario"          // Сценарий (baseline, optimistic, pessimistic)
  | "source"            // Источник данных
  | "metric"            // Метрика/цифра
  | "risk"              // Риск
  | "domain"            // Домен (Financial, Political, etc.)

interface NodeData {
  // Common
  label: string;
  domain: string;
  confidence: number;
  sources: SourceRef[];
  
  // Entity-specific
  entityType?: string;  // "person", "organization", "concept"
  attributes?: Record<string, any>;
  
  // Fact-specific
  subject?: string;     // Entity ID
  predicate?: string;
  object?: string;      // Entity ID or value
  perspectives?: PerspectiveData[];
  
  // Trend-specific
  direction?: "increasing" | "decreasing" | "stable" | "oscillating";
  horizon?: TemporalHorizon;
  velocity?: number;
  
  // Scenario-specific
  likelihood?: number;
  scenarioType?: "baseline" | "optimistic" | "pessimistic";
  
  // Metric-specific
  value?: number;
  unit?: string;
  timestamp?: string;
  
  // Risk-specific
  severity?: "low" | "medium" | "high" | "critical";
  riskType?: string;
}

interface GraphEdge {
  id: string;
  source: string;        // Node ID
  target: string;       // Node ID
  type: EdgeType;
  data: EdgeData;
  animated?: boolean;   // Для временных связей
}

type EdgeType =
  | "influences"        // Влияет на (entity → entity)
  | "contributes"       // Контрибутит к (fact → scenario, trend → scenario)
  | "causes"            // Причина (fact → fact, trend → trend)
  | "evidences"         // Подтверждает (fact → fact, source → fact)
  | "measures"          // Измеряет (metric → entity, metric → trend)
  | "indicates"        // Указывает на (trend → risk, metric → risk)
  | "temporal"          // Временная связь (cross-domain, lag)
  | "correlates"        // Коррелирует с (trend ↔ trend)

interface EdgeData {
  label: string;
  strength: number;      // 0-1, сила связи
  confidence: number;    // 0-1, уверенность в связи
  lag?: number;         // Временной лаг (дни)
  contribution?: number; // 0-1, вклад в результат
  sources: SourceRef[];
  domain?: string;      // Для cross-domain
  description?: string; // Описание связи
}
```

### Graph Transformation

**Из ScenarioAnalysis в Graph:**

```typescript
function scenarioToGraph(scenario: ScenarioAnalysis): ScenarioGraph {
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];
  
  // 1. Entities → nodes
  scenario.knowledgeBase.entities.forEach(entity => {
    nodes.push({
      id: `entity_${entity.id}`,
      type: "entity",
      data: {
        label: entity.canonicalName,
        domain: entity.domain,
        confidence: entity.confidence,
        entityType: entity.type,
        attributes: entity.attributes,
        sources: entity.sources,
      },
      position: { x: 0, y: 0 }, // Auto-layout
    });
  });
  
  // 2. Facts → nodes
  scenario.knowledgeBase.facts.forEach(fact => {
    nodes.push({
      id: `fact_${fact.id}`,
      type: "fact",
      data: {
        label: fact.summary,
        domain: fact.domain,
        confidence: fact.confidence,
        subject: fact.subjectEntity,
        predicate: fact.predicate,
        object: fact.objectEntity || fact.objectValue,
        perspectives: fact.perspectives,
        sources: fact.sources,
      },
      position: { x: 0, y: 0 },
    });
    
    // Fact → Entity edges (subject/object)
    if (fact.subjectEntity) {
      edges.push({
        id: `edge_${fact.id}_subject`,
        source: `entity_${fact.subjectEntity}`,
        target: `fact_${fact.id}`,
        type: "evidences",
        data: {
          label: "subject of",
          strength: 1.0,
          confidence: fact.confidence,
          sources: fact.sources,
        },
      });
    }
  });
  
  // 3. Trends → nodes
  scenario.knowledgeBase.trends.forEach(trend => {
    nodes.push({
      id: `trend_${trend.id}`,
      type: "trend",
      data: {
        label: `${trend.entity}: ${trend.direction}`,
        domain: trend.domain,
        confidence: trend.confidence,
        direction: trend.direction,
        horizon: trend.horizon,
        velocity: trend.velocity,
        sources: trend.sources,
      },
      position: { x: 0, y: 0 },
    });
    
    // Trend → Entity edge
    edges.push({
      id: `edge_${trend.id}_entity`,
      source: `entity_${trend.entity}`,
      target: `trend_${trend.id}`,
      type: "measures",
      data: {
        label: "trends",
        strength: 0.8,
        confidence: trend.confidence,
        sources: trend.sources,
      },
    });
  });
  
  // 4. Scenarios → nodes
  scenario.scenarios.forEach((scenarioItem, idx) => {
    nodes.push({
      id: `scenario_${idx}`,
      type: "scenario",
      data: {
        label: `${scenarioItem.type} (${Math.round(scenarioItem.likelihood * 100)}%)`,
        domain: "cross-domain",
        confidence: scenario.confidence,
        likelihood: scenarioItem.likelihood,
        scenarioType: scenarioItem.type,
        sources: scenarioItem.sources,
      },
      position: { x: 0, y: 0 },
    });
    
    // Facts → Scenario edges (contributions)
    scenarioItem.contributingFacts?.forEach(factId => {
      edges.push({
        id: `edge_fact_${factId}_scenario_${idx}`,
        source: `fact_${factId}`,
        target: `scenario_${idx}`,
        type: "contributes",
        data: {
          label: "contributes to",
          strength: 0.7,
          confidence: scenario.confidence,
          contribution: 0.3, // Рассчитывается на основе важности факта
          sources: [],
        },
      });
    });
    
    // Trends → Scenario edges
    scenarioItem.contributingTrends?.forEach(trendId => {
      edges.push({
        id: `edge_trend_${trendId}_scenario_${idx}`,
        source: `trend_${trendId}`,
        target: `scenario_${idx}`,
        type: "contributes",
        data: {
          label: "contributes to",
          strength: 0.8,
          confidence: scenario.confidence,
          contribution: 0.4,
          sources: [],
        },
      });
    });
  });
  
  // 5. Cross-Domain Relations → edges
  scenario.crossDomainLinks.forEach(link => {
    const sourceNode = findNodeByEntity(link.sourceEntity, nodes);
    const targetNode = findNodeByEntity(link.targetEntity, nodes);
    
    if (sourceNode && targetNode) {
      edges.push({
        id: `edge_cross_${link.id}`,
        source: sourceNode.id,
        target: targetNode.id,
        type: link.relationType === "causes" ? "causes" : "influences",
        data: {
          label: link.relationType,
          strength: link.strength,
          confidence: link.confidence,
          lag: link.timeLag,
          domain: `${link.sourceDomain} → ${link.targetDomain}`,
          description: link.description,
          sources: link.sources,
        },
        animated: link.timeLag > 0, // Анимация для временных связей
      });
    }
  });
  
  // 6. Sources → nodes (опционально, если нужно показать источники отдельно)
  const uniqueSources = new Set<string>();
  nodes.forEach(node => {
    node.data.sources?.forEach(src => uniqueSources.add(src.id));
  });
  
  uniqueSources.forEach(sourceId => {
    nodes.push({
      id: `source_${sourceId}`,
      type: "source",
      data: {
        label: `Source ${sourceId}`,
        domain: "meta",
        confidence: 1.0,
        sources: [],
      },
      position: { x: 0, y: 0 },
    });
  });
  
  return {
    nodes,
    edges,
    metadata: {
      scenarioId: scenario.id,
      confidence: scenario.confidence,
      domains: [...new Set(nodes.map(n => n.data.domain))],
      totalEntities: scenario.knowledgeBase.entities.length,
      totalFacts: scenario.knowledgeBase.facts.length,
      totalTrends: scenario.knowledgeBase.trends.length,
    },
  };
}
```

### React Flow Components

**ScenarioGraphView.tsx:**

```typescript
import ReactFlow, {
  Node,
  Edge,
  Background,
  Controls,
  MiniMap,
  Panel,
} from 'reactflow';
import 'reactflow/dist/style.css';

interface ScenarioGraphViewProps {
  scenario: ScenarioAnalysis;
  interactionId: string;
}

export function ScenarioGraphView({ scenario, interactionId }: ScenarioGraphViewProps) {
  const graph = scenarioToGraph(scenario);
  
  // Custom node types
  const nodeTypes = {
    entity: EntityNode,
    fact: FactNode,
    trend: TrendNode,
    scenario: ScenarioNode,
    source: SourceNode,
    metric: MetricNode,
    risk: RiskNode,
  };
  
  // Custom edge types
  const edgeTypes = {
    influences: InfluenceEdge,
    contributes: ContributeEdge,
    causes: CauseEdge,
    evidences: EvidenceEdge,
    temporal: TemporalEdge,
  };
  
  return (
    <div style={{ width: '100%', height: '600px' }}>
      <ReactFlow
        nodes={graph.nodes}
        edges={graph.edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
      >
        <Background />
        <Controls />
        <MiniMap />
        <Panel position="top-left">
          <GraphLegend />
          <GraphStats metadata={graph.metadata} />
        </Panel>
      </ReactFlow>
    </div>
  );
}
```

### Custom Node Components

**EntityNode.tsx:**

```typescript
import { Handle, Position } from 'reactflow';

export function EntityNode({ data }: { data: NodeData }) {
  return (
    <div className="entity-node">
      <Handle type="target" position={Position.Top} />
      <div className="node-header">
        <span className="node-type-badge">{data.entityType}</span>
        <span className="domain-badge">{data.domain}</span>
      </div>
      <div className="node-label">{data.label}</div>
      <div className="node-footer">
        <span className="confidence">Confidence: {data.confidence}</span>
        <span className="sources-count">{data.sources.length} sources</span>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}
```

**FactNode.tsx:**

```typescript
export function FactNode({ data }: { data: NodeData }) {
  return (
    <div className="fact-node">
      <Handle type="target" position={Position.Top} />
      <div className="node-label">{data.label}</div>
      {data.perspectives && data.perspectives.length > 0 && (
        <div className="perspectives-indicator">
          {data.perspectives.length} perspectives
        </div>
      )}
      <div className="node-footer">
        <span className="confidence">{data.confidence}</span>
        <span className="sources-count">{data.sources.length} sources</span>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}
```

**ScenarioNode.tsx:**

```typescript
export function ScenarioNode({ data }: { data: NodeData }) {
  const scenarioColors = {
    baseline: '#3b82f6',
    optimistic: '#10b981',
    pessimistic: '#ef4444',
  };
  
  return (
    <div 
      className="scenario-node"
      style={{ borderColor: scenarioColors[data.scenarioType] }}
    >
      <Handle type="target" position={Position.Top} />
      <div className="node-header">
        <span className="scenario-type">{data.scenarioType}</span>
        <span className="likelihood">{Math.round(data.likelihood * 100)}%</span>
      </div>
      <div className="node-label">{data.label}</div>
      <div className="node-footer">
        <span className="confidence">{Math.round(data.confidence * 100)}%</span>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}
```

### Custom Edge Components

**ContributeEdge.tsx:**

```typescript
import { BaseEdge, EdgeProps, getBezierPath } from 'reactflow';

export function ContributeEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  data,
}: EdgeProps) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
  });
  
  return (
    <>
      <BaseEdge id={id} path={edgePath} />
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${targetX}px,${targetY}px)`,
          }}
          className="edge-label"
        >
          <div className="edge-label-text">{data.label}</div>
          {data.contribution && (
            <div className="edge-contribution">
              Contribution: {Math.round(data.contribution * 100)}%
            </div>
          )}
          {data.strength && (
            <div className="edge-strength">
              Strength: {Math.round(data.strength * 100)}%
            </div>
          )}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}
```

**TemporalEdge.tsx:**

```typescript
export function TemporalEdge({ ...props }: EdgeProps) {
  // Анимированная стрелка для временных связей
  return (
    <>
      <BaseEdge {...props} style={{ strokeDasharray: '5,5' }} />
      {props.data.lag && (
        <EdgeLabelRenderer>
          <div className="temporal-label">
            {props.data.lag} days lag
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
```

### Graph Layout Strategies

**1. Hierarchical Layout (для сценариев):**
```
Sources (top)
  ↓
Entities & Facts (middle)
  ↓
Trends (middle)
  ↓
Scenarios (bottom)
```

**2. Domain-Based Clustering:**
- Entities/Facts/Trends группируются по доменам
- Cross-domain edges соединяют кластеры
- Scenarios в центре, собирают все домены

**3. Force-Directed Layout:**
- Автоматическое позиционирование на основе связей
- Сильные связи притягивают узлы ближе
- Слабые связи дальше

**4. Timeline Layout (для временных данных):**
- Ось X = время
- Entities/Facts/Trends позиционируются по временным меткам
- Scenarios в будущем (правая часть)

### Interactive Features

**Node Interactions:**
- **Click:** Показать детали (sources, attributes, contributions)
- **Hover:** Tooltip с краткой информацией
- **Drag:** Перемещение узлов
- **Select:** Выделение узла и всех связанных edges

**Edge Interactions:**
- **Hover:** Показать метаданные связи (strength, confidence, lag, contribution)
- **Click:** Показать sources и описание связи
- **Filter:** Показать только связи определенного типа

**Filtering & Highlighting:**
- Фильтр по домену (показать только Financial/Political/etc.)
- Фильтр по типу узла (только Entities, только Trends)
- Фильтр по confidence (только высокие)
- Highlight path от Entity до Scenario (показать всю цепочку влияния)

**Contribution Tracing:**
- При клике на Scenario → подсветить все contributing nodes
- Показать contribution weights
- Показать path от конкретного Fact/Trend до Scenario

### Example Visualization

**Example Graph Structure:**

```
┌─────────────────────────────────────────────────────────────┐
│  Scenario Analysis Graph                                    │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  [Sources]                                                   │
│    📄 Financial Times  📄 Reuters  📄 Government Reports      │
│         │                 │              │                  │
│         └─────────────────┼──────────────┘                  │
│                           │                                  │
│  [Entities]                                                │
│    💼 GDP (Financial)      👤 President (Political)         │
│         │                       │                          │
│         │ influences            │ influences                │
│         ▼                       ▼                          │
│  [Facts]                                                     │
│    📊 GDP declined 5%    📊 Approval rating dropped 15%     │
│         │                       │                          │
│         │ contributes           │ contributes               │
│         │ (30%)                 │ (40%)                     │
│         └───────────┬───────────┘                          │
│                     │                                       │
│  [Trends]                                                   │
│    📈 GDP: declining   📈 Approval: decreasing              │
│         │                       │                          │
│         │ contributes           │ contributes               │
│         │ (35%)                 │ (45%)                     │
│         └───────────┬───────────┘                          │
│                     │                                       │
│         [Cross-Domain Link]                                 │
│         Financial → Political (30 days lag)                │
│                     │                                       │
│                     ▼                                       │
│  [Scenarios]                                                │
│    🎯 Baseline (70%)  🎯 Optimistic (20%)  🎯 Pessimistic (10%)│
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**Visual Features:**
- **Node Colors:** Domain-based (Financial=blue, Political=red, Social=green, Technical=purple)
- **Node Sizes:** Based on confidence or importance
- **Edge Thickness:** Based on strength/contribution
- **Edge Colors:** Type-based (contributes=green, causes=red, influences=blue)
- **Animated Edges:** Temporal relations with lag
- **Tooltips:** Hover to see details (sources, confidence, contribution %)

**Contribution Tracing Example:**

При клике на "Baseline Scenario":
```
Baseline Scenario (70%)
  ↑
  ├─ GDP declined 5% (contributes 30%)
  │    ↑ from: GDP entity
  │    ↑ sources: Financial Times, Reuters
  │
  ├─ Approval rating dropped 15% (contributes 40%)
  │    ↑ from: President entity
  │    ↑ sources: Government Reports
  │
  └─ Cross-Domain Link: Financial → Political (30 days)
       ↑ strength: 0.75
       ↑ confidence: 0.82
```

### Integration with ScenarioMessage

**ScenarioMessage.tsx:**

```typescript
export function ScenarioMessage({ scenario, interactionId }: Props) {
  const [viewMode, setViewMode] = useState<"summary" | "graph">("summary");
  
  return (
    <div className="scenario-message">
      <div className="scenario-header">
        <h3>Scenario Analysis</h3>
        <div className="view-toggle">
          <button 
            onClick={() => setViewMode("summary")}
            className={viewMode === "summary" ? "active" : ""}
          >
            Summary
          </button>
          <button 
            onClick={() => setViewMode("graph")}
            className={viewMode === "graph" ? "active" : ""}
          >
            Graph View
          </button>
        </div>
      </div>
      
      {viewMode === "summary" ? (
        <ScenarioSummaryView scenario={scenario} />
      ) : (
        <ScenarioGraphView scenario={scenario} interactionId={interactionId} />
      )}
    </div>
  );
}
```

**Graph Controls:**

```typescript
// GraphToolbar.tsx
export function GraphToolbar({ onFilter, onLayoutChange }: Props) {
  return (
    <div className="graph-toolbar">
      <select onChange={(e) => onFilter("domain", e.target.value)}>
        <option value="">All Domains</option>
        <option value="financial">Financial</option>
        <option value="political">Political</option>
        <option value="social">Social</option>
        <option value="technical">Technical</option>
      </select>
      
      <select onChange={(e) => onFilter("nodeType", e.target.value)}>
        <option value="">All Types</option>
        <option value="entity">Entities</option>
        <option value="fact">Facts</option>
        <option value="trend">Trends</option>
        <option value="scenario">Scenarios</option>
      </select>
      
      <select onChange={(e) => onLayoutChange(e.target.value)}>
        <option value="hierarchical">Hierarchical</option>
        <option value="force">Force-Directed</option>
        <option value="domain-cluster">Domain Clusters</option>
        <option value="timeline">Timeline</option>
      </select>
      
      <button onClick={() => onFilter("confidence", "high")}>
        High Confidence Only
      </button>
    </div>
  );
}
```

## Data Storage

### Firestore Collections

**Global (tenantId = ""):**
- `knowledge_entities/`
- `knowledge_facts/` (with domain-specific subcollections)
- `knowledge_events/`
- `knowledge_metrics/`
- `knowledge_relations/`
- `cross_domain_relations/`
- `trends/`
- `cross_domain_trends/`

**Tenant-Specific:**
- `core_interactions/` (with subkind "scenario.analysis")
- `core_projects/`
- `core_docs/` (private documents)

### Indexes Required

```json
{
  "collectionGroup": "knowledge_entities",
  "fields": [
    {"fieldPath": "canonicalName", "order": "ASCENDING"},
    {"fieldPath": "type", "order": "ASCENDING"},
    {"fieldPath": "relevance", "order": "DESCENDING"}
  ]
},
{
  "collectionGroup": "knowledge_facts",
  "fields": [
    {"fieldPath": "subjectEntity", "order": "ASCENDING"},
    {"fieldPath": "validFrom", "order": "DESCENDING"},
    {"fieldPath": "validUntil", "order": "ASCENDING"}
  ]
},
{
  "collectionGroup": "trends",
  "fields": [
    {"fieldPath": "domain", "order": "ASCENDING"},
    {"fieldPath": "horizon", "order": "ASCENDING"},
    {"fieldPath": "detectedAt", "order": "DESCENDING"}
  ]
}
```

## Implementation Plan (Fractal Approach)

**Принцип:** Каждая итерация - полностью рабочая система. Последующие итерации добавляют функции и улучшают качество, но не ломают существующее.

## Stub Tracking & Elimination

**Критически важно:** Заглушки (stubs) должны быть явно отслеживаемы и заменяемы. Забытые заглушки - одна из главных проблем проектов.

### Правила работы с заглушками

1. **Все заглушки документируются:**
   - Список в разделе "Заглушки, созданные в этой итерации"
   - Указание в какой итерации будет заменена
   - Комментарий в коде: `// STUB: Replace in Iteration N`

2. **Каждая итерация заменяет заглушки предыдущей:**
   - **ПЕРВАЯ задача в каждой итерации:** "ЗАМЕНИТЬ ЗАГЛУШКИ из Iteration N-1"
   - Все заглушки из предыдущей итерации ДОЛЖНЫ быть заменены
   - **ОБЯЗАТЕЛЬНАЯ проверка в критериях готовности:** "ВСЕ заглушки из Iteration N-1 заменены"
   - Если заглушка не заменена → итерация НЕ завершена

3. **Поиск заглушек в коде:**
   ```bash
   # Поиск всех заглушек в Go коде
   grep -r "STUB\|not_implemented\|TODO.*stub\|FIXME.*stub" backend/
   
   # Поиск в TypeScript
   grep -r "STUB\|not_implemented\|TODO.*stub\|FIXME.*stub" web/
   ```

4. **Обязательные комментарии:**
   ```go
   // STUB: Replace in Iteration 1
   // Issue: #123
   func (r *KnowledgeRepository) SaveEntity(ctx context.Context, entity *KnowledgeEntity) error {
       return errors.New("not implemented")
   }
   ```

5. **Тесты на заглушки:**
   - Unit тесты должны проверять что заглушки возвращают корректный статус
   - Integration тесты должны fail если заглушка не заменена в нужной итерации

### Stub Registry (по итерациям)

**ВАЖНО:** Эта таблица должна обновляться после каждой итерации. Статус меняется с ⚠️ на ✅ когда заглушка заменена.

| Iteration | Stub Location | Replace In | Status | Notes |
|-----------|---------------|------------|--------|-------|
| 0 | `POST /api/v1/chat/knowledge` | Iteration 1 | ⚠️ | HTTP endpoint |
| 0 | `POST /api/v1/chat/knowledge/scenario` | Iteration 9 | ⚠️ | HTTP endpoint |
| 0 | `KnowledgeRepository.SaveEntity()` | Iteration 1 | ⚠️ | Repository method |
| 0 | `KnowledgeRepository.SaveFact()` | Iteration 1 | ⚠️ | Repository method |
| 0 | `KnowledgeRepository.QueryEntities()` | Iteration 1 | ⚠️ | Repository method |
| 0 | `BaseAgent.ExtractKnowledge()` | Iteration 1 | ⚠️ | Base agent method |
| 0 | `BaseAgent.HandleQuery()` | Iteration 1 | ⚠️ | Base agent method |
| 0 | `KnowledgeRouter.Route()` | Iteration 1 | ⚠️ | Router method |
| 1 | Semantic similarity (Entity Resolution) | Iteration 3 | ⚠️ | Feature stub |
| 1 | Специализированные промпты | Iteration 2 | ⚠️ | Feature stub |
| 1 | Multi-perspective extraction | Iteration 2 | ⚠️ | Feature stub |
| 1 | Relations extraction | Iteration 2 | ⚠️ | Feature stub |
| 2 | Semantic similarity | Iteration 3 | ⚠️ | Inherited from Iteration 1 |
| 3 | LLM-based trend detection | Iteration 7 | ⚠️ | Feature stub |
| 9 | Agent Orchestration (enrichment) | Iteration 10 | ⚠️ | Feature stub |
| 9 | WebSocket progress updates | Iteration 10 | ⚠️ | Feature stub |
| 9 | Enrichment logic | Iteration 10 | ⚠️ | Feature stub |

**Примеры замененных заглушек:**
| Iteration | Stub Location | Replaced In | Status |
|-----------|---------------|-------------|--------|
| 2 | Multi-perspective extraction | Iteration 2 | ✅ | Заменена в той же итерации |

**Легенда:**
- ⚠️ Заглушка активна (еще не заменена)
- ✅ Заглушка заменена (закрыта)
- 🔴 Заглушка просрочена (должна была быть заменена)

### Automation для tracking

**Pre-commit hook:**
```bash
#!/bin/bash
# Проверка что все заглушки из предыдущей итерации заменены
stubs_from_prev=$(grep -r "STUB.*Iteration $PREV_ITER" backend/ | wc -l)
if [ $stubs_from_prev -gt 0 ]; then
    echo "ERROR: Found $stubs_from_prev stubs from Iteration $PREV_ITER that should be replaced!"
    exit 1
fi
```

**CI/CD check:**
```yaml
- name: Check Stubs
  run: |
    echo "Checking for overdue stubs..."
    current_iter=$(get_current_iteration)
    overdue=$(grep -r "STUB.*Iteration [0-9]" backend/ | grep -v "Iteration $current_iter" | wc -l)
    if [ $overdue -gt 0 ]; then
      echo "Found overdue stubs!"
      exit 1
    fi
```

### ⚠️ КРИТИЧЕСКИ ВАЖНО: Забытые заглушки

**Проблема:** Заглушки часто остаются в коде навсегда, создавая технический долг.

**Почему это происходит:**
1. Заглушка работает "достаточно хорошо" для текущих нужд
2. Нет явного tracking заглушек
3. Команда забывает про заглушки между итерациями
4. Нет автоматической проверки

**Последствия:**
- Технический долг растет
- Система работает неоптимально
- Сложно поддерживать код
- Баги из-за незавершенной логики

**Решение (реализовано в этом плане):**
1. ✅ Явный список заглушек в каждой итерации
2. ✅ Обязательная задача "ЗАМЕНИТЬ ЗАГЛУШКИ" в начале каждой итерации
3. ✅ Проверка в критериях готовности
4. ✅ Stub Registry для tracking
5. ✅ Автоматические проверки (pre-commit, CI/CD)
6. ✅ Комментарии в коде с меткой `STUB: Replace in Iteration N`

**Правило:** Если заглушка не заменена в нужной итерации → итерация НЕ завершена. ❌

### Iteration 0: Foundation (Week 1) - Минимально работающая архитектура

**Цель:** Создать полную структуру данных, интерфейсы и минимальные работающие заглушки. Система компилируется, деплоится и отвечает на запросы.

**Задачи:**
- ✅ Определить все структуры данных (Go types)
  - KnowledgeEntity (полная структура)
  - FinancialKnowledge, PoliticalKnowledge, SocialKnowledge, TechnicalKnowledge
  - KnowledgeRelation, CrossDomainRelation
  - Trend (полная структура со всеми полями)
  - ScenarioAnalysis (полная структура)
- ✅ Определить DomainAgent interface (полный контракт)
- ✅ **Создать иерархию интерфейсов для унификации всех агентов**
  - ✅ Создать `BaseAgent` - базовый интерфейс для всех агентов
  - ✅ Переименовать текущий `Agent` → `TaskAgent` (расширяет `BaseAgent`)
  - ✅ Создать универсальный `Agent` интерфейс (объединяет все типы)
  - ✅ `DomainAgent` расширяет `BaseAgent` через встраивание
  - ✅ Создать `TaskAgentAdapter` и `DomainAgentAdapter` для унификации
  - ✅ Обновить Registry для использования универсального `Agent` интерфейса
  - ✅ Обеспечить обратную совместимость (существующие агенты не меняются)
- ✅ Создать BaseAgent (базовая реализация с заглушками)
- ✅ Определить KnowledgeRouter interface (с default implementation)
- ✅ Создать Firestore коллекции (пустые, но со схемами)
- ✅ Создать все репозитории (с методами-заглушками, возвращающими "not implemented")
- ✅ Определить API endpoints и реализовать заглушки:
  - `POST /api/v1/chat/knowledge` → `{"status": "not_implemented", "message": "Coming in Iteration 1"}`
  - `POST /api/v1/chat/knowledge/scenario` → `{"status": "not_implemented", "message": "Coming in Iteration 9"}`
- ✅ UI показывает "scenario" mode (disabled) с tooltip "Coming soon"

**Результат:** Полная архитектура готова. **Система работает минимально:**
- HTTP endpoints отвечают `200 OK` + JSON `{"status": "not_implemented"}`
- Firestore коллекции созданы (пустые, но со схемами)
- Все интерфейсы определены с default implementations
- UI показывает новые режимы (disabled) с "Coming soon"
- **Можно деплоить без ошибок** (пользователь не видит 500-х ошибок)
- Ничего не сломается при добавлении функциональности

**Заглушки, созданные в этой итерации (должны быть заменены):**
- ⚠️ `POST /api/v1/chat/knowledge` → заменить в Iteration 1
- ⚠️ `POST /api/v1/chat/knowledge/scenario` → заменить в Iteration 9
- ⚠️ `KnowledgeRepository` методы → заменить в Iteration 1
- ⚠️ `BaseAgent.ExtractKnowledge()` → заменить в Iteration 1
- ⚠️ `BaseAgent.HandleQuery()` → заменить в Iteration 1
- ⚠️ `KnowledgeRouter.Route()` → заменить в Iteration 1

**Критерии готовности:**
- ✅ Компилируется без ошибок
- ✅ Все endpoints отвечают (пусть заглушкой `not_implemented`)
- ✅ Все структуры данных готовы
- ✅ Можно деплоить в production (graceful degradation)
- ✅ UI показывает новые режимы (disabled)
- ✅ **Все заглушки документированы и помечены для замены**

---

### Iteration 1: Core Extraction (Week 2-3) - Минимально рабочая система

**Цель:** Система работает, извлекает знания, сохраняет в БД. Можно использовать из чата.

**Задачи:**
- ✅ **ЗАМЕНИТЬ ЗАГЛУШКИ из Iteration 0:**
  - ✅ `POST /api/v1/chat/knowledge` → реальная реализация
  - ✅ `KnowledgeRepository` методы → реальная реализация (SaveEntity, SaveFact, QueryEntities)
  - ✅ `BaseAgent.ExtractKnowledge()` → реальная реализация
  - ✅ `BaseAgent.HandleQuery()` → реальная реализация
  - ✅ `KnowledgeRouter.Route()` → реальная реализация
- ✅ Реализовать простой KnowledgeExtractor
  - Извлечение Entities (по имени, типу)
  - Извлечение Facts (базовые утверждения)
  - LLM extraction с простым промптом
- ✅ Реализовать базовый Entity Resolution
  - Точное совпадение имен
  - Type-based filtering
  - Нет semantic similarity (пока)
- ✅ Реализовать 2 агента (Financial, Political)
  - Базовая реализация DomainAgent interface
  - ExtractKnowledge работает
  - HandleQuery работает (простой поиск)
- ✅ Реализовать KnowledgeRouter
  - Intent classification (по ключевым словам)
  - Маршрутизация к агентам
  - Простой unified response
- ✅ Интеграция с чатом
  - HTTP endpoint `/api/v1/chat/knowledge`
  - Возвращает текстовый ответ + структурированные данные
- ✅ Firestore persistence
  - Сохранение Entities
  - Сохранение Facts
  - Базовые индексы

**Качество:**
- Entity Resolution: точное совпадение имен (accuracy ~70%)
- Extraction: базовый промпт, confidence ~0.6-0.7
- Response time: <3s для запросов

**Результат:** Полностью рабочая система. Можно задавать вопросы в чате, получать ответы на основе извлеченных знаний. Ограничения есть, но система работает.

**Заглушки, созданные в этой итерации (должны быть заменены):**
- ⚠️ Semantic similarity в Entity Resolution → заменить в Iteration 3
- ⚠️ Специализированные промпты для доменов → заменить в Iteration 2
- ⚠️ Multi-perspective extraction → заменить в Iteration 2
- ⚠️ Relations extraction → заменить в Iteration 2

**Критерии готовности:**
- ✅ **ВСЕ заглушки из Iteration 0 заменены на реальную реализацию**
- ✅ Можно задать вопрос в чате → получить ответ
- ✅ Знания извлекаются из документов → сохраняются
- ✅ Entity Resolution работает (базовый уровень)
- ✅ Все endpoints из Iteration 0 работают (не заглушки)

---

### Iteration 2: Quality Improvement (Week 4-5) - Улучшение качества

**Цель:** Система работает лучше. Более точное извлечение, лучший Entity Resolution.

**Задачи:**
- ✅ **ЗАМЕНИТЬ ЗАГЛУШКИ из Iteration 1:**
  - ✅ Специализированные промпты для доменов → реальная реализация
  - ✅ Multi-perspective extraction → реальная реализация
  - ✅ Relations extraction → реальная реализация
- ✅ Улучшить Entity Resolution
  - Добавить fuzzy matching (Levenshtein)
  - Добавить normalized matching (lowercase, punctuation)
  - Attribute-based matching (для person: birthDate, nationality)
  - Accuracy: ~85%
- ✅ Улучшить Extraction
  - Специализированные промпты для каждого домена
  - Multi-perspective extraction для Political
  - Confidence calibration
  - Average confidence: ~0.75
- ✅ Добавить Relations
  - Извлечение Relations между Entities
  - Сохранение в knowledge_relations
  - Базовый граф работает
- ✅ Добавить базовый Cross-Domain Linking
  - Простое обнаружение связей (co-occurrence)
  - Сохранение cross_domain_relations
  - Accuracy: ~60% (базовый уровень)

**Качество:**
- Entity Resolution: accuracy ~85%
- Extraction: confidence ~0.75
- Cross-domain: базовые связи работают

**Результат:** Та же система, но работает лучше. Более точные ответы, меньше дубликатов сущностей.

**Заглушки, созданные в этой итерации (должны быть заменены):**
- ⚠️ Semantic similarity в Entity Resolution → заменить в Iteration 3

**Критерии готовности:**
- ✅ **ВСЕ заглушки из Iteration 1 заменены на реальную реализацию**
- ✅ Entity Resolution accuracy >80%
- ✅ Средний confidence >0.7
- ✅ Cross-domain связи обнаруживаются

---

### Iteration 3: Semantic Enhancement (Week 6-7) - Семантическое улучшение

**Цель:** Семантический поиск, улучшенный Entity Resolution, embeddings.

**Задачи:**
- ✅ **ЗАМЕНИТЬ ЗАГЛУШКУ из Iteration 1:**
  - ✅ Semantic similarity в Entity Resolution → реальная реализация
- ✅ Добавить Embeddings
  - Embedding для всех Entities
  - Embedding для Facts
  - Векторный поиск работает
- ✅ Semantic Entity Resolution
  - Cosine similarity для мержа
  - Threshold: 0.9 (conservative)
  - Accuracy: ~90%
- ✅ Semantic Search
  - Query embedding
  - Vector search в Firestore
  - Более релевантные результаты
- ✅ Knowledge Enrichment (базовый)
  - Cross-reference с существующими фактами
  - Автоматическое обогащение атрибутов
  - Confidence улучшается со временем

**Качество:**
- Entity Resolution: accuracy ~90%
- Search relevance: улучшение на 30-40%
- Enrichment: автоматическое улучшение качества

**Результат:** Система работает еще лучше. Семантический поиск, меньше false positives в Entity Resolution.

**Критерии готовности:**
- ✅ **Заглушка "Semantic similarity" из Iteration 1 заменена на реальную реализацию**
- ✅ Semantic search работает
- ✅ Entity Resolution accuracy >88%
- ✅ Enrichment автоматически улучшает данные

---

### Iteration 4: Temporal Knowledge (Week 8-9) - Временные знания

**Цель:** Поддержка временных запросов, версионность фактов.

**Задачи:**
- ✅ Temporal Validity
  - ValidFrom/ValidUntil для всех Facts
  - Temporal queries ("knowledge at time T")
  - Производительность: <2s для запросов
- ✅ Versioning
  - Version tracking для Facts
  - Replaces/ReplacedBy поля
  - История изменений доступна
- ✅ Temporal Relations
  - Временная валидность Relations
  - Query relations at time T
- ✅ Snapshots (оптимизация)
  - Периодические снэпшоты для популярных Entities
  - Ускорение temporal queries

**Качество:**
- Temporal queries: <2s
- Version accuracy: 100%
- Snapshots: 10x ускорение для частых запросов

**Результат:** Система поддерживает временные запросы. Можно спрашивать "что было известно на дату X".

**Критерии готовности:**
- Temporal queries работают
- Versioning работает корректно
- Производительность приемлемая

---

### Iteration 5: All Domains (Week 10-11) - Все домены

**Цель:** Добавить Social и Technical агенты. Система работает со всеми доменами.

**Задачи:**
- ✅ SocialAgent
  - Реализация DomainAgent interface
  - Cultural context extraction
  - Social groups identification
  - Public sentiment analysis
- ✅ TechnicalAgent
  - Реализация DomainAgent interface
  - Specifications extraction
  - Capabilities detection
  - Innovations tracking
- ✅ Улучшить Cross-Domain
  - Все 4 домена работают вместе
  - Лучшее обнаружение связей между доменами
  - Cross-domain queries работают

**Качество:**
- Все 4 агента работают
- Cross-domain coverage: все комбинации
- Unified view поддерживает все домены

**Результат:** Полная мульти-доменная система. Можно анализировать финансовые, политические, социальные и технические данные вместе.

**Критерии готовности:**
- Все 4 агента работают
- Cross-domain queries работают
- Unified view корректно отображает все домены

---

### Iteration 6: Trend Detection - Short Term (Week 12-13) - Детекция трендов (короткие горизонты)

**Цель:** Детекция трендов на коротких горизонтах. Система видит паттерны.

**Задачи:**
- ✅ Trend Detection (Immediate, Short-Term)
  - Статистические методы (linear regression)
  - Детекция для количественных данных
  - Сохранение в trends коллекцию
- ✅ Trend API
  - GET /api/v1/knowledge/trends
  - Фильтрация по домену, горизонту
  - Response time: <1s
- ✅ Trend Visualization (базовая)
  - Direction (up/down/sideways)
  - Strength indicator
  - Basic chart data

**Качество:**
- Detection accuracy: ~75% для количественных данных
- False positive rate: <15%
- Coverage: Immediate + Short-Term горизонты

**Результат:** Система детектирует краткосрочные тренды. Можно видеть, что растет/падает.

**Критерии готовности:**
- Trend detection работает для Immediate + Short-Term
- API возвращает корректные тренды
- Accuracy >70%

---

### Iteration 7: Trend Detection - All Horizons (Week 14-15) - Все горизонты

**Цель:** Детекция трендов на всех временных горизонтах. Иерархия трендов.

**Задачи:**
- ✅ Trend Detection (Medium, Long, Strategic)
  - Все горизонты работают
  - LLM-based для качественных данных
  - Statistical + Semantic комбинация
- ✅ Trend Hierarchy
  - Parent/Child связи между трендами
  - Build hierarchy автоматически
  - Visualization hierarchy
- ✅ Trend Forecasts
  - Базовые прогнозы (extrapolation)
  - Confidence intervals
  - Accuracy: ~60-70% для ближайшего будущего

**Качество:**
- Detection accuracy: ~75% (все горизонты)
- Hierarchy accuracy: ~80%
- Forecast accuracy: ~65% для 1-3 месяцев

**Результат:** Полная система детекции трендов. Видно краткосрочные и долгосрочные паттерны.

**Критерии готовности:**
- Все горизонты работают
- Hierarchy строится корректно
- Forecasts имеют разумную точность

---

### Iteration 8: Cross-Domain Trends (Week 16-17) - Кросс-доменные тренды

**Цель:** Обнаружение паттернов между доменами. Cascade effects.

**Задачи:**
- ✅ Cross-Domain Trend Detection
  - Correlation analysis между доменами
  - Temporal lag detection
  - Pattern recognition (cascade, correlation, divergence)
- ✅ Cross-Domain API
  - GET /api/v1/knowledge/trends/cross-domain
  - Фильтрация по паттернам
  - Time lag visualization
- ✅ Leading Domain Detection
  - Определение, какой домен "ведет"
  - Cascade visualization
  - Prediction на основе leading domain

**Качество:**
- Pattern detection accuracy: ~70%
- Time lag accuracy: ±3 дня
- Cascade detection: ~75% accuracy

**Результат:** Система видит, как тренды в одном домене влияют на другие. Cascade effects видны.

**Критерии готовности:**
- Cross-domain trends детектируются
- Time lags определяются корректно
- Cascade patterns видны

---

### Iteration 9: Scenario Generation - Quick Scan (Week 18-19) - Быстрый скрининг

**Цель:** Быстрая генерация предварительных сценариев на основе существующих знаний.

**Задачи:**
- ✅ **ЗАМЕНИТЬ ЗАГЛУШКУ из Iteration 0:**
  - ✅ `POST /api/v1/chat/knowledge/scenario` → реальная реализация Quick Scan
- ✅ QuickAnalyzer
  - Query analysis (LLM, fast model)
  - Knowledge lookup (Firestore)
  - Quick scenario generation (LLM, fast)
  - Task planning для агентов
- ✅ Quick Scan API
  - POST /api/v1/chat/knowledge/scenario
  - Response time: <1 минута
  - Возвращает preliminary Interaction
- ✅ UI для preliminary
  - Отображение quick scan результатов
  - Индикатор обогащения
  - Status: "preliminary"

**Качество:**
- Response time: <1 минута
- Preliminary confidence: ~0.55
- User satisfaction: быстрый ответ, есть что показать

**Результат:** Пользователь быстро получает предварительный анализ. Система работает, но данные ограничены.

**Заглушки, созданные в этой итерации (должны быть заменены):**
- ⚠️ Agent Orchestration (enrichment) → заменить в Iteration 10
- ⚠️ WebSocket progress updates → заменить в Iteration 10
- ⚠️ Enrichment logic → заменить в Iteration 10

**Критерии готовности:**
- ✅ **Заглушка `POST /api/v1/chat/knowledge/scenario` из Iteration 0 заменена на реальную реализацию**
- ✅ Quick scan работает <1 минуты
- ✅ Preliminary scenarios генерируются
- ✅ UI отображает preliminary состояние

---

### Iteration 10: Agent Orchestration & Enrichment (Week 20-21) - Обогащение через агентов

**Цель:** Асинхронная оркестрация агентов для обогащения предварительных сценариев.

**Задачи:**
- ✅ **ЗАМЕНИТЬ ЗАГЛУШКИ из Iteration 9:**
  - ✅ Agent Orchestration (enrichment) → реальная реализация
  - ✅ WebSocket progress updates → реальная реализация
  - ✅ Enrichment logic → реальная реализация
- ✅ ScenarioOrchestrator
  - Запуск агентов параллельно
  - Прогресс tracking через WebSocket
  - Агрегация результатов агентов
  - Обогащение сценариев новыми данными
- ✅ WebSocket Integration
  - EventScenarioOrchestrationStarted
  - EventScenarioAgentProgress
  - EventScenarioOrchestrationCompleted
- ✅ Enrichment Logic
  - Объединение quick scan + agent results
  - Пересчет сценариев с новыми данными
  - Улучшение confidence
  - Update существующего Interaction
- ✅ UI для enriched
  - Отображение обогащенных результатов
  - Прогресс агентов в реальном времени
  - Comparison preliminary vs enriched

**Качество:**
- Enrichment time: 2-10 минут (зависит от задач)
- Final confidence: ~0.8+
- Enrichment impact: +30-50% confidence improvement

**Результат:** Система обогащает предварительные сценарии данными от агентов. Пользователь видит прогресс и получает улучшенный анализ.

**Критерии готовности:**
- ✅ **ВСЕ заглушки из Iteration 9 заменены на реальную реализацию**
- ✅ Агенты запускаются параллельно
- ✅ Прогресс отслеживается через WebSocket
- ✅ Enrichment улучшает confidence
- ✅ UI показывает обогащение

---

### Iteration 11: Advanced Scenarios (Week 22-23) - Продвинутые сценарии

**Цель:** Улучшенные сценарии с рисками, альтернативами, рекомендациями.

**Задачи:**
- ✅ Advanced Scenario Features
  - Risk assessment
  - Alternative scenarios
  - Recommendations
  - Uncertainty analysis
- ✅ Multi-Perspective Scenarios
  - Учет разных точек зрения в сценариях
  - Bias mitigation в сценариях
  - Balance scoring
- ✅ Scenario Validation
  - Consistency checks
  - Cross-domain validation
  - Temporal alignment
- ✅ React Flow Graph Visualization
  - scenarioToGraph transformation function
  - Custom node components (Entity, Fact, Trend, Scenario, Source)
  - Custom edge components (Contributes, Causes, Influences, Temporal)
  - Graph layout strategies (hierarchical, force-directed, domain-cluster, timeline)
  - Interactive features (filtering, highlighting, contribution tracing)
  - Graph controls toolbar
  - Integration with ScenarioMessage (toggle summary/graph view)

**Качество:**
- Scenario completeness: ~85%
- Risk assessment accuracy: ~70%
- User satisfaction: улучшение на 60%

**Результат:** Полноценная система генерации сценариев. Комплексный анализ с рисками и рекомендациями.

**Критерии готовности:**
- Advanced features работают
- Качество приемлемое
- User satisfaction улучшился

---

### Iteration 12: Performance & Scale (Week 24-25) - Производительность

**Цель:** Оптимизация производительности. Система готова к масштабированию.

**Задачи:**
- ✅ Caching Layer
  - Extraction cache (по content hash)
  - Query cache (по query hash)
  - Entity cache (LRU)
  - Cache hit rate: >60%
- ✅ Batch Processing
  - Batch extraction (10-20 docs)
  - Batch entity resolution
  - Cost reduction: 30-40%
- ✅ Performance Optimization
  - Query optimization
  - Index optimization
  - Denormalization где нужно
  - Response time: <1s для 90% запросов
- ✅ Monitoring
  - Quality metrics
  - Performance metrics
  - Cost metrics
  - Alerts setup

**Качество:**
- Response time: <1s (90th percentile)
- Cache hit rate: >60%
- Cost reduction: 30-40%

**Результат:** Оптимизированная система. Готова к production нагрузке.

**Критерии готовности:**
- Performance targets достигнуты
- Monitoring работает
- Cost под контролем

---

### Iteration 13: Polish & Enhancement (Week 26+) - Доводка и улучшения

**Цель:** Доводка качества, дополнительные улучшения.

**Задачи:**
- ✅ Quality Improvements
  - Confidence calibration
  - Better entity resolution (accuracy >95%)
  - Better extraction prompts
- ✅ Additional Features
  - Knowledge export
  - Advanced visualizations
  - Expert verification (опционально)
- ✅ Documentation
  - API documentation
  - User guides
  - Architecture docs

**Качество:**
- Entity Resolution: accuracy >95%
- Overall quality: улучшение на 20-30%
- User satisfaction: высокий

**Результат:** Production-ready система высокого качества.

**Критерии готовности:**
- Все метрики качества достигнуты
- Documentation полная
- Система стабильна

---

## Fractal Principles

### Принцип 1: Работоспособность на каждом этапе
- ✅ Каждая итерация - полностью рабочая система
- ✅ Можно использовать в production после каждой итерации
- ✅ Даже Iteration 0 работает (с заглушками "not implemented")
- ✅ Нет состояний "наполовину готово" или "сломано"

### Принцип 2: Улучшение качества
- ✅ Каждая итерация улучшает качество предыдущей
- ✅ Метрики улучшаются постепенно (Entity Resolution: 70% → 85% → 90% → 95%)
- ✅ Обратная совместимость сохраняется ВСЕГДА

### Принцип 3: Базовые сущности сразу
- ✅ Все структуры данных определены в Iteration 0
- ✅ Все интерфейсы определены сразу
- ✅ Изменения только добавляют, не меняют структуру
- ✅ Firestore схемы готовы до первого использования

### Принцип 4: Постепенное усложнение
- ✅ Начинаем с простого (2 агента, точное совпадение имен)
- ✅ Добавляем сложность постепенно (semantic search, temporal queries)
- ✅ Каждый шаг понятен и обоснован
- ✅ Сложность растет естественно, не скачками

### Принцип 5: Измеримость прогресса
- ✅ Каждая итерация имеет метрики качества
- ✅ Критерии готовности четко определены
- ✅ Можно оценить прогресс в любой момент
- ✅ Regression testing между итерациями

### Принцип 6: Обратная совместимость (NEW)
- ✅ Каждая итерация сохраняет функциональность предыдущих
- ✅ Все данные совместимы (автоматическая миграция, если нужна)
- ✅ UI предыдущих режимов работает без изменений
- ✅ Performance не деградирует (±5% допустимо)
- ✅ Все тесты предыдущих итераций продолжают проходить

## Backward Compatibility Strategy

### Гарантии обратной совместимости

**Каждая новая итерация обязана:**

1. **API Compatibility:**
   - Все endpoints предыдущих итераций работают
   - Старые запросы получают корректные ответы
   - Новые поля добавляются опционально (optional fields)
   - Deprecated endpoints работают минимум 2 итерации

2. **Data Compatibility:**
   - Все данные совместимы со старыми структурами
   - Автоматическая миграция при чтении (lazy migration)
   - Нет breaking changes в Firestore схемах
   - Версионность данных (version field) для критичных изменений

3. **UI Compatibility:**
   - Старые режимы (text, graph) продолжают работать
   - Новые режимы (scenario) добавляются опционально
   - Graceful degradation если backend не поддерживает новую фичу
   - Feature flags для плавного роллаута

4. **Performance Compatibility:**
   - Performance не деградирует (±5% допустимо)
   - Новые фичи не замедляют старые
   - Caching для сохранения скорости
   - Индексы оптимизированы для всех query patterns

### Regression Testing Requirements

**Перед завершением каждой итерации:**

```
✅ Run all tests from previous iterations
✅ Verify all metrics from previous iterations (не хуже ±5%)
✅ Test all API endpoints from previous iterations
✅ Verify UI previous modes still work
✅ Load test с workload предыдущих итераций
✅ Data migration test (если были изменения схемы)
```

**Regression Test Suite:**

| Iteration | New Features Test | Regression Test | Status |
|-----------|-------------------|-----------------|--------|
| 0 | Architecture compiles, endpoints respond | N/A (first) | ✅ |
| 1 | Extraction works, 2 agents work | Iteration 0 endpoints still work | ✅ |
| 2 | Quality improved (80%+ accuracy) | Iterations 0-1 still work | ✅ |
| 3 | Semantic search works | Iterations 0-2 still work | ✅ |
| 4 | Temporal queries work | Iterations 0-3 still work | ✅ |
| 5 | All 4 agents work | Iterations 0-4 still work | ✅ |
| 6 | Short trends detected | Iterations 0-5 still work | ✅ |
| 7 | All horizons work | Iterations 0-6 still work | ✅ |
| 8 | Cross-domain trends work | Iterations 0-7 still work | ✅ |
| 9 | Quick scan works (<1 min) | Iterations 0-8 still work | ✅ |
| 10 | Enrichment works | Iterations 0-9 still work | ✅ |
| 11 | Advanced features + graph | Iterations 0-10 still work | ✅ |
| 12 | Performance improved | Iterations 0-11 still work, faster | ✅ |
| 13 | Quality polished (95%+) | Iterations 0-12 still work | ✅ |

## Rollback Strategy

### Принцип

**Если итерация N не готова или сломана, можно откатиться к итерации N-1 без потери данных.**

### Механизмы

1. **Feature Flags:**
```go
type FeatureFlags struct {
    SemanticSearch      bool  // Iteration 3
    TemporalQueries     bool  // Iteration 4
    SocialAgent         bool  // Iteration 5
    TechnicalAgent      bool  // Iteration 5
    TrendDetection      bool  // Iteration 6-8
    QuickScan           bool  // Iteration 9
    AgentOrchestration  bool  // Iteration 10
    GraphVisualization  bool  // Iteration 11
}
```

2. **Graceful Degradation:**
   - Если фича недоступна → fallback к предыдущей итерации
   - Пример: Если AgentOrchestration не работает → используем только QuickScan
   - Пользователь не видит ошибок, просто не получает enrichment

3. **Data Migration:**
   - Все миграции обратимы
   - Lazy migration (при чтении)
   - Не удаляем старые поля сразу (deprecated, но работают)
   - Version field для отслеживания

4. **Deployment Strategy:**
   - Canary deployment (5% → 25% → 50% → 100%)
   - Monitoring metrics на каждом этапе
   - Auto-rollback если metrics деградируют >10%
   - Manual rollback всегда доступен

### Rollback Triggers

**Автоматический rollback если:**
- Error rate >5% (для любого endpoint)
- Response time деградация >20%
- Ключевая метрика (accuracy, confidence) упала >10%
- Memory leak detected
- Database errors >1%

**Manual rollback если:**
- Обнаружен критичный баг
- User reports значительный
- Business decision
- Cost spike >30%

### Rollback Procedure

```bash
# 1. Disable feature flag
curl -X POST /api/v1/admin/features \
  -d '{"agentOrchestration": false}'

# 2. Verify rollback
curl /api/v1/chat/knowledge/scenario
# Should fallback to quick scan only

# 3. Monitor metrics
# - Response time should return to baseline
# - Error rate should drop to <1%
# - User satisfaction should recover

# 4. Investigate issue
# Fix in dev/staging before re-enabling
```

### Example: Iteration 10 Rollback

**Scenario:** Agent Orchestration (Iteration 10) не работает стабильно.

**Action:**
1. Disable `AgentOrchestration` feature flag
2. Система откатывается к Iteration 9 (Quick Scan only)
3. Пользователь продолжает получать preliminary ответы (<1 минута)
4. Нет ошибок, просто нет enrichment
5. В UI показывается: "Enrichment temporarily unavailable"

**Result:**
- ✅ Система работает (Iteration 9)
- ✅ Пользователь получает ценность (quick scan)
- ✅ Нет ошибок или downtime
- ✅ Можно спокойно чинить Iteration 10

**Lesson:** Фрактальный подход позволяет откатываться к любой предыдущей итерации без потери функциональности.

## Risk Mitigation per Iteration

**Iteration 1-3:** Риск низкого качества → быстрое улучшение в следующих итерациях  
**Iteration 4-5:** Риск сложности → поэтапное добавление  
**Iteration 6-8:** Риск производительности → оптимизация в Iteration 11  
**Iteration 9-10:** Риск качества сценариев → итеративное улучшение  
**Iteration 11-12:** Риск масштабирования → мониторинг и оптимизация

## Success Metrics per Iteration

| Iteration | Key Metric | Target | Measurement |
|-----------|------------|--------|-------------|
| 1 | System Works | ✅ | Can answer queries |
| 2 | Entity Resolution | >80% | Accuracy test |
| 3 | Semantic Search | >70% relevance | User feedback |
| 4 | Temporal Queries | <2s | Performance test |
| 5 | All Domains | ✅ | 4 agents work |
| 6 | Short Trends | >70% accuracy | Trend validation |
| 7 | All Horizons | >75% accuracy | Trend validation |
| 8 | Cross-Domain | >70% accuracy | Pattern validation |
| 9 | Quick Scan | <1 min | Response time test |
| 10 | Enrichment | >0.8 confidence | Enrichment validation |
| 11 | Advanced | >85% completeness | Scenario review |
| 12 | Performance | <1s (90p) | Load testing |
| 13 | Quality | >95% accuracy | Comprehensive test |

## Technical Considerations

### Performance

**Optimizations:**
- Batch processing for extraction
- Caching for frequent queries
- Denormalization for temporal queries (snapshots)
- Pagination for large results
- Indexes for all query patterns

**Limits:**
- Firestore document size (1MB)
- Query complexity
- Vector search performance

### Cost Management

**LLM Usage:**
- Batch extraction (10-20 docs per request)
- Model selection by complexity:
  - Simple: gemini_flash
  - Medium: gemini_fast
  - Complex: gemini_quality
- Caching by content hash

**Estimated Costs:**
- Extraction: $0.01-0.05 per document
- Entity Resolution: $0.001-0.01 per entity
- Enrichment: $0.001-0.005 per entity
- Scenario Analysis: $0.1-0.5 per analysis

### Quality Assurance

**Metrics:**
- Extraction accuracy
- Entity resolution F1 score
- Average confidence
- Source diversity
- Perspective balance
- Contradiction rate

**Validation:**
- Confidence calibration
- Cross-validation
- Multi-source verification
- Quality gates

### Risk Mitigation

**Entity Resolution:**
- Conservative thresholds
- Type-based filtering
- Context validation
- Confidence-based approval

**Bias Mitigation:**
- Multi-prompt strategies
- Source diversity tracking
- Perspective balance
- Bias detection

**Consistency:**
- Cross-domain validation
- Temporal alignment
- Value conflict detection
- Logical consistency checks

## API Design

### Endpoints

**Knowledge Extraction:**
```
POST /api/v1/knowledge/extract
GET  /api/v1/knowledge/entities/{id}
GET  /api/v1/knowledge/facts/{id}
```

**Query:**
```
POST /api/v1/knowledge/query
GET  /api/v1/knowledge/query/domains/{domain}
```

**Trends:**
```
GET  /api/v1/knowledge/trends
GET  /api/v1/knowledge/trends/cross-domain
GET  /api/v1/knowledge/trends/hierarchy
```

**Scenarios:**
```
POST /api/v1/knowledge/scenarios/generate
GET  /api/v1/knowledge/scenarios/{id}
```

**Chat Integration:**
```
POST /api/v1/chat/knowledge          # Text mode (existing)
POST /api/v1/chat/knowledge/scenario # Scenario mode (two-stage)
```

### WebSocket Events (Scenario Mode)

**Stage 1: Quick Scan**
```
# Client sends:
POST /api/v1/chat/knowledge/scenario
{
  "query": "...",
  "projectId": "...",
  "mode": "scenario"
}

# Server responds immediately (HTTP):
{
  "interactionId": "...",
  "status": "preliminary",
  "stage": "quick_scan",
  "confidence": 0.55
}

# Stage 2 starts automatically (async)
```

**Stage 2: Agent Orchestration**
```
# WebSocket Events (via existing realtime.Publisher):

1. EventScenarioOrchestrationStarted
{
  "event": "scenario.orchestration.started",
  "projectId": "...",
  "interactionId": "...",
  "tasks": [
    { "agent": "financial", "task": "Get GDP data", "priority": 1 },
    { "agent": "political", "task": "Get approval ratings", "priority": 1 }
  ]
}

2. EventScenarioAgentProgress (multiple, per agent)
{
  "event": "scenario.agent.progress",
  "projectId": "...",
  "interactionId": "...",
  "agentId": "financial",
  "progress": 50,  // 0-100
  "message": "Collecting market data..."
}

3. EventScenarioAgentCompleted (per agent)
{
  "event": "scenario.agent.completed",
  "projectId": "...",
  "interactionId": "...",
  "agentId": "financial",
  "result": {
    "entitiesFound": 12,
    "factsFound": 45,
    "confidence": 0.85
  }
}

4. EventScenarioOrchestrationCompleted
{
  "event": "scenario.orchestration.completed",
  "projectId": "...",
  "interactionId": "...",
  "enrichment": {
    "newEntities": 12,
    "newFacts": 45,
    "newTrends": 3,
    "confidenceImprovement": 0.27,  // 0.55 → 0.82
    "status": "final"
  }
}

# Interaction automatically updated in Firestore
# Frontend receives update via existing subscription
```

## Monitoring & Observability

### Metrics

**Quality:**
- Extraction accuracy
- Entity resolution F1
- Perspective balance
- Source diversity
- Confidence calibration

**Performance:**
- Extraction latency
- Query latency
- Entity resolution time
- Trend detection time

**Cost:**
- Cost per document
- Cost per entity
- Cost per query
- Cost per scenario

**Growth:**
- Entities count
- Facts count
- Trends count
- Growth rate

### Logging

- Extraction logs (with content hash)
- Entity resolution decisions
- Cross-domain link creation
- Trend detection triggers
- Scenario generation

## Security & Privacy

### Data Isolation

- **Global data:** tenantId = "" (public sources only)
- **Tenant data:** tenantId = "xxx" (private)
- **Never mix:** Private artifacts never in global KB

### Access Control

- Authentication required for all endpoints
- Tenant isolation in queries
- Read-only global KB
- Write access only for own tenant data

## Future Enhancements

1. **Manual Review Queue** - для спорных entity resolutions
2. **Expert Verification** - опциональная верификация критичных фактов
3. **Knowledge Graph Visualization** - UI для просмотра графа
4. **Predictive Models** - ML-based forecasting
5. **Additional Domains** - расширение на другие области знаний
6. **Knowledge Export** - экспорт знаний в стандартные форматы
7. **Integration APIs** - подключение внешних источников знаний

## Dependencies

- Existing infrastructure:
  - Firestore
  - genai.go (LLM client)
  - Authentication
  - Document storage
  
- New dependencies:
  - Vector search (embeddings)
  - Statistical libraries (trend detection)
  - Time series analysis

## Success Criteria

1. ✅ Knowledge extraction from documents
2. ✅ Entity resolution with >90% accuracy
3. ✅ Cross-domain relation detection
4. ✅ Trend detection on all horizons
5. ✅ Scenario generation integrating multiple domains
6. ✅ Chat integration working
7. ✅ Performance: <2s for queries, <30s for extraction
8. ✅ Cost: <$0.10 per document processed

## Risks & Mitigation

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Entity resolution errors | High | Medium | Conservative thresholds, validation |
| LLM cost explosion | High | Medium | Batching, caching, model selection |
| Performance degradation | Medium | Low | Indexes, caching, optimization |
| Bias in extraction | Medium | Medium | Multi-perspective, diversity tracking |
| Data quality issues | High | Medium | Quality gates, validation, metrics |

## Open Questions

1. **Manual Moderation:** Нужна ли очередь для ручной проверки спорных cases?
2. **Version Retention:** Как долго хранить старые версии фактов?
3. **Trend Update Frequency:** Как часто обновлять тренды для каждого горизонта?
4. **Cross-Domain Causality:** Как строго проверять причинность vs корреляцию?
5. **UI Integration:** Как интегрировать в существующий UI чата?

## References

- Entity Resolution: https://en.wikipedia.org/wiki/Entity_resolution
- Knowledge Graphs: https://en.wikipedia.org/wiki/Knowledge_graph
- Temporal Knowledge: Research on temporal validity
- Multi-Perspective Analysis: Bias mitigation strategies

---

**Document Version:** 1.0  
**Last Updated:** 2024-12-20  
**Author:** System Design Discussion  
**Reviewers:** TBD

