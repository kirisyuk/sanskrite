package scout

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestSupplyChainParser_AbstainsOnUnknownPair(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.8,
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/market/sa/finance/banking", Type: "MARKET", Name: "Saudi Banking"},
				{CanonicalPath: "/loc/sa/na/na", Type: "GEO_REGION", Name: "Saudi Arabia"},
			},
		},
	}
	result, err := parser.ParseSupplyChains(chains, "/market/sa/finance/banking")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("unknown pair (MARKET->GEO_REGION) should abstain, got %d edges", len(result.Edges))
	}
	if result.Stats.EdgesAbstainedUnknownPair != 1 {
		t.Fatalf("EdgesAbstainedUnknownPair=%d, want 1", result.Stats.EdgesAbstainedUnknownPair)
	}
	if len(result.ParseWarnings) == 0 {
		t.Fatalf("expected ParseWarning for abstained unknown pair")
	}
	found := false
	for _, w := range result.ParseWarnings {
		if strings.Contains(w, "abstain:") && strings.Contains(w, "unknown pair") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected abstain warning in ParseWarnings: %v", result.ParseWarnings)
	}
}

func TestParseSupplyChains_SupplyLinkSegmentBasic(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence:   0.92,
			TransportVia: []string{"/fac/ae/jebel-ali-port", "/loc/ae/na/na"},
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/cn/huawei-technologies-co-ltd",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/product/telecom/mobile-phones",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/org/kz/kcell-jsc",
					Type:          "ORGANIZATION",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/cn/huawei-technologies-co-ltd")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}
	if len(result.SupplyLinkSegments) != 1 {
		t.Fatalf("segments=%d, want 1", len(result.SupplyLinkSegments))
	}
	seg := result.SupplyLinkSegments[0]
	// Full chain org→fac→product→fac→org: keep org as-is, do not convert
	if seg.SupplierRef != ncp("/org/cn/huawei-technologies-co-ltd") {
		t.Fatalf("supplierRef=%q, want %q", seg.SupplierRef, ncp("/org/cn/huawei-technologies-co-ltd"))
	}
	if seg.CustomerRef != ncp("/org/kz/kcell-jsc") {
		t.Fatalf("customerRef=%q, want %q", seg.CustomerRef, ncp("/org/kz/kcell-jsc"))
	}
	if len(seg.TransportViaRefs) != 1 || seg.TransportViaRefs[0] != "/fac/ae/jebel-ali-port" {
		t.Fatalf("transportViaRefs=%v, want only /fac/ae/jebel-ali-port", seg.TransportViaRefs)
	}
}

func TestParseSupplyChains_SuppliesToEdgeHasTransportVia(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence:   0.87,
			TransportVia: []string{"/fac/us/port-long-beach"},
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/us/intel-corporation",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/product/semiconductors/fabs/chips",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/org/tw/tsmc",
					Type:          "ORGANIZATION",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/us/intel-corporation")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var suppliesEdge *ChainEdge
	for _, edge := range result.Edges {
		if edge == nil || edge.RelationType != string(models.DependencyTypeSuppliesTo) {
			continue
		}
		// Full chain: keep org as endpoint
		if edge.SourceCanonicalPath == ncp("/org/us/intel-corporation") && edge.TargetCanonicalPath == ncp("/org/tw/tsmc") {
			suppliesEdge = edge
			break
		}
	}
	if suppliesEdge == nil {
		t.Fatalf("expected inferred SUPPLIES_TO edge (org→org kept as-is)")
	}
	rawVia, ok := suppliesEdge.Qualifiers[models.QualifierTransportVia]
	if !ok {
		t.Fatalf("expected transport_via qualifier")
	}
	via, ok := rawVia.([]string)
	if !ok {
		t.Fatalf("transport_via type=%T, want []string", rawVia)
	}
	if len(via) != 1 || via[0] != "/fac/us/port-long-beach" {
		t.Fatalf("transport_via=%v, want [/fac/us/port-long-beach]", via)
	}
	if got, _ := suppliesEdge.Qualifiers["source_role"].(string); got != "supplier" {
		t.Fatalf("source_role=%q, want supplier", got)
	}
	if got, _ := suppliesEdge.Qualifiers["target_role"].(string); got != "customer" {
		t.Fatalf("target_role=%q, want customer", got)
	}
	if got, _ := suppliesEdge.Qualifiers["direction_basis"].(string); got != "supply_flow" {
		t.Fatalf("direction_basis=%q, want supply_flow", got)
	}
	if got, _ := suppliesEdge.Qualifiers["product_ref"].(string); got != "/product/semiconductors/fabs/chips" {
		t.Fatalf("product_ref=%q, want /product/semiconductors/fabs/chips", got)
	}
}

