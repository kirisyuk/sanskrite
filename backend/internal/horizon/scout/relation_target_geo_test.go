package scout

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

func TestBestEffortEnrichRelationTargetGeo_FacilityUsesParentAndCandidateGeocoding(t *testing.T) {
	engine := &ScoutEngine{
		geocodingClient: &fakeGeocodingClient{
			results: map[string]*coreontology.GeocodingResult{
				"Onsan Refinery, KR": {
					Lat:              35.4322,
					Lon:              129.3306,
					FormattedAddress: "Onsan, Ulsan, South Korea",
					PlaceID:          "google-place-onsan-refinery",
				},
			},
		},
	}
	parentCC := "KR"
	parent := &models.HorizonFragment{
		Type:        models.FragmentTypeOrganization,
		Labels:      map[string]string{"en": "S-Oil"},
		Coordinates: &models.Coordinates{Lat: 0.0, Lon: 0.0},
		HomeLocation: &models.Location{
			ISO3166_1: &parentCC,
		},
	}
	target := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/kr/onsan-refinery",
		Labels:        map[string]string{"en": "Onsan Refinery"},
	}

	engine.bestEffortEnrichRelationTargetGeo(context.Background(), target, parent, "test", "job-1")

	if target.Coordinates == nil {
		t.Fatalf("expected coordinates to be attached")
	}
	if target.Coordinates.Lat != 35.4322 || target.Coordinates.Lon != 129.3306 {
		t.Fatalf("unexpected coordinates: %+v", target.Coordinates)
	}
	if target.HomeLocation == nil || target.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*target.HomeLocation.ISO3166_1) != "KR" {
		t.Fatalf("expected ISO3166_1=KR, got %+v", target.HomeLocation)
	}
	if target.HomeLocation.ISO6709 == nil || strings.TrimSpace(*target.HomeLocation.ISO6709) == "" {
		t.Fatalf("expected ISO6709 to be backfilled")
	}
}

func TestBestEffortEnrichRelationTargetGeo_FacilityFallsBackToParentCoordinates(t *testing.T) {
	engine := &ScoutEngine{}
	parentCC := "SA"
	parent := &models.HorizonFragment{
		Type:        models.FragmentTypeOrganization,
		Coordinates: &models.Coordinates{Lat: 26.7070, Lon: 50.0407},
		HomeLocation: &models.Location{
			ISO3166_1: &parentCC,
		},
	}
	target := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/sa/ras-tanura-terminal",
	}

	engine.bestEffortEnrichRelationTargetGeo(context.Background(), target, parent, "test", "job-2")

	if target.Coordinates == nil {
		t.Fatalf("expected coordinates from parent propagation")
	}
	if target.Coordinates.Lat != parent.Coordinates.Lat || target.Coordinates.Lon != parent.Coordinates.Lon {
		t.Fatalf("expected parent coordinates, got %+v", target.Coordinates)
	}
	if target.HomeLocation == nil || target.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*target.HomeLocation.ISO3166_1) != "SA" {
		t.Fatalf("expected ISO3166_1=SA, got %+v", target.HomeLocation)
	}
}

func TestBestEffortEnrichRelationTargetGeo_OrganizationUsesCanonicalCountryAndGeocoding(t *testing.T) {
	engine := &ScoutEngine{
		geocodingClient: &fakeGeocodingClient{
			results: map[string]*coreontology.GeocodingResult{
				"Bahri Chemicals, Saudi Arabia": {
					Lat:              24.7136,
					Lon:              46.6753,
					FormattedAddress: "Riyadh, Saudi Arabia",
					PlaceID:          "google-place-bahri-chemicals",
					Provider:         coreontology.GeocodingProviderGoogle,
				},
			},
		},
	}
	target := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/sa/bahri-chemicals",
		Labels:        map[string]string{"en": "Bahri Chemicals"},
	}

	engine.bestEffortEnrichRelationTargetGeo(context.Background(), target, nil, "test", "job-3")

	if target.HomeLocation == nil || target.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*target.HomeLocation.ISO3166_1) != "SA" {
		t.Fatalf("expected ISO3166_1=SA, got %+v", target.HomeLocation)
	}
	if target.Coordinates == nil {
		t.Fatalf("expected geocoded coordinates")
	}
	if target.Coordinates.Lat != 24.7136 || target.Coordinates.Lon != 46.6753 {
		t.Fatalf("unexpected coordinates: %+v", target.Coordinates)
	}
	if len(engine.geocodingClient.(*fakeGeocodingClient).calls) == 0 || engine.geocodingClient.(*fakeGeocodingClient).calls[0] != "Bahri Chemicals, Saudi Arabia" {
		t.Fatalf("expected org revalidation query, got %#v", engine.geocodingClient.(*fakeGeocodingClient).calls)
	}
	if target.Qualifiers == nil || target.Qualifiers["geoResolveTrace"] == nil {
		t.Fatalf("expected geoResolveTrace to be recorded")
	}
}

func TestBestEffortEnrichRelationTargetGeo_FacilityUsesFullNameForResolve(t *testing.T) {
	geocoder := &fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Jiuquan Satellite Launch Center, CN": {
				Lat:              40.984524,
				Lon:              100.208695,
				FormattedAddress: "Buge Yin'a Rila, Ejin Banner, Alxa League, Inner Mongolia, China, 735412",
				PlaceID:          "google-place-jiuquan-launch-center",
				Provider:         "google_geocoding",
			},
			"Launch, CN": {
				Lat:              28.4289124,
				Lon:              -80.5776203,
				FormattedAddress: "Launch Complex 29, Cape Canaveral, FL, United States",
				PlaceID:          "bad-launch-hit",
				Provider:         "nominatim",
			},
		},
	}
	engine := &ScoutEngine{geocodingClient: geocoder}
	target := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/cn/jiuquan-satellite-launch-center",
		Labels:        map[string]string{"en": "Jiuquan Satellite Launch Center"},
	}

	engine.bestEffortEnrichRelationTargetGeo(context.Background(), target, nil, "test", "job-4")

	if target.Coordinates == nil {
		t.Fatalf("expected geocoded coordinates")
	}
	if target.Coordinates.Lat != 40.984524 || target.Coordinates.Lon != 100.208695 {
		t.Fatalf("unexpected coordinates: %+v", target.Coordinates)
	}
	if len(geocoder.calls) == 0 || geocoder.calls[0] != "Jiuquan Satellite Launch Center, CN" {
		t.Fatalf("expected full-name query first, got %#v", geocoder.calls)
	}
	for _, call := range geocoder.calls {
		if call == "Launch, CN" {
			t.Fatalf("did not expect weak hint query after successful full-name resolve: %#v", geocoder.calls)
		}
	}
	if got := target.Identifiers["geoCoordinatesQuery"]; got != "Jiuquan Satellite Launch Center, CN" {
		t.Fatalf("expected attribution query to use full name, got %q", got)
	}
}
