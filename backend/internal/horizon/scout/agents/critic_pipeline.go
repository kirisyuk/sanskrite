package agents

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CriticPipeline runs domain-specific validators and merges results.
func (a *CriticAgent) CriticPipeline() *CriticPipelineRunner {
	return &CriticPipelineRunner{agent: a}
}

// CriticPipelineRunner runs validators by domain mode.
type CriticPipelineRunner struct {
	agent *CriticAgent
}

// Validate runs domain-specific validation. By default, it uses a single composed LLM call
// for Core + all non-strategic domain modes. Strategic mode uses a dedicated overlay prompt.
// Falls back to per-validator sequential loop when composed mode is unsuitable.
func (p *CriticPipelineRunner) Validate(ctx context.Context, input CriticValidatorInput, domainModes []string) (*ValidationResult, error) {
	domainModes = contractsemantics.SortDomainModesForExecution(domainModes)
	hasStrategic := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeStrategic)
	nonStrategicModes := filterOutMode(domainModes, taxonomy.DomainModeStrategic)

	var result *ValidationResult

	// Composed validation: single LLM call for Core + all non-strategic domain modes.
	if len(nonStrategicModes) > 0 && p.agent != nil {
		prompt := p.agent.buildComposedValidationPrompt(input, nonStrategicModes)
		var err error
		result, err = p.agent.runValidationPrompt(ctx, prompt)
		if err != nil {
			return nil, err
		}
	}

	// When the executor is unavailable (nil) or returned nothing, provide a baseline result
	// so callers always get a valid (possibly empty) ValidationResult.
	if result == nil && len(nonStrategicModes) > 0 {
		result = &ValidationResult{IsValid: true, Confidence: 1.0, MissingInfo: []string{}, Conflicts: []ValidationConflict{}}
	}

	// Strategic overlay: only for DECISION_POINT/MECHANISM entities or entities with strategic relations.
	// Skip for MARKET/GEO_REGION/ORG to avoid false "Rejected" when Critic expects strategic schema but Scout produced different type.
	if hasStrategic && input.Fragment != nil && p.agent != nil {
		ft := input.Fragment.Type
		cp := strings.ToLower(strings.TrimSpace(input.Fragment.CanonicalPath))
		isStrategicEntity := ft == models.FragmentTypeDecisionPoint || ft == models.FragmentTypeMechanism ||
			strings.HasPrefix(cp, "/decision/") || strings.HasPrefix(cp, "/mechanism/")
		if !isStrategicEntity {
			isStrategicEntity = hasStrategicRelations(input.Relations)
		}
		if isStrategicEntity {
			stratResult, err := p.agent.ValidateStrategicOverlay(ctx, input.Fragment, input.Relations, input.GraphContext)
			if err == nil && stratResult != nil {
				result = mergeValidationResults(result, stratResult)
			}
		}
	}

	// Deterministic invariant checks (no LLM).
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) {
		invariantGaps := checkSupplyChainInvariants(input.SupplyChains, input.WorkingProductScope)
		if len(invariantGaps) > 0 {
			if result == nil {
				result = &ValidationResult{IsValid: true, Confidence: 1.0, MissingInfo: []string{}, Conflicts: []ValidationConflict{}}
			}
			result.MissingInfo = append(result.MissingInfo, invariantGaps...)
			result.IsValid = false
			if result.Confidence > 0.5 {
				result.Confidence = 0.5
			}
		}
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGovernance) {
		invariantGaps := checkGovernanceInvariants(input.Relations)
		if len(invariantGaps) > 0 {
			if result == nil {
				result = &ValidationResult{IsValid: true, Confidence: 1.0, MissingInfo: []string{}, Conflicts: []ValidationConflict{}}
			}
			result.MissingInfo = append(result.MissingInfo, invariantGaps...)
			result.IsValid = false
			if result.Confidence > 0.6 {
				result.Confidence = 0.6
			}
		}
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModePolicyImpact) {
		invariantGaps := checkPolicyImpactInvariants(input.Relations)
		if len(invariantGaps) > 0 {
			if result == nil {
				result = &ValidationResult{IsValid: true, Confidence: 1.0, MissingInfo: []string{}, Conflicts: []ValidationConflict{}}
			}
			result.MissingInfo = append(result.MissingInfo, invariantGaps...)
			result.IsValid = false
			if result.Confidence > 0.6 {
				result.Confidence = 0.6
			}
		}
	}

	return result, nil
}

