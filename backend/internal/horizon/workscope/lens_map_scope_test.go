package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestLensScopePathsWithResolvedQueries_expandsQuery(t *testing.T) {
	q := "/query/sector:/Technology/**"
	lens := &horizonmodels.Lens{
		Scope: []string{"/org/a", q, "/org/b"},
		ResolvedQueries: map[string][]string{
			q: {"/org/x", "/org/y"},
		},
	}
	got := LensScopePathsWithResolvedQueries(lens)
	want := map[string]struct{}{"/org/a": {}, "/org/x": {}, "/org/y": {}, "/org/b": {}}
	if len(got) != len(want) {
		t.Fatalf("len got %d %v", len(got), got)
	}
	for _, p := range got {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing or extra: still %+v from got %v", want, got)
	}
}

func TestMergeMapNeighborWorkScope_unionsPaths(t *testing.T) {
	base := FromLensMapWorkScope("t", "p", "L1", []string{"/a"})
	got := MergeMapNeighborWorkScope(base, []string{"/b"}, []string{"id1"})
	if len(got.Refs.ScopePaths) != 2 {
		t.Fatalf("paths %v", got.Refs.ScopePaths)
	}
	if len(got.Refs.FragmentIDs) != 1 || got.Refs.FragmentIDs[0] != "id1" {
		t.Fatalf("fragment ids %v", got.Refs.FragmentIDs)
	}
}
