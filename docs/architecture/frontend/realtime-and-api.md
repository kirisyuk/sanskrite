## Realtime and API

### Realtime
- WebSocket client: `web/src/lib/ws.ts`
- Consumers: pages and stores subscribe to updates for discovery/profiling

### API
- REST calls from components/pages to backend handlers under `backend/internal/http/*`

### Auth
- Auth context and guard: `web/src/contexts/AuthContext.tsx`, `web/src/components/AuthGuard.tsx`


