# Big Bang Plan: Снижение Логических Warning В Scout/Policy

## 1. Цель и принципы

### Цель
Свести к минимуму `ValidationPolicy would reject (non-blocking)` и related drift-кейсы без потери связности графа.

### Неподвижные принципы
- `best effort` сохраняется: мы не превращаем пайплайн в fail-closed.
- Минимизируем шум через автоисправления и калибровку confidence, а не через массовый drop связей.
- Единый контракт типов/направлений обязателен для prompt, canonicalization и runtime policy.

---

## 2. Фундаментальные причины (по приоритету)

0. **Два неконсолидированных реестра direction policy.**
   `edge_normalizer.go` (`EdgeDirectionPolicies`, scout package) и `validation_policy.go` (`edgeDirectionPolicies`, policy package) содержат **независимые копии** маппинга `edgeType → validSourceTypes × validTargetTypes`. Они расходятся: например, `RELATED_TO` и `RELATIVE_OF` присутствуют в `edge_normalizer.go`, но отсутствуют в `validation_policy.go`. Это **корневая причина** "missing direction policy" ошибок при валидации ребер, которые нормализатор считает допустимыми.
1. Рассинхрон между prompt-allowlist и runtime direction policy.
2. Отсутствие direction-policy в `validation_policy.go` для `RELATED_TO`, `RELATIVE_OF` (хотя `edge_normalizer.go` и `taxonomy_rules.go` их знают).
3. Неполное покрытие canonicalization для массовых паттернов ошибок направления/типа.
4. Policy `ValidTargetTypes`/`ValidSourceTypes` слишком узкие для ряда легитимных пар (PERSON→ORG INFLUENCES, ORG→MEDIA_CHANNEL OWNS, и др.) — ложные reject'ы.
5. Type/path mismatch (`targetType` и `targetCanonicalPath` конфликтуют), включая системный GOV-ORG case (`/gov/` path + type `ORGANIZATION`).
6. `reject` non-blocking без системной деградации веса: связь остается почти в том же качестве.
7. Недостаточная операционная видимость по классам ошибок и hit-rate автофиксов.

---

## 3. Big Bang Scope (один релиз)

### 3.1 Единый контракт Edge Semantics (source of truth)
- Ввести единый реестр `relationType x sourceType x targetType`:
  - валидные пары,
  - симметричность,
  - reverse policy,
  - canonical rewrite policy,
  - confidence penalty policy.
- Генерировать из него:
  - runtime `ValidationPolicy` mapping,
  - prompt constraints для `ScoutAgent/ResearchAgent/CriticAgent`,
  - canonicalization matrix.

### 3.2 Закрыть дырки direction policy (синхронизация реестров)

**Корневой fix**: слить `edge_normalizer.go:EdgeDirectionPolicies` и `validation_policy.go:edgeDirectionPolicies` в один source of truth, генерируемый из единого контракта (3.1). До этого — синхронизировать вручную.

**Немедленно добавить в `validation_policy.go:edgeDirectionPolicies`:**

- `RELATED_TO` (PERSON <-> PERSON, symmetric) — уже есть в `edge_normalizer.go` и `taxonomy_rules.go:SymmetricEdges`.
- `RELATIVE_OF` (PERSON <-> PERSON, symmetric) — аналогично.

**Проверка**: после fix оба типа не должны попадать в `unknown edge type ... missing direction policy`.

### 3.2.1 Расширить ValidSourceTypes/ValidTargetTypes для легитимных пар (false-positive reject'ы)

Ряд пар вызывают reject не из-за семантической ошибки, а из-за слишком узких policy-списков. По данным GCP-телеметрии (nv-horizon, 2026-02-11) — это стабильные high-volume reject'ы для связей, которые осмысленны в реальном мире:

