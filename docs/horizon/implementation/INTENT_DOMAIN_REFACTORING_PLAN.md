# План рефакторинга: Intent vs Domain Execution Contract

## Статус

**В работе** — Phases 0–6, 8 выполнены. Phase 7 (миграция данных) — отложена.

## Сводка фаз

| Фаза | Название | Статус | Ключевой результат |
|------|----------|--------|-------------------|
| 0 | Подготовка | ✅ | Инвентаризация, миграционный документ, feature flag |
| 1 | Lens model | ✅ | domainModes, intents в Lens; effectiveDomainModesForLens |
| 2 | Intent resolution | ✅ | ensureJobDomainMode: lens.DomainModes, fallback ViewConfig |
| 3 | Убрать domain как ноду | ✅ | DomainAsConfigEnabled → no-op для /domain/ job |
| 4 | Упростить domain expansion | ✅ | Skip domain expansion при flag |
| 5 | Job state | ✅ | domainModes из lens при создании job |
| 6 | Domain в scope | ✅ | Skip jobs для /domain/ в scope при flag |
| 7 | Cleanup | ⏸ | Удалить domain fragments/edges; миграция данных — отложено |
| 8 | Тесты и документация | ✅ | domain_modes_test.go, runtime_flags_test.go |

## Цели

1. **Разделить Intent и Domain** — Intent = живой вопрос (много на линзе), Domain = execution contract (правила, один или composite на линзу).
2. **Domain не нода** — убрать domain как сущность из графа; domain в scope линзы = конфиг, не фрагмент.
3. **Domain contract не порождает сущности** — убрать entity spawning из domain expansion; scope и child jobs — только из lens scope и discovered relations.
4. **Хранение** — domainModes и intents в Lens (без новых коллекций).

---

## Фазы исполнения

### Phase 0: Подготовка и инвентаризация

**Цель**: Зафиксировать текущее состояние, зависимости, точки входа.

| Задача | Описание | Файлы/область |
|--------|----------|---------------|
| 0.1 | Инвентаризация всех мест, где создаются/читаются domain fragments | `engine_ext.go` (executeDomainExpansionJob), `fragments` repo, `lens_artifacts.go`, `domain_answers.go` |
| 0.2 | Инвентаризация domain-domain и domain-entity relations | `engine_ext.go`, `dependency_edges` repo, DOMAIN relation types (FOCUSES_ON, INVOLVES_ACTOR, etc.) |
| 0.3 | Инвентаризация lens scope с /domain/ путями | `lens/service.go`, `engine_ext.go` (contractTypesFromLensScope), CreateLens/UpdateLens |
| 0.4 | Документ миграции данных | Какие lenses имеют domains в scope; какие fragments с Type=Domain; какие edges с domain source/target |
| 0.5 | Feature flag для поэтапного rollout | `ScoutComposablePipelineEnabled`-подобный флаг для нового поведения |

**Результат**: Документ `INTENT_DOMAIN_REFACTORING_INVENTORY.md` с полным списком затронутых мест.

---

### Phase 1: Lens model — domainModes и intents

**Цель**: Добавить в Lens поля для domain modes и intents; сохранить обратную совместимость.

| Задача | Описание | Детали |
|--------|----------|--------|
| 1.1 | Добавить `Lens.DomainModes []string` | Прямое поле или `ViewConfig["domainModes"]`. Приоритет: явное поле. |
| 1.2 | Добавить `Lens.Intents []Intent` (или `ViewConfig["intents"]`) | Структура: `{ id?, question, createdAt?, status? }`. Начать с массива в ViewConfig. |
| 1.3 | Repurpose `ViewConfig["domains"]` | Оставить для backward compat при чтении; при записи — маппить в domainModes + intents. Или deprecated. |
| 1.4 | Обновить Lens Create/Update API | Принимать domainModes, intents; при intent resolution — писать оба. |
| 1.5 | Миграция существующих lenses | Скрипт: для каждого lens с `domains` в ViewConfig → извлечь contractTypes → DomainModesForContractTypes → записать domainModes; intents — из domains как вопросы (title/description). |

**Результат**: Lens поддерживает domainModes и intents; старые lenses мигрированы.

---

### Phase 2: Intent resolution — разделить вывод

**Цель**: Intent resolution возвращает intents и contract types; маппинг contractType → domain mode — отдельно.

