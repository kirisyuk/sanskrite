package scout

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// FacilityGeoRevalidationResult captures deterministic geo corrections applied to a facility.
type FacilityGeoRevalidationResult struct {
	Applied             bool
	ChangedISO2         bool
	ChangedPath         bool
	ChangedLocationName bool
	ChangedCoordinates  bool
	PreviousISO2        string
	ResolvedISO2        string
	Query               string
	FormattedAddress    string
	Provider            string
	Reason              string
}

// RevalidateFacilityGeo uses geocoding to verify facility geography before persistence.
// It is intentionally conservative:
// - it prefers queries that contain an explicit place hint,
// - it only overrides conflicting country data when geocoding confirms the place,
// - it upgrades /fac/na/... only after the country is revalidated.
func RevalidateFacilityGeo(
	ctx context.Context,
	fragment *models.HorizonFragment,
	geocodingClient coreontology.GeocodingClientInterface,
) (FacilityGeoRevalidationResult, error) {
	var result FacilityGeoRevalidationResult
	if fragment == nil || geocodingClient == nil || fragment.Type != models.FragmentTypeFacility {
		return result, nil
	}

	queries := facilityGeoValidationQueries(fragment)
	if len(queries) == 0 {
		return result, nil
	}

	currentISO2 := currentFacilityISO2(fragment)
	pathISO2 := facilityPathISO2(fragment.CanonicalPath)
	result.PreviousISO2 = firstNonEmptyUpper(currentISO2, pathISO2)

	var lastErr error
	for _, query := range queries {
		geo, err := geocodingClient.Geocode(ctx, query)
		if err != nil {
			lastErr = err
			continue
		}
		if geo == nil {
			continue
		}
		resolvedISO2 := inferCountryISO2FromFormattedAddress(geo.FormattedAddress)
		if resolvedISO2 == "" {
			continue
		}
		if !shouldAcceptFacilityGeoResult(fragment, query, geo.FormattedAddress, currentISO2, pathISO2, resolvedISO2) {
			continue
		}

		if fragment.HomeLocation == nil {
			fragment.HomeLocation = &models.Location{}
		}
		if fragment.HomeLocation.ISO3166_1 == nil || !strings.EqualFold(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1), resolvedISO2) {
			v := resolvedISO2
			fragment.HomeLocation.ISO3166_1 = &v
			result.ChangedISO2 = true
		}
		if place := facilityPlaceNameHint(fragment); place != "" && shouldUpgradePlaceName(fragment.HomeLocation.Name, place) {
			fragment.HomeLocation.Name = place
			result.ChangedLocationName = true
		}
		if models.IsRealGeoCoordinate(geo.Lat, geo.Lon) {
			result.ChangedCoordinates = henrich.ApplyResolvedCoordinates(fragment, geo.Lat, geo.Lon)
		}
		if fragment.Identifiers == nil {
			fragment.Identifiers = map[string]string{}
		}
		henrich.RecordGeoCoordinatesAttribution(fragment, henrich.GeoCoordinatesAttribution{
			Method:           "geocoding_revalidate",
			Provider:         strings.TrimSpace(geo.Provider),
			Query:            query,
			FormattedAddress: strings.TrimSpace(geo.FormattedAddress),
			PlaceID:          strings.TrimSpace(geo.PlaceID),
			CountryAlpha2:    resolvedISO2,
		})
		fragment.Identifiers["geoVerifiedCountryAlpha2"] = strings.ToUpper(resolvedISO2)
		if strings.TrimSpace(geo.PlaceID) != "" {
			fragment.Identifiers["geoVerifiedPlaceId"] = strings.TrimSpace(geo.PlaceID)
		}
		fragment.Identifiers["geoVerifiedMethod"] = "geocoding"
		if provider := strings.TrimSpace(geo.Provider); provider != "" {
			fragment.Identifiers["geoVerifiedProvider"] = provider
		}
		if formatted := strings.TrimSpace(geo.FormattedAddress); formatted != "" {
			fragment.Identifiers["geoVerifiedFormattedAddress"] = formatted
		}
		fragment.Identifiers["geoVerifiedQuery"] = query

		if upgradedPath, ok := canonical.InferFacilityCanonicalPathFromGeo(fragment); ok {
			fragment.CanonicalPath = upgradedPath
			if parts, err := taxonomy.ParseCanonicalPathParts(upgradedPath); err == nil && parts != nil {
				fragment.CanonicalParts = parts
			}
			result.ChangedPath = true
		}

		result.Applied = result.ChangedISO2 || result.ChangedPath || result.ChangedCoordinates || result.ChangedLocationName
		result.ResolvedISO2 = strings.ToUpper(resolvedISO2)
		result.Query = query
		result.FormattedAddress = strings.TrimSpace(geo.FormattedAddress)
		result.Provider = strings.TrimSpace(geo.Provider)
		switch {
		case result.ChangedISO2 && result.ChangedPath && result.ChangedCoordinates:
			result.Reason = "facility country, coordinates, and canonical path revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedLocationName:
			result.Reason = "facility coordinates and place name revalidated via geocoding"
		case result.ChangedLocationName:
			result.Reason = "facility place name revalidated via geocoding"
		case result.ChangedISO2 && result.ChangedPath:
			result.Reason = "facility country and canonical path revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedISO2:
			result.Reason = "facility country and coordinates revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedPath:
			result.Reason = "facility coordinates and canonical path revalidated via geocoding"
		case result.ChangedISO2:
			result.Reason = "facility country revalidated via geocoding"
		case result.ChangedCoordinates:
			result.Reason = "facility coordinates revalidated via geocoding"
		case result.ChangedPath:
			result.Reason = "facility canonical path upgraded after geocoding verification"
		default:
			result.Reason = "facility geography confirmed via geocoding"
		}
		return result, nil
	}
	return result, lastErr
}

