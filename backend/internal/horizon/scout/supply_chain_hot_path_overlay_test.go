package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestMergeSupplyChainObservationOverlays_PreservesGraphAndAddsCapabilityFacts(t *testing.T) {
	base := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/qa/qatarenergy", Type: "ORGANIZATION", Name: "QatarEnergy"},
				{CanonicalPath: "/fac/qa/ras-laffan-lng-complex", Type: "FACILITY", Name: "Ras Laffan LNG Complex"},
				{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "LNG"},
				{CanonicalPath: "/fac/gb/south-hook-terminal", Type: "FACILITY", Name: "South Hook LNG Terminal"},
			},
			TransportVia: []string{"/fac/eg/suez-terminal"},
		},
	}
	overlay := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/qa/qatarenergy", Type: "ORGANIZATION", Name: "QatarEnergy"},
				{CanonicalPath: "/fac/qa/ras-laffan-lng-complex", Type: "FACILITY", Name: "Ras Laffan LNG Complex"},
				{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "LNG"},
				{CanonicalPath: "/fac/gb/south-hook-terminal", Type: "FACILITY", Name: "South Hook LNG Terminal"},
			},
			CarrierProductObservations: []agents.CarrierProductObservation{
				{
					CarrierRef: "/fac/qa/ras-laffan-lng-complex",
					ProductRef: "/product/raw/hydrocarbon/liquefaction/lng",
					Metrics: map[string]interface{}{
						"annual_capacity": map[string]interface{}{"value": 77.0, "unit": "metric_tons_per_year"},
					},
					Confidence: 0.8,
				},
			},
		},
	}

	merged := mergeSupplyChainObservationOverlays(base, overlay)
	if len(merged) != 1 {
		t.Fatalf("expected one merged chain, got %d", len(merged))
	}
	if len(merged[0].Nodes) != 4 {
		t.Fatalf("expected original graph nodes to remain intact, got %d", len(merged[0].Nodes))
	}
	if len(merged[0].CarrierProductObservations) != 1 {
		t.Fatalf("expected capability overlay to be merged into existing chain")
	}
	if len(merged[0].TransportVia) != 1 || merged[0].TransportVia[0] != "/fac/eg/suez-terminal" {
		t.Fatalf("expected transportVia to be preserved, got %#v", merged[0].TransportVia)
	}
}

func TestApplyScopedObservationBundleToSupplyChains_AttachesCapabilityToMatchingChain(t *testing.T) {
	chains := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/qa/qatarenergy", Type: "ORGANIZATION", Name: "QatarEnergy"},
				{CanonicalPath: "/fac/qa/ras-laffan-lng-complex", Type: "FACILITY", Name: "Ras Laffan LNG Complex"},
				{CanonicalPath: "/product/raw/hydrocarbon/liquefaction/lng", Type: "PRODUCT", Name: "LNG"},
				{CanonicalPath: "/fac/gb/south-hook-terminal", Type: "FACILITY", Name: "South Hook LNG Terminal"},
			},
		},
	}
	bundle := &agents.SupplyChainScopedObservations{
		CarrierProductObservations: []agents.CarrierProductObservation{
			{
				CarrierRef: "/fac/qa/ras-laffan-lng-complex",
				ProductRef: "/product/raw/hydrocarbon/liquefaction/lng",
				Metrics: map[string]interface{}{
					"annual_capacity": map[string]interface{}{"value": 77.0, "unit": "metric_tons_per_year"},
				},
				Confidence: 0.83,
			},
		},
	}

	applied := applyScopedObservationBundleToSupplyChains(chains, bundle)
	if len(applied) != 1 {
		t.Fatalf("expected one chain after applying bundle, got %d", len(applied))
	}
	if len(applied[0].CarrierProductObservations) != 1 {
		t.Fatalf("expected one carrier observation attached to chain")
	}
	if _, ok := applied[0].CarrierProductObservations[0].Metrics["annual_capacity"]; !ok {
		t.Fatalf("expected annual_capacity metric to stay attached")
	}
}

func TestBuildFocusedSupplyChainHotPathQuery_UsesDerivedProducts(t *testing.T) {
	query := agents.SupplyChainQuery{
		Anchor:        "/org/de/siemens-energy",
		AnchorType:    "org",
		Direction:     "upstream",
		Depth:         4,
		ProductFilter: []string{"spare parts"},
		Reason:        "test",
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		Strategy:              "oem_aftermarket",
		DerivedProductFilters: []string{"turbine blades", "combustion system parts"},
	}

	focused := buildFocusedSupplyChainHotPathQuery(query, prePlan)
	if focused == nil {
		t.Fatalf("expected focused query variant")
	}
	if len(focused.ProductFilter) != 2 {
		t.Fatalf("expected focused variant to replace broad filter with derived filters, got %#v", focused.ProductFilter)
	}
	if focused.ProductFilter[0] != "turbine blades" {
		t.Fatalf("expected first derived filter to be preserved, got %#v", focused.ProductFilter)
	}
	if focused.Reason == query.Reason {
		t.Fatalf("expected focused query reason to include pre-hot-path marker")
	}
}

func TestBuildFocusedSupplyChainHotPathQuery_FallsBackToCandidateProductFamilies(t *testing.T) {
	query := agents.SupplyChainQuery{
		Anchor:        "/product/raw/gas/helium",
		AnchorType:    "product",
		Direction:     "both",
		Depth:         4,
		ProductFilter: []string{"helium"},
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		Strategy:                 "industrial_components",
		CandidateProductFamilies: []string{"liquid helium", "industrial helium supply"},
	}

	focused := buildFocusedSupplyChainHotPathQuery(query, prePlan)
	if focused == nil {
		t.Fatalf("expected focused query variant from candidate product families")
	}
	if len(focused.ProductFilter) != 2 {
		t.Fatalf("expected candidate families to become focused filters, got %#v", focused.ProductFilter)
	}
	if focused.ProductFilter[0] != "liquid helium" {
		t.Fatalf("expected first candidate family to be preserved, got %#v", focused.ProductFilter)
	}
}

func TestDeriveSupplyChainHotPathVariants_DedupsOriginalWhenNoNovelDerivedFilters(t *testing.T) {
	query := agents.SupplyChainQuery{
		Anchor:        "/org/qa/qatarenergy",
		AnchorType:    "org",
		Direction:     "down",
		Depth:         4,
		ProductFilter: []string{"lng"},
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		Strategy:              "commodity_corridor",
		DerivedProductFilters: []string{"lng"},
	}

	variants := deriveSupplyChainHotPathVariants(query, prePlan)
	if len(variants) != 1 {
		t.Fatalf("expected only original query when derived filters are not novel, got %d", len(variants))
	}
}
