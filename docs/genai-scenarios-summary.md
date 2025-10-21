## GenAI Scenarios — Consolidated Results (Grok, OpenAI, Gemini)

Date: 2025-10-19

Models:
- Grok: `grok_fast` → grok-4-fast-reasoning, `grok_quality` → grok-4-0709
- OpenAI: `gpt_fast` → gpt-5-nano, `gpt_balanced` → gpt-5-mini, `gpt_quality` → gpt-5
- Gemini: `gemini_fast` → gemini-2.5-flash-lite, `gemini_balanced` → gemini-2.5-flash, `gemini_quality` → gemini-2.5-pro

Scenarios (per model):
1. loose: Q/A only
2. loose: Q/A + tool
3. loose: Q/A + search
4. loose: Q/A + tool + search
5. strict: Q/A only (schema)
6. strict: Q/A + tool (schema)
7. strict: Q/A + search (schema)
8. strict: Q/A + tool + search (schema)

Legend: 🟩 OK, 🟨 Timeout, 🟥 Fail/Unsupported

### Status Matrix

| Model | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| grok_fast | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |
| grok_quality | 🟩 | 🟨 | 🟩 | 🟨 | 🟩 | 🟩 | 🟩 | 🟨 |
| gpt_fast | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |
| gpt_balanced | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |
| gpt_quality | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟨 |
| gemini_fast | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |
| gemini_balanced | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |
| gemini_quality | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 | 🟩 |

Notes:
- Gemini: public v1beta API не поддерживает одновременные tools + search в одном запросе. В коде реализован двухшаговый пайплайн (tool‑only → grounded‑only) — поэтому сценарии 4 и 8 отмечены как OK.
- Grok quality: наблюдались таймауты на сценариях 2, 4, 8 (перемежающаяся нестабильность). Таймаут клиента отключен, руководствуемся deadline контекста.
- OpenAI quality: в одном из прогонов `8` дал deadline exceeded.

### Indicative Timings (per model, recent runs)

| Model | Suite Duration |
|---|---:|
| grok_fast | ~58s |
| grok_quality | varies; timeouts on heavy scenarios |
| gpt_fast | ~0m37s |
| gpt_balanced | 3m30s |
| gpt_quality | 6m36s (last scenario timed out) |
| gemini_fast | ~0m18s |
| gemini_quality | ~0m40s |
| gemini_balanced | 2m45s |

Timing caveats:
- Время зависит от длины промпта и латентности поиска. Значения ориентировочные, зафиксированы по последним прогонам.

### Quality Assessment (1–10)

Критерии: точность, полнота, структурность (strict), устойчивость ссылок/источников, стабильность сценариев.

| Model | Quality (1–10) | Rationale |
|---|---:|---|
| grok_fast | 7.5 | Хорошая структурность и ссылки; местами колебания поиска. |
| grok_quality | 8.0 | Сильное содержание, но нестабильность/таймауты в сложных сценариях. |
| gpt_fast | 7.0 | Быстро и стабильно, умеренная полнота. |
| gpt_balanced | 8.0 | Баланс качества/скорости, корректные function calls и источники. |
| gpt_quality | 8.5 | Глубже по содержанию; один таймаут в `8`. |
| gemini_fast | 7.5 | Быстрый отклик, корректные tool‑вызовы, адекватные ссылки. |
| gemini_balanced | 8.0 | Стабильно, хорошие атрибуции. |
| gemini_quality | 9.0 | Лучшие grounded ответы, строгие structured вызовы стабильно. |

### Provider‑Specific Notes

- Gemini
  - Tools + Search в одном запросе не поддержаны публичным API: используем двухшаговый пайплайн (tool → grounded) с объединением Usage/Sources/SearchQueries.
  - Strict JSON: через function tool `return_structured` (без `response_mime_type`), схема валидируется на сервере. 
  - MALFORMED_FUNCTION_CALL: см. `docs/gemini-scenarios.md` — упрощать схему, один tool, понижать temperature.

- OpenAI
  - Web search включается как tool; парсим источники/запросы из function calls и текста. 
  - В редких случаях длинные strict + search сценарии требуют увеличения таймаута.

- Grok
  - Live search — request‑level `search` с `sources: [{type: "web"}]`.
  - Отдельные сценарии на `grok_quality` требовательны к таймингам (рекомендация — более щедрый deadline).


