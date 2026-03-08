# RSS Signal Calibration Rules

Статус: active working policy

Файл описывает свод правил для heuristic path:

1. `rss/source mechanics`
2. `event recognition`
3. `signal extraction`
4. `seed resolution`
5. `graph propagation`
6. `endpoint interpretation`

Цель: максимизировать business-meaningful propagation без массового использования LLM.

## Контекст

По live-audit на `2026-03-07` для реальных RSS потоков и реального Vertex datastore `etigram-fragments-store` были подтверждены три класса проблем:

1. Generic news RSS без явной risk semantics слишком легко деградировал в `risk:temporal_anomaly`.
2. Semantic seeding без axis-aware фильтрации выбирал текстово близкие, но business-слабо полезные узлы:
   - `PERSON`
   - `EVENT`
   - `SUPPLY_LINK`
   - media-ish / headline-noise entities
3. Country fallback без ранжирования размывал propagation по слишком широкому набору узлов.
4. Военные headlines с названиями платформ (`Typhoons`) могут ошибочно выглядеть как climate/disaster lexeme.
5. Даже после успешного propagation часть сигналов не приносит ни одного business endpoint; это надо считать как отдельный failure mode, а не как success.
6. Live defense RSS продолжает порождать headline-shape noise:
   - roundup/commentary items
   - generic service branches (`Air Force`, `Navy`)
   - country-government placeholders (`Iran government`)
   - opaque acronyms (`The OSMP`)
   - vague conjunctions (`Patriot and THAAD`, `American and Israeli`)
7. Для propagation одного `FragmentType` недостаточно:
   - `FACILITY` может быть как chokepoint/base/port, так и weak distributor node
   - `ORGANIZATION` может быть operational actor или abstract regulator
8. Multi-country headlines требуют multi-anchor seeding:
   - один `LocationPath` не покрывает все театры события
   - без этого graph либо слишком локален, либо уходит в ложный country fan-out

## Правила распознавания

### Rule R1: Generic news не должен автоматически становиться `temporal_anomaly`

Принцип:
- generic RSS article без конкретного disruptive mechanism не должен идти в graph propagation только потому, что его не удалось хорошо классифицировать.

Реализация:
- `inferHorizonMonitorSignalType(...)` использует:
  - source category
  - headline/body keywords
  - risk-specific keyword groups
- `risk:temporal_anomaly` остается только последним fallback.

### Rule R2: `temporal_anomaly` для generic analysis suppressится

Принцип:
- если статья не дает предметного risk signal, она может остаться документом/классификацией, но не должна загрязнять propagation.

Реализация:
- `shouldSuppressHeuristicSignalPropagation(...)`
- suppress применяется только для generic RSS path
- suppress не применяется к machine feeds

Что suppressится:
- strategy/industry/analysis materials без явного disruptive trigger
- roundup/commentary/podcast materials без явного disruptive trigger

Что не suppressится:
- event-like anomaly headlines с явной operational semantics:
  - `attack`
  - `missile`
  - `drone`
  - `outage`
  - `explosion`
  - `earthquake`
  - `flood`
  - `wildfire`

### Rule R3: Military `strikes` != protest

Принцип:
- `strike` в defense headline не должен автоматически классифицироваться как `protest`.

Реализация:
- protest keyword group больше не матчится на голое `strike`
- protest требует более узких форм:
  - `labor strike`
  - `workers strike`
  - `dock strike`
  - `general strike`

### Rule R4: Military platform names не должны уводить в disaster axis

Принцип:
- названия вооружений и платформ могут пересекаться со словами природных явлений, но в defense-контексте это не weather event.

Реализация:
- `inferHorizonMonitorSignalType(...)` проверяет military context раньше climate fallback.

Примеры:
- `UK sends additional Typhoons to bolster Qatar's air defence` -> `risk:conflict_event`
- а не `risk:climate_anomaly`

### Rule R5: Defense-industrial и military hardware headlines не должны деградировать в `protest`

Принцип:
- статьи про платформы, боеприпасы, anti-drone systems, air bases и defense testing не являются civil unrest.

Реализация:
- `inferHorizonMonitorSignalType(...)` поднимает `conflict_event`, если видит:
  - `F-16`
  - `JDAM`
  - `precision bomb`
  - `anti-drone system`
  - `air base`
  - `laser test`
  - `task force`
  - `fighter`

Даже если headline короткий и feed/category hint слабый, military hardware context должен перевешивать generic fallback.

## Правила seed resolution

### Rule S1: SeedNames полезны только если они business-shaped

Принцип:
- title-case phrase extraction без фильтра рождает мусор:
  - `Related Posts`
  - `Backstage Partner`
  - `Exquisite Class`
  - `In Iran`

Реализация:
- `heuristicSignalSeedNames(...)`
- `extractHeuristicSeedPhrases(...)`
- `isWeakSeedPhrase(...)`

Seed phrase принимается только если:
- есть сильный entity-pattern
- или есть domain indicator:
  - `government`
  - `department`
  - `ministry`
  - `forces`
  - `committee`
  - `port`
  - `terminal`
  - `pipeline`
  - `market`
  - `strait`
  - `gulf`
  - `sea`
  - `energy`

### Rule S2: Semantic seeding должен быть axis-aware

