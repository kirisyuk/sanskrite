# Roadmap реализации системы мониторинга горизонта

## Обзор

Данный документ описывает план реализации системы мониторинга горизонта по этапам. Реализация разбита на логические этапы, каждый из которых добавляет новую функциональность и может быть развернут независимо.

## Текущий план: стабилизация → улучшения (backend-first)

Ниже — актуальный TODO-план, который отражает **текущее состояние реализации** (код уже в основном готов) и следующие шаги.
Принцип: **сначала стабилизируем текущую модель**, затем добавляем минимальные онтологические улучшения, не раздувая таксономию.

### A) Стабилизация текущей модели (MVP hardening)

**Цель**: сделать текущую систему предсказуемой в проде (без “скрытых” несоответствий данных/доков, без ручной магии).

**TODO**:
- **Срочные задачи**
  - Вынести метрики из фрагментов в оверлей
  - [x] Реализовать подгрузку материалов вручную из фрагмента-продукта через wikidata - срочно
    - POST `/api/horizon/fragments/{id}/expand-materials` — синхронный вызов, возвращает статистику
  - Добавить клиентов организации в скаутинг, вызываемый вручную - тоже срочно
- **Wikidata QID resolver (suggest-only → gated auto-apply)**
  - Вынести поиск/скоринг Wikidata QID из `cmd/tools/horizon_find_missing_wikidata` в ядро как **suggest-only resolver** (без авто-записи).
  - Добавить кэширование/TTL, type-gating (например, `P31=Q5` для PERSON), и телеметрию качества (score распределения, false positives).
  - Добавить режим **auto-apply** только под feature-flag + строгие пороги/правила + dry-run.
  - Перед включением auto-apply: прогнать тщательную проверку качества на репрезентативной выборке (precision/recall, ручной аудит top ошибок).
- **Vertex AI Search для Horizon fragments**
  - Завершить overwrite re-export всех фрагментов в GCS в финальном формате Discovery Engine.
  - Перезапустить backfill (батчи) и убедиться, что поиск по fragments возвращает результаты.
  - Зафиксировать/проверить необходимые Firestore индексы под запросы Horizon.
- **QueryAddress**
  - Задеплоить “adult” реализацию QueryAddress (AST, скобки, NOT, корректный OR).
  - Подтвердить поведение `sphere:`: работает через `ontology_facts` и поддерживает только exact match (без wildcard).
  - Подтвердить поведение `location:`: best-effort `/loc/{country}/{admin1}/na` из `homeLocation`.
  - Убедиться, что background refresh корректно управляет подписками (added/removed paths) и не оставляет “мёртвых” подписок.
- **Документация (backend-first)**
  - Держать `architecture/taxonomy.md` и `architecture/query-address.md` синхронно с кодом (не обещать enforcement, которого нет).
  - В `IMPLEMENTATION_AUDIT.md` поддерживать актуальный “snapshot” (деплой/инфра/текущее состояние миграций).
- **Операционные контуры**
  - После завершения backfill: безопасно включить обратно Cloud Tasks / Scout queue (если была на паузе).

**Критерии приемки**:
- Re-export/backfill завершены, поиск по fragments в Vertex работает в проде.
- QueryAddress ведёт себя детерминированно (скобки/NOT/OR), документы и код не противоречат друг другу.
- Нет “обязательных” ручных шагов, кроме чётко описанных в docs.

### B) Улучшения онтологии (после стабилизации; минимальные элементы, без усложнения таксономии)

**Цель**: добавить выразительность “реальности” через универсальные онтологические оси (time/roles/modality/jurisdiction), не пытаясь сделать таксономию полной онтологией мира.

**TODO**:
- **Market Segments (канон `/segment/...` + признаки организации)**
  - Ввести в `HorizonFragment` (для `ORGANIZATION`/`FACILITY`) поля:
    - `marketSegments: []string` — массив канонических путей сегментов рынка, где работает организация.
    - `orgSize: string` — размер организации (например: `sme` | `le` | `enterprise` | `mega` | `na`).
  - Зафиксировать **плоский канонический путь фиксированной длины** для сегмента рынка:
    - формат: `/segment/{domain}/{vertical}/{geo}`
    - `geo`: либо ISO3166-1 alpha2 lowercase (2 символа), либо `global`.
    - `domain`/`vertical`: контролируемый словарь + synonyms (runtime taxonomy / справочник).
  - Обновить Scout/Research промпты: при разведке `ORGANIZATION`/`FACILITY` извлекать `marketSegments[]` и `orgSize` (роль не атрибут сущности; контекст выражается типом/направлением связи и `reason`).
  - Обновить Critic: если `marketSegments[]` пуст (или очевидно неполон относительно контекста) — добавлять в `missingInfo` точные вопросы/поисковые запросы для сегментов и размера организации.
  - (Опционально позже) Материализовать сегменты отдельными фрагментами с тем же canonical path (`/segment/...`) и продублировать связь “org → segment” через отдельный edge type (для индексации/UX).
- **Time (единый контракт времени)**
  - Формализовать и использовать консистентно: `occurredAt` (мир), `observedAt` (данные), `effectiveFrom/effectiveTo` (модель).
  - Добавить `effectiveFrom/effectiveTo` для отношений (edges/relations), чтобы связи не были “вечными”.
- **Modality (наблюдаемость/сценарность)**
  - Добавить `modality` на уровень фактов/утверждений (например: observed / reported / predicted / hypothetical).
  - Обновить Watchdog/Scout правила, чтобы propagation/обновления учитывали modality (например, predicted не всегда должен менять state).
- **Jurisdiction / Scope**
  - Явно договориться, что такое `locationPath`/`jurisdictionPath` для фактов и как он соотносится с `/loc/...`.
  - Свести к одному каноническому формату и задокументировать.
- **Roles/Relations как слой реальности**
  - Для `DependencyEdge`/relations добавить временной диапазон + минимальные атрибуты (confidence/sourceRef) при необходимости.
  - Расширить типы связей (минимальный OSINT‑слой, без “ролей” как атрибута человека):
    - `AUDITED_BY` (ORGANIZATION/FACILITY → ORGANIZATION): внешний аудитор компании.
    - `RELATIVE_OF` (PERSON ↔ PERSON): прямое родство (публичные родственники).
    - `RELATED_TO` (PERSON ↔ PERSON): прочие общеизвестные связи (бизнес‑партнёры/друзья/однокурсники и т.п.), контекст в `reason`/qualifiers.
  - Обновить Scout/Research промпты и Critic `missingInfo` под новые связи (аудитор компании; публичные родственники; public "related" связи) с жёсткими правилами по направлению/типам.

---
### Большой раздел работ (единый блок): расширение онтологии/таксономии Horizon
Отсюда и до секции **“Обработка метрик при propagation (семантика поверх "доставки дельт")”** включительно — один целостный блок работ: новые canonical paths, новые fragment/edge types, нормализация, требования Critic/Scout, lifecycle/time, edge metrics/temporal validity, и интеграция с propagation.
---
- **Требования к скаутингу металлургических и горнодобывающих компаний (metal & mining)**
  - При скаутинге компаний из секторов `/Materials/Mining/*` (Precious_Metals, Industrial_Metals, Rare_Earths) scout-critic **обязательно должен требовать наличие связанных объектов типа `/fac`** (Facility) и/или `/deposit/` (Natural Resource Deposit).
  - Для каждого связанного `/fac` требуется следующая информация:
    - **Продукты**: список продуктов, которые добываются/производятся на объекте (связи `PRODUCES` с `/product/...` путями).
    - **Запасы и добыча по каждому продукту отдельно**:
      - **Разведанные запасы** (proven reserves) для каждого продукта: объёмы с единицами измерения из канонического словаря (`allowedMetricUnits` в `horizon_taxonomy`).
      - **Вероятные запасы** (probable reserves) для каждого продукта: объёмы с единицами измерения из канонического словаря.
      - **Добыча** (production/extraction) для каждого продукта: объёмы с единицами измерения из канонического словаря.
  - Если запасы известны только на уровне месторождения (deposit), а не конкретного `/fac`:
    - Создавать сущность `/deposit/` с reserves-метриками.
    - Связывать `/fac → EXTRACTS_FROM → /deposit/`.
  - Единицы измерения должны соответствовать каноническому словарю (`allowedMetricUnits`), например: `metric_tons`, `kt`, `mt`, `oz`, `moz`, `kg`, `lbs` и т.д.
  - Обновить Critic Agent: если для metal & mining компании отсутствуют связанные `/fac` объекты или для `/fac`/`/deposit/` отсутствует информация о продуктах/запасах/добыче по каждому продукту — добавлять в `missingInfo` конкретные вопросы для поиска этой информации.
  - Обновить Scout/Research промпты: при разведке metal & mining компаний явно извлекать информацию о производственных объектах (`/fac`), месторождениях (`/deposit/`), их продуктах, а также запасы и добычу **отдельно для каждого продукта**.
- **Требования к скаутингу нефтегазовых компаний (oil & gas)**
  - При скаутинге компаний из секторов `/Energy/Fossil_Fuels/Oil_Gas_*` (Upstream, Midstream, Downstream) scout-critic **обязательно должен требовать наличие связанных объектов типа `/fac`** (Facility) и/или `/deposit/` (Natural Resource Deposit — basin/field).
  - Для **upstream** компаний (exploration & production):
    - Требовать связанные `/deposit/` (месторождения/бассейны) с reserves-метриками.
    - Требовать связанные `/fac/` (скважины, платформы, терминалы) с production-метриками.
    - Связи: `/fac → EXTRACTS_FROM → /deposit/`, `/org → OPERATES_IN → /deposit/`.
  - Для **midstream** компаний (pipelines, storage, LNG):
    - Требовать связанные `/fac/` (трубопроводы, хранилища, LNG-терминалы) с capacity-метриками.
  - Для **downstream** компаний (refining, petrochemicals):
    - Требовать связанные `/fac/` (НПЗ, нефтехимические заводы) с capacity/throughput-метриками.
  - Для каждого связанного `/deposit/` (для upstream) требуется:
    - **Продукты**: crude oil, natural gas, condensate, NGL (связи `PRODUCES` с `/product/...`).
    - **Запасы по классификации SPE-PRMS**:
      - `reserves_proven_{product}` (1P): доказанные запасы
      - `reserves_probable_{product}` (2P): вероятные запасы
      - `reserves_possible_{product}` (3P): возможные запасы (опционально)
    - **Добыча**: `production_{product}` — текущий уровень добычи.
  - Единицы измерения для O&G: `bbl`, `mbbl`, `mmbbl` (нефть), `bcf`, `tcf`, `mcf` (газ), `boe` (barrels of oil equivalent), `bpd`, `mbpd` (добыча в день).
  - Обновить Critic Agent: если для O&G upstream компании отсутствуют связанные `/deposit/` или `/fac/`, или отсутствует информация о reserves/production — добавлять в `missingInfo` конкретные вопросы.
  - Обновить Scout/Research промпты: при разведке O&G компаний явно извлекать информацию о месторождениях (`/deposit/`), производственных объектах (`/fac/`), и reserves/production **с корректными единицами измерения**.
