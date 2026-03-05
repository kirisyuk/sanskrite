package risk

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestPrimaryScope_NilHomeLocation_NoPanic(t *testing.T) {
	r := &AppetiteResolver{}
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/sa/saudi-aramco-jsc",
		HomeLocation:  nil,
	}

	got := r.primaryScope(frag)
	if got != "" {
		t.Fatalf("expected empty scope, got %q", got)
	}
}

func TestPrimaryScope_HomeLocationISO_ResolvesLocScope(t *testing.T) {
	r := &AppetiteResolver{}
	cc := "SA"
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/sa/saudi-aramco-jsc",
		HomeLocation: &horizonmodels.Location{
			ISO3166_1: &cc,
		},
	}

	got := r.primaryScope(frag)
	if got != "/loc/sa" {
		t.Fatalf("expected /loc/sa, got %q", got)
	}
}
