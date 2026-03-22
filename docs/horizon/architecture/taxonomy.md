# Таксономия Horizon (backend-first)

## Обзор

В Sanskrite/Horizon есть два “слоя” таксономии:

- **Runtime taxonomy (источник правды для бэкенда)**: хранится в Firestore в коллекции `horizon_taxonomy`, doc id = версия (по умолчанию `v1`). Бэкенд загружает её при старте и периодически обновляет в памяти. Это то, что реально используется для нормализации/валидации некоторых словарей (например `spherePaths`, `eventType`, `relationType`).
- **Core taxonomy (гайд для классификации/LLM)**: описывает рекомендуемые `entityPath`, `sectorPath` и метрики. Важно: **этот слой сейчас не является строгой allow-list в рантайме** (сервер не блокирует неизвестные значения `entityPath/sectorPath`), но он используется как контракт и база для промптов/классификатора.

Отдельно от этих двух слоёв теперь существует третий, самостоятельный operational layer:

- **Supply Chain Canonical Control Plane**: словари канонических продуктов, product synonyms, product transform/assembly pairs и candidate queues.

Он не живёт внутри общего `horizon_taxonomy` blob и описан отдельно:

- [Supply Chain Canonical Control Plane](/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/supply-chain-canonical-control-plane.md)

Таксономия в документации ниже приведена в backend-first виде: сначала то, что “enforced” бэкендом, потом — рекомендации (Core taxonomy guide).

## 0. Runtime taxonomy (Firestore, backend source of truth)

### Где хранится и как загружается

- **Firestore**: `horizon_taxonomy/{version}` (например `horizon_taxonomy/v1`)
- **Загрузка в рантайм**: сервер читает `v1` при старте и авто-рефрешит.
- **Инспекция**: есть утилита `backend/cmd/tools/horizon_get_taxonomy` (печатает текущий документ как JSON).
- **Сидинг/обновление**: есть утилита `backend/cmd/tools/horizon_seed_taxonomy` (записывает дефолтный словарь v1).

### Что внутри (v1, по модели бэкенда)

Runtime taxonomy в коде представлена моделью `HorizonTaxonomy` и включает:

- **canonicalPrefixes**: разрешённые пространства имен для canonicalPath (Horizon-first)
- **allowedSpherePaths + sphereSynonyms**: allow-list сфер и алиасы (label → `/sphere/...`)
- **allowedEventTypes + eventTypeSynonyms**: словарь `eventType` и алиасы (label → нормализованный тип)
- **allowedRelationTypes**: allow-list типов связей dependency edges (upper snake-case)
- **allowedMetricUnits**: allow-list единиц измерения (используется как словарь для промптов/валидаций по необходимости)

Ниже приведены **дефолтные значения v1**, которые сидятся через `backend/cmd/tools/horizon_seed_taxonomy`.
Фактические значения в окружении всегда можно получить через `backend/cmd/tools/horizon_get_taxonomy`.

### canonicalPrefixes (default v1)

`/gov/`, `/org/`, `/fac/`, `/loc/`, `/person/`, `/product/`, `/program/`, `/market/`, `/segment/`, `/event/`, `/sphere/`, `/industry/`, `/media/`, `/deposit/`

### eventType vocabulary (default v1)

`attack`, `cyberattack`, `sanction`, `blockade`, `policy_change`, `license_change`, `partnership`, `acquisition`, `release`, `deprecation`, `pricing_change`, `availability_change`, `capability_change`, `protest`, `strike`, `election`, `warning`, `announcement`, `incident`, `other`

**Нормализация**: если значение не поддерживается — приводится к `other` (в т.ч. через `eventTypeSynonyms`).

### relationType vocabulary (default v1)

`SUPPLIES_TO`, `LOCATED_IN`, `OWNS`, `PRODUCES`, `CONSUMES`, `OPERATES_IN`, `REGULATES`, `COMPETES_WITH`, `INFLUENCES`, `DEPENDS_ON`, `SUBSIDIARY_OF`, `TRANSFORMS_INTO`, `MANAGES`, `CONTROLS`, `EMPLOYED_BY`, `BOARD_MEMBER_OF`, `FOUNDED`, `INVESTS_IN`, `PARTNERS_WITH`, `AUDITED_BY`, `RELATIVE_OF`, `RELATED_TO`, `EXTRACTS_FROM`

