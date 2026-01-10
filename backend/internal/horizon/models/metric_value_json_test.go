package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMetricValue_UnmarshalJSON_ValueAndConfidenceLooseFormats(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	raw := []byte(`{
	  "value": "10M",
	  "unit": "usd",
	  "confidence": "80",
	  "observedAt": "` + now + `",
	  "provenance": { "sourceIds": ["x"], "method": "extracted" }
	}`)

	var mv MetricValue
	if err := json.Unmarshal(raw, &mv); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if mv.Value != 10000000 {
		t.Fatalf("Value=%v, want 10000000", mv.Value)
	}
	if mv.Confidence != 0.8 {
		t.Fatalf("Confidence=%v, want 0.8", mv.Confidence)
	}
	if mv.ObservedAt.IsZero() {
		t.Fatal("ObservedAt should not be zero")
	}
	if mv.Unit == nil || *mv.Unit != "usd" {
		t.Fatalf("Unit=%v, want usd", mv.Unit)
	}
}

func TestMetricValue_UnmarshalJSON_DateOnlyObservedAt(t *testing.T) {
	raw := []byte(`{
	  "value": "45%",
	  "unit": "percent",
	  "confidence": 0.95,
	  "observedAt": "2026-01-10",
	  "provenance": { "sourceIds": ["x"], "method": "extracted" }
	}`)

	var mv MetricValue
	if err := json.Unmarshal(raw, &mv); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if mv.Value != 45.0 {
		t.Fatalf("Value=%v, want 45.0", mv.Value)
	}
	if mv.ObservedAt.UTC().Format("2006-01-02") != "2026-01-10" {
		t.Fatalf("ObservedAt=%v, want 2026-01-10", mv.ObservedAt.UTC())
	}
}

func TestMetricValue_UnmarshalJSON_UsesProvenanceObservedAtFallback(t *testing.T) {
	p := time.Date(2025, 12, 17, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	raw := []byte(`{
	  "value": 1234.5,
	  "confidence": 0.9,
	  "provenance": { "sourceIds": ["x"], "method": "extracted", "observedAt": "` + p + `" }
	}`)

	var mv MetricValue
	if err := json.Unmarshal(raw, &mv); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if mv.ObservedAt.UTC().Format(time.RFC3339) != p {
		t.Fatalf("ObservedAt=%v, want %v", mv.ObservedAt.UTC().Format(time.RFC3339), p)
	}
}


