# Horizon Database Consistency Tools

Набор Go-скриптов для обслуживания консистентности базы данных Horizon. Все инструменты поддерживают dry-run режим по умолчанию и требуют флаг `--apply` для применения изменений.

## Содержание

- [Дедупликация фрагментов](#дедупликация-фрагментов)
- [Очистка orphaned данных](#очистка-orphaned-данных)
- [Слияние и миграция фрагментов](#слияние-и-миграция-фрагментов)
- [Нормализация данных](#нормализация-данных)
- [Заполнение индексов](#заполнение-индексов)
- [Проверка консистентности](#проверка-консистентности)
- [Дополнительные утилиты](#дополнительные-утилиты)

---

## Дедупликация фрагментов

### `horizon_deduplicate_fragments`

**Назначение:** Находит и объединяет дубликаты фрагментов по `canonicalPath`.

**Как работает:**
1. Сканирует все фрагменты и группирует по `canonicalPath`
2. Для каждой группы дубликатов выбирает основной фрагмент по приоритету:
   - Verified статус (высший приоритет)
   - Наиболее свежий `LastVerifiedAt`
   - Старейший `CreatedAt` (более устоявшийся)
3. Мигрирует все ссылки с дубликатов на основной:
   - `DependencyEdges` (source/target)
   - `WatchdogConfigs`
   - `Lenses` (scopeFragments)
   - `FragmentVersions` (удаляет версии дубликатов)
4. Удаляет дубликаты из Firestore и GCS
5. Переэкспортирует основной фрагмент в GCS

**Использование:**
```bash
go run ./cmd/tools/horizon_deduplicate_fragments \
  --project=hatlone-test \
  --dry-run=false \
  --limit=100 \
  --verbose
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--dry-run` (default: true) - только отчет без изменений
- `--tenant` (default: "global") - Tenant ID (Horizon всегда global)
- `--limit` (default: 0) - ограничение количества обработанных групп
- `--bucket` - GCS bucket для фрагментов (по умолчанию из env)
- `--verbose` - подробное логирование

---

### `horizon_smart_dedup`

**Назначение:** Умная дедупликация фрагментов по множественным сигналам с использованием различных стратегий сопоставления.

**Как работает:**
1. **Построение индексов** по различным сигналам:
   - **Wikidata ID** (высший приоритет) - точное совпадение
   - **shortName** - короткое имя организации/продукта
   - **legalName** - юридическое название
   - **label[en]** - английская метка
   - **Нормализованные имена** - удаление суффиксов (JSC, LLC, Inc и т.д.)
   - **Географические города** - с фильтрацией по стране
   - **Продукты** - по slug и алиасам (label/short/legal)
   - **Фонетические совпадения** - для персон (Tahnoon/Tahnoun)
   - **Facility normalized names** - нормализованные имена объектов
   - **Canonical path aliases** - через merged_paths, legacy_org_canonical_path

2. **Построение кластеров** дубликатов с учетом приоритетов:
   - Priority 0: Организации как страны (ORG → GEO_REGION)
   - Priority 0.5: Неканонические GEO пути → канонические
   - Priority 0.75: Алиасы canonical paths
   - Priority 0.8: Нормализация путей организаций (- vs _)
   - Priority 1: Wikidata ID
   - Priority 2-8: Остальные сигналы

3. **Выбор канонического фрагмента** по скору:
   - Wikidata ID (+100)
   - IndustryPaths (+60)
   - Правильный формат пути (для GEO: /loc/{cc}/{admin1}/{city})
   - Verified статус (+30)
   - Количество идентификаторов и KBRefs

4. **Слияние данных** (fill-only):
   - Labels, Identifiers, KBRefs, Description, Tags
   - Отслеживание merged_paths

5. **Исправление рёбер:**
   - Переназначение edges с дубликатов на канонический
   - Исправление направлений (SUBSIDIARY_OF → OWNS, HAS_KEY_PERSON → KEY_PERSON)
   - Нормализация симметричных рёбер
   - Глобальная дедупликация рёбер

6. **Удаление дубликатов** из Firestore, GCS и синхронизация с Vertex AI Search

**Использование:**
```bash
# Дедупликация всех типов
go run ./cmd/tools/horizon_smart_dedup \
  --project=hatlone-test \
  --apply \
  --verbose

# Только организации
go run ./cmd/tools/horizon_smart_dedup \
  --project=hatlone-test \
  --type=ORGANIZATION \
  --apply \
  --enable-org-name-only \
  --enable-org-fuzzy-name

# Только исправление рёбер (без дедупликации фрагментов)
go run ./cmd/tools/horizon_smart_dedup \
  --project=hatlone-test \
  --fix-edges-only \
  --apply
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--apply` - применить изменения (по умолчанию dry-run)
- `--dry-run` - принудительный dry-run
- `--tenant` (default: "global") - Tenant ID
- `--type` - фильтр по типу фрагмента (GEO_REGION, ORGANIZATION, FACILITY и т.д.)
- `--union-clusters` - построение транзитивных кластеров через union-find
- `--skip-vertex-sync` - пропустить синхронизацию с Vertex AI Search
- `--enable-product-alias` - включить дедупликацию продуктов по алиасам
- `--enable-org-name-only` - включить дедупликацию ORG только по имени (риск false positives)
- `--enable-org-fuzzy-name` - включить fuzzy дедупликацию ORG (нормализованные имена)
- `--fix-edges-only` - только исправить/нормализовать рёбра, пропустить дедупликацию фрагментов
- `--verbose` - подробное логирование

---

## Очистка orphaned данных

### `horizon_cleanup_orphaned_versions`

**Назначение:** Находит и удаляет версии фрагментов, которые ссылаются на несуществующие фрагменты.

**Как работает:**
1. Загружает все существующие ID фрагментов в память
2. Сканирует все версии фрагментов
3. Проверяет, существует ли фрагмент для каждой версии
4. Удаляет orphaned версии

**Использование:**
```bash
go run ./cmd/tools/horizon_cleanup_orphaned_versions \
  --project=hatlone-test \
  --dry-run=false
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--tenant` (default: "global") - Tenant ID
- `--dry-run` (default: true) - только отчет без удаления

---

## Слияние и миграция фрагментов

### `horizon_merge_discovered`

**Назначение:** Мигрирует исторические фрагменты из `/discovered/*` в стабильные канонические пространства имен (`/org/*`, `/product/*`, `/fac/*`, `/loc/*`, `/event/*`).

**Как работает:**
1. Находит все фрагменты с `canonicalPath` начинающимся с `/discovered/`
2. Для каждого фрагмента:
   - Использует LLM для выбора канонического пути (с учетом кандидатов из Vertex AI Search)
   - Создает или обновляет канонический фрагмент (fill-only merge)
   - Мигрирует edges (заменяет discovered ID на canonical ID)
   - Мигрирует frames (обновляет targetCanonicalPath)
   - Удаляет discovered фрагмент из Firestore и GCS
3. Запускает FULL reconcile в Vertex AI Search для очистки индекса

**Использование:**
```bash
go run ./cmd/tools/horizon_merge_discovered \
  --project=hatlone-test \
  --apply \
  --limit=100
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--apply` - применить изменения
- `--dry-run` - принудительный dry-run
- `--tenant` (default: "global") - Tenant ID
- `--limit` - максимальное количество фрагментов для обработки
- `--vertex-store` - Vertex AI Search data store ID
- `--vertex-location` - Vertex AI Search location
- `--rag-bucket` - GCS bucket для Vertex индексации
- `--vertex-full-reconcile` (default: true) - запустить FULL reconcile в конце

---

### `horizon_merge_org_aliases`

**Назначение:** Объединяет алиасы организаций (например, `/org/ford-motor-company` → `/org/ford`) используя shortName с подтверждением через LLM.

**Как работает:**
1. Находит все фрагменты `/org/*`
2. Для каждого фрагмента:
   - Использует LLM для нормализации (извлечение shortName, legalName, preferred canonicalPath)
   - Если preferred путь существует:
     - Проверяет через LLM, что это одна и та же сущность (с порогом confidence)
     - Объединяет данные в preferred фрагмент (fill-only)
     - Мигрирует edges и frames
     - Удаляет старый фрагмент
   - Если preferred путь не существует:
     - Создает новый фрагмент с preferred путем
     - Мигрирует edges и frames
     - Удаляет старый фрагмент
3. Запускает FULL reconcile в Vertex AI Search

**Использование:**
```bash
go run ./cmd/tools/horizon_merge_org_aliases \
  --project=hatlone-test \
  --apply \
  --min-confidence=0.85 \
  --only=ford-motor-company,byd-company
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--apply` - применить изменения
- `--dry-run` - принудительный dry-run
- `--tenant` (default: "global") - Tenant ID
- `--limit` - максимальное количество фрагментов для обработки
- `--min-confidence` (default: 0.85) - минимальный confidence для слияния
- `--only` - фильтр по подстрокам canonicalPath (через запятую)
- `--vertex-store`, `--vertex-location`, `--bucket` - конфигурация Vertex AI Search
- `--vertex-full-reconcile` (default: true) - запустить FULL reconcile

---

### `horizon_merge_geo_locations`

**Назначение:** Заглушка (не реализована).

**Статус:** Текущая реализация - безопасная заглушка, которая компилируется и выводит сообщение.

---

## Нормализация данных

### `horizon_normalize_loc_geonames`

**Назначение:** Нормализует географические пути фрагментов через GeoNames API.

**Как работает:**
1. Сканирует все активные фрагменты с `canonicalPath` начинающимся с `/loc/`
2. Для каждого фрагмента:
   - Использует `CanonicalPathResolver` с GeoNames клиентом для нормализации пути
   - Если путь изменился:
     - Проверяет confidence (пропускает если ниже порога)
     - Если целевой путь уже существует - дедуплицирует в существующий
     - Иначе обновляет canonicalPath и indexPaths
     - Мигрирует frames (targetCanonicalPath)
   - Опционально прикрепляет GeoNames ID к фрагментам (для строгих путей `/loc/{cc}/{adm1}/{city}`)
3. Поддерживает курсор для возобновления обработки

**Использование:**
```bash
# Dry-run с прикреплением GeoNames ID
go run ./cmd/tools/horizon_normalize_loc_geonames \
  --project=hatlone-test \
  --limit=1000 \
  --skip-confidence-below=0.8

# Применить изменения
go run ./cmd/tools/horizon_normalize_loc_geonames \
  --project=hatlone-test \
  --apply \
  --apply-geonames-id \
  --cursor-file=/tmp/normalize_cursor.json
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--apply` - применить изменения
- `--dry-run` - принудительный dry-run
- `--tenant` (default: "global") - Tenant ID
- `--limit` - максимальное количество обработанных фрагментов
- `--max-scan` (default: 100000) - максимальное количество сканированных документов
- `--page-size` (default: 200) - размер страницы для пагинации
- `--start-after` - курсор для возобновления (Firestore document ID)
- `--cursor-file` - файл для сохранения курсора
- `--save-cursor-every-pages` (default: 5) - сохранять курсор каждые N страниц
- `--skip-confidence-below` (default: 0.8) - пропускать нормализацию с низким confidence
- `--apply-geonames-id` - прикреплять GeoNames ID к фрагментам
- `--skip-vertex-sync` - пропустить синхронизацию с Vertex AI Search
- `--progress-every` - логировать прогресс каждые N обработанных строк
- `--heartbeat-sec` - heartbeat логи каждые N секунд

**Требования:**
- Переменная окружения `GEONAMES_USERNAME` должна быть установлена

---

## Заполнение индексов

### `horizon_backfill_index_paths`

**Назначение:** Заполняет поле `indexPaths` для всех фрагментов Horizon.

**Как работает:**
1. Сканирует все фрагменты с пагинацией
2. Для каждого фрагмента:
   - Вычисляет желаемые `indexPaths` через `models.BuildIndexPaths()`
   - Сравнивает с существующими (нормализованными)
   - Обновляет только при расхождениях
3. Опционально обновляет timestamps (`updatedAt`, `metadata.lastUpdatedAt`)

**Использование:**
```bash
# Dry-run
go run ./cmd/tools/horizon_backfill_index_paths \
  --project=hatlone-test \
  --page-size=200

# Применить изменения
go run ./cmd/tools/horizon_backfill_index_paths \
  --project=hatlone-test \
  --dry-run=false \
  --touch-timestamps \
  --start-after=lastDocID
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--tenant` (default: "global") - Tenant ID (Horizon всегда global)
- `--page-size` (default: 200) - размер страницы для пагинации (рекомендуется 100-500)
- `--limit` - ограничение количества обработанных документов
- `--start-after` - курсор для возобновления (Firestore document ID)
- `--dry-run` (default: true) - только отчет без изменений
- `--sleep-per-page-ms` (default: 250) - задержка между страницами
- `--update-qps` (default: 10) - максимальное количество обновлений в секунду
- `--refresh-client-minutes` (default: 25) - пересоздавать Firestore клиент каждые N минут
- `--page-retry-max` (default: 5) - максимальное количество повторов для страницы
- `--touch-timestamps` (default: true) - обновлять timestamps при изменении indexPaths

---

## Проверка консистентности

### `horizon_check_signals`

**Назначение:** Инспектирует конкретный фрагмент и показывает связанные сигналы, метрики и факты.

**Как работает:**
1. Загружает фрагмент по `canonicalPath`
2. Показывает метрики рисков (ключи содержащие "risk")
3. Показывает связанные факты (по типам)
4. Показывает последний metric fact с деталями
5. Показывает сигналы для фрагмента

**Использование:**
```bash
go run ./cmd/tools/horizon_check_signals \
  --project=hatlone-test \
  --path=/org/byd \
  --limit=200
```

**Флаги:**
- `--project` (default: "hatlone-test") - GCP project ID
- `--tenant` (default: "global") - Tenant ID
- `--path` (required) - canonical path для инспекции (например, `/org/byd`)
- `--limit` (default: 200) - максимальное количество фактов для сканирования

---

### `check_market_submarket_bidirectional`

**Назначение:** Находит пары MARKET↔MARKET с bidirectional HAS_SUBMARKET (оба A→B и B→A). Проверяет гипотезу: путь с большим числом сегментов = submarket (child), с меньшим = parent.

**Использование:**
```bash
go run ./cmd/tools/check_market_submarket_bidirectional --project=hatlone-test
go run ./cmd/tools/check_market_submarket_bidirectional --project=hatlone-test --limit=20
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--tenant` (default: "global") - Tenant ID
- `--limit` (default: 0) - макс. пар для вывода (0 = все)
- `--apply` - применить исправления: удалить некорректные child→parent рёбра (и оба sibling-рёбра)

---

### `check_founded_edges`

**Назначение:** Проверяет неправильные направления рёбер между организациями и людьми.

**Как работает:**
1. Сканирует все рёбра
2. Находит рёбра типа `ORG → PERSON` (неправильное направление)
3. Показывает статистику по типам рёбер

**Использование:**
```bash
go run ./cmd/tools/check_founded_edges
```

**Примечание:** Использует хардкод project ID "hatlone-test". Для других проектов нужно изменить код.

---

## Дополнительные утилиты

### `horizon_reexport_fragments`

**Назначение:** Переэкспортирует все фрагменты Horizon из Firestore в GCS в новом формате, совместимом с Discovery Engine.

**Как работает:**
1. Сканирует все фрагменты с пагинацией
2. Экспортирует каждый фрагмент в GCS через `fragmentsRepo.ExportToGCS()`
3. Поддерживает checkpoint для возобновления
4. Опционально пропускает уже существующие файлы

**Использование:**
```bash
go run ./cmd/tools/horizon_reexport_fragments \
  --project=hatlone-test \
  --bucket=my-bucket \
  --resume \
  --checkpoint-file=/tmp/reexport_checkpoint.txt
```

**Флаги:**
- `--project` - GCP project ID
- `--bucket` - GCS bucket для фрагментов
- `--tenant` (default: "global") - Tenant ID
- `--limit` - максимальное количество фрагментов для экспорта
- `--batch` (default: 100) - размер батча для обработки
- `--skip-existing` - пропускать фрагменты, которые уже существуют в GCS
- `--resume` - возобновить с checkpoint файла
- `--checkpoint-file` - путь к файлу checkpoint
- `--start-after` - начать после указанного document ID
- `--page-size` (default: 300) - размер страницы для Firestore

---

### `horizon_propagate_metric`

**Назначение:** Изменяет метрику у фрагмента и запускает распространение через граф зависимостей.

**Как работает:**
1. Загружает фрагмент по `canonicalPath`
2. Изменяет указанную метрику на `delta`
3. Обновляет фрагмент в Firestore
4. Запускает propagation через `PolicyExecutor` с учетом правил распространения
5. Публикует события в Pub/Sub

**Использование:**
```bash
go run ./cmd/tools/horizon_propagate_metric \
  --project=hatlone-test \
  --path=/org/byd \
  --metric=/metric/risk/supply_chain_criticality \
  --delta=0.1
```

**Флаги:**
- `--project` (default: "hatlone-test") - GCP project ID
- `--path` (required) - canonical path фрагмента
- `--metric` (required) - имя метрики для изменения
- `--delta` (default: 0.1) - изменение значения метрики

---

### `horizon_edge_policy_report`

**Назначение:** Строит comparison-report для KPI Big Bang Edge Policy (`baseline` vs `after`) и печатает `top invalid triples`.

**Как работает:**
1. Читает агрегированные счетчики из `horizon_telemetry`:
   - `component=edge_policy`, `metric=kpi_rates`
   - `component=edge_policy`, `metric=top_invalid_triples`
2. Формирует два окна:
   - `after`: последние `--window-hours`
   - `baseline`: окно такой же длины, заканчивающееся на `now - --baseline-shift-hours`
3. Считает KPI-rate как `counter / policy_decision_total`
4. Печатает Top-N invalid triples по after-window.

**Использование:**
```bash
go run ./cmd/tools/horizon_edge_policy_report \
  --project=hatlone-test \
  --tenant=global \
  --window-hours=24 \
  --baseline-shift-hours=24 \
  --top-invalid-limit=20
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--tenant` (default: "global") - Tenant ID
- `--window-hours` (default: 24) - длина сравниваемого окна
- `--baseline-shift-hours` (default: 24) - смещение baseline-окна назад
- `--limit` (default: 720) - лимит telemetry windows per metric
- `--top-invalid-limit` (default: 20) - сколько invalid triples вывести

---

### `horizon_find_missing_wikidata`

**Назначение:** Находит фрагменты без Wikidata ID и опционально предлагает кандидатов через Wikidata API.

**Как работает:**
1. Сканирует все фрагменты (или только ACTIVE)
2. Проверяет наличие Wikidata ID в фрагменте и связанной сущности
3. Опционально ищет кандидатов через Wikidata API
4. Опционально применяет найденные Wikidata ID и industryPaths
5. Выводит результаты в JSONL или TSV формате

**Использование:**
```bash
# Найти фрагменты без Wikidata
go run ./cmd/tools/horizon_find_missing_wikidata \
  --project=hatlone-test \
  --limit=1000 \
  --format=jsonl

# С предложениями Wikidata
go run ./cmd/tools/horizon_find_missing_wikidata \
  --project=hatlone-test \
  --suggest-wikidata \
  --limit=100

# Применить найденные Wikidata ID
go run ./cmd/tools/horizon_find_missing_wikidata \
  --project=hatlone-test \
  --suggest-wikidata \
  --apply-wikidata-ref \
  --apply-industry \
  --wikidata-score-min=120
```

**Флаги:**
- `--project` (required) - GCP project ID
- `--tenant` (default: "global") - Tenant ID
- `--limit` (default: 100) - максимальное количество фрагментов для вывода
- `--max-scan` (default: 50000) - максимальное количество сканированных фрагментов
- `--page-size` (default: 500) - размер страницы для пагинации
- `--start-after` - курсор для возобновления
- `--active-only` (default: true) - только ACTIVE фрагменты
- `--include-entity` (default: true) - проверять связанные сущности
- `--suggest-wikidata` - искать кандидатов через Wikidata API
- `--apply-industry` - применять industryPaths (требует --suggest-wikidata)
- `--apply-wikidata-ref` - записывать Wikidata KBRef в фрагмент
- `--apply-wikidata-names` - перезаписывать имена из Wikidata
- `--wikidata-score-min` (default: 120) - минимальный score для применения
- `--format` (default: "jsonl") - формат вывода: jsonl | tsv
- `--out` - путь к выходному файлу (по умолчанию stdout)

---

### `horizon_wikidata_reclassify_org_person_edges`

**Назначение:** Переклассифицирует рёбра между организациями и людьми используя данные из Wikidata.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_llm_classify_weak_org_person_edges`

**Назначение:** Классифицирует слабые рёбра между организациями и людьми используя LLM.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_llm_backfill`

**Назначение:** Унифицированный LLM-backfill батчами для разных типов фрагментов и наборов полей/метрик (общие: скан, пагинация, курсор, батчи, structured JSON).

**Фичи (`--features`):**
- `org_classifiers`: backfill `orgSize` + `marketSegments` для `ORGANIZATION`
- `market_profile`: backfill `marketDomain` + `marketVertical` + `marketChannels` для `MARKET`
- `market_metrics`: backfill market metrics в `state` для `MARKET` под ключами:
  - `/metric/market/tam_usd` (unit=`usd`)
  - `/metric/market/cagr_percent` (unit=`percent`)
  - `/metric/market/hhi` (unit=`index`, 0..10000)
  - `/metric/market/cr4_percent` (unit=`percent`, 0..100)
  - `/metric/market/supply_chain_risk` (0..1)

**Использование:**
```bash
# Dry-run: backfill MARKET profile + market metrics
go run ./cmd/tools/horizon_llm_backfill \
  --project=hatlone-test \
  --features=market_profile,market_metrics \
  --types=MARKET \
  --limit=200 \
  --batch-size=10 \
  --page-size=500

# Apply: backfill org classifiers
go run ./cmd/tools/horizon_llm_backfill \
  --project=hatlone-test \
  --apply \
  --features=org_classifiers \
  --types=ORGANIZATION \
  --limit=1000 \
  --batch-size=10 \
  --cursor-file=/tmp/horizon_llm_backfill_cursor.json
```

**Флаги (основные):**
- `--features` - список фич через запятую
- `--types` - типы фрагментов (если не указано, выводится из `--features`)
- `--limit`, `--batch-size`, `--page-size`, `--max-scan`
- `--start-after`, `--cursor-file`, `--save-cursor-every-pages`
- `--model`, `--engine` (`task_executor|direct`), `--grounding`, `--max-output-tokens`
- `--apply` + per-feature overwrite:
  - `--overwrite-orgsize`, `--overwrite-segments`
  - `--overwrite-market-profile`, `--overwrite-market-metrics`

---

### `horizon_expand_taxonomy_relation_types`

**Назначение:** Расширяет типы отношений в таксономии.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_seed_taxonomy`

**Назначение:** Инициализирует таксономию Horizon.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_seed_propagation_policies`

**Назначение:** Инициализирует политики распространения метрик.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_bootstrap_lens`

**Назначение:** Инициализирует линзы Horizon.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_get_taxonomy`

**Назначение:** Получает и выводит таксономию Horizon.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_get_fragment_by_id`

**Назначение:** Получает фрагмент по ID и выводит его данные.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_find_fragments_by_name`

**Назначение:** Находит фрагменты по имени.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_intent_scenario`

**Назначение:** Тестирует сценарии интентов.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_test_intent_decomposition`

**Назначение:** Тестирует декомпозицию интентов.

**Статус:** Требует изучения кода для детального описания.

---

### `horizon_ux_scenario`

**Назначение:** Тестирует UX сценарии.

**Статус:** Требует изучения кода для детального описания.

---

### `inspect_agent_jobs`

**Назначение:** Инспектирует задачи агентов.

**Статус:** Требует изучения кода для детального описания.

---

### `inspect_horizon_lenses`

**Назначение:** Инспектирует линзы Horizon.

**Статус:** Требует изучения кода для детального описания.

---

### `purge_agent_jobs`

**Назначение:** Удаляет задачи агентов.

**Статус:** Требует изучения кода для детального описания.

---

### `watchdog_facts_smoke`

**Назначение:** Smoke тесты для watchdog фактов.

**Статус:** Требует изучения кода для детального описания.

---

### `loc_canon_smoke`

**Назначение:** Smoke тесты для канонических путей локаций.

**Статус:** Требует изучения кода для детального описания.

---

### `geonames_smoke`

**Назначение:** Smoke тесты для GeoNames интеграции.

**Статус:** Требует изучения кода для детального описания.

---

## Общие паттерны

### Dry-run режим

Большинство инструментов по умолчанию работают в dry-run режиме и требуют флаг `--apply` для применения изменений:

```bash
# Только отчет
go run ./cmd/tools/horizon_deduplicate_fragments --project=hatlone-test

# Применить изменения
go run ./cmd/tools/horizon_deduplicate_fragments --project=hatlone-test --dry-run=false
```

### Миграция ссылок

При слиянии или удалении фрагментов автоматически обновляются:
- **DependencyEdges** - переназначаются source/target fragment IDs
- **Frames** - обновляется targetCanonicalPath
- **WatchdogConfigs** - обновляется targetFragmentID
- **FragmentVersions** - удаляются версии удаленных фрагментов

### Fill-only merge

При слиянии фрагментов данные заполняются только в пустые поля, существующие данные не перезаписываются:
- Labels, Identifiers - заполняются только пустые ключи
- KBRefs - добавляются только отсутствующие ссылки
- Description - заполняется только если пусто
- Tags - объединяются (union)

### Синхронизация с Vertex AI Search

Многие инструменты поддерживают синхронизацию с поисковым индексом:
- `--skip-vertex-sync` - пропустить синхронизацию (быстрее, но нужно запустить bulk import отдельно)
- `--vertex-full-reconcile` - запустить FULL reconcile в конце (очищает удаленные документы из индекса)

### Поддержка курсоров

Длительные операции поддерживают возобновление через курсоры:
- `--start-after` - начать после указанного Firestore document ID
- `--cursor-file` - файл для сохранения/загрузки курсора
- `--save-cursor-every-pages` - периодичность сохранения курсора

### Обработка ошибок

Инструменты обрабатывают:
- Транзитные ошибки Firestore (retry с backoff)
- Отсутствующие фрагменты/рёбра
- Невалидные данные
- Ошибки GCS/Vertex AI Search (логируются как warnings)

---

## Рекомендации по использованию

1. **Всегда начинайте с dry-run** для оценки изменений
2. **Используйте `--limit`** для тестирования на небольшом наборе данных
3. **Сохраняйте курсоры** для длительных операций через `--cursor-file`
4. **Мониторьте логи** на наличие ошибок и warnings
5. **Запускайте инструменты в правильном порядке:**
   - Сначала дедупликация (`horizon_smart_dedup`)
   - Затем нормализация (`horizon_normalize_loc_geonames`)
   - Потом заполнение индексов (`horizon_backfill_index_paths`)
   - В конце проверка консистентности (`horizon_check_signals`)

---

## Требования

- Go 1.19+
- Доступ к GCP проекту с Firestore
- Переменные окружения:
  - `GCP_PROJECT` или `--project` флаг
  - `GEONAMES_USERNAME` (для `horizon_normalize_loc_geonames`)
  - `VERTEX_API_KEY` или доступ к Secret Manager (для LLM инструментов)
  - `HORIZON_FRAGMENTS_BUCKET` (опционально, по умолчанию `{project}-horizon-fragments`)
  - `RAG_DOCUMENTS_BUCKET` (для Vertex AI Search синхронизации)

---

## Лицензия

Внутренние инструменты проекта Sanskrite.
