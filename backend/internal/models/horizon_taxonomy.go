package models

import "time"

// HorizonTaxonomy is the single source-of-truth taxonomy document stored in Firestore.
// It is intentionally explicit (no inference) to keep ontology stable over time.
type HorizonTaxonomy struct {
	ID        string    `json:"id" firestore:"id"`
	Version   string    `json:"version" firestore:"version"` // e.g. "v1"
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Canonical namespaces/prefixes (Horizon-first).
	CanonicalPrefixes []string `json:"canonicalPrefixes,omitempty" firestore:"canonicalPrefixes,omitempty"`

	// Spheres of influence (/sphere/...)
	AllowedSpherePaths []string          `json:"allowedSpherePaths,omitempty" firestore:"allowedSpherePaths,omitempty"`
	SphereSynonyms     map[string]string `json:"sphereSynonyms,omitempty" firestore:"sphereSynonyms,omitempty"` // label -> /sphere/... path

	// Event type vocabulary (eventType) used for ontology_facts where kind=event.
	AllowedEventTypes []string          `json:"allowedEventTypes,omitempty" firestore:"allowedEventTypes,omitempty"`
	EventTypeSynonyms map[string]string `json:"eventTypeSynonyms,omitempty" firestore:"eventTypeSynonyms,omitempty"` // label -> allowed eventType

	// Relation types for horizon_dependency_edges.
	AllowedRelationTypes []string `json:"allowedRelationTypes,omitempty" firestore:"allowedRelationTypes,omitempty"`

	// Signal types for OntologyFact(kind=signal)
	AllowedSignalKinds []string                      `json:"allowedSignalKinds,omitempty" firestore:"allowedSignalKinds,omitempty"` // "risk", "opportunity", "trend", "direction"
	SignalTypes        map[string]SignalTypeSpecData `json:"signalTypes,omitempty" firestore:"signalTypes,omitempty"`              // "risk:supply_disruption" -> spec
	SignalTypeSynonyms map[string]string             `json:"signalTypeSynonyms,omitempty" firestore:"signalTypeSynonyms,omitempty"` // alias -> canonical type

	// Optional: metric vocabulary and constraints (kept as data, used by prompts/validators as needed).
	AllowedMetricUnits []string `json:"allowedMetricUnits,omitempty" firestore:"allowedMetricUnits,omitempty"`
}

// SignalTypeSpecData is the Firestore representation of SignalTypeSpec.
type SignalTypeSpecData struct {
	Kind               string `json:"kind" firestore:"kind"`                             // "risk", "opportunity", "trend", "direction"
	Label              string `json:"label" firestore:"label"`                          // Human-readable label
	DefaultMetric      string `json:"defaultMetric" firestore:"defaultMetric"`          // Default metric name for rollup
	PropagationProfile string `json:"propagationProfile" firestore:"propagationProfile"` // "upstream_amplified", "downstream_amplified", "jurisdiction_scoped", etc.
}

