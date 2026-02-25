package enrich

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// FactCoordinateEnricher resolves coordinates for ontology facts using GeoNames, Geocoding, Wikidata.
// Same 3-service approach as fragment coordinate backfill.
type FactCoordinateEnricher struct {
	geonames   ontology.GeoNamesClientInterface
	geocoding  ontology.GeocodingClientInterface
	fragments  FragmentLookupForCoordinates // optional: resolve /org/... via fragment -> Wikidata
}

// FragmentLookupForCoordinates fetches fragment by canonical path for Wikidata/address fallback.
type FragmentLookupForCoordinates interface {
	GetByCanonicalPath(ctx context.Context, path string) (*hmodels.HorizonFragment, error)
}

// NewFactCoordinateEnricher creates an enricher with the given clients.
func NewFactCoordinateEnricher(
	geonames ontology.GeoNamesClientInterface,
	geocoding ontology.GeocodingClientInterface,
) *FactCoordinateEnricher {
	return &FactCoordinateEnricher{
		geonames:  geonames,
		geocoding: geocoding,
	}
}

// WithFragmentsRepo wires fragment lookup for /org/... -> address/Wikidata fallback.
func (e *FactCoordinateEnricher) WithFragmentsRepo(lookup FragmentLookupForCoordinates) *FactCoordinateEnricher {
	e.fragments = lookup
	return e
}

// Enrich resolves coordinates for a fact when LocationPath, EventAddress, EventPlace, or canonicalPaths provide location hints.
// Returns true if coordinates were set.
func (e *FactCoordinateEnricher) Enrich(ctx context.Context, f *coremodels.OntologyFact) (bool, error) {
	if e == nil || f == nil {
		return false, nil
	}
	if hasValidCoordinates(f) {
		return false, nil
	}

	// 0. EventISO6709 from LLM (coordinates already provided)
	if f.EventISO6709 != nil {
		s := strings.TrimSpace(*f.EventISO6709)
		if s != "" {
			if lat, lon, ok := hmodels.ParseISO6709Coordinates(s); ok && hmodels.IsRealGeoCoordinate(lat, lon) {
				f.Coordinates = &coremodels.FactCoordinates{Lat: lat, Lon: lon}
				return true, nil
			}
		}
	}

	// 1. LocationPath /loc/cc/admin/city -> GeoNames
	if e.geonames != nil {
		lp := ""
		if f.LocationPath != nil {
			lp = strings.TrimSpace(*f.LocationPath)
		}
		if strings.HasPrefix(lp, "/loc/") {
			if changed, _ := enrichFromGeoNames(ctx, e.geonames, f); changed {
				return true, nil
			}
		}
	}

	// 2. Build address from fact (Title + country) -> Geocoding
	if e.geocoding != nil {
		addr := buildAddressFromFact(f)
		if addr != "" {
			result, err := e.geocoding.Geocode(ctx, addr)
			if err == nil && result != nil && hmodels.IsValidCoordinates(result.Lat, result.Lon) &&
				hmodels.IsRealGeoCoordinate(result.Lat, result.Lon) {
				f.Coordinates = &coremodels.FactCoordinates{Lat: result.Lat, Lon: result.Lon}
				return true, nil
			}
		}
	}

	// 3. CanonicalPaths /org/..., /gov/... -> lookup fragment -> use fragment coordinates or address
	if e.fragments != nil {
		for _, cp := range f.CanonicalPaths {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				continue
			}
			if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") || strings.HasPrefix(cp, "/fac/") {
				frag, err := e.fragments.GetByCanonicalPath(ctx, cp)
				if err != nil || frag == nil {
					continue
				}
				// Use fragment coordinates if available
				if frag.Coordinates != nil && hmodels.IsValidCoordinates(frag.Coordinates.Lat, frag.Coordinates.Lon) &&
					hmodels.IsRealGeoCoordinate(frag.Coordinates.Lat, frag.Coordinates.Lon) {
					f.Coordinates = &coremodels.FactCoordinates{Lat: frag.Coordinates.Lat, Lon: frag.Coordinates.Lon}
					return true, nil
				}
				// Fallback: geocode fragment address
				if e.geocoding != nil {
					addr := BuildAddressFromFragment(frag)
					if addr != "" {
						result, err := e.geocoding.Geocode(ctx, addr)
						if err == nil && result != nil && hmodels.IsValidCoordinates(result.Lat, result.Lon) &&
							hmodels.IsRealGeoCoordinate(result.Lat, result.Lon) {
							f.Coordinates = &coremodels.FactCoordinates{Lat: result.Lat, Lon: result.Lon}
							return true, nil
						}
					}
				}
				break // one path is enough
			}
		}
	}

	return false, nil
}

