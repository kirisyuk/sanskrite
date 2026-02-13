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
	}
}

