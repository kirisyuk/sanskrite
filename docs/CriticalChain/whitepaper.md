# CriticalChain — Whitepaper

> Мониторинг цепочек поставок критических материалов
> Версия 1.0 + Дополнение v2.0 | Март 2026

---


CRITICALCHAIN — WHITEPAPER
Система мониторинга цепочек поставок критических материалов
Версия 1.0 | Март 2026

════════════════════════════════════════════════════════════════
EXECUTIVE SUMMARY
════════════════════════════════════════════════════════════════

Мир вступил в эру «войн за материалы». Редкоземельные элементы, литий, кобальт, полупроводниковый кремний, гелий и десятки других критических веществ определяют, кто производит электромобили, МРТ-сканеры, ракеты и смартфоны. Сбои в этих цепочках — от геополитических конфликтов до стихийных бедствий — обходятся мировой экономике в сотни миллиардов долларов ежегодно.

CriticalChain — это профессиональный инструмент аналитики и мониторинга цепочек поставок критических материалов, их многоуровневых деривативов и связанных рисков. Продукт предназначен для корпоративных аналитиков, государственных агентств и инвестиционных фондов, которым необходимо понимать реальные зависимости — от шахты до конечного продукта.

Ключевые характеристики продукта:
• Граф цепочек поставок с многоуровневыми переделами (материал → полуфабрикат → конечный продукт)
• Интерактивная карта на MapLibre: маршруты, объекты, риск-зоны
• AI-анализ рисков через Vertex AI / Gemini с Search Grounding
• Обновление данных каждые 6 часов из предопределённого набора открытых источников
• Явная визуализация рисков: геополитика, санкции, конфликты, природные угрозы
• Стек: GCP / Go / React / TypeScript / Vite / MapLibre

Рыночная дыра: существующие платформы (Everstream, Resilinc, Interos) фокусируются на supplier risk management для корпораций, но не дают аналитикам и государственным структурам инструмент для исследования полных цепочек критических материалов с переделами, с акцентом на геополитику и open data.

Потенциальные покупатели: strategic intelligence команды крупных корпораций (automotive, aerospace, pharma, defense), государственные агентства по критическим материалам, хедж-фонды и commodity trading firms, ESG/sustainability исследовательские организации.

════════════════════════════════════════════════════════════════
РАЗДЕЛ 1: ЧТО ЗАБЫЛ ЗАКАЗЧИК
════════════════════════════════════════════════════════════════

При анализе исходного ТЗ выявлен ряд критических пробелов, которые необходимо закрыть ДО начала разработки.

1.1 DATA GOVERNANCE И ИСТОРИЧНОСТЬ

Забыто полностью. Критично для продукта.

Вопросы, требующие ответа:
• Как долго хранить исторические снимки данных? Рекомендация: минимум 2 года rolling window, с агрегированными архивами за предыдущие периоды.
• Как обрабатывать contradicting data из разных источников? Нужна политика source priority и conflict resolution.
• Версионирование цепочек: если в 2024 году маршрут был X, а в 2025 стал Y — это должно быть отражено в истории.
• Data lineage: для каждого факта в системе должно быть видно, из какого источника он получен и когда обновлён.
• Retention policy для compliance: если продукт будет работать в EU, нужно соответствовать GDPR по данным об организациях.

Рекомендация: использовать BigQuery с партиционированием по дате + отдельную таблицу снимков (snapshots) для каждой сущности.

1.2 ALERTING И NOTIFICATIONS

Не упомянуто. Без алертинга продукт теряет 50% ценности.

Что нужно:
• Пользовательские watches: «уведомить меня, если risk score для цепочки лития вырастет выше 7/10»
• Системные алерты: новый конфликт в регионе с критическими объектами, санкции против ключевого поставщика
• Дайджест: еженедельная/ежедневная сводка по отслеживаемым материалам
• Каналы доставки: email, Slack/Teams webhook, in-app

Стек: Cloud Pub/Sub → Cloud Run workers → SendGrid/Slack API

1.3 USER ROLES И MULTI-TENANCY

Полностью отсутствует в ТЗ. Без этого — продукт нельзя продавать корпоративным клиентам.

Предложенная модель ролей:
• Viewer: только просмотр данных и карты
• Analyst: создание кастомных views, экспорт данных, настройка алертов
• Admin: управление пользователями организации, API ключи, интеграции
• Super Admin (internal): управление источниками данных, системные настройки

Multi-tenancy: каждый клиент — отдельный tenant с изолированными данными пользовательских настроек, watches, и кастомных аннотаций. Данные о цепочках — общие.

1.4 API ДЛЯ ВНЕШНИХ ПОТРЕБИТЕЛЕЙ

Не упомянуто, но критично для B2B монетизации.

Что нужно:
• REST API + GraphQL для программного доступа к данным о цепочках
• Webhook API для интеграции с внутренними системами клиентов (SAP, Oracle)
• Rate limiting по тарифным планам
• API keys management через UI
• OpenAPI/Swagger документация
• SDK на Python и TypeScript (опционально, для enterprise tier)

1.5 МОБАЙЛ

Не упомянуто. Нужно определиться.

Рекомендация: мобильный веб (PWA) как MVP. Нативные приложения — не нужны для B2B аналитического инструмента. Адаптивный дизайн с responsive MapLibre для планшетов — обязателен. Смартфон: только read-only режим (просмотр алертов, базовые карты).

1.6 ИНТЕРНАЦИОНАЛИЗАЦИЯ (i18n)

Забыто. Рынок критических материалов — глобальный.

Рекомендация для MVP: английский как primary язык. Архитектурно подготовить i18n через react-i18next. Приоритет локализации: EN → ZH → JA → DE → FR (исходя из ключевых рынков critical minerals).

1.7 ЭКСПОРТ ДАННЫХ

Не упомянуто. Аналитики требуют экспорта.

Форматы: CSV, Excel, JSON, PDF-отчёты (с картой и графиками). Шаблонные отчёты по материалу/региону/риску — важная фича для enterprise продаж.

1.8 SANDBOX / ДЕМО РЕЖИМ

Без демо-режима сложно продавать. Нужен публичный демо-стенд с реальными (но ограниченными) данными — без требования регистрации.

1.9 COMPLIANCE И САНКЦИОННЫЕ СПИСКИ

Упомянуты конфликты, но не упомянуты конкретные compliance требования:
• OFAC (US), EU Restrictive Measures, UN Sanctions — автоматическая проверка объектов
• EU Critical Raw Materials Act (2024) — регуляторный контекст для европейских клиентов
• US Inflation Reduction Act — требования к sourcing для американских клиентов
• CBAM (Carbon Border Adjustment Mechanism) — влияет на цепочки для EU

1.10 АРХИТЕКТУРНЫЕ РЕШЕНИЯ ДО КОДА

Критические вопросы, требующие ответа ПРЕЖДЕ написания кода:

а) Data model: как моделировать «передел»? Рекомендация: directed graph где нода — это Material или Facility, ребро — ProcessingStep с атрибутами (yield ratio, typical location, key players).

б) Conflict detection: единый canonical ID для организаций и материалов. Без него данные из разных источников не сольются. Рекомендация: использовать GLEIF LEI для организаций, HS-коды + CAS номера для материалов.

в) Geo model: один слой не подойдёт. Нужно разделить: точечные объекты (шахты, заводы) vs. линейные маршруты vs. полигональные риск-зоны.

г) Graph storage: отдельная graph DB vs. BigQuery? Рекомендация: Spanner Graph (preview) или Neo4j на GKE для граф-запросов, BigQuery — для аналитики и ML.

д) AI hallucination mitigation: Gemini с Search Grounding уменьшает галлюцинации, но нужен human review workflow для AI-генерированных risk scores.

════════════════════════════════════════════════════════════════
РАЗДЕЛ 2: АНАЛИЗ ДОМЕНА
════════════════════════════════════════════════════════════════

2.1 SUPPLY CHAIN CRITICALITY: ЧТО ЭТО

Критичность материала определяется двумя осями:
1. Экономическая важность: незаменимость в ключевых технологиях и отраслях
2. Риск нарушения поставок: географическая концентрация, геополитика, монополии

Индексы и списки критических материалов:
• USGS 2025 Critical Minerals List: 60 минералов и материалов
• EU Critical Raw Materials Act 2024: 34 критических + 17 стратегических материалов
• IEA Critical Minerals for Clean Energy Transitions
• DoD Critical Materials List (США, частично открытый)

2.2 КАТЕГОРИИ КРИТИЧЕСКИХ МАТЕРИАЛОВ

РЕДКОЗЕМЕЛЬНЫЕ ЭЛЕМЕНТЫ (REE)
Примеры: неодим, диспрозий, тербий, лантан, церий
Применение: постоянные магниты (EV моторы, ветрогенераторы), электроника, катализаторы
Риск: ~90% добычи — Китай; ~85% переработки — Китай
Ключевые цепочки: REE оксид → сплав NdFeB → магнит → EV двигатель

БАТАРЕЙНЫЕ МЕТАЛЛЫ
Литий: Австралия, Чили, Аргентина (Lithium Triangle) → 90% переработки Китай
Кобальт: 70%+ добычи — ДРК; 80%+ рафинирования — Китай
Никель: Индонезия, Филиппины, Россия
Графит: ~80% производства — Китай (природный и синтетический)
Марганец: ЮАР, Австралия, Габон

