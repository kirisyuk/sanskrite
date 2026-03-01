package enrich

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// GeoRelevantTypes are fragment types where coordinates are useful for map rendering.
// Gov bodies use ORGANIZATION type with /gov/ canonical path.
// GEO_REGION and DEPOSIT are geo-anchored entities (regions, mines, basins).
var GeoRelevantTypes = map[hmodels.FragmentType]struct{}{
	hmodels.FragmentTypeOrganization: {},
	hmodels.FragmentTypeFacility:     {},
	hmodels.FragmentTypeCommunity:    {},
	hmodels.FragmentTypeProgram:      {},
	hmodels.FragmentTypeGeoRegion:    {},
	hmodels.FragmentTypeDeposit:      {},
	hmodels.FragmentTypeEvent:       {},
	hmodels.FragmentTypeMarket:      {},
	hmodels.FragmentTypeAffectedZone: {},
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

	if _, ok := GeoRelevantTypes[fragment.Type]; !ok {
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
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 4 || parts[0] != "loc" {
		return false, nil
	}
	cc := normalizeISO2(parts[1])
	adm := strings.TrimSpace(parts[2])
	city := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3])))
	if cc == "" || city == "" || strings.EqualFold(city, "na") {
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
	if !strings.HasPrefix(cp, "/zone/") || fragment.Type != hmodels.FragmentTypeAffectedZone {
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
	fragment.Coordinates = &hmodels.Coordinates{Lat: lat, Lon: lon}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", lat, lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) == "" {
		fragment.HomeLocation.ISO6709 = &iso
	}
	return true
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
	if !isParentGeoPropagationType(fragment.Type) {
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
	if _, ok := GeoRelevantTypes[fragment.Type]; !ok {
		return false, nil
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false, nil
	}

	addr := BuildAddressFromFragment(fragment)
	if addr == "" || !addressvalidate.IsPlausibleAddress(addr) {
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

	// Apply coordinates
	fragment.Coordinates = &hmodels.Coordinates{Lat: result.Lat, Lon: result.Lon}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", result.Lat, result.Lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) == "" {
		fragment.HomeLocation.ISO6709 = &iso
	}
	return true, nil
}

// BuildAddressFromFragment constructs an address string from fragment fields.
// Exported for tooling (e.g. horizon_backfill_coordinates).
// Only returns strings that look like real addresses (street, city, place+country).
// Does NOT use company names (label/legalName) — those are not addresses.
//
// Priority: explicit address identifiers > homeLocation (place name + country).
func BuildAddressFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
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
					return addr
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
						return addr
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
				return addr
			}
		}
		// For geo-centric fragment kinds, a plain place name can still be geocoded.
		if name != "" && (f.Type == hmodels.FragmentTypeGeoRegion || f.Type == hmodels.FragmentTypeEvent || f.Type == hmodels.FragmentTypeAffectedZone) {
			if addressvalidate.IsPlausibleAddress(name) {
				return name
			}
		}
	}

	// 2b. /zone/{geo}/{condition_type}/{slug}: build "slug, CC" from canonical path for AFFECTED_ZONE.
	if f.Type == hmodels.FragmentTypeAffectedZone && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/zone/") {
		parts := strings.Split(strings.Trim(strings.TrimSpace(f.CanonicalPath), "/"), "/")
		if len(parts) >= 4 && parts[0] == "zone" {
			geo := strings.TrimSpace(parts[1])
			slug := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3])))
			if slug != "" && !strings.EqualFold(slug, "na") {
				cc := extractCountryFromCanonicalPath("/zone/" + geo) // uses zone geo handling below
				if cc != "" {
					addr := slug + ", " + cc
					if addressvalidate.IsPlausibleAddress(addr) {
						return addr
					}
				}
				// Fallback: slug + geo as-is (for composite tr-sy use first part for geocoding)
				if len(geo) == 2 && geo != "na" {
					addr := slug + ", " + strings.ToUpper(geo)
					if addressvalidate.IsPlausibleAddress(addr) {
						return addr
					}
				}
				if idx := strings.Index(geo, "-"); idx > 0 {
					firstCC := strings.ToUpper(geo[:idx])
					if len(firstCC) == 2 {
						addr := slug + ", " + firstCC
						if addressvalidate.IsPlausibleAddress(addr) {
							return addr
						}
					}
				}
			}
		}
	}

	// 3. /loc/{cc}/{adm1}/{city}: build "city, CC" from canonical path for geocoding fallback when GeoNames unavailable.
	if strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/loc/") {
		parts := strings.Split(strings.Trim(strings.TrimSpace(f.CanonicalPath), "/"), "/")
		if len(parts) >= 4 && parts[0] == "loc" {
			cc := strings.ToUpper(strings.TrimSpace(parts[1]))
			city := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3])))
			if len(cc) == 2 && cc != "NA" && city != "" && !strings.EqualFold(city, "na") {
				addr := city + ", " + cc
				if addressvalidate.IsPlausibleAddress(addr) {
					return addr
				}
			}
		}
	}

	// 4. Labels["en"] + country for DEPOSIT (e.g. "Permian Basin, US") — deposit names are place names, not company names.
	if f.Type == hmodels.FragmentTypeDeposit {
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
					return addr
				}
			}
		}
	}

	// 5. Last-resort fallback for entity-centric types:
	// geocode "entity name, CC" when no structured address/homeLocation is available.
	// This is intentionally limited to types where HQ/site geocoding is still useful.
	if isEntityNameGeoFallbackType(f.Type) {
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
					return addr
				}
			}
		}
	}
	return ""
}

// BuildFacilityAddressCandidates builds additional facility geocoding candidates using parent-org context.
// The first candidate (when present) is always BuildAddressFromFragment(fragment).
func BuildFacilityAddressCandidates(fragment *hmodels.HorizonFragment, parent *hmodels.HorizonFragment) []string {
	if fragment == nil || fragment.Type != hmodels.FragmentTypeFacility {
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

	add(BuildAddressFromFragment(fragment))

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

func isEntityNameGeoFallbackType(t hmodels.FragmentType) bool {
	switch t {
	case hmodels.FragmentTypeOrganization,
		hmodels.FragmentTypeFacility,
		hmodels.FragmentTypeCommunity,
		hmodels.FragmentTypeProgram:
		return true
	default:
		return false
	}
}

func isParentGeoPropagationType(t hmodels.FragmentType) bool {
	switch t {
	case hmodels.FragmentTypeFacility, hmodels.FragmentTypeCommunity, hmodels.FragmentTypeProgram:
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
	case "org", "gov", "loc", "market", "community":
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
