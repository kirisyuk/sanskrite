package policy

import "testing"

func TestBuildDecisionTelemetrySnapshot_QuarantineTriple(t *testing.T) {
	s := BuildDecisionTelemetrySnapshot(DecisionTelemetryInput{
		Kind:         "edge",
		Decision:     PolicyDecision{Action: "quarantine"},
		SourceType:   "MARKET",
		TargetType:   "MARKET",
		RelationType: "PARTICIPATES_IN",
		FinalStatus:  "quarantined",
		FinalReason:  "quarantine: MARKET->MARKET PARTICIPATES_IN invalid",
	})
	if s.Counters["policy_decision_total"] != 1 {
		t.Fatalf("expected policy_decision_total=1")
	}
	if s.Counters["quarantine_rate"] != 1 {
		t.Fatalf("expected quarantine_rate=1")
	}
	if got := s.InvalidTripleCount["top_invalid_triples"]; got != 1 {
		t.Fatalf("expected top_invalid_triples=1, got %d", got)
	}
	if s.InvalidTripleDims["edgeType"] != "PARTICIPATES_IN" {
		t.Fatalf("expected invalid triple edgeType=PARTICIPATES_IN")
	}
}

func TestBuildDecisionTelemetrySnapshot_AutoFixFromCanonicalization(t *testing.T) {
	s := BuildDecisionTelemetrySnapshot(DecisionTelemetryInput{
		Kind:          "edge",
		Decision:      PolicyDecision{Action: "allow"},
		SourceType:    "ORGANIZATION",
		TargetType:    "ORGANIZATION",
		RelationType:  "PRODUCES",
		FinalStatus:   "auto_fixed",
		Canonicalized: true,
	})
	if s.Counters["auto_fix_rate"] != 1 {
		t.Fatalf("expected auto_fix_rate=1")
	}
	if s.Counters["auto_fix_hit_rate"] != 1 {
		t.Fatalf("expected auto_fix_hit_rate=1")
	}
}

func TestBuildDecisionTelemetrySnapshot_UnknownDirectionAndTypePathMismatch(t *testing.T) {
	s := BuildDecisionTelemetrySnapshot(DecisionTelemetryInput{
		Kind:         "edge",
		Decision:     PolicyDecision{Action: "reject"},
		SourceType:   "PERSON",
		TargetType:   "PERSON",
		RelationType: "UNKNOWN_EDGE_TYPE",
		FinalStatus:  "quarantined",
		FinalReason:  "unknown edge type UNKNOWN_EDGE_TYPE: missing direction policy; type_path_mismatch: /product/x with type=ORGANIZATION",
	})
	if s.Counters["policy_reject_non_blocking_rate"] != 1 {
		t.Fatalf("expected policy_reject_non_blocking_rate=1")
	}
	if s.Counters["unknown_direction_policy_count"] != 1 {
		t.Fatalf("expected unknown_direction_policy_count=1")
	}
	if s.Counters["type_path_mismatch_rate"] != 1 {
		t.Fatalf("expected type_path_mismatch_rate=1")
	}
	if s.InvalidTripleCount["top_invalid_triples"] != 1 {
		t.Fatalf("expected top_invalid_triples=1 for unknown direction policy")
	}
}

func TestBuildDecisionTelemetrySnapshot_WeakLinkCountsFalsePositiveRejectRate(t *testing.T) {
	s := BuildDecisionTelemetrySnapshot(DecisionTelemetryInput{
		Kind:         "edge",
		Decision:     PolicyDecision{Action: "allow_with_penalty"},
		SourceType:   "ORGANIZATION",
		TargetType:   "SPHERE",
		RelationType: "INFLUENCES",
		FinalStatus:  "penalized",
		FinalReason:  "INFLUENCES to SPHERE treated as weak-link; preserved with penalty",
	})
	if s.Counters["penalty_rate"] != 1 {
		t.Fatalf("expected penalty_rate=1")
	}
	if s.Counters["false_positive_reject_rate"] != 1 {
		t.Fatalf("expected false_positive_reject_rate=1")
	}
}