Принцип:
- Vertex Search дает хорошие текстовые совпадения, но не знает сам по себе, какой fragment type бизнес-осмыслен для данного сигнала.

Реализация:
- `resolveSeedName(...)`
- `seedFragmentTypeAllowed(...)`
- `semanticSeedLocationBonus(...)`

Допустимые типы по axes:

`geo/conflict`
- `GEO_REGION`
- `ORGANIZATION`
- `FACILITY`
- `COMMUNITY`
- `MARKET`

`logistics/*`, `physical_flow/*`
- `FACILITY`
- `MARKET`
- `ORGANIZATION`
- `GEO_REGION`
- `DEPOSIT`
- `PROGRAM`

По умолчанию запрещаются как прямые semantic seeds:
- `PERSON`
- `EVENT`
- `MEDIA_CHANNEL`
- `SUPPLY_LINK`

Причина:
- они слишком часто текстово близки headline, но редко являются лучшей стартовой точкой для business propagation.

### Rule S2.1: Semantic seed query должен проходить shape-filter до Vertex Search

Принцип:
- плохой query shape почти гарантирует плохой seed, даже если Vertex находит “что-то похожее”.

Сейчас suppressятся:
- media phrases:
  - `The Associated Press`
- generic service branches:
  - `Air Force`
  - `Navy`
  - `Army`
- country-government placeholders:
  - `Iran government`
  - `Mexico government`
  - `government of X`
- opaque acronyms:
  - `The OSMP`
- vague conjunctions / nationality pairs:
  - `Patriot and THAAD`
  - `American and Israeli`

Причина:
- такие queries почти всегда тянут либо политический шум,
- либо generic govt/location node,
- либо вообще не дают полезного business seed.

### Rule S2.2: Document-markup/OCR noise не должен попадать в `SeedNames`

Принцип:
- служебные заголовки документов, OCR-мусор и classification markings не являются entities и не должны доходить до Vertex Search.

Сейчас suppressятся:
- `TOP SECRET`
- `HAND DISTRIBUTION ONLY`
- `CODEX LEVEL II`
- `PRINTED BY`
- `CLASSIFIED BY`
- `DECLASSIFY ON`

Реализация:
- upstream:
  - `heuristicSignalSeedNames(...)`
  - `extractHeuristicSeedPhrases(...)`
  - `isDocumentMarkupSeedPhrase(...)`
- downstream safety-net:
  - `shouldSearchSemanticSeedQuery(...)`
  - `semanticSeedDocumentMarkupQuery(...)`

Причина:
- такой мусор не является propagation signal;
- он портит semantic seeding;
- он искажает calibration telemetry.

### Rule S2.3: Abstract hubs и artifact-like nodes не должны становиться semantic seeds

Принцип:
- даже хороший text match бесполезен, если target fragment structurally слишком абстрактен для seeding.

Сейчас запрещаются:
- `/supply-link/...`
- `SUPPLY_LINK`
- generic `/loc/{cc}/na`
- abstract `/sphere/...`
- abstract `/product/...`

Реализация:
- `semanticSeedFragmentAllowed(...)`

Причина:
- это либо structural artifacts,
- либо semantic buckets,
- либо container geo nodes без достаточной operational specificity.

### Rule S3: Location fallback должен быть ранжированным, а не слепым

Принцип:
- если semantic seeding не дал якоря, нельзя просто брать первые попавшиеся узлы по стране.

Реализация:
- `resolveLocationFallback(...)`
- `listCountryPrefixCandidates(...)`
- `rankLocationFallbackCandidates(...)`
- `seedPriorityForAxis(...)`

Fallback порядок:
- сначала edge-based candidates от `/loc/{cc}`
- если `/loc/{cc}` слабо связан или отсутствует:
  - `/gov/{cc}/`
  - `/org/{cc}/`
  - `/fac/{cc}/`
  - `/market/{cc}/`
- затем candidates ранжируются по:
  - axis fit
  - applicability score
  - location compatibility

### Rule S4: Location fallback должен быть axis-capped и business-biased

Принцип:
- location-only conflict signals нельзя запускать со слишком широкого country fan-out, иначе propagation выглядит “богато”, но бизнес-смысл теряется.

Реализация:
- `locationSeedCapForSignal(...)`
- `locationFallbackPriorityForAxis(...)`

Текущие caps:
- `geo/conflict` -> `18`
- `logistics/*` -> `16`
- `physical_flow/*`, `operational/continuity` -> `20`
- default -> `24`

Приоритеты для `geo/conflict` location fallback:
- `loc`
- `fac`
- `market`
- `org`
- `gov`

Смысл:
- semantic seeding по явным именам всё ещё может выбрать govt actor,
- но голый country fallback не должен начинаться с россыпи государственных сущностей.

### Rule S5: Seed paths и seed names нельзя слепо считать параллельными массивами

Принцип:
- `SeedPaths` и `SeedNames` могут появляться из разных heuristic layers.
- если считать их по индексу “параллельными”, location anchor начинает идти в Vertex с чужим query.

Реализация:
- `seedPathSearchName(...)`

Правило:
- pairing path/name допустим только когда массивы реально параллельны по длине и смыслу.

