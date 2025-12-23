# QueryAddress - Динамическая адресация в Lens

## Обзор

QueryAddress — это специальный тип адреса, который позволяет динамически определять набор HorizonFragment через запросы на основе критериев классификации (sphere, sector, location, entityType). В отличие от статических canonical paths, QueryAddress разрешается в множество путей во время выполнения и автоматически обновляется при изменении онтологии.

## Проблема

Традиционные canonical paths (`/org/tsmc`, `/fac/tsmc/fab-21`) жестко привязаны к конкретным сущностям. Это создает ограничения:

1. **Невозможность выразить классы сущностей**: "все компании в секторе полупроводников", "все порты в Индонезии, Иране и Египте"
2. **Ручное управление**: При появлении новой сущности нужно вручную обновлять Lens
3. **Масштабируемость**: Сложно отслеживать динамически растущие категории сущностей

## Решение: QueryAddress

QueryAddress — это адрес формата `/query/{expression}`, который автоматически разрешается в список canonical paths на основе критериев фильтрации.

### Синтаксис

```
/query/{expression}

expression :=
  term
  | NOT expression
  | expression AND expression
  | expression OR expression
  | '(' expression ')'
```

**Поддерживаемые поля**:
- `sphere` или `spherePath` — путь в таксономии сфер (например, `/sphere/logistics/maritime`)
- `sector` или `sectorPath` — путь в отраслевой таксономии (например, `/Technology/Semiconductors`)
- `location` или `locationPath` — путь локации (например, `/loc/id` для Индонезии)
- `entityType` или `entity_type` — путь типа сущности (например, `/Infrastructure/Logistics/Seaport`)
- `canonicalPath` или `path` — прямое совпадение с canonical path

**Операторы**:
- `NOT` — отрицание
- `AND` — конъюнкция
- `OR` — дизъюнкция

**Приоритеты (как в “нормальном” языке запросов)**:
- `NOT` > `AND` > `OR`
- Скобки `(...)` задают явный приоритет

**Wildcard**:
- `**` — рекурсивное совпадение (например, `/Technology/Semiconductors/**` включает все подкатегории)

Примечание: для `sphere:` в текущей реализации поддерживается только **точное совпадение** (без `*`/`**`), потому что сферы хранятся как массив `spherePaths` у `ontology_facts`.

### Примеры

#### 1. Все компании в секторе полупроводников

```
/query/sector:/Technology/Semiconductors/**
```

Разрешается в:
```
/org/tsmc
/org/samsung/semiconductor
/org/intel
...
```

#### 2. Порты в Индонезии, Иране и Египте (OR логика)

```
/query/location:/loc/id/** OR location:/loc/ir/** OR location:/loc/eg/**
```

Разрешается в:
```
/port/jakarta-tanjung-priok
/port/tehran-bandar-abbas
/port/alexandria
...
```

#### 3. Морские логистические риски (AND логика)

```
/query/sphere:/sphere/logistics/maritime AND entityType:/Infrastructure/Logistics/Seaport
```

#### 4. Новые компании в полупроводниках с высоким риском

```
/query/sector:/Technology/Semiconductors/** AND sphere:/sphere/cyber
```

## Архитектура

### Компоненты

#### 1. QueryAddress Model (`query_address.go`)

```go
type QueryAddress struct {
    Raw                   string   // Исходная строка запроса
    SpherePaths           []string // Критерии sphere
    SectorPatterns        []string // Критерии sector (поддержка **)
    LocationPatterns      []string // Критерии location (поддержка **)
    EntityTypePatterns    []string // Критерии entityType (поддержка **)
    CanonicalPathPatterns []string // Критерии canonicalPath (поддержка **)
    Operator              string   // "AND" | "OR"
}
```

**Функции**:
- `IsQueryAddress(address string) bool` — проверяет, является ли строка QueryAddress
- `ParseQueryAddress(address string) (*QueryAddress, error)` — парсит синтаксис QueryAddress
- `MatchesPattern(value, pattern string) bool` — проверяет соответствие паттерну с поддержкой `**`

