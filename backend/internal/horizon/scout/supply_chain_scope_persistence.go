package scout

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
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
		qualifiers := buildCapabilityQualifiersFromSupplyLink(frag, seg)
		delete(qualifiers, "carrierType")
		metrics := map[string]models.MetricValue{}
		for _, key := range []string{"customs_clearance_days", "route_fragility", "route_rerouteability", "supply_chain_criticality"} {
			if metric := lookupMetricValue(frag.State, key); metric != nil {
				metrics[key] = *metric
			}
		}
		_, _ = e.routeObsRepo.Create(ctx, &models.RouteObservation{
			TenantID:                strings.TrimSpace(frag.TenantID),
			SupplyLinkCanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			ProductRef:              strings.TrimSpace(seg.ProductRef),
			TransportViaRefs:        append([]string(nil), seg.TransportViaRefs...),
			Metrics:                 metrics,
			Qualifiers:              qualifiers,
			SourceIDs:               sourceIDs,
			ObservedAt:              now,
		})
	}
	supplierRef := strings.TrimSpace(seg.SupplierRef)
	var capabilityProfile *models.CapabilityProfile
	if e.capabilityProfilesRepo != nil && supplierRef != "" && strings.TrimSpace(seg.ProductRef) != "" {
		existingProfile, _ := e.capabilityProfilesRepo.Get(ctx, supplierRef, strings.TrimSpace(seg.ProductRef))
		metrics, _ := extractCapabilityMetricsFromSupplyLink(frag, seg)
		qualifiers := buildCapabilityQualifiersFromSupplyLink(frag, seg)
		capabilityProfile = mergeCapabilityProfileObservation(existingProfile, strings.TrimSpace(frag.TenantID), supplierRef, strings.TrimSpace(seg.ProductRef), metrics, qualifiers)
		if capabilityProfile != nil && (len(capabilityProfile.Metrics) > 0 || len(capabilityProfile.Qualifiers) > 0) {
			_ = e.capabilityProfilesRepo.Upsert(ctx, capabilityProfile)
		}
		if e.capabilityObsRepo != nil && (len(metrics) > 0 || len(qualifiers) > 0) {
			_, _ = e.capabilityObsRepo.Create(ctx, &models.CapabilityObservation{
				TenantID:   strings.TrimSpace(frag.TenantID),
				CarrierRef: supplierRef,
				ProductRef: strings.TrimSpace(seg.ProductRef),
				Metrics:    cloneMetricValuesForPersistence(metrics),
				Qualifiers: cloneInterfaceMapForPersistence(qualifiers),
				SourceIDs:  sourceIDs,
				ObservedAt: now,
			})
		}
	}
	if e.derivedRiskProfilesRepo != nil {
		rollup := computeSupplyLinkRiskRollup(frag, seg, capabilityProfile)
		for _, profile := range buildDerivedRiskProfiles(frag, seg, rollup) {
			_ = e.derivedRiskProfilesRepo.Upsert(ctx, profile)
		}
	}
	if updatedScope := markWorkingProductCoverage(job, strings.TrimSpace(seg.ProductRef), "partial", models.WorkingProductStatusConfirmed); updatedScope != nil {
		persistWorkingProductScope(ctx, e, job, updatedScope)
	}
}

