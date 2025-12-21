# Базовая таксономия ядра (Core Taxonomy v1.0)

## Обзор

Базовая таксономия ядра определяет стандарты классификации и оценки сущностей в системе динамического анализа рисков. Таксономия строго на английском языке, использует иерархические пути для расширяемости и нормализованные числовые метрики.

**Важно**: На этапе MVP мы строго придерживаемся данной таксономии. Любые отклонения должны быть согласованы и документированы.

## 1. Numeric Standards (Принципы оценки)

Все метрики в системе делятся на два класса:

### A. Magnitude Metrics (0.0 ... 1.0)

Используются для физических величин, вероятностей, интенсивности.

- **0.0**: Абсолютный ноль / Отсутствие
- **1.0**: Максимум / Полная загрузка / 100% вероятность

**Примеры**:
- `utilization_rate` — уровень использования мощности
- `probability` — вероятность события
- `water_intensity` — интенсивность потребления воды
- `operational_health` — состояние операционной деятельности
- `capacity_utilization` — использование мощности
- `supply_chain_criticality` — критичность цепочки поставок

### B. Directional Metrics (-1.0 ... 1.0)

Используются для отношений, настроений, политических векторов.

- **-1.0**: Полный негатив / Враждебность / Противофаза
- **0.0**: Нейтралитет / Отсутствие смещения
- **1.0**: Полный позитив / Союзничество / Синфаза

**Примеры**:
- `sentiment_score` — оценка настроений
- `political_alignment` — политическое выравнивание
- `correlation_coefficient` — коэффициент корреляции
- `geopolitical_alignment` — геополитическое выравнивание
- `market_sentiment` — настроение рынка
- `public_perception` — общественное восприятие

## 2. Entity Types Taxonomy (entity_path)

Определяет физическую природу узла. Это иерархия, позволяющая уточнять тип по мере поступления данных.

**Формат**: `/Category/Type/SubType`

### 2.1. Organization (Юридические лица)

```
/Organization/Corporation/Public          # Публичные компании
/Organization/Corporation/Private         # Частные компании
/Organization/Corporation/StateOwned       # Государственные предприятия (SOE)
/Organization/Government/Agency           # Регуляторы, министерства
/Organization/Government/Military         # Военные организации
/Organization/NonProfit/NGO               # Некоммерческие организации
/Organization/International/IGO           # Международные организации (UN, WTO, NATO)
```

### 2.2. Infrastructure (Физические активы)

#### Logistics (Логистика)
```
/Infrastructure/Logistics/Seaport          # Морской порт
/Infrastructure/Logistics/Airport         # Аэропорт
/Infrastructure/Logistics/RailHub         # Железнодорожный узел
/Infrastructure/Logistics/Warehouse       # Склад
```

#### Energy (Энергетика)
```
/Infrastructure/Energy/PowerPlant/Nuclear      # Атомная электростанция
/Infrastructure/Energy/PowerPlant/Thermal      # Тепловая электростанция
/Infrastructure/Energy/PowerPlant/Renewable    # Возобновляемая энергия
/Infrastructure/Energy/Grid/Substation        # Подстанция
/Infrastructure/Energy/Pipeline/Oil            # Нефтепровод
/Infrastructure/Energy/Pipeline/Gas            # Газопровод
```

#### Manufacturing (Производство)
```
/Infrastructure/Manufacturing/Factory         # Завод
/Infrastructure/Manufacturing/Mine             # Шахта/рудник
```

#### Digital (Цифровая инфраструктура)
```
/Infrastructure/Digital/DataCenter            # Дата-центр
/Infrastructure/Digital/InternetExchange       # Точка обмена интернет-трафиком
```

### 2.3. Resource (Сырье и товары)

```
/Resource/RawMaterial/Mineral          # Минералы (Литий, Кобальт)
/Resource/RawMaterial/Agricultural     # Сельскохозяйственные (Пшеница, Соя)
/Resource/Energy/Fossil                # Ископаемое топливо (Нефть, СПГ)
/Resource/Energy/Electricity           # Электроэнергия
/Resource/Component/Electronic         # Электронные компоненты (Полупроводники)
/Resource/Component/Mechanical         # Механические компоненты
```

### 2.4. Person (Физические лица)

```
/Person/PEP            # Политически значимое лицо (Politically Exposed Person)
/Person/Executive      # Руководитель (CEO, CFO)
/Person/Shareholder    # Акционер (UBO - Ultimate Beneficial Owner)
```

### 2.5. Geo (Географические регионы)

Определяет географические локации с использованием ISO стандартов.

