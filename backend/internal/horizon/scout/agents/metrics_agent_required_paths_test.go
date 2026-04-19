package agents

import (
	"testing"

)

func TestRequiredMetricPathsForType_IncludesDefaultsAndRequested(t *testing.T) {
	paths := RequiredMetricPathsForType(FragmentTypePerson, []string{
		"risk/custom_pressure",
		"/metric/custom/special_signal",
		"risk/custom_pressure", // duplicate
	})

	if len(paths) == 0 {
		t.Fatalf("expected non-empty required paths")
	}

	hasDefault := false
	hasRequestedA := false
	hasRequestedB := false
	for _, p := range paths {
		if p == "/metric/risk/public_perception" {
			hasDefault = true
		}
		if p == "/metric/risk/custom_pressure" {
			hasRequestedA = true
		}
		if p == "/metric/custom/special_signal" {
			hasRequestedB = true
		}
	}

	if !hasDefault {
		t.Fatalf("expected default person metric in required paths")
	}
	if !hasRequestedA || !hasRequestedB {
		t.Fatalf("expected requested metrics to be canonicalized and included, got %v", paths)
	}
}
