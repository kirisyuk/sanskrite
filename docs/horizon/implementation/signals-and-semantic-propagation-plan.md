# Signals & Semantic Propagation: Implementation Plan

## Обзор

Этот документ описывает план реализации **типизированных сигналов** (risk/opportunity/trend/direction) и **семантической пропагации** в Horizon. Цель — создать структурный слой, где ответы на вопросы типа "What are the top supply disruption risks for Tesla battery logistics?" строятся из графа и фактов, а не через LLM каждый раз.

---

## Принципы

1. **Расширять, не ломать**: совместимость с текущими `OntologyFact`, `HorizonFragment`, `DependencyEdge`.
2. **Сигналы = факты**: не создавать новую коллекцию; `OntologyFact(kind=signal)`.
3. **Propagation policies как данные**: матрица `(metricName × relationType) → behavior` в Firestore/config.
4. **Critical paths**: не все рёбра равны; некоторые ветки распространения приоритетнее.
5. **LLM — хирургически**: extraction/classification async через Cloud Tasks, не на каждый запрос.
6. **Rollups важнее raw signals**: пользователь видит агрегат + explain, не 50 сырых фактов.

---

## Фаза 1: Расширение таксономии (Signal Types)

### 1.1 Runtime Taxonomy: Signal Kinds & Types

Расширить `horizon_taxonomy/v1` в Firestore:

```json
{
  "version": "v1.1",
  "allowedSignalKinds": ["risk", "opportunity", "trend", "direction"],
  "signalTypes": {
    "risk:supply_disruption": {
      "kind": "risk",
      "label": "Supply Disruption Risk",
      "synonyms": ["logistics_risk", "supply_chain_risk"],
      "defaultMetric": "risk_supply_disruption_score",
      "propagationProfile": "upstream_amplified"
    },
    "risk:regulatory": {
      "kind": "risk",
      "label": "Regulatory Risk",
      "synonyms": ["compliance_risk", "legal_risk"],
      "defaultMetric": "risk_regulatory_score",
      "propagationProfile": "jurisdiction_scoped"
    },
    "risk:geopolitical": {
      "kind": "risk",
      "label": "Geopolitical Risk",
      "synonyms": ["sanction_risk", "political_risk"],
      "defaultMetric": "risk_geopolitical_score",
      "propagationProfile": "location_scoped"
    },
    "opportunity:cost_reduction": {
      "kind": "opportunity",
      "label": "Cost Reduction Opportunity",
      "synonyms": ["savings_opportunity"],
      "defaultMetric": "opportunity_cost_reduction_score",
      "propagationProfile": "downstream_amplified"
    },
    "direction:vertical_integration": {
      "kind": "direction",
      "label": "Vertical Integration Direction",
      "synonyms": ["integration_trend"],
      "defaultMetric": "direction_vertical_integration_score",
      "propagationProfile": "ownership_scoped"
    }
  },
  "signalTypeSynonyms": {
    "logistics_risk": "risk:supply_disruption",
    "supply_chain_risk": "risk:supply_disruption"
  }
}
```

### 1.2 Code: `taxonomy/runtime.go` расширение

```go
type RuntimeTaxonomy struct {
    // ... existing fields ...
    
    AllowedSignalKinds map[string]struct{}        // "risk", "opportunity", "trend", "direction"
    SignalTypes        map[string]SignalTypeSpec  // "risk:supply_disruption" -> spec
    SignalTypeSynonyms map[string]string          // alias -> canonical type
}

type SignalTypeSpec struct {
    Kind               string `json:"kind"`
    Label              string `json:"label"`
    DefaultMetric      string `json:"defaultMetric"`
    PropagationProfile string `json:"propagationProfile"`
}

func NormalizeSignalType(in string) string {
    // Similar to NormalizeEventType: apply synonyms, validate against allowlist
}
```

### 1.3 OntologyFact: kind=signal

Существующий `OntologyFact` уже поддерживает:
- `Kind`: добавить "signal" в allowlist
- `Type`: использовать `risk:supply_disruption` и т.п.
- `CanonicalPaths`, `SpherePaths`, `LocationPath`: привязка к узлам/сферам
- `Confidence`: сила сигнала (0..1)
- `MetricUpdates`: патч на rollup-метрики узла

**Никаких изменений в struct не требуется** — только runtime taxonomy и validation.

---

## Фаза 2: Propagation Policies как данные

### 2.1 PropagationPolicy struct

