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

## 6. Деплой новых серверных ревизий завершён

В течение дня и ночного цикла были последовательно выкачены live-revisions:

- `nv-horizon-api-00064-wfk`
- `nv-horizon-api-00067-z25`
- `nv-horizon-api-00068-jpv`
- `nv-horizon-api-00069-6b4`
- `nv-horizon-api-00070-wzm`
- `nv-horizon-api-00071-sm4`
- `nv-horizon-api-00072-vvr`
- `nv-horizon-api-00073-jbk`

Текущий сервис URL:

- `https://nv-horizon-api-vosmg5vzoa-uc.a.run.app`

Это важно, потому что проверка шла уже через deployed API path, а не только через локальные tool-runtime сценарии.

## 7. Исправлен критичный live-баг public/global ingest

В server path был найден и исправлен инфраструктурный дефект:

- public/global source документы писались в GCS с пустым tenant segment;
- Vertex sync получал URI вида `gs://bucket//source/doc.txt`;
- из-за этого live ingest мог ломаться до `landing -> warmup`.

Исправление:

- `DocumentsRepository` теперь нормализует пустой tenant segment в `global` для object path;
- тот же `global` tenant пишется в storage metadata;
- добавлен unit test на это поведение.

Результат:

- live `horizon_monitor_feed` ingest снова проходит по обычному server path;
- новый `SignalLandingSession` стал materialize’иться уже из live API ingest, не только из replay tools.

## 8. Live API path до signal landing подтверждён

После исправления ingest path был прогнан через live source:

- source: `kauQCSBHBhOhUXiKkhUk`
- scenario: Hormuz / Gulf disruption

Подтвердилось:

- `IngestSource` реально стартует на Cloud Run;
- создаётся новый `SignalLandingSession`;
- confirmed hits и missing candidates materialize’ятся;
- `SignalLandingSummary` обновляется на live server path.

Подтверждённые live landing refs:

- `/loc/sa`
- `/loc/qa`
- `/loc/ir`
- `/loc/int/strait-of-hormuz`
- `/event/horizon-monitor/na/codex_hormuz_20260403_1`

Подтверждённые missing candidates:

- `helium`
- `lng`
- unresolved precise affected zone around Iran side

## 9. Найден и исправлен логический баг selective warmup

После live landing стало видно, что stale projection plane может искажать impacted lens selection.

Реальный дефект:

- `ref -> scope projection` для `/loc/sa` указывал на Oman lens;
- hook trust’ил projection как достаточное основание;
- при наличии projection-hit не делался fallback по реальному `lens.scope`;
- из-за этого корректные Saudi-focused линзы не будились.

Исправление:

- server hook теперь объединяет:
  - `projectionHits`
  - `scopeFallbackHits`
- stale projection больше не может заблокировать корректные линзы.

Live-подтверждение после выката:

- hook log на `nv-horizon-api-00064-wfk`:
  - `matchedLenses=4`
  - `projectionHits=1`
  - `scopeFallbackHits=3`

Это важное доказательство, что correctness теперь не зависит только от projection plane.

## 10. Live signal реально будит фокусные линзы

После последнего фикса signal landing дошёл до selective warmup не только логически, но и в persisted state.

Live-наблюдения:

- `Codex Lens Chat Smoke: /loc/sa`
  - `warmStatus=queued`
  - `warmReason=watch_signal_landing_delta`
- `UAE and Saudi Arabia Space Industry Value Chain Mapping`
  - `warmStatus=queued`
  - `warmReason=watch_signal_landing_delta`

То есть live signal уже будит не весь мир, а конкретные impacted lenses.

Дополнительный operational хвост:

- stale Oman projection lens `x6cfAg9pBzPPNVIC8n5A` всё ещё попадает как projection-hit;
- для части линз persisted warm state write даёт `DeadlineExceeded`;
- historical failed/running states старых warmup запусков ещё загрязняют картину.

Но это уже не ломает correctness самого сигнального контура.

## 11. Частично подтверждён changed answer path

На Saudi scratch lens был прогнан вопрос:

