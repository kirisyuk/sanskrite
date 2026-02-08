package ai

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// Этот файл содержит примеры использования AI модуля

// ExampleBasicUsage демонстрирует базовое использование AI executor
func ExampleBasicUsage() {
	// Создаем model manager
	modelManager := NewAIModelManager()

	// Создаем сервисы для различных провайдеров
	openAIService := NewOpenAIService("your-openai-api-key", modelManager)
	claudeService := NewClaudeService("your-claude-api-key", modelManager)
	vertexService := NewGeminiService("your-gemini-api-key", modelManager)

	// Создаем executor с доступными сервисами
	executor := NewAITaskExecutor(
		[]AIService{openAIService, claudeService, vertexService},
		modelManager,
	)

	// Создаем задачу
	task := &AITask{
		TaskType:     TaskTypeSummarization,
		InputContent: "Your long text to summarize here...",
		AIRequirements: AIRequirements{
			MinContextWindow:         4000,
			MaxOutputSize:            1000,
			MinCapabilityScore:       5,
			TextGenerationCapability: true,
		},
		EnableTelemetry: true,
	}

	// Выполняем задачу
	ctx := context.Background()
	result, err := executor.ExecuteTask(ctx, task)
	if err != nil {
		log.Fatalf("Error executing task: %v", err)
	}

	fmt.Printf("Result from model %s:\n%s\n", result.Model, result.Content)
	fmt.Printf("Cost: $%.6f, Tokens: %d in + %d out\n",
		result.ProcessingCost, result.InputTokens, result.OutputTokens)
}

// ExampleStructuredOutput демонстрирует использование с верификатором для структурированного вывода
func ExampleStructuredOutput() {
	modelManager := NewAIModelManager()
	openAIService := NewOpenAIService("your-openai-api-key", modelManager)
	executor := NewAITaskExecutor([]AIService{openAIService}, modelManager)

	// Создаем верификатор для извлечения сущностей
	verifier := NewEntityExtractionVerifier()

	task := &AITask{
		TaskType: TaskTypeEntityExtraction,
		InputContent: `Extract entities from: "Apple Inc. announced a new iPhone in San Francisco 
		              on September 15, 2023, with CEO Tim Cook presenting."`,
		AIRequirements: AIRequirements{
			MinContextWindow:         2000,
			MaxOutputSize:            500,
			MinCapabilityScore:       7,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}

	ctx := context.Background()
	result, err := executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		log.Fatalf("Error executing structured task: %v", err)
	}

	// Приводим к нужному типу
	if entities, ok := result.(EntityExtractionResult); ok {
		fmt.Printf("Extracted %d entities:\n", len(entities.Entities))
		for _, entity := range entities.Entities {
			fmt.Printf("- %s: %s (confidence: %.2f)\n",
				entity.Type, entity.Value, entity.Confidence)
		}
	}
}

