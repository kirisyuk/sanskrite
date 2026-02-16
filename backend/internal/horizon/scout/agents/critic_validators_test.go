package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// deprecatedRelationTypes must not appear in composable validator prompts (TZ 6.3.1).
var deprecatedRelationTypes = []string{
	"RELATED_TO", "AFFILIATED_WITH", "PARTNERS_WITH", "DOMINATES",
	"WORKS_FOR", "HAS_KEY_PERSON", "RENAMED_TO", "KEY_PERSON",
	"ABOUT", "AFFECTS", "SURFACES", "INVOLVES_ACTOR", "IN_GEO_SCOPE",
	"RELATED_DOMAIN", "BROADENS_TO", "NARROWS_TO",
}

func TestComposableValidationPrompts_DoNotContainDeprecatedTypes(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          "ORGANIZATION",
		CanonicalPath: "/org/us/example-inc",
	}
	input := CriticValidatorInput{
		Fragment:  frag,
		Relations: nil,
	}
	agent := NewCriticAgent(nil)

	prompts := []struct {
		name   string
		build  func(CriticValidatorInput) string
	}{
		{"Core", agent.buildCoreValidationPrompt},
		{"SupplyChain", agent.buildSupplyChainValidationPrompt},
		{"Governance", agent.buildGovernanceValidationPrompt},
		{"Market", agent.buildMarketValidationPrompt},
	}
	for _, p := range prompts {
		t.Run(p.name, func(t *testing.T) {
			prompt := p.build(input)
			upper := strings.ToUpper(prompt)
			for _, dep := range deprecatedRelationTypes {
				if strings.Contains(upper, dep) {
					t.Errorf("composable %s prompt must not mention deprecated type %q", p.name, dep)
				}
			}
		})
	}
}

func TestCriticPipeline_Validate_supplyChainMode(t *testing.T) {
	runner := (&CriticAgent{}).CriticPipeline()
	ctx := context.Background()
	input := CriticValidatorInput{Fragment: &models.HorizonFragment{}}
	modes := []string{taxonomy.DomainModeSupplyChain}

	result, err := runner.Validate(ctx, input, modes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil merged result")
	}
	// With nil agent, all validators return nil; pipeline still returns merged (empty) result.
	if !result.IsValid {
		t.Errorf("expected isValid=true for empty merge")
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence=1.0, got %f", result.Confidence)
	}
}

func TestCriticPipeline_Validate_governanceMode(t *testing.T) {
	runner := (&CriticAgent{}).CriticPipeline()
	ctx := context.Background()
	input := CriticValidatorInput{Fragment: &models.HorizonFragment{}}
	modes := []string{taxonomy.DomainModeGovernance}

	result, err := runner.Validate(ctx, input, modes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil merged result")
	}
	if !result.IsValid {
		t.Errorf("expected isValid=true")
	}
}

func TestCriticPipeline_Validate_generalModeSkipsComposable(t *testing.T) {
	runner := (&CriticAgent{}).CriticPipeline()
	modes := []string{taxonomy.DomainModeGeneral}

	validators := runner.validatorsForModes(modes)
	// general should yield only Core (no domain-specific validators)
	if len(validators) != 1 {
		t.Errorf("general mode: expected 1 validator (Core), got %d", len(validators))
	}
	if len(validators) > 0 && validators[0].DomainMode() != "core" {
		t.Errorf("first validator should be Core, got %q", validators[0].DomainMode())
	}
}

func TestCriticPipeline_Validate_compositeModes(t *testing.T) {
	runner := (&CriticAgent{}).CriticPipeline()
	modes := []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance}
	validators := runner.validatorsForModes(modes)
	// Core + SupplyChain + Governance
	if len(validators) != 3 {
		t.Errorf("expected 3 validators (Core, SupplyChain, Governance), got %d", len(validators))
	}
	modesSeen := make(map[string]bool)
	for _, v := range validators {
		modesSeen[v.DomainMode()] = true
	}
	for _, want := range []string{"core", taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance} {
		if !modesSeen[want] {
			t.Errorf("missing validator for mode %q", want)
		}
	}
}

func TestStrategicValidator_nilFragmentReturnsNil(t *testing.T) {
	v := &StrategicValidator{agent: &CriticAgent{}}
	ctx := context.Background()
	input := CriticValidatorInput{Fragment: nil}
	result, err := v.Validate(ctx, input)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for nil fragment, got %+v", result)
	}
}
