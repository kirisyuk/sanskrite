package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type OrgGeoRevalidationResult struct {
	Applied          bool
	ChangedISO2      bool
	ChangedPath      bool
	PreviousISO2     string
	ResolvedISO2     string
	Query            string
	FormattedAddress string
	Reason           string
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
		if fragment.Coordinates == nil && models.IsRealGeoCoordinate(geo.Lat, geo.Lon) {
			fragment.Coordinates = &models.Coordinates{Lat: geo.Lat, Lon: geo.Lon}
		}
		if fragment.Identifiers == nil {
			fragment.Identifiers = map[string]string{}
		}
		fragment.Identifiers["geoVerifiedCountryAlpha2"] = strings.ToUpper(resolvedISO2)
		if strings.TrimSpace(geo.PlaceID) != "" {
			fragment.Identifiers["geoVerifiedPlaceId"] = strings.TrimSpace(geo.PlaceID)
		}
		fragment.Identifiers["geoVerifiedMethod"] = "geocoding"

		if upgradedPath := upgradedOrgCanonicalPath(fragment, resolvedISO2); upgradedPath != "" && upgradedPath != strings.TrimSpace(fragment.CanonicalPath) {
			fragment.CanonicalPath = upgradedPath
			if parts, err := taxonomy.ParseCanonicalPathParts(upgradedPath); err == nil && parts != nil {
				fragment.CanonicalParts = parts
			}
			result.ChangedPath = true
		}

		result.Applied = result.ChangedISO2 || result.ChangedPath
		result.ResolvedISO2 = strings.ToUpper(resolvedISO2)
		result.Query = query
		result.FormattedAddress = strings.TrimSpace(geo.FormattedAddress)
		switch {
		case result.ChangedISO2 && result.ChangedPath:
			result.Reason = "organization jurisdiction and canonical path revalidated via geocoding"
		case result.ChangedISO2:
			result.Reason = "organization jurisdiction revalidated via geocoding"
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

	name := orgRevalidationName(fragment)
	place := ""
	if fragment.HomeLocation != nil {
		place = strings.TrimSpace(fragment.HomeLocation.Name)
	}
	if name != "" && place != "" {
		add(name + ", " + place)
	}
	if name != "" {
		if labelISO2 := inferISO2FromLabel(name, fragment); labelISO2 != "" {
			if country := countryNameByISO2(labelISO2); country != "" {
				add(name + ", " + country)
				if place != "" {
					add(name + ", " + place + ", " + country)
				}
			}
		}
	}
	if currentISO2 := inferOrgISO2(fragment); currentISO2 != "" {
		if country := countryNameByISO2(currentISO2); country != "" && name != "" && place != "" {
			add(name + ", " + place + ", " + country)
		}
	}
	if pathISO2 := orgPathISO2(fragment.CanonicalPath); pathISO2 != "" && pathISO2 != "na" {
		if country := countryNameByISO2(pathISO2); country != "" && name != "" && place != "" {
			add(name + ", " + place + ", " + country)
		}
	}
	if name != "" && place == "" {
		return out
	}
	if place != "" {
		add(place)
	}
	return out
}

func orgRevalidationName(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.Identifiers != nil {
		if v := strings.TrimSpace(fragment.Identifiers["legalName"]); v != "" {
			return v
		}
	}
	if fragment.Labels != nil {
		if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
			return v
		}
	}
	if fragment.Identifiers != nil {
		if v := strings.TrimSpace(fragment.Identifiers["shortName"]); v != "" {
			return v
		}
	}
	return slugToLabel(fragment.CanonicalPath)
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

	if pathISO2 != "" && pathISO2 != "na" && pathISO2 == resolvedISO2 {
		return true
	}

	place := ""
	if fragment != nil && fragment.HomeLocation != nil {
		place = strings.TrimSpace(fragment.HomeLocation.Name)
	}
	queryHasPlace := place != "" && containsNormalizedGeoPhrase(query, place)
	formattedHasPlace := place != "" && containsNormalizedGeoPhrase(formattedAddress, place)
	labelISO2 := strings.ToLower(strings.TrimSpace(inferISO2FromLabel(orgRevalidationName(fragment), fragment)))
	if currentISO2 != "" && currentISO2 == resolvedISO2 {
		return queryHasPlace && formattedHasPlace
	}

	if pathISO2 != "" && pathISO2 != "na" && pathISO2 != resolvedISO2 {
		return labelISO2 != "" && labelISO2 == resolvedISO2
	}

	if currentISO2 != "" && currentISO2 != resolvedISO2 {
		if pathISO2 == "na" || pathISO2 == "" {
			return place != "" && queryHasPlace && formattedHasPlace
		}
		return labelISO2 != "" && labelISO2 == resolvedISO2
	}

	if place != "" && queryHasPlace && formattedHasPlace {
		return true
	}

	if labelISO2 != "" && labelISO2 == resolvedISO2 {
		return true
	}
	return false
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
	return msg
}
