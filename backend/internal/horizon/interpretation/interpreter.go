package interpretation

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/feedback"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// RiskInterpretation represents a human-readable interpretation of risk/opportunity signals.
type RiskInterpretation struct {
	NodeID        string             `json:"nodeId" firestore:"nodeId"`
	CanonicalPath string             `json:"canonicalPath" firestore:"canonicalPath"`
	Polarity      string             `json:"polarity" firestore:"polarity"` // "risk" | "opportunity"
	Summary       string             `json:"summary" firestore:"summary"`   // 2-3 sentence human-readable summary
	AxisBreakdown []AxisContribution `json:"axisBreakdown" firestore:"axisBreakdown"`
	TopEvents     []EventProvenance  `json:"topEvents" firestore:"topEvents"`
	Impact        ImpactEstimate     `json:"impact" firestore:"impact"`
	Mitigations   []Mitigation       `json:"mitigations" firestore:"mitigations"`
	Confidence    float64            `json:"confidence" firestore:"confidence"`
	GeneratedAt   time.Time          `json:"generatedAt" firestore:"generatedAt"`
	ExpiresAt     time.Time          `json:"expiresAt" firestore:"expiresAt"` // TTL for cache
}

// AxisContribution represents contribution from a specific axis.
type AxisContribution struct {
	Axis      string  `json:"axis" firestore:"axis"`
	Amplitude float64 `json:"amplitude" firestore:"amplitude"`
	Mechanism string  `json:"mechanism,omitempty" firestore:"mechanism,omitempty"`
}

// EventProvenance represents a top contributing event.
type EventProvenance struct {
	SignalID     string   `json:"signalId" firestore:"signalId"`
	EventSummary string   `json:"eventSummary" firestore:"eventSummary"`
	Path         []string `json:"path" firestore:"path"` // Propagation path
	Amplitude    float64  `json:"amplitude" firestore:"amplitude"`
	Hops         int      `json:"hops" firestore:"hops"`
}

// ImpactEstimate represents estimated impact.
type ImpactEstimate struct {
	Operational string `json:"operational,omitempty" firestore:"operational,omitempty"`
	Financial   string `json:"financial,omitempty" firestore:"financial,omitempty"`
	Timeline    string `json:"timeline,omitempty" firestore:"timeline,omitempty"`
}

// Mitigation represents a recommended action.
type Mitigation struct {
	Action      string `json:"action" firestore:"action"`
	Description string `json:"description,omitempty" firestore:"description,omitempty"`
}

// Interpreter generates human-readable risk/opportunity interpretations using LLM.
type Interpreter struct {
	aiExecutor         *ai.AITaskExecutor
	fragmentsRepo      *repo.HorizonFragmentsRepository
	signalsRepo        SignalsRepo        // For retrieving signal details
	eventFactsResolver EventFactsResolver // Optional: resolve SourceEventID to fact summary for prompt
	feedbackEmitter    feedback.Emitter
}

// SignalsRepo provides access to signal descriptors.
type SignalsRepo interface {
	GetSignal(ctx context.Context, signalID string) (*horizonmodels.SignalDescriptor, error)
}

// NewInterpreter creates a new Interpreter.
func NewInterpreter(
	aiExecutor *ai.AITaskExecutor,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	signalsRepo SignalsRepo,
) *Interpreter {
	return &Interpreter{
		aiExecutor:    aiExecutor,
		fragmentsRepo: fragmentsRepo,
		signalsRepo:   signalsRepo,
	}
}

// WithEventFactsResolver sets the optional resolver for SourceEventID → fact summary (traceability).
func (i *Interpreter) WithEventFactsResolver(r EventFactsResolver) *Interpreter {
	i.eventFactsResolver = r
	return i
}

func (i *Interpreter) WithFeedbackEmitter(emitter feedback.Emitter) *Interpreter {
	i.feedbackEmitter = emitter
	return i
}