#### 2. QueryResolver Service (`query_resolver.go`)

Разрешает QueryAddress в список canonical paths через Firestore запросы.

```go
type QueryResolver struct {
    fragmentsRepo *HorizonFragmentsRepository
}

func (r *QueryResolver) Resolve(ctx context.Context, tenantID string, qa *QueryAddress) ([]string, error)
```

**Стратегия оптимизации**:
1. Использует Firestore фильтры для наиболее ограничивающих критериев
2. Применяет pattern matching (wildcard `**`) в памяти
3. Учитывает ограничения Firestore (только один `array-contains`, нет `OR`)

**Как это выполняется “по-взрослому”**:
- Парсится в AST (скобки/NOT/приоритеты).
- Для `OR` веток делается union кандидатов (несколько Firestore-запросов) и затем **полная фильтрация выражением в памяти**.
- `sphere:` резолвится через `ontology_facts` (по `spherePaths` → `canonicalPaths`) и применяется к фрагментам как “есть ли факт в этой сфере для canonicalPath”.

#### 3. Lens Model Extension

```go
type Lens struct {
    // ... existing fields ...

    // Scope: Mixed array of canonical paths AND QueryAddress
    Scope []string `json:"scope" firestore:"scope"`

    // ScopeFragments: Cache canonicalPath → fragmentID (optimization)
    ScopeFragments map[string]string `json:"scopeFragments,omitempty" firestore:"scopeFragments,omitempty"`

    // ResolvedQueries: Cache QueryAddress → []canonicalPath
    ResolvedQueries map[string][]string `json:"resolvedQueries,omitempty" firestore:"resolvedQueries,omitempty"`

    // LastResolved: Timestamp последнего разрешения QueryAddress
    LastResolved *time.Time `json:"lastResolved,omitempty" firestore:"lastResolved,omitempty"`
}
```

#### 4. LensService Integration

**CreateLens**:
1. Разделяет `scope` на статические пути и QueryAddress
2. Резолвит QueryAddress → canonical paths
3. Сохраняет результат в `ResolvedQueries`
4. Создает ScoutingJob и WatchdogConfig для всех resolved paths
5. Устанавливает `LastResolved` timestamp

**UpdateLens**:
1. Сравнивает старый и новый `scope`
2. Резолвит новые QueryAddress
3. Управляет подписками (increment/decrement activeSubscribers)
4. Обновляет кэш `ResolvedQueries`

**DeleteLens**:
1. Отписывается от всех canonical paths (включая resolved из QueryAddress)
2. Очищает кэш `ResolvedQueries`

#### 5. QueryRefreshJob (`query_refresh_job.go`)

Фоновый job для периодического переразрешения QueryAddress.

```go
type QueryRefreshJob struct {
    lensesRepo    *LensesRepository
    fragmentsRepo *HorizonFragmentsRepository
    queryResolver *QueryResolver
    interval      time.Duration // Default: 24h
}
```

**Функции**:
- `Start(ctx)` — запускает фоновый ticker для переразрешения всех активных Lens
- `refreshAll(ctx)` — переразрешает все QueryAddress во всех активных Lens
- `RefreshLens(ctx, lensID)` — переразрешает QueryAddress для конкретного Lens (ручной refresh)

**Периодичность**: 1 раз в день (configurable)

## Workflow

### Создание Lens с QueryAddress

```
User → CreateLens(scope: ["/org/tsmc", "/query/sector:/Technology/Semiconductors/**"])
  ↓
LensService.CreateLens()
  ↓
Parse scope → ["/org/tsmc"] + ["/query/sector:/Technology/Semiconductors/**"]
  ↓
QueryResolver.Resolve("/query/sector:/Technology/Semiconductors/**")
  ↓
Firestore query: WHERE sectorPath >= "/Technology/Semiconductors" AND sectorPath < "/Technology/Semiconductors\ufffd"
  ↓
In-memory pattern matching (check wildcards)
  ↓
Returns: ["/org/tsmc-semiconductor", "/org/samsung/semiconductor", "/org/intel"]
  ↓
Save to Lens.ResolvedQueries: {"/query/sector:/Technology/Semiconductors/**": [...]}
  ↓
Create ScoutingJob + WatchdogConfig for all resolved paths
  ↓
Set Lens.LastResolved = now
```

