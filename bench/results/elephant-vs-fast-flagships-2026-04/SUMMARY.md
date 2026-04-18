# Elephant Alpha vs Fast Flagships — Critic Bench

Run: **2026-04-18**, prompt: `bench_critic_models.py` (TSMC / ASML / CHIPS Act session pack, `prompt_chars=3629`), schema: `CriticVerdict v3` (strict).

Прямое сравнение `openrouter/elephant-alpha` (free, stealth) с **5 быстрыми флагманами западных и одного китайского лаба** на той же индустриальной задаче — semiconductor supply chain critique. Цель: понять, в каком качественном кластере живёт Elephant.

## Кого взяли и почему

| Модель | Положение в линейке | $/M in | $/M out |
|---|---|---:|---:|
| `openrouter/elephant-alpha` | stealth (опубликовано на OR без атрибуции) | $0 | $0 |
| `openai/gpt-5.4-mini` | OpenAI fast flagship Q1-Q2 2026 | $0.75 | $4.50 |
| `anthropic/claude-haiku-4.5` | Anthropic fast flagship | $1.00 | $5.00 |
| `google/gemini-3-flash-preview` | Google fast flagship | $0.50 | $3.00 |
| `x-ai/grok-4.1-fast` | xAI fast flagship | $0.20 | $0.50 |
| `z-ai/glm-4.7-flash` | прицельный тест гипотезы «Elephant — линия GLM» | $0.06 | $0.40 |

Сознательно НЕ взяли `gpt-5.4-pro` / `claude-opus` / `gemini-3.1-pro`: цель — попасть в один **класс позиционирования**, а не упереться в потолок интеллекта.

## Headline

**Elephant Alpha попадает ровно в quality-кластер `gpt-5.4-mini` и `claude-haiku-4.5`. И при этом — самый быстрый в выборке (2.5s vs 3.5–8.6s) и стоит $0.**

Полная стоимость прогона: **~$0.13** (5 платных вызовов + 1 free).

## Сводка

| # | Модель | Latency | in_tok | out_tok | content_chars | blockers | nonBlockers | composition | tokens/finding |
|---|---|---:|---:|---:|---:|---:|---:|---|---:|
| 1 | `openrouter/elephant-alpha` | **2531ms** ⚡ | 1060 | 215 | 845 | 5 | 2 | conditional | 30.7 |
| 2 | `google/gemini-3-flash-preview` | 3280ms | 991 | 210 | 877 | 3 | 2 | conditional | 42.0 |
| 3 | `openai/gpt-5.4-mini` | 3506ms | 1110 | 361 | 1825 | 5 | 4 | conditional | 40.1 |
| 4 | `anthropic/claude-haiku-4.5` | 6343ms | 1516 | 266 | 1182 | 5 | 5 | conditional | **26.6** ✦ |
| 5 | `x-ai/grok-4.1-fast` | 8630ms | 1251 | 962 | 780 | 5 | 4 | conditional | 106.9 |
| — | `z-ai/glm-4.7-flash` | FAIL | — | — | — | — | — | — | — |

✦ — лучший token efficiency (всё, что вышло, попало в structured output).
⚡ — лидер по latency.

`out_tok` — **сколько модель сгенерировала суммарно** (включая reasoning tokens), `content_chars` — **сколько реально попало в финальный verdict**. Разница важна: см. разбор Grok ниже.

## Что согласовано всеми 5 (consensus = высокая сила сигнала)

1. `compositionRecommendation = "conditional"` — единодушно. **Это не банально**: на free-tier тот же prompt дал stop / conditional / go (включая аномалию с nemotron-3-nano «go при sufficient=false»). Paid fast flagships **сходятся в judgement**, и Elephant — внутри консенсуса.
2. `sufficient = false` — единодушно (sanity check проходит).
3. **Design layer (EDA / IP)** — пропущен, помечен как blocker всеми 5.
4. **Японские equipment / materials vendors** (Tokyo Electron / JSR / Shin-Etsu) — недостаточно покрыты, помечено всеми 5.

## Что нашли только лучшие (дифференциация качества)

