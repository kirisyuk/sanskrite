package models

import (
	"strings"
	"time"
)

// SignalDescriptor is the canonical representation of a quasi-physical impulse.
// It is extracted from events and stored as OntologyFact(kind=signal).
// See risk-architecture.md Contract 1 for full specification.
type SignalDescriptor struct {
	// === Identity ===
	ID        string    `json:"id" firestore:"id"`
	FactID    string    `json:"factId" firestore:"factId"` // Reference to source OntologyFact(kind=event)
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`

	// === Quasi-Physics (the "what") ===
	Axis      string  `json:"axis" firestore:"axis"`                               // Primary physics axis (see Contract 2: 12-axis basis)
	Mechanism string  `json:"mechanism,omitempty" firestore:"mechanism,omitempty"` // Optional: specific mechanism (e.g., "sanctions", "airspace_closure")
	Amplitude float64 `json:"amplitude" firestore:"amplitude"`                     // [0.0, 1.0] - strength of the impulse
	Polarity  string  `json:"polarity" firestore:"polarity"`                       // "risk" | "opportunity"

	// === Spatial Binding (the "where") ===
	ActorPaths   []string `json:"actorPaths,omitempty" firestore:"actorPaths,omitempty"`     // Who caused this (e.g., ["/gov/ir/civil-aviation"])
	SeedPaths    []string `json:"seedPaths,omitempty" firestore:"seedPaths,omitempty"`       // Where to inject the impulse (initial affected nodes)
	SeedNames    []string `json:"seedNames,omitempty" firestore:"seedNames,omitempty"`       // Human-readable names for semantic search (parallel to SeedPaths)
	LocationPath string   `json:"locationPath,omitempty" firestore:"locationPath,omitempty"` // Geographic scope (e.g., "/loc/ir/na/na")
	SpherePaths  []string `json:"spherePaths,omitempty" firestore:"spherePaths,omitempty"`   // Domain/industry scope

	// === Constraints (the "conditions") ===
	Constraints SignalConstraints `json:"constraints,omitempty" firestore:"constraints,omitempty"`

	// === Provenance ===
	Confidence float64   `json:"confidence" firestore:"confidence"`                     // [0.0, 1.0]
	SourceRefs []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"` // Document IDs
	ObservedAt time.Time `json:"observedAt" firestore:"observedAt"`                     // When the event occurred

	// === Supply Chain Attribution ===
	// ChainRefs lists IDs of MaterializedArtifact (supply chain paths) affected by this signal.
	// Populated during seed injection when seed nodes belong to identified supply chains.
	ChainRefs []string `json:"chainRefs,omitempty" firestore:"chainRefs,omitempty"`
}

// SignalConstraints represents conditions and scope for signal propagation.
type SignalConstraints struct {
	DestinationPaths     []string       `json:"destinationPaths,omitempty" firestore:"destinationPaths,omitempty"`         // For route-dependent signals
	Timeframe            string         `json:"timeframe,omitempty" firestore:"timeframe,omitempty"`                       // Legacy alias for TimeHorizon
	TimeHorizon          string         `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`                   // "immediate" | "short_term" | "medium_term" | "long_term"
	Modality             string         `json:"modality,omitempty" firestore:"modality,omitempty"`                         // "actual|confirmed|disruption|escalation|projected|potential|rhetoric"
	Scope                string         `json:"scope,omitempty" firestore:"scope,omitempty"`                               // "primary", "secondary"
	Likelihood           float64        `json:"likelihood,omitempty" firestore:"likelihood,omitempty"`                     // [0.0, 1.0]
	Impact               float64        `json:"impact,omitempty" firestore:"impact,omitempty"`                             // [0.0, 1.0]
	EscalationLevel      string         `json:"escalationLevel,omitempty" firestore:"escalationLevel,omitempty"`           // "none" | "watch" | "warning" | "critical"
	EscalationConfidence float64        `json:"escalationConfidence,omitempty" firestore:"escalationConfidence,omitempty"` // [0.0, 1.0]
	EvidenceBuckets      map[string]int `json:"evidenceBuckets,omitempty" firestore:"evidenceBuckets,omitempty"`           // intent/capability/preparation/corroboration
}

const (
	SignalTimeHorizonImmediate  = "immediate"
	SignalTimeHorizonShortTerm  = "short_term"
	SignalTimeHorizonMediumTerm = "medium_term"
	SignalTimeHorizonLongTerm   = "long_term"

	SignalModalityActual     = "actual"
	SignalModalityConfirmed  = "confirmed"
	SignalModalityDisruption = "disruption"
	SignalModalityEscalation = "escalation"
	SignalModalityProjected  = "projected"
	SignalModalityPotential  = "potential"
	SignalModalityRhetoric   = "rhetoric"

	EscalationLevelNone     = "none"
	EscalationLevelWatch    = "watch"
	EscalationLevelWarning  = "warning"
	EscalationLevelCritical = "critical"
)

