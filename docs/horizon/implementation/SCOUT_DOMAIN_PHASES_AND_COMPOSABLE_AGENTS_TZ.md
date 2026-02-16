# ТЗ: Domain-specific фазы Scout, composable Research/Critic и упрощение модели рёбер

## Статус

**Draft** — к реализации после согласования.

## 1. Область и цели

### 1.1 Цели

1. **Развести выполнение Scout по доменам**: supply chain, governance (ownership/equity), market, strategic, general — с отдельными фазами и без дублирования.
2. **Сделать Critic и Research composable**: отдельные валидаторы и исследовательские стратегии по домену вместо одного монолитного промпта.
3. **Жёстко отделить специализированные фазы от general**: по intent запускать только нужный домен; general — только при неопределённом intent; каждая фаза пишет только разрешённые типы рёбер (allowlist).
4. **Упростить модель рёбер**: консолидация типов через qualifiers, семантические группы для фильтрации, deprecation части типов.

### 1.2 Вне области (non-goals)

- Изменение формата хранения графа (Firestore коллекции, структура документов) — только логика execution и промптов.
- Полная замена intent resolver — используем существующий вывод intent (типы доменов) и маппим в domain mode.
- Изменение canonical path для facility (подтипы warehouse и т.д.) — выносится в отдельное ТЗ.

---

## 2. Текущее состояние (кратко)

- **Specialties**: `supply_chain`, `equity`, `management`, `products_and_services` — передаются в job state и добавляют блоки в единый промпт Scout/Research/Critic. Один вызов LLM на всё.
- **Recursion**: `shouldRecurse()` фильтрует по specialty (supply_chain → только supply edge types; equity → только ownership types).
- **Critic**: один `buildValidationPrompt()` ~250+ строк с множеством MANDATORY блоков под все типы сущностей и specialties.
- **Research**: один `buildEnrichmentPrompt()` с условными блоками по specialties.
- **Domain contracts**: домены из intent (например `/domain/v1/supply-chain/...`) обрабатываются через domain expansion job; нет единого «domain mode», управляющего фазами обычного scouting job.
- **Рёбра**: 58 DependencyType, 47 правил канонизации, 6 слоёв обработки; много «родственных» типов (RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH и т.д.).

---

## 3. Целевая архитектура

### 3.0 Домены как исполнимые контракты (опора на существующий код)

**Домены в системе — это исполнимые контракты, а не просто тип сущности.** Эта логика уже есть в коде:

- **DomainContract** (`pipeline/contract.go`): контракт с полями `ContractType`, `CanonicalPath`, `Facets`; путь вида `/domain/v1/{contractType}/{facetKey}/{facetValue}...`.
- **Intent resolution**: IntentResolver возвращает домены с **распознанным contractType** (allowlist в `domain_resolver_agent.go`, пути в scope).
- **ContractRegistry + ContractHandler**: по `contractType` выбирается обработчик, который выполняет контракт (`Execute(ctx, eng, in)`). Тип контракта определяет, какая логика запускается.
- **Domain engine** (`domain/engine.go`): для доменов вызывается Run (resolve → steer → gate → enrich → assemble → snapshot). Поведение внутри движка зависит от **contractType** через `steeringProfileForContractType(contractType)`.
- **Steering profiles** (`domain/steering_profiles.go`): маппинг contractType → SteeringProfileName (supply_chain, regulatory_exposure, competitive_landscape, geographic_concentration и др.). По профилю уже заданы ожидаемые типы рёбер (`expectedRelationsForContractType`), фильтры типов связей, чувствительность к регуляторике и т.д.
- **Комментарий в engine.go**: «domains are contracts, not real-world entities» — выполнение идёт до проверки freshness для обычных сущностей.

**Вывод для ТЗ:** Индивидуальные фазы (какой Research/Critic/allowlist запускать) должны опираться на **распознанные типы доменных контрактов** (из intent/scope), а не на отдельный параллельный «domain mode». Реализация: domain mode в этом ТЗ считать производным от contract type (и steering profile): при появлении в job scope домена с `contractType=supply-chain` (или из intent) выставляем mode supply_chain и запускаем соответствующие фазы; при `contractType=sanctions-exposure` / `regulatory-impact` — strategic; при `competitive-landscape` / `market-opportunity` — market; при ownership-structure / equity-контрактах — governance. Маппинг contractType → domain mode зафиксировать в коде, используя по возможности существующий `steeringProfileForContractType` и расширяя его при необходимости (например ownership-structure → governance).

### 3.1 Domain mode (единая типизация выполнения)

**Domain mode** — enum (или мульти-значение при composite), определяющий, какие фазы и какие стратегии Research/Critic запускаются. **Источник истины** — распознанные типы доменных контрактов (contractType из intent/scope) и маппинг contractType → steering profile → domain mode (см. п. 3.0).

