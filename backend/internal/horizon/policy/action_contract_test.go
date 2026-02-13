package policy

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeValidationAction(t *testing.T) {
	tests := map[string]string{
		"reject":     ActionQuarantineSoft,
		"quarantine": ActionQuarantineSoft,
		"penalty":    ActionAllowWithPenalty,
		"allow":      ActionAllow,
		"reverse":    ActionReverse,
	}
	for in, want := range tests {
		if got := NormalizeValidationAction(in); got != want {
			t.Fatalf("NormalizeValidationAction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergePolicyStatusMonotonic(t *testing.T) {
	status := horizonmodels.PolicyStatusPenalized
	status = MergePolicyStatus(status, horizonmodels.PolicyStatusClean)
	if status != horizonmodels.PolicyStatusPenalized {
		t.Fatalf("expected status to remain penalized, got %s", status)
	}
	status = MergePolicyStatus(status, horizonmodels.PolicyStatusQuarantined)
	if status != horizonmodels.PolicyStatusQuarantined {
		t.Fatalf("expected status to escalate to quarantined, got %s", status)
	}
}

func TestApplyEdgeCanonicalizationPolicyTransitionFallback(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:        horizonmodels.DependencyTypeDependsOn,
		SourceType:      horizonmodels.FragmentTypeOrganization,
		TargetType:      horizonmodels.FragmentTypeOrganization,
		RawRelationType: "formerly_known_as",
	})
	if got.EdgeType != horizonmodels.DependencyTypeInfluences {
		t.Fatalf("expected transition fallback to INFLUENCES, got %s", got.EdgeType)
	}
	if got.PolicyReason != "transition_unclassified" {
		t.Fatalf("expected policy reason transition_unclassified, got %q", got.PolicyReason)
	}
	if got.PolicyPenalty < 0.15 {
		t.Fatalf("expected policy penalty >= 0.15, got %.2f", got.PolicyPenalty)
	}
}
