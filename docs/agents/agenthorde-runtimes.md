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

### DONE: incremental analyzer mode + critic focused continuation (бывший TODO-4)

**Контекст:** на длинных вопросах analyzer вырождался в «monstrous one-shot»: одна LLM-итерация, в которой модель должна одновременно перечислить весь Tier-1, Tier-2, governance и SPOFs. Качество такого прохода жёстко зависит от модели — Sonnet 4.6 случайно вытягивал ZEISS/Cymer/Trumpf, а mini-класс терял Tier-2 целиком. Решение должно было сместить нагрузку с «модель умнее = глубже Tier» на «архитектура декомпозирует = глубже Tier даже на mini».

**Mental model — diff:** аналитик считает свой первый прогон **снимком** (создаёт каркас + декларирует `scopePlan`), а каждый следующий — **diff'ом** к уже накопленному `SessionPack`. Логика merge остаётся инкрементальной (как раньше), меняется только мышление агента: «расширить, а не пересоздать».

**Что реализовано (через общий пакет `agenthorde_common` — обе обвязки получают это автоматически):**

1. **`AnalyzerOutput.scopePlan`** — самодекларация контракта прогона: `focus_areas[]`, `depth_hint`, `deliberate_omissions[]`, `rationale`. Фиксируется в выдаче, чтобы критик мог судить fidelity, а не догадываться о намерении.
2. **`CriticVerdict.scopeAssessment` + `CriticVerdict.recommendedScopeFocus`** — двухосевая оценка прошедшего прогона (adequacy против вопроса × execution fidelity против `scopePlan`) и **структурный** directive на следующий проход: `target_agent`, `add_focus_areas[]`, `deepen_entity_ids[]` (в namespaced виде `analyzer:e3`, …), `evidence_gaps[]`, `directive_text`. Это заменяет рыхлый prose-mission критика.
3. **Supervisor (`coerce_supervisor_decision`)** — когда критик прислал `recommendedScopeFocus` и роутер не задал собственную содержательную миссию, supervisor сворачивает structured directive в строку `mission` с префиксом `FOCUSED FOLLOW-UP. ` — аналитик читает уже структурированное намерение.
4. **Analyzer prompts (`ANALYZER_OPERATING_MODE`)** — явное различение first-pass («строй каркас + объяви scopePlan») и follow-up-pass («это не новый граф, это diff: добавляй только новое; cite existing endpoints by `analyzer:eN` namespaced ids verbatim»). Применено к supply-chain / governance / sanctions analyzer'ам единым шаблоном.
5. **Provenance — `addedAtCycle`** на `PackProposedEntity`, `PackProposedRelation`, `ReasoningNode`. `_merge_analyzer_into_pack` проставляет cycle index при первой вставке и сохраняет при последующих merge'ах. Это нужно чтобы (a) различать «это пришло на cycle-0» vs «появилось в фокусированном follow-up» и (b) уметь строить attribution / отчёты по тому, какой проход что добавил.
6. **Cross-document references в `proposedRelations`** — локальный валидатор `_relations_reference_registry_ids` принимает `analyzer:`/`governance:`/`sanctions:`/`pack:`-namespaced ids и pack-fragment ids (`n12`) как валидные cross-document endpoints, чтобы diff-relation мог связать новый local entity с уже существующим в pack. Без этого весь diff-mode падал на схеме.

**Cross-runtime parity:** Vertex (ADK) и Azure используют один `supervisor_core.py` и один пакет инструкций — поэтому никаких runtime-локальных правок не потребовалось. И Vertex, и Azure автоматически читают `recommendedScopeFocus`, проставляют `addedAtCycle` и принимают diff-mode выдачу.

**Smoke-результат (gpt-5.4-mini, full loop):** cycle-0 выдал 8 поверхностных entities + scopePlan; критик вернул `recommendedScopeFocus` с указанием на Tier-2 chokepoints behind ASML; supervisor свернул это в FOCUSED FOLLOW-UP mission; cycle-1 в diff-mode за 7s добавил **ZEISS SMT, Cymer, Trumpf, Cadence, Synopsys, ARM** + раздельные **Dutch / Japanese / US export control** режимы. Ровно тот срез, который раньше эпизодически вытягивал только Sonnet 4.6 на one-shot. Критерии smoke перенесены: ширину спрашиваем у cycle-0, Tier-2 глубину — у follow-up.

### DONE: канонизатор адаптирован под L1+L2+L3 / incremental analyzer changes

**Контекст:** после внедрения sanctions_analyzer'а, incremental diff-mode и `addedAtCycle` provenance канонизатор отстал — он молча терял всё, что приходило из sanctions, не пробрасывал cycle-провенанс на canonical fragments и сваливал любую неразрешённую relation-endpoint в один общий `entity_id_not_in_registry`. Это превращало тонкие диагностические сигналы в шум и закрывало целый источник evidence для downstream-сшивки.