| Domain mode | Описание | Источник (contractType / intent / scope) |
|-------------|----------|----------------------------|
| `supply_chain` | Цепочки поставок до продуктов и потребителей | intent: supply-chain, value-chain, procurement, sourcing; scope с `/domain/v1/supply-chain/...` |
| `governance` | Ownership, контроль, совет, ключевые лица | intent: equity, ownership, corporate structure; scope с доменами regulatory/ownership |
| `market` | Игроки, доли, submarkets, sizing | intent: market-opportunity, competitive-landscape, market-sizing |
| `strategic` | Decision points, mechanisms, export control | intent: sanctions, export control, regulatory-impact; scope с `/decision/`, `/mechanism/` |
| `general` | Текущее универсальное поведение | intent не определён или явно general |

**Composite**: при нескольких доменах в intent (до 3) — массив mode: `["supply_chain", "governance"]`. Каждый mode получает свой прогон domain_expansion и domain_validation в рамках одного job (см. фазы).

**Маппинг contractType / intent → domain mode** (зафиксировать в коде/конфиге):

- По scope или по результату IntentResolver извлекаем доменные пути `/domain/v1/{contractType}/...` и contractType.
- contractType → steering profile уже реализован в `domain/steering_profiles.go`; дополняем маппинг **steering profile → domain mode** (supply_chain→supply_chain, regulatory_exposure→strategic, competitive_landscape→market, при необходимости ownership-structure→governance).
- Существующие specialties сохраняем как fallback при отсутствии доменов в scope; при наличии распознанных контрактов приоритет у contract type.

### 3.2 Фазы выполнения ScoutJob

Все фазы выполняются в рамках **одного** ScoutJob. Порядок фиксирован.

| Фаза | Назначение | Кто участвует | Что пишет (allowlist) |
|------|------------|---------------|------------------------|
| **Phase 0 — structural** | Создать/обновить якорный фрагмент и минимальные связи для появления ноды в графе | ScoutAgent (structural prompt) | Fragment + только: LOCATED_IN, OPERATES_IN; опционально базовые PRODUCES/CONSUMES если однозначно из контента. |
| **Phase 1 — domain_expansion** | Доменно-специфичное извлечение и обогащение | Research (стратегия по domain mode) + при необходимости Scout (domain-specific extraction) | Только рёбра из **allowlist данного domain mode** (см. п. 3.4). Не пишет рёбра других доменов. |
| **Phase 2 — domain_validation** | Проверка полноты и корректности по домену | Critic (набор валидаторов по domain mode) | Не пишет данные; только missingInfo и conflicts. |
| **Phase 3 — decomposition** | Планирование дочерних job’ов | Engine (shouldRecurse, schedule) | Не пишет граф; создаёт дочерние job’ы с наследованием domain mode. |

- **General mode**: при `domainMode=general` Phase 1 и Phase 2 используют универсальные (текущие) стратегии Research и Critic с широким allowlist. Structural и decomposition — как сейчас.
- **Специализированный mode**: при `supply_chain` / `governance` / `market` / `strategic` в Phase 1 и Phase 2 участвуют только соответствующие стратегии; allowlist — только свой домен (см. п. 3.4).

### 3.3 Разделение специализированных фаз и general

- **По запуску**: если intent дал домен(ы) — в job выставляется соответствующий domain mode (или composite). **General не запускается** для того же intent; general только если intent = general/unknown.
- **По записи**: у каждой доменной стратегии (Research) и у каждой фазы expansion — **явный allowlist типов рёбер и фрагментов**. Persistence layer (или слой перед записью) проверяет: при domain_expansion для mode X разрешены только рёбра из allowlist(X). Так специализированные фазы не создают рёбра «чужого» домена.
- **Повторный проход**: если позже по тому же фрагменту запускается другой домен (например сначала supply_chain, потом governance) — политика **merge**: новые рёбра добавляются, существующие не удаляются другим доменом.

### 3.4 Allowlist по domain mode

Используются для: (1) выбора, какие рёбра имеет право создавать Phase 1 для данного mode; (2) фильтрации в UI/API; (3) подсказок LLM (в промпте указывать только допустимые типы).

| Domain mode | Разрешённые типы рёбер (allowlist) |
|-------------|-------------------------------------|
| supply_chain | PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN |
| governance | OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED |
| market | PARTICIPATES_IN (marketRole + metrics.market_share), HAS_SUBMARKET |
| strategic | CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO |
| general | Полный набор (все типы, как сейчас) |

Типы фрагментов: для supply_chain — ORGANIZATION, FACILITY, PRODUCT, DEPOSIT, GEO_REGION; для governance — ORGANIZATION, PERSON, FACILITY; и т.д. (детализировать в реализации.)

---

## 4. Composable Critic

### 4.1 Интерфейс валидатора

```go
// Validator проверяет фрагмент и связи в рамках своего домена.
// Возвращает конфликты и missingInfo; не изменяет граф.
type CriticValidator interface {
    DomainMode() string       // supply_chain | governance | market | strategic | general
    Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error)
}

type CriticValidatorInput struct {
    Fragment   *models.HorizonFragment
    Relations  []DiscoveredRelation
    SupplyChains []SupplyChain
    SourceContent string
    GraphContext  string
}
```

