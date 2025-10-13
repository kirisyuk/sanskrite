## Discovery Module

### Scope
- Orchestrates discovery and profiling of repositories and resources.

### Key Code
- Orchestrator: `backend/internal/discovery/orchestrator.go`
- Profiler: `backend/internal/discovery/profiler.go`
- Crawler: `backend/internal/crawler/crawler.go`

### Interactions
- Reads/writes via `internal/repo/*`
- Uses AI module for analysis when needed
- Publishes progress/events to realtime subsystem


