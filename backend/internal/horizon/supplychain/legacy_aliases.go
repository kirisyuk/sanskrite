package supplychain

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local aliases that retain the legacy DependencyType/FragmentType
// vocabulary used throughout the supplychain package while we run the AH
// taxonomy "Big Bang" migration. Source-of-truth mapping:
// docs/taxonomy/migration_map.yaml.
//
// See backend/internal/horizon/scout/legacy_aliases.go for the canonical
// template — this file is a verbatim per-package replica.

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

// ProductTransitionEdgeTypes returns the canonical set of product->product
// transition relations consumed by supply-chain pathfinding. Mirror of the
// legacy models.ProductTransitionEdgeTypes() helper (now lives in
// pathbuilder/legacy_aliases.go) — replicated locally to avoid an
// import cycle with pathbuilder.
func ProductTransitionEdgeTypes() []DependencyType {
	return []DependencyType{
		DependencyTypeTransformsInto,
		DependencyTypeAssemblesInto,
		DependencyTypeUsedIn,
	}
}
