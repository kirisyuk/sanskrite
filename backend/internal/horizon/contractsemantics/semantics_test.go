package contractsemantics

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	if got := EdgeLayer(models.DependencyTypeSuppliesTo); got != GraphLayerPathCarried {
		t.Fatalf("expected path carried, got %s", got)
	}
	if got := EdgeLayer(models.DependencyTypeUsedIn); got != GraphLayerPathCarried {
		t.Fatalf("expected USED_IN to be path carried, got %s", got)
	}
	if got := EdgeLayer(models.DependencyTypeRegulates); got != GraphLayerContractOverlay {
		t.Fatalf("expected contract overlay, got %s", got)
	}
	if got := EdgeLayer(models.DependencyTypeAffiliatedWith); got != GraphLayerContractOverlay {
		t.Fatalf("expected community affiliation to be contract overlay, got %s", got)
	}
	if got := EdgeLayer(models.DependencyTypeInfluences); got != GraphLayerInterpretive {
		t.Fatalf("expected interpretive, got %s", got)
	}
}

func TestSupplyChainContractDoesNotLeakOverlayOnlyEdges(t *testing.T) {
	profile := ProfileForDomainMode(taxonomy.DomainModeSupplyChain)
	for _, forbidden := range []models.DependencyType{
		models.DependencyTypeRegulates,
		models.DependencyTypeControlsAccess,
		models.DependencyTypeImposesCondition,
		models.DependencyTypeGatekeepsDecision,
		models.DependencyTypeUsesMechanism,
		models.DependencyTypeAppliesTo,
		models.DependencyTypeInfluences,
		models.DependencyTypeDependsOn,
	} {
		if ModeAllowsEdge(taxonomy.DomainModeSupplyChain, forbidden) {
			t.Fatalf("supply_chain must not allow %s", forbidden)
		}
	}
	if len(profile.PathCarriedEdges) == 0 {
		t.Fatal("expected path-carried edges for supply_chain")
	}
}

func TestServiceChainContractAllowsOperationalDependencyAndAccessEdges(t *testing.T) {
	profile := ProfileForDomainMode(taxonomy.DomainModeServiceChain)
	for _, required := range []models.DependencyType{
		models.DependencyTypeDependsOn,
		models.DependencyTypeControlsAccess,
		models.DependencyTypeOperatesVia,
		models.DependencyTypeSuppliesTo,
	} {
		if !ModeAllowsEdge(taxonomy.DomainModeServiceChain, required) {
			t.Fatalf("service_chain must allow %s", required)
		}
	}
	for _, forbidden := range []models.DependencyType{
		models.DependencyTypeRegulates,
		models.DependencyTypeInfluences,
		models.DependencyTypePartnersWith,
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
	if len(profile.InterpretiveEdges) == 0 {
		t.Fatal("expected interpretive edges for service_chain")
	}
}

func TestGovernanceAndPolicyRemainOverlayContracts(t *testing.T) {
	for _, mode := range []string{taxonomy.DomainModeGovernance, taxonomy.DomainModeAssurance, taxonomy.DomainModePolicyImpact, taxonomy.DomainModeStrategic} {
		profile := ProfileForDomainMode(mode)
		if profile.PrimaryLayer != GraphLayerContractOverlay {
			t.Fatalf("%s primary layer=%s, want contract_overlay", mode, profile.PrimaryLayer)
		}
		for _, forbidden := range []models.DependencyType{
			models.DependencyTypeProduces,
			models.DependencyTypeSuppliesTo,
			models.DependencyTypeTransformsInto,
			models.DependencyTypeAssemblesInto,
			models.DependencyTypeUsedIn,
			models.DependencyTypeExtractsFrom,
		} {
			if ModeAllowsEdge(mode, forbidden) {
				t.Fatalf("%s must not allow path-carried edge %s", mode, forbidden)
			}
		}
	}
}
