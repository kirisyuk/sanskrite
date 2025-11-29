package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// PlanGenerateExecutor генерирует план выполнения из естественного языка
type PlanGenerateExecutor struct {
	genaiClient        *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger             *log.Logger
}

// NewPlanGenerateExecutor создает новый исполнитель для генерации планов
func NewPlanGenerateExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *PlanGenerateExecutor {
	return &PlanGenerateExecutor{
		genaiClient:        genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:             logger,
	}
}

func (e *PlanGenerateExecutor) Capability() Capability {
	return CapabilityPlanGenerate
}

func (e *PlanGenerateExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["userQuery"].(string); !ok {
		return fmt.Errorf("userQuery is required and must be a string")
	}
	return nil
}

func (e *PlanGenerateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	userQuery := stepCtx.Input["userQuery"].(string)

	// Получить список доступных capabilities
	availableCapabilities := e.capabilityRegistry.List()
	capabilityNames := make([]string, 0, len(availableCapabilities))
	for _, cap := range availableCapabilities {
		capabilityNames = append(capabilityNames, string(cap))
	}

	// Контекст проекта (если есть)
	contextInfo := ""
	if ctxInfo, ok := stepCtx.Input["context"].(map[string]interface{}); ok {
		if ctxJSON, err := json.Marshal(ctxInfo); err == nil {
			contextInfo = string(ctxJSON)
		}
	}

	if e.logger != nil {
		e.logger.Printf("[PlanGenerateExecutor] Generating plan for query: %s", userQuery)
	}

	// JSON Schema для плана
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"intent": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":          map[string]interface{}{"type": "string"},
					"target":          map[string]interface{}{"type": "string"},
					"expectedOutcome": map[string]interface{}{"type": "string"},
				},
				"required": []string{"action", "target", "expectedOutcome"},
			},
			"goal": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{"type": "string"},
					"successCriteria": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"description", "successCriteria"},
			},
			"steps": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":         map[string]interface{}{"type": "string"},
						"capability": map[string]interface{}{"type": "string"},
						"input":      map[string]interface{}{"type": "object"},
						"dependsOn": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"reason":     map[string]interface{}{"type": "string"},
						"isCritical": map[string]interface{}{"type": "boolean"},
					},
					"required": []string{"id", "capability", "input", "reason"},
				},
			},
		},
		"required": []string{"intent", "goal", "steps"},
	}

	prompt := fmt.Sprintf(`You are a task planner for an agentic system. Your task is to create an execution plan for a user's request.

User Request: "%s"

Available Capabilities (tools):
%s

Project Context:
%s

Create an execution plan that includes:
1. Intent Understanding: What the user wants to accomplish, what resources to work with, and what outcome is expected
2. Goal Definition: Description of the desired result and success criteria
3. Execution Steps: A sequence of steps using available capabilities

Rules and Guidelines:
- Each step must use one of the available capabilities
- Specify dependencies between steps using dependsOn (IDs of previous steps)
- Critical steps (isCritical: true) are those whose failure prevents task completion (e.g., document read/write operations)
- Non-critical steps can be skipped on error without stopping the entire task
- Step IDs must be unique (e.g., step1, step2, step3)
- In the "reason" field, explain why this step is needed
- For searching documents within the project, use data_search (searches project documents), NOT web_search (searches the internet)
- If you need to find a document in the project, use data_search with a query in the input field
- CRITICAL: NEVER use placeholder values like "doc-id-placeholder", "doc-id-from-step1", "placeholder", "summary-doc-id", etc. - these will FAIL
- If a step depends on previous steps that haven't completed yet, you CANNOT provide values - DO NOT create that step
- If data_search is used, subsequent document_read steps MUST wait for data_search to complete first (use dependsOn)
- If data_search hasn't completed yet, DO NOT create document_read steps - they will fail
- Consider the logical flow: search → read → process → write
- Only create steps where you can provide REAL, CONCRETE values - not placeholders, not empty strings, not "placeholder"
- If you cannot provide real values, DO NOT create the step - it will fail immediately

Important Capability Notes:
- data_search: Searches documents within the project. Input: {"searchQuery": "query text", "limit": number}. Output: {"matches": [...], "documentIds": ["docId1", "docId2"], "count": N} - use "documentIds" array for document_read
- document_read: Reads document content. Input: {"documentIds": ["id1", "id2"]} - requires Document IDs from data_search output's "documentIds" array. Returns content of the document.
- document_write: Writes/updates document content. Input: {"documentId": "id", "content": "text"}. Creates document if it doesn't exist.
- llm_generate: Generates text using LLM. Input: {"prompt": "text"}. Output: {"content": "generated text"}
- llm_analyze: Performs structured analysis. Input: {"text": "content", "analysisType": "type"}. Output: {"analysis": {...}}
- web_search: Searches the internet (use only when external information is needed)
- bigquery_query: Executes SQL queries against BigQuery for analytics. Input: {"sql": "SELECT...", "maxResults": 1000}. Output: {"results": [...], "rowCount": N}
  BigQuery tables: core_docs_metadata (id, tenantId, projectId, filename, extractedText), documents_metadata
  
  For CSV analysis, parse extractedText (tab-separated values):
  - Count rows: SELECT ARRAY_LENGTH(SPLIT(extractedText, '\n')) as row_count FROM core_docs_metadata WHERE filename='data.csv'
  - Parse CSV columns: SELECT SPLIT(line, '\t')[SAFE_OFFSET(0)] as col1, SPLIT(line, '\t')[SAFE_OFFSET(1)] as col2 FROM core_docs_metadata, UNNEST(SPLIT(extractedText, '\n')) as line WHERE filename='data.csv' LIMIT 10
  - Aggregate: Use SAFE_CAST to convert text to numbers for SUM/AVG

CRITICAL: When referencing outputs from previous steps, use the EXACT field names from the output:
- For data_search output, use: ${stepId.documentIds[0]} to get first document ID (NOT ${stepId.output.documentIds})
- For document_read output, use: ${stepId.documents[0].content} to get content from first document
- For llm_generate output, use: ${stepId.content} to get generated text
- Always use direct field access, NOT nested "output" wrapper

IMPORTANT: You MUST return ONLY valid JSON. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON with fields: intent, goal, steps.`,
		userQuery,
		formatCapabilities(capabilityNames),
		contextInfo,
	)

	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// GenerateJSONStrict должен возвращать строгий JSON
	// Для Gemini это может быть в ToolCalls с именем "return_structured"
	jsonContent := result.Content
	
	// Если Content пустой или не начинается с JSON, проверяем ToolCalls (для Gemini strict JSON mode)
	if (jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{')) && len(result.ToolCalls) > 0 {
		for _, tc := range result.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				jsonContent = tc.Arguments
				if e.logger != nil {
					e.logger.Printf("[PlanGenerateExecutor] Extracted JSON from return_structured tool call")
				}
				break
			}
		}
	}
	
	// Если все еще пусто или не JSON, используем RawJSON
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{') {
		if result.RawJSON != "" {
			jsonContent = result.RawJSON
		}
	}
	
	// Убираем markdown code fences если есть (на случай если LLM все же добавил их)
	jsonContent = cleanJSONContent(jsonContent)
	
	// Попытка извлечь JSON из текста, если Content начинается с текста
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{') {
		// Ищем JSON объект в тексте
		if idx := strings.Index(result.Content, "{"); idx != -1 {
			// Находим последнюю закрывающую скобку
			lastIdx := strings.LastIndex(result.Content, "}")
			if lastIdx > idx {
				jsonContent = result.Content[idx : lastIdx+1]
				if e.logger != nil {
					e.logger.Printf("[PlanGenerateExecutor] Extracted JSON from text content (idx: %d-%d)", idx, lastIdx+1)
				}
			}
		}
	}
	
	// Если JSON содержит "candidates" wrapper от Gemini API, извлекаем текст из него
	if strings.Contains(jsonContent, `"candidates"`) && strings.Contains(jsonContent, `"parts"`) {
		// Попробуем распарсить как Gemini response wrapper
		var geminiWrapper struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(jsonContent), &geminiWrapper); err == nil {
			if len(geminiWrapper.Candidates) > 0 && len(geminiWrapper.Candidates[0].Content.Parts) > 0 {
				extractedText := geminiWrapper.Candidates[0].Content.Parts[0].Text
				// Очищаем markdown code fences
				extractedText = strings.TrimSpace(extractedText)
				if strings.HasPrefix(extractedText, "```json") {
					extractedText = strings.TrimPrefix(extractedText, "```json")
					extractedText = strings.TrimSuffix(extractedText, "```")
					extractedText = strings.TrimSpace(extractedText)
				} else if strings.HasPrefix(extractedText, "```") {
					extractedText = strings.TrimPrefix(extractedText, "```")
					extractedText = strings.TrimSuffix(extractedText, "```")
					extractedText = strings.TrimSpace(extractedText)
				}
				jsonContent = extractedText
				if e.logger != nil {
					e.logger.Printf("[PlanGenerateExecutor] Extracted JSON from Gemini candidates wrapper")
				}
			}
		}
	}
	
	if jsonContent == "" {
		return nil, fmt.Errorf("GenerateJSONStrict returned empty response (Content, ToolCalls, and RawJSON all empty)")
	}

	// Строгая типизация для плана
	type PlanIntent struct {
		Action          string `json:"action"`
		Target          string `json:"target"`
		ExpectedOutcome string `json:"expectedOutcome"`
	}

	type PlanGoal struct {
		Description     string   `json:"description"`
		SuccessCriteria []string `json:"successCriteria"`
	}

	type PlanStep struct {
		ID         string                 `json:"id"`
		Capability string                 `json:"capability"`
		Input      map[string]interface{} `json:"input"`
		DependsOn  []string               `json:"dependsOn,omitempty"`
		Reason     string                 `json:"reason"`
		IsCritical bool                   `json:"isCritical"`
	}

	type PlanResponse struct {
		Intent PlanIntent `json:"intent"`
		Goal   PlanGoal   `json:"goal"`
		Steps  []PlanStep `json:"steps"`
	}

	var plan PlanResponse
	if err := json.Unmarshal([]byte(jsonContent), &plan); err != nil {
		if e.logger != nil {
			previewLen := min(500, len(jsonContent))
			e.logger.Printf("[PlanGenerateExecutor] Failed to parse plan JSON: %v, content: %s", err, jsonContent[:previewLen])
			e.logger.Printf("[PlanGenerateExecutor] RawJSON: %s", result.RawJSON[:min(500, len(result.RawJSON))])
		}
		return nil, fmt.Errorf("failed to parse plan JSON from GenerateJSONStrict response: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[PlanGenerateExecutor] Parsed plan: intent.action=%s, goal.description=%s, steps count=%d",
			plan.Intent.Action, plan.Goal.Description, len(plan.Steps))
		if len(plan.Steps) == 0 {
			e.logger.Printf("[PlanGenerateExecutor] WARNING: No steps generated! JSON content: %s", jsonContent[:min(1000, len(jsonContent))])
		}
	}

	// Валидировать, что все capabilities существуют
	for _, step := range plan.Steps {
		if _, err := e.capabilityRegistry.Get(Capability(step.Capability)); err != nil {
			return nil, fmt.Errorf("invalid capability '%s' in step %s: %w", step.Capability, step.ID, err)
		}
	}

	// Конвертировать в формат для output ([]interface{} для совместимости)
	stepsOutput := make([]interface{}, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		stepMap := map[string]interface{}{
			"id":         step.ID,
			"capability": step.Capability,
			"input":      step.Input,
			"reason":     step.Reason,
			"isCritical": step.IsCritical,
		}
		if len(step.DependsOn) > 0 {
			stepMap["dependsOn"] = step.DependsOn
		}
		stepsOutput = append(stepsOutput, stepMap)
	}

	// Конвертировать intent и goal в map[string]interface{} для output
	intentMap := map[string]interface{}{
		"action":          plan.Intent.Action,
		"target":          plan.Intent.Target,
		"expectedOutcome": plan.Intent.ExpectedOutcome,
	}
	goalMap := map[string]interface{}{
		"description":     plan.Goal.Description,
		"successCriteria": plan.Goal.SuccessCriteria,
	}

	confidence := 0.9 // Высокая уверенность в сгенерированном плане
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"intent": intentMap,
			"goal":   goalMap,
			"steps":  stepsOutput,
		},
		Message:         fmt.Sprintf("Generated plan with %d steps", len(plan.Steps)),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          "Plan generated successfully from user query",
	}, nil
}

