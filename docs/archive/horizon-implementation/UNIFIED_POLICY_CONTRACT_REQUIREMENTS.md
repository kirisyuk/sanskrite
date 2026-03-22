# Unified Policy Contract Requirements (Best-Effort First)

## Статус
- Тип: requirements specification
- Дата: 2026-02-13
- Ревизия: v2
- Область: `backend/internal/horizon/policy`, `backend/internal/horizon/scout`, `backend/internal/horizon/repo`, `backend/internal/horizon/models`, `backend/internal/horizon/edgesemantics`, `backend/internal/horizon/naming`
- Ограничение этого документа: требования и дизайн-контракт

### Статус реализации (v2)

| Требование | Статус | Подробности |
|---|---|---|
| 5. Policy каталог (все policy существуют) | ✅ | Все 10 policy из каталога реализованы и живут в нужных пакетах |
| 6. Action-контракт (allow/penalty/quarantine) | ✅ | `action_contract.go` реализует нормализацию, `MergePolicyStatus` монотонен |
| 7.1 Unknown edge type fallback | ✅ | `edge_canonicalization_policy.go` rewrites unknown → INFLUENCES + penalized |
| 7.2 Canonicalization едина | ✅ | `ApplyEdgeCanonicalizationPolicy` — единый entrypoint (path-aware) |
| 7.3 Transition fallback | ✅ | `transition_unclassified` в `edge_canonicalization_policy.go`, единообразно |
| 7.4 Prefix policy | ✅ | `fragment_path_policy.go` — единый справочник |
| 8.1 Формат префикса | ✅ | `[HPolicy:<name>:<stage>]` в `logger.go` |
| 8.2 Обязательные поля | ⚠️ | `LogPolicyEvent` есть, но не enforce required fields (TODO) |
| 8.3 No-silent-fail | ⚠️ | Логируется, но ~15 `fmt.Printf` в engine.go/engine_ext.go ещё не мигрированы |
| 9. Name/Decide/Explain на всех policy | ✅ | Реализовано на всех, включая `HumanInTheLoopPolicy` |
| 10.2.6 edge_materializer → unified policy | ✅ | REGULATES swap и ValidateEdgeDirection перенесены в policy-слой |
| 10.2.7 engine_ext → unified policy | ✅ | Аналогично edge_materializer |
| 10.2.10 HumanInTheLoopPolicy interface | ✅ | Name/Decide/Explain реализованы |
| 13. Feature flag | ✅ | `HORIZON_UNIFIED_POLICY_CONTRACT` существует |

## 1. Контекст и принцип

Система работает в режиме `best effort`: наличие данных лучше их отсутствия. Это означает:
1. Policy-слой не должен маскировать проблему удалением/блокировкой данных.
2. Ошибка/неопределенность должна приводить к сохранению артефакта с пониженным доверием, а не к потере артефакта.
3. Любое отклонение от канона должно быть прозрачно в логах и policy qualifiers (`policyStatus`, `policyReason`, `policyPenalty`).

## 2. Проблема текущего состояния (подтверждено кодом)

### 2.1 Декларируемый policy-интерфейс не покрывает весь фактический policy-контур
- Единый интерфейс Policy есть: `backend/internal/horizon/policy/policy.go:13`.
- Фактические бизнес-правила распределены по множеству мест (scout/repo/policy_checker), включая дублированную canonicalization/validation логику.

### 2.2 `reject` семантически блокирующий, но фактически неблокирующий
- ValidationPolicy возвращает `reject` как строгий исход: `backend/internal/horizon/policy/validation_policy.go:197`.
- Runtime apply-layer трактует `reject` как non-blocking warning: `backend/internal/horizon/policy/validation_apply.go:18`.
- Результат: терминологическое противоречие и риск неверной интерпретации сигналов.

### 2.3 Разная трактовка unknown edge type
- `edgesemantics` считает unknown edge type невалидным: `backend/internal/horizon/edgesemantics/direction_policy.go:502`.
- `TaxonomyRules` считает отсутствие правила валидным (`No rule found = valid`): `backend/internal/horizon/policy/taxonomy_rules.go:574`.
- Результат: ingestion path и checker path могут давать противоположные решения.

### 2.4 Canonicalization матрица используется неединообразно
- В ingestion/repo применяется path-aware canonicalization: `CanonicalizeEdgeWithPaths` в `backend/internal/horizon/scout/edge_materializer.go:241` и `backend/internal/horizon/repo/dependency_edges.go:152`.
- В PolicyChecker используется только `CanonicalizeEdge` (без path-aware части): `backend/internal/horizon/policy/policy_checker.go:1394`.
- Результат: checker может предлагать иные фиксы, чем runtime.

