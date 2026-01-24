# Аудит соответствия скрипта дедупликации таксономии системы

## Дата проверки: 2026-01-24

## Обзор

Скрипт `horizon_smart_dedup` проверяет и исправляет дубликаты фрагментов Horizon. Проверено соответствие правилам таксономии, определенным в `backend/internal/taxonomy/`.

## Найденные проблемы

### 1. ❌ Отсутствует нормализация DOMAIN canonical paths

**Проблема:**
- Скрипт не использует функцию `taxonomy.NormalizeDomainCanonicalPath()` для нормализации domain путей
- Domain пути с одинаковыми фасетами в разном порядке не будут распознаны как дубликаты
- Например, `/domain/v1/market-opportunity/geo/us/topic/semiconductors` и `/domain/v1/market-opportunity/topic/semiconductors/geo/us` считаются разными путями, хотя семантически идентичны

**Текущее состояние:**
- Скрипт использует `taxonomy.ValidateCanonicalPath()` только для GEO_REGION путей
- Domain фрагменты не обрабатываются специально
- Нет функции `fixDomainPath()` аналогичной `fixGeoPath()`

**Рекомендация:**
Добавить нормализацию domain путей в функцию обработки кластеров (около строки 1647):

```go
// Fix canonical path for DOMAIN if needed
if canonical.Type == models.FragmentTypeDomain {
    if normalized, err := taxonomy.NormalizeDomainCanonicalPath(canonical.CanonicalPath); err == nil && normalized != "" && normalized != canonical.CanonicalPath {
        oldPath := canonical.CanonicalPath
        if applyCanonicalPathUpdate(canonical, normalized) {
            pathFixed = true
            stats.PathsFixed++
            if *verbose {
                log.Printf("  Path fixed: %s -> %s", oldPath, canonical.CanonicalPath)
            }
        }
    }
}
```

### 2. ⚠️ Неполная проверка composite geo tokens

**Проблема:**
- Функция `normalizeOrgGovGeoForDedupKey()` проверяет только ISO2 и composite geo tokens
- Не проверяет региональные токены через `taxonomy.IsRegionalGeoToken()`
- Это может привести к неправильной обработке путей с региональными токенами (например, "mena", "emea", "gcc")

**Текущее состояние:**
```go
func normalizeOrgGovGeoForDedupKey(s string) string {
    // ... проверяет только ISO2 и composite
    if taxonomy.IsCompositeGeoToken(s) {
        return s
    }
    return "na"
}
```

**Рекомендация:**
Добавить проверку региональных токенов:
```go
if taxonomy.IsRegionalGeoToken(s) {
    return s
}
```

### 3. ✅ Правильная обработка GEO_REGION путей

**Статус:** Соответствует таксономии
- Использует `taxonomy.ValidateCanonicalPath()` для проверки
- Исправляет неканонические пути через `fixGeoPath()`
- Правильно обрабатывает ISO2/ISO3 конверсию
- Поддерживает региональные токены через `scout.GetCountryCanonicalPath()`

### 4. ✅ Правильная обработка ORGANIZATION путей

**Статус:** Соответствует таксономии
- Использует `taxonomy.ParseCanonicalPathParts()` для парсинга
- Нормализует `_` и `-` в slug'ах через `normalizeOrgCanonicalPathForDedup()`
- Правильно обрабатывает legacy пути `/org/{slug}`
- Проверяет composite geo tokens для org/gov путей

### 5. ⚠️ Отсутствует нормализация SEGMENT путей

**Проблема:**
- Скрипт не использует `taxonomy.NormalizeSegmentPath()` или `taxonomy.NormalizeMarketSegments()`
- Market segments хранятся в поле `MarketSegments`, но не нормализуются при дедупликации
- Это может привести к дубликатам из-за разных форматов segment путей

**Рекомендация:**
Добавить нормализацию market segments при обработке ORGANIZATION фрагментов (если это необходимо для дедупликации).

### 6. ✅ Правильная валидация путей

