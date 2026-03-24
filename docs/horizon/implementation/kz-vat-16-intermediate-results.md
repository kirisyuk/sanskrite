# Kazakhstan VAT 16% Intermediate Results

Дата промежуточного среза: `2026-03-23`.

Этот файл фиксирует первый рабочий результат после поднятия canonical hot-path для кейса `повышение НДС до 16% в Казахстане`, до следующего усиления agent policy layer.

## Что уже сделано

- поднят taxonomy-safe hot-path для `policy_impact`
- добавлены canonical `decision`, `mechanism`, `program`, `market`, `community`, `gov` anchors
- seeded calibration assumptions на `/loc/kz`
- выполнен multi-run пакет по 4 осям
- отдельно сохранён управленческий readout для госоргана: `kz-vat-16-government-readout.md`

## Hot-Path Coverage

Первичный seed дал:

- `9` новых fragment nodes
- `2` обновлённых fragment nodes
- `21` новых dependency edges

Повторный idempotent прогон дал:

- `0` новых fragment nodes
- `11` обновлённых fragment nodes
- `0` новых edges
- `21` обновлённую связь

Это важный structural shift: сценарий ушёл от почти пустого `/loc/kz` baseline к явной регуляторной цепочке.

## Baseline Preview Delta

До hot-path baseline preview был почти пустым: фактически `1` узел и `1` путь.

После hot-path на живом trace:

- `seedFragmentIds = 11`
- `nodes_updated = 11`
- `paths_traced = 11`
- `propagationTimeMs ≈ 2089`

То есть теперь propagation реально проходит по policy graph, а не только по гео-якорю.

## Live Runs

### 1. Legal / Compliance

- `runId`: `Ivay3sEgAVKD1RnACfJ3`
- `bundleArtifactId`: `2O7QzORawsnnpjNqtBnV`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.573`
- `price_pressure = 0.426`
- `policy_pressure = 0.363`
- `narrative_volatility = 0.475`
- `supply_stress = 0.628`

Interpretation:

- государство и крупный бизнес быстро переходят в режим исполнения, объяснений и бюджетной адаптации
- ключевые механизмы: `delay_purchase`, `lobby`, `broadcast_message`, `buy`, `do_nothing`

### 2. Financial / Settlement

- `runId`: `HajoLY8KTBUbtkidBcXz`
- `bundleArtifactId`: `CBLWY8Kmyt3aVMxt8f93`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.550`
- `price_pressure = 0.539`
- `policy_pressure = 0.520`
- `narrative_volatility = 0.699`
- `supply_stress = 0.582`

Interpretation:

- главный эффект уходит в цену, оборотный капитал и публичное давление
- импортёры и розница оказываются в более жёстком режиме, чем это было видно в первом “тонком” baseline

### 3. Demand / Channel Shift

- `runId`: `MMR4D8FDaj0b4rrm6ksF`
- `bundleArtifactId`: `LR6WmmI7XvLrqGELNOC7`
- `status`: `DEGRADED`

Outcome distribution:

- `risk_score = 0.550`
- `price_pressure = 0.550`
- `policy_pressure = 0.520`
- `narrative_volatility = 0.680`
- `supply_stress = 0.590`

Review downgrade rationale:

- нулевая вариативность между мирами
- sensitivity по всем метрикам равна `0`
- действия агентов слишком generic для consumer-demand/regime-shift dynamics

Это не бесполезный run. Это точный указатель, где simulation layer ещё слаб: `consumer behavior`, `informal trade shift`, `shadow economy adaptation`.

### 4. Narrative / Lobbying

