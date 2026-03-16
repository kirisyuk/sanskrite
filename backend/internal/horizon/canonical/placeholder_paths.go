package canonical

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type PlaceholderPathKind string

const (
	PlaceholderPathNone                   PlaceholderPathKind = ""
	PlaceholderPathFacilitySynthetic      PlaceholderPathKind = "facility_synthetic_placeholder"
	PlaceholderPathFacilityUnknownGeo     PlaceholderPathKind = "facility_unknown_geo"
	PlaceholderPathOrgSynthetic           PlaceholderPathKind = "org_synthetic_placeholder"
	PlaceholderPathOrgUnknownJurisdiction PlaceholderPathKind = "org_unknown_jurisdiction"
	PlaceholderPathGovSynthetic           PlaceholderPathKind = "gov_synthetic_placeholder"
	PlaceholderPathGovUnknownJurisdiction PlaceholderPathKind = "gov_unknown_jurisdiction"
	PlaceholderPathLocPlaceholderAdmin1   PlaceholderPathKind = "loc_placeholder_admin1"
	PlaceholderPathLocPlaceholderAdmin12  PlaceholderPathKind = "loc_placeholder_admin1_admin2"
	PlaceholderPathLocPlaceholderAdmin2   PlaceholderPathKind = "loc_placeholder_admin2"
)

func ClassifyPlaceholderCanonicalPath(path string) PlaceholderPathKind {
	cp := strings.TrimSpace(strings.ToLower(path))
	if cp == "" {
		return PlaceholderPathNone
	}
	switch {
	case strings.HasPrefix(cp, "/fac/na/"):
		if FacilitySlugHasSyntheticTail(pathSlug(cp)) {
			return PlaceholderPathFacilitySynthetic
		}
		return PlaceholderPathFacilityUnknownGeo
	case strings.HasPrefix(cp, "/fac/") && FacilitySlugHasSyntheticTail(pathSlug(cp)):
		return PlaceholderPathFacilitySynthetic
	case strings.HasPrefix(cp, "/org/na/"):
		if OrgSlugForbiddenPlaceholder(pathSlug(cp)) {
			return PlaceholderPathOrgSynthetic
		}
		return PlaceholderPathOrgUnknownJurisdiction
	case strings.HasPrefix(cp, "/org/") && OrgSlugForbiddenPlaceholder(pathSlug(cp)):
		return PlaceholderPathOrgSynthetic
	case strings.HasPrefix(cp, "/gov/na/"):
		if GovSlugForbiddenPlaceholder(pathSlug(cp)) {
			return PlaceholderPathGovSynthetic
		}
		return PlaceholderPathGovUnknownJurisdiction
	case strings.HasPrefix(cp, "/gov/") && GovSlugForbiddenPlaceholder(pathSlug(cp)):
		return PlaceholderPathGovSynthetic
	case strings.HasPrefix(cp, "/loc/"):
		cc, level1, level2, ok := taxonomy.ParseLocCanonicalPath(cp)
		if !ok || cc == "" || cc == "na" {
			return PlaceholderPathNone
		}
		switch {
		case level1 == "" && strings.Contains(cp, "/na/na"):
			return PlaceholderPathLocPlaceholderAdmin12
		case level1 == "" && strings.HasSuffix(cp, "/na"):
			return PlaceholderPathLocPlaceholderAdmin1
		case level1 != "" && level2 == "" && strings.HasSuffix(cp, "/na"):
			return PlaceholderPathLocPlaceholderAdmin2
		}
	}
	return PlaceholderPathNone
}

func pathSlug(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func IsPlaceholderCanonicalPath(path string) bool {
	return ClassifyPlaceholderCanonicalPath(path) != PlaceholderPathNone
}

func NormalizePlaceholderCanonicalPath(path string) (string, bool) {
	cp := strings.TrimSpace(path)
	if cp == "" {
		return "", false
	}
	switch ClassifyPlaceholderCanonicalPath(cp) {
	case PlaceholderPathLocPlaceholderAdmin1, PlaceholderPathLocPlaceholderAdmin12, PlaceholderPathLocPlaceholderAdmin2:
		if norm := NormalizeGeoPathToStrict(cp); norm != "" && norm != cp {
			return norm, true
		}
	}
	return "", false
}
