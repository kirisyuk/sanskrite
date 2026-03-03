# Supply Chain Pipeline Map

Карта фаз пайплайна, типов объектов и запросов для supply chain domain.

---

## Два параллельных потока

| Поток | Триггер | Путь |
|-------|---------|------|
| **Domain expansion** | Job для `/domain/.../supply-chain/...` | executeDomainExpansionJob |
| **Entity pipeline** | Job для `/org/`, `/fac/`, `/deposit/` | runEntityScout → stageEntitySupplyChainRefine |

Директивы (`directive: [...]`) появляются во **втором** потоке (Entity), когда скоутим конкретную организацию (tipco-foods) — не в domain expansion.

---

## Flow 1: Domain Expansion (`/domain/`)

```
Planning → Extraction (skip) → Critic (skip) → Research → Materialize → Policy
```

### Фаза Research (plan mode)

| Шаг | Кто | Запрос | Структура | Результат |
|-----|-----|--------|-----------|-----------|
| 1 | IntentResolver (ранее) | domain path + facets | `SupplyChainExecutionPlan{Queries: []SupplyChainQuery}` | План с anchor/product/market anchors |
| 2 | expandSupplyChainQueriesWithOrgs | product/market → org+fac | IntentDecomposer | `SupplyChainQuery.Anchors = []string{"/fac/..."}` |
| 3 | EnrichSupplyChainFull | anchor-centric | `SupplyChainQuery{Anchor\|Anchors, Direction, Depth, ProductFilter, GeoFilter}` | `[]SupplyChain` |

**Структура SupplyChainQuery:**
- `Anchor` — один путь (если нет Anchors)
- `Anchors` — список `/org/` или `/fac/` (приоритет)
- `AnchorType` — `"org"` \| `"product"` \| `"deposit"`
- `Direction` — `"upstream"` \| `"downstream"` \| `"both"`
- `Depth` — 3–6
- `ProductFilter`, `GeoFilter` — фильтры

**Структура SupplyChain:**
- `Nodes: []SupplyChainNode` — actor→product→actor→product
- `SupplyChainNode`: `CanonicalPath`, `Name`, `Type` (ORGANIZATION \| FACILITY \| PRODUCT), `Role`
- `SegmentMetrics`, `Alternatives`, `TransportVia`

### Фаза Materialize

- `processSupplyChains` — парсит chains → создаёт Fragments, Edges, SupplyLinks
- `runSupplyChainExpandPhase` — optional 1-hop expand (low confidence / gapHints / productFilter)
- `runSupplyChainStage2Overlays` — regulatory/market/decision_points по анкерам

**GapAnalyzer и RefineDirectives в domain expansion НЕ вызываются.**

---

## Flow 2: Entity Pipeline (`/org/`, `/fac/`)

```
SourcePlanning → SourceExecution → Extraction → Critic → Research → SupplyChainRefine → Persistence → RelationMaterialize → SupplyChainMaterialize
```

### Фаза Research

| Шаг | Кто | Запрос | Результат |
|-----|-----|--------|-----------|
| Research | ResearchPipeline | `ResearchStrategyInput{Fragment, MissingInfo, ChainContext}` | `Relations`, `SupplyChain` |

Chains приходят из `scoutResult.SupplyChains` (extraction + research).

### Фаза SupplyChainRefine (источник директив)

| Шаг | Кто | Вход | Выход |
|-----|-----|------|-------|
| 1 | SupplyChainGapAnalyzer.Analyze | `chains`, `fragment` | `SupplyChainGapReport` |
| 2 | buildDirectives | report | `RefineDirectives` |
| 3 | RefineSupplyChain | chains + directives | `[]SupplyChainPatch` |
| 4 | ApplyPatches | chains + patches | refined chains |

---

## Откуда берутся RefineDirectives

`buildDirectives` собирает директивы **строго** из детерминированных gap-типов и (опционально) LLM `industryGaps`:

| Gap тип | Источник | Структура RefineDirective | Запрос (Query) | PatchType |
|---------|----------|---------------------------|----------------|-----------|
| **missing_tier** | TierGap (checkTierDepth) | `AnchorUpstream`, `AnchorDownstream`, `ExpectedStages` | `"Find intermediate processing stages between X and Y"` | splice |
| **depth_gap** | DepthGap (checkUpstreamDownstream) | `AnchorUpstream`/`AnchorDownstream`, `ChainIndex` | `"Extend the supply chain upstream/downstream from X"` | extend_upstream \| extend_downstream |
| **concentration_risk** | ConcentrationRisk (checkConcentration) | `AnchorUpstream=ProductPath` | `"Find alternative suppliers for ProductName (productPath)"` | alternatives |
| **logistics_gap** | LogisticsGap (checkLogistics) | `AnchorUpstream`, `AnchorDownstream` | `"Identify transport routes between X and Y"` | splice |
| **industry_gap** | **LLM** (runLLMGapAnalysis) | `AnchorPath` (optional), `Description` | **g.Description** (свободный текст!) | extend_downstream |

**Проблема:** для `industry_gap` Query = `g.Description` — то есть LLM сам пишет описание в `IndustryGap.Description`, и это попадает в RefineDirective как есть. Промпт `buildLLMGapAnalysisPrompt` просит "identify typical value chain stages", "chokepoints", "geopolitical risks" — отсюда размытые "Strait of Malacca transit", "Climate-driven crop volatility".

---

## Типы объектов (каноничные)

| Префикс | Тип | Пример |
|---------|-----|--------|
| `/org/` | ORGANIZATION | `/org/th/tipco-foods` |
| `/fac/` | FACILITY | `/fac/tipco-foods-general-production` |
| `/product/` | PRODUCT | `/product/raw/fruit/fresh/pineapple` |
| `/deposit/` | DEPOSIT | extraction sources |
| `/market/` | MARKET | абстрактный якорь для decomposition |

Цепочка: `actor → product → actor → product → ...` (org/fac/deposit → product → org/fac).

---

## Patch-типы и ожидаемые структуры

| PatchType | Ожидание | Anchor |
|-----------|----------|--------|
| **splice** | ≥3 nodes: anchor₁ → inner → anchor₂ (оба anchor уже в chain) | BetweenUpstream, BetweenDownstream |
| **extend_upstream** | ≥2 nodes: new... → anchor (последний = существующий) | TerminalNode |
| **extend_downstream** | ≥2 nodes: anchor → new... (первый = существующий) | TerminalNode |
| **alternatives** | `alternatives[]` с `supplierRef`, `technicalFit`, `regulatoryFit` | ProductPath |

---

## Корневая причина "хрени" в запросах

1. **industry_gap** — единственный тип, где Query формируется LLM, а не шаблоном. `buildDirectives` просто делает `Query: g.Description`.
2. Промпт `buildLLMGapAnalysisPrompt` просит "chokepoints (Suez, Malacca...)", "geopolitical risks", "substitution opportunities" — без привязки к **конкретным узлам графа**.
3. Нет жёсткой схемы для `IndustryGap`: `gapKind` свободный, `description` свободный, `anchorPath` опционален.
4. Результат — расплывчатые "Strait of Malacca transit", "Climate-driven crop volatility", "Aseptic packaging supply", которые не маппятся на splice/extend/alternatives.

---

## Что должно быть вместо этого

- **Только structural gaps**: пропущенные узлы (org/fac/product) и рёбра (SUPPLIES_TO, TRANSFORMS_INTO).
- **Запросы = шаблоны** с подставленными canonical path: `"Extend upstream from {terminalPath}"`, `"Find alternatives for {productPath}"`, `"Splice between {upPath} and {downPath} for expected stage {stage}"`.
- **industry_gap либо убрать**, либо сузить до "missing_stage" / "missing_component" с обязательным `anchorPath` и фиксированным шаблоном Query.
- **RefineDirective.Query** — всегда чёткий actionable текст, не свободная фантазия LLM.