### 2.5 Переходный fallback `transition_unclassified` не сквозной
- Реализован в scout materialization: `backend/internal/horizon/scout/edge_materializer.go:263`, `backend/internal/horizon/scout/engine_ext.go:4036`.
- Отсутствует как единое правило в `repo` write-path и PolicyChecker.
- Результат: одно и то же событие ведет себя по-разному в разных путях записи/проверки.

### 2.6 Неполная унификация fragment prefix policy
- Префиксы в PreCreateValidator ограничены подмножеством типов: `backend/internal/horizon/scout/precreate_validator.go:562`.
- В ValidationPolicy покрытие шире: `backend/internal/horizon/policy/validation_policy.go:630`.
- Результат: часть типов имеет разные требования в зависимости от точки входа.

### 2.7 Логгирование фрагментировано по формату префиксов и каналам
- Есть единичные `logPrefix` (например `backend/internal/horizon/policy/canonization_policy.go:47`).
- Массово используются `fmt.Printf` и разноформатные префиксы в scout (`backend/internal/horizon/scout/engine.go`, `backend/internal/horizon/scout/engine_ext.go`).
- Результат: нет сквозного трассируемого policy-audit trail.

## 3. Цели

1. Ввести единый, непротиворечивый policy-контракт с однозначной семантикой действий.
2. Перенести policy-правила в явные policy-модули с собственными именами и пакетами.
3. Убрать дублирование policy-логики вне policy-слоя (кроме thin-adapter вызовов).
4. Унифицировать behavior между scout, repo и policy_checker.
5. Закрепить strict best-effort поведение: проблемные данные сохраняются как penalized/quarantined, а не теряются.
6. Ввести сквозное логгирование с говорящими префиксами и обязательным набором полей.

## 4. Не-цели

1. Не менять бизнес-онтологию вне явно описанных fallback/transition контрактов.
2. Не делать hard reject как рабочий режим runtime.
3. Не ломать совместимость существующих qualifiers и telemetry полей.

## 5. Единый policy-каталог (обязательный)

Все policy должны быть явно именованы, размещены в профильных пакетах и вызываться централизованно.

| Policy Name | Пакет | Ответственность | Вход | Выход |
|---|---|---|---|---|
| `naming_policy` | `backend/internal/horizon/naming` | Нормализация имен/алиасов | fragment/relation names | normalized names |
| `canonization_policy` | `backend/internal/horizon/policy` | Нормализация type/path/relation token | raw tokens | canonical token |
| `fragment_validation_policy` | `backend/internal/horizon/policy` | Валидация/penalty для фрагментов | fragment + context | policy decision |
| `edge_canonicalization_policy` | `backend/internal/horizon/policy` | Единая матрица edge canonicalization (path-aware) | edge + endpoint types/paths | normalized edge + reason |
| `edge_validation_policy` | `backend/internal/horizon/policy` | Проверка направления/ограничений edge | edge + endpoint types/paths | policy decision |
| `dedup_policy_org` | `backend/internal/horizon/scout` (выделяемый policy-модуль) | Identity-anchor + name/history dedup | fragment candidate | replacement/no replacement |
| `policy_checker_policy` | `backend/internal/horizon/policy` | Post-write детерминированные и LLM-checks | created/modified artifacts | violations |
| `enforcement_policy` | `backend/internal/horizon/policy` | Исполнение corrective tasks | violations | enforcement results |
| `propagation_policy` | `backend/internal/horizon/policy` | Правила распространения метрик | metric + relation | propagate/skip |
| `hitl_policy` | `backend/internal/horizon/policy` | Human-in-the-loop access/approval | actor + tenant + action | allow/deny + reason |

## 6. Единый action-контракт (runtime)

### 6.1 Допустимые action

1. `allow`
2. `allow_with_penalty`
3. `auto_fix`
4. `reverse`
5. `rewrite_type`
6. `quarantine_soft`

### 6.2 Нормализация legacy actions

1. `reject` обязан маппиться в `quarantine_soft` без drop данных.
2. `penalty` обязан маппиться в `allow_with_penalty`.
3. `quarantine` обязан маппиться в `quarantine_soft`.

### 6.3 Монотонность policyStatus

