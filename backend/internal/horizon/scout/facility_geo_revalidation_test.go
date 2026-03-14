package scout

import "testing"

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