### 4.2 Набор валидаторов (по домену)

| Валидатор | Domain mode | Ответственность | Длина промпта (целевая) |
|-----------|------------|-----------------|--------------------------|
| CoreValidator | (всегда) | Canonical path, базовые поля, lifecycle stage, entityPath/sectorPath | 40–60 строк |
| SupplyChainValidator | supply_chain | Цепочки actor→product→actor, PRODUCES/CONSUMES по facility, EXTRACTS_FROM для extractive, gaps | 50–70 строк |
| GovernanceValidator | governance | OWNS/SUBSIDIARY_OF, ownership_percent, board, key persons, control | 50–70 строк |
| MarketValidator | market | PARTICIPATES_IN (marketRole, market_share), sizing, concentration, HAS_SUBMARKET | 40–60 строк |
| StrategicValidator | strategic | Уже есть: ValidateStrategicOverlay (DECISION_POINT, MECHANISM) | as-is |
| PersonValidator | governance / general | RELATIVE_OF, RELATED_TO, employment | 30–50 строк |
| ExtractiveValidator | supply_chain / general | Deposits, reserves, production metrics для mining/O&G | 40–50 строк |
| GeneralValidator | general | Текущий монолитный блок (временно), пока не разобьём на домены | — |

Для каждого job в Phase 2 вызывается только цепочка валидаторов, соответствующая domain mode (и при composite — несколько цепочек по очереди). Общий результат — объединение `conflicts` и `missingInfo` от всех вызванных валидаторов.

### 4.3 Миграция текущего промпта

- Вынести из текущего `buildValidationPrompt()` блоки в отдельные функции/шаблоны по валидаторам.
- CoreValidator: canonical path rules, базовые MANDATORY (lifecycle, org path format, product path format, geo path format).
- Остальные — по таблице выше. Strategic — переиспользовать `buildStrategicValidationPrompt()`.
- Общий entry point: `CriticAgent.Validate()` вызывает `CriticPipeline.Validate(input, domainModes)` → для каждого mode запускает соответствующие валидаторы и мержит результаты.

---

## 5. Composable Research

### 5.1 Интерфейс стратегии

```go
// ResearchStrategy обогащает фрагмент в рамках своего домена (по missingInfo и контексту).
type ResearchStrategy interface {
    DomainMode() string
    Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error)
}

type ResearchStrategyInput struct {
    Fragment    *models.HorizonFragment
    MissingInfo []string
    GraphContext string
    ChainContext string
    // ... существующие поля EnrichFragmentWithGrounding
}

type ResearchStrategyOutput struct {
    FragmentUpdates map[string]interface{}
    Relations       []DiscoveredRelation
    SupplyChains    []SupplyChain
}
```

### 5.2 Стратегии по домену

| Стратегия | Domain mode | Цель | Allowlist рёбер (генерировать только их) |
|-----------|-------------|------|------------------------------------------|
| SupplyChainResearch | supply_chain | Upstream/downstream, продукты по звеньям, facility→product | PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM |
| GovernanceResearch | governance | Владельцы, дочерние, совет, ключевые лица | OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED |
| MarketResearch | market | Игроки, доли, submarkets, sizing | PARTICIPATES_IN (marketRole, metrics.market_share), HAS_SUBMARKET |
| StrategicResearch | strategic | Decision points, mechanisms (уже частично есть) | CONTROLS_ACCESS, USES_MECHANISM, APPLIES_TO, GATEKEEPS_DECISION, IMPOSES_CONDITION |
| GeneralResearch | general | Текущее универсальное обогащение | Все типы |

В Phase 1 для данного job вызывается одна или несколько стратегий по domain mode. Результат мержится; при записи применяется allowlist соответствующего mode (см. п. 3.4).

### 5.3 Связь с specialties

- Если в запросе передан только `specialties` (без intent): маппинг specialties → domain mode: `supply_chain`→supply_chain, `equity`→governance, `management`→часть governance, `products_and_services`→часть supply_chain/general. Один specialty может задать один mode; несколько specialties — composite mode.
- Если передан intent с доменами: intent имеет приоритет; итоговый domain mode = от intent, при необходимости объединённый с specialties (например intent=supply_chain + specialty=equity → composite [supply_chain, governance]).

---

## 6. Упрощение модели рёбер

### 6.1 Консолидация типов (один тип + опциональный qualifier)

Выполнять последовательно; после каждого шага — обновить canonicalization, normalizer, validation policy и промпты. **Qualifiers не являются обязательными**: ребро может существовать с типом без qualifier; qualifier уточняет семантику, когда известен.

