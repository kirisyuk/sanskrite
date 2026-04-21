package policy

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
		logPrefix, fragment.ID, fragment.Class, fragment.CanonicalPath)

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

	if fragment.Class == "" {
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

	// Policy 2.4: Enforce canonical path contract.
	if err := taxonomy.ValidateCanonicalPath(strings.TrimSpace(fragment.CanonicalPath)); err != nil {
		reason := fmt.Sprintf("invalid canonical path %q: %v", fragment.CanonicalPath, err)
		log.Printf("%s REJECT: %s", logPrefix, reason)
		fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
		return PolicyDecision{
			Action:       ActionQuarantineSoft,
			Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
			DecisionPath: "rule:hardcoded:invalid_canonical_path",
			Confidence:   1.0,
		}, nil
	}

	// Policy 2.5: Deterministic type/path reconciliation.
	if reconciledClass, reconciledSubtype, changed := edgesemantics.ReconcileClassWithCanonicalPath(fragment.Class, fragment.CanonicalPath); changed {
		originalType := fragment.Class
		originalSubtype := fragment.Subtype
		fragment.Class = reconciledClass
		fragment.Subtype = reconciledSubtype
		reason := fmt.Sprintf("type_path_mismatch: canonicalPath=%q with type=%s/%s reconciled to %s/%s", fragment.CanonicalPath, originalType, originalSubtype, reconciledClass, reconciledSubtype)
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
	expectedPrefix := ExpectedCanonicalPathPrefix(fragment.Class, fragment.Subtype)
	if !IsCanonicalPathAllowedForType(fragment.Class, fragment.Subtype, fragment.CanonicalPath) {
		warning := fmt.Sprintf("canonical path %q does not start with expected prefix %q for type %s/%s",
			fragment.CanonicalPath, expectedPrefix, fragment.Class, fragment.Subtype)
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
	if fragment.Class == classGeoRegion {
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

		cc, _, _, ok := taxonomy.ParseLocCanonicalPath(fragment.CanonicalPath)
		if !ok {
			reason := fmt.Sprintf("GEO_REGION canonicalPath %q must be /loc/{country}[/{level1}[/{level2}]]", fragment.CanonicalPath)
			log.Printf("%s REJECT: %s", logPrefix, reason)
			fragment.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, 0.2)
			return PolicyDecision{
				Action:       ActionQuarantineSoft,
				Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
				DecisionPath: "rule:hardcoded:geo_format",
				Confidence:   1.0,
			}, nil
		}

		if !taxonomy.IsValidLocCountryToken(cc) {
			reason := fmt.Sprintf("GEO_REGION canonicalPath %q has invalid country/region %q (expected %s)",
				fragment.CanonicalPath, cc, taxonomy.ISO2GeoDescriptionForPrompts())
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

	// Check DB rules for additional validation
	if p.rulesRepo == nil {
		log.Printf("%s ALLOW: fragment validation passed (canonicalPath=%s, type=%s)",
			logPrefix, fragment.CanonicalPath, fragment.Class)
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
		rule := p.matchRule(rules, string(fragment.Class), "*", "*")
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
		logPrefix, fragment.CanonicalPath, fragment.Class)
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
	var relationReason string
	var relationQualifiers map[string]interface{}

	if input.Edge != nil {
		sourceID = input.Edge.SourceFragmentID
		targetID = input.Edge.TargetFragmentID
		relationType = string(input.Edge.Relation)
		relationQualifiers = input.Edge.Qualifiers
		if input.Edge.Qualifiers != nil {
			if v, ok := input.Edge.Qualifiers["sourceCanonicalPath"].(string); ok {
				sourcePath = strings.TrimSpace(v)
			}
			if v, ok := input.Edge.Qualifiers["targetCanonicalPath"].(string); ok {
				targetPath = strings.TrimSpace(v)
			}
			if v, ok := input.Edge.Qualifiers["reason"].(string); ok {
				relationReason = strings.TrimSpace(v)
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
		relationReason, _ = input.Context["reason"].(string)
		relationQualifiers = mapFromAny(input.Context["qualifiers"])
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
		sourceType = string(input.Fragment.Class)
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
		if reconciledClass, _, changed := edgesemantics.ReconcileClassWithCanonicalPath(ah.FragmentClass(sourceType), sourcePath); changed {
			log.Printf("%s INFO: type_path_mismatch source reconciled: path=%s type=%s -> %s",
				logPrefix, sourcePath, sourceType, reconciledClass)
			sourceType = string(reconciledClass)
		}
	}
	if targetType != "" && targetPath != "" {
		if reconciledClass, _, changed := edgesemantics.ReconcileClassWithCanonicalPath(ah.FragmentClass(targetType), targetPath); changed {
			log.Printf("%s INFO: type_path_mismatch target reconciled: path=%s type=%s -> %s",
				logPrefix, targetPath, targetType, reconciledClass)
			targetType = string(reconciledClass)
		}
	}

	// Policy 2: Edge direction validation (hardcoded from EdgeDirectionPolicies)
	if sourceType != "" && targetType != "" && relationType != "" {
		edgeType := ah.RelationName(relationType)
		isValid, shouldReverse, rule := p.validateEdgeDirection(edgeType,
			ah.FragmentClass(sourceType), ah.FragmentClass(targetType))

		if !isValid {
			// Exceptional supply shortcut takes precedence over both reverse and quarantine
			// dispositions: deposit→ORG/FAC SUPPLIES_TO is preserved with penalty even though
			// AH catalog says it should reverse (deposits are structurally upstream).
			baseReason := fmt.Sprintf("edge direction invalid: %s -> %s (%s). Rule: %s",
				sourceType, targetType, relationType, rule.Description)
			if decision, ok := allowExceptionalSupplyShortcut(
				edgeType,
				ah.FragmentClass(sourceType),
				ah.FragmentClass(targetType),
				baseReason,
			); ok {
				log.Printf("%s ALLOW_WITH_PENALTY: %s (penalty=%.2f)", logPrefix, decision.Params["reason"], decision.Params["penalty"])
				return decision, nil
			}
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
				if decision, ok := allowExceptionalSupplyShortcut(
					edgeType,
					ah.FragmentClass(sourceType),
					ah.FragmentClass(targetType),
					baseReason,
				); ok {
					log.Printf("%s ALLOW_WITH_PENALTY: %s (penalty=%.2f)", logPrefix, decision.Params["reason"], decision.Params["penalty"])
					return decision, nil
				}
				penalty, reason := quarantinePenaltyForTriple(
					edgeType,
					ah.FragmentClass(sourceType),
					ah.FragmentClass(targetType),
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

	// Policy 2.2: role-based disambiguation for directionally ambiguous edges.
	// Type compatibility alone does not resolve source/target semantics for these relations.
	// Exception: supply-chain-originated edges already have structurally determined direction
	// (the parser resolves source/target based on fragment type analysis).
	fromSupplyChain, _ := input.Context["fromSupplyChain"].(bool)
	if relationType != "" && !fromSupplyChain {
		edgeType := ah.RelationName(relationType)
		if requiresDirectionalRoleEvidence(edgeType, ah.FragmentClass(sourceType), ah.FragmentClass(targetType)) &&
			!hasDirectionalRoleEvidence(edgeType, relationReason, relationQualifiers) {
			reason := fmt.Sprintf("%s requires explicit direction evidence: qualifiers.source_role/target_role (or direction_basis)", edgeType)
			log.Printf("%s QUARANTINE: %s", logPrefix, reason)
			return PolicyDecision{
				Action: ActionQuarantineSoft,
				Params: map[string]interface{}{
					"penalty": 0.35,
					"reason":  reason,
				},
				DecisionPath: "rule:hardcoded:direction_role_ambiguous",
				Confidence:   0.65,
			}, nil
		}
	}

	// Policy 2.55: HAS_MARKET_SEGMENT path rule (best-effort). Ideal: child path = parent path + one segment.
	// Policy 2.56: PRODUCT↔MARKET must use TRADED_IN.
	isProductMarketPair := (sourceType == string(classProduct) && targetType == string(classMarket)) ||
		(sourceType == string(classMarket) && targetType == string(classProduct))
	if isProductMarketPair &&
		relationType != string(relTradedIn) {
		reason := "PRODUCT↔MARKET edges must use TRADED_IN (direction PRODUCT -> MARKET); PARTICIPATES_IN is for actors, not products"
		log.Printf("%s QUARANTINE: %s", logPrefix, reason)
		return PolicyDecision{
			Action: ActionQuarantineSoft,
			Params: map[string]interface{}{
				"penalty": 0.35,
				"reason":  reason,
			},
			DecisionPath: "rule:hardcoded:product_market_traded_in_only",
			Confidence:   0.85,
		}, nil
	}

	// Policy 2.57: ORG↔MARKET must be PARTICIPATES_IN — except GOV entities which may
	// REGULATES/CONTROLS_ACCESS/INFLUENCES/ADMINISTERS markets.
	isOrgMarketPair := (sourceType == string(classOrganization) && targetType == string(classMarket)) ||
		(sourceType == string(classMarket) && targetType == string(classOrganization))
	if isOrgMarketPair &&
		relationType != string(relParticipatesIn) {
		govPath := sourcePath
		if targetType == string(classOrganization) {
			govPath = targetPath
		}
		isGovEntity := strings.HasPrefix(govPath, "/gov/")
		isGovValidEdge := isGovEntity && (relationType == string(relRegulates) ||
			relationType == string(relControlsAccess) ||
			relationType == string(relInfluences) ||
			relationType == string(relAdministers))
		if !isGovValidEdge {
			reason := "ORG↔MARKET edges must use PARTICIPATES_IN (direction ORGANIZATION -> MARKET); GOV entities may also use REGULATES/CONTROLS_ACCESS/INFLUENCES/ADMINISTERS"
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
	}

	// Policy 2.55: HAS_MARKET_SEGMENT path rule (best-effort). Ideal: child path = parent path + one segment.
	if relationType == string(relHasMarketSegment) &&
		sourceType == string(classMarket) &&
		targetType == string(classMarket) &&
		sourcePath != "" && targetPath != "" &&
		strings.HasPrefix(sourcePath, "/market/") && strings.HasPrefix(targetPath, "/market/") {
		if !MarketSegmentPathsFollowRule(sourcePath, targetPath) {
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
	if relationType == string(relInfluences) &&
		sourceType == string(classOrganization) &&
		targetType == string(classSphere) {
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

	// Policy 3: Validate influence qualifiers for INFLUENCES edges.
	//
	// The legacy strongly-typed InfluenceQualifiers struct + Extract/Validate
	// helpers were removed during the AH migration. The new contract treats
	// qualifiers as opaque map[string]interface{} that the canonicalizer
	// validates against the AH closed-enum catalog. Until the catalog
	// validator is wired in here, we only run a minimal shape check
	// (closed-enum direction values).
	if input.Edge != nil && input.Edge.Relation == relInfluences && input.Edge.Qualifiers != nil {
		if dirRaw, ok := input.Edge.Qualifiers["direction"]; ok {
			if dir, isStr := dirRaw.(string); isStr {
				switch strings.ToLower(strings.TrimSpace(dir)) {
				case "", "positive", "negative", "neutral":
					// valid
				default:
					reason := fmt.Sprintf("invalid influence qualifier: direction=%q", dir)
					log.Printf("%s REJECT: %s", logPrefix, reason)
					return PolicyDecision{
						Action:       ActionQuarantineSoft,
						Params:       map[string]interface{}{"reason": reason, "penalty": 0.2},
						DecisionPath: "rule:hardcoded:invalid_influence_qualifiers",
						Confidence:   1.0,
					}, nil
				}
			}
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

// validateEdgeDirection checks edge direction validity. It consults the
// canonical AH endpoint catalog via edgesemantics and the local
// legacyEdgeDirectionPairs table. The legacy table is treated as additive:
// when AH rejects (or only suggests reverse), a legacy-accepted pair wins
// to keep historical wire payloads working through the policy layer.
func (p *ValidationPolicy) validateEdgeDirection(edgeType ah.RelationName, sourceType, targetType ah.FragmentClass) (bool, bool, *EdgeDirectionRule) {
	ahValid, ahReverse, ahRule := edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
	if ahValid && ahRule != nil && len(ahRule.Pairs) > 0 {
		return true, false, ahRule
	}
	if v, _, lrule, recognized := legacyValidateEdgeDirection(edgeType, sourceType, targetType); recognized && v {
		return true, false, lrule
	}
	if ahValid {
		return true, false, ahRule
	}
	if ahReverse {
		return false, true, ahRule
	}
	if _, _, lrule, recognized := legacyValidateEdgeDirection(edgeType, sourceType, targetType); recognized {
		return false, false, lrule
	}
	return ahValid, ahReverse, ahRule
}

// EdgeDirectionRule delegates to the unified edgesemantics contract.
type EdgeDirectionRule = edgesemantics.EdgeDirectionRule

func quarantinePenaltyForTriple(edgeType ah.RelationName, sourceType, targetType ah.FragmentClass) (float64, string) {
	switch {
	case edgeType == relParticipatesIn &&
		sourceType == classMarket &&
		targetType == classMarket:
		return 0.25, "quarantine: MARKET -> MARKET PARTICIPATES_IN is semantically invalid"
	case edgeType == relCompetesWith &&
		sourceType == classMarket &&
		targetType == classMarket:
		return 0.25, "quarantine: MARKET -> MARKET COMPETES_WITH is semantically invalid"
	case edgeType == relAssures &&
		sourceType == classGeoRegion:
		return 0.40, "quarantine: GEO_REGION cannot be ASSURES source (assurer)"
	case edgeType == relAuditedBy &&
		sourceType == classGeoRegion &&
		targetType == classOrganization:
		return 0.40, "quarantine: GEO_REGION cannot be AUDITED_BY organization"
	case edgeType == relOperatesVia &&
		sourceType == classFacility &&
		targetType == classOrganization:
		return 0.30, "quarantine: FACILITY -> ORGANIZATION OPERATES_VIA requires MEDIA_CHANNEL target"
	case edgeType == relHasMarketSegment &&
		sourceType == classMarket &&
		targetType == classProduct:
		return 0.25, "quarantine: MARKET -> PRODUCT HAS_MARKET_SEGMENT requires MARKET target"
	case edgeType == relCompetesWith &&
		sourceType == classProgram &&
		targetType == classProgram:
		return 0.20, "quarantine: PROGRAM -> PROGRAM COMPETES_WITH is low-confidence in current ontology"
	case edgeType == relInvolvesActor &&
		sourceType == classPerson &&
		targetType == classOrganization:
		return 0.30, "quarantine: INVOLVES_ACTOR requires DOMAIN source"
	case edgeType == relFocusesOn &&
		sourceType == classMarket &&
		targetType == classOrganization:
		return 0.30, "quarantine: FOCUSES_ON requires DOMAIN source"
	default:
		return 0.20, ""
	}
}

func allowExceptionalSupplyShortcut(
	edgeType ah.RelationName,
	sourceType, targetType ah.FragmentClass,
	baseReason string,
) (PolicyDecision, bool) {
	if edgeType == relSuppliesTo &&
		sourceType == classDeposit &&
		(targetType == classOrganization || targetType == classFacility) {
		reason := "exceptional supply shortcut preserved with penalty: deposit can stand in for an upstream resource source when the extracting/processing facility is not yet known"
		if strings.TrimSpace(baseReason) != "" {
			reason += "; " + baseReason
		}
		return PolicyDecision{
			Action: ActionAllowWithPenalty,
			Params: map[string]interface{}{
				"penalty":      0.30,
				"reason":       reason,
				"edgeModality": "potential",
			},
			DecisionPath: "rule:hardcoded:deposit_supply_shortcut_penalty",
			Confidence:   0.70,
		}, true
	}
	return PolicyDecision{}, false
}

func requiresDirectionalRoleEvidence(
	edgeType ah.RelationName,
	sourceType, targetType ah.FragmentClass,
) bool {
	isOrg := func(t ah.FragmentClass) bool { return t == classOrganization }
	isFac := func(t ah.FragmentClass) bool { return t == classFacility }
	isMechanism := func(t ah.FragmentClass) bool { return t == classMechanism }
	isProgram := func(t ah.FragmentClass) bool { return t == classProgram }
	isMarket := func(t ah.FragmentClass) bool { return t == classMarket }
	isProduct := func(t ah.FragmentClass) bool { return t == classProduct }
	isCommunity := func(t ah.FragmentClass) bool { return t == classCommunity }
	isPerson := func(t ah.FragmentClass) bool { return t == classPerson }
	isActorLike := func(t ah.FragmentClass) bool {
		return isOrg(t) || isFac(t) || isProgram(t) || isCommunity(t) || isPerson(t)
	}
	isPolicyTarget := func(t ah.FragmentClass) bool {
		return isActorLike(t) || isMarket(t) || isProduct(t)
	}

	switch edgeType {
	case relOwns,
		relSubsidiaryOf,
		relInvestsIn,
		relAssures,
		relControls:
		return isOrg(sourceType) && isOrg(targetType)
	case relDependsOn:
		return isActorLike(sourceType) && isActorLike(targetType)
	case relSuppliesTo:
		// PRODUCT→ORG/FAC: direction clear, no role evidence needed. ORG/FAC→ORG/FAC: may need evidence.
		if sourceType == classProduct && (isOrg(targetType) || isFac(targetType)) {
			return false
		}
		return (isOrg(sourceType) || isFac(sourceType)) && (isOrg(targetType) || isFac(targetType))
	case relControlsAccess:
		return (isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType)) && isPolicyTarget(targetType)
	case relImposesCondition:
		return (isMechanism(sourceType) || isOrg(sourceType) || isProgram(sourceType) || isCommunity(sourceType)) && isPolicyTarget(targetType)
	default:
		return false
	}
}

func hasDirectionalRoleEvidence(edgeType ah.RelationName, reason string, qualifiers map[string]interface{}) bool {
	_ = reason // role qualifiers are the deterministic contract for directional ambiguity handling.
	sourceRole := normalizedRole(readStringAny(qualifiers, "source_role", "sourceRole", "subject_role", "subjectRole"))
	targetRole := normalizedRole(readStringAny(qualifiers, "target_role", "targetRole", "object_role", "objectRole"))
	if sourceRole != "" || targetRole != "" {
		return rolePairMatchesEdgeType(edgeType, sourceRole, targetRole)
	}

	dir := normalizedRole(readStringAny(qualifiers, "direction_basis", "directionBasis", "relation_direction", "relationDirection"))
	return directionQualifierMatchesEdgeType(edgeType, dir)
}

func rolePairMatchesEdgeType(edgeType ah.RelationName, sourceRole, targetRole string) bool {
	switch edgeType {
	case relOwns:
		return inRole(sourceRole, "owner", "parent_owner", "shareholder", "beneficial_owner") &&
			inRole(targetRole, "owned_entity", "subsidiary", "asset")
	case relSubsidiaryOf:
		return inRole(sourceRole, "subsidiary", "owned_entity", "child_entity") &&
			inRole(targetRole, "parent_owner", "owner", "parent")
	case relInvestsIn:
		return inRole(sourceRole, "investor") &&
			inRole(targetRole, "investee", "portfolio_company", "target")
	case relAssures:
		return inRole(sourceRole, "assurer", "auditor", "certifier", "inspector", "regulator") &&
			inRole(targetRole, "assuree", "audited_entity", "certified_entity", "inspected_entity", "regulated_entity")
	case relControls:
		return inRole(sourceRole, "controller") &&
			inRole(targetRole, "controlled", "controlled_entity")
	case relDependsOn:
		return inRole(sourceRole, "dependent", "consumer", "requirer", "requester") &&
			inRole(targetRole, "dependency", "supplier", "provider", "enabler", "input")
	case relSuppliesTo:
		return inRole(sourceRole, "supplier", "provider", "seller", "producer", "vendor") &&
			inRole(targetRole, "customer", "buyer", "consumer", "recipient", "offtaker")
	case relGatekeepsDecision:
		return inRole(sourceRole, "gatekeeper", "approver", "decision_maker", "authority", "veto_holder") &&
			inRole(targetRole, "decision_point", "decision", "approval", "ruling")
	case relUsesMechanism:
		return inRole(sourceRole, "mechanism_user", "policy_source", "decision", "regulator", "administrator") &&
			inRole(targetRole, "mechanism", "instrument", "policy_mechanism")
	case relControlsAccess:
		return inRole(sourceRole, "controller", "gatekeeper", "access_controller", "regulator") &&
			inRole(targetRole, "access_subject", "controlled_access_target", "affected_entity", "regulated_entity", "subject")
	case relImposesCondition:
		return inRole(sourceRole, "imposer", "condition_imposer", "regulator", "mechanism") &&
			inRole(targetRole, "constrained_entity", "conditioned_entity", "affected_entity", "subject")
	case relAppliesTo:
		return inRole(sourceRole, "mechanism", "instrument", "rule") &&
			inRole(targetRole, "applicable_entity", "covered_entity", "subject", "affected_entity")
	case relAdministers:
		return inRole(sourceRole, "administrator", "administering_body", "operator") &&
			inRole(targetRole, "administered_entity", "administered_program", "subordinate")
	case relInGeoScope:
		return inRole(sourceRole, "scope_owner", "scoped_instrument", "mechanism", "policy_source") &&
			inRole(targetRole, "geo_scope", "jurisdiction", "geography", "location")
	case relAffects:
		return inRole(sourceRole, "impact_source", "mechanism", "source", "driver") &&
			inRole(targetRole, "affected_entity", "impacted_entity", "exposure_target")
	default:
		return false
	}
}

func directionQualifierMatchesEdgeType(edgeType ah.RelationName, direction string) bool {
	switch edgeType {
	case relOwns:
		return inRole(direction, "owner_to_owned", "parent_to_subsidiary")
	case relSubsidiaryOf:
		return inRole(direction, "subsidiary_to_parent", "child_to_parent")
	case relInvestsIn:
		return inRole(direction, "investor_to_investee")
	case relAssures:
		return inRole(direction, "assurer_to_assuree", "auditor_to_audited")
	case relControls:
		return inRole(direction, "controller_to_controlled")
	case relDependsOn:
		return inRole(direction, "dependent_to_dependency", "consumer_to_supplier")
	case relSuppliesTo:
		return inRole(direction, "supplier_to_customer", "provider_to_recipient")
	case relGatekeepsDecision:
		return inRole(direction, "gatekeeper_to_decision", "authority_to_decision", "approver_to_decision")
	case relUsesMechanism:
		return inRole(direction, "user_to_mechanism", "decision_to_mechanism", "source_to_mechanism")
	case relControlsAccess:
		return inRole(direction, "controller_to_access_subject", "gatekeeper_to_subject", "controller_to_target")
	case relImposesCondition:
		return inRole(direction, "imposer_to_constrained_entity", "mechanism_to_constrained_entity", "regulator_to_constrained_entity")
	case relAppliesTo:
		return inRole(direction, "mechanism_to_applicable_entity", "instrument_to_subject")
	case relAdministers:
		return inRole(direction, "administrator_to_administered_entity", "operator_to_administered_entity")
	case relInGeoScope:
		return inRole(direction, "scope_owner_to_geo_scope", "mechanism_to_geo_scope", "instrument_to_jurisdiction")
	case relAffects:
		return inRole(direction, "impact_source_to_affected_entity", "source_to_affected_entity", "driver_to_affected_entity")
	default:
		return false
	}
}

func readStringAny(m map[string]interface{}, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch s := v.(type) {
			case string:
				if strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

func inRole(role string, allowed ...string) bool {
	role = normalizedRole(role)
	if role == "" {
		return false
	}
	for _, a := range allowed {
		if role == normalizedRole(a) {
			return true
		}
	}
	return false
}

func normalizedRole(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func mapFromAny(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	case map[string]string:
		out := make(map[string]interface{}, len(m))
		for k, vv := range m {
			out[k] = vv
		}
		return out
	default:
		return nil
	}
}