ПОЛУПРОВОДНИКОВЫЕ МАТЕРИАЛЫ
Кремний metal-grade → поликремний → кремниевые вафли → чипы
Германий, Галлий: ключевые для compound semiconductors; ~80% — Китай
Тантал: конденсаторы; основной источник — ДРК (coltan)
Индий: дисплеи ITO; ~58% — Китай

СПЕЦИАЛЬНЫЕ ГАЗЫ
Гелий: уникально критичен; США (Wyoming), Катар, Алжир, РФ — основные источники; применение: МРТ, квантовые компьютеры, ракетное топливо
Неон: 65-70% — Украина (до 2022); криптолитография чипов
Ксенон, Криптон: ion propulsion, освещение

ЭНЕРГЕТИЧЕСКИЕ МАТЕРИАЛЫ
Уран: Казахстан (42%), Намибия, Канада, Австралия
Кобальт для ядерных реакторов
Природный газ как критическое сырьё для удобрений (аммиак)

ПРОДОВОЛЬСТВЕННЫЕ/АГРО ЦЕПОЧКИ
Калий (удобрения): Беларусь, РФ, Канада — три глобальных источника
Фосфаты: Марокко (~70% мировых резервов), Китай, РФ
Аммиак: зависит от природного газа

МЕДИЦИНСКИЕ
Йод: Чили, Япония — производство; применение: контрастные вещества, антисептики
Гафний, Молибден-99: медицинские изотопы
API (active pharmaceutical ingredients): ~80% синтезируется в Китае и Индии

2.3 МНОГОУРОВНЕВЫЕ ЦЕПОЧКИ: ПРИМЕРЫ

ЦЕПОЧКА ЛИТИЯ (EV)
Уровень 0: Добыча → Литиевая руда / Сподумен / Соляные рапы
  └── Австралия (Greenbushes), Чили (Atacama), Аргентина
Уровень 1: Переработка → Карбонат лития (Li2CO3) / Гидроксид лития
  └── Китай (~60%), Чили, Австралия
Уровень 2: Производство катодного материала → LFP, NMC, NCA
  └── Китай (~70%), Корея (LG, Samsung), Япония (Panasonic)
Уровень 3: Производство ячеек → Battery cells
  └── CATL (Китай), BYD, LG Energy, Panasonic, Samsung SDI
Уровень 4: Battery pack assembly → Батарейные модули
  └── OEM автопроизводители
Уровень 5: Конечный продукт → Электромобили
  └── Tesla, BYD, VW, GM, Ford, BMW...

ЦЕПОЧКА ГЕЛИЯ (МРТ)
Уровень 0: Добыча → Природный газ с содержанием He
  └── США (Wyoming, Kansas), Катар, РФ, Алжир
Уровень 1: Очистка и сжижение → Жидкий гелий (LHe)
  └── BLM (US), Air Liquide, Linde, Matheson
Уровень 2: Дистрибуция → Криогенные контейнеры-танкеры
  └── Глобальная логистика, критичность: НЕТ СУБСТИТУТОВ
Уровень 3: Охлаждение магнитов → Сверхпроводящие магниты
  └── Производители: Siemens Healthineers, GE Healthcare, Philips
Уровень 4: Конечный продукт → МРТ-сканеры
  └── Больницы, медицинские центры по всему миру

ЦЕПОЧКА КРЕМНИЯ (Смартфон/Чип)
Уровень 0: Добыча кварца → SiO2
  └── Глобально (США, Норвегия, Бразилия)
Уровень 1: Metal-grade silicon → Si 98.5%
  └── Китай (~70%), Норвегия, Бразилия
Уровень 2: Поликремний → Si 99.9999%+
  └── Китай (Daqo, GCL), США (Hemlock), Германия (Wacker)
Уровень 3: Выращивание кристаллов → Слитки (Czochralski)
  └── Shin-Etsu, Sumco (Япония), SK Siltron (Корея)
Уровень 4: Нарезка и полировка → Кремниевые вафли 300mm
  └── Те же игроки
Уровень 5: Fab-процессинг → Чипы (TSMC, Samsung, Intel)
  └── Тайвань (TSMC ~55% foundry), Корея, США
Уровень 6: Сборка и тестирование (OSAT)
  └── ASE, Amkor (Тайвань, Малайзия, Вьетнам)
Уровень 7: Конечный продукт → Смартфон, ноутбук, авто
  └── Apple, Samsung, Qualcomm...

2.4 ИСТОЧНИКИ ДАННЫХ: ОЦЕНКА

UN COMTRADE / Comtrade+
Покрытие: ~200 стран, ежегодные и месячные данные, ~5000 HS-кодов
Качество: ★★★★☆ (высокое, но лаг 3-18 месяцев)
Лицензия: Бесплатно для некоммерческого использования; API с лимитами; платная подписка для bulk
Ограничения: Нет данных по sub-national уровню; HS-коды не всегда точно выделяют critical minerals
Рекомендация: Основной источник для trade flows. Использовать через Comtrade+ API.

USGS Mineral Resources Data System (MRDS) + Mineral Commodity Summaries
Покрытие: 60+ critical minerals, глобальные данные по добыче и резервам, локации шахт
Качество: ★★★★★ (наиболее авторитетный источник по критическим минералам)
Лицензия: Public Domain (US Government)
Ограничения: Ежегодное обновление (январь-март); нет real-time данных
Рекомендация: Основной источник для объектов добычи и резервов.

IEA Critical Minerals Data Explorer
Покрытие: Ключевые clean energy minerals (Li, Co, Ni, REE, Cu, Al)
Качество: ★★★★☆ (хорошо для energy transition фокуса)
Лицензия: Открытый доступ, частично с attribution
Ограничения: Фокус только на clean energy; ограниченный API
Рекомендация: Дополнительный источник для прогнозов спроса.

BACI (CEPII International Trade Database)
Покрытие: 200+ стран, HS 6-digit level, с 1995
Качество: ★★★★☆ (гармонизированные данные, устраняет несоответствия)
Лицензия: Бесплатно для исследований; commercial use требует договора
Ограничения: Лаг ~2 года; требует регистрации
Рекомендация: Хорош для исторического анализа trade flows.

OECD TiVA (Trade in Value Added)
Покрытие: 76 стран, ~45 отраслей; value chain декомпозиция
Качество: ★★★★☆ (уникален для анализа добавленной стоимости в цепочках)
Лицензия: Открытый доступ
Ограничения: Агрегированный (по отраслям, не продуктам); лаг 3-4 года
Рекомендация: Важен для понимания экономической зависимости.

OpenStreetMap (OSM)
Покрытие: Глобальная база объектов (шахты, заводы, порты, железные дороги)
Качество: ★★★☆☆ (неравномерное; развитые страны — отлично, ДРК/Боливия — слабо)
Лицензия: ODbL (Open Database License) — требует attribution и share-alike
Ограничения: Данные требуют верификации; нет данных о собственниках
Рекомендация: Хорошая база для геолокаций; дополнять USGS MRDS.

Global Forest Watch / GFW Mining Layer
Покрытие: Горнодобывающие объекты, связь с вырубкой лесов
Качество: ★★★☆☆ (специализированный, но полезный для ESG overlay)
Лицензия: Creative Commons BY
Рекомендация: ESG/sustainability overlay слой.

ACLED (Armed Conflict Location & Event Data)
Покрытие: 250+ стран, 1997-настоящее, события конфликтов с координатами
Качество: ★★★★★ (лучший публичный источник данных о конфликтах)
Лицензия: Бесплатно для некоммерческих; платно для коммерческих пользователей
Ограничения: Требует коммерческой лицензии для продукта
Рекомендация: Ключевой источник для конфликт-оверлея. Обязательно.

GDELT Project
Покрытие: Новостные события глобально, обновление каждые 15 минут
Качество: ★★★☆☆ (много шума, требует фильтрации)
Лицензия: Полностью открытый (public domain)
Ограничения: Высокий noise ratio; требует ML-фильтрации
Рекомендация: Источник для event detection (новые конфликты, санкции).

GLEIF (Global LEI Foundation) — Legal Entity Identifier
Покрытие: 2+ млн юридических лиц с LEI
Качество: ★★★★☆
Лицензия: Открытый доступ (CC0)
Рекомендация: Canonical ID для организаций в системе. Обязательно.

MarineTraffic (открытые данные)
Покрытие: AIS-данные по судам; бесплатный tier очень ограничен
Качество: ★★★☆☆ (полные данные — только за деньги)
Лицензия: Коммерческая; open API очень ограничен
Ограничения: Для маршрутов (не трекинга) — использовать исторические паттерны
Рекомендация: Использовать sea route databases (Natural Earth, OpenSeaMap) для маршрутов, а не real-time AIS.

Humanitarian Data Exchange (HDX) / OCHA
Покрытие: Гуманитарные данные, включая кризисы и конфликты
Качество: ★★★★☆ (для humanitarian context)
Лицензия: Открытый доступ
Рекомендация: Дополнительный источник для гуманитарных кризисов.

OFAC SDN List / EU Sanctions List
Покрытие: Санкционные списки лиц и организаций
Качество: ★★★★★ (официальные источники)
Лицензия: Public domain (US), открытый доступ (EU)
Рекомендация: Автоматическая проверка объектов. Обязательно.

2.5 КОНКУРЕНТЫ И ДЫРЫ В РЫНКЕ

Everstream Analytics
Что делают: AI-driven supply chain risk monitoring для логистики и procurement
Сильные стороны: Real-time alerts, tariff intelligence, Gartner Leader 2025
Слабые стороны: НЕ фокусируется на critical minerals specifically; нет graph visualization переделов; B2B enterprise с высокой ценой
Дыра: Нет multi-tier material transformation chains; нет специфики critical materials

