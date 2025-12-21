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

	// Optional: metric vocabulary and constraints (kept as data, used by prompts/validators as needed).
	AllowedMetricUnits []string `json:"allowedMetricUnits,omitempty" firestore:"allowedMetricUnits,omitempty"`
}

