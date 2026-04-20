package metriccore

import "testing"

func TestPathqueryMetrics_roundTrip(t *testing.T) {
	ResetPathqueryMetricsForTest()
	IncPathqueryCacheHit()
	IncPathqueryCacheMiss()
	IncPathqueryBuilderInvocation()
	if PathqueryCacheHits() != 1 || PathqueryCacheMisses() != 1 || PathqueryBuilderInvocations() != 1 {
		t.Fatal()
	}
}
