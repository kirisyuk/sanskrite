package models

import "testing"

func TestNormalizeUnitCode_AliasesAndFallback(t *testing.T) {
	if got := NormalizeUnitCode("network_ops"); got != UnitCodeCoreOps {
		t.Fatalf("network_ops => %q, want %q", got, UnitCodeCoreOps)
	}
	if got := NormalizeUnitCode("enterprise"); got != UnitCodeEnterprise {
		t.Fatalf("enterprise => %q, want %q", got, UnitCodeEnterprise)
	}
	if got := NormalizeUnitCode("unknown-value"); got != UnitCodeUnclassified {
		t.Fatalf("unknown => %q, want %q", got, UnitCodeUnclassified)
	}
}

func TestNormalizeUnitPipelinePhase_DefaultAndFlags(t *testing.T) {
	if got := NormalizeUnitPipelinePhase("p2"); got != UnitPipelinePhaseP2 {
		t.Fatalf("p2 => %q, want %q", got, UnitPipelinePhaseP2)
	}
	if got := NormalizeUnitPipelinePhase("unexpected"); got != UnitPipelinePhaseP3 {
		t.Fatalf("default => %q, want %q", got, UnitPipelinePhaseP3)
	}
	if UnitPipelinePhaseP5.IsAutoPhase() {
		t.Fatalf("P5 must not be auto phase")
	}
	if !UnitPipelinePhaseP3.IsAutoPhase() {
		t.Fatalf("P3 must be auto phase")
	}
}

func TestScoutingJob_UnitPhaseHelpers(t *testing.T) {
	job := &ScoutingJob{}
	if got := job.GetUnitPipelinePhase(); got != UnitPipelinePhaseP3 {
		t.Fatalf("default phase => %q, want %q", got, UnitPipelinePhaseP3)
	}
	job.SetUnitPipelinePhase(UnitPipelinePhaseP5)
	if got := job.GetUnitPipelinePhase(); got != UnitPipelinePhaseP5 {
		t.Fatalf("set phase => %q, want %q", got, UnitPipelinePhaseP5)
	}
	if job.HasUnitDiveRequest() {
		t.Fatalf("expected no dive request")
	}
	job.State[StateKeyUnitDiveRequest] = map[string]interface{}{"requestId": "r1"}
	if !job.HasUnitDiveRequest() {
		t.Fatalf("expected dive request to be detected")
	}
}