- `runId`: `q08vCHAhAcyVAMET8Tj6`
- `bundleArtifactId`: `KFqPEDbeM7VjmbA2YCOy`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.800`
- `price_pressure = 0.614`
- `policy_pressure = 0.511`
- `narrative_volatility = 0.671`
- `supply_stress = 0.835`

Interpretation:

- самый жёсткий контур сейчас не supply-chain breakdown, а политико-коммуникационная эскалация вокруг цены, справедливости и давления на регулятора

## C-Level Readout

### Что важно для руководителя

Основной риск в этом кейсе выглядит не как одномоментный экономический обвал, а как связка из четырёх волн:

1. рост цен и пересборка маржи
2. давление на ликвидность и оборотный капитал
3. лоббизм и публичное давление на дизайн/смягчение режима
4. частичный сдвиг спроса и транзакций в менее формальные каналы

### Кто реагирует первым

- Минфин и налоговое администрирование
- крупная розница
- импортная дистрибуция
- банки и контур working capital
- бизнес-ассоциации
- малый торговый сектор
- медиа

### Почему это значимо

Для C-level это означает, что вопрос не в том, “будет ли реакция”, а в том:

- где именно возникнет основной stress
- насколько быстро narrative перейдёт в policy pressure
- какой объём спроса и cashflow уйдёт в альтернативные каналы
- когда потребуется смягчение, компенсация или таргетированная коммуникация

## Усиленная итерация: scenario-aware agents

После первого среза был усилен именно agent layer:

- generic fallback population заменён на `scenario-aware` archetypes для `kzvat`
- увеличена и структурирована популяция агентов
- добавлены world-specific bootstrap adjustments
- добавлены axis-aware action plans для `compliance`, `settlement`, `demand`, `uncertainty`

Технически это привело к трём важным изменениям:

1. `demand-shift` перестал быть `DEGRADED`
2. sensitivity перестала быть нулевой
3. действия агентов стали доменно-специфичнее и ближе к C-level смыслу

### Усиленный live pack

#### 1. Legal / Compliance

- `runId`: `tbQN00rWkiqC6Kfemvs4`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.666`
- `price_pressure = 0.385`
- `policy_pressure = 0.569`
- `narrative_volatility = 0.533`
- `supply_stress = 0.751`

Key mechanisms:

- `do_nothing`
- `enforce_rule`
- `issue_statement`
- `broadcast_message`
- `delay_purchase`

Что изменилось:

- compliance contour стал заметно ближе к реальному регуляторному сценарию
- появились `enforce_rule` и `issue_statement`, а не только generic lobbying/consumer actions

#### 2. Financial / Settlement

- `runId`: `rs3OsX4GApwbhoY8HfgG`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.624`
- `price_pressure = 0.828`
- `policy_pressure = 0.180`
- `narrative_volatility = 0.404`
- `supply_stress = 0.500`

Key mechanisms:

- `adjust_budget`
- `delay_purchase`
- `do_nothing`
- `sell`
- `switch_supplier`

Что изменилось:

- financial run теперь гораздо лучше отделён от остальных осей
- вместо generic `buy/lobby` появились `adjust_budget`, `sell`, `switch_supplier`
- это делает контур понятным для CFO/COO уровня

#### 3. Demand / Channel Shift

- `runId`: `qmhro9Mi0Fe3hFcifAZv`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.614`
- `price_pressure = 0.908`
- `policy_pressure = 0.154`
- `narrative_volatility = 0.699`
- `supply_stress = 0.504`

Sensitivity:

- `price_pressure = 0.174`
- `supply_stress = 0.293`

Key mechanisms:

- `do_nothing`
- `switch_supplier`
- `adjust_budget`
- `buy`
- `delay_purchase`

Что изменилось:

- run больше не degraded
- появилась реальная world variance
- спросовой контур теперь показывает, что strongest commercial effect уходит именно в цену и перестройку покупки, а не только в narrative

#### 4. Narrative / Lobbying

