package scout

import "testing"

func TestInferISO2FromLabel_UsesVariableDepthLocPaths(t *testing.T) {
	if got := inferISO2FromLabel("AEON Malaysia Distribution", nil); got != "my" {
		t.Fatalf("got %q want %q", got, "my")
	}
}
