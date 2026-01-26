package horizon

import "testing"

func TestCollectNearbyDomainPathsFromGraph(t *testing.T) {
	scope := []string{
		"/domain/v1/power-dynamics/sector/ai-chips",
		"/org/us/asml",
		"/org/cn/smic",
	}
	edges := []overlayEdgePath{
		{Source: "/org/us/asml", Target: "/domain/v1/market-opportunity/geo/us/sector/ai-chips"},
		{Source: "/domain/v1/power-dynamics/sector/ai-chips", Target: "/org/cn/smic"},
	}
	got := collectNearbyDomainPathsFromGraph(scope, edges)
	if len(got) != 2 {
		t.Fatalf("collectNearbyDomainPathsFromGraph() len=%d, want 2 (got=%v)", len(got), got)
	}
}