- `runId`: `9QiC81gdwACtSrt3WtHA`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.972`
- `price_pressure = 0.108`
- `policy_pressure = 0.364`
- `narrative_volatility = 0.925`
- `supply_stress = 1.000`

Key mechanisms:

- `do_nothing`
- `broadcast_message`
- `delay_purchase`
- `issue_statement`
- `enforce_rule`

Что изменилось:

- narrative run стал чище как отдельная ось
- здесь now highest systemic risk and uncertainty profile
- это делает сценарий понятным для CEO/Public Affairs/GR слоя: информационное и политическое давление само по себе становится бизнес-фактором первого порядка

## Before / After

### Что улучшилось после усиления агентов

- `demand-shift`: было `DEGRADED`, стало `SUCCEEDED`
- `financial-settlement`: стало более CFO-реалистичным по action mix
- `legal-compliance`: появились реальные enforcement/state behaviors
- world variance выросла и sensitivity стала ненулевой

### Что это значит для C-level

После усиления модели картина стала более бизнес-полезной:

- **CEO / Country Manager**
  теперь видно, что тема может раскладываться в два независимых executive риска:
  1. коммерческий price/demand shock
  2. narrative/policy escalation

- **CFO**
  отдельный settlement contour теперь читается как pressure на `маржу`, `ликвидность`, `working capital`, `supplier mix`

- **COO / Commercial**
  demand contour показывает, что реакция населения и канала сбыта может быть не просто “меньше купят”, а “купят иначе, позже и в других каналах”

- **GR / Public Affairs**
  narrative contour показывает, что коммуникация и lobbying — не побочный шум, а один из основных драйверов итоговой траектории

## Что делаем дальше

Следующий шаг уже не в baseline graph, а в глубину agent cognition:

- добавить ещё более предметные archetypes для `importers`, `large retail`, `informal trade`, `banks`, `households`
- связать часть archetypes с реальными org nodes/lens scopes
- добавить richer action vocabulary для теневой экономики, channel substitution и price communication
- дожать C-level bundle synthesis в отдельный управленческий artefact

Этот документ остаётся промежуточным, но теперь он фиксирует уже две стадии: `working hot-path` и `strengthened agent layer`.

## Текущий agent roster

На текущем срезе `kzvat` hot-path уже использует не generic population, а широкий набор scenario-aware archetypes:

- `regulator`
- `retailer`
- `importer`
- `bank`
- `supplier`
- `wholesaler`
- `payment_rail`
- `regional_official`
- `logistics_operator`
- `warehouse_operator`
- `restaurant_operator`
- `tax_advisor`
- `municipal_buyer`
- `consumer_low_income`
- `consumer_mid_income`
- `consumer_high_income`
- `cross_border_shopper`
- `small_trader`
- `marketplace_seller`
- `ecommerce_platform`
- `association`
- `agri_producer`
- `fuel_distributor`
- `analyst`
- `media`
- `influencer`
- `opposition_politician`

Это уже даёт не одну “среднюю реакцию”, а несколько разных operating theatres:

- фискально-регуляторный
- commercial / retail / pricing
- import / logistics / warehouse
- liquidity / settlement / payment rails
- informal trade / cross-border substitution
- digital channel migration
- public narrative / lobbying / political escalation

## Почему это важно для C-level

После расширения roster результат становится важен не только как simulation trace, но и как executive diagnostic.

- **CEO / Country Manager** видит, что НДС-решение может одновременно ударить по `цене`, `нарративу`, `каналам продаж` и `отношениям с регулятором`.
- **CFO** получает не общий “риск”, а разложение по `working capital`, `supplier rotation`, `inventory pressure`, `settlement friction`.
- **COO / Commercial** видит отдельные механизмы для `логистики`, `складов`, `HoReCa`, `ритейла`, `e-commerce`, `bazaar / informal`.
- **GR / Public Affairs** получает контур `tax interpretation -> media amplification -> lobbying -> political escalation`, а не только consumer noise.

Именно это делает систему полезной для C-level: она объясняет не просто “что случится”, а `где образуется напряжение`, `кто будет первым усилителем`, `какой канал распространения наиболее опасен` и `какой тип управленческого ответа нужен`.

## Усиленная итерация 2: expanded roster and stronger divergence

После дальнейшего расширения agent roster и world variance был выполнен ещё один live pack на новой AgentMesh revision.

### Live runs

#### 1. Legal / Compliance

- `runId`: `IQvFZ2POCDPIbGV42gHM`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.659`
- `price_pressure = 0.708`
- `policy_pressure = 0.605`
- `narrative_volatility = 0.618`
- `supply_stress = 0.733`

