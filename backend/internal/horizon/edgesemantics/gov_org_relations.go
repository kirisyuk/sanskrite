package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// GovOrgRelationSpec is the prompt-facing summary of allowed relations
// between /gov/ (legacy) / org subtype=gov and ordinary /org/ entities.
//
// Under the AH taxonomy /gov/ paths are shorthand for org with
// subtype=gov; the same direction discipline applies. The string still
// uses the legacy UPPER_SNAKE relation names because LLM prompts and
// frontend tooltips have been authored against them.
var GovOrgRelationSpec = `
GOV-ORG RELATION TYPES (canonical direction: /gov/ → /org/)

Allowed types and direction (source MUST be /gov/, target MUST be /org/):
- BENEFICIALLY_OWNS: government owns state-owned enterprise.
- OPERATES: superior gov body operates subordinate org/site.
- REGULATES: regulator (/gov/) regulates regulated entity (/org/, /site/, /market/, /route/).
- CONTROLS_VIA: gov controls org via non-equity arrangements.

Bidirectional (direction does not depend on gov vs org):
- JOINT_VENTURE_WITH, SUPPLIES, INFLUENCES, etc.
  For these, path order does not imply hierarchy.
`

// IsGovPath returns true if the canonical path is under /gov/.
func IsGovPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(path)), "/gov/")
}

// IsOrgPath returns true if the canonical path is under /org/.
func IsOrgPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(path)), "/org/")
}

// GovOrgDirectionSwapRelations enumerates AH relations where /org/ → /gov/
// is invalid and must be swapped to /gov/ → /org/. Government owns,
// operates, regulates, or controls organisations — never the reverse.
var GovOrgDirectionSwapRelations = map[ah.RelationName]bool{
	ah.RelationNameBeneficiallyOwns: true,
	ah.RelationNameOperates:         true,
	ah.RelationNameRegulates:        true,
	ah.RelationNameControlsVia:      true,
}

// ShouldSwapGovOrgDirection reports whether the (rel, sourcePath,
// targetPath) triple represents a /org/ → /gov/ edge that should be
// rewritten to /gov/ → /org/.
func ShouldSwapGovOrgDirection(rel ah.RelationName, sourcePath, targetPath string) bool {
	if !GovOrgDirectionSwapRelations[rel] {
		return false
	}
	return IsOrgPath(sourcePath) && IsGovPath(targetPath)
}
