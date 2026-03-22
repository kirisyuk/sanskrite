## HTTP Module

### Scope

- Роутинг и middleware для **платформы** (`/api/v1`, `/ws`, internal tasks) в `backend/cmd/server/main.go` и хендлерах `backend/internal/http/*.go`.
- Отдельные подсистемы регистрируют свои префиксы:
  - **Horizon**: `backend/internal/http/horizon/*.go` → `/api/horizon/*`
  - **Critical Chain**: `backend/internal/http/criticalchain/handlers.go` → `/api/v1/criticalchain/*`

### Key Code

- Auth: `backend/internal/http/auth.go`
- Discovery: `backend/internal/http/discovery.go`
- Projects: `backend/internal/http/projects.go`
- Users: `backend/internal/http/users.go`
- GenAI: `backend/internal/http/genai_handler.go` (и связанные)
- Responses: `backend/internal/http/responses.go`

### Документация

- Сводный реестр HTTP: [apis.md](../../apis.md)
- Horizon API: [horizon/api/endpoints.md](../../../horizon/api/endpoints.md)
