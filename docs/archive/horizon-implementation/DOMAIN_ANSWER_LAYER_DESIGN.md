# Domain Answer Layer Design

## Статус

Draft → **Agreed** (decisions fixed, ready for implementation)

## Pre-implementation Reconciliation (код ↔ дизайн)

Расхождения между текущим кодом и дизайном, которые нужно разрешить перед реализацией.

### P1. Типы контрактов: naming convention

**Расхождение**: в дизайне `supply_chain_investigation` (snake_case), в коде — kebab-case allowlist (`supply-chain`, `risk-assessment`, и т.д.).

**Код**: `paths.go:296` — `domainContractTypes` map, `domain_resolver_agent.go:132` — prompt allowlist. Всего ~40 типов, canonical path `/domain/v1/{contractType}/...`.

**Решение**: **сохраняем kebab-case** — это каноническая форма, уже в проде, уже в canonical paths. Дизайн адаптируется.

Маппинг дизайн → код:

| Дизайн (Steering Profile) | Код (contractType) | Примечание |
|----------------------------|-------------------|------------|
| `supply_chain_investigation` | `supply-chain` | Основной тип. Также: `value-chain`, `logistics-flow`, `trade-flow`, `sourcing-map`, `procurement-map` |
| `regulatory_exposure` | `regulatory-impact` | Также: `sanctions-exposure`, `compliance-landscape`, `licensing-requirements`, `policy-and-regulation` |
| `competitive_landscape` | `competitive-landscape` | Также: `market-opportunity`, `market-entry`, `market-sizing` |
| `geographic_concentration` | Нет прямого аналога | **Добавить** `geographic-concentration` в `domainContractTypes` allowlist |

**Steering Profile привязывается к группе contractTypes**, не к одному. Один profile покрывает несколько kebab-case типов:

```go
// steering_profiles.go
var profileByContractType = map[string]SteeringProfileName{
    "supply-chain":           ProfileSupplyChain,
    "value-chain":            ProfileSupplyChain,
    "logistics-flow":         ProfileSupplyChain,
    "trade-flow":             ProfileSupplyChain,
    "sourcing-map":           ProfileSupplyChain,
    "procurement-map":        ProfileSupplyChain,

    "regulatory-impact":      ProfileRegulatoryExposure,
    "sanctions-exposure":     ProfileRegulatoryExposure,
    "compliance-landscape":   ProfileRegulatoryExposure,
    "policy-and-regulation":  ProfileRegulatoryExposure,
    "licensing-requirements": ProfileRegulatoryExposure,

    "competitive-landscape":  ProfileCompetitiveLandscape,
    "market-opportunity":     ProfileCompetitiveLandscape,
    "market-entry":           ProfileCompetitiveLandscape,
    "market-sizing":          ProfileCompetitiveLandscape,
    "demand-forecast":        ProfileCompetitiveLandscape,

    // ... etc
}
```

**TODO при реализации**:
- [ ] Добавить `geographic-concentration` в `paths.go:domainContractTypes`.
- [ ] Обновить prompt allowlist в `domain_resolver_agent.go`.
- [ ] В DomainContractEngine: resolve contractType → SteeringProfile через маппинг.
- [ ] В Gate checks: тоже через SteeringProfile, не через raw contractType.

### P2. Multi-domain: несколько доменов в одном job, без иерархии

**Расхождение**: ранняя формулировка дизайна ("один домен = один запрос") конфликтовала с `IntentResolverAgent`, который возвращает до 3 доменов. Уточнённое решение снимает конфликт.

**Код**: `intent_resolver_agent.go:296` — cap на 3 домена. `lenses_from_query.go:507-519` — все domain paths идут в Lens scope и скедулятся как отдельные scouting jobs.

**Решение**: **допускаем несколько доменов в одном job, обрабатываем каждый независимо**.

1. IntentResolver возвращает N доменов (до 3) — оставляем as-is.
2. Каждый домен получает **свой прогон DomainContractEngine**: свой Steering Profile, свой Gate, свой `DomainAnswerSnapshot`.
3. Все домены работают на **одном и том же графе** (Lens scope), но интерпретируют его по-разному (разные contractType → разные Steering Profiles → разные критерии полноты).
4. Домены — **peers, не иерархия**. Нет parent/child. Нет merge в primary.
5. UI: Domain Overlay — сворачиваемый контрол поверх графа (mini → domain list → expanded). Граф первичен, overlay управляет подсветкой.

**Пример**: запрос "Analyze semiconductor supply chain risks and regulatory exposure":
- Домен 1: `supply-chain` (facets: litho, packaging, inputs) → свой DomainAnswerSnapshot.
- Домен 2: `sanctions-exposure` (facets: export controls, licensing) → свой DomainAnswerSnapshot.
- Оба работают на одном графе. Если supply-chain скаут нашёл TSMC→CONSUMES→neon, regulatory домен тоже видит это ребро и может его использовать.

**Cross-domain awareness** (future, не блокирует реализацию):
- В будущем домены могут обмениваться findings (e.g., regulatory домен знает что "Китай ограничил экспорт галлия" → supply-chain домен учитывает это в scoring). Сейчас — каждый видит полный граф, что достаточно.

**TODO при реализации**:
- [ ] `DomainContractEngine.Run()` вызывается для каждого домена в scope отдельно (loop или parallel).
- [ ] Cockpit API: возвращает `domainAnswers[]` (массив, а не singular).
- [ ] Domain Overlay UI: сворачиваемый контрол (mini → list → expanded) с поддержкой нескольких доменов.
- [ ] IntentResolver: оставить as-is (cap 3).

### P3. API контракт: Cockpit и graph-query

**Расхождение**: Cockpit возвращает `{lens, graph, summary}` без `domainAnswer` (`cockpit.go:240`). Graph-query принимает сырой graph snapshot (`graph_query.go:374`).

**Код**:

```go
// cockpit.go:240 — текущий response
response := map[string]interface{}{
    "lens":    lensPayload,
    "graph":   map[string]interface{}{"nodes": rootFragments, "edges": allEdges},
    "summary": map[string]interface{}{"totalFragments": ..., "totalEdges": ..., "metrics": ...},
}

// graph_query.go:374 — текущий request
type graphQueryRequest struct {
    Question string
    Graph    struct {
        Nodes []graphSnapshotNode
        Edges []graphSnapshotEdge
    }
}
```

**Решение**: расширяем оба endpoint'а с обратной совместимостью.

**Cockpit** — добавляем `domainAnswers` (массив, может быть пуст):

```go
response := map[string]interface{}{
    "lens":    lensPayload,
    "graph":   map[string]interface{}{"nodes": rootFragments, "edges": allEdges},
    "summary": map[string]interface{}{...},
    // NEW: domain answer layers (пустой массив если нет domains или snapshots ещё не собраны)
    "domainAnswers": domainAnswerPayloads, // []DomainAnswerSnapshot (может быть 0..3)
}
```

Фронт: если `len(domainAnswers) > 0` → показать Domain Overlay (mini-плашка, сворачиваемая до списка доменов и expanded view). Если пуст → поведение как сейчас (только граф).

**Graph-query** — расширяем request, добавляя `domainAnswerContext` (optional):

```go
type graphQueryRequest struct {
    Question string
    Graph    struct { ... }  // backward compat: фронт всё ещё может слать snapshot
    // NEW: если есть — LLM использует как primary context (quality выше)
    DomainAnswerContext *DomainAnswerSnapshot `json:"domainAnswerContext,omitempty"`
}
```

Логика: если `DomainAnswerContext` передан → LLM отвечает из него (drill-down). Если нет → fallback на текущий graph snapshot (backward compat).

**TODO при реализации**:
- [ ] `cockpit.go`: загрузить `[]DomainAnswerSnapshot` для всех доменов в Lens scope из Firestore; добавить в response как `domainAnswers`.
- [ ] `graph_query.go`: расширить request struct; приоритет: DomainAnswerContext > Graph snapshot.
- [ ] Новый endpoint (опционально): `GET /api/horizon/lenses/{lensId}/domain-answers` — только domain answers без графа (для лёгких UI-запросов).
- [ ] `GET /api/horizon/lenses/{lensId}/domain-answers/{domainId}` — конкретный domain answer.

### P4. Пороговые значения Gate (placeholders)

**Расхождение**: в Gate L1 несколько порогов оставлены как placeholder (≥ N, ≥ 3, < 70%).

**Решение**: зафиксировать **начальные значения** для реализации. Пороги выносятся в конфигурацию (Firestore config или Go constants), чтобы можно было тюнить без деплоя.

| Check | Порог | Обоснование | Конфиг-ключ |
|-------|-------|-------------|-------------|
| **U1** Facet Population | P0: 0 узлов; P1: < 3 узлов | 3 — минимум для "не единственный пример" | `gate.min_nodes_per_facet = 3` |
| **U2** Node Quality (EpistemicStatus) | Weighted score ≥ 0.5 (VERIFIED=1.0, LOW_CONFIDENCE=0.5, REJECTED=0.0, empty=0.5) | Половина данных ненадёжна = недостаточно для ответа. См. P5 | `gate.min_quality_ratio = 0.5` |
| **U3** Inter-Facet Connectivity | ≥ 1 path ≤ 5 hops на пару facets | 5 hops — разумный diameter для supply chain graph | `gate.max_inter_facet_hops = 5` |
| **U4** Critical Dependency Paths | Top-K: K = min(10, total) | 10 — из playbook ("top 10 dependencies") | `gate.top_k_dependencies = 10` |
| **U5** Edge Direction & Policy | 0 edges без policy в critical paths | Strict: critical paths должны быть policy-clean | N/A (boolean) |
| **U6** Data Gap Ratio | P0: ≥ 70%; P1: ≥ 50% | 70% — "в основном не знаем"; 50% — "знаем примерно половину" | `gate.max_gap_ratio_p0 = 0.7`, `gate.max_gap_ratio_p1 = 0.5` |
| **U7** Temporal Validity | ≥ 50% edges с temporal overlap | Половина — компромисс (многие edges не имеют temporal) | `gate.min_temporal_coverage = 0.5` |
| **U8** Source Diversity | ≥ 2 unique sources для top-K | 2 — минимальная corroboration | `gate.min_sources_per_dep = 2` |
| **SC2** Upstream/Downstream | ≥ 1 upstream AND ≥ 1 downstream | Минимум для "цепочка, а не точка" | N/A (boolean) |
| **SC6** Geographic Distribution | ≥ 80% critical nodes с geo | 80% — допуск на entities без известной гео | `gate.min_geo_coverage = 0.8` |
| **Enrichment cycles** | ≤ 2 | 2 — баланс полноты и бюджета | `gate.max_enrichment_cycles = 2` |

