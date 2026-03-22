# Intent vs Domain Execution Contract

## Статус

**Утверждено** — концептуальное разделение для дальнейшего рефакторинга.

---

## 1. Суть разделения

| Концепция | Определение | Примеры |
|-----------|-------------|--------|
| **Intent** | Живой вопрос пользователя: *что* он хочет узнать. Ситуативен, может быть много на линзе. | market-sizing, technology-landscape, supply-chain mapping, competitive-landscape, «Как А повлияет на Б» |
| **Domain (Execution Contract)** | Контракт исполнения: определяет scope и *как* пайп отрабатывает правила. Стабилен для линзы. | supply_chain, governance, market, strategic, assurance, policy_impact |

**Intent** — живой вопрос, на него не нужен ответ сразу (кроме проброски по job). После создания линзы пользователь может задавать и другие вопросы — они лягут в свою логику. «Как А повлияет на Б» — чисто ситуативная история, её нужно отработать, но мешать с доменным контрактом не надо.

**Domain Execution Contract** — определяет scope и как пайп отрабатывает правила. Один контракт покрывает множество intent'ов с одинаковым способом исполнения.

---

## 2. Domain как контракт

Domain — это **не сущность**, а **контракт исполнения**:

- **Определяет scope** — какие сущности, рёбра, метрики в фокусе.
- **Как исполнять** — scout, research, critic: какие relation types извлекать, какие стратегии Research/Critic запускать, какой allowlist рёбер.
- **Как отвечать** — структура ответа, приоритеты, формат.

Один execution contract покрывает множество intent'ов с одинаковым способом исполнения.

---

## 3. Intent как категория: много на линзе

**Линза может иметь много Intent'ов в scope.** Это отдельная категория от Domain Execution Contract.

- **Domain** — один (или composite) на линзу. Стабилен. Определяет, как пайп работает.
- **Intent** — множество. Живые вопросы. Ситуативны. Могут добавляться после создания линзы.
- Intent не определяет execution — это вопрос, на который мы отвечаем в рамках контракта.

---

## 4. Текущее смешение в коде

Intent и Domain (execution contract) смешаны в архитектуре:

| Что по сути | Как сейчас называется | Где в коде |
|-------------|------------------------|------------|
| Intent (вопрос пользователя) | "domain" из intent resolution | `domain_resolver_agent`, `intent_resolver_agent` → `contractType`, `domains` в ViewConfig |
| Execution contract | "domain mode" | `supply_chain`, `governance`, `market` и т.д. в `taxonomy`, `domain/steering_profiles.go` |

### 4.1 Проявления смешения

1. **Именование** — термин "domain" используется и для intent'а, и для контракта исполнения.
2. **Структура** — путь `/domain/v1/{contractType}/...` одновременно:
   - нода в графе (domain как сущность),
   - носитель intent'а (`contractType`),
   - источник для выбора execution contract (через маппинг).
3. **Поток данных** — intent resolution → "domains" → `contractType` → domain mode. Intent и контракт идут одной цепочкой без явного разделения.

### 4.2 Размножение "доменов"

Intent resolution выдаёт много contract types (market-opportunity, competitive-landscape, market-sizing, technology-landscape, ecosystem-mapping и т.д.). Каждый contract type маппится в steering profile → domain mode.

Но **реальных execution contracts мало**:
- supply_chain — цепочки, DEPENDS_ON, SUPPLIES_TO, PRODUCES
- governance — OWNS, CONTROLS, REGULATES, MANAGES
- market — PARTICIPATES_IN, HAS_MARKET_SEGMENT
- strategic — GATEKEEPS_DECISION, USES_MECHANISM
- assurance — ASSURES, AUDITED_BY
- policy_impact — propagation-специфика

Много intent'ов, мало контрактов. Domain создаётся/выбирается, когда есть **отдельный контракт исполнения**, а не по каждому удобному случаю.

---

## 5. Целевая модель

