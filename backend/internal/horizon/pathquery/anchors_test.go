package pathquery

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFilterArtifactsByAnchors(t *testing.T) {
	a1 := &horizonmodels.MaterializedArtifact{
		ID:       "1",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a", "/product/p"},
	}
	a2 := &horizonmodels.MaterializedArtifact{ID: "2", Kind: horizonmodels.MaterializedKindSupplyChainPath, NodeRefs: []string{"/org/z"}}
	out := filterArtifactsByAnchors([]*horizonmodels.MaterializedArtifact{a1, a2}, []string{"/org/a", "/org/b"})
	if len(out) != 0 {
		t.Fatalf("want empty, got %d", len(out))
	}
	out = filterArtifactsByAnchors([]*horizonmodels.MaterializedArtifact{a1, a2}, []string{"/org/a"})
	if len(out) != 1 || out[0].ID != "1" {
		t.Fatalf("got %#v", out)
	}
}

func TestPayloadContainsSupplyLinkAnchor(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{SupplyLinkCanonicalPath: "/supply-link/x"}
	parsed := &horizonmodels.SupplyChainPathPayload{Legs: []horizonmodels.SupplyChainPathLeg{leg}}
	if !payloadContainsSupplyLinkAnchor(parsed, "/supply-link/x") {
		t.Fatal()
	}
	if payloadContainsSupplyLinkAnchor(nil, "/supply-link/x") {
		t.Fatal()
	}
}

func TestFilterByProductAxis(t *testing.T) {
	d := Diagnostics{}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, ProductRef: "/product/x"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	a := &horizonmodels.MaterializedArtifact{
		ID:      "1",
		Kind:    horizonmodels.MaterializedKindSupplyChainPath,
		Payload: payload,
	}
	out := filterByProductAxis([]*horizonmodels.MaterializedArtifact{a}, "/product/x", &d)
	if len(out) != 1 || !d.ProductAxisFiltered {
		t.Fatalf("got %d filtered=%v", len(out), d.ProductAxisFiltered)
	}
}
