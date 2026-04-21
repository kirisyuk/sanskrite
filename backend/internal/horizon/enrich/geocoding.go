package enrich

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	geoAddressSourceExplicitAddress    = "explicit_address"
	geoAddressSourcePlaceCountry       = "place_country"
	geoAddressSourceZoneSlugCountry    = "zone_slug_country"
	geoAddressSourceLocPath            = "loc_path"
	geoAddressSourceDepositNameCountry = "deposit_name_country"
	geoAddressSourceEntityNameCountry  = "entity_name_country"
)

// GeoCoordinatesAttribution stores explainable metadata about how fragment coordinates were resolved.
type GeoCoordinatesAttribution struct {
	Method           string
	Provider         string
	Query            string
	FormattedAddress string
	PlaceID          string
	CountryAlpha2    string
}

// GeoRelevantTypes are fragment classes where coordinates are useful for map rendering.
// Gov bodies use ORGANIZATION class with /gov/ canonical path.
// Legacy DEPOSIT/PROGRAM/AFFECTED_ZONE are folded into site/org/geo classes under
// the AH taxonomy and are therefore already covered by the entries below.
var GeoRelevantTypes = map[FragmentType]struct{}{
	FragmentTypeOrganization: {},
	FragmentTypeFacility:     {},
	FragmentTypeCommunity:    {},
	FragmentTypeGeoRegion:    {},
	FragmentTypeEvent:        {},
	FragmentTypeMarket:       {},
}

// EnrichCoordinatesFromGeoNames resolves coordinates via GeoNames.
// Priority:
// 1) strict /loc/{cc}/{adm1}/{city} canonical path,
// 2) explicit identifiers.geonamesId,
// 3) homeLocation.name + country for geo-relevant fragment types.
// Returns changed=true if coordinates were applied. Use before EnrichCoordinatesFromGeocoding.
func EnrichCoordinatesFromGeoNames(
	ctx context.Context,
	gn coreontology.GeoNamesClientInterface,
	fragment *hmodels.HorizonFragment,
) (changed bool, err error) {
	if gn == nil || fragment == nil {
		return false, nil
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false, nil
	}

	if changed, err := enrichCoordinatesFromGeoNamesCanonicalLoc(ctx, gn, fragment); changed || err != nil {
		return changed, err
	}
	if changed, err := enrichCoordinatesFromGeoNamesCanonicalZone(ctx, gn, fragment); changed || err != nil {
		return changed, err
	}

	if geonamesID := extractGeoNamesID(fragment); geonamesID > 0 {
		place, err := gn.GetPlace(ctx, geonamesID)
		if err != nil {
			return false, err
		}
		if applyGeoNamesCoordinates(fragment, place) {
			if fragment.Identifiers == nil {
				fragment.Identifiers = map[string]string{}
			}
			fragment.Identifiers["geonamesId"] = strconv.Itoa(geonamesID)
			return true, nil
		}
	}

	if _, ok := GeoRelevantTypes[fragment.Class]; !ok {
		return false, nil
	}
	name := ""
	if fragment.HomeLocation != nil {
		name = strings.TrimSpace(fragment.HomeLocation.Name)
	}
	cc := extractCountryFromFragment(fragment)
	if name == "" || cc == "" {
		return false, nil
	}

	best, err := searchBestGeoNamesPlace(ctx, gn, name, cc, "")
	if err != nil {
		return false, err
	}
	if best == nil || best.GeoNameID <= 0 {
		return false, nil
	}
	place, err := gn.GetPlace(ctx, best.GeoNameID)
	if err != nil {
		return false, err
	}
	if !applyGeoNamesCoordinates(fragment, place) {
		return false, nil
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	fragment.Identifiers["geonamesId"] = strconv.Itoa(best.GeoNameID)
	return true, nil
}

func enrichCoordinatesFromGeoNamesCanonicalLoc(
	ctx context.Context,
	gn coreontology.GeoNamesClientInterface,
	fragment *hmodels.HorizonFragment,
) (bool, error) {
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/loc/") {
		return false, nil
	}
	ccParsed, level1, level2, ok := taxonomy.ParseLocCanonicalPath(cp)
	if !ok {
		return false, nil
	}
	cc := normalizeISO2(ccParsed)
	if cc == "" {
		return false, nil
	}
	adm := strings.TrimSpace(level1)
	cityToken := strings.TrimSpace(level2)
	if cityToken == "" {
		cityToken = strings.TrimSpace(level1)
		adm = ""
	}
	city := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(cityToken)))
	if city == "" {
		return false, nil
	}

	best, err := searchBestGeoNamesPlace(ctx, gn, city, cc, adm)
	if err != nil {
		return false, err
	}
	if best == nil || best.GeoNameID <= 0 {
		return false, nil
	}
	place, err := gn.GetPlace(ctx, best.GeoNameID)
	if err != nil {
		return false, err
	}
	if !applyGeoNamesCoordinates(fragment, place) {
		return false, nil
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	fragment.Identifiers["geonamesId"] = strconv.Itoa(best.GeoNameID)
	return true, nil
}

