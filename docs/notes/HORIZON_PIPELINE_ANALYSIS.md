# Анализ пайплайна Horizon: Scout → Critic → Research → Policy

## Обзор пайплайна

Пайплайн состоит из следующих этапов:

1. **Scout (Extraction)** — извлечение структурированных данных из источников
2. **Critic (Validation)** — валидация извлеченных данных и определение missingInfo
3. **Research** — дообогащение данных на основе missingInfo от Critic
4. **Conflict Resolution** (опционально) — разрешение конфликтов между Scout и Critic
5. **Metrics** — вычисление метрик риска
6. **Persistence** — создание/обновление фрагментов и рёбер
7. **Policy Check** (финальный, best-effort) — проверка всех созданных/измененных сущностей

**Важно:** Intent Resolution (`IntentResolverAgent`) выполняется ДО пайплайна (в `lenses_from_query.go`) — это понимание намерения пользователя и расположен правильно.

---

## 🔴 Реальные логические проблемы

### 1. Conflict Resolution выполняется ДО Research

**Проблема:**
```go
// engine.go:2644-2686 — Resolution ДО Research
if validationResult.HasConflicts {
    resolutionResult, err := e.resolverAgent.Resolve(ctx, agents.ResolutionRequest{
        ScoutData:       extractedFragment,  // данные ДО Research
        CriticArguments: validationResult.Conflicts,
        SourceContent:   aggregatedContent,
    })
}

// engine.go:2543 — Research ПОСЛЕ Resolution
enrichedFragment, newRelations, newSupplyChains, err := e.researchAgent.EnrichFragment(...)
extractedFragment = enrichedFragment
```

`ResolverAgent` — это **Conflict Resolution** (арбитр между Scout и Critic), а не Intent Resolution. Он работает с неполными данными, потому что Research ещё не отработал.

**Рекомендация:** Переместить Conflict Resolution после Research.

---

### 2. Research не валидируется повторно через Critic

```go
// engine.go:2543
enrichedFragment, newRelations, newSupplyChains, err := e.researchAgent.EnrichFragment(...)
// Данные сразу идут в persistence без повторной проверки качества
```

Research может добавить некорректные данные или relations, которые не проверяются на соответствие источникам. Новые relations от Research обрабатываются наравне с Scout-relations, хотя их provenance отличается.

**Рекомендация:** Добавить одну повторную валидацию через Critic после Research (без бесконечного реитерирования, максимум 1-2 итерации).

---

### 3. Policy применяется инконсистентно

Policy применяется в 3 разных местах с разной логикой:

1. **При создании рёбер** (`edge_materializer.go:225`, `dependency_edges.go:138`) — может reject/reverse/penalty, non-blocking
2. **При создании фрагментов** (`precreate_validator.go:107`) — может reject/penalty/allow, non-blocking
3. **Финальная проверка** (`engine.go:3454`, `runPolicyCheck`) — проверяет все созданные сущности, non-blocking

Все три точки — best-effort (и это правильно), но логика применения отличается. Например, reject при создании edge просто логируется, а в repo-уровне — тоже логируется, но с другими побочными эффектами.

**Рекомендация:** Унифицировать логику применения Policy во всех точках (одинаковые action handlers), сохраняя best-effort подход.

---

### 4. `recordDeterministicRedirect` вызывается не во всех точках изменения path

Redirects записываются через `RecordObservation()` в коллекцию `canonical_path_redirects` и применяются в **Step 0** (`applyRedirects()`) при каждом `Resolve()` — это позволяет быстро разрешать пути без повторных дорогих операций. `RecordDeterministicRedirect()` использует `warmupThreshold=1` (стабильность сразу).

Но вызывается не везде:
- Research upgrade: ✅
- CANONICAL RETARGET: ✅
- ORG LEGACY: ✅
- Market deduplication: ❌
- Pre-create validation: ❌

**Последствия:** Теряется возможность быстрого разрешения для некоторых изменений — при повторном resolve будет выполняться полная цепочка нормализации.

**Рекомендация:** Всегда вызывать `recordDeterministicRedirect()` при изменении path, или встроить в `bestEffortRewriteLensScope()`.

