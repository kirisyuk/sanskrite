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
	// Operational types only (fractal model): PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM
	if !strings.Contains(prompt, "PRODUCES") || !strings.Contains(prompt, "SUPPLIES_TO") ||
		!strings.Contains(prompt, "TRANSFORMS_INTO") || !strings.Contains(prompt, "EXTRACTS_FROM") {
		t.Error("SupplyChain prompt should list operational types: PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM")
	}
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

	_, _, _, _, _, err := runner.Enrich(ctx, input, modes)
	// With nil executor, supply-chain strategy returns "not configured"; pipeline propagates the error
	if err == nil {
		t.Fatal("expected error when supply-chain strategy has nil executor")
	}
	if !strings.Contains(err.Error(), "supply-chain") && !strings.Contains(err.Error(), "supply_chain") {
		t.Errorf("expected supply-chain config error, got: %v", err)
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
			want:   taxonomy.SupplyChainEdgeTypesString(),
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
			want:   taxonomy.SupplyChainEdgeTypesString(),
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
		Fragment:    &models.HorizonFragment{CanonicalPath: "/fac/kz/aktau-sea-port", Type: models.FragmentTypeFacility},
		MissingInfo: []string{"Find route links"},
	}
	prompt := a.buildSupplyChainEnrichmentPrompt(input)
	for _, want := range []string{
		"SUPPLY CHAINS JSON CONTRACT (STRICT):",
		"\"nodes\": REQUIRED array of objects (NOT string and NOT shorthand)",
		"ORGANIZATION|FACILITY|PRODUCT|DEPOSIT",
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
      "nodes": "/fac/kz/aktau-sea-port -> /program/int/titr-middle-corridor -> /market/eu/all/logistics",
      "confidence": "0.9",
      "reason": 777,
      "transportVia": "/fac/kz/aktau-sea-port"
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

func TestInferCanonicalPathFromNameAndType(t *testing.T) {
	tests := []struct {
		name     string
		nodeType string
		want     string
	}{
		{"Forvia", "ORGANIZATION", "/org/discovered/forvia"},
		{"Steel Coil", "PRODUCT", "/product/discovered/steel-coil"},
		{"Shanghai Port", "FACILITY", "/fac/discovered/shanghai-port"},
		{"EU Automotive", "MARKET", "/market/discovered/eu-automotive"},
		{"East Asia", "GEO_REGION", "/loc/discovered/east-asia"},
		{"Iron Ore Mine", "DEPOSIT", "/deposit/discovered/iron-ore-mine"},
		{"", "ORGANIZATION", ""},
		{"   ", "ORGANIZATION", ""},
	}
	for _, tt := range tests {
		got := inferCanonicalPathFromNameAndType(tt.name, tt.nodeType)
		if got != tt.want {
			t.Errorf("inferCanonicalPathFromNameAndType(%q, %q) = %q, want %q", tt.name, tt.nodeType, got, tt.want)
		}
	}
}

func TestSanitizeNode_GeneratesCanonicalPath(t *testing.T) {
	node := sanitizeSupplyChainNode(map[string]interface{}{
		"name": "ArcelorMittal",
		"type": "ORGANIZATION",
	})
	if node == nil {
		t.Fatal("node should not be nil")
	}
	cp, _ := node["canonicalPath"].(string)
	if cp == "" {
		t.Fatal("canonicalPath should be auto-generated")
	}
	// ORGANIZATION stays as /org/ in supply chain
	if cp != "/org/discovered/arcelormittal" {
		t.Errorf("canonicalPath = %q, want /org/discovered/arcelormittal", cp)
	}
}

func TestSanitizeNode_PreservesExistingCanonicalPath(t *testing.T) {
	node := sanitizeSupplyChainNode(map[string]interface{}{
		"canonicalPath": "/org/lu/arcelormittal",
		"name":          "ArcelorMittal",
		"type":          "ORGANIZATION",
	})
	if node == nil {
		t.Fatal("node should not be nil")
	}
	cp := node["canonicalPath"].(string)
	if cp != "/org/lu/arcelormittal" {
		t.Errorf("canonicalPath = %q, want /org/lu/arcelormittal (should not override)", cp)
	}
}

func TestSanitizeNodeString_GeneratesCanonicalPath(t *testing.T) {
	node := sanitizeSupplyChainNodeString("Nippon Steel Corporation")
	if node == nil {
		t.Fatal("node should not be nil")
	}
	cp := node["canonicalPath"].(string)
	if cp == "" {
		t.Fatal("canonicalPath should be auto-generated for string node")
	}
	// ORGANIZATION stays as /org/ in supply chain
	if !strings.HasPrefix(cp, "/org/") {
		t.Errorf("canonicalPath = %q, should start with /org/", cp)
	}
}

func TestRepairSupplyChainNodePaths(t *testing.T) {
	chains := []SupplyChain{{
		Nodes: []SupplyChainNode{
			{Name: "Steel Mill", Type: "FACILITY"},
			{CanonicalPath: "/org/de/forvia", Name: "Forvia", Type: "ORGANIZATION"},
			{Name: "Volkswagen", Type: "ORGANIZATION"},
		},
	}}
	repairSupplyChainNodePaths(chains)
	if chains[0].Nodes[0].CanonicalPath == "" {
		t.Error("node[0] should have generated canonical path")
	}
	if chains[0].Nodes[1].CanonicalPath != "/org/de/forvia" {
		t.Error("node[1] existing path should be preserved")
	}
	if chains[0].Nodes[2].CanonicalPath == "" {
		t.Error("node[2] should have generated canonical path")
	}
}

func TestExtractNodesMultiKey(t *testing.T) {
	tests := []struct {
		name     string
		chain    map[string]interface{}
		wantN    int
		wantName string
	}{
		{
			name: "standard nodes key",
			chain: map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"name": "Forvia", "type": "ORGANIZATION"},
				},
			},
			wantN: 1, wantName: "Forvia",
		},
		{
			name: "participants key",
			chain: map[string]interface{}{
				"participants": []interface{}{
					map[string]interface{}{"name": "ThyssenKrupp", "type": "ORGANIZATION"},
					map[string]interface{}{"name": "Steel Coil", "type": "PRODUCT"},
				},
			},
			wantN: 2, wantName: "ThyssenKrupp",
		},
		{
			name: "steps key",
			chain: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "ArcelorMittal", "type": "ORGANIZATION"},
				},
			},
			wantN: 1, wantName: "ArcelorMittal",
		},
		{
			name: "chain key",
			chain: map[string]interface{}{
				"chain": []interface{}{
					map[string]interface{}{"name": "BASF", "type": "ORGANIZATION"},
				},
			},
			wantN: 1, wantName: "BASF",
		},
		{
			name: "stages key (observed in prod)",
			chain: map[string]interface{}{
				"stages": []interface{}{
					map[string]interface{}{"name": "Supplier A", "type": "ORGANIZATION"},
					map[string]interface{}{"name": "Steel Coil", "type": "PRODUCT"},
					map[string]interface{}{"name": "OEM", "type": "ORGANIZATION"},
				},
			},
			wantN: 3, wantName: "Supplier A",
		},
		{
			name:  "no matching key",
			chain: map[string]interface{}{"flow": "A → B → C"},
			wantN: 0,
		},
		{
			name:  "empty nodes",
			chain: map[string]interface{}{"nodes": []interface{}{}},
			wantN: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNodesMultiKey(tt.chain)
			if len(got) != tt.wantN {
				t.Errorf("extractNodesMultiKey() got %d nodes, want %d", len(got), tt.wantN)
			}
			if tt.wantN > 0 && tt.wantName != "" {
				if m, ok := got[0].(map[string]interface{}); ok {
					if m["name"] != tt.wantName {
						t.Errorf("first node name = %v, want %v", m["name"], tt.wantName)
					}
				}
			}
		})
	}
}

