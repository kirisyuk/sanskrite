package propagation

import "math"

// PathFamilyScoringProfile isolates deterministic weights for ranking business-meaningful path families.
type PathFamilyScoringProfile struct {
	BaseWeights                map[string]float64
	AmplitudeWeight            float64
	DepthStep                  float64
	DepthCap                   float64
	BusinessTailBonus          float64
	FacilityTailBonus          float64
	CrossBorderBonus           float64
	CrossBorderCap             float64
	EnergyLogisticsBonus       float64
	RegulatoryBonus            float64
	ChokepointBonus            float64
	ConflictEnergyBridgeBonus  float64
	FlowBusinessBridgeBonus    float64
	BusinessWeight             float64
	CrossBorderWeight          float64
	Bias                       float64
	Temperature                float64
	BusinessThreshold          float64
	CrossBorderFamilyThreshold float64
}

func DefaultPathFamilyScoringProfile() PathFamilyScoringProfile {
	return PathFamilyScoringProfile{
		BaseWeights: map[string]float64{
			"energy_supply_chain":      0.34,
			"chokepoint_logistics":     0.33,
			"market_exposure":          0.28,
			"facility_dependency":      0.30,
			"regulatory_control":       0.26,
			"organizational_control":   0.18,
			"geo_operational_exposure": 0.22,
			"direct_dependency":        0.14,
		},
		AmplitudeWeight:            0.28,
		DepthStep:                  0.05,
		DepthCap:                   0.16,
		BusinessTailBonus:          0.16,
		FacilityTailBonus:          0.10,
		CrossBorderBonus:           0.12,
		CrossBorderCap:             0.20,
		EnergyLogisticsBonus:       0.10,
		RegulatoryBonus:            0.08,
		ChokepointBonus:            0.12,
		ConflictEnergyBridgeBonus:  0.12,
		FlowBusinessBridgeBonus:    0.10,
		BusinessWeight:             0.74,
		CrossBorderWeight:          0.36,
		Bias:                       -0.34,
		Temperature:                0.26,
		BusinessThreshold:          0.70,
		CrossBorderFamilyThreshold: 0.62,
	}
}

func pathFamilySigmoid(v float64) float64 {
	if v > 12 {
		return 1
	}
	if v < -12 {
		return 0
	}
	return 1.0 / (1.0 + math.Exp(-v))
}
