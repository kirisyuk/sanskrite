# Horizon Watch

## Статус

Product / UX / Read-model specification (draft v1)

## Кратко

`Horizon Watch` — отдельная страница внутри Horizon, которая превращает внешние и внутренние сигналы, графовые изменения и интерпретации системы в красивую, персонализированную и объяснимую картину текущего риска и давления на мир.

Это не “новостная лента” и не “второй WorldMonitor UI”.

Это:
- глобальный экран ситуационной осведомленности;
- risk-monitor first, а не media surface;
- персональный watchlist по странам, рынкам, регионам, коридорам, сущностям и графовым срезам;
- read-model над `HorizonFragment`, `DependencyEdge`, `Lens`, `OntologyFact`, сигналами, propagation и materialized artifacts;
- входная точка в граф, а не отдельный продукт в стороне от графа.

Главная ценность:
- пользователь за 10-20 секунд понимает, **где сейчас накапливается риск и давление**;
- может добавить интересующие области в личный watch;
- видит не raw event, а **кого, где и как это заденет**;
- может провалиться из любой карточки в граф, evidence, пути влияния, артефакты и источники.

## Зачем это нужно

Сейчас Horizon силён как графовая и аналитическая система, но ему нужен мощный “surface layer”, который:
- показывает ценность без предварительной глубокой сборки сложного Lens;
- даёт ощущение живого мира, а не только ответов по запросу;
- показывает интерпретации, задетые объекты и impact geometry раньше, чем пользователь уйдёт в глубину;
- связывает источники, сигналы, propagation, overlays и dashboard-слои в единый пользовательский экран;
- формирует привычку регулярного возврата в продукт.

`Horizon Watch` должен стать:
- стартовым обзорным экраном для части пользователей;
- постоянным мониторинговым экраном для операторов;
- верхним синтетическим слоем над существующей horizon-моделью.

## Принципиальная позиция

### Чем Horizon Watch является

- Синтетическим экраном ситуационной осведомленности.
- Риск-монитором поверх графа и сигналов.
- Персонализированным monitoring surface.
- Объяснимым read-model над horizon-данными.
- Навигационным слоем в граф, профили фрагментов, пути влияния и evidence.

### Чем Horizon Watch не является

- Не бесконечной RSS-лентой.
- Не “картой новостей мира”.
- Не параллельной аналитической системой со своей правдой.
- Не заменой `LensCanvas`, а витриной и точкой входа в него.

## Продуктовая формула

Формула страницы:

**Risk monitoring + personal watch + impact explanation + graph drill-down**

Пользователь должен всегда видеть четыре вещи:
- `где накапливается риск`;
- `насколько это важно`;
- `что это затронет`;
- `почему система так считает`.

## Синхронизация с публичными service lines

Публичный сайт обещает три основные сервисные линии:
- `Geopolitical Risks Overview`
- `Deep Monitoring`
- `Scenario Planning`

`Horizon Watch` должен быть внутренней продуктовой поверхностью, которая аккуратно сшивает эти три линии в один работающий monitoring surface.

Важно:
- это не три разных продукта;
- это три разных режима чтения одной и той же horizon-системы;
- данные, граф, сигналы, paths, interpretations и map layers должны переиспользоваться между ними.

### Общий принцип сшивки

Один и тот же underlying stack обслуживает все три линии:
- `sources / feeds / facts`;
- `SignalDescriptor` и риск-сигналы;
- `Propagation`;
- `Lens`;
- `MaterializedArtifact`;
- `ExposurePath`;
- `Interpretation artifacts`;
- карты, объекты, зоны, flows, path legs;
- user watch targets и persistent profile preferences.

Меняется не ядро системы, а:
- framing;
- уровень абстракции;
- горизонт анализа;
- тип brief;
- тип dashboards;
- тип drill-down.

## High-grade стандарт для service lines

Под `high-grade` здесь понимается не просто красивый интерфейс.

Это комбинация из четырёх вещей:
- знакомая пользователю профессиональная форма;
- гораздо более сильная внутренняя семантика;
- жёсткая связность с graph/path/evidence слоями;
- ощущение, что система умеет не только показывать, но и мыслить.

### Что пользователь уже узнаёт

Пользователь привык к нескольким классам профессиональных поверхностей:
- executive risk briefing;
- monitoring terminal;
- scenario workspace;
- map-driven threat picture;
- dashboard wall;
- analyst drill-down panel.

`Horizon Watch` должен брать эти знакомые формы, но собирать их не поверх рыхлых таблиц и ручных комментариев, а поверх:
- `OntologyFact`;
- `SignalDescriptor`;
- `Propagation`;
- `MaterializedArtifact`;
- `ExposurePath`;
- `Interpretation artifacts`;
- `Lens` и user watch scopes.

### В чём наша особенность

Классические системы часто распадаются на:
- отдельный news room;
- отдельный dashboard tool;
- отдельный scenario spreadsheet;
- отдельный analyst memo.

`Horizon Watch` должен делать обратное:
- одна семантика;
- один graph-aware evidence layer;
- один interpretation layer;
- три service-line режима над тем же ядром.

Именно это делает продукт high-grade: пользователь видит знакомую профессиональную форму, но ощущает существенно большую связность, объяснимость и глубину.

## Service Line 01: Geopolitical Risks Overview

Публичная формулировка:
- `Geopolitical Risks Overview`
- `Executive risk framing`

Продуктовый смысл внутри `Horizon Watch`:
- давать руководителю или аналитическому лидеру быстрый стратегический обзор регионов, коридоров, policy moves и системных зависимостей;
- не тонуть в потоке слабых сигналов;
- показывать готовые risk briefs и interpretation-first surfaces.

### Знакомая пользователю форма

Этот режим должен ощущаться как сочетание:
- executive briefing;
- global risk terminal;
- geopolitics dashboard;
- region/corridor intelligence panel.

Но ключевое отличие:
- это не статичная презентация;
- не набор headline cards;
- а graph-aware strategic picture с path-grounded reasoning.

### Как это создаётся системой

1. Система выбирает macro scopes:
- регионы;
- страны;
- chokepoints;
- коридоры;
- policy surfaces;
- стратегические рынки.

2. По каждому scope собираются:
- signals;
- propagation outcomes;
- policy / sanctions facts;
- materialized paths;
- exposure paths;
- interpretation artifacts.

3. `Interpretation Engine` строит:
- `InterpretationThesis`;
- `MechanismChain`;
- `TrustShift`, если применимо;
- `Beneficiary / Loser Map`;
- executive summary с горизонтом и confidence.

4. Read-model ранжирует slices не по объёму новостей, а по:
- strategic pressure;
- consequence depth;
- graph centrality;
- path richness;
- interpretation strength.

5. UI публикует executive-level `WatchBrief`.

### Основные UX-формы

- `Situation Bar`
- `Global Hot Slices`
- `Global Pressure Map`
- macro / region / corridor cards
- beneficiary / loser interpretation blocks
- executive-level path explanations
- context map fragments по chokepoints и route geometry

### Dashboard grammar

Дашборды для этой линии должны быть обзорными, а не операционными.

Подходящий набор:
- `Global Pressure Map`
- `Regional Pressure Table`
- `Corridor Stress Board`
- `Policy / Sanctions Shift Board`
- `Strategic Dependency Heatmap`
- `Beneficiary / Loser Surface`

Каждый виджет должен отвечать на один из вопросов:
- где накапливается давление;
- какие policy shifts уже material;
- какие corridors становятся ключевыми;
- где downstream consequences выше headline noise;
- кто выигрывает или проигрывает структурно.

### Типы кейсов

1. Региональная risk picture
- `Persian Gulf`
- `South China Sea`
- `Eastern Mediterranean`
- `Black Sea`
- `Horn of Africa`

2. Коридорный и chokepoint обзор
- `Red Sea shipping pressure`
- `Suez and Bab el-Mandeb corridor risk`
- `Hormuz exposure`
- `Black Sea grain corridor`
- `Trans-Caspian / Middle Corridor pressure`

3. Policy and alignment overview
- санкционные сдвиги;
- экспортные ограничения;
- дипломатические рассинхронизации;
- оборонные и технологические перестройки;
- институциональные разрывы между союзниками.

4. Strategic dependency overview
- критические цепочки поставок;
- технологические зависимости;
- энергологистика;
- downstream exposure to upstream stress;
- geography-bound industrial concentration.

### Артефакты системы, на которые это ложится

- `WatchBrief` по региону, стране, рынку, corridor;
- `InterpretationThesis`;
- `MechanismChain`;
- `Beneficiary / Loser Map`;
- `TrustShift`;
- `Risk Map Fragments`;
- `MaterializedArtifact` и `ExposurePath`;
- aggregate dashboards по географии, рынкам, pressure.

### Drill-down model

Пользователь должен иметь три уровня провала:

1. Executive brief expand
- раскрыть тезис;
- увидеть main mechanisms;
- увидеть top affected and top beneficiaries.

2. Analytical detail
- policy moves;
- signals and interpretation deltas;
- path-level explanations;
- map fragment detail;
- evidence buckets.

3. Core horizon drill-down
- graph;
- map;
- region / corridor lens;
- materialized path;
- exposure path;
- source/evidence viewer.

### Типовой выход для пользователя

Пользователь получает:
- сильный executive brief;
- краткий список главных рисковых зон;
- объяснение, что меняется и почему;
- path-grounded view of consequences;
- переход в подробный graph/map/evidence context при необходимости.

## Service Line 02: Deep Monitoring

Публичная формулировка:
- `Deep Monitoring`
- `Persistent intelligence watch`

Продуктовый смысл внутри `Horizon Watch`:
- не просто дать обзор, а держать дисциплинированное постоянное наблюдение по выбранным темам, юрисдикциям, сущностям, weak signals и emerging patterns;
- превращать пользовательский интерес в долговременный watch surface;
- держать историю deltas, reframes и усилений.

### Знакомая пользователю форма

Этот режим должен ощущаться как сочетание:
- monitoring terminal;
- persistent watchboard;
- analyst cockpit;
- live interpreted tracking surface.

Но отличаться он должен тем, что:
- в центре не event stream;
- а evolving meaning of the watched scope.

### Как это создаётся системой

1. Пользователь или система создаёт `WatchTarget`:
- страна;
- рынок;
- corridor;
- entity;
- topic;
- lens;
- query-address scope.

2. Watchdog и signal pipeline непрерывно обновляют:
- due facts;
- signal descriptors;
- propagation state;
- exposure/materialized overlays;
- baseline deltas.

3. `Interpretation Engine` строит:
- `InterpretationDelta`;
- `TrustShift`;
- `StrategicShiftHypothesis`;
- escalation / cooling narrative;
- `what changed since last visit`.

4. Read-model объединяет:
- raw change;
- meaning of change;
- affected targets;
- confidence;
- evidence strength;
- realtime priority.

5. UI собирает persistent watch cards и live operational rails.

### Основные UX-формы

- `My Watch`
- persistent watch targets
- watch-specific filters
- live deltas and interpretation deltas
- entity / country / market / corridor-specific cards
- update since last visit
- compact map fragments по объектам и маршрутам

### Dashboard grammar

Дашборды для этой линии должны быть monitoring-first.

Подходящий набор:
- `Watched Targets Status`
- `Escalating Targets Board`
- `Signal Maturation Board`
- `Propagation Activity Rail`
- `Evidence Strength Mix`
- `New Since Last Visit`
- `Weak Signal Accumulation`

Каждый виджет должен помогать оператору понять:
- где ускорение;
- где накопление;
- где weak signal становится material;
- где требуется review;
- где interpretation changed.

### Типы кейсов

1. Country / jurisdiction watch
- `Saudi Arabia`
- `Iran`
- `Turkey`
- `Kazakhstan`
- `Egypt`

2. Entity watch
- порты;
- fabs;
- госпрограммы;
- энергетические узлы;
- нацчемпионы;
- поставщики оборудования;
- конкретные госструктуры или корпорации.

3. Topic / sector watch
- semiconductors;
- drone and defense tech;
- oil and gas;
- ports and rail;
- sanctions exposure;
- critical minerals;
- telecom / 5G / AI infrastructure.

4. Weak signal monitoring
- ранние признаки vendor substitution;
- локализационные сдвиги;
- изменение tone of official messaging;
- рост альтернативных маршрутов;
- накопление инфраструктурной активности;
- повторяющиеся soft indicators без headline-value, но с высокой стратегической ценностью.

