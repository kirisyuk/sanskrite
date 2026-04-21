package lens

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
	FragmentTypeDomain       = ah.FragmentClassGeo
)