| Задача | Описание | Детали |
|--------|----------|--------|
| 2.1 | Расширить вывод IntentResolver | Возвращать `Intents []Intent` (вопросы) и `ContractTypes []string` (для маппинга в domain modes). |
| 2.2 | Маппинг при создании lens | contractTypes → DomainModesForContractTypes → Lens.DomainModes; intents → Lens.Intents. |
| 2.3 | Убрать создание /domain/ путей в scope из intent resolution | Intent resolution больше не добавляет `/domain/v1/...` в scope. Scope = только anchors (org, market, fac, etc.). |
| 2.4 | Domain в scope — только как конфиг | Если пользователь явно выбирает "domain" (contract) — хранить domainModes, не путь в scope. Scope остаётся для реальных сущностей. |

**Результат**: Intent resolution не смешивает intent и domain; scope без /domain/ путей.

---

### Phase 3: Убрать domain как ноду и связи

**Цель**: Domain не создаётся как fragment; domain-domain и domain-entity edges не создаются.

| Задача | Описание | Детали |
|--------|----------|--------|
| 3.1 | Прекратить создание domain fragments | В `executeDomainExpansionJob` — не вызывать `fragmentsRepo.Create/Update` для domain. Удалить блок создания/обновления domain fragment (строки ~2191–2284 в engine_ext.go). |
| 3.2 | Прекратить создание domain-domain relations | Не материализовать FOCUSES_ON, INVOLVES_ACTOR, RELATED_DOMAIN, AFFECTS и т.д. с source/target = /domain/... |
| 3.3 | Прекратить создание domain-entity relations | Не создавать edges от domain к first-order entities. First-order entities будут добавляться в scope явно (пользователь или другой механизм). |
| 3.4 | Обработка job с target /domain/ | Job с targetCanonicalPath = /domain/... больше не создаётся. Или: если в scope остаётся /domain/ (временно) — такой job завершать no-op, не создавая fragment. |
| 3.5 | Обновить lens scope handling | При CreateLens/UpdateLens — фильтровать /domain/ из scope или не добавлять. Scope = только реальные сущности. |
| 3.6 | Обновить UI/API | Не показывать domain как ноды в графе; не создавать jobs для /domain/ в scope. |

**Результат**: Domain не существует как нода; граф без domain fragments и domain edges.

---

### Phase 4: Упростить domain expansion — убрать entity spawning

**Цель**: Domain contract = только правила. Не порождать first-order entities и child jobs.

| Задача | Описание | Детали |
|--------|----------|--------|
| 4.1 | Убрать domain research из domain expansion | Не вызывать `EnrichFragmentWithGrounding` для domain с "Expand into 5-10 first-order entities". |
| 4.2 | Убрать создание child jobs из first-order entities | domain expansion не создаёт scouting jobs для first-order. |
| 4.3 | Источник scope и root jobs | Только lens scope (явные anchors) и, при необходимости, intent resolution — но без /domain/ путей. Root jobs = по путям из scope. |
| 4.4 | Domain expansion job — упростить или удалить | Вариант A: удалить полностью — job с target /domain/ не создаётся. Вариант B: оставить как no-op или только для вычисления domainModes (если path ещё где-то есть). Рекомендация: удалить. |
| 4.5 | ensureJobDomainMode — источник domainModes | Брать из `Lens.DomainModes` (через job state при создании job). Убрать contractTypesFromLensScope для /domain/ путей. |

**Результат**: Domain contract не порождает сущности и jobs; domainModes приходят из lens.

---

### Phase 5: Job state и pipeline — domainModes из lens

**Цель**: Job получает domainModes при создании из lens; ensureJobDomainMode — fallback.

| Задача | Описание | Детали |
|--------|----------|--------|
| 5.1 | При создании job — передавать domainModes из lens | `lens/service.go` CreateLens/UpdateLens: `state["domainModes"] = lens.DomainModes`. |
| 5.2 | ensureJobDomainMode — приоритет lens | Если `job.State["domainModes"]` уже задан (из lens) — не перезаписывать. Fallback: specialties, общий general. |
| 5.3 | Убрать contractTypesFromDomainsRaw из ensureJobDomainMode | Или оставить только для backward compat при миграции. После миграции — удалить. |
| 5.4 | Убрать contractTypesFromLensScope для /domain/ | Scope больше не содержит /domain/; эта ветка не нужна. |
| 5.5 | copyDomainModeStateToChild | Оставить — child jobs наследуют domainModes от parent. |

**Результат**: domainModes течёт из lens в job при создании; ensureJobDomainMode — упрощён.

---

### Phase 6: Обработка domain в scope линзы

**Цель**: Domain в scope = конфиг для правил, не нода. Обрабатывать без создания fragment.