### Артефакты системы, на которые это ложится

- `WatchTarget`
- `WatchFeedEntry`
- `InterpretationDelta`
- `TrustShift`
- `StrategicShiftHypothesis`
- watch-specific `Risk Map Fragments`
- entity-linked `MaterializedArtifact` and `ExposurePath`
- profile persistence and user preferences

### Drill-down model

1. Monitoring brief expand
- delta since last visit;
- interpretation shift;
- affected targets;
- why it moved up.

2. Operational detail
- signal timeline;
- propagation chain;
- new evidence;
- map object / route fragment;
- source cluster by confidence.

3. Core horizon drill-down
- entity profile;
- graph;
- map;
- artifact detail;
- lens;
- source/evidence viewer.

### Типовой выход для пользователя

Пользователь получает:
- не одноразовый обзор, а живую tracked surface;
- понимание, что именно изменилось с прошлого визита;
- сигнал о переходе weak signal в material development;
- объяснение, почему какой-то объект стал важнее;
- возможность быстро ужесточить или сузить watch.

## Service Line 03: Scenario Planning

Публичная формулировка:
- `Scenario Planning`
- `Contingency modeling`

Продуктовый смысл внутри `Horizon Watch`:
- строить не только picture of now, но и ветвления развития;
- моделировать escalation, disruption и second-order effects;
- связывать текущие сигналы с возможными траекториями, trigger conditions и planning consequences.

### Знакомая пользователю форма

Этот режим должен ощущаться как сочетание:
- scenario workspace;
- board-grade contingency surface;
- decision support studio;
- professional what-if environment.

Но его основа должна быть не в слайдах и spreadsheet thinking, а в тех же horizon-артефактах, что уже питают Watch.

### Как это создаётся системой

1. Сценарий стартует от текущего observed scope:
- страна;
- corridor;
- market;
- entity;
- supply chain path;
- lens.

2. Система использует ту же семантику доменных контрактов, которая уже есть в `Scout+` и соседних pipeline:
- `supply_chain`
- `governance`
- `regulatory impact`
- и связанные contract overlays / path semantics.

3. Поверх текущего состояния строятся scenario branches:
- escalation;
- disruption;
- closure;
- sanction tightening;
- rerouting;
- localization response;
- supplier substitution.

4. Для каждой ветки система строит:
- trigger conditions;
- affected paths;
- control points;
- beneficiaries / losers;
- second-order effects;
- disconfirmers;
- planning implications.

5. Read-model собирает scenario panels, comparison cards и projected impact surfaces.

### Основные UX-формы

- scenario panels поверх watch briefs
- branch cards
- trigger and disconfirmation blocks
- expected impact paths
- contingency dashboards
- what-changes-if mode
- projected map fragments и route overlays

### Dashboard grammar

Дашборды для этой линии должны быть planning-first.

Подходящий набор:
- `Scenario Comparison Board`
- `Trigger Conditions Panel`
- `Impact Cone`
- `Regulatory Impact Surface`
- `Governance Stress View`
- `Supply Chain Breakpoint Map`
- `Projected Beneficiary / Loser Matrix`

Каждый виджет должен помогать ответить на вопросы:
- какие ветки реалистичнее;
- что запускает переход в другую ветку;
- какие control points самые чувствительные;
- где second-order effects сильнее прямого шока;
- какие planning decisions зависят от сценария.

### Типы кейсов

1. Escalation scenarios
- ограниченная эскалация;
- расширение театра;
- удар по chokepoint;
- дипломатическое окно деэскалации;
- asymmetric retaliation pattern.

2. Disruption scenarios
- перекрытие маршрута;
- остановка ключевого узла;
- экспортное ограничение;
- санкционное ужесточение;
- supplier failure / asset denial.

3. Second-order effects
- рост локализации у клиентов;
- vendor diversification;
- рост местных чемпионов;
- price / logistics pressure beyond immediate region;
- reallocation of long-horizon capex and partnerships.

4. Board / planning cases
- что произойдёт с цепочкой поставок при закрытии X;
- где у нас наибольшая зависимость при сценарии Y;
- кто из альтернативных поставщиков выигрывает;
- какой рынок окажется under pressure вторым или третьим;
- какие сигналы будут ранним подтверждением сценария.

### Артефакты системы, на которые это ложится

- `Scenario`-артефакты и scenario branches;
- `StrategicShiftHypothesis`;
- `MechanismChain`;
- `Disconfirming Signal Set`;
- projected `ExposurePath` / `MaterializedArtifact`;
- contract-driven supply chain / governance / regulatory overlays;
- scenario-linked dashboards and map fragments.

### Drill-down model

1. Scenario brief expand
- selected branch;
- main assumptions;
- trigger conditions;
- top impacted paths.

2. Planning detail
- contract mode overlays;
- projected exposure;
- governance / regulatory impacts;
- beneficiary / loser shifts;
- disconfirmation layer.

3. Core horizon drill-down
- scenario graph;
- projected path artifact;
- route / map overlay;
- lens-based scope;
- evidence and assumptions.

### Типовой выход для пользователя

Пользователь получает:
- не только interpretation текущего момента;
- но и structured view of plausible futures;
- набор условий, которые усилят или ослабят сценарий;
- понимание second-order effects;
- материал для planning cycles и board-level discussion.

## Как три линии сходятся в одном Watch surface

Эти три линии должны восприниматься не как разрозненные разделы, а как единый стек глубины.

Логика перехода:

1. `Geopolitical Risks Overview`
- даёт общую strategic picture.

2. `Deep Monitoring`
- закрепляет постоянное наблюдение по выбранным slices.

3. `Scenario Planning`
- переводит текущие наблюдения в возможные траектории и contingency discussion.

То есть внутри `Horizon Watch` одна и та же тема может проходить три стадии:
- `overview`
- `persistent watch`
- `scenario branch`

### Пример сквозного кейса

Для одного и того же объекта или региона система может показать:
- global overview card: `Hormuz pressure rising`
- personal monitoring card: `Iran / Gulf / shipping / insurers added to My Watch`
- scenario panel: `If chokepoint closure probability rises above threshold, downstream exposure expands to X, Y, Z`

Именно это и есть аккуратная сшивка логик, которые уже есть в системе.

## Визуальная реализация по service lines

Ниже фиксируется не просто набор блоков, а целевой visual grammar для каждого режима.

### Visual mode: Geopolitical Risks Overview

Этот режим должен быть:
- panoramic;
- editorial;
- executive;
- calm but forceful.

#### Главный экран

Композиция:
- крупный `Situation Bar`;
- 1 hero regional brief;
- 3-6 macro cards;
- справа или ниже обзорные dashboards;
- impact rail с короткими mechanism chains;
- context map fragments по chokepoints, corridors и hotspot-кластерам.

#### Основные визуальные артефакты

- `Executive Brief Card`
- `Region / Corridor Card`
- `Policy Shift Card`
- `Beneficiary / Loser Block`
- `Pressure Map`
- `Mechanism Rail`
- `Strategic Dependency Heatmap`

#### Как пользователь этим управляет

Пользователь может:
- переключать `Global / Sector / Region`;
- менять горизонт `24h / 7d / 30d / strategic`;
- включать только policy / only chokepoints / only dependencies;
- pin region or corridor into `My Watch`;
- раскрывать interpretation and path explanation.

#### Куда он проваливается

- в regional detail brief;
- в corridor lens;
- в map fragment -> full map;
- в path detail;
- в evidence and policy source cluster;
- в beneficiary / loser rationale.

### Visual mode: Deep Monitoring

Этот режим должен быть:
- denser;
- more operational;
- alive but controlled;
- built around deltas.

#### Главный экран

Композиция:
- compact `Situation Bar`;
- `My Watch` в приоритете;
- stacked monitoring cards;
- live status rail;
- compact dashboards;
- compact map fragments around watched objects and routes.

#### Основные визуальные артефакты

- `Monitoring Card`
- `Delta Card`
- `Interpretation Shift Badge`
- `Escalation Marker`
- `Signal Maturation Panel`
- `Propagation Rail`
- `New Since Last Visit`
- `Watched Target Status Matrix`

#### Как пользователь этим управляет

Пользователь может:
- добавлять и удалять watch targets;
- pin/unpin priorities;
- задавать filters by geography / sector / target type / evidence quality;
- включать `Only Escalating`, `Only Material`, `Only With Paths`;
- замораживать realtime;
- менять density интерфейса;
- открывать target-specific monitoring surfaces.

#### Куда он проваливается

- в target detail panel;
- в signal timeline;
- в propagation chain;
- в map object detail;
- в full artifact / path detail;
- в underlying lens or graph.

### Visual mode: Scenario Planning

Этот режим должен быть:
- deliberate;
- high-control;
- comparison-friendly;
- board-grade.

#### Главный экран

Композиция:
- сверху current-state brief;
- рядом branch selector;
- ниже scenario comparison surface;
- projected impact paths;
- trigger / disconfirmation panel;
- scenario-specific maps and dashboards.

#### Основные визуальные артефакты

- `Scenario Branch Card`
- `Trigger Conditions Panel`
- `Disconfirmation Panel`
- `Projected Impact Path`
- `Impact Cone`
- `Governance / Regulatory Overlay Card`
- `Scenario Comparison Matrix`
- `Planning Decision Panel`

#### Как пользователь этим управляет

Пользователь может:
- выбирать baseline;
- переключать branches;
- смотреть optimistic / base / stress modes;
- включать supply-chain / governance / regulatory overlays;
- pin assumptions;
- сравнивать branch-to-branch deltas;
- переходить из watch brief в scenario workspace.

#### Куда он проваливается

- в branch assumptions;
- в projected exposure detail;
- в governance / regulatory overlays;
- в projected map routes;
- в scenario graph;
- в evidence and disconfirmation logic.

## Каталог пользовательских артефактов

Пользователь не должен взаимодействовать с “сырыми сущностями системы”.
Он должен получать устойчивый набор UI-артефактов, каждый из которых имеет ясный смысл.

### Артефакты первого уровня

Это то, что видно сразу на странице:
- `Watch Brief`
- `Executive Brief Card`
- `Monitoring Card`
- `Scenario Branch Card`
- `Risk Map Fragment`
- `Dashboard Widget`
- `Impact Rail Item`

### Артефакты второго уровня

Это то, что раскрывается по клику:
- `Interpretation Detail`
- `Mechanism Chain View`
- `Evidence Cluster`
- `Signal Timeline`
- `Propagation Detail`
- `Beneficiary / Loser Rationale`
- `Trigger / Disconfirmation Detail`

### Артефакты глубинного уровня

Это уже core Horizon surfaces:
- graph;
- map;
- lens;
- fragment profile;
- materialized artifact detail;
- exposure path detail;
- source / evidence viewer.

## Модель управления пользователем

High-grade интерфейс должен давать пользователю чувство контроля, но не перегружать его формами.

### Управление scope

Пользователь должен уметь:
- выбрать `Global`, `My Watch`, `Combined`;
- переключать service-line emphasis;
- выбирать target type;
- фиксировать регионы, рынки, corridors, entities, lenses;
- открывать curated watch packs.

### Управление плотностью и режимом чтения

Пользователь должен уметь:
- выбрать compact / comfortable density;
- включить focus mode;
- переключить emphasis на dashboards / briefs / map / scenarios;
- заморозить realtime;
- включить `since last visit`.

### Управление доверием и строгостью

Пользователь должен уметь:
- поднять minimum confidence;
- включить `Trusted Evidence Only`;
- скрыть speculative interpretations;
- включить `show disconfirmers`;
- переключить short / medium / long horizons.

### Управление провалом вглубь

Пользователь должен уметь:
- быстро раскрыть brief inline;
- открыть side detail;
- уйти в full graph/map/lens surface;
- возвращаться назад без потери контекста.

## Minimum-click и latency-first принципы

Для `Horizon Watch` недостаточно быть сильным по смыслу.
Он обязан быть быстрым по ощущению.

Пользователь должен доходить до meaningful result за минимальное число действий и без ощущения, что система “думает слишком долго”.

### Product rule: minimum clicks to value

Целевой принцип:
- первый meaningful insight должен быть доступен без дополнительных действий;
- второй уровень понимания должен открываться максимум за 1 клик;
- core proof layer должен быть доступен максимум за 2 клика;
- уход в graph / map / lens допустим как глубокий drill-down, но не должен быть обязательным для базового понимания.

