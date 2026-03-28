package horizon

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestNormalizeCreateLensFromQueryMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", createLensFromQueryModeResearchFirst},
		{"research_first", createLensFromQueryModeResearchFirst},
		{"hot_path", createLensFromQueryModeHotPath},
		{"HOT_PATH", createLensFromQueryModeHotPath},
		{"just_open", createLensFromQueryModeJustOpen},
		{"unknown", createLensFromQueryModeResearchFirst},
	}
	for _, tt := range tests {
		if got := normalizeCreateLensFromQueryMode(tt.in); got != tt.want {
			t.Fatalf("normalizeCreateLensFromQueryMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestForceIntentHotPathOnly(t *testing.T) {
	intent := &agents.IntentResolution{
		Domains: []agents.DomainResolution{
			{
				CanonicalPath: "/domain/v1/supply-chain/geo/qa/product/lng",
				ContractType:  "supply-chain",
				SupplyChainExecutionPlan: &agents.SupplyChainExecutionPlan{
					GraphBuildMode: agents.SupplyChainGraphBuildModeHotPathPlusGraph,
				},
			},
			{
				CanonicalPath: "/domain/v1/regulatory-impact/geo/qa/subject/lng",
				ContractType:  "regulatory-impact",
			},
		},
	}
	out := forceIntentHotPathOnly(intent)
	if out == intent {
		t.Fatal("expected copy, got same pointer")
	}
	if got := out.Domains[0].SupplyChainExecutionPlan.GraphBuildMode; got != agents.SupplyChainGraphBuildModeHotPathOnly {
		t.Fatalf("supply chain graphBuildMode = %q, want %q", got, agents.SupplyChainGraphBuildModeHotPathOnly)
	}
	if out.Domains[1].SupplyChainExecutionPlan != nil {
		t.Fatal("non-supply-chain domain must not receive a synthetic supply-chain plan")
	}
}

func TestMarkLensHotPathRuntime(t *testing.T) {
	lens := &horizonmodels.Lens{}
	plan := horizonscout.ExecutionPlanContract{Version: "v1", Mode: "supply_chain", MaxAnchors: 4, MaxRootJobs: 4, MaxRecursionDepth: 2, MaxChildrenPerJob: 4, MaxCascadeJobs: 12, MaxSupplyChainQueries: 2}
	markLensHotPathRuntime(lens, plan)
	if !isHotPathRuntimeLens(lens) {
		t.Fatal("expected lens to be marked as hot path runtime")
	}
	if lens.ViewConfig == nil {
		t.Fatal("expected viewConfig")
	}
	if _, ok := lens.ViewConfig[horizonscout.ExecutionPlanStateKey]; !ok {
		t.Fatal("expected executionPlan in viewConfig")
	}
}

func TestIsTerminalHotPathStatus(t *testing.T) {
	tests := map[string]bool{
		"answered":   true,
		"ready":      true,
		"partial":    true,
		"failed":     true,
		"assembling": false,
		"simulating": false,
		"":           false,
	}
	for in, want := range tests {
		if got := isTerminalHotPathStatus(in); got != want {
			t.Fatalf("isTerminalHotPathStatus(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestHotPathArtifactScopeCount(t *testing.T) {
	lens := &horizonmodels.Lens{
		Scope: []string{
			"/artifact/supply-chain/global/a",
			"/org/example",
		},
		ScopeArtifacts: map[string]horizonmodels.ScopeArtifactRef{
			"/artifact/supply-chain/global/b": {Kind: string(horizonmodels.MaterializedKindSupplyChainPath)},
			"/artifact/other/global/c":        {Kind: "OTHER"},
		},
	}
	if got := hotPathArtifactScopeCount(lens); got != 2 {
		t.Fatalf("hotPathArtifactScopeCount() = %d, want 2", got)
	}
	if got := hotPathArtifactScopeCount(&horizonmodels.Lens{ViewConfig: map[string]any{"artifactScopeAddCount": float64(3)}}); got != 3 {
		t.Fatalf("hotPathArtifactScopeCount(viewConfig) = %d, want 3", got)
	}
}
