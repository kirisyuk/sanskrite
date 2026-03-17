package scout

import (
	"context"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var capabilityMetricKeys = map[string]struct{}{
	"annual_capacity":             {},
	"annual_output":               {},
	"lead_time_days_baseline":     {},
	"lead_time_volatility_days":   {},
	"inventory_days_baseline":     {},
	"on_time_delivery_baseline":   {},
	"yield_ratio":                 {},
	"scrap_rate":                  {},
	"quality_score":               {},
	"purity_percent":              {},
	"available_export_share":      {},
	"utilization_rate":            {},
	"downtime_days_per_year":      {},
	"technical_substitutability":  {},
	"regulatory_substitutability": {},
	"service_criticality":         {},
}

var supplyLinkRiskMetricKeys = map[string]struct{}{
	"confidence":                          {},
	"volume":                              {},
	"share":                               {},
	"lead_time_days":                      {},
	"customs_clearance_days":              {},
	"inventory_cover_days_at_destination": {},
	"switch_time_days":                    {},
	"technical_fit":                       {},
	"regulatory_fit":                      {},
	"fill_rate":                           {},
	"rejection_rate":                      {},
}

func persistSupplyChainScopeArtifacts(ctx context.Context, e *ScoutEngine, job *models.ScoutingJob, frag *models.HorizonFragment, seg *SupplyLinkSegment) {
	if e == nil || frag == nil || seg == nil {
		return
	}
	now := time.Now().UTC()
	sourceIDs := []string{}
	if job != nil && strings.TrimSpace(job.ID) != "" {
		sourceIDs = append(sourceIDs, strings.TrimSpace(job.ID))
	}

	if e.supplyLinkObsRepo != nil {
		_, _ = e.supplyLinkObsRepo.Create(ctx, &models.SupplyLinkObservation{
			TenantID:                strings.TrimSpace(frag.TenantID),
			SupplyLinkCanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			ProductRef:              strings.TrimSpace(seg.ProductRef),
			Metrics:                 cloneMetricValuesForPersistence(frag.State),
			Qualifiers:              cloneInterfaceMapForPersistence(frag.Qualifiers),
			SourceIDs:               sourceIDs,
			ObservedAt:              now,
		})
	}
	if e.routeObsRepo != nil && (len(seg.TransportViaRefs) > 0 || hasVesselType(seg, frag)) {
		qualifiers := map[string]interface{}{}
		if vesselType := effectiveVesselType(seg, frag); vesselType != "" {
			qualifiers["vesselType"] = vesselType
		}
		if seg.SegmentClass != "" {
			qualifiers["segmentClass"] = strings.TrimSpace(seg.SegmentClass)
		}
		_, _ = e.routeObsRepo.Create(ctx, &models.RouteObservation{
			TenantID:                strings.TrimSpace(frag.TenantID),
			SupplyLinkCanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			ProductRef:              strings.TrimSpace(seg.ProductRef),
			TransportViaRefs:        append([]string(nil), seg.TransportViaRefs...),
			Qualifiers:              qualifiers,
			SourceIDs:               sourceIDs,
			ObservedAt:              now,
		})
	}
	supplierRef := strings.TrimSpace(seg.SupplierRef)
	if e.capabilityProfilesRepo != nil && supplierRef != "" && strings.TrimSpace(seg.ProductRef) != "" {
		qualifiers := map[string]interface{}{
			"carrierType": capabilityCarrierType(supplierRef),
		}
		if seg.SegmentClass != "" {
			qualifiers["segmentClass"] = strings.TrimSpace(seg.SegmentClass)
		}
		if vesselType := effectiveVesselType(seg, frag); vesselType != "" {
			qualifiers["vesselType"] = vesselType
		}
		profile := &models.CapabilityProfile{
			TenantID:       strings.TrimSpace(frag.TenantID),
			CarrierRef:     supplierRef,
			ProductRef:     strings.TrimSpace(seg.ProductRef),
			Metrics:        selectMetricKeys(frag.State, capabilityMetricKeys),
			Qualifiers:     qualifiers,
			CoverageStatus: "partial",
			LastObservedAt: now,
			LastRolledUpAt: now,
		}
		_ = e.capabilityProfilesRepo.Upsert(ctx, profile)
		if e.capabilityObsRepo != nil {
			_, _ = e.capabilityObsRepo.Create(ctx, &models.CapabilityObservation{
				TenantID:   strings.TrimSpace(frag.TenantID),
				CarrierRef: supplierRef,
				ProductRef: strings.TrimSpace(seg.ProductRef),
				Metrics:    cloneMetricValuesForPersistence(profile.Metrics),
				Qualifiers: cloneInterfaceMapForPersistence(profile.Qualifiers),
				SourceIDs:  sourceIDs,
				ObservedAt: now,
			})
		}
	}
	if e.derivedRiskProfilesRepo != nil {
		_ = e.derivedRiskProfilesRepo.Upsert(ctx, &models.DerivedRiskProfile{
			TenantID:    strings.TrimSpace(frag.TenantID),
			ScopeRef:    strings.TrimSpace(frag.CanonicalPath),
			ProductRef:  strings.TrimSpace(seg.ProductRef),
			ProfileType: "supply_link_summary",
			Metrics:     selectMetricKeys(frag.State, supplyLinkRiskMetricKeys),
			Qualifiers: map[string]interface{}{
				"segmentClass":      strings.TrimSpace(seg.SegmentClass),
				"transportViaCount": len(seg.TransportViaRefs),
				"hasVesselType":     hasVesselType(seg, frag),
			},
		})
	}
}

func selectMetricKeys(in map[string]models.MetricValue, allowed map[string]struct{}) map[string]models.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue)
	for key, value := range in {
		if _, ok := allowed[strings.TrimSpace(key)]; !ok {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMetricValuesForPersistence(in map[string]models.MetricValue) map[string]models.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInterfaceMapForPersistence(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func effectiveVesselType(seg *SupplyLinkSegment, frag *models.HorizonFragment) string {
	if frag != nil && frag.Qualifiers != nil {
		for _, key := range []string{"vesselType", "vessel_type"} {
			if raw, ok := frag.Qualifiers[key].(string); ok {
				if value := strings.TrimSpace(raw); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func hasVesselType(seg *SupplyLinkSegment, frag *models.HorizonFragment) bool {
	return effectiveVesselType(seg, frag) != ""
}

func capabilityCarrierType(ref string) string {
	switch {
	case strings.HasPrefix(ref, "/fac/"):
		return "facility"
	case strings.HasPrefix(ref, "/org/"), strings.HasPrefix(ref, "/gov/"):
		return "org"
	case strings.HasPrefix(ref, "/deposit/"):
		return "deposit"
	default:
		return "actor"
	}
}
