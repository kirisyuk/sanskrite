# Документация Sanskrite / Horizon

Техническое имя репозитория — **Sanskrite**; продуктовый бренд — **Horizon** (Nordic Vision) (`horizon.nordicvision.io`). **Источник правды по поведению** — код (`backend/`, `web/`) и конфиги деплоя; документы здесь — карты местности, контракты и чеклисты.

## Быстрый вход

| Документ | Назначение |
|----------|------------|
| [Корневой README](../README.md) | Структура репо, локальный запуск, обзор API |
| [Обзор системы](architecture/overview.md) | Контекстная диаграмма, слои, ссылки на модули |
| [**Каталог HTTP API (платформа)**](architecture/apis.md) | `/api/v1`, `/ws`, internal tasks, Critical Chain; сверка с `main.go` |
| [Чеклист деплоя](deployment/deployment-checklist.md) | Продакшен |
| [Быстрый деплой](../DEPLOY_QUICK_START.md) | Шпаргалка у корня |

## Подсистемы

### Horizon

Граф сущностей, Lens, Scout, propagation, Horizon API и UI.

- [horizon/README.md](horizon/README.md) — навигация по разделам
- [horizon/implementation/roadmap.md](horizon/implementation/roadmap.md) — **единственный актуальный статус** этапов и компонентов
- [horizon/implementation/technical-decisions.md](horizon/implementation/technical-decisions.md) — зафиксированные решения
- [horizon/api/endpoints.md](horizon/api/endpoints.md) — спецификация `/api/horizon`

Код: `backend/internal/horizon/`, `backend/internal/http/horizon/`, UI: `web/src/pages/HorizonPage.tsx`, `web/src/stores/horizonRealtimeStore.ts`.

### Vertex AI Search (RAG)

- [architecture/vertex-ai-search.md](architecture/vertex-ai-search.md) — входная точка, ссылки на канон и скрипты

### Critical Chain

- [CriticalChain/README.md](CriticalChain/README.md)

### Агенты (платформа)

Обзорные документы перенесены под архитектуру:

- [architecture/agent-system-architecture.md](architecture/agent-system-architecture.md)
- [architecture/agent-capabilities-use-cases.md](architecture/agent-capabilities-use-cases.md)
- [architecture/agent-dynamic-planning-architecture.md](architecture/agent-dynamic-planning-architecture.md)

Код AgentMesh: `backend/internal/agentmesh/`.

## Сценарии AI и отладка

| Документ | Назначение |
|----------|------------|
| [features/ai-scenarios/genai-scenarios-summary.md](features/ai-scenarios/genai-scenarios-summary.md) | Сводка по сценариям GenAI |
| [features/ai-scenarios/gemini-scenarios.md](features/ai-scenarios/gemini-scenarios.md) | Сценарии Gemini |
| [features/ai-scenarios/openai-scenarios.md](features/ai-scenarios/openai-scenarios.md) | Сценарии OpenAI |
| [features/ai-scenarios/grok-scenarios.md](features/ai-scenarios/grok-scenarios.md) | Сценарии Grok |
| [development/DEBUGGING_AI_TASKS.md](development/DEBUGGING_AI_TASKS.md) | Отладка AI-задач и таймауты |

## Прочие фичи (обзоры)

- [features/analytics-mode-capabilities.md](features/analytics-mode-capabilities.md)
- [features/entity-deduplication-system.md](features/entity-deduplication-system.md)

## Справочники данных и домена

- [architecture/system-entities.md](architecture/system-entities.md) — сущности системы
- [horizon/SUPPLY_CHAIN_PIPELINE_MAP.md](horizon/SUPPLY_CHAIN_PIPELINE_MAP.md) — карта пайплайна supply chain (контекст Horizon)

## Архив и заметки

| Место | Содержимое |
|-------|------------|
| [archive/](archive/README.md) | Устаревшие деревья и дубликаты (risk-analysis, планы Horizon, старые Vertex-гайды) |
| [notes/](notes/README.md) | Разовые разборы, отчёты, старые деплой-заметки |

`temp-repos/**` — внешний код, не часть продуктовой документации.

## Где что класть (конвенция)

- Продукт и архитектура: `docs/architecture/`, `docs/horizon/`, `docs/deployment/`, `docs/development/`.
- Новые **долгие планы / ТЗ по Horizon**, если не хотите раздувать roadmap: сразу `docs/archive/horizon-implementation/` + одна строка в roadmap при необходимости.
- Секреты и ключи — не в git; `TEST_KEYS.md` / `paypal-credentials.md` в корне только локально и по возможности в `.gitignore`.
