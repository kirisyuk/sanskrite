package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestRelationTypesPromptForDomainMode_UsesContractSemantics(t *testing.T) {
	got := strings.Split(relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), ", ")
	expected := contractsemantics.AllowedEdgeStringsForDomainMode(taxonomy.DomainModePolicyImpact)
	if len(got) != len(expected) {
		t.Fatalf("len mismatch: got=%v want=%v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("order/value mismatch at %d: got=%v want=%v", i, got, expected)
		}
	}
}