### Periodic Refresh (Background Job)

```
QueryRefreshJob (runs every 24h)
  ↓
List all active Lens with QueryAddress
  ↓
For each Lens:
  ↓
  QueryResolver.Resolve(each QueryAddress in scope)
  ↓
  Compare old vs new resolved paths
  ↓
  Update Lens.ResolvedQueries
  ↓
  Set Lens.LastResolved = now
  ↓
  (See roadmap: background refresh should manage subscriptions for added/removed paths)
```

**Note**: Полное управление подписками (создание/удаление WatchdogConfig и ScoutingJob) реализуется при явном UpdateLens. Background job только обновляет кэш.

### Manual Refresh

```
User → API call to refresh specific Lens
  ↓
QueryRefreshJob.RefreshLens(lensID)
  ↓
Similar to periodic refresh, but for single Lens
```

## Ограничения Firestore

### Проблемы

1. **Только один `array-contains` per query** — нельзя фильтровать по нескольким sphere paths одновременно
2. **Нет `OR` operator** — нужны множественные queries для OR логики
3. **Inequality только на одном поле** — нельзя `WHERE sectorPath >= X AND locationPath >= Y`

### Решение

**Hybrid approach**:
- Firestore используется для **наиболее ограничивающего** фильтра (sphere → sector → location → entityType → canonicalPath)
- **Pattern matching** (wildcard `**`, OR логика, дополнительные criteria) выполняется **в памяти**

**Пример**: `/query/sector:/Technology/** OR location:/loc/id/**`
- Firestore query 1: `WHERE sectorPath >= "/Technology" AND sectorPath < "/Technology\ufffd"`
- Firestore query 2: `WHERE homeLocation.iso3166_1 == "id"`
- Merge + deduplicate results in-memory

## Performance

### Оптимизации

1. **Firestore indexes**: Composite indexes для (tenantId, sectorPath, status), (tenantId, entityPath, status)
2. **Caching**: `ResolvedQueries` хранит результаты в Firestore (не пересчитывается при каждом чтении)
3. **Background refresh**: Переразрешение 1 раз в день вместо real-time
4. **In-memory pattern matching**: Минимизирует Firestore queries

### Оценки

**Сценарий**: Lens с `/query/sector:/Technology/Semiconductors/**`
- Firestore query: 1 запрос (range query на sectorPath)
- In-memory matching: ~100-1000 fragments проверяются на wildcard
- Время: <100ms для ~1000 fragments

**Сценарий**: Lens с `/query/sphere:/sphere/logistics/maritime OR sphere:/sphere/cyber`
- Firestore queries: 2 запроса (array-contains для каждого sphere)
- In-memory merging: ~200-2000 fragments
- Время: <200ms для ~2000 fragments

## Примеры использования

### Use Case 1: Мониторинг всех компаний в секторе полупроводников

**Цель**: Автоматически отслеживать все компании в секторе `/Technology/Semiconductors`, включая новые.

**Lens.Scope**:
```json
{
  "scope": [
    "/query/sector:/Technology/Semiconductors/**"
  ]
}
```

**Результат**:
- При создании Lens: разрешается в список текущих компаний (TSMC, Samsung, Intel, ...)
- При появлении новой компании: фоновый job обновляет `ResolvedQueries`, добавляя новую компанию
- WatchdogConfig автоматически создается для новых компаний (при следующем UpdateLens)

### Use Case 2: Логистические риски в портах Индонезии, Ирана, Египта

