# Supply Chain Paths & Influence Links Model

## Обзор

Данный документ описывает архитектуру **Supply Chain Paths** и **Influence Links** — расширение графа Horizon для моделирования:
1. Конкретных цепочек поставок (кто поставляет что кому)
2. Политических/экономических влияний между акторами

## Терминология (важно)

Чтобы избежать путаницы, фиксируем терминологию:

- **Звено цепочки поставок (SupplyChainLink / Segment)**: атомарное, сшиваемое отношение вида
  `(Org|Fac) ──[Product]──> (Org|Fac)`.
  В коде/модели ниже это называется **SupplyLink** (tenant/global), с отдельными observation/claim по метрикам.

- **Путь цепочки поставок (SupplyChainPath)**: **собранный** (вычисляемый) путь A→…→Z,
  который представляет собой последовательность звеньев (SupplyLink) и онтологических переходов (например, `TRANSFORMS_INTO` между продуктами).

- **Materialized Artifact / Overlay**: отдельный базовый тип для хранения “слоёв” поверх world graph (materialized paths/slices),
  чтобы не смешивать каркас мира и вычисленные представления. См. [materialized-artifacts.md](materialized-artifacts.md).

## Проблема

Текущий граф Horizon имеет **два изолированных слоя**:

### Слой 1: Абстрактный продуктовый граф (глобальный)
```
lithium-ore ──TRANSFORMS_INTO──> lithium-carbonate ──TRANSFORMS_INTO──> battery-cell ──TRANSFORMS_INTO──> electric-vehicle
```
Это онтологический уровень — "из чего делается что". Строится автоматически через Wikidata.

### Слой 2: Связи org→product
```
Albemarle ──PRODUCES──> lithium-carbonate
CATL ──PRODUCES──> battery-cell
VW ──CONSUMES──> battery-cell (гипотеза)
```

### Проблема: отсутствующий мост

**Невозможно выразить:**
```
VW покупает battery-cell ИМЕННО у CATL (а не у Samsung SDI)
    └── которые сделаны из lithium-carbonate ИМЕННО от Albemarle (а не от SQM)
        └── ИМЕННО с шахты в Чили
```

Это разница между:
| Уровень | Пример | Horizon сейчас |
|---------|--------|----------------|
| **Ontology** | "Battery cells делаются из lithium carbonate" | ✅ TRANSFORMS_INTO |
| **Instance** | "VW ID.4 содержит батареи от CATL из лития Albemarle" | ❌ Нет способа выразить |

---

## Решение: Supply Chain Paths

### Атомарная единица (звено): SupplyLink

Минимальный путь: `(Org|Fac) ──[Product]──> (Org|Fac)`

```
┌────────────────────────────────────────────┐
│  SupplyLink                                │
│                                            │
│  Supplier ───[Product]───> Customer        │
│                                            │
│  + SupplierFacility? (optional)            │
│  + CustomerFacility? (optional)            │
│  + Volume, Criticality, Confidence         │
│  + ValidFrom/To                            │
│  + Provenance                              │
└────────────────────────────────────────────┘
```

### Транзитивная стыковка

Минимум для стыковки: `Product → Org → *`

Практический нюанс: граф **не идеален**. При сборке пути мы часто не имеем (или не хотим тащить в выборку) “конечный продукт”
на стороне потребителя. Например, для ответа “шахта → литий → BMW” нам не нужна конкретная модель автомобиля на конце.

Поэтому:
- **точка стыка** между звеньями — это общий узел `(Org|Fac)` (Link₁.Customer == Link₂.Supplier);
- проверка “Product(i) связан с Product(i+1)” через `TRANSFORMS_INTO` — **опциональна** и используется как:
  - фильтр (“интересуют только пути, которые согласуются с value chain”), или
  - ранжирование/penalty (“если нет подтверждения связи продуктов — ниже confidence”), или
  - строгая валидация в режимах, где это оправдано.

Два звена (SupplyLink) в “строгом режиме” стыкуются, если:
1. `Link₁.Customer == Link₂.Supplier`
2. Продукты связаны через `TRANSFORMS_INTO` в глобальном графе