```
/Geo/Region/Country          # Страна (ISO 3166-1 alpha-2: US, KZ, RU)
/Geo/Region/Subdivision     # Административное деление (ISO 3166-2: US-CA, KZ-75, RU-MOW)
/Geo/Region/City             # Город с ISO6709 координатами
/Geo/Region/Metropolitan     # Метрополия / Агломерация
```

**Требования к Location**:
- **ISO 3166-1 alpha-2**: Обязательно для Country (например, "US", "KZ", "RU")
- **ISO 3166-2**: Обязательно для Subdivision (например, "US-CA", "KZ-75", "RU-MOW")
- **ISO 6709**: Обязательно для координат (формат: `±DD.DDDD±DDD.DDDD/` или `±DDMM.MMMM±DDDMM.MMMM/`)

**Примеры**:
- `/Geo/Region/Country` с ISO3166-1: "US", "KZ", "RU"
- `/Geo/Region/Subdivision` с ISO3166-2: "US-CA" (Калифорния), "KZ-75" (Алматы), "RU-MOW" (Москва)
- `/Geo/Region/City` с ISO6709: "+3740+12225/" (Сан-Франциско)

## 3. Industry & Sector Taxonomy (sector_path)

Базируется на логике GICS/NAICS, но упрощена для риск-анализа. Расширяема добавлением уровней.

**Формат**: `/Sector/IndustryGroup/Industry/SubIndustry`

### 3.1. Energy & Materials

#### Energy (Энергетика)
```
/Energy/Fossil_Fuels/Oil_Gas_Upstream      # Разведка и добыча
/Energy/Fossil_Fuels/Oil_Gas_Midstream     # Трубопроводы и хранение
/Energy/Fossil_Fuels/Oil_Gas_Downstream    # Переработка
/Energy/Renewable/Solar                     # Солнечная энергия
/Energy/Renewable/Wind                     # Ветровая энергия
```

#### Materials (Материалы)
```
/Materials/Mining/Precious_Metals         # Драгоценные металлы
/Materials/Mining/Industrial_Metals       # Промышленные металлы (Медь, Алюминий)
/Materials/Mining/Rare_Earths             # Редкоземельные элементы
/Materials/Chemicals/Specialty            # Специальные химикаты
/Materials/Chemicals/Fertilizers          # Удобрения
```

### 3.2. Industrials & Manufacturing

#### Industrials (Промышленность)
```
/Industrials/Aerospace_Defense/Defense_Prime      # Оборонные подрядчики
/Industrials/Aerospace_Defense/Components         # Компоненты
/Industrials/Machinery/Industrial_Automation      # Промышленная автоматизация
/Industrials/Transportation/Marine                # Морской транспорт
/Industrials/Transportation/Air_Freight           # Авиаперевозки
/Industrials/Transportation/Rail                  # Железнодорожный транспорт
```

#### Automotive (Автомобильная промышленность)
```
/Automotive/Vehicles/EV                    # Электромобили
/Automotive/Components/Batteries            # Аккумуляторы
```

### 3.3. Technology

```
/Technology/Semiconductors/Design          # Проектирование (Fabless)
/Technology/Semiconductors/Manufacturing   # Производство (Foundry)
/Technology/Semiconductors/Equipment       # Оборудование (ASML и др.)
/Technology/Hardware/Consumer_Electronics  # Потребительская электроника
/Technology/Software/Enterprise            # Корпоративное ПО
/Technology/Software/Cybersecurity        # Кибербезопасность
```

### 3.4. Healthcare

```
/Healthcare/Pharma/Biotech                 # Биотехнологии
/Healthcare/Pharma/Generics               # Генерики
/Healthcare/Equipment/Medical_Devices      # Медицинское оборудование
```

### 3.5. Financials

```
/Financials/Banking/Systemic              # Системно значимые банки (G-SIBs)
/Financials/Banking/Regional             # Региональные банки
/Financials/Insurance/Reinsurance         # Перестрахование
/Financials/Capital_Markets/Asset_Management  # Управление активами
```

### 3.6. Public Sector (Государственный сектор)

```
/Public/Executive/Ministry                # Исполнительная власть / Министерство
/Public/Legislative/Parliament            # Законодательная власть / Парламент
/Public/Judicial/Court                    # Судебная власть / Суд
/Public/Central_Bank                      # Центральный банк
```

## 4. Standardized Metrics (Стандартизированные метрики)

Метрики в системе используют гибкую модель с описаниями и типами, но придерживаются стандартизированных диапазонов согласно Numeric Standards.

### Структура метрики

Каждая метрика имеет следующую структуру:

