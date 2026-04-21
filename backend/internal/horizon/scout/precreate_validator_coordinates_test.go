package scout

import (
	"context"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"testing"
)

type fakeGeoNamesCoordinates struct{}

type fakeGeocodingClient struct {
	results map[string]*coreontology.GeocodingResult
	calls   []string
}

func (f *fakeGeocodingClient) Geocode(_ context.Context, address string) (*coreontology.GeocodingResult, error) {
	f.calls = append(f.calls, address)
	if f == nil || f.results == nil {
		return nil, nil
	}
	if res, ok := f.results[address]; ok {
		return res, nil
	}
	return nil, nil
}

func (f *fakeGeoNamesCoordinates) Search(ctx context.Context, query string, maxResults int, opts coreontology.GeoNamesSearchOptions) (*coreontology.GeoNamesSearchResponse, error) {
	if query == "abu dhabi" && opts.Country == "AE" && opts.FeatureClass == "P" {
		return &coreontology.GeoNamesSearchResponse{
			Query: query,
			Results: []coreontology.GeoNamesPlace{
				{
					GeoNameID:   292968,
					Name:        "Abu Dhabi",
					CountryCode: "AE",
					AdminCode1:  "AZ",
					Lat:         "24.46667",
					Lng:         "54.36667",
					Population:  1800000,
				},
			},
		}, nil
	}
	return &coreontology.GeoNamesSearchResponse{Query: query}, nil
}

func (f *fakeGeoNamesCoordinates) GetPlace(ctx context.Context, geonameID int) (*coreontology.GeoNamesPlace, error) {
	return nil, nil
}

func (f *fakeGeoNamesCoordinates) GetHierarchy(ctx context.Context, geonameID int) (*coreontology.GeoNamesHierarchyResponse, error) {
	return nil, nil
}

func (f *fakeGeoNamesCoordinates) GetCountryCode(ctx context.Context, lat, lon float64) (string, error) {
	return "", nil
}

func TestPreCreateValidator_AttachesGeoNamesIDWithoutCoordinates(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeoNamesClient(&fakeGeoNamesCoordinates{})

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassGeo,
		CanonicalPath: "/loc/ae/az/abu-dhabi",
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.Coordinates != nil {
		t.Fatalf("expected coordinates to remain unset, got %+v", frag.Coordinates)
	}
	if got := frag.Identifiers["geonamesId"]; got != "292968" {
		t.Fatalf("expected geonamesId=292968, got %q", got)
	}
}

func TestPreCreateValidator_UpgradesFacilityPathFromHomeLocation(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	cc := "AE"
	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/jebel-ali-port",
		HomeLocation: &models.Location{
			Name:      "Dubai",
			ISO3166_1: &cc,
		},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.CanonicalPath != "/fac/ae/jebel-ali-port" {
		t.Fatalf("expected upgraded facility path, got %q", frag.CanonicalPath)
	}
}

func TestPreCreateValidator_RevalidatesFacilityGeoMismatchViaGeocoding(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Giga Shanghai, Shanghai": {
				Lat:              31.2304,
				Lon:              121.4737,
				FormattedAddress: "Pudong, Shanghai, China",
				PlaceID:          "google-place-shanghai",
			},
		},
	})

	cc := "US"
	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/giga-shanghai",
		Labels:        map[string]string{"en": "Giga Shanghai"},
		HomeLocation: &models.Location{
			Name:      "Shanghai",
			ISO3166_1: &cc,
		},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*frag.HomeLocation.ISO3166_1) != "CN" {
		t.Fatalf("expected corrected facility ISO2=CN, got %+v", frag.HomeLocation)
	}
	if frag.CanonicalPath != "/fac/cn/giga-shanghai" {
		t.Fatalf("expected upgraded facility path, got %q", frag.CanonicalPath)
	}
	if frag.Coordinates == nil {
		t.Fatalf("expected coordinates to be attached from revalidation")
	}
}

