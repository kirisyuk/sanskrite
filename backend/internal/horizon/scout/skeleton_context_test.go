package scout

import (
	"encoding/json"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestGovSkeletonContext_DecisionEdgesFieldExists(t *testing.T) {
	ctx := govSkeletonContext{
		Kind:          "gov_skeleton",
		CanonicalPath: "/gov/sa/sama",
		Exists:        true,
		FragmentID:    "f1",
		DecisionEdges: []skeletonEdgeView{
			{
				Type:       string(models.DependencyTypeGatekeepsDecision),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/decision/sa-banking/grant-banking-license",
			},
			{
				Type:       string(models.DependencyTypeControlsAccess),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/org/sa/some-bank",
			},
			{
				Type:       string(models.DependencyTypeUsesMechanism),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/mechanism/regulatory/banking-license",
			},
		},
		IncomingStratEdges: []skeletonEdgeView{
			{
				Type:       string(models.DependencyTypeAppliesTo),
				SourcePath: "/mechanism/regulatory/capital-adequacy",
				TargetPath: "/gov/sa/sama",
			},
		},
	}

	b, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("failed to marshal govSkeletonContext: %v", err)
	}
	s := string(b)

	for _, want := range []string{
		`"decisionEdges"`,
		`"incomingStrategicEdges"`,
		`GATEKEEPS_DECISION`,
		`CONTROLS_ACCESS`,
		`USES_MECHANISM`,
		`APPLIES_TO`,
		`/decision/sa-banking/grant-banking-license`,
		`/mechanism/regulatory/banking-license`,
	} {
		if !containsSubstr(s, want) {
			t.Errorf("expected JSON to contain %q", want)
		}
	}
}

func TestGovSkeletonContext_EmptyDecisionEdgesOmitted(t *testing.T) {
	ctx := govSkeletonContext{
		Kind:          "gov_skeleton",
		CanonicalPath: "/gov/sa/sama",
	}

	b, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	s := string(b)

	if containsSubstr(s, `"decisionEdges"`) {
		t.Errorf("empty decisionEdges should be omitted from JSON")
	}
	if containsSubstr(s, `"incomingStrategicEdges"`) {
		t.Errorf("empty incomingStrategicEdges should be omitted from JSON")
	}
}

func TestGovStrategicEdgeTypes_ContainsExpected(t *testing.T) {
	expected := map[models.DependencyType]bool{
		models.DependencyTypeGatekeepsDecision: false,
		models.DependencyTypeControlsAccess:    false,
		models.DependencyTypeUsesMechanism:     false,
		models.DependencyTypeImposesCondition:   false,
		models.DependencyTypeAppliesTo:          false,
		models.DependencyTypeRegulates:          false,
	}

	for _, et := range govStrategicEdgeTypes {
		if _, ok := expected[et]; !ok {
			t.Errorf("unexpected edge type in govStrategicEdgeTypes: %s", et)
		}
		expected[et] = true
	}

	for et, found := range expected {
		if !found {
			t.Errorf("missing expected edge type in govStrategicEdgeTypes: %s", et)
		}
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
