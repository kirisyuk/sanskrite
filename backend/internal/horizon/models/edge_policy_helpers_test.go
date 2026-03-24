package models

import "testing"

func TestHasOwnershipEvidence_RecognizesGovernanceQualifiers(t *testing.T) {
	qualifiers := map[string]interface{}{
		"share_type":     "beneficial",
		"control_type":   "trustee",
		"mechanism":      "nominee_structure",
		"stake_pct":      "87",
		"voting_percent": "92",
	}

	if !HasOwnershipEvidence("", qualifiers, nil) {
		t.Fatal("expected ownership evidence from governance qualifiers")
	}
}

func TestHasOwnershipEvidence_RecognizesBeneficialOwnerReasoning(t *testing.T) {
	if !HasOwnershipEvidence("Ultimate beneficial owner acts through nominee trustees", nil, nil) {
		t.Fatal("expected ownership evidence from beneficial owner language")
	}
}