func facilityGeoValidationQueries(fragment *models.HorizonFragment) []string {
	if fragment == nil || fragment.Type != models.FragmentTypeFacility {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 12)
	add := func(raw string) {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			return
		}
		key := strings.ToLower(addr)
		if _, ok := seen[key]; ok {
			return
		}
		if !addressvalidate.IsPlausibleAddress(addr) {
			return
		}
		seen[key] = struct{}{}
		out = append(out, addr)
	}

	for _, candidate := range henrich.BuildFacilityAddressCandidates(fragment, nil) {
		add(candidate)
	}

	place := ""
	if fragment.HomeLocation != nil {
		place = strings.TrimSpace(fragment.HomeLocation.Name)
	}
	for _, name := range collectFacilityNameCandidates(fragment) {
		if facilityAllowsPlainNameGeocode(fragment) {
			add(name)
		}
		if place != "" {
			add(name + ", " + place)
		}
	}
	if place != "" {
		add(place)
	}
	for _, candidate := range henrich.BuildLastSegmentAddressCandidates(fragment) {
		add(candidate)
	}
	return out
}

func collectFacilityNameCandidates(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
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
	if fragment.Labels != nil {
		add(fragment.Labels["en"])
	}
	if fragment.Identifiers != nil {
		add(fragment.Identifiers["legalName"])
		add(fragment.Identifiers["shortName"])
	}
	if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
		add(slugToLabel(cp))
	}
	return out
}

func shouldAcceptFacilityGeoResult(
	fragment *models.HorizonFragment,
	query string,
	formattedAddress string,
	currentISO2 string,
	pathISO2 string,
	resolvedISO2 string,
) bool {
	resolvedISO2 = strings.ToLower(strings.TrimSpace(resolvedISO2))
	if resolvedISO2 == "" || !taxonomy.IsISO2CountryCode(resolvedISO2) {
		return false
	}

	currentISO2 = strings.ToLower(strings.TrimSpace(currentISO2))
	pathISO2 = strings.ToLower(strings.TrimSpace(pathISO2))
	query = strings.TrimSpace(query)
	formattedAddress = strings.TrimSpace(formattedAddress)
	place := facilityPlaceHint(fragment)
	queryHasPlace := place != "" && containsNormalizedGeoPhrase(query, place)
	formattedHasPlace := place != "" && containsNormalizedGeoPhrase(formattedAddress, place)
	compositeQuery := strings.Count(query, ",") >= 1

	// If a non-NA canonical path already agrees with the result, accept it as verification.
	if pathISO2 != "" && pathISO2 != "na" && pathISO2 == resolvedISO2 {
		return place != "" && queryHasPlace
	}

	// For /fac/na, matching the current country alone is not evidence: current ISO may itself be wrong.
	if currentISO2 != "" && currentISO2 == resolvedISO2 {
		return place != "" && queryHasPlace && formattedHasPlace
	}

	// Override a conflicting country only when the query/result both support the place hint.
	if place != "" && queryHasPlace && formattedHasPlace {
		return true
	}

	// For /fac/na and empty country, a composite query is enough to accept the result.
	if currentISO2 == "" && (pathISO2 == "" || pathISO2 == "na") && compositeQuery {
		return true
	}

	return false
}

