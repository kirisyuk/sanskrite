package propagation

import "testing"

func TestPropagationModeAllowsTraversal(t *testing.T) {
	tests := []struct {
		mode      string
		direction string
		want      bool
	}{
		{mode: "both", direction: "source_to_target", want: true},
		{mode: "both", direction: "target_to_source", want: true},
		{mode: "downstream", direction: "source_to_target", want: true},
		{mode: "downstream", direction: "target_to_source", want: false},
		{mode: "upstream", direction: "source_to_target", want: false},
		{mode: "upstream", direction: "target_to_source", want: true},
		{mode: "", direction: "source_to_target", want: true}, // defaults to both
		{mode: "unknown", direction: "target_to_source", want: true},
	}

	for _, tt := range tests {
		if got := propagationModeAllowsTraversal(tt.mode, tt.direction); got != tt.want {
			t.Fatalf("mode=%q direction=%q got=%v want=%v", tt.mode, tt.direction, got, tt.want)
		}
	}
}

func TestNormalizeStrongEdgeDirection(t *testing.T) {
	if got := normalizeStrongEdgeDirection(" both "); got != "both" {
		t.Fatalf("expected both, got %q", got)
	}
	if got := normalizeStrongEdgeDirection("invalid"); got != "" {
		t.Fatalf("expected empty for invalid direction, got %q", got)
	}
}