```go
// PropagationPolicy определяет поведение распространения для комбинации metric × relationType.
type PropagationPolicy struct {
    ID              string  `json:"id" firestore:"id"`
    MetricPattern   string  `json:"metricPattern" firestore:"metricPattern"`     // "risk_*" или "risk_supply_disruption_score"
    RelationType    string  `json:"relationType" firestore:"relationType"`       // "SUPPLIES_TO" или "*"
    
    // Behavior
    Passes          bool    `json:"passes" firestore:"passes"`                   // Проходит ли вообще
    Weight          float64 `json:"weight" firestore:"weight"`                   // Множитель (0..2, default 1.0)
    MaxDepth        int     `json:"maxDepth" firestore:"maxDepth"`               // Макс. глубина для этой комбинации
    DampingOverride *float64 `json:"dampingOverride" firestore:"dampingOverride"` // Override базового damping
    Direction       string  `json:"direction" firestore:"direction"`             // "upstream" | "downstream" | "both"
    
    // Critical path boost
    CriticalPathBoost float64 `json:"criticalPathBoost" firestore:"criticalPathBoost"` // Буст для critical paths (1.0 = no boost)
    
    Priority        int     `json:"priority" firestore:"priority"`               // Для разрешения конфликтов (выше = приоритетнее)
}
```

### 2.2 Firestore collection: `horizon_propagation_policies`

Примеры записей:

| metricPattern | relationType | passes | weight | maxDepth | direction | criticalPathBoost |
|---------------|--------------|--------|--------|----------|-----------|-------------------|
| `risk_supply_*` | `SUPPLIES_TO` | true | 1.2 | 4 | upstream | 1.5 |
| `risk_supply_*` | `COMPETES_WITH` | true | 0.3 | 2 | both | 1.0 |
| `risk_regulatory_*` | `REGULATES` | true | 1.5 | 3 | downstream | 1.3 |
| `risk_regulatory_*` | `SUPPLIES_TO` | true | 0.5 | 2 | upstream | 1.0 |
| `risk_geopolitical_*` | `LOCATED_IN` | true | 1.4 | 3 | both | 1.2 |
| `opportunity_*` | `SUPPLIES_TO` | true | 0.8 | 3 | downstream | 1.0 |
| `*` | `*` | true | 1.0 | 3 | both | 1.0 |

### 2.3 PolicyMatcher

```go
type PolicyMatcher struct {
    policies []*PropagationPolicy // sorted by priority desc
}

func (m *PolicyMatcher) Match(metricName, relationType string) *PropagationPolicy {
    // 1. Exact match metricName + relationType
    // 2. Pattern match (metric_* + relationType)
    // 3. Exact metricName + wildcard relationType
    // 4. Wildcard + wildcard (default)
    // Return highest priority match
}
```

### 2.4 Интеграция в PropagationEngine

```go
func (e *PropagationEngine) propagateHotPath(...) error {
    // ... existing code ...
    
    for _, edge := range outgoingEdges {
        // Get policy for this metric × relationType combination
        policy := e.policyMatcher.Match(metricName, string(edge.Type))
        
        if !policy.Passes {
            continue // This metric doesn't propagate through this edge type
        }
        
        // Apply policy-specific damping
        effectiveDamping := e.damping.baseDampingFactor
        if policy.DampingOverride != nil {
            effectiveDamping = *policy.DampingOverride
        }
        
        // Apply weight
        effectiveDelta := delta * policy.Weight * effectiveDamping
        
        // Check direction
        if policy.Direction == "upstream" && !isUpstreamEdge(edge) {
            continue
        }
        if policy.Direction == "downstream" && !isDownstreamEdge(edge) {
            continue
        }
        
        // Check depth limit
        if currentDepth >= policy.MaxDepth {
            continue
        }
        
        // Apply critical path boost if applicable
        if isCriticalPath(edge, sourceFragment, targetFragment) {
            effectiveDelta *= policy.CriticalPathBoost
        }
        
        // Continue propagation...
    }
}
```

---

## Фаза 3: Critical Paths

### 3.1 Что такое Critical Path

**Critical path** — это путь в графе, который:
1. Имеет высокую `criticality` на рёбрах (уже есть в `DependencyEdge.Criticality`)
2. Связывает узлы с высоким `supply_chain_criticality` или аналогичными метриками
3. Находится на "кратчайшем пути" между anchor-узлами Lens'а
4. Помечен явно через `DependencyEdge.Qualifiers["critical_path"] = true`