**Нормализация**: unknown → `INFLUENCES` (safe default), `DEPENDS` → `DEPENDS_ON`, `OWNED_BY` → `SUBSIDIARY_OF`.

### Временная валидность рёбер (ValidFrom/ValidTo)

Каждое ребро имеет поля `ValidFrom` и `ValidTo`, которые определяют **реальное время действия** связи в мире:
- `ValidFrom`: Когда связь стала действующей (начало отношений, инвестиции, назначение на должность)
- `ValidTo`: Когда связь прекратила действие (выход из инвестиции, увольнение, продажа доли)

**Важно**: Это **не** время наблюдения данных (epistemic time) — для этого используется `MetricValue.ObservedAt + Provenance`.

**Методы фильтрации**:
- `IsActive(at time.Time)`: Проверяет, активна ли связь в момент `at`
- `IsActiveNow()`: Проверяет, активна ли связь сейчас
- `IsHistorical()`: Проверяет, закончилась ли связь (ValidTo < now)

### Edge Modality (edgeModality)

Квалификатор `edgeModality` в `Qualifiers` описывает статус связи:
- `actual`: Действующая связь (default)
- `projected`: Планируемая связь (например, для объектов в стадии construction)
- `historical`: Историческая связь (auto-set если ValidTo в прошлом)
- `potential`: Потенциальная связь (обсуждается, но не подтверждена)

**Auto-set логика**:
- Если `target.lifecycleStage` не `operational` или `expansion` и ValidFrom в будущем → `projected`
- Если `ValidTo` в прошлом → `historical`

### allowedMetricUnits (default v1)

`usd`, `eur`, `rub`, `units`, `units_per_day`, `days`, `hours`, `ratio`, `index_point`, `code`, `tons`, `barrels`, `cubic_meters`, `percent`

**Pattern-based единицы**: Валюты и индексы могут быть любыми (не ограничены allowlist):
- `currency_code_pattern`: Любой 3-буквенный код валюты (USD, EUR, RUB, KZT, USDT, и т.д.)
- `index_point`: Любой числовой индекс

### allowedDepositResources (default v1)

Канонические типы ресурсов для `/deposit/` сущностей:

`oil`, `natural-gas`, `coal`, `iron-ore`, `copper-ore`, `gold-ore`, `silver-ore`, `lithium-ore`, `uranium-ore`, `bauxite`, `phosphate`, `potash`, `diamond`, `water`, `other`

### allowedLifecycleStages (default v1)

Канонические стадии жизненного цикла для ORGANIZATION, FACILITY, DEPOSIT:

`concept`, `planning`, `construction`, `operational`, `expansion`, `suspended`, `closed`, `decommissioned`, `other`

**Синонимы** (нормализуются автоматически):
- `exploration` → `concept`
- `pre-feasibility`, `feasibility` → `planning`
- `development` → `construction`
- `producing`, `active` → `operational`
- `mothballed`, `shut-in` → `suspended`
- `abandoned` → `closed`

### allowedEdgeMetrics (default v1)

Метрики, разрешённые для разных типов рёбер:

| Edge Type | Allowed Metrics |
|-----------|-----------------|
| `OWNS` | `ownership_percent`, `voting_rights_percent` |
| `INVESTS_IN` | `investment_amount`, `valuation_at_investment` |
| `SUPPLIES_TO` | `annual_volume`, `contract_value` |
| `PRODUCES` | `annual_production_volume`, `reserves_volume` |
| `CONSUMES` | `annual_consumption_volume` |

### allowedEdgeModalities (default v1)

`actual`, `projected`, `historical`, `potential`

### allowedServiceDomains (default v1)

Домены для канонических путей сервисов `/product/service/{domain}/...`:

`it`, `finance`, `consulting`, `logistics`, `legal`, `hr`, `marketing`, `energy`, `mining`, `healthcare`, `construction`, `other`

### allowedServiceSubdomains (default v1)

Субдомены для каждого домена:

| Domain | Subdomains |
|--------|------------|
| `it` | `software-development`, `cloud-services`, `cybersecurity`, `it-consulting`, `data-analytics`, `telecom`, `other` |
| `finance` | `banking`, `asset-management`, `insurance`, `advisory`, `other` |
| `consulting` | `management`, `strategy`, `technical`, `other` |
| `logistics` | `transportation`, `warehousing`, `freight-forwarding`, `other` |
| `energy` | `drilling`, `exploration`, `refining`, `power-generation`, `other` |
| `mining` | `exploration`, `drilling`, `processing`, `other` |
| `construction` | `engineering`, `project-management`, `heavy-equipment-rental`, `other` |
| `other` | `general`, `miscellaneous` |

## 0.1 Canonical paths (то, что реально валидируется кодом)

CanonicalPath — это **Horizon-first** идентификатор фрагмента/сущности, в формате:

- **формат**: `/{namespace}/{slug}/{slug}/...`
- **slug**: нижний регистр, `[a-z0-9-]`
- **namespace (enforced)**: `org`, `person`, `product`, `program`, `market`, `fac`, `loc`, `sphere`, `event`
- **namespace (enforced)**: `gov`, `org`, `person`, `product`, `program`, `market`, `segment`, `fac`, `loc`, `sphere`, `event`, `industry`, `media`

### Organization & Government canonicalPath (важно для дедупа и коллизий)

- **Organizations (jurisdiction-scoped, preferred)**:
  - `/org/{iso2|na}/{slug}`
  - `iso2`: страна происхождения / основной юрисдикции (ISO3166-1 alpha2 lowercase). Если неизвестно — `na`.
  - Legacy формат `/org/{slug}` может существовать в графе (backward compatibility), но для новых сущностей предпочтителен scoped формат.
- **Government bodies (jurisdiction-scoped, required)**:
  - `/gov/{iso2|na}/{slug}`
  - Используется для министерств/ведомств/регуляторов, чтобы избежать ложных совпадений между странами (например, “Ministry of Defence”).

### Location canonicalPath (важное отличие от entityPath)

- **canonicalPath для локаций (namespace `loc`)** в коде канонизируется как:
  - `/loc/{country}/{admin1}/{city}`
  - `country`: ISO3166-1 alpha2 (lowercase)
  - `admin1/city`: slug или `na`

Это **не то же самое**, что `entityPath` (см. ниже): `entityPath` может быть `/Geo/...`, а canonicalPath для location-fragment при этом будет `/loc/...`.

### Deposit canonicalPath (для месторождений)

- **canonicalPath для месторождений (namespace `deposit`)**:
  - `/deposit/{resource-slug}/{deposit-slug}`
  - `resource-slug`: канонический тип ресурса из `allowedDepositResources` (oil, natural-gas, iron-ore, и т.д.)
  - `deposit-slug`: slug названия месторождения (permian-basin, ghawar-field, pilbara-mine)

**Примеры**:
- "Пермский нефтяной бассейн" → `/deposit/oil/permian-basin`
- "Месторождение Гавар (Саудовская Аравия)" → `/deposit/oil/ghawar-field`
- "Железорудная шахта Пилбара" → `/deposit/iron-ore/pilbara-mine`

**Fallback**: Если тип ресурса неизвестен → `/deposit/other/{deposit-slug}`

### Product canonicalPath (включая модели/SKU)

- **canonicalPath для продуктов (namespace `product`)**:
  - `/product/{category}/{type}/{stage}/{slug}[/{model-sku-slug}]`
  - `category`: `raw` | `component` | `finished` | `service`
  - `type`: `mineral` | `agricultural` | `chemical` | `electronic` | `mechanical` | `energy` | и т.д.
  - `stage`: `extraction` | `refining` | `processing` | `manufacturing` | `assembly` | `delivery` | и т.д.
  - `slug`: slug названия продукта
  - `model-sku-slug`: **(опционально)** slug конкретной модели/SKU

**Примеры**:
- "Литиевая руда" → `/product/raw/mineral/extraction/lithium-ore`
- "Карбонат лития" → `/product/raw/chemical/refining/lithium-carbonate`
- "Батарейная ячейка" → `/product/component/electronic/manufacturing/battery-cell`
- "Электромобиль" → `/product/finished/mechanical/assembly/electric-vehicle`
- "Экскаватор CAT 320" → `/product/finished/mechanical/manufacturing/excavator/excavator-cat-320`

**Fallback**: Если category/type/stage неизвестны → `/product/{slug}`

### Service canonicalPath (для сервисов)