```go
type MetricDefinition struct {
    Name        string  // Имя метрики (например, "operational_health")
    Type        string  // "magnitude" | "directional"
    Range       struct {
        Min float64    // 0.0 для magnitude, -1.0 для directional
        Max float64    // 1.0 для обоих типов
    }
    Unit        string  // Единица измерения (опционально): "%", "USD", "hours", ""
    Description string  // Описание метрики
    Category    string  // Категория: "operational", "risk", "physical", "relational"
}

type MetricValue struct {
    Name        string      // Имя метрики
    Value       float64     // Значение в нормализованном диапазоне
    Unit        *string     // Единица измерения (если применимо)
    ErrorMargin *ErrorMargin // Интервал ошибки (опционально)
    Confidence  float64     // Уверенность в значении (0.0-1.0)
    ObservedAt  time.Time   // Время наблюдения
    Provenance  Provenance  // Источник данных
}
```

### Стандартизированные метрики

Ниже приведены стандартизированные метрики, которые LLM должна заполнять. Система также поддерживает дополнительные метрики с описаниями и типами.

### 4.1. Operational Metrics (0.0 ... 1.0)

#### operational_health
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `operational`
- **Описание**: Состояние операционной деятельности объекта
- **Значения**:
  - **1.0**: Полностью функционирует, оптимальная эффективность
  - **0.5**: Сниженная мощность, техническое обслуживание
  - **0.0**: Не функционирует, остановлен, уничтожен

**Примеры использования**:
- Завод: 1.0 = работает на полную мощность, 0.0 = закрыт
- Порт: 1.0 = полностью функционирует, 0.5 = частично закрыт

#### capacity_utilization
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `"%"`
- **Category**: `operational`
- **Описание**: Процент использования максимальной мощности
- **Значения**:
  - **1.0**: 100% использование мощности
  - **0.0**: 0% использование мощности

**Примеры использования**:
- Фабрика полупроводников: текущая мощность / проектная мощность
- Электростанция: текущая выработка / максимальная выработка

#### supply_chain_criticality
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `operational`
- **Описание**: Критичность узла для цепочки поставок
- **Значения**:
  - **1.0**: Единственная точка отказа (нет альтернатив)
  - **0.5**: Важный узел, но есть альтернативы
  - **0.0**: Товар массового потребления, легко заменим

**Примеры использования**:
- Единственный производитель критического компонента: 1.0
- Один из многих поставщиков стандартного товара: 0.2

### 4.2. Physical Dependencies (0.0 ... 1.0)

#### water_intensity
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `physical`
- **Описание**: Интенсивность потребления воды
- **Значения**:
  - **1.0**: Требует массивного потребления воды (например, Fab, АЭС)
  - **0.5**: Умеренное потребление воды
  - **0.0**: Сухой процесс, не требует воды

**Примеры использования**:
- Фабрика полупроводников: 1.0 (требует огромного количества чистой воды)
- Офисное здание: 0.1 (минимальное потребление)

#### energy_intensity
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `physical`
- **Описание**: Интенсивность потребления энергии
- **Значения**:
  - **1.0**: Массивный потребитель энергии (например, выплавка алюминия)
  - **0.5**: Умеренное потребление энергии
  - **0.0**: Минимальное потребление энергии

**Примеры использования**:
- Алюминиевый завод: 1.0 (энергоемкое производство)
- Дата-центр: 0.8 (высокое потребление энергии)
- Офис: 0.2 (низкое потребление)

#### labor_intensity
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `physical`
- **Описание**: Интенсивность использования рабочей силы
- **Значения**:
  - **1.0**: Требует большого количества рабочей силы (например, сборочное производство)
  - **0.5**: Умеренное использование рабочей силы
  - **0.0**: Полностью автоматизировано

**Примеры использования**:
- Сборочный завод: 1.0 (трудоемкое производство)
- Автоматизированная фабрика: 0.3 (минимальная рабочая сила)

### 4.3. Risk & Stability Metrics (0.0 ... 1.0)

#### stability_index
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `risk`
- **Описание**: Индекс стабильности окружающей среды
- **Значения**:
  - **1.0**: Чрезвычайно стабильная среда
  - **0.5**: Умеренная стабильность
  - **0.0**: Хаос, активная зона конфликта

**Примеры использования**:
- Стабильная демократия: 0.9
- Зона конфликта: 0.1
- Переходный период: 0.5

#### corruption_vulnerability
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `risk`
- **Описание**: Уязвимость к коррупции
- **Значения**:
  - **1.0**: Высокая уязвимость / Клептократия
  - **0.5**: Умеренная уязвимость
  - **0.0**: Прозрачное верховенство закона