- `What disruptions are affecting Saudi Arabia right now and why do they matter downstream?`

Результат:

- длительность около `14s`;
- answer path увидел `signal_landing_summary` и analytical aggregates;
- в highlighted paths вошли:
  - `/loc/sa`
  - `/loc/int/strait-of-hormuz`
  - `/loc/ir`
  - `/loc/qa`

Это ещё не идеальный product answer, но уже не purely factual answer и не purely ad hoc synthesis.

## 12. Найден и исправлен корневой баг warmup state persistence

В live-диагностике выяснилось, что часть warmup-состояний выглядит “зависшей” не только из-за исторического хвоста, но и из-за конкретного бага записи:

- warmup мог дойти до реального импорта и даже логировать `ready`;
- но nested field `viewConfig.internalKnowledge` обновлялся не тем способом;
- в результате Firestore иногда оставался в старом `queued`/`running`, хотя фактический прогрев уже сдвинулся.

Исправление:

- добавлен целевой nested update через Firestore `Update(...)`, а не косвенное поле merge;
- `WarmupService` больше не переписывает весь lens document ради state transition;
- состояние `internalKnowledge` обновляется отдельно и предсказуемо.

Подтверждение:

- Saudi scratch lens `/loc/sa` (`Kdn5G1AR4rfCkLwvQTse`) после repair-run получил свежий `updatedAt`, новый `substrateHash` и корректный `runningAt`;
- это подтвердило, что ядро теперь действительно пишет state туда, куда нужно.

## 13. Найден и частично устранён второй корень хвостов: слишком дорогой queued-state и reconcile I/O

После исправления nested update стало видно следующее:

- ядро всё ещё ловит `DeadlineExceeded` на части queued-state writes;
- reconcile периодически ловит `DeadlineExceeded` на чтении списка линз;
- это уже не ломает correctness, но продолжает создавать operational noise и stale-state хвосты.

Что исправлено:

- queued-state больше не делает лишний предварительный `lens.Get(...)` перед записью;
- добавлен patch-update отдельных `internalKnowledge.*` полей без полного reread;
- reconcile теперь читает только `viewConfig.assemblyStatus` и `viewConfig.internalKnowledge`, а не весь `viewConfig`;
- server config для warmup ужесточён в сторону меньшего I/O fan-out:
  - `StatusWriteTimeout = 45s`
  - `ReconcileInterval = 5m`
  - `ReconcileLimit = 24`
- upload prepared docs в GCS получил retry, чтобы transient TLS/network ошибки не превращались в permanent failed warmup.

Это именно hardening ядра, а не одноразовая ручная зачистка.

## 14. Автолечение исторического хвоста уже подтверждено

После broadened reconcile eligibility и live repair-run’ов стало видно, что старые зависшие warmup состояния начинают оживать сами:

- `Saudi Arabia Oil Export Diversification and Logistics Infrastructure` (`pbOu4sMhN1mCB4SbkwEw`) перешёл из старого failed-хвоста в `queued` с причиной `failed_reconcile_retry`;
- старый Hamburg scratch lens (`AYbd3mwrlJbtCoHQMCs5`) получил новый `repair_stale_running` и свежий `substrateHash`;
- богатая `EU auto + EV global` линза уже ранее самовосстановилась через `queued_reconcile` и остаётся `ready`.

Это значит, что мы уже не просто вручную чиним хвосты, а заставляем ядро их перерабатывать.

## 15. Analytical shortcut на risk-вопросах реально заработал хотя бы на части линз

Для Hamburg full-warmed lens (`0wM2CR0UThd6VEK7NGNh`) был повторно прогнан вопрос:

- `What weak points exist around Port of Hamburg right now?`

Результат:

- ответ пришёл примерно за `12.4s`;
- `search_internal_knowledge.result.analyticalShortcut = true`;
- ответ действительно опирался на prewarmed analytical aggregate/primitives, а не на долгую допланировку.

Из ответа уже стабильно вытягиваются:

- `sole_source_lock_in`
- `low_substitutability`
- `inventory_vulnerability`
- конкретные связанные узлы вокруг Hamburg / Eurogate / BASF / Wolfsburg

