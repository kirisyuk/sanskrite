package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeAIServiceCall struct {
	model        string
	requirements AIRequirements
}

type fakeAIService struct {
	provider  AIProvider
	executeFn func(task *AITask, model string) (*AITaskResult, error)
	calls     []fakeAIServiceCall
}

func (s *fakeAIService) Provider() AIProvider {
	return s.provider
}

func (s *fakeAIService) ExecuteTask(_ context.Context, task *AITask, model string) (*AITaskResult, error) {
	s.calls = append(s.calls, fakeAIServiceCall{
		model:        model,
		requirements: task.AIRequirements,
	})
	if s.executeFn != nil {
		return s.executeFn(task, model)
	}
	return &AITaskResult{
		Status:  AITaskStatusCompleted,
		Content: `{"ok":true}`,
	}, nil
}

func (s *fakeAIService) ExecuteEmbeddingTask(_ context.Context, _ string, _ string) (*AITaskResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *fakeAIService) ExecuteImageAnalysisByLinkTask(_ context.Context, _, _, _ string) (*AITaskResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *fakeAIService) ExecuteSearch(_ context.Context, _ string, _ string) (*AITaskResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestExecuteStructuredTaskWithModel_GeminiGroundingFallback(t *testing.T) {
	mm := NewAIModelManager()
	vertex := &fakeAIService{
		provider: AIProviderVertexAI,
		executeFn: func(_ *AITask, _ string) (*AITaskResult, error) {
			return nil, fmt.Errorf("Vertex AI API error 403: permission denied")
		},
	}
	gemini := &fakeAIService{
		provider: AIProviderGemini,
		executeFn: func(task *AITask, _ string) (*AITaskResult, error) {
			if task.AIRequirements.InternalDataGroundingCapability {
				return nil, fmt.Errorf("internal grounding must be disabled in gemini fallback phase")
			}
			if !task.AIRequirements.SearchGroundingCapability {
				return nil, fmt.Errorf("search grounding must stay enabled in gemini fallback phase")
			}
			return &AITaskResult{
				Status:  AITaskStatusCompleted,
				Content: `{"ok":true}`,
			}, nil
		},
	}
	exec := NewAITaskExecutor([]AIService{vertex, gemini}, mm)
	task := &AITask{
		TaskType:     TaskTypeKnowledgeSynthesis,
		InputContent: strings.Repeat("x", 1024),
		AIRequirements: AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                1000,
			MaxOutputSize:                   2000,
		},
		Verifier: NewSimpleJSONVerifier(),
	}

	parsed, model, err := exec.ExecuteStructuredTaskWithModel(context.Background(), task)
	if err != nil {
		t.Fatalf("ExecuteStructuredTaskWithModel returned error: %v", err)
	}
	if parsed == nil {
		t.Fatalf("parsed result is nil")
	}
	if !strings.HasPrefix(model, "gemini-") {
		t.Fatalf("expected gemini model in fallback phase, got %s", model)
	}
	if len(vertex.calls) == 0 {
		t.Fatalf("expected vertex attempts before gemini fallback")
	}
	if len(gemini.calls) == 0 {
		t.Fatalf("expected at least one gemini fallback attempt")
	}
	lastGemini := gemini.calls[len(gemini.calls)-1]
	if lastGemini.requirements.InternalDataGroundingCapability {
		t.Fatalf("expected internal grounding to be disabled in gemini fallback")
	}
	if !lastGemini.requirements.SearchGroundingCapability {
		t.Fatalf("expected search grounding to stay enabled in gemini fallback")
	}
	if got := strings.TrimSpace(lastGemini.requirements.PreferredModel); got != "gemini-3-flash-preview" {
		t.Fatalf("expected preferred model to be gemini-3-flash-preview in fallback, got %q", got)
	}
}

func TestExecuteStructuredTaskWithModel_GeminiFallbackSkippedWithoutGeminiProvider(t *testing.T) {
	mm := NewAIModelManager()
	vertex := &fakeAIService{
		provider: AIProviderVertexAI,
		executeFn: func(_ *AITask, _ string) (*AITaskResult, error) {
			return nil, fmt.Errorf("Vertex AI API error 403: permission denied")
		},
	}
	exec := NewAITaskExecutor([]AIService{vertex}, mm)
	task := &AITask{
		TaskType:     TaskTypeKnowledgeSynthesis,
		InputContent: strings.Repeat("x", 1024),
		AIRequirements: AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                1000,
		},
		Verifier: NewSimpleJSONVerifier(),
	}

	_, _, err := exec.ExecuteStructuredTaskWithModel(context.Background(), task)
	if err == nil {
		t.Fatalf("expected error when only vertex provider is available")
	}
	if !strings.Contains(err.Error(), "No suitable AI models available after all fallbacks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

