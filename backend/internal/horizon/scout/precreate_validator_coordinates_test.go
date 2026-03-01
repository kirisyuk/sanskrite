package scout

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type fakeGeoNamesCoordinates struct{}

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

func TestPreCreateValidator_AttachesCoordinatesFromGeoNames(t *testing.T) {
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
	if frag.Coordinates == nil {
		t.Fatalf("expected coordinates to be set")
	}
	if !models.IsValidCoordinates(frag.Coordinates.Lat, frag.Coordinates.Lon) {
		t.Fatalf("expected valid coordinates, got %+v", frag.Coordinates)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO6709 == nil || *frag.HomeLocation.ISO6709 == "" {
		t.Fatalf("expected homeLocation.iso6709 to be set")
	}
	if got := frag.Identifiers["geonamesId"]; got != "292968" {
		t.Fatalf("expected geonamesId=292968, got %q", got)
	}
}
