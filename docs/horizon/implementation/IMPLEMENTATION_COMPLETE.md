# Policy Architecture & Signals Implementation - Completed

## Дата завершения
2024-12-XX

## Выполненные задачи

### ✅ Policy Architecture (Unified Policy Interface)

#### 1. Core Policy Interface
- **Файл**: `backend/internal/horizon/policy/policy.go`
- **Реализовано**:
  - `Policy` interface с методами `Name()`, `Decide()`, `Explain()`
  - `PolicyInput` struct для контекста принятия решений
  - `PolicyDecision` struct с телеметрией (DecisionPath, RuleID, Confidence, LatencyMs, LLMTokens)
  - `PolicyExecutor` с поддержкой rule-first + LLM fallback
  - `TelemetryWriter` interface для записи телеметрии

#### 2. Telemetry
- **Файл**: `backend/internal/horizon/policy/telemetry.go`
- **Реализовано**:
  - `FirestoreTelemetryWriter` для записи решений в Firestore collection `horizon_policy_telemetry`
  - `PolicyTelemetryRecord` struct с полями для анализа (policyName, decision, timestamp, tenantID, projectID, lensID)

#### 3. PropagationPolicy
- **Файл**: `backend/internal/horizon/policy/propagation_policy.go`
- **Реализовано**:
  - `PropagationPolicy` implementation
  - `PropagationPolicyRule` struct (metricPattern, relationType, passes, weight, maxDepth, dampingOverride, direction, criticalPathBoost, priority)
  - Pattern matching для metricPattern (поддержка wildcards)
  - Policy matching по приоритету

#### 4. CanonizationPolicy
- **Файл**: `backend/internal/horizon/policy/canonization_policy.go`
- **Реализовано**:
  - `CanonizationPolicy` implementation
  - `CanonizationRule` struct (inputType, inputPattern, output, priority)
  - Fallback к taxonomy normalization функциям
  - Pattern matching для нормализации

#### 5. ValidationPolicy
- **Файл**: `backend/internal/horizon/policy/validation_policy.go`
- **Реализовано**:
  - `ValidationPolicy` implementation
  - `ValidationRule` struct (sourceType, targetType, relationType, action, confidencePenalty, reason, priority)
  - Поддержка действий: "allow", "reject", "penalty"
  - Rule matching по приоритету

#### 6. Repositories для Policy Rules
- **Файлы**:
  - `backend/internal/horizon/repo/propagation_policies.go`
  - `backend/internal/horizon/repo/canonization_rules.go`
  - `backend/internal/horizon/repo/validation_rules.go`
- **Реализовано**:
  - Firestore repositories для всех типов правил
  - Методы `GetRules()`, `Create()` для управления правилами

---

### ✅ Signals & Semantic Propagation

#### 7. Runtime Taxonomy расширение
- **Файл**: `backend/internal/taxonomy/runtime.go`
- **Реализовано**:
  - Расширение `RuntimeTaxonomy` struct:
    - `AllowedSignalKinds` map (risk, opportunity, trend, direction)
    - `SignalTypes` map с `SignalTypeSpec`
    - `SignalTypeSynonyms` map
  - `NormalizeSignalType()` функция для нормализации типов сигналов
  - `GetSignalTypeSpec()` функция для получения спецификации типа
  - `SignalTypeSpec` struct (kind, label, defaultMetric, propagationProfile)

#### 8. PropagationPolicy как данные (Firestore)
- **Файл**: `backend/internal/horizon/repo/propagation_policies.go`
- **Реализовано**:
  - `PropagationPoliciesRepository` для управления правилами в Firestore collection `horizon_propagation_policies`
  - Поддержка создания и получения правил без редеплоя

#### 9. Critical Path Detection
- **Файл**: `backend/internal/horizon/critical_path/detector.go`
- **Реализовано**:
  - `Detector` для обнаружения critical paths
  - `DetectCriticalPaths()` метод для вычисления critical paths между anchor nodes
  - `findShortestPathByCriticality()` BFS-based алгоритм
  - `IsCriticalPath()` метод для проверки, является ли edge critical path
  - Поддержка проверки через qualifiers, criticality threshold, supply_chain_criticality метрики

