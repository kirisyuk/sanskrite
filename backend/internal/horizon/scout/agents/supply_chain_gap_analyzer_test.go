package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func lithiumBatteryChain() SupplyChain {
	return SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/au/pilbara-minerals", Name: "Pilbara Minerals", Type: "ORGANIZATION", Role: "supplier"},
			{CanonicalPath: "/product/spodumene-concentrate", Name: "Spodumene Concentrate", Type: "PRODUCT"},
			{CanonicalPath: "/org/cn/ganfeng-lithium", Name: "Ganfeng Lithium", Type: "ORGANIZATION", Role: "processor"},
			{CanonicalPath: "/product/lithium-carbonate", Name: "Lithium Carbonate", Type: "PRODUCT"},
			{CanonicalPath: "/org/kr/lg-energy-solution", Name: "LG Energy Solution", Type: "ORGANIZATION", Role: "manufacturer"},
			{CanonicalPath: "/product/ev-battery-cell", Name: "EV Battery Cell", Type: "PRODUCT"},
			{CanonicalPath: "/org/us/tesla", Name: "Tesla", Type: "ORGANIZATION", Role: "customer"},
		},
		Confidence: 0.8,
		Reason:     "lithium to EV battery supply chain",
		SegmentMetrics: []SegmentMetrics{
			{FromNodeIndex: 0, ToNodeIndex: 1, Confidence: 0.7, Volume: &MetricWithConfidence{Value: 300000, Unit: "metric_tons_per_year", Confidence: 0.6}, Period: "2023"},
			{FromNodeIndex: 2, ToNodeIndex: 3, Confidence: 0.7, Volume: &MetricWithConfidence{Value: 50000, Unit: "metric_tons_per_year", Confidence: 0.5}},
		},
		Alternatives: []AlternativeSuppliers{
			{CustomerNodeIndex: 4, ProductNodeIndex: 3, Suppliers: []AlternativeContract{
				{SupplierRef: "/org/cl/sqm", TechnicalFit: 0.8, RegulatoryFit: 0.9, Confidence: 0.6},
			}},
		},
	}
}

func shortCommodityChain() SupplyChain {
	return SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/sa/aramco", Name: "Saudi Aramco", Type: "ORGANIZATION", Role: "supplier"},
			{CanonicalPath: "/product/crude-oil", Name: "Crude Oil", Type: "PRODUCT"},
			{CanonicalPath: "/org/nl/shell", Name: "Shell", Type: "ORGANIZATION", Role: "customer"},
		},
		Confidence: 0.9,
	}
}

func singleSourceChain() SupplyChain {
	return SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/tw/tsmc", Name: "TSMC", Type: "ORGANIZATION", Role: "supplier"},
			{CanonicalPath: "/product/advanced-chips", Name: "Advanced Chips (3nm)", Type: "PRODUCT"},
			{CanonicalPath: "/org/us/apple", Name: "Apple", Type: "ORGANIZATION", Role: "customer"},
		},
		Confidence: 0.85,
	}
}

func multiRegionNoLogistics() SupplyChain {
	return SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/au/bhp", Name: "BHP", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/iron-ore", Name: "Iron Ore", Type: "PRODUCT"},
			{CanonicalPath: "/org/cn/baosteel", Name: "Baosteel", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/steel-slab", Name: "Steel Slab", Type: "PRODUCT"},
			{CanonicalPath: "/org/de/thyssenkrupp", Name: "ThyssenKrupp", Type: "ORGANIZATION"},
		},
		Confidence: 0.7,
	}
}

// ---------------------------------------------------------------------------
// Deterministic check tests
// ---------------------------------------------------------------------------

func TestCheckTierDepth_ManufacturingShort(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/x", Name: "Supplier", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/raw", Name: "Raw Material", Type: "PRODUCT"},
			{CanonicalPath: "/org/y", Name: "Assembler", Type: "ORGANIZATION", Role: "assembler"},
			{CanonicalPath: "/product/final", Name: "Final Product", Type: "PRODUCT"},
		},
		Confidence: 0.8,
	}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkTierDepth([]SupplyChain{ch}, report)

	if len(report.TierGaps) == 0 {
		t.Fatal("expected tier gap for manufacturing chain with only 2 product tiers")
	}
	if report.TierGaps[0].ChainIndex != 0 {
		t.Errorf("expected chainIndex=0, got %d", report.TierGaps[0].ChainIndex)
	}
}