func TestAlwaysSanitizeSupplyChains(t *testing.T) {
	// Simulate LLM output where supply chains use "participants" instead of "nodes"
	// and "reliability" instead of "confidence". Direct unmarshal would produce
	// zero-value SupplyChain structs.
	rawJSON := `{
		"fragment": {"description": "Test entity"},
		"relations": [],
		"supplyChains": [
			{
				"participants": [
					{"name": "ArcelorMittal", "type": "ORGANIZATION"},
					{"name": "Hot-rolled Steel", "type": "PRODUCT"},
					{"name": "Forvia", "type": "ORGANIZATION"}
				],
				"reliability": 0.85,
				"explanation": "Steel supply chain"
			}
		]
	}`

	// Direct unmarshal should produce zero-value chains
	var directRes researchResult
	if err := json.Unmarshal([]byte(rawJSON), &directRes); err != nil {
		t.Fatalf("direct unmarshal should succeed: %v", err)
	}
	if len(directRes.SupplyChains) != 1 {
		t.Fatalf("direct unmarshal should find 1 chain, got %d", len(directRes.SupplyChains))
	}
	if len(directRes.SupplyChains[0].Nodes) != 0 {
		t.Errorf("direct unmarshal should have 0 nodes (field name mismatch), got %d",
			len(directRes.SupplyChains[0].Nodes))
	}

	// Sanitize-first path should recover the nodes
	sanitized, err := sanitizeResearchResultForUnmarshal([]byte(rawJSON))
	if err != nil {
		t.Fatalf("sanitize failed: %v", err)
	}
	var sanitizedRes researchResult
	if err := json.Unmarshal(sanitized, &sanitizedRes); err != nil {
		t.Fatalf("unmarshal after sanitize failed: %v", err)
	}
	if len(sanitizedRes.SupplyChains) != 1 {
		t.Fatalf("sanitized should find 1 chain, got %d", len(sanitizedRes.SupplyChains))
	}
	if len(sanitizedRes.SupplyChains[0].Nodes) != 3 {
		t.Errorf("sanitized should recover 3 nodes, got %d", len(sanitizedRes.SupplyChains[0].Nodes))
	}
	if sanitizedRes.SupplyChains[0].Confidence < 0.8 {
		t.Errorf("sanitized confidence should be ~0.85, got %.2f", sanitizedRes.SupplyChains[0].Confidence)
	}
}