Resilinc
Что делают: Multi-tier supplier mapping, EventWatchAI (104M источников)
Сильные стороны: Глубокое supplier mapping, Fortune 500 клиенты
Слабые стороны: Нет специализации на critical minerals; нет географической визуализации маршрутов; очень дорого
Дыра: Нет open data интеграции; нет focus на material transformation chains

Interos
Что делают: AI risk intelligence, deep third-party relationship mapping
Сильные стороны: Граф зависимостей, SAP интеграция, cyber + ESG + catastrophic risks
Слабые стороны: Нет critical minerals специфики; нет карты поставочных маршрутов
Дыра: Нет focus на geopolitical supply concentration; нет public data approach

Kpler
Что делают: Commodity flows tracking (нефть, СПГ, сухие грузы) с AIS-трекингом
Сильные стороны: Real-time vessel tracking, commodity volumes
Слабые стороны: Только энергетические commodities; нет critical minerals; нет переделов
Дыра: Нет analysis of transformation chains; нет risk scoring

Windward
Что делают: Maritime AI — vessel risk, sanctions compliance
Сильные стороны: Vessel behavior analysis, sanctions screening
Слабые стороны: Только maritime; нет supply chain graph; нет материального фокуса

ВЫВОД: CriticalChain занимает уникальную нишу — critical materials специфика + multi-tier transformation chains + open data + geopolitical analysis + map visualization. Ни один из конкурентов не закрывает эту комбинацию.

════════════════════════════════════════════════════════════════
РАЗДЕЛ 3: АРХИТЕКТУРА РЕШЕНИЯ
════════════════════════════════════════════════════════════════

3.1 ОБЩАЯ АРХИТЕКТУРА (GCP-NATIVE)

┌─────────────────────────────────────────────────────────────┐
│                    CLIENT LAYER                              │
│  React + TypeScript + Vite + MapLibre + Zustand/TanStack    │
│  PWA  |  Web App (SPA)                                      │
└────────────────┬────────────────────────────────────────────┘
                 │ HTTPS / WebSocket
┌────────────────▼────────────────────────────────────────────┐
│                    API GATEWAY LAYER                         │
│  Cloud Load Balancer → Cloud Run (Go API)                   │
│  REST API v1 | GraphQL | WebSocket (alerts)                 │
│  Auth: Firebase Auth / Identity Platform                     │
└────────────────┬────────────────────────────────────────────┘
                 │
    ┌────────────┼────────────────┐
    │            │                │
┌───▼───┐  ┌────▼────┐  ┌────────▼────────┐
│ Graph │  │  Geo    │  │  Analytics      │
│  DB   │  │  DB     │  │  (BigQuery)     │
│Spanner│  │PostGIS/ │  │  Trade flows    │
│ Graph │  │Cloud SQL│  │  Time series    │
│Supply │  │Objects  │  │  Aggregates     │
│Chains │  │Routes   │  └────────┬────────┘
└───────┘  └─────────┘           │
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

ВНЕШНИЕ ИСТОЧНИКИ:
UN Comtrade API → Trade Adapter
USGS MRDS API  → Minerals Adapter  
ACLED API      → Conflict Adapter
GDELT BigQuery → Events Adapter
OFAC/EU Lists  → Sanctions Adapter
OSM Overpass   → Geo Adapter
IEA Data       → Energy Adapter
GLEIF API      → Entity Adapter

3.2 DATA MODEL: ГРАФ ЦЕПОЧЕК ПОСТАВОК

NODES (вершины):

Material {
  id: string (CAS number + HS code composite)
  name: string
  aliases: string[]
  category: enum (REE | BATTERY | SEMICONDUCTOR | ENERGY | AGRO | MEDICAL | GAS)
  criticalityScore: float (0-10)
  riskScore: float (0-10, AI-generated)
  hsCodes: string[]
  casNumber: string
}

Facility {
  id: string (GLEIF LEI | OSM ID | USGS ID)
  name: string
  type: enum (MINE | REFINERY | PROCESSING | FACTORY | PORT | STORAGE)
  country: string (ISO3)
  coordinates: Point (WGS84)
  operators: string[] (LEI references)
  capacity: float
  capacityUnit: string
  productionMaterials: Material[]
  riskScore: float (AI-generated)
  sanctioned: boolean
}

Route {
  id: string
  type: enum (SEA | LAND | AIR | PIPELINE | RAIL)
  geometry: LineString (GeoJSON)
  fromFacility: Facility.id
  toFacility: Facility.id
  vesselTypes: string[]  // bulk carrier, tanker, etc.
  typicalVolume: float
  volumeUnit: string
  riskScore: float
}

ConflictZone {
  id: string
  name: string
  geometry: Polygon (GeoJSON)
  severity: enum (LOW | MEDIUM | HIGH | CRITICAL)
  type: enum (ARMED | SANCTIONS | NATURAL | POLITICAL)
  affectedMaterials: Material[]
  source: string (ACLED)
  updatedAt: timestamp
}

EDGES (рёбра):

ProcessingStep {
  from: Material.id | Facility.id
  to: Material.id | Facility.id
  type: enum (EXTRACTION | REFINING | PROCESSING | MANUFACTURING | TRANSPORT)
  yieldRatio: float  // сколько output на единицу input
  keyPlayers: Facility[]
  geographicConcentration: float  // HHI index
  riskMultiplier: float
}

Dependency {
  from: Material.id
  to: Material.id
  type: enum (DERIVED | COMPONENT | SUBSTITUTE | COMPETING)
  strength: float  // 0-1
}

3.3 DATA PIPELINE: 6-ЧАСОВОЙ ЦИКЛ

Cloud Scheduler (каждые 6 часов):
  ├── Trigger: Pub/Sub topic "pipeline-trigger"
  │
  ├── Worker 1: Trade Data Fetcher
  │   └── UN Comtrade API → BigQuery raw_trades
  │       (месячные данные, лаг ~2 мес, но новые данные при появлении)
  │
  ├── Worker 2: Minerals & Facilities Fetcher  
  │   └── USGS MRDS → Spanner Graph (facilities) + BigQuery
  │
  ├── Worker 3: Conflict & Events Fetcher
  │   └── ACLED API → Spanner Graph (conflict_zones)
  │   └── GDELT BigQuery public dataset → events table
  │
  ├── Worker 4: Sanctions Fetcher
  │   └── OFAC SDN XML → entity_sanctions table
  │   └── EU Consolidated List CSV → entity_sanctions table
  │
  ├── Worker 5: Geo Enrichment
  │   └── OSM Overpass → Facility coordinates update
  │   └── Natural Earth routes → Route geometries
  │
  └── Worker 6: AI Risk Scoring
      └── Vertex AI Gemini → Batch risk scores
          Input: entity data + recent events + sanctions
          Output: risk_scores table (BigQuery)
          Storage: Risk scores cached 6 hours in Memorystore (Redis)

SOURCE ADAPTER PATTERN (расширяемость):
Каждый источник реализует интерфейс:

type DataSourceAdapter interface {
  Name() string
  Fetch(ctx context.Context, since time.Time) ([]RawRecord, error)
  Normalize(raw RawRecord) (CanonicalRecord, error)
  Schema() SourceSchema
  RateLimit() RateLimitConfig
}

Добавление нового источника = реализация интерфейса + регистрация в registry.

3.4 AI ИНТЕГРАЦИЯ (VERTEX AI / GEMINI)

Risk Scoring Pipeline:
  Input контекст для каждого объекта:
  - Геолокация + страна
  - Исторические данные производства
  - Текущие конфликты в регионе (ACLED)
  - Санкционный статус (OFAC/EU)
  - Зависимость от других критических материалов
  - Последние новостные события (GDELT)

  Gemini prompt: структурированный JSON output с объяснением
  Grounding: Google Search для верификации актуальности
  Human review queue: AI scores с confidence < 0.7 → flagged для review

Search Grounding для Q&A:
  Пользователь спрашивает: "Как повлияет эскалация в Южно-Китайском море на цепочки редкоземельных металлов?"
  → RAG: граф цепочек + Gemini Search Grounding
  → Structured response: affected materials + affected routes + risk delta estimate

3.5 ГЕОДАННЫЕ: ВЫБОР ХРАНИЛИЩА

Решение: ГИБРИДНЫЙ ПОДХОД

Cloud SQL (PostgreSQL + PostGIS):
  - Точечные объекты (facilities): precise lat/lng, complex queries
  - Маршруты (routes): LineString геометрии
  - Конфликтные зоны: Polygon геометрии
  - Пространственные индексы GIST
  - Хорошо для: spatial queries, nearby search, polygon intersection

BigQuery GIS:
  - Аналитика: какие маршруты пересекают зоны конфликтов
  - Агрегации: объём торговли по регионам
  - Хорошо для: большие объёмы, сложные аналитические запросы

Memorystore (Redis):
  - Кэш: результаты пространственных запросов
  - Кэш: MapLibre tile данные
  - TTL: 6 часов (синхронизирован с pipeline)

════════════════════════════════════════════════════════════════
РАЗДЕЛ 4: UX И ВИЗУАЛИЗАЦИЯ
════════════════════════════════════════════════════════════════

4.1 КОНЦЕПЦИЯ: INVESTIGATION TOOL, НЕ DASHBOARD

