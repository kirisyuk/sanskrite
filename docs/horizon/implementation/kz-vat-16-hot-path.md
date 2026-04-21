# Kazakhstan VAT 16% Hot Path

Этот runbook фиксирует reusable hot-path для сценария `повышение НДС до 16% в Казахстане` в рамках taxonomy-first ядра Horizon.

## Цель

Поднять не пустой гео-якорь `/loc/kz`, а минимально достаточную регуляторную цепочку `authority -> decision -> mechanism -> program -> exposed markets/community`, чтобы симуляция шла не только от абстрактного сигнала, а от реального policy-impact графа.

## Canonical Nodes

- `/loc/kz`
- `/gov/kz/ministry-of-finance`
- `/gov/kz/state-revenue-committee`
- `/decision/taxation/raise-vat-to-16pct-kazakhstan-2026`
- `/mechanism/regulatory/vat-rate-increase`
- `/program/kz/vat-reform-2026`
- `/market/kz/consumer/food`
- `/market/kz/consumer/retail`
- `/market/kz/industrial/import-distribution`
- `/market/kz/finance/working-capital`
- `/community/kz/trade/bazaar-traders`

Все пути валидны по canonical taxonomy:

- `gov`: `/gov/{geo}/{slug}`
- `decision`: `/decision/{domain}/{slug}`
- `mechanism`: `/mechanism/{category}/{slug}`
- `program`: `/program/{jurisdiction}/{slug}`
- `market`: `/market/{geo}/{domain}[/{vertical}...]`
- `community`: `/community/{geo}/{type}/{slug}`

## Canonical Edges

Hot-path специально ограничен allowlist `policy_impact`:

- `ADMINISTERS`
- `GATEKEEPS_DECISION`
- `USES_MECHANISM`
- `APPLIES_TO`
- `IMPOSES_CONDITION`
- `INFLUENCES`
- `REGULATES`
- `PARTICIPATES_IN`
- `OPERATES_IN`

Ключевая цепочка:

1. `/gov/kz/ministry-of-finance ADMINISTERS /gov/kz/state-revenue-committee`
2. `/gov/kz/ministry-of-finance GATEKEEPS_DECISION /decision/taxation/raise-vat-to-16pct-kazakhstan-2026`
3. `/decision/taxation/raise-vat-to-16pct-kazakhstan-2026 USES_MECHANISM /mechanism/regulatory/vat-rate-increase`
4. `/gov/kz/ministry-of-finance ADMINISTERS /program/kz/vat-reform-2026`
5. `/gov/kz/state-revenue-committee ADMINISTERS /program/kz/vat-reform-2026`
6. `/program/kz/vat-reform-2026 IMPOSES_CONDITION /market/kz/consumer/food`
7. `/program/kz/vat-reform-2026 IMPOSES_CONDITION /market/kz/consumer/retail`
8. `/mechanism/regulatory/vat-rate-increase APPLIES_TO /market/kz/industrial/import-distribution`
9. `/mechanism/regulatory/vat-rate-increase APPLIES_TO /market/kz/finance/working-capital`
10. `/program/kz/vat-reform-2026 INFLUENCES /community/kz/trade/bazaar-traders`
11. `/gov/kz/state-revenue-committee REGULATES /market/kz/consumer/retail`
12. `/gov/kz/state-revenue-committee REGULATES /market/kz/industrial/import-distribution`
13. `/community/kz/trade/bazaar-traders PARTICIPATES_IN /market/kz/consumer/retail`

## Qualifiers and Metrics

На hot-path добавляются именно те qualifiers, которые policy-impact propagation и C-level explainability реально используют:

- `rate_from_pct`, `rate_to_pct`, `effective_date`
- `pass_through_estimate`
- `tax_base_coverage`
- `elasticity_hint`
- `sector_sensitivity`
- `channel_type`
- `observability`
- `measurement_lag_months`
- `measurement_confidence`

На `/loc/kz` задаются calibration assumptions для макро-адаптера:

- `/metric/policy/vat_rate_pct = 12`
- `/metric/policy/baseline_gdp_bln_kzt = 140000`
- `/metric/policy/baseline_vat_revenue_bln_kzt = 4200`

Это не объявляется ground-truth forecast. Это стартовые scenario assumptions, достаточные для первого симуляционного прогона и сравнения последующих калибровок.

## Execution Path

Для этого кейса используется multi-run пакет, а не один сигнал:

1. `legal/compliance`
2. `financial/settlement`
3. `physical_flow/demand`
4. `info/uncertainty`

Причина: повышение НДС раскладывается не по одной оси. Один run хорошо ловит либо compliance, либо narrative, но теряет cashflow и demand effects. Пакет даёт лучшее покрытие для C-level readout.

## Tooling

Повторяемый entrypoint:

```bash
go run ./backend/cmd/tools/horizon_kz_vat_hot_path --project prevalent-horizon-test
```

Что делает tool:

1. upsert’ит canonical fragments в `horizon_fragments`
2. upsert’ит regulatory edges в `horizon_dependency_edges`
3. запускает simulation pack через `POST /api/horizon/simulations`
4. ждёт terminal state
5. печатает technical summary + C-level readout

## Design Rules for Next Regulatory Scenarios

- Всегда строить цепочку `authority -> decision -> mechanism -> exposed markets/community`.
- Не моделировать regulatory scenario как чистый текстовый сигнал на `/loc/...`, если можно явно поднять `decision` и `mechanism`.
- Если не хватает экспонированных узлов, добавлять canonical `market/program/community` объекты, а не временные ad hoc labels.
- Для каждого major exposure edge добавлять `observability` и `measurement_*` qualifiers.
- Если сценарий реально многослойный, запускать axis-pack, а не один run.

## Relation to Existing Architecture

Этот hot-path дополняет уже описанный policy-impact substrate и concretizes VAT example из:

- [`/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/structural-org-supply-chain-model.md`](/Users/kirisyuk/cursor/sanskrite/docs/horizon/architecture/structural-org-supply-chain-model.md)

Разница в том, что здесь пример доведён до executable form: есть canonical nodes, canonical edges, seedable metrics и готовый CLI entrypoint для повторного прогонки на живой инфраструктуре.
