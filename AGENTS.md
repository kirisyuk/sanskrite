# AGENTS.md

Этот файл задаёт обязательный входной контекст для агентов, работающих в этом репозитории.

Если явно заданы User Rules или системные инструкции среды и они расходятся с формулировками ниже, **приоритет у User Rules / system**; этот файл дополняет их, а не отменяет.

## Horizon / Supply Chain: читать в первую очередь

Если задача касается хотя бы одного из пунктов:
- products
- product synonyms
- product transforms
- `TRANSFORMS_INTO`
- `ASSEMBLES_INTO`
- `DEPENDS_ON` между продуктами
- `SUPPLY_LINK`
- materialized supply-chain artifacts
- paths / links / legs
- pathbuilder

то перед работой с кодом агент должен сначала прочитать:

1. [supply-chain-canonical-control-plane.md](docs/horizon/architecture/supply-chain-canonical-control-plane.md)
2. [PRODUCT_CANONICAL_NAMES_AND_PATHS.md](docs/canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md)
3. [PRODUCT_CANONICAL_TRANSFORMS.md](docs/canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md)

## Taxonomy system of record

AH-таксономия **генерируется** — не правьте сгенерированные привязки вручную.

- **Источник правды:** `agents/shared/agenthorde_common/agenthorde_common/taxonomy.py` (Python) и зеркало `shared/taxonomy/taxonomy.json`.
- **Go:** `backend/internal/taxonomy/ah/taxonomy_generated.go` — регенерация из каталога `backend`: `go generate ./internal/taxonomy/ah/...` (см. `//go:generate` в `backend/internal/taxonomy/ah/doc.go`).
- **TypeScript:** `web/src/lib/taxonomy.generated.ts` — `cd web && npm run gen:taxonomy`; проверка свежести: `npm run gen:taxonomy:check`.
- Пайплайн и CI-гарды описаны в [`docs/taxonomy/CODEGEN.md`](docs/taxonomy/CODEGEN.md).

### Обязательные правила для таксономии

- **Никогда** не редактируйте `taxonomy_generated.go` и `taxonomy.generated.ts` вручную — меняйте источник и регенерируйте.
- **Никогда** не вводите новые wire-токены в UPPER_SNAKE: wire-язык сейчас AH snake_case (`ah.RelationName`, `ah.FragmentClass` + `Subtype`).
- UPPER_SNAKE допустим только в двух carve-out доменах: граница LLM-промптов (см. [`docs/agents/agenthorde-runtimes.md`](docs/agents/agenthorde-runtimes.md)) и runtime-ontology (`backend/internal/taxonomy/*`, Firestore-словари). При сомнении — спросите.
- Завершённая миграция P0–P14 и исторические карты лежат в [`docs/archive/taxonomy-migration-2026/`](docs/archive/taxonomy-migration-2026/README.md); это **не** оперативный SoT для рантайма.

## Обязательная модель мышления

Для supply-chain работы агент должен считать истинным следующее:

- Продуктовая идентичность определяется canonical product path, а не сырым названием.
- Product synonym dictionary и product transform dictionary — это разные слои и их нельзя смешивать.
- Если продукт известен в synonym dictionary, нужно использовать canonical path.
- Если product-to-product pair известна в transform dictionary, нужно использовать canonical relation и `relationKind`.
- Если новый продукт или новая product pair не найдены в словарях, их нельзя молча терять или плодить ad hoc словари. Их нужно писать в candidate collections.

## AgentHorde: агент vs канонизатор

- **Агент** — исследование и фиксация фактов в prose + **реестр сущностей** (`proposedEntities`: стабильный `id` + дословный `label` из своего же текста) + **связи по id** (`proposedRelations`: `srcId`/`dstId` только из этого реестра). Он не задаёт онтологический тип сущности и не эмитит внутренние коды режимов каталога — только читаемые имена.
- **Канонизатор** — приземление на системные сущности (NER/типизация, canonical paths, дедуп по jurisdiction gate, словари). Контракт стабильных id → mention → fragment обеспечивает симметрию с агентом без «третьего написания» endpoint'а.

