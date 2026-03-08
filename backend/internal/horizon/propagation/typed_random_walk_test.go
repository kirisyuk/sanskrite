package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestDefaultHeuristicSignalKernelConfigUsesHybridForBusinessAxes(t *testing.T) {
	cfg := DefaultHeuristicSignalKernelConfig(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict})
	if cfg.Mode != SignalKernelModeHybrid {
		t.Fatalf("expected hybrid mode for geo/conflict, got %q", cfg.Mode)
	}
	cfg = DefaultHeuristicSignalKernelConfig(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisInfoUncertainty})
	if cfg.Mode != SignalKernelModeBounded {
		t.Fatalf("expected bounded mode for info/uncertainty, got %q", cfg.Mode)
	}
}

func TestRunTypedRandomWalkPrefersBusinessTail(t *testing.T) {
	seed := signalStateKey{FragmentID: "seed", Axis: horizonmodels.AxisGeoConflict}
	factory := signalStateKey{FragmentID: "factory", Axis: horizonmodels.AxisGeoConflict}
	gov := signalStateKey{FragmentID: "gov", Axis: horizonmodels.AxisGeoConflict}
	graph := &signalStateGraph{
		Nodes: map[signalStateKey]*signalStateNode{
			seed:    {Key: seed},
			factory: {Key: factory},
			gov:     {Key: gov},
		},
		Transitions: map[signalStateKey][]signalStateTransition{
			seed: {
				{To: factory, Weight: 1.4},
				{To: gov, Weight: 0.4},
			},
		},
	}
	selected, scores := runTypedRandomWalk(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict}, graph, []signalStateKey{seed}, SignalKernelConfig{
		Mode:               SignalKernelModeTypedRWR,
		RestartProbability: 0.35,
		MaxIterations:      18,
		MinTailScore:       0.05,
		MaxTailNodes:       2,
		IncludeSeedNodes:   true,
	})
	if len(selected) < 2 {
		t.Fatalf("expected seed and at least one tail, got %d selections", len(selected))
	}
	if scores[factory] <= scores[gov] {
		t.Fatalf("expected factory score %.4f to exceed gov score %.4f", scores[factory], scores[gov])
	}
}

func TestMergeSignalSimulationResultsKeepsStrongerAccumulatedAmplitude(t *testing.T) {
	base := &SignalSimulationResult{
		AffectedFragments: []string{"a"},
		Accumulated: map[string][]AccumulatedSignal{
			"a": {{Axis: horizonmodels.AxisGeoConflict, Polarity: "risk", Amplitude: 0.3, HopCount: 2}},
		},
	}
	tail := &SignalSimulationResult{
		AffectedFragments: []string{"a", "b"},
		Accumulated: map[string][]AccumulatedSignal{
			"a": {{Axis: horizonmodels.AxisGeoConflict, Polarity: "risk", Amplitude: 0.5, HopCount: 3}},
			"b": {{Axis: horizonmodels.AxisLogisticsCapacity, Polarity: "risk", Amplitude: 0.4, HopCount: 2}},
		},
	}
	merged := mergeSignalSimulationResults(base, tail)
	if len(merged.AffectedFragments) != 2 {
		t.Fatalf("expected 2 affected fragments, got %d", len(merged.AffectedFragments))
	}
	if got := merged.Accumulated["a"][0].Amplitude; got != 0.5 {
		t.Fatalf("expected stronger amplitude to win, got %.2f", got)
	}
}
