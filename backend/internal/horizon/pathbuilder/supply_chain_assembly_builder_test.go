package pathbuilder

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildAssemblyPayloadFromContributions_MultiBranchPhoneGraph(t *testing.T) {
	anchors := []string{"/product/device/electronics/smartphone"}
	opts := DefaultSupplyChainAssemblyOptions()
	payload, labels, nodeRefs, coverage, openGaps := buildAssemblyPayloadFromContributions(anchors, []assemblyContribution{
		{
			canonicalKey: "/supply-chain-path/global/battery",
			parsed: &models.SupplyChainPathPayload{
				Nodes: []models.SupplyChainPathNode{
					{Type: "org", Ref: "/org/global/lithium-miner"},
					{Type: "product", Ref: "/product/material/lithium"},
					{Type: "org", Ref: "/org/global/battery-maker"},
					{Type: "product", Ref: "/product/component/battery-pack"},
					{Type: "org", Ref: "/org/global/phone-assembler"},
					{Type: "product", Ref: "/product/device/electronics/smartphone"},
				},
				Legs: []models.SupplyChainPathLeg{
					{SupplierRef: "/org/global/lithium-miner", ProductRef: "/product/material/lithium", CustomerRef: "/org/global/battery-maker", SupplyLinkCanonicalPath: "/supply-link/global/lithium", SegmentConfidence: 0.91},
					{SupplierRef: "/org/global/battery-maker", ProductRef: "/product/component/battery-pack", CustomerRef: "/org/global/phone-assembler", SupplyLinkCanonicalPath: "/supply-link/global/battery", SegmentConfidence: 0.94, TransportViaRefs: []string{"/fac/cn/shenzhen/port-yantian"}},
					{SupplierRef: "/org/global/phone-assembler", ProductRef: "/product/device/electronics/smartphone", CustomerRef: "/org/global/brand", SupplyLinkCanonicalPath: "/supply-link/global/smartphone-battery", SegmentConfidence: 0.96},
				},
			},
		},
		{
			canonicalKey: "/supply-chain-path/global/chip",
			parsed: &models.SupplyChainPathPayload{
				Nodes: []models.SupplyChainPathNode{
					{Type: "org", Ref: "/org/global/silicon-refiner"},
					{Type: "product", Ref: "/product/material/silicon-wafer"},
					{Type: "org", Ref: "/org/global/chip-fab"},
					{Type: "product", Ref: "/product/component/application-processor"},
					{Type: "org", Ref: "/org/global/phone-assembler"},
					{Type: "product", Ref: "/product/device/electronics/smartphone"},
				},
				Legs: []models.SupplyChainPathLeg{
					{SupplierRef: "/org/global/silicon-refiner", ProductRef: "/product/material/silicon-wafer", CustomerRef: "/org/global/chip-fab", SupplyLinkCanonicalPath: "/supply-link/global/silicon"},
					{SupplierRef: "/org/global/chip-fab", ProductRef: "/product/component/application-processor", CustomerRef: "/org/global/phone-assembler", SupplyLinkCanonicalPath: "/supply-link/global/chip"},
					{SupplierRef: "/org/global/phone-assembler", ProductRef: "/product/device/electronics/smartphone", CustomerRef: "/org/global/brand", SupplyLinkCanonicalPath: "/supply-link/global/smartphone-chip"},
				},
				Sufficiency: &models.SupplyChainPathSufficiency{Status: models.SupplyChainSufficiencyHeuristic, OpenGaps: []string{"camera_branch_missing"}},
			},
		},
		{
			canonicalKey: "/supply-chain-path/global/display",
			parsed: &models.SupplyChainPathPayload{
				Nodes: []models.SupplyChainPathNode{
					{Type: "org", Ref: "/org/global/glass-supplier"},
					{Type: "product", Ref: "/product/material/display-glass"},
					{Type: "org", Ref: "/org/global/display-maker"},
					{Type: "product", Ref: "/product/component/display-module"},
					{Type: "org", Ref: "/org/global/phone-assembler"},
					{Type: "product", Ref: "/product/device/electronics/smartphone"},
				},
				Legs: []models.SupplyChainPathLeg{
					{SupplierRef: "/org/global/glass-supplier", ProductRef: "/product/material/display-glass", CustomerRef: "/org/global/display-maker", SupplyLinkCanonicalPath: "/supply-link/global/glass"},
					{SupplierRef: "/org/global/display-maker", ProductRef: "/product/component/display-module", CustomerRef: "/org/global/phone-assembler", SupplyLinkCanonicalPath: "/supply-link/global/display"},
					{SupplierRef: "/org/global/phone-assembler", ProductRef: "/product/device/electronics/smartphone", CustomerRef: "/org/global/brand", SupplyLinkCanonicalPath: "/supply-link/global/smartphone-display"},
				},
			},
		},
	}, opts)
	if payload == nil {
		t.Fatal("expected assembly payload")
	}
	if coverage == nil {
		t.Fatal("expected coverage")
	}
	if coverage.ProductsN != 7 {
		t.Fatalf("coverage.ProductsN=%d, want 7", coverage.ProductsN)
	}
	if coverage.TransformationsN != 6 {
		t.Fatalf("coverage.TransformationsN=%d, want 6", coverage.TransformationsN)
	}
	if coverage.Truncated {
		t.Fatal("coverage.Truncated must be false")
	}
	if labels["anchorProduct"] != anchors[0] {
		t.Fatalf("anchorProduct=%q, want %q", labels["anchorProduct"], anchors[0])
	}
	if len(nodeRefs) < 10 {
		t.Fatalf("expected rich nodeRefs set, got %d", len(nodeRefs))
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if len(parsed.AssemblyEdges) != 3 {
		t.Fatalf("len(parsed.AssemblyEdges)=%d, want 3", len(parsed.AssemblyEdges))
	}
	if len(parsed.PrimaryBranches) < 3 {
		t.Fatalf("len(parsed.PrimaryBranches)=%d, want >= 3", len(parsed.PrimaryBranches))
	}
	if parsed.Coverage == nil || parsed.Coverage.PrimaryBranchesN < 3 {
		t.Fatalf("coverage=%+v, want at least 3 primary branches", parsed.Coverage)
	}
	if len(parsed.RouteBindings) != 1 {
		t.Fatalf("len(parsed.RouteBindings)=%d, want 1", len(parsed.RouteBindings))
	}
	if len(parsed.TopProductFamilies) == 0 {
		t.Fatalf("TopProductFamilies=%v, want non-empty", parsed.TopProductFamilies)
	}
	if len(openGaps) != 1 || openGaps[0] != "camera_branch_missing" {
		t.Fatalf("openGaps=%v, want [camera_branch_missing]", openGaps)
	}
}

func TestBuildAssemblyPayloadFromContributions_TruncationAddsOpenGap(t *testing.T) {
	opts := DefaultSupplyChainAssemblyOptions()
	opts.MaxProductNodes = 2

	payload, _, _, coverage, openGaps := buildAssemblyPayloadFromContributions(
		[]string{"/product/device/electronics/smartphone"},
		[]assemblyContribution{
			{
				canonicalKey: "/supply-chain-path/global/truncated",
				parsed: &models.SupplyChainPathPayload{
					Legs: []models.SupplyChainPathLeg{
						{SupplierRef: "/org/global/a", ProductRef: "/product/material/lithium", CustomerRef: "/org/global/b", SupplyLinkCanonicalPath: "/supply-link/global/a"},
						{SupplierRef: "/org/global/b", ProductRef: "/product/component/battery-pack", CustomerRef: "/org/global/c", SupplyLinkCanonicalPath: "/supply-link/global/b"},
						{SupplierRef: "/org/global/c", ProductRef: "/product/device/electronics/smartphone", CustomerRef: "/org/global/d", SupplyLinkCanonicalPath: "/supply-link/global/c"},
					},
				},
			},
		},
		opts,
	)
	if payload == nil {
		t.Fatal("expected payload even when truncated")
	}
	if coverage == nil || !coverage.Truncated {
		t.Fatalf("coverage=%+v, want truncated", coverage)
	}
	found := false
	for _, gap := range openGaps {
		if gap == "graph_truncated:product_nodes" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("openGaps=%v must include graph_truncated:product_nodes", openGaps)
	}
}

func TestBuildAssemblyPayloadFromInputs_RawGraphSupportsMixedAnchorsWithoutSupplyLinks(t *testing.T) {
	opts := DefaultSupplyChainAssemblyOptions()
	opts.AnchorSet = []string{
		"/org/kz/meloman-marwin",
		"/product/device/electronics/smartphone",
		"/fac/kz/almaty/distribution-center",
	}
	opts.SourceAnchors = append([]string(nil), opts.AnchorSet...)
	opts.RawNodeRefs = []string{
		"/org/kz/meloman-marwin",
		"/fac/kz/almaty/distribution-center",
		"/product/device/electronics/smartphone",
		"/product/component/display-module",
	}
	opts.RawEdges = []SupplyChainAssemblyRawEdge{
		{Source: "/org/kz/meloman-marwin", Target: "/fac/kz/almaty/distribution-center", EdgeType: "OWNS"},
		{Source: "/fac/kz/almaty/distribution-center", Target: "/product/device/electronics/smartphone", EdgeType: "PRODUCES"},
		{Source: "/product/component/display-module", Target: "/product/device/electronics/smartphone", EdgeType: "TRANSFORMS_INTO"},
	}

	payload, labels, nodeRefs, coverage, openGaps := buildAssemblyPayloadFromInputs(opts.AnchorSet, nil, opts)
	if payload == nil {
		t.Fatal("expected payload")
	}
	if coverage == nil {
		t.Fatal("expected coverage")
	}
	if coverage.ProductsN != 2 {
		t.Fatalf("coverage.ProductsN=%d, want 2", coverage.ProductsN)
	}
	if coverage.ActorsN != 2 {
		t.Fatalf("coverage.ActorsN=%d, want 2", coverage.ActorsN)
	}
	if len(openGaps) != 0 {
		t.Fatalf("openGaps=%v, want none", openGaps)
	}
	if labels["anchorRef"] != "/fac/kz/almaty/distribution-center" && labels["anchorRef"] != "/org/kz/meloman-marwin" && labels["anchorRef"] != "/product/device/electronics/smartphone" {
		t.Fatalf("unexpected anchorRef label=%q", labels["anchorRef"])
	}
	if len(nodeRefs) != 4 {
		t.Fatalf("len(nodeRefs)=%d, want 4", len(nodeRefs))
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if parsed.Stage != "" {
		t.Fatalf("payload builder should not set stage directly, got %q", parsed.Stage)
	}
	if len(parsed.AnchorSet) != 3 {
		t.Fatalf("len(parsed.AnchorSet)=%d, want 3", len(parsed.AnchorSet))
	}
	if len(parsed.AnchorComponents) != 1 {
		t.Fatalf("len(parsed.AnchorComponents)=%d, want 1", len(parsed.AnchorComponents))
	}
	if len(parsed.TransformEdges) != 1 {
		t.Fatalf("len(parsed.TransformEdges)=%d, want 1", len(parsed.TransformEdges))
	}
	if len(parsed.PrimaryBranches) == 0 {
		t.Fatalf("PrimaryBranches=%v, want non-empty", parsed.PrimaryBranches)
	}
	if len(parsed.TopDistributionNodes) == 0 {
		t.Fatalf("TopDistributionNodes=%v, want non-empty", parsed.TopDistributionNodes)
	}
}

func TestBuildAssemblyPayloadFromInputs_RawOrgFallbackBuildsActorOnlyAssembly(t *testing.T) {
	opts := DefaultSupplyChainAssemblyOptions()
	opts.AnchorSet = []string{"/org/kz/meloman-marwin"}
	opts.SourceAnchors = []string{"/org/kz/meloman-marwin"}
	opts.RawNodeRefs = []string{
		"/org/kz/meloman-marwin",
		"/org/kz/meloman-retail",
		"/fac/kz/almaty/distribution-center",
	}
	opts.RawEdges = []SupplyChainAssemblyRawEdge{
		{Source: "/org/kz/meloman-marwin", Target: "/org/kz/meloman-retail", EdgeType: "CONTROLS"},
		{Source: "/org/kz/meloman-marwin", Target: "/fac/kz/almaty/distribution-center", EdgeType: "OWNS"},
	}

	payload, _, nodeRefs, coverage, openGaps := buildAssemblyPayloadFromInputs(opts.AnchorSet, nil, opts)
	if payload == nil {
		t.Fatal("expected payload")
	}
	if coverage == nil {
		t.Fatal("expected coverage")
	}
	if coverage.ProductsN != 0 {
		t.Fatalf("coverage.ProductsN=%d, want 0", coverage.ProductsN)
	}
	if coverage.ActorsN != 3 {
		t.Fatalf("coverage.ActorsN=%d, want 3", coverage.ActorsN)
	}
	if len(nodeRefs) != 3 {
		t.Fatalf("len(nodeRefs)=%d, want 3", len(nodeRefs))
	}
	if len(openGaps) != 0 {
		t.Fatalf("openGaps=%v, want none", openGaps)
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if len(parsed.ActorNodes) != 3 {
		t.Fatalf("len(parsed.ActorNodes)=%d, want 3", len(parsed.ActorNodes))
	}
	if len(parsed.AnchorSet) != 1 || parsed.AnchorSet[0] != "/org/kz/meloman-marwin" {
		t.Fatalf("AnchorSet=%v", parsed.AnchorSet)
	}
	if len(parsed.AnchorComponents) != 1 {
		t.Fatalf("len(parsed.AnchorComponents)=%d, want 1", len(parsed.AnchorComponents))
	}
	if len(parsed.AnchorComponents[0].ActorRefs) != 3 {
		t.Fatalf("AnchorComponents[0].ActorRefs=%v, want 3 actor refs", parsed.AnchorComponents[0].ActorRefs)
	}
	if len(parsed.PrimaryBranches) == 0 {
		t.Fatalf("PrimaryBranches=%v, want non-empty", parsed.PrimaryBranches)
	}
}

func TestBuildAssemblyPayloadFromContributions_MalformedProductTypedActorFallsBackToActorNode(t *testing.T) {
	opts := DefaultSupplyChainAssemblyOptions()
	opts.AnchorSet = []string{"/org/kz/meloman-marwin"}
	opts.SourceAnchors = []string{"/org/kz/meloman-marwin"}
	opts.RawNodeRefs = []string{
		"/org/kz/meloman-marwin",
		"/org/kz/meloman-publishing",
	}
	opts.RawEdges = []SupplyChainAssemblyRawEdge{
		{Source: "/org/kz/meloman-marwin", Target: "/org/kz/meloman-publishing", EdgeType: "CONTROLS"},
	}

	payload, _, _, coverage, _ := buildAssemblyPayloadFromContributions(opts.AnchorSet, []assemblyContribution{
		{
			canonicalKey: "/supply-chain-path/global/malformed-meloman",
			parsed: &models.SupplyChainPathPayload{
				Nodes: []models.SupplyChainPathNode{
					{Type: "product", Ref: "/org/kz/meloman-distribution"},
				},
			},
		},
	}, opts)
	if payload == nil {
		t.Fatal("expected payload")
	}
	if coverage == nil {
		t.Fatal("expected coverage")
	}
	if coverage.ProductsN != 0 {
		t.Fatalf("coverage.ProductsN=%d, want 0", coverage.ProductsN)
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if len(parsed.ProductNodes) != 0 {
		t.Fatalf("len(parsed.ProductNodes)=%d, want 0", len(parsed.ProductNodes))
	}
	foundActor := false
	for _, node := range parsed.ActorNodes {
		if node.Ref == "/org/kz/meloman-distribution" {
			foundActor = true
			break
		}
	}
	if !foundActor {
		t.Fatalf("ActorNodes=%v, want /org/kz/meloman-distribution present", parsed.ActorNodes)
	}
}

func TestBuildAssemblyPayloadFromContributions_PackagingSupportDoesNotOutrankGoods(t *testing.T) {
	opts := DefaultSupplyChainAssemblyOptions()
	opts.SourceAnchors = []string{"/org/kz/meloman-marwin", "/product/material/packaging-carton"}
	payload, _, _, _, _ := buildAssemblyPayloadFromContributions([]string{"/org/kz/meloman-marwin"}, []assemblyContribution{
		{
			canonicalKey: "/supply-chain-path/global/books",
			parsed: &models.SupplyChainPathPayload{
				Legs: []models.SupplyChainPathLeg{
					{SupplierRef: "/org/de/book-factory", ProductRef: "/product/goods/books/children-books", CustomerRef: "/fac/kz/astana/distribution-center", SupplyLinkCanonicalPath: "/supply-link/global/books", TransportViaRefs: []string{"/fac/kz/astana/customs-terminal"}, SegmentConfidence: 0.92},
				},
			},
		},
		{
			canonicalKey: "/supply-chain-path/global/packaging",
			parsed: &models.SupplyChainPathPayload{
				Legs: []models.SupplyChainPathLeg{
					{SupplierRef: "/org/kz/carton-mill", ProductRef: "/product/material/packaging-carton", CustomerRef: "/fac/kz/astana/distribution-center", SupplyLinkCanonicalPath: "/supply-link/global/carton", SegmentConfidence: 0.71},
				},
			},
		},
	}, opts)
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if len(parsed.PrimaryBranches) == 0 {
		t.Fatalf("PrimaryBranches=%v, want non-empty", parsed.PrimaryBranches)
	}
	if parsed.PrimaryBranches[0].BranchClass == branchClassPackagingSupport {
		t.Fatalf("primary branch incorrectly promoted packaging support: %+v", parsed.PrimaryBranches[0])
	}
	foundSuppressedPackaging := false
	for _, branch := range parsed.SuppressedSupportBranches {
		if branch.BranchClass == branchClassPackagingSupport {
			foundSuppressedPackaging = true
			break
		}
	}
	if !foundSuppressedPackaging {
		t.Fatalf("SuppressedSupportBranches=%v, want packaging support branch", parsed.SuppressedSupportBranches)
	}
}
