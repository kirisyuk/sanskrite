## APIs

Каталог HTTP (платформа `/api/v1`, Horizon, Critical Chain), WebSocket и внутренних задач. **Первичный источник правды** — регистрация маршрутов в коде; этот файл — сводка и детальные контракты там, где они полезны.

### Где смотреть в коде

| Область | Файл(ы) |
|---------|---------|
| Регистрация большинства `/api/v1/*`, `/ws`, internal tasks | `backend/cmd/server/main.go` |
| Horizon `/api/horizon/*` + feeds | `backend/internal/http/horizon/handlers.go`, `horizonmonitor_external_routes.go` |
| Enforcement | `backend/internal/http/horizon/enforcement_handlers.go` |
| Critical Chain | `backend/internal/http/criticalchain/handlers.go` |
| Auth middleware | `backend/internal/http/auth.go` |

### Health

- `GET /healthz` — liveness
- `GET /api/v1/healthz` — то же под префиксом API

### WebSocket

- `GET /ws` — см. `backend/internal/realtime/ws.go` (токен и события в query/протоколе хендлера)

### Внутренние задачи (не пользовательский Firebase)

Пути `/api/v1/internal/tasks/*` вызываются оркестраторами (например Cloud Tasks) с **OIDC Bearer** или иной проверкой в теле хендлера — см. соответствующие блоки в `main.go`:

- `POST .../internal/tasks/scout/run`
- `POST .../internal/tasks/risk/run`
- `POST .../internal/tasks/scope-agents/run`
- `POST .../internal/tasks/profile/refresh`
- `POST .../internal/tasks/sources/poll`
- `POST .../internal/tasks/canvas/ingest`

### Публичные и сервисные маршруты без Firebase (выборочно)

- `POST` / `GET /api/v1/waitlist` (join / check — без Firebase middleware)
- `POST /api/v1/paypal/webhook`
- `GET /api/v1/webhooks/...`
- `POST /api/v1/subscriptions/billing` (Stripe и т.п. — см. код)
- `POST /api/v1/artifacts/scheduled-update`, `POST /api/v1/signals/process` (секрет/внутренний вызов — см. код)
- `GET /api/v1/public/share/...`, `GET /api/v1/public/verified/...`, `GET /api/v1/public/documents/...`
- Демо Critical Chain: `GET /api/v1/criticalchain/*` (часть без `authMiddleware` — см. `criticalchain/handlers.go`)
- `GET /api/v1/vessels/bbox` (AISStream и т.д. — см. `main.go`)

### `/api/v1` под Firebase (сводка)

| Группа | Путь (паттерн) |
|--------|----------------|
| Пользователи | `/api/v1/users`, `/api/v1/users/{id}` |
| Waitlist (admin) | `/api/v1/waitlist/approve`, `/api/v1/waitlist/list` |
| Подписки | `/api/v1/subscriptions/me`, `/api/v1/subscriptions/voucher` |
| PayPal (кроме webhook) | `/api/v1/paypal/subscription`, `.../execute`, `.../cancel` |
| Ваучеры | `/api/v1/vouchers`, `/api/v1/vouchers/...` |
| Админ телеметрия Horizon | `/api/v1/admin/horizon/telemetry`, `.../external-services` |
| Проекты | `/api/v1/projects`, `/api/v1/projects/{id}` и **вложенные** пути (см. ниже) |
| Правила | `/api/v1/rules`, `/api/v1/rules/{id}` |
| Listening signals (глобальные) | `/api/v1/listening-signals`, `/api/v1/listening-signals/...` (если включено) |
| BigQuery | `POST /api/v1/bigquery/query` (если включено) |
| Discovery | `POST /api/v1/sources-discovery/execute` |
| Агенты (реестр) | `/api/v1/agents` |
| GenAI | `/api/v1/genai/complete`, `complete-json`, `complete-json-strict`, `generate`, `complete-with-tools`, `stream`, `adhoc-json` |
| Usage | `/api/v1/usage`, `/api/v1/usage/events`, `/api/v1/usage/avg-timing` |
| AI structured | `POST /api/v1/ai/structured` (если включено) |
| Шаблоны / артефакты платформы | `/api/v1/artifact-templates`, `/api/v1/artifacts/...` |
| Источники | `/api/v1/sources`, `/api/v1/sources/...`, preview telegram/rss |
| Verified | `POST /api/v1/verified` |
| Generation sessions | `/api/v1/generation-sessions/...` |
| Админ jobs | `POST /api/v1/admin/sources/coordinate` (**jobAuth**), `POST /api/v1/admin/horizon/watchdog/run` (**jobAuth**), `POST /api/v1/admin/horizon/lens_runtime/sync` (**jobAuth**) |
| Critical Chain admin | `POST /api/v1/criticalchain/admin/pipeline/run` (**jobAuth**) |

