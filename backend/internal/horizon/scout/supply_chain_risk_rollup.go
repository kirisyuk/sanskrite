package scout

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type supplyLinkRiskMechanism struct {
	Key         string
	Label       string
	Score       float64
	MetricKey   string
	MetricLabel string
	MetricValue *models.MetricValue
	Summary     string
}

type supplyLinkRiskRollup struct {
	Mechanisms          []supplyLinkRiskMechanism
	PrimaryMechanism    supplyLinkRiskMechanism
	Criticality         float64
	CoverageStatus      string
	GateStatus          string
	Summary             string
	Flags               []string
	MechanismScores     map[string]float64
	MechanismSummaries  map[string]string
	EffectiveMetrics    map[string]models.MetricValue
	PrimaryMetricKey    string
	PrimaryMetricLabel  string
	PrimaryMetricValue  *models.MetricValue
	RouteFragility      float64
	RouteRerouteability float64
}

func enrichSupplyLinkFragmentWithOperationalRisk(
	ctx context.Context,
	engine *ScoutEngine,
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
	capabilityCache map[string]*models.CapabilityProfile,
) *models.CapabilityProfile {
	if frag == nil || seg == nil {
		return nil
	}
	capability := loadCapabilityProfileForSegment(ctx, engine, strings.TrimSpace(frag.TenantID), seg, capabilityCache)
	rollup := computeSupplyLinkRiskRollup(frag, seg, capability)
	applySupplyLinkRiskRollup(frag, rollup)
	return capability
}

func loadCapabilityProfileForSegment(
	ctx context.Context,
	engine *ScoutEngine,
	tenantID string,
	seg *SupplyLinkSegment,
	cache map[string]*models.CapabilityProfile,
) *models.CapabilityProfile {
	if engine == nil || engine.capabilityProfilesRepo == nil || seg == nil {
		return nil
	}
	carrierRef := strings.TrimSpace(seg.SupplierRef)
	productRef := strings.TrimSpace(seg.ProductRef)
	if carrierRef == "" || productRef == "" {
		return nil
	}
	cacheKey := strings.TrimSpace(tenantID) + "|" + carrierRef + "|" + productRef
	if cache != nil {
		if cached, ok := cache[cacheKey]; ok {
			return cached
		}
	}
	profile, err := engine.capabilityProfilesRepo.Get(ctx, strings.TrimSpace(tenantID), carrierRef, productRef)
	if err != nil {
		return nil
	}
	if cache != nil {
		cache[cacheKey] = profile
	}
	return profile
}

