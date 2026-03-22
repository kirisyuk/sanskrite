# Инвентаризация: Intent vs Domain Refactoring

## 1. Domain fragments — создание и чтение

| Файл | Функция/место | Действие |
|------|---------------|----------|
| `scout/engine_ext.go` | `executeDomainExpansionJob` ~2191-2284 | Create/Update domain fragment |
| `scout/engine_ext.go` | `executeDomainOverlayJob` ~2646-2713 | Create/Update domain fragment (strategic overlay) |
| `horizon/service.go` | `SearchDomainFragments` | Поиск DOMAIN fragments |
| `horizon/service.go` | `ResolveDomainByPath` | Резолв domain по path |
| `http/horizon/domain_answers.go` | `RunDomainAnswer` | Запуск DomainContractEngine для domain fragment |
| `http/horizon/lens_artifacts.go` | ~258, 306 | Фильтрация domain paths |
| `repo/fragments.go` | — | Общая коллекция horizon_fragments |

## 2. Domain-domain и domain-entity relations

| Файл | Relation types | Действие |
|------|----------------|----------|
| `scout/engine_ext.go` | FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, RELATED_DOMAIN, AFFECTS | Материализация в executeDomainExpansionJob |
| `policy/taxonomy_rules.go` | FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, RELATED_DOMAIN, AFFECTS | Validation rules |
| `models/edge_canonicalization.go` | DOMAIN→entity, DOMAIN→DOMAIN | Canonicalization |
| `edgesemantics/direction_policy.go` | DOMAIN source/target types | Direction validation |
| `models/dependency_edge.go` | DependencyTypeFocusesOn, etc. | Type definitions |

## 3. Lens scope с /domain/ путями

| Файл | Место | Действие |
|------|-------|----------|
| `lens/service.go` | CreateLens, UpdateLens | scope может содержать /domain/ из intent resolution |
| `lens/service.go` | ~766 | Domain в scope → всегда создавать job (isFragmentFreshEnough=false) |
| `lens/service.go` | ~151 | ScopeFragments: skip /domain/ при resolve |
| `scout/engine_ext.go` | `contractTypesFromLensScope` | Извлечение contract types из scope для ensureJobDomainMode |
| `scout/engine_ext.go` | ~740-745 | directPaths: exclude /domain/ для Phase2 |
| `scout/engine_ext.go` | ~2047 | existingEntities из lens.Scope |

## 4. Domain expansion — entity spawning

| Файл | Место | Действие |
|------|-------|----------|
| `scout/engine_ext.go` | ~1995-2120 | Domain research: EnrichFragmentWithGrounding "Expand into 5-10 first-order entities" |
| `scout/engine_ext.go` | ~2151-2159 | firstOrder из research relations |
| `scout/engine_ext.go` | ~2285+ | Materialize edges (domain→entity, domain→domain) |
| `scout/engine_ext.go` | ~2380+ | Create child jobs для first-order entities |
| `scout/engine_ext.go` | ~2325-2425 | Create edges: facet_geo, facet_*, first_order_entity, related_domain, affected_domain |

## 5. Job creation для /domain/ target

| Файл | Место | Действие |
|------|-------|----------|
| `lens/service.go` | CreateLens | Jobs для каждого path в scope (включая /domain/) |
| `scout/engine.go` | ~2771-2808 | if target /domain/ → pipeline.Run (domain expansion) |
| `scout/engine.go` | ~933-956 | isDomainJob checks |
| `scout/engine.go` | ~3255 | defaultType=Domain для /domain/ path |

## 6. ensureJobDomainMode — источники

| Источник | Файл | Приоритет |
|----------|------|-----------|
| path /domain/v1/ | engine_ext.go ~1648 | 1 |
| job.State["domains"] | engine_ext.go ~1655 | 2 |
| lens scope (contractTypesFromLensScope) | engine_ext.go ~1677 | 3 |
| specialties fallback | engine_ext.go ~1685 | 4 |

## 7. Intent resolution — domains output

| Файл | Структура | Действие |
|------|-----------|----------|
| `scout/agents/intent_resolver_agent.go` | DomainResolution: CanonicalPath, ContractType, etc. | Возвращает domains с /domain/ путями |
| `scout/agents/domain_resolver_agent.go` | Аналогично | Domain resolution prompt |
| Lens ViewConfig["domains"] | Массив domain objects | Хранение результата intent resolution |

## 8. Другие зависимости

| Файл | Зависимость |
|------|-------------|
| `domain/engine.go` | Domain contract engine для /domain/ paths |
| `domain/resolver.go` | Resolve domain path |
| `http/horizon/domain_overlay.go` | Strategic overlay для domain |
| `scout/domain_contract_runner.go` | Pipeline для domain jobs |
| `scout/pipeline/passes.go` | BaseEnrichmentPass → domain expansion |
| `taxonomy/paths.go` | ValidateCanonicalPath для /domain/ |
| `policy/fragment_path_policy.go` | FragmentTypeDomain → /domain/ prefix |
