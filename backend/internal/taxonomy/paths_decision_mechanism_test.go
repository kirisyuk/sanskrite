package taxonomy

import "testing"

func TestValidateCanonicalPath_DecisionAndMechanism(t *testing.T) {
	valid := []string{
		"/decision/ai-chips/euv-export-control",
		"/mechanism/regulatory/export-control",
	}
	for _, p := range valid {
		if err := ValidateCanonicalPath(p); err != nil {
			t.Fatalf("ValidateCanonicalPath(%q) error: %v", p, err)
		}
	}

	invalid := []string{
		"/decision/ai-chips",
		"/mechanism/only",
		"/decision//missing",
		"/mechanism//missing",
	}
	for _, p := range invalid {
		if err := ValidateCanonicalPath(p); err == nil {
			t.Fatalf("ValidateCanonicalPath(%q) expected error", p)
		}
	}
}
