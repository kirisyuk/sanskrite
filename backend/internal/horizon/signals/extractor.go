package signals

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// SignalExtractor extracts SignalDescriptor from events using rule-based and LLM-based methods.
type SignalExtractor struct {
	rulesRepo  SignalExtractionRulesRepo
	aiExecutor *ai.AITaskExecutor
}

// SignalExtractionRulesRepo provides access to signal extraction rules.
type SignalExtractionRulesRepo interface {
	GetRules(ctx context.Context) ([]*SignalExtractionRule, error)
}

// SignalExtractionRule defines a rule-based event → signal transformation.
type SignalExtractionRule struct {
	ID                 string   `json:"id"`
	FactKind           string   `json:"factKind"`           // "event" | "claim" | "metric"
	FactTypePattern    string   `json:"factTypePattern"`    // "port_closure" | "sanction_*"
	SpherePattern      string   `json:"spherePattern"`      // "/sphere/logistics/*"
	Axis               string   `json:"axis"`               // Axis from 12-axis basis (e.g., "logistics/capacity")
	Mechanism          string   `json:"mechanism,omitempty"` // Optional mechanism (e.g., "airspace_closure")
	Polarity           string   `json:"polarity"`            // "risk" | "opportunity"
	Amplitude          float64  `json:"amplitude"`          // [0.0, 1.0] - default amplitude
	ConfidenceMultiplier float64 `json:"confidenceMultiplier"` // Multiplier for fact confidence
}

// NewSignalExtractor creates a new SignalExtractor.
func NewSignalExtractor(rulesRepo SignalExtractionRulesRepo, aiExecutor *ai.AITaskExecutor) *SignalExtractor {
	return &SignalExtractor{
		rulesRepo:  rulesRepo,
		aiExecutor: aiExecutor,
	}
}

// Extract extracts SignalDescriptor(s) from an event OntologyFact.
// Returns 1..N signals based on rule-based and LLM-based extraction.
func (e *SignalExtractor) Extract(ctx context.Context, fact *coremodels.OntologyFact) ([]*horizonmodels.SignalDescriptor, error) {
	if e == nil {
		return nil, nil
	}
	if fact == nil {
		return nil, nil
	}

	// Only process events
	if fact.Kind != "event" {
		return nil, nil
	}

	log.Printf("%s start eventId=%s type=%s title=%q", signalsys.LogExtract, strings.TrimSpace(fact.ID), strings.TrimSpace(fact.Type), strings.TrimSpace(fact.Title))

	var signals []*horizonmodels.SignalDescriptor

	// Step 1: Try rule-based extraction first
	if e.rulesRepo != nil {
		rules, err := e.rulesRepo.GetRules(ctx)
		if err == nil {
			for _, rule := range rules {
				if e.matches(fact, rule) {
					signal := e.createSignalFromRule(fact, rule)
					if signal != nil {
						if err := signal.Validate(); err == nil {
							signals = append(signals, signal)
						}
					}
				}
			}
		}
	}

	// Step 2: If no rule matched, try LLM-based extraction
	if len(signals) == 0 {
		llmSignals, err := e.extractWithLLM(ctx, fact)
		if err == nil && len(llmSignals) > 0 {
			signals = append(signals, llmSignals...)
		}
	}

	if len(signals) == 0 {
		log.Printf("%s no signals eventId=%s", signalsys.LogExtract, strings.TrimSpace(fact.ID))
		return nil, nil
	}
	axes := make([]string, 0, len(signals))
	for _, s := range signals {
		if s == nil {
			continue
		}
		axes = append(axes, strings.TrimSpace(s.Polarity)+":"+strings.TrimSpace(s.Axis))
	}
	log.Printf("%s done eventId=%s signals=%d [%s]", signalsys.LogExtract, strings.TrimSpace(fact.ID), len(signals), strings.Join(axes, ", "))

	return signals, nil
}