```
Link₁: Albemarle ──[lithium-carbonate]──> CATL
                                           │
                                      стык: CATL
                                           │
Link₂:                                   CATL ──[battery-cell]──> VW

Глобальный граф: lithium-carbonate ──TRANSFORMS_INTO──> battery-cell

Результат: SupplyChainPath Albemarle → CATL → VW
```

### Граничные случаи

| Тип | Структура | Пример |
|-----|-----------|--------|
| **Полный** | Org → Product → Org | Albemarle → lithium → CATL |
| **Финишер** | Org → Product → ∅ | BMW → EV → (конечный потребитель) |
| **Стартер** | ∅ → Product → Org | (природа) → lithium-ore → Albemarle |

### FragmentType: SupplyLink

```go
type HorizonFragment struct {
    Type:      FragmentTypeSupplyLink,
    TenantID:  "bmw",  // НЕ global — это tenant-specific knowledge
    
    CanonicalPath: "/supply-link/{tenant}/{supplier-slug}/{product-slug}/{customer-slug}",
    // e.g. /supply-link/bmw/catl/battery-cell/bmw
    
    // Структура тройки (ссылки на глобальные фрагменты)
    SupplierRef:  *string  // /org/... или /fac/... (nil = природный ресурс)
    ProductRef:   string   // /product/... (обязателен)
    CustomerRef:  *string  // /org/... или /fac/... (nil = конечный потребитель)
    
    // Опционально: конкретные facility
    SupplierFacilityRef: *string  // /fac/catl/ningde
    CustomerFacilityRef: *string  // /fac/bmw/munich
    
    // Метрики
    State: {
        "volume_gwh_year":   {Value: 15.0, ...},
        "contract_share":    {Value: 0.4, ...},  // 40% батарей от CATL
        "criticality":       {Value: 0.9, ...},
    },
    
    ValidFrom: "2022-01-01",
    ValidTo:   "2027-12-31",
    
    Provenance: {
        Method:    "internal_erp_import",
        SourceIDs: ["contract-123", "procurement-system"],
    },
}
```

### Метрики поставок: Identity vs Observations (time windows, sources, confidence)

Важно разделить:

- **SupplyLink (identity, “звено”)**: стабильная сущность отношения `(supplier, product, customer[, facilities])`.
  - Это “у нас есть/может быть связь поставки” как объект, к которому можно привязывать UI, обсуждения, HIL и т.п.
  - **SupplyLink НЕ должен размножаться по окнам времени**, иначе мы получим миллионы “одинаковых” линков по месяцам/кварталам.

- **SupplyLinkObservation / SupplyLinkClaim (observations)**: конкретные утверждения/наблюдения про этот SupplyLink.
  - Их может быть **несколько одновременно**: разные источники, разные confidence, разные time windows, разные modality (observed/reported/predicted/hypothetical).
  - Позже отдельным алгоритмом/политикой можно делать rollup (“какая сейчас лучшая оценка”) — но хранить нужно все.

Принцип разделения ответственности:
- `ontology_facts` — **доказательная база (evidence)**: “что мы нашли/извлекли/зафиксировали” из источников.
- Метрики supply chain — **модельный слой**: нормализованные значения/ряды, которые **ссылаются** на `ontology_facts`, но **не живут** внутри них.

#### SupplyLinkMetricObservation (предлагаемая схема)

```go
// A single statement about supply-chain metrics for a SupplyLink for a time window.
// Can be tenant-scoped or global-scoped (assumption). Multiple observations can coexist.
type SupplyLinkMetricObservation struct {
    ID        string
    TenantID  string        // e.g. "bmw" or "global"
    ProjectID string        // optional: scope per project/lens (tenant data)

    SupplyLinkID   string   // reference to SupplyLink fragment id (or canonicalPath)

    // Time contract:
    // - observedAt: when we observed/ingested the data
    // - effectiveFrom/effectiveTo: which period in the world/model it refers to (e.g., March 2025; or year 2026 forecast)
    ObservedAt   time.Time
    EffectiveFrom *time.Time
    EffectiveTo   *time.Time

    // Modality: observed | reported | predicted | hypothetical
    Modality string

    // Confidence is per-observation (0..1). Multiple observations can coexist; resolution is a separate policy.
    Confidence float64

    // Metrics payload. Keys are named metrics; values use existing MetricValue contract (value+unit+confidence+observedAt+provenance).
    // IMPORTANT: units may differ across metrics (tons, kg, USD, EUR, days, %...), so unit must be per-metric.
    Metrics map[string]MetricValue

    // Evidence links back to ontology facts (proof points). This keeps evidence pipeline separate from model layer.
    EvidenceFactIDs []string // ontology_facts ids

    // Optional: freeform qualifiers for contract ids, shipment ids, incoterms, currency, pricing basis, etc.
    Qualifiers map[string]interface{}
}
```

