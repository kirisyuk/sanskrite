package policy

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
)

func TestDefaultTaxonomyRules_EdgeDirectionRulesComeFromEdgeSemantics(t *testing.T) {
	rules := DefaultTaxonomyRules()
	if rules == nil {
		t.Fatal("expected default taxonomy rules")
	}

	got := rules.EdgeDirection.Rules
	want := buildEdgeDirectionRulesFromSemantics()
	if len(got) != len(want) {
		t.Fatalf("edge-direction len mismatch: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i].EdgeType != want[i].EdgeType {
			t.Fatalf("edge type mismatch at %d: got=%q want=%q", i, got[i].EdgeType, want[i].EdgeType)
		}
		if got[i].Direction != want[i].Direction {
			t.Fatalf("edge direction mismatch for %s: got=%q want=%q", got[i].EdgeType, got[i].Direction, want[i].Direction)
		}
	}

	if len(got) != len(edgesemantics.EdgeDirectionPolicies) {
		t.Fatalf("expected 1:1 coverage with edgesemantics, got=%d want=%d", len(got), len(edgesemantics.EdgeDirectionPolicies))
	}
}
