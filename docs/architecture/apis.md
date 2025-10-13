## APIs

This document catalogs HTTP REST, WebSocket events, and Pub/Sub topics. The authoritative definitions are the handlers and subscribers in code.

### REST Endpoints
- Handlers under `backend/internal/http/*.go` (e.g., `projects.go`, `users.go`, `discovery.go`).
- Auth middleware: `backend/internal/http/auth.go`.

Contract shapes correspond to models in `backend/internal/models/models.go`.

### WebSocket
- WS server: `backend/internal/realtime/ws.go`.
- Events: client subscribes for updates; server pushes discovery/profiling progress and results.

### Pub/Sub
- Pub/Sub integration: `backend/internal/realtime/pubsub.go`, `backend/internal/realtime/subscriber.go`.
- Topics and subscriptions defined alongside; see code for names and attributes.


