# Knowledge Graph & Scenario Analysis - Review

## Проверка соответствия идее

**Дата:** 2025-11-03  
**Статус:** ✅ Все несоответствия исправлены

---

## ✅ Что соответствует идее

### 1. Двухэтапный процесс
- ✅ **Phase 1 (Quick Scan):** Быстрый ответ (<1 минута) на основе существующих знаний
- ✅ **Phase 2 (Agent Orchestration):** Асинхронное обогащение данными от агентов (2-10 минут)
- ✅ WebSocket для прогресса
- ✅ Обновление существующего Interaction (preliminary → enriched)

### 2. Глобальная база знаний
- ✅ Layer 1 (RAW DATA) - общие внешние источники
- ✅ Layer 2 (EXTRACTED KNOWLEDGE) - глобальная база знаний
- ✅ Layer 3 (TENANT-SPECIFIC) - приватные данные тенанта
- ✅ tenantId="" для глобальных данных

### 3. Domain Agents
- ✅ 4 агента: Financial, Political, Social, Technical
- ✅ Унифицированный DomainAgent interface
- ✅ Независимые модульные сервисы
- ✅ Multi-perspective extraction для Political domain

### 4. Trend Detection
- ✅ Иерархическая система: Immediate, Short-Term, Medium-Term, Long-Term, Strategic
- ✅ Within-domain и cross-domain trends
- ✅ Temporal lag detection

### 5. UI Integration
- ✅ UnfoldRequest с режимом "scenario"
- ✅ ScenarioMessage компонент для отображения
- ✅ ChatMessage.tsx детектирует subkind="scenario.analysis"
- ✅ Toggle между Summary и Graph View

### 6. React Flow Graph
- ✅ Графовое представление scenario analysis
- ✅ Узлы: Entity, Fact, Trend, Scenario, Source, Metric, Risk
- ✅ Связи: influences, contributes, causes, evidences, temporal
- ✅ Contribution tracing (path от Fact/Trend до Scenario)
- ✅ Interactive features (filtering, highlighting)

### 7. Fractal Implementation Plan
- ✅ Iteration 0: Architecture & Types (nothing breaks)
- ✅ Iteration 1: Core Extraction (minimally working)
- ✅ Каждая итерация - рабочая система, качество улучшается

---

## ❌ Несоответствия и проблемы

### 1. **Структура данных ScenarioAnalysis**

**Проблема:** Go структура не соответствует JSON content и TypeScript коду.

**Go структура (строка 253):**
```go
type ScenarioAnalysis struct {
    Scenarios          []Scenario
    AlternativeScenarios []AlternativeScenario
    Risks              []RiskAssessment
    Uncertainty        UncertaintyAnalysis
    CrossDomainAnalysis CrossDomainAnalysis
}
```

**JSON content (строки 727, 773):**
```json
{
  "phase": "quick_scan",
  "scenarios": [...],
  "knowledgeBase": {        // ❌ НЕТ в Go структуре
    "entities": [...],
    "facts": [...],
    "trends": [...]
  },
  "pendingTasks": [...],    // ❌ НЕТ в Go структуре
  "confidence": 0.55,       // ❌ НЕТ в Go структуре
  "status": "preliminary"   // ❌ НЕТ в Go структуре
}
```

**TypeScript код (строка 1124):**
```typescript
scenario.knowledgeBase.entities.forEach(entity => {  // ❌ Ожидает knowledgeBase
```

**Исправление:**
```go
type ScenarioAnalysis struct {
    // Phase information
    Phase               string              // "quick_scan" or "enriched"
    Status              string              // "preliminary" or "final"
    Confidence          float64
    
    // Knowledge Base (for graph visualization)
    KnowledgeBase       KnowledgeBaseSnapshot
    
    // Scenarios
    Scenarios           []Scenario
    AlternativeScenarios []AlternativeScenario
    
    // Cross-domain analysis
    CrossDomainLinks    []CrossDomainLink
    
    // Assessment
    Risks               []RiskAssessment
    Uncertainty         UncertaintyAnalysis
    
    // For Phase 1 (preliminary)
    PendingTasks        []TaskSpec          // Only in Phase 1
    
    // For Phase 2 (enriched)
    Enrichment          *EnrichmentInfo     // Only in Phase 2
}

type KnowledgeBaseSnapshot struct {
    Entities    []KnowledgeEntity
    Facts       []KnowledgeFact
    Trends      []Trend
    Events      []KnowledgeEvent
    Metrics     []KnowledgeMetric
}

type EnrichmentInfo struct {
    NewEntities         int
    NewFacts            int
    NewTrends           int
    ImprovedScenarios   bool
    ConfidenceImprovement float64
    AgentResults        map[string]AgentResult
}
```

