package dubaicit

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local aliases that retain the legacy DependencyType/FragmentType
// vocabulary used throughout the dubaicit scenario seeder while we run the
// AH taxonomy "Big Bang" migration. Source-of-truth mapping:
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

	// Subtype-bearing legacy fragment kinds folded into a parent class
	// (the FragmentSeed only carries Class today; subtype distinction
	// lives in the canonical path the seeder writes for each fragment).
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

	DependencyTypeOperatesIn       DependencyType = "operates_in"
	DependencyTypeOperatesVia      DependencyType = "operates_via"
	DependencyTypeManages          DependencyType = "manages"
	DependencyTypeAffiliatedWith   DependencyType = "affiliated_with"
	DependencyTypeParticipatesIn   DependencyType = "participates_in"
	DependencyTypeInGeoScope       DependencyType = "in_geo_scope"
	DependencyTypeAppliesTo        DependencyType = "applies_to"
	DependencyTypeFocusesOn        DependencyType = "focuses_on"
	DependencyTypeDependsOn        DependencyType = "depends_on"
)
