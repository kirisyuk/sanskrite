package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// AnswerSynthesizer generates a grounded QAAnswer from simulation context.
type AnswerSynthesizer struct {
	executor *ai.AITaskExecutor
}

// NewAnswerSynthesizer creates a new AnswerSynthesizer.
func NewAnswerSynthesizer(executor *ai.AITaskExecutor) *AnswerSynthesizer {
	return &AnswerSynthesizer{executor: executor}
}

// Synthesize generates a QAAnswer from the context.
func (s *AnswerSynthesizer) Synthesize(ctx context.Context, contextPrompt string, plan *horizonmodels.QuestionPlan, simResult *horizonmodels.SimulationResult) (*horizonmodels.QAAnswer, error) {
	prompt := contextPrompt + "\n\nGenerate the answer JSON now."

	verifier := ai.NewGenericVerifier[map[string]interface{}](nil)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MinContextWindow:         8000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	result, err := s.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		b, _ := json.Marshal(result)
		var m2 map[string]interface{}
		if err := json.Unmarshal(b, &m2); err != nil {
			return nil, fmt.Errorf("failed to parse synthesis result: %w", err)
		}
		m = m2
	}

	answer := &horizonmodels.QAAnswer{
		AnswerMode:    plan.AnswerMode,
		TimeHorizon:   plan.TimeHorizon,
		SimulatedAxes: plan.TargetFilter.ObserveAxes,
	}

	if t, ok := m["text"].(string); ok {
		answer.Text = strings.TrimSpace(t)
	}
	if c, ok := m["confidence"].(float64); ok {
		answer.Confidence = c
	}

	// Highlighted nodes from simulation
	for _, ni := range simResult.NodeImpacts {
		if ni.FragmentID != "" {
			answer.HighlightedNodeIDs = append(answer.HighlightedNodeIDs, ni.FragmentID)
		}
	}
	answer.TracePaths = simResult.TracePaths

	// Evidence
	if evArr, ok := m["evidence"].([]interface{}); ok {
		for _, e := range evArr {
			if em, ok := e.(map[string]interface{}); ok {
				ev := horizonmodels.AnswerEvidence{}
				if v, ok := em["type"].(string); ok {
					ev.Type = v
				}
				if v, ok := em["canonicalPath"].(string); ok {
					ev.CanonicalPath = v
				}
				if v, ok := em["summary"].(string); ok {
					ev.Summary = v
				}
				if v, ok := em["confidence"].(float64); ok {
					ev.Confidence = v
				}
				if v, ok := em["sourceRef"].(string); ok {
					ev.SourceRef = v
				}
				answer.Evidence = append(answer.Evidence, ev)
			}
		}
	}

	// ImpactBreakdown — prefer LLM output; fallback to simulation (NodeImpacts)
	if ibArr, ok := m["impactBreakdown"].([]interface{}); ok {
		for _, i := range ibArr {
			if im, ok := i.(map[string]interface{}); ok {
				item := horizonmodels.ImpactItem{}
				if v, ok := im["entity"].(string); ok {
					item.Entity = v
				}
				if v, ok := im["entityName"].(string); ok {
					item.EntityName = v
				}
				if v, ok := im["axis"].(string); ok {
					item.Axis = v
				}
				if v, ok := im["amplitude"].(float64); ok {
					item.Amplitude = v
				}
				if v, ok := im["mechanism"].(string); ok {
					item.Mechanism = v
				}
				if v, ok := im["hops"].(float64); ok {
					item.Hops = int(v)
				}
				answer.ImpactBreakdown = append(answer.ImpactBreakdown, item)
			}
		}
	}
	if len(answer.ImpactBreakdown) == 0 && len(simResult.NodeImpacts) > 0 {
		// Fallback: derive from simulation (plan: signals, not invented evidence)
		for _, ni := range simResult.NodeImpacts {
			for axis, amp := range ni.AxisAmplitudes {
				answer.ImpactBreakdown = append(answer.ImpactBreakdown, horizonmodels.ImpactItem{
					Entity:     ni.CanonicalPath,
					EntityName: ni.Name,
					Axis:       axis,
					Amplitude:  amp,
					Hops:       ni.Hops,
				})
			}
		}
	}

	// Alternatives
	if altArr, ok := m["alternatives"].([]interface{}); ok {
		for _, a := range altArr {
			if am, ok := a.(map[string]interface{}); ok {
				alt := horizonmodels.QAAlternative{}
				if v, ok := am["entity"].(string); ok {
					alt.Entity = v
				}
				if v, ok := am["entityName"].(string); ok {
					alt.EntityName = v
				}
				if v, ok := am["description"].(string); ok {
					alt.Description = v
				}
				if v, ok := am["tradeOff"].(string); ok {
					alt.TradeOff = v
				}
				answer.Alternatives = append(answer.Alternatives, alt)
			}
		}
	}

	// DataGaps
	if dgArr, ok := m["dataGaps"].([]interface{}); ok {
		for _, d := range dgArr {
			if dm, ok := d.(map[string]interface{}); ok {
				gap := horizonmodels.QADataGap{}
				if v, ok := dm["description"].(string); ok {
					gap.Description = v
				}
				if v, ok := dm["whyItMatters"].(string); ok {
					gap.WhyItMatters = v
				}
				answer.DataGaps = append(answer.DataGaps, gap)
			}
		}
	}

	return answer, nil
}
