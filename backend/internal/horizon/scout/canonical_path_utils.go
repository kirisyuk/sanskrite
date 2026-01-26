package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// InferFragmentTypeFromCanonicalPath returns a best-effort fragment type based on canonical path prefix.
// It is intentionally conservative and used for early canonicalization/validation plumbing when only a path is available.
func InferFragmentTypeFromCanonicalPath(path string) models.FragmentType {
	p := strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(p, "/domain/"):
		return models.FragmentTypeDomain
	case strings.HasPrefix(p, "/decision/"):
		return models.FragmentTypeDecisionPoint
	case strings.HasPrefix(p, "/mechanism/"):
		return models.FragmentTypeMechanism
	case strings.HasPrefix(p, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(p, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(p, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(p, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(p, "/loc/") || strings.HasPrefix(p, "/region/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(p, "/event/"):
		return models.FragmentTypeEvent
	case strings.HasPrefix(p, "/person/"):
		return models.FragmentTypePerson
	case strings.HasPrefix(p, "/media/"):
		return models.FragmentTypeMediaChannel
	case strings.HasPrefix(p, "/deposit/"):
		return models.FragmentTypeDeposit
	case strings.HasPrefix(p, "/community/"):
		return models.FragmentTypeCommunity
	case strings.HasPrefix(p, "/org/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(p, "/gov/"):
		// Government bodies are modeled as ORGANIZATION but use /gov namespace.
		return models.FragmentTypeOrganization
	default:
		return models.FragmentTypeOrganization
	}
}

