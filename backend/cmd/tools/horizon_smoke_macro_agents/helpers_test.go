package main

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFilterFreshRunsReturnsOnlyNewRuns(t *testing.T) {
	started := time.Now().UTC()
	before := []*horizonmodels.ScopeAgentRun{
		{ID: "old-1", CreatedAt: started.Add(-time.Minute)},
	}
	after := []*horizonmodels.ScopeAgentRun{
		{ID: "new-1", CreatedAt: started.Add(time.Second)},
		{ID: "old-1", CreatedAt: started.Add(-time.Minute)},
		{ID: "stale-1", CreatedAt: started.Add(-5 * time.Second)},
	}
	got := filterFreshRuns(after, before, started)
	if len(got) != 1 || got[0].ID != "new-1" {
		t.Fatalf("filterFreshRuns() ids = %v, want [new-1]", collectRunIDs(got))
	}
}

func TestEvaluateAppearanceFailsWithoutFreshExecution(t *testing.T) {
	report := &E2EReport{
		Runtime: &RuntimeSummary{FreshExecution: false, NoopReason: "no_fresh_scope_agent_run"},
	}
	verdict := evaluateAppearance(report, toolOptions{RequireMaterialChange: true})
	if verdict.Status != verdictFail {
		t.Fatalf("status = %q, want %q", verdict.Status, verdictFail)
	}
	if verdict.Reason != "no_fresh_scope_agent_run" {
		t.Fatalf("reason = %q, want no_fresh_scope_agent_run", verdict.Reason)
	}
}

func TestMatchesSignalAxisPrefix(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisPhysicalFlowSupply}
	if !matchesSignalAxisPrefix(signal, "physical_flow") {
		t.Fatalf("expected physical_flow prefix to match %q", signal.Axis)
	}
	if matchesSignalAxisPrefix(signal, "info/uncertainty") {
		t.Fatalf("did not expect info/uncertainty prefix to match %q", signal.Axis)
	}
}

func TestMatchesCanonicalContains(t *testing.T) {
	if !matchesCanonicalContains("/fac/cn/port-of-shanghai", "shanghai") {
		t.Fatalf("expected substring match")
	}
	if matchesCanonicalContains("/fac/cn/port-of-shanghai", "tesla") {
		t.Fatalf("did not expect non-matching substring")
	}
}

func TestTopNodeImpactsPrefersFarEndpoints(t *testing.T) {
	items := []horizonmodels.NodeImpact{
		{FragmentID: "seed", Hops: 0, TotalAmplitude: 1.0},
		{FragmentID: "near-1", Hops: 1, TotalAmplitude: 1.0},
		{FragmentID: "near-2", Hops: 1, TotalAmplitude: 0.9},
		{FragmentID: "far-1", Hops: 2, TotalAmplitude: 0.8},
		{FragmentID: "far-2", Hops: 3, TotalAmplitude: 0.7},
	}
	got := topNodeImpacts(items, 3, []string{"seed"})
	if len(got) != 3 {
		t.Fatalf("len(topNodeImpacts) = %d, want 3", len(got))
	}
	farCount := 0
	for _, item := range got {
		if item.Hops >= 2 {
			farCount++
		}
	}
	if farCount == 0 {
		t.Fatalf("expected at least one far endpoint in %+v", got)
	}
}

func TestEvaluateFarEndPassesOnFarHopEndpoints(t *testing.T) {
	report := &E2EReport{
		PropagationAnalysis: &PropagationAnalysisSummary{
			Signals: []*PropagationSignalSummary{
				{
					TopEndpoints: []horizonmodels.NodeImpact{
						{FragmentID: "far", Hops: 2, TotalAmplitude: 0.8},
					},
				},
			},
			NonSeedEndpoints: 1,
		},
	}
	verdict := evaluateFarEnd(report)
	if verdict.Status != verdictPass {
		t.Fatalf("status = %q, want %q", verdict.Status, verdictPass)
	}
}

func TestEvaluateMeaningfulnessPassesForMultiHopWithoutEdgeTrace(t *testing.T) {
	report := &E2EReport{
		PropagationAnalysis: &PropagationAnalysisSummary{
			Signals: []*PropagationSignalSummary{
				{
					TopEndpoints: []horizonmodels.NodeImpact{
						{FragmentID: "far", Hops: 2, TotalAmplitude: 0.8},
					},
					Simulation: &horizonmodels.SimulationResult{
						TracePaths: []horizonmodels.TracePath{
							{
								NodeCanonicalPaths: []string{"/org/us/tesla", "/product/component/energy/assembly/battery-pack", "/org/us/redwood-materials"},
								Amplitudes:         []float64{0.95, 0.8, 0.6},
							},
						},
					},
				},
			},
			MeaningfulFarEnds: 1,
			NonSeedEndpoints:  1,
		},
	}
	verdict := evaluateMeaningfulness(report)
	if verdict.Status != verdictPass {
		t.Fatalf("status = %q, want %q", verdict.Status, verdictPass)
	}
}
