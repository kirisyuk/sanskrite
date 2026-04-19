package scout

import (
	"testing"

)

func TestNormalizeFragmentTypeHardcoded_ExtendedCoverage(t *testing.T) {
	// MIGRATION NOTE (AH taxonomy): legacy FragmentTypeDomain /
	// FragmentTypeSphere / FragmentTypeSupplyLink folded onto their
	// parent AH classes (evidence/evidence/site). The hardcoded
	// normalizer now returns just the parent class — subtype hints
	// (domain / sphere / supply_link) are recovered separately by
	// callers that need them via models.NormalizeFragmentClassName.
	tests := []struct {
		in   string
		want FragmentType
	}{
		{in: "GOV", want: FragmentTypeOrganization},
		{in: "DOMAIN", want: FragmentTypeEvent},
		{in: "SPHERE", want: FragmentTypeEvent},
		{in: "MECHANISM", want: FragmentTypeMechanism},
		{in: "SUPPLYLINK", want: FragmentTypeFacility},
		{in: "NETWORK", want: FragmentTypeCommunity},
	}

	for _, tt := range tests {
		got := normalizeFragmentTypeHardcoded(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeFragmentTypeHardcoded(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFragmentTypeHardcoded_UnknownNeverEmpty(t *testing.T) {
	got := normalizeFragmentTypeHardcoded("UNMAPPED_TYPE_TOKEN")
	if got == "" {
		t.Fatalf("normalizeFragmentTypeHardcoded must not return empty")
	}
	if got != FragmentTypeOrganization {
		t.Fatalf("unknown token should fall back to ORGANIZATION, got %q", got)
	}
}
