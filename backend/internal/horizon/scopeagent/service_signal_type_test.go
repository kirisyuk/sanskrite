package scopeagent

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeSignalTypeCanonicalizesAliases(t *testing.T) {
	got := normalizeSignalType("supply_chain_disruption", "risk")
	want := "risk:" + horizonmodels.AxisPhysicalFlowSupply + ":supply_chain_disruption"
	if got != want {
		t.Fatalf("normalizeSignalType() = %q, want %q", got, want)
	}
}

func TestNormalizeSignalTypeDefaultsToCanonicalWatchPressure(t *testing.T) {
	got := normalizeSignalType("", "risk")
	want := "risk:" + horizonmodels.AxisInfoUncertainty + ":watch_pressure"
	if got != want {
		t.Fatalf("normalizeSignalType() = %q, want %q", got, want)
	}
}