// mergeValidationResults merges b into a, returning the combined result.
func mergeValidationResults(a, b *ValidationResult) *ValidationResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	a.HasConflicts = a.HasConflicts || b.HasConflicts

	// Dedup conflicts by Field — keep higher severity per field.
	severityOrder := map[string]int{"info": 0, "warning": 1, "error": 2, "critical": 3}
	conflictByField := make(map[string]ValidationConflict, len(a.Conflicts))
	for _, c := range a.Conflicts {
		conflictByField[c.Field] = c
	}
	for _, c := range b.Conflicts {
		if existing, ok := conflictByField[c.Field]; ok {
			if severityOrder[strings.ToLower(c.Severity)] > severityOrder[strings.ToLower(existing.Severity)] {
				conflictByField[c.Field] = c
			}
		} else {
			conflictByField[c.Field] = c
		}
	}
	deduped := make([]ValidationConflict, 0, len(conflictByField))
	for _, c := range conflictByField {
		deduped = append(deduped, c)
	}
	a.Conflicts = deduped

	// Dedup MissingInfo by lowercased trimmed string.
	seenMissing := make(map[string]struct{}, len(a.MissingInfo))
	merged := make([]string, 0, len(a.MissingInfo)+len(b.MissingInfo))
	for _, m := range a.MissingInfo {
		k := strings.ToLower(strings.TrimSpace(m))
		if _, ok := seenMissing[k]; !ok {
			seenMissing[k] = struct{}{}
			merged = append(merged, m)
		}
	}
	for _, m := range b.MissingInfo {
		k := strings.ToLower(strings.TrimSpace(m))
		if _, ok := seenMissing[k]; !ok {
			seenMissing[k] = struct{}{}
			merged = append(merged, m)
		}
	}
	a.MissingInfo = merged

	if b.Confidence < a.Confidence {
		a.Confidence = b.Confidence
	}
	a.IsValid = a.IsValid && b.IsValid
	return a
}

// hasStrategicRelations returns true if any relation type maps to SemanticGroupStrategic.
func hasStrategicRelations(relations []DiscoveredRelation) bool {
	for _, r := range relations {
		dt := models.DependencyType(strings.ToUpper(strings.TrimSpace(r.RelationType)))
		if models.SemanticGroupForEdgeType(dt) == models.SemanticGroupStrategic {
			return true
		}
	}
	return false
}

// filterOutMode returns a copy of modes with the specified mode removed.
func filterOutMode(modes []string, exclude string) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		if strings.TrimSpace(strings.ToLower(m)) != exclude {
			out = append(out, m)
		}
	}
	return out
}

// validatorsForModes returns the list of validators to run: Core first, then by mode.
func (p *CriticPipelineRunner) validatorsForModes(modes []string) []CriticValidator {
	modes = contractsemantics.SortDomainModesForExecution(modes)
	out := make([]CriticValidator, 0, 5)
	// Core always first
	out = append(out, &CoreValidator{agent: p.agent})
	seen := map[string]bool{}
	for _, m := range modes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m == "" || m == taxonomy.DomainModeGeneral {
			continue
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		switch m {
		case taxonomy.DomainModeSupplyChain:
			out = append(out, &SupplyChainValidator{agent: p.agent})
		case taxonomy.DomainModeGovernance:
			out = append(out, &GovernanceValidator{agent: p.agent})
		case taxonomy.DomainModeAssurance:
			out = append(out, &AssuranceValidator{agent: p.agent})
		case taxonomy.DomainModeMarket:
			out = append(out, &MarketValidator{agent: p.agent})
		case taxonomy.DomainModeStrategic:
			out = append(out, &StrategicValidator{agent: p.agent})
		case taxonomy.DomainModePolicyImpact:
			out = append(out, &PolicyImpactValidator{agent: p.agent})
		case taxonomy.DomainModeEcosystem:
			out = append(out, &EcosystemValidator{agent: p.agent})
		}
	}
	return out
}
