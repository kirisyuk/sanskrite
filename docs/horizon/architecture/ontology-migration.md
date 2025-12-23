# Миграция базовой онтологии в Horizon (строгий контракт)

## Контекст и цель

Базовая онтология (“ontology_*”) изначально проектировалась как широкий слой артефактов (entities/events/claims/metrics/trends/scenarios/causal/uncertainty), где возможны неполнота и fuzziness (особенно на входе из LLM).

Horizon появился как ответ на проблему расплывания: **нужна строгая, детерминированная система**, которая:
- работает предсказуемо в проде,
- даёт воспроизводимые результаты,
- умеет explain/trace “почему так”,
- не взрывается по объёму из‑за propagation.

Цель этого документа: зафиксировать **как онтология “въезжает” в Horizon** под единым смысловым зонтиком, без размывания ядра.

## Принцип: “Strict core first”

1) **Сначала загоняем всё, что попадает в Horizon, в рамки** (Horizon‑grade контракт).  
2) **Потом** явно определяем “fuzzy зоны” и допускаемые вольности — но как исключения, а не как норма.

## Термины

- **Ontology artifacts**: коллекции `ontology_entities`, `ontology_events`, `ontology_claims`, `ontology_relations`, `ontology_metric_states`, `ontology_trends`, `ontology_causal_links`, `ontology_scenarios`, `ontology_uncertainty_models`, и т.д.
- **OntologyFact**: унифицированный “факт/фрейм” (`ontology_facts`), который уже задуман как:
  - инкрементальный вход для Watchdog,
  - Vertex‑индексируемый документ (через `vectorText`),
  - фильтруемый по `canonicalPaths`/`actorPaths`/`locationPath`/`spherePaths` и времени.
- **Horizon‑grade**: подмножество артефактов, которые удовлетворяют строгому контракту и допускаются в мониторинг/propagation/роллапы.

## Horizon‑grade контракт (что обязательно)

Любой “сигнал”, который используется Horizon (для Watchdog/propagation/UX‑роллапов), обязан иметь:

- **Привязка к графу**
  - `canonicalPaths` **не пусто** (куда “прикрепляется” сигнал в Horizon),
  - опционально: `actorPaths`, `locationPath` (`/loc/...`), `spherePaths`.
- **Контролируемый тип**
  - `kind` и `type` — из контролируемого словаря (runtime taxonomy: allowed + synonyms).
- **Время и модальность**
  - минимум: `observedAt` или `occurredAt`,
  - далее: `effectiveFrom/effectiveTo` (окно действия) и `modality` (observed/reported/predicted/hypothetical).
- **Идентичность и дедуп**
  - детерминированный identity‑ключ (для устранения “почти одинаковых” сигналов при propagation).
- **Uncertainty / конфликтность**
  - `confidence` + возможность пометить конфликт/неопределённость и политику разрешения.
- **Explainability**
  - любая выдача (raw или rollup) обязана ссылаться на доказательную базу (см. ниже).

### Provenance как отдельный слой

Provenance не храним “простынёй” внутри Horizon‑узла.
Horizon‑grade сигнал **ссылается** на доказательную базу через онтологические ноды/факты:
- `ontology_facts` (унифицированные фреймы),
- `ontology_events` / `ontology_claims` (первоисточники смысла),
- `ontology_sources` (источники),
- `rag_documents`/`rag_chunks` (контент).

## Правило миграции: через “signal layer”

Мы не “переписываем онтологию в Horizon”. Вместо этого вводим понятный слой:

### 1) Источник истины для сигналов (минимум)

Базовый поток для Horizon — это `ontology_facts`.
Он уже содержит ключевые поля (`kind`, `type`, `confidence`, `occurredAt/observedAt`, `canonicalPaths/...`, `sourceRefs`).

### 2) First‑class артефакты сигналов

Отдельные артефакты базовой онтологии подключаются как сигнал‑носители:
- `Trend` → сигнал “trend”
- `Concept(kind=Risk/Opportunity)` (+ связи/qualifiers) → сигнал “risk/opportunity”
- `CausalLink` → причинность (не сводить к корреляции)
- `Scenario` → predicted/hypothetical слой
- `UncertaintyModel` → явный учёт конфликтов/неопределённости

Все они входят в Horizon только если удовлетворяют Horizon‑grade контракту привязки (paths/time/type).

## Quarantine / “не прошёл строгий контракт”

Для всего, что не проходит контракт, нужен явный режим:
- либо статус/метка “draft/unresolved” и исключение из propagation,
- либо отдельная “quarantine” зона (коллекция/префикс), где можно доработать нормализацию.

Это ключевой механизм, чтобы ядро Horizon не расплывалось.

## План миграции (высокоуровневый)

1) **Определить словари (runtime taxonomy)**
   - allowed `kind/type` для сигналов (risk/opportunity/trend/direction + event/constraint/anomaly/hypothesis при необходимости)
   - synonyms → canonical type.
2) **Единая нормализация путей**
   - обеспечить, что `canonicalPaths/locationPath/actorPaths/spherePaths` генерируются/валидируются единым кодом.
3) **Horizon‑grade “gate”**
   - формальная валидация контрактов + дедуп‑ключи.
4) **Backfill**
   - прогнать исторические ontology artifacts → привести к Horizon‑grade/или quarantine.
5) **UX/API**
   - выдача “signals for fragment”: raw + rollups + explain/trace.
6) **Propagation**
   - policies‑as‑data + handlers по `(kind,type)`; derived signals отделены от raw.

## См. также

- `docs/horizon/architecture/ontology-facts.md`
- `docs/horizon/architecture/taxonomy.md`
- `docs/horizon/implementation/roadmap.md`


