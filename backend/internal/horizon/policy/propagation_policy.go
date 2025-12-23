package policy

import (
	"context"
	"fmt"
	"strings"
)

// PropagationPolicy decides how metrics propagate through edges.
type PropagationPolicy struct {
	rulesRepo PropagationPolicyRulesRepo
}

// PropagationPolicyRulesRepo provides access to propagation policy rules.
type PropagationPolicyRulesRepo interface {
	GetRules(ctx context.Context) ([]*PropagationPolicyRule, error)
}

// PropagationPolicyRule defines behavior for a metric × relationType combination.
type PropagationPolicyRule struct {
	ID               string   `json:"id" firestore:"id"`
	MetricPattern    string   `json:"metricPattern" firestore:"metricPattern"`     // "risk_*" or "risk_supply_disruption_score"
	RelationType     string   `json:"relationType" firestore:"relationType"`       // "SUPPLIES_TO" or "*"
	Passes           bool     `json:"passes" firestore:"passes"`                  // Whether propagation passes through
	Weight           float64  `json:"weight" firestore:"weight"`                  // Multiplier (0..2, default 1.0)
	MaxDepth         int      `json:"maxDepth" firestore:"maxDepth"`              // Max depth for this combination
	DampingOverride  *float64 `json:"dampingOverride,omitempty" firestore:"dampingOverride,omitempty"` // Override base damping
	Direction        string   `json:"direction" firestore:"direction"`            // "upstream" | "downstream" | "both"
	CriticalPathBoost float64 `json:"criticalPathBoost" firestore:"criticalPathBoost"` // Boost for critical paths (1.0 = no boost)
	Priority         int      `json:"priority" firestore:"priority"`              // For conflict resolution (higher = more priority)
}

// NewPropagationPolicy creates a new PropagationPolicy.
func NewPropagationPolicy(rulesRepo PropagationPolicyRulesRepo) *PropagationPolicy {
	return &PropagationPolicy{
		rulesRepo: rulesRepo,
	}
}

// Name returns the policy name.
func (p *PropagationPolicy) Name() string {
	return "propagation"
}

// Decide decides how a metric should propagate through an edge.
func (p *PropagationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	// Extract context
	metricName, _ := input.Context["metricName"].(string)
	relationType, _ := input.Context["relationType"].(string)
	currentDepth, _ := input.Context["currentDepth"].(int)
	isCriticalPath, _ := input.Context["isCriticalPath"].(bool)

	if metricName == "" || relationType == "" {
		return PolicyDecision{
			Action:       "skip",
			DecisionPath: "rule:invalid_input",
			Confidence:   1.0,
		}, nil
	}

	// Get rules
	rules, err := p.rulesRepo.GetRules(ctx)
	if err != nil {
		return PolicyDecision{
			Action:       "propagate",
			Params:       map[string]interface{}{"weight": 1.0, "maxDepth": 3},
			DecisionPath: "rule:default",
			Confidence:   0.5,
		}, nil
	}

	// Match rule
	rule := p.matchRule(rules, metricName, relationType)
	if rule == nil {
		return PolicyDecision{
			Action:       "propagate",
			Params:       map[string]interface{}{"weight": 1.0, "maxDepth": 3},
			DecisionPath: "rule:default",
			Confidence:   0.5,
		}, nil
	}

	// Check if propagation passes
	if !rule.Passes {
		return PolicyDecision{
			Action:       "skip",
			DecisionPath: "rule:exact_match",
			RuleID:       &rule.ID,
			Confidence:   1.0,
		}, nil
	}

	// Check depth limit
	if currentDepth >= rule.MaxDepth {
		return PolicyDecision{
			Action:       "skip",
			DecisionPath: "rule:exact_match",
			RuleID:       &rule.ID,
			Confidence:   1.0,
		}, nil
	}

	// Apply critical path boost
	effectiveWeight := rule.Weight
	if isCriticalPath {
		effectiveWeight *= rule.CriticalPathBoost
	}

	// Build decision
	decision := PolicyDecision{
		Action:       "propagate",
		Params: map[string]interface{}{
			"weight":          effectiveWeight,
			"maxDepth":        rule.MaxDepth,
			"dampingOverride": rule.DampingOverride,
			"direction":       rule.Direction,
		},
		DecisionPath: "rule:exact_match",
		RuleID:       &rule.ID,
		Confidence:   1.0,
	}

	return decision, nil
}

// Explain returns explanation for a decision.
func (p *PropagationPolicy) Explain(decision PolicyDecision) string {
	if decision.RuleID != nil {
		return fmt.Sprintf("Propagation policy rule %s: %s", *decision.RuleID, decision.Action)
	}
	return fmt.Sprintf("Propagation policy: %s (default)", decision.Action)
}

// matchRule finds the best matching rule for metricName × relationType.
func (p *PropagationPolicy) matchRule(rules []*PropagationPolicyRule, metricName, relationType string) *PropagationPolicyRule {
	var bestMatch *PropagationPolicyRule
	bestPriority := -1

	for _, rule := range rules {
		// Check if rule matches
		metricMatch := p.matchesPattern(metricName, rule.MetricPattern)
		relationMatch := rule.RelationType == "*" || rule.RelationType == relationType

		if metricMatch && relationMatch {
			if rule.Priority > bestPriority {
				bestMatch = rule
				bestPriority = rule.Priority
			}
		}
	}

	return bestMatch
}

// matchesPattern checks if value matches pattern (supports * wildcard).
func (p *PropagationPolicy) matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}
	// Pattern matching: "risk_*" matches "risk_supply_disruption_score"
	if strings.HasSuffix(pattern, "_*") {
		prefix := strings.TrimSuffix(pattern, "_*")
		return strings.HasPrefix(value, prefix+"_")
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return false
}

