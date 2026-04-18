# Supply-chain relation endpoint kinds

Онтологический контракт канонизатора AgentHorde: **каждая признаваемая пара endpoint-классов для каждой relation имеет явный семантический лейбл `kind`**. Этот документ — источник правды для таблицы `RELATION_ENDPOINT_KINDS` в [agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py](../../../agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py).

## Зачем это

Одно и то же утверждение — «Saudi Aramco supplies crude oil to China via Malacca» — честно читается на разных уровнях абстракции:

- **Physical** — какой site-to-site leg цепочки (Ras Tanura → Qingdao).
- **Commercial** — какой seller-buyer контракт (Saudi Aramco → Unipec).
- **Aggregate** — какой макро-trade flow (Saudi Arabia → China).
- **Actor-level** — какой актор обслуживает какой рынок/регион/коридор (Saudi Aramco → East Asia market / Malacca corridor).
- **Service** — какой carrier обеспечивает какой хаб (Maersk → Singapore).

Это **разные проекции одной связи**. Ранняя версия канонизатора распознавала только physical-срез (`supplies: site→site`) и выбрасывала все остальные в `relation_errors`. Это ломало представление supply chains, которые в источниках чаще формулируются на actor- или aggregate-уровне, чем в терминах конкретных терминалов.

Вместо того чтобы (a) наивно расширять whitelist и терять информацию о различиях срезов, или (b) заставлять агентов делать type inference в процессе рассуждения (нарушает сепарацию «агент рассуждает — канонизатор типизирует»), вводим **явный каталог признаваемых проекций** с трёхуровневой семантикой распознавания.

## Принцип

1. Одна relation в закрытом словаре — **N признанных проекций**, каждая = пара `(from_class, to_class)` + уникальный `kind`-лейбл.
2. `kind` — короткое snake_case имя проекции (`physical_leg`, `carrier_hub`, `actor_to_market`, `trade_flow`, ...). Оно осмысленно без ссылки на relation (в контексте `supplies` — очевидно, о чём речь).
3. Канонизатор проставляет `kind` как значение `CanonicalEdge.attributes.kind` во время phase 4. Downstream-запросы могут фильтровать по проекции: `supplies WHERE kind='physical_leg'` vs `supplies WHERE kind IN ('commercial','actor_to_market')`.

## Три уровня распознавания

Валидация теперь не бинарная (ok / error), а троичная:

| уровень | условие | что делаем | куда пишем |
|---------|---------|------------|------------|
| **recognized** | `(from, to)` есть в `RELATION_ENDPOINT_KINDS[relation]` | ребро приземляется; `attributes.kind = <label>` | — |
| **unspecified** | `(from, to)` нет ни в kinds, ни в blacklist | ребро приземляется; `attributes.kind = "unspecified"` | `ValidationReport.relation_kind_warnings` + `candidates.unknown_relations (reason="endpoint_pair_not_in_catalog")` |
| **forbidden** | `(from, to)` в `RELATION_ENDPOINT_BLACKLIST[relation]` | ребро **не создаётся** | `ValidationReport.relation_errors` + `candidates.unknown_relations (reason="forbidden_endpoint_pair")` |

Hard rejection только для явно семантически бессмысленных пар (`supplies person→product`, `charters site→org`). Всё остальное либо распознаётся как конкретная проекция, либо сохраняется с меткой unspecified — граф никогда не теряет факт молча.

Агентские промпты про это ничего не знают; type-матчинг полностью на стороне канонизатора.

## Каталог проекций

Ниже — полный каталог `(relation, from_class, to_class, kind)` для всех relation закрытого словаря. Порядок групп соответствует §1–§5 из `README.md`.

### §1. Material flow / topology

#### `supplies`

