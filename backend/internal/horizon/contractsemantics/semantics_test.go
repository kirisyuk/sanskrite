package contractsemantics

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestSortDomainModesForExecution(t *testing.T) {
	got := SortDomainModesForExecution([]string{taxonomy.DomainModePolicyImpact, taxonomy.DomainModeGovernance, taxonomy.DomainModeSupplyChain})
	want := []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance, taxonomy.DomainModePolicyImpact}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch: got=%v want=%v", got, want)
		}
	}
}

func TestComposeDomainModesCarriesSupplyAndGovernance(t *testing.T) {
	profile := ComposeDomainModes([]string{taxonomy.DomainModeGovernance, taxonomy.DomainModeSupplyChain})
	if len(profile.PathCarriedEdges) == 0 {
		t.Fatalf("expected path carried edges")
	}
	if len(profile.OverlayEdges) == 0 {
		t.Fatalf("expected overlay edges")
	}
	if profile.Modes[0] != taxonomy.DomainModeSupplyChain {
		t.Fatalf("expected supply chain first, got %v", profile.Modes)
	}
}

func TestEdgeLayerClassification(t *testing.T) {
	if got := EdgeLayer(ah.RelationNameSupplies); got != GraphLayerPathCarried {
		t.Fatalf("expected supplies path carried, got %s", got)
	}
	if got := EdgeLayer(ah.RelationNameProduces); got != GraphLayerPathCarried {
		t.Fatalf("expected produces path carried, got %s", got)
	}
	if got := EdgeLayer(ah.RelationNameRegulates); got != GraphLayerContractOverlay {
		t.Fatalf("expected regulates contract overlay, got %s", got)
	}
	if got := EdgeLayer(ah.RelationNameMemberOf); got != GraphLayerContractOverlay {
		t.Fatalf("expected member_of to be contract overlay, got %s", got)
	}
	if got := EdgeLayer(ah.RelationNameInfluences); got != GraphLayerInterpretive {
		t.Fatalf("expected influences interpretive, got %s", got)
	}
	if got := EdgeLayer(ah.RelationNameDisrupts); got != GraphLayerCorePropagation {
		t.Fatalf("expected disrupts core propagation, got %s", got)
	}
}

// TestSupplyChainContractDoesNotLeakOverlayOnlyEdges verifies that
// pure overlay/policy AH relations never sneak into the supply-chain
// path-carried set. Several legacy edge types (CONTROLS_ACCESS,
// IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO,
// DEPENDS_ON) were folded into mechanism/imposes/wields under AH and
// no longer correspond to standalone edges, so we only assert the
// AH-existing overlay relations are kept out.
func TestSupplyChainContractDoesNotLeakOverlayOnlyEdges(t *testing.T) {
	profile := ProfileForDomainMode(taxonomy.DomainModeSupplyChain)
	for _, forbidden := range []ah.RelationName{
		ah.RelationNameRegulates,
		ah.RelationNameImposes,
		ah.RelationNameGatekeeps,
		ah.RelationNameInfluences,
	} {
		if ModeAllowsEdge(taxonomy.DomainModeSupplyChain, forbidden) {
			t.Fatalf("supply_chain must not allow %s", forbidden)
		}
	}
	if len(profile.PathCarriedEdges) == 0 {
		t.Fatal("expected path-carried edges for supply_chain")
	}
}

// TestServiceChainContractAllowsOperationalAndAccessEdges verifies
// the AH-equivalent of the legacy DEPENDS_ON / CONTROLS_ACCESS /
// OPERATES_VIA / SUPPLIES_TO checks: under AH these are
// requires_capability / wields / supplies. The legacy aliases route
// through the same wire allowlist via models.NormalizeWireRelation.
func TestServiceChainContractAllowsOperationalAndAccessEdges(t *testing.T) {
	profile := ProfileForDomainMode(taxonomy.DomainModeServiceChain)
	for _, required := range []ah.RelationName{
		ah.RelationNameSupplies,
		ah.RelationNameWields,
	} {
		if !ModeAllowsEdge(taxonomy.DomainModeServiceChain, required) {
			t.Fatalf("service_chain must allow %s", required)
		}
	}
	for _, forbidden := range []ah.RelationName{
		ah.RelationNameRegulates,
		ah.RelationNameJointVentureWith,
	} {
		if ModeAllowsEdge(taxonomy.DomainModeServiceChain, forbidden) {
			t.Fatalf("service_chain must not allow %s", forbidden)
		}
	}
	if len(profile.PathCarriedEdges) == 0 {
		t.Fatal("expected path-carried edges for service_chain")
	}
	if len(profile.OverlayEdges) == 0 {
		t.Fatal("expected overlay edges for service_chain")
	}
}

func TestGovernanceAndPolicyRemainOverlayContracts(t *testing.T) {
	for _, mode := range []string{taxonomy.DomainModeGovernance, taxonomy.DomainModeAssurance, taxonomy.DomainModePolicyImpact, taxonomy.DomainModeStrategic} {
		profile := ProfileForDomainMode(mode)
		if profile.PrimaryLayer != GraphLayerContractOverlay {
			t.Fatalf("%s primary layer=%s, want contract_overlay", mode, profile.PrimaryLayer)
		}
		for _, forbidden := range []ah.RelationName{
			ah.RelationNameProduces,
			ah.RelationNameSupplies,
		} {
			if ModeAllowsEdge(mode, forbidden) {
				t.Fatalf("%s must not allow path-carried edge %s", mode, forbidden)
			}
		}
	}
}
