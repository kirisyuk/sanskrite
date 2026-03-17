# Roadmap реализации системы мониторинга горизонта

## Обзор

Данный документ описывает план реализации системы мониторинга горизонта по этапам. Реализация разбита на логические этапы, каждый из которых добавляет новую функциональность и может быть развернут независимо.

## Статус реализации (сводка)

| Компонент | Статус | Комментарий |
|-----------|--------|--------------|
| expand-materials | ✅ | POST `/api/horizon/fragments/{id}/expand-materials` |
| highProfile (global) | ✅ | `models/high_profile.go`, `highprofile/calculator.go`, `horizon_backfill_high_profile` |
| marketSegments, orgSize | ✅ | `models/fragment.go`, Scout/Critic/Research, `segment_market_links.go`, `horizon_llm_backfill_org_segments_size` |
| AUDITED_BY, RELATIVE_OF, RELATED_TO | ✅ | taxonomy, edge_normalizer, промпты Scout/Critic/Research |
| DEPOSIT, EXTRACTS_FROM | ✅ | `FragmentTypeDeposit`, validation_policy, edge_normalizer, supply_chain_parser |
| edgeModality, ValidFrom/ValidTo | ✅ | qualifiers, `normalizeTemporalModality`, policy_checker |
| QueryAddress | ✅ | `query_address.go`, `query_resolver.go`, `query_refresh_job.go` |
| Policy infrastructure | ✅ | Policy interface, PolicyExecutor, Firestore collections |
| MaterializedArtifact, SupplyLink | ✅ | core-model/runtime готовы; API endpoints для SupplyLink/paths — в TODO (см. секцию F) |
| Ручной scouting клиентов организации | ✅ | через `POST /api/horizon/fragments/{id}/refresh` + `specialties=["supply_chain"]` |
| Appetite governance (override + audit trail) | ◑ | runtime appetite есть; analyst override endpoint и audit trail — в TODO |
| Interpretation queue hardening (DLQ/retry/SLO) | ❌ | базовая очередь есть, hardening ещё не завершён |
| Lens-specific highProfile | ❌ | отложено до полноценного дизайна |
| Метрики в оверлей | ❌ | метрики пока в `State` фрагментов |
| Org identity anchor & transitions | ❌ | identity anchor, nameHistory, transition edges (RENAMED_TO, LEGAL_SUCCESSOR_OF…); см. `ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md` |
| Scout domain phases & composable agents | ✅ | Domain modes (intent→job.State), allowlists, semantic groups; CriticPipeline/ResearchPipeline by mode; Phase 0–3 in engine; domain mode in child jobs; canonical edge types (ASSOCIATED_WITH, CORPORATE_TRANSITION, DOMAIN_RELATION). Toggle: `HORIZON_SCOUT_COMPOSABLE_PIPELINE=false` для отката на монолитный путь. ТЗ: `SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ.md`. |
| Signaling Pipeline | ❌ | Отдельный пайплайн: news flow → events → 12-axis signals → propagation. Scout не создаёт сигналы. План: `SIGNALING_PIPELINE_IMPLEMENTATION_PLAN.md`. |
| Fractal Hierarchy & Scale-Invariant Contracts | ◑ | Sphere taxonomy (L1/L2/L3+), SpherePaths, ResolutionHint, ParentPath — реализованы. Zoom, Metric Composition, Edge Projection — отложены. Units deprecated. Chokepoint transit amplification — реализовано. |

## Unified Remediation Plan (единый всеобъемлющий план, 2026-03)

Этот раздел фиксирует один объединенный план исправлений. Он заменяет разрозненные списки "12 пунктов" и "8 пунктов": это не два разных плана, а одна программа с общим порядком исполнения.

Цель программы: устранить корневые причины ошибок в онтологической материализации, ложных остановок исследования, дефицита глубины/ширины evidence и преждевременного `ready_with_gaps`, сохранив управляемость стоимости и откатываемость.

### Принципы исполнения

1. Причина-first: сначала устраняем архитектурную причину, потом симптом.
2. Hard-stop и soft-stop разведены явно; soft-stop не может завершать исследование без fallback-прохода.
3. Один source of truth для direction/allowed triples.
4. Fail-open допустим для discovery, но fail-closed для decision-grade выводов при P0/P1 gaps.
5. Каждый шаг: код -> тесты -> replay -> canary -> rollout.

### Workstream 0. Baseline и инвентаризация причин остановок

Проблема: причины остановок и деградаций наблюдаются как "шум", но не как формализованный реестр первопричин.

Исправление:
1. Ввести единый реестр stop-причин с классификацией `hard_stop|soft_stop|recoverable`.
2. Нормализовать reason codes по стадиям pipeline, чтобы одна и та же причина не логировалась разными кодами.
3. Собрать baseline по 7 дням до изменений (по contractType и depth tiers).

Файлы:
1. `backend/internal/horizon/scout/engine.go`
2. `backend/internal/horizon/scout/post_processing_policy.go`
3. `backend/internal/horizon/domain/engine.go`

DoD:
1. Любой stop в runtime имеет нормализованный reason code.
2. Доступна baseline-таблица "причина -> частота -> доля провалов gate".

### Workstream 1. Единый онтологический контракт (USOT)

Проблема: direction/edge contract размазан между prompt, canonicalization, policy и runtime-sanitizer.

Корень:
1. Разные источники правды для допустимых source/target пар.
2. Lossy нормализация (например, принудительное сведение в `FOCUSES_ON`) конфликтует с direction policy.

Исправление:
1. Зафиксировать `edgesemantics` как USOT для валидных направлений.
2. Удалить/ограничить lossy rewriting, которое разрушает валидные стратегические связи.
3. Привести policy canonicalization к строгому подчинению USOT.

Файлы:
1. `backend/internal/horizon/edgesemantics/direction_policy.go`
2. `backend/internal/horizon/models/edge_canonicalization.go`
3. `backend/internal/horizon/policy/edge_canonicalization_policy.go`
4. `backend/internal/horizon/scout/engine_ext.go`

Тесты:
1. Табличные tests по всем strategic/domain edge types.
2. Отдельный regression на `DOMAIN -> MECHANISM|DECISION_POINT`.

DoD:
1. Нулевая рассинхронизация allowlist/direction между слоями.
2. Системные quarantine по валидным triple исчезают.

### Workstream 2. Удаление двойного контура генерации отношений

Проблема: часть путей идет через mode-aware стратегии, часть через general/monolith, что создает конфликт контрактов.

Исправление:
1. Все domain expansion и strategic overlay перевести на `EnrichFragmentWithGroundingWithDomainModes`.
2. Запретить скрытый fallback в general mode для mode-specific задач.
3. Добавить явное логирование выбранного mode и prompt family.

Файлы:
1. `backend/internal/horizon/scout/engine_ext.go`
2. `backend/internal/horizon/scout/agents/research_agent.go`
3. `backend/internal/horizon/scout/research_orchestrator.go`

DoD:
1. Для mode-specific job в логах нет запуска general monolith.
2. Снижение direction-invalid edges на affected-zone/policy-impact.

### Workstream 3. Синхронизация prompt-словарей relation types

Проблема: `DomainMode` строки и фактические prompt allowlists расходятся.

Исправление:
1. Генерировать prompt allowlist из единого декларативного источника.
2. Убрать ручные разрозненные списки relation types в стратегиях.
3. Добавить compile-time/CI проверку синхронности списков.

Файлы:
1. `backend/internal/taxonomy/domain_mode.go`
2. `backend/internal/horizon/scout/agents/research_strategies_impl.go`
3. `backend/internal/horizon/scout/agents/research_agent.go`

DoD:
1. Один mode всегда дает один и тот же набор edge types.
2. CI падает при drift списков.

### Workstream 4. False-stop elimination (глубинные причины ложных остановок)

Проблема: исследование преждевременно останавливается по эвристическим причинам, которые не являются бизнес-окончанием.

Корень:
1. `tier`-гейтинг source planning на depth>0.
2. Freshness skip без учета незакрытых критических checks.
3. `no_searchable_gaps` трактуется как конец работы.
4. Cooldown/exhaustion маскируют неразрешенные P0/P1.
5. `LastVerifiedAt` используется как признак "свежести" даже для low-confidence/stub узлов.
6. Tier-2 fast path создает stub и завершает run без попытки доглубления.
7. MissingInfo status обновляется агрегатно (`foundSomething`), что может ложноположительно "закрывать" все вопросы.
8. Reset exhausted по новым источникам фактически не срабатывает (нет вызовов + пустой fingerprint).
9. Domain enrichment цикл не ожидает завершения child enrichment jobs (enqueue -> immediate collect).
10. Domain graph collection жестко ограничен `limit=4000` без пагинации (ложный "нет прогресса").
11. Preflight `budget_exhausted_partial` и circuit breaker работают как безусловный hard-stop без проверки recoverability.

Исправление:
1. Ввести обязательный fallback-проход для любого soft-stop.
2. Разрешать freshness-skip только при закрытых P0/P1 и достаточном evidence coverage.
3. Заменить бинарный запрет source planning на risk-aware policy.
4. При `no_searchable_gaps` запускать deterministic question seeding по типу сущности.
5. При cooldown/exhaustion формировать `research_debt_queue` на доглубление.
6. Развести `verified_fresh` и `recently_touched`: freshness учитывать только для `EpistemicStatus=verified`.
7. Убрать автоприсвоение `LastVerifiedAt` на create для low-confidence/stub либо в freshness-чеке игнорировать такие узлы.
8. Для tier-2 fast path: вместо финализации ставить `deferred_deep_research` task при критичных контрактах.
9. Перевести учет missingInfo с агрегатного на per-question/per-evidence outcome (resolved/failed частично, а не массово).
10. Включить `ResetForNewSources` в runtime и заполнять/обновлять `SourceFingerprint` при каждом исследовании.
11. Для DomainEngine добавить await/poll child job completion перед `collectGraph` в enrichment cycle.
12. Добавить пагинацию `collectGraph` (artifact pages) и явный флаг truncation в gate checks.
13. Для budget/circuit-breaker: проверять recoverability и разрешать ограниченный recovery-pass до hard-stop.

Файлы:
1. `backend/internal/horizon/scout/post_processing_policy.go`
2. `backend/internal/horizon/scout/engine.go`
3. `backend/internal/horizon/scout/agents/critic_agent.go`
4. `backend/internal/horizon/scout/scout_outbox.go`
5. `backend/internal/horizon/repo/fragments.go`
6. `backend/internal/horizon/repo/missing_info_status_repo.go`
7. `backend/internal/horizon/models/missing_info_status.go`
8. `backend/internal/horizon/domain/engine.go`

Тесты:
1. Scenario tests: soft-stop не завершает pipeline без fallback.
2. P0/P1 unresolved + fresh data -> исследование продолжается.
3. Low-confidence stub не может проходить `fresh_no_new_facts` short-circuit.
4. MissingInfo: частичный успех не закрывает все вопросы; exhausted reset работает при новом fingerprint.
5. Domain enrichment: после enqueue cycle дожидается фактического прироста графа или фиксирует timeout reason.
6. collectGraph pagination: truncation детектируется и не маскируется под "полноту недостаточно".

DoD:
1. Soft-stop больше не приводит к silent terminate.
2. Доля `ready_with_gaps` по soft-stop причинам снижается до целевого уровня.
3. Причины `fresh_no_new_facts` и `research_skipped_no_searchable_gaps` больше не встречаются при незакрытых P0/P1.
4. Для каждого false-stop reason code есть deterministic recovery path.

### Workstream 5. Управление глубиной и шириной исследований

Проблема: ширина и глубина ограничиваются внутренними лимитами, а не бизнес-контрактом.

Исправление:
1. Ввести per-contract минимумы: actors, geo coverage, mechanisms, independent sources.
2. При недоборе автоматически включать broadening pass.
3. Ввести depth budget не как фиксированное число, а как адаптивный budget от unresolved critical checks.

Файлы:
1. `backend/internal/horizon/domain/gate_config.go`
2. `backend/internal/horizon/domain/engine.go`
3. `backend/internal/horizon/scout/planner.go`

DoD:
1. Каждый contractType достигает минимальной требуемой ширины.
2. Недобор ширины фиксируется как причина enrichment, а не как "готово".

### Workstream 6. Cycle governance и запрет преждевременного `ready_with_gaps`

Проблема: при лимите циклов система может публиковать `ready_with_gaps` с незакрытыми критическими проверками.

Исправление:
1. При наличии P0/P1 вводится mandatory recovery cycle (минимум один доп. цикл).
2. `ready_with_gaps` допускается только при выполнении строгого guard condition.
3. Явно логировать effective cycle budget (base + adaptive + mandatory recovery).

Файлы:
1. `backend/internal/horizon/domain/engine.go`
2. `backend/internal/horizon/domain/gate_config.go`

DoD:
1. `ready_with_gaps` не публикуется для необработанных recoverable P0/P1.
2. Решение о fallback полностью трассируемо.

### Workstream 7. CanonicalParts completeness и geo-token contract

Проблема: `canonicalParts present but incomplete` системно повторяется для `zone/domain`.

Исправление:
1. Добавить parse/build поддержку `/zone/...` и `/domain/...`.
2. Зафиксировать `sea` как валидный geo token для соответствующих сценариев.
3. Выполнить backfill существующих фрагментов.

Файлы:
1. `backend/internal/taxonomy/canonical_parts.go`
2. `backend/internal/taxonomy/paths.go`
3. `backend/internal/horizon/repo/fragments.go`

DoD:
1. Warning по incomplete canonicalParts для zone/domain стремится к нулю.
2. Ошибка `invalid geo sea` отсутствует.

### Workstream 8. ISO2 remediation без unsafe auto-fix

Проблема: `fix_iso2` зависит от LLM confidence и часто отклоняется при слабом контексте.

Исправление:
1. Добавить deterministic pre-resolver (homeLocation, edge-derived geo hints, alias dictionary).
2. LLM оставить как fallback, не как первый шаг.
3. Расширить evidence-пакет для inference без снижения precision guard.

Файлы:
1. `backend/internal/horizon/policy/enforcement_executor.go`

DoD:
1. Частота `low_confidence_iso2_inference` снижается, без роста ошибочных автофиксов.

### Workstream 9. Разделение physical supply и financial flow

Проблема: parser может материализовать `SUPPLIES_TO` в финансовом контексте.

Исправление:
1. Явно ввести `flow_kind` в parser contract: `physical|financial|capital`.
2. Для `financial|capital` запретить materialization в `SUPPLIES_TO`.
3. Добавить map в соответствующие финансовые relation types.

Файлы:
1. `backend/internal/horizon/scout/supply_chain_parser.go`
2. `backend/internal/horizon/scout/agents/research_strategies_impl.go`

DoD:
1. Ложные `SUPPLIES_TO` в banking/investment кейсах устранены.

### Workstream 10. Directional role evidence contract hardening

Проблема: частичные/неканоничные role qualifiers приводят к quarantine при формально заданных ролях.

Исправление:
1. Унифицировать canonical role vocabulary между autofill и validation.
2. Добавить pre-normalization role qualifiers перед directional validation.
3. Добавить explicit reject-reasons для диагностики pair mismatch.

Файлы:
1. `backend/internal/horizon/scout/edge_directional_roles.go`
2. `backend/internal/horizon/policy/validation_policy.go`

DoD:
1. Плавающие allow/quarantine по одинаковым relation cases исчезают.

### Workstream 11. Test matrix, replay и shadow validation

Проблема: без replay легко получить скрытые регрессии.

Исправление:
1. Зафиксировать golden log suite по affected-zone, policy-impact, sector-overview.
2. Запустить counterfactual replay: "что было бы без soft-stop".
3. Shadow-режим на production трафике до canary.

DoD:
1. Любой критический регресс ловится до rollout.
2. Отчет replay подтверждает сокращение ложных остановок.

### Workstream 12. Rollout, guardrails и эксплуатационная фиксация нормы

Исправление:
1. Feature flags для рискованных изменений (USOT rewrite, stop-policy, flow-kind parser).
2. Canary 10% -> 50% -> 100% с авто-rollback при деградации.
3. Обновить OK-правила: что является бизнес-нормой, а что инцидентом.

Acceptance criteria программы:
1. `quarantined_edge_rate` по affected-zone/policy-impact снижен минимум на 50%.
2. `ready_with_gaps` с незакрытыми recoverable P0/P1 исключен.
3. `soft_stop_recovered_rate` достигает целевого порога (вводится как release gate).
4. `invalid geo sea` = 0.
5. `canonicalParts incomplete (zone/domain)` -> near-zero.
6. `low_confidence_iso2_inference` снижена до допустимого уровня без роста false-fix.

### Порядок выполнения (dependency-safe sequence)

1. WS0 -> WS1 -> WS2 -> WS3.
2. WS4 -> WS5 -> WS6.
3. WS7 -> WS8 -> WS9 -> WS10.
4. WS11 -> WS12.

Release gate:
1. Следующий этап разрешается только после выполнения DoD предыдущего этапа и прохождения replay/shadow checks.

### Детализация исполнения (обязательная надстройка к WS0-WS12)

Этот блок задает точный backlog исполнения с идентификаторами задач. Любая реализация в PR должна ссылаться на `Task ID`.

#### 1) Реестр корневых причин (Root Cause Register)

| RC ID | Корневая причина | Где проявляется | Текущий эффект | Целевое исправление |
|------|-------------------|-----------------|----------------|---------------------|
| RC-FS-01 | Tier-gating отключает planning/research на depth>0 | Scout policy/runtime | Ложные stop на дочерних job | Risk-aware gating + soft-stop fallback |
| RC-FS-02 | Freshness опирается на `LastVerifiedAt` для low-confidence/stub | Scout preflight | `fresh_no_new_facts` при незакрытых критичных gaps | Freshness only for verified-quality nodes |
| RC-FS-03 | Tier2 fast path финализирует run после stub | Scout tier2 path | Глубина исследования “обрывается” | Deferred deep-research scheduling |
| RC-FS-04 | `no_searchable_gaps` трактуется как terminal skip | Scout research stage | Преждевременный skip исследования | Deterministic reseeding + debt queue |
| RC-FS-05 | MissingInfo attempts пишутся агрегатно (`foundSomething`) | MissingInfo status lifecycle | Ложное "resolved" для части вопросов | Per-question outcome accounting |
| RC-FS-06 | Exhausted reset по новым источникам не работает в runtime | MissingInfo repo/runtime | “Залипшие” exhausted вопросы | Fingerprint update + runtime reset hook |
| RC-FS-07 | DomainEngine не дожидается child enrichment jobs | Domain enrichment cycle | Мнимый “нет прогресса” в цикле | Await/poll + timeout reasoning |
| RC-FS-08 | Graph collect limit=4000 без пагинации | Domain graph collection | Усеченная картина и ложные gaps | Pagination + truncation signal |
| RC-FS-09 | Budget/circuit-breaker hard-stop без recoverability-pass | Scout preflight | Ранний terminal outcome | Recovery pass before hard-stop |
| RC-FS-10 | Разрозненные reason codes остановок | Logs/telemetry | Невозможность точной диагностики | Unified stop reason taxonomy |

#### 2) Пошаговый backlog (Task-level plan)

##### Статус исполнения (факт на 2026-03-01)

| Task ID | Статус | Что реализовано |
|--------|--------|-----------------|
| TASK-S0-01 | ✅ | Введена унифицированная stop-taxonomy (`reason_code`, `stop_kind`, `recoverable`) в stage runtime и terminal outcome. |
| TASK-S0-02 | ✅ | В telemetry добавлены обязательные поля `critical_open_count`, `evidence_coverage`, `domain_mode`, `pipeline_tier`, `recursion_depth`. |
| TASK-S0-03 | ✅ | Добавлен runtime contract-assert: soft/skip terminal при `critical_open_count>0` помечается как pipeline issue и переводится в partial outcome. |
| TASK-S1-01 | ✅ | Добавлен risk-aware `CanSkipPlanning()` (учет `critical_open_count` + `evidence_coverage`), заменен бинарный tier-skip planning. |
| TASK-S1-02 | ✅ | Freshness skip ограничен verified-фрагментами (`EpistemicStatus=VERIFIED`). |
| TASK-S1-03 | ✅ | Убрана автопростановка `Metadata.LastVerifiedAt` при `fragments.Create` для low-confidence. |
| TASK-S2-01 | ✅ | MissingInfo attempts пишутся per-question (не агрегатный `foundSomething`). |
| TASK-S2-02 | ✅ Частично | Добавлен deterministic probe mode при `FilterSearchable -> empty` и новом evidence fingerprint. |
| TASK-S2-03 | ✅ | В lifecycle attempts передается и сохраняется `sourceFingerprint`. |
| TASK-S2-04 | ✅ | Runtime вызывает `ResetForNewSources` до финального skip вопросов. |
| TASK-S2-05 | ✅ | Реализована `research_debt_queue` через scout outbox intent `research_debt_queue` и enqueue при `no_searchable_gaps`. |
| TASK-S3-01 | ✅ | В DomainEngine добавлен bounded await/poll (`awaitGraphDelta`) после enqueue enrichment. |
| TASK-S3-02 | ✅ | Timeout/cancel await enrichment маркируется как `partial_wait_timeout` в runtime snapshot/gate summary. |
| TASK-S3-03 | ✅ | `collectGraph` переведен на пагинацию (`ListByLensPaginated`) с cap и сигналом truncation. |
| TASK-S3-04 | ✅ | `graph_truncated=true` добавлен в `DomainGateSummary`; при truncation форсируется enrichment task `artifact_coverage_expand`. |
| TASK-S1-04 | ✅ Частично | Для high-risk canonical paths tier2 fast path bypassed (вместо stub-only terminal выполняется full pipeline). |
| TASK-S1-05 | ✅ Частично | Добавлен recoverability-pass: `budget_exhausted_partial` переводится в soft-stop при recovery сигналах; circuit-breaker окно расширяется до 7 попыток. |
| TASK-S3-05 | ✅ | Добавлен publish guard: при recoverable critical gaps (`graph_truncated`/`partial_wait_timeout`) `publish.completed` блокируется и заменяется `publish.blocked`. |

Кодовые изменения этого этапа:
1. `backend/internal/horizon/scout/post_processing_policy.go`
2. `backend/internal/horizon/scout/engine.go`
3. `backend/internal/horizon/scout/engine_ext.go`
4. `backend/internal/horizon/repo/fragments.go`
5. `backend/internal/horizon/repo/missing_info_status_repo.go`
6. `backend/internal/horizon/models/missing_info_status.go`
7. `backend/internal/horizon/domain/engine.go`

##### Sprint A (S0): Инструментация причин остановок

1. `TASK-S0-01`: Ввести единый enum reason codes для stop/skip/fallback.
2. `TASK-S0-02`: Добавить в telemetry обязательные поля: `stop_kind`, `recoverable`, `critical_open_count`, `evidence_coverage`.
3. `TASK-S0-03`: Добавить runtime-assert: terminal skip при `critical_open_count>0` логируется как ошибка контракта.

Изменяемые файлы:
1. `backend/internal/horizon/scout/engine.go`
2. `backend/internal/horizon/scout/post_processing_policy.go`
3. `backend/internal/horizon/domain/engine.go`

Acceptance:
1. Все stop/skip события имеют нормализованный `reason_code`.
2. Нет `reason_code=unknown` на production replay.

##### Sprint B (S1): Устранение false-stop в Scout preflight/runtime

1. `TASK-S1-01` (RC-FS-01): заменить бинарный `PipelineTier` gating на policy-функцию `CanSkipPlanning(...)` с входами `domainMode`, `critical_open_count`, `evidence_coverage`.
2. `TASK-S1-02` (RC-FS-02): freshness skip разрешать только при `EpistemicStatus=verified` и `critical_open_count=0`.
3. `TASK-S1-03` (RC-FS-02): убрать влияние автоматически проставленного `LastVerifiedAt` для low-confidence/stub в freshness branch.
4. `TASK-S1-04` (RC-FS-03): в tier2 path заменить terminal finalize на enqueue `deferred_deep_research` при high-risk contract types.
5. `TASK-S1-05` (RC-FS-09): добавить `recoverability_check` перед `budget_exhausted_partial` и `circuit_breaker` hard-stop.

Изменяемые файлы:
1. `backend/internal/horizon/scout/post_processing_policy.go`
2. `backend/internal/horizon/scout/engine.go`
3. `backend/internal/horizon/repo/fragments.go`

Acceptance:
1. `fresh_no_new_facts` не встречается, если есть P0/P1 gaps.
2. Tier2 jobs для high-risk контрактов больше не теряются после stub.

##### Sprint C (S2): MissingInfo lifecycle correctness

1. `TASK-S2-01` (RC-FS-05): заменить агрегатный `foundSomething` на per-question resolution map.
2. `TASK-S2-02` (RC-FS-04): если после `FilterSearchable` список пуст, запускать `deterministic_gap_reseed`.
3. `TASK-S2-03` (RC-FS-06): сохранять и обновлять `SourceFingerprint` в конце каждого research цикла.
4. `TASK-S2-04` (RC-FS-06): вызывать `ResetForNewSources` перед окончательным skip exhausted вопросов.
5. `TASK-S2-05`: добавить `research_debt_queue` запись для каждого filtered-out critical gap.

Изменяемые файлы:
1. `backend/internal/horizon/scout/engine.go`
2. `backend/internal/horizon/repo/missing_info_status_repo.go`
3. `backend/internal/horizon/models/missing_info_status.go`
4. `backend/internal/horizon/scout/scout_outbox.go`

Acceptance:
1. Нет массового закрытия missingInfo одним успешным relation batch.
2. Exhausted вопросы возобновляются при новом source fingerprint.

##### Sprint D (S3): DomainEngine cycle reliability

1. `TASK-S3-01` (RC-FS-07): после `enqueueEnrichmentTasks` ввести `awaitEnrichmentCompletion` с таймаутом и лимитом retries.
2. `TASK-S3-02` (RC-FS-07): если child jobs не завершены вовремя, помечать gate как `partial_wait_timeout`, а не `ready_with_gaps`.
3. `TASK-S3-03` (RC-FS-08): заменить `ListByLens(..., 4000)` на постраничный сбор артефактов до configured cap.
4. `TASK-S3-04` (RC-FS-08): добавить `graph_truncated=true` в gate summary и принудительный enrichment task `artifact_coverage_expand`.
5. `TASK-S3-05`: ограничить публикацию `ready_with_gaps` при наличии recoverable truncation/timeout причин.

