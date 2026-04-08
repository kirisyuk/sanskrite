package horizon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

const (
	finalSynthesisMaxAttempts = 3
	finalSynthesisBaseBackoff = 4 * time.Second
)

var finalSynthesisFallbackModels = []string{
	"gemini-3-flash-preview",
	"gemini-2.5-flash",
	"gemini-2.5-pro",
}

type finalSynthesisSpec struct {
	TenantID          string
	ProjectID         string
	CascadeID         string
	UseCase           string
	Prompt            string
	SystemInstruction string
	MaxOutputTokens   int
}

func (h *HorizonHandlers) executeFinalSynthesis(ctx context.Context, spec finalSynthesisSpec) (string, string, error) {
	if h == nil || h.service == nil || h.service.AIExecutor() == nil {
		return "", "", fmt.Errorf("AI executor is not configured for final synthesis")
	}

	fullInput := strings.TrimSpace(spec.Prompt)
	if si := strings.TrimSpace(spec.SystemInstruction); si != "" {
		fullInput = si + "\n\nUser request: " + fullInput
	}
	if fullInput == "" {
		return "", "", fmt.Errorf("final synthesis input is empty")
	}

	maxOutput := spec.MaxOutputTokens
	if maxOutput <= 0 {
		maxOutput = 1400
	}
	minContext := len(fullInput) / 4
	if minContext < 1000 {
		minContext = 1000
	}

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: fullInput,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           lensChatModel,
			FallbackModels:           append([]string(nil), finalSynthesisFallbackModels...),
			MinContextWindow:         minContext,
			MaxOutputSize:            maxOutput,
			MinCapabilityScore:       6,
			TextGenerationCapability: true,
		},
		TenantID:  strings.TrimSpace(spec.TenantID),
		Project:   strings.TrimSpace(spec.ProjectID),
		UseCase:   strings.TrimSpace(spec.UseCase),
		CascadeID: strings.TrimSpace(spec.CascadeID),
	}

	var lastErr error
	for attempt := 1; attempt <= finalSynthesisMaxAttempts; attempt++ {
		if attempt > 1 {
			backoff := finalSynthesisBaseBackoff * time.Duration(1<<(attempt-2))
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := h.service.AIExecutor().ExecuteTask(ctx, task)
		if err == nil {
			err = finalSynthesisResultError(result)
			if err == nil {
				return strings.TrimSpace(result.Content), strings.TrimSpace(result.Model), nil
			}
		}
		lastErr = err
		if !isRetryableFinalSynthesisError(err) {
			break
		}
	}

	return "", "", lastErr
}

func finalSynthesisResultError(result *ai.AITaskResult) error {
	if result == nil {
		return fmt.Errorf("final synthesis returned nil result")
	}
	if result.Status != ai.AITaskStatusCompleted {
		msg := strings.TrimSpace(result.Content)
		if msg == "" {
			msg = "final synthesis task failed"
		}
		return fmt.Errorf("%s (model=%s status=%s)", msg, strings.TrimSpace(result.Model), result.Status)
	}
	if strings.TrimSpace(result.Content) == "" {
		return fmt.Errorf("final synthesis returned empty content (model=%s)", strings.TrimSpace(result.Model))
	}
	return nil
}

func isRetryableFinalSynthesisError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "503") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "resource exhausted") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "retryable") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "all models have been tried") ||
		strings.Contains(msg, "task could not be completed due to unavailability")
}