---

## ⚠️ Потенциальные улучшения

### 5. Множественные изменения canonicalPath в одном пайплайне

CanonicalPath может изменяться в нескольких местах:
1. Early canonicalization (`engine.go:1673-1715`)
2. Research upgrade (`engine.go:2556-2575`)
3. CANONICAL RETARGET (`engine.go:2744-2794`)
4. ORG LEGACY CANONICALIZATION (`engine.go:2796-2851`)
5. Market deduplication (`engine.go:2946-2973`)
6. Pre-create validation (`engine.go:2975-3003`)

Каждое изменение вызывает `bestEffortRewriteLensScope()` → множественные обновления одной линзы.

**Рекомендация:** Рассмотреть накопление изменений и применение одним вызовом в конце пайплайна (originalPath → finalPath).

---

### 6. aliasCanonicalPath сохраняет только первый alias

```go
if _, ok := job.State["aliasCanonicalPath"]; !ok && old != "" {
    job.State["aliasCanonicalPath"] = old
}
```

При цепочке `/org/tesla` → `/org/us/tesla` → `/org/us/tesla-inc` сохраняется только `/org/tesla`, промежуточный `/org/us/tesla` теряется.

**Рекомендация:** Сохранять массив aliases.

---

### 7. Budget checks только перед Research

```go
// engine.go:2418-2440
if e.budgetPolicy.Enabled && e.spendLedger != nil {
    if cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
        goto SKIP_RESEARCH
    }
}
```

Scout и Critic могут потратить бюджет до проверки. Для MVP это допустимо, но при масштабировании стоит добавить проверки раньше.

---

## ✅ Что сделано хорошо

1. **Fail-open стратегия** — система не падает при ошибках отдельных этапов
2. **Best-effort Policy** — Policy не блокирует пайплайн, только аудит + опциональный enforcement
3. **Tracking missingInfo** — `missingInfoStatusRepo` отслеживает попытки поиска, предотвращает бесконечные циклы
4. **Budget controls** — защита от перерасхода бюджета на уровне cascade и lens
5. **Canonicalization** — несколько уровней нормализации путей
6. **Телеметрия redirects** — `applyRedirects()` в Step 0 позволяет быстро разрешать пути без повторных дорогих операций
7. **recordDeterministicRedirect** — `warmupThreshold=1` для немедленной стабильности при детерминистических решениях
8. **Session Graph** — in-memory кэш решает проблему Vertex Search indexing delay и предотвращает дубликаты внутри job (кросс-job дедупликация — задача `canonicalPathResolver` и БД)
9. **Deduplication** — Session Graph + canonicalPathResolver + orgDeduplicator
10. **Explicit source handling** — поддержка Phase 2 strategic relations с явным указанием source
11. **Обновление всех затронутых линз** — `bestEffortRewriteLensScope()` обновляет до 200 линз с retry для primary lens
12. **Защита от циклов redirects** — `applyRedirects()` проверяет `seen` map и ограничивает цепочку до 4 шагов

---

## 📋 Рекомендации по улучшению (приоритизированные)

### Приоритет 1 (Реальные проблемы)
1. **Переместить Conflict Resolution после Research** — работает с неполными данными
2. **Добавить одну повторную валидацию через Critic после Research** — без бесконечного реитерирования (макс 1-2 итерации)
3. **Унифицировать логику Policy** — одинаковые action handlers во всех точках, best-effort
4. **Всегда вызывать recordDeterministicRedirect при изменении path** — критично для производительности

### Приоритет 2 (Улучшения)
5. **Централизовать canonicalization** — накапливать изменения, применять один раз
6. **Сохранять историю aliases** — массив вместо одного значения
7. **Добавить проверки бюджета на ранних этапах** — при масштабировании

---

## Предлагаемый улучшенный порядок пайплайна

