package graphenrich

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// graphenrich-local facet enum and qualifier keys
// -------------------------------------------------------------------
// Under the legacy taxonomy the dependency-edge had a first-class
// `Facet` field (DependencyFacet enum) and a `Type` field
// (DependencyType). Both were dropped in P1 of the AH migration:
//   - DependencyType -> ah.RelationName (typed at the model layer).
//   - DependencyFacet -> retired entirely; facets now live as a free-
//     form qualifier (`facet`) on the Qualifiers bag, written by the
//     enrichment pipeline as a confidence-scored hint.
//
// The graphenrich package is the only writer/reader of the facet
// today; until (and if) facet is promoted into the AH attribute_spec
// the canonical strings live here. See migration_map.yaml ->
// dependency_facet for the rationale (facets folded into qualifiers).
//
// Same applies to the transmission-contract metadata qualifiers and
// the transport_via / flow_refs / zone_refs / affected_zone_refs keys
// that the propagation layer reads — they were qualifier-bag strings
// before and remain qualifier-bag strings now, but with their string
// constants centralised here for type safety inside graphenrich.

// EdgeFacet is the heuristic facet inferred for a dependency edge.
// Strings match the legacy DependencyFacet enum so existing data on
// disk and downstream readers (propagation, lens UI) keep working.
type EdgeFacet string

const (
	FacetLogistics      EdgeFacet = "logistics"
	FacetInfrastructure EdgeFacet = "infrastructure"
	FacetSupply         EdgeFacet = "supply"
	FacetDistribution   EdgeFacet = "distribution"
	FacetRegulatory     EdgeFacet = "regulatory"
	FacetPayment        EdgeFacet = "payment"
)

// QualifierFacet is the qualifier-bag key under which the inferred
// facet string is stored.
const QualifierFacet = "facet"

// Transmission-contract metadata qualifier keys + values. These are
// written to `edge.Qualifiers` so the propagation layer can audit
// which transmission inputs were inferred vs. provided.
const (
	QualifierTransmissionContractVersion = "transmission_contract_version"
	QualifierTransmissionContractStatus  = "transmission_contract_status"
	QualifierTransmissionContractMode    = "transmission_contract_mode"
	QualifierTransmissionContractMissing = "transmission_contract_missing"

	TransmissionContractVersionV1 = "v1"

	TransmissionContractStatusComplete = "complete"
	TransmissionContractStatusPartial  = "partial"
	TransmissionContractStatusMissing  = "missing"

	TransmissionContractModeInferred = "inferred"
	TransmissionContractModeExplicit = "explicit"
)

// Edge qualifier keys for routing/zone references.
const (
	QualifierTransportVia     = "transport_via"
	QualifierFlowRefs         = "flow_refs"
	QualifierZoneRefs         = "zone_refs"
	QualifierAffectedZoneRefs = "affected_zone_refs"
)

// edgeFacet returns the inferred facet for an edge as stored in the
// qualifiers bag. Returns "" when no facet has been inferred yet.
func edgeFacet(edge *horizonmodels.DependencyEdge) EdgeFacet {
	if edge == nil || edge.Qualifiers == nil {
		return ""
	}
	raw, ok := edge.Qualifiers[QualifierFacet]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return EdgeFacet(strings.TrimSpace(v))
	case EdgeFacet:
		return v
	default:
		return ""
	}
}

// supplyFlowRelations is the AH relation set that historically mapped
// onto the legacy SUPPLIES_TO/PRODUCES/EXTRACTS_FROM family. Used by
// heuristics + facet scoring to cluster supply-side semantics.
var supplyFlowRelations = map[ah.RelationName]struct{}{
	ah.RelationNameSupplies: {},
	ah.RelationNameProduces: {},
}

// isSupplyFlowRelation reports whether the relation belongs to the
// AH supply family (supplies + produces). Production qualifiers like
// production_kind + production_role replace the legacy distinction
// between SUPPLIES_TO / PRODUCES / EXTRACTS_FROM.
func isSupplyFlowRelation(rel ah.RelationName) bool {
	_, ok := supplyFlowRelations[rel]
	return ok
}

// employmentRelations is the AH relation set covering historic
// EMPLOYED_BY / BOARD_MEMBER_OF / MANAGES (now consolidated under
// member_of with role qualifier per migration_map.yaml).
var employmentRelations = map[ah.RelationName]struct{}{
	ah.RelationNameMemberOf: {},
}

func isEmploymentRelation(rel ah.RelationName) bool {
	_, ok := employmentRelations[rel]
	return ok
}
