package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestNormalizeFragmentTypeHardcoded_ExtendedCoverage(t *testing.T) {
	// MIGRATION NOTE (AH taxonomy): legacy ah.FragmentClassGeo /
	// FragmentTypeSphere / ah.FragmentClassSite folded onto their
	// parent AH classes (evidence/evidence/site). The hardcoded
	// normalizer now returns just the parent class — subtype hints
	// (domain / sphere / supply_link) are recovered separately by
	// callers that need them via models.NormalizeFragmentClassName.
	tests := []struct {
		in   string
		want FragmentType
	}{
		{in: "GOV", want: ah.FragmentClassOrg},
		{in: "DOMAIN", want: ah.FragmentClassEvidence},
		{in: "SPHERE", want: ah.FragmentClassEvidence},
		{in: "MECHANISM", want: ah.FragmentClassMechanism},
		{in: "SUPPLYLINK", want: ah.FragmentClassSite},
		{in: "NETWORK", want: ah.FragmentClassCommunity},
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
	if got != ah.FragmentClassOrg {
		t.Fatalf("unknown token should fall back to ORGANIZATION, got %q", got)
	}
}
