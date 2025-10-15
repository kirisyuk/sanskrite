package crawler

import "testing"

func TestCanonicalizeURL(t *testing.T) {
	cases := map[string]string{
		"http://Example.com/a?x=1#h": "https://example.com/a",
		"https://example.com/a/":     "https://example.com/a",
	}
	for in, want := range cases {
		if got := CanonicalizeURL(in); got != want {
			t.Fatalf("%s -> %s, want %s", in, got, want)
		}
	}
}
