package scout

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type fakeGeoNamesCoordinates struct{}

type fakeGeocodingClient struct {
	results map[string]*coreontology.GeocodingResult
}

func (f *fakeGeocodingClient) Geocode(_ context.Context, address string) (*coreontology.GeocodingResult, error) {
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
		Type:          models.FragmentTypeGeoRegion,
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
		Type:          models.FragmentTypeFacility,
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
		Type:          models.FragmentTypeFacility,
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

func TestPreCreateValidator_RevalidatesOrgGeoMismatchViaGeocoding(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil).WithGeocodingClient(&fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Kazatomprom, Astana": {
				Lat:              51.1694,
				Lon:              71.4491,
				FormattedAddress: "Astana, Kazakhstan",
				PlaceID:          "google-place-astana",
			},
		},
	})

	cc := "US"
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
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
		Type:          models.FragmentTypeOrganization,
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