Для данного продукта нужен investigation tool (инструмент исследования), а не статичный dashboard. Аналитик приходит с вопросом ("что происходит с цепочкой лития?") и системе нужно дать возможность исследовать, drill down, выдвигать гипотезы.

Модель взаимодействия: Progressive Disclosure
Уровень 1: Global overview — карта мира с риск-хeatmap, топ-алерты
Уровень 2: Material focus — выбор материала → его цепочка на карте + граф переделов
Уровень 3: Facility detail — клик по объекту → полная карточка, риски, связи
Уровень 4: Route analysis — конкретный маршрут → исторические данные, альтернативы

4.2 КОМПОНЕНТЫ ИНТЕРФЕЙСА

ГЛАВНЫЙ ЭКРАН (3-панельный layout):
┌──────────────────────────────────────────────────────────────┐
│ [HEADER] CriticalChain | Material Search | Alerts (3) | User │
├────────────┬─────────────────────────────────┬───────────────┤
│            │                                 │               │
│ LEFT PANEL │    CENTRAL MAP (MapLibre)        │ RIGHT PANEL  │
│            │                                 │               │
│ Materials  │  [Layers control]               │ Chain graph   │
│ ──────     │  [Risk heatmap overlay]         │ (D3 force/    │
│ • Lithium  │  [Conflict zones overlay]       │  Cytoscape)   │
│ • Cobalt   │  [Routes layer]                 │               │
│ • Nickel   │  [Facilities dots]              │ Risk score    │
│            │                                 │ panel         │
│ Filters    │                                 │               │
│ ──────     │                                 │ AI Insights   │
│ Region     │                                 │ (Gemini)      │
│ Risk level │                                 │               │
│ Category   │                                 │               │
└────────────┴─────────────────────────────────┴───────────────┘

4.3 MAPLIBR СЛОИ (рекомендации)

Слой 1 (base): Topo/political basemap (MapTiler или стандартный MapLibre Style)
Слой 2: Risk heatmap — polygon fill, интенсивность = risk score
  → Источник: ConflictZone + country risk polygons
  → Цвет: gradient green → yellow → orange → red
Слой 3: Conflict zones — semi-transparent polygons с border
  → Tooltip: название конфликта, тип, severity, affected materials
Слой 4: Trade routes — animated arc lines (flow lines)
  → Толщина линии = объём торговли
  → Цвет = риск маршрута
  → Анимация: пульсирующие точки показывают направление flow
Слой 5: Facilities — кластеризованные точки
  → Иконки по типу (mine ⛏, factory 🏭, port ⚓)
  → Цвет точки = risk score объекта
  → Клик → sidebar с деталями
Слой 6: Selected material highlight — highlight всех объектов в цепочке
  → При выборе материала — все связанные facilities подсвечиваются
  → Маршруты между ними — активный слой

Технические детали MapLibre:
• Использовать MapLibre GL JS v4+
• Векторные тайлы для facilities (PMTiles или Mapbox protocol)
• deck.gl для arc/flow визуализации поверх MapLibre
• WebGL-based heatmap для performance
• Cluster: supercluster для facilities > 500 объектов

4.4 ГРАФ ПЕРЕДЕЛОВ (Chain Visualization)

Отдельный компонент — не карта, а граф:
• Библиотека: Cytoscape.js или D3-force
• Layout: directed acyclic graph (DAG), left-to-right
• Ноды: материалы (круги) и объекты (прямоугольники)
• Рёбра: подписаны типом передела
• Цвет нодов: risk score
• Интерактивность: hover → tooltip, click → drill-down

Пример: Цепочка лития
[Li Ore] → [Li2CO3] → [Cathode NMC] → [Battery Cell] → [Battery Pack] → [EV]
  ▲            ▲           ▲                ▲               ▲             ▲
 РИСК:8      РИСК:9     РИСК:7           РИСК:6           РИСК:4       РИСК:3
(Китай)   (Китай 60%) (Китай 70%)    (CATL monopoly)    (OEM)

4.5 AI-FIRST UX ПАТТЕРНЫ 2025-2026

Contextual AI Panel:
  → Постоянно доступный Gemini chat в правой панели
  → Контекст = текущий выбранный материал/маршрут/регион
  → Пример: "Объясни, почему риск-скор неодима вырос с 6 до 8 за последний месяц"
  → Ответ: структурированный с источниками (Search Grounding)

Proactive Alerts:
  → AI автоматически генерирует alerts при аномалиях
  → "Конфликт в провинции Катанга (ДРК) → 3 кобальтовых шахты в риск-зоне → цепочка батарей Tesla под угрозой"
  → Severity + explanation + suggested actions

Smart Search:
  → Semantic search по материалам, объектам, регионам
  → Natural language: "покажи все цепочки, зависящие от российских поставок"

4.6 МОБАЙЛ

Рекомендация: PWA (Progressive Web App), не нативное приложение.

Mobile layout:
  → Full-screen карта как primary view
  → Bottom sheet для panels (material list, alerts)
  → Упрощённый граф переделов (горизонтальная прокрутка)
  → Push notifications через Web Push API

Нативное приложение: нет необходимости в MVP. Рассмотреть в Phase 3, если появится consumer demand.

════════════════════════════════════════════════════════════════
РАЗДЕЛ 5: РИСКИ ПРОДУКТА
════════════════════════════════════════════════════════════════

5.1 ЮРИДИЧЕСКИЕ РИСКИ

Data Licensing:
• UN Comtrade: коммерческое использование данных требует проверки TOS. API для коммерческих продуктов — платный tier.
• ACLED: обязательная коммерческая лицензия для SaaS продуктов (~ $5-15K/год)
• OSM: ODbL требует share-alike для derivative databases. Осторожно с derived geo products.
• USGS: Public Domain (безопасно).
• MarineTraffic: строго коммерческая лицензия; даже historical routes — платно.
Митигация: Юридический аудит каждого источника ПЕРЕД запуском. Бюджет на лицензии источников: $20-50K/год.

Re-export Restrictions:
• Данные о военных объектах, оборонных производствах — могут подпадать под EAR (Export Administration Regulations)
• Данные о ядерных материалах — требуют особой осторожности
Митигация: Исключить из MVP данные о военных и ядерных объектах. Консультация с export control юристом.

GDPR / CCPA:
• Данные об организациях (LEI, адреса, ownership) — применимо GDPR если есть PII
• Cookies и user analytics — стандартная compliance
Митигация: Privacy by design; DPA (Data Processing Agreement) с клиентами.

EU Critical Raw Materials Act:
• Создаёт новые compliance требования для European buyers
• Возможность позиционировать продукт как compliance tool (плюс!)

5.2 ГЕОПОЛИТИЧЕСКИЕ РИСКИ

Чувствительность данных:
• Точные данные о местоположении критических объектов (шахты, рафинировочные заводы) могут интересовать государственных акторов
• Данные о санкционных обходах — потенциально sensitive
• Mapping военных supply chains — прямо запрещено к публикации в ряде юрисдикций

Китай:
• ~70-80% переработки большинства critical minerals — в Китае
• Публикация данных о китайских объектах может создать юридические риски при работе с китайскими клиентами
• Доступ к продукту из Китая — потенциально проблематичен

Санкции:
• Нельзя предоставлять доступ к продукту пользователям из SDN-листа
• Данные о российских и белорусских объектах — требуют disclaimer

Митигация: Terms of Service с явными ограничениями; geo-blocking для sanctioned territories; legal entity screening для enterprise клиентов.

5.3 ТЕХНИЧЕСКИЕ РИСКИ

Data Freshness:
• UN Comtrade лаг: 2-18 месяцев. Реальных "свежих" trade данных нет.
• Решение: чётко показывать дату последнего обновления каждого data point; не создавать у пользователя иллюзию real-time.

Source Reliability:
• Данные о производстве из развивающихся стран (ДРК, Боливия) — часто неточные или отсутствуют
• Решение: confidence scores для каждого data point; transparency о пробелах.

AI Hallucination в Risk Scoring:
• Gemini может генерировать правдоподобные, но неточные risk assessments
• Особенно опасно для investment decisions
• Решение: 
  a) Mandatory disclaimer: AI scores — не investment advice
  b) Human review queue для score changes > 2 points
  c) Fact-checking через Search Grounding
  d) Audit trail: что именно повлияло на score

Graph Complexity:
• Глобальный граф всех critical materials может достичь миллионов нодов
• Решение: материализованные sub-graphs по материалу; ленивая загрузка; pagination

5.4 БИЗНЕСОВЫЕ РИСКИ

Кто покупатель:
• ICP 1: Strategic Intelligence команды — Fortune 500 automotive, aerospace, pharma, electronics
• ICP 2: Government agencies — Dept. of Commerce, EU Commission, BRICS equivalents
• ICP 3: Commodity trading firms и хедж-фонды
• ICP 4: ESG/sustainability research organizations
• ICP 5: Consulting firms (McKinsey, BCG, Kearney)

GTM:
• Прямые продажи для enterprise (ACV $50-200K/год)
• Partnership с Big 4 consulting для distribution
• Government contracts — требуют FedRAMP compliance (США)
• Freemium tier для brand awareness (ограниченные данные)

Monetization:
• Tier 1 (Free): Публичные данные, 3 материала, без alerts — $0
• Tier 2 (Professional): All materials, 5 users, alerts, export — $2-5K/мес
• Tier 3 (Enterprise): Unlimited users, API access, custom integrations, SLA — $10-30K/мес
• Tier 4 (Government): On-premise option, FedRAMP, classified data integration — custom

