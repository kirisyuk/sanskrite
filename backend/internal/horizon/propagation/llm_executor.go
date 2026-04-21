package propagation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// PropagationLLMTask represents a task for LLM-driven propagation decisions.
type PropagationLLMTask struct {
	TaskID          string                 `json:"taskId"`
	TenantID        string                 `json:"tenantId"`
	FragmentID      string                 `json:"fragmentId"`
	MetricName      string                 `json:"metricName"`
	InputSignals    []SignalSummary        `json:"inputSignals"`    // Summarized signals for context
	CurrentValue    float64                `json:"currentValue"`
	ProposedDelta   float64                `json:"proposedDelta"`
	CurrentDepth    int                    `json:"currentDepth"`
	MaxDepth        int                    `json:"maxDepth"`
	DampingFactor   float64                `json:"dampingFactor"`
	UpstreamPath    []string               `json:"upstreamPath"`    // Fragment IDs in propagation chain
	CreatedAt       time.Time              `json:"createdAt"`
}

// SignalSummary is a compact representation of a signal for LLM context.
type SignalSummary struct {
	SignalID     string  `json:"signalId"`
	SignalType   string  `json:"signalType"`
	Value        float64 `json:"value"`
	Confidence   float64 `json:"confidence"`
	SourcePath   string  `json:"sourcePath"`
}

// PropagationLLMResponse is the structured response from LLM.
type PropagationLLMResponse struct {
	SuggestedDelta      float64  `json:"suggestedDelta"`
	Confidence          float64  `json:"confidence"`
	Reasoning           string   `json:"reasoning"`
	ContinuePropagation bool     `json:"continuePropagation"`
	AffectedMetrics     []string `json:"affectedMetrics"` // Other metrics that should be updated
}

// PropagationLLMExecutor executes LLM-based propagation decisions.
type PropagationLLMExecutor struct {
	aiExecutor    *ai.AITaskExecutor
	fragmentsRepo *repo.HorizonFragmentsRepository
	damping       *WaveDamping
}

// NewPropagationLLMExecutor creates a new PropagationLLMExecutor.
func NewPropagationLLMExecutor(
	aiExecutor *ai.AITaskExecutor,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	damping *WaveDamping,
) *PropagationLLMExecutor {
	return &PropagationLLMExecutor{
		aiExecutor:    aiExecutor,
		fragmentsRepo: fragmentsRepo,
		damping:       damping,
	}
}

// Execute executes a propagation LLM task.
func (e *PropagationLLMExecutor) Execute(ctx context.Context, task *PropagationLLMTask) error {
	// Check damping: if currentDepth >= maxDepth, skip
	if task.CurrentDepth >= task.MaxDepth {
		return nil // Damping rule: don't process beyond max depth
	}

	// Build prompt
	prompt := e.buildPrompt(task)

	// Call LLM with min_capability_score=6
	aiTask := &ai.AITask{
		TaskType: ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			MinCapabilityScore: 6,
			TextGenerationCapability: true,
		},
		Verifier: ai.NewGenericVerifier(func(r PropagationLLMResponse) error {
			if math.IsNaN(r.SuggestedDelta) || math.IsInf(r.SuggestedDelta, 0) {
				return fmt.Errorf("invalid suggestedDelta")
			}
			if r.Confidence < 0 || r.Confidence > 1 {
				return fmt.Errorf("confidence out of range: %v", r.Confidence)
			}
			return nil
		}),
	}

	resultInterface, err := e.aiExecutor.ExecuteStructuredTask(ctx, aiTask)
	if err != nil {
		return fmt.Errorf("LLM execution failed: %w", err)
	}

	// Parse LLM response - ExecuteStructuredTask returns the verifier-parsed result
	response, ok := resultInterface.(PropagationLLMResponse)
	if !ok {
		return fmt.Errorf("unexpected result type from LLM: %T", resultInterface)
	}

	// Apply damping to LLM's suggested delta
	dampedDelta := response.SuggestedDelta * task.DampingFactor

	// Skip insignificant updates to avoid noise
	if math.Abs(dampedDelta) < 0.001 {
		return nil // Delta too small, skip update
	}

	// Update fragment
	fragment, err := e.fragmentsRepo.Get(ctx, task.FragmentID)
	if err != nil {
		return fmt.Errorf("fragment not found: %w", err)
	}

	if fragment.State == nil {
		fragment.State = make(map[string]models.MetricValue)
	}

	metric := fragment.State[task.MetricName]
	metric.Value += dampedDelta
	metric.Provenance.Method = "propagation_llm"
	metric.Provenance.SourceIDs = append(metric.Provenance.SourceIDs, task.TaskID)
	fragment.State[task.MetricName] = metric

	if err := e.fragmentsRepo.Update(ctx, fragment); err != nil {
		return fmt.Errorf("failed to update fragment: %w", err)
	}

	// If LLM suggests continuing propagation and depth allows, create child tasks
	// This would be handled by a Cloud Tasks queue dispatcher (not implemented here)
	if response.ContinuePropagation && task.CurrentDepth+1 < task.MaxDepth {
		// TODO: Create child PropagationLLMTasks for downstream fragments
		// This requires integration with Cloud Tasks queue: propagation-llm
	}

	return nil
}

// buildPrompt constructs the prompt for LLM fallback.
func (e *PropagationLLMExecutor) buildPrompt(task *PropagationLLMTask) string {
	signalsSummary := ""
	for i, sig := range task.InputSignals {
		if i > 0 {
			signalsSummary += "\n"
		}
		signalsSummary += fmt.Sprintf("- %s (%s): value=%.3f confidence=%.3f source=%s",
			sig.SignalID, sig.SignalType, sig.Value, sig.Confidence, sig.SourcePath)
	}

	upstreamPathStr := ""
	for i, fid := range task.UpstreamPath {
		if i > 0 {
			upstreamPathStr += " -> "
		}
		upstreamPathStr += fid
	}

	return fmt.Sprintf(`You are a propagation analysis agent for Horizon.

TASK: Determine how metric changes should propagate through the knowledge graph.

CURRENT STATE:
- Fragment: %s
- Metric: %s
- Current Value: %.3f
- Proposed Delta: %.3f

INPUT SIGNALS:
%s

PROPAGATION CONTEXT:
- Depth: %d/%d
- Upstream Path: %s
- Damping Factor: %.3f

RULES:
1. Consider the semantic meaning of the metric and fragment type.
2. Proposed delta has already been damped; you may adjust further or confirm.
3. If delta is insignificant (< 0.01 after your adjustment), set suggestedDelta to 0.
4. Explain your reasoning briefly.

OUTPUT (JSON):
{
  "suggestedDelta": <float>,
  "confidence": <0..1>,
  "reasoning": "<string>",
  "continuePropagation": <bool>,
  "affectedMetrics": ["<other_metric_names_if_any>"]
}`, task.FragmentID, task.MetricName, task.CurrentValue, task.ProposedDelta,
		signalsSummary, task.CurrentDepth, task.MaxDepth, upstreamPathStr, task.DampingFactor)
}