### 3.2 Critical Path Detection

**Статический режим** (при создании/обновлении Lens):
```go
func (s *LensService) detectCriticalPaths(ctx context.Context, lens *models.Lens) error {
    // 1. Получить все anchor nodes из lens.Scope
    // 2. Для каждой пары anchors найти shortest paths (BFS/Dijkstra по criticality)
    // 3. Пометить рёбра на этих путях как critical_path
    // 4. Сохранить в lens.ViewConfig["criticalPathEdges"] = []string{edgeIDs...}
}
```

**Динамический режим** (при propagation):
```go
func isCriticalPath(edge *models.DependencyEdge, source, target *models.HorizonFragment) bool {
    // 1. Check edge.Qualifiers["critical_path"]
    // 2. Check if edge.Criticality > threshold (e.g., 0.7)
    // 3. Check if source or target has high supply_chain_criticality
    return ...
}
```

### 3.3 Critical Path Boost в Propagation

Когда волна идёт по critical path:
- **Меньше затухания**: `dampingFactor *= 1.2` (или из policy)
- **Больше глубина**: `maxDepth += 1`
- **Приоритет в очереди**: hot path вместо cold path

---

## Фаза 4: Metric Registry & Handlers

### 4.1 MetricSpec

```go
type MetricSpec struct {
    Name           string  `json:"name"`           // "risk_supply_disruption_score"
    Kind           string  `json:"kind"`           // "risk" | "opportunity" | "trend" | "direction" | "operational"
    Unit           string  `json:"unit"`           // "ratio" | "score" | "index"
    
    // Aggregation
    AggregationFunc string  `json:"aggregationFunc"` // "max" | "sum_capped" | "weighted_avg" | "min"
    AggregationCap  *float64 `json:"aggregationCap"`  // For sum_capped
    
    // Decay
    DecayFunc       string   `json:"decayFunc"`       // "none" | "exponential" | "linear" | "step"
    DecayHalfLife   *string  `json:"decayHalfLife"`   // "7d" | "30d" | "90d"
    
    // Thresholds
    PropagationThreshold float64 `json:"propagationThreshold"` // Min delta to propagate
    AlertThreshold       *float64 `json:"alertThreshold"`       // For UI/notifications
    
    // Derived
    DerivedFrom []string `json:"derivedFrom"` // Other metrics this is derived from
}
```

### 4.2 Firestore collection: `horizon_metric_specs`

Примеры:

| name | kind | aggregationFunc | decayHalfLife | propagationThreshold |
|------|------|-----------------|---------------|----------------------|
| `risk_supply_disruption_score` | risk | max | 30d | 0.05 |
| `risk_regulatory_score` | risk | max | 90d | 0.1 |
| `opportunity_cost_reduction_score` | opportunity | sum_capped(1.0) | 14d | 0.05 |
| `lead_time_days` | operational | weighted_avg | none | 1.0 |

### 4.3 MetricHandler interface

```go
type MetricHandler interface {
    // Aggregate combines multiple input values into one (for rollups)
    Aggregate(inputs []MetricValue) MetricValue
    
    // Decay applies freshness decay to a value
    Decay(value MetricValue, now time.Time) MetricValue
    
    // Transform applies any custom transformation
    Transform(value MetricValue, context TransformContext) MetricValue
}

// Built-in handlers
type MaxHandler struct{}
type SumCappedHandler struct{ cap float64 }
type WeightedAvgHandler struct{}
type ExponentialDecayHandler struct{ halfLife time.Duration }
```

### 4.4 MetricRegistry

```go
type MetricRegistry struct {
    specs    map[string]*MetricSpec
    handlers map[string]MetricHandler
}

func (r *MetricRegistry) GetHandler(metricName string) MetricHandler {
    spec := r.specs[metricName]
    if spec == nil {
        return r.defaultHandler
    }
    return r.handlers[spec.AggregationFunc]
}
```

---

## Фаза 5: Signal Synthesis (Fact → Signal)

### 5.1 Детерминированные правила (без LLM)