| Edge Type | Расширение | Обоснование |
|-----------|-----------|-------------|
| `INFLUENCES` | Добавить `PERSON` в ValidSourceTypes | PEP-влияние на организации — core use case (Sheikh MBS → Saudi Aramco) |
| `INFLUENCES` | Добавить `MARKET`, `SPHERE` в ValidTargetTypes | Организация влияет на рынок/сферу (OPEC → oil market) |
| `OWNS` | Добавить `MEDIA_CHANNEL` в ValidTargetTypes | Медиа-владение (MBC Group → Al Arabiya) |
| `OWNS` | Добавить `COMMUNITY` в ValidTargetTypes | Организация владеет/контролирует community structure (holding → foundation) |
| `AUDITED_BY` | Добавить `PROGRAM` в ValidSourceTypes | Госпрограммы аудитируются (GAO → government program) |
| `USES_MECHANISM` | Добавить `PROGRAM` в ValidSourceTypes | Программа использует механизм контроля (субсидии, квоты, тарифы) |
| `GATEKEEPS_DECISION` | Добавить `PERSON` в ValidSourceTypes | Персона-гейткипер стратегического решения (министр → лицензионное решение) |
| `COMPETES_WITH` | Добавить `PRODUCT` в ValidSourceTypes/ValidTargetTypes | Продукты конкурируют между собой (LNG vs pipeline gas) |
| `PARTICIPATES_IN` | Добавить `PERSON` в ValidSourceTypes | Персона участвует в рынке (инвестор, оператор) |

**Принцип**: расширяем только для пар, подтвержденных реальной телеметрией и бизнес-смыслом. Не расширяем "на всякий случай".

### 3.3 Расширить canonicalization (до policy apply)

Auto-rewrite rules для частых паттернов, подтвержденных GCP-телеметрией. Каждое правило срабатывает **до** policy apply и оставляет provenance trace.

**Retag rules (семантически близко, но неправильный тип ребра):**

| # | Pattern | Rewrite | Обоснование |
|---|---------|---------|-------------|
| 1 | `ORG → SPHERE (INFLUENCES)` | `INFLUENCES` + `edgeModality=potential` + penalty 0.15 | Связь осмысленна, но sphere — слабый target. Weak-link strategy. |
| 2 | `COMMUNITY → ORG (OWNS)` | Retag → `CONTROLS` | Династия/клан не владеет юридически, а контролирует. |
| 3 | `ORG → ORG (PRODUCES)` | Попытка retarget: если target по path == `/product/...` → fix targetType на PRODUCT. Иначе → `SUPPLIES_TO` + penalty 0.10 | Target часто mistyped как ORG, когда реально PRODUCT. |
| 4 | `PERSON → PERSON (MANAGES)` | Retag → `RELATED_TO` | В Horizon-модели MANAGES = формальная власть над ORG/FAC/PROGRAM, не над человеком. |
| 5 | `PERSON → COMMUNITY (AFFILIATED_WITH)` | Retag → `MEMBER_OF` | AFFILIATED_WITH target = ORGANIZATION. Человек + community = membership. |
| 6 | `ORG → ORG (AFFILIATED_WITH)` | Retag → `PARTNERS_WITH` | Аффилиация org-org — это партнерство/JV. AFFILIATED_WITH предназначен для COMMUNITY/PERSON → ORG. |
| 7 | `ORG → FACILITY (INVESTS_IN)` | Retag → `OWNS` при наличии ownership evidence, иначе `OPERATES_IN` + penalty 0.10 | INVESTS_IN target = ORG/PROGRAM/PRODUCT. Инвестиция в facility в Horizon = ownership. |

**Принципы:**
- Для ambiguous cases не удалять edge, а переводить в weaker canonical edge + penalty.
- Каждый retag записывает `rewriteReason` в edge provenance.
- Если retag невозможен (нет подходящего типа) → `quarantine` (см. 3.4).

### 3.4 Policy Apply: от бинарного reject к градуированному best-effort

**4 класса действия:**
- `allow` — связь валидна, проходит без изменений.
- `auto_fix` (rewrite + trace reason) — связь осмысленна, но тип/направление неправильное. Автоматически исправляется (см. 3.3).
- `allow_with_penalty` (снижение confidence) — связь допустима, но слабая/сомнительная. Сохраняется с пониженным весом.
- `quarantine` — участвует в графе, но исключается из риск-критичных расчетов по умолчанию.

**`hard_reject`** оставить только для truly-invalid инвариантов (self-loop, пустые id/path, unsafe type corruption).

#### 3.4.1 Policy status qualifier (обязателен для всех non-allow решений)

Каждое ребро/нода, прошедшее через policy apply с решением отличным от `allow`, получает явный маркер в `Qualifiers` map. Без этого маркера невозможно фильтровать quarantine в propagation, строить метрики и проводить ручной review.

**На ребрах (`DependencyEdge.Qualifiers`):**