**Что реализовано (в `agenthorde_common`, оба рантайма автоматически получают это):**

1. **`CanonicalizerInput.sanctions_output`** — новое поле (alias `sanctionsOutput`) принимает блоб `sanctions_analyzer` той же формы, что `analyzer_output` / `governance_output`. Phase 0 обрабатывает три источника симметрично; sanctions-сущности и -relations доходят до phase 4 наравне с supply-chain и governance. Vertex `runner_cli.py` и Azure `runner_azure_cli.py` передают этот блоб в канонизатор и кладут `sanctionsOutput` в выходной JSON.
2. **`addedAtCycle` provenance проброс** — `CanonicalFragment.attributes.addedAtCycle` и `Provenance.agentFirstCycle` берутся из `pack.proposedEntities[].addedAtCycle` (с резолвом через `agent_registry_id_to_entity_id`); fallback — `pack.cycleIndex`. Edges в `_remap_edges` стэмпятся `attributes.addedAtCycle = max(endpoint cycles)` — ребро не может быть старше своих концов.
3. **Cross-doc relation refs распознаются отдельно** — endpoints вида `n12` (pack reasoning-graph node id) и `analyzer:e99` (stale namespaced id из прошлого цикла, не присутствует в текущем merged registry) больше не попадают в безликий `entity_id_not_in_registry`, а получают **выделенные коды** `relation_endpoint_pack_fragment_ref` / `relation_endpoint_stale_namespaced_id`. Downstream (например, pathbuilder) может теперь принимать решение, стоит ли стичить отдельно.
4. **`_namespace_entity_id`** — больше не превращает `n12` в `analyzer:n12`; pack-fragment ids передаются как есть.

**Tests:** `tests/test_pipeline_integration.py::test_pipeline_sanctions_output_flows_through`, `…::test_pipeline_added_at_cycle_propagates_from_pack`, `…::test_pipeline_pack_fragment_relation_ref_recorded_distinctly`, `tests/test_phase0_entity_registry.py::test_phase0_distinguishes_pack_fragment_relation_endpoint`. Полный suite — 298 passed.

### DONE: critic — structural-not-enumerative directive contract

**Контекст:** на бенчах было видно, что критик в `recommendedScopeFocus` начинал перечислять **конкретные имена** ожидаемых сущностей (ZEISS, JSR, Cymer, …), вместо того чтобы описать **слой**, который аналитик должен раскрыть. Это превращает аналитика в «механический expander списка от критика», а сам критик — в «autocomplete-сервис над `domain_priors_lookup`». Качество перестаёт зависеть от способности аналитика к research и начинает целиком зависеть от того, угадал ли критик имена. Концептуально это смешение ролей: критик отвечает за **shape of missing layer**, аналитик — за **discovery**.

**Принцип (зафиксирован в промптах + descriptions полей схемы):**

- `addFocusAreas` — только имена **слоёв/подсистем** (`"OSAT advanced packaging"`, `"photomask supply layer behind TSMC"`, `"UBO chain beyond registered owner"`). Имена вендоров / персон / режимов сюда **не пишутся** — это ответы, которые должен найти аналитик.
- `deepenEntityIds` — pack-level registry id'ы тех **поверхностных** сущностей, которые аналитик уже эмитнул (`analyzer:e3`, …). Поле существует именно для того, чтобы критик мог сказать «копай за ВОТ ЭТОЙ», не называя то, что глубже.
- `evidenceGaps` — **kinds** недостающего evidence (`"as-of date"`, `"primary-filing URL"`, `"vendor identifiers in lithography sub-supplier layer"`), не конкретные факты вида «подтверди, что ZEISS SMT делает линзы».
- `directiveText` — структурный imperative, описывающий слой + surface для копания. Допускается **ровно один** `"e.g. <name>"` anchor для калибровки глубины — но он явно прочитывается аналитиком как anchor, не checklist.
- `domain_priors_lookup` теперь явно описан в инструкции критика как **gap detector** («есть ли пропущенный слой?»), а не как справочник, имена из которого можно копировать в `evidenceGaps`/`addFocusAreas`.

**Симметрия на стороне аналитика:** в `ANALYZER_OPERATING_MODE` явно прописано: любые proper nouns в `directiveText` (особенно с префиксом `"e.g."`) — **anchors for depth calibration**, не список к механическому emit'у. Аналитик обязан surface полную структуру слоя, а не только anchor. Если критик нарушил контракт и записал имена в `addFocusAreas` — аналитик извлекает подразумеваемый слой и эмитит полный набор всё равно.

**Файлы:**

