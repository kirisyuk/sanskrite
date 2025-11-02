package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// AITaskExecutor центральный оркестратор для выполнения AI-задач.
// Выбирает подходящего провайдера и модель, обрабатывает ошибки,
// управляет кэшированием и обеспечивает отказоустойчивость.
type AITaskExecutor struct {
	aiServices       map[AIProvider]AIService
	modelManager     *AIModelManager
	usageMetricsRepo interface {
		CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string) (string, error)
	} // Wrapper to avoid circular import
}

// NewAITaskExecutor создает новый экземпляр AITaskExecutor
func NewAITaskExecutor(aiServices []AIService, modelManager *AIModelManager) *AITaskExecutor {
	log.Printf("Initializing AITaskExecutor")

	if len(aiServices) == 0 {
		log.Printf("WARNING: aiServices is empty in AITaskExecutor constructor")
	}

	servicesMap := make(map[AIProvider]AIService)
	for _, svc := range aiServices {
		servicesMap[svc.Provider()] = svc
	}

	log.Printf("AITaskExecutor initialized successfully with %d AI services", len(servicesMap))

	return &AITaskExecutor{
		aiServices:       servicesMap,
		modelManager:     modelManager,
		usageMetricsRepo: nil,
	}
}

// WithUsageMetrics sets the usage metrics repository for tracking AI usage
// repo should be *repo.UsageMetricsRepository but we use interface to avoid circular import
func (e *AITaskExecutor) WithUsageMetrics(repo interface {
	CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string) (string, error)
}) *AITaskExecutor {
	e.usageMetricsRepo = repo
	return e
}

