# Horizon Documentation

Продуктовый бренд — **Horizon** (Prevalent). Техническое имя checkout-директории — `sanskrite` (legacy, не трогаем до отдельного PR). **Источник правды по поведению — код** (`backend/`, `web/`) и конфиги деплоя (`scripts/deploy`, `backend/cloudbuild.*.yaml`, `web/cloudbuild.deploy.yaml`); документы здесь — карты местности, контракты и чеклисты.

## Быстрый вход

| Документ | Назначение |
|----------|------------|
| [Корневой README](../README.md) | Структура репо, локальный запуск, обзор API |
| [Обзор системы](architecture/overview.md) | Контекстная диаграмма, слои, ссылки на модули |
| [**Каталог HTTP API (платформа)**](architecture/apis.md) | `/api/v1`, `/ws`, internal tasks |
| [Fork migration 2026](deployment/FORK_MIGRATION_2026.md) | Переименование в Prevalent Horizon, GCP-конвенции |
| [Infrastructure deployment](architecture/infrastructure/deployment.md) | Где лежат деплой-артефакты (SoT — `scripts/deploy`) |

## Подсистемы

### Horizon core

Граф фрагментов, Lens, Scout, propagation, supply-chain canonical control plane.

- [horizon/README.md](horizon/README.md) — навигация
- [horizon/architecture/supply-chain-canonical-control-plane.md](horizon/architecture/supply-chain-canonical-control-plane.md) — **обязательное чтение** перед supply-chain изменениями
- [horizon/implementation/roadmap.md](horizon/implementation/roadmap.md) — актуальный статус этапов
- [horizon/api/endpoints.md](horizon/api/endpoints.md) — спецификация `/api/horizon`

Код: `backend/internal/horizon/`, `backend/internal/http/horizon/`, UI: `web/src/pages/HorizonPage.tsx`.

### Таксономия (Horizon / AgentHorde)

- [taxonomy/](taxonomy/) — SoT по FragmentClass / Subtype / RelationName. Go: `backend/internal/taxonomy/ah/taxonomy_generated.go`; TS: `web/src/lib/taxonomy.generated.ts`.

### Canonical paths и продукты

- [canonical_names_and_paths/](canonical_names_and_paths/) — canonical product paths, synonyms, transforms.

### Агенты (AgentHorde)

- [agents/agenthorde-runtimes.md](agents/agenthorde-runtimes.md) — Vertex (ADK) и Azure (OpenAI) рантаймы; общие канонизатор/схемы в `agents/shared/agenthorde_common/`.
- [architecture/agent-system-architecture.md](architecture/agent-system-architecture.md)

Код AgentMesh: `backend/internal/agentmesh/`.

### Сценарии AI и отладка

| Документ | Назначение |
|----------|------------|
| [features/ai-scenarios/genai-scenarios-summary.md](features/ai-scenarios/genai-scenarios-summary.md) | Сводка по сценариям GenAI |
| [features/ai-scenarios/gemini-scenarios.md](features/ai-scenarios/gemini-scenarios.md) | Сценарии Gemini |
| [features/ai-scenarios/openai-scenarios.md](features/ai-scenarios/openai-scenarios.md) | Сценарии OpenAI |
| [features/ai-scenarios/grok-scenarios.md](features/ai-scenarios/grok-scenarios.md) | Сценарии Grok |

## Архив и заметки

| Место | Содержимое |
|-------|------------|
| [archive/](archive/README.md) | Устаревшие деревья и дубликаты |
| [notes/](notes/README.md) | Разовые разборы и отчёты |

`temp-repos/**` — внешний код, не часть продуктовой документации.

## Где что класть (конвенция)

- Продукт и архитектура: `docs/architecture/`, `docs/horizon/`, `docs/deployment/`.
- Новые долгие ТЗ по Horizon: `docs/archive/horizon-implementation/` + одна строка в roadmap при необходимости.
- Секреты и ключи — не в git.
- Принцип: если код/скрипт сам по себе честный SoT, не дублируйте в md — он протухнет быстрее, чем кто-либо его прочитает.
