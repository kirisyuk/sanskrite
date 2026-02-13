# План реализации: Internal-First Grounding для Scout и Risk

## Статус
- Тип: implementation plan
- Дата: 2026-02-13
- Ревизия: v7 (сверка с кодом — актуальные статусы всех пунктов)
- Область: `backend/internal/horizon/scout`, `backend/internal/horizon/risk`, `backend/internal/ai`, `backend/internal/horizon/domain`, `backend/cmd/server/main.go`

### Легенда статусов
- `[ЕСТЬ]` — код полностью реализован и работает в runtime.
- `[ЕСТЬ, НЕ ПОДКЛЮЧЁН]` — код написан, но не вызывается в основном flow. Нужно подключить/активировать.
- `[ДОДЕЛАТЬ]` — частичная реализация есть; нужно расширить или дотянуть.
- `[С НУЛЯ]` — ничего нет, писать целиком.

### Сводка по Phase

| Phase | Статус | Детали |
|-------|--------|--------|
| **P0** Контракты, flags | ✅ Готово | Все 3 feature flags, provenance schema, state key константы — реализованы |
| **P1** AI/grounding wiring | ✅ Готово | Model registry, VertexAI wiring, все 4 grounded агента на `ExecuteStructuredTaskFull` |
| **P2** Materialization | ✅ Готово | Grounding-aware path активирован, Domain Answer Layer интегрирован, reason codes, provenance |
| **P3** Internal-first domain jobs | ✅ Готово | Expansion + overlay используют PlanSources/fast-path/graph context. Completeness gate — отложен |
| **P4** Risk hardening | ✅ Готово | fragmentId-first, preflight, anti-cascade + cooldown |
| **P5** Наблюдаемость | ⚠️ Почти готово | Метрики, structured logs, no-silent-fail — готовы. Dashboard/alert конфиги — НЕТ |

**Ключевой итог**: реализация практически полная. Единственный открытый пункт — P5.4 (dashboard/alert конфигурации).

## 1. Проблема

Scout **уже частично реализует** internal-first стратегию: source planning предпочитает Firestore facts, fast-path может завершить job без AI-агентов, canonicalizer ищет существующие фрагменты через Vertex AI Search. Однако эта стратегия **не завершена**: domain jobs обходят internal-first фазы, grounding evidence из AI-вызовов теряется, а materialization не использует готовый grounding-aware путь. Это приводит к ложным дубликатам, потому что:
1. Grounding evidence из AI-ответов не доходит до materialization слоя (агенты вызывают `ExecuteStructuredTask`, а не `ExecuteStructuredTaskFull`).
2. Есть готовый grounding-aware путь дедупа (`processDiscoveredRelationsWithGrounding`), но в основном flow он не используется.
3. Domain jobs (`executeDomainExpansionJob`, `executeDomainStrategicOverlayJob`) идут сразу к AI-агентам, минуя internal-first фазы planning и fast-path.
4. Risk stage `IDENTIFY` опирается на `targetCanonicalPath`, а не на стабильный `fragmentId` при его наличии.
5. Не везде прозрачно различается происхождение данных: внутренние (система) vs внешние (web).
6. В некоторых местах ошибки обрабатываются как best-effort без унифицированного префикса/контекста, что усложняет быстрый RCA (root cause analysis).

## 2. Цели
1. Сделать `internal-first` поведением по умолчанию в Scout и Risk.
2. Использовать внешний search grounding только как fallback/расширение.
3. Передавать в дедуп и materialization строгий provenance (internal/external/mixed).
4. Снизить false-positive дедупа и повторное создание уже существующих фрагментов.
5. Сделать аудит происхождения данных детерминированным и наблюдаемым в логах/метриках.
6. Ввести сквозное логгирование c единым префиксом и запрет silent исключений, чтобы инциденты быстро локализовались до конкретной причины.