Канонизатор и общие схемы — пакет `agenthorde_common` (`agents/shared/agenthorde_common/`); Vertex (ADK) и Azure (OpenAI) — отдельные рантаймы. См. [agenthorde-runtimes.md](docs/agents/agenthorde-runtimes.md).

## Чего нельзя делать

Агент не должен:

- вводить новый параллельный словарь product identity мимо canonical control plane
- заново локально решать synonym/transform задачу там, где runtime уже умеет спросить словарь
- считать raw product label источником правды, если доступен canonical path
- смешивать:
  - synonym equivalence
  - transformation
  - assembly
  - generic dependency
- вводить локальные alias-таблицы или legacy-shim слои в application-коде; если нужен перевод wire-формы, он допускается только в явных Category B (промпты / разбор LLM-JSON) или Category C (`internal/taxonomy/*`), а не новыми скрытыми словарями в сервисах.

## Где это уже встроено в runtime

Для текущего состояния системы агент должен знать, что canonical control plane уже подключён к:

- раннему product resolve
- fragment create/update
- product-to-product edge create/update
- materialized artifact create/update

Поэтому при изменениях в supply-chain ядре сначала нужно проверять, не ломают ли они этот слой.

## Роль и рабочий режим (Composer ближе к Opus)

Цель этого раздела: в среде **Composer** держать **тот же класс привычек**, что у сильных моделей вроде Opus — глубина разбора, самостоятельность, аккуратные диффы и самопроверка — без «болтовни ради болтовни» и без раздувания объёма правок.

### Мышление и автономность

- **Строгость и самопроверка:** не менять код вслепую; перед отчётом «готово» прогнать код глазами на импорты, типы, гонки, очевидные edge cases и побочные эффекты. По возможности прогнать релевантные тесты/сборку, если это уместно по задаче.
- **Сбор контекста:** если чего-то не хватает, доставать самому через поиск по репозиторию и чтение соседнего кода, а не ждать пошаговых указаний.
- **Хирургичность:** менять ровно то, что нужно для задачи; избегать массовых правок «заодно» и перезаписи файлов целиком ради одной строки.

### Цикл выполнения

На нетривиальную задачу имеет смысл явно пройти:

1. **Разобрать** релевантные файлы и зависимости.
2. **Спланировать** кратко: что меняется, что может затронуть соседние модули.
3. **Сделать** изменения.
4. **Проверить:** линтер/типы/тесты по зоне изменений, явные регрессии.

### Качество кода

- Читаемость и модульность важнее хитрости; дублирование убирать там, где это уже принято в проекте.
- Типизация максимально строгая; `any` и отключение линтеров — только если пользователь явно попросил или это уже принято в конкретном месте кодовой базы.
- Ошибки и краевые случаи не глотаются: осмысленные ошибки, логи или явная деградация по контракту модуля.
- Для мок-данных, меток в логах и комментариев с датами в этом репозитории используйте формат **`MM-YYYY`**, если не оговорено иначе.

### Коммуникация

- **Плотно и по делу:** без длинных приветствий, извинений и фраз вроде «конечно, помогу»; сразу суть, выводы и что сделано.
- **Не путать краткость с расплывчатостью:** если нужен контекст или trade-off, дать его структурированно нормальным языком (как в хорошем техтексте), а не только обрывками.
- **Рассуждение:** длинный сырой chain-of-thought наружу не выносить; в ответе оставлять релевантные выводы и аргументы, а не поток внутренних шагов.

### UI / визуальный контекст

Если даны макет или скриншот, выравнивать вёрстку с опорой на существующую дизайн-систему проекта (Tailwind, MUI и т.д.): сетка, отступы, типографика и иерархия должны совпадать с референсом настолько, насколько это требует задача.
