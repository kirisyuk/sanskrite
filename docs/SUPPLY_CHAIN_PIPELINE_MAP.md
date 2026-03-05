# Supply Chain Pipeline Map

Карта фаз пайплайна, типов объектов и логики изоляции для supply chain domain.

---

## Единый поток обнаружения (Domain Expansion)

В новой архитектуре мы отказались от альтернативного потока (`SupplyChainGapAnalyzer` и `RefineDirectives`) в пользу единого, строго типизированного потока `Domain Expansion`. Вся логика построения цепочек поставок централизована.

```
Planning → Extraction (skip) → Critic (skip) → Research → Materialize → Policy
```

### Фаза Research (plan mode)

| Шаг | Кто | Запрос | Структура | Результат |
|-----|-----|--------|-----------|-----------|
| 1 | IntentResolver | domain path + facets | `SupplyChainExecutionPlan{Queries: []SupplyChainQuery}` | План с anchor/product/market anchors |
| 2 | IntentDecomposer | product/market → org+fac | `expandSupplyChainQueriesWithOrgs` | `SupplyChainQuery.Anchors = []string{"/fac/..."}` |
| 3 | EnrichSupplyChainFull | anchor-centric | `SupplyChainQuery{Anchor\|Anchors, Direction, Depth, ProductFilter, GeoFilter}` | `[]SupplyChain` |
| 4 | Domain Isolation | Strict Filters | `FilterDomainChains`, `FilterDomainRelations` | Отфильтрованные `[]SupplyChain` без нарушений |

**Структура SupplyChainQuery:**
- `Anchor` — один путь (если нет Anchors)
- `Anchors` — список `/org/` или `/fac/` или `/deposit/` (приоритет)
- `AnchorType` — `"org"` | `"product"` | `"deposit"`
- `Direction` — `"upstream"` | `"downstream"` | `"both"`
- `Depth` — 3–6
- `ProductFilter`, `GeoFilter` — фильтры

**Структура SupplyChain:**
- `Nodes: []SupplyChainNode` — actor→product→actor→product
- `SupplyChainNode`: `CanonicalPath`, `Name`, `Type` (ORGANIZATION | FACILITY | DEPOSIT | PRODUCT), `Role`
- `SegmentMetrics`, `Alternatives`, `TransportVia`

---

## Строгая изоляция доменов (Domain Isolation)

Исполнители доменных контрактов строго изолированы по логике и работают только со своим разрешенным набором объектов и связей. Связи или объекты, сгенерированные LLM с нарушением этих правил, жестко отбрасываются (drop/truncate) на этапе Research до попадания в граф.

### Разрешенные типы сущностей (Entity Types)

| Домен (Domain Mode) | Разрешенные типы сущностей (Allowed Entities) |
|---------------------|-----------------------------------------------|
| **supply_chain** | `ORGANIZATION`, `FACILITY`, `PRODUCT`, `DEPOSIT`, `GEO_REGION`, `COMMUNITY`, `PROGRAM` |
| **governance** | `ORGANIZATION`, `PERSON`, `COMMUNITY`, `PROGRAM` |
| **market** | `ORGANIZATION`, `FACILITY`, `COMMUNITY`, `PROGRAM`, `MARKET` |
| **assurance** | `ORGANIZATION`, `FACILITY`, `PROGRAM` |

*Важно: объекты типа `DEPOSIT` (месторождения) являются полноправными участниками supply_chain (как добывающие узлы) и обрабатываются наравне с `FACILITY` и `ORGANIZATION`.*

### Разрешенные типы связей (Edge Types)

| Домен (Domain Mode) | Разрешенные типы связей (Allowed Edges) |
|---------------------|-----------------------------------------|
| **supply_chain** | `PRODUCES`, `SUPPLIES_TO`, `TRANSFORMS_INTO`, `EXTRACTS_FROM`, `LOCATED_IN`, `OPERATES_IN`, `DEPENDS_ON`, `HAS_MARKET_SEGMENT` |
| **governance** | `OWNS`, `SUBSIDIARY_OF`, `CONTROLS`, `MANAGES`, `INVESTS_IN`, `BOARD_MEMBER_OF`, `EXECUTIVE_OF`, `EMPLOYED_BY`, `FOUNDED`, `ADMINISTERS`, `ASSURES` |
| **assurance** | `ASSURES`, `REGULATES` |

Данные правила программно закреплены в `backend/internal/taxonomy/domain_mode.go` (`AllowedEntityTypesForDomainMode`, `AllowedEdgeTypesForDomainMode`) и применяются фильтрами в `domain_filters.go`. Исключения из правил логируются как `domain_isolation.exception` в GCP traces.

---

## Типы объектов (каноничные)

| Префикс | Тип | Пример | Описание |
|---------|-----|--------|----------|
| `/org/` | ORGANIZATION | `/org/th/tipco-foods` | Компании, юрлица. В supply chain часто маппятся на facility. |
| `/fac/` | FACILITY | `/fac/tipco-foods-general-production` | Заводы, фабрики, порты. Физические точки логистики. |
| `/product/` | PRODUCT | `/product/raw/fruit/fresh/pineapple` | Товары, сырье, компоненты. |
| `/deposit/` | DEPOSIT | `/deposit/cl/chuquicamata-copper-mine` | Месторождения природных ресурсов. Критичны для upstream supply chains (шахта -> сырье). |
| `/market/` | MARKET | `/market/electric-vehicles` | Абстрактный якорь для декомпозиции. |

Цепочка: `actor → product → actor → product → ...` (org/fac/deposit → product → org/fac).

---

## Фаза Materialize

- `processSupplyChains` — парсит отфильтрованные chains → создаёт Fragments, Edges, SupplyLinks
- `runSupplyChainExpandPhase` — optional 1-hop expand (low confidence / gapHints / productFilter)
- `runSupplyChainStage2Overlays` — regulatory/market/decision_points по анкерам
- Данные собираются в `Engine.collectGraph` (в `domain/engine.go`), который также проверяет соответствие артефактов `expectedRelationsForContractType` и `expectedEntitiesForContractType` (fail-safe проверка на этапе чтения).

## Логирование и UI (Telemetry & Realtime)

- **GCP Traces:** Очищены от спама. Оставлены только высокоуровневые события (start/end), отклонения доменной изоляции (`domain_isolation.exception`), и критические ошибки/решения.
- **Frontend UI:** Агрегированные, человекочитаемые события отправляются с бэкенда (`humanReadable` payload) по окончании стадий пайплайна (например, "Supply chain saved • +5 new • +8 links"). Сложный RegEx парсинг сырых логов на фронтенде удален.