## 3. Не-цели
1. Не менять бизнес-онтологию и taxonomy namespaces.
2. Не пересобирать полностью движок дедупа с нуля.
3. Не делать массовую историческую миграцию всех старых артефактов в рамках первого релиза.

## 4. Текущее состояние (подтверждено кодом)

### 4.1 AI routing и grounding
1. `[ЕСТЬ]` `VertexAIService` реализован (`ai/vertex_ai_service.go`): mixed grounding (datastore + google search), парсинг `GroundingEvidence`. **Подключён** в `main.go` первым в списке `aiServices` (при наличии `GCP_PROJECT_ID`), с `WithDataStore()` для Horizon datastore.
2. `[ЕСТЬ]` Scout-агенты вызывают `ExecuteStructuredTaskFull` и возвращают `GroundingEvidence` через `*WithGrounding` варианты: `research_agent`, `scout_agent`, `strategic_overlay_agent`, `intent_decomposer_agent`.
3. `[ЕСТЬ]` Модели `vertex-gemini-2.5-pro`, `vertex-gemini-3-pro-preview`, `vertex-gemini-3-flash-preview` зарегистрированы с `Provider: AIProviderVertexAI` и `InternalDataGroundingCapability: true`. `SelectModel()` фильтрует по этому флагу.

### 4.2 Существующая internal-first стратегия в Scout (отдельные фазы, не через AI grounding)
Scout уже реализует internal-first через прямые вызовы Vertex AI Search и Firestore — **параллельный механизм**, независимый от AI model grounding:

1. **Source planning — Firestore facts first** (`planner.go: PlanSources()`):
   - Priority 1: `factsRepo.ListUpdatedSinceByCanonicalPath()` — загрузка фактов напрямую из Firestore.
   - Priority 2: Vertex AI Search fallback — только если Firestore не вернул фактов.
   - Работает **до** запуска AI-агентов.

2. **Fast-path facts application** (`engine.go`):
   - `FactApplier.TryApplyFactsFastPath()` детерминистически применяет факты с порогами relevance (0.70) / confidence (0.80).
   - Если факты проходят пороги — job завершается **без вызова AI-агентов вообще**.

3. **Canonical path resolution — fragment lookup через Vertex AI Search** (`canonicalizer.go`):
   - `resolveRelationCanonicalPath()` ищет существующие фрагменты по имени через `horizonVertexClient()`.

4. **Fact relevance scoring** (`fact_applier.go`):
   - Vertex AI Search relevance score как прокси для косинусного сходства между фактами и фрагментами.

5. **Graph-backed context fallback** (`engine.go: buildGraphBackedInternalContext()`):
   - Target fragment + 1-hop graph neighborhood + ontology facts.

### 4.3 Scout materialization (grounding-aware path)
1. `[ЕСТЬ]` `processDiscoveredRelationsWithGrounding(...)` (`engine_ext.go`) **вызывается** в основном flow (`engine.go:3696`) и в domain expansion (`engine_ext.go:2203`).
2. `[ЕСТЬ]` `GroundingEvidenceResolver` **вызывается**: `Resolve()` в `engine.go:3682` и `engine_ext.go:2199`. `FindBestMatch()` в `engine_ext.go:3165`.
3. `[ЕСТЬ]` `processRelationsInternal` содержит grounding-aware ветку (`groundingCtx != nil && !groundingCtx.IsEmpty()`) — **активируется** при `internalFirstGroundingEnabled()`.

### 4.4 Domain Answer Layer (текущее состояние)
1. `[ЕСТЬ]` `DomainEngine` реализует 5-фазный pipeline с `GroundingEvidence` в `RunInput`.
2. `[ЕСТЬ]` `ExecuteBaseCollect` и `ExecuteStrategicCollect` принимают `*ai.GroundingEvidence` и передают его в Scout materialization.
3. `[ЕСТЬ]` Gate L2 и Structured Narrative используют `ExecuteStructuredTaskFull`, grounding evidence применяется к snapshot через `applyGroundingEvidenceToSnapshot()`.
4. `[ЕСТЬ]` `DomainAnswerSnapshot` содержит `OriginMode`, `InternalEvidence`, `ExternalEvidence`, `ResolutionStats`.

