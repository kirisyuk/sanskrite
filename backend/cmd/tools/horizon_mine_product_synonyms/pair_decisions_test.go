package main

import (
	"context"
	"testing"
)

func TestBuildDecisionSetFromPairDocs_CollapsesCanonicalChains(t *testing.T) {
	set, stats, err := buildDecisionSetFromPairDocs(context.Background(), nil, []synonymPairDoc{
		{
			CanonicalFragmentID: "canon-a",
			CanonicalPath:       "/product/a",
			CanonicalName:       "A",
			SynonymFragmentID:   "syn-b",
			SynonymPath:         "/product/b",
			SynonymName:         "B",
		},
		{
			CanonicalFragmentID: "canon-root",
			CanonicalPath:       "/product/root",
			CanonicalName:       "Root",
			SynonymFragmentID:   "canon-a",
			SynonymPath:         "/product/a",
			SynonymName:         "A",
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildDecisionSetFromPairDocs() error = %v", err)
	}
	if stats.DecisionsBuilt != 1 {
		t.Fatalf("decisionsBuilt = %d, want 1", stats.DecisionsBuilt)
	}
	if len(set.Accepted) != 1 {
		t.Fatalf("accepted decisions = %d, want 1", len(set.Accepted))
	}
	got := set.Accepted[0]
	if got.CanonicalID != "canon-root" {
		t.Fatalf("canonicalID = %q, want canon-root", got.CanonicalID)
	}
	if len(got.SynonymIDs) != 2 {
		t.Fatalf("synonym count = %d, want 2", len(got.SynonymIDs))
	}
	if got.SynonymIDs[0] != "canon-a" || got.SynonymIDs[1] != "syn-b" {
		t.Fatalf("synonymIDs = %#v, want [canon-a syn-b]", got.SynonymIDs)
	}
}

func TestBuildDecisionSetFromPairDocs_RejectsConflictingSynonymOwners(t *testing.T) {
	_, _, err := buildDecisionSetFromPairDocs(context.Background(), nil, []synonymPairDoc{
		{CanonicalFragmentID: "canon-a", SynonymFragmentID: "syn-x"},
		{CanonicalFragmentID: "canon-b", SynonymFragmentID: "syn-x"},
	}, nil)
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}
}

func TestBuildDecisionSetFromPairDocs_CollapsesCycleToSingleRoot(t *testing.T) {
	set, stats, err := buildDecisionSetFromPairDocs(context.Background(), nil, []synonymPairDoc{
		{
			CanonicalFragmentID: "canon-a",
			CanonicalPath:       "/product/component/chemical/a",
			CanonicalName:       "A",
			SynonymFragmentID:   "canon-b",
			SynonymPath:         "/product/finished/mechanical/assembly/b",
			SynonymName:         "B",
		},
		{
			CanonicalFragmentID: "canon-b",
			CanonicalPath:       "/product/finished/mechanical/assembly/b",
			CanonicalName:       "B",
			SynonymFragmentID:   "canon-a",
			SynonymPath:         "/product/component/chemical/a",
			SynonymName:         "A",
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildDecisionSetFromPairDocs() error = %v", err)
	}
	if stats.CycleComponents != 1 {
		t.Fatalf("cycleComponents = %d, want 1", stats.CycleComponents)
	}
	if len(set.Accepted) != 1 {
		t.Fatalf("accepted decisions = %d, want 1", len(set.Accepted))
	}
	if set.Accepted[0].CanonicalID != "canon-a" {
		t.Fatalf("canonicalID = %q, want canon-a", set.Accepted[0].CanonicalID)
	}
	if len(set.Accepted[0].SynonymIDs) != 1 || set.Accepted[0].SynonymIDs[0] != "canon-b" {
		t.Fatalf("synonymIDs = %#v, want [canon-b]", set.Accepted[0].SynonymIDs)
	}
}
