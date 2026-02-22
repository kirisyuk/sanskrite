package policy

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

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
	ID                string  `json:"id" firestore:"id"`
	SourceType        string  `json:"sourceType" firestore:"sourceType"`                                   // "ORGANIZATION", "FACILITY", "*"
	TargetType        string  `json:"targetType" firestore:"targetType"`                                   // "ORGANIZATION", "FACILITY", "*"
	RelationType      string  `json:"relationType" firestore:"relationType"`                               // "SUPPLIES_TO", "*"
	Action            string  `json:"action" firestore:"action"`                                           // "allow", "reject", "penalty"
	ConfidencePenalty float64 `json:"confidencePenalty,omitempty" firestore:"confidencePenalty,omitempty"` // For "penalty" action
	Reason            string  `json:"reason" firestore:"reason"`                                           // Human-readable reason
	Priority          int     `json:"priority" firestore:"priority"`
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

// Decide validates a source×target×relationType combination or fragment.
func (p *ValidationPolicy) Decide(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	logPrefix := "[ValidationPolicy]"

	// Determine validation kind
	kind, _ := input.Context["validationKind"].(string)
	if kind == "" {
		kind = input.Kind
	}
	if kind == "" {
		// Infer from input
		if input.Fragment != nil {
			kind = "fragment"
		} else if input.Edge != nil {
			kind = "edge"
		} else {
			kind = "edge" // default
		}
	}

	log.Printf("%s Decide called: kind=%s", logPrefix, kind)

	// Fragment validation
	if kind == "fragment" && input.Fragment != nil {
		decision, err := p.validateFragment(ctx, input)
		return NormalizeValidationDecision(decision), err
	}

	// Edge validation
	if kind == "edge" {
		decision, err := p.validateEdge(ctx, input)
		return NormalizeValidationDecision(decision), err
	}

	// Fallback
	log.Printf("%s Unknown validation kind: %s, defaulting to allow", logPrefix, kind)
	return PolicyDecision{
		Action:       ActionAllow,
		DecisionPath: "rule:unknown_kind",
		Confidence:   0.5,
	}, nil
}

