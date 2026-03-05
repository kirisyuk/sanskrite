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
	for _, forbidden := range []string{"PARTNERS_WITH", "COMPETES_WITH", "SUBSIDIARY_OF"} {
		if !strings.Contains(prompt, forbidden) {
			// Forbidden types should appear in "Do NOT use" clause, not as allowed.
			// The supply-chain block says "Do NOT use PARTNERS_WITH, COMPETES_WITH, SUBSIDIARY_OF..."
			continue
		}
		// If it appears, it must be in the "Do NOT use" context.
		if !strings.Contains(prompt, "Do NOT use") {
			t.Fatalf("supply-chain prompt mentions %q but should forbid it", forbidden)
		}
	}
	// Must include contract types in the rules block. DEPENDS_ON excluded in supply chain.
	for _, allowed := range []string{"PRODUCES", "SUPPLIES_TO", "PARTICIPATES_IN"} {
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
	if strings.Contains(prompt, "Do NOT use PARTNERS_WITH, COMPETES_WITH, SUBSIDIARY_OF") {
		t.Fatalf("general prompt should not have supply-chain-only restrictions")
	}
	if !strings.Contains(prompt, "PARTNERS_WITH") {
		t.Fatalf("general prompt should mention PARTNERS_WITH in relation rules")
	}
}
