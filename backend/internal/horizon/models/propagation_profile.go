package models

import "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"

// PropagationProfile defines how an axis propagates through the graph.
// See docs/notes/risk-architecture.md Contract 3 for the original
// pre-migration specification; the AH-canonical rewrite below is the
// single source of truth for runtime propagation.
type PropagationProfile struct {
	AxisKey         string               `json:"axisKey" firestore:"axisKey"`
	DefaultDamping  float64              `json:"defaultDamping" firestore:"defaultDamping"`
	DefaultMaxHops  int                  `json:"defaultMaxHops" firestore:"defaultMaxHops"`
	PropagationMode string               `json:"propagationMode" firestore:"propagationMode"`
	StrongEdges     []StrongEdgeSpec     `json:"strongEdges,omitempty" firestore:"strongEdges,omitempty"`
	Transformations []TransformationRule `json:"transformations,omitempty" firestore:"transformations,omitempty"`
}

// StrongEdgeSpec defines a strong edge for propagation. `Type` MUST be
// an AH RelationName (see internal/taxonomy/ah/taxonomy_generated.go).
// Legacy UPPER_SNAKE DependencyType strings are no longer accepted on
// this boundary; migrate-cli rewrites every persisted profile to the
// AH form before the runtime sees it.
type StrongEdgeSpec struct {
	Type      string  `json:"type" firestore:"type"`
	Direction string  `json:"direction" firestore:"direction"` // "source_to_target" | "target_to_source" | "both"
	Weight    float64 `json:"weight" firestore:"weight"`
}

// TransformationRule defines axis transformation when signal passes
// through an edge. `OnEdgeType` MUST be an AH RelationName.
type TransformationRule struct {
	OnEdgeType string  `json:"onEdgeType" firestore:"onEdgeType"`
	ToAxis     string  `json:"toAxis" firestore:"toAxis"`
	Factor     float64 `json:"factor" firestore:"factor"`
}

