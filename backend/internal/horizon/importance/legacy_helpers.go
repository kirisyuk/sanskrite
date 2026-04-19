package importance

import (
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// edgeSubstitutability is a local replacement for the retired
// edge.GetSubstitutability() helper; the qualifier itself still lives on
// the free-form Qualifiers bag (see models/transmission_qualifiers.go).
func edgeSubstitutability(e *horizonmodels.DependencyEdge) float64 {
	if e == nil || e.Qualifiers == nil {
		return 0
	}
	raw, ok := e.Qualifiers[horizonmodels.QualifierAliasSubstitutability]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return clampUnit(v)
	case float32:
		return clampUnit(float64(v))
	case int:
		return clampUnit(float64(v))
	case int64:
		return clampUnit(float64(v))
	}
	return 0
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Legacy helper port of the deleted models.SemanticGroup* vocabulary and
// SemanticGroupForEdgeType resolver. Importance reuses the same closed
// vocabulary the scout package keeps in scout/legacy_helpers.go; this is
// a faithful local replica so the importance scorer/explainer can keep
// classifying edges without the now-removed models helper.

const (
	SemanticGroupSupplyChain = "supply_chain"
	SemanticGroupGovernance  = "governance"
	SemanticGroupAssurance   = "assurance"
	SemanticGroupMarket      = "market"
	SemanticGroupStrategic   = "strategic"
	SemanticGroupDomain      = "domain"
	SemanticGroupPerson      = "person"
	SemanticGroupTransition  = "transition"
	SemanticGroupOther       = "other"
)

func SemanticGroupForEdgeType(t DependencyType) string {
	if g, ok := edgeTypeToSemanticGroup[t]; ok {
		return g
	}
	return SemanticGroupOther
}

var edgeTypeToSemanticGroup = map[DependencyType]string{
	DependencyTypeProduces:       SemanticGroupSupplyChain,
	DependencyTypeSuppliesTo:     SemanticGroupSupplyChain,
	DependencyTypeTransformsInto: SemanticGroupSupplyChain,
	DependencyTypeAssemblesInto:  SemanticGroupSupplyChain,
	DependencyTypeUsedIn:         SemanticGroupSupplyChain,
	DependencyTypeExtractsFrom:   SemanticGroupSupplyChain,
	DependencyTypeLocatedIn:      SemanticGroupSupplyChain,
	DependencyTypeOperatesIn:     SemanticGroupSupplyChain,

	DependencyTypeOwns:          SemanticGroupGovernance,
	DependencyTypeSubsidiaryOf:  SemanticGroupGovernance,
	DependencyTypeControls:      SemanticGroupGovernance,
	DependencyTypeInvestsIn:     SemanticGroupGovernance,
	DependencyTypeBoardMemberOf: SemanticGroupGovernance,
	DependencyTypeEmployedBy:    SemanticGroupGovernance,
	DependencyTypeFounded:       SemanticGroupGovernance,
	DependencyTypeAdministers:   SemanticGroupGovernance,

	DependencyTypeAssures:   SemanticGroupAssurance,
	DependencyTypeAuditedBy: SemanticGroupAssurance,
	DependencyTypeRegulates: SemanticGroupAssurance,

	DependencyTypeParticipatesIn:   SemanticGroupMarket,
	DependencyTypeTradedIn:         SemanticGroupMarket,
	DependencyTypeHasMarketSegment: SemanticGroupMarket,
	DependencyTypeDominates:        SemanticGroupMarket,

	DependencyTypeControlsAccess:    SemanticGroupStrategic,
	DependencyTypeImposesCondition:  SemanticGroupStrategic,
	DependencyTypeGatekeepsDecision: SemanticGroupStrategic,
	DependencyTypeUsesMechanism:     SemanticGroupStrategic,
	DependencyTypeAppliesTo:         SemanticGroupStrategic,

	DependencyTypeFocusesOn:      SemanticGroupDomain,
	DependencyTypeDomainRelation: SemanticGroupDomain,
	DependencyTypeRelatedDomain:  SemanticGroupDomain,
	DependencyTypeBroadensTo:     SemanticGroupDomain,
	DependencyTypeNarrowsTo:      SemanticGroupDomain,
	DependencyTypeAffects:        SemanticGroupDomain,
	DependencyTypeAbout:          SemanticGroupDomain,
	DependencyTypeSurfaces:       SemanticGroupDomain,
	DependencyTypeInvolvesActor:  SemanticGroupDomain,
	DependencyTypeInGeoScope:     SemanticGroupDomain,

	DependencyTypeRelativeOf:     SemanticGroupPerson,
	DependencyTypeAssociatedWith: SemanticGroupPerson,
	DependencyTypeRelatedTo:      SemanticGroupPerson,
	DependencyTypeMemberOf:       SemanticGroupPerson,
	DependencyTypeAffiliatedWith: SemanticGroupPerson,
	DependencyTypePartnersWith:   SemanticGroupPerson,
	DependencyTypeWorksFor:       SemanticGroupPerson,
	DependencyTypeKeyPerson:      SemanticGroupPerson,
	DependencyTypeHasKeyPerson:   SemanticGroupPerson,

	DependencyTypeCorporateTransition: SemanticGroupTransition,
	DependencyTypeRenamedTo:           SemanticGroupTransition,
	DependencyTypeRebrandedTo:         SemanticGroupTransition,
	DependencyTypeLegalSuccessorOf:    SemanticGroupTransition,
	DependencyTypeMergedInto:          SemanticGroupTransition,
	DependencyTypeSpunOffFrom:         SemanticGroupTransition,
	DependencyTypeAcquiredBy:          SemanticGroupTransition,

	DependencyTypeCompetesWith: SemanticGroupOther,
	DependencyTypeInfluences:   SemanticGroupOther,
	DependencyTypeDependsOn:    SemanticGroupOther,
	DependencyTypeManages:      SemanticGroupOther,
	DependencyTypeOperatesVia:  SemanticGroupOther,
	DependencyTypeHasSubgroup:  SemanticGroupOther,
}
