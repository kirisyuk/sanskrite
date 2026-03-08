package graphenrich

import "testing"

func TestSupportedMarketDomainsIncludesExpandedDomains(t *testing.T) {
	domains := SupportedMarketDomains()
	want := map[string]bool{
		"energy":                true,
		"industrial":            true,
		"finance":               true,
		"consumer":              true,
		"agri-food":             true,
		"professional-services": true,
		"public-sector":         true,
		"education":             true,
		"media":                 true,
	}
	got := map[string]bool{}
	for _, domain := range domains {
		got[domain] = true
	}
	for domain := range want {
		if !got[domain] {
			t.Fatalf("expected domain %q in supported domains", domain)
		}
	}
}
