# Roadmap реализации системы мониторинга горизонта

## Обзор

Данный документ описывает план реализации системы мониторинга горизонта по этапам. Реализация разбита на логические этапы, каждый из которых добавляет новую функциональность и может быть развернут независимо.

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
  - Добавить клиентов организации в скаутинг, вызываемый вручную - тоже срочно
- **Wikidata QID resolver (suggest-only → gated auto-apply)**
  - Вынести поиск/скоринг Wikidata QID из `cmd/tools/horizon_find_missing_wikidata` в ядро как **suggest-only resolver** (без авто-записи).
  - Добавить кэширование/TTL, type-gating (например, `P31=Q5` для PERSON), и телеметрию качества (score распределения, false positives).
  - Добавить режим **auto-apply** только под feature-flag + строгие пороги/правила + dry-run.
  - Перед включением auto-apply: прогнать тщательную проверку качества на репрезентативной выборке (precision/recall, ручной аудит top ошибок).
- **Vertex AI Search для Horizon fragments**
  - Завершить overwrite re-export всех фрагментов в GCS в финальном формате Discovery Engine.
  - Перезапустить backfill (батчи) и убедиться, что поиск по fragments возвращает результаты.
  - Зафиксировать/проверить необходимые Firestore индексы под запросы Horizon.
- **QueryAddress**
  - Задеплоить “adult” реализацию QueryAddress (AST, скобки, NOT, корректный OR).
  - Подтвердить поведение `sphere:`: работает через `ontology_facts` и поддерживает только exact match (без wildcard).
  - Подтвердить поведение `location:`: best-effort `/loc/{country}/{admin1}/na` из `homeLocation`.
  - Убедиться, что background refresh корректно управляет подписками (added/removed paths) и не оставляет “мёртвых” подписок.
- **Документация (backend-first)**
  - Держать `architecture/taxonomy.md` и `architecture/query-address.md` синхронно с кодом (не обещать enforcement, которого нет).
  - В `IMPLEMENTATION_AUDIT.md` поддерживать актуальный “snapshot” (деплой/инфра/текущее состояние миграций).
- **Операционные контуры**
  - После завершения backfill: безопасно включить обратно Cloud Tasks / Scout queue (если была на паузе).

**Критерии приемки**:
- Re-export/backfill завершены, поиск по fragments в Vertex работает в проде.
- QueryAddress ведёт себя детерминированно (скобки/NOT/OR), документы и код не противоречат друг другу.
- Нет “обязательных” ручных шагов, кроме чётко описанных в docs.

### B) Улучшения онтологии (после стабилизации; минимальные элементы, без усложнения таксономии)

**Цель**: добавить выразительность “реальности” через универсальные онтологические оси (time/roles/modality/jurisdiction), не пытаясь сделать таксономию полной онтологией мира.

**TODO**:
- **Market Segments (канон `/segment/...` + признаки организации)**
  - Ввести в `HorizonFragment` (для `ORGANIZATION`/`FACILITY`) поля:
    - `marketSegments: []string` — массив канонических путей сегментов рынка, где работает организация.
    - `orgSize: string` — размер организации (например: `sme` | `le` | `enterprise` | `mega` | `na`).
  - Зафиксировать **плоский канонический путь фиксированной длины** для сегмента рынка:
    - формат: `/segment/{domain}/{vertical}/{geo}`
    - `geo`: либо ISO3166-1 alpha2 lowercase (2 символа), либо `global`.
    - `domain`/`vertical`: контролируемый словарь + synonyms (runtime taxonomy / справочник).
  - Обновить Scout/Research промпты: при разведке `ORGANIZATION`/`FACILITY` извлекать `marketSegments[]` и `orgSize` (роль не атрибут сущности; контекст выражается типом/направлением связи и `reason`).
  - Обновить Critic: если `marketSegments[]` пуст (или очевидно неполон относительно контекста) — добавлять в `missingInfo` точные вопросы/поисковые запросы для сегментов и размера организации.
  - (Опционально позже) Материализовать сегменты отдельными фрагментами с тем же canonical path (`/segment/...`) и продублировать связь “org → segment” через отдельный edge type (для индексации/UX).
- **Time (единый контракт времени)**
  - Формализовать и использовать консистентно: `occurredAt` (мир), `observedAt` (данные), `effectiveFrom/effectiveTo` (модель).
  - Добавить `effectiveFrom/effectiveTo` для отношений (edges/relations), чтобы связи не были “вечными”.
- **Modality (наблюдаемость/сценарность)**
  - Добавить `modality` на уровень фактов/утверждений (например: observed / reported / predicted / hypothetical).
  - Обновить Watchdog/Scout правила, чтобы propagation/обновления учитывали modality (например, predicted не всегда должен менять state).