### 4.5 Datastore scoping
1. `[ЕСТЬ]` `horizonVertexClient()` используется консистентно: planner, fact_applier, canonicalizer — все через horizon-scoped client.

### 4.6 Risk pipeline
1. `[ЕСТЬ]` `stageIdentify()` проверяет `targetFragmentId`/`fragmentId` из state (gated by `riskIdentifyFragmentIDEnabled()`), fallback на `GetByCanonicalPath()`.
2. `[ЕСТЬ]` API `CreateRiskJob` включает preflight resolve `targetFragmentId`.
3. `[ЕСТЬ]` Anti-cascade: `riskTriggeredScoutRefresh` marker, max depth=1, cooldown 180 min (configurable).

## 5. Целевая архитектура (North Star)

### 5.1 Принцип data precedence
1. Internal datastore/graph/facts — первичный источник.
2. External web grounding — только если internal контекст пуст или недостаточен для полноты.
3. Любое внешнее утверждение сначала пытается связаться с внутренним fragmentId через grounding evidence и/или canonical resolver.

### 5.2 Provenance contract
Единый контракт происхождения для job/frame/edge/fact:
1. `originMode`: `internal_only | external_only | mixed`.
2. `internalEvidence`:
- `fragmentIds[]`
- `datastoreUris[]`
- `sourceIds[]`
- `documentIds[]`
3. `externalEvidence`:
- `webUrls[]`
- `citations[]`
4. `resolutionStats`:
- `relationsResolvedByGrounding`
- `relationsCreatedNew`
- `dedupRedirectsByGrounding`

### 5.3 Model routing через capability flag
`AIModelCapabilities` содержит `InternalDataGroundingCapability`. Модели VertexAI зарегистрированы как зеркала grounding-capable Gemini-моделей с `Provider: AIProviderVertexAI` и `InternalDataGroundingCapability: true`. При запросе с `SearchGroundingCapability=true` + `InternalDataGroundingCapability=true` `SelectModel()` выбирает модель через стандартный пайплайн.

## 6. Детальный план реализации

## Phase 0. Контракты и feature flags (подготовка) — ✅ ГОТОВО

### P0.1 Ввести флаги управления rollout
1. `[ЕСТЬ]` `HORIZON_GROUNDING_INTERNAL_FIRST=1` — master switch. `grounding_runtime.go:18`, функция `internalFirstGroundingEnabled()`.
2. `[ЕСТЬ]` `HORIZON_GROUNDING_REQUIRE_PROVENANCE=1` — fail-soft guard. `grounding_runtime.go:19`, функция `requireGroundingProvenanceEnabled()`.
3. `[ЕСТЬ]` `HORIZON_RISK_IDENTIFY_USE_FRAGMENT_ID=1`. `runtime_flags.go:9`, функция `riskIdentifyFragmentIDEnabled()`.

### P0.2 Зафиксировать schema для provenance
1. `[ЕСТЬ]` Стандартизированы ключи в `job.State`: `stateKeyGroundingContractVer`, `stateKeyOriginMode`, `stateKeyInternalEvidence`, `stateKeyExternalEvidence`, `stateKeyResolutionStats` + error state keys — все как константы в `grounding_runtime.go:20-31`.
2. `[ЕСТЬ]` Версионирование: `groundingContractVersionV1 = "v1"` (`grounding_runtime.go:17`).
3. `[ЕСТЬ]` `Provenance` struct (`models/common.go:38-60`) расширен: `OriginMode`, `InternalEvidence` (`*GroundingInternalEvidence`), `ExternalEvidence` (`*GroundingExternalEvidence`), `ResolutionStats` (`*GroundingResolutionStats`), `GroundingContractVersion`.