Это важное доказательство, что warmed analytical layer уже работает как primary source хотя бы на части risk-class вопросов.

## 16. Product/domain gap отделён от warmup/landing проблем

Для missing candidates `helium` и `lng` был отдельно проверен canonical product control plane.

Результат:

- `/product/raw/gas/extraction/crude-helium` — `known=false`
- `/product/finished/mechanical/assembly/helium` — `known=false`
- `/product/finished/mechanical/assembly/liquefied-natural-gas-lng` — `known=false`

Это критично для интерпретации текущего статуса:

- landing работает правильно, когда поднимает `helium` и `lng` как missing anchors;
- но product/domain auto-warm дальше не взлетает не потому, что broken warmup;
- а потому, что сам canonical product plane ещё не содержит эти product anchors.

Иными словами: здесь уже упираемся не в signal landing, а в control-plane gap.

## 17. Redis-path в коде есть, но live Cloud Run всё ещё не может достучаться до Memorystore

По live logs на актуальных ревизиях продолжает подтверждаться:

- `redis: connection pool: failed to dial ... dial tcp 10.1.0.3:6379: i/o timeout`

Что это означает:

- Redis runtime path в коде есть и fallback не ломает систему;
- но Cloud Run service пока не имеет реально работающей сетевой связности с `nv-horizon-redis`;
- это инфраструктурный blocker, а не логическая ошибка analytical/runtime слоя.

Следствие:

- секундный Redis-first hot path подтверждён из среды внутри VPC;
- но на live API он пока не станет основным, пока не исправлена VPC/Memorystore connectivity.

## 18. Ночной live hardening на новых ревизиях подтвердил landing, но добил только часть warmup-хвоста

После дополнительных выкладок на `00071/00072/00073` были подтверждены следующие вещи:

- live ingest снова создаёт новый `SignalLandingSession` и обновляет `SignalLandingSummary` уже на таймштампах около `18:42Z`;
- свежая landing-сессия на `00073` снова села на:
  - `/loc/int/strait-of-hormuz`
  - `/loc/ir`
  - `/loc/qa`
  - `/loc/sa`
- missing candidates `helium` и `lng` по-прежнему materialize’ятся корректно;
- на свежих ревизиях не зафиксировано новых log lines вида:
  - `failed to persist queued state`
  - `reconcile list failed`

Это важный operational progress: ядро на новых ревизиях перестало явно шуметь теми timeout-ошибками, которые раньше были постоянными.

## 19. Введён режим `refresh поверх уже готовой температуры`

Чтобы новый сигнал не деградировал линзу обратно в грубый `queued`, когда полезная температура уже достигнута, добавлен режим refresh поверх existing warm state.

Что изменилось:

- `RequestInternalKnowledgeWarmup(...)` теперь распознаёт, когда нужная температура уже есть;
- в этом случае включается режим `refresh:...`;
- persisted state теперь пишет специальные поля:
  - `refreshQueuedAt`
  - `refreshRunningAt`
- это позволяет обновлять warmup без обязательного уничтожения уже доступного слоя.

Подтверждение в live query на Saudi scratch lens:

- после нового ingest в `warmState` появились `refreshQueuedAt` и `refreshRunningAt`;
- свежие `signal_landing_summary` снова попали в retrieval;
- user-visible answer на вопрос про Saudi disruptions остался в диапазоне примерно `13-15s`.

Ограничение:

- one-off full repair на Saudi scratch lens затянулся и был остановлен;
- поэтому для этой конкретной линзы остался legacy `queuedAt`/`runningAt` хвост от предыдущих циклов;
- то есть новый режим уже живёт рядом с хвостом, но ещё не вычистил все старые поля исторически.

## 20. Появился cycle-inspector для полного среза `landing -> impacted lenses -> warmup`

Добавлен расширенный режим в tool:

- `backend/cmd/tools/horizon_inspect_signal_landing`
- новый флаг `--include-cycle`

Теперь tool умеет показать в одном ответе:

- `landingCreatedAt`
- `landingUpdatedAt`
- `landingDurationMs`
- impacted refs
- impacted lenses
- текущую usable temperature по каждой линзе
- `landing -> hot/warm/full` latency по каждой линзе

Live-срез по свежей Hormuz/Gulf landing session показал:

- `landingDurationMs ~= 7599`
- impacted refs:
  - `/event/horizon-monitor/na/codex_hormuz_20260403_1`
  - `/loc/int/strait-of-hormuz`
  - `/loc/ir`
  - `/loc/qa`
  - `/loc/sa`
- impacted lenses: `4`

На этом же срезе:

- Saudi scratch lens (`Kdn5G1AR4rfCkLwvQTse`) уже `ready/full`
  - `landingToWarmMs ~= 426799`
  - `landingToFullMs ~= 478898`
- rich impacted lenses вроде `Ojkm...`, `pbOu...`, `x6cf...` тоже уже `ready/full`
  - но с latency на десятки минут, что и даёт понятный следующий optimization target

То есть цикл теперь не только “кажется рабочим”, а реально измеряется.

## 21. Старый reconcile-хвост прижат ещё сильнее

После очередного live прогона текущий срез warmup states стал существенно чище:

- `Kdn...` (`/loc/sa`) — `ready`, `watch_signal_landing_delta`
- `Ojkm...` — `ready`, `watch_signal_landing_delta`
- `pbOu...` — `ready`, `watch_signal_landing_delta`
- `x6cf...` — `ready`, `watch_signal_landing_delta`
- `AYbd...` — `ready`, `stale_running_reconcile`

Это означает, что основная масса линз уже не зависает в бесконечных `queued/running`, а действительно доходит до usable state.

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

Но его нужно дожать именно живым e2e-сценарием на deployed API для product/domain lenses, а не только для geography-focused lenses.

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

## 5. Projection plane ещё нуждается в hardening

Теперь это подтверждено не теоретически, а live-дефектом:

- projection для `/loc/sa` указывал на Oman lens;
- без scope fallback это ломало бы impacted lens selection.

Следствия:

- нужен rebuild / verification loop для projection plane;
- projection должен оставаться `projection-first`, но не `projection-only`;
- stale projection state должен быть наблюдаем и ремонтируем.

## 6. Warmup status semantics всё ещё слегка путают operational картину

По staged-модели `hot -> warm -> full` часть линз может выглядеть как `running`, даже если `hot` уже готов и query path уже может использовать этот слой.

Это не баг корректности, но это:

- затрудняет чтение live состояния;
- может выглядеть как хвост даже там, где полезная температура уже достигнута;
- требует либо отдельного UI/ops представления “какая температура уже готова”, либо дальнейшей нормализации статуса.

Дополнительное live-наблюдение:

- на новых ревизиях часть старых rich lenses всё ещё может получить `queued_reconcile`, даже если раньше уже были `ready`;
- это значит, что reconcile semantics и исторический warm state ещё не полностью согласованы;
- это уже не ломает landing correctness, но остаётся отдельным хвостом, который надо добить.

## Что нужно доделать дальше

## Приоритет 1. Довести live path от impacted warmup до richer changed answer

Сейчас уже доказано:

1. deployed API source ingest
2. event creation
3. signal landing summary
4. affected refs
5. impacted lens warmup enqueue
6. changed answer на geography scratch lens

Следующий шаг:

- richer changed answer на domain/product lenses после queued/running warmup;
- особенно для:
  - helium
  - LNG
  - reverse osmosis membranes
  - desalination / water infra

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

## Обновление: BigQuery и Redis hardening

### BigQuery

Корневая причина шума оказалась не в BigQuery как сервисе, а в рассинхроне между infra и runtime:

- deploy/setup infra создавал dataset `nv_horizon_test_documents`
- API runtime по умолчанию смотрел в `nv_horizon_documents`

Из-за этого сервис поднимался, включал BigQuery sync, а потом начинал шуметь `not found`.

Что исправлено:

