package agents

import (
	"context"
	"strings"

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

	// Strategic overlay: separate validation (uses different prompt structure).
	if hasStrategic && input.Fragment != nil && p.agent != nil {
		stratResult, err := p.agent.ValidateStrategicOverlay(ctx, input.Fragment, input.Relations, input.GraphContext)
		if err == nil && stratResult != nil {
			result = mergeValidationResults(result, stratResult)
		}
	}

	// Deterministic invariant checks (no LLM).
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) {
		invariantGaps := checkSupplyChainInvariants(input.SupplyChains)
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
	a.Conflicts = append(a.Conflicts, b.Conflicts...)
	a.MissingInfo = append(a.MissingInfo, b.MissingInfo...)
	if b.Confidence < a.Confidence {
		a.Confidence = b.Confidence
	}
	a.IsValid = a.IsValid && b.IsValid
	return a
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
		}
	}
	return out
}