| from → to | kind | значение реального мира |
|-----------|------|-------------------------|
| `site → site` | `physical_leg` | физическая нога цепочки: отгрузка с площадки A на площадку B, кладётся в маршрут |
| `org → site` | `seller_to_destination` | продавец/mer chaнт/трейдер доставляет груз на конкретную площадку покупателя |
| `org → org` | `commercial` | коммерческий контракт seller→buyer без привязки к конкретным площадкам (offtake, SPA, framework supply) |
| `org → geo` | `actor_to_region` | актор поставляет в регион/страну — aggregate actor-level (Saudi Aramco supplies China) |
| `org → market` | `actor_to_market` | актор работает на торговую площадку/хаб (Vitol supplies Rotterdam bunker market) |
| `org → route` | `actor_via_corridor` | актор оперирует через конкретный коридор (Unipec ships via Malacca) — генерализация выше physical_leg |
| `site → geo` | `site_to_region` | конкретная площадка снабжает регион (Ras Tanura supplies East Asia) |
| `site → market` | `site_to_market` | площадка → рынок (Jamnathtown refinery supplies Singapore gasoline hub) |
| `geo → geo` | `trade_flow` | макро-trade flow между странами/регионами (Australia → China iron ore) |
| `geo → market` | `region_to_market` | страна → торговая площадка (Brazil → LME iron ore) |

Blacklist: `person→*`, `*→person`, `vessel_class→*`, `*→vessel_class`, `product→*`, `*→product`, `regime→*`, `*→regime`, `project→*`, `*→project`, `evidence→*`, `*→evidence`.

Атрибуты на всех проекциях одинаковые: `attributes.product` (путь к продукт-узлу) — mandatory; `attributes.incoterm` — optional; `attributes.kind` — proставляется канонизатором.

#### `leg_connects`

| from → to | kind | значение |
|-----------|------|----------|
| `site → site` | `site_to_site` | две площадки соединены ногой перевозки (mode — атрибут) |
| `site → route` | `site_to_route` | площадка привязана к коридору (порт — точка входа) |
| `route → site` | `route_to_site` | коридор приходит в конкретную площадку |
| `route → geo` | `route_to_region` | коридор заканчивается в регионе/стране |
| `site → geo` | `site_to_region` | площадка — точка входа в регион |

Blacklist: `org→*`, `*→org`, `person→*`, `product→*`, `regime→*`, `market→*` (leg — это чисто геометрия пути, не коммерция).

#### `transships_at`

| from → to | kind | значение |
|-----------|------|----------|
| `route → site` | `route_hub` | коридор транзитом через хаб |
| `route → geo` | `route_region` | коридор транзитом через регион (coverage coarse) |
| `org → site` | `carrier_hub` | carrier использует хаб для перевалки (Maersk transships at Singapore) |
| `site → site` | `hub_cascade` | перевалка между двумя площадками в одном узле |

Blacklist: `person→*`, `vessel_class→*`, `product→*`, `regime→*`.

#### `passes_through`

| from → to | kind | значение |
|-----------|------|----------|
| `route → geo` | `route_over_region` | маршрут проходит через регион/choke point-страну |
| `route → site` | `route_over_site` | маршрут проходит через конкретный узел (пролив/канал/шлюз, моделируемый как site) |
| `route → route` | `route_over_corridor` | маршрут использует внутренний коридор как часть пути |
| `vessel_class → route` | `class_over_corridor` | класс судов проходит через коридор (VLCC → Malacca) |
| `vessel_class → site` | `class_over_site` | класс судов способен пройти конкретную точку |
| `vessel_class → geo` | `class_over_region` | класс судов в регионе |
| `site → route` | `chokepoint_on_corridor` | конкретный узел-choke point (пролив/канал/шлюз, моделируемый как site) лежит на коридоре (Bosporus → Black Sea corridor, Suez Canal → Med→Red Sea) |

Blacklist: `person→*`, `org→*`, `product→*`, `regime→*`.

#### `alternate_to`

| from → to | kind | значение |
|-----------|------|----------|
| `route → route` | `route_alt` | сценарная альтернатива маршрута (тот же O–D, другой путь) |
| `site → site` | `site_alt` | альтернативный хаб / терминал |
| `vessel_class → vessel_class` | `fleet_alt` | альтернативный класс судов для той же задачи (VLCC ↔ Suezmax) |

