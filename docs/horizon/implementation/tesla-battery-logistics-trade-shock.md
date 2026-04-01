# Tesla Battery Logistics Trade Shock

## Сценарий

Внешний сигнал:

> Новые экспортные ограничения и рост морских и сухопутных задержек на 20-30% по батарейным материалам и ячейкам для Tesla. Как отреагирует battery logistics контур Tesla в ближайшие 2-4 квартала: где возникнут bottleneck, как поведут себя Gigafactory Texas и Nevada, что произойдет с lead time, inventory, freight cost и product mix?

Дата live-run: 2026-03-23.

## Что было добавлено в Horizon graph

Под сценарий был достроен canonical hot path:

- географии: `/loc/us`, `/loc/jp`, `/loc/cn`, `/loc/cl`
- компании: `/org/us/tesla`, `/org/jp/panasonic`, `/org/cn/catl`, `/org/cl/sqm`
- регуляторы: `/gov/us/department-of-commerce`, `/gov/us/customs-and-border-protection`
- объекты исполнения: `/fac/us/gigafactory-nevada`, `/fac/us/gigafactory-texas`, `/fac/us/tesla-lithium-refinery-texas`, `/fac/jp/panasonic-giga-nevada-lines`, `/fac/cn/catl-ningde`, `/fac/cl/sqm-atacama-operations`, `/fac/us/port-of-long-beach`
- decision/mechanism/program:
  - `/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026`
  - `/mechanism/regulatory/export-controls-and-logistics-delays`
  - `/program/us/tesla-battery-logistics-resilience-2026`
- рынки:
  - `/market/us/industrial/critical-battery-materials`
  - `/market/us/industrial/ev-battery-cells`
  - `/market/us/transportation/freight-logistics`
  - `/market/us/finance/industrial-working-capital`
  - `/market/us/automotive/electric-vehicles`
  - `/market/us/energy/energy-storage-systems`
  - `/market/us/capital/industrial-investor-confidence`
- продуктовый путь:
  - `/product/raw/chemical/refining/lithium-carbonate`
  - `/product/raw/chemical/refining/lithium-hydroxide`
  - `/product/component/chemical/manufacturing/cathode-active-material`
  - `/product/component/electronic/manufacturing/ev-battery-cells`
  - `/product/component/electronic/assembly/ev-battery-module`
  - `/product/component/electronic/assembly/ev-battery-pack`
  - `/product/finished/mechanical/assembly/electric-vehicle`

Первичный seed дал:

- `18` новых fragments
- `16` updated fragments
- `50` новых edges

## Агентный слой

Сценарий описан через reusable JSON DSL, а не только кодом. Основные классы:

- `trade_official`, `customs_official`
- `facility_operator_nevada`, `facility_operator_texas`
- `panasonic_line_operator`, `catl_export_desk`, `material_supplier_lithium`, `lithium_refinery_operator`
- `port_operator`, `ocean_logistics_operator`, `ground_logistics_operator`, `rail_carrier`, `warehouse_operator`
- `procurement_director`, `production_planner`
- `vehicle_program_manager`, `energy_storage_program_manager`
- `enterprise_treasury`
- `large_energy_customer`
- `equity_analyst`, `investor_relations`
- `competitor_demand_sink`

Межагентные взаимодействия включали:

- customs pressure на logistics и supplier desks
- procurement reallocation между CATL, Panasonic и internal refining
- facility priority signals между Nevada, Texas и program managers
- investor confidence loop через `equity_analyst` и `investor_relations`
- competing demand pressure на battery-cell allocation

## Live Runs

### 1. Tesla trade/compliance contour

- runId: `wKojDFBzjuTij1YRUSop`
- статус: `SUCCEEDED`
- dominant stressors: `risk_score`, `policy_pressure`
- summary:
  - `risk_score=1.000`
  - `policy_pressure=0.989`
  - `price_pressure=0.950`
  - `supply_stress=0.844`
  - `narrative_volatility=0.860`
- top actor reactions:
  - `trade_official` -> `enforce_rule`, `issue_statement`
  - `customs_official` -> `enforce_rule`, `issue_statement`
  - `enterprise_treasury` -> `adjust_budget`, `lobby`
  - `facility_operator_nevada` -> `adjust_budget`, `switch_supplier`
  - `facility_operator_texas` -> `adjust_budget`, `switch_supplier`

Интерпретация:

- главный риск здесь не физический коллапс, а регуляторная жёсткость и compliance friction
- customs и trade-policy контур быстро поднимают policy pressure и заставляют Tesla уходить в supplier reallocation и финансовую защиту
- Texas и Nevada оба попадают под давление, но прежде всего как execution nodes, а не как isolated factories

