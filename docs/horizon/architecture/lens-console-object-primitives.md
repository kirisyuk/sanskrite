# Lens Console Tool Surface And Execution Primitives

## Статус

Черновик архитектурного среза.

Пока **не реализовано** как полноценный LLM tool surface и execution broker для `Lens Console`.

Цель документа:
- зафиксировать, какими должны быть **LLM-facing tools** для консоли;
- отделить внешний смысловой tool surface от внутренних object-level execution primitives;
- предусмотреть **broker + WebSocket execution model**;
- зафиксировать, что новая система должна **переиспользовать** существующие Horizon contracts и pipeline code, а не строить параллельный runtime.

## Ключевая смена модели

Предыдущая формулировка делала акцент на мелких object primitives как на прямом surface для LLM:
- `lens.get_snapshot`
- `lens.find_nodes`
- `lens.materialize_fragment`
- `lens.plan_graph_patch`
- и т.д.

Эта модель полезна как внутренний execution layer, но плохо совпадает с тем, как реально мыслит оператор и как должен мыслить LLM.

Пользователь не думает так:
- "сначала materialize fragment"
- "потом merge duplicate"
- "потом patch edge qualifier"

Пользователь думает так:
- "посмотри, что уже есть в графе"
- "исследуй, чего не хватает"
- "собери нужный кусок цепочки"
- "покажи, что изменится"
- "примени"

Поэтому внешняя модель должна быть другой:

- **наружу** LLM получает небольшое число **смысловых tools**;
- **внутри** execution runtime сам раскладывает их на object primitives:
  - resolve
  - dedup
  - materialize
  - create
  - merge
  - patch
  - validate
  - dry-run
  - apply

То есть:

- **LLM-facing tools** должны называться по смыслу действия;
- **runtime primitives** должны называться по смыслу исполнения.

## Главный принцип

`Lens Console` должна уметь:

1. отвечать на вопросы о текущей линзе;
2. понимать намерение пользователя на уровне задачи;
3. исследовать и добирать недостающие данные;
4. собирать proposal изменений;
5. показывать preview;
6. применять изменения;
7. делать все это через bounded, explainable, reusable execution pipeline.

При этом LLM не должно быть обязано вручную управлять каждым внутренним шагом типа `materialize`, `merge`, `patch`.

## Что именно должен уметь LLM

На практическом уровне LLM должно уметь три вещи.

### 1. Понять текущее состояние графа

LLM должно иметь способ получить рабочую картину текущей линзы:
- какие объекты уже есть;
- какие есть связи;
- какие типы объектов присутствуют;
- что в фокусе;
- какие доменные режимы и ограничения уже активны;
- какие очевидные пробелы есть рядом с запросом.

Это не просто снимок canvas.
Это operational graph context.

### 2. Понять пользовательское намерение

LLM должно уметь различать:
- чистый вопрос;
- исследовательский запрос;
- построение нового graph slice;
- update существующего slice;
- controlled local expansion;
- destructive mutation.

То есть запрос:

`посмотри supply chain обуви в Караганде`

не означает один-единственный low-level tool.

Это означает:
- проверить, что уже есть;
- исследовать missing entities и relations;
- определить, что нужно переиспользовать, а что создавать;
- собрать proposal для графа;
- при необходимости применить.

### 3. Действовать через смысловые команды

Наружный tool surface должен быть composable, но не дробным.

LLM должно мыслить такими действиями:
- наблюдай;
- прочитай;
- найди;
- исследуй;
- собери;
- обнови;
- покажи diff;
- примени;
- сверься с интернетом.

А не такими:
- materialize relation;
- dedup candidate;
- patch qualifier.

Последнее должно быть responsibility execution runtime.

## LLM-facing tool surface

Ниже предлагается минимальный surface для модели.

Важно:
- без префикса `lens.*`;
- с названиями, близкими к смыслу;
- с сохранением места для низкоуровневых runtime terms вроде `apply` и `patch` там, где они реально нужны.

## 1. Read / comprehension tools