**TODO при реализации**:
- [ ] Создать `domain/gate_config.go` с defaults (Go constants) + Firestore override.
- [ ] Все пороги — float64/int, читаемые из config с fallback на defaults.
- [ ] Логировать каждый check result в structured log (для будущего тюнинга).

### P5. Gate U2: `verificationStatus=stub` vs `EpistemicStatus`

**Расхождение**: Gate check U2 ссылается на `verificationStatus != stub`, но в модели такого поля нет. Фактический сигнал качества фрагмента — `EpistemicStatus` (`VERIFIED | REJECTED | LOW_CONFIDENCE`).

**Код**: `fragment.go:182` — `EpistemicStatus EpistemicStatus`. `common.go:131` — три значения.

**Решение**: U2 переформулируется на реальную модель. Один формальный критерий — **weighted score**, без параллельных формулировок.

| Было (дизайн) | Стало (по модели) |
|----------------|-------------------|
| `verificationStatus != stub` | Weighted score по `EpistemicStatus` (единственный критерий, см. ниже) |
| `policyStatus != quarantined` | Без изменений — `policyStatus` есть на edge level через policy qualifiers |

**Реальные поля модели** (`fragment.go:172-182`):

| Поле | Тип | Роль |
|------|-----|------|
| `EpistemicStatus` | `VERIFIED \| REJECTED \| LOW_CONFIDENCE` | Основной сигнал качества для Gate U2 |
| `LastVerifiedAt` | `time.Time` | Watermark последней верификации (можно использовать для temporal freshness, но **не** для U2) |
| `LastFactsSeenAt` | `time.Time` | Watermark incremental facts processing (не для U2) |

**Gate U2 criterion (единственная формулировка)**:

Weighted quality score ≥ `gate.min_quality_ratio` (default 0.5) для каждого facet.

Веса:

| EpistemicStatus | Weight |
|-----------------|--------|
| `VERIFIED` | 1.0 |
| `LOW_CONFIDENCE` | 0.5 |
| `""` (empty/unset) | 0.5 |
| `REJECTED` | 0.0 |

`score = sum(weight_i) / count(fragments_in_facet)`

Если `score < 0.5` — facet не проходит U2 → enrichment.

**TODO при реализации**:
- [ ] В `gate.go`: U2 check использует `fragment.Metadata.EpistemicStatus`, не мифический `verificationStatus`.
- [ ] Weighted scoring: единственный критерий, таблица весов выше. Без альтернативных формулировок.

### P6. Существующий `domain_overlay.go` — миграция

**Расхождение**: уже есть endpoints `PreviewDomainOverlay` и `ExecuteDomainOverlay` (`domain_overlay.go`), которые делают intent → domain contract → job creation. Это прото-версия DomainContractEngine.Resolve() + Steer().

**Код**: `domain_overlay.go:41` — `PreviewDomainOverlay` (resolve intent without job), `ExecuteDomainOverlay` (resolve + create job).

**Решение**: **мигрировать, не держать параллельно**.

