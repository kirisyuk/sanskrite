package policy

import "testing"

func TestPolicyEnvEnabled(t *testing.T) {
	t.Setenv(envUnifiedPolicyContract, "")
	if !UnifiedPolicyContractEnabled() {
		t.Fatalf("expected unified policy contract enabled by default")
	}

	t.Setenv(envUnifiedPolicyContract, "0")
	if UnifiedPolicyContractEnabled() {
		t.Fatalf("expected unified policy contract disabled when env=0")
	}

	t.Setenv(envUnifiedPolicyShadow, "true")
	if !UnifiedPolicyShadowModeEnabled() {
		t.Fatalf("expected shadow mode enabled when env=true")
	}
}
