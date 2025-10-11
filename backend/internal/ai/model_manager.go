package ai

import (
	"fmt"
	"log"
	"sort"
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
	mm.models["gemini-2.0-flash"] = AIModelCapabilities{
		Name:                          "gemini-2.0-flash",
		ContextWindow:                 1000000,
		OutputTokens:                  1000000,
		PricePerInputToken:            0.0000050,
		PricePerOutputToken:           0.0000200,
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
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	mm.models["gemini-2.0-flash-lite"] = AIModelCapabilities{
		Name:                          "gemini-2.0-flash-lite",
		ContextWindow:                 1000000,
		OutputTokens:                  1000000,
		PricePerInputToken:            0.0000010,
		PricePerOutputToken:           0.0000040,
		Provider:                      AIProviderGemini,
		MaxTokensPerMinute:            6000000,
		MaxTokensPerDay:               12000000,
		NearlyRealTime:                true,
		CapabilityScore:               5,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	mm.models["gemini-2.0-pro"] = AIModelCapabilities{
		Name:                          "gemini-2.0-pro",
		ContextWindow:                 2000000,
		OutputTokens:                  2000000,
		PricePerInputToken:            0.0000100,
		PricePerOutputToken:           0.0000400,
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
		SupportsToolCalling:           true,
		SupportsMcp:                   true,
		ToolUseProtocol:               ToolUseProtocolGemini,
	}

	// ------------------------- DeepSeek (без нативного Search) -------------------------
	mm.models["deepseek-chat"] = AIModelCapabilities{
		Name:                          "deepseek-chat",
		ContextWindow:                 64000,
		OutputTokens:                  64000,
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

	mm.models["grok-4-fast-non-reasoning"] = AIModelCapabilities{
		Name:                          "grok-4-fast-non-reasoning",
		ContextWindow:                 2000000,
		OutputTokens:                  2000000,
		PricePerInputToken:            0.00000020, // $0.20 / 1M
		PricePerOutputToken:           0.00000050, // $0.50 / 1M
		Provider:                      AIProviderGrok,
		MaxTokensPerMinute:            2000000,
		MaxTokensPerDay:               0,
		NearlyRealTime:                false,
		CapabilityScore:               7,
		ImageGenerationCapability:     false,
		EmbeddingGenerationCapability: false,
		TextGenerationCapability:      true,
		ImageAnalysisCapability:       false,
		BuiltInSearchCapability:       false,
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

	log.Printf("AIModelManager SelectModel called with requirements: MinContextWindow=%d, InputTokens=%d, PreferNearlyRealTime=%v, MinCapabilityScore=%d",
		requirements.MinContextWindow, inputTokens, preferNearlyRealTime, requirements.MinCapabilityScore)

	if excludeModels == nil {
		excludeModels = []string{}
	}

	// Минимально необходимый размер контекстного окна
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)

	// Если поиск не требуется, исключаем модели со встроенным поиском
	searchRequired := requirements.BuiltInSearchCapability

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
		// Логика для поиска: либо требуется и модель поддерживает, либо не требуется и модель не поддерживает
		if (searchRequired && !m.BuiltInSearchCapability) || (!searchRequired && m.BuiltInSearchCapability) {
			continue
		}

		capableModels = append(capableModels, modelCandidate{name: name, model: m})
	}

	// Отдаём приоритет более дешёвым моделям
	sort.Slice(capableModels, func(i, j int) bool {
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

// SelectToolCallingModel выбирает оптимальную модель с поддержкой tool-calling
func (mm *AIModelManager) SelectToolCallingModel(requirements AIRequirements, inputTokens int, excludeModels []string, preferNearlyRealTime bool) string {
	// Нормализация флагов
	mm.normalizeCapabilities(&requirements)

	if excludeModels == nil {
		excludeModels = []string{}
	}
	requiredContextWindow := max(requirements.MinContextWindow, inputTokens)
	searchRequired := requirements.BuiltInSearchCapability

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
		inputCost := float64(inputTokens) * capabilities.PricePerInputToken
		outputCost := float64(outputTokens) * capabilities.PricePerOutputToken
		return inputCost + outputCost, nil
	}
	return 0, fmt.Errorf("model %s not found", modelName)
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
	if requirements.BuiltInSearchCapability {
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
