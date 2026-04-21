package scout

import (
	"encoding/json"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestGovSkeletonContext_DecisionEdgesFieldExists(t *testing.T) {
	ctx := govSkeletonContext{
		Kind:          "gov_skeleton",
		CanonicalPath: "/gov/sa/sama",
		Exists:        true,
		FragmentID:    "f1",
		DecisionEdges: []skeletonEdgeView{
			{
				Type:       string(ah.RelationNameGatekeeps),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/decision/sa-banking/grant-banking-license",
			},
			{
				Type:       string(DependencyType("controls_access")),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/org/sa/some-bank",
			},
			{
				Type:       string(ah.RelationNameWields),
				SourcePath: "/gov/sa/sama",
				TargetPath: "/mechanism/regulatory/banking-license",
			},
		},
		IncomingStratEdges: []skeletonEdgeView{
			{
				Type:       string(DependencyType("applies_to")),
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

	// MIGRATION NOTE (AH taxonomy / docs/archive/taxonomy-migration-2026/migration_map.yaml +
	// AGENTS.md): DependencyType*Decision-layer constants now resolve to
	// AH RelationName values (snake_case), so the skeleton context JSON
	// surfaces canonical AH tokens — gatekeeps / controls_access /
	// wields / applies_to — not the pre-migration UPPER_SNAKE wire
	// names. Asserting on the canonical AH names locks the contract
	// expected by downstream agent prompts (which the AGENTS.md "agent
	// emits human-readable snake_case for proposedRelations" rule
	// pins). The substring assertion otherwise still proves all four
	// edge types reach the JSON payload.
	for _, want := range []string{
		`"decisionEdges"`,
		`"incomingStrategicEdges"`,
		`gatekeeps`,
		`controls_access`,
		`wields`,
		`applies_to`,
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
	expected := map[DependencyType]bool{
		ah.RelationNameGatekeeps:          false,
		DependencyType("controls_access"): false,
		ah.RelationNameWields:             false,
		DependencyType("applies_to"):      false,
		ah.RelationNameImposes:            false,
		ah.RelationNameRegulates:          false,
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
