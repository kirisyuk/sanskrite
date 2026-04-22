package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// normalizeRelationToken accepts canonical AH snake_case relation names and the
// remaining legacy wire aliases, and always returns the canonical AH relation
// slug. Unknown or dropped relations return an empty string instead of being
// silently coerced into a generic fallback.
func normalizeRelationToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if rel, ok := ah.NormalizeRelation(raw); ok {
		return string(rel)
	}
	if dec, ok := models.NormalizeRelationName(raw); ok {
		if dec.Dropped || dec.Relation == "" {
			return ""
		}
		return string(dec.Relation)
	}
	if normalized := normalizeToScoutRelation(raw); normalized != "" {
		if _, ok := scoutKnownDependencyTypes[normalized]; ok {
			return string(normalized)
		}
	}
	return ""
}