### 2. Tesla financial/settlement contour

- runId: `Fu2hchKOEGp4xtQcQCtf`
- статус: `SUCCEEDED`
- dominant stressors: `risk_score`, `price_pressure`
- summary:
  - `risk_score=0.819`
  - `price_pressure=0.652`
  - `supply_stress=0.553`
  - `policy_pressure=0.491`
  - `narrative_volatility=0.459`
- top actor reactions:
  - `enterprise_treasury` -> `lobby`, `adjust_budget`, `change_belief`
  - `ocean_logistics_operator` -> `adjust_budget`, `issue_statement`
  - `production_planner` -> `adjust_budget`, `issue_statement`
  - `facility_operator_nevada` -> `adjust_budget`, `switch_supplier`
  - `energy_storage_program_manager` -> `sell`, `delay_purchase`

Интерпретация:

- это контур freight cost, working capital и allocation pressure
- первым начинает двигаться treasury, затем production planning и logistics
- Nevada и energy-storage контур получают заметное давление через cost-to-serve и inventory discipline

### 3. Tesla physical flow contour

- runId: `igDp8c0FBsvweGAFmKNm`
- статус: `SUCCEEDED`
- dominant stressors: `price_pressure`, `narrative_volatility`
- summary:
  - `price_pressure=0.770`
  - `risk_score=0.589`
  - `narrative_volatility=0.625`
  - `supply_stress=0.100`
  - `policy_pressure=0.077`
- top actor reactions:
  - `production_planner` -> `adjust_budget`, `switch_supplier`
  - `facility_operator_nevada` -> `adjust_budget`, `switch_supplier`
  - `facility_operator_texas` -> `adjust_budget`, `switch_supplier`
  - `energy_storage_program_manager` -> `delay_purchase`
  - `large_energy_customer` -> `delay_purchase`, `buy`
  - `ocean_logistics_operator` -> `switch_supplier`
  - `ground_logistics_operator` -> `switch_supplier`

Интерпретация:

- это самый “операционный” contour во всём пакете
- физический ответ системы не ушёл в regulatory panic; он ушёл в rebalance
- Texas и Nevada в основном реагируют через supplier switching и budget adjustment
- energy storage получает сжатие через `delay_purchase`, что намекает на product-mix reallocation в пользу более приоритетных линий

### 4. Tesla investor and uncertainty contour

- runId: `WHBCBHg4Mhwo0UIMt2gF`
- статус: `SUCCEEDED`
- dominant stressors: `risk_score`, `policy_pressure`
- summary:
  - `risk_score=1.000`
  - `policy_pressure=0.963`
  - `price_pressure=0.921`
  - `narrative_volatility=0.936`
  - `supply_stress=0.845`
- top actor reactions:
  - `trade_official` -> `issue_statement`, `enforce_rule`
  - `ocean_logistics_operator` -> `issue_statement`, `adjust_budget`, `broadcast_message`
  - `enterprise_treasury` -> `issue_statement`, `adjust_budget`, `lobby`
  - `facility_operator_texas` -> `issue_statement`, `adjust_budget`, `switch_supplier`
  - `equity_analyst` -> `change_belief`, `broadcast_message`, `issue_statement`
  - `procurement_director` -> `issue_statement`, `lobby`, `switch_supplier`

Интерпретация:

- uncertainty contour очень быстро превращает supply-chain shock в confidence shock
- analyst/investor loop усиливает narrative pressure почти так же быстро, как customs и trade officials усиливают policy pressure
- этот contour показал, что для Tesla тема быстро перестаёт быть только про freight и становится темой execution credibility

## C-level вывод

Если объяснять этот пакет руководителю, то вывод такой.

Сценарий не показывает один общий “срыв цепочки”. Он показывает четыре связанных режима.

1. Регуляторный режим.
Торговые ограничения и customs friction быстро поднимают compliance burden. Это давит не только на поставки, но и на стратегическую свободу Tesla по supplier allocation.

2. Финансовый режим.
Freight cost и working-capital pressure первыми бьют по treasury и planning. Здесь ключевой риск в том, что стоимость устойчивости цепочки резко растёт ещё до фактического дефицита.

3. Операционный режим.
Texas и Nevada не “ломаются”, а начинают агрессивно ребалансировать sourcing и product allocation. Самый вероятный ответ системы здесь не shutdown, а forced prioritization.

4. Информационно-инвесторский режим.
Если ограничения и delays длятся, рынок начинает воспринимать историю как признак более хрупкой execution model. Это ухудшает investor confidence быстрее, чем меняются физические метрики на земле.