## Phase 1. Wiring AI/grounding в Scout — ✅ ГОТОВО

### P1.1 Расширить model registry для VertexAI internal grounding
1. `[ЕСТЬ]` `InternalDataGroundingCapability bool` в `AIModelCapabilities` и `AIRequirements` (`ai/models.go`).
2. `[ЕСТЬ]` Зарегистрированы `vertex-gemini-2.5-pro`, `vertex-gemini-3-pro-preview`, `vertex-gemini-3-flash-preview` с `Provider: AIProviderVertexAI` и `InternalDataGroundingCapability: true` (`ai/model_manager.go:368-424`).
3. `[ЕСТЬ]` `SelectModel()` фильтрует по `InternalDataGroundingCapability` в: preferred model fast path, main candidate loop, largest-model fallback, `SelectToolCallingModel`.

### P1.2 Подключить `VertexAIService` в runtime executor
1. `[ЕСТЬ]` `VertexAIService` инициализирован и зарегистрирован **первым** в `aiServices` (`main.go:294-321`), с `WithDataStore()` для Horizon datastore.
2. `[ЕСТЬ]` Routing по provider автоматический: `AITaskExecutor` ищет service по `capabilities.Provider`.

### P1.3 Перевести grounded-агенты на full result API
1. `[ЕСТЬ]` `ExecuteStructuredTaskFull` используется в 4 агентах:
   - `research_agent.go` → `EnrichFragmentWithGrounding` + `InternalDataGroundingCapability: true`
   - `intent_decomposer_agent.go` → `DecomposeWithGrounding` + `InternalDataGroundingCapability: true`
   - `strategic_overlay_agent.go` → `ExtractWithGrounding` + `InternalDataGroundingCapability: true`
   - `scout_agent.go` → `ExtractFragmentDataWithGrounding` + `InternalDataGroundingCapability: true`
2. `[ЕСТЬ]` `domain_resolver_agent.go` и `intent_resolver_agent.go` — pre-AI агенты, грounding не нужен (`SearchGroundingCapability: false`).
3. `[ЕСТЬ]` `GroundingEvidence` логируется через `logGroundingInfof` и сохраняется в `job.State` через `applyGroundingEvidenceToJobState()`.

## Phase 2. Grounding-aware materialization в Scout и Domain Answer Layer — ✅ ГОТОВО

### P2.1 Активировать grounding-aware relation path
1. `[ЕСТЬ]` `processDiscoveredRelationsWithGrounding(...)` вызывается в основном scout flow (`engine.go:3696`) и domain expansion (`engine_ext.go:2203`).
2. `[ЕСТЬ]` `GroundingEvidenceResolver.Resolve()` вызывается в `engine.go:3682` и `engine_ext.go:2199`.
3. `[ЕСТЬ]` `processRelationsInternal` grounding-aware ветка активируется при `internalFirstGroundingEnabled()`. `FindBestMatch()` вызывается в `engine_ext.go:3165`.

### P2.2 Интеграция с Domain Answer Layer
1. `[ЕСТЬ]` `RunInput` содержит `GroundingEvidence *ai.GroundingEvidence` (`domain/types.go:47`).
2. `[ЕСТЬ]` `ExecuteBaseCollect`/`ExecuteStrategicCollect` принимают `*ai.GroundingEvidence` (`domain/types.go:48-49`).
3. `[ЕСТЬ]` Gate L2 и Structured Narrative используют `ExecuteStructuredTaskFull` (`domain/engine.go:1541, 1843`). Grounding evidence применяется через `applyGroundingEvidenceToSnapshot()`.
4. `[ЕСТЬ]` `DomainAnswerSnapshot` содержит `OriginMode`, `InternalEvidence`, `ExternalEvidence`, `ResolutionStats` (`models/domain_answer_snapshot.go:157-160`).

