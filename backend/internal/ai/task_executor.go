package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// AITaskExecutor центральный оркестратор для выполнения AI-задач.
// Выбирает подходящего провайдера и модель, обрабатывает ошибки,
// управляет кэшированием и обеспечивает отказоустойчивость.
type AITaskExecutor struct {
	aiServices       map[AIProvider]AIService
	modelManager     *AIModelManager
	usageMetricsRepo interface {
		CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int, useCase *string) (string, error)
	} // Wrapper to avoid circular import
	horizonSpendSink interface {
		AddHorizonSpend(ctx context.Context, tenantID, lensID, cascadeID string, amountUSD float64, inputTokens, outputTokens int) error
	}
	providerPressureMu sync.Mutex
	providerPressure   map[AIProvider]providerPressureState
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
		providerPressure: make(map[AIProvider]providerPressureState),
	}
}

// WithUsageMetrics sets the usage metrics repository for tracking AI usage
// repo should be *repo.UsageMetricsRepository but we use interface to avoid circular import
func (e *AITaskExecutor) WithUsageMetrics(repo interface {
	CreateMetric(ctx context.Context, tenantID, userID, provider, model string, inputTokens, outputTokens int, cost float64, taskType, projectID *string, durationMs *int, useCase *string) (string, error)
}) *AITaskExecutor {
	e.usageMetricsRepo = repo
	return e
}

// WithHorizonSpendSink installs a sink that receives per-call cost attribution for Horizon (lens/day + cascade).
func (e *AITaskExecutor) WithHorizonSpendSink(sink interface {
	AddHorizonSpend(ctx context.Context, tenantID, lensID, cascadeID string, amountUSD float64, inputTokens, outputTokens int) error
}) *AITaskExecutor {
	e.horizonSpendSink = sink
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
		currentModel := e.selectFallbackModelAdaptive(task.AIRequirements, inputTokens, triedModels)
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
			e.recordProviderFailure(modelCapabilities.Provider, currentModel, err)
			log.Printf("Error executing task with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}
		e.recordProviderSuccess(modelCapabilities.Provider)

		result.Status = AITaskStatusCompleted
		result.InputTokens = inputTokens
		result.OutputTokens = len(result.Content) / 4
		cost, _ := e.modelManager.CalculateProcessingCost(currentModel, result.InputTokens, result.OutputTokens)
		result.ProcessingCost = cost
		result.ProcessingTime = time.Since(startedAt)

		// Save usage metrics if repository is available
		durationMs := int(result.ProcessingTime.Milliseconds())
		e.saveUsageMetric(ctx, task, currentModel, result.InputTokens, result.OutputTokens, len(result.Content), cost, &durationMs)

		return result, nil
	}
}

// ExecuteStructuredTask выполняет задачу, которая должна вернуть строго типизированный ответ,
// и использует для его проверки верификатор из объекта AITask
func (e *AITaskExecutor) ExecuteStructuredTask(ctx context.Context, task *AITask) (interface{}, error) {
	parsed, _, err := e.ExecuteStructuredTaskWithModel(ctx, task)
	return parsed, err
}

// ExecuteStructuredTaskFull is like ExecuteStructuredTask but also returns the full AITaskResult.
// This is essential for callers that need GroundingEvidence (Vertex AI Search provenance).
func (e *AITaskExecutor) ExecuteStructuredTaskFull(ctx context.Context, task *AITask) (interface{}, *AITaskResult, error) {
	parsed, fullResult, _, err := e.executeStructuredTaskInternal(ctx, task)
	return parsed, fullResult, err
}

// ExecuteStructuredTaskWithModel returns the parsed result and the actual model used.
func (e *AITaskExecutor) ExecuteStructuredTaskWithModel(ctx context.Context, task *AITask) (interface{}, string, error) {
	parsed, _, model, err := e.executeStructuredTaskInternal(ctx, task)
	return parsed, model, err
}

