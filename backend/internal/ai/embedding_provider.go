package ai

import (
	"context"
)

// EmbeddingProvider abstracts embedding generation
type EmbeddingProvider interface {
	// ModelID returns current embedding model identifier
	ModelID() string
	// EmbedTexts returns embeddings for input texts (same dimension for all)
	EmbedTexts(ctx context.Context, texts []string) ([][]float64, error)
}

// OpenAIEmbedding wraps AITaskExecutor to generate embeddings using a configured OpenAI model
type OpenAIEmbedding struct {
	exec    *AITaskExecutor
	modelID string
}

func NewOpenAIEmbedding(exec *AITaskExecutor, modelID string) *OpenAIEmbedding {
	if modelID == "" {
		modelID = "text-embedding-3-small"
	}
	return &OpenAIEmbedding{exec: exec, modelID: modelID}
}

func (p *OpenAIEmbedding) ModelID() string { return p.modelID }

func (p *OpenAIEmbedding) EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		task := &AITask{TaskType: TaskTypeEmbeddingCalculation, InputContent: t, AIRequirements: AIRequirements{EmbeddingGenerationCapability: true, MinCapabilityScore: 1}}
		res, err := p.exec.ExecuteEmbeddingCalculationTask(ctx, task)
		if err != nil {
			return nil, err
		}
		out = append(out, res.Embedding)
	}
	return out, nil
}

// NoOpEmbedding returns zero vectors — for signaling/RSS ingest when RAG search is not needed.
// Chunks are created but won't match semantic search; document.process still works.
const NoOpEmbeddingDim = 1536

type NoOpEmbedding struct{}

func NewNoOpEmbedding() *NoOpEmbedding { return &NoOpEmbedding{} }

func (p *NoOpEmbedding) ModelID() string { return "noop" }

func (p *NoOpEmbedding) EmbedTexts(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = make([]float64, NoOpEmbeddingDim)
	}
	return out, nil
}
