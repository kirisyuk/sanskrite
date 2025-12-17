package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// CriticAgent проверяет извлеченные данные на соответствие источникам
type CriticAgent struct {
	executor *ai.AITaskExecutor
}

// NewCriticAgent создает новый Critic Agent
func NewCriticAgent(executor *ai.AITaskExecutor) *CriticAgent {
	return &CriticAgent{
		executor: executor,
	}
}

// ValidationResult результат валидации Critic Agent
type ValidationResult struct {
	HasConflicts bool                   `json:"hasConflicts"`
	Conflicts    []ValidationConflict   `json:"conflicts,omitempty"`
	Confidence   float64                `json:"confidence"` // 0..1
	IsValid      bool                   `json:"isValid"`
}

// ValidationConflict конфликт между извлеченными данными и источниками
type ValidationConflict struct {
	Field       string `json:"field"`
	ExtractedValue interface{} `json:"extractedValue"`
	SourceValue interface{} `json:"sourceValue"`
	Reason      string `json:"reason"`
	Severity    string `json:"severity"` // "low", "medium", "high"
}

// Validate проверяет извлеченные данные на соответствие источникам
func (a *CriticAgent) Validate(ctx context.Context, extractedData *models.HorizonFragment, sourceContent string) (*ValidationResult, error) {
	prompt := a.buildValidationPrompt(extractedData, sourceContent)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         8000,
			MaxOutputSize:            2048,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate data: %w", err)
	}

	// Преобразуем результат в ValidationResult
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var validationResult ValidationResult
	if err := json.Unmarshal(jsonData, &validationResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal validation result: %w", err)
	}

	return &validationResult, nil
}

// buildValidationPrompt строит промпт для валидации данных
func (a *CriticAgent) buildValidationPrompt(extractedData *models.HorizonFragment, sourceContent string) string {
	extractedJSON, _ := json.MarshalIndent(extractedData, "", "  ")
	return fmt.Sprintf(`You are a validation agent (Critic) for a risk analysis system. Your task is to verify that extracted data matches the source content.

EXTRACTED DATA (JSON):
%s

SOURCE CONTENT:
%s

TASK: Compare the extracted data with the source content and identify any conflicts, inconsistencies, or missing information.

Return a JSON object with:
- hasConflicts: boolean indicating if conflicts were found
- conflicts: array of conflict objects (if any) with fields: field, extractedValue, sourceValue, reason, severity
- confidence: float (0..1) indicating confidence in the validation
- isValid: boolean indicating if data is valid overall

A conflict should be reported if:
1. Extracted value contradicts information in source
2. Extracted value is not supported by source
3. Important information from source is missing in extracted data
4. Metrics are outside valid ranges or inconsistent

Return ONLY valid JSON. Do not include markdown code fences.`, string(extractedJSON), sourceContent)
}