func TestCheckTierDepth_CommoditySufficient(t *testing.T) {
	ch := shortCommodityChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkTierDepth([]SupplyChain{ch}, report)

	// Commodity chain with 1 product is flagged (< 2 product tiers)
	if len(report.TierGaps) == 0 {
		t.Fatal("expected tier gap for commodity chain with only 1 product tier")
	}
}

func TestCheckTierDepth_LithiumChainOK(t *testing.T) {
	ch := lithiumBatteryChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkTierDepth([]SupplyChain{ch}, report)

	if len(report.TierGaps) != 0 {
		t.Errorf("expected no tier gaps for lithium chain with 3 product tiers, got %d", len(report.TierGaps))
	}
}

func TestCheckUpstreamDownstream_AnchorInMiddle(t *testing.T) {
	ch := lithiumBatteryChain()
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/cn/ganfeng-lithium",
	}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkUpstreamDownstream([]SupplyChain{ch}, frag, report)

	if len(report.DepthGaps) != 0 {
		t.Errorf("anchor in middle should have both upstream and downstream, got %d depth gaps", len(report.DepthGaps))
	}
}

func TestCheckUpstreamDownstream_AnchorAtEnd(t *testing.T) {
	ch := lithiumBatteryChain()
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/us/tesla",
	}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkUpstreamDownstream([]SupplyChain{ch}, frag, report)

	if len(report.DepthGaps) != 1 {
		t.Fatalf("expected 1 depth gap for anchor at chain end, got %d", len(report.DepthGaps))
	}
	if report.DepthGaps[0].Direction != "downstream" {
		t.Errorf("expected direction=downstream, got %s", report.DepthGaps[0].Direction)
	}
}

func TestCheckConcentration_SingleSource(t *testing.T) {
	ch := singleSourceChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkConcentration([]SupplyChain{ch}, report)

	if len(report.ConcentrationRisks) != 1 {
		t.Fatalf("expected 1 concentration risk, got %d", len(report.ConcentrationRisks))
	}
	if report.ConcentrationRisks[0].ProductPath != "/product/advanced-chips" {
		t.Errorf("unexpected product path: %s", report.ConcentrationRisks[0].ProductPath)
	}
}

func TestCheckConcentration_WithAlternatives(t *testing.T) {
	ch := lithiumBatteryChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkConcentration([]SupplyChain{ch}, report)

	// Lithium carbonate (index 3) has alternatives mapped; spodumene (index 1) and battery cell (index 5) don't.
	for _, cr := range report.ConcentrationRisks {
		if cr.ProductPath == "/product/lithium-carbonate" {
			t.Error("lithium-carbonate has alternatives, should not be flagged")
		}
	}
}

func TestCheckMetricCoverage(t *testing.T) {
	ch := lithiumBatteryChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkMetricCoverage([]SupplyChain{ch}, report)

	// Chain has 6 segment pairs (0-1, 1-2, 2-3, 3-4, 4-5, 5-6), metrics cover 0-1 and 2-3.
	if len(report.MetricGaps) < 3 {
		t.Errorf("expected at least 3 metric gaps for lithium chain (4 uncovered pairs), got %d", len(report.MetricGaps))
	}
}

func TestCheckLogistics_MultiRegion(t *testing.T) {
	ch := multiRegionNoLogistics()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkLogistics([]SupplyChain{ch}, report)

	if len(report.LogisticsGaps) != 1 {
		t.Fatalf("expected 1 logistics gap for multi-region chain without transport, got %d", len(report.LogisticsGaps))
	}
}

