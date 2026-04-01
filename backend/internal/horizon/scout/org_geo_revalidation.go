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
	var bestGeo *coreontology.GeocodingResult
	var bestResolvedISO2 string
	var bestAttempt *geoResolveAttempt
	attempts := make([]geoResolveAttempt, 0, len(queries))
	for _, query := range queries {
		if isWeakGeoResolveQuery(query) {
			attempts = append(attempts, geoResolveAttempt{
				Query:   query,
				Outcome: "rejected",
				Reason:  "weak_query",
			})
			continue
		}
		geo, err := geocodingClient.Geocode(ctx, query)
		if err != nil {
			lastErr = err
			attempts = append(attempts, geoResolveAttempt{
				Query:   query,
				Outcome: "error",
				Reason:  formatGeoResolveErr(err),
			})
			continue
		}
		if geo == nil {
			attempts = append(attempts, geoResolveAttempt{
				Query:   query,
				Outcome: "miss",
				Reason:  "no_result",
			})
			continue
		}
		resolvedISO2 := strings.ToLower(strings.TrimSpace(inferCountryISO2FromFormattedAddress(geo.FormattedAddress)))
		if resolvedISO2 == "" || !taxonomy.IsISO2CountryCode(resolvedISO2) {
			resolvedISO2 = inferredOrgISO2FromStrongProviderHit(fragment, query, geo, currentISO2, pathISO2)
			if resolvedISO2 == "" || !taxonomy.IsISO2CountryCode(resolvedISO2) {
				attempts = append(attempts, geoResolveAttempt{
					Query:            query,
					Provider:         strings.TrimSpace(geo.Provider),
					Outcome:          "rejected",
					Reason:           "missing_resolved_country",
					FormattedAddress: strings.TrimSpace(geo.FormattedAddress),
				})
				continue
			}
		}
		score, acceptReason := scoreOrgGeoResult(fragment, query, geo.FormattedAddress, currentISO2, pathISO2, resolvedISO2)
		if score <= 0 {
			attempts = append(attempts, geoResolveAttempt{
				Query:            query,
				Provider:         strings.TrimSpace(geo.Provider),
				Outcome:          "rejected",
				Reason:           acceptReason,
				ResolvedISO2:     resolvedISO2,
				FormattedAddress: strings.TrimSpace(geo.FormattedAddress),
			})
			continue
		}
		candidate := geoResolveAttempt{
			Query:            query,
			Provider:         strings.TrimSpace(geo.Provider),
			Outcome:          "candidate",
			Reason:           acceptReason,
			ResolvedISO2:     resolvedISO2,
			FormattedAddress: strings.TrimSpace(geo.FormattedAddress),
			Score:            score,
		}
		attempts = append(attempts, candidate)
		if bestAttempt == nil || candidate.Score > bestAttempt.Score {
			copyCandidate := candidate
			bestAttempt = &copyCandidate
			bestGeo = geo
			bestResolvedISO2 = resolvedISO2
		}
	}
	if bestGeo != nil && bestAttempt != nil {
		if fragment.HomeLocation == nil {
			fragment.HomeLocation = &models.Location{}
		}
		if fragment.HomeLocation.ISO3166_1 == nil || !strings.EqualFold(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1), bestResolvedISO2) {
			v := strings.ToUpper(bestResolvedISO2)
			fragment.HomeLocation.ISO3166_1 = &v
			result.ChangedISO2 = true
		}
		if place := bestOrgPlaceHint(fragment); place != "" && shouldUpgradePlaceName(fragment.HomeLocation.Name, place) {
			fragment.HomeLocation.Name = strings.TrimSpace(place)
			result.ChangedLocationName = true
		}
		if models.IsRealGeoCoordinate(bestGeo.Lat, bestGeo.Lon) {
			result.ChangedCoordinates = henrich.ApplyResolvedCoordinates(fragment, bestGeo.Lat, bestGeo.Lon)
		}
		if fragment.Identifiers == nil {
			fragment.Identifiers = map[string]string{}
		}
		henrich.RecordGeoCoordinatesAttribution(fragment, henrich.GeoCoordinatesAttribution{
			Method:           "geocoding_revalidate",
			Provider:         strings.TrimSpace(bestGeo.Provider),
			Query:            bestAttempt.Query,
			FormattedAddress: strings.TrimSpace(bestGeo.FormattedAddress),
			PlaceID:          strings.TrimSpace(bestGeo.PlaceID),
			CountryAlpha2:    bestResolvedISO2,
		})
		fragment.Identifiers["geoVerifiedCountryAlpha2"] = strings.ToUpper(bestResolvedISO2)
		if strings.TrimSpace(bestGeo.PlaceID) != "" {
			fragment.Identifiers["geoVerifiedPlaceId"] = strings.TrimSpace(bestGeo.PlaceID)
		}
		fragment.Identifiers["geoVerifiedMethod"] = "geocoding"
		if provider := strings.TrimSpace(bestGeo.Provider); provider != "" {
			fragment.Identifiers["geoVerifiedProvider"] = provider
		}
		if formatted := strings.TrimSpace(bestGeo.FormattedAddress); formatted != "" {
			fragment.Identifiers["geoVerifiedFormattedAddress"] = formatted
		}
		fragment.Identifiers["geoVerifiedQuery"] = bestAttempt.Query

		if upgradedPath := upgradedOrgCanonicalPath(fragment, bestResolvedISO2); upgradedPath != "" && upgradedPath != strings.TrimSpace(fragment.CanonicalPath) {
			fragment.CanonicalPath = upgradedPath
			if parts, err := taxonomy.ParseCanonicalPathParts(upgradedPath); err == nil && parts != nil {
				fragment.CanonicalParts = parts
			}
			result.ChangedPath = true
		}

		result.Applied = result.ChangedISO2 || result.ChangedPath || result.ChangedCoordinates || result.ChangedLocationName
		result.ResolvedISO2 = strings.ToUpper(bestResolvedISO2)
		result.Query = bestAttempt.Query
		result.FormattedAddress = strings.TrimSpace(bestGeo.FormattedAddress)
		result.Provider = strings.TrimSpace(bestGeo.Provider)
		recordGeoResolveTrace(fragment, "org_revalidate", attempts, bestAttempt)
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
		recordGeoResolveTrace(fragment, "org_revalidate", attempts, nil)
		return result, nil
	}
	recordGeoResolveTrace(fragment, "org_revalidate", attempts, nil)
	return result, lastErr
}