Sensitivity:

- `narrative_volatility = 0.383`
- `policy_pressure = 0.395`
- `price_pressure = 0.293`

Key mechanisms:

- `do_nothing`
- `adjust_budget`
- `change_belief`
- `issue_statement`
- `lobby`

#### 2. Financial / Settlement

- `runId`: `9TjxGBwfTS6wQHLOytKb`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.639`
- `price_pressure = 0.936`
- `policy_pressure = 0.164`
- `narrative_volatility = 0.910`
- `supply_stress = 0.197`

Key mechanisms:

- `adjust_budget`
- `delay_purchase`
- `issue_statement`
- `sell`
- `switch_supplier`

#### 3. Demand / Channel Shift

- `runId`: `pWtWExEuzdVMiByL7FSb`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.617`
- `price_pressure = 0.965`
- `policy_pressure = 0.129`
- `narrative_volatility = 1.000`
- `supply_stress = 0.000`

Key mechanisms:

- `adjust_budget`
- `delay_purchase`
- `do_nothing`
- `issue_statement`
- `switch_supplier`

#### 4. Narrative / Lobbying

- `runId`: `mZVsJeNjD4oFKy6Qhi3S`
- `status`: `SUCCEEDED`

Outcome distribution:

- `risk_score = 0.938`
- `price_pressure = 0.568`
- `policy_pressure = 0.269`
- `narrative_volatility = 1.000`
- `supply_stress = 1.000`

Key mechanisms:

- `broadcast_message`
- `change_belief`
- `do_nothing`
- `issue_statement`
- `enforce_rule`

## Что дало расширение

Новый roster усилил три вещи:

- в контур вошёл `change_belief`, то есть не только действия, но и волны переинтерпретации режима
- compliance и settlement миры стали сильнее расходиться по профилю цены, политики и narrative
- C-level bundle теперь гораздо лучше показывает, что “налоговое изменение” быстро становится одновременно `pricing shock`, `channel shift`, `interpretation war` и `public pressure cycle`

Одновременно модель показала и новый diagnostic:

- `demand` и `narrative` оси местами уже упираются в saturation (`narrative_volatility = 1.000`)
- значит, следующий шаг нужен не в количестве агентов, а в более тонком damping/heterogeneity policy для информационных и consumer loops

## Усиленная итерация 3: differentiated scoring and axis relevance

После замечания о слишком похожих числах был переработан не только roster, но и сам scoring слой:

- действия перестали двигать все метрики одинаковыми фиксированными константами
- появился `domain-aware impact model`
- появился `axis relevance layer`, который подавляет off-axis шум
- в bundle добавлен более явный executive summary и counterfactual slices

### Последний live pack

#### 1. Legal / Compliance

- `runId`: `YL1ijzrpcif22K8o5MFM`
- `status`: `SUCCEEDED`
- `risk_score = 0.979`
- `price_pressure = 0.849`
- `policy_pressure = 0.956`
- `narrative_volatility = 0.722`
- `supply_stress = 0.816`

#### 2. Financial / Settlement

- `runId`: `0ex48BO2KycqmKWpMfd4`
- `status`: `SUCCEEDED`
- `risk_score = 0.789`
- `price_pressure = 0.726`
- `policy_pressure = 0.611`
- `narrative_volatility = 0.567`
- `supply_stress = 0.334`

#### 3. Demand / Channel Shift

- `runId`: `wXtBUkDAbtTvwvmunELO`
- `status`: `SUCCEEDED`
- `risk_score = 0.844`
- `price_pressure = 0.906`
- `policy_pressure = 0.389`
- `narrative_volatility = 0.552`
- `supply_stress = 0.468`

#### 4. Narrative / Lobbying

- `runId`: `0pZe7Cs7a4tgVsBg4mAT`
- `status`: `SUCCEEDED`
- `risk_score = 1.000`
- `price_pressure = 0.531`
- `policy_pressure = 0.969`
- `narrative_volatility = 0.976`
- `supply_stress = 0.952`

