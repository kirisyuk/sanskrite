## Documentation Conventions

- Use Mermaid for context, component, and sequence diagrams.
- Link directly to code paths using backticks, e.g., `backend/internal/http/auth.go`.
- **HTTP-маршруты:** не перечислять «вручную из головы» — сверять с `backend/cmd/server/main.go` и `backend/internal/http/horizon/handlers.go`; платформенный каталог поддерживать в [apis.md](../apis.md), Horizon — в [endpoints.md](../../horizon/api/endpoints.md).
- **Health:** основной эндпоинт бэкенда — `/healthz` (дубль `/api/v1/healthz`), не путать с устаревшим `/health`.
- Avoid duplication: summarize and link to existing deployment docs under `docs/deployment/*`.
- Prefer stable headings and anchors for AI linking.
- Keep documents concise; the source of truth is code.
- `docs/archive/` и `docs/notes/` не обязаны быть синхронны с каждым релизом.

### Diagram IDs
- Keep node IDs stable across diagrams to help cross-referencing by AI.

### File Linking Pattern
- Module doc -> Primary code files -> Related sequences -> Related APIs


