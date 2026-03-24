package scout

import (
	"context"
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

func TestMergeWorkingProductScopeFromChainsAppliesCandidateWorkflow(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{ID: "job-3", TenantID: "global", State: map[string]interface{}{}},
	}
	scope := ensureWorkingProductScopeSeeded(job, []string{"lithium chloride"}, "query_seed", "")
	scope = mergeWorkingProductScopeFromChains(job, []agents.SupplyChain{
		{
			Confidence: 0.84,
			Reason:     "grounded chain",
			ProductCandidates: []agents.ProductCandidate{
				{Name: "Lithium Chloride", Reason: "alias duplicate", Confidence: 0.81},
				{Name: "Lithium Hydroxide", Reason: "adjacent discovered product", Confidence: 0.79},
				{Name: "Brine Intermediate", Reason: "weak candidate", Confidence: 0.61},
				{Name: "Unrelated Solvent", Reason: "low confidence", Confidence: 0.31},
			},
		},
	}, "chain_discovery")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 2 {
		t.Fatalf("canonical refs = %v, want 2 active products (seeded + accepted candidate)", refs)
	}
	if !containsStringWithSuffixWP(refs, "lithium-hydroxide") {
		t.Fatalf("canonical refs = %v, want lithium-hydroxide accepted", refs)
	}
	if containsStringWithSuffixWP(refs, "brine-intermediate") {
		t.Fatalf("canonical refs = %v, stale candidate must not be active", refs)
	}
	if containsStringWithSuffixWP(refs, "unrelated-solvent") {
		t.Fatalf("canonical refs = %v, rejected candidate must not be active", refs)
	}
	entryByRef := map[string]models.WorkingProductEntry{}
	for _, entry := range scope.CanonicalProducts {
		entryByRef[entry.ProductRef] = entry
	}
	if staleRef := findRefWithSuffixWP(entryByRef, "brine-intermediate"); staleRef == "" || entryByRef[staleRef].Status != models.WorkingProductStatusStaleCandidate {
		t.Fatalf("brine intermediate entry missing or wrong status: %+v", entryByRef)
	}
	if rejectedRef := findRefWithSuffixWP(entryByRef, "unrelated-solvent"); rejectedRef == "" || entryByRef[rejectedRef].Status != models.WorkingProductStatusRejected {
		t.Fatalf("unrelated solvent entry missing or wrong status: %+v", entryByRef)
	}
	if !containsStringWithSuffixWP(scope.RejectedProductRefs, "unrelated-solvent") {
		t.Fatalf("rejected refs = %v, want unrelated-solvent present", scope.RejectedProductRefs)
	}
}

func TestEnsureWorkingProductScopeLoadedBackfillsLegacyWorkingProductRefs(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			ID:       "job-4",
			TenantID: "global",
			State: map[string]interface{}{
				"workingProductRefs": []string{"/product/material/lithium-chloride", "/product/material/lithium-hydroxide"},
			},
		},
	}
	scope := ensureWorkingProductScopeLoaded(context.Background(), nil, job, nil, "query_seed", "/domain/test")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 2 {
		t.Fatalf("canonical refs = %v, want 2 legacy refs backfilled", refs)
	}
	if _, ok := job.State[stateKeyWorkingProductScope]; !ok {
		t.Fatal("workingProductScope not mirrored into job state after legacy backfill")
	}
}

func TestWorkingProductScopeCollapsesEquivalentOperationalRefs(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{ID: "job-5", TenantID: "global", State: map[string]interface{}{}},
	}
	scope := ensureWorkingProductScopeSeeded(job, []string{"/product/finished/mechanical/assembly/lng"}, "query_seed", "")
	scope = mergeWorkingProductScopeFromChains(job, []agents.SupplyChain{
		{
			Confidence: 0.88,
			Reason:     "grounded LNG chain",
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "LNG"},
				{CanonicalPath: "/product/energy/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "Liquefied Natural Gas"},
			},
		},
	}, "chain_discovery")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 1 {
		t.Fatalf("canonical refs = %v, want 1 operational cluster", refs)
	}
	entry := scope.CanonicalProducts[0]
	if got := strings.TrimSpace(entry.PrimaryOperationalRef); got == "" || !strings.HasSuffix(got, "/lng") {
		t.Fatalf("primary operational ref = %q, want lng ref", got)
	}
	if strings.Contains(entry.PrimaryOperationalRef, "/finished/mechanical/assembly/") {
		t.Fatalf("primary operational ref = %q, want a more operational ref than finished/mechanical/assembly", entry.PrimaryOperationalRef)
	}
	if len(entry.EquivalentProductRefs) < 2 {
		t.Fatalf("equivalent refs = %v, want multiple equivalent LNG refs", entry.EquivalentProductRefs)
	}
}

func TestWorkingProductScopeCollapsesAliasAcronymEquivalence(t *testing.T) {
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{ID: "job-6", TenantID: "global", State: map[string]interface{}{}},
	}
	scope := ensureWorkingProductScopeSeeded(job, []string{"liquefied-natural-gas"}, "query_seed", "")
	scope = mergeWorkingProductScopeFromChains(job, []agents.SupplyChain{
		{
			Confidence: 0.86,
			Reason:     "grounded LNG chain",
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "LNG"},
			},
		},
	}, "chain_discovery")
	if scope == nil {
		t.Fatal("scope is nil")
	}
	refs := scope.CanonicalProductRefs()
	if len(refs) != 1 {
		t.Fatalf("canonical refs = %v, want 1 cluster after alias/acronym collapse", refs)
	}
	entry := scope.CanonicalProducts[0]
	if len(entry.EquivalentProductRefs) < 2 {
		t.Fatalf("equivalent refs = %v, want seeded alias and lng ref collapsed", entry.EquivalentProductRefs)
	}
}

func containsStringWithSuffixWP(values []string, suffix string) bool {
	for _, value := range values {
		if strings.HasSuffix(strings.TrimSpace(value), strings.TrimSpace(suffix)) {
			return true
		}
	}
	return false
}

func findRefWithSuffixWP(values map[string]models.WorkingProductEntry, suffix string) string {
	for ref := range values {
		if strings.HasSuffix(strings.TrimSpace(ref), strings.TrimSpace(suffix)) {
			return ref
		}
	}
	return ""
}
