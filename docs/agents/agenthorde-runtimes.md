# AgentHorde: два рантайма (Vertex и Azure)

Мы держим **одну общую «смысловую» часть** и **две обвязки под облако**, чтобы сравнивать поведение моделей и оркестрации без дублирования канонизатора и JSON-контрактов.

## Раскладка в репозитории

| Слой | Путь | Назначение |
|------|------|------------|
| **Общее** | [`agents/shared/agenthorde_common/`](../../agents/shared/agenthorde_common/) | Пакет `agenthorde_common`: пост-ран канонизатор (фазы 0–9), таксономия, словари, **схемы** `SessionPack`, `AnalyzerOutput`, `SupervisorDecision`, … |
| **Vertex + ADK** | [`agents/vertex/agenthorde/`](../../agents/vertex/agenthorde/) | `google-adk` + Gemini/Vertex: супервизор, воркеры, tools с Vertex grounding, критик (Vertex → OpenRouter fallback), `runner_cli.py`. |
| **Azure (без ADK)** | [`agents/azure/agenthorde_azure/`](../../agents/azure/agenthorde_azure/) | Нативный для Azure путь: **Azure OpenAI** (`openai.AsyncAzureOpenAI`), обычный asyncio; мультиагентный цикл переносится сюда по мере готовности. |

Установка общего пакета (из venv, в каталоге любого рантайма):

```bash
pip install -e agents/shared/agenthorde_common
```

Vertex-стек дополнительно тянет `agents/vertex/agenthorde/requirements.txt` (там же `-e` на общий пакет).

## Что считаем «контрактом» между средами

- **Идентичны по смыслу**: структуры в `agenthorde_common.schemas.*`, вход/выход канонизатора (`CanonicalizerInput` / `CanonicalizerResult`), переменные окружения `AGENTHORDE_CANONICALIZER_*` и `HORIZON_FIRESTORE_PROJECT` / `GOOGLE_CLOUD_PROJECT` (см. `agenthorde_common.config`).
- **Различаются намеренно**: способ вызова LLM (Vertex genai + ADK vs Azure OpenAI / Microsoft Foundry), сессии ADK, и **платформенный бэкенд** для одних и тех же имён tools (см. ниже).
- **Общее для tools**: имена и JSON-схемы function-calling живут в `agenthorde_common.llm.openai_tool_definitions`; маршрутизация имён (`web_search`, `retrieve`, …) — в `agenthorde_common.tools.openai_function_dispatch.build_standard_openai_tool_dispatch`, куда рантайм подставляет только реализацию grounded `web_search` (Vertex: Google Search grounding в `agenthorde.tools.builtins`; Azure: OpenAI **Responses API** + tool `web_search` в `agenthorde_azure.web_search`).
- **Контекст вызова tool (sync, Vertex ADK)**: `agenthorde_common.tools.tool_context` — протокол `AgentHordeToolContext` с единственным полем `state` (тот же словарь сессии, что и у Azure dispatch). Тесты и мосты используют `MutableStateToolContext` / `state_as_tool_context`.

### Где здесь MCP (Model Context Protocol)

**MCP** — это не замена супервизору AgentHorde, а **стандартный транспорт/обёртка для внешних tools и ресурсов** (stdio/SSE, список tools, вызов по имени с JSON args). В общей архитектуре он полезен как **опциональный слой сшивки**:

- **Снаружи внутрь**: отдельный MCP-сервер может отдавать, например, корпоративный поиск или БД; хост AgentHorde (или отдельный адаптер) по имени tool дергает MCP `call_tool` и кладёт результат в тот же формат, что и сегодняшние `ToolEnvelope` / session ledger.
- **Внутрь наружу**: наоборот, набор AgentHorde session tools можно экспонировать как MCP tools для других клиентов — контракт сессии остаётся `state`, MCP лишь переносит вызовы.

Репозиторий **не тянет** зависимость `mcp` на уровне core: протокол `AgentHordeToolContext` описан так, чтобы объект с `.state` из MCP-хоста или тонкой прослойки был совместим без импорта MCP в `agenthorde_common`.

## Как тестируем «в обе стороны»

1. **Канонизатор и схемы** — pytest под [`agents/vertex/agenthorde/tests/`](../../agents/vertex/agenthorde/tests/): импорты идут в `agenthorde_common`; эти тесты не требуют ни Vertex, ни Azure, если не дергают живой LLM/Firestore.
2. **Vertex-мультиагент** — интеграционные прогоны с ADC и проектом GCP (`runner_cli.py`, см. README в `agenthorde`).
3. **Azure** — после настройки `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_DEPLOYMENT`, `AZURE_OPENAI_API_VERSION` (по умолчанию в коде — preview с поддержкой `/openai/responses` и grounded `web_search`, см. README Azure): [`agents/azure/agenthorde_azure/runner_azure_cli.py`](../../agents/azure/agenthorde_azure/runner_azure_cli.py) запускает **тот же** супервизорный цикл, что и Vertex. `web_search` идёт через **Azure OpenAI Responses API** (источники из `url_citation` в ответе); при ошибке API — деградация к обычному chat без живого web. `--probe` — один вызов deployment.

