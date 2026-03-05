package enrich

import (
	"context"
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type fakeGeocodingClient struct {
	lastAddress string
	result      *ontology.GeocodingResult
}

func (f *fakeGeocodingClient) Geocode(_ context.Context, address string) (*ontology.GeocodingResult, error) {
	f.lastAddress = address
	return f.result, nil
}

type fakeGeoNamesClient struct {
	searchCalls int
	getCalls    int
	searchResp  *ontology.GeoNamesSearchResponse
	places      map[int]*ontology.GeoNamesPlace
}

func (f *fakeGeoNamesClient) Search(_ context.Context, _ string, _ int, _ ontology.GeoNamesSearchOptions) (*ontology.GeoNamesSearchResponse, error) {
	f.searchCalls++
	return f.searchResp, nil
}

func (f *fakeGeoNamesClient) GetPlace(_ context.Context, geonameID int) (*ontology.GeoNamesPlace, error) {
	f.getCalls++
	if f.places == nil {
		return nil, nil
	}
	return f.places[geonameID], nil
}

func (f *fakeGeoNamesClient) GetHierarchy(_ context.Context, _ int) (*ontology.GeoNamesHierarchyResponse, error) {
	return nil, nil
}

func (f *fakeGeoNamesClient) GetCountryCode(_ context.Context, _, _ float64) (string, error) {
	return "", nil
}

func TestEnrichCoordinatesFromGeocoding_NullIslandDoesNotBlock(t *testing.T) {
	cc := "AE"
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeOrganization,
		HomeLocation: &hmodels.Location{
			Name:      "Dubai",
			ISO3166_1: &cc,
		},
		Coordinates: &hmodels.Coordinates{Lat: 0, Lon: 0},
	}
	gc := &fakeGeocodingClient{
		result: &ontology.GeocodingResult{Lat: 25.2048, Lon: 55.2708},
	}

	changed, err := EnrichCoordinatesFromGeocoding(context.Background(), gc, frag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true for null-island placeholder coordinates")
	}
	if frag.Coordinates == nil || frag.Coordinates.Lat != 25.2048 || frag.Coordinates.Lon != 55.2708 {
		t.Fatalf("expected geocoded coordinates, got %+v", frag.Coordinates)
	}
}

func TestEnrichCoordinatesFromGeocoding_MarketIsGeoRelevant(t *testing.T) {
	cc := "US"
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeMarket,
		HomeLocation: &hmodels.Location{
			Name:      "Houston",
			ISO3166_1: &cc,
		},
	}
	gc := &fakeGeocodingClient{
		result: &ontology.GeocodingResult{Lat: 29.7604, Lon: -95.3698},
	}

	changed, err := EnrichCoordinatesFromGeocoding(context.Background(), gc, frag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true for market fragment")
	}
	if gc.lastAddress != "Houston, US" {
		t.Fatalf("expected market geocoding address to include country, got %q", gc.lastAddress)
	}
}

func TestBuildAddressFromFragment_FacilityUsesParentCountry(t *testing.T) {
	frag := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/na/plant-7",
		ParentPath:    "/org/us/acme-holding",
		HomeLocation: &hmodels.Location{
			Name: "Houston",
		},
	}

	addr := BuildAddressFromFragment(frag)
	if addr != "Houston, US" {
		t.Fatalf("BuildAddressFromFragment() = %q, want %q", addr, "Houston, US")
	}
}

func TestEnrichCoordinatesFromGeoNames_UsesGeoNamesIDForFacility(t *testing.T) {
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeFacility,
		Identifiers: map[string]string{
			"geonamesId": "292223",
		},
	}
	gn := &fakeGeoNamesClient{
		places: map[int]*ontology.GeoNamesPlace{
			292223: {GeoNameID: 292223, Lat: "25.2048", Lng: "55.2708", CountryCode: "AE"},
		},
	}

	changed, err := EnrichCoordinatesFromGeoNames(context.Background(), gn, frag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when geonamesId is present")
	}
	if gn.searchCalls != 0 {
		t.Fatalf("search should not be called when geonamesId is present, got %d", gn.searchCalls)
	}
	if frag.Coordinates == nil || frag.Coordinates.Lat != 25.2048 || frag.Coordinates.Lon != 55.2708 {
		t.Fatalf("expected coordinates from geonamesId, got %+v", frag.Coordinates)
	}
}

func TestEnrichCoordinatesFromGeoNames_UsesGeoNamesKBRefID(t *testing.T) {
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeFacility,
		KBRefs: []hmodels.KBReference{
			{KB: "GeoNames", ExternalID: "292223"},
		},
	}
	gn := &fakeGeoNamesClient{
		places: map[int]*ontology.GeoNamesPlace{
			292223: {GeoNameID: 292223, Lat: "25.2048", Lng: "55.2708", CountryCode: "AE"},
		},
	}

	changed, err := EnrichCoordinatesFromGeoNames(context.Background(), gn, frag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when geonames KBRef is present")
	}
	if frag.Coordinates == nil || frag.Coordinates.Lat != 25.2048 || frag.Coordinates.Lon != 55.2708 {
		t.Fatalf("expected coordinates from geonames KBRef, got %+v", frag.Coordinates)
	}
}

func TestEnrichCoordinatesFromParent_Facility(t *testing.T) {
	cc := "US"
	parent := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/acme",
		Coordinates:   &hmodels.Coordinates{Lat: 29.7604, Lon: -95.3698},
		HomeLocation:  &hmodels.Location{ISO3166_1: &cc},
	}
	child := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/na/plant-7",
	}

	changed := EnrichCoordinatesFromParent(child, parent)
	if !changed {
		t.Fatalf("expected propagation from parent")
	}
	if child.Coordinates == nil || child.Coordinates.Lat != 29.7604 || child.Coordinates.Lon != -95.3698 {
		t.Fatalf("unexpected child coordinates: %+v", child.Coordinates)
	}
	if child.HomeLocation == nil || child.HomeLocation.ISO3166_1 == nil || *child.HomeLocation.ISO3166_1 != "US" {
		t.Fatalf("expected child country to be inherited from parent")
	}
}

func TestBuildFacilityAddressCandidates_UsesParentContext(t *testing.T) {
	cc := "US"
	frag := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/na/petrochem-plant-alpha",
		Identifiers: map[string]string{
			"city": "Houston",
		},
	}
	parent := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/acme",
		Labels:        map[string]string{"en": "Acme Corp"},
		HomeLocation:  &hmodels.Location{ISO3166_1: &cc},
	}

	candidates := BuildFacilityAddressCandidates(frag, parent)
	if len(candidates) == 0 {
		t.Fatalf("expected non-empty facility candidates")
	}
	found := false
	for _, c := range candidates {
		if c == "petrochem plant alpha, Acme Corp, US" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parent-aware candidate, got %#v", candidates)
	}
}

func TestBuildAddressFromFragment_EntityNameCountryFallback(t *testing.T) {
	frag := &hmodels.HorizonFragment{
		Type:          hmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/acme-corp",
		Labels:        map[string]string{"en": "Acme Corp"},
		Identifiers:   map[string]string{},
	}

	addr := BuildAddressFromFragment(frag)
	if addr != "Acme Corp, US" {
		t.Fatalf("BuildAddressFromFragment() = %q, want %q", addr, "Acme Corp, US")
	}
}
