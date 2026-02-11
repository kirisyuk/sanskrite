package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeFragmentTypeHardcoded_ExtendedCoverage(t *testing.T) {
	tests := []struct {
		in   string
		want models.FragmentType
	}{
		{in: "GOV", want: models.FragmentTypeOrganization},
		{in: "DOMAIN", want: models.FragmentTypeDomain},
		{in: "SPHERE", want: models.FragmentTypeSphere},
		{in: "MECHANISM", want: models.FragmentTypeMechanism},
		{in: "SUPPLYLINK", want: models.FragmentTypeSupplyLink},
		{in: "NETWORK", want: models.FragmentTypeCommunity},
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
	if got != models.FragmentTypeOrganization {
		t.Fatalf("unknown token should fall back to ORGANIZATION, got %q", got)
	}
}
