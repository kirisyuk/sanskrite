package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestIntentDecomposer_SupplyChainPromptExcludesNonContractTypes(t *testing.T) {
	a := NewIntentDecomposerAgent(nil)
	prompt := a.buildPromptWithOptions("/product/global/electronics/batteries", "supply chain", nil, nil, DecomposeOptions{
		DomainModes: []string{taxonomy.DomainModeSupplyChain},
	})
	// Supply-chain prompt must stay in canonical AH relation space.
	for _, want := range []string{"produces", "supplies", "participates_in", "beneficially_owns", "controls_via"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("supply-chain prompt must contain canonical relation %q", want)
		}
	}
	// Must explicitly forbid non-contract types.
	for _, forbidden := range []string{"subsidiary_of"} {
		if !strings.Contains(prompt, forbidden) {
			continue
		}
		if !strings.Contains(prompt, "Do NOT use") {
			t.Fatalf("supply-chain prompt mentions %q but should forbid it", forbidden)
		}
	}
	if !strings.Contains(prompt, "alternate_to is allowed only for explicit direct substitution/competition relationships") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded alternate_to usage")
	}
	if !strings.Contains(prompt, "joint_venture_with and wields are allowed only for explicit operational/ecosystem overlays") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded joint_venture_with/wields usage")
	}
	if !strings.Contains(prompt, "invested_in and member_of are allowed only for explicit institutional/structural overlays") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded invested_in/member_of usage")
	}
	for _, allowed := range []string{"produces", "supplies", "participates_in", "alternate_to", "joint_venture_with", "wields", "member_of", "invested_in"} {
		if !strings.Contains(prompt, allowed) {
			t.Fatalf("supply-chain prompt must mention %q", allowed)
		}
	}
	if !strings.Contains(prompt, "Do NOT use depends_on") {
		t.Fatalf("supply-chain prompt must forbid depends_on")
	}
}

func TestIntentDecomposer_GeneralPromptIncludesBroadTypes(t *testing.T) {
	a := NewIntentDecomposerAgent(nil)
	prompt := a.buildPromptWithOptions("/org/us/acme", "", nil, nil, DecomposeOptions{})
	// General prompt uses AllEdgeTypesForPrompt (broader).
	if strings.Contains(prompt, "Do NOT use PARTNERS_WITH, SUBSIDIARY_OF") {
		t.Fatalf("general prompt should not have supply-chain-only restrictions")
	}
	if !strings.Contains(prompt, "joint_venture_with") {
		t.Fatalf("general prompt should mention joint_venture_with in relation rules")
	}
}
