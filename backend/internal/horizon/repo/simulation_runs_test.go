package repo

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestSimulationRunLensMatches(t *testing.T) {
	t.Parallel()

	run := &horizonmodels.SimulationRun{}
	run.Request.LensID = "lens-request"
	run.BaselineRefs.LensID = "lens-baseline"

	if !simulationRunLensMatches(run, "lens-request") {
		t.Fatalf("expected match by request lens id")
	}
	if !simulationRunLensMatches(run, "lens-baseline") {
		t.Fatalf("expected match by baseline lens id")
	}
	if simulationRunLensMatches(run, "other-lens") {
		t.Fatalf("did not expect unrelated lens id to match")
	}
}
