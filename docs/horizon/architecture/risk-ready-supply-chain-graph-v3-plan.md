# Risk-Ready Supply Chain Graph v3

## Summary

Цель: сделать supply-chain pipeline product-aware, risk-ready и reuse-first. Типовые риски должны извлекаться из графа и derived operational layer детерминированно, а не рождаться в `Domain Answer`. Внутри каждого supply-chain job должен существовать явный внутренний `WorkingProductScope`: canonical product set, с которым работает весь job от ранних scout/research шагов до capability resolution, path assembly и финального ответа.

Ключевая модель:
- `Org` / `Fac` — реальные graph entities
- `Product` — taxonomy reference, не основной carrier operational metrics
- `WorkingProductScope` — internal per-job product memory, canonical set продуктов/материалов/компонентов для данного job
- `SupplyLink` — конкретизация поставки продукта по цепочке
- `Capability` — internal scoped object для `carrierRef × productRef`, не graph node, не UI node, не создается LLM, создается и обновляется алгоритмом
- `Path/Leg` — derived transport layer с route-specific semantics

Зафиксированные решения:
- Объем: `Full end-state`
- Observation model: `Separate layer`
- Refresh: `Hybrid`
- Evidence policy: `Mixed gates`
- Capability storage: `Internal scoped object`

## Implementation Changes

### 1. Data model

- Ввести internal scoped object `CapabilityProfile` keyed by `carrierRef + productRef`.
- Ввести internal per-job object `WorkingProductScope` keyed by `jobID`:
  - canonical product refs in scope
  - aliases/raw mentions
  - source of inclusion: query seed, user wording, discovered evidence, inferred BOM relation
  - inclusion status: `seeded | discovered | confirmed | rejected | stale_candidate`
  - confidence and provenance
- `CapabilityProfile` хранит reusable operational truth для `org × product` или `fac × product`.
- `Product` оставить taxonomy-centric:
  - class/family, regulatory/hazard defaults, broad semantic grouping
  - без vendor-specific operational metrics
- `SupplyLink` оставить как actual supply representation:
  - source/target may be `org` or `fac`
  - продукт может висеть на link/edge
  - link хранит contract/customer/destination-specific overrides
- Ввести separate observation entities:
  - `CapabilityObservation`
  - `SupplyLinkObservation`
  - `RouteObservation`
  - при необходимости `ProductScopeObservation` для discovered product additions inside a job
- Ввести derived records:
  - `SupplyLinkRiskProfile`
  - `BottleneckProfile`
  - `SubstitutionProfile`
  - `ControlExposureProfile`

### 2. Job product scope

- `WorkingProductScope` должен создаваться рано, сразу после intent/query normalization, до активного supply-chain research.
- Initial seeding:
  - `SupplyChainQuery.ProductFilter`
  - product mentions из исходного user/query text
  - domain-specific obvious anchors, если они уже уверенно выведены из intent stage
- `WorkingProductScope` должен быть единственным источником product memory внутри job:
  - prompts не должны вести собственные несогласованные product lists
  - capability resolution, supply-link assembly и risk logic читают products из этого scope
- Controlled expansion:
  - scout/research может предложить новый product/material/component
  - он сначала попадает как candidate addition с provenance/evidence
  - затем проходит canonicalization + dedup + inclusion gating
  - только потом становится частью canonical scope
- Controlled dedup:
  - один и тот же product не должен повторно появляться как независимая сущность job memory
  - все aliases и raw mentions привязываются к одному canonical product ref
- Exclusion logic:
  - найденные, но нерелевантные или слишком слабые candidates помечаются `rejected` или `stale_candidate`
  - они не должны снова активно подмешиваться в prompts без нового сильного evidence
- `WorkingProductScope` должен хранить:
  - `canonicalProducts`
  - `aliasesByProduct`
  - `discoveryReasons`
  - `coverageStatus`
  - `lastExpandedAt`
  - `lastValidatedAt`

### 3. Metric scopes, keys, and units

- Зафиксировать scopes:
  - `capability(org × product | fac × product)`
  - `supply_link`
  - `route_leg`
  - `path_derived`
- Установить precedence rules для effective metric resolution:
  1. `route_leg` / `path_derived`
  2. `supply_link`
  3. `fac × product capability`
  4. `org × product capability`
  5. taxonomy-level product defaults

Capability metrics:
- `annual_capacity`, `annual_output`
  - units: `gwh_per_year | mwh_per_year | metric_tons_per_year | kt_per_year | bpd | mbpd | mcf_per_day | mmcf_per_day | units_per_year | cells`
- `lead_time_days_baseline`, `lead_time_volatility_days`, `inventory_days_baseline`, `downtime_days_per_year`
  - units: `days`
- `on_time_delivery_baseline`, `yield_ratio`, `scrap_rate`, `quality_score`, `utilization_rate`, `technical_substitutability`, `regulatory_substitutability`
  - units: `ratio`
