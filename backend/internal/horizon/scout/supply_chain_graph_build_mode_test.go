package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestDecodeSupplyChainExecutionPlan_NormalizesGraphBuildMode(t *testing.T) {
	raw := map[string]interface{}{
		"graphBuildMode": "HOT_PATH_ONLY",
		"queries": []map[string]interface{}{
			{
				"anchor":     "/org/us/acme",
				"anchorType": "org",
				"direction":  "upstream",
				"depth":      4,
				"reason":     "test",
			},
		},
	}

	plan := decodeSupplyChainExecutionPlan(raw)
	if plan == nil {
		t.Fatalf("expected plan")
	}
	if got := plan.GraphBuildMode; got != agents.SupplyChainGraphBuildModeHotPathOnly {
		t.Fatalf("expected normalized graphBuildMode %q, got %q", agents.SupplyChainGraphBuildModeHotPathOnly, got)
	}
}

func TestSupplyChainGraphBuildMode_PrefersPlanThenStateThenDefault(t *testing.T) {
	job := &models.ScoutingJob{}
	if got := supplyChainGraphBuildMode(nil, job); got != agents.SupplyChainGraphBuildModeHotPathPlusGraph {
		t.Fatalf("expected default mode %q, got %q", agents.SupplyChainGraphBuildModeHotPathPlusGraph, got)
	}

	job.State = map[string]interface{}{"supplyChainGraphBuildMode": "hot_path_only"}
	if got := supplyChainGraphBuildMode(nil, job); got != agents.SupplyChainGraphBuildModeHotPathOnly {
		t.Fatalf("expected state mode %q, got %q", agents.SupplyChainGraphBuildModeHotPathOnly, got)
	}

	plan := &agents.SupplyChainExecutionPlan{GraphBuildMode: "hot_path_plus_graph"}
	if got := supplyChainGraphBuildMode(plan, job); got != agents.SupplyChainGraphBuildModeHotPathPlusGraph {
		t.Fatalf("expected plan mode %q, got %q", agents.SupplyChainGraphBuildModeHotPathPlusGraph, got)
	}
}