| Key | Type | Значения | Описание |
|-----|------|----------|----------|
| `policyStatus` | string | `clean` \| `auto_fixed` \| `penalized` \| `quarantined` | Результат policy apply. Отсутствие ключа = `clean`. |
| `policyReason` | string | free-text | Человекочитаемая причина (e.g., `"retag: COMMUNITY→ORG OWNS → CONTROLS"`, `"quarantine: ORG→ORG CONSUMES, target type mismatch"`) |
| `policyPenalty` | float64 | 0.0–1.0 | Размер примененного penalty. Только для `penalized` и `quarantined`. |
| `policyTimestamp` | string (RFC3339) | — | Когда решение было принято. Для audit trail и re-evaluation. |
| `policyOriginalType` | string | e.g. `"OWNS"` | Исходный тип ребра до auto_fix retag. Только для `auto_fixed`. |

**На фрагментах (`HorizonFragment.Qualifiers` или `metadata` map):**

| Key | Type | Значения | Описание |
|-----|------|----------|----------|
| `policyStatus` | string | `clean` \| `penalized` \| `quarantined` | Результат policy apply для фрагмента (type/path mismatch и т.д.) |
| `policyReason` | string | free-text | Причина (e.g., `"type_path_mismatch: /product/brunp-recycling with type=ORGANIZATION"`) |

**Runtime-контракт фильтрации:**

- **Propagation engine**: при расчете risk paths исключать ребра с `policyStatus=quarantined`. Ребра с `penalized` участвуют, но с пониженным confidence.
- **Risk rollup**: quarantined ребра не входят в критичные exposure paths. Могут входить в расширенный контекст (explainability), но помечаются как `[quarantined]`.
- **UI/API**: quarantined ребра отображаются визуально отличимо (dashed line, muted color) и доступны для ручного review (flip `quarantined` → `clean` или `hard_reject`).
- **Метрики**: `quarantine_rate` = count(`policyStatus=quarantined`) / total edges per window.

**Жизненный цикл quarantine:**
1. Edge создается → policy apply → `policyStatus=quarantined` + reason.
2. Edge участвует в графе для поиска/контекста, но не в propagation/rollup.
3. Оператор может вручную поменять статус: `quarantined` → `clean` (false alarm) или → удалить (confirmed garbage).
4. При появлении нового auto_fix rule — batch-процесс может пересмотреть quarantined edges и применить fix.

#### 3.4.2 Explicit quarantine patterns

Корректные reject'ы — связь семантически невалидна, но удалять из графа не нужно:

| # | Pattern | Почему quarantine, а не fix | Penalty |
|---|---------|---------------------------|---------|
| 1 | `ORG → ORG (CONSUMES)` | Организация не потребляет организацию. Target должен быть PRODUCT. Без знания правильного product нельзя auto-fix. | 0.30 |
| 2 | `ORG → FACILITY (CONSUMES)` | Аналогично — target должен быть PRODUCT, не facility. | 0.30 |
| 3 | `MARKET → MARKET (PARTICIPATES_IN)` | Рынок не "участвует" в рынке. Если иерархия — должно быть HAS_SUBMARKET, но авто-ретаг рискован. | 0.25 |
| 4 | `MARKET → MARKET (COMPETES_WITH)` | Рынки не конкурируют между собой. Semantic confusion LLM. | 0.25 |
| 5 | `GEO_REGION → ORG (AUDITED_BY)` | Регион не может быть аудитируемым. Бессмыслица. | 0.40 |
| 6 | `FACILITY → ORG (OPERATES_VIA)` | OPERATES_VIA target = MEDIA_CHANNEL, не ORG. Нельзя авто-ретагнуть без потери смысла. | 0.30 |
| 7 | `MARKET → PRODUCT (HAS_SUBMARKET)` | Подрынок — это рынок, а не продукт. Type error на target. | 0.25 |
| 8 | `PROGRAM → PROGRAM (COMPETES_WITH)` | Программы в текущей модели не конкурируют. Спорный кейс, сохраняем но понижаем. | 0.20 |
| 9 | `PERSON → ORG (INVOLVES_ACTOR)` | INVOLVES_ACTOR source = DOMAIN only. Персона не может быть source для domain-edge. | 0.30 |
| 10 | `MARKET → ORG (FOCUSES_ON)` | FOCUSES_ON source = DOMAIN only. Market не может быть source. | 0.30 |

