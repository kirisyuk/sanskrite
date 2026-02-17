# Structural Organization & Supply Chain Model

## Обзор

Документ фиксирует обновлённую модель для **структурированных организаций** и их **upstream/downstream** в Horizon.

Ключевая идея: для крупных компаний полезнее сначала строить **макро-граф из орг-юнитов**, а затем делать адресный deep dive в выбранные пары `unit ↔ unit`, чем сразу пытаться строить полный продуктово-логистический граф на верхнем уровне.

---

## 1. Бизнес-мотивация

### 1.1 Что ломается в плоском org-графе

Связь уровня `Huawei -> Kazakhtelecom` без внутренней декомпозиции:
- смешивает разные бизнес-линии (consumer vs enterprise),
- мешает рисковому анализу (сбой в одном контуре выглядит как сбой всей компании),
- даёт слабую explainability для пользователя.

### 1.2 Что даёт unit-модель

1. Сохраняем макро-правду: `Huawei supplies to Kazakhtelecom`.
2. Добавляем операционную точность: `Huawei Consumer -> K-Cell Consumer`, `Huawei Enterprise -> KT Enterprise`.
3. Получаем управляемую глубину: deep dive запускается только там, где это нужно.

---

## 2. Базовая абстракция (Semantic Zoom)

### 2.1 Уровни детализации

| Уровень | Суть | Назначение |
|--------|------|------------|
| **L0 Macro** | ORG ↔ ORG | Быстрый обзор экосистемы |
| **L1 Unit** | UNIT ↔ UNIT (внутри ORG) | Операционная структура спроса/предложения |
| **L2 Deep Dive** | Legal entities, products, facilities, routes, contracts | Форензика/точный разбор цепочек |

### 2.2 Политика по умолчанию

- По умолчанию используем **Broad + Semi-depth** (L0 + L1).
- L2 запускается по триггеру (клик, высокий risk, низкая уверенность, приоритетный вопрос).

---

## 3. Что такое Org Unit

### 3.1 Определение

**Org Unit** — структурная часть организации с собственными входами/выходами (division, department, business line, JV-контур и т.д.).

### 3.2 Важный принцип

`Unit != Legal Entity`.

Unit может:
- включать несколько юрлиц,
- совпадать с департаментом (без отдельного юрлица),
- пересекаться по людям/процессам с другим unit.

Это сознательное упрощение v1: избегаем ложной строгости 1:1 и сохраняем гибкость.

---

## 4. Какие юниты показываем на первом экране

Юнитов не должно быть много: они должны помещаться рядом с org-нодой и оставаться читаемыми.

### 4.1 Core-набор v1 (до 6 на визуале)

1. `consumer`
2. `enterprise`
3. `core_ops`
4. `it`
5. `logistics`
6. `corporate`

`rnd` — опциональный, показывать только при наличии значимых связей.

### 4.2 Доменные алиасы (нормализация в core-коды)

- Telecom: `network_ops -> core_ops`
- Mining: `extraction -> core_ops`, `processing -> core_ops`
- Retail: `merchandising -> core_ops`
- IT vendor: `product -> core_ops`, `cloud_ops -> core_ops`, `support -> enterprise|consumer` (по контексту)

### 4.3 Почему так

Функционально-универсально-продуктовая нарезка на старте:
- снижает дрейф в LLM-классификации,
- упрощает визуал,
- жертвует глубиной осознанно, но даёт стабильный каркас.

---

## 5. Модель данных для Unit (концепт v1)

Минимальный профиль юнита:

```json
{
  "unit_id": "kt:consumer",
  "parent_org_ref": "/org/kz/kazakhtelecom",
  "unit_code": "consumer",
  "label": "K-Cell / Consumer",
  "geo": ["kz", "almaty"],
  "legal_entity_refs": ["/org/kz/kcell-jsc"],
  "confidence": 0.82
}
```

Поля:
- `unit_id` — стабильный идентификатор в рамках `parent_org_ref`.
- `parent_org_ref` — обязательная ссылка на макро-организацию.
- `unit_code` — только из контролируемого словаря.
- `legal_entity_refs[]` — опциональные many-to-many ссылки на юрлица.
- `geo` — опциональный qualifier.

