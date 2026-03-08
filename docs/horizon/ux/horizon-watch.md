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