Риск: Конкуренты имеют многолетнюю фору и большие клиентские базы. Путь к рынку через niche specialization (critical materials specifically) — единственный реалистичный.

════════════════════════════════════════════════════════════════
РАЗДЕЛ 6: ROADMAP
════════════════════════════════════════════════════════════════

6.1 MVP (МЕСЯЦЫ 1-4)

ФОКУС: Доказательство ценности для первых клиентов. 5 материалов, базовая карта, реальные данные.

Месяц 1 — Фундамент:
• GCP инфраструктура (Cloud Run, Cloud SQL PostGIS, BigQuery, Pub/Sub)
• Go API: базовые CRUD endpoints для materials, facilities, routes
• Data adapters: USGS MRDS + UN Comtrade (основные данные)
• React/Vite/TypeScript скелет с MapLibre базовой картой
• Firebase Auth (Google SSO)
• CI/CD: Cloud Build → Cloud Run

Месяц 2 — Данные и граф:
• Spanner Graph или Neo4j: базовый граф для 5 материалов (Li, Co, REE, He, Si)
• Supply chain для 5 материалов: ручная верификация данных
• Data pipeline: ACLED + OFAC + GDELT adapters
• Конфликтные зоны на карте
• Базовый risk scoring (rule-based, без AI пока)

Месяц 3 — UI и AI:
• Полноценный 3-панельный layout
• Граф переделов (Cytoscape.js)
• MapLibre: facilities, routes, heatmap
• Vertex AI: первый Gemini risk scoring pipeline
• Базовые алерты (email)
• Export: CSV, JSON

Месяц 4 — Polish и запуск:
• User roles (Admin/Analyst/Viewer)
• Демо режим с публичным доступом
• Performance optimization
• Documentation
• Security audit
• Beta клиенты: 2-3 пилотных клиента

MVP НЕ включает: мобайл PWA, внешний API, i18n, advanced AI Q&A, Government tier

6.2 PHASE 2 (МЕСЯЦЫ 5-8)

• 20+ материалов в системе
• AI Q&A (RAG + Gemini Search Grounding)
• REST API + API keys management
• Advanced alerting (Slack/Teams/webhooks)
• Исторические данные (2 года)
• PWA для мобайл
• i18n EN + ZH
• PDF отчёты

6.3 PHASE 3 (МЕСЯЦЫ 9-12)

• 50+ материалов (все USGS Critical Minerals)
• Multi-tenant SaaS с billing
• GraphQL API
• Custom data integrations для enterprise
• FedRAMP assessment (для US Government)
• Scenario modeling: "что если Китай закроет экспорт gallium?"
• Supply chain stress testing / simulation

6.4 FULL PRODUCT (ГОД 2+)

• Predictive analytics: early warning system
• Alternative supply chains: AI-generated recommendations
• Portfolio risk: корзина материалов, portfolio exposure
• Private data integration: клиент загружает свои supplier данные
• Marketplace: аналитические отчёты от партнёров
• API ecosystem: SDK, webhooks, Zapier integration
• White-label опция

6.5 ЧТО АВТОМАТИЗИРОВАТЬ СРАЗУ:

✓ Data pipeline (Scheduler → Workers) — с первого дня
✓ Sanctions checking — с первого дня  
✓ ACLED conflict updates — с первого дня
✓ CI/CD (Cloud Build) — с первого дня
✓ AI risk scoring — с месяца 3

ЧТО АВТОМАТИЗИРОВАТЬ ПОТОМ:

○ User onboarding (пока — мануальный)
○ Billing и subscription management (Stripe — Phase 2)
○ Advanced alerting rules engine (Phase 2)
○ Report generation (Phase 2)

════════════════════════════════════════════════════════════════
ПРИЛОЖЕНИЕ А: СПИСОК ИСТОЧНИКОВ ДАННЫХ С ОЦЕНКАМИ
════════════════════════════════════════════════════════════════

#  | Источник              | Покрытие | Качество | Лицензия        | Приоритет | Стоимость
───┼───────────────────────┼──────────┼──────────┼─────────────────┼───────────┼──────────
1  | USGS MRDS             | Глоб.    | ★★★★★    | Public Domain   | MVP       | Бесплатно
2  | UN Comtrade+          | Глоб.    | ★★★★☆    | API платный     | MVP       | ~$5K/год
3  | ACLED                 | 250 стр. | ★★★★★    | Коммерческая    | MVP       | ~$10K/год
4  | OFAC SDN List         | США-SDN  | ★★★★★    | Public Domain   | MVP       | Бесплатно
5  | EU Sanctions List     | EU       | ★★★★★    | Open            | MVP       | Бесплатно
6  | GLEIF LEI             | Глоб.    | ★★★★☆    | CC0             | MVP       | Бесплатно
7  | GDELT (BigQuery)      | Глоб.    | ★★★☆☆    | Public Domain   | MVP       | GCP costs
8  | OpenStreetMap         | Глоб.    | ★★★☆☆    | ODbL (осторожно)| MVP       | Бесплатно
9  | IEA Data Explorer     | Energy   | ★★★★☆    | Attribution     | Phase 2   | Бесплатно
10 | BACI (CEPII)          | Глоб.    | ★★★★☆    | Research/Comm.  | Phase 2   | ~$3K/год
11 | OECD TiVA             | 76 стр.  | ★★★★☆    | Open            | Phase 2   | Бесплатно
12 | Natural Earth         | Глоб.    | ★★★★☆    | Public Domain   | MVP       | Бесплатно
13 | Global Forest Watch   | Глоб.    | ★★★☆☆    | CC BY           | Phase 2   | Бесплатно
14 | HDX / OCHA            | Гум.     | ★★★★☆    | Various CC      | Phase 2   | Бесплатно
15 | FAO STAT              | Агро     | ★★★★☆    | Open            | Phase 2   | Бесплатно
16 | World Bank Open Data  | Macro    | ★★★★☆    | CC BY 4.0       | Phase 2   | Бесплатно

════════════════════════════════════════════════════════════════
ПРИЛОЖЕНИЕ Б: ТЕХНИЧЕСКИЕ ЗАВИСИМОСТИ И ВЕРСИИ
════════════════════════════════════════════════════════════════

Backend (Go):
• Go 1.22+
• Gin или Chi (HTTP router)
• pgx v5 (PostgreSQL/PostGIS driver)
• cloud.google.com/go/spanner
• cloud.google.com/go/bigquery
• cloud.google.com/go/vertexai
• google.golang.org/protobuf

Frontend:
• React 19+
• TypeScript 5.4+
• Vite 5+
• MapLibre GL JS 4+
• deck.gl 9+ (arc layers)
• Cytoscape.js (supply chain graph)
• TanStack Query v5 (data fetching)
• Zustand (state management)
• Recharts (time series)
• Tailwind CSS v4

GCP Services:
• Cloud Run (API + Workers)
• Cloud SQL (PostgreSQL 16 + PostGIS 3.4)
• Cloud Spanner (Graph — preview feature)
• BigQuery (analytics + GDELT)
• Pub/Sub (pipeline orchestration)
• Memorystore Redis (caching)
• Cloud Scheduler (6-hour trigger)
• Vertex AI (Gemini Pro)
• Firebase Authentication
• Cloud CDN (static assets)
• Cloud Load Balancing
• Secret Manager (API keys)
• Cloud Build (CI/CD)

════════════════════════════════════════════════════════════════

© 2026 CriticalChain. Whitepaper v1.0.
Документ подготовлен как часть архитектурного pre-development исследования.



════════════════════════════════════════════════════════════════
ДОПОЛНЕНИЕ К WHITEPAPER — ПРАВКИ И РАСШИРЕНИЯ v2.0
════════════════════════════════════════════════════════════════

Дата обновления: Март 2026
Статус: Официальное дополнение к основному документу


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 1. ИСТОЧНИКИ ДАННЫХ О КОНФЛИКТАХ И СОБЫТИЯХ (КОСВЕННЫЕ)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1.1 Принцип: ACLED и GDELT исключены из MVP

ACLED (Armed Conflict Location & Event Data Project) и GDELT исключены из MVP по экономическим соображениям: стоимость $10,000/год неприемлема на нулевой выручке. Вместо них используется многоуровневая система косвенных сигналов + Gemini Search Grounding. Принцип: freshness важнее precision на MVP. Достаточно достоверных сигналов из открытых источников лучше, чем идеальные данные за деньги.

1.2 Уровни источников (от реального времени к историческим)

УРОВЕНЬ 0 — GEMINI SEARCH GROUNDING (основной агрегатор)
Инструмент: Google Gemini API с функцией Search Grounding
Что даёт: синтез новостей в реальном времени по любому запросу ("конфликт в регионе X", "забастовка на руднике Y", "санкции против Z"). Gemini сам агрегирует Reuters, AP, BBC, Al Jazeera, локальные медиа.
Freshness: < 1 часа
Coverage: глобальная
Стоимость: включена в Gemini API (pay-per-token)
Ограничение: нет структурированных данных, только синтезированный текст — нужен парсинг через промпт

УРОВЕНЬ 1 — ОФИЦИАЛЬНЫЕ TRAVEL ADVISORIES ПРАВИТЕЛЬСТВ

US State Department (travel.state.gov)
  - RSS Feed: https://travel.state.gov/content/travel/en/traveladvisories/traveladvisories.html.rss
  - API: api.state.gov (устаревший, 2013, но работает)
  - Формат: JSON/XML, страновые уровни 1-4
  - Freshness: обновляется при изменении ситуации, обычно 24-72 ч
  - Coverage: ~200 стран
  - Лицензия: Public Domain (US Gov)
  - Парсинг: простой RSS → country_code + advisory_level + summary

