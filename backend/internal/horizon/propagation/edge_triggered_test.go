package propagation

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestCollectEdgeTriggeredSignals(t *testing.T) {
	ctx := context.Background()

	edge := &horizonmodels.DependencyEdge{
		SourceFragmentID: "src-id",
		TargetFragmentID: "tgt-id",
	}

	t.Run("nil edge", func(t *testing.T) {
		signals := CollectEdgeTriggeredSignals(ctx, nil, nil, nil)
		if len(signals) != 0 {
			t.Errorf("expected 0 signals, got %d", len(signals))
		}
	})

	t.Run("no fragments", func(t *testing.T) {
		signals := CollectEdgeTriggeredSignals(ctx, edge, nil, nil)
		if len(signals) != 0 {
			t.Errorf("expected 0 signals, got %d", len(signals))
		}
	})

	t.Run("source with signals above threshold", func(t *testing.T) {
		src := &horizonmodels.HorizonFragment{
			ID: "src-id",
			State: map[string]horizonmodels.MetricValue{
				"physical_flow/supply|positive|amplitude": {Value: 0.7},
				"logistics/capacity|negative|amplitude":   {Value: -0.3},
			},
		}
		signals := CollectEdgeTriggeredSignals(ctx, edge, src, nil)
		if len(signals) != 2 {
			t.Errorf("expected 2 signals, got %d", len(signals))
		}
		for _, s := range signals {
			if s.FragmentID != "src-id" {
				t.Errorf("expected fragmentID=src-id, got %q", s.FragmentID)
			}
		}
	})

	t.Run("below threshold filtered out", func(t *testing.T) {
		src := &horizonmodels.HorizonFragment{
			ID: "src-id",
			State: map[string]horizonmodels.MetricValue{
				"physical_flow/supply|positive|amplitude": {Value: 0.01},
			},
		}
		signals := CollectEdgeTriggeredSignals(ctx, edge, src, nil)
		if len(signals) != 0 {
			t.Errorf("expected 0 signals (below threshold), got %d", len(signals))
		}
	})

	t.Run("both endpoints have signals", func(t *testing.T) {
		src := &horizonmodels.HorizonFragment{
			ID: "src-id",
			State: map[string]horizonmodels.MetricValue{
				"physical_flow/supply|positive|amplitude": {Value: 0.6},
			},
		}
		tgt := &horizonmodels.HorizonFragment{
			ID: "tgt-id",
			State: map[string]horizonmodels.MetricValue{
				"legal/access|positive|amplitude": {Value: 0.4},
			},
		}
		signals := CollectEdgeTriggeredSignals(ctx, edge, src, tgt)
		if len(signals) != 2 {
			t.Errorf("expected 2 signals, got %d", len(signals))
		}
	})
}

func TestDeduplicateEdgeTriggeredSignals(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		out := DeduplicateEdgeTriggeredSignals(nil)
		if len(out) != 0 {
			t.Errorf("expected 0, got %d", len(out))
		}
	})

	t.Run("deduplicates keeping higher amplitude", func(t *testing.T) {
		signals := []EdgeTriggeredSignal{
			{FragmentID: "f1", MetricName: "m1", Amplitude: 0.3},
			{FragmentID: "f1", MetricName: "m1", Amplitude: 0.7},
			{FragmentID: "f2", MetricName: "m1", Amplitude: 0.5},
		}
		out := DeduplicateEdgeTriggeredSignals(signals)
		if len(out) != 2 {
			t.Errorf("expected 2 unique signals, got %d", len(out))
		}
		for _, s := range out {
			if s.FragmentID == "f1" && s.Amplitude != 0.7 {
				t.Errorf("expected f1 to keep amplitude 0.7, got %v", s.Amplitude)
			}
		}
	})
}

func TestParseAxisPolarityFromMetric(t *testing.T) {
	tests := []struct {
		metric       string
		wantAxis     string
		wantPolarity string
	}{
		{"physical_flow/supply|positive|amplitude", "physical_flow/supply", "positive"},
		{"logistics/capacity|negative|amplitude", "logistics/capacity", "negative"},
		{"simple_axis", "simple_axis", "positive"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			axis, polarity := parseAxisPolarityFromMetric(tt.metric)
			if axis != tt.wantAxis {
				t.Errorf("axis = %q, want %q", axis, tt.wantAxis)
			}
			if polarity != tt.wantPolarity {
				t.Errorf("polarity = %q, want %q", polarity, tt.wantPolarity)
			}
		})
	}
}
