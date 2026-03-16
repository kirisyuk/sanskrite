package ai

import "testing"

func TestSelectModel_PrefersGemini31FlashLitePreviewOverGemini25FlashLite(t *testing.T) {
	mm := NewAIModelManager()
	selected := mm.SelectModelForProviders(AIRequirements{
		MinCapabilityScore:      7,
		TextGenerationCapability: true,
	}, 2048, nil, map[AIProvider]struct{}{AIProviderGemini: {}})
	if selected != "gemini-3.1-flash-lite-preview" {
		t.Fatalf("expected gemini-3.1-flash-lite-preview, got %q", selected)
	}
}
