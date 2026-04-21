package horizon

import (
	"strings"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestBuildGraphIntentSharedPromptBlocks_UsesSharedPipelineContracts(t *testing.T) {
	snapshot := horizonmodels.GraphIntentSnapshot{
		Nodes: []horizonmodels.GraphIntentSnapshotNode{
			// Legacy DOMAIN class is no longer in the AH taxonomy; the wire
			// payload still carries the legacy UPPER_SNAKE token "DOMAIN" for
			// back-compat (graph-intent prompt blocks must keep accepting it).
			{CanonicalPath: "/domain/v1/supply-chain/geo/de/product/ev-batteries", Type: "DOMAIN"},
		},
	}

	block, modes := buildGraphIntentSharedPromptBlocks(snapshot)
	if !strings.Contains(block, "NAME NORMALIZATION CONTRACT") {
		t.Fatal("expected naming contract block")
	}
	if !strings.Contains(block, "DOMAIN CONTRACT TYPES (shared with domain resolver)") {
		t.Fatal("expected domain contract block")
	}
	if !strings.Contains(block, "RELATION DIRECTION CONTRACT (shared with intent decomposition)") {
		t.Fatal("expected shared relation direction block")
	}
	if !strings.Contains(block, "ACTIVE DOMAIN MODES") {
		t.Fatal("expected active domain modes block")
	}
	if len(modes) == 0 || !taxonomy.HasDomainMode(modes, taxonomy.DomainModeSupplyChain) {
		t.Fatalf("expected supply_chain mode, got %v", modes)
	}
}

func TestGraphIntentNormalizeUnsupportedRelation_MapsUsedInToSuppliesToForActorTarget(t *testing.T) {
	got, _ := graphIntentNormalizeUnsupportedRelation(
		"USED_IN",
		graphIntentNodeRef{Type: string(ah.FragmentClassProduct)},
		graphIntentNodeRef{Type: string(ah.FragmentClassOrg)},
	)
	if got != "SUPPLIES_TO" {
		t.Fatalf("expected SUPPLIES_TO, got %q", got)
	}
}