func enrichCoordinatesFromGeoNamesCanonicalZone(
	ctx context.Context,
	gn coreontology.GeoNamesClientInterface,
	fragment *hmodels.HorizonFragment,
) (bool, error) {
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/zone/") || !fragment.IsAffectedZoneGeo() {
		return false, nil
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 4 || parts[0] != "zone" {
		return false, nil
	}
	geo := strings.TrimSpace(strings.ToLower(parts[1]))
	slug := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3])))
	if slug == "" || strings.EqualFold(slug, "na") {
		return false, nil
	}
	cc := ""
	if len(geo) == 2 && geo != "na" {
		cc = strings.ToUpper(geo)
	} else if idx := strings.Index(geo, "-"); idx > 0 && len(geo[:idx]) == 2 {
		cc = strings.ToUpper(geo[:idx])
	}
	if cc == "" {
		return false, nil
	}

	best, err := searchBestGeoNamesPlace(ctx, gn, slug, cc, "")
	if err != nil {
		return false, err
	}
	if best == nil || best.GeoNameID <= 0 {
		return false, nil
	}
	place, err := gn.GetPlace(ctx, best.GeoNameID)
	if err != nil {
		return false, err
	}
	if !applyGeoNamesCoordinates(fragment, place) {
		return false, nil
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	fragment.Identifiers["geonamesId"] = strconv.Itoa(best.GeoNameID)
	return true, nil
}

func searchBestGeoNamesPlace(
	ctx context.Context,
	gn coreontology.GeoNamesClientInterface,
	name string,
	cc string,
	adm string,
) (*coreontology.GeoNamesPlace, error) {
	opts := coreontology.GeoNamesSearchOptions{Country: cc, FeatureClass: "P", NameEquals: true}
	admNorm := strings.ToUpper(strings.TrimSpace(adm))
	if len(admNorm) == 2 && admNorm[0] >= 'A' && admNorm[0] <= 'Z' {
		opts.AdminCode1 = admNorm
	}
	resp, err := gn.Search(ctx, name, 10, opts)
	if err != nil {
		return nil, err
	}
	if best := pickBestGeoNamesResult(resp, cc); best != nil {
		return best, nil
	}

	// Fallback to fuzzy name search when strict NameEquals had no hits.
	opts.NameEquals = false
	resp, err = gn.Search(ctx, name, 10, opts)
	if err != nil {
		return nil, err
	}
	return pickBestGeoNamesResult(resp, cc), nil
}

func pickBestGeoNamesResult(resp *coreontology.GeoNamesSearchResponse, cc string) *coreontology.GeoNamesPlace {
	if resp == nil || len(resp.Results) == 0 {
		return nil
	}
	var best *coreontology.GeoNamesPlace
	for i := range resp.Results {
		r := &resp.Results[i]
		if cc != "" && !strings.EqualFold(strings.TrimSpace(r.CountryCode), cc) {
			continue
		}
		if best == nil || r.Population > best.Population {
			best = r
		}
	}
	return best
}

