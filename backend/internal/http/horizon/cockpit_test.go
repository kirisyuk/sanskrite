package horizon

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestShouldSkipDomainIncomingEdgeForCockpit(t *testing.T) {
	org := &models.HorizonFragment{Class: ah.FragmentClassOrg, CanonicalPath: "/org/kz/ministry-of-agriculture"}
	// Legacy DOMAIN class was removed post-AH; the fragment is recognised by
	// canonical-path prefix only (see isDomainFragment).
	domain := &models.HorizonFragment{CanonicalPath: "/domain/v1/market-opportunity/geo/kz/industry/agro-food"}

	if !shouldSkipDomainIncomingEdgeForCockpit(org, domain) {
		t.Fatalf("expected non-domain -> domain edge to be skipped")
	}
	if shouldSkipDomainIncomingEdgeForCockpit(domain, org) {
		t.Fatalf("did not expect domain -> non-domain edge to be skipped")
	}
	if shouldSkipDomainIncomingEdgeForCockpit(domain, domain) {
		t.Fatalf("did not expect domain -> domain edge to be skipped")
	}
}