- **Новый тип сущности: Natural Resource Deposit (`/deposit/`)**
  - **Назначение**: моделирование геологических единиц (месторождения, бассейны, формации) с ресурсными характеристиками. Отличается от `/loc/` (административно-географическая единица) и `/fac/` (производственный объект).
  - **Canonical path format**: `/deposit/{resource-slug}/{deposit-slug}`
    - `{resource-slug}`: основной (primary) тип ресурса в deposit'е
    - `{deposit-slug}`: уникальный идентификатор месторождения (lowercase, slugified)
  - **Примеры canonical paths**:
    - `/deposit/crude-oil/permian-basin` — Permian Basin (Texas/New Mexico)
    - `/deposit/crude-oil/ghawar` — Ghawar Field (Saudi Arabia)
    - `/deposit/natural-gas/north-sea` — North Sea gas fields
    - `/deposit/lithium/atacama-salar` — Salar de Atacama (Chile)
    - `/deposit/copper/escondida` — Escondida copper deposit (Chile)
    - `/deposit/iron-ore/pilbara` — Pilbara iron ore province (Australia)
    - `/deposit/gold/witwatersrand` — Witwatersrand Basin (South Africa)
    - `/deposit/uranium/olympic-dam` — Olympic Dam (Australia, polymetallic)
  - **FragmentType**: `DEPOSIT`
  - **Deposit-specific атрибуты** (через `Identifiers` или новые поля):
    - `depositKind`: классификация типа месторождения
      - O&G: `basin`, `field`, `formation`, `play`, `block`
      - Mining: `deposit`, `belt`, `province`, `district`, `salar`
    - `primaryResource`: основной ресурс (используется в canonical path)
    - `secondaryResources`: дополнительные ресурсы (для polymetallic deposits)
    - `geologicalAge`: геологический возраст (опционально)
    - `discoveryYear`: год открытия (опционально)
  - **Multi-resource deposits**: если deposit содержит несколько ресурсов (напр. Olympic Dam = uranium + copper + gold):
    - Primary resource → идёт в canonical path
    - Secondary resources → хранятся в `Identifiers["secondaryResources"]` или `Tags`
    - Reserves/production хранятся отдельно для каждого ресурса в `State`
  - **Связи (relation types)**:
    - `EXTRACTS_FROM` (новый тип): `/fac/` → `/deposit/` — facility добывает из месторождения
    - `LOCATED_IN`: `/deposit/` → `/loc/` — месторождение находится в регионе
    - `OPERATES_IN`: `/org/` → `/deposit/` — компания работает в месторождении
    - `PRODUCES`: `/deposit/` → `/product/` — месторождение производит ресурс (агрегированно)
  - **Хранение метрик reserves/production** (используем существующий `State map[string]MetricValue`):
    - Naming convention для ключей:
      - `reserves_proven_{product}` — доказанные запасы (1P / SPE-PRMS proven)
      - `reserves_probable_{product}` — вероятные запасы (2P / SPE-PRMS probable)
      - `reserves_possible_{product}` — возможные запасы (3P / SPE-PRMS possible)
      - `production_{product}` — текущая добыча
    - `MetricValue.Value` — абсолютное значение (float64, поддерживает большие числа)
    - `MetricValue.Unit` — единица измерения из `allowedMetricUnits`
    - `MetricValue.ObservedAt` — дата актуальности данных (важно для reserves)
    - `MetricValue.Confidence` — уверенность в оценке
    - `MetricValue.Provenance` — источник данных
  - **Пример структуры для O&G deposit**:
    ```json
    {
      "canonicalPath": "/deposit/crude-oil/permian-basin",
      "type": "DEPOSIT",
      "labels": {"en": "Permian Basin"},
      "identifiers": {
        "depositKind": "basin",
        "primaryResource": "crude-oil",
        "secondaryResources": "natural-gas,ngl"
      },
      "homeLocation": {
        "name": "West Texas / Southeast New Mexico",
        "iso3166_1": "US",
        "iso3166_2": "US-TX"
      },
      "state": {
        "reserves_proven_crude_oil": {"value": 46000000000, "unit": "bbl", "confidence": 0.95, "observedAt": "2024-01-15T00:00:00Z"},
        "reserves_probable_crude_oil": {"value": 12000000000, "unit": "bbl", "confidence": 0.8},
        "reserves_proven_natural_gas": {"value": 75, "unit": "tcf", "confidence": 0.9},
        "production_crude_oil": {"value": 5500000, "unit": "bpd", "confidence": 0.95}
      }
    }
    ```
  - **Пример структуры для mining deposit**:
    ```json
    {
      "canonicalPath": "/deposit/lithium/atacama-salar",
      "type": "DEPOSIT",
      "labels": {"en": "Salar de Atacama"},
      "identifiers": {
        "depositKind": "salar",
        "primaryResource": "lithium",
        "secondaryResources": "potassium"
      },
      "homeLocation": {
        "name": "Atacama Desert, Chile",
        "iso3166_1": "CL",
        "iso3166_2": "CL-AN"
      },
      "state": {
        "reserves_proven_lithium_carbonate": {"value": 6800000, "unit": "metric_tons", "confidence": 0.9},
        "reserves_probable_lithium_carbonate": {"value": 2100000, "unit": "metric_tons", "confidence": 0.75},
        "production_lithium_carbonate": {"value": 85000, "unit": "metric_tons_per_year", "confidence": 0.95},
        "reserves_proven_potassium": {"value": 1200000000, "unit": "metric_tons", "confidence": 0.85}
      }
    }
    ```
  - **Связь `/deposit/` и `/product/`** — разные сущности:
    - `/deposit/` — геологический объект с запасами
    - `/product/` — товар/материал в supply chain
    - Один deposit → много products (Atacama даёт lithium и potassium)
    - Один product → много deposits (lithium добывают из разных месторождений)
  - **Scout/Critic интеграция**:
    - При скаутинге extractive company → проверять наличие `/deposit/` или `/fac/` с reserves
    - Если reserves известны только для deposit (не для fac) → создать `/deposit/`, привязать reserves туда
    - Если reserves известны для конкретного fac → хранить на fac
    - Critic должен требовать `/deposit/` для upstream O&G и mining companies
- **Расширение таксономии продуктов для extractive industries**
  - **AllowedProductTypes** — добавить:
    - `hydrocarbon` — crude oil, condensate, NGL
    - `fuel` — refined fuels (gasoline, diesel, jet fuel)
    - `petrochemical` — ethylene, propylene, polymers
  - **AllowedProductStages** — добавить:
    - `liquefaction` — LNG production
    - `cracking` — petrochemical cracking
  - **KnownProducts** — расширить словарь:
    - **O&G Upstream**: `crude-oil`, `natural-gas`, `condensate`, `ngl`
    - **O&G Midstream/Downstream**: `lng`, `gasoline`, `diesel`, `jet-fuel`, `naphtha`, `ethylene`, `propylene`
    - **Mining (расширить)**: `iron-ore`, `copper-ore`, `copper-concentrate`, `refined-copper`, `gold-ore`, `gold-bullion`, `rare-earth-ore`, `neodymium-oxide`, `uranium-ore`, `yellowcake`
  - **ProductSynonyms** — добавить алиасы:
    - O&G: `oil` → `crude-oil`, `petroleum` → `crude-oil`, `gas` → `natural-gas`, `petrol` → `gasoline`, etc.
    - Mining: `iron` → `iron-ore`, `copper` → `copper-ore`, `gold` → `gold-ore`, `u3o8` → `yellowcake`, etc.
  - **Примеры canonical paths для продуктов**:
    - `/product/raw/hydrocarbon/extraction/crude-oil`
    - `/product/raw/hydrocarbon/liquefaction/lng`
    - `/product/finished/fuel/refining/gasoline`
    - `/product/component/petrochemical/cracking/ethylene`
    - `/product/raw/mineral/extraction/iron-ore`
    - `/product/component/mineral/refining/refined-copper`