### P2.3 Детальные reason-codes
1. `[ЕСТЬ]` `GroundingMatchReason` type с константами в `grounding_resolver.go:70-76`:
   - `GroundingMatchExactCanonical`
   - `GroundingMatchIdentifier`
   - `GroundingMatchLabel`
   - `GroundingNoMatchCreatedNew`
2. `[ЕСТЬ]` `FindBestMatch()` возвращает `(*HorizonFragment, GroundingMatchReason)` с приоритетом: canonical > identifier > label.

### P2.4 Привязка provenance к edge/fact
1. `[ЕСТЬ]` `applyGroundingProvenance()` в `grounding_runtime.go:271-292` — применяет provenance к `models.Provenance`.
2. `[ЕСТЬ]` `applyGroundingEvidenceToJobState()` вызывается в множественных точках: основной scout flow, domain expansion, strategic overlay, intent decomposition (`engine.go:2524,2769,4103`, `engine_ext.go:1784,2565,4664`).
3. `[ЕСТЬ]` Fragment и edge provenance: `engine.go:3599`, `engine_ext.go:3285,4410`.

## Phase 3. Расширение существующего internal-first на domain jobs — ✅ ГОТОВО

> **Контекст**: Phase A (PlanSources, fast-path facts, graph-backed context) теперь расширена на domain jobs.

### P3.1 Internal-first фаза для domain expansion job
`executeDomainExpansionJob()` (`engine_ext.go`) теперь включает internal-first фазу (gated by `internalFirstGroundingEnabled()`):
1. `[ЕСТЬ]` `PlanSources()` — `engine_ext.go:1487-1506`. Загрузка facts из Firestore → Vertex Search fallback.
2. `[ЕСТЬ]` `FactApplier.TryApplyFactsFastPath()` — `engine_ext.go:1515-1540`. При успешном fast-path AI-агент пропускается (`domainFastPathApplied`).
3. `[ЕСТЬ]` `buildGraphBackedInternalContext()` — `engine_ext.go:1509-1514`.
4. `[ЕСТЬ]` Internal context передаётся в `EnrichFragmentWithGrounding()` как `chainContext` (`engine_ext.go:1777`).

### P3.2 Internal-first фаза для domain strategic overlay job
`executeDomainStrategicOverlayJob()` (`engine_ext.go`) теперь включает internal-first фазу:
1. `[ЕСТЬ]` `PlanSources()` — `engine_ext.go:2487-2512`.
2. `[ЕСТЬ]` `buildGraphBackedInternalContext()` — `engine_ext.go:2509-2514`.
3. `[ЕСТЬ]` Internal context передаётся в `ExtractWithGrounding()`.
4. Примечание: `FactApplier` не используется в overlay — overlay извлекает decision points/mechanisms, а не применяет facts к фрагментам. Это корректное поведение.

### P3.3 Двухфазная стратегия
1. `[ЕСТЬ]` Phase A работает для основного Scout flow и domain jobs.
2. `[ЕСТЬ]` Domain expansion: при успешном fast-path AI-агент пропускается полностью.
3. `[ДОДЕЛАТЬ]` Phase B (явный gate "internal sufficient → skip external grounding" при вызове AI-агента) — нет explicit check. Internal context передаётся как primary, но external grounding остаётся доступным. В текущей архитектуре это допустимо — VertexAI mixed grounding приоритизирует internal datastore.

### P3.4 Completeness gate
> **DEFERRED**: Как и планировалось, конкретные пороги определяются на основе production данных.

### P3.5 Datastore scoping hardening
1. `[ЕСТЬ]` `horizonVertexClient()` используется консистентно: planner (construction-time), fact_applier, canonicalizer — все через horizon-scoped client.
2. `[ЕСТЬ]` Нет прямого использования unscoped Vertex client в scout package для Horizon retrieval.

## Phase 4. Risk pipeline hardening — ✅ ГОТОВО