// matches checks if a fact matches a signal extraction rule.
func (e *SignalExtractor) matches(fact *coremodels.OntologyFact, rule *SignalExtractionRule) bool {
	// Check fact kind
	if rule.FactKind != "*" && rule.FactKind != fact.Kind {
		return false
	}

	// Check fact type pattern
	if !e.matchesPattern(fact.Type, rule.FactTypePattern) {
		return false
	}

	// Check sphere pattern
	if rule.SpherePattern != "*" {
		matched := false
		for _, spherePath := range fact.SpherePaths {
			if e.matchesPattern(spherePath, rule.SpherePattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// matchesPattern checks if value matches pattern (supports * wildcard).
func (e *SignalExtractor) matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}
	// Pattern matching: "port_closure" matches "port_closure", "sanction_*" matches "sanction_imposed"
	if strings.HasSuffix(pattern, "_*") {
		prefix := strings.TrimSuffix(pattern, "_*")
		return strings.HasPrefix(value, prefix+"_")
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	// Sphere path matching: "/sphere/logistics/*" matches "/sphere/logistics/procurement"
	if strings.Contains(pattern, "/") && strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(value, prefix+"/") || value == prefix
	}
	return false
}

// createSignalFromRule creates a SignalDescriptor from a fact and rule.
func (e *SignalExtractor) createSignalFromRule(fact *coremodels.OntologyFact, rule *SignalExtractionRule) *horizonmodels.SignalDescriptor {
	// Validate axis
	if !horizonmodels.IsValidAxis(rule.Axis) {
		return nil
	}

	// Validate polarity
	if rule.Polarity != "risk" && rule.Polarity != "opportunity" {
		return nil
	}

	// Compute amplitude (use rule amplitude, scaled by fact confidence)
	amplitude := rule.Amplitude * fact.Confidence * rule.ConfidenceMultiplier
	if amplitude > 1.0 {
		amplitude = 1.0
	}
	if amplitude < 0.0 {
		amplitude = 0.0
	}

	// Get location path
	locationPath := ""
	if fact.LocationPath != nil {
		locationPath = *fact.LocationPath
	}

	// Get observed time
	observedAt := time.Now()
	if fact.ObservedAt != nil {
		observedAt = *fact.ObservedAt
	} else if fact.OccurredAt != nil {
		observedAt = *fact.OccurredAt
	}

	// Build source refs
	sourceRefs := make([]string, 0)
	for _, ref := range fact.SourceRefs {
		if ref.SourceID != "" {
			sourceRefs = append(sourceRefs, ref.SourceID)
		}
	}
	sourceRefs = append(sourceRefs, fact.SourceIDs...)

	return &horizonmodels.SignalDescriptor{
		ID:           "", // Will be generated when saved
		FactID:       fact.ID,
		CreatedAt:    time.Now(),
		Axis:         rule.Axis,
		Mechanism:    rule.Mechanism,
		Amplitude:    amplitude,
		Polarity:     rule.Polarity,
		ActorPaths:   fact.ActorPaths,
		SeedPaths:    fact.CanonicalPaths, // Use canonical paths as seed paths
		LocationPath: locationPath,
		SpherePaths:  fact.SpherePaths,
		Constraints:  horizonmodels.SignalConstraints{}, // Can be extended later
		Confidence:   fact.Confidence * rule.ConfidenceMultiplier,
		SourceRefs:   sourceRefs,
		ObservedAt:   observedAt,
	}
}

// extractWithLLM extracts signals using LLM for complex events.
func (e *SignalExtractor) extractWithLLM(ctx context.Context, fact *coremodels.OntologyFact) ([]*horizonmodels.SignalDescriptor, error) {
	if e.aiExecutor == nil {
		return nil, fmt.Errorf("AI executor not configured")
	}

	// Build prompt
	prompt := e.buildLLMPrompt(fact)

	// Create AI task
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:        6,
			MaxOutputSize:             2048,
			MinContextWindow:          4000,
		},
		Verifier: ai.NewMapVerifier([]string{"signals"}, nil),
	}

	// Execute task
	parsed, err := e.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM extraction failed: %w", err)
	}

	// Parse result
	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	signalsRaw, ok := resultMap["signals"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("signals field not found or invalid")
	}

	var signals []*horizonmodels.SignalDescriptor
	for _, sRaw := range signalsRaw {
		sMap, ok := sRaw.(map[string]interface{})
		if !ok {
			continue
		}

		signal := e.parseLLMSignal(fact, sMap)
		if signal != nil {
			if err := signal.Validate(); err == nil {
				signals = append(signals, signal)
			}
		}
	}

	return signals, nil
}