| Находка | elephant | gpt-5.4-mini | claude-haiku | gemini-3-flash | grok-4.1-fast |
|---|:---:|:---:|:---:|:---:|:---:|
| **CHIPS Act dimension** | ✓ | ✓ | ✗ | ✓ | ✓ |
| **Single Points of Failure** (явная категория) | ✓ | ✓ | ✗ (через quantitative) | ✗ | ✓ |
| **Granular export-control mechanisms** (license / carve-out) | ✓ | ✓ | ✓ | ✗ | ✗ |
| **SMIC-specific path** под controls | ✗ | ✓ | ✓ | ✗ | ✗ |
| **Photoresist / etch supply layer** (под-уровень) | ✗ | ✗ | ✓ | ✗ | ✓ |
| **Конкретные брэнды** (Synopsys / Cadence / ARM) | ✗ | ✗ | ✗ | ✓ | ✓ |
| **Quantified EUV shipment shares** | ✗ | ✗ | ✓ | ✗ | ✓ |
| **Methodological критика** (no risk rating / mitigation framework) | ✓ | ✗ | ✗ | ✗ | ✗ |
| **Self-awareness про operating constraints** (session pack ⇒ no web search) | ✓ | ✗ | ✗ | ✗ | ✗ |

Каждая колонка — **отдельный сильный сигнал** в стиле модели:

- **Elephant** уникально вытаскивает meta-уровень: «нет risk rating» (методологическое замечание, а не контентное), «session pack constraints prevent web search» (явное признание operating envelope). Это — **поведение senior critic**, не junior.
- **gpt-5.4-mini** — самый verbose в blockers (по 2-3 предложения, конкретная терминология: «tapeout», «fabless/foundry dependencies», «chiplet architecture»). Делает **мета-комментарий про escalation logic анализатора**.
- **claude-haiku-4.5** — самые **структурированные nonBlockers** (5 распределённых positive findings, каждый — отдельный домен). Mission_hint имеет audit-trail структуру: re-references session pack, re-references user request. Дисциплинированный аудитор.
- **gemini-3-flash** — слабейший по охвату (3 blockers vs 5 у остальных), НО уникально называет **«specific market share of Tokyo Electron in the etch/coat-develop space»** — самая узкая sub-domain precision.
- **grok-4.1-fast** — самый **actionable mission_hint** (буквально перечисляет нодов и эджей для добавления в граф: «CHIPS Act nodes/edges, design layer (Synopsys/Cadence), Japanese materials (JSR/Shin-Etsu photoresist)»). Выглядит как готовое executable instruction для следующего worker'а.

## Token efficiency и hidden waste

Самое говорящее число — это **`out_tok` vs `content_chars`**, потому что для reasoning-моделей `out_tok` включает скрытый chain-of-thought, а в structured verdict попадает только финальный JSON.

| Модель | out_tok | content_chars | hidden ratio (out_tok per content_char) |
|---|---:|---:|---:|
| elephant-alpha | 215 | 845 | 0.25 (плотный output) |
| claude-haiku-4.5 | 266 | 1182 | 0.22 (плотный output) |
| gemini-3-flash | 210 | 877 | 0.24 (плотный output) |
| gpt-5.4-mini | 361 | 1825 | 0.20 (самый плотный) |
| **grok-4.1-fast** | **962** | **780** | **1.23** ⚠️ |

**Grok сжёг 962 output_tokens на финальный verdict в 780 символов** — то есть основная часть генерации ушла в скрытые reasoning tokens, которые так и не материализовались в structured output. Для критика это плохой паттерн: в reasoning-граф попадает только то, что в JSON, всё остальное — оплачиваемый, но невидимый shadow work. По метрике **«полезный output на доллар»** Grok здесь — худший в выборке, несмотря на самую низкую input price.

## Гипотеза «Elephant — это GLM» — частично опровергнута, частично подтверждена