- **Jurisdiction / Scope**
  - Явно договориться, что такое `locationPath`/`jurisdictionPath` для фактов и как он соотносится с `/loc/...`.
  - Свести к одному каноническому формату и задокументировать.
- **Roles/Relations как слой реальности**
  - Для `DependencyEdge`/relations добавить временной диапазон + минимальные атрибуты (confidence/sourceRef) при необходимости.
  - Расширить типы связей (минимальный OSINT‑слой, без “ролей” как атрибута человека):
    - `AUDITED_BY` (ORGANIZATION/FACILITY → ORGANIZATION): внешний аудитор компании.
    - `RELATIVE_OF` (PERSON ↔ PERSON): прямое родство (публичные родственники).
    - `RELATED_TO` (PERSON ↔ PERSON): прочие общеизвестные связи (бизнес‑партнёры/друзья/однокурсники и т.п.), контекст в `reason`/qualifiers.
  - Обновить Scout/Research промпты и Critic `missingInfo` под новые связи (аудитор компании; публичные родственники; public “related” связи) с жёсткими правилами по направлению/типам.
- **Обработка метрик при propagation (семантика поверх “доставки дельт”)**
  - Ввести registry/factory **именованных метрик**.
  - Для части метрик применять **детерминированные handler’ы** (algorithmic rules сначала): stateful / categorical / gated.
  - Волна propagation должна вызывать handler’ы для “именованных” метрик, чтобы изменения могли влиять на:
    - derived fields/flags,
    - дискретные статусы/режимы,
    - вторичные характеристики сущности,
    - (опционально) политику применения/порогов.
  - Требования: **идемпотентность**, **трассировка (explain/trace)**, **версионирование логики** (чтобы воспроизводить состояние).
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
- [ ] Альтернатива (lightweight): простой Firestore-based UI в админке hatl.one для просмотра последних N решений и агрегированных метрик

**Acceptance**:
- Все Horizon-службы используют Policy interface для принятия решений
- Телеметрия показывает % rule-based vs LLM-based решений
- Можно добавить новое правило в Firestore без редеплоя и увидеть изменение ratio

---

### E) Semantic Propagation & Signals (risk/opportunity/trend/direction)

**Цель**: создать структурный слой для ответов на вопросы типа "What are the top supply disruption risks for Tesla battery logistics?" — из графа и фактов, а не через LLM каждый раз.

**Детальный план**: см. [signals-and-semantic-propagation-plan.md](signals-and-semantic-propagation-plan.md)

**TODO (критические пути)**:

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
    - Отдельная Cloud Tasks queue `sanskrite-propagation-llm` с throttling.
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

- **Phase 0: MaterializedArtifact (базовый тип для слоёв/оверлеев)**
  - [x] Ввести `MaterializedArtifact` как отдельную коллекцию/тип (tenant/global)
  - [x] Enum `MaterializedKind`, первый kind: `SUPPLY_CHAIN_PATH`
  - [x] Payload v1: упорядоченный массив узлов пути + опциональные "ребра" (supply-link / ontology transitions)
  - [x] Ссылки на метрики/observations + evidence refs (ontology_facts) без смешивания ответственности

- **Phase 1: SupplyLink (высокий приоритет)**
  - [x] Добавить `FragmentTypeSupplyLink`
  - [x] Добавить поля `SupplierRef`, `CustomerRef`, `ProductRef` в HorizonFragment
  - [x] Валидация: SupplyLink требует `tenantId` (может быть tenant или "global" как assumption-layer)
  - [ ] API endpoint для создания SupplyLink
  - [ ] Интеграция с Scout для автоматического обнаружения

- **Phase 2: Path Building**
  - [x] Функция `BuildUpstreamPath(customer, opts)` с confidence propagation
  - [x] Tenant-first, global-fallback precedence на уровне сегмента
  - [x] Интеграция с MaterializedArtifact для сохранения путей
  - [ ] API endpoint для получения upstream/downstream paths
  - [ ] Кэширование построенных путей

- **Phase 3: Influence Links**
  - [x] Стандартизация qualifiers (`via`, `mechanism`, `direction`, `reversible`, `conditionality`, `strength`)
  - [x] Поддержка influence qualifiers в ValidationPolicy
  - [x] Helper functions для ExtractInfluenceQualifiers, SetInfluenceQualifiers, ValidateInfluenceQualifiers
  - [ ] API для запроса influence paths

- **Phase 4: HIL Integration**
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
   - Обновление `backend/firestore.indexes.json`
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

