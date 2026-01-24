package scout

import "testing"

func TestInferFragmentTypeFromCanonicalPath_Domain(t *testing.T) {
	if got := InferFragmentTypeFromCanonicalPath("/domain/v1/supply-chain/geo/us/material/lithium"); string(got) != "DOMAIN" {
		t.Fatalf("InferFragmentTypeFromCanonicalPath(domain) = %q, want %q", got, "DOMAIN")
	}
}

