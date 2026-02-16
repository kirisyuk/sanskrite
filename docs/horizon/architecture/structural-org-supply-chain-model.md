# Structural Organization & Supply Chain Model

## Обзор

Документ фиксирует архитектурные решения по моделированию **структурированных организаций** и их **входов-выходов** (upstream/downstream) в контексте supply chain. Обсуждение состоялось в феврале 2025.

Ключевой тезис: **крупные компании не имеют плоских или линейных supply chain**. У них есть структурированный upstream и структурированный downstream — и структура эта «голографическая»: внутри организации существуют подмножества (divisions, subsidiaries, units), у каждого свои входы и выходы.

---

## 1. Проблема плоского графа

### 1.1 Текущее ограничение

В плоской модели организация — точка. Связь «Samsung поставляет в Apple» теряет суть:
- Samsung Display поставляет экраны
- Samsung Memory поставляет чипы
- Разные бизнес-процессы, риски, upstream-цепочки

### 1.2 Проблема смешения контекстов

Один aggregate link «SAP → Kazakhtelecom» скрывает:
- SAP → IT-департамент KT (ERP, облако)
- Huawei → сетевой департамент KT (5G, оборудование)

При сбое у TSMC пострадает Network департамент, но не IT (SAP). Детализация критична.

---

## 2. Голографические множества

### 2.1 Определение

**Голографическое множество** — подмножество организации, которое:
- Имеет автономию (кроме клинических случаев)
- Имеет свои upstream и downstream
- Может быть division, subsidiary, department, JV

На верхнем уровне «KazMunayGas поставляет в Halliburton» — правда. Но реально поставляет конкретная часть KazMunayGas (JV, подразделение, дочка). Детализация — уточнение, не замена.

### 2.2 Входы и выходы — отдельные сущности

- **Input** (upstream) и **Output** (downstream) — отдельные сущности
- На верхнем уровне они агрегированы
- Внутри их объединяет только **unit** (org unit)

---

## 3. Слои структуры

### 3.1 Порядок слоёв

| Слой | Описание | Когда |
|------|----------|-------|
| **Функциональный** | Базовый. Core Ops, IT, Logistics, Corporate, R&D | Всегда, основа |
| **Продуктовый** | Enterprise/Consumer, отраслевые product buckets | Будущее, требует LLM-идентификации |
| **Юридический** | Subsidiaries, JV | Нанизывается на функциональный |

**Продуктовая таксономия как основа кластеризации** — отвергнута. Вызывает больший дрейф, чем чёткая функциональная структура на раннем этапе.

### 3.2 Org Unit как сущность

**Org unit** — обязательная сущность. Не виртуальная кластеризация по продукту, а явный фрагмент/узел в графе.

---

## 4. Универсальный набор фасетов (микс product + functional)

### 4.1 Product orientation (кто клиент / что продаём)

| Фасет | Описание |
|-------|----------|
| **Enterprise** | B2B, корпоративные, промышленные клиенты |
| **Consumer** | B2C, розница, физлица |

Почти у всех есть хотя бы один. Оба — часто.

### 4.2 Functional (что делаем внутри)

| Фасет | Описание |
|-------|----------|
| **Core Operations** | Основное производство / операции |
| **IT & Digital** | ERP, облако, софт |
| **Logistics & Supply Chain** | Транспорт, склады, закупки |
| **Corporate** | Юр, HR, финансы, консалтинг |
| **R&D** | Где применимо |

### 4.3 По индустриям

| Индустрия | Enterprise | Consumer | Core Ops | Доп. product/functional |
|-----------|------------|----------|----------|--------------------------|
| **Metal/Mining** | Продажи руды/металлов | — | Добыча, обогащение | Extraction, Processing |
| **Oil & Gas** | Нефть/газ, сервисы | АЗС (если есть) | Upstream, Midstream, Downstream | — |
| **Discrete mfg** | Fleet, оборудование | Розница, дилеры | Сборка | Components |
| **Retail** | Опт, корпоративные | Магазины, e-commerce | Merchandising, Store Ops | — |
| **Telecom/Utilities** | B2B connectivity, DC | Резиденты, мобилка | Network Ops | Infrastructure |
| **IT vendors** | Enterprise software, cloud | Consumer software | Product/Engineering | Cloud Ops, Support |
| **Holdings** | — | — | Corporate (центр) | Рекурсия по дочкам |

### 4.4 Базовый шаблон (универсальный)

```
Enterprise | Consumer
Core Operations | IT | Logistics | Corporate | R&D
```

**Расширения по домену:** Extraction, Processing (mining); Upstream/Midstream/Downstream (oil); Merchandising (retail); Product, Cloud Ops (IT vendor).

### 4.5 Опциональность

Фасеты (units) **опциональны**. У малого бизнеса может не быть R&D или отдельной Logistics.

---

## 5. Дополнительные аспекты

### 5.1 География

