# Supply Chain Canonicalization

Документ описывает подход к канонизации supply-chain связей в Horizon графе.

Важное замечание:

этот документ описывает семантику supply-chain канонизации, но не является главным документом про product identity и canonical product steps.
Для этого теперь есть отдельный верхнеуровневый документ:

- [Supply Chain Canonical Control Plane](/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/supply-chain-canonical-control-plane.md)

Если задача касается products, synonyms, transforms, assembly или materialized supply-chain paths, начинать нужно именно с него.

---

## 1. От организации (Organization-Centric View)

Когда в центре анализа находится **организация**, мы рассматриваем её связи в контексте supply chain.

### 1.1 Типы связей между организациями

| Связь | Направление | Семантика | Атрибуты |
|-------|-------------|-----------|----------|
| `SUPPLIES_TO` | org → org | Поставка продуктов | `products[]`, `brandNames[]`, `modelNames[]` |
| `DEPENDS_ON` | org → org | Зависимость (грубая) | — |
| `OWNS` | org → org | Владение (equity) | `share`, `controlType` |
| `CONTROLS` | org → org | Управление | `controlType` |

### 1.2 Ключевой принцип

**`SUPPLIES_TO` — основная supply-chain связь.**

Продукт — это **атрибут связи**, а не промежуточный узел в цепочке:

```
/org/zeiss-smt ──SUPPLIES_TO──> /org/asml
    └── products: ["/product/euv-optics"]
    └── brandNames: ["Starlith 3300"]
```

Это предпочтительнее, чем:
```
/org/zeiss-smt ──PRODUCES──> /product/euv-optics ──SUPPLIES_TO──> /org/asml
```

### 1.3 Сценарии восстановления связей

#### 1.3.1 Исходящие SUPPLIES_TO (org поставляет кому-то)

**Вопрос:** Что именно организация поставляет получателю?

**Контекст для LLM:**
- Что организация `PRODUCES` (известные продукты)
- Существующие `SUPPLIES_TO` связи с products
- Известные продукты в графе (`knownProducts`)

**Результат:**
```json
{
  "edge": "SUPPLIES_TO",
  "source": "/org/asml",
  "target": "/org/tsmc",
  "products": ["/product/euv-lithography-system"],
  "brandNames": ["TWINSCAN NXE:3600D"]
}
```

#### 1.3.2 Исходящие DEPENDS_ON (org зависит от кого-то)

**Вопрос:** Через какие продукты идёт зависимость?

**Семантика:** `A DEPENDS_ON B` ≈ `B SUPPLIES_TO A` с неизвестными products.

> Важно: это приближение. Не всякая зависимость — это поставка товара.
> `DEPENDS_ON` может означать сервис/поддержку/ПО/лицензии/процессы/уникальные компетенции/регуляторные ограничения.
> Тем не менее, **в supply-chain канонизации мы сначала пытаемся выразить зависимость как “поставка некоторого продукта/класса услуги”**,
> и только если это нельзя сделать честно — оставляем `DEPENDS_ON` как “грубую” связь без products (или помечаем неизвестность в on-demand ответе).

**Контекст для LLM:**
- Что поставщик (target) `PRODUCES`
- Существующие `SUPPLIES_TO` от поставщика
- Известные продукты в графе

**Результат:** Либо обогащение `DEPENDS_ON` атрибутами, либо создание обратной `SUPPLIES_TO`:
```json
{
  "edge": "SUPPLIES_TO",
  "source": "/org/zeiss-smt",
  "target": "/org/asml",
  "products": ["/product/euv-optics"]
}
```

#### 1.3.3 Входящие DEPENDS_ON (кто-то зависит от org)

**Вопрос:** Что организация поставляет зависимым?

**Семантика:** `B DEPENDS_ON A` ≈ `A SUPPLIES_TO B`.

**Контекст для LLM:**
- Что организация (selected) `PRODUCES`
- Кто от неё зависит и что им нужно

**Результат:**
```json
{
  "edge": "SUPPLIES_TO",
  "source": "/org/asml",
  "target": "/org/tsmc",
  "products": ["/product/euv-lithography-system"]
}
```

#### 1.3.4 PRODUCES (org производит product)

**Вопрос:** Действительно ли организация производит этот продукт?

**Действие:** Верификация существующей связи. Дополнение brandNames/modelNames если известны.

#### 1.3.5 Не-товарные зависимости (сервисы/ПО/лицензии)

Если LLM “знает”, что зависимость не описывается физическим товаром, мы всё равно стараемся сохранить канонизацию через `SUPPLIES_TO`,
но допускаем **класс продукта-услуги** как `/product/<service-class>`:

- Примеры: `/product/maintenance-service`, `/product/field-service`, `/product/software-license`, `/product/process-control-software`.

Это позволяет строить упорядоченные supply-chain деревья по организациям, не теряя важные зависимости.
Если даже так не получается (слишком абстрактно/неясно) — оставляем `DEPENDS_ON` как есть.

### 1.4 Расширенный контекст для LLM

Для корректной работы LLM нужен контекст **окрестности**, а не только пара узлов:

