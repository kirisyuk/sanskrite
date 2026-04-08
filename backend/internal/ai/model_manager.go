package ai

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// AIModelManager управляет реестром доступных AI-моделей, их характеристиками и ценами.
// Предоставляет логику для выбора оптимальной модели на основе требований задачи.
type AIModelManager struct {
	models map[string]AIModelCapabilities
}

// NewAIModelManager создает новый экземпляр AIModelManager с предустановленным реестром моделей
func NewAIModelManager() *AIModelManager {
	mm := &AIModelManager{
		models: make(map[string]AIModelCapabilities),
	}
	mm.initializeModels()
	log.Printf("Initialized AIModelManager with %d models", len(mm.models))
	return mm
}

// initializeModels инициализирует реестр моделей с их характеристиками
func (mm *AIModelManager) initializeModels() {
	// -------------------- Anthropic (без нативного Search) --------------------
	mm.models["claude-sonnet-4-20250514"] = AIModelCapabilities{
		Name:                          "claude-sonnet-4-20250514",
		ContextWindow:                 200000,
		OutputTokens:                  64000,
		PricePerInputToken:            0.000003, // $3 / 1M
		PricePerOutputToken:           0.000015, // $15 / 1M
		Provider:                      AIProviderClaude,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                false,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolAnthropic,
	}

	mm.models["claude-sonnet-4-5-20250929"] = AIModelCapabilities{
		Name:                          "claude-sonnet-4-5-20250929",
		ContextWindow:                 200000,
		OutputTokens:                  64000,
		PricePerInputToken:            0.000003, // $3 / 1M
		PricePerOutputToken:           0.000015, // $15 / 1M
		Provider:                      AIProviderClaude,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                false,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolAnthropic,
	}

	mm.models["claude-haiku-4-5-20251001"] = AIModelCapabilities{
		Name:                          "claude-haiku-4-5-20251001",
		ContextWindow:                 200000,
		OutputTokens:                  64000,
		PricePerInputToken:            0.00000025, // $0.25 / 1M
		PricePerOutputToken:           0.00000125, // $1.25 / 1M
		Provider:                      AIProviderClaude,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                true,
		CapabilityScore:               6,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolAnthropic,
	}

	mm.models["claude-opus-4-1-20250805"] = AIModelCapabilities{
		Name:                          "claude-opus-4-1-20250805",
		ContextWindow:                 200000,
		OutputTokens:                  32000,
		PricePerInputToken:            0.000015, // $15 / 1M
		PricePerOutputToken:           0.000075, // $75 / 1M
		Provider:                      AIProviderClaude,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                false,
		CapabilityScore:               9,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolAnthropic,
	}

	// -------------------- OpenAI (поиск есть только у GPT-4o-семейства) --------------------
	mm.models["gpt-4o-search-preview"] = AIModelCapabilities{
		Name:                          "gpt-4o-search-preview",
		ContextWindow:                 128000,
		OutputTokens:                  16384,
		PricePerInputToken:            0.0000050, // $5 / 1M
		PricePerOutputToken:           0.0000200, // $20 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            40000,
		MaxTokensPerDay:               200000,
		NearlyRealTime:                false,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      false,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       true,
		SupportsToolCalling:           false,
		SupportsMcp:                   false,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	mm.models["gpt-4o-mini"] = AIModelCapabilities{
		Name:                          "gpt-4o-mini",
		ContextWindow:                 128000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000060, // $0.60 / 1M
		PricePerOutputToken:           0.00000240, // $2.40 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            60000,
		MaxTokensPerDay:               300000,
		NearlyRealTime:                true,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	mm.models["gpt-4o-mini-search-preview"] = AIModelCapabilities{
		Name:                          "gpt-4o-mini-search-preview",
		ContextWindow:                 128000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000060, // $0.60 / 1M
		PricePerOutputToken:           0.00000240, // $2.40 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            60000,
		MaxTokensPerDay:               300000,
		NearlyRealTime:                true,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      false,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       true,
		SupportsToolCalling:           false,
		SupportsMcp:                   false,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	mm.models["gpt-4.1-mini-2025-04-14"] = AIModelCapabilities{
		Name:                          "gpt-4.1-mini-2025-04-14",
		ContextWindow:                 1047576,
		OutputTokens:                  128000,
		PricePerInputToken:            0.0000004, // $0.40 / 1M
		PricePerOutputToken:           0.0000016, // $1.60 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            60000,
		MaxTokensPerDay:               300000,
		NearlyRealTime:                true,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	// --------- Новые флагманские GPT-5 (без встроенного Search) ----------
	mm.models["gpt-5"] = AIModelCapabilities{
		Name:                          "gpt-5",
		ContextWindow:                 400000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000100, // $1.00 / 1M
		PricePerOutputToken:           0.00000800, // $8.00 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            80000,
		MaxTokensPerDay:               400000,
		NearlyRealTime:                false,
		CapabilityScore:               10,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	// --------- GPT-5.1 (обновленная версия GPT-5) ----------
	mm.models["gpt-5.1"] = AIModelCapabilities{
		Name:                          "gpt-5.1",
		ContextWindow:                 400000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000100, // $1.00 / 1M (предположительно, как у gpt-5)
		PricePerOutputToken:           0.00000800, // $8.00 / 1M (предположительно, как у gpt-5)
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            80000,
		MaxTokensPerDay:               400000,
		NearlyRealTime:                false,
		CapabilityScore:               10,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	// --------- GPT-5.2 (обновленная версия GPT-5.1) ----------
	mm.models["gpt-5.2"] = AIModelCapabilities{
		Name:                          "gpt-5.2",
		ContextWindow:                 400000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000175, // $1.75 / 1M
		PricePerOutputToken:           0.00001400, // $14.00 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            80000,
		MaxTokensPerDay:               400000,
		NearlyRealTime:                false,
		CapabilityScore:               10,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	mm.models["gpt-5-mini"] = AIModelCapabilities{
		Name:                          "gpt-5-mini",
		ContextWindow:                 400000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000025, // $0.25 / 1M
		PricePerOutputToken:           0.00000200, // $2.00 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            80000,
		MaxTokensPerDay:               400000,
		NearlyRealTime:                true,
		CapabilityScore:               9,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	mm.models["gpt-5-nano"] = AIModelCapabilities{
		Name:                          "gpt-5-nano",
		ContextWindow:                 400000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000005, // $0.05 / 1M
		PricePerOutputToken:           0.00000040, // $0.40 / 1M
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            120000,
		MaxTokensPerDay:               600000,
		NearlyRealTime:                true,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolOpenAI,
	}

	// --------- OpenAI Embeddings ----------
	mm.models["text-embedding-3-large"] = AIModelCapabilities{
		Name:                          "text-embedding-3-large",
		ContextWindow:                 8191,
		OutputTokens:                  8191,
		PricePerInputToken:            0.00000013, // $0.13 / 1M
		PricePerOutputToken:           0.00000013,
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                true,
		CapabilityScore:               1,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: true,
		TextGenerationCapability:      false,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	mm.models["text-embedding-3-small"] = AIModelCapabilities{
		Name:                          "text-embedding-3-small",
		ContextWindow:                 8191,
		OutputTokens:                  8191,
		PricePerInputToken:            0.00000002, // $0.02 / 1M
		PricePerOutputToken:           0.00000002,
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            100000,
		MaxTokensPerDay:               1000000,
		NearlyRealTime:                true,
		CapabilityScore:               1,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: true,
		TextGenerationCapability:      false,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	mm.models["gpt-image-1"] = AIModelCapabilities{
		Name:                          "gpt-image-1",
		ContextWindow:                 0,
		OutputTokens:                  0,
		PricePerInputToken:            0.000005, // $5 / 1M (текст промпта)
		PricePerOutputToken:           0.000040, // $40 / 1M (выход изображений)
		Provider:                      AIProviderOpenAI,
		MaxTokensPerMinute:            0,
		MaxTokensPerDay:               0,
		NearlyRealTime:                true,
		CapabilityScore:               2,
		ImageGenerationCapability:     true,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      false,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	// ------------------- Google DeepMind Gemini (с Grounding) -------------------
	mm.models["gemini-2.5-flash"] = AIModelCapabilities{
		Name:                          "gemini-2.5-flash",
		ContextWindow:                 1000000,
		OutputTokens:                  1000000,
		PricePerInputToken:            0.00000030, // $0.30 / 1M (max in range)
		PricePerOutputToken:           0.00000250, // $2.50 / 1M (max in range)
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            5000000,
		MaxTokensPerDay:               10000000,
		NearlyRealTime:                true,
		CapabilityScore:               6,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	mm.models["gemini-2.5-flash-lite"] = AIModelCapabilities{
		Name:                "gemini-2.5-flash-lite",
		ContextWindow:       1000000,
		OutputTokens:        1000000,
		PricePerInputToken:  0.00000010, // $0.10 / 1M (max in range)
		PricePerOutputToken: 0.00000041, // $0.41 / 1M (slightly increased to favor gpt-5-nano)
		Provider:            AIProviderGemini,
		MaxTokensPerMinute:  6000000,
		MaxTokensPerDay:     12000000,
		NearlyRealTime:      true,
		// Raise to 7 so Horizon heavy tasks (MinCapabilityScore=7) can still select this cheap model
		// when grounding isn't required.
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	// Gemini 3.1 Flash-Lite Preview is listed in the official Gemini API supported models catalog.
	// Pricing is not yet split out separately on the public pricing page, so we mirror the current Flash-Lite envelope
	// until Google publishes dedicated preview pricing.
	mm.models["gemini-3.1-flash-lite-preview"] = AIModelCapabilities{
		Name:                          "gemini-3.1-flash-lite-preview",
		ContextWindow:                 1000000,
		OutputTokens:                  65536,
		PricePerInputToken:            0.00000010,
		PricePerOutputToken:           0.00000041,
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            6000000,
		MaxTokensPerDay:               12000000,
		NearlyRealTime:                true,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	mm.models["gemini-2.5-pro"] = AIModelCapabilities{
		Name:                          "gemini-2.5-pro",
		ContextWindow:                 2000000,
		OutputTokens:                  2000000,
		PricePerInputToken:            0.00000250, // $2.50 / 1M (max in range)
		PricePerOutputToken:           0.00001500, // $15.00 / 1M (max in range)
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            4000000,
		MaxTokensPerDay:               10000000,
		NearlyRealTime:                false,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     true,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	mm.models["gemini-3-pro-preview"] = AIModelCapabilities{
		Name: "gemini-3-pro-preview",
		// Official Gemini API pricing page lists 1M / 64k for Gemini 3 Pro Preview.
		ContextWindow:                 1048576,
		OutputTokens:                  65536,
		PricePerInputToken:            0.00000200, // $2.00 / 1M (up to 200K), $4.00 / 1M (above 200K)
		PricePerOutputToken:           0.00001200, // $12.00 / 1M (up to 200K), $18.00 / 1M (above 200K)
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            4000000,
		MaxTokensPerDay:               10000000,
		NearlyRealTime:                false,
		CapabilityScore:               9, // Higher than 2.5-pro as it's newer
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     true,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	// Gemini 3 Flash Preview (official Gemini API pricing: $0.50 input (text/image/video), $3.00 output per 1M tokens).
	// Source: https://ai.google.dev/gemini-api/docs/pricing
	mm.models["gemini-3-flash-preview"] = AIModelCapabilities{
		Name:                          "gemini-3-flash-preview",
		ContextWindow:                 1048576,
		OutputTokens:                  65536,
		PricePerInputToken:            0.00000050, // $0.50 / 1M (text/image/video). Audio differs ($1.00 / 1M).
		PricePerOutputToken:           0.00000300, // $3.00 / 1M output (including thinking tokens).
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            6000000,
		MaxTokensPerDay:               12000000,
		NearlyRealTime:                true,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       true,
		BuiltInSearchCapability:       false,
		SearchGroundingCapability:     true,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	// ------------------- Vertex AI mirrors for internal datastore grounding -------------------
	// These entries deliberately mirror grounding-capable Gemini models, but with Provider=VertexAI
	// and InternalDataGroundingCapability=true. Routing remains capability-first then price-based.
	mm.models["vertex-gemini-2.5-pro"] = AIModelCapabilities{
		Name:                            "vertex-gemini-2.5-pro",
		ContextWindow:                   2000000,
		OutputTokens:                    2000000,
		PricePerInputToken:              0.00000250,
		PricePerOutputToken:             0.00001500,
		Provider:                        AIProviderVertexAI,
		MaxTokensPerMinute:              4000000,
		MaxTokensPerDay:                 10000000,
		NearlyRealTime:                  false,
		CapabilityScore:                 8,
		ImageGenerationCapability:       false,
		EmbeddingGenerationCapability:   false,
		TextGenerationCapability:        true,
		ImageAnalysisCapability:         true,
		BuiltInSearchCapability:         false,
		SearchGroundingCapability:       true,
		InternalDataGroundingCapability: true,
		SupportsToolCalling:             true,
		SupportsMcp:                     true,
		ToolUseProtocol:                 ToolUseProtocolGemini,
	}
	mm.models["vertex-gemini-3-pro-preview"] = AIModelCapabilities{
		Name:                            "vertex-gemini-3-pro-preview",
		ContextWindow:                   1048576,
		OutputTokens:                    65536,
		PricePerInputToken:              0.00000200,
		PricePerOutputToken:             0.00001200,
		Provider:                        AIProviderVertexAI,
		MaxTokensPerMinute:              4000000,
		MaxTokensPerDay:                 10000000,
		NearlyRealTime:                  false,
		CapabilityScore:                 9,
		ImageGenerationCapability:       false,
		EmbeddingGenerationCapability:   false,
		TextGenerationCapability:        true,
		ImageAnalysisCapability:         true,
		BuiltInSearchCapability:         false,
		SearchGroundingCapability:       true,
		InternalDataGroundingCapability: true,
		SupportsToolCalling:             true,
		SupportsMcp:                     true,
		ToolUseProtocol:                 ToolUseProtocolGemini,
	}
	mm.models["vertex-gemini-3-flash-preview"] = AIModelCapabilities{
		Name:                            "vertex-gemini-3-flash-preview",
		ContextWindow:                   1048576,
		OutputTokens:                    65536,
		PricePerInputToken:              0.00000050,
		PricePerOutputToken:             0.00000300,
		Provider:                        AIProviderVertexAI,
		MaxTokensPerMinute:              6000000,
		MaxTokensPerDay:                 12000000,
		NearlyRealTime:                  true,
		CapabilityScore:                 8,
		ImageGenerationCapability:       false,
		EmbeddingGenerationCapability:   false,
		TextGenerationCapability:        true,
		ImageAnalysisCapability:         true,
		BuiltInSearchCapability:         false,
		SearchGroundingCapability:       true,
		InternalDataGroundingCapability: true,
		SupportsToolCalling:             true,
		SupportsMcp:                     true,
		ToolUseProtocol:                 ToolUseProtocolGemini,
	}

	// ------------------------- DeepSeek (без нативного Search) -------------------------
	mm.models["deepseek-chat"] = AIModelCapabilities{
		Name:                          "deepseek-chat",
		ContextWindow:                 128000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000027,
		PricePerOutputToken:           0.00000110,
		Provider:                      AIProviderDeepSeek,
		MaxTokensPerMinute:            40000,
		MaxTokensPerDay:               200000,
		NearlyRealTime:                true,
		CapabilityScore:               5,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	mm.models["deepseek-reasoner"] = AIModelCapabilities{
		Name:                          "deepseek-reasoner",
		ContextWindow:                 128000,
		OutputTokens:                  128000,
		PricePerInputToken:            0.00000027,
		PricePerOutputToken:           0.00000110,
		Provider:                      AIProviderDeepSeek,
		MaxTokensPerMinute:            40000,
		MaxTokensPerDay:               200000,
		NearlyRealTime:                true,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	// ------------------------- Grok (с Live Search) -------------------------
	mm.models["grok-4-0709"] = AIModelCapabilities{
		Name:                          "grok-4-0709",
		ContextWindow:                 256000,
		OutputTokens:                  256000,
		PricePerInputToken:            0.000003, // $3 / 1M
		PricePerOutputToken:           0.000015, // $15 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            2000000,
		MaxTokensPerDay:               0,
		NearlyRealTime:                false,
		CapabilityScore:               9,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       true,
	}

	mm.models["grok-4-1-fast-non-reasoning"] = AIModelCapabilities{
		Name:                          "grok-4-1-fast-non-reasoning",
		ContextWindow:                 2000000,
		OutputTokens:                  2000000,
		PricePerInputToken:            0.00000020, // $0.20 / 1M
		PricePerOutputToken:           0.00000050, // $0.50 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            4000000, // 4M tpm
		MaxTokensPerDay:               0,
		NearlyRealTime:                false,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
	}

	mm.models["grok-4-1-fast-reasoning"] = AIModelCapabilities{
		Name:                          "grok-4-1-fast-reasoning",
		ContextWindow:                 2000000,
		OutputTokens:                  2000000,
		PricePerInputToken:            0.00000020, // $0.20 / 1M
		PricePerOutputToken:           0.00000050, // $0.50 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            4000000, // 4M tpm
		MaxTokensPerDay:               0,
		NearlyRealTime:                false,
		CapabilityScore:               8, // Higher than non-reasoning due to reasoning capability
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
	}

	mm.models["grok-3"] = AIModelCapabilities{
		Name:                          "grok-3",
		ContextWindow:                 131072,
		OutputTokens:                  131072,
		PricePerInputToken:            0.000003, // $3 / 1M
		PricePerOutputToken:           0.000015, // $15 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            0,
		MaxTokensPerDay:               0,
		NearlyRealTime:                false,
		CapabilityScore:               8,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       true,
	}

	mm.models["grok-3-mini"] = AIModelCapabilities{
		Name:                          "grok-3-mini",
		ContextWindow:                 131072,
		OutputTokens:                  131072,
		PricePerInputToken:            0.00000030, // $0.30 / 1M
		PricePerOutputToken:           0.00000050, // $0.50 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            0,
		MaxTokensPerDay:               0,
		NearlyRealTime:                true,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       true,
	}
}

// SelectModel выбирает оптимальную модель на основе требований
func (mm *AIModelManager) SelectModel(requirements AIRequirements, inputTokens int, excludeModels []string, preferNearlyRealTime bool) string {
	// Нормализация взаимоисключающих флагов
	mm.normalizeCapabilities(&requirements)

	log.Printf("AIModelManager SelectModel called with requirements: MinContextWindow=%d, InputTokens=%d, PreferNearlyRealTime=%v, MinCapabilityScore=%d, PreferredModel=%s, SearchGrounding=%v, InternalDataGrounding=%v",
		requirements.MinContextWindow, inputTokens, preferNearlyRealTime, requirements.MinCapabilityScore, requirements.PreferredModel, requirements.SearchGroundingCapability, requirements.InternalDataGroundingCapability)

	if excludeModels == nil {
		excludeModels = []string{}
	}

	// Fast path: if PreferredModel is set, not excluded, and meets basic requirements — use it directly.
	if pm := strings.TrimSpace(requirements.PreferredModel); pm != "" && !contains(excludeModels, pm) {
		if m, ok := mm.models[pm]; ok {
			if m.ContextWindow >= inputTokens &&
				m.TextGenerationCapability &&
				(!requirements.BuiltInSearchCapability || m.BuiltInSearchCapability) &&
				(!requirements.SearchGroundingCapability || m.SearchGroundingCapability) &&
				(!requirements.InternalDataGroundingCapability || m.InternalDataGroundingCapability) {
				log.Printf("AIModelManager: using PreferredModel %s", pm)
				return pm
			}
			log.Printf("AIModelManager: PreferredModel %s does not meet capability requirements, falling back", pm)
		} else {
			log.Printf("AIModelManager: PreferredModel %s not found in model registry, falling back", pm)
		}
	}

	// If an explicit fallback chain is configured, treat it as an allowlist and preserve its order.
	if explicit := mm.selectExplicitFallbackModel(requirements, inputTokens, excludeModels, preferNearlyRealTime); explicit != "" {
		log.Printf("AIModelManager: using explicit fallback model %s", explicit)
		return explicit
	}
	if len(compactUniqueModelNames(requirements.FallbackModels)) > 0 {
		log.Printf("AIModelManager: explicit fallback chain exhausted without suitable model")
		return ""
	}

	// Минимально необходимый размер контекстного окна
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)

	// Если поиск не требуется, исключаем модели со встроенным поиском
	searchRequired := requirements.BuiltInSearchCapability
	// Требование на Search Grounding (Vertex/Gemini)
	groundingRequired := requirements.SearchGroundingCapability
	// Требование на grounding по internal datastore (Vertex AI Search).
	internalGroundingRequired := requirements.InternalDataGroundingCapability

	// Фильтруем модели по всем критериям
	type modelCandidate struct {
		name  string
		model AIModelCapabilities
	}

	var capableModels []modelCandidate

	for name, m := range mm.models {
		// Проверка всех критериев
		if m.ContextWindow < requiredContextWindow {
			continue
		}
		if contains(excludeModels, name) {
			continue
		}
		if preferNearlyRealTime && !m.NearlyRealTime {
			continue
		}
		if m.CapabilityScore < requirements.MinCapabilityScore {
			continue
		}
		if requirements.TextGenerationCapability && !m.TextGenerationCapability {
			continue
		}
		if requirements.ImageGenerationCapability && !m.ImageGenerationCapability {
			continue
		}
		if requirements.EmbeddingGenerationCapability && !m.EmbeddingGenerationCapability {
			continue
		}
		if requirements.ImageAnalysisCapability && !m.ImageAnalysisCapability {
			continue
		}
		// Логика для классического веб-поиска: либо требуется и модель поддерживает, либо не требуется и модель не поддерживает
		if (searchRequired && !m.BuiltInSearchCapability) || (!searchRequired && m.BuiltInSearchCapability) {
			continue
		}
		// Если требуется Grounding, отбираем только модели с поддержкой Grounding
		if groundingRequired && !m.SearchGroundingCapability {
			continue
		}
		// Если требуется grounding по internal datastore, оставляем только внутренне-grounded модели.
		if internalGroundingRequired && !m.InternalDataGroundingCapability {
			continue
		}

		capableModels = append(capableModels, modelCandidate{name: name, model: m})
	}

	// Отдаём приоритет более дешёвым моделям
	sort.Slice(capableModels, func(i, j int) bool {
		if capableModels[i].model.PricePerOutputToken == capableModels[j].model.PricePerOutputToken {
			if capableModels[i].model.CapabilityScore == capableModels[j].model.CapabilityScore {
				return capableModels[i].name < capableModels[j].name
			}
			return capableModels[i].model.CapabilityScore > capableModels[j].model.CapabilityScore
		}
		return capableModels[i].model.PricePerOutputToken < capableModels[j].model.PricePerOutputToken
	})

	if len(capableModels) > 0 {
		selected := capableModels[0].name
		log.Printf("AIModelManager selected model: %s", selected)
		return selected
	}

	// Если ничего не найдено, пробуем найти модель с самым большим окном, игнорируя размер входа
	var largestModel *modelCandidate
	maxContextWindow := 0

	for name, m := range mm.models {
		if contains(excludeModels, name) {
			continue
		}
		if preferNearlyRealTime && !m.NearlyRealTime {
			continue
		}
		if m.CapabilityScore < requirements.MinCapabilityScore {
			continue
		}
		if requirements.TextGenerationCapability && !m.TextGenerationCapability {
			continue
		}
		if requirements.ImageGenerationCapability && !m.ImageGenerationCapability {
			continue
		}
		if requirements.EmbeddingGenerationCapability && !m.EmbeddingGenerationCapability {
			continue
		}
		if requirements.ImageAnalysisCapability && !m.ImageAnalysisCapability {
			continue
		}
		if (searchRequired && !m.BuiltInSearchCapability) || (!searchRequired && m.BuiltInSearchCapability) {
			continue
		}
		if groundingRequired && !m.SearchGroundingCapability {
			continue
		}
		if internalGroundingRequired && !m.InternalDataGroundingCapability {
			continue
		}

		if m.ContextWindow > maxContextWindow {
			maxContextWindow = m.ContextWindow
			largestModel = &modelCandidate{name: name, model: m}
		}
	}

	if largestModel != nil {
		log.Printf("No model can handle full input. Selected largest model: %s", largestModel.name)
		return largestModel.name
	}

	log.Printf("AIModelManager: No suitable AI model found after applying all filters.")
	return ""
}

// SelectFallbackModel выбирает резервную модель. Логика идентична основному выбору, но с учётом уже опробованных моделей
func (mm *AIModelManager) SelectFallbackModel(requirements AIRequirements, inputTokens int, excludeModels []string) string {
	const MaxFallbacks = 4 // allow initial try + up to 4 fallbacks
	if len(excludeModels) >= MaxFallbacks {
		log.Printf("AIModelManager: fallback limit reached (%d). Returning empty string to stop further attempts.", MaxFallbacks)
		return ""
	}

	return mm.SelectModel(requirements, inputTokens, excludeModels, false)
}

// SelectModelForProviders returns the best model that belongs to one of the given providers.
// Used when normal selection returns only models from unregistered providers (e.g. Vertex-only executor).
func (mm *AIModelManager) SelectModelForProviders(requirements AIRequirements, inputTokens int, excludeModels []string, providers map[AIProvider]struct{}) string {
	if len(providers) == 0 {
		return ""
	}
	if explicit := mm.selectExplicitFallbackModelForProviders(requirements, inputTokens, excludeModels, providers); explicit != "" {
		return explicit
	}
	if len(compactUniqueModelNames(requirements.FallbackModels)) > 0 {
		return ""
	}
	providerSet := make(map[AIProvider]bool)
	for p := range providers {
		providerSet[p] = true
	}
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)
	searchRequired := requirements.BuiltInSearchCapability
	groundingRequired := requirements.SearchGroundingCapability
	internalGroundingRequired := requirements.InternalDataGroundingCapability

	type modelCandidate struct {
		name  string
		model AIModelCapabilities
	}
	var capableModels []modelCandidate
	for name, m := range mm.models {
		if !providerSet[m.Provider] {
			continue
		}
		if m.ContextWindow < requiredContextWindow || contains(excludeModels, name) ||
			m.CapabilityScore < requirements.MinCapabilityScore {
			continue
		}
		if requirements.TextGenerationCapability && !m.TextGenerationCapability {
			continue
		}
		if (searchRequired && !m.BuiltInSearchCapability) || (!searchRequired && m.BuiltInSearchCapability) {
			continue
		}
		if groundingRequired && !m.SearchGroundingCapability {
			continue
		}
		if internalGroundingRequired && !m.InternalDataGroundingCapability {
			continue
		}
		capableModels = append(capableModels, modelCandidate{name: name, model: m})
	}
	if len(capableModels) == 0 {
		return ""
	}
	sort.Slice(capableModels, func(i, j int) bool {
		if capableModels[i].model.PricePerOutputToken == capableModels[j].model.PricePerOutputToken {
			if capableModels[i].model.CapabilityScore == capableModels[j].model.CapabilityScore {
				return capableModels[i].name < capableModels[j].name
			}
			return capableModels[i].model.CapabilityScore > capableModels[j].model.CapabilityScore
		}
		return capableModels[i].model.PricePerOutputToken < capableModels[j].model.PricePerOutputToken
	})
	return capableModels[0].name
}

func compactUniqueModelNames(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func (mm *AIModelManager) selectExplicitFallbackModel(requirements AIRequirements, inputTokens int, excludeModels []string, preferNearlyRealTime bool) string {
	for _, name := range compactUniqueModelNames(requirements.FallbackModels) {
		if contains(excludeModels, name) {
			continue
		}
		if mm.modelMeetsRequirements(name, requirements, inputTokens, preferNearlyRealTime, nil) {
			return name
		}
	}
	return ""
}

func (mm *AIModelManager) selectExplicitFallbackModelForProviders(requirements AIRequirements, inputTokens int, excludeModels []string, providers map[AIProvider]struct{}) string {
	for _, name := range compactUniqueModelNames(requirements.FallbackModels) {
		if contains(excludeModels, name) {
			continue
		}
		if mm.modelMeetsRequirements(name, requirements, inputTokens, false, providers) {
			return name
		}
	}
	return ""
}

func (mm *AIModelManager) modelMeetsRequirements(name string, requirements AIRequirements, inputTokens int, preferNearlyRealTime bool, providers map[AIProvider]struct{}) bool {
	m, ok := mm.models[name]
	if !ok {
		return false
	}
	if len(providers) > 0 {
		if _, ok := providers[m.Provider]; !ok {
			return false
		}
	}
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)
	searchRequired := requirements.BuiltInSearchCapability
	groundingRequired := requirements.SearchGroundingCapability
	internalGroundingRequired := requirements.InternalDataGroundingCapability

	if m.ContextWindow < requiredContextWindow {
		return false
	}
	if preferNearlyRealTime && !m.NearlyRealTime {
		return false
	}
	if m.CapabilityScore < requirements.MinCapabilityScore {
		return false
	}
	if requirements.TextGenerationCapability && !m.TextGenerationCapability {
		return false
	}
	if requirements.ImageGenerationCapability && !m.ImageGenerationCapability {
		return false
	}
	if requirements.EmbeddingGenerationCapability && !m.EmbeddingGenerationCapability {
		return false
	}
	if requirements.ImageAnalysisCapability && !m.ImageAnalysisCapability {
		return false
	}
	if (searchRequired && !m.BuiltInSearchCapability) || (!searchRequired && m.BuiltInSearchCapability) {
		return false
	}
	if groundingRequired && !m.SearchGroundingCapability {
		return false
	}
	if internalGroundingRequired && !m.InternalDataGroundingCapability {
		return false
	}
	return true
}

// SelectToolCallingModel выбирает оптимальную модель с поддержкой tool-calling
func (mm *AIModelManager) SelectToolCallingModel(requirements AIRequirements, inputTokens int, excludeModels []string, preferNearlyRealTime bool) string {
	// Нормализация флагов
	mm.normalizeCapabilities(&requirements)

	if excludeModels == nil {
		excludeModels = []string{}
	}
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)
	searchRequired := requirements.BuiltInSearchCapability
	groundingRequired := requirements.SearchGroundingCapability
	internalGroundingRequired := requirements.InternalDataGroundingCapability

	type modelCandidate struct {
		name  string
		model AIModelCapabilities
	}

	var capableModels []modelCandidate

	for name, m := range mm.models {
		if m.ContextWindow < requiredContextWindow {
			continue
		}
		if contains(excludeModels, name) {
			continue
		}
		if preferNearlyRealTime && !m.NearlyRealTime {
			continue
		}
		if m.CapabilityScore < requirements.MinCapabilityScore {
			continue
		}
		if requirements.TextGenerationCapability && !m.TextGenerationCapability {
			continue
		}
		if requirements.ImageGenerationCapability && !m.ImageGenerationCapability {
			continue
		}
		if requirements.EmbeddingGenerationCapability && !m.EmbeddingGenerationCapability {
			continue
		}
		if requirements.ImageAnalysisCapability && !m.ImageAnalysisCapability {
			continue
		}
		if searchRequired && !m.BuiltInSearchCapability {
			continue
		}
		if groundingRequired && !m.SearchGroundingCapability {
			continue
		}
		if internalGroundingRequired && !m.InternalDataGroundingCapability {
			continue
		}
		if !m.SupportsToolCalling {
			continue
		}

		capableModels = append(capableModels, modelCandidate{name: name, model: m})
	}

	sort.Slice(capableModels, func(i, j int) bool {
		return capableModels[i].model.PricePerOutputToken < capableModels[j].model.PricePerOutputToken
	})

	if len(capableModels) > 0 {
		selected := capableModels[0].name
		log.Printf("AIModelManager selected tool-calling model: %s", selected)
		return selected
	}

	// Fallback к наибольшей модели с tool-calling
	var largestModel *modelCandidate
	maxContextWindow := 0

	for name, m := range mm.models {
		if contains(excludeModels, name) {
			continue
		}
		if preferNearlyRealTime && !m.NearlyRealTime {
			continue
		}
		if m.CapabilityScore < requirements.MinCapabilityScore {
			continue
		}
		if requirements.TextGenerationCapability && !m.TextGenerationCapability {
			continue
		}
		if requirements.ImageGenerationCapability && !m.ImageGenerationCapability {
			continue
		}
		if requirements.EmbeddingGenerationCapability && !m.EmbeddingGenerationCapability {
			continue
		}
		if requirements.ImageAnalysisCapability && !m.ImageAnalysisCapability {
			continue
		}
		if searchRequired && !m.BuiltInSearchCapability {
			continue
		}
		if groundingRequired && !m.SearchGroundingCapability {
			continue
		}
		if internalGroundingRequired && !m.InternalDataGroundingCapability {
			continue
		}
		if !m.SupportsToolCalling {
			continue
		}

		if m.ContextWindow > maxContextWindow {
			maxContextWindow = m.ContextWindow
			largestModel = &modelCandidate{name: name, model: m}
		}
	}

	if largestModel != nil {
		log.Printf("No tool-calling model can handle full input. Selected largest tool-calling model: %s", largestModel.name)
		return largestModel.name
	}

	log.Printf("AIModelManager: No suitable tool-calling AI model found after applying all filters.")
	return ""
}

// SelectFallbackToolCallingModel резервный выбор для tool-calling с учётом уже опробованных моделей
func (mm *AIModelManager) SelectFallbackToolCallingModel(requirements AIRequirements, inputTokens int, excludeModels []string) string {
	const MaxFallbacks = 4
	if len(excludeModels) >= MaxFallbacks {
		log.Printf("AIModelManager: tool-calling fallback limit reached (%d). Returning empty string.", MaxFallbacks)
		return ""
	}
	return mm.SelectToolCallingModel(requirements, inputTokens, excludeModels, false)
}

// CalculateProcessingCost рассчитывает примерную стоимость выполнения задачи
func (mm *AIModelManager) CalculateProcessingCost(modelName string, inputTokens int, outputTokens int) (float64, error) {
	if capabilities, ok := mm.models[modelName]; ok {
		// For Gemini models, apply tiered pricing based on token thresholds
		if capabilities.Provider == AIProviderGemini {
			return mm.calculateGeminiCost(modelName, inputTokens, outputTokens, capabilities)
		}
		inputCost := float64(inputTokens) * capabilities.PricePerInputToken
		outputCost := float64(outputTokens) * capabilities.PricePerOutputToken
		return inputCost + outputCost, nil
	}
	return 0, fmt.Errorf("model %s not found", modelName)
}

// calculateGeminiCost calculates cost for Gemini models with tiered pricing
// Gemini pricing typically has tiers: first 128K tokens at one rate, after 128K at a different rate
func (mm *AIModelManager) calculateGeminiCost(modelName string, inputTokens int, outputTokens int, capabilities AIModelCapabilities) (float64, error) {
	const geminiTierThreshold = 128000 // 128K tokens threshold

	// Base prices (first tier, up to 128K tokens)
	// Gemini pricing has tiers: first 128K tokens at full price, after 128K at 50% discount
	// Prices from model_manager.go (these are max prices for first tier)
	// For Gemini 2.5 Flash: $0.30/$2.50 per 1M tokens (input/output) for first 128K
	// For Gemini 2.5 Flash Lite: $0.10/$0.41 per 1M tokens (input/output) for first 128K
	// For Gemini 2.5 Pro: $2.50/$15.00 per 1M tokens (input/output) for first 128K

	// After 128K threshold: 50% discount
	var inputPriceTier1, inputPriceTier2, outputPriceTier1, outputPriceTier2 float64

	// Check more specific models first (flash-lite before flash, pro before other)
	switch {
	case strings.Contains(modelName, "gemini-3-pro"):
		// Gemini 3 Pro Preview: tier threshold is 200K (not 128K)
		// Up to 200K: $2.00 / 1M input, $12.00 / 1M output
		// Above 200K: $4.00 / 1M input, $18.00 / 1M output
		const gemini3TierThreshold = 200000 // 200K tokens threshold for Gemini 3 Pro
		var inputCost, outputCost float64
		if inputTokens <= gemini3TierThreshold {
			inputCost = float64(inputTokens) * 0.00000200 // $2.00 / 1M
		} else {
			inputCost = float64(gemini3TierThreshold)*0.00000200 + float64(inputTokens-gemini3TierThreshold)*0.00000400
		}
		if outputTokens <= gemini3TierThreshold {
			outputCost = float64(outputTokens) * 0.00001200 // $12.00 / 1M
		} else {
			outputCost = float64(gemini3TierThreshold)*0.00001200 + float64(outputTokens-gemini3TierThreshold)*0.00001800
		}
		return inputCost + outputCost, nil
	case strings.Contains(modelName, "gemini-2.5-flash-lite"):
		// Most specific: flash-lite (cheapest)
		inputPriceTier1 = 0.00000010   // $0.10 / 1M (from model_manager.go)
		inputPriceTier2 = 0.00000005   // $0.05 / 1M (50% discount after threshold)
		outputPriceTier1 = 0.00000041  // $0.41 / 1M (from model_manager.go)
		outputPriceTier2 = 0.000000205 // $0.205 / 1M (50% discount after threshold)
	case strings.Contains(modelName, "gemini-2.5-pro"):
		// Pro model (most expensive)
		inputPriceTier1 = 0.00000250  // $2.50 / 1M (from model_manager.go)
		inputPriceTier2 = 0.00000125  // $1.25 / 1M (50% discount after threshold)
		outputPriceTier1 = 0.00001500 // $15.00 / 1M (from model_manager.go)
		outputPriceTier2 = 0.00000750 // $7.50 / 1M (50% discount after threshold)
	case strings.Contains(modelName, "gemini-2.5-flash"):
		// Flash (default/middle tier)
		inputPriceTier1 = 0.00000030  // $0.30 / 1M (from model_manager.go)
		inputPriceTier2 = 0.00000015  // $0.15 / 1M (50% discount after threshold)
		outputPriceTier1 = 0.00000250 // $2.50 / 1M (from model_manager.go)
		outputPriceTier2 = 0.00000125 // $1.25 / 1M (50% discount after threshold)
	default:
		// Fallback to capabilities prices if model not specifically matched
		inputPriceTier1 = capabilities.PricePerInputToken
		inputPriceTier2 = capabilities.PricePerInputToken * 0.5
		outputPriceTier1 = capabilities.PricePerOutputToken
		outputPriceTier2 = capabilities.PricePerOutputToken * 0.5
	}

	// Calculate input cost with tiered pricing
	var inputCost float64
	if inputTokens <= geminiTierThreshold {
		inputCost = float64(inputTokens) * inputPriceTier1
	} else {
		inputCost = float64(geminiTierThreshold)*inputPriceTier1 + float64(inputTokens-geminiTierThreshold)*inputPriceTier2
	}

	// Calculate output cost with tiered pricing
	var outputCost float64
	if outputTokens <= geminiTierThreshold {
		outputCost = float64(outputTokens) * outputPriceTier1
	} else {
		outputCost = float64(geminiTierThreshold)*outputPriceTier1 + float64(outputTokens-geminiTierThreshold)*outputPriceTier2
	}

	return inputCost + outputCost, nil
}

// GetModelCapabilities возвращает характеристики указанной модели
func (mm *AIModelManager) GetModelCapabilities(modelName string) (AIModelCapabilities, error) {
	if capabilities, ok := mm.models[modelName]; ok {
		return capabilities, nil
	}
	return AIModelCapabilities{}, fmt.Errorf("model %s not found", modelName)
}

// normalizeCapabilities приводит флаги требуемых возможностей в консистентное состояние
func (mm *AIModelManager) normalizeCapabilities(requirements *AIRequirements) {
	// Поиск может сочетаться с генерацией текста
	if requirements.BuiltInSearchCapability || requirements.SearchGroundingCapability || requirements.InternalDataGroundingCapability {
		if !requirements.ImageAnalysisCapability &&
			!requirements.TextGenerationCapability &&
			!requirements.ImageGenerationCapability &&
			!requirements.EmbeddingGenerationCapability {
			requirements.TextGenerationCapability = true
		}
	}

	// Обработка взаимоисключающих флагов
	if requirements.ImageAnalysisCapability {
		requirements.TextGenerationCapability = false
		requirements.ImageGenerationCapability = false
		requirements.EmbeddingGenerationCapability = false
	} else if requirements.TextGenerationCapability {
		requirements.ImageGenerationCapability = false
		requirements.EmbeddingGenerationCapability = false
	} else if requirements.ImageGenerationCapability {
		requirements.TextGenerationCapability = false
		requirements.EmbeddingGenerationCapability = false
	} else if requirements.EmbeddingGenerationCapability {
		requirements.TextGenerationCapability = false
		requirements.ImageGenerationCapability = false
	} else if !requirements.BuiltInSearchCapability {
		// Если ничего не выбрано (и поиск не нужен), по умолчанию ставим генерацию текста
		requirements.TextGenerationCapability = true
		requirements.ImageGenerationCapability = false
		requirements.EmbeddingGenerationCapability = false
		requirements.ImageAnalysisCapability = false
	}
}

// Вспомогательные функции
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
