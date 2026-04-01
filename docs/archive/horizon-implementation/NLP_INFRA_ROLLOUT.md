# NLP Infra Rollout

## Scope

The RSS/Horizon Monitor path now supports a hybrid NLP stack:

1. `event-gate` service
2. `entity-extract` service
3. deterministic fallback inside core discovery

Core orchestration remains in the backend:
- RSS polling
- source orchestration
- facts/signals/zones
- propagation
- repair/backfill

The model layer is optional and replaceable.

## Services

### Event Gate
- Cloud Run service: `nv-horizon-event-gate`
- endpoint: `POST /v1/event-gate/assess`
- purpose:
  - `real_event`
  - `analysis`
  - `advisory`
  - `roundup`
  - `broad_scope`
  - `noise`

### Entity Extract
- Cloud Run service: `nv-horizon-entity-extract`
- endpoint: `POST /v1/entity-extract/extract`
- purpose:
  - extract domain entities from text
  - stay stateless
  - do not link or decide zones

## Core env

- `EVENT_GATE_SERVICE_URL`
- `ENTITY_EXTRACT_SERVICE_URL`
- `EVENT_GATE_MODE=rules|shadow|hybrid|model`
- `ENTITY_EXTRACT_MODE=rules|shadow|hybrid|model`
- `EVENT_GATE_TIMEOUT_MS`
- `ENTITY_EXTRACT_TIMEOUT_MS`

## Recommended rollout

1. Deploy `event-gate`
2. Enable `EVENT_GATE_MODE=shadow`
3. Audit divergence
4. Enable `hybrid`
5. Deploy `entity-extract`
6. Enable `ENTITY_EXTRACT_MODE=shadow`
7. Re-audit `affected zones`
8. Only then consider `model`

## Deployment

The deploy script now supports:

- `./scripts/deploy --event-gate`
- `./scripts/deploy --entity-extract`

`--api` auto-discovers these services and injects their URLs into backend env when present.

## Failure mode

If any NLP service fails:
- core backend falls back to deterministic rules
- RSS ingestion must continue
- no NLP service is allowed to become a hard dependency
