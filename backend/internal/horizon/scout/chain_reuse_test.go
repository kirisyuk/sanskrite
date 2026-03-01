package scout

import (
	"context"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
)

type mockChainReuseBuilder struct {
	chains map[string]*models.MaterializedArtifact
}

func (m *mockChainReuseBuilder) GetChainByCanonicalKey(_ context.Context, tenantID, canonicalKey string) (*models.MaterializedArtifact, error) {
	if m.chains == nil {
		return nil, nil
	}
	art, ok := m.chains[tenantID+"|"+canonicalKey]
	if !ok {
		return nil, nil
	}
	return art, nil
}

func TestEvaluateChainReuse_NoBuilder(t *testing.T) {
	segments := []pathbuilder.PathSegment{{SupplierRef: "/org/a", ProductRef: "/product/b", CustomerRef: "/org/c"}}
	d := evaluateChainReuse(context.Background(), nil, "global", segments)
	if d.Level != ChainReuseNone {
		t.Fatalf("expected ChainReuseNone, got %s", d.Level)
	}
}

func TestEvaluateChainReuse_EmptySegments(t *testing.T) {
	builder := &mockChainReuseBuilder{}
	d := evaluateChainReuse(context.Background(), builder, "global", nil)
	if d.Level != ChainReuseNone {
		t.Fatalf("expected ChainReuseNone, got %s", d.Level)
	}
}

func TestEvaluateChainReuse_ArtifactNotFound(t *testing.T) {
	builder := &mockChainReuseBuilder{chains: map[string]*models.MaterializedArtifact{}}
	segments := []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	}
	d := evaluateChainReuse(context.Background(), builder, "global", segments)
	if d.Level != ChainReuseNone {
		t.Fatalf("expected ChainReuseNone, got %s", d.Level)
	}
	if d.CanonicalKey == "" {
		t.Fatal("expected non-empty canonical key")
	}
}

func TestEvaluateChainReuse_FullSkip(t *testing.T) {
	key := pathbuilder.BuildStructuralCanonicalKey("global", []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	})
	builder := &mockChainReuseBuilder{chains: map[string]*models.MaterializedArtifact{
		"global|" + key: {
			ID:         "art-123",
			PathStatus: "validated",
			UpdatedAt:  time.Now().Add(-1 * time.Hour),
			Payload: map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"type": "org", "ref": "/org/catl"},
					map[string]interface{}{"type": "product", "ref": "/product/lithium-battery-cell"},
					map[string]interface{}{"type": "org", "ref": "/org/bmw-ag"},
				},
			},
			NodeRefs: []string{"/org/catl", "/product/lithium-battery-cell", "/org/bmw-ag"},
		},
	}}
	segments := []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	}
	d := evaluateChainReuse(context.Background(), builder, "global", segments)
	if d.Level != ChainReuseFullSkip {
		t.Fatalf("expected ChainReuseFullSkip, got %s (reason: %s)", d.Level, d.Reason)
	}
	if d.Artifact == nil {
		t.Fatal("expected non-nil artifact on full skip")
	}
}

func TestEvaluateChainReuse_EnrichMerge_Hypothesis(t *testing.T) {
	key := pathbuilder.BuildStructuralCanonicalKey("global", []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	})
	builder := &mockChainReuseBuilder{chains: map[string]*models.MaterializedArtifact{
		"global|" + key: {
			ID:         "art-456",
			PathStatus: "hypothesis",
			UpdatedAt:  time.Now().Add(-1 * time.Hour),
			Payload: map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"type": "org", "ref": "/org/catl"},
					map[string]interface{}{"type": "product", "ref": "/product/lithium-battery-cell"},
					map[string]interface{}{"type": "org", "ref": "/org/bmw-ag"},
				},
			},
			NodeRefs: []string{"/org/catl", "/product/lithium-battery-cell", "/org/bmw-ag"},
		},
	}}
	segments := []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	}
	d := evaluateChainReuse(context.Background(), builder, "global", segments)
	if d.Level != ChainReuseEnrichMerge {
		t.Fatalf("expected ChainReuseEnrichMerge, got %s (reason: %s)", d.Level, d.Reason)
	}
}

