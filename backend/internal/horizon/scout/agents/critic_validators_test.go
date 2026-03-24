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
	"RELATED_TO", "PARTNERS_WITH", "DOMINATES",
	"WORKS_FOR", "HAS_KEY_PERSON", "RENAMED_TO", "KEY_PERSON",
	"ABOUT", "SURFACES", "INVOLVES_ACTOR",
	"RELATED_DOMAIN", "BROADENS_TO", "NARROWS_TO",
}

func TestComposableValidationPrompts_DoNotContainDeprecatedTypes(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          "ORGANIZATION",
		CanonicalPath: "/org/us/example-inc",
	}
	input := CriticValidatorInput{
		Fragment:            frag,
		Relations:           nil,
		WorkingProductScope: []string{"/product/material/lithium-chloride"},
	}
	agent := NewCriticAgent(nil)

	prompts := []struct {
		name  string
		build func(CriticValidatorInput) string
	}{
		{"Core", agent.buildCoreValidationPrompt},
		{"SupplyChain", agent.buildSupplyChainValidationPrompt},
		{"Governance", agent.buildGovernanceValidationPrompt},
		{"Market", agent.buildMarketValidationPrompt},
		{"PolicyImpact", agent.buildPolicyImpactValidationPrompt},
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

func TestComposableValidationPrompts_ContainStrictOutputSchema(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          "ORGANIZATION",
		CanonicalPath: "/org/us/example-inc",
	}
	input := CriticValidatorInput{
		Fragment:            frag,
		Relations:           nil,
		WorkingProductScope: []string{"/product/material/lithium-chloride"},
	}
	agent := NewCriticAgent(nil)

	prompts := []struct {
		name  string
		build func(CriticValidatorInput) string
	}{
		{"Core", agent.buildCoreValidationPrompt},
		{"SupplyChain", agent.buildSupplyChainValidationPrompt},
		{"Governance", agent.buildGovernanceValidationPrompt},
		{"Market", agent.buildMarketValidationPrompt},
		{"PolicyImpact", agent.buildPolicyImpactValidationPrompt},
	}

	for _, p := range prompts {
		t.Run(p.name, func(t *testing.T) {
			prompt := p.build(input)
			if !strings.Contains(prompt, "STRICT OUTPUT SCHEMA") {
				t.Fatalf("%s prompt must include strict output schema block", p.name)
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

func TestSupplyChainValidationPrompt_IncludesWorkingProductScopeRules(t *testing.T) {
	agent := NewCriticAgent(nil)
	prompt := agent.buildSupplyChainValidationPrompt(CriticValidatorInput{
		Fragment:            &models.HorizonFragment{CanonicalPath: "/org/us/example", Type: models.FragmentTypeOrganization},
		WorkingProductScope: []string{"/product/material/lithium-chloride"},
		SupplyChains: []SupplyChain{
			{
				ProductCandidates: []ProductCandidate{{CanonicalPath: "/product/material/lithium-hydroxide", Name: "Lithium Hydroxide", Confidence: 0.81}},
			},
		},
	})
	if !strings.Contains(prompt, "WORKING PRODUCT SCOPE") {
		t.Fatalf("prompt must include working product scope block")
	}
	if !strings.Contains(prompt, "productCandidates must be genuinely new scope additions") {
		t.Fatalf("prompt must include candidate scope rule")
	}
}

func TestPolicyImpactValidationPrompt_RequiresDirectedFiveLayerCoverage(t *testing.T) {
	agent := NewCriticAgent(nil)
	prompt := agent.buildPolicyImpactValidationPrompt(CriticValidatorInput{
		Fragment: &models.HorizonFragment{
			CanonicalPath: "/gov/ae/ministry-of-finance",
			Type:          models.FragmentTypeOrganization,
		},
	})
	for _, want := range []string{
		"GATEKEEPS_DECISION must read gatekeeper -> decision_point",
		"IN_GEO_SCOPE must read scope_owner -> geo_scope",
		"AFFECTS must read impact_source -> affected_entity",
		"geo/community/media/program transmission completeness",
		"five-layer coverage check: Policy Source, Mechanism, Scope/Exposure, Transmission/Response, Observability",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("policy-impact validation prompt missing %q", want)
		}
	}
}

func TestGovernanceValidationPrompt_RequiresBeneficialOwnershipChecks(t *testing.T) {
	agent := NewCriticAgent(nil)
	prompt := agent.buildGovernanceValidationPrompt(CriticValidatorInput{
		Fragment: &models.HorizonFragment{
			CanonicalPath: "/org/ae/difc-opco",
			Type:          models.FragmentTypeOrganization,
		},
	})
	for _, want := range []string{
		"beneficial-owner completeness",
		"voting-rights vs equity divergence",
		"community/dynasty lineage completeness",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("governance validation prompt missing %q", want)
		}
	}
}

func TestCheckSupplyChainInvariants_ValidatesScopedCandidatesAndObservations(t *testing.T) {
	gaps := checkSupplyChainInvariants([]SupplyChain{
		{
			Nodes: []SupplyChainNode{
				{CanonicalPath: "/org/kz/example", Type: "ORGANIZATION"},
				{CanonicalPath: "/product/material/lithium-chloride", Type: "PRODUCT"},
				{CanonicalPath: "/org/ru/example", Type: "ORGANIZATION"},
			},
			ProductCandidates: []ProductCandidate{
				{CanonicalPath: "/product/material/lithium-chloride", Name: "Lithium Chloride", Confidence: 0.81},
			},
			CarrierProductObservations: []CarrierProductObservation{
				{CarrierRef: "/org/kz/example", ProductRef: "/product/material/unknown-feedstock", Confidence: 0.74},
			},
			LinkObservations: []SupplyLinkObservation{
				{SupplierRef: "/org/kz/example", CustomerRef: "/org/ru/example", ProductRef: "/product/material/unknown-feedstock", Confidence: 0.74},
			},
		},
	}, []string{"/product/material/lithium-chloride"})
	if len(gaps) == 0 {
		t.Fatal("expected invariant gaps")
	}
	joined := strings.Join(gaps, "\n")
	if !strings.Contains(joined, "productCandidate duplicates") {
		t.Fatalf("gaps must mention duplicate productCandidate, got %v", gaps)
	}
	if !strings.Contains(joined, "carrierProductObservation productRef") {
		t.Fatalf("gaps must mention carrierProductObservation scope mismatch, got %v", gaps)
	}
	if !strings.Contains(joined, "linkObservation productRef") {
		t.Fatalf("gaps must mention linkObservation scope mismatch, got %v", gaps)
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

func TestCriticPipeline_Validate_policyImpactMode(t *testing.T) {
	runner := (&CriticAgent{}).CriticPipeline()
	modes := []string{taxonomy.DomainModePolicyImpact}
	validators := runner.validatorsForModes(modes)
	if len(validators) != 2 {
		t.Errorf("expected 2 validators (Core, PolicyImpact), got %d", len(validators))
	}
	modesSeen := make(map[string]bool)
	for _, v := range validators {
		modesSeen[v.DomainMode()] = true
	}
	for _, want := range []string{"core", taxonomy.DomainModePolicyImpact} {
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

func TestRuleBasedQuickValidation_MarketWithEmptyRelations(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeMarket,
		CanonicalPath: "/market/global/public-safety",
	}
	relations := []DiscoveredRelation{}
	domainModes := []string{taxonomy.DomainModeMarket}

	result := RuleBasedQuickValidation(frag, relations, nil, domainModes)
	if result == nil {
		t.Fatal("expected non-nil ValidationResult")
	}
	if len(result.MissingInfo) == 0 {
		t.Fatal("Market with empty relations should produce missingInfo (HAS_MARKET_SEGMENT, PARTICIPATES_IN, etc.)")
	}
	// Should include market-specific questions
	hasMarket := false
	for _, q := range result.MissingInfo {
		if strings.Contains(strings.ToLower(q), "market") || strings.Contains(strings.ToLower(q), "segment") ||
			strings.Contains(strings.ToLower(q), "participant") || strings.Contains(strings.ToLower(q), "tam") ||
			strings.Contains(strings.ToLower(q), "cagr") {
			hasMarket = true
			break
		}
	}
	if !hasMarket {
		t.Errorf("expected market-specific questions, got %v", result.MissingInfo)
	}
}

func TestRuleBasedQuickValidation_SupplyChainModeRequestsProduceConsume(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/us/acme",
	}
	relations := []DiscoveredRelation{}
	domainModes := []string{taxonomy.DomainModeSupplyChain}

	result := RuleBasedQuickValidation(frag, relations, nil, domainModes)
	if result == nil {
		t.Fatal("expected non-nil ValidationResult")
	}
	hasProduceConsume := false
	for _, q := range result.MissingInfo {
		if strings.Contains(strings.ToLower(q), "produc") || strings.Contains(strings.ToLower(q), "consum") {
			hasProduceConsume = true
			break
		}
	}
	if !hasProduceConsume {
		t.Errorf("supply_chain mode with no relations should request products produced/consumed, got %v", result.MissingInfo)
	}
}

func TestParseValidationResultMap_NormalizesObservedModelDrift(t *testing.T) {
	payload := map[string]interface{}{
		"hasConflicts": false,
		"isValid":      true,
		"confidence":   "0.72",
		"missingInfo": []interface{}{
			"What controls access?",
			map[string]interface{}{"question": "Who gatekeeps this decision?"},
			map[string]interface{}{"text": "What mechanism applies?"},
		},
	}

	res, err := parseValidationResultMap(payload)
	if err != nil {
		t.Fatalf("parseValidationResultMap: %v", err)
	}
	if res.Confidence != 0.72 {
		t.Fatalf("expected confidence 0.72, got %f", res.Confidence)
	}
	if len(res.MissingInfo) != 3 {
		t.Fatalf("expected 3 missingInfo items, got %d", len(res.MissingInfo))
	}
}
