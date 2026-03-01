// Package ai provides AI model management and task execution capabilities.
package ai

import (
	"strings"
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
	AIProviderGemini   AIProvider = "Gemini"   // Google AI (generativelanguage.googleapis.com) — API key auth
	AIProviderVertexAI AIProvider = "VertexAI" // Vertex AI Platform (aiplatform.googleapis.com) — OAuth2, supports Vertex AI Search grounding
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

	// InternalDataGroundingCapability требуется модель с поддержкой grounding по internal datastore (Vertex AI Search).
	InternalDataGroundingCapability bool `json:"internalDataGroundingCapability"`
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

	// InternalDataGroundingCapability поддерживает ли модель grounding по внутреннему datastore.
	InternalDataGroundingCapability bool

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

	// UserID идентификатор пользователя для телеметрии и usage tracking
	UserID string `json:"userId,omitempty"`

	// Environment окружение для телеметрии
	Environment string `json:"environment,omitempty"`

	// Project проект для телеметрии и usage tracking
	Project string `json:"project,omitempty"`

	// SessionID идентификатор сессии для телеметрии
	SessionID string `json:"sessionId,omitempty"`

	// TraceID идентификатор трассировки для телеметрии
	TraceID string `json:"traceId,omitempty"`

	// UseCase overrides the default TaskType->UseCase mapping when saving usage metrics.
	// Example: "horizon.scout", "horizon.research", "artifact.generate".
	UseCase string `json:"useCase,omitempty"`

	// SpanID идентификатор спана для телеметрии
	SpanID string `json:"spanId,omitempty"`

	// CascadeID optional cascade id for admission control (e.g. Horizon scout cascade).
	CascadeID string `json:"cascadeId,omitempty"`
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

	// GroundingEvidence — свидетельства из Vertex AI grounding: какие фрагменты из базы и веб-источники
	// были использованы моделью. Заполняется только VertexAIService.
	// Система (не модель) определяет provenance по groundingMetadata.
	GroundingEvidence *GroundingEvidence `json:"groundingEvidence,omitempty"`
}

// GroundingEvidence содержит системно извлечённые свидетельства из Vertex AI grounding metadata.
// Позволяет надёжно определить, какие данные из собственной базы (datastore) и какие из интернета (web)
// были использованы при генерации ответа.
type GroundingEvidence struct {
	// DatastoreFragmentIDs — ID фрагментов из Firestore, извлечённые из retrievedContext URIs.
	// Формат URI: gs://bucket/tenantId/horizon_fragments/{fragmentId}.json
	// Это надёжный источник правды: если fragment ID найден → entity точно в базе.
	DatastoreFragmentIDs []string `json:"datastoreFragmentIds,omitempty"`

	// DatastoreURIs — сырые GCS URI из retrievedContext chunks (для отладки).
	DatastoreURIs []string `json:"datastoreUris,omitempty"`

	// WebSources — источники из Google Search grounding (URL + title).
	WebSources []WebCitation `json:"webSources,omitempty"`

	// HasDatastoreResults — были ли результаты из собственной базы.
	HasDatastoreResults bool `json:"hasDatastoreResults"`

	// HasWebResults — были ли результаты из Google Search.
	HasWebResults bool `json:"hasWebResults"`
}

// MergeGroundingEvidence merges add into base, deduplicating fragment IDs, URIs, and web sources.
// Returns a new merged evidence; base and add are not modified.
// Used when combining grounding from multiple research strategies.
func MergeGroundingEvidence(base, add *GroundingEvidence) *GroundingEvidence {
	if base == nil && add == nil {
		return nil
	}
	if base == nil {
		cp := *add
		cp.DatastoreFragmentIDs = append([]string(nil), add.DatastoreFragmentIDs...)
		cp.DatastoreURIs = append([]string(nil), add.DatastoreURIs...)
		cp.WebSources = append([]WebCitation(nil), add.WebSources...)
		return &cp
	}
	if add == nil {
		return base
	}
	out := &GroundingEvidence{
		HasDatastoreResults: base.HasDatastoreResults || add.HasDatastoreResults,
		HasWebResults:       base.HasWebResults || add.HasWebResults,
		DatastoreFragmentIDs: appendUniqueStrings(base.DatastoreFragmentIDs, add.DatastoreFragmentIDs...),
		DatastoreURIs:       appendUniqueStrings(base.DatastoreURIs, add.DatastoreURIs...),
	}
	webSeen := make(map[string]struct{})
	for _, c := range base.WebSources {
		key := c.URL + "|" + c.Title
		if key != "|" {
			webSeen[key] = struct{}{}
			out.WebSources = append(out.WebSources, c)
		}
	}
	for _, c := range add.WebSources {
		key := c.URL + "|" + c.Title
		if key == "|" {
			continue
		}
		if _, ok := webSeen[key]; ok {
			continue
		}
		webSeen[key] = struct{}{}
		out.WebSources = append(out.WebSources, c)
	}
	return out
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(base)+len(values))
	for _, v := range base {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
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
