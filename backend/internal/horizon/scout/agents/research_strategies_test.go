package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Research prompts must list only canonical types as allowed; they may mention deprecated types in "Do NOT use" clauses.
func TestSupplyChainEnrichmentPrompt_OnlyCanonicalTypes(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example"},
		MissingInfo: []string{"Find suppliers"},
	}
	prompt := a.buildSupplyChainEnrichmentPrompt(input)
	if !strings.Contains(prompt, "PRODUCES") || !strings.Contains(prompt, "CONSUMES") ||
		!strings.Contains(prompt, "SUPPLIES_TO") || !strings.Contains(prompt, "EXTRACTS_FROM") {
		t.Error("SupplyChain prompt should list canonical types: PRODUCES, CONSUMES, SUPPLIES_TO, EXTRACTS_FROM")
	}
	// Must not allow deprecated types as valid outputs (e.g. "use ... RELATED_TO" as allowed)
	if !strings.Contains(strings.ToUpper(prompt), "USE ONLY") {
		t.Error("SupplyChain prompt should restrict to canonical types only (Use ONLY ...)")
	}
}

func TestGovernanceEnrichmentPrompt_OnlyCanonicalTypes(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example"},
		MissingInfo: []string{"Find owners"},
	}
	prompt := a.buildGovernanceEnrichmentPrompt(input)
	if !strings.Contains(prompt, "OWNS") || !strings.Contains(prompt, "BOARD_MEMBER_OF") ||
		!strings.Contains(prompt, "SUBSIDIARY_OF") {
		t.Error("Governance prompt should list canonical types: OWNS, BOARD_MEMBER_OF, SUBSIDIARY_OF")
	}
	if !strings.Contains(strings.ToUpper(prompt), "USE ONLY") {
		t.Error("Governance prompt should restrict to canonical types only (Use ONLY ...)")
	}
}

func TestResearchPipeline_SupplyChainMode(t *testing.T) {
	runner := NewResearchAgent(nil).ResearchPipeline()
	ctx := context.Background()
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example"},
		MissingInfo: []string{"Find suppliers"},
	}
	modes := []string{taxonomy.DomainModeSupplyChain}

	frag, relations, chains, _, err := runner.Enrich(ctx, input, modes)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if frag == nil {
		t.Fatal("expected non-nil fragment")
	}
	if len(relations) != 0 || len(chains) != 0 {
		// With nil executor, strategies return nil output; pipeline still returns fragment with empty lists
		t.Logf("relations=%d chains=%d (nil executor yields no results)", len(relations), len(chains))
	}
}

func TestResearchPipeline_StrategiesForModes(t *testing.T) {
	runner := NewResearchAgent(nil).ResearchPipeline()
	modes := []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance}
	strategies := runner.strategiesForModes(modes)
	if len(strategies) != 2 {
		t.Errorf("expected 2 strategies for supply_chain+governance, got %d", len(strategies))
	}
	general := runner.strategiesForModes([]string{taxonomy.DomainModeGeneral})
	if general != nil {
		t.Errorf("general mode should return nil strategies, got %d", len(general))
	}

	mixed := runner.strategiesForModes([]string{taxonomy.DomainModeGeneral, taxonomy.DomainModeSupplyChain})
	if len(mixed) != 1 {
		t.Errorf("general+supply_chain should keep supply_chain strategy, got %d", len(mixed))
	}

	policy := runner.strategiesForModes([]string{taxonomy.DomainModePolicyImpact})
	if len(policy) != 1 {
		t.Errorf("policy_impact should enable exactly one strategy, got %d", len(policy))
	}
}

func TestSupplyChainEnrichmentPrompt_PhaseGuardrails(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:          &models.HorizonFragment{CanonicalPath: "/org/us/example"},
		MissingInfo:       []string{"Find suppliers"},
		UnitPipelinePhase: models.UnitPipelinePhaseP3,
	}
	prompt := a.buildSupplyChainEnrichmentPrompt(input)
	if !strings.Contains(prompt, "UNIT PIPELINE PHASE: P0-P3") {
		t.Fatalf("expected P0-P3 guardrails in prompt")
	}

	input.UnitPipelinePhase = models.UnitPipelinePhaseP5
	prompt = a.buildSupplyChainEnrichmentPrompt(input)
	if !strings.Contains(prompt, "UNIT PIPELINE PHASE: P5") {
		t.Fatalf("expected P5 guardrails in prompt")
	}
}

func TestMarketEnrichmentPrompt_DirectionGuardrails(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/market/kz/agriculture/credit"},
		MissingInfo: []string{"Find top players"},
	}
	prompt := a.buildMarketEnrichmentPrompt(input)
	if !strings.Contains(prompt, "PARTICIPATES_IN direction is participant -> market.") {
		t.Fatalf("expected strict PARTICIPATES_IN direction guardrail in market prompt")
	}
	if !strings.Contains(prompt, "targetCanonicalPath MUST NOT start with /domain/.") {
		t.Fatalf("expected non-domain target guardrail in market prompt")
	}
	if !strings.Contains(prompt, "sourceCanonicalPath/sourceType/sourceName = player") {
		t.Fatalf("expected explicit-source instruction for market player relations")
	}
}

func TestPolicyImpactEnrichmentPrompt_DomainTargetGuardrails(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/gov/kz/ministry-of-agriculture"},
		MissingInfo: []string{"Map policy channels"},
	}
	prompt := a.buildPolicyImpactEnrichmentPrompt(input)
	if !strings.Contains(prompt, "targetCanonicalPath MUST NOT start with /domain/") {
		t.Fatalf("expected domain-as-config guardrail in policy impact prompt")
	}
	if !strings.Contains(prompt, "targetCanonicalPath MUST NOT start with /domain/.") {
		t.Fatalf("expected non-domain target guardrail in policy impact prompt")
	}
	if !strings.Contains(prompt, "PARTICIPATES_IN direction is participant -> market.") {
		t.Fatalf("expected strict PARTICIPATES_IN direction guardrail in policy impact prompt")
	}
}
