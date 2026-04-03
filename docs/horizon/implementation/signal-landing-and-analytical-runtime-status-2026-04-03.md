# Signal Landing и Analytical Runtime: статус на 2026-04-03

## Зачем всё это делалось

Изначальная цель была не просто ускорить ответы, а перестроить длинный контур целиком:

1. Сигнал или новость из реального мира должны корректно приземляться на сущности, продукты, маршруты, chokepoints и affected zones.
2. Система должна уметь отвечать не только на `what exists`, но и на `weak points / bottlenecks / critical dependencies / disruption impact`.
3. Для этого нужен не новый внешний scout, а новый внутренний materialized слой:
   - factual substrate уже есть;
   - analytical substrate должен быть прогрет заранее;
   - signal landing должен быть единым и независимым от канала доставки.
4. Небольшие изменения сигналов не должны приводить к полному пересчёту всего мира.
5. Прогрев должен быть многотемпературным и bounded:
   - не греть весь мир;
   - греть фокусные линзы, watch subjects и соседние impact neighborhoods.

## Что хотели получить

### 1. Analytical substrate поверх factual substrate

Нужен отдельный persisted слой аналитики:

- primitives
- aggregates
- scenario outputs
- warmed analytical docs/capsules

чтобы риск-вопросы шли из уже подготовленного знания, а не собирались заново на лету.

### 2. Единый signal landing plane

Нужен отдельный первый слой, который:

- принимает событие/сигнал вне зависимости от канала;
- раскладывает его на impact claims;
- сажает claims на графовые сущности;
- создаёт missing-anchor candidates, если сущности явно важны, но не найдены;
- обязательно создаёт affected zones для shock/disruption-категорий, где геозона по смыслу обязательна.

### 3. Многотемпературный warmup

Нужно уйти от бинарного `ready/not ready` к слоям:

- `flash`
- `hot`
- `warm`
- `full`

чтобы:

- маленькие дельты обновляли верхние слои быстро;
- сложные полные прогревы оставались асинхронными;
- rich lens отвечали быстро по уже прогретым данным.

### 4. Delta-first refresh

Нужно было добиться, чтобы:

- changed refs не запускали полный пересчёт линзы;
- propagation строила bounded affected delta;
- analytical refresh шёл только по затронутым scope’ам;
- Redis и projection indexes готовили основу для секундного hot path.

## Что уже реализовано

## A. Analytical runtime

Реализован отдельный analytical слой поверх graph/materialized substrate:

- analytical primitives
- analytical aggregates
- scenario/baseline separation
- ref-scope projections
- compiled watch projections
- analytical runtime refresh по changed refs

Ключевые направления изменений:

- `backend/internal/horizon/analytical/`
- `backend/internal/horizon/models/analytical_knowledge.go`
- `backend/internal/horizon/models/analytical_aggregate.go`
- `backend/internal/horizon/models/analytical_runtime.go`
- `backend/internal/horizon/repo/analytical_knowledge.go`
- `backend/internal/horizon/repo/analytical_aggregates.go`
- `backend/internal/horizon/repo/ref_scope_projections.go`
- `backend/internal/horizon/repo/path_family_projections.go`
- `backend/internal/horizon/repo/compiled_watch_subjects.go`

## B. Delta-first refresh и bounded hot path

Сделан сдвиг от тяжёлого пересчёта к bounded refresh:

- propagation больше не является главным bottleneck;
- маленькие signal/metric deltas теперь обновляют только локальный analytical хвост;
- добавлен Redis-aware runtime path;
- появилась projection-driven логика вместо разбросанных lookup fan-out.

Ключевые места:

- `backend/internal/horizon/propagation/engine.go`
- `backend/internal/cache/redis.go`
- `backend/internal/horizon/analytical/runtime.go`
- `backend/cmd/tools/horizon_propagate_metric/main.go`

## C. Многотемпературный internal knowledge warmup

