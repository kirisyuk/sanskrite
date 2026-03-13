package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
)

func TestBuildSupplyChainFamilyBatches_AssignsSharedFamily(t *testing.T) {
	pathA := &pathbuilder.SupplyChainPath{
		Segments: []pathbuilder.PathSegment{
			{SupplierRef: "/org/us/supplier-a", ProductRef: "/product/component/test/battery-cell", CustomerRef: "/org/de/bmw-ag"},
		},
	}
	pathB := &pathbuilder.SupplyChainPath{
		Segments: []pathbuilder.PathSegment{
			{SupplierRef: "/org/jp/supplier-b", ProductRef: "/product/component/test/battery-cell", CustomerRef: "/org/de/bmw-ag"},
		},
	}
	families := buildSupplyChainFamilyBatches("global", 1, []supplyChainFamilyCandidate{
		{
			canonicalKey: "/supply-chain/global/a",
			path:         pathA,
			sufficiency: &models.SupplyChainPathSufficiency{
				Status:   models.SupplyChainSufficiencyHeuristic,
				OpenGaps: []string{"Resolve transport evidence"},
			},
		},
		{
			canonicalKey: "/supply-chain/global/b",
			path:         pathB,
			sufficiency: &models.SupplyChainPathSufficiency{
				Status: models.SupplyChainSufficiencyValidated,
			},
		},
	})
	if len(families) != 1 {
		t.Fatalf("families=%d, want 1", len(families))
	}
	for _, batch := range families {
		if batch.PathCount != 2 {
			t.Fatalf("pathCount=%d, want 2", batch.PathCount)
		}
		if batch.ValidatedPathCount != 1 {
			t.Fatalf("validated=%d, want 1", batch.ValidatedPathCount)
		}
	}
	if pathA.Family == nil || pathB.Family == nil {
		t.Fatal("expected family metadata on both paths")
	}
	if pathA.Family.ID != pathB.Family.ID {
		t.Fatalf("family ids differ: %s vs %s", pathA.Family.ID, pathB.Family.ID)
	}
}