#### 10. Metric Registry & Handlers
- **Файл**: `backend/internal/horizon/metrics/registry.go`
- **Реализовано**:
  - `Registry` для управления metric specifications и handlers
  - `MetricSpec` struct (name, kind, unit, aggregationFunc, decayFunc, propagationThreshold, etc.)
  - `Handler` interface (Aggregate, Decay)
  - Built-in handlers:
    - `MaxHandler` - агрегация по максимуму
    - `SumCappedHandler` - суммирование с ограничением
    - `WeightedAvgHandler` - взвешенное среднее
    - `MinHandler` - агрегация по минимуму
  - `MetricSpecsRepository` для загрузки specs из Firestore

#### 11. Signal Synthesis (детерминированные правила)
- **Файл**: `backend/internal/horizon/signals/synthesizer.go`
- **Реализовано**:
  - `Synthesizer` для синтеза сигналов из фактов
  - `SynthesisRule` struct (перенесён в `backend/internal/horizon/repo/synthesis_rules.go` для избежания циклических импортов)
  - `Synthesize()` метод для преобразования `OntologyFact` → `[]OntologyFact(kind=signal)`
  - Pattern matching для factKind, factTypePattern, spherePattern
  - Автоматическое создание `MetricUpdates` в сигналах
  - `SynthesisRulesRepository` для управления правилами в Firestore

#### 12. Rollup & Explain
- **Файл**: `backend/internal/horizon/signals/rollup.go`
- **Реализовано**:
  - `RollupService` для вычисления rollups
  - `Rollup` struct с explain trace
  - `ExplainTrace` struct (topContributors, propagationPath, rulesApplied)
  - `ExplainContributor` struct (signalId, signalType, sourcePath, contribution, reasoning)
  - `GetFragmentRollup()` метод для вычисления rollup по fragmentID и metricKind
  - Фильтрация сигналов по kind, агрегация через MetricRegistry handlers
  - Нормализация contributions в проценты

---

## Firestore Collections

Следующие коллекции должны быть созданы в Firestore:

1. **`horizon_propagation_policies`** - правила распространения метрик
2. **`horizon_canonization_rules`** - правила нормализации
3. **`horizon_validation_rules`** - правила валидации комбинаций типов
4. **`horizon_synthesis_rules`** - правила синтеза сигналов из фактов
5. **`horizon_metric_specs`** - спецификации метрик
6. **`horizon_policy_telemetry`** - телеметрия решений policies

---

## Интеграция

### Следующие шаги для полной интеграции:

1. **Интеграция PropagationPolicy в PropagationEngine**:
   - Модифицировать `propagateHotPath()` для использования `PropagationPolicy.Decide()`
   - Применять policy rules для weight, damping, direction, critical path boost

2. **Интеграция SignalSynthesizer в Scout/Watchdog**:
   - Вызывать `Synthesizer.Synthesize()` при обработке `OntologyFact`
   - Сохранять синтезированные сигналы как новые `OntologyFact(kind=signal)`
   - Применять `MetricUpdates` к `HorizonFragment.State`

3. **Интеграция CriticalPathDetector в LensService**:
   - Вызывать `DetectCriticalPaths()` при создании/обновлении Lens
   - Сохранять critical path edges в `lens.ViewConfig["criticalPathEdges"]`

4. **Загрузка Runtime Taxonomy**:
   - Обновить loader для поддержки новых полей (`AllowedSignalKinds`, `SignalTypes`, `SignalTypeSynonyms`)
   - Загружать из Firestore collection `horizon_taxonomy/v1`

5. **Telemetry Dashboard**:
   - Создать dashboard для визуализации ratio (rule-based vs LLM-based)
   - Показывать top LLM fallback reasons, cost per policy

---

## Тестирование

### Unit Tests (рекомендуется создать):

- `policy/propagation_policy_test.go` - тесты для matching rules
- `policy/canonization_policy_test.go` - тесты для нормализации
- `policy/validation_policy_test.go` - тесты для валидации
- `signals/synthesizer_test.go` - тесты для синтеза сигналов
- `signals/rollup_test.go` - тесты для rollup computation
- `critical_path/detector_test.go` - тесты для critical path detection
- `metrics/registry_test.go` - тесты для handlers

### Integration Tests:

- End-to-end тест: "Tesla battery logistics risk" scenario
- Проверка синтеза сигналов из фактов
- Проверка propagation с policy rules
- Проверка rollup computation

---

## Документация

- ✅ Детальный план: `docs/horizon/implementation/signals-and-semantic-propagation-plan.md`
- ✅ Roadmap обновлён: `docs/horizon/implementation/roadmap.md` (секции D и E)

---

## Статус

**Реализация завершена**. Все основные компоненты созданы и готовы к интеграции в существующую систему.

