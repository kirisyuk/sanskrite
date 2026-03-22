## Backend Overview

### Process Topology
- Entrypoint: `backend/cmd/server/main.go`
- HTTP/WS handlers: `backend/internal/http/*`, `backend/internal/http/horizon/*`, `backend/internal/http/criticalchain/*`, `backend/internal/realtime/ws.go`
- Core modules: `internal/ai`, `internal/discovery`, `internal/horizon`, `internal/criticalchain`, `internal/agentmesh`, `internal/repo`, `internal/gcp`, `internal/realtime`, `internal/telegram`, …

Маршруты: [apis.md](../apis.md), Horizon: [horizon/api/endpoints.md](../../horizon/api/endpoints.md).

### Package Map
```mermaid
graph TD
  main --> http
  main --> realtime
  main --> discovery
  discovery --> ai
  discovery --> repo
  repo --> gcp
  realtime --> pubsub
  http --> repo
  http --> auth
```

### Responsibilities
- Routing/auth, discovery/profiling orchestration, AI calls, repository access, realtime updates.

See module docs under `architecture/backend/modules/*`.


