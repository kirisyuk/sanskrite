// Package ai provides AI model management and task execution capabilities.
package ai

import (
	"time"
)

// TaskType определяет тип задачи, выполняемой AI
type TaskType string

const (
	TaskTypeSummarization              TaskType = "Summarization"
	TaskTypeEntityExtraction           TaskType = "EntityExtraction"
	TaskTypeArtifactExtraction         TaskType = "ArtifactExtraction"
	TaskTypeContentAnalysis            TaskType = "ContentAnalysis"
	TaskTypeTopicSuggestion            TaskType = "TopicSuggestion"
	TaskTypeTrendDetection             TaskType = "TrendDetection"
	TaskTypeAnomalyDetection           TaskType = "AnomalyDetection"
	TaskTypeCrossDomainInsight         TaskType = "CrossDomainInsight"
	TaskTypeNarrativeGeneration        TaskType = "NarrativeGeneration"
	TaskTypeOntologyMapping            TaskType = "OntologyMapping"
	TaskTypeOntologyNodeSuggestion     TaskType = "OntologyNodeSuggestion"
	TaskTypeDomainClassification       TaskType = "DomainClassification"
	TaskTypeReportPlanning             TaskType = "ReportPlanning"
	TaskTypeContentGeneration          TaskType = "ContentGeneration"
	TaskTypeEthicsValidation           TaskType = "EthicsValidation"
	TaskTypeNecessityValidation        TaskType = "NecessityValidation"
	TaskTypeKnowledgeSynthesis         TaskType = "KnowledgeSynthesis"
	TaskTypeStrategicValidation        TaskType = "StrategicValidation"
	TaskTypePatternEnrichment          TaskType = "PatternEnrichment"
	TaskTypeContentEnhancement         TaskType = "ContentEnhancement"
	TaskTypePatternAnalysis            TaskType = "PatternAnalysis"
	TaskTypeInsightSynthesis           TaskType = "InsightSynthesis"
	TaskTypeCrossThemeAnalysis         TaskType = "CrossThemeAnalysis"
	TaskTypeEmbeddingCalculation       TaskType = "EmbeddingCalculation"
	TaskTypeImageAnalysis              TaskType = "ImageAnalysis"
	TaskTypeSearch                     TaskType = "Search"
	TaskTypeSignalVerification         TaskType = "SignalVerification"
	TaskTypeInquiryAnalysis            TaskType = "InquiryAnalysis"
	TaskTypeZoneRefinement             TaskType = "ZoneRefinement"
	TaskTypeAliasEnrichment            TaskType = "AliasEnrichment"
	TaskTypeUncertaintyResolution      TaskType = "UncertaintyResolution"
	TaskTypeContextPlacement           TaskType = "ContextPlacement"
	TaskTypeContextInitiation          TaskType = "ContextInitiation"
	TaskTypeContextPassport            TaskType = "ContextPassport"
	TaskTypeTextProcessing             TaskType = "TextProcessing"
	TaskTypeActionFrameDecomposition   TaskType = "ActionFrameDecomposition"
	TaskTypeConsoleInterpretation      TaskType = "ConsoleInterpretation"
	TaskTypeEntityExtractionForConn    TaskType = "EntityExtractionForConnections"
	TaskTypeContextTitleRefresh        TaskType = "ContextTitleRefresh"
	TaskTypeContextDeduplication       TaskType = "ContextDeduplication"
	TaskTypeIntraEventLinking          TaskType = "IntraEventLinking"
	TaskTypeContextBriefing            TaskType = "ContextBriefing"
	TaskTypeCausalLinkClassification   TaskType = "CausalLinkClassification"
	TaskTypeBatchCausalLinkClass       TaskType = "BatchCausalLinkClassification"
	TaskTypeContextAnalysis            TaskType = "ContextAnalysis"
	TaskTypeSeverityAnalysis           TaskType = "SeverityAnalysis"
	TaskTypeCountryCodeMapping         TaskType = "CountryCodeMapping"
	TaskTypeConnectionStrengthAnalysis TaskType = "ConnectionStrengthAnalysis"
)

// AIProvider идентифицирует провайдера AI-моделей
type AIProvider string

const (
	AIProviderOpenAI   AIProvider = "OpenAI"
	AIProviderClaude   AIProvider = "Claude"
	AIProviderGemini   AIProvider = "Gemini"
	AIProviderDeepSeek AIProvider = "DeepSeek"
	AIProviderGrok     AIProvider = "Grok"
)

// AITaskStatus статус выполнения AI-задачи
type AITaskStatus string