UK FCDO (gov.uk/foreign-travel-advice)
  - API: https://www.gov.uk/api/content/foreign-travel-advice/{country-slug}
  - Формат: JSON (GOV.UK Content API)
  - Freshness: 24-48 ч при изменениях
  - Coverage: ~220 стран
  - Лицензия: Open Government Licence v3.0
  - Парсинг: GET /api/content/foreign-travel-advice/congo → поле "details.parts[*].body"

Australian DFAT (smartraveller.gov.au)
  - RSS: https://www.smartraveller.gov.au/destinations/rss
  - Freshness: 24-72 ч
  - Coverage: ~170 стран
  - Лицензия: Creative Commons Attribution

Агрегация: US Dept + FCDO + DFAT дают трёхточечный консенсус для страновой риск-оценки. Если два из трёх повысили уровень — сигнал достоверен.

УРОВЕНЬ 2 — МОРСКИЕ И АВИАЦИОННЫЕ ПРЕДУПРЕЖДЕНИЯ

AIS (Automatic Identification System) — движение судов
  - aisstream.io: бесплатный WebSocket API, реальное время, глобальный
  - AISHub: бесплатно при шеринге собственного фида
  - MarineCadastre.gov (NOAA): исторические треки, GeoParquet формат
  - Сигнал конфликта: "AIS blackout" в регионе (суда отключают транспондеры) = прокси угрозы
  - Пример: исчезновение трафика в Баб-эль-Мандебе в 2024 → сигнал атак хуситов за несколько часов до медийного освещения
  - Парсинг: bbox-фильтр по региону → подсчёт active vessels → отклонение от baseline = риск-сигнал
  - Limitation: в горячих зонах суда намеренно глушат AIS — интерпретировать silence как risk, не как отсутствие трафика

NAVTEX (Navigational Telex) — морские предупреждения
  - navtex.net: бесплатный агрегатор сообщений
  - VesselAPI NAVTEX: REST API, обновление каждые 6 ч
  - NGA Maritime Safety API: https://msi.nga.mil/api/publications/query → категории Navigational Warnings
  - Содержит: зоны минирования, запрещённые проходы, учения, бандитская активность
  - Лицензия: Public Domain (US Gov, NGA)

NOTAM (Notice to Airmen) — авиационные запреты
  - FAA API: api.faa.gov/notamSearch (бесплатно, регистрация обязательна, 30 req/min)
  - NASA NOTAM redistribution: DIP catalog, GeoJSON формат
  - Ключевые сигналы: запрет полётов над регионом = активный конфликт или ракетная угроза
  - Парсинг: фильтр по тексту PROHIB/RESTRICT + координаты → region_risk_flag

УРОВЕНЬ 3 — ЭКОНОМИЧЕСКИЕ ПРОКСИ РИСКОВ

Baltic Dry Index (BDI) — фрахт сухого балка
  - Trading Economics: исторические данные, CSV скачивание
  - Investing.com: исторические данные, неофициальный scraping
  - Freightos Baltic Index (FBX): контейнерный фрахт, бесплатный terminal portal
  - Интерпретация: резкий рост BDI/FBX по маршруту → дефицит пропускной способности = конфликт или ЧП
  - Freshness: ежедневно

Страховые индексы Lloyds / War Risk Premiums
  - Источник: Lloyds Market Association (LMA) публикует war risk зоны
  - Прокси: рост war risk premium на маршрут = рынок видит угрозу
  - Парсинг: нет прямого API, мониторинг через news + Gemini Grounding

УРОВЕНЬ 4 — МЕДИА И ОСИНТ

Reuters / AP RSS-ленты
  - Reuters: http://feeds.reuters.com/Reuters/worldNews (бесплатно)
  - AP: https://rsshub.app/apnews/topics/world-news (через RSSHub)
  - Wikipedia Current Events: https://en.wikipedia.org/wiki/Portal:Current_events (scraping / RSS)
  - Freshness: 15-60 мин
  - Лицензия: для MVP допустимо как информационный агрегатор (не перепродажа)

Global Threat Map (OSINT GitHub)
  - unicodeveloper/globalthreatmap: открытый проект, API поверх OSINT
  - Не production-ready, но полезен для прототипа

1.3 Стратегия обработки сигналов (Signal Fusion)

На MVP: все источники → Gemini API (синтез) → structured risk signal
Формат выхода:
{
  "region": "Red Sea",
  "risk_level": 4,
  "confidence": "medium",
  "signals": ["AIS blackout detected", "FCDO raised to Level 3", "NAVTEX warning active"],
  "freshness": "2026-03-19T06:00Z",
  "source_count": 3
}

Правило MVP: если ≥2 независимых источника подтверждают угрозу в регионе → risk_level повышается. Один источник → флаг "unconfirmed".


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 2. CANONICAL PATH — КОНЦЕПЦИЯ И ФОРМАТ
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

2.1 Переход от Canonical ID к Canonical Path

Термин "Canonical ID" заменяется на "Canonical Path". Это не просто уникальный идентификатор сущности, а явный путь в иерархическом графе цепочки поставок. Путь несёт структурную семантику: он показывает, где сущность находится в системе, как она связана с другими узлами, и как к ней прийти из корня иерархии.

Аналогия: как файловый путь /usr/local/bin/python описывает не только имя файла, но и его положение в файловой системе — Canonical Path описывает узел в supply chain graph через его положение в иерархии переделов, географии и цепочек трансформации.

2.2 Структура Canonical Path

Формат: {material}/{source_type}/{geography}/{stage}/{entity}

Сегменты:
  material    — тип сырья (lithium, cobalt, ree, helium, silicon, crude_oil, natural_gas)
  source_type — происхождение (mine, well, field, refinery, processor, trader, port, end_user)
  geography   — иерархия: {continent}/{country}/{region}/{city_or_site}
  stage       — стадия передела (0=extraction, 1=processing, 2=refining, 3=precursor, 4=component, 5=product)
  entity      — нормализованное название сущности (slug, lowercase, underscores)

2.3 Примеры Canonical Path

Литиевый рудник в Чили:
  lithium/mine/americas/chile/atacama/stage0/sociedad_quimica_y_minera

Завод по переработке кобальта в Китае:
  cobalt/refinery/asia/china/zhejiang/stage2/huayou_cobalt

Порт отгрузки REE из Мьянмы:
  ree/port/asia/myanmar/mandalay_region/stage1/muse_border_crossing

Газовое месторождение в Катаре:
  natural_gas/field/middle_east/qatar/north_field/stage0/qatar_energy_nf

Нефтяной терминал в Роттердаме:
  crude_oil/port/europe/netherlands/rotterdam/stage2/vopak_maasvlakte

LFP-катод (промежуточный передел лития):
  lithium/processor/asia/china/hunan/stage3/btr_new_energy_lfp

2.4 Как строится Canonical Path

Алгоритм построения:
1. Нормализовать название сущности (alias resolution): "SQM", "Sociedad Química y Minera", "SQM Chile" → sociedad_quimica_y_minera
2. Определить материал через entity_material_map (словарь: компания → материалы)
3. Геолокировать через GeoNames или собственный справочник → {continent}/{country}/{region}
4. Определить стадию передела через production_stage_classifier (по типу деятельности)
5. Выбрать source_type из фиксированного словаря

Alias Resolution: каждый Canonical Path хранит массив known_aliases — все вариации написания из разных источников. При ingestion нового datapoint — fuzzy match по aliases → assign canonical path.

2.5 Использование Canonical Path для сопоставления данных

Сопоставление cross-source: данные из EIA, Reuters, FCDO, AIS — все маппятся на единый граф через Canonical Path. Запись из Reuters "SQM приостановила добычу" → resolve "SQM" → canonical path lithium/mine/.../sociedad_quimica_y_minera → прикрепить событие к узлу графа.

Граф строится как направленный граф (DAG): узлы = Canonical Paths, рёбра = физические потоки материала. Путь от корня (добыча) до листа (конечный потребитель) — это полная цепочка поставок. Canonical Path позволяет однозначно определить позицию любого узла в этом DAG без знания всего графа заранее.

Версионирование: если компания меняет владельца или перепрофилируется — старый path помечается deprecated, создаётся новый. История сохраняется для аналитики.


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 3. AI RISK SCORING — ДИСКЛАЙМЕР MVP
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

3.1 Позиция продукта на MVP

AI-скоринг рисков на MVP является экспериментальной функцией. Все риск-оценки генерируются языковой моделью на основе агрегированных открытых источников и не являются профессиональными финансовыми, инвестиционными или аналитическими заключениями.

3.2 Текст дисклаймера (обязателен в UI)

ОСНОВНОЙ ДИСКЛАЙМЕР (показывается при первом входе + в настройках):

"Оценки рисков, отображаемые в этом приложении, формируются автоматически на основе открытых источников с использованием искусственного интеллекта. Они носят информационный, а не рекомендательный характер. Данные могут быть неполными, устаревшими или ошибочными. Не используйте эти данные как единственный источник для принятия коммерческих, инвестиционных или операционных решений. Разработчик не несёт ответственности за убытки, возникшие вследствие использования этих оценок."

INLINE ДИСКЛАЙМЕР (рядом с каждым risk score):
"⚠ AI-оценка. Экспериментально. Не является профессиональным заключением."

