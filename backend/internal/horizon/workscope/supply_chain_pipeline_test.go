package workscope

import "testing"

func TestSupplyChainExecutionPipeline_StageNames(t *testing.T) {
	pipeline := NewSupplyChainExecutionPipeline(WorkScope{})
	want := []string{
		string(ExecutionLayerResolve),
		string(ExecutionLayerCanonicalize),
		string(ExecutionLayerGraphEnrich),
		string(ExecutionLayerMaterializedPaths),
		string(ExecutionLayerRouteOverlays),
		string(ExecutionLayerOperationalHydrate),
		string(ExecutionLayerSignalPropagation),
		string(ExecutionLayerRiskRules),
		string(ExecutionLayerSimulation),
	}
	got := pipeline.StageNames()
	if len(got) != len(want) {
		t.Fatalf("stages=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stages=%v want=%v", got, want)
		}
	}
}
