package ai

import "testing"

func TestSelectModel_PrefersGemini31FlashLitePreviewOverGemini25FlashLite(t *testing.T) {
	mm := NewAIModelManager()
	selected := mm.SelectModelForProviders(AIRequirements{
		MinCapabilityScore:       7,
		TextGenerationCapability: true,
	}, 2048, nil, map[AIProvider]struct{}{AIProviderGemini: {}})
	if selected != "gemini-3.1-flash-lite-preview" {
		t.Fatalf("expected gemini-3.1-flash-lite-preview, got %q", selected)
	}
}

func TestSelectModel_UsesExplicitFallbackOrderAsAllowlist(t *testing.T) {
	mm := NewAIModelManager()
	selected := mm.SelectModel(AIRequirements{
		PreferredModel:            "gemini-3.1-flash-lite-preview",
		FallbackModels:            []string{"gemini-2.5-pro", "gemini-2.5-flash"},
		MinCapabilityScore:        6,
		TextGenerationCapability:  true,
		SearchGroundingCapability: true,
	}, 2048, []string{"gemini-3.1-flash-lite-preview"}, false)
	if selected != "gemini-2.5-pro" {
		t.Fatalf("expected explicit fallback gemini-2.5-pro, got %q", selected)
	}
}

func TestSelectModel_ExplicitFallbackChainDoesNotEscapeAllowlist(t *testing.T) {
	mm := NewAIModelManager()
	selected := mm.SelectModel(AIRequirements{
		FallbackModels:           []string{"gemini-2.5-flash"},
		MinCapabilityScore:       8,
		TextGenerationCapability: true,
	}, 2048, nil, false)
	if selected != "" {
		t.Fatalf("expected no model when explicit fallback chain is exhausted, got %q", selected)
	}
}

func TestSelectModelForProviders_ExplicitFallbackRespectsProviderSet(t *testing.T) {
	mm := NewAIModelManager()
	selected := mm.SelectModelForProviders(AIRequirements{
		FallbackModels:                  []string{"gemini-2.5-pro", "vertex-gemini-2.5-pro"},
		MinCapabilityScore:              8,
		TextGenerationCapability:        true,
		InternalDataGroundingCapability: true,
	}, 2048, nil, map[AIProvider]struct{}{AIProviderVertexAI: {}})
	if selected != "vertex-gemini-2.5-pro" {
		t.Fatalf("expected vertex fallback, got %q", selected)
	}
}