```go
// SynthesisRule определяет автоматическое преобразование fact → signal
type SynthesisRule struct {
    ID              string            `json:"id"`
    FactKind        string            `json:"factKind"`        // "event" | "claim" | "metric"
    FactTypePattern string            `json:"factTypePattern"` // "port_closure" | "sanction_*"
    SpherePattern   string            `json:"spherePattern"`   // "/sphere/logistics/*"
    
    OutputSignalType string           `json:"outputSignalType"` // "risk:supply_disruption"
    ConfidenceMultiplier float64      `json:"confidenceMultiplier"` // 0.8 = 80% of fact confidence
    MetricName       string           `json:"metricName"`       // "risk_supply_disruption_score"
    MetricValue      *float64         `json:"metricValue"`      // Fixed value or nil (use confidence)
}
```

Firestore collection: `horizon_synthesis_rules`

| factKind | factTypePattern | spherePattern | outputSignalType | metricName |
|----------|-----------------|---------------|------------------|------------|
| event | port_closure | /sphere/logistics/* | risk:supply_disruption | risk_supply_disruption_score |
| event | sanction_* | * | risk:geopolitical | risk_geopolitical_score |
| claim | cost_increase | /sphere/procurement/* | risk:supply_disruption | risk_supply_disruption_score |
| claim | price_drop | /sphere/procurement/* | opportunity:cost_reduction | opportunity_cost_reduction_score |

### 5.2 SignalSynthesizer

```go
type SignalSynthesizer struct {
    rules []*SynthesisRule
}

func (s *SignalSynthesizer) Synthesize(fact *OntologyFact) []*OntologyFact {
    var signals []*OntologyFact
    for _, rule := range s.rules {
        if s.matches(fact, rule) {
            signal := s.createSignal(fact, rule)
            signals = append(signals, signal)
        }
    }
    return signals
}

func (s *SignalSynthesizer) createSignal(fact *OntologyFact, rule *SynthesisRule) *OntologyFact {
    return &OntologyFact{
        Kind:           "signal",
        Type:           rule.OutputSignalType,
        CanonicalPaths: fact.CanonicalPaths,
        SpherePaths:    fact.SpherePaths,
        LocationPath:   fact.LocationPath,
        Confidence:     fact.Confidence * rule.ConfidenceMultiplier,
        SourceRefs:     []SourceRef{{SourceID: fact.ID, SourceType: "ontology_fact"}},
        MetricUpdates: []FactMetricUpdate{{
            MetricName: rule.MetricName,
            Value:      s.computeValue(fact, rule),
            Confidence: fact.Confidence * rule.ConfidenceMultiplier,
            ObservedAt: fact.ObservedAt,
        }},
    }
}
```

### 5.3 Интеграция в Watchdog/Scout

```go
// В ScoutEngine.applyOntologyFacts или Watchdog.processFact:
func (e *ScoutEngine) processFact(ctx context.Context, fact *OntologyFact) error {
    // 1. Apply fact directly (existing logic)
    
    // 2. Synthesize signals from fact (deterministic rules)
    signals := e.signalSynthesizer.Synthesize(fact)
    for _, signal := range signals {
        // Save signal as OntologyFact(kind=signal)
        signalID, err := e.factsRepo.Create(ctx, signal)
        if err != nil {
            continue
        }
        
        // Apply signal's MetricUpdates to target fragments
        for _, path := range signal.CanonicalPaths {
            fragment, _ := e.fragmentsRepo.GetByCanonicalPath(ctx, "global", path)
            if fragment != nil {
                e.applyMetricUpdates(fragment, signal.MetricUpdates)
                e.fragmentsRepo.Update(ctx, fragment)
                // Trigger propagation
                e.propagationEngine.TriggerPropagation(ctx, fragment.ID)
            }
        }
    }
    
    return nil
}
```

---

## Фаза 6: Async LLM для сложных Propagation-решений

### 6.1 Когда нужен LLM

LLM нужен когда:
1. **Классификация неоднозначна**: факт не матчится ни одному synthesis rule
2. **Conflict resolution**: несколько сигналов противоречат
3. **Causal inference**: нужно понять "почему" для explain trace
4. **Novel patterns**: новый тип события, не покрытый правилами

### 6.2 Cloud Tasks Queue: `h1horizon-propagation-llm`

Отдельная очередь с:
- `maxConcurrentDispatches`: 10 (throttling)
- `rateLimits.maxDispatchesPerSecond`: 5
- `retryConfig.maxAttempts`: 3

### 6.3 PropagationLLMTask

```go
type PropagationLLMTask struct {
    TaskID          string                 `json:"taskId"`
    TenantID        string                 `json:"tenantId"`
    FragmentID      string                 `json:"fragmentId"`
    MetricName      string                 `json:"metricName"`
    InputSignals    []SignalSummary        `json:"inputSignals"`    // Summarized signals for context
    CurrentValue    float64                `json:"currentValue"`
    ProposedDelta   float64                `json:"proposedDelta"`
    
    // Damping context (LLM executor follows same rules)
    CurrentDepth    int                    `json:"currentDepth"`
    MaxDepth        int                    `json:"maxDepth"`
    DampingFactor   float64                `json:"dampingFactor"`
    
    // For explain trace
    UpstreamPath    []string               `json:"upstreamPath"`    // Fragment IDs in propagation chain
    
    CreatedAt       time.Time              `json:"createdAt"`
}
```

### 6.4 PropagationLLMExecutor

```go
type PropagationLLMExecutor struct {
    aiExecutor    *ai.AITaskExecutor
    fragmentsRepo *repo.HorizonFragmentsRepository
    damping       *WaveDamping
}

func (e *PropagationLLMExecutor) Execute(ctx context.Context, task *PropagationLLMTask) error {
    // 1. Check damping: if currentDepth >= maxDepth, skip
    if task.CurrentDepth >= task.MaxDepth {
        return nil // Damping rule: don't process beyond max depth
    }
    
    // 2. Build prompt
    prompt := e.buildPrompt(task)
    
    // 3. Call LLM with min_capability_score=6
    aiTask := &ai.AITask{
        TaskType: ai.TaskTypeKnowledgeSynthesis,
        InputContent: prompt,
        AIRequirements: ai.AIRequirements{
            MinCapabilityScore: 6,
            TextGenerationCapability: true,
        },
        Verifier: ai.NewSimpleJSONVerifier(),
    }
    
    result, err := e.aiExecutor.ExecuteStructuredTask(ctx, aiTask)
    if err != nil {
        return err
    }
    
    // 4. Parse LLM response
    var response PropagationLLMResponse
    // ... parse result into response ...
    
    // 5. Apply damping to LLM's suggested delta
    dampedDelta := response.SuggestedDelta * task.DampingFactor
    
    // 6. Update fragment
    fragment, _ := e.fragmentsRepo.Get(ctx, task.FragmentID)
    if fragment != nil {
        metric := fragment.State[task.MetricName]
        metric.Value += dampedDelta
        metric.Provenance.Method = "propagation_llm"
        metric.Provenance.SourceIDs = append(metric.Provenance.SourceIDs, task.TaskID)
        fragment.State[task.MetricName] = metric
        e.fragmentsRepo.Update(ctx, fragment)
    }
    
    // 7. If LLM suggests continuing propagation and depth allows, create child tasks
    if response.ContinuePropagation && task.CurrentDepth+1 < task.MaxDepth {
        // ... create child PropagationLLMTasks for downstream fragments ...
    }
    
    return nil
}

type PropagationLLMResponse struct {
    SuggestedDelta      float64  `json:"suggestedDelta"`
    Confidence          float64  `json:"confidence"`
    Reasoning           string   `json:"reasoning"`
    ContinuePropagation bool     `json:"continuePropagation"`
    AffectedMetrics     []string `json:"affectedMetrics"` // Other metrics that should be updated
}
```

### 6.5 Prompt Template

```
You are a propagation analysis agent for Horizon.

TASK: Determine how metric changes should propagate through the knowledge graph.

CURRENT STATE:
- Fragment: {fragmentPath} ({fragmentType})
- Metric: {metricName}
- Current Value: {currentValue}
- Proposed Delta: {proposedDelta}

INPUT SIGNALS:
{signalsSummary}

PROPAGATION CONTEXT:
- Depth: {currentDepth}/{maxDepth}
- Upstream Path: {upstreamPath}
- Damping Factor: {dampingFactor}

RULES:
1. Consider the semantic meaning of the metric and fragment type.
2. Proposed delta has already been damped; you may adjust further or confirm.
3. If delta is insignificant (< 0.01 after your adjustment), set suggestedDelta to 0.
4. Explain your reasoning briefly.

OUTPUT (JSON):
{
  "suggestedDelta": <float>,
  "confidence": <0..1>,
  "reasoning": "<string>",
  "continuePropagation": <bool>,
  "affectedMetrics": ["<other_metric_names_if_any>"]
}
```

---

## Фаза 7: Rollup & Explain

### 7.1 Rollup Computation

При запросе к Lens или Fragment:

```go
func (s *HorizonService) GetFragmentRollup(ctx context.Context, fragmentID string, metricKind string) (*Rollup, error) {
    fragment, _ := s.fragmentsRepo.Get(ctx, fragmentID)
    
    // Get all signals attached to this fragment
    signals, _ := s.factsRepo.ListByCanonicalPathAndKind(ctx, fragment.CanonicalPath, "signal")
    
    // Filter by kind (e.g., "risk")
    filtered := filterByKind(signals, metricKind)
    
    // Get metric spec for aggregation
    spec := s.metricRegistry.GetSpec(metricKind + "_score")
    handler := s.metricRegistry.GetHandler(spec.Name)
    
    // Apply decay to each signal
    now := time.Now()
    decayedValues := make([]MetricValue, len(filtered))
    for i, sig := range filtered {
        decayedValues[i] = handler.Decay(sig.MetricUpdates[0].ToMetricValue(), now)
    }
    
    // Aggregate
    aggregated := handler.Aggregate(decayedValues)
    
    return &Rollup{
        MetricName:   spec.Name,
        Value:        aggregated.Value,
        Confidence:   aggregated.Confidence,
        InputCount:   len(filtered),
        LastUpdated:  now,
        ExplainTrace: s.buildExplainTrace(filtered),
    }, nil
}
```

### 7.2 Explain Trace

```go
type ExplainTrace struct {
    TopContributors []ExplainContributor `json:"topContributors"`
    PropagationPath []string             `json:"propagationPath"` // Fragment IDs
    RulesApplied    []string             `json:"rulesApplied"`    // Synthesis rule IDs
}

type ExplainContributor struct {
    SignalID    string  `json:"signalId"`
    SignalType  string  `json:"signalType"`
    SourcePath  string  `json:"sourcePath"`  // Canonical path of origin
    Contribution float64 `json:"contribution"` // % of final value
    Reasoning   string  `json:"reasoning"`    // From LLM or rule
}
```

---

## Порядок реализации (по фазам)

### Sprint 1: Foundation (1.5 недели)
- [ ] Фаза 1.1-1.2: Runtime Taxonomy расширение (signal kinds/types)
- [ ] Фаза 2.1-2.3: PropagationPolicy struct + Firestore collection + PolicyMatcher
- [ ] Фаза 3.1-3.2: Critical Path detection (базовый: по criticality threshold)

### Sprint 2: Semantic Propagation (1.5 недели)
- [ ] Фаза 2.4: Интеграция PolicyMatcher в PropagationEngine
- [ ] Фаза 3.3: Critical Path Boost в propagation
- [ ] Фаза 4.1-4.4: MetricSpec + MetricRegistry + Handlers

### Sprint 3: Signal Synthesis (1 неделя)
- [ ] Фаза 5.1-5.2: SynthesisRule + SignalSynthesizer
- [ ] Фаза 5.3: Интеграция в Scout/Watchdog

### Sprint 4: Async LLM (1 неделя)
- [ ] Фаза 6.1-6.2: Cloud Tasks queue setup
- [ ] Фаза 6.3-6.5: PropagationLLMTask + Executor + Prompt

### Sprint 5: Rollup & Explain (1 неделя)
- [ ] Фаза 7.1: Rollup computation
- [ ] Фаза 7.2: Explain trace
- [ ] API endpoints: GET /api/horizon/fragments/{id}/rollup, GET /api/horizon/fragments/{id}/explain

### Sprint 6: Validation & Polish (0.5 недели)
- [ ] End-to-end test: "Tesla battery logistics risk" scenario
- [ ] Туning: thresholds, damping factors, policies
- [ ] Documentation update

---

## Зависимости и риски

### Зависимости
- Runtime Taxonomy loader должен поддерживать новые поля
- Firestore indexes для `ontology_facts` по `kind=signal`
- Cloud Tasks queue creation (отдельная от `h1horizon-scout-v2`)

### Риски
- **LLM latency**: async queue митигирует, но explain trace может быть "stale"
- **Policy explosion**: если много комбинаций metric × relationType — нужен UI для управления
- **Critical path computation cost**: BFS/Dijkstra на больших графах может быть дорогим — кэширование на Lens уровне

---

## Метрики успеха

- **Latency**: rollup computation < 100ms (без LLM)
- **Accuracy**: risk scores correlate with actual supply chain events (manual validation)
- **Coverage**: 80%+ фактов покрыты synthesis rules (не требуют LLM)
- **Explainability**: каждый rollup имеет explain trace с топ-3 contributors


