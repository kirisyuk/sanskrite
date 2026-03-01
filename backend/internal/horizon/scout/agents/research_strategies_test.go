package agents

import (
	"context"
	"encoding/json"
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
		!strings.Contains(prompt, "SUBSIDIARY_OF") || !strings.Contains(prompt, "ASSURES") {
		t.Error("Governance prompt should list canonical types: OWNS, BOARD_MEMBER_OF, SUBSIDIARY_OF, ASSURES")
	}
	if !strings.Contains(strings.ToUpper(prompt), "USE ONLY") {
		t.Error("Governance prompt should restrict to canonical types only (Use ONLY ...)")
	}
}

func TestGovernanceEnrichmentPrompt_OwnsDirectionGuardrails(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example"},
		MissingInfo: []string{"Who owns this org?"},
	}
	prompt := a.buildGovernanceEnrichmentPrompt(input)
	for _, want := range []string{
		"For OWNS, direction is ALWAYS owner -> owned entity.",
		"NEVER output reversed OWNS (CURRENT FRAGMENT -> its owner).",
		"For INVESTS_IN, direction is ALWAYS investor -> investee.",
		"qualifiers.source_role and qualifiers.target_role",
		"\"sourceCanonicalPath?\", \"sourceType?\", \"sourceName?\"",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("governance prompt missing OWNS direction guardrail %q", want)
		}
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

	frag, relations, chains, _, _, err := runner.Enrich(ctx, input, modes)
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
	if len(general) != 1 {
		t.Errorf("general mode should return exactly one strategy, got %d", len(general))
	}

	mixed := runner.strategiesForModes([]string{taxonomy.DomainModeGeneral, taxonomy.DomainModeSupplyChain})
	if len(mixed) != 1 {
		t.Errorf("general+supply_chain should keep supply_chain strategy, got %d", len(mixed))
	}

	policy := runner.strategiesForModes([]string{taxonomy.DomainModePolicyImpact})
	if len(policy) != 1 {
		t.Errorf("policy_impact should enable exactly one strategy, got %d", len(policy))
	}

	empty := runner.strategiesForModes(nil)
	if len(empty) != 1 {
		t.Errorf("empty mode should route to exactly one general strategy, got %d", len(empty))
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

func TestGeneralEnrichmentPrompt_ModularContract(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment: &models.HorizonFragment{
			CanonicalPath: "/fac/global/red-sea-corridor",
			Type:          models.FragmentTypeFacility,
			Qualifiers:    map[string]interface{}{"route": true},
		},
		MissingInfo:      []string{"Find key logistics operators"},
		ExistingEntities: []string{"/org/us/example-logistics-inc"},
		ChainContext:     "Known route: Suez -> Jeddah -> Dammam",
	}
	prompt := a.buildGeneralEnrichmentPrompt(input)
	for _, want := range []string{
		"OBJECT-TYPE TASK SPECIALIZATION",
		"OBJECT QUALIFIER CONTRACT (STRICT)",
		"EXISTING ENTITIES (reuse instead of creating duplicates):",
		"DIRECTION ROLE CONTRACT (mandatory when applicable):",
		"geo=sea",
		"ALLOWED RELATION TAXONOMY:",
		"CANONICAL PATH RULES:",
		"SUPPLY CHAIN CONTEXT:",
		"Product Canonicalization:",
		"Name History (ORG/PERSON",
		"/segment/{domain}/{vertical}/{geo}",
		"/media/{platform}/{slug}",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("general enrichment prompt missing %q", want)
		}
	}
}

func TestGeneralEnrichmentPrompt_OwnsDirectionGuardrails(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example", Type: models.FragmentTypeOrganization},
		MissingInfo: []string{"Who owns this org?"},
	}
	prompt := a.buildGeneralEnrichmentPrompt(input)
	for _, want := range []string{
		"THIS FRAGMENT is owned by X",
		"THIS FRAGMENT is invested in by X",
		"Never emit reversed OWNS (THIS FRAGMENT -> its owner).",
		"use explicit source whenever THIS FRAGMENT is the semantic target",
		"qualifiers.source_role and qualifiers.target_role",
		"dependent->dependency, supplier->customer",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("general prompt missing OWNS direction guardrail %q", want)
		}
	}
}

func TestGeneralEnrichmentPrompt_DomainOverride(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment: &models.HorizonFragment{
			CanonicalPath: "/domain/v1/supply-chain/geo/sa",
			Type:          models.FragmentTypeDomain,
		},
		MissingInfo: []string{"Expand first-order anchors"},
	}
	prompt := a.buildGeneralEnrichmentPrompt(input)
	for _, want := range []string{
		"DOMAIN EXPANSION GUIDANCE (CRITICAL OVERRIDE)",
		"Use ONLY domain relation types: " + domainScaffoldRelationTypesPromptString() + ".",
		"ALLOWED RELATION TAXONOMY (DOMAIN OVERRIDE):",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("domain general prompt missing %q", want)
		}
	}
}