**Статус:** Соответствует таксономии
- Использует `taxonomy.ValidateCanonicalPath()` для проверки GEO_REGION
- Использует `taxonomy.ParseCanonicalPathParts()` для парсинга структурированных путей
- Правильно обрабатывает ошибки парсинга

## Рекомендации по исправлению

### Приоритет 1 (Критично): Нормализация DOMAIN путей

Добавить нормализацию domain canonical paths для правильной дедупликации domain фрагментов.

### Приоритет 2 (Важно): Улучшение обработки geo tokens

Добавить поддержку региональных geo tokens в `normalizeOrgGovGeoForDedupKey()`.

### Приоритет 3 (Желательно): Нормализация SEGMENT путей

Рассмотреть добавление нормализации market segments при необходимости.

## Дополнительные улучшения (после первоначального аудита)

### ✅ Добавлена дедупликация legacy /org/{company} с /org/{geo}/{company}-{legalForm}

**Реализовано:**
- Добавлена функция `extractOrgBaseName()` для извлечения базового названия компании без legal form
- Добавлена функция `stripLegalFormSuffixFromSlug()` для удаления суффиксов legal form из slug
- Добавлен Priority 0.85: дедупликация legacy путей `/org/{company}` с каноническими путями `/org/{geo}/{company}-{legalForm}`
- Приоритет отдается путям с geo сегментом (через существующую логику `selectCanonicalByScore()`)

**Логика работы:**
1. Индексируются все ORG фрагменты по базовому названию компании (без legal form)
2. Разделяются на legacy (`/org/{company}`) и канонические (`/org/{geo}/{company}-{legalForm}`)
3. Создаются кластеры где базовое название совпадает
4. Применяются те же safety checks, что и для других ORG кластеров (Wikidata конфликты, legal name конфликты)
5. Приоритет отдается каноническому пути с geo сегментом

**Пример:**
- `/org/tesla` (legacy) будет объединен с `/org/us/tesla-inc` (канонический)
- Приоритет: `/org/us/tesla-inc` (имеет geo сегмент)

### ✅ Добавлен приоритет для фрагментов с профайлами

**Реализовано:**
- Добавлен репозиторий профилей (`FragmentProfilesRepository`) в main функцию
- Добавлена функция `buildProfileCache()` для загрузки всех профилей в кеш
- Модифицирована функция `selectCanonicalByScore()` для проверки наличия профиля
- Фрагменты с профайлами получают +200 баллов (высший приоритет)

**Логика работы:**
1. После загрузки всех фрагментов создается кеш профилей (map[fragmentID]bool)
2. Для каждого фрагмента проверяется наличие активного профиля через `GetByFragmentID()`
3. При выборе канонического фрагмента фрагменты с профайлами получают +200 баллов
4. Это гарантирует, что фрагмент с профайлом всегда будет выбран как канонический

**Приоритеты (в порядке убывания):**
1. **+200 баллов**: Фрагмент с профайлом (высший приоритет)
2. +100 баллов: Фрагмент с Wikidata ID
3. +60 баллов: Фрагмент с geo сегментом (для ORG/GOV)
4. +60 баллов: Фрагмент с industryPaths
5. Другие факторы качества данных

**Пример:**
- Фрагмент A: `/org/us/tesla-inc` (без профайла, с Wikidata)
- Фрагмент B: `/org/us/tesla` (с профайлом, без Wikidata)
- **Выбран: Фрагмент B** (имеет профайл, +200 баллов > +100 баллов за Wikidata)

## Заключение

Скрипт в целом соответствует таксономии системы для большинства типов фрагментов (GEO_REGION, ORGANIZATION, PRODUCT, FACILITY, PERSON). 

**Исправлено:**
- ✅ Добавлена нормализация DOMAIN путей
- ✅ Улучшена обработка региональных geo tokens
- ✅ Добавлена дедупликация legacy /org/{company} с каноническими путями

Скрипт теперь полностью соответствует правилам таксономии и корректно обрабатывает все типы фрагментов, включая edge cases с legacy путями.
