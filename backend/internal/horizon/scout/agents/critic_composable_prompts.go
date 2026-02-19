package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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

func buildUnitPhaseCriticBlock(phase models.UnitPipelinePhase) string {
	switch phase {
	case models.UnitPipelinePhaseP5:
		return `UNIT PIPELINE PHASE: P5 (Unit Dive Execution)
- Deep verification is allowed for selected unit-pair context.
- Validate deep chain consistency only within provided dive scope and evidence.`
	default:
		return `UNIT PIPELINE PHASE: P0-P3 (Critic Lite)
- Validate macro/unit coverage and qualifier quality.
- Do not require full legal-entity decomposition or SKU/contract detail.
- If deeper evidence is needed, return targeted missingInfo to trigger optional dive planning.`
	}
}

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

Return ONLY valid JSON.`, buildUnitPhaseCriticBlock(input.UnitPipelinePhase), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
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

%s
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
7. qualifier quality (confidence, source hints, transport qualifiers when applicable);
8. missing unit qualifiers should be explicit and resolved as unclassified when uncertain.

%s

Return ONLY valid JSON.`, buildUnitPhaseCriticBlock(input.UnitPipelinePhase), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), string(chainsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildGovernanceValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	return fmt.Sprintf(`You are a Critic validator for GOVERNANCE mode.

%s
Allowed relation types: OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, ADMINISTERS.
Allowed actor types: %s.

FRAGMENT:
%s

RELATIONS:
%s

Validate ownership and control coherence:
1. parent/subsidiary direction consistency;
2. board and executive coverage at practical broad level;
3. government hierarchy consistency when /gov/ entities appear;
4. missing information that should be researched before deep dive.

%s

Return ONLY valid JSON.`, buildUnitPhaseCriticBlock(input.UnitPipelinePhase), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeGovernance), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildMarketValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for MARKET mode.

%s
Allowed relation types: PARTICIPATES_IN, HAS_MARKET_SEGMENT.
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

Return ONLY valid JSON.`, buildUnitPhaseCriticBlock(input.UnitPipelinePhase), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeMarket), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildPolicyImpactValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for POLICY IMPACT mode.

%s
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

Return ONLY valid JSON.`, buildUnitPhaseCriticBlock(input.UnitPipelinePhase), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}
