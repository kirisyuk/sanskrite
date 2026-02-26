package taxonomy

import (
	"regexp"
	"sort"
	"strings"
)

var sourceFormatAllowlist = []string{
	"wire",
	"mainstream",
	"gov",
	"regulator",
	"intl-org",
	"think-tank",
	"research",
	"osint",
	"market-data",
	"industry-media",
	"tech-media",
	"blog",
	"newsletter",
	"podcast",
	"aggregator",
	"forum",
	"social",
	"official-release",
}

var sourceFormatSet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(sourceFormatAllowlist))
	for _, v := range sourceFormatAllowlist {
		out[v] = struct{}{}
	}
	return out
}()

var sourceFormatAliases = map[string]string{
	"newswire":                   "wire",
	"wire-service":               "wire",
	"major-media":                "mainstream",
	"government":                 "gov",
	"official-gov":               "gov",
	"state":                      "gov",
	"regulatory":                 "regulator",
	"regulatory-body":            "regulator",
	"international-org":          "intl-org",
	"international-organization": "intl-org",
	"io":                         "intl-org",
	"thinktank":                  "think-tank",
	"think-tanks":                "think-tank",
	"research-institute":         "research",
	"market":                     "market-data",
	"financial-data":             "market-data",
	"industry":                   "industry-media",
	"tech":                       "tech-media",
	"open-source-intel":          "osint",
	"osint-media":                "osint",
	"substack":                   "newsletter",
	"news-letter":                "newsletter",
	"audio":                      "podcast",
	"rss-aggregator":             "aggregator",
	"community":                  "forum",
	"social-media":               "social",
	"official":                   "official-release",
	"press-release":              "official-release",
}

var sourceGeoFacetSet = func() map[string]struct{} {
	// Start from regional tokens already accepted in canonical path rules.
	out := make(map[string]struct{}, len(regionalGeoTokens)+32)
	for _, t := range RegionalGeoTokenList() {
		out[t] = struct{}{}
	}
	// Add global/international scopes and common multilateral blocs/orgs.
	for _, t := range []string{
		"global",
		"int",
		"un",
		"unsc",
		"nato",
		"eu",
		"asean",
		"brics",
		"g7",
		"g20",
		"oecd",
		"opec",
		"apec",
		"wto",
		"imf",
		"world-bank",
		"who",
		"iaea",
		"unhcr",
		"osce",
		"african-union",
		"arab-league",
		"mercosur",
		"caricom",
		"ecowas",
		"sadc",
		"eac",
		"comesa",
		"igad",
		"eaeu",
		"csto",
		"sco",
		"rcep",
		"cptpp",
		"bimstec",
	} {
		out[t] = struct{}{}
	}
	return out
}()

var sourceGeoFacetAliases = map[string]string{
	"world":                                  "global",
	"worldwide":                              "global",
	"international":                          "int",
	"intl":                                   "int",
	"european-union":                         "eu",
	"association-of-southeast-asian-nations": "asean",
	"north-atlantic-treaty-organization":     "nato",
	"united-nations":                         "un",
	"un-security-council":                    "unsc",
	"world-health-organization":              "who",
	"international-monetary-fund":            "imf",
	"world-bank-group":                       "world-bank",
	"african-union":                          "african-union",
	"league-of-arab-states":                  "arab-league",
}

var reSourceGeoISO2 = regexp.MustCompile(`^[a-z]{2}$`)

// AllowedSourceFormatFacets returns the closed allowlist for sourceFormatFacets.
func AllowedSourceFormatFacets() []string {
	out := make([]string, len(sourceFormatAllowlist))
	copy(out, sourceFormatAllowlist)
	return out
}

// AllowedSourceGeoFacetTokens returns accepted non-ISO2 geo facet tokens.
func AllowedSourceGeoFacetTokens() []string {
	out := make([]string, 0, len(sourceGeoFacetSet))
	for t := range sourceGeoFacetSet {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// NormalizeSourceFormatFacet canonicalizes source format facet.
func NormalizeSourceFormatFacet(in string) (string, bool) {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return "", false
	}
	in = strings.ReplaceAll(in, "_", "-")
	in = strings.ReplaceAll(in, " ", "-")
	in = Slugify(in)
	if in == "" {
		return "", false
	}
	if mapped, ok := sourceFormatAliases[in]; ok {
		in = mapped
	}
	if _, ok := sourceFormatSet[in]; !ok {
		return "", false
	}
	return in, true
}

// NormalizeSourceGeoFacet canonicalizes geo facet:
// - ISO3166-1 alpha2 (us, ua, de, ...)
// - global scopes (global, int)
// - common regional/bloc/org tokens (eu, asean, brics, nato, ...).
func NormalizeSourceGeoFacet(in string) (string, bool) {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return "", false
	}
	in = strings.ReplaceAll(in, "\\", "/")
	in = strings.Trim(in, "/")
	if in == "" {
		return "", false
	}

	// Accept shorthand forms like loc/us, geo/eu, source/us/...
	if strings.HasPrefix(in, "loc/") {
		in = strings.TrimPrefix(in, "loc/")
	} else if strings.HasPrefix(in, "geo/") {
		in = strings.TrimPrefix(in, "geo/")
	} else if strings.HasPrefix(in, "source/") {
		in = strings.TrimPrefix(in, "source/")
	}
	if idx := strings.Index(in, "/"); idx >= 0 {
		in = in[:idx]
	}

	in = Slugify(in)
	if in == "" {
		return "", false
	}
	if mapped, ok := sourceGeoFacetAliases[in]; ok {
		in = mapped
	}
	if reSourceGeoISO2.MatchString(in) {
		return in, true
	}
	if _, ok := sourceGeoFacetSet[in]; !ok {
		return "", false
	}
	return in, true
}

// NormalizeSourceFormatFacets normalizes and deduplicates format facets.
func NormalizeSourceFormatFacets(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v, ok := NormalizeSourceFormatFacet(raw)
		if !ok {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// NormalizeSourceGeoFacets normalizes and deduplicates geo facets.
func NormalizeSourceGeoFacets(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v, ok := NormalizeSourceGeoFacet(raw)
		if !ok {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
