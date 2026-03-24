package propagation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type EdgeFamilyPhysicsSummary struct {
	Family     string
	Multiplier float64
	Reasons    []string
}

func ApplyEdgeFamilyPhysics(axis string, fromFrag *horizonmodels.HorizonFragment, edge *horizonmodels.DependencyEdge, profile EdgeFamilyPhysicsProfile) EdgeFamilyPhysicsSummary {
	family := classifyEdgeFamily(axis, fromFrag, edge)
	if family == "" {
		family = "direct_dependency"
	}
	multiplier := profile.baseMultiplier(strings.TrimSpace(axis), family)
	reasons := []string{family}
	edgeOverlay := BuildPropagationEdgeOverlay(axis, edge, fromFrag)
	nodeOverlay := BuildPropagationNodeOverlay(fromFrag)
	if edgeOverlay.WeightHint > 0 {
		multiplier *= edgeOverlay.WeightHint
		reasons = append(reasons, "overlay_edge_profile")
	}
	if nodeOverlay.HasRole("bridge") {
		multiplier *= 1 + profile.BridgeOriginBonus
		reasons = append(reasons, "overlay_bridge_origin")
	}
	if nodeOverlay.HasRole("regulator") && strings.TrimSpace(axis) != horizonmodels.AxisInfoUncertainty {
		multiplier *= 1 - profile.RegulatorOriginPenalty
		reasons = append(reasons, "overlay_regulator_origin")
	}
	if nodeOverlay.BusinessRole == "distributor" && strings.TrimSpace(axis) == horizonmodels.AxisGeoConflict {
		multiplier *= 1 - profile.DistributorOriginPenalty
		reasons = append(reasons, "overlay_distributor_origin")
	}

	qualifierBoost := 0.0
	if edge != nil && edge.IsTransmissionSensitive() {
		qualifierBoost += edge.GetThroughputShare() * 0.10
		qualifierBoost += edge.GetReroutePenalty() * 0.05
		qualifierBoost += edge.GetCapacityConstraint() * 0.07
		qualifierBoost += minFloat(edge.GetRerouteLatencyDeltaHours()/240.0, 1.0) * 0.03
		qualifierBoost += minFloat(edge.GetRerouteCostDeltaPct()/100.0, 1.0) * 0.03
		qualifierBoost = minFloat(qualifierBoost*profile.TransmissionQualifierGain/0.22, profile.TransmissionQualifierCap)
		if qualifierBoost > 0 {
			multiplier += qualifierBoost
			reasons = append(reasons, "transmission_qualifiers")
		}
		if sub := edge.GetSubstitutability(); sub > 0 {
			subPenalty := minFloat(sub*profile.SubstitutabilityPenalty, profile.SubstitutabilityCap)
			multiplier -= subPenalty
			reasons = append(reasons, "substitutability")
		}
	}
	if edge != nil {
		switch edge.GetEdgeModality() {
		case "projected":
			multiplier *= profile.ProjectedPenalty
			reasons = append(reasons, "projected_edge")
		case "historical":
			multiplier *= profile.HistoricalPenalty
			reasons = append(reasons, "historical_edge")
		}
	}
	if IsChokepointFragment(fromFrag) {
		if bonus, ok := profile.ChokepointTransitBonus[strings.TrimSpace(axis)]; ok && bonus > 0 {
			multiplier *= bonus
			reasons = append(reasons, "chokepoint_transit")
		}
	}
	multiplier = clampPhysicsMultiplier(multiplier)
	return EdgeFamilyPhysicsSummary{
		Family:     family,
		Multiplier: multiplier,
		Reasons:    uniquePhysicsReasons(reasons),
	}
}

func classifyEdgeFamily(axis string, fromFrag *horizonmodels.HorizonFragment, edge *horizonmodels.DependencyEdge) string {
	if edge == nil {
		return "direct_dependency"
	}
	edgeType := strings.TrimSpace(string(edge.Type))
	facet := strings.TrimSpace(string(edge.GetFacet()))
	text := strings.ToLower(strings.Join([]string{
		edgeType,
		facet,
		fragmentCanonicalPath(fromFrag),
		edgePhysicsPathText(fromFrag),
	}, " "))

	switch edge.Type {
	case horizonmodels.DependencyTypeRegulates,
		horizonmodels.DependencyTypeAppliesTo,
		horizonmodels.DependencyTypeImposesCondition,
		horizonmodels.DependencyTypeControlsAccess,
		horizonmodels.DependencyTypeAdministers,
		horizonmodels.DependencyTypeGatekeepsDecision,
		horizonmodels.DependencyTypeUsesMechanism:
		return "regulatory_control"
	case horizonmodels.DependencyTypeSuppliesTo,
		horizonmodels.DependencyTypeTransformsInto,
		horizonmodels.DependencyTypeAssemblesInto,
		horizonmodels.DependencyTypeUsedIn,
		horizonmodels.DependencyTypeExtractsFrom,
		horizonmodels.DependencyTypeProduces:
		if strings.Contains(text, "lng") || strings.Contains(text, "gas") || strings.Contains(text, "pipeline") || strings.Contains(text, "refinery") {
			return "energy_supply_chain"
		}
		return "facility_dependency"
	case horizonmodels.DependencyTypeParticipatesIn,
		horizonmodels.DependencyTypeTradedIn,
		horizonmodels.DependencyTypeHasMarketSegment,
		horizonmodels.DependencyTypeHasSubmarket,
		horizonmodels.DependencyTypeDominates:
		return "market_exposure"
	case horizonmodels.DependencyTypeOwns,
		horizonmodels.DependencyTypeControls,
		horizonmodels.DependencyTypeManages,
		horizonmodels.DependencyTypeInvestsIn:
		return "organizational_control"
	case horizonmodels.DependencyTypeLocatedIn,
		horizonmodels.DependencyTypeOperatesIn,
		horizonmodels.DependencyTypeAffects,
		horizonmodels.DependencyTypeTriggeredBy,
		horizonmodels.DependencyTypeInGeoScope:
		if IsChokepointFragment(fromFrag) {
			return "chokepoint_logistics"
		}
		return "geo_operational_exposure"
	case horizonmodels.DependencyTypeDependsOn:
		if facet == string(horizonmodels.FacetLogistics) || facet == string(horizonmodels.FacetDistribution) || strings.Contains(text, "port") || strings.Contains(text, "shipping") || strings.Contains(text, "strait") || strings.Contains(text, "canal") {
			return "chokepoint_logistics"
		}
		if facet == string(horizonmodels.FacetSupply) || facet == string(horizonmodels.FacetInfrastructure) || strings.Contains(text, "pipeline") || strings.Contains(text, "terminal") || strings.Contains(text, "refinery") || strings.Contains(text, "lng") || strings.Contains(text, "gas") || strings.Contains(text, "power") {
			return "energy_supply_chain"
		}
		return "facility_dependency"
	}

	if strings.Contains(text, "/market/") {
		return "market_exposure"
	}
	if strings.Contains(text, "/fac/") {
		return "facility_dependency"
	}
	if strings.TrimSpace(axis) == horizonmodels.AxisInfoUncertainty || strings.TrimSpace(axis) == horizonmodels.AxisInfoReputation {
		return "informational_association"
	}
	return "direct_dependency"
}

func edgePhysicsPathText(frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(frag.CanonicalPath),
		strings.TrimSpace(frag.MarketDomain),
		strings.TrimSpace(frag.MarketVertical),
		strings.TrimSpace(frag.GeoFeatureType),
	}
	for _, p := range frag.IndexPaths {
		parts = append(parts, strings.TrimSpace(p))
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func uniquePhysicsReasons(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
