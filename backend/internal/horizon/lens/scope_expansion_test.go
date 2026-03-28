package lens

import "testing"

func TestNormalizeScopeExpansionPaths_preservesFirstSeenOrder(t *testing.T) {
	got := normalizeScopeExpansionPaths([]string{" /org/a ", "/org/b", "/org/a", "", "/org/c"})
	want := []string{"/org/a", "/org/b", "/org/c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestPlanScopeExpansion_respectsExistingAndLimit(t *testing.T) {
	apply, already := planScopeExpansion(
		[]string{"/org/a"},
		[]string{"/org/a", "/org/b", "/org/c"},
		2,
	)
	wantApply := []string{"/org/b"}
	wantAlready := []string{"/org/a"}
	if len(apply) != len(wantApply) || apply[0] != wantApply[0] {
		t.Fatalf("apply %v want %v", apply, wantApply)
	}
	if len(already) != len(wantAlready) || already[0] != wantAlready[0] {
		t.Fatalf("already %v want %v", already, wantAlready)
	}
}