// ExampleWithToolCalling демонстрирует использование с tool-calling
func ExampleWithToolCalling() {
	modelManager := NewAIModelManager()
	openAIService := NewOpenAIService("your-openai-api-key", modelManager)
	executor := NewAITaskExecutor([]AIService{openAIService}, modelManager)

	// Определяем доступные инструменты
	tools := []ToolSchema{
		{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			ParametersJSONSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
		{
			Name:        "search_web",
			Description: "Search the web for information",
			ParametersJSONSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
	}

	task := &AITask{
		TaskType:     TaskTypeContentAnalysis,
		InputContent: "What's the weather in London and find recent news about AI?",
		AIRequirements: AIRequirements{
			MinContextWindow:         4000,
			MaxOutputSize:            2000,
			MinCapabilityScore:       7,
			TextGenerationCapability: true,
		},
	}

	ctx := context.Background()
	result, err := executor.ExecuteTaskWithTools(ctx, task, tools)
	if err != nil {
		log.Fatalf("Error executing task with tools: %v", err)
	}

	fmt.Printf("Result: %s\n", result.Content)
	fmt.Printf("Used %d tool calls in %d iterations\n",
		len(result.ToolCalls), result.ToolCallIterations)
	for _, toolCall := range result.ToolCalls {
		fmt.Printf("- Called %s (took %dms, success: %v)\n",
			toolCall.ToolName, toolCall.DurationMs, toolCall.Success)
	}
}

// ExampleCustomVerifier демонстрирует создание кастомного верификатора
func ExampleCustomVerifier() {
	// Создаем кастомный верификатор с функцией исправления
	repairFunc := func(data map[string]interface{}) (map[string]interface{}, error) {
		// Исправляем распространенные ошибки
		if title, ok := data["title"].(string); ok {
			// Убираем лишние пробелы
			data["title"] = strings.TrimSpace(title)
		}

		// Добавляем значения по умолчанию для отсутствующих полей
		if _, ok := data["confidence"]; !ok {
			data["confidence"] = 0.5
		}

		return data, nil
	}

	validateFunc := func(data map[string]interface{}) error {
		// Проверяем обязательные поля
		if _, ok := data["title"]; !ok {
			return fmt.Errorf("title is required")
		}

		// Проверяем диапазоны
		if conf, ok := data["confidence"].(float64); ok {
			if conf < 0 || conf > 1 {
				return fmt.Errorf("confidence must be between 0 and 1")
			}
		}

		return nil
	}

	verifier := NewRepairableVerifier(repairFunc, validateFunc)

	modelManager := NewAIModelManager()
	openAIService := NewOpenAIService("your-openai-api-key", modelManager)
	executor := NewAITaskExecutor([]AIService{openAIService}, modelManager)

	task := &AITask{
		TaskType:     TaskTypeContentAnalysis,
		InputContent: "Analyze this content...",
		AIRequirements: AIRequirements{
			MinContextWindow:         2000,
			MaxOutputSize:            500,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}

	ctx := context.Background()
	result, err := executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("Result: %+v\n", result)
}

// ExampleModelSelection демонстрирует выбор моделей с различными требованиями
func ExampleModelSelection() {
	modelManager := NewAIModelManager()

	// Выбор модели для большого контекста
	requirements1 := AIRequirements{
		MinContextWindow:         500000,
		MinCapabilityScore:       8,
		TextGenerationCapability: true,
	}
	model1 := modelManager.SelectModel(requirements1, 100000, nil, false)
	fmt.Printf("For large context: %s\n", model1)

	// Выбор быстрой модели
	requirements2 := AIRequirements{
		MinContextWindow:         10000,
		MinCapabilityScore:       6,
		TextGenerationCapability: true,
	}
	model2 := modelManager.SelectModel(requirements2, 5000, nil, true)
	fmt.Printf("For fast response: %s\n", model2)

	// Выбор модели с поиском
	requirements3 := AIRequirements{
		MinContextWindow:        20000,
		MinCapabilityScore:      7,
		BuiltInSearchCapability: true,
	}
	model3 := modelManager.SelectModel(requirements3, 5000, nil, false)
	fmt.Printf("For search: %s\n", model3)

	// Выбор модели с tool-calling
	requirements4 := AIRequirements{
		MinContextWindow:         30000,
		MinCapabilityScore:       7,
		TextGenerationCapability: true,
	}
	model4 := modelManager.SelectToolCallingModel(requirements4, 10000, nil, false)
	fmt.Printf("For tool-calling: %s\n", model4)
}

// ExampleFallbackBehavior демонстрирует поведение fallback при ошибках
func ExampleFallbackBehavior() {
	modelManager := NewAIModelManager()

	// Создаем executor с несколькими провайдерами
	openAIService := NewOpenAIService("invalid-key-to-trigger-fallback", modelManager)
	claudeService := NewClaudeService("your-claude-api-key", modelManager)
	executor := NewAITaskExecutor([]AIService{openAIService, claudeService}, modelManager)

	task := &AITask{
		TaskType:     TaskTypeSummarization,
		InputContent: "Text to summarize...",
		AIRequirements: AIRequirements{
			MinContextWindow:         4000,
			MaxOutputSize:            1000,
			MinCapabilityScore:       5,
			TextGenerationCapability: true,
		},
	}

	ctx := context.Background()
	result, err := executor.ExecuteTask(ctx, task)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Executor автоматически попробует следующую модель при ошибке
	fmt.Printf("Successfully completed with model: %s\n", result.Model)
}

