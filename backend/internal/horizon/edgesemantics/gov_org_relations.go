package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// GovOrgRelationSpec defines allowed relation types between /gov/ and /org/ entities
// and their canonical direction. Both namespaces use FragmentTypeOrganization, so
// path-based rules are required to enforce correct semantics.
//
// Canonical direction: /gov/ (source) → /org/ (target) for governance relations.
// If a pipeline or LLM emits the reverse, the canonicalizer swaps source and target.
var GovOrgRelationSpec = `
GOV-ORG RELATION TYPES (canonical direction: /gov/ → /org/)

Allowed types and direction (source MUST be /gov/, target MUST be /org/):
- OWNS: government owns state-owned enterprise (e.g. /gov/iq/federal-government OWNS /org/iq/agricultural-cooperative-bank).
  WRONG: /org/ OWNS /gov/ — canonicalizer swaps to /gov/ OWNS /org/.
- ADMINISTERS: superior gov body administers subordinate org (e.g. ministry administers state-owned company).
  WRONG: /org/ ADMINISTERS /gov/ — canonicalizer swaps.
- REGULATES: regulator (/gov/) regulates regulated entity (/org/).
  WRONG: /org/ REGULATES /gov/ — canonicalizer swaps.
- CONTROLS: gov controls org (non-equity control, state control).
  Note: CONTROLS /product/ → /gov/|/org/ is also wrong; product cannot control org/gov.

Bidirectional (direction does not depend on gov vs org):
- PARTNERS_WITH, COMPETES_WITH, DEPENDS_ON, SUPPLIES_TO, INFLUENCES, etc.
  For these, path order does not imply hierarchy.
`

// IsGovPath returns true if the canonical path is under /gov/.
func IsGovPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "/gov/")
}

// IsOrgPath returns true if the canonical path is under /org/.
func IsOrgPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "/org/")
}

// GovOrgDirectionSwapTypes lists edge types where /org/ → /gov/ is invalid
// and must be swapped to /gov/ → /org/. Government owns/administers/regulates
// organizations, never the reverse.
var GovOrgDirectionSwapTypes = map[models.DependencyType]bool{
	models.DependencyTypeOwns:        true, // gov owns state-owned enterprises
	models.DependencyTypeAdministers: true, // gov administers subordinate orgs
	models.DependencyTypeRegulates:   true, // gov regulates orgs
}

// ShouldSwapGovOrgDirection returns true if the edge /org/ → /gov/ with the given
// type should be swapped to /gov/ → /org/. Used by canonicalizer.
func ShouldSwapGovOrgDirection(edgeType models.DependencyType, sourcePath, targetPath string) bool {
	if !GovOrgDirectionSwapTypes[edgeType] {
		return false
	}
	return IsOrgPath(sourcePath) && IsGovPath(targetPath)
}
