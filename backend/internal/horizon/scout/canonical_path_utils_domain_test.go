package scout

import "testing"

func TestInferFragmentTypeFromCanonicalPath_Domain(t *testing.T) {
	if got := InferFragmentTypeFromCanonicalPath("/domain/v1/supply-chain/geo/us/material/lithium"); string(got) != "DOMAIN" {
		t.Fatalf("InferFragmentTypeFromCanonicalPath(domain) = %q, want %q", got, "DOMAIN")
	}
	if got := InferFragmentTypeFromCanonicalPath("/decision/ai-chips/euv-export-control"); string(got) != "DECISION_POINT" {
		t.Fatalf("InferFragmentTypeFromCanonicalPath(decision) = %q, want %q", got, "DECISION_POINT")
	}
	if got := InferFragmentTypeFromCanonicalPath("/mechanism/regulatory/export-control"); string(got) != "MECHANISM" {
		t.Fatalf("InferFragmentTypeFromCanonicalPath(mechanism) = %q, want %q", got, "MECHANISM")
	}
}