const (
	AITaskStatusCompleted AITaskStatus = "Completed"
	AITaskStatusFailed    AITaskStatus = "Failed"
)

// ToolUseProtocol протокол tool-calling для конкретного провайдера
type ToolUseProtocol string

const (
	ToolUseProtocolNone      ToolUseProtocol = "None"
	ToolUseProtocolOpenAI    ToolUseProtocol = "OpenAI"
	ToolUseProtocolAnthropic ToolUseProtocol = "Anthropic"
	ToolUseProtocolGemini    ToolUseProtocol = "Gemini"
)

// AIRequirements определяет требования к AI-модели для выполнения конкретной задачи
type AIRequirements struct {
	// PreferredModel предпочтительная модель для выполнения задачи
	PreferredModel string `json:"preferredModel,omitempty"`

	// FallbackModels список резервных моделей, если предпочтительная недоступна
	FallbackModels []string `json:"fallbackModels,omitempty"`

	// MinContextWindow минимально требуемый размер контекстного окна в токенах
	MinContextWindow int `json:"minContextWindow"`

	// MaxOutputSize максимальный желаемый размер ответа в токенах
	MaxOutputSize int `json:"maxOutputSize"`

	// MinCapabilityScore минимальная условная оценка возможностей модели
	MinCapabilityScore int `json:"minCapabilityScore"`

	// TextGenerationCapability требуется возможность генерации текста
	TextGenerationCapability bool `json:"textGenerationCapability"`

	// ImageGenerationCapability требуется возможность генерации изображений
	ImageGenerationCapability bool `json:"imageGenerationCapability"`

	// EmbeddingGenerationCapability требуется возможность вычисления эмбеддингов
	EmbeddingGenerationCapability bool `json:"embeddingGenerationCapability"`

	// ImageAnalysisCapability требуется возможность анализа изображений
	ImageAnalysisCapability bool `json:"imageAnalysisCapability"`

	// BuiltInSearchCapability требуется модель со встроенной функцией веб-поиска
	BuiltInSearchCapability bool `json:"builtInSearchCapability"`

	// SearchGroundingCapability требуется модель с поддержкой Google Search Grounding (Vertex/Gemini)
	SearchGroundingCapability bool `json:"searchGroundingCapability"`
}

// AIModelCapabilities описывает технические и коммерческие характеристики конкретной AI-модели
type AIModelCapabilities struct {
	// Name имя модели, используемое в API-запросах
	Name string

	// ContextWindow максимальный размер контекстного окна в токенах
	ContextWindow int

	// OutputTokens максимальный размер генерируемого ответа в токенах
	OutputTokens int

	// PricePerInputToken стоимость одного входного токена в USD
	PricePerInputToken float64

	// PricePerOutputToken стоимость одного выходного токена в USD
	PricePerOutputToken float64

	// Provider провайдер, предоставляющий данную модель
	Provider AIProvider

	// MaxTokensPerMinute ограничение на количество токенов в минуту
	MaxTokensPerMinute int

	// MaxTokensPerDay ограничение на количество токенов в день
	MaxTokensPerDay int

	// NearlyRealTime флаг, указывающий, что модель работает с низкой задержкой
	NearlyRealTime bool

	// CapabilityScore условная оценка общих возможностей модели (например, по шкале от 1 до 10)
	CapabilityScore int

	// TextGenerationCapability поддерживает ли модель генерацию текста
	TextGenerationCapability bool

	// ImageGenerationCapability поддерживает ли модель генерацию изображений
	ImageGenerationCapability bool

	// EmbeddingGenerationCapability поддерживает ли модель вычисление векторных представлений
	EmbeddingGenerationCapability bool

	// ImageAnalysisCapability поддерживает ли модель анализ изображений
	ImageAnalysisCapability bool

	// BuiltInSearchCapability поддерживает ли модель встроенную функцию веб-поиска
	BuiltInSearchCapability bool

	// SearchGroundingCapability поддерживает ли модель Search Grounding (Gemini/Vertex)
	SearchGroundingCapability bool

	// SupportsToolCalling поддерживает ли модель function/tool-calling
	SupportsToolCalling bool

	// SupportsMcp предполагается ли использование MCP при tool-calling
	SupportsMcp bool

	// ToolUseProtocol какой протокол tool-use должен применяться при обращении к этой модели
	ToolUseProtocol ToolUseProtocol
}