---

## 6. Как связываем SupplyLink с входами/выходами юнитов

### 6.1 Выбранная схема v1

Используем **org-level link + unit qualifiers**.

- `supplier_org_ref` / `customer_org_ref` — макро-связь всегда сохраняется.
- `supplier_unit_ref` / `customer_unit_ref` — уточняют конкретные юниты.

Это соответствует принципу: не теряем верхнеуровневую правду и не ломаем текущий org-контракт.

### 6.2 Почему не path `/org/.../unit/...` в v1

На текущем стеке canonical path для ORG ограничен формами:
- `/org/{slug}` (legacy),
- `/org/{geo}/{slug}`.

Схема `/org/.../unit/...` требует изменения path policy и рискует ломать текущую валидацию/канонизацию.

### 6.3 Неполные данные

Если unit не определён:
- link сохраняется на уровне org,
- unit кладётся в `unclassified`.

Нельзя терять связь только из-за отсутствия unit-классификации.

---

## 7. Upstream/Downstream множества юнитов

Определения:
- `unit.upstream` = все links, где `customer_unit_ref = unit_id`
- `unit.downstream` = все links, где `supplier_unit_ref = unit_id`

Агрегаты:
- `org.upstream/downstream` = rollup по всем unit данного `parent_org_ref` + org-only links.

---

## 8. Визуальная модель

### 8.1 Режимы отображения

1. **Collapsed ORG**: один узел компании, агрегированные входы/выходы.
2. **Expanded Units**: рядом с org показываются core-unit ноды.
3. **Unit Graph**: рёбра между юнитами разных компаний.

### 8.2 Пример (телеком)

- `Huawei Consumer -> K-Cell Consumer` (телефоны)
- `Huawei Consumer -> other telco consumer units`
- `Huawei Enterprise -> Kazakhtelecom Enterprise/Network`
- `Cisco Enterprise -> Kazakhtelecom Enterprise/Network`

Пользователь видит сразу:
- кто у кого покупает в consumer-контуре,
- кто конкурирует/дублируется в enterprise-контуре,
- где есть концентрационный риск.

---

## 9. Scout / Research / Critic: новая фаза обработки

### 9.1 Общий порядок

1. **Macro Anchor**: заякорили макросущность (`/org/...`).
2. **Unit Discovery**: выделили набор unit для этой org.
3. **Unit-to-Unit Mapping**: связали unit с контрагентами (без глубоких product chains).
4. **Optional Deep Dive**: адресно углубились в выбранный `unit ↔ unit`.

### 9.2 Что меняется в Scout

Scout после macro anchor должен сначала делать **структурный проход**:
- найти unit-каркас,
- присвоить `unit_code`,
- извлечь базовые upstream/downstream наборы по unit.

Запрещено на этом этапе:
- уходить в детальный SKU-level/contract-level граф по всем найденным связям.

### 9.3 Что меняется в Research

Research по умолчанию работает на уровне:
- `unit -> counterpart units`,
- широкие product/service buckets (опционально),
- confidence + provenance.

Цель — целостный **macro-unit graph**, а не глубокая реконструкция каждой поставки.

### 9.4 Что меняется в Critic

Critic проверяет в первую очередь:
1. Полноту unit-каркаса (есть ли ключевые юниты).
2. Отсутствие семантического смешения (`consumer` vs `enterprise`).
3. Покрытие входов/выходов по unit (или явный `unclassified`).
4. Качество unit qualifiers на links.

Critic **не должен** требовать глубокий product/legal уровень в каждом запуске.

---

## 10. Контракты типов (Type Contracts, v1)

Цель контрактов типов: фиксировать стабильные поля для L0/L1 без обязательного погружения в L2.

### 10.1 `UnitCode` (контролируемый enum)

```text
consumer | enterprise | core_ops | it | logistics | corporate | rnd | unclassified
```

Правила:
- `unclassified` допустим только как fallback.
- Доменные алиасы нормализуются в core-коды до записи.

### 10.2 `OrgUnitProfile`

