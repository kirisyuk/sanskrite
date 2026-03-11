package models

import (
	"testing"
	"time"
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
						map[string]interface{}{"type": "org", "ref": "/org/catl"},
						map[string]interface{}{"type": "product", "ref": "/product/component/electronic/manufacturing/battery-cell"},
						map[string]interface{}{"type": "org", "ref": "/org/bmw"},
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
						{Type: "org", Ref: "/org/catl"},
						{Type: "product", Ref: "/product/component/electronic/manufacturing/battery-cell"},
						{Type: "org", Ref: "/org/bmw"},
					},
					[]SupplyChainPathEdge{
						{
							Type:                    "SUPPLY_LINK",
							From:                    0,
							To:                      2,
							ViaProductIndex:         intPtr(1),
							ProductNodeIndex:        intPtr(1),
							ProductRef:              "/product/component/electronic/manufacturing/battery-cell",
							SupplyLinkCanonicalPath: "/supply-link/bmw/org-catl/product-component-electronic-manufacturing-battery-cell/org-bmw",
							SegmentIndex:            intPtr(0),
							CarriesProduct:          true,
							Role:                    "product_flow",
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
		{Type: "org", Ref: "/org/catl"},
		{Type: "product", Ref: "/product/.../battery-cell"},
		{Type: "org", Ref: "/org/bmw"},
	}
	edges := []SupplyChainPathEdge{
		{
			Type:                    "SUPPLY_LINK",
			From:                    0,
			To:                      2,
			ViaProductIndex:         intPtr(1),
			ProductNodeIndex:        intPtr(1),
			ProductRef:              "/product/.../battery-cell",
			SupplyLinkCanonicalPath: "/supply-link/bmw/org-catl/product-battery-cell/org-bmw",
			SegmentIndex:            intPtr(0),
			CarriesProduct:          true,
			Role:                    "product_flow",
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

func TestParseSupplyChainPathPayload(t *testing.T) {
	payload := map[string]interface{}{
		"v": float64(1), // JSON unmarshal uses float64
		"nodes": []interface{}{
			map[string]interface{}{"type": "org", "ref": "/org/catl"},
			map[string]interface{}{"type": "product", "ref": "/product/.../battery-cell"},
			map[string]interface{}{"type": "org", "ref": "/org/bmw"},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"type":                    "SUPPLY_LINK",
				"from":                    float64(0),
				"to":                      float64(2),
				"viaProductIndex":         float64(1),
				"productNodeIndex":        float64(1),
				"productRef":              "/product/.../battery-cell",
				"supplyLinkCanonicalPath": "/supply-link/bmw/org-catl/product-battery-cell/org-bmw",
				"segmentIndex":            float64(0),
				"carriesProduct":          true,
				"role":                    "product_flow",
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
				{Type: "org", Ref: "/org/catl"},
				{Type: "product", Ref: "/product/.../battery-cell"},
				{Type: "org", Ref: "/org/bmw"},
			},
			[]SupplyChainPathEdge{
				{Type: "SUPPLY_LINK", From: 0, To: 2, ViaProductIndex: intPtr(1), ProductNodeIndex: intPtr(1), ProductRef: "/product/.../battery-cell", CarriesProduct: true, Role: "product_flow"},
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
				{Type: "org", Ref: "/org/catl"},
				{Type: "product", Ref: "/product/.../battery-cell"},
				{Type: "org", Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Type: "SUPPLY_LINK", From: 0, To: 2, ViaProductIndex: intPtr(1), ProductNodeIndex: intPtr(1), ProductRef: "/product/.../battery-cell"},
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
				{Type: "org", Ref: "/org/catl"},
			},
			edges: []SupplyChainPathEdge{
				{Type: "SUPPLY_LINK", From: 0, To: 5},
			},
			wantErr: true,
		},
		{
			name: "viaProductIndex points to non-product",
			nodes: []SupplyChainPathNode{
				{Type: "org", Ref: "/org/catl"},
				{Type: "org", Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Type: "SUPPLY_LINK", From: 0, To: 1, ViaProductIndex: intPtr(0)},
			},
			wantErr: true,
		},
		{
			name: "productNodeIndex points to non-product",
			nodes: []SupplyChainPathNode{
				{Type: "org", Ref: "/org/catl"},
				{Type: "org", Ref: "/org/bmw"},
			},
			edges: []SupplyChainPathEdge{
				{Type: "LOGISTICS", From: 0, To: 1, ProductNodeIndex: intPtr(0), ProductRef: "/product/.../battery-cell"},
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
				{Type: "org", Ref: "/org/a"},
				{Type: "product", Ref: "/product/component/test/widget"},
				{Type: "fac", Ref: "/fac/port"},
				{Type: "org", Ref: "/org/c"},
			},
			[]SupplyChainPathEdge{
				{
					Type:                    "SUPPLY_LINK",
					From:                    0,
					To:                      3,
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
				},
				{
					Type:                    "LOGISTICS",
					From:                    0,
					To:                      2,
					ProductNodeIndex:        intPtr(1),
					ProductRef:              "/product/component/test/widget",
					SupplyLinkCanonicalPath: "/supply-link/global/org-a/product-component-test-widget/org-c",
				},
				{
					Type:                    "LOGISTICS",
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
		if edge.Type == "SUPPLY_LINK" && edge.Role != "product_flow" {
			t.Fatalf("edge[%d] missing inferred role: %+v", i, edge)
		}
		if edge.Type == "LOGISTICS" && edge.Role != "logistics_leg" {
			t.Fatalf("edge[%d] missing inferred role: %+v", i, edge)
		}
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("artifact should validate after repair: %v", err)
	}
}

func intPtr(i int) *int {
	return &i
}
