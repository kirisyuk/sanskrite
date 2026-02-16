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
		MaxEnrichmentCycles: 1,
		MaxCrossFacetChecks: 24,
		MaxGateChecks:       60,
		MaxDataGaps:         30,

		// Target aggregate budget per domain run (reduced from 100k for cost control).
		ExplanatoryTokenBudget: 30000,
		// Safe per-call chunk budget for prompt body.
		ExplanatoryChunkTokens: 6000,
		// Consider top dependencies for explanatory coverage (reduced from 120).
		ExplanatoryMaxCandidates: 30,
		// Cap persisted units to keep payload practical for UI.
		ExplanatoryMaxUnits: 30,
	}
}