// validateFragment validates fragment creation rules (hardcoded from precreate_validator.go).
func (p *ValidationPolicy) validateFragment(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	logPrefix := "[ValidationPolicy:Fragment]"
	fragment := input.Fragment

	if fragment == nil {
		log.Printf("%s ERROR: fragment is nil", logPrefix)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": "fragment is nil"},
			DecisionPath: "rule:hardcoded",
			Confidence:   1.0,
		}, nil
	}

	log.Printf("%s Validating fragment: id=%s, type=%s, canonicalPath=%s",
		logPrefix, fragment.ID, fragment.Type, fragment.CanonicalPath)

	// Policy 1: Block /discovered/ paths
	if strings.HasPrefix(fragment.CanonicalPath, "/discovered/") {
		reason := "canonical path uses /discovered/ prefix - must be canonicalized first"
		log.Printf("%s REJECT: %s (canonicalPath=%s)", logPrefix, reason, fragment.CanonicalPath)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
			DecisionPath: "rule:hardcoded:discovered_prefix",
			Confidence:   1.0,
		}, nil
	}

	// Policy 2: Required fields
	if strings.TrimSpace(fragment.CanonicalPath) == "" {
		reason := "canonical path is required"
		log.Printf("%s REJECT: %s", logPrefix, reason)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
			DecisionPath: "rule:hardcoded:empty_path",
			Confidence:   1.0,
		}, nil
	}

	if fragment.Type == "" {
		reason := "fragment type is required"
		log.Printf("%s REJECT: %s", logPrefix, reason)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
			DecisionPath: "rule:hardcoded:empty_type",
			Confidence:   1.0,
		}, nil
	}

	// Policy 2.5: Deterministic type/path reconciliation.
	if reconciledType, changed := edgesemantics.ReconcileTypeWithCanonicalPath(fragment.Type, fragment.CanonicalPath); changed {
		originalType := fragment.Type
		fragment.Type = reconciledType
		reason := fmt.Sprintf("type_path_mismatch: canonicalPath=%q with type=%s reconciled to %s", fragment.CanonicalPath, originalType, reconciledType)
		log.Printf("%s INFO: %s", logPrefix, reason)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusPenalized, reason, 0.05)
		return PolicyDecision{
			Action: ActionAllowWithPenalty,
			Params: map[string]interface{}{
				"penalty": 0.05,
				"reason":  reason,
			},
			DecisionPath: "rule:hardcoded:type_path_reconciled",
			Confidence:   0.95,
		}, nil
	}

	// Policy 3: Enforce canonical path prefixes by type
	expectedPrefix := ExpectedCanonicalPathPrefix(fragment.Type)
	if !IsCanonicalPathAllowedForType(fragment.Type, fragment.CanonicalPath) {
		warning := fmt.Sprintf("canonical path %q does not start with expected prefix %q for type %s",
			fragment.CanonicalPath, expectedPrefix, fragment.Type)
		log.Printf("%s WARNING: %s", logPrefix, warning)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusPenalized, warning, 0.1)
		// Continue with penalty instead of reject
		return PolicyDecision{
			Action: ActionAllowWithPenalty,
			Params: map[string]interface{}{
				"penalty": 0.1,
				"reason":  warning,
			},
			DecisionPath: "rule:hardcoded:prefix_mismatch",
			Confidence:   0.9,
		}, nil
	}

	// Policy 4: GEO_REGION strict validation
	if fragment.Type == horizonmodels.FragmentTypeGeoRegion {
		// Legacy /region/ normalization check
		if strings.HasPrefix(fragment.CanonicalPath, "/region/") {
			log.Printf("%s WARNING: legacy /region/ path detected: %s", logPrefix, fragment.CanonicalPath)
			// This should be normalized before validation, but we log it
		}

		if !strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			reason := fmt.Sprintf("GEO_REGION canonicalPath %q must use /loc/ prefix", fragment.CanonicalPath)
			log.Printf("%s REJECT: %s", logPrefix, reason)
			fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
			return PolicyDecision{
				Action:       ActionQuarantineSoft,
				Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
				DecisionPath: "rule:hardcoded:geo_prefix",
				Confidence:   1.0,
			}, nil
		}

		// Enforce strict 3-position geo model: /loc/{country}/{admin1}/{city}
		parts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(parts) != 4 || parts[0] != "loc" {
			reason := fmt.Sprintf("GEO_REGION canonicalPath %q must be /loc/{country}/{admin1}/{city}", fragment.CanonicalPath)
			log.Printf("%s REJECT: %s (parts=%v)", logPrefix, reason, parts)
			fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
			return PolicyDecision{
				Action:       ActionQuarantineSoft,
				Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
				DecisionPath: "rule:hardcoded:geo_format",
				Confidence:   1.0,
			}, nil
		}

		if !reCountryAlpha2.MatchString(parts[1]) {
			reason := fmt.Sprintf("GEO_REGION canonicalPath %q has invalid country code %q (expected ISO3166-1 alpha2 lowercase)",
				fragment.CanonicalPath, parts[1])
			log.Printf("%s REJECT: %s", logPrefix, reason)
			fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
			return PolicyDecision{
				Action:       ActionQuarantineSoft,
				Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
				DecisionPath: "rule:hardcoded:geo_country_code",
				Confidence:   1.0,
			}, nil
		}

		log.Printf("%s GEO_REGION validation passed: %s", logPrefix, fragment.CanonicalPath)
	}

	// Policy 5: Warn about flat facility paths (penalty, not reject)
	if fragment.Type == horizonmodels.FragmentTypeFacility {
		pathParts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(pathParts) == 2 && pathParts[0] == "fac" {
			warning := fmt.Sprintf("flat facility path %q - consider nesting under parent org: /fac/{org}/%s",
				fragment.CanonicalPath, pathParts[1])
			log.Printf("%s WARNING: %s", logPrefix, warning)
			fragment.SetPolicyStatus(horizonmodels.PolicyStatusPenalized, warning, 0.05)
			return PolicyDecision{
				Action: ActionAllowWithPenalty,
				Params: map[string]interface{}{
					"penalty": 0.05,
					"reason":  warning,
				},
				DecisionPath: "rule:hardcoded:flat_facility",
				Confidence:   0.95,
			}, nil
		}
	}

	// Check DB rules for additional validation
	if p.rulesRepo == nil {
		log.Printf("%s ALLOW: fragment validation passed (canonicalPath=%s, type=%s)",
			logPrefix, fragment.CanonicalPath, fragment.Type)
		return PolicyDecision{
			Action:       ActionAllow,
			DecisionPath: "rule:hardcoded:passed",
			Confidence:   1.0,
		}, nil
	}

	rules, err := p.rulesRepo.GetRules(ctx)
	if err != nil {
		log.Printf("%s ERROR: failed to get rules from repo: %v", logPrefix, err)
		// Continue with hardcoded validation result
	}

	if err == nil && len(rules) > 0 {
		rule := p.matchRule(rules, string(fragment.Type), "*", "*")
		if rule != nil {
			log.Printf("%s DB rule matched: id=%s, action=%s, priority=%d",
				logPrefix, rule.ID, rule.Action, rule.Priority)
			switch rule.Action {
			case "reject":
				log.Printf("%s REJECT by DB rule: %s", logPrefix, rule.Reason)
				return PolicyDecision{
					Action:       ActionQuarantineSoft,
					Params:       map[string]interface{}{"reason": rule.Reason, "penalty": 0.2},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			case "penalty":
				log.Printf("%s PENALTY by DB rule: %s (penalty=%.2f)", logPrefix, rule.Reason, rule.ConfidencePenalty)
				return PolicyDecision{
					Action:       ActionAllowWithPenalty,
					Params:       map[string]interface{}{"penalty": rule.ConfidencePenalty, "reason": rule.Reason},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0 - rule.ConfidencePenalty,
				}, nil
			case "allow":
				log.Printf("%s ALLOW by DB rule: %s", logPrefix, rule.Reason)
				return PolicyDecision{
					Action:       ActionAllow,
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
	}

	log.Printf("%s ALLOW: fragment validation passed (canonicalPath=%s, type=%s)",
		logPrefix, fragment.CanonicalPath, fragment.Type)
	return PolicyDecision{
		Action:       ActionAllow,
		DecisionPath: "rule:hardcoded:passed",
		Confidence:   1.0,
	}, nil
}

// validateEdge validates edge creation rules (hardcoded from edge_normalizer.go).
func (p *ValidationPolicy) validateEdge(ctx context.Context, input PolicyInput) (PolicyDecision, error) {
	logPrefix := "[ValidationPolicy:Edge]"

	var sourceType, targetType, relationType string
	var sourceID, targetID string
	var sourcePath, targetPath string

	if input.Edge != nil {
		sourceID = input.Edge.SourceFragmentID
		targetID = input.Edge.TargetFragmentID
		relationType = string(input.Edge.Type)
		if input.Edge.Qualifiers != nil {
			if v, ok := input.Edge.Qualifiers["sourceCanonicalPath"].(string); ok {
				sourcePath = strings.TrimSpace(v)
			}
			if v, ok := input.Edge.Qualifiers["targetCanonicalPath"].(string); ok {
				targetPath = strings.TrimSpace(v)
			}
		}
		log.Printf("%s Validating edge: sourceID=%s, targetID=%s, type=%s",
			logPrefix, sourceID, targetID, relationType)
	} else {
		sourceID, _ = input.Context["sourceFragmentID"].(string)
		targetID, _ = input.Context["targetFragmentID"].(string)
		sourceType, _ = input.Context["sourceType"].(string)
		targetType, _ = input.Context["targetType"].(string)
		relationType, _ = input.Context["relationType"].(string)
		sourcePath, _ = input.Context["sourceCanonicalPath"].(string)
		targetPath, _ = input.Context["targetCanonicalPath"].(string)
		log.Printf("%s Validating edge from context: sourceType=%s, targetType=%s, type=%s",
			logPrefix, sourceType, targetType, relationType)
	}

	// Policy 1: Self-loop prevention
	if sourceID != "" && targetID != "" && strings.TrimSpace(sourceID) == strings.TrimSpace(targetID) {
		reason := fmt.Sprintf("self-loop detected: sourceID=%s == targetID=%s", sourceID, targetID)
		log.Printf("%s REJECT: %s", logPrefix, reason)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
			DecisionPath: "rule:hardcoded:self_loop",
			Confidence:   1.0,
		}, nil
	}

	// Extract types if available
	if input.Fragment != nil {
		sourceType = string(input.Fragment.Type)
		log.Printf("%s Source type from fragment: %s", logPrefix, sourceType)
	}
	if targetType == "" {
		targetType, _ = input.Context["targetType"].(string)
	}
	if sourcePath == "" {
		sourcePath, _ = input.Context["sourceCanonicalPath"].(string)
	}
	if targetPath == "" {
		targetPath, _ = input.Context["targetCanonicalPath"].(string)
	}

	// Policy 1.5: deterministic edge endpoint type/path reconciliation.
	if sourceType != "" && sourcePath != "" {
		if reconciled, changed := edgesemantics.ReconcileTypeWithCanonicalPath(horizonmodels.FragmentType(sourceType), sourcePath); changed {
			log.Printf("%s INFO: type_path_mismatch source reconciled: path=%s type=%s -> %s",
				logPrefix, sourcePath, sourceType, reconciled)
			sourceType = string(reconciled)
		}
	}
	if targetType != "" && targetPath != "" {
		if reconciled, changed := edgesemantics.ReconcileTypeWithCanonicalPath(horizonmodels.FragmentType(targetType), targetPath); changed {
			log.Printf("%s INFO: type_path_mismatch target reconciled: path=%s type=%s -> %s",
				logPrefix, targetPath, targetType, reconciled)
			targetType = string(reconciled)
		}
	}

	// Policy 2: Edge direction validation (hardcoded from EdgeDirectionPolicies)
	if sourceType != "" && targetType != "" && relationType != "" {
		edgeType := horizonmodels.DependencyType(relationType)
		isValid, shouldReverse, rule := p.validateEdgeDirection(edgeType,
			horizonmodels.FragmentType(sourceType), horizonmodels.FragmentType(targetType))

		if !isValid {
			if shouldReverse {
				reason := fmt.Sprintf("edge direction invalid but reversible: %s -> %s (%s). Rule: %s",
					sourceType, targetType, relationType, rule.Description)
				log.Printf("%s REVERSE: %s", logPrefix, reason)
				return PolicyDecision{
					Action:       ActionReverse,
					Params:       map[string]interface{}{"reason": reason, "shouldReverse": true},
					DecisionPath: "rule:hardcoded:direction_reverse",
					Confidence:   1.0,
				}, nil
			} else {
				baseReason := fmt.Sprintf("edge direction invalid and not reversible: %s -> %s (%s). Rule: %s",
					sourceType, targetType, relationType, rule.Description)
				penalty, reason := quarantinePenaltyForTriple(
					edgeType,
					horizonmodels.FragmentType(sourceType),
					horizonmodels.FragmentType(targetType),
				)
				if reason == "" {
					reason = baseReason
				} else {
					reason = reason + "; " + baseReason
				}
				log.Printf("%s QUARANTINE: %s (penalty=%.2f)", logPrefix, reason, penalty)
				return PolicyDecision{
					Action: ActionQuarantineSoft,
					Params: map[string]interface{}{
						"reason":  reason,
						"penalty": penalty,
					},
					DecisionPath: "rule:hardcoded:direction_invalid_quarantine",
					Confidence:   1.0 - penalty,
				}, nil
			}
		} else if rule != nil {
			log.Printf("%s Direction validation passed: %s -> %s (%s). Rule: %s",
				logPrefix, sourceType, targetType, relationType, rule.Description)
		}
	}

	// Policy 2.55: HAS_MARKET_SEGMENT path rule (best-effort). Ideal: child path = parent path + one segment.
	// Policy 2.57: ORG↔MARKET must be PARTICIPATES_IN.
	isOrgMarketPair := (sourceType == string(horizonmodels.FragmentTypeOrganization) && targetType == string(horizonmodels.FragmentTypeMarket)) ||
		(sourceType == string(horizonmodels.FragmentTypeMarket) && targetType == string(horizonmodels.FragmentTypeOrganization))
	if isOrgMarketPair &&
		relationType != string(horizonmodels.DependencyTypeParticipatesIn) {
		reason := "ORG↔MARKET edges must use PARTICIPATES_IN (direction ORGANIZATION -> MARKET)"
		log.Printf("%s QUARANTINE: %s", logPrefix, reason)
		return PolicyDecision{
			Action: ActionQuarantineSoft,
			Params: map[string]interface{}{
				"penalty": 0.30,
				"reason":  reason,
			},
			DecisionPath: "rule:hardcoded:org_market_participates_only",
			Confidence:   0.70,
		}, nil
	}

	// Policy 2.55: HAS_MARKET_SEGMENT path rule (best-effort). Ideal: child path = parent path + one segment.
	if relationType == string(horizonmodels.DependencyTypeHasMarketSegment) &&
		sourceType == string(horizonmodels.FragmentTypeMarket) &&
		targetType == string(horizonmodels.FragmentTypeMarket) &&
		sourcePath != "" && targetPath != "" &&
		strings.HasPrefix(sourcePath, "/market/") && strings.HasPrefix(targetPath, "/market/") {
		if !horizonmodels.MarketSegmentPathsFollowRule(sourcePath, targetPath) {
			reason := "HAS_MARKET_SEGMENT: ideal rule is child path = parent path + one segment; paths deviate (best-effort)"
			log.Printf("%s PENALTY: %s", logPrefix, reason)
			return PolicyDecision{
				Action: ActionAllowWithPenalty,
				Params: map[string]interface{}{
					"penalty": 0.05,
					"reason":  reason,
				},
				DecisionPath: "rule:hardcoded:has_market_segment_path_rule",
				Confidence:   0.95,
			}, nil
		}
	}

	// Policy 2.5: Soft-penalty for weak but semantically plausible links.
	if relationType == string(horizonmodels.DependencyTypeInfluences) &&
		sourceType == string(horizonmodels.FragmentTypeOrganization) &&
		targetType == string(horizonmodels.FragmentTypeSphere) {
		reason := "INFLUENCES to SPHERE treated as weak-link; preserved with penalty"
		return PolicyDecision{
			Action: ActionAllowWithPenalty,
			Params: map[string]interface{}{
				"penalty":      0.15,
				"reason":       reason,
				"edgeModality": "potential",
			},
			DecisionPath: "rule:hardcoded:influences_sphere_penalty",
			Confidence:   0.85,
		}, nil
	}

	// Policy 3: Validate influence qualifiers for INFLUENCES edges
	if input.Edge != nil && input.Edge.Type == horizonmodels.DependencyTypeInfluences {
		qualifiers, err := horizonmodels.ExtractInfluenceQualifiers(input.Edge)
		if err == nil && qualifiers != nil {
			if err := horizonmodels.ValidateInfluenceQualifiers(qualifiers); err != nil {
				reason := fmt.Sprintf("invalid influence qualifiers: %v", err)
				log.Printf("%s REJECT: %s", logPrefix, reason)
				return PolicyDecision{
					Action:       ActionQuarantineSoft,
					Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
					DecisionPath: "rule:hardcoded:invalid_influence_qualifiers",
					Confidence:   1.0,
				}, nil
			}
			log.Printf("%s Influence qualifiers validation passed", logPrefix)
		}
	}

	// Check DB rules
	if p.rulesRepo == nil {
		log.Printf("%s ALLOW: edge validation passed (sourceType=%s, targetType=%s, relationType=%s)",
			logPrefix, sourceType, targetType, relationType)
		return PolicyDecision{
			Action:       ActionAllow,
			DecisionPath: "rule:hardcoded:passed",
			Confidence:   0.7,
		}, nil
	}

	rules, err := p.rulesRepo.GetRules(ctx)
	if err != nil {
		log.Printf("%s ERROR: failed to get rules from repo: %v", logPrefix, err)
		// Continue with hardcoded validation result
	}

	if err == nil && len(rules) > 0 && sourceType != "" && targetType != "" && relationType != "" {
		rule := p.matchRule(rules, sourceType, targetType, relationType)
		if rule != nil {
			log.Printf("%s DB rule matched: id=%s, action=%s, priority=%d",
				logPrefix, rule.ID, rule.Action, rule.Priority)
			switch rule.Action {
			case "reject":
				log.Printf("%s REJECT by DB rule: %s", logPrefix, rule.Reason)
				return PolicyDecision{
					Action:       ActionQuarantineSoft,
					Params:       map[string]interface{}{"reason": rule.Reason, "penalty": 0.2},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			case "penalty":
				log.Printf("%s PENALTY by DB rule: %s (penalty=%.2f)", logPrefix, rule.Reason, rule.ConfidencePenalty)
				return PolicyDecision{
					Action:       ActionAllowWithPenalty,
					Params:       map[string]interface{}{"penalty": rule.ConfidencePenalty, "reason": rule.Reason},
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0 - rule.ConfidencePenalty,
				}, nil
			case "allow":
				log.Printf("%s ALLOW by DB rule: %s", logPrefix, rule.Reason)
				return PolicyDecision{
					Action:       ActionAllow,
					DecisionPath: "rule:db:exact_match",
					RuleID:       &rule.ID,
					Confidence:   1.0,
				}, nil
			}
		}
	}

	log.Printf("%s ALLOW: edge validation passed (sourceType=%s, targetType=%s, relationType=%s)",
		logPrefix, sourceType, targetType, relationType)
	return PolicyDecision{
		Action:       ActionAllow,
		DecisionPath: "rule:hardcoded:passed",
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

// validateEdgeDirection checks edge direction validity (hardcoded from edge_normalizer.go).
func (p *ValidationPolicy) validateEdgeDirection(edgeType horizonmodels.DependencyType, sourceType, targetType horizonmodels.FragmentType) (bool, bool, *EdgeDirectionRule) {
	return edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
}

// EdgeDirectionRule delegates to the unified edgesemantics contract.
type EdgeDirectionRule = edgesemantics.EdgeDirectionRule

func quarantinePenaltyForTriple(edgeType horizonmodels.DependencyType, sourceType, targetType horizonmodels.FragmentType) (float64, string) {
	switch {
	case edgeType == horizonmodels.DependencyTypeConsumes &&
		sourceType == horizonmodels.FragmentTypeOrganization &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return 0.30, "quarantine: ORGANIZATION -> ORGANIZATION CONSUMES is semantically invalid"
	case edgeType == horizonmodels.DependencyTypeConsumes &&
		sourceType == horizonmodels.FragmentTypeOrganization &&
		targetType == horizonmodels.FragmentTypeFacility:
		return 0.30, "quarantine: ORGANIZATION -> FACILITY CONSUMES is semantically invalid"
	case edgeType == horizonmodels.DependencyTypeParticipatesIn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeMarket:
		return 0.25, "quarantine: MARKET -> MARKET PARTICIPATES_IN is semantically invalid"
	case edgeType == horizonmodels.DependencyTypeCompetesWith &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeMarket:
		return 0.25, "quarantine: MARKET -> MARKET COMPETES_WITH is semantically invalid"
	case edgeType == horizonmodels.DependencyTypeAssures &&
		sourceType == horizonmodels.FragmentTypeGeoRegion:
		return 0.40, "quarantine: GEO_REGION cannot be ASSURES source (assurer)"
	case edgeType == horizonmodels.DependencyTypeAuditedBy &&
		sourceType == horizonmodels.FragmentTypeGeoRegion &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return 0.40, "quarantine: GEO_REGION cannot be AUDITED_BY organization"
	case edgeType == horizonmodels.DependencyTypeOperatesVia &&
		sourceType == horizonmodels.FragmentTypeFacility &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return 0.30, "quarantine: FACILITY -> ORGANIZATION OPERATES_VIA requires MEDIA_CHANNEL target"
	case edgeType == horizonmodels.DependencyTypeHasMarketSegment &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeProduct:
		return 0.25, "quarantine: MARKET -> PRODUCT HAS_MARKET_SEGMENT requires MARKET target"
	case edgeType == horizonmodels.DependencyTypeCompetesWith &&
		sourceType == horizonmodels.FragmentTypeProgram &&
		targetType == horizonmodels.FragmentTypeProgram:
		return 0.20, "quarantine: PROGRAM -> PROGRAM COMPETES_WITH is low-confidence in current ontology"
	case edgeType == horizonmodels.DependencyTypeInvolvesActor &&
		sourceType == horizonmodels.FragmentTypePerson &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return 0.30, "quarantine: INVOLVES_ACTOR requires DOMAIN source"
	case edgeType == horizonmodels.DependencyTypeFocusesOn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return 0.30, "quarantine: FOCUSES_ON requires DOMAIN source"
	default:
		return 0.20, ""
	}
}
