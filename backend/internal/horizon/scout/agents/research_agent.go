package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ResearchAgent выполняет дополнительный поиск информации через LLM с Search Grounding
type ResearchAgent struct {
	executor *ai.AITaskExecutor
}

// NewResearchAgent создает новый Research Agent
func NewResearchAgent(executor *ai.AITaskExecutor) *ResearchAgent {
	return &ResearchAgent{
		executor: executor,
	}
}

// EnrichFragment выполняет поиск недостающей информации и обновляет фрагмент
func (a *ResearchAgent) EnrichFragment(ctx context.Context, fragment *models.HorizonFragment, missingInfo []string) (*models.HorizonFragment, error) {
	if len(missingInfo) == 0 {
		return fragment, nil
	}

	prompt := a.buildResearchPrompt(fragment, missingInfo)

	// Ожидаем возврата полного обновленного JSON фрагмента
	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true, // Включаем Google Search Grounding
			MinCapabilityScore:        9,    // Требуется высокая способность к рассуждению и поиску
			MinContextWindow:          16000,
			MaxOutputSize:             4096,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to execute research task: %w", err)
	}

	// Преобразуем результат в HorizonFragment
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal research result: %w", err)
	}

	var enrichedFragment models.HorizonFragment
	if err := json.Unmarshal(jsonData, &enrichedFragment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal enriched fragment: %w", err)
	}

	// Восстанавливаем некоторые поля, которые LLM могла затереть или изменить некорректно
	// (ID, TenantID, системные поля метаданных)
	enrichedFragment.ID = fragment.ID
	enrichedFragment.TenantID = fragment.TenantID
	enrichedFragment.CanonicalPath = fragment.CanonicalPath // Путь не должен меняться
	enrichedFragment.CreatedAt = fragment.CreatedAt
	enrichedFragment.CreatedBy = fragment.CreatedBy
	enrichedFragment.Version = fragment.Version
	
	// Сохраняем исходные Governance данные, если LLM их не вернула
	if enrichedFragment.Governance.Owner == "" {
		enrichedFragment.Governance = fragment.Governance
	}

	return &enrichedFragment, nil
}

// buildResearchPrompt строит промпт для дообогащения
func (a *ResearchAgent) buildResearchPrompt(fragment *models.HorizonFragment, questions []string) string {
	fragmentJSON, _ := json.MarshalIndent(fragment, "", "  ")
	questionsList := strings.Join(questions, "\n- ")

	return fmt.Sprintf(`You are a Research Agent for a risk analysis system. Your goal is to fill in missing information for a specific entity using Google Search capabilities.

TARGET ENTITY (Current Data):
%s

MISSING INFORMATION (Research Questions):
- %s

TASK:
1. Use Google Search to find answers to the missing information questions.
2. Verify the information from search results.
3. Update the entity data with the new findings.
4. Ensure all metrics are normalized to [0.0..1.0] range.
5. Return the FULLY UPDATED JSON object of the entity.

REQUIREMENTS:
- Preserve all existing correct data.
- If search yields no results for a specific question, leave the field as is (do not hallucinate).
- Add new metrics to the 'state' map if found.
- Update 'description' if new significant context is found.
- Update 'homeLocation' or 'identifiers' if found.

Return ONLY valid JSON matching the HorizonFragment schema.`, string(fragmentJSON), questionsList)
}

