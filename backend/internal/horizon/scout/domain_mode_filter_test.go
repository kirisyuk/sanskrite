package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEdgeAllowlistByDomainMode_SupplyChainIncludesDependsOn(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeSupplyChain)
	if !AllowlistContains(allow, "DEPENDS_ON") {
		t.Fatalf("supply_chain allowlist must include DEPENDS_ON")
	}
}

func TestEdgeAllowlistByDomainMode_PolicyImpactIncludesInfluences(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModePolicyImpact)
	if !AllowlistContains(allow, "INFLUENCES") {
		t.Fatalf("policy_impact allowlist must include INFLUENCES")
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChain(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{RelationType: "SUPPLIES_TO"},
		{RelationType: "depends_on"},
		{RelationType: "OWNS"},
	}
	out := filterDiscoveredRelationsByDomainModes(in, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 relations after supply_chain filter, got %d", len(out))
	}
	if out[0].RelationType != "SUPPLIES_TO" {
		t.Fatalf("first relationType=%q, want SUPPLIES_TO", out[0].RelationType)
	}
	if out[1].RelationType != "DEPENDS_ON" {
		t.Fatalf("second relationType=%q, want DEPENDS_ON", out[1].RelationType)
	}
}

func TestSupplyChainsEnabled(t *testing.T) {
	if !supplyChainsEnabled([]string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be enabled for supply_chain domain mode")
	}
	if !supplyChainsEnabled([]string{taxonomy.DomainModeGovernance}, []string{taxonomy.SpecialtySupplyChain}) {
		t.Fatalf("supply chains must be enabled when supply_chain specialty is requested")
	}
	if supplyChainsEnabled([]string{taxonomy.DomainModeGovernance}, nil) {
		t.Fatalf("supply chains must be disabled outside supply_chain mode/specialty")
	}
}
