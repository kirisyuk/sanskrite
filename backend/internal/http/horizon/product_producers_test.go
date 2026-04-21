package horizon

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestMergeProductProducerCandidatePrefersTenantScope(t *testing.T) {
	dst := map[string]*productProducerCandidate{}

	global := &horizonmodels.HorizonFragment{
		ID:            "global-fac",
		Class:         ah.FragmentClassSite,
		CanonicalPath: "/fac/global/test-plant",
		Labels:        map[string]string{"en": "Test Plant"},
	}
	tenant := &horizonmodels.HorizonFragment{
		ID:            "tenant-fac",
		Class:         ah.FragmentClassSite,
		CanonicalPath: "/fac/global/test-plant",
		Labels:        map[string]string{"en": "Test Plant"},
	}

	mergeProductProducerCandidate(dst, global, "global")
	mergeProductProducerCandidate(dst, tenant, "tenant-a")

	got := dst["/fac/global/test-plant"]
	if got == nil {
		t.Fatalf("expected merged candidate")
	}
	if got.fragment == nil || got.fragment.ID != "tenant-fac" {
		t.Fatalf("expected tenant-scoped fragment to win, got %#v", got.fragment)
	}
	if got.edgeCount != 2 {
		t.Fatalf("expected edgeCount=2, got %d", got.edgeCount)
	}
}

func TestSortProductProducersFacilityBeforeOrganization(t *testing.T) {
	producers := []productProducerResponse{
		{Label: "Zeta Org", FragmentType: string(ah.FragmentClassOrg), EdgeCount: 1},
		{Label: "Alpha Facility", FragmentType: string(ah.FragmentClassSite), EdgeCount: 1},
		{Label: "Beta Facility", FragmentType: string(ah.FragmentClassSite), EdgeCount: 3},
	}

	sortProductProducers(producers)

	if producers[0].Label != "Beta Facility" {
		t.Fatalf("expected highest edgeCount facility first, got %q", producers[0].Label)
	}
	if producers[1].Label != "Alpha Facility" {
		t.Fatalf("expected second facility next, got %q", producers[1].Label)
	}
	if producers[2].Label != "Zeta Org" {
		t.Fatalf("expected organization last, got %q", producers[2].Label)
	}
}