func applyGeoNamesCoordinates(fragment *hmodels.HorizonFragment, place *coreontology.GeoNamesPlace) bool {
	if fragment == nil || place == nil || strings.TrimSpace(place.Lat) == "" || strings.TrimSpace(place.Lng) == "" {
		return false
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(place.Lat), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(place.Lng), 64)
	if errLat != nil || errLon != nil || !hmodels.IsValidCoordinates(lat, lon) || !hmodels.IsRealGeoCoordinate(lat, lon) {
		return false
	}
	changed := ApplyResolvedCoordinates(fragment, lat, lon)
	RecordGeoCoordinatesAttribution(fragment, GeoCoordinatesAttribution{
		Method:           "geonames",
		Provider:         "geonames",
		PlaceID:          strconv.Itoa(place.GeoNameID),
		FormattedAddress: strings.TrimSpace(firstNonEmpty(place.Name, place.ToponymName)),
		CountryAlpha2:    strings.TrimSpace(place.CountryCode),
	})
	return changed
}

func extractGeoNamesID(fragment *hmodels.HorizonFragment) int {
	if fragment == nil {
		return 0
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"geonamesId", "geonameId", "geonames"} {
			if raw := strings.TrimSpace(fragment.Identifiers[key]); raw != "" {
				if id, err := strconv.Atoi(raw); err == nil && id > 0 {
					return id
				}
			}
		}
	}
	for _, ref := range fragment.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "GeoNames") {
			if raw := strings.TrimSpace(ref.ExternalID); raw != "" {
				if id, err := strconv.Atoi(raw); err == nil && id > 0 {
					return id
				}
			}
		}
	}
	return 0
}

// EnrichCoordinatesFromParent applies parent coordinates to child fragment when:
// - child has no real coordinates,
// - parent has real coordinates,
// - child type is propagation-eligible (facility/community/program),
// - and country doesn't explicitly conflict.
func EnrichCoordinatesFromParent(fragment *hmodels.HorizonFragment, parent *hmodels.HorizonFragment) bool {
	if fragment == nil || parent == nil {
		return false
	}
	if !isParentGeoPropagationType(fragment.Class) {
		return false
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false
	}
	if !hmodels.HasRealGeoCoordinates(parent.Coordinates) {
		return false
	}
	childCC := extractCountryFromFragment(fragment)
	parentCC := extractCountryFromFragment(parent)
	if childCC != "" && parentCC != "" && childCC != parentCC {
		return false
	}

	fragment.Coordinates = &hmodels.Coordinates{
		Lat: parent.Coordinates.Lat,
		Lon: parent.Coordinates.Lon,
	}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", parent.Coordinates.Lat, parent.Coordinates.Lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) == "" {
		fragment.HomeLocation.ISO6709 = &iso
	}
	if parentCC != "" && (fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) == "") {
		cc := parentCC
		fragment.HomeLocation.ISO3166_1 = &cc
	}
	RecordGeoCoordinatesAttribution(fragment, GeoCoordinatesAttribution{
		Method:           "parent_propagation",
		Provider:         "parent_fragment",
		FormattedAddress: strings.TrimSpace(parent.CanonicalPath),
		CountryAlpha2:    parentCC,
	})
	return true
}

