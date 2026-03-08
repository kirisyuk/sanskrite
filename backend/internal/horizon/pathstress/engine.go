package pathstress

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type Evaluator struct{}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

func (e *Evaluator) EvaluateLeg(leg horizonmodels.SupplyChainPathLeg) *horizonmodels.SupplyChainLegStress {
	fragility := 0.18
	rerouteability := 0.55
	zonePressure := 0.0
	affectedPressure := 0.0
	recoveryLatency := 18.0
	factors := make([]string, 0, 8)

	mode := strings.TrimSpace(leg.Mode)
	switch mode {
	case "sea":
		fragility += 0.14
		recoveryLatency += 36
		factors = append(factors, "sea_mode")
	case "rail":
		fragility += 0.10
		recoveryLatency += 18
		factors = append(factors, "rail_mode")
	case "road":
		fragility += 0.08
		recoveryLatency += 12
		factors = append(factors, "road_mode")
	case "pipeline_gas", "pipeline_oil", "pipeline_h2":
		fragility += 0.22
		rerouteability -= 0.20
		recoveryLatency += 72
		factors = append(factors, "pipeline_mode")
	case "internet_cable":
		fragility += 0.28
		rerouteability -= 0.15
		recoveryLatency += 96
		factors = append(factors, "cable_mode")
	case "intermodal":
		rerouteability += 0.10
		factors = append(factors, "intermodal_mode")
	}

	if len(leg.TransportViaRefs) > 0 {
		fragility += 0.08
		recoveryLatency += 12
		factors = append(factors, "route_qualified")
	}
	if len(leg.TransportViaRefs) > 2 {
		fragility += 0.07
		recoveryLatency += 12
		factors = append(factors, "multi_leg")
	}
	if containsString(leg.SpatialSemantics, "chokepoint_exposed") {
		fragility += 0.18
		rerouteability -= 0.15
		zonePressure += 0.12
		recoveryLatency += 48
		factors = append(factors, "chokepoint_exposed")
	}
	if len(leg.ZoneRefs) > 0 {
		zonePressure += minFloat(float64(len(leg.ZoneRefs))*0.12, 0.30)
		for _, zoneRef := range leg.ZoneRefs {
			switch inferZoneRole(zoneRef) {
			case "logistics":
				zonePressure += 0.06
				factors = appendIfMissing(factors, "logistics_zone")
			case "energy":
				zonePressure += 0.07
				factors = appendIfMissing(factors, "energy_zone")
			case "industrial":
				zonePressure += 0.05
				factors = appendIfMissing(factors, "industrial_zone")
			}
		}
	}
	if len(leg.AffectedZoneRefs) > 0 {
		affectedPressure += 0.35 + minFloat(float64(len(leg.AffectedZoneRefs)-1)*0.10, 0.25)
		recoveryLatency += 24
		factors = append(factors, "affected_zone_pressure")
	}
	if len(leg.AlternateFlowRefs) > 0 {
		rerouteability += 0.22
		fragility -= 0.08
		factors = append(factors, "alternate_route")
	} else {
		rerouteability -= 0.05
	}
	if leg.BindingConfidence > 0 {
		fragility += clamp01(leg.BindingConfidence) * 0.12
		zonePressure += clamp01(leg.BindingConfidence) * 0.04
	}
	if hasPlaceholderPath(leg.SupplierRef) || hasPlaceholderPath(leg.CustomerRef) || hasPlaceholderPaths(leg.TransportViaRefs) {
		fragility += 0.10
		rerouteability -= 0.05
		factors = append(factors, "placeholder_path")
	}

	fragility = clamp01(fragility)
	rerouteability = clamp01(rerouteability)
	zonePressure = clamp01(zonePressure)
	affectedPressure = clamp01(affectedPressure)
	stressScore := clamp01(fragility*0.40 + (1-rerouteability)*0.25 + zonePressure*0.20 + affectedPressure*0.15)

	return &horizonmodels.SupplyChainLegStress{
		Fragility:            fragility,
		Rerouteability:       rerouteability,
		ZonePressure:         zonePressure,
		AffectedZonePressure: affectedPressure,
		RecoveryLatencyHrs:   recoveryLatency,
		StressScore:          stressScore,
		DominantFactors:      factors,
	}
}

func (e *Evaluator) EvaluatePath(legs []horizonmodels.SupplyChainPathLeg) *horizonmodels.SupplyChainPathStress {
	if len(legs) == 0 {
		return nil
	}
	maxStress := -1.0
	sumStress := 0.0
	sumFragility := 0.0
	sumReroute := 0.0
	sumZone := 0.0
	sumAffected := 0.0
	maxRecovery := 0.0
	bottleneck := 0
	dominant := []string{}
	count := 0.0
	for _, leg := range legs {
		if leg.Stress == nil {
			continue
		}
		count++
		sumStress += leg.Stress.StressScore
		sumFragility += leg.Stress.Fragility
		sumReroute += leg.Stress.Rerouteability
		sumZone += leg.Stress.ZonePressure
		sumAffected += leg.Stress.AffectedZonePressure
		if leg.Stress.RecoveryLatencyHrs > maxRecovery {
			maxRecovery = leg.Stress.RecoveryLatencyHrs
		}
		if leg.Stress.StressScore > maxStress {
			maxStress = leg.Stress.StressScore
			bottleneck = leg.Index
			dominant = append([]string(nil), leg.Stress.DominantFactors...)
		}
	}
	if count == 0 {
		return nil
	}
	avgStress := sumStress / count
	return &horizonmodels.SupplyChainPathStress{
		PathScore:            clamp01(maxStress*0.55 + avgStress*0.45),
		Fragility:            sumFragility / count,
		Rerouteability:       sumReroute / count,
		ZonePressure:         sumZone / count,
		AffectedZonePressure: sumAffected / count,
		RecoveryLatencyHrs:   maxRecovery,
		BottleneckLegIndex:   bottleneck,
		DominantFactors:      dominant,
	}
}

func inferZoneRole(zoneRef string) string {
	ref := strings.ToLower(strings.TrimSpace(zoneRef))
	switch {
	case strings.Contains(ref, "port"), strings.Contains(ref, "corridor"), strings.Contains(ref, "logistics"), strings.Contains(ref, "chokepoint"):
		return "logistics"
	case strings.Contains(ref, "energy"), strings.Contains(ref, "gas"), strings.Contains(ref, "oil"), strings.Contains(ref, "petro"), strings.Contains(ref, "mining"):
		return "energy"
	case strings.Contains(ref, "industrial"), strings.Contains(ref, "battery"), strings.Contains(ref, "manufact"), strings.Contains(ref, "chem"):
		return "industrial"
	default:
		return ""
	}
}

func hasPlaceholderPath(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	return strings.Contains(ref, "/na/") || strings.Contains(ref, "/xx/") || strings.Contains(ref, "/zz/")
}

func hasPlaceholderPaths(refs []string) bool {
	for _, ref := range refs {
		if hasPlaceholderPath(ref) {
			return true
		}
	}
	return false
}

func containsString(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func appendIfMissing(items []string, value string) []string {
	if !containsString(items, value) {
		return append(items, value)
	}
	return items
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
