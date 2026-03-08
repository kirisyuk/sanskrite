package geoambiguity

import "testing"

func TestCountryAliasAllowedInText(t *testing.T) {
	tests := []struct {
		alias string
		text  string
		want  bool
	}{
		{alias: "georgia", text: "Port of Savannah, Georgia handles container traffic", want: false},
		{alias: "georgia", text: "Georgia air defense and Tbilisi security policy", want: true},
		{alias: "jordan", text: "Michael Jordan brand revenue rises", want: false},
		{alias: "jordan", text: "RAF adds to drone tally over Jordan", want: true},
		{alias: "chad", text: "Chad government signs security pact", want: true},
	}
	for _, tc := range tests {
		if got := CountryAliasAllowedInText(tc.alias, tc.text); got != tc.want {
			t.Fatalf("alias=%q text=%q got=%v want=%v", tc.alias, tc.text, got, tc.want)
		}
	}
}
