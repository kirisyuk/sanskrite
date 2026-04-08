package zonegeo

import (
	"fmt"
	"regexp"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var trailingConditionSuffix = regexp.MustCompile(`\s+\([^)]*\)\s*$`)

func DerivePlaceCountry(zone *horizonmodels.HorizonFragment) (place, countryCode, geoPrecision string) {
	if zone == nil {
		return "", "", ""
	}
	countryCode = strings.ToUpper(strings.TrimSpace(countryCodeFromFragment(zone)))
	geoPrecision = strings.TrimSpace(qualifierString(zone.Qualifiers, "geoPrecision"))
	place = sanitizePlaceLabel(firstNonEmpty(
		zoneLabel(zone),
		humanizeZonePath(zone.CanonicalPath),
	))
	if geoPrecision == "" {
		geoPrecision = inferPrecisionFromZone(zone.CanonicalPath, countryCode)
	}
	return place, countryCode, geoPrecision
}

func BuildGeocodeQueries(place, countryCode, geoPrecision, canonicalPath string) []string {
	place = sanitizePlaceLabel(place)
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	queries := make([]string, 0, 6)
	push := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range queries {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		queries = append(queries, value)
	}

	push(place)
	if place == "" {
		push(humanizeZonePath(canonicalPath))
	}
	if place != "" && countryCode != "" && countryCode != "INT" && countryCode != "GLOBAL" {
		push(fmt.Sprintf("%s, %s", place, countryCode))
		if geoPrecision == "country" {
			push(countryCode)
		}
	}
	if tail := humanizeZonePath(canonicalPath); tail != "" && !strings.EqualFold(tail, place) {
		push(tail)
		if countryCode != "" && countryCode != "INT" && countryCode != "GLOBAL" {
			push(fmt.Sprintf("%s, %s", tail, countryCode))
		}
	}
	return queries
}

func ApplyCoordinates(zone *horizonmodels.HorizonFragment, lat, lon float64, place, countryCode string) bool {
	if zone == nil || !horizonmodels.IsRealGeoCoordinate(lat, lon) {
		return false
	}
	changed := false
	if zone.Coordinates == nil || !horizonmodels.IsRealGeoCoordinate(zone.Coordinates.Lat, zone.Coordinates.Lon) ||
		abs(zone.Coordinates.Lat-lat) > 0.0001 || abs(zone.Coordinates.Lon-lon) > 0.0001 {
		zone.Coordinates = &horizonmodels.Coordinates{Lat: lat, Lon: lon}
		changed = true
	}
	if zone.HomeLocation == nil {
		zone.HomeLocation = &horizonmodels.Location{}
	}
	if name := strings.TrimSpace(place); name != "" && strings.TrimSpace(zone.HomeLocation.Name) == "" {
		zone.HomeLocation.Name = name
		changed = true
	}
	iso := formatISO6709(lat, lon)
	if zone.HomeLocation.ISO6709 == nil || strings.TrimSpace(*zone.HomeLocation.ISO6709) != iso {
		isoCopy := iso
		zone.HomeLocation.ISO6709 = &isoCopy
		changed = true
	}
	if cc := strings.ToUpper(strings.TrimSpace(countryCode)); len(cc) == 2 {
		if zone.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*zone.HomeLocation.ISO3166_1) != cc {
			ccCopy := cc
			zone.HomeLocation.ISO3166_1 = &ccCopy
			changed = true
		}
	}
	if !zone.HasCoordinates {
		zone.HasCoordinates = true
		changed = true
	}
	return changed
}

func PreferredGeocodePrecision(place, countryCode, fallback string) string {
	placeNorm := normalizeText(place)
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	switch {
	case placeNorm == "":
		return firstNonEmpty(strings.TrimSpace(fallback), "precise")
	case len(cc) == 2 && (placeNorm == normalizeText(cc) || placeNorm == normalizeText(iso2ToCountry[cc])):
		return "country"
	case strings.Contains(placeNorm, "strait") || strings.Contains(placeNorm, "canal") || strings.Contains(placeNorm, "sea"):
		return "region"
	default:
		return "precise"
	}
}

func sanitizePlaceLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.TrimSpace(trailingConditionSuffix.ReplaceAllString(value, ""))
}

func humanizeZonePath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 4 || parts[0] != "zone" {
		return ""
	}
	return humanizeSlug(parts[3])
}

func inferPrecisionFromZone(path, countryCode string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 4 || parts[0] != "zone" {
		return ""
	}
	if len(strings.TrimSpace(countryCode)) == 2 && strings.EqualFold(parts[3], strings.TrimSpace(countryCode)) {
		return "country"
	}
	return "region"
}

func zoneLabel(zone *horizonmodels.HorizonFragment) string {
	if zone == nil {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(zone.Labels["en"]),
		strings.TrimSpace(zone.Description),
	)
}

func countryCodeFromFragment(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if cc := strings.ToUpper(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)); len(cc) == 2 {
			return cc
		}
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(fragment.CanonicalPath), "/"), "/")
	if len(parts) >= 2 && parts[0] == "zone" {
		if cc := strings.ToUpper(strings.TrimSpace(parts[1])); len(cc) == 2 {
			return cc
		}
	}
	return ""
}

func qualifierString(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	if raw, ok := values[key]; ok && raw != nil {
		return strings.TrimSpace(fmt.Sprint(raw))
	}
	return ""
}

func humanizeSlug(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	return strings.TrimSpace(value)
}

func normalizeText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatISO6709(lat, lon float64) string {
	return fmt.Sprintf("%+.6f%+.6f/", lat, lon)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

var iso2ToCountry = map[string]string{
	"AE": "United Arab Emirates",
	"BH": "Bahrain",
	"EG": "Egypt",
	"IL": "Israel",
	"IQ": "Iraq",
	"IR": "Iran",
	"KW": "Kuwait",
	"LY": "Libya",
	"OM": "Oman",
	"QA": "Qatar",
	"SA": "Saudi Arabia",
	"TR": "Turkey",
	"UA": "Ukraine",
	"YE": "Yemen",
}