1. `PreviewDomainOverlay` → `DomainContractEngine.Resolve()` (preview mode: resolve contract, return, don't execute).
2. `ExecuteDomainOverlay` → `DomainContractEngine.Run()` (full cycle: Resolve → Steer → ... → Publish).
3. Старые endpoints оставить как thin wrappers, делегирующие в DomainContractEngine (backward compat для фронта).
4. После миграции фронта — deprecated, удалить.

**TODO при реализации**:
- [ ] `DomainContractEngine.Resolve()` должен поддерживать preview mode (без создания job).
- [ ] `domain_overlay.go` endpoints → delegate to DomainContractEngine.
- [ ] Фронт: мигрировать с `domain-overlay-preview`/`domain-overlay-execute` на новые endpoints.

### P7. Опорные точки в коде (для реализации)

Зафиксированы extension points, которые DomainContractEngine использует:

| Опорная точка | Файл | Как используется |
|---------------|------|-----------------|
| Domain job ветка | `engine.go:1847` | `/domain/` prefix → DomainContractEngine orchestration вместо/после текущего domain-expansion |
| ContractRegistry | `pipeline/passes.go:30`, `contract.go:57` | `DomainAnswerHandler` регистрируется через `ContractRegistry` для domain contractTypes. Внутри — delegates to DomainContractEngine (R8) |
| BuildContractInput / PersistViewDirective | `pipeline/passes.go:31,58` | Pipeline вызывает handler → handler возвращает adapted `ViewDirective` (backward compat). Snapshot публикуется в отдельный repo |
| Domains в job.State | `lenses_from_query.go`, `service.go` | DomainContractEngine читает domain contracts из `job.State["domains"]` |
| Post-processing pattern | `engine.go:1320` (`persistLensArtifactsFromJobState`) | Assemble фаза работает аналогично — post-processing после job completion |
| Cockpit API | `cockpit.go:240` | Расширяем response: `domainAnswers[]` |
| Graph-query API | `graph_query.go:374` | Расширяем request: `DomainAnswerContext` |
| Domain overlay endpoints | `domain_overlay.go` | Мигрируются в DomainContractEngine (P6) |

## Зачем

Текущий Horizon хорошо строит и показывает граф, но для C-level клиентского сценария нужен режим `answer-first`:

- сначала ответ на вопрос пользователя;
- затем объяснимые пути в графе как доказательная проекция;
- текстовый executive summary вторичен и генерируется в конце.

Запросы класса:

> Investigate systemic risks and opportunities in the global semiconductor supply chain (2026–2030). Start with lithography equipment, advanced packaging, and critical inputs (neon, gallium, germanium). Map top suppliers, geographic concentration, single points of failure, sanctions/export-control exposure, and substitution options. Quantify TAM/CAGR/CR4/HHI where available, and prioritize the top 10 highest-impact dependencies with confidence and sources.

Система разбирает и строит граф, но отображает просто как граф. Domain Answer Layer превращает граф в структурированный ответ.

## Основная идея

Ввести над графом отдельный слой `Domain Answer Layer`:

- `Domain` остается контрактом (что именно хотим понять);
- в одном job может быть **несколько доменов** (до 3), каждый обрабатывается независимо;
- домены — **peers, не иерархия**; под-темы внутри одного домена — **facets**;
- после сборки графа — post-processing шаг на каждый домен: детерминированная проекция + один structured LLM вызов;
- из артефактов собираются storyline, UI-оверлеи и только потом текст.

## Архитектурные решения (зафиксированы)

### R1. Домены без иерархии, facets внутри домена

Один `Domain` = один `DomainAnswerSnapshot`. В одном job может быть несколько доменов (IntentResolver возвращает до 3); каждый обрабатывается DomainContractEngine **независимо**, на одном и том же графе (Lens scope).

Под-темы запроса (lithography equipment, advanced packaging, critical inputs) — это **facets внутри единого доменного контракта**, а не отдельные домены. Тип контракта определяет **структуру** ответа (какие артефакты нужны), facets определяют **разрезы** (по каким частям графа их собирать).

```
DomainContract {
  type: "supply-chain"
  query: "<исходный запрос пользователя>"
  facets: [
    { key: "lithography_equipment", scope: "/product/finished/electronic/manufacturing/lithography-**" },
    { key: "advanced_packaging",    scope: "/product/finished/electronic/manufacturing/packaging-**" },
    { key: "critical_inputs",       scope: "/product/raw/chemical/**, /product/raw/mineral/**" }
  ]
  temporal_horizon: "2026-01-01..2030-12-31"
  polarity: ["risk", "opportunity"]
}
```

- `critical_dependencies[]` — каждая зависимость тегируется facet-ключом.
- `DomainAnswerSnapshot` — один, но с facet-группировкой.
- UI показывает один ответ с навигацией по facets (табы, фильтры, highlight в графе).
- Граф один, overlay один, подсвечиваемые пути зависят от выбранного facet.

Если когда-нибудь потребуется иерархия — facets можно вложить или переиспользовать как отдельные домены. Но сейчас — flat.

### R2. Сборка артефактов — post-processing + один structured LLM вызов

Артефакты **не** накапливаются параллельно с Scout/Research. Вместо этого:

1. Job проходит стандартный пайплайн (Scout → Critic → Research → Policy) — **без изменений**.
2. После сборки графа — **post-processing шаг** (`domain_answer_assemble`):
   - **Детерминированная проекция** из графа:
     - `critical_dependencies` — scoring по формуле `TopDependencyScore` (impact × exposure × concentration × (1 − substitutability) × sanction_export_control_exposure × confidence).
     - `critical_nodes` — из highProfile signals + path centrality.
     - `concentration_evidence` — агрегация TAM/CAGR/CR4/HHI с market-узлов; explicit `data_gap` где нет.
     - `control_exposure` — обход regulatory layer (REGULATES/CONTROLS_ACCESS/USES_MECHANISM edges).
     - `impact_paths` — BFS/Dijkstra по criticality, explainable transmission paths.
     - `data_gaps` — перечень отсутствующих метрик, пустых facets, недостающих substitution данных.
   - **Один structured LLM вызов** (после детерминированной части):
     - Вход: вычисленные артефакты + domain contract + исходный запрос.
     - Выход: `thesis` (structured short), `alternatives[].feasibility` оценки, `objective_check`.

Промпты Scout/Critic/Research **не усложняются**. LLM работает как "редактор" над уже вычисленным, а не как "исследователь".

### R3. Completeness Gate — гибрид детерминированных проверок + Domain Critic

Двухуровневая проверка полноты:

**Level 1 — Structure completeness (детерминированный, без LLM):**

| Проверка | Критерий | Приоритет | Если не пройден |
|----------|----------|-----------|-----------------|
| Facet population | ≥ 3 узлов в каждом facet (P0 если 0) | P0/P1 | enrichment задачи на пустые facets |
| Inter-facet connectivity | ≥ 1 path ≤ 5 hops между каждой парой facets | P0 | cross-facet stitching |
| Market metrics | ≥ 1 market-узел с TAM или CR4 | P1 | enrichment на market data |
| Regulatory edges | ≥ 1 REGULATES/CONTROLS_ACCESS edge | P1 | enrichment на control layer |
| Critical paths | Top-10 зависимостей имеют explainable paths | P0 (top-3), P1 (4-10) | enrichment на path gaps |
| Substitution data | Для top-10 зависимостей есть alternatives или explicit gap | P2 | enrichment на alternatives |
| Data gap ratio | < 70% артефактов — data_gap | P0 (≥70%), P1 (≥50%) | приоритизировать top gaps |

Конкретные пороги и конфиг-ключи — см. секцию **P4** в Pre-implementation Reconciliation и **Completeness Gate (детальный дизайн)**.

Если Level 1 не пройден — enrichment **без LLM вызова**, задачи генерируются детерминированно.

**Level 2 — Semantic completeness (Domain Critic, старшая модель):**

Если Level 1 пройден:
- Старшая модель получает описание собранного графа + исходный запрос.
- Оценивает: достаточно ли графа для объективного ответа на запрос?
- Если нет — enrichment signals (по паттерну Critic): конкретные задачи на дообогащение в рамках того же job.

**Ограничения enrichment цикла:**
- ≤ 2 цикла enrichment.
- Budget cap (USD + output tokens).
- Если полнота не достигнута: `readiness_status: "ready_with_gaps"` с явным перечнем пробелов.

### R4. Graph-query → drill-down поверх DomainAnswerSnapshot

Текущий `POST /api/horizon/lenses/{lensId}/graph-query` эволюционирует:

- **Сейчас**: фронт отправляет снэпшот графа (nodes, edges), LLM отвечает на произвольный вопрос.
- **После**: LLM получает `DomainAnswerSnapshot` как контекст (не голый граф). Drill-down вопросы ("расскажи подробнее про зависимость от неона") работают с уже структурированным ответом → качество радикально выше.

### R5. Cross-facet connectivity: домен стирит scouting

Для explainability недостаточно, чтобы в каждом facet были узлы. Нужны **связи между facets** — иначе impact_paths невозможны, а ответ превращается в список сущностей без истории.

**Два уровня связей, оба обязательны:**

1. **Domain → answer-relevant nodes (вертикальные)**: домен "знает" какие узлы входят в его ответ — через facet scope matching + explicit domain-level edges (`INVESTIGATES`).

2. **Between answer-relevant nodes (горизонтальные)**: между узлами разных facets должны быть цепочки edges (SUPPLIES_TO, CONSUMES, LOCATED_IN, REGULATES...). Без них нет transmission chain, нет explainability.

**Что это означает для пайплайна:**

- На этапе `domain_resolve`: из facets формируются **cross-facet expectations** — ожидаемые типы связей между facets ("между lithography equipment и critical inputs ожидаем CONSUMES/DEPENDS_ON").
- На этапе `graph_collect`: DomainContractEngine передаёт Scout'у domain context — при обработке узла из одного facet'а Scout **активно ищет связи к узлам других facets** (не просто "обогати TSMC", а "обогати TSMC и проверь: использует ли он neon/gallium/germanium?").
- В Completeness Gate (Level 1): проверка **inter-facet connectivity** — есть ли paths между facets? Если facets изолированы — это critical gap, enrichment обязателен. Приоритет enrichment: cross-facet gaps > intra-facet gaps.

```
                    ┌─────────────────────┐
                    │      DOMAIN         │
                    │ (supply_chain_inv.) │
                    └──┬──────┬──────┬────┘
                       │      │      │         ← вертикальные (domain → facet nodes)
              ┌────────┘      │      └────────┐
              ▼               ▼               ▼
     ┌────────────┐  ┌───────────┐  ┌─────────────┐
     │ TSMC       │  │ ASML      │  │ Ingas       │
     │ (litho)    │  │ (litho)   │  │ (crit.input)│
     └─────┬──────┘  └─────┬─────┘  └──────┬──────┘
           │                │                │
           │    SUPPLIES_TO │   CONSUMES     │     ← горизонтальные (inter-node,
           │◄───────────────┘                │        cross-facet paths)
           │                                 │
           │         CONSUMES                │
           └─────────────────────────────────┘
                         ▲
                    без этого ребра нет impact_path
                    и нет explainability
```

### R6. DomainContractEngine — выделенный расширяемый движок

Исполнение доменных контрактов выносится в **отдельный движок** `DomainContractEngine` (`backend/internal/horizon/domain/engine.go`).

**Зачем отдельный движок:**

Домены — это не просто "ещё один тип фрагмента". Это отправная точка для:
- Структурированных ответов на запросы (Domain Answer Layer — сейчас).
- Сценарного анализа ("что если неон перекроют?" — будущее).
- Сравнительного анализа ("сравни зависимости lithography vs packaging" — будущее).
- Мониторинга изменений по контракту ("алерт если top-зависимость изменилась" — будущее).

Наполнение и исполнение доменного контракта — самостоятельная ответственность, которая не должна быть размазана по ScoutEngine или engine_ext.go. Scout строит граф. DomainContractEngine **осмысляет** граф через призму контракта.

**Ответственности DomainContractEngine:**

| Фаза | Ответственность |
|------|-----------------|
| `Resolve` | NL запрос → типизированный DomainContract + facets + cross-facet expectations |
| `Steer` | Генерация domain-aware scouting directives для ScoutEngine (cross-facet связи, приоритеты обогащения) |
| `Gate` | Completeness check (L1 structural + L2 Domain Critic) |
| `Enrich` | Генерация enrichment задач при неполноте; управление циклом (≤ 2, budget) |
| `Assemble` | Сборка DomainAnswerSnapshot (детерминированная проекция + structured LLM вызов) |
| `Publish` | Публикация DomainAnswerSnapshot (данные для Domain Overlay + graph highlight IDs) |
| _(future)_ `Scenario` | Сценарный what-if анализ поверх собранного контракта |
| _(future)_ `Compare` | Сравнительный анализ между facets или между доменами |
| _(future)_ `Watch` | Мониторинг изменений графа относительно доменного контракта |

**Взаимодействие с другими движками:**

```
User query
    │
    ▼
DomainContractEngine.Resolve()     ← NL → typed contract
    │
    ├──► ScoutEngine (steered)     ← domain context: cross-facet expectations
    │       │
    │       ▼
    │    Graph (fragments + edges)
    │
    ▼
DomainContractEngine.Gate()        ← structural + semantic completeness
    │
    ├──► (если неполно) ScoutEngine (enrichment, steered)
    │
    ▼
DomainContractEngine.Assemble()    ← deterministic projection + 1 LLM call
    │
    ▼
DomainContractEngine.Publish()     ← UI overlays
```

**Принципы расширяемости:**

1. Контракт типизирован (kebab-case: `supply-chain`, `regulatory-impact`, ...) — новые типы контрактов добавляются как конфигурации, а не как код.
2. Каждая фаза (Resolve/Steer/Gate/Assemble) — отдельный интерфейс, заменяемый/расширяемый.
3. DomainContractEngine не знает про Scout internals — взаимодействие через scouting directives (задачи).
4. Будущие возможности (Scenario/Compare/Watch) — новые фазы того же движка, работающие с уже собранным `DomainAnswerSnapshot`.

### R7. Инкрементальность — отложена

Пересборка `DomainAnswerSnapshot` при изменениях графа — **deferred**.

Когда потребуется:
- Ввести `assembledAt` + `staleAfter` в `DomainAnswerSnapshot`.
- При изменении узлов/рёбер, входящих в `critical_dependencies` или `impact_paths`, помечать снэпшот `stale`.
- Пересборка: lazy (при открытии пользователем) или eager (для top-зависимостей).
- Аналог `QueryRefreshJob` → `DomainAnswerRefreshJob`.
- Это естественная ответственность фазы `Watch` в DomainContractEngine.

### R8. Интеграция через ContractHandler — adapter pattern, не ломка pipeline

**Ограничение**: текущий `ContractHandler.Execute()` заточен под `ContractOutput` = `{ViewDirective, CreatedFragmentIDs, CreatedEdgeIDs}` (`contract.go:50`). Pipeline (`passes.go:57`) знает **только** как persist `ViewDirective` в `lens.ViewConfig`. `DomainAnswerSnapshot` — это принципиально другой артефакт (structured answer, не view instruction).

**Код**:

```go
// contract.go:22-26 — текущий интерфейс
type ContractHandler interface {
    ContractType() string
    Execute(ctx context.Context, eng Engine, in ContractInput) (*ContractOutput, error)
}

// contract.go:50-54 — output ограничен ViewDirective
type ContractOutput struct {
    ViewDirective      *models.ViewDirective
    CreatedFragmentIDs []string
    CreatedEdgeIDs     []string
}

// passes.go:57-58 — pipeline персистит только ViewDirective
if out != nil && out.ViewDirective != nil {
    if err := eng.PersistViewDirective(ctx, in.Job, out.ViewDirective); err != nil { ... }
}
```

**Почему нельзя расширять `ContractOutput`**:
- Добавить `DomainAnswerSnapshot` в `ContractOutput` = загрязнить контракт pipeline. Все handlers (ownership-structure, commonality-analysis) начнут знать про domain answers, хотя им это не нужно.
- `PersistViewDirective` персистит в `lens.ViewConfig` (key-value in lens doc). `DomainAnswerSnapshot` — тяжёлый structured doc, ему нужна **отдельная collection** (Firestore `domain_answer_snapshots/`).
- Pipeline не должен знать про lifecycle domain answer (Gate → Enrich → Assemble → Publish).

**Решение — adapter pattern**:

1. **`DomainContractEngine`** — отдельный пакет (`backend/internal/horizon/domain/`), самостоятельный orchestrator. Публикует `DomainAnswerSnapshot` в **свой repo** (Firestore `domain_answer_snapshots/{snapshotId}`).

2. **Новый `ContractHandler`** (e.g., `DomainAnswerHandler`) регистрируется в `ContractRegistry` для domain contractTypes (`supply-chain`, `regulatory-impact`, etc.). Внутри `Execute()`:
   - Вызывает `DomainContractEngine.Run(ctx, contract, graphSnapshot)`.
   - DomainContractEngine публикует snapshot в свой repo.
   - Handler **опционально** генерирует `ViewDirective` как backward-compat адаптер (highlight paths из snapshot, focus nodes, summary из thesis).
   - Возвращает `ContractOutput{ViewDirective: adapted}` → pipeline персистит как обычно.

3. **Pipeline не меняется**. `passes.go`, `ContractRegistry`, `Engine` interface — untouched. Для pipeline это обычный handler, возвращающий ViewDirective.

```
Pipeline (passes.go)
    │
    ▼
ContractRegistry.Get("supply-chain")
    │
    ▼
DomainAnswerHandler.Execute(ctx, eng, input)
    │
    ├──► DomainContractEngine.Run(...)          ← full lifecycle
    │       │                                      Resolve → Steer → Gate →
    │       │                                      Enrich → Assemble → Publish
    │       │
    │       ▼
    │    DomainAnswerSnapshot                   ← persisted in own repo
    │       │
    │       ▼
    ├──► adaptSnapshotToViewDirective(snapshot) ← backward compat
    │
    ▼
ContractOutput{ViewDirective: adapted}          ← pipeline sees normal output
```

**`adaptSnapshotToViewDirective`** — детерминированная функция:
- `HighlightPaths` ← из `snapshot.ImpactPaths` (canonical paths).
- `FocusNodePaths` ← из `snapshot.CriticalDependencies` top-K paths.
- `Annotations` ← из `snapshot.ConcentrationEvidence` (stake_pct, HHI, CR4).
- `Summary` ← из `snapshot.Thesis`.
- `FilterRelTypes` ← из `snapshot.ContractType` → default relation types.
- `ResearchPlan` ← из `snapshot.DataGaps` (if any).

**Что это даёт:**
- Фронт, который уже умеет ViewDirective → **работает без изменений** (highlights, focus, annotations).
- Новый фронт с Domain Overlay → читает `DomainAnswerSnapshot` через Cockpit API (`domainAnswers[]`).
- Миграция фронта — **инкрементальная**: сначала ViewDirective (уже есть), затем overlay (новый).

**domain_overlay.go — миграция:**
- `PreviewDomainOverlay` → делегирует `DomainContractEngine.Resolve()` (preview mode).
- `ExecuteDomainOverlay` → делегирует `DomainContractEngine.Run()`.
- Старые endpoints → thin wrappers, затем deprecated.

**TODO при реализации:**
- [ ] Создать `backend/internal/horizon/domain/` пакет.
- [ ] `DomainAnswerHandler` implements `pipeline.ContractHandler` — регистрируется в `contracts.registerBuiltinHandlers()` для каждого domain contractType.
- [ ] `DomainAnswerHandler.Execute()` → delegates to `DomainContractEngine.Run()`, adapts to ViewDirective.
- [ ] `adaptSnapshotToViewDirective()` — чистая функция snapshot → ViewDirective.
- [ ] `DomainAnswerSnapshot` repo — Firestore collection `domain_answer_snapshots/`, read by Cockpit API.
- [ ] `domain_overlay.go` endpoints → delegate to DomainContractEngine, then deprecate.

### R9. WS-события DomainContractEngine — realtime лог для фронта

**Контекст**: инфраструктура WS-событий полностью готова. Backend публикует через `ScoutEngine.publish()` → Pub/Sub (`nv_horizon.horizon.events`) → WS Hub → фронт (`horizonRealtimeStore.ts`). Для domain jobs уже есть события:

| Существующее событие | Файл |
|---------------------|------|
| `horizon.domain.research.started / .completed` | `engine_ext.go:1622, 1763` |
| `horizon.domain.base_enrichment.started / .completed` | `engine_ext.go:1824, 2146` |
| `horizon.domain.strategic_projection.started / .completed` | `engine_ext.go:2327, 2683` |

Фронт: **store** (`horizonRealtimeStore.ts:378-449`) парсит `horizon.domain.*` в `events[]` с иконками и human-readable текстом. Однако **`HorizonRealtimeCallouts.tsx:4-10`** фильтрует показ только для `node.upserted`, `edge.upserted`, `scout.job.state`, `scout.job.completed` — domain events сейчас **не попадают в callouts**. Они хранятся в store, но не визуализируются.

**При реализации**: расширить `isRelevant()` в `HorizonRealtimeCallouts.tsx` для `horizon.domain.contract.*`, либо (лучше) выводить domain contract events в отдельный timeline в Domain Overlay, а не в общие callouts.

**Новые WS-события для DomainContractEngine** (namespace `horizon.domain.contract.*`):

| Событие | Когда | humanReadable.title | Данные |
|---------|-------|---------------------|--------|
| `horizon.domain.contract.resolve.started` | Начало Resolve фазы | "Resolving domain contract" | `{contractType, facets[], query}` |
| `horizon.domain.contract.resolve.completed` | Resolve завершён | "Domain contract resolved: {contractType}" | `{contractType, facetCount, crossFacetExpectations[]}` |
| `horizon.domain.contract.steer.started` | Начало Steering | "Steering scouting for {contractType}" | `{wave: "anchor"|"stitch"|"gap", directiveCount}` |
| `horizon.domain.contract.steer.completed` | Steering завершён | "Steering complete: {N} directives" | `{directiveCount, wave}` |
| `horizon.domain.contract.gate.started` | Начало Gate check | "Checking completeness" | `{level: 1|2, cycle}` |
| `horizon.domain.contract.gate.passed` | Gate пройден | "Completeness check passed (L{level})" | `{level, score, checks[]}` |
| `horizon.domain.contract.gate.failed` | Gate не пройден | "Completeness gaps found (L{level})" | `{level, failedChecks[], enrichmentTasks[]}` |
| `horizon.domain.contract.enrich.started` | Начало enrichment цикла | "Enriching: {N} tasks" | `{cycle, taskCount, focusAreas[]}` |
| `horizon.domain.contract.enrich.completed` | Enrichment завершён | "Enrichment cycle {N} complete" | `{cycle, newNodes, newEdges}` |
| `horizon.domain.contract.assemble.started` | Начало сборки snapshot | "Assembling domain answer" | `{contractType}` |
| `horizon.domain.contract.assemble.completed` | Snapshot собран | "Domain answer ready: {thesis_short}" | `{snapshotId, readinessStatus, artifactCount}` |
| `horizon.domain.contract.publish.completed` | Snapshot опубликован | "Domain answer published" | `{snapshotId, contractType}` |
| `horizon.domain.contract.budget.warning` | Budget close to cap | "Budget warning: {pct}% used" | `{usedUSD, capUSD, usedTokens}` |
| `horizon.domain.contract.fallback` | Fallback на ready_with_gaps | "Answer ready with gaps" | `{gapCount, readinessStatus}` |

**Формат** — стандартный envelope (как существующие domain events):

```go
e.publish(ctx, realtime.EventType("horizon.domain.contract.gate.failed"), map[string]interface{}{
    "tenantId":      job.TenantID,
    "projectId":     job.ProjectID,
    "lensId":        job.LensID,
    "jobId":         job.ID,
    "canonicalPath": domainCP,
    "contractType":  contract.ContractType,
    "level":         1,
    "failedChecks":  []string{"inter_facet_connectivity", "market_metrics"},
    "humanReadable": map[string]interface{}{
        "title":       "Completeness gaps found (L1)",
        "description": "Missing inter-facet paths and market data; scheduling enrichment",
        "entityName":  contract.Title,
    },
})
```

**Фронт**:
- `horizonRealtimeStore.ts`: добавить обработку `horizon.domain.contract.*` — автоматически попадают в существующий `events[]` массив.
- Domain Overlay (expanded state): показывать **timeline лог** фаз DomainContractEngine для текущего домена — filtered по `canonicalPath`. Пользователь видит: "Resolving → Steering (wave 1) → Gate L1 failed → Enriching → Gate L1 passed → Gate L2 passed → Assembling → Published".
- Mini-state overlay: показывать текущую фазу как badge ("Steering...", "Enriching 2/2", "Ready").

**Публикация из DomainContractEngine**:
- `DomainContractEngine` получает `publisher` interface (не зависит от `ScoutEngine.publish()` напрямую).
- Interface: `type EventPublisher interface { Publish(ctx context.Context, eventType string, data map[string]interface{}) }`.
- ScoutEngine передаёт свой publisher при создании DomainContractEngine — reuse инфраструктуры без coupling.

**TODO при реализации**:
- [ ] Определить `domain.EventPublisher` interface в `backend/internal/horizon/domain/`.
- [ ] DomainContractEngine.Run() публикует events на каждой фазе.
- [ ] Фронт: добавить handler для `horizon.domain.contract.*` в `horizonRealtimeStore.ts` (парсинг + `events[]`).
- [ ] Решить: domain contract events → в `HorizonRealtimeCallouts` (расширить `isRelevant()`) **или** в отдельный timeline в Domain Overlay (предпочтительно — меньше шума в общих callouts).
- [ ] Domain Overlay (expanded): timeline лог фаз, filtered по `canonicalPath`.
- [ ] Domain Overlay (mini): badge текущей фазы.

## Продуктовое поведение (Frontend)

### Принцип: граф первичен, Domain Overlay — не агрессивный, сворачиваемый

Граф остаётся primary view на весь экран (как сейчас). Домены появляются как **лёгкий overlay поверх графа** — не sidebar, не отдельная панель. Overlay сворачивается в несколько уровней: от полного содержимого до мини-списка доменов и до полного скрытия.

Это инкрементальное улучшение: существующий UX не ломается, domain-функциональность — additive, ненавязчивая. В будущем можно расширить — но текущая форма минимально инвазивна.

### Три состояния overlay (сворачивание)

```
STATE 1: Collapsed (mini)               STATE 2: Domain list              STATE 3: Expanded (full)
Граф на весь экран                      Граф на весь экран                Граф на весь экран

┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────┐
│ ┌──┐                  │   │ ┌────────────────┐    │   │ ┌────────────────┐    │
│ │🔍│ ← плашка,        │   │ │ Supply Chain ● │    │   │ │ Supply Chain ● │    │
│ │2 │   клик раскрывает │   │ │ Regulatory   ○ │    │   │ │ Regulatory   ○ │    │
│ └──┘                  │   │ │                │    │   │ ├────────────────┤    │
│                       │   │ │ Ready ✓ 2 gaps │    │   │ │ Thesis: "Semi- │    │
│       GRAPH           │   │ └────────────────┘    │   │ │ conductor..."  │    │
│    (full screen)      │   │                       │   │ ├────────────────┤    │
│                       │   │       GRAPH           │   │ │ ▸ Top Deps (10)│    │
│                       │   │    (full screen)      │   │ │ ▸ Hotspots  (3)│    │
│                       │   │                       │   │ │ ▸ Regulatory(2)│    │
│                       │   │                       │   │ │ ▸ Gaps      (2)│    │
│                       │   │                       │   │ │ [Litho|Pack|In]│    │
│                       │   │                       │   │ └────────────────┘    │
│                       │   │                       │   │       GRAPH           │
└───────────────────────┘   └───────────────────────┘   └───────────────────────┘
```

**State 1 → 2**: клик по мини-плашке → раскрывается список доменов.
**State 2 → 3**: клик по конкретному домену → раскрывается содержимое (thesis, deps, etc.).
**State 3 → 2**: клик по заголовку домена или chevron → сворачивается обратно до списка.
**State 2 → 1**: клик по chevron или Esc → сворачивается до мини-плашки.
**Любой → 1**: Esc из любого состояния → мини.

Overlay **не сдвигает граф** — он рисуется поверх, с полупрозрачным фоном. Граф остаётся интерактивным за пределами overlay.

### State 1: Mini (collapsed)

Мини-плашка в углу графа (top-left или bottom-left):

| Элемент | Что показывает |
|---------|----------------|
| Иконка | Тип контракта (маленькая) |
| Badge count | Количество доменов ("2") |
| Клик | → State 2 |

Занимает ~40×40px. Не мешает графу. Если доменов нет — не показывается.

### State 2: Domain List

Компактная карточка (~200px шириной) поверх графа:

| Элемент | Источник | Interaction |
|---------|----------|-------------|
| Список доменов | `domainAnswers[]` | Каждый домен — строка с названием и readiness badge |
| Readiness badge | `readiness_status` | "Ready ✓" / "⚠ 2 gaps" |
| Клик по домену | → | State 3 (expanded для этого домена) |
| Collapse chevron | → | State 1 |

### State 3: Expanded (содержимое домена)

Карточка ~280-320px шириной, scrollable, поверх графа. Все секции — **collapsible accordion** (▸/▾):

**Секции (сверху вниз):**

#### Header (всегда видим)

| Элемент | Источник |
|---------|----------|
| Domain title | `domain_contract.title` |
| Readiness + gap count | `readiness_status`, `len(data_gaps)` |
| Back chevron | → State 2 (к списку доменов) |

#### ▸ Thesis (раскрыт по умолчанию)

| Элемент | Источник | Зачем для CxO |
|---------|----------|---------------|
| Thesis text | `thesis` | Ответ на вопрос — 2-3 предложения |
| Confidence badge | `objective_check` + `readiness_status` | high/medium/low |

#### ▸ Top Dependencies

| Элемент | Источник | Interaction |
|---------|----------|-------------|
| Ranked list (1..10) | `critical_dependencies[]` | **Клик → граф подсвечивает transmission path** |
| Score bar | `TopDependencyScore` | ●●●●○ |
| Facet tag | `facet_key` | Цветной pill |
| ▶ Expand item | evidence, alternatives | Раскрыть: альтернативы, источники, метрики |

#### ▸ Concentration Hotspots

| Элемент | Источник | Interaction |
|---------|----------|-------------|
| Compact cards | `concentration_evidence[]` | "Taiwan: 67%", "China: 80% Ga" |
| Click | → | Подсветить geo-cluster в графе |

#### ▸ Regulatory & Control

| Элемент | Источник | Interaction |
|---------|----------|-------------|
| Alert-style items | `control_exposure[]` | Severity badge (red/yellow) |
| Click | → | Подсветить regulatory path в графе |

#### ▸ What We Don't Know

| Элемент | Источник |
|---------|----------|
| Gap list | `data_gaps[]` |
| "Enrich" button | `enrichment_hint` (для аналитика) |

#### Facet pills (нижняя полоса, всегда видима)

| Элемент | Interaction |
|---------|-------------|
| "All" (default) / "Litho" / "Packaging" / "Inputs" | Фильтрует все секции выше + подсвечивает узлы facet в графе |

### Граф: что меняется

Граф как компонент **не переписывается**. Добавляется **overlay-режим**, управляемый Domain Overlay:

| Состояние | Что показывает граф |
|-----------|--------------------|
| Overlay в State 1 (mini) / нет доменов | Как сейчас — полный граф |
| Домен выбран (State 3), ничего не кликнуто | Граф как есть, но domain-узлы визуально выделены (border/glow) |
| Кликнута dependency в overlay | **Transmission path highlighted**: узлы и рёбра на пути — яркие, остальные — muted (opacity 0.2) |
| Выбран facet | Узлы в scope facet'а — яркие, остальные — muted |
| Кликнут regulatory item | Regulatory path highlighted (REGULATES → CONTROLS_ACCESS → ...) |
| Esc / клик вне overlay | Вернуть граф в нормальный режим |

**Технически**: overlay — это массив `highlightedNodeIDs` + `highlightedEdgeIDs`, передаваемый в graph component. Не-highlighted узлы рисуются с пониженной opacity. Минимальное изменение в graph renderer.

### Состояния (edge cases)

| Состояние | UI поведение |
|-----------|-------------|
| Нет доменов в scope | Overlay не показывается. UI = как сейчас |
| Домены есть, но snapshots не собраны (job в процессе) | Mini-плашка с spinner |
| `readiness_status: "ready_with_gaps"` | Badge "⚠ 2 gaps" в State 2; gaps-секция раскрыта в State 3 |
| Все facets пусты | Thesis: "Insufficient data to answer", gaps доминируют в State 3 |
| Один домен | State 2 показывает один домен; клик → сразу State 3 |
| Default state при загрузке | State 1 (mini) — граф на весь экран, overlay не мешает |

## Что собираем как answer-артефакты

### Детерминированные (из графа, без LLM)

1. **`critical_dependencies[]`**
   - source/target/product context
   - `TopDependencyScore` (impact × exposure × concentration × (1 − substitutability) × sanction_export_control_exposure × confidence)
   - facet_key
   - evidence refs (edge IDs, source URLs)

2. **`critical_nodes[]`**
   - node id/canonical path
   - criticality (из highProfile signals + path centrality)
   - criticality_reason
   - confidence

3. **`concentration_evidence[]`**
   - market node path
   - TAM/CAGR/CR4/HHI (если есть) с единицами, периодом, источником
   - explicit `data_gap` при отсутствии

4. **`control_exposure[]`**
   - mechanism type (sanctions/export-control/regulatory)
   - affected dependencies/nodes (edge IDs)
   - severity и confidence
   - regulatory path (REGULATES → CONTROLS_ACCESS → USES_MECHANISM)

5. **`impact_paths[]`**
   - explainable transmission paths (ordered node/edge sequences)
   - path criticality score
   - facet_key
   - proof refs

6. **`data_gaps[]`**
   - gap type (missing_metric / empty_facet / no_alternatives / no_regulatory / low_confidence)
   - affected facet/dependency/node
   - enrichment hint (что нужно дособрать)

### LLM-генерируемые (один structured вызов)

7. **`thesis`**
   - structured short answer на исходный запрос
   - по facets (если применимо)

8. **`alternatives[]`** (feasibility оценка)
   - alternative supplier/path
   - feasibility assessment
   - capacity delta, latency delta, cost delta, time to switch
   - LLM confidence + reasoning

9. **`objective_check`**
   - ontology conformance
   - policy conformance
   - provenance/traceability quality
   - bias/completeness assessment

## Как это встроить в job (DomainContractEngine orchestration)

DomainContractEngine оркестрирует исполнение контракта внутри существующего job. Scout/Critic/Research/Policy пайплайн **не меняется** — DomainContractEngine стирит его и обрабатывает результат.

### 1. `Resolve` (DomainContractEngine)

Нормализация запроса в domain contract:
- Резолвим intent в тип контракта (kebab-case: `supply-chain`, `regulatory-impact`, и др.).
- Определяем facets из запроса (scope-фильтры по canonical paths).
- Определяем temporal horizon и polarity.
- Генерируем **cross-facet expectations** (ожидаемые типы связей между facets).
- Формируем `QueryAddress` для scope.
- Создаём domain fragment с edges `INVESTIGATES` к anchor-узлам.

### 2. `Steer` → `graph_collect` (DomainContractEngine → ScoutEngine)

См. подробное описание в секции **"Steering (детальный дизайн)"** ниже.

### 3. `Gate` — Level 1 (DomainContractEngine, детерминированный)

Набор автопроверок без LLM: универсальные (для любого контракта) + специфичные для типа контракта + специфичные для типов facets. Каждая проверка возвращает `pass | fail | partial` + gap description + enrichment hint + приоритет.

См. подробное описание в секции **"Completeness Gate (детальный дизайн)"** ниже.

Если не пройден → `Enrich` (задачи генерируются детерминированно из gap descriptions).

### 4. `Gate` — Level 2 (DomainContractEngine, Domain Critic)

Если Level 1 пройден:
- Старшая модель получает описание графа + исходный запрос + domain contract.
- Оценивает семантическую полноту графа относительно запроса.
- Если не пройден → `Enrich` (задачи из LLM enrichment signals).

### 5. `Enrich` (DomainContractEngine → ScoutEngine)

- Запускается в том же job.
- DomainContractEngine генерирует enrichment directives (по паттерну Critic: конкретные missingInfo-вопросы).
- **Приоритет**: cross-facet connectivity gaps > intra-facet gaps > metric gaps.
- ScoutEngine исполняет как обычные scouting задачи.
- ≤ 2 цикла, budget cap, fallback `ready_with_gaps`.

### 6. `Assemble` (DomainContractEngine)

Финальная сборка `DomainAnswerSnapshot`:
1. Детерминированная проекция из графа (scoring, BFS/Dijkstra, агрегация метрик, gap detection, facet tagging).
2. Один structured LLM вызов (thesis, alternatives feasibility, objective check).

### 7. `Publish` (DomainContractEngine)

Публикация UI-слоев:
- Сохранение `DomainAnswerSnapshot` в Firestore.
- Генерация `highlightedNodeIDs` / `highlightedEdgeIDs` для каждого артефакта (dependencies, paths, facets).
- Данные готовы для Domain Overlay (фронт читает из Cockpit API).

## Steering (детальный дизайн)

Steering — ключевая фаза DomainContractEngine, которая обеспечивает **полноту графа** для ответа на вопрос. Без steering Scout работает "от сущности наружу" — обогащает каждый узел изолированно. Steering превращает набор изолированных scouting задач в **целенаправленное исследование**, ориентированное на контракт.

### Принцип

DomainContractEngine **не меняет Scout внутри**. Steering формулирует директивы через существующие параметры `ScoutingJob.State`:

| Параметр Scout | Как использует Steering |
|----------------|------------------------|
| `focus` | Domain-aware фокус: что именно искать для этого узла в контексте контракта |
| `specialties` | Какие модули Scout включить (supply_chain, products_and_services, и др.) |
| `keywords` | Ключевые слова из контракта и cross-facet узлов для поиска связей |
| `missingInfo` | Конкретные вопросы для Research — главный рычаг cross-facet steering |
| `decompositionHints` | Подсказки для декомпозиции: какие под-сущности искать |
| `requestedMetrics` | Какие метрики запросить (TAM/CAGR/CR4/HHI для market-узлов) |
| `chainContext` | Supply chain контекст для LLM: куда смотреть в цепочке |
| `maxRecursionDepth` | Глубина рекурсии (больше для critical paths, меньше для периферии) |
| `maxChildrenPerJob` | Ширина обогащения (больше для anchor facets) |
| `originalQuery` | Исходный запрос пользователя — общий контекст для Scout/Critic/Research |
| `domains` | Domain contract — передаётся как есть для domain-aware промптов |

### Три волны Steering

Steering работает не как один пакет директив, а как **три последовательные волны**, каждая из которых использует результаты предыдущей.

#### Волна 1: Anchor Expansion (начальное обогащение)

**Цель**: создать базовый граф по каждому facet.

**Входы**: domain contract + facets.

**Что генерирует**:

Для каждого facet → набор ScoutingJob:

```
// Пример: facet "lithography_equipment"
ScoutingJob {
  targetCanonicalPath: "/org/nl/asml"
  state: {
    focus:          "EUV lithography equipment manufacturer, supply chain dependencies, market position"
    specialties:    ["supply_chain", "products_and_services"]
    keywords:       ["lithography", "EUV", "DUV", "semiconductor equipment"]
    requestedMetrics: ["TAM", "CAGR", "CR4", "HHI"]
    originalQuery:  "<исходный запрос>"
    domains:        [<domain contract>]
    maxRecursionDepth: 2
    maxChildrenPerJob: 80
  }
}
```

**Особенность**: на этом этапе cross-facet связи ещё не ищутся — сначала нужно построить базовый граф в каждом facet, чтобы знать какие узлы вообще существуют.

#### Волна 2: Cross-Facet Stitching (поиск связей между facets)

**Цель**: обнаружить и материализовать связи между узлами разных facets.

**Входы**: результат Волны 1 (узлы в каждом facet) + cross-facet expectations из Resolve.

**Логика**:

1. Для каждой пары facets взять cross-facet expectations (ожидаемые типы связей).
2. Для каждого "якорного" узла facet A сформировать задачу: "проверить связи с узлами facet B".

```
// Пример: TSMC (litho) ↔ critical_inputs
// Ожидание: CONSUMES/DEPENDS_ON между litho-узлами и raw materials
ScoutingJob {
  targetCanonicalPath: "/org/tw/tsmc"
  state: {
    focus:       "What critical raw materials does TSMC consume? Focus on: neon, gallium, germanium, photoresists, silicon wafers"
    specialties: ["supply_chain"]
    keywords:    ["neon", "gallium", "germanium", "photoresist", "silicon wafer",
                  "raw material", "critical input", "supply dependency"]
    missingInfo: [
      "What purified gases (neon, argon, fluorine) does TSMC use in EUV/DUV lithography?",
      "Who are TSMC's primary suppliers of photoresists and advanced chemicals?",
      "What is TSMC's dependency on specific grades of silicon wafers?"
    ]
    chainContext: "upstream: raw materials and critical inputs for semiconductor fabrication"
    maxRecursionDepth: 1      // не уходить глубоко — только прямые связи
    maxChildrenPerJob: 20     // меньше ширина, больше точность
  }
}
```

**Ключевой механизм**: `missingInfo` — это прямые вопросы, которые Research Agent будет искать в источниках. Они формулируются из cross-facet expectations и конкретных узлов другого facet.

**Как формулировать `missingInfo` для cross-facet**:

| Cross-facet пара | Ожидаемые связи | Пример missingInfo |
|-----------------|-----------------|-------------------|
| litho ↔ critical_inputs | CONSUMES, DEPENDS_ON | "What critical gases/chemicals does {org} use in {process}?" |
| litho ↔ packaging | SUPPLIES_TO, DEPENDS_ON | "Does {org} supply components to advanced packaging companies?" |
| any ↔ geo_regulation | LOCATED_IN, REGULATES | "In which jurisdictions does {org} operate? What export controls apply?" |
| supplier ↔ market | PARTICIPATES_IN, DOMINATES | "What is {org}'s market share in {market}? Who are competitors?" |

**Генерация keywords**: Steering берёт `displayName` и `canonicalPath` узлов из другого facet и добавляет их как keywords. Так Scout/Research при обогащении TSMC "видит" слова "neon", "gallium", "germanium" — и с большей вероятностью найдёт и зафиксирует связи.

#### Волна 3: Gap-Driven Enrichment (целевое дообогащение)

**Цель**: закрыть конкретные пробелы, выявленные Gate.

**Входы**: результат Gate (Level 1 и/или Level 2) — список gaps с приоритетами.

**Типы gaps и соответствующие директивы**:

| Gap тип | Приоритет | Directive |
|---------|-----------|-----------|
| `inter_facet_disconnect` | **P0** (critical) | Повторный cross-facet stitching с расширенными keywords и более конкретными missingInfo |
| `missing_critical_path` | **P0** | Обогатить промежуточные узлы на вероятном пути; увеличить maxRecursionDepth |
| `empty_facet` | **P1** | Новые anchor-узлы для facet; расширить scope; добавить decompositionHints |
| `no_regulatory_edges` | **P1** | Добавить specialties: включить regulatory focus; missingInfo про sanctions/export controls |
| `missing_market_metrics` | **P2** | requestedMetrics: ["TAM", "CAGR", "CR4", "HHI"]; focus на market sizing |
| `no_alternatives` | **P2** | missingInfo: "Who are alternative suppliers of {product}? What is switching cost/time?" |
| `low_confidence` | **P3** | Дополнительный Research pass с другими источниками |

**Ограничения Волны 3**: ≤ 2 цикла, budget cap, exhaustion/cooldown для повторных missingInfo.

### Steering Profile по типу контракта

Разные типы доменных контрактов требуют разных стратегий steering. Тип контракта определяет **профиль steering** — набор defaults для волн.

| Тип контракта | Волна 1 фокус | Волна 2 приоритеты | Специфичные metrics |
|---------------|---------------|--------------------|--------------------|
| `supply-chain` (+ `value-chain`, `sourcing-map`, ...) | supply_chain, products | upstream/downstream CONSUMES/SUPPLIES_TO cross-facet | TAM, CAGR, CR4, HHI, throughput_share |
| `regulatory-impact` (+ `sanctions-exposure`, ...) | regulatory, geo | entity ↔ regulation REGULATES/CONTROLS_ACCESS | sanction_count, export_control_scope |
| `competitive-landscape` (+ `market-opportunity`, ...) | products, market | entity ↔ market PARTICIPATES_IN/DOMINATES | market_share, CR4, HHI, CAGR |
| `geographic-concentration` | geo, supply_chain | entity ↔ geo LOCATED_IN/OPERATES_IN | facility_count, capacity_share |

Новые типы контрактов → новые профили steering. Профиль — это конфигурация, а не код.

### Cross-Facet Expectations: как Resolve формирует ожидания

На этапе Resolve из facets и типа контракта генерируются `CrossFacetExpectation`:

```
CrossFacetExpectation {
  facet_a:           "lithography_equipment"
  facet_b:           "critical_inputs"
  expected_relations: ["CONSUMES", "DEPENDS_ON"]
  direction:         "a_to_b"        // litho потребляет inputs, не наоборот
  min_paths:         1               // минимум 1 path для полноты
  priority:          "critical"      // без этого нет explainability
  missingInfo_templates: [
    "What {expected_relations} does {node_a.displayName} have with {facet_b} entities?",
    "Does {node_a.displayName} use {node_b.displayName} in its operations?"
  ]
}
```

Волна 2 берёт эти expectations и для каждой пары (node_a, facet_b) генерирует конкретные `missingInfo`, подставляя реальные имена узлов.

### Пример полного Steering прогона (Semiconductor)

**Contract**: `supply-chain`, facets: litho, packaging, critical_inputs.

**Resolve output** — cross-facet expectations:
- litho ↔ critical_inputs: CONSUMES (critical)
- litho ↔ packaging: SUPPLIES_TO/DEPENDS_ON (high)
- critical_inputs ↔ geo: LOCATED_IN/EXTRACTS_FROM (high)
- all ↔ regulatory: REGULATES/CONTROLS_ACCESS (high)

**Волна 1** — 3 пакета jobs:
- litho: ASML, TSMC, Samsung, Tokyo Electron → specialties=supply_chain, focus=lithography
- packaging: ASE, Amkor, JCET → specialties=supply_chain, focus=advanced packaging
- critical_inputs: neon, gallium, germanium → specialties=supply_chain, focus=raw materials

**Волна 1 результат**: ~50 узлов, ~120 рёбер, но litho и critical_inputs **не связаны** (Scout обогащал их независимо).

**Волна 2** — cross-facet stitching:
- Для TSMC: missingInfo=["What purified gases does TSMC consume?", "What is TSMC's neon sourcing?"], keywords=["neon", "gallium", "germanium"]
- Для ASML: missingInfo=["What critical chemicals does ASML EUV require?"], keywords=["neon", "hydrogen", "tin"]
- Для neon suppliers: missingInfo=["Who are main customers of {supplier} purified neon?"], keywords=["TSMC", "Samsung", "Intel"]

**Волна 2 результат**: +15 cross-facet edges (TSMC→CONSUMES→Neon, ASML→DEPENDS_ON→tin droplet generator, etc.). Facets теперь связаны.

**Gate L1**: inter-facet connectivity ✓, но no_regulatory_edges для critical_inputs.

**Волна 3** (enrichment):
- Для neon/gallium/germanium: missingInfo=["What export controls apply to {material}?", "Which countries restrict export of {material}?"], specialties добавить regulatory focus.

**Волна 3 результат**: +5 regulatory edges (China→CONTROLS_ACCESS→gallium export, etc.)

**Gate L1**: ✓ pass → Gate L2 (Domain Critic) → ✓ pass → Assemble.

### Предполагаемая реализация (steering.go)

```
backend/internal/horizon/domain/
├── steering.go
│   ├── SteeringEngine              // оркестрация волн
│   ├── SteeringProfile             // конфигурация по типу контракта
│   ├── Wave1AnchorExpansion()      // генерация начальных jobs
│   ├── Wave2CrossFacetStitching()  // генерация cross-facet jobs
│   ├── Wave3GapDrivenEnrichment()  // генерация enrichment jobs из gap list
│   ├── BuildDirective()            // ScoutingJob.State из параметров
│   └── ResolveMissingInfo()        // шаблоны missingInfo → конкретные вопросы
├── steering_profiles.go
│   ├── SupplyChainProfile
│   ├── RegulatoryExposureProfile
│   ├── CompetitiveLandscapeProfile
│   └── GeographicConcentrationProfile
└── expectations.go
    ├── CrossFacetExpectation
    └── GenerateCrossFacetExpectations()  // contract type + facets → expectations
```

## Completeness Gate (детальный дизайн)

Gate — автоматическая оценка полноты графа относительно доменного контракта. Два уровня: Level 1 (детерминированный, без LLM) и Level 2 (Domain Critic, старшая модель).

Каждая проверка Level 1 возвращает:

```
CheckResult {
  check_id:        string            // e.g. "U3_inter_facet_connectivity"
  status:          "pass" | "fail" | "partial"
  priority:        "P0" | "P1" | "P2" | "P3"
  gap_description: string            // что именно отсутствует
  affected:        []string          // facet keys / node paths / edge IDs
  enrichment_hint: string            // что дособрать (передаётся в Enrich → Волна 3)
}
```

Решение Gate L1: `fail` если есть хотя бы один P0 check в статусе `fail`. `partial` если есть P1 fails. `pass` если все P0 и P1 — pass (P2/P3 fails допустимы → `ready_with_gaps`).

---

### Level 1 — Универсальные проверки (для любого типа контракта)

Применяются всегда, независимо от типа контракта.

#### U1. Facet Population

| | |
|---|---|
| **Что проверяет** | В каждом facet достаточно узлов для осмысленного ответа |
| **Критерий** | ≥ 3 узлов в scope каждого facet |
| **Приоритет** | **P0** если facet пуст (0 узлов); **P1** если 1-2 узла |
| **Enrichment hint** | "Expand facet '{key}': find more entities matching scope {scope}" |

#### U2. Facet Node Quality

| | |
|---|---|
| **Что проверяет** | Узлы в facets имеют минимальное качество по `EpistemicStatus` |
| **Критерий** | Weighted score ≥ 0.5 для каждого facet (`VERIFIED=1.0`, `LOW_CONFIDENCE=0.5`, `REJECTED=0.0`, empty=0.5) |
| **Приоритет** | **P1** |
| **Enrichment hint** | "Re-scout low-quality nodes: {paths}" |

#### U3. Inter-Facet Connectivity

| | |
|---|---|
| **Что проверяет** | Между facets есть пути — граф не распадается на изолированные кластеры |
| **Критерий** | Между каждой парой facets, для которых определены `CrossFacetExpectation`, есть ≥ 1 path длиной ≤ 5 hops |
| **Приоритет** | **P0** |
| **Enrichment hint** | "Cross-facet disconnect: {facet_a} ↔ {facet_b}. Run Wave 2 stitching with expanded keywords" |

#### U4. Critical Dependency Paths

| | |
|---|---|
| **Что проверяет** | Для top-K зависимостей (по `TopDependencyScore`) есть explainable paths |
| **Критерий** | Каждая из top-K (K = min(10, total_deps)) зависимостей имеет ≥ 1 transmission path |
| **Приоритет** | **P0** для top-3, **P1** для top-4..10 |
| **Enrichment hint** | "No explainable path for dependency {source} → {target}. Scout intermediate nodes" |

#### U5. Edge Direction & Policy

| | |
|---|---|
| **Что проверяет** | Рёбра, входящие в critical paths, имеют direction policy и policyStatus |
| **Критерий** | 0 рёбер в critical paths без `policyStatus`; 0 рёбер без direction |
| **Приоритет** | **P1** |
| **Enrichment hint** | "Edges without policy in critical paths: {edge_ids}. Re-run Policy check" |

#### U6. Overall Data Gap Ratio

| | |
|---|---|
| **Что проверяет** | Общая доля пробелов не делает ответ бессмысленным |
| **Критерий** | Доля `data_gap` артефактов < 70% |
| **Приоритет** | **P1** если 50-70%; **P0** если ≥ 70% |
| **Enrichment hint** | "Data gap ratio {ratio}% exceeds threshold. Prioritize: {top_gaps}" |

#### U7. Temporal Validity

| | |
|---|---|
| **Что проверяет** | Данные соответствуют temporal_horizon контракта |
| **Критерий** | ≥ 50% рёбер в critical paths имеют `ValidFrom`/`ValidTo`, пересекающийся с `temporal_horizon` контракта; рёбра с `ValidTo` < начало горизонта помечаются как stale |
| **Приоритет** | **P2** |
| **Enrichment hint** | "Stale edges (expired before {horizon_start}): {edge_ids}" |

#### U8. Source Diversity

| | |
|---|---|
| **Что проверяет** | Ответ не опирается на единственный источник |
| **Критерий** | Для top-K зависимостей: ≥ 2 уникальных source URL/ref в evidence |
| **Приоритет** | **P3** |
| **Enrichment hint** | "Single source for dependency {dep}: {source}. Corroborate with additional research" |

---

### Level 1 — Проверки по типу контракта

Каждый тип контракта активирует свой набор дополнительных проверок. Проверки определяются в Steering Profile.

#### Тип: `supply-chain` (profile: SupplyChain)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| SC1 | **Supply chain edges exist** | ≥ 1 edge типа SUPPLIES_TO/CONSUMES/DEPENDS_ON/PRODUCES на каждый facet | P0 |
| SC2 | **Upstream/downstream coverage** | Для каждого anchor-узла: есть ≥ 1 upstream edge (CONSUMES/DEPENDS_ON) И ≥ 1 downstream edge (SUPPLIES_TO/PRODUCES) | P1 |
| SC3 | **Market metrics** | Для каждого MARKET-узла в scope: TAM/CAGR/CR4/HHI заполнены или явный `data_gap` | P2 |
| SC4 | **Concentration evidence** | Хотя бы 1 MARKET-узел с CR4 или HHI (или proxy через counting PARTICIPATES_IN edges) | P1 |
| SC5 | **Substitution data** | Для top-K зависимостей: есть alternatives[] или explicit `data_gap: "no_alternatives"` | P2 |
| SC6 | **Geographic distribution** | Для critical_nodes: ≥ 80% имеют LOCATED_IN/OPERATES_IN edges | P2 |
| SC7 | **Throughput/capacity qualifiers** | Для top-K supply chain edges: `throughput_share` или `capacity_constraint` заполнены или data_gap | P3 |

#### Тип: `regulatory-impact` (profile: RegulatoryExposure)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| RE1 | **Regulatory edges exist** | ≥ 1 edge REGULATES/CONTROLS_ACCESS/USES_MECHANISM в графе | P0 |
| RE2 | **Decision points** | ≥ 1 DECISION_POINT или MECHANISM узел в scope | P0 |
| RE3 | **Jurisdiction coverage** | Для каждого facet: ≥ 1 GOV-узел (/gov/...) или regulatory ORGANIZATION связан | P1 |
| RE4 | **Affected entities mapped** | Каждый regulatory edge имеет target-узел с identified affected products/orgs | P1 |
| RE5 | **Mechanism specificity** | Для каждого REGULATES edge: есть qualifier `mechanism_type` (sanction/export_control/license/tariff) или data_gap | P2 |
| RE6 | **Temporal scope** | regulatory edges имеют ValidFrom (дата вступления в силу) | P2 |

#### Тип: `competitive-landscape` (profile: CompetitiveLandscape)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| CL1 | **Market participation** | ≥ 1 MARKET-узел в scope с ≥ 3 PARTICIPATES_IN edges (от разных организаций) | P0 |
| CL2 | **Market share data** | Для primary market: CR4 организации идентифицированы (≥ 4 PARTICIPATES_IN с `market_share` qualifier или data_gap) | P1 |
| CL3 | **Product overlap** | Для top-K конкурентов: есть общие PRODUCES → PRODUCT edges (пересечение по продуктам) | P2 |
| CL4 | **Concentration metrics** | CR4/HHI на primary market заполнены или data_gap | P1 |
| CL5 | **Revenue/size data** | Для top-K конкурентов: есть orgSize или revenue metric или data_gap | P3 |

#### Тип: `geographic-concentration` (profile: GeographicConcentration)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| GC1 | **Geographic edges** | ≥ 80% узлов в scope имеют LOCATED_IN/OPERATES_IN edges | P0 |
| GC2 | **Geo diversity** | ≥ 2 уникальных GEO_REGION узлов (иначе нет "concentration" для анализа) | P0 |
| GC3 | **Facility data** | Для FACILITY-узлов: есть capacity/throughput metrics или data_gap | P1 |
| GC4 | **Capacity share by geo** | Можно вычислить долю capacity по регионам (сумма capacity по geo > 0) | P2 |
| GC5 | **Risk exposure by geo** | Для каждого GEO_REGION: есть ≥ 1 risk signal или explicit "no known risk" | P2 |

---

### Level 1 — Проверки по типу facet

Дополнительно к проверкам контракта, каждый facet проверяется по типу сущностей, которые он содержит.

#### Facet содержит организации (`/org/...`)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| FO1 | **Products/services identified** | ≥ 1 PRODUCES edge на каждую org или data_gap | P1 |
| FO2 | **Key relationships** | ≥ 1 SUPPLIES_TO или CONSUMES edge на каждую org | P1 |
| FO3 | **Geographic presence** | ≥ 1 LOCATED_IN/OPERATES_IN edge на каждую org | P2 |
| FO4 | **highProfile computed** | `highProfile.signals` не пуст для ≥ 50% org-узлов | P3 |

#### Facet содержит продукты/материалы (`/product/...`)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| FP1 | **Producers identified** | ≥ 1 org с PRODUCES edge к каждому product | P1 |
| FP2 | **Consumers identified** | ≥ 1 org с CONSUMES edge к каждому product (если downstream контекст) | P2 |
| FP3 | **Market linked** | ≥ 1 MARKET-узел связан через PARTICIPATES_IN/HAS_SUBMARKET | P2 |
| FP4 | **Substitutes or gap** | Для критичных products: есть TRANSFORMS_INTO или alternative product или data_gap | P2 |

#### Facet содержит рынки (`/market/...`)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| FM1 | **Participants** | ≥ 3 PARTICIPATES_IN edges (org → market) | P1 |
| FM2 | **Metrics** | TAM или CAGR заполнены или data_gap | P2 |
| FM3 | **Concentration** | CR4 или HHI заполнены или data_gap | P2 |
| FM4 | **Submarket structure** | Если market крупный: ≥ 1 HAS_SUBMARKET edge или explicit "no submarkets" | P3 |

#### Facet содержит географию/deposits (`/loc/...`, `/deposit/...`)

| ID | Проверка | Критерий | Приоритет |
|----|----------|----------|-----------|
| FG1 | **Entities in geo** | ≥ 1 org/facility с LOCATED_IN/OPERATES_IN к каждому geo | P1 |
| FG2 | **Extraction links** | Для deposits: ≥ 1 EXTRACTS_FROM edge | P1 |
| FG3 | **Regulatory overlay** | Для geo: ≥ 1 GOV-узел или REGULATES edge или "no known regulation" | P2 |

---

### Level 1 — Логика агрегации

```
GateL1Result {
  overall_status:   "pass" | "fail" | "partial"
  total_checks:     int
  passed:           int
  failed:           int
  partial:          int

  // по приоритету
  p0_fails:         []CheckResult    // любой → overall fail
  p1_fails:         []CheckResult    // любой → overall partial
  p2_fails:         []CheckResult    // допустимы → ready_with_gaps
  p3_fails:         []CheckResult    // информационные

  // для Enrich
  enrichment_queue: []EnrichmentTask  // сортировано по priority
  estimated_cost:   float64           // прогноз USD на enrichment
}
```

Правила:
- **P0 fail** → Gate L1 `fail` → Enrich обязателен → повторить Gate после Enrich.
- **P1 fail, P0 pass** → Gate L1 `partial` → Enrich рекомендован → если budget позволяет, Enrich; иначе `ready_with_gaps`.
- **P0+P1 pass** → Gate L1 `pass` → переход к Gate L2 (Domain Critic). P2/P3 fails записываются в `data_gaps`.
- После ≤ 2 циклов Enrich: если P0 всё ещё fail → `ready_with_gaps` с critical warnings.

---

### Level 2 — Semantic completeness (Domain Critic)

Запускается **только если Level 1 pass** (все P0+P1 пройдены).

Старшая модель получает:
- Исходный запрос пользователя.
- Domain contract с facets.
- Сводку собранного графа (типы/количество узлов и рёбер, заполненность метрик).
- Результат Gate L1 (включая partial checks и P2/P3 fails).
- Перечень уже выявленных data_gaps.

Оценивает:
- Можно ли объективно ответить на исходный вопрос с имеющимся графом?
- Какие конкретные пробелы критичны для ответа (то, что детерминированные проверки не поймали)?
- Enrichment signals (конкретные задачи на дообогащение — по паттерну Critic missingInfo).

Примеры того, что ловит L2, но не L1:
- "В графе есть TSMC и neon, и связь между ними, но нет данных о масштабе зависимости (% от потребления) — ответ о systemic risk будет неполным."
- "Запрос спрашивает про opportunities, но все собранные данные — только про risks. Нет ни одного opportunity signal."
- "Facet advanced_packaging содержит только тайваньских производителей — geographic bias, нужны корейские/американские."

## Политика цикла дообогащения

Чтобы не уйти в бесконечный enrichment:

1. ≤ 2 циклов enrichment в рамках одного job.
2. Budget cap: USD лимит + output tokens лимит на enrichment.
3. Если полнота не достигнута: `readiness_status: "ready_with_gaps"` с явным перечнем пробелов.
4. Exhaustion/cooldown для конкретных missingInfo-вопросов (если данных нет в источниках — не переспрашивать).

## Пример (Semiconductor / Neon)

**Domain contract:**
```
type: "supply-chain"
facets: [{ key: "critical_inputs", scope: "/product/raw/**" }]
```

**В графе уже есть:**
- `TSMC -> CONSUMES -> Purified Neon Gas (99.999%)`

**Domain Answer Layer добавляет (детерминированно):**

1. `critical_dependencies[0]`:
   - source: TSMC, target: Purified Neon Gas, facet: "critical_inputs"
   - `TopDependencyScore`: 0.87 (high concentration × low substitutability × sanction exposure)
   - evidence: edge IDs, source URLs

2. `concentration_evidence[0]`:
   - market: `/market/industrial-gases/neon`
   - CR4: 0.92 (Ingas, Cryoin, Air Liquide, Linde) — или `data_gap: "CR4 not available"`
   - HHI: 3200+ (high concentration)

3. `control_exposure[0]`:
   - mechanism: "Russia/Ukraine conflict → neon supply disruption"
   - affected: edge `TSMC -> CONSUMES -> Neon`
   - severity: high, confidence: 0.8

4. `impact_paths[0]`:
   - path: `Ingas (Mariupol) → Purified Neon → TSMC → ASML EUV lithography → Advanced chips`
   - path criticality: 0.91

5. `data_gaps[0]`:
   - type: "no_alternatives"
   - affected: "Purified Neon Gas substitution options"
   - hint: "Find alternative neon suppliers or recycling technologies"

**LLM добавляет (один вызов):**

6. `thesis`: "Semiconductor supply chain has critical single-point dependencies on neon gas (>90% concentration in conflict zone) and EUV lithography (ASML monopoly). Top risk: neon supply disruption with 12-18 month recovery time."

7. `alternatives[0].feasibility`: "Air separation unit recycling at fab — feasible, 60% reduction in virgin neon demand, 18-month deployment, $50M+ capex per fab."

8. `objective_check`: { ontology_conformance: "pass", policy_conformance: "pass", bias_assessment: "geographic bias toward East Asia supply chain — Western alternatives underrepresented" }

## Контракт ответа

```
DomainAnswerSnapshot {
  // metadata
  domain_id:          string
  domain_contract:    DomainContract        // type + facets + temporal + polarity
  assembled_at:       timestamp
  readiness_status:   "ready" | "ready_with_gaps"
  enrichment_cycles:  int                   // 0..2

  // детерминированные артефакты
  critical_dependencies:  []CriticalDependency    // scored, facet-tagged
  critical_nodes:         []CriticalNode
  concentration_evidence: []ConcentrationEvidence
  control_exposure:       []ControlExposure
  impact_paths:           []ImpactPath
  data_gaps:              []DataGap

  // LLM-генерируемые артефакты
  thesis:                 Thesis                  // structured short answer
  alternatives:           []Alternative           // with feasibility
  objective_check:        ObjectiveCheck

  // для UI
  facet_summary:          map[facet_key] -> FacetSummary  // per-facet aggregation
}
```

## Связь с существующими компонентами

| Компонент | Связь с Domain Answer Layer |
|-----------|---------------------------|
| **`DomainContractEngine`** (новый) | **Центральный движок**. Resolve → Steer → Gate → Enrich → Assemble → Publish. Расширяемый: Scenario/Compare/Watch в будущем. |
| `DomainContract` (engine_ext.go) | Расширяется: добавляются facets, type, cross-facet expectations |
| `ScoutEngine` | **Не меняется внутри**. Получает scouting directives от DomainContractEngine (включая cross-facet задачи) |
| Critic/Research/Policy | **Не меняются**. Работают как часть Scout pipeline |
| `TopDependencyScore` (playbook) | Используется DomainContractEngine.Assemble() для scoring |
| `highProfile` (high_profile.go) | Используется для critical_nodes |
| `graph-query` endpoint | Эволюционирует в drill-down поверх DomainAnswerSnapshot |
| `Cockpit` API | Расширяется: `{ lens, graph, domainAnswer }` |
| `PropagationEngine` | impact_paths используют propagation paths |
| `PolicyChecker` | control_exposure строится из policy edges |

## Предполагаемая структура бэкенда

```
backend/internal/horizon/domain/
├── engine.go              // DomainContractEngine: orchestration (Resolve → Steer → Gate → Enrich → Assemble → Publish)
├── contract.go            // DomainContract, DomainContractType, Facet, CrossFacetExpectation
├── resolver.go            // Resolve: NL query → typed DomainContract
├── steering.go            // Steer: contract → scouting directives (включая cross-facet)
├── gate.go                // Gate: L1 structural + L2 Domain Critic
├── assembler.go           // Assemble: deterministic projection + structured LLM call
├── publisher.go           // Publish: UI overlays generation
└── snapshot.go            // DomainAnswerSnapshot, CriticalDependency, ImpactPath, etc.
```

## Definition of Done

1. `DomainContractEngine` реализован как самостоятельный движок (`backend/internal/horizon/domain/`), с чёткими фазами (Resolve/Steer/Gate/Enrich/Assemble/Publish) и расширяемой архитектурой.
2. Система отвечает структурированно из собранного графа (post-processing, без повторного research).
3. Для top-зависимостей доступны детерминированные доказательства (scores, paths, metrics), а не только текст.
4. Один structured LLM вызов генерирует thesis + feasibility + objective check.
5. Completeness Gate (L1 deterministic + L2 Domain Critic) работает; enrichment ≤ 2 цикла; inter-facet connectivity проверяется.
6. Cross-facet scouting directives генерируются DomainContractEngine и исполняются ScoutEngine.
7. Фронт показывает Domain Overlay — сворачиваемый контрол поверх графа (mini → domain list → expanded), управляющий подсветкой графа.
8. Граф объясняет ответ (path-centric), а не заменяет ответ.
9. Facets работают как навигация внутри единого ответа.
10. Пробелы данных явно видны пользователю (`data_gaps`, `ready_with_gaps`).
11. `graph-query` работает как drill-down поверх `DomainAnswerSnapshot`.
