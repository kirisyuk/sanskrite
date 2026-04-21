package supplychain

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
)

const (
	DependencyTypeOwns       = ah.RelationNameBeneficiallyOwns
	DependencyTypeControls   = ah.RelationNameControlsVia
	DependencyTypeProduces   = ah.RelationNameProduces
	DependencyTypeSuppliesTo = ah.RelationNameSupplies

	DependencyTypeTransformsInto DependencyType = "transforms_into"
	DependencyTypeAssemblesInto  DependencyType = "assembles_into"
	DependencyTypeOperatesIn     DependencyType = "operates_in"
	DependencyTypeManages        DependencyType = "manages"
	DependencyTypeUsedIn         DependencyType = "used_in"
)

// ProductTransitionEdgeTypes returns product→product transition relations for pathfinding.
// Kept local to avoid an import cycle with pathbuilder.
func ProductTransitionEdgeTypes() []DependencyType {
	return []DependencyType{
		DependencyTypeTransformsInto,
		DependencyTypeAssemblesInto,
		DependencyTypeUsedIn,
	}
}
