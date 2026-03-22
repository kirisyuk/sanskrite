# AGENTS.md

Этот файл задаёт обязательный входной контекст для агентов, работающих в этом репозитории.

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

1. [/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/supply-chain-canonical-control-plane.md](/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/supply-chain-canonical-control-plane.md)
2. [/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md](/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_NAMES_AND_PATHS.md)
3. [/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md](/Users/kirisyuk/cursor/sanskrite/docs/canonical_names_and_paths/PRODUCT_CANONICAL_TRANSFORMS.md)

## Обязательная модель мышления

Для supply-chain работы агент должен считать истинным следующее:

- Продуктовая идентичность определяется canonical product path, а не сырым названием.
- Product synonym dictionary и product transform dictionary — это разные слои и их нельзя смешивать.
- Если продукт известен в synonym dictionary, нужно использовать canonical path.
- Если product-to-product pair известна в transform dictionary, нужно использовать canonical relation и `relationKind`.
- Если новый продукт или новая product pair не найдены в словарях, их нельзя молча терять или плодить ad hoc словари. Их нужно писать в candidate collections.

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

## Где это уже встроено в runtime

Для текущего состояния системы агент должен знать, что canonical control plane уже подключён к:

- раннему product resolve
- fragment create/update
- product-to-product edge create/update
- materialized artifact create/update

Поэтому при изменениях в supply-chain ядре сначала нужно проверять, не ломают ли они этот слой.
