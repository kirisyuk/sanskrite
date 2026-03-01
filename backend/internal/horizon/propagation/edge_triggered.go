package propagation

import (
	"context"
	"math"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EdgeTriggeredSignal represents an accumulated signal on a fragment endpoint
// that should be propagated along a newly created edge.
type EdgeTriggeredSignal struct {
	FragmentID string
	MetricName string
	Amplitude  float64
	Axis       string
	Polarity   string
}

// CollectEdgeTriggeredSignals inspects both endpoints of a new edge and collects
// signals with amplitude above MinAmplitudeThreshold. These signals should be
// re-propagated along the new edge asynchronously.
func CollectEdgeTriggeredSignals(
	ctx context.Context,
	edge *horizonmodels.DependencyEdge,
	sourceFragment, targetFragment *horizonmodels.HorizonFragment,
) []EdgeTriggeredSignal {
	if edge == nil {
		return nil
	}
	var signals []EdgeTriggeredSignal

	if sourceFragment != nil && len(sourceFragment.State) > 0 {
		for metricKey, mv := range sourceFragment.State {
			amp := math.Abs(mv.Value)
			if amp < MinAmplitudeThreshold {
				continue
			}
			axis, polarity := parseAxisPolarityFromMetric(metricKey)
			if axis == "" {
				continue
			}
			signals = append(signals, EdgeTriggeredSignal{
				FragmentID: sourceFragment.ID,
				MetricName: metricKey,
				Amplitude:  mv.Value,
				Axis:       axis,
				Polarity:   polarity,
			})
		}
	}

	if targetFragment != nil && len(targetFragment.State) > 0 {
		for metricKey, mv := range targetFragment.State {
			amp := math.Abs(mv.Value)
			if amp < MinAmplitudeThreshold {
				continue
			}
			axis, polarity := parseAxisPolarityFromMetric(metricKey)
			if axis == "" {
				continue
			}
			signals = append(signals, EdgeTriggeredSignal{
				FragmentID: targetFragment.ID,
				MetricName: metricKey,
				Amplitude:  mv.Value,
				Axis:       axis,
				Polarity:   polarity,
			})
		}
	}

	return signals
}

// DeduplicateEdgeTriggeredSignals removes duplicate signals by (fragmentID, metricKey),
// keeping the one with higher absolute amplitude.
func DeduplicateEdgeTriggeredSignals(signals []EdgeTriggeredSignal) []EdgeTriggeredSignal {
	if len(signals) == 0 {
		return nil
	}
	best := make(map[string]EdgeTriggeredSignal, len(signals))
	for _, s := range signals {
		key := s.FragmentID + "|" + s.MetricName
		if existing, ok := best[key]; !ok || math.Abs(s.Amplitude) > math.Abs(existing.Amplitude) {
			best[key] = s
		}
	}
	out := make([]EdgeTriggeredSignal, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	return out
}

// parseAxisPolarityFromMetric extracts axis and polarity from a metric key.
// Metric keys follow the pattern: "axis/subaxis|polarity|amplitude" or "axis/subaxis".
func parseAxisPolarityFromMetric(metricKey string) (axis, polarity string) {
	metricKey = strings.TrimSpace(metricKey)
	if metricKey == "" {
		return "", ""
	}
	parts := strings.Split(metricKey, "|")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), "positive"
}