```
Intent (много, детальных)     →     Execution Contract (мало, фиксированных)
     market-sizing                        market
     competitive-landscape          →     market
     market-entry                        market
     supply-chain                  →     supply_chain
     value-chain                         supply_chain
     technology-landscape          →     supply_chain | market | general (по решению)
```

**Правило**: Domain (execution contract) создаётся/выбирается только когда есть **отдельный способ исполнения**. Не плодить контракты для каждого intent'а.

---

## 6. Маппинг

| Уровень | Роль | Количество |
|---------|------|------------|
| Intent / contractType | Детализация вопроса пользователя | Много (market-sizing, supply-chain, ecosystem-mapping...) |
| Steering profile | Промежуточная группировка | Средне (competitive_landscape, supply_chain, regulatory_exposure...) |
| Domain mode (execution contract) | Контракт исполнения | Мало (supply_chain, governance, market, strategic, assurance, policy_impact) |

Маппинг Intent → Execution Contract должен быть **консолидирующим**: несколько intent'ов → один контракт.

---

## 7. На что влияет Domain Execution Contract (не терять при разделении)

Domain mode (`domainModes` / `domainMode` в job state) влияет на множество компонентов. При разделении Intent и Domain **эту логику сохранять** — она опирается на execution contract, а не на intent.

| Компонент | Влияние | Файлы |
|------------|---------|-------|
| **Scout extraction** | Allowlist relation types для извлечения; domain guardrail; relation catalog override | `scout_agent.go`: `extractionAllowedRelationTypesByDomainModes`, `extractionDomainGuardrailBlock`, `extractionDomainRelationCatalogOverrideBlock`, `buildExtractionRelationsRulesBlock` |
| **Scout engine** | Фильтрация relations по allowlist; unit qualifiers; supply chains on/off | `engine.go`: `filterDiscoveredRelationsByDomainModes`, `ensureUnitQualifiersForRelations`, `supplyChainsEnabled` |
| **Research** | Выбор стратегий (supply_chain, governance, market и т.д.); fallback в monolithic при general | `research_pipeline.go`: `strategiesForModes`; `research_strategies_impl.go`: специализированные промпты |
| **Critic** | Выбор валидаторов по mode; composable vs monolithic | `critic_pipeline.go`: `validatorsForModes`; `critic_composable_prompts.go` |
| **Entity types** | Какие типы сущностей в промптах Research/Critic | `taxonomy/domain_mode.go`: `EntityTypesForDomainMode`, `EntityActorTypesForDomainMode` |
| **Edge allowlist** | Какие relation types разрешены для записи/фильтрации | `scout/allowlist.go`: `EdgeAllowlistByDomainModes` |
| **Propagation** | Выбор domain adapter (policy_impact и др.); mode fallback | `propagation/domain_adapter.go`: `ForContractTypeOrMode`; `scenario_runner.go` |
| **Child jobs** | Наследование domain mode при рекурсии | `engine_ext.go`: `copyDomainModeStateToChild` |
| **Job setup** | Вычисление mode из contract types / path / lens scope / specialties | `engine_ext.go`: `ensureJobDomainMode`, `getJobDomainModes` |

**Источник domain mode**: `ensureJobDomainMode` → contract types из path, `job.State["domains"]`, lens scope, specialties fallback → `DomainModesForContractTypes` → `job.State["domainMode"]` / `job.State["domainModes"]`.

---

## 8. Связанные документы

- [INTENT_DOMAIN_REFACTORING_PLAN](../../archive/horizon-implementation/INTENT_DOMAIN_REFACTORING_PLAN.md) — план исполнения рефакторинга
- [SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ](../../archive/horizon-implementation/SCOUT_DOMAIN_PHASES_AND_COMPOSABLE_AGENTS_TZ.md) — фазы Scout, composable Research/Critic
- `backend/internal/horizon/domain/steering_profiles.go` — маппинг contractType → steering profile → domain mode
- `backend/internal/horizon/scout/engine_ext.go` — `ensureJobDomainMode`, `contractTypesFromDomainsRaw`
