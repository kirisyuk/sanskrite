package discovery

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// fake AI service that returns a fixed content as title
type fakeAIService struct{}

func (f *fakeAIService) Provider() ai.AIProvider { return ai.AIProviderOpenAI }
func (f *fakeAIService) ExecuteTask(ctx context.Context, task *ai.AITask, model string) (*ai.AITaskResult, error) {
	return &ai.AITaskResult{Content: "\"Great Title\"", Status: ai.AITaskStatusCompleted}, nil
}
func (f *fakeAIService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*ai.AITaskResult, error) {
	return &ai.AITaskResult{Embedding: []float64{1}}, nil
}
func (f *fakeAIService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*ai.AITaskResult, error) {
	return &ai.AITaskResult{Content: "ok"}, nil
}
func (f *fakeAIService) ExecuteSearch(ctx context.Context, query string, modelName string) (*ai.AITaskResult, error) {
	return &ai.AITaskResult{Content: "ok"}, nil
}

func TestGenerateProjectTitle_SanitizesQuotes(t *testing.T) {
	exec := ai.NewAITaskExecutor([]ai.AIService{&fakeAIService{}}, ai.NewAIModelManager())
	o := &Orchestrator{}
	o.exec = exec
	title, err := o.generateProjectTitle(context.Background(), "Some query", "en")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if title == "" || title[0] == '"' || title[len(title)-1] == '"' {
		t.Fatalf("expected sanitized title without quotes, got %q", title)
	}
}
