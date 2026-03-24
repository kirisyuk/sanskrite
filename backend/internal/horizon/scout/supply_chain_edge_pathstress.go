package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
)

// applyPathStressToSupplyEdge derives substitutability / throughput hints from structural leg stress
// (pathstress.EvaluateLeg), matching the plan: use dictionary physics when creating supply edges.
func applyPathStressToSupplyEdge(edge *models.DependencyEdge, chainEdge *ChainEdge) {
	if edge == nil || chainEdge == nil {
		return
	}
	leg := models.SupplyChainPathLeg{
		Index:             0,
		SupplierRef:       strings.TrimSpace(chainEdge.SourceCanonicalPath),
		CustomerRef:       strings.TrimSpace(chainEdge.TargetCanonicalPath),
		Mode:              qualifierString(chainEdge.Qualifiers, "mode", "transportMode", "legMode"),
		BindingConfidence: chainEdge.Confidence,
	}
	if chainEdge.Qualifiers != nil {
		if v, ok := chainEdge.Qualifiers["transportVia"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				leg.TransportViaRefs = append(leg.TransportViaRefs, strings.TrimSpace(s))
			}
			if arr, ok := v.([]interface{}); ok {
				for _, x := range arr {
					if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
						leg.TransportViaRefs = append(leg.TransportViaRefs, strings.TrimSpace(s))
					}
				}
			}
		}
	}
	ev := pathstress.NewEvaluator()
	st := ev.EvaluateLeg(leg)
	if st == nil {
		return
	}
	edge.SetSubstitutability(pathStressClamp01(st.Rerouteability))
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers[models.QualifierReroutePenalty] = pathStressClamp01(1.0 - st.Rerouteability)
	// Lower fragility ⇒ higher assumed routed share on this leg.
	edge.Qualifiers[models.QualifierThroughputShare] = pathStressClamp01(1.0 - 0.55*st.Fragility)
	if st.RecoveryLatencyHrs > 0 && edge.Properties.LatencyHours > 0 && edge.Properties.LatencyHours == 24 {
		lh := int(st.RecoveryLatencyHrs)
		if lh > 720 {
			lh = 720
		}
		if lh > edge.Properties.LatencyHours {
			edge.Properties.LatencyHours = lh
		}
	}
}

func qualifierString(q map[string]interface{}, keys ...string) string {
	if q == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := q[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func pathStressClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
