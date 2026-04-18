# Horde.Vision Insights Portal — видение

Статус: vision / pre-scope. Не план реализации — а fix позиционирования и формы.
Адресат: команда сайта (`site/`), команда AgentHorde (`agents/`), команда платформы (`backend/internal/ai/`).

## TL;DR

Превратить статический маркетинговый сайт `site/` в **аналитический портал**, у которого есть собственное содержательное ядро — не блог и не «press releases», а **серия воспроизводимых индустриальных исследований**, выпускаемых под одним зонтиком:

> Не существует «лучшей» LLM. Есть лучшая модель **для роли** в мульти-агентной системе, и эта роль **зависит от вертикали**.

Каждый отчёт — доказательство этого утверждения на одной вертикали (semiconductor / energy / maritime / lithium / REE) и одной роли (explorer / critic / canonicalizer / supervisor). Прогон, оценка, golden-set и интерпретация выполняются нашей собственной mesh-инфраструктурой; интерпретация генерируется самой сильной из доступных моделей по фиксированному prompt-шаблону.

Это **первый публичный role-fit × industrial-vertical бенч моделей**. Аналогов сейчас нет.

---

## 1. Контекст и наблюдения

### 1.1 Что у нас уже есть (артефакты для бенча, без новой инфры)

- `agents/vertex/agenthorde/scripts/bench_critic_models.py` — рабочий one-shot bench: один pack, N моделей через OpenRouter, strict JSON Schema (`CriticVerdict`), JSONL-вывод, метрики latency / tokens / schema_pass / blockers / composition.
- `agents/shared/agenthorde_common/agenthorde_common/critic/ab_log.py` — формализованная схема `CriticAttemptLog` (cycle, provider, model, ok, latency, verdict_sufficient, composition, blockers_count). Уже работает как row-формат.
- `agents/shared/agenthorde_common/agenthorde_common/critic/critic_chain.py` + `openrouter_client.py` + `strict_schema.py` — переиспользуемое ядро вызова OR.
- `agents/vertex/agenthorde/agenthorde/critic/fallback.py` — orchestrator A/B-сравнения (Vertex Gemini + N OR-моделей на одном запросе) с per-attempt JSONL.
- Зафиксированный аналитический вывод в `agents/vertex/agenthorde/README.md` (раздел «Критик OpenRouter fallback», апрель 2026): порядок моделей, цена, latency, schema-fidelity. Это и есть прото-отчёт #1.
- Fixtures под packs: `agents/vertex/agenthorde/fixtures/` (semiconductor, malacca strait, russian crude, lithium и др.).
- `backend/internal/ai/model_manager.go` — централизованный реестр моделей с провайдерами, slugs, fallback-логикой. В будущем — единая точка маршрутизации бенч-прогонов.
- Сайт `site/` — статический трёхязычный (EN/RU/KZ) Firebase Hosting лендинг. Существующие подстраницы (`platform.html`, `agent-mesh.html`, `technical-disclosure.html`) задают стилистику и сетку.

### 1.2 Что про оценку моделей сейчас фактически говорят фронтирные лабы

В публикациях / system cards Anthropic (Claude Opus 4.6/4.7, Constitutional AI), OpenAI (GPT-5 spec, Model Spec), Google DeepMind (Gemini 3, Med-PaLM continuation) системно проступают пять направлений, **которые не покрыты** Artificial Analysis / LMSYS / Vellum:

1. **Faithfulness CoT и calibration** — соответствует ли rationale финалу; калиброваны ли выраженные степени уверенности.
2. **Specification compliance под давлением** — держится ли strict-schema через многоходовые сценарии и попытки дрейфа.
3. **Tool calibration** — знает ли модель, **когда не вызывать** инструмент.
4. **Refusal asymmetry / cooperative honesty** — отказ в простом vs согласие со сложно-опасным; fabrication of citations.
5. **Temporal calibration** — знает ли модель границы своего знания, не присваивает ли пост-cutoff фактам уверенность.

И **главная** недо-измеренная область — **мультиагентное поведение**: handoff fidelity, disagreement productivity, role-fit, self-vs-other criticism bias. Public benches one-shot — мы in-system.