### Вложенные маршруты проекта `/api/v1/projects/{projectId}/…`

Роутинг задаётся в `main.go` (блок `mux.Handle("/api/v1/projects/", ...)`) и в `routeProjectsByID`. Типичные суффиксы:

| Назначение | Примеры суффиксов |
|------------|-------------------|
| Источники | `.../datasources`, `.../sources` (POST attach), `.../sources/{sourceId}/status` (GET/PATCH), DELETE detach |
| Поиск | `.../search` (GET), `.../vector-search` (POST), `.../vertex-search` (POST) |
| Ingest | `.../ingest` (POST) |
| Анализ | `.../analyze/entities`, `.../analyze/facts`, `.../analyze/events`, `.../analyze/metrics`, `.../analyze/decisions` |
| Документы | `.../docs`, `.../docs/upload`, `.../docs/from-text`, `.../docs/{docId}` (PATCH/DELETE), `.../parse`, `.../download`, `.../index-status`, `.../bigquery-status`, `.../vertex-search-availability`, `.../export` |
| Meta | `.../meta:analyze`, `.../meta/events`, `.../meta/aggregate` |
| Analytics / BigQuery UI | `.../analytics/schema`, `.../analytics/generate-sql`, `.../analytics/execute-sql`, `.../analytics/prepare/analyze`, `.../prepare/apply`, `.../analytics/query` |
| Share | `.../share` (POST), `.../share/...` (DELETE) |
| Чат / взаимодействия | `.../chat` (POST), `.../interactions`, `.../interactions/{id}` (PATCH/DELETE) |
| Сценарии | `.../scenarios`, `.../scenarios/{scenarioId}`, `.../scenarios/{scenarioId}/data`, … |
| Агент по проекту | `.../agents/{agentId}/execute` |
| Артефакты проекта | `.../artifacts`, `.../artifacts/{artifactId}` |
| Listening signals (per project) | `.../listening-signals`, `.../listening-signals/{id}`, activate/deactivate |
| Signal history | `.../signal-history` (GET) |
| Agent jobs | `.../agent-jobs`, `.../agent-jobs/{jobId}`, execute/pause/resume/cancel/steps |

Точные методы и тела — в соответствующих хендлерах (`projects.go`, `discovery.go`, и т.д.).

### Horizon

Префикс `/api/horizon` — отдельный реестр: [docs/horizon/api/endpoints.md](../horizon/api/endpoints.md).

### Critical Chain (демо + админ)

Публичные GET под `/api/v1/criticalchain/...` и админ `POST /api/v1/criticalchain/admin/pipeline/run` — см. `backend/internal/http/criticalchain/handlers.go`.

### Pub/Sub

Интеграция: `backend/internal/realtime/pubsub.go`, подписчики рядом; имена топиков — только из кода и конфигов деплоя.

---

Ниже — **детальные** фрагменты контрактов (GenAI и др.). Они дополняют сводку выше.