```json
{
  "unit_id": "string",
  "parent_org_ref": "/org/... or /gov/...",
  "unit_code": "consumer|enterprise|core_ops|it|logistics|corporate|rnd|unclassified",
  "label": "string",
  "status": "ACTIVE|DORMANT|MERGED|UNKNOWN",
  "geo": ["string"],
  "legal_entity_refs": ["/org/..."],
  "aliases": ["string"],
  "confidence": 0.0,
  "sources": ["string"]
}
```

Обязательные поля:
- `unit_id`
- `parent_org_ref`
- `unit_code`
- `label`

Опциональные:
- `status`, `geo`, `legal_entity_refs`, `aliases`, `confidence`, `sources`

Ограничения:
- `unit_id` уникален в рамках `parent_org_ref`.
- `legal_entity_refs` many-to-many, без требования 1:1.

### 10.3 `UnitSupplyLinkQualifier`

```json
{
  "supplier_org_ref": "/org/...",
  "customer_org_ref": "/org/...",
  "supplier_unit_ref": "unit_id or null",
  "customer_unit_ref": "unit_id or null",
  "broad_product_buckets": ["string"],
  "channel": "direct|distributor|retail|unknown",
  "geo_scope": ["string"],
  "confidence": 0.0,
  "sources": ["string"]
}
```

Принцип:
- org refs обязательны всегда;
- unit refs опциональны;
- отсутствие unit refs не блокирует запись связи.

### 10.4 `UnitGraphEdge` (визуальный L1-артефакт)

```json
{
  "edge_id": "string",
  "supplier_org_ref": "/org/...",
  "customer_org_ref": "/org/...",
  "supplier_unit_ref": "unit_id or unclassified",
  "customer_unit_ref": "unit_id or unclassified",
  "direction": "upstream|downstream",
  "weight": 0.0,
  "confidence": 0.0
}
```

Назначение:
- быстрый рендер unit-графа,
- вход для выбора edge на deep dive.

### 10.5 `UnitDiveRequest` (контракт запуска L2)

```json
{
  "request_id": "string",
  "mode": "MANUAL|AUTO_POLICY",
  "supplier_org_ref": "/org/...",
  "customer_org_ref": "/org/...",
  "supplier_unit_ref": "unit_id",
  "customer_unit_ref": "unit_id",
  "depth": "semi|deep",
  "reason": "user_click|high_criticality|low_confidence|analyst_request",
  "budget": {
    "max_hops_upstream": 2,
    "max_hops_downstream": 2,
    "max_new_nodes": 50
  }
}
```

Ограничения:
- `supplier_unit_ref` и `customer_unit_ref` обязательны.
- Без unit-пары L2 не запускается.

### 10.6 `UnitDiveResult` (контракт результата L2)

```json
{
  "request_id": "string",
  "status": "COMPLETED|PARTIAL|FAILED",
  "resolved_legal_entities": ["/org/..."],
  "upstream_chain": ["node refs..."],
  "downstream_chain": ["node refs..."],
  "new_supply_links": ["ids..."],
  "confidence": 0.0,
  "notes": "string"
}
```

---

## 11. Контракты пайплайна (Phase Contracts)

Ключевой принцип: `Scout/Research/Critic` упрощаются в первых фазах именно потому, что pipeline явно фазирован.

### 11.1 Фазы, входы, выходы, режим запуска

| Phase | Название | Вход | Выход | Запуск |
|------|----------|------|-------|--------|
| `P0` | Macro Anchor | target org | anchor org fragment | auto |
| `P1` | Unit Discovery | anchor org | `OrgUnitProfile[]` | auto |
| `P2` | Unit Mapping | org + units | `UnitGraphEdge[]` | auto |
| `P3` | Critic Lite | units + edges | gaps, quality flags, dive candidates | auto |
| `P4` | Dive Planning | critic flags + policy | `UnitDiveRequest[]` | conditional |
| `P5` | Unit Dive Execution | selected `UnitDiveRequest` | `UnitDiveResult` | manual by default |

### 11.2 Неавтоматический deep dive (жёсткое правило)

