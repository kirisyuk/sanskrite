# Realtime (WebSocket + Pub/Sub broker) — Horizon

Этот документ описывает **фактическую** реализацию realtime-событий Horizon в коде.

## Архитектура

Horizon использует общий realtime pipeline:

- Backend публикует события через `internal/realtime.Publisher` в **Google Cloud Pub/Sub**.
- Отдельный сервис WS-брокера `cmd/ws` подписан на Pub/Sub топики и ретранслирует события в WebSocket клиентов.
- Клиент открывает WebSocket и **подписывается на проект** control-сообщением `subscribe`.

## WebSocket подключение

### URL

- **WebSocket endpoint**: `/ws`
- **Auth**: Firebase ID Token передаётся через query параметр `token`.

Пример:

```text
wss://<WS_BROKER_OR_API_HOST>/ws?token=<FIREBASE_ID_TOKEN>
```

Примечание: фронтенд может подключаться либо к API, либо к выделенному брокеру (см. `VITE_WS_BROKER_URL`).

### Подписка (project-based)

После `onopen` клиент отправляет:

```json
{"type":"subscribe","projectId":"<projectId>"}
```

WS сервер отвечает ACK:

```json
{
  "type": "ws.subscribed",
  "data": {
    "tenantId": "<tenantId>",
    "projectId": "<projectId>",
    "connId": "<connId>",
    "ts": 1730000000000
  }
}
```

## Модель безопасности