### P4.1 `IDENTIFY` должен предпочитать fragmentId
`stageIdentify()` (`risk/pipeline.go:310-349`):
1. `[ЕСТЬ]` Проверка `job.State["targetFragmentId"]` (приоритет 1) и `job.State["fragmentId"]` (приоритет 2). Gated by `riskIdentifyFragmentIDEnabled()`.
2. `[ЕСТЬ]` Fallback `job.TargetCanonicalPath` → `normalizeRiskTargetPath()` → `GetByCanonicalPath()`.
3. Примечание: порядок приоритетов (`targetFragmentId` → `fragmentId`) отличается от исходного плана (`fragmentId` → `targetFragmentId`), но функционально эквивалентен — watchdog ставит `targetFragmentId`.

### P4.2 Preflight при создании RiskJob
1. `[ЕСТЬ]` `CreateRiskJob` (`risk_jobs.go:79-107`) — preflight: если `targetFragmentID` задан → `Get()` для валидации; если только path → `GetByCanonicalPath()` + заполнение `targetFragmentID`.
2. `[ЕСТЬ]` `targetFragmentId` записывается в `State` (`risk_jobs.go:154-156`).

### P4.3 Anti-cascade guard
1. `[ЕСТЬ]` `riskTriggeredScoutRefresh` state marker (`runtime_flags.go:20`).
2. `[ЕСТЬ]` Max depth=1: `stateInt(job.State, riskStateKeyScoutRefreshDepth) >= 1` → блокировка (`pipeline.go:947`).
3. `[ЕСТЬ]` Cooldown: `riskStateKeyScoutRefreshAtUTC` с дефолтом 180 мин, configurable через `HORIZON_RISK_SCOUT_REFRESH_COOLDOWN_MINUTES`.

## Phase 5. Наблюдаемость, SLO и trustworthiness — ⚠️ ПОЧТИ ГОТОВО

### P5.1 Метрики
Все 8 метрик реализованы (в `engine.go`, `engine_ext.go`, `pipeline.go`):
1. `[ЕСТЬ]` `horizon_grounding_origin_mode_total{mode=...}` (`engine.go:2527,2772`)
2. `[ЕСТЬ]` `horizon_grounding_relations_resolved_total{reason=...}` (`engine_ext.go:4544`)
3. `[ЕСТЬ]` `horizon_grounding_new_fragments_total{origin=...}` (`engine_ext.go:4553`)
4. `[ЕСТЬ]` `horizon_risk_identify_resolution_total{path|fragmentId|fallback}` (`pipeline.go:383`)
5. `[ЕСТЬ]` `horizon_dedup_false_positive_suspected_total` (`engine_ext.go:4558`)
6. `[ЕСТЬ]` `horizon_risk_scout_refresh_total{outcome=resolved|still_empty}` (`pipeline.go:402`)
7. `[ЕСТЬ]` `horizon_grounding_errors_total{component,phase,error_class,reason_code}` (`engine.go:1142`, `pipeline.go:1295`)
8. `[ЕСТЬ]` `horizon_grounding_best_effort_total{component,phase,reason_code}` (`engine.go:1149`, `pipeline.go:1302`)

### P5.2 Structured logs
1. `[ЕСТЬ]` `logGroundingInfof()` (`grounding_runtime.go:47-49`) — информационные логи с префиксом `HZN_GROUNDING`.
2. `[ЕСТЬ]` `logGroundingErrorWithContext()` (`grounding_runtime.go:55-93`) — логи ошибок с полями: `component`, `phase`, `jobId`, `tenantId`, `projectId`, `targetCanonicalPath`, `originMode`, `reasonCode`, `errorClass`, `best_effort`.