- `P5` **не является обязательным auto-шагом**.
- По умолчанию pipeline завершается на `P3`.
- `P5` запускается:
  - вручную (клик на edge / явный запрос),
  - либо авто-политикой только при строгих триггерах и бюджете.

### 11.3 Контракт переходов между фазами

`P0 -> P1`:
- если anchor не найден, pipeline завершает run с `insufficient_anchor`.

`P1 -> P2`:
- если units пустые, создаётся один synthetic unit `unclassified`, pipeline продолжается.

`P2 -> P3`:
- critic проверяет coverage и конфликтность unit-классификации.

`P3 -> P4`:
- только если есть `dive_candidate=true`.

`P4 -> P5`:
- только при `mode=MANUAL` подтверждение пользователя,
- при `mode=AUTO_POLICY` — только если удовлетворены thresholds и budget.

### 11.4 Роли агентов по фазам

Scout:
- отвечает за `P1` (структура units),
- не строит глубокие продуктовые цепочки в `P1`.

Research:
- отвечает за `P2` (unit-to-unit mapping),
- в `P2` работает на broad buckets, не на SKU/contract деталях.

Critic:
- отвечает за `P3` (quality gates),
- не требует L2-детализации по умолчанию.

### 11.5 Guardrails для упрощения первых фаз

Для `P1-P3` запрещено:
- массовое раскрытие legal entity graphs,
- глобальный multi-hop product traversal,
- детальная маршрутизация каждой поставки.

Для `P1-P3` обязательно:
- сохранять org-level truth,
- использовать `unclassified` вместо потери связи,
- фиксировать confidence/sources на unit/edge уровне.

---

## 12. Prompt-контракты по фазам

### 12.1 Unit Discovery Prompt (`P1`, Scout)

Обязательные инструкции:
- Identify organizational units for the target organization.
- Classify each unit using allowed unit codes only.
- Return only `OrgUnitProfile`-compatible fields.
- Do not expand into detailed product/facility/contract chains.

### 12.2 Unit Mapping Prompt (`P2`, Research)

Обязательные инструкции:
- Build links between supplier and customer units.
- Keep org-level truth and add unit qualifiers where possible.
- Use broad product buckets only.
- Return unresolved links as `unclassified` rather than dropping them.

### 12.3 Critic Lite Prompt (`P3`, Critic)

Обязательные инструкции:
- Validate unit coverage for key business contours.
- Detect classification conflicts and missing unit qualifiers.
- Propose dive candidates, but do not require automatic deep dive.

### 12.4 Unit Dive Prompt (`P5`, Research on-demand)

Запускается только по selected edge (`UnitDiveRequest`):
- фиксированы `supplier_unit_ref` и `customer_unit_ref`,
- разрешено разворачивать цепочки до/после unit в рамках budget.

---

## 13. Политика глубины (Depth Policy)

### 13.1 По умолчанию

- `broad`: ORG + UNIT graph
- `semi-depth`: 1 уровень уточнения для ключевых unit links

### 13.2 Когда разрешён multi-level dive

Только при явном триггере:
- клик пользователя на unit-link,
- высокий критикалити/объём/концентрация,
- низкая уверенность и необходимость верификации.

### 13.3 Ограничение v1

Одновременно не разворачиваем весь граф во многоуровневый deep mode.
Deep dive — точечный, edge-centric.

---

## 14. Агрегаты и хранение

### 14.1 Принцип

Агрегаты храним отдельно (materialized), не считаем всё on-the-fly.

### 14.2 Уровни агрегирования

- ORG-level: `SAP -> Kazakhtelecom`
- UNIT-level: `SAP Enterprise -> KT Corporate/IT unit`
- EDGE-level deep context: только для выбранных unit-связей

---

## 15. Связь с текущим кодом (as-is ограничения)

1. Текущий `SupplyLink` хранит org/fac refs и facility qualifiers; unit refs пока нет.
2. Path policy для ORG не поддерживает `/org/.../unit/...` как стабильный контракт.
3. В pathbuilder остаётся технический риск поиска links с ограничением выборки.
4. Prompt-логика уже богата supply-chain семантикой, но ещё не разделена строго на `unit discovery` и `unit mapping` фазы.

