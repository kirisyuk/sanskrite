package propagation

import (
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type PathFamilySummary struct {
	Key               string
	Label             string
	Score             float64
	Steps             int
	BusinessTailSteps int
	FacilityTailSteps int
	CrossBorderSteps  int
	MaxHops           int
	Channels          []string
	EdgeTypes         []string
	SamplePaths       []string
}

type PathFamilyInterestingnessSummary struct {
	PathFamilies         int
	BusinessPathFamilies int
	CrossBorderFamilies  int
	TopFamilies          []PathFamilySummary
}

func ScorePathFamilies(
	signal *horizonmodels.SignalDescriptor,
	trace []SignalPropagationTraceEvent,
	limit int,
) PathFamilyInterestingnessSummary {
	summary := PathFamilyInterestingnessSummary{}
	if signal == nil || len(trace) == 0 {
		return summary
	}
	if limit <= 0 {
		limit = 4
	}
	profile := DefaultPathFamilyScoringProfile()
	type familyAgg struct {
		PathFamilySummary
		amplitudeSum float64
	}
	aggs := make(map[string]*familyAgg)
	for _, ev := range trace {
		if strings.TrimSpace(ev.Outcome) != "propagated" {
			continue
		}
		keys := classifyPathFamilies(ev)
		if len(keys) == 0 {
			keys = []string{"direct_dependency"}
		}
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			agg := aggs[key]
			if agg == nil {
				agg = &familyAgg{
					PathFamilySummary: PathFamilySummary{
						Key:         key,
						Label:       labelForPathFamily(key),
						Channels:    make([]string, 0, 4),
						EdgeTypes:   make([]string, 0, 4),
						SamplePaths: make([]string, 0, 3),
					},
				}
				aggs[key] = agg
			}
			agg.Steps++
			agg.amplitudeSum += ev.AmplitudeOut
			if ev.Hops > agg.MaxHops {
				agg.MaxHops = ev.Hops
			}
			if isBusinessPath(ev.ToPath) {
				agg.BusinessTailSteps++
			}
			if isFacilityPath(ev.ToPath) {
				agg.FacilityTailSteps++
			}
			if isCrossBorderEvent(ev) {
				agg.CrossBorderSteps++
			}
			agg.Channels = appendUniqueString(agg.Channels, strings.TrimSpace(ev.Channel))
			agg.EdgeTypes = appendUniqueString(agg.EdgeTypes, strings.TrimSpace(ev.EdgeType))
			if sample := strings.TrimSpace(ev.ToPath); sample != "" && len(agg.SamplePaths) < 3 {
				agg.SamplePaths = appendUniqueString(agg.SamplePaths, sample)
			}
		}
	}

	families := make([]PathFamilySummary, 0, len(aggs))
	for key, agg := range aggs {
		if agg == nil || agg.Steps == 0 {
			continue
		}
		avgAmp := agg.amplitudeSum / float64(agg.Steps)
		base := profile.BaseWeights[key]
		businessScore := base + avgAmp*profile.AmplitudeWeight
		businessScore += pathFamilyMinFloat(float64(agg.MaxHops)*profile.DepthStep, profile.DepthCap)
		if agg.BusinessTailSteps > 0 {
			businessScore += pathFamilyMinFloat(float64(agg.BusinessTailSteps)*profile.BusinessTailBonus, 0.28)
		}
		if agg.FacilityTailSteps > 0 {
			businessScore += pathFamilyMinFloat(float64(agg.FacilityTailSteps)*profile.FacilityTailBonus, 0.18)
		}
		crossBorderScore := 0.0
		if agg.CrossBorderSteps > 0 {
			crossBorderScore += pathFamilyMinFloat(float64(agg.CrossBorderSteps)*profile.CrossBorderBonus, profile.CrossBorderCap)
		}
		if familyLooksEnergyOrLogistics(key, agg.Channels, agg.EdgeTypes, agg.SamplePaths) {
			businessScore += profile.EnergyLogisticsBonus
		}
		if key == "regulatory_control" {
			businessScore += profile.RegulatoryBonus
		}
		if key == "chokepoint_logistics" {
			businessScore += profile.ChokepointBonus
		}
		switch strings.TrimSpace(signal.Axis) {
		case horizonmodels.AxisGeoConflict:
			if key == "energy_supply_chain" || key == "chokepoint_logistics" || key == "market_exposure" {
				businessScore += profile.ConflictEnergyBridgeBonus
			}
		case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
			if agg.BusinessTailSteps > 0 {
				businessScore += profile.FlowBusinessBridgeBonus
			}
		}
		raw := businessScore*profile.BusinessWeight + crossBorderScore*profile.CrossBorderWeight + profile.Bias
		score := pathFamilySigmoid(raw / profile.Temperature)
		family := agg.PathFamilySummary
		family.Score = score
		family.Channels = sortedStrings(family.Channels)
		family.EdgeTypes = sortedStrings(family.EdgeTypes)
		family.SamplePaths = sortedStrings(family.SamplePaths)
		families = append(families, family)
	}

	sort.Slice(families, func(i, j int) bool {
		if families[i].Score == families[j].Score {
			if families[i].BusinessTailSteps == families[j].BusinessTailSteps {
				return families[i].Key < families[j].Key
			}
			return families[i].BusinessTailSteps > families[j].BusinessTailSteps
		}
		return families[i].Score > families[j].Score
	})

	summary.PathFamilies = len(families)
	for _, family := range families {
		if family.Score >= profile.BusinessThreshold {
			summary.BusinessPathFamilies++
		}
		if family.CrossBorderSteps > 0 && family.Score >= profile.CrossBorderFamilyThreshold {
			summary.CrossBorderFamilies++
		}
	}
	if len(families) > limit {
		families = families[:limit]
	}
	summary.TopFamilies = families
	return summary
}

