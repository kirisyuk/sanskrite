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
	// Supply-chain prompt must use supply-chain-only enum.
	wantEnum := taxonomy.SupplyChainEdgeTypesForPrompt()
	if !strings.Contains(prompt, wantEnum) {
		t.Fatalf("supply-chain prompt must contain relation enum %q", wantEnum)
	}
	// Must explicitly forbid non-contract types.
	for _, forbidden := range []string{"SUBSIDIARY_OF"} {
		if !strings.Contains(prompt, forbidden) {
			continue
		}
		if !strings.Contains(prompt, "Do NOT use") {
			t.Fatalf("supply-chain prompt mentions %q but should forbid it", forbidden)
		}
	}
	if !strings.Contains(prompt, "COMPETES_WITH is allowed only for explicit direct competitor relationships") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded COMPETES_WITH usage")
	}
	if !strings.Contains(prompt, "PARTNERS_WITH and OPERATES_VIA are allowed only for explicit operational/ecosystem overlays") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded PARTNERS_WITH/OPERATES_VIA usage")
	}
	if !strings.Contains(prompt, "INVESTS_IN and MEMBER_OF are allowed only for explicit institutional/structural overlays") {
		t.Fatalf("supply-chain prompt must explicitly allow guarded INVESTS_IN/MEMBER_OF usage")
	}
	// Must include contract types in the rules block. DEPENDS_ON excluded in supply chain.
	for _, allowed := range []string{"PRODUCES", "SUPPLIES_TO", "PARTICIPATES_IN", "COMPETES_WITH", "PARTNERS_WITH", "OPERATES_VIA", "MEMBER_OF", "INVESTS_IN"} {
		if !strings.Contains(prompt, allowed) {
			t.Fatalf("supply-chain prompt must mention %q", allowed)
		}
	}
	// Supply chain must explicitly forbid DEPENDS_ON.
	if !strings.Contains(prompt, "Do NOT use DEPENDS_ON") {
		t.Fatalf("supply-chain prompt must forbid DEPENDS_ON")
	}
}

func TestIntentDecomposer_GeneralPromptIncludesBroadTypes(t *testing.T) {
	a := NewIntentDecomposerAgent(nil)
	prompt := a.buildPromptWithOptions("/org/us/acme", "", nil, nil, DecomposeOptions{})
	// General prompt uses AllEdgeTypesForPrompt (broader).
	if strings.Contains(prompt, "Do NOT use PARTNERS_WITH, SUBSIDIARY_OF") {
		t.Fatalf("general prompt should not have supply-chain-only restrictions")
	}
	if !strings.Contains(prompt, "PARTNERS_WITH") {
		t.Fatalf("general prompt should mention PARTNERS_WITH in relation rules")
	}
}
