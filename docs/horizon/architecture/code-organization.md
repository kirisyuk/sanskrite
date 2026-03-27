# Организация кода Horizon в моноядре

Документ описывает **фактическую** раскладку по репозиторию (не целевой дизайн). Детали протокола HTTP — [api/endpoints.md](../api/endpoints.md) и `backend/internal/http/horizon/handlers.go`.

## Принципы

- Доменная логика Horizon живёт в **`backend/internal/horizon/`**; HTTP — в **`backend/internal/http/horizon/`**.
- Зависимости от платформы (AI, realtime, core repo) задаются через **`HorizonServiceDependencies`** в [`interfaces.go`](../../../backend/internal/horizon/interfaces.go).
- Фасад оркестрации — **`HorizonService`** в [`service.go`](../../../backend/internal/horizon/service.go) (Scout, propagation, lens, watch, recommendations и т.д.).

## Пакет `backend/internal/horizon/`

Каталоги верхнего уровня (роль в одну строку):

| Каталог / файл | Назначение |
|----------------|------------|
| `models/` | Доменные типы: фрагменты, рёбра, линзы, сигналы, артефакты, watch и др. |
| `repo/` | Firestore и прочие репозитории под коллекции Horizon |
| `scout/` | Scout engine: планировщик, агенты, canonical path, supply-chain парсинг, domain modes |
| `domain/` | Доменный режим / контракты исполнения, связанные с Scout |
| `propagation/` | Распространение метрик и сигналов по графу |
| `lens/` | Сервис линз, подписки, пересчёты |
| `workscope/` | WorkScope: единый фокус работы, резолв в materialized IDs, общие пути линзы (симуляция, метрики, HTTP, карта). Архитектура: [workscope.md](workscope.md) |
| `watch/`, `watchdog/`, `watchruntime/` | Watch-таргеты, дог-фон, runtime-снимки страниц |
| `policy/` | Политики, enforcement executor, валидация рёбер |
| `pathbuilder/`, `pathbinding/`, `pathstress/` | Сборка supply-chain путей, привязки, стресс-пути |
| `materialize/`, `supplysemantics/` | Материализация артефактов и семантика supply |
| `interpretation/` | Очередь/слой интерпретации (сигналы → состояние) |
| `signals/`, `signal/` | Извлечение/префиксы сигналов |
| `qa/` | Lens Q&A (декомпозиция, контекст, синтез) |
| `recommendations/` | Стратегические рекомендации |
| `risk/` | Риск-метрики, concentration, appetite |
| `enrich/` | Обогащение сущностей (гео, внешние источники) |
| `graphenrich/` | Контекст для обогащения графа |
| `edgesemantics/`, `entity_semantics/`, `contractsemantics/` | Семантика рёбер и сущностей |
| `canonical/`, `naming/` | Канонизация имён и путей |
| `highprofile/`, `importance/`, `metrics/` | Профили важности и метрики |
| `merge/` | Слияние фрагментов |
| `scopeagent/` | Агент области линзы |
| `tasks/` | Фоновые задачи Horizon |
| `telemetry/`, `telemetrycollector/`, `trace/` | Телеметрия вызовов и трассы |
| `feedback/`, `rtctx/` | Обратная связь и realtime-контекст |
| `critical_path/` | Критический путь (интеграция с Critical Chain) |
| `geoambiguity/`, `mapactivity/` | Гео-неоднозначность и активность на карте |
| `interfaces.go` | Зависимости сервиса от платформы |
| `service.go` | Сборка подсистем и публичные операции сервиса |

Набор пакетов со временем растёт; при сомнениях смотрите **`ls backend/internal/horizon/`** и импорты в `service.go`.

## HTTP: `backend/internal/http/horizon/`

- **`handlers.go`** — `RegisterRoutes`: таблица всех префиксов `/api/horizon/...`.
- **`HandleLensRoutes` / `HandleFragmentRoutes` / `HandleEdgeRoutes` / `HandleScoutRoutes`** — ветвление по подпутям (см. комментарии в `handlers.go`).
- **`enforcement_handlers.go`** — `EnforcementHandlers.RegisterRoutes` для `/api/horizon/enforcement/...` (регистрируется из `main` рядом с Horizon).
- **`horizonmonitor_external_routes.go`** — прокси внешних фидов `/api/horizon/feeds/...` (**без** обёртки Firebase auth в `RegisterFeedRoutes`, см. комментарий в файле).

Остальные `.go` файлы в пакете — обработчики по областям (линзы, фрагменты, scout jobs, watch, co-pilot, canvas, QA SSE, complete-substrate, recover-metrics, identify-stresses и т.д.).

## Связка в сервере

Инициализация репозиториев, `HorizonService`, `HorizonHandlers`, опциональные canvas (news-by-fragments, identify-stresses, complete-substrate, recover-metrics) и вызов `RegisterRoutes` выполняются в **`backend/cmd/server/main.go`** (поиск по `horizonhttp` / `NewHorizonHandlers`).

## Модели

Доменные структуры Horizon — в **`backend/internal/horizon/models/`**. Общие модели платформы (пользователь, проект) остаются в **`backend/internal/models/`**.
