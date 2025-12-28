# Анализ реализации Policies в Horizon

## Дата анализа
2025-01-XX

## Резюме

Реализация policies в Horizon частично завершена. **PropagationPolicy** полностью интегрирована и используется, но **ValidationPolicy** и **CanonizationPolicy** определены, но не используются в реальной обработке данных.

---

## ✅ Полностью реализовано

### 1. PropagationPolicy

**Статус**: ✅ Полностью интегрирована и используется

**Место использования**: 
- `backend/internal/horizon/propagation/engine.go` (строки 449-495)
- Инициализация в `backend/cmd/server/main.go` (строки 1148-1153)

**Как работает**:
- Используется через `PolicyExecutor` при распространении метрик через edges
- Принимает решения о том, должна ли метрика распространяться через конкретный edge
- Учитывает: `metricName`, `relationType`, `currentDepth`, `isCriticalPath`
- Возвращает: `weight` (уже включает critical-path boost при необходимости), `maxDepth`, `dampingOverride`, `direction`

**Пример использования**:
```go
decision, err := e.policyExecutor.Execute(ctx, policy.PolicyInput{
    Kind: "propagation",
    Context: map[string]interface{}{
        "metricName":     metricName,
        "relationType":   string(edge.Type),
        "currentDepth":   it.hops,
        "isCriticalPath": isCriticalPath,
        "delta":          it.d,
    },
    Edge: edge,
})
```

**Телеметрия**: ✅ Записывается в Firestore через `FirestoreTelemetryWriter`

---

## ⚠️ Частично реализовано (определено, но не используется)

### 2. ValidationPolicy

**Статус**: ⚠️ Определена, но НЕ используется при создании edges

**Где определена**:
- `backend/internal/horizon/policy/validation_policy.go`
- `backend/internal/horizon/repo/validation_rules.go` (репозиторий для правил)

**Где должна использоваться, но не используется**:
1. `backend/internal/horizon/repo/dependency_edges.go::Create()` - создание edges через API
2. `backend/internal/horizon/scout/engine_ext.go::processRelations()` - создание edges в Scout
3. `backend/internal/http/horizon/edges.go::CreateEdge()` - HTTP endpoint для создания edges

**Что используется вместо ValidationPolicy**:
- **Hardcoded правила** в `backend/internal/horizon/scout/edge_normalizer.go`:
  - `ValidateEdgeDirection()` - проверка направления edges
  - `EdgeDirectionPolicies` - статический map с правилами
  - `NormalizeSymmetricEdge()` - нормализация симметричных edges
- **Hardcoded валидация** в `backend/internal/horizon/scout/precreate_validator.go`:
  - `PreCreateValidator` - валидация fragments перед созданием
  - Правила захардкожены в коде (блокировка `/discovered/` путей, проверка префиксов и т.д.)

**Проблема**: 
Правила валидации захардкожены в коде, что делает их:
- Невозможными для изменения без деплоя
- Не отслеживаемыми через телеметрию
- Не консистентными между разными местами создания edges

**Рекомендация**: 
Интегрировать `ValidationPolicy` через `PolicyExecutor` в:
- `DependencyEdgesRepository.Create()`
- `ScoutEngine.processRelations()`
- HTTP handlers для создания edges

---

### 3. CanonizationPolicy

**Статус**: ⚠️ Определена, но НЕ используется при нормализации данных

**Где определена**:
- `backend/internal/horizon/policy/canonization_policy.go`
- `backend/internal/horizon/repo/canonization_rules.go` (репозиторий для правил)

**Где должна использоваться, но не используется**:
1. `backend/internal/horizon/scout/canonicalizer.go::resolveCanonicalPath()` - нормализация путей
2. `backend/internal/horizon/scout/canonicalizer.go::normalizeFragmentType()` - нормализация типов
3. `backend/internal/horizon/scout/edge_normalizer.go::normalizeRelationEdge()` - нормализация edges
4. `backend/internal/horizon/scout/precreate_validator.go` - валидация и нормализация путей

**Что используется вместо CanonizationPolicy**:
- **Hardcoded функции** в `backend/internal/horizon/scout/canonicalizer.go`:
  - `normalizeFragmentType()` - статический switch для нормализации типов
  - `resolveCanonicalPath()` - сложная логика с hardcoded правилами
  - `normalizeLegacyGeoPath()` - специальная обработка legacy путей
- **Hardcoded нормализация** в других местах:
  - `normalizeRelationEdge()` - нормализация типов edges
  - `PreCreateValidator.expectedPathPrefix()` - статический map префиксов

**Проблема**: 
Нормализация захардкожена в коде, что делает её:
- Невозможной для настройки через правила в БД
- Не отслеживаемой через телеметрию
- Не консистентной между разными местами обработки

