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

func TestLensResearchObjectPaths_sortedAndFiltered(t *testing.T) {
	lens := &horizonmodels.Lens{
		ResearchObjects: map[string]horizonmodels.LensResearchObjectRef{
			"/org/b":                   {CanonicalPath: "/org/b"},
			"/query/should-not-appear": {CanonicalPath: "/query/should-not-appear"},
			"/org/a":                   {},
		},
	}
	got := LensResearchObjectPaths(lens)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "/org/a" || got[1] != "/org/b" {
		t.Fatalf("unexpected order/content: %v", got)
	}
}

func TestLensResearchUniversePaths_unionsWorkbenchAndResearch(t *testing.T) {
	lens := &horizonmodels.Lens{
		Scope: []string{"/org/a"},
		ResearchObjects: map[string]horizonmodels.LensResearchObjectRef{
			"/org/a": {CanonicalPath: "/org/a"},
			"/org/c": {CanonicalPath: "/org/c"},
		},
		ViewConfig: map[string]any{
			"deferredScopePaths": []any{"/org/b"},
		},
	}
	got := LensResearchUniversePaths(lens)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	seen := map[string]bool{}
	for _, path := range got {
		seen[path] = true
	}
	if !seen["/org/a"] || !seen["/org/b"] || !seen["/org/c"] {
		t.Fatalf("unexpected union %v", got)
	}
}