| Шаг | Было | Стало | Qualifier (опционально) |
|-----|------|--------|-------------------------|
| 1 | RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH | ASSOCIATED_WITH | subtype: related \| affiliated \| partnership |
| 2 | EMPLOYED_BY, WORKS_FOR, KEY_PERSON, HAS_KEY_PERSON | EMPLOYED_BY | role: employee \| key_person \| executive (HAS_KEY_PERSON → нормализация в EMPLOYED_BY, role=key_person) |
| 3 | RENAMED_TO, REBRANDED_TO, LEGAL_SUCCESSOR_OF, MERGED_INTO, SPUN_OFF_FROM, ACQUIRED_BY | CORPORATE_TRANSITION | transitionType: rename \| rebrand \| succession \| merger \| spinoff \| acquisition |
| 4 | BROADENS_TO, NARROWS_TO, RELATED_DOMAIN | DOMAIN_RELATION | direction: broadens \| narrows \| related |
| 5 | Связи **домен → объект** (entity и др.): FOCUSES_ON (текущий), ABOUT, AFFECTS, SURFACES, INVOLVES_ACTOR, IN_GEO_SCOPE | **FOCUSES_ON** | facet: actor \| geo \| topic \| impact \| surfaces (опционально; при нормализации: INVOLVES_ACTOR→actor, IN_GEO_SCOPE→geo, ABOUT→topic, AFFECTS→impact, SURFACES→surfaces) |
| 6 | DOMINATES (ORG/FAC→MARKET) | PARTICIPATES_IN | marketRole: participant \| leader \| dominant; доля рынка — в metrics: market_share (0..1 или percent) |

**Домен–объект и домен–домен:** для связей от домена к другим объектам (entity, product, mechanism и т.д.) используется **только FOCUSES_ON** (опционально с qualifier `facet`). Для связей **домен ↔ домен** — **только DOMAIN_RELATION** (опционально `direction`). Типы ABOUT, AFFECTS, SURFACES, INVOLVES_ACTOR, IN_GEO_SCOPE deprecated: при записи и при чтении нормализовать в FOCUSES_ON + facet при известной семантике; без facet — допустимо.

**Рынок–орг:** один тип связи — PARTICIPATES_IN. Роль игрока (лидер / доминирующий / рядовой участник) задаётся qualifier `marketRole` (опционально); доля рынка — метрика на ребре `market_share` (value 0..1 или в percent, unit в MetricValue). Тип DOMINATES deprecated: при записи и при чтении отображать в PARTICIPATES_IN + marketRole=leader или dominant + переносить/ожидать market_share в metrics.

Новые типы (ASSOCIATED_WITH, CORPORATE_TRANSITION, DOMAIN_RELATION) добавить в `DependencyType`; старые пометить deprecated и при записи нормализовать в новый тип + qualifier при возможности; qualifier не обязателен. При чтении: старые типы по маппингу отображать в новый + qualifier (для API/UI).

### 6.2 Семантические группы (только чтение/фильтрация)

Ввести справочник (в коде или конфиге): каждый DependencyType → semanticGroup.

| semanticGroup | Назначение |
|---------------|------------|
| supply_chain | PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN |
| governance | OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, ADMINISTERS |
| market | PARTICIPATES_IN, HAS_SUBMARKET (DOMINATES → PARTICIPATES_IN + marketRole + market_share) |
| strategic | CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO |
| domain | FOCUSES_ON (домен→объект), DOMAIN_RELATION (домен↔домен) |
| person | EMPLOYED_BY, RELATIVE_OF, RELATED_TO, ASSOCIATED_WITH, MEMBER_OF |
| transition | CORPORATE_TRANSITION |
| other | REGULATES, AUDITED_BY, COMPETES_WITH, INFLUENCES, DEPENDS_ON, … |

Использование: фильтрация в API/UI, выбор allowlist по domain mode, `shouldRecurse()` и domain-specific traversal. Новые «типы» по возможности не добавлять, а расширять qualifiers у существующих типов.

### 6.3 Deprecation

- WORKS_FOR, HAS_KEY_PERSON → при записи и в normalizer приводить к EMPLOYED_BY + role (опционально).
- DOMINATES → при записи и при чтении отображать в PARTICIPATES_IN + qualifier marketRole (leader/dominant) + метрика market_share на ребре.
- ABOUT, AFFECTS, SURFACES, INVOLVES_ACTOR, IN_GEO_SCOPE → при записи и при чтении отображать в FOCUSES_ON + facet при известной семантике (topic, impact, surfaces, actor, geo); qualifier не обязателен.
- Типы из п. 6.1 после консолидации: в справочнике пометить deprecated; в промптах и контрактах использовать только канонические типы (FOCUSES_ON, DOMAIN_RELATION для доменов). Миграция существующих документов — опционально (можно маппить при чтении).

### 6.3.1 Обязательный контракт при реализации (не забыть)

**В процессе исполнения ТЗ обязательно соблюдать:**