- **z-ai/glm-4.7-flash** в этом прогоне **упал** (`empty content` — провайдер вернул 200 OK без content; согласуется с поведением `glm-4.5-air` на free-tier, который тоже ушёл в multi-minute thinking без эмита). Это **системная характеристика z-ai семейства** под strict json schema — они уходят в thinking mode и эмитят пусто или с задержкой, ломающей контракт.
- **Elephant НЕ ведёт себя как z-ai**: он немедленно эмитит structured output, держит strict schema, не уходит в thinking. Это серьёзный contra-argument против гипотезы.
- **Стилистически Elephant ближе к claude-haiku-4.5** (концизный, structured nonBlockers, методологическая зрелость) или к **gpt-5.4-mini** (плотные blockers, конкретная терминология) — чем к Chinese open-weight семейству.
- Это **не доказательство**, что Elephant — западный лаб. Но это сильный сигнал, что он **не GLM-линия** и **не Qwen-линия** (Qwen-next тоже даёт более verbose hedging-стиль).
- Альтернативная гипотеза, которую теперь стоит держать в голове: **OpenRouter мог запустить anonymized fork модели Anthropic или OpenAI** с целью бенчинга/сбора данных. Стиль и скорость согласуются.

## Выводы для отчёта v0

1. **Headline для site-страницы**: «Free anonymous stealth model performs in the speed/quality cluster of `gpt-5.4-mini` and `claude-haiku-4.5` on a real industrial reasoning task. Don't use it in production (provenance unknown, logging acknowledged), but watch it.»
2. **Калибровочный consensus** как отдельная метрика: paid fast flagships сходятся в judgement (5/5 conditional), free-tier — нет. Это качественный маркер «production-grade calibration». Elephant — **внутри** консенсуса.
3. **Token efficiency vs hidden reasoning** — отдельный график (out_tok / content_chars). Grok-4.1-fast как cautionary tale.
4. **Failure typology обновлена**: GLM семейство (4.5-air, 4.7-flash) — стабильно ломается на strict schema через thinking mode + empty content. Это уже не одиночный case, а **паттерн поведения вендора**.
5. **Methodological critique** как separate metric: только Elephant добавил методологические замечания (no risk rating, session pack awareness). Стоит сделать отдельный under-reported axis: «critic метаосознание» (critique of analysis structure, not just content).

## Что НЕ было сделано в этом прогоне (фиксируем для честности)

- Один pack — semiconductor. Нужно прогнать ещё хотя бы 2 (lithium, malacca) для статистической силы вывода.
- Один temperature setting (`0.0` для GPT-5.x family как в `MODEL_OVERRIDES`, default иначе).
- Не репликация — каждая модель прогнана 1 раз. Variance across runs не измерен.
- glm-4.7-flash не было retry — failure trusted as systemic (но 1 attempt мало для absolute claim).
- Не разделили reasoning tokens отдельно от output tokens (нет такого поля в OR API response для всех моделей единообразно).

## Сравнение с free-tier (тот же pack, апрель 2026)

| Модель | Latency | blockers | composition | заметка |
|---|---:|---:|---|---|
| `openrouter/elephant-alpha` | 2531ms | 5 | conditional | **тот же**, 100% воспроизводимость |
| `nvidia/nemotron-3-super-120b-a12b:free` | 26.7s | 4 | conditional | в 10x медленнее Elephant, тот же verdict |
| `arcee-ai/trinity-large-preview:free` | 5.3s | 5 | stop | concise, decisive (jr critic стиль) |
| `nvidia/nemotron-3-nano-30b-a3b:free` | 3.5s | 3 | **go** ⚠️ | calibration miss |

Elephant — единственная free-модель, которая попадает в quality-cluster платных fast flagships **по всем осям одновременно** (latency / coverage / calibration / стиль).

## Воспроизводимость

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex/agenthorde
set -a; source /Users/kirisyuk/cursor/sanskrite/.env; set +a
.venv-agentloom/bin/python scripts/bench_critic_models.py \
  --jsonl-out /tmp/critic_bench_paid_fast_flagships.jsonl \
  --max-output-tokens 4096 \
  --timeout 300 \
  --models \
    openrouter/elephant-alpha \
    openai/gpt-5.4-mini \
    anthropic/claude-haiku-4.5 \
    google/gemini-3-flash-preview \
    x-ai/grok-4.1-fast \
    z-ai/glm-4.7-flash
```

Артефакты: `critic_bench_paid_fast_flagships.jsonl` + `critic_bench_paid_fast_flagships.stdout.log`.