func TestPreCreateValidator_RevalidatesHighSignalFacilityFromPlainName(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Port of Charleston": {
				Lat:              32.7765,
				Lon:              -79.9311,
				FormattedAddress: "Port of Charleston, Charleston, SC, USA",
				PlaceID:          "google-place-charleston-port",
			},
		},
	})

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/port-of-charleston",
		Labels:        map[string]string{"en": "Port of Charleston"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.CanonicalPath != "/fac/us/port-of-charleston" {
		t.Fatalf("expected upgraded facility path, got %q", frag.CanonicalPath)
	}
}

func TestPreCreateValidator_RevalidatesOrgGeoMismatchViaGeocoding(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Kazatomprom, Astana": {
				Lat:              51.1694,
				Lon:              71.4491,
				FormattedAddress: "Astana, Kazakhstan",
				PlaceID:          "google-place-astana",
				Provider:         coreontology.GeocodingProviderGoogle,
			},
		},
	})

	cc := "US"
	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/org/na/kazatomprom",
		Labels:        map[string]string{"en": "Kazatomprom"},
		HomeLocation: &models.Location{
			Name:      "Astana",
			ISO3166_1: &cc,
		},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*frag.HomeLocation.ISO3166_1) != "KZ" {
		t.Fatalf("expected corrected org ISO2=KZ, got %+v", frag.HomeLocation)
	}
	if frag.CanonicalPath != "/org/kz/kazatomprom" {
		t.Fatalf("expected upgraded org path, got %q", frag.CanonicalPath)
	}
	if frag.Identifiers["geoCoordinatesProvider"] != coreontology.GeocodingProviderGoogle {
		t.Fatalf("expected geoCoordinatesProvider=%q, got %q", coreontology.GeocodingProviderGoogle, frag.Identifiers["geoCoordinatesProvider"])
	}
}

func TestPreCreateValidator_RevalidationOverwritesExistingWrongCoordinates(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Kazatomprom, Astana": {
				Lat:              51.1694,
				Lon:              71.4491,
				FormattedAddress: "Astana, Kazakhstan",
				PlaceID:          "google-place-astana",
				Provider:         coreontology.GeocodingProviderGoogle,
			},
		},
	})

	cc := "KZ"
	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/org/kz/kazatomprom",
		Labels:        map[string]string{"en": "Kazatomprom"},
		Coordinates:   &models.Coordinates{Lat: 0.1, Lon: 0.2},
		HomeLocation: &models.Location{
			Name:      "Astana",
			ISO3166_1: &cc,
		},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.Coordinates == nil || frag.Coordinates.Lat != 51.1694 || frag.Coordinates.Lon != 71.4491 {
		t.Fatalf("expected revalidated coordinates, got %+v", frag.Coordinates)
	}
	if frag.Identifiers["geoVerifiedProvider"] != coreontology.GeocodingProviderGoogle {
		t.Fatalf("expected geoVerifiedProvider=%q, got %q", coreontology.GeocodingProviderGoogle, frag.Identifiers["geoVerifiedProvider"])
	}
}

func TestPreCreateValidator_BlocksForbiddenFacilityPlaceholder(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/general-plant",
		Labels:        map[string]string{"en": "General Production"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected validation result")
	}
	if res.IsValid {
		t.Fatalf("expected forbidden placeholder to be blocked")
	}
	if !strings.Contains(strings.ToLower(res.BlockReason), "forbidden /fac/ placeholder slug") {
		t.Fatalf("unexpected block reason: %q", res.BlockReason)
	}
}

func TestPreCreateValidator_BlocksMalformedSyntheticFacilityPlaceholder(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/asml-japan-co-ltd/general-production",
		Labels:        map[string]string{"en": "General Production"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected validation result")
	}
	if res.IsValid {
		t.Fatalf("expected malformed synthetic facility placeholder to be blocked")
	}
	if !strings.Contains(strings.ToLower(res.BlockReason), "forbidden /fac/ placeholder slug") {
		t.Fatalf("unexpected block reason: %q", res.BlockReason)
	}
}

