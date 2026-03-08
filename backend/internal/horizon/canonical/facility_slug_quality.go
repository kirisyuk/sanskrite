package canonical

import "strings"

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