- `agenthorde_common/prompts/instructions.py` — `CRITIC_INSTRUCTION` (раздел "recommendedScopeFocus is structural, not enumerative" + переформулировка `domain_priors_lookup` как gap detector); `ANALYZER_OPERATING_MODE` (раздел "Reading the directive — names are anchors, not a checklist").
- `agenthorde_common/schemas/agents_io.py` — `RecommendedScopeFocus` field descriptions (`add_focus_areas`, `deepen_entity_ids`, `evidence_gaps`, `directive_text`) с явными good/bad примерами.

**Зачем:** убрать зависимость качества Tier-2/Tier-3 покрытия от способности конкретной модели в роли критика «угадать» имена. Структурный directive переносит работу обратно туда, где она должна быть, — в research-фазу аналитика.

### DONE: critic — domain-typed gap classification (MVP1) + naming drift

**Контекст:** до этой итерации критик оперировал универсальной «coverage discipline» (depth walking, regime distinctness, SPOF taxonomy, …) без явной таксономии классов недостатков. Findings было невозможно надёжно роутить по доменам без re-parse прозы, а сам набор проверок не отражал того факта, что **классы graph-incompleteness — доменно-зависимые**: «не хватает Tier-2 за вендором» — это `supply_chain.tier_resolution`, «не хватает UBO за registered owner» — это `governance.owner_vs_ubo`, и эти два класса требуют **разных** сигналов, разной дисциплины, разных tools. Унификация в одном generic «node depth» теряла эту разницу.

Параллельно — нужно было сменить терминологию `blocker` / `nonBlocker` (контрактно-юридическая коннотация) на нейтральное `finding` (research-коннотация). Это снижает риск ложных срабатываний внешних safety-фильтров на нейтральных рассуждениях о supply chain / governance.

**Что сделано:**

1. **`GapClass` enum** (`agenthorde_common/schemas/agents_io.py`) — 5 семейств кодов с phylo-стилем prefix:
   - `generic.*` — доменно-нейтральная graph-quality таксономия (`node_depth`, `node_breadth`, `edge_existence`, `edge_semantics`, `node_attribution`, `resolution`, `cross_subgraph`, `counterfactual`, `conditional`, `confidence`, `node_tier_jump`).
   - `supply_chain.*` — supply-chain-specific (`tier_resolution`, `product_on_edge`, `transformation_process`, `process_granularity`, `mode_route`, `capacity_share`, `substitutability`, `lead_time`, `inter_tier_control`).
   - `governance.*` — corporate-control specific (`owner_vs_ubo`, `layering`, `nominee`, `voting_vs_economic`, `affiliation`, `interlocking_directorates`, `jurisdiction_incorporation`, `trustee_spv`, `proximity_to_listed`).
   - `compliance.*` — regulatory-regime specific (`regime_distinctness`, `designation_lifecycle`, `license_exemption`, `conditional_applicability`, `secondary_effect`, `territorial_scope`, `carve_out`, `enforcement_body`, `wind_down`).
   - `methodology.*` — про процесс аналитика, а не про объект графа (`scope_adequacy`, `execution_fidelity`, `framing_bias`, `composition_inconsistency`).
