package scout

import (
	"regexp"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var genericGovernmentLabelRE = regexp.MustCompile(`(?i)^government of [a-z][a-z .'-]+$`)

var orgRegionalGeoHints = map[string]string{
	"asean":                    "asean",
	"african union":            "african-union",
	"european union":           "eu",
	"european commission":      "eu",
	"gulf cooperation council": "gcc",
	"gcc":                      "gcc",
	"united nations":           "int",
	"opec":                     "opec",
	"oecd":                     "oecd",
	"g20":                      "g20",
	"g7":                       "g7",
	"arab league":              "arab-league",
}

var orgJurisdictionHints = map[string]string{
	"abu dhabi": "ae",
	"dubai":     "ae",
	"emirati":   "ae",
	"uae":       "ae",
	"saudi":     "sa",
	"riyadh":    "sa",
	"iran":      "ir",
	"tehran":    "ir",
	"qatar":     "qa",
	"doha":      "qa",
	"mexico":    "mx",
	"egypt":     "eg",
	"turkey":    "tr",
	"japan":     "jp",
}

func inferOrgGeoTokenFromHints(label string, fragment *models.HorizonFragment) string {
	if iso2 := inferOrgISO2(fragment); iso2 != "" {
		return iso2
	}
	label = normalizeGovLabel(label)
	if label == "" {
		return ""
	}
	for needle, geo := range orgRegionalGeoHints {
		if strings.Contains(label, needle) {
			return geo
		}
	}
	if iso2 := inferISO2FromLabel(label, fragment); iso2 != "" {
		return iso2
	}
	for needle, geo := range orgJurisdictionHints {
		if strings.Contains(label, needle) {
			return geo
		}
	}
	return ""
}

func inferOrgCanonicalPathFromHints(fragment *models.HorizonFragment) string {
	if fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return ""
	}
	label := orgCanonicalizationLabel(fragment, strings.TrimSpace(fragment.CanonicalPath))
	if label == "" {
		return ""
	}
	geo := inferOrgGeoTokenFromHints(label, fragment)
	if geo == "" {
		geo = "na"
	}
	if looksLikeGovLabel(label) {
		if genericGovernmentLabelRE.MatchString(strings.TrimSpace(label)) {
			return taxonomy.GovCanonicalPath(geo, "government")
		}
		return taxonomy.GovCanonicalPath(geo, label)
	}
	return taxonomy.OrgCanonicalPath(geo, label)
}

// InferOrgCanonicalPathFromHints is an exported wrapper for deterministic org/gov placeholder
// repair and backfill tools. It shares the same logic as create-time repair.
func InferOrgCanonicalPathFromHints(fragment *models.HorizonFragment) string {
	return inferOrgCanonicalPathFromHints(fragment)
}