Изменяемые файлы:
1. `backend/internal/horizon/domain/engine.go`
2. `backend/internal/horizon/domain/gate_config.go`

Acceptance:
1. Domain cycles фиксируют фактический прогресс, а не “нулевой” из-за async race.
2. Усечение графа не маскируется под business gap.

##### Sprint E (S4): USOT + prompt drift cleanup (стабилизация направления)

1. `TASK-S4-01`: синхронизировать strategy prompts с USOT relation contracts.
2. `TASK-S4-02`: убрать конфликтующие lossy rewrites, нарушающие direction policy.
3. `TASK-S4-03`: добавить CI check на согласованность edge allowlists.

Статус (2026-03-01):
1. `TASK-S4-01` выполнен: relation lists в Research/Critic prompt-ах переведены на USOT (`edgesemantics` allowlists/domain scaffold contract), удалены hardcoded mode lists.
2. `TASK-S4-02` выполнен: domain canonicalization больше не схлопывает валидные relation types в `FOCUSES_ON`; для `DOMAIN -> MECHANISM|DECISION_POINT` fallback теперь `SURFACES`.
3. `TASK-S4-03` выполнен: добавлены CI tests на drift prompt relation sets vs USOT allowlists (`backend/internal/taxonomy/domain_mode_prompt_contract_test.go` + tests в `scout/agents` и `edgesemantics`).

Изменяемые файлы:
1. `backend/internal/horizon/edgesemantics/direction_policy.go`
2. `backend/internal/horizon/policy/edge_canonicalization_policy.go`
3. `backend/internal/horizon/scout/agents/research_strategies_impl.go`
4. `backend/internal/taxonomy/domain_mode.go`

Acceptance:
1. Снижение quarantine по direction-contract mismatch.

##### Sprint F (S5): Canary, shadow, rollout

1. `TASK-S5-01`: запустить shadow replay на historical logs (affected-zone/policy-impact/sector-overview).
2. `TASK-S5-02`: canary 10% -> 50% -> 100% с auto-rollback policy.
3. `TASK-S5-03`: зафиксировать обновленные OK-rules и SLO.

Статус (2026-03-01):
1. `TASK-S5-01` выполнен: добавлен `scripts/horizon_shadow_replay.sh` (лог-анализ + release gates + JSON report).
2. `TASK-S5-02` выполнен: добавлен `scripts/horizon_canary_rollout.sh` (10/50/100 phases + gate checks + auto-rollback).
3. `TASK-S5-03` выполнен: зафиксирован runbook `docs/horizon/implementation/SHADOW_CANARY_ROLLOUT.md` с OK-rules/SLO gate-порогами и операционной процедурой.

Acceptance:
1. Все release gates выполнены (см. секцию ниже).

#### 3) Детальная тест-матрица (обязательная)

| Test ID | Сценарий | Проверка | Ожидаемый результат |
|--------|----------|----------|---------------------|
| T-FS-01 | Fresh node + unresolved P0/P1 | Skip policy | Нет `fresh_no_new_facts` terminal |
| T-FS-02 | Tier2 high-risk child | Tier2 behavior | Ставится deferred deep research |
| T-FS-03 | FilterSearchable -> empty | Research stage | Выполняется reseed/debt queue |
| T-FS-04 | Partial research success | MissingInfo state | Резолвятся только подтвержденные вопросы |
| T-FS-05 | New sources after exhausted | MissingInfo reset | Статус reset в `open` |
| T-FS-06 | Domain enqueue async lag | Domain cycle | Await/poll, no false no-progress |
| T-FS-07 | Artifact count > 4000 | collectGraph | Пагинация + `graph_truncated` signal |
| T-FS-08 | budget exhausted recoverable | preflight stop | Recovery pass before hard-stop |

Минимальный набор автоматизации:
1. Unit tests для policy/reasoning веток.
2. Integration tests для Scout pipeline stop semantics.
3. Replay tests на real logs.

#### 4) Контракт наблюдаемости (Telemetry Contract)

Каждое stop/skip/recovery событие обязано включать:
1. `reason_code`
2. `stop_kind` (`hard|soft|recovery`)
3. `recoverable` (`true|false`)
4. `critical_open_count`
5. `evidence_coverage`
6. `domain_mode`
7. `pipeline_tier`
8. `recursion_depth`
9. `graph_truncated` (для domain cycle)
10. `source_fingerprint_changed` (для missingInfo reset path)

#### 5) Release gates (точные пороги)

Перед переходом на следующий этап:
1. `fresh_no_new_facts` при `critical_open_count>0` = 0.
2. `research_skipped_no_searchable_gaps` при `critical_open_count>0` = 0.
3. `ready_with_gaps` с recoverable P0/P1 = 0.
4. `soft_stop_recovered_rate >= 0.80` на canary.
5. `direction_contract_quarantine_rate` снижен минимум на 50% от baseline.

#### 6) План отката (Rollback Plan)

Если любой release gate нарушен:
1. Откат соответствующего feature flag до предыдущего stable value.
2. Заморозка перехода к следующему sprint.
3. Инцидентный разбор: `reason_code distribution`, `top failing contracts`, `regressed test IDs`.
4. Повторный canary только после закрытия root-cause action items.

#### 7) Ответственность и артефакты результата

Для каждого Workstream обязателен набор deliverables:
1. PR со ссылкой на `Task ID`.
2. Тесты (unit/integration/replay) + отчет выполнения.
3. Сводка telemetry до/после (baseline vs candidate).
4. Решение по rollout gate (`pass|hold|rollback`) с конкретной причиной.

## Delta (2026-02-13): Internal-First Grounding (v5)

Источник: `docs/horizon/implementation/INTERNAL_FIRST_GROUNDING_IMPLEMENTATION_PLAN.md` (rev v5).

Фиксируем согласованные уточнения без урезания целевого функционального объёма:

1. `P2.1` (grounding-aware wiring) не выносится в отдельный декомпозированный scope reduction. Реализуем в основном контуре plan без снижения целевой функциональности.
2. Model routing: capability-first.
   - Сначала фильтрация по capabilities (`SearchGroundingCapability` + `InternalDataGroundingCapability`).
   - Затем выбор по цене внутри отфильтрованных кандидатов.
   - Отдельный hardcoded provider-priority не вводится.
3. Datastore scoping: единый источник истины для Horizon retrieval path — `VERTEX_SEARCH_HORIZON_DATA_STORE_ID`. Не допускаем параллельной эксплуатации двух разных datastore источников в critical path.
4. Risk `IDENTIFY`: fragmentId-first с унификацией state-ключей и canonical normalization по naming/taxonomy policy перед path fallback.
5. Provenance rollout: staged delivery без сокращения full scope.
   - Milestone A (v1): базовый контракт provenance для job/frame + resolution stats/telemetry.
   - Milestone B: полный охват edge/fact/domain snapshot provenance, как зафиксировано в implementation plan.

## External Risk Maturity Plan (без миграций данных)

Фокус: только внешний контур (global/external drivers). Внутренние операционные и договорные риски остаются за владельцами соответствующих систем.

### Целевая цепочка (signal-first)

`event -> signal descriptor -> applicability -> seed injection -> axis propagation -> rollup/explain -> interpretation -> concentration`

### Реализовано в runtime

- Signal-first propagation: распространяются только канонические метрики `/metric/{risk|opportunity}/{axis}/{mechanism?}`.
- Shared axis propagation profile lookup (без локальных defaults-only движков в Scout fast-path).
- Auto interpretation runtime:
  - trigger evaluator + queue + processor + cache;
  - enqueue после обновления узлов propagation.
- Applicability перед seed injection:
  - типовая, гео- и sphere-оценка применимости внешнего сигнала к узлу;
  - неприменимые seed-узлы отбрасываются.
- Risk appetite:
  - канонический профиль `RiskAppetiteProfile` по scope (`/market/...`, `/loc/...`);
  - auto-default seeding;
  - соседи/зависимые рынки учитываются через графовые связи (neighbor scopes).
- Rollup/Explain:
  - strict canonical signal metrics;
  - top contributors и propagation paths из provenance.
- Concentration snapshot API:
  - `GET /api/horizon/risk/concentration?polarity=risk&limit=10`.

### Оставшиеся шаги (без миграций)

1. Усилить applicability policy:
   - добавить конфигурируемые правила по vertical/geo/axis;
   - включить policy-backed explain reasons.
2. Appetite governance (частично):
   - UI/endpoint для analyst override профилей аппетита;
   - audit trail для изменений порогов.
3. Quality loop:
   - регулярные backtest job’ы (precision/recall/false alerts);
   - drift monitoring по axis amplitude/confidence.
4. Alerting hardening (частично):
   - dead-letter и retry policy для interpretation queue;
   - SLO-мониторинг latency event->alert.

## Signaling Pipeline (Roadmap)

**Контекст:** Scout больше не создаёт сигналы. Сигналы — отдельный пайплайн из потока новостей. Источники (rss, horizon_monitor_feed) уже есть; система сама управляет расписанием поллинга.

**План:** см. `SIGNALING_PIPELINE_IMPLEMENTATION_PLAN.md`.

**В roadmap (позже):**
- **Fallback для слепых зон:** org в графе, но не попал ни в один event из концентрационных фидов — возможность точечного досмотра по запросу пользователя (например, "проверь новости по G42"). Не делать регулярным сканом по всем org.
- **TODO: рассылка WS при propagation сигналов.** Сейчас `PropagateSignalFromSeeds` (news-by-fragments, signal pipeline) не шлёт WS; только `PropagateChange` (metric propagation) публикует `horizon.propagation.started`, `horizon.propagation.node_updated`, `horizon.graph.node.upserted`, `horizon.propagation.completed`. Добавить аналогичную рассылку при propagation сигналов, чтобы фронт мог обновлять граф в реальном времени во время news-by-fragments.

## Canonical Playbook: Systemic Dependency Investigation (semiconductor-class queries)

Цель: зафиксировать единый контракт для запросов класса:

`Investigate systemic risks and opportunities in the global semiconductor supply chain (2026–2030)... prioritize top 10 dependencies with confidence and sources.`

Этот playbook задаёт **как исследуем**, **как материализуем в граф**, и **как отдаём результат** в UI/API без ad-hoc логики.

### 1) Нормализация запроса в Domain + QueryAddress

1. Сначала резолвим intent в домен-контракт (`/domain/v1/supply-chain/...`) и целевые разрезы:
   - value-chain сегменты: lithography equipment, advanced packaging, critical inputs;
   - временной горизонт: `2026-01-01 .. 2030-12-31`;
   - полярность: `risk` + `opportunity`.
2. Затем формируем `QueryAddress` для scope (sector/sphere/location/entityType) и snapshot resolved paths.
3. Все дальнейшие шаги работают с каноническими path, а не со свободным текстом.

### 2) Двухконтурный runtime (обязательно)

1. **Structural contour**: `Scout -> Critic -> Research -> Policy`.
   - формирует/обновляет `HorizonFragment` и `DependencyEdge`;
   - применяет canonicalization + policy statuses (`clean|auto_fixed|penalized|quarantined`).
2. **Signal contour**: `IDENTIFY -> ESCALATE -> APPLICABILITY -> SEED -> PROPAGATE -> ROLLUP -> INTERPRET -> ALERT`.
   - работает только с `SignalDescriptor` (`OntologyFact(kind=signal)`), canonical metric paths и traceable reason codes.
3. Результат ответа всегда объединяет оба контура: структура + динамика сигнала.

### 3) Онтологическая проекция (обязательные сущности/связи)

Минимальный набор узлов:
- `ORGANIZATION`, `FACILITY`, `PRODUCT`, `MARKET`, `DEPOSIT`, `GEO_REGION`, `PROGRAM`.
- Для санкций/экспорт-контроля: `DECISION_POINT`, `MECHANISM`, GOV-узлы (`/gov/...` как `ORGANIZATION`).

Минимальный набор рёбер:
- supply/value chain: `SUPPLIES_TO`, `PRODUCES`, `CONSUMES`, `TRANSFORMS_INTO`, `EXTRACTS_FROM`, `DEPENDS_ON`.
- geo/regulatory: `LOCATED_IN`, `OPERATES_IN`, `REGULATES`, `CONTROLS_ACCESS`, `USES_MECHANISM`, `APPLIES_TO`.
- market/concentration: `PARTICIPATES_IN`, `DOMINATES`, `HAS_SUBMARKET`.

Обязательные qualifiers/поля для transmission-grade ребер:
- `throughput_share`, `capacity_constraint`, `reroute_penalty`, `reroute_latency_delta_hours`, `reroute_cost_delta_pct`;
- `edgeModality`, `ValidFrom`, `ValidTo`;
- policy qualifiers (`policyStatus`, `policyReason`, `policyPenalty`, `policyTimestamp`).

### 4) Контракт числовых метрик (TAM/CAGR/CR4/HHI и др.)

Для каждого ключевого market/submarket узла:
1. Пытаться материализовать `TAM`, `CAGR`, `CR4`, `HHI` (+ период, единицы, источник).
2. Если метрика отсутствует:
   - сохранять `unknown` с explicit gap reason;
   - не подменять пустоту псевдо-точностью.
3. `CR4/HHI` использовать как вход в concentration/risk ranking.

Для каждого dependency edge:
1. Хранить долевые/пропускные величины как edge metrics/qualifiers.
2. Разделять observed vs projected через `edgeModality` + temporal validity.

### 5) Топ-10 зависимостей: deterministic ranking

`TopDependencyScore` (нормализованный 0..1, explainable):

`impact * exposure * concentration * (1 - substitutability) * sanction_export_control_exposure * confidence`

Где:
1. `impact/exposure` — из propagation + path criticality.
2. `concentration` — из `CR4/HHI` и/или proxy.
3. `substitutability` — из reroute/capacity qualifiers и observed alternatives.
4. `sanction_export_control_exposure` — из decision/mechanism layer (`REGULATES/CONTROLS_ACCESS/USES_MECHANISM`).
5. `confidence` — агрегат source quality + corroboration + recency + policy penalties.

Выход top-10 обязан включать:
1. зависимость (source -> target, product/mechanism context),
2. score + confidence,
3. why-path (минимум 1 transmission path),
4. substitution options,
5. sources (прямые URL/refs).

### 6) Графовый output контракт (UI/API)

В graph-view показываем 3 слоя одновременно (с возможностью фильтра):
1. **Structural layer**: реальные поставки/трансформации/гео-размещение.
2. **Control layer**: регуляторы, decision points, mechanisms, access control.
3. **Signal layer**: risk/opportunity волны, contributors, rollup.

Обязательные визуальные правила:
1. Quarantined edges всегда видимы, но исключены из risk-critical path (dashed/muted).
2. Каждый top dependency кликабелен до evidence chain (sources + reason codes + policy qualifiers).
3. Временной фильтр `2026–2030` влияет на edge/node validity и сценарные метрики.
4. Переключение `risk|opportunity` меняет ranking/paths, а не только цвет.

### 7) Definition of Done для этого класса запросов

1. Ответ воспроизводим повторным прогоном (одинаковый top-10 при том же входе/данных).
2. У каждого top-10 есть machine-readable explain + источники.
3. В выдаче нет рёбер без direction policy и без policyStatus.
4. Метрики концентрации либо заполнены, либо явно помечены как data gap.
5. Sanctions/export-control exposure объяснён через decision/mechanism слой, а не свободным текстом.

### 8) Domain Answer Layer + DomainContractEngine

**Дизайн-документ**: `docs/horizon/implementation/DOMAIN_ANSWER_LAYER_DESIGN.md`

**Суть**: Ввести над графом `Domain Answer Layer` — домены как структурированные контракты, ответ собирается из артефактов, граф объясняет ответ (path-centric), а не заменяет его. Исполнение контрактов — в выделенном движке `DomainContractEngine`.

**Ключевые решения** (зафиксированы):

1. **Несколько доменов в одном job, без иерархии**. IntentResolver возвращает до 3 доменов; каждый обрабатывается DomainContractEngine независимо на одном графе. Под-темы внутри одного домена — facets. Контракт типизирован (kebab-case: `supply-chain`, `regulatory-impact`, `competitive-landscape` и др. — см. `paths.go:domainContractTypes`).
2. **`DomainContractEngine`** — выделенный расширяемый движок (`backend/internal/horizon/domain/`):
   - Фазы: Resolve → Steer → Gate → Enrich → Assemble → Publish.
   - Scout/Critic/Research/Policy **не меняются** — DomainContractEngine стирит их через scouting directives.
   - Будущее расширение: Scenario (what-if), Compare, Watch.
3. **Cross-facet connectivity** — домен стирит scouting для поиска связей между facets. Без горизонтальных связей между facets нет explainability. Inter-facet connectivity — обязательная проверка в Completeness Gate.
4. **Сборка артефактов — post-processing с одним structured LLM вызовом**:
   - Основные артефакты (`critical_dependencies`, `concentration_evidence`, `control_exposure`, `impact_paths`, `data_gaps`) — детерминированная проекция из графа (scoring, BFS/Dijkstra, агрегация метрик).
   - LLM (один structured вызов) — `thesis`, `alternatives.feasibility`, `objective_check`.
5. **Completeness Gate — гибрид**:
   - Level 1 (детерминированный): structural checks (минимум узлов/facet, inter-facet connectivity, market metrics, regulatory edges). Если не пройден — enrichment без LLM.
   - Level 2 (Domain Critic, старшая модель): семантическая полнота. Если не пройден — enrichment signals → новые задачи в том же job. ≤ 2 цикла, budget cap, fallback `ready_with_gaps`.
6. **Graph-query эволюционирует в drill-down** поверх готового `DomainAnswerSnapshot`.
7. **UI**: Domain Overlay — сворачиваемый контрол поверх графа (mini → domain list → expanded с thesis/deps/hotspots/gaps/facets). Граф первичен, overlay управляет подсветкой.
8. **Интеграция через adapter pattern (R8)**: `ContractHandler.Execute()` возвращает только `ViewDirective` — pipeline не меняется. Новый `DomainAnswerHandler` регистрируется в `ContractRegistry`, внутри делегирует в `DomainContractEngine.Run()`. Snapshot публикуется в отдельную Firestore collection `domain_answer_snapshots/`. `ViewDirective` генерируется как backward-compat адаптер из snapshot. `domain_overlay.go` endpoints мигрируются в thin wrappers над DomainContractEngine.
9. **WS-события DomainContractEngine (R9)**: Namespace `horizon.domain.contract.*` — события на каждой фазе (resolve/steer/gate/enrich/assemble/publish). Инфраструктура готова (Pub/Sub → WS Hub → фронт). DomainContractEngine получает `EventPublisher` interface, не зависит от ScoutEngine напрямую. Фронт: timeline лог фаз в Domain Overlay (expanded), badge текущей фазы в mini-state.

**Engineering notes (Phase 1 review, не блокируют, учесть при масштабировании)**:

1. **Gate "partial" и enrichment loop**: `summarizeChecks` при P1 fail возвращает `OverallStatus = "partial"`. Enrichment loop продолжает цикл при `OverallStatus != "pass"`, т.е. partial тоже триггерит enrichment. Это корректно, но L2 semantic critic может вернуть "partial" (= "mostly ok") — и это потратит лишний enrichment cycle. При тюнинге: рассмотреть отдельный порог для L2 partial vs L1 partial.
2. **Enrichment jobs — async gap**: `enqueueEnrichmentTasks` создаёт scouting jobs, затем `ExecuteBaseCollect` вызывается сразу после. Если enrichment jobs обрабатываются async (а не в том же executor context) — повторный `collectGraph` может пересобрать тот же граф без новых данных. Убедиться, что enrichment jobs завершаются до re-collect, или добавить await/poll.
3. **`collectGraph` hardcoded limit 4000 artifacts**: для крупных линз с множеством доменов может не хватить. Вынести в `GateConfig` или сделать пропорциональным количеству доменов.
4. **BFS complexity в `hasConnectivityBetweenFacets`**: для каждой пары facet nodes запускается BFS. При N facets × M nodes per facet = до N²×M² BFS runs. Early return спасает. Для текущих масштабов (< 4K nodes) — ок. При росте: рассмотреть single-source multi-target BFS или union-find.

**Инкрементальность (deferred)**:
- Пересборка `DomainAnswerSnapshot` при изменениях графа — **отложено**.
- Когда будет нужно: `assembledAt` + `staleAfter`; stale-маркировка при изменениях в critical paths; пересборка lazy/eager. Естественная ответственность фазы `Watch` в DomainContractEngine.

## Decision-Grade Reasoning Upgrade (Prompt + Algorithms)

### Контекст и формулировка проблемы

Текущее состояние системы можно считать **достаточным для discovery (примерно 7/10)**, но **недостаточным для принятия управленческих решений (примерно 5/10)**. Ключевой лимит находится не в одном компоненте, а в комбинации:

1. Промпты хорошо формируют графовые факты, но слабо требуют конкурентных гипотез и явного опровержения альтернатив.
2. Алгоритмика пайплайна больше оптимизирована под materialization и policy safety, чем под decision-quality inference.
3. Confidence локален (узел/ребро), но не агрегируется в уровень решения (вариант действия, сценарий, trade-off).
4. Нет полноценного контура «обоснование ↔ контробоснование ↔ арбитраж».
5. Нет полного feedback loop от решений аналитика обратно в prompt/policy behavior.

### Цель программы

Перевести контур reasoning из режима «graph discovery» в режим **decision-grade reasoning** без разрушения текущей архитектуры Scout/Critic/Research/Policy:

1. Повысить качество решения до **8+/10** по внутренним benchmark-кейсам.
2. Для каждого управленческого вывода давать:
   - набор конкурирующих гипотез,
   - выбранную гипотезу с причиной выбора,
   - список отвергнутых альтернатив с reason codes,
   - чувствительность к ключевым предположениям,
   - машиночитаемую неопределенность.
3. Сохранить обратную совместимость с текущими графовыми артефактами и UI.

### Non-goals (что не делаем в этом треке)

1. Не переписываем с нуля весь ScoutEngine.
2. Не удаляем существующий graph/materialization pipeline.
3. Не делаем «AGI-оркестратор»; изменения должны быть локально объяснимы, тестируемы и откатываемы.
4. Не блокируем текущий production flow ради полной идеальности.

### Принципы дизайна

1. **Hypothesis-first, not fact-first**: решение строится вокруг конкурирующих гипотез, а не вокруг единственного «лучшего» ответа.
2. **Adversarial-by-design**: каждая гипотеза обязана проходить фазу опровержения.
3. **Deterministic scoring envelope**: финальный рейтинг гипотез считается алгоритмически, а не только LLM confidence.
4. **Uncertainty is first-class**: неопределенность хранится и распространяется по контуру, а не теряется при агрегации.
5. **Fail-open для discovery, fail-closed для decisions**: для decision artifacts при нехватке качества — `ready_with_gaps`, а не псевдо-уверенный вывод.

### Целевая архитектура reasoning-контура

Новый логический слой над текущим Scout/Critic/Research:

1. `Hypothesis Generation` (N=2..4 конкурентных гипотез).
2. `Evidence Packing` (детерминированная сборка evidence units из графа + сигналов + provenance).
3. `Adversarial Critique` (попытка опровергнуть каждую гипотезу).
4. `Contradiction Graph` (обнаружение конфликтов между утверждениями).
5. `Deterministic Arbitration` (алгоритмический ranking + фильтры quality gate).
6. `Uncertainty Propagation` (перенос uncertainty по цепочке влияния).
7. `Decision Artifact Assembly` (action options, conditions, failure modes).
8. `Learning Feedback` (подтверждение/отклонение пользователем).

### Рабочие потоки (Workstreams)

#### WS-A: Prompt Contracts 2.0 (Hypothesis-First)

**Цель**: сделать промпты структурно «decision-aware».

1. Ввести обязательный JSON-контракт в research/critic:
   - `hypotheses[]` (id, claim, assumptions, expected_signals),
   - `counterarguments[]`,
   - `disconfirming_evidence[]`,
   - `decision_implications[]`.
2. Запретить single-path reasoning в decision mode:
   - минимум 2 гипотезы, если задача не тривиальна.
3. Ввести требование «negative evidence»:
   - если evidence против гипотезы найден, он не может быть скрыт.
4. Ввести явный `abstain` контракт:
   - когда evidence не хватает, модель возвращает `insufficient_evidence` вместо домысливания.
5. Привязать prompts к allowed reason codes:
   - чтобы downstream scoring использовал не свободный текст, а канонические классификаторы.

**Изменяемые точки (первый этап):**

1. `backend/internal/horizon/scout/agents/research_strategies_impl.go`
2. `backend/internal/horizon/scout/agents/critic_agent.go`
3. `backend/internal/horizon/scout/agents/critic_validators_impl.go`

#### WS-B: Algorithmic Arbitration Layer

**Цель**: исключить зависимость финального решения от «последнего LLM-ответа».

1. Добавить детерминированный scoring гипотез:
   - `score = evidence_strength * coherence * impact * robustness * policy_cleanliness * uncertainty_penalty`.
2. Ввести компонент `HypothesisArbiter`:
   - принимает normalized hypotheses + evidence graph,
   - выдает ranking + reasoned rejection list.
3. Ввести `ContradictionResolver`:
   - фиксирует конфликтующие claim pairs,
   - маркирует гипотезу как `contested` до разрешения.
4. Ввести hard-gates перед публикацией decision artifact:
   - min evidence coverage,
   - max unresolved contradictions,
   - max uncertainty band.
5. Добавить fallback mode:
   - если gates не пройдены, публиковать `ready_with_gaps`, не «recommended_action».

**Изменяемые точки (первый этап):**

1. `backend/internal/horizon/domain/engine.go`
2. `backend/internal/horizon/scout/engine.go`
3. `backend/internal/horizon/scout/agents/research_pipeline.go`
4. `backend/internal/horizon/scout/agents/critic_pipeline.go`

#### WS-C: Uncertainty Model and Propagation

**Цель**: перенести uncertainty с edge-level на decision-level.

1. Ввести unified uncertainty schema:
   - `epistemic_uncertainty`, `model_uncertainty`, `data_freshness_uncertainty`.
2. Считать uncertainty band для каждой гипотезы:
   - lower/median/upper estimate.