### `observe_graph`

Дает LLM компактную, но рабочую структуру текущего graph context.

Должен возвращать:
- materialized objects;
- основные связи;
- selection/focus;
- anchors;
- active filters and view constraints;
- object counts by type;
- detected notable gaps around current focus.

Это главный tool для понимания "что уже есть".

### `read_object`

Читает один конкретный объект или связь в нормализованном виде.

Нужен, когда LLM уже увидело candidate и хочет понять:
- что это за объект;
- какие у него свойства;
- какие привязки к fragment/canonical identity;
- какие метрики и qualifiers уже есть.

### `find_objects`

Поиск внутри текущего graph/lens context.

Используется для вопросов типа:
- "а есть ли уже обувные магазины в Караганде?"
- "есть ли уже поставщики батарей в этой линзе?"

Это bounded search по уже материализованному слою.

### `answer`

Чистый read-only answer path для chat-вопросов.

Опирается на:
- internal knowledge;
- prepared capsules;
- grounded evidence;
- conversation context;
- при необходимости bounded fallback search.

Это не mutation tool.

## 2. Research / enrichment tools

### `research`

Основной tool для controlled lookup outside current materialized graph.

LLM вызывает его, когда понимает:
- текущей линзы недостаточно;
- нужно найти missing entities;
- нужно понять, какие объекты и связи уже существуют в system knowledge;
- нужно boundedly дообогатить контекст.

Важно:
- `research` не должен автоматически запускать тяжелый full scout;
- он должен работать в bounded budget;
- он может использовать prepared internal knowledge, indexed graph lookup, lightweight scoped search.

### `web_search`

Используется, когда система должна явно свериться с интернетом.

Это отдельный tool по смыслу:
- не "исследуй что угодно";
- а "возьми внешний grounded web context".

Он должен возвращать:
- документы;
- snippets;
- citations;
- normalized search results;
- confidence / coverage hints.

### `ground_search_results`

Нужен, когда результаты `web_search` надо сопоставить с graph/system identity.

Это bridge между внешним интернет-контекстом и внутренним graph/runtime.

По смыслу:
- нашел кандидатов вовне;
- теперь свяжи это с тем, что система уже знает или может создать.

## 3. Composition / mutation tools

### `compose`

Главный orchestration tool для задач типа:
- "собери карту магазинов обуви в Караганде"
- "собери supply chain этого продукта"
- "добавь недостающих участников и связи"
- "обнови этот кусок графа на основе исследования"

Это **не apply**.

Это tool, который должен собрать structured graph proposal:
- что уже существует и будет переиспользовано;
- что будет materialized;
- что будет создано заново;
- что будет merged;
- какие связи и метрики будут добавлены/обновлены;
- какие элементы unresolved.

Именно `compose` должен скрывать от LLM детали типа:
- materialize vs create;
- merge vs reuse;
- patch vs create edge.

### `update`

Нужен для точечных правок существующего graph slice.

Примеры:
- "переименуй объект"
- "замени тип связи"
- "обнови роль участника"
- "исправь описание"

Это не composition целого нового slice, а bounded change поверх existing object/edge set.

### `update_metrics`

Отдельный tool для обновления метрик, qualifiers, weights, capacities, operational annotations.

Его полезно держать отдельно, потому что:
- метрики часто меняются независимо от topology;
- это позволяет не смешивать structural updates с numeric/operational edits;
- легче валидировать и preview.

### `preview_changes`

Показывает пользователю и LLM dry-run результата.

Обязательно должен возвращать:
- что будет created;
- что будет reused;
- что будет materialized;
- что будет merged;
- что будет updated;
- что будет deleted;
- warnings;
- unresolved.

### `apply_changes`

Применяет уже подготовленный and validated plan.

Это единственный tool, который должен делать реальную запись после preview/confirmation.

## Почему именно так

Эта модель совпадает с реальной когнитивной схемой:

1. `observe_graph`
2. `research`
3. `compose`
4. `preview_changes`
5. `apply_changes`

