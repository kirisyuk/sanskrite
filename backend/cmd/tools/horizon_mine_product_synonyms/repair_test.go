package main

import (
	"context"
	"testing"
)

func TestBuildPathRewritesFromPairs_CollapsesChainsToRootPath(t *testing.T) {
	rewrites, err := buildPathRewritesFromPairs(context.Background(), nil, []synonymPairDoc{
		{
			CanonicalFragmentID: "canon-a",
			CanonicalPath:       "/product/a",
			SynonymFragmentID:   "syn-b",
			SynonymPath:         "/product/b",
		},
		{
			CanonicalFragmentID: "canon-root",
			CanonicalPath:       "/product/root",
			SynonymFragmentID:   "canon-a",
			SynonymPath:         "/product/a",
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildPathRewritesFromPairs() error = %v", err)
	}
	if len(rewrites) != 2 {
		t.Fatalf("rewrite count = %d, want 2", len(rewrites))
	}
	if got := rewrites["/product/b"]; got != "/product/root" {
		t.Fatalf("rewrite[/product/b] = %q, want /product/root", got)
	}
	if got := rewrites["/product/a"]; got != "/product/root" {
		t.Fatalf("rewrite[/product/a] = %q, want /product/root", got)
	}
}

func TestBuildPathRewritesFromPairs_ChoosesCycleRootDeterministically(t *testing.T) {
	rewrites, err := buildPathRewritesFromPairs(context.Background(), nil, []synonymPairDoc{
		{
			CanonicalFragmentID: "canon-a",
			CanonicalPath:       "/product/component/chemical/a",
			SynonymFragmentID:   "canon-b",
			SynonymPath:         "/product/finished/mechanical/assembly/b",
		},
		{
			CanonicalFragmentID: "canon-b",
			CanonicalPath:       "/product/finished/mechanical/assembly/b",
			SynonymFragmentID:   "canon-a",
			SynonymPath:         "/product/component/chemical/a",
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildPathRewritesFromPairs() error = %v", err)
	}
	if len(rewrites) != 1 {
		t.Fatalf("rewrite count = %d, want 1", len(rewrites))
	}
	if got := rewrites["/product/finished/mechanical/assembly/b"]; got != "/product/component/chemical/a" {
		t.Fatalf("rewrite[/product/finished/mechanical/assembly/b] = %q, want /product/component/chemical/a", got)
	}
}
