package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildPropagationNodeOverlay_UsesMarketDomainSemantics(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/mastercard",
		MarketDomain:  "finance",
		MarketVertical:"payments",
	}
	overlay := BuildPropagationNodeOverlay(frag)
	if overlay.PrimaryRole != "financial_infrastructure" {
		t.Fatalf("expected financial_infrastructure, got %q", overlay.PrimaryRole)
	}
	if !overlay.HasExposure("payment") {
		t.Fatalf("expected payment exposure")
	}
}

func TestBuildPropagationNodeOverlay_UsesIndustrialLogisticsSemantics(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/de/port-of-hamburg",
		MarketDomain:  "industrial",
		MarketVertical:"port-services",
	}
	overlay := BuildPropagationNodeOverlay(frag)
	if overlay.PrimaryRole != "logistics_hub" {
		t.Fatalf("expected logistics_hub, got %q", overlay.PrimaryRole)
	}
	if !overlay.HasRole("bridge") {
		t.Fatalf("expected bridge role")
	}
}
