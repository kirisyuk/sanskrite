package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestRepairSyntheticOrgCanonicalPath_RepairsGovKnownGeoPlaceholderSlug(t *testing.T) {
	frag := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/gov/qa/na",
		Labels: map[string]string{
			"en": "Government of Qatar",
		},
		Identifiers: map[string]string{
			"legalName": "Government of Qatar",
			"shortName": "Government of Qatar",
		},
	}

	got, reason, ok := RepairSyntheticOrgCanonicalPath(frag)
	if !ok {
		t.Fatalf("expected repair to trigger")
	}
	if got != "/gov/qa/government" {
		t.Fatalf("repair path=%q, want /gov/qa/government", got)
	}
	if reason == "" {
		t.Fatalf("expected non-empty repair reason")
	}
}
