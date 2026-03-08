package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var govKeywordTokens = []string{
	"government",
	"ministry",
	"department",
	"agency",
	"authority",
	"regulator",
	"commission",
	"council",
	"committee",
	"secretariat",
	"bureau",
	"office",
	"treasury",
	"central bank",
	"state council",
}

var govAcronyms = map[string]struct{}{
	"moe":  {}, // ministry of education
	"mof":  {}, // ministry of finance
	"moi":  {}, // ministry of interior
	"mod":  {}, // ministry of defense
	"moa":  {}, // ministry of agriculture
	"mot":  {}, // ministry of transport
	"med":  {}, // ministry of energy/education (ambiguous, still gov-leaning)
	"mofa": {}, // ministry of foreign affairs
	"moh":  {}, // ministry of health
	"doe":  {}, // department of energy/education
	"doj":  {}, // department of justice
	"sec":  {}, // securities and exchange commission
}

func looksLikeGovLabel(label string) bool {
	l := normalizeGovLabel(label)
	if l == "" {
		return false
	}
	for _, token := range govKeywordTokens {
		if strings.Contains(l, token) {
			return true
		}
	}
	parts := strings.Fields(l)
	for _, part := range parts {
		if _, ok := govAcronyms[part]; ok {
			return true
		}
	}
	return false
}

func normalizeGovLabel(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return ""
	}
	repl := strings.NewReplacer("-", " ", "_", " ", ".", " ", ",", " ", "(", " ", ")", " ")
	return strings.TrimSpace(repl.Replace(label))
}

func inferISO2FromLabel(label string, fragment *models.HorizonFragment) string {
	if fragment != nil {
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
	}

	l := normalizeGovLabel(label)
	if l == "" {
		return ""
	}

	bestMatch := ""
	bestLen := 0
	for name := range KnownCountries {
		if strings.Contains(l, name) && len(name) > bestLen {
			bestMatch = name
			bestLen = len(name)
		}
	}
	for alias := range CountryAliases {
		if strings.Contains(l, alias) && len(alias) > bestLen {
			bestMatch = alias
			bestLen = len(alias)
		}
	}
	if bestMatch == "" {
		return ""
	}

	if cp := GetCountryCanonicalPath(bestMatch); strings.HasPrefix(cp, "/loc/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 && parts[0] == "loc" && len(parts[1]) == 2 {
			return strings.ToLower(parts[1])
		}
	}
	return ""
}