Практически это означает:
- `Situation Bar` уже даёт тезис;
- `Watch Brief` уже даёт смысл и последствия;
- inline expand даёт mechanism/evidence preview;
- только потом пользователь уходит в полноразмерные surfaces.

### Interaction budget

Желаемая модель:
- `0 clicks`: увидеть, что важно сейчас;
- `1 click`: понять, почему это важно;
- `2 clicks`: увидеть path / evidence / assumptions;
- `3+ clicks`: уйти в экспертную глубину graph/map/scenario.

Если для базового ответа на вопрос `что происходит и почему это важно` нужно больше, интерфейс спроектирован плохо.

## Latency strategy

`Horizon Watch` должен работать в режиме `latency-first`.

Это означает:
- deterministic and cached answers first;
- async refinement second;
- LLM only when it действительно добавляет ценность;
- stale-but-good answer лучше, чем пустой loading state.

### Целевой порядок ответа

1. Отдать готовый read-model немедленно, если он уже есть.
2. Если read-model слегка устарел, отдать stale snapshot + пометить refresh.
3. Параллельно запустить revalidation / refinement.
4. Только если нужен новый interpretation pass, запускать async/controlled LLM path.

### UX-правило

Пользователь не должен ждать LLM ради:
- открытия страницы;
- смены фильтра;
- перехода между service-line режимами;
- просмотра уже известных briefs;
- открытия detail panel по готовому артефакту.

LLM допустим только для:
- новой сложной интерпретации;
- сложного scenario synthesis;
- refinement там, где deterministic layer явно недостаточен;
- background reframing после появления новых данных.

## Latency budget

Ниже не SLA, а целевой продуктовый budget.

### P0 interactions

Это то, что должно быть почти мгновенным:
- открытие `/horizon/watch`;
- переключение `Global / My / Combined`;
- открытие already-materialized brief;
- раскрытие inline details;
- переключение quick filters;
- opening map fragment from cached context.

Цель:
- perceived response: `~100-300ms`
- hard upper bound для большинства таких действий: `< 500ms`

### P1 interactions

Это то, что может требовать чуть больше времени, но всё ещё должно ощущаться быстрым:
- opening detail drawer with evidence cluster;
- loading target-specific monitoring panel;
- loading branch comparison from precomputed scenario workspace;
- opening artifact detail with supporting overlays.

Цель:
- perceived response: `~300-800ms`
- upper bound: `< 1.5s`

### P2 interactions

Это уже тяжёлые операции:
- сборка нового scenario branch;
- новая interpretation synthesis;
- сложный graph drill-down, который требует пересчёта;
- first-time scope assembly without warm cache.

Цель:
- synchronous phase: отдать shell + partial result сразу;
- background computation: async;
- UI должен показывать progressive readiness, а не блокирующий спиннер.

## Sync / async execution model

Нужно разделить все действия на три класса.

### 1. Fully synchronous

Без LLM и без тяжёлых пересчётов:
- чтение готовых briefs;
- чтение dashboards;
- фильтрация read-model;
- открытие detail drawers;
- map fragment rendering из готового data slice.

### 2. Synchronous with background refresh

Используется для:
- slightly stale read-models;
- recently changed watch targets;
- refresh after new propagation wave;
- refresh after new evidence arrived.

Поведение:
- сразу отдать последнюю хорошую версию;
- в фоне пересчитать;
- мягко обновить через realtime.

### 3. Async with progressive UI

Используется для:
- новой LLM-backed интерпретации;
- first-time scenario generation;
- complex what-if synthesis;
- expensive artifact materialization.

Поведение:
- отдать промежуточный deterministic skeleton;
- показать, что refined interpretation is in progress;
- после готовности обновить конкретные блоки, а не весь экран.

## Redis и кеширование

Если в инфраструктуре уже есть Redis, `Horizon Watch` должен использовать его активно как operational cache layer.

### Что нужно кешировать

1. Page-level read-models
- `Global Watch` snapshot
- `My Watch` snapshot
- combined mode snapshot

2. Brief-level artifacts
- `WatchBrief`
- `InterpretationThesis`
- `MechanismChain` summaries
- `Beneficiary / Loser` summaries

3. Dashboard read-models
- geographic pressure aggregates
- monitoring counters
- scenario comparison aggregates

4. Map fragment read-models
- prepared object subsets
- route overlays
- viewport/caption bundles

5. Detail surfaces
- evidence clusters
- propagation summaries
- scenario branch summaries

### Что не надо кешировать бездумно

- бесконечно сырые source documents;
- всё подряд на уровне низкоуровневых graph responses без access pattern;
- highly user-specific heavy payloads без нормальных keys and invalidation rules.

### Cache strategy

Предпочтительная стратегия:
- `cache-aside` для read-models;
- `stale-while-revalidate` для UI-critical surfaces;
- short-lived hot cache для dashboards;
- medium-lived cache для interpretation summaries;
- event-driven invalidation after new signals / propagation / artifact updates.

### Cache keys

Ключи должны учитывать:
- tenant;
- project;
- user or global mode;
- service-line mode;
- scope hash;
- filters hash;
- horizon window;
- version of interpretation/read-model schema.

## LLM gating и cost/latency discipline

LLM не должен быть default path для пользовательского действия.

### LLM вызывается только если

- deterministic synthesis не даёт приемлемого explanation;
- нужен новый strategic interpretation;
- нужен сложный scenario branch;
- есть конфликтующие сигналы, требующие critic/refiner pass;
- пользователь явно запросил более глубокий generated reasoning.

### LLM не вызывается для

- простого открытия страницы;
- смены режимов и фильтров;
- уже собранных brief surfaces;
- большинства dashboard interactions;
- обычных map / graph drill-downs;
- простого открытия cached scenario branch.

### Правило качества

Сначала:
- rules;
- graph math;
- propagation;
- cached interpretations;
- existing materialized paths.

Потом, если нужно:
- LLM refinement.

Это полностью соответствует общей логике Horizon: `rule-first, deterministic-by-default, LLM as controlled fallback`.

## Progressive rendering

Даже когда система вынуждена считать дольше, интерфейс не должен выглядеть пустым.

### Что отдаём сразу

- текущий brief shell;
- headline и status;
- last known confidence;
- last updated timestamp;
- quick explanation bullets;
- cached dashboards;
- map fragment placeholder with caption if geometry is known.

### Что может доезжать позже

- refined interpretation;
- updated mechanism chain;
- beneficiary / loser reshuffle;
- new evidence cluster;
- scenario branch enrichment;
- richer map overlay details.

### UI behavior

- обновляются только изменившиеся блоки;
- не пересобираем всю страницу;
- не теряем scroll / local focus;
- показываем `updated` or `refined` markers.

## Realtime + cache coherence

Realtime в `Horizon Watch` должен работать в связке с cache invalidation.

Новая волна сигналов или propagation не должна требовать полного hard refresh.

Правильное поведение:
- событие приходит;
- invalidates or patches related Redis read-model;
- UI получает WS event;
- перезагружает только нужный slice or block;
- старый usable state остаётся на месте до прихода нового.

## Acceptance criteria для скорости и UX-эффективности

Слой можно считать удачным, если:
- пользователь получает первый meaningful insight без кликов;
- path to explanation укладывается в 1 клик;
- path to proof укладывается в 2 клика;
- большая часть page opens и filter changes не требует LLM;
- cached read-model покрывает основной пользовательский трафик;
- stale-but-usable surfaces показываются вместо пустого loading;
- realtime refresh не ломает чтение и не сбрасывает контекст;
- тяжёлые LLM-backed операции почти всегда идут в async refinement mode.

## Как эти принципы реализуются в системе

Ниже фиксируется уже не только желаемое поведение, а практический способ реализации поверх текущего Horizon.

Главный принцип:

`Horizon Watch` не строится как “ещё один UI к графу”.
Он строится как отдельный snapshot/read-model слой над уже существующими модулями Horizon.

### Что уже можно переиспользовать

В текущей системе уже есть сильные опоры:
- interpretation cache и interpreter;
- propagation engine;
- scenario runner;
- map activity engine;
- realtime broker через Pub/Sub + WebSocket;
- lens and artifact infrastructure.

Это означает:
- не нужно строить второе аналитическое ядро;
- нужно собрать watch-oriented read-model layer над уже работающими частями.

## Implementation shape

### 1. Watch Read-Model Builder

Нужен отдельный backend-компонент:
- `WatchReadModelBuilder`

Его задача:
- читать core Horizon artifacts;
- собирать user-facing snapshots;
- не делать тяжёлую аналитику в момент запроса, если её можно подготовить заранее;
- быть главным producer для `/api/horizon/watch`.

Он должен строить как минимум:
- `GlobalWatchSnapshot`
- `UserWatchSnapshot`
- `CombinedWatchSnapshot`
- `BriefDetailSnapshot`
- `DashboardSnapshot`
- `ScenarioWorkspaceSnapshot`

### 2. Источники данных для Builder

Builder читает:
- `OntologyFact`
- `SignalDescriptor`
- `Propagation` state / traces / overlays
- `MaterializedArtifact`
- `ExposurePath`
- `Interpretation*`
- `Lens`
- map activity / map object slices
- user watch preferences

То есть Builder ничего не “выдумывает”.
Он агрегирует и упаковывает уже существующие артефакты в формы, удобные для UI.

### 3. Read-model first, not query fan-out from UI

UI не должен по открытию страницы ходить:
- отдельно за сигналами;
- отдельно за paths;
- отдельно за interpretations;
- отдельно за map objects;
- отдельно за scenario metadata.

Вместо этого:
- сервер отдаёт already-shaped snapshot;
- UI лишь раскрывает и фильтрует его;
- глубокие drill-down вызывают отдельные targeted endpoints.

Это ключевой механизм и для speed, и для minimum-click UX.

## Redis как hot cache над durable state

На практике я бы делал двухслойную схему:

### Durable layer

Для устойчивого хранения и совместимости с уже существующей системой:
- Firestore / текущие horizon collections;
- существующие persisted caches и interpretation artifacts.

### Hot layer

Для быстрых UI-ответов:
- Redis.

Роль Redis:
- хранить самые частые page snapshots;
- хранить brief-level summaries;
- хранить dashboard aggregates;
- хранить map fragment payloads;
- хранить scenario workspace summaries.

Это позволит:
- не пересобирать страницу каждый раз из десятков коллекций;
- быстро отдавать `stale-but-good`;
- держать filter switches и page opens в пределах P0/P1 latency.

## Схема обновления данных

### 1. Event arrives

Новый input приходит в систему:
- fact;
- signal;
- propagation result;
- artifact update;
- scenario recomputation;
- user watch change.

### 2. Affected watch scopes are resolved

Watch layer определяет:
- какие global slices затронуты;
- какие user watch targets затронуты;
- какие dashboards стали stale;
- какие map fragments или scenario branches надо обновить.

### 3. Redis invalidation or patch

Далее:
- либо инвалидируется соответствующий snapshot;
- либо в Redis вносится patch/update;
- либо ставится флаг `needs_refinement`.

### 4. Realtime event is published

После этого публикуется watch-oriented realtime event.

Например:
- `horizon.watch.snapshot.updated`
- `horizon.watch.brief.upserted`
- `horizon.watch.dashboard.updated`
- `horizon.watch.scenario.updated`

### 5. UI patches only local blocks

UI не делает hard refresh страницы.

Он:
- обновляет только нужный block;
- сохраняет scroll и local focus;
- показывает `updated` / `refined`;
- при необходимости подтягивает новый snapshot для конкретного slice.

## Как реализуется minimum-click UX

Это достигается не только дизайном, а формой backend-ответа.

### Что отдаётся на page open

`GET /api/horizon/watch` должен уже содержать:
- headline;
- top global briefs;
- top my-watch briefs;
- top dashboards;
- impact rail preview;
- compact map fragment refs;
- `updated since last visit` summary;
- top scenario pointers, если есть.

То есть базовый ответ already contains product value.

### Что раскрывается за 1 клик

Открытие brief должно давать:
- interpretation summary;
- main mechanism chain;
- top affected targets;
- top evidence preview;
- main map/path context.

### Что раскрывается за 2 клика

Дальше идут:
- full evidence cluster;
- path detail;
- propagation detail;
- scenario assumptions;
- beneficiary / loser rationale.

