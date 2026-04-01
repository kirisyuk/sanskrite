package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestDownstreamTraversalProductFiltersDropAfterFirstDepth(t *testing.T) {
	filters := []string{"helium"}
	if got := downstreamTraversalProductFilters(filters, 0); len(got) != 1 || got[0] != "helium" {
		t.Fatalf("expected depth 0 to keep product filters, got %v", got)
	}
	if got := downstreamTraversalProductFilters(filters, 1); got != nil {
		t.Fatalf("expected depth 1 to drop product filters, got %v", got)
	}
}

func TestShouldKeepDownstreamProducedProductAllowsDescendantOutputsAfterFirstDepth(t *testing.T) {
	if !shouldKeepDownstreamProducedProduct("/product/raw/gas/refined/helium", []string{"helium"}, 0) {
		t.Fatal("expected depth 0 to keep the filtered anchor product")
	}
	if shouldKeepDownstreamProducedProduct("/product/finished/electronic/component/wafer", []string{"helium"}, 0) {
		t.Fatal("expected depth 0 to reject unrelated produced products")
	}
	if !shouldKeepDownstreamProducedProduct("/product/finished/electronic/component/wafer", []string{"helium"}, 1) {
		t.Fatal("expected depth 1 to allow descendant downstream products")
	}
}

func TestFilterDownstreamChainsForTraversalDoesNotDropDescendantSegmentsAfterFirstDepth(t *testing.T) {
	chains := []agents.SupplyChain{{
		Nodes: []agents.SupplyChainNode{
			{CanonicalPath: "/org/jp/fab-owner", Type: "ORGANIZATION"},
			{CanonicalPath: "/fac/jp/fab", Type: "FACILITY"},
			{CanonicalPath: "/product/finished/electronic/component/wafer", Type: "PRODUCT"},
			{CanonicalPath: "/fac/jp/chip-plant", Type: "FACILITY"},
			{CanonicalPath: "/org/jp/chipmaker", Type: "ORGANIZATION"},
		},
		Confidence: 0.8,
		Reason:     "downstream descendant segment",
	}}
	filteredDepth0 := filterDownstreamChainsForTraversal(chains, []string{"helium"}, nil, 0)
	if len(filteredDepth0) != 0 {
		t.Fatalf("expected depth 0 filtering to drop unrelated product segment, got %d chains", len(filteredDepth0))
	}
	filteredDepth1 := filterDownstreamChainsForTraversal(chains, []string{"helium"}, nil, 1)
	if len(filteredDepth1) != 1 {
		t.Fatalf("expected depth 1 filtering to preserve descendant segment, got %d chains", len(filteredDepth1))
	}
}