3.3 Где показывать

- Onboarding: полный текст, требует явного accept (чекбокс)
- Dashboard: постоянная иконка "ℹ" рядом с заголовком Risk Score
- Risk badge: tooltip с коротким дисклаймером при hover/tap
- Export/Report: footer с полным текстом дисклаймера
- API response: поле "disclaimer" в каждом ответе с risk_score

3.4 Планируемое снятие ограничений

Дисклаймер снимается или смягчается когда:
- Проведена верификация модели на исторических данных (backtesting)
- Внедрена модель ансамблирования нескольких источников
- Привлечён domain expert для валидации алгоритма
- Получена юридическая консультация по ответственности в целевых юрисдикциях


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 4. РАСШИРЕНИЕ MVP: НЕФТЬ И ГАЗ
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

4.1 Обновлённый список материалов MVP

Полный список на MVP v1:
  - Литий (Li) — батерейная цепочка, EV
  - Кобальт (Co) — катоды, аэрокосмос
  - Редкоземельные элементы (REE) — магниты, электроника
  - Гелий (He) — полупроводники, МРТ, криогеника
  - Кремний (Si) — солнечная энергетика, полупроводники
  → ДОБАВЛЕНО: Нефть (Crude Oil) — транспорт, нефтехимия
  → ДОБАВЛЕНО: Природный газ (Natural Gas) — энергетика, удобрения

4.2 Отличие нефти и газа от других материалов

Нефть и газ — принципиально другая data landscape:

ОБЪЁМ ДАННЫХ: На порядки больше источников. Нефть — самый мониторируемый товар в мире.
ЦЕНООБРАЗОВАНИЕ: Рыночные цены (Brent, WTI, Henry Hub, TTF) обновляются в реальном времени, в отличие от лития и кобальта где цены частично непрозрачны.
ИНФРАСТРУКТУРА: Pipeline networks, LNG terminals, tanker fleets — всё это публично отслеживается через AIS, FERC, ENTSO-G.
РЕГУЛЯТОРЫ-ИСТОЧНИКИ: EIA, IEA, OPEC, JODI — публикуют детальную статистику ежемесячно/еженедельно.

4.3 Специфические источники данных для нефти и газа

EIA (U.S. Energy Information Administration)
  - API: api.eia.gov (бесплатно, API key по регистрации)
  - Данные: добыча по странам, запасы, импорт/экспорт, цены, прогнозы
  - Freshness: weekly (запасы), monthly (международная статистика)
  - Coverage: США детально + международные данные
  - Лицензия: Public Domain
  - Ключевые серии: PET.RWTC.W (WTI price), NG.RNGWHHD.D (Henry Hub)

IEA (International Energy Agency)
  - Данные: Oil Market Report (OMR), Gas Market Report — ежемесячно
  - Free tier: агрегированные данные, WEO datasets (CC BY-NC-SA)
  - API: только для enterprise лицензий — на MVP использовать free downloads
  - Coverage: 38 стран ОЭСР + ключевые производители

OPEC
  - Annual Statistical Bulletin: скачивается бесплатно (PDF/Excel)
  - Monthly Oil Market Report: краткое резюме бесплатно, данные платно
  - На MVP: использовать Gemini Grounding для извлечения цифр из публичных отчётов

JODI (Joint Organisations Data Initiative)
  - URL: jodidata.org
  - Данные: нефть и газ по ~100 странам, унифицированный формат
  - Лицензия: бесплатно для non-commercial
  - Freshness: с задержкой ~2 месяца — не для real-time, но для базовых объёмов

Platts / Argus Media
  - Коммерческие, не для MVP. Gemini Grounding может синтезировать публикуемые ценовые индикаторы из открытых источников.

ENTSO-G (European gas network)
  - API: transparency.entsog.eu (бесплатно)
  - Данные: потоки газа по европейским pipeline в реальном времени
  - Ключевой сигнал: снижение потоков через критические точки (Суджа, Nord Stream коридоры) = геополитический риск

4.4 Граф переделов для нефти и газа

Нефть:
  stage0: Скважина (upstream) → stage1: Сепарация/стабилизация → stage2: Трубопровод/танкер → stage3: НПЗ (refining) → stage4: Нефтепродукты (diesel, jet fuel, naphtha) → stage5: Конечный потребитель

Газ:
  stage0: Скважина/месторождение → stage1: Подготовка газа (сепарация, осушка) → stage2: Pipeline / СПГ-завод → stage3: Регазификационный терминал → stage4: Распределительная сеть → stage5: Конечный потребитель

Ключевые точки концентрации риска:
  - Strait of Hormuz: 20% мировой нефти и 17% СПГ
  - Bab-el-Mandeb: маршрут для 9% мировой нефти
  - Турецкие проливы: российская нефть в Черном море
  - Суэцкий канал: критический узел для нефтяных танкеров

4.5 Переосмысление MVP scope с нефтью и газом

Добавление нефти и газа РАСШИРЯЕТ аудиторию MVP:
  - Нефтетрейдеры и commodity desks банков
  - Энергетические компании (downstream риски)
  - Правительственные energy security офисы
  - Страховые компании (marine, energy risk)

Но требует отдельного UI-слоя: для нефти и газа критичны price charts + flow maps (ENTSO-G стиль), которых нет в базовом supply chain view. Рекомендуется: материалы группировать по типу — "Critical Minerals" vs "Energy Commodities" — с разными дашбордами но единым графом и единой архитектурой данных.


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 5. UX 2026 — ПРИНЦИПЫ ИНТЕРФЕЙСА
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

5.1 Философия: Аналитический инструмент, не приложение

Этот продукт не конкурирует с потребительскими приложениями. Он конкурирует с Excel, Bloomberg Terminal, и custom Python дашбордами аналитиков. Пользователь платит за информацию, а не за красивые анимации. Каждый пиксель должен нести данные или структуру, помогающую данные воспринимать.

Принцип: плотность информации > визуальная лёгкость. Никаких hero sections, никакого onboarding с иллюстрациями, никаких "пустых состояний" с котятами.

5.2 Визуальный язык

ТИПОГРАФИКА:
  - Основной шрифт: JetBrains Mono (monospace) для числовых данных, метрик, кодов
  - Текстовый шрифт: Inter или IBM Plex Sans — нейтральный, высокая читаемость при малом размере
  - Размерная шкала: жёсткая — 11px / 13px / 15px / 19px / 24px. Ничего промежуточного.
  - Никакого letter-spacing для красоты, только функциональный

ЦВЕТОВАЯ СХЕМА:
  Основа: тёмный фон (не чёрный — #0D1117 GitHub Dark или #111318)
  Текст: #E6EDF3 основной, #8B949E вторичный, #484F58 disabled
  Риск-палитра (строго семантическая):
    risk_critical: #FF4444 (красный, только для уровня 5)
    risk_high:     #FF8C00 (оранжевый, уровень 4)
    risk_medium:   #FFD700 (жёлтый, уровень 3)
    risk_low:      #00C853 (зелёный, уровень 1-2)
    risk_unknown:  #484F58 (серый, нет данных)
  Акцент: #4FC3F7 (голубой) — только для интерактивных элементов
  Линии/рамки: #21262D
  НЕ использовать: градиенты, тени, border-radius > 4px

5.3 Компоненты

RISK BADGE:
  Формат: [●] CRITICAL  или  [●] 4.2 / 5
  Реализация: цветная точка + текст уровня + числовой скор (если есть)
  Размер: 12px точка + 11px текст, всё в одну строку
  Hover tooltip: "Sources: FCDO L3, AIS blackout, NAVTEX active — Updated 2h ago"
  Никаких progress bars, ring charts, или animated пульсирующих кружков

SUPPLY CHAIN GRAPH:
  Тип: Directed Acyclic Graph (DAG), горизонтальный layout (слева = добыча, справа = потребитель)
  Узлы: прямоугольники с Canonical Path label + risk badge + объём (если доступен)
  Рёбра: направленные линии, толщина = объём потока (нормализованный), цвет = риск ребра
  Интерактивность: click на узел → expand/collapse дочерние узлы (drill-down в граф)
  Zoom: mouse wheel / pinch, pan: drag
  Рендеринг: D3.js или Cytoscape.js, SVG (не Canvas для accessibility)
  Референс: Kpler flow diagrams — компактность и плотность; НЕ как у Palantir Gotham (слишком декоративно)

DRILL-DOWN МЕХАНИКА:
  Уровень 1 (Global): карта мира + список топ-5 рисков по материалу
  Уровень 2 (Region): регион на карте + узлы supply chain в регионе
  Уровень 3 (Node): карточка узла: Canonical Path, все источники данных, история риск-уровня, related events
  Уровень 4 (Event): конкретное событие: источник, текст, дата, impact на узлы
  Навигация: breadcrumb bar сверху, back button, URL reflects drill-down state (deep links)

ТАБЛИЦЫ ДАННЫХ:
  Стиль: плотные (row height 28px), zebra striping (#0D1117 / #161B22)
  Сортировка: по клику на заголовок, стрелка показывает направление
  Фильтрация: inline filter row под заголовком, не модальные панели
  Pagination: 50/100/200 строк, не infinite scroll (аналитик должен знать где он)
  Референс: Linear table view — минимализм и быстродействие

КАРТА:
  Тип: тёмная базовая карта (MapBox Dark или самохостируемый OpenMapTiles)
  Слои: 1) риск по странам (choropleth), 2) потоки материалов (animated arcs), 3) ключевые узлы (точки)
  Никаких декоративных элементов: только данные + минимальные подписи стран
  Choropleth: та же риск-палитра что и risk badge — семантическое единство

