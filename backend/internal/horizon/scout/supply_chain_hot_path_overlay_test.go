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

func TestBuildAnchorFocusedSupplyChainHotPathQueries_UsesCandidateAliases(t *testing.T) {
	query := agents.SupplyChainQuery{
		Anchor:        "Schlumberger",
		AnchorType:    "org",
		Direction:     "both",
		Depth:         4,
		ProductFilter: []string{"spare-parts"},
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		CandidateAnchorTerms: []string{"SLB", "M-I SWACO", "Schlumberger"},
	}

	variants := buildAnchorFocusedSupplyChainHotPathQueries(query, prePlan)
	if len(variants) != 2 {
		t.Fatalf("expected 2 novel anchor variants, got %d", len(variants))
	}
	if variants[0].Anchor != "SLB" {
		t.Fatalf("expected first alias variant to preserve SLB, got %#v", variants[0].Anchor)
	}
	if variants[1].Anchor != "M-I SWACO" {
		t.Fatalf("expected second alias variant to preserve M-I SWACO, got %#v", variants[1].Anchor)
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

func TestFilterSupplyChainsByAnchorScope_PrefersSubjectAndTargetGeo(t *testing.T) {
	chains := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/fac/us/cameron-houston-plant", Type: "FACILITY", Name: "Cameron Houston Plant"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Type: "PRODUCT", Name: "Oilfield Spare Parts"},
				{CanonicalPath: "/fac/ru/slb-tyumen-factory", Type: "FACILITY", Name: "SLB Tyumen Factory"},
			},
			Confidence: 0.82,
		},
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/fac/de/siemens-energy-muelheim-factory", Type: "FACILITY", Name: "Siemens Energy Muelheim Factory"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/gas-turbine-spare-parts", Type: "PRODUCT", Name: "Gas Turbine Spare Parts"},
				{CanonicalPath: "/fac/sa/qassim-2-power-plant", Type: "FACILITY", Name: "Qassim 2 Power Plant"},
			},
			Confidence: 0.88,
		},
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/us/slb", Type: "ORGANIZATION", Name: "SLB"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Type: "PRODUCT", Name: "Oilfield Spare Parts"},
				{CanonicalPath: "/org/ae/schlumberger-middle-east", Type: "ORGANIZATION", Name: "Schlumberger Middle East"},
			},
			Confidence: 0.71,
		},
	}
	query := agents.SupplyChainQuery{
		Anchor:     "Schlumberger",
		AnchorType: "org",
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		CandidateAnchorTerms: []string{"SLB", "Schlumberger Middle East"},
	}

	filtered := filterSupplyChainsByAnchorScope(chains, query, prePlan, []string{"ru"})
	if len(filtered) != 1 {
		t.Fatalf("expected only subject+target-geo chain to survive, got %d", len(filtered))
	}
	if got := filtered[0].Nodes[len(filtered[0].Nodes)-1].CanonicalPath; got != "/fac/ru/slb-tyumen-factory" {
		t.Fatalf("unexpected surviving chain target: %s", got)
	}
}

func TestFilterSupplyChainsByAnchorScope_FallsBackToAnchorOnlyWhenTargetGeoTooSparse(t *testing.T) {
	chains := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/us/slb", Type: "ORGANIZATION", Name: "SLB"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Type: "PRODUCT", Name: "Oilfield Spare Parts"},
				{CanonicalPath: "/org/ae/schlumberger-middle-east", Type: "ORGANIZATION", Name: "Schlumberger Middle East"},
			},
			Confidence: 0.76,
		},
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/fac/us/cameron-houston-plant", Type: "FACILITY", Name: "Cameron Houston Plant"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Type: "PRODUCT", Name: "Oilfield Spare Parts"},
				{CanonicalPath: "/fac/sa/dhahran-oilfield-hub", Type: "FACILITY", Name: "Dhahran Oilfield Hub"},
			},
			Confidence: 0.7,
		},
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/fac/de/siemens-energy-muelheim-factory", Type: "FACILITY", Name: "Siemens Energy Muelheim Factory"},
				{CanonicalPath: "/product/component/mechanical/manufacturing/gas-turbine-spare-parts", Type: "PRODUCT", Name: "Gas Turbine Spare Parts"},
				{CanonicalPath: "/fac/sa/qassim-2-power-plant", Type: "FACILITY", Name: "Qassim 2 Power Plant"},
			},
			Confidence: 0.88,
		},
	}
	query := agents.SupplyChainQuery{
		Anchor:     "Schlumberger",
		AnchorType: "org",
	}
	prePlan := &agents.SupplyChainPreHotPathPlan{
		CandidateAnchorTerms: []string{"SLB", "Schlumberger Middle East"},
	}

	filtered := filterSupplyChainsByAnchorScope(chains, query, prePlan, []string{"ru"})
	if len(filtered) != 1 {
		t.Fatalf("expected only anchor-related chain to survive when target geo is sparse, got %d", len(filtered))
	}
	for _, chain := range filtered {
		last := chain.Nodes[len(chain.Nodes)-1].CanonicalPath
		if last == "/fac/sa/qassim-2-power-plant" {
			t.Fatalf("unexpected unrelated chain survived anchor filtering")
		}
	}
}
