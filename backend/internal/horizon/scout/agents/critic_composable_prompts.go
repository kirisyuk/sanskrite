package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const criticValidationOutputSchema = `STRICT OUTPUT SCHEMA (types are mandatory):
{
  "hasConflicts": boolean,
  "conflicts": [
    {
      "field": "string",
      "extractedValue": any,
      "sourceValue": any,
      "reason": "string",
      "severity": "low" | "medium" | "high"
    }
  ],
  "confidence": number,           // float 0..1, MUST be numeric (not string)
  "isValid": boolean,
  "missingInfo": ["string"]       // array of strings only
}
Rules:
- missingInfo items MUST be plain strings, not objects.
- Return exactly one JSON object and no extra prose.`

func (a *CriticAgent) runValidationPrompt(ctx context.Context, prompt string) (*ValidationResult, error) {
	if a == nil || a.executor == nil {
		return nil, nil
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         12000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}
	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate data: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	validationResult, err := parseValidationResultJSON(jsonData)
	if err != nil {
		return nil, err
	}
	return validationResult, nil
}

func (a *CriticAgent) buildCoreValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for core entity integrity.

%s
EXTRACTED FRAGMENT:
%s

EXTRACTED RELATIONS:
%s

Validate:
1. canonicalPath correctness and basic type-path consistency;
2. obvious conflicts between fragment fields and source context;
3. structural completeness at broad level only;
4. missing evidence questions needed for next non-deep pass.

Return JSON:
{
  "hasConflicts": boolean,
  "conflicts": [{"field":"string","extractedValue":"any","sourceValue":"any","reason":"string","severity":"low|medium|high"}],
  "confidence": 0.0,
  "isValid": boolean,
  "missingInfo": ["string"]
}

%s

