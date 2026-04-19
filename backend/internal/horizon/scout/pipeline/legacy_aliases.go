package pipeline

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local aliases that retain the legacy DependencyType/FragmentType
// vocabulary used throughout the scout package while we run the AH taxonomy
// "Big Bang" migration. Source-of-truth mapping:
// docs/taxonomy/migration_map.yaml.
//
// The intent is to keep scout-package source diff readable: instead of
// rewriting ~95 files to use ah.RelationName / ah.FragmentClass directly
// at every call site, we expose the legacy Go identifiers (FragmentType,
// FragmentTypeOrganization, DependencyType, DependencyTypeOwns, …) at
// scout-package scope. They resolve to the canonical AgentHorde values
// per the migration map. Subtype-bearing fragment classes collapse into
// the parent class (program/media_channel/deposit/supply_link/affected_zone
// resolve to org/site/geo respectively); callers that need the subtype
// still inspect frag.Subtype directly or use frag.IsLegacySupplyLink().
//
// Where the legacy concept has no direct AH equivalent (PARTICIPATES_IN,
// COMPETES_WITH, DEPENDS_ON, OPERATES_VIA, …) the constant is defined as
// a snake_case ah.RelationName literal so scout can keep matching legacy
// persisted strings and emit them in scaffolds during the migration window.

// FragmentType is the scout-local alias for ah.FragmentClass.
type FragmentType = ah.FragmentClass

// DependencyType is the scout-local alias for ah.RelationName.
type DependencyType = ah.RelationName

// Fragment class constants (legacy names, AH values).
const (
	FragmentTypeOrganization = ah.FragmentClassOrg
	FragmentTypeFacility     = ah.FragmentClassSite
	FragmentTypeGeoRegion    = ah.FragmentClassGeo
	FragmentTypeProduct      = ah.FragmentClassProduct
	FragmentTypeMarket       = ah.FragmentClassMarket
	FragmentTypePerson       = ah.FragmentClassPerson
	FragmentTypeCommunity    = ah.FragmentClassCommunity
	FragmentTypeDecisionPoint = ah.FragmentClassDecision
	FragmentTypeMechanism    = ah.FragmentClassMechanism
	FragmentTypeTrack        = ah.FragmentClassMobileAsset
	FragmentTypeEvent        = ah.FragmentClassEvidence

	// NOTE: Legacy fragment-type identifiers PROGRAM, MEDIA_CHANNEL, DEPOSIT,
	// SUPPLY_LINK, AFFECTED_ZONE, DOMAIN, SPHERE are intentionally NOT exposed
	// as ah.FragmentClass aliases here. Those legacy values resolve to a
	// (Class, Subtype) tuple under the AH taxonomy:
	//
	//   PROGRAM        -> Class=org,  Subtype=program        (use models.ClassSubtypeProgram      / frag.IsLegacyProgram())
	//   MEDIA_CHANNEL  -> Class=org,  Subtype=media_channel  (use models.ClassSubtypeMediaChannel / frag.IsLegacyMediaChannel())
	//   DEPOSIT        -> Class=site, Subtype=deposit        (use models.ClassSubtypeDeposit      / frag.IsLegacyDeposit())
	//   SUPPLY_LINK    -> Class=site, Subtype=supply_link    (use models.ClassSubtypeSupplyLink   / frag.IsLegacySupplyLink())
	//   AFFECTED_ZONE  -> Class=geo,  Subtype=affected_zone  (use models.ClassSubtypeAffectedZone / frag.IsLegacyAffectedZone())
	//   DOMAIN/SPHERE  -> folded to attributes/canonical-path overlay (use frag.IsLegacyDomain() / frag.IsLegacySphere())
	//
	// Exposing these as bare ah.FragmentClass values would silently mismatch
	// real persisted fragments (which carry the canonical Class+Subtype) and
	// poison Firestore queries; do not re-introduce them in any form.
)

// Dependency relation constants (legacy names, AH values where they exist;
// snake_case literal otherwise so scout can keep reading legacy data).
const (
	// Direct AH equivalents.
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

	// Legacy snake_case literals — no AH edge equivalent. Scout keeps
	// these for read-side back-compat with pre-migration Firestore data
	// and for scaffold emission during the migration window.
	DependencyTypeOperatesIn        DependencyType = "operates_in"
	DependencyTypeOperatesVia       DependencyType = "operates_via"
	DependencyTypeManages           DependencyType = "manages"
	DependencyTypeHasSubgroup       DependencyType = "has_subgroup"
	DependencyTypeHasSubmarket      DependencyType = "has_submarket"
	DependencyTypeHasMarketSegment  DependencyType = "has_market_segment"
	DependencyTypeAffiliatedWith    DependencyType = "affiliated_with"
	DependencyTypeAssociatedWith    DependencyType = "associated_with"
	DependencyTypeEmployedBy        DependencyType = "employed_by"
	DependencyTypeWorksFor          DependencyType = "works_for"
	DependencyTypeFounded           DependencyType = "founded"
	DependencyTypeKeyPerson         DependencyType = "key_person"
	DependencyTypeHasKeyPerson      DependencyType = "has_key_person"
	DependencyTypeLocatedIn         DependencyType = "located_in"
	DependencyTypeDependsOn         DependencyType = "depends_on"
	DependencyTypeFocusesOn         DependencyType = "focuses_on"
	DependencyTypeCompetesWith      DependencyType = "competes_with"
	DependencyTypeParticipatesIn    DependencyType = "participates_in"
	DependencyTypeInGeoScope        DependencyType = "in_geo_scope"
	DependencyTypeControlsAccess    DependencyType = "controls_access"
	DependencyTypeAppliesTo         DependencyType = "applies_to"
	DependencyTypeTransformsInto    DependencyType = "transforms_into"
	DependencyTypeAssemblesInto     DependencyType = "assembles_into"
	DependencyTypeExtractsFrom      DependencyType = "extracts_from"
	DependencyTypeUsedIn            DependencyType = "used_in"
	DependencyTypeAuditedBy         DependencyType = "audited_by"
	DependencyTypeInvolvesActor     DependencyType = "involves_actor"
	DependencyTypeAbout             DependencyType = "about"
	DependencyTypeSurfaces          DependencyType = "surfaces"
	DependencyTypeRelatedTo         DependencyType = "related_to"
	DependencyTypeRelativeOf        DependencyType = "relative_of"
	DependencyTypeRenamedTo         DependencyType = "renamed_to"
	DependencyTypeRebrandedTo       DependencyType = "rebranded_to"
	DependencyTypeLegalSuccessorOf  DependencyType = "legal_successor_of"
	DependencyTypeSpunOffFrom       DependencyType = "spun_off_from"
	DependencyTypeAcquiredBy        DependencyType = "acquired_by"
	DependencyTypeCorporateTransition DependencyType = "corporate_transition"
	DependencyTypeDominates         DependencyType = "dominates"
	DependencyTypeDomainRelation    DependencyType = "domain_relation"
	DependencyTypeRelatedDomain     DependencyType = "related_domain"
	DependencyTypeBroadensTo        DependencyType = "broadens_to"
	DependencyTypeNarrowsTo         DependencyType = "narrows_to"
)

// Qualifier-bag string keys that used to live on the models package and
// were moved to graphenrich/facets.go (canonical writers) and to local
// heuristics. Keep the legacy names available to scout as plain string
// constants so transition code can keep referring to them.
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