### 1.3 Наш позиционный зазор

Мы можем занять **три ниши одновременно**, и каждая — белое пятно:

- **Role-fit benchmarking** (модель × роль).
- **Industrial vertical benchmarking** (модель × вертикаль).
- **Multi-agent dynamics** (модель × модель в цепочке).

Любая одна из трёх — уже differentiator. Все три вместе — позиция, которую нельзя повторить без mesh-инфраструктуры и вертикальных fixtures. Это то, что у нас уже есть.

---

## 2. Что это есть — и что это НЕ есть

| Это | Это **не** |
|---|---|
| Воспроизводимые исследования с публикуемыми артефактами | Блог / press releases / маркетинг |
| Узкая вертикальная аналитика (supply-chain, geopolitics, regulation) | Универсальный leaderboard «кто умнее» |
| Multi-axis trade-off карты (quality × cost × speed × role × vertical) | Один скаляр / один рейтинг |
| Открытые packs + golden sets + raw responses + interpretation prompt | Закрытая методика «верьте на слово» |
| Интерпретация, написанная фиксированной моделью по фиксированному шаблону | Аналитика «от руки», субъективная |
| Регулярные релизы (~раз в квартал на бенч + ad-hoc field notes) | Ежедневный поток / новостной формат |

---

## 3. Место на сайте

### 3.1 Информационная архитектура

- **Top-nav:** новый пункт `Insights` / `Аналитика` / `Аналитика` между `Platforms` и `Contact`.
- **Index страница:** `site/insights.html` — карточки публикаций с фильтрами (категория, вертикаль, дата).
- **Подстраницы отчётов:** `site/insights/<slug>.html` (например `insights/critic-bench-semiconductor-2026-q2.html`).
- **Технические артефакты:** ссылка на raw JSONL / pack / golden / interpretation prompt — в репо или public bucket.

Используем существующий `styles.css`, `subpage-hero` паттерн, без новой билд-системы. На v1 — статическая генерация через скрипт-renderer (см. §6).

### 3.2 Категории отчётов

| Категория | Что внутри | Каденция |
|---|---|---|
| **Model Benchmarks** | Role × model × vertical fitness reports | ~раз в квартал |
| **Field Notes** | Аналитика реальных кейсов (как разные модели интерпретировали одну ситуацию) | по событиям |
| **Methodology** | Открытые методички: как строим packs, как размечаем golden, как промптим judge | по необходимости |
| **Data drops** | Публикуемые fixtures + raw run data | вместе с бенчами |

Каждая категория имеет свой шаблон страницы (см. §5).

### 3.3 Языки

EN / RU / KZ — как сейчас на сайте. Но честно зафиксировать: **interpretation в v1 только на EN** (один interpreter, один prompt, одна версия). RU / KZ — структурные строки UI (заголовки, навигация, метаданные карточек), таблицы метрик, методология.

---

## 4. Что мерим — критерии

Метрики разделены на **четыре слоя** + три **специальных среза** (multi-agent, role-fit, industrial). Каждый слой имеет своё назначение и свою стоимость получения.

### 4.1 Слой A — Operational (machine-recorded, бесплатно)

| Метрика | Зачем |
|---|---|
| `latency_ms` (p50, p95) | Цена ожидания в проде |
| `input_tokens / output_tokens` | Базовая стоимость |
| `cost_usd` (по прайслисту OR / провайдера на момент прогона) | Сравнимость по бюджету |
| `schema_pass_rate` | Базовый сигнал пригодности для агентского пайплайна |

### 4.2 Слой B — Contract fidelity

Подметрики `schema_pass_rate`, но публикуются отдельно:

| Метрика | Что ловит |
|---|---|
| `strict_json_pass` | Прошёл ли response_format strict |
| `schema_validation_pass` | Прошёл ли pydantic после парсинга |
| `truncation_rate` | `finish_reason=length` |
| `enum_violation_count` | Попытки выйти за enum (`composition`, `relationKind`, etc.) |
| `schema_drift_over_turns` | Schema держится в N=1, падает в N=5? (только для multi-turn ролей) |

Это **уникальная для нас** ось: никто публично не разделяет «формально валидный JSON» и «семантически верный структурный ответ».