func TestCheckLogistics_WithTransport(t *testing.T) {
	ch := multiRegionNoLogistics()
	ch.TransportVia = []string{"/fac/au/port-hedland", "/fac/cn/qingdao-port"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkLogistics([]SupplyChain{ch}, report)

	if len(report.LogisticsGaps) != 0 {
		t.Errorf("chain with transport should have 0 logistics gaps, got %d", len(report.LogisticsGaps))
	}
}

func TestCheckTemporalCoverage(t *testing.T) {
	ch := lithiumBatteryChain()
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkTemporalCoverage([]SupplyChain{ch}, report)

	// First metric has period "2023", second lacks period/start/end.
	if len(report.TemporalGaps) != 1 {
		t.Fatalf("expected 1 temporal gap (second metric missing period), got %d", len(report.TemporalGaps))
	}
}

// ---------------------------------------------------------------------------
// Full Analyze integration test (deterministic only, no LLM)
// ---------------------------------------------------------------------------

func TestAnalyze_DeterministicOnly(t *testing.T) {
	ch := lithiumBatteryChain()
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/cn/ganfeng-lithium",
		Type:          models.FragmentTypeOrganization,
	}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), []SupplyChain{ch}, frag)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Score <= 0 || report.Score > 1.0 {
		t.Errorf("score out of range: %f", report.Score)
	}
	if len(report.RefineDirectives) == 0 {
		t.Error("expected at least some refine directives for lithium chain")
	}
	for _, d := range report.RefineDirectives {
		if d.Query == "" {
			t.Error("directive has empty query")
		}
		if d.Priority == "" {
			t.Error("directive has empty priority")
		}
	}
}

// ---------------------------------------------------------------------------
// Patch application tests
// ---------------------------------------------------------------------------

func TestApplyPatches_Splice(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/raw", Name: "Raw", Type: "PRODUCT"},
			{CanonicalPath: "/org/c", Name: "C", Type: "ORGANIZATION"},
		},
		SegmentMetrics: []SegmentMetrics{
			{FromNodeIndex: 0, ToNodeIndex: 1, Confidence: 0.5},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "splice",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/product/raw", Name: "Raw", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B (Refiner)", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/refined", Name: "Refined", Type: "PRODUCT"},
			{CanonicalPath: "/org/c", Name: "C", Type: "ORGANIZATION"},
		},
		SegmentMetrics: []SegmentMetrics{
			{FromNodeIndex: 0, ToNodeIndex: 1, Confidence: 0.6},
			{FromNodeIndex: 1, ToNodeIndex: 2, Confidence: 0.6},
		},
		Confidence: 0.7,
	}

	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(result))
	}
	if len(result[0].Nodes) != 5 {
		t.Fatalf("expected 5 nodes after splice, got %d: %+v", len(result[0].Nodes), nodeNames(result[0].Nodes))
	}
	if result[0].Nodes[0].Name != "A" || result[0].Nodes[4].Name != "C" {
		t.Error("original boundary nodes should be preserved")
	}
	if result[0].Nodes[2].Name != "B (Refiner)" {
		t.Errorf("inner node should be inserted, got %s at position 2", result[0].Nodes[2].Name)
	}
}

func TestApplyPatches_ExtendDownstream(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "extend_downstream",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/q", Name: "Q", Type: "PRODUCT"},
		},
		Confidence: 0.6,
	}
	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(result[0].Nodes) != 4 {
		t.Fatalf("expected 4 nodes after extend, got %d", len(result[0].Nodes))
	}
	if result[0].Nodes[3].Name != "Q" {
		t.Errorf("last node should be Q, got %s", result[0].Nodes[3].Name)
	}
}

func TestApplyPatches_ExtendUpstream(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "extend_upstream",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A (Mine)", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/ore", Name: "Ore", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
		},
		Confidence: 0.6,
	}
	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(result[0].Nodes) != 4 {
		t.Fatalf("expected 4 nodes after upstream extend, got %d", len(result[0].Nodes))
	}
	if result[0].Nodes[0].Name != "A (Mine)" {
		t.Errorf("first node should be A (Mine), got %s", result[0].Nodes[0].Name)
	}
}

func TestApplyPatches_Alternatives(t *testing.T) {
	ch := singleSourceChain()
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "alternatives",
		Alternatives: []AlternativeSuppliers{
			{CustomerNodeIndex: 2, ProductNodeIndex: 1, Suppliers: []AlternativeContract{
				{SupplierRef: "/org/kr/samsung-foundry", TechnicalFit: 0.6, Confidence: 0.5},
			}},
		},
		Confidence: 0.7,
	}
	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(result[0].Alternatives) != 1 {
		t.Fatalf("expected 1 alternative group, got %d", len(result[0].Alternatives))
	}
	if len(result[0].Alternatives[0].Suppliers) != 1 {
		t.Fatalf("expected 1 supplier, got %d", len(result[0].Alternatives[0].Suppliers))
	}
}

