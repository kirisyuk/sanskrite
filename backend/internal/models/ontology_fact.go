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

	// EventPlace is the place name from LLM (e.g. "Dubai airport"); used for geocoding fallback.
	EventPlace *string `json:"eventPlace,omitempty" firestore:"eventPlace,omitempty"`
	// EventAddress is the full address from LLM for geocoding (e.g. "123 Main St, Houston, TX"); Google Geo resolves to Coordinates.
	EventAddress *string `json:"eventAddress,omitempty" firestore:"eventAddress,omitempty"`
	// EventISO6709 is coordinates from LLM in ISO6709 format (e.g. "+25.27+55.30/"); parsed to Coordinates when valid.
	EventISO6709 *string `json:"eventIso6709,omitempty" firestore:"eventIso6709,omitempty"`

	// CanonicalNames are human-readable names of the canonical paths (e.g. "San Francisco", "Emirates").
	// Used for search/lookup when resolving facts to visible fragments.
	CanonicalNames []string `json:"canonicalNames,omitempty" firestore:"canonicalNames,omitempty"`

	// ExtractionLineage stores the deterministic/LLM pipeline layers that produced this fact.
	// It allows downstream graph logic to distinguish source mechanics, heuristic normalization,
	// signal derivation, and optional LLM-assisted interpretation without relying on ad-hoc tags.
	ExtractionLineage *FactExtractionLineage `json:"extractionLineage,omitempty" firestore:"extractionLineage,omitempty"`
}

type FactExtractionLayer string

const (
	FactExtractionLayerSourceMechanics FactExtractionLayer = "source_mechanics"
	FactExtractionLayerHeuristic       FactExtractionLayer = "heuristic"
	FactExtractionLayerLLM             FactExtractionLayer = "llm"
	FactExtractionLayerSignal          FactExtractionLayer = "signal"
	FactExtractionLayerInterpretation  FactExtractionLayer = "interpretation"
	FactExtractionLayerWarning         FactExtractionLayer = "warning"
)

type FactExtractionStep struct {
	Layer         FactExtractionLayer `json:"layer" firestore:"layer"`
	Driver        string              `json:"driver,omitempty" firestore:"driver,omitempty"`
	Method        string              `json:"method,omitempty" firestore:"method,omitempty"`
	Deterministic bool                `json:"deterministic" firestore:"deterministic"`
	LLMUsed       bool                `json:"llmUsed" firestore:"llmUsed"`
}

type FactExtractionLineage struct {
	Pipeline     string               `json:"pipeline,omitempty" firestore:"pipeline,omitempty"`
	SourceType   string               `json:"sourceType,omitempty" firestore:"sourceType,omitempty"`
	CurrentLayer FactExtractionLayer  `json:"currentLayer,omitempty" firestore:"currentLayer,omitempty"`
	LLMEnabled   bool                 `json:"llmEnabled" firestore:"llmEnabled"`
	LLMUsed      bool                 `json:"llmUsed" firestore:"llmUsed"`
	Steps        []FactExtractionStep `json:"steps,omitempty" firestore:"steps,omitempty"`
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

	if f.Title != "" {
		add(f.Title)
	}
	if f.Summary != "" {
		add(f.Summary)
	}
	if f.Kind != "" || f.Type != "" {
		kindType := []string{}
		if f.Kind != "" {
			kindType = append(kindType, fmt.Sprintf("kind %s", f.Kind))
		}
		if f.Type != "" {
			kindType = append(kindType, fmt.Sprintf("type %s", f.Type))
		}
		add("Fact classification: " + strings.Join(kindType, ", "))
	}
	if f.Confidence > 0 {
		add(fmt.Sprintf("Confidence: %.3f", f.Confidence))
	}

	if len(f.CanonicalPaths) > 0 {
		add("Canonical paths: " + strings.Join(f.CanonicalPaths, ", "))
	}
	if len(f.CanonicalNames) > 0 {
		add("Canonical names: " + strings.Join(f.CanonicalNames, ", "))
	}
	if f.LocationPath != nil && strings.TrimSpace(*f.LocationPath) != "" {
		add("Location path: " + strings.TrimSpace(*f.LocationPath))
	}
	if f.EventPlace != nil && strings.TrimSpace(*f.EventPlace) != "" {
		add("Place: " + strings.TrimSpace(*f.EventPlace))
	}
	if f.EventAddress != nil && strings.TrimSpace(*f.EventAddress) != "" {
		add("Address: " + strings.TrimSpace(*f.EventAddress))
	}
	if len(f.ActorPaths) > 0 {
		add("Actor paths: " + strings.Join(f.ActorPaths, ", "))
	}
	if len(f.SpherePaths) > 0 {
		add("Sphere paths: " + strings.Join(f.SpherePaths, ", "))
	}
	if f.OccurredAt != nil {
		add("Occurred at: " + f.OccurredAt.UTC().Format(time.RFC3339))
	}
	if f.ObservedAt != nil {
		add("Observed at: " + f.ObservedAt.UTC().Format(time.RFC3339))
	}
	if len(f.SourceIDs) > 0 {
		add("Source IDs: " + strings.Join(f.SourceIDs, ", "))
	}
	if f.ExtractionLineage != nil {
		if strings.TrimSpace(f.ExtractionLineage.Pipeline) != "" {
			add("Extraction pipeline: " + strings.TrimSpace(f.ExtractionLineage.Pipeline))
		}
		if strings.TrimSpace(string(f.ExtractionLineage.CurrentLayer)) != "" {
			add("Extraction layer: " + strings.TrimSpace(string(f.ExtractionLineage.CurrentLayer)))
		}
		if len(f.ExtractionLineage.Steps) > 0 {
			stepParts := make([]string, 0, len(f.ExtractionLineage.Steps))
			for _, step := range f.ExtractionLineage.Steps {
				layer := strings.TrimSpace(string(step.Layer))
				if layer == "" {
					continue
				}
				part := layer
				if strings.TrimSpace(step.Driver) != "" {
					part += ":" + strings.TrimSpace(step.Driver)
				}
				stepParts = append(stepParts, part)
			}
			if len(stepParts) > 0 {
				add("Extraction steps: " + strings.Join(stepParts, " -> "))
			}
		}
	}
	if len(f.MetricUpdates) > 0 {
		for _, mu := range f.MetricUpdates {
			if strings.TrimSpace(mu.MetricName) == "" {
				continue
			}
			metricLine := fmt.Sprintf("Metric %s value %.6f", strings.TrimSpace(mu.MetricName), mu.Value)
			if mu.Unit != nil && strings.TrimSpace(*mu.Unit) != "" {
				metricLine += " " + strings.TrimSpace(*mu.Unit)
			}
			add(metricLine)
			if mu.Confidence > 0 {
				add(fmt.Sprintf("Metric confidence: %.3f", mu.Confidence))
			}
		}
	}

	return strings.Join(parts, "\n")
}
