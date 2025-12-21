package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// ResolverAgent разрешает конфликты между Scout и Critic
type ResolverAgent struct {
	executor *ai.AITaskExecutor
}

// NewResolverAgent создает новый Resolver Agent
func NewResolverAgent(executor *ai.AITaskExecutor) *ResolverAgent {
	return &ResolverAgent{
		executor: executor,
	}
}

// ResolutionRequest запрос на разрешение конфликта
type ResolutionRequest struct {
	ScoutData       interface{}          `json:"scoutData"`
	CriticArguments []ValidationConflict `json:"criticArguments"`
	SourceContent   string               `json:"sourceContent"`
}

// ResolutionResult результат разрешения конфликта
type ResolutionResult struct {
	Verdict      string      `json:"verdict"` // "Verified", "Low_Confidence", "Rejected"
	Reasoning    string      `json:"reasoning"`
	ResolvedData interface{} `json:"resolvedData,omitempty"`
	Confidence   float64     `json:"confidence"` // 0..1
}

// ValidationConflict используется тип из critic_agent.go

// Resolve разрешает конфликт между Scout и Critic
func (a *ResolverAgent) Resolve(ctx context.Context, req ResolutionRequest) (*ResolutionResult, error) {
	prompt := a.buildResolutionPrompt(req)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         8000,
			MaxOutputSize:            2048,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve conflict: %w", err)
	}

	// Преобразуем результат в ResolutionResult
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var resolutionResult ResolutionResult
	if err := json.Unmarshal(jsonData, &resolutionResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resolution result: %w", err)
	}

	return &resolutionResult, nil
}

// buildResolutionPrompt строит промпт для разрешения конфликта
func (a *ResolverAgent) buildResolutionPrompt(req ResolutionRequest) string {
	scoutJSON, _ := json.MarshalIndent(req.ScoutData, "", "  ")
	conflictsJSON, _ := json.MarshalIndent(req.CriticArguments, "", "  ")
	return fmt.Sprintf(`You are a resolution agent (Resolver/Judge) for a risk analysis system. Your task is to resolve conflicts between extracted data (Scout) and validation results (Critic).

SCOUT DATA (extracted):
%s

CRITIC ARGUMENTS (conflicts):
%s

SOURCE CONTENT:
%s

TASK: Analyze the conflicts and make a final decision. Consider:
1. Which side (Scout or Critic) has stronger evidence
2. Whether conflicts are critical or minor
3. Whether data can be corrected or should be rejected

Return a JSON object with:
- verdict: "Verified" (data is correct), "Low_Confidence" (data has issues but usable), or "Rejected" (data is invalid)
- reasoning: Explanation of your decision
- resolvedData: Corrected data object (if verdict is Verified or Low_Confidence)
- confidence: float (0..1) indicating confidence in the resolution

Return ONLY valid JSON. Do not include markdown code fences.`, string(scoutJSON), string(conflictsJSON), req.SourceContent)
}
