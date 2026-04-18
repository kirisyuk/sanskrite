# Elephant Alpha vs Frontier — и неожиданное открытие про природу Elephant

Run: **2026-04-18**, prompt: `bench_critic_models.py` (TSMC / ASML / CHIPS Act session pack, `prompt_chars=3629`), schema: `CriticVerdict v3` (strict).

Гипотеза: возможно, Elephant Alpha — не fast-tier (`mini`/`flash`/`haiku` класса, как мы решили в [elephant-vs-fast-flagships-2026-04](../elephant-vs-fast-flagships-2026-04/SUMMARY.md)), а frontier-class (`sonnet 4.6`, `opus 4.7`, `gemini 3.1 pro`, `gpt-5.4`, `glm 5.1`). Прогнали тот же strict-prompt против пяти flagship'ов + добавили variance-исследование Elephant.

## Главный вывод сразу

**Elephant Alpha — это routing alias поверх как минимум двух разных бэкендов.** Не одна модель.

Это объясняет всё: и calibration variance, и latency-разброс 15x на одном prompt'е, и стилистические переключения. Гипотеза «frontier vs fast» применима только к одному из бэкендов в каждый конкретный момент. **Ни один из бэкендов не достигает реального frontier-уровня** (см. ниже про опыт Opus 4.7 и Sonnet 4.6).

## Headline numbers — Frontier batch

| Модель | Latency | in_tok | out_tok | content_chars | composition | blk | nblk |
|---|---:|---:|---:|---:|---|---:|---:|
| `openrouter/elephant-alpha` | **2017ms** ⚡ | 1060 | 154 | 486 | go | 5 | 0 |
| `openai/gpt-5.4` | 7869ms | 1110 | 366 | 1892 | stop | 5 | 2 |
| `anthropic/claude-opus-4.7` | 24195ms | 2141 | 551 | 1254 | conditional | 3 | 3 |
| `google/gemini-3.1-pro-preview` | 24319ms | 992 | 1585 | 599 | go | 2 | 2 |
| `anthropic/claude-sonnet-4.6` | 34871ms | 1517 | 418 | 1662 | stop | 5 | 5 |
| `z-ai/glm-5.1` | 35784ms | 987 | 1156 | 948 | stop | 4 | 3 |

5 из 6 моделей сошлись на `sufficient: false` (sanity check), но **composition consensus сломался полностью**: `stop` ×3 (sonnet, gpt-5.4, glm), `conditional` ×1 (opus), `go` ×2 (elephant, gemini).

## Calibration consensus снижается на frontier — отдельный сильный сигнал

Сравнение с предыдущим батчем (fast flagships):

| Tier | Кол-во моделей | Composition consensus |
|---|---:|---|
| Fast (mini/flash/haiku/grok-fast) | 5 | **5/5 conditional** |
| Frontier (sonnet/opus/gemini-pro/gpt-5.4/glm-5.1) | 5 | **3 stop / 1 conditional / 1 go** |

**Контр-интуитивное наблюдение**: чем сильнее модель, тем менее она сходится с другими сильными моделями в judgement. Fast-flagship'и осторожничают «conditional» (безопасная середина). Frontier'ы имеют **более выраженные мнения** и расходятся.

Это — публикуемый результат сам по себе. Стандартный нарратив «масштаб → консенсус → правда» здесь работает наоборот: масштаб даёт **полярность мнений**, не консенсус. Для multi-agent системы это значит: выбор frontier-критика → нужно учитывать, что он будет более оппозиционным к решениям других агентов.

## Что только frontier нашёл (чего не достигли fast tier и Elephant)

| Находка | Кто увидел |
|---|---|
| **ZEISS как ASML sub-supplier** (EUV optics monopoly) | только `claude-opus-4.7` |
| **Wassenaar arrangement vs BIS/EAR** (regulatory regime granularity) | только `claude-opus-4.7` |
| **References to specific graph node IDs** (n10, n11, n12) | только `claude-sonnet-4.6` |
| **CHIPS Act subsidies → Intel/Samsung competitive positioning** | gpt-5.4, glm-5.1 (extended) |
| **Дисциплинированные numbered enumeration mission_hints** | sonnet-4.6, opus-4.7, gpt-5.4 |