Все паттерны из таблицы получают `policyStatus=quarantined` с соответствующим `policyReason` и `policyPenalty`.

### 3.5 Type/Path reconciliation layer
- Перед materialization всегда синхронизировать типы по canonical path.
- Логировать конфликт как отдельный telemetry class (`type_path_mismatch`) и применять deterministic fix.

**GOV-ORG whitelist rule:**
GOV bodies хранятся как `type=ORGANIZATION` с `canonicalPath=/gov/...` — это архитектурное решение, а не ошибка. Текущая валидация выдает penalty 0.10 (`canonical path "/gov/..." does not start with expected prefix "/org/" for type ORGANIZATION`). Это ложный шум.

Fix: добавить explicit whitelist rule в `expectedPathPrefix()`:
- `type=ORGANIZATION && path=/gov/*` → allow, no penalty.

**Deterministic reconciliation rules:**

| Path prefix | Expected type | Действие при mismatch |
|-------------|--------------|----------------------|
| `/org/` | `ORGANIZATION` | Fix type → ORGANIZATION |
| `/gov/` | `ORGANIZATION` | Allow (whitelist) |
| `/fac/` | `FACILITY` | Fix type → FACILITY |
| `/loc/` | `GEO_REGION` | Fix type → GEO_REGION |
| `/person/` | `PERSON` | Fix type → PERSON |
| `/product/` | `PRODUCT` | Fix type → PRODUCT (NB: не путать с `/product/...` path у mistyped ORG фрагментов — см. телеметрию: `"/product/brunp-recycling"` с type=ORGANIZATION) |
| `/market/` | `MARKET` | Fix type → MARKET |
| `/deposit/` | `DEPOSIT` | Fix type → DEPOSIT |
| `/community/` | `COMMUNITY` | Fix type → COMMUNITY |
| `/media/` | `MEDIA_CHANNEL` | Fix type → MEDIA_CHANNEL |
| `/domain/` | `DOMAIN` | Fix type → DOMAIN |