Blacklist: смешанные классы (`route ↔ site`, `route ↔ vessel_class`, и т.д.) — разные по природе сущности, не alternatives друг другу; попадают в `constrained_by` / `passes_through` / `charters`.

### §2. Catalog

#### `produces`

| from → to | kind | значение |
|-----------|------|----------|
| `org → product` | `org_produces_product_line` | организация выпускает класс-левел продукт |

Blacklist: всё остальное.

### §3. Actors / operations

#### `operates`

| from → to | kind | значение |
|-----------|------|----------|
| `org → site` | `site_operator` | организация оперирует site (terminal operator, concessionaire, plant operator) |

Blacklist: `org→org` (это `managed_by` или `subsidiary_of`), `person→site` (физик не оперирует напрямую), остальное.

#### `charters`

| from → to | kind | значение |
|-----------|------|----------|
| `org → vessel_class` | `fleet_charter` | чартер на класс судов (time / voyage / COA — на атрибутах) |

Специфический single-vessel charter пока моделируется через этот же kind (vessel-level узлов нет в таксономии). Если/когда появится `vessel`-класс — добавится `org → vessel` = `vessel_charter`.

Blacklist: всё остальное.

#### `regulates`

| from → to | kind | значение |
|-----------|------|----------|
| `org → geo` | `regulates_region` | регулятор с юрисдикцией над регионом |
| `org → site` | `regulates_site` | регулятор с юрисдикцией над конкретным объектом |
| `org → route` | `regulates_corridor` | регулятор корридора/пролива |
| `org → market` | `regulates_market` | регулятор торговой площадки / биржи |

Blacklist: всё, где from ≠ org.

### §4. Regimes / risks

#### `constrained_by`

| from → to | kind | значение |
|-----------|------|----------|
| `site → geo` | `site_geo_rule` | площадка ограничена гео-правилом (правила порта, национальные нормы) |
| `vessel_class → geo` | `class_geo_rule` | класс судов ограничен региональным правилом |
| `vessel_class → route` | `class_over_route_draft` | осадочное / габаритное ограничение для прохода коридора |
| `vessel_class → site` | `class_over_site_draft` | осадочное / причальное ограничение в site |
| `route → route` | `route_over_corridor` | коридор зажат choke-point'ом другого коридора |
| `route → geo` | `route_over_region` | коридор ограничен региональным правилом |
| `site → route` | `site_over_corridor` | доступность площадки лимитирована коридором |

Blacklist: `org→*`, `person→*`, `product→*` (ограничение не накладывается на актора — для этого есть `subject_to`).

#### `requires_capability`

| from → to | kind | значение |
|-----------|------|----------|
| `product → site` | `product_needs_capability` | тип груза требует оборудования терминала (LNG регас, deep draft, reefer) |

Blacklist: остальное.

#### `subject_to`

| any → regime | kind | значение |
|--------------|------|----------|
| `org → regime` | `org_subject_to` | актор под нормативным режимом |
| `site → regime` | `site_subject_to` | площадка под нормативным режимом |
| `route → regime` | `route_subject_to` | коридор под режимом |
| `vessel_class → regime` | `class_subject_to` | класс судов под режимом |
| `product → regime` | `product_subject_to` | продукт под режимом |
| `geo → regime` | `region_subject_to` | регион под режимом |
| `market → regime` | `market_subject_to` | рынок под режимом |

Blacklist: всё, где `to ≠ regime`. `person → regime` — допускаем (`person_subject_to`), пригодится для governance (sanctions на физика).

#### `exposes_to`

Симметрично `subject_to`: any → regime. Семантически: `subject_to` — формальная подчинённость нормативу; `exposes_to` — фактический риск-экспозишн. Тот же набор from-классов, те же kinds с суффиксом `_exposes` (или просто `_exposed`).

