package models

// Semantic group names for edge types (TZ §6.2). Used for filtering, allowlist by domain mode, shouldRecurse.
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

// SemanticGroupForEdgeType returns the semantic group for a dependency type.
// Per TZ §6.2. After Stage 5 consolidation, domain group will only have FOCUSES_ON and DOMAIN_RELATION.
func SemanticGroupForEdgeType(t DependencyType) string {
	if g, ok := edgeTypeToSemanticGroup[t]; ok {
		return g
	}
	return SemanticGroupOther
}

var edgeTypeToSemanticGroup = map[DependencyType]string{
	// supply_chain
	DependencyTypeProduces:     SemanticGroupSupplyChain,
	DependencyTypeSuppliesTo:   SemanticGroupSupplyChain,
	DependencyTypeTransformsInto: SemanticGroupSupplyChain,
	DependencyTypeExtractsFrom: SemanticGroupSupplyChain,
	DependencyTypeLocatedIn:   SemanticGroupSupplyChain,
	DependencyTypeOperatesIn:  SemanticGroupSupplyChain,
	// governance
	DependencyTypeOwns:           SemanticGroupGovernance,
	DependencyTypeSubsidiaryOf:   SemanticGroupGovernance,
	DependencyTypeControls:      SemanticGroupGovernance,
	DependencyTypeInvestsIn:     SemanticGroupGovernance,
	DependencyTypeBoardMemberOf: SemanticGroupGovernance,
	DependencyTypeEmployedBy:    SemanticGroupGovernance,
	DependencyTypeFounded:      SemanticGroupGovernance,
	DependencyTypeAdministers:  SemanticGroupGovernance,
	// assurance (Third Line: audit, certification, regulatory oversight)
	DependencyTypeAssures:   SemanticGroupAssurance,
	DependencyTypeAuditedBy: SemanticGroupAssurance,
	DependencyTypeRegulates: SemanticGroupAssurance,
	// market
	DependencyTypeParticipatesIn:  SemanticGroupMarket,
	DependencyTypeHasMarketSegment: SemanticGroupMarket,
	DependencyTypeDominates:        SemanticGroupMarket,
	// strategic
	DependencyTypeControlsAccess:   SemanticGroupStrategic,
	DependencyTypeImposesCondition: SemanticGroupStrategic,
	DependencyTypeGatekeepsDecision: SemanticGroupStrategic,
	DependencyTypeUsesMechanism:    SemanticGroupStrategic,
	DependencyTypeAppliesTo:        SemanticGroupStrategic,
	// domain (canonical: FOCUSES_ON, DOMAIN_RELATION)
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
	// person (canonical: ASSOCIATED_WITH, EMPLOYED_BY, MEMBER_OF, RELATIVE_OF)
	DependencyTypeRelativeOf:     SemanticGroupPerson,
	DependencyTypeAssociatedWith: SemanticGroupPerson,
	DependencyTypeRelatedTo:      SemanticGroupPerson,
	DependencyTypeMemberOf:       SemanticGroupPerson,
	DependencyTypeAffiliatedWith: SemanticGroupPerson,
	DependencyTypePartnersWith:   SemanticGroupPerson,
	DependencyTypeWorksFor:       SemanticGroupPerson,
	DependencyTypeKeyPerson:      SemanticGroupPerson,
	DependencyTypeHasKeyPerson:   SemanticGroupPerson,
	// transition (canonical: CORPORATE_TRANSITION)
	DependencyTypeCorporateTransition: SemanticGroupTransition,
	DependencyTypeRenamedTo:           SemanticGroupTransition,
	DependencyTypeRebrandedTo:        SemanticGroupTransition,
	DependencyTypeLegalSuccessorOf:   SemanticGroupTransition,
	DependencyTypeMergedInto:         SemanticGroupTransition,
	DependencyTypeSpunOffFrom:        SemanticGroupTransition,
	DependencyTypeAcquiredBy:         SemanticGroupTransition,
	// other
	DependencyTypeCompetesWith: SemanticGroupOther,
	DependencyTypeInfluences:   SemanticGroupOther,
	DependencyTypeDependsOn:    SemanticGroupOther,
	DependencyTypeManages:      SemanticGroupOther,
	DependencyTypeOperatesVia:  SemanticGroupOther,
	DependencyTypeHasSubgroup:  SemanticGroupOther,
}
