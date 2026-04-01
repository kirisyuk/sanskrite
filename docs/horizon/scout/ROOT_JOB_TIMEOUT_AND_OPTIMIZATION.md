# Scout+ Root Job Timeout And Optimization Notes

## Контекст

Этот документ фиксирует разбор реального кейса со Scout+:

- линза: `Kazakhstan SME Footwear Supply Chain and Global Trade Analysis`
- `lensId`: `NanZwGaNJD167mwwFHgW`
- корневой job: `HR1iICE8XtZLV0A8xXjJ`
- дата сбоя: `2026-04-01`

Цель документа:

- объяснить, почему линза не дошла до `assembly.ready`
- отделить причину сбоя от причин высокой latency
- зафиксировать, что именно в алгоритмах Scout+ стоит оптимизировать без потери качества

## Что произошло

Линза не прогрелась автоматически не потому, что не хватило Vertex, GCS или данных.

Корневой supply-chain job выполнил большую часть полезной работы, но не смог корректно завершиться. В результате:

- job остался в `running`
- линза не получила `assembly.ready`
- internal knowledge warmup не стартовал

## Непосредственная причина сбоя

У job истёк execution context на хвосте выполнения, а финализация шла на том же уже истёкшем `ctx`.

Механика была такой:

1. Cloud Tasks handler выдаёт scout execution timeout, по умолчанию `55m`.
2. Длинный root supply-chain job проживает почти весь этот бюджет.
3. На финализации вызывается `TransitionState(ctx, ...)`.
4. `TransitionState` начинает с `agentJobsRepo.Get(ctx, jobID)`.
5. Поскольку `ctx` уже умер, чтение job падает с `context deadline exceeded`.
6. Терминальное состояние не записывается.
7. On-complete hook не вызывается.
8. `assembly.ready` не публикуется.
9. Warmup не стартует.

Иными словами, это был не просто timeout, а timeout в сочетании с неустойчивой terminal finalization path.

## Что job делал всё это время

Job не простаивал. По runtime state видно, что время ушло на тяжёлый корневой domain supply-chain проход:

- `planning`: примерно `08:52:56 -> 09:20:12`
- `research`: примерно `08:52:56 -> 09:45:38`
- `materialization`: примерно `09:14:08 -> 09:46:19`
- далее хвост assembly/finalize, где и произошёл сбой

По факту job успел собрать большой граф:

- `140-160` discovered nodes
- `684-706` discovered edges
- `379` supply links
- `122` materialized paths

То есть основное время было потрачено на реальную graph/materialization работу, а не на warmup.

## Почему этот кейс важен

Этот сбой выявил две разные проблемы:

1. `Reliability problem`

- длинный root job может закончить полезную работу и всё равно не перейти в terminal state
- это блокирует `assembly.ready`
- это блокирует substrate warmup

2. `Algorithmic problem`

- root job делает слишком много работы в одном длинном проходе
- даже без бага такой job слишком близко подходит к execution timeout ceiling

Первая проблема уже была bug-level дефектом.
Вторая остаётся системной точкой роста.

## Что уже было исправлено

В коде внесены две защитные правки:

1. Финализация и terminal transitions теперь идут через durable context, а не через уже истёкший execution context.
2. Poison path после исчерпания retry теперь тоже вызывает on-complete lifecycle, чтобы `assembly.ready` и warmup не терялись.

Это лечит зависание линзы в `running` после фактического завершения работы.

## Что здесь можно оптимизировать по алгоритмам

Ниже не про урезание качества, а про уменьшение лишней стоимости и длительности root jobs.

### 1. Убрать монолитный root job

Сейчас broad supply-chain root job делает слишком много в одном проходе:

- planning
- research
- relation materialization
- supply chain materialization
- refine / assembly

Рекомендация:

- как только есть хороший первый materialized result, root job должен терминализоваться
- дальнейшее расширение должно идти continuation jobs
- continuation должен работать как controlled deepening, а не как один длинный request-bound execution

Ожидаемый эффект:

- меньше риск таймаутов
- меньше blast radius одного job
- выше предсказуемость `assembly.ready`

