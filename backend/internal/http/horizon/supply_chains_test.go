package horizon

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestExtractPreferredAssemblyPathKeys_PrefersDirectPaths(t *testing.T) {
	payload := horizonmodels.BuildSupplyChainAssemblyGraphPayload(
		[]string{"/product/component/mechanical/manufacturing/oilfield-spare-parts"},
		[]horizonmodels.SupplyChainAssemblyNode{
			{Type: "product", Ref: "/product/component/mechanical/manufacturing/oilfield-spare-parts"},
		},
		[]horizonmodels.SupplyChainAssemblyNode{
			{Type: "fac", Ref: "/fac/ae/jebel-ali-logistics-center", Role: "supplier"},
		},
		nil,
		nil,
		nil,
		nil,
		&horizonmodels.SupplyChainAssemblyCoverage{ProductsN: 1, ActorsN: 1, DerivedPathsN: 2},
		nil,
		[]string{
			"/supply-chain/global/derived/noisy-1",
			"/supply-chain/global/derived/noisy-2",
		},
		nil,
	)
	payload["directPathKeys"] = []string{
		"/supply-chain/global/direct/a",
		"/supply-chain/global/direct/b",
	}
	artifact := &horizonmodels.MaterializedArtifact{
		Kind: horizonmodels.MaterializedKindSupplyChainAssemblyGraph,
		Payload: payload,
	}

	got := extractPreferredAssemblyPathKeys(artifact)
	want := []string{
		"/supply-chain/global/direct/a",
		"/supply-chain/global/direct/b",
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want=%d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want=%q full=%v", i, got[i], want[i], got)
		}
	}
}

func TestExtractPreferredAssemblyPathKeys_FallsBackToDerivedPaths(t *testing.T) {
	payload := horizonmodels.BuildSupplyChainAssemblyGraphPayload(
		[]string{"/product/component/mechanical/manufacturing/oilfield-spare-parts"},
		[]horizonmodels.SupplyChainAssemblyNode{
			{Type: "product", Ref: "/product/component/mechanical/manufacturing/oilfield-spare-parts"},
		},
		[]horizonmodels.SupplyChainAssemblyNode{
			{Type: "fac", Ref: "/fac/ae/jebel-ali-logistics-center", Role: "supplier"},
		},
		nil,
		nil,
		nil,
		nil,
		&horizonmodels.SupplyChainAssemblyCoverage{ProductsN: 1, ActorsN: 1, DerivedPathsN: 2},
		nil,
		[]string{
			"/supply-chain/global/derived/a",
			"/supply-chain/global/derived/b",
			"/supply-chain/global/derived/a",
		},
		nil,
	)
	artifact := &horizonmodels.MaterializedArtifact{
		Kind: horizonmodels.MaterializedKindSupplyChainAssemblyGraph,
		Payload: payload,
	}

	got := extractPreferredAssemblyPathKeys(artifact)
	want := []string{
		"/supply-chain/global/derived/a",
		"/supply-chain/global/derived/b",
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want=%d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want=%q full=%v", i, got[i], want[i], got)
		}
	}
}