### 4.3 Слой C — Reasoning quality (требует golden set)

| Метрика | Что меряет |
|---|---|
| `blocker_recall` | Какую долю «известных пробелов» в pack модель назвала |
| `blocker_precision` | Сколько из её blockers — реальные, а не фантомные |
| `composition_agreement` | `go / conditional / stop` совпадает с reference |
| `composition_calibration` | Когда модель ставит `stop`, она и правда чаще права? |
| `mission_hint_actionability` | LLM-as-judge оценка 0–3 «насколько hint исполним» |

### 4.4 Слой D — Editorial interpretation

Не метрика — narrative. Пишется interpreter LLM по фиксированному шаблону (§7). Не оцифровывается, **публикуется отдельно** от чисел.

### 4.5 Срез M — Multi-agent dynamics

Главная белая дыра в индустрии. У нас есть mesh — мы можем мерить:

| Метрика | Как считается |
|---|---|
| `fact_survival_rate` | % фактов из output агента A, выживших в reasoning агента B |
| `entity_preservation` | Доля упоминаний, использующих ту же label / canonical path, что и в pack A |
| `reference_resolution` | Понял ли B `id` сущности из pack A (не «node 7 (Taiwan?)») |
| `blocker_overlap` | Пересечение списков blockers двух разных моделей-критиков на одном pack |
| `mission_hint_compliance` | % случаев, когда hint от модели-критика к модели-аналитику в N+1 цикле был исполнен |
| `convergence_cycles` | Сколько supervisor-циклов до `composition=stop` (с поправкой на сложность pack) |
| `self_vs_other_bias` | Та же модель X критикует output X vs output Y — есть ли systematic снисходительность к собственному |

Эти метрики **в принципе невозможно** посчитать на one-shot harness. Это наш моат.

### 4.6 Срез R — Role-fit (модель × роль)

Каждая модель тестируется в **четырёх ролях** на тех же packs:

| Роль | Главная метрика | Вторичная | Не важно |
|---|---|---|---|
| Explorer / supply_chain_analyzer | `recall@known-entities` на pack | `novelty rate` (новые валидные факты) | latency |
| Critic | `blocker_recall`, `blocker_precision` | `composition_calibration` | output verbosity |
| Canonicalizer / NER-typer | `term_preservation`, `type_accuracy` | `enum_loyalty` | reasoning depth |
| Supervisor | `cycle_efficiency`, `hint_actionability` | `budget_discipline` | one-shot accuracy |

Результат — **вторая ось** в trade-off карте: «fitness by role». Узнаваемо, не похоже на остальной маркет.

### 4.7 Срез I — Industrial competence (модель × вертикаль)

Самостоятельный mini-suite на каждую вертикаль. Не «понимает ли модель supply chain в целом» — а конкретные disciplinary probes:

| Тип пробы | Пример |
|---|---|
| **Disciplinary literacy** | Различает ли DUV / EUV / High-NA EUV; liquefaction vs regasification terminal; VLCC vs Suezmax по route applicability |
| **Acronym discipline** | Использует ли FOB / CIF как incoterm, не разворачивая в «free on board (a shipping term)» |
| **Unit fidelity** | bpd ↔ MMbbl/yr; DWT vs GT; Brent ↔ Urals в $/bbl |
| **Source authority recognition** | Знает ли что JKM от Platts; SCFI от SSE; OPEC MOMR vs IEA OMR — разные методологии |
| **Negative space industrial reasoning** | Pack про экспорт 7nm чипов в КНР без слова «sanctions» — модель *должна* в blockers упомянуть BIS EAR / FDPR |
| **Cross-jurisdictional reasoning** | Различает G7 price cap vs EU sanctions vs OFAC SDN vs UK OFSI |
| **Synonym collision resistance** | Aramco (Saudi Aramco) vs Aramco Trading Company vs SAMREF — не сольёт ли |

Каждая проба — micro-pack 200–500 токенов. Полный suite — 50–100 проб на 5 индустрий. Это, по сути, **вертикальный ARC-AGI для supply-chain**, и его никто не делал.

### 4.8 Принцип отображения

