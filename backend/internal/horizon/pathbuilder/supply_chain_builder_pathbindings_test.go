package pathbuilder

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
		if edge.Relation == ah.RelationNameLegConnects {
			foundRouteEdge = true
			role, _ := edge.Qualifiers["role"].(string)
			if edge.CarriesProduct && edge.ProductRef == "/product/raw/hydrocarbon/distribution/lng" && role == "logistics_leg" {
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
		if edge.Relation != ah.RelationNameLegConnects || edge.Qualifiers == nil {
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
	if legs[0].RouteVariantKey == "" {
		t.Fatal("expected routeVariantKey on leg")
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

func TestSelectPrimaryAndAlternateFlowRefs_PrefersResolvedGeometryFlows(t *testing.T) {
	primary, alternate := selectPrimaryAndAlternateFlowRefs(
		[]string{"sea_us_gulf_jebel_ali_via_suez"},
		[]string{"legacy_guess"},
		[]string{"sea_jebel_ali_sohar_feeder"},
		"sea",
	)
	if len(primary) != 1 || primary[0] != "sea_jebel_ali_sohar_feeder" {
		t.Fatalf("primary=%v, want resolved geometry flow", primary)
	}
	if len(alternate) != 2 {
		t.Fatalf("alternate=%v, want both legacy alternatives", alternate)
	}
}

func TestSelectPrimaryAndAlternateFlowRefs_PrefersSeaLabelOverLandWhenModeIsSea(t *testing.T) {
	primary, alternate := selectPrimaryAndAlternateFlowRefs(
		nil,
		nil,
		[]string{"land_port_of_shanghai_baowu_dri", "sea_shanghai_sohar_via_malacca"},
		"sea",
	)
	if len(primary) != 2 || primary[0] != "sea_shanghai_sohar_via_malacca" {
		t.Fatalf("primary=%v, want sea flow first", primary)
	}
	if len(alternate) != 0 {
		t.Fatalf("alternate=%v, want empty", alternate)
	}
}

func TestPruneAbstractNetworkViaRefs_DropsSyntheticRailNetworkWhenRailFlowExists(t *testing.T) {
	got := PruneAbstractNetworkViaRefs(
		"rail",
		[]string{"rail_russia_europe"},
		[]string{"/fac/ru/rzd-rail-network", "/fac/kz/dostyk-rail-terminal"},
	)
	want := []string{"/fac/kz/dostyk-rail-terminal"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestPruneAbstractNetworkViaRefs_DropsSyntheticRailNetworkWithoutRailBinding(t *testing.T) {
	got := PruneAbstractNetworkViaRefs(
		"",
		nil,
		[]string{"/fac/kz/ktz-rail-network", "/fac/kz/taraz-rail-terminal"},
	)
	want := []string{"/fac/kz/taraz-rail-terminal"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestPruneAbstractNetworkViaRefs_DropsSyntheticDistributionNetwork(t *testing.T) {
	got := PruneAbstractNetworkViaRefs(
		"rail",
		[]string{"rail_russia_europe"},
		[]string{"/fac/ru/phosagro-region-distribution-network", "/fac/ru-kz/iletsk-rail-crossing"},
	)
	want := []string{"/fac/ru-kz/iletsk-rail-crossing"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("via=%v, want %v", got, want)
	}
}

func TestCollapseAbstractNetworkSegments_MergesSyntheticRailNetworkHop(t *testing.T) {
	segments := collapseAbstractNetworkSegments([]PathSegment{
		{
			SupplierRef: "/fac/ru/balakovo-apatit-plant",
			ProductRef:  "/product/chemical/fertilizer/ammonium-sulphate",
			CustomerRef: "/fac/ru/rzd-rail-network",
			Confidence:  0.8,
		},
		{
			SupplierRef: "/fac/ru/rzd-rail-network",
			ProductRef:  "/product/chemical/fertilizer/ammonium-sulphate",
			CustomerRef: "/fac/kz/phosagro-kazakhstan-astana-dc",
			TransportViaRefs: []string{
				"/fac/kz/dostyk-rail-terminal",
			},
			Confidence: 0.7,
		},
	})
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	if segments[0].SupplierRef != "/fac/ru/balakovo-apatit-plant" || segments[0].CustomerRef != "/fac/kz/phosagro-kazakhstan-astana-dc" {
		t.Fatalf("merged segment=%+v", segments[0])
	}
	if len(segments[0].TransportViaRefs) != 1 || segments[0].TransportViaRefs[0] != "/fac/kz/dostyk-rail-terminal" {
		t.Fatalf("merged via=%v", segments[0].TransportViaRefs)
	}
}

func TestCollapseAbstractNetworkSegments_MergesSyntheticDistributionNetworkHop(t *testing.T) {
	segments := collapseAbstractNetworkSegments([]PathSegment{
		{
			SupplierRef: "/fac/ru/balakovo-apatit-plant",
			ProductRef:  "/product/chemical/fertilizer/ammonium-sulphate",
			CustomerRef: "/fac/ru/phosagro-region-distribution-network",
			Confidence:  0.8,
		},
		{
			SupplierRef: "/fac/ru/phosagro-region-distribution-network",
			ProductRef:  "/product/chemical/fertilizer/ammonium-sulphate",
			CustomerRef: "/fac/kz/astana-agrochemical-hub",
			TransportViaRefs: []string{
				"/fac/ru-kz/iletsk-rail-crossing",
			},
			Confidence: 0.7,
		},
	})
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	if segments[0].SupplierRef != "/fac/ru/balakovo-apatit-plant" || segments[0].CustomerRef != "/fac/kz/astana-agrochemical-hub" {
		t.Fatalf("merged segment=%+v", segments[0])
	}
	if len(segments[0].TransportViaRefs) != 1 || segments[0].TransportViaRefs[0] != "/fac/ru-kz/iletsk-rail-crossing" {
		t.Fatalf("merged via=%v", segments[0].TransportViaRefs)
	}
}

func TestBuildSupplyChainArtifactNotes_IncludeContractAndStressMetadata(t *testing.T) {
	path := &SupplyChainPath{
		Confidence: 0.77,
		BuildMode:  "test_mode",
		MaxDepth:   4,
	}
	stress := &models.SupplyChainPathStress{
		PathScore:          0.61,
		BottleneckLegIndex: 2,
		RecoveryLatencyHrs: 48,
	}
	notes := buildSupplyChainArtifactNotes(path, []models.SupplyChainPathLeg{{Index: 0}, {Index: 1}}, stress)
	if notes["graphLayer"] != "path_carried" {
		t.Fatalf("graphLayer=%v, want path_carried", notes["graphLayer"])
	}
	if notes["artifactKind"] != "SUPPLY_CHAIN_PATH" {
		t.Fatalf("artifactKind=%v, want SUPPLY_CHAIN_PATH", notes["artifactKind"])
	}
	if notes["legCount"] != 2 {
		t.Fatalf("legCount=%v, want 2", notes["legCount"])
	}
	if _, ok := notes["pathCarriedEdges"]; !ok {
		t.Fatal("expected pathCarriedEdges in notes")
	}
	if notes["pathStressScore"] != 0.61 {
		t.Fatalf("pathStressScore=%v, want 0.61", notes["pathStressScore"])
	}
}

func TestPathToMaterializedPayload_PreservesOptionalVesselType(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{{
			SupplierRef:      "/fac/sa/ras-tanura-terminal",
			ProductRef:       "/product/raw/hydrocarbon/crude-oil",
			CustomerRef:      "/fac/in/jamnagar-refinery",
			TransportViaRefs: []string{"/fac/eg/suez-terminal"},
			VesselType:       "tanker",
			Confidence:       0.81,
		}},
		Confidence: 0.81,
		BuildMode:  "test",
		MaxDepth:   3,
	}

	_, edges, legs, _ := builder.pathToMaterializedPayload(context.Background(), path)
	if len(legs) != 1 {
		t.Fatalf("legs=%d, want 1", len(legs))
	}
	if legs[0].VesselType != "tanker" {
		t.Fatalf("leg vesselType=%q, want tanker", legs[0].VesselType)
	}
	found := false
	for _, edge := range edges {
		if edge.Relation != ah.RelationNameLegConnects || edge.Qualifiers == nil {
			continue
		}
		if edge.Qualifiers["vesselType"] == "tanker" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected vesselType qualifier on logistics edge")
	}
}

func TestPrepareMaterializedPathArtifactsBatch_GroupsRouteVariantsByStructuralKey(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	paths := []*SupplyChainPath{
		{
			Segments: []PathSegment{{
				SupplierRef:             "/org/us/supplier",
				ProductRef:              "/product/mining/iron-ore",
				CustomerRef:             "/org/io/customer",
				SupplyLinkCanonicalPath: "/supply-link/global/supplier/product/customer",
				RouteVariantKey:         "via-suez",
				TransportViaRefs:        []string{"/fac/eg/suez-terminal"},
				Confidence:              0.81,
			}},
		},
		{
			Segments: []PathSegment{{
				SupplierRef:             "/org/us/supplier",
				ProductRef:              "/product/mining/iron-ore",
				CustomerRef:             "/org/io/customer",
				SupplyLinkCanonicalPath: "/supply-link/global/supplier/product/customer",
				RouteVariantKey:         "via-cape",
				TransportViaRefs:        []string{"/fac/za/cape-of-good-hope-terminal"},
				Confidence:              0.79,
			}},
		},
	}
	canonicalKey := BuildStructuralCanonicalKey("global", paths[0].Segments)
	artifacts, keys, err := builder.PrepareMaterializedPathArtifactsBatch(
		context.Background(),
		paths,
		"global",
		"lens-1",
		[]string{canonicalKey, canonicalKey},
	)
	if err != nil {
		t.Fatalf("PrepareMaterializedPathArtifactsBatch() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%d, want 1", len(artifacts))
	}
	if len(keys) != 1 || keys[0] != canonicalKey {
		t.Fatalf("keys=%v, want [%s]", keys, canonicalKey)
	}
	parsed, err := models.ParseSupplyChainPathPayload(artifacts[0].Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if len(parsed.Legs) != 1 {
		t.Fatalf("legs=%d, want 1 structural leg", len(parsed.Legs))
	}
	if len(parsed.Legs[0].RouteOverlays) != 2 {
		t.Fatalf("routeOverlays=%d, want 2", len(parsed.Legs[0].RouteOverlays))
	}
}

func TestBuildStructuralCanonicalKey_DiffersForDifferentIntermediateStructure(t *testing.T) {
	first := []PathSegment{
		{
			SupplierRef:             "/fac/vn/binh-tan-factory",
			ProductRef:              "/product/finished/mechanical/assembly/footwear",
			CustomerRef:             "/fac/be/nike-european-logistics-center",
			SupplyLinkCanonicalPath: "/supply-link/global/binh-tan/footwear/nike-elc",
		},
		{
			SupplierRef:             "/org/nl/nike-european-operations-netherlands",
			ProductRef:              "/product/finished/mechanical/assembly/footwear",
			CustomerRef:             "/fac/kz/almaty-airport-terminal",
			SupplyLinkCanonicalPath: "/supply-link/global/nike-europe-ops/footwear/almaty-airport-terminal",
		},
	}
	second := []PathSegment{
		{
			SupplierRef:             "/fac/vn/binh-tan-factory",
			ProductRef:              "/product/finished/mechanical/assembly/footwear",
			CustomerRef:             "/fac/de/frankfurt-airport",
			SupplyLinkCanonicalPath: "/supply-link/global/binh-tan/footwear/frankfurt-airport",
		},
		{
			SupplierRef:             "/org/de/adidas",
			ProductRef:              "/product/finished/mechanical/assembly/footwear",
			CustomerRef:             "/fac/kz/almaty-airport-terminal",
			SupplyLinkCanonicalPath: "/supply-link/global/adidas/footwear/almaty-airport-terminal",
		},
	}
	firstKey := BuildStructuralCanonicalKey("global", first)
	secondKey := BuildStructuralCanonicalKey("global", second)
	if firstKey == "" || secondKey == "" {
		t.Fatalf("expected non-empty keys, got %q and %q", firstKey, secondKey)
	}
	if firstKey == secondKey {
		t.Fatalf("expected distinct keys for different intermediate structure, got %q", firstKey)
	}
}

func TestMergeMaterializedPathArtifacts_ReplacesMismatchedLegacyPayload(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	firstPath := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef:             "/fac/vn/binh-tan-factory",
				ProductRef:              "/product/finished/mechanical/assembly/footwear",
				CustomerRef:             "/fac/be/nike-european-logistics-center",
				SupplyLinkCanonicalPath: "/supply-link/global/binh-tan/footwear/nike-elc",
			},
			{
				SupplierRef:             "/org/nl/nike-european-operations-netherlands",
				ProductRef:              "/product/finished/mechanical/assembly/footwear",
				CustomerRef:             "/fac/kz/almaty-airport-terminal",
				SupplyLinkCanonicalPath: "/supply-link/global/nike-europe-ops/footwear/almaty-airport-terminal",
			},
		},
	}
	secondPath := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef:             "/fac/vn/binh-tan-factory",
				ProductRef:              "/product/finished/mechanical/assembly/footwear",
				CustomerRef:             "/fac/de/frankfurt-airport",
				SupplyLinkCanonicalPath: "/supply-link/global/binh-tan/footwear/frankfurt-airport",
			},
			{
				SupplierRef:             "/org/de/adidas",
				ProductRef:              "/product/finished/mechanical/assembly/footwear",
				CustomerRef:             "/fac/kz/almaty-airport-terminal",
				SupplyLinkCanonicalPath: "/supply-link/global/adidas/footwear/almaty-airport-terminal",
			},
		},
	}
	key := "/supply-chain/global/fac_vn_binh-tan-factory/product_finished_mechanical_assembly_footwear/fac_kz_almaty-airport-terminal"
	existing := buildMaterializedArtifactFromDraft(firstPath, builder.BuildMaterializationDraft(context.Background(), firstPath), "global", "lens-1", key, builder.materializedKind())
	incoming := buildMaterializedArtifactFromDraft(secondPath, builder.BuildMaterializationDraft(context.Background(), secondPath), "global", "lens-1", key, builder.materializedKind())
	merged, err := mergeMaterializedPathArtifacts(existing, incoming)
	if err != nil {
		t.Fatalf("mergeMaterializedPathArtifacts() error = %v", err)
	}
	parsed, err := models.ParseSupplyChainPathPayload(merged.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if len(parsed.Legs) != 2 {
		t.Fatalf("legs=%d, want incoming replacement shape", len(parsed.Legs))
	}
	if parsed.Legs[0].SupplyLinkCanonicalPath != "/supply-link/global/binh-tan/footwear/frankfurt-airport" {
		t.Fatalf("first leg=%q, want incoming replacement", parsed.Legs[0].SupplyLinkCanonicalPath)
	}
}

func TestMergeMaterializedPathArtifacts_PreservesExistingLensID(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	path := &SupplyChainPath{
		Segments: []PathSegment{
			{
				SupplierRef:             "/fac/cn/dongguan-factory",
				ProductRef:              "/product/finished/mechanical/assembly/footwear",
				CustomerRef:             "/fac/kz/khorgos-gateway-terminal",
				SupplyLinkCanonicalPath: "/supply-link/global/dongguan/footwear/khorgos-gateway-terminal",
			},
		},
	}
	key := BuildStructuralCanonicalKey("global", path.Segments)
	existing := buildMaterializedArtifactFromDraft(path, builder.BuildMaterializationDraft(context.Background(), path), "global", "lens-existing", key, builder.materializedKind())
	incoming := buildMaterializedArtifactFromDraft(path, builder.BuildMaterializationDraft(context.Background(), path), "global", "lens-incoming", key, builder.materializedKind())
	merged, err := mergeMaterializedPathArtifacts(existing, incoming)
	if err != nil {
		t.Fatalf("mergeMaterializedPathArtifacts() error = %v", err)
	}
	if got := strings.TrimSpace(merged.LensID); got != "lens-existing" {
		t.Fatalf("lensID=%q, want existing lens id preserved", got)
	}
}

func TestSelectArtifactRebuildPath_PrefersAnchorMatchingPath(t *testing.T) {
	artifact := &models.MaterializedArtifact{
		AnchorSupplier: "/fac/vn/binh-tan-factory",
		AnchorCustomer: "/fac/kz/almaty-airport-terminal",
		ProductAxis:    "/product/finished/mechanical/assembly/footwear",
		Payload: models.BuildSupplyChainPathPayloadDetailed(
			nil,
			nil,
			[]models.SupplyChainPathLeg{
				{
					Index:       0,
					SupplierRef: "/fac/vn/binh-tan-factory",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/be/nike-european-logistics-center",
				},
				{
					Index:       1,
					SupplierRef: "/org/nl/nike-european-operations-netherlands",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/kz/almaty-airport-terminal",
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}
	paths := []*SupplyChainPath{
		{
			Confidence: 0.95,
			Segments: []PathSegment{
				{
					SupplierRef: "/fac/de/adidas-cdc-rieste",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/de/frankfurt-airport",
				},
				{
					SupplierRef: "/org/de/adidas",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/kz/almaty-airport-terminal",
				},
			},
		},
		{
			Confidence: 0.81,
			Segments: []PathSegment{
				{
					SupplierRef: "/fac/vn/binh-tan-factory",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/be/nike-european-logistics-center",
				},
				{
					SupplierRef: "/org/nl/nike-european-operations-netherlands",
					ProductRef:  "/product/finished/mechanical/assembly/footwear",
					CustomerRef: "/fac/kz/almaty-airport-terminal",
				},
			},
		},
	}

	selected := selectArtifactRebuildPath(artifact, paths, "/fac/kz/almaty-airport-terminal")
	if selected == nil {
		t.Fatal("expected selected path")
	}
	if selected.Segments[0].SupplierRef != "/fac/vn/binh-tan-factory" {
		t.Fatalf("selected supplier=%q, want anchor-matching path", selected.Segments[0].SupplierRef)
	}
}

func TestSupplyChainPathBuilder_MaterializationStageNamesIncludeRouteOverlays(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	stageNames := builder.MaterializationStageNames()
	found := false
	for _, stage := range stageNames {
		if stage == string(SupplyChainMaterializationStageRouteOverlays) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("stages=%v, want route_overlays", stageNames)
	}
}

func TestPrepareMaterializedPathArtifactsBatch_ReportsUnresolvedSupplyLinkRefs(t *testing.T) {
	builder := NewSupplyChainPathBuilder(nil, nil, nil)
	_, _, err := builder.PrepareMaterializedPathArtifactsBatch(
		context.Background(),
		[]*SupplyChainPath{{
			Segments: []PathSegment{{
				SupplierRef: "/org/kz/damu-entrepreneurship-development-fund",
				ProductRef:  "/product/finished/consumer/footwear",
				CustomerRef: "/market/kz/retail/footwear",
			}},
			Confidence: 0.74,
			BuildMode:  "test",
			MaxDepth:   2,
		}},
		"global",
		"lens-test",
		[]string{"/supply-chain/global/damu-footwear"},
	)
	if err == nil {
		t.Fatal("expected unresolved supply-link validation error")
	}
	errText := err.Error()
	if !strings.Contains(errText, "key=/supply-chain/global/damu-footwear") {
		t.Fatalf("error=%q, want canonical key", errText)
	}
	if !strings.Contains(errText, "unresolved supply-link refs=") {
		t.Fatalf("error=%q, want unresolved supply-link details", errText)
	}
	if !strings.Contains(errText, "/org/kz/damu-entrepreneurship-development-fund -> /product/finished/consumer/footwear -> /market/kz/retail/footwear") {
		t.Fatalf("error=%q, want segment triple", errText)
	}
}
