package watchdog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// ChangeAssessmentResult результат оценки значимости изменений через LLM
type ChangeAssessmentResult struct {
	ChangeAmount float64 `json:"changeAmount"`
	Significance string  `json:"significance"` // "high", "medium", "low"
	Reasoning    string  `json:"reasoning,omitempty"`
}

// ChangeAssessor оценивает значимость изменений через LLM
type ChangeAssessor struct {
	executor *ai.AITaskExecutor
}

// NewChangeAssessor создает новый оценщик изменений
func NewChangeAssessor(executor *ai.AITaskExecutor) *ChangeAssessor {
	return &ChangeAssessor{
		executor: executor,
	}
}

// AssessChangeAmount оценивает значимость изменений между старым и новым контентом через LLM
func (c *ChangeAssessor) AssessChangeAmount(ctx context.Context, oldContent string, newContent string, targetFragmentCanonicalPath string) (float64, error) {
	if c == nil || c.executor == nil {
		return 0.5, fmt.Errorf("change assessor not configured")
	}
	if oldContent == "" {
		return 1.0, nil // Первое сканирование - считаем максимальным изменением
	}

	if newContent == "" {
		return 0.0, nil // Нет нового контента - изменений нет
	}

	prompt := c.buildChangeAssessmentPrompt(oldContent, newContent, targetFragmentCanonicalPath)

	// Создаем типизированный верификатор
	verifier := ai.NewGenericVerifier[ChangeAssessmentResult](func(result ChangeAssessmentResult) error {
		if result.ChangeAmount < 0.0 || result.ChangeAmount > 1.0 {
			return fmt.Errorf("changeAmount must be between 0.0 and 1.0")
		}
		if result.Significance != "high" && result.Significance != "medium" && result.Significance != "low" {
			return fmt.Errorf("significance must be 'high', 'medium', or 'low'")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7, // Хорошее качество для оценки изменений
			MinContextWindow:         16000, // Может быть большой контент
			MaxOutputSize:            256,
		},
		Verifier: verifier,
	}

	result, err := c.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return 0.5, fmt.Errorf("failed to assess change amount: %w", err) // Fallback на среднее значение
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return 0.5, fmt.Errorf("failed to marshal result: %w", err)
	}

	var assessment ChangeAssessmentResult
	if err := json.Unmarshal(jsonData, &assessment); err != nil {
		return 0.5, fmt.Errorf("failed to unmarshal assessment: %w", err)
	}

	return assessment.ChangeAmount, nil
}

// buildChangeAssessmentPrompt строит промпт для оценки значимости изменений
func (c *ChangeAssessor) buildChangeAssessmentPrompt(oldContent string, newContent string, targetFragmentCanonicalPath string) string {
	// Ограничиваем размер контента для промпта (первые 2000 символов каждого)
	oldPreview := oldContent
	if len(oldPreview) > 2000 {
		oldPreview = oldPreview[:2000] + "..."
	}

	newPreview := newContent
	if len(newPreview) > 2000 {
		newPreview = newPreview[:2000] + "..."
	}

	return fmt.Sprintf(`You are a change significance assessment agent for a risk analysis system. Your task is to assess the significance of changes between old and new content from a data source.

TARGET ENTITY:
- Canonical Path: %s

OLD CONTENT (previous version):
%s

NEW CONTENT (current version):
%s

TASK: Assess the significance of changes between old and new content on a scale from 0.0 to 1.0, where:
- 0.0-0.2: No significant changes (minor formatting, typos, insignificant updates)
- 0.2-0.5: Low significance (minor factual updates, small additions)
- 0.5-0.7: Medium significance (substantial updates, new information, important changes)
- 0.7-1.0: High significance (major changes, critical updates, breaking news, significant events)

Consider:
1. Semantic meaning of changes (not just character differences)
2. Relevance to the target entity
3. Importance of information added/removed/changed
4. Whether changes represent new facts, events, or significant updates
5. Impact on risk assessment for the target entity

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "changeAmount": 0.65,
  "significance": "medium",
  "reasoning": "Brief explanation of the assessment"
}

RULES:
1. changeAmount must be a number between 0.0 and 1.0
2. significance must be one of: "high", "medium", "low"
3. Focus on semantic significance, not just text differences
4. Consider relevance to the target entity
5. Return ONLY valid JSON, no markdown code fences or additional text

Return the change assessment as JSON:`, targetFragmentCanonicalPath, oldPreview, newPreview)
}