var signalModalityRank = map[string]int{
	SignalModalityRhetoric:   0,
	SignalModalityPotential:  1,
	SignalModalityProjected:  2,
	SignalModalityEscalation: 3,
	SignalModalityDisruption: 4,
	SignalModalityConfirmed:  5,
	SignalModalityActual:     6,
}

func NormalizeSignalModality(modality string) string {
	m := strings.ToLower(strings.TrimSpace(modality))
	switch m {
	case SignalModalityActual,
		SignalModalityConfirmed,
		SignalModalityDisruption,
		SignalModalityEscalation,
		SignalModalityProjected,
		SignalModalityPotential,
		SignalModalityRhetoric:
		return m
	default:
		return ""
	}
}

func IsValidSignalModality(modality string) bool {
	return NormalizeSignalModality(modality) != ""
}

// StrongerSignalModality returns the stronger (more-realized) modality.
// If both are invalid/empty, returns empty string.
func StrongerSignalModality(a, b string) string {
	am := NormalizeSignalModality(a)
	bm := NormalizeSignalModality(b)
	if am == "" {
		return bm
	}
	if bm == "" {
		return am
	}
	if signalModalityRank[bm] > signalModalityRank[am] {
		return bm
	}
	return am
}

func NormalizeSignalTimeHorizon(h string) string {
	normalized := strings.ToLower(strings.TrimSpace(h))
	switch normalized {
	case SignalTimeHorizonImmediate,
		SignalTimeHorizonShortTerm,
		SignalTimeHorizonMediumTerm,
		SignalTimeHorizonLongTerm:
		return normalized
	default:
		return ""
	}
}

func IsValidSignalTimeHorizon(h string) bool {
	return NormalizeSignalTimeHorizon(h) != ""
}

func IsValidEscalationLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", EscalationLevelNone, EscalationLevelWatch, EscalationLevelWarning, EscalationLevelCritical:
		return true
	default:
		return false
	}
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

	// Normalize and validate horizon aliases.
	horizon := NormalizeSignalTimeHorizon(s.Constraints.TimeHorizon)
	if horizon == "" && strings.TrimSpace(s.Constraints.Timeframe) != "" {
		horizon = NormalizeSignalTimeHorizon(s.Constraints.Timeframe)
	}
	if strings.TrimSpace(s.Constraints.TimeHorizon) != "" && horizon == "" {
		return ErrInvalidSignalDescriptor("constraints.timeHorizon must be one of immediate|short_term|medium_term|long_term")
	}
	if strings.TrimSpace(s.Constraints.Timeframe) != "" && NormalizeSignalTimeHorizon(s.Constraints.Timeframe) == "" {
		return ErrInvalidSignalDescriptor("constraints.timeframe must be one of immediate|short_term|medium_term|long_term")
	}
	if horizon != "" {
		s.Constraints.TimeHorizon = horizon
		s.Constraints.Timeframe = horizon // keep backward compatibility
	}

	// Normalize and validate modality.
	if strings.TrimSpace(s.Constraints.Modality) != "" {
		modality := NormalizeSignalModality(s.Constraints.Modality)
		if modality == "" {
			return ErrInvalidSignalDescriptor("constraints.modality is invalid")
		}
		s.Constraints.Modality = modality
	}

	// Tail-risk semantics are optional but must be bounded when present.
	if s.Constraints.Likelihood < 0.0 || s.Constraints.Likelihood > 1.0 {
		return ErrInvalidSignalDescriptor("constraints.likelihood must be in [0.0, 1.0]")
	}
	if s.Constraints.Impact < 0.0 || s.Constraints.Impact > 1.0 {
		return ErrInvalidSignalDescriptor("constraints.impact must be in [0.0, 1.0]")
	}
	if s.Constraints.EscalationConfidence < 0.0 || s.Constraints.EscalationConfidence > 1.0 {
		return ErrInvalidSignalDescriptor("constraints.escalationConfidence must be in [0.0, 1.0]")
	}
	if !IsValidEscalationLevel(s.Constraints.EscalationLevel) {
		return ErrInvalidSignalDescriptor("constraints.escalationLevel must be none|watch|warning|critical")
	}

	return nil
}

// ErrInvalidSignalDescriptor represents a validation error for SignalDescriptor.
type ErrInvalidSignalDescriptor string

func (e ErrInvalidSignalDescriptor) Error() string {
	return "invalid SignalDescriptor: " + string(e)
}
