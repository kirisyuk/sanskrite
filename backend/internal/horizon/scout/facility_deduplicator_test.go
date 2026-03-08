package scout

import "testing"

func TestFacilityPathHasLowInformationSlug(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/fac/na/general-production", want: true},
		{path: "/fac/na/casablanca-plant", want: true},
		{path: "/fac/eg/bosch-cairo-distribution-center", want: false},
		{path: "/fac/ir/bandar-abbas-refinery", want: false},
	}
	for _, tc := range tests {
		if got := facilityPathHasLowInformationSlug(tc.path); got != tc.want {
			t.Fatalf("path=%q got=%v want=%v", tc.path, got, tc.want)
		}
	}
}

func TestExtractFacilityGeoTokenFromPath(t *testing.T) {
	if got := extractFacilityGeoTokenFromPath("/fac/na/general-production"); got != "na" {
		t.Fatalf("expected na, got %q", got)
	}
	if got := extractFacilityGeoTokenFromPath("/fac/eg/port-of-alexandria"); got != "eg" {
		t.Fatalf("expected eg, got %q", got)
	}
}