Если пользователю для базового понимания приходится идти глубже, значит read-model слишком тонкий.

## Как реализуется latency-first

### Rule 1. Cached snapshot first

Первый ответ почти всегда должен идти из:
- Redis hot cache;
- либо из persisted ready snapshot.

### Rule 2. Deterministic computation second

Если snapshot отсутствует, но всё можно собрать без LLM:
- собрать сервером синхронно;
- закешировать;
- вернуть.

### Rule 3. LLM refinement third

Если нужен LLM:
- вернуть deterministic shell;
- поставить async refinement task;
- после готовности обновить только соответствующий блок.

Это особенно важно для:
- interpretation reframes;
- complex scenario synthesis;
- beneficiary / loser explanations;
- multi-step strategic hypotheses.

## Как реализуется LLM gating

Практический принцип:

`LLM should refine surfaces, not block surfaces`

### Что делаем без LLM

- ranking;
- aggregation;
- watch filtering;
- snapshot assembly;
- map fragment packaging;
- path previews;
- propagation summaries;
- most dashboards;
- already-known scenario branch reads.

### Где LLM действительно нужен

- новый сложный interpretation pass;
- synthesis для strategic thesis;
- difficult scenario branch generation;
- conflict resolution when deterministic logic is insufficient;
- optional higher-grade narrative refinement.

### Как это сочетается с текущим Horizon

Это уже соответствует текущим архитектурным направлениям в системе:
- rule-first;
- deterministic-by-default;
- async LLM для сложных решений;
- cached interpretation where possible.

То есть `Horizon Watch` не требует философского разворота системы, только правильной orchestration layer поверх уже существующих практик.

## Как реализуется каждая service line

### Geopolitical Risks Overview

Реализация:
- precomputed macro scopes;
- aggregate regional/corridor snapshots;
- interpretation layer поверх macro signals and paths;
- executive dashboards из hot cache;
- map fragments из existing map stack.

Основной объект:
- `ExecutiveBriefSnapshot`

### Deep Monitoring

Реализация:
- user watch targets хранятся в профиле;
- для каждого target собирается `MonitoringTargetSnapshot`;
- updates идут от watchdog + signal pipeline + propagation;
- interpretation deltas собираются отдельно;
- UI получает tracked operational surface.

Основной объект:
- `MonitoringDeltaSnapshot`

### Scenario Planning

Реализация:
- baseline scope -> domain contract semantics -> scenario runner;
- precomputed common branches;
- async custom branch synthesis;
- projected paths and overlays materialize into scenario read-model;
- branch comparison и trigger logic отдаются как отдельный workspace snapshot.

Основной объект:
- `ScenarioWorkspaceSnapshot`

## Как это ложится на существующие модули

Ниже практическое отображение на уже существующую кодовую базу.

### Interpretation

Переиспользуем:
- текущий interpreter;
- текущий interpretation cache;
- current interpretation processing pipeline.

Что добавляем:
- watch-oriented summary packaging;
- separation between durable interpretation storage and hot UI cache.

### Propagation

Переиспользуем:
- propagation engine;
- signal traces;
- path scoring and endpoint scoring;
- policy impact semantics.

Что добавляем:
- read-model packaging for impact rails and brief consequences;
- patch-triggering updates for affected watch scopes.

### Scenario

Переиспользуем:
- scenario runner;
- contract semantics;
- scenario propagation.

Что добавляем:
- scenario workspace snapshots;
- branch comparison packaging;
- trigger/disconfirmation panels;
- projected path and map-fragment read-models.

### Map

Переиспользуем:
- existing map stack;
- geo objects;
- zones;
- transport flows;
- path legs;
- map activity engine.

Что добавляем:
- `MapFragmentReadModel`;
- compact viewports with captions;
- watch-aware map drill-down routing.

### Realtime

Переиспользуем:
- existing Pub/Sub + WS broker.

Что добавляем:
- watch-specific event types;
- block-level UI patch flow;
- cache invalidation / refresh coordination.

## Практический rollout

Лучше делать это поэтапно.

### Phase 1

- `WatchReadModelBuilder`
- page snapshots
- global + my watch
- Redis hot cache
- block-level realtime refresh

### Phase 2

- interpretation refinement layer
- beneficiary / loser surfaces
- richer dashboards
- map fragments
- detail drawers

### Phase 3

- scenario workspace
- branch comparison
- projected overlays
- triggers / disconfirmers
- advanced watch packs

## Критический architectural rule

Если коротко, то реализация должна следовать одному правилу:

`Do not let the UI assemble meaning from raw Horizon parts at request time`

Нужно наоборот:
- система собирает meaning заранее или почти заранее;
- Redis и read-model layer делают это быстрым;
- realtime делает это живым;
- LLM делает это глубже, но не тормозит базовый ответ.

## Placement inside Horizon

`Horizon Watch` должен жить внутри `Horizon`, но не в одном-единственном режиме навигации.

Правильная модель здесь двухуровневая.

### 1. Tenant / global-wide Watch

Это отдельная верхнеуровневая поверхность Horizon.

Смысл:
- смотреть мир широко;
- видеть global or tenant-wide picture;
- работать с macro slices, regions, corridors, markets, policy surfaces;
- использовать watch как соседний по уровню режим к `World Map`.

Навигационно это должно быть доступно прямо из sidebar Horizon, на том же уровне, где пользователь воспринимает global modes.

Принцип:
- `Watch` не должен быть спрятан глубоко внутрь lens-only UX;
- для обзорного и monitoring-first использования он должен быть top-level destination.

### 2. Lens-wide Watch

Одновременно `Watch` должен быть встроен и в lens-scoped контекст.

Смысл:
- смотреть not the whole world, а “what matters inside this lens”;
- видеть interpretation, deltas, paths и scenarios в рамках конкретного Lens scope;
- работать с тем же watch language, но на более узком, операторском или аналитическом контексте.

Это значит, что из Lens пользователь должен иметь прямой переход в:
- `Lens Watch`
- `Lens Scenarios`
- `Lens Monitoring`

без ощущения, что он покинул Horizon и попал в другой продукт.

### Правило информационной архитектуры

`Watch` не является просто “одним из способов показать карту”.

И в то же время он не должен быть только top-level screen.

Правильнее так:
- как global mode он соседствует с `World Map`;
- как lens mode он встраивается в конкретный `Lens`;
- визуальный и смысловой язык остаётся единым;
- меняется только scope.

### Как это может выглядеть в навигации

На уровне Horizon sidebar:
- `Watch`
- `World Map`
- `Lenses`

При этом внутри выбранного Lens:
- `Cockpit`
- `Watch`
- `Scenarios`
- `Map`

То есть:
- top-level `Watch` отвечает за tenant/global-wide monitoring;
- lens-level `Watch` отвечает за scoped interpreted monitoring;
- обе поверхности используют одни и те же read-model principles и artifacts.

### Routing model

Рекомендуемая модель маршрутов:
- `/horizon/watch` — global / tenant-wide watch
- `/horizon/watch?lensId=...` или аналогичный scoped mode — lens-aware watch

Важно не столько имя route, сколько то, чтобы обе формы существования были first-class.

### Почему это важно

Если оставить `Watch` только как global screen:
- он будет слишком общий для operational work inside a lens.

Если оставить `Watch` только внутри Lens:
- потеряется стратегическая обзорная ценность и публично обещанная service-line логика.

Поэтому `Horizon Watch` должен жить:
- и на уровне wide situational awareness;
- и на уровне scoped analytical work.

## Лучшие практики для такого класса систем

Ниже фиксируются не vendor-specific референсы, а практики, которые характерны для сильных intelligence / operations / decision-support systems.

### 1. Summary first, proof immediately available

Сильная система всегда начинает с:
- тезиса;
- статуса;
- последствий.

Но рядом должна быть короткая дорога к:
- evidence;
- paths;
- assumptions;
- disconfirmers.

### 2. Surface hierarchy instead of flat dashboards

Слабая система показывает двадцать одинаково важных виджетов.
Сильная система строит иерархию:
- what matters now;
- why it matters;
- what changed;
- where to go deeper.

### 3. One semantic core, many professional surfaces

Если geopolitics, monitoring и scenario живут на разных схемах, система распадётся.
High-grade практика здесь:
- одно ядро данных;
- несколько read-model surfaces;
- единая drill-down логика.

### 4. Delta is often more valuable than state

В таких системах критично показывать не только состояние, но и:
- что изменилось;
- что усилилось;
- что ослабло;
- что впервые стало material.

### 5. Paths beat isolated facts

Пользователь лучше понимает риск, когда видит:
- маршрут;
- цепочку зависимости;
- propagation;
- governance or regulatory path;

а не просто отдельную карточку факта.

### 6. Map only when geography is explanatory

Карта должна объяснять, а не украшать.

### 7. Scenario needs triggers and disconfirmers

Любой planning mode без:
- trigger conditions;
- branch comparison;
- disconfirmation logic;

деградирует в красивую фантазию.

### 8. Realtime must feel controlled

Профессиональный интерфейс не должен дёргаться.
Обновления должны быть:
- мягкими;
- локальными;
- агрегированными;
- pauseable.

## Как данные берутся из системы и превращаются в surfaces

Ниже фиксируется canonical flow для `Horizon Watch`.

### Layer 1. Core inputs

Базовые входы:
- `OntologyFact`
- `SignalDescriptor`
- `Propagation` outputs
- `MaterializedArtifact`
- `ExposurePath`
- `Lens`
- map objects / zones / flows / path legs
- user watch preferences

Это raw system layer.

### Layer 2. Interpretation and synthesis

Поверх core inputs строятся:
- `InterpretationThesis`
- `MechanismChain`
- `TrustShift`
- `StrategicShiftHypothesis`
- `InterpretationDelta`
- `Beneficiary / Loser Map`
- `Disconfirming Signal Set`

Это synthesis layer.

### Layer 3. Read-models for UI

Поверх synthesis layer строятся UI-ориентированные read-models:
- `WatchBrief`
- `DashboardReadModel`
- `MapFragmentReadModel`
- `MonitoringDeltaReadModel`
- `ScenarioBranchReadModel`
- `ImpactRailReadModel`

Это presentation layer.

### Layer 4. UI surfaces

Из read-model рождаются:
- executive surfaces;
- monitoring surfaces;
- scenario surfaces;
- detail drawers;
- graph/map drill-downs.

Это user-facing layer.

## Правила интерпретации данных

Система не должна напрямую рендерить сырые данные в интерфейс как будто они уже “понятны”.

### Raw fact не равен insight

`OntologyFact` или incoming signal сам по себе не является продуктовым объектом.

Система обязана спросить:
- к какому scope это относится;
- что это меняет относительно baseline;
- как это распространяется;
- кого это задевает;
- какой у этого вероятный смысл;
- что это может опровергнуть.

### Signal не равен conclusion

Сигнал нужен для:
- обнаружения;
- ранжирования;
- запуска propagation;
- сборки interpretation.

Но не для прямой публикации “вывода”.

### Path + interpretation = high-grade insight

Сильный пользовательский инсайт рождается, когда система соединяет:
- signal pressure;
- graph/path geometry;
- interpretation layer;
- visible consequences;
- confidence and disconfirmation.

### Contract-driven semantics for scenarios

Для `Scenario Planning` интерпретация должна идти не свободным нарративом, а через те семантики, которые уже есть в системе:
- `supply_chain`
- `governance`
- `regulatory impact`

Это критично для связности продукта.

## Какие read-models нужны дополнительно

Чтобы всё это работало аккуратно, кроме уже описанных сущностей потребуется набор специализированных read-models.

### ExecutiveReadModel

Для `Geopolitical Risks Overview`:
- top regional briefs;
- strategic pressure counters;
- macro dashboards;
- beneficiary / loser summaries;
- top map fragments.

### MonitoringReadModel

Для `Deep Monitoring`:
- watched targets;
- deltas since last visit;
- interpretation changes;
- escalation markers;
- signal maturity states;
- target-specific mini maps.

### ScenarioWorkspaceReadModel

Для `Scenario Planning`:
- baseline state;
- branches;
- triggers;
- disconfirmers;
- projected paths;
- contract overlays;
- branch comparison metrics.

## Кейсовый каталог для Horizon Watch

Ниже зафиксирован стартовый каталог case families, которые уже хорошо ложатся на существующие данные и логику.

