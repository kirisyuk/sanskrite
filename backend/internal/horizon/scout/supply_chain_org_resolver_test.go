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
		"/org/us/tsmc":    "frag-1",
		"/product/x/y/z":  "frag-2",
	}
	result := resolver.Resolve(ctx, "global", pathToID)
	if result == nil || len(result.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg when no /fac/ paths, got %v", result.FacToOrg)
	}
}

func TestSupplyChainOrgResolver_Resolve_SkipsGeneralProduction(t *testing.T) {
	resolver := NewSupplyChainOrgResolver(nil, nil)
	ctx := context.Background()
	// /fac/{geo}/general-production and /fac/{geo}/{org}-general-production are skipped (already org-level)
	pathToID := map[string]string{
		"/fac/na/general-production": "frag-1",
	}
	result := resolver.Resolve(ctx, "global", pathToID)
	if result == nil || len(result.FacToOrg) != 0 {
		t.Errorf("expected empty FacToOrg for general-production (skipped), got %v", result.FacToOrg)
	}
}

func TestSupplyChainOrgResolver_fallbackFromFacPath(t *testing.T) {
	resolver := NewSupplyChainOrgResolver(nil, nil)
	tests := []struct {
		facPath      string
		want         string
	}{
		{"/fac/tw/fab-21", "/fac/tw/general-production"},
		{"/fac/us/silver-peak", "/fac/us/general-production"},
		{"/fac/tw/tsmc-general-production", "/fac/tw/tsmc-general-production"}, // already fallback, returns as-is
		{"", ""},
		{"/org/us/tsmc", ""},
		{"/fac/na/unknown", "/fac/na/general-production"},
	}
	for _, tt := range tests {
		t.Run(tt.facPath, func(t *testing.T) {
			got := resolver.fallbackFromFacPath(tt.facPath)
			if got != tt.want {
				t.Errorf("fallbackFromFacPath(%q) = %q, want %q", tt.facPath, got, tt.want)
			}
		})
	}
}

