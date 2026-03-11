package nlp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPEventGateAssess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/event-gate/assess" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ArticleAssessment{
			IsEvent:          true,
			EventClass:       "real_event",
			EventConfidence:  0.92,
			ShouldCreateZone: true,
		})
	}))
	defer server.Close()

	client := NewHTTPEventGate(server.URL, 0)
	out, err := client.Assess(DocumentInput{Title: "Missile strike hits port"})
	if err != nil {
		t.Fatalf("Assess error: %v", err)
	}
	if out == nil || !out.IsEvent || out.EventClass != "real_event" {
		t.Fatalf("unexpected assessment: %#v", out)
	}
}

func TestHTTPEntityExtractorExtract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/entity-extract/extract" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(EntityExtractionResult{
			Entities: []ExtractedEntity{{Surface: "QatarEnergy", EntityType: "company", Role: "actor", Confidence: 0.88}},
		})
	}))
	defer server.Close()

	client := NewHTTPEntityExtractor(server.URL, 0)
	out, err := client.Extract(DocumentInput{Title: "QatarEnergy expands LNG output"})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if out == nil || len(out.Entities) != 1 || out.Entities[0].EntityType != "company" {
		t.Fatalf("unexpected extraction: %#v", out)
	}
}