#### Примеры claims (один SupplyLink, несколько истин)

```
SupplyLink: CATL ──[battery-cell]──> BMW

Claim A (observed, March 2025, ERP):
  effectiveFrom=2025-03-01, effectiveTo=2025-03-31, modality=observed, confidence=0.95
  metrics: volume_gwh=1.2, on_time_delivery=0.93, avg_lead_time_days=21

Claim B (reported, March 2025, supplier portal):
  effectiveFrom=2025-03-01, effectiveTo=2025-03-31, modality=reported, confidence=0.80
  metrics: volume_gwh=1.15, on_time_delivery=0.90

Claim C (predicted, 2026, planning forecast):
  effectiveFrom=2026-01-01, effectiveTo=2026-12-31, modality=predicted, confidence=0.60
  metrics: volume_gwh=18.0, price_index=1.08
  evidenceFactIds: [fact_9012, fact_9013]
```

#### Rollup (не хранить как “истину”, только вычислять/кешировать)

SupplyLink может иметь (опционально) **агрегированное состояние** для UX/поиска, но оно производное:
- `HorizonFragment.State` на SupplyLink = “лучшее текущее предположение” (по выбранной policy: max-confidence, trust-weighted, recency-weighted, keep-both-with-caveat, etc.)
- сырьё для rollup = множество `SupplyLinkMetricObservation`

### Связь глобального и tenant графа

```
┌─────────────────────────────────────────────────────────────┐
│  GLOBAL GRAPH (tenantId: "global")                          │
│                                                             │
│  /org/catl ──PRODUCES──> /product/.../battery-cell          │
│  /org/albemarle ──PRODUCES──> /product/.../lithium-carbonate│
│  /product/.../lithium-carbonate ──TRANSFORMS_INTO──>        │
│                        /product/.../battery-cell            │
│  (qualifiers.at_facility: /fac/catl/ningde — where transformation occurs) │
│  (qualifiers.transport_via: [/fac/port/antofagasta, /fac/port/shanghai] — transport route) │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ references
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  TENANT GRAPH (tenantId: "bmw")                             │
│                                                             │
│  SupplyLink {                                               │
│    supplier: /org/catl,                                     │
│    product: /product/.../battery-cell,                      │
│    customer: /org/bmw,                                      │
│    volume: 15 GWh/year, share: 40%                          │
│  }                                                          │
│                                                             │
│  SupplyLink {                                               │
│    supplier: /org/samsung-sdi,                              │
│    product: /product/.../battery-cell,                      │
│    customer: /org/bmw,                                      │
│    volume: 10 GWh/year, share: 27%                          │
│  }                                                          │
└─────────────────────────────────────────────────────────────┘
```

### Workflow создания SupplyLink

**Источники SupplyLink:**
1. **Scout/Research supply chains** (автоматически): при парсинге цепочек из LLM (supplyChains) парсер извлекает сегменты actor→product→actor и материализует SupplyLink фрагменты. `provenance.method: "scout_supply_chain"`. PathBuilder использует их для обхода upstream.
2. **LLM + HIL** (будущее): Scout находит гипотезу "BMW likely sources batteries from CATL" → draft SupplyLink → HIL подтверждает → confirmed.

---

## Решение: Influence Links

### Аналогия с Supply Chain