Разрешенные значения: `clean`, `auto_fixed`, `penalized`, `quarantined`.

Переходы только в сторону усиления строгости:
1. `clean -> auto_fixed`
2. `auto_fixed -> penalized`
3. `penalized -> quarantined`

Снижение строгости в рамках одного прохода запрещено.

## 7. Обязательные унификации (what/why/how)

### 7.1 Unknown edge type / unknown direction

Что создается:
1. Единый fallback policy-rule в `edge_validation_policy`.

Что меняется:
1. Убирается расхождение между `edgesemantics` и `taxonomy_rules`.

Как:
1. Если тип неизвестен, edge сохраняется как weak-link (`INFLUENCES`) с `policyStatus=penalized` и reason.
2. Не допускается silent-allow без policy annotation.

Кодовые референсы текущего рассогласования:
1. `backend/internal/horizon/edgesemantics/direction_policy.go:502`
2. `backend/internal/horizon/policy/taxonomy_rules.go:574`

### 7.2 Canonicalization едина во всех путях

Что создается:
1. Единый entrypoint `edge_canonicalization_policy` (path-aware).

Что меняется:
1. `policy_checker` должен использовать тот же canonicalization entrypoint, что ingestion/repo.

Как:
1. Вынести `CanonicalizeEdgeWithPaths` orchestration в policy-слой.
2. Запретить локальные матрицы canonicalization вне policy-слоя.

Кодовые референсы:
1. `backend/internal/horizon/scout/edge_materializer.go:241`
2. `backend/internal/horizon/repo/dependency_edges.go:152`
3. `backend/internal/horizon/policy/policy_checker.go:1394`

### 7.3 Transition fallback сквозной

Что создается:
1. Единое transition fallback правило в `edge_canonicalization_policy`.

Что меняется:
1. Правило `transition_unclassified` применяется в scout, repo и checker одинаково.

Как:
1. Для transition-like input без точной классификации сохранять edge как `INFLUENCES`.
2. Обязательно выставлять `policyStatus=penalized`, `policyReason=transition_unclassified`, `policyPenalty`.

Кодовые референсы:
1. `backend/internal/horizon/scout/edge_materializer.go:263`
2. `backend/internal/horizon/scout/engine_ext.go:4036`

### 7.4 Prefix policy унифицируется и расширяется

Что создается:
1. Общий справочник expected canonical prefixes (`fragment_path_policy`).

Что меняется:
1. `PreCreateValidator` и `ValidationPolicy` используют один источник.

Как:
1. Убрать дублированные `expectedPathPrefix` из разных мест.
2. Покрыть одинаковый перечень типов во всех входах.

Кодовые референсы:
1. `backend/internal/horizon/scout/precreate_validator.go:562`
2. `backend/internal/horizon/policy/validation_policy.go:630`

## 8. Сквозное логгирование (обязательный стандарт)

### 8.1 Формат префикса

Единый префикс:

`[HPolicy:<policy_name>:<stage>]`

Где:
1. `<policy_name>` из каталога раздела 5.
2. `<stage>` один из: `input`, `decision`, `apply`, `fallback`, `result`, `error`.

Примеры:
1. `[HPolicy:edge_validation_policy:decision]`
2. `[HPolicy:edge_canonicalization_policy:fallback]`
3. `[HPolicy:fragment_validation_policy:apply]`

### 8.2 Обязательные поля в каждом policy-логе

Лог обязан содержать key=value поля:
1. `trace_id`
2. `tenant_id`
3. `job_id` (если есть)
4. `fragment_id` и/или `edge_id`
5. `source_type`, `target_type`, `relation_type` (для edge)
6. `action`
7. `policy_status`
8. `policy_reason`
9. `policy_penalty`
10. `decision_path`
11. `rule_id` (если есть)
12. `confidence`
13. `origin_mode` (если есть)

### 8.3 No-silent-fail правило

1. Любой fail-open/fallback обязан логироваться с stage `fallback`.
2. Любая ошибка policy-вычисления обязана логироваться с stage `error` и `error_class`.
3. Запрещены неструктурированные `fmt.Printf` для policy-событий.

Текущее расхождение по префиксам и каналам видно в:
1. `backend/internal/horizon/policy/canonization_policy.go:47`
2. `backend/internal/horizon/scout/engine_ext.go` (много `fmt.Printf` с разными префиксами)
3. `backend/internal/horizon/scout/engine.go` (много `fmt.Printf` для policy-related событий)