- `purity_percent`, `available_export_share`
  - units: `percent | ratio`
- `min_order_quantity`, `batch_size`
  - units: `metric_tons | kt | units | cells | barrels | mcf`
- `service_criticality`
  - unitless `0..1`

Supply link metrics:
- `volume`, `committed_capacity`
  - units: same flow-family as current supply-chain contract
- `share`, `allocated_capacity_share`, `technical_fit`, `regulatory_fit`, `fill_rate`, `rejection_rate`
  - units: `percent | ratio`
- `contract_value`, `switch_cost`
  - units: currency token, default allowlist `usd | eur | rub`, extensible
- `lead_time_days`, `customs_clearance_days`, `inventory_cover_days_at_destination`, `switch_time_days`, `payment_delay_days`
  - units: `days`
- `available_capacity`
  - units: same family as `volume`

Route/path metrics:
- `reroute_latency_delta_hours`, `recovery_latency_hours`
  - units: `hours`
- `reroute_cost_delta_pct`, `throughput_share`, `capacity_constraint`, `path_stress_score`, `path_fragility`, `path_rerouteability`, `path_zone_pressure`, `path_affected_pressure`
  - units: `percent | ratio` or unitless `0..1`

Qualifier-only fields:
- `qualification_required`
- `license_required`
- `incoterm`
- `packaging_mode`
- `purity_band`
- `service_contract_required`
- `approved_vendor_only`
- `dual_use_flag`

### 4. Pipeline lifecycle

Разбить pipeline на явные стадии:
1. `Intent / Query Normalization`
2. `Working Product Scope Seeding`
3. `Identity Materialization`
4. `Capability Resolution`
5. `Observation Persistence`
6. `Product Scope Expansion / Validation`
7. `Capability Rollup`
8. `SupplyLink Rollup`
9. `Route Binding`
10. `Stress / Bottleneck Computation`
11. `Derived Risk Primitive Computation`
12. `Propagation / Ranking`
13. `Answer Projection`

Правила:
- `Working Product Scope Seeding`:
  - собирает стартовый canonical product set
  - нормализует raw mentions и filters
  - фиксирует product memory до глубокого research
- `Capability Resolution`:
  - создает или обновляет internal `CapabilityProfile` algorithmically
  - работает только по product refs, разрешенным в `WorkingProductScope`
- `Observation Persistence`:
  - все новые facts/claims пишутся как time/source-specific observations
  - observations ссылаются на `ontology_facts`, `sourceIDs`, time window и confidence
- `Product Scope Expansion / Validation`:
  - новые найденные продукты сначала идут в candidate list
  - проходят canonicalization, dedup, inclusion gating
  - при успешной валидации добавляются в `WorkingProductScope`
  - инвалидируют downstream capability/link derivations только по соответствующему product
- `Rollup`:
  - current state на capability и supply link собирается из observations
  - хранить freshness watermarks, source diversity, disagreement/conflict metadata
- `Hybrid refresh`:
  - cheap deterministic updates on write
  - тяжелые recomputes event-driven
  - lazy recompute on read if profile stale or gaps exceed threshold

### 5. Search, reuse, and grounding

- Planner должен работать product-scope-first:
  1. читать `WorkingProductScope`
  2. искать existing capability observations / rollups по продуктам в scope
  3. искать supply-link observations / rollups
  4. искать route observations
  5. только потом запускать external search / grounding
- Scope-aware enrichment:
  - не запрашивать path-specific metric, если уже есть reusable capability-level truth
  - не искать “новые продукты” повторно, если они уже есть в canonical scope или явно rejected
  - link/route enrichment запускать только при реальной потребности
- Fast-path fact application расширить:
  - не только на fragment state
  - но и на `WorkingProductScope`, capability/supply-link rollups
- Новый запрос внутри одного job строит context из:
  - `WorkingProductScope`
  - capability rollups
  - supply-link rollups
  - derived profiles
  - path/leg snapshots
  - data gaps and gate status

### 6. Derived risk logic

- Ввести deterministic pattern library для classical risks:
  - sole-source lock-in
  - low substitutability
  - route bottleneck
  - customs friction
  - export-control / sanctions access risk
  - low inventory + long lead time vulnerability
  - service / repair dependency
- Каждый pattern опирается на capability/link/route scopes и `WorkingProductScope`:
  - risk extraction разрешен только для products in scope
  - findings должны явно указывать product ref или product cluster из текущего job scope
- `DomainAnswerSnapshot` перевести на consumption derived profiles.
- `Domain Answer` не должен вычислять первичную risk semantics, только проектировать already-derived findings в executive form.

### 7. Prompt contract changes

- Расширить `Research` prompts:
  - различать reusable `carrier × product` facts, link-specific facts и route-specific facts
  - получать явный `WorkingProductScope` в prompt context
  - разрешать model дополнять product scope только через candidate additions, а не через бесконтрольное размножение product list
