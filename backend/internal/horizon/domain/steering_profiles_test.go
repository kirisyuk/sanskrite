package domain

import (
	"reflect"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestDomainModesForContractTypes_PartnershipLandscape(t *testing.T) {
	modes := DomainModesForContractTypes([]string{"partnership-landscape"})
	want := []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance}
	if !reflect.DeepEqual(modes, want) {
		t.Errorf("partnership-landscape: got %v, want %v", modes, want)
	}
}

func TestDomainModesForContractTypes_PartnershipLandscapeWithOther(t *testing.T) {
	modes := DomainModesForContractTypes([]string{"partnership-landscape", "supply-chain"})
	// Should have supply_chain, governance (deduped)
	if len(modes) != 2 {
		t.Errorf("expected 2 modes, got %v", modes)
	}
	hasSupplyChain := false
	hasGovernance := false
	for _, m := range modes {
		if m == taxonomy.DomainModeSupplyChain {
			hasSupplyChain = true
		}
		if m == taxonomy.DomainModeGovernance {
			hasGovernance = true
		}
	}
	if !hasSupplyChain || !hasGovernance {
		t.Errorf("expected supply_chain and governance, got %v", modes)
	}
}

func TestDomainModesForContractTypes_PolicyImpact(t *testing.T) {
	modes := DomainModesForContractTypes([]string{"policy-impact"})
	want := []string{taxonomy.DomainModePolicyImpact}
	if !reflect.DeepEqual(modes, want) {
		t.Errorf("policy-impact: got %v, want %v", modes, want)
	}
}

func TestDomainModesForContractTypes_RegulatoryImpactUsesPolicyImpactMode(t *testing.T) {
	modes := DomainModesForContractTypes([]string{"regulatory-impact"})
	want := []string{taxonomy.DomainModePolicyImpact}
	if !reflect.DeepEqual(modes, want) {
		t.Errorf("regulatory-impact: got %v, want %v", modes, want)
	}
}