func classifyPathFamilies(ev SignalPropagationTraceEvent) []string {
	text := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(ev.EdgeType),
		strings.TrimSpace(ev.Channel),
		strings.TrimSpace(ev.Scope),
		strings.TrimSpace(ev.Spillover),
		strings.TrimSpace(ev.FromPath),
		strings.TrimSpace(ev.ToPath),
	}, " "))
	keys := make([]string, 0, 2)
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}

	switch strings.TrimSpace(ev.EdgeType) {
	case string(horizonmodels.DependencyTypeRegulates),
		string(horizonmodels.DependencyTypeControlsAccess),
		string(horizonmodels.DependencyTypeImposesCondition),
		string(horizonmodels.DependencyTypeGatekeepsDecision),
		string(horizonmodels.DependencyTypeUsesMechanism),
		string(horizonmodels.DependencyTypeAppliesTo):
		add("regulatory_control")
	case string(horizonmodels.DependencyTypeSuppliesTo),
		string(horizonmodels.DependencyTypeTransformsInto),
		string(horizonmodels.DependencyTypeAssemblesInto),
		string(horizonmodels.DependencyTypeUsedIn),
		string(horizonmodels.DependencyTypeExtractsFrom),
		string(horizonmodels.DependencyTypeProduces):
		add("energy_supply_chain")
	case string(horizonmodels.DependencyTypeParticipatesIn),
		string(horizonmodels.DependencyTypeTradedIn),
		string(horizonmodels.DependencyTypeHasMarketSegment),
		string(horizonmodels.DependencyTypeHasSubmarket),
		string(horizonmodels.DependencyTypeDominates):
		add("market_exposure")
	case string(horizonmodels.DependencyTypeOwns),
		string(horizonmodels.DependencyTypeControls),
		string(horizonmodels.DependencyTypeManages),
		string(horizonmodels.DependencyTypeAdministers),
		string(horizonmodels.DependencyTypeInvestsIn):
		add("organizational_control")
	case string(horizonmodels.DependencyTypeLocatedIn),
		string(horizonmodels.DependencyTypeOperatesIn),
		string(horizonmodels.DependencyTypeAffects),
		string(horizonmodels.DependencyTypeTriggeredBy):
		add("geo_operational_exposure")
	case string(horizonmodels.DependencyTypeDependsOn):
		add("facility_dependency")
	}
	if strings.Contains(text, "lng") || strings.Contains(text, "natural-gas") || strings.Contains(text, "pipeline") ||
		strings.Contains(text, "refinery") || strings.Contains(text, "petrochem") || strings.Contains(text, "power") {
		add("energy_supply_chain")
	}
	if strings.Contains(text, "port") || strings.Contains(text, "shipping") || strings.Contains(text, "maritime") ||
		strings.Contains(text, "strait") || strings.Contains(text, "canal") || strings.Contains(text, "logistics") || strings.Contains(text, "tanker") {
		add("chokepoint_logistics")
	}
	if strings.Contains(text, "/market/") {
		add("market_exposure")
	}
	if len(keys) == 0 {
		add("direct_dependency")
	}
	return keys
}

func labelForPathFamily(key string) string {
	switch strings.TrimSpace(key) {
	case "energy_supply_chain":
		return "Energy Supply Chain"
	case "chokepoint_logistics":
		return "Chokepoint Logistics"
	case "market_exposure":
		return "Market Exposure"
	case "facility_dependency":
		return "Facility Dependency"
	case "regulatory_control":
		return "Regulatory Control"
	case "organizational_control":
		return "Organizational Control"
	case "geo_operational_exposure":
		return "Geo Operational Exposure"
	default:
		return "Direct Dependency"
	}
}

func isCrossBorderEvent(ev SignalPropagationTraceEvent) bool {
	scope := strings.TrimSpace(ev.Scope)
	spillover := strings.TrimSpace(ev.Spillover)
	return scope == "cross_border" || strings.HasPrefix(spillover, "cross_border")
}

func isBusinessPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/fac/") ||
		strings.HasPrefix(path, "/market/") ||
		strings.HasPrefix(path, "/deposit/") ||
		strings.HasPrefix(path, "/program/")
}

func isFacilityPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "/fac/")
}

func familyLooksEnergyOrLogistics(key string, channels, edgeTypes, samplePaths []string) bool {
	text := strings.ToLower(strings.Join(append(append(channels, edgeTypes...), samplePaths...), " "))
	return key == "energy_supply_chain" ||
		key == "chokepoint_logistics" ||
		strings.Contains(text, "energy") ||
		strings.Contains(text, "logistics") ||
		strings.Contains(text, "shipping") ||
		strings.Contains(text, "port")
}

func appendUniqueString(in []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	for _, existing := range in {
		if existing == value {
			return in
		}
	}
	return append(in, value)
}

func sortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func pathFamilyMinFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