**Один скаляр = ложь.** Любая страница отчёта показывает **минимум три оси** (Quality / Cost / Speed) для каждой роли, и **отдельную таблицу** для industrial probes. Лидеры на разных осях разные — это и есть главный insight каждого отчёта.

---

## 5. Как показываем — форма страницы отчёта

Шаблон жёсткий, как у научной публикации. Каждая страница = одна вертикаль × все роли × все модели в скоупе.

```
┌──────────────────────────────────────────────────────────────┐
│ [eyebrow] Model Benchmarks · v1 · 2026-Q2                    │
│ [h1]      Critic role: supply-chain reasoning                │
│           under strict JSON contract — semiconductor         │
│ [lead]    8 моделей × 1 роль × 5 packs. $0.18 за прогон.    │
│           Лидер по качеству ≠ лидер по цене ≠ лидер по      │
│           jurisdictional fidelity.                           │
└──────────────────────────────────────────────────────────────┘

[TL;DR — 4–5 строк]
- Best quality:                <model>  (recall 0.82 / precision 0.91)
- Best vocabulary discipline:  <model>  (term_preservation 0.96)
- Best calibration:            <model>  (confidence ECE 0.04)
- Best $/quality ratio:        <model>  ($0.0004 / quality 0.78)
- Most reliable schema:        <model>  (100% over 25 runs)

[Trade-off chart — minimum 3 axes]
  Quality × Cost × Speed   (scatter с подсветкой лидеров)

[Role × Model fitness matrix]      ← главный визуал v1+
              Explorer  Critic  Canonicalizer  Supervisor
gpt-5.3       ●●●●○    ●●●○○   ●●●●●          ●●●●○
claude-opus   ●●●●●    ●●●●●   ●●●●○          ●●●●●
gemini-3.1    ●●●●●    ●●●○○   ●●●○○          ●●●○○
o4-mini       ●●○○○    ●●●●●   ●●●●○          ●●●○○
…

[Industrial competence probes — для этой вертикали]
  20 проб, accuracy heatmap по моделям

[Handoff fidelity heatmap — multi-agent срез]
  A → B стрелки, ширина = fact_survival_rate

[Calibration plot — для топ-модели в роли критика]
  confidence (выраженная) vs accuracy (по golden)
  (это «разоблачающая» картинка, которой нет ни у кого)

[Detailed table — sortable, sticky header]
  model | quality | speed (p50/p95) | cost/call | schema | …

[Methodology block — на странице, не в приложении]
  - Pack set:        <slug>  (link)
  - Schema:          <slug>  v3  (link)
  - Reference:       human-curated, 2 reviewers (link)
  - Judge model:     <slug>  (disclosure)
  - Run timestamp:   2026-04-17 14:00 UTC
  - Provider snapshot: link to raw responses

[Interpretation — see §7]
  600–1000 слов, фиксированная структура

[Reproduction]
  git checkout <sha>
  python scripts/bench_role_fit.py --pack <slug> --role critic
  → ожидаемый разброс: ±X% quality, ±Y% latency

[Artifacts]
  - raw JSONL  (download)
  - session pack  (download)
  - reference  (download)
  - interpretation prompt  (link)
```

UX-принципы (обязательные):

1. **TL;DR сверху, четырьмя строками.** Нет «погружения в методологию». Победитель по каждой оси сразу.
2. **Trade-off chart, не leaderboard.** Минимум три оси.
3. **Methodology прямо на странице** — не в приложении.
4. **Reproduction one-liner** — знак качества для технической аудитории.
5. **Disclosure judge-модели обязателен.**

---

## 6. Как оцифровываем — формат хранения

Чтобы раздел был **датасет + срез**, а не «странички с цифрами»:

### 6.1 Источник истины

`bench/results/<bench_slug>/<run_id>.json` в репо. Каждый run — иммутабельный JSON-файл. Один файл на прогон (не на модель — иначе теряется атомарность).

### 6.2 Schema run-файла (минимум)

