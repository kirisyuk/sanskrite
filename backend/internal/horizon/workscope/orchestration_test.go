package workscope

import "testing"

func TestBuildExecutionPlanForRequest_defaultsByAction(t *testing.T) {
	request := NewAdHocRequest("tenant", "project", []string{"/org/a"}, ActionHydrateMetrics)
	plan := BuildExecutionPlanForRequest(request)
	want := []ExecutionLayer{
		ExecutionLayerResolve,
		ExecutionLayerMaterializedPaths,
		ExecutionLayerRouteOverlays,
		ExecutionLayerOperationalHydrate,
	}
	if len(plan.Layers) != len(want) {
		t.Fatalf("layers %v want %v", plan.Layers, want)
	}
	for i := range want {
		if plan.Layers[i] != want[i] {
			t.Fatalf("layers %v want %v", plan.Layers, want)
		}
	}
}

func TestBuildExecutionPlanForRequest_requestedLayersStayOrdered(t *testing.T) {
	request := WorkRequest{
		Scope: WorkScope{
			Intent: WorkScopeIntent{
				ScopeKind: ScopeKindAdHoc,
			},
		},
		Intent: ExecutionIntent{
			Action: ActionDeriveRisk,
			RequestedLayers: []ExecutionLayer{
				ExecutionLayerRiskRules,
				ExecutionLayerGraphEnrich,
				ExecutionLayerSignalPropagation,
				ExecutionLayerGraphEnrich,
			},
		},
	}
	plan := BuildExecutionPlanForRequest(request)
	want := []ExecutionLayer{
		ExecutionLayerResolve,
		ExecutionLayerGraphEnrich,
		ExecutionLayerSignalPropagation,
		ExecutionLayerRiskRules,
	}
	if len(plan.Layers) != len(want) {
		t.Fatalf("layers %v want %v", plan.Layers, want)
	}
	for i := range want {
		if plan.Layers[i] != want[i] {
			t.Fatalf("layers %v want %v", plan.Layers, want)
		}
	}
}

func TestDefaultRequestForScope_usesScopeKind(t *testing.T) {
	scope := WorkScope{
		Intent: WorkScopeIntent{
			ScopeKind: ScopeKindSimulationBaseline,
		},
	}
	request := DefaultRequestForScope(scope)
	if request.Intent.EntryPoint != EntryPointSimulation || request.Intent.Action != ActionSimulate {
		t.Fatalf("request intent %+v", request.Intent)
	}
	if !request.Intent.Projection.IncludePropagatedReach {
		t.Fatalf("projection %+v", request.Intent.Projection)
	}
}
