package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

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

func TestFacilitySlugCriticalDedupAnchor(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{slug: "port-of-rotterdam", want: true},
		{slug: "europipe-ii", want: true},
		{slug: "dresden-fab", want: true},
		{slug: "casablanca-plant", want: false},
	}
	for _, tc := range tests {
		if got := canonical.FacilitySlugCriticalDedupAnchor(tc.slug); got != tc.want {
			t.Fatalf("slug=%q got=%v want=%v", tc.slug, got, tc.want)
		}
	}
}

func TestFacilityPlaceholderMatchCandidateRejectsTwoPlaceholders(t *testing.T) {
	incoming := &models.HorizonFragment{
		Class:          FragmentTypeFacility,
		CanonicalPath: "/fac/na/port-of-rotterdam",
		Labels:        map[string]string{"en": "Port of Rotterdam"},
	}
	candidate := &models.HorizonFragment{
		Class:          FragmentTypeFacility,
		CanonicalPath: "/fac/na/port-of-rotterdam",
		Labels:        map[string]string{"en": "Port of Rotterdam"},
	}
	if isFacilityPlaceholderMatchCandidate(incoming, candidate) {
		t.Fatalf("expected placeholder-to-placeholder candidate to be rejected")
	}
}
