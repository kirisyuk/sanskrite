package horizon

import "testing"

func TestInferWatchKind(t *testing.T) {
	tests := map[string]string{
		"/loc/ua":                "country",
		"/market/semiconductors": "market",
		"/corridor/red-sea":      "corridor",
		"/org/us/apple-inc":      "fragment",
	}
	for path, want := range tests {
		if got := inferWatchKind(path); got != want {
			t.Fatalf("inferWatchKind(%q) = %q, want %q", path, got, want)
		}
	}
}