| Аспект | Supply Chain | Influence/Political |
|--------|--------------|---------------------|
| **Атом** | Supplier → Product → Customer | Actor → Channel → Actor |
| **"Продукт"** | Материал/компонент | Механизм влияния |
| **Направление** | Однонаправленный поток | Может быть взаимным |
| **Петли** | Аномалия | Норма (взаимозависимость) |

### Роль определяется связью, не объектом

Один и тот же фрагмент (например, газ) может выступать в разных ролях:

| Контекст | Роль газа | Тип связи |
|----------|-----------|-----------|
| Supply Chain | Товар | SupplyLink: Gazprom → gas → BASF |
| Political | Рычаг влияния | InfluenceLink: Russia → gas → Germany |

```
/product/raw/energy/extraction/natural-gas
          │
          ├── SupplyLink (коммерция)
          │   Gazprom ──[natural-gas]──> BASF
          │
          └── InfluenceLink (политика)
              Russia ──[natural-gas]──> Germany
              (type: LEVERAGES_VIA, direction: negative)
```

### Via может быть любым фрагментом

| Via | Пример | Механизм |
|-----|--------|----------|
| Product | gas, chips, wheat | Товарная зависимость |
| Org | SWIFT, IMF | Институциональный рычаг |
| Facility | Strait of Hormuz, Suez | Инфраструктурный контроль |
| Financial | USD reserves, debt | Финансовый рычаг |

### DependencyEdge с Via

Используем существующий `DependencyEdge` с qualifier `via`:

```go
DependencyEdge {
    Source:   "/loc/ru",           // Россия
    Target:   "/loc/de",           // Германия  
    Type:     INFLUENCES,          // или LEVERAGES
    TenantID: "global",            // политические факты могут быть глобальными
    
    Qualifiers: {
        "via":           "/product/.../natural-gas",  // инструмент
        "mechanism":     "energy_dependency",
        "direction":     "negative",                  // давление
        "reversible":    true,                        // €€€ текут обратно
        "conditionality": ["winter", "low_storage"],
    },
}
```

### Петли и взаимозависимость

Петли — это взаимозависимость, моделируется как **две встречные связи**:

```
Link₁: Russia ──[gas]──> Germany (strength: 0.4, direction: negative)
Link₂: Germany ──[€€€]──> Russia (strength: 0.3, direction: positive)

Net dependency = f(Link₁, Link₂) — вычисляется при запросе, не хранится
```

### Примеры Influence Links

```
USA ──[SWIFT]──> Iran (type: SANCTIONS_VIA)
      via: /org/swift

China ──[rare-earths]──> USA (type: LEVERAGES_VIA)
        via: /product/.../rare-earth-elements

Turkey ──[Bosphorus]──> Russia (type: CONTROLS_ACCESS)
         via: /fac/bosphorus-strait

China ──[Belt&Road debt]──> Kazakhstan (type: INFLUENCES)
        via: /program/belt-and-road
        qualifiers: {mechanism: "debt_trap"}
```

---

## Построение путей

### Multi-tenancy: global + tenant звенья, приоритеты и “склейка без разрывов”

Звено (SupplyLink) и его claims/метрики могут быть:
- **tenant**: “по нашим данным” (внутренние источники, HIL), более высокий приоритет
- **global**: assumption / общая модель мира (низкий/средний приоритет), используется как fallback

**Правило приоритета (segment-level precedence)**:
- При сборке пути на каждом шаге сначала выбираем tenant-звенья и tenant-claims.
- Если для продолжения пути **нет подходящих tenant-звеньев** (или они не попадают в фильтры по времени/modality), используем global-звенья/global-claims, чтобы не получить разрыв.

Итог: `SupplyChainPath` почти всегда будет **гибридом** tenant+global, но на каждом звене tenant “перебивает” global, если есть данные.

### Запрос: "От каких поставщиков лития зависит BMW?"

```
1. Найти SupplyLinks где customer = BMW
   → [CATL→battery-cell→BMW, Samsung-SDI→battery-cell→BMW]

2. Для каждого supplier (CATL, Samsung-SDI):
   - Найти SupplyLinks где customer = supplier
   - Или найти edges с product qualifier в глобальном графе
   
3. Рекурсивно до сырья или max_depth

4. Результат:
   BMW
   ├── CATL (battery-cell, 15 GWh, 40%)
   │   ├── Albemarle (lithium-carbonate, inferred)
   │   └── Ganfeng (lithium-carbonate, inferred)
   └── Samsung-SDI (battery-cell, 10 GWh, 27%)
       └── SQM (lithium-carbonate, inferred)
```