Вывод: для v1 безопаснее внедрять unit как **квалификаторы/отдельный слой**, а не ломать canonical path ORG.

---

## 16. План внедрения (по шагам)

1. Зафиксировать контролируемый словарь `unit_code` и доменные алиасы.
2. Добавить слой хранения unit-профилей (`parent_org_ref`, `unit_id`, `unit_code`, `legal_entity_refs[]`).
3. Добавить `supplier_unit_ref/customer_unit_ref` в link-модель.
4. Внедрить фазовые контракты `P0..P5` в pipeline state.
5. Обновить critic-правила под coverage unit-структуры и dive candidacy.
6. Добавить визуальные режимы `collapsed org` / `expanded units`.

---

## 17. Открытые вопросы

1. `OrgUnit` как новый `FragmentType` или отдельная сущность вне fragment namespace?
2. Где хранить unit-слой: в `HorizonFragment`-совместимом storage или в отдельной коллекции?
3. Как версионировать `unit_code` при реорганизациях (merge/split/rename)?
4. Какой минимум confidence/evidence нужен, чтобы link считался unit-resolved?
5. Нужен ли отдельный edge type для `UNIT_OF` (unit -> org) или достаточно reference-поля?
6. Какие пороги включают `AUTO_POLICY` для `P5` и в каких доменах он вообще разрешён?

---

## 18. Horizon Modeler и Policy Impact (единый контракт)

### 18.1 Принцип

Модель не делится на два документа и не делится на две базы:

1. Базовый граф Horizon остаётся единым (`HorizonFragment` + `DependencyEdge`).
2. Modeler живёт в `/horizon` как режим работы с тем же графом и storage.
3. `policy-impact` — отдельный `contractType` и отдельный `domainMode` (`policy_impact`).

### 18.2 Domain contract -> Domain mode

Нормативно:

1. Поведение pipeline определяется типом доменного контракта.
2. Исполнение маршрутизируется через `domainMode`.
3. Один `contractType` обычно даёт один `domainMode`, но несколько контрактов могут сводиться к одному mode.
4. Для policy impact: `policy-impact -> policy_impact`.

### 18.3 Policy-impact pipeline (сверху-вглубь)

Используется тот же фазовый каркас `P0..P5`, но с другой семантикой фаз:

| Phase | Название | Вход | Выход | Запуск |
|------|----------|------|-------|--------|
| `P0` | Policy Anchor | target policy/program | anchor fragment | auto |
| `P1` | Source/Instrument Discovery | anchor | instruments, authority chain, `OrgUnitProfile[]` для exposed orgs | auto |
| `P2` | Channel/Segment Mapping | instruments + exposed segments | exposure edges с quantitative qualifiers | auto |
| `P3` | Critic Lite (Model Readiness) | edges + qualifiers | coverage gaps, qualifier gaps, observability gaps | auto |
| `P4` | Scenario Planning | critic flags + adapter encode preview | `ScenarioInput`, signal descriptors preview | conditional |
| `P5` | Targeted Deep Dive | selected exposure edge | detailed pass-through model for specific sector/org | manual by default |

По умолчанию: `P0..P3`; `P5` только по явному триггеру.

#### 18.3.1 Связь policy-impact pipeline с unit-моделью

Policy exposure раскладывается на unit-уровень, когда exposed entity — крупная организация:
- Повышение НДС влияет на `Kazakhtelecom.consumer` и `Kazakhtelecom.enterprise` по-разному (consumer = B2C, pass-through к абонентам; enterprise = B2B, договорные цены).
- Research в P2 должен проставлять `target_unit_ref` и `target_unit_code` на exposure edges к org с unit-каркасом.
- Если unit-каркас для org ещё не построен, P2 ставит `unclassified` и формирует missingInfo.
- Critic в P3 проверяет: для крупных exposed org (orgSize >= "enterprise") unit qualifiers обязательны.

### 18.4 Propagation и математическая модель

Ключевое правило:

1. `propagation` остаётся signal-first и не становится «моделью всего мира».
2. Доменная математика вводится отдельной абстракцией `DomainModelAdapter`.
3. Adapter привязан к типу контракта (contract type family), fallback — на `domainMode`.

