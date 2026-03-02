package qa

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// BusinessRelevanceEvaluator scores how well an answer addresses the question (0-1).
type BusinessRelevanceEvaluator struct {
	executor *ai.AITaskExecutor
}

// NewBusinessRelevanceEvaluator creates a new evaluator.
func NewBusinessRelevanceEvaluator(executor *ai.AITaskExecutor) *BusinessRelevanceEvaluator {
	return &BusinessRelevanceEvaluator{executor: executor}
}

// Evaluate returns 0.0-1.0: how business-relevant the answer is to the question.
func (e *BusinessRelevanceEvaluator) Evaluate(ctx context.Context, question string, answer *horizonmodels.QAAnswer) (float64, error) {
	if e == nil || e.executor == nil || answer == nil {
		return 0.5, nil // neutral when not wired
	}
	question = strings.TrimSpace(question)
	answerText := strings.TrimSpace(answer.Text)
	if answerText == "" {
		return 0, nil
	}

	prompt := fmt.Sprintf(`Score how well this answer addresses the business question. Return ONLY a number 0.0-1.0.

QUESTION: %s

ANSWER: %s

Scoring criteria (penalize if missing):
- Direct answer: does it address what was asked?
- Quantification: specific numbers/ranges (e.g. "2-4%%") > vague ("likely", "potentially" without numbers)
- Alternatives: actionable options mentioned > none
- Evidence: grounded in data > generic statements
- Actionability: concrete next steps > abstract

Return a single number 0.0-1.0. No explanation.`, question, answerText)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       4,
			MaxOutputSize:            32,
			MinContextWindow:         2000,
		},
	}

	aiResult, err := e.executor.ExecuteTask(ctx, task)
	if err != nil {
		return 0.5, fmt.Errorf("relevance evaluation failed: %w", err)
	}
	if aiResult == nil {
		return 0.5, nil
	}

	text := aiResult.Content
	text = strings.TrimSpace(strings.TrimPrefix(text, "```"))
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[:idx]
	}
	score, err := strconv.ParseFloat(text, 64)
	if err != nil || score < 0 {
		return 0.5, nil
	}
	if score > 1 {
		score = 1
	}
	return score, nil
}
