package taxonomy

import "testing"

func TestResolveIndustryKey_AmbiguousAliasRejected(t *testing.T) {
	prev := GetIndustryRegistry()
	t.Cleanup(func() { SetIndustryRegistry(prev) })

	SetIndustryRegistry(&IndustryRegistry{
		Version: "test",
		Nodes: map[string]IndustryNode{
			"/industry/energy-storage/batteries/lithium-ion": {
				Path:            "/industry/energy-storage/batteries/lithium-ion",
				Status:          "active",
				AllowedForTypes: allowedIndustryFragmentTypes,
			},
			"/industry/energy-storage/batteries/lithium-ion/lfp": {
				Path:            "/industry/energy-storage/batteries/lithium-ion/lfp",
				Status:          "active",
				AllowedForTypes: allowedIndustryFragmentTypes,
			},
		},
		Aliases: map[string]map[string]string{
			TaxonomyAliasKeyKindLabel: {
				"lithium ion": "/industry/energy-storage/batteries/lithium-ion",
			},
		},
		Ambiguous: map[string]map[string]struct{}{
			TaxonomyAliasKeyKindLabel: {
				"battery": {},
			},
		},
	})

	if _, ok := ResolveIndustryKey(TaxonomyNamespaceIndustry, TaxonomyAliasKeyKindLabel, "battery"); ok {
		t.Fatalf("expected ambiguous alias to be rejected")
	}
	if got, ok := ResolveIndustryKey(TaxonomyNamespaceIndustry, TaxonomyAliasKeyKindLabel, "lithium-ion"); !ok || got != "/industry/energy-storage/batteries/lithium-ion" {
		t.Fatalf("expected lithium-ion alias to resolve, got=%q ok=%v", got, ok)
	}
}

func TestNormalizeIndustryPaths_AllowedFragmentTypesOnly(t *testing.T) {
	prev := GetIndustryRegistry()
	t.Cleanup(func() { SetIndustryRegistry(prev) })

	SetIndustryRegistry(&IndustryRegistry{
		Version: "test",
		Nodes: map[string]IndustryNode{
			"/industry/energy-storage": {
				Path:            "/industry/energy-storage",
				Status:          "active",
				AllowedForTypes: allowedIndustryFragmentTypes,
			},
			"/industry/energy-storage/batteries": {
				Path:            "/industry/energy-storage/batteries",
				Status:          "active",
				AllowedForTypes: allowedIndustryFragmentTypes,
			},
			"/industry/energy-storage/batteries/lithium-ion": {
				Path:            "/industry/energy-storage/batteries/lithium-ion",
				Status:          "active",
				AllowedForTypes: allowedIndustryFragmentTypes,
			},
		},
	})

	paths := []string{
		"/industry/energy-storage/batteries/lithium-ion",
		"/industry/energy-storage/batteries/lithium-ion",
		"/industry/unknown/path",
	}
	got := NormalizeIndustryPaths(paths, "ORGANIZATION")
	if len(got) != 1 || got[0] != "/industry/energy-storage/batteries/lithium-ion" {
		t.Fatalf("unexpected normalized paths: %#v", got)
	}

	if blocked := NormalizeIndustryPaths(paths, "MARKET"); blocked != nil {
		t.Fatalf("expected MARKET to reject industryPaths, got %#v", blocked)
	}
}
