package scout

import (
	"context"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestResolveCanonicalPathsBatch_UsesCache(t *testing.T) {
	ctx := context.Background()
	cc := newCanonCache()
	ctx = withCanonCache(ctx, cc)

	cc.set(canonCacheKey("BOTAS", "ORGANIZATION"), "/org/tr/botas")
	cc.set(canonCacheKey("Gazprom", "ORGANIZATION"), "/org/ru/gazprom")

	relations := []agents.DiscoveredRelation{
		{TargetName: "BOTAS", TargetType: "ORGANIZATION"},
		{TargetName: "Gazprom", TargetType: "ORGANIZATION"},
		{TargetName: "Unknown Corp", TargetType: "ORGANIZATION"},
	}

	e := &ScoutEngine{}
	results := e.resolveCanonicalPathsBatch(ctx, relations)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Resolved || results[0].Result != "/org/tr/botas" {
		t.Errorf("entry 0: resolved=%v result=%q, want resolved=true result=/org/tr/botas", results[0].Resolved, results[0].Result)
	}
	if !results[1].Resolved || results[1].Result != "/org/ru/gazprom" {
		t.Errorf("entry 1: resolved=%v result=%q, want resolved=true result=/org/ru/gazprom", results[1].Resolved, results[1].Result)
	}
	if results[2].Resolved {
		t.Errorf("entry 2: should not be resolved without LLM/Firestore")
	}
}

func TestResolveCanonicalPathsBatch_InlineValidation(t *testing.T) {
	ctx := context.Background()
	ctx = withCanonCache(ctx, newCanonCache())

	relations := []agents.DiscoveredRelation{
		{TargetName: "BOTAS", TargetType: "ORGANIZATION", TargetCanonicalPath: "/org/tr/botas"},
		{TargetName: "Bad Path", TargetType: "ORGANIZATION", TargetCanonicalPath: "/discovered/tmp/bad"},
	}

	e := &ScoutEngine{}
	results := e.resolveCanonicalPathsBatch(ctx, relations)

	if !results[0].Resolved || results[0].Result != "/org/tr/botas" {
		t.Errorf("entry 0 with valid inline path should resolve: resolved=%v result=%q", results[0].Resolved, results[0].Result)
	}
	if results[1].Resolved {
		t.Errorf("entry 1 with /discovered/ path should not resolve via inline validation")
	}
}

func TestResolveCanonicalPathsBatch_SessionGraphHit(t *testing.T) {
	ctx := context.Background()
	sg := NewSessionGraph()
	ctx = WithSessionGraph(ctx, sg)
	ctx = withCanonCache(ctx, newCanonCache())

	sg.Add(&models.HorizonFragment{
		ID:            "frag-123",
		CanonicalPath: "/org/tr/botas",
		Class:          ah.FragmentClassOrg,
		Labels:        map[string]string{"en": "BOTAS"},
	})

	relations := []agents.DiscoveredRelation{
		{TargetName: "BOTAS", TargetType: "ORGANIZATION", TargetShortName: "BOTAS"},
	}

	e := &ScoutEngine{}
	results := e.resolveCanonicalPathsBatch(ctx, relations)

	if !results[0].Resolved {
		t.Errorf("entry 0 should be resolved from SessionGraph")
	}
	if results[0].Result != "/org/tr/botas" {
		t.Errorf("entry 0 result = %q, want /org/tr/botas", results[0].Result)
	}
}

func TestBuildBatchCanonicalizationPrompt(t *testing.T) {
	entries := []canonBatchEntry{
		{Index: 0, Relation: agents.DiscoveredRelation{TargetName: "BOTAS", TargetType: "ORGANIZATION"}},
		{Index: 1, Relation: agents.DiscoveredRelation{TargetName: "Gazprom", TargetType: "ORGANIZATION"}},
		{Index: 2, Relation: agents.DiscoveredRelation{TargetName: "TurkStream Pipeline", TargetType: "FACILITY"}},
	}
	indices := []int{0, 1, 2}

	prompt := buildBatchCanonicalizationPrompt(entries, indices)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !containsAny(prompt, "BOTAS", "Gazprom", "TurkStream") {
		t.Error("prompt should contain all entity names")
	}
	if !containsAny(prompt, "index") {
		t.Error("prompt should reference index field")
	}
}
