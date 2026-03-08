package canonical

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// InferFacilityISO2 returns the best-effort facility jurisdiction from homeLocation
// or deterministic identifier hints.
func InferFacilityISO2(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := normalizeISO2(*fragment.HomeLocation.ISO3166_1); v != "" {
			return v
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := normalizeISO2(fragment.Identifiers[key]); v != "" {
				return v
			}
		}
	}
	return ""
}

// InferFacilityCanonicalPathFromGeo upgrades /fac/na/... to /fac/{iso2}/...
// when the facility already has deterministic country evidence.
func InferFacilityCanonicalPathFromGeo(fragment *models.HorizonFragment) (string, bool) {
	if fragment == nil || fragment.Type != models.FragmentTypeFacility {
		return "", false
	}
	oldPath := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(oldPath, "/fac/na/") {
		return "", false
	}
	iso2 := InferFacilityISO2(fragment)
	if iso2 == "" {
		return "", false
	}
	label := firstNonEmpty(
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
	)
	if label == "" {
		parts := strings.Split(strings.Trim(oldPath, "/"), "/")
		if len(parts) == 3 {
			label = strings.ReplaceAll(strings.TrimSpace(parts[2]), "-", " ")
		}
	}
	newPath := taxonomy.FacilityCanonicalPath(iso2, label)
	if newPath == "" || newPath == oldPath {
		return "", false
	}
	return newPath, true
}

func normalizeISO2(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) != 2 {
		return ""
	}
	if !taxonomy.IsISO2CountryCode(v) {
		return ""
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
