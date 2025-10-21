## OpenAI: Scenario Behavior and Timings

Date: 2025-10-19

Models:
- gpt_fast → gpt-5-nano
- gpt_balanced → gpt-5-mini
- gpt_quality → gpt-5

Scenarios (per model):
1. loose: Q/A only
2. loose: Q/A + tool
3. loose: Q/A + search
4. loose: Q/A + tool + search
5. strict: Q/A only (schema)
6. strict: Q/A + tool (schema)
7. strict: Q/A + search (schema)
8. strict: Q/A + tool + search (schema)

Execution settings:
- MaxOutputTokens: 8192
- Per-scenario timeouts: fast ~45s, balanced ~60s, quality ~75s
- Search: enabled via `web_search` tool when `webSearchEnabled=true`
- Strict: function-calling with `strict: true` and schema; tool_choice `required` for tool scenarios

### Results Overview
- Все три модели прошли 8-сценарный прогон. В одном из запусков 2 tool-сценария вернули 400 (schema validation) из‑за `required`; схема в тест-раннере исправлена (добавлен `keyPoints`), повторные прогоны проходят.
- Tool-calling работает стабильно; `return_structured` вызывается со строгой схемой.
- Web search выполняется (видно несколько `web_search_call`), ссылки извлекаются из контента и аннотаций.
- Постобработка URL сохраняет скобки у Wikipedia‑ссылок, обрезает шум, добалансирует `)` при необходимости.

### Timings (approx.)
- gpt_fast: ~35–65s
- gpt_balanced: ~38–60s
- gpt_quality: ~35–65s

### Quality Notes
- Q/A only: связные ответы; без источников.
- Tool: структурированный вывод по схеме; значения `confidence` выглядят реалистично.
- Search: источники включают требуемую страницу Wikipedia; иногда добавляются релевантные ссылки (иногда с UTM‑параметрами).
- Usage: возвращаются `prompt/output/total` и `reasoning_tokens`.

### Scenario status (последние прогоны)
- loose: Q/A only — OK
- loose: Q/A + tool — OK (после фикса required)
- loose: Q/A + search — OK
- loose: Q/A + tool + search — OK (после фикса required)
- strict: Q/A only (schema) — OK
- strict: Q/A + tool (schema) — OK
- strict: Q/A + search (schema) — OK
- strict: Q/A + tool + search (schema) — OK

### Extracted Parameters
- Usage: prompt/output/total/reasoning aggregated.
- Sources: parsed from assistant `content` and function `arguments`; balanced parentheses preserved.
- Search queries: captured when present in structured snippets.

### Recommendations
- Consider normalizing UTM parameters out of final URLs if not desired.
- If stricter JSON is needed, continue to enforce via tool schema + `strict` and `tool_choice: required`.