### Геополитика и безопасность

- Gulf escalation and perception shifts
- Red Sea / Suez / Bab el-Mandeb monitoring
- Eastern Mediterranean power projection
- Black Sea grain and logistics stress
- sanctions and export-control pressure mapping

### Индустрия и технологии

- semiconductor exposure and fab concentration
- AI infrastructure dependencies
- telecom / 5G vendor substitution
- drone / defense technology diffusion
- critical equipment and industrial localization

### Энергия и сырьё

- oil and gas corridor exposure
- LNG and maritime dependency
- pipeline and terminal risk
- critical minerals supply pressure
- energy-linked industrial downstream exposure

### Логистика и инфраструктура

- ports, rail, and route disruption
- chokepoint dependency
- downstream manufacturing exposure
- route rerouting and substitute corridor viability
- infrastructure cluster vulnerability

### Политическая экономия и доверие

- trust erosion and hedge behavior
- alliance ambiguity and sovereign buildout
- local champion acceleration
- procurement diversification
- investment and capex reweighting

## Основные сущности Horizon Watch

### 1. Watch Slice

Базовая единица интерфейса.

`Watch Slice` — это наблюдаемый срез мира, по которому система строит risk brief, hotness, impact paths, interpretation layer и evidence.

Примеры:
- страна: `Germany`
- макрорегион: `South China Sea`
- рынок: `Semiconductors`
- коридор: `Red Sea shipping corridor`
- сущность: `TSMC`
- материализованный путь: конкретный `SUPPLY_CHAIN_PATH`
- query-address scope: например весь сектор `/query/sector:/Technology/Semiconductors/**`

### 2. Watch Target

Объект, который пользователь добавляет “в наблюдение”.

Типы `WatchTarget` v1:
- `country`
- `macro_region`
- `market`
- `sector`
- `corridor`
- `fragment`
- `lens`
- `query_address`
- `materialized_artifact`
- `exposure_path`

### 3. Watch Brief

Синтетический артефакт для UI по конкретному `Watch Slice`.

Это не первичная истина, а производный read-model, содержащий:
- headline;
- summary;
- hotness;
- confidence;
- risk posture;
- dominant themes;
- top affected targets;
- top interpretations;
- top signals;
- top evidence;
- propagation summary;
- ссылки на underlying graph/artifacts/sources.

### 4. Watch Feed Entry

Отдельное изменение внутри slice:
- новый сигнал;
- усиление существующего сигнала;
- materialized path update;
- propagation outcome;
- существенная смена оценки.

## Что пользователь может добавлять в Watch

Пользователь может выбрать интересующее его через поиск, карту, граф или карточки:
- страны;
- макрорегионы;
- рынки;
- отрасли;
- транспортные и supply-chain коридоры;
- конкретные организации, объекты, правительства, программы, династии и прочие `HorizonFragment`;
- готовые `Lens`;
- materialized supply chain paths;
- exposure paths;
- query-address срезы.

Любой такой выбор должен иметь кнопку:
- `Add to Watch`

После добавления объект:
- сохраняется в профиле пользователя;
- начинает участвовать в персональном разделе страницы;
- получает realtime-обновления;
- может использоваться для приоритизации Watchdog / rollup / alerting в будущем.

## Главный UX-сценарий

### Сценарий 1. Пользователь открывает страницу без личных подписок

Система показывает:
- глобальный набор самых горячих срезов;
- curated макрорегионы;
- горячие рынки;
- важнейшие цепочки влияния;
- интерпретации по самым важным зонам давления;
- объяснение “why it matters”.

Это режим:
- `Global Watch`

### Сценарий 2. Пользователь выбирает интересующие области

Пользователь:
- ищет страну, рынок, компанию или коридор;
- нажимает `Add to Watch`;
- видит, что новый target появился в персональном watch;
- при следующем входе видит те же selections восстановленными.

Это режим:
- `My Watch`

### Сценарий 3. Пользователь читает карточку и уходит глубже

Из карточки он может открыть:
- граф по affected paths;
- профиль фрагмента;
- список сигналов;
- evidence / sources;
- materialized supply chain path;
- соответствующий Lens;
- карту.

### Сценарий 4. Пользователь фильтрует экран

Фильтры v1:
- geography;
- watch target type;
- market / sector;
- risk / opportunity / disruption polarity;
- time horizon `24h / 7d / 30d`;
- confidence floor;
- only my watch / global + my watch;
- only with impact paths;
- only with evidence from trusted sources.

## Информационная архитектура страницы

Рекомендуемый маршрут:

- `/horizon/watch`

Страница делится на восемь зон.

### 1. Hero / Situation Bar

Верхний блок, который мгновенно создаёт ощущение “живого risk surface”.

Содержит:
- headline дня;
- количество активных hot slices;
- сколько material risk changes произошло за период;
- сколько объектов в `My Watch` сейчас под давлением;
- переключатель `Global Watch / My Watch / Combined`.

### 2. Watch Picker

Универсальный блок добавления в watch.

Входы:
- text search;
- suggested entities;
- suggested countries / markets / corridors;
- быстрые чипы популярных тем.

Действия:
- `Add to Watch`
- `Open in Graph`
- `Open profile`

### 3. Global Hot Slices

Набор карточек по умолчанию для всех пользователей.

Это не случайные новости, а top-ranked risk slices по hotness и consequence depth.

Примеры:
- `Red Sea logistics pressure rising`
- `Taiwan semiconductor exposure tightening`
- `Germany industrial power cost stress`
- `Black Sea grain corridor uncertainty`

### 4. My Watch Grid

Персональный раздел пользователя.

Каждая карточка показывает:
- объект наблюдения;
- изменение за период;
- affected paths;
- top evidence;
- confidence;
- CTA в graph/source/artifact/profile.

### 5. Impact Paths Rail

Отдельная колонка или горизонтальная полоса с “почему это важно”.

Показывает:
- materialized paths;
- exposure paths;
- propagation chains;
- top downstream affected fragments.

Это критический блок. Именно он делает экран “не новостным”.

### 6. Risk Map Fragments

Отдельный визуальный слой для случаев, где spatial context критичен для понимания риска.

Это не “огромная карта как главный экран”.
Это встроенные контекстные фрагменты карты, которые показываются внутри briefs, рядом с impact rail или в detail panel, когда география и расположение объектов являются частью объяснения.

Показывать их нужно, когда есть хотя бы одно из:
- chokepoint;
- маршрут, коридор или path leg;
- пространственная близость риска к объекту;
- кластер инфраструктуры;
- порт, фабрика, месторождение, терминал, энергетический узел;
- propagation, который невозможно быстро понять без карты.

Типы `Risk Map Fragment`:
- `risk vicinity map`
- `corridor segment map`
- `supply path geo fragment`
- `cluster pressure map`
- `before/after impact map`

Каждый map fragment должен:
- быть компактным;
- отвечать на один конкретный тезис;
- иметь краткий explanatory caption;
- вести в полноценный map context при клике.

### 7. Watch Dashboards

Отдельный слой компактных dashboard-модулей, но не основа всего продукта.

Типы dashboard-блоков v1/v2:
- geographic heat;
- market stress;
- corridor pressure;
- source reliability mix;
- propagation load;
- top impacted watched targets.

Правило:
- dashboards помогают быстро сканировать обстановку;
- dashboards не подменяют собой brief layer;
- любой виджет должен вести в watch brief, graph или evidence, а не быть тупиковым числом.

### 8. Live Signal Stream

Компактный поток свежих changes:
- новые сигналы;
- эскалации;
- propagation completions;
- артефакты, пересобранные после изменений.

Поток должен быть коротким и отобранным, а не бесконечным.

## Анатомия Watch Card

Каждая карточка `Watch Slice` должна содержать:

- `title`
- `subtitle`
- `location / market / scope badge`
- `hotness score`
- `confidence`
- `time horizon`
- `one-sentence briefing`
- `top drivers` (2-4)
- `who gets hit` (affected fragments / sectors / markets)
- `why now`
- `evidence count`
- `path count`
- `actions`

Действия v1:
- `Open Graph`
- `Open Sources`
- `Open Evidence`
- `Open Path`
- `Add/Remove Watch`

## UX Layout Spec

Этот раздел фиксирует уже не только концепцию, а конкретную композицию страницы и поведение основных блоков.

### Главный принцип композиции

Экран должен читаться в три волны:
- сначала пользователь считывает общую обстановку;
- затем понимает, что важно лично для его watch;
- затем уходит в explanation layer через пути, evidence и граф.

То есть eye-flow такой:

`Situation -> My pressure -> Why it matters -> Drill-down`

### Desktop layout

Desktop-версия должна использовать широкий editorial layout, а не классическую админскую сетку.

Рекомендуемая композиция:
- сверху full-width `Situation Bar`;
- под ним full-width `Watch Picker`;
- ниже двухслойный основной экран;
- внутри основного экрана доминируют briefs, а не charts;
- `Risk Map Fragments` встраиваются в briefs и detail surfaces там, где это нужно;
- dashboards и live stream работают как supporting rails.

#### Desktop grid

Рекомендуемая логика колонок:
- левая колонка `2.5-3fr`: `Global Hot Slices` и `My Watch Grid`;
- правая колонка `1.2-1.5fr`: `Impact Paths Rail`, `Risk Map Fragments`, `Watch Dashboards`, `Live Signal Stream`;
- при необходимости сверху между ними может встраиваться compact world/map panel.

#### Desktop reading order

Порядок чтения:
1. Situation Bar
2. Watch Picker
3. первый ряд глобальных hottest briefs
4. первый ряд персональных watch briefs
5. impact rail
6. context map fragments
7. dashboards
8. live stream

### Mobile layout

На мобильном нельзя просто “сжать desktop”.

Должен быть другой ритм:
- sticky compact header;
- first screen = один мощный hero brief + переключатель режима;
- затем горизонтальный scroller с my watch chips;
- затем stack из watch cards;
- impact rail превращается в горизонтальный narrative rail;
- критичные map fragments показываются inline между карточками;
- dashboards сворачиваются в 2-3 критичных summary blocks.

#### Mobile order

Порядок блоков:
1. compact Situation Bar
2. mode toggle
3. watch chips
4. hero brief
5. my watch cards
6. global hot cards
7. impact rail
8. risk map fragments
9. compact dashboards
10. live stream drawer

### Первый экран

Первый экран должен отвечать на вопрос:

`What should I care about right now?`

На первом экране нельзя тратить пространство на длинные control panels или тяжелые таблицы.

Обязательные элементы above the fold:
- headline / global pressure statement;
- режим `Global / My / Combined`;
- краткие counters;
- `Add to Watch` affordance;
- 1-3 strongest briefs.

### Situation Bar: точная роль

`Situation Bar` не просто header.

Это слой мгновенной ориентации.

Он должен содержать:
- главный синтетический тезис периода;
- current risk climate indicator;
- число новых важных reframes за период;
- число watched targets under pressure;
- last refresh / realtime indicator;
- mode toggle.

#### Примеры headline-формата

Хороший headline:
- `Industrial pressure in Europe is widening beyond energy into materials and manufacturing.`
- `Maritime chokepoint stress is spreading into downstream delivery risk.`
- `Semiconductor exposure is tightening across Taiwan-linked manufacturing chains.`

Плохой headline:
- `27 new events detected`
- `Breaking news from multiple countries`

### Watch Picker: interaction contract

`Watch Picker` должен ощущаться как command bar для наблюдаемого мира.

Сценарий:
1. пользователь начинает печатать;
2. система предлагает смешанные target types;
3. пользователь видит, что можно добавить не только entity, но и country, market, corridor, lens, path;
4. после выбора появляется явный CTA `Add to Watch`;
5. после добавления target немедленно появляется в `My Watch`.

Рядом с каждым результатом поиска желательно показывать:
- тип;
- географию;
- краткое объяснение, почему это meaningful watch target;
- индикатор “already watched”, если объект уже сохранён.

### Global Hot Slices: layout contract

Этот блок должен выглядеть как curated wall из сильных briefs.

Правила:
- первая карточка может быть hero-size;
- ещё 3-6 карточек идут сеткой;
- карточки не одинаковы по весу: должна быть visual hierarchy;
- глобальные briefs не должны полностью вытеснять personal watch.

### My Watch Grid: layout contract

Это сердце страницы для возвращающегося пользователя.

