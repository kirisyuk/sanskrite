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
	MissingInfo  []string               `json:"missingInfo,omitempty"` // Список вопросов для доизучения
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
func (a *CriticAgent) Validate(ctx context.Context, extractedData *models.HorizonFragment, relations []DiscoveredRelation, sourceContent string) (*ValidationResult, error) {
	prompt := a.buildValidationPrompt(extractedData, relations, sourceContent)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         16000, // Увеличиваем окно, так как добавляем relations
			MaxOutputSize:            4096,
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
func (a *CriticAgent) buildValidationPrompt(extractedData *models.HorizonFragment, relations []DiscoveredRelation, sourceContent string) string {
	extractedJSON, _ := json.MarshalIndent(extractedData, "", "  ")
	relationsJSON, _ := json.MarshalIndent(relations, "", "  ")

	return fmt.Sprintf(`You are a validation agent (Critic) for a risk analysis system. Your task is to verify that extracted data matches the source content and assess the completeness of the knowledge graph.

EXTRACTED DATA (JSON):
%s

EXTRACTED RELATIONS (JSON):
%s

SOURCE CONTENT:
%s

TASK: Compare the extracted data with the source content and identify any conflicts, inconsistencies, or missing information. Pay close attention to missing relationships.

	Return a JSON object with:
	- hasConflicts: boolean indicating if conflicts were found
	- conflicts: array of conflict objects (if any) with fields: field, extractedValue, sourceValue, reason, severity
	- confidence: float (0..1) indicating confidence in the validation
	- isValid: boolean indicating if data is valid overall
	- missingInfo: array of strings (questions). Identify CRITICAL missing information based on the entity type (e.g., missing location for a factory, missing capacity metrics). Formulate each item as a specific search query to find this data.

	A conflict should be reported if:
	1. Extracted value contradicts information in source
	2. Extracted value is not supported by source
	3. Metrics are outside valid ranges or inconsistent

	Missing Info logic:
	- If important fields (like location, key metrics for the sector) are empty or unknown, add a specific question to 'missingInfo'.
	- If the entity is an Organization or Facility, and NO supply chain relations (suppliers, customers, competitors) were found, you MUST add a question to find them. Example: "Who are the key suppliers for Tesla?" or "What are the major competitors of this entity?"
	- If relations seem sparse or inconsistent with the entity's scale, flag it.

	Return ONLY valid JSON. Do not include markdown code fences.`, string(extractedJSON), string(relationsJSON), sourceContent)
}