// ExecuteTask выполняет общую текстовую задачу с автоматическим выбором модели и отказоустойчивостью
func (e *AITaskExecutor) ExecuteTask(ctx context.Context, task *AITask) (*AITaskResult, error) {
	log.Printf("ExecuteTask called with task type: %s", task.TaskType)
	inputTokens := len(task.InputContent) / 4

	inputHash := e.computeInputHash(task)
	_ = inputHash // для будущего кэширования

	triedModels := []string{}
	startedAt := time.Now()

	for {
		currentModel := e.modelManager.SelectFallbackModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("All models have been tried or are unavailable. Unable to complete the task.")
			return &AITaskResult{
				Content:  "Task could not be completed due to unavailability of suitable AI models.",
				Model:    "None",
				TaskType: task.TaskType,
				Status:   AITaskStatusFailed,
			}, nil
		}

		modelCapabilities, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		aiService, ok := e.aiServices[modelCapabilities.Provider]
		if !ok {
			log.Printf("Provider %s not registered", modelCapabilities.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		log.Printf("Executing task using %s with model %s", modelCapabilities.Provider, currentModel)
		result, err := aiService.ExecuteTask(ctx, task, currentModel)
		if err != nil {
			log.Printf("Error executing task with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		result.Status = AITaskStatusCompleted
		result.InputTokens = inputTokens
		result.OutputTokens = len(result.Content) / 4
		cost, _ := e.modelManager.CalculateProcessingCost(currentModel, result.InputTokens, result.OutputTokens)
		result.ProcessingCost = cost
		result.ProcessingTime = time.Since(startedAt)

		// Save usage metrics if repository is available
		e.saveUsageMetric(ctx, task, currentModel, result.InputTokens, result.OutputTokens, cost)

		return result, nil
	}
}

// ExecuteStructuredTask выполняет задачу, которая должна вернуть строго типизированный ответ,
// и использует для его проверки верификатор из объекта AITask
func (e *AITaskExecutor) ExecuteStructuredTask(ctx context.Context, task *AITask) (interface{}, error) {
	log.Printf("ExecuteStructuredTask called with task type: %s", task.TaskType)
	inputChars := len(task.InputContent)
	inputTokens := inputChars / 4 // Approximate token count (4 chars per token)
	startedAt := time.Now()

	if task.Verifier == nil {
		return nil, fmt.Errorf("ExecuteStructuredTask requires a Verifier in the AITask object, but it was nil")
	}

	triedModels := []string{}

	for {
		currentModel := e.modelManager.SelectFallbackModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("ERROR: All models tried or unavailable for structured task '%s'. Total models tried: %d, Input size: %d chars (%d tokens est)",
				task.TaskType, len(triedModels), inputChars, inputTokens)
			return nil, fmt.Errorf("unable to complete structured task '%s': No suitable AI models available after all fallbacks (windowChars=%d)", task.TaskType, inputChars)
		}

		modelCapabilities, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		aiService, ok := e.aiServices[modelCapabilities.Provider]
		if !ok {
			log.Printf("Provider %s not registered", modelCapabilities.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		log.Printf("[%s] Executing structured task with model %s (input: %d chars, %d tokens est)",
			modelCapabilities.Provider, currentModel, inputChars, inputTokens)

		result, err := aiService.ExecuteTask(ctx, task, currentModel)
		if err != nil {
			// Log detailed information about the request when it fails
			log.Printf("WARNING: Task failed with model %s. Error: %v", currentModel, err)
			log.Printf("WARNING: Request details: TaskType=%s, InputSize=%d chars (%d tokens est), Provider=%s",
				task.TaskType, inputChars, inputTokens, modelCapabilities.Provider)

			// If context was canceled, log a specific message
			if ctx.Err() != nil {
				log.Printf("WARNING: Context error detected: %v. This usually means timeout or cancellation.", ctx.Err())
			}

			log.Printf("WARNING: Error executing task with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		if result.Status != AITaskStatusCompleted {
			log.Printf("WARNING: Task with model %s did not complete successfully (Status: %s). Trying fallback.", currentModel, result.Status)
			triedModels = append(triedModels, currentModel)
			continue
		}

		// Extract strict JSON in case provider wrapped it (markdown/code fences, preambles)
		cleanJSON, err := e.extractValidJSON(result.Content)
		if err != nil {
			log.Printf("WARNING: [Raw LLM Response that failed JSON extraction BEGIN]\n%s\n[Raw LLM Response that failed JSON extraction END]", result.Content)
			log.Printf("WARNING: Failed to extract JSON from response with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		parsedResult, err := task.Verifier.VerifyAndParse(cleanJSON)
		if err != nil {
			log.Printf("WARNING: [Raw LLM Response that failed verification BEGIN]\n%s\n[Raw LLM Response that failed verification END]", result.Content)
			log.Printf("WARNING: Failed to verify result with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		// Calculate and save usage metrics
		if result != nil && result.Status == AITaskStatusCompleted {
			inputToks := inputChars / 4
			outputToks := len(result.Content) / 4
			cost, _ := e.modelManager.CalculateProcessingCost(currentModel, inputToks, outputToks)
			e.saveUsageMetric(ctx, task, currentModel, inputToks, outputToks, cost)
		}

		_ = startedAt // для телеметрии
		return parsedResult, nil
	}
}

// ExecuteEmbeddingCalculationTask выполняет задачу по вычислению эмбеддингов
func (e *AITaskExecutor) ExecuteEmbeddingCalculationTask(ctx context.Context, task *AITask) (*AITaskResult, error) {
	inputTokens := len(task.InputContent) / 4

	inputHash := e.computeInputHash(task)
	_ = inputHash // для будущего кэширования

	triedModels := []string{}
	startedAt := time.Now()

	for {
		currentModel := e.modelManager.SelectFallbackModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("All models tried or unavailable for embedding calculation.")
			return &AITaskResult{
				Status:  AITaskStatusFailed,
				Content: "No suitable model found.",
			}, nil
		}

		modelCapabilities, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		if !modelCapabilities.EmbeddingGenerationCapability {
			log.Printf("Model %s does not support embedding generation. Skipping.", currentModel)
			triedModels = append(triedModels, currentModel)
			continue
		}

		aiService, ok := e.aiServices[modelCapabilities.Provider]
		if !ok {
			log.Printf("Provider %s not registered", modelCapabilities.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		result, err := aiService.ExecuteEmbeddingTask(ctx, task.InputContent, currentModel)
		if err != nil {
			log.Printf("Error executing embedding task with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		result.ProcessingTime = time.Since(startedAt)
		return result, nil
	}
}

// ExecuteImageAnalysisTask выполняет задачу по анализу изображения
func (e *AITaskExecutor) ExecuteImageAnalysisTask(ctx context.Context, task *AITask) (*AITaskResult, error) {
	log.Printf("ExecuteImageAnalysisTask called")
	if task.TaskType != TaskTypeImageAnalysis {
		log.Printf("TaskType is not ImageAnalysis. This may not work as expected.")
	}

	var inputData ImageAnalysisInput
	if err := json.Unmarshal([]byte(task.InputContent), &inputData); err != nil {
		return nil, fmt.Errorf("failed to parse ImageAnalysisInput from task.InputContent: %w", err)
	}

	if inputData.UserQuery == "" || inputData.ImageLink == "" {
		return nil, fmt.Errorf("invalid or missing fields in InputContent for image analysis")
	}

	inputTokens := (len(inputData.UserQuery) + len(inputData.ImageLink)) / 4

	inputHash := e.computeInputHash(task)
	_ = inputHash // для будущего кэширования

	triedModels := []string{}
	task.AIRequirements.ImageAnalysisCapability = true
	startedAt := time.Now()

	for {
		currentModel := e.modelManager.SelectFallbackModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("No suitable model found for image analysis.")
			return &AITaskResult{
				Status:  AITaskStatusFailed,
				Content: "No suitable model for image analysis.",
			}, nil
		}

		modelCapabilities, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		if !modelCapabilities.ImageAnalysisCapability {
			log.Printf("Model %s does not support image analysis. Skipping.", currentModel)
			triedModels = append(triedModels, currentModel)
			continue
		}

		aiService, ok := e.aiServices[modelCapabilities.Provider]
		if !ok {
			log.Printf("Provider %s not registered", modelCapabilities.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		log.Printf("Executing image analysis using %s with model %s", modelCapabilities.Provider, currentModel)
		result, err := aiService.ExecuteImageAnalysisByLinkTask(ctx, inputData.UserQuery, inputData.ImageLink, currentModel)
		if err != nil {
			log.Printf("Error executing image analysis with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		result.ProcessingTime = time.Since(startedAt)
		return result, nil
	}
}

// ExecuteSearch выполняет поисковый запрос
func (e *AITaskExecutor) ExecuteSearch(ctx context.Context, task *AITask) (*AITaskResult, error) {
	log.Printf("ExecuteSearch called")
	inputTokens := len(task.InputContent) / 4

	inputHash := e.computeInputHash(task)
	_ = inputHash // для будущего кэширования

	task.AIRequirements.BuiltInSearchCapability = true

	triedModels := []string{}
	startedAt := time.Now()

	for {
		currentModel := e.modelManager.SelectFallbackModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("All models tried or unavailable for search task.")
			return &AITaskResult{
				Status:  AITaskStatusFailed,
				Content: "No suitable model with search capability.",
			}, nil
		}

		modelCapabilities, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		if !modelCapabilities.BuiltInSearchCapability {
			log.Printf("Model %s does not support built-in search. Skipping.", currentModel)
			triedModels = append(triedModels, currentModel)
			continue
		}

		aiService, ok := e.aiServices[modelCapabilities.Provider]
		if !ok {
			log.Printf("Provider %s not registered", modelCapabilities.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		log.Printf("Executing search using %s with model %s", modelCapabilities.Provider, currentModel)
		result, err := aiService.ExecuteSearch(ctx, task.InputContent, currentModel)
		if err != nil {
			log.Printf("Error executing search with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		log.Printf("Search completed. Found %d citations.", len(result.Citations))
		result.ProcessingTime = time.Since(startedAt)
		return result, nil
	}
}

// ExecuteTaskWithTools выполняет задачу с поддержкой MCP/tool-calling
func (e *AITaskExecutor) ExecuteTaskWithTools(ctx context.Context, task *AITask, tools []ToolSchema) (*AITaskResult, error) {
	inputTokens := len(task.InputContent) / 4
	triedModels := []string{}
	startedAt := time.Now()

	for {
		currentModel := e.modelManager.SelectFallbackToolCallingModel(task.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			log.Printf("All models tried or unavailable for ExecuteTaskWithTools.")
			return &AITaskResult{
				Status:  AITaskStatusFailed,
				Content: "No suitable models for tool-calling.",
			}, nil
		}

		caps, err := e.modelManager.GetModelCapabilities(currentModel)
		if err != nil {
			log.Printf("Error getting model capabilities: %v", err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		if !caps.SupportsToolCalling {
			log.Printf("Model %s does not support tool-calling. Trying fallback.", currentModel)
			triedModels = append(triedModels, currentModel)
			continue
		}

		baseSvc, ok := e.aiServices[caps.Provider]
		if !ok {
			log.Printf("Provider %s not registered.", caps.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		svc, ok := baseSvc.(ToolCallingAIService)
		if !ok {
			log.Printf("Provider %s not registered as tool-calling service.", caps.Provider)
			triedModels = append(triedModels, currentModel)
			continue
		}

		step, err := svc.BeginChatWithTools(ctx, task, currentModel, tools)
		if err != nil {
			log.Printf("Error beginning chat with tools with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		safeguard := 0
		iteration := 0
		var callLogs []ToolCallRecord

		for step != nil && !step.IsFinal && safeguard < 8 {
			safeguard++
			if len(step.ToolCalls) == 0 {
				log.Printf("Tool-calling loop: no toolCalls returned, breaking.")
				break
			}

			iteration++
			var execResults []ToolExecutionResult
			for _, call := range step.ToolCalls {
				started := time.Now()
				// Здесь должен быть вызов MCP брокера, но пока заглушка
				resultJSON := fmt.Sprintf(`{"result":"tool %s executed successfully"}`, call.ToolName)
				execResults = append(execResults, ToolExecutionResult{
					ToolName:               call.ToolName,
					CallID:                 call.CallID,
					ResultJSON:             resultJSON,
					RequestedArgumentsJSON: call.ArgumentsJSON,
				})
				callLogs = append(callLogs, ToolCallRecord{
					CallID:        call.CallID,
					ToolName:      call.ToolName,
					ArgumentsJSON: call.ArgumentsJSON,
					ResultJSON:    resultJSON,
					DurationMs:    int(time.Since(started).Milliseconds()),
					Success:       true,
				})
			}

			step, err = svc.ContinueWithToolResults(ctx, step.SessionID, currentModel, execResults)
			if err != nil {
				log.Printf("Error continuing with tool results with model %s. Message: %v. Attempting fallback.", currentModel, err)
				triedModels = append(triedModels, currentModel)
				break
			}
		}

		if err != nil {
			continue
		}

		content := ""
		if step != nil {
			content = step.Content
		}

		result := &AITaskResult{
			Content:            content,
			Model:              currentModel,
			TaskType:           task.TaskType,
			Status:             AITaskStatusCompleted,
			InputTokens:        inputTokens,
			OutputTokens:       max(1, len(content)/4),
			UsedToolCalling:    true,
			ToolUseProtocol:    caps.ToolUseProtocol,
			ToolCallIterations: iteration,
			ToolCalls:          callLogs,
			ProcessingTime:     time.Since(startedAt),
		}

		cost, _ := e.modelManager.CalculateProcessingCost(currentModel, result.InputTokens, result.OutputTokens)
		result.ProcessingCost = cost

		if step != nil && step.IsFinal {
			result.FinishReason = "final_message"
		} else if safeguard >= 8 {
			result.FinishReason = "max_iterations"
		} else {
			result.FinishReason = "no_tool_calls"
		}

		return result, nil
	}
}

// Вспомогательные методы

func (e *AITaskExecutor) computeInputHash(task *AITask) string {
	// Нормализуем контент (удаляем whitespace) для кэширования
	normalized := strings.Join(strings.Fields(task.InputContent), "")
	normalized = strings.ToLower(normalized)

	reqJSON, _ := json.Marshal(task.AIRequirements)
	inputString := fmt.Sprintf("%s:%s:%s", task.TaskType, normalized, string(reqJSON))

	hash := sha256.Sum256([]byte(inputString))
	return hex.EncodeToString(hash[:])
}

func (e *AITaskExecutor) extractValidJSON(responseContent string) (string, error) {
	if strings.TrimSpace(responseContent) == "" {
		return "", fmt.Errorf("response content is null or empty")
	}

	// Try to extract from markdown code fence
	markdownTag := "```json"
	markdownStartIndex := strings.Index(responseContent, markdownTag)
	if markdownStartIndex != -1 {
		effectiveStartIndex := markdownStartIndex + len(markdownTag)
		markdownEndIndex := strings.LastIndex(responseContent, "```")
		if markdownEndIndex > effectiveStartIndex {
			potentialJSON := strings.TrimSpace(responseContent[effectiveStartIndex:markdownEndIndex])
			if json.Valid([]byte(potentialJSON)) {
				return potentialJSON, nil
			}
		}
	}

	// Try to find JSON by braces/brackets
	firstBrace := strings.Index(responseContent, "{")
	firstBracket := strings.Index(responseContent, "[")

	if firstBrace == -1 && firstBracket == -1 {
		return "", fmt.Errorf("no opening brace or bracket found in the response")
	}

	startIndex := firstBrace
	endChar := "}"
	if (firstBracket != -1 && firstBrace == -1) || (firstBracket != -1 && firstBracket < firstBrace) {
		startIndex = firstBracket
		endChar = "]"
	}

	endIndex := strings.LastIndex(responseContent, endChar)
	if endIndex == -1 || endIndex < startIndex {
		return "", fmt.Errorf("no matching closing brace or bracket found")
	}

	jsonSubstring := responseContent[startIndex : endIndex+1]

	if json.Valid([]byte(jsonSubstring)) {
		return jsonSubstring, nil
	}

	// Try to fix common JSON issues
	fixed := e.tryFixJSON(jsonSubstring)
	if json.Valid([]byte(fixed)) {
		return fixed, nil
	}

	return "", fmt.Errorf("invalid JSON structure in the response")
}

func (e *AITaskExecutor) tryFixJSON(jsonStr string) string {
	// Remove trailing commas before } or ]
	re := regexp.MustCompile(`,\s*([}\]])`)
	fixed := re.ReplaceAllString(jsonStr, "$1")

	// Remove comments (both // and /* */)
	re = regexp.MustCompile(`//.*?\n`)
	fixed = re.ReplaceAllString(fixed, "\n")
	re = regexp.MustCompile(`/\*.*?\*/`)
	fixed = re.ReplaceAllString(fixed, "")

	return fixed
}

// saveUsageMetric saves usage metrics if repository is available and task has required metadata
// Note: We use interface{} to avoid circular import with repo package
func (e *AITaskExecutor) saveUsageMetric(_ context.Context, task *AITask, modelName string, inputTokens, outputTokens int, cost float64) {
	if e.usageMetricsRepo == nil {
		return
	}

	// Only save if we have tenantID and userID
	if task.TenantID == "" || task.UserID == "" {
		return
	}

	// Get provider from model name via modelManager
	provider := "unknown"
	if caps, err := e.modelManager.GetModelCapabilities(modelName); err == nil {
		switch caps.Provider {
		case AIProviderGemini:
			provider = "gemini"
		case AIProviderOpenAI:
			provider = "openai"
		case AIProviderClaude:
			provider = "anthropic"
		case AIProviderGrok:
			provider = "grok"
		case AIProviderDeepSeek:
			provider = "deepseek"
		}
	}

	// Prepare taskType and projectID pointers
	var taskTypePtr *string
	taskTypeStr := string(task.TaskType)
	if taskTypeStr != "" {
		taskTypePtr = &taskTypeStr
	}

	var projectIDPtr *string
	if task.Project != "" {
		projectIDPtr = &task.Project
	}

	// Save asynchronously to avoid blocking
	// Use a new context with timeout to avoid cancellation from parent context
	go func() {
		metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := e.usageMetricsRepo.CreateMetric(metricCtx, task.TenantID, task.UserID, provider, modelName, inputTokens, outputTokens, cost, taskTypePtr, projectIDPtr)
		if err != nil {
			log.Printf("[AITaskExecutor] Failed to save usage metric: %v", err)
		}
	}()
}
