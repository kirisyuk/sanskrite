package simulation

import (
	"testing"
	"time"
)

type testNestedEvent struct {
	Step    int                `json:"step"`
	Reason  string             `json:"reason,omitempty"`
	Factors map[string]float64 `json:"factors,omitempty"`
}

func TestSanitizeSimulationDocumentMap_JSONSafeNestedPayload(t *testing.T) {
	now := time.Date(2026, 3, 23, 10, 30, 0, 0, time.UTC)
	input := map[string]any{
		"seedFragmentIds":   []string{"frag-1"},
		"previewGeneratedAt": now,
		"propagationPaths": [][]string{
			{"a", "b", "c"},
		},
		"trace": []testNestedEvent{
			{
				Step:   1,
				Reason: "seed",
				Factors: map[string]float64{
					"damping": 0.8,
				},
			},
		},
	}

	got := sanitizeSimulationDocumentMap(input)
	if got == nil {
		t.Fatalf("sanitizeSimulationDocumentMap returned nil")
	}
	if got["previewGeneratedAt"] == nil {
		t.Fatalf("expected previewGeneratedAt to survive sanitization")
	}
	trace, ok := got["trace"].([]any)
	if !ok || len(trace) != 1 {
		t.Fatalf("expected trace to be []any with one item, got %#v", got["trace"])
	}
	event, ok := trace[0].(map[string]any)
	if !ok {
		t.Fatalf("expected trace event to be map[string]any, got %#v", trace[0])
	}
	if event["reason"] != "seed" {
		t.Fatalf("expected trace reason to round-trip, got %#v", event["reason"])
	}
	paths, ok := got["propagationPaths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected propagationPaths to be []any with one item, got %#v", got["propagationPaths"])
	}
	pathObject, ok := paths[0].(map[string]any)
	if !ok {
		t.Fatalf("expected propagation path to be wrapped object, got %#v", paths[0])
	}
	items, ok := pathObject["items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("expected wrapped nested path items, got %#v", pathObject["items"])
	}
}