1. **В промптах (Scout, Research, Critic)** фигурируют **только новые канонические типы рёбер**; qualifiers в промптах указываются как **опциональные** (модель может вернуть ребро без qualifier). Устаревшие типы (RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH, WORKS_FOR, KEY_PERSON, HAS_KEY_PERSON, DOMINATES, RENAMED_TO, REBRANDED_TO, LEGAL_SUCCESSOR_OF, MERGED_INTO, SPUN_OFF_FROM, ACQUIRED_BY, BROADENS_TO, NARROWS_TO, RELATED_DOMAIN, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, SURFACES в старом смысле) **не упоминаются** в инструкциях для LLM, в примерах выходного JSON и в списках допустимых relationType. Модель получает только: ASSOCIATED_WITH (subtype опц.), EMPLOYED_BY (role опц.), CORPORATE_TRANSITION (transitionType опц.), DOMAIN_RELATION (direction опц., только домен↔домен), FOCUSES_ON (facet опц., только домен→объект), PARTICIPATES_IN (marketRole + metrics.market_share опц.) и остальные не консолидированные типы.

2. **В бизнес-логике** (allowlists по domain mode, shouldRecurse, validation policy по типу ребра, фильтры UI/API, semantic groups) используются **только канонические типы**. Устаревшие типы **не участвуют**: не входят в allowlist, не фигурируют в условиях рекурсии, не используются в валидации как целевой тип. Единственное место, где устаревший тип появляется — **нормализатор/канонизация**: на входе приходят старые значения (от старых данных или от legacy-источников), и они **только преобразуются** в новый тип + qualifier; дальше по коду идут уже канонические типы.

3. **Policy check и policy enforce** работают **только с каноническими типами**; qualifiers опциональны (правила могут проверять тип с учётом qualifier при его наличии). Правила задаются для канонических типов (ASSOCIATED_WITH, EMPLOYED_BY, CORPORATE_TRANSITION, DOMAIN_RELATION, FOCUSES_ON, PARTICIPATES_IN и т.д.). Устаревшие типы в policy не фигурируют: ребро до попадания в ValidationPolicy / PolicyChecker / enforcement уже пройдёт слой канонизации. При реализации: обновить `validation_policy.go`, edge direction policies, `policy_checker.go` и enforcement под новые типы; правила для deprecated типов удалить или заменить на правила для канонических.

4. **Чтение старых данных**: при отдаче в API/UI допускается маппинг «старый тип в хранилище → новый тип + qualifier при отдаче», чтобы клиенты видели единую модель. Внутри пайплайна (Scout/Research/Critic, persistence, propagation) работаем только с каноническими типами.

**Чеклист при реализации (Этап 5 и при добавлении промптов в этапах 2–3):**
- [ ] В промптах нет упоминаний deprecated типов (RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH, DOMINATES, WORKS_FOR, HAS_KEY_PERSON, KEY_PERSON, RENAMED_TO, ABOUT, AFFECTS, SURFACES, INVOLVES_ACTOR, IN_GEO_SCOPE, …) в качестве допустимых выходов.
- [ ] В промптах qualifiers указаны как опциональные (допустимо ребро без qualifier).
- [ ] Allowlists и semantic groups: для доменов только FOCUSES_ON (домен→объект) и DOMAIN_RELATION (домен↔домен); остальные канонические типы по таблице 6.2.
- [ ] В shouldRecurse, validation policy, фильтрах по типу ребра используются только канонические типы.
- [ ] **Policy check и enforce**: правила заданы только для канонических типов; qualifiers в правилах опциональны; вход в policy — после канонизации; deprecated типы из policy убраны.
- [ ] Устаревшие типы обрабатываются только в слое нормализации/канонизации (вход → преобразование → выход канонический).

### 6.4 Регламент allowlist по домену

Зафиксировать в коде и в документации (data-model или отдельный doc): для каждого domain mode список разрешённых DependencyType и, при необходимости, FragmentType. При добавлении нового типа ребра указывать semanticGroup и в какие domain allowlist он входит. Это уменьшит появление «размытых» типов.

### 6.5 Упрощение канонизации и policy (без изменения онтологии)

Цель: сократить дублирование правил, оставить один источник истины для переписывания типов рёбер и упростить поддержку. Онтология (набор типов, направлений, семантика) не меняется.

**6.5.1 Один источник правил переписывания (type + endpoint types)**

Сейчас одни и те же правила переписывания (тип ребра + типы концов) заданы в двух местах: `policy.ResolveEdgeDirectionAutoFix` и `models.CanonicalizeEdge` / `CanonicalizeEdgeWithPaths`. Например: RELATED_TO + entity+market → PARTICIPATES_IN, OWNS+org+program → ADMINISTERS, LOCATED_IN+org+facility → OPERATES_IN, FOUNDED+community/org+person → swap и т.д. В результате при изменении логики нужно править два файла.