Прогрев переведён на staged-модель вместо грубого `ready`:

- `hot`
- `warm`
- `full`

Под это переделаны:

- prepared builder
- warmup service
- lens chat / lens console demand behavior
- warmup tool freshness semantics

Ключевые места:

- `backend/internal/horizon/internalknowledge/prepared_builder.go`
- `backend/internal/horizon/internalknowledge/warmup_service.go`
- `backend/internal/horizon/lens/service.go`
- `backend/internal/http/horizon/lens_chat.go`
- `backend/internal/http/horizon/lens_console.go`
- `backend/cmd/tools/horizon_warm_internal_knowledge_lens/main.go`

## D. Signal Landing Plane

Появился отдельный persisted слой signal landing:

- `SignalLandingSession`
- `SignalImpactClaim`
- `SignalLandingHit`
- `SignalMissingAnchorCandidate`
- `SignalLandingSummary`

Он теперь:

- не зависит только от одного канала;
- умеет materialize confirmed hits;
- фиксирует missing anchors;
- включает affected zones в контракт там, где это обязательный класс сигнала.

Ключевые места:

- `backend/internal/horizon/models/signal_landing.go`
- `backend/internal/horizon/repo/signal_landing.go`
- `backend/internal/horizon/signallanding/service.go`
- `docs/horizon/architecture/signal-landing-plane.md`

## E. Интеграция landing с worldmonitor/watch/warmup

Landing встроен в:

- `SignalPipeline`
- `HorizonMonitorPipeline`
- watch runtime monitoring snapshot
- prepared corpus build

Добавлен серверный hook:

- `landing -> impacted refs -> impacted lenses -> RequestInternalKnowledgeWarmup(...)`

Ключевые места:

- `backend/internal/horizon/scout/signal_pipeline.go`
- `backend/internal/discovery/worldmonitor_pipeline.go`
- `backend/internal/horizon/watchruntime/service.go`
- `backend/internal/horizon/watchruntime/monitoring.go`
- `backend/internal/horizon/internalknowledge/prepared_builder.go`
- `backend/cmd/server/main.go`

## Что подтверждено реальными прогонами

## 1. Базовый factual/analytical warm path на линзах работает

Для уже богатых линз система отвечает осмысленно на:

- `weak points in EU EV battery supply chain`
- `what happens if Port of Hamburg is disrupted`

и действительно вытаскивает:

- chokepoints
- порты
- corridor concentration
- downstream plants

Это не идеальный latency, но это уже не “пустая импровизация”, а grounded answer из прогретого factual + analytical substrate.

## 2. Малые дельты перестали тянуть тяжёлый полный пересчёт

Живой кейс:

- менялся риск-сигнал на `Port of Hamburg`;
- затем делался rollback.

Результат:

- propagation сама по себе быстрая;
- bounded analytical refresh обновляет только затронутые primitives/aggregates;
- в VPC на Redis-backed hot path получены секундные значения порядка `0.55s - 0.60s` для fast refresh части.

Это ключевое подтверждение, что архитектурный сдвиг в правильную сторону уже состоялся.

## 3. Реальный сигнал из мира доезжает до графа и удалённых узлов

Был прогнан реальный Gulf/Hormuz-style world signal через replay path.

Подтвердилось:

- на Saudi/Gulf refs реально создаются события и сигналы;
- propagated metrics доходят до удалённых infra nodes;
- affected refs появляются не только на географии, но и на downstream facilities.

Это подтверждает, что длинный путь `world signal -> graph impact` уже работает.

## 4. Signal landing материализуется как отдельное знание

На живом тестовом сценарии landing показал:

- confirmed refs:
  - `/loc/sa`
  - `/loc/qa`
  - `/loc/om`
  - `/loc/ir`
  - `/loc/int/strait-of-hormuz`
- `zoneEligibility=required`
- `missingPreciseZone=true`
- missing candidates:
  - `helium`
  - `lng`

