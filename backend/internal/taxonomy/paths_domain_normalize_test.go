package taxonomy

import "testing"

func TestNormalizeDomainCanonicalPath_BilateralRelations_SortsGeo(t *testing.T) {
	in := "/domain/v1/bilateral-relations/geo/sa/geo/ae"
	if err := ValidateCanonicalPath(in); err != nil {
		t.Fatalf("expected input to be valid: %v", err)
	}
	out, err := NormalizeDomainCanonicalPath(in)
	if err != nil {
		t.Fatalf("NormalizeDomainCanonicalPath error: %v", err)
	}
	want := "/domain/v1/bilateral-relations/geo/ae-sa"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestNormalizeDomainCanonicalPath_BilateralRelations_CompositeGeoStable(t *testing.T) {
	in := "/domain/v1/bilateral-relations/geo/sa-ae"
	if err := ValidateCanonicalPath(in); err != nil {
		t.Fatalf("expected input to be valid: %v", err)
	}
	out, err := NormalizeDomainCanonicalPath(in)
	if err != nil {
		t.Fatalf("NormalizeDomainCanonicalPath error: %v", err)
	}
	want := "/domain/v1/bilateral-relations/geo/ae-sa"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestNormalizeDomainCanonicalPath_DefaultSortsFacets(t *testing.T) {
	in := "/domain/v1/supply-chain/material/lithium/geo/us/application/ev"
	if err := ValidateCanonicalPath(in); err != nil {
		t.Fatalf("expected input to be valid: %v", err)
	}
	out, err := NormalizeDomainCanonicalPath(in)
	if err != nil {
		t.Fatalf("NormalizeDomainCanonicalPath error: %v", err)
	}
	// Deterministic order by key/value: application, geo, material
	want := "/domain/v1/supply-chain/application/ev/geo/us/material/lithium"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}

func TestValidateCanonicalPath_SupplyChainProductFacet(t *testing.T) {
	paths := []string{
		"/domain/v1/supply-chain/geo/th/product/fruits",
		"/domain/v1/supply-chain/geo/th/crop/rice",
		"/domain/v1/supply-chain/geo/th/commodity/rubber",
	}
	for _, p := range paths {
		if err := ValidateCanonicalPath(p); err != nil {
			t.Errorf("ValidateCanonicalPath(%q) = %v", p, err)
		}
	}
}

func TestValidateCanonicalPath_AffectedZoneAfricaGeo(t *testing.T) {
	paths := []string{
		"/domain/v1/affected-zone/geo/africa",
		"/domain/v1/affected-zone/geo/ssa",
		"/domain/v1/affected-zone/geo/mena",
		"/domain/v1/affected-zone/geo/oceania",
		"/domain/v1/affected-zone/geo/sea",
		"/domain/v1/affected-zone/geo/southeast-asia",
		"/domain/v1/affected-zone/geo/central-east-africa",
		"/domain/v1/geopolitical-risk/geo/central-east-africa/topic/military-conflict",
		"/loc/africa/na/na",
	}
	for _, p := range paths {
		if err := ValidateCanonicalPath(p); err != nil {
			t.Errorf("ValidateCanonicalPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateCanonicalPath_ContinentsAndZones(t *testing.T) {
	paths := []string{
		"/loc/oceania/na/na",
		"/loc/europe/na/na",
		"/loc/asia/na/na",
		"/loc/southeast-asia/na/na",
		"/gov/oceania/regional-body",
		"/gov/int/african-union",
		"/org/oceania/pacific-org",
		"/market/oceania/technology/software",
		"/zone/sea/political_tension/spratly-islands",
	}
	for _, p := range paths {
		if err := ValidateCanonicalPath(p); err != nil {
			t.Errorf("ValidateCanonicalPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestNormalizeDomainCanonicalPath_IndustryAnalysis_PreservesIndustryChain(t *testing.T) {
	in := "/domain/v1/industry-analysis/industry/batteries/geo/us/industry/lithium-ion/industry/lfp"
	if err := ValidateCanonicalPath(in); err != nil {
		t.Fatalf("expected input to be valid: %v", err)
	}
	out, err := NormalizeDomainCanonicalPath(in)
	if err != nil {
		t.Fatalf("NormalizeDomainCanonicalPath error: %v", err)
	}
	want := "/domain/v1/industry-analysis/geo/us/industry/batteries/industry/lithium-ion/industry/lfp"
	if out != want {
		t.Fatalf("want %q, got %q", want, out)
	}
}
