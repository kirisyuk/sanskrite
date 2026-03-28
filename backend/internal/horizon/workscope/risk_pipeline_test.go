package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNewRiskMonitorPipeline_normalizes(t *testing.T) {
	p := NewRiskMonitorPipeline(WorkScope{
		TenantID: " t ",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID: " L1 ",
		},
	})
	if p.Scope.TenantID != "t" || p.Scope.Refs.LensID != "L1" {
		t.Fatalf("scope %+v", p.Scope)
	}
	names := p.StageNames()
	if len(names) < 4 {
		t.Fatalf("stages %v", names)
	}
	foundRouteOverlays := false
	for _, name := range names {
		if name == string(ExecutionLayerRouteOverlays) {
			foundRouteOverlays = true
			break
		}
	}
	if !foundRouteOverlays {
		t.Fatalf("stages %v do not include route_overlays", names)
	}
}
