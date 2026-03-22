package main

import "testing"

func TestLoadDecisionSetFile(t *testing.T) {
	set, err := loadDecisionSet("decisions_global.json")
	if err != nil {
		t.Fatalf("loadDecisionSet() error = %v", err)
	}
	if err := validateDecisionSet(set); err != nil {
		t.Fatalf("validateDecisionSet() error = %v", err)
	}
	if len(set.Accepted) == 0 {
		t.Fatalf("accepted decisions should not be empty")
	}
	if len(set.Rejected) == 0 {
		t.Fatalf("rejected decisions should not be empty")
	}
}

func TestValidateDecisionSetRejectsCanonicalInSynonyms(t *testing.T) {
	set := curatedDecisionSet{
		SchemaVersion: "v1",
		Accepted: []curatedAcceptedDecision{
			{
				ClusterID:   "cluster-a",
				CanonicalID: "frag-a",
				SynonymIDs:  []string{"frag-a", "frag-b"},
			},
		},
	}
	if err := validateDecisionSet(set); err == nil {
		t.Fatalf("validateDecisionSet() error = nil, want rejection")
	}
}

func TestValidateDecisionSetRejectsDuplicateClusterIDs(t *testing.T) {
	set := curatedDecisionSet{
		SchemaVersion: "v1",
		Accepted: []curatedAcceptedDecision{
			{
				ClusterID:   "cluster-a",
				CanonicalID: "frag-a",
				SynonymIDs:  []string{"frag-b"},
			},
			{
				ClusterID:   "cluster-a",
				CanonicalID: "frag-c",
				SynonymIDs:  []string{"frag-d"},
			},
		},
	}
	if err := validateDecisionSet(set); err == nil {
		t.Fatalf("validateDecisionSet() error = nil, want rejection")
	}
}
