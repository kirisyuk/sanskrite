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

