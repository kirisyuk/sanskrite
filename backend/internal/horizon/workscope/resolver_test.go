package workscope

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type mockMaterialized struct {
	byLens []*horizonmodels.MaterializedArtifact
}

func (m *mockMaterialized) ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if m == nil {
		return nil, nil
	}
	return m.byLens, nil
}

func (m *mockMaterialized) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	return nil, nil // unused in Resolve
}

func TestResolver_Resolve_unionsLensAndExplicit(t *testing.T) {
	m := &mockMaterialized{
		byLens: []*horizonmodels.MaterializedArtifact{
			{ID: "z2"},
			{ID: "y1"},
		},
	}
	r := NewResolver(m)
	ws := FromBaselineRefs("t", "", horizonmodels.SimulationBaselineRefs{
		LensID:      "L1",
		ArtifactIDs: []string{"a1"},
		ScopePaths:  []string{"/x"},
	})
	got, err := r.Resolve(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a1", "y1", "z2"}
	if len(got.MaterializedArtifactIDs) != len(want) {
		t.Fatalf("ids %v want %v", got.MaterializedArtifactIDs, want)
	}
	for i := range want {
		if got.MaterializedArtifactIDs[i] != want[i] {
			t.Fatalf("ids %v want %v", got.MaterializedArtifactIDs, want)
		}
	}
	if len(got.ScopePaths) != 1 || got.ScopePaths[0] != "/x" {
		t.Fatalf("scope paths: %v", got.ScopePaths)
	}
}

func TestResolver_Resolve_nilRepo_noLens(t *testing.T) {
	r := NewResolver(nil)
	ws := FromBaselineRefs("t", "", horizonmodels.SimulationBaselineRefs{
		ArtifactIDs: []string{"b", "a"},
	})
	got, err := r.Resolve(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MaterializedArtifactIDs) != 2 || got.MaterializedArtifactIDs[0] != "a" || got.MaterializedArtifactIDs[1] != "b" {
		t.Fatalf("ids %v", got.MaterializedArtifactIDs)
	}
}

func TestDigest_stableUnderEquivalentMerge(t *testing.T) {
	a := Normalize(WorkScope{
		TenantID: "tenant",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:      "L",
			ArtifactIDs: []string{"art_b", "art_a"},
		},
	})
	b := Normalize(WorkScope{
		TenantID: "tenant",
		Refs: horizonmodels.SimulationBaselineRefs{
			LensID:      "L",
			ArtifactIDs: []string{"art_a", "art_b"},
		},
	})
	ra := ResolvedFromScope(a)
	rb := ResolvedFromScope(b)
	if Digest(ra) != Digest(rb) {
		t.Fatalf("digest mismatch %s vs %s", Digest(ra), Digest(rb))
	}
}