3. Для top decision options считать sensitivity:
   - какие 3-5 предположений сильнее всего меняют выбор.
4. Ввести propagation правил:
   - uncertainty не усредняется наивно, а агрегируется с учетом weakest-link и corroboration count.
5. Экспортировать uncertainty в UI/API:
   - decision карточка должна показывать не только score, но и uncertainty band.

**Изменяемые точки (первый этап):**

1. `backend/internal/horizon/models/*` (новые decision/uncertainty структуры)
2. `backend/internal/horizon/propagation/*` (агрегация uncertainty)
3. `backend/internal/http/horizon/*` (контракт ответа)

#### WS-D: Decision Semantics and Output Contract

**Цель**: выдача, пригодная к управленческим действиям.

1. Ввести новый артефакт `DecisionAssessment`:
   - `recommended_option`,
   - `alternatives[]`,
   - `conditions_of_validity[]`,
   - `failure_modes[]`,
   - `monitoring_triggers[]`.
2. Ввести обязательные секции:
   - `what_we_know`,
   - `what_can_break_this`,
   - `what_to_measure_next`.
3. Привязать каждый пункт к source/evidence paths.
4. Ввести machine-readable actionability levels:
   - `informational`, `monitor`, `prepare`, `act_now`.

#### WS-E: Learning Loop (Human Feedback → System Behavior)

**Цель**: системно обучать reasoning-контур на решениях команды.

1. Собирать feedback события:
   - accepted/rejected/deferred decision options.
2. Логировать причины отклонения:
   - `too_speculative`, `wrong_assumption`, `missing_evidence`, `policy_conflict`.
3. Еженедельно обновлять:
   - prompt templates,
   - policy weights,
   - arbitration thresholds.
4. Ввести `Reasoning Regression Suite`:
   - фиксированные кейсы, где ожидаем стабильный выбор гипотезы.

### Декомпозиция этапов внедрения (итеративно)

#### Phase R0 (1 спринт): Foundation

1. Зафиксировать decision-mode JSON schemas.
2. Добавить feature flags:
   - `HORIZON_DECISION_MODE_ENABLED`
   - `HORIZON_HYPOTHESIS_ARBITER_ENABLED`
   - `HORIZON_UNCERTAINTY_BAND_ENABLED`
3. Подготовить benchmark-набор из 30-50 кейсов (market/affected-zone/event/regulatory).
4. Добавить telemetry baseline:
   - contradiction rate,
   - abstain rate,
   - top-1 flip rate между повторами.

**DoD R0**:

1. Схемы и флаги в коде.
2. Baseline метрики снимаются автоматически.

#### Phase R1 (2 спринта): Hypothesis + Adversarial Critic

1. Обновить prompts на hypothesis-first контракт.
2. Добавить adversarial critique шаг в critic pipeline.
3. Добавить rejection reasons в output.
4. Включить на shadow traffic (без влияния на production decisions).

**DoD R1**:

1. Для decision-mode задач возвращается >=2 гипотез.
2. Каждая гипотеза имеет хотя бы 1 counterargument или explicit `none_found`.
3. Нет schema drift в 95%+ запусков.

#### Phase R2 (2 спринта): Deterministic Arbitration

1. Внедрить HypothesisArbiter.
2. Внедрить contradiction graph + gates.
3. Выводить ranked options + rejected alternatives.
4. Включить limited rollout (10-20% decision-mode запросов).

**DoD R2**:

1. Повторяемость top-1 решения растет (target: +25% к baseline).
2. Доля «необъяснимых» рекомендаций (без reason path) < 5%.

#### Phase R3 (2 спринта): Uncertainty Propagation

1. Добавить uncertainty schema и агрегатор.
2. Добавить sensitivity analysis в decision artifact.
3. Добавить UI/API поля uncertainty band.

**DoD R3**:

1. 100% decision options имеют uncertainty band.
2. 90%+ options имеют минимум 1 sensitivity driver.

#### Phase R4 (постоянный цикл): Learning Loop

1. Собирать человеческий feedback.
2. Ввести еженедельный tuning cadence.
3. Регрессионно проверять, что улучшения не ломают existing discovery quality.

**DoD R4**:

1. Есть стабильный процесс улучшения с KPI-tracking.
2. Regression suite запускается автоматически в CI.

### Изменения в данных и контрактах (детально)

1. Новый доменный объект `DecisionHypothesis`:
   - `id`, `claim`, `assumptions[]`, `supporting_evidence[]`, `opposing_evidence[]`, `status`.
2. Новый объект `DecisionOption`:
   - `option_id`, `action`, `score`, `uncertainty_band`, `preconditions[]`, `failure_modes[]`.
3. Новый объект `DecisionAssessment`:
   - `recommended_option_id`, `alternatives[]`, `rationale`, `gaps`, `monitoring_plan`.
4. Обязательные поля provenance для decision outputs:
   - `source_refs[]`, `evidence_paths[]`, `policy_status_trace[]`.

### Метрики качества и SLO для decision-grade

1. `decision_reproducibility_rate`:
   - одинаковый top-1 на повторных прогонах.
2. `contradiction_resolution_rate`:
   - доля resolved конфликтов до публикации.
3. `abstain_precision`:
   - как часто abstain совпадает с ручной оценкой «нельзя рекомендовать».
4. `false_confidence_rate`:
   - high-confidence решения, позднее отклоненные аналитиком.
5. `time_to_decision_artifact`:
   - latency от запроса до decision output.
6. `human_acceptance_rate`:
   - доля принятых рекомендаций.

### Риски и меры снижения

1. Риск: рост latency из-за multi-hypothesis.
   - Митигатор: budget caps, adaptive depth, caching evidence packs.
2. Риск: prompt усложнение увеличит schema drift.
   - Митигатор: strict verifiers + auto-repair + contract tests.
3. Риск: arbitration будет переусложнен.
   - Митигатор: сначала минимальная формула scoring, далее тюнинг.
4. Риск: uncertainty будет непонятен пользователю.
   - Митигатор: стандартизованный UI-паттерн band + sensitivity explanation.

### Definition of Done для всей программы

Программа считается завершенной, когда одновременно выполнены условия:

1. На benchmark-наборе decision quality >= 8/10 по экспертной оценке.
2. Не менее 90% decision outputs имеют:
   - ranked alternatives,
   - explicit rejection reasons,
   - uncertainty band,
   - evidence paths.
3. False-confidence rate снижен минимум на 40% от baseline.
4. Production rollout 100% для decision-mode задач без деградации discovery SLO.
5. Learning loop работает как постоянный операционный процесс (не ad-hoc).

## Пайплайн Scout → Critic → Research → Policy

**Контекст**: Анализ пайплайна Horizon в `engine.go` (см. `HORIZON_PIPELINE_ANALYSIS.md`).

**Текущий порядок** (соблюдён): Intent Resolution → Scout → Critic → Research → Critic Re-validation → Conflict Resolution → Metrics → Persistence → Policy Check.

**Готово**: Conflict Resolution перенесён после Research; добавлена re-validation после Research; `recordDeterministicRedirect` вызывается в ключевых path-change кейсах (`market_dedup`, `precreate_normalization`).

**TODO для проверки и реализации**:

| Приоритет | Задача | Описание |
|-----------|--------|----------|
| P1 | Унифицировать Policy | Policy применяется в 3+ местах (`edge_materializer`, `precreate_validator`, `dependency_edges`, `runPolicyCheck`) с разной логикой. Одинаковые action handlers (reject/reverse/penalty) во всех точках, best-effort. |
| P3 | `recordDeterministicRedirect` coverage | Основные кейсы закрыты (в т.ч. Market deduplication, Pre-create validation). Оставить регрессионный контроль на новых местах path rewrite. |
| P2 | Централизовать canonicalization | `canonicalPath` меняется в 6 местах → множество вызовов `bestEffortRewriteLensScope`. Рассмотреть накопление изменений и один финальный rewrite. |
| P2 | Убрать legacy monolithic fallback в ResearchAgent | После ввода composable `GeneralResearchStrategy` оставить единый путь enrichment (pipeline-only), удалить `enrichFragmentWithGroundingMonolith` и мёртвый fallback-контур, сохранить backward-compatible контракты промптов/схем. |
| P2 | История aliases | `aliasCanonicalPath` хранит только первый alias. При цепочке `/org/tesla` → `/org/us/tesla` → `/org/us/tesla-inc` сохранять массив aliases. |
| P2 | Budget checks раньше | Проверка бюджета только перед Research. Scout и Critic могут потратить до проверки. При масштабировании добавить проверки на ранних этапах. |

## Markets (MARKET) — high-level

- **Market↔Market relations (future, not MVP)**:
  - **Goal**: model value-chain / dependency structure between markets (e.g., "EV batteries market depends on lithium chemicals market").
  - **Default in MVP**: use `DEPENDS_ON` if needed (MARKET → MARKET) as a generic relationship.
  - **Possible dedicated edge types (later)**:
    - `UPSTREAM_OF` / `DOWNSTREAM_OF` (explicit value chain semantics)
    - `SUBMARKET_OF` (taxonomy hierarchy / market decomposition)
  - **Reason to defer**: requires clear semantics + direction rules + UI/analytics decisions; MVP focus is market metrics + participants/share.

---

## Financial Data Sources & Canonicalization (future)

**Контекст**: Финансовые данные — одна из ключевых осей 12-axis basis (`financial/settlement`, `financial/credit`), но сейчас нет ни одного подключённого финансового источника. Заглушки существуют (yahoo-finance, coingecko, finnhub, fred-data, etf-flows, stablecoin-markets, stock-index, eia-petroleum, macro-signals), но не реализованы. Финансовые сущности моделируются как `/org/`, что теряет специфику.

**Цель**: Полноценное покрытие финансовых данных в системе — от raw data до 12-axis сигналов, с правильной канонизацией и провенансом.

### Этап 1: Canonical paths для финансовых сущностей

Новые namespace-ы в `fragment_path_policy.go`:
- `/instrument/{type}/{exchange}/{ticker}` — финансовые инструменты (акции, облигации, ETF, фьючерсы)
  - Примеры: `/instrument/equity/nasdaq/tsla`, `/instrument/commodity/nymex/cl-2026-06`, `/instrument/etf/nyse/spy`
- `/index/{provider}/{slug}` — индексы
  - Примеры: `/index/sp/sp500`, `/index/msci/em`, `/index/bdi/baltic-dry`
- `/rate/{type}/{geo}/{slug}` — ставки, курсы
  - Примеры: `/rate/interest/us/fed-funds`, `/rate/fx/eur-usd`, `/rate/cds/br/5y`

Существующие namespace-ы покрывают:
- `/org/` — банки, фонды, брокеры (остаются как организации)
- `/market/` — финансовые рынки как сегменты
- `/gov/` — регуляторы (ЦБ, SEC, и т.д.)

### Этап 2: Источники данных (приоритизация)

**Высокий приоритет** (широкое покрытие, бесплатные/дешёвые API):

| Источник | Данные | Оси 12-axis | API |
|----------|--------|-------------|-----|
| **Yahoo Finance** | Цены, объёмы, фундаментал | `financial/settlement`, `financial/credit` | Неофициальный, бесплатный |
| **FRED (Federal Reserve)** | Макро-индикаторы (ставки, инфляция, GDP, M2) | `financial/credit`, `info/uncertainty` | Бесплатный (API key) |
| **EIA Petroleum** | Нефтяные запасы, добыча, цены | `physical_flow/supply`, `financial/settlement` | Бесплатный |
| **Baltic Exchange / Freightos** | Фрахтовые индексы (BDI, FBX) | `logistics/capacity`, `financial/settlement` | Платный |

**Средний приоритет** (специализированные):

| Источник | Данные | Оси 12-axis | API |
|----------|--------|-------------|-----|
| **Finnhub** | Real-time котировки, новости, SEC filings | `financial/settlement`, `info/reputation` | Freemium |
| **CoinGecko** | Крипто цены, объёмы, market cap | `financial/settlement` | Бесплатный |
| **Polymarket** | Prediction markets (вероятности событий) | `info/uncertainty` | Бесплатный |

**Низкий приоритет** (нишевые):

| Источник | Данные | Оси 12-axis |
|----------|--------|-------------|
| **Stablecoin flows** | Объёмы USDT/USDC | `financial/settlement` |
| **ETF flows** | Потоки в ETF | `financial/credit`, `physical_flow/demand` |

### Этап 3: Адаптеры и Signal Pipeline

Для каждого источника:

1. **HTTP Client** (`internal/http/{source}_client.go`) — API wrapper с rate limiting, retry, auth
2. **Adapter** (`internal/discovery/adapters/{source}.go`) — нормализация в `HorizonMonitorItem` с:
   - Canonical path (e.g., Yahoo Finance TSLA → `/instrument/equity/nasdaq/tsla`)
   - Metric values mapped to 12-axis (price change → `financial/settlement`, volatility → `info/uncertainty`)
3. **Feed registration** — как `horizon_monitor_feed` DataSource kind
4. **Telemetry wrapper** (`internal/horizon/telemetry/{source}_wrapper.go`) — мониторинг API вызовов
5. **Signal extraction rules** (`SignalExtractionRule`) для автоматического маппинга финансовых событий на оси

### Этап 4: EntityTypeKey и семантика

- Добавить `INSTRUMENT`, `INDEX`, `RATE` в `EntityTypeKey` (`entity_semantics/types.go`)
- Правила валидации: ticker required, exchange required для instruments
- Связи: `INSTRUMENT --LISTED_ON--> ORG(exchange)`, `INSTRUMENT --ISSUED_BY--> ORG`, `INDEX --TRACKS--> MARKET`
- `InferFragmentTypeFromCanonicalPath`: `/instrument/` → INSTRUMENT, `/index/` → INDEX, `/rate/` → RATE

### Этап 5: Канонизация и дедупликация

- `InstrumentCanonicalizer` по аналогии с `ProductCanonicalizer`:
  - Нормализация тикеров (TSLA = Tesla Inc. equity on NASDAQ)
  - Дедупликация: один инструмент на разных биржах — разные фрагменты, связанные `SAME_AS`
  - Cross-listing resolution через ISIN/FIGI/CUSIP где доступно
- Wikidata enrichment: компании имеют P414 (stock exchange), P249 (ticker symbol)

### Зависимости

- Требует стабильной работы HorizonMonitor feed pipeline
- Требует `fragment_path_policy.go` обновления для новых namespace-ов
- Не блокирует текущий QA план — финансовые вопросы покрываются через Gemini Search Grounding (best-effort)

---

## Fractal Hierarchy & Scale-Invariant Contracts

**Контекст**: Граф знаний обладает свойством масштабной инвариантности — узлы на разных уровнях абстракции (макро-организация ↔ подразделение ↔ объект; рынок ↔ сегмент ↔ ниша; династия ↔ ветвь ↔ персона) описываются единым интерфейсом и обрабатываются одним механизмом распространения сигналов. Сейчас это **эмерджентное** свойство (следствие единообразия `HorizonFragment`), но не зафиксировано как архитектурный принцип. Цель — сделать фрактальность **явной** через пять контрактов.

**Принцип**: явно ≠ строго. Контракты должны быть чёткими, но не навязывать математическую фрактальность. Реальный мир не фрактален в точном смысле; династия House of Saud не является уменьшенной копией рынка нефти. Они просто описываются одинаковым набором инструментов (узел + метрики + рёбра + propagation).

### Архитектурные решения (реализовано 2026-02)

**Идентичность сущности**: `Entity = (Type, SpherePaths[], ResolutionHint, ParentPath?)`

- **Units deleted**: `UnitCode`, `OrgUnitProfile`, `UnitGraphEdge`, `UnitSupplyLinkQualifier`, `UnitTemplatePair`, Unit Pipeline (P0–P5), unit graph snapshots, unit dive results — полностью удалены из кодовой базы. Классификация операционных подразделений заменена на `SpherePaths` + `ResolutionHint` + `ParentPath`.
- **Sphere taxonomy**: L1 (closed list ~14), L2 (controlled per L1), L3+ (free-form, slugified). Валидация: `taxonomy.ValidateSpherePath`, `taxonomy.NormalizeSpherePath`.
- **Chokepoint transit amplification**: при прохождении сигнала через chokepoint-фрагмент (geoFeatureType: strait/canal/pass/pipeline-corridor или tag `chokepoint`) амплитуда усиливается (TransitAmplify) вместо затухания. Коэффициенты по осям: logistics 1.25, physical flow 1.20, geo conflict 1.15, прочие 1.10.

### Sphere Taxonomy Contract (реализовано)

**L1** (closed list ~14): defense, finance, energy, trade, technology, logistics, health, legal, governance, security, environment, social, infrastructure, other.

**L2** (controlled per L1): задаётся в seed taxonomy (`SphereL2Map`). Пример: defense → naval, air, land, cyber, space, procurement, intelligence, other.

**L3+** (free-form): slugified, до 5 сегментов. Путь: `/sphere/L1/L2/L3/L4`.

**Нормализация**: неизвестный L1 → `/sphere/other`; неизвестный L2 → `/sphere/{L1}/other`. `taxonomy.NormalizeSpherePaths`, `taxonomy.ValidateSpherePath`.

**SpherePaths на HorizonFragment**: `[]string`, выводятся через `sphere_inferrer.InferSpherePaths` по типу сущности (GOV, ORG, MARKET, PRODUCT, FACILITY, GEO_REGION, COMMUNITY).

### Chokepoint Transit Amplification Contract (реализовано)

**Chokepoint** = фрагмент с `geoFeatureType` ∈ (strait, canal, pass, pipeline-corridor) или tag `chokepoint`.

**Transit amplification**: при BFS-распространении сигнала, если текущий узел — chokepoint, амплитуда усиливается перед передачей соседям (вместо затухания):
- AxisLogisticsCapacity / AxisLogisticsLatency: ×1.25
- AxisPhysicalFlowSupply / AxisPhysicalFlowDemand: ×1.20
- AxisGeoConflict: ×1.15
- прочие: ×1.10

**Discovery**: промпты Research/Critic для energy/logistics/trade сфер включают поиск chokepoint-зависимостей (DEPENDS_ON с qualifiers: throughputShare, substitutability, reroutePenalty).

---

### Контракт 1: Canonical Path Hierarchy (унификация первых N уровней)

**Проблема**: Иерархия реализована шестью разными механизмами:
- Markets: `ParentMarketPath` на фрагменте
- Government: `ParentGovPath` на фрагменте
- Communities: `HAS_SUBGROUP` ребро в графе
- Products: сегменты canonical path (конвенция)
- Orgs → Units: `OrgUnitProfile.ParentOrgRef` в отдельной структуре
- Geo: `/loc/{country}/{admin1}/{city}` — конвенция пути

**Решение**: Унифицировать **первые N уровней** canonical path; допускать разночтения только на концах — специфичных для отдельных типов сущностей.

**Универсальные уровни (N=2)**:

| Уровень | Семантика | Правило |
|---------|-----------|---------|
| L0 | namespace | org \| gov \| loc \| market \| community \| segment \| fac \| product \| ... |
| L1 | primary scope | geo (ISO2/composite/regional/na) для geo-scoped; domain для segment/product; orgSlug для fac |

**Тип-специфичный хвост (L2+)**:

| Type | L2 | L3 | L4 | Примечание |
|------|----|----|----|------------|
| org | {slug} | — | — | |
| gov | {slug} | — | — | |
| loc | {admin1} | {city} | — | geo-иерархия |
| market | {domain} | {vertical} | [subvertical...] | |
| community | {type} | {slug} | — | |
| segment | {vertical} | {geo} | — | geo в хвосте (отличие от org/gov) |
| fac | {facilitySlug} | — | — | или {orgSlug}/{facilitySlug} |
| product | {type} | {stage} | {slug}[...] | своя таксономия |

**Единый примитив parent**: `parentPath(path) = path без последнего сегмента` для path-based иерархии. Для edge-based (HAS_SUBGROUP и т.п.) — отдельный канал, но `ParentPath` (или аналог) для консистентности.

**Реализовано**:
- [x] `ParentPath` на `HorizonFragment` — универсальное поле
- [x] Нормализация копирует `ParentGovPath`/`ParentMarketPath` в `ParentPath` при пустом (backward-compat)

### Контракт 2: Resolution Level (уровень абстракции)

**Проблема**: Нет поля, которое говорит «этот узел — макро-объект, а этот — его детализация». Unit Pipeline (P0→P5) — фазы исследования, а не атрибут сущности. PropagationEngine не знает, что сигнал на `/org/sa/aramco` (L0 macro) и сигнал на facility Aramco (L2) — разные масштабы одной сущности.

**Решение**: Маркер `resolutionHint` — аннотация, не валидация.

| Значение | Семантика | Примеры |
|----------|-----------|---------|
| `macro` | Макро-объект (ORG, MARKET, GEO_REGION country-level) | `/org/sa/aramco`, `/market/ae/oil-gas/upstream` |
| `unit` | Подразделение/сегмент внутри macro | OrgUnit, submarket |
| `detail` | Конкретный объект (facility, route, person) | `/fac/aramco/ghawar-refinery` |
| `unspecified` | По умолчанию | legacy nodes |

**Назначение**: UI zoom, propagation (учёт масштаба при damping/aggregation), explainability.

**Реализовано**:
- [x] `ResolutionHint string` в `HorizonFragment` (macro|unit|detail|unspecified)
- [x] Вывод при создании/обновлении: `sphere_inferrer.InferResolutionHint` по правилам для GOV, ORG, MARKET, PRODUCT, FACILITY, GEO_REGION, COMMUNITY

### Контракт 3: Zoom (Drill / Roll-up) — отложен

**Основа**: SpherePaths + ResolutionHint + ParentPath уже задают иерархию; ZoomResolver будет опираться на них.

**Проблема**: Unit Pipeline делает drill для ORG→UNIT, но это hardcoded flow. Для markets, communities, gov — нет аналога. Нет операции «покажи то же самое, но детальнее».

**Решение**: Единый интерфейс `zoomInto(node)` → возвращает дочерние узлы того же смыслового домена.

| Тип узла | Реализация zoom | Источник дочерних |
|----------|-----------------|-------------------|
| ORG | units, subsidiaries | `OrgUnitProfile`, `SUBSIDIARY_OF` edges |
| MARKET | sub-segments | `HAS_SUBMARKET`, `ParentMarketPath` |
| COMMUNITY | sub-groups | `HAS_SUBGROUP` edges |
| GOV | subordinate agencies | `ParentGovPath`, `ADMINISTERS` |
| GEO_REGION | admin1, cities | path prefix match `/loc/{cc}/{admin1}/*` |
| PRODUCT | sub-products, SKU | path prefix, taxonomy |

**API**: `GET /api/horizon/fragments/{id}/children?depth=1` или аналогичный. Реализация — type-specific, но контракт единый.

**TODO**:
- [ ] Определить `ZoomResolver` interface
- [ ] Реализовать по типам (ORG, MARKET, COMMUNITY, GOV, GEO, PRODUCT)
- [ ] Endpoint или метод в graph service

### Контракт 4: Metric Composition (агрегация метрик)

**Проблема**: Метрики живут на конкретном узле. Propagation распространяет сигнал, но нет контракта «метрика организации — агрегат метрик её facility». Нет связи: «метрика X на узле A является roll-up метрики X на дочерних B, C, D».

**Решение**: Не обязательно автоматическая агрегация. Явная связь — аннотация или контракт:

1. **Семантическая совместимость**: метрика `/metric/risk/logistics/capacity` на macro-узле имеет тот же смысл, что на detail-узле. Propagation может учитывать это при roll-up.
2. **Опциональный roll-up**: для узлов с `resolutionHint=macro` и известными детьми — вычислять агрегат (max, weighted avg) по дочерним метрикам того же axis. Не заменять propagation, а дополнять.
3. **Provenance**: если метрика macro — roll-up, в provenance указывать `method: "rollup"` и `sourceNodeIds: [...]`.

**TODO**:
- [ ] Задокументировать контракт семантической совместимости метрик по уровням
- [ ] Опционально: `MetricComposition` в propagation/rollup — учитывать дочерние узлы при score
- [ ] Не блокировать MVP: propagation остаётся primary

### Контракт 5: Edge Projection (проекция рёбер по масштабу)

**Проблема**: Ребро `SUPPLIES_TO` между `/org/sa/aramco` и `/org/cn/sinopec` — одно. Ребро между refinery и terminal — другое. Они никак не связаны, хотя второе — «то же самое, но на другом масштабе».

**Решение**: Не жёсткая связь, но **проекция** — знание о том, что edge на L2 является детализацией edge на L0.

**Варианты**:
1. **Мягкая связь**: qualifier `macroEdgeId` на micro-edge — ссылка на «родительское» макро-ребро (если известно).
2. **Вывод**: при zoom — показывать, через какие конкретные связи реализуется макро-связь. BFS от macro source/target к detail nodes.
3. **Propagation**: macro edge — «shadow» множества micro edges. При propagation на macro можно учитывать плотность micro edges (опционально).

**TODO**:
- [ ] Определить, нужна ли явная проекция (qualifier) или достаточно вывода через граф
- [ ] Для zoom UI: при клике на macro edge — highlight реализующие micro paths
- [ ] Отложено: propagation с учётом edge density (низкий приоритет)

### Сводка: приоритеты реализации

| Контракт | Приоритет | Зависимости | Статус |
|----------|-----------|-------------|--------|
| 1. Canonical Path Hierarchy | P1 | — | ✅ `ParentPath` на фрагменте; sync из `ParentGovPath`/`ParentMarketPath` |
| 2. Resolution Level | P1 | — | ✅ `ResolutionHint` на фрагменте; `InferResolutionHint` по типам |
| Sphere Taxonomy | P1 | — | ✅ L1/L2/L3+ иерархия; `SpherePaths` на фрагменте; `InferSpherePaths` |
| Chokepoint Transit Amplification | P1 | — | ✅ `TransitAmplify` в BFS; `IsChokepointFragment`; `GeoFeatureType` |
| Units Deprecation | P1 | Sphere Taxonomy | ✅ Код заморожен; `SpherePathsFromUnitCode` |
| 3. Zoom | P2 | 1, 2 | ❌ `ZoomResolver` interface + реализации по типам |
| 4. Metric Composition | P3 | 2, 3 | ❌ Документация; roll-up опционально |
| 5. Edge Projection | P3 | 3 | ❌ Qualifier или вывод; UI highlight |