// formatCapabilities форматирует список capabilities для промпта
func formatCapabilities(caps []string) string {
	result := ""
	for i, cap := range caps {
		result += fmt.Sprintf("- %s\n", cap)
		if i >= 20 { // Ограничиваем до 20 для промпта
			result += fmt.Sprintf("... и еще %d capabilities\n", len(caps)-20)
			break
		}
	}
	return result
}

// cleanJSONContent убирает markdown code fences из JSON
func cleanJSONContent(content string) string {
	content = trimSpace(content)
	content = trimPrefix(content, "```json")
	content = trimPrefix(content, "```")
	content = trimSuffix(content, "```")
	return trimSpace(content)
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// ReplanExecutor корректирует план на основе результатов выполнения
type ReplanExecutor struct {
	genaiClient        *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger             *log.Logger
}

// NewReplanExecutor создает новый исполнитель для репланирования
func NewReplanExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *ReplanExecutor {
	return &ReplanExecutor{
		genaiClient:        genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:             logger,
	}
}

func (e *ReplanExecutor) Capability() Capability {
	return CapabilityReplan
}

func (e *ReplanExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["currentPlan"].(map[string]interface{}); !ok {
		return fmt.Errorf("currentPlan is required")
	}
	if _, ok := input["completedSteps"].([]interface{}); !ok {
		return fmt.Errorf("completedSteps is required")
	}
	return nil
}

func (e *ReplanExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	currentPlan := stepCtx.Input["currentPlan"].(map[string]interface{})
	completedStepsRaw := stepCtx.Input["completedSteps"].([]interface{})
	failedStepsRaw := []interface{}{}
	if fs, ok := stepCtx.Input["failedSteps"].([]interface{}); ok {
		failedStepsRaw = fs
	}
	currentGoal := stepCtx.Input["currentGoal"].(map[string]interface{})

	// Получить список доступных capabilities
	availableCapabilities := e.capabilityRegistry.List()
	capabilityNames := make([]string, 0, len(availableCapabilities))
	for _, cap := range availableCapabilities {
		capabilityNames = append(capabilityNames, string(cap))
	}

	if e.logger != nil {
		e.logger.Printf("[ReplanExecutor] Replanning based on %d completed steps, %d failed steps",
			len(completedStepsRaw), len(failedStepsRaw))
	}

	// JSON Schema для реплана
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"updatedPlan": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"steps": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":         map[string]interface{}{"type": "string"},
								"capability": map[string]interface{}{"type": "string"},
								"input":      map[string]interface{}{"type": "object"},
								"dependsOn": map[string]interface{}{
									"type":  "array",
									"items": map[string]interface{}{"type": "string"},
								},
								"reason":     map[string]interface{}{"type": "string"},
								"isCritical": map[string]interface{}{"type": "boolean"},
							},
							"required": []string{"id", "capability", "input", "reason"},
						},
					},
					"reason": map[string]interface{}{"type": "string"},
				},
				"required": []string{"steps", "reason"},
			},
			"goalAchieved": map[string]interface{}{"type": "boolean"},
			"nextSteps": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		"required": []string{"updatedPlan", "goalAchieved"},
	}

	// Сериализовать входные данные для промпта
	planJSON, _ := json.MarshalIndent(currentPlan, "", "  ")
	completedJSON, _ := json.MarshalIndent(completedStepsRaw, "", "  ")
	failedJSON, _ := json.MarshalIndent(failedStepsRaw, "", "  ")
	goalJSON, _ := json.MarshalIndent(currentGoal, "", "  ")

	prompt := fmt.Sprintf(`You are a task planner for an agentic system. Your task is to revise an execution plan based on step execution results.

Current Plan:
%s

Completed Steps (with results):
%s

Failed Steps:
%s

Target Goal:
%s

Available Capabilities:
%s

Analyze the situation and:
1. Determine if the goal has been achieved (goalAchieved: true/false)
2. If the goal is not achieved, create an updated plan (updatedPlan) with new steps
3. Specify the reason for changes (reason in updatedPlan)
4. List the IDs of next steps to execute (nextSteps array)

Capability Notes:
- bigquery_query: Executes SQL against BigQuery for analytics. Input: {"sql": "SELECT...", "maxResults": 1000}
  BigQuery tables: core_docs_metadata (id, tenantId, projectId, filename, extractedText), documents_metadata
  Use for data analysis, aggregations, and complex queries on CSV/structured data.

Rules and Guidelines:
- Use only available capabilities from the list above
- CRITICAL: Extract ACTUAL values from completed steps' outputs - look at the output JSON structure
- When a step fails, analyze WHY it failed and propose a concrete alternative approach
- New step IDs must be unique and sequential (e.g., step4, step5, step6)
- Specify dependencies using dependsOn array
- For data analysis on CSV files, consider using bigquery_query to query the extractedText from core_docs_metadata table
- ABSOLUTELY FORBIDDEN: NEVER use placeholder values like "doc-id-placeholder", "doc-id-from-step1", "placeholder", "summary-doc-id-placeholder", "doc-id", etc. - these will FAIL IMMEDIATELY
- Extract real data from completed steps: if data_search output has "documentIds" array with values, use those exact IDs in document_read step
- IMPORTANT: data_search returns "documentIds" array containing Document IDs - use these IDs directly in document_read step's "documentIds" field
- CRITICAL: When referencing step outputs, use DIRECT field access: ${stepId.field} NOT ${stepId.output.field}
  - For data_search: ${stepId.documentIds[0]} to get first document ID
  - For document_read: ${stepId.documents[0].content} to get content
  - For llm_generate: ${stepId.content} to get generated text
  - For bigquery_query: ${stepId.results} to get query results array
- CRITICAL: If data_search completed but found 0 documents (check "count": 0 or empty "documentIds" array or empty "matches" array), DO NOT create document_read steps AT ALL
- If data_search found 0 documents, you have two options:
  1. Try a different search query with data_search (use different keywords, broader terms)
  2. If goal cannot be achieved without documents, set goalAchieved: false and explain why in updatedPlan.reason
- NEVER create steps with placeholder values - they will fail immediately and waste resources
- If you cannot extract real values from completed steps, DO NOT create steps that require those values - skip them entirely
- Look at the actual output structure of completed steps in the "Completed Steps" section above - check the JSON structure carefully
- Example: if completed step1 (data_search) has output: {"count": 0, "documentIds": []}, then DO NOT create document_read steps - there are no documents to read

IMPORTANT: You MUST return ONLY valid JSON. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON with fields: updatedPlan (object with steps array and reason string), goalAchieved (boolean), nextSteps (array of step IDs).`,
		string(planJSON),
		string(completedJSON),
		string(failedJSON),
		string(goalJSON),
		formatCapabilities(capabilityNames),
	)

	// Используем строгий JSON режим с низкой температурой для детерминированного репланирования
	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{
		Temperature: 0.3, // Низкая температура для более детерминированного репланирования
	})
	if err != nil {
		return nil, fmt.Errorf("failed to replan: %w", err)
	}

	// GenerateJSONStrict должен возвращать строгий JSON
	// Для Gemini это может быть в ToolCalls с именем "return_structured"
	jsonContent := result.Content
	
	// Если Content пустой, проверяем ToolCalls (для Gemini strict JSON mode)
	if jsonContent == "" && len(result.ToolCalls) > 0 {
		for _, tc := range result.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				jsonContent = tc.Arguments
				if e.logger != nil {
					e.logger.Printf("[ReplanExecutor] Extracted JSON from return_structured tool call")
				}
				break
			}
		}
	}
	
	// Если все еще пусто, используем RawJSON
	if jsonContent == "" {
		jsonContent = result.RawJSON
	}
	
	jsonContent = cleanJSONContent(jsonContent)

	// Строгая типизация для реплана
	type ReplanStep struct {
		ID         string                 `json:"id"`
		Capability string                 `json:"capability"`
		Input      map[string]interface{} `json:"input"`
		DependsOn  []string               `json:"dependsOn,omitempty"`
		Reason     string                 `json:"reason"`
		IsCritical bool                   `json:"isCritical"`
	}

	type UpdatedPlan struct {
		Steps  []ReplanStep `json:"steps"`
		Reason string       `json:"reason"`
	}

	type ReplanResponse struct {
		UpdatedPlan  UpdatedPlan `json:"updatedPlan"`
		GoalAchieved bool        `json:"goalAchieved"`
		NextSteps    []string    `json:"nextSteps,omitempty"`
	}

	var replan ReplanResponse
	if err := json.Unmarshal([]byte(jsonContent), &replan); err != nil {
		if e.logger != nil {
			// Показываем больше контекста для отладки
			previewLen := min(1000, len(jsonContent))
			e.logger.Printf("[ReplanExecutor] Failed to parse replan JSON: %v", err)
			e.logger.Printf("[ReplanExecutor] JSON content preview (first %d chars): %s", previewLen, jsonContent[:previewLen])
			// Пытаемся найти JSON в тексте, если LLM вернул текст с JSON внутри
			if strings.Contains(jsonContent, "{") && strings.Contains(jsonContent, "}") {
				// Пытаемся извлечь JSON из текста
				startIdx := strings.Index(jsonContent, "{")
				endIdx := strings.LastIndex(jsonContent, "}")
				if startIdx < endIdx {
					jsonExtracted := jsonContent[startIdx : endIdx+1]
					e.logger.Printf("[ReplanExecutor] Attempting to extract JSON from text...")
					if err2 := json.Unmarshal([]byte(jsonExtracted), &replan); err2 == nil {
						e.logger.Printf("[ReplanExecutor] Successfully extracted JSON from text")
					} else {
						return nil, fmt.Errorf("failed to parse replan JSON (original error: %v, extraction error: %v): %s", err, err2, jsonContent[:previewLen])
					}
				} else {
					return nil, fmt.Errorf("failed to parse replan JSON: %w, content preview: %s", err, jsonContent[:previewLen])
				}
			} else {
				return nil, fmt.Errorf("failed to parse replan JSON: %w, content preview: %s", err, jsonContent[:previewLen])
			}
		} else {
			return nil, fmt.Errorf("failed to parse replan: %w", err)
		}
	}

	// Валидировать capabilities
	for _, step := range replan.UpdatedPlan.Steps {
		if _, err := e.capabilityRegistry.Get(Capability(step.Capability)); err != nil {
			return nil, fmt.Errorf("invalid capability '%s' in step %s: %w", step.Capability, step.ID, err)
		}
	}

	// Конвертировать в формат для output ([]interface{} для совместимости)
	stepsOutput := make([]interface{}, 0, len(replan.UpdatedPlan.Steps))
	for _, step := range replan.UpdatedPlan.Steps {
		stepMap := map[string]interface{}{
			"id":         step.ID,
			"capability": step.Capability,
			"input":      step.Input,
			"reason":     step.Reason,
			"isCritical": step.IsCritical,
		}
		if len(step.DependsOn) > 0 {
			stepMap["dependsOn"] = step.DependsOn
		}
		stepsOutput = append(stepsOutput, stepMap)
	}

	confidence := 0.85
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"updatedPlan": map[string]interface{}{
				"steps":  stepsOutput,
				"reason": replan.UpdatedPlan.Reason,
			},
			"goalAchieved": replan.GoalAchieved,
			"nextSteps":    replan.NextSteps,
		},
		Message:         fmt.Sprintf("Replanned: %s", replan.UpdatedPlan.Reason),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          replan.UpdatedPlan.Reason,
	}, nil
}