## Честная оценка результата

Что улучшилось:

- `demand` теперь заметно лучше отделён от `legal` и `narrative` по `policy_pressure`
- `financial` стал лучше читаться как price/liquidity contour, а не как almost-generic stress cloud
- variance и executive reading стали полезнее

Что ещё не идеально:

- `legal` и `narrative` остаются перегретыми и местами всё ещё поднимают слишком много метрик одновременно
- это значит, что следующий шаг уже не просто “ещё лучше scoring”, а `axis-specific activation`: разные оси должны запускать разные активные cohorts и разные step rhythms

## Усиленная итерация 4: segmented consumer channels and large enterprise

После этого этапа в `kzvat` hot-path были добавлены:

- сегментные consumer cohorts:
  - `consumer_urban_low_income`
  - `consumer_rural_low_income`
  - `consumer_family_mid_income`
  - `consumer_ecommerce_native`
  - `consumer_bazaar_sensitive`
  - `consumer_affluent`
- large enterprise cohorts:
  - `large_enterprise_retail`
  - `large_enterprise_importer`
  - `large_enterprise_manufacturer`
  - `enterprise_treasury`
- scenario-first population policy для `kzvat`, чтобы curated сегменты не размывались generic LLM classes
- axis-aware activation, чтобы поздние consumer/enterprise слои реально доходили до action loop

### Live pack

#### 1. Legal / Compliance

- `runId`: `OxPS9wmHd1wDs6tABSOR`
- `status`: `SUCCEEDED`
- `policy_pressure = 0.982`
- `risk_score = 0.975`

Dominant actor reactions:

- `importer` -> `lobby`, `adjust_budget`, `switch_supplier`
- `retailer` -> `lobby`, `adjust_budget`, `delay_purchase`
- `regulator` -> `enforce_rule`, `issue_statement`
- `consumer_bazaar_sensitive` -> `broadcast_message`, `switch_supplier`

#### 2. Financial / Settlement

- `runId`: `Qq8mhLzwaFc8Cw0wwIuF`
- `status`: `SUCCEEDED`

Dominant actor reactions:

- `retailer` -> `adjust_budget`, `delay_purchase`
- `importer` -> `sell`, `switch_supplier`, `adjust_budget`
- `consumer_urban_low_income` -> `delay_purchase`
- `consumer_family_mid_income` -> `adjust_budget`

Note:

- в `run summary` этого прогона outcome-map пустой, хотя bundle и actor reactions собрались; это отдельный технический дефект агрегированного summary, а не отсутствие симуляции

#### 3. Demand / Channel Shift

- `runId`: `ExIPfov2mdYrrbNWQQHh`
- `status`: `SUCCEEDED`
- `price_pressure = 0.848`
- `policy_pressure = 0.130`
- `supply_stress = 0.155`

Dominant actor reactions:

- `consumer_urban_low_income` -> `delay_purchase`, `switch_supplier`
- `consumer_family_mid_income` -> `adjust_budget`, `delay_purchase`
- `retailer` -> `adjust_budget`, `sell`
- `importer` -> `switch_supplier`

Это важный сдвиг: demand contour стал заметно чище и ближе к consumer/channel logic, а не к общему policy cloud.

#### 4. Narrative / Lobbying

- `runId`: `V17RqBa00pYYbbdYBthE`
- `status`: `SUCCEEDED`
- `narrative_volatility = 0.995`
- `policy_pressure = 0.991`

Dominant actor reactions:

- `importer` -> `broadcast_message`, `escalate`
- `retailer` -> `broadcast_message`, `escalate`
- `consumer_bazaar_sensitive` -> `broadcast_message`
- `consumer_ecommerce_native` -> `broadcast_message`

## Что реально улучшилось

- consumer channel больше не сводится к generic household behavior
- стали видны разные режимы:
  - low-income pressure
  - family budget compression
  - bazaar-sensitive substitution
  - e-commerce-native reaction
