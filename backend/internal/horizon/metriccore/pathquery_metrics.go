package metriccore

import "sync/atomic"

// Pathquery cache / builder counters (P8 observability). Best-effort in-process only.
// Spec names (for future Prometheus wiring): pathquery_cache_hits_total,
// pathquery_cache_misses_total, pathquery_builder_invocations_total.

var (
	pathqueryCacheHits           atomic.Uint64
	pathqueryCacheMisses         atomic.Uint64
	pathqueryBuilderInvocations  atomic.Uint64
)

func IncPathqueryCacheHit() {
	pathqueryCacheHits.Add(1)
}

func IncPathqueryCacheMiss() {
	pathqueryCacheMisses.Add(1)
}

func IncPathqueryBuilderInvocation() {
	pathqueryBuilderInvocations.Add(1)
}

func PathqueryCacheHits() uint64 {
	return pathqueryCacheHits.Load()
}

func PathqueryCacheMisses() uint64 {
	return pathqueryCacheMisses.Load()
}

func PathqueryBuilderInvocations() uint64 {
	return pathqueryBuilderInvocations.Load()
}

func ResetPathqueryMetricsForTest() {
	pathqueryCacheHits.Store(0)
	pathqueryCacheMisses.Store(0)
	pathqueryBuilderInvocations.Store(0)
}
