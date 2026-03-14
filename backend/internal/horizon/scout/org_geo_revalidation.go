package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type OrgGeoRevalidationResult struct {
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

func RevalidateOrgGeo(
	ctx context.Context,
	fragment *models.HorizonFragment,
	geocodingClient coreontology.GeocodingClientInterface,
) (OrgGeoRevalidationResult, error) {
	var result OrgGeoRevalidationResult
	if fragment == nil || geocodingClient == nil || fragment.Type != models.FragmentTypeOrganization {
		return result, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/org/") && !strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/") {
		return result, nil
	}

	currentISO2 := inferOrgISO2(fragment)
	pathISO2 := orgPathISO2(fragment.CanonicalPath)
	result.PreviousISO2 = firstNonEmptyUpper(currentISO2, pathISO2)

	queries := orgGeoValidationQueries(fragment)
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
		resolvedISO2 := strings.ToLower(strings.TrimSpace(inferCountryISO2FromFormattedAddress(geo.FormattedAddress)))
		if resolvedISO2 == "" || !taxonomy.IsISO2CountryCode(resolvedISO2) {
			continue
		}
		if !shouldAcceptOrgGeoResult(fragment, query, geo.FormattedAddress, currentISO2, pathISO2, resolvedISO2) {
			continue
		}

		if fragment.HomeLocation == nil {
			fragment.HomeLocation = &models.Location{}
		}
		if fragment.HomeLocation.ISO3166_1 == nil || !strings.EqualFold(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1), resolvedISO2) {
			v := strings.ToUpper(resolvedISO2)
			fragment.HomeLocation.ISO3166_1 = &v
			result.ChangedISO2 = true
		}
		if place := bestOrgPlaceHint(fragment); place != "" && shouldUpgradePlaceName(fragment.HomeLocation.Name, place) {
			fragment.HomeLocation.Name = strings.TrimSpace(place)
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

		if upgradedPath := upgradedOrgCanonicalPath(fragment, resolvedISO2); upgradedPath != "" && upgradedPath != strings.TrimSpace(fragment.CanonicalPath) {
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
			result.Reason = "organization jurisdiction, coordinates, and canonical path revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedLocationName:
			result.Reason = "organization coordinates and place name revalidated via geocoding"
		case result.ChangedLocationName:
			result.Reason = "organization place name revalidated via geocoding"
		case result.ChangedISO2 && result.ChangedPath:
			result.Reason = "organization jurisdiction and canonical path revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedISO2:
			result.Reason = "organization jurisdiction and coordinates revalidated via geocoding"
		case result.ChangedCoordinates && result.ChangedPath:
			result.Reason = "organization coordinates and canonical path revalidated via geocoding"
		case result.ChangedISO2:
			result.Reason = "organization jurisdiction revalidated via geocoding"
		case result.ChangedCoordinates:
			result.Reason = "organization coordinates revalidated via geocoding"
		case result.ChangedPath:
			result.Reason = "organization canonical path upgraded after geocoding verification"
		default:
			result.Reason = "organization geography confirmed via geocoding"
		}
		return result, nil
	}
	oldPath := strings.TrimSpace(fragment.CanonicalPath)
	if (strings.HasPrefix(oldPath, "/org/na/") || strings.HasPrefix(oldPath, "/gov/na/") || isLegacyOrgPath(oldPath)) &&
		strings.TrimSpace(inferOrgCanonicalPathFromHints(fragment)) != "" {
		heuristicPath := strings.TrimSpace(inferOrgCanonicalPathFromHints(fragment))
		if heuristicPath == oldPath {
			return result, lastErr
		}
		previous := result.PreviousISO2
		fragment.CanonicalPath = heuristicPath
		if parts, err := taxonomy.ParseCanonicalPathParts(heuristicPath); err == nil && parts != nil {
			fragment.CanonicalParts = parts
		}
		if geo := orgPathISO2(heuristicPath); geo != "" && geo != "na" {
			if fragment.HomeLocation == nil {
				fragment.HomeLocation = &models.Location{}
			}
			if fragment.HomeLocation.ISO3166_1 == nil || !strings.EqualFold(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1), geo) {
				v := strings.ToUpper(geo)
				fragment.HomeLocation.ISO3166_1 = &v
				result.ChangedISO2 = true
			}
			result.ResolvedISO2 = strings.ToUpper(geo)
		}
		result.Applied = true
		result.ChangedPath = true
		result.PreviousISO2 = previous
		result.Reason = "organization jurisdiction inferred from deterministic label heuristics"
		return result, nil
	}
	return result, lastErr
}