// executeStructuredTaskInternal is the core implementation for structured task execution.
// Returns: parsed result, full AITaskResult (with GroundingEvidence), model used, error.
func (e *AITaskExecutor) executeStructuredTaskInternal(ctx context.Context, task *AITask) (interface{}, *AITaskResult, string, error) {
	log.Printf("ExecuteStructuredTask called with task type: %s", task.TaskType)
	inputChars := len(task.InputContent)
	inputTokens := inputChars / 4 // Approximate token count (4 chars per token)
	startedAt := time.Now()
	debug := aiDebugLogEnabled()

	if task.Verifier == nil {
		return nil, nil, "", fmt.Errorf("ExecuteStructuredTask requires a Verifier in the AITask object, but it was nil")
	}

	activeTask := *task
	triedModels := []string{}
	geminiSearchGroundingFallbackApplied := false

	for {
		attempt := len(triedModels) + 1

		currentModel := e.selectFallbackModelAdaptive(activeTask.AIRequirements, inputTokens, triedModels)
		if currentModel == "" {
			if !geminiSearchGroundingFallbackApplied && e.enableGeminiSearchGroundingFallback(&activeTask, inputChars) {
				geminiSearchGroundingFallbackApplied = true
				triedModels = nil // New fallback phase: restart attempts with downgraded requirements.
				continue
			}
			log.Printf("ERROR: All models tried or unavailable for structured task '%s'. Total models tried: %d, Input size: %d chars (%d tokens est)",
				activeTask.TaskType, len(triedModels), inputChars, inputTokens)
			return nil, nil, "", fmt.Errorf("unable to complete structured task '%s': No suitable AI models available after all fallbacks (windowChars=%d)", activeTask.TaskType, inputChars)
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

		deadlineStr := "none"
		remainingStr := "n/a"
		if d, ok := ctx.Deadline(); ok {
			deadlineStr = d.Format(time.RFC3339Nano)
			remainingStr = time.Until(d).String()
		}

		log.Printf("[%s] Executing structured task with model %s (attempt=%d input: %d chars, %d tokens est, ctxDeadline=%s, remaining=%s)",
			modelCapabilities.Provider, currentModel, attempt, inputChars, inputTokens, deadlineStr, remainingStr)

		attemptStartedAt := time.Now()
		result, err := aiService.ExecuteTask(ctx, &activeTask, currentModel)
		if err != nil {
			e.recordProviderFailure(modelCapabilities.Provider, currentModel, err)
			// Log detailed information about the request when it fails
			elapsed := time.Since(attemptStartedAt)
			log.Printf("WARNING: Task failed with model %s after %s (task=%s provider=%s attempt=%d input=%d chars %d tokens est). Error: %v",
				currentModel, elapsed, activeTask.TaskType, modelCapabilities.Provider, attempt, inputChars, inputTokens, err)

			// If context was canceled, log a specific message
			if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				log.Printf("WARNING: Context error detected: ctxErr=%v ctxDeadline=%s remaining=%s (this usually means timeout or cancellation)",
					ctx.Err(), deadlineStr, remainingStr)
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
		e.recordProviderSuccess(modelCapabilities.Provider)

		if debug && result != nil {
			out := strings.TrimSpace(result.Content)
			log.Printf("[AI DEBUG] Structured task raw response received (task=%s provider=%s model=%s outputChars=%d sha=%s)\n%s",
				task.TaskType,
				modelCapabilities.Provider,
				currentModel,
				len(out),
				shortHash(out),
				truncateForLog(out, 6000),
			)
		}

		// Extract strict JSON in case provider wrapped it (markdown/code fences, preambles)
		cleanJSON, err := e.extractValidJSON(result.Content)
		if err != nil {
			if debug {
				log.Printf("WARNING: [Raw LLM Response that failed JSON extraction BEGIN]\n%s\n[Raw LLM Response that failed JSON extraction END]",
					truncateForLog(result.Content, 6000))
			} else {
				log.Printf("WARNING: LLM response failed JSON extraction (task=%s provider=%s model=%s outputChars=%d sha=%s): %v",
					task.TaskType, modelCapabilities.Provider, currentModel, len(result.Content), shortHash(result.Content), err)
			}
			log.Printf("WARNING: Failed to extract JSON from response with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		parsedResult, err := task.Verifier.VerifyAndParse(cleanJSON)
		if err != nil {
			if debug {
				log.Printf("WARNING: [Raw LLM Response that failed verification BEGIN]\n%s\n[Raw LLM Response that failed verification END]",
					truncateForLog(result.Content, 6000))
			} else {
				log.Printf("WARNING: LLM response failed verification (task=%s provider=%s model=%s outputChars=%d sha=%s): %v",
					task.TaskType, modelCapabilities.Provider, currentModel, len(result.Content), shortHash(result.Content), err)
			}
			log.Printf("WARNING: Failed to verify result with model %s. Message: %v. Attempting fallback.", currentModel, err)
			triedModels = append(triedModels, currentModel)
			continue
		}

		// Calculate and save usage metrics
		if result != nil && result.Status == AITaskStatusCompleted {
			inputToks := inputChars / 4
			outputToks := len(result.Content) / 4
			cost, _ := e.modelManager.CalculateProcessingCost(currentModel, inputToks, outputToks)
			durationMs := int(time.Since(startedAt).Milliseconds())
			e.saveUsageMetric(ctx, &activeTask, currentModel, inputToks, outputToks, len(result.Content), cost, &durationMs)
		}

		_ = startedAt // для телеметрии
		return parsedResult, result, currentModel, nil
	}
}

// enableGeminiSearchGroundingFallback downgrades strict internal-grounding tasks to
// external search grounding on Gemini when all internal-grounding models are exhausted.
func (e *AITaskExecutor) enableGeminiSearchGroundingFallback(task *AITask, inputChars int) bool {
	if task == nil {
		return false
	}
	req := task.AIRequirements
	if !req.InternalDataGroundingCapability {
		return false
	}
	if _, ok := e.aiServices[AIProviderGemini]; !ok {
		log.Printf("WARNING: Gemini search-grounding fallback skipped for task '%s': Gemini provider is not registered",
			task.TaskType)
		return false
	}

	req.InternalDataGroundingCapability = false
	req.SearchGroundingCapability = true
	req.BuiltInSearchCapability = false
	if pm := strings.TrimSpace(req.PreferredModel); pm == "" || strings.HasPrefix(pm, "vertex-") {
		req.PreferredModel = "gemini-3-flash-preview"
	}
	task.AIRequirements = req

	log.Printf("WARNING: Applying Gemini fallback for structured task '%s': downgraded capabilities to search grounding (windowChars=%d, preferredModel=%s)",
		task.TaskType, inputChars, task.AIRequirements.PreferredModel)
	return true
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
		currentModel := e.selectFallbackToolModelAdaptive(task.AIRequirements, inputTokens, triedModels)
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
			e.recordProviderFailure(caps.Provider, currentModel, err)
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
				e.recordProviderFailure(caps.Provider, currentModel, err)
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
		e.recordProviderSuccess(caps.Provider)

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

// mapTaskTypeToUseCase maps TaskType to business use case category
func mapTaskTypeToUseCase(taskType TaskType) string {
	switch taskType {
	// Chat and user interactions
	case TaskTypeContentGeneration, TaskTypeNarrativeGeneration:
		return "chat"

	// Bias analysis
	case TaskTypeEthicsValidation, TaskTypeNecessityValidation, TaskTypeStrategicValidation:
		return "bias_analysis"

	// Frame decomposition
	case TaskTypeActionFrameDecomposition:
		return "frame_decomposition"

	// Source processing
	case TaskTypeSummarization, TaskTypeContentAnalysis, TaskTypeTopicSuggestion,
		TaskTypeTrendDetection, TaskTypeAnomalyDetection, TaskTypeCrossDomainInsight,
		TaskTypePatternEnrichment, TaskTypeContentEnhancement, TaskTypePatternAnalysis,
		TaskTypeInsightSynthesis, TaskTypeCrossThemeAnalysis, TaskTypeSignalVerification,
		TaskTypeInquiryAnalysis, TaskTypeZoneRefinement, TaskTypeUncertaintyResolution:
		return "source_processing"

	// Document processing
	case TaskTypeEntityExtraction, TaskTypeArtifactExtraction, TaskTypeOntologyMapping,
		TaskTypeOntologyNodeSuggestion, TaskTypeDomainClassification, TaskTypeReportPlanning,
		TaskTypeKnowledgeSynthesis, TaskTypeContextPlacement, TaskTypeContextInitiation,
		TaskTypeContextPassport, TaskTypeTextProcessing, TaskTypeConsoleInterpretation,
		TaskTypeEntityExtractionForConn, TaskTypeContextTitleRefresh, TaskTypeContextDeduplication,
		TaskTypeIntraEventLinking, TaskTypeContextBriefing, TaskTypeCausalLinkClassification,
		TaskTypeBatchCausalLinkClass, TaskTypeContextAnalysis, TaskTypeSeverityAnalysis,
		TaskTypeCountryCodeMapping, TaskTypeConnectionStrengthAnalysis, TaskTypeAliasEnrichment:
		return "document_processing"

	// Search
	case TaskTypeSearch:
		return "search"

	// Embeddings
	case TaskTypeEmbeddingCalculation:
		return "embedding_calculation"

	// Image analysis
	case TaskTypeImageAnalysis:
		return "image_analysis"

	// Default fallback
	default:
		return "other"
	}
}

// saveUsageMetric saves usage metrics if repository is available and task has required metadata
// Note: We use interface{} to avoid circular import with repo package
func (e *AITaskExecutor) saveUsageMetric(ctx context.Context, task *AITask, modelName string, inputTokens, outputTokens int, outputChars int, cost float64, durationMs *int) {
	if e.usageMetricsRepo == nil {
		return
	}

	tenantID := strings.TrimSpace(task.TenantID)
	userID := strings.TrimSpace(task.UserID)
	projectID := strings.TrimSpace(task.Project)
	if tenantID == "" {
		tenantID = usageTenantFromContext(ctx)
	}
	if userID == "" {
		userID = usageUserFromContext(ctx)
	}
	if projectID == "" {
		projectID = usageProjectFromContext(ctx)
	}
	// Only save if we have tenantID and userID
	if tenantID == "" || userID == "" {
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
	if projectID != "" {
		projectIDPtr = &projectID
	}

	// Map TaskType to UseCase
	useCaseStr := mapTaskTypeToUseCase(task.TaskType)
	if strings.TrimSpace(task.UseCase) != "" {
		useCaseStr = strings.TrimSpace(task.UseCase)
	} else if uc := usageUseCaseFromContext(ctx); uc != "" {
		useCaseStr = uc
	}
	var useCasePtr *string
	if useCaseStr != "" {
		useCasePtr = &useCaseStr
	}

	// Save asynchronously to avoid blocking
	// Use a new context with timeout to avoid cancellation from parent context.
	// 30s allows for Firestore latency under load or cold start (10s was too short).
	go func() {
		metricCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := e.usageMetricsRepo.CreateMetric(metricCtx, tenantID, userID, provider, modelName, inputTokens, outputTokens, cost, taskTypePtr, projectIDPtr, durationMs, useCasePtr)
		if err != nil {
			log.Printf("[AITaskExecutor] Failed to save usage metric: %v", err)
		}
		// Best-effort: update Horizon spend ledger (if context includes lens/cascade ids).
		if e.horizonSpendSink != nil {
			lensID := usageLensFromContext(ctx)
			cascadeID := usageCascadeFromContext(ctx)
			if lensID != "" || cascadeID != "" {
				// Safety: if token counts are zero, fall back to char-based estimates (/4).
				it := inputTokens
				ot := outputTokens
				if it <= 0 && task != nil {
					it = len(task.InputContent) / 4
				}
				if ot <= 0 && outputChars > 0 {
					ot = outputChars / 4
				}
				_ = e.horizonSpendSink.AddHorizonSpend(metricCtx, tenantID, lensID, cascadeID, cost, it, ot)
			}
		}
	}()
}
