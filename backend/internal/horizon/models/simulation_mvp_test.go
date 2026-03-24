package models

import "testing"

func TestSimulationRunRequestValidateDefaults(t *testing.T) {
	req := &SimulationRunRequest{
		Question: "What happens if export controls tighten?",
		Signal: &SimulationShock{
			Type:      "policy",
			Magnitude: 1.4,
			SeedPaths: []string{"/org/us/acme"},
		},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if req.WorldCount != 8 {
		t.Fatalf("expected default worldCount=8, got %d", req.WorldCount)
	}
	if req.TickCount != 6 {
		t.Fatalf("expected default tickCount=6, got %d", req.TickCount)
	}
	if req.Signal == nil || req.Signal.Magnitude != 1 {
		t.Fatalf("expected signal magnitude to be capped at 1, got %#v", req.Signal)
	}
}

func TestSimulationActionValidateRejectsUnknownType(t *testing.T) {
	action := &SimulationAction{
		Type:    "invent_new_action",
		ActorID: "agent-1",
	}
	if err := action.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported action type")
	}
}

func TestSimulationActionValidateRequiresTargetsForStructuredExecution(t *testing.T) {
	action := &SimulationAction{
		Type:    string(SimulationActionRerouteShipment),
		ActorID: "agent-1",
	}
	if err := action.Validate(); err == nil {
		t.Fatal("expected validation error for reroute_shipment without targetRefs")
	}
}

func TestSimulationBundleDigestStable(t *testing.T) {
	bundleA := &SimulationBundle{
		RunSummary: map[string]any{"runId": "run-1"},
		OutcomeDistribution: map[string]float64{
			"risk_score": 0.52,
		},
	}
	bundleB := &SimulationBundle{
		RunSummary: map[string]any{"runId": "run-1"},
		OutcomeDistribution: map[string]float64{
			"risk_score": 0.52,
		},
	}
	if bundleA.Digest() == "" {
		t.Fatal("expected non-empty digest")
	}
	if bundleA.Digest() != bundleB.Digest() {
		t.Fatalf("expected equal digests, got %q vs %q", bundleA.Digest(), bundleB.Digest())
	}
}