## 9. Требования к структуре кода

1. Код policy-решений должен жить только в policy-пакетах.
2. Scout/repo/checker используют только policy API и handlers.
3. Дублирование разрешено только в thin adapters (маппинг входов/выходов), без бизнес-правил.
4. Каждый policy обязан иметь:
- `Name()`
- `Decide()`
- `Explain()`
- unit tests на deterministic ветки
- structured logs по стандарту раздела 8

## 10. План изменения файлов (create/modify)

### 10.1 Create

1. `backend/internal/horizon/policy/action_contract.go`
- Нормализация action (`reject -> quarantine_soft`, `penalty -> allow_with_penalty`).

2. `backend/internal/horizon/policy/edge_canonicalization_policy.go`
- Единый path-aware canonicalization entrypoint.
- Включает transition fallback (`transition_unclassified`).

3. `backend/internal/horizon/policy/fragment_path_policy.go`
- Единый expected-prefix справочник для всех fragment types.

4. `backend/internal/horizon/policy/logger.go`
- Policy logger c префикс-стандартом и обязательными полями.

5. `backend/internal/horizon/policy/policy_registry.go`
- Явная регистрация policy по именам.

### 10.2 Modify

1. `backend/internal/horizon/policy/validation_apply.go`
- Привести actions к unified action-contract (`quarantine_soft`).

2. `backend/internal/horizon/policy/validation_policy.go`
- Возвращать только action из единого контракта.
- Использовать `fragment_path_policy` как source of truth.

3. `backend/internal/horizon/policy/taxonomy_rules.go`
- Убрать поведение `No rule found = valid` для edge direction.
- Делегировать в unified edge validation semantics.

4. `backend/internal/horizon/policy/policy_checker.go`
- Использовать `edge_canonicalization_policy` вместо direct `CanonicalizeEdge`.

5. `backend/internal/horizon/scout/precreate_validator.go`
- Использовать общий `fragment_path_policy`.
- Убрать локальную дублирующую prefix-матрицу.

6. `backend/internal/horizon/scout/edge_materializer.go`
- Вызов только unified `edge_canonicalization_policy` и `edge_validation_policy`.
- Логгирование через policy logger.

7. `backend/internal/horizon/scout/engine_ext.go`
- Аналогично `edge_materializer.go`.

8. `backend/internal/horizon/repo/dependency_edges.go`
- Тот же unified policy pipeline и transition fallback.

9. `backend/internal/horizon/edgesemantics/direction_policy.go`
- Привести unknown behavior к unified контракту.

10. `backend/internal/horizon/policy/human_in_the_loop_policy.go`
- Привести к явному policy-контракту (`Name/Decide/Explain`) либо официально вынести из policy-catalog.

### 10.3 Optional cleanup

1. `backend/internal/horizon/scout/engine.go`
- Мигрировать policy-related `fmt.Printf` в structured policy logger.

## 11. Acceptance criteria

1. Один и тот же входной артефакт дает одинаковое policy-решение в scout/repo/checker.
2. Нет code path, где policy-rule живет только локально и не представлен в policy-пакете.
3. Для unknown/ambiguous edge нет drop behavior, только best-effort persist + annotation.
4. `transition_unclassified` работает одинаково во всех write/check путях.
5. Prefix-политика едина и полна для всех fragment types.
6. Все policy-решения и fallback имеют сквозные структурированные логи с единым префиксом.

## 12. Тестовые требования

1. Contract tests на action normalization (`reject`, `penalty`, `quarantine`).
2. Alignment tests: scout vs repo vs checker на одном наборе edge cases.
3. Unknown edge type tests: всегда best-effort persist + penalized reason.
4. Transition tests: classified and unclassified flows.
5. Fragment prefix tests: единый справочник для precreate + validation.
6. Logging tests: наличие обязательных полей и корректного префикса.

## 13. Rollout

1. Feature flag `HORIZON_UNIFIED_POLICY_CONTRACT=1`.
2. Shadow mode: считать новое решение параллельно старому, сравнивать telemetry.
3. После стабилизации перевести все runtime paths на unified policy pipeline.

## 14. Связанные документы

1. `docs/archive/horizon-implementation/INTERNAL_FIRST_GROUNDING_IMPLEMENTATION_PLAN.md`
2. `docs/archive/horizon-implementation/ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md`
3. `docs/archive/horizon-implementation/BIG_BANG_EDGE_POLICY_REMEDIATION_PLAN.md`