// DefaultPropagationProfiles returns the AH-canonical propagation
// profiles for all 12 axes.
//
// Translation provenance (legacy -> AH, per docs/taxonomy/migration_map.yaml):
//
//	OWNS                -> beneficially_owns
//	CONTROLS            -> controls_via
//	SUBSIDIARY_OF       -> subsidiary_of
//	REGULATES           -> regulates
//	IMPOSES_CONDITION   -> imposes              (rewrite_edge: org wields mechanism + mechanism imposes target;
//	                                              the propagation-relevant tail is `imposes`)
//	APPLIES_TO          -> imposes              (same machinery as IMPOSES_CONDITION)
//	CONTROLS_ACCESS     -> imposes              (rewrite_edge to mechanism+imposes)
//	USES_MECHANISM      -> wields
//	GATEKEEPS_DECISION  -> gatekeeps
//	INFLUENCES          -> influences
//	ADMINISTERS         -> operates             (gov-operates-program, after PROGRAM folded to org)
//	PARTNERS_WITH       -> joint_venture_with
//	SUPPLIES_TO         -> supplies
//	DEPENDS_ON          -> per_pair_routing:
//	                         site<->site        -> supplies
//	                         org->mechanism     -> wields
//	                         org->regime        -> subject_to
//	                         product->site      -> requires_capability
//	                         product->product   -> siteTransforms[] (no edge; propagation skips it)
//	                         org<->org          -> candidate_queue (propagation skips it)
//	INVESTS_IN          -> invested_in
//	PRODUCES            -> produces
//	EXTRACTS_FROM       -> produces             (with attributes.production_kind=extraction)
//	MANAGES             -> managed_by           (reversed: A MANAGES B becomes B managed_by A;
//	                                              propagation direction flips accordingly)
//	AFFECTS             -> disrupts             (with attributes.disruption_kind)
//	PARTICIPATES_IN     -> per_pair_routing:
//	                         org/person<->community -> member_of
//	                         org->market, *->evidence -> attributes (no edge)
//	TRADED_IN           -> listed_on
//	HAS_MARKET_SEGMENT  -> attributes (no edge; propagation entry removed)
//	TRANSFORMS_INTO     -> siteTransforms attribute (no edge; propagation entry removed)
//	ASSEMBLES_INTO      -> siteTransforms attribute (no edge; propagation entry removed)
//	USED_IN             -> dropped (inverse of produces; FE always queries by produces)
//	OPERATES_IN         -> attributes.operating_geos (no edge; propagation entry removed)
//	LOCATED_IN          -> canonical_path slot (no edge; propagation entry removed)
//	TRIGGERED_BY        -> attributes.triggers (no edge)
//
// Where an old edge collapses to a non-edge (attributes/canonical_path/
// siteTransforms), the entry simply disappears from the profile.
// Logistics axes additionally pick up `leg_connects` and `passes_through`
// because the legacy /supply_link + /route overlay is now an explicit
// chain of leg_connects edges through /route fragments.
func DefaultPropagationProfiles() map[string]PropagationProfile {
	return map[string]PropagationProfile{
		AxisLegalAccess: {
			AxisKey:         AxisLegalAccess,
			DefaultDamping:  0.95,
			DefaultMaxHops:  5,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameBeneficiallyOwns), Direction: "target_to_source", Weight: 1.0}, // OWNS
				{Type: string(ah.RelationNameControlsVia), Direction: "target_to_source", Weight: 0.9},     // CONTROLS
				{Type: string(ah.RelationNameSubsidiaryOf), Direction: "source_to_target", Weight: 1.0},
				{Type: string(ah.RelationNameRegulates), Direction: "source_to_target", Weight: 1.2},
				// IMPOSES_CONDITION + APPLIES_TO + CONTROLS_ACCESS all collapse onto `imposes`;
				// keep the strongest legacy weight (1.1).
				{Type: string(ah.RelationNameImposes), Direction: "source_to_target", Weight: 1.1},
				{Type: string(ah.RelationNameWields), Direction: "source_to_target", Weight: 1.05},   // USES_MECHANISM
				{Type: string(ah.RelationNameGatekeeps), Direction: "source_to_target", Weight: 1.08}, // GATEKEEPS_DECISION
				{Type: string(ah.RelationNameInfluences), Direction: "both", Weight: 0.85},
				{Type: string(ah.RelationNameOperates), Direction: "source_to_target", Weight: 0.95}, // ADMINISTERS (gov operates a program-org)
			},
			Transformations: []TransformationRule{
				{OnEdgeType: string(ah.RelationNameJointVentureWith), ToAxis: AxisLegalCompliance, Factor: 0.8}, // PARTNERS_WITH
			},
		},
		AxisLegalCompliance: {
			AxisKey:         AxisLegalCompliance,
			DefaultDamping:  0.85,
			DefaultMaxHops:  3,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameJointVentureWith), Direction: "both", Weight: 0.9}, // PARTNERS_WITH
				// SUPPLIES_TO + DEPENDS_ON(site<->site) both collapse to supplies; pick the higher legacy weight.
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 0.8},
				{Type: string(ah.RelationNameRegulates), Direction: "source_to_target", Weight: 1.1},
				{Type: string(ah.RelationNameImposes), Direction: "source_to_target", Weight: 1.08},
				{Type: string(ah.RelationNameInfluences), Direction: "both", Weight: 0.82},
				{Type: string(ah.RelationNameWields), Direction: "source_to_target", Weight: 1.0},
				{Type: string(ah.RelationNameGatekeeps), Direction: "source_to_target", Weight: 1.05},
				// DEPENDS_ON(org->regime) collapses to subject_to; relevant for legal/compliance.
				{Type: string(ah.RelationNameSubjectTo), Direction: "source_to_target", Weight: 1.0},
			},
			Transformations: []TransformationRule{},
		},
		AxisFinancialSettlement: {
			AxisKey:         AxisFinancialSettlement,
			DefaultDamping:  0.85,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 1.0}, // DEPENDS_ON(site<->site) -> supplies
				{Type: string(ah.RelationNameBeneficiallyOwns), Direction: "both", Weight: 0.85},
				{Type: string(ah.RelationNameInvestedIn), Direction: "both", Weight: 0.75},
				// DEPENDS_ON(org->regime) -> subject_to; financial regimes propagate settlement risk.
				{Type: string(ah.RelationNameSubjectTo), Direction: "source_to_target", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisFinancialCredit: {
			AxisKey:         AxisFinancialCredit,
			DefaultDamping:  0.80,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 1.0},
				{Type: string(ah.RelationNameBeneficiallyOwns), Direction: "both", Weight: 0.9},
				{Type: string(ah.RelationNameInvestedIn), Direction: "both", Weight: 0.8},
				{Type: string(ah.RelationNameSubjectTo), Direction: "source_to_target", Weight: 0.85},
			},
			Transformations: []TransformationRule{},
		},
		AxisPhysicalFlowSupply: {
			AxisKey:         AxisPhysicalFlowSupply,
			DefaultDamping:  0.75,
			DefaultMaxHops:  4,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				// SUPPLIES_TO + DEPENDS_ON(site<->site) collapse to supplies.
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 1.0},
				// PRODUCES + EXTRACTS_FROM both collapse to `produces`; the legacy
				// EXTRACTS_FROM(target_to_source) signal direction (deposit -> site) is
				// the same source_to_target after migration because the deposit becomes
				// the source of the produces edge per migration_map.yaml.
				{Type: string(ah.RelationNameProduces), Direction: "source_to_target", Weight: 0.85},
				// DEPENDS_ON(product->site) collapses to requires_capability (product -> site);
				// signal at the site flows back to the product (target_to_source).
				{Type: string(ah.RelationNameRequiresCapability), Direction: "target_to_source", Weight: 0.85},
				// MANAGES (A->B) becomes B managed_by A; legacy direction was source_to_target,
				// so after the swap the same physical flow becomes target_to_source.
				{Type: string(ah.RelationNameManagedBy), Direction: "target_to_source", Weight: 0.80},
				{Type: string(ah.RelationNameControlsVia), Direction: "source_to_target", Weight: 0.78},     // CONTROLS
				{Type: string(ah.RelationNameBeneficiallyOwns), Direction: "source_to_target", Weight: 0.75}, // OWNS
				{Type: string(ah.RelationNameJointVentureWith), Direction: "both", Weight: 0.6},              // PARTNERS_WITH
				// AH overlay: routes carry supplies via leg_connects + passes_through; signal
				// at any leg propagates to the upstream/downstream supplies endpoint.
				{Type: string(ah.RelationNameLegConnects), Direction: "both", Weight: 0.85},
				{Type: string(ah.RelationNamePassesThrough), Direction: "both", Weight: 0.7},
				// Disruptions on a route/site that carry physical product propagate as supply risk.
				{Type: string(ah.RelationNameDisrupts), Direction: "source_to_target", Weight: 0.85},
			},
			Transformations: []TransformationRule{},
		},
		AxisPhysicalFlowDemand: {
			AxisKey:         AxisPhysicalFlowDemand,
			DefaultDamping:  0.75,
			DefaultMaxHops:  4,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				// SUPPLIES_TO(target_to_source) -> supplies(target_to_source): demand flows
				// from customer back to supplier.
				{Type: string(ah.RelationNameSupplies), Direction: "target_to_source", Weight: 0.95},
				// DEPENDS_ON(product->site) -> requires_capability(source_to_target): demand
				// at the product propagates to the sites that have to deliver capability.
				{Type: string(ah.RelationNameRequiresCapability), Direction: "source_to_target", Weight: 0.9},
				// PRODUCES(target_to_source): demand at the produced product flows back to
				// the producing site/deposit; same direction post-EXTRACTS_FROM merge.
				{Type: string(ah.RelationNameProduces), Direction: "target_to_source", Weight: 0.88},
				// AH overlay: leg_connects also propagates upstream demand through routes.
				{Type: string(ah.RelationNameLegConnects), Direction: "target_to_source", Weight: 0.8},
				// HAS_MARKET_SEGMENT was the legacy bridge between an org and the market
				// it serves (signal flowed market->org). In AH the org<->market wiring is
				// folded onto attributes.market_segment_norms; the propagation-relevant
				// surface that survives as a graph edge is `influences` with
				// influence_kind=market_segment. Demand at a market reaches participating
				// orgs source_to_target.
				{Type: string(ah.RelationNameInfluences), Direction: "source_to_target", Weight: 0.8},
			},
			Transformations: []TransformationRule{},
		},
		AxisLogisticsCapacity: {
			AxisKey:         AxisLogisticsCapacity,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				// SUPPLIES_TO + DEPENDS_ON(site<->site) -> supplies; carries logistics demand
				// between supplier/customer endpoints.
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 0.95},
				// LOCATED_IN/OPERATES_IN are folded; the AH way to express
				// "this leg/site is constrained by capacity at /geo/X" is via
				// disrupts(geo -> site/route/mobile_asset/community).
				{Type: string(ah.RelationNameDisrupts), Direction: "source_to_target", Weight: 1.0},
				{Type: string(ah.RelationNameLegConnects), Direction: "both", Weight: 0.9},
				{Type: string(ah.RelationNamePassesThrough), Direction: "both", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisLogisticsLatency: {
			AxisKey:         AxisLogisticsLatency,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 0.95},
				{Type: string(ah.RelationNameDisrupts), Direction: "source_to_target", Weight: 1.0},
				{Type: string(ah.RelationNameLegConnects), Direction: "both", Weight: 0.9},
				{Type: string(ah.RelationNamePassesThrough), Direction: "both", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisOperationalContinuity: {
			AxisKey:         AxisOperationalContinuity,
			DefaultDamping:  0.70,
			DefaultMaxHops:  3,
			PropagationMode: "upstream",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameBeneficiallyOwns), Direction: "target_to_source", Weight: 1.0}, // OWNS
				// MANAGES(target_to_source) -> managed_by(source_to_target): same physical flow.
				{Type: string(ah.RelationNameManagedBy), Direction: "source_to_target", Weight: 0.9},
				// PRODUCES + EXTRACTS_FROM both collapse to produces; legacy direction was
				// target_to_source (signal at output flows back to producer).
				{Type: string(ah.RelationNameProduces), Direction: "target_to_source", Weight: 0.85},
				// AFFECTS -> disrupts (zone -> facility/org); same direction.
				{Type: string(ah.RelationNameDisrupts), Direction: "source_to_target", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisInfoUncertainty: {
			AxisKey:         AxisInfoUncertainty,
			DefaultDamping:  0.60,
			DefaultMaxHops:  2,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				// DEPENDS_ON(site<->site) -> supplies; DEPENDS_ON(org->regime) -> subject_to.
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 0.8},
				{Type: string(ah.RelationNameSubjectTo), Direction: "source_to_target", Weight: 0.85},
				{Type: string(ah.RelationNameJointVentureWith), Direction: "both", Weight: 0.7}, // PARTNERS_WITH
				// PARTICIPATES_IN(org/person -> community) -> member_of; uncertainty about a
				// community/program propagates to its members.
				{Type: string(ah.RelationNameMemberOf), Direction: "target_to_source", Weight: 0.7},
				{Type: string(ah.RelationNameRegulates), Direction: "source_to_target", Weight: 0.95},
				// IMPOSES_CONDITION + APPLIES_TO + CONTROLS_ACCESS all collapse to imposes.
				{Type: string(ah.RelationNameImposes), Direction: "source_to_target", Weight: 0.92},
				{Type: string(ah.RelationNameInfluences), Direction: "both", Weight: 0.88},
				{Type: string(ah.RelationNameWields), Direction: "source_to_target", Weight: 0.9},
				{Type: string(ah.RelationNameGatekeeps), Direction: "source_to_target", Weight: 0.9},
			},
			Transformations: []TransformationRule{},
		},
		AxisInfoReputation: {
			AxisKey:         AxisInfoReputation,
			DefaultDamping:  0.60,
			DefaultMaxHops:  2,
			PropagationMode: "both",
			StrongEdges: []StrongEdgeSpec{
				{Type: string(ah.RelationNameSupplies), Direction: "both", Weight: 0.8},
				{Type: string(ah.RelationNameJointVentureWith), Direction: "both", Weight: 0.7},
				{Type: string(ah.RelationNameMemberOf), Direction: "target_to_source", Weight: 0.7},
				// Reputation flows naturally through influences edges.
				{Type: string(ah.RelationNameInfluences), Direction: "both", Weight: 0.75},
			},
			Transformations: []TransformationRule{},
		},
		AxisGeoConflict: {
			AxisKey:         AxisGeoConflict,
			DefaultDamping:  0.80,
			DefaultMaxHops:  2,
			PropagationMode: "downstream",
			StrongEdges: []StrongEdgeSpec{
				// LOCATED_IN/OPERATES_IN are folded to canonical_path/attributes; the AH way
				// to express "conflict at /geo/X reaches site/route/org" is the explicit
				// disrupts edge from /geo (subtype=affected_zone) to the impaired endpoint.
				{Type: string(ah.RelationNameDisrupts), Direction: "source_to_target", Weight: 1.0},
				// Routes that pass through a conflict geo carry the risk to both ends.
				{Type: string(ah.RelationNamePassesThrough), Direction: "both", Weight: 0.9},
				// Geopolitical influence between actors stays as influences.
				{Type: string(ah.RelationNameInfluences), Direction: "both", Weight: 0.7},
			},
			Transformations: []TransformationRule{},
		},
	}
}