| from → to | kind |
|-----------|------|
| `org → regime` | `org_exposed` |
| `site → regime` | `site_exposed` |
| `route → regime` | `route_exposed` |
| `vessel_class → regime` | `class_exposed` |
| `product → regime` | `product_exposed` |
| `geo → regime` | `region_exposed` |
| `market → regime` | `market_exposed` |
| `person → regime` | `person_exposed` |

Blacklist: всё, где `to ≠ regime`.

#### `mitigated_by`

| from → to | kind | значение |
|-----------|------|----------|
| `regime → route` | `regime_mitigated_by_route` | альтернативный маршрут снижает риск-режим |
| `regime → org` | `regime_mitigated_by_org` | актор (страховщик, регулятор, альянс) митигирует режим |
| `regime → regime` | `regime_chain` | один режим частично нейтрализует другой (режим страхования vs. war-risk) |

Blacklist: from ≠ regime (митигация — свойство режима, не объекта).

#### `priced_under`

| from → to | kind | значение |
|-----------|------|----------|
| `route → regime` | `route_incoterm` | маршрут / leg в инкотермах |
| `route → market` | `route_benchmark` | маршрут привязан к индексу рынка |
| `product → regime` | `product_incoterm` | продукт продаётся под инкотермом (crude oil CIF) |
| `product → market` | `product_benchmark` | продукт benchmark'ом к рыночному индексу |
| `site → regime` | `site_incoterm` | продажа с площадки под инкотермом |
| `site → market` | `site_benchmark` | привязка цены с площадки к рынку |

Blacklist: `org→*` (актор не «priced_under» — это атрибут его контрактов).

#### `benchmarked_to`

| from → to | kind | значение |
|-----------|------|----------|
| `product → market` | `product_benchmark` | продукт бенчмаркится к индексу (Brent, TTF) |
| `route → market` | `route_benchmark` | маршрут бенчмаркится к индексу |
| `site → market` | `site_benchmark` | площадка к индексу |

Blacklist: остальное.

#### `publishes_benchmark`

| from → to | kind | значение |
|-----------|------|----------|
| `org → market` | `price_reference_publisher` | Price Reporting Agency (Platts, Argus, OPIS, ICIS, FastMarkets, Bloomberg, Reuters/Refinitiv и т.д.) публикует референсную оценку для рынка/бенчмарка. Каталог-level fact: «кто печатает цифру, на которую другие contract'ы ссылаются через `benchmarked_to`». |

Blacklist: всё остальное (`from` обязан быть `org`, `to` обязан быть `market`).

Связь с `benchmarked_to`: пара «Urals `benchmarked_to` Dated Brent» и «Platts `publishes_benchmark` Dated Brent» вместе формируют полный pricing-anchor: *что* бенчмарк и *кто* его публикует. В рабочем графе оба ребра обычно присутствуют одновременно.

### §5. Ownership

#### `subsidiary_of`, `beneficially_owns`, `controls_via`, `holds_board_seat`, `joint_venture_with`, `listed_on`, `incorporated_in`, `managed_by`

Kinds (документирование существующего whitelist, без семантического расширения):

| relation | from → to | kind |
|----------|-----------|------|
| `subsidiary_of` | `org → org` | `subsidiary` |
| `beneficially_owns` | `person → org` | `ubo_stake` |
| `beneficially_owns` | `org → org` | `institutional_stake` |
| `controls_via` | `person → org` | `person_controls_org` |
| `controls_via` | `org → org` | `org_controls_org` |
| `holds_board_seat` | `person → org` | `director` |
| `holds_board_seat` | `org → org` | `nominee_org` |
| `joint_venture_with` | `org → org` | `jv` |
| `listed_on` | `org → market` | `listed` |
| `incorporated_in` | `org → geo` | `incorporated` |
| `managed_by` | `org → org` | `management_contract` |

Blacklist для всех: пары вне указанных (`person → person`, `site → org`, …).

### §6. Corporate events (flow)

