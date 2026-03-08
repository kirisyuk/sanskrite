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
	if got := EdgeLayer(models.DependencyTypeRegulates); got != GraphLayerContractOverlay {
		t.Fatalf("expected contract overlay, got %s", got)
	}
	if got := EdgeLayer(models.DependencyTypeInfluences); got != GraphLayerInterpretive {
		t.Fatalf("expected interpretive, got %s", got)
	}
}