### Confidence Propagation

```
Segment 1: Albemarle → lithium → CATL (confidence: 0.9, disclosed)
Segment 2: CATL → battery → BMW (confidence: 0.85, verified)

Inferred path: Albemarle → ... → BMW 
  confidence: 0.9 * 0.85 * market_share_factor = ~0.5
```

---

## Изменения в модели данных

### Новый FragmentType

```go
const (
    FragmentTypeSupplyLink FragmentType = "SUPPLY_LINK"
)
```

### Новые поля в HorizonFragment

```go
type HorizonFragment struct {
    // ... existing fields ...
    
    // Supply Link specific (when Type == FragmentTypeSupplyLink)
    SupplierRef         *string `json:"supplierRef,omitempty" firestore:"supplierRef,omitempty"`
    CustomerRef         *string `json:"customerRef,omitempty" firestore:"customerRef,omitempty"`
    ProductRef          *string `json:"productRef,omitempty" firestore:"productRef,omitempty"`
    SupplierFacilityRef *string `json:"supplierFacilityRef,omitempty" firestore:"supplierFacilityRef,omitempty"`
    CustomerFacilityRef *string `json:"customerFacilityRef,omitempty" firestore:"customerFacilityRef,omitempty"`
}
```

### Расширение DependencyEdge Qualifiers

Стандартизированные qualifiers для influence:

```go
// Standard qualifier keys for influence edges
const (
    QualifierVia           = "via"           // /product/..., /org/..., /fac/...
    QualifierMechanism     = "mechanism"     // energy_dependency, debt_trap, etc.
    QualifierDirection     = "direction"     // positive, negative, neutral
    QualifierReversible    = "reversible"    // bool
    QualifierConditionality = "conditionality" // []string triggers
)
```

---

## План реализации

### Milestone 1: SupplyLink (High Priority)
- [x] Добавить `FragmentTypeSupplyLink`
- [x] Добавить поля `SupplierRef`, `CustomerRef`, `ProductRef` в HorizonFragment
- [x] Валидация: SupplyLink требует `tenantId` (может быть tenant или "global" как assumption-layer)
- [x] Валидация: ProductRef обязателен
- [ ] API endpoint для создания SupplyLink
- [ ] Интеграция с Scout для автоматического обнаружения

### Milestone 2: Path Building
- [x] Функция `BuildUpstreamPath(customer, opts)` с confidence propagation
- [x] `BuildUpstreamPaths` — возврат топ-K путей (MaxPaths, по умолчанию 10) вместо одного
- [x] `RequireProductTransformation: true` по умолчанию — проверка TRANSFORMS_INTO между продуктами
- [x] Tenant-first, global-fallback precedence на уровне сегмента
- [x] Интеграция с MaterializedArtifact для сохранения путей
- [ ] Кэширование построенных путей
- [ ] API endpoint для получения upstream/downstream paths

### Milestone 3: Influence Links
- [x] Стандартизация qualifiers для INFLUENCES edges (`via`, `mechanism`, `direction`, `reversible`, `conditionality`, `strength`)
- [x] Поддержка influence qualifiers в ValidationPolicy
- [x] Helper functions: ExtractInfluenceQualifiers, SetInfluenceQualifiers, ValidateInfluenceQualifiers
- [ ] API для запроса influence paths
- [ ] Визуализация в UI

### Milestone 4: HIL Integration
- [ ] Draft SupplyLink workflow
- [ ] UI для подтверждения/корректировки
- [ ] Confidence upgrade при верификации

---

## См. также

- [Data Model](data-model.md)
- [Structural Org & Supply Chain Model](structural-org-supply-chain-model.md) — структурированные организации, org units, голографические множества
- [Taxonomy](taxonomy.md)
- [Product Hierarchy](product-hierarchy.md)
- [Roadmap](../implementation/roadmap.md)

