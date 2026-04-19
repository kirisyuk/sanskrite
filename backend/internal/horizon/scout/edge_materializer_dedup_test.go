package scout

import (
	"testing"

)

func TestEdgeDedupVariant_AssuresByQualifier(t *testing.T) {
	got := edgeDedupVariant(
		DependencyTypeAssures,
		"ASSURES",
		map[string]interface{}{"assurance_type": "certification"},
		"",
	)
	if got != "assurance_type=certification" {
		t.Fatalf("variant=%q, want assurance_type=certification", got)
	}
}

func TestEdgeDedupVariant_AssuresFromLegacyRaw(t *testing.T) {
	got := edgeDedupVariant(
		DependencyTypeAssures,
		"AUDITED_BY",
		nil,
		"",
	)
	if got != "assurance_type=audit" {
		t.Fatalf("variant=%q, want assurance_type=audit", got)
	}
}

func TestEdgeDedupVariant_NonAssuresEmpty(t *testing.T) {
	got := edgeDedupVariant(
		DependencyTypeOwns,
		"AUDITED_BY",
		map[string]interface{}{"assurance_type": "audit"},
		"",
	)
	if got != "" {
		t.Fatalf("variant=%q, want empty", got)
	}
}
