package models

import (
	"time"
)

// SignalDescriptor is the canonical representation of a quasi-physical impulse.
// It is extracted from events and stored as OntologyFact(kind=signal).
// See risk-architecture.md Contract 1 for full specification.
type SignalDescriptor struct {
	// === Identity ===
	ID        string    `json:"id" firestore:"id"`
	FactID    string    `json:"factId" firestore:"factId"`    // Reference to source OntologyFact(kind=event)
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`

	// === Quasi-Physics (the "what") ===
	Axis      string  `json:"axis" firestore:"axis"`           // Primary physics axis (see Contract 2: 12-axis basis)
	Mechanism string  `json:"mechanism,omitempty" firestore:"mechanism,omitempty"` // Optional: specific mechanism (e.g., "sanctions", "airspace_closure")
	Amplitude float64 `json:"amplitude" firestore:"amplitude"` // [0.0, 1.0] - strength of the impulse
	Polarity  string  `json:"polarity" firestore:"polarity"`   // "risk" | "opportunity"

	// === Spatial Binding (the "where") ===
	ActorPaths   []string `json:"actorPaths,omitempty" firestore:"actorPaths,omitempty"`   // Who caused this (e.g., ["/gov/ir/civil-aviation"])
	SeedPaths    []string `json:"seedPaths,omitempty" firestore:"seedPaths,omitempty"`    // Where to inject the impulse (initial affected nodes)
	LocationPath string   `json:"locationPath,omitempty" firestore:"locationPath,omitempty"` // Geographic scope (e.g., "/loc/ir/na/na")
	SpherePaths  []string `json:"spherePaths,omitempty" firestore:"spherePaths,omitempty"` // Domain/industry scope

	// === Constraints (the "conditions") ===
	Constraints SignalConstraints `json:"constraints,omitempty" firestore:"constraints,omitempty"`

	// === Provenance ===
	Confidence float64   `json:"confidence" firestore:"confidence"`   // [0.0, 1.0]
	SourceRefs []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"` // Document IDs
	ObservedAt time.Time `json:"observedAt" firestore:"observedAt"`   // When the event occurred
}

// SignalConstraints represents conditions and scope for signal propagation.
type SignalConstraints struct {
	DestinationPaths []string `json:"destinationPaths,omitempty" firestore:"destinationPaths,omitempty"` // For route-dependent signals
	Timeframe        string   `json:"timeframe,omitempty" firestore:"timeframe,omitempty"`               // "immediate", "short_term", "long_term"
	Modality         string   `json:"modality,omitempty" firestore:"modality,omitempty"`                  // "actual", "projected", "potential"
	Scope            string   `json:"scope,omitempty" firestore:"scope,omitempty"`                        // "primary", "secondary"
}

// MetricPath returns the canonical metric path for this signal.
// Format: /metric/{polarity}/{axis}/{mechanism?}
func (s *SignalDescriptor) MetricPath() string {
	path := "/metric/" + s.Polarity + "/" + s.Axis
	if s.Mechanism != "" {
		path += "/" + s.Mechanism
	}
	return path
}

// Validate validates the SignalDescriptor according to Contract 1 invariants.
func (s *SignalDescriptor) Validate() error {
	// Axis must be from 12-axis basis (validated separately)
	if s.Axis == "" {
		return ErrInvalidSignalDescriptor("axis is required")
	}

	// Amplitude must be in [0.0, 1.0]
	if s.Amplitude < 0.0 || s.Amplitude > 1.0 {
		return ErrInvalidSignalDescriptor("amplitude must be in [0.0, 1.0]")
	}

	// Polarity must be "risk" or "opportunity"
	if s.Polarity != "risk" && s.Polarity != "opportunity" {
		return ErrInvalidSignalDescriptor("polarity must be 'risk' or 'opportunity'")
	}

	// At least one of SeedPaths or LocationPath must be present
	if len(s.SeedPaths) == 0 && s.LocationPath == "" {
		return ErrInvalidSignalDescriptor("at least one of SeedPaths or LocationPath must be present")
	}

	// Confidence must be in [0.0, 1.0]
	if s.Confidence < 0.0 || s.Confidence > 1.0 {
		return ErrInvalidSignalDescriptor("confidence must be in [0.0, 1.0]")
	}

	return nil
}

// ErrInvalidSignalDescriptor represents a validation error for SignalDescriptor.
type ErrInvalidSignalDescriptor string

func (e ErrInvalidSignalDescriptor) Error() string {
	return "invalid SignalDescriptor: " + string(e)
}