Правила:
- pinned targets идут первыми;
- карточки с новыми изменениями поднимаются выше;
- должно быть видно, что именно изменилось с прошлого визита;
- карточка обязана показывать не только текущее состояние, но и delta.

Полезные micro-elements:
- `Updated since last visit`;
- `New downstream impact`;
- `Confidence increased`;
- `New evidence added`.

### Impact Paths Rail: layout contract

Это explanation spine всей страницы.

Если briefs отвечают на “что важно”, то rail отвечает на “почему это правда”.

Каждый элемент rail должен быть path-first:
- `A -> B -> C`;
- короткий explanatory sentence;
- severity / confidence;
- CTA в graph или artifact.

Допустимые типы rail item:
- `Supply chain path`
- `Exposure path`
- `Propagation chain`
- `Interpretation delta`

### Risk Map Fragments: layout contract

Map fragment должен объяснять пространственную геометрию риска за 2-4 секунды.

Правила:
- один fragment = один тезис;
- ограниченный viewport;
- 1-3 ключевых объекта в фокусе;
- короткий caption, объясняющий смысл;
- severity/impact overlays должны быть минимальными и читаемыми;
- click открывает существующий Horizon map context.

Что можно показывать:
- affected facilities;
- ports / pipelines / maritime routes;
- conflict radius / disruption zone;
- chokepoint pressure;
- downstream exposed geo objects;
- path legs для materialized routes.

Что нельзя:
- перегружать fragment большим числом слоёв;
- превращать его в mini-GIS;
- показывать карту без ясного explanatory caption.

Примеры caption:
- `Two fabrication clusters sit inside the widening disruption radius.`
- `This corridor segment feeds the downstream export path now under stress.`
- `Pressure concentrates around the port cluster anchoring the affected route.`

### Watch Dashboards: layout contract

Dashboards должны быть максимально утилитарными и вторичными.

Хороший dashboard-widget:
- визуально компактен;
- отражает состояние или динамику;
- ведёт в соответствующий brief;
- не требует долгого чтения.

Плохой dashboard-widget:
- большой красивый график без ясного action;
- много легенд и осей;
- самостоятельный аналитический мир без связи с briefs.

### Live Signal Stream: layout contract

`Live Signal Stream` должен передавать ощущение живой системы, но не доминировать.

Правила:
- максимум 5-8 элементов в видимой зоне;
- элементы короткие;
- у каждого элемента должен быть связанный brief или target;
- поток должен иметь `Pause` / `Resume`;
- при high update rate поток агрегируется.

## Карты в Horizon Watch

`Horizon Watch` не создаёт отдельную картографическую систему.

Нужно использовать ту же картографическую подложку, тот же map stack и те же базовые слои, которые уже есть в Horizon.

Это означает:
- reuse существующего map component layer;
- reuse существующей подложки карты;
- reuse текущих geo objects / zones / edges / flows / path legs;
- reuse текущей theme-интеграции и поведения карты;
- reuse существующего full map surface как target drill-down.

Нельзя:
- заводить отдельный map provider только для Watch;
- дублировать те же геоданные в отдельной watch-only схеме без необходимости;
- собирать визуально другой “мир карты”, который конфликтует с остальным Horizon.

### Map fragments как новый surface над существующей картой

`Risk Map Fragments` следует рассматривать как новый presentation surface над уже существующей Horizon map infrastructure.

То есть:
- данные и базовые слои остаются теми же;
- меняется только framing, crop, caption и UI-композиция;
- полный drill-down открывает ту же карту, которую система уже использует.

### Что именно переиспользуем

На уровне реализации нужно переиспользовать существующие возможности horizon-карты:
- geo objects;
- zones;
- edges;
- transport flows;
- path legs;
- связанные object detail / selection flows;
- existing full map panel.

### Когда карта обязательна

Карта должна становиться обязательной частью brief или detail view, если без пространственного контекста пользователь не поймёт risk geometry.

Типичные случаи:
- морские chokepoints;
- маршруты поставок;
- портовая и железнодорожная логистика;
- приграничное давление;
- proximity risk вокруг фабрик, баз, энергoобъектов и хабов;
- региональные кластеры производства;
- propagation через географически связанные объекты.

### Когда карта не нужна

Карту не нужно добавлять механически.

Если риск лучше объясняется через:
- ownership / control;
- corporate dependency;
- regulatory cascade;
- financial exposure;
- abstract dependency graph;

то лучше показывать path, graph overlay, chart или narrative brief без карты.

## Состояния страницы

### 1. Cold start

Пользователь впервые открыл страницу.

Показываем:
- curated global watch;
- объяснение, что сюда можно добавлять свои targets;
- несколько suggested watch targets.

### 2. Personal watch ready

Пользователь уже имеет сохранённые targets.

Показываем:
- `My Watch` первым или вторым экранным блоком;
- summary, сколько watched targets сейчас в норме / под давлением / требуют внимания;
- highlights since last visit.

### 3. High activity mode

Сигналов слишком много.

Поведение:
- усиленная агрегация stream;
- упор на interpreted briefs, а не на поток мелких изменений;
- sticky notice `High update activity`;
- предложение включить stricter filters.

### 4. Sparse mode

Изменений мало.

Поведение:
- меньше драматизации;
- больше long-horizon context;
- показывать slow-moving risk shifts и structural pressure, а не пытаться симулировать “срочность”.

### 5. Error / degraded mode

Если часть read-model недоступна:
- ситуация должна быть деградирующей, но не катастрофической;
- можно скрыть dashboards;
- можно показать stale badge на briefs;
- нельзя ронять весь экран, если временно недоступен один supporting block.

## Состояния карточки

У карточки должны быть разные визуальные состояния:
- normal;
- rising;
- critical;
- cooling down;
- stale;
- updating.

### Visual behavior

`rising`:
- мягкое усиление акцента;
- subtle pulse или border glow;
- badge `Escalating`.

`critical`:
- самый сильный визуальный вес;
- контрастный severity marker;
- первым показывается affected impact.

`cooling down`:
- меньше визуального напряжения;
- явный marker `Pressure easing`.

`stale`:
- приглушённая карточка;
- пометка, что evidence needs refresh.

`updating`:
- скелетон запрещён для всей карточки;
- обновляется локально и деликатно только изменившийся блок.

## Drill-down model

Из страницы должно быть несколько уровней провала.

### Уровень 1. Quick expand

Inline-expansion внутри карточки:
- дополнительные affected targets;
- top evidence previews;
- короткий path summary.

### Уровень 2. Brief detail panel

Side panel / modal / drawer:
- полный narrative brief;
- ranked drivers;
- evidence;
- impact paths;
- related watched targets;
- timeline of recent reframes.

### Уровень 3. Core Horizon surfaces

Переход в:
- graph;
- fragment profile;
- lens;
- map;
- source/evidence viewer;
- materialized artifact detail.

Правило:
- `Horizon Watch` не должен дублировать глубинные surfaces;
- он должен быстро доставлять в них.

## Поведение фильтров

Фильтры должны быть быстрыми и “editorial”, а не enterprise-form-like.

Нужны два уровня:
- быстрые chips above content;
- advanced filter drawer.

### Quick filters

Рекомендуемый набор:
- `24h`
- `7d`
- `30d`
- `Only My Watch`
- `Only Escalating`
- `Only With Paths`
- `Trusted Evidence`

### Advanced filters

Могут включать:
- geography tree;
- market / sector;
- target type;
- confidence range;
- polarity;
- severity;
- artifact availability;
- source quality.

## Dashboard widget catalog

Ниже зафиксирован рекомендуемый начальный каталог виджетов.

### Widget 1. Global Pressure Map

Показывает:
- страны/регионы с aggregated pressure;
- hover summary;
- click в соответствующий brief set.

Примечание:
- это overview map;
- она не заменяет context-specific `Risk Map Fragments` внутри briefs.

### Widget 2. Market Stress Board

Показывает:
- топ-рынки по растущему давлению;
- delta against previous period;
- click в market brief.

### Widget 3. Watched Targets Status

Показывает:
- сколько targets в `stable / watch / warning / critical`;
- click в filtered my watch grid.

### Widget 4. Propagation Load

Показывает:
- сколько существенных propagations произошло;
- где были самые длинные / значимые paths;
- click в impact rail.

### Widget 5. Evidence Strength Mix

Показывает:
- распределение сильных / средних / слабых evidence;
- помогает быстро понять, насколько текущая picture опирается на solid ground.

## Motion and visual rhythm

Анимация здесь должна работать на восприятие системы как живой, но не шумной.

Нужны:
- плавное появление briefs;
- мягкое обновление counters;
- аккуратные highlight transitions при realtime changes;
- subtle path reveal для impact rail.

Нельзя:
- агрессивные бесконечные анимации;
- flashing severity blocks;
- noisy tickers как на ТВ.

## Empty-state copy и тон

Тон страницы должен быть деловым, точным и сильным.

Не надо писать:
- `No news yet`
- `Nothing happened`

Лучше:
- `No material pressure shifts detected in your watch for this horizon.`
- `Your watch is quiet. Global structural pressure remains visible below.`

## Acceptance criteria для UX

UX раздел можно считать удачным, если:
- первый экран читается за 10-20 секунд;
- пользователь понимает difference between `global` и `my watch`;
- хотя бы один path-based explanation доступен без ухода далеко вглубь;
- dashboards помогают ориентироваться, но не перетягивают внимание с briefs;
- любой важный элемент ведёт в graph/evidence/artifact;
- mobile-версия не является урезанной копией desktop, а имеет собственную композицию.

## Что именно делает карточку сильной

Карточка обязана отвечать не только на “что случилось”, но и на:
- что изменилось относительно baseline;
- это локальная история или системная;
- распространяется ли это дальше по графу;
- кто downstream-получатель последствий;
- насколько вывод основан на сильных evidence;
- есть ли materialized path, который объясняет связь.

Без этих слоёв `Horizon Watch` быстро превратится в красивый, но поверхностный media panel.

## Логика отбора глобальных hot slices

Глобальный экран по умолчанию должен строиться не по простому объёму новостей, а по composite-ranking риска, последствий и graph relevance.

### Компоненты hotness

`hotness` v1 складывается из:
- силы первичного сигнала;
- новизны;
- количества независимых источников;
- качества источников;
- confidence нормализации;
- графовой центральности затронутых фрагментов;
- глубины/масштаба propagation;
- количества materialized/exposure paths, затронутых изменением;
- соответствия приоритетным регионам/рынкам;
- аномальности относительно baseline.

### Anti-noise правила

Нужно специально давить:
- однотипный дубликатный news spam;
- громкие, но не влияющие события;
- пропагандистские всплески без подтверждения;
- локальные факты без meaningful impact paths;
- старые истории, которые просто продолжают переписываться в медиа.

### Curated default slices

Даже при полном авторанжировании должен существовать curated bootstrap-набор.

Примеры default buckets:
- глобальные chokepoints;
- стратегические цепочки поставок;
- крупнейшие державы;
- зоны военного и санкционного давления;
- энергорынки;
- полупроводники;
- морская логистика;
- крупные производственные узлы.

## Источники данных и входные сигналы

`Horizon Watch` потребляет не сырые новости напрямую, а нормализованные horizon-артефакты и события.

Новости, RSS и прочие открытые источники здесь вторичны.
Они являются входным сырьём для сигнального и интерпретационного слоя, но не основным UX-объектом страницы.

Основные входы:
- RSS / web / Telegram / внешние фиды;
- `OntologyFact(kind=event|signal|relation|claim)` и соседние нормализованные факты;
- `SignalDescriptor` и производные risk/opportunity/trend сигналы;
- `Watchdog`-срабатывания;
- `Propagation` outcomes;
- `Lens`-пересчёты;
- `MaterializedArtifact` updates;
- `ExposurePath` updates;
- вручную добавленные операторские/аналитические markers.

Ключевое правило:

`Horizon Watch` не создаёт свою отдельную “правду”, а использует уже нормализованный слой Horizon как source of truth.

Ключевой UX-объект страницы:
- не статья и не новость;
- а brief об изменившемся рисковом состоянии конкретного slice.

## Связь с графом

`Horizon Watch` должен быть глубоко графоцентричным.

Каждый `Watch Slice` обязан ссылаться как минимум на одно из:
- `canonical paths`;
- `fragment ids`;
- `lens ids`;
- `materialized artifact ids`;
- `exposure path keys`;
- `risk job / propagation job refs`.

