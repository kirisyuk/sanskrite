package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestPolicyImpactTransferFactor_AllowsConfiguredAxis(t *testing.T) {
	f, ok := policyImpactTransferFactor(horizonmodels.DependencyTypeRegulates, horizonmodels.AxisLegalCompliance)
	if !ok {
		t.Fatalf("expected legal/compliance to be allowed on REGULATES")
	}
	if f <= 1.0 {
		t.Fatalf("expected amplification for legal/compliance on REGULATES, got %.3f", f)
	}
}

func TestPolicyImpactTransferFactor_BlocksUnsupportedAxis(t *testing.T) {
	if _, ok := policyImpactTransferFactor(horizonmodels.DependencyTypeRegulates, horizonmodels.AxisLogisticsCapacity); ok {
		t.Fatalf("expected logistics/capacity to be blocked on REGULATES")
	}
}

func TestPolicyImpactCanonicalGeo_ExtractsDomainGeoFacet(t *testing.T) {
	geo, ok := policyImpactCanonicalGeo("/domain/v1/policy-impact/geo/kz/topic/taxation")
	if !ok {
		t.Fatalf("expected domain path geo extraction")
	}
	if geo != "kz" {
		t.Fatalf("unexpected geo: %q", geo)
	}
}

func TestPolicyImpactDecodeScopeFactor_JurisdictionFilter(t *testing.T) {
	scenario := ScenarioInput{
		Parameters: map[string]interface{}{
			"location_path": "/loc/kz/na/na",
		},
	}
	inScope := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/kz/ministry-of-finance",
	}
	outOfScope := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/ae/mubadala",
	}

	if factor, ok := policyImpactDecodeScopeFactor(inScope, scenario); !ok || factor <= 0 {
		t.Fatalf("expected in-scope node to pass decode filter, factor=%.3f ok=%v", factor, ok)
	}
	if factor, ok := policyImpactDecodeScopeFactor(outOfScope, scenario); ok || factor > 0 {
		t.Fatalf("expected out-of-scope node to be blocked, factor=%.3f ok=%v", factor, ok)
	}
}

func TestPolicyImpactDecodeScopeFactor_LegacyOrgPathBlocked(t *testing.T) {
	scenario := ScenarioInput{
		Parameters: map[string]interface{}{
			"location_path": "/loc/kz/na/na",
		},
	}
	legacy := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/mubadala-investment-company-pjsc",
	}
	if factor, ok := policyImpactDecodeScopeFactor(legacy, scenario); ok || factor > 0 {
		t.Fatalf("expected legacy /org/{slug} to be blocked for country-scoped decode")
	}
}