---

### 2. **Структура Scenario**

**Проблема:** В Go структуре нет полей для contribution tracking.

**Go структура (строка 265):**
```go
type Scenario struct {
    ID              string
    Title           string
    Type            string // "baseline", "optimistic", "pessimistic"
    Domains         []string
    DomainScenarios map[string]DomainScenario
    CrossDomainLinks []CrossDomainLink
    Likelihood      string
    Confidence      float64
    Impact          MultiDomainImpact
}
```

**TypeScript код (строки 1224, 1241):**
```typescript
scenarioItem.contributingFacts?.forEach(factId => {  // ❌ НЕТ в Go структуре
scenarioItem.contributingTrends?.forEach(trendId => { // ❌ НЕТ в Go структуре
```

**Исправление:**
```go
type Scenario struct {
    ID                  string
    Title               string
    Type                string // "baseline", "optimistic", "pessimistic"
    Description         string
    
    // Domains
    Domains             []string
    DomainScenarios     map[string]DomainScenario
    
    // Cross-domain
    CrossDomainLinks    []CrossDomainLink
    
    // Contributions (для графа)
    ContributingFacts   []string          // Fact IDs
    ContributingTrends  []string          // Trend IDs
    ContributingEvents  []string          // Event IDs
    
    // Assessment
    Likelihood          float64           // 0-1 (не string!)
    Confidence          float64
    Impact              MultiDomainImpact
    
    // Sources
    Sources             []SourceRef
}
```

---

### 3. **QuickScanResult не определена**

**Проблема:** Используется в коде, но структура не определена.

**Код (строка 430):**
```go
return &QuickScanResult{
    Scenarios: quickScenarios,
    KnowledgeBase: existingKnowledge,
    PendingTasks: tasks,
    Confidence: 0.55,
    Status: "preliminary",
    ProcessingTime: time.Since(startTime),
}, nil
```

**Исправление:** Добавить определение:
```go
type QuickScanResult struct {
    Scenarios       []Scenario
    KnowledgeBase   KnowledgeBaseSnapshot
    PendingTasks    []TaskSpec
    Confidence      float64
    Status          string
    ProcessingTime  time.Duration
}
```

---

### 4. **Likelihood: string vs float64**

**Проблема:** В Go структуре `Likelihood string`, в UI отображается как число `{data.likelihood}%`.

**Go (строка 272):**
```go
Likelihood      string
```

**TypeScript UI (строка 1450):**
```typescript
<span className="likelihood">{data.likelihood}%</span>  // Ожидает число
```

**Исправление:** Использовать `float64` (0-1) везде, в UI умножать на 100 для процентов.

---

### 5. **CrossDomainAnalysis vs CrossDomainLinks**

**Проблема:** В `ScenarioAnalysis` есть `CrossDomainAnalysis`, но в коде используется `scenario.crossDomainLinks`.

**Go (строка 258):**
```go
CrossDomainAnalysis CrossDomainAnalysis
```

**TypeScript (строка 1259):**
```typescript
scenario.crossDomainLinks.forEach(link => {  // Ожидает массив
```

**Исправление:** В `ScenarioAnalysis` использовать `[]CrossDomainLink` вместо `CrossDomainAnalysis`.

---

### 6. **Scenario.sources не определено**

**Проблема:** В TypeScript ожидается `scenarioItem.sources`, но в Go структуре его нет.

**TypeScript (строка 1218):**
```typescript
sources: scenarioItem.sources,  // ❌ НЕТ в Go Scenario
```