// AITask представляет собой инкапсулированную задачу для выполнения AI-сервисом
type AITask struct {
	// TaskType тип выполняемой задачи
	TaskType TaskType `json:"taskType"`

	// InputContent входные данные для задачи в виде строки
	InputContent string `json:"inputContent"`

	// AIRequirements требования к AI-модели для выполнения этой задачи
	AIRequirements AIRequirements `json:"aiRequirements"`

	// Verifier опциональный верификатор, который определяет, является ли ответ от AI валидным
	Verifier AITaskResultVerifier `json:"-"`

	// EnableTelemetry включает автоматическую отправку телеметрии (по умолчанию true)
	EnableTelemetry bool `json:"enableTelemetry"`

	// TenantID мультиарендность для телеметрии
	TenantID string `json:"tenantId,omitempty"`

	// Environment окружение для телеметрии
	Environment string `json:"environment,omitempty"`

	// Project проект для телеметрии
	Project string `json:"project,omitempty"`

	// SessionID идентификатор сессии для телеметрии
	SessionID string `json:"sessionId,omitempty"`

	// TraceID идентификатор трассировки для телеметрии
	TraceID string `json:"traceId,omitempty"`

	// SpanID идентификатор спана для телеметрии
	SpanID string `json:"spanId,omitempty"`
}

// WebCitation представляет ссылку на веб-источник
type WebCitation struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	StartIndex int    `json:"startIndex"`
	EndIndex   int    `json:"endIndex"`
}

// ToolCallRecord журнал вызова инструмента
type ToolCallRecord struct {
	CallID        string `json:"callId"`
	ToolName      string `json:"toolName"`
	ArgumentsJSON string `json:"argumentsJson"`
	ResultJSON    string `json:"resultJson,omitempty"`
	DurationMs    int    `json:"durationMs"`
	Success       bool   `json:"success"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
}

// AITaskResult представляет стандартизированный результат выполнения задачи AI
type AITaskResult struct {
	// Content основной текстовый результат, сгенерированный моделью
	Content string `json:"content"`

	// Model имя модели, которая выполнила задачу
	Model string `json:"model"`

	// TaskType тип выполненной задачи
	TaskType TaskType `json:"taskType"`

	// Status статус выполнения (успех или ошибка)
	Status AITaskStatus `json:"status"`

	// InputTokens количество токенов во входном запросе
	InputTokens int `json:"inputTokens"`

	// OutputTokens количество токенов в сгенерированном ответе
	OutputTokens int `json:"outputTokens"`

	// ProcessingCost расчетная стоимость выполнения задачи в USD
	ProcessingCost float64 `json:"processingCost"`

	// ProcessingTime время, затраченное на выполнение задачи
	ProcessingTime time.Duration `json:"processingTime"`

	// Embedding векторное представление (эмбеддинг), если задачей было его вычисление
	Embedding []float64 `json:"embedding,omitempty"`

	// Citations список ссылок на веб-источники, использованных моделью при поиске
	Citations []WebCitation `json:"citations,omitempty"`

	// UsedToolCalling был ли использован режим tool/function-calling
	UsedToolCalling bool `json:"usedToolCalling"`

	// ToolUseProtocol протокол tool-use, применённый провайдером
	ToolUseProtocol ToolUseProtocol `json:"toolUseProtocol"`

	// ToolCallIterations количество итераций цикла tool-calls
	ToolCallIterations int `json:"toolCallIterations"`

	// ToolCalls журнал вызовов инструментов
	ToolCalls []ToolCallRecord `json:"toolCalls,omitempty"`

	// FinishReason причина завершения
	FinishReason string `json:"finishReason,omitempty"`
}

// ImageAnalysisInput структура входных данных для анализа изображения
type ImageAnalysisInput struct {
	UserQuery string `json:"userQuery"`
	ImageLink string `json:"imageLink"`
}

// ToolSchema описание инструмента для tool-calling
type ToolSchema struct {
	Name                 string      `json:"name"`
	Description          string      `json:"description"`
	ParametersJSONSchema interface{} `json:"parameters"`
}

// ToolCallRequest запрос на вызов инструмента от модели
type ToolCallRequest struct {
	CallID        string `json:"callId"`
	ToolName      string `json:"toolName"`
	ArgumentsJSON string `json:"argumentsJson"`
}

// ToolExecutionResult результат выполнения инструмента
type ToolExecutionResult struct {
	CallID                 string `json:"callId"`
	ToolName               string `json:"toolName"`
	ResultJSON             string `json:"resultJson"`
	RequestedArgumentsJSON string `json:"requestedArgumentsJson,omitempty"`
}

// ToolChatStep шаг диалога с tool-calling
type ToolChatStep struct {
	SessionID string            `json:"sessionId"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []ToolCallRequest `json:"toolCalls"`
	IsFinal   bool              `json:"isFinal"`
}