| Задача | Описание | Детали |
|--------|----------|--------|
| 6.1 | Scope без /domain/ путей | При создании/обновлении lens — не добавлять /domain/ в scope. Или: хранить отдельно `Lens.DomainContractRefs` (для отображения "какие контракты") без создания jobs. |
| 6.2 | Если scope исторически содержит /domain/ | При загрузке lens — фильтровать /domain/ при передаче в job creation. Не создавать jobs для них. |
| 6.3 | Domain answers / domain snapshot | `domain_answers.go`, `RunDomainAnswer` — пересмотреть. Domain snapshot может быть привязан к lens + domainMode, не к domain fragment. |
| 6.4 | Lens artifacts для domain | `lens_artifacts.go` — убрать логику для domain paths как fragment. |

**Результат**: Domain в scope обрабатывается как конфиг; нет jobs и fragments для domain.

---

### Phase 7: Cleanup и миграция данных

**Цель**: Удалить domain fragments и edges; финальная миграция.

| Задача | Описание | Детали |
|--------|----------|--------|
| 7.1 | Миграция: удалить domain fragments | Скрипт: найти все fragments с `Type=Domain` или `CanonicalPath` начинается с `/domain/`; удалить или архивировать. |
| 7.2 | Миграция: удалить domain edges | Скрипт: найти edges где source или target — domain path; удалить. |
| 7.3 | Обновить lens.Scope | Удалить /domain/ пути из scope существующих lenses. |
| 7.4 | Обновить ScopeFragments | Удалить записи для domain paths. |
| 7.5 | Удалить мёртвый код | executeDomainExpansionJob (или свести к no-op), domain fragment create/update, domain relation materialization, bestEffortRewriteLensScope для domain. |
| 7.6 | Taxonomy/paths | Оставить валидацию /domain/ путей для backward compat (если где-то остались ссылки); пометить deprecated. |

**Результат**: Чистый граф без domain нод и связей; код без domain expansion entity spawning.

---

### Phase 8: Тесты и документация

| Задача | Описание |
|--------|----------|
| 8.1 | Обновить тесты | Убрать тесты на domain fragments/edges; добавить тесты на domainModes из lens, intent handling. |
| 8.2 | Обновить документы | `intent-vs-domain-execution-contract.md`, `SCOUT_DOMAIN_PHASES`, roadmap. |
| 8.3 | API changelog | Описать изменения в Lens API, поведение scope, domainModes, intents. |

---

## Порядок выполнения (рекомендуемый)

```
Phase 0 (подготовка)
    ↓
Phase 1 (Lens model) + Phase 2 (Intent resolution) — можно параллельно
    ↓
Phase 5 (Job state) — зависит от Phase 1
    ↓
Phase 3 + Phase 4 (убрать domain node + entity spawning) — вместе
    ↓
Phase 6 (обработка domain в scope)
    ↓
Phase 7 (cleanup, миграция данных)
    ↓
Phase 8 (тесты, документация)
```

---

## Риски и митигация

| Риск | Митигация |
|------|-----------|
| Существующие lenses с domain в scope | Phase 1 миграция: domainModes из domains; scope очистить от /domain/ в Phase 7 |
| Domain answers / snapshots зависят от domain fragment | Phase 6.3: перепривязать к lens + domainMode |
| Propagation, policy_impact используют domain | Проверить `domain_adapter.go`, `scenario_runner` — они используют domainModes, не domain path. Оставить как есть. |
| UI ожидает domain ноды | Phase 6–7: не создавать; UI не получит domain nodes. Обновить UI. |

---

## Зависимости (что не трогать)

- **Domain mode logic** (allowlist, strategies, allowlist) — сохранить. Меняется только источник (lens вместо domains/scope).
- **Propagation domain adapters** — используют domainModes, не domain path. Без изменений.
- **Taxonomy DomainModesForContractTypes** — оставить для маппинга intent → domain mode.
- **FragmentTypeDomain** — можно оставить в enum для backward compat; не создавать новые.

---

## Включение

```bash
export HORIZON_DOMAIN_AS_CONFIG=true
```

При включении:
- Scope при CreateLensFromQuery не содержит /domain/ путей
- Jobs для /domain/ в scope не создаются
- Job с target /domain/ завершается no-op (без domain expansion)
- domainModes и intents заполняются из intent resolution

---

## Связанные документы

- [Intent vs Domain Execution Contract](../architecture/intent-vs-domain-execution-contract.md)
- [SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ](./SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ.md)
- [Roadmap](./roadmap.md)
