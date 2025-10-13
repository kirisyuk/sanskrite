## Realtime Module

### Scope
- Bridges internal events (Pub/Sub) to clients via WebSocket; subscribes to topics.

### Key Code
- WebSocket server: `backend/internal/realtime/ws.go`
- Pub/Sub wrapper: `backend/internal/realtime/pubsub.go`
- Subscriber: `backend/internal/realtime/subscriber.go`

### Flow
1. Internal events published to Pub/Sub
2. Subscriber processes and forwards to WS clients
3. Clients receive updates on active sessions


