package scoring

import (
	"math"
)

// Weights from whitepaper 3.5
const (
	WeightCountryRisk        = 0.25
	WeightConflictEvents     = 0.30
	WeightSupplyConcentration = 0.20
	WeightPriceVolatility     = 0.15
	WeightFreightStress       = 0.10
)

// SignalValues holds raw signal values (1-5 scale each).
type SignalValues struct {
	CountryRisk        float64 // 1-5 from travel advisories
	ConflictEvents     float64 // 1-5 from GDELT, AIS, NAVTEX
	SupplyConcentration float64 // 1-5 from HHI index
	PriceVolatility    float64 // 1-5 from price series
	FreightStress      float64 // 1-5 from BDI/FBX
}

// ComputeNodeRisk computes weighted risk score (1-5) for a facility or material.
func ComputeNodeRisk(signals SignalValues) float64 {
	s := clamp(signals.CountryRisk, 1, 5)
	if s == 0 {
		s = 1
	}
	c := clamp(signals.ConflictEvents, 1, 5)
	if c == 0 {
		c = 1
	}
	h := clamp(signals.SupplyConcentration, 1, 5)
	if h == 0 {
		h = 1
	}
	p := clamp(signals.PriceVolatility, 1, 5)
	if p == 0 {
		p = 1
	}
	f := clamp(signals.FreightStress, 1, 5)
	if f == 0 {
		f = 1
	}
	score := s*WeightCountryRisk + c*WeightConflictEvents + h*WeightSupplyConcentration +
		p*WeightPriceVolatility + f*WeightFreightStress
	return clamp(score, 1, 5)
}

// HHIToRisk converts HHI index to 1-5 scale. formula: min(5, hhi/2000)
func HHIToRisk(hhi float64) float64 {
	return clamp(hhi/2000, 1, 5)
}

// RouteRisk computes route risk: max(waypoint risks) * chokepoint_multiplier
func RouteRisk(maxNodeRisk, chokepointMultiplier float64) float64 {
	return clamp(maxNodeRisk*chokepointMultiplier, 1, 5)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// RoundToScale rounds score to 1 decimal for display.
func RoundToScale(score float64) float64 {
	return math.Round(score*10) / 10
}
