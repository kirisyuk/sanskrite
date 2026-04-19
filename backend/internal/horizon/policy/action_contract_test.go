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
		EdgeType:        relDependsOn,
		SourceType:      classOrganization,
		TargetType:      classOrganization,
		RawRelationType: "formerly_known_as",
	})
	if got.EdgeType != relInfluences {
		t.Fatalf("expected transition fallback to INFLUENCES, got %s", got.EdgeType)
	}
	if got.PolicyReason != "transition_unclassified" {
		t.Fatalf("expected policy reason transition_unclassified, got %q", got.PolicyReason)
	}
	if got.PolicyPenalty < 0.15 {
		t.Fatalf("expected policy penalty >= 0.15, got %.2f", got.PolicyPenalty)
	}
}

func TestApplyEdgeCanonicalizationPolicyTransformsIntoStageOrderSwap(t *testing.T) {
	got := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
		EdgeType:   relTransformsInto,
		SourceType: classProduct,
		TargetType: classProduct,
		SourcePath: "/product/finished/mineral/processing/iron-pellet",
		TargetPath: "/product/raw/mineral/extraction/iron-ore",
	})
	if !got.Changed {
		t.Fatalf("expected canonicalization change for reversed TRANSFORMS_INTO stage order")
	}
	if got.EdgeType != relTransformsInto {
		t.Fatalf("expected edge type to stay TRANSFORMS_INTO, got %s", got.EdgeType)
	}
	if !got.ShouldSwap {
		t.Fatalf("expected shouldSwap=true")
	}
}
