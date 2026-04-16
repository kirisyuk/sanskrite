package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Integration: real OpenRouter when OPENROUTER_API_KEY is set; exercises AIModelManager + OpenRouterService.

func TestOpenRouter_NemotronViaModelManager(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set; skipping OpenRouter integration test")
	}

	mm := NewAIModelManager()
	svc := NewOpenRouterService(apiKey, mm)

	models := []string{
		"nvidia/nemotron-3-nano-30b-a3b",
		"nvidia/nemotron-3-super-120b-a12b",
		"nvidia/nemotron-3-nano-30b-a3b:free",
		"nvidia/nemotron-3-super-120b-a12b:free",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			caps, err := mm.GetModelCapabilities(model)
			if err != nil {
				t.Fatalf("GetModelCapabilities: model %q must be in registry: %v", model, err)
			}
			if caps.Provider != AIProviderOpenRouter {
				t.Fatalf("expected provider OpenRouter, got %q", caps.Provider)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			task := &AITask{
				TaskType:     TaskTypeContentGeneration,
				InputContent: "What is 2+2? Reply with one digit only.",
				AIRequirements: AIRequirements{
					TextGenerationCapability: true,
					MinCapabilityScore:       1,
					MaxOutputSize:            256,
				},
			}

			res, err := svc.ExecuteTask(ctx, task, model)
			if err != nil {
				t.Fatalf("ExecuteTask: %v", err)
			}
			if res.Status != AITaskStatusCompleted {
				t.Fatalf("unexpected status: %s", res.Status)
			}
			if strings.TrimSpace(res.Content) == "" {
				t.Fatalf("empty Content")
			}
		})
	}
}
