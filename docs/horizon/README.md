# Horizon

Horizon — платформа для графового анализа сущностей реального мира (организации, объекты инфраструктуры, гео-регионы) с автоматическим сбором, верификацией, канонизацией и propagation сигналов. SoT по поведению — код в `backend/internal/horizon/` + `backend/internal/http/horizon/` + UI в `web/src/pages/HorizonPage.tsx`.

## Обязательное чтение

Перед любыми правками в supply-chain или фрагментном ядре:

- [architecture/supply-chain-canonical-control-plane.md](architecture/supply-chain-canonical-control-plane.md)
- [../canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md](../canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md)
- [../canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md](../canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md)
- [../AGENTS.md](../../AGENTS.md) — модель мышления для агентов

## Архитектура

- [architecture/overview.md](architecture/overview.md) — высокоуровневая архитектура
- [architecture/signal-landing-plane.md](architecture/signal-landing-plane.md) — первый слой pipeline: event → impact claims → landed refs → warmup
- [architecture/home-workspace-runtime.md](architecture/home-workspace-runtime.md) — Home-first entry surface
- [architecture/materialized-artifacts.md](architecture/materialized-artifacts.md)
- [architecture/decision-layer-design.md](architecture/decision-layer-design.md)
- [architecture/workscope.md](architecture/workscope.md)
- [architecture/canonicalization.md](architecture/canonicalization.md)
- [architecture/query-address.md](architecture/query-address.md)
- [architecture/ontology-facts.md](architecture/ontology-facts.md)
- [architecture/core-layer-contracts-and-workscope-v2.md](architecture/core-layer-contracts-and-workscope-v2.md)
- [architecture/risk-ready-supply-chain-graph-v3-plan.md](architecture/risk-ready-supply-chain-graph-v3-plan.md)
- [architecture/structural-org-supply-chain-model.md](architecture/structural-org-supply-chain-model.md)
- [architecture/supply-chain-relation-endpoint-kinds.md](architecture/supply-chain-relation-endpoint-kinds.md)
- [architecture/operational-metric-hydration-and-simulation-early-warning.md](architecture/operational-metric-hydration-and-simulation-early-warning.md)
- [architecture/agentic-scouting-environment.md](architecture/agentic-scouting-environment.md)
- [architecture/agentic-scout-gap-analysis.md](architecture/agentic-scout-gap-analysis.md)
- [architecture/code-organization.md](architecture/code-organization.md)
- [SUPPLY_CHAIN_PIPELINE_MAP.md](SUPPLY_CHAIN_PIPELINE_MAP.md)

## Таксономия

Horizon использует AgentHorde-таксономию (FragmentClass / Subtype / RelationName). SoT:

- Go: `backend/internal/taxonomy/ah/taxonomy_generated.go`
- TS: `web/src/lib/taxonomy.generated.ts`
- Живые доки: [architecture/taxonomy.md](architecture/taxonomy.md), [../taxonomy/](../taxonomy/)

## Рабочие процессы

- [workflows/scout-workflow.md](workflows/scout-workflow.md)
- [workflows/propagation-workflow.md](workflows/propagation-workflow.md)
- [workflows/lifecycle.md](workflows/lifecycle.md)
- [implementation/signal-landing-and-analytical-runtime-status-2026-04-03.md](implementation/signal-landing-and-analytical-runtime-status-2026-04-03.md)

## UX

- [ux/user-journey.md](ux/user-journey.md)
- [ux/interface-principles.md](ux/interface-principles.md)
- [ux/horizon-watch.md](ux/horizon-watch.md)

## API

- [api/endpoints.md](api/endpoints.md) — `/api/horizon/*`. Схемы — в Go-типах (`backend/internal/http/horizon/*.go`, `backend/internal/horizon/models/*.go`); wire-контракт JSON выводится из `json:` тегов.

## Реализация

- [implementation/roadmap.md](implementation/roadmap.md) — **единственный актуальный статус** этапов
- [implementation/technical-decisions.md](implementation/technical-decisions.md)
- [../archive/horizon-implementation/](../archive/horizon-implementation/) — архив планов и ТЗ