Ни Elephant, ни fast tier этого уровня не достигают. Особенно показательно про ZEISS — это второго порядка детализация (sub-supplier sub-supplier'а), которая показывает реальную глубину знания цепочки.

`anthropic/claude-opus-4.7` в этой задаче — **best in batch по индустриальной зрелости**, несмотря на средний latency и не-самое-высокое количество blockers. Качество над количеством.

`anthropic/claude-sonnet-4.6` — **best по operational discipline**: ссылается на конкретные node ID из графа, использует numbered enumeration (1-2-3-4-5), audit-trail структура mission_hint.

## Variance discovery — Elephant Alpha не одна модель

После того как заметили, что Elephant дал **разный verdict** в двух соседних батчах (`conditional` vs `go`), запустили 5 изолированных прогонов на том же prompt'е. Результат:

| Run | Latency | composition | blk | nblk | стиль blockers |
|---|---:|---|---:|---:|---|
| variance_1 | 2759ms | stop | 5 | 2 | snake_case |
| **variance_2** | **30270ms** ⚠️ | stop | 5 | 0 | prose |
| variance_3 | 3190ms | stop | 6 | 5 | prose |
| variance_4 | 2233ms | stop | 3 | 2 | prose |
| variance_5 | 2063ms | go | 5 | 0 | snake_case |
| (vs fast tier) | 2531ms | conditional | 5 | 2 | prose |
| (vs frontier) | 2017ms | go | 5 | 0 | snake_case |

**Latency: range 2.0s → 30.3s (factor 15x).**
**Composition: stop ×5, conditional ×1, go ×2 (out of 7).**
**Blockers: range 3 → 6.**
**Style: чётко бимодальный.**

### Two distinct backends (явно различимы)

**Mode A — `snake_case_terse`** (variance_1, variance_5, frontier-batch run):

```
blockers:
  - single_points_of_failure_not_explicitly_flagged
  - design_layer_coverage_missing
  - lithography_layer_coverage_missing
  - dependencies_TSMC_ASML_Samsung_Intel_SMIC_unquantified
  - Japanese_Dutch_vendor_dependency_details_missing
```

- ~150-210 out_tok
- 2.0-2.7s latency (стабильно быстро)
- Часто `go` или короткий `stop`
- Mission_hint — одна короткая фраза
- Нет конкретных entities

**Mode B — `prose_verbose`** (variance_2/3/4, fast-tier batch):

```
blockers:
  - Missing explicit dependencies between TSMC, ASML, Samsung, Intel,
    SMIC and Japanese/Dutch equipment vendors in design and lithography layers
  - No single points of failure identified for design tools/IP and
    lithography delivery/service
  ...
```

- ~210-285 out_tok
- 2.2-30.3s latency (variable, иногда крайне медленно)
- Чаще `stop` или `conditional`
- Mission_hint — детальный, иногда с методологическим осознанием
- Конкретные имена компаний, технологий, регуляций

**Это не single model with high temperature variance.** Это **routing alias over a pool**. Стилистическая дистанция между Mode A и Mode B (snake_case identifiers vs full prose с именами) слишком велика для temperature-эффекта одной модели — temperature меняет выбор слов в рамках одного стиля, а не переключает между двумя структурно разными форматами output'а.

### Что это значит про Elephant

1. **Provenance остаётся unknown** — теперь даже сильнее. Это не «модель X», это «эксперимент платформы OpenRouter по A/B-тестированию двух кандидатов под одним alias».
2. **Frontier-tier гипотеза опровергнута** — даже в Mode B (более развёрнутом) Elephant не достигает уровня детализации Opus 4.7 или Sonnet 4.6. Нет ZEISS, нет Wassenaar/BIS distinction, нет ссылок на graph node IDs.
3. **Fast-tier гипотеза частично подтверждена** — Mode B стилистически похож на claude-haiku-4.5 / gpt-5.4-mini. Mode A похож на gpt-5.4-nano или меньшую модель.
4. **Production usage противопоказан** — критик, который на одном и том же prompt'е даёт `stop`, `conditional`, и `go` в разных вызовах, нельзя использовать как deterministic gate в pipeline. Variance в judgement слишком высока.
5. **Speed champion статус сохраняется** — даже Mode B fast часто (2.2s), Mode A почти всегда (2.0-2.7s). Variance_2 (30.3s) — outlier на ~14% случаев.

## Ответ на исходный вопрос

> «А что если elephant alpha — не класса mini/flash/haiku/grok-fast? Что если он смотрит ближе к sonnet 4.6, opus 4.7, gemini 3.1 pro, gpt 5.4? или glm 5.1?»

**Нет.** Elephant Alpha **не frontier-класса**. Контентная глубина не дотягивает: ни один из бэкендов не воспроизводит уровень детализации Opus 4.7 (ZEISS optics, Wassenaar regulatory layer) или Sonnet 4.6 (graph node citations).

**Но и не «просто mini-класс».** Elephant — это **multi-backend routing alias**, где как минимум один бэкенд выглядит как fast-flagship (haiku/gpt-5.4-mini-уровня по стилю), а второй — заметно более лёгкий и terse. Какой бэкенд срабатывает — зависит от условий вызова (нагрузка? хеш запроса? рандом?), мы не знаем.

**Самый интересный вопрос — не «какого он класса», а «зачем платформа делает A/B-тест под одним именем».** Возможные мотивации (спекулятивно):
- сравнение двух претендентов на пост постоянной модели OR
- сбор human preference data для RLHF / preference modeling (через user feedback на OR)
- стресс-тест двух разных inference стеков под одним брендом
- честный staged rollout новой версии: Mode A — старая, Mode B — новая, маршрутизация постепенно сдвигается

В любом случае: **production-grade использование Elephant — bad idea**, поскольку контракт «один alias → один behavior» не выполняется.

## Обновление выводов от прошлого батча

Прошлый отчёт ([elephant-vs-fast-flagships-2026-04](../elephant-vs-fast-flagships-2026-04/SUMMARY.md)) сделал вывод «Elephant попадает в quality-кластер `gpt-5.4-mini` и `claude-haiku-4.5`». **Это было верно для Mode B**, но мы поймали один-единственный snapshot Mode B и проинтерпретировали его как «характеристику модели». На самом деле — это характеристика **одного из двух бэкендов**, причём не доминирующего (в variance-серии Mode B встретился 3 раза из 5, Mode A — 2 раза, плюс наши прежние 2 батча: 1× Mode B и 1× Mode A).

Прошлый отчёт стоит читать с поправкой: «**Mode B** Elephant'а сравним с claude-haiku-4.5, **Mode A** заметно проще». Прошлая аналитика про methodological awareness (`session pack constraints prevent web search`, `no risk rating`) — это **только про Mode B**.

## Что отсюда тянется в site-отчёт

1. **Headline для отдельной страницы про Elephant**: «Stealth model on OpenRouter is actually a routing alias over multiple backends. Don't trust single-shot evaluations of stealth aliases.» — это тип статьи, который никто не публикует.
2. **Variance metric** как отдельный раздел: для критика production-readiness ≡ behavior consistency across replicates на одном входе. У Elephant эта метрика **проваливается**.
3. **Calibration polarity at scale** — frontier модели расходятся в judgement сильнее, чем fast tier. Контр-интуитивно. Достойно отдельной статьи.
4. **Industrial depth ladder**: ZEISS-уровень детализации (Opus 4.7) → graph node citations (Sonnet 4.6) → CHIPS Act + SMIC pathways (gpt-5.4, glm-5.1) → general dependencies (fast tier + Elephant Mode B) → snake_case identifiers (Elephant Mode A). **Лестница сравнения по содержательной глубине** — наш собственный stratified test.
5. **Methodology lesson**: стандартный one-shot бенч обманывает на stealth моделях. Минимально нужно ≥5 replicates per (model × prompt) для сильных claim'ов. Это надо вшить в pipeline для permanent benchmark.

## Что НЕ сделано

- Не прогнали Mode A vs Mode B на разных prompt'ах — variance_test был только на semiconductor prompt. Возможно, на других prompts mode-распределение другое.
- Не измерили вероятность каждой моды (для этого нужны десятки прогонов, не 5-7).
- Не проверили, корректно ли OR API возвращает request_id / provider_id, который мог бы пролить свет на routing (некоторые провайдеры через `provider` headers сообщают backend identity).
- Не сравнили Mode A vs Mode B напрямую по semantic similarity (можно было бы embeddings посчитать).

## Рекомендация для permanent benchmark

Из этого опыта вытекает методологическое правило для портала:

> **Stealth/anonymous models должны прогоняться минимум 5 раз per (prompt × model)**, и median + variance публикуются отдельно. Single-shot оценки stealth моделей помечаются явно «n=1, provenance unknown, treat as preview».

Для production моделей с известным provenance n=1 допустимо для headline-сравнения, но calibration variance всё равно стоит периодически проверять (раз в квартал, например).

## Воспроизводимость

Frontier batch:
```bash
.venv-agentloom/bin/python scripts/bench_critic_models.py \
  --jsonl-out /tmp/critic_bench_frontier_vs_elephant.jsonl \
  --max-output-tokens 8192 --timeout 600 \
  --models \
    openrouter/elephant-alpha \
    anthropic/claude-sonnet-4.6 \
    anthropic/claude-opus-4.7 \
    google/gemini-3.1-pro-preview \
    openai/gpt-5.4 \
    z-ai/glm-5.1
```

Variance series:
```bash
for i in 1 2 3 4 5; do
  .venv-agentloom/bin/python scripts/bench_critic_models.py \
    --jsonl-out /tmp/elephant_variance_$i.jsonl \
    --max-output-tokens 4096 --timeout 120 \
    --models openrouter/elephant-alpha
  sleep 3
done
```

Артефакты сохранены в этой директории.