Самый важный управленческий смысл:

- bottleneck возникает не в одной точке, а на стыке `customs -> freight -> cell allocation -> factory planning -> investor confidence`
- Nevada и Texas реагируют похоже по типу действий, но различаются по смыслу:
  - Nevada сильнее завязана на energy storage и cell continuity
  - Texas сильнее чувствительна к vehicle mix и execution credibility
- product mix с высокой вероятностью будет пересматриваться в пользу приоритетных линий и более защищённых направлений
- freight cost и uncertainty растут быстрее, чем чистый physical stress

## Технический вывод

Система здесь отработала как связка Horizon + AgentMesh:

- Horizon graph дал канонический scope поставщиков, продуктов, мощностей, регуляторов и логистических узлов
- AgentMesh развернул 4 contour-run’а поверх одного baseline snapshot
- JSON DSL задал population и interaction rules
- итоговые executive/technical readout были синтезированы из traces swarm-а, а не написаны “из головы”

Это уже рабочий шаблон для следующего класса supply-chain policy simulations.

## Runtime v2 Hardening

После первого прогона simulation runtime был углублён по четырём направлениям:

- расширена action ontology:
  - `reserve_inventory`
  - `reroute_shipment`
  - `expedite_shipment`
  - `allocate_capacity`
  - `reprioritize_flow`
  - `request_exemption`
  - `hedge_cost`
  - `adjust_guidance`
- scenario DSL получил обязательные `metadata`, `graphRequirements`, `interpretation`
- graph authoring requirements стали валидируемыми через required paths и required bindings
- generic interpretation был отделён от scenario overlay, а Tesla readout стал отдельным overlay-слоем

### Live rerun on runtime v2

- `tesla-legal`: `UUcFcr4bWDnbDtHXxdnr`
- `tesla-financial`: `S6OY2Pc0z2oNdwcaEjsH`
- `tesla-info`: `N0NsGG1v5veqF6p9ZjQ6`
- `tesla-physical`: `ETSaN4zIXESZZT6XxsxZ`

### Live rerun on runtime v3: systemic executive memo + PR agent

Новый runtime закрепил executive-layer как часть `SimulationBundle`, а не как внешний текст.

- AgentMesh revision: `nv-horizon-agentmesh-00032-5mm`
- `tesla-legal`: `7ebHDuAX68xxBOWfchXA`
- `tesla-financial`: `406Tuqpk85VMZ8XYNSgw`
- `tesla-physical`: `X3MY76CE0sjVyZVec1Bv`
- `tesla-info`: `11tO72gR1uuCQclCu47W`

Новый bundle теперь системно содержит:

- `executiveNarrative`
- `executiveMemo.whatIsHappening`
- `executiveMemo.whatIsAtRisk`
- `executiveMemo.actions30Days`
- `executiveMemo.weeklyMonitoring`
- `executiveMemo.prResponse`

`prResponse` строится из swarm traces и теперь является частью стандартного результата, а не внешним комментарием.

### Что показал runtime v3

`tesla-legal`

- `risk_score=1.000`
- `policy_pressure=0.992`
- `price_pressure=0.954`
- memo-смысл: это уже не просто compliance contour, а fight-for-room-to-operate contour через `request_exemption`, `enforce_rule`, `issue_statement`

`tesla-financial`

- `risk_score=0.654`
- `price_pressure=0.755`
- `narrative_volatility=0.696`
- memo-смысл: treasury, planning и factory allocation стали ранним управленческим слоем, а не вторичной реакцией

`tesla-physical`

- `risk_score=0.613`
- `price_pressure=0.895`
- `policy_pressure=0.164`
- memo-смысл: это чистый forced-prioritization contour, где Tesla прежде всего теряет гибкость, а не сам факт способности работать

`tesla-info`

- `risk_score=1.000`
- `narrative_volatility=0.989`
- `policy_pressure=0.989`
- memo-смысл: supply-chain story превращается в control-and-credibility story, и PR/IR контур становится частью operating response

### CEO-level reading of runtime v3

Система теперь говорит это не только в prose, а прямо в системном memo-контракте.

Что происходит:

- trade shock проходит через customs, freight, allocation, factory planning и investor confidence
- Texas и Nevada выступают не просто factories, а узлами приоритизации
- PR/IR больше не внешний wrapper, а часть response loop

Что под риском:

- flexibility, а не только continuity
- margin defense и treasury buffer
- product-priority balance между EV и storage
- credibility рынка, если narrative отрывается от реальных действий

Что делать в ближайшие 30 дней:

