# WorkScope: единый фокус работы Horizon

## Зачем это нужно

В Horizon одновременно существуют несколько «объёмов внимания»: сырой `lens.Scope`, разрешённые query-пути, view/deferred пути, exposure, материализованные артефакты по линзе, соседи на карте, operational-метрики и симуляция. Раньше похожая логика дублировалась в HTTP-handlers, симуляции и гидраторе, что давало расхождения (один экран видел один набор узлов, другой — другой).

**WorkScope** и пакет `backend/internal/horizon/workscope/` вводят **одну каноническую модель фокуса работы** и **общие правила развёртывания линзы в пути и идентификаторы**, чтобы слои согласованно отвечали на вопрос: «что сейчас в работе для этого tenant/project/lens?».

## Два уровня: тип WorkScope и «пути линзы»

### 1. `WorkScope` (вход пайплайна)

Структура **`WorkScope`** — универсальный вход для стадий обогащения графа, материализованных путей, operational-гидрации, распространения сигналов и симуляции.

- **`TenantID` / `ProjectID`** — контекст исполнения.
- **`Refs`** — по смыслу совпадает с **`SimulationBaselineRefs`**: `LensID`, `ScopePaths`, `FragmentIDs`, `FragmentPaths`, `EdgeIDs`, `FactIDs`, ключи/ID артефактов, `SnapshotDigest`.

Операции **`Normalize`** и **`Merge`** задают детерминированное приведение: обрезка строк, удаление пустых, дедупликация, сортировка слайсов — чтобы **digest/идемпотентность** и сравнение объёмов были стабильными.

Пакет также даёт **`FromBaselineRefs` / `ToBaselineRefs`** для совместимости с API и хранилищем.

### 2. Разрешение в рабочий набор: `Resolver` и `ResolvedWorkSet`

**`Resolver`** (над репозиторием материализованных артефактов) раскрывает `WorkScope` в **`ResolvedWorkSet`**: явные ID артефактов из refs плюс, при заданном `LensID`, выборка **`ListByLens`** с **единым лимитом** из констант пакета (см. ниже). Поведение согласовано с operational hydrator: ошибки `ListByLens` не отменяют явно переданные ID.

Таким образом, **один и тот же контракт «что подтянуть по линзе»** используется при построении baseline симуляции и при гидрации метрик, а не двумя расходящимися ветками кода.

## Единые пути и контексты, выводимые из `Lens`

Отдельно от типа `WorkScope` в пакете собраны **функции «workbench paths»** — как из документа `Lens` получить согласованные списки canonical-путей и ссылок для API и скоринга.

| Функция | Назначение |
|---------|------------|
| **`LensScopePathsWithResolvedQueries`** | Как у гео-списка по линзе: записи `/query/...` заменяются на `ResolvedQueries` при наличии; порядок «первое вхождение», дедуп. |
| **`LensWorkbenchScopePaths`** | Объединённый набор для «рабочего стола»: расширенный scope, deferred/view directive пути, supply-chain anchors из доменных query, `ExposurePaths`. Используется симуляцией и HTTP там, где нужен полный фокус линзы (например, список фрагментов по линзе). |
| **`LensCountryHintPaths`** | Подсказки стран для карты/активности: workbench-пути плюс **все** пути из значений `ResolvedQueries` (паритет с эвристиками map activity). |
| **`LensSupplyChainNodeRefs`** | Узловые ref’ы для скоринга overlap supply-chain (аналог бывшего локального сбора в HTTP). |
| **`CollectMapOneHopNeighborFragmentIDs`** | Соседи по рёбрам dependency graph вокруг фрагментов scope, не входящие в сам scope — паритет с `listGeoObjectsForLens`. |

**Важно:** сырой **`lens.Scope`** по-прежнему может использоваться там, где продуктово нужны именно «корни», заданные пользователем (например, часть cockpit/QA), без автоматического расширения. Там, где нужен **тот же объём, что у симуляции и карты**, вызываются функции из `workscope`.

## Лимиты `ListByLens`

Константы в **`workscope/limits.go`** — **SSOT** для перекрёстных потолков выборки материализованных артефактов по линзе:

- **`MaterializedListByLensLimitDefault`** — общий резолвер и operational-потоки.
- **`MaterializedListByLensLimitSimulation`** — baseline симуляции, где намеренно другой потолок.

Так лимиты не разъезжаются между пакетами.

## Кто потребляет

- **Operational metric core / hydrator** — строит цели гидрации из разрешённого scope.
- **Simulation** — baseline и семена через те же пути линзы и те же правила резолва артефактов.
- **Scout / Watch** — сбор `WorkScope` из задач и watch-субъектов; ключи идемпотентности опираются на нормализованный scope и digest, где применимо.
- **HTTP (`internal/http/horizon`)** — фрагменты по линзе, гео-объекты, supply-chain контекст, map activity используют перечисленные хелперы вместо копий логики.
- **Риск-пайплайн** — опирается на общий слой workscope там, где сценарий мониторинга является клиентом того же контура.

## Очередь ручного resolve и гео-пробелы

**`RecordManualResolveGeoGaps`** связывает workscope с очередью **`horizon_manual_resolve_queue`**: по набору фрагментов (например, в объёме линзы) ставятся human-in-the-loop задачи на пробелы вроде координат у org/gov/fac. **Гео-фильтр отображения карты** остаётся отдельным слоем; очередь — про явные дыры в данных, а не про то, что показать на экране.

## Расположение в репозитории

- Реализация: **`backend/internal/horizon/workscope/`**
- Связанные модели: **`backend/internal/horizon/models/`** (`SimulationBaselineRefs`, `Lens`, …)
- Очередь: **`backend/internal/horizon/manualresolve/`**, репозиторий в **`backend/internal/horizon/repo/`**

## Связь с operational metrics

Направление «симуляция и гидрация делят один metric core» зафиксировано в [operational-metric-hydration-and-simulation-early-warning.md](operational-metric-hydration-and-simulation-early-warning.md). WorkScope — **конкретный слой общей scope-resolution логики**, о которой там говорится: без дублирования путей линзы и без второго независимого `ListByLens` в симуляции и метриках.
