package models

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// DeriveGeoSlug computes the primary geo token for top-level filtering.
// Preference order:
//  1. canonicalPath geo/jurisdiction slot
//  2. homeLocation ISO3166-1
func DeriveGeoSlug(canonicalPath string, homeLocation *Location) string {
	if geo := taxonomy.ExtractGeoSlugFromCanonicalPath(canonicalPath); geo != "" {
		return geo
	}
	if homeLocation == nil || homeLocation.ISO3166_1 == nil {
		return ""
	}
	geo := strings.ToLower(strings.TrimSpace(*homeLocation.ISO3166_1))
	if geo == "" {
		return ""
	}
	return geo
}