### Следующие шаги: спланировать и реализовать

Отложенные задачи из Sphere Taxonomy & Resolution плана. Зависят от того, что SpherePaths/ResolutionHint/ParentPath уже заполняются на новых фрагментах.

| # | Задача | Зависимости | Приоритет | Описание |
|---|--------|-------------|-----------|----------|
| 1 | **Backfill SpherePaths/ResolutionHint/ParentPath** | Sphere Taxonomy ✅ | P1 | Миграционный скрипт: пройти все существующие фрагменты, вызвать `InferSpherePaths` / `InferResolutionHint`, записать. Отдельный `cmd/tools/horizon_backfill_sphere_resolution`. |
| 2 | **ZoomResolver** (Contract 3) | Backfill, Contract 1+2 ✅ | P2 | `ZoomResolver` interface: `ZoomInto(node) → children`. Реализации по типам: ORG (subsidiaries), MARKET (subsegments), GOV (subordinate agencies), GEO (admin hierarchy), COMMUNITY (subgroups), PRODUCT (path prefix children). Endpoint: `GET /api/horizon/fragments/{id}/children?depth=1`. |
| 3 | **ProductHierarchyBuilder** | Backfill | P2 | Явный builder (аналог `OrgHierarchyBuilder`, `MarketHierarchyBuilder`): при создании продукта — найти/создать родителя по path prefix. Сейчас `ParentPath` выводится из prefix, но без гарантии существования родительского фрагмента. |
| 4 | **Metric Composition / roll-up** (Contract 4) | ZoomResolver | P3 | Контракт семантической совместимости метрик по уровням. Опционально: `MetricComposition` в propagation/rollup — агрегат (max, weighted avg) по дочерним узлам для `resolutionHint=macro`. Provenance: `method: "rollup"`, `sourceNodeIds`. |
| 5 | **Edge Projection** (Contract 5) | ZoomResolver, Resolution backfill | P3 | Проекция рёбер по масштабу: qualifier `macroEdgeId` на micro-edge; или вывод через граф при zoom. UI: клик на macro edge → highlight реализующие micro paths. Propagation: опционально учитывать edge density. |
| 6 | **Sphere-aware signal propagation** | SpherePaths backfill | P2 | Фильтрация/приоритизация propagation paths по пересечению сфер. Сигнал в `/sphere/energy` сильнее распространяется по узлам с совпадающими SpherePaths; слабее — по узлам из несвязанных сфер. Коэффициент `sphereOverlapFactor` в damping. |
| 7 | **~~Unit pipeline deletion~~** | ~~SpherePaths proven at scale~~ | ✅ Done | Удалено: `UnitCode`, `OrgUnitProfile`, `UnitGraphEdge`, `UnitSupplyLinkQualifier`, `UnitTemplatePair`, unit pipeline (P0–P5), unit graph snapshots, unit dive results, все unit-related функции в engine/agents/prompts. |
| 8 | **Dynamic chokepoint discovery** | Chokepoint amplification ✅ | P3 | Автоматическое обнаружение bottleneck-узлов по топологии графа (high betweenness centrality) вместо явного тегирования. Дополняет ручные/seed chokepoints, не заменяет. |

### Фокус реализации: логика пайплайна, не промпты

**Ключевой принцип**: Уровни, агрегация, проекция и zoom должны быть заложены в **логику пайплайна** — в структуру контрактов, фазы, правила выбора scope и сборку контекста. Промпты — лишь один слой; если логика не учитывает resolution/уровни, промпты будут компенсировать это ad-hoc и нестабильно.

**Пять фокусных направлений**:

1. **Уровни в графе и в canonical path**
   - Имеет смысл выделять уровни для разных типов сущностей — и в графе (depth, parent, resolutionHint), и в структуре path (L0/L1/хвост).
   - Оба слоя согласованы: path-иерархия и графовая иерархия не противоречат друг другу.

2. **Агрегация сущностей и метрик**
   - Имеет смысл агрегировать сущности (org ↔ fac, market ↔ submarket) и, как следствие — агрегировать метрики.
   - org — агрегат своих facilities; roll-up метрик по иерархии (max, weighted avg). Propagation и roll-up — два дополняющих механизма.

3. **Проекция рёбер (транзитивность)**
   - Цепочка a→b→c→d и прямое a→d — одна и та же связь на разных масштабах. Macro-edge — «тень» micro-path.
   - При zoom: «через какие конкретные шаги реализуется a→d». При propagation: macro-edge как сжатый путь.

4. **Zoom in на уровне системы**
   - Zoom — системная операция (ZoomResolver, endpoint), не UI-трюк.
   - **Units deleted**: UnitCode/OrgUnitProfile/Unit Pipeline полностью удалены. Подразделения с собственным lifecycle материализуются как обычные HorizonFragment (ORGANIZATION) с `ResolutionHint=unit`, `ParentPath` родителя, `SpherePaths` уже родителя.

5. **Учёт в логике пайплайна**
   - DomainContractEngine, Unit Pipeline, Resolve → Steer → Gate — все фазы должны учитывать resolution/уровни.
   - **Правила выбора scope**: что искать на macro vs unit vs detail — заложено в логику, не в свободный текст промпта.
   - **Сборка контекста**: какие узлы/рёбра в scope в зависимости от resolution — детерминированно.
   - **missingInfo / enrichment**: формулируются из структуры (facets, cross-facet expectations, resolution), а не только из LLM-интерпретации.
   - TODO: вынести resolution-aware логику в отдельные модули (scope resolver, context builder), чтобы промпты получали уже отфильтрованный/собранный контекст.

**Чеклист интеграции в пайплайн**:

| Компонент | Что учитывать |
|-----------|---------------|
| IntentResolver / DomainContractEngine | resolution в domain contract (macro/unit/detail expectations) |
| Steer / Gate | scope по resolution (macro → units, unit → facilities) |
| Scout directives | что искать на каком уровне (не «всё подряд», а по resolution) |
| Research/Critic prompts | получают pre-filtered context (узлы, edges, resolution hints) |
| ZoomResolver | единая точка входа для drill-down |
| Propagation / Rollup | resolutionHint при damping, roll-up по иерархии |

**Формулировка для whitepaper** (как отвечать на вопрос «узлы имеют свойства фрактальности?»):

> Граф знаний обладает свойством масштабной инвариантности: узлы на разных уровнях абстракции описываются единым интерфейсом и обрабатываются одним механизмом распространения сигналов. Это не математическая фрактальность, а архитектурная самоподобность — один и тот же «круг на воде» проходит через все масштабы одним алгоритмом. Пять контрактов (path hierarchy, resolution level, zoom, metric composition, edge projection) делают эту самоподобность из эмерджентной явной.

---

## Текущий план: стабилизация → улучшения (backend-first)

Ниже — актуальный TODO-план, который отражает **текущее состояние реализации** (код уже в основном готов) и следующие шаги.
Принцип: **сначала стабилизируем текущую модель**, затем добавляем минимальные онтологические улучшения, не раздувая таксономию.

### A) Стабилизация текущей модели (MVP hardening)

**Цель**: сделать текущую систему предсказуемой в проде (без “скрытых” несоответствий данных/доков, без ручной магии).

**TODO**:
- **Срочные задачи**
  - Вынести метрики из фрагментов в оверлей
  - [x] Реализовать подгрузку материалов вручную из фрагмента-продукта через wikidata - срочно
    - POST `/api/horizon/fragments/{id}/expand-materials` — синхронный вызов, возвращает статистику
  - [x] Добавить клиентов организации в скаутинг, вызываемый вручную - тоже срочно
    - Используется ручной `POST /api/horizon/fragments/{id}/refresh` с `specialties=["supply_chain"]` (поиск downstream customers в Scout/Critic/Research).
- **Wikidata QID resolver (suggest-only → gated auto-apply)**
  - Вынести поиск/скоринг Wikidata QID из `cmd/tools/horizon_find_missing_wikidata` в ядро как **suggest-only resolver** (без авто-записи).
  - Добавить кэширование/TTL, type-gating (например, `P31=Q5` для PERSON), и телеметрию качества (score распределения, false positives).
  - Добавить режим **auto-apply** только под feature-flag + строгие пороги/правила + dry-run.
  - Перед включением auto-apply: прогнать тщательную проверку качества на репрезентативной выборке (precision/recall, ручной аудит top ошибок).
- **Vertex AI Search для Horizon fragments**
  - ✅ Инфра реализована: sync в GCS, `VERTEX_SEARCH_HORIZON_DATA_STORE_ID`, PolicyChecker prefetch. TODO (операционные): завершить overwrite re-export, перезапустить backfill (батчи), убедиться что поиск по fragments возвращает результаты.
  - Зафиксировать/проверить необходимые Firestore индексы под запросы Horizon.
- **QueryAddress**
  - ✅ Реализовано: `query_address.go`, `query_resolver.go`, `query_refresh_job.go`, `splitScope`, lens service. TODO: подтвердить полноту “adult” (AST, скобки, NOT, корректный OR).
  - Подтвердить поведение `sphere:`: работает через `ontology_facts` и поддерживает только exact match (без wildcard).
  - Подтвердить поведение `location:`: best-effort `/loc/{country}/{admin1}/na` из `homeLocation`.
  - Убедиться, что background refresh корректно управляет подписками (added/removed paths) и не оставляет “мёртвых” подписок.
- **Документация (backend-first)**
  - Держать `architecture/taxonomy.md` и `architecture/query-address.md` синхронно с кодом (не обещать enforcement, которого нет).
  - В `IMPLEMENTATION_AUDIT.md` поддерживать актуальный “snapshot” (деплой/инфра/текущее состояние миграций).
- **Операционные контуры**
  - После завершения backfill: безопасно включить обратно Cloud Tasks / Scout queue (если была на паузе).
- **Technical debt: нейминг RAG → document indexing (отложено)**
  - После рефакторинга document flow (Vertex AI Search вместо chunks/embeddings) остались устаревшие упоминания RAG в инфраструктурном коде.
  - TODO: переименовать в CLI-утилитах, README и BigQuery — `RAG_DOCUMENTS_BUCKET`, "RAG documents", "RAG Documents bucket" и т.п. в пользу "documents bucket" / "document indexing".
  - Env `RAG_DOCUMENTS_BUCKET` оставлен для обратной совместимости; при миграции можно ввести алиас `DOCUMENTS_BUCKET` и постепенно вывести RAG из названий.

**Критерии приемки**:
- Re-export/backfill завершены, поиск по fragments в Vertex работает в проде.
- QueryAddress ведёт себя детерминированно (скобки/NOT/OR), документы и код не противоречат друг другу.
- Нет “обязательных” ручных шагов, кроме чётко описанных в docs.

### B) Улучшения онтологии (после стабилизации; минимальные элементы, без усложнения таксономии)

**Цель**: добавить выразительность “реальности” через универсальные онтологические оси (time/roles/modality/jurisdiction), не пытаясь сделать таксономию полной онтологией мира.

**TODO**:
- **High-profile signals (global vs lens-specific)** — ✅ **Global реализован**
  - **Цель (бизнес)**: быстро подсвечивать пользователю “критические участки” (влияние/власть/узкие места), не пересчитывая признаки каждый раз на фронте.
  - **Global (✅ сделано)**:
    - В `HorizonFragment` хранится `highProfile` (`backend/internal/horizon/models/high_profile.go`).
    - Формат: `highProfile.signals[<key>] = { score, level, confidence, evidence }`. Пересчёт через `highprofile.Calculator`, backfill через `horizon_backfill_high_profile`.
  - **Lens-specific (❌ отложено)**:
    - Хранить признаки “в контексте lens” в overlay-артефакте (например `horizon_materialized_artifacts` kind=`cockpit_overlay` или аналог),
      чтобы не “портить” глобальный смысл меток вроде “на критическом пути”/“ключевой узел именно этого lens”.
    - Минимум: `lensId + fragmentId -> { flags, score, evidence(edgeIds) }` с TTL/updatedAt.
- **Market Segments (канон `/segment/...` + признаки организации)** — ✅ **Реализовано**
  - В `HorizonFragment` есть `marketSegments: []string` и `orgSize`. Формат сегмента: `/segment/{domain}/{vertical}/{geo}`. Scout/Critic/Research извлекают и требуют эти поля. Backfill: `horizon_llm_backfill_org_segments_size`. `segment_market_links.go` материализует derived market nodes.
  - **TODO (опционально)**: Материализовать сегменты отдельными фрагментами с тем же canonical path (`/segment/...`) и продублировать связь “org → segment” через отдельный edge type (для индексации/UX).
- **Time (единый контракт времени)** — частично
  - `observedAt` есть в `MetricValue`, `OntologyFact`. `ValidFrom`/`ValidTo` есть на `DependencyEdge`. TODO: формализовать `effectiveFrom`/`effectiveTo` для всех relations и задокументировать контракт.
- **Modality (наблюдаемость/сценарность)** — частично
  - `edgeModality` (actual/projected/historical/potential) есть в qualifiers edges, `normalizeTemporalModality`, policy_checker. TODO: добавить `modality` на уровень фактов/утверждений; обновить Watchdog/Scout правила для propagation.
- **Jurisdiction / Scope**
  - Явно договориться, что такое `locationPath`/`jurisdictionPath` для фактов и как он соотносится с `/loc/...`.
  - Свести к одному каноническому формату и задокументировать.
- **Roles/Relations как слой реальности** — ✅ **AUDITED_BY, RELATIVE_OF, RELATED_TO реализованы**
  - В taxonomy и edge_normalizer добавлены: `AUDITED_BY` (ORG/FACILITY → ORG), `RELATIVE_OF` (PERSON ↔ PERSON), `RELATED_TO` (PERSON ↔ PERSON). Scout/Critic/Research промпты обновлены.
  - **TODO (stability)**: разобраться, почему LLM/пайплайн регулярно эмитит `REGULATES` в виде `/org/... → /gov/...` (инверсия “regulated → regulator”). Сейчас это нужно best-effort канонизировать свапом в `/gov/... → /org/...`, но первопричину (промпты/инструкции/пример) стоит исправить.

---
### Большой раздел работ (единый блок): расширение онтологии/таксономии Horizon
Отсюда и до секции **“Обработка метрик при propagation (семантика поверх "доставки дельт")”** включительно — один целостный блок работ: новые canonical paths, новые fragment/edge types, нормализация, требования Critic/Scout, lifecycle/time, edge metrics/temporal validity, и интеграция с propagation.
---
- **Organization Identity Anchor & Corporate Transitions (rename/rebrand/successor)** — ❌ **TODO**
  - Вынесено в отдельный документ для реализации отдельным агентом: `docs/horizon/implementation/ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md`.
  - В `roadmap.md` оставляем только статус/ссылку, чтобы не перегружать основной roadmap деталями национальных реестров, identity anchor и transition-логики.

- **Natural Resource Deposit (`/deposit/`) и EXTRACTS_FROM** — ✅ **Реализовано**
  - `FragmentTypeDeposit`, canonical path `/deposit/{resource-slug}/{deposit-slug}`, validation_policy, edge_normalizer, supply_chain_parser, research_profile. EXTRACTS_FROM (FACILITY → DEPOSIT), OPERATES_IN (ORG → DEPOSIT), PRODUCES (DEPOSIT → PRODUCT), LOCATED_IN (DEPOSIT → GEO_REGION). Канонизация ORG EXTRACTS_FROM DEPOSIT → OPERATES_IN (best-effort).
- **Требования к скаутингу металлургических и горнодобывающих компаний (metal & mining)**
  - При скаутинге компаний из секторов `/Materials/Mining/*` (Precious_Metals, Industrial_Metals, Rare_Earths) scout-critic **обязательно должен требовать наличие связанных объектов типа `/fac`** (Facility) и/или `/deposit/` (Natural Resource Deposit).
  - Для каждого связанного `/fac` требуется следующая информация:
    - **Продукты**: список продуктов, которые добываются/производятся на объекте (связи `PRODUCES` с `/product/...` путями).
    - **Запасы и добыча по каждому продукту отдельно**:
      - **Разведанные запасы** (proven reserves) для каждого продукта: объёмы с единицами измерения из канонического словаря (`allowedMetricUnits` в `horizon_taxonomy`).
      - **Вероятные запасы** (probable reserves) для каждого продукта: объёмы с единицами измерения из канонического словаря.
      - **Добыча** (production/extraction) для каждого продукта: объёмы с единицами измерения из канонического словаря.
  - Если запасы известны только на уровне месторождения (deposit), а не конкретного `/fac`:
    - Создавать сущность `/deposit/` с reserves-метриками.
    - Связывать `/fac → EXTRACTS_FROM → /deposit/`.
  - Единицы измерения должны соответствовать каноническому словарю (`allowedMetricUnits`), например: `metric_tons`, `kt`, `mt`, `oz`, `moz`, `kg`, `lbs` и т.д.
  - Обновить Critic Agent: если для metal & mining компании отсутствуют связанные `/fac` объекты или для `/fac`/`/deposit/` отсутствует информация о продуктах/запасах/добыче по каждому продукту — добавлять в `missingInfo` конкретные вопросы для поиска этой информации.
  - Обновить Scout/Research промпты: при разведке metal & mining компаний явно извлекать информацию о производственных объектах (`/fac`), месторождениях (`/deposit/`), их продуктах, а также запасы и добычу **отдельно для каждого продукта**.
- **Требования к скаутингу нефтегазовых компаний (oil & gas)**
  - При скаутинге компаний из секторов `/Energy/Fossil_Fuels/Oil_Gas_*` (Upstream, Midstream, Downstream) scout-critic **обязательно должен требовать наличие связанных объектов типа `/fac`** (Facility) и/или `/deposit/` (Natural Resource Deposit — basin/field).
  - Для **upstream** компаний (exploration & production):
    - Требовать связанные `/deposit/` (месторождения/бассейны) с reserves-метриками.
    - Требовать связанные `/fac/` (скважины, платформы, терминалы) с production-метриками.
    - Связи: `/fac → EXTRACTS_FROM → /deposit/`, `/org → OPERATES_IN → /deposit/`.
  - Для **midstream** компаний (pipelines, storage, LNG):
    - Требовать связанные `/fac/` (трубопроводы, хранилища, LNG-терминалы) с capacity-метриками.
  - Для **downstream** компаний (refining, petrochemicals):
    - Требовать связанные `/fac/` (НПЗ, нефтехимические заводы) с capacity/throughput-метриками.
  - Для каждого связанного `/deposit/` (для upstream) требуется:
    - **Продукты**: crude oil, natural gas, condensate, NGL (связи `PRODUCES` с `/product/...`).
    - **Запасы по классификации SPE-PRMS**:
      - `reserves_proven_{product}` (1P): доказанные запасы
      - `reserves_probable_{product}` (2P): вероятные запасы
      - `reserves_possible_{product}` (3P): возможные запасы (опционально)
    - **Добыча**: `production_{product}` — текущий уровень добычи.
  - Единицы измерения для O&G: `bbl`, `mbbl`, `mmbbl` (нефть), `bcf`, `tcf`, `mcf` (газ), `boe` (barrels of oil equivalent), `bpd`, `mbpd` (добыча в день).
  - Обновить Critic Agent: если для O&G upstream компании отсутствуют связанные `/deposit/` или `/fac/`, или отсутствует информация о reserves/production — добавлять в `missingInfo` конкретные вопросы.
  - Обновить Scout/Research промпты: при разведке O&G компаний явно извлекать информацию о месторождениях (`/deposit/`), производственных объектах (`/fac/`), и reserves/production **с корректными единицами измерения**.
- **Новый тип сущности: Natural Resource Deposit (`/deposit/`)**
  - ✅ **Реализовано** (см. выше). Ниже — спецификация для справки.
  - **Назначение**: моделирование геологических единиц (месторождения, бассейны, формации) с ресурсными характеристиками. Отличается от `/loc/` (административно-географическая единица) и `/fac/` (производственный объект).
  - **Canonical path format**: `/deposit/{resource-slug}/{deposit-slug}`
    - `{resource-slug}`: основной (primary) тип ресурса в deposit'е
    - `{deposit-slug}`: уникальный идентификатор месторождения (lowercase, slugified)
  - **Примеры canonical paths**:
    - `/deposit/crude-oil/permian-basin` — Permian Basin (Texas/New Mexico)
    - `/deposit/crude-oil/ghawar` — Ghawar Field (Saudi Arabia)
    - `/deposit/natural-gas/north-sea` — North Sea gas fields
    - `/deposit/lithium/atacama-salar` — Salar de Atacama (Chile)
    - `/deposit/copper/escondida` — Escondida copper deposit (Chile)
    - `/deposit/iron-ore/pilbara` — Pilbara iron ore province (Australia)
    - `/deposit/gold/witwatersrand` — Witwatersrand Basin (South Africa)
    - `/deposit/uranium/olympic-dam` — Olympic Dam (Australia, polymetallic)
  - **FragmentType**: `DEPOSIT`
  - **Deposit-specific атрибуты** (через `Identifiers` или новые поля):
    - `depositKind`: классификация типа месторождения
      - O&G: `basin`, `field`, `formation`, `play`, `block`
      - Mining: `deposit`, `belt`, `province`, `district`, `salar`
    - `primaryResource`: основной ресурс (используется в canonical path)
    - `secondaryResources`: дополнительные ресурсы (для polymetallic deposits)
    - `geologicalAge`: геологический возраст (опционально)
    - `discoveryYear`: год открытия (опционально)
  - **Multi-resource deposits**: если deposit содержит несколько ресурсов (напр. Olympic Dam = uranium + copper + gold):
    - Primary resource → идёт в canonical path
    - Secondary resources → хранятся в `Identifiers["secondaryResources"]` или `Tags`
    - Reserves/production хранятся отдельно для каждого ресурса в `State`
  - **Связи (relation types)**:
    - `EXTRACTS_FROM` (новый тип): `/fac/` → `/deposit/` — facility добывает из месторождения
    - `LOCATED_IN`: `/deposit/` → `/loc/` — месторождение находится в регионе
    - `OPERATES_IN`: `/org/` → `/deposit/` — компания работает в месторождении
    - `PRODUCES`: `/deposit/` → `/product/` — месторождение производит ресурс (агрегированно)
  - **Хранение метрик reserves/production** (используем существующий `State map[string]MetricValue`):
    - Naming convention для ключей:
      - `reserves_proven_{product}` — доказанные запасы (1P / SPE-PRMS proven)
      - `reserves_probable_{product}` — вероятные запасы (2P / SPE-PRMS probable)
      - `reserves_possible_{product}` — возможные запасы (3P / SPE-PRMS possible)
      - `production_{product}` — текущая добыча
    - `MetricValue.Value` — абсолютное значение (float64, поддерживает большие числа)
    - `MetricValue.Unit` — единица измерения из `allowedMetricUnits`
    - `MetricValue.ObservedAt` — дата актуальности данных (важно для reserves)
    - `MetricValue.Confidence` — уверенность в оценке
    - `MetricValue.Provenance` — источник данных
  - **Пример структуры для O&G deposit**:
    ```json
    {
      "canonicalPath": "/deposit/crude-oil/permian-basin",
      "type": "DEPOSIT",
      "labels": {"en": "Permian Basin"},
      "identifiers": {
        "depositKind": "basin",
        "primaryResource": "crude-oil",
        "secondaryResources": "natural-gas,ngl"
      },
      "homeLocation": {
        "name": "West Texas / Southeast New Mexico",
        "iso3166_1": "US",
        "iso3166_2": "US-TX"
      },
      "state": {
        "reserves_proven_crude_oil": {"value": 46000000000, "unit": "bbl", "confidence": 0.95, "observedAt": "2024-01-15T00:00:00Z"},
        "reserves_probable_crude_oil": {"value": 12000000000, "unit": "bbl", "confidence": 0.8},
        "reserves_proven_natural_gas": {"value": 75, "unit": "tcf", "confidence": 0.9},
        "production_crude_oil": {"value": 5500000, "unit": "bpd", "confidence": 0.95}
      }
    }
    ```
  - **Пример структуры для mining deposit**:
    ```json
    {
      "canonicalPath": "/deposit/lithium/atacama-salar",
      "type": "DEPOSIT",
      "labels": {"en": "Salar de Atacama"},
      "identifiers": {
        "depositKind": "salar",
        "primaryResource": "lithium",
        "secondaryResources": "potassium"
      },
      "homeLocation": {
        "name": "Atacama Desert, Chile",
        "iso3166_1": "CL",
        "iso3166_2": "CL-AN"
      },
      "state": {
        "reserves_proven_lithium_carbonate": {"value": 6800000, "unit": "metric_tons", "confidence": 0.9},
        "reserves_probable_lithium_carbonate": {"value": 2100000, "unit": "metric_tons", "confidence": 0.75},
        "production_lithium_carbonate": {"value": 85000, "unit": "metric_tons_per_year", "confidence": 0.95},
        "reserves_proven_potassium": {"value": 1200000000, "unit": "metric_tons", "confidence": 0.85}
      }
    }
    ```
  - **Связь `/deposit/` и `/product/`** — разные сущности:
    - `/deposit/` — геологический объект с запасами
    - `/product/` — товар/материал в supply chain
    - Один deposit → много products (Atacama даёт lithium и potassium)
    - Один product → много deposits (lithium добывают из разных месторождений)
  - **Scout/Critic интеграция**:
    - При скаутинге extractive company → проверять наличие `/deposit/` или `/fac/` с reserves
    - Если reserves известны только для deposit (не для fac) → создать `/deposit/`, привязать reserves туда
    - Если reserves известны для конкретного fac → хранить на fac
    - Critic должен требовать `/deposit/` для upstream O&G и mining companies