func TestPreCreateValidator_RepairsSyntheticFacilityPlaceholderFromSpecificLabel(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/general-production",
		Labels:        map[string]string{"en": "Canon Oita Manufacturing Facility"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.CanonicalPath != "/fac/na/canon-oita-manufacturing-facility" {
		t.Fatalf("expected repaired facility path, got %q", frag.CanonicalPath)
	}
}

func TestPreCreateValidator_RepairsSyntheticFacilityPlaceholderUsingCountryHintInLabel(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassSite,
		CanonicalPath: "/fac/na/general-production",
		Labels:        map[string]string{"en": "AEON Malaysia Distribution"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.CanonicalPath != "/fac/my/aeon-malaysia-distribution" {
		t.Fatalf("expected repaired facility path with country hint, got %q", frag.CanonicalPath)
	}
}

func TestPreCreateValidator_BlocksSyntheticOrgPlaceholder(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/org/na/retailer",
		Labels:        map[string]string{"en": "Retailer"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected validation result")
	}
	if res.IsValid {
		t.Fatalf("expected synthetic org placeholder to be blocked")
	}
	if !strings.Contains(strings.ToLower(res.BlockReason), "forbidden synthetic /org/ placeholder slug") {
		t.Fatalf("unexpected block reason: %q", res.BlockReason)
	}
}

func TestPreCreateValidator_BlocksSyntheticGovPlaceholder(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/gov/na/ministry-of",
		Labels:        map[string]string{"en": "Ministry Of"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected validation result")
	}
	if res.IsValid {
		t.Fatalf("expected synthetic gov placeholder to be blocked")
	}
	if !strings.Contains(strings.ToLower(res.BlockReason), "forbidden synthetic /gov/ placeholder slug") {
		t.Fatalf("unexpected block reason: %q", res.BlockReason)
	}
}

func TestPreCreateValidator_RepairsSyntheticGovPlaceholderWithKnownGeo(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/gov/qa/na",
		Labels:        map[string]string{"en": "Government of Qatar"},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected validation result")
	}
	if !res.IsValid {
		t.Fatalf("expected synthetic gov placeholder with known geo to be repaired")
	}
	if frag.CanonicalPath != "/gov/qa/government" {
		t.Fatalf("expected repaired gov path, got %q", frag.CanonicalPath)
	}
}

func TestPreCreateValidator_DoesNotOverrideGovPathOnAmbiguousCityOnly(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"National Border Service (SENAFRONT), Panama City": {
				Lat:              30.1588,
				Lon:              -85.6602,
				FormattedAddress: "Panama City, FL, USA",
				PlaceID:          "google-place-panama-city-fl",
			},
			"National Border Service (SENAFRONT), Panama City, Panama": {
				Lat:              8.9824,
				Lon:              -79.5199,
				FormattedAddress: "Panama City, Panama",
				PlaceID:          "google-place-panama-city-pa",
			},
		},
	})

	cc := "PA"
	frag := &models.HorizonFragment{
		Class:          ah.FragmentClassOrg,
		CanonicalPath: "/gov/pa/senafront",
		Labels:        map[string]string{"en": "National Border Service (SENAFRONT)"},
		HomeLocation: &models.Location{
			Name:      "Panama City",
			ISO3166_1: &cc,
		},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil || !res.IsValid {
		t.Fatalf("expected valid result, got %+v", res)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*frag.HomeLocation.ISO3166_1) != "PA" {
		t.Fatalf("expected gov ISO2 to remain PA, got %+v", frag.HomeLocation)
	}
	if frag.CanonicalPath != "/gov/pa/senafront" {
		t.Fatalf("expected gov path to remain stable, got %q", frag.CanonicalPath)
	}
}