func TestParseSupplyChains_SuppliesToEdgeWithoutTransportStillHasDirectionalQualifiers(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.9,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/br/vale-sa",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/product/raw/minerals/iron-ore",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/fac/cn/baosteel-shanghai",
					Type:          "FACILITY",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/br/vale-sa")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var suppliesEdge *ChainEdge
	for _, edge := range result.Edges {
		if edge == nil || edge.RelationType != string(models.DependencyTypeSuppliesTo) {
			continue
		}
		// Full chain: keep org as supplier
		if edge.SourceCanonicalPath == ncp("/org/br/vale-sa") && edge.TargetCanonicalPath == ncp("/fac/cn/baosteel-shanghai") {
			suppliesEdge = edge
			break
		}
	}
	if suppliesEdge == nil {
		t.Fatalf("expected SUPPLIES_TO edge for actor->product->actor pattern")
	}
	if _, ok := suppliesEdge.Qualifiers[models.QualifierTransportVia]; ok {
		t.Fatalf("transport_via should be omitted when route is unknown")
	}
	if got, _ := suppliesEdge.Qualifiers["source_role"].(string); got != "supplier" {
		t.Fatalf("source_role=%q, want supplier", got)
	}
	if got, _ := suppliesEdge.Qualifiers["target_role"].(string); got != "customer" {
		t.Fatalf("target_role=%q, want customer", got)
	}
	if got, _ := suppliesEdge.Qualifiers["direction_basis"].(string); got != "supply_flow" {
		t.Fatalf("direction_basis=%q, want supply_flow", got)
	}
	if got, _ := suppliesEdge.Qualifiers["product_ref"].(string); got != "/product/raw/minerals/iron-ore" {
		t.Fatalf("product_ref=%q, want /product/raw/minerals/iron-ore", got)
	}
}

func TestParseSupplyChains_OrgToFacilityUsesOwns(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.84,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/us/intel-corporation",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/fac/us/chandler-fab-42",
					Type:          "FACILITY",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/us/intel-corporation")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	foundOwns := false
	for _, edge := range result.Edges {
		if edge == nil {
			continue
		}
		if edge.RelationType == string(models.DependencyTypeOwns) &&
			edge.SourceCanonicalPath == "/org/us/intel-corporation" &&
			edge.TargetCanonicalPath == "/fac/us/chandler-fab-42" {
			foundOwns = true
		}
	}
	if !foundOwns {
		t.Fatalf("expected OWNS edge for org->facility anchor")
	}
}

func TestParseSupplyChains_GovToFacilityUsesControls(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.84,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/gov/us/department-of-energy",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/fac/us/strategic-reserve-terminal",
					Type:          "FACILITY",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/gov/us/department-of-energy")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	foundControls := false
	for _, edge := range result.Edges {
		if edge == nil {
			continue
		}
		if edge.RelationType == string(models.DependencyTypeControls) &&
			edge.SourceCanonicalPath == "/gov/us/department-of-energy" &&
			edge.TargetCanonicalPath == "/fac/us/strategic-reserve-terminal" {
			foundControls = true
		}
	}
	if !foundControls {
		t.Fatalf("expected CONTROLS edge for gov->facility anchor")
	}
}

func TestParseSupplyChains_NormalizesLegacyCanonicalPaths(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.9,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/tesla-inc",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/product/technology/automotive/manufacturing/electric-vehicle",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/org/us/panasonic",
					Type:          "ORGANIZATION",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/tesla-inc")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}
	if _, ok := result.Fragments["/org/na/tesla"]; !ok {
		t.Fatalf("expected normalized fragment key /org/na/tesla, got keys=%v", keys(result.Fragments))
	}
	if len(result.SupplyLinkSegments) != 1 {
		t.Fatalf("expected one supply-link segment, got %d", len(result.SupplyLinkSegments))
	}
	if got := result.SupplyLinkSegments[0].SupplierRef; got != "/org/na/tesla" {
		t.Fatalf("supplierRef=%q, want /org/na/tesla", got)
	}
}

func TestParseSupplyChains_OrgToFacNoConsecutiveTypeWarning(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.8,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/us/intel",
					Type:          "ACTOR",
				},
				{
					CanonicalPath: "/fac/us/chandler-fab-42",
					Type:          "ACTOR",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/us/intel")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}
	if result.Stats.ConsecutiveSameTypeNodes != 0 {
		t.Fatalf("ConsecutiveSameTypeNodes=%d, want 0", result.Stats.ConsecutiveSameTypeNodes)
	}
}

