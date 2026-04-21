# CriticalChain — Источники данных и ключи API

## 1. UN Comtrade+ (торговые потоки)

| Параметр | Значение |
|----------|----------|
| **URL регистрации** | https://comtradedeveloper.un.org/ |
| **API Explorer** | https://comtradeplus.un.org/APIPortal/APIExplorer |
| **Документация ключей** | https://uncomtrade.org/docs/api-subscription-keys/ |
| **Env переменная** | `UN_COMTRADE_KEY` или `COMTRADE_SUBSCRIPTION_KEY` |
| **Примечание** | Бесплатный tier: `comtrade - v1`. Premium (~$5K/год) — bulk downloads. Ключ в Azure API Management. |

---

## 2. ENTSO-G (европейский газ)

| Параметр | Значение |
|----------|----------|
| **Платформа** | https://transparency.entsog.eu/ |
| **API документация** | https://transparency.entsog.eu/content/static_content/Static%20content/web%20api/Guide.html |
| **API Manual PDF** | https://transparency.entsog.eu/api/archiveDirectories/8/api-manual/TP_REG715_Documentation_TP_API%20-%20v2.1.pdf |
| **Базовый URL API** | `https://transparency.entsog.eu/api/v1/operationaldatas` |
| **Ключ** | Публичный API, **без ключа** (rate limits по IP) |
| **Примечание** | ENTSO-E (электричество) — https://transparency.entsoe.eu/ требует регистрации. ENTSO-G (газ) — публичный. |

---

## 3. JODI (нефть и газ, статистика)

| Параметр | Значение |
|----------|----------|
| **Сайт** | https://www.jodidata.org/ |
| **Data Downloads** | https://www.jodidata.org/gas/database/data-downloads.aspx |
| **Формат** | CSV, IVT (Beyond 20/20) — **ручная загрузка**, API нет |
| **Подписка на новости** | https://www.jodidata.org/sign-up.aspx |
| **Примечание** | Официального API нет. Использовать парсинг CSV или интеграцию через jodidb.org. |

---

## 4. Country Risk (State Dept, FCDO, DFAT)

### US State Department

| Параметр | Значение |
|----------|----------|
| **API (без ключа)** | https://cadataapi.state.gov/ |
| **Endpoints** | `/api/TravelAdvisories`, `/api/CountryTravelInformation` |
| **RSS** | https://travel.state.gov/_res/rss/TAsTWs.xml |
| **Ключ** | **Не требуется** |

### FCDO (UK)

| Параметр | Значение |
|----------|----------|
| **Atom feed** | https://www.gov.uk/foreign-travel-advice.atom |
| **Data.gov.uk** | https://data.gov.uk/dataset/fco-online-travel-advice |
| **Ключ** | **Не требуется** |

### DFAT (Australia)

| Параметр | Значение |
|----------|----------|
| **Smartraveller** | https://www.smartraveller.gov.au/ |
| **RSS** | https://www.smartraveller.gov.au/rss.xml (если есть) |
| **Примечание** | Официального API нет. Использовать парсинг HTML или community API (smartraveller.api.kevle.xyz). |

---

## 5. GDELT (события)

| Параметр | Значение |
|----------|----------|
| **BigQuery dataset** | `gdelt-bq.gdeltv2` (или `gdelt-bq.gdeltv2.events`) |
| **Документация** | https://blog.gdeltproject.org/gdelt-worlds-largest-event-dataset-now-publicly-available-in-google-bigquery/ |
| **Ключ** | **Не требуется** — BigQuery public dataset. Оплата только за запросы (1 TB/мес бесплатно). |
| **GCP** | Нужен проект GCP с включённым BigQuery. |

### Настройка GDELT в Horizon

1. **BigQuery API** — включён в проекте `prevalent-horizon-test`.
2. **GCP_PROJECT_ID** — задаётся при деплое Cloud Run (`scripts/deploy`). Fallback: `GOOGLE_CLOUD_PROJECT` (автоматически в Cloud Run).
3. **Проверка доступа:**
   ```bash
   bq query --project_id=prevalent-horizon-test --use_legacy_sql=false \
     "SELECT COUNT(*) FROM \`gdelt-bq.gdeltv2.events\` WHERE SQLDATE >= 20260301 LIMIT 1"
   ```

---

## 6. EU Consolidated List (санкции)

| Параметр | Значение |
|----------|----------|
| **Data.europa.eu** | https://data.europa.eu/data/datasets/consolidated-list-of-persons-groups-and-entities-subject-to-eu-financial-sanctions |
| **EC Sanctions** | https://ec.europa.eu/external_relations/cfsp/sanctions/list/version4/global/help_online/process_data_structure.html |
| **Формат** | XML (GLOBAL.xml, DELTA.xml) |
| **Ключ** | **Не требуется** — публичные данные. |

---

## 7. Geo Enrichment

### OSM Overpass

| Параметр | Значение |
|----------|----------|
| **Endpoint** | https://overpass-api.de/api/interpreter |
| **Документация** | https://wiki.openstreetmap.org/wiki/Overpass_API |
| **Ключ** | **Не требуется** для обычных запросов. API key только для `out attribution` / user metadata. |

### Natural Earth

| Параметр | Значение |
|----------|----------|
| **Сайт** | https://www.naturalearthdata.com/downloads/ |
| **Формат** | SHP, GeoJSON, SQLite — статическая загрузка |
| **Ключ** | **Не требуется** — Public Domain. |

### GLEIF (LEI)

| Параметр | Значение |
|----------|----------|
| **API** | https://api.gleif.org/ |
| **Документация** | https://www.gleif.org/en/lei-data/gleif-api |
| **Golden Copy** | https://www.gleif.org/en/lei-data/gleif-golden-copy/download-the-golden-copy |
| **Ключ** | **Не требуется** для базового доступа. Rate limits применяются. |

---

## 8. OFAC SDN (уже реализован)

| Параметр | Значение |
|----------|----------|
| **XML** | https://www.treasury.gov/ofac/downloads/sdn.xml |
| **Ключ** | **Не требуется** |

---

## 9. EIA (уже реализован)

| Параметр | Значение |
|----------|----------|
| **Регистрация** | https://www.eia.gov/opendata/register.php |
| **API** | https://api.eia.gov/ |
| **Env переменная** | `EIA_API_KEY` или Secret Manager `eia_api_key` |
| **Примечание** | Единственный источник с обязательным ключом. Остальные — публичные. |

---

## 10. USGS MRDS (уже реализован)

| Параметр | Значение |
|----------|----------|
| **Данные** | Public Domain, API/CSV |
| **Ключ** | **Не требуется** |

---

## Сводка по ключам

| Источник | Ключ нужен | Где взять |
|----------|------------|-----------|
| UN Comtrade | Да | https://comtradedeveloper.un.org/ |
| ENTSO-G | Нет | — |
| JODI | Нет (API нет) | CSV download |
| State Dept | Нет | — |
| FCDO | Нет | — |
| DFAT | Нет | — |
| GDELT | Нет (BigQuery) | GCP проект |
| EU Sanctions | Нет | — |
| OSM Overpass | Нет | — |
| Natural Earth | Нет | — |
| GLEIF | Нет | — |
| EIA | Да | https://www.eia.gov/opendata/register.php |
| OFAC | Нет | — |
| USGS | Нет | — |
