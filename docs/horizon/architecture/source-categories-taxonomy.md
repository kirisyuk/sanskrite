# Таксономия категорий источников новостей (RSS) — v1

## Цель

Этот документ фиксирует практичную таксономию для `sources` в формате:

- `sourceCategoryPaths: ["/source/{category}/{subcategory}"]`
- `primarySourceCategoryPath: "/source/{category}/{subcategory}"`
- `sourceGeoFacets: ["{iso2|token}"]`
- `sourceFormatFacets: ["{format}"]`

Подход: **L1 (category) закрытый**, **L2+ (subcategory) открытый**, но с рекомендуемым словарем.

## Канонический формат

- Путь: `/source/{category}/{subcategory}[/{topic}...]`
- Сегменты: `lowercase-slug` (`[a-z0-9-]`)
- Если подкатегория не указана: использовать `general`
- Для одного источника обычно достаточно **одной primary-категории**
- Дополнительные категории использовать только если источник действительно мульти-доменный

## Фасеты (обязательно отдельным полем)

### Geo facet

- Поле: `sourceGeoFacets: []string`
- Формат:
  - ISO2 (`us`, `de`, `ua`, `sa`, ...)
  - глобальные токены: `global`, `int`
  - региональные/блоковые токены: `eu`, `asean`, `brics`, `nato`, `g7`, `g20`, `oecd`, `opec`, `apec`, `mercosur`, `ecowas`, `sadc`, `eaeu`, `csto`, `sco`, `rcep`, `cptpp`, `bimstec` и др.
  - международные организации: `un`, `unsc`, `who`, `iaea`, `unhcr`, `imf`, `world-bank`, `wto`, `osce`

Важно: география хранится как facet, а не в path.  
То есть использовать `/source/{category}/...` + `sourceGeoFacets`, а не `/source/{iso2}/{category}/...`.

### Format facet

- Поле: `sourceFormatFacets: []string`
- Рекомендуемый словарь:
  - `wire`, `mainstream`, `gov`, `regulator`, `intl-org`
  - `think-tank`, `research`, `osint`
  - `market-data`, `industry-media`, `tech-media`
  - `blog`, `newsletter`, `podcast`, `aggregator`, `forum`, `social`, `official-release`

## Правило выбора primary

`primary` должна отражать **редакционную специализацию самого источника**, а не случайную тему отдельной новости.

Примеры:
- сайт регулятора рынков капитала: primary `/source/policy/regulation`, а не `/source/markets/equities`
- отраслевое медиа про полупроводники: primary `/source/technology/semiconductors`
- новостной сайт Минздрава: primary `/source/health/public-health`

## L1 категории (закрытый список)

Ниже список категорий, который стоит держать фиксированным.

| L1 category | Что покрывает | Рекомендуемые L2 подкатегории |
|---|---|---|
| `agriculture` | сельское хозяйство и продбез | `crops`, `livestock`, `fertilizers`, `agtech`, `food-security` |
| `climate` | климатические риски и климат-повестка | `adaptation`, `mitigation`, `emissions`, `climate-risk`, `climate-policy` |
| `compliance` | комплаенс-практики и контроль соответствия | `aml-kyc`, `sanctions-compliance`, `export-control`, `privacy`, `esg-reporting` |
| `cyber` | киберугрозы и кибербезопасность | `threat-intel`, `vulnerabilities`, `incident-response`, `ransomware`, `cyber-policy` |
| `defense` | оборона и ВПК | `procurement`, `military-tech`, `dual-use`, `defense-industry`, `operations` |
| `economy` | макроэкономика и экономполитика | `macro`, `inflation`, `labor`, `trade-balance`, `development` |
| `energy` | энергетика | `oil-gas`, `power`, `renewables`, `grid`, `nuclear`, `storage` |
| `environment` | экология и природные системы | `pollution`, `water`, `waste`, `biodiversity`, `conservation` |
| `finance` | финансовая система и институты | `banking`, `payments`, `insurance`, `asset-management`, `private-equity` |
| `geopolitics` | международные отношения и конфликты | `diplomacy`, `sanctions`, `conflict`, `alliances`, `intelligence` |
| `health` | здравоохранение как система | `public-health`, `healthcare-systems`, `epidemiology`, `health-policy`, `healthcare-finance` |
| `infrastructure` | базовая и цифровая инфраструктура | `construction`, `utilities`, `telecom`, `digital-infrastructure`, `pipelines` |
| `legal` | право и судебная практика | `legislation`, `litigation`, `court-decisions`, `antitrust`, `ip` |
| `logistics` | цепочки поставок и логоперации | `shipping`, `ports`, `warehousing`, `customs`, `last-mile` |
| `markets` | рыночные площадки и инструменты | `commodities`, `equities`, `fixed-income`, `fx`, `derivatives` |
| `medicine` | клиническая медицина и фарма/биотех | `pharma`, `biotech`, `medical-devices`, `clinical-trials`, `approvals` |
| `policy` | государственная политика и регулирование | `regulation`, `public-policy`, `public-procurement`, `standards`, `enforcement` |
| `science` | научные исследования | `physics`, `chemistry`, `materials`, `earth-science`, `space` |
| `social` | общественные процессы | `demography`, `migration`, `education`, `civil-society`, `labor-rights` |
| `technology` | технологии и ИТ-индустрия | `ai`, `semiconductors`, `cloud`, `platforms`, `robotics` |
| `transport` | транспортные системы | `aviation`, `rail`, `maritime`, `road`, `urban-mobility` |

## Практические правила для RSS-источников

1. Ставить одну primary-категорию по умолчанию.
2. Добавлять вторую категорию только если это постоянный cross-domain фокус источника.
3. Не подменять категорию по единичному инфоповоду.
4. Для узко-ведомственных источников брать институциональный домен:
   - регулятор/министерство: чаще `policy`, `legal`, `compliance`
   - научный журнал: `science`
   - индустриальное медиа: профильная отрасль (`energy`, `technology`, `transport` и т.д.)
5. Если сомнение между `policy` и отраслью:
   - источник описывает решения/правила государства -> `policy/*`
   - источник описывает рынок/операции/компании -> отраслевая категория

## Примеры назначения

- Всемирная организация здравоохранения (RSS): `/source/health/public-health`
- FDA newsroom: `/source/medicine/approvals`
- SEC newsroom: `/source/policy/regulation`
- IEA news: `/source/energy/power` или `/source/energy/oil-gas` (по профилю ленты)
- Maritime industry news: `/source/transport/maritime` или `/source/logistics/shipping` (что является core фокусом редакции)
- Semiconductor trade media: `/source/technology/semiconductors`

## Эволюция таксономии

- L1 менять редко и только через явное архитектурное решение.
- L2 расширять можно быстро, если появляется новый устойчивый класс источников.
- L3 использовать для более точной специализации (например, `/source/technology/ai/llm`).
- При добавлении новых L1 нужно синхронизировать:
  - рантайм-валидацию backend (`internal/taxonomy/source_categories.go`)
  - этот документ.
