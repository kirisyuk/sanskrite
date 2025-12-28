package policy

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
var multiDash = regexp.MustCompile(`-+`)

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
				log.Printf("%s DB rule matched: id=%s, inputType=%s, pattern=%s, output=%s", 
					logPrefix, rule.ID, rule.InputType, rule.InputPattern, rule.Output)
				return PolicyDecision{
					Action:       "normalize_to",
					Params:       map[string]interface{}{"output": rule.Output},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
		log.Printf("%s No DB rule matched for inputType=%s, inputValue=%s", logPrefix, inputType, inputValue)
	}

	// Fallback to hardcoded normalization
	normalized := p.normalizeFallback(inputType, inputValue)
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
	normalized := strings.TrimSpace(strings.ToUpper(s))
	switch normalized {
	case string(horizonmodels.FragmentTypeOrganization), "ORG":
		return horizonmodels.FragmentTypeOrganization
	case string(horizonmodels.FragmentTypeFacility), "FAC":
		return horizonmodels.FragmentTypeFacility
	case string(horizonmodels.FragmentTypeGeoRegion), "REGION", "GEO", "GEOREGION":
		return horizonmodels.FragmentTypeGeoRegion
	case string(horizonmodels.FragmentTypeProduct):
		return horizonmodels.FragmentTypeProduct
	case string(horizonmodels.FragmentTypeProgram):
		return horizonmodels.FragmentTypeProgram
	case string(horizonmodels.FragmentTypeMarket):
		return horizonmodels.FragmentTypeMarket
	case string(horizonmodels.FragmentTypeEvent):
		return horizonmodels.FragmentTypeEvent
	case string(horizonmodels.FragmentTypePerson), "HUMAN", "INDIVIDUAL":
		return horizonmodels.FragmentTypePerson
	default:
		return ""
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
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
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

