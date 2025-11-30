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
	openDataDiscovery := isOpenDataDiscoveryQuery(userQuery)

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
						"input":      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": true},
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

	specialInstructions := ""
	if openDataDiscovery {
		specialInstructions = `
Special Guidance (Rolling Horizon Planning):
- Do NOT try to plan the entire task from start to finish if it involves discovery or uncertain data.
- Create ONLY the "Entry Strategy" (Discovery Phase) - the first 2-3 steps to gather information.
- ALWAYS end your initial plan with a 'plan_calibrate' step. This step will analyze the discovery results and generate the next steps dynamically.

CORRECT Initial Plan Structure:
1. source_list (optional) - check what we already have
2. web_search OR data_search - find new information/candidates
3. plan_calibrate - analyze findings and decide what to do next

WRONG Plan Structure (Anti-Pattern):
1. web_search
2. telegram_channel_read (WRONG: you don't know the channel yet)
3. source_create (WRONG: you don't have data yet)
4. ... (WRONG: guessing future steps)

CRITICAL RULES:
- NEVER create 'telegram_channel_read', 'document_read', or 'source_create' steps in the initial plan unless the user EXPLICITLY provided the IDs/URLs in the request.
- If the user says "Find crypto channels", your plan is: 1. web_search "crypto channels", 2. plan_calibrate. STOP THERE.
- Trust 'plan_calibrate' to handle the rest. It acts as a router/iterator.`
	}

	prompt := fmt.Sprintf(`You are a strategic task planner. Your goal is to create an INITIAL entry plan for a user request.

User Request: "%s"

Available Capabilities:
%s

Project Context:
%s

PLANNING STRATEGY (Rolling Horizon):
1. Discovery First: Plan only the steps needed to gather initial information (search, list).
2. No Hallucinations: Do not create steps that require IDs or URLs you don't have yet.
3. Calibrate: Always add 'plan_calibrate' as the final step of this initial plan. It will handle the dynamic generation of subsequent steps based on real data.

Example for "Find and add crypto channels":
- Step 1: web_search (query: "best crypto telegram channels")
- Step 2: plan_calibrate (reason: "Analyze search results and iterate over found channels")
-> STOP. Do not plan further.

Example for "Summarize document X":
- Step 1: data_search (query: "X")
- Step 2: plan_calibrate (reason: "Check if document found and decide to read it")
-> STOP.

Output Format:
Return ONLY valid JSON matching this exact structure:
{
  "intent": { "action": "...", "target": "...", "expectedOutcome": "..." },
  "goal": { "description": "...", "successCriteria": ["..."] },
  "steps": [
    { "id": "step1", "capability": "...", "input": {...}, "reason": "...", "isCritical": true }
  ]
}

CRITICAL: Field names must match EXACTLY: "intent", "action", "target", "expectedOutcome", "goal", "description", "successCriteria", "steps", "id", "capability", "input", "dependsOn", "reason", "isCritical". All fields are REQUIRED except "dependsOn".`,
		userQuery,
		formatCapabilities(capabilityNames),
		contextInfo,
		specialInstructions,
	)

	// Используем Gemini с ResponseFormat: "json" и SystemInstruction для надежного JSON вывода
	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{
		ModelOverride: "gemini_fast",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	jsonContent := result.Content
	if jsonContent == "" {
		return nil, fmt.Errorf("GenerateJSONStrict returned empty response")
	}

	// Очищаем от markdown code fences
	jsonContent = cleanJSONContent(jsonContent)

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
	if len(caps) == 0 {
		return "- (no capabilities registered)\n"
	}

	var sb strings.Builder
	limit := min(len(caps), 20)
	for i := 0; i < limit; i++ {
		capName := caps[i]
		desc := capabilityDescriptions[strings.ToLower(capName)]
		if desc == "" {
			desc = "Registered capability available to the agent."
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", capName, desc))
	}
	if len(caps) > limit {
		sb.WriteString(fmt.Sprintf("... and %d more capabilities\n", len(caps)-limit))
	}
	return sb.String()
}

// fixInvalidJSONEscapes исправляет невалидные escape-последовательности в JSON
// LLM иногда возвращает \' вместо ' что невалидно в JSON
func fixInvalidJSONEscapes(content string) string {
	// Заменяем \' на ' (невалидный escape в JSON)
	content = strings.ReplaceAll(content, `\'`, `'`)
	// Заменяем \` на ` (невалидный escape в JSON)
	content = strings.ReplaceAll(content, "\\`", "`")
	return content
}

// extractPureJSON извлекает чистый JSON объект из текста
// Ищет первый сбалансированный JSON объект с учетом вложенности
func extractPureJSON(content string) string {
	if content == "" {
		return ""
	}

	// Найти первую открывающую скобку
	startIdx := strings.Index(content, "{")
	if startIdx == -1 {
		return ""
	}

	// Найти соответствующую закрывающую скобку с учетом вложенности
	depth := 0
	inString := false
	escaped := false

	for i := startIdx; i < len(content); i++ {
		c := content[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return content[startIdx : i+1]
			}
		}
	}

	return ""
}