| relation | from → to | kind |
|----------|-----------|------|
| `invested_in` | `org → org` | `equity_investment` |
| `acquired_stake_from` | `org → org` | `secondary_stake_acquisition` |
| `asset_contribution` | `org → org` | `asset_in_kind` |
| `merged_into` | `org → org` | `merger` |
| `spun_out` | `org → org` | `spinoff` |
| `divested_to` | `org → org` | `divestiture` |

Blacklist: пары вне org→org.

### §7. Evidence meta

`asserted_by`, `supported_by`, `contradicts`, `under_hypothesis` остаются в словаре ради обратной совместимости; в графе не эмитятся (см. § 5 README про `EvidenceArchive`). Поэтому kind-таблицы для них не поддерживаем — если где-то в истории появится такое ребро, validation просто не увидит и свалит в unspecified.

## Примеры чтения графа по проекциям

### Malacca (multi-projection)

Факт: «Saudi Aramco поставляет crude oil в Китай через Малаккский пролив; Unipec контрактует перевозку на VLCC; Maersk перегружает контейнеры в Сингапуре».

Проекции, которые канонизатор кладёт в граф:

```
supplies (trade_flow):           /geo/sa  → /geo/cn                     product=crude-oil
supplies (actor_to_region):      /org/sa/saudi-aramco → /geo/cn         product=crude-oil
supplies (commercial):           /org/sa/saudi-aramco → /org/cn/unipec  product=crude-oil incoterm=CIF
supplies (actor_via_corridor):   /org/cn/unipec → /route/na/malacca     product=crude-oil
charters (fleet_charter):        /org/cn/unipec → /vessel_class/na/vlcc
passes_through (class_over_corridor): /vessel_class/na/vlcc → /route/na/malacca
transships_at (carrier_hub):     /org/dk/maersk → /site/sg/singapore
transships_at (route_hub):       /route/na/malacca → /site/sg/singapore
```

Восемь рёбер описывают один и тот же реальный поток на четырёх уровнях (aggregate / actor / physical / service) — граф мульти-проекционный, но каждая проекция имеет явный kind и читается отдельно.

### Tesla governance

Факт: «The Vanguard Group holds 7.1% of Tesla».

```
beneficially_owns (institutional_stake): /org/us/vanguard → /org/us/tesla-inc  stake_pct=7.1
```

Один kind, одна пара классов. Никакой многозначности.

## Контракт между канонизатором и апстримом

- **Агент** выдаёт свободные `proposedRelations` в terms of closed-vocab relation names и surface-label endpoints. Тип endpoint'а не указывает.
- **Канонизатор phase 1–3** резолвит surfaces → canonical entities → `fragment_class`.
- **Канонизатор phase 4** собирает `(fragment_class_from, fragment_class_to)` и смотрит в `RELATION_ENDPOINT_KINDS[relation]`:
  - recognized → ребро + `attributes.kind`.
  - unspecified → ребро + `attributes.kind = "unspecified"` + запись в `candidates.unknown_relations` + `ValidationReport.relation_kind_warnings`.
  - forbidden → ребро НЕ создаётся; запись в `candidates.unknown_relations` с `reason="forbidden_endpoint_pair"` + `ValidationReport.relation_errors`.
- **Downstream** (property graph writer, lens, query layer) может фильтровать по `attributes.kind` и строить многоплоскостные представления без изменения канонического слоя.

## Эволюция каталога

Расширять каталог — законная онтологическая работа: добавить строку в этот документ, синхронизировать с `RELATION_ENDPOINT_KINDS` в коде, добавить тест. Наоборот — сужать (перевести ранее recognized-пару в blacklist) — это breaking change для downstream и требует отдельного ADR / миграции.

Периодически стоит просматривать `candidates.unknown_relations` c `reason="endpoint_pair_not_in_catalog"` на свежих прогонах: если некая пара появляется устойчиво и даёт смысл — повысить её до recognized с kind-лейблом. Если стабильно мусорная — понизить в blacklist.