**Рекомендация**: 
Интегрировать `CanonizationPolicy` через `PolicyExecutor` в:
- `resolveCanonicalPath()` - для нормализации путей
- `normalizeFragmentType()` - для нормализации типов
- `normalizeRelationEdge()` - для нормализации типов edges
- `PreCreateValidator` - для нормализации путей перед созданием

---

## 📊 Сравнительная таблица

| Policy | Определена | Используется | Телеметрия | Правила в БД | Статус |
|--------|-----------|--------------|------------|--------------|--------|
| **PropagationPolicy** | ✅ | ✅ | ✅ | ✅ | ✅ Готово |
| **ValidationPolicy** | ✅ | ❌ | ❌ | ✅ | ⚠️ Требует интеграции |
| **CanonizationPolicy** | ✅ | ❌ | ❌ | ✅ | ⚠️ Требует интеграции |

---

## 🔍 Детальный анализ проблем

### Проблема 1: Дублирование логики валидации

**Текущая ситуация**:
- `ValidationPolicy` определена и может работать с правилами из БД
- Но вместо неё используются hardcoded правила в `edge_normalizer.go` и `precreate_validator.go`

**Последствия**:
- Изменение правил требует изменения кода и деплоя
- Нет единой точки управления правилами валидации
- Нет телеметрии по принятым решениям

**Пример дублирования**:
```go
// В edge_normalizer.go (hardcoded)
var EdgeDirectionPolicies = map[models.DependencyType]EdgeDirectionRule{
    models.DependencyTypeRegulates: {
        ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
        ...
    },
}

// В validation_policy.go (не используется)
func (p *ValidationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
    // Может работать с правилами из БД, но не вызывается
}
```

### Проблема 2: Дублирование логики нормализации

**Текущая ситуация**:
- `CanonizationPolicy` определена и может работать с правилами из БД
- Но вместо неё используются hardcoded функции нормализации

**Последствия**:
- Невозможно настроить нормализацию через UI/API
- Нет единой точки управления правилами нормализации
- Нет телеметрии по принятым решениям

**Пример дублирования**:
```go
// В canonicalizer.go (hardcoded)
func normalizeFragmentType(s string) models.FragmentType {
    switch strings.TrimSpace(strings.ToUpper(s)) {
    case string(models.FragmentTypeOrganization), "ORG":
        return models.FragmentTypeOrganization
    ...
    }
}

// В canonization_policy.go (не используется)
func (p *CanonizationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
    // Может работать с правилами из БД, но не вызывается
}
```

---

## 🎯 Рекомендации по улучшению

### Приоритет 1: Интегрировать ValidationPolicy

**Шаги**:
1. Добавить `PolicyExecutor` с `ValidationPolicy` в `DependencyEdgesRepository`
2. Вызывать policy перед созданием edge в `Create()`
3. Вызывать policy в `ScoutEngine.processRelations()` перед созданием edge
4. Обрабатывать решения policy: `reject`, `penalty`, `allow`
5. Мигрировать существующие hardcoded правила в БД

**Ожидаемый результат**:
- Единая точка управления правилами валидации
- Телеметрия по всем решениям валидации
- Возможность изменения правил без деплоя

### Приоритет 2: Интегрировать CanonizationPolicy

**Шаги**:
1. Добавить `PolicyExecutor` с `CanonizationPolicy` в `ScoutEngine`
2. Заменить `normalizeFragmentType()` на вызов policy
3. Заменить части `resolveCanonicalPath()` на вызов policy
4. Заменить `normalizeRelationEdge()` на вызов policy
5. Мигрировать существующие hardcoded правила в БД

**Ожидаемый результат**:
- Единая точка управления правилами нормализации
- Телеметрия по всем решениям нормализации
- Возможность настройки нормализации через UI/API

### Приоритет 3: Унифицировать использование PolicyExecutor

**Шаги**:
1. Создать единый `PolicyService` для управления всеми policies
2. Обеспечить консистентное использование во всех местах обработки данных
3. Добавить middleware для автоматической записи телеметрии

---

## 📝 Выводы

1. **PropagationPolicy** - отличный пример правильной интеграции ✅
2. **ValidationPolicy** и **CanonizationPolicy** - архитектура готова, но не используется ⚠️
3. **Дублирование логики** - hardcoded правила вместо использования policies
4. **Отсутствие телеметрии** - нет отслеживания решений для Validation и Canonization
5. **Невозможность настройки** - правила нельзя изменить без деплоя

**Общая оценка**: 3/5
- Архитектура policies хорошо спроектирована
- Но только 1 из 3 policies реально используется
- Требуется интеграция ValidationPolicy и CanonizationPolicy

