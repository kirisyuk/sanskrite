package scout

import (
	"context"
	"testing"

	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type fakeGeoNames struct {
	search func(query string, max int, opts coreontology.GeoNamesSearchOptions) *coreontology.GeoNamesSearchResponse
}

func (f *fakeGeoNames) Search(ctx context.Context, query string, maxResults int, opts coreontology.GeoNamesSearchOptions) (*coreontology.GeoNamesSearchResponse, error) {
	if f.search == nil {
		return &coreontology.GeoNamesSearchResponse{Query: query}, nil
	}
	return f.search(query, maxResults, opts), nil
}
func (f *fakeGeoNames) GetPlace(ctx context.Context, geonameID int) (*coreontology.GeoNamesPlace, error) {
	return nil, nil
}
func (f *fakeGeoNames) GetHierarchy(ctx context.Context, geonameID int) (*coreontology.GeoNamesHierarchyResponse, error) {
	return nil, nil
}

func TestNormalizeGeoPathViaGeoNames_StrictAdminFix(t *testing.T) {
	ctx := context.Background()
	client := &fakeGeoNames{
		search: func(query string, max int, opts coreontology.GeoNamesSearchOptions) *coreontology.GeoNamesSearchResponse {
			// We expect Abu Dhabi within AE to return adminCode1 "AZ".
			if query == "abu dhabi" && opts.Country == "AE" && opts.FeatureClass == "P" {
				return &coreontology.GeoNamesSearchResponse{
					Query: query,
					Results: []coreontology.GeoNamesPlace{
						{Name: "Abu Dhabi", CountryCode: "AE", AdminCode1: "AZ", Population: 1500000},
					},
				}
			}
			return &coreontology.GeoNamesSearchResponse{Query: query}
		},
	}

	norm, _, _ := normalizeGeoPathViaGeoNames(ctx, "/loc/ae/na/abu-dhabi", client)
	if norm != "/loc/ae/az/abu-dhabi" {
		t.Fatalf("expected admin fix, got %q", norm)
	}
}

func TestNormalizeGeoPathViaGeoNames_LooseCityUpgrade(t *testing.T) {
	ctx := context.Background()
	client := &fakeGeoNames{
		search: func(query string, max int, opts coreontology.GeoNamesSearchOptions) *coreontology.GeoNamesSearchResponse {
			// Global search.
			if query == "abu dhabi" && opts.FeatureClass == "P" && opts.Country == "" {
				return &coreontology.GeoNamesSearchResponse{
					Query: query,
					Results: []coreontology.GeoNamesPlace{
						{Name: "Abu Dhabi", CountryCode: "AE", AdminCode1: "AZ", Population: 1500000},
					},
				}
			}
			return &coreontology.GeoNamesSearchResponse{Query: query}
		},
	}

	norm, _, _ := normalizeGeoPathViaGeoNames(ctx, "/loc/abu-dhabi", client)
	if norm != "/loc/ae/az/abu-dhabi" {
		t.Fatalf("expected city upgrade, got %q", norm)
	}
}

func TestNormalizeGeoPathViaGeoNames_AmbiguousNoOp(t *testing.T) {
	ctx := context.Background()
	client := &fakeGeoNames{
		search: func(query string, max int, opts coreontology.GeoNamesSearchOptions) *coreontology.GeoNamesSearchResponse {
			if query == "springfield" && opts.FeatureClass == "P" {
				return &coreontology.GeoNamesSearchResponse{
					Query: query,
					Results: []coreontology.GeoNamesPlace{
						{Name: "Springfield", CountryCode: "US", AdminCode1: "IL", Population: 100000},
						{Name: "Springfield", CountryCode: "US", AdminCode1: "MO", Population: 90000},
					},
				}
			}
			return &coreontology.GeoNamesSearchResponse{Query: query}
		},
	}

	norm, _, _ := normalizeGeoPathViaGeoNames(ctx, "/loc/springfield", client)
	if norm != "" {
		t.Fatalf("expected no-op, got %q", norm)
	}
}
