package domain

type GateConfig struct {
	MinNodesPerFacet    int
	MinQualityRatio     float64
	MaxInterFacetHops   int
	TopKDependencies    int
	MaxGapRatioP0       float64
	MaxGapRatioP1       float64
	MinTemporalCoverage float64
	MinSourcesPerDep    int
	MinGeoCoverage      float64
	MaxEnrichmentCycles int
	MaxCrossFacetChecks int
	MaxGateChecks       int
	MaxDataGaps         int

	// Explanatory layer (domain-specific mechanism explanations)
	ExplanatoryTokenBudget   int
	ExplanatoryChunkTokens   int
	ExplanatoryMaxCandidates int
	ExplanatoryMaxUnits      int
}

func DefaultGateConfig() GateConfig {
	return GateConfig{
		MinNodesPerFacet:    3,
		MinQualityRatio:     0.5,
		MaxInterFacetHops:   5,
		TopKDependencies:    10,
		MaxGapRatioP0:       0.7,
		MaxGapRatioP1:       0.5,
		MinTemporalCoverage: 0.5,
		MinSourcesPerDep:    2,
		MinGeoCoverage:      0.8,
		MaxEnrichmentCycles: 2,
		MaxCrossFacetChecks: 24,
		MaxGateChecks:       60,
		MaxDataGaps:         30,

		// Target aggregate budget per domain run (~100k tokens overall across chunks).
		ExplanatoryTokenBudget: 100000,
		// Safe per-call chunk budget for prompt body.
		ExplanatoryChunkTokens: 12000,
		// Consider more than top-10 dependencies for explanatory coverage.
		ExplanatoryMaxCandidates: 120,
		// Cap persisted units to keep payload practical for UI.
		ExplanatoryMaxUnits: 120,
	}
}