func hasValidCoordinates(f *coremodels.OntologyFact) bool {
	if f == nil || f.Coordinates == nil {
		return false
	}
	return hmodels.IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon) &&
		hmodels.IsRealGeoCoordinate(f.Coordinates.Lat, f.Coordinates.Lon)
}

func enrichFromGeoNames(ctx context.Context, gn ontology.GeoNamesClientInterface, f *coremodels.OntologyFact) (bool, error) {
	if gn == nil || f == nil {
		return false, nil
	}
	lp := ""
	if f.LocationPath != nil {
		lp = strings.TrimSpace(*f.LocationPath)
	}
	if !strings.HasPrefix(lp, "/loc/") {
		return false, nil
	}
	parts := strings.Split(strings.Trim(lp, "/"), "/")
	if len(parts) != 4 || parts[0] != "loc" {
		return false, nil
	}
	cc := strings.ToUpper(strings.TrimSpace(parts[1]))
	adm := strings.TrimSpace(parts[2])
	city := strings.TrimSpace(parts[3])
	if len(cc) != 2 || strings.EqualFold(city, "na") || city == "" {
		return false, nil
	}

	cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(city)))
	if cityName == "" {
		return false, nil
	}

	opts := ontology.GeoNamesSearchOptions{Country: cc, FeatureClass: "P", NameEquals: true}
	if len(adm) == 2 && adm[0] >= 'A' && adm[0] <= 'Z' {
		opts.AdminCode1 = strings.ToUpper(adm)
	}

	resp, err := gn.Search(ctx, cityName, 10, opts)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return false, err
	}

	var results []ontology.GeoNamesPlace
	for _, r := range resp.Results {
		if strings.EqualFold(strings.TrimSpace(r.CountryCode), cc) {
			results = append(results, r)
		}
	}
	if len(results) == 0 {
		return false, nil
	}

	best := results[0]
	for _, r := range results {
		if r.Population > best.Population {
			best = r
		}
	}
	if best.GeoNameID <= 0 {
		return false, nil
	}

	place, err := gn.GetPlace(ctx, best.GeoNameID)
	if err != nil || place == nil || place.Lat == "" || place.Lng == "" {
		return false, err
	}
	lat, errLat := parseFloat(place.Lat)
	lon, errLon := parseFloat(place.Lng)
	if errLat != nil || errLon != nil || !hmodels.IsValidCoordinates(lat, lon) ||
		!hmodels.IsRealGeoCoordinate(lat, lon) {
		return false, nil
	}
	f.Coordinates = &coremodels.FactCoordinates{Lat: lat, Lon: lon}
	return true, nil
}

// buildAddressFromFact builds an address string for geocoding from fact.
// Only uses EventAddress and EventPlace+country — never Title/Summary (often headlines).
func buildAddressFromFact(f *coremodels.OntologyFact) string {
	if f == nil {
		return ""
	}
	// 1. EventAddress from LLM (best for geocoding when it's a real address)
	if f.EventAddress != nil {
		addr := strings.TrimSpace(*f.EventAddress)
		if addr != "" && addressvalidate.IsPlausibleAddress(addr) {
			return addr
		}
	}
	// 2. EventPlace + country (e.g. "Dubai airport, AE")
	cc := extractCountryFromFact(f)
	if f.EventPlace != nil {
		place := strings.TrimSpace(*f.EventPlace)
		if place != "" && cc != "" {
			addr := place + ", " + cc
			if addressvalidate.IsPlausibleAddress(addr) {
				return addr
			}
		}
	}
	return ""
}

func extractCountryFromFact(f *coremodels.OntologyFact) string {
	if f == nil {
		return ""
	}
	// From LocationPath: /loc/ir/na/na -> ir
	if f.LocationPath != nil {
		lp := strings.TrimSpace(*f.LocationPath)
		if strings.HasPrefix(lp, "/loc/") {
			parts := strings.Split(strings.Trim(lp, "/"), "/")
			if len(parts) >= 2 {
				cc := strings.ToUpper(strings.TrimSpace(parts[1]))
				if len(cc) == 2 && cc != "NA" {
					return cc
				}
			}
		}
	}
	// From canonicalPaths: /org/ae/... -> ae
	for _, cp := range f.CanonicalPaths {
		cp = strings.TrimSpace(cp)
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 {
			second := strings.ToLower(strings.TrimSpace(parts[1]))
			if len(second) == 2 && second != "na" {
				return strings.ToUpper(second)
			}
		}
	}
	return ""
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}