В ней:
- LLM мыслит на уровне задачи;
- runtime исполняет на уровне примитивов;
- пользователь взаимодействует с осмысленной системой, а не с объектным редактором по кускам.

## Внутренние execution primitives

Ниже то, что не обязательно должно быть отдельными tools для LLM, но обязательно должно существовать внутри runtime.

## 1. Graph observation primitive

Снять компактный operational snapshot текущей линзы:
- nodes;
- edges;
- focus;
- anchors;
- filters;
- local constraints;
- visible domain modes.

Это внутренний primitive для `observe_graph`.

## 2. Identity resolution primitive

Разрешить пользовательское mention или найденный candidate в:
- existing materialized object;
- existing fragment/canonical path;
- unresolved candidate set.

Это основной guardrail против дублей.

## 3. Candidate grounding primitive

В bounded режиме найти кандидатов:
- внутри system knowledge;
- внутри prepared internal knowledge;
- внутри lightweight scoped search;
- внутри grounded web search results.

Это внутренний слой для `research`.

## 4. Materialization primitive

Добавить в текущую линзу:
- existing fragment;
- existing relation;
- bounded neighborhood;
- route/path artifact;

без создания новой идентичности там, где объект уже существует.

## 5. Creation primitive

Создать truly new node/edge, когда identity resolution не нашел existing object.

Должен проходить через:
- canonical validation;
- taxonomy allowlists;
- bounded mutation limits.

## 6. Merge / dedup primitive

Если composition обнаруживает, что:
- есть materialized duplicate;
- найденный candidate совпадает с existing object;
- newly created node надо привязать к already known identity;

то merge/dedup должен происходить как внутренняя часть execution plan.

LLM не обязано знать об этом шаге как об отдельной команде.

## 7. Patch primitive

Применение whitelist updates к existing object/edge:
- labels;
- metadata;
- qualifiers;
- relation type;
- direction;
- state;
- selected metrics.

## 8. Plan / dry-run primitive

Сборка bounded execution plan:
- counts;
- actions;
- conflicts;
- unresolved;
- risk/warning summary.

Это внутренний слой для `compose`, `update`, `update_metrics`, `preview_changes`.

## 9. Apply primitive

Идемпотентное применение валидированного execution plan.

Это внутренний слой для `apply_changes`.

## Внешний tool surface и внутренний runtime

Главная архитектурная мысль:

- **LLM-facing tools** должны быть крупнее и ближе к смыслу;
- **runtime primitives** должны быть мельче и ближе к исполнению.

### Пример

Пользователь пишет:

`создай карту магазинов обуви в Караганде и поставок обуви в них`

LLM не должно вручную вызывать:
- `materialize`
- `merge`
- `patch edge`
- `create edge`

LLM должно сделать:

1. `observe_graph`
2. `research`
3. `compose`
4. `preview_changes`
5. `apply_changes`

А уже `compose` внутри:
- проверяет, что есть в графе;
- находит missing entities;
- резолвит identity;
- materialize/reuse existing fragments;
- создает missing nodes;
- прокидывает missing edges;
- добавляет route/path/metrics там, где это уместно;
- dedup/merge если нужно;
- возвращает structured proposal.

Именно это и есть правильный orchestration layer.

## Broker и WebSocket model

Новый surface не должен быть реализован как “LLM напрямую вызывает repo/HTTP handlers”.

Нужен явный execution broker.

## Почему нужен broker

Потому что у нас задачи такого рода:
- не всегда мгновенные;
- могут состоять из нескольких стадий;
- требуют progress reporting;
- должны уметь стримить preview/result/status в UI;
- должны жить совместимо с текущими pipeline patterns.

## Роль broker-а

Broker должен:
- принимать tool invocation request;
- нормализовать context:
  - tenant
  - project
  - lens
  - current graph state
  - current selection/focus
  - user/session
- вызывать нужный orchestrated execution pipeline;
- стримить промежуточные события;
- отдавать финальный structured result;
- хранить execution metadata для audit/debug/replay.

