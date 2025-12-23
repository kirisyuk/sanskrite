package signals

import (
	"context"
	"fmt"
	"strings"

	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Synthesizer synthesizes signals from facts using deterministic rules.
type Synthesizer struct {
	rulesRepo SynthesisRulesRepo
}

// SynthesisRulesRepo provides access to synthesis rules.
type SynthesisRulesRepo interface {
	GetRules(ctx context.Context) ([]*repo.SynthesisRule, error)
}

// NewSynthesizer creates a new SignalSynthesizer.
func NewSynthesizer(rulesRepo SynthesisRulesRepo) *Synthesizer {
	return &Synthesizer{
		rulesRepo: rulesRepo,
	}
}

// Synthesize synthesizes signals from a fact using deterministic rules.
func (s *Synthesizer) Synthesize(ctx context.Context, fact *coremodels.OntologyFact) ([]*coremodels.OntologyFact, error) {
	if fact == nil {
		return nil, nil
	}

	rules, err := s.rulesRepo.GetRules(ctx)
	if err != nil {
		return nil, err
	}

	var signals []*coremodels.OntologyFact
	matchedRules := make(map[string]bool) // Track matched rules to avoid duplicates

	for _, rule := range rules {
		if s.matches(fact, rule) && !matchedRules[rule.ID] {
			signal := s.createSignal(fact, rule)
			if signal != nil {
				signals = append(signals, signal)
				matchedRules[rule.ID] = true
			}
		}
	}

	return signals, nil
}

// matches checks if a fact matches a synthesis rule.
func (s *Synthesizer) matches(fact *coremodels.OntologyFact, rule *repo.SynthesisRule) bool {
	// Check fact kind
	if rule.FactKind != "*" && rule.FactKind != fact.Kind {
		return false
	}

	// Check fact type pattern
	if !s.matchesPattern(fact.Type, rule.FactTypePattern) {
		return false
	}

	// Check sphere pattern
	if rule.SpherePattern != "*" {
		matched := false
		for _, spherePath := range fact.SpherePaths {
			if s.matchesPattern(spherePath, rule.SpherePattern) {
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

// createSignal creates a signal OntologyFact from a fact and rule.
func (s *Synthesizer) createSignal(fact *coremodels.OntologyFact, rule *repo.SynthesisRule) *coremodels.OntologyFact {
	// Normalize signal type
	signalType := taxonomy.NormalizeSignalType(rule.OutputSignalType)
	if signalType == "other" {
		return nil // Skip invalid signal types
	}

	// Compute metric value
	metricValue := fact.Confidence
	if rule.MetricValue != nil {
		metricValue = *rule.MetricValue
	}

	// Create signal fact
	signal := &coremodels.OntologyFact{
		BaseOntologyFields: fact.BaseOntologyFields, // This includes CanonicalPaths, SpherePaths, LocationPath
		Kind:               "signal",
		Type:               signalType,
		Title:              fmt.Sprintf("%s signal: %s", rule.OutputSignalType, fact.Title),
		Summary:            fact.Summary,
		Confidence:         fact.Confidence * rule.ConfidenceMultiplier,
		OccurredAt:         fact.OccurredAt,
		ObservedAt:         fact.ObservedAt,
		SourceRefs:         fact.SourceRefs,
		SourceIDs:          fact.SourceIDs,
		MetricUpdates: []coremodels.FactMetricUpdate{
			{
				MetricName: rule.MetricName,
				Value:      metricValue,
				Confidence: fact.Confidence * rule.ConfidenceMultiplier,
				ObservedAt: fact.ObservedAt,
			},
		},
	}

	// Generate new ID for signal
	signal.ID = ""

	return signal
}

// matchesPattern checks if value matches pattern (supports * wildcard).
func (s *Synthesizer) matchesPattern(value, pattern string) bool {
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

