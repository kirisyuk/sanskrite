package signals

import "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"

// FragmentType and DependencyType name ah.FragmentClass / ah.RelationName within this package.
type FragmentType = ah.FragmentClass
type DependencyType = ah.RelationName

const (
	FragmentTypeOrganization  = ah.FragmentClassOrg
	FragmentTypeFacility      = ah.FragmentClassSite
	FragmentTypeGeoRegion     = ah.FragmentClassGeo
	FragmentTypeProduct       = ah.FragmentClassProduct
	FragmentTypeMarket        = ah.FragmentClassMarket
	FragmentTypePerson        = ah.FragmentClassPerson
	FragmentTypeCommunity     = ah.FragmentClassCommunity
	FragmentTypeDecisionPoint = ah.FragmentClassDecision
	FragmentTypeMechanism     = ah.FragmentClassMechanism
	FragmentTypeTrack         = ah.FragmentClassMobileAsset
	FragmentTypeEvent         = ah.FragmentClassEvidence
)

const (
	DependencyTypeOwns              = ah.RelationNameBeneficiallyOwns
	DependencyTypeSubsidiaryOf      = ah.RelationNameSubsidiaryOf
	DependencyTypeControls          = ah.RelationNameControlsVia
	DependencyTypeInvestsIn         = ah.RelationNameInvestedIn
	DependencyTypeSuppliesTo        = ah.RelationNameSupplies
	DependencyTypeProduces          = ah.RelationNameProduces
	DependencyTypeRegulates         = ah.RelationNameRegulates
	DependencyTypeAssures           = ah.RelationNameAssures
	DependencyTypeInfluences        = ah.RelationNameInfluences
	DependencyTypeGatekeepsDecision = ah.RelationNameGatekeeps
	DependencyTypeUsesMechanism     = ah.RelationNameWields
	DependencyTypeImposesCondition  = ah.RelationNameImposes
	DependencyTypeAdministers       = ah.RelationNameOperates
	DependencyTypeMemberOf          = ah.RelationNameMemberOf
	DependencyTypeTradedIn          = ah.RelationNameListedOn
	DependencyTypePartnersWith      = ah.RelationNameJointVentureWith
	DependencyTypeAffects           = ah.RelationNameDisrupts
	DependencyTypeBoardMemberOf     = ah.RelationNameHoldsBoardSeat
	DependencyTypeMergedInto        = ah.RelationNameMergedInto

	DependencyTypeOperatesIn          DependencyType = "operates_in"
	DependencyTypeOperatesVia         DependencyType = "operates_via"
	DependencyTypeManages             DependencyType = "manages"
	DependencyTypeHasSubgroup         DependencyType = "has_subgroup"
	DependencyTypeHasSubmarket        DependencyType = "has_submarket"
	DependencyTypeHasMarketSegment    DependencyType = "has_market_segment"
	DependencyTypeAffiliatedWith      DependencyType = "affiliated_with"
	DependencyTypeAssociatedWith      DependencyType = "associated_with"
	DependencyTypeEmployedBy          DependencyType = "employed_by"
	DependencyTypeWorksFor            DependencyType = "works_for"
	DependencyTypeFounded             DependencyType = "founded"
	DependencyTypeKeyPerson           DependencyType = "key_person"
	DependencyTypeHasKeyPerson        DependencyType = "has_key_person"
	DependencyTypeLocatedIn           DependencyType = "located_in"
	DependencyTypeDependsOn           DependencyType = "depends_on"
	DependencyTypeFocusesOn           DependencyType = "focuses_on"
	DependencyTypeCompetesWith        DependencyType = "competes_with"
	DependencyTypeParticipatesIn      DependencyType = "participates_in"
	DependencyTypeInGeoScope          DependencyType = "in_geo_scope"
	DependencyTypeControlsAccess      DependencyType = "controls_access"
	DependencyTypeAppliesTo           DependencyType = "applies_to"
	DependencyTypeTransformsInto      DependencyType = "transforms_into"
	DependencyTypeAssemblesInto       DependencyType = "assembles_into"
	DependencyTypeExtractsFrom        DependencyType = "extracts_from"
	DependencyTypeUsedIn              DependencyType = "used_in"
	DependencyTypeAuditedBy           DependencyType = "audited_by"
	DependencyTypeInvolvesActor       DependencyType = "involves_actor"
	DependencyTypeAbout               DependencyType = "about"
	DependencyTypeSurfaces            DependencyType = "surfaces"
	DependencyTypeRelatedTo           DependencyType = "related_to"
	DependencyTypeRelativeOf          DependencyType = "relative_of"
	DependencyTypeRenamedTo           DependencyType = "renamed_to"
	DependencyTypeRebrandedTo         DependencyType = "rebranded_to"
	DependencyTypeLegalSuccessorOf    DependencyType = "legal_successor_of"
	DependencyTypeSpunOffFrom         DependencyType = "spun_off_from"
	DependencyTypeAcquiredBy          DependencyType = "acquired_by"
	DependencyTypeCorporateTransition DependencyType = "corporate_transition"
	DependencyTypeDominates           DependencyType = "dominates"
	DependencyTypeDomainRelation      DependencyType = "domain_relation"
	DependencyTypeRelatedDomain       DependencyType = "related_domain"
	DependencyTypeBroadensTo          DependencyType = "broadens_to"
	DependencyTypeNarrowsTo           DependencyType = "narrows_to"
)

const (
	QualifierTransportVia                = "transport_via"
	QualifierAtFacility                  = "at_facility"
	QualifierFlowRefs                    = "flow_refs"
	QualifierZoneRefs                    = "zone_refs"
	QualifierAffectedZoneRefs            = "affected_zone_refs"
	QualifierFacet                       = "facet"
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
