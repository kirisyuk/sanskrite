package signal

import (
	"math"
	"time"
)

// FusedSignal represents aggregated risk signal for a region (whitepaper 3.4).
type FusedSignal struct {
	Region      string   `json:"region"`
	RiskLevel   float64  `json:"risk_level"`
	Confidence  string   `json:"confidence"` // "high" | "medium" | "low" | "unconfirmed"
	Signals     []string `json:"signals"`
	Freshness   string   `json:"freshness"`
	SourceCount int      `json:"source_count"`
}

// Fuse aggregates signals from multiple sources.
// Rule: if ≥2 independent sources confirm threat → risk_level elevated; one source → unconfirmed.
func Fuse(regions []RegionSignals) []FusedSignal {
	var out []FusedSignal
	decayHalflifeHours := 24.0
	for _, r := range regions {
		level := 1.0
		confidence := "unconfirmed"
		if len(r.Sources) >= 2 {
			confidence = "medium"
			if len(r.Sources) >= 3 {
				confidence = "high"
			}
			for _, s := range r.Sources {
				weight := math.Exp(-math.Ln2 * s.AgeHours / decayHalflifeHours)
				decayedRisk := s.RiskLevel * weight
				if decayedRisk > level {
					level = decayedRisk
				}
			}
			level = math.Min(5, level+0.5)
		} else if len(r.Sources) == 1 {
			s := r.Sources[0]
			weight := math.Exp(-math.Ln2 * s.AgeHours / decayHalflifeHours)
			level = s.RiskLevel * weight
		}
		signals := make([]string, 0, len(r.Sources))
		for _, s := range r.Sources {
			signals = append(signals, s.Name+" "+s.Detail)
		}
		out = append(out, FusedSignal{
			Region:      r.Region,
			RiskLevel:   level,
			Confidence:  confidence,
			Signals:     signals,
			Freshness:   time.Now().UTC().Format(time.RFC3339),
			SourceCount: len(r.Sources),
		})
	}
	return out
}

// RegionSignals groups signals by region.
type RegionSignals struct {
	Region  string
	Sources []SourceSignal
}

// SourceSignal is a single source contribution.
type SourceSignal struct {
	Name      string
	Detail    string
	RiskLevel float64
	AgeHours  float64
}
