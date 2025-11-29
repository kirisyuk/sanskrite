package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// PlanGenerateExecutor генерирует план выполнения из естественного языка
type PlanGenerateExecutor struct {
	genaiClient     *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger          *log.Logger
}

// NewPlanGenerateExecutor создает новый исполнитель для генерации планов
func NewPlanGenerateExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *PlanGenerateExecutor {
	return &PlanGenerateExecutor{
		genaiClient:       genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:            logger,
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
					"action": map[string]interface{}{"type": "string"},
					"target": map[string]interface{}{"type": "string"},
					"expectedOutcome": map[string]interface{}{"type": "string"},
				},
				"required": []string{"action", "target", "expectedOutcome"},
			},
			"goal": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{"type": "string"},
					"successCriteria": map[string]interface{}{
						"type": "array",
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
						"id": map[string]interface{}{"type": "string"},
						"capability": map[string]interface{}{"type": "string"},
						"input": map[string]interface{}{"type": "object"},
						"dependsOn": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"reason": map[string]interface{}{"type": "string"},
						"isCritical": map[string]interface{}{"type": "boolean"},
					},
					"required": []string{"id", "capability", "input", "reason"},
				},
			},
		},
		"required": []string{"intent", "goal", "steps"},
	}

	prompt := fmt.Sprintf(`Ты - планировщик задач для агентской системы. Твоя задача - создать план выполнения для запроса пользователя.

Запрос пользователя: "%s"

Доступные capabilities (инструменты):
%s

Контекст проекта:
%s

Создай план выполнения, который включает:
1. Понимание намерения (intent): что пользователь хочет сделать, с чем работать, какой результат ожидается
2. Целевой результат (goal): описание желаемого результата и критерии успеха
3. Шаги выполнения (steps): последовательность шагов с использованием доступных capabilities

Правила:
- Каждый шаг должен использовать одну из доступных capabilities
- Указывай зависимости между шагами через dependsOn (ID предыдущих шагов)
- Критичные шаги (isCritical: true) - это те, при сбое которых задача не может быть выполнена (например, чтение/запись документов)
- Некритичные шаги могут быть пропущены при ошибке
- ID шагов должны быть уникальными (например, step1, step2, step3)
- В reason объясни, зачем нужен этот шаг

Верни JSON с полями: intent, goal, steps.`, 
		userQuery,
		formatCapabilities(capabilityNames),
		contextInfo,
	)

	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// Парсить JSON из Content или RawJSON
	jsonContent := result.Content
	if jsonContent == "" && result.RawJSON != "" {
		jsonContent = result.RawJSON
	}

	// Убираем markdown code fences если есть
	jsonContent = cleanJSONContent(jsonContent)

	var plan struct {
		Intent map[string]interface{} `json:"intent"`
		Goal   map[string]interface{} `json:"goal"`
		Steps  []struct {
			ID         string                 `json:"id"`
			Capability string                 `json:"capability"`
			Input      map[string]interface{} `json:"input"`
			DependsOn  []string               `json:"dependsOn,omitempty"`
			Reason     string                 `json:"reason"`
			IsCritical bool                   `json:"isCritical"`
		} `json:"steps"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &plan); err != nil {
		if e.logger != nil {
			e.logger.Printf("[PlanGenerateExecutor] Failed to parse plan JSON: %v, content: %s", err, jsonContent[:min(500, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	// Валидировать, что все capabilities существуют
	for _, step := range plan.Steps {
		if _, err := e.capabilityRegistry.Get(Capability(step.Capability)); err != nil {
			return nil, fmt.Errorf("invalid capability '%s' in step %s: %w", step.Capability, step.ID, err)
		}
	}

	// Конвертировать в формат для output
	stepsOutput := make([]map[string]interface{}, 0, len(plan.Steps))
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

	confidence := 0.9 // Высокая уверенность в сгенерированном плане
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"intent": plan.Intent,
			"goal":   plan.Goal,
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
	genaiClient       *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger            *log.Logger
}

// NewReplanExecutor создает новый исполнитель для репланирования
func NewReplanExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *ReplanExecutor {
	return &ReplanExecutor{
		genaiClient:       genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:            logger,
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
									"type": "array",
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
				"type": "array",
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

	prompt := fmt.Sprintf(`Ты - планировщик задач для агентской системы. Твоя задача - скорректировать план выполнения на основе результатов выполнения шагов.

Текущий план:
%s

Выполненные шаги (с результатами):
%s

Провалившиеся шаги:
%s

Целевой результат:
%s

Доступные capabilities:
%s

Проанализируй ситуацию и:
1. Определи, достигнута ли цель (goalAchieved)
2. Если цель не достигнута, создай обновленный план (updatedPlan) с новыми шагами
3. Укажи причину изменений (reason)
4. Перечисли ID следующих шагов для выполнения (nextSteps)

Правила:
- Используй только доступные capabilities
- Учитывай результаты выполненных шагов (можешь ссылаться на их output через {{stepId.field}})
- При провале шага предложи альтернативный подход
- ID новых шагов должны быть уникальными (например, step4, step5)
- Указывай зависимости через dependsOn

Верни JSON с полями: updatedPlan, goalAchieved, nextSteps.`,
		string(planJSON),
		string(completedJSON),
		string(failedJSON),
		string(goalJSON),
		formatCapabilities(capabilityNames),
	)

	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to replan: %w", err)
	}

	jsonContent := result.Content
	if jsonContent == "" && result.RawJSON != "" {
		jsonContent = result.RawJSON
	}
	jsonContent = cleanJSONContent(jsonContent)

	var replan struct {
		UpdatedPlan struct {
			Steps  []struct {
				ID         string                 `json:"id"`
				Capability string                 `json:"capability"`
				Input      map[string]interface{} `json:"input"`
				DependsOn  []string               `json:"dependsOn,omitempty"`
				Reason     string                 `json:"reason"`
				IsCritical bool                   `json:"isCritical"`
			} `json:"steps"`
			Reason string `json:"reason"`
		} `json:"updatedPlan"`
		GoalAchieved bool     `json:"goalAchieved"`
		NextSteps    []string `json:"nextSteps,omitempty"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &replan); err != nil {
		if e.logger != nil {
			e.logger.Printf("[ReplanExecutor] Failed to parse replan JSON: %v, content: %s", err, jsonContent[:min(500, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to parse replan: %w", err)
	}

	// Валидировать capabilities
	for _, step := range replan.UpdatedPlan.Steps {
		if _, err := e.capabilityRegistry.Get(Capability(step.Capability)); err != nil {
			return nil, fmt.Errorf("invalid capability '%s' in step %s: %w", step.Capability, step.ID, err)
		}
	}

	// Конвертировать в формат для output
	stepsOutput := make([]map[string]interface{}, 0, len(replan.UpdatedPlan.Steps))
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
			"progress": map[string]interface{}{"type": "number"}, // 0.0-1.0
			"goalAchieved": map[string]interface{}{"type": "boolean"},
			"missing": map[string]interface{}{
				"type": "array",
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

	prompt := fmt.Sprintf(`Ты - рефлексивный агент, оценивающий прогресс выполнения задачи.

Целевой результат:
%s

Выполненные шаги:
%s

Текущее состояние системы:
%s

Оцени:
1. Прогресс (progress): насколько близко к цели (0.0-1.0)
2. Достигнута ли цель (goalAchieved)
3. Что еще нужно сделать (missing) - список недостающих элементов
4. Предложения (suggestions): конкретные действия для достижения цели

Верни JSON с полями: progress, goalAchieved, missing, suggestions.`,
		string(goalJSON),
		string(completedJSON),
		string(stateJSON),
	)

	result, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to reflect: %w", err)
	}

	jsonContent := result.Content
	if jsonContent == "" && result.RawJSON != "" {
		jsonContent = result.RawJSON
	}
	jsonContent = cleanJSONContent(jsonContent)

	var reflection struct {
		Progress     float64 `json:"progress"`
		GoalAchieved bool    `json:"goalAchieved"`
		Missing      []string `json:"missing"`
		Suggestions  []struct {
			Action string                 `json:"action"`
			Step   map[string]interface{} `json:"step,omitempty"`
			Reason string                 `json:"reason"`
		} `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &reflection); err != nil {
		if e.logger != nil {
			e.logger.Printf("[ReflectExecutor] Failed to parse reflection JSON: %v, content: %s", err, jsonContent[:min(500, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to parse reflection: %w", err)
	}

	confidence := 0.8
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"progress":     reflection.Progress,
			"goalAchieved": reflection.GoalAchieved,
			"missing":      reflection.Missing,
			"suggestions":  reflection.Suggestions,
		},
		Message:         fmt.Sprintf("Progress: %.0f%%, Goal achieved: %v", reflection.Progress*100, reflection.GoalAchieved),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          fmt.Sprintf("Reflection completed, %d suggestions", len(reflection.Suggestions)),
	}, nil
}