- **Опциональный последний сегмент: конкретное название продукта (model/SKU)**
  - **Формат**:
    ```
    Goods:    /product/{category}/{type}/{stage}/{slug}[/{model-slug}]
    Services: /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}]
    ```
  - **Назначение**: Позволяет моделировать конкретные модели/SKU/брендированные продукты:
    - `/product/finished/mechanical/manufacturing/excavator` — экскаваторы (generic)
    - `/product/finished/mechanical/manufacturing/excavator/cat-390f` — конкретная модель Caterpillar 390F
    - `/product/finished/mechanical/manufacturing/excavator/komatsu-pc2000` — Komatsu PC2000
  - **Правила**:
    - Последний сегмент **опционален** — можно использовать generic slug
    - Model-slug должен быть **официальным названием** (или его каноническим slug'ом)
    - Slug'ы нормализуются: lowercase, дефисы, без спецсимволов
    - Pattern matching работает: `/product/*/mechanical/*/excavator/*` поймает все модели экскаваторов
  - **Примеры для Goods**:
    | Категория | Generic product | Конкретная модель |
    |-----------|-----------------|-------------------|
    | Automotive | `/product/finished/mechanical/assembly/electric-vehicle` | `.../electric-vehicle/tesla-model-3` |
    | Semiconductors | `/product/component/electronic/manufacturing/gpu` | `.../gpu/nvidia-h100` |
    | Pharma | `/product/finished/healthcare/manufacturing/vaccine` | `.../vaccine/pfizer-comirnaty` |
    | Aircraft | `/product/finished/mechanical/assembly/commercial-aircraft` | `.../commercial-aircraft/boeing-737-max` |
    | Mining Equipment | `/product/finished/mechanical/manufacturing/haul-truck` | `.../haul-truck/caterpillar-797f` |
  - **Примеры для Services**:
    | Категория | Generic service | Конкретное предложение |
    |-----------|-----------------|------------------------|
    | Cloud | `/product/service/it/cloud/platform` | `.../platform/aws-ec2` |
    | CRM | `/product/service/it/software/crm-saas` | `.../crm-saas/salesforce-sales-cloud` |
    | Banking | `/product/service/financial/banking/payment-processing` | `.../payment-processing/stripe-connect` |
  - **Когда использовать model-slug**:
    - Когда нужно отслеживать **конкретную модель** в supply chain (e.g. "Boeing 737 MAX" vs "Boeing 787")
    - Когда модель имеет **уникальные характеристики** (sanctions, safety issues, etc.)
    - Когда нужно привязать **capacity/production** к конкретной модели
    - **Не обязательно** если generic category достаточно для анализа
  - **Scout/Critic интеграция**:
    - Scout извлекает model name если упоминается в источнике
    - Critic **не требует** model-slug — это enhancement, не mandatory
    - Для связей `PRODUCES` model-slug полезен если production идёт по конкретным моделям
  - **KnownProductModels** — словарь известных моделей (опционально):
    ```go
    KnownProductModels: map[string]ProductModelSpec{
        "tesla-model-3": {GenericSlug: "electric-vehicle", Manufacturer: "tesla"},
        "nvidia-h100":   {GenericSlug: "gpu", Manufacturer: "nvidia"},
        "boeing-737-max": {GenericSlug: "commercial-aircraft", Manufacturer: "boeing"},
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/taxonomy/runtime.go` → обновить `ValidateProductPath()` для опционального 5го сегмента
    - [ ] `backend/internal/models/horizon_taxonomy.go` → опционально `KnownProductModels map[string]ProductModelSpec`
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать model-slug
- **Расширение canonical path для Services (отдельная структура для услуг)**
  - **Проблема**: Текущий формат `/product/{category}/{type}/{stage}/{slug}` не подходит для услуг:
    - `type` (mineral/electronic/chemical) — бессмысленно для IT-консалтинга
    - `stage` (extraction/manufacturing) — бессмысленно для услуг
    - Если domain услуги только в slug — **pattern matching невозможен** для модели рисков
  - **Зачем нужна сегментация**: Модель рисков/ограничений будет:
    1. Канонизировать ограничение: "IT-услуги в RU" → pattern `/product/service/it/*`
    2. Алгоритмически искать в графе edges по pattern match
    3. Сегментность canonical path позволяет matching без знания конкретных slug'ов
  - **Решение**: Отдельный формат для services:
    ```
    Goods (текущее):  /product/{category}/{type}/{stage}/{slug}
    Services (новое): /product/service/{domain}/{subdomain}/{slug}
    ```
  - **AllowedServiceDomains** — новый словарь:
    ```go
    AllowedServiceDomains: []string{
        "it",           // IT services, software, cloud, hosting
        "financial",    // banking, insurance, payments, investment
        "legal",        // law, compliance, audit, notary
        "consulting",   // management, strategy, advisory
        "logistics",    // transport, warehousing, customs
        "engineering",  // R&D, design, architecture
        "media",        // advertising, PR, content, broadcasting
        "telecom",      // communications, internet, mobile
        "education",    // training, certification, academic
        "healthcare",   // medical, pharma, clinical
        "construction", // building, civil engineering
        "security",     // physical, cyber, investigation
        "hr",           // recruitment, staffing, payroll
        "real-estate",  // brokerage, property management
        "other",        // fallback bucket when domain is unclear (must be allowed if used)
    }
    ```
  - **AllowedServiceSubdomains** — по domain:
    | Domain | Subdomains |
    |--------|------------|
    | `it` | `software`, `cloud`, `hosting`, `consulting`, `integration`, `support`, `security`, `data` |
    | `financial` | `banking`, `insurance`, `payments`, `investment`, `audit`, `tax`, `crypto` |
    | `legal` | `corporate`, `litigation`, `compliance`, `ip`, `sanctions`, `immigration` |
    | `consulting` | `management`, `strategy`, `operations`, `hr`, `marketing` |
    | `logistics` | `freight`, `warehousing`, `customs`, `courier`, `cold-chain` |
    | `engineering` | `design`, `rd`, `testing`, `certification`, `maintenance` |
    | `media` | `advertising`, `pr`, `content`, `broadcasting`, `production` |
    | `telecom` | `mobile`, `fixed`, `internet`, `satellite`, `voip` |
    | `other` | `other` |
  - **Примеры canonical paths для services**:
    - `/product/service/it/software/custom-development`
    - `/product/service/it/cloud/hosting`
    - `/product/service/it/consulting/digital-transformation`
    - `/product/service/financial/banking/payment-processing`
    - `/product/service/financial/audit/external-audit`
    - `/product/service/legal/compliance/sanctions-advisory`
    - `/product/service/legal/corporate/m-and-a`
    - `/product/service/logistics/freight/air-cargo`
    - `/product/service/consulting/strategy/market-entry`
  - **Pattern matching для модели рисков**:
    - "Запретить все IT-услуги" → `/product/service/it/*`
    - "Запретить банковские услуги" → `/product/service/financial/banking/*`
    - "Запретить юр. консультации по санкциям" → `/product/service/legal/compliance/sanctions-*`
    - "Запретить все финансовые услуги" → `/product/service/financial/*`
  - **Пример использования с jurisdictions**:
    ```
    Санкция: "IT-услуги из KZ в RU запрещены"
    
    Канонизация ограничения:
      product_pattern: /product/service/it/*
      source_jurisdiction: kz
      target_jurisdiction: ru
    
    Алгоритмическая проверка:
      for edge in edges where edge.type == "SUPPLIES_TO":
          if edge.source.jurisdiction == "kz" 
             && edge.target.jurisdiction == "ru"
             && match(edge.qualifiers.product, "/product/service/it/*"):
              → RISK SIGNAL
    ```
  - **KnownServices** — словарь известных услуг:
    ```go
    KnownServices: map[string]ServiceSpecData{
        "custom-software-development": {Domain: "it", Subdomain: "software"},
        "cloud-hosting": {Domain: "it", Subdomain: "cloud"},
        "payment-processing": {Domain: "financial", Subdomain: "banking"},
        "external-audit": {Domain: "financial", Subdomain: "audit"},
        "sanctions-compliance": {Domain: "legal", Subdomain: "compliance"},
        "air-freight": {Domain: "logistics", Subdomain: "freight"},
    }
    ```
  - **ServiceSynonyms** — алиасы:
    ```go
    ServiceSynonyms: map[string]string{
        "software development": "custom-software-development",
        "app development": "custom-software-development",
        "web hosting": "cloud-hosting",
        "aws": "cloud-hosting",
        "gcp": "cloud-hosting",
        "azure": "cloud-hosting",
        "payment gateway": "payment-processing",
        "sanctions screening": "sanctions-compliance",
        "aml": "sanctions-compliance",
    }
    ```
  - **Интеграция с goods**: Goods сохраняют текущий формат, services — новый:
    - Goods: `/product/{category}/{type}/{stage}/{slug}` где category ≠ "service"
    - Services: `/product/service/{domain}/{subdomain}/{slug}`
    - Общий prefix `/product/` сохраняется для единообразия
  - **Scout/Critic интеграция**:
    - При извлечении услуг Scout должен определять domain/subdomain
    - Critic требует корректный формат для services
    - Если domain не определён → fallback `/product/service/other/{slug}` с missingInfo
  - **Чеклист изменений**:
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedServiceDomains []string`, `AllowedServiceSubdomains map[string][]string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить domains и subdomains
    - [ ] `backend/internal/taxonomy/runtime.go` → `NormalizeServicePath()`, `ValidateServicePath()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → логика определения domain/subdomain для services
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать формат services
- **Расширение allowedMetricUnits для extractive industries**
  - **O&G units (volume)**:
    - `bbl` — barrel (основная единица нефти)
    - `mbbl` — thousand barrels
    - `mmbbl` — million barrels
    - `bcf` — billion cubic feet (газ)
    - `tcf` — trillion cubic feet (газ)
    - `mcf` — thousand cubic feet (газ)
    - `boe` — barrels of oil equivalent (конвертация газ → нефть)
  - **O&G units (rate)**:
    - `bpd` — barrels per day
    - `mbpd` — thousand barrels per day
    - `mcf_per_day` — thousand cubic feet per day
    - `mmcf_per_day` — million cubic feet per day
  - **Mining units (mass)**:
    - `metric_tons` — метрические тонны
    - `kt` — thousand metric tons
    - `mt` — million metric tons
    - `tons` — short tons (US)
    - `oz` — troy ounces (precious metals)
    - `moz` — million troy ounces
    - `kg` — kilograms
    - `g` — grams
    - `lbs` — pounds
    - `carats` — carats (diamonds)
  - **Mining units (rate)**:
    - `metric_tons_per_year` — годовая добыча
    - `oz_per_year` — унции в год (precious metals)
    - `kt_per_year` — тысячи тонн в год
- **Новый relation type: EXTRACTS_FROM**
  - **Направление**: `/fac/` → `/deposit/` (facility извлекает ресурсы из месторождения)
  - **Семантика**: более точная, чем `LOCATED_IN`, отражает операционную связь
  - **Использование**:
    - Шахта `/fac/bhp/escondida-mine` → `EXTRACTS_FROM` → `/deposit/copper/escondida`
    - Скважина `/fac/aramco/ghawar-field-ops` → `EXTRACTS_FROM` → `/deposit/crude-oil/ghawar`
  - **Fallback**: если точная связь неизвестна, можно использовать `LOCATED_IN`
  - **Добавить в AllowedRelationTypes**: `EXTRACTS_FROM`
- **Allowed deposit resource slugs** (новый словарь в runtime taxonomy):
  - **Hydrocarbons**: `crude-oil`, `natural-gas`, `oil-gas` (combined), `coal`, `oil-sands`, `shale-oil`
  - **Base metals**: `copper`, `zinc`, `nickel`, `aluminum`, `tin`, `lead`
  - **Precious metals**: `gold`, `silver`, `platinum`, `palladium`
  - **Battery metals**: `lithium`, `cobalt`
  - **Ferrous**: `iron-ore`, `manganese`, `chromium`
  - **Strategic/Rare**: `rare-earth`, `uranium`, `tungsten`, `molybdenum`
  - **Industrial minerals**: `potash`, `phosphate`, `sulfur`
- **Чеклист изменений для extractive industries support (код + таксономия)**
  - [ ] **CanonicalPrefixes** — добавить `/deposit/` в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `CanonicalPrefixes`
    - `docs/horizon/architecture/taxonomy.md` (документация)
  - [ ] **FragmentType** — добавить `DEPOSIT` в:
    - `backend/internal/horizon/models/common.go` → `FragmentTypeDeposit = "DEPOSIT"`
    - Обновить switch-case в `engine.go`, `taxonomy.go` и других местах, где определяется тип по canonical path prefix
  - [ ] **AllowedRelationTypes** — добавить `EXTRACTS_FROM` в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedRelationTypes`
    - `backend/internal/taxonomy/runtime.go` → `builtInAllowedRelationTypes`
  - [ ] **AllowedMetricUnits** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedMetricUnits`
    - Добавить: `bbl`, `mbbl`, `mmbbl`, `bcf`, `tcf`, `mcf`, `boe`, `bpd`, `mbpd`, `metric_tons`, `kt`, `mt`, `oz`, `moz`, `kg`, `lbs`, `carats`, `metric_tons_per_year`, `oz_per_year`, `kt_per_year`
  - [ ] **AllowedProductTypes** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedProductTypes`
    - Добавить: `hydrocarbon`, `fuel`, `petrochemical`
  - [ ] **AllowedProductStages** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `AllowedProductStages`
    - Добавить: `liquefaction`, `cracking`
  - [ ] **KnownProducts** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `KnownProducts`
    - Добавить O&G продукты: `crude-oil`, `natural-gas`, `condensate`, `ngl`, `lng`, `gasoline`, `diesel`, `jet-fuel`, `naphtha`, `ethylene`, `propylene`
    - Добавить mining продукты: `iron-ore`, `copper-ore`, `copper-concentrate`, `refined-copper`, `gold-ore`, `gold-bullion`, `rare-earth-ore`, `neodymium-oxide`, `uranium-ore`, `yellowcake`
  - [ ] **ProductSynonyms** — расширить в:
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → `ProductSynonyms`
    - O&G: `oil` → `crude-oil`, `petroleum` → `crude-oil`, `gas` → `natural-gas`, `petrol` → `gasoline`, etc.
    - Mining: `iron` → `iron-ore`, `copper` → `copper-ore`, `gold` → `gold-ore`, `u3o8` → `yellowcake`, etc.
  - [ ] **AllowedDepositResources** — новый словарь в:
    - `backend/internal/models/horizon_taxonomy.go` → добавить поле `AllowedDepositResources []string`
    - `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить словарь
    - `backend/internal/taxonomy/runtime.go` → добавить `AllowedDepositResources` в `RuntimeTaxonomy`
    - `backend/internal/taxonomy/cache_firestore.go` → загрузка из Firestore
  - [ ] **Scout/Critic промпты** — обновить для extractive industries:
    - `backend/internal/horizon/scout/agents/scout_agent.go` → добавить `/deposit/` в canonical path rules
    - `backend/internal/horizon/scout/agents/critic_agent.go` → добавить проверку `/deposit/` для mining/O&G
    - `backend/internal/horizon/scout/taxonomy_classifier.go` → добавить DEPOSIT classification
  - [ ] **Critic: обязательные products для FACILITY** (независимо от specialty):
    - Для **любого `/fac/`** Critic должен требовать:
      - Наличие связей `PRODUCES → /product/...` (что производит facility)
      - Наличие связей `CONSUMES → /product/...` (что потребляет facility)
      - Если products отсутствуют — добавлять в `missingInfo` вопросы с требованием canonical `/product/...` paths
    - Для **extractive `/fac/`** (mining: `/Materials/Mining/*`, O&G: `/Energy/Fossil_Fuels/Oil_Gas_*`):
      - Требовать reserves/production метрики **по каждому продукту отдельно** (не агрегированно)
      - Naming convention: `reserves_proven_{product_slug}`, `production_{product_slug}`
      - Если reserves известны только на уровне deposit → требовать `/deposit/` с `EXTRACTS_FROM` связью
    - Текущий gap: Critic спрашивает "what products" только при specialty=products_and_services, и не требует создания `/product/` фрагментов
    - Обновить `critic_agent.go`:
      ```
      - If entity is FACILITY (canonicalPath starts with /fac/):
        - MANDATORY: If no PRODUCES relations with /product/... targets exist, add:
          "What products does this facility produce? Return as /product/{category}/{type}/{stage}/{slug} canonical paths."
        - MANDATORY: If no CONSUMES relations with /product/... targets exist, add:
          "What inputs/materials does this facility consume? Return as /product/... canonical paths."
        - If facility is in extractive sector (mining/O&G based on sectorPath):
          - For each product in PRODUCES: require reserves and production metrics
          - If reserves are at deposit level, require /deposit/ and EXTRACTS_FROM relation
      ```
  - [ ] **Critic: обязательные products/services для ORGANIZATION** (независимо от specialty — это базовая информация):
    - **ВАЖНО**: Это требование применяется **всегда**, не только при `specialty=products_and_services`.
    - **Текущий gap в коде**: `critic_agent.go` (строки 151-155) требует products только если запрошен `specialty=products_and_services`. Это неправильно — products/services нужны для понимания бизнеса **любой** operating company.
    - Для **любого `/org/`** Critic должен определить:
      1. Это **операционная компания** (производит products/services сама)?
      2. Или это **холдинг/головная компания** (производит через дочерние общества)?
    - **Логика определения холдинга**:
      - Если есть `OWNS` или `SUBSIDIARY_OF` (inverse) edges → вероятно холдинг
      - Если `sectorPath` = `/Financials/Capital_Markets/*` или `/Organization/Holding` → холдинг
      - Если `entityPath` содержит "holding", "investment" → холдинг
      - Если нет subsidiaries и не холдинг → операционная компания
    - **Для операционной компании** (`/org/` без subsidiaries):
      - MANDATORY: Требовать связи `PRODUCES → /product/...` или `PRODUCES → /product/service/...`
      - Если products/services отсутствуют — добавлять в `missingInfo`:
        - "What products does <org> manufacture/produce? Return as /product/{category}/{type}/{stage}/{slug} canonical paths."
        - "What services does <org> provide? Return as /product/service/{domain}/{subdomain}/{slug} canonical paths."
    - **Для холдинга/головной компании** (`/org/` с subsidiaries):
      - MANDATORY: **Сначала** требовать subsidiaries через `OWNS` или обратные `SUBSIDIARY_OF`
      - Добавлять в `missingInfo`:
        - "What are the operating subsidiaries of <org>? Return as /org/{iso2}/{slug} canonical paths with OWNS relations and ownership_percent."
      - **Затем** для каждой subsidiary требовать products/services (рекурсивно)
      - Холдинг сам НЕ обязан иметь PRODUCES — products привязаны к subsidiaries
    - **Примеры по секторам** (products/services нужны для ВСЕХ операционных компаний):
      | Сектор | Пример компании | Ожидаемые PRODUCES |
      |--------|-----------------|-------------------|
      | **Technology/IT** | Salesforce | `/product/service/it/software/crm-saas`, `/product/service/it/cloud/platform` |
      | **Technology/IT** | NVIDIA | `/product/component/electronic/manufacturing/gpu`, `/product/service/it/software/cuda` |
      | **Consumer/Retail** | Nike | `/product/finished/consumer/manufacturing/footwear`, `/product/finished/consumer/manufacturing/apparel` |
      | **Healthcare/Pharma** | Pfizer | `/product/finished/healthcare/manufacturing/vaccine`, `/product/finished/healthcare/manufacturing/drug` |
      | **Industrial/Manufacturing** | Caterpillar | `/product/finished/mechanical/manufacturing/excavator`, `/product/finished/mechanical/manufacturing/bulldozer` |
      | **Financial/Banking** | JPMorgan | `/product/service/financial/banking/commercial-lending`, `/product/service/financial/investment/asset-management` |
      | **Energy/Utilities** | NextEra | `/product/raw/energy/generation/electricity`, `/product/service/energy/distribution/grid-services` |
      | **Telecom** | Verizon | `/product/service/telecom/mobile/wireless-service`, `/product/service/telecom/internet/broadband` |
      | **Real Estate** | CBRE | `/product/service/real-estate/brokerage/commercial-brokerage`, `/product/service/consulting/operations/property-management` |
      | **Extractive** (Mining) | BHP | `/product/raw/mineral/extraction/iron-ore`, `/product/raw/mineral/extraction/copper-ore` + reserves/production metrics |
      | **Extractive** (O&G) | ExxonMobil | `/product/raw/hydrocarbon/extraction/crude-oil`, `/product/finished/fuel/refining/gasoline` + reserves/production metrics |
      | **Holding** | Berkshire Hathaway | Нет PRODUCES (холдинг) → требовать OWNS → subsidiaries (GEICO, BNSF, etc.) → на них products |
    - **Порядок проверки Critic**:
      ```
      1. If entity is ORGANIZATION (canonicalPath starts with /org/):
         a. Check if entity has OWNS edges to other /org/ (subsidiaries)
         b. If YES (holding company):
            - If subsidiaries list is incomplete or missing → add to missingInfo:
              "What operating subsidiaries does <org> own? Include ownership percentage."
            - Do NOT require PRODUCES for the holding itself
            - Flag subsidiaries for separate scouting with products/services requirement
         c. If NO (operating company):
            - MANDATORY (ALWAYS, not just for products_and_services specialty):
              If no PRODUCES relations exist → add to missingInfo:
              "What products does <org> produce?" and/or
              "What services does <org> provide?"
            - Require canonical /product/... or /product/service/... paths
      2. For FACILITY (/fac/): always require PRODUCES (as before)
      ```
    - **Связь с `specialty=products_and_services`**:
      - Базовые products/services нужны **всегда** (даже без specialty)
      - Если `products_and_services` запрошен — требовать **дополнительную детализацию**:
        - TRANSFORMS_INTO relations для продуктов
        - CONSUMES relations (inputs/materials)
        - Capacity/volume metrics на edges
    - **Связь с equity specialty**:
      - Если запрошена specialty=equity → усиленные требования к ownership structure
      - Требовать полную цепочку: ultimate parent → intermediate holdings → operating companies
    - **Чеклист**:
      - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → **УБРАТЬ** условие `if specialty == products_and_services` для базового PRODUCES check
      - [ ] `critic_agent.go` → добавить логику holding vs operating (всегда, не только при specialty)
      - [ ] `critic_agent.go` → для `products_and_services` specialty добавить **дополнительные** требования (TRANSFORMS_INTO, CONSUMES, metrics)
      - [ ] Определить эвристику `isHoldingCompany()` (по OWNS edges, sectorPath, entityPath keywords)
      - [ ] Для холдингов: сначала subsidiaries, потом products на subsidiaries
      - [ ] Для операционных: сразу products/services
      - [ ] Unit tests: проверить что IT/Manufacturing/Retail компании без products → missingInfo
  - **MissingInfo → Research Plan (и защита от бесконечных циклов)**
    - **Идея**: `missingInfo[]` — это не просто список вопросов, а **план дозапроса/доскаутинга** (grounded research) для конкретной сущности.
    - **Как работает сейчас (код)**:
      - Critic возвращает `missingInfo[]` (вопросы/поисковые запросы).
      - ScoutEngine **один раз** вызывает ResearchAgent с этим списком, не делая циклов внутри одного job.
      - Важно: после research мы **не перегоняем** Critic заново в рамках того же job (иначе легко получить loop).
    - **Где есть защита уже сейчас**:
      - **Idempotency**: terminal jobs (`VERIFIED/FAILED`) не переисполняются (Cloud Tasks retries).
      - **Circuit breaker**: ограничение попыток обработки одного и того же `targetCanonicalPath` внутри одного job (защита от alias/dedup oscillation и повторных retries).
      - **Freshness threshold**: если фрагмент недавно верифицирован и нет новых фактов — ScoutEngine пропускает дорогой scouting/research (для Lens-driven jobs).
      - **Budget caps**: дневные лимиты на USD и output tokens (lens/cascade), чтобы не сжечь бюджет даже при повторных попытках.
    - **Чего НЕ хватает (для полного отсутствия бесконечных циклов между jobs)**:
      - Если `missingInfo` не удалось закрыть (данных нет в источниках), Critic на следующем job может снова запросить то же самое.
      - Нужен слой **“exhaustion/cooldown”** для конкретных missingInfo-вопросов.
    - **Требование (добавить)**: Per-entity “missingInfo exhaustion”
      - Храним попытки по ключу `(tenantId, canonicalPath, missingInfoHash)` в отдельной коллекции:
        - `horizon_missing_info_status/{tenantId}:{canonicalPath}:{hash}`
        - поля: `attempts`, `lastAttemptAt`, `status=open|resolved|exhausted`, `cooldownUntil`, `lastSourcesFingerprint`
      - Правила:
        - Один и тот же missingInfo query **не пытаться** чаще 1 раза в N часов (cooldown), пока не появятся новые факты/источники.
        - Если attempts ≥ K и “новых источников нет” → `status=exhausted` на M дней.
        - Если появились новые источники/факты (fingerprint изменился) → сбрасываем cooldown и пробуем снова.
      - **Stop-conditions** (чётко):
        - MAX attempts per (canonicalPath, query) = K
        - cooldown = N hours between attempts
        - hard TTL для “exhausted” = M days, после чего можно попробовать снова (на случай появления новых данных).
    - **Что показывать UI/аналитике**:
      - “Unresolved missingInfo” как backlog по узлу (что не нашли и почему: exhausted/cooldown).
      - Это позволит пользователю/Watchdog целенаправленно добавить источники или смириться с unknown.
    - **Чеклист изменений**:
      - [ ] `backend/internal/horizon/scout/engine.go` → перед вызовом ResearchAgent проверять `horizon_missing_info_status` и фильтровать exhausted/cooldown вопросы
      - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → вопросы формулировать **детерминированно** (чтобы хеш стабильно совпадал)
      - [ ] Firestore: новая коллекция `horizon_missing_info_status` + индексы (tenantId, canonicalPath, status)
      - [ ] Telemetry: логировать “attempted/blocked_by_cooldown/exhausted”
      - [ ] UI: показывать unresolved missingInfo backlog
  - [ ] **Canonical path inference** — обновить:
    - `backend/internal/horizon/scout/taxonomy.go` → `InferTaxonomyFromCanonicalPath` для `/deposit/`
  - [ ] **Документация** — обновить:
    - `docs/horizon/architecture/taxonomy.md` — добавить секцию про `/deposit/`
    - `docs/horizon/architecture/data-model.md` — добавить DEPOSIT fragment type
  - [ ] **Firestore re-seed** — после изменений:
    - Выполнить `go run ./cmd/tools/horizon_seed_taxonomy --project <project> --version v1`
  - [ ] **Type & Edge Normalizers** — код приведения типов/рёбер к единообразию:
    - **`backend/internal/horizon/scout/type_normalizer.go`**:
      - `pathTypeMap` (строки ~108-120) — маппит canonical path prefixes к FragmentType
      - **Нужно добавить**: `/deposit/` → `FragmentTypeDeposit`
      - Код:
        ```go
        pathTypeMap := map[string]models.FragmentType{
            "/loc/":         models.FragmentTypeGeoRegion,
            "/geo/":         models.FragmentTypeGeoRegion,
            "/org/":         models.FragmentTypeOrganization,
            "/fac/":         models.FragmentTypeFacility,
            "/deposit/":     models.FragmentTypeDeposit,  // NEW
            "/product/":     models.FragmentTypeProduct,
            "/program/":     models.FragmentTypeProgram,
            "/market/":      models.FragmentTypeMarket,
            "/event/":       models.FragmentTypeEvent,
            "/person/":      models.FragmentTypePerson,
            "/media/":       models.FragmentTypeMediaChannel,
            "/supply-link/": models.FragmentTypeSupplyLink,
        }
        ```
    - **`backend/internal/horizon/scout/edge_normalizer.go`**:
      - `EdgeDirectionPolicies` — определяет валидные source/target FragmentTypes для каждого edge type
      - **Нужно добавить**:
        1. `EXTRACTS_FROM` — новый edge type:
           ```go
           models.DependencyTypeExtractsFrom: {
               ValidSourceTypes: []models.FragmentType{models.FragmentTypeFacility},
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeDeposit},
               Description:      "Facility extracts from deposit",
           }
           ```
        2. Обновить `OWNS` — добавить `FragmentTypeDeposit` в ValidTargetTypes:
           ```go
           models.DependencyTypeOwns: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypePerson,
               },
               ValidTargetTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypeFacility,
                   models.FragmentTypeProduct,
                   models.FragmentTypeDeposit,  // NEW
               },
           }
           ```
        3. Обновить `PRODUCES` — добавить `FragmentTypeDeposit` в ValidSourceTypes (deposit produces resources):
           ```go
           models.DependencyTypeProduces: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeOrganization,
                   models.FragmentTypeFacility,
                   models.FragmentTypeDeposit,  // NEW - deposits produce resources
               },
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
           }
           ```
        4. Обновить `LOCATED_IN` — добавить `FragmentTypeDeposit` в ValidSourceTypes:
           ```go
           models.DependencyTypeLocatedIn: {
               ValidSourceTypes: []models.FragmentType{
                   models.FragmentTypeFacility,
                   models.FragmentTypeOrganization,
                   models.FragmentTypePerson,
                   models.FragmentTypeGeoRegion,
                   models.FragmentTypeDeposit,  // NEW
               },
               ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
           }
           ```
        5. Обновить `OPERATES_IN` — добавить `FragmentTypeDeposit` в ValidTargetTypes:
           ```go
           models.DependencyTypeOperatesIn: {
               ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
               ValidTargetTypes: []models.FragmentType{
                   models.FragmentTypeGeoRegion,
                   models.FragmentTypeFacility,
                   models.FragmentTypeDeposit,  // NEW
               },
           }
           ```
    - **`backend/internal/taxonomy/runtime.go`**:
      - `NormalizeRelationType()` — нормализует relation types к канону
      - **Нужно добавить**: `EXTRACTS_FROM` в switch/map алиасов
      - `NormalizeProductName()` — нормализует product names
      - **Нужно обновить**: для services формат `/product/service/{domain}/{subdomain}/{slug}`
    - **`backend/internal/horizon/policy/canonization_policy.go`**:
      - Политики канонизации путей
      - **Проверить**: корректность для `/deposit/` и `/product/service/...`
    - **Чеклист для normalizers**:
      - [ ] `type_normalizer.go` → добавить `/deposit/` в `pathTypeMap`
      - [ ] `edge_normalizer.go` → добавить `EXTRACTS_FROM` в `EdgeDirectionPolicies`
      - [ ] `edge_normalizer.go` → обновить `OWNS`, `PRODUCES`, `LOCATED_IN`, `OPERATES_IN` для `DEPOSIT`
      - [ ] `runtime.go` → добавить `EXTRACTS_FROM` в `NormalizeRelationType()`
      - [ ] `runtime.go` → обновить `NormalizeProductName()` для services
      - [ ] `canonization_policy.go` → проверить/обновить для новых путей
- **Lifecycle Stages для HorizonFragment (стадии строительства и жизненного цикла)**
  - **Проблема**: Не все `/fac/`, `/deposit/`, `/org/` существуют и работают. Некоторые строятся, планируются, законсервированы или закрыты. Текущая модель не различает "завод работает" от "завод строится".
  - **Решение**: Добавить поля lifecycle в `HorizonFragment`:
    ```go
    type HorizonFragment struct {
        // ... existing fields ...
        
        // Lifecycle — стадия жизненного цикла объекта в реальном мире
        LifecycleStage    string     `json:"lifecycleStage,omitempty" firestore:"lifecycleStage,omitempty"`
        ExpectedStartDate *time.Time `json:"expectedStartDate,omitempty" firestore:"expectedStartDate,omitempty"`
        ActualStartDate   *time.Time `json:"actualStartDate,omitempty" firestore:"actualStartDate,omitempty"`
        ExpectedEndDate   *time.Time `json:"expectedEndDate,omitempty" firestore:"expectedEndDate,omitempty"`
        ActualEndDate     *time.Time `json:"actualEndDate,omitempty" firestore:"actualEndDate,omitempty"`
    }
    ```
  - **Семантика полей**:
    - `LifecycleStage` — текущая стадия объекта (canonical value из словаря)
    - `ExpectedStartDate` — когда объект **планируется** ввести в эксплуатацию (для construction)
    - `ActualStartDate` — когда объект **фактически** начал работу
    - `ExpectedEndDate` — когда объект **планируется** закрыть (decommissioning)
    - `ActualEndDate` — когда объект **фактически** закрылся
  - **Отличие от существующих полей**:
    - `DependencyEdge.ValidFrom/ValidTo` — про **реальный срок действия** связи/факта (effective time: когда связь началась/закончилась в мире)
    - `MetricValue.ObservedAt` + `Provenance` — про **эпистемику** (когда мы наблюдали/узнали и из какого источника)
    - `Governance.Status` — про состояние **в нашей системе** (ACTIVE/DORMANT/ARCHIVED)
    - `LifecycleStage` — про состояние **объекта в реальном мире** (строится/работает/закрыт)
  - **Canonical lifecycle stages** (единый словарь для всех типов):
    ```
    AllowedLifecycleStages: []string{
        // Ранние стадии
        "concept",           // раннее планирование, pre-feasibility
        "planning",          // детальное планирование, feasibility study
        "permitting",        // получение разрешений, licensing
        "financing",         // привлечение финансирования
        
        // Строительство
        "construction",      // строительство, development drilling
        "commissioning",     // пуско-наладка, ramp-up
        
        // Эксплуатация
        "operational",       // в эксплуатации (default для работающих объектов)
        "expansion",         // расширение (работает + строится дополнительная мощность)
        
        // Остановка
        "suspended",         // временно остановлен, care & maintenance, shut-in
        
        // Закрытие
        "decommissioning",   // вывод из эксплуатации, abandonment
        "closed",            // закрыт, dissolved, depleted, demolished
    }
    ```
  - **Маппинг по типам фрагментов**:
    | Stage | FACILITY | DEPOSIT | ORGANIZATION |
    |-------|----------|---------|--------------|
    | `concept` | Pre-feasibility | Early exploration | Pre-incorporation |
    | `planning` | Feasibility study | Appraisal | Business planning |
    | `permitting` | Getting permits | Licensing | Registration |
    | `financing` | Securing funding | — | Fundraising |
    | `construction` | Building | Development drilling | — |
    | `commissioning` | Ramp-up | First production | Launch |
    | `operational` | Running ✓ | Production ✓ | Operating ✓ |
    | `expansion` | Adding capacity | Infill drilling | Scaling |
    | `suspended` | Idle / C&M | Shut-in | Dormant |
    | `decommissioning` | Closing down | Abandonment | Liquidation |
    | `closed` | Demolished | Depleted | Dissolved |
  - **Industry-specific aliases** (для удобства маппятся в canonical):
    - **O&G**: `exploration` → `concept`, `appraisal` → `planning`, `development` → `construction`, `production` → `operational`, `shut_in` → `suspended`
    - **Mining**: `pre_feasibility` → `concept`, `feasibility` → `planning`, `development` → `construction`
    - **ORG**: `startup` → `commissioning`, `restructuring` → `suspended`, `liquidation` → `decommissioning`, `dissolved` → `closed`
  - **Примеры использования**:
    ```json
    {
      "canonicalPath": "/fac/tesla/monterrey-gigafactory",
      "type": "FACILITY",
      "lifecycleStage": "construction",
      "expectedStartDate": "2026-03-01T00:00:00Z",
      "labels": {"en": "Tesla Gigafactory Monterrey"}
    }
    ```
    ```json
    {
      "canonicalPath": "/deposit/lithium/thacker-pass",
      "type": "DEPOSIT",
      "lifecycleStage": "permitting",
      "expectedStartDate": "2027-01-01T00:00:00Z",
      "labels": {"en": "Thacker Pass Lithium Project"}
    }
    ```
  - **Влияние на supply chain**:
    - Объект в `construction` не может быть **текущим** supplier/customer
    - Edges к таким объектам должны иметь `Qualifiers["edgeModality"] = "projected"` (чтобы не путать с эпистемической modality: observed/reported/predicted)
    - Propagation engine учитывает lifecycleStage при расчёте весов:
      - `operational` → нормальный вес
      - `construction` → сниженный вес или отдельный "projected" signal
      - `closed` → не участвует в propagation
  - **Scout/Critic интеграция**:
    - Scout должен извлекать lifecycleStage и dates из источников
    - Critic должен требовать lifecycleStage для:
      - Всех `/fac/` — обязательно
      - Всех `/deposit/` — обязательно
      - `/org/` в extractive sectors — обязательно
    - Если lifecycleStage == "construction" или "planning", требовать expectedStartDate
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/fragment.go` → добавить поля `LifecycleStage`, `ExpectedStartDate`, `ActualStartDate`, `ExpectedEndDate`, `ActualEndDate`
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedLifecycleStages []string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedLifecycleStages`
    - [ ] `backend/internal/taxonomy/runtime.go` → добавить `AllowedLifecycleStages`, `NormalizeLifecycleStage()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать lifecycle info
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать lifecycle для /fac/, /deposit/
    - [ ] Propagation engine → учитывать lifecycleStage при расчёте весов
    - [ ] `docs/horizon/architecture/taxonomy.md` → документировать lifecycle stages
- **Edge Metrics (метрики на рёбрах: %, суммы, объёмы)**
  - **Проблема**: Текущие edges имеют только абстрактные `EdgeProperties` (Criticality, Amplification, LatencyHours). Нет конкретных метрик:
    - OWNS → на сколько %?
    - INVESTS_IN → сколько вложил? каким раундом?
    - SUPPLIES_TO → какой объём? на какую сумму?
  - **Решение**: Добавить `Metrics map[string]MetricValue` в `DependencyEdge`:
    ```go
    type DependencyEdge struct {
        // ... existing fields ...
        
        // Edge-specific metrics — numerical data with units (like fragment State)
        Metrics map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
    }
    ```
  - **Переиспользуем существующий MetricValue**:
    - `Value` (float64) — значение
    - `Unit` (*string) — единица измерения
    - `Confidence` (float64) — уверенность
    - `ObservedAt` (time.Time) — дата наблюдения
    - `Provenance` — источник данных
    - `ErrorMargin` — интервал ошибки
  - **Разделение Metrics vs Qualifiers**:
    - `Qualifiers` (существующий) — categorical/string данные (`round: "series_b"`, `product: "/product/..."`)
    - `Metrics` (новый) — numerical данные с unit/confidence/provenance
  - **Canonical metric names по типам edges**:
    | Edge Type | Metric Name | Unit | Описание |
    |-----------|-------------|------|----------|
    | `OWNS` | `ownership_percent` | `percent` | Доля владения |
    | `OWNS` | `voting_percent` | `percent` | Доля голосов (если отличается) |
    | `SUBSIDIARY_OF` | `ownership_percent` | `percent` | Доля владения родителем |
    | `INVESTS_IN` | `investment_amount` | currency | Сумма инвестиции |
    | `INVESTS_IN` | `round_size_total` | currency | Общий размер раунда |
    | `SUPPLIES_TO` | `annual_volume` | varies | Годовой объём поставок |
    | `SUPPLIES_TO` | `contract_value` | currency | Стоимость контракта |
    | `SUPPLIES_TO` | `contract_duration_years` | `years` | Срок контракта |
    | `PRODUCES` | `annual_capacity` | varies | Годовая мощность |
    | `PRODUCES` | `annual_output` | varies | Фактический выпуск |
    | `CONSUMES` | `annual_consumption` | varies | Годовое потребление |
    | `EXTRACTS_FROM` | `annual_extraction` | varies | Годовая добыча |
    | `EMPLOYED_BY` | `tenure_years` | `years` | Стаж работы |
    | `EMPLOYED_BY` | `compensation` | currency | Компенсация |
    | `BOARD_MEMBER_OF` | `tenure_years` | `years` | Срок в совете |
    | `OPERATES_IN` | `market_share` | `percent` | Доля рынка |
    | `COMPETES_WITH` | `relative_market_share` | `ratio` | Относительная доля |
  - **Canonical qualifiers по типам edges** (categorical, в Qualifiers):
    | Edge Type | Qualifier | Values |
    |-----------|-----------|--------|
    | `INVESTS_IN` | `round` | `seed`, `series_a`, `series_b`, `series_c`, `series_d`, `growth`, `ipo`, `secondary` |
    | `INVESTS_IN` | `roundDate` | ISO date string |
    | `SUPPLIES_TO` | `product` | `/product/...` canonical path |
    | `PRODUCES` | `product` | `/product/...` canonical path |
    | `CONSUMES` | `product` | `/product/...` canonical path |
    | `EXTRACTS_FROM` | `product` | `/product/...` canonical path |
    | `EMPLOYED_BY` | `role` | `ceo`, `cfo`, `coo`, `cto`, etc. |
    | `EMPLOYED_BY` | `startDate` | ISO date string |
    | `BOARD_MEMBER_OF` | `committees` | comma-separated list |
    | `REGULATES` | `regulationType` | `licensing`, `environmental`, `safety`, etc. |
  - **Примеры использования**:
    ```json
    {
      "type": "OWNS",
      "sourceFragmentId": "/org/sa/aramco",
      "targetFragmentId": "/org/kr/s-oil",
      "metrics": {
        "ownership_percent": {
          "value": 63.4,
          "unit": "percent",
          "confidence": 0.99,
          "observedAt": "2024-01-01T00:00:00Z"
        }
      },
      "properties": {"criticality": 0.9}
    }
    ```
    ```json
    {
      "type": "INVESTS_IN",
      "sourceFragmentId": "/org/us/sequoia",
      "targetFragmentId": "/org/us/stripe",
      "metrics": {
        "investment_amount": {"value": 500000000, "unit": "usd", "confidence": 0.9}
      },
      "qualifiers": {
        "round": "series_h",
        "roundDate": "2023-03-15"
      }
    }
    ```
    ```json
    {
      "type": "SUPPLIES_TO",
      "sourceFragmentId": "/org/cl/sqm",
      "targetFragmentId": "/org/cn/catl",
      "metrics": {
        "annual_volume": {"value": 50000, "unit": "metric_tons_per_year"},
        "contract_value": {"value": 1500000000, "unit": "usd"},
        "contract_duration_years": {"value": 5, "unit": "years"}
      },
      "qualifiers": {
        "product": "/product/raw/chemical/refining/lithium-carbonate"
      }
    }
    ```
  - **Связь Metrics → Criticality**:
    - `EdgeProperties.Criticality` может быть **derived** из Metrics
    - Например: OWNS с ownership_percent > 50% → criticality = 0.9
    - Или: SUPPLIES_TO с contract_value > $1B → criticality = 0.8
    - Это делает граф самодокументируемым: criticality имеет обоснование
  - **AllowedMetricUnits — расширить для edges**:
    ```go
    // Добавить к существующим
    "percent",           // ownership, market share
    "years",             // tenure, contract duration
    "count",             // committees, employees
    "ratio",             // relative values
    // Currency / index units:
    // ВАЖНО: валюты и финансовые индексы НЕ ограничиваем allowlist'ом.
    // Unit может быть любым тикером/кодом (например: "usd", "eur", "usdt", "inr", "rub", "cny",
    // "brent", "wti", "cpi_ru", "moex", "sp500") — чтобы не ломать санкционные/теневые рынки.
    //
    // Имплементация (runtime validation):
    // - Для currency/index единиц разрешаем pattern, а не точный allowlist.
    // - Минимальный контракт: lowercase, [a-z0-9_], длина 2..32.
    ```
  - **Scout/Critic интеграция**:
    - Scout должен извлекать edge metrics из источников
    - Critic должен требовать metrics для:
      - `OWNS` → обязательно `ownership_percent`
      - `INVESTS_IN` → желательно `investment_amount`, обязательно `round` qualifier
      - `SUPPLIES_TO` → желательно `annual_volume` или `contract_value`
    - Если metrics отсутствуют → добавлять в `missingInfo` вопросы
  - **Связь с Events**:
    - Investment rounds = Events (`/event/stripe-series-h-2023`)
    - Edge `INVESTS_IN` может иметь `Qualifiers["eventId"]` → ссылка на Event fragment
    - Event содержит полные детали: организаторы, все участники, условия
    - Базовые metrics живут на edge, детали — в Event
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/dependency_edge.go` → добавить `Metrics map[string]MetricValue`
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedEdgeMetrics map[string][]string` (edge type → allowed metrics)
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedEdgeMetrics`
    - [ ] `backend/internal/taxonomy/runtime.go` → `AllowedEdgeMetrics`, `ValidateEdgeMetrics()`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → расширить `AllowedMetricUnits` (`percent`, `years`, `count`, `ratio`)
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать edge metrics
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать metrics для OWNS, INVESTS_IN, SUPPLIES_TO
    - [ ] `docs/horizon/architecture/data-model.md` → документировать edge metrics
- **Edge Temporal Validity (срок действия связей)**
  - **Проблема**: Связи могут иметь ограниченный срок действия:
    - Инвестировал в прошлом, но **уже вышел**
    - Работал в прошлом (**бывший сотрудник**)
    - Руководил в прошлом (**экс-CEO**)
    - Был членом совета директоров
    - Поставлял в прошлом, но контракт завершён
  - **Существующие поля** (уже есть в `DependencyEdge`):
    ```go
    ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
    ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`
    ```
  - **Семантика**:
    - `ValidFrom` — когда связь **начала** действовать (дата найма, инвестиции, контракта)
    - `ValidTo` — когда связь **прекратила** действовать (увольнение, выход из инвестиции, конец контракта)
    - `ValidTo == nil` → связь **активна** на текущий момент
    - `ValidTo != nil && ValidTo < now` → связь **историческая**
  - **ВАЖНО: Исторические связи НЕ удаляются**:
    - Хранят ценную информацию о прошлых связях
    - Нужны для анализа карьерных путей, инвестиционной истории
    - Нужны для compliance (кто был связан с sanctioned entity)
    - Просто помечаются как неактивные через `ValidTo`
  - **Методы на DependencyEdge**:
    ```go
    // IsActive проверяет активна ли связь на заданный момент времени
    func (e *DependencyEdge) IsActive(at time.Time) bool {
        if e.ValidFrom != nil && at.Before(*e.ValidFrom) {
            return false // связь ещё не началась
        }
        if e.ValidTo != nil && at.After(*e.ValidTo) {
            return false // связь уже завершилась
        }
        return true
    }
    
    // IsActiveNow проверяет активна ли связь сейчас
    func (e *DependencyEdge) IsActiveNow() bool {
        return e.IsActive(time.Now())
    }
    
    // IsHistorical проверяет завершилась ли связь
    func (e *DependencyEdge) IsHistorical() bool {
        return e.ValidTo != nil && time.Now().After(*e.ValidTo)
    }
    ```
  - **Edge types где ValidTo особенно важен**:
    | Edge Type | Пример завершения | Как заполнять |
    |-----------|-------------------|---------------|
    | `EMPLOYED_BY` | Увольнение, уход | ValidTo = дата ухода |
    | `BOARD_MEMBER_OF` | Выход из совета | ValidTo = дата выхода |
    | `MANAGES` | Смена руководства | ValidTo = дата смены |
    | `OWNS` | Продажа доли | ValidTo = дата продажи |
    | `INVESTS_IN` | Выход из инвестиции (exit) | ValidTo = дата exit |
    | `SUPPLIES_TO` | Завершение контракта | ValidTo = дата окончания |
    | `AUDITED_BY` | Смена аудитора | ValidTo = дата смены |
    | `CONTROLS` | Потеря контроля | ValidTo = дата потери |
  - **Влияние на Propagation**:
    - **Активные связи** (`IsActiveNow() == true`) — участвуют в propagation полноценно
      - (т.е. propagation фильтрует edges по окну `ValidFrom/ValidTo`, а не только по `edgeModality`)
    - **Исторические связи** (`IsHistorical() == true`) — **не участвуют** в real-time propagation
    - Но исторические связи **участвуют** в:
      - Timeline analysis (как развивались связи)
      - Compliance checks (был ли связан с sanctioned entity в прошлом)
      - Career path analysis
    - **Будущие связи** (`ValidFrom > now`) — не участвуют в propagation, но видны как "projected"
    - **Правило выставления `edgeModality` на основе дат** (для единообразия UI/аналитики):
      - если `ValidFrom != nil && ValidFrom > now` → `edgeModality = "projected"`
      - если `ValidTo != nil && ValidTo < now` → `edgeModality = "historical"`
      - иначе → `edgeModality = "actual"`
  - **Влияние на риски (Risk Model)**:
    - Исторические связи с sanctioned entities = **lower but non-zero risk**
    - "Бывший сотрудник sanctioned company" — всё равно red flag
    - Модель рисков должна учитывать `ValidTo` при расчёте risk score
  - **Scout/Critic интеграция**:
    - Scout должен извлекать temporal info:
      - "worked at X from 2015 to 2020" → ValidFrom = 2015-01-01, ValidTo = 2020-12-31
      - "former CEO" → ValidTo = past date (если известна)
      - "joined in 2021" → ValidFrom = 2021-01-01, ValidTo = nil
    - Critic должен:
      - Если source содержит "former", "ex-", "бывший" → требовать ValidTo
      - Для `EMPLOYED_BY`, `BOARD_MEMBER_OF`, `MANAGES` → желательно ValidFrom
  - **Qualifiers для temporal context** (дополнительно к ValidFrom/ValidTo):
    - `Qualifiers["startReason"]`: "hired", "elected", "acquired", "contracted"
    - `Qualifiers["endReason"]`: "resigned", "fired", "retired", "expired", "sold", "exited"
    - `Qualifiers["tenure"]`: "2015-2020", "5 years" (human-readable summary)
  - **JSON пример — исторический EMPLOYED_BY**:
    ```json
    {
      "type": "EMPLOYED_BY",
      "sourceFragmentId": "/person/us/satya-nadella",
      "targetFragmentId": "/org/us/sun-microsystems",
      "validFrom": "1992-01-01T00:00:00Z",
      "validTo": "1992-12-31T00:00:00Z",
      "qualifiers": {
        "role": "Intern",
        "endReason": "contract_ended"
      }
    }
    ```
  - **JSON пример — активный OWNS**:
    ```json
    {
      "type": "OWNS",
      "sourceFragmentId": "/org/us/softbank-vision-fund",
      "targetFragmentId": "/org/us/wework",
      "validFrom": "2017-08-01T00:00:00Z",
      "validTo": null,
      "metrics": {
        "ownership_percent": {"value": 29.5, "unit": "percent"}
      }
    }
    ```
  - **JSON пример — exit из INVESTS_IN**:
    ```json
    {
      "type": "INVESTS_IN",
      "sourceFragmentId": "/org/us/sequoia-capital",
      "targetFragmentId": "/org/us/airbnb",
      "validFrom": "2009-04-01T00:00:00Z",
      "validTo": "2020-12-10T00:00:00Z",
      "metrics": {
        "investment_amount": {"value": 600000, "unit": "usd"}
      },
      "qualifiers": {
        "round": "seed",
        "endReason": "ipo_exit"
      }
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/horizon/models/dependency_edge.go` → добавить методы `IsActive()`, `IsActiveNow()`, `IsHistorical()`
    - [ ] `backend/internal/horizon/scout/agents/scout_agent.go` → извлекать temporal info из sources ("former", "ex-", "since", "until")
    - [ ] `backend/internal/horizon/scout/agents/critic_agent.go` → требовать ValidTo для "former/ex-" relationships
    - [ ] Propagation engine → фильтровать historical edges из real-time propagation
    - [ ] Risk model (future) → учитывать historical relationships в risk scoring
    - [ ] Query API → параметр `?includeHistorical=true|false` (default: false для active only)
    - [ ] `docs/horizon/architecture/data-model.md` → документировать edge temporal validity
- **Связь Lifecycle Stage → Edge Modality**
  - **Проблема**: Если target fragment в `construction`, edge к нему — это "будущая" связь, не текущая.
  - **Решение**: Автоматически помечать edges к non-operational targets:
    - Если `target.LifecycleStage` не в [`operational`, `expansion`]:
      - Устанавливать `edge.Qualifiers["edgeModality"] = "projected"` (отдельно от эпистемической modality)
      - Или `edge.ValidFrom` = target's `ExpectedStartDate`
    - Propagation engine учитывает `edgeModality`:
      - `edgeModality: "actual"` (default) → полный вес
      - `edgeModality: "projected"` → сниженный вес или отдельный signal type
      - `edgeModality: "historical"` → не участвует в текущем propagation
  - **Canonical edge modalities**:
    ```go
    AllowedEdgeModalities: []string{
        "actual",      // связь существует и активна (default)
        "projected",   // связь планируется (target в construction)
        "historical",  // связь существовала в прошлом
        "potential",   // связь возможна, но не подтверждена
    }
    ```
  - **Примеры**:
    ```json
    {
      "type": "SUPPLIES_TO",
      "sourceFragmentId": "/fac/tesla/monterrey-gigafactory",
      "targetFragmentId": "/org/na/ford",
      "qualifiers": {
        "edgeModality": "projected",
        "product": "/product/component/electronic/manufacturing/battery-cell"
      },
      "validFrom": "2026-03-01T00:00:00Z"
    }
    ```
  - **Чеклист изменений**:
    - [ ] `backend/internal/models/horizon_taxonomy.go` → добавить `AllowedEdgeModalities []string`
    - [ ] `backend/cmd/tools/horizon_seed_taxonomy/main.go` → заполнить `AllowedEdgeModalities`
    - [ ] `backend/internal/horizon/scout/engine.go` → при создании edge проверять target.LifecycleStage и устанавливать modality
    - [ ] Propagation engine → учитывать modality при расчёте весов
- **Обработка метрик при propagation (семантика поверх "доставки дельт")**
  - Ввести registry/factory **именованных метрик**.
  - Для части метрик применять **детерминированные handler’ы** (algorithmic rules сначала): stateful / categorical / gated.
  - Волна propagation должна вызывать handler’ы для “именованных” метрик, чтобы изменения могли влиять на:
    - derived fields/flags,
    - дискретные статусы/режимы,
    - вторичные характеристики сущности,
    - (опционально) политику применения/порогов.
  - Требования: **идемпотентность**, **трассировка (explain/trace)**, **версионирование логики** (чтобы воспроизводить состояние).

---
### Конец большого раздела работ: расширение онтологии/таксономии Horizon
---
- **Signal handlers: риски / возможности / тренды / направления (семантика поверх доставки)**
  - Ввести единый тип “signal” (kind: risk|opportunity|trend|direction) с **каноническим type** (runtime taxonomy: allowed + synonyms).
  - Provenance считаем “вынесенным”: сигнал должен ссылаться на доказательную базу через онтологические ноды/факты (например, `ontology_facts`, `ontology_events`, `ontology_claims`, `ontology_sources`), а не тащить “простыни” ссылок внутрь Horizon.
  - Определить минимальный контракт времени/модальности для сигналов (использовать то, что запланировано выше):
    - `occurredAt` / `observedAt` для происхождения сигнала,
    - `effectiveFrom/effectiveTo` для “окна действия”,
    - `modality` (observed / reported / predicted / hypothetical), чтобы различать факт и сценарий.
  - Определить **identity/dedup ключ** сигнала (например: `(kind, type, subjectCanonicalPath, scope{sphere/location/actor}, timeWindow, modality, sourceRef)`), чтобы propagation не размножал почти-одинаковые записи.
  - Определить **lifecycle** сигнала: create/update/expire/archive + политика “когда сигнал считается устаревшим/неверным” (в т.ч. для predicted).
  - Ввести **uncertainty/conflict** поверх сигналов:
    - явная конфликтность (два сигналa противоречат по одному ключу),
    - хранение confidence + (опционально) интервала/распределения,
    - политика разрешения (voting / trust-weighted / keep-both-with-caveat).
  - Продумать **агрегацию/rollup**: как из множества сигналов делать “сводный” по узлу/сектору/лине (max/worst-of/decay/hysteresis) и что является сущностью результата (например, `SignalState`/`SignalRollup`).
  - В propagation вызывать **видовые handler’ы** по `(kind, type)`:
    - правила затухания/усиления по типу ребра,
    - агрегация/дедуп (чтобы сигнал не размножался бесконтрольно),
    - (опционально) трансформации сигналов (например, risk санкций → risk supply disruption).
  - Добавить/учесть **causality vs correlation**: причинность — отдельный артефакт/ссылка (не выводить “причину” автоматически из совместной динамики).
  - Явно предусмотреть **feedback loops / derived signals**: propagation может порождать производные сигналы — нужно разделение raw vs derived + защита от циклов (см. секцию C).
  - Вынести **propagation policies как данные** (таблично): какие `relationType`/ребра пропускают какие `kind/type`, на какую глубину и с каким весом (минимизировать деплои кода при расширении словаря).
  - Зафиксировать **контракт на explainability**: любой сигнал/rollup обязан уметь ответить “почему” (trace: входы, правила, веса, time/modality окна, источники).
  - Требования: **детерминированность**, **идемпотентность**, **explain/trace**, **версионирование правил**, и политика “как predicted влияет на state”.
- **Интеграция “базовой онтологии” с Horizon под единым смысловым зонтиком**
  - Зафиксировать “зонтик” терминологии: Horizon = мониторинг графа + подписки/линзы + Vertex-индексация; Ontology = источник нормализованных артефактов (entities/events/claims/metrics/trends/scenarios/causal/uncertainty).
  - Определить маппинг **онтологических артефактов → Horizon**:
    - `ontology_facts` как унифицированный поток “фактов/сигналов” для Watchdog/Scout (incremental, watermark),
    - `Trend`, `Scenario`, `CausalLink`, `UncertaintyModel`, `Concept(kind=risk/opportunity/...)` — как first-class носители сигналов, привязанные через `canonicalPaths`/`actorPaths`/`locationPath`/`spherePaths`.
  - Утвердить единые канонические поля связи:
    - `canonicalPaths` (куда “прикрепляется” сигнал в Horizon),
    - `locationPath` (`/loc/...`), `actorPaths`, `spherePaths` (для фильтрации/QueryAddress и propagation policies).
  - Описать стратегию миграции “strict core first” и Horizon‑grade контракт для онтологии: см. [Ontology → Horizon migration](../architecture/ontology-migration.md).
  - Спланировать интеграцию в UX/API:
    - “сводка сигналов” по `HorizonFragment.canonicalPath` (raw + rollups + explain),
    - единая выдача поиска: fragments + ontology_facts (или отдельные вертикали) под один сценарий.
  - Спланировать совместную индексацию в Vertex AI Search:
    - Horizon fragments (уже есть),
    - ontology_facts (уже есть),
    - определить, как tenant/scope разделяются и как объединяются в выдаче.
- **Мульти‑классификация без взрыва словаря**
  - При необходимости: "primary sector/entity + tags" или аккуратные `[]string` поля, но без попытки сделать дерево бесконечным.
- **Intent/Keywords Resolution (Lens-driven)**
  - Реализовать механизм инкрементального резолва keywords в canonical paths и sphere paths по мере работы агентов.
  - Keywords должны жить на Lens (в `ViewConfig.keywords`), передаваться в `ScoutingJob.State`, и по мере нахождения сущностей агентами — резолвиться обратно в Lens (`resolvedKeywords` mapping).
  - `QueryAddress` должен поддерживать `keyword:` префикс для динамического резолва (например, `/query/(canonicalPath:/org/tesla AND keyword:batteries AND sphere:/sphere/logistics)`).
  - Гарантировать, что сущности, относящиеся к сфере (через `spherePaths` в facts), получают связи со сферой в графе (sphere fragments и `INFLUENCES` edges).
  - **Expose NL precheck in backend API (Hook-ready)**
    - `POST /api/horizon/intent/resolve` (NL query → taxonomy-aligned intent: anchors + spherePaths + keywords + focus).
    - `POST /api/horizon/lenses/from-query` (resolve intent → create Lens with scope=anchors and viewConfig steering).
    - Подключить Horizon Hook UX к этим endpoint'ам (в идеале: typed draft preview перед созданием Lens).

**Критерии приемки**:
- Можно отвечать на вопросы “что было верно на дату X” (минимально для edges/facts).
- Факты различаются по модальности и не приводят к неконтролируемым обновлениям state.
- Улучшения не ломают текущие canonical paths и QueryAddress.

### C) Масштабирование и “графовая” корректность (после стабилизации)

**Цель**: сделать поведение Watchdog/Scout/Propagation устойчивым на больших графах и в сложных случаях (циклы, условные зависимости, батчинг).

**TODO**:
- **Propagation: защита от циклов (важно)**
  - Добавить `visited`/дедуп в рамках одной волны по ключу `(fragmentId, metricName)` и не рекурсировать повторно.
  - Добавить лимиты: `maxDepth`, `maxNodesUpdated`, `timeBudget` (fail-safe).
- **Batching: аккумулировать вызовы Scout и изменения**
  - Очередь/батчинг для ScoutingJob (группировка/дедуп), чтобы один факт не раздувал десятки вызовов.
  - Батч-обработка обновлений в Propagation Engine и (по возможности) batch writes в Firestore.
- **LLM-free эвристики корректности и “анти-взрыва” графа (graph shaping without LLM)**
  - **Зачем**: уменьшить “мусор” в графе и экспоненциальный рост ScoutingJobs/propagation **без** дополнительных LLM вызовов; сделать поведение предсказуемым (bounded worst-case).
  - `canonicalPath → fragmentType` как **hard rule** (path-based type inference).
    - Зачем: детерминированная нормализация типов (`/org/*`, `/loc/*`, `/fac/*`, `/sphere/*`), устраняет класс ошибок агентов, упрощает direction/rules.
  - Soft-валидация `relationType × (sourceType,targetType)` как **penalty/ранжирование** (не hard reject) + **audit** для грубых нарушений.
    - Зачем: отсекать явно бессмысленные комбинации типов, но не ломать “реальный мир” (конгломераты/нетипичные связи).
  - Depth-decay: затухание `effectiveConfidence/effectivePriority` с глубиной + пороги/бюджеты на создание child jobs.
    - Зачем: главный анти-экспоненциальный механизм даже при `maxDepth=2` (мягко режет хвосты, где больше шума).
  - Hub-aware expansion: **не блокировать** хабы, а ограничивать раскрытие по relevance к `Lens.viewConfig.focus/spherePaths` + бюджетам.
    - Зачем: хабы (страны/мегакорпы) важны, но раскрывать “целиком” нельзя — иначе граф превращается в энциклопедию.
  - Negative evidence cache (short TTL, soft signal): если уже пытались подтвердить/найти и не нашли — понижать приоритет повторов, но не блокировать полностью.
    - Зачем: ускорять повторные/похожие запросы и снижать стоимость на “мы это уже проверяли”, не кешируя “навсегда”.
  - Anomaly/audit сигналы (без авто-фильтра): fan-in/fan-out дисбаланс, self-edges, транзитивный шум `LOCATED_IN`, конфликтные/цикличные отношения.
    - Зачем: безопасно повышать наблюдаемость качества и облегчать дебаг без риска false negatives.
  - **Acceptance**:
    - Worst-case по созданию child jobs должен быть предсказуем и ограничен бюджетами/порогами.
    - Не добавляем новые LLM вызовы (правила должны быть детерминированными/конфигурируемыми).
    - Алерты/audit должны быть трассируемыми: “почему это помечено/понижено”.
- **Watchdog: “соседние пути” в графе**
  - Реагировать не только на прямой canonicalPath, но и на соседние узлы по ребрам (глубина 1–2) при обработке релевантных фактов.
  - Кэш/снапшот графа зависимостей для удешевления обхода.
- **Conditional dependencies (условные зависимости)**
  - Возможность задавать условия/контекст ребра (когда зависимость “включена”), чтобы волны и watchdog не применяли изменения “вслепую”.
- **Relevance / distance heuristics**
  - Механизм "расстояния/релевантности" между запросом/фактом и фрагментами (например, учитывать географический контекст), чтобы выборка не касалась всего пула игроков.

### D) Unified Policy Architecture (высокий приоритет)

**Цель**: выделить класс задач "принятие решений" в унифицированный артефакт **Policy**, который содержит:
- **(a) LLM fallback**: инструменты для случаев, когда детерминированные методы пока не покрывают
- **(b) Deterministic rules**: эвристики и алгоритмы, удерживающие строгость таксономии и графа

**Зачем**:
- **Телеметрия**: видеть ratio (a)/(b), latency, cost по каждой Policy в реальном времени
- **Эволюция**: начинаем с высоким (a), смотрим паттерны в LLM решениях, "кристаллизуем" в правила, (b) растёт, costs↓
- **Качество**: правила проверены на реальных данных, не теряем quality при снижении LLM usage
- **Testability**: детерминированная часть покрыта unit-тестами, LLM — smoke/integration

**Policies**:

| Policy | Зона ответственности |
|--------|---------------------|
| `PropagationPolicy` | Как метрики текут по рёбрам (weight, damping, direction, critical path boost) |
| `ScoutPolicy` | Когда скаутить, какие источники, глубина, freshness thresholds, budget limits |
| `CanonizationPolicy` | Нормализация paths, types, names, synonyms resolution |
| `SynthesisPolicy` | Fact → Signal rules (когда детерминированно, когда LLM) |
| `ValidationPolicy` | Какие комбинации (source×target×relationType) валидны, soft penalties, hard rejects |
| `PrioritizationPolicy` | Critical paths, hub handling, relevance scoring |
| `AggregationPolicy` | Как агрегировать метрики (max, sum_capped, decay) |

**Unified Contract**:
```go
type Policy interface {
    Name() string
    Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error)
    Explain(decision PolicyDecision) string
}

type PolicyDecision struct {
    Action       string                 // "propagate", "skip", "normalize_to", etc.
    Params       map[string]interface{}
    DecisionPath string                 // "rule:exact_match" | "rule:pattern" | "llm:fallback"
    RuleID       *string
    Confidence   float64
    LatencyMs    int
    LLMTokens    *int
}
```

**TODO**:
- [x] Определить `Policy` interface и `PolicyDecision` struct (`policy/policy.go`)
- [x] Реализовать `PolicyExecutor` с телеметрией (rule-first, LLM-fallback) (`policy/policy.go`)
- [x] Создать Firestore collections для правил каждой Policy (`horizon_propagation_policies`, `horizon_canonization_rules`, `horizon_validation_rules`, `horizon_synthesis_rules`, `horizon_metric_specs`)
- [x] Реализовать базовые Policies: `PropagationPolicy`, `CanonizationPolicy`, `ValidationPolicy` (`policy/*.go`)
- [x] Интегрировать телеметрию Firestore (`horizon_policy_telemetry`) (`policy/telemetry.go`)
- [x] Интегрировать `PolicyExecutor` в `PropagationEngine` hot path (`cmd/server/main.go`)
- [x] Посеять минимальные propagation policies (7 правил: risk × SUPPLIES_TO/DEPENDS_ON/OWNS/COMPETES_WITH/LOCATED_IN/INFLUENCES/PRODUCES)
- [ ] **Dashboard для Policy Telemetry** (приоритет: средний, см. ниже)

**Dashboard для Policy Telemetry (отдельный TODO)**:
- [ ] BigQuery pipeline: экспорт `horizon_policy_telemetry` → BigQuery (batch или Pub/Sub → streaming)
- [ ] Looker Studio / Data Studio дашборд:
  - Ratio rule-based vs LLM-based решений по policy
  - Top LLM fallback reasons (DecisionPath анализ)
  - Cost per policy (tokens × pricing)
  - Latency distribution (p50/p95/p99) по policy
- [ ] Альтернатива (lightweight): простой Firestore-based UI в админке hatl.one для просмотра последних N решений и агрегированных метрик

**Acceptance**:
- Все Horizon-службы используют Policy interface для принятия решений
- Телеметрия показывает % rule-based vs LLM-based решений
- Можно добавить новое правило в Firestore без редеплоя и увидеть изменение ratio

---

### E) Semantic Propagation & Signals (risk/opportunity/trend/direction)

**Цель**: создать структурный слой для ответов на вопросы типа "What are the top supply disruption risks for Tesla battery logistics?" — из графа и фактов, а не через LLM каждый раз.

**Детальный план**: см. [signals-and-semantic-propagation-plan.md](signals-and-semantic-propagation-plan.md)

**TODO (критические пути)**:

- **Critical Paths в Propagation (высокий приоритет)**
  - **Проблема**: сейчас затухание "тупое" — все рёбра равны, а некоторые ветки распространения важнее других.
  - **Решение**:
    - Определить **critical path** как путь с высокой `criticality` на рёбрах, между anchor-узлами Lens'а, или явно помеченный.
    - При создании/обновлении Lens вычислять critical paths (BFS/Dijkstra по criticality) и сохранять в `lens.ViewConfig["criticalPathEdges"]`.
    - В PropagationEngine: **меньше затухания** на critical paths (`dampingFactor *= 1.2`), **больше глубина** (`maxDepth += 1`), **приоритет** в hot path.
  - **Acceptance**: critical paths вычисляются при создании Lens; propagation через critical path затухает медленнее на 20%+.

- **Propagation Policies как данные (высокий приоритет)**
  - **Проблема**: сейчас propagation "тупой" — гоняет числовые дельты одинаково по любому ребру. Логистический риск должен течь сильно по `SUPPLIES_TO`, слабо по `COMPETES_WITH`.
  - **Решение**:
    - Firestore collection `horizon_propagation_policies` с матрицей `(metricPattern × relationType) → behavior` (passes, weight, maxDepth, dampingOverride, direction, criticalPathBoost).
    - `PolicyMatcher` выбирает policy по приоритету (exact match > pattern > wildcard).
    - PropagationEngine использует policy для каждой комбинации metric × edge.
  - **Acceptance**: можно задать "risk_supply_* проходит по SUPPLIES_TO с weight=1.2, по COMPETES_WITH с weight=0.3" без изменения кода.

- **Metric Registry & Handlers (средний приоритет)**
  - **Проблема**: нет контракта на aggregation/decay для метрик; все метрики обрабатываются одинаково.
  - **Решение**:
    - Firestore collection `horizon_metric_specs` с `MetricSpec` (name, kind, aggregationFunc, decayHalfLife, propagationThreshold).
    - `MetricHandler` interface: `Aggregate(inputs) → MetricValue`, `Decay(value, now) → MetricValue`.
    - Built-in handlers: `MaxHandler`, `SumCappedHandler`, `WeightedAvgHandler`, `ExponentialDecayHandler`.
  - **Acceptance**: risk-метрики агрегируются как "max", opportunity-метрики как "sum_capped".

- **Signal Types в Runtime Taxonomy (средний приоритет)**
  - **Проблема**: нет структурного слоя для risk/opportunity/trend/direction — всё "размазано" по фактам.
  - **Решение**:
    - Расширить `horizon_taxonomy/v1`: `allowedSignalKinds`, `signalTypes` (с label, synonyms, defaultMetric, propagationProfile).
    - `OntologyFact(kind=signal, type=risk:supply_disruption)` — не новая коллекция, а расширение существующей.
    - `NormalizeSignalType()` в `taxonomy/runtime.go`.
  - **Acceptance**: можно создать факт `kind=signal, type=risk:supply_disruption` и он валидируется runtime taxonomy.

- **Signal Synthesis (Fact → Signal) (средний приоритет)**
  - **Проблема**: каждый факт требует LLM для классификации как сигнал.
  - **Решение**:
    - Firestore collection `horizon_synthesis_rules` с детерминированными правилами `(factKind, factTypePattern, spherePattern) → (outputSignalType, metricName, confidenceMultiplier)`.
    - `SignalSynthesizer` применяет правила без LLM.
    - Интеграция в Scout/Watchdog: при обработке факта автоматически синтезируются сигналы.
  - **Acceptance**: 80%+ фактов покрыты synthesis rules и не требуют LLM для классификации.

- **Async LLM Queue для сложных Propagation-решений (низкий приоритет)**
  - **Когда**: классификация неоднозначна, conflict resolution, causal inference для explain trace.
  - **Решение**:
    - Отдельная Cloud Tasks queue `sanskrite-propagation-llm` с throttling.
    - `PropagationLLMTask` содержит контекст (fragment, metric, signals, damping state).
    - `PropagationLLMExecutor` подчиняется тем же правилам затухания (depth check, damping factor), использует `min_capability_score=6`.
  - **Acceptance**: LLM вызывается только async, подчиняется damping, не блокирует hot path.

- **Rollup & Explain (после Signal Synthesis)**
  - `GetFragmentRollup(fragmentID, metricKind)` возвращает агрегированный score + explain trace.
  - `ExplainTrace` содержит top contributors, propagation path, rules applied.
  - **Acceptance**: каждый rollup имеет explain trace с топ-3 contributors.

**Критерии приемки (общие)**:
- Rollup computation < 100ms (без LLM).
- Можно ответить на "Top supply disruption risks for Tesla battery logistics" из структурированных данных.
- Propagation policies и synthesis rules управляются через Firestore без редеплоя.

### F) Supply Chain Paths & Influence Links (после стабилизации)

**Цель**: моделировать конкретные цепочки поставок ("кто поставляет что кому") и политические/экономические влияния между акторами.

**Детальная архитектура**: см. [supply-chain-influence-model.md](../architecture/supply-chain-influence-model.md)

**Базовый слой оверлеев (materialized views)**: см. [materialized-artifacts.md](../architecture/materialized-artifacts.md)

**Проблема**: текущий граф имеет абстрактный продуктовый слой (`TRANSFORMS_INTO`) и связи org→product (`PRODUCES`/`CONSUMES`), но нет способа выразить "VW покупает батареи именно у CATL, которые сделаны из лития именно от Albemarle".

**Ключевые концепции**:

- **SupplyLink** — атомарная единица supply chain: `(Supplier, Product, Customer)`
  - Минимальный путь: `/org или /fac` → `/product` → `/org или /fac`
  - Минимум для стыковки: `/product` → `/org` → `*`
  - Tenant-specific (не global) — "по внутренним данным, мы знаем, что покупаем у CATL"
  
- **Influence Link** — политическое/экономическое влияние с qualifier `via`
  - Один объект (газ) может быть и товаром (SupplyLink), и рычагом влияния (InfluenceLink)
  - Петли допустимы (взаимозависимость: Russia→gas→Germany, Germany→€€€→Russia)

**TODO**:

- **Phase 0: MaterializedArtifact (базовый тип для слоёв/оверлеев)**
  - [x] Ввести `MaterializedArtifact` как отдельную коллекцию/тип (tenant/global)
  - [x] Enum `MaterializedKind`, первый kind: `SUPPLY_CHAIN_PATH`
  - [x] Payload v1: упорядоченный массив узлов пути + опциональные "ребра" (supply-link / ontology transitions)
  - [x] Ссылки на метрики/observations + evidence refs (ontology_facts) без смешивания ответственности

- **Phase 1: SupplyLink (высокий приоритет)**
  - [x] Добавить `FragmentTypeSupplyLink`
  - [x] Добавить поля `SupplierRef`, `CustomerRef`, `ProductRef` в HorizonFragment
  - [x] Валидация: SupplyLink требует `tenantId` (может быть tenant или "global" как assumption-layer)
  - [ ] API endpoint для создания SupplyLink
  - [ ] Интеграция с Scout для автоматического обнаружения

- **Phase 2: Path Building**
  - [x] Функция `BuildUpstreamPath(customer, opts)` с confidence propagation
  - [x] Tenant-first, global-fallback precedence на уровне сегмента
  - [x] Интеграция с MaterializedArtifact для сохранения путей
  - [ ] API endpoint для получения upstream/downstream paths
  - [ ] Кэширование построенных путей

- **Phase 3: Influence Links**
  - [x] Стандартизация qualifiers (`via`, `mechanism`, `direction`, `reversible`, `conditionality`, `strength`)
  - [x] Поддержка influence qualifiers в ValidationPolicy
  - [x] Helper functions для ExtractInfluenceQualifiers, SetInfluenceQualifiers, ValidateInfluenceQualifiers
  - [ ] API для запроса influence paths

- **Phase 4: HIL Integration**
  - [ ] Draft SupplyLink workflow (LLM inference → HIL verification)
  - [ ] UI для подтверждения/корректировки
  - [ ] Confidence upgrade при верификации

**Критерии приемки**:
- Можно построить путь "BMW ← CATL ← Albemarle" из tenant-specific SupplyLinks
- Confidence propagation работает через цепочку
- Influence edges с `via` qualifier валидируются и визуализируются

## Этап 1: Модели данных и репозитории

**Цель**: Создание базовых моделей данных и репозиториев для работы с Firestore

**Задачи**:
1. Создание моделей данных:
   - `HorizonFragment` (`backend/internal/horizon/models/fragment.go`)
   - `Frame` (`backend/internal/horizon/models/frame.go`)
   - `Lens` (`backend/internal/horizon/models/lens.go`)
   - `DependencyEdge` (`backend/internal/horizon/models/dependency_edge.go`)
   - `WatchdogConfig` (`backend/internal/horizon/models/watchdog.go`)

2. Создание репозиториев:
   - `HorizonFragmentRepository` (`backend/internal/repo/horizon_fragments.go`)
   - `FrameRepository` (`backend/internal/repo/frames.go`)
   - `LensRepository` (`backend/internal/repo/lenses.go`)
   - `DependencyEdgeRepository` (`backend/internal/repo/dependency_edges.go`)
   - `WatchdogConfigRepository` (`backend/internal/repo/watchdog_configs.go`)

3. Создание Firestore индексов:
   - Обновление `backend/firestore.indexes.json`
   - Создание составных индексов для частых запросов

4. Миграции данных (если требуется):
   - Скрипты для миграции существующих данных
   - Валидация данных после миграции

**Критерии приемки**:
- ✅ Все модели данных соответствуют схемам из документации
- ✅ Репозитории реализуют CRUD операции
- ✅ Индексы созданы и протестированы
- ✅ Unit тесты для репозиториев написаны и проходят

**Оценка**: 2-3 недели

## Этап 2: Scout Engine (базовый)

**Цель**: Реализация базового функционала Scout Engine для сбора и верификации данных

**Задачи**:
1. Создание агентов:
   - `ScoutAgent` (`backend/internal/horizon/scout/agents/scout_agent.go`)
   - `CriticAgent` (`backend/internal/horizon/scout/agents/critic_agent.go`)
   - `ResolverAgent` (`backend/internal/horizon/scout/agents/resolver_agent.go`)

2. Создание основного движка:
   - `ScoutEngine` (`backend/internal/horizon/scout/engine.go`)
   - Планирование источников (`backend/internal/horizon/scout/planner.go`)
   - Выполнение запросов (`backend/internal/horizon/scout/executor.go`)

3. Интеграция с AI системой:
   - Использование `AITaskExecutor` для LLM вызовов
   - Constrained decoding для генерации JSON
   - Валидация схем через Pydantic (или Go эквивалент)

4. Workflow состояний:
   - Реализация конечного автомата состояний
   - Интеграция с `AgentJob` системой
   - Обработка ошибок и повторные попытки

5. Создание Frame:
   - Генерация Frame документов
   - Сохранение evidence и validation meta
   - Связь Frame с HorizonFragment

**Критерии приемки**:
- ✅ Scout Agent извлекает данные в правильном формате
- ✅ Critic Agent обнаруживает несоответствия
- ✅ Resolver Agent принимает решения
- ✅ Workflow проходит все состояния успешно
- ✅ Frame создаются с полной метаинформацией

**Оценка**: 4-5 недель

## Этап 3: Lens Subsystem

**Цель**: Реализация подсистемы управления Lens и подписками на HorizonFragment

**Задачи**:
1. Создание сервиса Lens:
   - `LensService` (`backend/internal/horizon/lens/service.go`)
   - Управление подписками (`backend/internal/horizon/lens/subscription.go`)
   - Пересчет метрик (`backend/internal/horizon/lens/calculator.go`)

2. Управление жизненным циклом:
   - Автоматическое управление `activeSubscribers`
   - Переходы между состояниями (ACTIVE → DORMANT → ARCHIVED)
   - Реактивация узлов при подписке

3. HTTP handlers:
   - `HorizonHandler` (`backend/internal/http/horizon/handlers.go`)
   - Endpoints для управления Lens
   - Валидация запросов

4. Интеграция с Project:
   - Связь Lens с Project
   - Контроль доступа через Project
   - Изоляция данных

**Критерии приемки**:
- ✅ Создание, обновление, удаление Lens работает
- ✅ Подписки на HorizonFragment управляются автоматически
- ✅ Жизненный цикл узлов работает корректно
- ✅ Контроль доступа через Project реализован

**Оценка**: 3-4 недели

## Этап 4: Propagation Engine

**Цель**: Реализация алгоритма распространения изменений в графе зависимостей

**Задачи**:
1. Создание Propagation Engine:
   - `PropagationEngine` (`backend/internal/horizon/propagation/engine.go`)
   - Алгоритм затухания волны (`backend/internal/horizon/propagation/wave_damping.go`)
   - Пороги значимости (`backend/internal/horizon/propagation/threshold.go`)
   - Ленивые вычисления (`backend/internal/horizon/propagation/lazy_eval.go`)

2. Топологическая оптимизация:
   - Поиск критических путей
   - Приоритетная обработка узлов
   - Оптимизация порядка пересчета

3. Интеграция с Realtime:
   - Публикация событий через Pub/Sub
   - WebSocket обновления для фронтенда
   - События распространения

4. Оптимизации производительности:
   - Батчинг обновлений
   - Кэширование графа
   - Параллельная обработка

**Критерии приемки**:
- ✅ Пороги значимости работают корректно
- ✅ Затухание волны предотвращает экспоненциальный рост
- ✅ Hot Path и Cold Path работают как ожидается
- ✅ Производительность соответствует требованиям (≤ 5 сек для Hot Path)

**Оценка**: 4-5 недель

## Этап 5: Watchdog

**Цель**: Реализация фонового мониторинга источников и автоматического обновления данных

**Задачи**:
1. Создание Watchdog Service:
   - `WatchdogService` (`backend/internal/horizon/watchdog/service.go`)
   - Мониторинг источников (`backend/internal/horizon/watchdog/monitor.go`)
   - Планирование проверок (`backend/internal/horizon/watchdog/scheduler.go`)

2. Интеграция с Cloud Functions:
   - Cloud Function для периодических задач
   - Cloud Scheduler для планирования
   - Обработка триггеров

3. Обнаружение изменений:
   - Вычисление хешей контента
   - Сравнение с предыдущими версиями
   - Определение порога изменений

4. Управление жизненным циклом:
   - Автоматический запуск для ACTIVE узлов
   - Автоматическая остановка для DORMANT узлов
   - Управление статусом (RUNNING, PAUSED)

5. Интеграция с Scout Engine:
   - Создание ScoutRequest при обнаружении изменений
   - Обработка результатов разведки
   - Обновление Watchdog конфигурации

**Критерии приемки**:
- ✅ Периодические проверки источников работают
- ✅ Изменения обнаруживаются корректно
- ✅ ScoutRequest создаются при превышении порога
- ✅ Жизненный цикл управляется автоматически

**Оценка**: 3-4 недели

## Этап 6: Графовая БД (отложено)

**Цель**: Миграция на специализированную графовую базу данных для сложных графовых запросов

**Статус**: Отложено до необходимости

**Причины отложения**:
- Firestore с индексами достаточен для начального этапа

## Intent / Taxonomy: Dynamic Spheres (Lens-driven)

**Требование**: поддержать “динамические сферы” из intent (LLM / precheck) — когда система сама предлагает/нормализует `spherePaths` под запрос, без ручного выбора пользователем.

**Зачем**: базовые запросы часто не мапятся на заранее известную сферу; сейчас всё падает в `/sphere/other`, что ухудшает переиспользуемость и качество графа/UX.

**Acceptance criteria**:
- `spherePaths` формируются из intent и **валидируются/нормализуются runtime taxonomy** (`/sphere/...` allowlist + synonyms).
- Если сферы нет в allowlist — система либо:
  - выбирает ближайшую существующую сферу (через synonyms/иерархию), либо
  - создаёт новую canonical `spherePath` через админ‑workflow и добавляет её в runtime taxonomy (желательно с synonyms).
- Для выбранных `spherePaths` автоматически создаётся sphere-fragment и `INFLUENCES` edge (**sphere → target**) с provenance `lens_intent_sphere_link`.
- Результат должен быть **переиспользуем**: похожий следующий запрос должен приходить к тем же `spherePaths` (при неизменной runtime taxonomy).
- Сложность миграции и синхронизации данных
- Дополнительные затраты на инфраструктуру

**Будущие задачи**:
1. Выбор графовой БД (Neo4j или Amazon Neptune)
2. Проектирование схемы миграции
3. Реализация синхронизации Firestore ↔ Graph DB
4. Миграция существующих данных
5. Обновление Propagation Engine для использования графовой БД

**Оценка**: 6-8 недель (когда будет необходимо)

## Интеграционное тестирование

После каждого этапа необходимо проводить интеграционное тестирование:

1. **Unit тесты**: Для каждого компонента
2. **Integration тесты**: Для взаимодействия компонентов
3. **End-to-End тесты**: Для полных сценариев использования
4. **Performance тесты**: Для проверки производительности

## Развертывание

### Этап 1-2: Внутреннее тестирование
- Развертывание в тестовой среде
- Тестирование базового функционала
- Исправление критических ошибок

### Этап 3-4: Бета-тестирование
- Развертывание для ограниченной группы пользователей
- Сбор обратной связи
- Оптимизация производительности

### Этап 5: Продакшн
- Полное развертывание
- Мониторинг и метрики
- Поддержка и исправление ошибок

## Приоритизация

**Высокий приоритет** (MVP):
- Этап 1: Модели данных и репозитории
- Этап 2: Scout Engine (базовый)
- Этап 3: Lens Subsystem (базовый)

**Средний приоритет**:
- Этап 4: Propagation Engine
- Этап 5: Watchdog

**Низкий приоритет**:
- Этап 6: Графовая БД

## Риски и митигация

### Риск 1: Производительность Propagation Engine

**Митигация**:
- Раннее прототипирование алгоритмов
- Performance тесты на больших графах
- Оптимизация и кэширование

### Риск 2: Стоимость LLM вызовов

**Митигация**:
- Оптимизация промптов для уменьшения токенов
- Кэширование результатов
- Использование более дешевых моделей где возможно

### Риск 3: Сложность интеграции с существующей системой

**Митигация**:
- Постепенная интеграция по этапам
- Тщательное тестирование на каждом этапе
- Документирование изменений

## Метрики успеха

### Технические метрики:
- Время выполнения Scout (Snapshot ≤ 1 мин, DeepDive ≤ 30 мин)
- Время пересчета Lens (Hot Path ≤ 5 сек)
- Точность извлечения данных (≥ 98%)
- Доступность системы (≥ 99.9%)

### Бизнес-метрики:
- Количество созданных HorizonFragment
- Количество активных Lens
- Средняя стоимость разведки
- Удовлетворенность пользователей

## См. также

- [Технические решения](technical-decisions.md)
- [Архитектурный обзор](../architecture/overview.md)
- [Supply Chain & Influence Model](../architecture/supply-chain-influence-model.md)

