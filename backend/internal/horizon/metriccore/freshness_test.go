package metriccore

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEvaluateRefreshReasonsForState(t *testing.T) {
	now := time.Now().UTC()

	t.Run("missing metric triggers refresh", func(t *testing.T) {
		reasons := EvaluateRefreshReasonsForState(map[string]horizonmodels.MetricValue{}, RequirementSet([]string{"/metric/risk/operational_health"}, 0, 0), now, DefaultRefreshPolicy())
		if len(reasons) == 0 {
			t.Fatal("expected refresh reasons")
		}
	})

	t.Run("low confidence triggers refresh", func(t *testing.T) {
		state := map[string]horizonmodels.MetricValue{
			"/metric/risk/operational_health": {
				Value:      0.4,
				Confidence: DefaultMetricsMinConfidence - 0.01,
				ObservedAt: now,
			},
		}
		reasons := EvaluateRefreshReasonsForState(state, RequirementSet([]string{"/metric/risk/operational_health"}, 0, 0), now, DefaultRefreshPolicy())
		if len(reasons) == 0 {
			t.Fatal("expected refresh reasons")
		}
	})

	t.Run("fresh until overrides age", func(t *testing.T) {
		freshUntil := now.Add(4 * time.Hour)
		state := map[string]horizonmodels.MetricValue{
			"/metric/risk/operational_health": {
				Value:      0.4,
				Confidence: DefaultMetricsMinConfidence + 0.1,
				ObservedAt: now.Add(-90 * 24 * time.Hour),
				FreshUntil: &freshUntil,
			},
		}
		reasons := EvaluateRefreshReasonsForState(state, RequirementSet([]string{"/metric/risk/operational_health"}, 0, 0), now, DefaultRefreshPolicy())
		if len(reasons) != 0 {
			t.Fatalf("expected no refresh reasons, got %v", reasons)
		}
	})

	t.Run("expired freshUntil triggers refresh", func(t *testing.T) {
		freshUntil := now.Add(-time.Hour)
		state := map[string]horizonmodels.MetricValue{
			"/metric/risk/operational_health": {
				Value:      0.4,
				Confidence: DefaultMetricsMinConfidence + 0.1,
				ObservedAt: now.Add(-12 * time.Hour),
				FreshUntil: &freshUntil,
			},
		}
		reasons := EvaluateRefreshReasonsForState(state, RequirementSet([]string{"/metric/risk/operational_health"}, 0, 0), now, DefaultRefreshPolicy())
		if len(reasons) == 0 {
			t.Fatal("expected refresh reasons")
		}
	})
}

func TestBetterMetricValuePrefersFreshUntilAndConfidence(t *testing.T) {
	now := time.Now().UTC()
	existing := horizonmodels.MetricValue{
		Value:      0.5,
		Confidence: 0.72,
		ObservedAt: now.Add(-6 * time.Hour),
	}
	freshUntil := now.Add(12 * time.Hour)
	incoming := horizonmodels.MetricValue{
		Value:      0.52,
		Confidence: 0.72,
		ObservedAt: now.Add(-2 * time.Hour),
		FreshUntil: &freshUntil,
	}
	if !BetterMetricValue(existing, incoming) {
		t.Fatal("expected incoming metric to be preferred")
	}
}