## WebSocket / realtime requirements

Через WS broker должен уметь пробрасывать:
- `tool.started`
- `tool.progress`
- `tool.preview`
- `tool.warning`
- `tool.requires_confirmation`
- `tool.completed`
- `tool.failed`

Это нужно, чтобы `Lens Console` умела:
- показывать живой ход исполнения;
- отображать intermediate findings;
- рендерить preview до apply;
- синхронно обновлять map/graph/docs surface после completion.

## Execution envelope

Каждый tool run должен жить в структурированном envelope:

```json
{
  "runId": "tool-run-123",
  "tool": "compose",
  "scope": {
    "tenantId": "t1",
    "projectId": "p1",
    "lensId": "l1"
  },
  "status": "running",
  "requiresConfirmation": false,
  "preview": null,
  "result": null,
  "warnings": [],
  "startedAt": "2026-04-01T10:00:00Z"
}
```

Это должно быть одинаково применимо:
- к быстрым tool calls;
- к multi-stage orchestration;
- к preview/apply cycle;
- к текущему real-time UI.

## Совместимость с pipeline логикой

Новая система должна быть не просто совместима с pipeline-ами, а обязана их **переиспользовать**.

## Что это значит

Нельзя делать:
- новый отдельный console runtime, который заново решает canonical validation;
- новый параллельный planner мимо graph-intent;
- новый CRUD мимо lens items и materialization contracts;
- новый scout runtime мимо уже существующих bounded scope actions.

Нужно делать:
- thin orchestration layer поверх текущих contracts;
- broker, который вызывает существующие execution primitives;
- новые functional tools, которые являются не новой логикой, а внешним semantic entrypoint в уже существующий runtime.

## Базовые reuse points в текущем коде

### Graph plan/apply

Уже есть:
- [backend/internal/http/horizon/graph_intent.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/http/horizon/graph_intent.go)
- [backend/internal/horizon/models/graph_intent.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/graph_intent.go)

Это должен быть основной reuse point для:
- `compose`
- `update`
- `update_metrics`
- `preview_changes`
- `apply_changes`

### Lens item CRUD / item-level mutations

Уже есть:
- [backend/internal/http/horizon/lens_items.go](/Users/kirisyuk/cursor/sanskrite/backend/internal/http/horizon/lens_items.go)

Это reuse point для:
- low-level materialization/create/update/delete primitives;
- execution internals, а не обязательно для LLM-facing tool names.

### Lightweight scope actions

Уже есть:
- `scope-scout`
- `scope-scout-query`
- `scope-scout-full`

Новая система должна использовать только bounded и explainable части этого слоя.

То есть:
- `research` может переиспользовать lightweight scoped query/explore paths;
- но не должен неожиданно эскалировать в тяжелый recursive job без явного policy.

### Cockpit materialization / node workflows

Уже есть в:
- [web/src/components/LensCanvasCockpit.tsx](/Users/kirisyuk/cursor/sanskrite/web/src/components/LensCanvasCockpit.tsx)

Новый runtime должен не дублировать этот смысл, а вытащить соответствующие примитивы в reusable execution layer.

## Mapping: внешний tool -> внутренние примитивы -> existing code

## `observe_graph`

Внутри:
- snapshot
- object counts
- current focus
- topology summary

Опора:
- existing lens graph snapshot logic
- graph-intent snapshot semantics
- cockpit graph state serialization

## `answer`

Внутри:
- internal knowledge retrieval
- bounded prepared-system fallback
- conversational context
- grounded synthesis

Опора:
- current lens chat read path
- internal knowledge warm state
- Vertex grounded search/synthesis

## `research`

Внутри:
- candidate grounding
- scoped bounded lookup
- optional web grounding
- identity hints

Опора:
- internal knowledge retrieval
- `scope-scout-query` style lightweight search
- search grounding and entity lookup

## `compose`

Внутри:
- observe
- resolve identity
- research missing pieces
- materialize/reuse existing fragments
- create missing nodes/edges
- merge/dedup
- attach route/path/metrics if needed
- produce execution plan

