package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// coerceCanonicalPathForType best-effort fixes obvious namespace/type mismatches.
// It is intentionally conservative and currently targets the most harmful cases:
// ORGANIZATION<->/product and PRODUCT<->/org.
func coerceCanonicalPathForType(path string, fragType FragmentType, fallbackName string) (string, bool) {
	cp := strings.TrimSpace(path)
	if cp == "" || fragType == "" {
		return cp, false
	}

	switch fragType {
	case FragmentTypeOrganization:
		// Organizations may live under /org/... or /gov/... .
		if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") {
			return cp, false
		}
		slug := lastPathSlug(cp)
		if slug == "" {
			slug = taxonomy.Slugify(fallbackName)
		}
		if slug == "" {
			return cp, false
		}
		fixed := taxonomy.JoinPath("org", "na", slug)
		return fixed, fixed != cp

	case FragmentTypeProduct:
		if strings.HasPrefix(cp, "/product/") {
			return cp, false
		}
		slug := lastPathSlug(cp)
		if slug == "" {
			slug = taxonomy.Slugify(fallbackName)
		}
		if slug == "" {
			return cp, false
		}
		fixed := taxonomy.JoinPath("product", "finished", "mechanical", "assembly", slug)
		return fixed, fixed != cp
	}

	return cp, false
}

func lastPathSlug(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return taxonomy.Slugify(parts[len(parts)-1])
}