func TestParseSupplyChains_ExtractivePatternUsesExtractsFromAndAvoidsDepositSupplies(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.88,
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/br/vale-sa",
					Type:          "ORGANIZATION",
				},
				{
					CanonicalPath: "/fac/br/carajas-mine",
					Type:          "FACILITY",
				},
				{
					CanonicalPath: "/deposit/iron-ore/carajas-complex",
					Type:          "DEPOSIT",
				},
				{
					CanonicalPath: "/product/raw/minerals/iron-ore",
					Type:          "PRODUCT",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/br/vale-sa")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	hasExtractsFrom := false
	for _, edge := range result.Edges {
		if edge == nil {
			continue
		}
		if edge.RelationType == string(models.DependencyTypeExtractsFrom) &&
			edge.SourceCanonicalPath == "/fac/br/carajas-mine" &&
			edge.TargetCanonicalPath == "/deposit/iron-ore/carajas-complex" {
			hasExtractsFrom = true
		}
		if edge.RelationType == string(models.DependencyTypeSuppliesTo) &&
			(strings.Contains(edge.SourceCanonicalPath, "/deposit/") || strings.Contains(edge.TargetCanonicalPath, "/deposit/")) {
			t.Fatalf("unexpected deposit-involved SUPPLIES_TO edge: %s -> %s", edge.SourceCanonicalPath, edge.TargetCanonicalPath)
		}
	}
	if !hasExtractsFrom {
		t.Fatalf("expected EXTRACTS_FROM edge for FACILITY -> DEPOSIT")
	}
	if len(result.SupplyLinkSegments) != 0 {
		t.Fatalf("extractive pattern should not produce actor->product->actor supply-link segments, got %d", len(result.SupplyLinkSegments))
	}
}

func TestParseSupplyChains_ProgramAndMarketNodesCreateFragments(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.81,
			Reason:     "Aktau port route to TITR and EU logistics market",
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/fac/kz/aktau-sea-port",
					Name:          "Aktau Sea Port",
					Type:          "FACILITY",
				},
				{
					CanonicalPath: "/program/int/titr-middle-corridor",
					Name:          "TITR Middle Corridor",
					Type:          "PROGRAM",
				},
				{
					CanonicalPath: "/market/eu/all/logistics",
					Name:          "EU Logistics Market",
					Type:          "MARKET",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/market/kz/investment/all/logistics")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	programFrag, ok := result.Fragments["/program/int/titr-middle-corridor"]
	if !ok || programFrag == nil {
		t.Fatalf("expected PROGRAM fragment to be materialized")
	}
	if programFrag.Type != models.FragmentTypeProgram {
		t.Fatalf("program fragment type=%s, want %s", programFrag.Type, models.FragmentTypeProgram)
	}

	marketFrag, ok := result.Fragments["/market/eu/all/logistics"]
	if !ok || marketFrag == nil {
		t.Fatalf("expected MARKET fragment to be materialized")
	}
	if marketFrag.Type != models.FragmentTypeMarket {
		t.Fatalf("market fragment type=%s, want %s", marketFrag.Type, models.FragmentTypeMarket)
	}

	if result.Stats.NodesSkippedUnknownType != 0 {
		t.Fatalf("nodesSkippedUnknownType=%d, want 0", result.Stats.NodesSkippedUnknownType)
	}
	// PROGRAM->MARKET is an unknown pair: parser abstains (no generic DEPENDS_ON) per GAP-03.
	if len(result.Edges) != 0 {
		t.Fatalf("PROGRAM->MARKET unknown pair should abstain, got %d edges", len(result.Edges))
	}
	if result.Stats.EdgesAbstainedUnknownPair < 1 {
		t.Fatalf("EdgesAbstainedUnknownPair=%d, want >= 1", result.Stats.EdgesAbstainedUnknownPair)
	}
}

func TestParseSupplyChains_TransformsIntoDirectionSwap_ProductToProduct(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.90,
			Reason:     "Copper value chain",
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/product/component/mineral/smelting/copper-anode",
					Name:          "Copper Anode",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/product/raw/mineral/extraction/copper-ore",
					Name:          "Copper Ore",
					Type:          "PRODUCT",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/product/component/mineral/smelting/copper-anode")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var transformEdge *ChainEdge
	for _, e := range result.Edges {
		if e.RelationType == string(models.DependencyTypeTransformsInto) {
			transformEdge = e
			break
		}
	}
	if transformEdge == nil {
		t.Fatalf("expected TRANSFORMS_INTO edge")
	}
	if transformEdge.SourceCanonicalPath != "/product/raw/mineral/extraction/copper-ore" {
		t.Fatalf("TRANSFORMS_INTO source=%q, want copper-ore (upstream)", transformEdge.SourceCanonicalPath)
	}
	if transformEdge.TargetCanonicalPath != "/product/component/mineral/smelting/copper-anode" {
		t.Fatalf("TRANSFORMS_INTO target=%q, want copper-anode (downstream)", transformEdge.TargetCanonicalPath)
	}
}

