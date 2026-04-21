package kzvat

import "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"

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

	FragmentTypeProgram      = ah.FragmentClassOrg
	FragmentTypeMediaChannel = ah.FragmentClassOrg
	FragmentTypeAffectedZone = ah.FragmentClassGeo
)

const (
	DependencyTypeOwns              = ah.RelationNameBeneficiallyOwns
	DependencyTypeControls          = ah.RelationNameControlsVia
	DependencyTypeProduces          = ah.RelationNameProduces
	DependencyTypeSuppliesTo        = ah.RelationNameSupplies
	DependencyTypeRegulates         = ah.RelationNameRegulates
	DependencyTypeAssures           = ah.RelationNameAssures
	DependencyTypeInfluences        = ah.RelationNameInfluences
	DependencyTypeGatekeepsDecision = ah.RelationNameGatekeeps
	DependencyTypeUsesMechanism     = ah.RelationNameWields
	DependencyTypeImposesCondition  = ah.RelationNameImposes
	DependencyTypeAdministers       = ah.RelationNameOperates
	DependencyTypeMemberOf          = ah.RelationNameMemberOf
	DependencyTypeAffects           = ah.RelationNameDisrupts
	DependencyTypeBoardMemberOf     = ah.RelationNameHoldsBoardSeat
	DependencyTypeMergedInto        = ah.RelationNameMergedInto

	DependencyTypeOperatesIn     DependencyType = "operates_in"
	DependencyTypeOperatesVia    DependencyType = "operates_via"
	DependencyTypeManages        DependencyType = "manages"
	DependencyTypeAffiliatedWith DependencyType = "affiliated_with"
	DependencyTypeParticipatesIn DependencyType = "participates_in"
	DependencyTypeInGeoScope     DependencyType = "in_geo_scope"
	DependencyTypeAppliesTo      DependencyType = "applies_to"
	DependencyTypeFocusesOn      DependencyType = "focuses_on"
	DependencyTypeDependsOn      DependencyType = "depends_on"
	DependencyTypeControlsAccess DependencyType = "controls_access"
)