- **Требование:** Все чистые переписывания по (edgeType, sourceType, targetType) и по (edgeType, sourceType, targetType, sourcePath, targetPath) сосредоточить в **одном месте** — `models.CanonicalizeEdge` и `models.CanonicalizeEdgeWithPaths`. На вход канонизации ребро уже приходит после нормализатора (только канонические типы).
- **Policy-слой** (`ApplyEdgeCanonicalizationPolicy`) вызывает только: (1) два особых кейса, которые не сводятся к типу+концы — REGULATES при path /org→/gov (swap) и INVESTS_IN+OwnershipEvidence→OWNS; (2) один вызов `CanonicalizeEdgeWithPaths` для всех остальных переписываний.
- **ResolveEdgeDirectionAutoFix:** либо удалить, либо сделать тонкой обёрткой над вызовом models-канонизации с приведением результата к структуре `EdgeDirectionAutoFix` (для обратной совместимости вызовов из policy_checker и тестов). Новые правила добавлять только в models.

**6.5.2 EdgeDirectionPolicies — только канонические типы**

В `edgesemantics.EdgeDirectionPolicies` сейчас заданы правила для deprecated-типов: WORKS_FOR, KEY_PERSON, HAS_KEY_PERSON, RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH, DOMINATES, ABOUT, AFFECTS, SURFACES, INVOLVES_ACTOR, IN_GEO_SCOPE, RELATED_DOMAIN, BROADENS_TO, NARROWS_TO, отдельные transition-типы.

- **Требование:** После консолидации типов (п. 6.1) в EdgeDirectionPolicies оставить **только канонические типы**: EMPLOYED_BY, ASSOCIATED_WITH, PARTICIPATES_IN, FOCUSES_ON, DOMAIN_RELATION, CORPORATE_TRANSITION и остальные из таблицы 6.2. Правила для deprecated-типов удалить — на вход policy ребро попадает уже после нормализатора, то есть с каноническим типом. Это уменьшает размер карты и упрощает поддержку.

**6.5.3 Domain–domain allowlist в CanonicalizeEdge**

В блоке канонизации для пары (DOMAIN, DOMAIN) сейчас в allowlist перечислены: FocusesOn, RelatedDomain, BroadensTo, NarrowsTo, Affects, DependsOn; остальное → FOCUSES_ON.

- **Требование:** После консолидации для связей домен↔домен допустимы только **FOCUSES_ON** и **DOMAIN_RELATION**. В `CanonicalizeEdge` для domain–domain: в switch оставить только `DependencyTypeFocusesOn, DependencyTypeDomainRelation` как разрешённые; в default — переписывать в FOCUSES_ON. Старые типы (RelatedDomain, BroadensTo, NarrowsTo, Affects и т.д.) на вход этого блока не попадают (уже приведены к DOMAIN_RELATION или FOCUSES_ON в нормализаторе).

**6.5.4 SymmetricEdgeTypes — только канонические типы**

Сейчас в `models.SymmetricEdgeTypes` перечислены, в частности: PartnersWith, RelatedTo, RelatedDomain.

- **Требование:** После консолидации обновить карту: использовать только канонические типы. Например: ASSOCIATED_WITH (вместо PartnersWith, RelatedTo), DOMAIN_RELATION (вместо RelatedDomain, если симметричность сохраняется), RELATIVE_OF, COMPETES_WITH. Удалить ключи для deprecated-типов.

**6.5.5 Хелпер для типов «участник» (entity)**

В `edge_canonicalization.go` и при необходимости в policy многократно повторяется условие вида (sourceType == Organization || sourceType == Facility || sourceType == Community || sourceType == Program).

- **Требование:** Ввести в пакете models (или edgesemantics) хелпер, например `IsParticipantEntity(ft FragmentType) bool` или срез допустимых типов, и использовать его в правилах канонизации и, при необходимости, в policy. Это уменьшает дублирование и снижает риск рассинхрона при добавлении новых типов фрагментов. Состав «участник» не меняет онтологию — только способ проверки.

**6.5.6 Порядок применения в ApplyEdgeCanonicalizationPolicy**

Зафиксировать единый порядок: (1) policy-специфичные кейсы (REGULATES /org→/gov swap, INVESTS_IN+OwnershipEvidence→OWNS); (2) один вызов канонизации по типам и путям (CanonicalizeEdgeWithPaths), в котором сосредоточены все остальные переписывания; (3) переход fallback (transition_unclassified → INFLUENCES при необходимости); (4) fallback для неизвестного типа ребра → INFLUENCES с penalty. Не дублировать одни и те же условия в ResolveEdgeDirectionAutoFix и в CanonicalizeEdge.

**Чеклист при реализации (Этап 5):**
- [ ] Все правила переписывания (type + endpoint types/paths) находятся только в models.CanonicalizeEdge / CanonicalizeEdgeWithPaths; в policy — только два особых кейса и вызов канонизации.
- [ ] ResolveEdgeDirectionAutoFix удалён или является обёрткой над models без дублирования правил.
- [ ] EdgeDirectionPolicies содержит только канонические типы; записи для deprecated удалены.
- [ ] В CanonicalizeEdge для domain–domain в allowlist только FOCUSES_ON и DOMAIN_RELATION.
- [ ] SymmetricEdgeTypes обновлён под канонические типы (ASSOCIATED_WITH, DOMAIN_RELATION и т.д.).
- [ ] Введён хелпер для проверки «участник» (entity) и использован в правилах канонизации.

