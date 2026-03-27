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
}