// EnrichCoordinatesFromGeocoding attempts to resolve coordinates for a fragment
// by geocoding its address. Only runs when:
// - Fragment type is geo-relevant (org, facility, gov, etc.)
// - Fragment does not already have valid coordinates
// - An address can be built from fragment fields
//
// Returns changed=true if coordinates were applied.
func EnrichCoordinatesFromGeocoding(
	ctx context.Context,
	client coreontology.GeocodingClientInterface,
	fragment *hmodels.HorizonFragment,
) (changed bool, err error) {
	if client == nil || fragment == nil {
		return false, nil
	}
	if _, ok := GeoRelevantTypes[fragment.Class]; !ok {
		return false, nil
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false, nil
	}

	addr, source := BuildAddressFromFragmentWithSource(fragment)
	if addr == "" || !addressvalidate.IsPlausibleAddress(addr) {
		return false, nil
	}
	if source == geoAddressSourceEntityNameCountry {
		// This is the weakest candidate and is the main source of random same-country placements.
		// Keep it available for explicit diagnostics, but do not auto-attach coordinates from it.
		return false, nil
	}

	result, err := client.Geocode(ctx, addr)
	if err != nil {
		return false, fmt.Errorf("geocoding %q: %w", addr, err)
	}
	if result == nil {
		return false, nil
	}
	if !hmodels.IsValidCoordinates(result.Lat, result.Lon) {
		return false, nil
	}
	if !hmodels.IsRealGeoCoordinate(result.Lat, result.Lon) {
		return false, nil
	}

	changed = ApplyResolvedCoordinates(fragment, result.Lat, result.Lon)
	RecordGeoCoordinatesAttribution(fragment, GeoCoordinatesAttribution{
		Method:           "geocoding",
		Provider:         strings.TrimSpace(result.Provider),
		Query:            addr,
		FormattedAddress: strings.TrimSpace(result.FormattedAddress),
		PlaceID:          strings.TrimSpace(result.PlaceID),
	})
	return changed, nil
}

// BuildAddressFromFragment constructs an address string from fragment fields.
// Exported for tooling (e.g. horizon_backfill_coordinates).
// Only returns strings that look like real addresses (street, city, place+country).
// Does NOT use company names (label/legalName) — those are not addresses.
//
// Priority: explicit address identifiers > homeLocation (place name + country).
func BuildAddressFromFragment(f *hmodels.HorizonFragment) string {
	addr, _ := BuildAddressFromFragmentWithSource(f)
	return addr
}

