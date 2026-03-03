package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEdgeAllowlistByDomainMode_SupplyChainOperationalTypesOnly(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeSupplyChain)
	operational := []string{"PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "EXTRACTS_FROM"}
	for _, ot := range operational {
		if !AllowlistContains(allow, models.DependencyType(ot)) {
			t.Fatalf("supply_chain allowlist must include %s", ot)
		}
	}
	if AllowlistContains(allow, models.DependencyType("DEPENDS_ON")) || AllowlistContains(allow, models.DependencyType("CONSUMES")) {
		t.Fatalf("supply_chain allowlist must exclude DEPENDS_ON and CONSUMES (operational types only)")
	}
}

func TestEdgeAllowlistByDomainMode_PolicyImpactIncludesInfluences(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModePolicyImpact)
	if !AllowlistContains(allow, "INFLUENCES") {
		t.Fatalf("policy_impact allowlist must include INFLUENCES")
	}
}

func TestEdgeAllowlistByDomainMode_GovernanceIncludesAssurance(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeGovernance)
	if !AllowlistContains(allow, "ASSURES") {
		t.Fatalf("governance allowlist must include ASSURES for auditor/certifier governance links")
	}
}

func TestEdgeAllowlistByDomainMode_AssuranceExcludesLegacyAuditedBy(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeAssurance)
	if AllowlistContains(allow, "AUDITED_BY") {
		t.Fatalf("assurance allowlist must not include legacy AUDITED_BY output")
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChain(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{RelationType: "SUPPLIES_TO"},
		{RelationType: "depends_on"},
		{RelationType: "PRODUCES"},
		{RelationType: "OWNS"},
	}
	out := filterDiscoveredRelationsByDomainModes(in, []string{taxonomy.DomainModeSupplyChain})
	// DEPENDS_ON and OWNS excluded (operational types only); SUPPLIES_TO and PRODUCES pass
	if len(out) != 2 {
		t.Fatalf("expected 2 relations after supply_chain filter, got %d", len(out))
	}
	types := map[string]bool{}
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["SUPPLIES_TO"] || !types["PRODUCES"] {
		t.Fatalf("expected SUPPLIES_TO and PRODUCES, got %v", types)
	}
}

func TestSupplyChainsEnabled(t *testing.T) {
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be enabled in core mode")
	}
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeGovernance}, []string{taxonomy.SpecialtySupplyChain}) {
		t.Fatalf("supply chains must remain enabled with specialty/domain overrides")
	}
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeGovernance}, nil) {
		t.Fatalf("supply chains must be enabled even outside explicit supply_chain mode/specialty")
	}
	if supplyChainsEnabled("/person/john-doe", "", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be disabled for /person paths")
	}
	if supplyChainsEnabled("/org/us/test", "PERSON", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be disabled for PERSON type")
	}
}
