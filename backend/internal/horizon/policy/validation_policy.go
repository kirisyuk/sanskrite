package policy

import (
	"context"
	"fmt"
)

// ValidationPolicy validates combinations of source×target×relationType.
type ValidationPolicy struct {
	rulesRepo ValidationRulesRepo
}

// ValidationRulesRepo provides access to validation rules.
type ValidationRulesRepo interface {
	GetRules(ctx context.Context) ([]*ValidationRule, error)
}

// ValidationRule defines validation behavior for a combination.
type ValidationRule struct {
	ID              string  `json:"id" firestore:"id"`
	SourceType      string  `json:"sourceType" firestore:"sourceType"`       // "ORGANIZATION", "FACILITY", "*"
	TargetType      string  `json:"targetType" firestore:"targetType"`       // "ORGANIZATION", "FACILITY", "*"
	RelationType    string  `json:"relationType" firestore:"relationType"`   // "SUPPLIES_TO", "*"
	Action          string  `json:"action" firestore:"action"`               // "allow", "reject", "penalty"
	ConfidencePenalty float64 `json:"confidencePenalty,omitempty" firestore:"confidencePenalty,omitempty"` // For "penalty" action
	Reason           string  `json:"reason" firestore:"reason"`              // Human-readable reason
	Priority         int     `json:"priority" firestore:"priority"`
}

// NewValidationPolicy creates a new ValidationPolicy.
func NewValidationPolicy(rulesRepo ValidationRulesRepo) *ValidationPolicy {
	return &ValidationPolicy{
		rulesRepo: rulesRepo,
	}
}

// Name returns the policy name.
func (p *ValidationPolicy) Name() string {
	return "validation"
}

// Decide validates a source×target×relationType combination.
func (p *ValidationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	var sourceType, targetType, relationType string

	if input.Edge != nil && input.Fragment != nil {
		// Extract from edge and fragments
		sourceType = string(input.Fragment.Type)
		relationType = string(input.Edge.Type)
		// Target type would need target fragment lookup, simplified here
		targetType, _ = input.Context["targetType"].(string)
	} else {
		sourceType, _ = input.Context["sourceType"].(string)
		targetType, _ = input.Context["targetType"].(string)
		relationType, _ = input.Context["relationType"].(string)
	}

	if sourceType == "" || targetType == "" || relationType == "" {
		return PolicyDecision{
			Action:       "allow",
			DecisionPath: "rule:default",
			Confidence:   0.5,
		}, nil
	}

	// Get rules
	rules, err := p.rulesRepo.GetRules(ctx)
	if err == nil {
		rule := p.matchRule(rules, sourceType, targetType, relationType)
		if rule != nil {
			switch rule.Action {
			case "reject":
				return PolicyDecision{
					Action:       "reject",
					Params:       map[string]interface{}{"reason": rule.Reason},
					DecisionPath: "rule:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			case "penalty":
				return PolicyDecision{
					Action:       "penalty",
					Params:       map[string]interface{}{"penalty": rule.ConfidencePenalty, "reason": rule.Reason},
					DecisionPath: "rule:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0 - rule.ConfidencePenalty,
				}, nil
			case "allow":
				return PolicyDecision{
					Action:       "allow",
					DecisionPath: "rule:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
	}

	// Default: allow with medium confidence
	return PolicyDecision{
		Action:       "allow",
		DecisionPath: "rule:default",
		Confidence:   0.7,
	}, nil
}

// Explain returns explanation for a decision.
func (p *ValidationPolicy) Explain(decision PolicyDecision) string {
	if decision.RuleID != nil {
		reason, _ := decision.Params["reason"].(string)
		return fmt.Sprintf("Validation rule %s: %s - %s", *decision.RuleID, decision.Action, reason)
	}
	return fmt.Sprintf("Validation: %s (default)", decision.Action)
}

// matchRule finds the best matching rule.
func (p *ValidationPolicy) matchRule(rules []*ValidationRule, sourceType, targetType, relationType string) *ValidationRule {
	var bestMatch *ValidationRule
	bestPriority := -1

	for _, rule := range rules {
		sourceMatch := rule.SourceType == "*" || rule.SourceType == sourceType
		targetMatch := rule.TargetType == "*" || rule.TargetType == targetType
		relationMatch := rule.RelationType == "*" || rule.RelationType == relationType

		if sourceMatch && targetMatch && relationMatch {
			if rule.Priority > bestPriority {
				bestMatch = rule
				bestPriority = rule.Priority
			}
		}
	}

	return bestMatch
}