func TestModePrompts_UseUSOTRelationAllowlists(t *testing.T) {
	a := NewResearchAgent(nil)
	baseInput := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/org/us/example", Type: models.FragmentTypeOrganization},
		MissingInfo: []string{"collect key relations"},
	}

	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{
			name:   "supply_chain_enrichment",
			prompt: a.buildSupplyChainEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeSupplyChain),
		},
		{
			name:   "governance_enrichment",
			prompt: a.buildGovernanceEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance),
		},
		{
			name:   "assurance_enrichment",
			prompt: a.buildAssuranceEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeAssurance),
		},
		{
			name:   "ecosystem_enrichment",
			prompt: a.buildEcosystemEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem),
		},
		{
			name:   "market_enrichment",
			prompt: a.buildMarketEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeMarket),
		},
		{
			name:   "strategic_enrichment",
			prompt: a.buildStrategicEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeStrategic),
		},
		{
			name:   "policy_impact_enrichment",
			prompt: a.buildPolicyImpactEnrichmentPrompt(baseInput),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
		},
		{
			name:   "supply_chain_full",
			prompt: a.buildSupplyChainFullQueryPrompt(SupplyChainQuery{Anchor: "/org/us/example", AnchorType: "org", Direction: "both", Depth: 3}),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeSupplyChain),
		},
		{
			name:   "governance_full",
			prompt: a.buildGovernanceFullPrompt(GovernanceQuery{Target: "/org/us/example", TargetType: "organization", Direction: "upstream", Depth: 3}),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance),
		},
		{
			name:   "ecosystem_full",
			prompt: a.buildEcosystemFullPrompt(EcosystemQuery{Focus: "/org/us/example", FocusType: "organization", Depth: 3}),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem),
		},
		{
			name:   "policy_impact_full",
			prompt: a.buildRegulatoryImpactFullPrompt(RegulatoryImpactQuery{Instrument: "VAT", Jurisdiction: "sa", Depth: 3}),
			want:   relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.prompt, tc.want) {
				t.Fatalf("prompt %s missing USOT relation list %q", tc.name, tc.want)
			}
		})
	}
}

func TestSupplyChainEnrichmentPrompt_StrictSupplyChainsContractBlock(t *testing.T) {
	a := NewResearchAgent(nil)
	input := ResearchStrategyInput{
		Fragment:    &models.HorizonFragment{CanonicalPath: "/fac/kazakhstan/aktau-sea-port", Type: models.FragmentTypeFacility},
		MissingInfo: []string{"Find route links"},
	}
	prompt := a.buildSupplyChainEnrichmentPrompt(input)
	for _, want := range []string{
		"SUPPLY CHAINS JSON CONTRACT (STRICT):",
		"\"nodes\": REQUIRED array of objects (NOT string and NOT shorthand)",
		"PROGRAM|MARKET",
		"INVALID shape example: \"nodes\": \"A -> B -> C\"",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("supply chain prompt missing %q", want)
		}
	}
}

func TestSanitizeResearchResultForUnmarshal_StructuralDrift(t *testing.T) {
	raw := []byte(`{
  "fragment": {"state": "not-a-map"},
  "relations": [
    {
      "targetName": "EU logistics",
      "targetType": "MARKET",
      "targetCanonicalPath": "/market/eu/all/logistics",
      "relationType": "DEPENDS_ON",
      "confidence": "0.77",
      "reason": 123,
      "metrics": 42,
      "qualifiers": "bad-shape"
    },
    "bad-relation"
  ],
  "supplyChains": [
    {
      "nodes": "/fac/kazakhstan/aktau-sea-port -> /program/int/titr-middle-corridor -> /market/eu/all/logistics",
      "confidence": "0.9",
      "reason": 777,
      "transportVia": "/fac/kazakhstan/aktau-sea-port"
    }
  ]
}`)

	sanitized, err := sanitizeResearchResultForUnmarshal(raw)
	if err != nil {
		t.Fatalf("sanitizeResearchResultForUnmarshal() error = %v", err)
	}

	var rr researchResult
	if err := json.Unmarshal(sanitized, &rr); err != nil {
		t.Fatalf("sanitized result should unmarshal: %v\npayload=%s", err, string(sanitized))
	}
	if len(rr.SupplyChains) != 1 {
		t.Fatalf("supplyChains=%d, want 1", len(rr.SupplyChains))
	}
	if got := len(rr.SupplyChains[0].Nodes); got != 3 {
		t.Fatalf("supplyChains[0].nodes=%d, want 3", got)
	}
	if rr.SupplyChains[0].Nodes[1].Type != "PROGRAM" {
		t.Fatalf("node[1].type=%q, want PROGRAM", rr.SupplyChains[0].Nodes[1].Type)
	}
	if rr.SupplyChains[0].Nodes[2].Type != "MARKET" {
		t.Fatalf("node[2].type=%q, want MARKET", rr.SupplyChains[0].Nodes[2].Type)
	}
	if len(rr.Relations) != 1 {
		t.Fatalf("relations=%d, want 1", len(rr.Relations))
	}
	if len(rr.Relations[0].Metrics) != 0 {
		t.Fatalf("metrics should be dropped for invalid shape, got=%v", rr.Relations[0].Metrics)
	}
}
