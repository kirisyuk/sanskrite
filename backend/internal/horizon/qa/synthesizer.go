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
// scopeFragments (canonicalPath -> fragmentID) is used for post-synthesis evidence validation;
// pass nil to skip validation.
func (s *AnswerSynthesizer) Synthesize(ctx context.Context, contextPrompt string, plan *horizonmodels.QuestionPlan, simResult *horizonmodels.SimulationResult, scopeFragments map[string]string) (*horizonmodels.QAAnswer, error) {
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
	if es, ok := m["executiveSummary"].(string); ok {
		answer.ExecutiveSummary = strings.TrimSpace(es)
	}
	if rl, ok := m["riskLevel"].(string); ok {
		answer.RiskLevel = strings.ToUpper(strings.TrimSpace(rl))
	}
	if rs, ok := m["riskScenario"].(string); ok {
		answer.RiskScenario = strings.TrimSpace(rs)
	}
	if recs, ok := m["recommendations"].([]interface{}); ok {
		for _, r := range recs {
			if s, ok := r.(string); ok {
				answer.Recommendations = append(answer.Recommendations, strings.TrimSpace(s))
			}
		}
	}
	if assumptions, ok := m["assumptions"].([]interface{}); ok {
		for _, item := range assumptions {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				answer.Assumptions = append(answer.Assumptions, strings.TrimSpace(s))
			}
		}
	}
	// Fallback for Executive Summary if LLM missed it
	if answer.ExecutiveSummary == "" && answer.Text != "" {
		parts := splitIntoSentences(answer.Text)
		if len(parts) > 0 {
			answer.ExecutiveSummary = parts[0]
		}
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

				// Post-processing: Filter out evidence with 0 confidence or "simulation" in sourceRef
				// unless it's explicitly valid.
				if ev.Confidence <= 0.01 {
					// Fallback: if sourceRef looks real (e.g. [L1]), boost confidence to default
					if strings.HasPrefix(ev.SourceRef, "[L") || strings.HasPrefix(ev.SourceRef, "L") {
						ev.Confidence = 0.8 // High confidence for leaf evidence
					} else if strings.Contains(strings.ToLower(ev.Summary), "simulation") {
						// It's just a simulation restatement — penalize or skip
						ev.Confidence = 0.1
					} else {
						ev.Confidence = 0.5 // Default medium confidence
					}
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
	answer.FirstOrderEffects = parseEffects(m["firstOrderEffects"])
	answer.SecondOrderEffects = parseEffects(m["secondOrderEffects"])
	answer.Watchpoints = parseWatchpoints(m["watchpoints"])

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

	if len(scopeFragments) > 0 {
		answer.Evidence = ValidateEvidence(answer.Evidence, scopeFragments)
	}
	applyScenarioFallbacks(answer, simResult)

	return answer, nil
}

// ValidateEvidence checks each evidence path against actual scope fragments.
// Paths not found in scope get " [unverified]" appended to Summary and Confidence reduced to 0.3.
func ValidateEvidence(evidence []horizonmodels.AnswerEvidence, scopeFragments map[string]string) []horizonmodels.AnswerEvidence {
	if len(evidence) == 0 || len(scopeFragments) == 0 {
		return evidence
	}
	for i := range evidence {
		if evidence[i].CanonicalPath == "" {
			continue
		}
		if _, ok := scopeFragments[evidence[i].CanonicalPath]; !ok {
			if !strings.HasSuffix(evidence[i].Summary, " [unverified]") {
				evidence[i].Summary += " [unverified]"
			}
			evidence[i].Confidence = 0.3
		}
	}
	return evidence
}

func splitIntoSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	delimiter := func(r rune) bool {
		switch r {
		case '.', '!', '?', ';', '\n':
			return true
		default:
			return false
		}
	}
	raw := strings.FieldsFunc(text, delimiter)
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func parseEffects(raw interface{}) []horizonmodels.QAEffect {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]horizonmodels.QAEffect, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		eff := horizonmodels.QAEffect{}
		if v, ok := m["summary"].(string); ok {
			eff.Summary = strings.TrimSpace(v)
		}
		if v, ok := m["horizon"].(string); ok {
			eff.Horizon = strings.TrimSpace(v)
		}
		if v, ok := m["severity"].(string); ok {
			eff.Severity = strings.ToUpper(strings.TrimSpace(v))
		}
		if v, ok := m["mechanism"].(string); ok {
			eff.Mechanism = strings.TrimSpace(v)
		}
		if entities, ok := m["affectedEntities"].([]interface{}); ok {
			for _, entity := range entities {
				if s, ok := entity.(string); ok && strings.TrimSpace(s) != "" {
					eff.AffectedEntities = append(eff.AffectedEntities, strings.TrimSpace(s))
				}
			}
		}
		if eff.Summary == "" && len(eff.AffectedEntities) == 0 {
			continue
		}
		out = append(out, eff)
	}
	return out
}

