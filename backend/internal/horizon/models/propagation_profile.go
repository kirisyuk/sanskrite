package models

// PropagationProfile defines how an axis propagates through the graph.
// See risk-architecture.md Contract 3 for full specification.
type PropagationProfile struct {
	AxisKey        string           `json:"axisKey" firestore:"axisKey"`               // Reference to AxisSpec key
	DefaultDamping float64          `json:"defaultDamping" firestore:"defaultDamping"`   // [0.0, 1.0]
	DefaultMaxHops int             `json:"defaultMaxHops" firestore:"defaultMaxHops"`   // Typical propagation depth
	PropagationMode string          `json:"propagationMode" firestore:"propagationMode"` // "upstream", "downstream", "both"
	StrongEdges    []StrongEdgeSpec `json:"strongEdges,omitempty" firestore:"strongEdges,omitempty"`
	Transformations []TransformationRule `json:"transformations,omitempty" firestore:"transformations,omitempty"`
}

// StrongEdgeSpec defines a strong edge type for propagation.
type StrongEdgeSpec struct {
	Type      string  `json:"type" firestore:"type"`           // Edge type (e.g., "OWNS", "SUPPLIES_TO")
	Direction string  `json:"direction" firestore:"direction"` // "source_to_target" | "target_to_source" | "both"
	Weight    float64 `json:"weight" firestore:"weight"`      // Edge weight multiplier (default 1.0)
}

// TransformationRule defines axis transformation when signal passes through an edge.
type TransformationRule struct {
	OnEdgeType string  `json:"onEdgeType" firestore:"onEdgeType"` // Edge type that triggers transformation
	ToAxis     string  `json:"toAxis" firestore:"toAxis"`         // Target axis after transformation
	Factor     float64 `json:"factor" firestore:"factor"`         // Amplitude multiplier (e.g., 0.8)
}

// DefaultPropagationProfiles returns default propagation profiles for all 12 axes.
// These match the specifications from risk-architecture.md (lines 416-426).
func DefaultPropagationProfiles() map[string]PropagationProfile {
	return map[string]PropagationProfile{
		AxisLegalAccess: {
			AxisKey:        AxisLegalAccess,
			DefaultDamping:  0.95,
			DefaultMaxHops:  5,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "OWNS", Direction: "target_to_source", Weight: 1.0},
				{Type: "CONTROLS", Direction: "target_to_source", Weight: 0.9},
				{Type: "SUBSIDIARY_OF", Direction: "source_to_target", Weight: 1.0},
				{Type: "REGULATES", Direction: "source_to_target", Weight: 1.2},
			},
			Transformations: []TransformationRule{
				{OnEdgeType: "PARTNERS_WITH", ToAxis: AxisLegalCompliance, Factor: 0.8},
			},
		},
		AxisLegalCompliance: {
			AxisKey:        AxisLegalCompliance,
			DefaultDamping:  0.85,
			DefaultMaxHops:  3,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "PARTNERS_WITH", Direction: "both", Weight: 0.9},
				{Type: "SUPPLIES_TO", Direction: "both", Weight: 0.8},
				{Type: "DEPENDS_ON", Direction: "both", Weight: 0.8},
			},
			Transformations: []TransformationRule{},
		},
		AxisFinancialSettlement: {
			AxisKey:        AxisFinancialSettlement,
			DefaultDamping:  0.85,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "both", Weight: 1.0},
				{Type: "OWNS", Direction: "both", Weight: 0.85},
				{Type: "INVESTS_IN", Direction: "both", Weight: 0.75},
			},
			Transformations: []TransformationRule{},
		},
		AxisFinancialCredit: {
			AxisKey:        AxisFinancialCredit,
			DefaultDamping:  0.80,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "both", Weight: 1.0},
				{Type: "OWNS", Direction: "both", Weight: 0.9},
				{Type: "INVESTS_IN", Direction: "both", Weight: 0.8},
			},
			Transformations: []TransformationRule{},
		},
		AxisPhysicalFlowSupply: {
			AxisKey:        AxisPhysicalFlowSupply,
			DefaultDamping:  0.75,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "SUPPLIES_TO", Direction: "source_to_target", Weight: 1.0},
				{Type: "DEPENDS_ON", Direction: "both", Weight: 0.9},
				{Type: "TRANSFORMS_INTO", Direction: "source_to_target", Weight: 0.8},
				{Type: "OWNS", Direction: "source_to_target", Weight: 0.7},
				{Type: "PARTNERS_WITH", Direction: "both", Weight: 0.6},
			},
			Transformations: []TransformationRule{},
		},
		AxisPhysicalFlowDemand: {
			AxisKey:        AxisPhysicalFlowDemand,
			DefaultDamping:  0.75,
			DefaultMaxHops:  4,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "CONSUMES", Direction: "source_to_target", Weight: 1.0},
				{Type: "DEPENDS_ON", Direction: "source_to_target", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisLogisticsCapacity: {
			AxisKey:        AxisLogisticsCapacity,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "target_to_source", Weight: 1.0},
				{Type: "LOCATED_IN", Direction: "both", Weight: 0.9},
				{Type: "OPERATES_IN", Direction: "both", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisLogisticsLatency: {
			AxisKey:        AxisLogisticsLatency,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "target_to_source", Weight: 1.0},
				{Type: "LOCATED_IN", Direction: "both", Weight: 0.9},
				{Type: "OPERATES_IN", Direction: "both", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisOperationalContinuity: {
			AxisKey:        AxisOperationalContinuity,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "OWNS", Direction: "target_to_source", Weight: 1.0},
				{Type: "MANAGES", Direction: "target_to_source", Weight: 0.9},
				{Type: "PRODUCES", Direction: "target_to_source", Weight: 0.8},
				{Type: "AFFECTS", Direction: "source_to_target", Weight: 0.9}, // zone → facility/org (disaster/conflict zone affects operations)
			},
			Transformations: []TransformationRule{},
		},
		AxisInfoUncertainty: {
			AxisKey:        AxisInfoUncertainty,
			DefaultDamping:  0.60,
			DefaultMaxHops:  2,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "both", Weight: 0.8},
				{Type: "PARTNERS_WITH", Direction: "both", Weight: 0.7},
				{Type: "PARTICIPATES_IN", Direction: "target_to_source", Weight: 0.7}, // market → community/org propagation
			},
			Transformations: []TransformationRule{},
		},
		AxisInfoReputation: {
			AxisKey:        AxisInfoReputation,
			DefaultDamping:  0.60,
			DefaultMaxHops:  2,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: "DEPENDS_ON", Direction: "both", Weight: 0.8},
				{Type: "PARTNERS_WITH", Direction: "both", Weight: 0.7},
				{Type: "PARTICIPATES_IN", Direction: "target_to_source", Weight: 0.7}, // market → community/org propagation
			},
			Transformations: []TransformationRule{},
		},
		AxisGeoConflict: {
			AxisKey:        AxisGeoConflict,
			DefaultDamping:  0.80,
			DefaultMaxHops:  2,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: "LOCATED_IN", Direction: "both", Weight: 1.0},
				{Type: "OPERATES_IN", Direction: "both", Weight: 0.9},
				{Type: "AFFECTS", Direction: "source_to_target", Weight: 1.0},       // zone → facility/org (conflict risk propagates to affected entities)
				{Type: "TRIGGERED_BY", Direction: "source_to_target", Weight: 0.95}, // event → zone (event triggers zone; signal flows event→zone)
			},
			Transformations: []TransformationRule{},
		},
	}
}