```json
{
  "selected": {
    "canonicalPath": "/org/asml",
    "type": "ORGANIZATION",
    "label": "ASML Holding N.V."
  },
  
  "incidentEdges": [
    { "edgeType": "DEPENDS_ON", "target": "/org/zeiss-smt" },
    { "edgeType": "DEPENDS_ON", "target": "/org/trumpf" },
    { "edgeType": "SUPPLIES_TO", "target": "/org/tsmc" }
  ],
  
  "neighborNodes": [
    { "canonicalPath": "/org/zeiss-smt", "label": "Carl Zeiss SMT" },
    { "canonicalPath": "/org/trumpf", "label": "TRUMPF" },
    { "canonicalPath": "/org/tsmc", "label": "TSMC" }
  ],
  
  "neighborContext": {
    "/org/zeiss-smt": {
      "produces": ["/product/euv-optics"],
      "suppliesTo": ["/org/asml"]
    },
    "/org/trumpf": {
      "produces": ["/product/high-power-co2-laser"],
      "suppliesTo": ["/org/asml"]
    },
    "/org/tsmc": {
      "produces": ["/product/semiconductor"],
      "dependsOn": ["/org/asml"]
    }
  },
  
  "knownProducts": [
    "/product/euv-optics",
    "/product/euv-lithography-system",
    "/product/high-power-co2-laser",
    "/product/semiconductor"
  ]
}
```

#### 1.4.1 Контекст как “каталог вариантов” (anti-hallucination)

Чтобы LLM **не придумывал** новые canonical paths и **переиспользовал** уже существующие, контекст должен содержать:

- **`knownProducts`**: список известных `/product/...` рядом с центром (желательно с `label`, если есть).
- **`neighborContext`**: для соседних организаций — агрегаты того, что уже известно:
  - `produces[]` (org→product)
  - `suppliesTo[]` и `suppliesToProducts` (org→org + products[])
  - `dependsOn[]` (org→org)
- **`knownFacilities`**: список доступных `/fac/...` (строго из графа) — чтобы LLM мог выбирать, но не изобретать.

Ключевой принцип: **LLM выбирает из каталога**, а генерирует новое только если выбора нет.

#### 1.4.2 Минимальные ограничения на размер контекста (чтобы не упираться в лимит)

Рекомендуемые правила формирования контекста для on-demand запросов:

- **Окрестность 1 hop** от `selected` (соседи + их агрегированное “что у них уже есть”).
- **Top-K соседей** по значимости (например, по `criticality`/весу ребра) + always-include явные `SUPPLIES_TO` связи.
- **Не тащить весь граф**: только поля, важные для канонизации (`canonicalPath`, `label`, `edgeType`, `products[]` и т.п.).
- **Дедуп** по `canonicalPath` (а не по label).
- **Список `knownProducts` ограничивать**:
  - сначала продукты из инцидентных связей и `neighborContext.produces[]`,
  - затем популярные/частотные продукты из локальной окрестности,
  - остальное — обрезать.

### 1.5 Правила для LLM

1. **Переиспользовать существующие canonical paths** из `knownProducts` и `neighborContext`
2. **Не изобретать новые /org/, /fac/ пути** — только из контекста
3. **Новые /product/ пути** — только если продукт действительно отсутствует и это реальный класс
4. **Предпочитать SUPPLIES_TO с products[]** вместо цепочки org→product→org

### 1.6 Результат канонизации

После канонизации supply-chain граф выглядит так:

```
              ┌─────────────┐
              │  Zeiss SMT  │
              │ PRODUCES:   │
              │ /product/   │
              │ euv-optics  │
              └──────┬──────┘
                     │
         SUPPLIES_TO [products: ["/product/euv-optics"]]
                     │
                     ▼
              ┌─────────────┐
              │    ASML     │
              │ PRODUCES:   │
              │ /product/   │
              │ euv-litho   │
              └──────┬──────┘
                     │
         SUPPLIES_TO [products: ["/product/euv-lithography-system"]]
                     │
                     ▼
              ┌─────────────┐
              │    TSMC     │
              │ PRODUCES:   │
              │ /product/   │
              │ semiconductor│
              └─────────────┘
```

**Цепочка по организациям:** `Zeiss SMT → ASML → TSMC`

**Продукты — атрибуты связей**, а не узлы в цепочке.

---

## 2. Связи между продуктами

*(Следующий раздел)*

Продукты как узлы нужны для:
- Таксономии и категоризации
- Связей `REQUIRES` (один продукт требует другой)
- Связей `TRANSFORMS_INTO` (трансформация продуктов)

```
/product/euv-lithography-system ──REQUIRES──> /product/euv-optics
/product/euv-lithography-system ──REQUIRES──> /product/high-power-co2-laser
/product/silicon-wafer ──TRANSFORMS_INTO──> /product/semiconductor
```

Эти связи ортогональны supply-chain связям между организациями и позволяют строить дерево зависимостей на уровне классов продуктов.

### 2.1 Семантика `REQUIRES`

`REQUIRES` означает, что один **класс продукта** нужен для полной реализации цепочки вокруг другого класса продукта.
Это шире, чем “нужно для производства” — сюда могут попадать:

- зависимости по компонентам/материалам,
- зависимости по оборудованию, без которого продукт/система не поставляется или не функционирует,
- зависимости по сервисам/ПО/лицензиям (если мы моделируем их как `/product/<service-class>`).

Важно: `REQUIRES` строит **дерево продуктовых зависимостей**, а `SUPPLIES_TO` строит **дерево организационных поставок**.
Обе структуры дополняют друг друга и соединяются через `products[]` на ребре `SUPPLIES_TO` и через `PRODUCES` (org→product).

---

## 3. Verdict семантика (On-Demand Resolver)

При анализе пары org↔org или product↔org используются следующие вердикты:

| verdict | scopeTag | relatedProduct | Смысл |
|---------|----------|----------------|-------|
| `produces` | `direct` | `null` | Производит запрошенный продукт |
| `produces_adjacent` | `adjacent` | заполнен | Производит связанный продукт (REQUIRES) |
| `does_not_produce` | `not_fit` | `null` | Не производит ничего релевантного |
| `unknown` | `not_fit` | `null` | Неизвестно |

`produces_adjacent` указывает на потенциальную связь `REQUIRES` между продуктами.
