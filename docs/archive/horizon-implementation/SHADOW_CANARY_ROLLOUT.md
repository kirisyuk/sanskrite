# Horizon Shadow/Canary Rollout Runbook

Этот runbook фиксирует операционный контур Sprint `S5`:
1. shadow replay validation на production логах,
2. canary rollout `10% -> 50% -> 100%`,
3. auto-rollback при провале release gates.

## 1) Пререквизиты

- `gcloud` авторизован на нужный проект (`etigram`) и регион (`us-central1`).
- Установлен `jq`.
- Candidate revision уже задеплоен и доступен в Cloud Run (`etigram-api-xxxxx-yyy`).
- Для candidate включены runtime flags нового контура (включая shadow compare режим, если требуется конкретным запуском).

## 2) Shadow replay gate-check (без переключения трафика)

Базовая команда:

```bash
scripts/horizon_shadow_replay.sh \
  --project etigram \
  --region us-central1 \
  --service etigram-api \
  --freshness 6h \
  --output-json /tmp/horizon-shadow-report.json
```

Проверка gate-порогов (exit code != 0 при fail):

```bash
scripts/horizon_shadow_replay.sh \
  --project etigram \
  --region us-central1 \
  --service etigram-api \
  --freshness 6h \
  --baseline-json /tmp/horizon-baseline.json \
  --enforce
```

### Что считает скрипт

- `quarantined_edges`
- `direction_contract_quarantine_rate`
- `ready_with_gaps`
- `fresh_no_new_facts`
- `research_skipped_no_searchable_gaps`
- `soft_stop_recovered_rate`
- диагностические индикаторы:
  - `source_planning_skips`
  - `no_source_content`
  - `direction_role_ambiguous`

## 3) Базовые release gates

По умолчанию `scripts/horizon_shadow_replay.sh --enforce` проверяет:

- `ready_with_gaps <= 0`
- `fresh_no_new_facts <= 0`
- `research_skipped_no_searchable_gaps <= 0`
- `soft_stop_recovered_rate >= 0.80`

Дополнительно для `direction_contract_quarantine_rate`:

- если передан `--baseline-json` с полем `direction_contract_quarantine_rate`,
  скрипт автоматически выставляет candidate gate: `<= baseline * 0.5`.
- можно задать абсолютный предел вручную через env:
  `MAX_DIRECTION_CONTRACT_QUARANTINE_RATE=0.08`.

## 4) Canary rollout (с auto-rollback)

Скрипт пошагового rollout:

```bash
scripts/horizon_canary_rollout.sh \
  --project etigram \
  --region us-central1 \
  --service etigram-api \
  --candidate-revision etigram-api-01234-abc \
  --stable-revision etigram-api-01233-def \
  --freshness 30m \
  --hold-minutes 10 \
  --baseline-json /tmp/horizon-baseline.json \
  --report-dir /tmp/horizon-canary-reports
```

Поведение:

1. Переключает трафик на `10%` candidate.
2. Ждет `hold-minutes`, запускает `shadow_replay --enforce`.
3. При `pass` повторяет шаг для `50%`, затем `100%`.
4. При `fail` на любой фазе автоматически делает rollback на stable revision (`100%`).

Dry-run (без изменения трафика):

```bash
scripts/horizon_canary_rollout.sh \
  --project etigram \
  --region us-central1 \
  --service etigram-api \
  --candidate-revision etigram-api-01234-abc \
  --dry-run
```

## 5) Артефакты запуска

- Shadow report: `--output-json <path>`
- Canary reports: `--report-dir/phase-10.json`, `phase-50.json`, `phase-100.json`

Эти JSON-отчеты используются как release evidence для gate-решения `pass|hold|rollback`.