Обязательные функции adapter:

- `encode`: реальный фактор/сценарий -> proxy-сигналы на существующий 12-axis basis.
- `transfer`: доменная трансформация сигнала по узлам/рёбрам (hook в BFS-цикл).
- `decode`: proxy-сигналы -> итоговые бизнес-метрики на endpoint-узлах.

#### 18.4.1 Контракт `DomainModelAdapter` (формальный интерфейс)

```go
type DomainModelAdapter interface {
    // ContractTypeFamily returns which contract family this adapter handles.
    ContractTypeFamily() string

    // Encode converts a domain scenario into proxy signals for injection
    // onto the existing 12-axis basis. Adapter does NOT create new axes.
    Encode(ctx context.Context, scenario ScenarioInput) ([]SignalDescriptor, error)

    // TransferHook is called per-edge during BFS propagation.
    // Returns adjusted amplitude/axis or Skip=true to block propagation.
    // If nil-result, engine uses default profile damping.
    TransferHook(ctx context.Context, signal SignalDescriptor, edge DependencyEdge, node HorizonFragment) (*TransferResult, error)

    // Decode converts accumulated proxy signals on endpoint nodes
    // into domain-level business metrics. Called on-demand (not during wave).
    Decode(ctx context.Context, node HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error)
}
```

Ключевые типы:

```go
type ScenarioInput struct {
    ScenarioID   string                 // unique scenario identifier
    ContractType string                 // "policy-impact", "supply-chain-disruption", etc.
    Parameters   map[string]interface{} // domain-specific: {"vat_rate_from":12, "vat_rate_to":16}
    BaselineRef  string                 // optional: snapshot of baseline state
    TargetPaths  []string               // optional: limit analysis scope
}

type TransferResult struct {
    AdjustedAmplitude float64
    AdjustedAxis      string // empty = keep current
    Skip              bool   // true = do not propagate through this edge
}

type AccumulatedSignal struct {
    Axis      string
    Amplitude float64
    Polarity  string
    HopCount  int
}

type BusinessMetric struct {
    MetricPath string  // e.g., "/metric/policy/budget_revenue_delta"
    Value      float64
    Unit       string
    Confidence float64
}
```

#### 18.4.2 Правило проекции на 12-axis basis

Adapter **не создаёт новые axes**. Propagation engine работает с фиксированным 12-axis basis.
Доменный сценарий проецируется на существующие axes через `Encode`:

| Доменный фактор | Проекция на axes | Обоснование |
|---|---|---|
| Повышение НДС | `financial/settlement` + `physical_flow/demand` | Рост фискальной нагрузки → рост транзакционных издержек + снижение спроса |
| Ужесточение compliance | `legal/compliance` + `info/uncertainty` | Регуляторная нагрузка + неопределённость правил |
| Санкции на сектор | `legal/access` + `physical_flow/supply` | Запрет доступа + разрыв поставок |
| Субсидирование | `financial/credit` (opportunity polarity) | Улучшение финансовых условий |

Каждый adapter определяет свою таблицу проекций в `Encode`.

#### 18.4.3 Правило вычисления

1. По волне ходят лёгкие proxy-сигналы (amplitude ∈ [0,1] на стандартных axes).
2. `TransferHook` применяет доменные коэффициенты per-edge (pass-through rate, sector sensitivity).
3. Тяжёлая математика выполняется в `decode` on-demand.
4. `decode` кэшируется по ключу `scenario_id + node_id + model_version`.
5. Инвалидация кэша: при изменении графа (новые edges/fragments) или параметров сценария.

#### 18.4.4 Adapter Registry

```go
type AdapterRegistry struct {
    adapters map[string]DomainModelAdapter // contract type family -> adapter
}

func (r *AdapterRegistry) ForContractType(ct string) (DomainModelAdapter, bool)
```

`PropagationEngine` подключает registry через `.WithAdapterRegistry(reg)`.
При наличии адаптера для текущего contract type, engine вызывает `TransferHook` в BFS-цикле.
При отсутствии — используется стандартный `PropagationProfile` (текущее поведение, backward compatible).