// Interpret generates an interpretation for a node.
func (i *Interpreter) Interpret(ctx context.Context, request *InterpretationRequest) (*RiskInterpretation, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	log.Printf("%s start fragmentId=%s polarity=%s", signalsys.LogInterpret, strings.TrimSpace(request.FragmentID), strings.TrimSpace(request.Polarity))

	// Get fragment
	fragment, err := i.fragmentsRepo.Get(ctx, request.FragmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fragment: %w", err)
	}

	// Build metrics breakdown
	metrics := make(map[string]float64)
	axisBreakdown := make([]AxisContribution, 0)
	for metricPath, value := range fragment.State {
		if !metricHasPolarity(metricPath, request.Polarity) {
			continue
		}
		metrics[metricPath] = value.Value
		axis, mech := metricExtractAxisAndMechanism(metricPath)
		if axis != "" {
			axisBreakdown = append(axisBreakdown, AxisContribution{
				Axis:      axis,
				Amplitude: value.Value,
				Mechanism: mech,
			})
		}
	}

	// Build top events from provenance (simplified - would need full provenance tracking)
	topEvents := i.buildTopEvents(ctx, fragment, request.Polarity)

	// Build LLM prompt
	prompt := i.buildInterpretationPrompt(fragment, request, metrics, axisBreakdown, topEvents)

	// Execute LLM task
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            4096,
			MinContextWindow:         8000,
		},
		Verifier: ai.NewMapVerifier([]string{"summary", "impact", "mitigations"}, nil),
	}

	parsed, err := i.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM interpretation failed: %w", err)
	}

	// Parse result
	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	interpretation := &RiskInterpretation{
		NodeID:        request.FragmentID,
		CanonicalPath: request.CanonicalPath,
		Polarity:      request.Polarity,
		AxisBreakdown: axisBreakdown,
		TopEvents:     topEvents,
		Confidence:    0.8, // Default confidence
		GeneratedAt:   time.Now(),
		ExpiresAt:     time.Now().Add(24 * time.Hour), // 24h TTL
	}

	// Parse summary
	if summary, ok := resultMap["summary"].(string); ok {
		interpretation.Summary = summary
	}

	// Parse impact
	if impactRaw, ok := resultMap["impact"].(map[string]interface{}); ok {
		if op, ok := impactRaw["operational"].(string); ok {
			interpretation.Impact.Operational = op
		}
		if fin, ok := impactRaw["financial"].(string); ok {
			interpretation.Impact.Financial = fin
		}
		if timeline, ok := impactRaw["timeline"].(string); ok {
			interpretation.Impact.Timeline = timeline
		}
	}

	// Parse mitigations
	if mitigationsRaw, ok := resultMap["mitigations"].([]interface{}); ok {
		interpretation.Mitigations = make([]Mitigation, 0, len(mitigationsRaw))
		for _, mRaw := range mitigationsRaw {
			if mMap, ok := mRaw.(map[string]interface{}); ok {
				mitigation := Mitigation{}
				if action, ok := mMap["action"].(string); ok {
					mitigation.Action = action
				}
				if desc, ok := mMap["description"].(string); ok {
					mitigation.Description = desc
				}
				if mitigation.Action != "" {
					interpretation.Mitigations = append(interpretation.Mitigations, mitigation)
				}
			}
		}
	}

	log.Printf("%s done fragmentId=%s polarity=%s axes=%d topEvents=%d", signalsys.LogInterpret, strings.TrimSpace(request.FragmentID), strings.TrimSpace(request.Polarity), len(axisBreakdown), len(topEvents))
	i.emitFeedbackIfNeeded(ctx, request, interpretation)
	return interpretation, nil
}

func (i *Interpreter) emitFeedbackIfNeeded(ctx context.Context, request *InterpretationRequest, interpretation *RiskInterpretation) {
	if i == nil || i.feedbackEmitter == nil || request == nil || interpretation == nil {
		return
	}
	maxAxisAmp := 0.0
	for _, a := range interpretation.AxisBreakdown {
		if a.Amplitude > maxAxisAmp {
			maxAxisAmp = a.Amplitude
		}
	}
	if interpretation.Confidence >= 0.55 || maxAxisAmp < 0.7 {
		return
	}
	_ = i.feedbackEmitter.Emit(ctx, feedback.Event{
		Type:          "re_evaluate",
		FragmentID:    strings.TrimSpace(request.FragmentID),
		CanonicalPath: strings.TrimSpace(request.CanonicalPath),
		Polarity:      strings.TrimSpace(request.Polarity),
		Reason:        "low_confidence_high_impact_combination",
		Payload: map[string]any{
			"interpretationConfidence": interpretation.Confidence,
			"maxAxisAmplitude":         maxAxisAmp,
		},
		EmittedAt: time.Now().UTC(),
	})
}

