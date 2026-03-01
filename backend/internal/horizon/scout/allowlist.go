package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EdgeAllowlistByDomainMode delegates to edgesemantics (single source of truth).
func EdgeAllowlistByDomainMode(mode string) []models.DependencyType {
	return edgesemantics.EdgeAllowlistByDomainMode(mode)
}

// EdgeAllowlistByDomainModes delegates to edgesemantics (single source of truth).
func EdgeAllowlistByDomainModes(modes []string) []models.DependencyType {
	return edgesemantics.EdgeAllowlistByDomainModes(modes)
}

// AllowlistContains delegates to edgesemantics (single source of truth).
func AllowlistContains(allowlist []models.DependencyType, t models.DependencyType) bool {
	return edgesemantics.AllowlistContains(allowlist, t)
}

// DomainScaffoldRelationTypesString delegates to edgesemantics (single source of truth).
func DomainScaffoldRelationTypesString() string {
	return edgesemantics.DomainScaffoldRelationTypesString()
}

// IsDomainScaffoldRelationType delegates to edgesemantics (single source of truth).
func IsDomainScaffoldRelationType(t models.DependencyType) bool {
	return edgesemantics.IsDomainScaffoldRelationType(t)
}
