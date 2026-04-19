package horizon

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// legacy_aliases.go — package-local aliases that retain the legacy
// FragmentType/DependencyType vocabulary used throughout the http/horizon
// package while we run the AgentHorde taxonomy "Big Bang" migration.
// Source-of-truth mapping: docs/taxonomy/migration_map.yaml.
//
// The intent is to keep handler-package source diff readable: instead of
// rewriting every call site to use ah.RelationName / ah.FragmentClass
// directly, we expose the legacy Go identifiers (FragmentType,
// FragmentTypeOrganization, DependencyType, DependencyTypeOwns, …) at
// http/horizon-package scope. They resolve to the canonical AgentHorde
// values per the migration map. Subtype-bearing fragment classes collapse
// into the parent class (program/media_channel/deposit/supply_link/
// affected_zone resolve to org/site/geo respectively); callers that need
// the subtype still inspect frag.Subtype directly or use
// frag.IsLegacySupplyLink().
//
// Where the legacy concept has no direct AH equivalent (PARTICIPATES_IN,
// COMPETES_WITH, DEPENDS_ON, OPERATES_VIA, …) the constant is defined as
// a snake_case ah.RelationName literal so the handler can keep matching
// legacy persisted strings during the migration window.

// FragmentType is the package-local alias for ah.FragmentClass.
type FragmentType = ah.FragmentClass

// DependencyType is the package-local alias for ah.RelationName.
type DependencyType = ah.RelationName

// Fragment class constants (legacy names, AH values).
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

	// Legacy fragment-type identifiers PROGRAM, MEDIA_CHANNEL, DEPOSIT,
	// SUPPLY_LINK, AFFECTED_ZONE resolve to a (Class, Subtype) tuple under
	// the AH taxonomy. The aliases below collapse to the parent FragmentClass
	// only — typed callers that need the subtype must inspect frag.Subtype
	// directly (or the helper predicates frag.IsLegacyProgram() etc.). This
	// mirrors backend/internal/horizon/lens/legacy_aliases.go, which makes
	// the same trade-off for path-based inference helpers.
	//
	//   PROGRAM        -> Class=org,  Subtype=program        (models.ClassSubtypeProgram)
	//   MEDIA_CHANNEL  -> Class=org,  Subtype=media_channel  (models.ClassSubtypeMediaChannel)
	//   DEPOSIT        -> Class=site, Subtype=deposit        (models.ClassSubtypeDeposit)
	//   SUPPLY_LINK    -> Class=site, Subtype=supply_link    (models.ClassSubtypeSupplyLink)
	//   AFFECTED_ZONE  -> Class=geo,  Subtype=affected_zone  (models.ClassSubtypeAffectedZone)
	FragmentTypeProgram      = ah.FragmentClassOrg
	FragmentTypeMediaChannel = ah.FragmentClassOrg
	FragmentTypeDeposit      = ah.FragmentClassSite
	FragmentTypeSupplyLink   = ah.FragmentClassSite
	FragmentTypeAffectedZone = ah.FragmentClassGeo

	// DOMAIN/SPHERE classes were dropped post-AH; survivors live in
	// canonical_path overlays (see isDomainFragment / isSphereFragment).
	// We retain a path-typed alias for inferFragmentTypeFromPath so the
	// /domain/* prefix can route to a non-empty FragmentClass; ah.FragmentClassGeo
	// is the closest neutral bucket (matches lens/legacy_aliases.go).
	FragmentTypeDomain = ah.FragmentClassGeo
)

// Dependency relation constants (legacy names, AH values where they exist;
// snake_case literal otherwise so handlers can keep reading legacy data).
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

	// Legacy snake_case literals — no AH edge equivalent. Handlers keep
	// these for read-side back-compat with pre-migration Firestore data.
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

// Qualifier-bag string keys that used to live on the models package and
// were moved to graphenrich/facets.go (canonical writers) and to local
// heuristics. Keep the legacy names available as plain string constants
// so handler code can keep referring to them.
const (
	QualifierTransportVia     = "transport_via"
	QualifierAtFacility       = "at_facility"
	QualifierFlowRefs         = "flow_refs"
	QualifierZoneRefs         = "zone_refs"
	QualifierAffectedZoneRefs = "affected_zone_refs"
	QualifierFacet            = "facet"

	// Path/route qualifier keys used by supply-chain leg materialization.
	QualifierRouteMode            = "route_mode"
	QualifierGeoRefs              = "geo_refs"
	QualifierAlternateFlowRefs    = "alternate_flow_refs"
	QualifierSpatialSemantics     = "spatial_semantics"
	QualifierBindingConfidence    = "binding_confidence"
	QualifierPathStressScore      = "path_stress_score"
	QualifierPathFragility        = "path_fragility"
	QualifierPathRerouteability   = "path_rerouteability"
	QualifierPathZonePressure     = "path_zone_pressure"
	QualifierPathAffectedPressure = "path_affected_pressure"
	QualifierRecoveryLatencyHours = "recovery_latency_hours"
)