func TestParseSupplyChains_TransformsIntoDirectionCorrect_NoSwap(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.90,
			Reason:     "Iron value chain",
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/product/raw/mineral/extraction/iron-ore",
					Name:          "Iron Ore",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/product/finished/mineral/processing/iron-pellet",
					Name:          "Iron Pellet",
					Type:          "PRODUCT",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/product/raw/mineral/extraction/iron-ore")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var transformEdge *ChainEdge
	for _, e := range result.Edges {
		if e.RelationType == string(models.DependencyTypeTransformsInto) {
			transformEdge = e
			break
		}
	}
	if transformEdge == nil {
		t.Fatalf("expected TRANSFORMS_INTO edge")
	}
	if transformEdge.SourceCanonicalPath != "/product/raw/mineral/extraction/iron-ore" {
		t.Fatalf("TRANSFORMS_INTO source=%q, want iron-ore (upstream)", transformEdge.SourceCanonicalPath)
	}
	if transformEdge.TargetCanonicalPath != "/product/finished/mineral/processing/iron-pellet" {
		t.Fatalf("TRANSFORMS_INTO target=%q, want iron-pellet (downstream)", transformEdge.TargetCanonicalPath)
	}
}

func TestParseSupplyChains_TransformsIntoViaFacility_DirectionSwap(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence: 0.85,
			Reason:     "Copper smelting chain",
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/product/component/mineral/smelting/copper-anode",
					Name:          "Copper Anode",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/fac/cl/chuquicamata-smelter",
					Name:          "Chuquicamata Smelter",
					Type:          "FACILITY",
				},
				{
					CanonicalPath: "/product/raw/mineral/extraction/copper-ore",
					Name:          "Copper Ore",
					Type:          "PRODUCT",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/product/component/mineral/smelting/copper-anode")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var transformEdge *ChainEdge
	for _, e := range result.Edges {
		if e.RelationType == string(models.DependencyTypeTransformsInto) {
			transformEdge = e
			break
		}
	}
	if transformEdge == nil {
		t.Fatalf("expected TRANSFORMS_INTO edge (via facility pattern)")
	}
	if transformEdge.SourceCanonicalPath != "/product/raw/mineral/extraction/copper-ore" {
		t.Fatalf("TRANSFORMS_INTO source=%q, want copper-ore (upstream)", transformEdge.SourceCanonicalPath)
	}
	if transformEdge.TargetCanonicalPath != "/product/component/mineral/smelting/copper-anode" {
		t.Fatalf("TRANSFORMS_INTO target=%q, want copper-anode (downstream)", transformEdge.TargetCanonicalPath)
	}
}

func TestShouldSwapProductStageOrder(t *testing.T) {
	tests := []struct {
		name     string
		src, dst string
		want     bool
	}{
		{
			name: "downstream->upstream should swap",
			src:  "/product/finished/mineral/processing/iron-pellet",
			dst:  "/product/raw/mineral/extraction/iron-ore",
			want: true,
		},
		{
			name: "upstream->downstream no swap",
			src:  "/product/raw/mineral/extraction/iron-ore",
			dst:  "/product/finished/mineral/processing/iron-pellet",
			want: false,
		},
		{
			name: "smelting->extraction should swap",
			src:  "/product/component/mineral/smelting/copper-anode",
			dst:  "/product/raw/mineral/extraction/copper-ore",
			want: true,
		},
		{
			name: "flat paths no stage info - no swap",
			src:  "/product/copper-anode",
			dst:  "/product/copper-ore",
			want: false,
		},
		{
			name: "same stage - no swap",
			src:  "/product/raw/mineral/extraction/copper-ore",
			dst:  "/product/raw/mineral/extraction/iron-ore",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSwapProductStageOrder(tt.src, tt.dst)
			if got != tt.want {
				t.Fatalf("shouldSwapProductStageOrder(%q, %q) = %v, want %v", tt.src, tt.dst, got, tt.want)
			}
		})
	}
}

func keys(m map[string]*models.HorizonFragment) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func ncp(path string) string {
	return canonical.NormalizeCanonicalPathForPersist(path)
}
