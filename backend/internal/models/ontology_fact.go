package models

import (
	"fmt"
	"strings"
	"time"
)

// OntologyFact is a unified, normalized fact/event frame produced by the ontology pipeline.
// It is designed to be:
// - Incrementally processed by Watchdog (watermark on UpdatedAt / ObservedAt)
// - Indexed by Vertex AI Search via VectorText + structData
// - Filterable by canonicalPaths / actorPaths / locationPath / time
type OntologyFact struct {
	BaseOntologyFields

	// Kind of fact frame: "event" | "claim" | "metric" | "relation" | "other"
	Kind string `json:"kind" firestore:"kind"`

	// Type is a controlled vocabulary identifier (e.g. event type, predicate, metric name).
	Type string `json:"type" firestore:"type"`

	// Title/summary are human-readable, while VectorText is for semantic indexing.
	Title   string `json:"title" firestore:"title"`
	Summary string `json:"summary,omitempty" firestore:"summary,omitempty"`

	// Confidence is a normalized confidence score [0..1] assigned by the extraction/normalization pipeline.
	Confidence float64 `json:"confidence" firestore:"confidence"`

	// Time anchoring
	OccurredAt *time.Time `json:"occurredAt,omitempty" firestore:"occurredAt,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`

	// References back to canonical ontology artifacts (optional).
	EventID *string `json:"eventId,omitempty" firestore:"eventId,omitempty"`
	ClaimID *string `json:"claimId,omitempty" firestore:"claimId,omitempty"`

	// Minimal provenance to reconstruct sources for Scout.
	SourceIDs  []string    `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`   // DataSource IDs
	SourceRefs []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"` // document/chat IDs etc.

	// MetricUpdates is an optional, structured patch that can be applied to HorizonFragment.State without LLM.
	// This is only used for safe, typed metric changes (e.g., numeric KPI updates) and is ignored otherwise.
	MetricUpdates []FactMetricUpdate `json:"metricUpdates,omitempty" firestore:"metricUpdates,omitempty"`
}

// FactMetricUpdate represents a safe, typed metric update derived from a fact.
type FactMetricUpdate struct {
	MetricName string     `json:"metricName" firestore:"metricName"`
	Value      float64    `json:"value" firestore:"value"`
	Unit       *string    `json:"unit,omitempty" firestore:"unit,omitempty"`
	Confidence float64    `json:"confidence" firestore:"confidence"`
	ObservedAt *time.Time `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
}

// GenerateVectorText builds a stable semantic text representation for indexing.
// It is purely deterministic and should be used consistently across pipeline stages.
func (f *OntologyFact) GenerateVectorText() string {
	if f == nil {
		return ""
	}
	parts := []string{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}

	add(fmt.Sprintf("kind: %s", f.Kind))
	add(fmt.Sprintf("type: %s", f.Type))
	add(f.Title)
	add(f.Summary)
	if f.Confidence > 0 {
		add(fmt.Sprintf("confidence: %.3f", f.Confidence))
	}

	if len(f.CanonicalPaths) > 0 {
		add("canonicalPaths: " + strings.Join(f.CanonicalPaths, ", "))
	}
	if f.LocationPath != nil && strings.TrimSpace(*f.LocationPath) != "" {
		add("locationPath: " + strings.TrimSpace(*f.LocationPath))
	}
	if len(f.ActorPaths) > 0 {
		add("actorPaths: " + strings.Join(f.ActorPaths, ", "))
	}
	if len(f.SpherePaths) > 0 {
		add("spherePaths: " + strings.Join(f.SpherePaths, ", "))
	}
	if f.OccurredAt != nil {
		add("occurredAt: " + f.OccurredAt.UTC().Format(time.RFC3339))
	}
	if f.ObservedAt != nil {
		add("observedAt: " + f.ObservedAt.UTC().Format(time.RFC3339))
	}
	if len(f.SourceIDs) > 0 {
		add("sourceIds: " + strings.Join(f.SourceIDs, ", "))
	}
	if len(f.MetricUpdates) > 0 {
		for _, mu := range f.MetricUpdates {
			if strings.TrimSpace(mu.MetricName) == "" {
				continue
			}
			add(fmt.Sprintf("metric: %s=%.6f", strings.TrimSpace(mu.MetricName), mu.Value))
			if mu.Unit != nil && strings.TrimSpace(*mu.Unit) != "" {
				add(fmt.Sprintf("metricUnit: %s", strings.TrimSpace(*mu.Unit)))
			}
			if mu.Confidence > 0 {
				add(fmt.Sprintf("metricConfidence: %.3f", mu.Confidence))
			}
		}
	}

	return strings.Join(parts, "\n")
}
