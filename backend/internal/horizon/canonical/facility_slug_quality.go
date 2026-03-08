package canonical

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var facilityGenericSlugTokens = map[string]struct{}{
	"general":             {},
	"production":          {},
	"distribution":        {},
	"center":              {},
	"centre":              {},
	"facility":            {},
	"facilities":          {},
	"plant":               {},
	"factory":             {},
	"fab":                 {},
	"terminal":            {},
	"hub":                 {},
	"logistics":           {},
	"warehouse":           {},
	"depot":               {},
	"manufacturing":       {},
	"assembly":            {},
	"processing":          {},
	"station":             {},
	"site":                {},
	"complex":             {},
	"industrial":          {},
	"international":       {},
	"export":              {},
	"import":              {},
	"trade":               {},
	"distribution-center": {},
}

var facilityForbiddenPlaceholderSlugs = map[string]struct{}{
	"general-production":   {},
	"general-distribution": {},
	"general-logistics":    {},
	"general-terminal":     {},
	"general-facility":     {},
	"general-factory":      {},
	"general-plant":        {},
	"general-warehouse":    {},
	"general-hub":          {},
	"general-depot":        {},
}

// FacilitySlugDistinctiveTokenCount estimates how many non-generic tokens a facility slug carries.
// This is used to detect low-information facility paths that tend to create graph collisions.
func FacilitySlugDistinctiveTokenCount(slug string) int {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return 0
	}
	tokens := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '/' })
	distinctive := 0
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" || len(token) < 2 {
			continue
		}
		if _, ok := facilityGenericSlugTokens[token]; ok {
			continue
		}
		distinctive++
	}
	return distinctive
}

// FacilitySlugLowInformation returns true when a facility slug is too generic to be trusted
// as a stable exact-identity key, especially under /fac/na.
func FacilitySlugLowInformation(slug string) bool {
	return FacilitySlugDistinctiveTokenCount(slug) < 2
}

// FacilitySlugForbiddenPlaceholder returns true for synthetic placeholder slugs that should not
// survive as first-class facilities in the graph. When concrete geography/facility identity is
// unknown, the system should keep org->product links instead of creating fake general facilities.
func FacilitySlugForbiddenPlaceholder(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	_, ok := facilityForbiddenPlaceholderSlugs[slug]
	return ok
}

// FacilitySlugHasSyntheticTail returns true when a slug embeds one of the forbidden synthetic
// placeholder tails, not only when it is exactly equal to it.
func FacilitySlugHasSyntheticTail(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	if FacilitySlugForbiddenPlaceholder(slug) {
		return true
	}
	for tail := range facilityForbiddenPlaceholderSlugs {
		if strings.Contains(slug, "-"+tail) || strings.HasSuffix(slug, tail) {
			return true
		}
	}
	return false
}

// FacilitySlugCriticalDedupAnchor returns true when a facility slug denotes critical operational
// infrastructure or highly specific manufacturing anchors where global exact-name reuse is safer
// than creating another /fac/na placeholder.
func FacilitySlugCriticalDedupAnchor(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	kind := taxonomy.FacilityKindFromSlug(slug)
	switch kind {
	case taxonomy.FacilityKindSeaport,
		taxonomy.FacilityKindAirport,
		taxonomy.FacilityKindPipeline,
		taxonomy.FacilityKindTerminal,
		taxonomy.FacilityKindRefinery,
		taxonomy.FacilityKindCable,
		taxonomy.FacilityKindMine,
		taxonomy.FacilityKindPowerPlant,
		taxonomy.FacilityKindSubstation:
		return true
	case taxonomy.FacilityKindFactory:
		return strings.Contains(slug, "fab") || strings.Contains(slug, "gigafactory") || strings.Contains(slug, "foundry")
	}
	return strings.Contains(slug, "strait") ||
		strings.Contains(slug, "canal") ||
		strings.Contains(slug, "tunnel") ||
		strings.Contains(slug, "pass") ||
		strings.Contains(slug, "pipe") ||
		strings.Contains(slug, "line") ||
		strings.Contains(slug, "chokepoint")
}