func TestEvaluateChainReuse_EnrichMerge_Stale(t *testing.T) {
	key := pathbuilder.BuildStructuralCanonicalKey("global", []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	})
	builder := &mockChainReuseBuilder{chains: map[string]*models.MaterializedArtifact{
		"global|" + key: {
			ID:         "art-789",
			PathStatus: "validated",
			UpdatedAt:  time.Now().Add(-30 * 24 * time.Hour),
			Payload: map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"type": "org", "ref": "/org/catl"},
					map[string]interface{}{"type": "product", "ref": "/product/lithium-battery-cell"},
					map[string]interface{}{"type": "org", "ref": "/org/bmw-ag"},
				},
			},
			NodeRefs: []string{"/org/catl", "/product/lithium-battery-cell", "/org/bmw-ag"},
		},
	}}
	segments := []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	}
	d := evaluateChainReuse(context.Background(), builder, "global", segments)
	if d.Level != ChainReuseEnrichMerge {
		t.Fatalf("expected ChainReuseEnrichMerge, got %s (reason: %s)", d.Level, d.Reason)
	}
}

func TestEvaluateChainReuse_EnrichMerge_SparseNodes(t *testing.T) {
	key := pathbuilder.BuildStructuralCanonicalKey("global", []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	})
	builder := &mockChainReuseBuilder{chains: map[string]*models.MaterializedArtifact{
		"global|" + key: {
			ID:         "art-sparse",
			PathStatus: "validated",
			UpdatedAt:  time.Now().Add(-1 * time.Hour),
			Payload: map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"type": "org", "ref": "/org/catl"},
				},
			},
			NodeRefs: []string{"/org/catl"},
		},
	}}
	segments := []pathbuilder.PathSegment{
		{SupplierRef: "/org/catl", ProductRef: "/product/lithium-battery-cell", CustomerRef: "/org/bmw-ag"},
	}
	d := evaluateChainReuse(context.Background(), builder, "global", segments)
	if d.Level != ChainReuseEnrichMerge {
		t.Fatalf("expected ChainReuseEnrichMerge, got %s (reason: %s)", d.Level, d.Reason)
	}
}

func TestFilterScopeWorthyRefs(t *testing.T) {
	refs := []string{
		"/org/catl",
		"/product/lithium-battery-cell",
		"/fac/de/bmw-munich",
		"/loc/de",
		"/deposit/au/greenbushes",
		"/gov/cn",
		"",
	}
	result := filterScopeWorthyRefs(refs)
	expected := map[string]bool{
		"/org/catl":                true,
		"/fac/de/bmw-munich":       true,
		"/deposit/au/greenbushes":  true,
		"/gov/cn":                  true,
	}
	if len(result) != len(expected) {
		t.Fatalf("expected %d scope-worthy refs, got %d: %v", len(expected), len(result), result)
	}
	for _, r := range result {
		if !expected[r] {
			t.Errorf("unexpected scope-worthy ref: %s", r)
		}
	}
}

func TestCountPayloadNodes(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		expected int
	}{
		{"nil payload", nil, 0},
		{"no nodes key", map[string]interface{}{"edges": []interface{}{}}, 0},
		{"3 nodes", map[string]interface{}{"nodes": []interface{}{"a", "b", "c"}}, 3},
		{"empty nodes", map[string]interface{}{"nodes": []interface{}{}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countPayloadNodes(tt.payload)
			if got != tt.expected {
				t.Errorf("countPayloadNodes() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestExtractPayloadConfidence(t *testing.T) {
	tests := []struct {
		name     string
		artifact *models.MaterializedArtifact
		want     float64
	}{
		{"nil artifact", nil, 0},
		{"validated no confidence", &models.MaterializedArtifact{PathStatus: "validated"}, 0.8},
		{"hypothesis no confidence", &models.MaterializedArtifact{PathStatus: "hypothesis"}, 0.5},
		{"explicit confidence in notes", &models.MaterializedArtifact{
			Payload: map[string]interface{}{
				"notes": map[string]interface{}{"confidence": 0.95},
			},
		}, 0.95},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPayloadConfidence(tt.artifact)
			if got != tt.want {
				t.Errorf("extractPayloadConfidence() = %f, want %f", got, tt.want)
			}
		})
	}
}
