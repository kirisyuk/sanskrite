package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFromScoutingJobParams_mergesLensTargetSeeds(t *testing.T) {
	p := &horizonmodels.ScoutingJobParams{
		LensID:              "L1",
		TenantID:            "t1",
		ProjectID:           "p1",
		TargetCanonicalPath: "/org/a",
		SeedHints:           []string{"/domain/b"},
		WorkScopeRefs: horizonmodels.SimulationBaselineRefs{
			FragmentPaths: []string{"/product/c"},
		},
	}
	got := FromScoutingJobParams(p)
	if got.TenantID != "t1" || got.ProjectID != "p1" || got.Refs.LensID != "L1" {
		t.Fatalf("context: %+v", got)
	}
	if len(got.Refs.ScopePaths) != 2 {
		t.Fatalf("scope paths %v", got.Refs.ScopePaths)
	}
	if len(got.Refs.FragmentPaths) != 1 || got.Refs.FragmentPaths[0] != "/product/c" {
		t.Fatalf("fragment paths %v", got.Refs.FragmentPaths)
	}
}