// buildTopEvents builds top contributing events from fragment provenance.
func (i *Interpreter) buildTopEvents(ctx context.Context, fragment *horizonmodels.HorizonFragment, polarity string) []EventProvenance {
	events := make([]EventProvenance, 0)

	// Prefer structured contributions (top-K provenance).
	type scored struct {
		ev EventProvenance
	}
	acc := make([]scored, 0)
	seen := make(map[string]bool) // signalId|metricPath

	for metricPath, mv := range fragment.State {
		if !metricHasPolarity(metricPath, polarity) {
			continue
		}

		for _, c := range mv.Provenance.Contributions {
			key := c.SignalID + "|" + metricPath
			if seen[key] {
				continue
			}
			seen[key] = true
			eventSummary := strings.TrimSpace(c.SourceEventID)
			if eventSummary != "" && i.eventFactsResolver != nil {
				if resolved, err := i.eventFactsResolver.ResolveEventSummary(ctx, c.SourceEventID); err == nil && resolved != "" {
					eventSummary = resolved
				}
			}
			acc = append(acc, scored{
				ev: EventProvenance{
					SignalID:     c.SignalID,
					EventSummary: eventSummary,
					Path:         c.Path,
					Amplitude:    c.Amplitude,
					Hops:         c.Hops,
				},
			})
		}
	}

	// Fallback: if contributions are absent, try legacy sourceIDs + optional signalsRepo
	if len(acc) == 0 {
		for metricPath, value := range fragment.State {
			if !metricHasPolarity(metricPath, polarity) {
				continue
			}
			for _, sourceID := range value.Provenance.SourceIDs {
				if i.signalsRepo == nil {
					continue
				}
				signal, err := i.signalsRepo.GetSignal(ctx, sourceID)
				if err == nil && signal != nil {
					eventSummary := strings.TrimSpace(signal.FactID)
					if eventSummary != "" && i.eventFactsResolver != nil {
						if resolved, err := i.eventFactsResolver.ResolveEventSummary(ctx, signal.FactID); err == nil && resolved != "" {
							eventSummary = resolved
						}
					}
					acc = append(acc, scored{
						ev: EventProvenance{
							SignalID:     signal.ID,
							EventSummary: eventSummary,
							Amplitude:    value.Value,
						},
					})
				}
			}
		}
	}

	sort.Slice(acc, func(i, j int) bool {
		return acc[i].ev.Amplitude > acc[j].ev.Amplitude
	})

	for _, it := range acc {
		events = append(events, it.ev)
		if len(events) >= 5 {
			break
		}
	}
	return events
}

// InterpretFromNodeImpact produces human-readable interpretation from QA simulation output.
// Humanization only at the end: takes NodeImpact (12-axis amplitudes from propagation) and
// leaf evidence, returns RiskInterpretation. No fragment.State — uses simulation data.
func (i *Interpreter) InterpretFromNodeImpact(
	ctx context.Context,
	fragment *horizonmodels.HorizonFragment,
	ni *horizonmodels.NodeImpact,
	leafEvidenceForNode []horizonmodels.LeafSignalEvidence,
	polarity string,
) (*RiskInterpretation, error) {
	if ni == nil || len(ni.AxisAmplitudes) == 0 {
		return nil, fmt.Errorf("node impact is nil or has no axis amplitudes")
	}
	if polarity == "" {
		polarity = "risk"
	}

	nodeLabel := ni.Name
	if nodeLabel == "" && fragment != nil && fragment.Labels != nil {
		if lbl, ok := fragment.Labels["en"]; ok && strings.TrimSpace(lbl) != "" {
			nodeLabel = strings.TrimSpace(lbl)
		}
	}
	if nodeLabel == "" {
		nodeLabel = ni.CanonicalPath
	}

	// Build axisBreakdown from simulation output (12-axis amplitudes)
	axisBreakdown := make([]AxisContribution, 0, len(ni.AxisAmplitudes))
	metrics := make(map[string]float64)
	for axis, amp := range ni.AxisAmplitudes {
		axisBreakdown = append(axisBreakdown, AxisContribution{Axis: axis, Amplitude: amp})
		metrics["/metric/"+polarity+"/"+axis] = amp
	}
	sort.Slice(axisBreakdown, func(a, b int) bool { return axisBreakdown[a].Amplitude > axisBreakdown[b].Amplitude })

	// Build topEvents from leaf evidence (physical signals at this node)
	topEvents := make([]EventProvenance, 0, len(leafEvidenceForNode))
	for _, le := range leafEvidenceForNode {
		topEvents = append(topEvents, EventProvenance{
			SignalID:     le.SignalType,
			EventSummary: le.Summary,
			Amplitude:    le.Amplitude,
		})
	}

	prompt := i.buildInterpretationPromptFromData(nodeLabel, ni.CanonicalPath, polarity, metrics, axisBreakdown, topEvents)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            4096,
			MinContextWindow:         8000,
		},
		Verifier: ai.NewMapVerifier([]string{"summary", "impact", "mitigations"}, nil),
	}

	parsed, err := i.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM interpretation failed: %w", err)
	}

	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	interpretation := &RiskInterpretation{
		NodeID:        ni.FragmentID,
		CanonicalPath: ni.CanonicalPath,
		Polarity:      polarity,
		AxisBreakdown: axisBreakdown,
		TopEvents:     topEvents,
		Confidence:    0.8,
		GeneratedAt:   time.Now(),
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}

	if summary, ok := resultMap["summary"].(string); ok {
		interpretation.Summary = summary
	}
	if impactRaw, ok := resultMap["impact"].(map[string]interface{}); ok {
		if op, ok := impactRaw["operational"].(string); ok {
			interpretation.Impact.Operational = op
		}
		if fin, ok := impactRaw["financial"].(string); ok {
			interpretation.Impact.Financial = fin
		}
		if timeline, ok := impactRaw["timeline"].(string); ok {
			interpretation.Impact.Timeline = timeline
		}
	}
	if mitigationsRaw, ok := resultMap["mitigations"].([]interface{}); ok {
		for _, mRaw := range mitigationsRaw {
			if mMap, ok := mRaw.(map[string]interface{}); ok {
				mitigation := Mitigation{}
				if action, ok := mMap["action"].(string); ok {
					mitigation.Action = action
				}
				if desc, ok := mMap["description"].(string); ok {
					mitigation.Description = desc
				}
				if mitigation.Action != "" {
					interpretation.Mitigations = append(interpretation.Mitigations, mitigation)
				}
			}
		}
	}

	log.Printf("%s qa_interpret fragmentId=%s polarity=%s axes=%d", signalsys.LogInterpret, strings.TrimSpace(ni.FragmentID), polarity, len(axisBreakdown))
	return interpretation, nil
}