- **Расширение таксономии продуктов для extractive industries**
  - **AllowedProductTypes** — добавить:
    - `hydrocarbon` — crude oil, condensate, NGL
    - `fuel` — refined fuels (gasoline, diesel, jet fuel)
    - `petrochemical` — ethylene, propylene, polymers
  - **AllowedProductStages** — добавить:
    - `liquefaction` — LNG production
    - `cracking` — petrochemical cracking
  - **KnownProducts** — расширить словарь:
    - **O&G Upstream**: `crude-oil`, `natural-gas`, `condensate`, `ngl`
    - **O&G Midstream/Downstream**: `lng`, `gasoline`, `diesel`, `jet-fuel`, `naphtha`, `ethylene`, `propylene`
    - **Mining (расширить)**: `iron-ore`, `copper-ore`, `copper-concentrate`, `refined-copper`, `gold-ore`, `gold-bullion`, `rare-earth-ore`, `neodymium-oxide`, `uranium-ore`, `yellowcake`
  - **ProductSynonyms** — добавить алиасы:
    - O&G: `oil` → `crude-oil`, `petroleum` → `crude-oil`, `gas` → `natural-gas`, `petrol` → `gasoline`, etc.
    - Mining: `iron` → `iron-ore`, `copper` → `copper-ore`, `gold` → `gold-ore`, `u3o8` → `yellowcake`, etc.
  - **Примеры canonical paths для продуктов**:
    - `/product/raw/hydrocarbon/extraction/crude-oil`
    - `/product/raw/hydrocarbon/liquefaction/lng`
    - `/product/finished/fuel/refining/gasoline`
    - `/product/component/petrochemical/cracking/ethylene`
    - `/product/raw/mineral/extraction/iron-ore`
    - `/product/component/mineral/refining/refined-copper`