- `scripts/deploy` теперь явно прокидывает `BIGQUERY_DATASET_ID`, вычисленный из project id
- server init считает BigQuery обязательным, если dataset ID задан явно
- live revision `nv-horizon-api-00076-2jz` стартовала с:
  - `BigQuery AI service enabled: project=nv-horizon-test, dataset=nv_horizon_test_documents`
  - без прежнего `Dataset ... not found`

### Redis / live hot path

С Serverless VPC Access connector в этом проекте воспроизвелась инфраструктурная проблема самого GCP:

- connector creation несколько раз падал в `ERROR`
- операции возвращали `Failed to create a VPC Access connector`
- при этом после неудачных попыток оставались скрытые service subnets и начинали конфликтовать ranges

Итоговое рабочее решение для live API:

- не connector path
- а direct VPC egress через выделенную Cloud Run subnet `nv-horizon-run` (`10.20.0.0/26`)

Что подтверждено:

- live revision `nv-horizon-api-00076-2jz` работает с
  - `run.googleapis.com/network-interfaces: [{"network":"default","subnetwork":"nv-horizon-run"}]`
  - `run.googleapis.com/vpc-access-egress: private-ranges-only`
- Redis client на старте инициализируется
- после live lens-chat запроса по Saudi lens новых `redis: connection pool ... i/o timeout` логов на `00076` не появилось

Дальнейшая диагностика показала, что этого ещё недостаточно для полного Redis fix:

- на `nv-horizon-api-00078-tcq` BigQuery уже clean, но `redis: ... i/o timeout` всё ещё воспроизводится
- текущий Redis instance оказался в `DIRECT_PEERING`

Поэтому следующий фундаментальный шаг уже начат:

- в проекте включён `servicenetworking.googleapis.com`
- создан PSA range `google-managed-services-default`
- поднят private services access peering `servicenetworking-googleapis-com`
- начато создание нового Redis instance `nv-horizon-redis-psa` в `PRIVATE_SERVICE_ACCESS`
- `scripts/deploy` уже переведён на PSA Redis как целевой infra path

На момент этого обновления новый PSA Redis ещё в `CREATING`, поэтому финальное переключение API на него и повторная live-проверка Redis hot path остаются последним незавершённым блоком.

### Полезные live сущности

- API URL:
  - `https://nv-horizon-api-vosmg5vzoa-uc.a.run.app`
- serving revision at end of this session:
  - `nv-horizon-api-00076-2jz`
- Saudi scratch lens:
  - `Kdn5G1AR4rfCkLwvQTse`
- UAE + Saudi space lens:
  - `Ojkm20F5vbR7i5Nw3y1O`
- Saudi oil export lens:
  - `pbOu4sMhN1mCB4SbkwEw`
- Rich Gulf / logistics related lenses нужно дополнительно переотобрать live list’ом
- live Hormuz/Gulf source used in server ingest checks:
  - `kauQCSBHBhOhUXiKkhUk`

### Последняя незавершённая операция

Текущий незакрытый хвост уже не про ingest/landing, а про richer downstream usage:

1. дождаться/добить running/ready state у новых `watch_signal_landing_delta` warmups;
2. прогнать richer risk questions на awakened lenses;
3. отдельно разобрать stale projection rebuild и persistent warm-state `DeadlineExceeded`.

## Короткий вывод

Мы уже добились следующего:

- система перестала быть только factual substrate;
- появился реальный analytical substrate;
- появился отдельный signal landing plane;
- появился bounded delta refresh;
- появился staged warmup;
- появился честный missing-anchor слой;
- live deploy новой ревизии завершён;
- server ingest path починен;
- stale projection больше не блокирует реальные impacted lenses;
- selective warmup по live signal подтверждён.
- BigQuery dataset mismatch устранён в корне;
- live API переведён на выделенную direct-VPC subnet для Redis hot path;
- стартовые BigQuery/Redis шумы на новой ревизии исчезли.

Чего ещё не хватает до по-настоящему законченного продового контура:

- доказать live server path до product/domain lenses;
- сделать risk-answer path analytical-first;
- довести watch subject до полноценного focus-runtime;
- улучшить landing ranking через structural importance + semantic grounding.
