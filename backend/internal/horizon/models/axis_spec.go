package models

// AxisSpec defines the properties of a quasi-physical axis.
// See docs/notes/risk-architecture.md Contract 2 for full specification.
type AxisSpec struct {
	Key              string   `json:"key" firestore:"key"`                           // e.g., "legal/access"
	Label            string   `json:"label" firestore:"label"`                       // Human-readable label
	DefaultDamping  float64  `json:"defaultDamping" firestore:"defaultDamping"`     // [0.0, 1.0]
	DefaultMaxHops  int      `json:"defaultMaxHops" firestore:"defaultMaxHops"`     // Typical propagation depth
	PropagationMode  string   `json:"propagationMode" firestore:"propagationMode"`   // "upstream", "downstream", "both"
	StrongEdgeTypes  []string `json:"strongEdgeTypes,omitempty" firestore:"strongEdgeTypes,omitempty"` // Default strong edges
	TransformsTo     []string `json:"transformsTo,omitempty" firestore:"transformsTo,omitempty"`         // Possible transformations
}

// The 12-Axis Basis - finite set of quasi-physical dimensions
// All signals MUST map to one of these axes.
const (
	AxisLegalAccess         = "legal/access"           // Legal permission to operate/transact (licenses, sanctions, bans)
	AxisLegalCompliance     = "legal/compliance"       // Secondary exposure risk from association with sanctioned/regulated entities
	AxisFinancialSettlement = "financial/settlement"   // Payment rails, banking access, clearing, FX (ability to move money)
	AxisFinancialCredit     = "financial/credit"       // Credit availability, counterparty risk, default exposure
	AxisPhysicalFlowSupply  = "physical_flow/supply"   // Physical availability of goods/materials (shortage or glut)
	AxisPhysicalFlowDemand = "physical_flow/demand"   // Demand changes (surge or collapse)
	AxisLogisticsCapacity   = "logistics/capacity"     // Transportation/infrastructure capacity (routes, ports, airspace)
	AxisLogisticsLatency    = "logistics/latency"      // Delays, lead time increases (not blockage, but slowdown)
	AxisOperationalContinuity = "operational/continuity" // Ability to operate (strikes, failures, disasters)
	AxisInfoUncertainty     = "info/uncertainty"      // Ambiguity of rules, timelines, exceptions (regulatory fog)
	AxisInfoReputation      = "info/reputation"        // Public perception, ESG signals, brand impact
	AxisGeoConflict         = "geo/conflict"           // Geopolitical instability, conflict zones, diplomatic friction
)

// AllAxes returns all 12 axis keys in order.
func AllAxes() []string {
	return []string{
		AxisLegalAccess,
		AxisLegalCompliance,
		AxisFinancialSettlement,
		AxisFinancialCredit,
		AxisPhysicalFlowSupply,
		AxisPhysicalFlowDemand,
		AxisLogisticsCapacity,
		AxisLogisticsLatency,
		AxisOperationalContinuity,
		AxisInfoUncertainty,
		AxisInfoReputation,
		AxisGeoConflict,
	}
}

// IsValidAxis checks if the given axis key is valid (from 12-axis basis).
func IsValidAxis(axis string) bool {
	for _, a := range AllAxes() {
		if a == axis {
			return true
		}
	}
	return false
}

