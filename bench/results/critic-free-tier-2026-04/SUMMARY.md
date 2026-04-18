# Critic Bench — OpenRouter free tier, semiconductor pack

Run: **2026-04-18**, prompt: `bench_critic_models.py` (TSMC / ASML / CHIPS Act session pack, `prompt_chars=3629`), schema: `CriticVerdict v3` (strict).

Это первый dry-run в духе документа [`docs/roadmap/site/insights-portal-vision.md`](../../../docs/roadmap/site/insights-portal-vision.md): берём готовый бенч-харнес и прогоняем по самому дешёвому слою рынка — бесплатным моделям OpenRouter — на той же индустриальной задаче, что и платные модели в апреле 2026.

## Состав

15 моделей из free-листа OR (исключены: Kimi K2.5 — отсутствует в free, Llama Nemotron Embed — embedding, не chat).

- **Strict run**: `critic_bench_free_strict.jsonl` + retry того же набора в `critic_bench_free_strict_retry.jsonl`
- **Lenient run** (для моделей-неудачников по schema): `critic_bench_free_lenient.jsonl`

## Headline

**Из 15 free-моделей под strict-JSON contract на индустриальной задаче работают 6.** Победитель — не самый большой, а самый дисциплинированный:

| # | Модель | Latency | out tokens | blockers | composition | примечание |
|---|---|---:|---:|---:|---|---|
| 1 | `openrouter/elephant-alpha` | **2.3s** ⚡ | 252 | 5 | stop | best speed × quality |
| 2 | `nvidia/nemotron-3-nano-30b-a3b:free` | 3.5s | 961 | 3 | **go** ⚠️ | calibration miss |
| 3 | `arcee-ai/trinity-large-preview:free` | 5.3s | 159 | 5 | stop | concise, decisive |
| 4 | `nvidia/nemotron-nano-12b-v2-vl:free` | 14.4s | 733 | 2 | stop | под-бенчмарк, но честный |
| 5 | `nvidia/nemotron-3-super-120b-a12b:free` | 26.7s | 943 | 4 | conditional | size ≠ speed |
| 6 | `nvidia/nemotron-nano-9b-v2:free` | 28.0s | 798 | 2 | stop | странно медленный для 9B |

**Cost: $0** (free tier, 15 attempted calls + 7 retries).

## Что не работает на free-tier — отдельный сигнал

Это **сама по себе метрика** для отчёта: не «модель плоха», а «модель недоступна для практической работы на free». Класс отказа важнее самого факта отказа.

### Класс 1 — модель ломает контракт (model-level fault, 2/15)

| Модель | Что произошло |
|---|---|
| `google/gemma-4-31b-it:free` | OR заявляет `structured_outputs=true`, по факту модель **придумала свою схему** (`verdict / reasoning / missingInformation / suggestedAction` вместо `sufficient / blockers / nonBlockers / compositionRecommendation`). Это не «не понял задачу», это **игнорирование response_format на стороне Google AI Studio**. |
| `z-ai/glm-4.5-air:free` | Под strict выдала JSON в markdown-fences (` ```json ... ``` `). Под lenient ушла в режим thinking, **285 секунд** генерации, truncated на 4096 output_tokens — schema так и не дозрела. **Verbose-thinking сжигает контракт.** |

Обе модели — кейсы для отчёта в стиле «как provider может маркетингово заявлять structured outputs, а на практике не enforce'ить».

### Класс 2 — провайдер недоступен (infra-level fault, 7/15)

| Провайдер | Модели | Симптом | Повторяемость |
|---|---|---|---|
| **Venice** | `qwen3-next-80b-a3b`, `qwen3-coder`, `llama-3.3-70b` | `429 — temporarily rate-limited upstream` | устойчиво при двух попытках с интервалом 75с |
| **OpenInference** | `minimax-m2.5`, `gpt-oss-120b`, `gpt-oss-20b` | `503 — no healthy upstream` | устойчиво |
| **Google AI Studio** | `gemma-4-26b-a4b` | `429 — rate-limited upstream` | устойчиво |

OR честно проксирует ошибку и предлагает «add your own key». Для нашей цели это значит: **на free-tier даже однократный вызов этих моделей не гарантирован**. Llama 3.3 70B и OpenAI gpt-oss — самые громкие имена в free-листе и оба недоступны через дефолтных провайдеров. **Это публикуемый факт.**

## Семантические наблюдения по 6 успешным

Все 6 правильно сказали `sufficient=false` — это базовый sanity check (pack действительно неполный, должен быть отказ). Но в **списке blockers** уже видна сильная дифференциация:

### Что нашли все 6
- ASML EUV monopoly как SPOF — упомянуто в большинстве (как blocker или nonBlocker).
- Отсутствие design layer (EDA / IP) — нашли все.
- Японские поставщики (Tokyo Electron / JSR / Shin-Etsu) — нашли все.

### Что нашли только лучшие
- **CHIPS Act dimension** — нашли только `nemotron-3-super-120b` и `elephant-alpha`. Остальные **проглотили эту часть user question целиком**, что для критика — серьёзный faux pas (юзер прямо спросил про CHIPS Act pressure).
- **Packaging / test dependencies** — нашёл только `elephant-alpha` (первое упоминание advanced packaging как отдельного слоя).
- **EDA / IP под export controls** (не просто «отсутствует EDA», а именно «EDA под restrictions») — нашёл только `elephant-alpha`.
- **Intel foundry positioning vs TSMC/Samsung** — нашёл только `arcee-trinity`.