- **canonicalPath для сервисов (подтип product)**:
  - `/product/service/{domain}/{subdomain}/{slug}[/{offering-slug}]`
  - `domain`: домен сервиса из `allowedServiceDomains` (it, finance, consulting, и т.д.)
  - `subdomain`: субдомен из `allowedServiceSubdomains` (software-development, cloud-services, и т.д.)
  - `slug`: slug названия сервиса
  - `offering-slug`: **(опционально)** slug конкретного предложения/пакета

**Примеры**:
- "Cloud Hosting" → `/product/service/it/cloud-services/cloud-hosting`
- "IT Consulting" → `/product/service/it/consulting/it-consulting`
- "Management Consulting" → `/product/service/consulting/management/management-consulting`
- "Нефтебуровые услуги" → `/product/service/energy/drilling/oil-drilling`

**Fallback**: Если domain/subdomain неизвестны → `/product/service/other/general/{slug}`

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

## 2. Entity Types Taxonomy (entity_path) — guide (не strict allow-list в рантайме)

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
/Resource/Deposit                      # Месторождение природных ресурсов (NEW)
```

### 2.6. Deposit (Месторождения) — NEW

```
/Resource/Deposit/Oil                  # Нефтяное месторождение
/Resource/Deposit/Gas                  # Газовое месторождение
/Resource/Deposit/Coal                 # Угольное месторождение
/Resource/Deposit/IronOre              # Железорудное месторождение
/Resource/Deposit/Gold                 # Золотое месторождение
/Resource/Deposit/Lithium              # Литиевое месторождение
/Resource/Deposit/Uranium              # Урановое месторождение
/Resource/Deposit/Other                # Другое месторождение
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

## 3. Industry & Sector Taxonomy (sector_path) — guide (не strict allow-list в рантайме)

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

## 5. Influence Spheres Taxonomy (spherePaths) — enforced через runtime taxonomy

Иногда событие/утверждение влияет не “на объект” напрямую, а на **сферу**: геополитику, социум, киберпространство, логистику и т.п.
Для этого в `ontology_facts` используется поле `spherePaths: []string` — **множественные** канонизированные пути, которые:
- используются для фильтрации/фасетинга в Vertex AI Search,
- дают основу для будущего распространения волн “по сфере” и “по месту” (политика распространения может эволюционировать отдельно).

**Канонический формат**:

```
/sphere/{domain}/{subdomain...}
```

**Примеры (дефолтный словарь v1, сидится через `horizon_seed_taxonomy`):**
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

Ниже — **рекомендуемые** проверки качества данных. Важно: в текущей реализации бэкенда **не всё из этого строго enforced** (см. разделы выше про runtime taxonomy).

1. **Проверка диапазонов**:
   - Magnitude Metrics: должны быть в диапазоне [0.0, 1.0]
   - Directional Metrics: должны быть в диапазоне [-1.0, 1.0]

2. **Проверка таксономии**:
   - `canonicalPath` должен соответствовать правилам canonical namespace/slug (enforced)
   - `spherePaths`, `eventType`, `relationType` должны нормализоваться через runtime taxonomy (enforced/normalized)
   - `entityPath` / `sectorPath` — сейчас это **гайд для классификации**, а не strict allow-list в рантайме

3. **Проверка обязательных метрик**:
   - Наборы “обязательных метрик” могут быть определены на уровне продукта/агента; бэкенд не должен ломать ingest из-за отсутствия не-критичных полей

## Расширение таксономии

При расширении таксономии ориентируемся на backend-first подход:

1. Обновить runtime taxonomy в Firestore (`horizon_taxonomy/v1`) или создать новый документ версии (`v2`) для несовместимых изменений
2. Обновить данный документ (чтобы отражал фактическое runtime поведение)
3. Обновить промпты/классификатор Scout Agent (если меняются словари/правила)

## Версионирование

Текущая runtime-версия в бэкенде: **`v1`** (doc id в Firestore: `horizon_taxonomy/v1`).

При изменении runtime taxonomy:
- Минорные изменения (добавление алиасов/значений): можно менять документ `v1` (понимая, что это “live” изменение).
- Мажорные изменения: заводим новый документ `v2` и обновляем бэкенд на загрузку `v2` (и/или делаем выбор версии конфигом).

## См. также

- [Модель данных](data-model.md)
- [Scout Workflow](../workflows/scout-workflow.md)
- [Архитектурный обзор](overview.md)
