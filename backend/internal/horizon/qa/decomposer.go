package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// QuestionDecomposer decomposes a natural language question into a structured QuestionPlan.
type QuestionDecomposer struct {
	executor *ai.AITaskExecutor
}

// NewQuestionDecomposer creates a new QuestionDecomposer.
func NewQuestionDecomposer(executor *ai.AITaskExecutor) *QuestionDecomposer {
	return &QuestionDecomposer{executor: executor}
}

// DecomposeContext provides lens context for decomposition.
type DecomposeContext struct {
	ScopePaths       []string // Top 30 canonical paths in lens scope
	DomainModes      []string
	ActiveSignals    []string // Top 5 signals by amplitude (human-readable)
	ArtifactSummaries []string // MaterializedArtifact display names
}

// Decompose converts a natural language question into a QuestionPlan.
func (d *QuestionDecomposer) Decompose(ctx context.Context, question string, lensCtx *DecomposeContext) (*horizonmodels.QuestionPlan, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if lensCtx == nil {
		lensCtx = &DecomposeContext{}
	}

	prompt := d.buildPrompt(question, lensCtx)
	verifier := ai.NewGenericVerifier(func(p horizonmodels.QuestionPlan) error {
		if strings.TrimSpace(p.AnswerMode) == "" {
			return fmt.Errorf("answerMode is required")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			MinCapabilityScore:        6,
			MinContextWindow:          8000,
			MaxOutputSize:             2048,
		},
		Verifier: verifier,
	}

	result, err := d.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("question decomposition failed: %w", err)
	}

	plan, ok := result.(horizonmodels.QuestionPlan)
	if !ok {
		// Try to unmarshal from map
		b, _ := json.Marshal(result)
		var p horizonmodels.QuestionPlan
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("failed to parse QuestionPlan: %w", err)
		}
		plan = p
	}

	// Normalize
	plan.NormalizedQuestion = strings.TrimSpace(plan.NormalizedQuestion)
	if plan.NormalizedQuestion == "" {
		plan.NormalizedQuestion = question
	}
	if plan.TargetFilter.MaxNodes <= 0 {
		plan.TargetFilter.MaxNodes = 20
	}

	return &plan, nil
}

func (d *QuestionDecomposer) buildPrompt(question string, lensCtx *DecomposeContext) string {
	var scopeStr string
	if len(lensCtx.ScopePaths) > 0 {
		limit := 30
		if len(lensCtx.ScopePaths) < limit {
			limit = len(lensCtx.ScopePaths)
		}
		scopeStr = strings.Join(lensCtx.ScopePaths[:limit], ", ")
	} else {
		scopeStr = "(no scope)"
	}

	var domainStr string
	if len(lensCtx.DomainModes) > 0 {
		domainStr = strings.Join(lensCtx.DomainModes, ", ")
	} else {
		domainStr = "(none)"
	}

	var signalsStr string
	if len(lensCtx.ActiveSignals) > 0 {
		n := 5
		if len(lensCtx.ActiveSignals) < n {
			n = len(lensCtx.ActiveSignals)
		}
		signalsStr = strings.Join(lensCtx.ActiveSignals[:n], "; ")
	} else {
		signalsStr = "(none)"
	}

	var chainsStr string
	if len(lensCtx.ArtifactSummaries) > 0 {
		n := 5
		if len(lensCtx.ArtifactSummaries) < n {
			n = len(lensCtx.ArtifactSummaries)
		}
		chainsStr = strings.Join(lensCtx.ArtifactSummaries[:n], "; ")
	} else {
		chainsStr = "(none)"
	}

	return fmt.Sprintf(`You are decomposing a business question into a simulation plan.

LENS CONTEXT:
- Scope: %s
- Domain modes: %s
- Active signals: %s
- Available chains: %s

AVAILABLE AXES (12-axis basis):
- legal/access: sanctions, bans, license revocations
- legal/compliance: regulatory exposure, secondary sanctions
- financial/settlement: payment rails, FX, banking access
- financial/credit: credit risk, default exposure
- physical_flow/supply: material shortage or glut
- physical_flow/demand: demand surge or collapse
- logistics/capacity: route closures, port congestion
- logistics/latency: delays, lead time increases
- operational/continuity: strikes, outages, disasters
- info/uncertainty: regulatory fog, ambiguous timelines
- info/reputation: ESG signals, brand impact
- geo/conflict: geopolitical instability, conflict zones

QUESTION: "%s"

Return a JSON object with this exact structure (no markdown, no code fences):
{
  "normalizedQuestion": "English-normalized form of the question",
  "answerMode": "impact_assessment|blocker_analysis|alternative_sourcing|risk_forecast|dependency_audit|out_of_scope",
  "timeHorizon": "immediate|short_term|medium_term|long_term",
  "hypotheticalSignals": [
    {
      "axis": "axis from 12-axis basis",
      "mechanism": "specific cause e.g. vat_increase, port_closure",
      "amplitude": 0.0-1.0,
      "polarity": "risk|opportunity",
      "seedHints": ["canonical path patterns from scope"],
      "timeHorizon": "immediate|short_term|medium_term|long_term",
      "modality": "projected|actual"
    }
  ],
  "targetFilter": {
    "observePathPatterns": ["e.g. /product/lithium-*", "/org/*/battery*"],
    "observeAxes": ["axes that matter"],
    "maxNodes": 20
  },
  "contextHints": ["supply_chains", "governance", "leaf_signals", "market_data"]
}

Rules:
- For "what would happen if X" → create HypotheticalSignal with modality=projected
- For "what are the current risks/blockers" → no signals (empty hypotheticalSignals), just targetFilter
- If question is unrelated to lens scope → answerMode: "out_of_scope"
- SeedHints: use canonical path patterns from lens scope (e.g. /org/id/*, /deposit/nickel/*)
- Amplitude: 0.3=minor, 0.5=moderate, 0.7=significant, 0.9=severe
- answerMode: pick the best fit for the question
`, scopeStr, domainStr, signalsStr, chainsStr, question)
}

