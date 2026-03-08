package canonical

import "testing"

func TestFacilitySlugLowInformation(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{slug: "general-production", want: true},
		{slug: "general-distribution", want: true},
		{slug: "casablanca-plant", want: true},
		{slug: "bouskoura-plant", want: true},
		{slug: "bosch-cairo-distribution-center", want: false},
		{slug: "gigafactory-texas", want: false},
		{slug: "bandar-abbas-refinery", want: false},
	}
	for _, tc := range tests {
		if got := FacilitySlugLowInformation(tc.slug); got != tc.want {
			t.Fatalf("slug=%q got=%v want=%v", tc.slug, got, tc.want)
		}
	}
}