**Цель**: Мониторить риски поставок через порты в странах с высоким геополитическим риском.

**Lens.Scope**:
```json
{
  "scope": [
    "/query/location:/loc/id/** AND entityType:/Infrastructure/Logistics/Seaport",
    "/query/location:/loc/ir/** AND entityType:/Infrastructure/Logistics/Seaport",
    "/query/location:/loc/eg/** AND entityType:/Infrastructure/Logistics/Seaport"
  ]
}
```

**Альтернативный синтаксис (OR)**:
```json
{
  "scope": [
    "/query/location:/loc/id/** OR location:/loc/ir/** OR location:/loc/eg/** AND entityType:/Infrastructure/Logistics/Seaport"
  ]
}
```

**Результат**:
- Разрешается в список всех портов в указанных странах
- Автоматически обновляется при добавлении новых портов в онтологию

### Use Case 3: Кибербезопасность технологических компаний

**Цель**: Отслеживать кибер-угрозы для компаний в технологическом секторе.

**Lens.Scope**:
```json
{
  "scope": [
    "/query/sphere:/sphere/cyber AND sector:/Technology/**"
  ]
}
```

**Результат**:
- Разрешается в список всех технологических компаний с меткой `/sphere/cyber`
- При появлении нового кибер-инцидента в онтологии (новый HorizonFragment с spherePath=`/sphere/cyber`), фрагмент автоматически добавляется в Lens

## Будущие улучшения

### 1. Real-time Refresh Triggers

Вместо periodic refresh (каждые 24h), триггерить переразрешение при:
- Создании нового HorizonFragment, который подходит под QueryAddress
- Изменении классификации (sectorPath, spherePaths) существующего HorizonFragment

**Реализация**: Firestore trigger на создание/обновление HorizonFragment → проверка соответствия активным QueryAddress → обновление `ResolvedQueries`

### 2. QueryAddress Validation API

Endpoint для preview разрешения QueryAddress до создания Lens:

```
POST /api/v1/horizon/query-preview
{
  "queryAddress": "/query/sector:/Technology/Semiconductors/**"
}

Response:
{
  "resolvedPaths": ["/org/tsmc", "/org/samsung/semiconductor", ...],
  "count": 42,
  "estimatedCost": "0.15 USD/day"
}
```

### 3. Advanced Query Syntax

- **Ranges**: `/query/metric:revenue>1000000000` (revenue > $1B)
- **Temporal**: `/query/validFrom:>2024-01-01` (созданные после 2024)
- **Negation**: `/query/NOT sector:/Energy/**` (исключить энергетику)

### 4. Query Optimizer

Автоматический выбор оптимального Firestore фильтра на основе кардинальности критериев:
- Анализ статистики по полям (сколько fragments в каждом sector/sphere)
- Выбор наиболее селективного фильтра первым

## Связь с другими компонентами

### Scout Engine
- ScoutingJob создается для каждого resolved canonical path
- При переразрешении QueryAddress новые пути автоматически получают ScoutingJob

### Watchdog
- WatchdogConfig создается для каждого resolved canonical path
- При переразрешении QueryAddress новые пути автоматически получают WatchdogConfig

### Propagation Engine
- Распространяет изменения по graphу, включая узлы из resolved QueryAddress
- QueryAddress не влияет на алгоритм распространения (работает с canonical paths после резолва)

### OntologyFacts
- Не затрагивается напрямую (OntologyFacts создаются глобально)
- QueryAddress фильтрует HorizonFragment, а не OntologyFacts

## Заключение

QueryAddress решает проблему жесткой привязки Lens к конкретным сущностям, позволяя динамически определять scope через запросы. Это особенно важно для:
- Мониторинга растущих категорий сущностей (новые компании, инфраструктура)
- Гибкой аналитики по сферам влияния и секторам
- Снижения manual overhead при управлении Lens

Система автоматически обновляет resolved paths через фоновый job, обеспечивая актуальность данных без ручного вмешательства.