Это очень важный результат:

- сигнал не только “что-то зашевелил”;
- он сел на реальные сущности;
- и явно показал, что в графе ещё не хватает продуктовых anchors.

## 5. Частичный warmup действительно bounded

Подтверждено на scratch и rich lenses:

- cold lens честно отвечает, что она не прогрета;
- `hot` warmup поднимает только небольшой локальный corpus;
- `full` warmup остаётся отдельным, более богатым процессом.

То есть частичный прогрев уже не означает перегрев “всего мира”.

## 6. Деплой новой серверной ревизии завершён

На момент этого статуса:

- API задеплоен на revision `nv-horizon-api-00056-v27`
- service URL: `https://nv-horizon-api-vosmg5vzoa-uc.a.run.app`

Это важно, потому что следующие прогоны должны уже идти через live server path, а не только через локальные tool-runtime сценарии.

## Что пока не доведено до конца

## 1. Risk answer path ещё не стал полностью warmed-first

Сейчас:

- factual warm path уже относительно хороший;
- analytical substrate уже добавлен;
- но answer path по risk/disruption вопросам всё ещё местами слишком зависит от corpus retrieval + synthesis.

Итог:

- ответы стали лучше;
- но на rich risk questions latency всё ещё может быть десятки секунд.

## 2. Auto-warm impacted domain/product lenses ещё не доказан до конца на live server path

Серверный hook уже добавлен:

- landing result refs
- ref projections
- impacted lenses
- `RequestInternalKnowledgeWarmup(...)`

Но его нужно дожать именно живым e2e-сценарием на deployed API.

Особенно важно проверить:

- helium lens
- reverse osmosis membrane lens
- desalination / water infrastructure lens

для Hormuz/Qatar-style сигналов.

## 3. Watch subject ещё не стал главным first-class runtime focus layer

Сейчас есть:

- compiled/runtime groundwork
- integration с landing summaries и monitoring snapshot

Но ещё не доказано в полном цикле, что:

- именно фокусные watch subjects получают signal hit первыми;
- они будят bounded warmup;
- и не приводят к global over-warm.

## 4. Affected zone precision ещё нужно улучшать

Сейчас система уже умеет:

- требовать zone output для shock/disruption cases;
- честно помечать `missingPreciseZone`.

Но нужно улучшить следующий шаг:

- точная геозона должна чаще материализоваться не только как `country + chokepoint refs`, но и как более точный area/zone object.

## 5. Live ingestion path от real feed ещё требует финального доказательства

На deployed API был начат live-путь через обычный source ingest.

Выяснилось:

- debug endpoint `events:inject` слишком тяжёл как live trigger;
- `/api/gdelt-doc` на публичном URL сервиса неожиданно даёт `404`;
- из-за этого для следующего цикла нужен контролируемый live feed path:
  - либо отдельный временный public bucket/feed snapshot;
  - либо исправление внешнего `/api/gdelt-doc` route;
  - либо реальный existing feed source, который уже доступен через API.

То есть не архитектура сломана, а конкретный live feed path для удобного теста пока не добит.

## Что нужно доделать дальше

## Приоритет 1. Доказать live server path до конца

Нужно сделать один чистый e2e:

1. real or controlled-real feed snapshot
2. deployed API source ingest
3. event creation
4. signal landing summary
5. affected refs
6. impacted lens warmup
7. changed retrieval / changed answer

Предпочтительный путь:

- либо починить `/api/gdelt-doc` на публичном API;
- либо поднять временный public feed snapshot и прогнать через обычный `horizon_monitor_feed`.

## Приоритет 2. Довести risk-answer path до analytical-first

Нужно, чтобы для classes:

- weak points
- bottlenecks
- critical dependencies
- disruption-sensitive nodes
- disruption impact

ответ шёл сначала через:

- analytical aggregates
- signal landing summaries
- primitives/evidence

