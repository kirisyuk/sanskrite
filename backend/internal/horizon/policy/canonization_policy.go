package policy

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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
	ID           string `json:"id" firestore:"id"`
	InputType    string `json:"inputType" firestore:"inputType"`       // "canonical_path", "entity_type", "name"
	InputPattern string `json:"inputPattern" firestore:"inputPattern"` // Pattern to match
	Output       string `json:"output" firestore:"output"`             // Normalized output
	Priority     int    `json:"priority" firestore:"priority"`
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
	logPrefix := "[CanonizationPolicy]"

	inputType, _ := input.Context["inputType"].(string)
	inputValue, _ := input.Context["inputValue"].(string)

	log.Printf("%s Decide called: inputType=%s, inputValue=%s", logPrefix, inputType, inputValue)

	if inputType == "" || inputValue == "" {
		log.Printf("%s ERROR: missing inputType or inputValue (inputType=%s, inputValue=%s)",
			logPrefix, inputType, inputValue)
		return PolicyDecision{
			Action:       "skip",
			DecisionPath: "rule:invalid_input",
			Confidence:   1.0,
		}, nil
	}

	// Try DB rules first
	rules, err := p.rulesRepo.GetRules(ctx)
	if err != nil {
		log.Printf("%s ERROR: failed to get rules from repo: %v", logPrefix, err)
		// Continue with hardcoded normalization
	}

	if err == nil && len(rules) > 0 {
		for _, rule := range rules {
			if rule.InputType == inputType && p.matchesPattern(inputValue, rule.InputPattern) {
				out := strings.TrimSpace(rule.Output)
				if out == "" {
					log.Printf("%s WARNING: skipping DB rule with empty output: id=%s, inputType=%s, pattern=%s",
						logPrefix, rule.ID, rule.InputType, rule.InputPattern)
					continue
				}
				log.Printf("%s DB rule matched: id=%s, inputType=%s, pattern=%s, output=%s",
					logPrefix, rule.ID, rule.InputType, rule.InputPattern, out)
				return PolicyDecision{
					Action:       "normalize_to",
					Params:       map[string]interface{}{"output": out},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
		log.Printf("%s No DB rule matched for inputType=%s, inputValue=%s", logPrefix, inputType, inputValue)
	}

	// Fallback to hardcoded normalization
	normalized := strings.TrimSpace(p.normalizeFallback(inputType, inputValue))
	if normalized == "" {
		normalized = p.safeNormalizationFallback(inputType, inputValue)
		log.Printf("%s WARNING: empty normalization output prevented (inputType=%s, inputValue=%s, fallback=%s)",
			logPrefix, inputType, inputValue, normalized)
	}
	log.Printf("%s Normalized via hardcoded rules: %s -> %s (inputType=%s)",
		logPrefix, inputValue, normalized, inputType)
	return PolicyDecision{
		Action:       "normalize_to",
		Params:       map[string]interface{}{"output": normalized},
		DecisionPath: "rule:hardcoded",
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

// normalizeFallback uses hardcoded rules for normalization (from canonicalizer.go).
func (p *CanonizationPolicy) normalizeFallback(inputType, inputValue string) string {
	switch inputType {
	case "fragment_type", "entity_type":
		// Normalize fragment type (hardcoded from canonicalizer.go)
		return string(p.normalizeFragmentType(inputValue))
	case "canonical_path":
		// Basic path normalization
		normalized := strings.ToLower(strings.TrimSpace(inputValue))
		// Handle legacy geo paths
		if strings.HasPrefix(normalized, "/region/") {
			normalized = p.normalizeLegacyGeoPath(normalized)
		}
		// Best-effort: MARKET canonical path tightening (non-blocking).
		// If we see a flat /market/{slug}, upgrade it to /market/global/{slug}.
		// Legacy /market/{domain}/{slug} is left untouched (cannot infer geo safely).
		if strings.HasPrefix(normalized, "/market/") {
			parts := strings.Split(strings.Trim(normalized, "/"), "/")
			// parts[0] == "market"
			if len(parts) == 2 && parts[1] != "" {
				normalized = "/market/global/" + parts[1]
			}
		}
		return normalized
	case "relation_type":
		return taxonomy.NormalizeRelationType(inputValue)
	case "event_type":
		return taxonomy.NormalizeEventType(inputValue)
	case "slug":
		return p.slugify(inputValue)
	default:
		return strings.ToLower(strings.TrimSpace(inputValue))
	}
}

// normalizeFragmentType maps common human labels to FragmentType enum (hardcoded from canonicalizer.go).
func (p *CanonizationPolicy) normalizeFragmentType(s string) horizonmodels.FragmentType {
	// Keep fragment/entity type normalization aligned with unified edge semantics contract.
	return edgesemantics.NormalizeFragmentTypeToken(s)
}

func (p *CanonizationPolicy) safeNormalizationFallback(inputType, inputValue string) string {
	switch inputType {
	case "fragment_type", "entity_type":
		return string(p.normalizeFragmentType(inputValue))
	case "canonical_path":
		return strings.ToLower(strings.TrimSpace(inputValue))
	case "relation_type", "event_type":
		return strings.ToUpper(strings.TrimSpace(inputValue))
	case "slug":
		return p.slugify(inputValue)
	default:
		return strings.TrimSpace(inputValue)
	}
}

// normalizeLegacyGeoPath maps older geo namespaces to /loc/{country}/{admin1}/{city} (hardcoded from canonicalizer.go).
func (p *CanonizationPolicy) normalizeLegacyGeoPath(path string) string {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "/loc/") {
		return normalized
	}
	if !strings.HasPrefix(normalized, "/region/") {
		return ""
	}
	parts := strings.Split(strings.Trim(normalized, "/"), "/")
	// parts[0] == "region"
	if len(parts) == 5 && parts[1] == "na" {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[2], parts[3], parts[4])
	}
	if len(parts) == 4 {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[1], parts[2], parts[3])
	}
	return ""
}

// slugify converts a string to a URL-safe slug (hardcoded from canonicalizer.go).
func (p *CanonizationPolicy) slugify(s string) string {
	// Keep slugging consistent across Horizon (scout/policies/tools) and Ontology.
	return taxonomy.Slugify(s)
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
