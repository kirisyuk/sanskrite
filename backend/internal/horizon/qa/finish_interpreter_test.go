package qa

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestRunFinishInterpretation_NilInputs(t *testing.T) {
	ctx := context.Background()

	// nil interpreter → nil
	out := RunFinishInterpretation(ctx, nil, &horizonmodels.SimulationResult{NodeImpacts: []horizonmodels.NodeImpact{{FragmentID: "f1"}}}, nil, "risk", 5)
	if out != nil {
		t.Errorf("expected nil when interpreter is nil, got %d items", len(out))
	}

	// nil simResult → nil
	out = RunFinishInterpretation(ctx, nil, nil, nil, "risk", 5)
	if out != nil {
		t.Errorf("expected nil when simResult is nil, got %d items", len(out))
	}

	// empty NodeImpacts → nil
	out = RunFinishInterpretation(ctx, nil, &horizonmodels.SimulationResult{}, nil, "risk", 5)
	if out != nil {
		t.Errorf("expected nil when NodeImpacts empty, got %d items", len(out))
	}
}