### 3.6 Unified observability
- Новые метрики:
  - `policy_reject_non_blocking_rate`,
  - `auto_fix_rate`,
  - `auto_fix_hit_rate` (% от ранее-reject кейсов, покрытых canonicalization rules),
  - `penalty_rate`,
  - `quarantine_rate`,
  - `unknown_direction_policy_count`,
  - `false_positive_reject_rate` (reject'ы для легитимных пар до расширения policy),
  - `type_path_mismatch_rate`,
  - `top_invalid_triples`.
- Dashboard + daily snapshot для regression контроля.
- Weekly comparison: contract coverage vs runtime telemetry (drift detection).

---

## 4. Автоисправления по стадиям

## 4.1 Стадия Canonicalization (pre-policy)
- Нормализовать relation type.
- Привести `targetType` к path-derived type.
- Применить deterministic rewrite matrix.
- Для слабых/сомнительных кейсов выставить `edgeModality=potential` + penalty candidate.

## 4.2 Стадия ValidationPolicy (decision)
- Использовать unified contract для `allow/reverse/rewrite/penalty/quarantine`.
- Не допускать unknown direction policy для разрешенных relation types.

## 4.3 Стадия ApplyDecision (post-policy)
- `auto_fix`: применить rewrite и отметить provenance reason.
- `allow_with_penalty`: уменьшить confidence, сохранить связь.
- `quarantine`: сохранить edge, но исключить из риск-критичных путей по умолчанию.

---

## 5. План реализации (трекер big bang)

Всё ниже выполняется в одном релизе, но с внутренним порядком работ и обязательной верификацией каждого блока.

Статусы задач:
- `TODO` — не начато
- `IN_PROGRESS` — в работе
- `BLOCKED` — заблокировано зависимостью
- `DONE` — завершено и верифицировано

### 5.1 WBS (work breakdown structure)

| ID | Задача | Статус | Зависит от | Артефакт результата | Верификация |
|---|---|---|---|---|---|
| BB-01 | Единый контракт direction policy и генерация маппингов для scout/policy | DONE | - | `edgesemantics/direction_policy.go` + alias in scout/policy | `go test ./backend/internal/horizon/policy ./backend/internal/horizon/scout` |
| BB-02 | Добавить `RELATED_TO`/`RELATIVE_OF` в runtime policy map и убрать `missing direction policy` | DONE | BB-01 | unified mapping + tests | `TestValidationPolicy_RelatedAndRelativeRulesPresent` |
| BB-03 | Расширить ValidSource/Target для легитимных пар из 3.2.1 | DONE | BB-01 | updated mapping in unified contract | unit tests + compile/runtime smoke |
| BB-04 | GOV-ORG whitelist и deterministic type/path reconciliation | DONE | BB-01 | whitelist в ValidationPolicy + reconcile by canonicalPath | `TestValidationPolicy_GovPathOrgWhitelist` |
| BB-05 | Расширить canonicalization matrix (7 retag rules) | DONE | BB-03, BB-04 | новые rewrite rules в `models/edge_canonicalization.go` | `go test ./backend/internal/horizon/models ./backend/internal/horizon/scout` |
| BB-06 | Внедрить `allow/auto_fix/allow_with_penalty/quarantine` + `policyStatus` qualifiers | DONE | BB-05 | `validation_apply.go` + `models/policy_qualifiers.go` | `go test ./backend/internal/horizon/policy ./backend/internal/horizon/scout` |
| BB-07 | Внедрить quarantine patterns (таблица 3.4.2) | DONE | BB-06 | quarantine classification + propagation skip quarantined edges | `go test ./backend/internal/horizon/policy ./backend/internal/horizon/propagation` |
| BB-08 | Синхронизировать prompt constraints (Scout/Research/Critic) с контрактом | DONE | BB-01 | Scout/Research/Critic prompts обновлены | prompt snapshot/manual review |
| BB-09 | Telemetry: метрики, поля, dashboard и daily snapshot | DONE | BB-06 | `edge_policy/kpi_rates`, `edge_policy/top_invalid_triples`, `/api/v1/admin/horizon/telemetry` rates | присутствие всех KPI-метрик в telemetry |
| BB-10 | Dry-run replay на исторических логах и сравнение baseline/after | DONE | BB-02..BB-09 | `cmd/tools/horizon_edge_policy_report` + baseline/after windows | достижение DoD из раздела 6 |
| BB-11 | Production rollout + 48h мониторинг | TODO | BB-10 | rollout report | no regression alert + top-20 invalid triples trend down |

### 5.2 Чеклист выполнения по документу

- [x] `BB-01` Unified contract создан и подключен.
- [x] `BB-02` Unknown direction policy для `RELATED_TO/RELATIVE_OF` устранен.
- [x] `BB-03` Полиси расширены для легитимных пар (таблица 3.2.1).
- [x] `BB-04` GOV-ORG whitelist и reconciliation layer в прод-коде.
- [x] `BB-05` Retag canonicalization rules внедрены.
- [x] `BB-06` Новый apply pipeline (`auto_fix/penalty/quarantine`) включен.
- [x] `BB-07` Quarantine patterns внедрены и участвуют в фильтрации propagation.
- [x] `BB-08` Промпты синхронизированы с контрактом.
- [x] `BB-09` Метрики и dashboard доступны.
- [x] `BB-10` Dry-run отчет приложен и подтверждает DoD.
- [ ] `BB-11` Rollout завершен, 48h пост-контроль пройден.

Примечание по `BB-11`: кодовая поддержка мониторинга реализована, но фактический 48h production-run выполняется как операционный шаг после деплоя.

### 5.3 Definition of Done traceability

| DoD criterion (раздел 6) | Проверяется в задаче | Evidence |
|---|---|---|
| `unknown_direction_policy_count = 0` | BB-02, BB-10 | replay + prod logs |
| reject non-blocking -70% | BB-03, BB-05, BB-06, BB-10 | before/after metric chart |
| `auto_fix_rate > 40%` | BB-05, BB-06, BB-10 | replay report |
| без деградации связности | BB-07, BB-10 | path reachability comparison |
| без роста critical false positives | BB-07, BB-10, BB-11 | risk rollup quality report |

### 5.4 PR/коммит трекинг (заполняется по факту)

| ID | PR/Commit | Дата | Ответственный | Комментарий |
|---|---|---|---|---|
| BB-01 | TBD | TBD | TBD |  |
| BB-02 | TBD | TBD | TBD |  |
| BB-03 | TBD | TBD | TBD |  |
| BB-04 | TBD | TBD | TBD |  |
| BB-05 | TBD | TBD | TBD |  |
| BB-06 | TBD | TBD | TBD |  |
| BB-07 | TBD | TBD | TBD |  |
| BB-08 | TBD | TBD | TBD |  |
| BB-09 | TBD | TBD | TBD |  |
| BB-10 | TBD | TBD | TBD |  |
| BB-11 | TBD | TBD | TBD |  |

---

## 6. Критерии приемки (Definition of Done)

- `unknown_direction_policy_count = 0` для allowlisted relation types.
- Снижение `ValidationPolicy would reject (non-blocking)` минимум на 70%.
- `auto_fix_rate` стабильно > 40% от ранее reject-класса.
- Нет деградации связности:
  - не падает число reachable entity paths в ключевых сценариях,
  - не растет materially missed entity rate.
- Нет роста critical false positives в risk rollup.

---

## 7. Риски и контрмеры

- Риск: over-normalization (слишком агрессивные rewrite).
  - Контрмера: quarantine для сомнительных кейсов, а не жесткий rewrite.
- Риск: drift после следующих изменений промптов.
  - Контрмера: генерация prompt constraints из единого контракта.
- Риск: падение explainability.
  - Контрмера: обязательный provenance reason для каждого auto-fix.

---

## 8. Post-Big-Bang контроль

- Ежедневный отчёт top-20 invalid triples.
- Weekly drift review: contract vs runtime telemetry.
- Регулярная подстройка rewrite/penalty правил без изменения best-effort принципа.

---

## Appendix A. GCP-телеметрия (baseline snapshot, 2026-02-11)

Данные из `nv-horizon` Cloud Logging. Использованы как основа для формирования retag rules (3.3), quarantine patterns (3.4) и policy expansion list (3.2.1).

### Top reject patterns (by frequency, ~10 min window)

| # | Pattern | Class | Count (approx) |
|---|---------|-------|-----------------|
| 1 | `ORG → SPHERE (INFLUENCES)` | false-positive / weak-link | high |
| 2 | `COMMUNITY → ORG (OWNS)` | retag → CONTROLS | high |
| 3 | `PERSON → PERSON (RELATED_TO)` | missing direction policy | high |
| 4 | `PERSON → PERSON (RELATIVE_OF)` | missing direction policy | medium |
| 5 | `ORG → ORG (PRODUCES)` | target type mismatch | high |
| 6 | `ORG → COMMUNITY (OWNS)` | retag or expand | medium |
| 7 | `ORG → ORG (AFFILIATED_WITH)` | retag → PARTNERS_WITH | medium |
| 8 | `ORG → MARKET (INFLUENCES)` | false-positive / expand policy | medium |
| 9 | `ORG → MEDIA_CHANNEL (OWNS)` | false-positive / expand policy | medium |
| 10 | `PERSON → PERSON (MANAGES)` | retag → RELATED_TO | medium |
| 11 | `ORG → MARKET (SUPPLIES_TO)` | target type mismatch | low |
| 12 | `PROGRAM → ORG (AUDITED_BY)` | false-positive / expand policy | low |
| 13 | `PERSON → COMMUNITY (AFFILIATED_WITH)` | retag → MEMBER_OF | low |
| 14 | `MARKET → MARKET (PARTICIPATES_IN)` | quarantine | low |
| 15 | `GOV path + type ORGANIZATION` | type/path whitelist | medium |

### Source log queries

```bash
# All non-blocking reject warnings
gcloud logging read 'textPayload=~"ValidationPolicy would reject"' --project=nv-horizon --limit=100 --freshness=7d

# Missing direction policy
gcloud logging read 'textPayload=~"missing direction policy"' --project=nv-horizon --limit=50 --freshness=7d

# Penalties and reversals
gcloud logging read 'textPayload=~"PENALTY" OR textPayload=~"REVERSING"' --project=nv-horizon --limit=50 --freshness=7d
```

### Dual-registry evidence

Историческая причина: `RELATED_TO`/`RELATIVE_OF` были в `scout` и taxonomy, но отсутствовали в local map ValidationPolicy.  
Текущий статус после big-bang реализации:
- единый контракт вынесен в `backend/internal/horizon/edgesemantics/direction_policy.go`;
- `scout` и `validation_policy` используют один и тот же source of truth;
- локальный `edgeDirectionPolicies` в `validation_policy.go` удален.