- Расширить `Scout` extraction contract:
  - добавить scoped output sections:
    - `carrierProductObservations`
    - `linkObservations`
    - `routeObservations`
    - `productCandidates`
  - topology extraction держать отдельно от product-scope expansion
- Расширить `Critic` prompts:
  - валидировать правильность scope для метрик и qualifiers
  - валидировать, что найденный product candidate действительно новый, а не alias уже имеющегося product in scope
  - отбрасывать customer-/route-specific metrics, ошибочно попавшие в capability baseline
- Расширить `MetricsAgent`:
  - сделать metric refresh scope-aware
  - работать только по canonical products in scope
- `Answer` prompts:
  - читать `WorkingProductScope`, rollups и derived profiles
  - не добывать новые operational facts и не расширять product set на answer stage
- Общий prompt principle:
  - LLM возвращает facts/claims/evidence in scoped form
  - product memory, precedence, rollup, invalidation и identity creation остаются в deterministic layer

## Interfaces and Storage

- Новые internal repositories:
  - `WorkingProductScopeRepository`
  - `CapabilityProfilesRepository`
  - `CapabilityObservationsRepository`
  - `SupplyLinkObservationsRepository`
  - `RouteObservationsRepository`
  - `DerivedRiskProfilesRepository`
- Capability не имеет graph edges и не показывается в UI-графе.
- `WorkingProductScope` не является graph entity и не показывается в UI как node/list by default.
- Capability логически принадлежит `org/fac`, а `WorkingProductScope` логически принадлежит `job`; оба физически хранятся как отдельные internal scoped objects.
- Existing `ontology_facts` остаются evidentiary substrate; observations, product-scope additions и derived profiles должны ссылаться на факты, а не заменять их.
- Existing graph/cockpit APIs не ломать на первом этапе.
- Добавить dual-read:
  - сначала new product-scope/rollup layer
  - fallback на legacy `ProductFilter` + `fragment.State` / `SupplyLink.State`
- Добавить dual-write:
  - новые jobs пишут product-scope state и observations
  - legacy summaries обновляются как compatibility cache

## Test Plan

Unit tests:
- `WorkingProductScope` seeding from query/filter/raw mentions
- product canonicalization, alias merge, candidate rejection
- capability keying and precedence resolution
- observation merge/rollup with conflicting sources and time windows
- effective metric resolver across capability/link/route scopes
- metric unit normalization and family compatibility
- prompt-output scope validation
- deterministic risk pattern evaluation
- freshness/invalidation logic

Integration tests:
- org-to-org supply link with product on link and no fac identified
- fac-to-fac supply with fac capability override over org capability
- same product supplied to multiple customers with shared capability baseline and distinct link overrides
- newly discovered product added mid-job and only relevant downstream capability/link computations invalidated
- repeated research step does not duplicate products already present in `WorkingProductScope`
- path recompute after route observation change without unnecessary capability recompute
- fallback from new rollup layer to legacy state
- prompt-generated scoped observations normalize correctly into internal storage

E2E scenarios:
- lithium compound from one supplier to multiple downstream plants with shared baseline metrics and one shared product in job scope
- supply-chain query seeded with broad product family, later narrowed by discovered canonical products without duplication
- sanctioned or export-controlled spare part on otherwise healthy route
- maintenance-critical spare part with customer-specific qualification and route delay
- chokepoint-exposed route with low rerouteability and customs friction

Acceptance criteria:
- внутри job существует единый canonical `WorkingProductScope`
- reusable actor-product metrics не копируются как primary truth в каждый path
- supply-link и route-specific semantics дают distinct conclusions where needed
- follow-up stages внутри job переиспользуют stored product/link/route truth before new enrichment
- prompts получают canonical product memory и не размножают product list
- critical findings traceable to observations/facts and pass mixed evidence gates

## Assumptions and Defaults

- `SupplyLink` may remain `org -> org`, `org -> fac`, `fac -> org`, or `fac -> fac`; fac is not required.
- `Product` remains taxonomy-like and is not the main carrier of operational metrics.
- `WorkingProductScope` — внутренний per-job объект памяти, а не пользовательская graph entity.
- Capability — internal algorithmic carrier, not a user-facing graph entity.
- Capability identity is never authored by LLM; LLM only contributes evidence/claims that are normalized into observations.
- Product additions by LLM/research are always candidate-only until deterministic canonicalization and inclusion gating succeed.
- Derived path/leg snapshots are caches for reads and explanations, not the canonical truth layer.
- Mixed evidence policy:
  - critical findings require stronger corroboration and coverage
  - secondary findings may be emitted best-effort with explicit gaps
- Migration is additive, with backfill from existing `ProductFilter`, `SupplyLink.State` and legacy fragment metrics into initial product-scope state and observations.
