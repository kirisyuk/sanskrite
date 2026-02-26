package taxonomy

import "testing"

func TestNormalizeSourceCategoryPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "/source/medicine/pharma", want: "/source/medicine/pharma", ok: true},
		{in: "source/medicine/pharma", want: "/source/medicine/pharma", ok: true},
		{in: "medicine/pharma", want: "/source/medicine/pharma", ok: true},
		{in: "/source/medicine", want: "/source/medicine/general", ok: true},
		{in: "/source/technology/machine-learning", want: "/source/technology/ai", ok: true},
		{in: "/source/markets/stocks", want: "/source/markets/equities", ok: true},
		{in: "/source/unknown/anything", want: "", ok: false},
		{in: "/source", want: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := NormalizeSourceCategoryPath(tt.in)
		if ok != tt.ok {
			t.Fatalf("NormalizeSourceCategoryPath(%q) ok=%v, want %v", tt.in, ok, tt.ok)
		}
		if got != tt.want {
			t.Fatalf("NormalizeSourceCategoryPath(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeSourceCategoryPaths_Dedup(t *testing.T) {
	in := []string{
		"/source/medicine/pharma",
		"medicine/pharma",
		"/source/medicine",
		"/source/unknown/foo",
	}
	got := NormalizeSourceCategoryPaths(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized paths, got %d (%v)", len(got), got)
	}
	if got[0] != "/source/medicine/pharma" {
		t.Fatalf("unexpected first path: %v", got)
	}
	if got[1] != "/source/medicine/general" {
		t.Fatalf("unexpected second path: %v", got)
	}
}

func TestRecommendedSourceCategoryTree_NotEmpty(t *testing.T) {
	tree := RecommendedSourceCategoryTree()
	if len(tree) == 0 {
		t.Fatalf("expected non-empty recommended source category tree")
	}
	technology, ok := tree["technology"]
	if !ok || len(technology) == 0 {
		t.Fatalf("expected technology branch in recommended tree")
	}
}
