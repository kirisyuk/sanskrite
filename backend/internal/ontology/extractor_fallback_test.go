package ontology

import "testing"

func TestFallbackInternationalLocationPath(t *testing.T) {
	cases := map[string]string{
		"Bab el-Mandeb":    "/loc/int/na/bab-el-mandeb",
		"bab al mandab":    "/loc/int/na/bab-el-mandeb",
		"Strait of Hormuz": "/loc/int/na/strait-of-hormuz",
		"Red Sea":          "/loc/int/na/red-sea",
		"Southern Lebanon": "",
		"Fujairah":         "",
	}
	for in, want := range cases {
		if got := fallbackInternationalLocationPath(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}
