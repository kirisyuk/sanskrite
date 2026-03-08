package propagation

import "math"

// EndpointScoringProfile isolates deterministic weights for ranking remote business tails.
// This keeps graph traversal stable while allowing calibration of endpoint interestingness.
type EndpointScoringProfile struct {
	FacilityTypeWeight        float64
	MarketTypeWeight          float64
	DepositTypeWeight         float64
	ProgramTypeWeight         float64
	OrganizationTypeWeight    float64
	ChokepointTypeWeight      float64
	GeoTypeWeight             float64
	GenericTypeWeight         float64
	CriticalityWeight         float64
	LowReplaceabilityBonus    float64
	EnergyLogisticsBonus      float64
	OverlayOperationalWeight  float64
	OverlayBridgeWeight       float64
	OverlayCriticalityWeight  float64
	OverlayChokepointBonus    float64
	OverlayDistributorPenalty float64
	SphereHubPenalty          float64
	ProductHubPenalty         float64
	GenericLocPenalty         float64
	GenericOrgPenalty         float64
	SyntheticOrgPenalty       float64
	SyntheticFacilityPenalty  float64
	UnknownGeoFacilityPenalty float64
	UnknownJurisdictionPenalty float64
	ConcreteFacilityBonus     float64
	ConcreteMarketBonus       float64
	RemoteBonus               float64
	MultiHopMinHops           int
	MultiHopBonus             float64
	DeepHopMinHops            int
	DeepHopBonus              float64
	PathDiversityStep         float64
	PathDiversityCap          float64
	ConflictBusinessTailBonus float64
	FlowBusinessTailBonus     float64
	TraceBusinessStepBonus    float64
	TraceCrossBorderBonus     float64
	TraceEconomicBonus        float64
	TraceOperationalBonus     float64
	TraceFlowAxisBonus        float64
	TracePhysicsWeight        float64
	TracePhysicsCap           float64
	BusinessWeight            float64
	SurpriseWeight            float64
	Bias                      float64
	Temperature               float64
	BusinessThreshold         float64
	NonObviousThreshold       float64
	NonObviousMinHops         int
	DiversityLambda           float64
}

func DefaultEndpointScoringProfile() EndpointScoringProfile {
	return EndpointScoringProfile{
		FacilityTypeWeight:        0.46,
		MarketTypeWeight:          0.41,
		DepositTypeWeight:         0.39,
		ProgramTypeWeight:         0.29,
		OrganizationTypeWeight:    0.21,
		ChokepointTypeWeight:      0.35,
		GeoTypeWeight:             0.13,
		GenericTypeWeight:         0.08,
		CriticalityWeight:         0.35,
		LowReplaceabilityBonus:    0.18,
		EnergyLogisticsBonus:      0.14,
		OverlayOperationalWeight:  0.16,
		OverlayBridgeWeight:       0.18,
		OverlayCriticalityWeight:  0.14,
		OverlayChokepointBonus:    0.12,
		OverlayDistributorPenalty: 0.10,
		SphereHubPenalty:          0.42,
		ProductHubPenalty:         0.24,
		GenericLocPenalty:         0.28,
		GenericOrgPenalty:         0.10,
		SyntheticOrgPenalty:       0.28,
		SyntheticFacilityPenalty:  0.32,
		UnknownGeoFacilityPenalty: 0.22,
		UnknownJurisdictionPenalty: 0.12,
		ConcreteFacilityBonus:     0.08,
		ConcreteMarketBonus:       0.06,
		RemoteBonus:               0.25,
		MultiHopMinHops:           2,
		MultiHopBonus:             0.12,
		DeepHopMinHops:            3,
		DeepHopBonus:              0.08,
		PathDiversityStep:         0.06,
		PathDiversityCap:          0.18,
		ConflictBusinessTailBonus: 0.12,
		FlowBusinessTailBonus:     0.10,
		TraceBusinessStepBonus:    0.12,
		TraceCrossBorderBonus:     0.11,
		TraceEconomicBonus:        0.08,
		TraceOperationalBonus:     0.06,
		TraceFlowAxisBonus:        0.09,
		TracePhysicsWeight:        0.18,
		TracePhysicsCap:           0.22,
		BusinessWeight:            0.68,
		SurpriseWeight:            0.42,
		Bias:                      -0.36,
		Temperature:               0.28,
		BusinessThreshold:         0.72,
		NonObviousThreshold:       0.84,
		NonObviousMinHops:         2,
		DiversityLambda:           0.72,
	}
}

func endpointSigmoid(v float64) float64 {
	if v > 12 {
		return 1
	}
	if v < -12 {
		return 0
	}
	return 1.0 / (1.0 + math.Exp(-v))
}
