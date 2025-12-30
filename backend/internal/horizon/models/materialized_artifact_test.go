package models

import (
	"testing"
	"time"
)

func TestMaterializedArtifact_Validate(t *testing.T) {
	tests := []struct {
		name    string
		artifact *MaterializedArtifact
		wantErr bool
	}{
		{
			name: "valid SUPPLY_CHAIN_PATH",
			artifact: &MaterializedArtifact{
				TenantID:    "bmw",
				Kind:        MaterializedKindSupplyChainPath,
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
			name: "missing tenantId",
			artifact: &MaterializedArtifact{
				Kind:        MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/test",
				Payload:     map[string]interface{}{"v": 1, "nodes": []interface{}{}},
			},
			wantErr: true,
		},
		{
			name: "invalid kind",
			artifact: &MaterializedArtifact{
				TenantID:    "bmw",
				Kind:        MaterializedKind("INVALID"),
				CanonicalKey: "/supply-chain/test",
				Payload:     map[string]interface{}{"v": 1, "nodes": []interface{}{}},
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
				TenantID:    "bmw",
				Kind:        MaterializedKindSupplyChainPath,
				CanonicalKey: "/supply-chain/test",
				Payload:     map[string]interface{}{"v": float64(2), "nodes": []interface{}{}},
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
		{Type: "SUPPLY_LINK", From: 0, To: 2, ViaProductIndex: intPtr(1)},
	}
	notes := map[string]interface{}{
		"buildMode": "tenant_first_global_fallback",
		"maxDepth":  6,
	}

	payload := BuildSupplyChainPathPayload(nodes, edges, notes)

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
				"type":            "SUPPLY_LINK",
				"from":            float64(0),
				"to":              float64(2),
				"viaProductIndex": float64(1),
			},
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
	if parsed.Nodes[0].Ref != "/org/catl" {
		t.Errorf("expected first node ref /org/catl, got %s", parsed.Nodes[0].Ref)
	}
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
				{Type: "SUPPLY_LINK", From: 0, To: 2, ViaProductIndex: intPtr(1)},
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

func intPtr(i int) *int {
	return &i
}