### P5.3 No-silent-fail policy
1. `[ЕСТЬ]` `recordGroundingErrorState()` (`grounding_runtime.go:95-115`) — сохраняет в `job.State`: `lastErrorPhase`, `lastErrorReasonCode`, `lastErrorAtUtc`, `lastErrorClass`, `groundingBestEffortCount`.
2. `[ЕСТЬ]` State key константы: `stateKeyLastErrorPhase`, `stateKeyLastErrorReasonCode`, `stateKeyLastErrorAtUTC`, `stateKeyLastErrorClass`, `stateKeyGroundingBestEffortN` (`grounding_runtime.go:28-32`).

### P5.4 Dashboards/alerts
Нет существующих dashboard/alert конфигов для grounding:
1. `[С НУЛЯ]` Alert на резкий рост `external_only`.
2. `[С НУЛЯ]` Alert на рост `createdNew` при высоком `HasDatastoreResults`.
3. `[С НУЛЯ]` Alert на `risk IDENTIFY not found` rate.
4. `[С НУЛЯ]` Alert на `scout_refresh` rate (anti-cascade health check).
5. `[С НУЛЯ]` Alert на `horizon_grounding_errors_total` (спайк по `component/phase`).
6. `[С НУЛЯ]` Alert на `horizon_grounding_best_effort_total` (аномальный рост как индикатор деградации).

## 7. Тестовый план

### 7.1 Unit
1. Grounding resolver match precedence: canonical > identifier > label.
2. Relation materialization с `groundingCtx` не создаёт дубль при существующем fragment.
3. Risk IDENTIFY resolution order (fragmentId-first).
4. Model selection: `InternalDataGroundingCapability=true` → выбирает `vertex-*` модель, routing на `AIProviderVertexAI`.
5. Anti-cascade: RiskJob с `riskTriggeredScoutRefresh=true` → Scout не создаёт дочерних RiskJob.
6. Domain engine: grounding context протекает через `ExecuteBaseCollect` → materialization.
7. Domain expansion job: вызывает `PlanSources()` / fast-path перед AI-агентом.
8. Domain strategic overlay job: загружает internal context перед `StrategicOverlayAgent.Extract()`.
9. Log contract: все ошибки в grounding critical path пишутся с префиксом `HZN_GROUNDING` и обязательными полями (`component`, `phase`, `reasonCode`).
10. No-silent-fail: в critical path отсутствуют ветки, где `error` теряется без структурного лога/метрики.

### 7.2 Integration
1. Existing entity in datastore + внешняя подсказка с другим названием -> должен выбрать существующий fragment.
2. Mixed grounding (internal+web) -> корректный `originMode=mixed`.
3. Path drift (`old canonicalPath`, valid fragmentId) -> Risk job продолжает pipeline.
4. Domain contract run → grounding evidence → snapshot с provenance.
5. Ошибка в любой фазе grounding/risk фиксируется сквозным логом с `HZN_GROUNDING` и диагностируется до `component + phase + reasonCode` за один запрос в логах.

### 7.3 E2E / Replay
1. Прогон на исторических кейсах с false positives.
2. Сравнение baseline vs after:
- duplicate creation rate,
- grounding match rate,
- risk identify failure rate.

## 8. Rollout план

### 8.1 Этапы
1. Canary tenant/project (5-10% нагрузки).
2. Limited production rollout (25%).
3. Full rollout (100%) после 48h стабильности.

### 8.2 Rollback
1. Выключение `HORIZON_GROUNDING_INTERNAL_FIRST`.
2. Возврат к старому path (без groundingCtx) через feature flag.
3. Сохранение telemetry для post-mortem.

## 9. Риски и меры
1. Риск: неверный match по label.
- Мера: приоритет canonical/id выше label; confidence guard и reason-codes.
2. Риск: рост latency из-за доп. resolution шага.
- Мера: батч-загрузка `GetMany`, лимиты и таймауты. Конкретные бюджеты определяются на этапе реализации.
3. Риск: неполные/грязные provenance поля в legacy jobs.
- Мера: optional поля + fail-soft + метрика качества заполнения.
4. Риск: рассинхрон datastore scopes.
- Мера: централизованный helper и smoke-тест на env-конфиг.
5. Риск: каскадный цикл Risk → Scout → Risk.
- Мера: anti-cascade guard с depth=1 + state marker + cooldown.

