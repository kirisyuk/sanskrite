package entity_semantics

import (
	"strings"
	"testing"
)

func TestPromptRelationTargetTypesPipeList(t *testing.T) {
	got := PromptRelationTargetTypesPipeList()
	if got == "" {
		t.Fatalf("PromptRelationTargetTypesPipeList() should not be empty")
	}
	for _, want := range []string{"\"ORGANIZATION\"", "\"FACILITY\"", "\"MARKET\"", "\"MECHANISM\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("PromptRelationTargetTypesPipeList() missing %s", want)
		}
	}
	if strings.Contains(got, "\"\"") {
		t.Fatalf("PromptRelationTargetTypesPipeList() must not contain empty type tokens")
	}
}