---

## 7. План реализации (по этапам)

### Этап 1: Подготовка (domain mode, allowlists, семантические группы)

**Задачи:**

1. Ввести в коде тип `DomainMode` (string constants: supply_chain, governance, market, strategic, general) и при необходимости список для composite.
2. Реализовать маппинг **contract type → domain mode**: по доменам в scope (canonical path `/domain/v1/{contractType}/...`) или по результату IntentResolver извлекать contractType; использовать существующий `steeringProfileForContractType` и добавить слой steering profile → domain mode; при отсутствии доменов — fallback на specialties. Заполнять `job.State["domainMode"]` или `job.State["domainModes"]` (массив при composite).
3. Добавить справочник allowlist по domain mode (таблица из п. 3.4) в код (константы или конфиг).
4. Добавить справочник DependencyType → semanticGroup (п. 6.2); использовать в существующем `shouldRecurse()` и при фильтрации (без изменения поведения, только подготовка).
5. Документировать в data-model или в отдельном md: domain modes, allowlists, semantic groups.

**Критерии приёмки:** по завершении job может иметь `domainMode`/`domainModes`; в коде доступны allowlists и semantic groups; поведение Scout пока без изменений.

---

### Этап 2: Composable Critic

**Задачи:**

1. Ввести интерфейс `CriticValidator` и `CriticValidatorInput` (п. 4.1).
2. Реализовать CoreValidator (canonical path, базовые поля, lifecycle) — вынести из текущего `buildValidationPrompt()`.
3. Реализовать SupplyChainValidator (цепочки, PRODUCES/CONSUMES по facility, gaps).
4. Реализовать GovernanceValidator (OWNS, ownership %, board, key persons).
5. Подключить StrategicValidator: обернуть существующий `ValidateStrategicOverlay` в интерфейс Validator.
6. Реализовать CriticPipeline: по domain mode выбирать список валидаторов, вызывать по очереди, мержить ValidationResult (conflicts, missingInfo, confidence, isValid).
7. В CriticAgent: при наличии domain mode вызывать CriticPipeline; при отсутствии — текущий монолитный промпт (fallback).
8. Unit-тесты: каждый валидатор отдельно; pipeline для комбинаций mode.

**Критерии приёмки:** при domainMode=supply_chain вызываются Core + SupplyChainValidator; при governance — Core + GovernanceValidator; при strategic — Core + StrategicValidator; результат совместим с текущим контрактом ValidationResult. Во всех промптах валидаторов — только канонические типы рёбер и qualifiers (п. 6.3.1).

---

### Этап 3: Composable Research

**Задачи:**

1. Ввести интерфейс `ResearchStrategy`, `ResearchStrategyInput`, `ResearchStrategyOutput` (п. 5.1).
2. Реализовать SupplyChainResearch (отдельный промпт, только supply chain вопросы и allowlist рёбер).
3. Реализовать GovernanceResearch (отдельный промпт, только ownership/board/key persons).
4. Реализовать ResearchPipeline: по domain mode выбирать стратегию(и), вызывать, мержить вывод (fragment updates, relations, supply chains).
5. В Phase 1 (domain_expansion): при наличии domain mode вызывать ResearchPipeline вместо единого EnrichFragmentWithGrounding; при general — текущий Research agent.
6. При записи результатов domain_expansion применять allowlist: отбрасывать или не сохранять рёбра с типом не из allowlist текущего mode.
7. Unit-тесты: стратегии по отдельности; pipeline для разных mode.

**Критерии приёмки:** при mode=supply_chain обогащение идёт только по цепочкам и продуктам; при mode=governance — только по ownership/board; в граф попадают только разрешённые типы рёбер. В промптах стратегий — только канонические типы рёбер и qualifiers (п. 6.3.1).

---

### Этап 4: Фазы в ScoutJob

**Задачи:**

1. Явно выделить в engine фазы: Phase 0 (structural), Phase 1 (domain_expansion), Phase 2 (domain_validation), Phase 3 (decomposition). Текущий поток разбить на эти фазы без изменения порядка вызовов (сначала рефакторинг структуры).
2. Phase 0: выделить/ввести «structural» extraction (минимальный фрагмент + LOCATED_IN, OPERATES_IN, при необходимости базовые PRODUCES/CONSUMES). Либо оставить текущий ScoutAgent.Extract с узким контрактом «только structural» при вызове из Phase 0.
3. Phase 1: вызывать ResearchPipeline (или текущий Research при general); применять allowlist при сохранении рёбер.
4. Phase 2: вызывать CriticPipeline (или текущий Critic при general).
5. Phase 3: оставить текущую логику shouldRecurse и schedule; передавать в дочерние job’ы domain mode (или composite).
6. Реализовать правило: при определённом intent не запускать general expansion/validation — только специализированные по mode. General только при mode=general.

**Критерии приёмки:** в логах/трейсе видно прохождение фаз; дочерние job’ы получают domain mode; при intent=supply_chain не вызывается общий «general» enrichment/validation.