5.4 Референсы: что брать, что не брать

Bloomberg Terminal — БРАТЬ: плотность данных, монохромная основа с цветовыми акцентами только для данных, keyboard-first навигация. НЕ БРАТЬ: устаревший 80-х UX, перегруженность виджетами без иерархии.

Kpler — БРАТЬ: flow diagrams для commodities, geografическая визуализация потоков, чёткая сегментация по материалам. НЕ БРАТЬ: некоторые декоративные элементы в маркетинговых материалах.

Linear — БРАТЬ: скорость, keyboard shortcuts, minimal chrome, мгновенный отклик UI, система меток (labels = наш risk badge). НЕ БРАТЬ: командный workflow не релевантен для нашего продукта.

Resilio / Notion — НЕ БРАТЬ НИЧЕГО. Это consumer/productivity инструменты, не аналитические.

5.5 Мобильная версия

Философия: мобайл = read-only alert system, не full dashboard.

Что есть на мобайле:
  - Push-уведомления при изменении риск-уровня для отслеживаемых узлов
  - Список активных алертов с risk badge и кратким описанием
  - Детальная карточка события (tap → level 3 drill-down в упрощённом виде)
  - Базовая карта с риск-heatmap (без интерактивного графа)

Что НЕТ на мобайле:
  - Редактирование настроек мониторинга
  - Полный supply chain graph
  - Таблицы данных
  - Экспорт отчётов

Технически: мобайл = отдельное React Native приложение или PWA с ограниченным набором экранов. Не адаптивная версия веб-приложения — это принципиально разные UX.


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
РАЗДЕЛ 6. ПРИНЦИПЫ УНИВЕРСАЛЬНОЙ АРХИТЕКТУРЫ
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

6.1 Принцип: новый материал = только данные, не код

Система должна быть спроектирована так, чтобы добавление нового материала (например, вольфрам, марганец, платина) требовало только заполнения конфигурационных данных — без изменения кода приложения, без деплоя, без участия разработчика.

Тест на универсальность: если добавление вольфрама требует хотя бы одного if/switch/hardcode в коде — архитектура нарушена.

6.2 Универсальная модель данных для материала

MaterialDefinition (YAML/JSON конфиг, хранится в data/materials/):

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
  conversion_factors:
    lb: 0.000453592
    short_ton: 0.907185
    kg: 0.001

production_stages:
  - id: 0
    name: mining
    type: extraction
    typical_forms: [wolframite_ore, scheelite_ore]
  - id: 1
    name: concentration
    type: processing
    typical_forms: [tungsten_concentrate]
    conversion_ratio: 0.65  # выход продукта из предыдущей стадии
  - id: 2
    name: apt_production
    name_full: Ammonium Paratungstate
    type: chemical_processing
    typical_forms: [APT]
    conversion_ratio: 0.85
  - id: 3
    name: oxide_metal
    type: refining
    typical_forms: [tungsten_oxide, tungsten_metal_powder]
  - id: 4
    name: fabrication
    type: manufacturing
    typical_forms: [tungsten_carbide, tungsten_wire, tungsten_alloy]
  - id: 5
    name: end_product
    type: consumption
    typical_forms: [cutting_tools, armor_piercing_rounds, filaments]

data_sources:
  - source_id: usgs_mcs
    type: annual_statistics
    url: https://pubs.usgs.gov/periodicals/mcs2024/mcs2024-tungsten.pdf
    fields: [production_by_country, reserves, price]
  - source_id: ita_tungsten
    type: trade_data
    url: https://www.itia.info/statistics.html
    fields: [trade_flows, consumption]

key_chokepoints:
  - path: critical_mineral/mine/asia/china/jiangxi/stage0
    share_of_global_supply: 0.82
    risk_multiplier: 2.5

concentration_hhi: 8900  # Herfindahl-Hirschman Index — степень монополизации

price_series:
  unit: USD/metric_ton
  frequency: monthly
  sources: [metal_bulletin, fastmarkets]

6.3 Универсальная модель маршрута

RouteDefinition — описывает физический путь материала между узлами:

route_id: "lithium/mine/americas/chile/atacama/.../stage0→stage3/china/..."
material: lithium
waypoints:
  - node_path: lithium/mine/americas/chile/atacama/stage0/sqm
    role: origin
  - node_path: lithium/port/americas/chile/antofagasta/stage1/puerto_antofagasta
    role: transit
    transport_mode: truck
    distance_km: 300
    typical_days: 2
  - node_path: lithium/port/asia/china/guangdong/stage2/guangzhou_nansha
    role: transit
    transport_mode: ocean_vessel
    distance_km: 18000
    typical_days: 35
  - node_path: lithium/processor/asia/china/jiangxi/stage3/ganfeng_lithium
    role: destination
    transport_mode: rail
    distance_km: 800
    typical_days: 3

risk_factors:
  - factor_id: strait_hormuz
    applies_to_legs: [1]  # индекс waypoint-перехода
    risk_type: geopolitical_chokepoint
    base_weight: 0.3
  - factor_id: chile_political
    applies_to_nodes: [lithium/mine/americas/chile/...]
    risk_type: country_risk
    weight_from_country_score: true

aggregation_rule: max_weighted  # max | mean | weighted_sum | max_weighted

6.4 Универсальный граф переделов

TransformationGraph — описывает как материал трансформируется между стадиями:

material: cobalt
transformations:
  - from_stage: 0  # добыча
    to_stage: 1    # переработка концентрата
    process_name: froth_flotation
    typical_yield: 0.70
    co2_kg_per_ton: 2.1
    water_m3_per_ton: 15
    typical_countries: [DRC, Zambia, Australia]
  - from_stage: 1
    to_stage: 2
    process_name: hydrometallurgical_refining
    typical_yield: 0.92
    typical_countries: [China, Finland, Belgium]
  - from_stage: 2
    to_stage: 3
    process_name: cathode_precursor_synthesis  # pCAM
    typical_yield: 0.88
    typical_countries: [China, South Korea]

Граф — это конфигурация, не код. Движок читает конфиг и строит DAG автоматически. Добавление нового передела = добавить объект в массив transformations, не писать класс.

6.5 Универсальные правила risk scoring

RiskScoringConfig — применяется к любому материалу:

scoring_version: "2.0"
algorithm: weighted_signal_fusion

signals:
  - id: country_risk
    source: travel_advisories  # us_state_dept + uk_fcdo + au_dfat
    weight: 0.25
    scale: [1, 5]
    aggregation: max_of_three_sources
  - id: conflict_events
    source: gemini_grounding + ais_blackout + notam
    weight: 0.30
    scale: [1, 5]
    decay_halflife_hours: 72  # событие теряет вес через 72ч если не подтверждено
  - id: supply_concentration
    source: hhi_index  # из MaterialDefinition
    weight: 0.20
    scale: [1, 5]
    formula: "min(5, hhi / 2000)"
  - id: price_volatility
    source: price_series
    weight: 0.15
    scale: [1, 5]
    formula: "min(5, 30d_volatility_pct / 10)"
  - id: freight_stress
    source: bdi_or_fbx
    weight: 0.10
    scale: [1, 5]
    formula: "min(5, (current - 90d_avg) / 90d_avg * 10)"

node_risk_formula: "sum(signal.value * signal.weight) for signal in signals"
route_risk_formula: "max(node_risk for node in route.waypoints) * route.chokepoint_multiplier"
material_risk_formula: "weighted_avg(route_risk for route in material.top_routes, weight=route.volume_share)"

Правила не зависят от материала. При добавлении вольфрама — те же сигналы, тот же алгоритм. Меняется только то, откуда берутся данные (price_series, hhi_index, key_chokepoints из MaterialDefinition).

6.6 Параметризованные визуализации и карта

Визуализации НЕ знают о конкретных материалах. Они получают конфиг:

MapLayerConfig:
  material: tungsten  # или "all"
  layer_type: choropleth
  data_field: country_risk_score
  color_scale: risk_palette  # ссылка на глобальную палитру
  tooltip_fields: [country_name, risk_score, top_signals, last_updated]

GraphConfig:
  material: tungsten
  max_depth: 4  # сколько уровней переделов показывать
  node_label: canonical_path_short  # или display_name
  edge_weight: volume_share  # толщина ребра = объём
  color_by: node_risk_score

Компонент SupplyChainGraph принимает GraphConfig и данные — и рисует граф. Добавление нового материала не требует изменения компонента — только новых данных в формате GraphConfig + nodes + edges.

6.7 Добавление нового материала: пошаговый процесс (без разработчика)

Шаг 1: Создать файл data/materials/tungsten.yaml (заполнить по шаблону MaterialDefinition)
Шаг 2: Загрузить список производителей в data/entities/tungsten_entities.csv (company, country, stage, capacity)
Шаг 3: Запустить скрипт: npm run ingest:material tungsten (парсит данные, строит canonical paths, создаёт граф)
Шаг 4: Настроить data_sources в конфиге (ссылки на открытые источники данных)
Шаг 5: Запустить: npm run score:material tungsten (первый проход risk scoring)
Шаг 6: Материал появляется в интерфейсе автоматически

Всё. Разработчик не нужен.

═══════════════════════════════════════════════════════════════
КОНЕЦ ДОПОЛНЕНИЯ v2.0
═══════════════════════════════════════════════════════════════