### Rule S6: Country-only `/loc/{cc}` должен резолвиться детерминированно

Принцип:
- bare country location anchor не должен идти в semantic search по query вроде `ir` или `qa`.

Реализация:
- `resolveCountryLocationPath(...)`

Правило:
- `/loc/{cc}` сначала резолвится в country geo fragment через prefix lookup,
- и только потом допускается более дорогой fallback.

### Rule S7: Semantic query с явной страной должен hard-filter mismatch countries

Принцип:
- `Qatar air defense` не должен резолвиться в египетский air-defense node только по текстовой близости.

Реализация:
- `semanticSeedCountryFromQuery(...)`
- `semanticSeedQueryGeoBonus(...)`
- `semanticSeedCountryMismatch(...)`

Правило:
- если query явно содержит страну, mismatch country fragment отбрасывается полностью.

### Rule S8: Multi-country headlines должны давать multi-anchor seeds

Принцип:
- headline вроде `UK sends Typhoons to bolster Qatar's air defence` описывает не одну географию.

Реализация:
- `heuristicSignalSeedPaths(...)`
- `extractCountryCodesFromText(...)`
- `extractGeoAnchorSeedNames(...)`

Следствие:
- сигнал может иметь:
  - primary `LocationPath`
  - дополнительные `SeedPaths`
  - geo-aware `SeedNames` для нескольких театров события

## Правила propagation

### Rule P1: Location-only signals допустимы, но должны считаться второсортными

Принцип:
- `location_only` signals нужны как fallback, но сами по себе они хуже actor/market/facility seeded signals.

Следствие:
- их нужно отдельно считать в telemetry
- их долю надо снижать по мере роста entity anchors

### Rule P2: `info/uncertainty` не должен доминировать

Принцип:
- `info/uncertainty` полезен как слабый слой uncertainty, но не как основной business trigger.

Следствие:
- нужно отдельно мониторить долю `signals_info_uncertainty_total`
- рост этой доли на live RSS означает ухудшение recognition quality

### Rule P3: Remote tails имеют смысл только при предметных seeds

Принцип:
- remote endpoint ценен, только если путь начался с business-valid seed.
- remote tail от случайного consumer-goods org в той же стране не является доказательством качества.

Следствие:
- remote tails нужно оценивать вместе с:
  - `seed_sample`
  - `signal axis`
  - `signal type`
  - `location_only`

### Rule P4: “Есть propagation” ещё не значит “есть смысл”

Принцип:
- если сигнал seeded и propagated, но не дал ни одного business endpoint, это failure mode graph/weights, а не success.

Реализация:
- считать `signals_zero_business_total`
- считать отдельно `signals_remote_endpoint_total`
- считать отдельно `signals_long_path_endpoint_total`

Это нужно, чтобы отличать:
- “ничего не дошло”
- “дошло, но только до шумовых/тривиальных хвостов”

### Rule P5: Propagation physics должна учитывать семьи ребер, а не только raw edge type

Принцип:
- одинаковый `DEPENDS_ON` семантически значит разное:
  - logistics chokepoint
  - facility dependency
  - energy flow
  - generic weak dependency
- без family-level weighting propagation получается слишком наивным.

Реализация:
- модуль: [edge_family_physics.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/edge_family_physics.go)
- профиль: [edge_family_physics_profile.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/edge_family_physics_profile.go)

Что учитывается:
- axis
- edge type
- dependency facet
- transmission qualifiers
- substitutability
- edge modality (`actual / projected / historical`)
- chokepoint transit

Зачем:
- `geo/conflict` должен легче входить в:
  - `chokepoint_logistics`
  - `energy_supply_chain`
  - `facility_dependency`
- и слабее расходиться по:
  - `regulatory_control`
  - generic organizational paths

### Rule P6: Поверх canonical graph нужен heuristic propagation overlay

Принцип:
- для propagation мало знать только `FACILITY / MARKET / ORG / LOC`.
- нужен дополнительный слой operational roles и exposure modes.

Реализация:
- модуль: [propagation_overlay.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/propagation_overlay.go)

Overlay выводит для fragment:
- `PrimaryRole`
- `BusinessRole`
- `Roles`
- `ExposureModes`
- `OperationalScore`
- `BridgeScore`
- `CriticalityScore`

Текущие primary roles:
- `chokepoint`
- `defense_operator`
- `energy_asset`
- `logistics_hub`
- `manufacturing_operator`
- `market_aggregator`
- `regulator`
- `geo_theater`
- `generic_node`

### Rule P7: Overlay должен участвовать в seed ranking и propagation physics

Принцип:
- `geo/conflict` должен предпочитать operationally relevant nodes,
- а generic distributors без релевантного flow context должны опускаться.

Реализация:
- `overlaySeedBonus(...)`
- `BuildPropagationNodeOverlay(...)`
- `BuildPropagationEdgeOverlay(...)`
- `ApplyEdgeFamilyPhysics(...)`

Что это даёт:
- bonus для bridges / chokepoints / defense operators
- penalty для generic distributors в weak conflict signals
- более честный `route_transit` / `energy_input` / `market_transmission` / `regulatory_gate`

### Rule P8: Если после overlay сигнал остаётся только в geo-operational tails, это лучше ложного business tail

