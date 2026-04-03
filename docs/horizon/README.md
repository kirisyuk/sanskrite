# Система мониторинга горизонта (Horizon)

## Обзор

Система мониторинга горизонта (Horizon System) — это подсистема платформы Sanskrite, предназначенная для автоматического сбора, верификации и мониторинга данных о сущностях реального мира (организации, объекты инфраструктуры, геополитические регионы) с последующим анализом их взаимосвязей и распространения изменений в графе зависимостей. Система обеспечивает ситуационную осведомленность и операционную разведку для принятия решений.

## Архитектурный документ

Данная документация основана на архитектурном решении от 17 декабря 2025 г., которое определяет:

- Принципы работы подсистемы «Разведчик» (Scout) с архитектурой «Зал Суда»
- Экономическую модель управления общими данными («Фрагментов Мира»)
- Алгоритм контроля и распространения изменений в графе зависимостей
- Спецификации системных артефактов (HorizonFragment, Frame, Lens, DependencyEdge)

## Навигация по документации

### Архитектура

- **[Обзор архитектуры](architecture/overview.md)** — высокоуровневая архитектура системы и интеграция с Sanskrite
- **[Supply Chain Canonical Control Plane](architecture/supply-chain-canonical-control-plane.md)** — обязательный верхнеуровневый документ про canonical products, synonyms, transforms/assembly pairs и candidate queues; его нужно читать перед работой с supply-chain кодом
- **[Signal Landing Plane](architecture/signal-landing-plane.md)** — первый слой long pipeline: event/signal -> impact claims -> landed refs -> missing anchors -> flash/hot warmup
- **[Компоненты системы](architecture/components.md)** — детальное описание компонентов (Scout Engine, Propagation Engine, Watchdog, Lens Subsystem)
- **[Модель данных](architecture/data-model.md)** — структуры данных, схемы Firestore, связи с существующими моделями
- **[Интеграция](architecture/integration.md)** — интеграция с существующими компонентами Sanskrite
- **[Базовая таксономия](architecture/taxonomy.md)** — стандарты классификации и оценки сущностей (Core Taxonomy v1.0)
- **[Семантический поиск](architecture/semantic-search.md)** — подход к семантическому поиску сущностей через vectorText
- **[Организация кода](architecture/code-organization.md)** — структура модуля в моноядре, интеграция с существующей системой
- **[Интеграция с Project и DataSource](architecture/project-integration.md)** — использование существующих проектов и источников данных

### Рабочие процессы

- **[Статус Signal Landing и Analytical Runtime (2026-04-03)](implementation/signal-landing-and-analytical-runtime-status-2026-04-03.md)** — большой handoff-документ: что хотели, что уже реализовано, что подтверждено живыми сценариями, что ещё нужно довести до live prod-grade
- **[Workflow Scout](workflows/scout-workflow.md)** — процесс сбора и верификации данных агентом Scout

### UX и пользовательские сценарии

- **[Типовой UX-сценарий](ux/user-journey.md)** — описание пользовательского пути от входа до работы в Cockpit UI
- **[Принципы интерфейса](ux/interface-principles.md)** — системные принципы организации Horizon-интерфейса, визуального языка, layout-логики и технического слоя под UI
- **[Horizon Watch](ux/horizon-watch.md)** — продуктовая спецификация обзорной и персонализированной monitoring-страницы поверх графа, сигналов и артефактов
- **[Workflow Propagation](workflows/propagation-workflow.md)** — алгоритм распространения изменений в графе зависимостей
- **[Жизненный цикл](workflows/lifecycle.md)** — жизненный цикл HorizonFragment (Active → Dormant → Archived)

### API

- **[Endpoints](api/endpoints.md)** — REST API endpoints для работы с системой
- **[Schemas](api/schemas.md)** — JSON схемы для запросов и ответов API

### Реализация

- **[Roadmap](implementation/roadmap.md)** — единственный актуальный статус и план (источник правды по этапам)
- **[Технические решения](implementation/technical-decisions.md)** — зафиксированные решения
- **[Архив планов и ТЗ](../archive/horizon-implementation/)** — черновики, разовые планы, QA-разборы (не обязаны совпадать с текущим кодом построчно)

## Статус реализации

Реализация ведётся в моноядре (`backend/internal/horizon`, HTTP в `backend/internal/http/horizon`, часть UI в `web/src/pages/HorizonPage.tsx` и связанных компонентах). **Актуальная сводка по компонентам и хвостам работ** — в [implementation/roadmap.md](implementation/roadmap.md) (таблица в начале документа).