```jsonc
{
  "bench_id": "role-fit-critic-semiconductor",
  "bench_version": "v1",
  "pack_id": "semiconductor-tsmc-chips-act",
  "pack_version": "v3",
  "run_id": "2026-04-17T14:00Z--abcd1234",
  "run_timestamp": "2026-04-17T14:00:00Z",
  "git_sha": "abcd1234...",
  "judge_model": "claude-opus-4.7",
  "judge_prompt_sha": "...",
  "interpreter_model": "claude-opus-4.7",
  "interpreter_prompt_sha": "...",
  "models": [
    {
      "slug": "openai/o4-mini",
      "provider_name_returned": "OpenAI",  // из OR ответа, страховка от drift
      "metrics": {
        "operational": { "latency_ms_p50": 6200, "cost_usd": 0.0026, ... },
        "contract":    { "strict_json_pass": true, ... },
        "reasoning":   { "blocker_recall": 0.82, ... },
        "multi_agent": { ... },
        "industrial":  { ... }
      },
      "raw_response": { ... }   // или ссылка
    }
  ]
}
```

### 6.3 Renderer

`scripts/render_bench_page.py` (новый) — берёт `<run_id>.json` + interpretation markdown и склеивает в `site/insights/<bench_slug>-<date>.html` по jinja-шаблону. Ручного HTML под отчёт **нет**.

### 6.4 Indexer

`site/insights.html` — список карточек, читает `bench/index.json` (auto-generated при каждом релизе).

### 6.5 Архивирование

Старые прогоны **не удаляются и не перезаписываются**. Через 6+ прогонов одного бенча получаем **trend chart** — самостоятельная фича раздела (например «o4-mini quality drift 2026-Q2 → 2026-Q4»).

---

## 7. Как интерпретируем — роль interpreter LLM

«Interpretation» — самостоятельный блок на 600–1000 слов в каждом отчёте, генерируется фиксированной моделью по фиксированному prompt-шаблону. Структура жёсткая, иначе скатится в маркетинг.

### 7.1 Структура интерпретации

| Параграф | Что в нём |
|---|---|
| **What changed since last run** | Если не первый прогон — что сдвинулось у моделей. **Самая ценная** часть для подписчика. |
| **Where the leaderboard is misleading** | На какой оси чей лидер ложный. «Модель X лидирует по cost, но в 1 из 5 прогонов даёт `composition=go` против reference `stop` — inversion failure, опасный паттерн». |
| **Failure modes by class** | Не «модель X плохая», а **типология ошибок**: schema-drop, blocker hallucination, composition inversion, mission-hint vagueness. По каждому — какая модель к какому классу склонна. |
| **Practical implication** | Что это значит для практика, который выбирает модель для агентского пайплайна. Без рекомендации «используйте X», только «если ваш use-case Y, ось Z важнее». |
| **What we did NOT measure** | Длинный контекст, multi-turn, tool-calling под burst, adversarial packs. Честный бенч **всегда** говорит, чего не покрыл. |

### 7.2 Жёсткие правила

- Interpreter **не оценивает сам**. Все числа приходят из прогонов и judge-модели **отдельным шагом**.
- Interpreter **читает таблицу и формулирует выводы**. Никаких «I think», только «data shows / data does not show».
- Interpreter prompt и его SHA **публикуются** в methodology каждого отчёта. Чтение → reproducibility.
- Interpreter model и judge model **должны различаться** или хотя бы быть разной версии — иначе self-confirmation.

### 7.3 Где живёт prompt

`agents/shared/agenthorde_common/agenthorde_common/bench/interpreter_prompt.py` (новая папка `bench/`). Версионируется в git. Каждое изменение prompt'а — bump SHA, явно отмечается в release notes отчёта.

---

## 8. Дорожная карта релизов

### v0 — proof-of-existence (~1 неделя)

Цель: одна страница на сайте, один отчёт, существующие данные.

