package pathbuilder

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func PruneAbstractNetworkViaRefs(mode string, flowRefs, viaRefs []string) []string {
	if len(viaRefs) == 0 {
		return nil
	}
	out := make([]string, 0, len(viaRefs))
	for _, ref := range viaRefs {
		if isAbstractTransportPlaceholderRef(ref) {
			continue
		}
		out = append(out, strings.TrimSpace(ref))
	}
	return out
}

func isAbstractTransportPlaceholderRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return false
	}
	if strings.HasSuffix(ref, "-rail-network") {
		return true
	}
	if strings.Contains(ref, "/rail-network") {
		return true
	}
	if strings.HasSuffix(ref, "-distribution-network") {
		return true
	}
	return strings.Contains(ref, "/distribution-network")
}

func collapseAbstractNetworkSegments(segments []PathSegment) []PathSegment {
	if len(segments) < 2 {
		return append([]PathSegment(nil), segments...)
	}
	out := make([]PathSegment, 0, len(segments))
	for idx := 0; idx < len(segments); idx++ {
		current := segments[idx]
		if idx+1 < len(segments) && isAbstractTransportPlaceholderRef(current.CustomerRef) {
			next := segments[idx+1]
			if strings.TrimSpace(next.SupplierRef) == strings.TrimSpace(current.CustomerRef) &&
				strings.TrimSpace(next.ProductRef) == strings.TrimSpace(current.ProductRef) {
				merged := current
				merged.CustomerRef = next.CustomerRef
				merged.CustomerFacilityRef = next.CustomerFacilityRef
				merged.TransportViaRefs = PruneAbstractNetworkViaRefs(
					"",
					nil,
					models.NormalizeTransportViaRefs(append(append([]string(nil), current.TransportViaRefs...), next.TransportViaRefs...)),
				)
				merged.Confidence = minSegmentConfidence(current.Confidence, next.Confidence)
				merged.Metrics = mergeSegmentMetrics(current.Metrics, next.Metrics)
				if strings.TrimSpace(current.SegmentClass) == "" {
					merged.SegmentClass = next.SegmentClass
				}
				if strings.TrimSpace(current.VesselType) == "" {
					merged.VesselType = strings.TrimSpace(next.VesselType)
				}
				if strings.TrimSpace(current.SupplyLinkCanonicalPath) != strings.TrimSpace(next.SupplyLinkCanonicalPath) {
					merged.SupplyLinkID = ""
					merged.SupplyLinkCanonicalPath = ""
					merged.RouteVariantKey = ""
				}
				out = append(out, merged)
				idx++
				continue
			}
		}
		out = append(out, current)
	}
	return out
}

func minSegmentConfidence(left, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	if left < right {
		return left
	}
	return right
}

func mergeSegmentMetrics(left, right map[string]models.MetricValue) map[string]models.MetricValue {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(left)+len(right))
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		if _, ok := out[key]; !ok {
			out[key] = value
		}
	}
	return out
}