**Примеры использования**:
- Страны с высоким уровнем коррупции: 0.8-1.0
- Страны с прозрачным правлением: 0.0-0.2

#### sanction_exposure
- **Type**: `magnitude`
- **Range**: `[0.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `risk`
- **Описание**: Подверженность санкциям
- **Значения**:
  - **1.0**: Прямые санкции (SDN List)
  - **0.5**: Секторальные санкции
  - **0.0**: Чисто (нет санкций)

**Примеры использования**:
- Компания в SDN List: 1.0
- Компания в секторе под санкциями: 0.5
- Компания без санкций: 0.0

### 4.4. Relational/Sentiment Metrics (-1.0 ... 1.0)

#### geopolitical_alignment
- **Type**: `directional`
- **Range**: `[-1.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `relational`
- **Описание**: Геополитическое выравнивание
- **Значения**:
  - **1.0**: Стратегический союзник (например, США-Великобритания)
  - **0.0**: Нейтральный / Неприсоединившийся
  - **-1.0**: Стратегический противник / Враждебный

**Примеры использования**:
- Союзники по НАТО: 0.8-1.0
- Нейтральные страны: -0.2 до 0.2
- Враждебные отношения: -1.0 до -0.7

#### market_sentiment
- **Type**: `directional`
- **Range**: `[-1.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `relational`
- **Описание**: Настроение рынка
- **Значения**:
  - **1.0**: Бычий рынок / Эйфория
  - **0.0**: Нейтральное настроение
  - **-1.0**: Медвежий рынок / Паника

**Примеры использования**:
- Рыночная эйфория: 0.8-1.0
- Нейтральный рынок: -0.2 до 0.2
- Рыночная паника: -1.0 до -0.7

#### public_perception
- **Type**: `directional`
- **Range**: `[-1.0, 1.0]`
- **Unit**: `""` (безразмерная)
- **Category**: `relational`
- **Описание**: Общественное восприятие
- **Значения**:
  - **1.0**: Высокая репутация
  - **0.0**: Нейтральное восприятие
  - **-1.0**: Токсичная репутация / Скандал

**Примеры использования**:
- Компании с отличной репутацией: 0.7-1.0
- Компании после скандала: -1.0 до -0.5

### 4.5. Дополнительные метрики

Система поддерживает добавление произвольных метрик с описаниями и типами. При добавлении новой метрики необходимо указать:

- **Name**: Уникальное имя метрики
- **Type**: `magnitude` или `directional`
- **Range**: Минимальное и максимальное значение
- **Unit**: Единица измерения (если применимо)
- **Description**: Подробное описание метрики
- **Category**: Категория метрики

**Пример дополнительной метрики**:
```json
{
  "name": "supply_chain_resilience",
  "type": "magnitude",
  "range": { "min": 0.0, "max": 1.0 },
  "unit": "",
  "description": "Устойчивость цепочки поставок к сбоям",
  "category": "operational"
}
```

## 5. Influence Spheres Taxonomy (spherePaths)

Иногда событие/утверждение влияет не “на объект” напрямую, а на **сферу**: геополитику, социум, киберпространство, логистику и т.п.
Для этого в `ontology_facts` используется поле `spherePaths: []string` — **множественные** канонизированные пути, которые:
- используются для фильтрации/фасетинга в Vertex AI Search,
- дают основу для будущего распространения волн “по сфере” и “по месту” (политика распространения может эволюционировать отдельно).

**Канонический формат**:

```
/sphere/{domain}/{subdomain...}
```

**Примеры (минимальный словарь v1):**
- `/sphere/ai`
- `/sphere/ai/models`
- `/sphere/ai/policy`
- `/sphere/ai/research`
- `/sphere/ai/applications`
- `/sphere/geopolitics`
- `/sphere/policy`
- `/sphere/policy/regulation`
- `/sphere/policy/trade`
- `/sphere/policy/sanctions`
- `/sphere/social`
- `/sphere/social/information`
- `/sphere/cyber`
- `/sphere/digital`
- `/sphere/logistics`
- `/sphere/logistics/maritime`
- `/sphere/energy`
- `/sphere/energy/markets`
- `/sphere/finance`
- `/sphere/finance/crypto`
- `/sphere/finance/markets`

**Правила:**
- `spherePaths` можно указывать 1..N (например, атака на порт: `/sphere/geopolitics` + `/sphere/logistics/maritime`).
- Если сфера неочевидна — лучше опустить поле, чем выдумывать.

## 6. Intangible signals → metricUpdates (как мы формируем “волны”)

Propagation в Horizon в текущей реализации реагирует на **дельты метрик** (`HorizonFragment.state`).
Поэтому для intangible факторов мы различаем:
- **факт как событие/утверждение** (`ontology_fact.kind=event|claim`, `type=eventType|predicate`, `spherePaths`, `locationPath`, `actorPaths`)
- **сигнал как метрика** (`ontology_fact.kind=metric` или `metricUpdates`), которая даёт детерминируемую дельту и запускает волну.

### Базовое правило
`metricUpdates` добавляются **только если их можно обосновать текстом** (явный эффект/ущерб/ограничения/длительность/интенсивность).
Если обоснования нет — метрики не добавляем (факт остаётся для поиска/контекста).

### Рекомендуемая “карта” (v1)
- **attack / incident** (геополитика/логистика/социум):
  - `incident_severity` (ratio, 0..1) — оценка тяжести из контекста
  - `impact_confidence` (ratio, 0..1) — уверенность в оценке влияния
  - `shipment_delay_days` / `freight_cost` — только если есть явные числа/задержки
- **cyberattack / outage** (кибер/цифровая сфера):
  - `incident_severity` (ratio)
  - `incident_duration_hours` (hours) — если длительность указана
  - `impact_confidence` (ratio)
- **sanction / blockade / policy_change** (геополитика/финансы):
  - `sanctions_risk` (ratio) — если действие реально вводит/усиливает ограничения
  - `impact_confidence` (ratio)
- **union / strike / protest** (социум/операции):
  - `incident_severity` (ratio)
  - `downtime_hours` (hours) — если указано/однозначно следует
  - `impact_confidence` (ratio)

## Использование в системе

### В HorizonFragment

```json
{
  "canonicalPath": "/fac/tsmc/fab-21",
  "type": "FACILITY",
  "entityPath": "/Infrastructure/Manufacturing/Factory",
  "sectorPath": "/Technology/Semiconductors/Manufacturing",
  "homeLocation": {
    "name": "Hsinchu, Taiwan",
    "iso6709": "+2457+12058/",
    "iso3166_1": "TW",
    "iso3166_2": "TW-HSQ"
  },
  "state": {
    "operational_health": {
      "value": 0.85,
      "confidence": 0.95,
      "observedAt": "2025-12-17T14:30:00Z",
      "unit": null,
      "errorMargin": { "low": 0.80, "high": 0.90 }
    },
    "capacity_utilization": {
      "value": 0.92,
      "confidence": 0.98,
      "observedAt": "2025-12-17T14:30:00Z",
      "unit": "%",
      "errorMargin": null
    },
    "water_intensity": {
      "value": 0.95,
      "confidence": 0.90,
      "observedAt": "2025-12-17T14:30:00Z"
    },
    "geopolitical_alignment": {
      "value": 0.3,
      "confidence": 0.85,
      "observedAt": "2025-12-17T14:30:00Z"
    }
  }
}
```

### В Scout Agent промптах

Scout Agent должен использовать данную таксономию при извлечении данных:

```
Extract the following metrics for the entity:
- operational_health (0.0-1.0): Current operational status
- capacity_utilization (0.0-1.0): Percentage of max capacity
- water_intensity (0.0-1.0): Water consumption intensity
- geopolitical_alignment (-1.0-1.0): Geopolitical alignment with reference country

Classify the entity using:
- entityPath: One of /Organization/*, /Infrastructure/*, /Resource/*, /Person/*
- sectorPath: One of /Energy/*, /Technology/*, /Materials/*, etc.
```

### Валидация данных

Все данные, извлеченные Scout Agent, должны проходить валидацию:

1. **Проверка диапазонов**:
   - Magnitude Metrics: должны быть в диапазоне [0.0, 1.0]
   - Directional Metrics: должны быть в диапазоне [-1.0, 1.0]

2. **Проверка таксономии**:
   - `entityPath` должен соответствовать допустимым путям
   - `sectorPath` должен соответствовать допустимым путям

3. **Проверка обязательных метрик**:
   - Для каждого типа сущности определен набор обязательных метрик

## Расширение таксономии

Таксономия может быть расширена добавлением новых путей и метрик. При расширении необходимо:

1. Обновить данный документ
2. Обновить схемы валидации
3. Обновить промпты Scout Agent
4. Версионировать таксономию (v1.1, v1.2, и т.д.)

## Версионирование

Текущая версия: **v1.0**

При изменении таксономии:
- Минорные изменения (добавление путей/метрик): v1.1, v1.2, ...
- Мажорные изменения (изменение принципов): v2.0

Версия таксономии хранится в метаданных HorizonFragment и Frame.

## См. также

- [Модель данных](data-model.md)
- [Scout Workflow](../workflows/scout-workflow.md)
- [Архитектурный обзор](overview.md)

