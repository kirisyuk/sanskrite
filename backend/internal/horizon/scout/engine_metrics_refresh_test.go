package scout

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestMetricsRefreshReasons(t *testing.T) {
	now := time.Now().UTC()

	t.Run("missing metric triggers refresh", func(t *testing.T) {
		frag := &models.HorizonFragment{
			State: map[string]models.MetricValue{},
		}
		reasons := evaluateMetricsRefreshContractReasons(frag, []string{"/metric/risk/operational_health"}, now)
		if len(reasons) == 0 {
			t.Fatalf("expected refresh reasons, got none")
		}
	})

	t.Run("low confidence triggers refresh", func(t *testing.T) {
		frag := &models.HorizonFragment{
			State: map[string]models.MetricValue{
				"/metric/risk/operational_health": {
					Value:      0.4,
					Confidence: metricsMinConfidence - 0.01,
					ObservedAt: now,
				},
			},
		}
		reasons := evaluateMetricsRefreshContractReasons(frag, []string{"/metric/risk/operational_health"}, now)
		if len(reasons) == 0 {
			t.Fatalf("expected refresh reasons, got none")
		}
	})

	t.Run("stale observedAt triggers refresh", func(t *testing.T) {
		frag := &models.HorizonFragment{
			State: map[string]models.MetricValue{
				"/metric/risk/operational_health": {
					Value:      0.4,
					Confidence: metricsMinConfidence + 0.1,
					ObservedAt: now.Add(-metricsMaxAge - time.Hour),
				},
			},
		}
		reasons := evaluateMetricsRefreshContractReasons(frag, []string{"/metric/risk/operational_health"}, now)
		if len(reasons) == 0 {
			t.Fatalf("expected refresh reasons, got none")
		}
	})

	t.Run("fresh complete metrics skip refresh", func(t *testing.T) {
		frag := &models.HorizonFragment{
			State: map[string]models.MetricValue{
				"/metric/risk/operational_health": {
					Value:      0.4,
					Confidence: metricsMinConfidence + 0.1,
					ObservedAt: now.Add(-12 * time.Hour),
				},
			},
		}
		reasons := evaluateMetricsRefreshContractReasons(frag, []string{"/metric/risk/operational_health"}, now)
		if len(reasons) != 0 {
			t.Fatalf("expected no refresh reasons, got %v", reasons)
		}
	})
}
