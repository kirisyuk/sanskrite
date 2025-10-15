package discovery

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// ruleMatches and clamp helpers are internal; test ruleMatches basic behavior
func TestRuleMatches_Keywords(t *testing.T) {
	r := &models.Rule{Config: map[string]any{"keywords": []interface{}{"alpha", "beta"}}}
	if !ruleMatches(r, "this contains alpha word") {
		t.Fatal("expected rule to match text with keyword alpha")
	}
	if ruleMatches(r, "no match here") {
		t.Fatal("did not expect match")
	}
}

func TestRuleMatches_Regex(t *testing.T) {
	r := &models.Rule{Config: map[string]any{"regex": "(?i)news\\d+"}}
	if !ruleMatches(r, "breaking NEWS123 today") {
		t.Fatal("expected regex rule to match")
	}
}

// compile-time guard for Start no-op when sub is nil
func TestDocumentProcessorStart_NoSub(t *testing.T) {
	w := &DocumentProcessor{}
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