func TestApplyPatches_SplicePreservesIntermediateNodes(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/raw", Name: "Raw", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B (existing)", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/final", Name: "Final", Type: "PRODUCT"},
			{CanonicalPath: "/org/c", Name: "C", Type: "ORGANIZATION"},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "splice",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/product/raw", Name: "Raw", Type: "PRODUCT"},
			{CanonicalPath: "/org/x", Name: "X (refiner)", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/intermediate", Name: "Intermediate", Type: "PRODUCT"},
			{CanonicalPath: "/product/final", Name: "Final", Type: "PRODUCT"},
		},
		Confidence: 0.7,
	}
	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	names := nodeNames(result[0].Nodes)
	// Existing node B must still be present between the new nodes and the downstream anchor.
	found := false
	for _, n := range names {
		if n == "B (existing)" {
			found = true
		}
	}
	if !found {
		t.Errorf("intermediate node 'B (existing)' was dropped, nodes: %v", names)
	}
	if len(result[0].Nodes) != 7 {
		t.Errorf("expected 7 nodes (5 original + 2 new inner), got %d: %v", len(result[0].Nodes), names)
	}
}

func TestApplyPatches_AlternativesMergePreservesOriginal(t *testing.T) {
	original := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
		},
		Alternatives: []AlternativeSuppliers{
			{CustomerNodeIndex: 2, ProductNodeIndex: 1, Suppliers: []AlternativeContract{
				{SupplierRef: "/org/existing", TechnicalFit: 0.8, Confidence: 0.7},
			}},
		},
	}
	origSupplierCount := len(original.Alternatives[0].Suppliers)

	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "alternatives",
		Alternatives: []AlternativeSuppliers{
			{CustomerNodeIndex: 2, ProductNodeIndex: 1, Suppliers: []AlternativeContract{
				{SupplierRef: "/org/new-alt", TechnicalFit: 0.6, Confidence: 0.5},
			}},
		},
	}
	result, _ := ApplyPatches([]SupplyChain{original}, []SupplyChainPatch{patch})
	if len(result[0].Alternatives[0].Suppliers) != 2 {
		t.Errorf("expected 2 suppliers after merge, got %d", len(result[0].Alternatives[0].Suppliers))
	}
	if len(original.Alternatives[0].Suppliers) != origSupplierCount {
		t.Errorf("original was mutated: had %d suppliers, now %d", origSupplierCount, len(original.Alternatives[0].Suppliers))
	}
}

func TestApplyPatches_BadAnchor(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "splice",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/nonexistent-up", Name: "X", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/new", Name: "New", Type: "PRODUCT"},
			{CanonicalPath: "/org/nonexistent-down", Name: "Y", Type: "ORGANIZATION"},
		},
		Confidence: 0.5,
	}
	_, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) == 0 {
		t.Error("expected warning for unmatched anchor nodes")
	}
}

func TestApplyPatches_PreservesOriginalChain(t *testing.T) {
	original := lithiumBatteryChain()
	originalNodeCount := len(original.Nodes)
	originalMetricCount := len(original.SegmentMetrics)

	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "extend_downstream",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/us/tesla", Name: "Tesla", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/model-y", Name: "Model Y", Type: "PRODUCT"},
			{CanonicalPath: "/org/us/consumer", Name: "Consumer", Type: "ORGANIZATION"},
		},
		Confidence: 0.5,
	}
	_, _ = ApplyPatches([]SupplyChain{original}, []SupplyChainPatch{patch})

	// Verify original is not mutated
	if len(original.Nodes) != originalNodeCount {
		t.Errorf("original chain was mutated: node count %d -> %d", originalNodeCount, len(original.Nodes))
	}
	if len(original.SegmentMetrics) != originalMetricCount {
		t.Errorf("original metrics were mutated: %d -> %d", originalMetricCount, len(original.SegmentMetrics))
	}
}

// ---------------------------------------------------------------------------
// Scoring test
// ---------------------------------------------------------------------------

