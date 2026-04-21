package repo

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func edgeFacetString(edge *models.DependencyEdge) string {
	if edge == nil || edge.Qualifiers == nil {
		return ""
	}
	if raw, ok := edge.Qualifiers["facet"]; ok {
		if s, ok := raw.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func edgeMetricValuesAsFloats(metrics map[string]models.MetricValue) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	out := make(map[string]float64, len(metrics))
	for key, mv := range metrics {
		out[key] = mv.Value
	}
	return out
}