- **Опциональный последний сегмент: конкретное название продукта (model/SKU)**
  - **Формат**:
    ```
    Goods:    /product/{category}/{type}/{stage}/{slug}[/{model-slug}]
    Services: /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}]
    ```
  - **Назначение**: Позволяет моделировать конкретные модели/SKU/брендированные продукты:
    - `/product/finished/mechanical/manufacturing/excavator` — экскаваторы (generic)
    - `/product/finished/mechanical/manufacturing/excavator/cat-390f` — конкретная модель Caterpillar 390F
    - `/product/finished/mechanical/manufacturing/excavator/komatsu-pc2000` — Komatsu PC2000
  - **Правила**:
    - Последний сегмент **опционален** — можно использовать generic slug
    - Model-slug должен быть **официальным названием** (или его каноническим slug'ом)
    - Slug'ы нормализуются: lowercase, дефисы, без спецсимволов
    - Pattern matching работает: `/product/*/mechanical/*/excavator/*` поймает все модели экскаваторов
  - **Примеры для Goods**:
    | Категория | Generic product | Конкретная модель |
    |-----------|-----------------|-------------------|
    | Automotive | `/product/finished/mechanical/assembly/electric-vehicle` | `.../electric-vehicle/tesla-model-3` |
    | Semiconductors | `/product/component/electronic/manufacturing/gpu` | `.../gpu/nvidia-h100` |
    | Pharma | `/product/finished/healthcare/manufacturing/vaccine` | `.../vaccine/pfizer-comirnaty` |
    | Aircraft | `/product/finished/mechanical/assembly/commercial-aircraft` | `.../commercial-aircraft/boeing-737-max` |
    | Mining Equipment | `/product/finished/mechanical/manufacturing/haul-truck` | `.../haul-truck/caterpillar-797f` |
  - **Примеры для Services**:
    | Категория | Generic service | Конкретное предложение |
    |-----------|-----------------|------------------------|
    | Cloud | `/product/service/it/cloud/platform` | `.../platform/aws-ec2` |
    | CRM | `/product/service/it/software/crm-saas` | `.../crm-saas/salesforce-sales-cloud` |
    | Banking | `/product/service/financial/banking/payment-processing` | `.../payment-processing/stripe-connect` |
  - **Когда использовать model-slug**:
    - Когда нужно отслеживать **конкретную модель** в supply chain (e.g. "Boeing 737 MAX" vs "Boeing 787")
    - Когда модель имеет **уникальные характеристики** (sanctions, safety issues, etc.)
    - Когда нужно привязать **capacity/production** к конкретной модели
    - **Не обязательно** если generic category достаточно для анализа
  - **Scout/Critic интеграция**:
    - Scout извлекает model name если упоминается в источнике
    - Critic **не требует** model-slug — это enhancement, не mandatory
    - Для связей `PRODUCES` model-slug полезен если production идёт по конкретным моделям
  - **KnownProductModels** — словарь известных моделей (опционально):
    ```go
    KnownProductModels: map[string]ProductModelSpec{
        "tesla-model-3": {GenericSlug: "electric-vehicle", Manufacturer: "tesla"},
        "nvidia-h100":   {GenericSlug: "gpu", Manufacturer: "nvidia"},
        "boeing-737-max": {GenericSlug: "commercial-aircraft", Manufacturer: "boeing"},
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/taxonomy/runtime.go` → обновить `ValidateProductPath()` для опционального 5го сегмента
    - [ ] `backend/internal/models/horizon_taxonomy.go` → опционально `KnownProductModels map[string]ProductModelSpec`
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать model-slug
- **Расширение canonical path для Services (отдельная структура для услуг)**
  - **Проблема**: Текущий формат `/product/{category}/{type}/{stage}/{slug}` не подходит для услуг:
    - `type` (mineral/electronic/chemical) — бессмысленно для IT-консалтинга
    - `stage` (extraction/manufacturing) — бессмысленно для услуг
    - Если domain услуги только в slug — **pattern matching невозможен** для модели рисков
  - **Зачем нужна сегментация**: Модель рисков/ограничений будет:
    1. Канонизировать ограничение: "IT-услуги в RU" → pattern `/product/service/it/*`
    2. Алгоритмически искать в графе edges по pattern match
    3. Сегментность canonical path позволяет matching без знания конкретных slug'ов
  - **Решение**: Отдельный формат для services:
    ```
    Goods (текущее):  /product/{category}/{type}/{stage}/{slug}
    Services (новое): /product/service/{domain}/{subdomain}/{slug}
    ```
  - **AllowedServiceDomains** — новый словарь:
    ```go
    AllowedServiceDomains: []string{
        "it",           // IT services, software, cloud, hosting
        "financial",    // banking, insurance, payments, investment
        "legal",        // law, compliance, audit, notary
        "consulting",   // management, strategy, advisory
        "logistics",    // transport, warehousing, customs
        "engineering",  // R&D, design, architecture
        "media",        // advertising, PR, content, broadcasting
        "telecom",      // communications, internet, mobile
        "education",    // training, certification, academic
        "healthcare",   // medical, pharma, clinical
        "construction", // building, civil engineering
        "security",     // physical, cyber, investigation
        "hr",           // recruitment, staffing, payroll
        "real-estate",  // brokerage, property management
        "other",        // fallback bucket when domain is unclear (must be allowed if used)
    }
    ```
  - **AllowedServiceSubdomains** — по domain:
    | Domain | Subdomains |
    |--------|------------|
    | `it` | `software`, `cloud`, `hosting`, `consulting`, `integration`, `support`, `security`, `data` |
    | `financial` | `banking`, `insurance`, `payments`, `investment`, `audit`, `tax`, `crypto` |
    | `legal` | `corporate`, `litigation`, `compliance`, `ip`, `sanctions`, `immigration` |
    | `consulting` | `management`, `strategy`, `operations`, `hr`, `marketing` |
    | `logistics` | `freight`, `warehousing`, `customs`, `courier`, `cold-chain` |
    | `engineering` | `design`, `rd`, `testing`, `certification`, `maintenance` |
    | `media` | `advertising`, `pr`, `content`, `broadcasting`, `production` |
    | `telecom` | `mobile`, `fixed`, `internet`, `satellite`, `voip` |
    | `other` | `other` |
  - **Примеры canonical paths для services**:
    - `/product/service/it/software/custom-development`
    - `/product/service/it/cloud/hosting`
    - `/product/service/it/consulting/digital-transformation`
    - `/product/service/financial/banking/payment-processing`
    - `/product/service/financial/audit/external-audit`
    - `/product/service/legal/compliance/sanctions-advisory`
    - `/product/service/legal/corporate/m-and-a`
    - `/product/service/logistics/freight/air-cargo`
    - `/product/service/consulting/strategy/market-entry`
  - **Pattern matching для модели рисков**:
    - "Запретить все IT-услуги" → `/product/service/it/*`
    - "Запретить банковские услуги" → `/product/service/financial/banking/*`
    - "Запретить юр. консультации по санкциям" → `/product/service/legal/compliance/sanctions-*`
    - "Запретить все финансовые услуги" → `/product/service/financial/*`
  - **Пример использования с jurisdictions**:
    ```
    Санкция: "IT-услуги из KZ в RU запрещены"
    
    Канонизация ограничения:
      product_pattern: /product/service/it/*
      source_jurisdiction: kz
      target_jurisdiction: ru
    
    Алгоритмическая проверка:
      for edge in edges where edge.type == "SUPPLIES_TO":
          if edge.source.jurisdiction == "kz" 
             && edge.target.jurisdiction == "ru"
             && match(edge.qualifiers.product, "/product/service/it/*"):
              → RISK SIGNAL
    ```
  - **KnownServices** — словарь известных услуг:
    ```go
    KnownServices: map[string]ServiceSpecData{
        "custom-software-development": {Domain: "it", Subdomain: "software"},
        "cloud-hosting": {Domain: "it", Subdomain: "cloud"},
        "payment-processing": {Domain: "financial", Subdomain: "banking"},
        "external-audit": {Domain: "financial", Subdomain: "audit"},
        "sanctions-compliance": {Domain: "legal", Subdomain: "compliance"},
        "air-freight": {Domain: "logistics", Subdomain: "freight"},
    }
    ```
  - **ServiceSynonyms** — алиасы:
    ```go
    ServiceSynonyms: map[string]string{
        "software development": "custom-software-development",
        "app development": "custom-software-development",
        "web hosting": "cloud-hosting",
        "aws": "cloud-hosting",
        "gcp": "cloud-hosting",
        "azure": "cloud-hosting",
        "payment gateway": "payment-processing",
        "sanctions screening": "sanctions-compliance",
        "aml": "sanctions-compliance",
    }
    ```
  - **Интеграция с goods**: Goods сохраняют текущий формат, services — новый:
    - Goods: `/product/{category}/{type}/{stage}/{slug}` где category ≠ "service"
    - Services: `/product/service/{domain}/{subdomain}/{slug}`
    - Общий prefix `/product/` сохраняется для единообразия
  - **Scout/Critic интеграция**:
    - При извлечении услуг Scout должен определять domain/subdomain
    - Critic требует корректный формат для services
    - Если domain не определён → fallback `/product/service/other/{slug}` с missingInfo
  - **Чеклист изменений**:
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedServiceDomains []string`, `AllowedServiceSubdomains map[string][]string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить domains и subdomains
    - [ ] `backend/internal/taxonomy/runtime.go` → `NormalizeServicePath()`, `ValidateServicePath()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → логика определения domain/subdomain для services
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать формат services
- **Расширение allowedMetricUnits для extractive industries**
  - **O&G units (volume)**:
    - `bbl` — barrel (основная единица нефти)
    - `mbbl` — thousand barrels
    - `mmbbl` — million barrels
    - `bcf` — billion cubic feet (газ)
    - `tcf` — trillion cubic feet (газ)
    - `mcf` — thousand cubic feet (газ)
    - `boe` — barrels of oil equivalent (конвертация газ → нефть)
  - **O&G units (rate)**:
    - `bpd` — barrels per day
    - `mbpd` — thousand barrels per day
    - `mcf_per_day` — thousand cubic feet per day
    - `mmcf_per_day` — million cubic feet per day
  - **Mining units (mass)**:
    - `metric_tons` — метрические тонны
    - `kt` — thousand metric tons
    - `mt` — million metric tons
    - `tons` — short tons (US)
    - `oz` — troy ounces (precious metals)
    - `moz` — million troy ounces
    - `kg` — kilograms
    - `g` — grams
    - `lbs` — pounds
    - `carats` — carats (diamonds)
  - **Mining units (rate)**:
    - `metric_tons_per_year` — годовая добыча
    - `oz_per_year` — унции в год (precious metals)
    - `kt_per_year` — тысячи тонн в год
- **Новый relation type: EXTRACTS_FROM**
  - **Направление**: `/fac/` → `/deposit/` (facility извлекает ресурсы из месторождения)
  - **Семантика**: более точная, чем `LOCATED_IN`, отражает операционную связь
  - **Использование**:
    - Шахта `/fac/bhp/escondida-mine` → `EXTRACTS_FROM` → `/deposit/copper/escondida`
    - Скважина `/fac/aramco/ghawar-field-ops` → `EXTRACTS_FROM` → `/deposit/crude-oil/ghawar`
  - **Fallback**: если точная связь неизвестна, можно использовать `LOCATED_IN`
  - **TODO (stability)**: LLM часто эмитит `EXTRACTS_FROM` как `/org/ → /deposit/` когда facility ещё не создан/не найден. Сейчас это нужно канонизировать в `OPERATES_IN` (best-effort), а позже — научиться создавать/находить `/fac/` и мигрировать связь в строгое `/fac/ → EXTRACTS_FROM → /deposit/` (без потери provenance/metrics).
  - **Добавить в AllowedRelationTypes**: `EXTRACTS_FROM`
- **Allowed deposit resource slugs** (новый словарь в runtime taxonomy):
  - **Hydrocarbons**: `crude-oil`, `natural-gas`, `oil-gas` (combined), `coal`, `oil-sands`, `shale-oil`
  - **Base metals**: `copper`, `zinc`, `nickel`, `aluminum`, `tin`, `lead`
  - **Precious metals**: `gold`, `silver`, `platinum`, `palladium`
  - **Battery metals**: `lithium`, `cobalt`
  - **Ferrous**: `iron-ore`, `manganese`, `chromium`
  - **Strategic/Rare**: `rare-earth`, `uranium`, `tungsten`, `molybdenum`
  - **Industrial minerals**: `potash`, `phosphate`, `sulfur`
- **Чеклист изменений для extractive industries support (код + таксономия)**
  - [ ] **CanonicalPrefixes** — добавить `/deposit/` в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `CanonicalPrefixes`
    - `docs/horizon/architecture/taxonomy.md` (документация)
  - [ ] **FragmentType** — добавить `DEPOSIT` в:
    - `backend/internal/horizon/models/common.go` → `FragmentTypeDeposit = "DEPOSIT"`
    - Обновить switch-case в `engine.go`, `taxonomy.go` и других местах, где определяется тип по canonical path prefix
  - [ ] **AllowedRelationTypes** — добавить `EXTRACTS_FROM` в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedRelationTypes`
    - `backend/internal/taxonomy/runtime.go` → `builtInAllowedRelationTypes`
  - [ ] **AllowedMetricUnits** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedMetricUnits`
    - Добавить: `bbl`, `mbbl`, `mmbbl`, `bcf`, `tcf`, `mcf`, `boe`, `bpd`, `mbpd`, `metric_tons`, `kt`, `mt`, `oz`, `moz`, `kg`, `lbs`, `carats`, `metric_tons_per_year`, `oz_per_year`, `kt_per_year`
  - [ ] **AllowedProductTypes** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedProductTypes`
    - Добавить: `hydrocarbon`, `fuel`, `petrochemical`
  - [ ] **AllowedProductStages** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedProductStages`
    - Добавить: `liquefaction`, `cracking`
  - [ ] **KnownProducts** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `KnownProducts`
    - Добавить O&G продукты: `crude-oil`, `natural-gas`, `condensate`, `ngl`, `lng`, `gasoline`, `diesel`, `jet-fuel`, `naphtha`, `ethylene`, `propylene`
    - Добавить mining продукты: `iron-ore`, `copper-ore`, `copper-concentrate`, `refined-copper`, `gold-ore`, `gold-bullion`, `rare-earth-ore`, `neodymium-oxide`, `uranium-ore`, `yellowcake`
  - [ ] **ProductSynonyms** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `ProductSynonyms`
    - O&G: `oil` → `crude-oil`, `petroleum` → `crude-oil`, `gas` → `natural-gas`, `petrol` → `gasoline`, etc.
    - Mining: `iron` → `iron-ore`, `copper` → `copper-ore`, `gold` → `gold-ore`, `u3o8` → `yellowcake`, etc.
  - [ ] **AllowedDepositResources** — новый словарь в:
    - `backend/internal/models/horizon_taxonomy.go` → добавить поле `AllowedDepositResources []string`
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить словарь
    - `backend/internal/taxonomy/runtime.go` → добавить `AllowedDepositResources` в `RuntimeTaxonomy`
    - `backend/internal/taxonomy/cache_firestore.go` → загрузка из Firestore
  - [ ] **Scout/Critic промпты** — обновить для extractive industries:
    - `backend/internal/horizon/scout/agents/scout_agent.go` → добавить `/deposit/` в canonical path rules
    - `backend/internal/horizon/scout/agents/critic_agent.go` → добавить проверку `/deposit/` для mining/O&G
    - `backend/internal/horizon/scout/taxonomy_classifier.go` → добавить DEPOSIT classification
  - [ ] **Critic: обязательные products для FACILITY** (независимо от specialty):
    - Для **любого `/fac/`** Critic должен требовать:
      - Наличие связей `PRODUCES → /product/...` (что производит facility)
      - Наличие связей `CONSUMES → /product/...` (что потребляет facility)
      - Если products отсутствуют — добавлять в `missingInfo` вопросы с требованием canonical `/product/...` paths
    - Для **extractive `/fac/`** (mining: `/Materials/Mining/*`, O&G: `/Energy/Fossil_Fuels/Oil_Gas_*`):
      - Требовать reserves/production метрики **по каждому продукту отдельно** (не агрегированно)
      - Naming convention: `reserves_proven_{product_slug}`, `production_{product_slug}`
      - Если reserves известны только на уровне deposit → требовать `/deposit/` с `EXTRACTS_FROM` связью
    - Текущий gap: Critic спрашивает "what products" только при specialty=products_and_services, и не требует создания `/product/` фрагментов
    - Обновить `critic_agent.go`:
      ```
      - If entity is FACILITY (canonicalPath starts with /fac/):
        - MANDATORY: If no PRODUCES relations with /product/... targets exist, add:
          "What products does this facility produce? Return as /product/{category}/{type}/{stage}/{slug} canonical paths."
        - MANDATORY: If no CONSUMES relations with /product/... targets exist, add:
          "What inputs/materials does this facility consume? Return as /product/... canonical paths."
        - If facility is in extractive sector (mining/O&G based on sectorPath):
          - For each product in PRODUCES: require reserves and production metrics
          - If reserves are at deposit level, require /deposit/ and EXTRACTS_FROM relation
      ```
  - [ ] **Critic: обязательные products/services для ORGANIZATION** (независимо от specialty — это базовая информация):
    - **ВАЖНО**: Это требование применяется **всегда**, не только при `specialty=products_and_services`.
    - **Текущий gap в коде**: `critic_agent.go` (строки 151-155) требует products только если запрошен `specialty=products_and_services`. Это неправильно — products/services нужны для понимания бизнеса **любой** operating company.
    - Для **любого `/org/`** Critic должен определить:
      1. Это **операционная компания** (производит products/services сама)?
      2. Или это **холдинг/головная компания** (производит через дочерние общества)?
    - **Логика определения холдинга**:
      - Если есть `OWNS` или `SUBSIDIARY_OF` (inverse) edges → вероятно холдинг
      - Если `sectorPath` = `/Financials/Capital_Markets/*` или `/Organization/Holding` → холдинг
      - Если `entityPath` содержит "holding", "investment" → холдинг
      - Если нет subsidiaries и не холдинг → операционная компания
    - **Для операционной компании** (`/org/` без subsidiaries):
      - MANDATORY: Требовать связи `PRODUCES → /product/...` или `PRODUCES → /product/service/...`
      - Если products/services отсутствуют — добавлять в `missingInfo`:
        - "What products does <org> manufacture/produce? Return as /product/{category}/{type}/{stage}/{slug} canonical paths."
        - "What services does <org> provide? Return as /product/service/{domain}/{subdomain}/{slug} canonical paths."
    - **Для холдинга/головной компании** (`/org/` с subsidiaries):
      - MANDATORY: **Сначала** требовать subsidiaries через `OWNS` или обратные `SUBSIDIARY_OF`
      - Добавлять в `missingInfo`:
        - "What are the operating subsidiaries of <org>? Return as /org/{iso2}/{slug} canonical paths with OWNS relations and ownership_percent."
      - **Затем** для каждой subsidiary требовать products/services (рекурсивно)
      - Холдинг сам НЕ обязан иметь PRODUCES — products привязаны к subsidiaries
    - **Примеры по секторам** (products/services нужны для ВСЕХ операционных компаний):
      | Сектор | Пример компании | Ожидаемые PRODUCES |
      |--------|-----------------|-------------------|
      | **Technology/IT** | Salesforce | `/product/service/it/software/crm-saas`, `/product/service/it/cloud/platform` |
      | **Technology/IT** | NVIDIA | `/product/component/electronic/manufacturing/gpu`, `/product/service/it/software/cuda` |
      | **Consumer/Retail** | Nike | `/product/finished/consumer/manufacturing/footwear`, `/product/finished/consumer/manufacturing/apparel` |
      | **Healthcare/Pharma** | Pfizer | `/product/finished/healthcare/manufacturing/vaccine`, `/product/finished/healthcare/manufacturing/drug` |
      | **Industrial/Manufacturing** | Caterpillar | `/product/finished/mechanical/manufacturing/excavator`, `/product/finished/mechanical/manufacturing/bulldozer` |
      | **Financial/Banking** | JPMorgan | `/product/service/financial/banking/commercial-lending`, `/product/service/financial/investment/asset-management` |
      | **Energy/Utilities** | NextEra | `/product/raw/energy/generation/electricity`, `/product/service/energy/distribution/grid-services` |
      | **Telecom** | Verizon | `/product/service/telecom/mobile/wireless-service`, `/product/service/telecom/internet/broadband` |
      | **Real Estate** | CBRE | `/product/service/real-estate/brokerage/commercial-brokerage`, `/product/service/consulting/operations/property-management` |
      | **Extractive** (Mining) | BHP | `/product/raw/mineral/extraction/iron-ore`, `/product/raw/mineral/extraction/copper-ore` + reserves/production metrics |
      | **Extractive** (O&G) | ExxonMobil | `/product/raw/hydrocarbon/extraction/crude-oil`, `/product/finished/fuel/refining/gasoline` + reserves/production metrics |
      | **Holding** | Berkshire Hathaway | Нет PRODUCES (холдинг) → требовать OWNS → subsidiaries (GEICO, BNSF, etc.) → на них products |
    - **Порядок проверки Critic**:
      ```
      1. If entity is ORGANIZATION (canonicalPath starts with /org/):
         a. Check if entity has OWNS edges to other /org/ (subsidiaries)
         b. If YES (holding company):
            - If subsidiaries list is incomplete or missing → add to missingInfo:
              "What operating subsidiaries does <org> own? Include ownership percentage."
            - Do NOT require PRODUCES for the holding itself
            - Flag subsidiaries for separate scouting with products/services requirement
         c. If NO (operating company):
            - MANDATORY (ALWAYS, not just for products_and_services specialty):
              If no PRODUCES relations exist → add to missingInfo:
              "What products does <org> produce?" and/or
              "What services does <org> provide?"
            - Require canonical /product/... or /product/service/... paths
      2. For FACILITY (/fac/): always require PRODUCES (as before)
      ```
    - **Связь с `specialty=products_and_services`**:
      - Базовые products/services нужны **всегда** (даже без specialty)
      - Если `products_and_services` запрошен — требовать **дополнительную детализацию**:
        - TRANSFORMS_INTO relations для продуктов
        - CONSUMES relations (inputs/materials)
        - Capacity/volume metrics на edges
	    - **Связь с equity specialty**:
	      - Если запрошена specialty=equity → усиленные требования к ownership structure
	      - Требовать полную цепочку: ultimate parent → intermediate holdings → operating companies
	    - **Next step: first-class `governance` specialty в backend**:
	      - Текущее UI/runtime поведение уже может явно выбирать governance scouting через `domainModes` и `uiScoutingMode`, но backend specialty всё ещё выражается legacy-комбинацией `management + equity`.
	      - Это переходное решение. Целевая модель: отдельная first-class specialty `governance`, которая используется в prompt selection, critic requirements, runtime routing и analytics без маппинга в legacy specialties.
	      - Scope изменения шире, чем просто UI-mode:
	        - taxonomy / specialty registry
	        - scout / critic / research prompt semantics
	        - runtime routing и derived specialties
	        - preflight / job state / telemetry / analytics
	        - backward compatibility для старых jobs и API-клиентов
	      - Переход должен быть staged:
	        1. Ввести `governance` в backend taxonomy/registry как first-class specialty.
	        2. Перенести governance-specific prompt/runtime semantics с `management + equity` на `governance`.
	        3. Оставить compatibility mapping `management + equity -> governance` на переходный период.
	        4. Удалить legacy-derived mapping после миграции runtime, API и аналитики.
	    - **Чеклист**:
	      - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → **УБРАТЬ** условие `if specialty == products_and_services` для базового PRODUCES check
	      - [ ] `critic_agent.go` → добавить логику holding vs operating (всегда, не только при specialty)
	      - [ ] `critic_agent.go` → для `products_and_services` specialty добавить **дополнительные** требования (TRANSFORMS_INTO, CONSUMES, metrics)
	      - [ ] Определить эвристику `isHoldingCompany()` (по OWNS edges, sectorPath, entityPath keywords)
	      - [ ] Для холдингов: сначала subsidiaries, потом products на subsidiaries
	      - [ ] Для операционных: сразу products/services
	      - [ ] Unit tests: проверить что IT/Manufacturing/Retail компании без products → missingInfo
	      - [ ] `backend/internal/horizon/scout/taxonomy.go` / specialty registry → ввести first-class specialty `governance`
	      - [ ] `backend/internal/horizon/scout/agents/*` → переключить governance prompts/requirements с `management + equity` на `governance`
	      - [ ] `backend/internal/http/horizon/*` → использовать `governance` как canonical backend specialty, сохранив временный compatibility mapping
	      - [ ] Runtime/telemetry → перестать считать governance через derived pair `management + equity`
	      - [ ] Migration/regression tests: старые jobs с `management + equity` продолжают отрабатываться как governance
	  - **MissingInfo → Research Plan (и защита от бесконечных циклов)**
    - **Идея**: `missingInfo[]` — это не просто список вопросов, а **план дозапроса/доскаутинга** (grounded research) для конкретной сущности.
    - **Как работает сейчас (код)**:
      - Critic возвращает `missingInfo[]` (вопросы/поисковые запросы).
      - ScoutEngine **один раз** вызывает ResearchAgent с этим списком, не делая циклов внутри одного job.
      - Важно: после research мы **не перегоняем** Critic заново в рамках того же job (иначе легко получить loop).
    - **Где есть защита уже сейчас**:
      - **Idempotency**: terminal jobs (`VERIFIED/FAILED`) не переисполняются (Cloud Tasks retries).
      - **Circuit breaker**: ограничение попыток обработки одного и того же `targetCanonicalPath` внутри одного job (защита от alias/dedup oscillation и повторных retries).
      - **Freshness threshold**: если фрагмент недавно верифицирован и нет новых фактов — ScoutEngine пропускает дорогой scouting/research (для Lens-driven jobs).
      - **Budget caps**: дневные лимиты на USD и output tokens (lens/cascade), чтобы не сжечь бюджет даже при повторных попытках.
    - **Чего НЕ хватает (для полного отсутствия бесконечных циклов между jobs)**:
      - Если `missingInfo` не удалось закрыть (данных нет в источниках), Critic на следующем job может снова запросить то же самое.
      - Нужен слой **“exhaustion/cooldown”** для конкретных missingInfo-вопросов.
    - **Требование (добавить)**: Per-entity “missingInfo exhaustion”
      - Храним попытки по ключу `(tenantId, canonicalPath, missingInfoHash)` в отдельной коллекции:
        - `horizon_missing_info_status/{tenantId}:{canonicalPath}:{hash}`
        - поля: `attempts`, `lastAttemptAt`, `status=open|resolved|exhausted`, `cooldownUntil`, `lastSourcesFingerprint`
      - Правила:
        - Один и тот же missingInfo query **не пытаться** чаще 1 раза в N часов (cooldown), пока не появятся новые факты/источники.
        - Если attempts ≥ K и “новых источников нет” → `status=exhausted` на M дней.
        - Если появились новые источники/факты (fingerprint изменился) → сбрасываем cooldown и пробуем снова.
      - **Stop-conditions** (чётко):
        - MAX attempts per (canonicalPath, query) = K
        - cooldown = N hours between attempts
        - hard TTL для “exhausted” = M days, после чего можно попробовать снова (на случай появления новых данных).
    - **Что показывать UI/аналитике**:
      - “Unresolved missingInfo” как backlog по узлу (что не нашли и почему: exhausted/cooldown).
      - Это позволит пользователю/Watchdog целенаправленно добавить источники или смириться с unknown.
    - **Чеклист изменений**:
      - [ ] `backend/internal/horizon/scout/engine.go` → перед вызовом ResearchAgent проверять `horizon_missing_info_status` и фильтровать exhausted/cooldown вопросы
      - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → вопросы формулировать **детерминированно** (чтобы хеш стабильно совпадал)
      - [ ] Firestore: новая коллекция `horizon_missing_info_status` + индексы (tenantId, canonicalPath, status)
      - [ ] Telemetry: логировать “attempted/blocked_by_cooldown/exhausted”
      - [ ] UI: показывать unresolved missingInfo backlog
  - [ ] **Canonical path inference** — обновить:
    - `backend/internal/horizon/scout/taxonomy.go` → `InferTaxonomyFromCanonicalPath` для `/deposit/`
  - [ ] **Документация** — обновить:
    - `docs/horizon/architecture/taxonomy.md` — добавить секцию про `/deposit/`
    - `docs/horizon/architecture/data-model.md` — добавить DEPOSIT fragment type
  - [ ] **Firestore re-seed** — после изменений:
    - Выполнить `go run ./cmd/tools/horizon_seed_taxonomy --project <project> --version v1`
  - [ ] **Type & Edge Normalizers** — код приведения типов/рёбер к единообразию:
    - **`backend/internal/horizon/scout/type_normalizer.go`**:
      - `pathTypeMap` (строки ~108-120) — маппит canonical path prefixes к FragmentType
      - **Нужно добавить**: `/deposit/` → `FragmentTypeDeposit`
      - Код:
        ```go
        pathTypeMap := map[string]models.FragmentType{
            "/loc/":         models.FragmentTypeGeoRegion,
            "/geo/":         models.FragmentTypeGeoRegion,
            "/org/":         models.FragmentTypeOrganization,
            "/fac/":         models.FragmentTypeFacility,
            "/deposit/":     models.FragmentTypeDeposit,  // NEW
            "/product/":     models.FragmentTypeProduct,
            "/program/":     models.FragmentTypeProgram,
            "/market/":      models.FragmentTypeMarket,
            "/event/":       models.FragmentTypeEvent,
            "/person/":      models.FragmentTypePerson,
            "/media/":       models.FragmentTypeMediaChannel,
            "/supply-link/": models.FragmentTypeSupplyLink,
        }
        ```
    - **`backend/internal/horizon/scout/edge_normalizer.go`**:
      - `EdgeDirectionPolicies` — определяет валидные source/target FragmentTypes для каждого edge type
      - **Нужно добавить**:
        1. `EXTRACTS_FROM` — новый edge type:
           ```go
           models.DependencyTypeExtractsFrom: {
               ValidSourceTypes: []models.FragmentType{models.FragmentTypeFacility},
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeDeposit},
               Description:      "Facility extracts from deposit",
           }
           ```
        2. Обновить `OWNS` — добавить `FragmentTypeDeposit` в ValidTargetTypes:
           ```go
           models.DependencyTypeOwns: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypePerson,
               },
               ValidTargetTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypeFacility,
                   models.FragmentTypeProduct,
                   models.FragmentTypeDeposit,  // NEW
               },
           }
           ```
        3. Обновить `PRODUCES` — добавить `FragmentTypeDeposit` в ValidSourceTypes (deposit produces resources):
           ```go
           models.DependencyTypeProduces: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypeFacility,
                   models.FragmentTypeDeposit,  // NEW - deposits produce resources
               },
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
           }
           ```
        4. Обновить `LOCATED_IN` — добавить `FragmentTypeDeposit` в ValidSourceTypes:
           ```go
           models.DependencyTypeLocatedIn: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeFacility,
                   models.FragmentTypeOrganization,
                   models.FragmentTypePerson,
                   models.FragmentTypeGeoRegion,
                   models.FragmentTypeDeposit,  // NEW
               },
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
           }
           ```
        5. Обновить `OPERATES_IN` — добавить `FragmentTypeDeposit` в ValidTargetTypes:
           ```go
           models.DependencyTypeOperatesIn: {
               ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
               ValidTargetTypes: []models.FragmentType{
                   models.FragmentTypeGeoRegion,
                   models.FragmentTypeFacility,
                   models.FragmentTypeDeposit,  // NEW
               },
           }
           ```
    - **`backend/internal/taxonomy/runtime.go`**:
      - `NormalizeRelationType()` — нормализует relation types к канону
      - **Нужно добавить**: `EXTRACTS_FROM` в switch/map алиасов
      - `NormalizeProductName()` — нормализует product names
      - **Нужно обновить**: для services формат `/product/service/{domain}/{subdomain}/{slug}`
    - **`backend/internal/horizon/policy/canonization_policy.go`**:
      - Политики канонизации путей
      - **Проверить**: корректность для `/deposit/` и `/product/service/...`
    - **Чеклист для normalizers**:
      - [ ] `type_normalizer.go` → добавить `/deposit/` в `pathTypeMap`
      - [ ] `edge_normalizer.go` → добавить `EXTRACTS_FROM` в `EdgeDirectionPolicies`
      - [ ] `edge_normalizer.go` → обновить `OWNS`, `PRODUCES`, `LOCATED_IN`, `OPERATES_IN` для `DEPOSIT`
      - [ ] `runtime.go` → добавить `EXTRACTS_FROM` в `NormalizeRelationType()`
      - [ ] `runtime.go` → обновить `NormalizeProductName()` для services
      - [ ] `canonization_policy.go` → проверить/обновить для новых путей
- **Lifecycle Stages для HorizonFragment (стадии строительства и жизненного цикла)**
  - **Проблема**: Не все `/fac/`, `/deposit/`, `/org/` существуют и работают. Некоторые строятся, планируются, законсервированы или закрыты. Текущая модель не различает "завод работает" от "завод строится".
  - **Решение**: Добавить поля lifecycle в `HorizonFragment`:
    ```go
    type HorizonFragment struct {
        // ... existing fields ...
        
        // Lifecycle — стадия жизненного цикла объекта в реальном мире
        LifecycleStage    string     `json:"lifecycleStage,omitempty" firestore:"lifecycleStage,omitempty"`
        ExpectedStartDate *time.Time `json:"expectedStartDate,omitempty" firestore:"expectedStartDate,omitempty"`
        ActualStartDate   *time.Time `json:"actualStartDate,omitempty" firestore:"actualStartDate,omitempty"`
        ExpectedEndDate   *time.Time `json:"expectedEndDate,omitempty" firestore:"expectedEndDate,omitempty"`
        ActualEndDate     *time.Time `json:"actualEndDate,omitempty" firestore:"actualEndDate,omitempty"`
    }
    ```
  - **Семантика полей**:
    - `LifecycleStage` — текущая стадия объекта (canonical value из словаря)
    - `ExpectedStartDate` — когда объект **планируется** ввести в эксплуатацию (для construction)
    - `ActualStartDate` — когда объект **фактически** начал работу
    - `ExpectedEndDate` — когда объект **планируется** закрыть (decommissioning)
    - `ActualEndDate` — когда объект **фактически** закрылся
  - **Отличие от существующих полей**:
    - `DependencyEdge.ValidFrom/ValidTo` — про **реальный срок действия** связи/факта (effective time: когда связь началась/закончилась в мире)
    - `MetricValue.ObservedAt` + `Provenance` — про **эпистемику** (когда мы наблюдали/узнали и из какого источника)
    - `Governance.Status` — про состояние **в нашей системе** (ACTIVE/DORMANT/ARCHIVED)
    - `LifecycleStage` — про состояние **объекта в реальном мире** (строится/работает/закрыт)
  - **Canonical lifecycle stages** (единый словарь для всех типов):
    ```
    AllowedLifecycleStages: []string{
        // Ранние стадии
        "concept",           // раннее планирование, pre-feasibility
        "planning",          // детальное планирование, feasibility study
        "permitting",        // получение разрешений, licensing
        "financing",         // привлечение финансирования
        
        // Строительство
        "construction",      // строительство, development drilling
        "commissioning",     // пуско-наладка, ramp-up
        
        // Эксплуатация
        "operational",       // в эксплуатации (default для работающих объектов)
        "expansion",         // расширение (работает + строится дополнительная мощность)
        
        // Остановка
        "suspended",         // временно остановлен, care & maintenance, shut-in
        
        // Закрытие
        "decommissioning",   // вывод из эксплуатации, abandonment
        "closed",            // закрыт, dissolved, depleted, demolished
    }
    ```
  - **Маппинг по типам фрагментов**:
    | Stage | FACILITY | DEPOSIT | ORGANIZATION |
    |-------|----------|---------|--------------|
    | `concept` | Pre-feasibility | Early exploration | Pre-incorporation |
    | `planning` | Feasibility study | Appraisal | Business planning |
    | `permitting` | Getting permits | Licensing | Registration |
    | `financing` | Securing funding | — | Fundraising |
    | `construction` | Building | Development drilling | — |
    | `commissioning` | Ramp-up | First production | Launch |
    | `operational` | Running ✓ | Production ✓ | Operating ✓ |
    | `expansion` | Adding capacity | Infill drilling | Scaling |
    | `suspended` | Idle / C&M | Shut-in | Dormant |
    | `decommissioning` | Closing down | Abandonment | Liquidation |
    | `closed` | Demolished | Depleted | Dissolved |
  - **Industry-specific aliases** (для удобства маппятся в canonical):
    - **O&G**: `exploration` → `concept`, `appraisal` → `planning`, `development` → `construction`, `production` → `operational`, `shut_in` → `suspended`
    - **Mining**: `pre_feasibility` → `concept`, `feasibility` → `planning`, `development` → `construction`
    - **ORG**: `startup` → `commissioning`, `restructuring` → `suspended`, `liquidation` → `decommissioning`, `dissolved` → `closed`
  - **Примеры использования**:
    ```json
    {
      "canonicalPath": "/fac/tesla/monterrey-gigafactory",
      "type": "FACILITY",
      "lifecycleStage": "construction",
      "expectedStartDate": "2026-03-01T00:00:00Z",
      "labels": {"en": "Tesla Gigafactory Monterrey"}
    }
    ```
    ```json
    {
      "canonicalPath": "/deposit/lithium/thacker-pass",
      "type": "DEPOSIT",
      "lifecycleStage": "permitting",
      "expectedStartDate": "2027-01-01T00:00:00Z",
      "labels": {"en": "Thacker Pass Lithium Project"}
    }
    ```
  - **Влияние на supply chain**:
    - Объект в `construction` не может быть **текущим** supplier/customer
    - Edges к таким объектам должны иметь `Qualifiers["edgeModality"] = "projected"` (чтобы не путать с эпистемической modality: observed/reported/predicted)
    - Propagation engine учитывает lifecycleStage при расчёте весов:
      - `operational` → нормальный вес
      - `construction` → сниженный вес или отдельный "projected" signal
      - `closed` → не участвует в propagation
  - **Scout/Critic интеграция**:
    - Scout должен извлекать lifecycleStage и dates из источников
    - Critic должен требовать lifecycleStage для:
      - Всех `/fac/` — обязательно
      - Всех `/deposit/` — обязательно
      - `/org/` в extractive sectors — обязательно
    - Если lifecycleStage == "construction" или "planning", требовать expectedStartDate
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/fragment.go` → добавить поля `LifecycleStage`, `ExpectedStartDate`, `ActualStartDate`, `ExpectedEndDate`, `ActualEndDate`
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedLifecycleStages []string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedLifecycleStages`
    - [ ] `backend/internal/taxonomy/runtime.go` → добавить `AllowedLifecycleStages`, `NormalizeLifecycleStage()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать lifecycle info
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать lifecycle для /fac/, /deposit/
    - [ ] Propagation engine → учитывать lifecycleStage при расчёте весов
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать lifecycle stages
- **Edge Metrics (метрики на рёбрах: %, суммы, объёмы)**
  - **Проблема**: Текущие edges имеют только абстрактные `EdgeProperties` (Criticality, Amplification, LatencyHours). Нет конкретных метрик:
    - OWNS → на сколько %?
    - INVESTS_IN → сколько вложил? каким раундом?
    - SUPPLIES_TO → какой объём? на какую сумму?
  - **Решение**: Добавить `Metrics map[string]MetricValue` в `DependencyEdge`:
    ```go
    type DependencyEdge struct {
        // ... existing fields ...
        
        // Edge-specific metrics — numerical data with units (like fragment State)
        Metrics map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
    }
    ```
  - **Переиспользуем существующий MetricValue**:
    - `Value` (float64) — значение
    - `Unit` (*string) — единица измерения
    - `Confidence` (float64) — уверенность
    - `ObservedAt` (time.Time) — дата наблюдения
    - `Provenance` — источник данных
    - `ErrorMargin` — интервал ошибки
  - **Разделение Metrics vs Qualifiers**:
    - `Qualifiers` (существующий) — categorical/string данные (`round: "series_b"`, `product: "/product/..."`)
    - `Metrics` (новый) — numerical данные с unit/confidence/provenance
  - **Canonical metric names по типам edges**:
    | Edge Type | Metric Name | Unit | Описание |
    |-----------|-------------|------|----------|
    | `OWNS` | `ownership_percent` | `percent` | Доля владения |
    | `OWNS` | `voting_percent` | `percent` | Доля голосов (если отличается) |
    | `SUBSIDIARY_OF` | `ownership_percent` | `percent` | Доля владения родителем |
    | `INVESTS_IN` | `investment_amount` | currency | Сумма инвестиции |
    | `INVESTS_IN` | `round_size_total` | currency | Общий размер раунда |
    | `SUPPLIES_TO` | `annual_volume` | varies | Годовой объём поставок |
    | `SUPPLIES_TO` | `contract_value` | currency | Стоимость контракта |
    | `SUPPLIES_TO` | `contract_duration_years` | `years` | Срок контракта |
    | `PRODUCES` | `annual_capacity` | varies | Годовая мощность |
    | `PRODUCES` | `annual_output` | varies | Фактический выпуск |
    | `CONSUMES` | `annual_consumption` | varies | Годовое потребление |
    | `EXTRACTS_FROM` | `annual_extraction` | varies | Годовая добыча |
    | `EMPLOYED_BY` | `tenure_years` | `years` | Стаж работы |
    | `EMPLOYED_BY` | `compensation` | currency | Компенсация |
    | `BOARD_MEMBER_OF` | `tenure_years` | `years` | Срок в совете |
    | `OPERATES_IN` | `market_share` | `percent` | Доля рынка |
    | `COMPETES_WITH` | `relative_market_share` | `ratio` | Относительная доля |
  - **Canonical qualifiers по типам edges** (categorical, в Qualifiers):
    | Edge Type | Qualifier | Values |
    |-----------|-----------|--------|
    | `INVESTS_IN` | `round` | `seed`, `series_a`, `series_b`, `series_c`, `series_d`, `growth`, `ipo`, `secondary` |
    | `INVESTS_IN` | `roundDate` | ISO date string |
    | `SUPPLIES_TO` | `product` | `/product/...` canonical path |
    | `PRODUCES` | `product` | `/product/...` canonical path |
    | `CONSUMES` | `product` | `/product/...` canonical path |
    | `EXTRACTS_FROM` | `product` | `/product/...` canonical path |
    | `EMPLOYED_BY` | `role` | `ceo`, `cfo`, `coo`, `cto`, etc. |
    | `EMPLOYED_BY` | `startDate` | ISO date string |
    | `BOARD_MEMBER_OF` | `committees` | comma-separated list |
    | `REGULATES` | `regulationType` | `licensing`, `environmental`, `safety`, etc. |
  - **Примеры использования**:
    ```json
    {
      "type": "OWNS",
      "sourceFragmentId": "/org/sa/aramco",
      "targetFragmentId": "/org/kr/s-oil",
      "metrics": {
        "ownership_percent": {
          "value": 63.4,
          "unit": "percent",
          "confidence": 0.99,
          "observedAt": "2024-01-01T00:00:00Z"
        }
      },
      "properties": {"criticality": 0.9}
    }
    ```
    ```json
    {
      "type": "INVESTS_IN",
      "sourceFragmentId": "/org/us/sequoia",
      "targetFragmentId": "/org/us/stripe",
      "metrics": {
        "investment_amount": {"value": 500000000, "unit": "usd", "confidence": 0.9}
      },
      "qualifiers": {
        "round": "series_h",
        "roundDate": "2023-03-15"
      }
    }
    ```
    ```json
    {
      "type": "SUPPLIES_TO",
      "sourceFragmentId": "/org/cl/sqm",
      "targetFragmentId": "/org/cn/catl",
      "metrics": {
        "annual_volume": {"value": 50000, "unit": "metric_tons_per_year"},
        "contract_value": {"value": 1500000000, "unit": "usd"},
        "contract_duration_years": {"value": 5, "unit": "years"}
      },
      "qualifiers": {
        "product": "/product/raw/chemical/refining/lithium-carbonate"
      }
    }
    ```
  - **Связь Metrics → Criticality**:
    - `EdgeProperties.Criticality` может быть **derived** из Metrics
    - Например: OWNS с ownership_percent > 50% → criticality = 0.9
    - Или: SUPPLIES_TO с contract_value > $1B → criticality = 0.8
    - Это делает граф самодокументируемым: criticality имеет обоснование
  - **AllowedMetricUnits — расширить для edges**:
    ```go
    // Добавить к существующим
    "percent",           // ownership, market share
    "years",             // tenure, contract duration
    "count",             // committees, employees
    "ratio",             // relative values
    // Currency / index units:
    // ВАЖНО: валюты и финансовые индексы НЕ ограничиваем allowlist'ом.
    // Unit может быть любым тикером/кодом (например: "usd", "eur", "usdt", "inr", "rub", "cny",
    // "brent", "wti", "cpi_ru", "moex", "sp500") — чтобы не ломать санкционные/теневые рынки.
    //
    // Имплементация (runtime validation):
    // - Для currency/index единиц разрешаем pattern, а не точный allowlist.
    // - Минимальный контракт: lowercase, [a-z0-9_], длина 2..32.
    ```
  - **Scout/Critic интеграция**:
    - Scout должен извлекать edge metrics из источников
    - Critic должен требовать metrics для:
      - `OWNS` → обязательно `ownership_percent`
      - `INVESTS_IN` → желательно `investment_amount`, обязательно `round` qualifier
      - `SUPPLIES_TO` → желательно `annual_volume` или `contract_value`
    - Если metrics отсутствуют → добавлять в `missingInfo` вопросы
  - **Связь с Events**:
    - Investment rounds = Events (`/event/stripe-series-h-2023`)
    - Edge `INVESTS_IN` может иметь `Qualifiers["eventId"]` → ссылка на Event fragment
    - Event содержит полные детали: организаторы, все участники, условия
    - Базовые metrics живут на edge, детали — в Event
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/dependency_edge.go` → добавить `Metrics map[string]MetricValue`
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedEdgeMetrics map[string][]string` (edge type → allowed metrics)
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedEdgeMetrics`
    - [ ] `backend/internal/taxonomy/runtime.go` → `AllowedEdgeMetrics`, `ValidateEdgeMetrics()`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → расширить `AllowedMetricUnits` (`percent`, `years`, `count`, `ratio`)
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать edge metrics
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать metrics для OWNS, INVESTS_IN, SUPPLIES_TO
    - [ ] `docs/horizon/architecture/data-model.md` → документировать edge metrics
- **Track Entities (Moving Objects) — `/track/...`**
  - **Контекст**: Для отслеживания перемещаемых объектов (суда, самолеты, грузы) используется отдельный тип фрагментов с высокой частотой обновлений.
  - **Canonical Path Format** (без брендинга, нейтральный):
    - `/track/air/{iso_or_global}/{slug}-{hash}` (e.g. `/track/air/us/af1-12345`)
    - `/track/sea/{iso_or_global}/{slug}-{hash}` (e.g. `/track/sea/imo/9876543`)
    - `/track/ground/{iso_or_global}/{slug}-{hash}`
  - **Модель данных**:
    - `FragmentType`: `TRACK`
    - **State/Metrics**: `lat`, `lon`, `heading`, `speed`, `altitude` (air), `depth` (sea), `lastSeenAt`, `geoHash`.
    - **Provenance**: `sourceId`, `confidence`.
  - **Логика обновления**:
    - **Критерий**: объект считается "треком" только при наличии валидной геопривязки (не 0,0) + разрешенного типа (vessel, aircraft) + признака трека.
    - **History vs State**: История позиций пишется в `ontology_facts` (signals) или TimeSeries хранилище. Во фрагменте (`HorizonFragment`) хранится только **latest state** для быстрого доступа и отображения на карте.
    - `fragment.HorizonMonitor` используется только как переходный контейнер при импорте, но не как основной критерий в runtime.
  - **Чеклист**:
    - [ ] Добавить `FragmentTypeTrack` в модели.
    - [ ] Реализовать `TrackIngest` пайплайн (отдельно от Scout), который обновляет только State фрагмента.
    - [ ] Настроить TTL или архивную политику для старых треков (чтобы не засорять граф "мертвыми" точками).
    - [ ] Убедиться, что бренд `worldmonitor` не используется в путях или тегах.

- **Edge Temporal Validity (срок действия связей)**
  - **Проблема**: Связи могут иметь ограниченный срок действия:
    - Инвестировал в прошлом, но **уже вышел**
    - Работал в прошлом (**бывший сотрудник**)
    - Руководил в прошлом (**экс-CEO**)
    - Был членом совета директоров
    - Поставлял в прошлом, но контракт завершён
  - **Существующие поля** (уже есть в `DependencyEdge`):
    ```go
    ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
    ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`
    ```
  - **Семантика**:
    - `ValidFrom` — когда связь **начала** действовать (дата найма, инвестиции, контракта)
    - `ValidTo` — когда связь **прекратила** действовать (увольнение, выход из инвестиции, конец контракта)
    - `ValidTo == nil` → связь **активна** на текущий момент
    - `ValidTo != nil && ValidTo < now` → связь **историческая**
  - **ВАЖНО: Исторические связи НЕ удаляются**:
    - Хранят ценную информацию о прошлых связях
    - Нужны для анализа карьерных путей, инвестиционной истории
    - Нужны для compliance (кто был связан с sanctioned entity)
    - Просто помечаются как неактивные через `ValidTo`
  - **Методы на DependencyEdge**:
    ```go
    // IsActive проверяет активна ли связь на заданный момент времени
    func (e *DependencyEdge) IsActive(at time.Time) bool {
        if e.ValidFrom != nil && at.Before(*e.ValidFrom) {
            return false // связь ещё не началась
        }
        if e.ValidTo != nil && at.After(*e.ValidTo) {
            return false // связь уже завершилась
        }
        return true
    }
    
    // IsActiveNow проверяет активна ли связь сейчас
    func (e *DependencyEdge) IsActiveNow() bool {
        return e.IsActive(time.Now())
    }
    
    // IsHistorical проверяет завершилась ли связь
    func (e *DependencyEdge) IsHistorical() bool {
        return e.ValidTo != nil && time.Now().After(*e.ValidTo)
    }
    ```
  - **Edge types где ValidTo особенно важен**:
    | Edge Type | Пример завершения | Как заполнять |
    |-----------|-------------------|---------------|
    | `EMPLOYED_BY` | Увольнение, уход | ValidTo = дата ухода |
    | `BOARD_MEMBER_OF` | Выход из совета | ValidTo = дата выхода |
    | `MANAGES` | Смена руководства | ValidTo = дата смены |
    | `OWNS` | Продажа доли | ValidTo = дата продажи |
    | `INVESTS_IN` | Выход из инвестиции (exit) | ValidTo = дата exit |
    | `SUPPLIES_TO` | Завершение контракта | ValidTo = дата окончания |
    | `AUDITED_BY` | Смена аудитора | ValidTo = дата смены |
    | `CONTROLS` | Потеря контроля | ValidTo = дата потери |
  - **Влияние на Propagation**:
    - **Активные связи** (`IsActiveNow() == true`) — участвуют в propagation полноценно
      - (т.е. propagation фильтрует edges по окну `ValidFrom/ValidTo`, а не только по `edgeModality`)
    - **Исторические связи** (`IsHistorical() == true`) — **не участвуют** в real-time propagation
    - Но исторические связи **участвуют** в:
      - Timeline analysis (как развивались связи)
      - Compliance checks (был ли связан с sanctioned entity в прошлом)
      - Career path analysis
    - **Будущие связи** (`ValidFrom > now`) — не участвуют в propagation, но видны как "projected"
    - **Правило выставления `edgeModality` на основе дат** (для единообразия UI/аналитики):
      - если `ValidFrom != nil && ValidFrom > now` → `edgeModality = "projected"`
      - если `ValidTo != nil && ValidTo < now` → `edgeModality = "historical"`
      - иначе → `edgeModality = "actual"`
  - **Влияние на риски (Risk Model)**:
    - Исторические связи с sanctioned entities = **lower but non-zero risk**
    - "Бывший сотрудник sanctioned company" — всё равно red flag
    - Модель рисков должна учитывать `ValidTo` при расчёте risk score
  - **Scout/Critic интеграция**:
    - Scout должен извлекать temporal info:
      - "worked at X from 2015 to 2020" → ValidFrom = 2015-01-01, ValidTo = 2020-12-31
      - "former CEO" → ValidTo = past date (если известна)
      - "joined in 2021" → ValidFrom = 2021-01-01, ValidTo = nil
    - Critic должен:
      - Если source содержит "former", "ex-", "бывший" → требовать ValidTo
      - Для `EMPLOYED_BY`, `BOARD_MEMBER_OF`, `MANAGES` → желательно ValidFrom
  - **Qualifiers для temporal context** (дополнительно к ValidFrom/ValidTo):
    - `Qualifiers["startReason"]`: "hired", "elected", "acquired", "contracted"
    - `Qualifiers["endReason"]`: "resigned", "fired", "retired", "expired", "sold", "exited"
    - `Qualifiers["tenure"]`: "2015-2020", "5 years" (human-readable summary)
  - **JSON пример — исторический EMPLOYED_BY**:
    ```json
    {
      "type": "EMPLOYED_BY",
      "sourceFragmentId": "/person/us/satya-nadella",
      "targetFragmentId": "/org/us/sun-microsystems",
      "validFrom": "1992-01-01T00:00:00Z",
      "validTo": "1992-12-31T00:00:00Z",
      "qualifiers": {
        "role": "Intern",
        "endReason": "contract_ended"
      }
    }
    ```
  - **JSON пример — активный OWNS**:
    ```json
    {
      "type": "OWNS",
      "sourceFragmentId": "/org/us/softbank-vision-fund",
      "targetFragmentId": "/org/us/wework",
      "validFrom": "2017-08-01T00:00:00Z",
      "validTo": null,
      "metrics": {
        "ownership_percent": {"value": 29.5, "unit": "percent"}
      }
    }
    ```
  - **JSON пример — exit из INVESTS_IN**:
    ```json
    {
      "type": "INVESTS_IN",
      "sourceFragmentId": "/org/us/sequoia-capital",
      "targetFragmentId": "/org/us/airbnb",
      "validFrom": "2009-04-01T00:00:00Z",
      "validTo": "2020-12-10T00:00:00Z",
      "metrics": {
        "investment_amount": {"value": 600000, "unit": "usd"}
      },
      "qualifiers": {
        "round": "seed",
        "endReason": "ipo_exit"
      }
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/dependency_edge.go` → добавить методы `IsActive()`, `IsActiveNow()`, `IsHistorical()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать temporal info из sources ("former", "ex-", "since", "until")
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать ValidTo для "former/ex-" relationships
    - [ ] Propagation engine → фильтровать historical edges из real-time propagation
    - [ ] Risk model (future) → учитывать historical relationships в risk scoring
    - [ ] Query API → параметр `?includeHistorical=true|false` (default: false для active only)
    - [ ] `docs/horizon/architecture/data-model.md` → документировать edge temporal validity
- **Связь Lifecycle Stage → Edge Modality**
  - **Проблема**: Если target fragment в `construction`, edge к нему — это "будущая" связь, не текущая.
  - **Решение**: Автоматически помечать edges к non-operational targets:
    - Если `target.LifecycleStage` не в [`operational`, `expansion`]:
      - Устанавливать `edge.Qualifiers["edgeModality"] = "projected"` (отдельно от эпистемической modality)
      - Или `edge.ValidFrom` = target's `ExpectedStartDate`
    - Propagation engine учитывает `edgeModality`:
      - `edgeModality: "actual"` (default) → полный вес
      - `edgeModality: "projected"` → сниженный вес или отдельный signal type
      - `edgeModality: "historical"` → не участвует в текущем propagation
  - **Canonical edge modalities**:
    ```go
    AllowedEdgeModalities: []string{
        "actual",      // связь существует и активна (default)
        "projected",   // связь планируется (target в construction)
        "historical",  // связь существовала в прошлом
        "potential",   // связь возможна, но не подтверждена
    }
    ```
  - **Примеры**:
    ```json
    {
      "type": "SUPPLIES_TO",
      "sourceFragmentId": "/fac/tesla/monterrey-gigafactory",
      "targetFragmentId": "/org/na/ford",
      "qualifiers": {
        "edgeModality": "projected",
        "product": "/product/component/electronic/manufacturing/battery-cell"
      },
      "validFrom": "2026-03-01T00:00:00Z"
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedEdgeModalities []string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedEdgeModalities`
    - [ ] `backend/internal/horizon/scout/engine.go` → при создании edge проверять target.LifecycleStage и устанавливать modality
    - [ ] Propagation engine → учитывать modality при расчёте весов
- **Обработка метрик при propagation (семантика поверх "доставки дельт")**
  - Ввести registry/factory **именованных метрик**.
  - Для части метрик применять **детерминированные handler’ы** (algorithmic rules сначала): stateful / categorical / gated.
  - Волна propagation должна вызывать handler’ы для “именованных” метрик, чтобы изменения могли влиять на:
    - derived fields/flags,
    - дискретные статусы/режимы,
    - вторичные характеристики сущности,
    - (опционально) политику применения/порогов.
  - Требования: **идемпотентность**, **трассировка (explain/trace)**, **версионирование логики** (чтобы воспроизводить состояние).

---
### Конец большого раздела работ: расширение онтологии/таксономии Horizon
---
- **Signal handlers: риски / возможности / тренды / направления (семантика поверх доставки)**
  - Ввести единый тип “signal” (kind: risk|opportunity|trend|direction) с **каноническим type** (runtime taxonomy: allowed + synonyms).
  - Provenance считаем “вынесенным”: сигнал должен ссылаться на доказательную базу через онтологические ноды/факты (например, `ontology_facts`, `ontology_events`, `ontology_claims`, `ontology_sources`), а не тащить “простыни” ссылок внутрь Horizon.
  - Определить минимальный контракт времени/модальности для сигналов (использовать то, что запланировано выше):
    - `occurredAt` / `observedAt` для происхождения сигнала,
    - `effectiveFrom/effectiveTo` для “окна действия”,
    - `modality` (observed / reported / predicted / hypothetical), чтобы различать факт и сценарий.
  - Определить **identity/dedup ключ** сигнала (например: `(kind, type, subjectCanonicalPath, scope{sphere/location/actor}, timeWindow, modality, sourceRef)`), чтобы propagation не размножал почти-одинаковые записи.
  - Определить **lifecycle** сигнала: create/update/expire/archive + политика “когда сигнал считается устаревшим/неверным” (в т.ч. для predicted).
  - Ввести **uncertainty/conflict** поверх сигналов:
    - явная конфликтность (два сигналa противоречат по одному ключу),
    - хранение confidence + (опционально) интервала/распределения,
    - политика разрешения (voting / trust-weighted / keep-both-with-caveat).
  - Продумать **агрегацию/rollup**: как из множества сигналов делать “сводный” по узлу/сектору/лине (max/worst-of/decay/hysteresis) и что является сущностью результата (например, `SignalState`/`SignalRollup`).
  - В propagation вызывать **видовые handler’ы** по `(kind, type)`:
    - правила затухания/усиления по типу ребра,
    - агрегация/дедуп (чтобы сигнал не размножался бесконтрольно),
    - (опционально) трансформации сигналов (например, risk санкций → risk supply disruption).
  - Добавить/учесть **causality vs correlation**: причинность — отдельный артефакт/ссылка (не выводить “причину” автоматически из совместной динамики).
  - Явно предусмотреть **feedback loops / derived signals**: propagation может порождать производные сигналы — нужно разделение raw vs derived + защита от циклов (см. секцию C).
  - Вынести **propagation policies как данные** (таблично): какие `relationType`/ребра пропускают какие `kind/type`, на какую глубину и с каким весом (минимизировать деплои кода при расширении словаря).
  - Зафиксировать **контракт на explainability**: любой сигнал/rollup обязан уметь ответить “почему” (trace: входы, правила, веса, time/modality окна, источники).
  - Требования: **детерминированность**, **идемпотентность**, **explain/trace**, **версионирование правил**, и политика “как predicted влияет на state”.
- **Интеграция “базовой онтологии” с Horizon под единым смысловым зонтиком**
  - Зафиксировать “зонтик” терминологии: Horizon = мониторинг графа + подписки/линзы + Vertex-индексация; Ontology = источник нормализованных артефактов (entities/events/claims/metrics/trends/scenarios/causal/uncertainty).
  - Определить маппинг **онтологических артефактов → Horizon**:
    - `ontology_facts` как унифицированный поток “фактов/сигналов” для Watchdog/Scout (incremental, watermark),
    - `Trend`, `Scenario`, `CausalLink`, `UncertaintyModel`, `Concept(kind=risk/opportunity/...)` — как first-class носители сигналов, привязанные через `canonicalPaths`/`actorPaths`/`locationPath`/`spherePaths`.
  - Утвердить единые канонические поля связи:
    - `canonicalPaths` (куда “прикрепляется” сигнал в Horizon),
    - `locationPath` (`/loc/...`), `actorPaths`, `spherePaths` (для фильтрации/QueryAddress и propagation policies).
  - Описать стратегию миграции “strict core first” и Horizon‑grade контракт для онтологии: см. [Ontology → Horizon migration](../architecture/ontology-migration.md).
  - Спланировать интеграцию в UX/API:
    - “сводка сигналов” по `HorizonFragment.canonicalPath` (raw + rollups + explain),
    - единая выдача поиска: fragments + ontology_facts (или отдельные вертикали) под один сценарий.
  - Спланировать совместную индексацию в Vertex AI Search:
    - Horizon fragments (уже есть),
    - ontology_facts (уже есть),
    - определить, как tenant/scope разделяются и как объединяются в выдаче.
  - **Идея: Сохранение профилей фрагментов как документов в GCS с синхронизацией в Vertex AI Search**
    - Сейчас `FragmentProfile` сохраняются только в Firestore (`horizon_fragment_profiles`), в отличие от других сущностей (fragments, documents, ontology_facts), которые также экспортируются в GCS и индексируются в Vertex AI Search.
    - Добавить в `FragmentProfilesRepository`:
      - `WithStorage()` — для экспорта в GCS (аналогично `HorizonFragmentsRepository`)
      - `WithVertexSearchSync()` — для автоматической синхронизации с Vertex AI Search
      - `uploadToGCS()` — экспорт профиля в JSON формате для Vertex AI Search
    - Путь в GCS: `{tenantId}/horizon_fragment_profiles/{profileId}.json` (или `global/horizon_fragment_profiles/{profileId}.json` для системного tenant)
    - Формат документа: JSON с `content` (base64 encoded) и `structData` (metadata: fragmentId, fragmentType, canonicalPath, status, capturedAt и т.д.)
    - Вызывать экспорт в `Create()` после успешного сохранения в Firestore (асинхронно, не блокируя основной поток)
    - Преимущества: профили станут доступны для семантического поиска, единообразие с другими сущностями системы, возможность поиска по содержимому профилей (overview, financials, leadership и т.д.)
  - **Вернуть “правильный” ордеринг/фильтрацию профилей средствами БД (Firestore)**
    - Контекст: из‑за отсутствующих composite indexes часть запросов к `horizon_fragment_profiles` была переведена на **in-memory сортировку** (workaround), чтобы избежать `FAILED_PRECONDITION: The query requires an index`.
    - Цель: восстановить корректные Firestore‑запросы вида:
      - `Where("fragmentId","==",...)` + `OrderBy("capturedAt", Desc)` (latest first)
      - `Where("canonicalPath","==",...)` + `Where("status","==","ACTIVE")` + `OrderBy("capturedAt", Desc)` (latest active)
    - Действия:
      - Явно описать и держать в репо нужные **composite indexes** (`web/firestore.indexes.json`) для этих паттернов.
      - После того как индексы в `READY`, вернуть DB‑ordering в:
        - `FragmentProfilesRepository.ListByFragmentID()`
        - `FragmentProfilesRepository.DeleteByFragmentID()` (если нужно детерминированно удалять “latest first”, иначе можно оставить без orderBy)
        - `GetByFragmentID()/GetByCanonicalPath()` (убрать workaround при наличии индексной поддержки).
    - Acceptance:
      - Нет 500/FAILED_PRECONDITION на проде при `GET/DELETE/POST profile`.
      - Запросы используют Firestore ordering (меньше данных, быстрее, предсказуемо).
      - Индексы задокументированы и воспроизводимы (infra-as-code).
- **Мульти‑классификация без взрыва словаря**
  - При необходимости: "primary sector/entity + tags" или аккуратные `[]string` поля, но без попытки сделать дерево бесконечным.
- **Intent/Keywords Resolution (Lens-driven)**
  - Реализовать механизм инкрементального резолва keywords в canonical paths и sphere paths по мере работы агентов.
  - Keywords должны жить на Lens (в `ViewConfig.keywords`), передаваться в `ScoutingJob.State`, и по мере нахождения сущностей агентами — резолвиться обратно в Lens (`resolvedKeywords` mapping).
  - `QueryAddress` должен поддерживать `keyword:` префикс для динамического резолва (например, `/query/(canonicalPath:/org/tesla AND keyword:batteries AND sphere:/sphere/logistics)`).
  - Гарантировать, что сущности, относящиеся к сфере (через `spherePaths` в facts), получают связи со сферой в графе (sphere fragments и `INFLUENCES` edges).
  - **Expose NL precheck in backend API (Hook-ready)**
    - `POST /api/horizon/intent/resolve` (NL query → taxonomy-aligned intent: anchors + spherePaths + keywords + focus).
    - `POST /api/horizon/lenses/from-query` (resolve intent → create Lens with scope=anchors and viewConfig steering).
    - Подключить Horizon Hook UX к этим endpoint'ам (в идеале: typed draft preview перед созданием Lens).

**Критерии приемки**:
- Можно отвечать на вопросы “что было верно на дату X” (минимально для edges/facts).
- Факты различаются по модальности и не приводят к неконтролируемым обновлениям state.
- Улучшения не ломают текущие canonical paths и QueryAddress.

### C) Масштабирование и “графовая” корректность (после стабилизации)

**Цель**: сделать поведение Watchdog/Scout/Propagation устойчивым на больших графах и в сложных случаях (циклы, условные зависимости, батчинг).

**TODO**:
- **Propagation: защита от циклов (важно)**
  - Добавить `visited`/дедуп в рамках одной волны по ключу `(fragmentId, metricName)` и не рекурсировать повторно.
  - Добавить лимиты: `maxDepth`, `maxNodesUpdated`, `timeBudget` (fail-safe).
- **Batching: аккумулировать вызовы Scout и изменения**
  - Очередь/батчинг для ScoutingJob (группировка/дедуп), чтобы один факт не раздувал десятки вызовов.
  - Батч-обработка обновлений в Propagation Engine и (по возможности) batch writes в Firestore.
- **Consistency: последовательные мутации “по объекту” (anti-race / lost updates)**
  - **Проблема**: в нагруженном пайплайне один и тот же `HorizonFragment` может **асинхронно** модифицироваться разными воркерами/ветками:
    - scout jobs (concurrent workers / Cloud Tasks),
    - policy check → enforcement auto-fixes (async),
    - propagation trigger (async),
    - ручной запуск enforcement batch.
  - При текущей модели записи “прочитал → `Set()` целиком документ” возможен классический **lost update / last-write-wins**:
    - пример: один агент исправляет `/org/na/...` (iso2), другой добавляет legal form `-ltd` → итог зависит от порядка записи, возможны откаты/потери соседних полей.
  - **Цель**: убрать зависимость результата от порядка конкурентных записей; добиться того, что “агенты исправляют своё и не ломают другое”.
  - **Идея решения (тяжёлая задача; планировать отдельно)**:
    - **Сериализация по `fragmentId`**: все операции, которые могут изменить один и тот же фрагмент, исполняются **строго последовательно**. Следующая операция получает на вход **явный результат** предыдущей (актуальный snapshot/версия).
    - **Patch-намерение вместо полной перезаписи**: агенты/таски формируют “mutation patch” (какие поля и как изменить), а единый “applier” применяет патч атомарно (transaction/CAS) и не трогает прочие поля.
    - **Preconditions**: каждая мутация содержит предусловия (например, “fix iso2 только если iso2==na”, “add legalForm только если legalForm пустой”), чтобы ретраи/дубликаты превращались в safe no-op.
    - **Multi-entity операции** (merge fragments / массовое rewire edges): требуют либо захвата нескольких ключей в детерминированном порядке (`min(id), max(id)`), либо отдельного “агрегата” (root id), иначе сериализация только по одному `fragmentId` не гарантирует консистентность.
  - **Пример формата мутации (эскиз)**:
    - `MutationEnvelope{fragmentId, patchId, preconditions, operations[], causedBy(jobId/taskId), createdAt}`
    - `operations`: `set(path)`, `mergeMap`, `appendUnique`, `rewriteCanonicalPathComponents(...)`, `edgeRewrite(...)`
    - “applier” возвращает `{beforeVersion, afterVersion, appliedOps, skippedOps, reason}` и кладёт это в очередь следующему шагу.
- **LLM-free эвристики корректности и “анти-взрыва” графа (graph shaping without LLM)**
  - **Зачем**: уменьшить “мусор” в графе и экспоненциальный рост ScoutingJobs/propagation **без** дополнительных LLM вызовов; сделать поведение предсказуемым (bounded worst-case).
  - `canonicalPath → fragmentType` как **hard rule** (path-based type inference).
    - Зачем: детерминированная нормализация типов (`/org/*`, `/loc/*`, `/fac/*`, `/sphere/*`), устраняет класс ошибок агентов, упрощает direction/rules.
  - Soft-валидация `relationType × (sourceType,targetType)` как **penalty/ранжирование** (не hard reject) + **audit** для грубых нарушений.
    - Зачем: отсекать явно бессмысленные комбинации типов, но не ломать “реальный мир” (конгломераты/нетипичные связи).
  - Depth-decay: затухание `effectiveConfidence/effectivePriority` с глубиной + пороги/бюджеты на создание child jobs.
    - Зачем: главный анти-экспоненциальный механизм даже при `maxDepth=2` (мягко режет хвосты, где больше шума).
  - Hub-aware expansion: **не блокировать** хабы, а ограничивать раскрытие по relevance к `Lens.viewConfig.focus/spherePaths` + бюджетам.
    - Зачем: хабы (страны/мегакорпы) важны, но раскрывать “целиком” нельзя — иначе граф превращается в энциклопедию.
  - Negative evidence cache (short TTL, soft signal): если уже пытались подтвердить/найти и не нашли — понижать приоритет повторов, но не блокировать полностью.
    - Зачем: ускорять повторные/похожие запросы и снижать стоимость на “мы это уже проверяли”, не кешируя “навсегда”.
  - Anomaly/audit сигналы (без авто-фильтра): fan-in/fan-out дисбаланс, self-edges, транзитивный шум `LOCATED_IN`, конфликтные/цикличные отношения.
    - Зачем: безопасно повышать наблюдаемость качества и облегчать дебаг без риска false negatives.
  - **Acceptance**:
    - Worst-case по созданию child jobs должен быть предсказуем и ограничен бюджетами/порогами.
    - Не добавляем новые LLM вызовы (правила должны быть детерминированными/конфигурируемыми).
    - Алерты/audit должны быть трассируемыми: “почему это помечено/понижено”.
- **Watchdog: “соседние пути” в графе**
  - Реагировать не только на прямой canonicalPath, но и на соседние узлы по ребрам (глубина 1–2) при обработке релевантных фактов.
  - Кэш/снапшот графа зависимостей для удешевления обхода.
- **Conditional dependencies (условные зависимости)**
  - Возможность задавать условия/контекст ребра (когда зависимость “включена”), чтобы волны и watchdog не применяли изменения “вслепую”.
- **Relevance / distance heuristics**
  - Механизм "расстояния/релевантности" между запросом/фактом и фрагментами (например, учитывать географический контекст), чтобы выборка не касалась всего пула игроков.

### D) Unified Policy Architecture (высокий приоритет)

**Цель**: выделить класс задач "принятие решений" в унифицированный артефакт **Policy**, который содержит:
- **(a) LLM fallback**: инструменты для случаев, когда детерминированные методы пока не покрывают
- **(b) Deterministic rules**: эвристики и алгоритмы, удерживающие строгость таксономии и графа

**Зачем**:
- **Телеметрия**: видеть ratio (a)/(b), latency, cost по каждой Policy в реальном времени
- **Эволюция**: начинаем с высоким (a), смотрим паттерны в LLM решениях, "кристаллизуем" в правила, (b) растёт, costs↓
- **Качество**: правила проверены на реальных данных, не теряем quality при снижении LLM usage
- **Testability**: детерминированная часть покрыта unit-тестами, LLM — smoke/integration

**Policies**:

| Policy | Зона ответственности |
|--------|---------------------|
| `PropagationPolicy` | Как метрики текут по рёбрам (weight, damping, direction, critical path boost) |
| `ScoutPolicy` | Когда скаутить, какие источники, глубина, freshness thresholds, budget limits |
| `CanonizationPolicy` | Нормализация paths, types, names, synonyms resolution |
| `SynthesisPolicy` | Fact → Signal rules (когда детерминированно, когда LLM) |
| `ValidationPolicy` | Какие комбинации (source×target×relationType) валидны, soft penalties, hard rejects |
| `PrioritizationPolicy` | Critical paths, hub handling, relevance scoring |
| `AggregationPolicy` | Как агрегировать метрики (max, sum_capped, decay) |

**Unified Contract**:
```go
type Policy interface {
    Name() string
    Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error)
    Explain(decision PolicyDecision) string
}

type PolicyDecision struct {
    Action       string                 // "propagate", "skip", "normalize_to", etc.
    Params       map[string]interface{}
    DecisionPath string                 // "rule:exact_match" | "rule:pattern" | "llm:fallback"
    RuleID       *string
    Confidence   float64
    LatencyMs    int
    LLMTokens    *int
}
```

**TODO**:
- [x] Определить `Policy` interface и `PolicyDecision` struct (`policy/policy.go`)
- [x] Реализовать `PolicyExecutor` с телеметрией (rule-first, LLM-fallback) (`policy/policy.go`)
- [x] Создать Firestore collections для правил каждой Policy (`horizon_propagation_policies`, `horizon_canonization_rules`, `horizon_validation_rules`, `horizon_synthesis_rules`, `horizon_metric_specs`)
- [x] Реализовать базовые Policies: `PropagationPolicy`, `CanonizationPolicy`, `ValidationPolicy` (`policy/*.go`)
- [x] Интегрировать телеметрию Firestore (`horizon_policy_telemetry`) (`policy/telemetry.go`)
- [x] Интегрировать `PolicyExecutor` в `PropagationEngine` hot path (`cmd/server/main.go`)
- [x] Посеять минимальные propagation policies (7 правил: risk × SUPPLIES_TO/DEPENDS_ON/OWNS/COMPETES_WITH/LOCATED_IN/INFLUENCES/PRODUCES)
- [ ] **Dashboard для Policy Telemetry** (приоритет: средний, см. ниже)

**Dashboard для Policy Telemetry (отдельный TODO)**:
- [ ] BigQuery pipeline: экспорт `horizon_policy_telemetry` → BigQuery (batch или Pub/Sub → streaming)
- [ ] Looker Studio / Data Studio дашборд:
  - Ratio rule-based vs LLM-based решений по policy
  - Top LLM fallback reasons (DecisionPath анализ)
  - Cost per policy (tokens × pricing)
  - Latency distribution (p50/p95/p99) по policy
- [ ] Альтернатива (lightweight): простой Firestore-based UI в админке etigram.one для просмотра последних N решений и агрегированных метрик

**Acceptance**:
- Все Horizon-службы используют Policy interface для принятия решений
- Телеметрия показывает % rule-based vs LLM-based решений
- Можно добавить новое правило в Firestore без редеплоя и увидеть изменение ratio

---

### E) Semantic Propagation & Signals (risk/opportunity/trend/direction)

**Цель**: создать структурный слой для ответов на вопросы типа "What are the top supply disruption risks for Tesla battery logistics?" — из графа и фактов, а не через LLM каждый раз.

**Детальный план**: см. [signals-and-semantic-propagation-plan.md](signals-and-semantic-propagation-plan.md)

**TODO (критические пути)**:

- **Chain Waveguide + Chain Scope + Edge-Triggered Re-Propagation (высокий приоритет)**
  - **Проблема**: propagation работает по отдельным рёбрам без awareness цепочки как целого; supply chains рассыпаются на отдельные scope entries; новые рёбра не провоцируют переток уже накопленных сигналов.
  - **Решение** (4 фазы):
    - **Phase 1 (Edge Annotation)**: при создании рёбер в scout pipeline, проставлять `artifactKey` qualifier = canonical key MaterializedArtifact. Привязка ребра к цепочке. Kind-agnostic (не только supply chains).
    - **Phase 2 (Chain Scope)**: новый тип scope entry `/artifact/{canonicalKey}` в Lens. Загрузка всей цепочки целиком (все узлы + все рёбра) при создании/обновлении линзы. Новое поле `ScopeArtifacts` в Lens.
    - **Phase 3 (Waveguide)**: рёбра с `artifactKey` получают waveguide boost (1.10-1.25x) в `calculateDampedAmplitudeWithBreakdown`. BFS queue item получает `artifactKey` для chain-direction preference (coherence bonus при движении по одной цепочке).
    - **Phase 4 (Edge-Triggered Re-Propagation)**: при создании нового ребра проверять `fragment.State` обоих endpoint'ов. Если есть accumulated signal > threshold — асинхронно запускать re-propagation через новое ребро. Использует существующий `propagateHotPathAxisBased` с dedup через visited map.
  - **Детальный план**: см. `.cursor/plans/chain_waveguide_and_scope_*.plan.md`
  - **Acceptance**: сигнал `physical_flow/supply` по 5-hop supply chain доходит до конца с amplitude >= 0.60 (vs 0.32 сейчас); supply chains появляются в scope как единый entry; новые рёбра провоцируют re-propagation.

- **Critical Paths в Propagation (высокий приоритет)**
  - **Проблема**: сейчас затухание "тупое" — все рёбра равны, а некоторые ветки распространения важнее других.
  - **Решение**:
    - Определить **critical path** как путь с высокой `criticality` на рёбрах, между anchor-узлами Lens'а, или явно помеченный.
    - При создании/обновлении Lens вычислять critical paths (BFS/Dijkstra по criticality) и сохранять в `lens.ViewConfig["criticalPathEdges"]`.
    - В PropagationEngine: **меньше затухания** на critical paths (`dampingFactor *= 1.2`), **больше глубина** (`maxDepth += 1`), **приоритет** в hot path.
  - **Acceptance**: critical paths вычисляются при создании Lens; propagation через critical path затухает медленнее на 20%+.

- **Propagation Policies как данные (высокий приоритет)**
  - **Проблема**: сейчас propagation "тупой" — гоняет числовые дельты одинаково по любому ребру. Логистический риск должен течь сильно по `SUPPLIES_TO`, слабо по `COMPETES_WITH`.
  - **Решение**:
    - Firestore collection `horizon_propagation_policies` с матрицей `(metricPattern × relationType) → behavior` (passes, weight, maxDepth, dampingOverride, direction, criticalPathBoost).
    - `PolicyMatcher` выбирает policy по приоритету (exact match > pattern > wildcard).
    - PropagationEngine использует policy для каждой комбинации metric × edge.
  - **Acceptance**: можно задать "risk_supply_* проходит по SUPPLIES_TO с weight=1.2, по COMPETES_WITH с weight=0.3" без изменения кода.

- **Metric Registry & Handlers (средний приоритет)**
  - **Проблема**: нет контракта на aggregation/decay для метрик; все метрики обрабатываются одинаково.
  - **Решение**:
    - Firestore collection `horizon_metric_specs` с `MetricSpec` (name, kind, aggregationFunc, decayHalfLife, propagationThreshold).
    - `MetricHandler` interface: `Aggregate(inputs) → MetricValue`, `Decay(value, now) → MetricValue`.
    - Built-in handlers: `MaxHandler`, `SumCappedHandler`, `WeightedAvgHandler`, `ExponentialDecayHandler`.
  - **Acceptance**: risk-метрики агрегируются как "max", opportunity-метрики как "sum_capped".

- **Signal Types в Runtime Taxonomy (средний приоритет)**
  - **Проблема**: нет структурного слоя для risk/opportunity/trend/direction — всё "размазано" по фактам.
  - **Решение**:
    - Расширить `horizon_taxonomy/v1`: `allowedSignalKinds`, `signalTypes` (с label, synonyms, defaultMetric, propagationProfile).
    - `OntologyFact(kind=signal, type=risk:supply_disruption)` — не новая коллекция, а расширение существующей.
    - `NormalizeSignalType()` в `taxonomy/runtime.go`.
  - **Acceptance**: можно создать факт `kind=signal, type=risk:supply_disruption` и он валидируется runtime taxonomy.

- **Signal Synthesis (Fact → Signal) (средний приоритет)**
  - **Проблема**: каждый факт требует LLM для классификации как сигнал.
  - **Решение**:
    - Firestore collection `horizon_synthesis_rules` с детерминированными правилами `(factKind, factTypePattern, spherePattern) → (outputSignalType, metricName, confidenceMultiplier)`.
    - `SignalSynthesizer` применяет правила без LLM.
    - Интеграция в Scout/Watchdog: при обработке факта автоматически синтезируются сигналы.
  - **Acceptance**: 80%+ фактов покрыты synthesis rules и не требуют LLM для классификации.

- **Async LLM Queue для сложных Propagation-решений (низкий приоритет)**
  - **Когда**: классификация неоднозначна, conflict resolution, causal inference для explain trace.
  - **Решение**:
    - Отдельная Cloud Tasks queue `etigram-propagation-llm` с throttling.
    - `PropagationLLMTask` содержит контекст (fragment, metric, signals, damping state).
    - `PropagationLLMExecutor` подчиняется тем же правилам затухания (depth check, damping factor), использует `min_capability_score=6`.
  - **Acceptance**: LLM вызывается только async, подчиняется damping, не блокирует hot path.

- **Rollup & Explain (после Signal Synthesis)**
  - `GetFragmentRollup(fragmentID, metricKind)` возвращает агрегированный score + explain trace.
  - `ExplainTrace` содержит top contributors, propagation path, rules applied.
  - **Acceptance**: каждый rollup имеет explain trace с топ-3 contributors.

**Критерии приемки (общие)**:
- Rollup computation < 100ms (без LLM).
- Можно ответить на "Top supply disruption risks for Tesla battery logistics" из структурированных данных.
- Propagation policies и synthesis rules управляются через Firestore без редеплоя.

### E.1) Node Impedance — сопротивление среды при пропагации (идея/roadmap)

**Цель**: моделировать **пассивное сопротивление** узлов графа к проходящим сигналам. Сейчас пропагация знает только два модификатора на уровне узла: chokepoint amplification (усиление на бутылочных горлышках) и waveguide (ослабление затухания внутри цепочек). Отсутствует обратный механизм — **импеданс**: узел с высокой устойчивостью (диверсификация поставщиков, стратегические запасы, geo-diversification) должен ослаблять проходящий сигнал.

**Физическая аналогия**: в электрических цепях импеданс — свойство компонента (не провода), определяющее, как сильно он сопротивляется прохождению тока. Аналогично, `NodeImpedance` — свойство фрагмента, определяющее, как сильно он ослабляет сигнал определённой оси.

**Три вида "сопротивления" в реальном мире**:

| Вид | Описание | Моделируемость |
|---|---|---|
| **A. Passive impedance** | Узел ослабляет сигнал из-за своих свойств (диверсификация, запасы, ликвидность) | Ложится на физическую модель; реализуемо |
| **B. Active reaction** | Узел порождает контр-сигнал (контрсанкции, перестройка логистики) | Другая парадигма (trigger rules); отложено |
| **C. Structural adaptation** | Узел перестраивает связи (поиск новых поставщиков) | Scouting/discovery, не propagation |

**Реализация passive impedance (когда будут данные)**:

- Функция `NodeImpedance(frag *HorizonFragment, axis string) float64` возвращает множитель `[0.0, 1.0]`:
  - `1.0` = нет сопротивления (сигнал проходит полностью)
  - `0.5` = высокое сопротивление (сигнал ослабляется вдвое)
- Место вызова: рядом с `IsChokepointFragment` / `TransitAmplify` в propagation loop
- Impedance per axis: один узел может быть устойчив к `physical_flow/supply` (5 альтернативных поставщиков), но уязвим к `legal/access` (одна юрисдикция)

**Источники impedance value**:

| Источник | Axis | Пример |
|---|---|---|
| Количество incoming SUPPLIES_TO edges | physical_flow/supply | 5 поставщиков = impedance 0.6 |
| Geo-diversification (unique jurisdictions) | legal/access, geo/conflict | Операции в 8 странах = impedance 0.5 |
| Market liquidity / participant count | financial/settlement | Высоколиквидный рынок = impedance 0.4 |
| Strategic reserves / buffer capacity | logistics/capacity | Стратегические запасы лития = impedance 0.7 |

**Три топологических модификатора propagation**:

| Modifier | Level | Effect | Status |
|---|---|---|---|
| Chokepoint amplification | Node | Усиливает | Реализовано |
| Chain waveguide | Edge/path | Снижает затухание | В разработке |
| **Node impedance** | Node | Ослабляет | Roadmap (нужен источник данных) |

**Блокер**: для impedance нужен **надёжный continuous-valued** источник данных. Chokepoint работает потому что `GeoFeatureType = "strait"` — бинарный и надёжный. Impedance требует вычисляемых значений (count of edges, diversity metrics), и ошибки в них будут искажать всю пропагацию.

**Предварительные шаги**:
- [ ] Определить минимальный набор impedance-источников (начать с graph-structural: count of alternative edges)
- [ ] Реализовать `NodeImpedance()` как read-only diagnostic (логирование, не влияет на пропагацию)
- [ ] Калибровать на реальных данных (сравнить impedance-aware vs текущий propagation)
- [ ] Включить impedance в propagation после калибровки

**Оценка**: 2-3 недели (когда будут данные для калибровки)

### E.2) Гибкие кластеры сущностей для упрощения управления рисками (идея/требование)

**Цель**: реализовать гибкие, нечёткие и, возможно, пересекающиеся кластеры сущностей в Horizon, к которым будут применимы те же идеи — рисковое управление, распространение, ресурсная общность, что и к обычным сущностям.

**Идея**: предвычислять такие кластеры и упростить управление рисками от общего к частному помимо прямого propagation по рёбрам. Возможно, это будет лучше, чем микроуправление графом.

**Концепция**:
- Кластеры — это абстракция над множеством сущностей, которые могут быть связаны не только явными рёбрами графа, но и неявными признаками (география, отрасль, временной контекст, семантическая близость).
- Кластеры могут быть **нечёткими** (fuzzy) — сущность может принадлежать кластеру с определённой степенью принадлежности (membership score).
- Кластеры могут **пересекаться** — одна сущность может входить в несколько кластеров одновременно.
- Кластеры могут быть **динамическими** — формироваться на основе текущего состояния графа и фактов, а не только статически заданных правил.

**Применение**:
- **Рисковое управление**: риски могут распространяться на весь кластер, а не только по явным рёбрам графа. Например, риск санкций для страны может автоматически применяться ко всем организациям в этой стране.
- **Propagation**: упрощение распространения метрик/сигналов от кластера к его элементам и наоборот, без необходимости явного построения всех рёбер.
- **Ресурсная общность**: сущности в кластере могут разделять общие ресурсы, зависимости, уязвимости.

**TODO (предварительный план)**:
- Определить модель данных для кластеров:
  - Тип `Cluster` с полями: `id`, `name`, `type` (geographic, industry, semantic, custom), `membershipRules` (детерминированные правила), `membershipScores` (fuzzy scores для сущностей).
  - Связь кластеров с сущностями через `ClusterMembership` (fragmentId, clusterId, score, reason).
- Механизм предвычисления кластеров:
  - Background job для автоматического обнаружения и обновления кластеров на основе текущего состояния графа.
  - Правила формирования кластеров (географические, отраслевые, семантические).
  - Поддержка ручного создания/редактирования кластеров через API.
- Интеграция с Propagation Engine:
  - Поддержка propagation через кластеры (от кластера к элементам, от элементов к кластеру).
  - Приоритизация: propagation через кластеры может быть более эффективным, чем микроуправление отдельными рёбрами.
- Интеграция с рисковым управлением:
  - Применение рисков на уровне кластера с автоматическим распространением на элементы.
  - Агрегация рисков элементов в риски кластера.
- Интеграция с Watchdog:
  - Watchdog может отслеживать изменения на уровне кластеров, а не только отдельных сущностей.
  - Упрощение правил мониторинга через кластеры.

**Критерии приемки (предварительные)**:
- Можно создать кластер на основе географических, отраслевых или семантических признаков.
- Propagation через кластеры работает и может быть более эффективным, чем через отдельные рёбра.
- Риски могут применяться на уровне кластера с автоматическим распространением на элементы.
- Кластеры могут пересекаться, и сущность может принадлежать нескольким кластерам одновременно.

### F) Supply Chain Paths & Influence Links (после стабилизации)

**Цель**: моделировать конкретные цепочки поставок ("кто поставляет что кому") и политические/экономические влияния между акторами.

**Детальная архитектура**: см. [supply-chain-influence-model.md](../architecture/supply-chain-influence-model.md)

**Базовый слой оверлеев (materialized views)**: см. [materialized-artifacts.md](../architecture/materialized-artifacts.md)

**Проблема**: текущий граф имеет абстрактный продуктовый слой (`TRANSFORMS_INTO`) и связи org→product (`PRODUCES`/`CONSUMES`), но нет способа выразить "VW покупает батареи именно у CATL, которые сделаны из лития именно от Albemarle".

**Ключевые концепции**:

- **SupplyLink** — атомарная единица supply chain: `(Supplier, Product, Customer)`
  - Минимальный путь: `/org или /fac` → `/product` → `/org или /fac`
  - Минимум для стыковки: `/product` → `/org` → `*`
  - Tenant-specific (не global) — "по внутренним данным, мы знаем, что покупаем у CATL"
  
- **Influence Link** — политическое/экономическое влияние с qualifier `via`
  - Один объект (газ) может быть и товаром (SupplyLink), и рычагом влияния (InfluenceLink)
  - Петли допустимы (взаимозависимость: Russia→gas→Germany, Germany→€€€→Russia)

**TODO**:

- **Milestone 0: MaterializedArtifact (базовый тип для слоёв/оверлеев)**
  - [x] Ввести `MaterializedArtifact` как отдельную коллекцию/тип (tenant/global)
  - [x] Enum `MaterializedKind`, первый kind: `SUPPLY_CHAIN_PATH`
  - [x] Payload v1: упорядоченный массив узлов пути + опциональные "ребра" (supply-link / ontology transitions)
  - [x] Ссылки на метрики/observations + evidence refs (ontology_facts) без смешивания ответственности

- **Milestone 1: SupplyLink (высокий приоритет)**
  - [x] Добавить `FragmentTypeSupplyLink`
  - [x] Добавить поля `SupplierRef`, `CustomerRef`, `ProductRef` в HorizonFragment
  - [x] Валидация: SupplyLink требует `tenantId` (может быть tenant или "global" как assumption-layer)
  - [ ] API endpoint для создания SupplyLink
  - [x] Интеграция с Scout для автоматического обнаружения

- **Milestone 2: Path Building**
  - [x] Функция `BuildUpstreamPath(customer, opts)` с confidence propagation
  - [x] Tenant-first, global-fallback precedence на уровне сегмента
  - [x] Интеграция с MaterializedArtifact для сохранения путей
  - [ ] API endpoint для получения upstream/downstream paths
  - [ ] Кэширование построенных путей

- **Milestone 3: Influence Links**
  - [x] Стандартизация qualifiers (`via`, `mechanism`, `direction`, `reversible`, `conditionality`, `strength`)
  - [x] Поддержка influence qualifiers в ValidationPolicy
  - [x] Helper functions для ExtractInfluenceQualifiers, SetInfluenceQualifiers, ValidateInfluenceQualifiers
  - [ ] API для запроса influence paths

- **Milestone 4: HIL Integration**
  - [ ] Draft SupplyLink workflow (LLM inference → HIL verification)
  - [ ] UI для подтверждения/корректировки
  - [ ] Confidence upgrade при верификации

**Критерии приемки**:
- Можно построить путь "BMW ← CATL ← Albemarle" из tenant-specific SupplyLinks
- Confidence propagation работает через цепочку
- Influence edges с `via` qualifier валидируются и визуализируются

## Этап 1: Модели данных и репозитории

**Цель**: Создание базовых моделей данных и репозиториев для работы с Firestore

**Задачи**:
1. Создание моделей данных:
   - `HorizonFragment` (`backend/internal/horizon/models/fragment.go`)
   - `Frame` (`backend/internal/horizon/models/frame.go`)
   - `Lens` (`backend/internal/horizon/models/lens.go`)
   - `DependencyEdge` (`backend/internal/horizon/models/dependency_edge.go`)
   - `WatchdogConfig` (`backend/internal/horizon/models/watchdog.go`)

2. Создание репозиториев:
   - `HorizonFragmentRepository` (`backend/internal/repo/horizon_fragments.go`)
   - `FrameRepository` (`backend/internal/repo/frames.go`)
   - `LensRepository` (`backend/internal/repo/lenses.go`)
   - `DependencyEdgeRepository` (`backend/internal/repo/dependency_edges.go`)
   - `WatchdogConfigRepository` (`backend/internal/repo/watchdog_configs.go`)

3. Создание Firestore индексов:
   - Обновление `web/firestore.indexes.json`
   - Создание составных индексов для частых запросов

4. Миграции данных (если требуется):
   - Скрипты для миграции существующих данных
   - Валидация данных после миграции

**Критерии приемки**:
- ✅ Все модели данных соответствуют схемам из документации
- ✅ Репозитории реализуют CRUD операции
- ✅ Индексы созданы и протестированы
- ✅ Unit тесты для репозиториев написаны и проходят

**Оценка**: 2-3 недели

## Этап 2: Scout Engine (базовый)

**Цель**: Реализация базового функционала Scout Engine для сбора и верификации данных

**Задачи**:
1. Создание агентов:
   - `ScoutAgent` (`backend/internal/horizon/scout/agents/scout_agent.go`)
   - `CriticAgent` (`backend/internal/horizon/scout/agents/critic_agent.go`)
   - `ResolverAgent` (`backend/internal/horizon/scout/agents/resolver_agent.go`)

2. Создание основного движка:
   - `ScoutEngine` (`backend/internal/horizon/scout/engine.go`)
   - Планирование источников (`backend/internal/horizon/scout/planner.go`)
   - Выполнение запросов (`backend/internal/horizon/scout/executor.go`)

3. Интеграция с AI системой:
   - Использование `AITaskExecutor` для LLM вызовов
   - Constrained decoding для генерации JSON
   - Валидация схем через Pydantic (или Go эквивалент)

4. Workflow состояний:
   - Реализация конечного автомата состояний
   - Интеграция с `AgentJob` системой
   - Обработка ошибок и повторные попытки

5. Создание Frame:
   - Генерация Frame документов
   - Сохранение evidence и validation meta
   - Связь Frame с HorizonFragment

**Критерии приемки**:
- ✅ Scout Agent извлекает данные в правильном формате
- ✅ Critic Agent обнаруживает несоответствия
- ✅ Resolver Agent принимает решения
- ✅ Workflow проходит все состояния успешно
- ✅ Frame создаются с полной метаинформацией

**Оценка**: 4-5 недель

## Этап 3: Lens Subsystem

**Цель**: Реализация подсистемы управления Lens и подписками на HorizonFragment

**Задачи**:
1. Создание сервиса Lens:
   - `LensService` (`backend/internal/horizon/lens/service.go`)
   - Управление подписками (`backend/internal/horizon/lens/subscription.go`)
   - Пересчет метрик (`backend/internal/horizon/lens/calculator.go`)

2. Управление жизненным циклом:
   - Автоматическое управление `activeSubscribers`
   - Переходы между состояниями (ACTIVE → DORMANT → ARCHIVED)
   - Реактивация узлов при подписке

3. HTTP handlers:
   - `HorizonHandler` (`backend/internal/http/horizon/handlers.go`)
   - Endpoints для управления Lens
   - Валидация запросов

4. Интеграция с Project:
   - Связь Lens с Project
   - Контроль доступа через Project
   - Изоляция данных

**Критерии приемки**:
- ✅ Создание, обновление, удаление Lens работает
- ✅ Подписки на HorizonFragment управляются автоматически
- ✅ Жизненный цикл узлов работает корректно
- ✅ Контроль доступа через Project реализован

**Оценка**: 3-4 недели

## Этап 4: Propagation Engine

**Цель**: Реализация алгоритма распространения изменений в графе зависимостей

**Задачи**:
1. Создание Propagation Engine:
   - `PropagationEngine` (`backend/internal/horizon/propagation/engine.go`)
   - Алгоритм затухания волны (`backend/internal/horizon/propagation/wave_damping.go`)
   - Пороги значимости (`backend/internal/horizon/propagation/threshold.go`)
   - Ленивые вычисления (`backend/internal/horizon/propagation/lazy_eval.go`)

2. Топологическая оптимизация:
   - Поиск критических путей
   - Приоритетная обработка узлов
   - Оптимизация порядка пересчета

3. Интеграция с Realtime:
   - Публикация событий через Pub/Sub
   - WebSocket обновления для фронтенда
   - События распространения

4. Оптимизации производительности:
   - Батчинг обновлений
   - Кэширование графа
   - Параллельная обработка

**Критерии приемки**:
- ✅ Пороги значимости работают корректно
- ✅ Затухание волны предотвращает экспоненциальный рост
- ✅ Hot Path и Cold Path работают как ожидается
- ✅ Производительность соответствует требованиям (≤ 5 сек для Hot Path)

**Оценка**: 4-5 недель

## Этап 5: Watchdog

**Цель**: Реализация фонового мониторинга источников и автоматического обновления данных

**Задачи**:
1. Создание Watchdog Service:
   - `WatchdogService` (`backend/internal/horizon/watchdog/service.go`)
   - Мониторинг источников (`backend/internal/horizon/watchdog/monitor.go`)
   - Планирование проверок (`backend/internal/horizon/watchdog/scheduler.go`)

2. Интеграция с Cloud Functions:
   - Cloud Function для периодических задач
   - Cloud Scheduler для планирования
   - Обработка триггеров

3. Обнаружение изменений:
   - Вычисление хешей контента
   - Сравнение с предыдущими версиями
   - Определение порога изменений

4. Управление жизненным циклом:
   - Автоматический запуск для ACTIVE узлов
   - Автоматическая остановка для DORMANT узлов
   - Управление статусом (RUNNING, PAUSED)

5. Интеграция с Scout Engine:
   - Создание ScoutRequest при обнаружении изменений
   - Обработка результатов разведки
   - Обновление Watchdog конфигурации

**Критерии приемки**:
- ✅ Периодические проверки источников работают
- ✅ Изменения обнаруживаются корректно
- ✅ ScoutRequest создаются при превышении порога
- ✅ Жизненный цикл управляется автоматически

**Оценка**: 3-4 недели

## Этап 6: Графовая БД (отложено)

**Цель**: Миграция на специализированную графовую базу данных для сложных графовых запросов

**Статус**: Отложено до необходимости

**Причины отложения**:
- Firestore с индексами достаточен для начального этапа

## Intent / Taxonomy: Dynamic Spheres (Lens-driven)

**Требование**: поддержать “динамические сферы” из intent (LLM / precheck) — когда система сама предлагает/нормализует `spherePaths` под запрос, без ручного выбора пользователем.

**Зачем**: базовые запросы часто не мапятся на заранее известную сферу; сейчас всё падает в `/sphere/other`, что ухудшает переиспользуемость и качество графа/UX.

**Acceptance criteria**:
- `spherePaths` формируются из intent и **валидируются/нормализуются runtime taxonomy** (`/sphere/...` allowlist + synonyms).
- Если сферы нет в allowlist — система либо:
  - выбирает ближайшую существующую сферу (через synonyms/иерархию), либо
  - создаёт новую canonical `spherePath` через админ‑workflow и добавляет её в runtime taxonomy (желательно с synonyms).
- Для выбранных `spherePaths` автоматически создаётся sphere-fragment и `INFLUENCES` edge (**sphere → target**) с provenance `lens_intent_sphere_link`.
- Результат должен быть **переиспользуем**: похожий следующий запрос должен приходить к тем же `spherePaths` (при неизменной runtime taxonomy).
- Сложность миграции и синхронизации данных
- Дополнительные затраты на инфраструктуру

**Будущие задачи**:
1. Выбор графовой БД (Neo4j или Amazon Neptune)
2. Проектирование схемы миграции
3. Реализация синхронизации Firestore ↔ Graph DB
4. Миграция существующих данных
5. Обновление Propagation Engine для использования графовой БД

**Оценка**: 6-8 недель (когда будет необходимо)

## Интеграционное тестирование

После каждого этапа необходимо проводить интеграционное тестирование:

1. **Unit тесты**: Для каждого компонента
2. **Integration тесты**: Для взаимодействия компонентов
3. **End-to-End тесты**: Для полных сценариев использования
4. **Performance тесты**: Для проверки производительности

## Развертывание

### Этап 1-2: Внутреннее тестирование
- Развертывание в тестовой среде
- Тестирование базового функционала
- Исправление критических ошибок

### Этап 3-4: Бета-тестирование
- Развертывание для ограниченной группы пользователей
- Сбор обратной связи
- Оптимизация производительности

### Этап 5: Продакшн
- Полное развертывание
- Мониторинг и метрики
- Поддержка и исправление ошибок

## Приоритизация

**Высокий приоритет** (MVP):
- Этап 1: Модели данных и репозитории
- Этап 2: Scout Engine (базовый)
- Этап 3: Lens Subsystem (базовый)

**Средний приоритет**:
- Этап 4: Propagation Engine
- Этап 5: Watchdog

**Низкий приоритет**:
- Этап 6: Графовая БД

## Риски и митигация

### Риск 1: Производительность Propagation Engine

**Митигация**:
- Раннее прототипирование алгоритмов
- Performance тесты на больших графах
- Оптимизация и кэширование

### Риск 2: Стоимость LLM вызовов

**Митигация**:
- Оптимизация промптов для уменьшения токенов
- Кэширование результатов
- Использование более дешевых моделей где возможно

### Риск 3: Сложность интеграции с существующей системой

**Митигация**:
- Постепенная интеграция по этапам
- Тщательное тестирование на каждом этапе
- Документирование изменений

## Метрики успеха

### Технические метрики:
- Время выполнения Scout (Snapshot ≤ 1 мин, DeepDive ≤ 30 мин)
- Время пересчета Lens (Hot Path ≤ 5 сек)
- Точность извлечения данных (≥ 98%)
- Доступность системы (≥ 99.9%)

### Бизнес-метрики:
- Количество созданных HorizonFragment
- Количество активных Lens
- Средняя стоимость разведки
- Удовлетворенность пользователей

## См. также

- [Технические решения](technical-decisions.md)
- [Архитектурный обзор](../architecture/overview.md)
- [Supply Chain & Influence Model](../architecture/supply-chain-influence-model.md)
- [ТЗ: Domain-specific фазы Scout, composable Research/Critic, упрощение рёбер](SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ.md)
- [Risk Architecture](../../../risk-architecture.md) — Canonical Paths, Signal Propagation, Lazy Interpretation