Принцип:
- отсутствие business endpoint после ужесточения seeding и overlay может быть корректным результатом.
- это лучше, чем synthetic LNG/market tail без реальной path semantics.

Live observation:
- `UK sends additional Typhoons to bolster Qatar's air defence`
  после overlay и stricter seeding перестал рождать ложные UK energy tails
  и остался в `geo_operational_exposure`.

Смысл:
- дальше нужно улучшать graph topology / edge semantics,
- а не ослаблять фильтры обратно.

## Правила интерпретации

### Rule I1: Interpretation должна идти после propagation, а не вместо него

Принцип:
- downstream warning не должен быть hardcoded правилом вида `Iran -> LNG Europe`.
- warning должен рождаться из:
  - signal axis
  - seed set
  - graph path
  - endpoint type

### Rule I2: В interpretation нужно поднимать объяснение маршрута

Рекомендуемый формат:
- source event
- recognized signal
- chosen seeds
- propagation path family
- endpoint impact class

Пример:
- `military tension in Persian Gulf`
- `logistics/capacity + geo/conflict`
- seeds:
  - `Persian Gulf`
  - `Port and Maritime Services Market in Iran`
- paths:
  - maritime chokepoints
  - LNG / petrochem chain exposure
- endpoints:
  - European LNG consumers
  - regional port/terminal dependence

### Rule I3: Non-obvious business tails должны оцениваться отдельным scorer-модулем

Принцип:
- “интересность” хвоста нельзя смешивать с самим traversal.
- traversal отвечает за достижимость,
- scorer отвечает за бизнес-значимость и неожиданность endpoint-а.

Реализация:
- модуль: [endpoint_scoring.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/endpoint_scoring.go)
- профиль весов: [endpoint_scoring_profile.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/endpoint_scoring_profile.go)

Факторы scorer-а:
- тип endpoint-а (`facility`, `market`, `deposit`, `program`, `org`, chokepoint)
- criticality
- low replaceability
- energy/logistics sensitivity
- remote country
- multi-hop depth
- path diversity
- axis-specific bonus (`conflict -> business tail`, `flow -> business tail`)

Математика:
- weighted features
- sigmoid squash на финальном score
- MMR-style diversification при выборе top endpoints
- отдельные thresholds для:
  - `business endpoint`
- `non-obvious endpoint`

Зачем diversification:
- без него top endpoints легко забиваются почти одинаковыми хвостами
- из одной страны
- одного типа
- одной и той же ветки графа

MMR здесь полезен именно как deterministic post-ranking:
- traversal не трогаем
- scorer не меняем по сути
- но top tails становятся более обзорными и калибруемыми

### Rule I3.1: Endpoint scoring должен читать не только `PropagationPaths`, но и `Trace`

Принцип:
- terminal path tails в текущем engine слишком узки для оценки дальних бизнес-концов.
- `Trace` уже содержит более богатую физику:
  - `channel`
  - `scope`
  - `spillover`
  - `axisOut`
  - transmission factors (`throughput`, `capacity`, `reroute`, `waveguide`)

Реализация:
- `ScoreInterestingEndpoints(...)` теперь агрегирует:
  - обычные terminal tails из `PropagationPaths`
  - propagated endpoints из `SignalPropagationTraceEvent`

Дополнительные факторы из trace:
- cross-border step
- economic / operational channel
- flow-axis bridge
- transmission physics bonus

Смысл:
- business endpoint может быть виден в propagation trace, даже если terminal path snapshot сам по себе бедный.

### Rule I4: Business meaning маршрута должен оцениваться отдельным path-family scorer

Принцип:
- endpoint сам по себе ещё не объясняет, почему сигнал туда дошёл.
- нужен отдельный слой, который агрегирует propagation trace в семейства маршрутов.

Реализация:
- модуль: [path_family_scoring.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/path_family_scoring.go)
- профиль весов: [path_family_scoring_profile.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/path_family_scoring_profile.go)

Текущие семьи маршрутов:
- `energy_supply_chain`
- `chokepoint_logistics`
- `market_exposure`
- `facility_dependency`
- `regulatory_control`
- `organizational_control`
- `geo_operational_exposure`
- `direct_dependency`

Scorer смотрит на:
- `EdgeType`
- `Channel`
- `Scope`
- `Spillover`
- canonical paths по trace
- business/facility tails
- cross-border steps
- глубину маршрута

Смысл:
- теперь можно видеть не только `куда дошло`,
- но и `какой бизнес-маршрут реально сработал`.

### Rule I5: Нужен отдельный breakpoint audit, чтобы видеть где сигнал умирает

Принцип:
- если сигнал не дошёл до нетривиального конца, нужно знать:
  - на каком family он ломается;
  - по какой причине;
  - это threshold, transfer skip или hop limit.

Реализация:
- модуль: [breakpoint_audit.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/propagation/breakpoint_audit.go)

Что считается:
- `damped_below_threshold`
- `transfer_skip`
- `hop_limit`
- top breakpoint families
- business breakpoints в:
  - `chokepoint_logistics`
  - `energy_supply_chain`
  - `facility_dependency`
  - `market_exposure`

Смысл:
- это уже не “почему итог 0 вообще”,
- а “на каком физическом участке графа сигнал потерялся”.

