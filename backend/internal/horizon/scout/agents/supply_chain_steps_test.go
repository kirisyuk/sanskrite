package agents

import (
	"context"
	"strings"
	"testing"
)

func TestSupplyChainPromptContextBlockMentionsOperationalUsageConsumers(t *testing.T) {
	ctx := WithSupplyChainStepPromptContext(context.Background(), SupplyChainStepPromptContext{
		ProductFilters: []string{"helium"},
	})
	block := supplyChainPromptContextBlock(ctx)
	if !strings.Contains(block, "industrial/institutional consuming facilities") {
		t.Fatalf("expected prompt context block to preserve industrial downstream consumers, got %q", block)
	}
}

func TestBuildRecipientFacsForProductsPromptAllowsIndustrialUsageConsumers(t *testing.T) {
	prompt := buildRecipientFacsForProductsPrompt(context.Background(), []string{"/product/raw/gas/refined/helium"})
	for _, want := range []string{
		"critical operational consumable",
		"directly consume this product as a strong operational input",
		"industrial and institutional consuming facilities",
		"broad industries like \"healthcare industry\" or \"electronics sector\" when a concrete receiving facility is not supported",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