- зафиксировать joint steering loop между procurement, treasury, logistics, factory planning, IR и PR
- формализовать Texas-vs-Nevada allocation rules
- поднять explicit exemption/customs response workstream
- говорить рынку только то, что подтверждено реальными operating actions

Что мониторить каждую неделю:

- customs clearance time
- freight premium
- reroute frequency
- EV-vs-storage allocation split
- narrative drift от temporary disruption к structural loss of control

### Что изменилось по сути

`tesla-legal`

- стал более дисциплинированным как policy/compliance contour
- новые key mechanisms: `request_exemption`, `enforce_rule`, `issue_statement`
- summary:
  - `risk_score=1.000`
  - `policy_pressure=0.990`
  - `price_pressure=0.958`
  - `supply_stress=0.933`

`tesla-financial`

- стал заметно богаче и реалистичнее:
  - `allocate_capacity`
  - `reprioritize_flow`
  - `adjust_guidance`
  - `hedge_cost`
- summary:
  - `risk_score=0.620`
  - `price_pressure=0.757`
  - `policy_pressure=0.211`
  - `supply_stress=0.133`

`tesla-info`

- стал чище как confidence/execution credibility contour
- появились `adjust_guidance`, `reroute_shipment`, `reprioritize_flow`
- summary:
  - `risk_score=1.000`
  - `policy_pressure=0.986`
  - `narrative_volatility=0.982`
  - `price_pressure=0.946`

`tesla-physical`

- на момент фиксации один мир из восьми ещё оставался `RUNNING`, но 7 из 8 миров уже показали устойчивый и очень содержательный pattern
- доминирующие механизмы по completed worlds:
  - `adjust_budget`
  - `allocate_capacity`
  - `reprioritize_flow`
  - `reroute_shipment`
  - `switch_supplier`
- approximate partial profile по 7 completed worlds:
  - `risk_score≈0.599`
  - `price_pressure≈0.907`
  - `policy_pressure≈0.099`
  - `supply_stress≈0.066`

### Новый управленческий смысл

Во второй итерации симуляция стала меньше говорить “про стресс вообще” и больше говорить “про способ управления стрессом”.

Это важно, потому что теперь Tesla scenario читается так:

- legal contour: Tesla пытается не просто выжить, а получать carve-out / exception pressure против ужесточения режима
- financial contour: основной ответ идёт через treasury, cost defense, capacity allocation и guidance management
- physical contour: фабрики и planning-функции не только “режут бюджет”, а реально начинают reroute, reserve и reprioritize
- info contour: проблема становится историей про execution credibility, а не только про shipment delay

Именно это и было целью углубления runtime.

## Runtime v4: discipline-first output with role adapters

Следующий шаг убрал `CEO memo` из позиции primary contract. Теперь первичный результат run-а:

- `DisciplinedSignals`
- `FunctionExposures`
- `DecisionDemands`
- `MonitoringSignals`
- `RoleReadouts`

`ExecutiveMemo` сохранён только как совместимый projection.

### Live rerun on runtime v4

- AgentMesh revision: `nv-horizon-agentmesh-00034-s28`
- `tesla-legal`: `SD8AA8nlUWV3dDu24qFH`
- `tesla-financial`: `uEvYkNIji4wzhovnoXqw`
- `tesla-physical`: `5S0U6SqvrCVVbuMKzvk8`
- `tesla-info`: `rjQrPEflWj8lPWBiYR9G`

### Что это дало

Теперь система говорит не “что сказать CEO”, а:

- какие дисциплины реально активны;
- какие функции реально под риском;
- какие решения swarm требует;
- что мониторить;
- как это переводится в readout для конкретной роли.

### Примеры role readouts из живого прогона

`CFO` на `tesla-financial`

- primary lens: `liquidity, cost, guidance`
- strongest discipline: `treasury_finance`
- priority functions: `communications`, `finance`, `procurement`
- decision demands:
  - `Capacity and routing control`
  - `Stakeholder narrative alignment`
  - `Treasury, margin, and guidance control`

`PR Lead` на `tesla-info`

- primary lens: `narrative control, market interpretation`
- strongest discipline: `policy_compliance`
- priority functions: `communications`, `finance`, `regulatory`
- decision demands:
  - `Regulatory pathway management`
  - `Stakeholder narrative alignment`
  - `Treasury, margin, and guidance control`

`COO` на `tesla-physical`

- primary lens: `continuity, capacity, flow`
- strongest discipline: `product_program`
- priority functions: `logistics`, `operations`, `procurement`, `product`
- decision demand:
  - `Capacity and routing control`