## Telemetry для калибровки

Уже опущено в telemetry:

`wm_quality`
- `signal_suppressed_total`
- `non_event_filtered_total`
- `zones_map_ready_total`
- `zones_precise_geo_total`
- `zones_approx_geo_total`

`wm_signal_flow`
- `signals_location_only_total`
- `signals_semantic_named_total`
- `signals_info_uncertainty_total`
- `signals_zero_reach_total`
- `signals_zero_business_total`
- `signals_remote_endpoint_total`
- `signals_long_path_endpoint_total`
- `signals_path_family_total`
- `signals_business_path_family_total`
- `signals_cross_border_family_total`
- `signals_breakpoint_total`
- `breakpoint_threshold_total`
- `breakpoint_transfer_skip_total`
- `breakpoint_hop_limit_total`
- `breakpoint_business_total`
- `affected_fragments_total`
- `propagation_paths_total`

Дополнительно стоит вывести в admin UI рядом:
- `signals_business_endpoint_total`
- `signals_non_obvious_endpoint_total`
- `facility_endpoints_total`

## Как калибровать дальше

### 1. Recognition pass

Смотреть:
- `category_known`
- `high_or_critical`
- `signal_suppressed_total`
- долю `signals_info_uncertainty_total`

Если `signals_info_uncertainty_total` растет:
- расширять keyword groups
- добавлять source-category priors
- усиливать suppress для generic analysis

### 2. Seeding pass

Смотреть:
- `signals_location_only_total`
- seed samples из live audit
- долю semantic seeds, которые реально совпадают по стране и axis

Если semantic seeds шумные:
- расширять stoplist noisy headline phrases
- усиливать fragment type restrictions
- добавлять domain-specific anchors:
  - chokepoints
  - terminals
  - ports
  - energy markets
  - military/government actors

Если в live audit снова появляются query shapes вида:
- `The X`
- `X government`
- generic branch names
- commentary/roundup phrases

то надо сначала лечить query shape filter, а не пытаться “выжать” из Vertex лучший результат.

### 3. Propagation pass

Смотреть:
- `signals_zero_reach_total`
- `signals_zero_business_total`
- `signals_remote_endpoint_total`
- `signals_long_path_endpoint_total`
- `signals_business_path_family_total`
- `signals_cross_border_family_total`
- `remote_tail_signals`
- `long_path_signals`

Если reach есть, но business endpoints нет:
- лечить seed specificity
- ужимать location fallback
- лечить endpoint scoring thresholds

Если reach есть, но tails бессмысленные:
- лечить seed quality
- лечить graph edge qualifiers
- лечить applicability scoring

Если `Trace` богатый, а `business endpoints = 0`:
- снижать trust к terminal path snapshots
- усиливать trace-aware endpoint/path-family scoring
- отдельно смотреть, не упирается ли propagation в generic govt/loc nodes

Если `breakpoint_threshold_total` высокий:
- ослаблять damping не глобально,
- а точечно по edge family

Если `breakpoint_transfer_skip_total` высокий:
- смотреть adapter / applicability / transformation policies

Если `breakpoint_business_total` высокий:
- значит сигнал ломается уже на meaningful route,
- и это приоритетнее, чем просто добавлять новые seeds

### 4. Interpretation pass

Смотреть:
- совпадает ли endpoint с реальным business narrative
- не приходит ли хвост в политический шум вместо supply/logistics exposure

Если interpretation weak:
- добавлять endpoint-type priors
- усиливать path-family grouping
- объяснять impact через chain classes, а не через raw nodes

## Практическое правило

Для heuristic RSS path порядок важности такой:

1. suppress useless signals
2. classify concrete signals
3. choose business-valid seeds
4. propagate through graph
5. interpret only meaningful tails

Если шаг 3 плохой, шаги 4-5 почти всегда будут выглядеть случайными, даже при хорошем графе.

Если шаг 4 формально успешен, но `signals_zero_business_total` высокий, это означает:
- граф/развесовки ещё не умеют доводить сигнал до бизнес-концов,
- и именно это нужно лечить следующим слоем, а не расширять RSS бесконечно.

## Текущий приоритет дальнейшей калибровки

По состоянию на `2026-03-07` следующий приоритет такой:

1. Убирать live query noise до Vertex:
   - generic service branches
   - `country government`
   - opaque acronyms
   - roundup/commentary title phrases
2. Поднимать seed specificity для `geo/conflict`:
   - меньше generic govt nodes
   - больше chokepoints / facilities / markets / deposits
3. Лечить propagation weights по edge families:
   - чтобы сигнал доезжал не до очевидных соседей,
   - а до дальних business tails
4. Калибровать interpretation уже после этого,
   - через path families,
   - а не через hardcoded geopolitical shortcuts.

## Kernel layers

С `2026-03-07` propagation надо считать многослойным kernel, а не одной BFS-функцией.

Слои:

1. `overlay`
   - узлы и связи получают operational roles
   - файл: `backend/internal/horizon/propagation/propagation_overlay.go`
2. `edge-family physics`
   - калибрует передачу по семействам ребер и qualifiers
   - файл: `backend/internal/horizon/propagation/edge_family_physics.go`
