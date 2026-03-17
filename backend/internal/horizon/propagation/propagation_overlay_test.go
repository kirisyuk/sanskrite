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

func TestBuildPropagationNodeOverlay_PrefersOrganizationManufacturingSemanticsOverNoisyLogisticsHints(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/tesla",
		MarketDomain:  "industrial",
		MarketVertical:"port-services",
		GeoFeatureType:"port",
		Tags: []string{
			"/biz/economic-role/corporate-entity",
			"/biz/risk-exposure/industrial-capacity",
			"/biz/value-pool/manufacturing-output",
			"chokepoint",
		},
		Identifiers: map[string]string{
			"legalName": "Tesla, Inc.",
			"shortName": "Tesla",
		},
	}
	overlay := BuildPropagationNodeOverlay(frag)
	if overlay.PrimaryRole != "manufacturing_operator" {
		t.Fatalf("expected manufacturing_operator, got %q", overlay.PrimaryRole)
	}
	if overlay.BusinessRole != "assembler" {
		t.Fatalf("expected assembler business role, got %q", overlay.BusinessRole)
	}
	if !overlay.HasExposure("component_input") {
		t.Fatalf("expected component_input exposure")
	}
}

func TestIsChokepointFragment_IgnoresNoisyOrgTags(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/tesla",
		GeoFeatureType:"port",
		Tags:          []string{"chokepoint"},
	}
	if IsChokepointFragment(frag) {
		t.Fatalf("organization fragment must not be treated as chokepoint from noisy tags")
	}
}
