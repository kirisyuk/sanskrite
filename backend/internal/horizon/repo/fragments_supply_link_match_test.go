package repo

import "testing"

func TestNormalizeSupplyLinkRef_NormalizesLegacyOrgPath(t *testing.T) {
	got := normalizeSupplyLinkRef("/org/tesla-inc")
	if got != "/org/na/tesla" {
		t.Fatalf("normalizeSupplyLinkRef(/org/tesla-inc)=%q, want /org/na/tesla", got)
	}
}

func TestBuildSupplyLinkCustomerRefAliases_NoEdgesRepoKeepsNormalizedInput(t *testing.T) {
	r := &HorizonFragmentsRepository{}
	aliases := r.buildSupplyLinkCustomerRefAliases(nil, "/org/tesla-inc", "global")
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d (%v)", len(aliases), aliases)
	}
	if _, ok := aliases["/org/na/tesla"]; !ok {
		t.Fatalf("expected /org/na/tesla alias, got %v", aliases)
	}
}
