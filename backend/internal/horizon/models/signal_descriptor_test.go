package models

import (
	"testing"
	"time"
)

func TestNormalizeSignalModality(t *testing.T) {
	if got := NormalizeSignalModality("  ESCALATION "); got != SignalModalityEscalation {
		t.Fatalf("expected %q, got %q", SignalModalityEscalation, got)
	}
	if got := NormalizeSignalModality("unknown"); got != "" {
		t.Fatalf("expected empty modality for unknown, got %q", got)
	}
}

func TestStrongerSignalModality(t *testing.T) {
	got := StrongerSignalModality(SignalModalityProjected, SignalModalityDisruption)
	if got != SignalModalityDisruption {
		t.Fatalf("expected stronger modality %q, got %q", SignalModalityDisruption, got)
	}
}

func TestSignalDescriptorValidate_NormalizesConstraints(t *testing.T) {
	now := time.Now().UTC()
	s := &SignalDescriptor{
		ID:         "s1",
		FactID:     "f1",
		CreatedAt:  now,
		Axis:       AxisGeoConflict,
		Amplitude:  0.6,
		Polarity:   "risk",
		SeedPaths:  []string{"/org/us/test-llc"},
		Confidence: 0.8,
		ObservedAt: now,
		Constraints: SignalConstraints{
			Timeframe:       "short_term",
			Modality:        "PROJECTED",
			Likelihood:      0.7,
			Impact:          0.9,
			EscalationLevel: EscalationLevelWatch,
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if s.Constraints.TimeHorizon != SignalTimeHorizonShortTerm {
		t.Fatalf("expected timeHorizon=%q, got %q", SignalTimeHorizonShortTerm, s.Constraints.TimeHorizon)
	}
	if s.Constraints.Modality != SignalModalityProjected {
		t.Fatalf("expected modality=%q, got %q", SignalModalityProjected, s.Constraints.Modality)
	}
}