func persistScopedObservationsFromChains(ctx context.Context, e *ScoutEngine, job *models.ScoutingJob, chains []agents.SupplyChain) {
	if e == nil || len(chains) == 0 {
		return
	}
	sourceIDs := []string{}
	if job != nil && strings.TrimSpace(job.ID) != "" {
		sourceIDs = append(sourceIDs, strings.TrimSpace(job.ID))
	}
	for _, chain := range chains {
		for _, obs := range chain.CarrierProductObservations {
			carrierRef := strings.TrimSpace(obs.CarrierRef)
			productRef := strings.TrimSpace(obs.ProductRef)
			if carrierRef == "" || productRef == "" {
				continue
			}
			metrics := metricValuesFromLooseMap(obs.Metrics, obs.Confidence)
			qualifiers := cloneInterfaceMapForPersistence(obs.Qualifiers)
			if e.capabilityObsRepo != nil {
				_, _ = e.capabilityObsRepo.Create(ctx, &models.CapabilityObservation{
					TenantID:   tenantIDFromJob(job),
					CarrierRef: carrierRef,
					ProductRef: productRef,
					Metrics:    metrics,
					Qualifiers: qualifiers,
					SourceIDs:  sourceIDs,
					ObservedAt: parseObservedAtOrNow(obs.ObservedAt),
				})
			}
			if e.capabilityProfilesRepo != nil {
				existing, _ := e.capabilityProfilesRepo.Get(ctx, carrierRef, productRef)
				profile := mergeCapabilityProfileObservation(existing, tenantIDFromJob(job), carrierRef, productRef, metrics, qualifiers)
				if profile != nil {
					_ = e.capabilityProfilesRepo.Upsert(ctx, profile)
				}
			}
			if updatedScope := markWorkingProductCoverage(job, productRef, "partial", models.WorkingProductStatusConfirmed); updatedScope != nil {
				persistWorkingProductScope(ctx, e, job, updatedScope)
			}
		}
		for _, obs := range chain.LinkObservations {
			productRef := strings.TrimSpace(obs.ProductRef)
			if productRef == "" || e.supplyLinkObsRepo == nil {
				continue
			}
			_, _ = e.supplyLinkObsRepo.Create(ctx, &models.SupplyLinkObservation{
				TenantID:                tenantIDFromJob(job),
				SupplyLinkCanonicalPath: scopedObservationSupplyLinkKey(obs.SupplierRef, obs.CustomerRef, productRef),
				ProductRef:              productRef,
				Metrics:                 metricValuesFromLooseMap(obs.Metrics, obs.Confidence),
				Qualifiers:              cloneInterfaceMapForPersistence(obs.Qualifiers),
				SourceIDs:               sourceIDs,
				ObservedAt:              parseObservedAtOrNow(obs.ObservedAt),
			})
			if updatedScope := markWorkingProductCoverage(job, productRef, "partial", models.WorkingProductStatusConfirmed); updatedScope != nil {
				persistWorkingProductScope(ctx, e, job, updatedScope)
			}
		}
		for _, obs := range chain.RouteObservations {
			if e.routeObsRepo == nil {
				continue
			}
			productRef := strings.TrimSpace(obs.ProductRef)
			transportViaRefs := normalizeRouteObservationViaRefs(obs)
			qualifiers := cloneInterfaceMapForPersistence(obs.Qualifiers)
			if qualifiers == nil {
				qualifiers = map[string]interface{}{}
			}
			if vesselType := strings.TrimSpace(obs.VesselType); vesselType != "" {
				qualifiers["vesselType"] = vesselType
			}
			_, _ = e.routeObsRepo.Create(ctx, &models.RouteObservation{
				TenantID:                tenantIDFromJob(job),
				SupplyLinkCanonicalPath: scopedObservationSupplyLinkKey(obs.SupplierRef, obs.CustomerRef, productRef),
				ProductRef:              productRef,
				TransportViaRefs:        transportViaRefs,
				Qualifiers:              qualifiers,
				SourceIDs:               sourceIDs,
				ObservedAt:              parseObservedAtOrNow(obs.ObservedAt),
			})
		}
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

func effectiveVesselType(_ *SupplyLinkSegment, frag *models.HorizonFragment) string {
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

func metricValuesFromLooseMap(raw map[string]interface{}, confidence float64) map[string]models.MetricValue {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(raw))
	confidence = normalizeLooseObservationConfidence(confidence)
	now := time.Now().UTC()
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if metric, ok := looseMetricValue(value, confidence, now); ok {
			out[key] = metric
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func looseMetricValue(raw interface{}, confidence float64, observedAt time.Time) (models.MetricValue, bool) {
	switch typed := raw.(type) {
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
			return models.MetricValue{Value: typed, Confidence: confidence, ObservedAt: observedAt}, true
		}
	case int:
		return models.MetricValue{Value: float64(typed), Confidence: confidence, ObservedAt: observedAt}, true
	case int64:
		return models.MetricValue{Value: float64(typed), Confidence: confidence, ObservedAt: observedAt}, true
	case map[string]interface{}:
		value, ok := typed["value"]
		if !ok {
			return models.MetricValue{}, false
		}
		mv, ok := looseMetricValue(value, confidence, observedAt)
		if !ok {
			return models.MetricValue{}, false
		}
		if unit := strings.TrimSpace(fmt.Sprint(typed["unit"])); unit != "" && unit != "<nil>" {
			mv.Unit = &unit
		}
		if rawConfidence, ok := typed["confidence"]; ok {
			if c, ok := looseMetricValue(rawConfidence, confidence, observedAt); ok {
				mv.Confidence = c.Value
			}
		}
		return mv, true
	}
	return models.MetricValue{}, false
}

func normalizeLooseObservationConfidence(confidence float64) float64 {
	if confidence <= 0 {
		return 0.6
	}
	if confidence > 1 {
		return 1
	}
	return confidence
}

func parseObservedAtOrNow(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func mergeCapabilityProfileObservation(
	existing *models.CapabilityProfile,
	tenantID, carrierRef, productRef string,
	metrics map[string]models.MetricValue,
	qualifiers map[string]interface{},
) *models.CapabilityProfile {
	var profile models.CapabilityProfile
	if existing != nil {
		profile = *existing
		profile.Metrics = cloneMetricValuesForPersistence(existing.Metrics)
		profile.Qualifiers = cloneInterfaceMapForPersistence(existing.Qualifiers)
	}
	profile.TenantID = strings.TrimSpace(tenantID)
	profile.CarrierRef = strings.TrimSpace(carrierRef)
	profile.ProductRef = strings.TrimSpace(productRef)
	profile.Metrics = mergeMetricMaps(profile.Metrics, metrics)
	if profile.Qualifiers == nil {
		profile.Qualifiers = map[string]interface{}{}
	}
	for key, value := range qualifiers {
		profile.Qualifiers[key] = value
	}
	profile.CoverageStatus = computeCapabilityCoverageStatus(profile.Metrics)
	profile.LastObservedAt = time.Now().UTC()
	profile.LastRolledUpAt = time.Now().UTC()
	return &profile
}

func persistDirectPathArtifactTruth(
	ctx context.Context,
	e *ScoutEngine,
	job *models.ScoutingJob,
	tenantID string,
	canonicalKeys []string,
) {
	if e == nil || e.supplyChainPathBuilder == nil || e.routeObsRepo == nil || len(canonicalKeys) == 0 {
		return
	}
	sourceIDs := []string{}
	if job != nil && strings.TrimSpace(job.ID) != "" {
		sourceIDs = append(sourceIDs, strings.TrimSpace(job.ID))
	}
	for _, canonicalKey := range dedupStrings(canonicalKeys) {
		artifact, err := e.supplyChainPathBuilder.GetChainByCanonicalKey(ctx, tenantID, canonicalKey)
		if err != nil || artifact == nil {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		bottleneckIndex := -1
		if parsed.Stress != nil {
			bottleneckIndex = parsed.Stress.BottleneckLegIndex
		}
		for idx, leg := range parsed.Legs {
			qualifiers := map[string]interface{}{
				"mode":              strings.TrimSpace(leg.Mode),
				"geoRefs":           append([]string(nil), leg.GeoRefs...),
				"flowRefs":          append([]string(nil), leg.FlowRefs...),
				"alternateFlowRefs": append([]string(nil), leg.AlternateFlowRefs...),
				"zoneRefs":          append([]string(nil), leg.ZoneRefs...),
				"affectedZoneRefs":  append([]string(nil), leg.AffectedZoneRefs...),
				"spatialSemantics":  append([]string(nil), leg.SpatialSemantics...),
				"bottleneck":        idx == bottleneckIndex,
			}
			if strings.TrimSpace(leg.VesselType) != "" {
				qualifiers["vesselType"] = strings.TrimSpace(leg.VesselType)
			}
			if leg.Stress != nil && len(leg.Stress.DominantFactors) > 0 {
				qualifiers["dominantFactors"] = append([]string(nil), leg.Stress.DominantFactors...)
			}
			metrics := buildRouteMetricsFromPathLeg(leg)
			legIndex := idx
			_, _ = e.routeObsRepo.Create(ctx, &models.RouteObservation{
				TenantID:                strings.TrimSpace(firstNonEmptyString(tenantID, tenantIDFromJob(job), "global")),
				SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				PathArtifactKey:         strings.TrimSpace(canonicalKey),
				LegIndex:                &legIndex,
				ProductRef:              strings.TrimSpace(leg.ProductRef),
				TransportViaRefs:        append([]string(nil), leg.TransportViaRefs...),
				Metrics:                 metrics,
				Qualifiers:              pruneEmptyInterfaceValues(qualifiers),
				SourceIDs:               sourceIDs,
				ObservedAt:              time.Now().UTC(),
			})
		}
	}
}

func buildRouteMetricsFromPathLeg(leg models.SupplyChainPathLeg) map[string]models.MetricValue {
	out := map[string]models.MetricValue{}
	now := time.Now().UTC()
	if leg.BindingConfidence > 0 {
		out["binding_confidence"] = models.MetricValue{Value: leg.BindingConfidence, Confidence: leg.BindingConfidence, ObservedAt: now}
	}
	if leg.SegmentConfidence > 0 {
		out["segment_confidence"] = models.MetricValue{Value: leg.SegmentConfidence, Confidence: leg.SegmentConfidence, ObservedAt: now}
	}
	if leg.Stress != nil {
		if leg.Stress.Fragility > 0 {
			out["route_fragility"] = models.MetricValue{Value: leg.Stress.Fragility, Confidence: 0.75, ObservedAt: now}
		}
		if leg.Stress.Rerouteability > 0 {
			out["route_rerouteability"] = models.MetricValue{Value: leg.Stress.Rerouteability, Confidence: 0.75, ObservedAt: now}
		}
		if leg.Stress.ZonePressure > 0 {
			out["path_zone_pressure"] = models.MetricValue{Value: leg.Stress.ZonePressure, Confidence: 0.74, ObservedAt: now}
		}
		if leg.Stress.AffectedZonePressure > 0 {
			out["path_affected_pressure"] = models.MetricValue{Value: leg.Stress.AffectedZonePressure, Confidence: 0.74, ObservedAt: now}
		}
		if leg.Stress.RecoveryLatencyHrs > 0 {
			unit := "hours"
			out["recovery_latency_hours"] = models.MetricValue{Value: leg.Stress.RecoveryLatencyHrs, Unit: &unit, Confidence: 0.74, ObservedAt: now}
		}
		if leg.Stress.StressScore > 0 {
			out["path_stress_score"] = models.MetricValue{Value: leg.Stress.StressScore, Confidence: 0.76, ObservedAt: now}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pruneEmptyInterfaceValues(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		case []string:
			if len(typed) == 0 {
				continue
			}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tenantIDFromJob(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	return strings.TrimSpace(job.TenantID)
}

func scopedObservationSupplyLinkKey(supplierRef, customerRef, productRef string) string {
	parts := []string{
		strings.ReplaceAll(strings.TrimSpace(supplierRef), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(productRef), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(customerRef), "/", "_"),
	}
	return strings.Trim(strings.Join(parts, "__"), "_")
}