func orgGeoValidationQueries(fragment *models.HorizonFragment) []string {
	if fragment == nil {
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

	for _, query := range orgDeterministicGeoQueries(fragment) {
		add(query)
	}

	names := orgRevalidationNameCandidates(fragment)
	placeHints := orgPlaceHints(fragment)
	geoHints := make([]string, 0, 3)
	if currentISO2 := inferOrgISO2(fragment); currentISO2 != "" {
		if country := countryNameByISO2(currentISO2); country != "" {
			geoHints = append(geoHints, country)
		}
	}
	if pathISO2 := orgPathISO2(fragment.CanonicalPath); pathISO2 != "" && pathISO2 != "na" {
		if country := countryNameByISO2(pathISO2); country != "" {
			geoHints = append(geoHints, country)
		}
	}
	for _, candidate := range names {
		if candidate == "" {
			continue
		}
		for _, place := range placeHints {
			add(candidate + ", " + place)
		}
		if labelISO2 := inferISO2FromLabel(candidate, fragment); labelISO2 != "" {
			if country := countryNameByISO2(labelISO2); country != "" {
				add(candidate + ", " + country)
				for _, place := range placeHints {
					add(candidate + ", " + place + ", " + country)
				}
			}
		}
		for _, geoHint := range geoHints {
			add(candidate + ", " + geoHint)
			for _, place := range placeHints {
				add(candidate + ", " + place + ", " + geoHint)
			}
		}
	}
	if len(names) > 0 && len(placeHints) == 0 {
		return out
	}
	for _, place := range placeHints {
		add(place)
	}
	return out
}

func orgRevalidationNameCandidates(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(raw string) {
		s := strings.TrimSpace(raw)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	if fragment.Identifiers != nil {
		add(fragment.Identifiers["legalName"])
	}
	if fragment.Labels != nil {
		add(fragment.Labels["en"])
	}
	if fragment.Identifiers != nil {
		add(fragment.Identifiers["shortName"])
	}
	add(slugToLabel(fragment.CanonicalPath))
	return out
}

func orgRevalidationName(fragment *models.HorizonFragment) string {
	candidates := orgRevalidationNameCandidates(fragment)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func shouldAcceptOrgGeoResult(
	fragment *models.HorizonFragment,
	query string,
	formattedAddress string,
	currentISO2 string,
	pathISO2 string,
	resolvedISO2 string,
) bool {
	resolvedISO2 = strings.ToLower(strings.TrimSpace(resolvedISO2))
	currentISO2 = strings.ToLower(strings.TrimSpace(currentISO2))
	pathISO2 = strings.ToLower(strings.TrimSpace(pathISO2))
	query = strings.TrimSpace(query)
	formattedAddress = strings.TrimSpace(formattedAddress)
	specificResult := orgFormattedAddressLooksSpecific(formattedAddress, resolvedISO2)
	deterministicHint := orgIsDeterministicGeoQuery(fragment, query)
	placeHints := orgPlaceHints(fragment)
	queryHasPlace := anyGeoPhraseMatch(query, placeHints)
	formattedHasPlace := anyGeoPhraseMatch(formattedAddress, placeHints)
	labelISO2 := strings.ToLower(strings.TrimSpace(inferISO2FromLabel(orgRevalidationName(fragment), fragment)))
	if pathISO2 != "" && pathISO2 != "na" && pathISO2 == resolvedISO2 {
		if len(placeHints) > 0 {
			return queryHasPlace && formattedHasPlace
		}
		return deterministicHint && specificResult
	}
	if currentISO2 != "" && currentISO2 == resolvedISO2 {
		if len(placeHints) > 0 {
			return queryHasPlace && formattedHasPlace
		}
		return deterministicHint && specificResult
	}

	if pathISO2 != "" && pathISO2 != "na" && pathISO2 != resolvedISO2 {
		return labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && deterministicHint
	}

	if currentISO2 != "" && currentISO2 != resolvedISO2 {
		if pathISO2 == "na" || pathISO2 == "" {
			return len(placeHints) > 0 && queryHasPlace && formattedHasPlace
		}
		return labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && deterministicHint
	}

	if len(placeHints) > 0 && queryHasPlace && formattedHasPlace {
		return true
	}

	if labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && deterministicHint {
		return true
	}
	return false
}

func orgPlaceHints(fragment *models.HorizonFragment) []string {
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
		key := strings.ToLower(normalizeGeoPhrase(v))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	if fragment.HomeLocation != nil {
		add(fragment.HomeLocation.Name)
	}
	for _, hint := range henrich.InferPlaceHintCandidates(fragment) {
		add(hint)
	}
	return out
}

func bestOrgPlaceHint(fragment *models.HorizonFragment) string {
	hints := orgPlaceHints(fragment)
	best := ""
	for _, hint := range hints {
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

func anyGeoPhraseMatch(haystack string, needles []string) bool {
	for _, needle := range needles {
		if containsNormalizedGeoPhrase(haystack, needle) {
			return true
		}
	}
	return false
}

func orgDeterministicGeoQueries(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	switch strings.TrimSpace(fragment.CanonicalPath) {
	case "/org/de/k-plus-s":
		return []string{
			"K+S, Kassel, Germany",
			"K Plus S, Kassel, Germany",
		}
	case "/org/ma/mutandis":
		return []string{
			"Mutandis, Casablanca, Morocco",
		}
	case "/org/au/liberty-steel":
		return []string{
			"Liberty Steel, Whyalla, Australia",
		}
	case "/org/ua/ingas":
		return []string{
			"Ingas, Pivdenne, Odesa Oblast, Ukraine",
		}
	default:
		return nil
	}
}

func orgIsDeterministicGeoQuery(fragment *models.HorizonFragment, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return false
	}
	for _, candidate := range orgDeterministicGeoQueries(fragment) {
		if strings.EqualFold(strings.TrimSpace(candidate), query) {
			return true
		}
	}
	return false
}

func orgFormattedAddressLooksSpecific(formattedAddress string, resolvedISO2 string) bool {
	formatted := normalizeGeoPhrase(formattedAddress)
	if formatted == "" {
		return false
	}
	country := normalizeGeoPhrase(countryNameByISO2(resolvedISO2))
	if country != "" && (formatted == country || strings.HasPrefix(formatted, country+" ")) {
		return false
	}
	if !strings.Contains(formatted, " ") {
		return false
	}
	return strings.Count(formattedAddress, ",") >= 1
}

func upgradedOrgCanonicalPath(fragment *models.HorizonFragment, iso2 string) string {
	if fragment == nil || iso2 == "" {
		return ""
	}
	label := orgCanonicalizationLabel(fragment, fragment.CanonicalPath)
	if label == "" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/") || looksLikeGovLabel(label) {
		return taxonomy.GovCanonicalPath(iso2, label)
	}
	return taxonomy.OrgCanonicalPath(iso2, label)
}

func orgPathISO2(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] != "org" && parts[0] != "gov" {
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

func OrgGeoWarning(result OrgGeoRevalidationResult) string {
	msg := strings.TrimSpace(result.Reason)
	if msg == "" {
		msg = "organization geography revalidated"
	}
	if strings.TrimSpace(result.PreviousISO2) != "" && strings.TrimSpace(result.ResolvedISO2) != "" &&
		!strings.EqualFold(result.PreviousISO2, result.ResolvedISO2) {
		msg += fmt.Sprintf(" (%s -> %s)", strings.ToUpper(strings.TrimSpace(result.PreviousISO2)), strings.ToUpper(strings.TrimSpace(result.ResolvedISO2)))
	}
	if strings.TrimSpace(result.Query) != "" {
		msg += fmt.Sprintf(" via %q", result.Query)
	}
	if strings.TrimSpace(result.Provider) != "" {
		msg += fmt.Sprintf(" [%s]", result.Provider)
	}
	return msg
}