- large enterprise начал проявляться как отдельный слой в legal/financial контурах, а не растворяться в обычном `retailer/importer`
- demand run стал лучше читаться как реакция слоёв рынка, а не как очередной policy-like stress profile

## Итерация 4. Достройка графа под бюджет, инвестклимат, производство и госуслуги

### Что было добавлено в hot-path graph

Новая волна усиления делалась не через ещё один generic prompt-layer, а через достройку canonical graph вокруг налогового решения.

Добавлены новые узлы:

- органы:
  - `/gov/kz/ministry-of-national-economy`
  - `/gov/kz/ministry-of-health`
  - `/gov/kz/ministry-of-industry-and-construction`
  - `/gov/kz/ministry-of-education`
- программы:
  - `/program/kz/investment-climate-stability`
  - `/program/kz/domestic-production-continuity`
  - `/program/kz/public-services-resilience`
- рынки:
  - `/market/kz/public-sector/budget-revenue`
  - `/market/kz/finance/investment-climate`
  - `/market/kz/industrial/internal-production`
  - `/market/kz/healthcare/healthcare-public`
  - `/market/kz/healthcare/healthcare-private`
  - `/market/kz/construction/engineering-services`
  - `/market/kz/education/education-system`

Добавлены новые связи:

- министерства `ADMINISTERS` соответствующие программы
- `vat-rate-increase` и `vat-reform-2026` теперь воздействуют не только на retail/import/working capital, но и на:
  - бюджетные сборы
  - инвестклимат
  - внутреннее производство
  - public healthcare
  - private healthcare
  - engineering infrastructure
  - education system
- введены вторичные эффекты:
  - `internal-production -> budget-revenue`
  - `internal-production -> investment-climate`
  - `budget-revenue -> public healthcare`
  - `budget-revenue -> education-system`
  - `budget-revenue -> engineering-services`

### Что было добавлено в roster агентов

Новые graph-backed агенты:

- `budget_revenue_officer`
- `investment_climate_officer`
- `internal_producer`
- `healthcare_public_operator`
- `healthcare_private_operator`
- `engineering_infrastructure_operator`
- `education_system_operator`

Они не живут отдельно от графа: каждый привязан к новым canonical nodes и играет уже как репрезентатор соответствующего слоя среды.

### Live rerun после достройки графа

Seed summary:

- `13` новых fragments
- `12` обновлённых fragments
- `26` новых edges
- `23` обновлённых edges

Новые run’ы:

- `legal-compliance`: `qEwoHcTfIcbA0No17f5M`
- `financial-settlement`: `TIv2jgshB9IkRXylHt1x`
- `demand-shift`: `ooAIHT0UsSzOuLRfbrWG`
- `narrative-volatility`: `4t2RDqMFvrUbxZhhBalL`

#### 1. Legal / Compliance

- `risk_score = 1.000`
- `policy_pressure = 0.992`
- `price_pressure = 0.867`
- `supply_stress = 0.880`

Dominant actors:

- `importer` -> `lobby`, `adjust_budget`, `switch_supplier`
- `retailer` -> `lobby`, `adjust_budget`
- `internal_producer` -> `lobby`
- `regulator` -> `enforce_rule`, `issue_statement`
- `investment_climate_officer` -> `issue_statement`

Вывод:

- legal contour теперь уже не только про налоговое администрирование
- в него вошли внутренняя промышленность и инвестиционный контур
- система читает этот режим как максимальный режим институционального напряжения

#### 2. Financial / Settlement

- `risk_score = 0.804`
- `price_pressure = 0.732`
- `policy_pressure = 0.675`
- `narrative_volatility = 0.766`
- `supply_stress = 0.243`

Dominant actors:

- `importer` -> `switch_supplier`, `adjust_budget`, `sell`
- `retailer` -> `adjust_budget`, `delay_purchase`
- `bank` -> `adjust_budget`
- `supplier` -> `sell`, `adjust_budget`
- `internal_producer` -> `adjust_budget`, `sell`
- `large_enterprise_retail` -> `adjust_budget`, `issue_statement`, `sell`