var capabilityDescriptions = map[string]string{
	"source_list":              "Lists current project data sources to avoid duplicates.",
	"web_search":               "Searches the public internet for fresh information and URLs.",
	"plan_calibrate":           "Analyzes previous steps and injects concrete follow-up actions with real parameters.",
	"telegram_channel_read":    "Reads messages from a specific Telegram channel (requires channelUsername).",
	"source_create":            "Creates a new data source (e.g., Telegram channel) and attaches it to the project.",
	"document_read":            "Reads stored project documents by ID.",
	"document_write":           "Creates or updates a project document.",
	"data_search":              "Searches existing project documents by text query.",
	"llm_generate":             "Generates free-form text using the LLM.",
	"llm_analyze":              "Performs structured analysis of supplied content via LLM.",
	"bigquery_query":           "Executes analytical SQL queries in BigQuery.",
	"plan_generate":            "Creates an initial multi-step plan from the user request.",
	"replan":                   "Updates the plan when existing steps fail or new data arrives.",
	"reflect":                  "Evaluates progress toward the overall goal.",
	"telegram_channel_reading": "Reads Telegram channel content (alias).",
}

func isOpenDataDiscoveryQuery(query string) bool {
	q := strings.ToLower(query)
	keywords := []string{
		"telegram", "телеграм", "канал", "каналы", "каналов",
		"источник", "источники", "источников", "rss", "web-source",
		"найди", "подбери", "подбор", "source", "sources",
	}
	for _, kw := range keywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
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

IMPORTANT: You MUST return ONLY valid JSON matching this EXACT structure. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON in this EXACT format:
{
  "updatedPlan": {
    "steps": [
      {
        "id": "step4",
        "capability": "capability_name",
        "input": {"key": "value"},
        "dependsOn": ["step1"],
        "reason": "Why this step is needed",
        "isCritical": false
      }
    ],
    "reason": "Explanation of plan changes"
  },
  "goalAchieved": false,
  "nextSteps": ["step4", "step5"]
}

CRITICAL: Field names must match EXACTLY: "updatedPlan", "steps", "reason", "goalAchieved", "nextSteps", "id", "capability", "input", "dependsOn", "isCritical".`,
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

	// Строгая типизация: GenerateJSONStrict должен возвращать валидный JSON
	if jsonContent == "" {
		return nil, fmt.Errorf("GenerateJSONStrict returned empty JSON content")
	}

	// Проверяем, что это валидный JSON (начинается с {)
	if len(jsonContent) > 0 && jsonContent[0] != '{' {
		return nil, fmt.Errorf("GenerateJSONStrict returned non-JSON content (starts with '%c') - this violates strict JSON mode", jsonContent[0])
	}

	var replan ReplanResponse
	if err := json.Unmarshal([]byte(jsonContent), &replan); err != nil {
		if e.logger != nil {
			previewLen := min(1000, len(jsonContent))
			e.logger.Printf("[ReplanExecutor] Failed to parse replan JSON: %v", err)
			e.logger.Printf("[ReplanExecutor] JSON content preview (first %d chars): %s", previewLen, jsonContent[:previewLen])
		}
		return nil, fmt.Errorf("failed to parse replan JSON (GenerateJSONStrict should return valid JSON): %w", err)
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
								"input":      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": true},
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

IMPORTANT: You MUST return ONLY valid JSON matching this EXACT structure. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON in this EXACT format:
{
  "progress": 0.75,
  "goalAchieved": false,
  "missing": ["element1", "element2"],
  "suggestions": [
    {
      "action": "action_description",
      "step": {"capability": "capability_name", "input": {}},
      "reason": "Why this suggestion"
    }
  ]
}

CRITICAL: Field names must match EXACTLY: "progress", "goalAchieved", "missing", "suggestions", "action", "step", "reason".`,
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
