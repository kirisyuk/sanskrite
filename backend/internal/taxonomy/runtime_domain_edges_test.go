package taxonomy

import "testing"

func TestNormalizeRelationType_AllowsDomainEdgesWithoutRuntimeTaxonomy(t *testing.T) {
	SetRuntimeTaxonomy(nil)
	cases := map[string]string{
		"FOCUSES_ON":     "FOCUSES_ON",
		"focuses_on":     "FOCUSES_ON",
		"INVOLVES_ACTOR": "INVOLVES_ACTOR",
		"IN_GEO_SCOPE":   "IN_GEO_SCOPE",
		"ABOUT":          "ABOUT",
		"RELATED_DOMAIN": "RELATED_DOMAIN",
		"BROADENS_TO":    "BROADENS_TO",
		"NARROWS_TO":     "NARROWS_TO",
		"AFFECTS":        "AFFECTS",
	}
	for in, want := range cases {
		if got := NormalizeRelationType(in); got != want {
			t.Fatalf("NormalizeRelationType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRelationType_AllowsDomainEdgesWithStaleRuntimeTaxonomy(t *testing.T) {
	// Simulate a stale Firestore runtime taxonomy that only knows about a subset.
	SetRuntimeTaxonomy(&RuntimeTaxonomy{
		AllowedRelationTypes: map[string]struct{}{
			"OWNS": {},
		},
	})
	if got := NormalizeRelationType("FOCUSES_ON"); got != "FOCUSES_ON" {
		t.Fatalf("NormalizeRelationType(%q) = %q, want %q", "FOCUSES_ON", got, "FOCUSES_ON")
	}
}

