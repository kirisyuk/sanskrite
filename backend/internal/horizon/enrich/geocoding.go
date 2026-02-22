package enrich

import (
	"context"
	"fmt"
	"strings"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// GeoRelevantTypes are fragment types where coordinates are useful for map rendering.
// Gov bodies use ORGANIZATION type with /gov/ canonical path.
var GeoRelevantTypes = map[hmodels.FragmentType]struct{}{
	hmodels.FragmentTypeOrganization: {},
	hmodels.FragmentTypeFacility:     {},
	hmodels.FragmentTypeCommunity:   {},
	hmodels.FragmentTypeProgram:     {},
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
	if fragment.Coordinates != nil && hmodels.IsValidCoordinates(fragment.Coordinates.Lat, fragment.Coordinates.Lon) {
		return false, nil
	}
	if hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false, nil
	}

	addr := BuildAddressFromFragment(fragment)
	if addr == "" {
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
// Priority: explicit address identifiers > homeLocation + name > canonical path + label.
func BuildAddressFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}

	// Explicit address fields (from OpenCorporates, Wikidata P969, etc.)
	for _, k := range []string{"registeredAddress", "address", "streetAddress", "street_address", "registered_address"} {
		if f.Identifiers != nil {
			if v := strings.TrimSpace(f.Identifiers[k]); v != "" {
				// Optionally append country for disambiguation
				if cc := extractCountryFromFragment(f); cc != "" {
					return v + ", " + cc
				}
				return v
			}
		}
	}

	// homeLocation.name + country
	if f.HomeLocation != nil {
		name := strings.TrimSpace(f.HomeLocation.Name)
		cc := ""
		if f.HomeLocation.ISO3166_1 != nil {
			cc = strings.TrimSpace(*f.HomeLocation.ISO3166_1)
		}
		if name != "" || cc != "" {
			if name != "" && cc != "" {
				return name + ", " + cc
			}
			if name != "" {
				return name
			}
			return cc
		}
	}

	// Fallback: label/legalName + country from canonical path
	label := strings.TrimSpace(f.Labels["en"])
	if label == "" && f.Identifiers != nil {
		label = strings.TrimSpace(f.Identifiers["legalName"])
	}
	if label == "" && f.Identifiers != nil {
		label = strings.TrimSpace(f.Identifiers["shortName"])
	}
	cc := extractCountryFromFragment(f)
	if label != "" && cc != "" {
		return label + ", " + cc
	}
	if cc != "" {
		return cc
	}
	return ""
}

// extractCountryFromFragment gets ISO3166-1 alpha2 from fragment (canonical path or homeLocation).
func extractCountryFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if f.HomeLocation != nil && f.HomeLocation.ISO3166_1 != nil {
		cc := strings.TrimSpace(*f.HomeLocation.ISO3166_1)
		if cc != "" {
			return strings.ToUpper(cc)
		}
	}
	// From canonical path: /org/{iso2}/..., /gov/{iso2}/..., /fac/{org}/...
	cp := strings.TrimSpace(f.CanonicalPath)
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 2 {
		// /org/ae/..., /gov/us/..., /fac/na/org-slug - second segment is often country or org
		second := strings.ToLower(strings.TrimSpace(parts[1]))
		if len(second) == 2 && second != "na" {
			return strings.ToUpper(second)
		}
	}
	return ""
}