### 18.5 Где отражаем эффекты НДС (пример policy-impact)

#### 18.5.1 Сквозной пример: НДС с 12% до 16% в Казахстане

**P0 — Policy Anchor:**
- anchor = `/gov/kz/ministry-of-finance`, contract type = `policy-impact`
- параметры: `{"vat_rate_from": 12, "vat_rate_to": 16, "effective_date": "2026-07-01", "jurisdiction": "kz"}`

**P1 — Source/Instrument Discovery (Scout):**
- instruments: `НДС (ставка базовая)`, `НДС (ставка на продовольствие, если льготная)`, `НДС (освобождения)`
- programs: `/program/kz/vat-reform-2026`
- authority chain: `/gov/kz/ministry-of-finance` ADMINISTERS `/program/kz/vat-reform-2026`

**P2 — Channel/Segment Mapping (Research):**
- IMPOSES_CONDITION: `/program/kz/vat-reform-2026` → `/market/kz/consumer/food` (qualifiers: `pass_through_estimate=0.6, tax_base_coverage=partial`)
- IMPOSES_CONDITION: `/program/kz/vat-reform-2026` → `/market/kz/consumer/retail` (qualifiers: `pass_through_estimate=0.85, tax_base_coverage=full`)
- INFLUENCES: `/program/kz/vat-reform-2026` → `/community/kz/trade/bazaar-traders` (qualifiers: `channel_type=evasion, lag_quarters=2`)
- DEPENDS_ON: `/org/kz/magnum-retail` → `/market/kz/consumer/food` (exposure)

**P3 — Critic Lite:**
- проверяет: есть ли pass_through_estimate на ключевых exposure edges? Есть ли observability hints?
- missingInfo: "What is the estimated pass-through rate for electronics retail in KZ?"
- dive candidates: `magnum-retail ↔ food-market` edge (high exposure, partial data)

**Encode (adapter):**
```
scenario: {vat_rate_from: 12, vat_rate_to: 16}
→ SignalDescriptor[]:
  - axis: "financial/settlement", amplitude: 0.33, polarity: "risk",
    seedPaths: ["/market/kz/consumer/food", "/market/kz/consumer/retail"],
    mechanism: "vat_increase"
  - axis: "physical_flow/demand", amplitude: 0.25, polarity: "risk",
    seedPaths: ["/market/kz/consumer/retail"],
    mechanism: "price_elasticity"
```

**TransferHook (adapter, per-edge):**
- edge `/market/kz/consumer/food` → `/org/kz/magnum-retail`: amplitude *= pass_through_estimate (0.6)
- edge `/market/kz/consumer/retail` → `/community/kz/trade/bazaar-traders`: amplitude *= 0.3 (low formal exposure, evasion channel)

**Decode (adapter, on-demand):**
- node `/gov/kz/ministry-of-finance`: `budget_revenue_delta_mln_kzt = +320`, `shadow_share_delta_pct = +1.2`
- node `/market/kz/consumer/food`: `consumer_price_index_delta_pct = +2.4`
- node `/org/kz/magnum-retail`: `margin_pressure_pct = -1.8`

#### 18.5.2 Нормативные слои интерпретации

Цепочка эффектов отражается в двух плоскостях:

1. В графе: кто выпускает policy, через какие каналы идёт влияние, кто экспонирован.
2. В endpoint-метриках через `decode`: бюджетные доходы, proxy ВВП, proxy теневой доли, proxy соц.настроений/трендов.

Пять слоёв интерпретации:

1. **Policy Source** (authority chain): кто принимает решение, по какой иерархии.
   - Граф: ADMINISTERS, REGULATES, GATEKEEPS_DECISION edges.
2. **Transmission Channel** (механизм передачи): как policy доходит до экономических агентов.
   - Граф: IMPOSES_CONDITION, USES_MECHANISM, CONTROLS_ACCESS edges.
   - Qualifiers: `channel_type`, `rate_from/rate_to`, `effective_date`.
