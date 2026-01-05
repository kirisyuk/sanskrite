# Product Hierarchy & Value Chain Architecture

## Цель

Создать первичную иерархию связей в графе для продуктов/материалов, чтобы было понятно, что от чего зависит "в общем случае" (value chain). Это позволяет алгоритмически вычислять upstream/downstream propagation без необходимости создавать edges для каждого конкретного случая.

## Подходы

### 1. Таксономическая иерархия (уже реализовано)

**Canonical paths кодируют стадию передела:**
- `/product/raw/mineral/extraction/lithium-ore` → стадия `extraction`
- `/product/raw/chemical/refining/lithium-carbonate` → стадия `refining`
- `/product/component/electronic/manufacturing/battery-cell` → стадия `manufacturing`

**Преимущество:** Алгоритмическое определение upstream/downstream по `ProductStageOrder`:
- Если `extraction` < `refining` < `processing` < `manufacturing` < `assembly` в порядке
- То автоматически: `lithium-ore` (extraction) → upstream для `lithium-carbonate` (refining)

**Ограничение:** Не покрывает специфичные химические/технологические связи.

### 2. Новые типы edges для value chain

Добавить в `DependencyType`:

```go
// TRANSFORMS_INTO: Product X transforms into Product Y (generic value chain)
// Example: lithium-ore → lithium-carbonate → cathode-material → battery-cell
DependencyTypeTransformsInto DependencyType = "TRANSFORMS_INTO"

// REFINES_TO: Raw material X refines into refined material Y
// Example: lithium-ore → lithium-carbonate
DependencyTypeRefinesTo DependencyType = "REFINES_TO"

// ASSEMBLES_INTO: Component X assembles into finished product Y
// Example: battery-cell → electric-vehicle
DependencyTypeAssemblesInto DependencyType = "ASSEMBLES_INTO"
```

**Использование:**
- `TRANSFORMS_INTO` — общий тип для любой трансформации в value chain
- `REFINES_TO` — специфичный для raw → refined materials
- `ASSEMBLES_INTO` — специфичный для component → finished goods

**Edge direction:**
- Source: upstream product (earlier stage)
- Target: downstream product (later stage)

### 3. ProductHierarchyBuilder (автоматическое создание иерархии)

По аналогии с `OrgHierarchyBuilder`, создавать edges автоматически при создании продукта:

```go
type ProductHierarchyBuilder struct {
    fragmentsRepo *repo.HorizonFragmentsRepository
    edgesRepo     *repo.DependencyEdgesRepository
    wikidataClient coreontology.WikidataClientInterface
}

// EnsureProductHierarchy создаёт TRANSFORMS_INTO edges на основе:
// 1. Таксономии (stage order)
// 2. Wikidata (P527 "has part", P366 "has use", P10565 "is produced from")
// 3. LLM extraction (если Wikidata недостаточно)
```

**Логика:**
1. При создании продукта с canonical path `/product/raw/chemical/refining/lithium-carbonate`
2. Найти все продукты со стадией `extraction` того же типа (`chemical`)
3. Создать `TRANSFORMS_INTO` edge: `lithium-ore` → `lithium-carbonate`
4. Найти все продукты со стадией `processing` того же типа
5. Создать `TRANSFORMS_INTO` edge: `lithium-carbonate` → `cathode-material`

### 4. ProductDeduplicator с Wikidata

По аналогии с `OrgDeduplicator`:

```go
type ProductDeduplicator struct {
    fragmentsRepo  *repo.HorizonFragmentsRepository
    wikidataClient coreontology.WikidataClientInterface
}

// DeduplicateProduct использует:
// Tier 1 (100% confidence):
//   1. Wikidata ID match
//   2. CAS Registry Number match (для химических веществ)
//   3. Chemical formula match (для raw materials)
//
// Tier 2 (high confidence):
//   4. Exact name match + same category/type/stage
//   5. Normalized name match (синонимы)
```

**Ключевые свойства Wikidata для материалов:**
- `P274` — chemical formula (Li2CO3)
- `P231` — CAS Registry Number (554-13-2 для lithium carbonate)
- `P527` — has part (для композитных материалов)
- `P366` — has use (для применения)
- `P10565` — is produced from (для value chain)

### 5. Химический состав как идентификатор

Для raw materials добавить в `HorizonFragment.Identifiers`:

```go
Identifiers: map[string]string{
    "shortName": "Lithium Carbonate",
    "casNumber": "554-13-2",           // CAS Registry Number
    "chemicalFormula": "Li2CO3",       // Chemical formula
    "wikidataId": "Q409857",           // Wikidata Q-ID
    "iupacName": "dilithium carbonate", // IUPAC name (опционально)
}
```