// buildInterpretationPromptFromData builds the prompt from raw data (for QA simulation path).
func (i *Interpreter) buildInterpretationPromptFromData(
	nodeLabel, canonicalPath, polarity string,
	metrics map[string]float64,
	axisBreakdown []AxisContribution,
	topEvents []EventProvenance,
) string {
	metricsText := ""
	for path, value := range metrics {
		metricsText += fmt.Sprintf("- %s: %.2f\n", path, value)
	}
	axisText := ""
	hasMechanism := false
	for _, contrib := range axisBreakdown {
		if strings.TrimSpace(contrib.Mechanism) != "" {
			hasMechanism = true
			axisText += fmt.Sprintf("- %s (mechanism=%s): %.2f\n", contrib.Axis, contrib.Mechanism, contrib.Amplitude)
		} else {
			axisText += fmt.Sprintf("- %s: %.2f\n", contrib.Axis, contrib.Amplitude)
		}
	}
	regulatoryNote := ""
	if hasMechanism {
		regulatoryNote = "\nSome axis entries include a mechanism slug from the metric path: relate findings to specific regulatory or policy instruments where evidence supports it.\n"
	}
	eventsText := ""
	for j, event := range topEvents {
		eventsText += fmt.Sprintf("%d. %s (Signal: %s)\n   - Contribution: %.2f\n", j+1, event.EventSummary, event.SignalID, event.Amplitude)
	}

	return fmt.Sprintf(`You are interpreting %s signals for an entity in a knowledge graph.

Entity: %s (%s)

Current %s Metrics:
%s

Axis Breakdown:
%s
%s
Top Contributing Events:
%s

Provide:
1. Summary (2-3 sentences explaining the %s situation)
2. Impact estimate (operational, financial, timeline)
3. Recommended mitigations (2-4 actionable items)

OUTPUT FORMAT (strict JSON):
{
  "summary": "2-3 sentence summary",
  "impact": {
    "operational": "operational impact description",
    "financial": "financial impact description",
    "timeline": "timeline estimate"
  },
  "mitigations": [
    {
      "action": "actionable item",
      "description": "optional description"
    }
  ]
}

Return ONLY valid JSON, no markdown code fences.`,
		polarity, nodeLabel, canonicalPath, polarity, metricsText, axisText, regulatoryNote, eventsText, polarity)
}

// buildInterpretationPrompt builds the LLM prompt for interpretation.
func (i *Interpreter) buildInterpretationPrompt(
	_ *horizonmodels.HorizonFragment,
	request *InterpretationRequest,
	metrics map[string]float64,
	axisBreakdown []AxisContribution,
	topEvents []EventProvenance,
) string {
	return i.buildInterpretationPromptFromData(
		request.NodeLabel,
		request.CanonicalPath,
		request.Polarity,
		metrics,
		axisBreakdown,
		topEvents,
	)
}

// Helper functions (local; keep names unique within package to avoid collisions).
func metricHasPolarity(metricPath, polarity string) bool {
	prefix := "/metric/" + polarity + "/"
	return len(metricPath) >= len(prefix) && metricPath[:len(prefix)] == prefix
}

func metricExtractAxisAndMechanism(metricPath string) (axis string, mechanism string) {
	parts := splitPathParts(metricPath)
	if len(parts) < 3 {
		return "", ""
	}
	if parts[0] != "metric" {
		return "", ""
	}
	axis = parts[2]
	if len(parts) >= 4 {
		mechanism = parts[3]
	}
	return axis, mechanism
}

func splitPathParts(path string) []string {
	if path == "" {
		return []string{}
	}
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	if path == "" {
		return []string{}
	}
	parts := make([]string, 0)
	current := ""
	for _, r := range path {
		if r == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
