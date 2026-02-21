package agents

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CoreValidator validates canonical path, lifecycle, and basic fields.
type CoreValidator struct {
	agent *CriticAgent
}

func (v *CoreValidator) DomainMode() string { return "core" }

func (v *CoreValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildCoreValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// SupplyChainValidator validates supply chains and related edges.
type SupplyChainValidator struct {
	agent *CriticAgent
}

func (v *SupplyChainValidator) DomainMode() string { return taxonomy.DomainModeSupplyChain }

func (v *SupplyChainValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildSupplyChainValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// GovernanceValidator validates ownership, board, key persons.
type GovernanceValidator struct {
	agent *CriticAgent
}

func (v *GovernanceValidator) DomainMode() string { return taxonomy.DomainModeGovernance }

func (v *GovernanceValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildGovernanceValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// AssuranceValidator validates audit, certification, regulatory oversight.
type AssuranceValidator struct {
	agent *CriticAgent
}

func (v *AssuranceValidator) DomainMode() string { return taxonomy.DomainModeAssurance }

func (v *AssuranceValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildAssuranceValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// MarketValidator validates market participation and sizing.
type MarketValidator struct {
	agent *CriticAgent
}

func (v *MarketValidator) DomainMode() string { return taxonomy.DomainModeMarket }

func (v *MarketValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildMarketValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// StrategicValidator wraps ValidateStrategicOverlay for the strategic domain mode.
type StrategicValidator struct {
	agent *CriticAgent
}

func (v *StrategicValidator) DomainMode() string { return taxonomy.DomainModeStrategic }

func (v *StrategicValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil || input.Fragment == nil {
		return nil, nil
	}
	return v.agent.ValidateStrategicOverlay(ctx, input.Fragment, input.Relations, input.GraphContext)
}

// PolicyImpactValidator validates policy-impact specific relation quality and coverage.
type PolicyImpactValidator struct {
	agent *CriticAgent
}

func (v *PolicyImpactValidator) DomainMode() string { return taxonomy.DomainModePolicyImpact }

func (v *PolicyImpactValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildPolicyImpactValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}