### REST Endpoints (детали и модули)
- Handlers under `backend/internal/http/*.go` (e.g., `projects.go`, `users.go`, `discovery.go`).
- Auth middleware: `backend/internal/http/auth.go`.
#### GenAI (Unified)
- Unified generate
  - POST `/api/v1/genai/generate`
  - Body (`GenAIRequest`):
    - `model: string` — provider alias, e.g. `gemini_fast|gemini_quality|gemini_balanced|gpt_fast|gpt_balanced|gpt_quality|grok_fast|grok_quality`
    - `prompt: string`
    - `systemInstruction?: string`
    - `temperature?: number`
    - `maxOutputTokens?: number`
    - `webSearchEnabled?: boolean`
    - `toolCallingEnabled?: boolean`
    - `strictJSONEnabled?: boolean`
    - `jsonSchema?: object` — JSON Schema for strict/function tool
    - `tools?: Tool[]` — function declarations `{ name, description, parameters }`
    - `toolChoice?: "auto"|"required"`
    - `parallelToolCalls?: boolean`
  - Resp (`GenAIResponse`):
    - `model: string`
    - `content?: string`
    - `toolCalls?: { name: string, arguments: string, callId?: string }[]`
    - `usage: { promptTokens: number, outputTokens: number, totalTokens: number, reasoningTokens?: number, toolUsePromptTokens?: number }`
    - `sources?: string[]` — cleaned URLs (no vertex redirect hosts)
    - `searchQueries?: string[]`
    - `attributions?: { title?: string, url?: string }[]`
    - `rawJSON?: string`
  - Notes:
    - Gemini: tools+search в одном запросе недоступны на публичном API. Эндпоинт выполняет двухшаговый пайплайн (tool‑only → grounded‑only) и мержит Usage/Sources/SearchQueries.
    - Gemini strict: реализовано через function tool `return_structured` и серверную валидацию схемы; `response_mime_type` при tools не задаётся.
    - OpenAI: web search включается через tool; извлечение источников/запросов — из function calls и текста.
    - Grok: live search на уровне request (`search: { sources: [{ type: "web" }] }`).


Contract shapes correspond to models in `backend/internal/models/models.go`.

#### Projects
- Create project
  - POST `/api/v1/projects`
  - Body: `{ displayName: string, description?: string, view?: string }`
  - Resp: `200/201` Project object

- Update project
  - PUT `/api/v1/projects/{id}`
  - Body: partial project `{ displayName?, description?, status?, view? }`
  - Resp: `200` Project object

- Get/List projects
  - GET `/api/v1/projects/{id}` → Project
  - GET `/api/v1/projects?view=discovery|workspace` → `Project[]`

#### Project Interactions
- List interactions
  - GET `/api/v1/projects/{id}/interactions`
  - Resp: `200 { items: Interaction[] }`

- Create interaction
  - POST `/api/v1/projects/{id}/interactions`
  - Body:
    - `{ kind: "user" | "assistant" | "system" | "note", content: string }`
  - Resp: `201 { id: string }`

- Delete interaction
  - DELETE `/api/v1/projects/{id}/interactions/{interactionId}`
  - Resp: `200 { success: true }`

#### Project–Source Linking
- Attach source to project
  - POST `/api/v1/projects/{id}/sources`
  - Body: `{ sourceId: string, status?: string }`
  - Resp: `201 { id: string }`

- Detach source from project
  - DELETE `/api/v1/projects/{id}/sources/{sourceId}`
  - Resp: `200 { success: true }`

Notes:
- Большинство перечисленных выше маршрутов требуют Firebase ID token (см. `auth.go`); публичные исключения — в сводке в начале файла.
- Tenancy enforced by `tenantId` on resources.

### WebSocket (события)

- Реализация: `backend/internal/realtime/ws.go`.
- Клиент подписывается на обновления; сервер пушит прогресс discovery/profiling, события GenAI stream и др. (конкретные `type` — в коде хаба и фронта).

### Pub/Sub
- Pub/Sub integration: `backend/internal/realtime/pubsub.go`, `backend/internal/realtime/subscriber.go`.
- Topics and subscriptions defined alongside; see code for names and attributes.


