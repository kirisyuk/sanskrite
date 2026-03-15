package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildScopedContinuationChainContext_FiltersToAssemblyScope(t *testing.T) {
	job := &models.ScoutingJob{}
	job.State = map[string]interface{}{
		"chainContext": `{
			"discoveredNodes":["/org/kz/meloman-marwin","/fac/kz/almaty/distribution-center","/product/device/electronics/smartphone","/org/kz/unrelated"],
			"discoveredEdges":[
				{"source":"/org/kz/meloman-marwin","target":"/fac/kz/almaty/distribution-center","edgeType":"OWNS"},
				{"source":"/fac/kz/almaty/distribution-center","target":"/product/device/electronics/smartphone","edgeType":"PRODUCES"},
				{"source":"/org/kz/unrelated","target":"/product/device/electronics/smartphone","edgeType":"SUPPLIES_TO"}
			]
		}`,
		"openGapsToResearch": []interface{}{"display_branch_missing"},
	}
	parsed := &models.SupplyChainAssemblyGraphPayload{
		ProductNodes: []models.SupplyChainAssemblyNode{
			{Type: "product", Ref: "/product/device/electronics/smartphone"},
		},
		ActorNodes: []models.SupplyChainAssemblyNode{
			{Type: "org", Ref: "/org/kz/meloman-marwin"},
			{Type: "fac", Ref: "/fac/kz/almaty/distribution-center"},
		},
	}

	scoped := buildScopedContinuationChainContext(job, parsed, []string{"/fac/kz/almaty/distribution-center"})
	if scoped == nil {
		t.Fatal("expected scoped context")
	}
	if len(scoped.DiscoveredNodes) != 3 {
		t.Fatalf("len(scoped.DiscoveredNodes)=%d, want 3", len(scoped.DiscoveredNodes))
	}
	if len(scoped.DiscoveredEdges) != 2 {
		t.Fatalf("len(scoped.DiscoveredEdges)=%d, want 2", len(scoped.DiscoveredEdges))
	}
	if len(scoped.GapHints) != 1 || scoped.GapHints[0] != "display_branch_missing" {
		t.Fatalf("GapHints=%v", scoped.GapHints)
	}
}

func TestBuildSupplyChainAssemblyScopeRefs_MergesFrontiers(t *testing.T) {
	parsed := &models.SupplyChainAssemblyGraphPayload{
		ProductNodes: []models.SupplyChainAssemblyNode{
			{Type: "product", Ref: "/product/device/electronics/smartphone"},
		},
		ActorNodes: []models.SupplyChainAssemblyNode{
			{Type: "org", Ref: "/org/kz/meloman-marwin"},
		},
	}

	scope := buildSupplyChainAssemblyScopeRefs(parsed, []string{"/fac/kz/almaty/distribution-center"})
	if len(scope) != 3 {
		t.Fatalf("len(scope)=%d, want 3", len(scope))
	}
}