3. `bounded traversal`
   - базовый deterministic path
   - файл: `backend/internal/horizon/propagation/signal_propagation.go`
4. `typed RWR`
   - long-tail discoverability для неочевидных downstream концов
   - файл: `backend/internal/horizon/propagation/typed_random_walk.go`
5. `endpoint/path-family scoring`
   - переводит reach в business-meaningful ranking
   - файлы:
     - `backend/internal/horizon/propagation/endpoint_scoring.go`
     - `backend/internal/horizon/propagation/path_family_scoring.go`

Конфигурация режима:
- `backend/internal/horizon/propagation/signal_kernel.go`

Режимы:
- `bounded`
- `typed_rwr`
- `hybrid`

Для heuristic RSS path default policy теперь:
- business axes -> `hybrid`
- weak informational axes -> `bounded`

Причина:
- `bounded` хорошо ловит прямую физику ближайших hops
- `typed_rwr` нужен для soft long-tail reach
- `hybrid` позволяет не ломать deterministic base, но искать более дальние business-кандидаты

## Rule P9: Typed RWR не должен подменять business semantics географическим шумом

Принцип:
- если `typed_rwr` начинает выбирать generic `/loc/*` tails, это не успех propagation.

Реализация:
- ranking состояния зависит не только от walk score, но и от overlay:
  - `energy_asset`
  - `logistics_hub`
  - `manufacturing_operator`
  - `chokepoint`
  - `market_aggregator`
- generic `geo_theater` и `generic_node` штрафуются
- plain `/loc/*` tails штрафуются, если это не chokepoint

Смысл:
- long-tail reach должен вытаскивать operators / facilities / markets / chokepoints,
- а не просто ещё один географический контейнер.

## Rule P10: Hybrid mode — это инструмент диагностики графа, а не автоматическая победа

Принцип:
- если после `hybrid` в live replay всё ещё нет `interesting endpoints`,
- это означает, что bottleneck уже сидит в graph topology / edge semantics / canonical richness.

Подтверждено на live replay `2026-03-07`:
- `Janes / Iran conflict 2026: UK sends additional Typhoons...`
  - `kernel=hybrid`
  - `tail_selected=13`
  - `interesting_endpoints=0`
  - `endpoint_sample` всё ещё уходит в `/loc/*`
- `Janes / The demise of the Iranian navies`
  - `kernel=hybrid`
  - `tail_selected=11`
  - `business_families=1`
  - но `interesting_endpoints=0`

Вывод:
- ядро propagation уже умеет добирать tails,
- но граф пока не даёт достаточно богатых business routes для company/facility discovery.

## Что калибровать дальше

Следующий приоритет после введения `hybrid + typed RWR`:

1. обогащать сам граф:
   - facility -> input dependency
   - operator -> facility
   - port/terminal -> route/chokepoint
   - energy input -> manufacturer / market / program
2. повышать каноническую детализацию fragments:
   - operational role
   - business role
   - exposure mode
   - substitutability / criticality
3. усиливать endpoint scorer:
   - penalize generic geo tails
   - boost concrete company/facility/market tails
4. смотреть не только `reach`, но и:
   - `tail_selected_total`
   - `signals_business_endpoint_total`
   - `signals_zero_business_total`
   - path family mix

## Rule P11: `SUPPLY_LINK` — это отдельный operational layer, а не просто provenance для PathBuilder

Ручной live diagnosis на `2026-03-07`:
- `supply_links=7771`
- `materialized_artifact_triad=7077 (91.1%)`
- `direct_supplies_edge=3311 (42.6%)`
- `transport_via=1514 (19.5%)`
- `direct_edge_with_transmission_qualifier=624 (8.0%)`
- `org_or_gov_endpoint=4677 (60.2%)`
- `supplierFacilityRef/customerFacilityRef=0%`

Смысл:
- `SUPPLY_LINK` не мёртвый слой.
- Но он был разделён на два мира:
  - pathbuilder/materialized artifacts знают про supply corridors;
  - propagation видел в основном только persistent dependency edges.
- Из-за этого `artifact_only` links не участвовали в real propagation как полноценные corridors.

Новая policy:
1. direct graph остаётся canonical truth.
2. `SUPPLY_LINK` используется как runtime operational overlay.
3. overlay добавляет virtual neighbors для propagation:
   - `supplier -> customer` как `SUPPLIES_TO`
   - `customer -> transport_via` как `DEPENDS_ON facet=logistics`
   - `transport_via -> customer` через обратный traversal того же dependency
4. если endpoint сидит на `/org|/gov`, overlay пытается operational fallback:
   - `/org/{geo}/{slug}` -> `/fac/{geo}/{slug}-general-production`
   - только как runtime resolution, без silent rewrite canonical graph

Файлы:
- `backend/internal/horizon/propagation/supply_link_overlay.go`
- `backend/internal/horizon/propagation/signal_propagation.go`

Почему это правильно:
- не засоряет Firestore тысячами synthetic edges
- использует уже существующий `SUPPLY_LINK` assumption layer
- делает pathbuilder knowledge доступным для propagation math

## Rule P12: `SUPPLY_LINK` transmission physics надо брать из нескольких уровней, а не ждать идеальных edge qualifiers