func computeSupplyLinkRiskRollup(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
	capability *models.CapabilityProfile,
) *supplyLinkRiskRollup {
	if frag == nil || seg == nil {
		return nil
	}
	effectiveMetrics := resolveSupplyLinkEffectiveMetrics(frag, seg, capability)
	mechanisms := make([]supplyLinkRiskMechanism, 0, 6)

	if mech := computeSoleSourceMechanism(effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeSubstitutabilityMechanism(effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeInventoryMechanism(effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeCustomsMechanism(effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeRouteMechanism(seg, frag, effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeControlExposureMechanism(frag, capability); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if mech := computeServiceDependencyMechanism(frag, effectiveMetrics); mech.Score > 0 {
		mechanisms = append(mechanisms, mech)
	}
	if len(mechanisms) == 0 {
		return &supplyLinkRiskRollup{
			Criticality:      frag.State["supply_chain_criticality"].Value,
			CoverageStatus:   computeRiskCoverageStatus(effectiveMetrics),
			GateStatus:       "best_effort",
			Summary:          "No deterministic supply-link risk mechanism exceeded threshold.",
			EffectiveMetrics: effectiveMetrics,
		}
	}
	sort.Slice(mechanisms, func(i, j int) bool {
		if mechanisms[i].Score == mechanisms[j].Score {
			return mechanisms[i].Key < mechanisms[j].Key
		}
		return mechanisms[i].Score > mechanisms[j].Score
	})
	primary := mechanisms[0]
	scoreSum := 0.0
	scores := make(map[string]float64, len(mechanisms))
	summaries := make(map[string]string, len(mechanisms))
	flags := make([]string, 0, len(mechanisms))
	for _, mechanism := range mechanisms {
		scoreSum += mechanism.Score
		scores[mechanism.Key] = roundRiskScore(mechanism.Score)
		if strings.TrimSpace(mechanism.Summary) != "" {
			summaries[mechanism.Key] = strings.TrimSpace(mechanism.Summary)
		}
		if mechanism.Score >= 0.45 {
			flags = append(flags, mechanism.Key)
		}
	}
	avgScore := scoreSum / float64(len(mechanisms))
	criticality := clamp01(primary.Score*0.62 + avgScore*0.38)
	routeFragility, routeRerouteability := deriveRouteShape(seg)
	return &supplyLinkRiskRollup{
		Mechanisms:          mechanisms,
		PrimaryMechanism:    primary,
		Criticality:         roundRiskScore(criticality),
		CoverageStatus:      computeRiskCoverageStatus(effectiveMetrics),
		GateStatus:          computeRiskGateStatus(primary.Score, effectiveMetrics),
		Summary:             strings.TrimSpace(primary.Summary),
		Flags:               uniqueSortedStrings(flags),
		MechanismScores:     scores,
		MechanismSummaries:  summaries,
		EffectiveMetrics:    effectiveMetrics,
		PrimaryMetricKey:    primary.MetricKey,
		PrimaryMetricLabel:  primary.MetricLabel,
		PrimaryMetricValue:  cloneMetricValuePtr(primary.MetricValue),
		RouteFragility:      roundRiskScore(routeFragility),
		RouteRerouteability: roundRiskScore(routeRerouteability),
	}
}

func applySupplyLinkRiskRollup(frag *models.HorizonFragment, rollup *supplyLinkRiskRollup) {
	if frag == nil || rollup == nil {
		return
	}
	if frag.State == nil {
		frag.State = make(map[string]models.MetricValue)
	}
	if frag.Qualifiers == nil {
		frag.Qualifiers = make(map[string]interface{})
	}
	now := time.Now().UTC()
	frag.State["supply_chain_criticality"] = models.MetricValue{
		Value:      rollup.Criticality,
		Confidence: 0.78,
		ObservedAt: now,
		Provenance: models.Provenance{Method: "deterministic_supply_link_risk_rollup"},
	}
	if rollup.RouteFragility > 0 {
		frag.State["route_fragility"] = models.MetricValue{
			Value:      rollup.RouteFragility,
			Confidence: 0.72,
			ObservedAt: now,
			Provenance: models.Provenance{Method: "deterministic_supply_link_risk_rollup"},
		}
	}
	if rollup.RouteRerouteability > 0 {
		frag.State["route_rerouteability"] = models.MetricValue{
			Value:      rollup.RouteRerouteability,
			Confidence: 0.72,
			ObservedAt: now,
			Provenance: models.Provenance{Method: "deterministic_supply_link_risk_rollup"},
		}
	}
	frag.Qualifiers["criticalSummary"] = rollup.Summary
	frag.Qualifiers["primaryRiskMechanism"] = rollup.PrimaryMechanism.Key
	frag.Qualifiers["primaryRiskLabel"] = rollup.PrimaryMechanism.Label
	frag.Qualifiers["primaryCriticalMetricKey"] = rollup.PrimaryMetricKey
	frag.Qualifiers["primaryCriticalMetricLabel"] = rollup.PrimaryMetricLabel
	frag.Qualifiers["riskCoverageStatus"] = rollup.CoverageStatus
	frag.Qualifiers["riskGateStatus"] = rollup.GateStatus
	frag.Qualifiers["riskFlags"] = append([]string(nil), rollup.Flags...)
	frag.Qualifiers["riskMechanisms"] = mechanismKeys(rollup.Mechanisms)
	frag.Qualifiers["riskMechanismScores"] = rollup.MechanismScores
	frag.Qualifiers["riskMechanismSummaries"] = rollup.MechanismSummaries
	frag.Qualifiers["routeFragility"] = rollup.RouteFragility
	frag.Qualifiers["routeRerouteability"] = rollup.RouteRerouteability
	if rollup.PrimaryMetricValue != nil {
		frag.Qualifiers["primaryCriticalMetricValue"] = rollup.PrimaryMetricValue.Value
		if rollup.PrimaryMetricValue.Unit != nil && strings.TrimSpace(*rollup.PrimaryMetricValue.Unit) != "" {
			frag.Qualifiers["primaryCriticalMetricUnit"] = strings.TrimSpace(*rollup.PrimaryMetricValue.Unit)
		}
	}
}

func buildCapabilityProfileRollup(
	existing *models.CapabilityProfile,
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
) *models.CapabilityProfile {
	if frag == nil || seg == nil {
		return nil
	}
	carrierRef := strings.TrimSpace(seg.SupplierRef)
	productRef := strings.TrimSpace(seg.ProductRef)
	if carrierRef == "" || productRef == "" {
		return nil
	}
	now := time.Now().UTC()
	var profile models.CapabilityProfile
	if existing != nil {
		profile = *existing
		profile.Metrics = cloneMetricValuesForPersistence(existing.Metrics)
		profile.Qualifiers = cloneInterfaceMapForPersistence(existing.Qualifiers)
	}
	profile.TenantID = strings.TrimSpace(frag.TenantID)
	profile.CarrierRef = carrierRef
	profile.ProductRef = productRef
	if profile.Qualifiers == nil {
		profile.Qualifiers = make(map[string]interface{})
	}
	qualifiers := buildCapabilityQualifiersFromSupplyLink(frag, seg)
	for key, value := range qualifiers {
		profile.Qualifiers[key] = value
	}
	metrics, bootstrapUsed := extractCapabilityMetricsFromSupplyLink(frag, seg)
	profile.Metrics = mergeMetricMaps(profile.Metrics, metrics)
	profile.CoverageStatus = computeCapabilityCoverageStatus(profile.Metrics)
	if bootstrapUsed && len(selectMetricKeys(frag.State, capabilityMetricKeys)) == 0 {
		profile.Qualifiers["bootstrapSource"] = "supply_link"
		if profile.CoverageStatus == "empty" {
			profile.CoverageStatus = "bootstrap"
		}
	}
	profile.LastObservedAt = maxTime(profile.LastObservedAt, now)
	profile.LastRolledUpAt = now
	return &profile
}

func extractCapabilityMetricsFromSupplyLink(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
) (map[string]models.MetricValue, bool) {
	metrics := selectMetricKeys(nilSafeState(frag), capabilityMetricKeys)
	bootstrap := capabilityBootstrapMetricsFromSupplyLink(frag, seg)
	bootstrapUsed := len(bootstrap) > 0
	metrics = mergeMetricMaps(metrics, bootstrap)
	return metrics, bootstrapUsed
}

func buildCapabilityQualifiersFromSupplyLink(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
) map[string]interface{} {
	qualifiers := map[string]interface{}{
		"carrierType": capabilityCarrierType(strings.TrimSpace(seg.SupplierRef)),
	}
	if strings.TrimSpace(seg.SegmentClass) != "" {
		qualifiers["segmentClass"] = strings.TrimSpace(seg.SegmentClass)
	}
	if vesselType := effectiveVesselType(seg, frag); vesselType != "" {
		qualifiers["vesselType"] = vesselType
	}
	if frag != nil && frag.Qualifiers != nil {
		for _, key := range []string{
			"service_contract_required",
			"approved_vendor_only",
			"export_license_required",
			"license_required",
			"sanctions_touchpoint",
			"dual_use_flag",
			"packaging_mode",
			"purity_band",
		} {
			if value, ok := frag.Qualifiers[key]; ok {
				qualifiers[key] = value
			}
		}
	}
	return qualifiers
}

func capabilityBootstrapMetricsFromSupplyLink(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
) map[string]models.MetricValue {
	out := make(map[string]models.MetricValue)
	addBootstrapMetric(out, frag, seg, "lead_time_days", "lead_time_days_baseline")
	addBootstrapMetric(out, frag, seg, "on_time_delivery", "on_time_delivery_baseline")
	if len(out) == 0 {
		return nil
	}
	return out
}

func addBootstrapMetric(
	out map[string]models.MetricValue,
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
	sourceKey, targetKey string,
) {
	if _, exists := out[targetKey]; exists {
		return
	}
	var metric *models.MetricValue
	if frag != nil {
		metric = lookupMetricValue(frag.State, sourceKey)
	}
	if metric == nil && seg != nil {
		metric = lookupMetricValue(seg.State, sourceKey)
	}
	if metric == nil {
		return
	}
	cloned := *metric
	if cloned.Provenance.Method == "" {
		cloned.Provenance.Method = "supply_link_capability_bootstrap"
	}
	out[targetKey] = cloned
}

func nilSafeState(frag *models.HorizonFragment) map[string]models.MetricValue {
	if frag == nil {
		return nil
	}
	return frag.State
}

func buildDerivedRiskProfiles(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
	rollup *supplyLinkRiskRollup,
) []*models.DerivedRiskProfile {
	if frag == nil || seg == nil || rollup == nil {
		return nil
	}
	now := time.Now().UTC()
	base := func(profileType string) *models.DerivedRiskProfile {
		return &models.DerivedRiskProfile{
			TenantID:    strings.TrimSpace(frag.TenantID),
			ScopeRef:    strings.TrimSpace(frag.CanonicalPath),
			ProductRef:  strings.TrimSpace(seg.ProductRef),
			ProfileType: profileType,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	out := make([]*models.DerivedRiskProfile, 0, 5)

	summary := base("supply_link_risk")
	summary.Metrics = map[string]models.MetricValue{
		"supply_chain_criticality": frag.State["supply_chain_criticality"],
	}
	if mv, ok := rollup.EffectiveMetrics["lead_time_days"]; ok {
		summary.Metrics["lead_time_days"] = mv
	}
	if mv, ok := rollup.EffectiveMetrics["inventory_cover_days_at_destination"]; ok {
		summary.Metrics["inventory_cover_days_at_destination"] = mv
	}
	summary.Qualifiers = map[string]interface{}{
		"criticalSummary":          rollup.Summary,
		"primaryRiskMechanism":     rollup.PrimaryMechanism.Key,
		"primaryCriticalMetricKey": rollup.PrimaryMetricKey,
		"riskFlags":                append([]string(nil), rollup.Flags...),
		"riskCoverageStatus":       rollup.CoverageStatus,
		"riskGateStatus":           rollup.GateStatus,
		"riskMechanismScores":      rollup.MechanismScores,
	}
	out = append(out, summary)

	for _, mechanism := range rollup.Mechanisms {
		var profileType string
		switch mechanism.Key {
		case "low_substitutability", "sole_source_lock_in":
			profileType = "substitution_profile"
		case "customs_friction", "route_fragility":
			profileType = "route_bottleneck_profile"
		case "control_exposure":
			profileType = "control_exposure_profile"
		case "inventory_vulnerability", "service_dependency":
			profileType = "inventory_vulnerability_profile"
		default:
			continue
		}
		profile := base(profileType)
		profile.Metrics = map[string]models.MetricValue{
			mechanism.Key: metricFromScore(mechanism.Score, now),
		}
		if mechanism.MetricValue != nil && strings.TrimSpace(mechanism.MetricKey) != "" {
			profile.Metrics[mechanism.MetricKey] = *cloneMetricValuePtr(mechanism.MetricValue)
		}
		profile.Qualifiers = map[string]interface{}{
			"mechanism":      mechanism.Key,
			"label":          mechanism.Label,
			"summary":        mechanism.Summary,
			"primary":        mechanism.Key == rollup.PrimaryMechanism.Key,
			"gateStatus":     rollup.GateStatus,
			"coverageStatus": rollup.CoverageStatus,
		}
		out = append(out, profile)
	}
	return out
}

func resolveSupplyLinkEffectiveMetrics(
	frag *models.HorizonFragment,
	seg *SupplyLinkSegment,
	capability *models.CapabilityProfile,
) map[string]models.MetricValue {
	out := make(map[string]models.MetricValue)
	if capability != nil {
		for k, v := range capability.Metrics {
			out[strings.TrimSpace(k)] = v
		}
	}
	if frag != nil {
		for k, v := range frag.State {
			out[strings.TrimSpace(k)] = v
		}
	}
	if seg != nil {
		for k, v := range seg.State {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

func computeSoleSourceMechanism(metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	share := lookupMetricValue(metrics, "share")
	switchTime := lookupMetricValue(metrics, "switch_time_days")
	techSub := lookupMetricValue(metrics, "technical_substitutability")
	regSub := lookupMetricValue(metrics, "regulatory_substitutability")
	shareRisk := normalizeHighRisk(metricFloatValue(share), 0.55, 0.85)
	switchRisk := normalizeHighRisk(metricFloatValue(switchTime), 30, 120)
	techRisk := normalizeLowRisk(metricFloatValue(techSub), 0.72, 0.35)
	regRisk := normalizeLowRisk(metricFloatValue(regSub), 0.72, 0.35)
	score := maxFloat(shareRisk, switchRisk, techRisk, regRisk)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	metricKey := "share"
	metricLabel := "Share"
	metric := share
	if switchRisk >= score {
		metricKey = "switch_time_days"
		metricLabel = "Switch time"
		metric = switchTime
	} else if techRisk >= score {
		metricKey = "technical_substitutability"
		metricLabel = "Technical substitutability"
		metric = techSub
	} else if regRisk >= score {
		metricKey = "regulatory_substitutability"
		metricLabel = "Regulatory substitutability"
		metric = regSub
	}
	return supplyLinkRiskMechanism{
		Key:         "sole_source_lock_in",
		Label:       "Sole-source lock-in",
		Score:       roundRiskScore(score),
		MetricKey:   metricKey,
		MetricLabel: metricLabel,
		MetricValue: cloneMetricValuePtr(metric),
		Summary:     fmt.Sprintf("Supplier lock-in risk is elevated because %s is driving concentration on this supply link.", strings.ToLower(metricLabel)),
	}
}

func computeSubstitutabilityMechanism(metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	techFit := lookupMetricValue(metrics, "technical_fit")
	regFit := lookupMetricValue(metrics, "regulatory_fit")
	techSub := lookupMetricValue(metrics, "technical_substitutability")
	regSub := lookupMetricValue(metrics, "regulatory_substitutability")
	techRisk := maxFloat(
		normalizeLowRisk(metricFloatValue(techFit), 0.82, 0.45),
		normalizeLowRisk(metricFloatValue(techSub), 0.72, 0.35),
	)
	regRisk := maxFloat(
		normalizeLowRisk(metricFloatValue(regFit), 0.82, 0.45),
		normalizeLowRisk(metricFloatValue(regSub), 0.72, 0.35),
	)
	score := maxFloat(techRisk, regRisk)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	metricKey := "technical_fit"
	metricLabel := "Technical fit"
	metric := techFit
	if regRisk >= score {
		metricKey = "regulatory_fit"
		metricLabel = "Regulatory fit"
		metric = regFit
	}
	return supplyLinkRiskMechanism{
		Key:         "low_substitutability",
		Label:       "Low substitutability",
		Score:       roundRiskScore(score),
		MetricKey:   metricKey,
		MetricLabel: metricLabel,
		MetricValue: cloneMetricValuePtr(metric),
		Summary:     fmt.Sprintf("Replacement risk is elevated because %s is weak for this product flow.", strings.ToLower(metricLabel)),
	}
}

func computeInventoryMechanism(metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	inventory := lookupMetricValue(metrics, "inventory_cover_days_at_destination")
	if inventory == nil {
		inventory = lookupMetricValue(metrics, "inventory_days_baseline")
	}
	lead := lookupMetricValue(metrics, "lead_time_days")
	if lead == nil {
		lead = lookupMetricValue(metrics, "lead_time_days_baseline")
	}
	inventoryRisk := normalizeLowRisk(metricFloatValue(inventory), 30, 7)
	leadRisk := normalizeHighRisk(metricFloatValue(lead), 14, 60)
	coverageGapRisk := 0.0
	if inventory != nil && lead != nil && metricFloatValue(lead) > 0 {
		gap := metricFloatValue(lead) - metricFloatValue(inventory)
		if gap > 0 {
			coverageGapRisk = clamp01(gap / metricFloatValue(lead))
		}
	}
	score := maxFloat(inventoryRisk, leadRisk*0.45+coverageGapRisk*0.7)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	metricKey := "inventory_cover_days_at_destination"
	metricLabel := "Inventory cover"
	metric := inventory
	if coverageGapRisk >= inventoryRisk && lead != nil {
		metricKey = "lead_time_days"
		metricLabel = "Lead time"
		metric = lead
	}
	return supplyLinkRiskMechanism{
		Key:         "inventory_vulnerability",
		Label:       "Inventory vulnerability",
		Score:       roundRiskScore(score),
		MetricKey:   metricKey,
		MetricLabel: metricLabel,
		MetricValue: cloneMetricValuePtr(metric),
		Summary:     "Downtime vulnerability is elevated because inventory cover is thin versus replenishment time.",
	}
}

func computeCustomsMechanism(metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	customs := lookupMetricValue(metrics, "customs_clearance_days")
	score := normalizeHighRisk(metricFloatValue(customs), 2, 10)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	return supplyLinkRiskMechanism{
		Key:         "customs_friction",
		Label:       "Customs friction",
		Score:       roundRiskScore(score),
		MetricKey:   "customs_clearance_days",
		MetricLabel: "Customs clearance",
		MetricValue: cloneMetricValuePtr(customs),
		Summary:     "Border and clearance latency is materially increasing risk on this supply link.",
	}
}

func computeRouteMechanism(seg *SupplyLinkSegment, frag *models.HorizonFragment, metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	if seg == nil {
		return supplyLinkRiskMechanism{}
	}
	fragility, rerouteability := deriveRouteShape(seg)
	customsRisk := normalizeHighRisk(metricFloatValue(lookupMetricValue(metrics, "customs_clearance_days")), 2, 10)
	score := clamp01(fragility*0.8 + customsRisk*0.25)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	metricKey := "route_fragility"
	metricLabel := "Route fragility"
	metric := &models.MetricValue{
		Value:      fragility,
		Confidence: 0.72,
		ObservedAt: time.Now().UTC(),
		Provenance: models.Provenance{Method: "deterministic_route_shape"},
	}
	if hasVesselType(seg, frag) {
		metricKey = "vessel_type"
		metricLabel = "Vessel type"
	}
	return supplyLinkRiskMechanism{
		Key:         "route_fragility",
		Label:       "Route fragility",
		Score:       roundRiskScore(score),
		MetricKey:   metricKey,
		MetricLabel: metricLabel,
		MetricValue: metric,
		Summary:     fmt.Sprintf("Route concentration is elevated with fragility %.2f and rerouteability %.2f.", fragility, rerouteability),
	}
}

func computeControlExposureMechanism(frag *models.HorizonFragment, capability *models.CapabilityProfile) supplyLinkRiskMechanism {
	var boolCount int
	qualifiers := map[string]interface{}{}
	if capability != nil {
		for k, v := range capability.Qualifiers {
			qualifiers[k] = v
		}
	}
	if frag != nil {
		for k, v := range frag.Qualifiers {
			qualifiers[k] = v
		}
	}
	keys := []string{"export_license_required", "license_required", "sanctions_touchpoint", "dual_use_flag", "approved_vendor_only", "service_contract_required"}
	for _, key := range keys {
		if parseBoolish(qualifiers[key]) {
			boolCount++
		}
	}
	score := clamp01(float64(boolCount) * 0.24)
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	return supplyLinkRiskMechanism{
		Key:         "control_exposure",
		Label:       "Control exposure",
		Score:       roundRiskScore(score),
		Summary:     "Regulatory or contractual control gates materially constrain this supply link.",
		MetricKey:   "control_exposure_count",
		MetricLabel: "Control gates",
		MetricValue: &models.MetricValue{
			Value:      float64(boolCount),
			Confidence: 0.68,
			ObservedAt: time.Now().UTC(),
			Provenance: models.Provenance{Method: "deterministic_control_exposure"},
		},
	}
}

func computeServiceDependencyMechanism(frag *models.HorizonFragment, metrics map[string]models.MetricValue) supplyLinkRiskMechanism {
	serviceCriticality := lookupMetricValue(metrics, "service_criticality")
	score := normalizeHighRisk(metricFloatValue(serviceCriticality), 0.45, 0.9)
	if frag != nil && parseBoolish(frag.Qualifiers["service_contract_required"]) {
		score = clamp01(score + 0.18)
	}
	if score < 0.2 {
		return supplyLinkRiskMechanism{}
	}
	return supplyLinkRiskMechanism{
		Key:         "service_dependency",
		Label:       "Service dependency",
		Score:       roundRiskScore(score),
		MetricKey:   "service_criticality",
		MetricLabel: "Service criticality",
		MetricValue: cloneMetricValuePtr(serviceCriticality),
		Summary:     "Replacement and recovery depend on service-sensitive support for this product flow.",
	}
}

func deriveRouteShape(seg *SupplyLinkSegment) (fragility, rerouteability float64) {
	viaCount := len(uniqueSortedStrings(seg.TransportViaRefs))
	switch {
	case viaCount <= 0:
		return 0.2, 0.85
	case viaCount == 1:
		return 0.72, 0.22
	case viaCount == 2:
		return 0.48, 0.46
	case viaCount == 3:
		return 0.34, 0.62
	default:
		return 0.22, 0.78
	}
}

func computeCapabilityCoverageStatus(metrics map[string]models.MetricValue) string {
	switch count := len(metrics); {
	case count >= 6:
		return "rich"
	case count >= 3:
		return "partial"
	case count > 0:
		return "thin"
	default:
		return "empty"
	}
}

func computeRiskCoverageStatus(metrics map[string]models.MetricValue) string {
	required := []string{"lead_time_days", "inventory_cover_days_at_destination", "share", "switch_time_days", "technical_fit", "regulatory_fit"}
	covered := 0
	for _, key := range required {
		if lookupMetricValue(metrics, key) != nil {
			covered++
		}
	}
	switch {
	case covered >= 5:
		return "rich"
	case covered >= 3:
		return "partial"
	case covered >= 1:
		return "thin"
	default:
		return "empty"
	}
}

func computeRiskGateStatus(primaryScore float64, metrics map[string]models.MetricValue) string {
	if primaryScore >= 0.72 && computeRiskCoverageStatus(metrics) == "rich" {
		return "corroborated"
	}
	if primaryScore >= 0.45 && computeRiskCoverageStatus(metrics) != "empty" {
		return "sufficient"
	}
	return "best_effort"
}

func mergeMetricMaps(base, incoming map[string]models.MetricValue) map[string]models.MetricValue {
	if len(base) == 0 && len(incoming) == 0 {
		return nil
	}
	out := cloneMetricValuesForPersistence(base)
	if out == nil {
		out = make(map[string]models.MetricValue)
	}
	for key, value := range incoming {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if existing, ok := out[key]; ok {
			out[key] = pickPreferredMetric(existing, value)
			continue
		}
		out[key] = value
	}
	return out
}

func pickPreferredMetric(existing, incoming models.MetricValue) models.MetricValue {
	if incoming.ObservedAt.After(existing.ObservedAt) && incoming.Confidence >= existing.Confidence {
		return incoming
	}
	if incoming.Confidence > existing.Confidence+0.05 {
		return incoming
	}
	if existing.ObservedAt.IsZero() && !incoming.ObservedAt.IsZero() {
		return incoming
	}
	return existing
}

func lookupMetricValue(metrics map[string]models.MetricValue, key string) *models.MetricValue {
	if len(metrics) == 0 {
		return nil
	}
	value, ok := metrics[strings.TrimSpace(key)]
	if !ok {
		return nil
	}
	return &value
}

func metricFloatValue(metric *models.MetricValue) float64 {
	if metric == nil {
		return 0
	}
	return metric.Value
}

func cloneMetricValuePtr(metric *models.MetricValue) *models.MetricValue {
	if metric == nil {
		return nil
	}
	out := *metric
	return &out
}

func normalizeHighRisk(value, safeThreshold, dangerThreshold float64) float64 {
	if dangerThreshold <= safeThreshold {
		return 0
	}
	if value <= safeThreshold {
		return 0
	}
	if value >= dangerThreshold {
		return 1
	}
	return (value - safeThreshold) / (dangerThreshold - safeThreshold)
}

func normalizeLowRisk(value, safeThreshold, dangerThreshold float64) float64 {
	if safeThreshold <= dangerThreshold {
		return 0
	}
	if value <= 0 && safeThreshold > 0 {
		return 1
	}
	if value >= safeThreshold {
		return 0
	}
	if value <= dangerThreshold {
		return 1
	}
	return (safeThreshold - value) / (safeThreshold - dangerThreshold)
}

func metricFromScore(score float64, observedAt time.Time) models.MetricValue {
	return models.MetricValue{
		Value:      roundRiskScore(score),
		Confidence: 0.76,
		ObservedAt: observedAt,
		Provenance: models.Provenance{Method: "deterministic_supply_link_risk_rollup"},
	}
}

func mechanismKeys(mechanisms []supplyLinkRiskMechanism) []string {
	if len(mechanisms) == 0 {
		return nil
	}
	out := make([]string, 0, len(mechanisms))
	for _, mechanism := range mechanisms {
		if strings.TrimSpace(mechanism.Key) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(mechanism.Key))
	}
	return uniqueSortedStrings(out)
}

func parseBoolish(raw interface{}) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "required", "strict":
			return true
		}
	case float64:
		return v > 0
	case int:
		return v > 0
	}
	return false
}

func maxFloat(values ...float64) float64 {
	best := 0.0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

func roundRiskScore(value float64) float64 {
	return math.Round(clamp01(value)*1000) / 1000
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
