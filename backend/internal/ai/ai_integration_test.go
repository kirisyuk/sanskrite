package ai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/prompts"
)

// Integration tests that call real OpenAI when OPENAI_API_KEY is set.

func TestOpenAI_SummarizationIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set; skipping LLM integration test")
	}

	svc := NewOpenAIService(apiKey, NewAIModelManager())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	task := &AITask{
		TaskType:     TaskTypeSummarization,
		InputContent: "Summarize in 1-2 sentences:\n\nOpenAI provides powerful models for text generation and understanding.",
		AIRequirements: AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5,
			MinContextWindow:         2000,
		},
	}

	// Use a concrete, commonly available model to avoid selection ambiguity
	res, err := svc.ExecuteTask(ctx, task, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("openai execute summarization err: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected non-empty summary, got empty")
	}
}

func TestOpenAI_DocumentExtractionIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set; skipping LLM integration test")
	}

	svc := NewOpenAIService(apiKey, NewAIModelManager())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sample := "Apple unveiled new Mac devices at its event. The products ship next month."
	prompt := prompts.Format(prompts.DocumentExtractionPrompt, map[string]string{
		"language": "en",
		"text":     sample,
	})
	task := &AITask{
		TaskType:     TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            2000,
		},
	}

	res, err := svc.ExecuteTask(ctx, task, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("openai execute extraction err: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected non-empty model output")
	}

	// Extract JSON from response using executor's helper
	exec := NewAITaskExecutor([]AIService{svc}, NewAIModelManager())
	jsonStr, err := exec.extractValidJSON(res.Content)
	if err != nil {
		t.Fatalf("failed to extract JSON: %v\nraw: %s", err, res.Content)
	}

	// Validate minimal required fields via MapVerifier
	ver := NewMapVerifier([]string{"language", "documentType"}, nil)
	if _, err := ver.VerifyAndParse(jsonStr); err != nil {
		t.Fatalf("verification failed: %v\njson: %s", err, jsonStr)
	}
}