Live diagnosis:
- `flow_edge_contract` есть только у `343 / 7771`
- `share` есть только у `171 / 7771`
- `volume` есть только у `247 / 7771`

Значит transmission physics надо строить каскадно:
1. explicit metrics:
   - `share` -> `throughput_share`
   - `flow_edge_contract.full|partial`
2. route heuristics:
   - `transport_via` + chokepoint-like path -> `reroute_penalty`, `capacity_constraint`
3. fallback confidence:
   - `confidence` из `SUPPLY_LINK.State`

Реализация:
- virtual overlay edges получают:
  - `facet=supply|logistics`
  - `throughput_share` если есть `share`
  - transmission contract metadata
  - `transport_via`
  - chokepoint-sensitive reroute penalties при route-like paths

## Rule P13: `AFFECTED_ZONE` blast metrics должны усиливать propagation, а не жить отдельно от карты

Практический вывод:
- если `AFFECTED_ZONE` уже имеет:
  - `strategic`
  - `bridge`
  - `business_blast_radius`
  - `conflict_blast_radius`
- эти сигналы надо использовать как propagation prior.

Реализация:
- `BuildPropagationNodeOverlay(...)` теперь учитывает blast metrics для `AFFECTED_ZONE`
- bridge/business/conflict blast повышают:
  - `BridgeScore`
  - `OperationalScore`
  - `CriticalityScore`
- zone может стать:
  - `source`
  - `bridge`
  - `amplifier`

Смысл:
- зона перестаёт быть только картографическим объектом
- и становится полноправным элементом graph propagation kernel

## Следующая системная работа

После этих изменений bottleneck уже яснее:

1. `artifact_only` links теперь доступны kernel'у, но graph всё ещё беден metadata.
2. Нужен централизованный heuristic backfill для fragments:
   - `geoFeatureType`
   - `govType`
   - `marketDomain`
   - `marketVertical`
   - `industryPaths`
3. Нужен централизованный heuristic backfill для transmission qualifiers на materialized/persistent edges:
   - `throughput_share`
   - `reroute_penalty`
   - `capacity_constraint`
   - `substitutability`
   - `transport_via`
   - `dependency_facet`
4. Нужно жёстче штрафовать `/sphere/*` и generic `/product/*` как tails и intermediate hubs.

Практически это означает следующий отдельный слой работы:
- не ещё один matcher,
- а `graph enrichment + backfill + hub-penalty policy`.

## Rule P14: fragment/edge enrichment должен быть централизован в ядре, а не жить отдельно в scout/tools

Новая централизованная точка:
- `backend/internal/horizon/graphenrich/heuristics.go`

Что туда вынесено:
1. fragment heuristics:
   - `geoFeatureType`
   - `govType`
   - `marketDomain`
   - `marketVertical`
   - `industryPaths`
   - chokepoint tag
   - normalization of `marketSegments`
2. edge heuristics:
   - `transport_via` alias normalization
   - `facet` inference
   - transmission qualifier normalization
   - transmission contract metadata
   - ownership percent mirroring
   - temporal modality normalization
   - route-constrained defaults:
     - `reroute_penalty`
     - `capacity_constraint`
     - `substitutability`

Policy:
1. repo boundary должен применять эти эвристики всегда:
   - fragment create/update
   - edge create/update
2. scout materialization должен использовать ту же функцию, а не свою локальную копию
3. backfill tool должен использовать тот же repo path, а не ручные Firestore patch rules

Файлы:
- `backend/internal/horizon/repo/fragments.go`
- `backend/internal/horizon/repo/dependency_edges.go`
- `backend/internal/horizon/scout/edge_enrichment.go`

## Rule P15: backfill должен использовать тот же код, что и production path

Новый tool:
- `backend/cmd/tools/horizon_backfill_graph_heuristics/main.go`

Назначение:
1. dry-run оценка плотности недостающих derived fields
2. controlled apply без LLM
3. повторяемый backfill после расширения эвристик

Режимы:
- `--mode=fragments`
- `--mode=edges`
- `--mode=all`
- `--path-prefix=/loc/...` для прицельной калибровки

Примеры:
```bash
cd backend
go run ./cmd/tools/horizon_backfill_graph_heuristics --project=etigram --mode=all --page-size=250 --limit=1000
go run ./cmd/tools/horizon_backfill_graph_heuristics --project=etigram --mode=fragments --path-prefix=/loc/sea --limit=500 --apply
go run ./cmd/tools/horizon_backfill_graph_heuristics --project=etigram --mode=edges --path-prefix=/fac/ir --limit=1000 --apply
```

Live results on `2026-03-07`:
1. dry-run `limit=1000`
   - `fragments_scanned=1000`
   - `fragments_changed=528`
   - `edges_scanned=1000`
   - `edges_changed=679`
2. strongest fragment fields:
   - `index_paths=465`
   - `market_domain=336`
   - `market_vertical=336`
   - `industry_paths=325`
   - `geo_feature_type=184`
3. strongest edge fields:
   - `qualifiers=679`
4. after applying first live batch and re-checking first `500` docs:
   - `fragments_changed=48`
   - `edges_changed=53`
   - residual deltas were mostly:
     - `geo_feature_type`
     - some remaining edge qualifiers

