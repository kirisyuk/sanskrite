# Horizon Fragment Deduplication Tool

Утилита для поиска и удаления дубликатов Horizon фрагментов по `canonicalPath` с автоматическим переопределением всех связей.

## Проблема

При параллельном создании фрагментов с одинаковым `canonicalPath` (например, `/org/tesla`) могут создаваться дубликаты, так как проверка `GetByCanonicalPath` → `Create` не атомарна.

## Решение

Скрипт:
1. **Находит все дубликаты** по `canonicalPath` в Firestore
2. **Выбирает главный фрагмент** по приоритету:
   - Verified статус > Low Confidence
   - Самый недавно верифицированный
   - Самый старый (создан первым)
3. **Мигрирует все связи**:
   - `DependencyEdges` (source/target fragment IDs)
   - `WatchdogConfigs` (target fragment ID)
   - `Lenses`: `scopeFragments` (canonicalPath → fragmentID), плюс дедуп `resolvedQueries`
   - `LensArtifacts`: `fragmentId`, `sourceFragId`, `targetFragId`
   - `LensItems`: `fragmentId`, `sourceFragmentId`, `targetFragmentId`
   - `FragmentProfiles`: `fragmentId`
   - `MissingInfoStatus`: `fragmentId` + deterministic ID re-key
   - `CascadeEntityRegistry`: `fragmentId`
   - `FragmentVersions` (удаляются вместе с дубликатами)
4. **Удаляет дубликаты** из Firestore
5. **Реэкспортирует** главный фрагмент в GCS (опционально)

## Использование

### Dry-run (рекомендуется сначала)

```bash
go run backend/cmd/tools/horizon_deduplicate_fragments/main.go \
  --project=your-gcp-project \
  --dry-run=true \
  --verbose
```

### Реальное выполнение

```bash
go run backend/cmd/tools/horizon_deduplicate_fragments/main.go \
  --project=your-gcp-project \
  --dry-run=false \
  --bucket=your-gcs-bucket \
  --tenant=global \
  --limit=10  # Обработать только первые 10 дубликатов (для теста)
```

### Параметры

- `--project` (required): GCP project ID
- `--dry-run` (default: true): Только показать что будет сделано, не применять изменения
- `--bucket` (optional): GCS bucket для реэкспорта фрагментов в Vertex AI Search
- `--tenant` (default: "global"): Tenant ID для обработки
- `--limit` (default: 0): Ограничить количество обрабатываемых canonical paths (0 = все)
- `--verbose`: Подробный вывод

## Что обновляется автоматически

✅ **DependencyEdges**: Все ребра где дубликат был source или target перенаправляются на главный фрагмент. Если ребро с главным уже существует - дубликат удаляется.

✅ **WatchdogConfigs**: Конфиги мониторинга перенаправляются на главный фрагмент. Если у главного уже есть конфиг - дубликат удаляется.

✅ **Lenses**: Для линз, где есть `canonicalPath` в `scope`, обновляется `scopeFragments[canonicalPath]` на главный `fragmentId`. В `resolvedQueries` выполняется дедуп путей.

✅ **LensArtifacts**: Переназначаются `fragmentId` для FRAGMENT-артефактов и `sourceFragId`/`targetFragId` для EDGE-артефактов. Self-loop после коллапса удаляются.

✅ **LensItems**: Переназначаются `fragmentId` (node) и `sourceFragmentId`/`targetFragmentId` (edge). Self-loop после коллапса удаляются.

✅ **FragmentProfiles**: Все профили дубликатов переводятся на главный `fragmentId`.

✅ **MissingInfoStatus**: Статусы переносятся на главный `fragmentId` с пересчётом deterministic ID (`hash(fragmentId + question)`), с merge при коллизии.

✅ **CascadeEntityRegistry**: Привязки `fragmentId` внутри `horizon_cascade_entities` обновляются на главный фрагмент.

✅ **FragmentVersions**: Версии дубликатов удаляются (они привязаны к конкретному fragment ID).

✅ **GCS Files**: Файлы удалённых дубликатов автоматически удаляются из GCS bucket.

✅ **Vertex AI Search**: При удалении файла из GCS триггерится синхронизация с Vertex AI Search. При INCREMENTAL режиме удалённые файлы автоматически удаляются из индекса при следующей синхронизации.

## Пример вывода

```
Starting deduplication scan (dry-run=true, tenant=global)...
Found 5 canonical paths with duplicates
Processing: /org/tesla (3 duplicates)
  [DRY-RUN] Would keep: abc123, remove: [def456, ghi789]
Processing: /fac/tw/fab-21 (2 duplicates)
  [DRY-RUN] Would keep: xyz999, remove: [uvw888]

=== Deduplication Summary ===
Dry-run mode: Found 5 duplicate groups, 8 total duplicate fragments
Run with --dry-run=false to apply changes
```

## Безопасность

- **Всегда запускайте сначала с `--dry-run=true`** чтобы увидеть что будет изменено
- Используйте `--limit` для тестирования на небольшом количестве дубликатов
- Скрипт логирует все операции, включая ошибки
- При ошибках миграции связей скрипт продолжает работу (логирует WARNING)

## Ограничения

1. **Lenses migration scope**: обновляются линзы, где `canonicalPath` присутствует в `scope` (`FindByScope`), а не полный скан коллекции.

2. **GCS/Vertex**: Скрипт автоматически определяет bucket из переменной окружения `HORIZON_FRAGMENTS_BUCKET` или использует дефолтный `{project}-horizon-fragments`. При удалении дубликатов файлы автоматически удаляются из GCS, и триггерится синхронизация с Vertex AI Search.

3. **Frames**: Frames используют `targetCanonicalPath` (путь, не ID), поэтому не требуют обновления.

## TODO

- [ ] Добавить транзакционную безопасность для критичных операций
- [ ] Добавить возможность отката изменений (backup перед удалением)
- [ ] Поддержка батч-обработки для больших объемов данных