func TestSufficiencyScore_PerfectChain(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "Miner", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/ore", Name: "Ore", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "Refiner", Type: "ORGANIZATION", Role: "processor"},
			{CanonicalPath: "/product/refined", Name: "Refined", Type: "PRODUCT"},
			{CanonicalPath: "/org/c", Name: "Manufacturer", Type: "ORGANIZATION", Role: "manufacturer"},
			{CanonicalPath: "/product/final", Name: "Final", Type: "PRODUCT"},
			{CanonicalPath: "/org/d", Name: "Customer", Type: "ORGANIZATION"},
		},
		Confidence: 0.9,
		SegmentMetrics: []SegmentMetrics{
			{FromNodeIndex: 0, ToNodeIndex: 1, Volume: &MetricWithConfidence{Value: 1000, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
			{FromNodeIndex: 1, ToNodeIndex: 2, Volume: &MetricWithConfidence{Value: 1000, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
			{FromNodeIndex: 2, ToNodeIndex: 3, Volume: &MetricWithConfidence{Value: 500, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
			{FromNodeIndex: 3, ToNodeIndex: 4, Volume: &MetricWithConfidence{Value: 500, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
			{FromNodeIndex: 4, ToNodeIndex: 5, Volume: &MetricWithConfidence{Value: 200, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
			{FromNodeIndex: 5, ToNodeIndex: 6, Volume: &MetricWithConfidence{Value: 200, Unit: "metric_tons_per_year"}, Period: "2023", Confidence: 0.8},
		},
		Alternatives: []AlternativeSuppliers{
			{CustomerNodeIndex: 2, ProductNodeIndex: 1, Suppliers: []AlternativeContract{{SupplierRef: "/org/x", Confidence: 0.5}}},
			{CustomerNodeIndex: 4, ProductNodeIndex: 3, Suppliers: []AlternativeContract{{SupplierRef: "/org/y", Confidence: 0.5}}},
			{CustomerNodeIndex: 6, ProductNodeIndex: 5, Suppliers: []AlternativeContract{{SupplierRef: "/org/z", Confidence: 0.5}}},
		},
	}
	frag := &models.HorizonFragment{CanonicalPath: "/org/b"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), []SupplyChain{ch}, frag)
	if err != nil {
		t.Fatal(err)
	}
	if report.Score < 0.8 {
		t.Errorf("well-covered chain should have high score, got %f", report.Score)
	}
	if !report.Sufficient {
		t.Error("well-covered chain should be sufficient")
	}
}

func TestSufficiencyScore_TinyChainCapped(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
		},
		Confidence: 0.8,
	}
	frag := &models.HorizonFragment{CanonicalPath: "/org/a"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), []SupplyChain{ch}, frag)
	if err != nil {
		t.Fatal(err)
	}
	// 2 nodes / 6 (min) = 0.33 ceiling, so score must be <= 0.34
	if report.Score > 0.34 {
		t.Errorf("tiny chain (2 nodes) should have capped score, got %f", report.Score)
	}
	if report.Sufficient {
		t.Error("tiny chain should NOT be sufficient")
	}
}

func TestApplyPatches_ExtendUpstreamBranch(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/a", Name: "A", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/p", Name: "P", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/q", Name: "Q", Type: "PRODUCT"},
		},
	}
	patch := SupplyChainPatch{
		DirectiveIndex: 0,
		PatchType:      "extend_upstream",
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/x", Name: "X (mine)", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/ore", Name: "Ore", Type: "PRODUCT"},
			{CanonicalPath: "/org/b", Name: "B", Type: "ORGANIZATION"},
		},
		Confidence: 0.7,
	}
	result, warnings := ApplyPatches([]SupplyChain{ch}, []SupplyChainPatch{patch})
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	// Anchor /org/b is at position 2 (not 0), so a new branch chain should be created.
	if len(result) != 2 {
		t.Fatalf("expected 2 chains (original + branch), got %d", len(result))
	}
	if len(result[0].Nodes) != 4 {
		t.Errorf("original chain should be unchanged (4 nodes), got %d", len(result[0].Nodes))
	}
	if len(result[1].Nodes) != 3 {
		t.Errorf("branch chain should have 3 nodes, got %d", len(result[1].Nodes))
	}
	if result[1].Nodes[0].Name != "X (mine)" {
		t.Errorf("branch should start with X (mine), got %s", result[1].Nodes[0].Name)
	}
}