У org unit может быть **geo qualifier**. Региональные подразделения (KT Алматы vs Астана) — разные поставщики.

### 5.2 Государственный сектор (gov)

Госы и gov-структуры — **дополнить таксономию**. Госзакупки, стратегические vs коммерческие направления.

### 5.3 JV и партнёрства

**JV — самостоятельная структура.** Любая org распадается на самостоятельные структуры (у них автономия). Units есть и у родителей, и у детей.

### 5.4 Версионность

Версионность структуры — поддерживается. В коде уже есть **преобразования юрлиц** (legal entity transformations).

### 5.5 Каналы

**Channel** (direct / distributor / retail) — отдельное измерение, учитывается.

### 5.6 Sales

**Sales не порождает продукт** — отдельным фасетом не выделяем.

### 5.7 After-sales / Service

Пока не берём — продукт неявный.

---

## 6. Связь SupplyLink с Org Unit

### 6.1 Проблема

Должен быть qualifier, который связывает SupplyLink с unit. Как?

### 6.2 Решение: unit refs

**Вариант A (refs на уровне unit):**
- `SupplierRef` / `CustomerRef` указывают на org unit: `/org/kt/unit/corp-center`
- При unit-level данных — ссылки на unit; агрегат на org — rollup по `unit.parent = org`

**Вариант B (qualifiers на org-level link):**
- `SupplierRef` / `CustomerRef` — всегда org
- `supplier_unit_ref`, `customer_unit_ref` — опциональные ссылки на unit

Qualifier, объединяющий: **`customer_unit_ref`** и **`supplier_unit_ref`**. Связь привязана к unit; агрегаты строятся по родительскому org.

---

## 7. Нормализация unit types

### 7.1 Контролируемый словарь

Чтобы «IT» у разных компаний означало одно и то же — **код + доменный шаблон**.

### 7.2 Универсальные коды

```
enterprise, consumer, core_ops, it, logistics, corporate, rnd
```

### 7.3 Расширения по домену

- Telecom: `network_ops`
- Mining: `extraction`, `processing`
- Retail: `merchandising`
- IT vendor: `product`, `cloud_ops`, `support`

### 7.4 Реализация

- Справочник: фрагмент или enum с кодом + лейблами (en, ru)
- LLM → код: маппинг «IT department» → `it`

---

## 8. Агрегаты

### 8.1 Хранение отдельно

Агрегаты **храним отдельно**, не вычисляем на лету. Иначе «разоримся на запросах».

### 8.2 Уровни агрегации

- **SAP → KT** (прямой, org-level)
- **SAP Enterprise → KT Corp Center** (прямой, unit-level)
- Внутри — голографическая детализация

Агрегаты могут иметь уровни (org, unit).

---

## 9. Supply Chain PathBuilder — реализованные улучшения

### 9.1 Дедупликация SupplyLink

**Было:** `FindSupplyLinksByCustomer(customer, limit 100)` — ненадёжно при >100 ссылок.

**Стало:** `GetByCanonicalPath(slFrag.CanonicalPath)` — canonical path детерминирован (tenant:supplier:product:customer), надёжный lookup.

### 9.2 RequireProductTransformation

По умолчанию **true**. Стыковка только при наличии `TRANSFORMS_INTO` между продуктами. Исключает нелогичные переходы (lithium → steel).

### 9.3 Множественные пути

- **BuildUpstreamPaths** — возвращает топ-K путей (MaxPaths, по умолчанию 10)
- Один путь — «обрезок»; supply chain в нормальном виде — граф с несколькими путями

---

## 10. Scout / Research — будущие расширения

### 10.1 Промпт

При скаутинге явно просить LLM: **«Classify suppliers by the functional area they serve»** (Network Ops, IT, Corporate, Logistics).

### 10.2 Доменные шаблоны

Предустановленный набор множеств (Functional Areas) по домену. LLM или алгоритм классифицирует: в какую функциональную зону получателя входит поставка?

---

## 11. Связь с существующими документами

| Документ | Связь |
|----------|-------|
| [supply-chain-influence-model.md](supply-chain-influence-model.md) | SupplyLink, PathBuilder, стыковка |
| [data-model.md](data-model.md) | HorizonFragment, FragmentType |
| [ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md](../implementation/ORG_IDENTITY_ANCHOR_AND_CORPORATE_TRANSITIONS.md) | Преобразования юрлиц |
| [industry-taxonomy.md](industry-taxonomy.md) | Доменные шаблоны |

---

## 12. Открытые вопросы

1. **FragmentType для Org Unit** — новый тип или подтип Organization?
2. **Canonical path для unit** — `/org/kt/unit/corp-center` или иная схема?
3. **SUBSIDIARY_OF** — связь org unit с родительской org; детали edge type.
4. **Маппинг LLM output → unit code** — правила, fallback при неизвестном.

---

*Документ создан по итогам обсуждения. Версия: 2025-02.*
