package analytical

import (
	"testing"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildSupplyLinkPrimitivesApplyRefreshPolicy(t *testing.T) {
	now := time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC)
	items := BuildSupplyLinkPrimitives(SupplyLinkPrimitiveInput{
		TenantID:            "global",
		ScopeRef:            "/edge/supply-link-1",
		ProductRef:          "/prod/battery",
		SupplierRef:         "/org/a",
		CustomerRef:         "/org/b",
		TransportViaRefs:    []string{"/port/aktau"},
		Criticality:         0.81,
		CoverageStatus:      "partial",
		GateStatus:          "ready",
		RouteFragility:      0.72,
		RouteRerouteability: 0.22,
		ComputedAt:          now,
	})
	if len(items) < 3 {
		t.Fatalf("expected multiple primitives, got %d", len(items))
	}
	byType := map[string]*hmodels.AnalyticalPrimitive{}
	for _, item := range items {
		byType[item.PrimitiveType] = item
	}
	if byType["supply_link_risk"] == nil || byType["supply_link_risk"].RefreshClass != "immediate" {
		t.Fatalf("supply_link_risk refreshClass = %q", byType["supply_link_risk"].RefreshClass)
	}
	if byType["route_concentration"] == nil || byType["route_concentration"].RefreshClass != "signal_driven" {
		t.Fatalf("route_concentration refreshClass = %q", byType["route_concentration"].RefreshClass)
	}
	if byType["low_redundancy_corridor"] == nil || byType["low_redundancy_corridor"].RefreshClass != "signal_driven" {
		t.Fatalf("low_redundancy_corridor refreshClass = %q", byType["low_redundancy_corridor"].RefreshClass)
	}
	if len(byType["route_concentration"].RecomputeTriggers) == 0 {
		t.Fatalf("route_concentration recompute triggers missing")
	}
	if byType["route_concentration"].StaleAt == nil || byType["route_concentration"].StaleAt.Sub(now) != 24*time.Hour {
		t.Fatalf("route_concentration staleAt = %v", byType["route_concentration"].StaleAt)
	}
}

func TestBuildScopeAggregatesApplyPolicyAndEarliestStaleAt(t *testing.T) {
	now := time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC)
	early := now.Add(6 * time.Hour)
	late := now.Add(24 * time.Hour)
	aggregates := BuildScopeAggregates(AggregateScopeInput{
		TenantID:    "global",
		ScopeType:   "lens",
		ScopeRef:    "lens-1",
		Layer:       "baseline",
		SourceClass: "test",
		Primitives: []*hmodels.AnalyticalPrimitive{
			{
				ID:                "p1",
				TenantID:          "global",
				ScopeRef:          "/edge/a",
				PrimitiveType:     "chokepoint_dependency",
				SeverityScore:     0.82,
				Confidence:        0.6,
				CoverageStatus:    "partial",
				GateStatus:        "degraded",
				StaleAt:           &late,
				BasisRefs:         []string{"/port/aktau"},
				ProductRefs:       []string{"/prod/battery"},
				RecomputeTriggers: []string{"signal_change"},
			},
			{
				ID:             "p2",
				TenantID:       "global",
				ScopeRef:       "/edge/b",
				PrimitiveType:  "low_redundancy_corridor",
				SeverityScore:  0.71,
				Confidence:     0.8,
				CoverageStatus: "complete",
				GateStatus:     "ready",
				StaleAt:        &early,
				RouteRefs:      []string{"/route/caspian"},
			},
		},
		ComputedAt: now,
	})
	if len(aggregates) != 1 {
		t.Fatalf("expected 1 aggregate, got %d", len(aggregates))
	}
	aggregate := aggregates[0]
	if aggregate.RefreshClass != "immediate" {
		t.Fatalf("refreshClass = %q", aggregate.RefreshClass)
	}
	if len(aggregate.RecomputeTriggers) == 0 {
		t.Fatalf("recompute triggers missing")
	}
	if aggregate.StaleAt == nil || !aggregate.StaleAt.Equal(early) {
		t.Fatalf("staleAt = %v want %v", aggregate.StaleAt, early)
	}
	if aggregate.SeverityScore < 0.82 {
		t.Fatalf("severityScore = %.2f", aggregate.SeverityScore)
	}
	if aggregate.AggregateType != "weak_points_summary" {
		t.Fatalf("aggregateType = %q", aggregate.AggregateType)
	}
}

func TestBuildSimulationRunAggregateUsesScenarioPolicy(t *testing.T) {
	now := time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC)
	run := &hmodels.SimulationRun{
		ID:       "run-1",
		TenantID: "global",
		Request: hmodels.SimulationRunRequest{
			Question: "What happens if Aktau port closes?",
			LensID:   "lens-1",
		},
		Summary: hmodels.SimulationSummary{
			OutcomeDistribution: map[string]float64{"severe": 0.88},
		},
		ExpiresAt: now.Add(72 * time.Hour),
	}
	aggregate := BuildSimulationRunAggregate(run, nil, now)
	if aggregate == nil {
		t.Fatal("expected aggregate")
	}
	if aggregate.Layer != "scenario" {
		t.Fatalf("layer = %q", aggregate.Layer)
	}
	if aggregate.RefreshClass != "scenario_only" {
		t.Fatalf("refreshClass = %q", aggregate.RefreshClass)
	}
	if len(aggregate.RecomputeTriggers) == 0 {
		t.Fatalf("recompute triggers missing")
	}
}