// BuildAddressFromFragmentWithSource returns the best geocoding candidate and its derivation source.
func BuildAddressFromFragmentWithSource(f *hmodels.HorizonFragment) (string, string) {
	if f == nil {
		return "", ""
	}

	// 1. Explicit address fields (from OpenCorporates, Wikidata P969, etc.)
	for _, k := range []string{
		"registeredAddress", "address", "streetAddress", "street_address", "registered_address",
		"headquartersAddress", "hqAddress", "postalAddress", "officeAddress", "location",
	} {
		if f.Identifiers != nil {
			if v := strings.TrimSpace(f.Identifiers[k]); v != "" {
				addr := v
				if cc := extractCountryFromFragment(f); cc != "" {
					addr = v + ", " + cc
				}
				if addressvalidate.IsPlausibleAddress(addr) {
					return addr, geoAddressSourceExplicitAddress
				}
			}
		}
	}

	// 1.5 City/locality fields from identifiers + country.
	if f.Identifiers != nil {
		for _, k := range []string{"city", "locality", "town", "municipality", "settlement"} {
			if v := strings.TrimSpace(f.Identifiers[k]); v != "" {
				if cc := extractCountryFromFragment(f); cc != "" {
					addr := v + ", " + cc
					if addressvalidate.IsPlausibleAddress(addr) {
						return addr, geoAddressSourcePlaceCountry
					}
				}
			}
		}
	}

	// 2. homeLocation: place name + country (e.g. "Dubai, AE", "Houston, US")
	// Fallback: when LLM returns name but not iso3166_1, use country from canonical path (/org/ae/..., /gov/us/...).
	if f.HomeLocation != nil {
		name := strings.TrimSpace(f.HomeLocation.Name)
		cc := ""
		if f.HomeLocation.ISO3166_1 != nil {
			cc = strings.TrimSpace(*f.HomeLocation.ISO3166_1)
		}
		if cc == "" {
			cc = extractCountryFromFragment(f)
		}
		if name != "" && cc != "" {
			addr := name + ", " + cc
			if addressvalidate.IsPlausibleAddress(addr) {
				return addr, geoAddressSourcePlaceCountry
			}
		}
		// For geo-centric fragment kinds, a plain place name can still be geocoded.
		if name != "" && (f.Class == FragmentTypeGeoRegion || f.Class == FragmentTypeEvent || f.IsAffectedZoneGeo()) {
			if addressvalidate.IsPlausibleAddress(name) {
				return name, geoAddressSourcePlaceCountry
			}
		}
	}

	// 2b. /zone/{geo}/{condition_type}/{slug}: build "slug, CC" from canonical path for AFFECTED_ZONE.
	if f.IsAffectedZoneGeo() && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/zone/") {
		parts := strings.Split(strings.Trim(strings.TrimSpace(f.CanonicalPath), "/"), "/")
		if len(parts) >= 4 && parts[0] == "zone" {
			geo := strings.TrimSpace(parts[1])
			slug := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3])))
			if slug != "" && !strings.EqualFold(slug, "na") {
				cc := extractCountryFromCanonicalPath("/zone/" + geo) // uses zone geo handling below
				if cc != "" {
					addr := slug + ", " + cc
					if addressvalidate.IsPlausibleAddress(addr) {
						return addr, geoAddressSourceZoneSlugCountry
					}
				}
				// Fallback: slug + geo as-is (for composite tr-sy use first part for geocoding)
				if len(geo) == 2 && geo != "na" {
					addr := slug + ", " + strings.ToUpper(geo)
					if addressvalidate.IsPlausibleAddress(addr) {
						return addr, geoAddressSourceZoneSlugCountry
					}
				}
				if idx := strings.Index(geo, "-"); idx > 0 {
					firstCC := strings.ToUpper(geo[:idx])
					if len(firstCC) == 2 {
						addr := slug + ", " + firstCC
						if addressvalidate.IsPlausibleAddress(addr) {
							return addr, geoAddressSourceZoneSlugCountry
						}
					}
				}
			}
		}
	}

	// 3. /loc/{country}[/{level1}[/{level2}]]: build "city, CC" from canonical path when possible.
	if strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/loc/") {
		ccParsed, level1, level2, ok := taxonomy.ParseLocCanonicalPath(strings.TrimSpace(f.CanonicalPath))
		if ok {
			cc := strings.ToUpper(strings.TrimSpace(ccParsed))
			cityToken := level2
			if strings.TrimSpace(cityToken) == "" {
				cityToken = level1
			}
			city := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(cityToken)))
			if len(cc) == 2 && cc != "NA" && city != "" && !strings.EqualFold(city, "na") {
				addr := city + ", " + cc
				if addressvalidate.IsPlausibleAddress(addr) {
					return addr, geoAddressSourceLocPath
				}
			}
		}
	}

	// 4. Labels["en"] + country for DEPOSIT (e.g. "Permian Basin, US") — deposit names are place names, not company names.
	if f.IsDepositSite() {
		name := ""
		if f.Labels != nil {
			name = strings.TrimSpace(f.Labels["en"])
		}
		if name == "" && f.Identifiers != nil {
			name = strings.TrimSpace(f.Identifiers["shortName"])
		}
		if name != "" {
			cc := extractCountryFromFragment(f)
			if cc != "" {
				addr := name + ", " + cc
				if addressvalidate.IsPlausibleAddress(addr) {
					return addr, geoAddressSourceDepositNameCountry
				}
			}
		}
	}

	// 5. Last-resort fallback for entity-centric types:
	// geocode "entity name, CC" when no structured address/homeLocation is available.
	// This is intentionally limited to types where HQ/site geocoding is still useful.
	if isEntityNameGeoFallbackType(f.Class) {
		cc := extractCountryFromFragment(f)
		if cc != "" {
			names := make([]string, 0, 3)
			if f.Labels != nil {
				if v := strings.TrimSpace(f.Labels["en"]); v != "" {
					names = append(names, v)
				}
			}
			if f.Identifiers != nil {
				if v := strings.TrimSpace(f.Identifiers["shortName"]); v != "" {
					names = append(names, v)
				}
				if v := strings.TrimSpace(f.Identifiers["legalName"]); v != "" {
					names = append(names, v)
				}
			}
			seen := map[string]struct{}{}
			for _, n := range names {
				key := strings.ToLower(strings.TrimSpace(n))
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				addr := n + ", " + cc
				if addressvalidate.IsPlausibleAddress(addr) {
					return addr, geoAddressSourceEntityNameCountry
				}
			}
		}
	}
	return "", ""
}

