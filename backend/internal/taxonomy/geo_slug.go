package taxonomy

import (
	"sort"
	"strings"
)

// ExtractGeoSlugFromCanonicalPath returns the primary geo token encoded in a canonical path.
// It is intended for top-level indexing/filtering and is therefore conservative:
// only namespaces with an explicit geo/jurisdiction slot are mapped.
func ExtractGeoSlugFromCanonicalPath(path string) string {
	parts, err := ParseCanonicalPathParts(path)
	if err != nil || parts == nil {
		return ""
	}
	switch strings.TrimSpace(parts.Namespace) {
	case "org":
		if parts.Org != nil {
			return normalizeGeoSlugCandidate(parts.Org.Geo)
		}
	case "gov":
		if parts.Gov != nil {
			return normalizeGeoSlugCandidate(parts.Gov.Geo)
		}
	case "community":
		if parts.Community != nil {
			return normalizeGeoSlugCandidate(parts.Community.Geo)
		}
	case "loc":
		if parts.Loc != nil {
			return normalizeGeoSlugCandidate(parts.Loc.Country)
		}
	case "segment":
		if parts.Segment != nil {
			return normalizeGeoSlugCandidate(parts.Segment.Geo)
		}
	case "market":
		if parts.Market != nil {
			return normalizeGeoSlugCandidate(parts.Market.Geo)
		}
	case "fac":
		if parts.Fac != nil {
			return normalizeGeoSlugCandidate(parts.Fac.Geo)
		}
	case "program":
		if parts.Program != nil {
			return normalizeGeoSlugCandidate(parts.Program.Jurisdiction)
		}
	case "zone":
		if parts.RawPathParts != nil && len(parts.RawPathParts.Segments) >= 2 {
			return normalizeGeoSlugCandidate(parts.RawPathParts.Segments[1])
		}
	case "domain":
		return extractDomainGeoSlug(parts.RawPathParts)
	}
	return ""
}

func extractDomainGeoSlug(raw *RawPathParts) string {
	if raw == nil || len(raw.Segments) < 5 {
		return ""
	}
	segs := raw.Segments
	start := 3
	if len(segs) >= 4 && strings.EqualFold(strings.TrimSpace(segs[2]), "other") {
		start = 4
	}
	if start >= len(segs) {
		return ""
	}

	geos := make([]string, 0, 2)
	for i := start; i+1 < len(segs); i += 2 {
		key := strings.ToLower(strings.TrimSpace(segs[i]))
		value := normalizeGeoSlugCandidate(segs[i+1])
		if key == "geo" && value != "" {
			geos = append(geos, value)
		}
	}
	if len(geos) == 0 {
		return ""
	}
	if len(geos) == 1 {
		return geos[0]
	}

	allISO2 := true
	seen := make(map[string]struct{}, len(geos))
	uniq := make([]string, 0, len(geos))
	for _, geo := range geos {
		if _, ok := seen[geo]; ok {
			continue
		}
		seen[geo] = struct{}{}
		uniq = append(uniq, geo)
		if !IsISO2CountryCode(geo) {
			allISO2 = false
		}
	}
	if !allISO2 {
		return uniq[0]
	}
	sort.Strings(uniq)
	return strings.Join(uniq, "-")
}

func normalizeGeoSlugCandidate(in string) string {
	geo := strings.ToLower(strings.TrimSpace(in))
	if geo == "" {
		return ""
	}
	switch {
	case geo == "global", geo == "int", geo == "na":
		return geo
	case IsISO2CountryCode(geo), IsRegionalGeoToken(geo), IsCompositeGeoToken(geo), reGeoSlug.MatchString(geo):
		return geo
	default:
		return ""
	}
}