Опора:
- graph-intent planner
- lens item mutation paths
- materialization flows
- lightweight scope actions

## `update`

Внутри:
- resolve target
- validate patch
- build bounded update plan

Опора:
- graph-intent plan/apply
- lens item update paths

## `update_metrics`

Внутри:
- resolve object/edge
- validate metric patch
- preview numeric or qualifier changes

Опора:
- graph-intent or item-level patch primitives
- existing qualifier/metrics handling

## `preview_changes`

Внутри:
- dry-run plan rendering
- conflict/unresolved reporting
- counts and warnings

Опора:
- graph-intent plan
- runtime diff rendering layer

## `apply_changes`

Внутри:
- idempotent apply
- mutation budget enforcement
- result reporting
- WS event stream

Опора:
- graph-intent apply
- lens item mutation code
- broker execution envelope

## Что LLM давать нельзя

LLM в console не должен получать прямой surface для:

- `run_full_scout_job`
- `run_domain_contract_pipeline`
- `run_supply_chain_refine`
- `run_service_chain_refine`
- прямого batch pathbuilder materialization
- сырого repo CRUD
- обхода canonical validation

Причины:
- высокий blast radius;
- дорогой execution;
- низкая explainability;
- сложно показать preview;
- трудно удержать bounded operator workflow.

## Confirmation policy

Любой tool, который меняет graph, должен соблюдать:

- explicit dry-run;
- bounded mutation counts;
- structured unresolved output;
- confirmation requirement для destructive или крупных операций;
- idempotency key;
- audit trail через broker;
- WS progress + completion events.

## Минимальный контракт результата mutation tool

```json
{
  "applied": true,
  "createdNodeIds": [],
  "createdEdgeIds": [],
  "materializedNodeIds": [],
  "materializedEdgeIds": [],
  "mergedNodeIds": [],
  "updatedNodeIds": [],
  "updatedEdgeIds": [],
  "deletedNodeIds": [],
  "deletedEdgeIds": [],
  "warnings": [],
  "unresolved": []
}
```

## Рекомендуемый минимальный набор для v1

Если делать самый прагматичный первый набор, то LLM-facing surface должен быть таким:

1. `observe_graph`
2. `answer`
3. `research`
4. `compose`
5. `preview_changes`
6. `apply_changes`
7. `update`
8. `update_metrics`
9. `web_search`

Не обязательно все сразу shipping в UI.

Но именно это правильный target model.

## Рекомендуемый первый execution slice

Если запускать это поэтапно, правильный порядок такой:

### Phase 1

- `answer`
- `observe_graph`
- broker envelope
- WS events for read path

### Phase 2

- `research`
- bounded system lookup
- optional `web_search`
- candidate grounding

### Phase 3

- `compose`
- `preview_changes`
- `apply_changes`
- reuse graph-intent plan/apply

### Phase 4

- `update`
- `update_metrics`
- richer merge/materialization internals

## Итог

Правильная модель для `Lens Console` выглядит так:

- **для LLM**:
  - немного tools
  - названия по смыслу
  - действия уровня задачи

- **для runtime**:
  - object-level primitives
  - canonical validation
  - bounded budgets
  - dry-run/apply
  - broker + WS events
  - reuse existing pipelines and contracts

То есть консоль должна быть не объектным пинцетом для LLM и не магическим монолитным агентом, а смысловым orchestration surface над уже существующим Horizon runtime.

## Следующий шаг

Когда вернемся к реализации, правильная последовательность такая:

1. Выделить execution broker contract.
2. Описать WS event schema для tool runs.
3. Выделить reusable runtime primitives из существующих graph-intent / lens-items / scoped actions.
4. Поверх этого ввести LLM-facing tool surface:
   - `observe_graph`
   - `answer`
   - `research`
   - `compose`
   - `preview_changes`
   - `apply_changes`
5. Только после этого добавлять более богатые mutation/update scenarios.