func parseWatchpoints(raw interface{}) []horizonmodels.QAWatchpoint {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]horizonmodels.QAWatchpoint, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		wp := horizonmodels.QAWatchpoint{}
		if v, ok := m["signal"].(string); ok {
			wp.Signal = strings.TrimSpace(v)
		}
		if v, ok := m["whyItMatters"].(string); ok {
			wp.WhyItMatters = strings.TrimSpace(v)
		}
		if v, ok := m["expectedWithin"].(string); ok {
			wp.ExpectedWithin = strings.TrimSpace(v)
		}
		if wp.Signal == "" {
			continue
		}
		out = append(out, wp)
	}
	return out
}

func applyScenarioFallbacks(answer *horizonmodels.QAAnswer, simResult *horizonmodels.SimulationResult) {
	if answer == nil || simResult == nil {
		return
	}
	if len(answer.FirstOrderEffects) == 0 || len(answer.SecondOrderEffects) == 0 {
		first, second := fallbackEffectsFromSimulation(simResult)
		if len(answer.FirstOrderEffects) == 0 {
			answer.FirstOrderEffects = first
		}
		if len(answer.SecondOrderEffects) == 0 {
			answer.SecondOrderEffects = second
		}
	}
	if len(answer.Watchpoints) == 0 {
		answer.Watchpoints = fallbackWatchpoints(simResult)
	}
	if answer.RiskScenario == "" {
		if len(answer.FirstOrderEffects) > 0 && len(answer.SecondOrderEffects) > 0 {
			answer.RiskScenario = answer.FirstOrderEffects[0].Summary + " Then " + answer.SecondOrderEffects[0].Summary
		} else if len(answer.FirstOrderEffects) > 0 {
			answer.RiskScenario = answer.FirstOrderEffects[0].Summary
		}
	}
	if answer.Text == "" {
		switch {
		case answer.RiskScenario != "":
			answer.Text = answer.RiskScenario
		case answer.ExecutiveSummary != "":
			answer.Text = answer.ExecutiveSummary
		}
	}
}

func fallbackEffectsFromSimulation(simResult *horizonmodels.SimulationResult) ([]horizonmodels.QAEffect, []horizonmodels.QAEffect) {
	first := make([]horizonmodels.QAEffect, 0, 3)
	second := make([]horizonmodels.QAEffect, 0, 3)
	for _, ni := range simResult.NodeImpacts {
		axis, amp := strongestImpactAxis(ni.AxisAmplitudes)
		summary := humanizeImpactSummary(ni, axis, amp)
		effect := horizonmodels.QAEffect{
			Summary:          summary,
			Horizon:          horizonForHops(ni.Hops),
			Severity:         severityForAmplitude(amp),
			AffectedEntities: compactAffectedEntities(ni),
			Mechanism:        axis,
		}
		if ni.Hops <= 1 {
			if len(first) < 3 {
				first = append(first, effect)
			}
			continue
		}
		if len(second) < 3 {
			second = append(second, effect)
		}
	}
	return first, second
}

func fallbackWatchpoints(simResult *horizonmodels.SimulationResult) []horizonmodels.QAWatchpoint {
	out := make([]horizonmodels.QAWatchpoint, 0, 3)
	for _, le := range simResult.LeafEvidence {
		if len(out) >= 3 {
			break
		}
		signal := strings.TrimSpace(le.Summary)
		if signal == "" {
			signal = strings.TrimSpace(le.Axis)
		}
		if signal == "" {
			continue
		}
		out = append(out, horizonmodels.QAWatchpoint{
			Signal:         signal,
			WhyItMatters:   "Confirms whether the projected impact is already materializing.",
			ExpectedWithin: "short_term",
		})
	}
	return out
}

func strongestImpactAxis(axisAmplitudes map[string]float64) (string, float64) {
	var (
		bestAxis string
		bestAmp  float64
	)
	for axis, amp := range axisAmplitudes {
		if bestAxis == "" || amp > bestAmp {
			bestAxis = axis
			bestAmp = amp
		}
	}
	return bestAxis, bestAmp
}

func humanizeImpactSummary(ni horizonmodels.NodeImpact, axis string, amp float64) string {
	name := strings.TrimSpace(ni.Name)
	if name == "" {
		name = strings.TrimSpace(ni.CanonicalPath)
	}
	if name == "" {
		name = "Affected node"
	}
	return fmt.Sprintf("%s is likely to face %s pressure through %s", name, strings.ToLower(severityForAmplitude(amp)), axis)
}

func compactAffectedEntities(ni horizonmodels.NodeImpact) []string {
	if strings.TrimSpace(ni.Name) != "" {
		return []string{strings.TrimSpace(ni.Name)}
	}
	if strings.TrimSpace(ni.CanonicalPath) != "" {
		return []string{strings.TrimSpace(ni.CanonicalPath)}
	}
	return nil
}

func horizonForHops(hops int) string {
	switch {
	case hops <= 0:
		return "immediate"
	case hops == 1:
		return "short_term"
	case hops == 2:
		return "medium_term"
	default:
		return "long_term"
	}
}

func severityForAmplitude(amp float64) string {
	switch {
	case amp >= 0.75:
		return "CRITICAL"
	case amp >= 0.40:
		return "HIGH"
	case amp >= 0.15:
		return "MODERATE"
	default:
		return "LOW"
	}
}