Цель раздельных прогонов: **один и тот же канонизатор** на одном и том же дампе сессии из Vertex или Azure, чтобы сравнивать только разницу в upstream (агенты/модели), а не пост-обработку графа.

## Известные асимметрии рантаймов и TODO

Различия в поведении, которые **не объясняются ни моделью, ни промптом**, а живут в обвязке вокруг LLM. Их стоит держать в голове при сравнительных бенчмарках.

### TODO-1: параллельный диспатч tool_calls в Azure

**Контекст:** уровень-1 цикл (внутри одного агентного хода) — это «модель → возможно несколько `tool_calls` → исполнение → следующий LLM ход».

- **Vertex (ADK):** когда модель в одном LLM-ходе возвращает несколько function calls, ADK параллелит их через `asyncio.create_task` + `asyncio.gather` (см. `google/adk/flows/llm_flows/functions.py`, поиск `# Create tasks for parallel execution`). Wall-clock одного раунда ≈ max по самому медленному tool, а не sum.
- **Azure (наш `agents/shared/agenthorde_common/agenthorde_common/llm/openai_tool_loop.py`):** `for tc in tool_calls: await dispatch(...)` — **строго последовательно**. На моделях, которые охотно возвращают 2–4 tool_calls за раунд (GPT-5.4 / GPT-5.4-mini), Azure-сторона теряет wall-clock даром — без какой-либо разницы в качестве.

**Что сделать:** заменить последовательный цикл на `asyncio.gather` с предварительной аккуратной проверкой того, что наши tool-реализации не делают конкурентных не-атомарных мутаций общего `state` (особенно `tool_ledger`, `proposed_*` буферы). Возможно, потребуется маленький `asyncio.Lock` вокруг операций, которые пишут в shared счётчики/реестры.

**Зачем:** убрать неинтерпретируемую разницу wall-clock между Vertex и Azure при сравнительных прогонах; иначе любая «Azure медленнее на N секунд» автоматически путается с эффектом модели.

### TODO-2: параллельные аналитики на уровне-2 (supervisor)

**Контекст:** уровень-2 цикл (`planner → analyzer → governance → sanctions → critic → next`).

Сейчас supervisor крутит агентов **строго сериально** в обоих рантаймах. Это даёт детерминированный порядок мутаций session state и совпадение с тем, как критик смотрит «уже накопленную» картину. Но `supply_chain_analyzer`, `governance_control_analyzer`, `sanctions_analyzer` после планнера **не имеют data-dependency друг от друга** — план зафиксирован, evidence у каждого своя.

**Что сделать (черновик):** ввести на supervisor уровне «фазу веера» сразу после `question_planner`: запустить 2–3 аналитика параллельно через `asyncio.gather`, каждый пишет в свою партицию state-ключей, критик дальше видит уже собранную картину. Поломает текущий контракт upd-порядка `proposedEntities/proposedRelations` — нужен merge-шаг.

**Зачем:** на длинных вопросах (напр. semi + governance + sanctions одновременно) дёшево сократить общий wall-clock × ~3.

### TODO-3: переименовать legacy venv `.venv-agentloom` → `.venv-agenthorde`

**Контекст:** в `agents/vertex/` лежит venv-директория `.venv-agentloom`, оставшаяся от раннего этапа разработки, когда эта часть системы называлась AgentLoom. Сейчас вся подсистема — **AgentHorde**, и параллельного имени быть не должно.

Это **только имя директории**: никакого пакета `agentloom` в коде нет, импорт `agentloom` нигде не делается, документация в `docs/` про AgentLoom не упоминает. Единственные текстовые следы — два docstring'а в probe-скриптах с примером запуска через путь к venv:

- `agents/vertex/agenthorde/scripts/probe_phase1_full_registry_o4.py`
- `agents/vertex/agenthorde/scripts/probe_canonicalizer_phases_o4.py`

**Что сделать:**

1. `mv agents/vertex/.venv-agentloom agents/vertex/.venv-agenthorde` (или пересоздать venv с правильным именем — на случай, если pyvenv.cfg ссылается на абсолютные пути).
2. Заменить `.venv-agentloom` → `.venv-agenthorde` в двух probe-скриптах.
3. Проверить локальные shell-aliases / IDE Python interpreter pinned-paths — у разработчика они могут быть.

**Зачем:** убрать визуальный шум в командах запуска (особенно в логах бенчмарков и в скриншотах для портала аналитики) — иначе читатель видит «agentloom» и думает, что это отдельный модуль.

### Наблюдение (не TODO): `reasoning.effort=low` неэффективен для stealth-моделей через OpenRouter

В smoke-прогоне на `openrouter/elephant-alpha` передача `extra_body={"reasoning":{"effort":"low"}}` в `chat.completions` **не сократила** thinking-бюджет analyzer'а — вместо ожидаемого ускорения латентность выросла (с 16с до 158с) и снова упёрлась в truncation. Гипотезы: либо провайдер игнорирует параметр для этой модели, либо elephant сам диктует свой бюджет thinking. Полезно знать при выборе тактики ускорения reasoning-моделей: эффорт не всегда работает «снаружи», иногда нужен role-shape (см. идею гибрида в TODO-1 — короткий критик на mini поверх глубокого аналитика на reasoning-модели).
