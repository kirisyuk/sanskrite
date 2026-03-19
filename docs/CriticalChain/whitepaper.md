# CriticalChain — Whitepaper v3.0

> Мониторинг цепочек поставок критических материалов
> Версия 3.0 | Март 2026 | Canonical Document

**Статус:** CriticalChain — эксперимент-подсистема. Разворачивается отдельно внутри того же GCP проекта, рядом с ядром (Horizon). Использует таксономию и возможности ядра, но в ядро не интегрируется. Совместимость с ядром сохраняется для возможного слияния в будущем.

---

## Содержание

1. [Executive Summary](#1-executive-summary)
2. [Анализ домена](#2-анализ-домена)
3. [Архитектура решения](#3-архитектура-решения)
4. [UX и визуализация](#4-ux-и-визуализация)
5. [Риски продукта](#5-риски-продукта)
6. [Roadmap](#6-roadmap)
7. [Принципы универсальной архитектуры](#7-принципы-универсальной-архитектуры)
8. [Интеграция с ядром](#8-интеграция-с-ядром)
9. [Приложение А: Источники данных](#приложение-а-источники-данных)
10. [Приложение Б: Технические зависимости](#приложение-б-технические-зависимости)

---

## 1. Executive Summary

Мир вступил в эру «войн за материалы». Редкоземельные элементы, литий, кобальт, полупроводниковый кремний, гелий, нефть и газ — десятки критических материалов определяют, кто производит электромобили, МРТ-сканеры, ракеты и смартфоны. Сбои в этих цепочках — от геополитических конфликтов до стихийных бедствий — обходятся мировой экономике в сотни миллиардов долларов ежегодно.

**CriticalChain** — профессиональный веб-инструмент аналитики и мониторинга цепочек поставок критических материалов, их многоуровневых деривативов и связанных рисков.

### Ключевые характеристики

- Граф цепочек поставок с многоуровневыми переделами (материал → полуфабрикат → конечный продукт)
- Интерактивная карта на MapLibre: маршруты, объекты, риск-зоны
- AI-анализ рисков через Vertex AI / Gemini с Search Grounding
- Обновление данных каждые 6 часов из открытых источников
- Явная визуализация рисков: геополитика, санкции, конфликты, природные угрозы
- Risk score по единой шкале **1–5** (везде в системе)
- Стек: GCP / Go / React / TypeScript / Vite / MapLibre

### Целевые клиенты

**Первичный рынок:**
- Консалтинговые компании (McKinsey, BCG, Kearney и аналоги) — стратегический анализ цепочек для клиентов
- КазМунайГаз и аналогичные национальные энергетические компании — мониторинг энергетической безопасности

**Расширенный рынок:**
- Strategic Intelligence команды Fortune 500 (automotive, aerospace, pharma, defence)
- Государственные агентства по критическим материалам
- Commodity trading firms и хедж-фонды
- ESG/sustainability исследовательские организации

### Рыночная дыра

Существующие платформы (Everstream, Resilinc, Interos) фокусируются на supplier risk management для корпораций, но не дают аналитикам и государственным структурам инструмент для исследования **полных цепочек критических материалов с переделами**, с акцентом на геополитику и open data.

CriticalChain занимает уникальную нишу: critical materials + multi-tier transformation chains + open data + geopolitical analysis + map visualization. Ни один из конкурентов не закрывает эту комбинацию.

---

## 2. Анализ домена

### 2.1 Supply Chain Criticality

Критичность материала определяется двумя осями:
1. **Экономическая важность:** незаменимость в ключевых технологиях и отраслях
2. **Риск нарушения поставок:** географическая концентрация, геополитика, монополии

Индексы и списки критических материалов:
- USGS 2025 Critical Minerals List: 60 минералов и материалов
- EU Critical Raw Materials Act 2024: 34 критических + 17 стратегических материалов
- IEA Critical Minerals for Clean Energy Transitions
- DoD Critical Materials List (США, частично открытый)

### 2.2 Материалы MVP v1

**Список материалов первого релиза:**

| Материал | Категория | Ключевые риски |
|---|---|---|
| Литий (Li) | Батарейные металлы | Чили/Австралия → 90% переработки Китай |
| Кобальт (Co) | Батарейные металлы | 70%+ добычи — ДРК; 80%+ рафинирования — Китай |
| Редкоземельные элементы (REE) | Специальные металлы | ~90% добычи и ~85% переработки — Китай |
| Гелий (He) | Специальные газы | США, Катар, РФ; нет субститутов |
| Кремний (Si) | Полупроводниковые материалы | Китай 70% поликремния; TSMC 55% foundry |
| **Нефть (Crude Oil)** | **Энергетические commodities** | **Hormuz, Bab-el-Mandeb, Turkish Straits** |
| **Природный газ (Natural Gas)** | **Энергетические commodities** | **Европейские трубопроводы; СПГ-маршруты** |

### 2.3 Многоуровневые цепочки: примеры

**Цепочка лития (EV)**
```
Уровень 0: Добыча → Литиевая руда / Сподумен / Соляные рапы
  └── Австралия (Greenbushes), Чили (Atacama), Аргентина
Уровень 1: Переработка → Карбонат лития / Гидроксид лития
  └── Китай (~60%), Чили, Австралия
Уровень 2: Производство катодного материала → LFP, NMC, NCA
  └── Китай (~70%), Корея (LG, Samsung), Япония (Panasonic)
Уровень 3: Производство ячеек → Battery cells
  └── CATL (Китай), BYD, LG Energy, Panasonic, Samsung SDI
Уровень 4: Battery pack assembly → Батарейные модули
Уровень 5: Конечный продукт → Электромобили
```

**Цепочка нефти**
```
Уровень 0: Скважина (upstream) — Саудовская Аравия, Россия, США, Казахстан
Уровень 1: Сепарация / стабилизация
Уровень 2: Трубопровод / танкер
  → Узкие места: Hormuz (20% мировой нефти), Bab-el-Mandeb (9%), Суэц
Уровень 3: НПЗ (refining)
Уровень 4: Нефтепродукты (diesel, jet fuel, naphtha, химсырьё)
Уровень 5: Конечный потребитель
```

**Цепочка природного газа**
```
Уровень 0: Скважина / месторождение
Уровень 1: Подготовка газа (сепарация, осушка)
Уровень 2: Pipeline / СПГ-завод
Уровень 3: Регазификационный терминал
Уровень 4: Распределительная сеть (ENTSO-G monitoring)
Уровень 5: Конечный потребитель (удобрения, электроэнергия, отопление)
```

### 2.4 Источники данных: оценка

Полная таблица — в [Приложении А](#приложение-а-источники-данных).

**Принципы выбора источников:**
- ACLED исключён по экономическим соображениям (~$10K/год). Заменяется многоуровневой системой косвенных сигналов + Gemini Search Grounding.
- GDELT включён (Public Domain) как источник event detection; требует ML-фильтрации шума.
- Для нефти и газа приоритетные источники: EIA API, IEA free datasets, ENTSO-G, JODI.

**Политика приоритетов при конфликте источников (Conflict Resolution Policy):**

| Тип данных | Приоритет источников |
|---|---|
| Координаты объектов | USGS MRDS > OSM Overpass > Natural Earth |
| Нефтяные объёмы и статистика | EIA > IEA > JODI > OPEC |
| Газовые потоки (Европа) | ENTSO-G > IEA > EIA |
| Торговые потоки (критические минералы) | UN Comtrade+ > BACI (CEPII) > OECD TiVA |
| Страновой риск | US State Dept + FCDO + AU DFAT (консенсус ≥2 из 3) |
| Санкции | OFAC SDN + EU Consolidated List (оба обязательны) |
| Canonical ID организаций | GLEIF LEI (единственный авторитетный источник) |
| Морская обстановка | NGA Maritime Safety / NAVTEX > aisstream.io |

При конфликте приоритет определяется порядком в таблице. Если оба источника присутствуют — берётся данные из источника с более высоким приоритетом; значение из второго источника логируется как `alternative_value` для аудита.

### 2.5 Конкуренты и дыры в рынке

| Платформа | Фокус | Дыра |
|---|---|---|
| Everstream Analytics | AI-driven supply chain risk для логистики | Нет critical minerals специфики; нет граф переделов |
| Resilinc | Multi-tier supplier mapping | Нет geographic visualization; нет open data |
| Interos | AI risk intelligence, third-party mapping | Нет critical minerals; нет карты маршрутов |
| Kpler | Commodity flows (нефть, СПГ) | Только энергетика; нет переделов; нет risk scoring |
| Windward | Maritime AI, sanctions compliance | Только maritime; нет supply chain graph |

**Вывод:** CriticalChain занимает уникальную нишу — единственный продукт с critical materials специфика + multi-tier chains + open data + geopolitical analysis + map visualization.

---

## 3. Архитектура решения

### 3.1 Общая архитектура (GCP-native)

```
┌─────────────────────────────────────────────────────────────┐
│                    CLIENT LAYER                              │
│  React + TypeScript + Vite + MapLibre + Zustand/TanStack    │
│  Web App (SPA) — только веб, без мобильного приложения     │
└────────────────┬────────────────────────────────────────────┘
                 │ HTTPS
┌────────────────▼────────────────────────────────────────────┐
│                    API GATEWAY LAYER                         │
│  Cloud Load Balancer → Cloud Run (Go API)                   │
│  REST API v1 | /api/v1/criticalchain/...                    │
│  Auth: Firebase Auth / Identity Platform (общий с Horizon) │
└────────────────┬────────────────────────────────────────────┘
                 │
    ┌────────────┼────────────────┐
    │            │                │
┌───▼───┐  ┌────▼────┐  ┌────────▼────────┐
│ Graph │  │  Geo    │  │  Analytics      │
│Firestore│ │Firestore│  │  (BigQuery)     │
│(MVP)   │  │GeoPoint │  │  Trade flows    │
│Supply │  │(MVP)    │  │  Time series    │
│Chains │  │Routes   │  │  Aggregates     │
└───────┘  └─────────┘  └────────┬────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────┐
│                    DATA PIPELINE LAYER                       │
│  Cloud Scheduler (каждые 6 часов)                           │
│    → Cloud Pub/Sub                                          │
│      → Cloud Run Workers (Go)                               │
│        → Source-specific adapters (plugin pattern)          │
└─────────────────────────────────┬───────────────────────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────┐
│                    AI / ML LAYER                             │
│  Vertex AI (Gemini Pro)                                     │
│  ├── Risk Scoring Pipeline (batch, 6h)                      │
│  ├── Search Grounding (real-time queries)                   │
│  ├── Event Summarization (GDELT + news)                     │
│  └── Supply Chain Q&A (RAG over graph)                      │
└─────────────────────────────────────────────────────────────┘
```

**Внешние источники (6-часовой цикл):**
```
Gemini Search Grounding → Signal Aggregator
USGS MRDS API          → Minerals & Facilities Adapter
EIA API                → Oil & Gas Adapter
ENTSO-G API            → European Gas Flows Adapter
JODI Data              → Global Oil/Gas Statistics Adapter
UN Comtrade+ API       → Trade Flows Adapter
GDELT BigQuery         → Events Adapter
OFAC SDN XML           → Sanctions Adapter
EU Consolidated List   → Sanctions Adapter
GLEIF API              → Entity Canonical ID Adapter
OSM Overpass           → Geo Enrichment Adapter
Natural Earth          → Route Geometries Adapter
State Dept RSS         → Country Risk Adapter
FCDO GOV.UK API        → Country Risk Adapter
AU DFAT RSS            → Country Risk Adapter
NGA Maritime Safety    → Maritime Warnings Adapter
aisstream.io           → AIS Vessel Monitoring Adapter
FAA NOTAM API          → Aviation Risk Adapter
```

**Развёртывание:** CriticalChain — тот же Cloud Run сервис, что и Horizon; отдельные routes `/api/v1/criticalchain/...`. CI/CD: отдельный ключ `--api-criticalchain` для деплоя.

**Vertex AI:** Тот же GCP проект; отдельный datastore для CriticalChain (facilities, materials, routes) — grounding не пересекается с Horizon.

**AgentMesh:** Не используется. Data pipeline — Cloud Scheduler → Pub/Sub → Cloud Run workers.

### 3.2 Хранилище данных

**MVP: Firestore**
- Граф цепочек: коллекции `criticalchain_nodes`, `criticalchain_edges`
- Гео: Firestore GeoPoint в документах (точечные объекты)
- Маршруты: массив waypoints (LineString хранится как array)
- Префикс `criticalchain_` изолирует от коллекций Horizon

**Phase 2: PostGIS + Spanner Graph**
- Cloud SQL (PostgreSQL + PostGIS): сложные пространственные запросы, polygon intersection
- Cloud Spanner Graph: граф-траверсалы, многоуровневые зависимости
- BigQuery GIS: аналитика пересечений маршрутов с зон конфликтов

### 3.3 Data Model: граф цепочек поставок

**NODES (вершины):**

```go
Material {
  id:              string  // CAS number + HS code composite
  name:            string
  aliases:         string[]
  category:        string  // critical_mineral | energy_commodity | agricultural
  criticalityScore: float  // 1–5
  riskScore:       float   // 1–5, AI-generated
  hsCodes:         string[]
  casNumber:       string
}

Facility {
  id:               string  // GLEIF LEI | OSM ID | USGS ID
  canonicalPath:    string  // /fac/{geo}/{slug}
  name:             string
  facilityKind:     FacilityKind  // из taxonomy: mine, refinery, seaport, ...
  facilityRole:     FacilityRole  // из taxonomy: extraction, refining, ...
  country:          string  // ISO3
  coordinates:      GeoPoint  // WGS84, Firestore
  operators:        string[]  // LEI references
  capacity:         float
  capacityUnit:     string
  productionMaterials: Material[]
  materialId:       string
  stage:            int
  riskScore:        float  // 1–5, AI-generated
  sanctioned:       boolean
}

Route {
  id:           string
  type:         enum(SEA | LAND | AIR | PIPELINE | RAIL)
  waypoints:    GeoPoint[]  // MVP: array; Phase 2: LineString in PostGIS
  fromFacility: Facility.id
  toFacility:   Facility.id
  typicalVolume: float
  volumeUnit:   string
  riskScore:    float  // 1–5
}

ConflictZone {
  id:               string
  name:             string
  boundingBox:      GeoPoint[]  // MVP: bbox; Phase 2: Polygon in PostGIS
  severity:         enum(LOW | MEDIUM | HIGH | CRITICAL)
  type:             enum(ARMED | SANCTIONS | NATURAL | POLITICAL)
  affectedMaterials: Material[]
  source:           string  // FCDO | STATE_DEPT | NAVTEX | ...
  updatedAt:        timestamp
}
```

**EDGES (рёбра):**

```go
ProcessingStep {
  from:                   Material.id | Facility.id
  to:                     Material.id | Facility.id
  type:                   enum(EXTRACTION | REFINING | PROCESSING | MANUFACTURING | TRANSPORT)
  yieldRatio:             float
  keyPlayers:             Facility[]
  geographicConcentration: float  // HHI index
  riskMultiplier:         float
}
```

### 3.4 Data Pipeline: 6-часовой цикл

```
Cloud Scheduler (каждые 6 часов):
  ├── Worker 1: Minerals & Facilities Fetcher
  │   └── USGS MRDS → criticalchain_nodes (facilities, mines)
  │
  ├── Worker 2: Oil & Gas Data Fetcher
  │   └── EIA API → criticalchain_nodes (oil fields, LNG terminals)
  │   └── ENTSO-G API → criticalchain_edges (gas flows Europe)
  │   └── JODI → BigQuery oil_gas_statistics
  │
  ├── Worker 3: Trade Data Fetcher
  │   └── UN Comtrade+ API → BigQuery raw_trades
  │
  ├── Worker 4: Country Risk Fetcher
  │   └── US State Dept RSS → country_risk table
  │   └── FCDO GOV.UK API → country_risk table
  │   └── AU DFAT RSS → country_risk table
  │   (консенсус ≥2 из 3 → risk_level повышается)
  │
  ├── Worker 5: Sanctions Fetcher
  │   └── OFAC SDN XML → entity_sanctions table
  │   └── EU Consolidated List CSV → entity_sanctions table
  │
  ├── Worker 6: Events & Signals Fetcher
  │   └── GDELT BigQuery public dataset → events table
  │   └── NGA Maritime Safety API → maritime_warnings table
  │   └── FAA NOTAM API → aviation_risk table
  │   └── aisstream.io → ais_traffic_baseline (для AIS blackout detection)
  │
  ├── Worker 7: Geo Enrichment
  │   └── OSM Overpass → Facility coordinates update
  │   └── Natural Earth → Route geometries
  │   └── GLEIF API → canonical LEI for organisations
  │
  └── Worker 8: AI Risk Scoring
      └── Vertex AI Gemini → Batch risk scores (шкала 1–5)
          Input: entity data + country risk + events + sanctions + AIS signals
          Output: risk_scores table (BigQuery)
          Cache: Memorystore Redis, TTL 6 часов
```

**Signal Fusion (агрегация сигналов):**
```json
{
  "region": "Red Sea",
  "risk_level": 4,
  "confidence": "medium",
  "signals": [
    "AIS blackout detected (aisstream.io)",
    "FCDO raised to Level 3",
    "NGA NAVTEX warning active"
  ],
  "freshness": "2026-03-19T06:00Z",
  "source_count": 3
}
```

Правило: если ≥2 независимых источника подтверждают угрозу → risk_level повышается. Один источник → флаг `unconfirmed`.

**Source Adapter Pattern:**
```go
type DataSourceAdapter interface {
  Name() string
  Fetch(ctx context.Context, since time.Time) ([]RawRecord, error)
  Normalize(raw RawRecord) (CanonicalRecord, error)
  Schema() SourceSchema
  RateLimit() RateLimitConfig
}
```

### 3.5 Risk Scoring: единая шкала 1–5

**Шкала риска (везде в системе: UI, API, база данных, конфиги):**

| Значение | Уровень | Цвет | Описание |
|---|---|---|---|
| 1 | LOW | #00C853 | Минимальный риск |
| 2 | LOW-MEDIUM | #00C853 | Повышенное внимание |
| 3 | MEDIUM | #FFD700 | Умеренный риск |
| 4 | HIGH | #FF8C00 | Высокий риск |
| 5 | CRITICAL | #FF4444 | Критический риск |

**Алгоритм (weighted signal fusion):**

```yaml
signals:
  - id: country_risk
    source: travel_advisories  # US State Dept + FCDO + AU DFAT
    weight: 0.25
    scale: [1, 5]
    aggregation: max_of_three_sources
  - id: conflict_events
    source: gemini_grounding + ais_blackout + navtex
    weight: 0.30
    scale: [1, 5]
    decay_halflife_hours: 72
  - id: supply_concentration
    source: hhi_index
    weight: 0.20
    scale: [1, 5]
    formula: "min(5, hhi / 2000)"
  - id: price_volatility
    source: price_series  # EIA для нефти/газа; commodity indices для минералов
    weight: 0.15
    scale: [1, 5]
    formula: "min(5, 30d_volatility_pct / 10)"
  - id: freight_stress
    source: bdi_or_fbx
    weight: 0.10
    scale: [1, 5]
    formula: "min(5, (current - 90d_avg) / 90d_avg * 10)"

node_risk_formula: "sum(signal.value * signal.weight)"
route_risk_formula: "max(node_risk for node in waypoints) * chokepoint_multiplier"
material_risk_formula: "weighted_avg(route_risk, weight=volume_share)"
```

### 3.6 AI интеграция (Vertex AI / Gemini)

**Risk Scoring Pipeline:**
- Входной контекст для каждого объекта: геолокация, страна, исторические данные производства, текущие конфликты в регионе (из signals), санкционный статус, зависимости от других материалов, последние события (GDELT)
- Gemini prompt: структурированный JSON output с объяснением
- Grounding: Google Search для верификации актуальности
- Confidence < 0.7 → флаг для аудита (не блокирует публикацию; human review queue — Phase 2)

**Search Grounding для Q&A:**
- Пользователь: "Как повлияет эскалация в Южно-Китайском море на цепочки редкоземельных металлов?"
- → RAG: граф цепочек + Gemini Search Grounding
- → Structured response: affected materials + affected routes + risk delta estimate

**Disclaimer (обязателен в UI):**

*Основной (при первом входе):* "Оценки рисков формируются автоматически на основе открытых источников с использованием ИИ. Они носят информационный, а не рекомендательный характер. Не используйте их как единственный источник для коммерческих, инвестиционных или операционных решений."

*Inline (рядом с каждым score):* "⚠ AI-оценка. Экспериментально."

### 3.7 Alias Resolution и Fuzzy Match

**Алгоритм сопоставления:**

| Тип данных | Алгоритм |
|---|---|
| Названия компаний / организаций | TF-IDF + Levenshtein distance (порог ≤ 0.2) |
| Описания и свободный текст объектов | Embedding similarity (Vertex AI text-embedding-004) |
| Названия материалов и химических соединений | Exact match по CAS/HS → fallback TF-IDF |
| Географические названия | OSM normalisation + USGS geographic names |

**Правило ingestion:** при fuzzy match score > 0.85 → автоматически assign canonical path. При 0.70–0.85 → флаг `needs_review`. При < 0.70 → создаётся новая запись.

Canonical ID для организаций: **GLEIF LEI** — единственный авторитетный источник.

### 3.8 Геоданные

**MVP (Firestore):**
- Точечные объекты (facilities): GeoPoint в документах
- Маршруты: массив waypoints GeoPoint
- Пространственные запросы: клиентская фильтрация по bbox
- Конфликтные зоны: bounding box + radius

**Phase 2 (PostGIS + BigQuery GIS):**
- Cloud SQL PostgreSQL + PostGIS: LineString для маршрутов, Polygon для зон конфликтов
- BigQuery GIS: аналитика пересечений (какие маршруты пересекают зоны риска)
- Memorystore Redis: кэш пространственных запросов, TTL 6 часов

---

## 4. UX и визуализация

### 4.1 Концепция: Investigation Tool, не Dashboard

Продукт конкурирует с Bloomberg Terminal, Excel и custom Python-дашбордами аналитиков. Пользователь платит за **информацию**, а не за анимации. Каждый пиксель несёт данные или структуру.

**Принцип:** плотность информации > визуальная лёгкость. Никаких hero sections, онбординга с иллюстрациями, "пустых состояний" с котятами.

**Модель взаимодействия: Progressive Disclosure**
- Уровень 1: Global overview — карта мира с риск-heatmap, топ-алерты
- Уровень 2: Material focus — выбор материала → цепочка на карте + граф переделов
- Уровень 3: Facility detail — клик по объекту → карточка, риски, связи
- Уровень 4: Route analysis — конкретный маршрут → исторические данные, альтернативы

### 4.2 Визуальный язык

**Типографика:**
- Данные и метрики: JetBrains Mono (monospace)
- Текст: Inter или IBM Plex Sans
- Размерная шкала: 11 / 13 / 15 / 19 / 24 px — ничего промежуточного

**Цветовая схема:**
```
Фон основной:  #0D1117
Текст основной: #E6EDF3
Текст вторичный: #8B949E
Disabled: #484F58
Линии/рамки: #21262D
Акцент (интерактив): #4FC3F7

Risk-палитра (строго семантическая):
  risk_critical:  #FF4444  (уровень 5)
  risk_high:      #FF8C00  (уровень 4)
  risk_medium:    #FFD700  (уровень 3)
  risk_low:       #00C853  (уровни 1–2)
  risk_unknown:   #484F58  (нет данных)
```

### 4.3 Главный экран

```
┌──────────────────────────────────────────────────────────────┐
│ [HEADER] CriticalChain | Material Search | Alerts | [Login] │
├────────────┬─────────────────────────────────┬───────────────┤
│            │                                 │               │
│ LEFT PANEL │    CENTRAL MAP (MapLibre)        │ RIGHT PANEL  │
│            │                                 │               │
│ Materials  │  [Layers: Choropleth | Routes   │ Chain graph   │
│ ──────     │   | Facilities | Conflict zones]│ (D3/Cytoscape)│
│ • Lithium  │                                 │               │
│ • Cobalt   │  Animated flow arcs             │ Risk score    │
│ • REE      │  Facility clusters              │ 1–5 badge     │
│ • Helium   │  Chokepoint markers             │               │
│ • Silicon  │                                 │ AI Insights   │
│ • Oil      │                                 │ (Gemini)      │
│ • Gas      │                                 │               │
│            │                                 │ ⚠ AI-оценка  │
│ Filters    │                                 │ Экспериментал.│
│ ──────     │                                 │               │
│ Region     │                                 │               │
│ Risk level │                                 │               │
└────────────┴─────────────────────────────────┴───────────────┘
```

### 4.4 MapLibre слои

| Слой | Данные | Визуализация |
|---|---|---|
| Base | Тёмная политическая карта | MapTiler Dark |
| Choropleth | Country risk score (1–5) | Gradient по risk-палитре |
| Conflict zones | bounding boxes риск-зон | Semi-transparent overlay |
| Trade routes | Route + volume | Animated arcs; толщина = объём |
| Facilities | criticalchain_nodes | Кластеры; иконка по facilityKind |
| Chokepoints | Hormuz, Bab-el-Mandeb и др. | Маркеры с risk badge |

Технические детали: MapLibre GL JS v4+; deck.gl для arc/flow визуализации; WebGL heatmap; supercluster для кластеризации объектов > 500.

### 4.5 Граф переделов

- Библиотека: Cytoscape.js или D3-force
- Layout: DAG (directed acyclic graph), слева-направо
- Узлы: материалы (круги) и объекты (прямоугольники)
- Цвет узла: risk score по risk-палитре (1–5)
- Рёбра: подписаны типом передела; толщина = объём потока
- Интерактивность: hover → tooltip, click → drill-down

```
[Li Ore] → [Li₂CO₃] → [Cathode NMC] → [Battery Cell] → [Battery Pack] → [EV]
  ▲            ▲            ▲               ▲               ▲              ▲
 risk:4       risk:5       risk:4          risk:3           risk:2         risk:1
(Китай)    (Китай 60%)  (Китай 70%)  (CATL monopoly)     (OEM)
```

### 4.6 Risk Badge

```
Формат: [●] CRITICAL  или  [●] 4.2 / 5
Размер: 12px точка + 11px текст
Hover tooltip: "Sources: FCDO L3, AIS blackout, NAVTEX active — Updated 2h ago"
```

Никаких progress bars, ring charts, animated пульсирующих кружков.

### 4.7 Таблицы данных

- Плотные (row height 28px), zebra striping
- Сортировка по заголовку, inline фильтрация
- Pagination: 50/100/200 строк (не infinite scroll)

### 4.8 Режимы доступа

**Демо-режим (без регистрации):**
- Полный просмотр данных, карты, графов переделов
- Risk scores видны с inline дисклаймером
- **Watches и алерты недоступны** — требуется аккаунт
- Экспорт недоступен

**Авторизованный пользователь:**
- Все функции демо-режима +
- Создание watches (уведомления при изменении risk score)
- Настройка алертов по материалам / регионам
- Экспорт данных (CSV, JSON, PDF)
- AI Q&A (Phase 2)

**Алерты — только для зарегистрированных пользователей:**
- Пользовательские watches: "уведомить при risk score > 4 для литиевой цепочки"
- Системные алерты: новый конфликт в регионе с критическими объектами
- Дайджест: ежедневная сводка по отслеживаемым материалам
- Каналы MVP: email; Phase 2: Slack/Teams webhooks, in-app

### 4.9 Продукт — только веб

CriticalChain — веб-приложение (SPA). Мобильного приложения нет. Нативных приложений нет. PWA не реализуется.

Обоснование: продукт ориентирован на корпоративных аналитиков, работающих за рабочими станциями. Таблицы данных, граф переделов и интерактивная карта с множественными слоями требуют большого экрана.

### 4.10 Дополнительные функции

**Data Governance:**
- Каждый data point: источник + дата обновления видны в UI
- Исторические снимки: 2 года rolling window (BigQuery партиционирование)
- Data lineage: audit trail для каждого факта

**Roles:**
- Viewer: просмотр данных и карты
- Analyst: watches, алерты, экспорт (требует авторизации)
- Admin: управление пользователями организации, API keys
- Super Admin: управление источниками, системные настройки

**Multi-tenancy:**
- Watches, алерты, аннотации — tenant-scoped
- Данные цепочек (facilities, materials, routes) — общие для всех авторизованных пользователей
- Risk scores — видны всем авторизованным

**Export (Phase 2):**
- Форматы: CSV, Excel, JSON, PDF-отчёты
- Шаблонные отчёты по материалу / региону / риску

**API (Phase 2):**
- REST API + API keys management
- Webhook API для интеграции с SAP, Oracle
- Rate limiting по тарифным планам
- OpenAPI/Swagger документация

**i18n:**
- MVP: английский язык
- Архитектурно подготовлено через react-i18next
- Приоритет локализации: EN → ZH → JA → DE → FR

---

## 5. Риски продукта

### 5.1 Юридические риски

**Data Licensing:**
- UN Comtrade+: коммерческое использование — платный tier (~$5K/год)
- ACLED: исключён по экономическим соображениям (~$10K/год). Используются бесплатные альтернативы.
- OSM: ODbL требует share-alike для derivative databases — осторожно с derived geo products
- USGS: Public Domain — безопасно
- EIA: Public Domain — безопасно
- ENTSO-G: бесплатный открытый API
- MarineTraffic: строго коммерческая. Использовать aisstream.io (бесплатный) для AIS-сигналов

Бюджет на лицензии источников: $10–20K/год (без ACLED).

**Re-export Restrictions:**
- Исключить из MVP данные о военных и ядерных объектах
- Консультация с export control юристом

**GDPR / CCPA:**
- Privacy by design; DPA с клиентами
- Данные об организациях (LEI, адреса) — применимо GDPR

**Регуляторные возможности:**
- EU Critical Raw Materials Act 2024 — продукт может позиционироваться как compliance tool
- US Inflation Reduction Act — требования к sourcing для американских клиентов

### 5.2 Геополитические риски

- Точные данные о местоположении критических объектов могут интересовать государственных акторов
- Данные о китайских объектах (~70–80% переработки) — юридические риски при работе с китайскими клиентами
- Нельзя предоставлять доступ пользователям из SDN-листа

**Митигация:** Terms of Service с явными ограничениями; geo-blocking для sanctioned territories; entity screening для enterprise клиентов.

### 5.3 Технические риски

**Data Freshness:**
- UN Comtrade лаг: 2–18 месяцев; чётко показывать дату обновления каждого data point
- Данные о производстве из развивающихся стран (ДРК, Боливия) — часто неточные

**AI Hallucination:**
- Mandatory disclaimer в UI (см. раздел 3.6)
- Audit trail: что повлияло на score
- Human review queue — Phase 2 (простой internal UI для проверки score changes > 1.5 units)

**Graph Complexity:**
- Материализованные sub-graphs по материалу; ленивая загрузка; pagination

### 5.4 Бизнес-риски

**Monetization:**
- Tier 1 (Demo): публичные данные, read-only, без watches — $0
- Tier 2 (Professional): все материалы, до 5 users, watches, алерты, export — $2–5K/мес
- Tier 3 (Enterprise): unlimited users, API access, custom integrations, SLA — $10–30K/мес
- Tier 4 (Government): on-premise option, FedRAMP, classified data integration — custom

**GTM:**
- Прямые продажи для enterprise (ACV $50–200K/год)
- Партнёрство с консалтингом (Big 4) для distribution
- Government contracts — требуют FedRAMP compliance (США)
- Демо-режим для brand awareness

---

## 6. Roadmap

### 6.1 MVP (Месяцы 1–4)

**Фокус:** доказательство ценности для первых клиентов. 7 материалов (включая нефть и газ), базовая карта, реальные данные.

**Месяц 1 — Фундамент:**
- GCP инфраструктура: Cloud Run, Firestore, BigQuery, Pub/Sub, Memorystore
- Go API: базовые CRUD endpoints для materials, facilities, routes
- Data adapters: USGS MRDS + EIA API + UN Comtrade (основные данные)
- React/Vite/TypeScript SPA с MapLibre базовой картой
- Firebase Auth (Google SSO)
- CI/CD: Cloud Build → Cloud Run
- Конфиги материалов (YAML): Li, Co, REE, He, Si, Oil, Gas

**Месяц 2 — Данные и граф:**
- Firestore граф (criticalchain_nodes, criticalchain_edges): 7 материалов
- Supply chain данные: ручная верификация для 7 материалов
- Data pipeline: Country Risk adapters (State Dept, FCDO, DFAT) + OFAC + GDELT + ENTSO-G
- Нефть и газ: EIA + ENTSO-G adapters; ключевые chokepoints (Hormuz, Bab-el-Mandeb)
- Конфликтные зоны на карте (из country risk + NAVTEX)
- Базовый risk scoring rule-based (шкала 1–5)

**Месяц 3 — UI и AI:**
- Полноценный 3-панельный layout
- Граф переделов (Cytoscape.js), DAG layout
- MapLibre: facilities, routes, chokepoints, choropleth
- Vertex AI: Gemini risk scoring pipeline (шкала 1–5)
- Базовые алерты (email) — только для авторизованных
- Демо-режим: read-only без регистрации

**Месяц 4 — Polish и запуск:**
- User roles (Viewer / Analyst / Admin)
- AIS blackout detection (aisstream.io integration)
- Signal Fusion: консенсус ≥2 источников
- Performance optimization
- Security audit; disclaimer onboarding
- Beta-клиенты: 2–3 пилота (целевые: консалтинг, КазМунайГаз)

**MVP НЕ включает:** API для внешних клиентов, i18n, advanced AI Q&A, Government tier, PostGIS, Spanner Graph, human review queue, PDF-отчёты, Slack/Teams webhooks.

### 6.2 Phase 2 (Месяцы 5–8)

- 20+ материалов в системе
- PostGIS: миграция гео-данных с LineString, Polygon для конфликтных зон
- Spanner Graph: граф-траверсалы, multi-hop зависимости
- AI Q&A (RAG + Gemini Search Grounding)
- REST API + API keys management
- Advanced alerting: Slack/Teams webhooks
- Исторические данные (2 года)
- Human review queue: simple internal UI для review score changes > 1.5
- PDF отчёты
- i18n EN + ZH

### 6.3 Phase 3 (Месяцы 9–12)

- 50+ материалов (все USGS Critical Minerals)
- Multi-tenant SaaS с billing (Stripe)
- GraphQL API
- Custom data integrations для enterprise
- FedRAMP assessment (US Government)
- Scenario modeling: "что если Китай закроет экспорт gallium?"
- Supply chain stress testing / simulation

### 6.4 Full Product (Год 2+)

- Predictive analytics: early warning system
- Alternative supply chains: AI-generated recommendations
- Portfolio risk: корзина материалов, portfolio exposure
- Private data integration: клиент загружает свои supplier данные
- Marketplace: аналитические отчёты от партнёров
- White-label опция

### 6.5 Автоматизация с первого дня

✓ Data pipeline (Scheduler → Workers)
✓ Sanctions checking (OFAC + EU)
✓ Country risk aggregation (State Dept + FCDO + DFAT)
✓ GDELT event detection
✓ AIS blackout monitoring
✓ CI/CD (Cloud Build)
✓ AI risk scoring — с месяца 3

○ User onboarding — пока мануальный
○ Billing / subscription management (Stripe) — Phase 2
○ Advanced alerting rules engine — Phase 2
○ PDF report generation — Phase 2

---

## 7. Принципы универсальной архитектуры

### 7.1 Принцип: новый материал = только данные, не код

Добавление нового материала требует только заполнения конфигурационных данных — без изменения кода, без деплоя, без разработчика.

**Тест:** если добавление вольфрама требует хотя бы одного `if/switch/hardcode` в коде — архитектура нарушена.

### 7.2 MaterialDefinition (YAML конфиг)

```yaml
# internal/criticalchain/config/materials/tungsten.yaml
material_id: tungsten
display_name:
  ru: "Вольфрам"
  en: "Tungsten"
chemical_symbol: W
category: critical_mineral  # critical_mineral | energy_commodity | agricultural
subcategory: refractory_metal

units:
  primary: metric_ton
  aliases: [t, MT, tonne]

production_stages:
  - id: 0
    name: mining
    type: extraction
    facilityKind: mine  # из taxonomy
    typical_forms: [wolframite_ore, scheelite_ore]
  - id: 1
    name: concentration
    type: processing
    facilityKind: refinery
    conversion_ratio: 0.65
  - id: 2
    name: apt_production
    type: chemical_processing
    facilityKind: factory
    typical_forms: [APT]
    conversion_ratio: 0.85

data_sources:
  - source_id: usgs_mcs
    type: annual_statistics
    fields: [production_by_country, reserves, price]

key_chokepoints:
  - path: /loc/cn/jiangxi
    share_of_global_supply: 0.82
    risk_multiplier: 2.5

concentration_hhi: 8900
```

### 7.3 Универсальные правила risk scoring

Правила не зависят от конкретного материала (см. алгоритм в разделе 3.5). При добавлении вольфрама — те же сигналы, тот же алгоритм 1–5. Меняется только конфигурация данных (price_series, hhi_index, key_chokepoints из MaterialDefinition).

### 7.4 Процесс добавления нового материала

```
1. Создать YAML: internal/criticalchain/config/materials/{material}.yaml
2. Загрузить данные: internal/criticalchain/config/entities/{material}_entities.csv
3. Запустить ingestion: npm run ingest:material {material}
4. Настроить data_sources в конфиге
5. Запустить scoring: npm run score:material {material}
6. Материал появляется в интерфейсе автоматически
```

### 7.5 Визуализации параметризованы

Компоненты не знают о конкретных материалах. Они получают конфиг:

```yaml
GraphConfig:
  material: tungsten
  max_depth: 4
  node_label: canonical_path_short
  edge_weight: volume_share
  color_by: node_risk_score  # шкала 1–5
```

---

## 8. Интеграция с ядром

### 8.1 Позиционирование

CriticalChain — эксперимент-подсистема. В ядро не интегрируется. Использует таксономию и возможности ядра. Совместимость сохраняется для возможного слияния.

### 8.2 Таксономия ядра (обязательно)

CriticalChain использует taxonomy package ядра (Horizon):

- **CanonicalPath:** `/fac/{geo}/{slug}`, `/org/{geo}/{slug}`, `/product/{category}/{type}/{stage}/{slug}`
- **FacilityKind:** mine, refinery, seaport, terminal, pipeline, factory, warehouse, power_plant, other
- **FacilityRole:** extraction, refining, processing, manufacturing, distribution, storage, logistics
- **EntityPath:** /Infrastructure/Manufacturing/Mine, /Resource/RawMaterial/Mineral, /Geo/Zone/Conflict, /Deposit/BatteryMetal...
- **Geography:** /loc/{country}/{admin1} с ISO

Для нефти и газа: FacilityKindWell → маппинг на `mine`; FacilityKindField → маппинг на Deposit.

### 8.3 Примеры canonical paths

```
Литиевый рудник в Чили:
  /fac/cl/atacama/sociedad_quimica_y_minera
  (material_id: lithium, stage: 0, facilityKind: mine)

НПЗ в Казахстане (для нефти):
  /fac/kz/atyrau/atyrau_refinery
  (material_id: crude_oil, stage: 3, facilityKind: refinery)

СПГ-терминал в Катаре (для газа):
  /fac/qa/ras_laffan/qatargas_lng
  (material_id: natural_gas, stage: 2, facilityKind: terminal)
```

### 8.4 Хранилище

- **Firestore:** отдельные коллекции с префиксом `criticalchain_` (не смешивать с Horizon)
- **Backend:** пакет `internal/criticalchain` (не размазываем по horizon, scout, taxonomy)
- **Frontend:** роут `/criticalchain` в том же SPA
- **Auth:** общий Firebase Auth, одни пользователи
- **Vertex AI:** тот же проект, отдельный datastore для CriticalChain

### 8.5 Multi-tenancy

- Watches, алерты, аннотации — tenant-scoped
- Данные цепочек — общие для всех авторизованных
- Risk scores — общедоступны по клику (после авторизации)
- Демо (без авторизации) — read-only, без watches и алертов

### 8.6 Граница с Horizon

- HorizonFragment не используем напрямую — своя модель в CriticalChain
- Связь по canonical path при необходимости
- Worldmonitor (temp-repos) не участвует в CriticalChain

---

## Приложение А: Источники данных

### Источники MVP (6-часовой цикл)

| # | Источник | Покрытие | Качество | Лицензия | Приоритет | Стоимость |
|---|---|---|---|---|---|---|
| 1 | USGS MRDS | Глоб. минералы | ★★★★★ | Public Domain | MVP | Бесплатно |
| 2 | EIA API | США + международная нефть/газ | ★★★★★ | Public Domain | MVP | Бесплатно |
| 3 | ENTSO-G API | Европейский газ | ★★★★☆ | Open | MVP | Бесплатно |
| 4 | JODI Data | Нефть/газ ~100 стран | ★★★★☆ | CC BY-NC | MVP | Бесплатно |
| 5 | UN Comtrade+ | Глоб. торговые потоки | ★★★★☆ | API платный | MVP | ~$5K/год |
| 6 | OFAC SDN List | США санкции | ★★★★★ | Public Domain | MVP | Бесплатно |
| 7 | EU Sanctions List | ЕС санкции | ★★★★★ | Open | MVP | Бесплатно |
| 8 | GLEIF LEI | 2M+ юрлиц | ★★★★☆ | CC0 | MVP | Бесплатно |
| 9 | GDELT (BigQuery) | Глоб. события | ★★★☆☆ | Public Domain | MVP | GCP costs |
| 10 | OSM Overpass | Глоб. объекты | ★★★☆☆ | ODbL | MVP | Бесплатно |
| 11 | Natural Earth | Маршруты, границы | ★★★★☆ | Public Domain | MVP | Бесплатно |
| 12 | US State Dept RSS | Страновые риски | ★★★★☆ | Public Domain | MVP | Бесплатно |
| 13 | FCDO GOV.UK API | Страновые риски | ★★★★☆ | OGL v3.0 | MVP | Бесплатно |
| 14 | AU DFAT RSS | Страновые риски | ★★★★☆ | CC Attribution | MVP | Бесплатно |
| 15 | NGA Maritime Safety | Морские предупреждения | ★★★★☆ | Public Domain | MVP | Бесплатно |
| 16 | aisstream.io | AIS трафик реальное время | ★★★☆☆ | Free tier | MVP | Бесплатно |
| 17 | FAA NOTAM API | Авиационные ограничения | ★★★★☆ | Public Domain | MVP | Бесплатно |
| 18 | Gemini Search Grounding | Агрегатор новостей | ★★★★☆ | pay-per-token | MVP | Gemini API |

### Источники Phase 2

| # | Источник | Покрытие | Приоритет | Стоимость |
|---|---|---|---|---|
| 19 | IEA Data Explorer | Clean energy minerals | Phase 2 | Бесплатно |
| 20 | BACI (CEPII) | Глоб. торговля, гармонизированная | Phase 2 | ~$3K/год |
| 21 | OECD TiVA | Value added в цепочках | Phase 2 | Бесплатно |
| 22 | Global Forest Watch | ESG overlay, горнодобыча | Phase 2 | Бесплатно |
| 23 | HDX / OCHA | Гуманитарные кризисы | Phase 2 | Бесплатно |
| 24 | FAO STAT | Агро-цепочки | Phase 2 | Бесплатно |
| 25 | World Bank Open Data | Макро-контекст | Phase 2 | CC BY 4.0 |

**Исключены по экономическим соображениям:**
- **ACLED** (~$10K/год) — заменён многоуровневой системой косвенных сигналов (уровни 0–4: Gemini Grounding, travel advisories, AIS/NAVTEX, BDI, медиа)
- **MarineTraffic** (коммерческая) — заменён aisstream.io (бесплатный) для AIS-сигналов

---

## Приложение Б: Технические зависимости

### Backend (Go)

```
Go 1.22+
Gin или Chi (HTTP router)
cloud.google.com/go/firestore
cloud.google.com/go/bigquery
cloud.google.com/go/vertexai
google.golang.org/protobuf
```

*Примечание: pgx v5 (PostgreSQL/PostGIS driver) — Phase 2. cloud.google.com/go/spanner (Spanner Graph) — Phase 2.*

### Frontend

```
React 19+
TypeScript 5.4+
Vite 5+
MapLibre GL JS 4+
deck.gl 9+ (arc layers)
Cytoscape.js (supply chain graph)
TanStack Query v5 (data fetching)
Zustand (state management)
Recharts (time series)
Tailwind CSS v4
react-i18next (i18n, архитектурно готово)
```

### GCP Services

**MVP:**
```
Cloud Run (API + Workers)
Firestore (graph + geo storage)
BigQuery (analytics + GDELT + trade flows)
Pub/Sub (pipeline orchestration)
Memorystore Redis (caching, TTL 6h)
Cloud Scheduler (6-hour trigger)
Vertex AI (Gemini Pro + Search Grounding)
Firebase Authentication
Cloud CDN (static assets)
Cloud Load Balancing
Secret Manager (API keys)
Cloud Build (CI/CD)
```

**Phase 2 (добавляются):**
```
Cloud SQL (PostgreSQL 16 + PostGIS 3.4)
Cloud Spanner (Graph — для graph traversals)
```

---

*© 2026 CriticalChain. Whitepaper v3.0 — Canonical Document.*
*Все противоречия между v1.0 и v2.0 разрешены. Решения заказчика внесены.*
*Следующее обновление — по результатам MVP-пилота.*
