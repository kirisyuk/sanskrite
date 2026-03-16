package extraction

import (
	"testing"
	"time"

	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

func TestEnsureFallbackEventForHighSignalDocument_SynthesizesMaritimeClosureEvent(t *testing.T) {
	now := time.Date(2026, 3, 15, 16, 30, 0, 0, time.UTC)
	out := &ontology.DocumentExtraction{}
	doc := &prov.NormalizedDocument{
		Title:             "Iran signals closure of Bab el-Mandeb alongside Strait of Hormuz",
		FullText:          "Iran signaled that Bab el-Mandeb and the Strait of Hormuz could be closed to shipping as maritime traffic faces renewed disruption.",
		OriginPublishedAt: &now,
	}
	ensureFallbackEventForHighSignalDocument(out, doc)
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 synthesized event, got %d", len(out.Events))
	}
	got := out.Events[0]
	if got.Title != doc.Title {
		t.Fatalf("unexpected fallback title: %q", got.Title)
	}
	if got.EventType != "disruption" {
		t.Fatalf("unexpected fallback event type: %q", got.EventType)
	}
	if got.DateISO != now.UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected fallback date: %q", got.DateISO)
	}
	if got.Summary == nil || *got.Summary == "" {
		t.Fatal("expected fallback summary")
	}
	if len(got.SpherePaths) == 0 {
		t.Fatal("expected fallback sphere paths")
	}
	if got.Location == nil || got.Location.Name == nil || *got.Location.Name != "Bab el-Mandeb" {
		t.Fatalf("expected fallback chokepoint location, got %+v", got.Location)
	}
}

func TestEnsureFallbackEventForHighSignalDocument_SkipsExplainer(t *testing.T) {
	out := &ontology.DocumentExtraction{}
	doc := &prov.NormalizedDocument{
		Title:    "Bab el-Mandeb Strait: What It Is and Why It Matters",
		FullText: "An explainer on the strategic chokepoint and what it is.",
	}
	ensureFallbackEventForHighSignalDocument(out, doc)
	if len(out.Events) != 0 {
		t.Fatalf("expected no synthesized event for explainer, got %d", len(out.Events))
	}
}