---

### Этап 5: Упрощение модели рёбер

**Задачи:**

1. Консолидация (п. 6.1): по шагам 1–6 ввести новые типы и qualifiers (включая PARTICIPATES_IN + marketRole + market_share вместо DOMINATES), добавить нормализацию старых → новые в edge_normalizer и canonicalization.
2. Обновить **validation policy** и **edge direction policies** под новые типы и qualifiers; удалить или заменить правила для deprecated типов. В логике (allowlists, shouldRecurse, фильтры) использовать **только** канонические типы (п. 6.3.1).
3. Обновить **policy check и enforce**: в `validation_policy.go`, `policy_checker.go`, edge direction policies и enforcement-логике — правила только для канонических типов и qualifiers (напр. PARTICIPATES_IN + marketRole/market_share, ASSOCIATED_WITH + subtype); гарантировать, что ребро попадает в policy **после** канонизации (на вход policy только канонический тип); deprecated типы из правил убрать (п. 6.3.1).
4. Обновить промпты Scout/Research/Critic: в allowlist и примерах — **только канонические типы и qualifiers**; устаревшие типы в промптах не упоминать (п. 6.3.1).
5. Справочник semantic groups подключить к allowlist и к UI/API (фильтрация по группе).
6. Документировать изменения в data-model (новая таблица DependencyType с группами и deprecation).
7. Миграция (опционально): скрипт перевода существующих рёбер со старых типов на новые + qualifier; либо только read-time маппинг.
8. **Упрощение канонизации и policy (п. 6.5):** один источник правил переписывания в models; в policy только два особых кейса и вызов CanonicalizeEdgeWithPaths; ResolveEdgeDirectionAutoFix убрать или сделать обёрткой; EdgeDirectionPolicies — только канонические типы; в CanonicalizeEdge для domain–domain allowlist только FOCUSES_ON и DOMAIN_RELATION; SymmetricEdgeTypes обновить под канонические типы; ввести хелпер для типов «участник» (entity) и использовать в правилах. Проверить чеклист из п. 6.5.

**Критерии приёмки:** новые рёбра не создаются deprecated типами; старые данные либо мигрированы, либо отображаются в новом виде при чтении; количество правил канонизации уменьшено; дублирование правил между policy и models устранено (п. 6.5). Policy check и enforce используют только канонические типы и qualifiers; вход в policy — после канонизации (п. 6.3.1).

---

### Этап 6: Тестирование и откат

**Задачи:**

1. E2E: job с intent supply_chain → только supply chain фазы и рёбра; job с intent equity → только governance.
2. Регрессия: job с general / без intent ведёт себя как раньше (monolithic Critic/Research).
3. Документация: обновить roadmap, workflows (scout-workflow), при необходимости API (если появится параметр domainMode в запросах).
4. Флаги/feature toggle: возможность включить composable pipeline по конфигу, чтобы откатить на старый путь без деплоя.

**Критерии приёмки:** регрессий нет; откат через toggle возможен; документация актуальна.

---

## 8. Зависимости и порядок

- Этап 1 — перед этапами 2, 3, 4, 5.
- Этап 2 (Critic) и Этап 3 (Research) можно вести параллельно после Этапа 1.
- Этап 4 (фазы) зависит от 2 и 3 (нуны pipeline’ы).
- Этап 5 (рёбра) можно начинать после Этапа 1; стыковка с 2–4 — обновление allowlists и промптов под новые типы.
- Этап 6 — после 4 и 5.

---

## 9. Риски и митигация

| Риск | Митигация |
|-----|-----------|
| Ухудшение качества при разбиении Critic на валидаторы | Сохранить fallback на монолитный промпт; A/B сравнение по метрикам (missingInfo coverage, conflict rate). |
| Дублирование контента в нескольких стратегиях Research | Чёткие allowlist и контракт «стратегия X возвращает только рёбра типа Y»; при merge не перезаписывать существующие рёбра. |
| Регрессия при консолидации типов рёбер | Read-time маппинг старых типов; миграция данных в отдельном окне; тесты на старых и новых типах. |
| Intent не всегда доступен | Оставить определение mode по specialties; при отсутствии и intent, и specialties — general. |

---

## 10. Ссылки

- [Data Model](../architecture/data-model.md)
- [Supply Chain & Influence Model](../architecture/supply-chain-influence-model.md)
- [Roadmap](roadmap.md)
- [Domain Answer Layer Design](DOMAIN_ANSWER_LAYER_DESIGN.md)
- Текущий код: `backend/internal/horizon/scout/engine.go`, `engine_ext.go`, `domain_contract_runner.go`, `pipeline/contract.go`, `pipeline/passes.go`; `backend/internal/horizon/domain/engine.go`, `steering_profiles.go`; `agents/critic_agent.go`, `research_agent.go`, `scout_agent.go`, `intent_resolver_agent.go`; `models/dependency_edge.go`, `edge_canonicalization.go`; `backend/internal/taxonomy/specialties.go`