- Вынести зафиксированный вывод из `agents/vertex/agenthorde/README.md` (раздел «Критик OpenRouter fallback», апрель 2026) в формат отчёта v0.
- Обобщить `bench_critic_models.py` под `--pack PATH` (поддержка нескольких packs).
- Закоммитить 3 fixture pack'а в `bench/packs/` (semiconductor, lithium, malacca — есть готовые).
- Прогнать единоразово на 8 моделях × 3 packs.
- Собрать одну страницу `site/insights/critic-bench-2026-q2.html` руками (HTML без renderer'а — пока).
- Один interpretation от Claude Opus / GPT-5 на 600 слов.
- Обновить top-nav на `index.html` + создать `insights.html` (index с одной карточкой).

**Продукт:** один публикуемый отчёт, демонстрирующий формат. Без role-fit, без industrial probes, без multi-agent. Только Operational + Contract fidelity + grubby версия Reasoning quality.

### v1 — публикуемый бенч (~6–8 недель)

Цель: первый отчёт уровня «можно ссылаться публично».

- Renderer (`scripts/render_bench_page.py`) + index generator.
- Golden set ручной разметки на 5 packs (один разметчик, один reviewer).
- Полностью слой Reasoning quality (recall / precision / composition agreement).
- Один срез из M (multi-agent): `fact_survival_rate` + `mission_hint_compliance` (требует прогона критик→аналитик в цикле).
- Industrial probe suite — 20 проб на одну вертикаль (semiconductor).
- Версионируемый interpreter prompt + SHA в methodology.
- Vertex Gemini в leaderboard как baseline (не только OR-модели).
- Реализовано на одной роли — **Critic** — но **уже на 3–5 packs**.

**Продукт:** один большой отчёт `Critic Role × Semiconductor Vertical`. Первая публикация уровня «независимое исследование».

### v2 — role-fit полностью (~3 месяца от v1)

Цель: второй отчёт, доказывающий главный тезис.

- Расширить harness под все 4 роли (explorer / critic / canonicalizer / supervisor).
- Industrial probes на 3 вертикали (semiconductor + energy + maritime).
- Multi-agent срез M полностью (handoff fidelity matrix, blocker_overlap, self_vs_other_bias).
- Trend chart — сравнение v1 vs v2 для общих моделей.
- Календарь релизов: квартальный.

**Продукт:** второй отчёт `Role-fit × 3 verticals × 8 models`. После него позиционирование **закреплено**.

### v3+ — постоянная работа

- Каденция: квартальный главный отчёт + ad-hoc field notes по событиям.
- Расширение по вертикалям: lithium, REE, agro, defense.
- Расширение по моделям: каждый новый релиз frontier модели — ad-hoc «delta report» (за 1 неделю после релиза).
- Open dataset: packs + golden + raw responses под CC-BY (см. §10).

---

## 9. Риски и как закрываем

| Риск | Закрытие |
|---|---|
| «Бенч self-serving — лидирует ваш стек» | Vertex Gemini как baseline в каждом отчёте; honest disclosure: «we use o4-mini in production critic; v1 confirms its position on Quality/Cost frontier — see methodology for selection criteria predating this bench» |
| «Один pack — не бенч» | v1 на 3–5 packs, явно фиксируем как floor |
| «LLM judge subjective» | В v1 judge используется только для `mission_hint_actionability`, **не для основных метрик** (они против golden) |
| «Interpreter biased» | Interpreter prompt публикуется; interpreter ≠ judge; в v3+ — ротация моделей-интерпретаторов |
| Дрейф моделей у OR-провайдеров | `provider_name_returned` в каждом run-файле; раз в N прогонов — **integrity probe** (одинаковый pack, разные провайдеры одной модели — тест на consistency) |
| «Это копия Artificial Analysis» | Differentiator явный: vertical (supply-chain) + role-fit + multi-agent + strict-schema fidelity. На главной странице раздела это утверждение **первое**, не последнее |
| Юридика по publishing reference verdicts | Golden sets создаются на open-domain фактах (TSMC, Aramco, etc.); никаких клиентских данных |
| Cost для нас | Полный прогон v1 ≈ $0.20–0.50 (8 моделей × 3 packs). Квартально — $1–2/год. Roundoff. |

---

## 10. Открытые вопросы (требуют решения до v0)

Не блокируют начало работы, но фиксируют позицию на старте:

| # | Вопрос | Опции | Рекомендация |
|---|---|---|---|
| 1 | Название раздела | `Insights` / `Аналитика` vs `Field Reports` / `Decision Lab` / `Open Lab` | `Insights` — нейтрально, расширяемо |
| 2 | Бренд бенча | `AgentHorde Critic Bench` (узко, технически) vs `Horde.Vision Supply-Chain Reasoning Bench` (шире, продуктовее) | Второе. Бенч ≠ AgentHorde, бенч — продуктовая аналитика |
| 3 | Каденция | По требованию / ежемесячно / ежеквартально | Квартально + ad-hoc field notes |
| 4 | Открытость данных | Полные packs + golden vs только metric tables | **Полные packs + golden под CC-BY** — иначе бенч не воспроизводимый, теряет научный вес |
| 5 | Interpreter | Одна модель всегда (Claude Opus / GPT-5) vs ротация | Одна в v0–v1, ротация в v3+ |
| 6 | Languages | EN-only interpretation в v1 vs обязательный RU | EN-only interpretation, RU/KZ только для UI |
| 7 | Где живёт harness | `agents/vertex/agenthorde/scripts/` (текущее) vs `bench/` в корне | **`bench/` в корне** — это уже не AgentHorde-внутреннее, а самостоятельный продукт |
| 8 | Хостинг страниц отчётов | В `site/` (Firebase) vs отдельный sub-domain | В `site/insights/` — переиспользуем хостинг и стилистику |

---

## 11. Связанные артефакты

### В коде

- `agents/vertex/agenthorde/scripts/bench_critic_models.py` — текущий one-shot bench, основа v0.
- `agents/shared/agenthorde_common/agenthorde_common/critic/ab_log.py` — формат row-логов для прогонов.
- `agents/shared/agenthorde_common/agenthorde_common/critic/openrouter_client.py` — OR-клиент.
- `agents/shared/agenthorde_common/agenthorde_common/critic/strict_schema.py` — нормализатор JSON Schema.
- `agents/vertex/agenthorde/agenthorde/critic/fallback.py` — A/B orchestrator (Vertex + N OR).
- `backend/internal/ai/model_manager.go` — реестр моделей.
- `agents/vertex/agenthorde/fixtures/` — текущие fixtures, кандидаты в packs.

### В документах

- `agents/vertex/agenthorde/README.md`, раздел **«Критик OpenRouter fallback»** — зафиксированный вывод бенча апрель 2026, готовый прото-отчёт.
- `docs/horizon/architecture/supply-chain-canonical-control-plane.md` — почему canonical paths критичны (релевантно для метрик `term_preservation`, `entity_preservation`).
- `docs/canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md` — словарь, против которого можно мерить vocabulary discipline.
- `AGENTS.md` — обязательная модель мышления supply-chain (релевантно для interpreter prompt'а).

### Будут созданы

- `bench/packs/<vertical>/<slug>.json` — публикуемые fixtures.
- `bench/golden/<pack-slug>.json` — reference verdicts.
- `bench/results/<bench-slug>/<run-id>.json` — иммутабельные прогоны.
- `bench/probes/<vertical>.jsonl` — industrial probe suite.
- `agents/shared/agenthorde_common/agenthorde_common/bench/interpreter_prompt.py` — версионируемый prompt.
- `scripts/render_bench_page.py` — generator страниц отчётов.
- `site/insights.html` — index раздела.
- `site/insights/<slug>.html` — отчёты.

---

## 12. Главный тезис ещё раз

Все public benches отвечают на вопрос «**какая модель умнее**». Ответ почти всегда один и тот же скучный (топ-3 frontier model по очереди), и он **бесполезен** тому, кто реально строит мульти-агентную систему на индустриальной задаче.

Мы отвечаем на другой вопрос: **«какая модель в какой роли работает на какой вертикали — и где её ломает».** Этот вопрос:

- задают все, кто строит агентов;
- его задают frontier лабы в своих system cards (но не дают ответа в форме бенча);
- на него **физически невозможно** ответить без mesh-инфраструктуры и вертикальных fixtures;
- у нас и mesh, и fixtures **уже есть**.

Раздел `Insights` на сайте — это не «ещё одна страница», это **публичная форма** того, что мы и так делаем внутри. Себестоимость прогонов копеечная, ценность для позиционирования высокая, и каждое следующее повторение делает раздел сильнее (trend charts, расширение вертикалей, новые модели).