Кратко: базовые модели, Firestore-репозитории, Lens API, Scout (в т.ч. composable pipeline / domain modes), QueryAddress, политики и значительная часть графа и субстрата уже в коде; отдельные направления (signaling pipeline, часть appetite governance, lens-specific highProfile и др.) помечены в roadmap как незавершённые или отложенные.

## Ключевые концепции

### Базовая таксономия (Core Taxonomy v1.0)

Система использует строгую таксономию для классификации сущностей и гибкие метрики с описаниями и типами для их оценки. Таксономия включает:
- Иерархические пути типов сущностей (`entityPath`): `/Organization/*`, `/Infrastructure/*`, `/Resource/*`, `/Person/*`, `/Geo/*`
- Иерархические пути отраслей (`sectorPath`): `/Energy/*`, `/Technology/*`, `/Materials/*`, и т.д.
- Стандартизированные метрики с типами (magnitude/directional) и диапазонами
- Поддержка дополнительных метрик с описаниями
- ISO стандарты для Location (ISO 3166-1/3166-2 для регионов, ISO 6709 для координат)

На этапе MVP мы строго придерживаемся данной таксономии. Подробнее см. [Базовая таксономия](architecture/taxonomy.md).

### HorizonFragment (Фрагмент Горизонта)

Атомарное, каноническое представление сущности реального мира (организация, объект инфраструктуры, геополитический регион) в графе знаний системы. HorizonFragment расширяет Entity из основной онтологии Sanskrite, добавляя:
- Классификацию через `entityPath` и `sectorPath` согласно базовой таксономии
- Гибкие метрики с описаниями, типами, единицами измерения и интервалами ошибок
- Связь с Entity через `entityId` для обогащения данными
- KBRefs для интеграции с внешними базами знаний (Wikidata и др.)
- Identifiers для связи с внешними системами (ISIN, LEI, Ticker и др.)
- Location с ISO стандартами (ISO 3166-1/3166-2, ISO 6709)
- Временную валидность через `validFrom`/`validTo`
- Provenance для прослеживаемости источников данных

### Frame (Кадр Данных)

Неизменяемый артефакт, представляющий результат одного цикла работы Scout Engine. Содержит извлеченные факты и полную метаинформацию о процессе извлечения.

### Lens (Линза)

Конфигурационный объект, определяющий пользовательский срез графа знаний для мониторинга и анализа. Описывает набор отслеживаемых HorizonFragment, их взаимосвязи и правила визуализации.

Lens поддерживает два типа адресации в поле `scope`:
- **Статические canonical paths**: прямые ссылки на конкретные сущности (например, `/org/tsmc`, `/fac/tsmc/fab-21`)
- **QueryAddress**: динамические запросы, разрешаемые в множество canonical paths (например, `/query/sector:/Technology/Semiconductors/**`)

Подробнее см. [QueryAddress](architecture/query-address.md).

### ExposurePath (ранний фундамент)

ExposurePath — **канонический, типизированный артефакт** вида “почему X экспонирован к Y”.

- В `Lens` хранится массив строк: `exposurePaths[]` (канонические ключи)
- Полные детали хранятся отдельными документами в `horizon_exposure_paths` (для последующей сложной логики: пути, evidence, версии во времени)

Канонический ключ:

`/exposure/<EXPOSURE_TYPE>/objective/<kind>/<id>[/related/<kind>/<id>][/path/<pathId>]`

Реализованный `ExposureType`:
- `SUPPLY_CHAIN_RISK`

### DependencyEdge (Ребро Зависимости)

Направленная типизированная связь между двумя HorizonFragment, описывающая характер и силу влияния одного узла на другой.

### Scout Engine

Подсистема автоматического сбора и верификации данных через архитектуру «Зал Суда» с использованием LLM-агентов (Scout, Critic, Resolver).

### Propagation Engine

Подсистема распространения изменений в графе зависимостей с использованием алгоритма затухания волны (Wave Damping) и порогов значимости (Delta Threshold).

### Watchdog

Фоновый процесс мониторинга внешних источников на предмет изменений и инициирования запуска Scout Engine для обновления HorizonFragment.

## Связь с основной системой

Система мониторинга горизонта интегрирована в моноядро Sanskrite и использует:

- **Firestore** — основное хранилище данных
- **AI система** (`internal/ai`) — для LLM вызовов агентов
- **Agents** (`internal/agents`) — вспомогательная оркестрация там, где подключено к основному приложению
- **AgentMesh** (`internal/agentmesh`) — отдельная подсистема агентов (см. `backend/internal/agentmesh/README.md`)
- **Realtime** (`internal/realtime`) — Pub/Sub и мост обновлений для клиента
- **Мультитенантность** — через TenantID во всех моделях

Подробнее см. [Интеграция с Sanskrite](architecture/integration.md).