func facilityPlaceHint(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil {
		if place := strings.TrimSpace(fragment.HomeLocation.Name); place != "" {
			return normalizeGeoPhrase(place)
		}
	}
	for _, hint := range henrich.InferPlaceHintCandidates(fragment) {
		if normalized := normalizeGeoPhrase(hint); normalized != "" {
			return normalized
		}
	}
	for _, candidate := range collectFacilityNameCandidates(fragment) {
		if hint := inferPlaceHintFromFacilityLabel(candidate); hint != "" {
			return hint
		}
	}
	return ""
}

func facilityPlaceNameHint(fragment *models.HorizonFragment) string {
	best := ""
	for _, hint := range henrich.InferPlaceHintCandidates(fragment) {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if best == "" || len(strings.Fields(hint)) > len(strings.Fields(best)) || len(hint) > len(best) {
			best = hint
		}
	}
	return best
}

func shouldUpgradePlaceName(current string, candidate string) bool {
	cur := normalizeGeoPhrase(current)
	next := normalizeGeoPhrase(candidate)
	if next == "" || cur == next {
		return false
	}
	if cur == "" {
		return true
	}
	curWords := len(strings.Fields(cur))
	nextWords := len(strings.Fields(next))
	return nextWords > curWords && strings.HasSuffix(next, " "+cur)
}

func inferPlaceHintFromFacilityLabel(label string) string {
	normalized := strings.Fields(normalizeGovLabel(label))
	if len(normalized) < 2 {
		return ""
	}
	if len(normalized) >= 3 && normalized[0] == "port" && normalized[1] == "of" {
		return strings.Join(normalized[2:], " ")
	}
	if len(normalized) >= 3 && normalized[0] == "strait" && normalized[1] == "of" {
		return strings.Join(normalized[2:], " ")
	}
	if len(normalized) >= 3 && normalized[0] == "canal" && normalized[1] == "of" {
		return strings.Join(normalized[2:], " ")
	}
	keywords := map[string]struct{}{
		"airport": {}, "port": {}, "terminal": {}, "fab": {}, "facility": {}, "plant": {},
		"refinery": {}, "field": {}, "mine": {}, "barrage": {}, "dam": {}, "base": {},
		"center": {}, "centre": {}, "park": {}, "hub": {},
	}
	for idx, token := range normalized {
		if _, ok := keywords[token]; !ok {
			continue
		}
		if idx == 0 {
			return ""
		}
		return strings.Join(normalized[:idx], " ")
	}
	if len(normalized) >= 2 {
		last := normalized[len(normalized)-1]
		prev := normalized[len(normalized)-2]
		if len(last) >= 4 && taxonomy.IsISO2CountryCode(strings.ToLower(prev)) {
			return last
		}
	}
	trimmed := trimTrailingFacilityGeoNoise(normalized)
	if len(trimmed) == 2 && len(trimmed[0]) >= 3 && len(trimmed[1]) >= 3 {
		return strings.Join(trimmed, " ")
	}
	if len(trimmed) >= 2 && len(trimmed[len(trimmed)-1]) >= 4 {
		return trimmed[len(trimmed)-1]
	}
	return ""
}

func trimTrailingFacilityGeoNoise(tokens []string) []string {
	trimmed := append([]string(nil), tokens...)
	for len(trimmed) > 0 {
		switch trimmed[len(trimmed)-1] {
		case "plant", "factory", "facility", "facilities", "terminal", "terminals", "site", "station", "stations",
			"center", "centre", "centers", "centres", "network", "networks", "hub", "hubs", "electronic", "electronics", "circuit",
			"fulfillment", "distribution", "logistics", "general", "production", "processing":
			trimmed = trimmed[:len(trimmed)-1]
		default:
			return trimmed
		}
	}
	return trimmed
}