func orgGeoValidationQueries(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	requireStrongName := orgHasStrongGeoResolvableName(fragment)
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
		if isWeakGeoResolveQuery(addr) {
			return
		}
		if requireStrongName && !orgQueryHasStrongEntityName(fragment, addr) {
			return
		}
		seen[key] = struct{}{}
		out = append(out, addr)
	}
	addForced := func(raw string) {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			return
		}
		key := strings.ToLower(addr)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, addr)
	}

	for _, query := range orgDeterministicGeoQueries(fragment) {
		addForced(query)
	}

	names := orgRevalidationNameCandidates(fragment)
	currentISO2Hint := inferOrgISO2(fragment)
	pathISO2Hint := orgPathISO2(fragment.CanonicalPath)
	resolvedISO2Hint := ""
	placeHints := filterCountryLikeGeoHints(orgPlaceHints(fragment), pathISO2Hint, currentISO2Hint, resolvedISO2Hint)
	geoHints := make([]string, 0, 3)
	if currentISO2 := currentISO2Hint; currentISO2 != "" {
		if country := countryNameByISO2(currentISO2); country != "" {
			geoHints = append(geoHints, country)
		}
	}
	if pathISO2 := pathISO2Hint; pathISO2 != "" && pathISO2 != "na" {
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
	score, _ := scoreOrgGeoResult(fragment, query, formattedAddress, currentISO2, pathISO2, resolvedISO2)
	return score > 0
}

