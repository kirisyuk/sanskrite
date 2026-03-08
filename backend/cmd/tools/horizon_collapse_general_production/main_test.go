package main

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestDeriveOrgLabelFromSyntheticFacility(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{name: "manufacturing suffix", label: "Canon Oita Manufacturing Facility", want: "Canon Oita"},
		{name: "distribution suffix", label: "AEON Malaysia Distribution", want: "AEON Malaysia"},
		{name: "generic placeholder", label: "General Production", want: ""},
		{name: "processing plant", label: "FAMA Corporation Processing Plant", want: "FAMA Corporation"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveOrgLabelFromSyntheticFacility(&models.HorizonFragment{
				Type:   models.FragmentTypeFacility,
				Labels: map[string]string{"en": tc.label},
			})
			if got != tc.want {
				t.Fatalf("deriveOrgLabelFromSyntheticFacility(%q)=%q want %q", tc.label, got, tc.want)
			}
		})
	}
}
