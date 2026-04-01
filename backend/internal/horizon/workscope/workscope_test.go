package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalize_dedupAndSort(t *testing.T) {
	s := WorkScope{
		TenantID: " t1 ",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:     " L1 ",
			ScopePaths: []string{" /b ", "/a", "/b", ""},
		},
		Intent: WorkScopeIntent{
			ScopeKind: ScopeKindLensMap,
			Expansion: WorkScopeExpansionPolicy{
				Mode:               " map_neighbors ",
				NeighborHops:       1,
				AllowDeferredPaths: true,
			},
		},
	}
	got := Normalize(s)
	if got.TenantID != "t1" {
		t.Fatalf("tenant: got %q", got.TenantID)
	}
	if got.Refs.LensID != "L1" {
		t.Fatalf("lens: got %q", got.Refs.LensID)
	}
	want := []string{"/a", "/b"}
	if len(got.Refs.ScopePaths) != len(want) {
		t.Fatalf("scope paths: got %v want %v", got.Refs.ScopePaths, want)
	}
	for i := range want {
		if got.Refs.ScopePaths[i] != want[i] {
			t.Fatalf("scope paths: got %v want %v", got.Refs.ScopePaths, want)
		}
	}
	if got.Intent.ScopeKind != ScopeKindLensMap {
		t.Fatalf("scope kind not normalized: %+v", got.Intent)
	}
	if got.Intent.Expansion.Mode != "map_neighbors" || got.Intent.Expansion.NeighborHops != 1 || !got.Intent.Expansion.AllowDeferredPaths {
		t.Fatalf("expansion not normalized: %+v", got.Intent.Expansion)
	}
}

func TestMerge_unionAndPreferFirstContext(t *testing.T) {
	a := WorkScope{
		TenantID:  "t1",
		ProjectID: "p1",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:     "L1",
			ScopePaths: []string{"/x"},
		},
		Intent: WorkScopeIntent{
			ScopeKind: ScopeKindLensStrict,
		},
	}
	b := WorkScope{
		TenantID:  "",
		ProjectID: "p2",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:      "",
			ScopePaths:  []string{"/y", "/x"},
			FragmentIDs: []string{"f1"},
		},
		Intent: WorkScopeIntent{
			Expansion: WorkScopeExpansionPolicy{
				Mode:         "map_neighbors",
				NeighborHops: 1,
			},
		},
	}
	got := Merge(a, b)
	if got.TenantID != "t1" || got.ProjectID != "p1" {
		t.Fatalf("context: tenant=%q project=%q", got.TenantID, got.ProjectID)
	}
	if got.Refs.LensID != "L1" {
		t.Fatalf("lens: got %q", got.Refs.LensID)
	}
	if len(got.Refs.ScopePaths) != 2 || got.Refs.ScopePaths[0] != "/x" || got.Refs.ScopePaths[1] != "/y" {
		t.Fatalf("scope paths: %v", got.Refs.ScopePaths)
	}
	if len(got.Refs.FragmentIDs) != 1 || got.Refs.FragmentIDs[0] != "f1" {
		t.Fatalf("fragment ids: %v", got.Refs.FragmentIDs)
	}
	if got.Intent.ScopeKind != ScopeKindLensStrict {
		t.Fatalf("intent context lost: %+v", got.Intent)
	}
	if got.Intent.Expansion.NeighborHops != 1 {
		t.Fatalf("expansion not merged: %+v", got.Intent.Expansion)
	}
}

func TestRoundTripBaselineRefs(t *testing.T) {
	refs := horizonmodels.SimulationBaselineRefs{
		LensID:        "L",
		ScopePaths:    []string{"/p/1"},
		FragmentPaths: []string{"/org/x"},
		ArtifactIDs:   []string{"a1"},
	}
	w := FromBaselineRefs("tenant", "proj", refs)
	refs2 := ToBaselineRefs(w)
	if refs2.LensID != "L" {
		t.Fatalf("lens %q", refs2.LensID)
	}
	if len(refs2.ScopePaths) != 1 || refs2.ScopePaths[0] != "/p/1" {
		t.Fatalf("scope %v", refs2.ScopePaths)
	}
	if w.TenantID != "tenant" || w.ProjectID != "proj" {
		t.Fatalf("context lost")
	}
}

func TestDefaultRequestForScope_lensResearchUsesLensEntryPoint(t *testing.T) {
	scope := WorkScope{
		TenantID: "t1",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:     "L1",
			ScopePaths: []string{"/org/a"},
		},
		Intent: WorkScopeIntent{
			ScopeKind: ScopeKindLensResearch,
			Expansion: WorkScopeExpansionPolicy{
				Mode: "lens_research",
			},
		},
	}
	request := DefaultRequestForScope(scope)
	if request.Intent.EntryPoint != EntryPointLens {
		t.Fatalf("entry point: %q", request.Intent.EntryPoint)
	}
	if request.Intent.Action != ActionInspect {
		t.Fatalf("action: %q", request.Intent.Action)
	}
	if request.Intent.Projection.Mode != ProjectionModeScopeOnly {
		t.Fatalf("projection: %+v", request.Intent.Projection)
	}
}

func TestMerge_equivalentOrderIndependentDigest(t *testing.T) {
	x := FromBaselineRefs("t", "", horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{"/a", "/b"},
	})
	y := FromBaselineRefs("t", "", horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{"/b", "/a"},
	})
	if x.Refs.ScopePaths[0] != y.Refs.ScopePaths[0] || x.Refs.ScopePaths[1] != y.Refs.ScopePaths[1] {
		t.Fatalf("normalize should align order: %v vs %v", x.Refs.ScopePaths, y.Refs.ScopePaths)
	}
}
