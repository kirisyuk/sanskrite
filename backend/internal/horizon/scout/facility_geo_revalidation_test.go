package scout

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

func TestInferPlaceHintFromFacilityLabel(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"Munich Airport Cargo Terminal", "munich"},
		{"Aliaga LNG Terminal", "aliaga lng"},
		{"Cebu IT Park Facility", "cebu it"},
		{"Kasawari Gas Field", "kasawari gas"},
		{"Eco Resource Kz Almaty", "almaty"},
		{"Salt Of The Earth Eilat", "eilat"},
		{"Byd Electronic Huizhou Factory", "huizhou"},
		{"Kaspi Fulfillment Network", ""},
		{"Terminal", ""},
	}
	for _, tc := range cases {
		if got := inferPlaceHintFromFacilityLabel(tc.label); got != tc.want {
			t.Fatalf("inferPlaceHintFromFacilityLabel(%q)=%q want %q", tc.label, got, tc.want)
		}
	}
}

func TestContainsNormalizedGeoPhrase_UsesPhraseBoundaries(t *testing.T) {
	if containsNormalizedGeoPhrase("FAMA Corporation Processing Plant", "roc") {
		t.Fatalf("expected alias roc not to match inside corporation")
	}
	if !containsNormalizedGeoPhrase("AEON Malaysia Distribution", "malaysia") {
		t.Fatalf("expected malaysia phrase to match")
	}
}

func TestShouldAcceptFacilityGeoResult_RejectsGenericPlaceOverrideQueries(t *testing.T) {
	cases := []struct {
		name             string
		fragment         *models.HorizonFragment
		query            string
		formattedAddress string
		pathISO2         string
		resolvedISO2     string
	}{
		{
			name: "manufacturing",
			fragment: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/ae/nestle-dubai-factory",
				Labels:        map[string]string{"en": "Nestle Dubai Manufacturing"},
			},
			query:            "Manufacturing",
			formattedAddress: "Manufacturing, Penang, Malaysia",
			pathISO2:         "ae",
			resolvedISO2:     "my",
		},
		{
			name: "care factory",
			fragment: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/ae/unilever-dubai-personal-care-factory",
				Labels:        map[string]string{"en": "Unilever Dubai Personal Care Factory"},
			},
			query:            "Care Factory",
			formattedAddress: "Care Factory, Kampala, Uganda",
			pathISO2:         "ae",
			resolvedISO2:     "ug",
		},
		{
			name: "showroom",
			fragment: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/om/towell-auto-centre-muscat",
				Labels:        map[string]string{"en": "Towell Auto Centre Muscat Showroom"},
			},
			query:            "Showroom",
			formattedAddress: "Showroom, Hyderabad, India",
			pathISO2:         "om",
			resolvedISO2:     "in",
		},
		{
			name: "border crossing",
			fragment: &models.HorizonFragment{
				Type:          models.FragmentTypeFacility,
				CanonicalPath: "/fac/ae/hatta-border-crossing",
				Labels:        map[string]string{"en": "Hatta Border Crossing"},
			},
			query:            "Border Crossing",
			formattedAddress: "Border Crossing, Chicago, Illinois, United States",
			pathISO2:         "ae",
			resolvedISO2:     "us",
		},
	}

	for _, tc := range cases {
		if shouldAcceptFacilityGeoResult(tc.fragment, tc.query, tc.formattedAddress, "", tc.pathISO2, tc.resolvedISO2) {
			t.Fatalf("%s: expected generic query to be rejected", tc.name)
		}
	}
}

func TestShouldAcceptFacilityGeoResult_AcceptsStrongNameCountryMatch(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/ae/unilever-dubai-personal-care-factory",
		Labels:        map[string]string{"en": "Unilever Dubai Personal Care Factory"},
	}

	if !shouldAcceptFacilityGeoResult(fragment, "Unilever Dubai Personal Care Factory, AE", "Dubai, United Arab Emirates", "", "ae", "ae") {
		t.Fatal("expected strong-name facility query to be accepted when the path country matches")
	}
}

func TestShouldAcceptFacilityGeoResult_RejectsCountryOnlyResultForSpecificFacility(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/kz/khorgos-nur-zholy-customs-post",
		Labels:        map[string]string{"en": "Khorgos Nur Zholy Customs Post"},
	}

	if shouldAcceptFacilityGeoResult(fragment, "Khorgos Nur Zholy Customs Post, KZ", "Kazakhstan", "", "kz", "kz") {
		t.Fatal("expected country-only result to be rejected for specific facility query")
	}
}

func TestFacilityGeoValidationQueries_StrongNameSkipsGenericFallbacks(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/ae/unilever-dubai-personal-care-factory",
		Labels:        map[string]string{"en": "Unilever Dubai Personal Care Factory"},
	}

	queries := facilityGeoValidationQueries(fragment)
	joined := strings.Join(queries, "\n")

	if !strings.Contains(joined, "Unilever Dubai Personal Care Factory") {
		t.Fatalf("expected strong-name facility queries, got %#v", queries)
	}
	for _, disallowed := range []string{"Care Factory", "Dubai", "Factory"} {
		for _, query := range queries {
			if strings.EqualFold(strings.TrimSpace(query), disallowed) || strings.EqualFold(strings.TrimSpace(query), disallowed+", AE") {
				t.Fatalf("did not expect generic fallback query %q in %#v", disallowed, queries)
			}
		}
	}
}

func TestRevalidateFacilityGeo_AcceptsStrongGoogleHitWithoutParseableCountry(t *testing.T) {
	fragment := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/ae/nestle-dubai-factory",
		Labels:        map[string]string{"en": "Nestlé Dubai Manufacturing"},
	}
	geocoder := &fakeGeocodingClient{
		results: map[string]*coreontology.GeocodingResult{
			"Nestlé Dubai Manufacturing, AE": {
				Lat:              24.932634,
				Lon:              55.051494,
				FormattedAddress: "Nestle Dubai Manufacturing Facility, الواجهة البحرية, دبي, الإمارات العربية المتحدة",
				PlaceID:          "google-place-nestle-dubai",
				Provider:         coreontology.GeocodingProviderGoogle,
			},
		},
	}

	result, err := RevalidateFacilityGeo(context.Background(), fragment, geocoder)
	if err != nil {
		t.Fatalf("RevalidateFacilityGeo() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected revalidation to apply, got %+v", result)
	}
	if fragment.Coordinates == nil || fragment.Coordinates.Lat != 24.932634 || fragment.Coordinates.Lon != 55.051494 {
		t.Fatalf("unexpected coordinates: %+v", fragment.Coordinates)
	}
	if fragment.HomeLocation == nil || fragment.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) != "AE" {
		t.Fatalf("expected ISO3166_1=AE, got %+v", fragment.HomeLocation)
	}
	if got := strings.TrimSpace(fragment.Identifiers["geoCoordinatesProvider"]); got != coreontology.GeocodingProviderGoogle {
		t.Fatalf("expected google attribution, got %q", got)
	}
	if len(geocoder.calls) == 0 || !strings.EqualFold(strings.TrimSpace(geocoder.calls[0]), "Nestlé Dubai Manufacturing, AE") {
		t.Fatalf("expected first query to use strong facility title, got %#v", geocoder.calls)
	}
}