3. **Exposure** (сектора/группы/гео): кто под воздействием и насколько.
   - Граф: DEPENDS_ON, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN edges к exposed nodes.
   - Qualifiers: `pass_through_estimate`, `elasticity_hint`, `tax_base_coverage`, `sector_sensitivity`.
4. **Response** (реакция и обратные волны): как агенты адаптируются.
   - Граф: INFLUENCES edges с `channel_type` = `substitution`, `evasion`, `behavioral`.
   - Qualifiers: `lag_quarters` (задержка реакции).
5. **Observability** (кто, как и с какой уверенностью измеряет):
   - Qualifiers на edges: `observability` = `direct|proxy|modeled`.
   - `measurement_lag_months`: через сколько месяцев эффект станет видимым в данных.
   - `measurement_confidence`: уверенность в возможности измерения (0.0-1.0).
   - Примеры источников: stat.gov.kz (retail sales, CPI), налоговая отчётность (VAT collection), customs data.
   - Critic проверяет: для каждого major exposure edge должен быть хотя бы один observability hint.

### 18.6 Текущее состояние (as-is)

Уже есть:

1. `policy-impact` как contract type и `policy_impact` как mode.
2. mode-specific ветки Research/Critic и allowlist.
3. Общий propagation engine (BFS, 12-axis basis, damping, transformations, explain trace).
4. Research prompt для policy-impact с quantitative qualifiers (pass_through, elasticity, observability).
5. Critic prompt с проверкой modeling readiness и observability completeness.
6. `DomainModelAdapter` + `AdapterRegistry` в runtime (`encode/transfer/decode` контракт, fallback `contractType -> domainMode`).
7. Интеграция `TransferHook` в hot-path и signal BFS (`PropagateSignalFromSeeds`) как opt-in слой.
8. `PolicyImpactAdapter` переведён в режим engine-router (`engineId`) с поддержкой нескольких реализаций.
9. Реализован первый `MacroEconomicalEngine` (`macro_economical_v1`) с явным macro metric set:
   `budget_revenue_delta_pct`, `budget_revenue_delta_bln_kzt`, `fiscal_balance_delta_pct_gdp`,
   `gdp_real_delta_pct`, `cpi_delta_pct`, `household_real_income_delta_pct`, `consumption_real_delta_pct`,
   `business_margin_delta_pct`, `investment_delta_pct`, `compliance_rate_delta_pct`, `shadow_economy_share_delta_pct`,
   `transmission_intensity_index`.
10. Read-only what-if path использует `SimulatePropagation` и умеет прокидывать `contractType/domainModes/engineId` для adapter routing.

Пока отсутствует:

1. Production-grade калибровка `MacroEconomicalEngine` на реальных data series (KZ, sector-level).
2. Добавление следующих engine-реализаций (`policy_impact.sociology.*`, др.) поверх общего propagation слоя.
3. Scenario management (baseline snapshot, diff, sensitivity analysis, versioning).
4. Кэш/инвалидация для `decode`-метрик (`scenario_id + node_id + model_version`).
5. Полный modeler UI для baseline/scenario/sensitivity.

### 18.7 Ближайший backlog

1. Добавить отдельный scenario-run endpoint (`scenario -> encode -> propagate -> decode`) без мутации graph state.
2. Добавить baseline snapshot / diff и sensitivity sweep (elasticity/pass-through ranges).
3. Формализовать policy-impact qualifiers/metrics/provenance в type contracts.
4. Расширить `PolicyImpactAdapter` калиброванными коэффициентами (по geo/sector/channel).
5. Добавить decode-cache + invalidation.
6. Покрыть интеграционными тестами end-to-end сценарий НДС (KZ 12% -> 16%) на sample graph.

---

## 19. Связанные документы

| Документ | Связь |
|----------|-------|
| [supply-chain-influence-model.md](supply-chain-influence-model.md) | SupplyLink, PathBuilder, стыковка |
| [data-model.md](data-model.md) | HorizonFragment, FragmentType |
| [ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md](../implementation/ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md) | Преобразования юрлиц |
| [industry-taxonomy.md](industry-taxonomy.md) | Доменные шаблоны |

---

*Документ обновлён по итогам обсуждения. Версия: 2026-02-17.*