// DefaultAxisSpecs returns default AxisSpec definitions for all 12 axes.
// These are used as fallback if Firestore doesn't have custom specs.
func DefaultAxisSpecs() map[string]AxisSpec {
	return map[string]AxisSpec{
		AxisLegalAccess: {
			Key:             AxisLegalAccess,
			Label:           "Legal Access",
			DefaultDamping:   0.95,
			DefaultMaxHops:   5,
			PropagationMode:  "upstream",
			StrongEdgeTypes:  []string{"OWNS", "CONTROLS", "SUBSIDIARY_OF", "REGULATES"},
			TransformsTo:     []string{AxisLegalCompliance},
		},
		AxisLegalCompliance: {
			Key:             AxisLegalCompliance,
			Label:           "Legal Compliance",
			DefaultDamping:   0.85,
			DefaultMaxHops:   3,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"PARTNERS_WITH", "SUPPLIES_TO", "DEPENDS_ON"},
			TransformsTo:     []string{},
		},
		AxisFinancialSettlement: {
			Key:             AxisFinancialSettlement,
			Label:           "Financial Settlement",
			DefaultDamping:   0.80,
			DefaultMaxHops:   4,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "OWNS", "INVESTS_IN", "TRADED_IN"},
			TransformsTo:     []string{},
		},
		AxisFinancialCredit: {
			Key:             AxisFinancialCredit,
			Label:           "Financial Credit",
			DefaultDamping:   0.80,
			DefaultMaxHops:   4,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "OWNS", "INVESTS_IN", "TRADED_IN"},
			TransformsTo:     []string{},
		},
		AxisPhysicalFlowSupply: {
			Key:             AxisPhysicalFlowSupply,
			Label:           "Physical Flow Supply",
			DefaultDamping:   0.75,
			DefaultMaxHops:   4,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"SUPPLIES_TO", "DEPENDS_ON", "TRANSFORMS_INTO", "PRODUCES", "OWNS", "CONTROLS", "MANAGES", "OPERATES_IN", "PARTNERS_WITH"},
			TransformsTo:     []string{},
		},
		AxisPhysicalFlowDemand: {
			Key:             AxisPhysicalFlowDemand,
			Label:           "Physical Flow Demand",
			DefaultDamping:   0.75,
			DefaultMaxHops:   4,
			PropagationMode:  "downstream",
			StrongEdgeTypes:  []string{"SUPPLIES_TO", "DEPENDS_ON", "TRANSFORMS_INTO", "TRADED_IN"},
			TransformsTo:     []string{},
		},
		AxisLogisticsCapacity: {
			Key:             AxisLogisticsCapacity,
			Label:           "Logistics Capacity",
			DefaultDamping:   0.70,
			DefaultMaxHops:   3,
			PropagationMode:  "downstream",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "LOCATED_IN", "OPERATES_IN"},
			TransformsTo:     []string{},
		},
		AxisLogisticsLatency: {
			Key:             AxisLogisticsLatency,
			Label:           "Logistics Latency",
			DefaultDamping:   0.70,
			DefaultMaxHops:   3,
			PropagationMode:  "downstream",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "LOCATED_IN", "OPERATES_IN"},
			TransformsTo:     []string{},
		},
		AxisOperationalContinuity: {
			Key:             AxisOperationalContinuity,
			Label:           "Operational Continuity",
			DefaultDamping:   0.70,
			DefaultMaxHops:   3,
			PropagationMode:  "upstream",
			StrongEdgeTypes:  []string{"OWNS", "MANAGES", "PRODUCES"},
			TransformsTo:     []string{},
		},
		AxisInfoUncertainty: {
			Key:             AxisInfoUncertainty,
			Label:           "Information Uncertainty",
			DefaultDamping:   0.60,
			DefaultMaxHops:   2,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "PARTNERS_WITH"},
			TransformsTo:     []string{},
		},
		AxisInfoReputation: {
			Key:             AxisInfoReputation,
			Label:           "Information Reputation",
			DefaultDamping:   0.60,
			DefaultMaxHops:   2,
			PropagationMode:  "both",
			StrongEdgeTypes:  []string{"DEPENDS_ON", "PARTNERS_WITH"},
			TransformsTo:     []string{},
		},
		AxisGeoConflict: {
			Key:             AxisGeoConflict,
			Label:           "Geopolitical Conflict",
			DefaultDamping:   0.80,
			DefaultMaxHops:   2,
			PropagationMode:  "downstream",
			StrongEdgeTypes:  []string{"LOCATED_IN", "OPERATES_IN"},
			TransformsTo:     []string{},
		},
	}
}
