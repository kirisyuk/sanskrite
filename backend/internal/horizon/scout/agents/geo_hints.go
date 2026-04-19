package agents

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func applyInferredHomeLocationHint(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	if fragment.Class != FragmentTypeFacility && fragment.Class != FragmentTypeOrganization {
		return
	}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &models.Location{}
	}
	if strings.TrimSpace(fragment.HomeLocation.Name) == "" {
		hints := inferPlaceHintCandidates(fragment)
		if len(hints) > 0 {
			fragment.HomeLocation.Name = strings.TrimSpace(hints[0])
		}
	}
	if (fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) == "") &&
		strings.TrimSpace(fragment.CanonicalPath) != "" {
		if iso2 := inferHomeLocationISO2FromPath(fragment.CanonicalPath); iso2 != "" {
			v := iso2
			fragment.HomeLocation.ISO3166_1 = &v
		}
	}
}

func inferHomeLocationISO2FromPath(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	switch parts[0] {
	case "fac", "org", "gov":
		if len(parts[1]) == 2 && parts[1] != "na" && taxonomy.IsISO2CountryCode(parts[1]) {
			return strings.ToUpper(parts[1])
		}
	}
	return ""
}

func inferPlaceHintCandidates(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(raw string) {
		v := strings.TrimSpace(raw)
		if v == "" {
			return
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	if fragment.HomeLocation != nil {
		add(fragment.HomeLocation.Name)
	}
	for _, raw := range geoHintSourceCandidates(fragment) {
		for _, hint := range inferPlaceHintsFromText(raw) {
			add(hint)
		}
	}
	return out
}

func geoHintSourceCandidates(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	out := make([]string, 0, 4)
	if fragment.Labels != nil {
		if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
			out = append(out, v)
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"legalName", "shortName"} {
			if v := strings.TrimSpace(fragment.Identifiers[key]); v != "" {
				out = append(out, v)
			}
		}
	}
	if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) > 0 {
			if slug := strings.TrimSpace(parts[len(parts)-1]); slug != "" {
				out = append(out, strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " "))
			}
		}
	}
	return out
}

func inferPlaceHintsFromText(raw string) []string {
	tokens := strings.Fields(normalizeGeoHintText(raw))
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(titleGeoHint(value))
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(tokens) >= 2 && taxonomy.IsISO2CountryCode(tokens[len(tokens)-2]) && len(tokens[len(tokens)-1]) >= 4 {
		add(tokens[len(tokens)-1])
	}
	trimmed := trimTrailingGeoNoise(tokens)
	if len(trimmed) == 0 {
		return out
	}
	if len(trimmed) == 2 && len(trimmed[0]) >= 3 && len(trimmed[1]) >= 3 &&
		!isGenericGeoNoiseToken(trimmed[0]) && !isGenericGeoNoiseToken(trimmed[1]) {
		add(strings.Join(trimmed, " "))
	}
	last := trimmed[len(trimmed)-1]
	if len(trimmed) >= 2 && len(last) >= 4 && !isGenericGeoNoiseToken(last) {
		add(last)
	}
	if len(trimmed) >= 2 && isDirectionalGeoToken(trimmed[len(trimmed)-2]) && len(trimmed[len(trimmed)-1]) >= 3 {
		add(strings.Join(trimmed[len(trimmed)-2:], " "))
	}
	return out
}

func normalizeGeoHintText(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	repl := strings.NewReplacer(",", " ", ".", " ", "-", " ", "_", " ", "(", " ", ")", " ", "/", " ", "'", " ", "\"", " ", "&", " ")
	s = repl.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func trimTrailingGeoNoise(tokens []string) []string {
	trimmed := append([]string(nil), tokens...)
	for len(trimmed) > 0 && isGenericGeoNoiseToken(trimmed[len(trimmed)-1]) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}

func isDirectionalGeoToken(token string) bool {
	switch strings.TrimSpace(strings.ToLower(token)) {
	case "north", "south", "east", "west", "upper", "lower", "new":
		return true
	default:
		return false
	}
}

func isGenericGeoNoiseToken(token string) bool {
	switch strings.TrimSpace(strings.ToLower(token)) {
	case "plant", "factory", "facility", "facilities", "terminal", "terminals", "site", "station", "stations",
		"center", "centre", "centers", "centres", "network", "networks", "hub", "hubs", "bank", "company", "co",
		"corp", "corporation", "group", "holding", "holdings", "limited", "ltd", "llc", "plc", "pjsc", "sa", "as",
		"spa", "ag", "gmbh", "bv", "nv", "inc", "industries", "industry", "electronic", "electronics", "circuit",
		"fulfillment", "distribution", "logistics", "general", "production", "processing":
		return true
	default:
		return false
	}
}

func titleGeoHint(raw string) string {
	parts := strings.Fields(strings.TrimSpace(raw))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