- **tenantId** используется как security namespace для WebSocket соединений (коннекты bucket'ятся по tenant).
- Хотя большинство Horizon объектов “глобальные”, realtime-события публикуются **в контексте tenant+project**, чтобы события получали только клиенты проекта.

## Корреляция (Hook → Assemble)

При создании Lens (фактический assemble) клиент может передать:

- `X-Correlation-Id` — сквозной идентификатор “сборки”
- `X-Session-Id` — заготовка для будущей session-based фильтрации (пока не используется для роутинга)

Бэкенд сгенерирует `X-Correlation-Id`, если он не передан, и вернёт его в ответе.

## Event types (актуальные)

Все события Horizon используют namespace **`horizon.*`** и уходят в отдельный Pub/Sub топик `nv_horizon.horizon.events`.

Envelope всегда одинаковый:

```json
{
  "type": "horizon.<event>",
  "data": { "...": "..." }
}
```

### Assembly (Sled)

- `horizon.assembly.started`
- `horizon.assembly.stage` (preferred)
- `horizon.assembly.phase` (legacy alias)
- `horizon.assembly.progress`
- `horizon.assembly.ready`
- `horizon.assembly.failed`

### Scout jobs

- `horizon.scout.job.created`
- `horizon.scout.job.state`
- `horizon.scout.job.log`
- `horizon.scout.job.completed`

### Domain pipeline passes (preferred)

- `horizon.domain.base_enrichment.started`
- `horizon.domain.base_enrichment.completed`
- `horizon.domain.strategic_projection.scheduled`
- `horizon.domain.strategic_projection.started`
- `horizon.domain.strategic_projection.completed`

Legacy (kept during migration):

- `horizon.domain.expansion.started`
- `horizon.domain.expansion.completed`
- `horizon.domain.overlay.scheduled`
- `horizon.domain.overlay.started`
- `horizon.domain.overlay.completed`

### Deprecations / rollout notes

- **Assembly naming**
  - Preferred: `horizon.assembly.stage` + `data.stage`
  - Legacy: `horizon.assembly.phase` + `data.phase` (still emitted/accepted for compatibility)
- **Domain pass naming**
  - Preferred: `horizon.domain.base_enrichment.*`, `horizon.domain.strategic_projection.*`
  - Legacy: `horizon.domain.expansion.*`, `horizon.domain.overlay.*`
- **Scout job hints**
  - Preferred: `hint=base_enrichment`, `hint=strategic_projection`
  - Legacy: `legacyHint=domain_expansion`, `legacyHint=domain_overlay`

### Graph materialization

- `horizon.graph.node.upserted`
- `horizon.graph.edge.upserted`

### Propagation / Lens

- `horizon.propagation.started`
- `horizon.propagation.node_updated`
- `horizon.propagation.completed`
- `horizon.lens.updated`

## Минимальные поля `data` (для роутинга и UI)

Почти все события содержат:

- `tenantId` (security namespace)
- `projectId` (**минимальный routing key**)
- `lensId` (UI scoping)
- `correlationId` (Hook→Assemble trace)
- `sessionId` (опционально, заготовка)
- `ts` (RFC3339Nano)

Дополнительно по событию:

- Scout: `jobId`, `state`, `progress`, `targetCanonicalPath`, `line`
- Graph node: `fragmentId`, `canonicalPath`, `status`, `epistemicStatus`, `version`
- Propagation: `sourceFragmentId`, `originLensId` (опционально)

## Карта соответствия: eventType → место в коде

Ниже — “куда смотреть”, если событие не приходит или payload не тот.

### WebSocket / broker plumbing

- **WS endpoint `/ws`**
  - `backend/cmd/ws/main.go` (отдельный сервис брокера)
  - `backend/internal/realtime/ws.go` (Hub: subscribe + project routing)
  - `backend/internal/realtime/subscriber.go` (Pub/Sub → WS delivery)
- **Pub/Sub routing по топикам**
  - `backend/internal/realtime/pubsub.go`
    - `TopicHorizonEvents = "nv_horizon.horizon.events"`
    - `routeTopicName(...)` — `horizon.*` → `nv_horizon.horizon.events`

### Hook → Assemble (создание Lens) / Assembly events

- **`horizon.assembly.started` / `horizon.assembly.stage` (preferred) / `horizon.assembly.phase` (legacy) / `horizon.assembly.progress` / `horizon.assembly.ready`**
  - `backend/internal/horizon/lens/service.go`
    - `(*LensService).CreateLens(...)`
  - Триггер: `POST /api/horizon/lenses` (`backend/internal/http/horizon/lenses.go`)
  - Корреляция: `X-Correlation-Id`, `X-Session-Id` → `rtctx` → `job.State`

### Scout jobs

- **`horizon.scout.job.created`**
  - `backend/cmd/server/main.go`
    - `horizonScoutingJobsRepo.WithOnCreate(...)` (Cloud Tasks hook)
  - Триггер: создание ScoutingJob в `LensService.CreateLens` (и рекурсивные jobs в ScoutEngine)

- **`horizon.scout.job.state`**
  - `backend/internal/horizon/scout/engine.go`
    - `(*ScoutEngine).transition(...)` (обёртка над `ScoutWorkflow.TransitionState`)
  - Триггер: переходы состояний PENDING→PLANNING→EXECUTION→EXTRACTION→...→VERIFIED/FAILED

- **`horizon.scout.job.log`**
  - `backend/internal/horizon/scout/engine.go`
    - `(*ScoutEngine).logLine(...)`
  - Триггер: planning/execution/extraction/validation/research этапы (best-effort лог-строки)

- **`horizon.scout.job.completed`**
  - `backend/internal/horizon/scout/engine.go`
    - после создания Frame + upsert Fragment (в конце `ExecuteScoutingJob`)
    - также fast-path ветки (fresh/no-new-facts, deterministic apply)

### Graph materialization

- **`horizon.graph.node.upserted`**
  - `backend/internal/horizon/lens/service.go`
    - cache-hit узлы + “узел в scouting” при создании job
  - `backend/internal/horizon/scout/engine.go`
    - после verified/low_confidence (по завершению job)
  - `backend/internal/horizon/propagation/engine.go`
    - при волнах propagation (status="propagated")

- **`horizon.graph.edge.upserted`**
  - `backend/internal/http/horizon/edges.go` (CreateEdge/UpdateEdge, best-effort — требует `X-Project-Id`)
  - `backend/internal/horizon/scout/engine_ext.go` (discovered relations → create edge)

### Propagation / Lens updates

- **`horizon.propagation.started`**
  - `backend/internal/horizon/propagation/engine.go`
    - `publishPropagationEvents(...)`
  - Важно: propagation запускается из ScoutEngine в goroutine; `rtctx` переносится вручную.

- **`horizon.propagation.node_updated` / `horizon.propagation.completed`**
  - `backend/internal/horizon/propagation/engine.go`
    - `publishPropagationEvents(...)`

- **`horizon.lens.updated`**
  - `backend/internal/horizon/propagation/engine.go`
    - `publishPropagationEvents(...)` (affectedLenses)
    - cold-path processor: best-effort publish только если есть `rtctx` (иначе skip, чтобы не “протечь” между tenant'ами)


