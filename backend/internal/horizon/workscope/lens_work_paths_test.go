package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestLensWorkbenchScopePaths_queryAndDeferred(t *testing.T) {
	lens := &horizonmodels.Lens{
		Scope: []string{"/query/q1", "/org/a"},
		ResolvedQueries: map[string][]string{
			"/query/q1": {"/org/b"},
		},
		ViewConfig: map[string]any{
			"deferredScopePaths": []any{"/org/c", ""},
		},
	}
	got := LensWorkbenchScopePaths(lens)
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		seen[p] = true
	}
	if !seen["/org/a"] || !seen["/org/b"] || !seen["/org/c"] {
		t.Fatalf("missing paths: %v", got)
	}
}

func TestLensSupplyChainNodeRefs_mergeResolved(t *testing.T) {
	lens := &horizonmodels.Lens{
		Scope: []string{"/query/q"},
		ResolvedQueries: map[string][]string{
			"/query/q": {"/fac/x"},
		},
		ScopeFragments: map[string]string{"/org/y": "id1"},
	}
	got := LensSupplyChainNodeRefs(lens)
	if len(got) < 2 {
		t.Fatalf("got %v", got)
	}
}