Из каждой карточки пользователь должен уметь:
- открыть соответствующий subgraph;
- перейти в fragment profile;
- открыть materialized path;
- увидеть upstream/downstream affected nodes;
- посмотреть underlying evidence.

## Связь с materialized artifacts

Materialized artifacts являются не дополнительным украшением, а одной из опорных объяснительных структур `Horizon Watch`.

Их роль:
- показывать цепочки поставок и иные overlays не как абстрактный текст, а как вычисленную структуру;
- объяснять “как именно событие добирается от A до B”;
- давать пользователю ощущение причинно-следственной геометрии.

V1 должен уметь показывать как минимум:
- `SUPPLY_CHAIN_PATH`
- `EXPOSURE_PATH` или совместимый path-like артефакт

## Связь с Lens

`Horizon Watch` не заменяет Lens, а работает в двух направлениях:

1. `Lens -> Watch`
- пользователь может добавить Lens в watch;
- watch brief может быть построен на lens scope;
- hot changes внутри активных lenses могут попадать в personal watch.

2. `Watch -> Lens`
- из watch card можно собрать/открыть lens;
- если slice достаточно широк, система может предложить `Create Lens from Watch`.

## Interpretation Layer и междисциплинарное мышление

`Horizon Watch` не должен ограничиваться:
- сигналами;
- сводками;
- path overlays;
- risk scores.

Для сильного стратегического продукта нужен отдельный интерпретационный слой, который строит не только ответ на вопрос `что изменилось`, но и ответ на вопрос:

`как именно это изменение переходит между слоями реальности и кого оно в итоге перестраивает`

Именно этот слой превращает систему из сильного мониторинга в систему стратегического чтения мира.

### Главный принцип

Система не должна писать публицистическое эссе “из воздуха”.

Она должна производить интерпретации как отдельные, проверяемые артефакты, опирающиеся на:
- факты;
- сигналы;
- граф зависимостей;
- baseline состояния;
- capability constraints;
- понятные causal bridges.

Интерпретация в системе должна быть:
- структурированной;
- споримой;
- проверяемой;
- привязанной к объектам, путям и evidence;
- готовой к уточнению и критике.

### Почему этого нельзя добиться только summaries

Обычная summary-логика хорошо отвечает на:
- что произошло;
- где произошло;
- какие источники об этом говорят.

Но она плохо отвечает на:
- как событие считывается разными аудиториями;
- как perception превращается в изменение доверия;
- как trust shift превращается в procurement shift;
- как political ambiguity превращается в industrial substitution;
- кто выигрывает не прямо, а через вторичный или третичный эффект.

Для этого нужен не summary layer, а `Interpretation Engine`.

## Interpretation Engine

`Interpretation Engine` — это слой Horizon, который собирает объяснимые стратегические гипотезы о перераспределении риска, доверия, зависимости, спроса, возможностей и силы.

Его задача:
- не пересказывать сигнал;
- не просто ранжировать hotness;
- а строить machine-readable интерпретации того, как событие или серия сигналов меняют поведение системных акторов.

### Что именно он должен уметь

1. Раскладывать событие на несколько дисциплинарных слоёв.
2. Видеть переход эффекта между слоями.
3. Строить actor-specific hypotheses.
4. Оценивать beneficiaries, losers и hedge behaviors.
5. Выделять не только прямые, но и second-order effects.
6. Формулировать disconfirming conditions.
7. Давать readable brief без потери структурной строгости.

## Дисциплины мышления, которые система должна совмещать

Интерпретационный слой должен мыслить не одной дисциплиной, а композиционно.

Минимальный набор дисциплин:
- военная и силовая динамика;
- политическая и дипломатическая динамика;
- медийное и narrative-восприятие;
- социум и элитное считывание;
- политическая экономия;
- финансовая и инвестиционная логика;
- промышленная и технологическая логика;
- supply chain и инфраструктурная логика;
- география, маршрутность, chokepoints;
- регуляторная и санкционная логика;
- институциональная устойчивость;
- временные горизонты `immediate / medium / long`.

Ключевая идея:

система должна уметь переводить вывод из одной дисциплины в последствия для другой.

Пример желаемого перехода:

`military event -> media reading -> elite trust delta -> procurement hedging -> supplier substitution -> industrial beneficiary`

## Типы интерпретационных артефактов

Ниже перечислены основные артефакты, которые должен уметь производить `Interpretation Engine`.

### 1. Interpretation Thesis

Главный тезис о происходящем.

Это не пересказ событий, а синтетическое утверждение уровня:
- `Gulf actors are likely to increase strategic hedging against Western suppliers.`
- `Maritime disruption is shifting from local logistics issue to downstream manufacturing pressure.`
- `Political ambiguity is weakening long-horizon trust more than immediate military balance.`

Свойства:
- один тезис = одна основная мысль;
- должен иметь scope, horizon и confidence;
- должен иметь список supporting mechanisms;
- должен иметь список disconfirming conditions.

### 2. Mechanism Chain

Формализованная цепочка трансляции эффекта.

Пример:

`policy ambiguity -> trust erosion -> hedge behavior -> supplier diversification -> local champion growth`

Свойства:
- ordered;
- каждый шаг имеет evidence and confidence;
- каждый мост должен быть объяснён;
- chain может ссылаться на graph/artifacts/metrics.

### 3. Perception Delta

Артефакт о том, как разные аудитории считывают одно и то же событие.

Аудитории могут быть:
- элиты региона;
- государственные закупщики;
- рынки капитала;
- оборонные planners;
- регуляторы;
- социальные группы;
- союзники;
- противники;
- глобальные поставщики.

Это особенно важно, потому что один и тот же факт может иметь разные operational consequences в зависимости от того, кем и как он считывается.

### 4. Trust Shift

Артефакт, описывающий изменение доверия между акторами по конкретной оси.

Оси:
- security guarantee credibility;
- policy predictability;
- supplier reliability;
- technology sovereignty risk;
- sanctions exposure risk;
- diplomatic alignment stability.

### 5. Strategic Shift Hypothesis

Гипотеза о смене поведения:
- закупки;
- локализация;
- диверсификация;
- перенос партнёрств;
- создание национального чемпиона;
- замещение технологий;
- изменение логистического маршрута;
- изменение инвестиционного приоритета.

### 6. Beneficiary / Loser Map

Артефакт, который показывает:
- кто выигрывает;
- кто проигрывает;
- по какой причине;
- на каком горизонте;
- через какой путь.

Это не должен быть примитивный список “победителей и проигравших”.
Он должен быть path-grounded.

### 7. Disconfirming Signal Set

Набор фактов и сигналов, которые могут ослабить или опровергнуть интерпретацию.

Это обязательно.

Без этого система будет красиво писать, но не будет мыслить строго.

### 8. Interpretation Delta

Артефакт о том, что именно изменилось в старой картине мира.

Не просто “появился новый сигнал”, а:
- какая гипотеза усилилась;
- какая ослабла;
- какая audience-reading изменилась;
- какой beneficiary moved up or down.

## Внутренние режимы мышления системы

Чтобы создавать сильные интерпретации, системе нужны не просто prompt templates, а несколько стабильных reasoning modes.

### 1. Cross-layer translation

Система должна уметь переводить событие между слоями:
- факт;
- сигнал;
- восприятие;
- доверие;
- поведение;
- контрактный выбор;
- цепочка поставки;
- геополитическая позиция;
- индустриальный результат.

### 2. Actor-centered reasoning

Система не должна мыслить “в среднем по миру”.

Она должна уметь спрашивать:
- что это означает для Саудовской Аравии;
- что это означает для госзакупщика в ОАЭ;
- что это означает для китайского поставщика;
- что это означает для европейского defense vendor;
- что это означает для местного industrial champion.

### 3. Audience-differentiated reading

Одно и то же событие может:
- усиливать страх в одной аудитории;
- усиливать оппортунизм в другой;
- усиливать осторожность в третьей;
- давать маркетинговый шанс в четвёртой.

Система должна различать это явно.

### 4. Baseline-vs-delta reasoning

Интерпретация должна строиться не в вакууме, а относительно baseline:
- какой был уровень доверия до события;
- какая была степень зависимости;
- был ли уже курс на локализацию;
- существовали ли альтернативные поставщики;
- была ли уже политическая усталость от партнёра.

### 5. Capability-constrained reasoning

Нельзя делать вывод “X выиграет”, если у X нет:
- мощностей;
- политического окна;
- цепочки поставок;
- компетенций;
- экспортного доступа;
- финансовой возможности масштабироваться.

То есть гипотеза должна проходить через слой `can they actually capture this upside`.

### 6. Counterfactual / disconfirmation reasoning

Для каждой сильной интерпретации система должна уметь сказать:
- что должно случиться, чтобы она ослабла;
- какие сигналы будут означать, что мир пошёл по другой ветке;
- где интерпретация уязвима.

### 7. Time-layered reasoning

Один и тот же эффект может быть:
- негативным в ближайшие 48 часов;
- нейтральным в среднесроке;
- выгодным для другого набора игроков в долгосроке.

Система должна уметь разносить эффекты по горизонтам, а не смешивать их.

## Как система должна строить интерпретацию

Ниже минимальный pipeline построения интерпретации.

### Stage 1. Event and signal assembly

Система собирает:
- события;
- сигналы;
- связанные facts;
- graph neighbors;
- materialized artifacts;
- relevant lenses;
- baseline state.

### Stage 2. Mechanism candidate generation

Система генерирует не narrative, а несколько candidate mechanisms:
- erosion of trust;
- supplier substitution;
- localization acceleration;
- corridor stress;
- alliance ambiguity;
- reputational weakening;
- demand reallocation.

### Stage 3. Gating by constraints

Каждый механизм проверяется на:
- graph support;
- evidence support;
- baseline compatibility;
- capability feasibility;
- temporal plausibility.

### Stage 4. Actor impact projection

Система проецирует механизм на:
- страны;
- компании;
- национальных чемпионов;
- sectors / markets;
- logistics corridors;
- watched targets.

### Stage 5. Critic and disconfirmation pass

Отдельный critic обязан искать:
- чрезмерно смелые причинные мосты;
- отсутствующие промежуточные шаги;
- смешение short-term и long-term эффектов;
- claims without capacity proof;
- narrative inflation.

### Stage 6. Interpretation artifact publication

Система публикует:
- thesis;
- mechanisms;
- affected / beneficiary map;
- disconfirmers;
- human-readable brief.

## Какими неявными мостами система должна уметь оперировать

Многие сильные инсайты рождаются не из фактов как таковых, а из связующих мостов между ними.

Примеры типовых мостов:
- `narrative incoherence -> elite distrust`
- `elite distrust -> shorter partnership horizon`
- `shorter partnership horizon -> diversification of vendors`
- `vendor diversification -> local champion opening`
- `sanctions or instability -> route rerouting`
- `combat visibility -> defense tech marketing uplift`
- `alliance ambiguity -> sovereign capability buildout`
- `industrial policy + threat perception -> accelerated localization`

Эти мосты должны быть представлены в системе не как свободный текст, а как управляемые mechanism templates.

## Что система не должна делать

Система не должна:
- генерировать “умное эссе” только по ленте новостей;
- перепрыгивать через причинные шаги;
- путать media visibility с реальным strategic effect;
- объявлять beneficiary без проверки capacity;
- смешивать normative judgement и operational interpretation;
- скрывать uncertainty.

## Как это проявляется в Horizon Watch

Пользователь в `Horizon Watch` должен видеть не только risk brief, но и при необходимости раскрывать его интерпретационный каркас.

Это может проявляться как:
- `Main Thesis`
- `Why the system thinks this`
- `Mechanism chain`
- `Who is affected`
- `Who benefits`
- `What would disprove this`
- `What changed since last update`

То есть brief должен быть narrative-friendly, но under the hood опираться на отдельные machine-readable interpretation artifacts.

## Предлагаемые системные контракты

Ниже не финальная schema, а направление модели.

### InterpretationThesis

```ts
type InterpretationThesis = {
  id: string;
  scopeRef: string;
  title: string;
  statement: string;
  horizon: 'immediate' | 'near' | 'medium' | 'long';
  polarity: 'risk' | 'opportunity' | 'mixed';
  confidence: number;
  mechanismChainIds: string[];
  affectedRefs: string[];
  beneficiaryRefs: string[];
  loserRefs: string[];
  disconfirmerIds: string[];
  evidenceFactIds: string[];
  updatedAt: string;
};
```