2. **`CriticFinding`** (бывш. `CriticBlocker`) — обязательное поле `gap_class` (alias `gapClass`). Validator: пустой `refs` разрешён только для `methodology.*`; для всех прочих классов finding обязан указывать на объект из object registry.
3. **`CriticVerdict.criticalFindings` / `advisoryFindings`** — заменили `blockers` / `nonBlockers`. Симметрия по форме сохранена: единственная разница — материальность для пользовательского вопроса, не класс incompleteness.
4. **`CRITIC_INSTRUCTION`** реструктурирован — раздел «Coverage discipline» переписан как **Gap typology** с пятью секциями A–E (по prefix'ам). Cross-cutting checks (domain-priors как gap detector, named-entity coverage, quantitative gap, SPOF decomposition) явно прописаны как **триггеры**, которые приземляются под нужный `gapClass`. `recommendedNextAgent` теперь явно роутит **по доминирующему `gapClass` prefix'у**, а не по prose-эвристике.
5. **`SUPERVISOR_INSTRUCTION`** — Priority-2 теперь сначала смотрит на dominance `gapClass` prefix'ов в `criticalFindings`, и только потом fallback на `refs` prefix-эвристику.
6. **A/B log** (`agenthorde_common/critic/ab_log.py`) — поля `critical_findings_count` / `advisory_findings_count` (бывш. `blockers_count` / `non_blockers_count`).
7. **Bench-смок-скрипты** (`bench_full_loop_smoke.py`, `bench_critic_models.py`) — обновлены под новые имена, добавлен критерий «every critical finding has gap_class» и распечатка `gap_class_prefixes` для визуального аудита распределения по доменам.

**Контракт без обратной совместимости:** runtime не в проде, поэтому старые имена убраны полностью (никаких alias'ов на `blockers` или валидации без `gapClass`). Любой исторический JSONL критика старого формата не пройдёт валидацию — это намеренно: исторические бенч-результаты остаются в `bench/results/**` как фиксация состояния «до», но новый код их не парсит.

**Тесты:** `tests/test_schemas.py` — добавлены `test_critic_finding_requires_gap_class`, `test_critic_finding_methodology_allows_empty_refs`, `test_critic_finding_object_class_rejects_empty_refs`, `test_critic_finding_alias_round_trip`, `test_critic_verdict_go_with_critical_findings_rejected`. `tests/test_critic_fallback.py`, `tests/test_critic_ab_log.py` — переписаны под новые имена. Полный suite: 303 passed.

**Файлы:**

- `agenthorde_common/schemas/agents_io.py` — `GapClass` enum, `CriticFinding`, `CriticVerdict.criticalFindings/advisoryFindings`.
- `agenthorde_common/schemas/__init__.py` — экспорт `CriticFinding`, `GapClass`.
- `agenthorde_common/prompts/instructions.py` — `CRITIC_INSTRUCTION` (новые секции A–E, cross-cutting triggers, output contract с `gapClass`); `SUPERVISOR_INSTRUCTION` (Priority-2 + Priority-D с дисперсией по `gapClass` prefix).
- `agenthorde_common/critic/prompt.py` — терминологический drift в registry preamble + domain hints.
- `agenthorde_common/critic/ab_log.py` — поля счётчиков переименованы.
- `agenthorde/critic/fallback.py`, `agenthorde_azure/azure_critic.py` — обновлены readers.
- `scripts/bench_full_loop_smoke.py`, `scripts/bench_critic_models.py` — обновлены readers + добавлены новые сигналы.

### TODO: critic split (MVP2) — три доменных критика, один meta-критик

**Контекст:** MVP1 закладывает структурную основу (один gap_class на каждый finding ⇒ роутинг по prefix dominance), но физически критик остаётся монолитным — один промпт, одна модель, один проход. Это работает, пока размер `CRITIC_INSTRUCTION` укладывается в context+attention бюджет модели и пока модель способна одновременно держать в голове все пять gap-классов и не «дрейфовать» к одному из них.

**Что хотим в MVP2:**

1. **Декомпозиция критика по prefix'у gap_class:**
   - `supply_chain_critic` — фокус на `supply_chain.*` + смежные `generic.*` (`resolution`, `node_depth` для supply graph).
   - `governance_critic` — фокус на `governance.*` + смежные `generic.*`.
   - `compliance_critic` — фокус на `compliance.*` + relevant `generic.*`.
   - `methodology_critic` — фокус на `methodology.*` (framing bias, scope adequacy, execution fidelity) — это **отдельная роль**, ближе к red-team, чем к domain expert.
2. **Meta-критик** агрегирует findings от доменных критиков, разрешает конфликты между ними (например, `supply_chain` хочет копать глубже, `compliance` считает, что это уже out-of-scope), и выдаёт итоговый `CriticVerdict` (обратно-совместимый по схеме).
3. **Per-критик model selection** — supply chain и governance могут идти на одной модели (lighter reasoning, structured-heavy), compliance — на модели с более широким legal/regulatory training, methodology — на сильной reasoning-модели или dedicated red-team.
4. **Параллельное исполнение** доменных критиков (как мы сейчас умеем для analyzer fanout) — снижает walltime для multi-domain вопросов.

**Когда переходить:** когда (a) монолитный критик начнёт показывать «дрейф» (одна доменная семья findings регулярно вытесняет другие при одинаковом графе), или (b) появятся доменно-специфичные tool-наборы (e.g. compliance-specific lookup'ов), которые имеет смысл держать только в одном из критиков. До тех пор — MVP1 корректно и без overhead.

**Несовместимости с MVP2 в текущем коде сознательно нет:** `GapClass` prefix-структура и `recommendedNextAgent` routing уже спроектированы под этот split — meta-критик MVP2 будет агрегировать `CriticVerdict`'ы тех же типов, и supervisor увидит ту же выходную форму.

### Наблюдение (не TODO): `reasoning.effort=low` неэффективен для stealth-моделей через OpenRouter

В smoke-прогоне на `openrouter/elephant-alpha` передача `extra_body={"reasoning":{"effort":"low"}}` в `chat.completions` **не сократила** thinking-бюджет analyzer'а — вместо ожидаемого ускорения латентность выросла (с 16с до 158с) и снова упёрлась в truncation. Гипотезы: либо провайдер игнорирует параметр для этой модели, либо elephant сам диктует свой бюджет thinking. Полезно знать при выборе тактики ускорения reasoning-моделей: эффорт не всегда работает «снаружи», иногда нужен role-shape (см. идею гибрида в TODO-1 — короткий критик на mini поверх глубокого аналитика на reasoning-модели).
