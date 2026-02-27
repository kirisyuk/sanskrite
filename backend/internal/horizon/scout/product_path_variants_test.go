package scout

import (
	"context"
	"testing"
)

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestProductCanonicalPathVariants_MalformedIncludesFlat(t *testing.T) {
	ctx := context.Background()
	in := "/product/x/y/avatr-12"
	variants := productCanonicalPathVariants(ctx, in)
	if !containsString(variants, in) {
		t.Fatalf("variants must include original path %q, got %v", in, variants)
	}
	if !containsString(variants, "/product/avatr-12") {
		t.Fatalf("variants must include flat fallback /product/avatr-12, got %v", variants)
	}
}

func TestNormalizeProductPathForResolve_MalformedToStrictHierarchical(t *testing.T) {
	ctx := context.Background()
	in := "/product/x/y/avatr-12"
	got := normalizeProductPathForResolve(ctx, in)
	if got != "/product/finished/mechanical/assembly/avatr-12" {
		t.Fatalf("normalizeProductPathForResolve(%q) = %q, want %q", in, got, "/product/finished/mechanical/assembly/avatr-12")
	}
}

func TestNormalizeProductPathForResolve_HierarchicalStays(t *testing.T) {
	ctx := context.Background()
	in := "/product/finished/mechanical/assembly/avatr-12"
	got := normalizeProductPathForResolve(ctx, in)
	if got != "" {
		t.Fatalf("normalizeProductPathForResolve(%q) = %q, want empty (already stable)", in, got)
	}
}

func TestNormalizeProductPathForResolve_PowerMosfetToComponent(t *testing.T) {
	ctx := context.Background()
	in := "/product/finished/mechanical/assembly/power-mosfet"
	got := normalizeProductPathForResolve(ctx, in)
	if got != "/product/component/electronic/manufacturing/power-mosfet" {
		t.Fatalf("normalizeProductPathForResolve(%q) = %q, want %q", in, got, "/product/component/electronic/manufacturing/power-mosfet")
	}
}