// ReflectExecutor оценивает прогресс относительно цели
type ReflectExecutor struct {
	genaiClient *gen_combined.Client
	logger      *log.Logger
}

// NewReflectExecutor создает новый исполнитель для рефлексии
func NewReflectExecutor(
	genaiClient *gen_combined.Client,
	logger *log.Logger,
) *ReflectExecutor {
	return &ReflectExecutor{
		genaiClient: genaiClient,
		logger:      logger,
	}
}

func (e *ReflectExecutor) Capability() Capability {
	return CapabilityReflect
}

func (e *ReflectExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["goal"].(map[string]interface{}); !ok {
		return fmt.Errorf("goal is required")
	}
	if _, ok := input["completedSteps"].([]interface{}); !ok {
		return fmt.Errorf("completedSteps is required")
	}
	return nil
}

func (e *ReflectExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	goal := stepCtx.Input["goal"].(map[string]interface{})
	completedStepsRaw := stepCtx.Input["completedSteps"].([]interface{})
	currentState := map[string]interface{}{}
	if cs, ok := stepCtx.Input["currentState"].(map[string]interface{}); ok {
		currentState = cs
	}

	if e.logger != nil {
		e.logger.Printf("[ReflectExecutor] Reflecting on progress with %d completed steps", len(completedStepsRaw))
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"progress":     map[string]interface{}{"type": "number"}, // 0.0-1.0
			"goalAchieved": map[string]interface{}{"type": "boolean"},
			"missing": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"suggestions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{"type": "string"}, // "add_step" | "replan" | "retry"
						"step": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":         map[string]interface{}{"type": "string"},
								"capability": map[string]interface{}{"type": "string"},
								"input":      map[string]interface{}{"type": "object"},
								"reason":     map[string]interface{}{"type": "string"},
							},
						},
						"reason": map[string]interface{}{"type": "string"},
					},
					"required": []string{"action", "reason"},
				},
			},
		},
		"required": []string{"progress", "goalAchieved", "missing", "suggestions"},
	}

	goalJSON, _ := json.MarshalIndent(goal, "", "  ")
	completedJSON, _ := json.MarshalIndent(completedStepsRaw, "", "  ")
	stateJSON, _ := json.MarshalIndent(currentState, "", "  ")

	prompt := fmt.Sprintf(`You are a reflective agent evaluating task execution progress.

Target Goal:
%s

Completed Steps:
%s

Current System State:
%s

Evaluate:
1. Progress (progress): How close to the goal (0.0-1.0, where 1.0 means goal achieved)
2. Goal achieved (goalAchieved): Boolean indicating if the goal has been fully achieved
3. Missing elements (missing): List of what still needs to be done
4. Suggestions (suggestions): Concrete actions to achieve the goal

Evaluation Criteria:
- Progress: Measure completion percentage based on completed steps and goal achievement
- Goal achieved: Determine if all success criteria from the goal have been met
- Missing: Identify gaps between current state and desired goal
- Suggestions: Provide actionable recommendations with specific steps if needed

IMPORTANT: You MUST return ONLY valid JSON. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON with fields: progress (number 0.0-1.0), goalAchieved (boolean), missing (array of strings), suggestions (array of objects with action, step, reason).`,
		string(goalJSON),
		string(completedJSON),
		string(stateJSON),
	)

	// Используем строгий JSON режим для рефлексии
	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{
		Temperature: 0.2, // Очень низкая температура для объективной оценки прогресса
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reflect: %w", err)
	}

	jsonContent := result.Content
	if jsonContent == "" && result.RawJSON != "" {
		jsonContent = result.RawJSON
	}
	jsonContent = cleanJSONContent(jsonContent)

	// Строгая типизация для рефлексии
	type ReflectionSuggestion struct {
		Action string                 `json:"action"`
		Step   map[string]interface{} `json:"step,omitempty"`
		Reason string                 `json:"reason"`
	}

	type ReflectionResponse struct {
		Progress     float64                `json:"progress"`
		GoalAchieved bool                   `json:"goalAchieved"`
		Missing      []string               `json:"missing"`
		Suggestions  []ReflectionSuggestion `json:"suggestions"`
	}

	var reflection ReflectionResponse
	if err := json.Unmarshal([]byte(jsonContent), &reflection); err != nil {
		if e.logger != nil {
			e.logger.Printf("[ReflectExecutor] Failed to parse reflection JSON: %v, content: %s", err, jsonContent[:min(500, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to parse reflection: %w", err)
	}

	// Конвертировать suggestions в []interface{} для совместимости
	suggestionsOutput := make([]interface{}, 0, len(reflection.Suggestions))
	for _, sug := range reflection.Suggestions {
		sugMap := map[string]interface{}{
			"action": sug.Action,
			"reason": sug.Reason,
		}
		if sug.Step != nil {
			sugMap["step"] = sug.Step
		}
		suggestionsOutput = append(suggestionsOutput, sugMap)
	}

	confidence := 0.8
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"progress":     reflection.Progress,
			"goalAchieved": reflection.GoalAchieved,
			"missing":      reflection.Missing,
			"suggestions":  suggestionsOutput,
		},
		Message:         fmt.Sprintf("Progress: %.1f%%, Goal achieved: %v", reflection.Progress*100, reflection.GoalAchieved),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          fmt.Sprintf("Reflection completed: %.1f%% progress", reflection.Progress*100),
	}, nil
}