Вывод:

- это уже не пустой summary, а внятный liquidity/settlement contour
- large enterprise и внутреннее производство теперь реально видны как отдельный слой
- инвестиционный и бюджетный контуры не доминируют здесь напрямую, но усиливают общую неопределённость и risk cloud

#### 3. Demand / Channel Shift

- `price_pressure = 0.760`
- `risk_score = 0.695`
- `supply_stress = 0.317`
- `policy_pressure = 0.103`

Dominant actors:

- `consumer_urban_low_income` -> `delay_purchase`, `switch_supplier`
- `importer` -> `switch_supplier`
- `retailer` -> `adjust_budget`, `sell`
- `internal_producer` -> `adjust_budget`
- `education_system_operator` -> `delay_purchase`
- `engineering_infrastructure_operator` -> `adjust_budget`
- `healthcare_private_operator` -> `sell`
- `healthcare_public_operator` -> `adjust_budget`

Вывод:

- consumer channel стал не только сегментным, но и связанным с public service layers
- система теперь видит, что налоговый shock проходит в бытовой спрос, а затем начинает давить и на связанные контуры услуг
- это особенно важно для оценки вторичных социальных эффектов

#### 4. Narrative / Lobbying

- `risk_score = 1.000`
- `narrative_volatility = 0.995`
- `policy_pressure = 0.995`
- `supply_stress = 0.968`

Dominant actors:

- `importer` -> `broadcast_message`, `escalate`
- `retailer` -> `broadcast_message`, `escalate`
- `budget_revenue_officer` -> `issue_statement`
- `education_system_operator` -> `issue_statement`
- `engineering_infrastructure_operator` -> `issue_statement`
- `healthcare_public_operator` -> `issue_statement`
- `healthcare_private_operator` -> `issue_statement`
- `investment_climate_officer` -> `change_belief`, `issue_statement`

Вывод:

- narrative contour теперь включает не только медиа и бизнес, но и контур государственных и окологосударственных объяснений
- это делает симуляцию ближе к реальности: при таком изменении ставки начинает говорить не только рынок, но и все слои, завязанные на бюджет и качество публичных услуг

## Что стало качественно сильнее

- вопрос про НДС теперь интерпретируется не только как retail/import tax shock
- появился управленческий контур:
  - сборы бюджета
  - устойчивость инвестклимата
  - внутреннее производство как опора ВВП
  - public/private healthcare
  - engineering infrastructure
  - education
- новые агенты не просто “для красоты”, а реально входят в propagation chains и actor reactions
- output стал ближе к policy instrument: система уже может объяснять не только коммерческую реакцию, но и риск для качества государственных функций и долгосрочного делового климата

## Итерация 5. JSON DSL для агентов + direct interactions

### Что изменено в архитектуре

В этой итерации `kzvat` перестал быть только Go-hardcode population layer.

Сделано:

- population archetypes вынесены в reusable JSON DSL
- interaction rules между агентами тоже описаны в JSON DSL
- runtime теперь:
  - поднимает population из scenario JSON
  - применяет directed interaction effects между агентами
  - записывает interaction summary в `messageBus`
  - меняет `stress`, `trust`, `budget`, `attention` у целевых агентов перед следующими шагами

Ключевой смысл:

- раньше агенты в основном взаимодействовали через общий state мира
- теперь у нас есть ещё и адресное воздействие:
  - `source class + action + axis -> target selectors + effect`

Это уже ближе к настоящей swarm-coordination logic, а не только к независимому проигрыванию ролей.

### Где это лежит

- DSL loader: `backend/internal/agentmesh/simulation/scenario_dsl.go`
- scenario spec: `backend/internal/agentmesh/simulation/scenarios/kzvat.json`
- runtime integration: `backend/internal/agentmesh/simulation/processor.go`

### Новый live pack после DSL + direct interactions

Новые run’ы:

- `legal-compliance`: `lHuDEstGPunmzdPQnV1D`
- `financial-settlement`: `ZjKL84wkW12NRvaJ88SA`
- `demand-shift`: `1XE2NzIyvFd1beP3NrGT`
- `narrative-volatility`: `Uk10CxGOTxNXvO9ifLoW`

#### 1. Legal / Compliance

- `risk_score = 1.000`
- `policy_pressure = 0.994`
- `price_pressure = 0.882`
- `supply_stress = 0.929`
- `narrative_volatility = 0.913`

Dominant reactions:

- `importer` -> `lobby`, `adjust_budget`, `switch_supplier`
- `retailer` -> `lobby`, `adjust_budget`, `delay_purchase`
- `internal_producer` -> `lobby`, `sell`, `switch_supplier`
- `regulator` -> `enforce_rule`, `issue_statement`, `adjust_budget`
- `investment_climate_officer` -> `issue_statement`, `change_belief`

Что изменилось:

- legal contour стал плотнее именно как institutional confrontation loop
- в цепочке сильнее видны `budget_revenue_officer` и `education_system_operator`
- это результат уже не только axis weighting, а направленных эффектов между business/state/public-service слоями

#### 2. Financial / Settlement

- `risk_score = 0.809`
- `price_pressure = 0.732`
- `policy_pressure = 0.695`
- `narrative_volatility = 0.762`
- `supply_stress = 0.248`

Dominant reactions:

- `retailer` -> `adjust_budget`, `delay_purchase`, `lobby`
- `importer` -> `switch_supplier`, `adjust_budget`, `sell`
- `bank` -> `adjust_budget`, `do_nothing`
- `internal_producer` -> `sell`, `adjust_budget`, `broadcast_message`
- `large_enterprise_importer` -> `sell`, `switch_supplier`, `adjust_budget`

Что изменилось:

- financial contour стал более межслойным
- consumer и treasury agents теперь сильнее видны в propagation chain
- связка `bank -> enterprise/importer -> consumer` читается лучше, чем раньше

#### 3. Demand / Channel Shift

- `price_pressure = 0.727`
- `risk_score = 0.664`
- `supply_stress = 0.243`
- `policy_pressure = 0.134`
- `narrative_volatility = 0.666`

Dominant reactions:

- `consumer_urban_low_income` -> `delay_purchase`, `switch_supplier`
- `consumer_rural_low_income` -> `switch_supplier`, `delay_purchase`
- `importer` -> `switch_supplier`, `lobby`
- `retailer` -> `adjust_budget`, `sell`, `delay_purchase`
- `education_system_operator` -> `delay_purchase`

Что изменилось:

- demand contour стал заметно менее “механическим”
- раньше он был чище, но более линейным; теперь в нём больше cross-layer effects
- за счёт direct interactions в consumer contour сильнее видны вторичные институциональные хвосты

#### 4. Narrative / Lobbying

- `risk_score = 1.000`
- `policy_pressure = 0.994`
- `narrative_volatility = 0.993`
- `supply_stress = 0.963`
- `price_pressure = 0.809`

Dominant reactions:

- `retailer` -> `broadcast_message`, `escalate`, `lobby`
- `importer` -> `broadcast_message`, `escalate`
- `internal_producer` -> `broadcast_message`
- `regulator` -> `issue_statement`, `enforce_rule`
- `budget_revenue_officer` -> `issue_statement`
- `education_system_operator` -> `issue_statement`
- `engineering_infrastructure_operator` -> `issue_statement`

Что изменилось:

- narrative contour теперь включает уже не только market/media, но и more explicit state counter-messaging
- public-service layers в narrative стали не фоном, а видимыми говорящими узлами

## Вывод по итерации DSL + direct interactions

- теперь у нас есть reusable язык для scenario-specific agent rosters
- и есть первый настоящий слой direct interactions между агентами
- это означает, что следующие сценарии можно собирать уже не в новом Go-hardcode, а в `scenario JSON + graph bindings + interaction rules`
- система стала не просто богаче по roster, а более инженерно переносимой
