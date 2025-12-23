package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CanonizationPolicy normalizes paths, types, names, and synonyms.
type CanonizationPolicy struct {
	rulesRepo CanonizationRulesRepo
}

// CanonizationRulesRepo provides access to canonization rules.
type CanonizationRulesRepo interface {
	GetRules(ctx context.Context) ([]*CanonizationRule, error)
}

// CanonizationRule defines a normalization rule.
type CanonizationRule struct {
	ID          string `json:"id" firestore:"id"`
	InputType   string `json:"inputType" firestore:"inputType"`   // "canonical_path", "entity_type", "name"
	InputPattern string `json:"inputPattern" firestore:"inputPattern"` // Pattern to match
	Output      string `json:"output" firestore:"output"`         // Normalized output
	Priority    int    `json:"priority" firestore:"priority"`
}

// NewCanonizationPolicy creates a new CanonizationPolicy.
func NewCanonizationPolicy(rulesRepo CanonizationRulesRepo) *CanonizationPolicy {
	return &CanonizationPolicy{
		rulesRepo: rulesRepo,
	}
}

// Name returns the policy name.
func (p *CanonizationPolicy) Name() string {
	return "canonization"
}

// Decide normalizes an input value.
func (p *CanonizationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	inputType, _ := input.Context["inputType"].(string)
	inputValue, _ := input.Context["inputValue"].(string)

	if inputType == "" || inputValue == "" {
		return PolicyDecision{
			Action:       "skip",
			DecisionPath: "rule:invalid_input",
			Confidence:   1.0,
		}, nil
	}

	// Try rules first
	rules, err := p.rulesRepo.GetRules(ctx)
	if err == nil {
		for _, rule := range rules {
			if rule.InputType == inputType && p.matchesPattern(inputValue, rule.InputPattern) {
				return PolicyDecision{
					Action:       "normalize_to",
					Params:       map[string]interface{}{"output": rule.Output},
					DecisionPath: "rule:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
	}

	// Fallback to taxonomy normalization
	normalized := p.normalizeFallback(inputType, inputValue)
	return PolicyDecision{
		Action:       "normalize_to",
		Params:       map[string]interface{}{"output": normalized},
		DecisionPath: "rule:taxonomy",
		Confidence:   0.9,
	}, nil
}

// Explain returns explanation for a decision.
func (p *CanonizationPolicy) Explain(decision PolicyDecision) string {
	output, _ := decision.Params["output"].(string)
	if decision.RuleID != nil {
		return fmt.Sprintf("Canonization rule %s: normalized to %s", *decision.RuleID, output)
	}
	return fmt.Sprintf("Canonization: normalized to %s (taxonomy)", output)
}

// normalizeFallback uses taxonomy functions for normalization.
func (p *CanonizationPolicy) normalizeFallback(inputType, inputValue string) string {
	switch inputType {
	case "canonical_path":
		// Use existing normalization if available
		return strings.ToLower(strings.TrimSpace(inputValue))
	case "entity_type":
		// Use taxonomy normalization
		return strings.ToLower(strings.TrimSpace(inputValue))
	case "relation_type":
		return taxonomy.NormalizeRelationType(inputValue)
	case "event_type":
		return taxonomy.NormalizeEventType(inputValue)
	default:
		return strings.ToLower(strings.TrimSpace(inputValue))
	}
}

// matchesPattern checks if value matches pattern.
func (p *CanonizationPolicy) matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}
	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(value, parts[0]) && strings.HasSuffix(value, parts[1])
		}
	}
	return false
}

