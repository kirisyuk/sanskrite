package synonymdict

import (
	"context"
	"testing"
)

func TestSeedAliasAndResolve(t *testing.T) {
	r := NewRuntime(nil)
	r.SeedAlias("TSMC", "/org/tw/taiwan-semiconductor-manufacturing-company")
	canon, matched, err := r.Resolve(context.Background(), "tsmc")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if !matched {
		t.Fatalf("Resolve should match TSMC")
	}
	if canon != "/org/tw/taiwan-semiconductor-manufacturing-company" {
		t.Fatalf("unexpected canonical: %s", canon)
	}
}

func TestResolveUnknown(t *testing.T) {
	r := NewRuntime(nil)
	canon, matched, err := r.Resolve(context.Background(), "Unknown Corp")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if matched {
		t.Fatalf("Resolve should NOT match unknown alias")
	}
	if canon != "" {
		t.Fatalf("expected empty canonical, got %q", canon)
	}
}

func TestSeedIgnoresEmpty(t *testing.T) {
	r := NewRuntime(nil)
	r.SeedAlias("", "/org/tw/foo")
	r.SeedAlias("foo", "")
	snap := r.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %v", snap)
	}
}

func TestResolveEmptyInput(t *testing.T) {
	r := NewRuntime(nil)
	canon, matched, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if matched || canon != "" {
		t.Fatalf("expected no match for empty alias")
	}
}