**Дедупликация по химическому составу:**
- Если два продукта имеют одинаковый `casNumber` → один и тот же материал
- Если два продукта имеют одинаковый `chemicalFormula` + `category=raw` + `type=chemical` → вероятно один и тот же материал

### 6. LLM для извлечения связей

Когда Wikidata недостаточно (новые материалы, специфичные процессы):

**Промпт для LLM:**
```
Extract value chain relationships from text:
- Input: Product name + description
- Output: JSON with upstream/downstream products
- Format: {"upstream": ["product1", "product2"], "downstream": ["product3"]}
```

**Использование:**
- При создании продукта без Wikidata ID
- Если таксономия не даёт однозначной связи
- Для специфичных технологических процессов

## Реализация

### Этап 1: ProductDeduplicator

1. Создать `ProductDeduplicator` по аналогии с `OrgDeduplicator`
2. Добавить поддержку CAS/chemical formula matching
3. Интегрировать Wikidata search для продуктов

### Этап 2: Новые edge types

1. Добавить `TRANSFORMS_INTO`, `REFINES_TO`, `ASSEMBLES_INTO` в `DependencyType`
2. Обновить `AllowedRelationTypes` в taxonomy
3. Обновить validation rules для новых типов

### Этап 3: ProductHierarchyBuilder

1. Создать `ProductHierarchyBuilder`
2. Реализовать логику создания edges на основе:
   - Таксономии (stage order)
   - Wikidata properties (P527, P366, P10565)
   - LLM extraction (fallback)

### Этап 4: Интеграция в Scout

1. Вызывать `ProductHierarchyBuilder.EnsureProductHierarchy()` при создании продукта
2. Вызывать `ProductDeduplicator.DeduplicateProduct()` перед созданием

## Примеры

### Пример 1: Lithium value chain

```
1. Создаём продукт: "Lithium Ore"
   - canonicalPath: /product/raw/mineral/extraction/lithium-ore
   - wikidataId: Q123456
   - stage: extraction

2. ProductHierarchyBuilder автоматически создаёт:
   - Нет upstream (extraction — первая стадия)
   - Ищет downstream: все продукты со stage=refining, type=chemical, содержащие "lithium"

3. Создаём продукт: "Lithium Carbonate"
   - canonicalPath: /product/raw/chemical/refining/lithium-carbonate
   - casNumber: 554-13-2
   - chemicalFormula: Li2CO3
   - wikidataId: Q409857

4. ProductDeduplicator проверяет:
   - Есть ли продукт с casNumber=554-13-2? → дедупликация
   - Есть ли продукт с chemicalFormula=Li2CO3? → дедупликация

5. ProductHierarchyBuilder создаёт:
   - TRANSFORMS_INTO: lithium-ore → lithium-carbonate (upstream)
   - Ищет downstream: продукты со stage=processing, содержащие "lithium" или "cathode"
```

### Пример 2: Battery value chain

```
1. Создаём: "Cathode Material" (NCM)
   - canonicalPath: /product/component/electronic/processing/cathode-material
   - wikidataId: Q789012

2. ProductHierarchyBuilder создаёт:
   - TRANSFORMS_INTO: lithium-carbonate → cathode-material (upstream)
   - Ищет downstream: battery-cell

3. Создаём: "Battery Cell"
   - canonicalPath: /product/component/electronic/manufacturing/battery-cell
   - wikidataId: Q345678

4. ProductHierarchyBuilder создаёт:
   - ASSEMBLES_INTO: cathode-material → battery-cell (upstream)
   - ASSEMBLES_INTO: anode-material → battery-cell (если есть)
   - Ищет downstream: electric-vehicle
```

## Преимущества подхода

1. **Автоматизация:** Иерархия создаётся автоматически при создании продуктов
2. **Строгость:** Таксономия даёт детерминированные связи
3. **Расширяемость:** Wikidata покрывает известные материалы
4. **Гибкость:** LLM заполняет пробелы для новых/специфичных случаев
5. **Дедупликация:** CAS/формула предотвращают дубликаты

## Вопросы для обсуждения

1. Нужны ли все три типа edges (`TRANSFORMS_INTO`, `REFINES_TO`, `ASSEMBLES_INTO`) или достаточно одного `TRANSFORMS_INTO`?
2. Как часто обновлять связи из Wikidata? (при создании продукта vs периодический sync)
3. Нужна ли валидация LLM-extracted связей перед созданием edges?
4. Как обрабатывать циклы в value chain? (например, recycling)





