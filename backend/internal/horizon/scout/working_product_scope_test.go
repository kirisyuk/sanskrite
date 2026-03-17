package scout

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestEnsureWorkingProductScopeSeededCanonicalizesAndDeduplicates(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{ID: "job-1", TenantID: "global", State: map[string]interface{}{}},
	}
	scope := ensureWorkingProductScopeSeeded(job, []string{"lithium carbonate", "/product/raw/lithium-carbonate", "lithium-carbonate"}, "query_seed", "/domain/v1/supply-chain")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 1 {
		t.Fatalf("canonical refs = %v, want single deduped product", refs)
	}
	if !strings.Contains(refs[0], "lithium-carbonate") {
		t.Fatalf("unexpected canonical ref %q", refs[0])
	}
	if _, ok := job.State[stateKeyWorkingProductScope]; !ok {
		t.Fatal("working product scope not mirrored to job state")
	}
}

func TestMergeWorkingProductScopeFromChainsAddsDiscoveredProducts(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{ID: "job-2", TenantID: "global", State: map[string]interface{}{}},
	}
	ensureWorkingProductScopeSeeded(job, []string{"battery-cells"}, "query_seed", "")
	scope := mergeWorkingProductScopeFromChains(job, []agents.SupplyChain{
		{
			Confidence: 0.82,
			Reason:     "supplier discovered in grounded chain",
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/cn/catl", Type: "ORGANIZATION", Name: "CATL"},
				{CanonicalPath: "/product/finished/battery-cells", Type: "PRODUCT", Name: "Battery Cells"},
				{CanonicalPath: "/org/de/bmw", Type: "ORGANIZATION", Name: "BMW"},
				{CanonicalPath: "/product/raw/lithium-carbonate", Type: "PRODUCT", Name: "Lithium Carbonate"},
			},
		},
	}, "chain_discovery")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 2 {
		t.Fatalf("canonical refs = %v, want 2 discovered products", refs)
	}
}
