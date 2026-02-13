package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type stubDecisionPolicy struct {
	name     string
	decision PolicyDecision
}

func (s *stubDecisionPolicy) Name() string { return s.name }
func (s *stubDecisionPolicy) Decide(context.Context, PolicyInput) (PolicyDecision, error) {
	return s.decision, nil
}
func (s *stubDecisionPolicy) Explain(PolicyDecision) string { return "" }

func TestPolicyCheckerUnifiedDirectionReverse(t *testing.T) {
	pc := &PolicyChecker{
		validationExec: NewPolicyExecutor(&stubDecisionPolicy{
			name: "validation",
			decision: PolicyDecision{
				Action:       ActionReverse,
				DecisionPath: "rule:reverse",
				Confidence:   1,
				Params: map[string]interface{}{
					"reason":        "reversed",
					"shouldReverse": true,
				},
			},
		}, nil, nil),
	}

	violations, _, err := pc.checkEdgeDirectionWithValidationPolicy(
		context.Background(),
		&horizonmodels.DependencyEdge{
			ID:               "e1",
			SourceFragmentID: "s1",
			TargetFragmentID: "t1",
			Type:             horizonmodels.DependencyTypeDependsOn,
		},
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeMarket,
		"/org/us/acme-inc",
		"/market/us/technology/semiconductors",
	)
	if err != nil {
		t.Fatalf("checkEdgeDirectionWithValidationPolicy failed: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix == nil || violations[0].SuggestedFix.Type != TaskReverseEdge {
		t.Fatalf("expected reverse-edge suggested fix, got %+v", violations[0].SuggestedFix)
	}
}

func TestPolicyCheckerUnifiedDirectionRewriteType(t *testing.T) {
	pc := &PolicyChecker{
		validationExec: NewPolicyExecutor(&stubDecisionPolicy{
			name: "validation",
			decision: PolicyDecision{
				Action:       ActionRewriteType,
				DecisionPath: "rule:rewrite",
				Confidence:   0.95,
				Params: map[string]interface{}{
					"reason":        "rewrite type",
					"newType":       "PARTICIPATES_IN",
					"shouldReverse": true,
				},
			},
		}, nil, nil),
	}

	violations, _, err := pc.checkEdgeDirectionWithValidationPolicy(
		context.Background(),
		&horizonmodels.DependencyEdge{
			ID:               "e2",
			SourceFragmentID: "s2",
			TargetFragmentID: "t2",
			Type:             horizonmodels.DependencyTypeInvestsIn,
		},
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeOrganization,
		"/market/us/technology/semiconductors",
		"/org/us/acme-inc",
	)
	if err != nil {
		t.Fatalf("checkEdgeDirectionWithValidationPolicy failed: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix == nil || violations[0].SuggestedFix.Type != TaskCanonicalizeEdge {
		t.Fatalf("expected canonicalize-edge suggested fix, got %+v", violations[0].SuggestedFix)
	}
}

func TestPolicyCheckerUnifiedDirectionQuarantineSoft(t *testing.T) {
	pc := &PolicyChecker{
		validationExec: NewPolicyExecutor(&stubDecisionPolicy{
			name: "validation",
			decision: PolicyDecision{
				Action:       ActionQuarantineSoft,
				DecisionPath: "rule:quarantine",
				Confidence:   0.8,
				Params: map[string]interface{}{
					"reason": "invalid",
				},
			},
		}, nil, nil),
	}

	violations, _, err := pc.checkEdgeDirectionWithValidationPolicy(
		context.Background(),
		&horizonmodels.DependencyEdge{
			ID:               "e3",
			SourceFragmentID: "s3",
			TargetFragmentID: "t3",
			Type:             horizonmodels.DependencyTypeConsumes,
		},
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeOrganization,
		"/org/us/acme-inc",
		"/org/us/other-inc",
	)
	if err != nil {
		t.Fatalf("checkEdgeDirectionWithValidationPolicy failed: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Severity != SeverityCritical {
		t.Fatalf("expected critical violation, got %s", violations[0].Severity)
	}
}
