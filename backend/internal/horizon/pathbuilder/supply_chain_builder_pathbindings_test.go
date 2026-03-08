package pathbuilder

import (
	"context"
	"testing"
)

func TestPathToMaterializedPayload_IncludesViaNodesAndLegs(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{{
			SupplierRef:      "/fac/ae/jebel-ali-terminal",
			ProductRef:       "/product/raw/hydrocarbon/distribution/lng",
			CustomerRef:      "/fac/in/mundra-terminal",
			TransportViaRefs: []string{"/fac/sea/strait-of-hormuz-terminal"},
			Confidence:       0.86,
		}},
		Confidence: 0.86,
		BuildMode:  "test",
		MaxDepth:   3,
	}
	nodes, edges, legs, stress := builder.pathToMaterializedPayload(context.Background(), path)
	if len(legs) != 1 {
		t.Fatalf("legs=%d, want 1", len(legs))
	}
	foundViaNode := false
	for _, node := range nodes {
		if node.Ref == "/fac/sea/strait-of-hormuz-terminal" {
			foundViaNode = true
			break
		}
	}
	if !foundViaNode {
		t.Fatal("expected transport via node in payload")
	}
	foundRouteEdge := false
	foundProductCarryingLogistics := false
	for _, edge := range edges {
		if edge.Type == "LOGISTICS" {
			foundRouteEdge = true
			if edge.CarriesProduct && edge.ProductRef == "/product/raw/hydrocarbon/distribution/lng" && edge.Role == "logistics_leg" {
				foundProductCarryingLogistics = true
			}
		}
	}
	if !foundRouteEdge {
		t.Fatal("expected logistics edges in payload")
	}
	if !foundProductCarryingLogistics {
		t.Fatal("expected product-carrying logistics edge in payload")
	}
	foundQualifiedEdge := false
	for _, edge := range edges {
		if edge.Type != "LOGISTICS" || edge.Qualifiers == nil {
			continue
		}
		if edge.Qualifiers["flow_refs"] != nil && edge.Qualifiers["binding_confidence"] != nil && edge.Qualifiers["path_stress_score"] != nil {
			foundQualifiedEdge = true
			break
		}
	}
	if !foundQualifiedEdge {
		t.Fatal("expected path-centric qualifiers on logistics edge")
	}
	if len(legs[0].TransportViaNodeIndices) == 0 {
		t.Fatal("expected transportViaNodeIndices on leg")
	}
	if len(legs[0].FlowRefs) == 0 {
		t.Fatal("expected flow bindings on leg")
	}
	if stress == nil || stress.PathScore <= 0 {
		t.Fatal("expected path stress summary")
	}
	if legs[0].Stress == nil || legs[0].Stress.StressScore <= 0 {
		t.Fatal("expected leg stress")
	}
}