### MechanismChain

```ts
type MechanismChain = {
  id: string;
  scopeRef: string;
  label: string;
  steps: Array<{
    kind: string;
    statement: string;
    confidence: number;
    evidenceFactIds?: string[];
    supportingRefs?: string[];
  }>;
  confidence: number;
};
```

### TrustShift

```ts
type TrustShift = {
  id: string;
  sourceActorRef: string;
  targetActorRef: string;
  dimension:
    | 'security_credibility'
    | 'policy_predictability'
    | 'supplier_reliability'
    | 'technology_sovereignty'
    | 'sanctions_exposure'
    | 'alignment_stability';
  direction: 'up' | 'down' | 'mixed';
  magnitude: number;
  confidence: number;
  evidenceFactIds: string[];
};
```

### StrategicShiftHypothesis

```ts
type StrategicShiftHypothesis = {
  id: string;
  actorRef: string;
  shiftType:
    | 'procurement_hedging'
    | 'supplier_diversification'
    | 'localization_push'
    | 'alliance_rebalancing'
    | 'industrial_expansion'
    | 'route_rerouting'
    | 'investment_reweighting';
  direction: 'increasing' | 'decreasing' | 'mixed';
  horizon: 'near' | 'medium' | 'long';
  confidence: number;
  requiredConditions: string[];
  disconfirmers: string[];
  supportingRefs: string[];
};
```

## Связь интерпретационного слоя с прочими артефактами Horizon

Интерпретация должна опираться на существующие артефакты, а не жить отдельно.

Связи:
- `OntologyFact` дают фактическую почву;
- `SignalDescriptor` даёт normalized event pressure;
- `Propagation` даёт graph-spread consequences;
- `MaterializedArtifact` даёт path geometry;
- `ExposurePath` даёт объяснение уязвимости;
- `Lens` даёт scope и пользовательский контекст;
- `WatchBrief` даёт UX-поверхность для подачи интерпретации.

### Иерархия слоёв

Рекомендуемый порядок:

`facts -> signals -> graph effects -> interpretation artifacts -> watch brief`

Это важно.

Если система будет прыгать сразу из `facts` в `brief`, она начнёт терять строгость.

## Acceptance criteria для Interpretation Layer

Слой можно считать зрелым, если:
- система умеет строить тезис, отличный от summary;
- у тезиса есть явные mechanism chains;
- у каждого сильного вывода есть хотя бы минимальный disconfirmation layer;
- beneficiaries и losers выводятся через path/mechanism, а не интуитивным списком;
- разные аудитории могут иметь разные readings одного события;
- short / medium / long эффекты не смешиваются в один комок;
- пользователь может раскрыть reasoning без чтения “романа”.

## Персонализация и сохранение в профиле

Выбор пользователя должен сохраняться в профиле, а не только в local state.

### Новая секция профиля

Предлагаемая структура:

```json
{
  "preferences": {
    "horizonWatch": {
      "targets": [
        {
          "id": "watch_target_de_semiconductors",
          "type": "country",
          "label": "Germany",
          "ref": "/geo/country/de",
          "createdAt": "2026-03-08T10:00:00Z",
          "pinned": true
        },
        {
          "id": "watch_target_semis_market",
          "type": "market",
          "label": "Semiconductors",
          "ref": "/market/semiconductors",
          "createdAt": "2026-03-08T10:02:00Z"
        }
      ],
      "defaultMode": "combined",
      "filters": {
        "timeHorizon": "7d",
        "minConfidence": 0.55,
        "onlyTrustedEvidence": false
      },
      "layout": {
        "density": "comfortable",
        "showLiveStream": true,
        "showImpactRail": true
      }
    }
  }
}
```

### Правила сохранения

- сохранение должно быть серверным;
- локальный кеш допустим только как ускоритель UI;
- порядок pinned-элементов тоже сохраняется;
- удаление из watch должно быть идемпотентным;
- одинаковые targets не должны дублироваться.

## Read-model и серверный контракт

Для страницы нужен отдельный read-model, оптимизированный под UX.

Не нужно собирать экран на клиенте из десятков сырых коллекций.

### Основные read-model сущности

#### WatchTarget

```ts
type WatchTarget = {
  id: string;
  type:
    | 'country'
    | 'macro_region'
    | 'market'
    | 'sector'
    | 'corridor'
    | 'fragment'
    | 'lens'
    | 'query_address'
    | 'materialized_artifact'
    | 'exposure_path';
  label: string;
  ref: string;
  canonicalPaths?: string[];
  fragmentIds?: string[];
  lensId?: string;
  artifactId?: string;
  exposurePathKey?: string;
  metadata?: Record<string, unknown>;
};
```

#### WatchBrief

```ts
type WatchBrief = {
  id: string;
  target: WatchTarget;
  mode: 'global' | 'personal';
  headline: string;
  summary: string;
  hotness: number;
  confidence: number;
  polarity: 'risk' | 'opportunity' | 'mixed';
  timeHorizon: '24h' | '7d' | '30d';
  dominantThemes: string[];
  affectedRefs: string[];
  topSignalIds: string[];
  evidenceFactIds: string[];
  materializedArtifactIds: string[];
  exposurePathKeys: string[];
  lensIds: string[];
  updatedAt: string;
};
```

#### WatchFeedEntry

```ts
type WatchFeedEntry = {
  id: string;
  watchBriefId: string;
  kind:
    | 'signal_created'
    | 'signal_escalated'
    | 'propagation_completed'
    | 'artifact_updated'
    | 'brief_reframed';
  title: string;
  summary: string;
  importance: number;
  confidence: number;
  relatedRefs: string[];
  createdAt: string;
};
```

## Предлагаемые API-контуры

### 1. Получить главный экран Watch

`GET /api/horizon/watch`

Возвращает:
- глобальные hot slices;
- персональные slices;
- live feed;
- saved filters;
- meta counters.

### 2. Поиск target для добавления в watch

`POST /api/horizon/watch/search`

Ищет по:
- странам;
- рынкам;
- фрагментам;
- коридорам;
- lenses;
- artifacts;
- exposure paths.

### 3. Добавить target в профиль пользователя

`POST /api/horizon/watch/targets`

### 4. Удалить target из watch

`DELETE /api/horizon/watch/targets/{targetId}`

### 5. Обновить настройки watch

`PUT /api/horizon/watch/preferences`

### 6. Получить детали конкретного brief

`GET /api/horizon/watch/briefs/{briefId}`

### 7. Получить path/evidence/source drill-down

Возможные endpoint-адаптеры:
- `GET /api/horizon/watch/briefs/{briefId}/evidence`
- `GET /api/horizon/watch/briefs/{briefId}/paths`
- `GET /api/horizon/watch/briefs/{briefId}/sources`

## Realtime-модель

Страница должна получать realtime-обновления, но без разрушения читабельности.

Нужны события вида:
- `horizon.watch.brief.upserted`
- `horizon.watch.feed.entry.created`
- `horizon.watch.target.updated`
- `horizon.watch.global.hotness.changed`

Realtime должен:
- мягко обновлять карточки;
- подсвечивать, что summary изменился;
- не перестраивать весь экран хаотично;
- позволять пользователю “заморозить” поток на время чтения.

## Визуальный характер страницы

Страница должна ощущаться как:
- sophisticated;
- panoramic;
- editorial;
- operational;
- slightly cinematic.

Нельзя делать её как обычный admin dashboard.

Нужны:
- крупные сильные заголовки;
- ощущение глобального обзора;
- сочетание карты, карточек, коротких risk briefs, dashboards и impact-структур;
- аккуратная цветовая драматургия hotness/confidence;
- ощущение “живой разведывательной комнаты”, а не CRM.

## Критерии качества UI

Пользователь без обучения должен понять:
- что глобально самое важное;
- что важно именно для него;
- почему карточка поднялась вверх;
- как добраться до доказательств и графа.

Если карточка не ведёт в explanation layer, она не готова.

## MVP v1

### Цель MVP

Запустить первую версию страницы, которая уже даёт сильную ценность без полной реализации всех будущих типов watch target.

### В MVP обязательно

- маршрут `/horizon/watch`;
- глобальные hot slices;
- персональный watch по странам, рынкам и fragment targets;
- сохранение watch targets в профиле;
- server-side read-model для карточек;
- brief-first presentation, а не feed-first presentation;
- dashboard-блоки как supporting layer;
- простые filters;
- drill-down в sources/evidence/graph;
- интеграция с signals + propagation summaries;
- базовая realtime-подсветка.

### Что можно отложить

- сложные curator tools;
- полноценный corridor graph composer;
- auto-generated “watch packs”;
- cross-user collaborative watchlists;
- advanced ranking explainability UI;
- alert policy editor внутри этой страницы.

## Phase 2

- поддержка `materialized_artifact` и `exposure_path` как first-class watch targets;
- watch packs: `Semiconductors`, `Energy chokepoints`, `China pressure`, `EU industrial stress`;
- richer map integration;
- historical playback;
- watch-specific alert thresholds;
- comparative mode по регионам/рынкам.

## Phase 3

- fully explainable ranking;
- operator curation workflows;
- “what changed since your last visit” mode;
- auto-suggested watch targets based on behavior;
- generated morning/evening brief;
- shared team watchboards.

## Метрики успеха

Продуктовые метрики:
- доля пользователей, открывающих `Horizon Watch` как стартовый экран;
- число сохранённых watch targets на пользователя;
- доля переходов из Watch в graph / evidence / profiles;
- частота возвращения на страницу;
- время до первого meaningful click;
- доля карточек, по которым был открыт explanation layer.

Качественные признаки:
- пользователь чувствует, что видит не новости, а структурированную картину давления;
- пользователь понимает, почему событие важно именно через affected paths;
- page worth revisiting even without manual query.

## Риски

### 1. Превращение в news feed

Если упор сместится на поток новостей или “dashboard ради dashboard”, продукт потеряет уникальность.

Контрмера:
- приоритет interpretations, impacted targets, propagation и overlays над raw news volume.

### 2. Слишком много шума

Если hotness и ranking будут сырыми, страница станет утомительной.

Контрмера:
- агрессивная дедупликация;
- confidence-aware ranking;
- suppression слабых сигналов.

### 3. Отрыв от core-модели Horizon

Если Watch начнёт жить отдельной жизнью, появится вторая truth system.

Контрмера:
- read-model only;
- ссылки на canonical refs;
- reuse существующих signal / graph / artifact contracts.

### 4. Красивый UI без достаточного explanation layer

Эффектный экран без доказательной глубины быстро разочарует.

Контрмера:
- каждая карточка обязана иметь evidence и путь drill-down.

## Решения, которые уже можно считать зафиксированными

- Название страницы: `Horizon Watch`
- Это отдельная страница внутри Horizon.
- По умолчанию показывается глобальный набор самых горячих срезов.
- Пользователь может добавлять интересующие его страны, рынки и другие targets в personal watch.
- Выбор пользователя сохраняется в профиле.
- Страница жёстко связана с графом, источниками, сигналами, materialized paths и связанными артефактами.
- Основной объект интерфейса: risk brief, а не источник и не новость.
- Dashboard-слой допустим и желателен, но только как supporting navigation layer.
- `Horizon Watch` реализуется как read-model / presentation layer над существующей horizon-архитектурой.

## Следующий уровень детализации

После утверждения этого документа следующими отдельными документами стоит зафиксировать:
- UX wireframe / layout spec страницы;
- backend read-model technical design;
- ranking/hotness methodology;
- profile schema changes;
- rollout plan по этапам frontend/backend.

## Связанные документы

- [docs/horizon/README.md](../README.md)
- [docs/horizon/ux/user-journey.md](./user-journey.md)
- [docs/horizon/architecture/materialized-artifacts.md](../architecture/materialized-artifacts.md)
- [docs/horizon/architecture/query-address.md](../architecture/query-address.md)
- [docs/horizon/implementation/RISK_SIGNAL_PIPELINE_TECHNICAL_DESIGN.md](../implementation/RISK_SIGNAL_PIPELINE_TECHNICAL_DESIGN.md)
- [docs/horizon/implementation/WORLDMONITOR_TRANSFORMATION_DRAFT.md](../implementation/WORLDMONITOR_TRANSFORMATION_DRAFT.md)
