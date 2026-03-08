package scout

import (
	"context"
	"testing"
)

func TestSupplyChainOrgResolver_Resolve_EmptyInput(t *testing.T) {
	resolver := NewSupplyChainOrgResolver(nil, nil)
	ctx := context.Background()
	result := resolver.Resolve(ctx, "global", nil)
	if result == nil || len(result.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg for nil pathToID, got %v", result)
	}

	result2 := resolver.Resolve(ctx, "global", map[string]string{})
	if result2 == nil || len(result2.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg for empty pathToID, got %v", result2)
	}
}

func TestSupplyChainOrgResolver_Resolve_NoFacPaths(t *testing.T) {
	resolver := NewSupplyChainOrgResolver(nil, nil)
	ctx := context.Background()
	pathToID := map[string]string{
		"/org/us/tsmc":   "frag-1",
		"/product/x/y/z": "frag-2",
	}
	result := resolver.Resolve(ctx, "global", pathToID)
	if result == nil || len(result.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg when no /fac/ paths, got %v", result.FacToOrg)
	}
}

func TestSupplyChainOrgResolver_Resolve_SkipsUnresolvedFacilityPlaceholder(t *testing.T) {
	resolver := NewSupplyChainOrgResolver(nil, nil)
	ctx := context.Background()
	// No fallback is synthesized anymore: unresolved facilities stay unmapped.
	pathToID := map[string]string{
		"/fac/na/unresolved-plant": "frag-1",
	}
	result := resolver.Resolve(ctx, "global", pathToID)
	if result == nil || len(result.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg when resolver cannot traverse ownership, got %v", result.FacToOrg)
	}
}
