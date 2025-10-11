// Package ai defines interfaces for AI services.
package ai

import "context"

// AITaskResultVerifier базовый интерфейс для верификатора результатов AI-задач
type AITaskResultVerifier interface {
	// VerifyAndParse проверяет и парсит сырой JSON-ответ от AI
	// Возвращает распарсенный объект или ошибку, если верификация/парсинг не удались
	VerifyAndParse(rawJSON string) (interface{}, error)
}

// AITaskResultVerifierTyped generic-интерфейс для верификатора с строгой типизацией
type AITaskResultVerifierTyped[T any] interface {
	AITaskResultVerifier
	// VerifyAndParseTyped проверяет и парсит сырой JSON-ответ от AI в строго типизированный объект
	VerifyAndParseTyped(rawJSON string) (T, error)
}

// AIService представляет универсальный интерфейс для сервисов AI
type AIService interface {
	// Provider возвращает идентификатор провайдера (OpenAI, Claude, Gemini и т.д.)
	Provider() AIProvider

	// ExecuteTask выполняет основную задачу по обработке текста (генерация, анализ, чат)
	ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error)

	// ExecuteEmbeddingTask выполняет задачу по вычислению векторного представления
	ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error)

	// ExecuteImageAnalysisByLinkTask выполняет задачу по анализу изображения по ссылке
	ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error)

	// ExecuteSearch выполняет поисковый запрос с помощью встроенных возможностей модели
	ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error)
}

// ToolCallingAIService расширенный интерфейс для сервисов с поддержкой tool-calling
type ToolCallingAIService interface {
	AIService

	// BeginChatWithTools начинает диалог с поддержкой tool-calling
	BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error)

	// ContinueWithToolResults продолжает диалог с результатами выполнения инструментов
	ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error)
}