Interpretation:
- централизованный heuristic layer реально закрывает существенную часть graph sparsity без LLM
- после первой apply-пачки основной массовый хвост уже снят
- остатки стали более точечными и пригодными для targeted calibration

## Дальнейшая калибровка

1. Гонять `horizon_backfill_graph_heuristics` сначала в dry-run, потом apply на целевых префиксах:
   - `/loc/sea`
   - `/fac`
   - `/market`
   - конфликтные зоны `/zone/...`
2. Отдельно смотреть fields, которые после backfill почти не двигаются:
   - это сигнал, что нужны не эвристики, а richer canonical graph data
3. После каждого крупного прохода сверять:
   - `horizon_graph_diagnose`
   - `horizon_supply_link_diagnose`
   - live RSS graph audit
4. Если после enrichment сигнал всё ещё умирает на длинных цепочках:
   - проблема уже не в derived fields,
   - а в topology / edge semantics / hub penalties / propagation weights

## Rule P16: Geo authority в ядре должна быть явной

Принцип:
- координаты для graph/runtime path должны идти от geocoding chain, а не от KB-side services.

Authority order:
1. `Google Geocoding`
2. `Nominatim`
3. parent propagation / existing fragment coordinates

Не считаются coordinate authority:
- `Wikidata P625`
- `GeoNames`

Policy:
1. `ScoutEngine` использует `Google -> Nominatim`
2. `PreCreateValidator` использует `Wikidata` только для labels/IDs/industry, но не для coordinates
3. `GeoNames` остаётся для deterministic geo IDs / normalization, но не пишет coordinates
4. `Ontology facts` тоже используют `Google -> Nominatim`, без `GeoNames` как coordinate authority

Причина:
- `Wikidata` и `GeoNames` полезны как KB/reference services,
- но spatial identity для business graph должна определяться geocoding chain.

## Rule P17: Facility canonical path не может оставаться `/fac/na/...`, если страна уже известна

Принцип:
- если у facility уже есть `homeLocation.iso3166_1`, canonical path обязан быть geo-scoped.

Policy:
1. при pre-create normalization:
   - `/fac/na/{slug}` -> `/fac/{iso2}/{slug}`
2. upgrade делается до persist, чтобы dedup/retarget работали на правильном identity path
3. существующие legacy `/fac/na/...` fragments требуют отдельного controlled backfill:
   - redirects
   - retarget
   - edge/lens/path rewrite

Причина:
- `/fac/na/...` при известной стране ломает:
  - semantic seeding
  - endpoint ranking
  - propagation specificity
  - downstream graph interpretation

## Rule P18: `homeLocation.iso3166_1` не является source of truth без place re-validation

Принцип:
- для facilities `homeLocation.iso3166_1` нельзя слепо доверять:
  - он может быть угадан LLM,
  - он может быть унаследован из partial update,
  - он может противоречить самому facility name/place clue.

Policy:
1. facility geo revalidation выполняется в core persistence path:
   - pre-create validator
   - create path
   - dedup merge path
   - update path
2. если facility geography подтверждается geocoding по `facility name + place hint`, система:
   - исправляет `homeLocation.iso3166_1`
   - пишет `geoVerifiedCountryAlpha2`
   - поднимает coordinates
   - апгрейдит `/fac/na/...` -> `/fac/{iso2}/...`
3. если текущий `iso3166_1` совпадает с geocode result только потому, что query уже содержит этот ISO, это НЕ считается достаточным подтверждением для `/fac/na/...`
4. conflicting geo (`Shanghai + US`, `Dresden + US`, `Bengaluru + US`) должен исправляться или оставаться unresolved, но не сохраняться как “валидный”

Controlled backfill policy:
- массовый backfill legacy `/fac/na/...` допускается только для geocode-revalidated cases.
- old `homeLocation.iso3166_1` without revalidation is insufficient for migration.

Причина:
- именно blind trust к `homeLocation.iso3166_1` создавал большую часть facility geo debt.
- иначе backfill не лечит систему, а закрепляет старые ошибки в canonical paths.

## Rule P19: Low-information facility slugs must not act as identity anchors

Принцип:
- facility canonical paths вроде:
  - `/fac/na/general-production`
  - `/fac/na/general-distribution`
  - `/fac/na/casablanca-plant`
  - `/fac/na/port-of-shanghai`
  несут слишком мало identity information и создают mass collisions.

Policy:
1. low-information facility slug определяется не по одному слову `plant/factory`, а по числу distinctive tokens;
2. если `/fac/na/{slug}` low-information:
   - exact-path deduplication не должен считаться надёжным identity match;
   - path должен считаться collision-prone;
   - система должна предпочитать:
     - stronger geo grounding,
     - operator/site-specific slug,
     - exact label/identifier match,
     - normalized-name match within grounded geo;
3. duplicate cleanup по таким путям нельзя делать слепым merge по canonicalPath alone;
4. такие пути должны попадать в debt diagnostics отдельно от обычных `/fac/na/...`.

Причина:
- часть graph debt возникает не от отсутствия ISO itself, а от слабого facility slug;
- blind exact-path dedup на таких путях склеивает разные реальные объекты;
- для early-warning это особенно вредно, потому что propagation потом “видит” ложный operational node.
