package models

import (
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestMaterializedArtifact_Validate(t *testing.T) {
	tests := []struct {
		name     string
		artifact *MaterializedArtifact
		wantErr  bool
	}{
		{
			name: "valid SUPPLY_CHAIN_PATH",
			artifact: &MaterializedArtifact{
				TenantID:     "bmw",
				Kind:         MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/bmw/test-path",
				Payload: map[string]interface{}{
					"v": float64(1), // JSON unmarshal uses float64 for numbers
					"nodes": []interface{}{
						map[string]interface{}{"class": "org", "ref": "/org/catl"},
						map[string]interface{}{"class": "product", "ref": "/product/component/electronic/manufacturing/battery-cell"},
						map[string]interface{}{"class": "org", "ref": "/org/bmw"},
					},
				},
				Status:    "active",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "valid SUPPLY_CHAIN_PATH built payload",
			artifact: &MaterializedArtifact{
				TenantID:     "bmw",
				Kind:         MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/bmw/test-built-path",
				Payload: BuildSupplyChainPathPayload(
					[]SupplyChainPathNode{
						{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
						{Class: ah.FragmentClassProduct, Ref: "/product/component/electronic/manufacturing/battery-cell"},
						{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
					},
					[]SupplyChainPathEdge{
						{
							Relation:                ah.RelationNameSupplies,
							From:                    0,
							To:                      2,
							ViaProductIndex:         intPtr(1),
							ProductNodeIndex:        intPtr(1),
							ProductRef:              "/product/component/electronic/manufacturing/battery-cell",
							SupplyLinkCanonicalPath: "/supply-link/bmw/org-catl/product-component-electronic-manufacturing-battery-cell/org-bmw",
							SegmentIndex:            intPtr(0),
							CarriesProduct:          true,
							Qualifiers:              map[string]interface{}{"role": "product_flow"},
						},
					},
					nil,
					nil,
					nil,
				),
				Status:    "active",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing tenantId",
			artifact: &MaterializedArtifact{
				Kind:         MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/test",
				Payload:      map[string]interface{}{"v": 1, "nodes": []interface{}{}},
			},
			wantErr: true,
		},
		{
			name: "invalid kind",
			artifact: &MaterializedArtifact{
				TenantID:     "bmw",
				Kind:         MaterializedKind("INVALID"),
				CanonicalKey: "/supply-chain/test",
				Payload:      map[string]interface{}{"v": 1, "nodes": []interface{}{}},
			},
			wantErr: true,
		},
		{
			name: "missing canonicalKey",
			artifact: &MaterializedArtifact{
				TenantID: "bmw",
				Kind:     MaterializedKindSupplyChainPath,
				Payload:  map[string]interface{}{"v": 1, "nodes": []interface{}{}},
			},
			wantErr: true,
		},
		{
			name: "invalid payload version",
			artifact: &MaterializedArtifact{
				TenantID:     "bmw",
				Kind:         MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/test",
				Payload:      map[string]interface{}{"v": float64(2), "nodes": []interface{}{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.artifact.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("MaterializedArtifact.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildSupplyChainPathPayload(t *testing.T) {
	nodes := []SupplyChainPathNode{
		{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
		{Class: ah.FragmentClassProduct, Ref: "/product/.../battery-cell"},
		{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
	}
	edges := []SupplyChainPathEdge{
		{
			Relation:                ah.RelationNameSupplies,
			From:                    0,
			To:                      2,
			ViaProductIndex:         intPtr(1),
			ProductNodeIndex:        intPtr(1),
			ProductRef:              "/product/.../battery-cell",
			SupplyLinkCanonicalPath: "/supply-link/bmw/org-catl/product-battery-cell/org-bmw",
			SegmentIndex:            intPtr(0),
			CarriesProduct:          true,
			Qualifiers:              map[string]interface{}{"role": "product_flow"},
		},
	}
	notes := map[string]interface{}{
		"buildMode": "tenant_first_global_fallback",
		"maxDepth":  6,
	}

	payload := BuildSupplyChainPathPayload(nodes, edges, nil, nil, notes)

	if payload["v"] != 1 {
		t.Errorf("expected version 1, got %v", payload["v"])
	}

	payloadNodes, ok := payload["nodes"].([]SupplyChainPathNode)
	if !ok {
		t.Errorf("nodes type assertion failed")
	}
	if len(payloadNodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(payloadNodes))
	}

	payloadEdges, ok := payload["edges"].([]SupplyChainPathEdge)
	if !ok {
		t.Errorf("edges type assertion failed")
	}
	if len(payloadEdges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(payloadEdges))
	}
}

func TestBuildSupplyChainPathPayloadWithSufficiency(t *testing.T) {
	payload := BuildSupplyChainPathPayloadWithSufficiency(
		[]SupplyChainPathNode{{Class: ah.FragmentClassOrg, Ref: "/org/catl"}},
		nil,
		nil,
		nil,
		&SupplyChainPathSufficiency{
			Status:  SupplyChainSufficiencyHeuristic,
			Score:   0.63,
			Summary: "Heuristic continuity",
		},
		map[string]interface{}{"buildMode": "test"},
	)
	parsed, err := ParseSupplyChainPathPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if parsed.Sufficiency == nil {
		t.Fatal("expected sufficiency in parsed payload")
	}
	if parsed.Sufficiency.Status != SupplyChainSufficiencyHeuristic {
		t.Fatalf("status=%s, want %s", parsed.Sufficiency.Status, SupplyChainSufficiencyHeuristic)
	}
}

func TestBuildSupplyChainPathPayloadDetailed_WithFamily(t *testing.T) {
	payload := BuildSupplyChainPathPayloadDetailed(
		[]SupplyChainPathNode{{Class: ah.FragmentClassOrg, Ref: "/org/catl"}},
		nil,
		nil,
		nil,
		&SupplyChainPathSufficiency{Status: SupplyChainSufficiencyValidated, Score: 1},
		&SupplyChainPathFamily{
			ID:                 "/supply-chain-family/global/customer_product/org-bmw/product-battery-cell",
			PathCount:          3,
			ValidatedPathCount: 1,
			GapPathCount:       1,
			OpenGaps:           []string{"Resolve transport evidence"},
		},
		map[string]interface{}{"buildMode": "test"},
	)
	parsed, err := ParseSupplyChainPathPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if parsed.Family == nil {
		t.Fatal("expected family in parsed payload")
	}
	if parsed.Family.PathCount != 3 {
		t.Fatalf("family pathCount=%d, want 3", parsed.Family.PathCount)
	}
}

func TestBuildSupplyChainPathPayloadDetailed_IncludesMetricSummary(t *testing.T) {
	payload := BuildSupplyChainPathPayloadDetailed(
		[]SupplyChainPathNode{{Class: ah.FragmentClassOrg, Ref: "/org/us/slb"}},
		nil,
		[]SupplyChainPathLeg{{Index: 0, BindingConfidence: 0.72}},
		&SupplyChainPathStress{
			PathScore:          0.81,
			RecoveryLatencyHrs: 96,
			BottleneckLegIndex: 0,
		},
		nil,
		nil,
		nil,
	)
	metrics, ok := payload["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected payload metrics block, got %#v", payload["metrics"])
	}
	if got := metrics["pathStressScore"]; got != 0.81 {
		t.Fatalf("pathStressScore=%v, want 0.81", got)
	}
	if got := metrics["legCount"]; got != 1 {
		t.Fatalf("legCount=%v, want 1", got)
	}
}

func TestPopulateMaterializedArtifactIndexesSetsPathMetricIndexes(t *testing.T) {
	artifact := &MaterializedArtifact{
		Kind: MaterializedKindSupplyChainPath,
		Payload: BuildSupplyChainPathPayloadDetailed(
			[]SupplyChainPathNode{{Class: ah.FragmentClassOrg, Ref: "/org/us/slb"}},
			nil,
			[]SupplyChainPathLeg{{Index: 0, BindingConfidence: 0.66}},
			&SupplyChainPathStress{
				PathScore:          0.74,
				RecoveryLatencyHrs: 72,
				BottleneckLegIndex: 0,
			},
			nil,
			nil,
			nil,
		),
	}
	PopulateMaterializedArtifactIndexes(artifact)
	if artifact.LegCount != 1 {
		t.Fatalf("legCount=%d, want 1", artifact.LegCount)
	}
	if artifact.BindingScore <= 0.65 || artifact.BindingScore >= 0.67 {
		t.Fatalf("bindingScore=%.3f, want ~0.66", artifact.BindingScore)
	}
	if artifact.PathStressScore != 0.74 {
		t.Fatalf("pathStressScore=%.2f, want 0.74", artifact.PathStressScore)
	}
	if artifact.RecoveryLatencyHrs != 72 {
		t.Fatalf("recoveryLatencyHrs=%.1f, want 72", artifact.RecoveryLatencyHrs)
	}
}

func TestParseSupplyChainPathPayload(t *testing.T) {
	payload := map[string]interface{}{
		"v": float64(1), // JSON unmarshal uses float64
		"nodes": []interface{}{
			map[string]interface{}{"class": "org", "ref": "/org/catl"},
			map[string]interface{}{"class": "product", "ref": "/product/.../battery-cell"},
			map[string]interface{}{"class": "org", "ref": "/org/bmw"},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"relation":                "supplies",
				"from":                    float64(0),
				"to":                      float64(2),
				"viaProductIndex":         float64(1),
				"productNodeIndex":        float64(1),
				"productRef":              "/product/.../battery-cell",
				"supplyLinkCanonicalPath": "/supply-link/bmw/org-catl/product-battery-cell/org-bmw",
				"segmentIndex":            float64(0),
				"carriesProduct":          true,
				"qualifiers":              map[string]interface{}{"role": "product_flow"},
			},
		},
		"stress": map[string]interface{}{
			"pathScore":            0.71,
			"fragility":            0.63,
			"rerouteability":       0.28,
			"recoveryLatencyHours": 72.0,
			"bottleneckLegIndex":   float64(0),
		},
		"notes": map[string]interface{}{
			"buildMode": "tenant_first",
		},
	}

	parsed, err := ParseSupplyChainPathPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}

	if parsed.Version != 1 {
		t.Errorf("expected version 1, got %d", parsed.Version)
	}
	if len(parsed.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(parsed.Nodes))
	}
	if len(parsed.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(parsed.Edges))
	}
	if parsed.Edges[0].ProductRef != "/product/.../battery-cell" || !parsed.Edges[0].CarriesProduct {
		t.Fatalf("expected carried product metadata on parsed edge: %+v", parsed.Edges[0])
	}
	if parsed.Nodes[0].Ref != "/org/catl" {
		t.Errorf("expected first node ref /org/catl, got %s", parsed.Nodes[0].Ref)
	}
	if parsed.Stress == nil || parsed.Stress.PathScore <= 0 {
		t.Fatal("expected parsed stress")
	}

	t.Run("typed arrays from builder", func(t *testing.T) {
		builtPayload := BuildSupplyChainPathPayload(
			[]SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
				{Class: ah.FragmentClassProduct, Ref: "/product/.../battery-cell"},
				{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
			},
			[]SupplyChainPathEdge{
				{Relation: ah.RelationNameSupplies, From: 0, To: 2, ViaProductIndex: intPtr(1), ProductNodeIndex: intPtr(1), ProductRef: "/product/.../battery-cell", SupplyLinkCanonicalPath: "/supply-link/bmw/org-catl/product-battery-cell/org-bmw", SegmentIndex: intPtr(0), CarriesProduct: true, Qualifiers: map[string]interface{}{"role": "product_flow"}},
			},
			nil,
			nil,
			nil,
		)

		typedParsed, err := ParseSupplyChainPathPayload(builtPayload)
		if err != nil {
			t.Fatalf("ParseSupplyChainPathPayload() typed payload error = %v", err)
		}
		if len(typedParsed.Nodes) != 3 {
			t.Fatalf("expected 3 nodes, got %d", len(typedParsed.Nodes))
		}
		if len(typedParsed.Edges) != 1 {
			t.Fatalf("expected 1 edge, got %d", len(typedParsed.Edges))
		}
	})
}

func TestValidateSupplyChainPathStructure(t *testing.T) {
	tests := []struct {
		name    string
		nodes   []SupplyChainPathNode
		edges   []SupplyChainPathEdge
		wantErr bool
	}{
		{
			name: "valid path",
			nodes: []SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
				{Class: ah.FragmentClassProduct, Ref: "/product/.../battery-cell"},
				{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Relation: ah.RelationNameSupplies, From: 0, To: 2, ViaProductIndex: intPtr(1), ProductNodeIndex: intPtr(1), ProductRef: "/product/.../battery-cell"},
			},
			wantErr: false,
		},
		{
			name:    "empty nodes",
			nodes:   []SupplyChainPathNode{},
			edges:   []SupplyChainPathEdge{},
			wantErr: true,
		},
		{
			name: "edge out of range",
			nodes: []SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
			},
			edges: []SupplyChainPathEdge{
				{Relation: ah.RelationNameSupplies, From: 0, To: 5},
			},
			wantErr: true,
		},
		{
			name: "viaProductIndex points to non-product",
			nodes: []SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
				{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Relation: ah.RelationNameSupplies, From: 0, To: 1, ViaProductIndex: intPtr(0)},
			},
			wantErr: true,
		},
		{
			name: "productNodeIndex points to non-product",
			nodes: []SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/catl"},
				{Class: ah.FragmentClassOrg, Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Relation: ah.RelationNameLegConnects, From: 0, To: 1, ProductNodeIndex: intPtr(0), ProductRef: "/product/.../battery-cell"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSupplyChainPathStructure(tt.nodes, tt.edges)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSupplyChainPathStructure() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepairSupplyChainPathLinkage_InferLegacySegmentIndex(t *testing.T) {
	artifact := &MaterializedArtifact{
		TenantID:     "global",
		Kind:         MaterializedKindSupplyChainPath,
		CanonicalKey: "/supply-chain/global/org-a/product-b/org-c",
		Payload: BuildSupplyChainPathPayload(
			[]SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/a"},
				{Class: ah.FragmentClassProduct, Ref: "/product/component/test/widget"},
				{Class: ah.FragmentClassSite, Ref: "/fac/port"},
				{Class: ah.FragmentClassOrg, Ref: "/org/c"},
			},
			[]SupplyChainPathEdge{
				{
					Relation:                ah.RelationNameSupplies,
					From:                    0,
					To:                      3,
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
				},
				{
					Relation:                ah.RelationNameLegConnects,
					From:                    0,
					To:                      2,
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
				},
				{
					Relation:                ah.RelationNameLegConnects,
					From:                    2,
					To:                      3,
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
				},
			},
			[]SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/org/a",
					CustomerRef:             "/org/c",
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
					SupplierNodeIndex:       intPtr(0),
					ProductNodeIndex:        intPtr(1),
					CustomerNodeIndex:       intPtr(3),
					TransportViaNodeIndices: []int{2},
				},
			},
			nil,
			nil,
		),
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	summary, err := RepairSupplyChainPathLinkage(artifact, nil)
	if err != nil {
		t.Fatalf("RepairSupplyChainPathLinkage() error = %v", err)
	}
	if !summary.Changed || !summary.FullyLinked {
		t.Fatalf("expected changed fully linked summary, got %+v", summary)
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	for i, edge := range parsed.Edges {
		if edge.SegmentIndex == nil || *edge.SegmentIndex != 0 {
			t.Fatalf("edge[%d] missing inferred segmentIndex: %+v", i, edge)
		}
		if edge.Relation != ah.RelationNameSupplies && edge.Relation != ah.RelationNameLegConnects {
			t.Fatalf("edge[%d] unexpected relation: %+v", i, edge)
		}
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact should validate after repair: %v", err)
	}
}

func TestRepairSupplyChainPathLinkage_SynthesizesLegacyLegs(t *testing.T) {
	nodes := []SupplyChainPathNode{
		{Class: ah.FragmentClassOrg, Ref: "/org/a"},
		{Class: ah.FragmentClassProduct, Ref: "/product/component/test/input"},
		{
			Class: ah.FragmentClassOrg,
			Ref:   "/org/b",
			SiteTransforms: []SiteTransform{
				{
					Kind:           "processing",
					InputProducts:  []string{"/product/component/test/input"},
					OutputProducts: []string{"/product/component/test/output"},
				},
			},
		},
		{Class: ah.FragmentClassProduct, Ref: "/product/component/test/output"},
		{Class: ah.FragmentClassOrg, Ref: "/org/c"},
	}
	edges := []SupplyChainPathEdge{
		{
			Relation:                ah.RelationNameSupplies,
			From:                    0,
			To:                      2,
			ViaProductIndex:         intPtr(1),
			ProductRef:              "/product/component/test/input",
			SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-input/org-b",
		},
		{
			Relation:                ah.RelationNameSupplies,
			From:                    2,
			To:                      4,
			ViaProductIndex:         intPtr(3),
			ProductRef:              "/product/component/test/output",
			SupplyLinkCanonicalPath: "/supply-link/global/org-b/product-component-test-output/org-c",
		},
	}
	artifact := &MaterializedArtifact{
		TenantID:     "global",
		Kind:         MaterializedKindSupplyChainPath,
		CanonicalKey: "/supply-chain/global/org-a/product-b/org-c",
		Payload:      BuildSupplyChainPathPayload(nodes, edges, nil, nil, nil),
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	summary, err := RepairSupplyChainPathLinkage(artifact, nil)
	if err != nil {
		t.Fatalf("RepairSupplyChainPathLinkage() error = %v", err)
	}
	if !summary.Changed || !summary.FullyLinked {
		t.Fatalf("expected changed fully linked summary, got %+v", summary)
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if len(parsed.Legs) != 2 {
		t.Fatalf("expected 2 synthesized legs, got %d", len(parsed.Legs))
	}
	for i, edge := range parsed.Edges {
		if edge.Relation != ah.RelationNameSupplies {
			continue
		}
		if edge.SegmentIndex == nil {
			t.Fatalf("edge[%d] missing segmentIndex after repair: %+v", i, edge)
		}
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact should validate after repair: %v", err)
	}
}

func TestBuildSupplyChainAssemblyGraphPayload_ParseAndValidate(t *testing.T) {
	payload := BuildSupplyChainAssemblyGraphPayload(
		[]string{"/product/device/electronics/smartphone"},
		[]SupplyChainAssemblyNode{
			{Class: ah.FragmentClassProduct, Ref: "/product/material/lithium"},
			{Class: ah.FragmentClassProduct, Ref: "/product/device/electronics/smartphone"},
		},
		[]SupplyChainAssemblyNode{
			{
				Class: ah.FragmentClassOrg,
				Ref:   "/org/global/battery-maker",
				Role:  "supplier",
				SiteTransforms: []SiteTransform{
					{Kind: "processing", InputProducts: []string{"/product/material/lithium"}, OutputProducts: []string{"/product/device/electronics/smartphone"}},
				},
			},
			{
				Class: ah.FragmentClassSite,
				Ref:   "/fac/cn/shenzhen/port-yantian",
				Role:  "route",
				SiteTransforms: []SiteTransform{
					{Kind: "assembly", InputProducts: []string{"/product/component/battery-pack"}, OutputProducts: []string{"/product/device/electronics/smartphone"}},
				},
			},
		},
		[]SupplyChainAssemblySupplyEdge{
			{
				SupplierRef:             "/org/global/battery-maker",
				ProductRef:              "/product/material/lithium",
				CustomerRef:             "/org/global/phone-assembler",
				SupplyLinkCanonicalPath: "/supply-link/global/battery",
				TransportViaRefs:        []string{"/fac/cn/shenzhen/port-yantian"},
				Confidence:              0.91,
			},
		},
		[]SupplyChainAssemblyRouteBinding{
			{ProductRef: "/product/material/lithium", SupplyLinkCanonicalPath: "/supply-link/global/battery", ViaRefs: []string{"/fac/cn/shenzhen/port-yantian"}},
		},
		&SupplyChainAssemblyCoverage{ProductsN: 2, ActorsN: 2, SuppliersN: 1, TransformationsN: 2, DerivedPathsN: 3},
		[]string{"camera_branch_missing"},
		[]string{"/supply-chain/global/path-1"},
		map[string]interface{}{"graphLayer": "assembly_graph"},
	)

	artifact := &MaterializedArtifact{
		TenantID:     "global",
		Kind:         MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: "/supply-chain-assembly/global/test-smartphone",
		Payload:      payload,
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("assembly artifact should validate: %v", err)
	}

	parsed, err := ParseSupplyChainAssemblyGraphPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if len(parsed.AnchorProducts) != 1 || parsed.AnchorProducts[0] != "/product/device/electronics/smartphone" {
		t.Fatalf("anchorProducts=%v", parsed.AnchorProducts)
	}
	if parsed.Coverage == nil || parsed.Coverage.DerivedPathsN != 3 {
		t.Fatalf("coverage=%+v, want DerivedPathsN=3", parsed.Coverage)
	}
	if len(parsed.RouteBindings) != 1 {
		t.Fatalf("routeBindings=%d, want 1", len(parsed.RouteBindings))
	}
}

func TestReplaceCanonicalPathInArtifact_AssemblyGraph(t *testing.T) {
	artifact := &MaterializedArtifact{
		TenantID:     "global",
		Kind:         MaterializedKindSupplyChainAssemblyGraph,
		CanonicalKey: "/supply-chain-assembly/global/test-smartphone",
		Payload: BuildSupplyChainAssemblyGraphPayload(
			[]string{"/product/device/electronics/smartphone"},
			[]SupplyChainAssemblyNode{
				{Class: ah.FragmentClassProduct, Ref: "/product/component/battery-pack"},
				{Class: ah.FragmentClassProduct, Ref: "/product/device/electronics/smartphone"},
			},
			[]SupplyChainAssemblyNode{
				{
					Class: ah.FragmentClassOrg,
					Ref:   "/org/global/battery-maker",
					SiteTransforms: []SiteTransform{
						{Kind: "processing", InputProducts: []string{"/product/component/battery-pack"}, OutputProducts: []string{"/product/device/electronics/smartphone"}},
					},
				},
			},
			[]SupplyChainAssemblySupplyEdge{
				{
					SupplierRef:             "/org/global/battery-maker",
					ProductRef:              "/product/component/battery-pack",
					CustomerRef:             "/org/global/phone-assembler",
					SupplyLinkCanonicalPath: "/supply-link/global/battery",
				},
			},
			nil,
			&SupplyChainAssemblyCoverage{ProductsN: 2},
			nil,
			nil,
			nil,
		),
	}

	changed := ReplaceCanonicalPathInArtifact(artifact, "/product/component/battery-pack", "/product/component/battery-module")
	if !changed {
		t.Fatal("expected replacement to report changed=true")
	}
	parsed, err := ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainAssemblyGraphPayload() error = %v", err)
	}
	if parsed.ProductNodes[0].Ref != "/product/component/battery-module" {
		t.Fatalf("product node ref=%q, want replacement", parsed.ProductNodes[0].Ref)
	}
	if parsed.SupplyEdges[0].ProductRef != "/product/component/battery-module" {
		t.Fatalf("supply edge product=%q, want replacement", parsed.SupplyEdges[0].ProductRef)
	}
	if len(parsed.ActorNodes) != 1 || len(parsed.ActorNodes[0].SiteTransforms) != 1 {
		t.Fatalf("actor siteTransforms=%+v", parsed.ActorNodes)
	}
	if parsed.ActorNodes[0].SiteTransforms[0].InputProducts[0] != "/product/component/battery-module" {
		t.Fatalf("siteTransform input=%q, want replacement", parsed.ActorNodes[0].SiteTransforms[0].InputProducts[0])
	}
}

func TestReplaceCanonicalPathInArtifact_SupplyChainPath(t *testing.T) {
	oldPath := "/fac/us/old-terminal"
	newPath := "/fac/us/new-terminal"
	supplyLink := "/supply-link/tenant-a/org-us-supplier/product-component-x/org-us-customer"
	artifact := &MaterializedArtifact{
		TenantID:         "tenant-a",
		Kind:             MaterializedKindSupplyChainPath,
		CanonicalKey:     "/supply-chain/tenant-a/test",
		NodeRefs:         []string{"/org/us/supplier", oldPath, "/org/us/customer"},
		GeoRefs:          []string{"/loc/us/texas", oldPath},
		FlowRefs:         []string{oldPath},
		ZoneRefs:         []string{oldPath},
		AffectedZoneRefs: []string{oldPath},
		AnchorSupplier:   oldPath,
		AnchorCustomer:   oldPath,
		ProductAxis:      oldPath,
		Labels: map[string]string{
			"anchorSupplier": oldPath,
			"anchorCustomer": oldPath,
			"productAxis":    oldPath,
		},
		Payload: BuildSupplyChainPathPayload(
			[]SupplyChainPathNode{
				{Class: ah.FragmentClassOrg, Ref: "/org/us/supplier"},
				{Class: ah.FragmentClassSite, Ref: oldPath},
				{Class: ah.FragmentClassOrg, Ref: "/org/us/customer"},
			},
			[]SupplyChainPathEdge{
				{
					Relation:                ah.RelationNameSupplies,
					From:                    0,
					To:                      2,
					ViaProductIndex:         intPtr(1),
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/x",
					SupplyLinkCanonicalPath: supplyLink,
					SegmentIndex:            intPtr(0),
					CarriesProduct:          true,
				},
			},
			[]SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/org/us/supplier",
					CustomerRef:             "/org/us/customer",
					ProductRef:              "/product/component/x",
					SupplyLinkCanonicalPath: supplyLink,
					TransportViaRefs:        []string{oldPath},
					GeoRefs:                 []string{"/loc/us/texas"},
					FlowRefs:                []string{oldPath},
					AlternateFlowRefs:       []string{oldPath},
					ZoneRefs:                []string{oldPath},
					AffectedZoneRefs:        []string{oldPath},
				},
			},
			nil,
			nil,
		),
	}

	changed := ReplaceCanonicalPathInArtifact(artifact, oldPath, newPath)
	if !changed {
		t.Fatal("expected replacement to report changed=true")
	}
	if artifact.AnchorSupplier != newPath || artifact.AnchorCustomer != newPath || artifact.ProductAxis != newPath {
		t.Fatalf("top-level anchors not rewritten: supplier=%q customer=%q product=%q", artifact.AnchorSupplier, artifact.AnchorCustomer, artifact.ProductAxis)
	}
	if len(artifact.GeoRefs) != 1 || artifact.GeoRefs[0] != "/loc/us/texas" {
		t.Fatalf("geoRefs=%v, want payload-derived rebuild without stale path", artifact.GeoRefs)
	}
	if len(artifact.FlowRefs) != 1 || artifact.FlowRefs[0] != newPath {
		t.Fatalf("flowRefs=%v, want %q", artifact.FlowRefs, newPath)
	}
	if len(artifact.ZoneRefs) != 1 || artifact.ZoneRefs[0] != newPath {
		t.Fatalf("zoneRefs=%v, want %q", artifact.ZoneRefs, newPath)
	}
	if len(artifact.AffectedZoneRefs) != 1 || artifact.AffectedZoneRefs[0] != newPath {
		t.Fatalf("affectedZoneRefs=%v, want %q", artifact.AffectedZoneRefs, newPath)
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if parsed.Nodes[1].Ref != newPath {
		t.Fatalf("node ref=%q, want replacement", parsed.Nodes[1].Ref)
	}
	if parsed.Edges[0].SupplyLinkCanonicalPath != supplyLink {
		t.Fatalf("edge supplyLinkCanonicalPath=%q, want unchanged supply link path", parsed.Edges[0].SupplyLinkCanonicalPath)
	}
	if parsed.Legs[0].SupplyLinkCanonicalPath != supplyLink {
		t.Fatalf("leg supplyLinkCanonicalPath=%q, want unchanged supply link path", parsed.Legs[0].SupplyLinkCanonicalPath)
	}
	if len(parsed.Legs[0].TransportViaRefs) != 1 || parsed.Legs[0].TransportViaRefs[0] != newPath {
		t.Fatalf("leg transportViaRefs=%v, want %q", parsed.Legs[0].TransportViaRefs, newPath)
	}
	if len(parsed.Legs[0].FlowRefs) != 1 || parsed.Legs[0].FlowRefs[0] != newPath {
		t.Fatalf("leg flowRefs=%v, want %q", parsed.Legs[0].FlowRefs, newPath)
	}
	if len(parsed.Legs[0].AlternateFlowRefs) != 1 || parsed.Legs[0].AlternateFlowRefs[0] != newPath {
		t.Fatalf("leg alternateFlowRefs=%v, want %q", parsed.Legs[0].AlternateFlowRefs, newPath)
	}
	if len(parsed.Legs[0].ZoneRefs) != 1 || parsed.Legs[0].ZoneRefs[0] != newPath {
		t.Fatalf("leg zoneRefs=%v, want %q", parsed.Legs[0].ZoneRefs, newPath)
	}
	if len(parsed.Legs[0].AffectedZoneRefs) != 1 || parsed.Legs[0].AffectedZoneRefs[0] != newPath {
		t.Fatalf("leg affectedZoneRefs=%v, want %q", parsed.Legs[0].AffectedZoneRefs, newPath)
	}
}

func TestParseSupplyChainPathPayload_NormalizesRouteOverlays(t *testing.T) {
	payload := BuildSupplyChainPathPayloadDetailed(
		[]SupplyChainPathNode{
			{Class: ah.FragmentClassOrg, Ref: "/org/us/supplier"},
			{Class: ah.FragmentClassSite, Ref: "/fac/ae/jebel-ali-port"},
			{Class: ah.FragmentClassSite, Ref: "/fac/za/cape-of-good-hope-terminal"},
			{Class: ah.FragmentClassOrg, Ref: "/org/io/customer"},
		},
		nil,
		[]SupplyChainPathLeg{
			{
				Index:                   0,
				SupplierRef:             "/org/us/supplier",
				CustomerRef:             "/org/io/customer",
				ProductRef:              "/product/mining/iron-ore",
				SupplyLinkCanonicalPath: "/supply-link/global/supplier/product/customer",
				RouteVariantKey:         "via-hormuz",
				TransportViaRefs:        []string{"/fac/ae/jebel-ali-port"},
				RouteGeometrySource:     "curated_flow_catalog",
				FlowRefs:                []string{"sea_gulf_diego_garcia_mainline"},
				BindingConfidence:       0.9,
			},
			{
				Index:                   0,
				SupplierRef:             "/org/us/supplier",
				CustomerRef:             "/org/io/customer",
				ProductRef:              "/product/mining/iron-ore",
				SupplyLinkCanonicalPath: "/supply-link/global/supplier/product/customer",
				RouteVariantKey:         "via-cape",
				TransportViaRefs:        []string{"/fac/za/cape-of-good-hope-terminal"},
				RouteGeometrySource:     "composed_route_segments",
				FlowRefs:                []string{"sea_gulf_rotterdam_via_cape"},
				BindingConfidence:       0.72,
			},
		},
		nil,
		nil,
		nil,
		nil,
	)

	parsed, err := ParseSupplyChainPathPayload(payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if len(parsed.Legs) != 1 {
		t.Fatalf("legs=%d, want 1 structural leg after overlay normalization", len(parsed.Legs))
	}
	leg := parsed.Legs[0]
	if len(leg.RouteOverlays) != 2 {
		t.Fatalf("routeOverlays=%d, want 2", len(leg.RouteOverlays))
	}
	if strings.TrimSpace(leg.RouteVariantKey) == "" {
		t.Fatal("primary routeVariantKey must be derived and non-empty")
	}
	if len(leg.TransportViaRefs) != 1 || leg.TransportViaRefs[0] != "/fac/ae/jebel-ali-port" {
		t.Fatalf("primary transportViaRefs=%v, want jebel ali", leg.TransportViaRefs)
	}
	if len(leg.FlowRefs) != 1 || leg.FlowRefs[0] != "sea_gulf_diego_garcia_mainline" {
		t.Fatalf("leg flowRefs=%v, want primary overlay flow refs only", leg.FlowRefs)
	}
	if len(leg.AlternateFlowRefs) != 1 || leg.AlternateFlowRefs[0] != "sea_gulf_rotterdam_via_cape" {
		t.Fatalf("leg alternateFlowRefs=%v, want alternate overlay flow refs", leg.AlternateFlowRefs)
	}
}

func TestSelectSupplyChainRouteOverlays_PrefersViableAlternate(t *testing.T) {
	leg := SupplyChainPathLeg{
		SupplierRef: "/org/us/supplier",
		CustomerRef: "/org/io/customer",
		ProductRef:  "/product/mining/iron-ore",
		RouteOverlays: []SupplyChainRouteOverlay{
			{
				RouteVariantKey:     "via-hormuz",
				TransportViaRefs:    []string{"/fac/sea/strait-of-hormuz-terminal"},
				RouteGeometrySource: "curated_flow_catalog",
				FlowRefs:            []string{"sea_us_gulf_diego_via_hormuz"},
				BindingConfidence:   0.9,
				Status:              "primary",
			},
			{
				RouteVariantKey:     "via-cape",
				TransportViaRefs:    []string{"/fac/za/cape-of-good-hope-terminal"},
				RouteGeometrySource: "composed_route_segments",
				FlowRefs:            []string{"sea_us_gulf_diego_via_cape"},
				BindingConfidence:   0.82,
			},
		},
	}
	selection := SelectSupplyChainRouteOverlays(leg, SupplyChainRouteOverlayConstraints{
		BlockedFlowRefs: []string{"sea_us_gulf_diego_via_hormuz"},
	})
	if selection.Primary == nil {
		t.Fatal("expected viable primary overlay")
	}
	wantVariant := BuildSupplyLinkRouteVariantKey([]string{"/fac/za/cape-of-good-hope-terminal"})
	if got := selection.Primary.RouteVariantKey; got != wantVariant {
		t.Fatalf("primary=%q, want %q", got, wantVariant)
	}
	if len(selection.Viable) != 1 || len(selection.Blocked) != 1 {
		t.Fatalf("viable=%d blocked=%d, want 1/1", len(selection.Viable), len(selection.Blocked))
	}
}

func intPtr(i int) *int {
	return &i
}
