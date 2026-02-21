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

// Validate runs the appropriate validators for the given domain modes and merges results.
// Core validator always runs first; then SupplyChain/Governance/Market/Strategic per mode.
func (p *CriticPipelineRunner) Validate(ctx context.Context, input CriticValidatorInput, domainModes []string) (*ValidationResult, error) {
	validators := p.validatorsForModes(domainModes)
	if len(validators) == 0 {
		// Fallback: no validators selected (e.g. general with no composable path)
		return nil, nil
	}
	var merged ValidationResult
	merged.Conflicts = []ValidationConflict{}
	merged.MissingInfo = []string{}
	merged.Confidence = 1.0
	merged.IsValid = true

	for _, v := range validators {
		result, err := v.Validate(ctx, input)
		if err != nil {
			return nil, err
		}
		if result == nil {
			continue
		}
		merged.HasConflicts = merged.HasConflicts || result.HasConflicts
		merged.Conflicts = append(merged.Conflicts, result.Conflicts...)
		merged.MissingInfo = append(merged.MissingInfo, result.MissingInfo...)
		if result.Confidence < merged.Confidence {
			merged.Confidence = result.Confidence
		}
		merged.IsValid = merged.IsValid && result.IsValid
	}
	return &merged, nil
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
