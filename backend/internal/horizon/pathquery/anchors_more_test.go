package pathquery

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestMergeUniqueStrings(t *testing.T) {
	got := mergeUniqueStrings([]string{"a"}, []string{"a", "b"})
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestUnionNodeRefs(t *testing.T) {
	a := &horizonmodels.MaterializedArtifact{NodeRefs: []string{"/a", "/b"}}
	b := &horizonmodels.MaterializedArtifact{NodeRefs: []string{"/b", "/c"}}
	refs := unionNodeRefsFromArtifacts([]*horizonmodels.MaterializedArtifact{a, b})
	if len(refs) != 3 {
		t.Fatalf("%v", refs)
	}
}

func TestUnionNodeRefs_includesSupplyPathLegRefs(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	a := &horizonmodels.MaterializedArtifact{
		Kind:    horizonmodels.MaterializedKindSupplyChainPath,
		Payload: payload,
	}
	refs := unionNodeRefsFromArtifacts([]*horizonmodels.MaterializedArtifact{a})
	got := map[string]bool{}
	for _, r := range refs {
		got[r] = true
	}
	if !got["/org/a"] || !got["/org/b"] {
		t.Fatalf("want leg refs, got %v", refs)
	}
}

func TestHasSupplyLinkAnchor(t *testing.T) {
	if !hasSupplyLinkAnchor([]string{"/supply-link/x"}) {
		t.Fatal()
	}
	if hasSupplyLinkAnchor([]string{"/org/x"}) {
		t.Fatal()
	}
}

func TestLegMatchesAnchor(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{SupplierRef: "/org/a"}
	if !legMatchesAnchor(leg, "/org/a") {
		t.Fatal()
	}
}