// BuildLastSegmentAddressCandidates builds geocoding candidates from the last segment of the canonical path.
// Useful when the path slug itself identifies a place (e.g. "north-sinai-insurgency-zone" → North Sinai).
// Returns candidates for zone, geo-region, deposit, and other geo-relevant types when the slug looks place-like.
func BuildLastSegmentAddressCandidates(f *hmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	if _, ok := GeoRelevantTypes[f.Class]; !ok {
		return nil
	}
	if f.Class == FragmentTypeMarket {
		return nil
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	if cp == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return nil
	}
	slug := strings.TrimSpace(parts[len(parts)-1])
	if slug == "" || strings.EqualFold(slug, "na") {
		return nil
	}
	// Humanize: north-sinai-insurgency-zone → North Sinai Insurgency Zone
	humanized := strings.Title(strings.ToLower(strings.NewReplacer("-", " ", "_", " ").Replace(slug)))
	if humanized == "" {
		return nil
	}
	cc := extractCountryFromFragment(f)
	if cc == "" {
		cc = extractCountryFromCanonicalPath(cp)
	}

	out := make([]string, 0, 6)
	seen := map[string]struct{}{}
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

	for _, hint := range InferPlaceHintCandidates(f) {
		if cc != "" {
			add(hint + ", " + cc)
		}
		add(hint)
	}

	if cc != "" {
		add(humanized + ", " + cc)
	}
	add(humanized)

	// Tail variants help facility/org slugs that end with a real place name:
	// eco-resource-kz-almaty -> Almaty, KZ
	slugParts := strings.Split(slug, "-")
	if len(slugParts) >= 2 {
		tail := strings.Title(strings.ToLower(strings.Join(slugParts[len(slugParts)-2:], " ")))
		if tail != "" {
			if cc != "" {
				add(tail + ", " + cc)
			}
			add(tail)
		}
	}
	if len(slugParts) >= 1 {
		last := strings.Title(strings.ToLower(slugParts[len(slugParts)-1]))
		if last != "" && len(last) >= 3 {
			if cc != "" {
				add(last + ", " + cc)
			}
			add(last)
		}
	}
	// Prefix variants remain useful for geo-first names like north-sinai-insurgency-zone,
	// but they should be lower priority than suffix place guesses.
	if len(slugParts) >= 2 {
		short := strings.Title(strings.ToLower(strings.Join(slugParts[:2], " ")))
		if short != "" {
			if cc != "" {
				add(short + ", " + cc)
			}
			add(short)
		}
	}
	return out
}