func facilityAllowsPlainNameGeocode(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	slug := ""
	if parts := strings.Split(strings.Trim(cp, "/"), "/"); len(parts) >= 3 {
		slug = strings.TrimSpace(parts[len(parts)-1])
	}
	if canonical.FacilitySlugCriticalDedupAnchor(slug) {
		return true
	}
	kind := taxonomy.InferFacilityKind(
		cp,
		fragment.Labels["en"],
		fragment.Description,
		fragment.EntityPath,
		fragment.SectorPath,
		fragment.Identifiers["facilityType"],
		fragment.Identifiers["supplyChainRole"],
	)
	if kind == taxonomy.FacilityKindSeaport ||
		kind == taxonomy.FacilityKindAirport ||
		kind == taxonomy.FacilityKindPipeline ||
		kind == taxonomy.FacilityKindTerminal ||
		kind == taxonomy.FacilityKindRefinery ||
		kind == taxonomy.FacilityKindCable ||
		kind == taxonomy.FacilityKindMine ||
		kind == taxonomy.FacilityKindPowerPlant ||
		kind == taxonomy.FacilityKindSubstation {
		return true
	}
	return kind == taxonomy.FacilityKindFactory && canonical.FacilitySlugCriticalDedupAnchor(slug)
}

func currentFacilityISO2(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := normalizeISO2(*fragment.HomeLocation.ISO3166_1); v != "" {
			return strings.ToLower(v)
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := normalizeISO2(fragment.Identifiers[key]); v != "" {
				return strings.ToLower(v)
			}
		}
	}
	return ""
}

func facilityPathISO2(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	if parts[1] == "na" || parts[1] == "" {
		return parts[1]
	}
	if taxonomy.IsISO2CountryCode(parts[1]) {
		return parts[1]
	}
	return ""
}

func inferCountryISO2FromFormattedAddress(formattedAddress string) string {
	parts := strings.Split(strings.TrimSpace(formattedAddress), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if iso2 := lookupCountryISO2(parts[i]); iso2 != "" {
			return iso2
		}
	}
	return ""
}

func lookupCountryISO2(raw string) string {
	term := strings.ToLower(strings.TrimSpace(strings.Trim(raw, ". ")))
	if term == "" {
		return ""
	}
	if len(term) == 2 && taxonomy.IsISO2CountryCode(term) {
		return strings.ToUpper(term)
	}
	if info, ok := KnownCountries[term]; ok && strings.TrimSpace(info.ISO2) != "" {
		return strings.ToUpper(strings.TrimSpace(info.ISO2))
	}
	if cp, ok := CountryAliases[term]; ok {
		if info, ok := KnownCountriesByPath[cp]; ok && strings.TrimSpace(info.ISO2) != "" {
			return strings.ToUpper(strings.TrimSpace(info.ISO2))
		}
	}
	switch term {
	case "usa", "u.s.a.", "u.s.", "united states":
		return "US"
	case "uk", "u.k.", "united kingdom":
		return "GB"
	case "uae", "u.a.e.", "united arab emirates":
		return "AE"
	case "kazakhstan", "kazakstan", "казахстан", "қазақстан", "republic of kazakhstan", "republic kazakhstan", "республика казахстан", "қазақстан республикасы":
		return "KZ"
	}
	return ""
}

func containsNormalizedGeoPhrase(haystack string, needle string) bool {
	h := normalizeGeoPhrase(haystack)
	n := normalizeGeoPhrase(needle)
	if h == "" || n == "" {
		return false
	}
	return strings.Contains(" "+h+" ", " "+n+" ")
}

func normalizeGeoPhrase(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	repl := strings.NewReplacer(",", " ", ".", " ", "-", " ", "_", " ", "(", " ", ")", " ", "/", " ")
	s = repl.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmptyUpper(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !strings.EqualFold(value, "na") {
			return strings.ToUpper(value)
		}
	}
	return ""
}
