## APIs

This document catalogs HTTP REST, WebSocket events, and Pub/Sub topics. The authoritative definitions are the handlers and subscribers in code.

### REST Endpoints
- Handlers under `backend/internal/http/*.go` (e.g., `projects.go`, `users.go`, `discovery.go`).
- Auth middleware: `backend/internal/http/auth.go`.

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
- All endpoints require Firebase ID token auth (see `auth.go`).
- Tenancy enforced by `tenantId` on resources.

### WebSocket
- WS server: `backend/internal/realtime/ws.go`.
- Events: client subscribes for updates; server pushes discovery/profiling progress and results.

### Pub/Sub
- Pub/Sub integration: `backend/internal/realtime/pubsub.go`, `backend/internal/realtime/subscriber.go`.
- Topics and subscriptions defined alongside; see code for names and attributes.


