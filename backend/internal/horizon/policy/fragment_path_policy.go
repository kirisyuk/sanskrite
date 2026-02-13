package policy

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var fragmentPathPrefixes = map[horizonmodels.FragmentType]string{
	horizonmodels.FragmentTypeOrganization:  "/org/",
	horizonmodels.FragmentTypeFacility:      "/fac/",
	horizonmodels.FragmentTypeGeoRegion:     "/loc/",
	horizonmodels.FragmentTypeProduct:       "/product/",
	horizonmodels.FragmentTypePerson:        "/person/",
	horizonmodels.FragmentTypeMediaChannel:  "/media/",
	horizonmodels.FragmentTypeCommunity:     "/community/",
	horizonmodels.FragmentTypeDomain:        "/domain/",
	horizonmodels.FragmentTypeSphere:        "/sphere/",
	horizonmodels.FragmentTypeSupplyLink:    "/supply-link/",
	horizonmodels.FragmentTypeProgram:       "/program/",
	horizonmodels.FragmentTypeMarket:        "/market/",
	horizonmodels.FragmentTypeEvent:         "/event/",
	horizonmodels.FragmentTypeDeposit:       "/deposit/",
	horizonmodels.FragmentTypeDecisionPoint: "/decision/",
	horizonmodels.FragmentTypeMechanism:     "/mechanism/",
}

// ExpectedCanonicalPathPrefix returns the canonical path prefix for fragment type.
func ExpectedCanonicalPathPrefix(fragType horizonmodels.FragmentType) string {
	return fragmentPathPrefixes[fragType]
}

// IsCanonicalPathAllowedForType checks path prefix with known exceptions.
func IsCanonicalPathAllowedForType(fragType horizonmodels.FragmentType, canonicalPath string) bool {
	path := strings.TrimSpace(canonicalPath)
	if path == "" {
		return false
	}
	expectedPrefix := ExpectedCanonicalPathPrefix(fragType)
	if expectedPrefix == "" {
		return true
	}
	if strings.HasPrefix(path, expectedPrefix) {
		return true
	}
	// Government entities remain ORGANIZATION in type system but use /gov/* path namespace.
	return fragType == horizonmodels.FragmentTypeOrganization && strings.HasPrefix(path, "/gov/")
}