```
0. Intent Resolution (ДО пайплайна) → понимание намерения пользователя
   ↓
1. Scout (Extraction)
   ↓
2. Critic (Validation) → missingInfo + conflicts
   ↓
3. Research (Enrichment) → новые данные
   ↓
4. Critic (Re-validation, 1 итерация) → проверка новых данных
   ↓
5. Conflict Resolution (если есть конфликты) → работа с полными данными
   ↓
6. Metrics (вычисление метрик)
   ↓
7. Persistence (создание фрагментов/рёбер с Policy validation, best-effort)
   ↓
8. Policy Check (финальный аудит, best-effort)
   ↓
9. Enforcement (автоматическое исправление, best-effort)
```

---

## 🔍 Анализ логики canonical paths и синхронизации со scope линзы

### Текущая реализация

**Места изменения canonicalPath в пайплайне:**

1. **Early canonicalization** (`engine.go:1673-1715`) — через `canonicalPathResolver`
2. **Research upgrade** (`engine.go:2556-2575`) — Research может обновить path
3. **CANONICAL RETARGET** (`engine.go:2744-2794`) — финальная проверка перед persistence
4. **ORG LEGACY CANONICALIZATION** (`engine.go:2796-2851`) — апгрейд legacy org paths
5. **Market deduplication** (`engine.go:2946-2973`) — дедупликация через tags
6. **Pre-create validation** (`engine.go:2975-3003`) — нормализация через preCreateValidator

**Синхронизация со scope линзы:**

- `bestEffortRewriteLensScope()` вызывается после каждого изменения path
- Обновляет: `lens.Scope`, `lens.ScopeFragments`, `lens.ResolvedQueries`
- Также обновляет `LensArtifacts` через `UpdateCanonicalPath()`
- Находит и обновляет все линзы, содержащие старый путь (до 200)
- Записывает redirect через `recordDeterministicRedirect()` (для быстрого разрешения в будущем)
- Primary lens обновляется с retry (до 3 попыток)

---

## Дополнительные компоненты пайплайна

### Supply Chains

После обработки relations (`engine.go:3405-3411`). Парсит supply chains, создает фрагменты и рёбра для узлов цепочки. Critic валидирует supply chains при запрошенной specialty `supply_chain`.

### Domain Expansion

Отдельный пайплайн для DOMAIN фрагментов (`engine_ext.go:executeDomainExpansionJob`). Расширяет DOMAIN контракт в first-order entities через `IntentDecomposerAgent`. Автоматически запускает Phase 2 Strategic Overlay после завершения. Дедупликация обеспечивается Session Graph и `canonicalPathResolver`.

### Phase 2 Strategic Overlay

Автоматически запускается после Domain Expansion (`engine_ext.go:1733-1737`). Генерирует DECISION_POINT и MECHANISM сущности. Запускает Critic + Research для обогащения стратегических сущностей. `shouldRunStrategicOverlay` фактически всегда возвращает `true` (осознанный best-effort выбор).

### Decomposition

Через `processDiscoveredRelations()`. Для каждой relation создает или находит target fragment и при необходимости запускает новый scout job. Защита от бесконечной рекурсии: `maxRecursionDepth` + `cascadeVisitedRepo`.

### Session Graph

In-memory кэш (`engine.go:1484-1487`). Решает проблему Vertex Search indexing delay и предотвращает дубликаты внутри одного job. Кросс-job дедупликация — задача `canonicalPathResolver` и БД (это by design).

### Lens Artifacts

Финализированный snapshot созданных/измененных fragments и edges (`engine.go:3420`). Используется как вход для Phase 2 Strategic Overlay. Это намеренная архитектура, а не дублирование данных.

### Propagation Trigger

Асинхронное распространение изменений в графе (`engine.go:3457-3473`). Best-effort by design — ошибки логируются, но не блокируют пайплайн.

### High-Profile Recompute

Пересчет `highProfile` сигнала для затронутых фрагментов. Выполняется внутри асинхронной горутины `runPolicyCheck` после Policy/Enforcement.

### Policy Tracker + Edge Affect Collector

Отслеживание созданных/измененных сущностей в рамках job. Policy Tracker сохраняет IDs в `job.State`. Edge Affect Collector собирает affected fragment IDs при создании edges (thread-safe). Оба используются для финального Policy Check и High-Profile recompute.