а уже потом через общий corpus fallback.

## Приоритет 3. Довести watch subjects до роли основного focus driver

Нужно, чтобы `WatchSubject` стал не просто объектом внимания, а runtime-first механизмом:

- выделения focus zones;
- selective warmup;
- bounded propagation;
- bounded refresh.

## Приоритет 4. Добавить structural importance prior в landing

Сейчас видно, что одного semantic hit недостаточно.

Нужен landing ranking, который учитывает:

1. semantic grounding quality
2. structural importance
3. dependency richness
4. chokepoint/product criticality
5. downstream amplification potential

То есть хорошими кандидатами посадки сигнала должны становиться не только “ближайшие по тексту” сущности, но и:

- chokepoints
- product families
- infrastructural hubs
- узлы с высокой downstream fragility

При этом важно не скатиться в тупой bias на “самые большие узлы”:

- слабый сигнал тоже может иметь огромные последствия;
- поэтому ranking должен сочетать semantic truth и structural amplification, а не заменять одно другим.

## Что именно важно помнить следующему агенту

### 1. Главный архитектурный поворот уже сделан

Дальше работа не про изобретение новой схемы, а про добивку already-correct layering:

- signal landing plane
- analytical substrate
- multitemperature warmup
- bounded delta refresh

### 2. Не надо смешивать каналы и landing

Канал доставки может быть любым:

- worldmonitor
- ручной inject
- source ingest
- watch replay

Но landing должен быть единым.

### 3. Affected zones обязательны только для zone-eligible shock categories

Не надо расширять эту логику на:

- corporate news
- holidays
- generic business updates

Affected zone — это именно карта территории встряски.

### 4. Главная незакрытая живая проверка

Следующий цикл должен довести сценарий:

`live feed ingest -> landing -> impacted product/domain lenses -> selective warmup -> changed answer`

Особенно на:

- Hormuz / Gulf
- helium
- LNG
- reverse osmosis membranes
- desalination / water infra

## Команды и артефакты, от которых удобно продолжать

### Полезные tools

- `backend/cmd/tools/horizon_inspect_signal_landing`
- `backend/cmd/tools/horizon_replay_worldmonitor_doc`
- `backend/cmd/tools/horizon_warm_internal_knowledge_lens`
- `backend/cmd/tools/horizon_lens_chat_smoke`
- `backend/cmd/tools/horizon_propagate_metric`

### Полезные live сущности

- API URL:
  - `https://nv-horizon-api-vosmg5vzoa-uc.a.run.app`
- Saudi scratch lens:
  - `Kdn5G1AR4rfCkLwvQTse`
- Rich Gulf / logistics related lenses нужно дополнительно переотобрать live list’ом
- временный live source, созданный для GDELT Hormuz test:
  - `tQd7xQ3sfe8jqSwva9DD`

### Последняя незавершённая операция

Во время последнего цикла был начат путь через временный public feed snapshot:

- `/tmp/hormuz_feed.json` создан
- uploaded to:
  - `gs://nv-horizon-test-nv-horizon-fragments/codex/hormuz_feed.json`
- попытка перейти к signed/public URL path была не завершена

Следующему агенту проще всего:

1. либо сделать отдельный public bucket/object;
2. либо починить/использовать уже готовый публичный feed route;
3. либо использовать существующий реальный horizon monitor source через API.

## Короткий вывод

Мы уже добились следующего:

- система перестала быть только factual substrate;
- появился реальный analytical substrate;
- появился отдельный signal landing plane;
- появился bounded delta refresh;
- появился staged warmup;
- появился честный missing-anchor слой;
- live deploy новой ревизии завершён.

Чего ещё не хватает до по-настоящему законченного продового контура:

- доказать live server path до product/domain lenses;
- сделать risk-answer path analytical-first;
- довести watch subject до полноценного focus-runtime;
- улучшить landing ranking через structural importance + semantic grounding.