Return ONLY valid JSON.`, graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildSupplyChainValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	chainsJSON, _ := json.MarshalIndent(input.SupplyChains, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for SUPPLY CHAIN mode.

Allowed relation types: DEPENDS_ON, PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN.
Allowed actor types: %s.
Canonical path rules:
%s

%s
FRAGMENT:
%s

RELATIONS:
%s

SUPPLY CHAINS:
%s

Validate:
1. chain continuity and direction consistency;
2. relation taxonomy compliance (type-direction-entity compatibility) and canonical path shape;
3. DEPENDS_ON vs SUPPLIES_TO semantics:
   - DEPENDS_ON is consumer-side dependency;
   - SUPPLIES_TO is supplier-side flow;
   - avoid contradictory duplicates unless source evidence explicitly supports both directions;
4. transmission/logistics qualifiers quality for supply dependencies when present (facet, throughput_share, capacity_constraint, reroute_* best-effort);
5. supplyChains node alternation actor→product and facility transformation hops;
6. upstream/downstream coverage at broad or semi-depth level;
7. qualifier quality (confidence, source hints, transport qualifiers when applicable).

%s

Return ONLY valid JSON.`, taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), string(chainsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildGovernanceValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	return fmt.Sprintf(`You are a Critic validator for GOVERNANCE mode.

Allowed relation types: %s.
Allowed actor types: %s.

FRAGMENT:
%s

RELATIONS:
%s

	Validate ownership and control coherence:
1. parent/subsidiary direction consistency;
2. board and executive coverage at practical broad level;
3. government hierarchy consistency when /gov/ entities appear;
4. assurance linkage quality when auditors/certifiers are present (ASSURES + assurance_type);
5. missing information that should be researched before deep dive.

%s

Return ONLY valid JSON.`, taxonomy.GovernanceEdgeTypesString(), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeGovernance), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildAssuranceValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	return fmt.Sprintf(`You are a Critic validator for ASSURANCE mode (Third Line: audit, certification, regulatory oversight).

Allowed relation types: ASSURES (with qualifiers.assurance_type: audit|certification|regulatory|inspection), REGULATES.
Allowed actor types: %s.

FRAGMENT:
%s

RELATIONS:
%s

Validate:
1. ASSURES direction: assurer (source) → assuree (target). Do not use AUDITED_BY legacy inverse.
2. external audit coverage for significant orgs/facilities;
3. certification and regulatory oversight when relevant to sector;
4. missing assurance information that should be researched.

%s

Return ONLY valid JSON.`, taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeAssurance), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildMarketValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for MARKET mode.

Allowed relation types: PARTICIPATES_IN, HAS_MARKET_SEGMENT. GOV entities (/gov/...) may also use REGULATES, CONTROLS_ACCESS, INFLUENCES toward MARKET targets.
Allowed actor types: %s.

%sFRAGMENT:
%s

RELATIONS:
%s

Validate:
1. market hierarchy placement and immediate parent/children coverage;
2. major participant coverage and market share plausibility when available;
3. required market metrics presence at broad level (tam, cagr, concentration).

%s

Return ONLY valid JSON.`, taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeMarket), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

// buildComposedValidationPrompt merges Core + all non-strategic domain validators into a single
// LLM prompt. This eliminates N sequential LLM calls (one per validator) and replaces them with
// a single call that covers all active domain modes. Strategic mode is excluded because it uses
// a structurally different prompt (ValidateStrategicOverlay).
func (a *CriticAgent) buildComposedValidationPrompt(input CriticValidatorInput, domainModes []string) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")

	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}

	var sections []string

	// Core validation (always included)
	sections = append(sections, `== CORE VALIDATION ==
Validate:
1. canonicalPath correctness and basic type-path consistency;
2. obvious conflicts between fragment fields and source context;
3. structural completeness at broad level only;
4. missing evidence questions needed for next non-deep pass.`)

	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		switch m {
		case taxonomy.DomainModeSupplyChain:
			chainsJSON, _ := json.MarshalIndent(input.SupplyChains, "", "  ")
			sections = append(sections, fmt.Sprintf(`== SUPPLY CHAIN VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.
Canonical path rules:
%s

SUPPLY CHAINS:
%s

Validate:
1. chain continuity and direction consistency;
2. relation taxonomy compliance (type-direction-entity compatibility) and canonical path shape;
3. DEPENDS_ON vs SUPPLIES_TO semantics;
4. transmission/logistics qualifiers quality when present;
5. supplyChains node alternation actor→product and facility transformation hops;
6. upstream/downstream coverage at broad or semi-depth level;
7. qualifier quality (confidence, source hints, transport qualifiers when applicable).`,
				taxonomy.SupplyChainEdgeTypesString(),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeSupplyChain),
				taxonomy.ExtendedCanonicalPathRulesPrompt(),
				string(chainsJSON)))

		case taxonomy.DomainModeGovernance:
			sections = append(sections, fmt.Sprintf(`== GOVERNANCE VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.

Validate ownership and control coherence:
1. parent/subsidiary direction consistency;
2. board and executive coverage at practical broad level;
3. government hierarchy consistency when /gov/ entities appear;
4. assurance linkage quality when auditors/certifiers are present;
5. missing information that should be researched before deep dive.`,
				taxonomy.GovernanceEdgeTypesString(),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeGovernance)))

		case taxonomy.DomainModeAssurance:
			sections = append(sections, fmt.Sprintf(`== ASSURANCE VALIDATION ==
Allowed relation types: ASSURES (with qualifiers.assurance_type: audit|certification|regulatory|inspection), REGULATES.
Allowed actor types: %s.

Validate:
1. ASSURES direction: assurer (source) → assuree (target);
2. external audit coverage for significant orgs/facilities;
3. certification and regulatory oversight when relevant to sector;
4. missing assurance information that should be researched.`,
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeAssurance)))

		case taxonomy.DomainModeMarket:
			sections = append(sections, fmt.Sprintf(`== MARKET VALIDATION ==
Allowed relation types: PARTICIPATES_IN, HAS_MARKET_SEGMENT. GOV entities (/gov/...) may also use REGULATES, CONTROLS_ACCESS, INFLUENCES toward MARKET targets.
Allowed actor types: %s.

Validate:
1. market hierarchy placement and immediate parent/children coverage;
2. major participant coverage and market share plausibility when available;
3. required market metrics presence at broad level (tam, cagr, concentration).`,
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeMarket)))

		case taxonomy.DomainModePolicyImpact:
			sections = append(sections, fmt.Sprintf(`== POLICY IMPACT VALIDATION ==
Allowed relation types: REGULATES, CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO, INFLUENCES, DEPENDS_ON, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN, ADMINISTERS.
Allowed actor types: %s.
Canonical path rules:
%s

Validate:
1. policy channel consistency: regulator/program/mechanism links are directionally coherent;
2. impact graph quality: affected segments are explicit and connected;
3. relation taxonomy compliance;
4. scenario modeling readiness (quantitative qualifiers on edges);
5. observability completeness;
6. five-layer coverage check: Policy Source, Transmission Channel, Exposure, Response, Observability.`,
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModePolicyImpact),
				taxonomy.ExtendedCanonicalPathRulesPrompt()))
		}
	}

	return fmt.Sprintf(`You are a Critic validator. Validate the entity across multiple dimensions in a single pass.

%s%s

EXTRACTED FRAGMENT:
%s

EXTRACTED RELATIONS:
%s

%s

Return ONLY valid JSON.`, graphBlock, strings.Join(sections, "\n\n"), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildPolicyImpactValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for POLICY IMPACT mode.

Allowed relation types: REGULATES, CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO, INFLUENCES, DEPENDS_ON, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN, ADMINISTERS.
Allowed actor types: %s.
Canonical path rules:
%s

%sFRAGMENT:
%s

RELATIONS:
%s

Validate:
1. policy channel consistency: regulator/program/mechanism links are directionally coherent;
2. impact graph quality: affected segments are explicit and connected through interpretable channels;
3. relation taxonomy compliance (type-direction-entity compatibility);
4. SCENARIO MODELING READINESS (critical for policy-impact):
   a. do REGULATES/IMPOSES_CONDITION edges carry quantitative qualifiers (rate_from, rate_to, effective_date)?
   b. do exposure edges carry pass_through_estimate, elasticity_hint, or sector_sensitivity?
   c. do INFLUENCES edges specify channel_type, lag_quarters, observability?
   d. flag edges lacking quantitative qualifiers as missingInfo with specific questions (e.g., "What is the estimated pass-through rate for VAT increase on food retail?");
5. OBSERVABILITY COMPLETENESS: are there edges or segments with no plausible measurement source?
   Flag segments where effect measurement is unclear as missingInfo;
6. five-layer coverage check: Policy Source, Transmission Channel, Exposure, Response, Observability.
   Report which layers are missing or thin for the current fragment.

%s

Return ONLY valid JSON.`, taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}
