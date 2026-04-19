package profile

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local aliases that retain the legacy DependencyType/FragmentType
// vocabulary used throughout the profile package while we run the AH
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