func TestDirectiveQueriesUseNames(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/de/bosch", Name: "Robert Bosch", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/raw/steel", Name: "Steel", Type: "PRODUCT"},
			{CanonicalPath: "/org/de/vw", Name: "Volkswagen", Type: "ORGANIZATION", Role: "manufacturer"},
			{CanonicalPath: "/product/final/car", Name: "Passenger Car", Type: "PRODUCT"},
		},
	}
	frag := &models.HorizonFragment{CanonicalPath: "/org/de/bosch"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), []SupplyChain{ch}, frag)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.RefineDirectives {
		if d.GapType == "industry_gap" {
			continue
		}
		if strings.Contains(d.Query, "/org/") || strings.Contains(d.Query, "/product/") {
			if !strings.Contains(d.Query, "(") {
				t.Errorf("directive query should use human-readable names, not raw paths: %q", d.Query)
			}
		}
	}
}

func TestSufficiencyScore_EmptyChains(t *testing.T) {
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Score != 1.0 || !report.Sufficient {
		t.Error("empty chains should be scored as sufficient (nothing to check)")
	}
}

func TestCheckUpstreamDownstream_BothDirections(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/org/de/bosch", Name: "Robert Bosch", Type: "ORGANIZATION"},
		},
	}
	frag := &models.HorizonFragment{CanonicalPath: "/org/de/bosch"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkUpstreamDownstream([]SupplyChain{ch}, frag, report)

	if len(report.DepthGaps) != 2 {
		t.Fatalf("expected 2 depth gaps (both upstream and downstream) for single-node chain, got %d", len(report.DepthGaps))
	}
	dirs := map[string]bool{}
	for _, g := range report.DepthGaps {
		dirs[g.Direction] = true
	}
	if !dirs["upstream"] || !dirs["downstream"] {
		t.Errorf("expected both upstream and downstream depth gaps, got directions: %v", dirs)
	}
}

// TestCheckUpstreamDownstream_OrgFallbackToFac verifies fractal model: when frag is /org/
// and chain has /fac/{slug}-general-production, we match and use facility path for directives.
func TestCheckUpstreamDownstream_OrgFallbackToFac(t *testing.T) {
	ch := SupplyChain{
		Nodes: []SupplyChainNode{
			{CanonicalPath: "/fac/smic-shanghai", Name: "SMIC Shanghai", Type: "FACILITY"},
			{CanonicalPath: "/product/kirin-9000s", Name: "Kirin 9000s", Type: "PRODUCT"},
			{CanonicalPath: "/fac/huawei-general-production", Name: "Huawei", Type: "FACILITY"},
			{CanonicalPath: "/product/smartphone", Name: "Smartphone", Type: "PRODUCT"},
			{CanonicalPath: "/fac/retailer", Name: "Retailer", Type: "FACILITY"},
		},
	}
	frag := &models.HorizonFragment{CanonicalPath: "/org/cn/huawei"}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report := &SupplyChainGapReport{}
	analyzer.checkUpstreamDownstream([]SupplyChain{ch}, frag, report)

	// Huawei (as /fac/huawei-general-production) is in middle: has both upstream and downstream
	if len(report.DepthGaps) != 0 {
		t.Errorf("org target /org/cn/huawei should match /fac/huawei-general-production; expected 0 depth gaps, got %d", len(report.DepthGaps))
	}
}

func TestDirectives_HaveSuggestedPatchType(t *testing.T) {
	ch := lithiumBatteryChain()
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/cn/ganfeng-lithium",
		Type:          models.FragmentTypeOrganization,
	}
	analyzer := NewSupplyChainGapAnalyzer(nil)
	report, err := analyzer.Analyze(context.Background(), []SupplyChain{ch}, frag)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.RefineDirectives {
		switch d.GapType {
		case "missing_tier":
			if d.SuggestedPatchType != "splice" {
				t.Errorf("missing_tier directive should suggest splice, got %q", d.SuggestedPatchType)
			}
		case "depth_gap":
			if d.SuggestedPatchType != "extend_upstream" && d.SuggestedPatchType != "extend_downstream" {
				t.Errorf("depth_gap directive should suggest extend_*, got %q", d.SuggestedPatchType)
			}
		case "concentration_risk":
			// no longer added (org→fac→product→fac→org model); no assertion
		case "logistics_gap":
			if d.SuggestedPatchType != "splice" {
				t.Errorf("logistics_gap directive should suggest splice, got %q", d.SuggestedPatchType)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nodeNames(nodes []SupplyChainNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}
