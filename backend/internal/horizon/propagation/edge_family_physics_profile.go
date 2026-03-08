package propagation

import "math"

// EdgeFamilyPhysicsProfile isolates deterministic family-level propagation weighting.
// It complements, but does not replace, axis profiles and raw edge qualifiers.
type EdgeFamilyPhysicsProfile struct {
	FamilyBaseMultiplier      map[string]map[string]float64
	TransmissionQualifierGain float64
	TransmissionQualifierCap  float64
	SubstitutabilityPenalty   float64
	SubstitutabilityCap       float64
	ProjectedPenalty          float64
	HistoricalPenalty         float64
	ChokepointTransitBonus    map[string]float64
	BridgeOriginBonus         float64
	RegulatorOriginPenalty    float64
	DistributorOriginPenalty  float64
}

func DefaultEdgeFamilyPhysicsProfile() EdgeFamilyPhysicsProfile {
	return EdgeFamilyPhysicsProfile{
		FamilyBaseMultiplier: map[string]map[string]float64{
			"geo/conflict": {
				"chokepoint_logistics":     1.22,
				"energy_supply_chain":      1.14,
				"market_exposure":          1.08,
				"facility_dependency":      1.10,
				"regulatory_control":       0.90,
				"organizational_control":   0.92,
				"geo_operational_exposure": 1.06,
				"direct_dependency":        0.96,
			},
			"logistics/capacity": {
				"chokepoint_logistics":     1.24,
				"energy_supply_chain":      1.16,
				"market_exposure":          1.08,
				"facility_dependency":      1.12,
				"regulatory_control":       0.90,
				"organizational_control":   0.94,
				"geo_operational_exposure": 1.04,
				"direct_dependency":        0.98,
			},
			"logistics/latency": {
				"chokepoint_logistics":     1.24,
				"energy_supply_chain":      1.14,
				"market_exposure":          1.06,
				"facility_dependency":      1.10,
				"regulatory_control":       0.90,
				"organizational_control":   0.94,
				"geo_operational_exposure": 1.04,
				"direct_dependency":        0.98,
			},
			"physical_flow/supply": {
				"chokepoint_logistics":     1.18,
				"energy_supply_chain":      1.24,
				"market_exposure":          1.12,
				"facility_dependency":      1.16,
				"regulatory_control":       0.92,
				"organizational_control":   0.98,
				"geo_operational_exposure": 1.02,
				"direct_dependency":        1.00,
			},
			"physical_flow/demand": {
				"chokepoint_logistics":     1.12,
				"energy_supply_chain":      1.16,
				"market_exposure":          1.18,
				"facility_dependency":      1.08,
				"regulatory_control":       0.94,
				"organizational_control":   0.98,
				"geo_operational_exposure": 1.00,
				"direct_dependency":        1.00,
			},
			"operational/continuity": {
				"chokepoint_logistics":     1.08,
				"energy_supply_chain":      1.12,
				"market_exposure":          1.04,
				"facility_dependency":      1.20,
				"regulatory_control":       0.95,
				"organizational_control":   1.08,
				"geo_operational_exposure": 1.12,
				"direct_dependency":        1.02,
			},
			"info/uncertainty": {
				"regulatory_control":       1.12,
				"organizational_control":   1.02,
				"market_exposure":          0.98,
				"facility_dependency":      0.94,
				"geo_operational_exposure": 0.94,
				"direct_dependency":        0.96,
			},
		},
		TransmissionQualifierGain: 0.22,
		TransmissionQualifierCap:  0.28,
		SubstitutabilityPenalty:   0.10,
		SubstitutabilityCap:       0.08,
		ProjectedPenalty:          0.92,
		HistoricalPenalty:         0.74,
		BridgeOriginBonus:         0.06,
		RegulatorOriginPenalty:    0.04,
		DistributorOriginPenalty:  0.07,
		ChokepointTransitBonus: map[string]float64{
			"logistics/capacity":     1.08,
			"logistics/latency":      1.08,
			"physical_flow/supply":   1.06,
			"physical_flow/demand":   1.04,
			"geo/conflict":           1.05,
			"operational/continuity": 1.04,
		},
	}
}

func (p EdgeFamilyPhysicsProfile) baseMultiplier(axis, family string) float64 {
	if families, ok := p.FamilyBaseMultiplier[axis]; ok {
		if mult, ok := families[family]; ok && mult > 0 {
			return mult
		}
		if mult, ok := families["direct_dependency"]; ok && mult > 0 {
			return mult
		}
	}
	return 1.0
}

func clampPhysicsMultiplier(v float64) float64 {
	return math.Max(0.68, math.Min(1.45, v))
}