**Исправление:** Добавить `Sources []SourceRef` в `Scenario` структуру (см. исправление #2 выше).

---

### 7. **JSON маршалинг полей**

**Проблема:** Go структуры не имеют JSON tags для корректного маршалинга в camelCase.

**Исправление:** Добавить JSON tags:
```go
type ScenarioAnalysis struct {
    Phase               string                  `json:"phase"`
    Status              string                  `json:"status"`
    Confidence          float64                 `json:"confidence"`
    KnowledgeBase       KnowledgeBaseSnapshot   `json:"knowledgeBase"`
    Scenarios           []Scenario              `json:"scenarios"`
    CrossDomainLinks    []CrossDomainLink       `json:"crossDomainLinks"`
    // ... etc
}
```

---

## 🔧 Рекомендации по исправлению

### Приоритет 1 (Критично)
1. ✅ Добавить `KnowledgeBase` в `ScenarioAnalysis`
2. ✅ Добавить `Phase`, `Status`, `Confidence` в `ScenarioAnalysis`
3. ✅ Добавить `ContributingFacts`, `ContributingTrends` в `Scenario`
4. ✅ Изменить `Likelihood` с `string` на `float64`
5. ✅ Добавить определение `QuickScanResult`

### Приоритет 2 (Важно)
6. ✅ Добавить JSON tags для всех структур
7. ✅ Добавить `Sources` в `Scenario`
8. ✅ Заменить `CrossDomainAnalysis` на `[]CrossDomainLink` в `ScenarioAnalysis`
9. ✅ Добавить `EnrichmentInfo` структуру

### Приоритет 3 (Хорошо иметь)
10. Добавить примеры JSON для всех структур (для документации)
11. Добавить validation rules (confidence 0-1, likelihood 0-1, etc.)
12. Добавить unit tests для `scenarioToGraph` трансформации

---

## ✅ Общая оценка

**Архитектура:** ✅ Отлично - соответствует идее  
**Двухэтапный процесс:** ✅ Отлично - правильно описан  
**UI Integration:** ✅ Хорошо - логично и понятно  
**React Flow Graph:** ✅ Отлично - детально проработано  

**Структуры данных:** ❌ Несоответствия между Go и TypeScript  
**Consistency:** ⚠️ Требуются исправления  

**Общая оценка:** 8/10

---

## ✅ Исправления внесены

Все обнаруженные несоответствия исправлены в документе `knowledge-graph-scenario-analysis.md`:

### 1. Структура ScenarioAnalysis
✅ **Добавлены поля:**
- `Phase string` ("quick_scan" or "enriched")
- `Status string` ("preliminary" or "final")
- `Confidence float64` (общий уровень уверенности)
- `KnowledgeBase KnowledgeBaseSnapshot` (для графового представления)
- `PendingTasks []TaskSpec` (для Phase 1)
- `Enrichment *EnrichmentInfo` (для Phase 2)
- `CrossDomainLinks []CrossDomainLink` (вместо CrossDomainAnalysis)

✅ **Добавлены новые типы:**
- `KnowledgeBaseSnapshot` - снимок базы знаний
- `EnrichmentInfo` - информация об обогащении
- `QuickScanResult` - результат быстрого скрининга

### 2. Структура Scenario
✅ **Добавлены поля:**
- `Description string` (описание сценария)
- `ContributingFacts []string` (для contribution tracing)
- `ContributingTrends []string` (для contribution tracing)
- `ContributingEvents []string` (для contribution tracing)
- `Sources []SourceRef` (источники для сценария)

✅ **Исправлены типы:**
- `Likelihood float64` (было string, теперь 0-1)

### 3. JSON Tags
✅ Добавлены JSON tags для всех полей структур (camelCase маршалинг)

### 4. TypeScript Interfaces
✅ Добавлены полные TypeScript интерфейсы, соответствующие Go структурам:
- `ScenarioAnalysis`
- `KnowledgeBaseSnapshot`
- `EnrichmentInfo`
- `Scenario`
- `TaskSpec`

### 5. UI Code
✅ Исправлены компоненты для работы с likelihood как number (0-1):
- `ScenarioNode.tsx`: `{Math.round(data.likelihood * 100)}%`
- `scenarioToGraph`: `{Math.round(scenarioItem.likelihood * 100)}%`

### 6. JSON Examples
✅ Обновлены все JSON примеры для соответствия новым структурам:
- Phase 1 (preliminary) пример
- Phase 2 (enriched) пример

---

## Итог

**Статус:** ✅ Документ полностью готов к реализации

Документ **полностью соответствует идее** и готов к реализации:

✅ Двухэтапный процесс (Quick Scan + Agent Orchestration)  
✅ Глобальная база знаний (Layer 1-2-3)  
✅ Domain Agents (Financial, Political, Social, Technical)  
✅ Trend Detection (иерархическая система)  
✅ UI Integration (UnfoldRequest, ScenarioMessage, Graph View)  
✅ React Flow Graph (полная визуализация с contribution tracing)  
✅ Fractal Implementation Plan (13 итераций)  
✅ Структуры данных синхронизированы (Go ↔ TypeScript)  

**Общая оценка:** 10/10

Система согласована, логична и готова к поэтапной реализации.

