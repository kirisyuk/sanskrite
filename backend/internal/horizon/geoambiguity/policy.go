package geoambiguity

import "strings"

var ambiguousCountryTerms = map[string]struct{}{
	"georgia": {},
	"jordan":  {},
	"chad":    {},
}

var genericGeoHints = []string{
	" country", " government", " military", " air force", " air defense", " air defence",
	" navy", " border", " capital", " ministry", " forces", " over ", " in ", " near ",
	" from ", " to ", " into ", " across ", " around ", " region", " republic", " kingdom",
}

var ambiguousCountryRules = map[string]struct {
	allow []string
	block []string
}{
	"georgia": {
		allow: []string{
			"republic of georgia", "georgian", "tbilisi", "batumi", "caucasus", "south ossetia", "-abkhazia",
		},
		block: []string{
			"state of georgia", "georgia state", "georgia tech", "atlanta", "savannah", "augusta", "macon", "columbus georgia",
			"port of savannah", "u.s. state of georgia",
		},
	},
	"jordan": {
		allow: []string{
			"hashemite kingdom of jordan", "kingdom of jordan", "jordanian", "amman", "aqaba", "west bank", "over jordan",
			"jordan air defense", "jordan air defence", "jordan military", "jordan government",
		},
		block: []string{
			"michael jordan", "air jordan", "dr jordan", "mr jordan", "ms jordan", "professor jordan", "coach jordan",
		},
	},
	"chad": {
		allow: []string{
			"republic of chad", "chadian", "ndjamena", "n'djamena", "lake chad", "in chad", "chad government",
		},
		block: []string{
			"chad smith", "chad michael", "hey chad", "dear chad",
		},
	},
}

func NormalizeTerm(v string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(v))), " ")
}

func IsAmbiguousCountryName(term string) bool {
	_, ok := ambiguousCountryTerms[NormalizeTerm(term)]
	return ok
}

// CountryAliasAllowedInText returns true when an ambiguous country alias has enough
// context to be treated as a country mention instead of a person/state/brand collision.
func CountryAliasAllowedInText(alias string, text string) bool {
	alias = NormalizeTerm(alias)
	text = NormalizeTerm(text)
	if alias == "" || text == "" {
		return false
	}
	if !IsAmbiguousCountryName(alias) {
		return true
	}
	rule, ok := ambiguousCountryRules[alias]
	if ok {
		for _, blocker := range rule.block {
			if strings.Contains(text, NormalizeTerm(blocker)) {
				return false
			}
		}
		for _, allow := range rule.allow {
			if strings.Contains(text, NormalizeTerm(allow)) {
				return true
			}
		}
	}
	for _, hint := range genericGeoHints {
		if strings.Contains(text, NormalizeTerm(alias+hint)) || strings.Contains(text, NormalizeTerm(hint+alias)) {
			return true
		}
	}
	return false
}
