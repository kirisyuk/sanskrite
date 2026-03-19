package horizon

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestShouldSkipDomainIncomingEdgeForCockpit(t *testing.T) {
	org := &models.HorizonFragment{Type: models.FragmentTypeOrganization, CanonicalPath: "/org/kz/ministry-of-agriculture"}
	domain := &models.HorizonFragment{Type: models.FragmentTypeDomain, CanonicalPath: "/domain/v1/market-opportunity/geo/kz/industry/agro-food"}

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