`elephant-alpha` (OR-собственная модель, 100B) — **по содержательной глубине лучший в выборке**. И при 2.3 секунды.

### Calibration anomaly
- `nvidia/nemotron-3-nano-30b-a3b:free` сказал `compositionRecommendation: "go"` при `sufficient: false` и **3 blockers**. Это **семантическая инверсия**: «недостаточно, есть 3 блокера, но давайте уже синтезировать». Все остальные при тех же условиях сказали `stop` или `conditional`. Маленький Nemotron слишком эyfger — **opasный паттерн для критика в production**.

### Verbose-vs-decisive trade-off
- `arcee-trinity-large-preview` — 159 output tokens, 5 blockers (high decisiveness).
- `nemotron-3-super-120b` — 943 output tokens, 4 blockers (verbose внутреннего reasoning, который не доходит до контента).
- `elephant-alpha` — 252 tokens, 5 blockers + 2 nonBlockers + длинный mission_hint (балансное).

Token efficiency у Trinity и Elephant — заметно лучше, чем у больших Nemotron'ов.

## Что отсюда тянется в site-отчёт v0

1. **Headline** для страницы: «6 of 15 free OpenRouter models can hold a strict JSON contract on a real industrial reasoning task. The winner is the smallest one fast enough to run twice».
2. **Trade-off chart** (3 оси): latency vs output_tokens vs blocker count. Лидеры на разных осях разные — это и есть месседж.
3. **Failure typology table** — два чётко разделённых класса (model-level vs infra-level). Это уникальный угол: никто публично не разделяет «провайдер лежит» от «модель не следует контракту».
4. **Calibration sidebar** — single-anomaly story про nemotron-3-nano-30b «go» при `sufficient=false`. Микро-кейс, мощный аргумент за бенчинг calibration отдельно (см. `insights-portal-vision.md`, §4.1 calibration).
5. **Что мы НЕ сделали в v0** (обязательно зафиксировать):
   - Один pack, не пять — пока нет статистической значимости.
   - Нет golden set — оценка quality пока relatively (друг против друга), не absolute.
   - Не покрыты role'ы explorer / canonicalizer / supervisor — только critic.
   - Не покрыта vertical industrial probe suite (semiconductor literacy).

## Сравнение с платным апрельским прогоном

Из `agents/vertex/agenthorde/README.md`, апрельский paid-bench (тот же pack):
- `openai/o4-mini`: 6.2s, 4 blockers, $0.0026
- `deepseek/deepseek-chat-v3.1`: 8.0s, 3 blockers, $0.0004

Free-tier ландшафт:
- `openrouter/elephant-alpha`: **2.3s, 5 blockers, $0** — **быстрее и содержательнее** o4-mini на этой задаче. Если elephant-alpha сохраняет качество на более широких packs (надо проверять), это серьёзный production-кандидат.
- `nemotron-3-super-120b:free`: 26.7s, 4 blockers, $0 — paid-версия в апреле была 60s+. Похоже, free-tier на NVIDIA Nemotron работает быстрее (или мы попали в окно низкой нагрузки).

## Следующие шаги (предложение)

1. ✅ **Артефакты сохранены** в `bench/results/critic-free-tier-2026-04/` (raw JSONL + stdout логи + этот summary).
2. **Прогнать тот же strict-prompt на платных flagship-моделях** для прямого сравнения: gpt-5.4 (medium reasoning), claude-opus-4.7, gemini-3.1-pro, deepseek-chat-v3.1, plus текущий champion o4-mini как baseline. Это даст полную верхнюю границу для одной шкалы (quality on this task).
3. **Расширить на 2 дополнительных pack** (lithium, malacca strait — fixtures есть) — иначе квалификация любых выводов про elephant-alpha vs Nemotron преждевременна.
4. После этого — собрать первую черновую страницу `site/insights/critic-bench-2026-q2.html` руками по шаблону из vision-документа (TL;DR + trade-off chart + table + failure typology + interpretation).

## Воспроизводимость

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex/agenthorde
set -a; source /Users/kirisyuk/cursor/sanskrite/.env; set +a
.venv-agentloom/bin/python scripts/bench_critic_models.py \
  --jsonl-out /tmp/critic_bench_free.jsonl \
  --max-output-tokens 4096 \
  --timeout 300 \
  --models \
    nvidia/nemotron-3-super-120b-a12b:free \
    openrouter/elephant-alpha \
    arcee-ai/trinity-large-preview:free \
    minimax/minimax-m2.5:free \
    qwen/qwen3-next-80b-a3b-instruct:free \
    nvidia/nemotron-nano-9b-v2:free \
    google/gemma-4-31b-it:free \
    google/gemma-4-26b-a4b-it:free \
    z-ai/glm-4.5-air:free \
    openai/gpt-oss-120b:free \
    openai/gpt-oss-20b:free \
    qwen/qwen3-coder:free \
    meta-llama/llama-3.3-70b-instruct:free \
    nvidia/nemotron-3-nano-30b-a3b:free \
    nvidia/nemotron-nano-12b-v2-vl:free
```

Ожидаемый разброс: latency ±30%, состав успешных моделей нестабилен между прогонами из-за infra-faults у Venice / OpenInference / Google AI Studio.