// InferPlaceHintCandidates extracts conservative city/site hints from fragment names and slugs.
// It is intentionally biased toward trailing geographic clues like "... Wuxi", "... Huizhou Factory",
// "... Eilat", "... Binh Duong Factory". Generic company/facility suffixes are stripped first.
func InferPlaceHintCandidates(f *hmodels.HorizonFragment) []string {
	if f == nil {
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
	if f.HomeLocation != nil {
		add(strings.TrimSpace(f.HomeLocation.Name))
	}
	for _, raw := range geoHintSourceCandidates(f) {
		for _, hint := range inferPlaceHintsFromText(raw) {
			add(hint)
		}
	}
	return out
}

func geoHintSourceCandidates(f *hmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, 4)
	if f.Labels != nil {
		if v := strings.TrimSpace(f.Labels["en"]); v != "" {
			out = append(out, v)
		}
	}
	if f.Identifiers != nil {
		for _, key := range []string{"legalName", "shortName"} {
			if v := strings.TrimSpace(f.Identifiers[key]); v != "" {
				out = append(out, v)
			}
		}
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	if cp != "" {
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
	add := func(parts ...string) {
		val := strings.TrimSpace(strings.Join(parts, " "))
		if val == "" {
			return
		}
		val = titleGeoHint(val)
		key := strings.ToLower(val)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, val)
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
		add(trimmed[0], trimmed[1])
	}
	last := trimmed[len(trimmed)-1]
	if len(trimmed) >= 2 && len(last) >= 4 && !isGenericGeoNoiseToken(last) {
		add(last)
	}
	if len(trimmed) >= 2 && isDirectionalGeoToken(trimmed[len(trimmed)-2]) && len(trimmed[len(trimmed)-1]) >= 3 {
		add(trimmed[len(trimmed)-2], trimmed[len(trimmed)-1])
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
		"fulfillment", "distribution", "logistics", "general", "production", "processing", "manufacturing",
		"showroom", "warehouse", "warehousing", "depot", "storage", "retail", "consumer", "industrial",
		"automotive", "personal", "care", "commercial", "operations", "sales", "border", "crossing",
		"checkpoint", "post", "supermarket", "hypermarket", "store", "shop", "outlet":
		return true
	default:
		return false
	}
}

func titleGeoHint(raw string) string {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

// BuildFacilityAddressCandidates builds additional facility geocoding candidates using parent-org context.
// The first candidate (when present) is always BuildAddressFromFragment(fragment).
func BuildFacilityAddressCandidates(fragment *hmodels.HorizonFragment, parent *hmodels.HorizonFragment) []string {
	if fragment == nil || fragment.Class != FragmentTypeFacility {
		return nil
	}
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
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

	if addr, source := BuildAddressFromFragmentWithSource(fragment); addr != "" && source != geoAddressSourceEntityNameCountry {
		add(addr)
	}

	cc := extractCountryFromFragment(fragment)
	if cc == "" && parent != nil {
		cc = extractCountryFromFragment(parent)
	}
	place := ""
	if fragment.HomeLocation != nil {
		place = strings.TrimSpace(fragment.HomeLocation.Name)
	}
	for _, cityKey := range []string{"city", "locality", "town", "municipality", "settlement"} {
		if place == "" && fragment.Identifiers != nil {
			place = strings.TrimSpace(fragment.Identifiers[cityKey])
		}
	}
	if place != "" && cc != "" {
		add(place + ", " + cc)
	}

	facilityNames := collectGeoCandidateNames(fragment)
	if len(facilityNames) == 0 {
		if fallback := facilityNameFromCanonicalPath(fragment.CanonicalPath); fallback != "" {
			facilityNames = append(facilityNames, fallback)
		}
	}
	parentNames := collectGeoCandidateNames(parent)

	for _, fn := range facilityNames {
		if cc != "" {
			add(fn + ", " + cc)
		}
		for _, pn := range parentNames {
			if cc != "" {
				add(fn + ", " + pn + ", " + cc)
			}
			add(fn + ", " + pn)
			if place != "" && cc != "" {
				add(place + ", " + pn + ", " + cc)
			}
		}
	}
	return out
}

func collectGeoCandidateNames(f *hmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(v string) {
		s := strings.TrimSpace(v)
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
	if f.Labels != nil {
		add(f.Labels["en"])
	}
	if f.Identifiers != nil {
		add(f.Identifiers["shortName"])
		add(f.Identifiers["legalName"])
	}
	if f.HomeLocation != nil {
		add(f.HomeLocation.Name)
	}
	return out
}

func facilityNameFromCanonicalPath(cp string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(cp), "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	name := strings.TrimSpace(parts[2])
	if name == "" || strings.EqualFold(name, "na") {
		return ""
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.TrimSpace(name)
}

func isEntityNameGeoFallbackType(t FragmentType) bool {
	// Legacy PROGRAM is org/program under AH and is therefore covered by FragmentTypeOrganization.
	switch t {
	case FragmentTypeOrganization,
		FragmentTypeFacility,
		FragmentTypeCommunity:
		return true
	default:
		return false
	}
}

// ApplyResolvedCoordinates updates fragment coordinates and ISO6709 from a trusted result.
// Returns true when coordinates changed materially.
func ApplyResolvedCoordinates(fragment *hmodels.HorizonFragment, lat, lon float64) bool {
	if fragment == nil || !hmodels.IsRealGeoCoordinate(lat, lon) {
		return false
	}
	changed := fragment.Coordinates == nil ||
		math.Abs(fragment.Coordinates.Lat-lat) > 0.000001 ||
		math.Abs(fragment.Coordinates.Lon-lon) > 0.000001
	fragment.Coordinates = &hmodels.Coordinates{Lat: lat, Lon: lon}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", lat, lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) != iso {
		fragment.HomeLocation.ISO6709 = &iso
	}
	return changed
}

// RecordGeoCoordinatesAttribution writes stable, queryable provenance for coordinate resolution.
func RecordGeoCoordinatesAttribution(fragment *hmodels.HorizonFragment, attr GeoCoordinatesAttribution) {
	if fragment == nil {
		return
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	set := func(key, value string) {
		if s := strings.TrimSpace(value); s != "" {
			fragment.Identifiers[key] = s
		}
	}
	set("geoCoordinatesMethod", attr.Method)
	set("geoCoordinatesProvider", attr.Provider)
	set("geoCoordinatesQuery", attr.Query)
	set("geoCoordinatesFormattedAddress", attr.FormattedAddress)
	set("geoCoordinatesPlaceId", attr.PlaceID)
	set("geoCoordinatesCountryAlpha2", strings.ToUpper(attr.CountryAlpha2))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

func isParentGeoPropagationType(t FragmentType) bool {
	// Legacy PROGRAM is org/program under AH; if the runtime cares about programs
	// here it should be re-introduced via a (Class, Subtype) check on the fragment.
	switch t {
	case FragmentTypeFacility, FragmentTypeCommunity:
		return true
	default:
		return false
	}
}

// extractCountryFromFragment gets ISO3166-1 alpha2 from fragment fields and context paths.
func extractCountryFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if f.HomeLocation != nil && f.HomeLocation.ISO3166_1 != nil {
		cc := normalizeISO2(*f.HomeLocation.ISO3166_1)
		if cc != "" {
			return cc
		}
	}
	if f.Identifiers != nil {
		for _, k := range []string{"iso3166_1", "countryAlpha2", "countryCode", "country_code", "country", "iso2"} {
			if cc := normalizeISO2(f.Identifiers[k]); cc != "" {
				return cc
			}
		}
	}
	if cc := extractCountryFromCanonicalPath(f.CanonicalPath); cc != "" {
		return cc
	}
	for _, ctxPath := range []string{f.ParentPath, f.ParentGovPath, f.ParentMarketPath, f.AffiliatedOrgPath} {
		if cc := extractCountryFromCanonicalPath(ctxPath); cc != "" {
			return cc
		}
	}
	for _, seg := range f.MarketSegments {
		parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(seg)), "/"), "/")
		if len(parts) >= 4 && parts[0] == "segment" {
			if cc := normalizeISO2(parts[len(parts)-1]); cc != "" {
				return cc
			}
		}
	}
	return ""
}

func extractCountryFromCanonicalPath(rawPath string) string {
	cp := strings.TrimSpace(rawPath)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.ToLower(cp), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community", "fac":
		return normalizeISO2(parts[1])
	case "zone":
		// /zone/{geo}/{condition_type}/{slug}: geo can be ISO2 (tr, sy) or composite (tr-sy)
		geo := strings.TrimSpace(strings.ToLower(parts[1]))
		if len(geo) == 2 && geo != "na" {
			return strings.ToUpper(geo)
		}
		if idx := strings.Index(geo, "-"); idx > 0 {
			firstCC := geo[:idx]
			if len(firstCC) == 2 {
				return strings.ToUpper(firstCC)
			}
		}
		return ""
	default:
		return ""
	}
}

func normalizeISO2(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if len(s) != 2 || s == "NA" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return ""
		}
	}
	return s
}