### 2. Валидировать edge policy после dedupe, а не на каждый raw edge

Сейчас policy validation запускается очень часто и поштучно по candidate edges.
На больших supply-chain jobs это даёт сотни почти одинаковых вызовов.

Рекомендация:

- ввести `normalized edge signature -> validation decision cache`
- валидировать после dedupe/normalization
- избегать повторной проверки одинаковых `sourceType + relationType + targetType + canonical path pattern`

Ожидаемый эффект:

- заметное снижение CPU и latency на materialization
- снижение policy-log noise
- без потери качества решений

### 3. Не писать тяжёлый `chainContext` blob обратно в job state

Сейчас в `agent_jobs` пишется тяжёлый JSON `chainContext`, который содержит большой graph context.
Для широких линз это дорогой write path и лишний риск дедлайнов на хвосте.

Рекомендация:

- в job state держать только summary
- heavy context складывать в отдельный artifact или GCS object
- в job state хранить `artifactKey`, `hash`, counters и compact refs

Ожидаемый эффект:

- меньше Firestore write cost
- меньше вероятность tail-timeout на persist
- проще continuation/retry semantics

### 4. Делать `fast-first` дефолтом для broad supply-chain roots

Сейчас система уже умеет fast-first / hot-path style execution, но не все broad root jobs используют его как основной режим.

Рекомендация:

- для широких supply-chain root jobs сначала строить минимально полезный skeleton graph
- затем выпускать continuation на graph completion
- не тащить весь breadth в один first pass

Ожидаемый эффект:

- быстрее first useful result
- раньше `assembly.ready`
- лучшая совместимость с warm-substrate architecture

### 5. Сделать retry smarter после частичной materialization

Сейчас если job уже успел materialize полезный supply-chain result, но упал на хвосте, retry может снова зайти слишком глубоко.

Рекомендация:

- если есть persisted `supplyChainMaterialization` и `pathArtifactKeys`, retry должен идти как recovery/finalize path
- не нужно повторять весь heavy pass
- нужен режим `materialized_but_not_terminal`

Ожидаемый эффект:

- меньше дублей работы
- меньше повторной нагрузки
- меньше вероятность зациклиться на почти готовом root job

### 6. Сильнее отделить answer-substrate readiness от long-tail graph completion

Для serving-архитектуры нам не всегда нужен максимально полный граф.
Нам нужен достаточно хороший и стабильный graph slice, после которого можно греть substrate.

Рекомендация:

- разрешить `assembly.ready` после quality threshold, а не только после исчерпания всего broad expansion
- long-tail expansion и enrich можно продолжать как background continuation

Ожидаемый эффект:

- раньше запускается warmup
- быстрее становится доступен internal answer plane
- меньше user-facing dependence on the slowest tail work

## Приоритет оптимизаций

Если делать по убыванию практической полезности:

1. `Continuation instead of one long root job`
2. `Edge policy validation cache + validation after dedupe`
3. `Move heavy chainContext out of agent_jobs state`
4. `Fast-first by default for broad supply-chain roots`
5. `Recovery mode for materialized-but-not-terminal jobs`
6. `Earlier assembly-ready threshold for answer-substrate use cases`

## Что не нужно делать

Чтобы ускорить систему, не нужно:

- ухудшать качество product/path semantics
- выкидывать materialized paths
- отключать policy validation целиком
- опускать качество graph substrate ради short-term latency

Правильный вектор:

- меньше лишней повторной и синхронной работы
- лучшее разделение `first useful result` и `long-tail completion`
- более дешёвый persist path
- более устойчивый terminal lifecycle

## Итог

Кейс с `Kazakhstan SME Footwear Supply Chain and Global Trade Analysis` показал не только bug в финализации, но и архитектурный предел текущего root-job execution pattern.

Главный вывод:

- Scout+ уже умеет строить богатый граф
- но broad supply-chain root jobs слишком монолитны
- их нужно дробить, кэшировать и checkpoint'ить
- тогда и стабильность, и скорость warm substrate будут заметно лучше без потери качества