func scoreOrgGeoResult(
	fragment *models.HorizonFragment,
	query string,
	formattedAddress string,
	currentISO2 string,
	pathISO2 string,
	resolvedISO2 string,
) (int, string) {
	resolvedISO2 = strings.ToLower(strings.TrimSpace(resolvedISO2))
	currentISO2 = strings.ToLower(strings.TrimSpace(currentISO2))
	pathISO2 = strings.ToLower(strings.TrimSpace(pathISO2))
	query = strings.TrimSpace(query)
	formattedAddress = strings.TrimSpace(formattedAddress)
	if isWeakGeoResolveQuery(query) {
		return 0, "weak_query"
	}
	specificResult := orgFormattedAddressLooksSpecific(formattedAddress, resolvedISO2)
	deterministicHint := orgIsDeterministicGeoQuery(fragment, query)
	strongNameHint := orgQueryHasStrongEntityName(fragment, query)
	placeHints := filterCountryLikeGeoHints(orgPlaceHints(fragment), pathISO2, currentISO2, resolvedISO2)
	queryHasPlace := anyGeoPhraseMatch(query, placeHints)
	formattedHasPlace := anyGeoPhraseMatch(formattedAddress, placeHints)
	labelISO2 := strings.ToLower(strings.TrimSpace(inferISO2FromLabel(orgRevalidationName(fragment), fragment)))
	score := 0
	reasons := make([]string, 0, 6)
	if pathISO2 != "" && pathISO2 != "na" && pathISO2 == resolvedISO2 {
		score += 30
		reasons = append(reasons, "path_country_match")
		if len(placeHints) > 0 {
			if queryHasPlace && formattedHasPlace {
				score += 50
				reasons = append(reasons, "query_place_match", "formatted_place_match")
				return score, geoResolveScoreReason(reasons...)
			}
			return 0, "path_country_match_without_place_support"
		}
		if (deterministicHint || strongNameHint) && specificResult {
			score += 35
			if deterministicHint {
				reasons = append(reasons, "deterministic_query")
			}
			if strongNameHint {
				reasons = append(reasons, "strong_name_query")
			}
			reasons = append(reasons, "specific_result")
			return score, geoResolveScoreReason(reasons...)
		}
		return 0, "path_country_match_without_specific_name_support"
	}
	if currentISO2 != "" && currentISO2 == resolvedISO2 {
		score += 15
		reasons = append(reasons, "current_country_match")
		if len(placeHints) > 0 {
			if queryHasPlace && formattedHasPlace {
				score += 40
				reasons = append(reasons, "query_place_match", "formatted_place_match")
				return score, geoResolveScoreReason(reasons...)
			}
			return 0, "current_country_match_without_place_support"
		}
		if (deterministicHint || strongNameHint) && specificResult {
			score += 25
			if deterministicHint {
				reasons = append(reasons, "deterministic_query")
			}
			if strongNameHint {
				reasons = append(reasons, "strong_name_query")
			}
			reasons = append(reasons, "specific_result")
			return score, geoResolveScoreReason(reasons...)
		}
		return 0, "current_country_match_without_specific_name_support"
	}

	if pathISO2 != "" && pathISO2 != "na" && pathISO2 != resolvedISO2 {
		if labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && (deterministicHint || strongNameHint) {
			score += 45
			reasons = append(reasons, "label_country_override", "specific_result")
			if deterministicHint {
				reasons = append(reasons, "deterministic_query")
			}
			if strongNameHint {
				reasons = append(reasons, "strong_name_query")
			}
			return score, geoResolveScoreReason(reasons...)
		}
		return 0, "path_country_conflict"
	}

	if currentISO2 != "" && currentISO2 != resolvedISO2 {
		if pathISO2 == "na" || pathISO2 == "" {
			if len(placeHints) > 0 && queryHasPlace && formattedHasPlace {
				score += 50
				reasons = append(reasons, "unknown_path_place_override")
				return score, geoResolveScoreReason(reasons...)
			}
			return 0, "current_country_conflict_without_place_support"
		}
		if labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && (deterministicHint || strongNameHint) {
			score += 45
			reasons = append(reasons, "label_country_override", "specific_result")
			return score, geoResolveScoreReason(reasons...)
		}
		return 0, "current_country_conflict"
	}

	if len(placeHints) > 0 && queryHasPlace && formattedHasPlace {
		score += 50
		reasons = append(reasons, "place_match")
		return score, geoResolveScoreReason(reasons...)
	}

	if labelISO2 != "" && labelISO2 == resolvedISO2 && specificResult && (deterministicHint || strongNameHint) {
		score += 40
		reasons = append(reasons, "label_country_match", "specific_result")
		if deterministicHint {
			reasons = append(reasons, "deterministic_query")
		}
		if strongNameHint {
			reasons = append(reasons, "strong_name_query")
		}
		return score, geoResolveScoreReason(reasons...)
	}
	return 0, "insufficient_place_or_name_support"
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
		if isWeakGeoResolveQuery(v) {
			return
		}
		for _, candidate := range orgRevalidationNameCandidates(fragment) {
			if normalizeGeoPhrase(candidate) == normalizeGeoPhrase(v) {
				return
			}
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

func filterCountryLikeGeoHints(hints []string, isoCandidates ...string) []string {
	if len(hints) == 0 {
		return nil
	}
	blocked := make(map[string]struct{})
	for _, iso := range isoCandidates {
		iso = strings.ToLower(strings.TrimSpace(iso))
		if iso == "" || iso == "na" {
			continue
		}
		blocked[iso] = struct{}{}
		if country := normalizeGeoPhrase(countryNameByISO2(iso)); country != "" {
			blocked[country] = struct{}{}
		}
	}
	out := make([]string, 0, len(hints))
	for _, hint := range hints {
		normalized := normalizeGeoPhrase(hint)
		if normalized == "" {
			continue
		}
		if _, ok := blocked[normalized]; ok {
			continue
		}
		out = append(out, hint)
	}
	return out
}

func inferredOrgISO2FromStrongProviderHit(
	fragment *models.HorizonFragment,
	query string,
	geo *coreontology.GeocodingResult,
	currentISO2 string,
	pathISO2 string,
) string {
	if geo == nil || !strings.EqualFold(strings.TrimSpace(geo.Provider), coreontology.GeocodingProviderGoogle) {
		return ""
	}
	query = strings.TrimSpace(query)
	formattedAddress := strings.TrimSpace(geo.FormattedAddress)
	formattedAddress = strings.TrimSpace(formattedAddress)
	if query == "" || formattedAddress == "" {
		return ""
	}
	pathISO2 = strings.ToLower(strings.TrimSpace(pathISO2))
	currentISO2 = strings.ToLower(strings.TrimSpace(currentISO2))
	placeHints := orgPlaceHints(fragment)
	queryHasPlace := anyGeoPhraseMatch(query, placeHints)
	formattedHasPlace := anyGeoPhraseMatch(formattedAddress, placeHints)
	deterministicHint := orgIsDeterministicGeoQuery(fragment, query)
	strongNameHint := orgQueryHasStrongEntityName(fragment, query)
	if !deterministicHint && !strongNameHint {
		return ""
	}
	if pathISO2 != "" && pathISO2 != "na" {
		switch {
		case orgFormattedAddressLooksSpecific(formattedAddress, pathISO2):
			return pathISO2
		case queryHasPlace && formattedHasPlace && len(strings.Fields(normalizeGeoPhrase(formattedAddress))) >= 2:
			return pathISO2
		}
	}
	if currentISO2 != "" && currentISO2 != "na" && queryHasPlace && formattedHasPlace && len(strings.Fields(normalizeGeoPhrase(formattedAddress))) >= 2 {
		return currentISO2
	}
	return ""
}

func orgQueryHasStrongEntityName(fragment *models.HorizonFragment, query string) bool {
	for _, candidate := range orgRevalidationNameCandidates(fragment) {
		if !orgNameLooksGeoResolvable(candidate) {
			continue
		}
		if containsNormalizedGeoPhrase(query, candidate) {
			return true
		}
	}
	return false
}

func orgHasStrongGeoResolvableName(fragment *models.HorizonFragment) bool {
	for _, candidate := range orgRevalidationNameCandidates(fragment) {
		if orgNameLooksGeoResolvable(candidate) {
			return true
		}
	}
	return false
}

func orgNameLooksGeoResolvable(raw string) bool {
	tokens := strings.Fields(normalizeGeoPhrase(raw))
	if len(tokens) == 0 {
		return false
	}
	signal := 0
	for _, token := range tokens {
		if len(token) <= 2 || isWeakGeoResolveToken(token) {
			continue
		}
		signal++
	}
	return signal >= 2 || (signal == 1 && len(tokens) >= 2)
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
