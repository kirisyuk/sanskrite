package edgesemantics

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeFragmentTypeToken_ExtendedTypesAndAliases(t *testing.T) {
	tests := []struct {
		in   string
		want models.FragmentType
	}{
		{in: "GOV", want: models.FragmentTypeOrganization},
		{in: "GOVERNMENT_BODY", want: models.FragmentTypeOrganization},
		{in: "FAC", want: models.FragmentTypeFacility},
		{in: "LOC", want: models.FragmentTypeGeoRegion},
		{in: "MEDIA", want: models.FragmentTypeMediaChannel},
		{in: "DOMAIN", want: models.FragmentTypeDomain},
		{in: "SPHERE", want: models.FragmentTypeSphere},
		{in: "DECISION", want: models.FragmentTypeDecisionPoint},
		{in: "MECHANISM", want: models.FragmentTypeMechanism},
		{in: "SUPPLYLINK", want: models.FragmentTypeSupplyLink},
		{in: "NETWORK", want: models.FragmentTypeCommunity},
	}

	for _, tt := range tests {
		got := NormalizeFragmentTypeToken(tt.in)
		if got != tt.want {
			t.Fatalf("NormalizeFragmentTypeToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFragmentTypeToken_PathInferenceAndUnknownFallback(t *testing.T) {
	if got := NormalizeFragmentTypeToken("/mechanism/regulatory/export-control"); got != models.FragmentTypeMechanism {
		t.Fatalf("path-based normalization should infer MECHANISM, got %q", got)
	}

	gotUnknown := NormalizeFragmentTypeToken("UNMAPPED_TYPE_TOKEN")
	if gotUnknown == "" {
		t.Fatalf("NormalizeFragmentTypeToken must never return empty")
	}
	if gotUnknown != models.FragmentTypeOrganization {
		t.Fatalf("unknown type fallback should be ORGANIZATION, got %q", gotUnknown)
	}
}
