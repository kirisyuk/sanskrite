package workscope

// Limits for materialized artifact fetches keyed by lens (single source of truth for cross-layer caps).
const (
	MaterializedListByLensLimitDefault    = 256
	MaterializedListByLensLimitSimulation = 200
)