// buildLLMPrompt builds the LLM prompt for signal extraction.
func (e *SignalExtractor) buildLLMPrompt(fact *coremodels.OntologyFact) string {
	locationInfo := ""
	if fact.LocationPath != nil {
		locationInfo = fmt.Sprintf("Location: %s\n", *fact.LocationPath)
	}

	actorInfo := ""
	if len(fact.ActorPaths) > 0 {
		actorInfo = fmt.Sprintf("Actors: %s\n", strings.Join(fact.ActorPaths, ", "))
	}

	sphereInfo := ""
	if len(fact.SpherePaths) > 0 {
		sphereInfo = fmt.Sprintf("Spheres: %s\n", strings.Join(fact.SpherePaths, ", "))
	}

	return fmt.Sprintf(`You are a signal extraction agent for a risk analysis system. Extract risk/opportunity signals from the following event.

EVENT:
Title: %s
Type: %s
Summary: %s
%s%s%s
Confidence: %.2f

TASK: Extract 1-3 signals that represent quasi-physical impulses from this event. Each signal must map to exactly one of the 12-axis basis.

12-AXIS BASIS:
1. legal/access - Legal permission to operate/transact (licenses, sanctions, bans)
2. legal/compliance - Secondary exposure risk from association with sanctioned/regulated entities
3. financial/settlement - Payment rails, banking access, clearing, FX (ability to move money)
4. financial/credit - Credit availability, counterparty risk, default exposure
5. physical_flow/supply - Physical availability of goods/materials (shortage or glut)
6. physical_flow/demand - Demand changes (surge or collapse)
7. logistics/capacity - Transportation/infrastructure capacity (routes, ports, airspace)
8. logistics/latency - Delays, lead time increases (not blockage, but slowdown)
9. operational/continuity - Ability to operate (strikes, failures, disasters)
10. info/uncertainty - Ambiguity of rules, timelines, exceptions (regulatory fog)
11. info/reputation - Public perception, ESG signals, brand impact
12. geo/conflict - Geopolitical instability, conflict zones, diplomatic friction

OUTPUT FORMAT (strict JSON):
{
  "signals": [
    {
      "axis": "one of the 12 axes above",
      "mechanism": "optional specific mechanism (e.g., 'sanctions', 'airspace_closure')",
      "amplitude": 0.0-1.0,
      "polarity": "risk" | "opportunity",
      "seedPaths": ["canonical paths of directly affected entities"],
      "locationPath": "canonical location path if applicable",
      "spherePaths": ["sphere paths if applicable"],
      "confidence": 0.0-1.0
    }
  ]
}

RULES:
- Select the PRIMARY axis (most direct physical effect)
- Amplitude should reflect the strength of the impulse (0.0 = weak, 1.0 = maximum)
- Polarity: "risk" for negative impacts, "opportunity" for positive impacts
- seedPaths should be canonical paths of entities directly affected (e.g., ["/org/ae/flydubai-llc"])
- If unsure, prefer conservative estimates (lower amplitude, higher confidence threshold)
- Return ONLY valid JSON, no markdown code fences`,
		fact.Title,
		fact.Type,
		fact.Summary,
		locationInfo,
		actorInfo,
		sphereInfo,
		fact.Confidence)
}

// parseLLMSignal parses a signal from LLM output.
func (e *SignalExtractor) parseLLMSignal(fact *coremodels.OntologyFact, sMap map[string]interface{}) *horizonmodels.SignalDescriptor {
	axis, _ := sMap["axis"].(string)
	if !horizonmodels.IsValidAxis(axis) {
		return nil
	}

	polarity, _ := sMap["polarity"].(string)
	if polarity != "risk" && polarity != "opportunity" {
		return nil
	}

	amplitude := 0.5 // default
	if amp, ok := sMap["amplitude"].(float64); ok {
		amplitude = amp
		if amplitude > 1.0 {
			amplitude = 1.0
		}
		if amplitude < 0.0 {
			amplitude = 0.0
		}
	}

	mechanism := ""
	if mech, ok := sMap["mechanism"].(string); ok {
		mechanism = mech
	}

	seedPaths := fact.CanonicalPaths // default to fact canonical paths
	if spRaw, ok := sMap["seedPaths"].([]interface{}); ok {
		seedPaths = make([]string, 0, len(spRaw))
		for _, sp := range spRaw {
			if spStr, ok := sp.(string); ok {
				seedPaths = append(seedPaths, spStr)
			}
		}
	}

	locationPath := ""
	if fact.LocationPath != nil {
		locationPath = *fact.LocationPath
	}
	if lp, ok := sMap["locationPath"].(string); ok && lp != "" {
		locationPath = lp
	}

	spherePaths := fact.SpherePaths
	if spRaw, ok := sMap["spherePaths"].([]interface{}); ok {
		spherePaths = make([]string, 0, len(spRaw))
		for _, sp := range spRaw {
			if spStr, ok := sp.(string); ok {
				spherePaths = append(spherePaths, spStr)
			}
		}
	}

	confidence := fact.Confidence
	if conf, ok := sMap["confidence"].(float64); ok {
		confidence = conf
		if confidence > 1.0 {
			confidence = 1.0
		}
		if confidence < 0.0 {
			confidence = 0.0
		}
	}

	// Build source refs
	sourceRefs := make([]string, 0)
	for _, ref := range fact.SourceRefs {
		if ref.SourceID != "" {
			sourceRefs = append(sourceRefs, ref.SourceID)
		}
	}
	sourceRefs = append(sourceRefs, fact.SourceIDs...)

	observedAt := time.Now()
	if fact.ObservedAt != nil {
		observedAt = *fact.ObservedAt
	} else if fact.OccurredAt != nil {
		observedAt = *fact.OccurredAt
	}

	return &horizonmodels.SignalDescriptor{
		ID:           "", // Will be generated when saved
		FactID:       fact.ID,
		CreatedAt:    time.Now(),
		Axis:         axis,
		Mechanism:    mechanism,
		Amplitude:    amplitude,
		Polarity:     polarity,
		ActorPaths:   fact.ActorPaths,
		SeedPaths:    seedPaths,
		LocationPath: locationPath,
		SpherePaths:  spherePaths,
		Constraints:  horizonmodels.SignalConstraints{},
		Confidence:   confidence,
		SourceRefs:   sourceRefs,
		ObservedAt:   observedAt,
	}
}