## 10. Definition of Done
1. В Scout >= 80% кейсов с доступными внутренними данными отрабатывают как `internal_only` или `mixed` без внешнего create-дубля.
2. Duplicate creation rate снижен минимум на 50% на контрольной выборке.
3. Для grounded Scout jobs в state/frame есть прозрачный provenance (`originMode`, internal/external evidence).
4. `Risk IDENTIFY` использует fragmentId-first и снижает `not found` минимум на 70% для drift-кейсов.
5. Все новые метрики и алерты включены и наблюдаемы в production.
6. Internal-grounded задачи маршрутизируются через VertexAI provider автоматически.
7. Domain Answer Snapshots включают корректный provenance.
8. Отсутствуют каскадные циклы Risk ↔ Scout (подтверждено тестами).
9. Для grounding/risk critical path нет silent исключений: любая ошибка имеет structured log с префиксом `HZN_GROUNDING`, reason-code и метрику.

## 11. Ссылки на затронутые зоны кода
1. `backend/cmd/server/main.go` — VertexAIService wiring
2. `backend/internal/ai/task_executor.go` — ExecuteStructuredTaskFull routing
3. `backend/internal/ai/vertex_ai_service.go` — mixed grounding implementation
4. `backend/internal/ai/model_manager.go` — InternalDataGroundingCapability, VertexAI model entries, SelectModel
5. `backend/internal/ai/models.go` — AIModelCapabilities, AIRequirements, GroundingEvidence
6. `backend/internal/horizon/scout/agents/research_agent.go` — EnrichFragmentWithGrounding
7. `backend/internal/horizon/scout/agents/intent_decomposer_agent.go` — DecomposeWithGrounding
8. `backend/internal/horizon/scout/agents/strategic_overlay_agent.go` — ExtractWithGrounding
9. `backend/internal/horizon/scout/agents/scout_agent.go` — ExtractFragmentDataWithGrounding
10. `backend/internal/horizon/scout/grounding_resolver.go` — GroundingEvidenceResolver, FindBestMatch, GroundingMatchReason
11. `backend/internal/horizon/scout/grounding_runtime.go` — feature flags, provenance functions, structured logging, error state
12. `backend/internal/horizon/scout/engine.go` — processDiscoveredRelationsWithGrounding wiring, fast-path facts, graph-backed context
13. `backend/internal/horizon/scout/engine_ext.go` — domain expansion/overlay internal-first, processDiscoveredRelationsWithGrounding
14. `backend/internal/horizon/scout/domain_contract_runner.go` — grounding context passthrough to domain engine
15. `backend/internal/horizon/scout/planner.go` — PlanSources (Firestore facts → Vertex Search fallback)
16. `backend/internal/horizon/scout/fact_applier.go` — fast-path facts application, relevance scoring
17. `backend/internal/horizon/scout/vertex_helpers.go` — horizonVertexClient() helper
18. `backend/internal/horizon/scout/canonicalizer.go` — fragment lookup via Vertex Search
19. `backend/internal/horizon/domain/engine.go` — grounding context in RunInput, provenance in snapshot
20. `backend/internal/horizon/domain/types.go` — RunInput with GroundingEvidence
21. `backend/internal/horizon/models/common.go` — Provenance struct with grounding fields
22. `backend/internal/horizon/models/domain_answer_snapshot.go` — DomainAnswerSnapshot grounding fields
23. `backend/internal/horizon/risk/pipeline.go` — IDENTIFY fragmentId-first, anti-cascade, metrics
24. `backend/internal/horizon/risk/runtime_flags.go` — risk feature flags, cooldown, cascade markers
25. `backend/internal/http/horizon/risk_jobs.go` — preflight fragmentId resolve
