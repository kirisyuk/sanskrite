package taxonomy

import "testing"

func withRuntimeTaxonomy(t *testing.T, tax *RuntimeTaxonomy) {
	t.Helper()
	rtMu.Lock()
	prev := rt
	rt = tax
	rtMu.Unlock()
	t.Cleanup(func() {
		rtMu.Lock()
		rt = prev
		rtMu.Unlock()
	})
}

func TestNormalizeRelationType_RuntimeTaxonomyDriftAllowsBuiltIns(t *testing.T) {
	// Simulate a "stale" runtime taxonomy that doesn't include newer built-in relation types.
	withRuntimeTaxonomy(t, &RuntimeTaxonomy{
		Version: "test",
		AllowedRelationTypes: map[string]struct{}{
			"SUPPLIES_TO": {}, // intentionally tiny allowlist
		},
	})

	tests := []struct {
		in   string
		want string
	}{
		{"controls", "CONTROLS"},
		{"MANAGES", "MANAGES"},
		{"controlled_by", "CONTROLS"}, // alias -> CONTROLS
		{"owned_by", "SUBSIDIARY_OF"}, // alias -> SUBSIDIARY_OF
		{"", "INFLUENCES"},
		{"some_new_unknown_type", "INFLUENCES"},
	}

	for _, tt := range tests {
		if got := NormalizeRelationType(tt.in); got != tt.want {
			t.Errorf("NormalizeRelationType(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}



