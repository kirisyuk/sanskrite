package watchdog

import "sync"

// RunBudget limits expensive operations per watchdog run (per ProcessScheduledScansWithStats call).
// This is purely algorithmic protection against cost blowups from cascading LLM calls.
type RunBudget struct {
	maxEmbeddingPairs int
	maxLLMAssessments int

	mu             sync.Mutex
	embeddingPairs int
	llmAssessments int
}

func NewRunBudget(maxEmbeddingPairs, maxLLMAssessments int) *RunBudget {
	if maxEmbeddingPairs < 0 {
		maxEmbeddingPairs = 0
	}
	if maxLLMAssessments < 0 {
		maxLLMAssessments = 0
	}
	return &RunBudget{
		maxEmbeddingPairs: maxEmbeddingPairs,
		maxLLMAssessments: maxLLMAssessments,
	}
}

func (b *RunBudget) TryUseEmbeddingPair() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.embeddingPairs >= b.maxEmbeddingPairs {
		return false
	}
	b.embeddingPairs++
	return true
}

func (b *RunBudget) TryUseLLMAssessment() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.llmAssessments >= b.maxLLMAssessments {
		return false
	}
	b.llmAssessments++
	return true
}

func (b *RunBudget) Snapshot() (embeddingPairs int, llmAssessments int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.embeddingPairs, b.llmAssessments
}
