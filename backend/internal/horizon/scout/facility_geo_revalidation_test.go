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
		{"Terminal", ""},
	}
	for _, tc := range cases {
		if got := inferPlaceHintFromFacilityLabel(tc.label); got != tc.want {
			t.Fatalf("inferPlaceHintFromFacilityLabel(%q)=%q want %q", tc.label, got, tc.want)
		}
	}
}
